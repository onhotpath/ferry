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
// Every address ferry hands you was minted by the schema compiler and is one of
// those three, so the type switch above covers every address you will be given.
// Core refuses anything else: a value of your own satisfying this interface is
// in no address set, and asking a set whether it holds one answers false.
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

func leafOf(p Path) LeafAddr           { return LeafAddr{p: p} }
func sectionOf(p Path) SectionAddr     { return SectionAddr{p: p} }
func compositeOf(p Path) CompositeAddr { return CompositeAddr{p: p} }

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
		return leafOf(p)
	case kindSection:
		return sectionOf(p)
	default:
		return compositeOf(p)
	}
}

// rank orders the kinds inside an address set, so that a set holding two kinds
// at one path is still enumerated in one stable order.
//
// The unknown arm is explicit and is not decoration. Go seals a type and not an
// interface: embedding a [SectionAddr] in a struct outside core promotes the
// unexported method, so a value satisfying [Member] that core never minted can
// be written. Falling through to the composite arm would have made such a value
// compare equal to a real composite at the same path, so [AddressSet.Has] would
// answer true for an address the schema does not hold and the diagnosis that
// followed would name the wrong kind. Ranking it after all three makes it equal
// to nothing, which is the true answer: no address core minted is one of these.
func rank(m Member) int {
	switch m.(type) {
	case LeafAddr:
		return int(kindLeaf)
	case SectionAddr:
		return int(kindSection)
	case CompositeAddr:
		return int(kindComposite)
	default:
		return kindForeign
	}
}

// kindForeign is where a [Member] core did not mint sorts: after every kind
// there is, so that it is equal to none of them (ADR-0016).
const kindForeign = int(kindComposite) + 1

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

	// ext is what the registry's declared foreign tag keys carried, and it rides
	// here rather than being plumbed because this is the handoff a driver
	// already receives (ADR-0021).
	ext ExtTable

	// kinds is what the schema wants at every static leaf, and elems is what it
	// wants at the leaves a composite's value mints. Both are filled by the
	// compiler alone, which is what keeps the seal: a driver reads the kind of
	// an address it was handed and cannot state one (proto: #309).
	kinds map[LeafAddr]VKind
	elems map[CompositeAddr]VKind
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

// firstComposite is the earliest composite this set holds, and whether it holds
// one at all.
//
// It is the question "can a dump of this schema need a retraction", answered off
// the set the compiler already built (ADR-0004). One address is enough because
// the answer is a yes or a no: a plane either can forget an address or cannot,
// and the address is carried so that the refusal names a field rather than a
// capability.
//
// The set is sorted segment-wise, so which composite that is does not vary
// between builds of the same schema.
func (a *AddressSet) firstComposite() (CompositeAddr, bool) {
	for m := range a.Seq() {
		if c, ok := m.(CompositeAddr); ok {
			return c, true
		}
	}

	return CompositeAddr{}, false
}

// Extension is one declared tag key's address-keyed view: for each address in
// this set whose field carried that key, the words it carried and the text of
// each.
//
//	func (s Sink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
//	    nodeTags := map[ferry.Path]string{}
//	    for addr, words := range addrs.Extension("yamlext") {
//	        nodeTags[addr] = words["node"]
//	    }
//	    ...
//	}
//
// It is how a driver reads its own key without a caller plumbing anything: the
// registry carries the declaration, this set carries the table, and the sink is
// still constructed the way it always was. Reading it once at Bind is the whole
// idiom, since the answer is a property of the schema and not of a call.
//
// A driver sees extension data only for addresses it was bound to. A key
// nothing declared, and a key no field carried, both yield an empty view rather
// than an error, and the result is freshly allocated and the caller's to keep.
//
// What is in it is inert to ferry: core validated the words against their
// declaration and acts on none of them. Acting is yours, and so is the proof
// that what you write can be read back.
func (a *AddressSet) Extension(key string) map[Path]map[string]string {
	if a == nil {
		return map[Path]map[string]string{}
	}

	return a.ext.Extension(key)
}

// KindAt is the [VKind] the schema wants at one leaf, and whether this set
// holds that leaf at all.
//
// It is what lets a plane carrying no type information of its own apply a
// spelling only where the schema asks for one: a driver reads it once, at Bind,
// into a table beside its plane keys.
//
//	for m := range addrs.Seq() {
//	    if leaf, ok := m.(ferry.LeafAddr); ok {
//	        if k, ok := addrs.KindAt(leaf); ok && k == ferry.KindBool {
//	            d.spell[leaf] = true
//	        }
//	    }
//	}
//
// The kind is what the field's own codec declares, so it is the plane
// vocabulary of [VKind] and never a Go type. A leaf accepts a [KindString]
// beside its own kind whatever this answers, so the kind is what the schema
// wants rather than the whole of what it will take.
//
// The sharp edge is that an address a value mints is in no address set, so this
// answers false for one: ask [AddressSet.ElemKind] about the composite that
// will mint it instead.
func (a *AddressSet) KindAt(addr LeafAddr) (VKind, bool) {
	if a == nil {
		return KindAbsent, false
	}

	k, ok := a.kinds[addr]

	return k, ok
}

// ElemKind is the [VKind] the schema wants at the leaves a composite's value
// mints, and whether those members are leaves at all.
//
// A slice of structs mints sections rather than leaves and answers false, and
// so does a composite this set does not hold. Every member of one composite has
// one type, so one answer covers all of them however many the value mints.
func (a *AddressSet) ElemKind(addr CompositeAddr) (VKind, bool) {
	if a == nil {
		return KindAbsent, false
	}

	k, ok := a.elems[addr]

	return k, ok
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
