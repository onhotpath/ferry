package concwalk

import (
	"errors"
	"testing"
)

// #254 reproduced in miniature: the shipped straight-line shape leaks the
// handle when the walk panics.
func TestShippedShapeLeaksOnPanic(t *testing.T) {
	p := &plane{}
	func() {
		defer func() { _ = recover() }()
		_ = entryShipped(p, func() error { panic("codec parse half panicked") })
	}()
	if p.closed {
		t.Fatal("shipped shape closed on panic - the defect this test documents is gone")
	}
}

// The proposed rule: release is deferred unconditionally. On a panic the
// handle is closed with Commit never called - closed-without-Commit stays
// the abort signal even on the path nobody planned.
func TestDeferredReleaseClosesOnPanic(t *testing.T) {
	p := &plane{}
	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		_ = entryDeferred(p, func() error { panic("codec parse half panicked") })
	}()
	if !panicked {
		t.Fatal("the panic must keep unwinding - swallowing it would hide the defect")
	}
	if !p.closed {
		t.Fatal("release did not run on the panic path")
	}
	if p.committed {
		t.Fatal("Commit ran on the panic path - abort signal destroyed")
	}
}

// The straight-line protocol is unchanged by the defer: Commit before Close
// on success, Close-without-Commit on error, walk error preserved.
func TestDeferredEquivalentOnStraightLine(t *testing.T) {
	ok := &plane{}
	if err := entryDeferred(ok, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !ok.committed || !ok.closed {
		t.Fatalf("success protocol broken: %+v", ok)
	}

	sentinel := errors.New("two bad leaves")
	bad := &plane{}
	err := entryDeferred(bad, func() error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("walk error lost: %v", err)
	}
	if bad.committed {
		t.Fatal("Commit ran after a failed walk")
	}
	if !bad.closed {
		t.Fatal("Close skipped after a failed walk")
	}
}
