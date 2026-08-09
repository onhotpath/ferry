package ferry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// The three signatures ADR-0010 publishes, pinned mechanically. A change to one
// of them stops this file compiling, which is the cheapest possible test for a
// surface nobody should be able to widen by accident.
var (
	_ func(context.Context, Source, ...Option) (walkConf, error)           = Load[walkConf]
	_ func(context.Context, walkConf, Source, ...Option) (walkConf, error) = LoadOver[walkConf]
	_ func(context.Context, walkConf, Sink, ...Option) error               = Dump[walkConf]
)

// The two failures a plane can have in these tests. A driver's own error is
// what reaches the caller under ferry's wrapper, so each is asserted by
// identity rather than by its text.
var (
	errOutage  = errors.New("the backend is down")
	errRefused = errors.New("this address is denied to this token")
)

func reportOf(err error) string { return fmt.Sprintf("%+v", err) }

// mustBeClass holds every element of a report to every sentinel it should
// answer to, which is the whole of how a caller matches: errors.Is, and no
// concrete type to switch on.
func mustBeClass(t *testing.T, err error, want ...error) {
	t.Helper()

	elements := Elements(err)
	if len(elements) == 0 {
		t.Fatalf("no error at all, want one matching %v", want)
	}

	for _, e := range elements {
		for _, sentinel := range want {
			if !errors.Is(e, sentinel) {
				t.Errorf("%v does not match %v", e, sentinel)
			}
		}
	}
}

func mustReport(t *testing.T, err error, want ...string) {
	t.Helper()

	mustBeClass(t, err, ErrPlane, ErrDriver)

	report := reportOf(err)
	for _, w := range want {
		if !strings.Contains(report, w) {
			t.Errorf("report\n\t%s\ndoes not contain\n\t%s", report, w)
		}
	}
}

// answering and refusing are fresh planes over the same contents, minted per
// call, because a plane shared across cases is the same defect as a destination
// shared across them.
func answering() *plane { return newPlane(contents()) }

func refusing() *plane {
	p := newPlane(contents())
	for addr := range contents() {
		p.fail[addr] = errOutage
	}

	return p
}

// TestDumpThenLoadRoundTrips is the first end-to-end path: a struct of string
// leaves, including a nested struct and a promoted embedded one, dumps to a
// plane and loads back equal.
func TestDumpThenLoadRoundTrips(t *testing.T) {
	t.Parallel()

	want := filled()
	p := newPlane(map[Path]Value{})

	if err := Dump(t.Context(), want, planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	got, err := Load[walkConf](t.Context(), planeSource{p: p})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got != want {
		t.Errorf("round tripped to %+v, want %+v", got, want)
	}

	if p.commits != 1 || p.closes != 2 {
		t.Errorf("committed %d and closed %d times, want 1 and 2", p.commits, p.closes)
	}
}

// TestLoadIsLoadOverWithTheZeroSeed asserts the implementation and not only the
// prose: the two agree on the value and on the report, on the path that
// succeeds and on the path that fails.
func TestLoadIsLoadOverWithTheZeroSeed(t *testing.T) {
	t.Parallel()

	t.Run("over a plane that answers", loadAgreesOverAnAnsweringPlane)
	t.Run("and over one that refuses", loadAgreesOverARefusingPlane)
}

func loadAgreesOverAnAnsweringPlane(t *testing.T) {
	t.Parallel()
	mustAgree(t, answering)
}

func loadAgreesOverARefusingPlane(t *testing.T) {
	t.Parallel()
	mustAgree(t, refusing)
}

func mustAgree(t *testing.T, mint func() *plane) {
	t.Helper()

	named, first := Load[walkConf](t.Context(), planeSource{p: mint()})
	seeded, second := LoadOver(t.Context(), walkConf{}, planeSource{p: mint()})

	if named != seeded {
		t.Errorf("Load gave %+v and LoadOver with the zero seed gave %+v", named, seeded)
	}

	if reportOf(first) != reportOf(second) {
		t.Errorf("Load reported\n\t%v\nand LoadOver reported\n\t%v", first, second)
	}
}

// TestAbsentDoesNotWrite is ADR-0006's one rule seen from the seed's side:
// nothing was written, so nothing changed.
func TestAbsentDoesNotWrite(t *testing.T) {
	t.Parallel()

	seed := filled()

	got, err := LoadOver(t.Context(), seed, planeSource{p: newPlane(map[Path]Value{})})
	if err != nil {
		t.Fatalf("load over an empty plane: %+v", err)
	}

	if got != seed {
		t.Errorf("an empty plane changed the seed to %+v, want %+v", got, seed)
	}
}

// TestLoadOverCarriesForwardAndLoadDoesNot is the difference between the two
// verbs stated as the caller sees it: a reload is the carry-over written out
// loud, and a load is not.
func TestLoadOverCarriesForwardAndLoadDoesNot(t *testing.T) {
	t.Parallel()

	// The plane has since lost every address but /region, which is the shape
	// ADR-0006 measured the in-place leak on.
	remaining := map[Path]Value{At("region"): String("us-east-1")}

	carried, err := LoadOver(t.Context(), filled(), planeSource{p: newPlane(remaining)})
	if err != nil {
		t.Fatalf("reload: %+v", err)
	}

	if carried.DB.Host != "db1" || carried.Region != "us-east-1" {
		t.Errorf("LoadOver gave %+v, want the previous value with /region replaced", carried)
	}

	fresh, err := Load[walkConf](t.Context(), planeSource{p: newPlane(remaining)})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if want := (walkConf{Region: "us-east-1"}); fresh != want {
		t.Errorf("Load gave %+v, want %+v: a load carries nothing forward", fresh, want)
	}
}

// TestFailureYieldsNoValueFerryBuilt is ADR-0011's rule read as "no value it
// built": the partial exists inside ferry and the question is only what crosses
// the boundary.
func TestFailureYieldsNoValueFerryBuilt(t *testing.T) {
	t.Parallel()

	seed := walkConf{
		walkTop: walkTop{Name: "seeded", Env: "seeded"},
		Region:  "seeded",
		DB:      walkDB{Host: "seeded", Port: "seeded"},
	}

	p := answering()
	p.fail[At("db", "host")] = errRefused

	got, err := LoadOver(t.Context(), seed, planeSource{p: p})
	if err == nil {
		t.Fatal("a refused address loaded clean")
	}

	// The walk really did build a partial: it read /name, and the plane's value
	// there is not the seed's. What came back is the seed all the same.
	if !p.answered(At("name")) {
		t.Fatal("the walk never read /name, so there was no partial for the boundary to hide")
	}

	if got != seed {
		t.Errorf("LoadOver returned %+v, want the seed it was handed, %+v", got, seed)
	}

	if zero, _ := Load[walkConf](t.Context(), planeSource{p: refusing()}); zero != (walkConf{}) {
		t.Errorf("Load returned %+v, want the zero value", zero)
	}
}

func (p *plane) answered(addr Path) bool {
	for _, got := range p.got {
		if got == addr {
			return true
		}
	}

	return false
}

// TestAGetErrorIsNeverReadAsAbsent is asserted directly, because a prototype
// committed exactly this defect: a total backend outage loaded as an all-zero
// struct with a nil error, and neither the survey's YAML provider nor core's
// own mirror of it was visible for four rounds.
func TestAGetErrorIsNeverReadAsAbsent(t *testing.T) {
	t.Parallel()

	got, err := Load[walkConf](t.Context(), planeSource{p: refusing()})
	if err == nil {
		t.Fatalf("a total outage loaded as %+v with a nil error", got)
	}

	if n := len(Elements(err)); n != 5 {
		t.Errorf("%+v holds %d elements, want one per address", err, n)
	}

	mustBeClass(t, err, ErrPlane, ErrDriver, errOutage)

	if report := reportOf(err); !strings.Contains(report, "/db/host: the driver failed: the backend is down") {
		t.Errorf("report\n\t%s\ndoes not name the address and the driver's own text", report)
	}
}

// twoEngines is the type that has to fail both entry points identically. Two
// entry points that could disagree about whether a type is legal would be the
// two-engines defect at ferry's own front door.
type twoEngines struct {
	Debug string
	Port  string `ferry:"port,requird"`
}

// TestOneCompilerBehindEveryVerb asserts the shared compiler by its output and
// by its placement: the reports are identical, and the plane is never bound,
// because a compile failure means no walk runs.
func TestOneCompilerBehindEveryVerb(t *testing.T) {
	t.Parallel()

	src, sink := answering(), newPlane(map[Path]Value{})

	_, loaded := Load[twoEngines](t.Context(), planeSource{p: src})
	dumped := Dump(t.Context(), twoEngines{}, planeSink{p: sink})
	compiled := Compile[twoEngines]()

	for name, err := range map[string]error{"Load": loaded, "Dump": dumped} {
		if reportOf(err) != reportOf(compiled) {
			t.Errorf("%s reported\n\t%+v\nand Compile reported\n\t%+v", name, err, compiled)
		}
	}

	mustBeClass(t, compiled, ErrSchema)

	if src.bound != nil || sink.bound != nil {
		t.Error("a plane was bound behind a schema that does not compile")
	}
}

// TestAnInterfaceRootRefusesAcrossVerbs pins #134's ruling: an interface-typed
// root stays refused. Compile[any]'s refusal was already pinned; this is the
// gap the issue named, Load[any] and Dump[any] asserted alongside it, all
// three agreeing because they share one compiler (TestOneCompilerBehindEveryVerb's
// property, at T = any).
func TestAnInterfaceRootRefusesAcrossVerbs(t *testing.T) {
	t.Parallel()

	src, sink := answering(), newPlane(map[Path]Value{})

	_, loaded := Load[any](t.Context(), planeSource{p: src})
	dumped := Dump(t.Context(), any(42), planeSink{p: sink})
	compiled := Compile[any]()

	for name, err := range map[string]error{"Load": loaded, "Dump": dumped} {
		if reportOf(err) != reportOf(compiled) {
			t.Errorf("%s reported\n\t%+v\nand Compile reported\n\t%+v", name, err, compiled)
		}
	}

	mustBeClass(t, compiled, ErrSchema)
	mustContain(t, reportOf(compiled), []string{
		"interface {} is not a struct ferry walks",
		"wrapping it in one is the whole remedy",
	})

	if src.bound != nil || sink.bound != nil {
		t.Error("a plane was bound behind a schema that does not compile")
	}
}

// TestASharedDestinationHidesABrokenWalk is the trap xload's own equivalence
// test falls into, reproduced here so that this suite's own fresh-destination
// rule has a reason attached to it.
//
// The broken second walk is an empty plane, which writes nothing and is
// therefore a walk that skips every leaf. Against a shared destination it
// passes; against a fresh one it fails.
func TestASharedDestinationHidesABrokenWalk(t *testing.T) {
	t.Parallel()

	want := filled()

	first, err := Load[walkConf](t.Context(), planeSource{p: answering()})
	if err != nil {
		t.Fatalf("the first walk: %+v", err)
	}

	shared, err := LoadOver(t.Context(), first, planeSource{p: newPlane(map[Path]Value{})})
	if err != nil {
		t.Fatalf("the second walk: %+v", err)
	}

	if shared != want {
		t.Errorf("the shared destination caught the broken walk; it is supposed to hide it: %+v", shared)
	}

	fresh, err := Load[walkConf](t.Context(), planeSource{p: newPlane(map[Path]Value{})})
	if err != nil {
		t.Fatalf("the second walk, fresh: %+v", err)
	}

	if fresh == want {
		t.Errorf("a fresh destination did not catch the broken walk: %+v", fresh)
	}
}

// TestAPointerRoot is the one root shape whose contents a copy of the seed
// would still share with the caller, plus the nil that has no address of its
// own for a null to sit at.
func TestAPointerRoot(t *testing.T) {
	t.Parallel()

	t.Run("a nil root is materialised", loadMaterialisesANilRoot)
	t.Run("a seeded root is copied rather than written through", loadCopiesASeededRoot)
	t.Run("dump refuses a nil root", dumpRefusesANilRoot)
	t.Run("dump reads through a non-nil root", dumpReadsThroughARoot)
}

// hostOnly is a plane holding one of the two addresses *walkDB names, so a
// seeded field and a loaded one are both visible in one result.
func hostOnly() *plane { return newPlane(map[Path]Value{At("host"): String("db1")}) }

func loadMaterialisesANilRoot(t *testing.T) {
	t.Parallel()

	got, err := Load[*walkDB](t.Context(), planeSource{p: hostOnly()})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got == nil || got.Host != "db1" {
		t.Errorf("load gave %v, want a materialised pointer holding db1", got)
	}
}

func loadCopiesASeededRoot(t *testing.T) {
	t.Parallel()

	seed := &walkDB{Host: "seeded", Port: "seeded"}

	got, err := LoadOver(t.Context(), seed, planeSource{p: hostOnly()})
	if err != nil {
		t.Fatalf("load over: %+v", err)
	}

	if got == seed {
		t.Fatal("the walk wrote through the caller's own pointer")
	}

	if *got != (walkDB{Host: "db1", Port: "seeded"}) || seed.Host != "seeded" {
		t.Errorf("load over gave %+v and left the seed as %+v", *got, *seed)
	}
}

func dumpRefusesANilRoot(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})

	err := Dump(t.Context(), (*walkDB)(nil), planeSink{p: p})
	if !strings.Contains(reportOf(err), "the root is a nil pointer") {
		t.Errorf("dumping a nil root reported %+v", err)
	}

	if len(p.set) != 0 {
		t.Errorf("the plane was written to at %v, want untouched", p.set)
	}
}

func dumpReadsThroughARoot(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})

	if err := Dump(t.Context(), &walkDB{Host: "db1", Port: "5432"}, planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if len(p.set) != 2 {
		t.Errorf("the plane was written %v, want both addresses", p.set)
	}
}

// TestTheDriverLifecycle is ADR-0004's protocol, every phase of it, from the
// two verbs that run it.
func TestTheDriverLifecycle(t *testing.T) {
	t.Parallel()

	t.Run("bind refuses before any I/O", bindRefusesBeforeAnyIO)
	t.Run("open refuses before any write", openRefusesBeforeAnyWrite)
	t.Run("commit and close both join the report", commitAndCloseJoinTheReport)
	t.Run("a close failure is a failure", aCloseFailureYieldsNothing)
	t.Run("a plane with no lifecycle is asked for none", noLifecycleIsAskedFor)
}

// bindRefusesBeforeAnyIO runs both directions, because the phase is a property
// of the contract rather than of the verb: a refusal about the address set
// happens before any backend call, which is what lets a plane-to-plane transfer
// be refused after zero of them.
func bindRefusesBeforeAnyIO(t *testing.T) {
	t.Parallel()

	src, sink := answering(), newPlane(map[Path]Value{})
	src.bindErr, sink.bindErr = errRefused, errRefused

	_, err := Load[walkConf](t.Context(), planeSource{p: src})
	mustReport(t, err, "the driver refused the address set")
	mustReport(t, Dump(t.Context(), filled(), planeSink{p: sink}), "the driver refused the address set")

	if len(src.got) != 0 || len(sink.set) != 0 {
		t.Errorf("the plane was used at %v and %v after refusing the address set", src.got, sink.set)
	}
}

func openRefusesBeforeAnyWrite(t *testing.T) {
	t.Parallel()

	src, sink := answering(), newPlane(map[Path]Value{})
	src.openErr, sink.openErr = errRefused, errRefused

	_, err := Load[walkConf](t.Context(), planeSource{p: src})
	mustReport(t, err, "opening the plane")
	mustReport(t, Dump(t.Context(), filled(), planeSink{p: sink}), "opening the plane")

	if len(src.got) != 0 || len(sink.set) != 0 {
		t.Errorf("the plane was used at %v and %v after refusing to open", src.got, sink.set)
	}
}

func commitAndCloseJoinTheReport(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	p.commitErr, p.closeErr = errRefused, errRefused

	err := Dump(t.Context(), filled(), planeSink{p: p})
	mustReport(t, err, "committing", "closing the plane")

	if n := len(Elements(err)); n != 2 {
		t.Errorf("%+v holds %d elements, want 2", err, n)
	}
}

func aCloseFailureYieldsNothing(t *testing.T) {
	t.Parallel()

	p := answering()
	p.closeErr = errRefused

	got, err := LoadOver(t.Context(), filled(), planeSource{p: p})
	mustReport(t, err, "closing the plane")

	if got != filled() {
		t.Errorf("a failed load returned %+v, want the seed", got)
	}
}

func noLifecycleIsAskedFor(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	p.lifecycle = false

	if err := Dump(t.Context(), filled(), planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if p.commits != 0 || p.closes != 0 {
		t.Errorf("committed %d and closed %d times, want neither", p.commits, p.closes)
	}
}

// TestAnOptionRefusalFailsTheCall is the Option list being wrong rather than
// the type: the refusal is about this call, and no plane is reached.
func TestAnOptionRefusalFailsTheCall(t *testing.T) {
	t.Parallel()

	p := answering()

	_, err := Load[walkConf](t.Context(), planeSource{p: p}, TagKey(""))
	mustBeClass(t, err, ErrSchema)

	if err = Dump(t.Context(), filled(), planeSink{p: p}, TagKey("")); !errors.Is(err, ErrSchema) {
		t.Errorf("Dump under a bad Option reported %+v", err)
	}

	if p.bound != nil {
		t.Error("a plane was bound under an Option list that does not resolve")
	}
}

// TestANilPlaneIsRefusedRatherThanDereferenced is the most ordinary caller
// mistake there is - a field never assigned, or a constructor whose error was
// dropped - and ADR-0011 exists so that it arrives as a diagnostic instead of a
// stack trace. The refusal is core's own, so it carries no provenance, and a
// plane that is not there holds no address, so the element has no location.
func TestANilPlaneIsRefusedRatherThanDereferenced(t *testing.T) {
	t.Parallel()

	load := withoutPanicking(t, func() error {
		_, err := Load[walkConf](t.Context(), nil)

		return err
	})

	dump := withoutPanicking(t, func() error { return Dump(t.Context(), filled(), nil) })

	mustRefuseANilPlane(t, load, "the source is nil")
	mustRefuseANilPlane(t, dump, "the sink is nil")

	// A caller who passed a nil Source and one who passed a nil Sink read the
	// same report otherwise, and would have to go back to the call site to find
	// out which half they left unassigned.
	if reportOf(load) == reportOf(dump) {
		t.Errorf("both halves report\n\t%+v\nand a caller cannot tell which one was nil", load)
	}
}

// TestANilSinkIsRefusedBeforeTheValueIsLookedAt pins the order of the two
// refusals a Dump can make when both apply.
//
// Dump is BindSink plus the method, and BindSink has no value in hand, so the
// nil sink is refused first and the nil root pointer is never reached. That is
// the defensible order rather than an accident of the split: the nil sink is a
// fault in the call and the nil root is a fault in the value, and a call that
// named no plane at all failed before the value was ever relevant.
func TestANilSinkIsRefusedBeforeTheValueIsLookedAt(t *testing.T) {
	t.Parallel()

	err := withoutPanicking(t, func() error { return Dump(t.Context(), (*walkDB)(nil), nil) })

	mustRefuseANilPlane(t, err, "the sink is nil")

	if strings.Contains(reportOf(err), "the root is a nil pointer") {
		t.Errorf("%+v reports the value, and the call named no plane to write it to", err)
	}
}

// withoutPanicking runs one call and turns a panic into a failure of this test.
//
// It is the assertion and not scaffolding: the defect under test is a nil
// dereference, and a dereference in a test binary takes every other test in the
// package down with it, so a regression here would be a crash with no name on it
// rather than a red test.
func withoutPanicking(t *testing.T, call func() error) error {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("a nil plane panicked with %v, want a refusal", r)
		}
	}()

	return call()
}

// mustRefuseANilPlane holds one half's refusal to ADR-0011's shape: the class a
// caller matches on, the provenance it must not claim, the absent location, and
// the half it names.
func mustRefuseANilPlane(t *testing.T, err error, half string) {
	t.Helper()

	mustBeClass(t, err, ErrPlane)

	if errors.Is(err, ErrDriver) {
		t.Errorf("%+v claims to come from a driver, and no driver was ever reached", err)
	}

	e, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("%v is not one of ferry's own errors", err)
	}

	if e.Address() != (Path{}) {
		t.Errorf("%+v is located at %s, and a plane that is not there holds no address", err, e.Address())
	}

	if !strings.Contains(e.Error(), half) {
		t.Errorf("%+v does not say %q, so it does not name the half that was nil", err, half)
	}
}

// TestARootPointerToALeafCarriesItsNull is the pointer-to-leaf root, in both
// directions.
//
// *int resolves to a leaf carrying a null rather than to a pointer the walk
// descends through, so nothing here dereferences it: the codec is built for
// *int and is the one thing that decides what nil means. Dereferencing it in
// the entry point handed an int to that codec, which panicked inside the shared
// walk and escaped the fence.
func TestARootPointerToALeafCarriesItsNull(t *testing.T) {
	t.Parallel()

	t.Run("a nil one dumps its null at the root", aNilRootPointerDumpsItsNull)
	t.Run("and a plane holding a null loads back nil", aNullAtTheRootLoadsAsNil)
	t.Run("and a plane holding a number loads back a pointer to it", aNumberAtTheRootLoadsAsAPointer)
}

func aNilRootPointerDumpsItsNull(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})

	if err := withoutPanicking(t, func() error {
		return Dump(t.Context(), (*int)(nil), planeSink{p: p})
	}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	mustBeAddresses(t, p.set, []string{""})

	if got := p.values[Path{}]; got != Null {
		t.Errorf("the plane holds %#v at the root, want the null the leaf carries", got)
	}
}

func aNullAtTheRootLoadsAsNil(t *testing.T) {
	t.Parallel()

	got, err := loadRootPointer(t, newPlane(map[Path]Value{{}: Null}))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got != nil {
		t.Errorf("loaded %v, want the nil the plane's null says", *got)
	}
}

func aNumberAtTheRootLoadsAsAPointer(t *testing.T) {
	t.Parallel()

	got, err := loadRootPointer(t, newPlane(map[Path]Value{{}: Number("8080")}))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got == nil || *got != 8080 {
		t.Errorf("loaded %v, want a pointer to the number the plane held", got)
	}
}

// loadRootPointer is one load of a *int root, with the panic the fix is about
// turned into this test's own failure rather than a crash with no name on it.
func loadRootPointer(t *testing.T, p *plane) (*int, error) {
	t.Helper()

	var (
		got *int
		err error
	)

	if panicked := withoutPanicking(t, func() error {
		got, err = Load[*int](t.Context(), planeSource{p: p})

		return nil
	}); panicked != nil {
		return nil, panicked
	}

	return got, err
}

// TestANilRootPointerToAStructIsStillRefused is what stops the narrowing going
// too far.
//
// A struct is not an address of its own, so the Null a nil composite writes has
// nowhere to sit, and a dump of one would write nothing and report success.
func TestANilRootPointerToAStructIsStillRefused(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})

	err := Dump(t.Context(), (*walkDB)(nil), planeSink{p: p})
	if err == nil {
		t.Fatal("dumping a nil pointer to a struct returned no error")
	}

	if !errors.Is(err, ErrValue) {
		t.Errorf("%+v is not an ErrValue", err)
	}

	if !strings.Contains(reportOf(err), "nil pointer to a struct") {
		t.Errorf("the report is %+v, want it to name the struct it is about", err)
	}

	if len(p.set) != 0 {
		t.Errorf("the plane was written to at %v, want untouched", p.set)
	}
}
