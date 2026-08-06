package ferry

// This file is the test-only door through the address sealing.
//
// The three address types carry an unexported field so that nothing outside
// core can mint one, and the schema compiler is the only thing that does
// (ADR-0016). That leaves core's own tests in package ferry_test with no way to
// build the address set NewKeys is checked against, and the sets that matter
// there are exactly the ones no legal schema produces: /DB/HOST beside /DB_HOST
// is two addresses one env key, which is the collision the check exists for.
//
// It is declared in a _test.go file, so it is compiled only under go test and
// go doc never prints it: the published surface still has no constructor, which
// is what the sealing is.

// LeafSet builds an address set of leaves, for a test outside this package.
func LeafSet(addrs ...Path) *AddressSet {
	members := make([]Member, 0, len(addrs))
	for _, addr := range addrs {
		members = append(members, leafOf(addr))
	}

	return newAddressSet(members...)
}

// Leaf is the leaf address at one path, for a test outside this package that
// has to hand one to a Reader or a Writer directly.
func Leaf(addr Path) LeafAddr { return leafOf(addr) }

// Section is the section address at one path.
func Section(addr Path) SectionAddr { return sectionOf(addr) }

// Composite is the composite address at one path.
func Composite(addr Path) CompositeAddr { return compositeOf(addr) }
