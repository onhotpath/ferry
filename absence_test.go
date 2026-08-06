package ferry

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// Every assertion in this file goes through Load, LoadOver and Dump. Absence is
// a rule about what a plane was asked, what came back, and what the field holds
// afterwards, so it is asserted as exactly those three things and never by
// calling into the walk.
//
// The one exception is the pair of source-level assertions at the foot of the
// file. "Core exports nothing for presence" and "the walk takes no lock" are
// statements about what is written rather than about any value a walk produces,
// and neither has a behaviour that could stand in for it.

// TestPresentBeatsAbsentAtAField is ADR-0006's one rule at the smallest shape
// that shows all three observations: absent leaves the field alone, and every
// other observation is a value the plane holds and is applied.
//
// The ADR measures the same three rows against a field declaring
// default=anonymous. A declared default is not applied on Load yet - that is
// #77's, and this branch only compiles the declaration into the schema - so the
// non-zero starting value here is a seed rather than a default. The rule under
// both is one rule: present beats absent, and empty is present.
func TestPresentBeatsAbsentAtAField(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		got  Value
		want string
	}{
		{name: "absent does not write", got: Value{}, want: "anonymous"},
		{name: "an explicit empty beats a non-zero starting value", got: String(""), want: ""},
		{name: "and a real value replaces it", got: String("svc"), want: "svc"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustLeaveTheField(t, c.got, c.want)
		})
	}
}

// mustLeaveTheField presents one observation over a field already holding
// "anonymous", and holds the result to what the rule says it should be.
func mustLeaveTheField(t *testing.T, got Value, want string) {
	t.Helper()

	src := planeSource{p: newPlane(map[Path]Value{leafAddr: got})}

	out, err := LoadOver(t.Context(), leafHolder[string]{V: "anonymous"}, src)
	if err != nil {
		t.Fatalf("load over: %+v", err)
	}

	if out.V != want {
		t.Errorf("%#v at the address left %q, want %q", got, out.V, want)
	}
}

// nullCase is one Go type against an explicit Null at its own address.
type nullCase struct {
	name  string
	admit bool

	// load runs the case and reports whether the field ended at the type's own
	// zero value, which is the observable difference between a null that was
	// applied and one that was not.
	load func(*testing.T) (zero bool, err error)
}

// nullAt builds one row.
//
// The seed is what the field holds before the load, and it is what makes an
// admitted Null observably a write: it replaces the seed with the type's own
// nil, where an Absent would have left the seed exactly where it was.
func nullAt[T any](name string, seed T, admit bool) nullCase {
	return nullCase{name: name, admit: admit, load: func(t *testing.T) (bool, error) {
		t.Helper()

		src := planeSource{p: newPlane(map[Path]Value{leafAddr: Null})}

		out, err := LoadOver(t.Context(), leafHolder[T]{V: seed}, src)

		return reflect.ValueOf(&out.V).Elem().IsZero(), err
	}}
}

// nullCases is ADR-0006's per-kind table, whole.
func nullCases() []nullCase {
	return slices.Concat(nullRefusingLeaves(), nullHoldingTypes())
}

// nullRefusingLeaves is every leaf in core's set with no null of its own. Each
// refuses a Null as a wrong kind, which is the same refusal a Bool gets at a
// string field, and the integer widths and both floats are listed one by one
// because "the integer kinds" is where a table quietly stops being exhaustive.
func nullRefusingLeaves() []nullCase {
	return []nullCase{
		nullAt("bool", true, false),
		nullAt("string", "seed", false),
		nullAt("int", int(1), false),
		nullAt("int8", int8(1), false),
		nullAt("int16", int16(1), false),
		nullAt("int32", int32(1), false),
		nullAt("int64", int64(1), false),
		nullAt("uint", uint(1), false),
		nullAt("uint8", uint8(1), false),
		nullAt("uint16", uint16(1), false),
		nullAt("uint32", uint32(1), false),
		nullAt("uint64", uint64(1), false),
		nullAt("float32", float32(1.5), false),
		nullAt("float64", float64(1.5), false),
		nullAt("time.Duration", time.Second, false),
		nullAt("time.Time", pinnedTime(), false),
		nullAt("[4]byte", [4]byte{'s', 'e', 'e', 'd'}, false),
	}
}

// nullHoldingTypes is every type in core's set that has a null, which is the
// whole of what a Null is admitted by: the one leaf with a nil, and the three
// composite shapes whose zero value is a nil.
func nullHoldingTypes() []nullCase {
	seedInt, seedTags := 7, []string{"seed"}

	return []nullCase{
		nullAt("[]byte", []byte("seed"), true),
		nullAt("*int, where the pointer adds a null to a leaf", &seedInt, true),
		nullAt("*T over a composite", &cred{User: "seed"}, true),
		nullAt("[]T", []string{"seed"}, true),
		nullAt("map[K]V", map[string]int{"seed": 1}, true),
		nullAt("*[]T, where the pointer adds no bit", &seedTags, true),
	}
}

// TestNullIsAdmittedByExactlyTheTypesThatHaveOne is ADR-0006's per-kind table.
//
// Null is not a second spelling of absence: it means the plane has this address
// and the value stored there is that plane's own null, so it is presence
// carrying a value and the only question is which Go types can hold one. The
// refusal is the recoverable direction, which is the whole argument for it: a
// registered codec for its own type can accept a Null and return whatever it
// likes, while a core that zeroed in the walk would zero before any codec is
// consulted and nothing could recover strictness for a plain int.
func TestNullIsAdmittedByExactlyTheTypesThatHaveOne(t *testing.T) {
	t.Parallel()

	for _, c := range nullCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			c.check(t)
		})
	}
}

func (c nullCase) check(t *testing.T) {
	t.Helper()

	zero, err := c.load(t)

	if !c.admit {
		c.checkRefused(t, err)

		return
	}

	if err != nil {
		t.Fatalf("%s does not take a null: %+v", c.name, err)
	}

	if !zero {
		t.Errorf("a null at %s left the seed in place, and a null is presence carrying a value", c.name)
	}
}

func (c nullCase) checkRefused(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatalf("%s took a null, and a null is admitted by exactly the types that have one", c.name)
	}

	if !errors.Is(err, ErrWrongKind) || !errors.Is(err, ErrValue) {
		t.Errorf("%s refused a null with %v, want a wrong-kind ErrValue", c.name, err)
	}
}

// everyLeaf holds one field per leaf shape core ships, plus the three composite
// shapes that have a null, so one dump of its zero value exercises every place a
// Null could be written.
type everyLeaf struct {
	B    bool           `ferry:"b"`
	S    string         `ferry:"s"`
	I    int            `ferry:"i"`
	U    uint           `ferry:"u"`
	F    float64        `ferry:"f"`
	D    time.Duration  `ferry:"d"`
	When time.Time      `ferry:"when"`
	Arr  [3]byte        `ferry:"arr"`
	By   []byte         `ferry:"by"`
	P    *int           `ferry:"p"`
	Sl   []string       `ferry:"sl"`
	M    map[string]int `ferry:"m"`
}

// TestNothingFerryDumpsWritesANullAtATypeThatRefusesOne closes the gap the
// per-kind refusal looks like it leaves.
//
// A Null is emitted only for a nil pointer, a nil or empty composite and a nil
// []byte, all of which accept one back. So the refusal fires against a plane a
// human wrote and never against ferry's own output, and value fidelity is
// untouched: the whole of the zero value goes out and comes back with no
// refusal anywhere.
func TestNothingFerryDumpsWritesANullAtATypeThatRefusesOne(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	if err := Dump(t.Context(), everyLeaf{}, planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	// The four addresses whose Go type has a null, split by where the null is
	// said. A nil []byte and a nil *int are leaves, so their null is a Value;
	// a nil slice and a nil map are composites, so theirs is the answer at the
	// container's own address (ADR-0016).
	leaves := []Path{At("by"), At("p")}
	containers := []Path{At("m"), At("sl")}

	if nulls := nullsWritten(t, p, leaves); nulls != len(leaves) {
		t.Errorf("the dump wrote %d nulls, want one per nullable leaf: %v", nulls, leaves)
	}

	if got := sortedPaths(p.ensured); !slices.Equal(got, containers) {
		t.Errorf("the dump answered at %v, want one per nullable container: %v", got, containers)
	}

	if _, err := Load[everyLeaf](t.Context(), planeSource{p: p}); err != nil {
		t.Errorf("ferry's own output does not load back, so the refusal fired against a plane ferry wrote: %+v", err)
	}
}

// nullsWritten counts the nulls one dump handed a leaf, and reports every one of
// them at an address whose Go type would refuse it on the way back.
func nullsWritten(t *testing.T, p *plane, nullable []Path) int {
	t.Helper()

	n := 0

	for _, addr := range p.set {
		if p.values[addr] != Null {
			continue
		}

		n++

		if !slices.Contains(nullable, addr) {
			t.Errorf("ferry wrote a null at %s, and that type refuses one on the way back", addr)
		}
	}

	return n
}

// mergeConf is the shape ADR-0006 measures partial presence on: a struct whose
// fields are separate addresses, beside the two composites that are one decision
// each.
type mergeConf struct {
	Auth   cred           `ferry:"auth"`
	Tags   []string       `ferry:"tags"`
	Limits map[string]int `ferry:"limits"`
}

// TestAStructMergesAndACompositeReplaces is one rule that does not look like one
// rule, which is why all three are in one value.
//
// A struct's fields are separate addresses, so the ones the plane does not have
// are Absent and are left alone. A composite is a single decision: the plane
// either has children under that address or it does not, and if it has any then
// it has said what the composite is. There is no third option available, because
// merging a slice or a map would mean deciding what an absent index or an absent
// key means against a seeded one, and a plane cannot report present-and-empty at
// a container address to decide it with.
func TestAStructMergesAndACompositeReplaces(t *testing.T) {
	t.Parallel()

	seed := mergeConf{
		Auth:   cred{User: "u", Pass: "p"},
		Tags:   []string{"a", "b"},
		Limits: map[string]int{"rps": 1, "burst": 2},
	}

	p := newPlane(map[Path]Value{
		At("auth", "user"):  String("NEW"),
		At("tags").Elem(0):  String("NEW"),
		At("limits", "rps"): Number("99"),
	})

	got, err := LoadOver(t.Context(), seed, treeSource{p: p})
	if err != nil {
		t.Fatalf("load over: %+v", err)
	}

	if want := (cred{User: "NEW", Pass: "p"}); got.Auth != want {
		t.Errorf("the struct loaded %+v, want %+v: a field the plane does not have is absent", got.Auth, want)
	}

	if !slices.Equal(got.Tags, []string{"NEW"}) {
		t.Errorf("the sequence loaded %v, want [NEW]: a composite is replaced wholesale", got.Tags)
	}

	if len(got.Limits) != 1 || got.Limits["rps"] != 99 {
		t.Errorf("the mapping loaded %v, want one entry: a composite is replaced wholesale", got.Limits)
	}
}

// TestThePresenceBitIsNotAZeroValueProbe is survey item 5.7 reproduced and then
// repaired, in one test, because the repair is only visible against the defect.
//
// xload allocates a fresh zero value and reflect.DeepEqual's it against the
// populated struct to decide whether a struct pointer is written back. A subtree
// the plane really did set to all zeros is indistinguishable from one nothing
// touched under that probe. ferry's walk carries a presence bit instead, and the
// bit is only correct because absence is a kind first: a bool threaded through a
// walk whose loader cannot report presence is set by a zero and a miss alike.
func TestThePresenceBitIsNotAZeroValueProbe(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{At("opt", "user"): String(""), At("opt", "pass"): String("")})

	got, err := Load[optConf](t.Context(), planeSource{p: p})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got.Opt == nil {
		t.Fatal("the section is nil, and the plane set every address under it to its zero")
	}

	if !reflect.DeepEqual(got.Opt, new(cred)) {
		t.Fatalf("the section loaded %+v, and this test is about a subtree that is all zeros", *got.Opt)
	}
}

// TestAPointedLeafTellsUnsetFromZeroOnAPlaneWithNoNull is the narrow half of the
// claim, stated as narrowly as it is true.
//
// A *T at a leaf expresses unset against zero fully on Load from any plane,
// because absence is observable on every plane; on Dump it does so only from a
// plane that has a null, since a null is what a nil pointer writes. The plane
// here answers with String and Number and never with a Null, which is env, TOML
// and query parameters, and the distinction survives it.
func TestAPointedLeafTellsUnsetFromZeroOnAPlaneWithNoNull(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		plane map[Path]Value
		want  *int
	}{{
		name:  "unset is nil",
		plane: map[Path]Value{},
		want:  nil,
	}, {
		name:  "and an explicit zero is a pointer to zero",
		plane: map[Path]Value{At("port"): Number("0")},
		want:  new(int),
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustLoadThePointedPort(t, c.plane, c.want)
		})
	}
}

func mustLoadThePointedPort(t *testing.T, values map[Path]Value, want *int) {
	t.Helper()

	got, err := Load[pointedLeaf](t.Context(), planeSource{p: newPlane(values)})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if (got.Port == nil) != (want == nil) {
		t.Fatalf("loaded %v, want %v", got.Port, want)
	}

	if want != nil && *got.Port != *want {
		t.Errorf("loaded %d, want %d", *got.Port, *want)
	}
}

// omittingConf holds the shape that leaves an address the schema names with no
// write at all: a nil optional section, whose two fields get no Set call,
// because the answer is at the section's own address instead.
type omittingConf struct {
	Name string   `ferry:"name"`
	Opt  *cred    `ferry:"opt"`
	Tags []string `ferry:"tags"`
}

// TestNoSetCallCarriesAnAbsent is the contract rule counted rather than
// described.
//
// Absent is a Reader-side kind: on Dump ferry holds the value and is the one
// making the plane, so there is no observation to report, and an omitted address
// gets no Set call at all rather than a Set carrying nothing. The prototype
// ADR-0004 recorded got this wrong in the other direction - its YAML sink mapped
// Absent to an explicit null, which read back as a Null - and that is the
// conflation ferry criticises xload for, committed on the write path.
func TestNoSetCallCarriesAnAbsent(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	if err := Dump(t.Context(), omittingConf{}, planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	for _, addr := range p.set {
		if p.values[addr].Kind() == KindAbsent {
			t.Errorf("Set was called at %s with an absent value, which is a Reader-side kind", addr)
		}
	}

	// The schema names five addresses, one leaf per value plus the two
	// containers, and the walk handed a Value to exactly one of them: the two
	// under the nil section got no call at all, and the nil section and the
	// empty sequence are answered at their own addresses instead (ADR-0016).
	wantBound := []string{"leaf /name", "section /opt", "leaf /opt/pass", "leaf /opt/user", "composite /tags"}
	if got := kinded(p.bound); !slices.Equal(got, wantBound) {
		t.Errorf("the driver was bound to\n\t%v\nand the golden set is\n\t%v", got, wantBound)
	}

	mustBeAddresses(t, sortedPaths(p.set), []string{"/name"})
	mustBeAddresses(t, sortedPaths(p.ensured), []string{"/opt", "/tags"})
}

// sortedPaths is a copy of what a plane was asked, in address order, because
// the assertion is about the set and not about the walk's own ordering.
func sortedPaths(got []Path) []Path {
	out := slices.Clone(got)
	slices.SortFunc(out, Path.Compare)

	return out
}

// observed is a caller-side Source decorator that keeps every Value the plane
// answered, including Absent.
//
// It is the whole of ADR-0006's plane-inspection mechanism: the observation is a
// property of one Load rather than of a field, and it needs nothing from core,
// because a Reader is already handed every address the walk asks about. Whether
// core ever spells it as a callback, a recorder or a returned report is #25's,
// and this is what makes it possible to answer later without changing the walk.
type observed struct {
	src  Source
	seen map[Path]Value
}

func (o *observed) Bind(addrs *AddressSet) (OpenFunc, error) {
	open, err := o.src.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (Reader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return observingReader{r: r, seen: o.seen}, nil
	}, nil
}

type observingReader struct {
	r    Reader
	seen map[Path]Value
}

func (o observingReader) Get(ctx context.Context, addr LeafAddr) (Value, error) {
	got, err := o.r.Get(ctx, addr)
	if err == nil {
		o.seen[addr.Path()] = got
	}

	return got, err
}

// TestPresenceSurvivesTheWalkAsAnObservationOfOneLoad is the row pair that makes
// the mechanism worth having: a key deleted from the plane and a key set to zero
// are one struct and two observations.
//
// The struct erases absence, which is the whole reason plane inspection was
// milestoned on this ticket. The observation does not, and it converts nothing:
// it reports the Value the driver produced, before any leaf decode, so there is
// exactly one conversion engine and the observation is upstream of it.
func TestPresenceSurvivesTheWalkAsAnObservationOfOneLoad(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		plane map[Path]Value
		want  int
		saw   Value
	}{
		{name: "a value", plane: map[Path]Value{At("v"): Number("5432")}, want: 5432, saw: Number("5432")},
		{name: "the address deleted", plane: map[Path]Value{}, want: 0, saw: Value{}},
		{name: "and the address set to zero", plane: map[Path]Value{At("v"): Number("0")}, want: 0, saw: Number("0")},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustObserve(t, c.plane, c.want, c.saw)
		})
	}
}

// mustObserve loads one plane through the decorator and holds both halves to
// their answers: what the field ended at, and what the boundary reported.
func mustObserve(t *testing.T, values map[Path]Value, want int, saw Value) {
	t.Helper()

	src := &observed{src: planeSource{p: newPlane(values)}, seen: map[Path]Value{}}

	got, err := Load[leafHolder[int]](t.Context(), src)
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got.V != want {
		t.Errorf("loaded %d, want %d", got.V, want)
	}

	if seen := src.seen[leafAddr]; seen != saw {
		t.Errorf("the load observed %#v at %s, want %#v", seen, leafAddr, saw)
	}
}

// containerPresence is the presence vocabulary core does publish, and it is all
// of it: what a plane holds at a container's own address, which is a fact about
// the plane that a driver reports and core resolves (ADR-0016).
//
// It is an allow-list rather than a skip, so the ratchet still holds: a new
// export whose name says presence fails this test until it is either withdrawn
// or added here on purpose.
var containerPresence = map[string]bool{
	"Presence":        true,
	"PresenceAbsent":  true,
	"PresenceNull":    true,
	"PresencePresent": true,
	"SectionAbsent":   true,
	"SectionInfo":     true,
	"SectionNull":     true,
	"SectionPresent":  true,
	"Prober":          true,
	"Ensurer":         true,
}

// TestCoreExportsNothingForPresenceAtAField is the other half of the mechanism:
// presence at a leaf is an observation of a Load and not a property of a field,
// so no type is added to the set, nothing is stored on the field, and core
// exports no Option, callback or report for it.
//
// It is read out of the source because there is no value that could be asked. An
// Optional[T] in the type set would need a proof in the harness, a row in the
// golden column and a representation on every plane, and ADR-0005 closed the set
// against exactly that.
//
// What core does spell is presence at a container's own address, which is a
// different fact about a different place: [containerPresence] is that list, and
// nothing on it is per field.
func TestCoreExportsNothingForPresenceAtAField(t *testing.T) {
	t.Parallel()

	names := coreExportedNames(t)
	if len(names) == 0 {
		t.Fatal("no exported name was read out of the package, so this test asserts nothing")
	}

	for _, name := range names {
		if !containerPresence[name] && namesPresence(name) {
			t.Errorf("core exports %s: presence at a field is an observation of one Load, which a "+
				"caller's own Reader already sees, and core spells nothing for it", name)
		}
	}
}

// namesPresence reports whether an exported name reads as a presence fact.
func namesPresence(name string) bool {
	for _, word := range []string{"Presence", "Present", "Observ", "Optional"} {
		if strings.Contains(name, word) {
			return true
		}
	}

	return false
}

// coreExportedNames is every exported top-level name and method in the package,
// read from the source rather than from a value.
func coreExportedNames(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}

	var out []string

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}

		f, err := parser.ParseFile(token.NewFileSet(), e.Name(), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}

		out = append(out, exportedDecls(f)...)
		out = append(out, exportedMethods(f)...)
	}

	return out
}

// exportedMethods is what exportedDecls skips, because a walk rule is asserted
// through a function and a report would arrive as a method on something.
func exportedMethods(f *ast.File) []string {
	var out []string

	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv != nil {
			out = append(out, exportedIdents(fn.Name)...)
		}
	}

	return out
}

// TestALengthComesFromEnumerationAndNeverFromProbing closes the half of
// ADR-0003's question that looks workable and is not.
//
// Probing /tags#0, /tags#1 and so on until a miss discovers a length, and a hole
// truncates the list silently, which is the silent loss the whole design rules
// out. The plane here holds two elements and lists one, so a walk that probed
// would return two and a walk that enumerates returns one - and the address the
// probe would have found is never asked for at all. The other half, that a hole
// in what was enumerated is refused rather than read through, is
// TestAPlaneThatContradictsTheContainerIsLoud's gap case.
func TestALengthComesFromEnumerationAndNeverFromProbing(t *testing.T) {
	t.Parallel()

	src := &listing{
		values: map[Path]Value{
			At("tags").Elem(0): String("a"),
			At("tags").Elem(1): String("b"),
		},
		children: map[Path][]Segment{At("tags"): {IndexSegment(0)}},
	}

	got, err := Load[tagsOnly](t.Context(), src)
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if !slices.Equal(got.Tags, []string{"a"}) {
		t.Errorf("loaded %v, want [a]: the length is whatever enumeration reported", got.Tags)
	}

	if slices.Contains(src.got, At("tags").Elem(1)) {
		t.Errorf("the walk asked for %v, and an index nothing enumerated is one nothing probes for", src.got)
	}
}

// TestTheWalkTakesNoLockAndStartsNoGoroutine keeps the concurrency question
// parked from the other side.
//
// Every per-subtree fact the walk carries is now returned rather than shared, so
// there is nothing here for a lock to protect; a lock or a goroutine added here
// would be an answer to #20 all the same, and the serial scheduler core ships
// needs neither.
func TestTheWalkTakesNoLockAndStartsNoGoroutine(t *testing.T) {
	t.Parallel()

	f, err := parser.ParseFile(token.NewFileSet(), "walk.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse walk.go: %v", err)
	}

	for _, imp := range f.Imports {
		if path := imp.Path.Value; path == `"sync"` || path == `"sync/atomic"` {
			t.Errorf("walk.go imports %s: the presence bit is handed to #20 as a hazard rather than locked", path)
		}
	}

	ast.Inspect(f, func(n ast.Node) bool {
		if _, ok := n.(*ast.GoStmt); ok {
			t.Error("walk.go starts a goroutine: core ships the serial scheduler and answers #20 with nothing")
		}

		return true
	})
}
