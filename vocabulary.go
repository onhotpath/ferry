package ferry

import (
	"fmt"
	"strconv"
	"strings"
)

// ferry's whole option vocabulary. ADR-0001 freezes it on publication, so a
// word not spent stays available and a word spent is permanent, and that
// asymmetry is what decided every word ferry does not have.
const (
	optRequired = "required"
	optOmitzero = "omitzero"
	optDefault  = "default"
)

// vocabulary is what a refusal names when the mistake is near nothing.
const vocabulary = "ferry's options are required, omitzero and default="

// optionWords is the vocabulary as data, for the near-miss search. The order is
// the search order, so a tie is broken by it rather than by a map's iteration.
var optionWords = [...]string{optRequired, optOmitzero, optDefault}

func isOptionWord(w string) bool {
	for _, word := range optionWords {
		if w == word {
			return true
		}
	}

	return false
}

// instead maps a word from the neighbourhood onto the sentence saying what
// ferry has in its place.
//
// This is the inline lesson taken seriously. inline is not in
// encoding/json/v2's option set, so it falls through the default arm and is
// ignored: roughly 29 thousand uses of a silent no-op. The fix for that is not
// only to reject the word, it is to say what to write instead.
var instead = map[string]string{
	"omitempty": "ferry has no omitempty; its omission option is omitzero, which asks about the Go value " +
		"rather than about an address, and there is no empty JSON object on a Consul plane",
	"inline": "ferry has no inline; an embedded field with no tag is already promoted to the parent address",
	"embed":  "ferry has no embed; an embedded field with no tag is already promoted to the parent address",
	"squash": "ferry has no squash; an embedded field with no tag is already promoted to the parent address",
	"prefix": "ferry has no prefix; a nested struct's own name is the prefix, and there is no concatenation " +
		"to get wrong under a structured address",
	"delimiter": "ferry has no delimiter; a composite gets one address per element, so there is nothing to " +
		"delimit, and how a driver joins segments is the driver's option",
	"separator": "ferry has no separator; a composite gets one address per element, so there is nothing to " +
		"separate, and how a driver joins segments is the driver's option",
	"case":   "ferry has no case option; core never folds, because which characters fold is plane knowledge",
	"string": "ferry has no string option; a plane's own kind assertion is respected rather than overridden",
	"format": "ferry has no format; a per-field layout is a representation ferry has no row for",
	"nodump": "ferry has no nodump; a field ferry loads and never writes cannot round-trip",
	"readonly": "ferry has no readonly; a field ferry loads and never writes cannot round-trip, so keeping a " +
		"secret off a plane is two structs rather than one option",
	"codec": "ferry has no codec option; a codec is selected by type, and a per-field override would be a " +
		"second selection authority for one type",
}

// unknownOption refuses a word ferry does not have, and tries twice to say
// something useful about it first.
//
// Edit distance is the layer json/v2's normalisation does not have: the four
// misspellings ADR-0008 names are each one or two edits from a real word, and
// none of them normalises to one, so v2 would ignore all four. Measured over 26
// misspellings and foreign words, 22 got a specific remedy and the four that
// did not are near nothing.
func unknownOption(head, key string) error {
	word := normaliseWord(head)

	msg, ok := instead[word]
	if !ok {
		msg = suggestion(word)
	}

	return fmt.Errorf("unknown option %q: %s%s", head, msg, collisionNote(key))
}

// suggestion is the near-miss remedy, or the whole vocabulary where the word is
// near nothing.
func suggestion(word string) string {
	if near := nearestWord(word); near != "" {
		return fmt.Sprintf("did you mean %q?", near)
	}

	return vocabulary
}

// normaliseWord is encoding/json/v2's own normalisation, which lowercases and
// strips underscores before looking a word up. It widens what the tables below
// catch and admits nothing: OmitZero is still a refusal, it is a refusal that
// knows what was meant.
func normaliseWord(w string) string {
	return strings.ToLower(strings.ReplaceAll(w, "_", ""))
}

// nearestWord is the option word within maxEdits edits of w, or "".
func nearestWord(w string) string {
	best, bestDist := "", maxEdits+1

	for _, word := range optionWords {
		if d := editDistance(w, word); d < bestDist {
			best, bestDist = word, d
		}
	}

	return best
}

// maxEdits is how far a typo may be from a real word before ferry stops
// guessing. Two covers a transposition, which is what two of ADR-0008's four
// misspellings are, and it reaches nothing in the neighbourhood table:
// omitempty is five edits from omitzero.
const maxEdits = 2

// editDistance is Levenshtein over bytes, which is what a struct tag is.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}

	curr := make([]int, len(b)+1)

	for i := range len(a) {
		editRow(curr, prev, a[i], b)
		prev, curr = curr, prev
	}

	return prev[len(b)]
}

// editRow fills one row of the distance matrix from the one above it.
func editRow(curr, prev []int, ac byte, b string) {
	curr[0] = prev[0] + 1

	for j := range len(b) {
		cost := 1
		if ac == b[j] {
			cost = 0
		}

		curr[j+1] = min(prev[j+1]+1, curr[j]+1, prev[j]+cost)
	}
}

// foreignKeys are the struct tag keys another mapper is known to own.
//
// The parenthetical below fires only for one of these, which is the difference
// between a refusal a user can act on and one that reads as a bug. Pointing the
// tag key at json is not itself refused - ferry reads its own grammar under
// whatever key it is told to read - but three fields in four of a json-tagged
// struct are, because omitempty and string are not ferry's words.
var foreignKeys = map[string]bool{
	"json": true, "yaml": true, "toml": true, "xml": true, "mapstructure": true,
	"env": true, "envconfig": true, "protobuf": true, "bson": true, "db": true,
	"gorm": true, "form": true, "query": true, "validate": true, "koanf": true,
}

func collisionNote(key string) string {
	if !foreignKeys[key] {
		return ""
	}

	return " (the ferry tag key is set to " + strconv.Quote(key) + ", which " + key +
		" also uses; ferry validates its own grammar under whatever key it is told to read)"
}
