package ferry

import (
	"testing"
	"time"
)

// The per-address kind a driver reads at Bind, which is what an untyped plane
// needs to apply a spelling only where the schema asks for one (proto: #309).
//
// It asserts through Bind, like kinds_test.go, and reads the set core handed
// the driver.

type kindAtSchema struct {
	Flag    bool              `ferry:"flag"`
	Name    string            `ferry:"name"`
	Port    int               `ferry:"port"`
	Blob    []byte            `ferry:"blob"`
	Wait    time.Duration     `ferry:"wait"`
	Pair    [2]bool           `ferry:"pair"`
	Flags   map[string]bool   `ferry:"flags"`
	Labels  map[string]string `ferry:"labels"`
	Structs []kindAtElem      `ferry:"structs"`
}

type kindAtElem struct {
	On bool `ferry:"on"`
}

func TestKindAtAnswersTheSchemasKindPerLeaf(t *testing.T) {
	t.Parallel()

	set := boundSet[kindAtSchema](t)

	want := map[string]VKind{
		"/flag":   KindBool,
		"/name":   KindString,
		"/port":   KindNumber,
		"/blob":   KindBytes,
		"/wait":   KindString,
		"/pair#0": KindBool,
		"/pair#1": KindBool,
	}

	got := leafKinds(t, set)

	for addr, kind := range want {
		if got[addr] != kind {
			t.Errorf("KindAt(%s) = %v, want %v", addr, got[addr], kind)
		}
	}
}

// leafKinds is what the set answers at every leaf it holds, keyed by address.
func leafKinds(t *testing.T, set *AddressSet) map[string]VKind {
	t.Helper()

	out := map[string]VKind{}

	for m := range set.Seq() {
		leaf, ok := m.(LeafAddr)
		if !ok {
			continue
		}

		kind, held := set.KindAt(leaf)
		if !held {
			t.Fatalf("KindAt(%s): the set holds the leaf and answers nothing", leaf)
		}

		out[leaf.String()] = kind
	}

	return out
}

func TestElemKindAnswersForTheAddressesAValueMints(t *testing.T) {
	t.Parallel()

	set := boundSet[kindAtSchema](t)

	want := map[string]VKind{"/flags": KindBool, "/labels": KindString}
	got := elemKinds(set)

	// A slice of structs mints sections rather than leaves, so it has no
	// element kind and is the negative half of this test.
	if _, held := got["/structs"]; held {
		t.Error("ElemKind(/structs) answered for a composite whose members are not leaves")
	}

	for addr, kind := range want {
		if got[addr] != kind {
			t.Errorf("ElemKind(%s) = %v, want %v", addr, got[addr], kind)
		}
	}
}

// elemKinds is what the set answers at every composite whose members are
// leaves.
func elemKinds(set *AddressSet) map[string]VKind {
	out := map[string]VKind{}

	for m := range set.Seq() {
		composite, ok := m.(CompositeAddr)
		if !ok {
			continue
		}

		if kind, held := set.ElemKind(composite); held {
			out[composite.String()] = kind
		}
	}

	return out
}

// TestTheKindSeamAnswersNothingForAnAddressTheSetDoesNotHold pins the two
// negative answers a driver has to handle: a foreign address, and the zero set.
func TestTheKindSeamAnswersNothingForAnAddressTheSetDoesNotHold(t *testing.T) {
	t.Parallel()

	set := boundSet[kindAtSchema](t)

	other := boundSet[struct {
		Elsewhere bool `ferry:"elsewhere"`
	}](t)

	var stray LeafAddr

	for m := range other.Seq() {
		if leaf, ok := m.(LeafAddr); ok {
			stray = leaf
		}
	}

	if _, held := set.KindAt(stray); held {
		t.Errorf("KindAt(%s) answered for an address of another schema", stray)
	}

	var none *AddressSet

	if _, held := none.KindAt(stray); held {
		t.Error("the nil set answered a kind")
	}

	if _, held := none.ElemKind(CompositeAddr{}); held {
		t.Error("the nil set answered an element kind")
	}
}
