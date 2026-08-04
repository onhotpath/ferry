package ferry

import (
	"context"
	"errors"
	"math/big"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

// Every assertion in this file goes through Compile[T], Load, LoadOver and Dump.
// An address-set rule is asserted through what a recording driver's Bind was
// handed, a walk rule through what a plane was asked and what came back, and a
// compiler rule through the report Compile returns. Nothing reaches the compiled
// schema, the node tree or the walker.

// cred is the two-leaf struct every composite shape below is built out of. Its
// unexported field is there to be skipped, which is a thing only reflect can
// see.
type cred struct {
	User   string `ferry:"user"`
	Pass   string `ferry:"pass"`
	hidden string
}

// Touch is what stops hidden being unused. It is there to be skipped, which is
// a thing only reflect can see.
func (c *cred) Touch() { c.hidden = "" }

// The four static shapes, one type each, so a golden address set is a statement
// about one shape rather than about a fixture.
type (
	plainStruct struct {
		Plain cred `ferry:"plain"`
	}
	pointerStruct struct {
		Opt *cred `ferry:"opt"`
	}
	arrayOfLeaves struct {
		Ports [3]string `ferry:"ports"`
	}
	arrayOfBytes struct {
		Key [4]byte `ferry:"key"`
	}
	everyShape struct {
		Plain plainStruct   `ferry:"a"`
		Opt   pointerStruct `ferry:"b"`
		Ports arrayOfLeaves `ferry:"c"`
		Key   arrayOfBytes  `ferry:"d"`
		Mat   [2]cred       `ferry:"e"`
	}
)

// boundBy is the address set a driver's Bind was handed for one dump.
func boundBy(t *testing.T, dump func(context.Context, Sink) error) []Path {
	t.Helper()

	p := newPlane(map[Path]Value{})
	if err := dump(t.Context(), planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	return slices.Collect(p.bound.All())
}

func mustBeAddresses(t *testing.T, got []Path, want []string) {
	t.Helper()

	rendered := make([]string, len(got))
	for i, p := range got {
		rendered[i] = p.String()
	}

	if !slices.Equal(rendered, want) {
		t.Errorf("the address set is\n\t%v\nand the golden set is\n\t%v", rendered, want)
	}
}

// TestTheStaticAddressSetPerTypeShape is the golden case per composite whose
// addresses come from the type, asserting exact membership and segment-wise
// order.
//
// The order is the set's own and is not the order of the renderings: a container
// address sorts before what is under it, and /e#0 before /e#1 numerically rather
// than bytewise.
func TestTheStaticAddressSetPerTypeShape(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		dump func(context.Context, Sink) error
		want []string
	}{{
		name: "a struct mints one Name segment per exported field",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, plainStruct{}, s) },
		want: []string{"/plain/pass", "/plain/user"},
	}, {
		name: "a pointer mints none of its own, and one container address",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, pointerStruct{}, s) },
		want: []string{"/opt", "/opt/pass", "/opt/user"},
	}, {
		name: "an array mints exactly N Index segments",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, arrayOfLeaves{}, s) },
		want: []string{"/ports#0", "/ports#1", "/ports#2"},
	}, {
		name: "an array of bytes is one leaf address, because it is a leaf",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, arrayOfBytes{}, s) },
		want: []string{"/key"},
	}, {
		name: "and the four together, segment-wise",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, everyShape{}, s) },
		want: []string{
			"/a/plain/pass", "/a/plain/user",
			"/b/opt", "/b/opt/pass", "/b/opt/user",
			"/c/ports#0", "/c/ports#1", "/c/ports#2",
			"/d/key",
			"/e#0/pass", "/e#0/user", "/e#1/pass", "/e#1/user",
		},
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustBeAddresses(t, boundBy(t, c.dump), c.want)
		})
	}
}

// withHidden has one unexported field of every shape this ticket admits, so
// "unexported fields are skipped" is a statement about the composites and not
// only about leaves.
type withHidden struct {
	Name   string `ferry:"name"`
	nested cred
	boxed  *cred
	arr    [2]string
}

// Touch is what stops the three unexported fields being unused. They are there
// to be skipped, which is a thing only reflect can see.
func (w *withHidden) Touch() { w.nested, w.boxed, w.arr = cred{}, nil, [2]string{} }

// TestUnexportedFieldsAreSkipped is the rule stated rather than left to silence.
// reflect cannot set an unexported field, so refusing every struct containing
// one would refuse every struct containing a sync.Mutex.
func TestUnexportedFieldsAreSkipped(t *testing.T) {
	t.Parallel()

	mustBeAddresses(t, boundBy(t, func(ctx context.Context, s Sink) error {
		return Dump(ctx, withHidden{}, s)
	}), []string{"/name"})
}

// starNamed is a field whose segment text genuinely is "*", which under ferry's
// escaping model is ordinary text rather than a marker.
type starNamed struct {
	Star string `ferry:"*"`
	Opt  *cred  `ferry:"opt"`
}

// TestNoWildcardShapeCrossesTheDriverBoundary asserts through a recording Source
// that every address a driver is handed is one it can fetch, write, name and
// check.
//
// A wildcard shape is the walk's own lookup key and there is nothing at it, so
// handing one over is wrong three ways and the third is silent: "*" is ordinary
// segment text, so a schema that genuinely names the segment "*" would render
// one plane key for two members of the set. This schema names it, and the set it
// produces holds that address once and holds no shape at all.
func TestNoWildcardShapeCrossesTheDriverBoundary(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})

	if _, err := Load[starNamed](t.Context(), planeSource{p: p}); err != nil {
		t.Fatalf("load: %+v", err)
	}

	bound := slices.Collect(p.bound.All())
	mustBeAddresses(t, bound, []string{"/*", "/opt", "/opt/pass", "/opt/user"})

	asked := slices.Clone(p.got)
	slices.SortFunc(asked, Path.Compare)

	if !slices.Equal(asked, bound) {
		t.Errorf("the walk asked about\n\t%v\nand the driver was bound to\n\t%v", asked, bound)
	}
}

// prefixFree is /Opt beside /Opt/User, which the amended rule accepts: a
// container address is a proper prefix of what is under it by construction, and
// that is what makes it a container.
type prefixFree struct {
	Opt  *cred     `ferry:"opt"`
	Mat  [2]cred   `ferry:"mat"`
	Flat string    `ferry:"opt-flat"`
	Arr  [2]string `ferry:"arr"`
}

// leafOverSubtree is the hazard the rule exists for: a leaf and a subtree at one
// segment, which a flat plane holds happily as DB and DB_HOST and a tree plane
// cannot represent at all.
type leafOverSubtree struct {
	DB   string `ferry:"db"`
	Deep cred   `ferry:"db"`
}

// boxedTwice is a container address and a leaf address at one place. The outer
// pointer takes a container address, and the inner one is a pointer to a leaf,
// which takes a leaf address at the same place.
type boxedTwice struct {
	P **int `ferry:"p"`
}

// TestPrefixFreenessIsOverTheLeafAddresses is ADR-0003's rule as amended: taken
// over the whole set it would refuse every schema the type set requires.
func TestPrefixFreenessIsOverTheLeafAddresses(t *testing.T) {
	t.Parallel()

	t.Run("a container address beside what is under it is accepted", func(t *testing.T) {
		t.Parallel()

		if err := Compile[prefixFree](); err != nil {
			t.Fatalf("%+v", err)
		}
	})

	run(t, []compileCase{{
		name:     "a leaf and a subtree at one segment is still rejected, naming every clash",
		run:      Compile[leafOverSubtree],
		want:     []string{"/db:", "a leaf address and a prefix of", "/DB", "/Deep/User", "/Deep/Pass"},
		elements: 2,
	}, {
		name:     "and a container address must be distinct from every leaf address",
		run:      Compile[boxedTwice],
		want:     []string{"/p:", "a container address and a leaf address at once", "nowhere to be"},
		elements: 1,
	}})
}

// The maps-no-address cases, at every level: the root, a field, under a pointer,
// and under an array.
type (
	nestedNoAddress struct {
		V netip.Addr `ferry:"v"`
	}
	pointedNoAddress struct {
		V *big.Int `ferry:"v"`
	}
	arrayNoAddress struct {
		V [3]netip.AddrPort `ferry:"v"`
	}
	oneGoodSibling struct {
		Name string     `ferry:"name"`
		Addr netip.Addr `ferry:"addr"`
	}
)

// TestAStructThatMapsNoAddressIsRefusedAtEveryLevel is ADR-0005's sharpest
// single line. Before the rule existed a dump of netip.MustParseAddr("192.0.2.1")
// produced zero addresses and a nil error, and the value was silently and
// totally lost - which is worse than an unsupported type, because it looks
// supported.
//
// The check is at every level and not only at the root, because one mapped
// sibling would otherwise hide the loss.
func TestAStructThatMapsNoAddressIsRefusedAtEveryLevel(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name:     "at the root",
		run:      Compile[netip.Addr],
		want:     []string{"netip.Addr maps no address", "register a codec for it"},
		elements: 1,
	}, {
		name:     "at a field",
		run:      Compile[nestedNoAddress],
		want:     []string{"netip.Addr maps no address"},
		elements: 1,
	}, {
		name:     "under a pointer",
		run:      Compile[pointedNoAddress],
		want:     []string{"big.Int maps no address"},
		elements: 1,
	}, {
		name:     "under an array, once rather than N times",
		run:      Compile[arrayNoAddress],
		want:     []string{"netip.AddrPort maps no address"},
		elements: 1,
	}, {
		name:     "and one mapped sibling does not hide it",
		run:      Compile[oneGoodSibling],
		want:     []string{"netip.Addr maps no address"},
		elements: 1,
	}})
}

// The three routes a type takes back to itself. None of them can be written
// through a plain struct field, because Go has no infinitely sized type.
type (
	viaPointer struct {
		Name string      `ferry:"name"`
		Next *viaPointer `ferry:"next"`
	}
	viaSlice struct {
		Kids []viaSlice `ferry:"kids"`
	}
	viaMap struct {
		M map[string]viaMap `ferry:"m"`
	}
	mutualA struct {
		B *mutualB `ferry:"b"`
	}
	mutualB struct {
		A *mutualA `ferry:"a"`
	}
)

// TestARecursiveTypeIsRefused is detected from reflect.TypeFor[T]() alone, with
// no value in hand: an address set that cannot be enumerated cannot be handed to
// Bind before I/O, which is the precondition the driver-side injectivity rule
// needs.
//
// The slice and the map rows matter even though neither composite compiles yet.
// "unsupported element type" is the wrong diagnosis for struct{ Kids []Tree },
// and it is the one a compiler that only stacks what it descends into gives.
func TestARecursiveTypeIsRefused(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name:     "through a pointer",
		run:      Compile[viaPointer],
		want:     []string{"/next:", "is recursive, through ferry.viaPointer", "register a codec for it"},
		elements: 1,
	}, {
		name:     "through a slice",
		run:      Compile[viaSlice],
		want:     []string{"/kids:", "[]ferry.viaSlice is recursive, through ferry.viaSlice"},
		elements: 1,
	}, {
		name:     "through a map",
		run:      Compile[viaMap],
		want:     []string{"/m:", "is recursive, through ferry.viaMap"},
		elements: 1,
	}, {
		name:     "and between two types rather than within one",
		run:      Compile[mutualA],
		want:     []string{"/b/a:", "is recursive, through ferry.mutualA"},
		elements: 1,
	}})
}

// arrayConf is the array under test, beside a leaf so an empty plane is not the
// only thing being asserted about.
type arrayConf struct {
	Name  string    `ferry:"name"`
	Ports [3]string `ferry:"ports"`
}

// TestAnArrayIsStaticSoItLoadsFromASourceThatCannotEnumerate is the capability
// difference between an array and a slice, and it is the reason an array is a
// static composite: its element addresses are known from the type.
//
// The plane below implements Get and nothing else, so it cannot list what it
// holds, and the load still reaches every element.
func TestAnArrayIsStaticSoItLoadsFromASourceThatCannotEnumerate(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{
		At("name"):          String("svc"),
		At("ports").Elem(0): String("a"),
		At("ports").Elem(2): String("c"),
	})

	got, err := Load[arrayConf](t.Context(), planeSource{p: p})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	// An absent element leaves the element at its zero value, exactly as an
	// absent struct field does.
	if want := [3]string{"a", "", "c"}; got.Ports != want {
		t.Errorf("loaded %q, want %q", got.Ports, want)
	}

	if _, ok := any(p.reader()).(Enumerator); ok {
		t.Error("the plane can enumerate, so this test asserts nothing about a plane that cannot")
	}
}

// listing is a plane that can list what it holds, which is what it takes to see
// an index an array cannot hold: the elements themselves are read by name.
type listing struct {
	values   map[Path]Value
	children map[Path][]Path
	listErr  error
}

func (l *listing) Bind(*AddressSet) (OpenFunc, error) {
	return func(context.Context) (Reader, error) { return l, nil }, nil
}

func (l *listing) Get(_ context.Context, addr Path) (Value, error) { return l.values[addr], nil }

func (l *listing) Children(_ context.Context, prefix Path) ([]Path, error) {
	return l.children[prefix], l.listErr
}

// TestAnIndexTheArrayCannotHoldIsLoud is the other half of the array rule.
// Padding or truncating would be the silent loss the whole design rules out, so
// the plane holding a fourth element of a three-element array is an error naming
// the index and the length.
func TestAnIndexTheArrayCannotHoldIsLoud(t *testing.T) {
	t.Parallel()

	t.Run("every index the array holds is accepted", func(t *testing.T) {
		t.Parallel()

		src := &listing{
			values:   map[Path]Value{At("ports").Elem(1): String("b")},
			children: map[Path][]Path{At("ports"): {At("ports").Elem(0), At("ports").Elem(1)}},
		}

		got, err := Load[arrayConf](t.Context(), src)
		if err != nil {
			t.Fatalf("load: %+v", err)
		}

		if want := [3]string{"", "b", ""}; got.Ports != want {
			t.Errorf("loaded %q, want %q", got.Ports, want)
		}
	})

	t.Run("and one it does not is refused, naming the index and the length", func(t *testing.T) {
		t.Parallel()

		src := &listing{
			children: map[Path][]Path{At("ports"): {At("ports").Elem(0), At("ports").Elem(7)}},
		}

		_, err := Load[arrayConf](t.Context(), src)

		report := reportOf(err)
		if want := "/ports: the plane holds index 7 and [3]string holds 3"; !strings.Contains(report, want) {
			t.Errorf("report\n\t%s\ndoes not contain\n\t%s", report, want)
		}

		mustBeClass(t, err, ErrValue)
	})
}

// optConf is a pointer to a composite beside a leaf, which is the shape ADR-0006
// measures the presence rule on.
type optConf struct {
	Name string `ferry:"name"`
	Opt  *cred  `ferry:"opt"`
}

// TestAPointerToACompositeIsAnAddressOfItsOwn is the container question, in both
// directions. A nil pointer writes a Null at the pointer's own address and
// nothing beneath it; a set one writes what is beneath it and nothing at its own
// address, so the container address is never realised at the same time as
// anything under it.
func TestAPointerToACompositeIsAnAddressOfItsOwn(t *testing.T) {
	t.Parallel()

	t.Run("a nil pointer dumps a null at its own address", aNilPointerDumpsANull)
	t.Run("and a set one dumps what is beneath it", aSetPointerDumpsWhatIsBeneathIt)
}

func aNilPointerDumpsANull(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	if err := Dump(t.Context(), optConf{Name: "svc"}, planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if got := p.values[At("opt")]; got != Null() {
		t.Errorf("/opt holds %v, want a null", got)
	}

	if slices.Contains(p.set, At("opt", "user")) {
		t.Errorf("the walk wrote %v, and a nil pointer has nothing beneath it", p.set)
	}
}

func aSetPointerDumpsWhatIsBeneathIt(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	v := optConf{Name: "svc", Opt: &cred{User: "u", Pass: "p"}}

	if err := Dump(t.Context(), v, planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if slices.Contains(p.set, At("opt")) {
		t.Errorf("the walk wrote %v, and a realised container competes with its own children", p.set)
	}

	if got := p.values[At("opt", "user")]; got != String("u") {
		t.Errorf("/opt/user holds %v, want string(u)", got)
	}
}

// TestAPointerIsMaterialisedWhereThePlaneSpokeUnderIt is ADR-0006's rule: an
// optional section stays optional, and a seed beneath it is not presence.
func TestAPointerIsMaterialisedWhereThePlaneSpokeUnderIt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		plane map[Path]Value
		want  *cred
	}{{
		name:  "nothing under it leaves it nil",
		plane: map[Path]Value{At("name"): String("svc")},
		want:  nil,
	}, {
		name:  "one address under it materialises it",
		plane: map[Path]Value{At("opt", "pass"): String("p")},
		want:  &cred{Pass: "p"},
	}, {
		name:  "and an explicit null at it is a nil pointer",
		plane: map[Path]Value{At("opt"): Null(), At("opt", "user"): String("u")},
		want:  nil,
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			p := newPlane(c.plane)

			got, err := Load[optConf](t.Context(), planeSource{p: p})
			if err != nil {
				t.Fatalf("load: %+v", err)
			}

			mustBeCred(t, got.Opt, c.want)
		})
	}
}

// mustBeCred compares two optional creds, either of which may be absent.
func mustBeCred(t *testing.T, got, want *cred) {
	t.Helper()

	if (got == nil) != (want == nil) {
		t.Fatalf("loaded %v, want %v", got, want)
	}

	if got != nil && *got != *want {
		t.Errorf("loaded %+v, want %+v", *got, *want)
	}
}

// TestAContainerAddressCarriesAbsenceOrANullAndNothingElse is what makes the
// container address safe to admit to the set: both of its observations mean
// "there is nothing under here", so it can never compete with a child.
func TestAContainerAddressCarriesAbsenceOrANullAndNothingElse(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{At("opt"): String("everything")})

	_, err := Load[optConf](t.Context(), planeSource{p: p})

	report := reportOf(err)
	want := "/opt: the plane holds string at a container address, which holds absence or a null"

	if !strings.Contains(report, want) {
		t.Errorf("report\n\t%s\ndoes not contain\n\t%s", report, want)
	}

	if strings.Contains(report, "everything") {
		t.Errorf("report\n\t%s\nrepeats a value the plane supplied", report)
	}

	mustBeClass(t, err, ErrValue)
}

// seeded is the seed every case below is handed, fresh per call, because a seed
// shared across subtests is the defect that hides a walk writing into it.
func seeded() optConf { return optConf{Opt: &cred{User: "seed", Pass: "seed"}} }

// TestLoadOverNeverWritesThroughTheSeedsOwnPointer is the property LoadOver's
// shallow copy rests on, checked at the one type in this ticket that could break
// it.
//
// `over := seed` copies a *cred rather than what it points at, so a walk that
// wrote through the seed's pointer would publish a partial load into a value the
// caller still holds, and "a failed load returns the seed, never the partial"
// would stop being a property of the shape. The pointee is built fresh and
// published only where the walk wrote, so it does not.
func TestLoadOverNeverWritesThroughTheSeedsOwnPointer(t *testing.T) {
	t.Parallel()

	t.Run("a load that succeeds", aSuccessfulLoadLeavesTheSeedAlone)
	t.Run("and one that fails", aFailedLoadLeavesTheSeedAlone)
}

func aSuccessfulLoadLeavesTheSeedAlone(t *testing.T) {
	t.Parallel()

	seed := seeded()
	p := newPlane(map[Path]Value{At("opt", "user"): String("plane")})

	got, err := LoadOver(t.Context(), seed, planeSource{p: p})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	mustBeUntouched(t, seed)

	if got.Opt == seed.Opt {
		t.Error("the load published the seed's own pointer, so a later load would mutate this result")
	}

	mustBeCred(t, got.Opt, &cred{User: "plane", Pass: "seed"})
}

func aFailedLoadLeavesTheSeedAlone(t *testing.T) {
	t.Parallel()

	seed := seeded()
	p := newPlane(map[Path]Value{
		At("opt", "user"): String("plane"),
		At("opt", "pass"): Number("1"),
	})

	got, err := LoadOver(t.Context(), seed, planeSource{p: p})
	if err == nil {
		t.Fatal("loaded clean, want a wrong-kind refusal at /opt/pass")
	}

	if got.Opt != seed.Opt {
		t.Errorf("a failed load yielded %+v, want the seed it was handed", got)
	}

	mustBeUntouched(t, seed)
}

// mustBeUntouched is the whole assertion: the walk was never allowed to reach
// what the caller still holds.
func mustBeUntouched(t *testing.T, seed optConf) {
	t.Helper()

	if want := (cred{User: "seed", Pass: "seed"}); *seed.Opt != want {
		t.Errorf("the seed's own pointee is now %+v, and the walk was never allowed to reach it", *seed.Opt)
	}
}

// pointedLeaf is *T where T is a leaf, which is not a composite: the leaf
// already had an address, and the pointer adds a null to it rather than a second
// place.
type pointedLeaf struct {
	Port *int `ferry:"port"`
}

// TestAPointerToALeafIsALeafWithANull is the one shape in the set that tells an
// explicit zero from an unset field on Dump.
func TestAPointerToALeafIsALeafWithANull(t *testing.T) {
	t.Parallel()

	zero := 0

	cases := []struct {
		name string
		v    *int
		want Value
	}{
		{name: "nil is a null", v: nil, want: Null()},
		{name: "and an explicit zero is a number", v: &zero, want: Number("0")},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustRoundTripAPointedLeaf(t, c.v, c.want)
		})
	}
}

func mustRoundTripAPointedLeaf(t *testing.T, v *int, want Value) {
	t.Helper()

	p := newPlane(map[Path]Value{})
	if err := Dump(t.Context(), pointedLeaf{Port: v}, planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	mustBeAddresses(t, slices.Collect(p.bound.All()), []string{"/port"})

	if got := p.values[At("port")]; got != want {
		t.Errorf("/port holds %v, want %v", got, want)
	}

	back, err := Load[pointedLeaf](t.Context(), planeSource{p: p})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if (back.Port == nil) != (v == nil) {
		t.Fatalf("loaded %v, want %v", back.Port, v)
	}

	if v != nil && *back.Port != *v {
		t.Errorf("loaded %d, want %d", *back.Port, *v)
	}
}

// rootArray, rootSlice and rootMap are the root shapes an entry point's
// signature cannot refuse for itself.
type (
	rootArray [2]string
	rootSlice []string
	rootMap   map[string]string
)

// TestTheRootMustBeAStructFerryWalks is the one rule Load[T] and Dump cannot
// express in their signatures.
func TestTheRootMustBeAStructFerryWalks(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name:     "a root leaf",
		run:      Compile[int],
		want:     []string{"int is not a struct ferry walks", "wrapping it in one is the whole remedy"},
		elements: 1,
	}, {
		name:     "a root array",
		run:      Compile[rootArray],
		want:     []string{"ferry.rootArray is not a struct ferry walks"},
		elements: 1,
	}, {
		name:     "a root slice",
		run:      Compile[rootSlice],
		want:     []string{"ferry.rootSlice is not a struct ferry walks"},
		elements: 1,
	}, {
		name:     "a root map, which is the shape a first run most often reaches for",
		run:      Compile[rootMap],
		want:     []string{"ferry.rootMap is not a struct ferry walks"},
		elements: 1,
	}, {
		name:     "and a root pointer to a leaf",
		run:      Compile[*int],
		want:     []string{"int is not a struct ferry walks"},
		elements: 1,
	}})

	t.Run("through Load", func(t *testing.T) {
		t.Parallel()

		p := newPlane(map[Path]Value{})
		if _, err := Load[int](t.Context(), planeSource{p: p}); err == nil {
			t.Fatal("Load[int] returned no error")
		}

		if p.bound != nil {
			t.Error("a driver was bound for a type that names no address")
		}
	})

	t.Run("and through Dump", func(t *testing.T) {
		t.Parallel()

		p := newPlane(map[Path]Value{})
		if err := Dump(t.Context(), 7, planeSink{p: p}); err == nil {
			t.Fatal("Dump of an int returned no error")
		}
	})
}

// treeSink is a sink shaped like a document format: every segment but the last
// names a node and the last names the value. It is the shape the root rule was
// measured against, where a YAML sink handed a root leaf wrote "{}" and returned
// a nil error.
type treeSink struct{ wrote map[string]Value }

func (s *treeSink) Bind(*AddressSet) (OpenWriterFunc, error) {
	return func(context.Context) (Writer, error) { return s, nil }, nil
}

// Set writes v at the name the last segment gives, under the node the ones
// before it name.
//
// Handed the empty path there is no last segment, so there is no name to write
// at and no node to write it in - and no error either, because a sink asked to
// put a value nowhere has done exactly what it was asked. That is the whole
// failure mode, and it is why the root rule is a refusal rather than a
// documented edge.
func (s *treeSink) Set(_ context.Context, addr Path, v Value) error {
	segs := slices.Collect(addr.Segments())
	if len(segs) == 0 {
		return nil
	}

	at := make([]string, 0, len(segs))
	for _, seg := range segs {
		at = append(at, seg.Text())
	}

	s.wrote[strings.Join(at, ".")] = v

	return nil
}

// TestTheRootCheckStandsBecauseTheEmptyPathIsSilent demonstrates the failure
// mode rather than asserting the refusal a second time.
//
// A root leaf mints the empty path, which renders as nothing and is therefore
// not distinguishable from no address at all, and a sink handed one writes
// nothing and reports success.
//
// A root map or a root slice is the same hole reached by the other door, and it
// is the value a first run most often has: a nil or empty one writes a Null at
// its own address, that address is the empty path, and the whole dump is one
// write nobody can see.
func TestTheRootCheckStandsBecauseTheEmptyPathIsSilent(t *testing.T) {
	t.Parallel()

	if got := At().String(); got != "" {
		t.Fatalf("the empty path renders as %q, want nothing at all", got)
	}

	sink := &treeSink{wrote: map[string]Value{}}

	if err := Dump(t.Context(), plainStruct{}, sink); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if len(sink.wrote) != 2 {
		t.Fatalf("the sink wrote %v, and it is meant to work for an ordinary schema", sink.wrote)
	}

	for _, v := range []Value{String("the whole configuration"), Null()} {
		mustWriteNothingAtTheEmptyPath(t, sink, v)
	}
}

// mustWriteNothingAtTheEmptyPath is the failure mode itself: a sink asked to put
// a value nowhere has done exactly what it was asked, and reports success.
func mustWriteNothingAtTheEmptyPath(t *testing.T, sink *treeSink, v Value) {
	t.Helper()

	before := len(sink.wrote)
	if err := sink.Set(t.Context(), At(), v); err != nil {
		t.Fatalf("the sink refused the empty path with %v, which is not the failure mode", err)
	}

	if len(sink.wrote) != before {
		t.Errorf("the sink wrote %v at the empty path, and the refusal exists because it writes nothing",
			sink.wrote)
	}
}

// TestARefusalAtACompositeReachesTheCaller is the other half of every rule
// above: a driver that says no is reported rather than read as absence, at a
// container address, at an array's own address, and inside a pointer to a leaf.
//
// A non-nil error must reach the caller as an error and never as an Absent,
// which is the conformance case a total backend outage otherwise loads as an
// all-zero struct with a nil error.
func TestARefusalAtACompositeReachesTheCaller(t *testing.T) {
	t.Parallel()

	t.Run("a plane that refuses a container address on load", aRefusedContainerOnLoad)
	t.Run("and on dump", aRefusedContainerOnDump)
	t.Run("a plane that cannot list an array", aRefusedListing)
	t.Run("and text a pointed leaf cannot parse", anUnparseablePointedLeaf)
}

func aRefusedContainerOnLoad(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	p.fail[At("opt")] = errors.New("no read ACL for this address")

	got, err := Load[optConf](t.Context(), planeSource{p: p})
	if got != (optConf{}) {
		t.Errorf("a failed load yielded %+v, want the zero value", got)
	}

	mustBeClass(t, err, ErrPlane, ErrDriver)
}

func aRefusedContainerOnDump(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	p.fail[At("opt")] = errors.New("no write ACL for this address")

	mustBeClass(t, Dump(t.Context(), optConf{}, planeSink{p: p}), ErrPlane, ErrDriver)
}

func aRefusedListing(t *testing.T) {
	t.Parallel()

	src := &listing{listErr: errors.New("no list capability on this token")}

	_, err := Load[arrayConf](t.Context(), src)
	mustBeClass(t, err, ErrPlane, ErrDriver)
}

func anUnparseablePointedLeaf(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{At("port"): Number("nope")})

	got, err := Load[pointedLeaf](t.Context(), planeSource{p: p})
	if got.Port != nil {
		t.Errorf("a failed parse published %d, want the pointer left alone", *got.Port)
	}

	report := reportOf(err)
	if want := "/port: the plane's value is not a valid int"; !strings.Contains(report, want) {
		t.Errorf("report\n\t%s\ndoes not contain\n\t%s", report, want)
	}

	mustBeClass(t, err, ErrValue)
}

// deepInner and deepOuter nest one optional section inside another, which is the
// shape that decides how a compiled node addresses its Go value.
//
// An index path rooted at the whole value would step through the outer pointer,
// and reflect.Value.FieldByIndex panics on a nil one - so it would panic at
// exactly the field whose nil-ness is the answer. A path relative to the
// container above cannot.
type (
	deepInner struct {
		Cred *cred     `ferry:"cred"`
		Arr  [2]string `ferry:"arr"`
	}
	deepOuter struct {
		Name string     `ferry:"name"`
		Sect *deepInner `ferry:"sect"`
	}
)

// TestOneOptionalSectionInsideAnother round-trips the nesting, and asserts that
// the outer section is materialised by presence arriving from two levels down.
func TestOneOptionalSectionInsideAnother(t *testing.T) {
	t.Parallel()

	t.Run("both sections set", bothSectionsRoundTrip)
	t.Run("and presence two levels down materialises both", presenceReachesTheOuterSection)
}

func bothSectionsRoundTrip(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	v := deepOuter{Name: "svc", Sect: &deepInner{Cred: &cred{User: "u"}, Arr: [2]string{"x", "y"}}}

	if err := Dump(t.Context(), v, planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	mustBeAddresses(t, slices.Collect(p.bound.All()), []string{
		"/name", "/sect", "/sect/arr#0", "/sect/arr#1", "/sect/cred", "/sect/cred/pass", "/sect/cred/user",
	})

	got, err := Load[deepOuter](t.Context(), planeSource{p: p})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got.Sect == nil || got.Sect.Arr != v.Sect.Arr {
		t.Fatalf("loaded %+v, want the section back", got)
	}

	mustBeCred(t, got.Sect.Cred, v.Sect.Cred)
}

func presenceReachesTheOuterSection(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{At("sect", "cred", "user"): String("u")})

	got, err := Load[deepOuter](t.Context(), planeSource{p: p})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got.Sect == nil {
		t.Fatal("the outer section is nil, and the plane spoke two levels under it")
	}

	mustBeCred(t, got.Sect.Cred, &cred{User: "u"})
}

// boxedComposites are the pointer shapes whose element is not a struct: an
// array, whose members are positions rather than fields, and a byte slice, which
// is a leaf and so takes no container address at all.
type boxedComposites struct {
	Arr   *[2]string `ferry:"arr"`
	Bytes *[]byte    `ferry:"bytes"`
}

// TestAPointerToSomethingOtherThanAStruct is the case that decides how a
// container reaches its one member.
//
// reflect.Value.FieldByIndex panics on anything but a struct, at an empty index
// path as much as at a populated one, so a pointer whose member is reached that
// way fails at *[N]T rather than at the elements it was meant to reach.
func TestAPointerToSomethingOtherThanAStruct(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	blob := []byte{0, 0xff, 'A'}

	if err := Dump(t.Context(), boxedComposites{Arr: &[2]string{"x", "y"}, Bytes: &blob}, planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	// The array takes a container address and its two positions; the byte slice
	// is a leaf, so its pointer adds a null to one address rather than a second.
	mustBeAddresses(t, slices.Collect(p.bound.All()), []string{"/arr", "/arr#0", "/arr#1", "/bytes"})

	got, err := Load[boxedComposites](t.Context(), planeSource{p: p})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got.Arr == nil || *got.Arr != [2]string{"x", "y"} {
		t.Errorf("loaded %v, want the array back", got.Arr)
	}

	if got.Bytes == nil || !slices.Equal(*got.Bytes, blob) {
		t.Errorf("loaded %v, want %v", got.Bytes, blob)
	}
}
