//go:build protoe

package env

import (
	"context"
	"path/filepath"

	"github.com/fsnotify/fsnotify"

	"github.com/onhotpath/ferry"
)

// This file is variant E of the typed watch prototype ported to the real
// fsnotify mechanism, and it is where the variant's driver-side cost is: one
// method and one small type.
//
// [New] is untouched and keeps returning a [Source]. Watching is a conversion
// on the source that already holds the files, so the file to watch and the
// watch itself are named in one place and there is no wiring to forget.

// Watched converts this source into one that can be watched.
//
//	wb, err := ferry.BindWatched[Config](env.New(env.DotEnv(".env")).Watched())
//
// It takes no arguments, because this source already knows which files it
// reads: what a watch needs here is what [DotEnv] already named. A driver whose
// watch needs something the source does not have takes it here instead, which
// is where a poll interval belongs.
//
// It touches nothing and starts nothing. Whether there is anything to watch is
// answered at [ferry.BindWatched], and the watch itself opens when a stream
// does, under that stream's own context.
//
// The source it converts is unchanged and still loadable. Converting twice is
// two watchable sources over one configuration and is not a mistake: a source
// is settled configuration, and nothing here consumes it.
func (s *Source) Watched() *WatchedSource { return &WatchedSource{src: s} }

// WatchedSource is a [Source] that can be watched, and it is what [Watched]
// returns. It loads exactly as the source it was converted from does.
type WatchedSource struct {
	src *Source
}

var _ ferry.WatchableSource = (*WatchedSource)(nil)

// Bind is the source's own Bind, so a WatchedSource loads through [ferry.Bind]
// and [ferry.Load] like any other and a caller who stops watching changes one
// call rather than two.
func (w *WatchedSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	return w.src.Bind(addrs)
}

// Watching answers with the mechanism this source's changes arrive through.
//
// It refuses a source with nothing to watch: no [DotEnv] named a file. A watch
// that opens successfully and never fires is the failure this refusal exists to
// avoid, and it lands at [ferry.BindWatched], before any load.
//
// It does no I/O. The directories are opened when a registration is placed.
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
// the wait that follows it is reported by that wait rather than missed.
//
// The directory holding each file is watched rather than the file itself,
// because an editor and this package's own sink both replace a file by renaming
// another over it, and a watch on the file survives that attached to an inode
// nobody reads any more. Everything else in those directories is ignored,
// including the ".ferry-*" files a save stages.
//
// One inotify watcher per registration is the cost this shape has on this
// mechanism, and it is bounded: the stream arms the next registration before it
// releases the current one, so there are at most two open at once.
func (n *notifier) Notify(context.Context) (ferry.Change, error) {
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, watchError("no watch could be opened: " + err.Error())
	}

	files, err := watchDirs(fs, n.paths)
	if err != nil {
		_ = fs.Close()

		return nil, err
	}

	return &change{fs: fs, files: files}, nil
}

// watchDirs watches the directory holding each file and answers with the
// resolved file names, so an event can be matched by name.
func watchDirs(fs *fsnotify.Watcher, paths []string) (map[string]bool, error) {
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

// change is one registration over its own inotify watcher.
type change struct {
	fs    *fsnotify.Watcher
	files map[string]bool
}

// Wait reports true on a change to a file this registration names, false with a
// nil error where ctx ended it, and an error where the watch was lost.
func (c *change) Wait(ctx context.Context) (bool, error) {
	for {
		if v := c.look(ctx); v.settled {
			return v.changed, v.err
		}
	}
}

func (c *change) Close() error { return c.fs.Close() }

// verdictE is what one turn of the wait concluded. An event about a file this
// registration does not name is a turn that settled nothing.
type verdictE struct {
	changed bool
	err     error
	settled bool
}

func (c *change) look(ctx context.Context) verdictE {
	select {
	case <-ctx.Done():
		return verdictE{settled: true}
	case ev, open := <-c.fs.Events:
		if !open {
			return verdictE{err: watchError("this watch was closed"), settled: true}
		}

		return c.event(ctx, ev)
	case err, open := <-c.fs.Errors:
		if !open {
			return verdictE{err: watchError("this watch was closed"), settled: true}
		}

		return verdictE{err: watchError(err.Error()), settled: true}
	}
}

// event decides what one filesystem event means to this registration.
//
// The context is read a second time here, because a select whose cases are both
// ready picks between them at random and a cancelled watch must not report one
// more change afterwards.
func (c *change) event(ctx context.Context, ev fsnotify.Event) verdictE {
	if ev.Has(fsnotify.Chmod) || !c.files[filepath.Clean(ev.Name)] {
		return verdictE{}
	}

	if ctx.Err() != nil {
		return verdictE{settled: true}
	}

	return verdictE{changed: true, settled: true}
}
