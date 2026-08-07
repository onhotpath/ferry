package ferry

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// Every assertion in this file goes through Load, LoadOver, Dump, Compile and
// Bind. Nothing reaches the scheduler, the semaphore or the walker directly:
// the gate is asserted by what a plane observed about overlap, and equivalence
// by what two whole loads produced.
//
// The plane below is a second test-local driver rather than a use of the one in
// walk_test.go, which records every address it was asked in a plain slice and
// would race the moment a walk overlapped. This one is safe to call from many
// goroutines, which is the obligation ferry.Concurrent puts on an instance that
// declares the capability, and it is the fixture as well as the subject.

// meterWait is how long a metered Get holds itself open waiting for the overlap
// a case expects. It is only ever reached when the fanout under test did not
// happen, and the case then fails on the peak it measured, so the wait is a
// bound on a broken run rather than a delay on a working one.
const meterWait = 2 * time.Second

// concPlane is a read-only plane over one immutable map that meters how many
// Gets were ever open at once.
//
// The rendezvous is what makes a peak assertion deterministic instead of a race
// against a sleep: every Get holds itself open until as many Gets are open as
// the case expects, and once that many ever have been, no Get waits again. A
// walk that cannot reach the number measures its own real peak and fails on it.
type concPlane struct {
	values map[Path]Value
	fail   map[Path]error

	// declares is whether the open instance implements Concurrent at all, and
	// tolerance is what it returns when it does.
	declares  bool
	tolerance int

	// want is the overlap the rendezvous waits for, reached is closed the first
	// time it is seen, and shut makes that closing happen once.
	want    int
	reached chan struct{}
	shut    sync.Once

	mu       sync.Mutex
	inflight int
	peak     int
	closes   int
	budget   int
}

// newConcPlane builds a plane over values whose instance declares nothing, so a
// case that wants the capability turns it on.
func newConcPlane(values map[Path]Value) *concPlane {
	return &concPlane{
		values:  values,
		fail:    map[Path]error{},
		want:    1,
		reached: make(chan struct{}),
	}
}

// concurrent turns the capability on, at the tolerance the instance declares.
// A tolerance of zero or less is the documented "no bound of my own".
func (p *concPlane) concurrent(tolerance, want int) *concPlane {
	p.declares, p.tolerance, p.want = true, tolerance, want

	return p
}

// refuses makes one address fail, which is how a case gets two failures out of
// one walk.
func (p *concPlane) refuses(at Path, err error) *concPlane {
	p.fail[at] = err

	return p
}

func (p *concPlane) Bind(*AddressSet) (OpenFunc, error) {
	return func(ctx context.Context) (Reader, error) {
		p.mu.Lock()
		p.budget = ConcurrencyBudget(ctx)
		p.mu.Unlock()

		if p.declares {
			return tolerantReader{concReader{p: p}}, nil
		}

		return concReader{p: p}, nil
	}, nil
}

// observed is the peak overlap, how many times the instance was released, and
// the budget the open read off its context.
func (p *concPlane) observed() (peak, closes, budget int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.peak, p.closes, p.budget
}

// enter records one more open call and holds it there until the case's expected
// overlap has been reached once.
func (p *concPlane) enter() {
	p.mu.Lock()
	p.inflight++

	if p.inflight > p.peak {
		p.peak = p.inflight
	}

	enough := p.inflight >= p.want
	p.mu.Unlock()

	if enough {
		p.shut.Do(func() { close(p.reached) })
	}

	select {
	case <-p.reached:
	case <-time.After(meterWait):
	}
}

func (p *concPlane) leave() {
	p.mu.Lock()
	p.inflight--
	p.mu.Unlock()
}

// concReader is the open instance without the capability: an ordinary reader
// that happens to be safe to call from many goroutines.
type concReader struct{ p *concPlane }

func (r concReader) Get(_ context.Context, addr LeafAddr) (Value, error) {
	r.p.enter()
	defer r.p.leave()

	at := addr.Path()
	if err := r.p.fail[at]; err != nil {
		return Value{}, err
	}

	return r.p.values[at], nil
}

// Close is here so that the deferred release can be asserted under fanout: it
// runs on the caller's goroutine after the scheduler has waited for every task.
func (r concReader) Close() error {
	r.p.mu.Lock()
	defer r.p.mu.Unlock()

	r.p.closes++

	return nil
}

// tolerantReader is the same instance declaring the capability.
type tolerantReader struct{ concReader }

func (t tolerantReader) MaxConcurrent() int { return t.p.tolerance }

// concLeaves is eight leaves under one container, which is the flat shape the
// gate is stated over.
type concLeaves struct {
	A string `ferry:"a"`
	B string `ferry:"b"`
	C string `ferry:"c"`
	D string `ferry:"d"`
	E string `ferry:"e"`
	F string `ferry:"f"`
	G string `ferry:"g"`
	H string `ferry:"h"`
}

func concLeafValues() map[Path]Value {
	out := map[Path]Value{}
	for _, name := range strings.Split("a b c d e f g h", " ") {
		out[At(name)] = String(name)
	}

	return out
}

// TestFanoutHappensOnlyWhereBothPartiesSaidYes is the gate, whole: the caller's
// Option and the instance's declaration both have to be there, and the overlap
// never passes the smaller of the two numbers.
func TestFanoutHappensOnlyWhereBothPartiesSaidYes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		plane *concPlane
		opts  []Option
		peak  int
	}{
		{"no capability, so the Option buys nothing", newConcPlane(concLeafValues()),
			[]Option{MaxConcurrency(4)}, 1},
		{"capability but no Option, so nothing was granted",
			newConcPlane(concLeafValues()).concurrent(0, 1), nil, 1},
		{"a budget of one is the serial walk spelled out",
			newConcPlane(concLeafValues()).concurrent(0, 1), []Option{MaxConcurrency(1)}, 1},
		{"both said yes, and the instance imposes no bound of its own",
			newConcPlane(concLeafValues()).concurrent(0, 4), []Option{MaxConcurrency(4)}, 4},
		{"the instance's tolerance is the smaller number",
			newConcPlane(concLeafValues()).concurrent(2, 2), []Option{MaxConcurrency(8)}, 2},
		{"the caller's budget is the smaller number",
			newConcPlane(concLeafValues()).concurrent(8, 3), []Option{MaxConcurrency(3)}, 3},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assertPeak(t, c.plane, c.opts, c.peak)
		})
	}
}

// assertPeak loads the flat fixture and holds the plane to the overlap the case
// expects, exactly: a walk that overlapped less did not spend what it was
// granted, and one that overlapped more spent what it was not.
func assertPeak(t *testing.T, p *concPlane, opts []Option, want int) {
	t.Helper()

	got, err := Load[concLeaves](t.Context(), p, opts...)
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got.A != "a" || got.H != "h" {
		t.Errorf("loaded %+v, want every leaf filled", got)
	}

	if peak, _, _ := p.observed(); peak != want {
		t.Errorf("the plane saw %d overlapping calls, want %d", peak, want)
	}
}

// TestTheBudgetRidesTheOpensContext is ADR-0019's one budget reaching both
// layers: the number core is about to spend is on the context the driver's own
// open runs under, and a caller who set none reads the serial one.
func TestTheBudgetRidesTheOpensContext(t *testing.T) {
	t.Parallel()

	if got := ConcurrencyBudget(context.Background()); got != 1 {
		t.Errorf("a context nobody budgeted reports %d, want 1", got)
	}

	p := newConcPlane(concLeafValues())
	if _, err := Load[concLeaves](t.Context(), p, MaxConcurrency(6)); err != nil {
		t.Fatalf("load: %+v", err)
	}

	if _, _, budget := p.observed(); budget != 6 {
		t.Errorf("the open read a budget of %d, want 6", budget)
	}

	bare := newConcPlane(concLeafValues())
	if _, err := Load[concLeaves](t.Context(), bare); err != nil {
		t.Fatalf("load: %+v", err)
	}

	if _, _, budget := bare.observed(); budget != 1 {
		t.Errorf("an unbudgeted open read %d, want 1", budget)
	}
}

// TestTheBudgetReachesASinksOpen is the same number on the write side. Nothing
// there fans out, and the sink is the layer that can still spend it at Commit.
func TestTheBudgetReachesASinksOpen(t *testing.T) {
	t.Parallel()

	var budget int

	sink := budgetSink{
		p:    newPlane(map[Path]Value{}),
		read: func(ctx context.Context) { budget = ConcurrencyBudget(ctx) },
	}

	if err := Dump(t.Context(), concLeaves{A: "a"}, sink, MaxConcurrency(5)); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if budget != 5 {
		t.Errorf("the sink's open read a budget of %d, want 5", budget)
	}
}

// budgetSink is planeSink with a hook on the open, which is the one place a
// dump has to read the budget from.
type budgetSink struct {
	p    *plane
	read func(ctx context.Context)
}

func (s budgetSink) Bind(*AddressSet) (OpenWriterFunc, error) {
	return func(ctx context.Context) (Writer, error) {
		s.read(ctx)

		return s.p.writer(), nil
	}, nil
}

// concNested is four levels of four members, which is the shape a per-container
// semaphore would spend the budget cubed on and a parent holding a slot while
// it waits on its children would deadlock at.
type concNested struct {
	A concLevel2 `ferry:"a"`
	B concLevel2 `ferry:"b"`
	C concLevel2 `ferry:"c"`
	D concLevel2 `ferry:"d"`
}

type concLevel2 struct {
	A concLevel3 `ferry:"a"`
	B concLevel3 `ferry:"b"`
	C concLevel3 `ferry:"c"`
	D concLevel3 `ferry:"d"`
}

type concLevel3 struct {
	A string `ferry:"a"`
	B string `ferry:"b"`
	C string `ferry:"c"`
	D string `ferry:"d"`
}

// concNestedValues fills every one of the 64 addresses concNested names.
func concNestedValues() map[Path]Value {
	members := []string{"a", "b", "c", "d"}
	values := map[Path]Value{}

	for _, one := range members {
		for _, two := range members {
			for _, three := range members {
				values[At(one, two, three)] = String(one + two + three)
			}
		}
	}

	return values
}

// TestADeepWalkCompletesUnderASmallBudget is the recursion the prototype could
// not vouch for: one semaphore for the whole walk, a budget smaller than the
// depth, and every leaf still read.
func TestADeepWalkCompletesUnderASmallBudget(t *testing.T) {
	t.Parallel()

	p := newConcPlane(concNestedValues()).concurrent(0, 2)

	got, err := Load[concNested](t.Context(), p, MaxConcurrency(2))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got.A.A.A != "aaa" || got.D.D.D != "ddd" {
		t.Errorf("a deep walk loaded %+v", got)
	}

	if peak, _, _ := p.observed(); peak != 2 {
		t.Errorf("a four-level walk under a budget of two overlapped %d calls, want 2", peak)
	}
}

// concEquiv is the equivalence fixture: leaves, a nested struct, a nullable
// section, an array, a default and a required.
type concEquiv struct {
	Name  string    `ferry:"name,required"`
	Count int       `ferry:"count"`
	DB    concDB    `ferry:"db"`
	Opt   *concOpt  `ferry:"opt"`
	Tri   [3]string `ferry:"tri"`
}

type concDB struct {
	Host string `ferry:"host,required"`
	Port int    `ferry:"port,default=5432"`
}

// concOpt is the nullable section, and nothing under it is required: a pointer
// the plane is silent about stays nil, and a required address beneath one would
// be a failure rather than an absence.
type concOpt struct {
	Region string `ferry:"region"`
}

func concEquivValues() map[Path]Value {
	return map[Path]Value{
		At("name"):        String("svc"),
		At("count"):       Number("7"),
		At("db", "host"):  String("db1"),
		At("tri").Elem(0): String("x"),
		At("tri").Elem(1): String("y"),
		At("tri").Elem(2): String("z"),
	}
}

// budgets is the ladder every equivalence case is run over, so that a defect
// that only shows at one width is not run past.
var budgets = []int{2, 3, 4, 8}

// TestAConcurrentLoadIsTheSerialLoad is the gate ADR-0019 sets on the whole
// feature: a concurrent run produces the destination a serial run produces.
//
// Every subtest builds its own plane and its own destination, because a shared
// destination is what makes a second walk that never ran look like one that
// agreed.
func TestAConcurrentLoadIsTheSerialLoad(t *testing.T) {
	t.Parallel()

	want, err := Load[concEquiv](t.Context(), newConcPlane(concEquivValues()))
	if err != nil {
		t.Fatalf("the serial load failed: %+v", err)
	}

	for _, n := range budgets {
		t.Run(budgetName(n), func(t *testing.T) {
			t.Parallel()
			assertSameValue(t, n, want)
		})
	}
}

// assertSameValue runs one budget over its own fresh plane and its own fresh
// destination, and holds what came back to what the serial walk produced.
func assertSameValue(t *testing.T, n int, want concEquiv) {
	t.Helper()

	p := newConcPlane(concEquivValues()).concurrent(0, min(n, 2))

	got, err := Load[concEquiv](t.Context(), p, MaxConcurrency(n))
	if err != nil {
		t.Fatalf("the concurrent load failed: %+v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("a budget of %d loaded %+v, want the serial %+v", n, got, want)
	}
}

// concFailing is the same fixture with three independent failures under it: a
// leaf the plane refuses, a leaf whose text the type cannot take, and a
// required address the plane does not hold.
func concFailing() *concPlane {
	values := concEquivValues()
	delete(values, At("db", "host"))
	values[At("count")] = String("not a number")

	return newConcPlane(values).refuses(At("tri").Elem(1), errors.New("the plane refused this one"))
}

// TestAConcurrentReportIsTheSerialReport is the other half of the gate: three
// failures out of one walk arrive in one flat report, in the same order and
// with the same text, however wide the walk ran.
func TestAConcurrentReportIsTheSerialReport(t *testing.T) {
	t.Parallel()

	_, serialErr := Load[concEquiv](t.Context(), concFailing())
	if serialErr == nil {
		t.Fatal("the serial load of the failing fixture succeeded")
	}

	if got := len(Elements(serialErr)); got != 3 {
		t.Fatalf("the serial load reported %d failures, want 3: %+v", got, serialErr)
	}

	for _, n := range budgets {
		t.Run(budgetName(n), func(t *testing.T) {
			t.Parallel()

			_, err := Load[concEquiv](t.Context(), concFailing().concurrent(0, min(n, 2)), MaxConcurrency(n))
			if err == nil {
				t.Fatal("the concurrent load of the failing fixture succeeded")
			}

			assertSameReport(t, err, serialErr)
		})
	}
}

// assertSameReport holds one report to another, text and addresses alike.
func assertSameReport(t *testing.T, got, want error) {
	t.Helper()

	if got.Error() != want.Error() {
		t.Errorf("the concurrent report reads\n\t%s\nand the serial one reads\n\t%s", got, want)
	}

	gotAt, wantAt := reportAddresses(got), reportAddresses(want)
	if !reflect.DeepEqual(gotAt, wantAt) {
		t.Errorf("the concurrent report names %v, want %v", gotAt, wantAt)
	}
}

// reportAddresses is the report's members in the order it holds them, which is
// the order that has to be the same and never the order the members finished
// in.
func reportAddresses(err error) []Path {
	out := []Path{}

	for _, e := range Elements(err) {
		if located, ok := errors.AsType[*Error](e); ok {
			out = append(out, located.Address())
		}
	}

	return out
}

func budgetName(n int) string {
	return "MaxConcurrency(" + string(rune('0'+n)) + ")"
}

// TestAPanicUnderFanoutStaysAnAddressedError is the fence under overlap: a
// codec that panics on four goroutines produces four addressed failures, its
// siblings finish, and the instance is still released.
func TestAPanicUnderFanoutStaysAnAddressedError(t *testing.T) {
	t.Parallel()

	p := newConcPlane(map[Path]Value{
		At("a"): String(theFuse),
		At("b"): String(theFuse),
		At("c"): String(theFuse),
		At("d"): String("fine"),
	}).concurrent(0, 2)

	_, err := Load[fusedFour](t.Context(), p, MaxConcurrency(4), WithRegistry(registryWith(t, fuseCodec())))
	if err == nil {
		t.Fatal("a walk of three panicking codecs succeeded")
	}

	if got := len(Elements(err)); got != 3 {
		t.Fatalf("a fanned-out walk reported %d failures, want 3: %+v", got, err)
	}

	if !errors.Is(err, ErrPanic) {
		t.Errorf("the report does not match ErrPanic: %+v", err)
	}

	if _, closes, _ := p.observed(); closes != 1 {
		t.Errorf("the instance was released %d times, want 1", closes)
	}
}

// fusedFour is three addresses whose codec panics and one that does not, so a
// case can assert that a sibling of a panicking task still ran.
type fusedFour struct {
	A fuse   `ferry:"a"`
	B fuse   `ferry:"b"`
	C fuse   `ferry:"c"`
	D string `ferry:"d"`
}

// TestMaxConcurrencyIsRefusedWhereItCannotMeanAnything is the Option's own
// surface: a budget below one, a second one, and a nil member of the list.
func TestMaxConcurrencyIsRefusedWhereItCannotMeanAnything(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		opts []Option
		says string
	}{
		{"zero", []Option{MaxConcurrency(0)}, "the smallest one is 1"},
		{"negative", []Option{MaxConcurrency(-3)}, "the smallest one is 1"},
		{"twice", []Option{MaxConcurrency(2), MaxConcurrency(4)}, "given twice"},
		{"nil beside it", []Option{MaxConcurrency(2), nil}, "ferry.MaxConcurrency"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assertRefused(t, c.opts, c.says)
		})
	}
}

// assertRefused holds one Option list to the refusal it earns, which Compile
// reaches without a plane because an Option list that is wrong is wrong before
// any type is looked at.
func assertRefused(t *testing.T, opts []Option, says string) {
	t.Helper()

	err := Compile[concLeaves](opts...)
	if err == nil {
		t.Fatal("the Option list was accepted")
	}

	if !strings.Contains(err.Error(), says) {
		t.Errorf("the refusal reads %q, and does not say %q", err, says)
	}

	if !errors.Is(err, ErrSchema) {
		t.Errorf("the refusal does not match ErrSchema: %+v", err)
	}
}

// TestTheBudgetIsNotPartOfTheSchemaKey is ADR-0010's cache rule held to: the
// budget changes how a load runs and never what a type compiles to, so two
// budgets over one type are one compile.
func TestTheBudgetIsNotPartOfTheSchemaKey(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()

	for _, opts := range [][]Option{
		{WithRegistry(reg)},
		{WithRegistry(reg), MaxConcurrency(2)},
		{WithRegistry(reg), MaxConcurrency(9)},
	} {
		if _, err := Load[concLeaves](t.Context(), newConcPlane(concLeafValues()), opts...); err != nil {
			t.Fatalf("load: %+v", err)
		}
	}

	if got := cachedSchemas(reg); got != 1 {
		t.Errorf("three budgets over one type made %d cache entries, want 1", got)
	}
}

// TestABudgetSurvivesAHeldBinding is the binding's half: the Option is resolved
// once, at Bind, and every load through the handle spends it.
func TestABudgetSurvivesAHeldBinding(t *testing.T) {
	t.Parallel()

	p := newConcPlane(concLeafValues()).concurrent(0, 4)

	b, err := Bind[concLeaves](p, MaxConcurrency(4))
	if err != nil {
		t.Fatalf("bind: %+v", err)
	}

	if _, err := b.Load(t.Context()); err != nil {
		t.Fatalf("load: %+v", err)
	}

	if peak, closes, _ := p.observed(); peak != 4 || closes != 1 {
		t.Errorf("a load through a held binding overlapped %d and released %d times, want 4 and 1", peak, closes)
	}
}
