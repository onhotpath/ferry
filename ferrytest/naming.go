package ferrytest

import (
	"maps"
	"strings"

	"github.com/onhotpath/ferry"
)

// caseNamed is case 22: a reader that names an address in its plane's own
// spelling names it the same way twice, and names it out of the address and its
// own configuration rather than out of what the plane turned out to hold
// (ADR-0011, ADR-0003).
//
// A name reaches a report in place of ferry's own rendering of the address, so
// it lands in whatever the caller logs. ferry's own message text carries no
// value the plane supplied, and a name computed from one would put it back in
// the line through the one channel core cannot inspect - which is why purity is
// a case rather than a sentence in the interface's doc comment.
//
// The two halves are asked of two planes: one freshly minted and empty, one
// carrying a fixture, both asked about the same addresses. Identical answers are
// the whole assertion, and the plane already builds both states, so the case
// costs one dump.
//
// It is skipped, out loud, for a reader that is no [ferry.PlaneNamer], which is
// every driver that leaves a report ferry's own rendering - a plane keyed by the
// address itself has nothing to add and correctly implements nothing.
func (d *driverRun) caseNamed() {
	d.rep.Helper()

	n, r, ok := d.namerOverEmpty()
	if !ok {
		return
	}

	defer closeIf(r)

	d.namesUnchanged(d.namesOf(n, namedAddrs()))
}

// namedAddrs is what this case asks the plane to name: the suite's own fixture
// addresses, written out rather than derived, the same way every other address
// in this package is.
//
// They are deliberately not confined to the set the read half was bound to.
// [ferry.PlaneNamer] takes a [ferry.Path] and core asks it about an address a
// value minted, which is a member of no bound set, so a name is a function of
// the address rather than of the binding. The first is the address the fixture
// below writes, which is the one a flattening driver answers out of its own
// table; the rest are answered by whatever it computes with.
func namedAddrs() []ferry.Path {
	return []ferry.Path{addrLeaf, addrList.Elem(0), addrMap.At(fixtureKey), ferry.At("under", "five")}
}

// namerOverEmpty opens the read half of a fresh, empty instance and asks whether
// it names anything, which is the same question core asks: on the reader an open
// returned, and never on the [ferry.Source].
//
// A plane that cannot be opened at all answers no here and is silent about it,
// because a broken open is cases 4, 6 and 10's and this case has nothing to add
// to what they already report.
func (d *driverRun) namerOverEmpty() (ferry.PlaneNamer, ferry.Reader, bool) {
	d.rep.Helper()

	r, ok := d.openedReader(d.plane.Open())
	if !ok {
		return nil, nil, false
	}

	n, named := r.(ferry.PlaneNamer)
	if !named {
		closeIf(r)
		d.skip(caseNamedNo, "the plane's reader has no name of its own for an address, so a report opens with "+
			"ferry's own rendering and there is no spelling here to hold it to")

		return nil, nil, false
	}

	return n, r, true
}

// namesOf is every address named twice, and the determinism half: a name that is
// not the same on the second call is not a name.
//
// An address the plane says it cannot name is not in the result, and that is the
// documented answer rather than a failure: what the case compares afterwards is
// the whole mapping, so an address named over one plane and refused over the
// other is caught by the comparison and not by this loop.
func (d *driverRun) namesOf(n ferry.PlaneNamer, addrs []ferry.Path) map[ferry.Path]string {
	d.rep.Helper()

	out := map[ferry.Path]string{}

	for _, at := range addrs {
		name, named := n.PlaneName(at)
		if !named {
			continue
		}

		if again, ok := n.PlaneName(at); !ok || again != name {
			d.fail(caseNamedNo, "the plane named "+at.String()+" "+name+" and then named it "+again+
				": a name is what a report opens with, and one that changes between two calls makes two "+
				"runs of one failure read as two different failures")

			continue
		}

		out[at] = name
	}

	return out
}

// namesUnchanged is the purity half: the same addresses, named over a plane
// carrying a fixture, answer exactly what they answered over an empty one.
//
// The fixture is leaf-only, so a sink that cannot forget a composite and a
// source that cannot list are both asked nothing they cannot answer, and the one
// address it writes is the one [namedAddrs] leads with.
func (d *driverRun) namesUnchanged(want map[ferry.Path]string) {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Sink == nil {
		d.skip(caseNamedNo, "the plane mints no sink, so there is no populated plane to name the same "+
			"addresses over and the determinism half is the whole of what ran")

		return
	}

	if err := ferry.Dump(inst.ctx(), onlyLeaf{Leaf: fixtureLeaf}, inst.Sink, d.opts...); err != nil {
		d.fail(caseNamedNo, "dumping the fixture this case names addresses over: "+err.Error())

		return
	}

	r, ok := d.openedReader(inst)
	if !ok {
		d.skip(caseNamedNo, "the plane that took the fixture could not be opened to read, which is cases 4, "+
			"6 and 10's failure rather than this one's")

		return
	}

	defer closeIf(r)

	d.sameNames(r, want)
}

// sameNames is the comparison itself, on the read half over the populated plane.
func (d *driverRun) sameNames(r ferry.Reader, want map[ferry.Path]string) {
	d.rep.Helper()

	n, named := r.(ferry.PlaneNamer)
	if !named {
		d.fail(caseNamedNo, "the reader over an empty plane names an address and the reader over a populated "+
			"one names nothing: what a report opens with would then depend on what the plane happened to hold")

		return
	}

	if got := d.namesOf(n, namedAddrs()); !maps.Equal(got, want) {
		d.fail(caseNamedNo, "the plane names these addresses differently once it holds something: "+
			nameDiff(want, got)+". A name is a function of the address and of the driver's own "+
			"configuration, and of nothing the plane holds, because it is printed in a report whose text "+
			"never repeats a value the plane supplied")
	}
}

// nameDiff is the addresses the two runs disagree about, one per address, so a
// failure names what moved rather than two whole tables.
func nameDiff(want, got map[ferry.Path]string) string {
	var b strings.Builder

	for at, first := range want {
		if second, still := got[at]; still && second == first {
			continue
		}

		writeDiff(&b, at, first, namedOr(got, at))
	}

	for at, second := range got {
		if _, before := want[at]; !before {
			writeDiff(&b, at, "no name at all", second)
		}
	}

	return b.String()
}

// writeDiff is one address the two runs disagree about.
func writeDiff(b *strings.Builder, at ferry.Path, first, second string) {
	b.WriteString(" " + at.String() + ": " + first + " over an empty plane, " + second + " over a populated one;")
}

// namedOr is what one run called an address, because "no name at all" and a name
// that happens to be empty text are two different answers.
func namedOr(names map[ferry.Path]string, at ferry.Path) string {
	if name, ok := names[at]; ok {
		return name
	}

	return "no name at all"
}
