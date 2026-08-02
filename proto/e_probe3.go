package main

// E9-E15.
//
// E9  (P5) is ErrorAt a SECOND way to attach an address, which is 5.14's first
//          item, "two ways to set the loader"
// E10 (P7) does the aggregate need a cap
// E11      the rendering at three errors and at forty
// E12      the ferrytest diff, and the suppression defect it exists to catch
// E13      the audit: one field tripping two rules, and an all-one-class set
// E14      what Load hands back when it fails
// E15      a driver holding an opinion about the class

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func init() {
	e9Hooks = append(e9Hooks, runE9, runE10, runE11, runE12, runE13, runE14, runE15)
}

// ---------------------------------------------------------------------------
// E9 (P5)
// ---------------------------------------------------------------------------

func runE9() {
	hdr("E9  (P5) is ErrorAt a second way to attach an address")

	// A driver refusing at Bind. Core has the whole SET and no single address,
	// so ADR-0004 leaves legality with the driver and the driver is the only
	// party that knows which address it disliked.
	drvErr := ErrorAt(path("feature flags"), fmt.Errorf("an env var name cannot contain a space: %w", ErrPlane))
	core := fromDriver(mBind, Path{}, false, drvErr)
	fmt.Printf("  driver refuses at Bind : %v\n", core)
	fmt.Printf("    address survived     : %s\n", core.Address())
	fmt.Printf("    class                : Plane=%v Value=%v\n", errors.Is(core, ErrPlane), errors.Is(core, ErrValue))
	fmt.Printf("    provenance           : Driver=%v\n", errors.Is(core, ErrDriver))

	// The check that matters for 5.14: is ErrorAt a second CONSTRUCTOR?
	bare := ErrorAt(path("x"), errorsNew("boom"))
	var asFerry *Error
	fmt.Printf("\n  ErrorAt alone is a *ferry.Error : %v\n", errors.As(bare, &asFerry))
	fmt.Printf("  ErrorAt alone matches any class : %v\n",
		errors.Is(bare, ErrPlane) || errors.Is(bare, ErrValue) || errors.Is(bare, ErrSchema) || errors.Is(bare, ErrMissing))
	fmt.Println("  So it ATTACHES and never CLASSIFIES, and it is inert until core wraps")
	fmt.Println("  it. There is one constructor of ferry errors, and it is core's.")

	// And where core already knows the address, core's wins: a driver cannot
	// misattribute a Get failure to a different address.
	lie := ErrorAt(path("somewhere", "else"), errorsNew("nope"))
	atGet := fromDriver(mWalk, path("db", "host"), true, lie)
	fmt.Printf("\n  driver names /somewhere/else inside a Get at /db/host:\n    -> %s\n", atGet.Address())
	fmt.Println("  Core supplies the address wherever core knows it. The driver's own")
	fmt.Println("  address is used only where core has none, which is Bind.")
}

// ---------------------------------------------------------------------------
// E10 (P7)
// ---------------------------------------------------------------------------

type E10Conf struct{ Creds map[string]int }

func runE10() {
	hdr("E10 (P7) does the aggregate need a cap")

	s := mustSchema(reflect.TypeFor[E10Conf]())
	const n = 10000
	plane := map[Path]Value{}
	for i := range n {
		plane[addr("Creds").Name(fmt.Sprintf("k%05d", i))] = String("not-a-number")
	}

	var v E10Conf
	sink := &errSink{}
	start := time.Now()
	_, _ = loadD(plane, s, reflect.ValueOf(&v).Elem(), loadOpts{sink: sink})
	walk := time.Since(start)
	start = time.Now()
	got := sink.result()
	build := time.Since(start)

	start = time.Now()
	line := got.Error()
	oneLine := time.Since(start)

	start = time.Now()
	full := fmt.Sprintf("%+v", got)
	report := time.Since(start)

	fmt.Printf("  %d failing map keys\n", n)
	fmt.Printf("    elements                 : %d\n", len(Elements(got)))
	fmt.Printf("    the walk itself          : %v\n", walk.Round(time.Microsecond))
	fmt.Printf("    sort + join              : %v\n", build.Round(time.Microsecond))
	fmt.Printf("    Error() one-line         : %v, %d bytes\n", oneLine.Round(time.Microsecond), len(line))
	fmt.Printf("    %%+v full report          : %v, %d bytes\n", report.Round(time.Microsecond), len(full))
	fmt.Printf("\n    the one-line form: %s\n", line)
	fmt.Println("\n  The one-line form is already bounded by the elision, so the thing that")
	fmt.Println("  would grow without limit is only reached by a caller who asked for it.")
	fmt.Println("  A cap would have to pick an N, and ADR-0001 forbids dropping anything")
	fmt.Println("  silently, so a capped set has to say what it dropped anyway.")
}

// ---------------------------------------------------------------------------
// E11
// ---------------------------------------------------------------------------

func runE11() {
	hdr("E11 the rendering, at one, at three and at forty")

	one := errAt(mWalk, ErrValue, path("db", "port"), "is not a valid int")
	fmt.Printf("  one, %%v   %v\n", one)
	fmt.Printf("  one, %%+v  %+v\n\n", one)

	three := join(
		errAt(mWalk, ErrValue, path("db", "port"), "is not a valid int"),
		errAt(mWalk, ErrMissing, path("tls", "cert"), "required, and the plane supplied nothing"),
		errAt(mWalk, ErrValue, path("workers").Index(7), "the plane has index 7 and [3]string holds 3"),
	)
	fmt.Printf("  three, %%v\n    %v\n\n", three)
	fmt.Printf("  three, %%+v\n%+v\n", three)

	var many []error
	for i := range 40 {
		many = append(many, errAt(mWalk, ErrValue, path("svc", fmt.Sprintf("f%02d", i)), "is not a valid int"))
	}
	forty := join(many...)
	fmt.Printf("  forty, %%v\n    %v\n", forty)
	fmt.Printf("\n  forty, wrapped by a caller:\n    %v\n", fmt.Errorf("loading config: %w", forty))
	fmt.Printf("\n  forty, %%+v is %d lines and is not shown here.\n", 41)
	fmt.Println("  The one-line form stays inside a sentence and still names three")
	fmt.Println("  addresses, which is what an operator acts on. Run `p9 etui` to drive it.")
}

// ---------------------------------------------------------------------------
// E12
// ---------------------------------------------------------------------------

type fakeT struct{ msgs []string }

func (f *fakeT) Helper() {}
func (f *fakeT) Errorf(format string, args ...any) {
	f.msgs = append(f.msgs, fmt.Sprintf(format, args...))
}

func runE12() {
	hdr("E12 the ferrytest diff, and the defect it exists to catch")

	got := join(
		errAt(mWalk, ErrValue, path("db", "port"), "is not a valid int"),
		errAt(mWalk, ErrMissing, path("tls", "cert"), "required, and the plane supplied nothing"),
	)

	t1 := &fakeT{}
	CheckErrors(t1, got,
		Want{path("db", "port"), ErrValue},
		Want{path("tls", "cert"), ErrMissing},
	)
	fmt.Printf("  exact match          : %d failures\n", len(t1.msgs))

	t2 := &fakeT{}
	CheckErrors(t2, got, Want{path("db", "port"), ErrMissing}, Want{path("tls", "cert"), ErrMissing})
	fmt.Printf("  wrong class          : %s\n", strings.ReplaceAll(t2.msgs[0], "\n", "\n  "))

	// The case the exact-set semantics exist for. ADR-0008's tiers suppress a
	// consequence; the defect they will develop is firing ONCE TOO OFTEN, and a
	// "contains" assertion cannot see it.
	overreported := join(
		errAt(mCompile, ErrSchema, path("origins"), "required is not available on []string"),
		errAt(mCompile, ErrSchema, path("origins"), "default is not available on []string"),
		errAt(mCompile, ErrSchema, path("origins"), "[]string maps no address"), // the consequence, wrongly reported
	)
	t3 := &fakeT{}
	CheckErrors(t3, overreported,
		Want{path("origins"), ErrSchema},
		Want{path("origins"), ErrSchema},
	)
	fmt.Printf("\n  a suppression rule firing once too often:\n  %s\n", strings.ReplaceAll(t3.msgs[0], "\n", "\n  "))
	containsPasses := errors.Is(overreported, ErrSchema)
	fmt.Printf("\n  what a contains-assertion would have said: errors.Is(...) = %v\n", containsPasses)
	fmt.Println("  which is why the assertion is over the SET and not over one element.")
}

// ---------------------------------------------------------------------------
// E13 the audit
// ---------------------------------------------------------------------------

type E13Bad struct {
	Origins []string `ferry:"origins,required,default=v"`
	Name    string   `ferry:"name"`
}

func runE13() {
	hdr("E13 the audit: the fixture shapes the other probes do not contain")

	// (a) ONE FIELD tripping TWO rules. Every other probe here has one error
	// per field, which is exactly the shape the handoff warned about.
	_, err := compileD(reflect.TypeFor[E13Bad]())
	elems := Elements(err)
	fmt.Printf("  (a) one field, two inadmissible options: %d errors\n", len(elems))
	for _, e := range elems {
		fmt.Printf("      %v\n", e)
	}
	fmt.Println("      admissibility is checked before contradictions, so `required and")
	fmt.Println("      default contradict` is NOT reported: neither survived tier two.")

	// (b) an aggregate where every element is one class. The risk is a report
	// that reads as though it found variety, and an errors.Is that says yes to
	// everything because some element somewhere matched.
	same := join(
		errAt(mWalk, ErrMissing, path("a"), "required, and the plane supplied nothing"),
		errAt(mWalk, ErrMissing, path("b"), "required, and the plane supplied nothing"),
		errAt(mWalk, ErrMissing, path("c"), "required, and the plane supplied nothing"),
	)
	fmt.Printf("\n  (b) three errors, all one class: %v\n", same)
	fmt.Printf("      Is(Missing)=%v  Is(Value)=%v  Is(Schema)=%v  Is(Plane)=%v\n",
		errors.Is(same, ErrMissing), errors.Is(same, ErrValue), errors.Is(same, ErrSchema), errors.Is(same, ErrPlane))

	// (c) the moment key: which moments can actually coexist in one aggregate?
	// Schema compile failing means no walk runs, so a compile error and a walk
	// error can never share an aggregate. Worth stating, because it bounds what
	// the first term of the sort key ever has to order.
	fmt.Println("\n  (c) which moments can share one aggregate:")
	fmt.Println("      compile fails -> no walk, so compile never coexists with walk")
	fmt.Println("      register fails -> no schema, so register never coexists with either")
	fmt.Println("      open/walk/commit/close DO coexist, which is E4's case")

	// (d) a location-less element inside a walk aggregate: a cancellation.
	cancelled := join(
		errAt(mWalk, ErrValue, path("a"), "is not a valid int"),
		errAt(mWalk, ErrValue, path("b"), "is not a valid int"),
		&Error{mom: mWalk, msg: "the load was cancelled", cause: context_Canceled},
	)
	fmt.Printf("\n  (d) a cancellation inside a walk aggregate:\n%+v\n", cancelled)
	fmt.Printf("      errors.Is(err, context.Canceled) = %v, and no ferry class matches\n",
		errors.Is(cancelled, context_Canceled))
	fmt.Println("      It sorts first within its moment, which reads correctly: it is why")
	fmt.Println("      there are two errors and not twelve.")
}

// ---------------------------------------------------------------------------
// E14
// ---------------------------------------------------------------------------

type E14Conf struct {
	Host string
	Port int
}

// LoadE is the shape question 10 of the grill settled: on failure ferry yields
// NO VALUE. The partial exists inside the walk - aggregating requires it - and
// it does not cross the boundary.
func LoadE(plane map[Path]Value) (E14Conf, error) {
	s, err := compileD(reflect.TypeFor[E14Conf]())
	if err != nil {
		var zero E14Conf
		return zero, err
	}
	var v E14Conf
	sink := &errSink{}
	_, _ = loadD(plane, s, reflect.ValueOf(&v).Elem(), loadOpts{sink: sink})
	if err := sink.result(); err != nil {
		var zero E14Conf
		return zero, err // the partial v is discarded HERE, deliberately
	}
	return v, nil
}

func runE14() {
	hdr("E14 what Load hands back when it fails")

	plane := map[Path]Value{addr("Host"): String("db1"), addr("Port"): String("not-a-port")}

	// What the walk actually built, which is what "unspecified" would hand back.
	s := mustSchema(reflect.TypeFor[E14Conf]())
	var partial E14Conf
	sink := &errSink{}
	_, _ = loadD(plane, s, reflect.ValueOf(&partial).Elem(), loadOpts{sink: sink})
	fmt.Printf("  the partial the walk built  : %+v\n", partial)

	got, err := LoadE(plane)
	fmt.Printf("  what LoadE returns          : %+v\n", got)
	fmt.Printf("  err                         : %v\n", err)
	fmt.Println("\n  Ignoring the error gives a wholly zero value, which fails immediately.")
	fmt.Println("  Handing back the partial gives a process that STARTS with Host set and")
	fmt.Println("  Port zero, which for a config loader is the worst outcome available.")
	fmt.Println("  The survey says document it; discarding it means there is nothing to")
	fmt.Println("  document, because the state it warns about is not observable.")
}

// ---------------------------------------------------------------------------
// E15
// ---------------------------------------------------------------------------

// A driver's own error vocabulary, which ferry knows nothing about.
var errYAMLSyntax = errorsNew("yaml: line 3: mapping values are not allowed in this context")

func runE15() {
	hdr("E15 a driver holding an opinion about the class")

	// (a) a plain driver error: core supplies the default for the moment.
	plain := fromDriver(mOpen, Path{}, false, errorsNew("open /etc/app.yaml: no such file or directory"))
	fmt.Printf("  (a) plain error at Open   : %v\n", plain)
	fmt.Printf("      Plane=%v Value=%v Driver=%v\n", errors.Is(plain, ErrPlane), errors.Is(plain, ErrValue), errors.Is(plain, ErrDriver))

	// (b) the driver declares Value: it is the operator's FILE, not the
	// infrastructure. This is ADR-0001's 5.11 given somewhere honest to land.
	declared := fromDriver(mOpen, Path{}, false, fmt.Errorf("%w: %w", ErrValue, errYAMLSyntax))
	fmt.Printf("\n  (b) driver declares Value : %v\n", declared)
	fmt.Printf("      Plane=%v Value=%v Driver=%v\n", errors.Is(declared, ErrPlane), errors.Is(declared, ErrValue), errors.Is(declared, ErrDriver))

	// (c) and the driver's own error is reachable through ferry's wrapper, so a
	// caller can match the driver's vocabulary without ferry knowing it exists.
	fmt.Printf("\n  (c) the driver's own sentinel, through ferry's wrapper: %v\n",
		errors.Is(declared, errYAMLSyntax))
	fmt.Println("      ferry never had to know that sentinel existed.")

	// (d) what a driver CANNOT do.
	forged := fromDriver(mWalk, path("db", "host"), true, fmt.Errorf("%w: I am a schema error", ErrSchema))
	fmt.Printf("\n  (d) a driver claiming Schema: %v\n", forged)
	fmt.Printf("      Schema=%v  <- nothing stops it, and it is a conformance case\n", errors.Is(forged, ErrSchema))
	fmt.Printf("      Driver=%v  <- provenance is core's, and it cannot be forged\n", errors.Is(forged, ErrDriver))
	fmt.Printf("      address=%s <- core's, so a Get failure cannot be misattributed\n", forged.Address())
}

// context.Canceled without the import, so this file stays dependency-free.
var context_Canceled = errorsNew("context canceled")

var _ = testing.AllocsPerRun
