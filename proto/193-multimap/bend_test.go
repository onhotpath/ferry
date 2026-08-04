package multimap

import (
	"context"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// counting wraps a Source and counts the boundary calls one load makes, which is
// what prices the bend: asking Children before Get changes how many calls a
// dynamic container costs, in both directions depending on what the plane holds.
type counting struct {
	inner ferry.Source
	gets  *int
	kids  *int
}

func (c counting) Bind(a *ferry.AddressSet) (ferry.OpenFunc, error) {
	open, err := c.inner.Bind(a)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		r, rerr := open(ctx)
		if rerr != nil {
			return nil, rerr
		}

		return countingReader{Reader: r, lister: r.(ferry.Enumerator), gets: c.gets, kids: c.kids}, nil
	}, nil
}

type countingReader struct {
	ferry.Reader
	lister ferry.Enumerator
	gets   *int
	kids   *int
}

func (c countingReader) Get(ctx context.Context, addr ferry.Path) (ferry.Value, error) {
	*c.gets++

	return c.Reader.Get(ctx, addr)
}

func (c countingReader) Children(ctx context.Context, prefix ferry.Path) ([]ferry.Path, error) {
	*c.kids++

	return c.lister.Children(ctx, prefix)
}

// TestBoundaryCallCount prices the reordering over the three things a dynamic
// container address can hold on a plane that has a null.
//
// Run this on origin/main and on the branch carrying the bend and compare.
func TestBoundaryCallCount(t *testing.T) {
	for _, tc := range []struct {
		label string
		plane map[ferry.Path]ferry.Value
	}{
		{"slice with two elements", map[ferry.Path]ferry.Value{
			ferry.At("tags").Elem(0): ferry.String("a"),
			ferry.At("tags").Elem(1): ferry.String("b"),
		}},
		{"container absent", map[ferry.Path]ferry.Value{}},
		{"container null", map[ferry.Path]ferry.Value{
			ferry.At("tags"): ferry.Null(),
		}},
	} {
		gets, kids := 0, 0

		got, err := ferry.Load[Tagged](context.Background(),
			counting{inner: ferrytest.Static(tc.plane), gets: &gets, kids: &kids})

		t.Logf("%-26s Get=%d Children=%d  -> %s", tc.label, gets, kids, outcome(got, err))
	}
}
