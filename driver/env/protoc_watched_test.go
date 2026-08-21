//go:build protoc

package env_test

import (
	"context"
	"errors"
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

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelling the context did not end the stream")
	}

	if err := errf(); !errors.Is(err, context.Canceled) {
		t.Fatalf("the stream ended with %v, want the cancellation", err)
	}

	settle()

	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines went from %d to %d, so the watch leaked one", before, after)
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
	dir := t.TempDir()
	inner := filepath.Join(dir, "conf")

	if err := os.Mkdir(inner, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	path := filepath.Join(inner, ".env")
	writeEnv(t, path, "HOST=db1\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := ferry.NewWatch(ctx)
	src := env.New(env.DotEnv(path), env.Environ(noEnviron), env.Watched(h))

	wb, err := ferry.BindWatched[protocConfig](src, h)
	if err != nil {
		t.Fatalf("bind watched: %v", err)
	}

	seq, errf := wb.Watch()

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

	recv(t, values, done)

	if err := os.RemoveAll(inner); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// The removal is a change as well as a loss, so the stream may reload once
	// before it ends. What matters is that it ends and says why.
	deadline := time.After(5 * time.Second)

	for {
		select {
		case <-done:
			if err := errf(); !errors.Is(err, ferry.ErrWatchLost) && !errors.Is(err, ferry.ErrMissing) {
				t.Fatalf("the stream ended with %v, want a lost watch or the load that found nothing", err)
			}

			return
		case <-values:
		case <-deadline:
			t.Fatal("the watch was lost and the stream did not end")
		}
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

func settle() {
	for range 50 {
		runtime.Gosched()
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
}
