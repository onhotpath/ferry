//go:build protoa

package env

import (
	"context"
	"path/filepath"

	"github.com/fsnotify/fsnotify"

	"github.com/onhotpath/ferry"
)

// This file is variant A of the typed watch prototype, ported to the real
// fsnotify mechanism this driver already ships.
//
// It replaces [WatchFiles] rather than joining it. There is no Option, no
// context held in the source, no goroutine started by [New] and no callback:
// the whole of the watch is one method the core stream calls, and the lifetime
// is the context that stream was opened with.

// BindWatch refuses a source that has nothing to watch.
//
// [WatchFiles] refused this at Bind by holding a watcher the whole source knew
// about; this does the same refusal with no state at all, because whether there
// is anything to watch is a question about the options and nothing else.
func (s *Source) BindWatch() error {
	if len(s.cfg.dotenv) == 0 {
		return watchError("this source watches no files: name them with env.DotEnv")
	}

	return nil
}

// Notify registers for the next change to a file [DotEnv] named.
//
// The registration is live when Notify returns, so a save between this call and
// the wait that follows it is reported by that wait rather than missed.
//
// It refuses when no [DotEnv] named a file, and when a directory holding one is
// not there, with an error wrapping [ErrWatch]: a watch that opens successfully
// and never fires is the failure this refusal exists to avoid.
//
// The directory holding each file is watched rather than the file itself,
// because an editor and this package's own sink both replace a file by renaming
// another over it, and a watch on the file survives that attached to an inode
// nobody reads any more. Everything else in those directories is ignored,
// including the ".ferry-*" files a save stages.
//
// Coalescing is not done here. A burst of events for one save is a burst of
// changes, and holding a reload back until the plane is quiet is the caller's
// setting rather than a constant buried in this package.
func (s *Source) Notify(context.Context) (ferry.Change, error) {
	if err := s.BindWatch(); err != nil {
		return nil, err
	}

	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, watchError("no watch could be opened: " + err.Error())
	}

	files, err := addDirs(fs, s.cfg.dotenv)
	if err != nil {
		_ = fs.Close()

		return nil, err
	}

	return &fileChange{fs: fs, files: files}, nil
}

// addDirs watches the directory holding each file and answers with the resolved
// file names, so an event can be matched by name.
func addDirs(fs *fsnotify.Watcher, paths []string) (map[string]bool, error) {
	files := make(map[string]bool, len(paths))
	dirs := make(map[string]bool, len(paths))

	for _, path := range paths {
		at := target(path)
		files[at] = true

		dir := filepath.Dir(at)
		if dirs[dir] {
			continue
		}

		dirs[dir] = true

		if err := fs.Add(dir); err != nil {
			return nil, watchError("the directory holding one of these files cannot be watched: " + err.Error())
		}
	}

	return files, nil
}

// fileChange is one registration over its own inotify watcher.
//
// One watcher per registration is the cost this shape has on this mechanism, and
// it is bounded: the stream arms the next registration before it releases the
// current one, so there are at most two open at once and one per change
// afterwards.
type fileChange struct {
	fs    *fsnotify.Watcher
	files map[string]bool
}

// Wait reports true on a change to a file this registration names, false with a
// nil error where ctx ended it, and an error where the watch was lost.
func (c *fileChange) Wait(ctx context.Context) (bool, error) {
	for {
		if v := c.look(ctx); v.settled {
			return v.changed, v.err
		}
	}
}

// verdict is what one turn of the wait concluded. An event about a file this
// registration does not name is a turn that settled nothing.
type verdictA struct {
	changed bool
	err     error
	settled bool
}

func (c *fileChange) look(ctx context.Context) verdictA {
	select {
	case <-ctx.Done():
		return verdictA{settled: true}
	case ev, open := <-c.fs.Events:
		if !open {
			return verdictA{err: watchError("this watch was closed"), settled: true}
		}

		return c.event(ctx, ev)
	case err, open := <-c.fs.Errors:
		if !open {
			return verdictA{err: watchError("this watch was closed"), settled: true}
		}

		return verdictA{err: watchError(err.Error()), settled: true}
	}
}

// event decides what one filesystem event means to this registration.
//
// The context is read a second time here, because a select whose cases are both
// ready picks between them at random and a cancelled watch must not report one
// more change afterwards.
func (c *fileChange) event(ctx context.Context, ev fsnotify.Event) verdictA {
	if ev.Has(fsnotify.Chmod) || !c.files[filepath.Clean(ev.Name)] {
		return verdictA{}
	}

	if ctx.Err() != nil {
		return verdictA{settled: true}
	}

	return verdictA{changed: true, settled: true}
}

func (c *fileChange) Close() error { return c.fs.Close() }
