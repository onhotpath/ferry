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
// [WatchFiles] with no [DotEnv] beside it, and a directory that is not there, are
// both this: a watch that succeeded silently and never fired is the failure mode
// the option exists to avoid, so it is refused at Bind instead.
//
// It wraps [ferry.ErrPlane], and it stays reachable under ferry's wrapper.
var ErrWatch = errors.New("env: this watch could not be opened")

// settle is how long the watcher waits for a file to stop changing before it
// calls back.
//
// One editor save produces several events - a write, a rename, a chmod - and a
// reload per event is several reloads of one change. Fifty milliseconds is long
// enough to swallow that burst and short enough that a reload still lands while
// the operator is looking at the terminal.
const settle = 50 * time.Millisecond

// watcher is the whole of the [WatchFiles] option: what to call, which files
// count, and the context that ends it.
//
// The context is held rather than passed in because there is nowhere to pass one
// (ADR-0020). Watching starts when the source is built and outlives every
// individual load, so the lifetime cannot come from a load's context, and the
// driver grows no Stop method for it: core ships no watch lifecycle, and one
// invented here would be a second lifecycle beside the one the caller already
// has.
type watcher struct {
	ctx      context.Context
	onChange func(context.Context)

	// files is every path being watched, resolved through the same symlink
	// following a save uses, so that an event's name can be matched exactly.
	files map[string]bool

	fs *fsnotify.Watcher
}

// WatchFiles calls onChange whenever a file [DotEnv] named changes, so that a
// process holding a loaded value can load a fresh one.
//
//	b, err := ferry.Bind[Config](env.New(env.DotEnv(path), env.WatchFiles(ctx, reload)))
//
//	func reload(ctx context.Context) {
//		cfg, err := b.Load(ctx) // a reload is a load
//		...                     // publish it by replacement, never by mutation
//	}
//
// It is opt-in and it is the only thing in this package that runs on a goroutine
// of its own. A source built without it touches the files only when a load asks
// it to.
//
// The watch begins when the source is built and ends when ctx is done, which is
// the only way to stop it: cancel the context you gave it, and the goroutine
// returns. The context reaches onChange as its argument, so a deadline, a
// cancellation and whatever the caller put in it are all in hand there.
//
// What is watched is the directory holding each file rather than the file itself,
// because an editor and this package's own sink both replace a file by renaming
// another over it, and a watch on the file survives that attached to an inode
// nobody reads any more. Everything else in those directories is ignored,
// including the ".ferry-*" files a save stages.
//
// It refuses at Bind, before any load, when no [DotEnv] named a file or when a
// directory holding one is not there. A watch that opens successfully and never
// fires is the failure this option exists to avoid.
//
// Sharp edges, and they are the reason this is a callback and not a stream.
//
// onChange runs on the watching goroutine and one call at a time. A callback that
// reloads inline is a slow one, and a slow callback delays the next look rather
// than running beside itself, so changes that land while it runs are one call
// afterwards rather than several. The Changed method of a Signal from
// github.com/onhotpath/ferry/watch returns immediately instead, which leaves the
// reload on the goroutine ranging the stream and this one free to keep looking.
//
// A panic in onChange takes the process down, exactly as it would on a goroutine
// the caller started. Nothing here recovers it: there is no result to hand a
// failure back through, and a watch that swallowed the panic would leave a
// process that has silently stopped reloading.
//
// Watching starts when the source is built, so it starts before [ferry.Bind] has
// handed back the binding the callback wants to load through, and a change can
// land while there is nothing yet to load through. A Signal from
// github.com/onhotpath/ferry/watch is what to pass here in that case: its Changed
// method records such a change rather than losing it, and the stream that opens
// afterwards begins with that reload, as the example in this package does.
//
// A call says a file may have changed and nothing more. Load to find out what it
// holds now, which is correct whether the change was real, coalesced with
// another, or a touch that rewrote the same bytes.
//
// A dump through [DotEnvSink] over the same path fires it, so a process that both
// watches and saves its own configuration hears its own writes. Nothing here
// suppresses that: a reload reads back what was just written, and a suppression
// window wide enough to cover a rename is wide enough to swallow somebody else's
// edit.
//
// Losing the watch - the directory removed or replaced - fires the callback once
// and stops. There is nowhere to report it, and the load that follows reports it
// through a surface the caller already handles. A cancelled context stops
// silently instead, so only losing the watch speaks.
func WatchFiles(ctx context.Context, onChange func(context.Context)) Option {
	return sourceOnly(func(c *config) {
		if onChange == nil {
			return
		}

		c.watch = &watcher{ctx: ctx, onChange: onChange}
	})
}

// start opens the watch and puts it on a goroutine of its own, and it runs inside
// [New], on the caller's goroutine.
//
// That placement is what gives a failure somewhere to go, and it is also what
// means no change is missed between New returning and the goroutine first
// running: the inotify watch is already open by then.
func (w *watcher) start(paths []string) error {
	if len(paths) == 0 {
		return watchError("env.WatchFiles was given no files to watch: name them with env.DotEnv")
	}

	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return watchError("no watch could be opened: " + err.Error())
	}

	if err := w.add(fs, paths); err != nil {
		_ = fs.Close()

		return err
	}

	w.fs = fs

	go w.run()

	return nil
}

// add resolves each file and watches the directory holding it.
//
// The directories are deduplicated, so several files in one directory open one
// watch, and the files are kept so an event can be matched by name.
func (w *watcher) add(fs *fsnotify.Watcher, paths []string) error {
	w.files = make(map[string]bool, len(paths))
	dirs := make(map[string]bool, len(paths))

	for _, path := range paths {
		at := target(path)
		w.files[at] = true

		dir := filepath.Dir(at)
		if dirs[dir] {
			continue
		}

		dirs[dir] = true

		if err := fs.Add(dir); err != nil {
			return watchError("the directory holding one of these files cannot be watched: " + err.Error())
		}
	}

	return nil
}

// run is the loop: collect events, coalesce them, call back, until the context
// ends or the watch is lost.
//
// The callback runs on this goroutine and one at a time, so a slow callback
// delays the next look rather than piling up beside itself, and changes that land
// while it runs coalesce into the burst that follows. That is the same contract a
// driver's change signal carries anywhere: it says the plane may have changed,
// and the reload is what reads the truth (ADR-0020).
//
// Nothing here fences the callback. It is user code, and the panicking call is
// the top of this goroutine's own stack.
func (w *watcher) run() {
	defer func() { _ = w.fs.Close() }()

	for {
		switch w.wait() {
		case changed:
			w.coalesce()
			w.fire()
		case lost:
			w.fire()

			return
		default:
			// stopped, and it is the only silent ending.
			return
		}
	}
}

// verdict is what one turn of the loop concluded, and the set is closed at three.
type verdict uint8

const (
	// stopped means the context is done, and it is the only silent ending.
	stopped verdict = iota
	// lost means the watch is gone: the directory was removed or replaced, and
	// nothing further will ever be reported through it.
	lost
	// changed means a file this watch names may hold something new.
	changed
)

// wait blocks until something happens to one of the watched files.
//
// Errors from the watcher are read and dropped rather than left unread, because
// an unread error channel stalls fsnotify's own goroutine. There is nowhere to
// report one, and the load that follows the next real change reports whatever is
// actually wrong.
func (w *watcher) wait() verdict {
	for {
		if v, settled := w.look(); settled {
			return v
		}
	}
}

// look is one turn of the wait, and it reports whether that turn settled the
// question: an event about a file this watch does not name is a turn that did
// not.
func (w *watcher) look() (verdict, bool) {
	select {
	case <-w.ctx.Done():
		return stopped, true
	case ev, open := <-w.fs.Events:
		if !open {
			return lost, true
		}

		return w.live(), w.interesting(ev)
	case _, open := <-w.fs.Errors:
		return lost, !open
	}
}

// live is the second read of the context, and it is the one that matters: a
// select whose cases are both ready picks between them at random, so a watch
// cancelled while an event was pending would otherwise be able to call back once
// more afterwards. Cancelling is the only way to stop a watch, so it has to mean
// stopped.
func (w *watcher) live() verdict {
	if w.ctx.Err() != nil {
		return stopped
	}

	return changed
}

// interesting reports whether one event is about a file this watch names.
//
// The name has to match exactly, which is what keeps the ".ferry-*" files a save
// stages, and everything else in the directory, out of the callback. A permission
// change is not a change to what the file holds.
func (w *watcher) interesting(ev fsnotify.Event) bool {
	return !ev.Has(fsnotify.Chmod) && w.files[filepath.Clean(ev.Name)]
}

// coalesce drains the burst one save produces into the one callback that
// follows it.
func (w *watcher) coalesce() {
	timer := time.NewTimer(settle)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			return
		case <-w.ctx.Done():
			return
		case <-w.fs.Events:
		case <-w.fs.Errors:
		}
	}
}

// fire calls back, unless the context ended while the burst was settling.
func (w *watcher) fire() {
	if w.ctx.Err() == nil {
		w.onChange(w.ctx)
	}
}

// watchError states the class this driver has an opinion about and keeps
// [ErrWatch] reachable underneath it.
func watchError(msg string) error {
	return fmt.Errorf("%w: %w: %s", ferry.ErrPlane, ErrWatch, msg)
}
