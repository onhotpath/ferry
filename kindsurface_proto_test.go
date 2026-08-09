package ferry

import (
	"testing"
	"unsafe"
)

// What the four spellings of the #309 seam cost, measured rather than argued.
//
// A is two methods on the address set, B is one, C puts the answer on the
// address itself, and D is the spelling that adds no exported name at all. This
// file measures the two properties the surface argument turns on: what the
// address weighs under C, and what a driver can ask with the names that already
// exist.

// TestTheAddressWeighsMoreWhenTheKindRidesOnIt is candidate C's price, and it is
// the size ADR-0016 measured for the alternative it rejected.
func TestTheAddressWeighsMoreWhenTheKindRidesOnIt(t *testing.T) {
	t.Parallel()

	section := unsafe.Sizeof(SectionAddr{})
	leaf := unsafe.Sizeof(LeafAddr{})
	composite := unsafe.Sizeof(CompositeAddr{})

	t.Logf("Path %d B, SectionAddr %d B, LeafAddr %d B, CompositeAddr %d B",
		unsafe.Sizeof(Path{}), section, leaf, composite)

	if leaf == section {
		t.Error("the kind rides on the address for free, which the padding rules say it does not")
	}

	if leaf != composite {
		t.Errorf("LeafAddr is %d B and CompositeAddr is %d B, and the two carry the same fields", leaf, composite)
	}
}

// TestAnAddressBuiltWithoutTheSchemasAnswerIsEqualToNothing is candidate C's
// hazard, and it is the reason the seal matters more under C than under A or B.
//
// Membership is decided by path and address kind, so the set still answers for
// an address built without the schema's answer. Equality is decided by every
// field, so a table keyed by the addresses the set handed out misses that same
// address. A driver keying its plane names by LeafAddr - which is the idiom
// ADR-0016 documents - is exactly the code that would trip over it.
func TestAnAddressBuiltWithoutTheSchemasAnswerIsEqualToNothing(t *testing.T) {
	t.Parallel()

	set := boundSet[kindAtSchema](t)

	names := map[LeafAddr]string{}

	for m := range set.Seq() {
		if leaf, ok := m.(LeafAddr); ok {
			names[leaf] = leaf.String()
		}
	}

	flag := leafOf(pathOf(t, "/flag"))

	if !set.Has(flag) {
		t.Fatal("the set does not hold /flag as a leaf, so this test measures nothing")
	}

	if _, found := names[flag]; found {
		t.Error("an address built with no kind is a key in a table built from the set, so candidate C's " +
			"equality and candidate C's membership agree after all")
	}

	if got := flag.Kind(); got != KindAbsent {
		t.Errorf("Kind() = %v on an address nothing typed, want absent", got)
	}
}

// pathOf is the address the compiler would have minted for one static leaf.
func pathOf(t *testing.T, rendered string) Path {
	t.Helper()

	set := boundSet[kindAtSchema](t)

	for m := range set.Seq() {
		if m.String() == rendered {
			return m.Path()
		}
	}

	t.Fatalf("no address renders as %s", rendered)

	return Path{}
}

// TestOneMethodAnswersBothQuestions is candidate B: one question over the sealed
// sum, and the three answers it has to keep apart.
func TestOneMethodAnswersBothQuestions(t *testing.T) {
	t.Parallel()

	set := boundSet[kindAtSchema](t)

	want := map[string]VKind{
		"/flag":   KindBool, // the kind at this address
		"/name":   KindString,
		"/flags":  KindBool, // the kind at addresses this one has not minted yet
		"/labels": KindString,
	}

	for m := range set.Seq() {
		checkOneAnswer(t, set, m, want)
	}
}

// checkOneAnswer is what candidate B has to keep apart at one member: the value
// at a leaf, the value at what a composite will mint, and nothing at a section.
func checkOneAnswer(t *testing.T, set *AddressSet, m Member, want map[string]VKind) {
	t.Helper()

	kind, held := set.Kind(m)

	if _, section := m.(SectionAddr); section && held {
		t.Errorf("Kind(%s) answered %v for a section, which holds no value", m, kind)
	}

	if expected, listed := want[m.String()]; listed && (!held || kind != expected) {
		t.Errorf("Kind(%s) = %v, %v, want %v, true", m, kind, held, expected)
	}
}

// TestNoExistingNameCanCarryTheAnswer is candidate D, which is the spelling that
// adds nothing, asked as something a test can execute.
//
// Every existing question on the set is asked with an address, and the only
// addresses outside core are the ones core handed out: there is no exported
// constructor, which is the seal. So a driver can ask "is this address in the
// set" and never "does the schema want a bool here", because the second question
// needs a kind in the question and nothing outside core can put one there.
//
// The assertion is the seal itself. It is false today and becomes true on the
// day an address gains an exported constructor, which is the day candidate D
// becomes writable and the day the sealed model stops holding.
func TestNoExistingNameCanCarryTheAnswer(t *testing.T) {
	t.Parallel()

	var forged any = LeafAddr{}

	if _, ok := forged.(interface{ WithKind(VKind) LeafAddr }); ok {
		t.Error("an address can be restated at another kind from outside core, so the sealing is gone " +
			"and with it the reason candidate D cannot be written")
	}

	set := boundSet[kindAtSchema](t)

	// What the existing names answer: membership, count, order and extension
	// data. None of them is about what the schema wants at an address.
	if set.Len() == 0 || len(set.Extension("nothing")) != 0 {
		t.Error("the set's existing surface does not answer what this test assumed")
	}
}
