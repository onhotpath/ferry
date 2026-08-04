package yaml_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/yaml"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestDriver is the conformance suite, in one call.
//
// The plane declares all six kinds, which is a declaration and not a wish: a
// resolved tag gives this plane a null, a boolean, a number and a string that
// stay apart, and the two spellings this driver owns give it bytes. A kind
// declared and then refused is a failure rather than a refusal (ADR-0005).
func TestDriver(t *testing.T) {
	ferrytest.Driver(t, plane(t))
}

// plane describes this driver to the suite. Open mints a fresh file in a fresh
// directory on every call, which is ADR-0014's fresh-destination rule: a plane
// shared across cases is the defect that hides a broken second walk.
func plane(t *testing.T) ferrytest.Plane {
	t.Helper()

	return ferrytest.Plane{
		Name: "yaml",
		Kinds: []ferry.VKind{
			ferry.KindAbsent, ferry.KindNull, ferry.KindBool,
			ferry.KindNumber, ferry.KindString, ferry.KindBytes,
		},
		Open: func() ferrytest.Instance {
			path := filepath.Join(t.TempDir(), "plane.yaml")

			return ferrytest.Instance{
				Source:   yaml.NewSource(path),
				Sink:     yaml.NewSink(path),
				Contents: func() ([]byte, error) { return os.ReadFile(path) },
			}
		},
		Golden: golden(),
	}
}

// golden pins this driver's own spelling of three fixed values, which is the
// one thing a round trip structurally cannot see: a round trip tests a function
// against its own inverse, so changing an encoder and its decoder together is
// invisible to it (ADR-0013).
//
// A change to one of these strings is a change to what every YAML file ferry
// has ever written means, and it is a major version of this module rather than
// a fixture edit.
func golden() []ferrytest.Artefact {
	type typed struct {
		Port  int      `ferry:"port"`
		Label string   `ferry:"label"`
		Debug bool     `ferry:"debug"`
		Ratio float64  `ferry:"ratio"`
		Tags  []string `ferry:"tags"`
	}

	type binary struct {
		B []byte `ferry:"b"`
	}

	type raw struct {
		S string `ferry:"s"`
	}

	return []ferrytest.Artefact{
		// The five values the typed boundary is for. A stringified boundary
		// writes all five as text and loses four of them permanently.
		ferrytest.Golden(typed{Port: 8080, Label: "8080", Debug: true, Ratio: 3.5},
			"port: 8080\nlabel: \"8080\"\ndebug: true\nratio: 3.5\ntags: null\n"),
		// Bytes are base64 under YAML's own tag, and this row is what makes
		// moving both halves of that encoding at once turn CI red.
		ferrytest.Golden(binary{B: []byte("hi")}, "b: !!binary aGk=\n"),
		// A Go string that is not valid UTF-8, under the one tag this driver
		// invents.
		ferrytest.Golden(raw{S: "\xff\xfe"}, "s: !ferry:str //4=\n"),
	}
}
