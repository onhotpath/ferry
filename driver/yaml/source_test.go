package yaml_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/yaml"
)

// The document the read-side cases are asked about. It carries one of each
// observation this plane can make, plus the one spelling this driver owns.
const observed = `nul: null
empty: ""
value: 8080
quoted: "8080"
yes: true
ratio: 3.5
bin: !!binary aGk=
when: 2026-08-02T12:00:00Z
list:
  - a
  - b
map:
  k: v
`

// TestObservations is the four distinct observations at four addresses that the
// typed boundary exists for, plus the kinds only a resolved tag can produce.
//
// A stringified boundary collapses the first four into one: null becomes "",
// 8080 becomes "8080", the quoted 8080 becomes the same "8080", and the missing
// key becomes "" as well. Here they are four values and five kinds.
func TestObservations(t *testing.T) {
	r := openReader(t, write(t, observed))

	cases := []struct {
		name string
		addr ferry.Path
		want ferry.Value
	}{
		{"an explicit null is Null", ferry.At("nul"), ferry.Null},
		{"an empty quoted string is an empty String", ferry.At("empty"), ferry.String("")},
		{"an unquoted number is a Number", ferry.At("value"), ferry.Number("8080")},
		{"a key that is not there is Absent", ferry.At("missing"), ferry.Value{}},
		{"a quoted number is a String", ferry.At("quoted"), ferry.String("8080")},
		{"a resolved bool is a Bool", ferry.At("yes"), ferry.Bool(true)},
		{"a float keeps the plane's own spelling", ferry.At("ratio"), ferry.Number("3.5")},
		{"a !!binary is Bytes", ferry.At("bin"), ferry.Bytes([]byte("hi"))},
		{"a timestamp is a String, because ferry's time codec reads one", ferry.At("when"),
			ferry.String("2026-08-02T12:00:00Z")},
		{"a sequence holds no value of its own", ferry.At("list"), ferry.Value{}},
		{"a mapping holds no value of its own", ferry.At("map"), ferry.Value{}},
		{"an element of a sequence is read by position", ferry.At("list").Elem(1), ferry.String("b")},
		{"a member of a mapping is read by name", ferry.At("map", "k"), ferry.String("v")},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { holds(t, r, c.addr, c.want) })
	}
}

// holds is one observation, lifted out of its table so that the table stays a
// table: a subtest body counts against the enclosing function's complexity.
func holds(t *testing.T, r ferry.Reader, addr ferry.Path, want ferry.Value) {
	t.Helper()

	got, err := r.Get(t.Context(), addr)
	if err != nil {
		t.Fatalf("Get(%s): %v", addr, err)
	}

	if got != want {
		t.Errorf("Get(%s) = %#v, want %#v", addr, got, want)
	}
}

// TestChildren asserts that enumeration answers with addresses rather than
// names, so that a sequence position and a mapping member stay different
// answers.
func TestChildren(t *testing.T) {
	r := openReader(t, write(t, observed))

	e, ok := r.(ferry.Enumerator)
	if !ok {
		t.Fatal("the reader does not enumerate, and a plane that cannot list is one no map can be loaded from")
	}

	cases := []struct {
		name   string
		prefix ferry.Path
		want   []ferry.Path
	}{
		{"a sequence answers with positions", ferry.At("list"),
			[]ferry.Path{ferry.At("list").Elem(0), ferry.At("list").Elem(1)}},
		{"a mapping answers with names", ferry.At("map"), []ferry.Path{ferry.At("map", "k")}},
		{"a scalar has no children", ferry.At("value"), nil},
		{"an address that is not there has no children", ferry.At("missing"), nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { lists(t, e, c.prefix, c.want) })
	}
}

// lists is one enumeration, lifted out of its table for [holds]'s reason.
func lists(t *testing.T, e ferry.Enumerator, prefix ferry.Path, want []ferry.Path) {
	t.Helper()

	got, err := e.Children(t.Context(), prefix)
	if err != nil {
		t.Fatalf("Children(%s): %v", prefix, err)
	}

	if !pathsEqual(got, want) {
		t.Errorf("Children(%s) = %v, want %v", prefix, got, want)
	}
}

// TestMalformedDocument is the ADR-0014 case 4 shape a suite cannot stage from
// outside: the driver's own parse failure.
//
// It must fail, it must fail inside the open rather than at Bind, and the class
// is ErrValue rather than ErrPlane - ferry reached the plane and read it, and
// what did not parse is the operator's file. Survey item 5.11 found a YAML
// provider discarding exactly this error and answering with an empty result.
func TestMalformedDocument(t *testing.T) {
	path := write(t, "key: [unclosed\n")

	open, err := yaml.NewSource(path).Bind(ferry.NewAddressSet(ferry.At("key")))
	if err != nil {
		t.Fatalf("Bind refused a legal address set over a plane it has not read: %v", err)
	}

	r, err := open(t.Context())
	if err == nil {
		t.Fatalf("opening a plane that is not a YAML document succeeded, and answered with %v", r)
	}

	if !errors.Is(err, ferry.ErrValue) {
		t.Errorf("opening a malformed document failed with %v, want an error carrying ferry.ErrValue: a document "+
			"that does not parse is the operator's file and not an unreachable plane", err)
	}

	if errors.Is(err, ferry.ErrPlane) {
		t.Errorf("opening a malformed document declared ferry.ErrPlane (%v), which is what core would have "+
			"defaulted to and what the driver is here to overrule", err)
	}
}

// TestMalformedDocumentThroughLoad asserts the same failure survives to a
// caller of ferry.Load, which is where anybody outside this package meets it.
func TestMalformedDocumentThroughLoad(t *testing.T) {
	type config struct {
		Key string `ferry:"key"`
	}

	_, err := ferry.Load[config](t.Context(), yaml.NewSource(write(t, "key: [unclosed\n")))
	if err == nil {
		t.Fatal("loading from a malformed document succeeded, so every field is at its zero value and nothing " +
			"says why")
	}

	if !errors.Is(err, ferry.ErrValue) {
		t.Errorf("Load reported %v, want an error carrying ferry.ErrValue", err)
	}
}

// TestMissingFileHoldsNothing asserts that a plane with no file at it opens and
// reports absence, rather than failing.
//
// Absent is how a plane says it does not hold an address, and a config file
// that has not been written yet holds none of them. It is also what makes a
// Dump to a fresh path the way the first file gets written.
func TestMissingFileHoldsNothing(t *testing.T) {
	r := openReader(t, filepath.Join(t.TempDir(), "absent.yaml"))

	got, err := r.Get(t.Context(), ferry.At("anything"))
	if err != nil {
		t.Fatalf("Get against a plane with no file: %v", err)
	}

	if got != (ferry.Value{}) {
		t.Errorf("Get against a plane with no file answered %#v, want absent", got)
	}
}

// TestAliasIsFollowed asserts that a document using YAML's anchors reads as the
// values it names rather than as a subtree of absences.
func TestAliasIsFollowed(t *testing.T) {
	r := openReader(t, write(t, "base: &b\n  host: localhost\nnext: *b\n"))

	got, err := r.Get(t.Context(), ferry.At("next", "host"))
	if err != nil {
		t.Fatalf("Get through an alias: %v", err)
	}

	if want := ferry.String("localhost"); got != want {
		t.Errorf("Get through an alias = %#v, want %#v", got, want)
	}
}

// TestUnreadableScalars asserts that a value this plane cannot read is a loud
// refusal rather than a plausible wrong answer.
func TestUnreadableScalars(t *testing.T) {
	cases := []struct {
		name string
		doc  string
	}{
		{"a hand-written !!bool that is neither true nor false", "v: !!bool yes\n"},
		{"a !!binary that is not base64", "v: !!binary not-base64!\n"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { refuses(t, c.doc) })
	}
}

// refuses reads the one address a document holds and asserts a refusal, lifted
// out of its table for [holds]'s reason.
func refuses(t *testing.T, doc string) {
	t.Helper()

	got, err := openReader(t, write(t, doc)).Get(t.Context(), ferry.At("v"))
	if err == nil {
		t.Fatalf("Get answered %#v, want a refusal", got)
	}

	if !errors.Is(err, ferry.ErrValue) {
		t.Errorf("Get failed with %v, want an error carrying ferry.ErrValue", err)
	}
}

// write puts a document in a directory of its own and hands back the path.
func write(t *testing.T, doc string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "plane.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	return path
}

// openReader binds a source over the plane and opens it, which is the two
// phases a load goes through.
func openReader(t *testing.T, path string) ferry.Reader {
	t.Helper()

	open, err := yaml.NewSource(path).Bind(ferry.NewAddressSet(ferry.At("unused")))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	r, err := open(t.Context())
	if err != nil {
		t.Fatalf("opening the plane: %v", err)
	}

	return r
}

// pathsEqual compares two address lists, treating nil and empty as one answer:
// a plane with nothing under an address has no children either way.
func pathsEqual(got, want []ferry.Path) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}
