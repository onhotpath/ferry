package yaml_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/yaml"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestDriver is the conformance suite, in one call.
//
// The plane declares all six kinds, which is a declaration and not a wish: a
// resolved tag gives this plane a null, a boolean, a number and a string that
// stay apart, and !!binary gives it bytes. A kind declared and then refused is a
// failure rather than a refusal (ADR-0005).
func TestDriver(t *testing.T) {
	ferrytest.Driver(t, plane(t))
}

// TestWatchConformance is the watching suite, in one call, over the real
// mechanism: the seven properties a watchable driver owes its caller, asked of
// this driver's own fsnotify watch (ADR-0020).
//
// The goroutine count around it is the eighth thing the suite cannot ask: every
// stream it opens is cancelled by the time it returns, so anything still
// running afterwards is this driver's.
func TestWatchConformance(t *testing.T) {
	before := runtime.NumGoroutine()

	ferrytest.Watchable(t, watchPlane(t))

	assertNoLeak(t, before)
}

// watchPlane describes this driver's watch to the suite: a file in a directory
// of its own, changed by writing it, and lost by taking the directory away.
//
// The directory is this test's own and not t.TempDir() itself, because losing
// the watch means removing it and the cleanup t.TempDir registers must still
// find something to remove.
func watchPlane(t *testing.T) ferrytest.WatchPlane {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "watched")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatalf("making the directory the watch is on: %v", err)
	}

	path := filepath.Join(dir, planeName)

	return ferrytest.WatchPlane{
		Name:   "yaml",
		Open:   func() ferry.WatchableSource { return yaml.NewSource(path).Watched() },
		Change: func(to string) { edit(t, path, "host: "+to+"\n") },
		Lose: func() {
			if err := os.RemoveAll(dir); err != nil {
				t.Fatalf("removing the directory the watch is on: %v", err)
			}
		},
		Unwatchable: func() ferry.WatchableSource { return yaml.NewSource("").Watched() },
		Settle:      watchSettle,
	}
}

// watchSettle is how long this driver may take to notice a change: an inotify
// event and a settle window, with room for a machine running the whole suite
// under the race detector.
const watchSettle = 3 * time.Second

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
		Except: notUnicode,
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

// notUnicode is this plane's one exception to the kinds above, and it is a
// property of YAML rather than of the values the suite happens to carry: a Go
// string is a byte sequence and a YAML string is a Unicode one, so a string that
// is not valid UTF-8 has no spelling here.
//
// Declaring it costs a refusal rather than buying a skip. The suite holds an
// excepted value to exactly what it holds a kind this plane never declared to,
// so a driver that mangled such a string instead of refusing it would be
// reported here (ADR-0005, and #157 for the limitation itself).
func notUnicode(v ferry.Value) bool {
	s, err := v.AsString()

	return err == nil && !utf8.ValidString(s)
}

// golden pins this driver's own spelling of two fixed values, which is the one
// thing a round trip structurally cannot see: a round trip tests a function
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

	return []ferrytest.Artefact{
		// The five values the typed boundary is for. A stringified boundary
		// writes all five as text and loses four of them permanently.
		ferrytest.Golden(typed{Port: 8080, Label: "8080", Debug: true, Ratio: 3.5},
			"port: 8080\nlabel: \"8080\"\ndebug: true\nratio: 3.5\ntags: null\n"),
		// Bytes are base64 under YAML's own tag, and this row is what makes
		// moving both halves of that encoding at once turn CI red. It is the
		// only spelling this driver owns: a Go string that is not valid UTF-8
		// has no row here because it has no spelling, and pinning one would make
		// a name of ferry's own a compatibility promise inside an operator's
		// file (ADR-0005, #157).
		ferrytest.Golden(binary{B: []byte("hi")}, "b: !!binary aGk=\n"),
	}
}
