//go:build protoe

package yaml_test

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
	"github.com/onhotpath/ferry/driver/yaml"
)

// The watching tests, ported from the callback option to the typed seam.
//
// What they assert is what the option's tests asserted, minus the two things
// that stopped existing: there is no callback to be handed a context, and there
// is no interval to default.

type watchedE struct {
	Port int `ferry:"port"`
}

// arrives is how long a test waits for a reload it is owed, and quiet is how
// long it waits for one it is not. Neither races the filesystem: the change is
// on disk before the wait starts.
const (
	arrivesE = 10 * time.Second
	quietE   = 200 * time.Millisecond
)

// stream ranges a watch on a goroutine of its own and hands the values over one
// at a time.
func stream(seq iter.Seq[watchedE]) (chan watchedE, chan struct{}) {
	values := make(chan watchedE)
	done := make(chan struct{})

	go func() {
		defer close(done)

		for v := range seq {
			values <- v
		}
	}()

	return values, done
}

func nextValue(t *testing.T, values chan watchedE, done chan struct{}, errf func() error) watchedE {
	t.Helper()

	select {
	case v := <-values:
		return v
	case <-done:
		t.Fatalf("the stream ended before a value arrived: %v", errf())
	case <-time.After(arrivesE):
		t.Fatal("no value arrived and the stream did not end")
	}

	return watchedE{}
}

// TestWatchedReloadsWhenTheFileChanges is the whole of the conversion: an
// operator edits the file and the next value off the stream is the new one,
// while the value held from before the edit is unchanged.
func TestWatchedReloadsWhenTheFileChanges(t *testing.T) {
	path := write(t, "port: 1\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wb, err := ferry.BindWatched[watchedE](yaml.NewSource(path).Watched())
	if err != nil {
		t.Fatalf("BindWatched: %v", err)
	}

	seq, errf := wb.Watch(ctx)
	values, done := stream(seq)

	before := nextValue(t, values, done, errf)
	if before.Port != 1 {
		t.Fatalf("the stream opened with %d, want 1", before.Port)
	}

	edit(t, path, "# edited by hand\nport: 2\n")

	after := nextValue(t, values, done, errf)
	if after.Port != 2 {
		t.Errorf("the reload produced %d, want 2", after.Port)
	}

	if before.Port != 1 {
		t.Errorf("the held value became %d, so a reload mutated it: a reload is a load, and the value held "+
			"across it does not change", before.Port)
	}
}

// TestWatchedStopsWithItsContext is the lifecycle, which is the context handed
// to Watch and nothing else: there is no Stop, so cancelling has to be one.
func TestWatchedStopsWithItsContext(t *testing.T) {
	path := write(t, "port: 1\n")

	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())

	wb, err := ferry.BindWatched[watchedE](yaml.NewSource(path).Watched())
	if err != nil {
		t.Fatalf("BindWatched: %v", err)
	}

	seq, errf := wb.Watch(ctx)
	values, done := stream(seq)

	nextValue(t, values, done, errf)

	// The first change proves the watch was running, which is what makes the
	// silence after the cancellation mean something.
	edit(t, path, "# the first edit\nport: 2\n")
	nextValue(t, values, done, errf)

	cancel()

	select {
	case <-done:
	case <-time.After(arrivesE):
		t.Fatal("cancelling the context did not end the stream")
	}

	if err := errf(); !errors.Is(err, context.Canceled) {
		t.Errorf("the stream ended with %v, want the cancellation", err)
	}

	if leakedE(before) {
		t.Errorf("goroutines went from %d to %d and stayed there, so the watch left one running",
			before, runtime.NumGoroutine())
	}
}

// TestWatchedIsOptIn asserts the plain source is what it always was: no
// conversion, no goroutine, and the file untouched until a load asks for it.
func TestWatchedIsOptIn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plane.yaml")

	src := yaml.NewSource(path)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stat of a plane no load has asked for: %v, want it still not to exist", err)
	}

	if _, err := ferry.Load[watchedE](t.Context(), src); err != nil {
		t.Fatalf("loading through a source over a file that is not there: %v", err)
	}

	// ferry.BindWatched[watchedE](src) does not compile: a yaml.Source has no
	// Watching method and there is no conversion.
}

// TestWatchedFileThatDoesNotExistYetIsWatched is the bootstrap case: the
// directory is there and the file is not, and the watch fires when it appears.
func TestWatchedFileThatDoesNotExistYetIsWatched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plane.yaml")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wb, err := ferry.BindWatched[watchedE](yaml.NewSource(path).Watched())
	if err != nil {
		t.Fatalf("BindWatched: %v", err)
	}

	seq, errf := wb.Watch(ctx)
	values, done := stream(seq)

	if first := nextValue(t, values, done, errf); first.Port != 0 {
		t.Fatalf("the stream opened with %d, want the empty plane's zero", first.Port)
	}

	edit(t, path, "port: 7\n")

	if after := nextValue(t, values, done, errf); after.Port != 7 {
		t.Errorf("the reload produced %d, want 7: a file that appears is a change", after.Port)
	}
}

// TestWatchedIgnoresTheSinkStagingFiles asserts the exact-name filter: a save
// through this package's own sink stages a ".ferry-*" file beside the plane,
// and that write is not a change to the plane.
func TestWatchedIgnoresTheSinkStagingFiles(t *testing.T) {
	path := write(t, "port: 1\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wb, err := ferry.BindWatched[watchedE](yaml.NewSource(path).Watched())
	if err != nil {
		t.Fatalf("BindWatched: %v", err)
	}

	seq, errf := wb.Watch(ctx)
	values, done := stream(seq)

	nextValue(t, values, done, errf)

	staged := filepath.Join(filepath.Dir(path), ".ferry-staging")
	if err := os.WriteFile(staged, []byte("port: 99\n"), 0o600); err != nil {
		t.Fatalf("staging a file beside the plane: %v", err)
	}

	select {
	case v := <-values:
		t.Errorf("a write to %s produced a reload holding %d, so the name filter is not exact",
			filepath.Base(staged), v.Port)
	case <-done:
		t.Errorf("a write beside the plane ended the stream: %v", errf())
	case <-time.After(quietE):
	}
}

// TestWatchedDirectoryThatIsNotThereIsRefused is the refusal this driver could
// not make at all while its watch was a poll: a path whose directory does not
// exist is a watch that would never fire, and it is refused before any value
// reaches the caller.
func TestWatchedDirectoryThatIsNotThereIsRefused(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "no-such-dir", "plane.yaml")

	wb, err := ferry.BindWatched[watchedE](yaml.NewSource(path).Watched())
	if err != nil {
		t.Fatalf("BindWatched: %v", err)
	}

	seq, errf := wb.Watch(t.Context())
	for range seq {
		t.Fatal("a watch over a directory that is not there yielded a value")
	}

	err = errf()
	if !errors.Is(err, ferry.ErrWatchLost) {
		t.Fatalf("the stream ended with %v, want a watch that could not be opened", err)
	}

	if !errors.Is(err, yaml.ErrWatch) {
		t.Errorf("the refusal is %v, which does not carry this driver's own reason", err)
	}
}

// TestWatchedWithNoPathIsRefusedAtBind is the instance refusal Watching makes:
// a source over no path at all cannot be watched, and it is refused at the bind.
func TestWatchedWithNoPathIsRefusedAtBind(t *testing.T) {
	t.Parallel()

	_, err := ferry.BindWatched[watchedE](yaml.NewSource("").Watched())
	if !errors.Is(err, ferry.ErrNotWatchable) {
		t.Fatalf("binding a source over no path reported %v, want a refusal at bind", err)
	}

	if !errors.Is(err, yaml.ErrWatch) {
		t.Errorf("the refusal is %v, which does not carry this driver's own reason", err)
	}
}

// TestAWatchedSourceStillLoads: the conversion changes nothing about loading.
func TestAWatchedSourceStillLoads(t *testing.T) {
	t.Parallel()

	path := write(t, "port: 3\n")

	got, err := ferry.Load[watchedE](t.Context(), yaml.NewSource(path).Watched())
	if err != nil {
		t.Fatalf("loading through a watched source: %v", err)
	}

	if got.Port != 3 {
		t.Errorf("the load holds %d, want 3", got.Port)
	}
}

// edit rewrites the plane in place, which is what an operator's editor does.
func edit(t *testing.T, path, doc string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("editing the plane: %v", err)
	}
}

// leakedE waits for the goroutine count to come back to where it was, because a
// goroutine returning is not instantaneous and a fixed sleep is either flaky or
// slow.
func leakedE(before int) bool {
	for range 100 {
		if runtime.NumGoroutine() <= before {
			return false
		}

		time.Sleep(20 * time.Millisecond)
	}

	return true
}
