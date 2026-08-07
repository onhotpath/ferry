package ferry

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Every rule in this file is asserted through Load, Dump and Compile: a cache
// hit is a compile that did not happen, and a compile is counted through a
// registered codec whose decode half a compile calls exactly once per leaf. The
// one exception is the size of the cache itself, which is asserted structurally
// and is argued at [cachedSchemas], because the size of a map has no behaviour.
//
// The tests that count compiles do not run in parallel, because the counter is
// one package-level value and a parallel neighbour compiling the same type
// would be indistinguishable from a cache miss. Go runs every serial top-level
// test to completion before it resumes the parallel ones, so serial is the
// whole of what that costs.

// cacheKeyed is one struct under two tag keys, which is ADR-0008's measurement
// as a fixture: one reflect.Type, two address sets, and therefore two schemas
// that a cache keyed by the type alone would confuse.
type cacheKeyed struct {
	Host string `ferry:"host" mylib:"HOST"`
	Port string `ferry:"port" mylib:"PORT"`
}

// cacheInterval is a named type over int64 that compiles both ways with
// different representations: unregistered its kind admits it as a number, and
// DurationLike gives it time.Duration's own text. That is what makes it the
// fixture for both the registry half of the cache key and for Compile retaining
// nothing - a type that only compiles with the codec makes the failure loud and
// measures nothing.
type cacheInterval time.Duration

type cacheIntervalConf struct {
	Poll cacheInterval `ferry:"poll"`
}

// cachedSchemas is how many entries a registry's schema cache holds.
//
// It is the one assertion in this file that is not made through the seam, and
// the reason is that the claim has no behaviour to observe: "one entry per root
// type" is a statement about the size of a map, and a cache holding three
// entries where it should hold one would answer every call identically. What
// the entry count buys over the behavioural half beside it is that a second
// entry for one configuration would be silent until the day it disagreed.
func cachedSchemas(r *Registry) int {
	n := 0

	r.schemas.Range(func(_, _ any) bool {
		n++

		return true
	})

	return n
}

// addressesOf renders a bound address set, so that two of them can be compared
// as data rather than as pointers.
func addressesOf(t *testing.T, a *AddressSet) []string {
	t.Helper()

	if a == nil {
		t.Fatal("no address set reached the driver's Bind")
	}

	return kinded(a)
}

// TestOneTypeUnderTwoTagKeysIsTwoEntries is the tag key's half of the cache
// key, which ADR-0008 put there and this ticket did not choose.
//
// The two loads differ in nothing but the Option, and what the driver was bound
// to is the observable difference: one reflect.Type, two address sets. A cache
// keyed by the type alone would hand the second load the first one's schema and
// bind the driver to addresses no field of it names.
func TestOneTypeUnderTwoTagKeysIsTwoEntries(t *testing.T) {
	t.Parallel()

	reg := MustRegistry()
	ferryPlane, libPlane := newPlane(map[Path]Value{}), newPlane(map[Path]Value{})

	if _, err := Load[cacheKeyed](t.Context(), planeSource{p: ferryPlane}, WithRegistry(reg)); err != nil {
		t.Fatalf("the load under the default key: %+v", err)
	}

	if _, err := Load[cacheKeyed](t.Context(), planeSource{p: libPlane},
		WithRegistry(reg), TagKey("mylib")); err != nil {
		t.Fatalf("the load under mylib: %+v", err)
	}

	under, over := addressesOf(t, ferryPlane.bound), addressesOf(t, libPlane.bound)
	if slices.Equal(under, over) {
		t.Errorf("both tag keys bound %v, and the two keys name different segments", under)
	}

	if want := []string{"leaf /host", "leaf /port"}; !slices.Equal(under, want) {
		t.Errorf("the default key bound %v, want %v", under, want)
	}

	if want := []string{"leaf /HOST", "leaf /PORT"}; !slices.Equal(over, want) {
		t.Errorf("mylib bound %v, want %v", over, want)
	}

	if got := cachedSchemas(reg); got != 2 {
		t.Errorf("one type under two tag keys made %d cache entries, want 2", got)
	}
}

// TestTwoRegistriesThatDisagreeAreTwoEntries is the registry's half, which
// ADR-0009 put there after measuring what its absence costs: a cache keyed by
// reflect.Type alone hands one registry another registry's codec, silently, so
// a service gets a representation it replaced with no error anywhere.
//
// The two registries disagree about one type and nothing else, and the
// difference is visible at the plane rather than inside ferry.
func TestTwoRegistriesThatDisagreeAreTwoEntries(t *testing.T) {
	t.Parallel()

	value := cacheIntervalConf{Poll: cacheInterval(90 * time.Second)}
	plain, timed := MustRegistry(), registryWith(t, DurationLike[cacheInterval]())

	kind := dumpedValue(t, value, At("poll"), WithRegistry(plain))
	if want := Number("90000000000"); kind != want {
		t.Errorf("the registry with no codec wrote %#v, want %#v", kind, want)
	}

	text := dumpedValue(t, value, At("poll"), WithRegistry(timed))
	if want := String("1m30s"); text != want {
		t.Errorf("the registry with the codec wrote %#v, want %#v", text, want)
	}

	// One entry each, under two registries, is the shape of the answer: the
	// registry is the outer level, so neither compile could have found the
	// other's.
	if got, want := cachedSchemas(plain), 1; got != want {
		t.Errorf("the registry with no codec holds %d entries, want %d", got, want)
	}

	if got, want := cachedSchemas(timed), 1; got != want {
		t.Errorf("the registry with the codec holds %d entries, want %d", got, want)
	}
}

// cachedCount is a named int whose registered codec counts every decode, which
// is how a compile is counted through the seam.
//
// A compile calls the decode half exactly once per leaf that carries both
// omitzero and a default, because that is the pair ADR-0006 makes the compiler
// resolve in order to tell a contradiction from a redundant declaration. A dump
// of the zero value calls it not at all, so a trial that dumps counts compiles
// and nothing else.
type cachedCount int

var cachedDecodes atomic.Int64

func cachedText(c cachedCount) (string, error) { return strconv.Itoa(int(c)), nil }

// compileWindow is how long one compile is made to take.
//
// It is here because the thing being measured is a window rather than a count:
// whether the naive shape duplicates work depends on how much of the herd
// arrives before the first compile finishes, and a compile that is over in
// nanoseconds measures the scheduler rather than the cache. Widening it makes
// the naive shape's failure reliable and makes the two-level form's "exactly
// one" a real claim rather than an artefact of the herd being too slow to
// overlap.
const compileWindow = 200 * time.Microsecond

func parseCached(text string) (cachedCount, error) {
	cachedDecodes.Add(1)
	time.Sleep(compileWindow)

	n, err := strconv.Atoi(text)

	return cachedCount(n), err
}

// countedConf is one leaf carrying the pair that makes a compile call the
// codec. The default is the type's own zero, because a default that is not the
// zero contradicts omitzero and would be a compile error rather than a compile.
type countedConf struct {
	N cachedCount `ferry:"n,omitzero,default=0"`
}

// countingRegistry is a fresh registry with the counting codec in it, which is
// also a cold cache: the cache hangs off the registry, so a new registry is the
// only way to get one.
func countingRegistry(t *testing.T) *Registry {
	t.Helper()

	return registryWith(t, StringValue(cachedText, parseCached))
}

const (
	// herdSize is ADR-0010's own figure for the herd, and the point of a large
	// one is that the naive shape's cost is bounded only by how many arrive
	// inside the window.
	herdSize = 64
	// coldTrials is why this is a measurement rather than an observation: one
	// trial cannot tell "the herd did not overlap" from "the cache worked".
	coldTrials = 20
)

// herd releases herdSize goroutines at once and waits for them.
func herd(do func()) {
	var wg sync.WaitGroup

	start := make(chan struct{})

	for range herdSize {
		wg.Go(func() {
			<-start
			do()
		})
	}

	close(start)
	wg.Wait()
}

// TestAColdCacheCompilesExactlyOncePerTrial is the two-level cache's whole
// claim: Load on the outer map, a cheap entry on a miss, LoadOrStore to settle
// the race, and a per-entry sync.OnceValues that the loser's entry never runs.
//
// Twenty cold caches, sixty-four goroutines each, and one compile per trial.
func TestAColdCacheCompilesExactlyOncePerTrial(t *testing.T) {
	for trial := range coldTrials {
		reg := countingRegistry(t)
		before := cachedDecodes.Load()

		herd(func() {
			err := Dump(t.Context(), countedConf{}, planeSink{p: newPlane(map[Path]Value{})}, WithRegistry(reg))
			if err != nil {
				t.Errorf("dump: %+v", err)
			}
		})

		if got := cachedDecodes.Load() - before; got != 1 {
			t.Fatalf("trial %d compiled %d times against a cold cache, want exactly one", trial, got)
		}
	}
}

// TestTheNaiveShapeDuplicatesWork is the other half of the same measurement,
// and it is here because "the two-level form compiles once" says nothing on its
// own: a herd that never overlaps would pass it under either shape.
//
// The naive shape is a single level with the expensive work done before the
// slot is claimed, which is what eight of eight standard-library type caches
// do and what encoding/gob states the philosophy for outright. What it costs
// here is a whole schema compile per goroutine that arrives inside the window,
// and the number to quote is the worst trial rather than the mean, because it
// is bounded only by the width of that window.
func TestTheNaiveShapeDuplicatesWork(t *testing.T) {
	worst, total := int64(0), int64(0)

	for range coldTrials {
		reg := countingRegistry(t)
		before := cachedDecodes.Load()

		var naive sync.Map

		herd(func() { naiveCompile(t, &naive, reg) })

		n := cachedDecodes.Load() - before
		total += n
		worst = max(worst, n)
	}

	t.Logf("the naive shape compiled %.1f times per trial, worst trial %d", float64(total)/coldTrials, worst)

	if worst <= 1 {
		t.Errorf("the naive shape compiled at most once in every trial, so this measurement is not "+
			"measuring the herd it exists to measure: worst %d over %d trials", worst, coldTrials)
	}
}

// naiveCompile is the shape ADR-0010 refuses, written out so that its failure
// is shown rather than asserted from a table.
//
// Compile is the uncached compiler, which is what makes this a control: it does
// the same work the cached path does, and every goroutine that finds the slot
// empty does it again.
func naiveCompile(t *testing.T, naive *sync.Map, reg *Registry) {
	t.Helper()

	if _, ok := naive.Load(reflect.TypeFor[countedConf]()); ok {
		return
	}

	if err := Compile[countedConf](WithRegistry(reg)); err != nil {
		t.Errorf("compile: %+v", err)
	}

	naive.Store(reflect.TypeFor[countedConf](), struct{}{})
}

// cacheUntagged is a type that does not compile, and the fault is one a reader
// of the report can act on rather than one this file invented.
type cacheUntagged struct {
	Host string
}

// TestACompileErrorIsMemoisedAndReplayed is what sync.OnceValues buys, and it
// matters more for ferry than for a cache whose work panics: ferry's compile
// returns errors, and a second level that only guarded the success would hand
// every later caller a zero schema - an empty address set, and therefore a Load
// that reads nothing and returns nil.
//
// Two callers receive the same error value, which is the strongest available
// statement of "it carries no per-call context": a value two calls share cannot
// hold either one's.
func TestACompileErrorIsMemoisedAndReplayed(t *testing.T) {
	t.Parallel()

	reg := MustRegistry()

	first := loadRefusal(t, reg, newPlane(map[Path]Value{}))
	second := loadRefusal(t, reg, newPlane(map[Path]Value{}))

	if !sameError(first, second) {
		t.Errorf("two callers received two error values:\n\t%+v\n\t%+v", first, second)
	}

	if a, b := fmt.Sprintf("%+v", first), fmt.Sprintf("%+v", second); a != b {
		t.Errorf("the replayed error renders as\n\t%s\nand the first as\n\t%s", b, a)
	}

	// Nothing was compiled the second time, so the cache holds the one entry
	// whose compile failed rather than retrying it per call.
	if n := cachedSchemas(reg); n != 1 {
		t.Errorf("a memoised failure left %d cache entries, want 1", n)
	}
}

// sameError is identity and deliberately not errors.Is.
//
// What is being asserted is that both callers hold one value, and errors.Is
// would answer yes for two equivalent errors compiled twice, which is exactly
// the failure the memoisation exists to prevent.
func sameError(a, b error) bool { return any(a) == any(b) }

// loadRefusal runs one Load that must fail at schema compile, and hands back
// the error value itself rather than a rendering of it.
func loadRefusal(t *testing.T, reg *Registry, p *plane) error {
	t.Helper()

	_, err := Load[cacheUntagged](t.Context(), planeSource{p: p}, WithRegistry(reg))
	if err == nil {
		t.Fatal("a type with an untagged field compiled")
	}

	if p.bound != nil {
		t.Error("a driver was bound although the schema did not compile")
	}

	return err
}

// TestOneMemoisedErrorIsOneRendering is the property that makes a memoised
// error safe to hand to every later caller: it is sorted at construction, so
// there is no lazy state for sixty-four formatters to race on and no run in
// which two of them disagree. CI runs this under -race.
func TestOneMemoisedErrorIsOneRendering(t *testing.T) {
	t.Parallel()

	err := loadRefusal(t, MustRegistry(), newPlane(map[Path]Value{}))
	got := make([]string, herdSize)

	var wg sync.WaitGroup

	for i := range herdSize {
		wg.Go(func() { got[i] = fmt.Sprintf("%+v", err) })
	}

	wg.Wait()

	if d := distinct(got); len(d) != 1 {
		t.Fatalf("%d distinct renderings across %d goroutines: %q", len(d), herdSize, d)
	}
}

// cacheInner is a leaf-carrying struct that appears twice under one root, and
// once under a second root.
type cacheInner struct {
	N cachedCount `ferry:"n,omitzero,default=0"`
}

type cacheTwice struct {
	A cacheInner `ferry:"a"`
	B cacheInner `ferry:"b"`
}

type cacheOnce struct {
	C cacheInner `ferry:"c"`
}

// TestTheCacheIsKeyedPerRootType is the fact json/v1's recursive-type
// placeholder would exist to work around, and the reason ferry does not need
// one.
//
// A nested struct's addresses depend on the path from the root, so the same
// type under two parents compiles to two different address sets and its
// subschema is not reusable. The cache is therefore keyed per root type, a
// compile never performs a cache lookup, and there is no cycle for a
// placeholder to break. Both halves are asserted: the inner type is compiled
// once per position rather than looked up, and the root contributes one entry
// rather than three.
func TestTheCacheIsKeyedPerRootType(t *testing.T) {
	reg := countingRegistry(t)
	before := cachedDecodes.Load()

	dumpedValue(t, cacheTwice{}, At("a", "n"), WithRegistry(reg))

	if got := cachedDecodes.Load() - before; got != 2 {
		t.Errorf("compiling struct{ A inner; B inner } compiled the inner type %d times, want one per "+
			"position: a lookup would make it 1 and a schema the walk cannot use", got)
	}

	if got := cachedSchemas(reg); got != 1 {
		t.Errorf("compiling struct{ A inner; B inner } added %d cache entries, want exactly 1", got)
	}

	// A second root holding the same inner type finds nothing, because nothing
	// was ever keyed under the inner type.
	second := cachedDecodes.Load()

	dumpedValue(t, cacheOnce{}, At("c", "n"), WithRegistry(reg))

	if got := cachedDecodes.Load() - second; got != 1 {
		t.Errorf("a second root holding the same inner type compiled it %d times, want 1", got)
	}

	if got := cachedSchemas(reg); got != 2 {
		t.Errorf("two root types made %d cache entries, want 2", got)
	}
}

type cacheAddrConf struct {
	Host string     `ferry:"host"`
	Port string     `ferry:"port"`
	DB   cacheAddrs `ferry:"db"`
}

type cacheAddrs struct {
	Name string `ferry:"name"`
	User string `ferry:"user"`
}

// TestTheAddressSetHandedToBindIsOnePointer is ADR-0012's property that lands
// here rather than with the binding, because it is a property of the compiled
// schema and not of any driver: the set is a field of the thing the walk
// iterates, so every load of one schema hands the driver the same one.
func TestTheAddressSetHandedToBindIsOnePointer(t *testing.T) {
	t.Parallel()

	reg := MustRegistry()
	first, second := newPlane(map[Path]Value{}), newPlane(map[Path]Value{})

	for _, p := range []*plane{first, second} {
		if _, err := Load[cacheAddrConf](t.Context(), planeSource{p: p}, WithRegistry(reg)); err != nil {
			t.Fatalf("load: %+v", err)
		}
	}

	if first.bound != second.bound {
		t.Errorf("two loads bound two address sets, %v and %v",
			addressesOf(t, first.bound), addressesOf(t, second.bound))
	}
}

// TestAWarmLookupBuildsNothing is the half that could not be got from the
// pointer above: an implementation that rebuilt the address set per load and
// cached the result would still hand out one pointer, and rebuilding it costs
// at least the clone and the header NewAddressSet allocates.
//
// The baseline is resolving the same Option list and stopping there, which
// every call pays whether or not anything is cached. What is measured is what
// the lookup adds on top of it, and it is nothing.
//
// It is serial because testing.AllocsPerRun refuses to run in a parallel test,
// which is the same reason the compile-counting tests are.
func TestAWarmLookupBuildsNothing(t *testing.T) {
	reg := MustRegistry()
	opts := []Option{WithRegistry(reg)}

	if _, err := schemaOf(reflect.TypeFor[cacheAddrConf](), opts, retained); err != nil {
		t.Fatalf("the first compile: %+v", err)
	}

	resolve := testing.AllocsPerRun(warmRuns, func() {
		if _, err := newConfig(opts); err != nil {
			t.Errorf("resolving the Options: %+v", err)
		}
	})

	warm := testing.AllocsPerRun(warmRuns, func() {
		if _, err := schemaOf(reflect.TypeFor[cacheAddrConf](), opts, retained); err != nil {
			t.Errorf("the warm lookup: %+v", err)
		}
	})

	if warm != resolve {
		t.Errorf("a warm lookup allocated %v times against %v for resolving its Options alone, and the "+
			"difference is work being redone per load", warm, resolve)
	}
}

// warmRuns is enough iterations for an allocation that happens on some but not
// all of them to show up as a fraction.
const warmRuns = 100

// TestCompileTakesNoCacheEntry is what is left of ADR-0010's retention rule
// after ADR-0017 moved the freeze into the registry's own construction: a
// Compile discards its schema, so it caches nothing, and there is no second
// omission to measure any more.
//
// The type compiles both ways with different representations, which is what an
// earlier draft of ADR-0010 got wrong: its fixture only compiled with the codec,
// so the failure was loud and the probe measured nothing. Here a Compile that
// retained its schema would surface at the Dump, as an entry it did not put
// there.
func TestCompileTakesNoCacheEntry(t *testing.T) {
	t.Parallel()

	reg := registryWith(t, DurationLike[cacheInterval]())
	value := cacheIntervalConf{Poll: cacheInterval(90 * time.Second)}

	if err := Compile[cacheIntervalConf](WithRegistry(reg)); err != nil {
		t.Fatalf("the compile: %+v", err)
	}

	if got, want := dumpedValue(t, value, At("poll"), WithRegistry(reg)), String("1m30s"); got != want {
		t.Errorf("after Compile and Dump, the plane holds %#v, want %#v", got, want)
	}

	if got := cachedSchemas(reg); got != 1 {
		t.Errorf("a Compile and a Dump left %d cache entries, want 1: Compile does not cache", got)
	}
}

// TestALoadRetainsItsSchema is the other end of the same rule, on the same
// fixture: the verb that keeps its resolution is the verb that caches it.
func TestALoadRetainsItsSchema(t *testing.T) {
	t.Parallel()

	reg := registryWith(t, DurationLike[cacheInterval]())
	p := newPlane(map[Path]Value{At("poll"): String("1m30s")})

	got, err := Load[cacheIntervalConf](t.Context(), planeSource{p: p}, WithRegistry(reg))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if want := (cacheIntervalConf{Poll: cacheInterval(90 * time.Second)}); got != want {
		t.Errorf("the load gave %v, want %v", time.Duration(got.Poll), time.Duration(want.Poll))
	}

	if n := cachedSchemas(reg); n != 1 {
		t.Errorf("a Load left %d cache entries, want the one it retained", n)
	}
}
