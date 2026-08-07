package env

import (
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestDriver is ADR-0014's conformance suite in one call, which is what a driver
// author writes and the whole of what this package has to pass.
func TestDriver(t *testing.T) {
	t.Parallel()

	ferrytest.Driver(t, plane())
}

// TestDriverAtWiderSeparator runs the same fifteen cases at the separator an
// operator reaches for when the default collides.
//
// It is a second run rather than a second plane because the separator changes
// which schemas the plane accepts and nothing else about it, and a suite that
// passed at one join and not the other would be a driver whose option is not
// really an option.
func TestDriverAtWiderSeparator(t *testing.T) {
	t.Parallel()

	ferrytest.Driver(t, plane(Separator("__")))
}

// TestRoundTripDynamic is the proof [Canonical] exists to make possible, and it
// does not run inside TestDriver: ferrytest.Driver guards five of its cases on a
// nil sink, so for a source-only plane the round-trip half of every proof it
// runs never reaches a dynamic address at all.
//
// Every case below is a composite whose members come from the value rather than
// from the type, so each one is a dump that mints addresses the static table
// never held and a load that has to recover them through Children. A composite
// with elements writes nothing at its own address (ADR-0005), so each case pins
// its golden at a minted address inside the value instead: what ferry encoded
// there is the only representation such a case has, and it is the half a round
// trip composing a spelling with its own inverse cannot see.
func TestRoundTripDynamic(t *testing.T) {
	t.Parallel()

	ferrytest.RoundTrip(t, plane(), []ferrytest.Proof{
		ferrytest.Type("map[string]string", ferrytest.MapEq[string](ferrytest.Eq[string]),
			ferrytest.Inside(map[string]string{"http": "1"}, ferry.At("http"), ferry.String("1")),
			ferrytest.Inside(map[string]string{"http": "1", "grpc": "2", "port80": "3"},
				ferry.At("port80"), ferry.String("3")),
		),
		ferrytest.Type("[]string", ferrytest.SliceEq(ferrytest.Eq[string]),
			ferrytest.Inside([]string{"a"}, ferry.Path{}.Elem(0), ferry.String("a")),
			ferrytest.Inside([]string{"a", "b", "c"}, ferry.Path{}.Elem(2), ferry.String("c")),
		),
		ferrytest.Type("map[string][]string", ferrytest.MapEq[string](ferrytest.SliceEq(ferrytest.Eq[string])),
			ferrytest.Inside(map[string]([]string){"origins": {"a", "b"}},
				ferry.At("origins").Elem(1), ferry.String("b")),
		),
	})
}

// TestRoundTripDynamicAtWiderSeparator is the same proof over segments the
// default join cannot express.
//
// A map key holding an underscore renders the same name as a nested container
// does at the default separator, and the enumeration resolves that as the
// nesting, so the key does not come back. At "__" the two are distinct and it
// does, which is exactly what the option is for: a wider separator moves the
// boundary, and the injectivity check is what makes either side of it safe.
func TestRoundTripDynamicAtWiderSeparator(t *testing.T) {
	t.Parallel()

	ferrytest.RoundTrip(t, plane(Separator("__")), []ferrytest.Proof{
		ferrytest.Type("map[string]string", ferrytest.MapEq[string](ferrytest.Eq[string]),
			ferrytest.Inside(map[string]string{"db_host": "h", "db_port": "p"},
				ferry.At("db_host"), ferry.String("h")),
		),
	})
}
