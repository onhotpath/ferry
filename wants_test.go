package ferry

import (
	"context"
	"testing"
	"time"
	"unsafe"
)

// What a leaf address answers about the schema that minted it (ADR-0016).
//
// Everything here asserts through Bind and through what a driver was handed,
// which is the seam a driver sees, except the widths at the bottom: those are a
// fact about the value types themselves and an ADR quotes them.

// wantsProbe records the set its Bind was handed and every address its Get was
// asked about, and answers a value for each.
type wantsProbe struct {
	bound    *AddressSet
	asked    []LeafAddr
	values   map[Path]Value
	children map[Path][]Segment
}

func (p *wantsProbe) Bind(addrs *AddressSet) (OpenFunc, error) {
	p.bound = addrs

	return func(context.Context) (Reader, error) { return p, nil }, nil
}

func (p *wantsProbe) Get(_ context.Context, addr LeafAddr) (Value, error) {
	p.asked = append(p.asked, addr)

	return p.values[addr.Path()], nil
}

func (p *wantsProbe) Children(_ context.Context, addr CompositeAddr) ([]Segment, error) {
	return p.children[addr.Path()], nil
}

func (*wantsProbe) Probe(context.Context, Container) (SectionInfo, error) {
	return SectionPresent, nil
}

// wantsAll is one field per shape a leaf takes: the five type families plus an
// array, whose two element addresses are leaves the type determined.
type wantsAll struct {
	Flag  bool          `ferry:"flag"`
	Name  string        `ferry:"name"`
	Port  int           `ferry:"port"`
	Raw   []byte        `ferry:"raw"`
	Wait  time.Duration `ferry:"wait"`
	Pair  [2]bool       `ferry:"pair"`
	Extra string        `ferry:"extra"`
}

// TestWantsAnswersTheSchemasKindPerLeaf is the whole of what the accessor
// promises: one answer per address, and it is the schema's rather than the
// plane's.
func TestWantsAnswersTheSchemasKindPerLeaf(t *testing.T) {
	t.Parallel()

	p := &wantsProbe{}
	if _, err := Bind[wantsAll](p); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	want := map[Path]VKind{
		At("flag"):         KindBool,
		At("name"):         KindString,
		At("port"):         KindNumber,
		At("raw"):          KindBytes,
		At("wait"):         KindString,
		At("pair").Elem(0): KindBool,
		At("pair").Elem(1): KindBool,
		At("extra"):        KindString,
	}

	seen := 0

	for m := range p.bound.Seq() {
		leaf, ok := m.(LeafAddr)
		if !ok {
			continue
		}

		seen++

		if got := leaf.Wants(); got != want[leaf.Path()] {
			t.Errorf("%s wants %v, want %v", leaf, got, want[leaf.Path()])
		}
	}

	if seen != len(want) {
		t.Errorf("the set holds %d leaves, want %d", seen, len(want))
	}
}

// wantsFlags is a mapping whose members are leaves the value mints, which is
// the address no Bind ever sees.
type wantsFlags struct {
	Flags map[string]bool `ferry:"flags"`
}

// TestAMintedAddressCarriesTheSchemasAnswer is the dynamic half: the walk
// realises the address from the value and types it from the element the schema
// compiled, so a driver reads the same answer at an address it was never bound
// to.
func TestAMintedAddressCarriesTheSchemasAnswer(t *testing.T) {
	t.Parallel()

	p := &wantsProbe{
		children: map[Path][]Segment{At("flags"): {NameSegment("beta")}},
		values:   map[Path]Value{At("flags", "beta"): Bool(true)},
	}

	if _, err := Load[wantsFlags](t.Context(), p); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(p.asked) != 1 {
		t.Fatalf("the walk asked for %d addresses, want the one the value minted", len(p.asked))
	}

	if got := p.asked[0].Wants(); got != KindBool {
		t.Errorf("%s wants %v, want %v", p.asked[0], got, KindBool)
	}
}

// TestAnAddressIsEqualToTheOneTheSetHolds is the equality-versus-membership
// hazard, pinned.
//
// Membership compares the path and the address kind, so an address minted
// without the schema's answer is in the set and is a key in no table built from
// it. With no kindless mint the case is unwritable, and this is what says so if
// somebody adds a constructor back.
func TestAnAddressIsEqualToTheOneTheSetHolds(t *testing.T) {
	t.Parallel()

	p := &wantsProbe{}
	if _, err := Load[wantsAll](t.Context(), p); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(p.asked) == 0 {
		t.Fatal("the walk asked for no address, so there is nothing to compare")
	}

	table := tableOver(p.bound)

	for _, addr := range p.asked {
		if !p.bound.Has(addr) {
			t.Errorf("the set does not hold %s, which the walk handed the driver", addr)
		}

		if _, ok := table[addr]; !ok {
			t.Errorf("%s is in the set and is a key in no table built from it", addr)
		}
	}
}

// tableOver is what a driver builds at Bind: one entry per leaf, keyed by the
// address itself.
func tableOver(set *AddressSet) map[LeafAddr]string {
	table := map[LeafAddr]string{}

	for m := range set.Seq() {
		if leaf, ok := m.(LeafAddr); ok {
			table[leaf] = leaf.String()
		}
	}

	return table
}

// TestTheAddressWeighsWhatItWeighs holds the widths an ADR quotes, so a field
// added to an address cannot change them in silence.
func TestTheAddressWeighsWhatItWeighs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"Path", unsafe.Sizeof(Path{}), 16},
		{"SectionAddr", unsafe.Sizeof(SectionAddr{}), 16},
		{"LeafAddr", unsafe.Sizeof(LeafAddr{}), 24},
		{"CompositeAddr", unsafe.Sizeof(CompositeAddr{}), 24},
	}

	for _, c := range cases {
		t.Logf("%s is %d B", c.name, c.got)

		if c.got != c.want {
			t.Errorf("%s is %d B, want %d B", c.name, c.got, c.want)
		}
	}
}
