package ferrytest

import (
	"context"
	"slices"
	"testing"

	"github.com/onhotpath/ferry"
)

// Two claims in this package have no behaviour to observe them through, so they
// are asserted from inside.
//
// This is the same exception the pure-value units in core get. The proof's
// three columns are no longer among them: [RoundTrip] reads the relation and
// the cases through [ferry.Dump] and [ferry.Load], and roundtrip_test.go
// asserts on what it reports, which is the seam this file promised to move them
// to. What is left is a key function whose only observable behaviour is also
// produced by keying on the address type, and an interface set whose two
// spellings core's own entry point cannot tell apart.

// TestStoreKeysAreRenderings is ADR-0003's first obligation, read off the key
// function itself.
//
// The obligation is about the key rather than about a behaviour, and the only
// behaviour it produces - that two spellings of one address are one slot - is
// also produced by keying on ferry.Path directly. What separates them is the
// claim the memory plane exists to make executable: the canonical rendering
// already identifies an address, so a plane with no format of its own needs
// nothing else to key by.
func TestStoreKeysAreRenderings(t *testing.T) {
	s := newMemStore()

	addrs := []ferry.Path{
		ferry.At("db", "host"),
		ferry.At("tags").Elem(0),
		ferry.At("odd/name"),
		ferry.At("Host"),
		ferry.At("host"),
	}

	for _, addr := range addrs {
		s.put(addr, ferry.String("x"))
	}

	want := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		want = append(want, addr.String())
	}

	got := make([]string, 0, len(s.entries))
	for k := range s.entries {
		got = append(got, k)
	}

	slices.Sort(got)
	slices.Sort(want)

	if !slices.Equal(got, want) {
		t.Errorf("store keys = %q, want the canonical renderings %q", got, want)
	}
}

// TestProofIsSealed asserts the seal that stops anything outside this package
// from being a [Proof].
//
// It is here because a method nothing ever calls is a method nothing ever
// checks is there: the seal has no behaviour, so no suite can observe it, and
// what it buys is the freedom for the suites to grow the methods they need
// without every proof outside this repository breaking.
func TestProofIsSealed(t *testing.T) {
	p, ok := Type("int", Eq[int], At(0, ferry.Number("0"))).(typeProof[int])
	if !ok {
		t.Fatal("Type did not build a typeProof")
	}

	p.proof()
}

// TestWrapWriterKeepsTheOptionalInterfaces is the recording sink's whole
// obligation, and it cannot be asserted through the entry point.
//
// A shell that implemented Commit and Close unconditionally and forwarded them
// to nothing would behave identically under [ferry.Dump] - a no-op Commit and a
// no-op Close return nil, which is what a writer without them produces anyway.
// What it would break is everything that asks a sink what it is: ADR-0014's
// driver conformance case 6 asserts that Commit runs only on success and that a
// Close failure appears in the reported error set, and against a wrapper that
// always answers yes it would be asserting about the wrapper.
func TestWrapWriterKeepsTheOptionalInterfaces(t *testing.T) {
	cases := []optionalCase{
		{name: "neither", inner: plainWriter{}},
		{name: "commits", inner: committingWriter{}, commits: true},
		{name: "releases", inner: releasingWriter{}, releases: true},
		{name: "both", inner: bothWriter{}, commits: true, releases: true},
	}

	for _, c := range cases {
		t.Run(c.name, c.assert)
	}
}

// optionalCase is one inner writer and the interface set its wrapper must have.
type optionalCase struct {
	name     string
	inner    ferry.Writer
	commits  bool
	releases bool
}

// assert reads the two optional interfaces off the wrapper.
func (c optionalCase) assert(t *testing.T) {
	t.Helper()

	w := wrapWriter(c.inner, map[ferry.Path]ferry.Value{})

	if _, ok := w.(ferry.Committer); ok != c.commits {
		t.Errorf("wrapped writer is a Committer = %v, want %v", ok, c.commits)
	}

	if _, ok := w.(ferry.Releaser); ok != c.releases {
		t.Errorf("wrapped writer is a Releaser = %v, want %v", ok, c.releases)
	}
}

// The four writers the table above wraps, which exist only to have the four
// combinations of the two optional interfaces.
type (
	plainWriter      struct{}
	committingWriter struct{ plainWriter }
	releasingWriter  struct{ plainWriter }
	bothWriter       struct{ plainWriter }
)

func (plainWriter) Set(context.Context, ferry.Path, ferry.Value) error { return nil }

func (committingWriter) Commit(context.Context) error { return nil }

func (releasingWriter) Close() error { return nil }

func (bothWriter) Commit(context.Context) error { return nil }

func (bothWriter) Close() error { return nil }
