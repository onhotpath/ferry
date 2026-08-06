package yaml_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/yaml"
)

// TestNoPlaneKeyIsBuilt asserts the asymmetry this driver is the example of: it
// walks segments as a tree, so it builds no plane key and makes no call to
// core's key-function helper at all.
//
// It reads the package's own source, which is unusual and is the only thing
// that can assert a call that is not made. The behavioural half is below and
// cannot stand alone: a driver could pass it while still building a key table
// that happens not to collide, and the obligation ADR-0003 puts on a flattening
// driver is not one this driver discharges - it is one this driver does not
// have.
func TestNoPlaneKeyIsBuilt(t *testing.T) {
	helpers := []string{"ferry.NewKeys", "ferry.Keys", "ferry.KeyFunc"}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package: %v", err)
	}

	for _, e := range entries {
		if name := e.Name(); strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			namesNoHelper(t, name, helpers)
		}
	}
}

// namesNoHelper asserts one of this package's files mentions none of the
// helpers, and is a function of its own so that the scan above stays a scan.
func namesNoHelper(t *testing.T, name string, helpers []string) {
	t.Helper()

	src := readSource(t, name)

	for _, h := range helpers {
		if strings.Contains(src, h) {
			t.Errorf("%s names %s: a driver that walks segments as a tree builds no plane key, so it has no "+
				"injectivity obligation and nothing to call the key helper for", name, h)
		}
	}
}

// TestBindTakesCollidingAddresses is the behavioural half: the address pairs a
// flattening key function folds together are three distinct paths through a
// document, and both halves of this driver take them without a word.
func TestBindTakesCollidingAddresses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plane.yaml")
	addrs := addressesOf[colliding](t).set

	if _, err := yaml.NewSource(path).Bind(addrs); err != nil {
		t.Errorf("Source.Bind refused %d addresses that collide only under a flattening key function: %v",
			addrs.Len(), err)
	}

	if _, err := yaml.NewSink(path).Bind(addrs); err != nil {
		t.Errorf("Sink.Bind refused %d addresses that collide only under a flattening key function: %v",
			addrs.Len(), err)
	}
}

// TestCaseVariantAddressesAreDistinctPlaces asserts what the acceptance of that
// set is worth: the two spellings are two members of the mapping, written and
// read back apart, where a folding plane would have kept one of them.
func TestCaseVariantAddressesAreDistinctPlaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plane.yaml")
	w := openWriter(t, path)

	a := addressesOf[colliding](t)

	for i, at := range []ferry.Path{ferry.At("Host"), ferry.At("host"), ferry.At("db", "host")} {
		addr := a.leaf(t, at)

		if err := w.Set(t.Context(), addr, ferry.Number(strings.Repeat("1", i+1))); err != nil {
			t.Fatalf("Set(%s): %v", addr, err)
		}
	}

	commit(t, w)

	const want = "Host: 1\nhost: 11\ndb:\n  host: 111\n"
	if got := read(t, path); got != want {
		t.Errorf("the plane holds %q, want %q", got, want)
	}
}

// TestDeepAddressesRoundTrip walks a document that is deeper than any conformance
// fixture, in both directions, so that the tree walk is exercised past one level.
func TestDeepAddressesRoundTrip(t *testing.T) {
	type leaf struct {
		Name string `ferry:"name"`
	}

	type group struct {
		Members []leaf `ferry:"members"`
	}

	type config struct {
		Groups map[string]group `ferry:"groups"`
	}

	path := filepath.Join(t.TempDir(), "plane.yaml")
	want := config{Groups: map[string]group{"a": {Members: []leaf{{Name: "one"}, {Name: "two"}}}}}

	if err := ferry.Dump(t.Context(), want, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	const spelled = "groups:\n  a:\n    members:\n      - name: one\n      - name: two\n"
	if got := read(t, path); got != spelled {
		t.Errorf("the plane holds %q, want %q", got, spelled)
	}

	got, err := ferry.Load[config](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if len(got.Groups["a"].Members) != 2 || got.Groups["a"].Members[1].Name != "two" {
		t.Errorf("loaded %+v, want %+v", got, want)
	}
}

// TestShapeIsReplaced asserts what a dump does to a document whose shape has
// moved on: the node an address needs is made into the container that address
// needs, and everything the addresses do not reach is left alone.
func TestShapeIsReplaced(t *testing.T) {
	type db struct {
		Host string `ferry:"host"`
	}

	type config struct {
		DB db `ferry:"db"`
	}

	path := write(t, "db: a-scalar-where-a-mapping-goes\nkeep: me\n")

	if err := ferry.Dump(t.Context(), config{DB: db{Host: "localhost"}}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	const want = "db:\n  host: localhost\nkeep: me\n"
	if got := read(t, path); got != want {
		t.Errorf("the plane holds %q, want %q", got, want)
	}
}

// readSource reads one of this package's own files.
func readSource(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}

	return string(data)
}
