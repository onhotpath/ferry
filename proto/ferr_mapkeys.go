package main

// Sorted map keys, rendered once, with the collapse check that ADR-0007's #45
// paragraph asks for.
//
// R3 in #45's costing. ADR-0009 puts injectivity on whoever supplied the codec
// and gives them `.AsMapKey()` to say they thought about it; ADR-0007 refuses a
// chain-claimed key outright because there is nobody to ask. Neither of those
// catches a registrant who says the word and is WRONG, and ADR-0009 says so:
// "shipping only the keyword leaves a registrant who has said the word with no
// way to check that the word was true."
//
// This is that check, and it needs nothing from the registrant. Two Go keys
// rendering to one address is observable exactly, at the moment of loss, from
// the values in hand. It costs nothing, because the walk already had to render
// every key and sort by the rendering: doing it once into a slice and scanning
// adjacent pairs replaces a comparator that re-rendered both sides on every
// comparison.
//
// It is a DUMP-side check and there is no Load-side counterpart. Measured
// (Y45=6, and #31 independently at K31=5): loadDir's members takes its
// addresses from FEnumerator.Children, which is already a set, so one walk
// cannot be handed the same address twice. Nothing was lost on that Load - the
// loss happened on whichever Dump wrote the file.
//
// TWO SESSIONS WROTE THIS CHECK WITHIN HOURS OF EACH OTHER, on #45 and on #31,
// and the function below is neither one verbatim. From #45: the two call sites
// (e_walk AND the superseded walk.go, which #31's single site missed) and the
// colliding address in the error's Path, where Elements() and ADR-0003's sort
// key read it. From #31: building []member in place, which costs one allocation
// fewer per map; counting the entries lost rather than assuming one, which #45
// got measurably wrong on a three-way collision; and the keyCollisionCheck seam,
// so the world as the tip shipped stays measurable.

import (
	"reflect"
	"slices"
)

// sortedMapMembers renders every key once, sorts by the rendering - which is
// ADR-0003's determinism invariant for the dynamic tier - and refuses if two
// keys landed on one address.
//
// Rendering once is both the check and an optimisation: the comparator used to
// re-render both sides on every comparison. Measured at the shipped call site,
// 3x faster at 8 keys and ~7x at 512.
func sortedMapMembers(v reflect.Value, at Path) ([]member, error) {
	keys := v.MapKeys()
	ms := make([]member, len(keys))
	for i, k := range keys {
		ms[i] = member{seg: Segment{Kind: Name, Text: mapKeyText(k)}, key: k}
	}
	slices.SortFunc(ms, func(a, b member) int { return cmpStr(a.seg.Text, b.seg.Text) })
	if !keyCollisionCheck {
		return ms, nil
	}
	lost, first := 0, ""
	for i := 1; i < len(ms); i++ {
		if ms[i].seg.Text == ms[i-1].seg.Text {
			if lost == 0 {
				first = ms[i].seg.Text
			}
			lost++
		}
	}
	if lost > 0 {
		return nil, mapKeyCollapse(at, v.Type(), first, lost)
	}
	return ms, nil
}

// mapKeyCollapse is the diagnostic. It names the address rather than the two Go
// values, because ferry's own message text never carries a value the plane
// supplied and a map key is the user's value on its way to becoming one.
//
// It COUNTS. #45's first version said "one entry would be lost" whatever the
// arity, which is wrong the moment three keys collide, and #31's review caught
// it.
func mapKeyCollapse(at Path, t reflect.Type, text string, lost int) error {
	return errAt(mWalk, ErrValue, at.Name(text),
		"keys of %s render to this one address, so %d entr%s would be lost; "+
			"a key codec's text must be injective over the key type", t, lost, plural(lost))
}
