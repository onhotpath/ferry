package ferry

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// Every assertion in this file goes through Compile[T], Load, LoadOver and Dump.
// A compiler rule is asserted through the report Compile returns, an address-set
// rule through what a recording driver's Bind was handed, and a walk rule
// through what a plane was asked and what came back.
//
// What separates this file from composite_test.go is where the addresses come
// from. There the type determines them, so a golden set is a statement about the
// type; here the value determines them, so the same type has as many address
// sets as it has values and the golden set is the container address alone.

// The dynamic shapes, one type each, so a golden address set is a statement
// about one shape rather than about a fixture.
type (
	sliceConf struct {
		Name string   `ferry:"name"`
		Tags []string `ferry:"tags"`
	}
	mapConf struct {
		Name   string         `ferry:"name"`
		Limits map[string]int `ferry:"limits"`
	}
	boxedDynamic struct {
		Tags *[]string       `ferry:"tags"`
		Lim  *map[string]int `ferry:"lim"`
	}
	nestedDynamic struct {
		Matrix [][]string          `ferry:"matrix"`
		Groups map[string][]string `ferry:"groups"`
	}
	sliceOfStruct struct {
		Pool []cred `ferry:"pool"`
	}
	mapOfStruct struct {
		Users map[string]cred `ferry:"users"`
	}
	// limitsOnly and tagsOnly are the maps-no-address backstop's case: a struct
	// whose only field is a dynamic composite, so the only thing standing
	// between it and a refusal is the backstop counting minted address shapes.
	limitsOnly struct {
		Limits map[string]int `ferry:"limits"`
	}
	tagsOnly struct {
		Tags []string `ferry:"tags"`
	}
	mixedDynamic struct {
		Tags   []string       `ferry:"tags"`
		Limits map[string]int `ferry:"limits"`
	}
	intKeyed struct {
		M map[int]string `ferry:"m"`
	}
)

// treeSource is the test plane with the one capability the dynamic tier needs:
// it can list what it holds under an address.
//
// It is a second Source over the same contents rather than a method on plane
// itself, because Enumerator is discovered by assertion and a plane that has it
// cannot demonstrate what a plane without it does. The array test next door
// asserts that plane cannot enumerate, and that assertion has to keep holding.
type treeSource struct{ p *plane }

func (s treeSource) Bind(addrs *AddressSet) (OpenFunc, error) {
	s.p.bound = addrs

	if s.p.bindErr != nil {
		return nil, s.p.bindErr
	}

	return func(context.Context) (Reader, error) { return treeReader{p: s.p}, nil }, nil
}

// treeReader answers one address and lists what is under one.
type treeReader struct{ p *plane }

func (r treeReader) Get(ctx context.Context, addr Path) (Value, error) { return r.p.Get(ctx, addr) }

// Children lists the immediate children of prefix, segment-wise and with each
// child's segment kind intact, which is the whole of why the interface hands
// back addresses rather than names.
func (r treeReader) Children(_ context.Context, prefix Path) ([]Path, error) {
	seen := map[Path]bool{}
	out := []Path{}

	for addr := range r.p.values {
		if addr == prefix || !prefix.isPrefixOf(addr) {
			continue
		}

		if kid := firstStep(prefix, addr); !seen[kid] {
			seen[kid] = true
			out = append(out, kid)
		}
	}

	slices.SortFunc(out, Path.Compare)

	return out, nil
}

// firstStep is the immediate child of prefix that addr lies under, built by
// cutting the rendering at the next delimiter rather than by parsing it, so the
// child carries the stored address's own segment kind.
func firstStep(prefix, addr Path) Path {
	rest := addr.below(prefix).rendered
	if i := strings.IndexAny(rest[1:], delims); i >= 0 {
		rest = rest[:i+1]
	}

	return Path{rendered: prefix.rendered + rest}
}

// roundTrip dumps a value into a fresh plane and loads it back, which is the
// whole of what a dynamic composite has to survive: the addresses it minted on
// the way out are the ones enumeration has to hand back on the way in.
func roundTrip[T any](t *testing.T, v T) (T, *plane) {
	t.Helper()

	p := newPlane(map[Path]Value{})
	if err := Dump(t.Context(), v, planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	got, err := Load[T](t.Context(), treeSource{p: p})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	return got, p
}

// TestTheStaticAddressSetOfADynamicComposite is the golden case per composite
// whose addresses come from the value.
//
// The set is the container address and nothing else, because a slice's length
// and a map's keys are properties of the value: the compiler records the
// element as a shape the walk realises per member, and a shape is not an
// address. A driver is handed only addresses it can fetch, write, name and
// check, so it never sees /tags#* or /limits/*.
func TestTheStaticAddressSetOfADynamicComposite(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		dump func(context.Context, Sink) error
		want []string
	}{{
		name: "a slice mints its own address and no element address",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, sliceConf{}, s) },
		want: []string{"/name", "/tags"},
	}, {
		name: "and a map the same",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, mapConf{}, s) },
		want: []string{"/limits", "/name"},
	}, {
		name: "a pointer to either adds no second address",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, boxedDynamic{}, s) },
		want: []string{"/lim", "/tags"},
	}, {
		name: "and one nested inside another adds none either",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, nestedDynamic{}, s) },
		want: []string{"/groups", "/matrix"},
	}, {
		name: "a slice of structs contributes its own address alone",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, sliceOfStruct{}, s) },
		want: []string{"/pool"},
	}, {
		name: "and so does a map of them",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, mapOfStruct{}, s) },
		want: []string{"/users"},
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			bound := boundBy(t, c.dump)
			mustBeAddresses(t, bound, c.want)
			mustHoldNoShape(t, bound)
		})
	}
}

// mustHoldNoShape holds an address set to carrying no wildcard shape.
//
// Handing one to a driver is wrong three ways and the third is silent: there is
// nothing at it to fetch, nothing at it to write, and "*" is ordinary segment
// text under ferry's escaping model rather than a marker, so a schema whose map
// genuinely holds the key "*" would render one plane key for two members of the
// set (ADR-0003).
func mustHoldNoShape(t *testing.T, got []Path) {
	t.Helper()

	for _, p := range got {
		if strings.Contains(p.String(), "/"+wildcard) {
			t.Errorf("the address set holds %s, which is a shape and not an address", p)
		}
	}
}

// TestTheMapsNoAddressBackstopCountsMintedShapes is the case a prototype got
// wrong three probes after writing the check, because every earlier fixture gave
// each struct a scalar field too.
//
// A dynamic composite contributes no static leaf address, so a backstop counting
// those refuses struct{ Limits map[string]int } - a struct whose one field is
// the whole configuration. What it has to count is the address shapes the type
// mints, which a slice and a map both do.
func TestTheMapsNoAddressBackstopCountsMintedShapes(t *testing.T) {
	t.Parallel()

	compiles := []struct {
		name string
		run  func(...Option) error
	}{
		{"a map is the whole of a struct", Compile[limitsOnly]},
		{"a slice is", Compile[tagsOnly]},
		{"a map of structs is", Compile[mapOfStruct]},
		{"a slice of structs is", Compile[sliceOfStruct]},
		{"and a composite inside a composite is", Compile[nestedDynamic]},
	}

	for _, c := range compiles {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if err := c.run(); err != nil {
				t.Errorf("%+v", err)
			}
		})
	}
}

// mapOfNothing and sliceOfNothing are the other side of the backstop: the shape
// is minted and contributes nothing, so the loss the rule exists for is back.
type (
	mapOfNothing struct {
		M map[string]time.Location `ferry:"m"`
	}
	sliceOfNothing struct {
		S []time.Location `ferry:"s"`
	}
	mapOfAny struct {
		M map[string]any `ferry:"m"`
	}
)

// TestAnElementThatMapsNoAddressIsRefusedAtItsShape is the backstop still
// firing, one level down, where the loss it catches is the same one.
//
// The refusal locates at the shape rather than at a realised address, because
// there is no value in hand at schema compile and every element would give the
// same diagnosis anyway.
func TestAnElementThatMapsNoAddressIsRefusedAtItsShape(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name:     "a map of a struct that maps no address",
		run:      Compile[mapOfNothing],
		want:     []string{"/M/*:", "time.Location maps no address"},
		elements: 1,
	}, {
		name:     "a slice of one",
		run:      Compile[sliceOfNothing],
		want:     []string{"/S/*:", "time.Location maps no address"},
		elements: 1,
	}, {
		name:     "and a map of a type outside the set entirely",
		run:      Compile[mapOfAny],
		want:     []string{"/m/*:", "interface {} is not a type ferry maps to an address", "register a codec"},
		elements: 1,
	}})
}

// TestACompositeWithNoElementsWritesNullAtItsOwnAddress is the forced collision.
//
// Three Go states meet two observations at a container address: measured through
// a real YAML plane, a missing key, an empty list and an empty mapping are one
// observation. The draft that chose the other collision - Null for nil and
// nothing at all for empty - made a map key whose value minted nothing vanish
// entirely, which is the silently dropped entry the whole design rules out.
func TestACompositeWithNoElementsWritesNullAtItsOwnAddress(t *testing.T) {
	t.Parallel()

	var (
		emptySlice = []string{}
		emptyMap   = map[string]int{}
	)

	cases := []struct {
		name string
		dump func(context.Context, Sink) error
		at   Path
	}{{
		name: "a nil slice",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, sliceConf{}, s) },
		at:   At("tags"),
	}, {
		name: "an empty slice, which is the same observation",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, sliceConf{Tags: emptySlice}, s) },
		at:   At("tags"),
	}, {
		name: "a nil map",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, mapConf{}, s) },
		at:   At("limits"),
	}, {
		name: "an empty map",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, mapConf{Limits: emptyMap}, s) },
		at:   At("limits"),
	}, {
		name: "a nil pointer to a slice",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, boxedDynamic{}, s) },
		at:   At("tags"),
	}, {
		name: "a pointer to an empty slice, which is one address carrying one value",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, boxedDynamic{Tags: &emptySlice}, s) },
		at:   At("tags"),
	}, {
		name: "a nil slice of slices",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, nestedDynamic{}, s) },
		at:   At("matrix"),
	}, {
		name: "and a nil map of slices",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, nestedDynamic{}, s) },
		at:   At("groups"),
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustWriteOneNull(t, c.dump, c.at)
		})
	}
}

// mustWriteOneNull holds a dump to writing a Null at one address and nothing
// under it, which is what "one address carrying one value" means when the
// address is a container's own.
func mustWriteOneNull(t *testing.T, dump func(context.Context, Sink) error, at Path) {
	t.Helper()

	p := newPlane(map[Path]Value{})
	if err := dump(t.Context(), planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if got := p.values[at]; got != Null() {
		t.Errorf("%s holds %#v, want a null", at, got)
	}

	if n := countWrites(t, p.set, at); n != 1 {
		t.Errorf("the walk wrote %s %d times, want one address carrying one value", at, n)
	}
}

// countWrites counts the writes at one address and reports every write beneath
// it, because a container address is never realised at the same time as anything
// under it.
func countWrites(t *testing.T, set []Path, at Path) int {
	t.Helper()

	n := 0

	for _, w := range set {
		switch {
		case w == at:
			n++
		case at.isPrefixOf(w):
			t.Errorf("the walk wrote %s beneath an empty composite, which has nothing beneath it", w)
		default:
			// A write somewhere else in the schema entirely, which this address
			// has nothing to say about.
		}
	}

	return n
}

// TestAnEmptyCompositeLoadsBackAsNil is the other half of the collision: the
// normalisation lands on the Go zero value rather than on a manufactured
// non-zero one, so nil and empty go out as one observation and come back as the
// one value the type already gives an unset composite.
func TestAnEmptyCompositeLoadsBackAsNil(t *testing.T) {
	t.Parallel()

	t.Run("a slice and a map", anEmptySliceAndMapLoadBackAsNil)
	t.Run("a pointer to either, which adds no bit", anEmptyBoxedCompositeLoadsBackAsNil)
	t.Run("and one nested inside another", anEmptyNestedCompositeLoadsBackAsNil)
}

func anEmptySliceAndMapLoadBackAsNil(t *testing.T) {
	t.Parallel()

	got, _ := roundTrip(t, mixedDynamic{Tags: []string{}, Limits: map[string]int{}})
	if got.Tags != nil || got.Limits != nil {
		t.Errorf("loaded %+v, want both nil", got)
	}
}

func anEmptyBoxedCompositeLoadsBackAsNil(t *testing.T) {
	t.Parallel()

	empty := []string{}

	got, _ := roundTrip(t, boxedDynamic{Tags: &empty})
	if got.Tags != nil || got.Lim != nil {
		t.Errorf("loaded %+v, want both nil: a pointer adds no bit at a composite", got)
	}
}

func anEmptyNestedCompositeLoadsBackAsNil(t *testing.T) {
	t.Parallel()

	got, _ := roundTrip(t, nestedDynamic{Matrix: [][]string{}, Groups: map[string][]string{}})
	if got.Matrix != nil || got.Groups != nil {
		t.Errorf("loaded %+v, want both nil", got)
	}
}

// TestAMapKeyWhoseValueIsAnEmptyCompositeDoesNotVanish is the probe that killed
// the other collision, run as a rule.
//
// A map key's existence is signalled only by having addresses under it, so a key
// whose value mints nothing is not enumerable at all: measured on the draft,
// map[string][]string{"a":{"x"}, "b":nil, "c":{}} loaded back without "c".
func TestAMapKeyWhoseValueIsAnEmptyCompositeDoesNotVanish(t *testing.T) {
	t.Parallel()

	v := nestedDynamic{
		Matrix: [][]string{{"a"}, {}, nil},
		Groups: map[string][]string{"a": {"x"}, "b": nil, "c": {}},
	}

	got, _ := roundTrip(t, v)

	if len(got.Groups) != 3 {
		t.Fatalf("loaded %v, and every key was written", got.Groups)
	}

	if !slices.Equal(got.Groups["a"], []string{"x"}) {
		t.Errorf("the key a loaded %v, want [x]", got.Groups["a"])
	}

	for _, k := range []string{"b", "c"} {
		if v, ok := got.Groups[k]; !ok || v != nil {
			t.Errorf("the key %s loaded %v (present %v), want a present key holding nil", k, v, ok)
		}
	}

	if want := [][]string{{"a"}, nil, nil}; !slices.EqualFunc(got.Matrix, want, slices.Equal) {
		t.Errorf("loaded %v, want %v: an empty inner element is nil and is still there", got.Matrix, want)
	}
}

// TestADynamicCompositeRoundTripsThroughItsElementAddresses is the ordinary
// case, and it is where a realised address that is not the compiled one is
// actually built: a struct under a map is compiled once at /users/* and walked
// at /users/root.
func TestADynamicCompositeRoundTripsThroughItsElementAddresses(t *testing.T) {
	t.Parallel()

	t.Run("a map of structs", aMapOfStructsRoundTrips)
	t.Run("a slice of structs", aSliceOfStructsRoundTrips)
	t.Run("a map of optional sections", aMapOfPointersRoundTrips)
	t.Run("a map of arrays, where a static tier sits under a dynamic one", aMapOfArraysRoundTrips)
	t.Run("and every key type core admits", everyAdmittedKeyTypeRoundTrips)
}

// mapOfPointer is an optional section under an address that came from a value,
// which is where the two tiers meet: the pointer's own address is realised per
// key and is the one its Null sits at.
type mapOfPointer struct {
	Users map[string]*cred `ferry:"users"`
}

func aMapOfPointersRoundTrips(t *testing.T) {
	t.Parallel()

	v := mapOfPointer{Users: map[string]*cred{"root": {User: "u"}, "nobody": nil}}

	got, p := roundTrip(t, v)

	if got.Users["nobody"] != nil {
		t.Errorf("the nil section loaded %+v, want a present key holding nil", got.Users["nobody"])
	}

	mustBeCred(t, got.Users["root"], v.Users["root"])
	mustBeAddresses(t, sorted(p.set), []string{"/users/nobody", "/users/root/pass", "/users/root/user"})
}

func aMapOfStructsRoundTrips(t *testing.T) {
	t.Parallel()

	v := mapOfStruct{Users: map[string]cred{"root": {User: "u", Pass: "p"}}}

	got, p := roundTrip(t, v)
	if got.Users["root"] != v.Users["root"] {
		t.Errorf("loaded %+v, want %+v", got.Users, v.Users)
	}

	mustBeAddresses(t, sorted(p.set), []string{"/users/root/pass", "/users/root/user"})
}

func aSliceOfStructsRoundTrips(t *testing.T) {
	t.Parallel()

	v := sliceOfStruct{Pool: []cred{{User: "a"}, {User: "b"}}}

	got, p := roundTrip(t, v)
	if len(got.Pool) != 2 || got.Pool[1].User != "b" {
		t.Errorf("loaded %+v, want %+v", got.Pool, v.Pool)
	}

	mustBeAddresses(t, sorted(p.set), []string{
		"/pool#0/pass", "/pool#0/user", "/pool#1/pass", "/pool#1/user",
	})
}

// mapOfArray is an array under an address that came from a value, so the array's
// own element addresses are static in shape and realised per key: /pairs/*#0 is
// what the compiler holds and /pairs/a#0 is what the plane is asked.
type mapOfArray struct {
	Pairs map[string][2]string `ferry:"pairs"`
}

func aMapOfArraysRoundTrips(t *testing.T) {
	t.Parallel()

	v := mapOfArray{Pairs: map[string][2]string{"a": {"x", "y"}}}

	got, p := roundTrip(t, v)
	if got.Pairs["a"] != v.Pairs["a"] {
		t.Errorf("loaded %v, want %v", got.Pairs, v.Pairs)
	}

	mustBeAddresses(t, sorted(p.set), []string{"/pairs/a#0", "/pairs/a#1"})
}

func everyAdmittedKeyTypeRoundTrips(t *testing.T) {
	t.Parallel()

	got, p := roundTrip(t, intKeyed{M: map[int]string{7: "seven", -1: "minus"}})
	if got.M[7] != "seven" || got.M[-1] != "minus" {
		t.Errorf("loaded %v, want the signed keys back", got.M)
	}

	mustBeAddresses(t, sorted(p.set), []string{"/m/-1", "/m/7"})

	up, p := roundTrip(t, uintKeyed{M: map[uint8]string{255: "max"}})
	if up.M[255] != "max" {
		t.Errorf("loaded %v, want an unsigned key back", up.M)
	}

	mustBeAddresses(t, p.set, []string{"/m/255"})

	// Identity before kind at the key position too: a kind-first resolution
	// would address /m/30000000000, because time.Duration's kind is int64.
	back, p := roundTrip(t, durationKeyed{M: map[time.Duration]string{30 * time.Second: "half"}})
	if back.M[30*time.Second] != "half" {
		t.Errorf("loaded %v, want a duration key rendered as ferry renders one", back.M)
	}

	mustBeAddresses(t, p.set, []string{"/m/30s"})
}

// durationKeyed and uintKeyed are the identity table declaring key
// admissibility for one of its two entries and not the other, and the unsigned
// half of the kind table.
type (
	durationKeyed struct {
		M map[time.Duration]string `ferry:"m"`
	}
	uintKeyed struct {
		M map[uint8]string `ferry:"m"`
	}
	portsOnly struct {
		Ports []int `ferry:"ports"`
	}
)

func sorted(addrs []Path) []Path {
	out := slices.Clone(addrs)
	slices.SortFunc(out, Path.Compare)

	return out
}

// TestLoadingADynamicCompositeNeedsASourceThatCanList is the asymmetry, stated
// as a refusal rather than left to be discovered.
//
// Dump covers every address always, because the value is in hand. Load covers
// the static addresses always and a dynamic one only from a source that can
// list, and the interface cannot be required: a Vault token with read and no
// list is ordinary. So the refusal names the field and the source, and it is
// never a silently empty map.
func TestLoadingADynamicCompositeNeedsASourceThatCanList(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{At("name"): String("svc")})

	if _, ok := any(p.reader()).(Enumerator); ok {
		t.Fatal("the plane can enumerate, so this test asserts nothing about a source that cannot")
	}

	_, err := Load[mapConf](t.Context(), planeSource{p: p})

	report := reportOf(err)
	for _, want := range []string{"/limits:", "map[string]int", "cannot list", "ferry.staged"} {
		if !strings.Contains(report, want) {
			t.Errorf("report\n\t%s\ndoes not name\n\t%s", report, want)
		}
	}

	mustBeClass(t, err, ErrPlane)

	// The same plane through a source that can list, so what is being refused is
	// the capability and not the contents.
	got, err := Load[mapConf](t.Context(), treeSource{p: p})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got.Name != "svc" || got.Limits != nil {
		t.Errorf("loaded %+v, want the leaf and an absent map left alone", got)
	}
}

// TestANullAtTheContainerAddressNeedsNoEnumerator is the reason the container's
// own address is asked before the members are: a Null is a complete answer, and
// a source that cannot list can still give it.
func TestANullAtTheContainerAddressNeedsNoEnumerator(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{At("tags"): Null()})

	got, err := LoadOver(t.Context(), tagsOnly{Tags: []string{"seed"}}, planeSource{p: p})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got.Tags != nil {
		t.Errorf("loaded %v, want nil: a null at a container address is the plane saying it holds nothing", got.Tags)
	}
}

// TestChildrenReturnsKindedAddresses is why the interface hands back addresses
// rather than names.
//
// Given only text an emitter has one signal for "is this container a sequence",
// which is whether the segment looks like a base-10 integer, and that signal
// turns a map holding the key "0" into a list and destroys the key. So the plane
// says which composite it is, and the walk does not guess.
func TestChildrenReturnsKindedAddresses(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	v := mixedDynamic{Tags: []string{"a", "b"}, Limits: map[string]int{"0": 1, "rps": 2}}

	if err := Dump(t.Context(), v, planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	r := treeReader{p: p}

	kids, err := r.Children(t.Context(), At("tags"))
	if err != nil {
		t.Fatalf("children: %v", err)
	}

	mustBeKind(t, kids, Index, []string{"/tags#0", "/tags#1"})

	kids, err = r.Children(t.Context(), At("limits"))
	if err != nil {
		t.Fatalf("children: %v", err)
	}

	// The map's key "0" is the case the kind exists for: it renders as a Name
	// segment and nothing about the text says so.
	mustBeKind(t, kids, Name, []string{"/limits/0", "/limits/rps"})

	got, err := Load[mixedDynamic](t.Context(), treeSource{p: p})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got.Limits["0"] != 1 || len(got.Tags) != 2 {
		t.Errorf("loaded %+v, want the sequence and the mapping told apart", got)
	}
}

func mustBeKind(t *testing.T, got []Path, want SegmentKind, rendered []string) {
	t.Helper()

	mustBeAddresses(t, got, rendered)

	for _, kid := range got {
		if k := lastSegment(kid).Kind(); k != want {
			t.Errorf("%s arrives as a %s segment, want %s", kid, k, want)
		}
	}
}

// TestAPlaneThatContradictsTheContainerIsLoud is the other half of the kinded
// address: a plane answering with the wrong shape is refused rather than read
// through.
func TestAPlaneThatContradictsTheContainerIsLoud(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		load func(context.Context, Source) error
		src  *listing
		want string
	}{{
		name: "a name under a sequence",
		load: loadInto[tagsOnly],
		src:  &listing{children: map[Path][]Path{At("tags"): {At("tags", "0")}}},
		want: "the plane holds /tags/0 under a sequence of 1",
	}, {
		name: "a gap in a sequence",
		load: loadInto[tagsOnly],
		src: &listing{children: map[Path][]Path{
			At("tags"): {At("tags").Elem(0), At("tags").Elem(2)},
		}},
		want: "the plane holds /tags#2 under a sequence of 2",
	}, {
		name: "a position under a mapping",
		load: loadInto[limitsOnly],
		src:  &listing{children: map[Path][]Path{At("limits"): {At("limits").Elem(0)}}},
		want: "the plane holds /limits#0 under this mapping",
	}, {
		name: "an address that is not an immediate child",
		load: loadInto[limitsOnly],
		src:  &listing{children: map[Path][]Path{At("limits"): {At("limits", "a", "b")}}},
		want: "the plane holds /limits/a/b under this mapping",
	}, {
		name: "a key the type cannot parse",
		load: loadInto[intKeyed],
		src:  &listing{children: map[Path][]Path{At("m"): {At("m", "abc")}}},
		want: "/m/abc: the plane's value is not a valid int",
	}, {
		name: "and two keys that read back as one",
		load: loadInto[intKeyed],
		src:  &listing{children: map[Path][]Path{At("m"): {At("m", "1"), At("m", "01")}}},
		want: "two plane keys read back as one Go key",
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			err := c.load(t.Context(), c.src)

			if report := reportOf(err); !strings.Contains(report, c.want) {
				t.Errorf("report\n\t%s\ndoes not contain\n\t%s", report, c.want)
			}

			mustBeClass(t, err, ErrValue)
		})
	}
}

// TestTheGapRefusalNamesARemedy is #120's correction. Every other refusal core
// writes ends in what to do about it, and this one used to state the rule and
// stop, which leaves a reader looking at TAGS_0 and TAGS_2 on a flat plane told
// what ferry requires and nothing about how to satisfy it.
//
// The two remedies are the only two there are: make the sequence contiguous, or
// stop calling it a sequence, because a map is how a plane-chosen set of
// positions is spelled.
func TestTheGapRefusalNamesARemedy(t *testing.T) {
	t.Parallel()

	err := loadInto[tagsOnly](t.Context(), &listing{children: map[Path][]Path{
		At("tags"): {At("tags").Elem(0), At("tags").Elem(2)},
	}})

	report := reportOf(err)
	for _, want := range []string{"fill the gap", "as a map keyed by those positions"} {
		if !strings.Contains(report, want) {
			t.Errorf("report\n\t%s\ndoes not offer\n\t%s", report, want)
		}
	}
}

// loadInto is one Load[T] as a value, so a table can hold one per destination
// type without naming the type twice.
func loadInto[T any](ctx context.Context, src Source) error {
	_, err := Load[T](ctx, src)

	return err
}

// TestARefusalAtADynamicCompositeReachesTheCaller is the rule every plane
// question obeys: a non-nil error reaches the caller as an error and never as an
// Absent, which is what a total backend outage would otherwise load as.
func TestARefusalAtADynamicCompositeReachesTheCaller(t *testing.T) {
	t.Parallel()

	t.Run("a plane that refuses the container address", func(t *testing.T) {
		t.Parallel()

		p := newPlane(map[Path]Value{})
		p.fail[At("limits")] = errors.New("no read ACL for this address")

		_, err := Load[limitsOnly](t.Context(), treeSource{p: p})
		mustBeClass(t, err, ErrPlane, ErrDriver)
	})

	t.Run("a plane that cannot list this address", func(t *testing.T) {
		t.Parallel()

		src := &listing{listErr: errors.New("no list capability on this token")}

		_, err := Load[limitsOnly](t.Context(), src)
		mustBeClass(t, err, ErrPlane, ErrDriver)
	})

	t.Run("a plane holding a value at a container address", func(t *testing.T) {
		t.Parallel()

		p := newPlane(map[Path]Value{At("tags"): String("everything")})

		_, err := Load[tagsOnly](t.Context(), treeSource{p: p})

		want := "the plane holds string at a container address"
		if report := reportOf(err); !strings.Contains(report, want) {
			t.Errorf("report\n\t%s\ndoes not contain\n\t%s", report, want)
		}

		mustBeClass(t, err, ErrValue)
	})

	t.Run("a sequence element the type cannot take", func(t *testing.T) {
		t.Parallel()

		p := newPlane(map[Path]Value{At("ports").Elem(0): Number("nope")})

		got, err := Load[portsOnly](t.Context(), treeSource{p: p})
		if got.Ports != nil {
			t.Errorf("a failed element published %v, want the sequence never set", got.Ports)
		}

		want := "/ports#0: the plane's value is not a valid int"
		if report := reportOf(err); !strings.Contains(report, want) {
			t.Errorf("report\n\t%s\ndoes not contain\n\t%s", report, want)
		}

		mustBeClass(t, err, ErrValue)
	})

	t.Run("and a sink that refuses an element address", func(t *testing.T) {
		t.Parallel()

		p := newPlane(map[Path]Value{})
		p.fail[At("tags").Elem(1)] = errors.New("no write ACL for this address")

		err := Dump(t.Context(), tagsOnly{Tags: []string{"a", "b"}}, planeSink{p: p})
		mustBeClass(t, err, ErrPlane, ErrDriver)
	})
}

// mapKeyed is one map key type under test. The value type is a string leaf so
// that nothing but the key can refuse.
type mapKeyed[K comparable] struct {
	M map[K]string `ferry:"m"`
}

// TestAMapKeyTypeIsDeclaredUsableAsOnePerEntry is the rule ADR-0005 restated
// after core had exempted itself from it.
//
// The obligation is injectivity under Go's ==, because == is what a Go map's key
// identity is and therefore what decides how many entries the map holds. Nothing
// else confers admissibility, membership of core's own identity table included:
// time.Duration declares it and time.Time does not.
func TestAMapKeyTypeIsDeclaredUsableAsOnePerEntry(t *testing.T) {
	t.Parallel()

	admitted := []struct {
		name string
		run  func(...Option) error
	}{
		{"string", Compile[mapKeyed[string]]},
		{"a named type over string", Compile[mapKeyed[namedName]]},
		{"int", Compile[mapKeyed[int]]},
		{"int8", Compile[mapKeyed[int8]]},
		{"uint", Compile[mapKeyed[uint]]},
		{"uint64", Compile[mapKeyed[uint64]]},
		{"and time.Duration, which the identity table declares", Compile[mapKeyed[time.Duration]]},
	}

	for _, c := range admitted {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if err := c.run(); err != nil {
				t.Errorf("%+v", err)
			}
		})
	}
}

// namedName is a named type over an admitted key kind, admitted with it exactly
// as a leaf of that kind is.
type namedName string

// TestAnInadmissibleMapKeyIsRefusedAtSchemaCompile is the refusal, and its
// message names no remedy that does not exist.
//
// time.Time is {wall, ext, loc *Location} and == compares the loc pointer, so no
// text carries what distinguishes two values that differ only in it: measured,
// 0 of 12 standard-library encodings tell time.UTC from FixedZone("GMT", 0). A
// message offering "register an injective codec for it" would be naming a
// remedy that cannot be written.
func TestAnInadmissibleMapKeyIsRefusedAtSchemaCompile(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name: "time.Time, which is in core's own set",
		run:  Compile[mapKeyed[time.Time]],
		want: []string{
			"/m:", "time.Time is in core's own set and is not usable as a map key",
			"== compares the *Location and no text carries a pointer",
			"convert the key yourself",
		},
		elements: 1,
	}, {
		name:     "a float, because two NaN payloads both format as NaN",
		run:      Compile[mapKeyed[float64]],
		want:     []string{"/m:", "float64 is not usable as a map key", "both format as NaN"},
		elements: 1,
	}, {
		name:     "a bool, which ferry simply does not key a map by",
		run:      Compile[mapKeyed[bool]],
		want:     []string{"/m:", "bool is not usable as a map key", "a string or an integer kind"},
		elements: 1,
	}, {
		name:     "and an array, which is comparable and is still not one",
		run:      Compile[mapKeyed[[2]int]],
		want:     []string{"/m:", "[2]int is not usable as a map key", "register a codec"},
		elements: 1,
	}})
}

// TestNoRemedyIsOfferedForAKeyThatCannotHaveOne holds the two non-injective
// refusals to offering no codec, which is the same rule the permanent kind
// refusals obey: a remedy nobody can write is worse than no remedy.
func TestNoRemedyIsOfferedForAKeyThatCannotHaveOne(t *testing.T) {
	t.Parallel()

	for _, run := range []func(...Option) error{
		Compile[mapKeyed[time.Time]], Compile[mapKeyed[float64]],
	} {
		if report := reportOf(run()); strings.Contains(report, "register a codec") {
			t.Errorf("a refusal with no injective text form offers a codec:\n%s", report)
		}
	}
}

// twoMaps and sliceOverArray are the two shapes whose realised addresses collide
// with something the static tier cannot see them collide with.
//
// Neither map contributes a leaf address, so prefix-freeness has nothing to
// compare and the container check finds no leaf at /x either; the array's own
// element addresses are static and the slice's are not, so nothing at compile
// time can know they will meet.
type (
	twoMaps struct {
		A map[string]string `ferry:"x"`
		B map[string]string `ferry:"x"`
	}
	sliceOverArray struct {
		S []string  `ferry:"x"`
		A [2]string `ferry:"x"`
	}
)

// TestACollidingAddressIsRefusedAsItIsMinted is the dynamic tier, and the point
// is where the refusal lands: as the address is minted, before the write it
// belongs to, and never after it.
//
// The determinism argument is the stronger of the two and does not depend on
// anybody agreeing that a lost entry matters. There is no stable answer to give,
// because which of two writes survives is which the walk makes last, so the only
// outcome consistent with ADR-0001 is a refusal - which is why this is asserted
// as an outcome set over 200 dumps rather than as one draw from it.
func TestACollidingAddressIsRefusedAsItIsMinted(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		dump func(context.Context, Sink) error
		want []string
	}{{
		name: "two map keys rendering to one address",
		dump: func(ctx context.Context, s Sink) error {
			return Dump(ctx, twoMaps{
				A: map[string]string{"a": "1", "k": "2"},
				B: map[string]string{"k": "3", "z": "4"},
			}, s)
		},
		want: []string{"/x:", "/x/k is addressed more than once", "map[string]string"},
	}, {
		name: "and an element address the type already determined",
		dump: func(ctx context.Context, s Sink) error {
			return Dump(ctx, sliceOverArray{S: []string{"a"}}, s)
		},
		want: []string{"/x:", "/x#0 is addressed more than once", "[]string"},
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustBeOneOutcome(t, c.dump, c.want)
		})
	}
}

// dumpRuns is how many dumps of one value the determinism invariant is asserted
// over. Go's map iteration order is randomised, so a refusal that depended on it
// would be flaky rather than wrong, and a flaky refusal is the thing sorting the
// members by key text exists to prevent.
const dumpRuns = 200

func mustBeOneOutcome(t *testing.T, dump func(context.Context, Sink) error, want []string) {
	t.Helper()

	outcomes := map[string]int{}

	for range dumpRuns {
		err := dump(t.Context(), planeSink{p: newPlane(map[Path]Value{})})
		outcomes[reportOf(err)]++
	}

	if len(outcomes) != 1 {
		t.Fatalf("%d dumps of one value produced %d distinct outcomes: %v", dumpRuns, len(outcomes), outcomes)
	}

	for report := range outcomes {
		mustContain(t, report, want)
	}

	mustBeClass(t, dump(t.Context(), planeSink{p: newPlane(map[Path]Value{})}), ErrValue)
}

// TestMapMembersAreSortedByKeyText is ADR-0001's determinism invariant at the
// one place a Go map reaches a plane.
//
// The text is computed once per key rather than inside the comparator, which
// arrives as a speedup rather than a cost: the sort was already required, and a
// comparator that recomputes the text does so O(n log n) times and never
// compares two of them for equality - which is exactly what the duplicate check
// needs to have done.
func TestMapMembersAreSortedByKeyText(t *testing.T) {
	t.Parallel()

	v := limitsOnly{Limits: map[string]int{"z": 1, "a": 2, "m": 3, "b": 4}}
	want := []string{"/limits/a", "/limits/b", "/limits/m", "/limits/z"}

	orders := map[string]int{}

	for range dumpRuns {
		p := newPlane(map[Path]Value{})
		if err := Dump(t.Context(), v, planeSink{p: p}); err != nil {
			t.Fatalf("dump: %+v", err)
		}

		orders[fmt.Sprint(p.set)]++
	}

	if len(orders) != 1 {
		t.Fatalf("%d dumps of one map produced %d distinct orderings: %v", dumpRuns, len(orders), orders)
	}

	p := newPlane(map[Path]Value{})
	if err := Dump(t.Context(), v, planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	mustBeAddresses(t, p.set, want)
}

// requiredSlice and the three beside it are ADR-0006's refusals at the dynamic
// tier: required names an address, so it is admissible exactly where that
// address's children come from the type.
type (
	requiredSlice struct {
		S []string `ferry:"s,required"`
	}
	requiredMap struct {
		M map[string]int `ferry:"m,required"`
	}
	requiredBoxedSlice struct {
		S *[]string `ferry:"s,required"`
	}
	defaultedSlice struct {
		S []string `ferry:"s,default=x"`
	}
)

// TestRequiredIsRefusedOnADynamicComposite is the refusal carrying the remedy,
// because the user reaching for it has an intent that is simply not writable.
//
// The reading they want is that an explicit [] satisfies required while a
// missing key and a null do not. Measured through a real YAML plane, five
// documents give three distinct observations at a container address, and a
// missing key and origins: [] are one of them - so the rule cannot be written,
// and a seventh Value kind would not rescue it either, because env, query
// parameters and opaque KV cannot express an empty list at all.
func TestRequiredIsRefusedOnADynamicComposite(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name:     "on a slice",
		run:      Compile[requiredSlice],
		want:     []string{"/s:", "required is not available on []string", "a struct with a set flag"},
		elements: 1,
	}, {
		name:     "on a map",
		run:      Compile[requiredMap],
		want:     []string{"/m:", "required is not available on map[string]int"},
		elements: 1,
	}, {
		name:     "through a pointer to one, because the pointer adds no address of its own",
		run:      Compile[requiredBoxedSlice],
		want:     []string{"/s:", "required is not available on []string"},
		elements: 1,
	}, {
		name:     "and a default, which has no single address to sit at",
		run:      Compile[defaultedSlice],
		want:     []string{"/s:", "[]string is not a leaf", "seed the value instead"},
		elements: 1,
	}})
}

// seedConf is a seed holding both dynamic composites, which is where LoadOver's
// failure property is most likely to break.
type seedConf struct {
	Tags   []string       `ferry:"tags"`
	Limits map[string]int `ferry:"limits"`
}

func dynamicSeed() seedConf {
	return seedConf{Tags: []string{"seed"}, Limits: map[string]int{"seed": 1}}
}

// TestLoadOverNeverWritesThroughTheSeedsOwnBackingArrayOrBuckets is the property
// this ticket is most likely to have broken.
//
// over := seed is a shallow copy, which survives a pointer because both pointer
// paths allocate before publishing. A slice shares its backing array and a map
// shares its buckets outright, so a walk that filled the seed's own map would
// mutate a value the caller still holds and the property would break in silence
// rather than loudly. Both are therefore built fresh and published only once
// every member has landed.
func TestLoadOverNeverWritesThroughTheSeedsOwnBackingArrayOrBuckets(t *testing.T) {
	t.Parallel()

	t.Run("a successful load leaves the seed alone", aSuccessfulLoadLeavesTheDynamicSeedAlone)
	t.Run("and a failed one leaves it alone as well", aFailedLoadLeavesTheDynamicSeedAlone)
}

func aSuccessfulLoadLeavesTheDynamicSeedAlone(t *testing.T) {
	t.Parallel()

	seed := dynamicSeed()
	p := newPlane(map[Path]Value{
		At("tags").Elem(0):    String("plane"),
		At("limits", "plane"): Number("2"),
	})

	got, err := LoadOver(t.Context(), seed, treeSource{p: p})
	if err != nil {
		t.Fatalf("load over: %+v", err)
	}

	// ADR-0006: a struct merges field by field and a composite is replaced
	// wholesale, because the plane either has children under that address or it
	// does not, and if it has any then it has said what the composite is.
	if !slices.Equal(got.Tags, []string{"plane"}) || len(got.Limits) != 1 {
		t.Errorf("loaded %+v, want both composites replaced wholesale", got)
	}

	mustBeTheSeed(t, seed)
}

func aFailedLoadLeavesTheDynamicSeedAlone(t *testing.T) {
	t.Parallel()

	seed := dynamicSeed()
	p := newPlane(map[Path]Value{
		At("tags").Elem(0):    String("plane"),
		At("limits", "plane"): Number("not a number"),
	})

	got, err := LoadOver(t.Context(), seed, treeSource{p: p})
	if err == nil {
		t.Fatal("a plane holding an unparseable element loaded clean")
	}

	if !slices.Equal(got.Tags, seed.Tags) || got.Limits["seed"] != 1 {
		t.Errorf("a failed load yielded %+v, want the seed it was handed", got)
	}

	mustBeTheSeed(t, seed)
}

// mustBeTheSeed holds the caller's own value to being byte-for-byte what it was,
// through the slice's backing array and the map's buckets rather than only
// through the header.
func mustBeTheSeed(t *testing.T, seed seedConf) {
	t.Helper()

	if !slices.Equal(seed.Tags, []string{"seed"}) {
		t.Errorf("the seed's slice is %v, and the walk was never to write through it", seed.Tags)
	}

	if len(seed.Limits) != 1 || seed.Limits["seed"] != 1 {
		t.Errorf("the seed's map is %v, and the walk was never to write into it", seed.Limits)
	}
}
