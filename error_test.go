package ferry

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// valueErr is the element most of these cases are built from: a walk-moment
// value failure at an address.
func valueErr(loc Path, msg string) *Error { return newError(momentWalk, ErrValue, loc, msg) }

// sole is the one leaf a driver failure naming at most one address becomes.
// fromDriver reports one failure per address the driver named, so a case that
// wants the leaf asserts that there is exactly one of them first.
func sole(t *testing.T, err error) *Error {
	t.Helper()

	e, ok := errors.AsType[*Error](err)
	if !ok || len(Elements(err)) != 1 {
		t.Fatalf("want one ferry error, got %d elements: %v", len(Elements(err)), err)
	}

	return e
}

// distinct counts how many different strings a run produced, which is the shape
// every determinism assertion in ADR-0011 is measured in.
func distinct(got []string) []string {
	seen := make(map[string]struct{}, len(got))
	for _, s := range got {
		seen[s] = struct{}{}
	}

	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}

	slices.Sort(out)

	return out
}

// TestErrorHasNoExportedFields is the property that lets the struct grow: the
// name is exported so errors.AsType works, and no field is, so a caller cannot
// build a switch over ferry's internals.
func TestErrorHasNoExportedFields(t *testing.T) {
	typ := reflect.TypeFor[Error]()
	if typ.NumField() == 0 {
		t.Fatal("Error has no fields at all, so this test asserts nothing")
	}

	for i := range typ.NumField() {
		if f := typ.Field(i); f.IsExported() {
			t.Fatalf("Error.%s is exported", f.Name)
		}
	}
}

// TestAsTypeFindsErrorThroughWrapping covers both wrapping forms the ADR
// measured, and the two of them at once: an aggregate inside a fmt.Errorf.
func TestAsTypeFindsErrorThroughWrapping(t *testing.T) {
	leaf := valueErr(At("db", "port"), "is not a valid int")
	other := valueErr(At("tls", "cert"), "is not a valid string")

	for _, c := range []struct {
		name string
		err  error
	}{
		{"bare", leaf},
		{"fmt.Errorf", fmt.Errorf("loading config: %w", leaf)},
		{"errors.Join", errors.Join(errors.New("unrelated"), leaf)},
		{"an aggregate", join(other, leaf)},
		{"an aggregate inside fmt.Errorf", fmt.Errorf("loading config: %w", join(leaf, other))},
		{"an aggregate inside errors.Join", errors.Join(join(leaf, other))},
		{"twice wrapped", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", leaf))},
	} {
		t.Run(c.name, func(t *testing.T) {
			checkAsTypeFinds(t, c.err, At("db", "port"))
		})
	}
}

func checkAsTypeFinds(t *testing.T, err error, want Path) {
	t.Helper()

	got, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("AsType found no *Error in %v", err)
	}

	if got.Address() != want {
		t.Fatalf("AsType picked the error at %s, want %s", got.Address(), want)
	}
}

// TestErrorIsOnThePointerReceiver is survey item 5.14's fourth entry closed by
// construction: declaring Error() on the value where a pointer is returned
// makes the natural value-form errors.As a silent false instead of a compile
// error.
func TestErrorIsOnThePointerReceiver(t *testing.T) {
	iface := reflect.TypeFor[error]()

	if reflect.TypeFor[Error]().Implements(iface) {
		t.Fatal("the value form implements error, so a value-form errors.As would compile and never match")
	}

	if !reflect.TypeFor[*Error]().Implements(iface) {
		t.Fatal("the pointer form does not implement error")
	}
}

// named is a sentinel and the identifier it is exported as.
type named struct {
	name string
	err  error
}

// sentinels is the whole vocabulary, and the table is the boundary: adding a
// public name to the family is an edit here.
func sentinels() []named {
	return []named{
		{"ErrSchema", ErrSchema},
		{"ErrMissing", ErrMissing},
		{"ErrValue", ErrValue},
		{"ErrPlane", ErrPlane},
		{"ErrDriver", ErrDriver},
		{"ErrReadOnly", ErrReadOnly},
	}
}

// TestSentinelsAreMatchedOnlyByIs asserts the vocabulary exists, that its
// members are distinct, and that none of them is a concrete type a caller could
// switch on: there is no Kind enum and no KindOf, so errors.Is is the whole
// mechanism.
func TestSentinelsAreMatchedOnlyByIs(t *testing.T) {
	all := sentinels()

	for i := range all {
		t.Run(all[i].name, func(t *testing.T) {
			checkSentinel(t, all, i)
		})
	}
}

func checkSentinel(t *testing.T, all []named, i int) {
	t.Helper()

	s := all[i]
	if s.err == nil || s.err.Error() == "" {
		t.Fatalf("%s is %v", s.name, s.err)
	}

	if _, ok := errors.AsType[*Error](s.err); ok {
		t.Fatalf("%s is a *ferry.Error, so it is matchable by something other than errors.Is", s.name)
	}

	// A sentinel from errors.New has no exported type behind it, which is what
	// leaves errors.Is as the only way to ask.
	if pkg := reflect.TypeOf(s.err).Elem().PkgPath(); pkg != "errors" {
		t.Fatalf("%s is a %s from %q, want a plain errors.New sentinel", s.name, reflect.TypeOf(s.err), pkg)
	}

	checkDistinctFromTheRest(t, all, i)
}

// checkDistinctFromTheRest asserts that no member of the vocabulary answers for
// another, which is what makes each of the six a separate question to ask.
func checkDistinctFromTheRest(t *testing.T, all []named, i int) {
	t.Helper()

	for j, other := range all {
		if i != j && errors.Is(all[i].err, other.err) {
			t.Fatalf("%s matches %s", all[i].name, other.name)
		}
	}
}

// classSet is the four classes, which are the members of the vocabulary an
// error is one of. ErrDriver is the second axis and ErrReadOnly is subordinate,
// so neither is here.
func classSet() []error { return []error{ErrSchema, ErrMissing, ErrValue, ErrPlane} }

// TestClassesMatchThroughFerrysError covers the other half: a ferry error
// carrying a class answers for that class and for no other.
func TestClassesMatchThroughFerrysError(t *testing.T) {
	all := classSet()

	for i := range all {
		t.Run(all[i].Error(), func(t *testing.T) {
			checkClassMatches(t, all, i)
		})
	}
}

func checkClassMatches(t *testing.T, all []error, i int) {
	t.Helper()

	err := newError(momentWalk, all[i], At("a"), "refused")
	if !errors.Is(err, all[i]) {
		t.Fatalf("%v does not match its own class %v", err, all[i])
	}

	if errors.Is(err, ErrDriver) {
		t.Fatal("a core error carries the driver marker")
	}

	for j, other := range all {
		if i != j && errors.Is(err, other) {
			t.Fatalf("an error of class %v also matches %v", all[i], other)
		}
	}
}

// TestElements covers the reader's half of the error set.
func TestElements(t *testing.T) {
	if got := Elements(nil); got != nil {
		t.Fatalf("Elements(nil) = %v, want nil", got)
	}

	one := valueErr(At("db", "port"), "is not a valid int")
	two := valueErr(At("tls", "cert"), "is not a valid string")

	// A single failure returns the leaf bare, and Elements gives a one-element
	// slice for it, so the caller's loop reads the same either way.
	checkBareLeaf(t, join(one), one)

	for _, c := range []struct {
		name string
		err  error
		want int
	}{
		{"a bare leaf", one, 1},
		{"a leaf inside a wrapper", fmt.Errorf("loading: %w", one), 1},
		{"an error ferry did not build", errors.New("kv: down"), 1},
		{"an aggregate", join(one, two), 2},
		{"an aggregate inside a wrapper", fmt.Errorf("loading: %w", join(one, two)), 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := Elements(c.err); len(got) != c.want {
				t.Fatalf("Elements gave %d elements, want %d: %v", len(got), c.want, got)
			}
		})
	}

	// The slice is the caller's: mutating it cannot reach the aggregate.
	agg := join(one, two)
	Elements(agg)[0] = nil

	if got := Elements(agg); got[0] == nil {
		t.Fatal("mutating the returned slice reached the aggregate")
	}
}

// checkBareLeaf asserts that an aggregate constructor handed one failure gave
// that failure back rather than wrapping it in an aggregate of one.
func checkBareLeaf(t *testing.T, got error, want *Error) {
	t.Helper()

	if _, ok := errors.AsType[*errorList](got); ok {
		t.Fatalf("one failure produced an aggregate: %v", got)
	}

	if !errors.Is(got, want) {
		t.Fatalf("one failure produced %v, want the leaf itself", got)
	}
}

// TestElementsAreFerryErrorsWhereFerryBuiltThem is ADR-0011's answer to what the
// ok branch of the caller's counting loop is for.
//
// Every element of an aggregate ferry constructs is a *Error outright, because
// core is the only party that mints one and a driver's error enters as a cause.
// Elements is wider than that aggregate by design, so the branch is reachable,
// and it is reachable only for an error with no ferry error anywhere in its
// tree.
func TestElementsAreFerryErrorsWhereFerryBuiltThem(t *testing.T) {
	one := valueErr(At("db", "port"), "is not a valid int")
	two := valueErr(At("tls", "cert"), "is not a valid string")

	// No element of ferry's own aggregate needs unwrapping to reach the type.
	for _, e := range Elements(join(one, two)) {
		if reflect.TypeOf(e) != reflect.TypeFor[*Error]() {
			t.Fatalf("an element of ferry's own aggregate is %T, want *Error outright", e)
		}
	}

	for _, c := range []reachCase{
		{"an aggregate", join(one, two), true},
		{"one failure, bare", join(one), true},
		{"one failure inside a wrapper", fmt.Errorf("loading: %w", join(one)), true},
		{"an aggregate inside a wrapper", fmt.Errorf("loading: %w", join(one, two)), true},
		{"an error ferry never built", errors.New("kv: down"), false},
	} {
		t.Run(c.name, func(t *testing.T) { checkElementsReachAFerryError(t, c) })
	}
}

// reachCase is one shape a caller can hand Elements, and whether AsType reaches
// a *Error through every element of it.
type reachCase struct {
	name  string
	err   error
	reach bool
}

// checkElementsReachAFerryError asserts what the ok branch of the counting loop
// answers for every element of one error.
func checkElementsReachAFerryError(t *testing.T, c reachCase) {
	t.Helper()

	for _, e := range Elements(c.err) {
		if _, ok := errors.AsType[*Error](e); ok != c.reach {
			t.Fatalf("AsType found a *Error = %v in %v, want %v", ok, e, c.reach)
		}
	}
}

// TestAggregateIsFlat covers both halves of the promise: ferry never nests
// ferry aggregates, and it never rewrites a driver's tree.
func TestAggregateIsFlat(t *testing.T) {
	a := valueErr(At("a"), "is not a valid int")
	b := valueErr(At("b"), "is not a valid int")
	c := valueErr(At("c"), "is not a valid int")

	outer := join(join(a, b), c)
	if got := Elements(outer); len(got) != 3 {
		t.Fatalf("a nested aggregate gave %d elements, want 3: %v", len(got), got)
	}

	for _, e := range Elements(outer) {
		if _, ok := errors.AsType[*errorList](e); ok {
			t.Fatalf("an element is itself an aggregate: %v", e)
		}
	}
}

// TestDriverJoinEntersAsOneElement is the precise form of the promise: ferry
// cannot attribute addresses to a third party's children, so a driver's own
// joined error is one element with its internal shape intact.
func TestDriverJoinEntersAsOneElement(t *testing.T) {
	first, second := errors.New("kv: /a refused"), errors.New("kv: /b refused")
	drv := fromDriver(momentBind, At("db"), errors.Join(first, second))
	agg := join(drv, valueErr(At("z"), "is not a valid int"))

	if got := Elements(agg); len(got) != 2 {
		t.Fatalf("the driver's join was flattened into %d elements, want 2: %v", len(got), got)
	}

	for _, want := range []error{first, second} {
		if !errors.Is(agg, want) {
			t.Fatalf("the driver's own %v is no longer reachable", want)
		}
	}

	if !errors.Is(agg, ErrDriver) || !errors.Is(agg, ErrPlane) {
		t.Fatalf("the wrapped driver failure lost its provenance or its class: %v", agg)
	}
}

// permutations is the six collection orders three errors can arrive in, which
// is what a walk with no ordering guarantee is entitled to produce.
var permutations = [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}

// TestSortingHappensAtConstruction is the probe that pays for itself: sorting
// only in Format leaves the printed form stable and errors.AsType
// nondeterministic, so 5.5 would look fixed and would not be.
func TestSortingHappensAtConstruction(t *testing.T) {
	const rounds = 50

	base := []error{
		valueErr(At("workers").Elem(7), "the plane has index 7 and [3]string holds 3"),
		valueErr(At("db", "port"), "is not a valid int"),
		newError(momentWalk, ErrMissing, At("tls", "cert"), "required, and the plane supplied nothing"),
	}

	picks := make([]string, 0, rounds*len(permutations))
	renders := make([]string, 0, rounds*len(permutations))

	for range rounds {
		for _, p := range permutations {
			pick, render := runOnce(t, base, p)
			picks = append(picks, pick)
			renders = append(renders, render)
		}
	}

	if got := distinct(picks); len(got) != 1 || got[0] != "/db/port" {
		t.Fatalf("errors.AsType picked %v over %d runs, want /db/port only", got, len(picks))
	}

	if got := distinct(renders); len(got) != 1 {
		t.Fatalf("%d distinct renderings over %d runs:\n%s", len(got), len(renders), strings.Join(got, "\n---\n"))
	}
}

// runOnce aggregates the same three failures in one collection order, and
// reports what a programmatic reader picks out of the result and what a log
// would show.
func runOnce(t *testing.T, base []error, order []int) (pick, render string) {
	t.Helper()

	agg := join(base[order[0]], base[order[1]], base[order[2]])

	got, ok := errors.AsType[*Error](agg)
	if !ok {
		t.Fatalf("AsType found no *Error in %v", agg)
	}

	return got.Address().String(), fmt.Sprintf("%+v", agg)
}

// TestSortKeyIsMomentThenLocationThenMessage renders ADR-0011's own worked
// example, from a deliberately reversed collection order: an open failure
// precedes the walk errors it caused and a close failure follows them.
func TestSortKeyIsMomentThenLocationThenMessage(t *testing.T) {
	open := fromDriver(momentOpen, Path{}, errors.New("kv: dial tcp: connection refused"))
	shut := fromDriver(momentClose, Path{}, errors.New("kv: flush failed"))
	host := valueErr(At("db", "host"), "value did not parse as int")
	port := valueErr(At("db", "port"), "value did not parse as int")

	const want = "ferry: 4 errors:" +
		"\n  opening the plane: kv: dial tcp: connection refused" +
		"\n  /db/host: value did not parse as int" +
		"\n  /db/port: value did not parse as int" +
		"\n  closing the plane: kv: flush failed"

	if got := fmt.Sprintf("%+v", join(shut, port, host, open)); got != want {
		t.Fatalf("the report is\n%s\nwant\n%s", got, want)
	}
}

// TestSortKeyTiebreaks covers the two terms the worked example does not
// exercise on their own: a location-less element sorts first inside its own
// moment, and two errors at one address are ordered by message.
func TestSortKeyTiebreaks(t *testing.T) {
	noLoc := newError(momentWalk, ErrSchema, Path{}, "the walk had nowhere to put it")
	second := valueErr(At("a"), "b is not a valid int")
	first := valueErr(At("a"), "a is not a valid int")
	foreign := context.Canceled

	const want = "ferry: 4 errors:" +
		"\n  the walk had nowhere to put it" +
		"\n  /a: a is not a valid int" +
		"\n  /a: b is not a valid int" +
		"\n  context canceled"

	if got := fmt.Sprintf("%+v", join(foreign, second, first, noLoc)); got != want {
		t.Fatalf("the report is\n%s\nwant\n%s", got, want)
	}

	// The summary names what it can, and an element with no address names its
	// moment rather than nothing.
	const line = "ferry: 4 errors: (walk), /a, /a, and 1 more"

	if got := join(foreign, second, first, noLoc).Error(); got != line {
		t.Fatalf("the one-line form is %q, want %q", got, line)
	}

	// An element ferry did not build sorts last and names itself as unknown,
	// which is what keeps the order total whatever a driver hands back.
	const short = "ferry: 2 errors: /a, (unknown)"

	if got := join(foreign, first).Error(); got != short {
		t.Fatalf("the one-line form is %q, want %q", got, short)
	}
}

// TestFortyErrorsAreOneLine is the cap the one-line form exists for: Error() is
// what lands inside somebody else's sentence, and a forty-line string inside a
// sentence is unusable.
func TestFortyErrorsAreOneLine(t *testing.T) {
	const count = 40

	errs := make([]error, 0, count)
	for i := range count {
		errs = append(errs, valueErr(At("svc", fmt.Sprintf("f%02d", i)), "is not a valid int"))
	}

	agg := join(errs...)

	const want = "ferry: 40 errors: /svc/f00, /svc/f01, /svc/f02, and 37 more"

	got := fmt.Sprintf("%v", agg)
	if got != want {
		t.Fatalf("%%v is %q, want %q", got, want)
	}

	if strings.Contains(got, "\n") {
		t.Fatalf("%%v is not one line: %q", got)
	}

	if wrapped := fmt.Errorf("loading config: %w", agg).Error(); wrapped != "loading config: "+want {
		t.Fatalf("inside a sentence it reads %q", wrapped)
	}

	// The elision is presentation and not data: nothing is dropped.
	if n := len(Elements(agg)); n != count {
		t.Fatalf("Elements gave %d, want %d", n, count)
	}

	if n := strings.Count(fmt.Sprintf("%+v", agg), "\n"); n != count {
		t.Fatalf("the report has %d element lines, want %d", n, count)
	}
}

// TestSummaryDoesNotElideAtTheThreshold pins the boundary the count is stated
// past.
func TestSummaryDoesNotElideAtTheThreshold(t *testing.T) {
	agg := join(
		valueErr(At("db", "port"), "is not a valid int"),
		newError(momentWalk, ErrMissing, At("tls", "cert"), "required, and the plane supplied nothing"),
		valueErr(At("workers").Elem(7), "the plane has index 7 and [3]string holds 3"),
	)

	const want = "ferry: 3 errors: /db/port, /tls/cert, /workers#7"

	if got := agg.Error(); got != want {
		t.Fatalf("the one-line form is %q, want %q", got, want)
	}
}

// TestReportSuppressesThePerLinePrefix asserts the promotion: a leaf's own
// Error() carries "ferry: ", and inside a report the header already said it.
func TestReportSuppressesThePerLinePrefix(t *testing.T) {
	leaf := valueErr(At("db", "port"), "is not a valid int")

	if got := leaf.Error(); !strings.HasPrefix(got, "ferry: ") {
		t.Fatalf("a leaf on its own renders as %q, want the prefix", got)
	}

	report := fmt.Sprintf("%+v", join(leaf, valueErr(At("tls", "cert"), "is not a valid string")))
	if n := strings.Count(report, "ferry: "); n != 1 {
		t.Fatalf("the prefix appears %d times in\n%s", n, report)
	}
}

// TestErrorAtAttachesAndNeverClassifies is survey item 5.14's first entry
// measured rather than argued: ErrorAt is a second thing that can put an
// address on an error, and it is inert until core wraps it.
func TestErrorAtAttachesAndNeverClassifies(t *testing.T) {
	inner := errors.New("kv: a key may not contain a space")
	carrier := ErrorAt(At("db", "host name"), inner)

	if _, ok := errors.AsType[*Error](carrier); ok {
		t.Fatal("ErrorAt alone produced a *ferry.Error")
	}

	for _, s := range sentinels() {
		if errors.Is(carrier, s.err) {
			t.Fatalf("ErrorAt alone matches %s", s.name)
		}
	}

	if !errors.Is(carrier, inner) {
		t.Fatal("the driver's own error is not reachable through the carrier")
	}

	// On its own it renders as what it is, an address and a cause, with no
	// ferry prefix and no class word: it is a driver's value until core takes
	// it.
	const want = "/db/host name: kv: a key may not contain a space"

	if got := carrier.Error(); got != want {
		t.Fatalf("the carrier renders as %q, want %q", got, want)
	}

	if ErrorAt(At("a"), nil) != nil {
		t.Fatal("ErrorAt returned a non-nil error for a nil cause")
	}
}

// TestCoreTakesTheAddressFromErrorAt covers what core does with the carrier:
// it takes the address only where it has none, and unwraps the carrier away so
// the address prints once.
func TestCoreTakesTheAddressFromErrorAt(t *testing.T) {
	inner := errors.New("kv: a key may not contain a space")

	bound := sole(t, fromDriver(momentBind, Path{}, ErrorAt(At("db", "host"), inner)))
	if bound.Address() != At("db", "host") {
		t.Fatalf("core did not take the driver's address, it has %s", bound.Address())
	}

	if n := strings.Count(bound.Error(), "/db/host"); n != 1 {
		t.Fatalf("the address appears %d times in %q", n, bound.Error())
	}

	if !errors.Is(bound, ErrPlane) || !errors.Is(bound, ErrDriver) || !errors.Is(bound, inner) {
		t.Fatalf("the wrapped carrier lost a class, its provenance or its cause: %v", bound)
	}

	// Where core already knows the address, core's wins, so a driver cannot
	// misattribute a read at one address to another.
	got := sole(t, fromDriver(momentWalk, At("db", "port"), ErrorAt(At("somewhere", "else"), inner)))
	if got.Address() != At("db", "port") {
		t.Fatalf("the driver overrode core's address with %s", got.Address())
	}

	if strings.Contains(got.Error(), "somewhere") {
		t.Fatalf("the driver's address reached the message: %q", got.Error())
	}
}

// errDenied is a driver's own sentinel, and refusal is its own concrete type.
// Neither is anything ferry knows about, which is what makes them the assertion
// that a member's whole chain survived and not only the first member's.
//
// The text names no address, so a report that prints the address twice is
// visible as one.
var errDenied = errors.New("kv: denied to this token")

type refusal struct{ addr, why string }

func (r *refusal) Error() string { return "kv: " + r.why }

func (*refusal) Unwrap() error { return errDenied }

// locatedPair is the shape #211 reports: one refusal naming two addresses,
// which is what a driver refusing over a whole address set produces when it
// dislikes more than one member of it.
func locatedPair() error {
	return errors.Join(
		ErrorAt(At("q"), &refusal{addr: "/q", why: "the first failure"}),
		ErrorAt(At("r"), &refusal{addr: "/r", why: "the second failure"}),
	)
}

// TestEveryAddressADriverNamesIsReported is ADR-0011's aggregation rule at the
// one place ErrorAt is for: every located failure a driver reported survives,
// each keeping its own address, its own cause and its own declared class.
//
// The two moments are the ones #211 reproduces, and the last three subtests are
// the cases that must not move: one carrier, no carrier in a join, and no
// carrier at all.
func TestEveryAddressADriverNamesIsReported(t *testing.T) {
	t.Parallel()

	t.Run("at close", locatedPairAtClose)
	t.Run("at bind", locatedPairAtBind)
	t.Run("each member declares its own class", eachMemberDeclaresItsOwnClass)
	t.Run("one carrier is unchanged", oneCarrierIsUnchanged)
	t.Run("a join with no carrier is unchanged", aJoinWithNoCarrierIsUnchanged)
	t.Run("a plain error is unchanged", aPlainDriverErrorIsUnchanged)
}

func locatedPairAtClose(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	p.closeErr = locatedPair()

	_, err := Load[walkDB](t.Context(), planeSource{p: p})

	mustReportBothAddresses(t, err)
}

func locatedPairAtBind(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	p.bindErr = locatedPair()

	_, err := Load[walkDB](t.Context(), planeSource{p: p})

	mustReportBothAddresses(t, err)
}

// mustReportBothAddresses is the whole of what a caller gets: two elements, one
// per address, each carrying the driver's own error for that address and none
// of them carrying the other's.
func mustReportBothAddresses(t *testing.T, err error) {
	t.Helper()

	els := Elements(err)
	if len(els) != 2 {
		t.Fatalf("a driver naming two addresses reported %d elements, want 2:\n%+v", len(els), err)
	}

	for i, want := range []string{"/q", "/r"} {
		mustBeLocatedRefusal(t, els[i], want)
	}

	if !errors.Is(err, errDenied) {
		t.Errorf("the driver's own sentinel is not reachable through ferry:\n%+v", err)
	}
}

func mustBeLocatedRefusal(t *testing.T, el error, want string) {
	t.Helper()

	e, ok := errors.AsType[*Error](el)
	if !ok || e.Address().String() != want {
		t.Fatalf("the element is %v, want a ferry error at %s", el, want)
	}

	if !errors.Is(e, ErrPlane) || !errors.Is(e, ErrDriver) {
		t.Errorf("the failure at %s lost its class or its provenance:\n%+v", want, e)
	}

	own, ok := errors.AsType[*refusal](e)
	if !ok || own.addr != want {
		t.Errorf("the driver's own error at %s is not reachable through ferry: %v", want, el)
	}
}

// eachMemberDeclaresItsOwnClass is the extension rule read per failure: a
// driver holds an opinion about the class, and a join carries one opinion per
// member rather than the first member's for all of them.
func eachMemberDeclaresItsOwnClass(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	p.closeErr = errors.Join(
		ErrorAt(At("q"), fmt.Errorf("%w: the document does not parse", ErrValue)),
		ErrorAt(At("r"), &refusal{addr: "/r", why: "the second failure"}),
	)

	_, err := Load[walkDB](t.Context(), planeSource{p: p})

	els := Elements(err)
	if len(els) != 2 {
		t.Fatalf("a driver naming two addresses reported %d elements, want 2:\n%+v", len(els), err)
	}

	if !errors.Is(els[0], ErrValue) {
		t.Errorf("the first member's declared class was dropped:\n%+v", els[0])
	}

	if !errors.Is(els[1], ErrPlane) || errors.Is(els[1], ErrValue) {
		t.Errorf("the second member took the first member's class:\n%+v", els[1])
	}
}

// oneCarrierIsUnchanged is the common case, and it is asserted rather than
// assumed because it is the one the split must not move: core takes the address
// and unwraps the carrier away, so the address prints once.
func oneCarrierIsUnchanged(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	p.closeErr = ErrorAt(At("q"), &refusal{addr: "/q", why: "the only failure"})

	_, err := Load[walkDB](t.Context(), planeSource{p: p})

	if n := len(Elements(err)); n != 1 {
		t.Fatalf("one located failure gave %d elements, want 1:\n%+v", n, err)
	}

	e, ok := errors.AsType[*Error](err)
	if !ok || e.Address() != At("q") {
		t.Fatalf("core did not take the driver's address from %v", err)
	}

	if n := strings.Count(e.Error(), "/q"); n != 1 {
		t.Errorf("the address appears %d times in %q", n, e.Error())
	}
}

// aJoinWithNoCarrierIsUnchanged is the control #211 reports beside the defect:
// ferry cannot attribute addresses to a third party's children, so a join with
// no address in it stays whole and enters as one element.
func aJoinWithNoCarrierIsUnchanged(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	p.closeErr = errors.Join(errors.New("kv: flush failed"), errors.New("kv: the socket is gone"))

	_, err := Load[walkDB](t.Context(), planeSource{p: p})

	if n := len(Elements(err)); n != 1 {
		t.Fatalf("a join with no address in it gave %d elements, want 1:\n%+v", n, err)
	}

	report := fmt.Sprintf("%+v", err)
	for _, want := range []string{"flush failed", "the socket is gone"} {
		if !strings.Contains(report, want) {
			t.Errorf("the report lost %q:\n%s", want, report)
		}
	}
}

func aPlainDriverErrorIsUnchanged(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	p.closeErr = &refusal{addr: "/q", why: "the only failure"}

	_, err := Load[walkDB](t.Context(), planeSource{p: p})

	els := Elements(err)
	if len(els) != 1 {
		t.Fatalf("a plain driver error gave %d elements, want 1:\n%+v", len(els), err)
	}

	e, ok := errors.AsType[*Error](els[0])
	if !ok || e.Address() != (Path{}) {
		t.Fatalf("a driver error naming no address reported %v", els[0])
	}

	if !errors.Is(err, errDenied) || !errors.Is(err, ErrDriver) {
		t.Errorf("the plain driver error lost its chain or its provenance:\n%+v", err)
	}
}

// TestAggregateNeverHoldsNil covers the rule the errors package documents as
// invalid to break.
func TestAggregateNeverHoldsNil(t *testing.T) {
	if got := join(nil, nil); got != nil {
		t.Fatalf("join of nothing but nils returned %v", got)
	}

	if got := join(); got != nil {
		t.Fatalf("join of nothing returned %v", got)
	}

	one := valueErr(At("a"), "is not a valid int")
	checkBareLeaf(t, join(nil, one, nil), one)

	agg := join(nil, one, nil, valueErr(At("b"), "is not a valid int"), nil)

	elems := Elements(agg)
	if len(elems) != 2 {
		t.Fatalf("the aggregate holds %d elements, want 2: %v", len(elems), elems)
	}

	for i, e := range elems {
		if e == nil {
			t.Fatalf("element %d is nil", i)
		}
	}
}

// planeSecret and planeNumber stand in for what a Vault or Consul plane holds,
// where every value is a secret by default. They are what ferry is handed, and
// they must reach no rendering ferry composes.
const (
	planeSecret = "AKIAIOSFODNN7EXAMPLE"
	planeNumber = "99999999999999999999"
)

// authored is one construction where ferry composes the message, with the
// plane's value planted in whatever ferry was handed.
type authored struct {
	name      string
	err       error
	wantCause error
}

// ferryAuthored is the table the redaction rule is asserted over. Add a row
// here whenever a ticket authors a message: the assertion is that the plane's
// own text reaches no rendering, and it only covers what this table names.
//
// Every row is a message ferry composes over a value the plane supplied, and
// the value is planted in whatever ferry was handed - the stdlib error that
// quotes its input, or the Value it read the wrong kind off.
func ferryAuthored() []authored {
	_, syntax := strconv.ParseInt(planeSecret, 10, 64)
	_, over := strconv.ParseInt(planeNumber, 10, 8)
	_, notBool := strconv.ParseBool(planeSecret)
	_, notFloat := strconv.ParseFloat(planeSecret, 64)
	_, notDur := time.ParseDuration(planeSecret)
	_, notTime := time.Parse(time.RFC3339, planeSecret)
	_, wrongKind := String(planeSecret).AsInt()
	_, ranged := Number(planeNumber).AsInt()

	rows := []struct {
		msg   string
		class error
		cause error
	}{
		{"is not a valid int", ErrValue, syntax},
		{"is out of range for int8", ErrValue, over},
		{"is not a valid bool", ErrValue, notBool},
		{"is not a valid float64", ErrValue, notFloat},
		{"is not a valid time.Duration: a duration needs a unit, as in 30s or 1h30m", ErrValue, notDur},
		{"is not a valid time.Time: a time is RFC 3339, as in 2026-08-02T12:00:00Z", ErrValue, notTime},
		{"the plane holds string and int64 cannot take one", ErrValue, wrongKind},
		{"is out of range for int64", ErrValue, ranged},
		{"required, and the plane supplied nothing", ErrMissing, nil},
	}

	out := make([]authored, 0, len(rows)+1)
	for _, r := range rows {
		out = append(out, authored{
			name:      r.msg,
			err:       newError(momentWalk, r.class, At("db", "secret"), r.msg).withCause(r.cause),
			wantCause: r.cause,
		})
	}

	return append(out, authored{name: "an aggregate of every row above", err: join(collect(out)...)})
}

func collect(rows []authored) []error {
	out := make([]error, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.err)
	}

	return out
}

// TestFerryComposesNoPlaneText is the one that protects the secret-leak rule.
// ADR-0011 measured four leaks in five naive messages, on values a Vault or
// Consul plane makes secret by default, so the rule is total.
func TestFerryComposesNoPlaneText(t *testing.T) {
	for _, row := range ferryAuthored() {
		t.Run(row.name, func(t *testing.T) {
			checkNoPlaneText(t, row)
		})
	}
}

func checkNoPlaneText(t *testing.T, row authored) {
	t.Helper()

	for _, format := range []string{"%v", "%+v", "%s", "%q"} {
		checkRenderingIsClean(t, format, fmt.Sprintf(format, row.err))
	}

	checkRenderingIsClean(t, "Error()", row.err.Error())

	// Redaction is not a loss: the cause stays in the chain, so a caller who
	// wants the precise answer still has it. Only the log loses the value.
	if row.wantCause != nil && !errors.Is(row.err, row.wantCause) {
		t.Fatalf("the cause %v is no longer reachable through %v", row.wantCause, row.err)
	}
}

func checkRenderingIsClean(t *testing.T, form, got string) {
	t.Helper()

	for _, planted := range []string{planeSecret, planeNumber} {
		if strings.Contains(got, planted) {
			t.Fatalf("%s repeats the plane's own text: %s", form, got)
		}
	}
}

// TestTheTwoCarveOuts states the rule's boundary rather than claiming a
// stronger property than it has. Both are plane-supplied text that ferry does
// print, and both are recorded in ADR-0011.
func TestTheTwoCarveOuts(t *testing.T) {
	// A driver's own error is printed, and the obligation not to put plane
	// values in it is the driver's. It is a conformance case.
	drv := fromDriver(momentOpen, Path{}, fmt.Errorf("yaml: cannot parse %q", planeSecret))
	if !strings.Contains(drv.Error(), planeSecret) {
		t.Fatalf("the driver's own text was swallowed: %s", drv.Error())
	}

	// A dynamic address segment comes from the plane too, and ferry cannot name
	// the address without it. A map key is a name, not a value.
	dyn := valueErr(At("creds", planeSecret), "is not a valid int")
	if !strings.Contains(dyn.Error(), planeSecret) {
		t.Fatalf("the address lost its own segment: %s", dyn.Error())
	}
}

// TestErrWrongKindComposesWithItsClass covers the decision #66's sentinel is
// kept under: it is subordinate to ErrValue rather than a seventh class, so an
// accessor's refusal reaching a caller through core answers to both.
func TestErrWrongKindComposesWithItsClass(t *testing.T) {
	_, cause := String(planeSecret).AsInt()

	core := valueErr(At("db", "port"), "the plane holds string and int64 cannot take one").withCause(cause)
	if !errors.Is(core, ErrWrongKind) || !errors.Is(core, ErrValue) {
		t.Fatalf("a wrong-kind refusal through core matches wrong-kind=%t value=%t",
			errors.Is(core, ErrWrongKind), errors.Is(core, ErrValue))
	}

	// Through a driver, where core's default for the moment would have been
	// ErrPlane, the finer sentinel is what decides the class.
	drv := fromDriver(momentWalk, At("db", "port"), cause)
	if !errors.Is(drv, ErrValue) || errors.Is(drv, ErrPlane) {
		t.Fatalf("a driver's wrong-kind refusal classified as value=%t plane=%t",
			errors.Is(drv, ErrValue), errors.Is(drv, ErrPlane))
	}

	if !errors.Is(drv, ErrDriver) || !errors.Is(drv, ErrWrongKind) {
		t.Fatalf("the driver marker or the sentinel was lost: %v", drv)
	}
}

// TestErrReadOnlyComposesWithErrPlane is the same subordination on ADR-0004's
// sentinel, which is what makes it a member of this family rather than an
// exception beside it.
func TestErrReadOnlyComposesWithErrPlane(t *testing.T) {
	drv := fromDriver(momentOpen, Path{}, fmt.Errorf("registry: opened without KEY_SET_VALUE: %w", ErrReadOnly))

	for _, want := range []error{ErrReadOnly, ErrPlane, ErrDriver} {
		if !errors.Is(drv, want) {
			t.Fatalf("a read-only refusal does not match %v: %v", want, drv)
		}
	}
}

// TestDriverDeclaresTheClass is the extension point: a driver holds an opinion
// about the class and about nothing else, and core supplies the default for the
// moment where it holds none.
func TestDriverDeclaresTheClass(t *testing.T) {
	plain := fromDriver(momentOpen, Path{}, errors.New("open /etc/app.yaml: no such file or directory"))
	if !errors.Is(plain, ErrPlane) || errors.Is(plain, ErrValue) {
		t.Fatalf("core's default class at open is not ErrPlane: %v", plain)
	}

	// ADR-0001 names the YAML provider discarding parse errors as the failure
	// it rules out by architecture; this is where that error lands with an
	// honest class, because only the driver knows the file is the operator's.
	declared := fromDriver(momentOpen, Path{}, fmt.Errorf("yaml: line 3: %w", ErrValue))
	if !errors.Is(declared, ErrValue) || errors.Is(declared, ErrPlane) {
		t.Fatalf("the driver's declared class was overridden: %v", declared)
	}

	for _, e := range []error{plain, declared} {
		if !errors.Is(e, ErrDriver) {
			t.Fatalf("provenance is core's and cannot be given up: %v", e)
		}
	}
}

// TestConcurrentFormattingIsOneRendering is what makes ADR-0010's memoised
// compile error safe to share: nothing is computed on first print, so there is
// no lazy state to race on. CI runs this under -race.
func TestConcurrentFormattingIsOneRendering(t *testing.T) {
	const goroutines = 64

	agg := join(
		valueErr(At("db", "port"), "is not a valid int"),
		newError(momentWalk, ErrMissing, At("tls", "cert"), "required, and the plane supplied nothing"),
		fromDriver(momentClose, Path{}, errors.New("kv: flush failed")),
	)

	got := make([]string, goroutines)

	var wg sync.WaitGroup

	wg.Add(goroutines)

	for i := range goroutines {
		go func() {
			defer wg.Done()

			got[i] = fmt.Sprintf("%+v", agg)
		}()
	}

	wg.Wait()

	if d := distinct(got); len(d) != 1 {
		t.Fatalf("%d distinct renderings across %d goroutines", len(d), goroutines)
	}
}

// TestFormatVerbs pins what each verb renders, for the leaf and the aggregate
// alike.
func TestFormatVerbs(t *testing.T) {
	leaf := valueErr(At("a"), "is not a valid int")
	agg := join(leaf, valueErr(At("b"), "is not a valid int"))

	for _, c := range []struct {
		name   string
		format string
		err    error
		want   string
	}{
		{"a leaf under %v", "%v", leaf, "ferry: /a: is not a valid int"},
		{"a leaf under %s", "%s", leaf, "ferry: /a: is not a valid int"},
		{"a leaf under %q", "%q", leaf, `"ferry: /a: is not a valid int"`},
		{"a leaf under %+v", "%+v", leaf, "ferry: /a: is not a valid int\n  walk, invalid value"},
		{"a leaf under a verb it has no answer for", "%d", leaf,
			"%!d(*ferry.Error=ferry: /a: is not a valid int)"},
		{"an aggregate under %v", "%v", agg, "ferry: 2 errors: /a, /b"},
		{"an aggregate under %s", "%s", agg, "ferry: 2 errors: /a, /b"},
		{"an aggregate under %q", "%q", agg, `"ferry: 2 errors: /a, /b"`},
		{"an aggregate under %+v", "%+v", agg,
			"ferry: 2 errors:\n  /a: is not a valid int\n  /b: is not a valid int"},
		{"an aggregate under a verb it has no answer for", "%d", agg,
			"%!d(*ferry.errorList=ferry: 2 errors: /a, /b)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := fmt.Sprintf(c.format, c.err); got != c.want {
				t.Fatalf("%s of the error is %q, want %q", c.format, got, c.want)
			}
		})
	}
}

// TestLeafReportNamesTheStructure covers the arms of %+v on a single failure,
// which is what a caller sees when exactly one thing went wrong.
func TestLeafReportNamesTheStructure(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want string
	}{
		{"a class and no driver", valueErr(At("a"), "is not a valid int"),
			"ferry: /a: is not a valid int\n  walk, invalid value"},
		{"a driver failure", fromDriver(momentOpen, Path{}, errors.New("kv: refused")),
			"ferry: opening the plane: kv: refused\n  open, plane error, driver"},
		{"no class at all", newError(momentWalk, nil, At("a"), "the load was cancelled"),
			"ferry: /a: the load was cancelled\n  walk"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := fmt.Sprintf("%+v", c.err); got != c.want {
				t.Fatalf("the report is\n%s\nwant\n%s", got, c.want)
			}
		})
	}
}

// TestMomentNames asserts the moment set's boundary, and that a moment outside
// it renders as itself rather than borrowing a neighbour's name.
func TestMomentNames(t *testing.T) {
	want := []struct {
		mom  moment
		name string
	}{
		{momentRegister, "register"},
		{momentCompile, "compile"},
		{momentBind, "bind"},
		{momentOpen, "open"},
		{momentWalk, "walk"},
		{momentCommit, "commit"},
		{momentClose, "close"},
		{momentUnknown, "unknown"},
	}

	if len(want) != int(momentUnknown)+1 {
		t.Fatalf("the moment set has %d members and this table names %d", int(momentUnknown)+1, len(want))
	}

	for i, w := range want {
		if int(w.mom) != i || w.mom.String() != w.name {
			t.Fatalf("moment %d is %q, want %q at %d", i, w.mom, w.name, w.mom)
		}
	}

	if got := moment(len(want)).String(); got != "moment(8)" {
		t.Fatalf("a moment past the end renders as %q", got)
	}
}

// TestDriverMessages covers the words ferry puts in front of a driver's own
// text. They are the moment in words, which is ferry's text and therefore
// always safe, and they are what stops a location-less driver error rendering
// as the bare word "driver".
func TestDriverMessages(t *testing.T) {
	for _, c := range []struct {
		mom  moment
		want string
	}{
		{momentBind, "the driver refused the address set"},
		{momentOpen, "opening the plane"},
		{momentCommit, "committing"},
		{momentClose, "closing the plane"},
		{momentWalk, "the driver failed"},
		{momentRegister, "the driver failed"},
	} {
		t.Run(c.mom.String()+"/"+c.want, func(t *testing.T) {
			if got := driverMsg(c.mom); got != c.want {
				t.Fatalf("driverMsg(%s) = %q, want %q", c.mom, got, c.want)
			}
		})
	}
}

// TestDeclaredClass covers the classification table directly, including the row
// for an error that declares nothing.
func TestDeclaredClass(t *testing.T) {
	_, wrongKind := Null().AsString()

	for _, c := range []struct {
		name string
		err  error
		want error
	}{
		{"a schema refusal", fmt.Errorf("x: %w", ErrSchema), ErrSchema},
		{"a missing address", fmt.Errorf("x: %w", ErrMissing), ErrMissing},
		{"a bad value", fmt.Errorf("x: %w", ErrValue), ErrValue},
		{"a plane failure", fmt.Errorf("x: %w", ErrPlane), ErrPlane},
		{"a read-only plane", fmt.Errorf("x: %w", ErrReadOnly), ErrPlane},
		{"an accessor's refusal", wrongKind, ErrValue},
		{"an error that declares nothing", errors.New("kv: down"), nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := declaredClass(c.err); !errors.Is(got, c.want) {
				t.Fatalf("declaredClass gave %v, want %v", got, c.want)
			}
		})
	}
}

// TestAddressIsTheOneAccessor covers what an error with no location answers,
// which is the zero Path rather than a flag a caller has to read first.
func TestAddressIsTheOneAccessor(t *testing.T) {
	if got := valueErr(At("db", "port"), "x").Address(); got != At("db", "port") {
		t.Fatalf("Address() = %s, want /db/port", got)
	}

	shut := sole(t, fromDriver(momentClose, Path{}, errors.New("kv: flush failed")))
	if got := shut.Address(); got != (Path{}) {
		t.Fatalf("a close failure has address %s, want the zero Path", got)
	}

	if got := shut.Error(); got != "ferry: closing the plane: kv: flush failed" {
		t.Fatalf("a location-less driver failure renders as %q", got)
	}

	// The compile moment is the other space the location holds: a Go field path
	// rather than a plane address, because a field with no tag never named one.
	compile := newError(momentCompile, ErrSchema, At("Debug"), "field Debug carries no ferry tag")
	if got := compile.Error(); got != "ferry: /Debug: field Debug carries no ferry tag" {
		t.Fatalf("a compile failure renders as %q", got)
	}
}

// TestUnwrapKeepsTheCauseReachable covers the half of the redaction rule that
// stops it being a loss.
func TestUnwrapKeepsTheCauseReachable(t *testing.T) {
	_, cause := strconv.ParseInt(planeSecret, 10, 64)

	err := valueErr(At("db", "secret"), "is not a valid int").withCause(cause)
	if got := errors.Unwrap(err); !errors.Is(got, cause) {
		t.Fatalf("Unwrap gave %v, want the cause", got)
	}

	if !errors.Is(err, strconv.ErrSyntax) {
		t.Fatal("strconv.ErrSyntax is no longer matchable through ferry's wrapper")
	}

	if got := valueErr(At("a"), "x"); errors.Unwrap(got) != nil {
		t.Fatalf("an error with no cause unwraps to %v", errors.Unwrap(got))
	}
}
