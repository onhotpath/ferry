package env

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/onhotpath/ferry"
)

// A watch is opt-in, it runs on a goroutine of its own, and everything about it
// that can be asserted is asserted here: which changes reach the callback, which
// do not, that a burst is one call, that cancelling stops it, and that stopping
// leaves nothing behind.

// calls is what a test's callback records, and it is a channel rather than a
// counter so a case waits for the call instead of sleeping for it.
type calls chan struct{}

// fired reports whether the callback ran inside a window generous enough for an
// inotify event and the settle timer.
func (c calls) fired(t *testing.T) bool {
	t.Helper()

	select {
	case <-c:
		return true
	case <-time.After(2 * time.Second):
		return false
	}
}

// quiet reports whether the callback stayed silent for long enough that an event
// would have reached it.
func (c calls) quiet(t *testing.T) bool {
	t.Helper()

	select {
	case <-c:
		return false
	case <-time.After(400 * time.Millisecond):
		return true
	}
}

// watching builds a source watching one file and answers with the file's path
// and the calls its callback made.
func watching(t *testing.T) (string, calls) {
	t.Helper()

	path := staged(t, "HOST=old\n")
	fired := make(calls, 8)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	src := New(Environ(noEnviron), DotEnv(path), WatchFiles(ctx, func(context.Context) {
		select {
		case fired <- struct{}{}:
		default:
		}
	}))

	// Bind is where a watch that could not be opened is reported, so a case that
	// got this far is watching for real.
	if _, err := ferry.Bind[host](src); err != nil {
		t.Fatalf("bind: %+v", err)
	}

	return path, fired
}

// TestAWriteFiresTheWatch is the ordinary case: somebody edits the file.
func TestAWriteFiresTheWatch(t *testing.T) {
	t.Parallel()

	path, fired := watching(t)

	write(t, path, "HOST=new\n")

	if !fired.fired(t) {
		t.Error("the callback did not run for a write to the watched file")
	}
}

// TestARenameOverTheFileFiresTheWatch is why the directory is watched rather than
// the file: an editor and this package's own sink both replace a file by renaming
// another over it, and a watch on the inode survives that attached to a file
// nobody reads any more.
func TestARenameOverTheFileFiresTheWatch(t *testing.T) {
	t.Parallel()

	path, fired := watching(t)

	tmp := path + ".incoming"
	write(t, tmp, "HOST=new\n")

	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("the rename: %v", err)
	}

	if !fired.fired(t) {
		t.Error("the callback did not run for a rename over the watched file")
	}
}

// TestADumpThroughTheSinkFiresTheWatch is the decision not to suppress a
// self-triggered reload, asserted so that it stays a decision.
func TestADumpThroughTheSinkFiresTheWatch(t *testing.T) {
	t.Parallel()

	path, fired := watching(t)

	if err := ferry.Dump(t.Context(), host{Host: "new"}, NewDotEnvSink(path)); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if !fired.fired(t) {
		t.Error("the callback did not run for this process's own save, and the documentation says it does")
	}
}

// TestAnUnrelatedFileInTheDirectoryDoesNotFireTheWatch is the filter, and the
// staged file below is the case it exists for: a save writes ".env.ferry-*"
// beside the plane, and a watch that fired on it would reload on its own
// temporary.
func TestAnUnrelatedFileInTheDirectoryDoesNotFireTheWatch(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"unrelated.txt", ".env.ferry-123456"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path, fired := watching(t)

			write(t, filepath.Join(filepath.Dir(path), name), "anything\n")

			if !fired.quiet(t) {
				t.Errorf("the callback ran for %s, which is not a file this watch names", name)
			}
		})
	}
}

// TestABurstOfEventsIsOneCall is the settle timer: one editor save produces
// several events, and a reload per event is several reloads of one change.
func TestABurstOfEventsIsOneCall(t *testing.T) {
	t.Parallel()

	path, fired := watching(t)

	for i := range 3 {
		write(t, path, "HOST=new"+string(rune('a'+i))+"\n")
	}

	if !fired.fired(t) {
		t.Fatal("the callback did not run at all")
	}

	if !fired.quiet(t) {
		t.Error("the callback ran more than once for one burst, so a save that writes several times reloads " +
			"several times")
	}
}

// TestSeveralFilesInOneDirectoryOpenOneWatch is the deduplication, and what it
// buys is one inotify watch per directory rather than one per file: naming five
// layers in one directory is the ordinary case.
//
// Both files still fire, which is what says the deduplication is over the
// directories rather than over the files.
func TestSeveralFilesInOneDirectoryOpenOneWatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	base, local := filepath.Join(dir, "base.env"), filepath.Join(dir, "local.env")

	write(t, base, "HOST=base\n")
	write(t, local, "HOST=local\n")

	fired := make(calls, 8)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	src := New(Environ(noEnviron), DotEnv(base, local), WatchFiles(ctx, func(context.Context) {
		select {
		case fired <- struct{}{}:
		default:
		}
	}))

	if _, err := ferry.Bind[host](src); err != nil {
		t.Fatalf("bind: %+v", err)
	}

	for _, path := range []string{base, local} {
		write(t, path, "HOST=changed\n")

		if !fired.fired(t) {
			t.Errorf("the callback did not run for a write to %s", filepath.Base(path))
		}
	}
}

// TestLosingTheWatchFiresOnceAndStops is the one ending that speaks.
//
// There is nowhere to report a watch that is gone, so the callback runs once and
// the goroutine returns: the caller's next load then reads the truth and reports
// it through a surface they already handle, rather than a process that has
// silently stopped reloading.
func TestLosingTheWatchFiresOnceAndStops(t *testing.T) {
	t.Parallel()

	path, fired := watching(t)

	if err := os.RemoveAll(filepath.Dir(path)); err != nil {
		t.Fatalf("removing the directory: %v", err)
	}

	if !fired.fired(t) {
		t.Error("the callback did not run when the watch was lost, so nothing tells the caller to look")
	}
}

// TestCancellingTheContextStopsTheWatch is the only way to stop one, so it has to
// mean stopped.
func TestCancellingTheContextStopsTheWatch(t *testing.T) {
	t.Parallel()

	path := staged(t, "HOST=old\n")
	fired := make(calls, 8)

	ctx, cancel := context.WithCancel(t.Context())

	src := New(Environ(noEnviron), DotEnv(path), WatchFiles(ctx, func(context.Context) {
		select {
		case fired <- struct{}{}:
		default:
		}
	}))

	if _, err := ferry.Bind[host](src); err != nil {
		t.Fatalf("bind: %+v", err)
	}

	cancel()

	// The goroutine returns on the cancellation, so give it a moment to before
	// the write it must not report.
	time.Sleep(100 * time.Millisecond)

	write(t, path, "HOST=new\n")

	if !fired.quiet(t) {
		t.Error("the callback ran after the context was cancelled, and cancelling is the only way to stop a watch")
	}
}

// TestAStoppedWatchLeaksNoGoroutine is the other half of stopping: the goroutine
// returns and the watcher it holds is closed.
func TestAStoppedWatchLeaksNoGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()

	path := staged(t, "HOST=old\n")
	ctx, cancel := context.WithCancel(t.Context())

	src := New(Environ(noEnviron), DotEnv(path), WatchFiles(ctx, func(context.Context) {}))
	if _, err := ferry.Bind[host](src); err != nil {
		t.Fatalf("bind: %+v", err)
	}

	cancel()

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

// TestAWatchThatCannotBeOpenedIsRefusedAtBind is the whole reason this is not a
// retry loop: a watch that succeeded silently and never fired is the failure this
// option exists to avoid.
func TestAWatchThatCannotBeOpenedIsRefusedAtBind(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		src  func(t *testing.T) *Source
	}{
		{
			"no files were named",
			func(t *testing.T) *Source {
				t.Helper()

				return New(Environ(noEnviron), WatchFiles(t.Context(), func(context.Context) {}))
			},
		},
		{
			"the directory is not there",
			func(t *testing.T) *Source {
				t.Helper()

				gone := filepath.Join(t.TempDir(), "no-such-directory", ".env")

				return New(Environ(noEnviron), DotEnv(gone), WatchFiles(t.Context(), func(context.Context) {}))
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			refusesTheWatch(t, c.src(t))
		})
	}
}

// refusesTheWatch is one unopenable watch, lifted out of its table.
func refusesTheWatch(t *testing.T, src *Source) {
	t.Helper()

	_, err := ferry.Bind[host](src)
	if err == nil {
		t.Fatal("the bind succeeded, want a refusal: a watch nobody could open is a process that has " +
			"silently stopped reloading")
	}

	answers(t, err, ferry.ErrPlane, ErrWatch)
}

// TestASourceWithNoWatchStartsNothing is the opt-in, and it is asserted because
// the cost of getting it wrong is a goroutine per source for every caller who
// never asked for one.
func TestASourceWithNoWatchStartsNothing(t *testing.T) {
	t.Parallel()

	src := New(Environ(noEnviron), DotEnv(filepath.Join(t.TempDir(), ".env")))

	if src.cfg.watch != nil {
		t.Error("a source built without env.WatchFiles is watching, and the option is the whole of the opt-in")
	}
}

// TestANilCallbackWatchesNothing is the same opt-in from the other side: there is
// nothing to call, so there is nothing to run.
func TestANilCallbackWatchesNothing(t *testing.T) {
	t.Parallel()

	src := New(Environ(noEnviron), DotEnv(filepath.Join(t.TempDir(), ".env")), WatchFiles(t.Context(), nil))

	if src.cfg.watch != nil {
		t.Error("a watch with no callback is watching, and there is nothing for it to call")
	}
}
