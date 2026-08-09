package ferry

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
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

// boundBy is the address set a driver's Bind was handed for one dump, rendered
// kind by kind, because the kind is half of what a member is (ADR-0016).
func boundBy(t *testing.T, dump func(context.Context, Sink) error) []string {
	t.Helper()

	p := newPlane(map[Path]Value{})
	if err := dump(t.Context(), planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	return kinded(p.bound)
}

// mustBeMembers is mustBeAddresses at the typed set: the golden list names each
// member's kind as well as its address.
func mustBeMembers(t *testing.T, got, want []string) {
	t.Helper()

	if !slices.Equal(got, want) {
		t.Errorf("the address set is\n\t%v\nand the golden set is\n\t%v", got, want)
	}
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
		want: []string{"section /plain", "leaf /plain/pass", "leaf /plain/user"},
	}, {
		name: "a pointer mints none of its own, and one section address",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, pointerStruct{}, s) },
		want: []string{"section /opt", "leaf /opt/pass", "leaf /opt/user"},
	}, {
		name: "an array is a section minting exactly N Index segments",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, arrayOfLeaves{}, s) },
		want: []string{"section /ports", "leaf /ports#0", "leaf /ports#1", "leaf /ports#2"},
	}, {
		name: "an array of bytes is one leaf address, because it is a leaf",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, arrayOfBytes{}, s) },
		want: []string{"leaf /key"},
	}, {
		name: "and the four together, segment-wise",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, everyShape{}, s) },
		want: []string{
			"section /a", "section /a/plain", "leaf /a/plain/pass", "leaf /a/plain/user",
			"section /b", "section /b/opt", "leaf /b/opt/pass", "leaf /b/opt/user",
			"section /c", "section /c/ports", "leaf /c/ports#0", "leaf /c/ports#1", "leaf /c/ports#2",
			"section /d", "leaf /d/key",
			"section /e", "section /e#0", "leaf /e#0/pass", "leaf /e#0/user",
			"section /e#1", "leaf /e#1/pass", "leaf /e#1/user",
		},
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustBeMembers(t, boundBy(t, c.dump), c.want)
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

	mustBeMembers(t, boundBy(t, func(ctx context.Context, s Sink) error {
		return Dump(ctx, withHidden{}, s)
	}), []string{"leaf /name"})
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

	mustBeMembers(t, kinded(p.bound), []string{"leaf /*", "section /opt", "leaf /opt/pass", "leaf /opt/user"})

	asked := slices.Clone(p.got)
	slices.SortFunc(asked, Path.Compare)

	if want := leavesOf(p.bound); !slices.Equal(asked, want) {
		t.Errorf("the walk asked about\n\t%v\nand the driver's leaf addresses are\n\t%v", asked, want)
	}

	if want := []Path{At("opt")}; !slices.Equal(p.probed, want) {
		t.Errorf("the walk probed\n\t%v\nand the driver's section addresses are\n\t%v", p.probed, want)
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

// boxedTwice is two pointers over one leaf. Neither mints a segment and neither
// takes a container address, because a pointer to a leaf is a leaf: the two of
// them add a null to one address rather than a second address (ADR-0016).
type boxedTwice struct {
	P **int `ferry:"p"`
}

// compositeOverLeaf is a container address and a leaf address at one place: the
// map takes /p as a composite and the string takes /p as a leaf, so a value
// would sit where the plane is only ever asked whether the container is there.
type compositeOverLeaf struct {
	M    map[string]string `ferry:"p"`
	Flat string            `ferry:"p"`
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

	// Two pointers over one leaf are one address, so the type compiles: a
	// pointer to a leaf is a leaf and takes no container address of its own.
	t.Run("and two pointers over one leaf are one leaf address", func(t *testing.T) {
		t.Parallel()

		if err := Compile[boxedTwice](); err != nil {
			t.Fatalf("%+v", err)
		}

		mustBeMembers(t, boundBy(t, func(ctx context.Context, s Sink) error {
			return Dump(ctx, boxedTwice{}, s)
		}), []string{"leaf /p"})
	})

	run(t, []compileCase{{
		name:     "a leaf and a subtree at one segment is still rejected, naming every clash",
		run:      Compile[leafOverSubtree],
		want:     []string{"/db:", "a leaf address and a prefix of", "/DB", "/Deep/User", "/Deep/Pass"},
		elements: 2,
	}, {
		name:     "and a container address must be distinct from every leaf address",
		run:      Compile[compositeOverLeaf],
		want:     []string{"/p:", "a container address and a leaf address at once", "nowhere to be"},
		elements: 1,
	}})
}

// The maps-no-address cases, at every level: the root, a field, under a pointer,
// and under an array.
//
// The type is time.Location rather than netip.Addr, which is what these cases
// were written against and what ADR-0005 names first. The codec chain claims
// netip.Addr, big.Int and netip.AddrPort through their text pairs before the
// backstop is reached (ADR-0007), so they no longer exercise the rule; that
// they no longer do is asserted in codec_test.go. time.Location is the fourth
// of ADR-0005's four and is the one with no text pair, so it still arrives
// here.
type (
	nestedNoAddress struct {
		V time.Location `ferry:"v"`
	}
	pointedNoAddress struct {
		V *time.Location `ferry:"v"`
	}
	arrayNoAddress struct {
		V [3]time.Location `ferry:"v"`
	}
	oneGoodSibling struct {
		Name string        `ferry:"name"`
		Addr time.Location `ferry:"addr"`
	}
)

// TestAStructThatMapsNoAddressIsRefusedAtEveryLevel is ADR-0005's sharpest
// single line. Before the rule existed a dump of a struct with no exported
// field produced zero addresses and a nil error, and the value was silently and
// totally lost - which is worse than an unsupported type, because it looks
// supported.
//
// The check is at every level and not only at the root, because one mapped
// sibling would otherwise hide the loss.
func TestAStructThatMapsNoAddressIsRefusedAtEveryLevel(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name:     "at the root",
		run:      Compile[time.Location],
		want:     []string{"time.Location maps no address", "register a codec for it"},
		elements: 1,
	}, {
		name:     "at a field",
		run:      Compile[nestedNoAddress],
		want:     []string{"time.Location maps no address"},
		elements: 1,
	}, {
		name:     "under a pointer",
		run:      Compile[pointedNoAddress],
		want:     []string{"time.Location maps no address"},
		elements: 1,
	}, {
		name:     "under an array, once rather than N times",
		run:      Compile[arrayNoAddress],
		want:     []string{"time.Location maps no address"},
		elements: 1,
	}, {
		name:     "and one mapped sibling does not hide it",
		run:      Compile[oneGoodSibling],
		want:     []string{"time.Location maps no address"},
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

// listing is a plane that can list what it holds, which is what it takes to
// load a composite whose members come from the value.
//
// It is never asked to list a section: an array's children come from the type,
// so a section address reaches no Children call at all (ADR-0016).
type listing struct {
	values   map[Path]Value
	children map[Path][]Segment
	listErr  error

	// got is every leaf the walk asked about, and listed is every composite it
	// asked to enumerate.
	got    []Path
	listed []Path
}

func (l *listing) Bind(*AddressSet) (OpenFunc, error) {
	return func(context.Context) (Reader, error) { return l, nil }, nil
}

func (l *listing) Get(_ context.Context, addr LeafAddr) (Value, error) {
	l.got = append(l.got, addr.Path())

	return l.values[addr.Path()], nil
}

func (l *listing) Children(_ context.Context, addr CompositeAddr) ([]Segment, error) {
	l.listed = append(l.listed, addr.Path())

	return l.children[addr.Path()], l.listErr
}

// TestAnArrayIsNeverEnumerated is the other half of the array rule, and it is a
// property of the address model rather than a check the walk performs.
//
// An array is a section, so its address is a SectionAddr and Children takes a
// CompositeAddr: there is no call by which a plane could offer an index the
// array cannot hold, and none by which it could offer a name (ADR-0016, #264).
func TestAnArrayIsNeverEnumerated(t *testing.T) {
	t.Parallel()

	src := &listing{values: map[Path]Value{At("ports").Elem(1): String("b")}}

	got, err := Load[arrayConf](t.Context(), src)
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if want := [3]string{"", "b", ""}; got.Ports != want {
		t.Errorf("loaded %q, want %q", got.Ports, want)
	}

	if len(src.listed) != 0 {
		t.Errorf("the walk asked to enumerate %v, and an array's children come from its type", src.listed)
	}
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

	if got := p.presence[At("opt")]; got != PresenceNull {
		t.Errorf("/opt is %v, want a null", got)
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

	if slices.Contains(p.ensured, At("opt")) {
		t.Errorf("the walk answered at %v, and a realised container competes with its own children", p.ensured)
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
		plane: map[Path]Value{At("opt"): Null, At("opt", "user"): String("u")},
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

// TestAContainerAddressIsNeverAskedForAValue is what makes the container
// address safe to admit to the set, and it is now a property of the types
// rather than a refusal the walk makes.
//
// A container's own address is a SectionAddr, and Get takes a LeafAddr, so a
// plane that happens to hold something under a section's own name cannot have
// that something mistaken for the section's value: the question does not
// compile. The refusal this test used to assert - "the plane holds string at a
// container address" - has no call left to fire on (ADR-0016, #219).
func TestAContainerAddressIsNeverAskedForAValue(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{At("opt"): String("everything")})

	if _, err := Load[optConf](t.Context(), planeSource{p: p}); err != nil {
		t.Fatalf("load: %+v", err)
	}

	if slices.Contains(p.got, At("opt")) {
		t.Errorf("the walk asked for a value at %v, and a section is asked whether it is there", p.got)
	}

	if !slices.Contains(p.probed, At("opt")) {
		t.Errorf("the walk probed %v, want the section's own address among them", p.probed)
	}

	if !p.bound.Has(sectionOf(At("opt"))) || p.bound.Has(leafOf(At("opt"), KindAbsent)) {
		t.Errorf("the driver was bound to %v, want /opt as a section and not as a leaf", kinded(p.bound))
	}
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
		{name: "nil is a null", v: nil, want: Null},
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

	mustBeMembers(t, kinded(p.bound), []string{"leaf /port"})

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
	}})
}

// TestARootLeafCompilesToOneAddress is the other half of the root rule: a type
// that resolves to a leaf sits at the root address, which is an address, so it
// compiles and the plane is asked about exactly one place.
func TestARootLeafCompilesToOneAddress(t *testing.T) {
	t.Parallel()

	t.Run("a root leaf", bindsToTheRoot(func(src Source) error {
		_, err := Bind[int](src)

		return err
	}))

	t.Run("and a root pointer to a leaf", bindsToTheRoot(func(src Source) error {
		_, err := Bind[*int](src)

		return err
	}))

	t.Run("through Load", aRootLeafLoadsFromTheRoot)
	t.Run("and through Dump", aRootLeafWritesTheRoot)
}

// bindsToTheRoot is one bind that must succeed and hand the driver exactly one
// address, which is the root. Every root-leaf shape in this file is asserted
// through it, so "one address, and it is the root" is written once.
func bindsToTheRoot(bind func(Source) error) func(*testing.T) {
	return func(t *testing.T) {
		t.Parallel()

		p := newPlane(map[Path]Value{})
		if err := bind(planeSource{p: p}); err != nil {
			t.Fatalf("bind: %+v", err)
		}

		mustBeMembers(t, kinded(p.bound), []string{"leaf "})
	}
}

func aRootLeafLoadsFromTheRoot(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{{}: Number("7")})

	got, err := Load[int](t.Context(), planeSource{p: p})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got != 7 {
		t.Errorf("loaded %d, want the value the plane held at the root", got)
	}

	mustBeMembers(t, kinded(p.bound), []string{"leaf "})
}

func aRootLeafWritesTheRoot(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	if err := Dump(t.Context(), 7, planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	mustBeAddresses(t, p.set, []string{""})
}

// treeSink is a sink shaped like a document format: every segment but the last
// names a node and the last names the value.
//
// It is the shape that has no name for the root: handed the empty path there is
// no last segment to write at and no node to write it in. So it says so, which
// is the answer ADR-0004 asks of a plane that cannot hold an address, and it is
// what this fixture exists to demonstrate. Writing nothing and reporting success
// is the failure mode it used to have, and the one the refusal replaces.
type treeSink struct{ wrote map[string]Value }

func (s *treeSink) Bind(*AddressSet) (OpenWriterFunc, error) {
	return func(context.Context) (Writer, error) { return s, nil }, nil
}

// Set writes v at the name the last segment gives, under the node the ones
// before it name, and refuses the root.
func (s *treeSink) Set(_ context.Context, addr LeafAddr, v Value) error {
	return s.write(addr.Path(), v)
}

// Ensure is the same write at a container's own address, which is where the
// null a nil composite says lands.
func (s *treeSink) Ensure(_ context.Context, addr Container, p Presence) error {
	if p != PresenceNull {
		return nil
	}

	return s.write(addr.Path(), Null)
}

func (s *treeSink) write(addr Path, v Value) error {
	segs := slices.Collect(addr.Segments())
	if len(segs) == 0 {
		return errNoRootNode
	}

	at := make([]string, 0, len(segs))
	for _, seg := range segs {
		at = append(at, seg.Text())
	}

	s.wrote[strings.Join(at, ".")] = v

	return nil
}

// errNoRootNode is the tree-shaped sink saying it has no node to put the root's
// value in, which is what a plane with no name for an address says.
var errNoRootNode = errors.New("this document has no node at the root to write a value in")

// TestARootCompositeIsRefusedBecauseTheEmptyPathIsWhereItsNullWouldGo is why
// the refusal narrowed to maps, slices and arrays rather than lifting whole.
//
// A root leaf now has an address and a plane that cannot name it says so. A
// root map or slice does not get that: it is the value a first run most often
// has, a nil or empty one writes a Null at its own address, that address is the
// root, and the leaf that would have carried the members is never reached. So
// the compiler refuses it and nothing reaches a plane at all.
func TestARootCompositeIsRefusedBecauseTheEmptyPathIsWhereItsNullWouldGo(t *testing.T) {
	t.Parallel()

	if got := At().String(); got != "" {
		t.Fatalf("the root address renders as %q, want nothing at all", got)
	}

	sink := &treeSink{wrote: map[string]Value{}}

	if err := Dump(t.Context(), rootMap(nil), sink); err == nil {
		t.Fatal("Dump of a root map returned no error")
	}

	if len(sink.wrote) != 0 {
		t.Errorf("the sink was written %v for a schema that never compiled", sink.wrote)
	}
}

// TestATreeShapedSinkStillTakesAnOrdinarySchema keeps the fixture above honest:
// it refuses the root and nothing else.
func TestATreeShapedSinkStillTakesAnOrdinarySchema(t *testing.T) {
	t.Parallel()

	sink := &treeSink{wrote: map[string]Value{}}

	if err := Dump(t.Context(), plainStruct{}, sink); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if len(sink.wrote) != 2 {
		t.Fatalf("the sink wrote %v, and it is meant to work for an ordinary schema", sink.wrote)
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

	_, err := Load[sliceConf](t.Context(), src)
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
	if want := "/port: what is set here is not a valid int"; !strings.Contains(report, want) {
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

	mustBeMembers(t, kinded(p.bound), []string{
		"leaf /name", "section /sect", "section /sect/arr", "leaf /sect/arr#0", "leaf /sect/arr#1",
		"section /sect/cred", "leaf /sect/cred/pass", "leaf /sect/cred/user",
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

	// The array takes a section address and its two positions; the byte slice
	// is a leaf, so its pointer adds a null to one address rather than a second.
	mustBeMembers(t, kinded(p.bound), []string{"section /arr", "leaf /arr#0", "leaf /arr#1", "leaf /bytes"})

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

// rootEndpoint is #306's shape: a struct with tagged fields that the codec
// chain claims through its text pair, so it is one leaf wherever it sits.
//
// The fields are what made the defect silent. A struct with no exported field
// was already refused for mapping no address, and this one walked its two tags
// instead, compiled a section at /host and /port, and wrote the codec nowhere.
type rootEndpoint struct {
	Host string `ferry:"host"`
	Port int    `ferry:"port"`
}

func (e rootEndpoint) MarshalText() ([]byte, error) {
	return fmt.Appendf(nil, "%s:%d", e.Host, e.Port), nil
}

func (e *rootEndpoint) UnmarshalText(text []byte) error {
	host, port, ok := strings.Cut(string(text), ":")
	if !ok {
		return errors.New("no colon")
	}

	n, err := strconv.Atoi(port)
	if err != nil {
		return err
	}

	e.Host, e.Port = host, n

	return nil
}

// holdsEndpoint is the same type one field down, which is the comparison the
// rule is about: the codec is honoured identically at both positions, and only
// the address the leaf would sit at differs.
type holdsEndpoint struct {
	E rootEndpoint `ferry:"e"`
}

// TestARootTypeACodecClaimsCompilesAsALeaf is #306's ordering, unchanged: the
// registry and the chain are consulted at the root in the order they are
// consulted below it, and what the root compiled to is what it is (ADR-0007,
// ADR-0010).
//
// What moved is the answer. A type either of them claims is one leaf at the
// root address rather than a refusal, and the ordering is still what decides
// which of them claimed it.
func TestARootTypeACodecClaimsCompilesAsALeaf(t *testing.T) {
	t.Parallel()

	t.Run("the chain claims it, and the tagged fields no longer hide that",
		bindsToTheRoot(func(src Source) error {
			_, err := Bind[rootEndpoint](src)

			return err
		}))

	t.Run("and a registration claims it at the root as it does anywhere else",
		bindsToTheRoot(func(src Source) error {
			_, err := Bind[rootEndpoint](src, WithRegistry(MustRegistry(StringText[rootEndpoint]())))

			return err
		}))

	// netip.Addr has no exported field, so the maps-no-address backstop reached
	// it first: being a struct is not what decides the root.
	t.Run("and what the type compiled to decides, rather than its Go kind",
		bindsToTheRoot(func(src Source) error {
			_, err := Bind[netip.Addr](src)

			return err
		}))
}

// TestACodecdRootLoadsThroughTheRootAddress is the same rule at the verb: the
// plane is asked about the root and the codec reads what it answers.
func TestACodecdRootLoadsThroughTheRootAddress(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{{}: String("h:8080")})

	got, err := Load[rootEndpoint](t.Context(), planeSource{p: p})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if want := (rootEndpoint{Host: "h", Port: 8080}); got != want {
		t.Errorf("loaded %v, want %v", got, want)
	}

	mustBeAddresses(t, p.got, []string{""})
}

// TestACodecdRootIsRefusedByASinkWithNoNameForTheRoot is the write half, and
// the refusal moved with it: the compiler no longer stands in the way, so the
// sink shaped like a document is asked about the root and answers for itself.
func TestACodecdRootIsRefusedByASinkWithNoNameForTheRoot(t *testing.T) {
	t.Parallel()

	sink := &treeSink{wrote: map[string]Value{}}

	err := Dump(t.Context(), rootEndpoint{Host: "h", Port: 8080}, sink)
	if err == nil {
		t.Fatal("the tree-shaped sink took a value at the root and said nothing")
	}

	if !errors.Is(err, errNoRootNode) {
		t.Errorf("the report is %+v, want the sink's own refusal in the chain", err)
	}

	if len(sink.wrote) != 0 {
		t.Errorf("the sink wrote %v at an address it says it has no node for", sink.wrote)
	}
}

// TestTheSameTypeOneFieldDownIsTheLeafItAlwaysWas is the other half of the
// consistency #306 was about. The refusal is about the address the root has and
// never about the codec, so the codec has to still carry the value below it.
func TestTheSameTypeOneFieldDownIsTheLeafItAlwaysWas(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})

	if err := Dump(t.Context(), holdsEndpoint{E: rootEndpoint{Host: "h", Port: 8080}}, planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	// One address and not two: the codec collapsed the struct's own /host and
	// /port to the single leaf the tag named.
	mustBeMembers(t, kinded(p.bound), []string{"leaf /e"})

	got, err := Load[holdsEndpoint](t.Context(), planeSource{p: p})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if want := (rootEndpoint{Host: "h", Port: 8080}); got.E != want {
		t.Errorf("loaded %v, want %v", got.E, want)
	}
}

// rootOpaque carries no text pair, so nothing claims it unless a registry does.
// It is what separates the two schemas the cache has to keep apart.
type rootOpaque struct {
	V string `ferry:"v"`
}

// TestTwoRegistriesDisagreeingAboutTheRootCompileTwoSchemas pins the cache key,
// through Load rather than through Compile, because Compile discards its result
// and takes no cache entry at all (ADR-0010).
func TestTwoRegistriesDisagreeingAboutTheRootCompileTwoSchemas(t *testing.T) {
	t.Parallel()

	claims := WithRegistry(MustRegistry(StringValue(
		func(o rootOpaque) (string, error) { return o.V, nil },
		func(text string) (rootOpaque, error) { return rootOpaque{V: text}, nil },
	)))

	load := func(t *testing.T, at Path, opts ...Option) (rootOpaque, error) {
		t.Helper()

		return Load[rootOpaque](t.Context(), planeSource{p: newPlane(map[Path]Value{at: String("x")})}, opts...)
	}

	got, err := load(t, At("v"))
	if err != nil {
		t.Fatalf("with no registry the type is a section and must load: %+v", err)
	}

	if got.V != "x" {
		t.Errorf("loaded %v, want the section's own leaf", got)
	}

	// With the registry the same type is one leaf at the root, which is a
	// different schema over the same reflect.Type, read at a different address.
	claimed, err := load(t, Path{}, claims)
	if err != nil {
		t.Fatalf("with the registry the type is one root leaf and must load: %+v", err)
	}

	if claimed.V != "x" {
		t.Errorf("loaded %v, want the whole value the codec read at the root", claimed)
	}

	// And the second schema did not land in the first registry's slot, which is
	// the whole of what the registry component of the key buys.
	if again, err := load(t, At("v")); err != nil || again.V != "x" {
		t.Errorf("the second load against the first registry: %v, %+v", again, err)
	}
}
