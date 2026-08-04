package kv_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/kv"
)

// TestReadOnlyRefusesInsideTheOpen is ADR-0004's placement clause, which is the
// whole of what a read-only refusal is about here: writability is a fact about
// the plane at this moment, so the question can only be asked where there is
// I/O to ask it with.
//
// Not at Bind, which does no I/O and so cannot know, and not at the first
// write, which has already half-written the plane. The refusal wraps
// [ferry.ErrReadOnly] so that the placement is checkable by anyone, and the
// client's own error stays reachable underneath it.
func TestReadOnlyRefusesInsideTheOpen(t *testing.T) {
	t.Parallel()

	t.Run("Bind succeeds and the open refuses", readOnlyRefusesAtTheOpen)
	t.Run("no write is reached", readOnlyReachesNoSet)
}

// readOnlyRefusesAtTheOpen is the placement itself: Bind takes the address set
// against a plane it may not write, and the open is where that is found out.
func readOnlyRefusesAtTheOpen(t *testing.T) {
	t.Parallel()

	store := newGuarded("")

	open, err := mustSink(t, store).Bind(ferry.NewAddressSet(ferry.At("host")))
	if err != nil {
		t.Fatalf("Bind refused a legal address set against a plane it has not reached: %v", err)
	}

	w, err := open(t.Context())
	if err == nil {
		t.Fatalf("the open handed back %T for a client with no write access", w)
	}

	for _, want := range []error{ferry.ErrReadOnly, errFakeDenied} {
		if !errors.Is(err, want) {
			t.Errorf("the refusal %v does not carry %v", err, want)
		}
	}
}

// readOnlyReachesNoSet is the other half of the placement, observed from
// outside: the client is asked about the prefix once and about no address at
// all, and an address is only ever asked about from inside a Set.
func readOnlyReachesNoSet(t *testing.T) {
	t.Parallel()

	store := newGuarded("")

	err := ferry.Dump(t.Context(), three{Host: "h", Port: "8080", Zone: "eu"}, mustSink(t, store))
	if !errors.Is(err, ferry.ErrReadOnly) || !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("the dump failed with %v, which is not a read-only plane refusal", err)
	}

	if asked := store.asked(); !slices.Equal(asked, []string{prefix}) {
		t.Errorf("the client was asked about %v, want only the prefix: a refusal at the open is a refusal "+
			"before any address is written", asked)
	}

	if got := store.putCount(); got != 0 {
		t.Errorf("a read-only refusal still wrote %d keys, want none", got)
	}
}

// TestDeniedPathsAggregate is the case that decides the write half aggregates:
// a token with write access to some paths and not others reports every address
// it refused, in one run.
//
// Under fail-fast an operator fixes one ACL rule, re-runs, and is told about the
// next one. Core's own encode phase names this case for the same reason, so a
// driver that returned early would take away a property the rest of ferry has.
func TestDeniedPathsAggregate(t *testing.T) {
	t.Parallel()

	const want = 2

	store := newGuarded("app/port", "app/zone")

	err := ferry.Dump(t.Context(), three{Host: "h", Port: "8080", Zone: "eu"}, mustSink(t, store))

	got := ferry.Elements(err)
	if len(got) != want {
		t.Fatalf("a token denied on two paths reported %d errors, want %d: %v", len(got), want, err)
	}

	for i, addr := range []ferry.Path{ferry.At("port"), ferry.At("zone")} {
		assertDenied(t, got[i], addr)
	}

	if n := store.putCount(); n != 0 {
		t.Errorf("a dump that failed wrote %d keys, want none: a staging sink commits only on success", n)
	}
}

// assertDenied holds one element of the report to naming its own address and
// carrying the client's own error, which is what makes an aggregate readable
// rather than merely long.
func assertDenied(t *testing.T, elem error, addr ferry.Path) {
	t.Helper()

	e, ok := errors.AsType[*ferry.Error](elem)
	if !ok {
		t.Fatalf("the element for %s is %T rather than one of ferry's own errors", addr, elem)
	}

	if e.Address() != addr {
		t.Errorf("an element names %s, want %s", e.Address(), addr)
	}

	if !errors.Is(e, errFakeDenied) {
		t.Errorf("the element for %s does not carry the client's own error: %v", addr, e)
	}
}

// TestCommitRunsOnlyOnSuccess is the lifecycle this driver owns, asserted
// through both paths, because the half that matters is the one that must not
// happen.
func TestCommitRunsOnlyOnSuccess(t *testing.T) {
	t.Parallel()

	t.Run("a walk that succeeded", commitsAfterASuccessfulWalk)
	t.Run("a walk that failed", commitsNothingAfterAFailedWalk)
	t.Run("nothing is written before Commit", writesNothingBeforeCommit)
}

func commitsAfterASuccessfulWalk(t *testing.T) {
	t.Parallel()

	const want = 3

	store := newFake()

	if err := ferry.Dump(t.Context(), three{Host: "h", Port: "8080", Zone: "eu"}, mustSink(t, store)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got := store.putCount(); got != want {
		t.Errorf("a dump that succeeded wrote %d keys, want %d", got, want)
	}
}

func commitsNothingAfterAFailedWalk(t *testing.T) {
	t.Parallel()

	store := newGuarded("app/zone")

	if err := ferry.Dump(t.Context(), three{Host: "h", Port: "8080", Zone: "eu"}, mustSink(t, store)); err == nil {
		t.Fatal("a dump whose write was denied reported success")
	}

	if got := store.putCount(); got != 0 {
		t.Errorf("a dump that failed wrote %d keys, want none: the store is byte-identical after a failed "+
			"walk, and closed-without-Commit is the abort signal", got)
	}
}

// writesNothingBeforeCommit is what makes the two halves above meaningful: a
// Set reaches the store at Commit and at no earlier moment, so "the walk
// failed" and "the store is untouched" are one fact.
func writesNothingBeforeCommit(t *testing.T) {
	t.Parallel()

	addr := ferry.At("host")
	store := newFake()
	w := openWriter(t, mustSink(t, store), addr)

	if err := w.Set(t.Context(), addr, ferry.String("h")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if got := store.putCount(); got != 0 {
		t.Fatalf("a staged write reached the store %d times before Commit, want none", got)
	}

	if err := committer(t, w).Commit(t.Context()); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if got := store.putCount(); got != 1 {
		t.Errorf("Commit wrote %d keys, want 1", got)
	}
}

// committer asserts that an open writer stages, which this driver's does.
func committer(t *testing.T, w ferry.Writer) ferry.Committer {
	t.Helper()

	c, ok := w.(ferry.Committer)
	if !ok {
		t.Fatalf("%T does not commit, and it stages every write", w)
	}

	return c
}

// TestWriterCommitsWithNothingToRelease pins the axis ADR-0004 admits this
// driver for, negative half included.
//
// A Close here would be `return nil`, and in the source that is
// indistinguishable from a driver that should have rolled back and did not. The
// two lifecycle interfaces are optional and discovered by assertion precisely so
// that a driver with nothing to release implements nothing.
func TestWriterCommitsWithNothingToRelease(t *testing.T) {
	t.Parallel()

	w := openWriter(t, mustSink(t, newFake()), ferry.At("host"))

	if _, ok := w.(ferry.Committer); !ok {
		t.Errorf("%T does not implement ferry.Committer, and it stages every write until the walk succeeds", w)
	}

	if _, ok := w.(ferry.Releaser); ok {
		t.Errorf("%T implements ferry.Releaser, and it holds no resource: the client is the sink's and outlives "+
			"every open of it", w)
	}
}

// nils is the three shapes that write a Null at a container address: a nil
// pointer, a nil composite and an empty one (ADR-0005).
type nils struct {
	Section *section          `ferry:"section"`
	Tags    []string          `ferry:"tags"`
	Labels  map[string]string `ferry:"labels"`
}

// TestNullIsRefusedRatherThanMangled is what makes this a plane with no null
// rather than a plane that quietly has one.
//
// The refusal is per address and the run reports all three, which is the same
// aggregation the denied-path case turns on. The class is ErrValue: nothing is
// wrong with the store, the value has no representation here, and retrying it
// is pointless in the way an ErrValue promises.
func TestNullIsRefusedRatherThanMangled(t *testing.T) {
	t.Parallel()

	const want = 3

	for _, tc := range []struct {
		name string
		v    nils
	}{
		{name: "nil", v: nils{}},
		{name: "empty", v: nils{Tags: []string{}, Labels: map[string]string{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertRefused(t, tc.v, want)
		})
	}
}

// assertRefused dumps a value whose container addresses hold a Null and holds
// the refusal to being one error per address, of the class the driver declared,
// with the store untouched.
func assertRefused(t *testing.T, v nils, want int) {
	t.Helper()

	store := newFake()

	got := ferry.Elements(ferry.Dump(t.Context(), v, mustSink(t, store)))
	if len(got) != want {
		t.Fatalf("%d container addresses holding a Null produced %d errors, want %d", want, len(got), want)
	}

	for i, e := range got {
		if !errors.Is(e, ferry.ErrValue) {
			t.Errorf("element %d is %v, which does not declare ErrValue", i, e)
		}
	}

	if n := store.putCount(); n != 0 {
		t.Errorf("a refused dump wrote %d keys, want none", n)
	}
}

// TestASecondWriteAtOneKeyIsRefused is ADR-0001's rule that nothing is ignored
// silently, at the one place this driver could ignore something: a store holds
// one value per key, so a second write at one address would replace the first
// and report success for a dump that lost a field.
func TestASecondWriteAtOneKeyIsRefused(t *testing.T) {
	t.Parallel()

	addr := ferry.At("host")
	w := openWriter(t, mustSink(t, newFake()), addr)

	if err := w.Set(t.Context(), addr, ferry.String("first")); err != nil {
		t.Fatalf("the first Set: %v", err)
	}

	err := w.Set(t.Context(), addr, ferry.String("second"))
	if err == nil {
		t.Fatal("a second write at one address was taken, and one of the two values would be lost")
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("the refusal %v is not a plane refusal", err)
	}
}

// TestCommitReportsEveryWriteThatFailed is the aggregation rule at the last
// moment it can be applied: a commit that could not write two of its keys names
// both, because an operator wants one report and not one round of the loop per
// key.
//
// The whole aggregate reaches ferry as one element, which is core's rule rather
// than this driver's: ferry cannot attribute addresses inside a third party's
// error tree, so each element names its own address in its own text.
func TestCommitReportsEveryWriteThatFailed(t *testing.T) {
	t.Parallel()

	store := newFake().failPuts("app/port", "app/zone")

	err := ferry.Dump(t.Context(), three{Host: "h", Port: "8080", Zone: "eu"}, mustSink(t, store))
	if err == nil {
		t.Fatal("a commit whose writes failed reported success")
	}

	if !errors.Is(err, errFakeWrite) {
		t.Errorf("the dump failed with %v, which does not carry the client's own error", err)
	}

	for _, addr := range []ferry.Path{ferry.At("port"), ferry.At("zone")} {
		if !strings.Contains(err.Error(), addr.String()) {
			t.Errorf("the report %q does not name %s, so one of two failed writes went unmentioned", err, addr)
		}
	}
}

// TestWritesRefuseACancelledContext is the write half of what a cancelled
// context means. What ferry does when a cancellation races a driver error is
// #20's and is not answered here.
func TestWritesRefuseACancelledContext(t *testing.T) {
	t.Parallel()

	addr := ferry.At("host")
	store := newFake()
	sink := mustSink(t, store)

	open, err := sink.Bind(ferry.NewAddressSet(addr))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	w := openWriter(t, sink, addr)
	c := committer(t, w)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err = open(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("the open answered %v under a cancelled context, want the context's own error", err)
	}

	if err = w.Set(ctx, addr, ferry.String("h")); !errors.Is(err, context.Canceled) {
		t.Errorf("Set answered %v under a cancelled context, want the context's own error", err)
	}

	if err = c.Commit(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Commit answered %v under a cancelled context, want the context's own error", err)
	}

	if got := store.putCount(); got != 0 {
		t.Errorf("a cancelled write reached the store %d times, want none", got)
	}
}

// TestSinkBindRefusesACollision is the injectivity obligation on the write
// side, where it is the one that matters most: two addresses rendering to one
// key would leave the store holding whichever was written last, with no error
// anywhere.
func TestSinkBindRefusesACollision(t *testing.T) {
	t.Parallel()

	store := newFake()
	addrs := ferry.NewAddressSet(ferry.At("tags").Elem(0), ferry.At("tags", "0"))

	if _, err := mustSink(t, store).Bind(addrs); err == nil {
		t.Fatal("Bind accepted an address set whose two members render to one store key")
	}

	if got := store.calls(); got != 0 {
		t.Errorf("Bind made %d backend calls, want none", got)
	}
}

// TestSetRefusesAnUnnameableMintedAddress is the dynamic tier of the legality
// check on the write side, which is where it runs before the write it belongs
// to rather than after it.
func TestSetRefusesAnUnnameableMintedAddress(t *testing.T) {
	t.Parallel()

	at := ferry.At("labels")
	store := newFake()
	w := openWriter(t, mustSink(t, store), at)

	if err := w.Set(t.Context(), at.At("a/b"), ferry.String("v")); err == nil {
		t.Error("Set accepted a minted address whose segment the store has no name for")
	}

	if got := store.putCount(); got != 0 {
		t.Errorf("a refused Set reached the store %d times, want none", got)
	}
}

// TestBytesAndBoolsAreStoredAsTheirOwnText pins what this plane does to the
// kinds it is handed, at the boundary rather than through a round trip: a
// store holds bytes, so every kind but Null is the text the boundary spelled.
func TestBytesAndBoolsAreStoredAsTheirOwnText(t *testing.T) {
	t.Parallel()

	store := newFake()
	addrs := []ferry.Path{ferry.At("flag"), ferry.At("raw"), ferry.At("port")}
	w := openWriter(t, mustSink(t, store), addrs...)

	values := []ferry.Value{ferry.Bool(true), ferry.Bytes([]byte("\x00\xffA")), ferry.Number("8080")}
	for i, addr := range addrs {
		if err := w.Set(t.Context(), addr, values[i]); err != nil {
			t.Fatalf("Set(%s): %v", addr, err)
		}
	}

	if err := committer(t, w).Commit(t.Context()); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	want := `"app/flag" = "true"` + "\n" + `"app/port" = "8080"` + "\n" + `"app/raw" = "\x00\xffA"` + "\n"
	if got := string(store.contents()); got != want {
		t.Errorf("the store holds\n%s\nwant\n%s", got, want)
	}
}

// openWriter binds one address and opens a writer over it.
func openWriter(t *testing.T, sink *kv.Sink, addrs ...ferry.Path) ferry.Writer {
	t.Helper()

	open, err := sink.Bind(ferry.NewAddressSet(addrs...))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	w, err := open(t.Context())
	if err != nil {
		t.Fatalf("opening a writer: %v", err)
	}

	return w
}
