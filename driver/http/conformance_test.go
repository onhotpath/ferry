package ferryhttp

import (
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestQueryDriver is ADR-0014's conformance suite in one call, which is what a
// driver author writes and the whole of what this package has to pass.
//
// Case 10 is this driver's own: it is the first in the repository whose plane
// instance is obtained freshly per load, so it is the first for which the case
// runs rather than skips. TestCase10Runs is the assertion that it does.
func TestQueryDriver(t *testing.T) {
	t.Parallel()

	ferrytest.Driver(t, queryPlaneFor())
}

// TestQueryDriverAtWiderSeparator runs the same twelve cases at the separator an
// operator reaches for when the default collides.
//
// It is a second run rather than a second plane because the separator changes
// which schemas the plane accepts and nothing else about it, and a suite that
// passed at one join and not the other would be a driver whose option is not
// really an option.
func TestQueryDriverAtWiderSeparator(t *testing.T) {
	t.Parallel()

	ferrytest.Driver(t, queryPlaneFor(Separator("..")))
}

// TestHeaderDriver is the same suite over the second plane this package ships.
//
// Two runs rather than one plane with a switch, because the two planes differ in
// what they can spell: the header one folds case, holds its names to a token
// grammar, and cannot carry a value with a control character in it. A run that
// passed for one and not the other would be one of the two claiming a property
// only the other has.
func TestHeaderDriver(t *testing.T) {
	t.Parallel()

	ferrytest.Driver(t, headerPlaneFor())
}

// TestHeaderDriverAtWiderSeparator is the header plane at the wider join, which
// is where a field name that itself contains a hyphen stops colliding with the
// nesting.
func TestHeaderDriverAtWiderSeparator(t *testing.T) {
	t.Parallel()

	ferrytest.Driver(t, headerPlaneFor(Separator("--")))
}

// TestRoundTripDynamic is the proof this plane's second dimension exists to make
// possible, and it does not run inside the suite above: ferrytest.Driver guards
// five of its cases on a nil sink, so for a source-only plane the round-trip
// half of every proof it runs never reaches a dynamic address at all. Measured
// on proto/210-http, no single suite catches everything either - Driver catches
// a Get that hands back the first of a repeated name, RoundTrip catches a
// Children that skips a one-element sequence, and neither catches the other.
//
// The rows are what a plane whose sequence positions live in the repetition of a
// name owes beyond what a single-valued flat plane owes.
//
//   - A one-element sequence, because Children answers n positions for any n
//     including one, and "n > 1" is the shortcut a driver naturally writes.
//   - A sequence of more than one element, which is the repeated name itself.
//   - A sequence holding a zero-length element, because empty is not absence
//     here and a position holding "" is what a length-based shortcut drops.
//   - A map with more than one member, which is the flat cut.
//   - A sequence under a minted name, which is the only place the two
//     dimensions and the two tiers compose, and the shape whose plane keys the
//     static table never held.
//
// The golden column is Absent for all of them, which is not a gap: a composite
// with elements writes nothing at its own address, so Absent is a true report
// that ferry encoded nothing there (ADR-0005).
func TestRoundTripDynamic(t *testing.T) {
	t.Parallel()

	for name, p := range map[string]ferrytest.Plane{"query": queryPlaneFor(), "header": headerPlaneFor()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ferrytest.RoundTrip(t, p, dynamicProofs())
		})
	}
}

func dynamicProofs() []ferrytest.Proof {
	return []ferrytest.Proof{
		ferrytest.Type("[]string", ferrytest.SliceEq(ferrytest.Eq[string]),
			ferrytest.At([]string{"a"}, ferry.Value{}),
			ferrytest.At([]string{"a", "b", "c"}, ferry.Value{}),
			ferrytest.At([]string{"a", "", "c"}, ferry.Value{}),
		),
		ferrytest.Type("map[string]string", ferrytest.MapEq[string](ferrytest.Eq[string]),
			ferrytest.At(map[string]string{"sort": "asc"}, ferry.Value{}),
			ferrytest.At(map[string]string{"sort": "asc", "page": "2", "q": ""}, ferry.Value{}),
		),
		ferrytest.Type("map[string][]string", ferrytest.MapEq[string](ferrytest.SliceEq(ferrytest.Eq[string])),
			ferrytest.At(map[string][]string{"origin": {"a"}}, ferry.Value{}),
			ferrytest.At(map[string][]string{"origin": {"a", "b"}}, ferry.Value{}),
		),
	}
}

// TestRoundTripAtWiderSeparator is the same proof over map keys the default join
// cannot express.
//
// A key holding the separator renders the same name as a nested container does,
// and the enumeration resolves that as the nesting, so the key does not come
// back. At the wider join the two are distinct and it does, which is exactly
// what the option is for: a wider separator moves the boundary, and the
// injectivity check is what makes either side of it safe.
func TestRoundTripAtWiderSeparator(t *testing.T) {
	t.Parallel()

	t.Run("query", func(t *testing.T) {
		t.Parallel()

		ferrytest.RoundTrip(t, queryPlaneFor(Separator("..")), []ferrytest.Proof{
			ferrytest.Type("map[string]string", ferrytest.MapEq[string](ferrytest.Eq[string]),
				ferrytest.At(map[string]string{"db.host": "h", "db.port": "p"}, ferry.Value{}),
			),
		})
	})

	t.Run("header", func(t *testing.T) {
		t.Parallel()

		ferrytest.RoundTrip(t, headerPlaneFor(Separator("--")), []ferrytest.Proof{
			ferrytest.Type("map[string]string", ferrytest.MapEq[string](ferrytest.Eq[string]),
				ferrytest.At(map[string]string{"db-host": "h", "db-port": "p"}, ferry.Value{}),
			),
		})
	})
}
