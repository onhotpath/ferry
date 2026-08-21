//go:build protob

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

// Variant B carried to the real fsnotify mechanism.

type protobConfig struct {
	Host string `ferry:"HOST,required"`
}

func writeEnv(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestVariantBOverFsnotify is the whole wiring in one function: one
// constructor, one bind, one stream, one context.
func TestVariantBOverFsnotify(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	writeEnv(t, path, "HOST=db1\n")

	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())

	wb, err := ferry.BindWatched[protobConfig](env.NewWatched(env.DotEnv(path), env.Environ(noEnviron)))
	if err != nil {
		t.Fatalf("bind watched: %v", err)
	}

	seq, errf := wb.Watch(ctx, ferry.Debounce(80*time.Millisecond))

	values := make(chan protobConfig)
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

// TestVariantBEnvSourceWithNothingToWatchIsRefusedAtBind is the refusal a type
// cannot make: env.NewWatched is a watchable source by type whatever its
// options say, so a source naming no file is refused by the value it carries,
// at BindWatched, before any load.
func TestVariantBEnvSourceWithNothingToWatchIsRefusedAtBind(t *testing.T) {
	t.Parallel()

	src := env.NewWatched(env.Environ(func() []string { return []string{"HOST=db1"} }))

	_, err := ferry.BindWatched[protobConfig](src)
	if !errors.Is(err, ferry.ErrNotWatchable) {
		t.Fatalf("binding an env source that watches no files reported %v, want a refusal at bind", err)
	}

	if !errors.Is(err, env.ErrWatch) {
		t.Fatalf("the refusal is %v, which does not carry the driver's own reason", err)
	}
}

// TestVariantBPlainSourceStillLoads is the other half of the cost: env.New is
// unchanged and cannot be watched, and the compiler is what says so.
func TestVariantBPlainSourceStillLoads(t *testing.T) {
	t.Parallel()

	src := env.New(env.Environ(func() []string { return []string{"HOST=db1"} }))

	v, err := ferry.Load[protobConfig](t.Context(), src)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if v.Host != "db1" {
		t.Fatalf("the load produced %q, want db1", v.Host)
	}

	// ferry.BindWatched[protobConfig](src) does not compile: *env.Source is not
	// a ferry.WatchableSource, and there is no conversion.
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

func recv(t *testing.T, values chan protobConfig, done chan struct{}) protobConfig {
	t.Helper()

	select {
	case v := <-values:
		return v
	case <-done:
		t.Fatal("the stream ended before a value arrived")
	case <-time.After(3 * time.Second):
		t.Fatal("no value arrived")
	}

	return protobConfig{}
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
