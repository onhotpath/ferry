package main

// X4, X5, X7, X9: `required` at a composite, the array bound, the harness
// through the engine, and concurrency.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

// --- X4 -----------------------------------------------------------------------

type X2Cred struct {
	User string `ferry:"user"`
	Pass string `ferry:"pass"`
}

type X2ReqPtr struct {
	Auth *X2Cred `ferry:"auth,required"`
}

type X2ReqStruct struct {
	Auth X2Cred `ferry:"auth,required"`
}

// X2ReqBoth is ADR-0011's own suppression fixture: a required child that is
// absent, under a required parent.
type X2ReqChild struct {
	User string `ferry:"user,required"`
	Pass string `ferry:"pass"`
}

type X2ReqBoth struct {
	Auth *X2ReqChild `ferry:"auth,required"`
}

// X2ReqPresent is the neighbouring case, which needs nothing: a child that is
// PRESENT and fails to decode.
type X2ReqNum struct {
	User int `ferry:"user"`
}

type X2ReqPresent struct {
	Auth *X2ReqNum `ferry:"auth,required"`
}

func runX2d() {
	ctx := context.Background()
	saysX2("ADR-0006", `"At a composite it means the plane supplied at least one of the address's
	static children."
	and, on the defect it records repairing in its own draft:
	"required on a non-pointer struct was accepted at schema compile and
	enforced by nothing... It now means the same thing it means on *struct,
	measured: ferry: /auth: required, and the plane supplied nothing under it"`)

	empty := map[Path]Value{}
	got1, e1 := Load[X2ReqPtr](ctx, x2FixedSource{empty})
	fmt.Printf("  *struct, empty plane  -> %+v err=%v\n", got1, e1)
	got2, e2 := Load[X2ReqStruct](ctx, x2FixedSource{empty})
	fmt.Printf("   struct, empty plane  -> %+v err=%v\n", got2, e2)
	fmt.Println("  BEFORE (#41 D12): both err=<nil>. applyOptions set node.required on a")
	fmt.Println("  struct and on a pointer node, and e_walk.go read n.required in exactly")
	fmt.Println("  one place, direction.leaf.")

	fmt.Println("\n  ADR-0006's own table, run through the entry point:")
	for _, tc := range []struct {
		label string
		vals  map[Path]Value
	}{
		{"key absent", empty},
		{"auth: {}", map[Path]Value{}},
		{"auth: {user: u}", map[Path]Value{Path{}.Name("auth").Name("user"): String("u")}},
		{"auth: {pass: p}", map[Path]Value{Path{}.Name("auth").Name("pass"): String("p")}},
		{"auth: null", map[Path]Value{Path{}.Name("auth"): Null()}},
	} {
		g, e := Load[X2ReqPtr](ctx, x2FixedSource{tc.vals})
		verdict := "satisfied"
		if e != nil {
			verdict = "refused"
		}
		fmt.Printf("    %-18s %-10s %v\n", tc.label, verdict, x2ShowPtr(g.Auth))
	}

	saysX2("ADR-0011", `"a composite's required failure is suppressed when a child under it
	already reported", because "the parent's check is the child's summary, so
	it is ADR-0008's tier rule at the walk".
	And: "The neighbouring case needs nothing... A child that is PRESENT and
	fails to decode already sets ADR-0006's presence bit, so the parent's
	required does not fire."`)

	_, e3 := Load[X2ReqBoth](ctx, x2FixedSource{empty})
	fmt.Printf("  a required child that is absent, under a required parent:\n")
	fmt.Printf("    elements=%d  %v\n", len(Elements(e3)), e3)
	for _, e := range Elements(e3) {
		fmt.Printf("      %s\n", e)
	}
	fmt.Println("    ^ ONE error and one remediation. Without the suppression bit this is")
	fmt.Println("      two errors - /auth and /auth/user - that setting AUTH_USER clears")
	fmt.Println("      both of.")

	_, e4 := Load[X2ReqPresent](ctx, x2FixedSource{
		map[Path]Value{Path{}.Name("auth").Name("user"): String("not-a-number")}})
	fmt.Printf("\n  a required subtree whose only present child fails to decode:\n")
	fmt.Printf("    elements=%d  %v\n", len(Elements(e4)), e4)
	fmt.Println("    ^ the parent's required did not fire, because the child was PRESENT.")

	fmt.Println("\n  and the #14/#15/#10 case that must keep working: a required child")
	fmt.Println("  under an ABSENT OPTIONAL section is not a failure of its own.")
	type X2OptSection struct {
		Auth *X2ReqChild `ferry:"auth"`
	}
	g5, e5 := Load[X2OptSection](ctx, x2FixedSource{empty})
	fmt.Printf("    optional section, empty plane -> Auth=%v err=%v\n", x2ShowPtr(g5.Auth), e5)

	fmt.Println("\n  required is Load's alone. On Dump the plane is the thing being")
	fmt.Println("  written, so the assertion has nothing to be about:")
	st := NewStore()
	fmt.Printf("    Dump of a zero X2ReqPtr -> %v\n", Dump(ctx, X2ReqPtr{}, BKVSink{Store: st}))

	fmt.Println("\n  read as a set, which is ADR-0001's route (b) and #14's consumer:")
	_, e6 := Load[X2ReqBoth](ctx, x2FixedSource{empty})
	var req []Path
	for _, e := range Elements(e6) {
		if errors.Is(e, ErrMissing) {
			var fe *Error
			if errors.As(e, &fe) {
				req = append(req, fe.Address())
			}
		}
	}
	fmt.Printf("    addresses the user must fill in: %v\n", req)
}

func x2ShowPtr[T any](p *T) string {
	if p == nil {
		return "<nil>"
	}
	return fmt.Sprintf("&%+v", *p)
}

// --- X5 -----------------------------------------------------------------------

type X2Arr struct {
	V [3]string `ferry:"v"`
}

func runX2e() {
	ctx := context.Background()
	saysX2("ADR-0005", `"Measured: [3]string given only index 0 loads ["a", "", ""], and given
	index 7 returns ferry: /V: plane has index 7, [3]string holds 3."
	An array's length is the TYPE's, not the plane's.`)

	for _, tc := range []struct {
		label string
		vals  map[Path]Value
	}{
		{"plane holds only #0", map[Path]Value{Path{}.Name("v").Index(0): String("a")}},
		{"plane holds only #7", map[Path]Value{Path{}.Name("v").Index(7): String("a")}},
		{"plane holds #0 and #7", map[Path]Value{
			Path{}.Name("v").Index(0): String("a"),
			Path{}.Name("v").Index(7): String("b")}},
		{"plane holds #0..#2", map[Path]Value{
			Path{}.Name("v").Index(0): String("a"),
			Path{}.Name("v").Index(1): String("b"),
			Path{}.Name("v").Index(2): String("c")}},
	} {
		g, e := Load[X2Arr](ctx, x2FixedSource{tc.vals})
		fmt.Printf("  %-22s -> %v  err=%v\n", tc.label, g.V, e)
	}
	fmt.Println("\n  BEFORE (#41 D13): index 7 loaded [\"\" \"\" \"\"] with a nil error. The")
	fmt.Println("  check existed at walk.go:333 on the walk the engine no longer calls, and")
	fmt.Println("  e_walk.go's nArray case iterated n.n static addresses and never")
	fmt.Println("  enumerated, so an index outside the array was not read and not reported.")

	fmt.Println("\n  the class, so a caller can match it without reading the message:")
	_, e := Load[X2Arr](ctx, x2FixedSource{map[Path]Value{Path{}.Name("v").Index(7): String("a")}})
	fmt.Printf("    errors.Is(err, ErrValue)=%v  Address()=%v\n", errors.Is(e, ErrValue), tAddress(e))

	fmt.Println("\n  a source with no Enumerator cannot report an index at all, so there")
	fmt.Println("  is no index to be out of range and the static addresses still load:")
	g, e2 := Load[X2Arr](ctx, x2NoEnumSource{map[Path]Value{Path{}.Name("v").Index(0): String("a")}})
	fmt.Printf("    %v err=%v\n", g.V, e2)
}

type x2NoEnumSource struct{ vals map[Path]Value }

func (s x2NoEnumSource) Bind(*AddressSet) (FOpenFunc, error) {
	return func(context.Context) (FReader, error) { return x2NoEnumReader{s.vals}, nil }, nil
}

type x2NoEnumReader struct{ vals map[Path]Value }

func (r x2NoEnumReader) Get(_ context.Context, p Path) (Value, error) { return r.vals[p], nil }

// --- X7 -----------------------------------------------------------------------

func runX2g() {
	saysX2("ADR-0005", `"CI runs the same table against three planes, and the third one is the
	point." Measured there: the memory plane 11 of 11 core types and 10 of 10
	composites; the YAML driver 11 of 11 and 10 of 10; the flattening plane
	11 of 11 core types and 8 of 10 composites.
	And: "A driver declares the Value kinds its plane can carry. The
	conformance suite runs the proofs that plane can express, and asserts that
	the ones it cannot are refused loudly rather than silently mangled."`)

	fmt.Println("  This harness called dump() and load() from walk.go, the SUPERSEDED")
	fmt.Println("  walk, so ADR-0005's numbers are statements about code the engine no")
	fmt.Println("  longer uses. It now runs through Dump and Load.")

	dir, _ := os.MkdirTemp("", "x2h")
	defer os.RemoveAll(dir)
	fmt.Printf("\n  %-40s %-24s %s\n", "plane", "core (11 proofs)", "composites (10 proofs)")
	for _, pl := range []Plane{memoryPlane(), yamlPlane(dir), flatPlane()} {
		c := RoundTrip(pl, CoreTypes()...)
		k := RoundTrip(pl, auditSet()...)
		fmt.Printf("  %-40s %-24s %s\n", pl.Name,
			fmt.Sprintf("%d/%d, %d refused", c.Pass, c.Pass+c.Fail, c.Refused),
			fmt.Sprintf("%d/%d, %d refused", k.Pass, k.Pass+k.Fail, k.Refused))
	}

	fmt.Println("\n  The flattening plane's refusals, per value, which is where ADR-0005's")
	fmt.Println("  published numbers move:")
	fp := flatPlane()
	for _, set := range [][]Proof{CoreTypes(), auditSet()} {
		for _, pr := range set {
			_, ref := pr.run(fp)
			if len(ref) > 0 {
				fmt.Printf("    %-22s %v\n", pr.Name(), ref)
			}
		}
	}
	fmt.Println("\n  Every one of them is a nil or empty composite, which dumpDir writes as")
	fmt.Println("  Null at the container address, and a flat plane has no null. ADR-0005")
	fmt.Println("  named only *int and *Cred, because the plane it measured was a map")
	fmt.Println("  transform that mapped Null onto String(\"\") - which is precisely the")
	fmt.Println("  SILENT MANGLING its own declaration rule exists to prevent.")

	fmt.Println("\n  the declaration, and that a plane contradicting it is a FAILURE and")
	fmt.Println("  not a refusal:")
	lying := Plane{Name: "a flat plane that claims it carries null", Kinds: allKinds, Open: flatPlane().Open}
	r := RoundTrip(lying, Type("*int", PtrEq(Eq[int]), nil, ptr(3)))
	for _, l := range r.Lines {
		fmt.Println("   " + l)
	}

	fmt.Println("\n  FOUND BY THIS ITEM, and it is not a fix: the harness's own")
	fmt.Println("  falsification check is now partly vacuous.")
	fmt.Println("    ADR-0010's schema cache memoises the RESOLVED codec, and ADR-0007's")
	fmt.Println("    identity step runs at compile. So swapping an identity-table codec")
	fmt.Println("    after a schema for that type has been compiled has no effect through")
	fmt.Println("    the entry point, where the superseded walk resolved per call.")
	x2CodecSwapCheck()
}

func x2CodecSwapCheck() {
	// float64 is admitted BY KIND and is not in the identity table, so its
	// encode falls through to a call-time lookup and the swap still bites.
	fmt.Printf("    float64 (kind-admitted)     before=%d after-swap=%d\n",
		len(failsOn(Type("float64", BitEq[float64], 1.0/3.0), memoryPlane())), func() int {
			restore := breakFloat64()
			defer restore()
			return len(failsOn(Type("float64", BitEq[float64], 1.0/3.0), memoryPlane()))
		}())

	// time.Duration IS in the identity table, so its codec is baked into the
	// cached schema and the swap is invisible.
	t := reflect.TypeFor[time.Duration]()
	before := len(failsOn(Type("time.Duration", Eq[time.Duration], time.Second), memoryPlane()))
	good := byIdentity[t]
	byIdentity[t] = leafCodec{
		name: "lossy: whole seconds only",
		enc:  func(v reflect.Value) (Value, error) { return String(time.Duration(v.Int()).Truncate(time.Minute).String()), nil },
		dec:  good.dec,
	}
	after := len(failsOn(Type("time.Duration", Eq[time.Duration], 90*time.Second), memoryPlane()))
	byIdentity[t] = good
	fmt.Printf("    time.Duration (identity)    before=%d after-swap=%d   <- the swap is INVISIBLE\n", before, after)
	fmt.Println("    That is ADR-0009's freeze and ADR-0010's cache working as decided,")
	fmt.Println("    and it means timecost.go's and audit2.go's codec-swap probes now")
	fmt.Println("    measure nothing. Reported, not fixed: the repair is a fresh Registry")
	fmt.Println("    per RoundTrip, and R10 records that ferrytest needs no Registry.")
}

// --- X9 -----------------------------------------------------------------------

type X2ConcConf struct {
	Name string `ferry:"name"`
	Port int    `ferry:"port"`
	Host string `ferry:"host"`
}

func runX2i() {
	ctx := context.Background()
	saysX2("ADR-0010 / ADR-0012", `ADR-0010 reports a data race on the presence bit under a
	goroutine-per-task scheduler. ADR-0012 reports 64 concurrent loads through
	one binding clean under -race. Both change meaning now that the default
	scheduler is the aggregating one rather than serial.`)

	vals := map[Path]Value{
		Path{}.Name("name"): String("svc"),
		Path{}.Name("port"): Number("8080"),
		Path{}.Name("host"): String("db1"),
	}

	fmt.Println("  ADR-0012's claim, re-run under the new default: 64 concurrent loads")
	fmt.Println("  through ONE binding.")
	b, err := Bind[X2ConcConf](x2FixedSource{vals})
	if err != nil {
		fmt.Println("    bind:", err)
		return
	}
	var wg sync.WaitGroup
	results := make([]X2ConcConf, 64)
	errs := make([]error, 64)
	for i := range 64 {
		wg.Go(func() { results[i], errs[i] = b.Load(ctx) })
	}
	wg.Wait()
	distinct := map[string]int{}
	bad := 0
	for i := range 64 {
		if errs[i] != nil {
			bad++
		}
		distinct[fmt.Sprintf("%+v", results[i])]++
	}
	fmt.Printf("    64 loads: %d distinct results, %d errors\n", len(distinct), bad)
	for k, v := range distinct {
		fmt.Printf("      %s x%d\n", k, v)
	}

	fmt.Println("\n  and 64 concurrent loads that all FAIL, which is the case the default")
	fmt.Println("  change creates: every one now builds an aggregate rather than")
	fmt.Println("  returning the first error.")
	bb, _ := Bind[X2Five](x2FixedSource{x2BadFive()})
	reports := make([]string, 64)
	var wg2 sync.WaitGroup
	for i := range 64 {
		wg2.Go(func() {
			_, e := bb.Load(ctx)
			reports[i] = fmt.Sprintf("%+v", e)
		})
	}
	wg2.Wait()
	seen := map[string]bool{}
	for _, r := range reports {
		seen[r] = true
	}
	fmt.Printf("    64 failing loads: %d distinct %%+v renderings\n", len(seen))
	fmt.Println("    Sorting AT CONSTRUCTION is what makes that 1: nothing is computed on")
	fmt.Println("    first print, so there is no lazy state to race on.")

	fmt.Println("\n  ADR-0010's presence-bit race, re-run. The bit is written by every")
	fmt.Println("  task in the nStruct and nArray arms, so a goroutine-per-task")
	fmt.Println("  scheduler races it. The DEFAULT is still single-goroutine, and that")
	fmt.Println("  is what makes it safe rather than any change to the walk.")
	fmt.Printf("    default scheduler is concurrent: %v\n", false)
	_, e := Load[X2ConcConf](ctx, x2FixedSource{vals}, WithSched(x2Parallel))
	fmt.Printf("    goroutine-per-task scheduler, one load -> err=%v\n", e)
	fmt.Println("    ^ run this probe under -race to see ADR-0010's finding; it is")
	fmt.Println("      unchanged by the default moving, because `aggregating` runs its")
	fmt.Println("      tasks in sequence exactly as `serial` did.")

	fmt.Println("\n  what a failing load costs, aggregating against first-error:")
	for _, r := range []struct {
		name string
		opts []Option
	}{{"aggregating (default)", nil}, {"serial", []Option{WithSched(serial)}}} {
		res := testing.Benchmark(func(bench *testing.B) {
			bench.ReportAllocs()
			for bench.Loop() {
				_, _ = Load[X2Five](ctx, x2FixedSource{x2BadFive()}, r.opts...)
			}
		})
		fmt.Printf("    %-24s %10d ns/op %8d B/op %6d allocs/op\n",
			r.name, res.NsPerOp(), res.AllocedBytesPerOp(), res.AllocsPerOp())
	}
}

// x2Parallel is ADR-0010's goroutine-per-task scheduler, kept here only so the
// presence-bit race stays reachable under -race.
func x2Parallel(tasks []func() error) error {
	var wg sync.WaitGroup
	errs := make([]error, len(tasks))
	for i, t := range tasks {
		wg.Go(func() { errs[i] = t() })
	}
	wg.Wait()
	return join(errs...)
}
