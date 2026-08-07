package ferrytest

import (
	"context"
	"fmt"
	"slices"

	"github.com/onhotpath/ferry"
)

// caseOrdering is case 20: a sequence of twelve members is listed by position
// and not by the rendering of its addresses (ADR-0003).
//
// The two orders agree up to nine positions and part company at ten, where
// sorting the text gives 0 1 10 11 2 and sorting the segments gives 0 1 2. Every
// other fixture in this suite carries two members, so a driver that answers in
// text order passes all of them and loses the shape of the first list a user
// writes with ten entries in it. Core refuses the scrambled answer at the load,
// because a sequence's positions have to run from zero upwards with none
// missing, so what a user sees is a list that will not load and a plane that
// looks fine.
//
// Case 5 sorts the driver's answer before comparing it, deliberately, because
// what that case is about is the set and the kind of each member. This case is
// about nothing else: it asks only where the members landed, and it says nothing
// about which of them came back. So a [ferry.Enumerator] that answers the wrong
// members, or fails, is case 5's failure and is skipped here rather than
// reported twice.
//
// It is skipped, out loud, for a reader that does not enumerate, since a
// sequence has no members at all without one, and for a plane that could not
// hold the twelve members in the first place, which is where a plane declaring
// no [ferry.KindString] lands.
func (d *driverRun) caseOrdering() {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Sink == nil || inst.Source == nil {
		return
	}

	set, ok := fixtureSet[dozen](d, caseOrderingNo)
	if !ok {
		return
	}

	if err := ferry.Dump(inst.ctx(), dozenFixture(), inst.Sink, d.opts...); err != nil {
		d.skip(caseOrderingNo, "the fixture could not be written, so there is no sequence to list in any "+
			"order, and case 1 is where a dump that cannot happen is reported: "+err.Error())

		return
	}

	d.positionsAreInOrder(inst, set)
}

// positionsAreInOrder lists the twelve members and holds the answer to the order
// it was written in.
//
// Everything about the answer other than its order belongs to case 5, so a
// Children that fails, or that hands back anything but the twelve positions,
// leaves quietly: the case has nothing left to measure and the mistake is
// already reported where it belongs.
func (d *driverRun) positionsAreInOrder(inst Instance, set *ferry.AddressSet) {
	d.rep.Helper()

	ctx, r, ok := d.openOver(inst.ctx(), inst, set, caseOrderingNo)
	if !ok {
		return
	}

	defer closeIf(r)

	e, lists := r.(ferry.Enumerator)
	if !lists {
		d.skip(caseOrderingNo, "the plane's reader does not enumerate, which ADR-0004 makes optional, so it "+
			"holds no sequence whose members could be out of order")

		return
	}

	// The fixture names one composite and nothing else, so the set is ranged
	// rather than searched: an address the compiler minted is the only kind of
	// address this package holds (ADR-0016), and taking it from the set is what
	// keeps the suite from asking for one that was never bound.
	for m := range set.Seq() {
		if at, is := m.(ferry.CompositeAddr); is {
			d.listAndCompare(ctx, e, at)
		}
	}
}

// listAndCompare enumerates one composite and hands the answer to the
// assertion, and a Children that fails is case 5's failure rather than this
// case's.
func (d *driverRun) listAndCompare(ctx context.Context, e ferry.Enumerator, at ferry.CompositeAddr) {
	d.rep.Helper()

	got, err := e.Children(ctx, at)
	if err != nil {
		return
	}

	d.compareToPositions(got)
}

// compareToPositions is the assertion itself: the segments as they arrived,
// against the same segments in position order.
func (d *driverRun) compareToPositions(got []ferry.Segment) {
	d.rep.Helper()

	want := make([]ferry.Segment, 0, len(dozenFixture().List))
	for i := range dozenFixture().List {
		want = append(want, ferry.IndexSegment(uint(i)))
	}

	sorted := slices.Clone(got)
	slices.SortFunc(sorted, compareSegments)

	if !slices.Equal(sorted, want) {
		return
	}

	if slices.Equal(got, want) {
		return
	}

	d.fail(caseOrderingNo, fmt.Sprintf("listing a sequence of %d at %s answered %v: an address is ordered "+
		"segment-wise, comparing a position numerically, and ordering the rendering instead sorts ten "+
		"and eleven between one and two, which core refuses at the load as a position it has no place "+
		"for", len(want), addrList, got))
}
