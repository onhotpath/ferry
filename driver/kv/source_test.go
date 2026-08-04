package kv_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/kv"
)

// three is the schema the batch-versus-lazy measurement is taken over, and it
// is three leaves because ADR-0004 states the measurement over three addresses.
type three struct {
	Host string `ferry:"host"`
	Port string `ferry:"port"`
	Zone string `ferry:"zone"`
}

// TestBatchAndLazyAgree is ADR-0004's measurement, kept as a test: one boolean
// inside the driver, three backend calls against one, and identical results.
//
// It is the case that decides ferry needs no Snapshotter interface. Nothing
// above the driver can tell the two runs apart, which is what makes the choice
// the driver's to make.
func TestBatchAndLazyAgree(t *testing.T) {
	t.Parallel()

	const wantLazy, wantBatch = 3, 1

	lazyStore, batchStore := seeded(), seeded()

	lazy, err := ferry.Load[three](t.Context(), mustSource(t, lazyStore))
	if err != nil {
		t.Fatalf("lazy load: %v", err)
	}

	batch, err := ferry.Load[three](t.Context(), mustSource(t, batchStore, kv.WithBatch()))
	if err != nil {
		t.Fatalf("batch load: %v", err)
	}

	if lazy != batch {
		t.Errorf("lazy loaded %+v and batch loaded %+v: the choice is the driver's precisely because nothing "+
			"above it can tell them apart", lazy, batch)
	}

	if got := lazyStore.calls(); got != wantLazy {
		t.Errorf("a lazy open made %d backend calls over a three-address schema, want %d", got, wantLazy)
	}

	if got := batchStore.calls(); got != wantBatch {
		t.Errorf("a batch open made %d backend calls over a three-address schema, want %d: batch is one round "+
			"trip at the open and no call per address", got, wantBatch)
	}
}

// seeded is the three-address store both halves of the measurement read.
func seeded() *fake {
	store := newFake()
	store.data["app/host"] = []byte("h")
	store.data["app/port"] = []byte("8080")
	store.data["app/zone"] = []byte("eu")

	return store
}

// TestGetFailureIsNeverAbsent is ADR-0014's conformance case 4 asserted against
// this driver directly rather than only through the suite, because the failure
// it rules out is one a driver can commit on its own: a survey found a real
// provider discarding its read errors and answering with an empty result, which
// is a config that loads, looks fine and is wrong.
func TestGetFailureIsNeverAbsent(t *testing.T) {
	t.Parallel()

	const third = 3

	store := seeded().failGet(third)

	got, err := ferry.Load[three](t.Context(), mustSource(t, store))
	if err == nil {
		t.Fatalf("a store whose third read failed loaded %+v with a nil error", got)
	}

	if !errors.Is(err, errFakeRead) {
		t.Errorf("the load failed with %v, which does not carry the client's own error: a driver's cause has to "+
			"survive to the caller", err)
	}

	if got != (three{}) {
		t.Errorf("a failed load yielded %+v, want the zero value", got)
	}
}

// TestGetAtAMissingAddressIsAbsent pins the other half of the same pair: an
// address the store does not hold is Absent, and one it holds with no bytes is
// a String with none. They are different observations and a plane with no null
// still keeps them apart.
func TestGetAtAMissingAddressIsAbsent(t *testing.T) {
	t.Parallel()

	store := newFake()
	store.data["app/empty"] = []byte{}

	for _, tc := range []struct {
		name string
		addr ferry.Path
		want ferry.Value
	}{
		{name: "held with no bytes", addr: ferry.At("empty"), want: ferry.String("")},
		{name: "not held at all", addr: ferry.At("missing"), want: ferry.Value{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertGet(t, openReader(t, mustSource(t, store), tc.addr), tc.addr, tc.want)
		})
	}
}

// assertGet reads one address and compares what came back, kind included: a
// [ferry.Value] is comparable, so == asserts the kind and the text at once.
func assertGet(t *testing.T, r ferry.Reader, addr ferry.Path, want ferry.Value) {
	t.Helper()

	got, err := r.Get(t.Context(), addr)
	if err != nil {
		t.Fatalf("Get(%s): %v", addr, err)
	}

	if got != want {
		t.Errorf("Get(%s) answered %#v, want %#v", addr, got, want)
	}
}

// TestChildrenReadTheSegmentKindOffTheText is the residue of a key space that
// carries no segment kind, pinned so that a change to it is deliberate.
//
// A position is canonical base-10 and everything else is a member name, which
// is exactly what the key function writes. The deeper key contributes the
// folder it lies in rather than itself, because Children answers with immediate
// members.
func TestChildrenReadTheSegmentKindOffTheText(t *testing.T) {
	t.Parallel()

	store := newFake()
	store.data["app/tags/0"] = []byte("a")
	store.data["app/tags/1"] = []byte("b")
	store.data["app/labels/env"] = []byte("prod")
	store.data["app/labels/team/name"] = []byte("core")
	store.data["app/labels/01"] = []byte("not a position")
	store.data["app/labels/99999999999999999999999"] = []byte("no position is this large")

	for _, tc := range []struct {
		name string
		at   ferry.Path
		want []ferry.Path
	}{
		{
			name: "base-10 names a position",
			at:   ferry.At("tags"),
			want: []ferry.Path{ferry.At("tags").Elem(0), ferry.At("tags").Elem(1)},
		},
		{
			name: "everything else names a member, and a deeper key names its folder",
			at:   ferry.At("labels"),
			want: []ferry.Path{
				ferry.At("labels", "01"), ferry.At("labels", "99999999999999999999999"),
				ferry.At("labels", "env"), ferry.At("labels", "team"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertChildren(t, enumerator(t, mustSource(t, store), tc.at), tc.at, tc.want)
		})
	}
}

// assertChildren compares whole addresses rather than segment text, which is
// what asserts the segment kind: two addresses are equal exactly when they
// render alike, and an Index segment renders differently from a Name one.
func assertChildren(t *testing.T, e ferry.Enumerator, at ferry.Path, want []ferry.Path) {
	t.Helper()

	got, err := e.Children(t.Context(), at)
	if err != nil {
		t.Fatalf("Children(%s): %v", at, err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("Children(%s) answered %v, want %v", at, got, want)
	}
}

// enumerator opens a reader and asserts that it lists, which this driver's does
// because a store lists trivially.
func enumerator(t *testing.T, src *kv.Source, addrs ...ferry.Path) ferry.Enumerator {
	t.Helper()

	r := openReader(t, src, addrs...)

	e, ok := r.(ferry.Enumerator)
	if !ok {
		t.Fatalf("%T does not enumerate, and a store lists trivially", r)
	}

	return e
}

// TestPrefixPrependsSegments is ADR-0003's prefix rule: a prefix prepends
// segments and never concatenates text, so the key is the prefix's segments
// followed by the address's own, joined by the store's separator.
func TestPrefixPrependsSegments(t *testing.T) {
	t.Parallel()

	store := newFake()
	store.data["one/two/db/host"] = []byte("h")

	src, err := kv.NewSource(store, kv.WithPrefix("one", "two"))
	if err != nil {
		t.Fatalf("kv.NewSource: %v", err)
	}

	addr := ferry.At("db", "host")

	got, err := openReader(t, src, addr).Get(t.Context(), addr)
	if err != nil {
		t.Fatalf("Get(%s): %v", addr, err)
	}

	if want := ferry.String("h"); got != want {
		t.Errorf("Get(%s) answered %#v, want %#v: two prefix segments are two steps of the key", addr, got, want)
	}
}

// TestBindRefusesACollisionBeforeAnyCall is ADR-0003's injectivity obligation,
// discharged through core's helper.
//
// The colliding pair is the one this key space actually has: a store key
// carries no segment kind, so the position 0 under /tags and the member "0"
// under /tags are one key, and one of the two would be lost. The refusal names
// both addresses, is a plane refusal, and lands before the store is touched -
// which is what lets a plane-to-plane transfer be refused after zero backend
// calls rather than after reading the whole source.
func TestBindRefusesACollisionBeforeAnyCall(t *testing.T) {
	t.Parallel()

	store := newFake()
	position, member := ferry.At("tags").Elem(0), ferry.At("tags", "0")
	addrs := ferry.NewAddressSet(position, member)

	_, err := mustSource(t, store).Bind(addrs)
	if err == nil {
		t.Fatal("Bind accepted an address set whose two members render to one store key")
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("Bind refused with %v, which is not a plane refusal", err)
	}

	for _, addr := range []ferry.Path{position, member} {
		if !strings.Contains(err.Error(), addr.String()) {
			t.Errorf("the refusal %q does not name %s, and one of the two is the address the author has to move",
				err, addr)
		}
	}

	if got := store.calls(); got != 0 {
		t.Errorf("Bind made %d backend calls, want none: both checks run before any I/O", got)
	}
}

// TestBindRefusesAnUnnameableAddress is the legality half of the same
// obligation: a segment the store has no name for is refused rather than
// written somewhere else.
func TestBindRefusesAnUnnameableAddress(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		addr ferry.Path
	}{
		{name: "an empty segment", addr: ferry.At("db", "")},
		{name: "a segment holding the separator", addr: ferry.At("db/host")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertUnnameable(t, tc.addr)
		})
	}
}

// assertUnnameable holds one address to the legality half of ADR-0003's rule:
// refused, as the plane's own class, and before the store is touched.
func assertUnnameable(t *testing.T, addr ferry.Path) {
	t.Helper()

	store := newFake()

	_, err := mustSource(t, store).Bind(ferry.NewAddressSet(addr))
	if err == nil {
		t.Fatalf("Bind accepted %s, which a store cannot name", addr)
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("Bind refused with %v, which is not a plane refusal", err)
	}

	if got := store.calls(); got != 0 {
		t.Errorf("Bind made %d backend calls, want none", got)
	}
}

// TestCancellationIsHonoured is the driver's half of what a cancelled context
// means. What ferry does when a cancellation races a driver error is #20's and
// is not answered here.
func TestCancellationIsHonoured(t *testing.T) {
	t.Parallel()

	t.Run("before the client is asked anything", cancelledBeforeTheCall)
	t.Run("while a read is in flight", cancelledDuringTheCall)
}

// cancelledBeforeTheCall is the guard at the top of every driver call: a
// context that has already ended is answered with its own error, and the store
// is never asked.
func cancelledBeforeTheCall(t *testing.T) {
	t.Parallel()

	store := seeded()
	addr := ferry.At("host")
	r := openReader(t, mustSource(t, store), addr)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := r.Get(ctx, addr); !errors.Is(err, context.Canceled) {
		t.Errorf("Get under a cancelled context answered %v, want the context's own error", err)
	}

	if got := store.calls(); got != 0 {
		t.Errorf("Get made %d backend calls under a cancelled context, want none", got)
	}
}

// cancelledDuringTheCall is the same rule where the client is genuinely in
// flight: the store's own report of the cancellation reaches the caller with
// context.Canceled still matchable under it.
func cancelledDuringTheCall(t *testing.T) {
	t.Parallel()

	store := newFake().blockGets()
	src := mustSource(t, store)
	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan error, 1)

	go func() {
		_, err := ferry.Load[three](ctx, src)
		done <- err
	}()

	<-store.entered
	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("a load cancelled while a read was in flight failed with %v, want the context's own error "+
			"reachable under it", err)
	}
}

// composites is the schema whose addresses come from the value rather than
// from the type, which is the only shape that needs an enumerator at all.
type composites struct {
	Tags   []string          `ferry:"tags"`
	Labels map[string]string `ferry:"labels"`
}

// TestBatchAnswersChildrenFromItsSnapshot is the batch half of enumeration: an
// open that already fetched the plane lists nothing, so a whole map and a whole
// slice load out of the one round trip the open made.
func TestBatchAnswersChildrenFromItsSnapshot(t *testing.T) {
	t.Parallel()

	const wantCalls = 1

	want := composites{Tags: []string{"a", "b"}, Labels: map[string]string{"env": "prod"}}

	store := newFake()
	if err := ferry.Dump(t.Context(), want, mustSink(t, store)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	// Also holds a key outside this schema's addresses, because a batch open
	// fetches the whole prefix and enumeration has to answer about the folder
	// it was asked about rather than about everything it holds.
	store.data["app/elsewhere"] = []byte("x")

	before := store.calls()

	got, err := ferry.Load[composites](t.Context(), mustSource(t, store, kv.WithBatch()))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("loaded %+v, want %+v", got, want)
	}

	if calls := store.calls() - before; calls != wantCalls {
		t.Errorf("a batch load of a slice and a map made %d backend calls, want %d: enumeration is answered out "+
			"of the snapshot the open already fetched", calls, wantCalls)
	}
}

// TestNoPrefixReachesTheWholeStore is the driver with no prefix, which is the
// root of the key space: an address is its own segments and a batch open lists
// everything rather than one folder.
func TestNoPrefixReachesTheWholeStore(t *testing.T) {
	t.Parallel()

	store := newFake()
	store.data["host"] = []byte("h")
	store.data["port"] = []byte("8080")
	store.data["zone"] = []byte("eu")

	src, err := kv.NewSource(store, kv.WithBatch())
	if err != nil {
		t.Fatalf("kv.NewSource: %v", err)
	}

	got, err := ferry.Load[three](t.Context(), src)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if want := (three{Host: "h", Port: "8080", Zone: "eu"}); got != want {
		t.Errorf("loaded %+v, want %+v", got, want)
	}
}

// TestListFailureIsReported is the other half of "a failure reaches the caller
// as a failure": a listing is I/O too, and a store that cannot be listed is not
// a store with nothing in it.
func TestListFailureIsReported(t *testing.T) {
	t.Parallel()

	t.Run("at a batch open", listFailsAtTheOpen)
	t.Run("at a lazy Children", listFailsAtChildren)
}

func listFailsAtTheOpen(t *testing.T) {
	t.Parallel()

	src := mustSource(t, newFake().failLists(), kv.WithBatch())

	open, err := src.Bind(ferry.NewAddressSet(ferry.At("host")))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	if _, err = open(t.Context()); !errors.Is(err, errFakeRead) {
		t.Errorf("a batch open over an unreadable store failed with %v, want the client's own error", err)
	}
}

func listFailsAtChildren(t *testing.T) {
	t.Parallel()

	at := ferry.At("labels")
	e := enumerator(t, mustSource(t, newFake().failLists()), at)

	if _, err := e.Children(t.Context(), at); !errors.Is(err, errFakeRead) {
		t.Errorf("Children over an unreadable store failed with %v, want the client's own error", err)
	}
}

// TestReadsRefuseAnUnnameableMintedAddress is the dynamic tier of the legality
// check: an address that came from a value is checked as it is asked for, not
// only at Bind, because a map key does not exist until there is a value.
func TestReadsRefuseAnUnnameableMintedAddress(t *testing.T) {
	t.Parallel()

	at := ferry.At("labels")
	minted := at.At("a/b")
	r := openReader(t, mustSource(t, newFake()), at)

	if _, err := r.Get(t.Context(), minted); err == nil {
		t.Errorf("Get accepted %s, whose segment the store has no name for", minted)
	}
}

// TestChildrenRefusesACancelledContext keeps the enumerator honest about the
// same rule Get is held to.
func TestChildrenRefusesACancelledContext(t *testing.T) {
	t.Parallel()

	at := ferry.At("labels")
	store := newFake()
	r := openReader(t, mustSource(t, store), at)

	e, ok := r.(ferry.Enumerator)
	if !ok {
		t.Fatalf("%T does not enumerate", r)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := e.Children(ctx, at); !errors.Is(err, context.Canceled) {
		t.Errorf("Children under a cancelled context answered %v, want the context's own error", err)
	}

	if got := store.calls(); got != 0 {
		t.Errorf("Children made %d backend calls under a cancelled context, want none", got)
	}
}

// TestOpenRefusesACancelledContext is the same rule at the open, which is where
// a batch source would otherwise make its one round trip.
func TestOpenRefusesACancelledContext(t *testing.T) {
	t.Parallel()

	store := newFake()

	open, err := mustSource(t, store, kv.WithBatch()).Bind(ferry.NewAddressSet(ferry.At("host")))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err = open(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("the open answered %v under a cancelled context, want the context's own error", err)
	}

	if got := store.calls(); got != 0 {
		t.Errorf("the open made %d backend calls under a cancelled context, want none", got)
	}
}

// openReader binds one address and opens a reader over it.
func openReader(t *testing.T, src *kv.Source, addrs ...ferry.Path) ferry.Reader {
	t.Helper()

	open, err := src.Bind(ferry.NewAddressSet(addrs...))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	r, err := open(t.Context())
	if err != nil {
		t.Fatalf("opening a reader: %v", err)
	}

	return r
}
