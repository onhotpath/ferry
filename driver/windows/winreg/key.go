package winreg

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/onhotpath/ferry"
)

// ErrIllegalName reports an address the registry has no name for.
//
// Two shapes, and no fold rescues either. An empty part has no name at all: two
// backslashes with nothing between them are one backslash, so the address would
// be written where another address already is. A part holding a backslash is
// worse, because it succeeds: the registry reads it as another step down its own
// hierarchy, so a map key "a\b" under /m would be written as the value b under
// the subkey m\a and read back as a different, empty member.
//
// A tagged field is refused at Bind and a map key that mints one is refused as it
// is minted, in either case before the read or the write it belongs to.
//
// It wraps [ferry.ErrPlane], and it stays reachable under ferry's wrapper, so
// errors.Is answers for it on what [ferry.Load] and [ferry.Dump] returned.
var ErrIllegalName = errors.New("winreg: this cannot be named in the registry")

// driverName is what this driver calls itself in a refusal core prints, so a
// schema that is fine on one plane and impossible on this one reports as this
// plane's problem rather than as ferry's.
const driverName = "winreg"

// separator is how the registry spells the step from one key to the next, and it
// is not configurable.
//
// It is the store's own syntax rather than a taste: a registry path is a
// backslash-separated hierarchy, and a driver joining with something else would
// be writing names whose structure RegOpenKeyEx cannot see (ADR-0003).
const separator = `\`

// segmentsHint is how many segments an ordinary address is guessed to hold, so
// that splitting one needs a single allocation.
const segmentsHint = 4

// key renders one address as this plane's key: every segment folded to lower
// case and joined with a backslash, the value's own name included, and the empty
// string for the root address.
//
// It is the [ferry.KeyFunc] this driver hands to [ferry.NewKeys], which runs
// ADR-0003's two obligations over it - legality per address, injectivity over the
// set - once per schema and before any registry call.
//
// # The fold is the whole reason this driver builds a key at all
//
// The registry is case-insensitive and case-preserving: setting Host and then
// host leaves one value, named Host, holding the second write's data, with no
// error anywhere. Folding here is what turns that silent loss into a refusal at
// Bind naming both addresses (ADR-0003's fourth column). The caller's own
// spelling is not destroyed by it, because the write path takes its text from
// [placeOf] rather than from this key.
//
// The fold is Go's own lower-casing and the registry's is Windows' upcase table,
// and the two are not identical outside ASCII. Where they differ, this one folds
// less, so the residue is a pair the registry merges and this driver accepted -
// which is the sharp edge the package documentation states rather than a case
// the check covers.
//
// # Why the value name is part of the key
//
// The registry keeps values and subkeys in two namespaces, so /db/host is the
// value host under the subkey db and /db is the subkey db. Both are named here,
// which is what lets [ferry.NewKeys] refuse two addresses of one kind that fold
// together while accepting a leaf and a container that merely share a name - they
// are two real objects. The split back into a subkey and a value name is
// unambiguous only because a backslash is refused in every segment, terminal
// included: with one allowed there, /db\host and /db/host would be two registry
// objects producing one key.
func key(addr ferry.Path) (string, error) {
	var b strings.Builder

	b.Grow(len(addr.String()))

	first := true

	for seg := range addr.Segments() {
		if err := nameable(seg.Text()); err != nil {
			return "", err
		}

		if !first {
			b.WriteString(separator)
		}

		first = false

		b.WriteString(fold(seg.Text()))
	}

	return b.String(), nil
}

// nameable reports whether the registry can name a part holding this text.
//
// It names no segment text. ADR-0011 makes "ferry's own message text never
// contains a value the plane supplied" a total rule, and a dynamic segment is the
// caller's value; core attaches the address itself, which is structure and is what
// a reader needs in order to act.
func nameable(text string) error {
	switch {
	case text == "":
		return illegalName("a part of it is empty, and the registry has no name for one: two backslashes " +
			"with nothing between them are one backslash, so it would be written at another address")
	case strings.Contains(text, separator):
		return illegalName("a part of it contains a backslash, and the registry reads that as another step " +
			"down its own hierarchy: it would be written under a key of that name and read back as a " +
			"different, empty object")
	default:
		return nil
	}
}

// illegalName states the class this driver has an opinion about and keeps
// [ErrIllegalName] reachable underneath it.
func illegalName(msg string) error {
	return fmt.Errorf("%w: %w: %s", ferry.ErrPlane, ErrIllegalName, msg)
}

// fold is the case fold the key function applies, and the one the write path
// deliberately does not.
func fold(text string) string { return strings.ToLower(text) }

// place is the registry object one leaf address names: the subkey path it lives
// under, and the value name inside it.
//
// Both are in the caller's own spelling rather than the folded one, because the
// registry preserves the case of whoever wrote a name first and there is no
// reason for this driver to be what destroys it. The folded form is the key
// function's, and it exists for the injectivity check.
type place struct{ subkey, name string }

// placeOf splits one leaf address into the subkey it lives under and the value
// name inside it.
//
// The root address is the empty subkey and the empty value name, which is the
// driver's own key and its unnamed (Default) value - a real, legal object, so
// this driver needs no option naming the root the way a plane with no such object
// does.
func placeOf(addr ferry.Path) place {
	parts := texts(addr)
	if len(parts) == 0 {
		return place{}
	}

	last := len(parts) - 1

	return place{subkey: strings.Join(parts[:last], separator), name: parts[last]}
}

// subkeyOf is the subkey one container address names, in the caller's own
// spelling. A container is a subkey outright, so nothing is split off it.
func subkeyOf(addr ferry.Path) string { return strings.Join(texts(addr), separator) }

// texts is one address's segment texts, in order.
func texts(addr ferry.Path) []string {
	out := make([]string, 0, segmentsHint)
	for seg := range addr.Segments() {
		out = append(out, seg.Text())
	}

	return out
}

// joinKey is one step down a registry path: prefix, a backslash and name.
//
// Either half may be empty and neither produces a bare backslash. An empty
// prefix is the top of whatever path is being built - a driver over a hive
// itself has no subkey to place its addresses under - and an empty name is no
// step at all, which is what the root address is: the driver's own key, and its
// unnamed value inside it.
func joinKey(prefix, name string) string {
	switch {
	case prefix == "":
		return name
	case name == "":
		return prefix
	default:
		return prefix + separator + name
	}
}

// segmentOf builds one member out of the name the registry spelled, reading the
// segment kind off the text because the registry carries none.
//
// It is driver/kv's recovery copied rather than shared, because ADR-0002 forbids
// the internal module that would carry it.
func segmentOf(name string) ferry.Segment {
	if i, ok := position(name); ok {
		return ferry.IndexSegment(i)
	}

	return ferry.NameSegment(name)
}

// position is the sequence index a member name spells, if it spells one.
//
// It accepts exactly what [ferry.Path] renders an Index segment as: canonical
// base-10 with no leading zero. "01" and "" are member names and not positions,
// which keeps this the inverse of the key function rather than a looser parse
// that would read one address as another. A number too large for the type is a
// name as well, because answering with a wrapped-around index would be the one
// thing worse than refusing it.
//
// Copied from driver/kv rather than shared, for the reason [segmentOf] gives.
func position(name string) (uint, bool) {
	if !canonicalDigits(name) {
		return 0, false
	}

	var n uint

	for i := range len(name) {
		d := uint(name[i] - '0')
		if n > (maxUint-d)/base10 {
			return 0, false
		}

		n = n*base10 + d
	}

	return n, true
}

// canonicalDigits reports whether text is base-10 with no leading zero, which is
// the only spelling ferry renders a position in and therefore the only one that
// may be read back as one.
func canonicalDigits(text string) bool {
	if text == "" || (text[0] == '0' && text != "0") {
		return false
	}

	for i := range len(text) {
		if text[i] < '0' || text[i] > '9' {
			return false
		}
	}

	return true
}

const (
	// base10 is the only base a position is ever spelled in, which is what makes
	// the rendering of an address unique.
	base10 = 10
	// maxUint is the largest position [ferry.Path.Elem] can take.
	maxUint = ^uint(0)
)

// compareSegments orders two members the way core orders the addresses they
// name: by kind first, and a position numerically rather than as text.
//
// Copied from driver/kv rather than shared, for the reason [segmentOf] gives.
func compareSegments(a, b ferry.Segment) int {
	if a.Kind() != b.Kind() {
		return int(a.Kind()) - int(b.Kind())
	}

	if a.Kind() == ferry.Index && len(a.Text()) != len(b.Text()) {
		return len(a.Text()) - len(b.Text())
	}

	return strings.Compare(a.Text(), b.Text())
}

// members is the immediate members of one container, as segments, sorted the way
// core orders the addresses they name.
//
// A value and a subkey of one name under a container are one member, because one
// member is one address: [reader.Get] is what settles which of the two holds its
// value, and it refuses a member that is only a subkey rather than reading the
// Go zero over it.
//
// The key's own unnamed value is not a member. It is the container's own value
// slot, which ferry has no address for, and minting a segment with no text out of
// it would be refused by the key function anyway.
func members(l Listing) []ferry.Segment {
	pick := make(map[string]string, len(l.Values)+len(l.Keys))

	claim(pick, l.Values)
	claim(pick, l.Keys)

	out := make([]ferry.Segment, 0, len(pick))
	for _, name := range pick {
		out = append(out, segmentOf(name))
	}

	slices.SortFunc(out, compareSegments)

	return out
}

// claim files each name under its folded spelling, keeping the smallest spelling
// among the names that fold together, so the answer does not depend on the order
// the registry enumerated in.
func claim(pick map[string]string, names []string) {
	for _, name := range names {
		if name == "" {
			continue
		}

		if held, taken := pick[fold(name)]; taken && held <= name {
			continue
		}

		pick[fold(name)] = name
	}
}
