package ferry

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// fuse is a registered type whose codec panics on one text and works on every
// other, so the registration passes the zero-value check and exactly one
// address reaches the panic.
type fuse string

// theFuse is the one value both halves refuse to return from, and it is the
// same text in both directions so one fixture covers Load and Dump.
const theFuse = "boom"

// fuseCodec panics with a value the report has to carry, chosen so that a
// report quoting it cannot be mistaken for a refusal ferry wrote.
func fuseCodec() Codec {
	return StringValue(
		func(f fuse) (string, error) {
			if f == theFuse {
				panic("nil map write in the encode half")
			}

			return string(f), nil
		},
		func(text string) (fuse, error) {
			if text == theFuse {
				panic("nil map write in the decode half")
			}

			return fuse(text), nil
		})
}

// fuseConf is one healthy leaf, one whose codec panics, and one the plane
// answers with a value the leaf cannot take, so a single run holds a recovered
// panic and an ordinary refusal beside each other.
type fuseConf struct {
	Good  string `ferry:"good"`
	Bad   fuse   `ferry:"bad"`
	Wrong int    `ferry:"wrong"`
}

func fusePlane() *plane {
	return newPlane(map[Path]Value{
		At("good"):  String("ok"),
		At("bad"):   String(theFuse),
		At("wrong"): String("not a number"),
	})
}

// recovered runs f and reports what it panicked with, or nil where it
// returned. It is how a test asserts that a panic is still a panic without
// taking the run down with it.
func recovered(f func()) (p any) {
	defer func() { p = recover() }()

	f()

	return nil
}

// elementAt is the one element of a report that sits at addr.
func elementAt(t *testing.T, err error, addr Path) error {
	t.Helper()

	for _, e := range Elements(err) {
		var fe *Error
		if errors.As(e, &fe) && fe.Address() == addr {
			return e
		}
	}

	t.Fatalf("no element at %s in\n%+v", addr, err)

	return nil
}

// TestACodecPanicOnLoadIsAnAddressedError is the fence's whole promise on the
// read side: one panicking codec costs one address, the panic value survives
// into the report, and the ordinary refusal beside it is still reported, which
// is only possible because the walk continued past the panic.
func TestACodecPanicOnLoadIsAnAddressedError(t *testing.T) {
	t.Parallel()

	p := fusePlane()

	_, err := Load[fuseConf](t.Context(), planeSource{p: p}, WithRegistry(registryWith(t, fuseCodec())))
	if err == nil {
		t.Fatal("a panicking codec loaded cleanly")
	}

	if got := len(Elements(err)); got != 2 {
		t.Fatalf("report holds %d elements, want the panic and the refusal beside it:\n%+v", got, err)
	}

	panicked := elementAt(t, err, At("bad"))
	if !errors.Is(panicked, ErrPanic) {
		t.Errorf("%v does not match ErrPanic", panicked)
	}

	if !strings.Contains(panicked.Error(), "nil map write in the decode half") {
		t.Errorf("the report lost the recovered value:\n%+v", err)
	}

	if refused := elementAt(t, err, At("wrong")); !errors.Is(refused, ErrValue) {
		t.Errorf("%v does not match ErrValue", refused)
	}

	// The healthy sibling was still asked for, which is what "the walk
	// continued" means at an address that did not fail.
	if !slices.Contains(p.got, At("good")) {
		t.Errorf("the healthy sibling was never read: %v", p.got)
	}
}

// TestACodecPanicOnLoadStillClosesTheReader is #254 on the read side. The fence
// converts the panic, and the release runs whether it converted one or not.
func TestACodecPanicOnLoadStillClosesTheReader(t *testing.T) {
	t.Parallel()

	p := fusePlane()

	if _, err := Load[fuseConf](t.Context(), planeSource{p: p},
		WithRegistry(registryWith(t, fuseCodec()))); err == nil {
		t.Fatal("a panicking codec loaded cleanly")
	}

	if p.closes != 1 {
		t.Errorf("closed %d times after a codec panicked, want 1", p.closes)
	}
}

// TestACodecPanicOnDumpLeavesTheSinkClosedWithoutCommit is the identical shape
// on the write side, and it is ADR-0004's abort signal on a path that used to
// have no signal at all.
func TestACodecPanicOnDumpLeavesTheSinkClosedWithoutCommit(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	v := fuseConf{Good: "ok", Bad: theFuse, Wrong: 8080}

	err := Dump(t.Context(), v, planeSink{p: p}, WithRegistry(registryWith(t, fuseCodec())))
	if err == nil {
		t.Fatal("a panicking codec dumped cleanly")
	}

	panicked := elementAt(t, err, At("bad"))
	if !errors.Is(panicked, ErrPanic) {
		t.Errorf("%v does not match ErrPanic", panicked)
	}

	if !strings.Contains(panicked.Error(), "nil map write in the encode half") {
		t.Errorf("the report lost the recovered value:\n%+v", err)
	}

	if p.commits != 0 || p.closes != 1 {
		t.Errorf("committed %d and closed %d times after a codec panicked, want 0 and 1", p.commits, p.closes)
	}
}

// TestAPanicOutsideTheFenceUnwindsAndStillReleases is the fence's boundary
// asserted from the other side, and the release fix asserted where nothing
// converts the panic for it: a plane that panics is not a codec, so the panic
// keeps unwinding, and the handle is closed on the way out.
func TestAPanicOutsideTheFenceUnwindsAndStillReleases(t *testing.T) {
	t.Parallel()

	t.Run("load", loadPanicUnwindsAndCloses)
	t.Run("dump", dumpPanicUnwindsAndCloses)
}

func loadPanicUnwindsAndCloses(t *testing.T) {
	t.Parallel()

	p := newPlane(contents())
	p.onGet = func() { panic(errOutage) }

	got := recovered(func() { _, _ = Load[walkConf](t.Context(), planeSource{p: p}) })
	if got == nil {
		t.Fatal("a panic outside the fence was swallowed, and it must keep unwinding")
	}

	if p.closes != 1 {
		t.Errorf("closed %d times while a panic unwound, want 1", p.closes)
	}
}

func dumpPanicUnwindsAndCloses(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	p.onSet = func() { panic(errOutage) }

	got := recovered(func() { _ = Dump(t.Context(), filled(), planeSink{p: p}) })
	if got == nil {
		t.Fatal("a panic outside the fence was swallowed, and it must keep unwinding")
	}

	if p.commits != 0 || p.closes != 1 {
		t.Errorf("committed %d and closed %d times while a panic unwound, want 0 and 1", p.commits, p.closes)
	}
}
