package ferrytest

import (
	"context"
	"fmt"
	"slices"

	"github.com/onhotpath/ferry"
)

// Injective reports every pair of the supplied values that ferry writes to one
// map key, which is the obligation [ferry.KeyCodec.AsMapKey] declares and nothing
// ferry can check for you.
//
// Two values that become one key are one entry in the loaded map, so one of them
// is lost. Nobody but you knows which values your program will hold, so pass the
// ones that are close together: the same address spelled two ways, the same
// identifier in two cases, a value carrying a zone or a scope.
//
//	for _, s := range ferrytest.Injective(reg,
//	    netip.MustParseAddr("192.0.2.1"),
//	    netip.MustParseAddr("::ffff:192.0.2.1"),
//	    netip.MustParseAddr("fe80::1%eth0"),
//	) {
//	    t.Errorf("as a key: %s", s)
//	}
//
// It returns data rather than failing anything, and it takes no
// [context.Context], for [Driver]'s reason. The result is sorted, so the report
// is the same string over repeated runs.
//
// The key text comes from ferry and never from the type's own String method.
// What addresses a plane is what your registered key codec produces, and a type
// whose String differs from it would answer about the wrong text, so every value
// here is resolved through a real dump of a real map.
//
// T is comparable because a Go map's key identity is ==, which is what decides
// how many entries the map holds and therefore what "two keys" means.
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
