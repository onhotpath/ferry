//go:build protoc

package env

import (
	"path/filepath"

	"github.com/fsnotify/fsnotify"

	"github.com/onhotpath/ferry"
)

// This file is variant C of the typed watch prototype ported to the real
// fsnotify mechanism.
//
// The driver keeps everything it already had: one constructor, one Option, a
// watch opened synchronously inside [New] on the caller's own goroutine, and a
// goroutine of its own afterwards. What changes is what the Option takes and
// what the goroutine can say. It takes a handle instead of a context and a
// callback, so there is one lifetime rather than two; and it can report that
// the watch is over, which is the one thing [WatchFiles] has nowhere to put.

// Watched watches the files [DotEnv] named and announces their changes into h.
//
//	h := ferry.NewWatch(ctx)
//	src := env.New(env.DotEnv(".env"), env.Watched(h))
//	wb, err := ferry.BindWatched[Config](src, h)
//
// It is opt-in and it is the only thing in this package that runs on a
// goroutine of its own. A source built without it touches the files only when a
// load asks it to.
//
// The watch begins when the source is built and ends when the handle's context
// is done, which is the only way to stop it. There is no second context to keep
// in step with: the one the handle was opened with is the driver's and the
// stream's alike.
//
// It declines, rather than starting a watch that would never fire, when no
// [DotEnv] named a file or a directory holding one is not there, and the
// refusal surfaces at [ferry.BindWatched], before any load.
//
// Losing the watch - the directory removed or replaced - ends the stream with
// the reason, so a process is never left holding stale configuration with
// nothing to tell it so.
//
// What is watched is the directory holding each file rather than the file
// itself, because an editor and this package's own sink both replace a file by
// renaming another over it. Everything else in those directories is ignored,
// including the ".ferry-*" files a save stages.
//
// Coalescing a burst is not done here. It is the caller's setting on the
// stream rather than a constant buried in this package.
func Watched(h *ferry.Watch) Option { return watchedOpt{h: h} }

// watchedOpt is [Watched]'s value, and it is a type of its own so that [New] can
// recognise it once the Source it configures has been built. It settles nothing
// into the config: what it carries is the caller's handle and nothing else.
type watchedOpt struct{ h *ferry.Watch }

func (watchedOpt) apply(*config) {}

// afterNew starts the watch the [Watched] option asked for, and it is called by
// [New] once the Source is built, so the handle is wired to the value the caller
// will hand to [ferry.BindWatched].
func afterNew(opts []Option, s *Source) {
	for _, o := range opts {
		w, ok := o.(watchedOpt)
		if !ok || w.h == nil {
			continue
		}

		startPortWatch(w.h, s, s.cfg.dotenv)

		return
	}
}

// portWatcher is the whole of the [Watched] option once [New] has started it.
type portWatcher struct {
	port  *ferry.WatchPort
	files map[string]bool
	fs    *fsnotify.Watcher
}

// startPortWatch opens the watch and puts it on a goroutine of its own, and it
// runs inside [New] on the caller's goroutine.
//
// That placement is what means no change is missed between New returning and
// the goroutine first running: the inotify watch is already open by then. A
// failure declines through the port rather than being dropped, so it lands at
// the bind.
func startPortWatch(h *ferry.Watch, s *Source, paths []string) {
	port := h.Wire(s)

	fs, files, err := openWatch(paths)
	if err != nil {
		port.Refuse(err)

		return
	}

	go (&portWatcher{port: port, files: files, fs: fs}).run()
}

// openWatch opens one inotify watcher over the directories holding paths.
func openWatch(paths []string) (*fsnotify.Watcher, map[string]bool, error) {
	if len(paths) == 0 {
		return nil, nil, watchError("this source watches no files: name them with env.DotEnv")
	}

	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, nil, watchError("no watch could be opened: " + err.Error())
	}

	files := make(map[string]bool, len(paths))
	dirs := make(map[string]bool, len(paths))

	for _, path := range paths {
		at := target(path)
		files[at] = true

		if err := addDir(fs, dirs, filepath.Dir(at)); err != nil {
			_ = fs.Close()

			return nil, nil, err
		}
	}

	return fs, files, nil
}

// addDir watches one directory, once.
func addDir(fs *fsnotify.Watcher, seen map[string]bool, dir string) error {
	if seen[dir] {
		return nil
	}

	seen[dir] = true

	if err := fs.Add(dir); err != nil {
		return watchError("the directory holding one of these files cannot be watched: " + err.Error())
	}

	return nil
}

// run is the loop: announce every change, and report the ending.
func (w *portWatcher) run() {
	defer func() { _ = w.fs.Close() }()

	for {
		if over, why := w.turn(); over {
			w.end(why)

			return
		}
	}
}

// turn is one look at the queue, reporting whether the watch is over and why.
//
// A nil reason with over set is the caller's own cancellation, which the stream
// already knows about and which is the one ending nothing is reported for.
func (w *portWatcher) turn() (bool, error) {
	select {
	case <-w.port.Context().Done():
		return true, nil
	case ev, open := <-w.fs.Events:
		if !open {
			return true, watchError("this watch was closed")
		}

		w.saw(ev)

		return false, nil
	case err, open := <-w.fs.Errors:
		if !open {
			return true, watchError("this watch was closed")
		}

		return true, watchError(err.Error())
	}
}

// end reports the ending, unless there is nothing to report.
func (w *portWatcher) end(why error) {
	if why == nil {
		return
	}

	w.port.Ended(why)
}

// saw announces one event that is about a file this watch names.
func (w *portWatcher) saw(ev fsnotify.Event) {
	if ev.Has(fsnotify.Chmod) || !w.files[filepath.Clean(ev.Name)] {
		return
	}

	if w.port.Context().Err() != nil {
		return
	}

	w.port.Changed()
}
