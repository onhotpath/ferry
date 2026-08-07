package yaml_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/yaml"
)

// watched is what the watching tests load, and the two documents below are the
// before and the after.
type watched struct {
	Port int `ferry:"port"`
}

// The interval every watching test polls at, and how long a test waits for a
// call that should arrive or for one that should not.
//
// Neither is a race against the filesystem: a call the watch owes arrives after
// however many looks it takes and the test blocks until it does, and the quiet
// period is bounded by the interval rather than by the write, since the change
// is on disk before the wait starts.
const (
	tick    = time.Millisecond
	arrives = 10 * time.Second
	quiet   = 100 * time.Millisecond
)

// key is what the test puts in the context it hands to Watch, to assert the
// callback is given that context and not one this package made up.
type key struct{}

// TestWatchCallsBackWhenTheFileChanges is the whole of the option: an operator
// edits the file, the callback runs, and a load through the binding held since
// before the edit produces the new value (ADR-0020).
func TestWatchCallsBackWhenTheFileChanges(t *testing.T) {
	path := write(t, "port: 1\n")

	ctx := context.WithValue(t.Context(), key{}, "the caller's own context")
	called := make(chan context.Context, 1)

	b, err := ferry.Bind[watched](yaml.NewSource(path, yaml.Watch(ctx, tick, sendTo(called))))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	before, err := b.Load(ctx)
	if err != nil {
		t.Fatalf("the first load: %v", err)
	}

	edit(t, path, "# edited by hand\nport: 2\n")

	got := awaitCall(t, called)
	if v := got.Value(key{}); v != "the caller's own context" {
		t.Errorf("the callback was given a context carrying %v, want the one Watch was called with: the "+
			"caller's deadline and values are what reach the reload", v)
	}

	after, err := b.Load(ctx)
	if err != nil {
		t.Fatalf("the reload: %v", err)
	}

	if before.Port != 1 || after.Port != 2 {
		t.Errorf("the held value is %d and the reload is %d, want 1 and 2: a reload is a load, and the value "+
			"held across it does not change", before.Port, after.Port)
	}
}

// TestWatchStopsWithItsContext is the lifecycle, which is the caller's context
// and nothing else: there is no Stop, so cancelling has to be one (ADR-0020).
func TestWatchStopsWithItsContext(t *testing.T) {
	path := write(t, "port: 1\n")

	ctx, cancel := context.WithCancel(t.Context())
	called := make(chan context.Context, 1)

	yaml.NewSource(path, yaml.Watch(ctx, tick, sendTo(called)))

	// The first change proves the watch was running, which is what makes the
	// silence after the cancellation mean something.
	edit(t, path, "# the first edit\nport: 2\n")
	awaitCall(t, called)

	cancel()

	edit(t, path, "# the edit nobody is watching for\nport: 3\n")

	select {
	case <-called:
		t.Error("the watch called back after the context that owns it was cancelled, so the goroutine it runs " +
			"on outlives the only thing that can stop it")
	case <-time.After(quiet):
	}
}

// TestWatchWithNoIntervalLooksAnyway asserts the documented default: an interval
// of zero or less is a look every second rather than a watch that does not run.
//
// The call arriving is the whole assertion. A ticker takes no interval that is
// not positive, so a watch that passed the caller's zero on would take the
// process down on its own goroutine before the first look (ADR-0020).
func TestWatchWithNoIntervalLooksAnyway(t *testing.T) {
	path := write(t, "port: 1\n")
	called := make(chan context.Context, 1)

	yaml.NewSource(path, yaml.Watch(t.Context(), 0, sendTo(called)))

	edit(t, path, "# the edit a default interval has to catch\nport: 2\n")

	awaitCall(t, called)
}

// TestWatchWithNoCallbackWatchesNothing asserts the other end of the same
// guard: a watch with nothing to call is a source that simply does not watch.
//
// The test surviving the change is the assertion. Without the guard the look
// after this edit would call a nil function, and a panic on the watching
// goroutine is not recoverable from here - it is the process (ADR-0020).
func TestWatchWithNoCallbackWatchesNothing(t *testing.T) {
	path := write(t, "port: 1\n")

	src := yaml.NewSource(path, yaml.Watch(t.Context(), tick, nil))

	edit(t, path, "# the edit nothing is listening for\nport: 2\n")

	<-time.After(quiet)

	got, err := ferry.Load[watched](t.Context(), src)
	if err != nil {
		t.Fatalf("loading through a source watching with no callback: %v", err)
	}

	if got.Port != 2 {
		t.Errorf("the load holds %d, want 2: a source that watches nothing still loads what the file holds",
			got.Port)
	}
}

// TestWatchIsOptIn asserts the plain source is what it always was: no option, no
// goroutine, and the file untouched until a load asks for it (ADR-0020).
func TestWatchIsOptIn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planeName)

	src := yaml.NewSource(path)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stat of a plane no load has asked for: %v, want it still not to exist", err)
	}

	if _, err := ferry.Load[watched](t.Context(), src); err != nil {
		t.Fatalf("loading through a source over a file that is not there: %v", err)
	}
}

// sendTo is the callback both watching tests hand to Watch. It gives the first
// call's context to the channel and drops the rest: one call is the assertion,
// and the loop behind it keeps running.
func sendTo(called chan<- context.Context) func(context.Context) {
	return func(c context.Context) {
		select {
		case called <- c:
		default:
		}
	}
}

// awaitCall blocks until the watch calls back, and fails the test rather than
// hanging where it never does.
func awaitCall(t *testing.T, called <-chan context.Context) context.Context {
	t.Helper()

	select {
	case c := <-called:
		return c
	case <-time.After(arrives):
		t.Fatal("the watch never called back for a change already on disk")

		return nil
	}
}

// edit rewrites the plane, which is the operator's own change in every test
// here.
func edit(t *testing.T, path, doc string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("writing the plane: %v", err)
	}
}
