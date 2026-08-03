package main

// Q2: should ADR-0003's segment-wise rule extend to the `Get` sequence?
//
// The ordering is NOT changed here. These probes establish what can observe a
// `Get` sequence, so the question is decided on consequences rather than on
// reading the sentence.
//
// The fixtures are X2's eight shapes, reused for the same reason X4's Dump
// probes reuse X2Eight.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// x4Shapes is the fixture set X2=10 records the Get sequence over, plus the two
// dynamic shapes it does not.
func x4Shapes() []struct {
	name string
	get  func(context.Context) []Path
} {
	return []struct {
		name string
		get  func(context.Context) []Path
	}{
		{"flat, 3 fields", func(c context.Context) []Path { return x2GetOrder(c, X2OFlat{"z", "a", "m"}) }},
		{"nested structs", func(c context.Context) []Path {
			return x2GetOrder(c, X2ONested{"z", X2OInner{"y", "a"}, "a"})
		}},
		{"slice + leaf", func(c context.Context) []Path {
			return x2GetOrder(c, X2OSlice{"z", []string{"s0", "s1"}, "a"})
		}},
		{"array + leaf", func(c context.Context) []Path {
			return x2GetOrder(c, X2OArray{"z", [2]string{"a0", "a1"}, "a"})
		}},
		{"pointer + leaf", func(c context.Context) []Path {
			return x2GetOrder(c, X2OPtr{"z", &X2OInner{"y", "a"}, "a"})
		}},
		{"promoted embedded", func(c context.Context) []Path {
			return x2GetOrder(c, X2OPromoted{"z", X2Common{"e", "n"}, "a"})
		}},
		{"map of structs", func(c context.Context) []Path {
			return x2GetOrder(c, X2OMapStruct{"z", map[string]X2OInner{"m2": {"y", "a"}, "m1": {"y", "a"}}, "a"})
		}},
		{"two levels", func(c context.Context) []Path {
			return x2GetOrder(c, X2OTwoLevel{"z", X2ONested{"z", X2OInner{"y", "a"}, "a"}, "a", &X2OInner{"y", "a"}})
		}},
	}
}

// --- X6: the presence observation ---------------------------------------------

func runX4f() {
	ctx := context.Background()
	saysX4("ADR-0006 / ADR-0012", `"The presence observation survives the walk", and ADR-0012 decided its
	spelling: "One Source wrapping another observes every boundary Value the
	load saw". ADR-0006 calls the result a report a user reads.`)
	saysX4("ADR-0001", `"every map iteration reaching a user-visible artefact" is covered by the
	determinism invariant.`)

	dir, _ := os.MkdirTemp("", "x4")
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "app.yaml")
	os.WriteFile(p, []byte("name: svc\ndb:\n  host: h\n  port: 5432\n"), 0o644)

	fmt.Println("  (1) THE RAW Observe CALLBACK, exactly as a user would collect it.")
	fmt.Println("      Nothing between the walk and the callback sorts anything.")
	var raw []Path
	_, _ = Load[B5Conf](ctx, FYAMLSource{Path: p}, Observe(func(a Path, _ Value) { raw = append(raw, a) }))
	fmt.Printf("      as delivered   : %v\n", raw)
	fmt.Printf("      segment-wise   : %v\n", sortedPaths(slices.Clone(raw)))
	fmt.Printf("      already sorted : %v\n", x2IsSorted(raw))
	fmt.Println()
	fmt.Println("      B5b prints this sequence through sortedPaths() and T9 through a")
	fmt.Println("      recorder whose All() sorts, so both existing probes HIDE the raw")
	fmt.Println("      order. The raw order is what the API hands over.")

	fmt.Println("\n  (2) IS IT DETERMINISTIC? That, and not sortedness, is what ADR-0001's")
	fmt.Println("      invariant actually requires. 200 runs, over the eight shapes,")
	fmt.Println("      counting DISTINCT Get sequences:")
	for _, sh := range x4Shapes() {
		seqs := map[string]int{}
		for range 200 {
			seqs[fmt.Sprint(sh.get(ctx))]++
		}
		fmt.Printf("      %-20s %d distinct sequence(s) over 200 runs   segment-wise=%v\n",
			sh.name, len(seqs), x2IsSorted(sh.get(ctx)))
	}
	fmt.Println()
	fmt.Println("      Every shape is single-valued, including `map of structs`, whose")
	fmt.Println("      members come from Enumerator.Children. The memory plane's")
	fmt.Println("      Children sorts (walk.go:272), so no map iteration reaches the")
	fmt.Println("      sequence. ADR-0001's invariant is SATISFIED as things stand, and")
	fmt.Println("      it is satisfied by determinism rather than by sortedness - the")
	fmt.Println("      two are different properties and ADR-0001 only asks for the first.")

	fmt.Println("\n  (3) BUT A DRIVER'S OWN Children ORDER IS THE DRIVER'S, and the walk")
	fmt.Println("      does not sort it. yaml enumerates a mapping in DOCUMENT order:")
	for _, body := range []string{
		"m:\n  zeta: 1\n  alpha: 2\n  mid: 3\n",
		"m:\n  alpha: 2\n  mid: 3\n  zeta: 1\n",
	} {
		os.WriteFile(p, []byte(body), 0o644)
		var seq []Path
		_, _ = Load[X4MapConf](ctx, FYAMLSource{Path: p}, Observe(func(a Path, _ Value) { seq = append(seq, a) }))
		fmt.Printf("      document %-28q -> %v\n", strings.ReplaceAll(strings.TrimSpace(body), "\n", " "), seq)
	}
	fmt.Println("      Deterministic per document, and not segment-wise. So even if the")
	fmt.Println("      walk sorted its struct fields, the dynamic half of a Get sequence")
	fmt.Println("      would still be whatever the driver returns, and core cannot fix")
	fmt.Println("      that without sorting Children - which is a change to the DRIVER")
	fmt.Println("      contract, not to the walk.")

	fmt.Println("\n  (4) SO IS THE OBSERVATION A USER-VISIBLE ARTEFACT?")
	fmt.Println("      Two halves, and they answer differently.")
	os.WriteFile(p, []byte("name: svc\ndb:\n  host: h\n  port: 5432\n"), 0o644)
	rec := NewRecord()
	_, _ = Load[B5Conf](ctx, BObserving{FYAMLSource{Path: p}, rec})
	fmt.Printf("      the VALUES, keyed by address : %v\n", rec.All())
	fmt.Println("      -> a map from address to Value. Order-free. This is what ADR-0006")
	fmt.Println("         is about - `was /db/port set, empty or missing` - and it is a")
	fmt.Println("         lookup, so the Get order cannot reach it.")
	fmt.Println("      the SEQUENCE                 : the callback's argument order")
	fmt.Println("      -> user-visible only if a user appends rather than indexes. ferry")
	fmt.Println("         ships no report that prints it, and BRecord.All() - the")
	fmt.Println("         recorder shape ADR-0012 endorses - sorts on the way out.")
	fmt.Println()
	fmt.Println("      MEASURED CONCLUSION: the observation as ADR-0006 defines it is a")
	fmt.Println("      MAPPING and is order-free. The sequence is visible to a user who")
	fmt.Println("      chooses to keep it, in the same way a driver's Get order is")
	fmt.Println("      visible to the driver. Neither is an artefact ferry produces.")
}

// X4MapConf is the dynamic shape: a map whose member order comes from the
// driver's Children, not from the walk.
type X4MapConf struct {
	M map[string]int `ferry:"m"`
}

// --- X7: a lazy driver's backend call order ------------------------------------

func runX4g() {
	ctx := context.Background()
	saysX4("ADR-0004", `"Batch versus lazy is a branch inside one driver. Bind already handed
	over the address set, so OpenFunc can fetch everything in one round trip
	or fetch nothing at all, and ferry never needs to know which."`)

	fmt.Println("  A lazy driver makes one round trip per Get, so its backend call order")
	fmt.Println("  IS the Get order. Measured, with the call log the driver would emit:")
	var log []string
	n := 0
	_, _ = Load[X2ONested](ctx, x4LoggingSource{&log, &n})
	for _, l := range log {
		fmt.Printf("    %s\n", l)
	}
	fmt.Printf("    %d round trips, in walk order, segment-wise=%v\n", n,
		x2IsSorted(x4PathsFromLog(log)))

	fmt.Println("\n  WHO OBSERVES IT. Enumerated against the codebase rather than argued:")
	fmt.Println("    - the driver itself      : yes, it is the one making the calls.")
	fmt.Println("    - ferry core             : no. The walk calls Get and keeps the")
	fmt.Println("                               Value; the sequence is not retained")
	fmt.Println("                               anywhere. grep for a []Path recording a")
	fmt.Println("                               Get in an engine file returns nothing.")
	fmt.Println("    - the conformance suite  : no. harness.go compares VALUES at")
	fmt.Println("                               addresses; RoundTrip has no sequence")
	fmt.Println("                               assertion. Measured below.")
	fmt.Println("    - a trace or a log       : the DRIVER's, and a driver is free to")
	fmt.Println("                               sort its own spans. ADR-0004 already")
	fmt.Println("                               says a driver may batch, in which case")
	fmt.Println("                               there is no per-Get span to order.")
	fmt.Println()
	fmt.Println("  And the batch branch of the same driver has NO per-Get backend call at")
	fmt.Println("  all, so on the ADR's own preferred shape the Get order is invisible")
	fmt.Println("  even to the driver:")
	nb := 0
	_, _ = Load[X4Three](ctx, x4LazySource{batch: true, calls: &nb})
	fmt.Printf("    batch driver, 3 addresses -> %d backend call(s), 0 ordered by ferry\n", nb)

	fmt.Println("\n  The one place a sorted Get sequence WOULD be worth something: a lazy")
	fmt.Println("  driver over a range-scannable plane could exploit locality if ferry")
	fmt.Println("  promised sorted Gets. Measured, that promise is not available to it")
	fmt.Println("  anyway, because the dynamic half of any schema is enumerated by the")
	fmt.Println("  driver itself (X6 (3)), so ferry could only promise sortedness for")
	fmt.Println("  the static half - which is exactly the half a driver can already")
	fmt.Println("  sort for itself, since Bind handed it the whole static address set:")
	s := mustSchema[X2ONested]()
	fmt.Printf("    the static set handed to Bind, sorted by ferry already: %v\n", s.as.All())
}

type x4LoggingSource struct {
	log *[]string
	n   *int
}

func (s x4LoggingSource) Bind(*AddressSet) (FOpenFunc, error) {
	return func(context.Context) (FReader, error) { return x4LoggingReader(s), nil }, nil
}

type x4LoggingReader struct {
	log *[]string
	n   *int
}

func (r x4LoggingReader) Get(_ context.Context, p Path) (Value, error) {
	*r.n++
	*r.log = append(*r.log, fmt.Sprintf("GET %s   (round trip %d)", p, *r.n))
	return String("v"), nil
}

func x4PathsFromLog(log []string) []Path {
	out := make([]Path, 0, len(log))
	for _, l := range log {
		f := strings.Fields(l)
		out = append(out, Path{f[1]})
	}
	return out
}

// --- X8: the error set, verified rather than assumed ---------------------------

// X4ErrFwd and X4ErrRev hold the SAME five addresses in opposite field order,
// so they produce the same error set through two different Get sequences.
type X4ErrFwd struct {
	Zeta  int `ferry:"zeta"`
	Mid   int `ferry:"mid"`
	Alpha int `ferry:"alpha"`
	Beta  int `ferry:"beta"`
	Yank  int `ferry:"yankee"`
}

type X4ErrRev struct {
	Yank  int `ferry:"yankee"`
	Beta  int `ferry:"beta"`
	Alpha int `ferry:"alpha"`
	Mid   int `ferry:"mid"`
	Zeta  int `ferry:"zeta"`
}

func runX4h() {
	ctx := context.Background()
	saysX4("ADR-0011", `"Ordering is not the walk's, because sorting happens when the aggregate
	is constructed, so the walk may emit in any order."
	and ADR-0003: error reports are one of the three places that sort
	segment-wise.`)

	bad := map[Path]Value{}
	for _, k := range []string{"zeta", "mid", "alpha", "beta", "yankee"} {
		bad[Path{}.Name(k)] = String("not-a-number")
	}

	var seqF, seqR []Path
	_, errF := Load[X4ErrFwd](ctx, x2OrderSource{bad, &seqF})
	_, errR := Load[X4ErrRev](ctx, x2OrderSource{bad, &seqR})

	fmt.Printf("  field order A, Get sequence : %v\n", seqF)
	fmt.Printf("  field order B, Get sequence : %v\n", seqR)
	fmt.Printf("  the two Get sequences agree : %v\n", fmt.Sprint(seqF) == fmt.Sprint(seqR))
	fmt.Println()
	fmt.Println("  the error report, field order A:")
	x4PrintErr(errF)
	fmt.Println("  the error report, field order B:")
	x4PrintErr(errR)
	fmt.Printf("\n  reports identical           : %v\n", x4ErrSeq(errF) == x4ErrSeq(errR))
	fmt.Printf("  reported segment-wise       : %v\n", x2IsSorted(x4ErrAddrs(errF)))
	fmt.Println()
	fmt.Println("  CONFIRMED, not assumed. Two Get sequences that are reverses of each")
	fmt.Println("  other produce one error report, in segment-wise order, because")
	fmt.Println("  join() sorts at construction on the three-part key")
	fmt.Println("  (moment, location, message) and the location leg is")
	fmt.Println("  CompareSegmentwise. ADR-0003's `error reports` place is therefore")
	fmt.Println("  covered no matter what the Get sequence is.")

	fmt.Println("\n  The same, with the errors ARRIVING in 200 shuffled orders, which is")
	fmt.Println("  what a concurrent walk would do (#20). Distinct rendered reports:")
	els := Elements(errF)
	seen := map[string]int{}
	for range 200 {
		sh := slices.Clone(els)
		x4Shuffle(sh)
		seen[x4ErrSeq(join(sh...))]++
	}
	fmt.Printf("    %d distinct report(s) over 200 shuffles\n", len(seen))
}

func x4PrintErr(err error) {
	for _, e := range Elements(err) {
		fmt.Printf("    %s\n", e)
	}
}

func x4ErrSeq(err error) string {
	var b strings.Builder
	for _, e := range Elements(err) {
		b.WriteString(e.Error())
		b.WriteByte('\n')
	}
	return b.String()
}

func x4ErrAddrs(err error) []Path {
	var out []Path
	for _, e := range Elements(err) {
		out = append(out, tAddress(e))
	}
	return out
}

// x4Shuffle is a deterministic in-place rotation-and-swap, so the probe has no
// randomness of its own to explain.
func x4Shuffle(s []error) {
	for i := len(s) - 1; i > 0; i-- {
		j := (i*7 + 3) % (i + 1)
		s[i], s[j] = s[j], s[i]
	}
}

// --- X9: ferrytest's exact-set diff --------------------------------------------

// X4Idx is the fixture ADR-0003 uses to distinguish the two orders: twelve
// indices, where sorting the RENDERING gives 0 1 10 11 2 and sorting
// segment-wise gives 0 1 2 ... 11.
type X4Idx struct {
	V []int `ferry:"v"`
}

func runX4i() {
	saysX4("ADR-0011", `"an EXACT-SET diff over (address, class) pairs", reported "in
	segment-wise order".`)
	saysX4("ADR-0003", `"Measured, twelve indices sorted by canonical bytes: 0 1 10 11 2 3 4 5
	6 7 8 9. Sorted segment-wise, comparing Index segments numerically:
	0 1 2 3 4 5 6 7 8 9 10 11."
	and "Sorting the rendering instead is a subtle bug that produces
	0 1 10 11 2, and it will be a conformance-suite case."`)

	fmt.Println("  (1) DOES DiffErrors DEPEND ON COLLECTION ORDER? Twelve failing")
	fmt.Println("      addresses, the error elements presented in 200 shuffled orders:")
	var els []error
	for i := range 12 {
		els = append(els, errAt(mWalk, ErrValue, Path{}.Name("v").Index(i), "is not a valid int"))
	}
	seen := map[string]int{}
	for range 200 {
		sh := slices.Clone(els)
		x4Shuffle(sh)
		seen[strings.Join(DiffErrors(join(sh...)), "|")]++
	}
	fmt.Printf("      %d distinct diff(s) over 200 shuffles -> collection-order INDEPENDENT\n", len(seen))
	fmt.Println("      It builds a map keyed by (address, class), then sorts the union of")
	fmt.Println("      the keys, so the input order is discarded before anything prints.")

	fmt.Println("\n  (2) BUT IN WHICH ORDER DOES IT SORT? ferr_ferrytest.go compares the")
	fmt.Println("      key's `addr` field with strings.Compare, which is the CANONICAL")
	fmt.Println("      RENDERING - the order ADR-0003 names as the subtle bug.")
	d := DiffErrors(join(els...))
	fmt.Println("      the diff, over twelve indices:")
	for _, l := range d {
		fmt.Printf("        %s\n", l)
	}
	got := make([]string, 0, len(d))
	for _, l := range d {
		f := strings.Fields(l)
		got = append(got, f[1])
	}
	fmt.Printf("\n      as an index list : %s\n", x4Indices(got))
	var want []string
	for _, p := range sortedPaths(x4IdxPaths(12)) {
		want = append(want, p.String())
	}
	fmt.Printf("      segment-wise     : %s\n", x4Indices(want))
	fmt.Printf("      agree            : %v\n", fmt.Sprint(got) == fmt.Sprint(want))
	fmt.Println()
	fmt.Println("      DEFECT, REPORTED AND NOT FIXED. This is ADR-0003's own named")
	fmt.Println("      conformance case, failing inside the helper ADR-0011 describes as")
	fmt.Println("      reporting `in segment-wise order`. The one-line repair is to")
	fmt.Println("      compare with CompareSegmentwise on the Path rather than")
	fmt.Println("      strings.Compare on its rendering, which means keying the map by")
	fmt.Println("      the Path - already comparable - instead of by its String().")
	fmt.Println("      ferr_ferrytest.go is an engine file, so this session does not")
	fmt.Println("      touch it.")

	fmt.Println("\n  (3) It is NOT the same defect as the Get sequence, and the two must")
	fmt.Println("      not be conflated: the diff is one of ADR-0003's three NAMED")
	fmt.Println("      places (the conformance suite) and is simply sorting wrongly; a")
	fmt.Println("      Get sequence is none of the three.")
	fmt.Println("      And the ordinary error report gets this right, which is what")
	fmt.Println("      shows the bug is local to the diff helper:")
	fmt.Printf("        join(...) over the same twelve: %s\n", x4Indices(x4ErrAddrStrings(join(els...))))
}

func x4IdxPaths(n int) []Path {
	out := make([]Path, 0, n)
	for i := range n {
		out = append(out, Path{}.Name("v").Index(i))
	}
	return out
}

func x4ErrAddrStrings(err error) []string {
	var out []string
	for _, e := range Elements(err) {
		out = append(out, tAddress(e).String())
	}
	return out
}

// x4Indices renders a list of /v#N addresses as bare indices, so the two orders
// are readable side by side.
func x4Indices(ss []string) string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if i := strings.LastIndexAny(s, "#"); i >= 0 {
			out = append(out, s[i+1:])
			continue
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

// --- X10: the rest of the surface ----------------------------------------------

func runX4j() {
	ctx := context.Background()
	saysX4("ADR-0003", `The three named places: "in dumped output, in error reports, and in the
	conformance suite".`)

	fmt.Println("  Everything else that could see a Get sequence, measured.")

	fmt.Println("\n  (a) DUMPED OUTPUT. Covered, and it does not go through Get at all.")
	var seen []Path
	_ = Dump(ctx, X2ONested{"z", X2OInner{"y", "a"}, "a"}, x2OrderSink{&seen})
	fmt.Printf("      Set sequence: %v  segment-wise=%v\n", seen, x2IsSorted(seen))

	fmt.Println("\n  (b) THE CONFORMANCE HARNESS. Does RoundTrip assert on any sequence?")
	fmt.Println("      harness.go's Proof compares ONE value through one plane and")
	fmt.Println("      reports failures and refusals; there is no address sequence in it.")
	fmt.Println("      Measured by running it and reading what it produces:")
	x4HarnessShape()

	fmt.Println("\n  (c) A YAML FILE WRITTEN BY THE SINK. Its key order is the SINK's, and")
	fmt.Println("      the sink is fed by sortedAddrs, so it is segment-wise regardless")
	fmt.Println("      of any Get:")
	dir, _ := os.MkdirTemp("", "x4y")
	defer os.RemoveAll(dir)
	yp := filepath.Join(dir, "out.yaml")
	_ = Dump(ctx, X2ONested{"z", X2OInner{"y", "a"}, "a"}, FYAMLSink{Path: yp})
	b, _ := os.ReadFile(yp)
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		fmt.Printf("        %s\n", l)
	}
	fmt.Println("      A Load of that file then Gets in walk order and writes the SAME")
	fmt.Println("      file back, so ADR-0001's round-trip obligation is unaffected:")
	var back X2ONested
	back, lerr := Load[X2ONested](ctx, FYAMLSource{Path: yp})
	yp2 := filepath.Join(dir, "out2.yaml")
	_ = Dump(ctx, back, FYAMLSink{Path: yp2})
	b2, _ := os.ReadFile(yp2)
	fmt.Printf("      load err=%v   byte-identical round trip: %v\n", lerr, string(b) == string(b2))

	fmt.Println("\n  (d) FirstOf. It stops at the first CHILD holding a value, over the")
	fmt.Println("      caller's argument list, and asks every child the same address.")
	fmt.Println("      The address order is irrelevant to which layer answers:")
	a, bb := 0, 0
	_ = x4FirstOfCalls(ctx, false, &a, &bb)
	c, d := 0, 0
	_ = x4FirstOfCalls(ctx, true, &c, &d)
	fmt.Printf("      as compiled -> child A %d, child B %d\n", a, bb)
	fmt.Printf("      reversed    -> child A %d, child B %d\n", c, d)

	fmt.Println("\n  (e) THE SCHEMA CACHE AND THE STATIC ADDRESS SET. Already sorted, and")
	fmt.Println("      it is what Bind hands a driver, so the one address list that")
	fmt.Println("      crosses the driver boundary as a LIST is segment-wise today:")
	s := mustSchema[X2OTwoLevel]()
	fmt.Printf("      %v  segment-wise=%v\n", s.as.All(), x2IsSorted(s.as.All()))

	fmt.Println("\n  (f) CONCURRENCY (#20). If the walk ever runs siblings in parallel,")
	fmt.Println("      the Get sequence stops being a sequence at all, and nothing that")
	fmt.Println("      matters may depend on it. Everything measured above already")
	fmt.Println("      survives that, which is the strongest form of the answer:")
	fmt.Println("      the error set is sorted at construction, the dumped output is")
	fmt.Println("      sorted at the write loop, the observation is a mapping, and the")
	fmt.Println("      harness compares values.")

	fmt.Println("\n  NOTHING FOUND that makes the Get sequence user-visible as an ordered")
	fmt.Println("  artefact ferry produces. The one thing that CAN see it - a lazy")
	fmt.Println("  driver's own instrumentation - is on the far side of an interface")
	fmt.Println("  whose batch branch does not see it either.")
}

func x4HarnessShape() {
	n := 0
	for _, pr := range CoreTypes() {
		n++
		_ = pr
	}
	fmt.Printf("      CoreTypes(): %d proofs, each an (address -> value) comparison.\n", n)
	fmt.Println("      Proof.run returns (failures, refusals) - two []string, neither an")
	fmt.Println("      address sequence.")
	fmt.Printf("      the harness's own reflect view of a Proof: %v\n",
		reflect.TypeOf(CoreTypes()[0]))
}

// --- X11: what extending the rule would cost ------------------------------------

func runX4k() {
	ctx := context.Background()
	saysX4("ADR-0003", `"So wherever ferry enumerates addresses, in dumped output, in error
	reports, and in the conformance suite, it sorts segment-wise."
	The proposal under costing: extend "wherever" to the Get sequence, by
	sorting the compiled node's field list once per schema.`)

	fmt.Println("  (1) WHAT THE EDIT IS. One sort in e_schema.go, after n.fields is")
	fmt.Println("      built, ordering each struct node's children by the segment they")
	fmt.Println("      contribute. It runs once per schema, so it is not on the hot path.")
	fmt.Println("      Two spellings of the key, benchmarked against the clone alone so")
	fmt.Println("      the sort is what is being measured:")
	fmt.Printf("      %-8s %12s %12s %12s\n", "fields", "clone only", "by Path", "by last seg")
	for _, n := range []int{8, 64, 512} {
		ps := make([]Path, 0, n)
		for i := range n {
			ps = append(ps, Path{}.Name(fmt.Sprintf("f%03d", (i*37)%n)))
		}
		keys := make([]string, 0, n)
		for _, p := range ps {
			keys = append(keys, p.String())
		}
		base := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				_ = slices.Clone(ps)
			}
		})
		byPath := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				c := slices.Clone(ps)
				slices.SortFunc(c, CompareSegmentwise)
			}
		})
		bySeg := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				c := slices.Clone(keys)
				slices.Sort(c)
			}
		})
		fmt.Printf("      %-8d %10d ns %10d ns %10d ns\n", n,
			base.NsPerOp(), byPath.NsPerOp(), bySeg.NsPerOp())
	}
	fmt.Println("      CompareSegmentwise re-parses the whole canonical path on every")
	fmt.Println("      comparison - Segments() allocates - so sorting a 512-field node by")
	fmt.Println("      it costs milliseconds. Sorting by the ONE segment the child")
	fmt.Println("      contributes is the affordable spelling, and it is also the only")
	fmt.Println("      correct one at a node, because a child's later segments are its")
	fmt.Println("      own subtree's business. Either way it is once per schema compile,")
	fmt.Println("      so on the hot path this is free.")

	fmt.Println("\n  (2) WHAT IT WOULD MOVE. The compiled schema walked three ways: as")
	fmt.Println("      the walk visits it today, as it would with n.fields sorted at each")
	fmt.Println("      node, and fully segment-wise, which is the property being asked")
	fmt.Println("      for. `[dyn]` marks a point where the driver's Children takes over.")
	fmt.Println()
	movers, wrong := 0, 0
	for _, f := range x4Fixtures() {
		s, err := schemaFor(f.typ, defaultOpts())
		if err != nil {
			fmt.Printf("      %-20s compile: %v\n", f.name, err)
			continue
		}
		now := x4SimWalk(s.root, Path{}, false)
		after := x4SimWalk(s.root, Path{}, true)
		full := x4FullSort(after)
		if fmt.Sprint(now) != fmt.Sprint(after) {
			movers++
		}
		ok := fmt.Sprint(after) == fmt.Sprint(full)
		if !ok {
			wrong++
		}
		fmt.Printf("      %-20s today  %v\n", f.name, now)
		fmt.Printf("      %-20s sorted %v\n", "", after)
		if !ok {
			fmt.Printf("      %-20s WANTED %v   <- a per-node sort does NOT reach it\n", "", full)
		}
	}
	fmt.Printf("\n      %d fixtures would change their Get sequence; on %d of them the\n", movers, wrong)
	fmt.Println("      per-node sort still does not produce segment-wise order.")

	fmt.Println("\n  (3) THE RECORDED OUTPUT IT WOULD MOVE. Two probes print a raw Get")
	fmt.Println("      sequence, and both are Observe callback logs:")
	dir, _ := os.MkdirTemp("", "x4c")
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "app.yaml")
	os.WriteFile(p, []byte("name: svc\ndb:\n  host: h\n  port: 5432\n"), 0o644)
	var seen []string
	_, _ = Load[CConf](ctx, cNaiveSource{FYAMLSource{Path: p}, &seen}, WithSched(tAggregating))
	fmt.Printf("      C10 C1, `the wrapper saw N addresses` : %v\n", seen)
	fmt.Printf("        segment-wise today                 : %v\n", x2IsSorted(x4PathsOfStrings(seen)))
	rec := newRecord()
	_, _ = Load[TConf](ctx, tObserving{tEmptySource{}, rec}, WithSched(tAggregating))
	fmt.Printf("      T14 T9, the observing Source's set   : %d addresses, printed through\n", len(rec.Addrs()))
	fmt.Println("        Addrs(), which sorts - so T9 does NOT move; the C10 line does.")
	fmt.Println("      X2=10's own `two levels` line moves, by construction.")

	fmt.Println("\n      AND ONE THING THAT IS NOT A RECORDED LINE. e3_resolved.go reads")
	fmt.Println("      the compiled schema by POSITION - `s.root.fields[5].node` - and")
	fmt.Println("      dereferences that node's `def`. Measured, which index carries a")
	fmt.Println("      default today, and which would after a per-node sort:")
	es, _ := schemaFor(reflect.TypeFor[E3Conf](), defaultOpts())
	fmt.Printf("        today  : ")
	x4PrintDefIdx(es.root, false)
	fmt.Printf("        sorted : ")
	x4PrintDefIdx(es.root, true)
	fmt.Println("      Under the sort, index 5 is a node with a nil `def`, so E16=3")
	fmt.Println("      SEGFAULTS. That is a probe defect rather than an engine one, and")
	fmt.Println("      it is exactly the kind of cost this measurement exists to surface:")
	fmt.Println("      n.fields order is currently load-bearing for anything that indexes")
	fmt.Println("      it, and only the fixture's declaration order documents that.")

	fmt.Println("\n      THE WHOLE BLAST RADIUS, measured by applying the sort to a")
	fmt.Println("      throwaway copy of this package outside the repo and diffing every")
	fmt.Println("      suite before and after:")
	fmt.Println("        B25 B5c    the Observe callback sequence                MOVED")
	fmt.Println("        C10 C1     `the wrapper saw 3 addresses`                MOVED")
	fmt.Println("        X2=10      its own Get-sequence lines                   MOVED")
	fmt.Println("        E16 E3     positional s.root.fields[5]                  PANIC")
	fmt.Println("        T14 W15 P19 P12 A41, and the default run                unchanged")
	fmt.Println("      Two lines in P19 and P12 also differ between the two captures and")
	fmt.Println("      are NOT caused by the sort: both are the `winner decided by map")
	fmt.Println("      iteration order` cases, and five repeat runs of the UNCHANGED")
	fmt.Println("      package flip them too. They are pre-existing nondeterminism in a")
	fmt.Println("      probe, worth a separate look and not this question's.")
	fmt.Println("      So the previous agent's `moved two real rows` is right about the")
	fmt.Println("      two Observe rows and misses the panic.")

	fmt.Println("\n  (4) AND THE PART THE ONE-LINE FIX DOES NOT REACH. Sorting n.fields")
	fmt.Println("      per node gives segment-wise order only where a node's children")
	fmt.Println("      contribute exactly one segment each. Two shapes break that:")
	fmt.Println()
	fmt.Println("      (i) A PROMOTED EMBEDDED STRUCT contributes NO segment of its own,")
	fmt.Println("          and its children address as the parent's siblings. There is")
	fmt.Println("          no key to sort it by, and sorting the parent's field list")
	fmt.Println("          cannot interleave a block into it:")
	ps, _ := schemaFor(reflect.TypeFor[X2OPromoted](), defaultOpts())
	fmt.Printf("            today                 : %v\n", x4SimWalk(ps.root, Path{}, false))
	fmt.Printf("            a per-node sort gives : %v\n", x4SimWalk(ps.root, Path{}, true))
	fmt.Printf("            segment-wise          : %v\n", x4FullSort(x4SimWalk(ps.root, Path{}, true)))
	fmt.Println("            The embedded block lands wherever its sort key puts it -")
	fmt.Println("            here first, because its key is the empty string - and that")
	fmt.Println("            is not the segment-wise order for ANY key. Getting")
	fmt.Println("            this right needs the walk to flatten promoted children into")
	fmt.Println("            the parent's field list at compile, which is a real change")
	fmt.Println("            to the schema shape and not a sort.")
	fmt.Println()
	fmt.Println("      (ii) A DYNAMIC COMPOSITE's members come from the driver's")
	fmt.Println("           Children, which the walk does not sort (X6 (3)). So even")
	fmt.Println("           after the fix, a Get sequence over a map is the driver's")
	fmt.Println("           order, and the promise would be false on exactly the shapes")
	fmt.Println("           ferry exists to serve.")
	os.WriteFile(p, []byte("m:\n  zeta: 1\n  alpha: 2\n"), 0o644)
	var mseq []Path
	_, _ = Load[X4MapConf](ctx, FYAMLSource{Path: p}, Observe(func(a Path, _ Value) { mseq = append(mseq, a) }))
	fmt.Printf("            measured, yaml map in document order: %v\n", mseq)

	fmt.Println("\n  (5) THE PRICE OF THE PROMISE, which is the real cost. A stated")
	fmt.Println("      ordering guarantee on Get is a DRIVER-CONTRACT obligation: it")
	fmt.Println("      would have to hold under #20's concurrent walk, which ADR-0011")
	fmt.Println("      already anticipates (\"the walk may emit in any order\"), and it")
	fmt.Println("      would have to be a conformance case per ADR-0003's own wording.")
	fmt.Println("      So the cheap edit buys a promise that is either false on maps and")
	fmt.Println("      embedded structs, or blocks concurrency. That is the trade.")

	fmt.Println("\n  (6) THE RECOMMENDATION, which the repo owner can overrule.")
	fmt.Println("      DO NOT EXTEND THE RULE, and say why in ADR-0003 rather than")
	fmt.Println("      leaving `wherever` to be read as covering Get.")
	fmt.Println()
	fmt.Println("      The evidence, all of it measured above:")
	fmt.Println("      - Nothing ferry produces exposes the Get sequence as an ordered")
	fmt.Println("        artefact (X6, X7, X8, X10). ADR-0006's observation is a MAPPING;")
	fmt.Println("        the error report sorts at construction; the dumped output is")
	fmt.Println("        sorted at the write loop; the harness compares values.")
	fmt.Println("      - ADR-0001's invariant is already satisfied, because it asks for")
	fmt.Println("        DETERMINISM and the sequence is single-valued on eight of eight")
	fmt.Println("        shapes over 200 runs. Sortedness is a different property and")
	fmt.Println("        ADR-0001 does not ask for it.")
	fmt.Println("      - The extension cannot be delivered in full: promoted embedded")
	fmt.Println("        structs and every dynamic composite stay unsorted (4).")
	fmt.Println("      - A partial promise is worse than none, because ADR-0003's own")
	fmt.Println("        wording would make it a conformance case, and it would collide")
	fmt.Println("        with #20 the moment the walk goes concurrent.")
	fmt.Println()
	fmt.Println("      WHAT IS WORTH DOING INSTEAD, and it is small: ADR-0003's sentence")
	fmt.Println("      names three places, and a driver author reading `wherever ferry")
	fmt.Println("      enumerates addresses` can reasonably read a fourth into it. One")
	fmt.Println("      clause - that the order in which core asks a Reader for addresses")
	fmt.Println("      is deliberately unspecified, so that the walk stays free to be")
	fmt.Println("      concurrent - closes the question at the cost of a line, and is")
	fmt.Println("      the thing ADR-0011 already assumes when it says the walk may emit")
	fmt.Println("      in any order.")
	fmt.Println()
	fmt.Println("      AND ONE THING THAT IS NOT THIS QUESTION AND SHOULD BE FIXED: X9's")
	fmt.Println("      finding. DiffErrors sorts by canonical rendering, so it reports")
	fmt.Println("      0 1 10 11 2 where ADR-0003 names that exact output as the subtle")
	fmt.Println("      bug and ADR-0011 claims the diff is segment-wise. That is one of")
	fmt.Println("      the three NAMED places, and it is wrong today.")
}

// x4PrintDefIdx prints a struct node's field list as index:name, marking the
// ones that carry a declared default, under the current order or a per-node
// sort. It reads the compiled schema; it does not change it.
func x4PrintDefIdx(n *node, sorted bool) {
	fs := slices.Clone(n.fields)
	if sorted {
		slices.SortStableFunc(fs, func(a, b sfield) int {
			return strings.Compare(x4Key(a.node, n), x4Key(b.node, n))
		})
	}
	parts := make([]string, 0, len(fs))
	for i, f := range fs {
		mark := ""
		if f.node.def != nil {
			mark = "*"
		}
		parts = append(parts, fmt.Sprintf("%d:%s%s", i, x4Key(f.node, n), mark))
	}
	fmt.Printf("%s   (* = has a default)\n", strings.Join(parts, " "))
}

// x4Fixtures is X2=10's eight shapes as types, so the schema can be walked
// directly rather than inferred from a Get log.
func x4Fixtures() []struct {
	name string
	typ  reflect.Type
} {
	return []struct {
		name string
		typ  reflect.Type
	}{
		{"flat, 3 fields", reflect.TypeFor[X2OFlat]()},
		{"nested structs", reflect.TypeFor[X2ONested]()},
		{"slice + leaf", reflect.TypeFor[X2OSlice]()},
		{"array + leaf", reflect.TypeFor[X2OArray]()},
		{"pointer + leaf", reflect.TypeFor[X2OPtr]()},
		{"promoted embedded", reflect.TypeFor[X2OPromoted]()},
		{"map of structs", reflect.TypeFor[X2OMapStruct]()},
		{"two levels", reflect.TypeFor[X2OTwoLevel]()},
	}
}

// x4SimWalk mirrors e_walk.go's traversal over a COMPILED schema and emits the
// addresses it would Get, with the option of sorting each struct node's field
// list first. It is a simulation and changes nothing: the engine is untouched.
//
// The sort key is the ONE segment the child contributes, which is what a
// per-node sort has available. A promoted embedded struct contributes none, so
// its key is empty - and that is the whole of finding (4)(i).
func x4SimWalk(n *node, at Path, sorted bool) []string {
	switch n.kind {
	case nLeaf:
		return []string{at.String()}
	case nStruct:
		fs := slices.Clone(n.fields)
		if sorted {
			slices.SortStableFunc(fs, func(a, b sfield) int {
				return strings.Compare(x4Key(a.node, n), x4Key(b.node, n))
			})
		}
		var out []string
		for _, f := range fs {
			cat := at
			if f.node.shape.String() != n.shape.String() {
				cat = childAddr(at, f.node.shape, n.shape)
			}
			out = append(out, x4SimWalk(f.node, cat, sorted)...)
		}
		return out
	case nPtr:
		return append([]string{at.String()}, x4SimWalk(n.elem, at, sorted)...)
	case nArray:
		var out []string
		for i := range n.n {
			out = append(out, x4SimWalk(n.elem, at.Index(i), sorted)...)
		}
		return out
	case nSlice, nMap:
		return []string{at.String(), at.String() + " [dyn]"}
	}
	return nil
}

// x4Key is the segment a child contributes to its parent's address, which is
// empty for a promoted embedded struct.
func x4Key(child, parent *node) string {
	cs, ps := child.shape.Segments(), parent.shape.Segments()
	if len(cs) <= len(ps) {
		return ""
	}
	return cs[len(ps)].Text
}

// x4FullSort is the segment-wise order over the same address list, which is the
// property the extension would be promising.
func x4FullSort(seq []string) []string {
	ps := make([]Path, 0, len(seq))
	dyn := map[string]bool{}
	for _, s := range seq {
		base := strings.TrimSuffix(s, " [dyn]")
		if base != s {
			dyn[base] = true
		}
		ps = append(ps, Path{base})
	}
	ps = sortedPaths(ps)
	out := make([]string, 0, len(seq))
	for _, p := range ps {
		out = append(out, p.String())
	}
	for i, s := range out {
		if dyn[s] && i > 0 && out[i-1] == s {
			out[i] = s + " [dyn]"
		}
	}
	return out
}

func x4PathsOfStrings(ss []string) []Path {
	out := make([]Path, 0, len(ss))
	for _, s := range ss {
		out = append(out, Path{s})
	}
	return out
}
