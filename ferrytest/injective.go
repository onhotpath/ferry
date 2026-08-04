package ferrytest

import (
	"context"
	"fmt"
	"slices"

	"github.com/onhotpath/ferry"
)

// Injective reports every pair of the supplied values that ferry writes to one
// map key, which is the obligation [ferry.Reg.AsMapKey] declares and nothing
// core can check.
//
//	for _, s := range ferrytest.Injective(reg,
//	    netip.MustParseAddr("192.0.2.1"),
//	    netip.MustParseAddr("::ffff:192.0.2.1"),
//	    netip.MustParseAddr("fe80::1%eth0"),
//	) {
//	    t.Errorf("as a key: %s", s)
//	}
//
// It returns data rather than asserting, and it takes no [context.Context], for
// the reasons [Complete] and [Driver] give. The result is sorted, so a report is
// one string over repeated runs (ADR-0011).
//
// # It is separate from Codec and is not folded into it
//
// The same argument ADR-0009 makes for [ferry.Reg.AsMapKey] being a keyword
// rather than an inference applies to the check: [Codec] asks what is true of a
// codec, and this asks what is true of the values a registrant cares about.
// Nobody but the registrant knows which values those are, and a codec that is
// injective over every value anybody will ever hold is not a property core can
// state or refuse.
//
// # T is comparable, because injectivity is over Go's ==
//
// A Go map's key identity is ==, so == is what decides how many entries the map
// holds and therefore what "two keys" means. A constraint of `any` would let a
// caller ask about values the question cannot be asked of.
//
// # The text comes from ferry and never from a format function the caller
// supplies
//
// An earlier shape took a func(T) string, and it measured wrong on the type it
// was written for: a registrant's own String() gave two distinct texts where
// ferry wrote one twice, so the check reported no collision on a pair that
// collides. What addresses a plane is the text ferry's own lookup produces for
// the key, which is the registered codec's and not the type's idea of itself, so
// this resolves every value through a real dump of a real map and reads the
// address ferry minted (ADR-0005, amended under #31).
func Injective[T comparable](reg *ferry.Registry, values ...T) []string {
	var (
		out  []string
		seen = make(map[T]bool, len(values))
		by   = make(map[string]T, len(values))
	)

	for _, v := range values {
		if seen[v] {
			continue
		}

		seen[v] = true

		text, err := keyText(reg, v)
		if err != nil {
			out = append(out, fmt.Sprintf("%#v has no key text: %v", v, err))

			continue
		}

		if other, taken := by[text]; taken {
			out = append(out, collides(other, v, text))

			continue
		}

		by[text] = v
	}

	slices.Sort(out)

	return out
}

// collides is the one report this check makes, and it names both values and the
// text they share: which of the two survives is which the walk writes last,
// which is not an answer anybody can act on.
func collides[T comparable](a, b T, text string) string {
	return fmt.Sprintf("%#v and %#v both address %q, so one of the two entries is lost with no error anywhere",
		a, b, text)
}

// keyText is the text ferry writes for one map key, read off the address ferry
// minted for it.
//
// It runs a real dump of a real one-entry map through [Record], because that is
// the only place the key text exists: the codec is resolved by the schema
// compiler and applied by the walk, and a second route that re-derived it would
// be measuring itself. One entry per dump rather than all of them at once, and
// deliberately: two keys rendering alike are refused as the second address is
// minted, so a single dump of the whole set would report the first collision and
// stop, where this reports every one of them.
func keyText[T comparable](reg *ferry.Registry, v T) (string, error) {
	opts := []ferry.Option{}
	if reg != nil {
		opts = append(opts, ferry.WithRegistry(reg))
	}

	got, err := Record(context.Background(), keyed[T]{Map: map[T]string{v: ""}}, opts...)
	if err != nil {
		return "", err
	}

	for addr := range got {
		return lastSegment(addr), nil
	}

	return "", errNoKeyAddress
}

// lastSegment is the key's own text, which is the last step of the address the
// walk minted for it.
func lastSegment(addr ferry.Path) string {
	var text string
	for seg := range addr.Segments() {
		text = seg.Text()
	}

	return text
}

// keyed is the struct one key travels in, for the reason [holder] gives: the
// root of a schema is a struct, and a bare map is refused for naming no address.
type keyed[T comparable] struct {
	Map map[T]string `ferry:"m"`
}

// errNoKeyAddress is the report for a dump that wrote nothing, which would mean
// the walk minted no address for a map that holds one entry.
var errNoKeyAddress = fmt.Errorf("%w: a map holding one entry wrote no address", ferry.ErrValue)
