//go:build protoe

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

// Variant E carried to the real fsnotify mechanism.

type protoeConfig struct {
	Host string `ferry:"HOST,required"`
}

func writeEnv(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestVariantEOverFsnotify is the whole wiring in one expression: the file is
// named once, the conversion to a watchable source is on the value that already
// holds it, and there is one error check.
func TestVariantEOverFsnotify(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	writeEnv(t, path, "HOST=db1\n")

	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())

	wb, err := ferry.BindWatched[protoeConfig](env.New(env.DotEnv(path), env.Environ(noEnviron)).Watched())
	if err != nil {
		t.Fatalf("bind watched: %v", err)
	}

	seq, errf := wb.Watch(ctx)

	values := make(chan protoeConfig)
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

// TestVariantETwoDotEnvFilesUnderOneWatch is #361 against the real driver, in
// the shape a caller actually hits: two files, one source, one watch. The
// driver already layers them, so nothing is composed at all.
func TestVariantETwoDotEnvFilesUnderOneWatch(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.env")
	local := filepath.Join(dir, "local.env")

	writeEnv(t, base, "HOST=from-base\n")
	writeEnv(t, local, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := env.New(env.DotEnv(base, local), env.Environ(noEnviron)).Watched()

	wb, err := ferry.BindWatched[protoeConfig](src)
	if err != nil {
		t.Fatalf("bind watched: %v", err)
	}

	seq, errf := wb.Watch(ctx)

	values := make(chan protoeConfig)
	done := make(chan struct{})

	go func() {
		defer close(done)

		for v := range seq {
			values <- v
		}
	}()

	if first := recv(t, values, done); first.Host != "from-base" {
		t.Fatalf("the stream opened with %q, want from-base", first.Host)
	}

	writeEnv(t, local, "HOST=from-local\n")

	if second := recv(t, values, done); second.Host != "from-local" {
		t.Fatalf("the reload produced %q, want the higher layer's value", second.Host)
	}

	cancel()
	endsOn(t, done, errf)
}

// TestVariantEUnwatchableEnvSourceIsRefusedAtBind: a source naming no file
// refuses at the bind through its own Watching, before any load, and there is
// no Option anywhere for a caller to have forgotten.
func TestVariantEUnwatchableEnvSourceIsRefusedAtBind(t *testing.T) {
	t.Parallel()

	src := env.New(env.Environ(func() []string { return []string{"HOST=db1"} })).Watched()

	_, err := ferry.BindWatched[protoeConfig](src)
	if !errors.Is(err, ferry.ErrNotWatchable) {
		t.Fatalf("binding an env source that watches no files reported %v, want a refusal at bind", err)
	}

	if !errors.Is(err, env.ErrWatch) {
		t.Fatalf("the refusal is %v, which does not carry the driver's own reason", err)
	}
}

// TestVariantEAWatchedSourceStillLoads: the conversion changes nothing about
// loading, so a caller who stops watching changes one call and not two.
func TestVariantEAWatchedSourceStillLoads(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	writeEnv(t, path, "HOST=db1\n")

	src := env.New(env.DotEnv(path), env.Environ(noEnviron)).Watched()

	v, err := ferry.Load[protoeConfig](t.Context(), src)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if v.Host != "db1" {
		t.Fatalf("the load produced %q, want db1", v.Host)
	}

	// ferry.BindWatched[protoeConfig](env.New(...)) does not compile: a plain
	// *env.Source has no Watching method and there is no conversion.
}

// TestVariantEConvertingTwiceIsTwoWatches: a source is settled configuration
// and the conversion consumes nothing, so converting twice is two independent
// watchable sources rather than a mistake.
func TestVariantEConvertingTwiceIsTwoWatches(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	writeEnv(t, path, "HOST=db1\n")

	src := env.New(env.DotEnv(path), env.Environ(noEnviron))

	for _, w := range []ferry.WatchableSource{src.Watched(), src.Watched()} {
		if _, err := ferry.BindWatched[protoeConfig](w); err != nil {
			t.Fatalf("bind watched: %v", err)
		}
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

func recv(t *testing.T, values chan protoeConfig, done chan struct{}) protoeConfig {
	t.Helper()

	select {
	case v := <-values:
		return v
	case <-done:
		t.Fatal("the stream ended before a value arrived")
	case <-time.After(3 * time.Second):
		t.Fatal("no value arrived")
	}

	return protoeConfig{}
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
