package ferry_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/onhotpath/ferry/ferrytest"
)

// This file is where core's own supported type set meets the engine that has to
// carry it, and today they disagree by eighteen rows.
//
// ADR-0013 makes ferrytest.CoreTypes a published artefact rather than a
// fixture, so #72 writes it complete - nineteen rows and 57 cases - ahead of the
// compiler that admits their types. The compiler admits string leaves and
// structs of them, so the table is run through ferrytest.RoundTrip behind the
// list below, which names every type the engine cannot yet satisfy and the
// ticket that closes it.
//
// The list is the point. A table quietly trimmed to what the engine can do today
// is a table that never records what is missing; this way main stays green, the
// remaining work is countable, and it is in the repository rather than in
// somebody's head.

// engineCannotCarry names every type CoreTypes proves and the compiler does not
// yet admit, against the ticket that lands it.
//
// It only ever shrinks. Deleting an entry is what a widening ticket does; adding
// one is what TestTheSkipListOnlyShrinks refuses, and leaving one behind after
// the engine gained the type is what TestEverySkippedTypeStillFails refuses.
// The ticket that empties it deletes it, and this file with it.
var engineCannotCarry = map[reflect.Type]string{
	// #75, the dynamic composites. A slice's addresses come from the value, and
	// a composite with no elements writes Null at its own address.
	reflect.TypeFor[[]string](): "#75",
}

// everSkipped is the list as #72 wrote it, frozen.
//
// It is the half of "shrinks monotonically" that an entry going stale cannot
// catch: a type that was never in this census arriving in the skip list is a
// type somebody stopped supporting, and it fails here rather than being read as
// ordinary maintenance. The other half is TestEverySkippedTypeStillFails, which
// is what makes a type put back into the skip list after its ticket landed fail
// as well.
var everSkipped = []string{
	"bool", "int", "int8", "int16", "int32", "int64",
	"uint", "uint8", "uint16", "uint32", "uint64",
	"float32", "float64", "[]uint8", "[3]uint8",
	"time.Duration", "time.Time", "[]string",
}

// carried splits CoreTypes into the rows the engine claims to handle and the
// rows the skip list excuses.
func carried(rows []ferrytest.Proof) (yes, no []ferrytest.Proof) {
	for _, p := range rows {
		if _, skipped := engineCannotCarry[p.Type()]; skipped {
			no = append(no, p)

			continue
		}

		yes = append(yes, p)
	}

	return yes, no
}

// TestCoreTypesOverTheMemoryPlane runs every row the engine claims to carry.
//
// The memory plane is where core's value fidelity is stated, because it is the
// only plane that adds nothing of its own: it stores the boundary Value itself,
// so a failure here is core's and never a driver's.
func TestCoreTypesOverTheMemoryPlane(t *testing.T) {
	t.Parallel()

	yes, _ := carried(ferrytest.CoreTypes())
	if len(yes) == 0 {
		t.Fatal("the skip list excuses every row, so this test asserts nothing")
	}

	ferrytest.RoundTrip(t, ferrytest.MemPlane(), yes)
}

// TestEverySkippedTypeStillFails is the assertion that makes the skip list
// shrink rather than merely be edited.
//
// Every excused row is run anyway, against a report that is captured rather than
// failed on, and a row that passes is a row whose entry is stale: the engine
// gained the type and the entry was not deleted. So the ticket that widens the
// compiler cannot land green without deleting its entries, and a type put back
// into the list after its ticket landed fails here on the next run.
func TestEverySkippedTypeStillFails(t *testing.T) {
	t.Parallel()

	_, no := carried(ferrytest.CoreTypes())
	for _, p := range no {
		t.Run(p.Name(), func(t *testing.T) {
			t.Parallel()

			var rep capture

			ferrytest.RoundTrip(&rep, ferrytest.MemPlane(), []ferrytest.Proof{p})

			if len(rep.lines) == 0 {
				t.Errorf("%s round trips, so %s is stale and its entry belongs deleted",
					p.Type(), engineCannotCarry[p.Type()])
			}
		})
	}
}

// TestTheSkipListOnlyShrinks holds the list to the census #72 froze, and holds
// every entry to naming a row that exists.
func TestTheSkipListOnlyShrinks(t *testing.T) {
	t.Parallel()

	if len(engineCannotCarry) > len(everSkipped) {
		t.Errorf("the skip list holds %d entries, and it has never held more than %d",
			len(engineCannotCarry), len(everSkipped))
	}

	rows := map[reflect.Type]bool{}
	for _, p := range ferrytest.CoreTypes() {
		rows[p.Type()] = true
	}

	for typ, ticket := range engineCannotCarry {
		if !slices.Contains(everSkipped, typ.String()) {
			t.Errorf("%s is excused by %s and was never in the frozen census, so the list grew",
				typ, ticket)
		}

		if !rows[typ] {
			t.Errorf("%s is excused by %s and CoreTypes has no row for it", typ, ticket)
		}
	}
}

// capture is what ferrytest.T buys: a suite reports through two methods, so a
// caller who wants to assert that a case fails can take the report as data
// rather than as a test failure.
type capture struct{ lines []string }

func (c *capture) Errorf(format string, args ...any) {
	c.lines = append(c.lines, fmt.Sprintf(format, args...))
}

func (*capture) Helper() {}

var _ ferrytest.T = (*capture)(nil)

// TestEverySkippedTypeIsRefusedByCore keeps the list from being excused by the
// wrong failure.
//
// A row that failed because the plane was missing, or because the harness could
// not compile its own wrapper, would satisfy the test above without saying
// anything about the type set. What is required instead is core's own refusal,
// at an address, naming the type the row is for - which is what a schema compile
// declining a type looks like from the outside.
//
// The refusals are two rather than one and both are the type set's: an
// unsupported type is declined by the leaf rule, and time.Time is declined by
// the maps-no-address rule, because its kind is struct and none of its fields is
// exported. The second is ADR-0005's single highest-value line and it is what
// stops a timestamp being written as nothing at all. Both name the type, so that
// is what is asserted rather than either message, and the address is asserted
// only to be present: the two rules locate themselves differently today, the
// leaf rule at the plane address /value and the maps-no-address rule at the Go
// field path /Value.
func TestEverySkippedTypeIsRefusedByCore(t *testing.T) {
	t.Parallel()

	_, no := carried(ferrytest.CoreTypes())
	for _, p := range no {
		var rep capture

		ferrytest.RoundTrip(&rep, ferrytest.MemPlane(), []ferrytest.Proof{p})

		report := strings.Join(rep.lines, "\n")
		if !strings.Contains(report, "dump: ferry: /") || !strings.Contains(report, p.Type().String()) {
			t.Errorf("%s is excused, and what it reports is not core refusing that type at an address:\n%s",
				p.Type(), report)
		}
	}
}
