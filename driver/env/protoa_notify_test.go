//go:build protoa

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

// Variant A carried to the real fsnotify mechanism. What this file proves is
// that the seam is portable to a driver that ships one, and that the nine-step
// wiring the shipped API needs is four lines here.

type protoaConfig struct {
	Host string `ferry:"HOST,required"`
}

func writeEnv(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// watchedEnv is the whole wiring: build the source, bind it, watch it.
func watchedEnv(ctx context.Context, t *testing.T, path string) *ferry.Watched[protoaConfig] {
	t.Helper()

	b, err := ferry.Bind[protoaConfig](env.New(env.DotEnv(path), env.Environ(noEnviron)))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	w, err := b.Watch(ctx, ferry.Debounce(80*time.Millisecond))
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	return w
}

// TestVariantAOverFsnotify runs scenarios 1, 2, 3 and 6 against the real driver:
// the stream opens with a load of what the file holds now, a save is one reload,
// and cancelling the one context ends everything with nothing left behind.
func TestVariantAOverFsnotify(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	writeEnv(t, path, "HOST=db1\n")

	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())

	w := watchedEnv(ctx, t, path)

	values := make(chan protoaConfig)
	done := make(chan struct{})

	go func() {
		defer close(done)

		for v := range w.Values() {
			values <- v
		}
	}()

	first := recv(t, values, done)
	if first.Host != "db1" {
		t.Fatalf("the stream opened with %q, want db1: no pre-load and no announced change", first.Host)
	}

	writeEnv(t, path, "HOST=db2\n")

	second := recv(t, values, done)
	if second.Host != "db2" {
		t.Fatalf("the reload produced %q, want db2", second.Host)
	}

	if first.Host != "db1" {
		t.Fatalf("the held value became %q, so a reload mutated it", first.Host)
	}

	cancel()
	endsOn(t, done, w.Err)

	if leaked(before) {
		t.Fatalf("goroutines went from %d to %d and stayed there, so the watch leaked one",
			before, runtime.NumGoroutine())
	}
}

// TestVariantAUnwatchableEnvSourceIsRefusedAtWatch is the ladder cost stated as
// a test: a binding built without WithWatch finds out at the Watch call, which
// is after Bind and is the position this variant exists to be judged on.
func TestVariantAUnwatchableEnvSourceIsRefusedAtWatch(t *testing.T) {
	t.Parallel()

	b, err := ferry.Bind[protoaConfig](env.New(env.Environ(func() []string { return []string{"HOST=db1"} })))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	_, err = b.Watch(t.Context())
	if !errors.Is(err, ferry.ErrNotWatchable) {
		t.Fatalf("watching a source that watches no files reported %v, want a refusal", err)
	}

	if !errors.Is(err, env.ErrWatch) {
		t.Fatalf("the refusal is %v, which does not carry the driver's own reason", err)
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

func recv(t *testing.T, values chan protoaConfig, done chan struct{}) protoaConfig {
	t.Helper()

	select {
	case v := <-values:
		return v
	case <-done:
		t.Fatal("the stream ended before a value arrived")
	case <-time.After(3 * time.Second):
		t.Fatal("no value arrived")
	}

	return protoaConfig{}
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

// The salvaged shape over the real driver: a source naming no file is refused at
// Bind, not at the stream, once WithWatch says the binding will be watched.
func TestVariantAWithWatchRefusesTheEnvSourceAtBind(t *testing.T) {
	t.Parallel()

	src := env.New(env.Environ(func() []string { return []string{"HOST=db1"} }))

	_, err := ferry.Bind[protoaConfig](src, ferry.WithWatch())
	if !errors.Is(err, ferry.ErrNotWatchable) {
		t.Fatalf("binding an env source that watches no files reported %v, want a refusal at Bind", err)
	}

	if !errors.Is(err, env.ErrWatch) {
		t.Fatalf("the refusal is %v, which does not carry the driver's own reason", err)
	}
}
