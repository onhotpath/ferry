package winreg_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/windows/winreg"
)

// The tests here drive a reader and a writer directly rather than through Load
// and Dump, and that is the exception rather than the habit.
//
// Two obligations this driver carries cannot be observed through the walk,
// because core gets there first or never goes there at all. Core checks the
// context between steps, so a cancellation arriving mid-walk is reported by core
// before the driver's own check runs; and core mints one plane key per address,
// so the guard in front of the staging list is one core's own refusal reaches
// first. Both are still obligations - the [winreg.Registry] contract says every
// call is cancellable, and the guard is what holds if this driver ever stages by
// a pair it computed itself - so they are asserted where they are observable.
//
// The address kinds are sealed and only the schema compiler mints one, so the
// addresses below come out of a compiled fixture. That is the same door core
// comes in through.

// TestEveryReadRefusesADoneContextBeforeTouchingTheRegistry is the read half of
// the cancellation obligation, and the assertion is that the registry was not
// reached at all.
func TestEveryReadRefusesADoneContextBeforeTouchingTheRegistry(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	store := newFake()
	set := setOf[textThenMap](t)
	r := openReader(t, source(store), set)

	if _, err := r.Get(ctx, leafIn(t, set, ferry.At("text"))); !errors.Is(err, context.Canceled) {
		t.Errorf("Get answered %v, want context.Canceled", err)
	}

	pr, ok := r.(ferry.Prober)
	if !ok {
		t.Fatal("the reader does not probe")
	}

	if _, err := pr.Probe(ctx, compositeIn(t, set, ferry.At("tags"))); !errors.Is(err, context.Canceled) {
		t.Errorf("Probe answered %v, want context.Canceled", err)
	}

	en, ok := r.(ferry.Enumerator)
	if !ok {
		t.Fatal("the reader does not enumerate")
	}

	if _, err := en.Children(ctx, compositeIn(t, set, ferry.At("tags"))); !errors.Is(err, context.Canceled) {
		t.Errorf("Children answered %v, want context.Canceled", err)
	}

	if n := store.calls(); n != 0 {
		t.Errorf("the registry was asked %d question(s) under a context that was already done", n)
	}
}

// TestEveryWriteRefusesADoneContextBeforeTouchingTheRegistry is the write half.
//
// The open is not among them: it is where the registry is first reached, and a
// context already done there is refused by the open itself, which is what
// [TestACancelledContextIsRefusedAtTheOpen] holds it to.
func TestEveryWriteRefusesADoneContextBeforeTouchingTheRegistry(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	store := newFake()
	set := setOf[textThenMap](t)
	w := openWriter(t, store, set)

	before := store.calls()

	if err := w.Set(ctx, leafIn(t, set, ferry.At("text")), ferry.String("x")); !errors.Is(err, context.Canceled) {
		t.Errorf("Set answered %v, want context.Canceled", err)
	}

	checkUnsetterCancelled(ctx, t, w, compositeIn(t, set, ferry.At("tags")))
	checkEnsurerCancelled(ctx, t, w, compositeIn(t, set, ferry.At("tags")))

	if c, ok := w.(ferry.Committer); ok {
		if err := c.Commit(ctx); !errors.Is(err, context.Canceled) {
			t.Errorf("Commit answered %v, want context.Canceled", err)
		}
	}

	if n := store.calls() - before; n != 0 {
		t.Errorf("the registry was written %d time(s) under a context that was already done", n)
	}
}

// checkUnsetterCancelled and checkEnsurerCancelled are the two optional halves of
// the write side, asked the same question.
func checkUnsetterCancelled(ctx context.Context, t *testing.T, w ferry.Writer, at ferry.CompositeAddr) {
	t.Helper()

	u, ok := w.(ferry.Unsetter)
	if !ok {
		t.Fatal("the writer cannot forget an address")
	}

	if err := u.Unset(ctx, at); !errors.Is(err, context.Canceled) {
		t.Errorf("Unset answered %v, want context.Canceled", err)
	}
}

func checkEnsurerCancelled(ctx context.Context, t *testing.T, w ferry.Writer, at ferry.CompositeAddr) {
	t.Helper()

	e, ok := w.(ferry.Ensurer)
	if !ok {
		t.Fatal("the writer cannot spell a container")
	}

	if err := e.Ensure(ctx, at, ferry.PresencePresent); !errors.Is(err, context.Canceled) {
		t.Errorf("Ensure answered %v, want context.Canceled", err)
	}
}

// TestOneOpenWritesOneValuePerRegistryName is the guard in front of the staging
// list, and driving the writer directly is the only way to reach it: core mints
// one plane key per address and refuses the second first.
//
// It is worth having for exactly that reason. It is the invariant that still
// holds if this driver ever stages by a pair it computed itself, and it turns the
// registry's silent second-write-wins into a refusal at the last possible moment.
func TestOneOpenWritesOneValuePerRegistryName(t *testing.T) {
	t.Parallel()

	set := setOf[oneText](t)
	w := openWriter(t, newFake(), set)
	at := leafIn(t, set, ferry.At("text"))

	if err := w.Set(t.Context(), at, ferry.String("first")); err != nil {
		t.Fatalf("the first Set: %v", err)
	}

	err := w.Set(t.Context(), at, ferry.String("second"))
	if !errors.Is(err, ferry.ErrPlane) {
		t.Fatalf("the second Set answered %v, want an error reaching ferry.ErrPlane", err)
	}

	if !strings.Contains(err.Error(), "one of the two writes would be lost") {
		t.Errorf("the refusal does not say what would be lost: %v", err)
	}
}

// TestEnsureRefusesAPresenceItHasNoAnswerFor is the arm core never reaches, and a
// live refusal rather than dead code.
//
// An absent container gets no call at all, so arriving here means core is asking
// something this driver has no answer for. A method that returned nil instead is
// one nothing would catch changing.
func TestEnsureRefusesAPresenceItHasNoAnswerFor(t *testing.T) {
	t.Parallel()

	set := setOf[tagsMap](t)

	e, ok := openWriter(t, newFake(), set).(ferry.Ensurer)
	if !ok {
		t.Fatal("the writer cannot spell a container")
	}

	err := e.Ensure(t.Context(), compositeIn(t, set, ferry.At("tags")), ferry.PresenceAbsent)
	if !errors.Is(err, ferry.ErrValue) {
		t.Fatalf("Ensure answered %v, want an error reaching ferry.ErrValue", err)
	}
}

// openReader and openWriter bind this driver to one compiled address set and open
// one instance of each half.
func openReader(t *testing.T, src ferry.Source, set *ferry.AddressSet) ferry.Reader {
	t.Helper()

	open, err := src.Bind(set)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	r, err := open(t.Context())
	if err != nil {
		t.Fatalf("the open: %v", err)
	}

	return r
}

func openWriter(t *testing.T, store winreg.Registry, set *ferry.AddressSet) ferry.Writer {
	t.Helper()

	open, err := sink(store).Bind(set)
	if err != nil {
		t.Fatalf("BindSink: %v", err)
	}

	w, err := open(t.Context())
	if err != nil {
		t.Fatalf("the open: %v", err)
	}

	return w
}

// leafIn and compositeIn find one address of one kind in a compiled set. A miss
// is a test naming an address its own fixture does not have.
func leafIn(t *testing.T, set *ferry.AddressSet, at ferry.Path) ferry.LeafAddr {
	t.Helper()

	for m := range set.Seq() {
		if a, ok := m.(ferry.LeafAddr); ok && a.Path() == at {
			return a
		}
	}

	t.Fatalf("the fixture names no leaf at %s", at)

	return ferry.LeafAddr{}
}

func compositeIn(t *testing.T, set *ferry.AddressSet, at ferry.Path) ferry.CompositeAddr {
	t.Helper()

	for m := range set.Seq() {
		if a, ok := m.(ferry.CompositeAddr); ok && a.Path() == at {
			return a
		}
	}

	t.Fatalf("the fixture names no composite at %s", at)

	return ferry.CompositeAddr{}
}
