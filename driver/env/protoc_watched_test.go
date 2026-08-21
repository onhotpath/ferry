//go:build protoc

package env_test

import (
	"context"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/env"
)

// Variant C carried to the real fsnotify mechanism.

type protocConfig struct {
	Host string `ferry:"HOST,required"`
}

func writeEnv(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestVariantCOverFsnotify is the whole wiring in four lines: a handle, a
// source over it, a bind of the two, a stream.
func TestVariantCOverFsnotify(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	writeEnv(t, path, "HOST=db1\n")

	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())

	h := ferry.NewWatch(ctx)
	src := env.New(env.DotEnv(path), env.Environ(noEnviron), env.Watched(h))

	wb, err := ferry.BindWatched[protocConfig](src, h)
	if err != nil {
		t.Fatalf("bind watched: %v", err)
	}

	seq, errf := wb.Watch(ferry.Debounce(80 * time.Millisecond))

	values := make(chan protocConfig)
	done := make(chan struct{})

	go func() {
		defer close(done)

		for v := range seq {
			values <- v
		}
	}()

	first := recv(t, values, done)
	if first.Host != "db1" {
		t.Fatalf("the stream opened with %q, want db1", first.Host)
	}

	writeEnv(t, path, "HOST=db2\n")

	if second := recv(t, values, done); second.Host != "db2" {
		t.Fatalf("the reload produced %q, want db2", second.Host)
	}

	if first.Host != "db1" {
		t.Fatalf("the held value became %q, so a reload mutated it", first.Host)
	}

	cancel()
	endsOn(t, done, errf)

	if leaked(before) {
		t.Fatalf("goroutines went from %d to %d and stayed there, so the watch leaked one",
			before, runtime.NumGoroutine())
	}
}

// TestVariantCEnvSourceWithNothingToWatchIsRefusedAtBind: the driver declines
// through the port and the refusal lands at the bind, before any load.
func TestVariantCEnvSourceWithNothingToWatchIsRefusedAtBind(t *testing.T) {
	t.Parallel()

	h := ferry.NewWatch(t.Context())
	src := env.New(env.Environ(func() []string { return []string{"HOST=db1"} }), env.Watched(h))

	_, err := ferry.BindWatched[protocConfig](src, h)
	if !errors.Is(err, ferry.ErrNotWatchable) {
		t.Fatalf("binding an env source that watches no files reported %v, want a refusal at bind", err)
	}

	if !errors.Is(err, env.ErrWatch) {
		t.Fatalf("the refusal is %v, which does not carry the driver's own reason", err)
	}
}

// TestVariantCHandleNoDriverWasGivenIsRefusedAtBind: a caller who opened a
// handle and forgot the driver option is refused rather than left with a stream
// that never fires.
func TestVariantCHandleNoDriverWasGivenIsRefusedAtBind(t *testing.T) {
	t.Parallel()

	h := ferry.NewWatch(t.Context())
	src := env.New(env.Environ(func() []string { return []string{"HOST=db1"} }))

	if _, err := ferry.BindWatched[protocConfig](src, h); !errors.Is(err, ferry.ErrWatchNotWired) {
		t.Fatalf("binding a handle no driver was given reported %v, want a refusal at bind", err)
	}
}

// TestVariantCLosingTheWatchEndsTheStream: the directory holding the file is
// removed, the driver reports it, and the stream ends with the reason rather
// than going quiet.
func TestVariantCLosingTheWatchEndsTheStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	inner, path := nestedEnv(t)

	h := ferry.NewWatch(ctx)
	src := env.New(env.DotEnv(path), env.Environ(noEnviron), env.Watched(h))

	wb, err := ferry.BindWatched[protocConfig](src, h)
	if err != nil {
		t.Fatalf("bind watched: %v", err)
	}

	seq, errf := wb.Watch()
	values, done := drain(ctx, seq)

	recv(t, values, done)

	if err := os.RemoveAll(inner); err != nil {
		t.Fatalf("remove: %v", err)
	}

	endsLost(t, values, done, errf)
}

// nestedEnv stages a .env in a directory of its own, so the directory can be
// removed without taking the test's own temporary directory with it.
func nestedEnv(t *testing.T) (dir, path string) {
	t.Helper()

	dir = filepath.Join(t.TempDir(), "conf")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	path = filepath.Join(dir, ".env")
	writeEnv(t, path, "HOST=db1\n")

	return dir, path
}

// drain ranges a stream on a goroutine of its own.
func drain(ctx context.Context, seq iter.Seq[protocConfig]) (chan protocConfig, chan struct{}) {
	values := make(chan protocConfig)
	done := make(chan struct{})

	go func() {
		defer close(done)

		for v := range seq {
			select {
			case values <- v:
			case <-ctx.Done():
				return
			}
		}
	}()

	return values, done
}

// endsLost waits for the stream to end, swallowing whatever it reloads on the
// way: removing the directory is a change as well as a loss, so the stream may
// yield once more before it ends. What matters is that it ends and says why.
func endsLost(t *testing.T, values chan protocConfig, done chan struct{}, errf func() error) {
	t.Helper()

	deadline := time.After(5 * time.Second)

	for {
		select {
		case <-done:
			assertLost(t, errf())

			return
		case <-values:
		case <-deadline:
			t.Fatal("the watch was lost and the stream did not end")
		}
	}
}

func assertLost(t *testing.T, err error) {
	t.Helper()

	if !errors.Is(err, ferry.ErrWatchLost) && !errors.Is(err, ferry.ErrMissing) {
		t.Fatalf("the stream ended with %v, want a lost watch or the load that found nothing", err)
	}
}

// endsOn waits for the range to exit and asserts it ended on the cancellation.
func endsOn(t *testing.T, done chan struct{}, errf func() error) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelling the context did not end the stream")
	}

	if err := errf(); !errors.Is(err, context.Canceled) {
		t.Fatalf("the stream ended with %v, want the cancellation", err)
	}
}

func recv(t *testing.T, values chan protocConfig, done chan struct{}) protocConfig {
	t.Helper()

	select {
	case v := <-values:
		return v
	case <-done:
		t.Fatal("the stream ended before a value arrived")
	case <-time.After(3 * time.Second):
		t.Fatal("no value arrived")
	}

	return protocConfig{}
}

func noEnviron() []string { return nil }

// leaked waits for the goroutine count to come back to where it was, because a
// goroutine returning is not instantaneous and a fixed sleep is either flaky or
// slow.
func leaked(before int) bool {
	for range 100 {
		if runtime.NumGoroutine() <= before {
			return false
		}

		time.Sleep(20 * time.Millisecond)
	}

	return true
}
