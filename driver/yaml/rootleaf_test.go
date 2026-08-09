package yaml_test

import (
	"bytes"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/yaml"
)

// TestARootLeafIsTheDocumentItself is the root address in both directions
// (#339): a destination type that is a leaf rather than a struct names one
// address, the empty path, and this driver has a name for it because an address
// here is a path through the document rather than a key built out of joined
// segments.
//
// Each kind is asserted on the file as well as on the value that loads back,
// because a round trip alone would pass over a document written at the wrong
// place: the whole of what is being tested is that the value lands at the top
// level and is spelled there as itself.
func TestARootLeafIsTheDocumentItself(t *testing.T) {
	rootLeaf(t, "null", (*int)(nil), "null\n")
	rootLeaf(t, "bool", true, "true\n")
	rootLeaf(t, "int", 8080, "8080\n")
	rootLeaf(t, "float", 3.5, "3.5\n")
	rootLeaf(t, "string", "hi", "hi\n")
}

// TestARootLeafOfBytesIsTaggedBinary is the sixth kind, which needs its own case
// because a []byte is not comparable. Its spelling is YAML's own !!binary, the
// same one a byte field at a key is written under.
func TestARootLeafOfBytesIsTaggedBinary(t *testing.T) {
	const doc = "!!binary aGk=\n"

	path := write(t, "")
	if err := ferry.Dump(t.Context(), []byte("hi"), yaml.NewSink(path)); err != nil {
		t.Fatalf("dumping bytes at the root: %v", err)
	}

	if got := read(t, path); got != doc {
		t.Errorf("the file holds %q, want %q: a root leaf is the document's own content node", got, doc)
	}

	got, err := ferry.Load[[]byte](t.Context(), yaml.NewSource(write(t, doc)))
	if err != nil {
		t.Fatalf("loading bytes from the root: %v", err)
	}

	if want := []byte("hi"); !bytes.Equal(got, want) {
		t.Errorf("the root loads back as %q, want %q", got, want)
	}
}

// TestARootLeafReplacesTheWholeDocument is the sharp edge the sink's
// documentation carries, asserted byte for byte (#339).
//
// It is dump-is-replace at the one address with no parent to be pruned from,
// rather than a mode this driver could be asked not to take: the keys a save
// leaves alone are the ones no address of the schema reaches, and a schema whose
// only address is the root reaches all of them.
func TestARootLeafReplacesTheWholeDocument(t *testing.T) {
	path := write(t, "keep: me\nother: 2\n")

	if err := ferry.Dump(t.Context(), 8080, yaml.NewSink(path)); err != nil {
		t.Fatalf("dumping at the root: %v", err)
	}

	if got, want := read(t, path), "8080\n"; got != want {
		t.Errorf("the file holds %q, want %q: a root leaf is written at the document's own content node, so "+
			"there is nothing above it for the save to leave alone", got, want)
	}
}

// TestAStructRootStillPatches is the reverse, and it is what keeps the case
// above from reading as a change to the merge (#339).
//
// A struct names a key per field, so a key no field maps is a key no address
// reaches and it survives the save exactly as it did before the root became an
// address.
func TestAStructRootStillPatches(t *testing.T) {
	type config struct {
		Port int `ferry:"port"`
	}

	path := write(t, "keep: me\nport: 1\n")

	if err := ferry.Dump(t.Context(), config{Port: 8080}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dumping a struct: %v", err)
	}

	if got, want := read(t, path), "keep: me\nport: 8080\n"; got != want {
		t.Errorf("the file holds %q, want %q: a save is still a merge for every address below the root",
			got, want)
	}
}

// rootLeaf is one leaf kind's round trip at the root, in a subtest of its own
// over a fresh file, which is ADR-0014's fresh-destination rule.
//
// The dump goes to an empty file and the load reads a file holding the document
// the dump is expected to write, so the two halves are held to the same spelling
// rather than only to each other.
func rootLeaf[T comparable](t *testing.T, name string, v T, doc string) {
	t.Helper()

	t.Run(name, func(t *testing.T) {
		rootLeafDumps(t, v, doc)
		rootLeafLoads(t, v, doc)
	})
}

// rootLeafDumps is the write half: an empty file holds the document and nothing
// around it after a dump at the root.
func rootLeafDumps[T comparable](t *testing.T, v T, doc string) {
	t.Helper()

	path := write(t, "")
	if err := ferry.Dump(t.Context(), v, yaml.NewSink(path)); err != nil {
		t.Fatalf("dumping at the root: %v", err)
	}

	if got := read(t, path); got != doc {
		t.Errorf("the file holds %q, want %q: a root leaf is the document's own content node", got, doc)
	}
}

// rootLeafLoads is the read half, over a file holding the document the write
// half is held to rather than over whatever the write half produced.
func rootLeafLoads[T comparable](t *testing.T, v T, doc string) {
	t.Helper()

	got, err := ferry.Load[T](t.Context(), yaml.NewSource(write(t, doc)))
	if err != nil {
		t.Fatalf("loading from the root: %v", err)
	}

	if got != v {
		t.Errorf("the root loads back as %v, want %v", got, v)
	}
}
