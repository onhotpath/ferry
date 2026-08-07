package ferrytest

import (
	"fmt"

	"github.com/onhotpath/ferry"
)

// caseOmission is case 21: an address a dump was silent at holds nothing
// afterwards (ADR-0006).
//
// Absent is a read-side kind. On a dump ferry holds the value and is the one
// making the plane, so there is no observation to report, and an address the
// value omits gets no write at all rather than a write of nothing. A sink that
// answers for the omission anyway - the recorded case is one that stored an
// explicit null - turns "ferry did not write here" into "the plane says this is
// null", which reads back as a value and is the conflation the whole model is
// built to avoid.
//
// Core is what guarantees no write carries an absent value, and core's own tests
// pin it. What a driver can still get wrong is the half this case asks: given no
// write at an address, the plane holds nothing there. So the fixture is dumped
// into a fresh plane and loaded back over a seed, and the seed has to survive,
// which is exactly what absence means to a Go field.
//
// It reads a leaf and nothing else, so a source that cannot list is asked
// nothing it cannot answer, and it is skipped, out loud, for a plane that could
// not take the dump at all, which is where a plane declaring no
// [ferry.KindString] lands.
func (d *driverRun) caseOmission() {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Sink == nil || inst.Source == nil {
		return
	}

	if err := ferry.Dump(inst.ctx(), omittedFixture(), inst.Sink, d.opts...); err != nil {
		d.skip(caseOmissionNo, "the fixture could not be written, so there is no dump to have been silent "+
			"at anything, and case 1 is where a dump that cannot happen is reported: "+err.Error())

		return
	}

	d.silenceLeftNothing(inst)
}

// silenceLeftNothing loads the fixture back over a seed and holds both halves to
// their answers: the address the dump wrote, and the one it did not.
//
// The written leaf is read as well as the silent one, because a plane that
// stored nothing at all would otherwise pass the case for the wrong reason.
func (d *driverRun) silenceLeftNothing(inst Instance) {
	d.rep.Helper()

	seed := omitted{Gone: omittedSeed}

	got, err := ferry.LoadOver(inst.ctx(), seed, inst.Source, d.opts...)
	if err != nil {
		d.fail(caseOmissionNo, fmt.Sprintf("loading the fixture back failed with %v: the dump wrote one "+
			"address and was silent at %s, and a plane that answers for the silence with a value of "+
			"its own hands that value to a field that never asked for one", err, addrGone))

		return
	}

	if got.Leaf != omittedFixture().Leaf {
		d.fail(caseOmissionNo, fmt.Sprintf("the address the dump did write holds %q, want %q: the case "+
			"reads both, because a plane that stored nothing at all would pass the half below without "+
			"having taken the dump", got.Leaf, omittedFixture().Leaf))

		return
	}

	if got.Gone == omittedSeed {
		return
	}

	d.fail(caseOmissionNo, fmt.Sprintf("after a dump that was silent at %s the load reads %q there, and the "+
		"seed said %q: an omitted address gets no write, so the plane has to be silent back, and a "+
		"stored null or a stored empty is an answer ferry never gave", addrGone, got.Gone, omittedSeed))
}
