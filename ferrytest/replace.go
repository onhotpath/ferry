package ferrytest

import (
	"fmt"
	"slices"

	"github.com/onhotpath/ferry"
)

// caseReplace is case 17: a sink declaring [ferry.Unsetter] leaves nothing of a
// longer dump behind when a shorter one follows it (ADR-0004).
//
// Case 15 re-dumps the same addresses with a different value and says so: it
// does not ask what a sink does about an address the new value no longer has.
// This is that question, and it is the one a plane can get wrong while every
// other case passes. Nothing in the contract obliges a dump to be a replacement
// until the driver declares that its plane can forget an address, and once it
// does, a load after two dumps must see the second value and only the second
// value.
//
// It runs for a sink asserting [ferry.Unsetter] and is skipped, out loud, for
// every sink that does not - which is every plane that replaces its contents
// wholesale and has nothing to forget, as well as every plane that genuinely
// cannot. A sink that has not made the claim cannot fail the case for it.
//
// The shape shrinks and the values do not, deliberately. What is asserted is
// that the second dump's list is the whole of the list, so a plane that kept the
// first dump's later positions reports a longer list rather than a wrong value,
// and the failure names what it found.
func (d *driverRun) caseReplace() {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Sink == nil || inst.Source == nil {
		return
	}

	if !d.declaresForget(inst) {
		d.skip(caseReplaceNo, "the plane's sink does not declare that it can forget an address, so a dump "+
			"adds to what a composite already held and there is nothing here to hold it to")

		return
	}

	if err := ferry.Dump(inst.ctx(), filledFixture(), inst.Sink, d.opts...); err != nil {
		d.skip(caseReplaceNo, "the first dump failed, so there is nothing for a shorter one to replace, and "+
			"case 1 is where a dump that cannot happen is reported: "+err.Error())

		return
	}

	if err := ferry.Dump(inst.ctx(), shrunkFixture(), inst.Sink, d.opts...); err != nil {
		d.fail(caseReplaceNo, "the second, shorter dump failed with "+err.Error()+": a value whose composite "+
			"lost a member is an ordinary value, and a sink that can forget an address has to be able "+
			"to write one")

		return
	}

	d.replacedLanded(inst)
}

// declaresForget opens the write half once and asks whether it declared the
// capability, which is the same question on the same value core's own dump asks:
// on the writer an open returned, and never on the [ferry.Sink].
func (d *driverRun) declaresForget(inst Instance) bool {
	d.rep.Helper()

	w, ok := d.openedWriter(inst)
	if !ok {
		return false
	}

	defer closeIf(w)

	_, declared := w.(ferry.Unsetter)

	return declared
}

// replacedLanded is the other half: what the plane holds afterwards is the
// second dump and nothing of the first.
//
// It reads the list back, which needs [ferry.Enumerator], because a position the
// second dump did not write is exactly what a load cannot see without one. A
// source that cannot list is asked nothing and the case says so.
func (d *driverRun) replacedLanded(inst Instance) {
	d.rep.Helper()

	got, err := ferry.Load[filled](inst.ctx(), inst.Source, d.opts...)
	if err != nil {
		d.skip(caseReplaceNo, "the fixture could not be read back, so what the second dump left cannot be "+
			"compared, and a source that cannot list is where that lands: "+err.Error())

		return
	}

	if slices.Equal(got.List, shrunkFixture().List) {
		return
	}

	d.fail(caseReplaceNo, fmt.Sprintf("after a dump of %v and then of %v the plane holds %v at %s: a sink "+
		"declaring it can forget an address makes a dump a replacement of a composite, and one that "+
		"leaves the earlier dump's later members behind reports success for a save that did not happen",
		filledFixture().List, shrunkFixture().List, got.List, addrList))
}
