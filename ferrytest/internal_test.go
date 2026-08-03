package ferrytest

import (
	"slices"
	"testing"

	"github.com/onhotpath/ferry"
)

// Two claims in this package have no behaviour to observe them through yet, so
// they are asserted from inside.
//
// This is the same exception the pure-value units in core get. The memory
// plane's key function and the proof's three columns have no engine behind
// them: Load, Dump and the suites that would exercise them do not exist, so
// there is no seam to assert through and the alternative is not asserting at
// all. Both move to the entry point when it lands.

// TestStoreKeysAreRenderings is ADR-0003's first obligation, read off the key
// function itself.
//
// The obligation is about the key rather than about a behaviour, and the only
// behaviour it produces - that two spellings of one address are one slot - is
// also produced by keying on ferry.Path directly. What separates them is the
// claim the memory plane exists to make executable: the canonical rendering
// already identifies an address, so a plane with no format of its own needs
// nothing else to key by.
func TestStoreKeysAreRenderings(t *testing.T) {
	s := newMemStore()

	addrs := []ferry.Path{
		ferry.At("db", "host"),
		ferry.At("tags").Elem(0),
		ferry.At("odd/name"),
		ferry.At("Host"),
		ferry.At("host"),
	}

	for _, addr := range addrs {
		s.put(addr, ferry.String("x"))
	}

	want := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		want = append(want, addr.String())
	}

	got := make([]string, 0, len(s.entries))
	for k := range s.entries {
		got = append(got, k)
	}

	slices.Sort(got)
	slices.Sort(want)

	if !slices.Equal(got, want) {
		t.Errorf("store keys = %q, want the canonical renderings %q", got, want)
	}
}

// TestTypeCarriesThreeColumns asserts that [Type] keeps all three of what it is
// given, because a proof that quietly dropped its relation or its cases would
// pass every test that only reads Name and Type - and the suites that read the
// other two are a later ticket.
func TestTypeCarriesThreeColumns(t *testing.T) {
	cases := []Case[int]{At(0, ferry.Number("0")), At(-5, ferry.Number("-5"))}

	p, ok := Type("int", Eq[int], cases...).(typeProof[int])
	if !ok {
		t.Fatal("Type did not build a typeProof")
	}

	if p.name != "int" {
		t.Errorf("name = %q, want %q", p.name, "int")
	}

	if p.eq == nil {
		t.Fatal("the relation was dropped")
	}

	if !p.eq(1, 1) || p.eq(1, 2) {
		t.Error("the relation is not the one Type was given")
	}

	if !slices.Equal(p.cases, cases) {
		t.Errorf("cases = %v, want %v", p.cases, cases)
	}

	// The seal itself, which is what stops anything outside this package from
	// being a Proof. It is called here because a method nothing ever calls is a
	// method nothing ever checks is there.
	p.proof()
}
