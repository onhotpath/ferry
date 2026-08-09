package protect_test

import (
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/windows/protect"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestDriver runs the driver conformance suite over a plane with this decorator
// in front of it, and it is the strongest statement this package can make about
// itself: whatever the suite says about a plane, it says the same thing about
// that plane decorated.
//
// The suite compiles its own fixtures, and none of them carries a protect tag,
// so nothing here is encrypted. That is the point rather than a gap. What is
// under test is transparency - that every one of the twenty-three cases sees the
// plane it would have seen, which means every optional interface the plane has
// survived the wrapping and none it lacks was invented. The encryption itself is
// proved in this package's own tests, against a plane whose contents a test can
// look inside.
//
// The registry declares this package's tag key, because [protect.FromTags]
// refuses a schema compiled without it - which is the whole of what the
// declaration buys, and it applies to a schema with no secrets in it exactly as
// it applies to one with ten.
func TestDriver(t *testing.T) {
	t.Parallel()

	ferrytest.Driver(t, protectedPlane(), ferry.WithRegistry(declaring()))
}

// protectedPlane is ferrytest's own memory plane with both halves decorated.
//
// Both halves come from one Open call and share one protector, which is the same
// rule a caller follows: a sink protecting under one descriptor and a source
// unprotecting through another never meet.
func protectedPlane() ferrytest.Plane {
	p := ferrytest.MemPlane()
	inner := p.Open

	p.Name = "protect over memory"
	p.Open = func() ferrytest.Instance {
		i := inner()
		k := newKeeper()

		i.Source = protect.Over(i.Source, protect.LocalSystem, protect.FromTags(), protect.Using(k))
		i.Sink = protect.OverSink(i.Sink, protect.LocalSystem, protect.FromTags(), protect.Using(k))

		return i
	}

	return p
}
