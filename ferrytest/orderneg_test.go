package ferrytest_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestDriverCase20ReportsPositionsInTheOrderTheyRender is case 20, negative.
//
// The plane below lists a sequence's positions sorted by the text of their
// addresses, which is the one defect the case exists for: 0 1 10 11 2, correct
// for every list of nine members or fewer and wrong for the first one a user
// writes with ten.
//
// Nothing else in the suite moves, because every other fixture's sequence has
// two members and the two orders agree there.
func TestDriverCase20ReportsPositionsInTheOrderTheyRender(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, renderOrderedPlane())

	only := onlyLine(t, c)
	if !strings.Contains(only, "case 20") {
		t.Errorf("report = %q, want case 20 and only case 20", only)
	}

	if !strings.Contains(only, "segment-wise") {
		t.Errorf("report = %q, want the ordering rule named", only)
	}
}

// renderOrderedPlane is the memory plane with one defect: its enumeration of a
// sequence comes back in the order the addresses render.
func renderOrderedPlane() ferrytest.Plane {
	mem := ferrytest.MemPlane()
	p := mem

	p.Name = "rendered"
	p.Open = func() ferrytest.Instance {
		inst := mem.Open()
		inst.Source = renderOrderedSource{inner: inst.Source}

		return inst
	}

	return p
}

type renderOrderedSource struct{ inner ferry.Source }

func (s renderOrderedSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return renderOrderedReader{inner: r}, nil
	}, nil
}

// renderOrderedReader forwards everything the memory plane's reader answers and
// reorders one answer, which keeps the plane broken in exactly one way.
type renderOrderedReader struct{ inner ferry.Reader }

func (r renderOrderedReader) Get(ctx context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	return r.inner.Get(ctx, addr)
}

func (r renderOrderedReader) Probe(ctx context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	return probeThrough(ctx, r.inner, addr)
}

func (r renderOrderedReader) Children(ctx context.Context, addr ferry.CompositeAddr) ([]ferry.Segment, error) {
	got, err := childrenThrough(ctx, r.inner, addr)
	if err != nil {
		return nil, err
	}

	slices.SortFunc(got, func(a, b ferry.Segment) int { return strings.Compare(a.Text(), b.Text()) })

	return got, nil
}

// TestDriverCase20SkipsAReaderThatDoesNotEnumerate is [ferry.Enumerator]'s
// optionality asserted from the side that has to stay silent.
//
// A sequence has no members at all without one, so there is no order for the
// case to be about, and the skip is said out loud because a case that quietly
// did nothing is indistinguishable from a case that passed. The reader is
// blinded only for the set this case binds, since a plane that never enumerates
// would fail case 1 for a reason this case is not about.
func TestDriverCase20SkipsAReaderThatDoesNotEnumerate(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("unordered-blind", func(inst *ferrytest.Instance) {
		inst.Source = blindSource{inner: inst.Source, when: isDozenSet}
	}))

	if len(c.lines) != 0 {
		t.Errorf("a reader that does not enumerate reported %q, want the case skipped", c.lines)
	}

	if !anyLineContains(c.logs, "case 20 skipped") {
		t.Errorf("the suite logged %q, want case 20's skip said out loud", c.logs)
	}
}

// isDozenSet is the address set case 20 binds: exactly the one sequence.
func isDozenSet(addrs *ferry.AddressSet) bool {
	return addrs.Len() == 1 && hasPath(addrs, ferry.At("list"))
}
