package yaml_test

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/yaml"
)

// TestCancelledContextRefusesTheOpen asserts the open honours the deadline it
// is handed. Bind takes no context because it does no I/O; the open does the
// I/O, so it is the open that can be cancelled.
func TestCancelledContextRefusesTheOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	path := write(t, "port: 1\n")

	openRead, err := yaml.NewSource(path).Bind(ferry.NewAddressSet(ferry.At("port")))
	if err != nil {
		t.Fatalf("Source.Bind: %v", err)
	}

	if _, err := openRead(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("opening a reader under a cancelled context gave %v, want context.Canceled", err)
	}

	openWrite, err := yaml.NewSink(path).Bind(ferry.NewAddressSet(ferry.At("port")))
	if err != nil {
		t.Fatalf("Sink.Bind: %v", err)
	}

	if _, err := openWrite(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("opening a writer under a cancelled context gave %v, want context.Canceled", err)
	}
}

// TestDocumentsWithNoContent asserts that a file which parses to nothing is an
// empty plane rather than a failure, and that a dump into one still writes.
//
// A file holding nothing but comments is the case worth naming: it parses to no
// document at all, so the comments are what a dump into it replaces. That is a
// limit of the parser rather than a decision, and it is stated in the package
// documentation.
func TestDocumentsWithNoContent(t *testing.T) {
	type config struct {
		Port int `ferry:"port"`
	}

	for _, doc := range []string{"", "# nothing but a comment\n"} {
		t.Run(doc, func(t *testing.T) {
			path := write(t, doc)

			holds(t, openReader(t, path), ferry.At("port"), ferry.Value{})
			dumps(t, path, config{Port: 1}, "port: 1\n")
		})
	}
}

// dumps writes one value into the plane and asserts what the plane then holds,
// lifted out of its table for [holds]'s reason.
func dumps[T any](t *testing.T, path string, v T, want string) {
	t.Helper()

	if err := ferry.Dump(t.Context(), v, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got := read(t, path); got != want {
		t.Errorf("the plane holds %q, want %q", got, want)
	}
}

// TestSequencePositionTooWide covers the one failure the tree walk can report
// that is not the plane's: a position no int on this platform can hold.
//
// It is unreachable through a walk over a real value, because no slice in
// memory has that many elements, and it is reported rather than assumed away -
// silently reading it as absent on the way in and silently dropping it on the
// way out are the two things ADR-0001 rules out.
func TestSequencePositionTooWide(t *testing.T) {
	addr := ferry.At("list").Elem(math.MaxUint)

	holds(t, openReader(t, write(t, "list:\n  - a\n")), addr, ferry.Value{})

	err := openWriter(t, filepath.Join(t.TempDir(), "plane.yaml")).Set(t.Context(), addr, ferry.String("x"))
	if err == nil {
		t.Fatal("a write at a position no int can hold was taken")
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("the refusal was %v, want an error carrying ferry.ErrPlane", err)
	}
}

// TestPositionPastTheEnd asserts a read past a sequence's last element is
// absence rather than a failure: the plane simply does not hold that address.
func TestPositionPastTheEnd(t *testing.T) {
	holds(t, openReader(t, write(t, "list:\n  - a\n")), ferry.At("list").Elem(4), ferry.Value{})
}

// TestEmptyPathIsNotAnAddress asserts the write side refuses the one path that
// names no place. An address has at least one segment.
func TestEmptyPathIsNotAnAddress(t *testing.T) {
	err := openWriter(t, filepath.Join(t.TempDir(), "plane.yaml")).Set(t.Context(), ferry.Path{}, ferry.String("x"))
	if err == nil {
		t.Fatal("a write at the empty path was taken, and the empty path is not an address")
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("the refusal was %v, want an error carrying ferry.ErrPlane", err)
	}
}

// TestSinkRefusesAMalformedPlane asserts a dump into a file that does not parse
// fails rather than replacing it.
//
// It is the sink's half of the read side's parse refusal, and it matters more:
// a dump merges into the document that is there, so a sink that shrugged at an
// unparseable file would silently replace an operator's config with whatever
// the struct happened to hold.
func TestSinkRefusesAMalformedPlane(t *testing.T) {
	type config struct {
		Port int `ferry:"port"`
	}

	before := "key: [unclosed\n"
	path := write(t, before)

	err := ferry.Dump(t.Context(), config{Port: 1}, yaml.NewSink(path))
	if err == nil {
		t.Fatal("dumping over a file that is not a YAML document succeeded")
	}

	if !errors.Is(err, ferry.ErrValue) {
		t.Errorf("the dump reported %v, want an error carrying ferry.ErrValue", err)
	}

	if got := read(t, path); got != before {
		t.Errorf("the plane holds %q, want the %q it held before a refused dump", got, before)
	}
}

// TestMultipleDocumentsAreRefused asserts a stream of documents is refused at
// the open in both directions, rather than half-read and half-written.
//
// An address names a place in one document. Reading the first of a stream and
// ignoring the rest would be tolerable; dumping into it would write the first
// and drop the rest, which is silent data loss on the operator's file.
func TestMultipleDocumentsAreRefused(t *testing.T) {
	type config struct {
		Port int `ferry:"port"`
	}

	before := "port: 1\n---\nport: 2\n"
	path := write(t, before)

	if _, err := ferry.Load[config](t.Context(), yaml.NewSource(path)); !errors.Is(err, ferry.ErrValue) {
		t.Errorf("loading from a stream of documents gave %v, want an error carrying ferry.ErrValue", err)
	}

	if err := ferry.Dump(t.Context(), config{Port: 3}, yaml.NewSink(path)); !errors.Is(err, ferry.ErrValue) {
		t.Errorf("dumping into a stream of documents gave %v, want an error carrying ferry.ErrValue", err)
	}

	if got := read(t, path); got != before {
		t.Errorf("the plane holds %q, want the %q it held before a refused dump", got, before)
	}
}

// TestUnreadablePlaneIsAPlaneError asserts that a plane ferry cannot read at
// all is ErrPlane, which is the class the driver has no opinion to overrule:
// nothing was parsed, so nothing about the operator's document is known.
func TestUnreadablePlaneIsAPlaneError(t *testing.T) {
	open, err := yaml.NewSource(t.TempDir()).Bind(ferry.NewAddressSet(ferry.At("port")))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	if _, err = open(t.Context()); !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("opening a plane that is a directory gave %v, want an error carrying ferry.ErrPlane", err)
	}

	if errors.Is(err, ferry.ErrValue) {
		t.Errorf("opening a plane that is a directory declared ferry.ErrValue (%v), which is the class for a "+
			"document that was read and did not parse", err)
	}
}

// TestBytesAreEncodedAndNotWrittenRaw is the second defect this driver carries a
// regression test for.
//
// A prototype wrote raw bytes under a !!binary tag and read them back the same
// wrong way, so the pair was self-consistent and round-tripped, and every test
// that only composed the two halves stayed green. What caught it was the
// emitter refusing to emit invalid !!binary - a net that still exists in
// go.yaml.in/yaml/v3 v3.0.5 and that covers only the bytes which are not valid
// UTF-8. So the spelling is asserted here and pinned by a golden artefact, and
// the value asserted is the one where the net would not have fired: "hi" is
// valid UTF-8, so an unencoded write of it would have emitted cleanly.
func TestBytesAreEncodedAndNotWrittenRaw(t *testing.T) {
	type config struct {
		Printable []byte `ferry:"printable"`
		Raw       []byte `ferry:"raw"`
	}

	path := filepath.Join(t.TempDir(), "plane.yaml")
	want := config{Printable: []byte("hi"), Raw: []byte("\x00\xffA")}

	if err := ferry.Dump(t.Context(), want, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	const spelled = "printable: !!binary aGk=\nraw: !!binary AP9B\n"
	if got := read(t, path); got != spelled {
		t.Errorf("the plane holds %q, want %q", got, spelled)
	}

	got, err := ferry.Load[config](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if string(got.Printable) != "hi" || string(got.Raw) != "\x00\xffA" {
		t.Errorf("loaded %q and %q, want %q and %q", got.Printable, got.Raw, "hi", "\x00\xffA")
	}
}
