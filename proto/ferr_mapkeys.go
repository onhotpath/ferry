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
	"fmt"
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
	// The count has to be true of the address the message NAMES, so it is the
	// length of the run at that address and not the map-wide total. addrs is
	// the map-wide statement, and it is a count of ADDRESSES rather than of
	// entries, so the two numbers cannot be read as one.
	first, lost, addrs := "", 0, 0
	for i, run := 1, 1; i <= len(ms); i++ {
		if i < len(ms) && ms[i].seg.Text == ms[i-1].seg.Text {
			run++
			continue
		}
		if run > 1 {
			addrs++
			if first == "" {
				first, lost = ms[i-1].seg.Text, run-1
			}
		}
		run = 1
	}
	if addrs > 0 {
		return nil, mapKeyCollapse(at, v.Type(), first, lost, addrs)
	}
	return ms, nil
}

// mapKeyCollapse is the diagnostic. It names the address rather than the two Go
// values, because ferry's own message text never carries a value the plane
// supplied and a map key is the user's value on its way to becoming one.
//
// It COUNTS, and the count is TRUE OF THE ADDRESS IT NAMES. Three versions of
// this message have now been wrong about arity, each caught by the next pair of
// eyes rather than by a fixture:
//
//	#45's first said "one entry would be lost" whatever the arity;
//	the merged version counted correctly but attributed the MAP-WIDE total to
//	the one address it named, so four keys collapsing into two addresses, one
//	entry lost at each, reported "so 2 entries would be lost" at /m/x;
//	this one reports the run length at that address, and states the map-wide
//	fact as a count of ADDRESSES so the two numbers cannot be read as one.
//
// No probe caught any of the three, because no probe builds a multi-run
// collision. That is a conformance case for #35, not a comment.
func mapKeyCollapse(at Path, t reflect.Type, text string, lost, addrs int) error {
	more := ""
	if addrs > 1 {
		more = fmt.Sprintf(" (%d addresses of this map collapse)", addrs)
	}
	return errAt(mWalk, ErrValue, at.Name(text),
		"keys of %s render to this one address, so %d entr%s would be lost%s; "+
			"a key codec's text must be injective over the key type",
		t, lost, plural(lost), more)
}
