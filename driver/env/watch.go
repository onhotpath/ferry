package env

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/onhotpath/ferry"
)

// ErrWatch reports a watch this driver could not open.
//
// A source that names no file with [DotEnv] is refused at [ferry.BindWatched]
// under this, before any load, because a watch that opened successfully and
// never fired is the failure that refusal exists to avoid. A directory that is
// not there, or one removed or moved away while a stream runs, carries this
// too, on the stream, under [ferry.ErrWatchLost].
//
// Ferry's own wrapper carries [ferry.ErrPlane], and this stays reachable
// underneath it, so errors.Is answers for both on what a bind returned and on
// what ended a stream.
var ErrWatch = errors.New("env: this watch could not be opened")

// settle is how long a wait gives a file to stop changing before it answers.
//
// One editor save produces several events - a write, a rename, a chmod - and a
// reload per event is several reloads of one change. Fifty milliseconds is long
// enough to swallow that burst and short enough that a reload still lands while
// the operator is looking at the terminal (ADR-0020).
const settle = 50 * time.Millisecond

// Watched converts this source into one that can be watched.
//
//	wb, err := ferry.BindWatched[Config](env.New(env.DotEnv(".env")).Watched())
//
// It takes no arguments, because this source already knows which files it reads:
// what a watch needs here is what [DotEnv] already named.
//
// It touches nothing and starts nothing. Whether there is anything to watch is
// answered at [ferry.BindWatched], and the watch itself opens when a stream
// does, under that stream's own context.
//
// The source it converts is unchanged and still loadable, so a caller who stops
// watching changes one call rather than two. Converting twice is two watchable
// sources over one configuration and is not a mistake: a source is settled
// configuration, and nothing here consumes it.
func (s *Source) Watched() *WatchedSource { return &WatchedSource{src: s} }

// WatchedSource is a [Source] that can be watched, and [Source.Watched] is the
// only way there is to build one. It loads exactly as the source it was
// converted from does.
//
// Changes arrive through the operating system's own file notifications, so a
// hand edit lands without polling latency. What is watched is the directory
// holding each file rather than the file itself, because an editor and this
// package's own sink both replace a file by renaming another over it, and a
// watch on the file survives that attached to an inode nobody reads any more.
// Everything else in those directories is ignored, including the ".ferry-*"
// files a save stages.
//
// Sharp edges.
//
// A burst is one change. One editor save produces a write, a rename and a chmod,
// and a short window after the first of them swallows the rest.
//
// A dump through [DotEnvSink] over the same path is a change like any other, so
// a process that both watches and saves its own configuration hears its own
// writes. Nothing here suppresses that: a suppression window wide enough to
// cover a rename is wide enough to swallow somebody else's edit.
//
// A change says a file may hold something new and nothing more. The reload is
// what reads it, which is correct whether the change was real, coalesced with
// another, or a touch that rewrote the same bytes.
//
// Losing the directory the watch is on - removed, or moved out from under it -
// ends the stream under [ferry.ErrWatchLost], with [ErrWatch] reachable
// underneath, rather than leaving a process quietly holding stale
// configuration.
type WatchedSource struct {
	src *Source
}

var _ ferry.WatchableSource = (*WatchedSource)(nil)

// Bind computes this schema's environment variable names and checks them,
// exactly as [Source.Bind] does.
func (w *WatchedSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	return w.src.Bind(addrs)
}

// Watching answers with the mechanism this source's changes arrive through.
//
// It refuses a source with nothing to watch: no [DotEnv] named a file. That
// refusal lands at [ferry.BindWatched], before any load, and it carries
// [ErrWatch].
//
// It does no I/O. The directories are opened when a stream places its first
// registration, so a directory that is not there is reported on the stream.
func (w *WatchedSource) Watching() (ferry.Notifier, error) {
	if len(w.src.cfg.dotenv) == 0 {
		return nil, watchError("this source watches no files: name them with env.DotEnv")
	}

	return &notifier{paths: w.src.cfg.dotenv}, nil
}

// notifier is the mechanism half: the files, and one registration per change.
type notifier struct {
	paths []string
}

// Notify registers for the next change to a file [DotEnv] named.
//
// The registration is live when Notify returns, so a save between this call and
// the wait that follows it is reported by that wait rather than missed
// (ADR-0020).
//
// One inotify watcher per registration is the cost the arm-once shape has on
// this mechanism, and it is bounded: the stream arms the next registration
// before it releases the current one, so there are at most two open at once.
//
// Nothing is lost in the swap. The stream places the next registration before
// it runs the reload (ADR-0020), so an edit landing while the old watcher is
// being released is read by that reload, and an edit landing after it is
// reported by the registration this call just placed.
func (n *notifier) Notify(context.Context) (ferry.Change, error) {
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, watchFailure("no watch could be opened", err)
	}

	c, err := watchDirs(fs, n.paths)
	if err != nil {
		_ = fs.Close()

		return nil, err
	}

	return c, nil
}

// watchDirs watches the directory holding each file and answers with the
// registration over them.
//
// Both names are kept, and both are matched against later: an event about a
// file is a change, and an event about a directory is the watch itself.
//
// The directories are deduplicated, so several files in one directory open one
// watch, and the files are resolved through the same symlink following a save
// uses.
func watchDirs(fs *fsnotify.Watcher, paths []string) (*change, error) {
	c := &change{
		fs:    fs,
		files: make(map[string]bool, len(paths)),
		dirs:  make(map[string]bool, len(paths)),
	}

	for _, path := range paths {
		at := target(path)
		c.files[at] = true

		dir := filepath.Dir(at)
		if c.dirs[dir] {
			continue
		}

		c.dirs[dir] = true

		if err := fs.Add(dir); err != nil {
			return nil, watchFailure("the directory holding one of these files cannot be watched", err)
		}
	}

	return c, nil
}

// change is one registration over its own inotify watcher.
type change struct {
	fs *fsnotify.Watcher
	// files is what a change is about, and dirs is what the watch is on. An
	// event naming one of the latter is the watch going away rather than a
	// file moving underneath it (ADR-0020).
	files map[string]bool
	dirs  map[string]bool
}

// Wait reports true on a change to a file this registration names, ctx's own
// error where ctx ended it, and an error where the watch was lost: the
// directory holding a watched file removed or renamed away, or the mechanism
// itself failing.
//
// A burst is one answer: the first interesting event starts a [settle] window,
// and the answer follows it.
func (c *change) Wait(ctx context.Context) (bool, error) {
	for {
		v := c.look(ctx)
		if !v.settled {
			continue
		}

		if v.changed {
			c.coalesce(ctx)
		}

		// The cancellation outranks whatever the mechanism saw, and this is
		// where that is decided (ADR-0020). A select whose cases are both ready
		// picks between them at random, and a burst may settle after ctx ended,
		// so a wait that answered with the verdict alone could report one more
		// change after the stream was cancelled. Core reads this as the
		// cancellation whichever way a driver spells it, and saying so outright
		// is the honest spelling.
		if err := ctx.Err(); err != nil {
			return false, err
		}

		return v.changed, v.err
	}
}

// coalesce drains the rest of the burst one save produces into the one change
// that follows it.
//
// The window is a context rather than a bare timer so that the wait ends on
// whichever comes first, the settle or the caller's cancellation, through one
// case instead of two.
func (c *change) coalesce(ctx context.Context) {
	window, stop := context.WithTimeout(ctx, settle)
	defer stop()

	for {
		select {
		case <-window.Done():
			return
		case <-c.fs.Events:
		case <-c.fs.Errors:
		}
	}
}

// Close releases the inotify watcher this registration holds.
func (c *change) Close() error { return c.fs.Close() }

// verdict is what one turn of the wait concluded, and the four constructors
// below are the whole of the set: nothing settled, the context ended it, a file
// this registration names changed, and the watch was lost. Only they build one,
// so a turn that is both a change and a failure cannot be spelled.
type verdict struct {
	changed bool
	err     error
	settled bool
}

// settledNothing is a turn about a file this registration does not name.
func settledNothing() verdict { return verdict{} }

// watchEnded is the context ending the wait, which is the one silent ending.
func watchEnded() verdict { return verdict{settled: true} }

// watchChanged is a file this registration names holding something new.
func watchChanged() verdict { return verdict{changed: true, settled: true} }

// watchLost is the mechanism saying nothing further will ever arrive, and it is
// the one place a registration is spelled over: the directory going, the
// watcher failing, and its channels closing all mint here, so the shape of that
// ending is written once.
func watchLost(err error) verdict { return verdict{err: err, settled: true} }

// closedWatch is what a wait concluded when the watcher's own channels went
// away underneath it, which nothing but its own Close does.
const closedWatch = "this watch was closed"

// look is one turn of the wait.
//
// Errors from the watcher are read rather than left unread, because an unread
// error channel stalls fsnotify's own goroutine, and here they are the watch
// being lost.
func (c *change) look(ctx context.Context) verdict {
	select {
	case <-ctx.Done():
		return watchEnded()
	case ev, open := <-c.fs.Events:
		if !open {
			return watchLost(watchError(closedWatch))
		}

		return c.event(ev)
	case err, open := <-c.fs.Errors:
		if !open {
			return watchLost(watchError(closedWatch))
		}

		return watchLost(watchFailure("this watch failed", err))
	}
}

// event decides what one filesystem event means to this registration.
//
// A watched directory that is removed or renamed away is asked about first,
// because it is the watch itself going and not a file changing underneath it.
// It has to be read from the directory's own event: the two ways of losing a
// directory do not deliver the same things. Removing one unlinks the file
// first, so an event names the file as well; renaming one away moves it whole,
// and the only event there is names the directory. A wait that listened for the
// file alone would sit through the second one forever (ADR-0020).
//
// Otherwise the name has to match a watched file exactly, which is what keeps
// the ".ferry-*" files a save stages, and everything else in the directory, out
// of the answer. A permission change is not a change to what the file holds.
func (c *change) event(ev fsnotify.Event) verdict {
	at := filepath.Clean(ev.Name)

	if c.dirs[at] && ev.Has(fsnotify.Remove|fsnotify.Rename) {
		return watchLost(watchError("the directory holding a watched file is gone"))
	}

	if ev.Has(fsnotify.Chmod) || !c.files[at] {
		return settledNothing()
	}

	return watchChanged()
}

// watchError states the reason under [ErrWatch] and nothing more. Core stamps
// [ferry.ErrPlane] once at its own seam - the bind refusal and the stream
// ending both wrap it there - so minting it here as well would spell the
// prefix twice (ADR-0020).
func watchError(msg string) error {
	return fmt.Errorf("%w: %s", ErrWatch, msg)
}

// watchFailure is [watchError] over something that went wrong underneath it,
// and it wraps that error rather than printing it, so errors.Is and errors.As
// still answer for whatever the operating system said.
func watchFailure(msg string, cause error) error {
	return fmt.Errorf("%w: %s: %w", ErrWatch, msg, cause)
}
