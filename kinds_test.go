package ferry

import (
	"context"
	"strings"
	"testing"
)

// The address set a driver is handed, by kind. Everything in this file asserts
// through Bind, which is the one seam a driver sees, and every case here was a
// defect that could not be stated at all while an address carried no kind
// (ADR-0016).

// kindsProbe records the address set its Bind was handed, and reads nothing.
type kindsProbe struct{ bound *AddressSet }

func (p *kindsProbe) Bind(addrs *AddressSet) (OpenFunc, error) {
	p.bound = addrs

	return func(context.Context) (Reader, error) { return p, nil }, nil
}

func (*kindsProbe) Get(context.Context, LeafAddr) (Value, error) { return Value{}, nil }

// boundSet compiles T and hands back the set core gave the driver.
func boundSet[T any](t *testing.T) *AddressSet {
	t.Helper()

	p := &kindsProbe{}
	if _, err := Bind[T](p); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	return p.bound
}

// kindLines is the set as one sorted list of "kind address", which is what makes
// two schemas that used to be indistinguishable distinguishable in a diff.
func kindLines(a *AddressSet) []string {
	var out []string

	for m := range a.Seq() {
		var kind string

		switch m.(type) {
		case LeafAddr:
			kind = "leaf"
		case SectionAddr:
			kind = "section"
		default:
			kind = "composite"
		}

		out = append(out, kind+" "+m.String())
	}

	return out
}

// TestThreeTypesAtOneTagCompileToThreeAddressSets is #239, which measured
// string, []string and map[string]string compiling to byte-identical sets, so a
// driver's container check at Bind could not fire and nothing said so.
func TestThreeTypesAtOneTagCompileToThreeAddressSets(t *testing.T) {
	t.Parallel()

	leaf := kindLines(boundSet[struct {
		Tags string `ferry:"tags"`
	}](t))

	list := kindLines(boundSet[struct {
		Tags []string `ferry:"tags"`
	}](t))

	mapping := kindLines(boundSet[struct {
		Tags map[string]string `ferry:"tags"`
	}](t))

	if got := strings.Join(leaf, ","); got != "leaf /tags" {
		t.Errorf("a string compiles to %q, want %q", got, "leaf /tags")
	}

	if got := strings.Join(list, ","); got != "composite /tags" {
		t.Errorf("a []string compiles to %q, want %q", got, "composite /tags")
	}

	if got := strings.Join(mapping, ","); got != "composite /tags" {
		t.Errorf("a map compiles to %q, want %q", got, "composite /tags")
	}

	if strings.Join(leaf, ",") == strings.Join(list, ",") {
		t.Error("a string and a []string still compile to one address set, so a driver cannot classify at Bind")
	}
}

// TestASetAnswersPerKind is the other half of the same rule: the kinds
// partition, so holding /tags as a composite answers nothing about /tags as a
// leaf.
func TestASetAnswersPerKind(t *testing.T) {
	t.Parallel()

	set := boundSet[struct {
		Tags []string `ferry:"tags"`
	}](t)

	if !set.Has(compositeOf(At("tags"))) {
		t.Error("the set does not hold /tags as a composite, which is what a []string determines")
	}

	if set.Has(leafOf(At("tags"))) {
		t.Error("the set holds /tags as a leaf, and a []string names no value at its own address")
	}

	if set.Has(sectionOf(At("tags"))) {
		t.Error("the set holds /tags as a section, and a []string's members come from the value")
	}
}

// TestASectionTakesAnAddressOfItsOwn is #219's mechanism: a nested struct used
// to take no address at all, so a flat driver had nothing bound at /home and
// read whatever the ambient environment happened to hold there.
func TestASectionTakesAnAddressOfItsOwn(t *testing.T) {
	t.Parallel()

	set := boundSet[struct {
		Home kindsHome `ferry:"home"`
	}](t)

	if !set.Has(sectionOf(At("home"))) {
		t.Error("the set does not hold /home as a section, so a driver is never told the address is a place")
	}

	if set.Has(leafOf(At("home"))) {
		t.Error("the set holds /home as a leaf, and no value is at a section's own address")
	}
}

// TestAnArrayIsASection is #255: compileArray recorded no container address at
// all, so core asked drivers about an address their Bind never saw and a schema
// that dumped cleanly could not load.
func TestAnArrayIsASection(t *testing.T) {
	t.Parallel()

	set := boundSet[struct {
		Pair [2]string `ferry:"pair"`
	}](t)

	if !set.Has(sectionOf(At("pair"))) {
		t.Error("the set does not hold /pair as a section, so core would ask about an address Bind never saw")
	}

	for _, want := range []Path{At("pair").Elem(0), At("pair").Elem(1)} {
		if !set.Has(leafOf(want)) {
			t.Errorf("the set does not hold %s, and an array's element addresses come from its type", want)
		}
	}
}

// TestAZeroLengthArrayIsRefused is #260: [0]T compiled clean, mapped no
// address, and its element type was never checked, so [0]chan int reached a
// shipped schema.
func TestAZeroLengthArrayIsRefused(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name: "a zero-length array maps no address",
		run: Compile[struct {
			Host string `ferry:"host"`
			Pad  [0]int `ferry:"pad"`
		}],
		want:     []string{"/pad:", "maps no address", "zero-length array"},
		elements: 1,
	}, {
		name: "and its element type is never reached, so the refusal has to be the array's",
		run: Compile[struct {
			Pad [0]chan int `ferry:"pad"`
		}],
		want:     []string{"/pad:", "maps no address"},
		elements: 1,
	}, {
		name: "through a map, which is the shape that used to compile clean",
		run: Compile[struct {
			M map[string][0]int `ferry:"m"`
		}],
		want:     []string{"maps no address"},
		elements: 1,
	}})
}

// TestTwoContainersAtOneAddressAreRefused is #225: nothing compared a container
// address against another container address, so two fields tagged at one
// address compiled clean and the dump realised the container and its children
// at once, losing one field's value in a round trip.
func TestTwoContainersAtOneAddressAreRefused(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name: "two optional sections at one address",
		run: Compile[struct {
			A *kindsSubA `ferry:"x"`
			B *kindsSubB `ferry:"x"`
		}],
		want:     []string{"/x:", "addressed by two containers", "write over the other's children"},
		elements: 1,
	}, {
		// This arm is #232: a map's container address sitting under another
		// field's subtree gained a map entry no value ever held, and it is the
		// same rule because it is the same mistake one kind over.
		name: "a section and a composite at one address",
		run: Compile[struct {
			A kindsSubA         `ferry:"x"`
			M map[string]string `ferry:"x"`
		}],
		want:     []string{"/x:", "addressed by two containers", "come from the value"},
		elements: 1,
	}})
}

type (
	kindsSubA struct {
		A string `ferry:"a"`
	}

	kindsSubB struct {
		B string `ferry:"b"`
	}

	// kindsHome is #219's shape: an ordinary nested struct, at the one segment
	// whose name a process environment is overwhelmingly likely to hold.
	kindsHome struct {
		Dir string `ferry:"dir"`
	}
)

// TestAnEmptyNameIsUnwritableFromATag is #233: the quoted empty token got past
// the emptiness check, which read the raw field text rather than the decoded
// token, and minted the empty Name segment the grammar says twice it cannot
// write.
func TestAnEmptyNameIsUnwritableFromATag(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name: "the quoted empty name",
		run: Compile[struct {
			A string `ferry:"''"`
			B string `ferry:"b"`
		}],
		want:     []string{"/A:", "the name is empty"},
		elements: 1,
	}, {
		name: "and the bare one, which was always refused",
		run: Compile[struct {
			A string `ferry:""`
		}],
		want:     []string{"the name is empty"},
		elements: 1,
	}})
}

// TestAMapKeyThatRendersEmptyIsRefused is #258: a value could mint the empty
// segment a tag cannot, and the dump returned nil after writing /m/ into the
// plane.
func TestAMapKeyThatRendersEmptyIsRefused(t *testing.T) {
	t.Parallel()

	var got []Path

	sink := &kindsSink{seen: &got}

	err := Dump(t.Context(), struct {
		M map[string]int `ferry:"m"`
	}{M: map[string]int{"": 1}}, sink)
	if err == nil {
		t.Fatal("dumping a map key that renders to empty text succeeded, so the plane holds /m/")
	}

	if !strings.Contains(err.Error(), "a key of this map is empty text") {
		t.Errorf("the refusal reads %q, and it has to name the map key the Go value rendered to nothing", err)
	}

	if len(got) != 0 {
		t.Errorf("the plane was written at %v, and the encode phase runs before any write", got)
	}
}

// TestAPlaneMemberSpelledWithAnEmptyNameIsRefused is #258's load side.
//
// The dump-side refusal shipped without a mirror, so a plane that enumerated an
// empty name loaded /m/ clean into a Go map and failed only when the same value
// was written back. An empty segment names no address at either end.
func TestAPlaneMemberSpelledWithAnEmptyNameIsRefused(t *testing.T) {
	t.Parallel()

	src := &listing{
		values:   map[Path]Value{At("m").At(""): String("v")},
		children: map[Path][]Segment{At("m"): {NameSegment("")}},
	}

	_, err := Load[struct {
		M map[string]string `ferry:"m"`
	}](t.Context(), src)
	if err == nil {
		t.Fatal("a member spelled with an empty name loaded, and the value it produced cannot be dumped back")
	}

	if !strings.Contains(err.Error(), "a member here with no name at all") {
		t.Errorf("the refusal reads %q, and it has to name the member the plane spelled without a name", err)
	}
}

// kindsSink records what reached the plane, so a refusal that arrives too late
// is visible as a write that happened.
type kindsSink struct{ seen *[]Path }

func (s *kindsSink) Bind(*AddressSet) (OpenWriterFunc, error) {
	return func(context.Context) (Writer, error) { return s, nil }, nil
}

func (s *kindsSink) Set(_ context.Context, addr LeafAddr, _ Value) error {
	*s.seen = append(*s.seen, addr.Path())

	return nil
}

func (*kindsSink) Ensure(context.Context, Container, Presence) error { return nil }

// Unset is what lets this sink be handed a schema holding a map at all: a plane
// that cannot forget an address is refused at the open, and what this fixture is
// about is a key the walk mints and not the capability check.
func (*kindsSink) Unset(context.Context, CompositeAddr) error { return nil }
