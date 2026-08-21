//go:build protoe

package yaml

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/onhotpath/ferry"
)

// This file is variant E of the typed watch prototype, and it is where this
// driver's watch stops being a poll.
//
// [NewSource] is untouched and keeps returning a [Source]. Watching is a
// conversion on the source that already holds the path, so the file and the
// watch are named in one expression and there is nothing to wire.

// ErrWatch reports a watch this driver could not open.
//
// A directory that is not there, and a watcher the operating system would not
// give, are both this. A watch that succeeded silently and never fired is the
// failure this refusal exists to avoid, so it is refused before any load
// instead.
//
// It wraps [ferry.ErrNotWatchable], and it stays reachable under ferry's
// wrapper.
var ErrWatch = errors.New("yaml: this watch could not be opened")

// settle is how long the watcher waits for the file to stop changing before it
// answers.
//
// One editor save produces several events - a write, a rename, a chmod - and a
// reload per event is several reloads of one change. Fifty milliseconds is long
// enough to swallow that burst and short enough that a reload still lands while
// the operator is looking at the terminal.
const settle = 50 * time.Millisecond

// Watched converts this source into one that can be watched.
//
//	wb, err := ferry.BindWatched[Config](yaml.NewSource("app.yaml").Watched())
//
// It takes no arguments, because this source already knows which file it reads
// and the mechanism has no interval to name.
//
// It touches nothing and starts nothing. Whether the file's directory can be
// watched is answered at [ferry.BindWatched], and the watch itself opens when a
// stream does, under that stream's own context.
//
// What is watched is the directory holding the file rather than the file
// itself, because an editor and this package's own sink both replace a file by
// renaming another over it, and a watch on the file survives that attached to an
// inode nobody reads any more. Everything else in that directory is ignored,
// including the ".ferry-*" files a save stages.
//
// A file that does not exist yet is legal, as long as the directory holding it
// does: the watch fires when the file appears.
//
// A dump through [NewSink] over the same path is a change like any other, so a
// process that both watches and saves its own configuration reloads its own
// writes. Nothing here suppresses that: a suppression window wide enough to
// cover a rename is wide enough to swallow somebody else's edit.
//
// The source it converts is unchanged and still loadable, and converting twice
// is two watchable sources over one path rather than a mistake.
func (s Source) Watched() *WatchedSource { return &WatchedSource{path: s.path} }

// WatchedSource is a [Source] that can be watched, and it is what [Watched]
// returns. It loads exactly as the source it was converted from does.
type WatchedSource struct {
	path string
}

var _ ferry.WatchableSource = (*WatchedSource)(nil)

// Bind is the source's own Bind, so a WatchedSource loads through [ferry.Bind]
// and [ferry.Load] like any other.
func (w *WatchedSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	return Source{path: w.path}.Bind(addrs)
}

// Watching answers with the mechanism this source's changes arrive through.
//
// It refuses a source whose path names no file at all. Everything the operating
// system has an opinion about - a directory that is not there, a watcher it will
// not give - is refused when the first registration is placed, which is still
// before any value reaches the caller.
//
// It does no I/O. The directory is opened when a registration is placed.
func (w *WatchedSource) Watching() (ferry.Notifier, error) {
	if w.path == "" {
		return nil, watchError("this source has no path to watch")
	}

	return &notifier{path: w.path}, nil
}

// notifier is the mechanism half: the file, and one registration per change.
type notifier struct {
	path string
}

// Notify registers for the next change to the file this source reads.
//
// The registration is live when Notify returns, so a save between this call and
// the wait that follows it is reported by that wait rather than missed.
//
// One inotify watcher per registration is the cost this shape has on this
// mechanism, and it is bounded: the stream arms the next registration before it
// releases the current one, so there are at most two open at once.
func (n *notifier) Notify(context.Context) (ferry.Change, error) {
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, watchError("no watch could be opened: " + err.Error())
	}

	at := filepath.Clean(n.path)

	if err := fs.Add(filepath.Dir(at)); err != nil {
		_ = fs.Close()

		return nil, watchError("the directory holding this file cannot be watched: " + err.Error())
	}

	return &change{fs: fs, file: at}, nil
}

// change is one registration over its own inotify watcher.
type change struct {
	fs   *fsnotify.Watcher
	file string
}

// Wait reports true on a change to the file this registration names, false with
// a nil error where ctx ended it, and an error where the watch was lost.
//
// A burst is one answer. One editor save produces a write, a rename and a
// chmod, so the first interesting event starts a settle window and the answer
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

		return v.changed, v.err
	}
}

// Close releases the watcher this registration holds.
func (c *change) Close() error { return c.fs.Close() }

// verdict is what one turn of the wait concluded. An event about something else
// in the directory is a turn that settled nothing.
type verdict struct {
	changed bool
	err     error
	settled bool
}

// look is one turn of the wait.
//
// Errors from the watcher are read rather than left unread, because an unread
// error channel stalls fsnotify's own goroutine, and an error here is the watch
// being lost.
func (c *change) look(ctx context.Context) verdict {
	select {
	case <-ctx.Done():
		return verdict{settled: true}
	case ev, open := <-c.fs.Events:
		if !open {
			return verdict{err: watchError("this watch was closed"), settled: true}
		}

		return c.event(ctx, ev)
	case err, open := <-c.fs.Errors:
		if !open {
			return verdict{err: watchError("this watch was closed"), settled: true}
		}

		return verdict{err: watchError(err.Error()), settled: true}
	}
}

// event decides what one filesystem event means to this registration.
//
// The name has to match exactly, which is what keeps the ".ferry-*" files a save
// stages, and everything else in the directory, out of the answer. A permission
// change is not a change to what the file holds.
//
// The context is read a second time here, because a select whose cases are both
// ready picks between them at random and a cancelled watch must not report one
// more change afterwards.
func (c *change) event(ctx context.Context, ev fsnotify.Event) verdict {
	if ev.Has(fsnotify.Chmod) || filepath.Clean(ev.Name) != c.file {
		return verdict{}
	}

	if ctx.Err() != nil {
		return verdict{settled: true}
	}

	return verdict{changed: true, settled: true}
}

// coalesce drains the rest of the burst one save produces into the one change
// that follows it.
func (c *change) coalesce(ctx context.Context) {
	timer := time.NewTimer(settle)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			return
		case <-ctx.Done():
			return
		case <-c.fs.Events:
		case <-c.fs.Errors:
		}
	}
}

// watchError states the class this driver has an opinion about and keeps
// [ErrWatch] reachable underneath it.
func watchError(msg string) error {
	return fmt.Errorf("%w: %s", ErrWatch, msg)
}
