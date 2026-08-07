package ferry

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"
)

// Every assertion in this file and in entry_test.go goes through Load, LoadOver
// and Dump. Nothing reaches the walker, the scheduler or the compiled node tree
// directly: a walk rule is asserted through what a plane was asked and what came
// back, and an address-set rule through what a driver's Bind was handed.
//
// The plane below is a test-local driver rather than an addition to ferrytest.
// ferrytest's exported surface is ADR-0014's list, RoundTrip and Record are a
// later ticket's, and none of the three things these tests need of a plane -
// recording what it was asked, refusing one named address, and declaring
// whether it has a lifecycle at all - is something ferrytest publishes today.

// plane is a whole driver over one map: both directions, a hook per address so
// a refusal can be injected at exactly one of them, and a record of everything
// it was asked.
type plane struct {
	values map[Path]Value
	// fail is the refusal per address, on whichever side is walking.
	fail map[Path]error

	// bound is the address set Bind was handed, which is the compiler's own set
	// arriving through the driver contract.
	bound *AddressSet
	got   []Path
	set   []Path

	// probed and ensured are the container side of the contract: what Probe was
	// asked about and what Ensure was told, kept apart from got and set because
	// a container address is never handed a Value (ADR-0016).
	probed  []Path
	ensured []Path
	// forgot is every composite Unset was told to let go of, in order.
	forgot []Path
	// presence is what Ensure wrote at a container address, which is what a
	// later Probe of the same plane reads back.
	presence map[Path]Presence

	bindErr, openErr, closeErr, commitErr error

	// lifecycle decides whether the reader and the writer implement Committer
	// and Releaser at all, because both are discovered by assertion and a plane
	// with nothing to release implements neither.
	lifecycle bool
	closes    int
	commits   int

	// onGet runs before the value is answered, which is how a cancellation is
	// made to arrive in the middle of a walk. onSet is its twin on the write
	// side, and both are also how a panic is made to arrive from below the
	// boundary and outside the codec fence.
	onGet func()
	onSet func()
}

func newPlane(values map[Path]Value) *plane {
	return &plane{
		values:    values,
		fail:      map[Path]error{},
		presence:  map[Path]Presence{},
		lifecycle: true,
	}
}

func (p *plane) Get(_ context.Context, addr LeafAddr) (Value, error) {
	at := addr.Path()
	p.got = append(p.got, at)

	if p.onGet != nil {
		p.onGet()
	}

	// Absent beside a non-nil error is the shape that makes ADR-0004's fourth
	// conformance case possible to fail: a core that reads the value and drops
	// the error loads a total outage as an all-zero struct with a nil error.
	if err := p.fail[at]; err != nil {
		return Value{}, err
	}

	return p.values[at], nil
}

func (p *plane) Set(_ context.Context, addr LeafAddr, v Value) error {
	at := addr.Path()
	p.set = append(p.set, at)

	if p.onSet != nil {
		p.onSet()
	}

	if err := p.fail[at]; err != nil {
		return err
	}

	p.values[at] = v

	return nil
}

// Probe is the container half of the read side. It answers out of the same map
// the leaves are read from, so a fixture that stores a Null at a container
// address still means the plane holds a null there (ADR-0016).
func (p *plane) Probe(_ context.Context, addr Container) (SectionInfo, error) {
	at := addr.Path()
	p.probed = append(p.probed, at)

	if p.onGet != nil {
		p.onGet()
	}

	if err := p.fail[at]; err != nil {
		return SectionInfo{}, err
	}

	if held, wrote := p.presence[at]; wrote {
		return infoFor(held), nil
	}

	return infoFor(presenceOf(p.values[at])), nil
}

// Ensure is the container half of the write side: a null at a nil pointer or an
// empty composite, and a present at a realised section that wrote nothing.
func (p *plane) Ensure(_ context.Context, addr Container, held Presence) error {
	at := addr.Path()
	p.ensured = append(p.ensured, at)

	if p.onSet != nil {
		p.onSet()
	}

	if err := p.fail[at]; err != nil {
		return err
	}

	p.presence[at] = held

	return nil
}

// Unset is the retraction half of the write side: the plane lets go of a
// composite's address and everything under it, which is what makes the writes
// that follow a replacement rather than an addition.
//
// Every plane in these tests can forget an address, because both shipped sinks
// can and because a schema holding a composite is refused at the open against
// one that cannot. The sink that cannot is built where that refusal is
// asserted, in unset_test.go.
func (p *plane) Unset(_ context.Context, addr CompositeAddr) error {
	at := addr.Path()
	p.forgot = append(p.forgot, at)

	if err := p.fail[at]; err != nil {
		return err
	}

	for held := range p.values {
		if at.isPrefixOf(held) {
			delete(p.values, held)
		}
	}

	for held := range p.presence {
		if at.isPrefixOf(held) {
			delete(p.presence, held)
		}
	}

	return nil
}

// presenceOf reads a stored Value as what a probe of that address reports, so
// one map serves both halves of the contract.
func presenceOf(v Value) Presence {
	switch v.Kind() {
	case KindAbsent:
		return PresenceAbsent
	case KindNull:
		return PresenceNull
	default:
		return PresencePresent
	}
}

func infoFor(held Presence) SectionInfo {
	switch held {
	case PresenceNull:
		return SectionNull
	case PresencePresent:
		return SectionPresent
	default:
		return SectionAbsent
	}
}

func (p *plane) reader() Reader {
	if p.lifecycle {
		return &staged{plane: p}
	}

	return p
}

func (p *plane) writer() Writer {
	if p.lifecycle {
		return &staged{plane: p}
	}

	return p
}

// staged is the same plane with the two optional interfaces on it, which is how
// a driver that holds a resource or stages its writes differs from one that
// does not.
type staged struct{ *plane }

func (s *staged) Close() error {
	s.closes++

	return s.closeErr
}

func (s *staged) Commit(context.Context) error {
	s.commits++

	return s.commitErr
}

// planeSource and planeSink are two types over one set of contents, which is the
// cost ADR-0004 states for making a read-only plane a compile-time refusal.
type planeSource struct{ p *plane }

func (s planeSource) Bind(addrs *AddressSet) (OpenFunc, error) {
	s.p.bound = addrs

	if s.p.bindErr != nil {
		return nil, s.p.bindErr
	}

	return func(context.Context) (Reader, error) {
		if s.p.openErr != nil {
			return nil, s.p.openErr
		}

		return s.p.reader(), nil
	}, nil
}

type planeSink struct{ p *plane }

func (s planeSink) Bind(addrs *AddressSet) (OpenWriterFunc, error) {
	s.p.bound = addrs

	if s.p.bindErr != nil {
		return nil, s.p.bindErr
	}

	return func(context.Context) (Writer, error) {
		if s.p.openErr != nil {
			return nil, s.p.openErr
		}

		return s.p.writer(), nil
	}, nil
}

// walkTop is embedded with no tag, so its fields are promoted to the parent
// address: /name and /env, never /walkTop/name.
type walkTop struct {
	Name string `ferry:"name"`
	Env  string `ferry:"env"`
}

// walkDB is a nested struct under a name of its own, which is the whole of what
// prefixing is under a structured address.
type walkDB struct {
	Host string `ferry:"host"`
	Port string `ferry:"port"`
}

// walkConf is this ticket's shape: string leaves, a nested struct, and a
// promoted embedded one.
type walkConf struct {
	walkTop
	Region string `ferry:"region"`
	DB     walkDB `ferry:"db"`
}

func filled() walkConf {
	return walkConf{
		walkTop: walkTop{Name: "svc", Env: "prod"},
		Region:  "eu-west-1",
		DB:      walkDB{Host: "db1", Port: "5432"},
	}
}

// contents is a fresh copy per call, because a plane shared across subtests is
// the same defect as a destination shared across them.
func contents() map[Path]Value {
	return map[Path]Value{
		At("name"):       String("svc"),
		At("env"):        String("prod"),
		At("region"):     String("eu-west-1"),
		At("db", "host"): String("db1"),
		At("db", "port"): String("5432"),
	}
}

// TestTheWalkVisitsTheCompilersAddressSet is axis 1 of ADR-0010, asserted from
// the two ends that can disagree: what Bind was handed is the compiler's set,
// and what Get and Set were called with is the walk's.
//
// The fixture is a promoted embedded struct because that is the rule that
// actually diverged in a real prototype: the compiler promoted and the walk did
// not, so the schema promised /name, the walk looked at /Common/name, and the
// load returned a nil error and a zero field.
func TestTheWalkVisitsTheCompilersAddressSet(t *testing.T) {
	t.Parallel()

	t.Run("load", loadVisitsTheAddressSet)
	t.Run("dump", dumpVisitsTheAddressSet)
}

func loadVisitsTheAddressSet(t *testing.T) {
	t.Parallel()

	p := newPlane(contents())

	if _, err := Load[walkConf](t.Context(), planeSource{p: p}); err != nil {
		t.Fatalf("load: %+v", err)
	}

	mustBeOneList(t, p.bound, p.got)
}

func dumpVisitsTheAddressSet(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})

	if err := Dump(t.Context(), filled(), planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	mustBeOneList(t, p.bound, p.set)
}

// mustBeOneList is axis 1 stated at the leaves, which is where the two lists
// have to agree exactly: every address handed a Value is one the compiler
// determined as a leaf, and every leaf the compiler determined was visited.
//
// The set is wider than the visit, and that is the address model rather than a
// gap: it also names the container addresses, and a plain nested struct is a
// section nothing asks about, because its members come from the type and the
// walk goes straight into them (ADR-0016).
func mustBeOneList(t *testing.T, bound *AddressSet, visited []Path) {
	t.Helper()

	if !bound.Has(leafOf(At("name"))) || bound.Has(leafOf(At("walkTop", "Name"))) {
		t.Errorf("the address set promotes nothing: %v", kinded(bound))
	}

	if !bound.Has(sectionOf(At("db"))) {
		t.Errorf("the nested struct names no section: %v", kinded(bound))
	}

	got := slices.Clone(visited)
	slices.SortFunc(got, Path.Compare)

	if want := leavesOf(bound); !slices.Equal(got, want) {
		t.Errorf("the walk visited\n\t%v\nand the compiler's leaf addresses are\n\t%v", got, want)
	}
}

// leavesOf is the leaf addresses of a bound set, in the set's own order.
func leavesOf(a *AddressSet) []Path {
	var out []Path

	for m := range a.Seq() {
		if leaf, ok := m.(LeafAddr); ok {
			out = append(out, leaf.Path())
		}
	}

	return out
}

// describe names one member as its kind and its address, which is what a
// failure has to distinguish: /db as a section and /db as a composite render
// alike and are not the same address.
func describe(m Member) string {
	switch m.(type) {
	case LeafAddr:
		return "leaf " + m.String()
	case SectionAddr:
		return "section " + m.String()
	default:
		return "composite " + m.String()
	}
}

// TestTheWalkChecksTheContext is a placement rather than a policy, and both
// halves of it are observable: nothing is asked of a plane under a context that
// is already done, and a cancellation arriving mid-walk stops the walk entering
// any further node, container or leaf.
func TestTheWalkChecksTheContext(t *testing.T) {
	t.Parallel()

	t.Run("before the first address", theContextIsCheckedBeforeTheFirstNode)
	t.Run("and at every node entry after", theContextIsCheckedAtEveryNodeAfter)
}

func theContextIsCheckedBeforeTheFirstNode(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	p := newPlane(contents())

	got, err := Load[walkConf](ctx, planeSource{p: p})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("load returned %+v, %v, want a cancellation", got, err)
	}

	if len(p.got) != 0 {
		t.Errorf("the plane was asked for %v under a cancelled context", p.got)
	}
}

func theContextIsCheckedAtEveryNodeAfter(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := newPlane(contents())
	p.onGet = cancel

	_, err := Load[walkConf](ctx, planeSource{p: p})

	// One Get, then /env, /region and the /db container each refuse at their
	// own entry. The container is the half that matters: it reports once and
	// never descends, so the two leaves under it produce one element between
	// them and neither is read.
	if len(p.got) != 1 {
		t.Errorf("the walk asked for %v after the context ended, want only the first address", p.got)
	}

	if n := len(Elements(err)); n != 3 {
		t.Errorf("%+v holds %d elements, want 3", err, n)
	}
}

// TestTheSchedulerRunsTheWholeBatch is what the seam buys, observed through the
// plane: a batch of siblings is handed over per container and every task in it
// runs, so two refusals are two errors rather than the first one.
func TestTheSchedulerRunsTheWholeBatch(t *testing.T) {
	t.Parallel()

	refused := errors.New("no write ACL for this address")

	p := newPlane(map[Path]Value{})
	p.fail[At("name")] = refused
	p.fail[At("db", "host")] = refused

	err := Dump(t.Context(), filled(), planeSink{p: p})

	if n := len(p.set); n != 5 {
		t.Errorf("the walk attempted %d addresses, want all 5: %v", n, p.set)
	}

	if n := len(Elements(err)); n != 2 {
		t.Errorf("%+v holds %d elements, want 2", err, n)
	}

	if p.commits != 0 {
		t.Errorf("committed %d times after a failed walk, want 0", p.commits)
	}

	if p.closes != 1 {
		t.Errorf("closed %d times, want 1: Close runs whether the walk succeeded or failed", p.closes)
	}
}

// TestTheWalkAndItsSchedulerAreUnexported is the parked concurrency question
// kept parked. A scheduler an importer could select would answer it by
// accident, so nothing in walk.go is reachable from outside this package.
func TestTheWalkAndItsSchedulerAreUnexported(t *testing.T) {
	t.Parallel()

	f, err := parser.ParseFile(token.NewFileSet(), "walk.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse walk.go: %v", err)
	}

	if names := exportedDecls(f); len(names) != 0 {
		t.Errorf("walk.go exports %v: the walker, the scheduler and the direction seam are unexported "+
			"so that no importer can select a scheduler", names)
	}
}

func exportedDecls(f *ast.File) []string {
	var out []string

	for _, decl := range f.Decls {
		out = append(out, declaredNames(decl)...)
	}

	return out
}

func declaredNames(decl ast.Decl) []string {
	fn, ok := decl.(*ast.FuncDecl)
	if ok {
		if fn.Recv != nil {
			return nil
		}

		return exportedIdents(fn.Name)
	}

	gen, ok := decl.(*ast.GenDecl)
	if !ok {
		return nil
	}

	var out []string
	for _, spec := range gen.Specs {
		out = append(out, specIdents(spec)...)
	}

	return out
}

func specIdents(spec ast.Spec) []string {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		return exportedIdents(s.Name)
	case *ast.ValueSpec:
		return exportedIdents(s.Names...)
	default:
		return nil
	}
}

func exportedIdents(ids ...*ast.Ident) []string {
	var out []string

	for _, id := range ids {
		if id.IsExported() {
			out = append(out, id.Name)
		}
	}

	return out
}

// TestLoadRefusesAKindAStringCannotTake is the other half of "Absent does not
// write": every observation that is not Absent is a value the plane holds, and
// it is handed to the type set, which either accepts it or refuses it loudly.
// wide is a container of twenty leaves, which is the shape the scheduler seam
// is priced on: what a container costs the walk is a function of how many
// members it has, and twenty is an ordinary section rather than an extreme one.
type wide struct {
	A string `ferry:"a"`
	B string `ferry:"b"`
	C string `ferry:"c"`
	D string `ferry:"d"`
	E string `ferry:"e"`
	F string `ferry:"f"`
	G string `ferry:"g"`
	H string `ferry:"h"`
	I string `ferry:"i"`
	J string `ferry:"j"`
	K string `ferry:"k"`
	L string `ferry:"l"`
	M string `ferry:"m"`
	N string `ferry:"n"`
	O string `ferry:"o"`
	P string `ferry:"p"`
	Q string `ferry:"q"`
	R string `ferry:"r"`
	S string `ferry:"s"`
	T string `ferry:"t"`
}

// BenchmarkLoadOverAWideContainer measures what one load spends over a single
// twenty-member container, which is where the count-and-one-body seam shows: a
// container costs one closure however many members it has, rather than one per
// member plus the slice holding them.
func BenchmarkLoadOverAWideContainer(b *testing.B) {
	values := map[Path]Value{}
	for _, name := range strings.Split("abcdefghijklmnopqrst", "") {
		values[At(name)] = String(name)
	}

	bound, err := Bind[wide](planeSource{p: newPlane(values)})
	if err != nil {
		b.Fatalf("bind: %+v", err)
	}

	b.ReportAllocs()

	for b.Loop() {
		if _, err := bound.Load(b.Context()); err != nil {
			b.Fatalf("load: %+v", err)
		}
	}
}

func TestLoadRefusesAKindAStringCannotTake(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{At("name"): Null, At("env"): Number("8080")})

	got, err := Load[walkConf](t.Context(), planeSource{p: p})
	if got != (walkConf{}) {
		t.Errorf("a failed load yielded %+v, want the zero value", got)
	}

	report := reportOf(err)
	for _, want := range []string{
		"/env: what is set here is number, and string cannot take one",
		"/name: what is set here is null, and string cannot take one",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report\n\t%s\ndoes not contain\n\t%s", report, want)
		}
	}

	if strings.Contains(report, "8080") {
		t.Errorf("report\n\t%s\nrepeats a value the plane supplied", report)
	}

	mustBeClass(t, err, ErrValue, ErrWrongKind)
}
