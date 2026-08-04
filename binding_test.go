package ferry

import (
	"context"
	"errors"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Everything here goes through Bind, BindSink, Load, LoadOver, Dump and
// Compile, which is the one seam. The two planes this file adds are drivers,
// not reaches inside core: they exist because the minted set's lifetime is only
// observable through a driver that flattens, and the plane in walk_test.go does
// not flatten.

// The five signatures ADR-0012 publishes, pinned mechanically, the way
// entry_test.go pins the other three. Two functions, two types, three methods,
// and no widening any of them by accident.
var (
	_ func(Source, ...Option) (*Binding[walkConf], error)   = Bind[walkConf]
	_ func(Sink, ...Option) (*SinkBinding[walkConf], error) = BindSink[walkConf]

	_ func(*Binding[walkConf], context.Context) (walkConf, error)           = (*Binding[walkConf]).Load
	_ func(*Binding[walkConf], context.Context, walkConf) (walkConf, error) = (*Binding[walkConf]).LoadOver
	_ func(*SinkBinding[walkConf], context.Context, walkConf) error         = (*SinkBinding[walkConf]).Dump
)

// TestAVerbIsItsBindingWithTheHandleDropped is ADR-0012's implementation
// constraint asserted rather than described: the two spellings agree on the
// value and on the report, in both directions and on both paths.
func TestAVerbIsItsBindingWithTheHandleDropped(t *testing.T) {
	t.Parallel()

	t.Run("load over a plane that answers", boundLoadAgreesOverAnAnsweringPlane)
	t.Run("load over one that refuses", boundLoadAgreesOverARefusingPlane)
	t.Run("load over a seed", boundLoadOverAgrees)
	t.Run("load over a seed that fails", boundLoadOverAgreesOnFailure)
	t.Run("dump", boundDumpAgrees)
}

// Each of these mints its own plane per call, because two spellings compared
// against one destination is the defect a fresh destination exists to catch.
func boundLoadAgreesOverAnAnsweringPlane(t *testing.T) {
	t.Parallel()
	mustAgreeWithABinding(t, answering)
}

func boundLoadAgreesOverARefusingPlane(t *testing.T) {
	t.Parallel()
	mustAgreeWithABinding(t, refusing)
}

func mustAgreeWithABinding(t *testing.T, mint func() *plane) {
	t.Helper()

	direct, first := Load[walkConf](t.Context(), planeSource{p: mint()})

	b, err := Bind[walkConf](planeSource{p: mint()})
	if err != nil {
		t.Fatalf("bind: %+v", err)
	}

	held, second := b.Load(t.Context())

	if direct != held {
		t.Errorf("Load gave %+v and a binding gave %+v", direct, held)
	}

	if reportOf(first) != reportOf(second) {
		t.Errorf("Load reported\n\t%v\nand a binding reported\n\t%v", first, second)
	}
}

func boundLoadOverAgrees(t *testing.T) {
	t.Parallel()

	// The plane holds one address, so what the seed carried and what the plane
	// answered are both visible in the result.
	remaining := map[Path]Value{At("region"): String("us-east-1")}

	direct, err := LoadOver(t.Context(), filled(), planeSource{p: newPlane(remaining)})
	if err != nil {
		t.Fatalf("load over: %+v", err)
	}

	b, err := Bind[walkConf](planeSource{p: newPlane(remaining)})
	if err != nil {
		t.Fatalf("bind: %+v", err)
	}

	held, err := b.LoadOver(t.Context(), filled())
	if err != nil {
		t.Fatalf("load over a binding: %+v", err)
	}

	if direct != held {
		t.Errorf("LoadOver gave %+v and a binding gave %+v", direct, held)
	}
}

func boundLoadOverAgreesOnFailure(t *testing.T) {
	t.Parallel()

	seed := filled()

	b, err := Bind[walkConf](planeSource{p: refusing()})
	if err != nil {
		t.Fatalf("bind: %+v", err)
	}

	held, failed := b.LoadOver(t.Context(), seed)
	if failed == nil {
		t.Fatal("a refusing plane loaded clean")
	}

	if held != seed {
		t.Errorf("a failed load returned %+v, want the seed it was handed, %+v", held, seed)
	}

	_, direct := LoadOver(t.Context(), seed, planeSource{p: refusing()})
	if reportOf(direct) != reportOf(failed) {
		t.Errorf("LoadOver reported\n\t%v\nand a binding reported\n\t%v", direct, failed)
	}
}

func boundDumpAgrees(t *testing.T) {
	t.Parallel()

	direct, held := newPlane(map[Path]Value{}), newPlane(map[Path]Value{})

	if err := Dump(t.Context(), filled(), planeSink{p: direct}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	b, err := BindSink[walkConf](planeSink{p: held})
	if err != nil {
		t.Fatalf("bind sink: %+v", err)
	}

	if err = b.Dump(t.Context(), filled()); err != nil {
		t.Fatalf("dump through a binding: %+v", err)
	}

	if !maps.Equal(direct.values, held.values) {
		t.Errorf("Dump wrote %v and a binding wrote %v", direct.values, held.values)
	}
}

// counting is a Source that records how often each phase of ADR-0004's
// lifecycle ran. It is the only test here that proves the change did anything.
type counting struct {
	src   Source
	binds atomic.Int64
	opens atomic.Int64
}

func (c *counting) Bind(addrs *AddressSet) (OpenFunc, error) {
	c.binds.Add(1)

	open, err := c.src.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (Reader, error) {
		c.opens.Add(1)

		return open(ctx)
	}, nil
}

// TestABindingBindsOnce is the whole point of the change: the expensive half of
// the contract runs once per binding and the cheap half runs once per load.
func TestABindingBindsOnce(t *testing.T) {
	t.Parallel()

	const loads = 10

	src := &counting{src: planeSource{p: answering()}}

	b, err := Bind[walkConf](src)
	if err != nil {
		t.Fatalf("bind: %+v", err)
	}

	for range loads {
		if _, err = b.Load(t.Context()); err != nil {
			t.Fatalf("load: %+v", err)
		}
	}

	if got := src.binds.Load(); got != 1 {
		t.Errorf("the driver was bound %d times, want 1", got)
	}

	if got := src.opens.Load(); got != loads {
		t.Errorf("the plane was opened %d times, want %d", got, loads)
	}
}

// TestOneShotVerbsStillBindPerCall is the same counter read from the other end,
// so that the test above is measuring the binding rather than a change nobody
// made to the one-shot path.
func TestOneShotVerbsStillBindPerCall(t *testing.T) {
	t.Parallel()

	const loads = 3

	src := &counting{src: planeSource{p: answering()}}

	for range loads {
		if _, err := Load[walkConf](t.Context(), src); err != nil {
			t.Fatalf("load: %+v", err)
		}
	}

	if got := src.binds.Load(); got != loads {
		t.Errorf("Load bound the driver %d times, want one per call, %d", got, loads)
	}
}

// TestABindingRefusesANilPlaneRatherThanDereferencingIt is
// entry_test.go's nil-plane case at the constructor, which is where the two
// refusals now live.
func TestABindingRefusesANilPlaneRatherThanDereferencingIt(t *testing.T) {
	t.Parallel()

	src := withoutPanicking(t, func() error {
		_, err := Bind[walkConf](nil)

		return err
	})

	sink := withoutPanicking(t, func() error {
		_, err := BindSink[walkConf](nil)

		return err
	})

	mustRefuseANilPlane(t, src, "the source is nil")
	mustRefuseANilPlane(t, sink, "the sink is nil")

	if reportOf(src) == reportOf(sink) {
		t.Errorf("both halves report\n\t%+v\nand a caller cannot tell which one was nil", src)
	}
}

// TestABindRefusalNeverReachesThePlane holds Bind to the phase order the
// one-shot verbs are already held to: the compile runs first, so a call that
// cannot compile never touches a driver.
func TestABindRefusalNeverReachesThePlane(t *testing.T) {
	t.Parallel()

	t.Run("an Option that does not resolve", bindRefusesABadOption)
	t.Run("a type that does not compile", bindRefusesABadType)
}

func bindRefusesABadOption(t *testing.T) {
	t.Parallel()

	p := answering()

	_, err := Bind[walkConf](planeSource{p: p}, TagKey(""))
	mustBeClass(t, err, ErrSchema)

	if _, err = BindSink[walkConf](planeSink{p: p}, TagKey("")); !errors.Is(err, ErrSchema) {
		t.Errorf("BindSink under a bad Option reported %+v", err)
	}

	if p.bound != nil {
		t.Error("a plane was bound under an Option list that does not resolve")
	}
}

func bindRefusesABadType(t *testing.T) {
	t.Parallel()

	p := answering()

	_, err := Bind[twoEngines](planeSource{p: p})
	if compiled := Compile[twoEngines](); reportOf(err) != reportOf(compiled) {
		t.Errorf("Bind reported\n\t%+v\nand Compile reported\n\t%+v", err, compiled)
	}

	if p.bound != nil {
		t.Error("a plane was bound behind a schema that does not compile")
	}
}

// TestBindFreezesTheRegistryAndCompileStillDoesNot is ADR-0010's retention rule
// with a new caller and no new wording: a binding keeps its schema for its whole
// life, so it freezes.
func TestBindFreezesTheRegistryAndCompileStillDoesNot(t *testing.T) {
	t.Parallel()

	type conf struct {
		Poll pollInterval `ferry:"poll"`
	}

	reg := registryWith(t)

	if err := Compile[conf](WithRegistry(reg)); err != nil {
		t.Fatalf("compile: %+v", err)
	}

	if err := reg.Register(DurationLike[pollInterval]()); err != nil {
		t.Fatalf("a registration after a discarded compile was refused: %+v", err)
	}

	if _, err := Bind[conf](planeSource{p: answering()}, WithRegistry(reg)); err != nil {
		t.Fatalf("bind: %+v", err)
	}

	mustRefuse(t, reg.Register(DurationLike[lateInterval]()),
		"the registry is frozen", "before the first Load, Dump or Bind")
}

// TestBindSinkFreezesTheRegistryToo is the same rule on the write side, on a
// registry of its own because a freeze is not reversible.
func TestBindSinkFreezesTheRegistryToo(t *testing.T) {
	t.Parallel()

	type conf struct {
		Poll pollInterval `ferry:"poll"`
	}

	reg := registryWith(t)

	if _, err := BindSink[conf](planeSink{p: newPlane(map[Path]Value{})}, WithRegistry(reg)); err != nil {
		t.Fatalf("bind sink: %+v", err)
	}

	mustRefuse(t, reg.Register(DurationLike[lateInterval]()), "the registry is frozen")
}

// TestABindingOutlivesItsConstructor is the shape a handler writes: the binding
// is built somewhere else and loaded through here.
func TestABindingOutlivesItsConstructor(t *testing.T) {
	t.Parallel()

	got, err := boundAtStartup(t).Load(t.Context())
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if want := filled(); got != want {
		t.Errorf("loaded %+v, want %+v", got, want)
	}
}

func boundAtStartup(t *testing.T) *Binding[walkConf] {
	t.Helper()

	b, err := Bind[walkConf](planeSource{p: answering()})
	if err != nil {
		t.Fatalf("bind: %+v", err)
	}

	return b
}

// foldConf is a type with a dynamic tier, so that the addresses a value mints
// are reachable from the two tests below.
type foldConf struct {
	M map[string]int `ferry:"m"`
}

// flatJoin is what a flattening driver does: one plane key per address, segments
// joined by an underscore.
func flatJoin(addr Path, transform func(string) string) (string, error) {
	out := make([]string, 0, 4)

	for s := range addr.Segments() {
		if s.Text() == "" {
			return "", errors.New("an empty segment names nothing on this plane")
		}

		out = append(out, transform(s.Text()))
	}

	return strings.Join(out, "_"), nil
}

// foldKey folds a hyphen onto an underscore, which is the ordinary
// environment-variable transform and which makes /m/a-b and /m/a_b one plane
// key. Two map keys that differ only there are therefore a collision this plane
// has to have an opinion about.
func foldKey(addr Path) (string, error) {
	return flatJoin(addr, func(text string) string { return strings.ReplaceAll(text, "-", "_") })
}

// asIs is the same join with no transform at all.
func asIs(addr Path) (string, error) { return flatJoin(addr, func(text string) string { return text }) }

// foldingSink is a flat sink over ferry's own key helper. Every open gets a
// store of its own, so what one dump wrote is visible on its own and two dumps
// through one binding cannot be confused for each other.
type foldingSink struct {
	mu    sync.Mutex
	dumps []map[string]Value
}

func (f *foldingSink) Bind(addrs *AddressSet) (OpenWriterFunc, error) {
	keys, err := NewKeys(addrs, "folding", foldKey)
	if err != nil {
		return nil, err
	}

	return func(context.Context) (Writer, error) {
		w := foldingWriter{store: map[string]Value{}, key: keys.Open()}

		// Guarded, because a binding is opened from many goroutines and this is
		// the driver obligation ADR-0012 creates rather than a test detail.
		f.mu.Lock()
		defer f.mu.Unlock()

		f.dumps = append(f.dumps, w.store)

		return w, nil
	}, nil
}

func (f *foldingSink) written() []map[string]Value {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]map[string]Value(nil), f.dumps...)
}

type foldingWriter struct {
	store map[string]Value
	key   KeyFunc
}

func (w foldingWriter) Set(_ context.Context, addr Path, v Value) error {
	key, err := w.key(addr)
	if err != nil {
		return err
	}

	w.store[key] = v

	return nil
}

// TestOneSinkBindingDumpsTwoShapes is ADR-0012's amended key helper seen from
// the public API: the minted set belongs to the open, so two dumps through one
// binding are each injective and are not held injective against each other.
//
// Under a minted set retained on the binding the second dump refuses, naming an
// address no plane still holds.
func TestOneSinkBindingDumpsTwoShapes(t *testing.T) {
	t.Parallel()

	sink := &foldingSink{}

	b, err := BindSink[foldConf](sink)
	if err != nil {
		t.Fatalf("bind sink: %+v", err)
	}

	if err = b.Dump(t.Context(), foldConf{M: map[string]int{"a-b": 1}}); err != nil {
		t.Fatalf("the first dump: %+v", err)
	}

	if err = b.Dump(t.Context(), foldConf{M: map[string]int{"a_b": 2}}); err != nil {
		t.Fatalf("the second dump, whose one key renders to the key the first dump used: %+v", err)
	}

	dumps := sink.written()
	if len(dumps) != 2 {
		t.Fatalf("the sink was opened %d times, want one per dump", len(dumps))
	}

	for i, want := range []string{"1", "2"} {
		if got := dumps[i]["m_a_b"]; got != Number(want) {
			t.Errorf("dump %d wrote %#v at m_a_b, want %#v", i, got, Number(want))
		}
	}
}

// minting is a flat source over ferry's own key helper, counting every call to
// the key function so that what an open retains is observable.
type minting struct {
	store map[string]Value
	kids  []Path
	calls atomic.Int64
}

func (m *minting) Bind(addrs *AddressSet) (OpenFunc, error) {
	keys, err := NewKeys(addrs, "minting", m.key)
	if err != nil {
		return nil, err
	}

	return func(context.Context) (Reader, error) {
		return mintingReader{store: m.store, kids: m.kids, key: keys.Open()}, nil
	}, nil
}

func (m *minting) key(addr Path) (string, error) {
	m.calls.Add(1)

	return asIs(addr)
}

type mintingReader struct {
	store map[string]Value
	kids  []Path
	key   KeyFunc
}

func (r mintingReader) Get(_ context.Context, addr Path) (Value, error) {
	key, err := r.key(addr)
	if err != nil {
		return Value{}, err
	}

	return r.store[key], nil
}

func (r mintingReader) Children(_ context.Context, prefix Path) ([]Path, error) {
	if prefix != At("m") {
		return nil, nil
	}

	return append([]Path(nil), r.kids...), nil
}

func aMintingSource() *minting {
	return &minting{
		store: map[string]Value{"m_a-b": Number("1"), "m_c-d": Number("2")},
		kids:  []Path{At("m", "a-b"), At("m", "c-d")},
	}
}

// TestAHeldBindingRetainsNothingAcrossLoads is the read half of the same rule.
// The second load mints exactly what the first did, so nothing an open produced
// outlived it.
func TestAHeldBindingRetainsNothingAcrossLoads(t *testing.T) {
	t.Parallel()

	src := aMintingSource()

	b, err := Bind[foldConf](src)
	if err != nil {
		t.Fatalf("bind: %+v", err)
	}

	first := mintedByOneLoad(t, b, src)
	second := mintedByOneLoad(t, b, src)

	if first == 0 {
		t.Fatal("the first load minted nothing, so this test asserts nothing")
	}

	if first != second {
		t.Errorf("the first load asked the key function %d times and the second asked it %d", first, second)
	}
}

func mintedByOneLoad(t *testing.T, b *Binding[foldConf], src *minting) int64 {
	t.Helper()

	before := src.calls.Load()

	got, err := b.Load(t.Context())
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if len(got.M) != 2 {
		t.Fatalf("loaded %v, want both entries", got.M)
	}

	return src.calls.Load() - before
}

// TestABindingIsSafeFromManyGoroutines is the promise the doc comment makes,
// and it asserts the binding rather than the walk: every goroutine runs a whole
// load or a whole dump of its own, and none of them runs one concurrently with
// itself.
func TestABindingIsSafeFromManyGoroutines(t *testing.T) {
	t.Parallel()

	t.Run("load", concurrentLoadsThroughOneBinding)
	t.Run("dump", concurrentDumpsThroughOneBinding)
}

const goroutines = 64

func concurrentLoadsThroughOneBinding(t *testing.T) {
	t.Parallel()

	b, err := Bind[foldConf](aMintingSource())
	if err != nil {
		t.Fatalf("bind: %+v", err)
	}

	var wg sync.WaitGroup

	for range goroutines {
		wg.Go(func() { mustLoadTheWholeMap(t, b) })
	}

	wg.Wait()
}

func mustLoadTheWholeMap(t *testing.T, b *Binding[foldConf]) {
	t.Helper()

	got, err := b.Load(t.Context())
	if err != nil {
		t.Errorf("load: %+v", err)

		return
	}

	if want := (map[string]int{"a-b": 1, "c-d": 2}); !maps.Equal(got.M, want) {
		t.Errorf("loaded %v, want %v", got.M, want)
	}
}

func concurrentDumpsThroughOneBinding(t *testing.T) {
	t.Parallel()

	sink := &foldingSink{}

	b, err := BindSink[foldConf](sink)
	if err != nil {
		t.Fatalf("bind sink: %+v", err)
	}

	var wg sync.WaitGroup

	for i := range goroutines {
		wg.Go(func() {
			if err := b.Dump(t.Context(), foldConf{M: map[string]int{"k": i}}); err != nil {
				t.Errorf("dump: %+v", err)
			}
		})
	}

	wg.Wait()

	if got := len(sink.written()); got != goroutines {
		t.Errorf("the sink was opened %d times, want one per dump, %d", got, goroutines)
	}
}

// benchPlane is a source with no bookkeeping at all, because a plane that
// records what it was asked grows a slice per Get and would be most of what the
// benchmarks below measured.
type benchPlane struct{ values map[Path]Value }

func (p benchPlane) Bind(*AddressSet) (OpenFunc, error) {
	return func(context.Context) (Reader, error) { return p, nil }, nil
}

func (p benchPlane) Get(_ context.Context, addr Path) (Value, error) { return p.values[addr], nil }

// BenchmarkLoad and BenchmarkHeldLoad are the same load over the same plane,
// bound per call and bound once. No figure from either goes into a doc comment,
// a README or a guide; the published numbers come from the pipeline.
func BenchmarkLoad(b *testing.B) {
	src := benchPlane{values: contents()}

	b.ReportAllocs()

	for b.Loop() {
		if _, err := Load[walkConf](b.Context(), src); err != nil {
			b.Fatalf("load: %+v", err)
		}
	}
}

func BenchmarkHeldLoad(b *testing.B) {
	held, err := Bind[walkConf](benchPlane{values: contents()})
	if err != nil {
		b.Fatalf("bind: %+v", err)
	}

	b.ReportAllocs()

	for b.Loop() {
		if _, err := held.Load(b.Context()); err != nil {
			b.Fatalf("load: %+v", err)
		}
	}
}
