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

// caseUnforgettable is case 19: a sink that cannot forget an address is refused
// a schema holding a composite, at the open and before any write (ADR-0004).
//
// It is case 17 scaled the other way round. Case 17 asks a sink that declared it
// can forget an address to prove that a dump replaces; this asks the sink that
// declared nothing what happens instead, and the answer is a refusal rather than
// a dump that quietly accumulates. So it runs for a sink that does not declare
// [ferry.Unsetter] and is skipped, out loud, for every sink that does.
//
// What it holds the plane to is that the refusal arrives at all. The rung is
// core's rather than the driver's, and a sink reaching it has one honest reading
// - this plane carries schemas of leaves and sections, and a slice or a map
// needs the capability - which is a sentence worth one case rather than the
// several the same refusal would otherwise be reported by.
func (d *driverRun) caseUnforgettable() {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Sink == nil {
		return
	}

	declared, opened := d.forgetDeclaration(inst)
	if !opened {
		d.skip(caseUnforgettableNo, "the plane's write half could not be opened, so what its writer declares "+
			"is not a question this case can ask, and cases 2 and 6 are where an open that fails is "+
			"reported")

		return
	}

	if declared {
		d.skip(caseUnforgettableNo, "the plane's sink declares that it can forget an address, so a dump of a "+
			"composite is a replacement and case 17 is where that is held to")

		return
	}

	err := ferry.Dump(inst.ctx(), justMap{Map: map[string]string{"k": "v"}}, inst.Sink, d.opts...)
	if err == nil {
		d.fail(caseUnforgettableNo, "a dump of a schema holding a mapping succeeded against a sink that does "+
			"not declare it can forget an address, so what an earlier dump left under "+addrMap.String()+
			" survives this one and loads back as a value nobody wrote")

		return
	}

	d.logf("plane %s: case %d: the plane's sink cannot forget an address, so every schema holding a slice or "+
		"a map is refused at the open: %v", d.plane.Name, caseUnforgettableNo, err)
}

// declaresForget opens the write half once and asks whether it declared the
// capability, which is the same question on the same value core's own dump asks:
// on the writer an open returned, and never on the [ferry.Sink].
func (d *driverRun) declaresForget(inst Instance) bool {
	d.rep.Helper()

	declared, _ := d.forgetDeclaration(inst)

	return declared
}

// forgetDeclaration is the same question with the open kept apart from the
// answer, because the two cases that ask it want different things of a write
// half that could not be opened: case 17 has nothing to hold such a plane to,
// and case 19 must not read a failed open as a plane that cannot forget.
func (d *driverRun) forgetDeclaration(inst Instance) (declared, opened bool) {
	d.rep.Helper()

	w, ok := d.openedWriter(inst)
	if !ok {
		return false, false
	}

	defer closeIf(w)

	_, declared = w.(ferry.Unsetter)

	return declared, true
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
