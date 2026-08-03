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
// (Y45=6): on Load the plane holds one address and nothing was lost on that
// Load - the loss happened on whichever Dump wrote the file.

import (
	"reflect"
	"slices"
)

// keyedMember is one map entry, with its address segment already rendered.
type keyedMember struct {
	text string
	key  reflect.Value
}

// sortedMapKeys renders every key once, sorts by the rendering - which is
// ADR-0003's determinism invariant for the dynamic tier - and reports the first
// pair that collapses.
//
// The returned index is into the sorted slice, or -1.
func sortedMapKeys(v reflect.Value) ([]keyedMember, int) {
	keys := v.MapKeys()
	ms := make([]keyedMember, len(keys))
	for i, k := range keys {
		ms[i] = keyedMember{text: mapKeyText(k), key: k}
	}
	slices.SortFunc(ms, func(a, b keyedMember) int { return cmpStr(a.text, b.text) })
	for i := 1; i < len(ms); i++ {
		if ms[i].text == ms[i-1].text {
			return ms, i
		}
	}
	return ms, -1
}

// mapKeyCollapse is the diagnostic. It names the address rather than the two Go
// values, because ferry's own message text never carries a value the plane
// supplied and a map key is the user's value on its way to becoming one -
// printing it would leak exactly what ADR-0011's redaction rule protects.
//
// The remedy it names is the registrant's, because under ADR-0007's reversal
// the only way to reach this at all is a registration carrying `.AsMapKey()`,
// or one of core's own pre-seeded entries. The latter is #31 and this message
// does not pretend to fix it.
func mapKeyCollapse(at Path, t reflect.Type, text string) error {
	return errAt(mWalk, ErrValue, at.Name(text),
		"two keys of %s render to this one address, so one entry would be lost; "+
			"a key codec's text must be injective over the key type", t)
}
