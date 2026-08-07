package kv_test

import (
	"context"
	"errors"
	"reflect"
	"strconv"
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

// The section holding one of each kind under it, which is what a section's
// presence has to be answered from.
type (
	deepOpts struct {
		Root  string            `ferry:"root"`
		Inner innerOpts         `ferry:"inner"`
		Tags  map[string]string `ferry:"tags"`
	}

	innerOpts struct {
		Deep string `ferry:"deep"`
	}

	deepConfig struct {
		Opts *deepOpts `ferry:"opts"`
	}
)

// TestASectionIsPresentFromItsOwnMembers is the scoping this driver shares with
// every flat plane.
//
// A section's members come from the type, so a key in the same folder that
// belongs to no address of this schema is somebody else's key and says nothing.
// A composite under the section is the case a table of keys cannot answer,
// because its own members come from the store, so its folder is listed and
// everything in it is one of them.
func TestASectionIsPresentFromItsOwnMembers(t *testing.T) {
	t.Parallel()

	cases := map[string]deepCase{
		"a leaf under a nested section":    {key: "app/opts/inner/deep", there: true},
		"a member of a composite under it": {key: "app/opts/tags/a", there: true},
		"a key this schema does not name":  {key: "app/opts_extra"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkDeepOpts(t, tc)
		})
	}
}

// deepCase is one key the store holds and whether the section is there with it.
type deepCase struct {
	key   string
	there bool
}

// checkDeepOpts loads one store holding one key and reports whether the section
// came back.
func checkDeepOpts(t *testing.T, tc deepCase) {
	t.Helper()

	store := newFake()
	store.data[tc.key] = []byte("v")

	got, err := ferry.Load[deepConfig](t.Context(), mustSource(t, store))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if (got.Opts != nil) != tc.there {
		t.Errorf("Opts = %+v, want a section there = %v", got.Opts, tc.there)
	}
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

	set := addrsOf[emptyMissing](t)

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
			assertGet(t, openReader(t, mustSource(t, store), set), leafAt(t, set, tc.addr), tc.want)
		})
	}
}

// assertGet reads one address and compares what came back, kind included: a
// [ferry.Value] is comparable, so == asserts the kind and the text at once.
func assertGet(t *testing.T, r ferry.Reader, addr ferry.LeafAddr, want ferry.Value) {
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

	set := addrsOf[tagsLabels](t)

	for _, tc := range []struct {
		name string
		at   ferry.Path
		want []ferry.Segment
	}{
		{
			name: "base-10 names a position",
			at:   ferry.At("tags"),
			want: []ferry.Segment{ferry.IndexSegment(0), ferry.IndexSegment(1)},
		},
		{
			name: "everything else names a member, and a deeper key names its folder",
			at:   ferry.At("labels"),
			want: []ferry.Segment{
				ferry.NameSegment("01"), ferry.NameSegment("99999999999999999999999"),
				ferry.NameSegment("env"), ferry.NameSegment("team"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			e := enumerator(t, mustSource(t, store), set)
			assertChildren(t, e, compositeAt(t, set, tc.at), tc.want)
		})
	}
}

// assertChildren compares whole segments rather than their text, which is what
// asserts the kind: a driver mints a position or a member and the two are
// different answers even where the text is alike.
func assertChildren(t *testing.T, e ferry.Enumerator, at ferry.CompositeAddr, want []ferry.Segment) {
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
func enumerator(t *testing.T, src *kv.Source, set *ferry.AddressSet) ferry.Enumerator {
	t.Helper()

	r := openReader(t, src, set)

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

	set := addrsOf[dbSection](t)
	addr := leafAt(t, set, ferry.At("db", "host"))

	got, err := openReader(t, src, set).Get(t.Context(), addr)
	if err != nil {
		t.Fatalf("Get(%s): %v", addr, err)
	}

	if want := ferry.String("h"); got != want {
		t.Errorf("Get(%s) answered %#v, want %#v: two prefix segments are two steps of the key", addr, got, want)
	}
}

// TestBindRefusesAnUnnameableAddress is the legality half of ADR-0003's rule: a
// segment the store has no name for is refused rather than written somewhere
// else, as the plane's own class, and before the store is touched.
//
// The injectivity half is not here, and its absence is a fact rather than a
// gap: the one many-to-one step this key space has is the segment kind, and
// both spellings at once need one address to be two containers, which the
// compiler now refuses from the type alone. TestTheOneCollisionThisKeySpaceHas-
// IsCaughtBeforeTheDriver is where that is asserted.
func TestBindRefusesAnUnnameableAddress(t *testing.T) {
	t.Parallel()

	store := newFake()

	_, err := mustSource(t, store).Bind(addrsOf[separatorInSegment](t))
	if err == nil {
		t.Fatal("Bind accepted a segment holding the store's own separator, which a store cannot name")
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("Bind refused with %v, which is not a plane refusal", err)
	}

	if !strings.Contains(err.Error(), separator) {
		t.Errorf("the refusal %q does not say what it is about", err)
	}

	if got := store.calls(); got != 0 {
		t.Errorf("Bind made %d backend calls, want none", got)
	}
}

// separatorInSegment names one address whose segment text holds the store's own
// separator, which is ordinary segment text to ferry and another step in the
// hierarchy to a store.
type separatorInSegment struct {
	Odd string `ferry:"db/host"`
}

// separator is the store's own, spelled here so the assertion above reads
// against the same byte the driver joins with.
const separator = "/"

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
	set := addrsOf[hostOnly](t)
	addr := leafAt(t, set, ferry.At("host"))
	r := openReader(t, mustSource(t, store), set)

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

	open, err := src.Bind(addrsOf[hostOnly](t))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	if _, err = open(t.Context()); !errors.Is(err, errFakeRead) {
		t.Errorf("a batch open over an unreadable store failed with %v, want the client's own error", err)
	}
}

func listFailsAtChildren(t *testing.T) {
	t.Parallel()

	set := addrsOf[tagsLabels](t)
	at := compositeAt(t, set, ferry.At("labels"))
	e := enumerator(t, mustSource(t, newFake().failLists()), set)

	if _, err := e.Children(t.Context(), at); !errors.Is(err, errFakeRead) {
		t.Errorf("Children over an unreadable store failed with %v, want the client's own error", err)
	}
}

// TestChildrenRefusesACancelledContext keeps the enumerator honest about the
// same rule Get is held to.
func TestChildrenRefusesACancelledContext(t *testing.T) {
	t.Parallel()

	set := addrsOf[tagsLabels](t)
	at := compositeAt(t, set, ferry.At("labels"))
	store := newFake()
	r := openReader(t, mustSource(t, store), set)

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

	open, err := mustSource(t, store, kv.WithBatch()).Bind(addrsOf[hostOnly](t))
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
func openReader(t *testing.T, src *kv.Source, set *ferry.AddressSet) ferry.Reader {
	t.Helper()

	open, err := src.Bind(set)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	r, err := open(t.Context())
	if err != nil {
		t.Fatalf("opening a reader: %v", err)
	}

	return r
}

// wide is the schema the concurrency measurement is taken over: eight leaves at
// two depths, which is enough members for a walk to have something to overlap
// and a container inside a container for it to overlap at more than one level.
//
// Every member is a map key of its own in the store, so a lazy load makes eight
// Gets and each of them is a place the walk can be scheduled.
type wide struct {
	One   string    `ferry:"one"`
	Two   string    `ferry:"two"`
	Three string    `ferry:"three"`
	Four  string    `ferry:"four"`
	Five  string    `ferry:"five"`
	Six   string    `ferry:"six"`
	Under wideUnder `ferry:"under"`
}

type wideUnder struct {
	Seven string `ferry:"seven"`
	Eight string `ferry:"eight"`
}

// wideValue is what every load in the concurrency tests reads back.
func wideValue() wide {
	return wide{
		One: "1", Two: "2", Three: "3", Four: "4", Five: "5", Six: "6",
		Under: wideUnder{Seven: "7", Eight: "8"},
	}
}

// TestLazyReadsOverlapUnderABudget is the capability this driver declares,
// measured rather than asserted: a lazy reader under a caller's budget has more
// than one read in the store at once, and produces the struct the serial load
// produces.
//
// The overlap is what the declaration promises core, and the equality is what
// makes the promise worth having. A driver that overlapped and answered
// differently would be faster and wrong.
func TestLazyReadsOverlapUnderABudget(t *testing.T) {
	t.Parallel()

	const budget = 4

	store := newFake().meterGets(2)
	if err := ferry.Dump(t.Context(), wideValue(), mustSink(t, store)); err != nil {
		t.Fatalf("seeding the store: %v", err)
	}

	got, err := ferry.Load[wide](t.Context(), mustSource(t, store), ferry.MaxConcurrency(budget))
	if err != nil {
		t.Fatalf("load under a budget of %d: %v", budget, err)
	}

	if got != wideValue() {
		t.Errorf("the load produced %+v, want %+v", got, wideValue())
	}

	if peak := store.peaked(); peak < 2 {
		t.Errorf("the store held %d reads at once under a budget of %d, want at least 2: a reader declaring "+
			"the capability is one core is allowed to overlap", peak, budget)
	}

	if peak := store.peaked(); peak > budget {
		t.Errorf("the store held %d reads at once, want no more than the budget of %d", peak, budget)
	}
}

// TestABudgetChangesNothingAboutWhatIsRead is the equivalence, from this
// driver's own side: every budget produces the value the serial load produces,
// and a batch open produces it too.
//
// The conformance suite holds this driver to the same property over its own
// fixtures. This is the same question asked over a store seeded by hand, which
// is what makes a failure here readable as this driver's rather than as a
// conformance case's.
func TestABudgetChangesNothingAboutWhatIsRead(t *testing.T) {
	t.Parallel()

	opens := map[string][]kv.Option{
		"lazy":  nil,
		"batch": {kv.WithBatch()},
	}

	for _, n := range []int{1, 2, 3, 8} {
		t.Run("budget "+strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			for how, opts := range opens {
				assertWideLoads(t, n, how, opts)
			}
		})
	}
}

// assertWideLoads loads the fixture out of a store of its own under one budget,
// which is the fresh-destination rule: a store shared between two schedules is
// what makes a broken second walk pass.
func assertWideLoads(t *testing.T, n int, how string, opts []kv.Option) {
	t.Helper()

	store := newFake()
	if err := ferry.Dump(t.Context(), wideValue(), mustSink(t, store)); err != nil {
		t.Fatalf("seeding the store: %v", err)
	}

	got, err := ferry.Load[wide](t.Context(), mustSource(t, store, opts...), ferry.MaxConcurrency(n))
	if err != nil {
		t.Fatalf("%s load under a budget of %d: %v", how, n, err)
	}

	if got != wideValue() {
		t.Errorf("a %s load under a budget of %d produced %+v, want %+v", how, n, got, wideValue())
	}
}

// TestMintingUnderABudgetIsSerialised is the obligation the capability carries:
// a key function belongs to its open and is not safe for concurrent use, and a
// driver that declares it tolerates overlap is the one that serialises its own.
//
// The addresses are minted rather than static - a map's keys come from the
// value, so every one of them goes through the open's minted set - and what
// reports a failure here is the race detector, which is why the assertion is
// only that the load produced what was stored.
func TestMintingUnderABudgetIsSerialised(t *testing.T) {
	t.Parallel()

	want := wideMap()

	store := newFake()
	if err := ferry.Dump(t.Context(), want, mustSink(t, store)); err != nil {
		t.Fatalf("seeding the store: %v", err)
	}

	got, err := ferry.Load[mapped](t.Context(), mustSource(t, store), ferry.MaxConcurrency(4))
	if err != nil {
		t.Fatalf("load under a budget: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("the load produced %+v, want %+v", got, want)
	}
}

// mapped is a schema whose members are minted from the value: four maps, each
// under a container of its own, so a walk that overlaps enters the key function
// from more than one goroutine and does so many times.
//
// Four rather than two, and six keys rather than three, because what is being
// looked for is an unsynchronised write and the race detector finds one by
// meeting it: every read this store answers takes the store's own lock, which
// orders whatever came before it, so the pairs that are genuinely unordered are
// the ones that got there first. Widening the schema widens that window until
// the defect is reported on every run rather than on some of them.
type mapped struct {
	One   map[string]string `ferry:"one"`
	Two   map[string]string `ferry:"two"`
	Three map[string]string `ferry:"three"`
	Four  map[string]string `ferry:"four"`
}

func wideMap() mapped {
	return mapped{
		One:   mintedKeys("a"),
		Two:   mintedKeys("b"),
		Three: mintedKeys("c"),
		Four:  mintedKeys("d"),
	}
}

// mintedKeys is six map entries under one prefix, and no two maps share a key,
// so every address this schema reaches is minted exactly once.
func mintedKeys(at string) map[string]string {
	out := make(map[string]string, 6)
	for i := range 6 {
		out[at+strconv.Itoa(i)] = strconv.Itoa(i)
	}

	return out
}
