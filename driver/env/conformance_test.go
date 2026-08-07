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
// never held and a load that has to recover them through Children. The golden
// column is Absent for all of them, which is not a gap: a composite with
// elements writes nothing at its own address, so Absent is a true report that
// ferry encoded nothing there (ADR-0005).
func TestRoundTripDynamic(t *testing.T) {
	t.Parallel()

	ferrytest.RoundTrip(t, plane(), []ferrytest.Proof{
		ferrytest.Type("map[string]string", ferrytest.MapEq[string](ferrytest.Eq[string]),
			ferrytest.At(map[string]string{"http": "1"}, ferry.Value{}),
			ferrytest.At(map[string]string{"http": "1", "grpc": "2", "port80": "3"}, ferry.Value{}),
		),
		ferrytest.Type("[]string", ferrytest.SliceEq(ferrytest.Eq[string]),
			ferrytest.At([]string{"a"}, ferry.Value{}),
			ferrytest.At([]string{"a", "b", "c"}, ferry.Value{}),
		),
		ferrytest.Type("map[string][]string", ferrytest.MapEq[string](ferrytest.SliceEq(ferrytest.Eq[string])),
			ferrytest.At(map[string]([]string){"origins": {"a", "b"}}, ferry.Value{}),
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
			ferrytest.At(map[string]string{"db_host": "h", "db_port": "p"}, ferry.Value{}),
		),
	})
}
