package ferrytest

import (
	"fmt"
	"slices"

	"github.com/onhotpath/ferry"
)

// caseSerialEquivalence is case 16: a reader that declares it tolerates
// overlapping calls produces, under every concurrency budget, the destination
// and the error report it produces serially (ADR-0019).
//
// It is the gate that makes fanout safe to enable at all, and it belongs here
// rather than in a driver's own tests because a driver declaring the capability
// is making a claim core relies on: core writes `go` only where the instance
// said it may, so the instance is what has to be held to what it said.
//
// It runs for a reader asserting [ferry.Concurrent] and is skipped, out loud,
// for every reader that does not. Absence of the capability is serial whatever
// the caller asked for, so for such a driver there is no second schedule and
// nothing here would be measuring one.
//
// Two halves, because the promise is about both. The destination half writes
// one plane and reads it back under several budgets. The report half loads a
// fixture of required addresses against a plane that holds none of them, which
// needs no sink and asks the harder question: an aggregate is many failures
// combined, and combining in completion order is the defect a concurrent
// scheduler introduces and a serial one cannot have.
//
// Every load mints its own destination, which is the fresh-destination rule
// ADR-0019 names as this property's trap: a destination shared between the two
// schedules makes a broken second walk pass.
func (d *driverRun) caseSerialEquivalence() {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Source == nil {
		return
	}

	if !d.declaresOverlap(inst) {
		d.skip(caseEquivalentNo, "the plane's reader does not declare that it tolerates overlapping calls, so "+
			"core walks it serially whatever the caller asked for and there is no second schedule to "+
			"compare against")

		return
	}

	d.equivalentValues(inst)
	d.equivalentReports()
}

// declaresOverlap opens the read half once and asks whether it declared the
// capability, which is the same question on the same value that core's own gate
// asks: on the instance an open returned, and never on the [ferry.Source].
//
// A plane that cannot be opened answers no here and is silent about it, because
// a broken open is cases 4, 6 and 10's and the skip above already says the case
// did not run.
func (d *driverRun) declaresOverlap(inst Instance) bool {
	d.rep.Helper()

	r, ok := d.openedReader(inst)
	if !ok {
		return false
	}

	defer closeIf(r)

	_, ok = r.(ferry.Concurrent)

	return ok
}

// equivalentValues is the destination half: one plane, written once, loaded
// once with no budget and once per budget, and every load has to produce the
// same value.
//
// The plane is written once and read many times deliberately. What is under
// test is the schedule and not the store, so every load has to be over contents
// that cannot have moved between them.
func (d *driverRun) equivalentValues(inst Instance) {
	d.rep.Helper()

	if inst.Sink == nil {
		return
	}

	if err := ferry.Dump(inst.ctx(), spreadFixture(), inst.Sink, d.opts...); err != nil {
		d.skip(caseEquivalentNo, "the fixture could not be dumped, so there is nothing to load under two "+
			"schedules: "+err.Error())

		return
	}

	want, err := ferry.Load[spread](inst.ctx(), inst.Source, d.opts...)
	if err != nil {
		d.skip(caseEquivalentNo, "the serial load failed, so there is no serial answer for a concurrent one "+
			"to be held to: "+err.Error())

		return
	}

	for _, n := range equivalenceBudgets {
		got, err := ferry.Load[spread](inst.ctx(), inst.Source, d.budgeted(n)...)
		d.sameValue(n, &got, &want, err)
	}
}

// sameValue holds one budget's load to what the serial one produced.
//
// The two are pointers because the fixture is wide enough to be worth not
// copying twice per budget, and never because either of them is written to.
func (d *driverRun) sameValue(n int, got, want *spread, err error) {
	d.rep.Helper()

	if err != nil {
		d.fail(caseEquivalentNo, fmt.Sprintf("a load under a budget of %d failed with %v, where the serial "+
			"load of the same plane succeeded: a reader that tolerates overlapping calls answers the same "+
			"however the walk was scheduled", n, err))

		return
	}

	if *got == *want {
		return
	}

	d.fail(caseEquivalentNo, fmt.Sprintf("a load under a budget of %d produced %+v, want the serial load's "+
		"%+v: a concurrent walk that reads a different plane is state shared between the calls the "+
		"capability said may overlap", n, got, want))
}

// equivalentReports is the report half: a plane holding none of a fixture's
// required addresses reports the same aggregate under every budget as it does
// serially, text for text.
//
// The text is compared rather than the count, and that is the point of the
// half. Core sorts a report at construction, and the scheduler combines the
// members in member order and never in completion order; a driver whose
// overlapping reads report the same failures in a different order is a caller's
// diff that changes between runs of one program.
func (d *driverRun) equivalentReports() {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Source == nil {
		return
	}

	_, err := ferry.Load[demanded](inst.ctx(), inst.Source, d.opts...)
	if err == nil {
		d.skip(caseEquivalentNo, "a load of four required addresses against a freshly minted plane succeeded, "+
			"so this plane holds them already and there is no report for two schedules to disagree about")

		return
	}

	want := report(err)

	for _, n := range equivalenceBudgets {
		_, err := ferry.Load[demanded](inst.ctx(), inst.Source, d.budgeted(n)...)
		d.sameReport(n, err, want)
	}
}

// report is the whole of what a failure says, element by element.
//
// It is the long form and not Error(), because the one-line form names the
// first few addresses and elides the rest, so two reports carrying different
// failures at the same addresses read alike - which is exactly the pair this
// case is looking for.
func report(err error) string { return fmt.Sprintf("%+v", err) }

// sameReport holds one budget's failure to the serial one's, as text.
func (d *driverRun) sameReport(n int, err error, want string) {
	d.rep.Helper()

	if err == nil {
		d.fail(caseEquivalentNo, fmt.Sprintf("a load under a budget of %d succeeded, where the serial load of "+
			"the same plane reported %q: a failure that disappears when the walk overlaps is a failure the "+
			"caller is not told about", n, want))

		return
	}

	if got := report(err); got != want {
		d.fail(caseEquivalentNo, fmt.Sprintf("a load under a budget of %d reported\n\t%s\nwant the serial "+
			"load's\n\t%s\na report that depends on the schedule is one a caller cannot diff between two "+
			"runs of one program", n, got, want))
	}
}

// budgeted is the caller's own Option list with one concurrency budget on the
// end.
//
// It is cloned rather than appended to, because two runs sharing the array
// behind one slice is the second budget overwriting the first, and the Option
// that refuses a budget given twice would never see it.
func (d *driverRun) budgeted(n int) []ferry.Option {
	return append(slices.Clone(d.opts), ferry.MaxConcurrency(n))
}

// equivalenceBudgets are the budgets case 16 compares against the serial walk.
//
// Three rather than one, and the smallest is 2 rather than 1. A budget of 1 is
// legal and means serial, so it would compare the serial walk with itself; the
// three below straddle the fixture's own width, so one of them fans out less
// than the container has members and one of them more.
var equivalenceBudgets = []int{2, 3, 8}
