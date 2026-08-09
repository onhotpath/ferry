package env

import (
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestDriver is ADR-0014's conformance suite in one call, which is what a driver
// author writes and the whole of what this package has to pass.
//
// It runs over the files alone, so the plane under it is exactly the format this
// package defines and nothing ambient reaches it.
func TestDriver(t *testing.T) {
	t.Parallel()

	ferrytest.Driver(t, filePlane(t))
}

// TestDriverAtWiderSeparator runs the same cases at the separator an operator
// reaches for when the default collides.
//
// It is a second run rather than a second plane because the separator changes
// which schemas the plane accepts and nothing else about it, and a suite that
// passed at one join and not the other would be a driver whose option is not
// really an option.
func TestDriverAtWiderSeparator(t *testing.T) {
	t.Parallel()

	ferrytest.Driver(t, filePlane(t, Separator("__")))
}

// TestDriverOverTheComposite is the whole plane under the suite: one file under
// one process environment, with a dump writing both halves.
//
// It is the run that could not exist before this package had a sink. What it
// proves is the thing the composite is for: a save that writes the file and
// leaves the process exporting the old value would pass every case above and
// fail here, because here the environment a load reads is the one the dump was
// told to keep in agreement with the file.
func TestDriverOverTheComposite(t *testing.T) {
	t.Parallel()

	ferrytest.Driver(t, compositePlane(t))
}

// goldenRows pins this driver's own spelling of a value, byte for byte.
//
// They are what catches a writer and a parser that are wrong in the same
// direction, which no round trip can see. Each row is one of the quoting
// decisions: bare for the ordinary value, double quotes with escapes for text
// that holds a newline or a quote, single quotes for text a shell would
// otherwise interpolate, double quotes holding raw bytes for a payload that is
// not text at all, and one row for the root leaf [RootVar] names.
//
// A change to one of these is a change to what every .env file this driver has
// ever written means.
func goldenRows() []ferrytest.Artefact {
	return []ferrytest.Artefact{
		ferrytest.Golden(struct {
			Host string `ferry:"host"`
			Port int    `ferry:"port"`
			On   bool   `ferry:"on"`
		}{"db.internal", 5432, true}, "HOST=db.internal\nPORT=5432\nON=true\n"),

		ferrytest.Golden(struct {
			Text string `ferry:"text"`
		}{"a\nb\"c"}, "TEXT=\"a\\nb\\\"c\"\n"),

		ferrytest.Golden(struct {
			Shell string `ferry:"shell"`
			Blank string `ferry:"blank"`
			Pad   string `ferry:"pad"`
		}{"$HOME", "", " padded "}, "SHELL='$HOME'\nBLANK=''\nPAD=' padded '\n"),

		ferrytest.Golden(struct {
			Payload []byte `ferry:"payload"`
		}{[]byte{0xff, 0xfe}}, "PAYLOAD=\"\xff\xfe\"\n"),

		ferrytest.Golden(8080, "ROOT=8080\n"),
	}
}

// TestRoundTripDynamic is the proof [Canonical] exists to make possible.
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

	ferrytest.RoundTrip(t, filePlane(t), []ferrytest.Proof{
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

	ferrytest.RoundTrip(t, filePlane(t, Separator("__")), []ferrytest.Proof{
		ferrytest.Type("map[string]string", ferrytest.MapEq[string](ferrytest.Eq[string]),
			ferrytest.Inside(map[string]string{"db_host": "h", "db_port": "p"},
				ferry.At("db_host"), ferry.String("h")),
		),
	})
}
