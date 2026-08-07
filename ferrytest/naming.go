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
// carrying the fixture, both named over the same address set. Identical answers
// are the whole assertion, and the plane already builds both states, so the case
// costs one dump.
//
// It is skipped, out loud, for a reader that is no [ferry.PlaneNamer], which is
// every driver that leaves a report ferry's own rendering - a plane keyed by the
// address itself has nothing to add and correctly implements nothing.
func (d *driverRun) caseNamed() {
	d.rep.Helper()

	set, ok := fixtureSet[spread](d, caseNamedNo)
	if !ok {
		return
	}

	n, r, ok := d.namerOverEmpty(set)
	if !ok {
		return
	}

	defer closeIf(r)

	d.namesUnchanged(set, d.namesOf(n, set))
}

// namerOverEmpty opens the read half of a fresh, empty instance and asks whether
// it names anything, which is the same question core asks: on the reader an open
// returned, and never on the [ferry.Source].
func (d *driverRun) namerOverEmpty(set *ferry.AddressSet) (ferry.PlaneNamer, ferry.Reader, bool) {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Source == nil {
		return nil, nil, false
	}

	_, r, ok := d.openOver(inst.ctx(), inst, set, caseNamedNo)
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

// namesOf is every address of the set named twice, and the determinism half: a
// name that is not the same on the second call is not a name.
//
// An address the plane says it cannot name is not in the result, and that is the
// documented answer rather than a failure: what the case compares afterwards is
// the whole mapping, so an address named on one plane and refused on the other
// is caught by the comparison and not by this loop.
func (d *driverRun) namesOf(n ferry.PlaneNamer, set *ferry.AddressSet) map[ferry.Path]string {
	d.rep.Helper()

	out := map[ferry.Path]string{}

	for m := range set.Seq() {
		at := m.Path()

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
// carrying the fixture, answer exactly what they answered over an empty one.
func (d *driverRun) namesUnchanged(set *ferry.AddressSet, want map[ferry.Path]string) {
	d.rep.Helper()

	_, r, ok := dumpAndOpen(d, spreadFixture(), set, caseNamedNo)
	if !ok {
		return
	}

	defer closeIf(r)

	n, named := r.(ferry.PlaneNamer)
	if !named {
		d.fail(caseNamedNo, "the reader over an empty plane names an address and the reader over a populated "+
			"one names nothing: what a report opens with would then depend on what the plane happened to hold")

		return
	}

	if got := d.namesOf(n, set); !maps.Equal(got, want) {
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
