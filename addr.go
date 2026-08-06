package ferry

import (
	"cmp"
	"iter"
	"slices"
)

// The three address types are sealed the way ADR-0009 seals a registration: one
// unexported field, so no package outside core can write the composite literal
// and no conversion into the struct type exists. A forged address is a compile
// error rather than a runtime refusal, and the schema compiler is the only thing
// that mints one (ADR-0016).
//
// They partition the address space and they are not interchangeable. /db as a
// SectionAddr and /db as a CompositeAddr are different addresses, and asking a
// set whether it holds one answers nothing about the other.

// LeafAddr is the address of a place a [Value] can be: what [Reader.Get] reads
// and what [Writer.Set] writes.
//
// It is comparable, so it is a map key with no encoding step, and a driver that
// classified the address set at [Source.Bind] can serve a read from a table it
// computed before any I/O.
//
// Read it with [LeafAddr.Path], which is what a key function walks.
type LeafAddr struct{ p Path }

// SectionAddr is the address of a place whose children are known from the type:
// a struct, an array, or either of those behind a pointer.
//
// A section is never enumerated, because its members come from the type rather
// than from the value. What a plane is asked about it is whether it is there,
// which is [Prober.Probe].
type SectionAddr struct{ p Path }

// CompositeAddr is the address of a place whose children come from the value: a
// slice or a map.
//
// Its members do not exist until there is a value, so [Load] discovers them
// through [Enumerator.Children] and a plane that cannot list reaches none of
// them. Which Go type produced it is not part of the address: a []string and a
// map[string]string are both a CompositeAddr, and a driver mints the [Name] or
// [Index] segments while the schema types the child they name.
type CompositeAddr struct{ p Path }

// Member is one address a compiled schema determines: a [LeafAddr], a
// [SectionAddr] or a [CompositeAddr], and nothing else.
//
// It is what [AddressSet.Seq] yields and what [AddressSet.Has] answers about. A
// driver classifies once, at [Source.Bind], with one range and one type switch
// on the cold path:
//
//	for m := range addrs.Seq() {
//	    switch a := m.(type) {
//	    case ferry.LeafAddr:      d.keys[a] = key(a.Path())
//	    case ferry.SectionAddr:   d.prefixes[a] = prefix(a.Path())
//	    case ferry.CompositeAddr: d.prefixes[a] = prefix(a.Path())
//	    }
//	}
//
// The set is closed: nothing outside core implements it, so the type switch
// above is total and needs no default arm that could quietly swallow a fourth
// kind.
type Member interface {
	// Path is the address with its kind dropped, which is what a key function
	// walks to build a plane key.
	Path() Path
	// String is the canonical rendering of the address.
	String() string

	member()
}

// Container is a [Member] that has children: a [SectionAddr] or a
// [CompositeAddr].
//
// It is what [Prober.Probe] is asked about and what [Ensurer.Ensure] writes at.
// A [LeafAddr] is not one, so asking whether a leaf is present, or writing a
// container-level answer at one, does not compile.
type Container interface {
	Member

	container()
}

// Path is the address with its kind dropped, which is what a [KeyFunc] walks to
// build a plane key. The kind is not part of a plane key, because a key is a
// function of the segments.
func (a LeafAddr) Path() Path { return a.p }

// Path is the section's address with its kind dropped.
func (a SectionAddr) Path() Path { return a.p }

// Path is the composite's address with its kind dropped.
func (a CompositeAddr) Path() Path { return a.p }

// String is the canonical rendering: /db/host, /tags#0. It identifies the
// address and it is not a plane key, for the reason [Path.String] gives.
func (a LeafAddr) String() string { return a.p.String() }

// String is the canonical rendering of the section's address.
func (a SectionAddr) String() string { return a.p.String() }

// String is the canonical rendering of the composite's address.
func (a CompositeAddr) String() string { return a.p.String() }

func (LeafAddr) member()      {}
func (SectionAddr) member()   {}
func (CompositeAddr) member() {}

func (SectionAddr) container()   {}
func (CompositeAddr) container() {}

// The three address types are usable directly as map keys, which is the
// property a driver's precomputed table rests on. It is a compile-time property
// rather than a documented promise, and this is where it is checked (ADR-0016).
var (
	_ map[LeafAddr]struct{}
	_ map[SectionAddr]struct{}
	_ map[CompositeAddr]struct{}
)

// The minting side is unexported, which is the whole of the sealing: only the
// compiler reaches these, and it reaches them from the node kind it decided
// (ADR-0016).

func leafAt(p Path) LeafAddr           { return LeafAddr{p: p} }
func sectionAt(p Path) SectionAddr     { return SectionAddr{p: p} }
func compositeAt(p Path) CompositeAddr { return CompositeAddr{p: p} }

// addrKind is how the compiler's own answer for a position travels to the
// places that need a typed address for it: the walk realises an address per
// member and types it from the node it is standing at.
type addrKind uint8

const (
	kindLeaf addrKind = iota
	kindSection
	kindComposite
)

// memberAt mints the address of one kind at one path. It is the one place a
// typed address is built from an untyped one, so the rule that the schema types
// every address is a rule with a single site (ADR-0016).
func memberAt(k addrKind, p Path) Member {
	switch k {
	case kindLeaf:
		return leafAt(p)
	case kindSection:
		return sectionAt(p)
	default:
		return compositeAt(p)
	}
}

// rank orders the kinds inside an address set, so that a set holding two kinds
// at one path is still enumerated in one stable order.
func rank(m Member) int {
	switch m.(type) {
	case LeafAddr:
		return int(kindLeaf)
	case SectionAddr:
		return int(kindSection)
	default:
		return int(kindComposite)
	}
}

// compareMembers is the set's ordering: segment-wise by address, and by kind
// where two members share one path.
func compareMembers(a, b Member) int {
	if c := a.Path().Compare(b.Path()); c != 0 {
		return c
	}

	return cmp.Compare(rank(a), rank(b))
}

// AddressSet is the set of addresses a compiled schema determines, and it is
// what [Source.Bind] and [Sink.Bind] are handed. Holding it before any I/O is
// what lets a driver precompute its plane keys once per schema and check them:
// see [NewKeys].
//
// Every member is typed: a [LeafAddr] for a place a value can be, a
// [SectionAddr] for a place whose children come from the type, a
// [CompositeAddr] for a place whose children come from the value. That is what
// lets a driver decide once, before any I/O, which question each address
// admits, rather than inferring it per call from the address text.
//
// It does not contain the addresses a value mints - a map key, a sequence index
// - because those do not exist until there is a value. A driver that treats its
// precomputed table as a closed set will refuse a legal write, which is why
// [Keys.Open] hands back a function rather than a map.
//
// It is sorted segment-wise, and [AddressSet.Seq] enumerates it in that order.
type AddressSet struct {
	// addrs is sorted by compareMembers and holds no duplicates.
	addrs []Member
}

// newAddressSet builds a set from the members given, sorting them segment-wise.
//
// It is unexported because the three address types are sealed: a constructor
// taking addresses from outside core would be the forging door the sealing
// exists to shut (ADR-0016).
func newAddressSet(members ...Member) *AddressSet {
	sorted := slices.Clone(members)
	slices.SortFunc(sorted, compareMembers)

	return &AddressSet{addrs: slices.Compact(sorted)}
}

// Len is how many addresses the set holds.
func (a *AddressSet) Len() int {
	if a == nil {
		return 0
	}

	return len(a.addrs)
}

// Seq enumerates the set segment-wise, one [Member] at a time. The order is
// stable across builds of the same schema, so a driver may key a table by
// position.
//
//	for m := range addrs.Seq() {
//	    switch a := m.(type) {
//	    case ferry.LeafAddr:      ...
//	    case ferry.SectionAddr:   ...
//	    case ferry.CompositeAddr: ...
//	    }
//	}
func (a *AddressSet) Seq() iter.Seq[Member] {
	if a == nil {
		return func(func(Member) bool) {}
	}

	return slices.Values(a.addrs)
}

// Has reports whether the set holds this address, at this kind.
//
// The kind is part of the question. A set holding /db as a section answers
// false for /db as a composite, because the two are different addresses that
// admit different questions.
func (a *AddressSet) Has(m Member) bool {
	if a == nil {
		return false
	}

	_, ok := slices.BinarySearchFunc(a.addrs, m, compareMembers)

	return ok
}
