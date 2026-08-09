package main

import (
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestMemoryPlaneAtTheEmptyPath is the ferrytest wrapperOf question: can a
// single codec type be exercised at the root, with no synthetic wrapper struct?
func TestMemoryPlaneAtTheEmptyPath(t *testing.T) {
	ferry.RootSentinel = ""

	src := ferrytest.Static(map[ferry.Path]ferry.Value{{}: ferry.Number("8080")})

	got, err := ferry.Load[int](t.Context(), src)
	t.Logf("Load[int] from Static{Path{}: Number(8080)} -> %#v, err=%v", got, err)
}
