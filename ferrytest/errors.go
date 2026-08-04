package ferrytest

import (
	"errors"
	"slices"
	"strings"

	"github.com/onhotpath/ferry"
)

// Want is one failure a call is expected to report: the address it happened at,
// and the class it belongs to.
//
//	ferrytest.Want{Address: ferry.At("db", "port"), Class: ferry.ErrValue}
//
// Class is matched with errors.Is, so a subordinate sentinel is a narrower
// expectation than the class above it: ferry.ErrReadOnly matches only a failure
// that declares it, where ferry.ErrPlane matches that one and every other plane
// failure beside it. A Want with no Class matches nothing and is reported as the
// mistake it is.
//
// The zero Address is a value rather than a wildcard. It is the address of a
// failure that has none, such as a plane that would not close, so a Want that
// leaves it out matches only such a failure.
type Want struct {
	Address ferry.Path
	Class   error
}

// DiffErrors reports how the failures a call produced differ from the failures
// it was expected to produce, as an exact set over address and class.
//
//	for _, s := range ferrytest.DiffErrors(err,
//	    ferrytest.Want{Address: ferry.At("db", "host"), Class: ferry.ErrMissing},
//	    ferrytest.Want{Address: ferry.At("db", "port"), Class: ferry.ErrValue},
//	) {
//	    t.Errorf("load: %s", s)
//	}
//
// An empty result means the call reported exactly those failures, no more and no
// fewer. Anything else is one line per expectation nothing matched and one line
// per failure nothing expected, each naming the address and the class, so a
// reader learns which failure went missing rather than that a count moved.
//
// Exact rather than "contains", because ferry's diagnostics suppress a failure
// that is a consequence of another, and a suppression rule fails by reporting
// once too often. An assertion that only checks the failures it named passes
// straight through the extra one.
//
// A Want and a failure pair when the addresses are equal and errors.Is answers
// for the class, and each pairs with at most one of the other, so two failures at
// one address need two Wants. Message text is asserted nowhere: a line quotes it
// only to say what arrived.
//
// It returns data rather than failing anything, so a caller who is asserting that
// a driver fails can read the answer instead of losing their run. [CheckErrors]
// is the same check reported to a test. Lines are ordered by address, so the
// report is the same string over repeated runs.
func DiffErrors(got error, want ...Want) []string {
	elems := ferry.Elements(got)
	pairedTo := pairElements(adjacency(want, elems), len(elems))

	lines := absent(want, pairedTo)
	lines = append(lines, unexpected(elems, pairedTo)...)

	return sortLines(lines)
}

// CheckErrors fails t with one line per difference between the failures a call
// produced and the failures it was expected to produce.
//
//	ferrytest.CheckErrors(t, err,
//	    ferrytest.Want{Address: ferry.At("db", "host"), Class: ferry.ErrMissing},
//	    ferrytest.Want{Address: ferry.At("db", "port"), Class: ferry.ErrValue},
//	)
//
// It is [DiffErrors] reported to a test and the semantics are that function's:
// an exact set over address and class, with no assertion on message text at any
// level. A call that reported exactly the wanted failures reports nothing here.
//
// t is [T] rather than *testing.T, which *testing.T satisfies for free, so the
// check runs from a probe as well as from a test.
func CheckErrors(t T, got error, want ...Want) {
	t.Helper()

	for _, s := range DiffErrors(got, want...) {
		t.Errorf("%s", s)
	}
}

// absent is one line per expectation that paired with no failure.
func absent(want []Want, pairedTo []int) []line {
	matched := make([]bool, len(want))

	for _, i := range pairedTo {
		if i >= 0 {
			matched[i] = true
		}
	}

	var out []line

	for i, w := range want {
		if !matched[i] {
			out = append(out, line{addr: w.Address, text: wantText(w)})
		}
	}

	return out
}

// unexpected is one line per failure that paired with no expectation.
func unexpected(elems []error, pairedTo []int) []line {
	var out []line

	for i, err := range elems {
		if pairedTo[i] < 0 {
			out = append(out, line{addr: addressOf(err), text: elementText(err)})
		}
	}

	return out
}

// adjacency is, per Want, every element it could pair with.
func adjacency(want []Want, elems []error) [][]int {
	adj := make([][]int, len(want))

	for i, w := range want {
		for j, err := range elems {
			if pairs(w, err) {
				adj[i] = append(adj[i], j)
			}
		}
	}

	return adj
}

// pairs reports whether one Want and one element are the same failure.
//
// A Want carrying no class pairs with nothing rather than with everything: the
// exact-set semantics ADR-0011 fixes are over (address, class) pairs, so a Want
// missing half of one is a mistake in the test, and [wantText] names it as that.
func pairs(w Want, err error) bool {
	return w.Class != nil && addressOf(err) == w.Address && errors.Is(err, w.Class)
}

// pairElements pairs Wants with elements one to one, and returns the Want each
// element took, or -1 for an element that took none.
//
// It is a maximum matching rather than a greedy pass because the classes are not
// disjoint: ADR-0011's subordinate sentinels mean one element can satisfy two
// Wants and one Want two elements, and ADR-0006 measured one address producing
// two errors, which is where that meets. A greedy pass over
// Want{ErrPlane} and Want{ErrReadOnly} against those two elements pairs them in
// one order and reports a difference that is not there in the other.
func pairElements(adj [][]int, n int) []int {
	pairedTo := make([]int, n)
	for i := range pairedTo {
		pairedTo[i] = -1
	}

	for u := range adj {
		augment(u, adj, make([]bool, n), pairedTo)
	}

	return pairedTo
}

// augment gives Want u one of the elements it can pair with, displacing the Want
// holding that element if that Want can move to another.
func augment(u int, adj [][]int, seen []bool, pairedTo []int) bool {
	for _, v := range adj[u] {
		if seen[v] {
			continue
		}

		seen[v] = true

		if free(pairedTo[v], adj, seen, pairedTo) {
			pairedTo[v] = u

			return true
		}
	}

	return false
}

// free reports whether the Want holding an element can give it up, which is
// either because no Want holds it or because that Want has somewhere to move.
func free(holder int, adj [][]int, seen []bool, pairedTo []int) bool {
	return holder < 0 || augment(holder, adj, seen, pairedTo)
}

// line is one difference, with the address kept apart from the text so the
// report can be ordered the way ferry orders addresses (ADR-0003) rather than by
// the spelling of the sentence.
type line struct {
	addr ferry.Path
	text string
}

// sortLines puts the report in address order, tie-broken on the text so that two
// lines about one address are ordered too, and returns nil for no difference.
func sortLines(lines []line) []string {
	slices.SortFunc(lines, compareLines)

	var out []string
	for _, l := range lines {
		out = append(out, l.text)
	}

	return out
}

func compareLines(a, b line) int {
	if c := a.addr.Compare(b.addr); c != 0 {
		return c
	}

	return strings.Compare(a.text, b.text)
}

// wantText is one expectation nothing matched.
func wantText(w Want) string {
	if w.Class == nil {
		return "want " + addressText(w.Address) + ": no class, so nothing can match it"
	}

	return "want " + addressText(w.Address) + ": " + w.Class.Error() + ", and nothing reported it"
}

// elementText is one failure nothing expected.
//
// It quotes the failure's own text after naming the address and the class,
// because a set difference that says only "one more than you wanted" is not
// something a reader can act on. That is a report and not an assertion: nothing
// here compares message text, which ADR-0011 keeps out of the API.
func elementText(err error) string {
	return "got " + addressText(addressOf(err)) + ": " + classText(err) + ", and nothing wanted it: " + err.Error()
}

// addressText names the address a line is about. A failure with no address gets
// a word rather than an empty column.
func addressText(p ferry.Path) string {
	if p == (ferry.Path{}) {
		return "(no address)"
	}

	return p.String()
}

// addressOf is where a failure happened, and the zero Path for an element ferry
// did not build, which has no address to read.
func addressOf(err error) ferry.Path {
	if e, ok := errors.AsType[*ferry.Error](err); ok {
		return e.Address()
	}

	return ferry.Path{}
}

// sentinels is ferry's vocabulary in the order a line names it: the four
// classes, then the two subordinate sentinels, then the provenance marker.
var sentinels = []error{
	ferry.ErrSchema, ferry.ErrMissing, ferry.ErrValue, ferry.ErrPlane,
	ferry.ErrReadOnly, ferry.ErrWrongKind, ferry.ErrDriver,
}

// classText names every sentinel an element answers to, because more than one
// can be true at once: a driver's refusal is a plane failure and a driver
// failure, and a read-only sink's is both of those and its own sentinel as well.
func classText(err error) string {
	var out []string

	for _, s := range sentinels {
		if errors.Is(err, s) {
			out = append(out, s.Error())
		}
	}

	if len(out) == 0 {
		return "no ferry class"
	}

	return strings.Join(out, ", ")
}
