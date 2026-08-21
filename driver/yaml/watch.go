package yaml

import (
	"cmp"
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
// A source naming no file, a directory that is not there, and a watcher the
// operating system would not give are all this. A watch that succeeded silently
// and never fired is the failure this refusal exists to avoid, so it is refused
// instead: the first at [ferry.BindWatched], the rest when the stream places
// its first registration, and both before any value reaches you.
//
// Ferry's own wrapper carries [ferry.ErrPlane], and this stays reachable
// underneath it, so [errors.Is] answers for both on what [ferry.BindWatched]
// and the stream returned.
var ErrWatch = errors.New("yaml: this watch could not be opened")

// settle is how long the mechanism waits for the file to stop changing before
// it answers.
//
// One editor save produces several events - a write, a rename, a chmod - and a
// reload per event is several reloads of one change. Fifty milliseconds is long
// enough to swallow that burst and short enough that a reload still lands while
// the operator is looking at the terminal (ADR-0020).
const settle = 50 * time.Millisecond

// Watched converts this source into one that can be watched.
//
//	wb, err := ferry.BindWatched[Config](yaml.NewSource("config.yaml").Watched())
//
// It takes no arguments, because this source already knows which file it reads
// and the mechanism has no interval to name.
//
// It touches nothing and starts nothing. Whether the file's directory can be
// watched is answered when a stream places its first registration, and the
// watch runs under that stream's own context.
//
// What is watched is the directory holding the file rather than the file
// itself, because an editor and this package's own sink both replace a file by
// renaming another over it, and a watch on the file survives that attached to
// an inode nobody reads any more. Everything else in that directory is ignored,
// including the files a save stages beside the plane.
//
// A file that does not exist yet is legal, as long as the directory holding it
// does: the watch fires when the file appears. A directory taken away under a
// running watch ends the stream with [ErrWatch] underneath [ferry.ErrWatchLost]
// rather than going quiet, because there is nothing left to watch and a process
// holding stale configuration has to be told.
//
// A dump through [NewSink] over the same path is a change like any other, so a
// process that both watches and saves its own configuration reloads its own
// writes. Nothing here suppresses that: a suppression window wide enough to
// cover a rename is wide enough to swallow somebody else's edit.
//
// The source it converts is unchanged and still loadable, and converting twice
// is two watchable sources over one path rather than a mistake.
func (s Source) Watched() *WatchedSource { return &WatchedSource{src: s} }

// WatchedSource is a [Source] that can be watched, and it is what
// [Source.Watched] returns. It loads exactly as the source it was converted
// from does.
type WatchedSource struct {
	src Source
}

var _ ferry.WatchableSource = (*WatchedSource)(nil)

// Bind is the source's own Bind, so a WatchedSource loads through [ferry.Bind]
// and [ferry.Load] like any other.
func (w *WatchedSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	return w.src.Bind(addrs)
}

// Watching answers with the mechanism this source's changes arrive through.
//
// It refuses a source naming no file at all, with [ErrWatch]. Everything the
// operating system has an opinion about - a directory that is not there, a
// watcher it will not give - is refused when the first registration is placed,
// which is still before any value reaches you.
//
// It does no I/O. The directory is opened when a registration is placed.
func (w *WatchedSource) Watching() (ferry.Notifier, error) {
	if w.src.path == "" {
		return nil, watchError("this source has no path to watch")
	}

	return &notifier{path: w.src.path}, nil
}

// notifier is the mechanism half: the file, and one registration per change.
type notifier struct {
	path string
}

// Notify registers for the next change to the file this source reads.
//
// The registration is live when Notify returns, so a save between this call and
// the wait that follows it is reported by that wait rather than missed
// (ADR-0020).
//
// One inotify watcher per registration is what the arm-once seam costs on this
// mechanism, and it is bounded: core arms the next registration before it
// releases the current one, so there are at most two open at once.
func (n *notifier) Notify(context.Context) (ferry.Change, error) {
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, watchFailure("no watch could be opened", err)
	}

	at := filepath.Clean(n.path)
	dir := filepath.Dir(at)

	if err := fs.Add(dir); err != nil {
		_ = fs.Close()

		return nil, watchFailure("the directory holding this file cannot be watched", err)
	}

	return &change{fs: fs, file: at, dir: dir}, nil
}

// change is one registration over its own inotify watcher.
//
// Both names are kept, and both are matched against later: an event about the
// file is a change, and an event about the directory is the watch itself going
// away (ADR-0020).
type change struct {
	fs   *fsnotify.Watcher
	file string
	dir  string
}

// Wait reports true on a change to the file this registration names, ctx's own
// error where ctx ended it, and an error where the watch was lost: the
// directory holding the file removed or renamed away, or the mechanism itself
// failing.
//
// A burst is one answer: one editor save produces a write, a rename and a
// chmod, so the first interesting event starts a [settle] window and the answer
// follows it.
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
// The window is a context rather than a bare timer so that the drain ends on
// whichever comes first, the settle or the caller's cancellation, and it is
// drained through the same turn the wait itself takes so that there is one
// place in this file that reads this watcher's two channels. What those turns
// concluded is thrown away: the change is already going to be reported, and a
// mechanism that is really gone fails at the next registration, which core
// places before the reload runs (ADR-0020).
func (c *change) coalesce(ctx context.Context) {
	window, stop := context.WithTimeout(ctx, settle)
	defer stop()

	for window.Err() == nil {
		_ = c.look(window)
	}
}

// Close releases the inotify watcher this registration holds.
func (c *change) Close() error { return c.fs.Close() }

// verdict is what one turn of the wait concluded, and the four constructors
// below are the whole of the set: nothing settled, the context ended it, the
// file changed, and the watch was lost. Only they build one, so a turn that is
// both a change and a failure cannot be spelled.
type verdict struct {
	changed bool
	err     error
	settled bool
}

// settledNothing is a turn about something this registration does not name.
func settledNothing() verdict { return verdict{} }

// watchEnded is the context ending the wait, which is the one silent ending.
func watchEnded() verdict { return verdict{settled: true} }

// watchChanged is the file this registration names holding something new.
func watchChanged() verdict { return verdict{changed: true, settled: true} }

// watchLost is the mechanism saying nothing further will ever arrive, and it is
// the one place a registration is spelled over: the directory going, the
// watcher failing, and its channels closing all mint here, so the shape of that
// ending is written once.
func watchLost(err error) verdict { return verdict{err: err, settled: true} }

// The two endings this file states itself, as errors rather than as messages,
// so that one wrapper mints every reason a registration is over.
//
// errWatchClosed is the watcher's own channels going away underneath a wait,
// which nothing but its own Close does. errDirectoryGone is the directory the
// watch is on being removed or renamed away.
var (
	errWatchClosed   = errors.New("this watch was closed")
	errDirectoryGone = errors.New("the directory holding this file is gone")
)

// look is one turn of the wait.
//
// Errors from the watcher are read rather than left unread, because an unread
// error channel stalls fsnotify's own goroutine, and here they are the watch
// being lost.
//
// Neither channel is asked whether it is still open, because the zero value it
// yields when it closes says so already: fsnotify sends no error that is nil
// and no event without an operation, so a nil error stands for
// [errWatchClosed] and a zero event is read as one by [change.event].
func (c *change) look(ctx context.Context) verdict {
	select {
	case <-ctx.Done():
		return watchEnded()
	case ev := <-c.fs.Events:
		return c.event(ev)
	case err := <-c.fs.Errors:
		return watchLost(watchCause(cmp.Or(err, errWatchClosed)))
	}
}

// event decides what one filesystem event means to this registration.
//
// A zero event is asked about first, because fsnotify sends none: it is the
// value a closed Events channel yields, and the watcher behind it is gone.
//
// The watched directory being removed or renamed away is asked about next,
// because it is the watch itself going and not the file changing underneath it.
// It has to be read from the directory's own event: the two ways of losing a
// directory do not deliver the same things. Removing one unlinks the file
// first, so an event names the file as well; renaming one away moves it whole,
// and the only event there is names the directory. A wait that listened for the
// file alone would sit through the second one forever (ADR-0020).
//
// Otherwise the name has to match exactly, which is what keeps the files a save
// stages beside the plane, and everything else in the directory, out of the
// answer. A permission change is not a change to what the file holds.
func (c *change) event(ev fsnotify.Event) verdict {
	if ev.Op == 0 {
		return watchLost(watchCause(errWatchClosed))
	}

	at := filepath.Clean(ev.Name)

	if at == c.dir && ev.Has(fsnotify.Remove|fsnotify.Rename) {
		return watchLost(watchCause(errDirectoryGone))
	}

	if ev.Has(fsnotify.Chmod) || at != c.file {
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

// watchCause is [watchError] over a reason that is already an error, wrapping
// it rather than printing it so that errors.Is and errors.As still answer for
// whatever it carries.
func watchCause(why error) error {
	return fmt.Errorf("%w: %w", ErrWatch, why)
}

// watchFailure is [watchError] over something that went wrong underneath it,
// and it wraps that error rather than printing it, so errors.Is and errors.As
// still answer for whatever the operating system said.
func watchFailure(msg string, cause error) error {
	return fmt.Errorf("%w: %s: %w", ErrWatch, msg, cause)
}
