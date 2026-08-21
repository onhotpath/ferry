package env

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/onhotpath/ferry"
)

// Watching is opt-in through one conversion, it runs over the real inotify
// mechanism, and what is asserted here is what belongs to this driver rather
// than to every watchable driver: which changes reach a reload, which do not,
// that a rename over the file is a change, that a save of this process's own is
// a change too, and that a source naming no file is refused at the bind.
//
// The seven properties every watchable driver owes its caller are asserted by
// ferrytest.Watchable in conformance_test.go, not repeated here.

// stream is one [ferry.WatchedBinding.Watch] carried down to a case: the values
// that arrived, and how it ended.
type stream struct {
	values chan host
	done   chan struct{}
	errf   func() error
	cancel context.CancelFunc
}

// watching binds a watched source over the named files and opens a stream over
// it, ranging on a goroutine of its own so a case can wait for a value instead
// of sleeping for one.
func watching(t *testing.T, paths ...string) *stream {
	t.Helper()

	wb, err := ferry.BindWatched[host](New(Environ(noEnviron), DotEnv(paths...)).Watched())
	if err != nil {
		t.Fatalf("bind watched: %+v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	seq, errf := wb.Watch(ctx)
	s := &stream{values: make(chan host), done: make(chan struct{}), errf: errf, cancel: cancel}

	go func() {
		defer close(s.done)

		for v := range seq {
			select {
			case s.values <- v:
			case <-ctx.Done():
				return
			}
		}
	}()

	return s
}

// next is the value the stream handed over, inside a window generous enough for
// an inotify event and the settle timer.
func (s *stream) next(t *testing.T) host {
	t.Helper()

	select {
	case v := <-s.values:
		return v
	case <-s.done:
		t.Fatalf("the stream ended before a value arrived: %+v", s.errf())
	case <-time.After(3 * time.Second):
		t.Fatal("no value arrived")
	}

	return host{}
}

// quiet reports whether the stream stayed silent for long enough that a change
// would have reached it.
func (s *stream) quiet(t *testing.T) bool {
	t.Helper()

	select {
	case <-s.values:
		return false
	case <-time.After(400 * time.Millisecond):
		return true
	}
}

// ended waits for the range to exit and answers with what ended it.
func (s *stream) ended(t *testing.T) error {
	t.Helper()

	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
		t.Fatal("the stream did not end")
	}

	return s.errf()
}

// opened is the stream's first value, which every case below needs before it can
// change anything.
func (s *stream) opened(t *testing.T, want string) {
	t.Helper()

	if got := s.next(t).Host; got != want {
		t.Fatalf("the stream opened with %q, want %q", got, want)
	}
}

// TestAWriteReloads is the ordinary case: somebody edits the file.
func TestAWriteReloads(t *testing.T) {
	t.Parallel()

	path := staged(t, "HOST=old\n")
	s := watching(t, path)
	s.opened(t, "old")

	write(t, path, "HOST=new\n")

	if got := s.next(t).Host; got != "new" {
		t.Errorf("the reload read %q, want the file's new contents", got)
	}
}

// TestARenameOverTheFileReloads is why the directory is watched rather than the
// file: an editor and this package's own sink both replace a file by renaming
// another over it, and a watch on the inode survives that attached to a file
// nobody reads any more.
func TestARenameOverTheFileReloads(t *testing.T) {
	t.Parallel()

	path := staged(t, "HOST=old\n")
	s := watching(t, path)
	s.opened(t, "old")

	tmp := path + ".incoming"
	write(t, tmp, "HOST=new\n")

	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("the rename: %v", err)
	}

	if got := s.next(t).Host; got != "new" {
		t.Errorf("the reload read %q, want the contents renamed over the watched file", got)
	}
}

// TestADumpThroughTheSinkReloads is the decision not to suppress a
// self-triggered reload, asserted so that it stays a decision.
func TestADumpThroughTheSinkReloads(t *testing.T) {
	t.Parallel()

	path := staged(t, "HOST=old\n")
	s := watching(t, path)
	s.opened(t, "old")

	if err := ferry.Dump(t.Context(), host{Host: "new"}, NewDotEnvSink(path)); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if got := s.next(t).Host; got != "new" {
		t.Errorf("this process's own save read back as %q, and the documentation says a dump is a change", got)
	}
}

// TestAnUnrelatedFileInTheDirectoryDoesNotReload is the filter, and the staged
// file below is the case it exists for: a save writes ".env.ferry-*" beside the
// plane, and a watch that fired on it would reload on its own temporary.
func TestAnUnrelatedFileInTheDirectoryDoesNotReload(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"unrelated.txt", ".env.ferry-123456"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := staged(t, "HOST=old\n")
			s := watching(t, path)
			s.opened(t, "old")

			write(t, filepath.Join(filepath.Dir(path), name), "anything\n")

			if !s.quiet(t) {
				t.Errorf("the stream reloaded for %s, which is not a file this watch names", name)
			}
		})
	}
}

// TestSeveralFilesInOneDirectoryOpenOneWatch is the deduplication, and what it
// buys is one inotify watch per directory rather than one per file: naming
// several layers in one directory is the ordinary case.
//
// Both files still reload, which is what says the deduplication is over the
// directories rather than over the files, and the value that arrives is the
// layering the driver already does.
func TestSeveralFilesInOneDirectoryOpenOneWatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	base, local := filepath.Join(dir, "base.env"), filepath.Join(dir, "local.env")

	write(t, base, "HOST=from-base\n")
	write(t, local, "")

	s := watching(t, base, local)
	s.opened(t, "from-base")

	write(t, local, "HOST=from-local\n")

	if got := s.next(t).Host; got != "from-local" {
		t.Errorf("the reload read %q, want the higher layer's value", got)
	}

	write(t, local, "")
	write(t, base, "HOST=base-again\n")

	if got := s.next(t).Host; got != "base-again" {
		t.Errorf("a write to the lower layer read %q, so that file is not watched", got)
	}
}

// TestAMissingDirectoryEndsTheStream is where the operating system's opinion
// lands: Watching does no I/O, so a directory that is not there is not a bind
// refusal but the first registration failing, on the stream, before any value.
func TestAMissingDirectoryEndsTheStream(t *testing.T) {
	t.Parallel()

	gone := filepath.Join(t.TempDir(), "no-such-directory", ".env")

	s := watching(t, gone)

	err := s.ended(t)
	if !errors.Is(err, ferry.ErrWatchLost) {
		t.Fatalf("the stream ended with %+v, want a lost watch", err)
	}

	answers(t, err, ferry.ErrPlane, ErrWatch)
}

// TestLosingTheDirectoryEndsTheStream is the ending the whole seam exists for,
// and both ways of losing a directory are here because they do not deliver the
// same events.
//
// Removing it unlinks the file first, so an event names the file and another
// names the directory. Renaming it away moves the directory whole, so the only
// event there is names the directory and nothing at all names the file. A wait
// that listened for the file alone would sit through the second one forever,
// which is a process holding stale configuration with nothing to tell it so.
func TestLosingTheDirectoryEndsTheStream(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		lose func(t *testing.T, dir string)
	}{
		{"removed", func(t *testing.T, dir string) {
			t.Helper()

			if err := os.RemoveAll(dir); err != nil {
				t.Fatalf("removing the directory: %v", err)
			}
		}},
		{"renamed away", func(t *testing.T, dir string) {
			t.Helper()

			if err := os.Rename(dir, dir+".gone"); err != nil {
				t.Fatalf("renaming the directory away: %v", err)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			path := staged(t, "HOST=old\n")
			s := watching(t, path)
			s.opened(t, "old")

			c.lose(t, filepath.Dir(path))

			err := s.ended(t)
			if !errors.Is(err, ferry.ErrWatchLost) {
				t.Fatalf("the stream ended with %+v, want a lost watch", err)
			}

			answers(t, err, ferry.ErrPlane, ErrWatch)
		})
	}
}

// TestASourceNamingNoFileIsRefusedAtBind is the instance-level half of
// watchability, and it is refused before any load: a watch that opened
// successfully and never fired is the failure this refusal exists to avoid.
func TestASourceNamingNoFileIsRefusedAtBind(t *testing.T) {
	t.Parallel()

	_, err := ferry.BindWatched[host](New(Environ(noEnviron)).Watched())
	if err == nil {
		t.Fatal("the bind succeeded, want a refusal: a watch nobody could open is a process that has " +
			"silently stopped reloading")
	}

	answers(t, err, ferry.ErrPlane, ErrWatch)
}

// TestAWatchedSourceStillLoads is what the conversion does not change: a caller
// who stops watching changes one call rather than two.
func TestAWatchedSourceStillLoads(t *testing.T) {
	t.Parallel()

	src := New(Environ(noEnviron), DotEnv(staged(t, "HOST=old\n"))).Watched()

	v, err := ferry.Load[host](t.Context(), src)
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if v.Host != "old" {
		t.Errorf("the load read %q, want the file's contents", v.Host)
	}

	// ferry.BindWatched[host](New(...)) does not compile: a plain *Source has
	// no Watching method, and the conversion is the whole of the opt-in.
}

// TestConvertingTwiceIsTwoWatchableSources is the conversion consuming nothing:
// a source is settled configuration, so naming it twice is two watches over one
// configuration rather than a mistake.
func TestConvertingTwiceIsTwoWatchableSources(t *testing.T) {
	t.Parallel()

	src := New(Environ(noEnviron), DotEnv(staged(t, "HOST=old\n")))

	for _, w := range []ferry.WatchableSource{src.Watched(), src.Watched()} {
		if _, err := ferry.BindWatched[host](w); err != nil {
			t.Fatalf("bind watched: %+v", err)
		}
	}
}

// TestASourceThatIsNotWatchedStartsNothing is the opt-in, and it is asserted
// because the cost of getting it wrong is a goroutine per source for every
// caller who never asked for one.
//
// Neither building the source nor converting it nor binding the conversion
// reaches the mechanism: the watch opens when a stream does.
func TestASourceThatIsNotWatchedStartsNothing(t *testing.T) {
	before := runtime.NumGoroutine()

	src := New(Environ(noEnviron), DotEnv(staged(t, "HOST=old\n"))).Watched()

	if _, err := ferry.BindWatched[host](src); err != nil {
		t.Fatalf("bind watched: %+v", err)
	}

	if !settled(before) {
		t.Errorf("goroutines went from %d to %d and stayed there, so binding a watch opened one",
			before, runtime.NumGoroutine())
	}
}

// TestACancelledStreamLeaksNoGoroutine is the other half of stopping, over the
// real mechanism: the range exits, the inotify watcher is closed, and nothing is
// left running.
func TestACancelledStreamLeaksNoGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()

	path := staged(t, "HOST=old\n")
	s := watching(t, path)
	s.opened(t, "old")

	write(t, path, "HOST=new\n")
	s.opened(t, "new")

	s.cancel()

	if err := s.ended(t); !errors.Is(err, context.Canceled) {
		t.Errorf("the stream ended with %+v, want the cancellation", err)
	}

	if !settled(before) {
		t.Errorf("goroutines went from %d to %d and stayed there, so a cancelled watch left one running",
			before, runtime.NumGoroutine())
	}
}

// settled waits for the goroutine count to come back to where it was, because a
// goroutine returning is not instantaneous and a fixed sleep is either flaky or
// slow.
func settled(before int) bool {
	for range 100 {
		if runtime.NumGoroutine() <= before {
			return true
		}

		time.Sleep(20 * time.Millisecond)
	}

	return false
}
