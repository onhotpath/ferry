//go:build protob

package env

import (
	"context"
	"path/filepath"

	"github.com/fsnotify/fsnotify"

	"github.com/onhotpath/ferry"
)

// This file is variant B of the typed watch prototype ported to the real
// fsnotify mechanism, and it is where the variant's driver-side cost is.
//
// [New] is untouched and keeps returning a [Source]. Watching needs a second
// constructor, [NewWatched], because the whole point is that a watchable source
// has a different type. Both take the same [Option] list, which is the
// duplication: two doors onto one configuration, and a driver that grows a
// third capability grows a fourth door.

// NewWatched builds a source over the process environment that can be watched.
//
//	wb, err := ferry.BindWatched[Config](env.NewWatched(env.DotEnv(".env")))
//
// It takes the same options [New] does and configures the same source. What it
// adds is the proof that this source can be watched, which is what
// [ferry.BindWatched] requires and what a plain [New] cannot supply.
//
// It refuses, inertly, a source with nothing to watch: no [DotEnv] named a
// file. The refusal is carried in the value and surfaces at
// [ferry.BindWatched], because a constructor with one result has nowhere else
// to put it, and it lands before any load.
//
// It touches nothing and starts nothing. The watch opens when a stream does,
// under that stream's own context, so there is no watch running before there is
// a binding to reload through.
func NewWatched(opts ...Option) ferry.WatchableSource {
	s := New(opts...)

	if len(s.cfg.dotenv) == 0 {
		return ferry.Unwatchable(watchError("this source watches no files: name them with env.DotEnv"))
	}

	return ferry.Watchable(s, watchedSource{cfg: s.cfg})
}

// watchedSource is the mechanism half, kept in a type of its own so that the
// plain [Source] does not carry a Notify method it only sometimes means.
type watchedSource struct {
	cfg config
}

// Notify registers for the next change to a file [DotEnv] named.
//
// The registration is live when Notify returns, so a save between this call and
// the wait that follows it is reported by that wait rather than missed.
//
// The directory holding each file is watched rather than the file itself,
// because an editor and this package's own sink both replace a file by renaming
// another over it. Everything else in those directories is ignored, including
// the ".ferry-*" files a save stages.
func (w watchedSource) Notify(context.Context) (ferry.Change, error) {
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, watchError("no watch could be opened: " + err.Error())
	}

	files, err := watchDirs(fs, w.cfg.dotenv)
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

// change is one registration over its own inotify watcher. The stream arms the
// next registration before it releases the current one, so there are at most
// two open at once.
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

// verdictB is what one turn of the wait concluded. An event about a file this
// registration does not name is a turn that settled nothing.
type verdictB struct {
	changed bool
	err     error
	settled bool
}

func (c *change) look(ctx context.Context) verdictB {
	select {
	case <-ctx.Done():
		return verdictB{settled: true}
	case ev, open := <-c.fs.Events:
		if !open {
			return verdictB{err: watchError("this watch was closed"), settled: true}
		}

		return c.event(ctx, ev)
	case err, open := <-c.fs.Errors:
		if !open {
			return verdictB{err: watchError("this watch was closed"), settled: true}
		}

		return verdictB{err: watchError(err.Error()), settled: true}
	}
}

// event decides what one filesystem event means to this registration.
//
// The context is read a second time here, because a select whose cases are both
// ready picks between them at random and a cancelled watch must not report one
// more change afterwards.
func (c *change) event(ctx context.Context, ev fsnotify.Event) verdictB {
	if ev.Has(fsnotify.Chmod) || !c.files[filepath.Clean(ev.Name)] {
		return verdictB{}
	}

	if ctx.Err() != nil {
		return verdictB{settled: true}
	}

	return verdictB{changed: true, settled: true}
}
