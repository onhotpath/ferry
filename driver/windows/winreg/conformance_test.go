package winreg_test

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/windows/winreg"
	"github.com/onhotpath/ferry/ferrytest"
)

// base is the subkey every plane in this package is built over, so that the
// driver's own prefix composes with the address set in the conformance run rather
// than only in the test that names it.
const base = `Software\Example`

// TestDriver is ADR-0014's conformance suite in one call, which is what a driver
// author writes and the whole of what this package has to pass.
func TestDriver(t *testing.T) {
	t.Parallel()

	ferrytest.Driver(t, regPlane())
}

// regPlane describes this driver to the conformance suite.
//
// # The kinds are a declaration, and this is what it declares
//
// Every kind but Null. A registry value cannot exist without a type and every
// type this driver writes carries a payload, so there is nothing a null could be
// stored as that would not also be the spelling of empty text or of no bytes.
// Bool and Number are carried because they survive the trip: both are stored as
// their own text and every Go leaf takes a String and parses it, so a proof
// carrying either round-trips exactly.
//
// Bytes is carried outright rather than as text, because REG_BINARY is a real
// type here and holds every byte including NUL.
//
// The Except is the two Go strings REG_SZ cannot spell, and it is a property of
// the format rather than of a value: a registry string is UTF-16 and ends at its
// first NUL.
func regPlane() ferrytest.Plane {
	return ferrytest.Plane{
		Name: "winreg",
		Kinds: []ferry.VKind{
			ferry.KindAbsent, ferry.KindBool, ferry.KindNumber, ferry.KindString, ferry.KindBytes,
		},
		Except: unspellable,
		Open: func() ferrytest.Instance {
			// A fresh store per call, which is ADR-0014's fresh-destination
			// rule: a plane shared across cases is the defect that hides a
			// broken second walk.
			store := newFake()

			return ferrytest.Instance{
				Source:   winreg.NewSource(winreg.CurrentUser, base, winreg.Store(store)),
				Sink:     winreg.NewSink(winreg.CurrentUser, base, winreg.Store(store)),
				Contents: func() ([]byte, error) { return store.contents(), nil },
			}
		},
		Golden: goldens(),
	}
}

// unspellable is [ferrytest.Plane.Except]: the values inside a kind this plane
// declares that REG_SZ cannot write down.
//
// A NUL is the first, because a registry string ends at one and every Windows
// reader would see the text up to it. Bytes that are not valid UTF-8 are the
// second, because a registry string is UTF-16 and the conversion replaces what it
// cannot encode. Neither is a Bytes value's problem: those are written as
// REG_BINARY, which carries every byte.
func unspellable(v ferry.Value) bool {
	s, err := v.AsString()

	return err == nil && (strings.IndexByte(s, 0) >= 0 || !utf8.ValidString(s))
}

// goldens pin this driver's own spelling of three fixed values.
//
// They are the one thing a round trip structurally cannot see: a round trip tests
// a function against its own inverse, so changing both halves together is
// invisible to it. What is pinned here is the whole of what this plane holds - the
// subkey each address lives under, the value name inside it, the registry type the
// value is stored as, and the payload - and a change to any of them is a change to
// what every registry ferry has ever written means (ADR-0013).
//
// The REG_SZ row and the REG_BINARY row are the two spellings this driver
// commits to, and the third row is the schema whose root is a single value,
// stored at the key's own unnamed (Default) value.
func goldens() []ferrytest.Artefact {
	return []ferrytest.Artefact{
		ferrytest.Golden(leaves{Host: "h", Port: 8080, Raw: []byte("\x00\xffA"), Wait: 30 * time.Second},
			"key \"\"\n"+
				"val \"\" \"host\" REG_SZ \"h\"\n"+
				"val \"\" \"port\" REG_SZ \"8080\"\n"+
				"val \"\" \"raw\" REG_BINARY \"\\x00\\xffA\"\n"+
				"val \"\" \"wait\" REG_SZ \"30s\"\n"),

		ferrytest.Golden(nested{DB: section{Host: "h"}, Tags: []string{"a", "b"}},
			"key \"\"\n"+
				"key \"db\"\n"+
				"key \"tags\"\n"+
				"val \"db\" \"host\" REG_SZ \"h\"\n"+
				"val \"tags\" \"0\" REG_SZ \"a\"\n"+
				"val \"tags\" \"1\" REG_SZ \"b\"\n"),

		ferrytest.Golden(8080, "key \"\"\nval \"\" \"\" REG_SZ \"8080\"\n"),
	}
}

// leaves is the golden row that pins one spelling per boundary kind this plane
// carries: text, a number, opaque bytes and an identity leaf.
type leaves struct {
	Host string        `ferry:"host"`
	Port int           `ferry:"port"`
	Raw  []byte        `ferry:"raw"`
	Wait time.Duration `ferry:"wait"`
}

// nested is the golden row that pins the key structure: a nested struct is a
// subkey, a sequence is a subkey holding one value per position, and a position is
// its own base-10 text.
type nested struct {
	DB   section  `ferry:"db"`
	Tags []string `ferry:"tags"`
}

type section struct {
	Host string `ferry:"host"`
}

// sink is the write half over one store, built the way this package's
// documentation tells a caller to build it.
func sink(store winreg.Registry) *winreg.Sink {
	return winreg.NewSink(winreg.CurrentUser, base, winreg.Store(store))
}
