package ferry

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// This file is the aggregation half of ADR-0011: where the rule lives, what the
// walk owns of it, and what the collection has to survive.
//
// Two tests here reach the scheduler seam, and they are the only tests in this
// package that reach anything the walk owns. That is deliberate rather than a
// lapse: the claim under test is that aggregation is a property of the
// scheduler and not of the walk, and there is no way to assert "one walk
// function, two schedulers" through a seam that publishes one scheduler and no
// way to choose it. Everything else goes through the published verbs.

// firstError is the scheduler ferry does not ship: stop at the first failure.
//
// It exists in this file and nowhere else. It is the comparison baseline
// ADR-0011's table calls a counterfactual, and having it here rather than in
// walk.go is the whole of what "no importer can select a scheduler" means.
func firstError(n int, run func(i int) (outcome, error)) (outcome, error) {
	var b batch

	for i := range n {
		got, err := run(i)
		b.add(got, err)

		if err != nil {
			return b.done()
		}
	}

	return b.done()
}

// loadUnder runs the walk over walkConf under a scheduler the caller chooses.
//
// It is Load's own body with one line changed, and the line is the scheduler.
// The walk function, the direction, the compiled schema and the plane are the
// same on both sides of every comparison below.
func loadUnder(t *testing.T, run sched, p *plane) error {
	t.Helper()

	sch, err := schemaOf(reflect.TypeFor[walkConf](), nil, discarded)
	if err != nil {
		t.Fatalf("compile: %+v", err)
	}

	open, err := (planeSource{p: p}).Bind(sch.addrs)
	if err != nil {
		t.Fatalf("bind: %+v", err)
	}

	r, err := open(t.Context())
	if err != nil {
		t.Fatalf("open: %+v", err)
	}

	var out walkConf

	w := newWalker(loadFrom{r: r})
	w.run = run

	_, err = w.walk(t.Context(), spot{n: sch.root, v: reflect.ValueOf(&out).Elem()})

	return err
}

// TestEverySubtreeReturnsItsOwnPresence is what makes the presence bit
// composable rather than merely correct today.
//
// The scheduler is where a subtree's answer arrives, so it is where the claim
// can be read: every member of a container hands back its own presence fact,
// and the container's own answer is those combined. A bit written into one
// location the whole walk shares looks the same from a caller and is a
// different fact - it is the walk's running total, and a subtree reading it
// across its own descent is reading whatever else ran in between.
//
// The plane holds one address, under /db, so exactly one member of the root
// says yes and exactly one member of /db does.
func TestEverySubtreeReturnsItsOwnPresence(t *testing.T) {
	t.Parallel()

	var said [][]bool

	inspect := func(n int, run func(i int) (outcome, error)) (outcome, error) {
		bits := make([]bool, 0, n)

		var b batch

		for i := range n {
			got, err := run(i)
			bits = append(bits, got.wrote)
			b.add(got, err)
		}

		said = append(said, bits)

		return b.done()
	}

	p := newPlane(map[Path]Value{At("db", "host"): String("db1")})
	if err := loadUnder(t, inspect, p); err != nil {
		t.Fatalf("load: %+v", err)
	}

	// The root's four members are /name, /env, /region and the /db container,
	// and /db's two are its leaves. Each list holds exactly one yes, which is
	// the subtree the plane spoke under and never a sibling of it.
	for _, bits := range said {
		if yes := countTrue(bits); yes != 1 {
			t.Errorf("a container's members said %v, want exactly one of them to have written", bits)
		}
	}

	if len(said) != 2 {
		t.Fatalf("%d containers were scheduled, want the root and /db", len(said))
	}
}

func countTrue(bits []bool) int {
	n := 0

	for _, bit := range bits {
		if bit {
			n++
		}
	}

	return n
}

// badFive is a plane whose five addresses all hold a kind a string cannot take,
// which is 5.4 in ferry's own shape: five bad fields.
func badFive() *plane {
	values := map[Path]Value{}
	for addr := range contents() {
		values[addr] = Number("8080")
	}

	return newPlane(values)
}

// TestAggregationIsTheSchedulersAndNotTheWalks is ADR-0010's measurement run
// rather than quoted, and it is criterion 5.4's fix: the same walk function
// under a first-error scheduler and under core's own reports one error and
// five over the same plane.
//
// The walk is unchanged between the two rows in the strongest available sense -
// it is one function, called from one helper, with one line differing at the
// call site - and the plane records the rest: what the first-error run asked
// for is a prefix of what the aggregating run asked for, so the two agree
// everywhere the first has an opinion and the second simply carries on.
func TestAggregationIsTheSchedulersAndNotTheWalks(t *testing.T) {
	t.Parallel()

	aggregating, failing := badFive(), badFive()

	many := loadUnder(t, serial, aggregating)
	one := loadUnder(t, firstError, failing)

	if n := len(Elements(many)); n != 5 {
		t.Errorf("the aggregating scheduler reported %d elements, want one per bad field:\n%+v", n, many)
	}

	if n := len(Elements(one)); n != 1 {
		t.Errorf("the first-error scheduler reported %d elements, want 1:\n%+v", n, one)
	}

	if len(failing.got) > len(aggregating.got) {
		t.Fatalf("the first-error walk asked for %v and the aggregating one for %v", failing.got, aggregating.got)
	}

	for i, addr := range failing.got {
		if aggregating.got[i] != addr {
			t.Errorf("the walks diverged at %d: %v against %v", i, failing.got, aggregating.got)
		}
	}
}

// TestLoadAggregatesThroughTheEntryPoint is the same fact where a caller can
// see it: nothing selects a scheduler, and five bad fields are five errors.
func TestLoadAggregatesThroughTheEntryPoint(t *testing.T) {
	t.Parallel()

	p := badFive()

	got, err := Load[walkConf](t.Context(), planeSource{p: p})
	if got != (walkConf{}) {
		t.Errorf("a failed load yielded %+v, want the zero value", got)
	}

	if n := len(Elements(err)); n != 5 {
		t.Fatalf("%+v holds %d elements, want 5", err, n)
	}

	if len(p.got) != 5 {
		t.Errorf("the walk asked for %v, want every address", p.got)
	}

	mustBeClass(t, err, ErrValue)
}

// The three rows the one suppression bit is measured on. reqPair is the section
// whose own required has something to summarise, and reqPlain is the section
// with nothing under it that can report.
type (
	reqPair struct {
		User string `ferry:"user,required"`
		Pass string `ferry:"pass"`
	}
	reqPlain struct {
		User string `ferry:"user"`
		Pass string `ferry:"pass"`
	}
	reqSectionOfPair struct {
		Auth reqPair `ferry:"auth,required"`
	}
	reqSectionOfPlain struct {
		Auth reqPlain `ferry:"auth,required"`
	}
	// reqOptionalOfPair is the same bit at the other composite shape, which is a
	// second code path: a pointer decides whether to materialise after its
	// subtree ran, where a plain struct only has required to answer.
	reqOptionalOfPair struct {
		Auth *reqPair `ferry:"auth,required"`
	}
)

// TestTheWalkOwnsOneSuppressionBit is the one case aggregation cannot see and
// the walk can, asserted from both sides of it.
//
// ADR-0011's rule is to report every failure that is not a consequence of
// another it is already reporting, and a composite's required failure is the
// summary of what its children just said: a required child absent under a
// required parent is one remediation, so the parent's failure is suppressed
// where a child under it already reported. The neighbouring case needs nothing,
// which is why this is one bit and not a redesign, and the third row is what
// stops the bit being a deletion: with nothing under the address reporting, the
// parent's required fires exactly as it always did.
func TestTheWalkOwnsOneSuppressionBit(t *testing.T) {
	t.Parallel()

	t.Run("a required child absent under a required parent", suppressedByAnAbsentChild)
	t.Run("a present child that fails to decode", suppressedByAFailedChild)
	t.Run("the same under an optional section", suppressedUnderAPointer)
	t.Run("and nothing under it at all still reports the parent", theParentStillReports)
}

func suppressedUnderAPointer(t *testing.T) {
	t.Parallel()

	_, err := Load[reqOptionalOfPair](t.Context(), planeSource{p: newPlane(map[Path]Value{})})

	mustBeOneRemediation(t, err, At("auth", "user"), ErrMissing)
}

func suppressedByAnAbsentChild(t *testing.T) {
	t.Parallel()

	_, err := Load[reqSectionOfPair](t.Context(), planeSource{p: newPlane(map[Path]Value{})})

	mustBeOneRemediation(t, err, At("auth", "user"), ErrMissing)
}

func suppressedByAFailedChild(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{At("auth", "user"): Number("8080")})

	_, err := Load[reqSectionOfPair](t.Context(), planeSource{p: p})

	mustBeOneRemediation(t, err, At("auth", "user"), ErrValue)
}

func theParentStillReports(t *testing.T) {
	t.Parallel()

	_, err := Load[reqSectionOfPlain](t.Context(), planeSource{p: newPlane(map[Path]Value{})})

	mustBeOneRemediation(t, err, At("auth"), ErrMissing)
}

// mustBeOneRemediation is one error at one address, which is the whole of what
// the bit buys: two errors and one remediation would make an operator fix the
// same thing twice.
func mustBeOneRemediation(t *testing.T, err error, want Path, class error) {
	t.Helper()

	if err == nil {
		t.Fatalf("nothing was reported, want one failure at %s", want)
	}

	if got := reportedAddresses(err); len(got) != 1 || got[0] != want {
		t.Fatalf("the report names %v, want exactly [%s]:\n%+v", got, want, err)
	}

	if !errors.Is(err, class) {
		t.Errorf("%+v is not a %v", err, class)
	}
}

// noTagAndBadOption is two schema faults on one type, over a plane that would
// fail every address it was asked for.
type noTagAndBadOption struct {
	Debug string
	Port  string `ferry:"port,requird"`
}

// TestACompileErrorNeverSharesAnAggregateWithAWalkError asserts the property
// rather than a rule enforcing it, because there is no rule: a compile failure
// means no walk runs, so schemaOf returns before Bind is ever called. The
// assertion is that the plane was not bound and not asked, which is what makes
// the aggregate's contents a consequence of the control flow.
func TestACompileErrorNeverSharesAnAggregateWithAWalkError(t *testing.T) {
	t.Parallel()

	p := refusing()

	_, err := Load[noTagAndBadOption](t.Context(), planeSource{p: p})

	elements := Elements(err)
	if len(elements) != 2 {
		t.Fatalf("%+v holds %d elements, want the two schema faults", err, len(elements))
	}

	for _, e := range elements {
		if !errors.Is(e, ErrSchema) || errors.Is(e, ErrPlane) || errors.Is(e, ErrDriver) {
			t.Errorf("%v is not a schema fault alone", e)
		}
	}

	if p.bound != nil || len(p.got) != 0 {
		t.Errorf("the plane was bound=%v and asked for %v behind a schema that does not compile", p.bound != nil, p.got)
	}
}

// TestTenThousandFailingMapKeys is the cap that is not there, and the two forms
// that make its absence safe: a one-line %v that states the count it did not
// name, and a %+v that drops nothing.
func TestTenThousandFailingMapKeys(t *testing.T) {
	t.Parallel()

	const count = 10000

	values := make(map[Path]Value, count)
	for i := range count {
		values[At("limits", fmt.Sprintf("k%05d", i))] = String("not a number")
	}

	_, err := Load[limitsOnly](t.Context(), treeSource{p: newPlane(values)})

	if n := len(Elements(err)); n != count {
		t.Fatalf("%d elements, want %d: there is no cap on the element count", n, count)
	}

	line := fmt.Sprintf("%v", err)

	const want = "ferry: 10000 errors: /limits/k00000, /limits/k00001, /limits/k00002, and 9997 more"

	if line != want {
		t.Fatalf("the one-line form is %q, want %q", line, want)
	}

	if n := strings.Count(fmt.Sprintf("%+v", err), "\n"); n != count {
		t.Errorf("the report has %d element lines, want %d: nothing is dropped silently", n, count)
	}
}

// TestFourGoroutinesProduceOneReport is the constraint the sort key inherits
// from the parked concurrency question: the collection has to be safe and the
// order must not be insertion order, or a concurrent walk brings item 5.5 back.
//
// The tasks are collected under a lock in whatever order they finish, which is
// exactly what a concurrent scheduler would do and is the shape that makes
// insertion order nondeterministic. Nothing here walks: the walk's presence bit
// and minted set are shared mutable state across the seam, which is the hazard
// ADR-0010 records and which this ticket does not fix.
func TestFourGoroutinesProduceOneReport(t *testing.T) {
	t.Parallel()

	const runs = 300

	printed, picked := map[string]struct{}{}, map[string]struct{}{}

	for range runs {
		err := fanOut(fourFailures())

		printed[fmt.Sprintf("%+v", err)] = struct{}{}

		first, ok := errors.AsType[*Error](err)
		if !ok {
			t.Fatalf("%v holds no ferry element", err)
		}

		picked[first.Address().String()] = struct{}{}
	}

	if len(printed) != 1 {
		t.Errorf("%d distinct reports over %d runs, want 1", len(printed), runs)
	}

	// The programmatic reader is the half sorting in Format would not have
	// fixed: errors.AsType returns the first match in tree order.
	if len(picked) != 1 {
		t.Errorf("errors.AsType picked %d distinct elements over %d runs, want 1", len(picked), runs)
	}
}

// fanOut is the scheduler shape a concurrent mode would have: every task in its
// own goroutine, and the failures collected as they arrive.
func fanOut(tasks []func() error) error {
	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)

	for _, task := range tasks {
		wg.Go(func() {
			if err := task(); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		})
	}

	wg.Wait()

	return join(errs...)
}

// fourFailures is four errors at four addresses, one per goroutine.
func fourFailures() []func() error {
	addrs := []Path{At("db", "port"), At("db", "host"), At("tls", "cert"), At("tls", "key")}
	tasks := make([]func() error, 0, len(addrs))

	for _, at := range addrs {
		tasks = append(tasks, func() error {
			return newError(momentWalk, ErrValue, at, "is not a valid int")
		})
	}

	return tasks
}

// TestNothingShipsToMakeFerryReportLess is the knob that is not there.
//
// StopOnFirstError is a public knob whose only job is to make ferry report
// less, and ferry has no old behaviour for it to restore. Not shipping it costs
// nothing and shipping it doubles the test matrix on every error path in the
// design; adding one later is a load-affecting Option, which is the cheap kind.
// This is a name scan rather than a promise, over every file core ships.
func TestNothingShipsToMakeFerryReportLess(t *testing.T) {
	t.Parallel()

	forbidden := []string{"StopOn", "FailFast", "FirstError", "MaxErrors"}

	for _, name := range exportedPackageNames(t) {
		for _, bad := range forbidden {
			if strings.Contains(name, bad) {
				t.Errorf("core exports %s: nothing ships whose job is to report less", name)
			}
		}
	}
}

// exportedPackageNames is every exported top-level name in core, read off the
// source rather than off a list somebody has to remember to update.
func exportedPackageNames(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}

	var out []string

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		out = append(out, exportedIn(t, name)...)
	}

	return out
}

func exportedIn(t *testing.T, file string) []string {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}

	return exportedDecls(f)
}
