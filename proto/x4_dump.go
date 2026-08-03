package main

// Q1: which cells of ADR-0011's Dump table depend on write order, and why only
// one row.
//
// The fixtures are X2's, deliberately: X2Eight, x2EightOK, x2EightBadTimes,
// x2StagingSink, x2RefusingSink and x2Merge. If the two suites disagree it is
// because the engine moved, not because a probe rebuilt the fixture.

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// --- what ADR-0011 published --------------------------------------------------

// x4Cell is one published triple.
type x4Cell struct{ a, w, e int }

func (c x4Cell) String() string { return fmt.Sprintf("%d / %d / %d", c.a, c.w, c.e) }

// x4Published is the table exactly as ADR-0011 prints it.
var x4Published = map[string][4]x4Cell{
	"fail-fast":                 {{1, 0, 1}, {2, 1, 1}, {3, 3, 1}, {2, 1, 1}},
	"aggregate, interleaved":    {{8, 0, 8}, {8, 6, 2}, {6, 6, 2}, {6, 4, 4}},
	"two-phase, then aggregate": {{8, 0, 8}, {8, 6, 2}, {0, 0, 2}, {0, 0, 2}},
}

var x4RowOrder = []string{"fail-fast", "aggregate, interleaved", "two-phase, then aggregate"}

var x4ColNames = [4]string{"whole plane refuses", "two addresses refuse", "two cannot encode", "both"}

// x4Refused is the ADR's own two-address ACL case.
func x4Refused() map[string]bool { return map[string]bool{"/Bucket": true, "/Region": true} }

// --- X1: the table through the engine ----------------------------------------

// x4Measure runs one column under one policy through the real Dump and returns
// the measured triple.
//
// `fail-fast` and `aggregate, interleaved` both go through the STAGING sink,
// because that is the branch ADR-0011 exempts from the encode gate, and reading
// `written` off the stage is what shows the plane an interleaved NON-staging
// sink would have been left holding. They differ only in the scheduler.
func x4Measure(policy string, col int) x4Cell {
	ctx := context.Background()
	var (
		v      X2Eight
		refuse map[string]bool
		all    bool
	)
	switch col {
	case 0:
		v, all = x2EightOK(), true
	case 1:
		v, refuse = x2EightOK(), x4Refused()
	case 2:
		v = x2EightBadTimes()
	case 3:
		v, refuse = x2EightBadTimes(), x4Refused()
	}
	pl := &x2Plane{}
	var err error
	switch policy {
	case "fail-fast":
		err = Dump(ctx, v, x2StagingSink{plane: pl, refuse: x2Merge(refuse, all)}, WithSched(serial))
		pl.written = pl.staged
	case "aggregate, interleaved":
		err = Dump(ctx, v, x2StagingSink{plane: pl, refuse: x2Merge(refuse, all)})
		pl.written = pl.staged
	default:
		err = Dump(ctx, v, x2RefusingSink{plane: pl, refuseAll: all, refuse: refuse})
	}
	n := 0
	if s := pl.writtenStr(); s != "(empty)" {
		n = len(strings.Fields(s))
	}
	return x4Cell{pl.attempts, n, len(Elements(err))}
}

func runX4a() {
	saysX4("ADR-0011", `"Measured over four failure shapes on an eight-address struct,
	attempts / written / errors:"
	followed by the three-row table this probe re-runs, cell for cell.`)

	fmt.Println("  Twelve cells. `pub` is ADR-0011's printed number, `got` is this")
	fmt.Println("  branch's. A cell that moved is marked MOVED.")
	fmt.Println()
	fmt.Printf("  %-26s", "policy")
	for _, c := range x4ColNames {
		fmt.Printf(" %-24s", c)
	}
	fmt.Println()
	moved := 0
	for _, row := range x4RowOrder {
		pub := x4Published[row]
		fmt.Printf("  %-26s", row)
		var marks [4]string
		for i := range 4 {
			got := x4Measure(row, i)
			if got == pub[i] {
				fmt.Printf(" %-24s", got.String())
				marks[i] = ""
				continue
			}
			moved++
			fmt.Printf(" %-24s", got.String()+" MOVED")
			marks[i] = fmt.Sprintf("published %s", pub[i])
		}
		fmt.Println()
		fmt.Printf("  %-26s", "")
		for i := range 4 {
			fmt.Printf(" %-24s", marks[i])
		}
		fmt.Println()
	}
	fmt.Printf("\n  %d of 12 cells reproduce; %d moved.\n", 12-moved, moved)
	fmt.Println("  Both moved cells are in the `fail-fast` row. The eight cells of the")
	fmt.Println("  two rows that describe policies ferry can actually be put in reproduce")
	fmt.Println("  exactly. X2 proves that is not luck: they are invariant over every")
	fmt.Println("  permutation of the write order, and fail-fast is not.")
}

// --- X2: the mechanism --------------------------------------------------------
//
// Three MODELS are in play and the previous report conflated two of them.
//
//	(i)   interleaved fail-fast, reflect walk order   - what ADR-0011 published
//	(ii)  interleaved fail-fast, segment-wise order   - the pure order change
//	(iii) buffered two-phase under `serial`           - what ferry can be put in
//
// ferry has NO interleaved code path any more: both branches of Dump buffer and
// write through one sortedAddrs loop. So (i) and (ii) are simulations, and (iii)
// is the engine. Attributing the whole move to ordering is only right for one
// of the two moved cells.

// x4Sim is the policy simulator. It is deliberately not the engine: it takes an
// address ORDER as data, so the same policy can be run under any ordering and
// the ordering is the only thing that changes.
type x4Sim struct {
	order      []string // addresses, in the order the policy visits them
	cannotEnc  map[string]bool
	refuse     map[string]bool
	failFast   bool
	twoPhase   bool // encode everything first; write nothing if any encode failed
	attempts   int
	written    []string
	errorCount int
}

func (s *x4Sim) run() x4Cell {
	if s.twoPhase {
		enc := 0
		for _, a := range s.order {
			if s.cannotEnc[a] {
				enc++
			}
		}
		if enc > 0 {
			return x4Cell{0, 0, enc}
		}
	}
	for _, a := range s.order {
		if s.cannotEnc[a] {
			s.errorCount++
			if s.failFast {
				break
			}
			continue // an encode failure never reaches the plane
		}
		s.attempts++
		if s.refuse[a] {
			s.errorCount++
			if s.failFast {
				break
			}
			continue
		}
		s.written = append(s.written, a)
	}
	return x4Cell{s.attempts, len(s.written), s.errorCount}
}

// x4WalkOrder is X2Eight's reflect struct-field order, read off the type rather
// than typed out, so it cannot drift from the fixture.
func x4WalkOrder() []string {
	t := reflect.TypeFor[X2Eight]()
	out := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		out = append(out, "/"+strings.Split(t.Field(i).Tag.Get("ferry"), ",")[0])
	}
	return out
}

// x4SegOrder is ADR-0003's order over the same eight addresses, produced by the
// engine's own comparator rather than by sorting strings.
func x4SegOrder() []string {
	ps := make([]Path, 0, 8)
	for _, s := range x4WalkOrder() {
		ps = append(ps, Path{}.Name(strings.TrimPrefix(s, "/")))
	}
	out := make([]string, 0, 8)
	for _, p := range sortedPaths(ps) {
		out = append(out, p.String())
	}
	return out
}

func x4ColSets(col int) (cannotEnc, refuse map[string]bool) {
	cannotEnc, refuse = map[string]bool{}, map[string]bool{}
	switch col {
	case 0:
		for _, a := range x4WalkOrder() {
			refuse[a] = true
		}
	case 1:
		refuse = x4Refused()
	case 2:
		cannotEnc = map[string]bool{"/Started": true, "/Expires": true}
	case 3:
		cannotEnc = map[string]bool{"/Started": true, "/Expires": true}
		refuse = x4Refused()
	}
	return
}

// x4FirstStop reports the address at which a fail-fast policy stops, under one
// ordering and one column.
func x4FirstStop(order []string, col int) string {
	enc, ref := x4ColSets(col)
	for _, a := range order {
		if enc[a] {
			return a + " (cannot encode)"
		}
		if ref[a] {
			return a + " (refused)"
		}
	}
	return "(nothing fails)"
}

func runX4b() {
	saysX4("ADR-0003", `"So wherever ferry enumerates addresses, in dumped output, in error
	reports, and in the conformance suite, it sorts segment-wise."`)
	saysX4("ADR-0011", `"Ordering is not the walk's, because sorting happens when the aggregate
	is constructed, so the walk may emit in any order. There is no cap, so
	there is no stop-after-N."`)

	walk, seg := x4WalkOrder(), x4SegOrder()
	fmt.Printf("  reflect struct-field order : %v\n", walk)
	fmt.Printf("  ADR-0003 segment-wise order: %v\n", seg)
	fmt.Println()
	fmt.Println("  And the order ferry ACTUALLY writes in, read off the sink:")
	var got []Path
	_ = Dump(context.Background(), x2EightOK(), x2OrderSink{&got})
	fmt.Printf("    %v\n", got)
	fmt.Printf("    == segment-wise: %v\n", fmt.Sprint(got) == fmt.Sprint(x4PathsOf(seg)))
	fmt.Println()

	fmt.Println("  WHERE A FAIL-FAST POLICY STOPS, per column, per ordering:")
	fmt.Printf("  %-24s %-34s %s\n", "column", "reflect walk order", "segment-wise order")
	for i, c := range x4ColNames {
		fmt.Printf("  %-24s %-34s %s\n", c, x4FirstStop(walk, i), x4FirstStop(seg, i))
	}
	fmt.Println()
	fmt.Println("  Columns 2 and 4 are the two where the two orderings stop at a")
	fmt.Println("  DIFFERENT address, and they are exactly the two cells the previous")
	fmt.Println("  report named. Column 1 stops at the first address either way because")
	fmt.Println("  every address refuses; column 3's first failure is an encode failure,")
	fmt.Println("  which ferry reaches through the WALK and not through the write loop.")

	fmt.Println("\n  --- the three models, side by side ---")
	fmt.Println()
	fmt.Println("  (i)   interleaved fail-fast, reflect walk order  - what ADR-0011 published")
	fmt.Println("  (ii)  interleaved fail-fast, segment-wise order  - the pure order change")
	fmt.Println("  (iii) buffered two-phase under `serial`          - what ferry can be put in")
	fmt.Println()
	fmt.Printf("  %-24s %-14s %-14s %-14s %s\n", "column", "published", "(i) walk", "(ii) segment", "(iii) engine")
	for i, c := range x4ColNames {
		enc, ref := x4ColSets(i)
		si := (&x4Sim{order: walk, cannotEnc: enc, refuse: ref, failFast: true}).run()
		enc2, ref2 := x4ColSets(i)
		sii := (&x4Sim{order: seg, cannotEnc: enc2, refuse: ref2, failFast: true}).run()
		fmt.Printf("  %-24s %-14s %-14s %-14s %s\n", c,
			x4Published["fail-fast"][i].String(), si.String(), sii.String(), x4Measure("fail-fast", i).String())
	}
	fmt.Println()
	fmt.Println("  READ (i) FIRST. The interleaved simulator in reflect walk order")
	fmt.Println("  reproduces ADR-0011's published fail-fast row in all FOUR cells. That")
	fmt.Println("  identifies the published row precisely: it is an interleaved fail-fast")
	fmt.Println("  - encode and write at each leaf, in the walk's own order - which is")
	fmt.Println("  what a Dump that wrote during the walk would do.")
	fmt.Println()
	fmt.Println("  THE PREVIOUS REPORT'S ATTRIBUTION IS HALF WRONG, and (ii) is what")
	fmt.Println("  shows it:")
	fmt.Println("    column 2  2/1/1 -> 1/0/1   ordering, and ONLY ordering. (ii) agrees")
	fmt.Println("              with the engine, so the whole move is the write order.")
	fmt.Println("    column 4  2/1/1 -> 2/1/2   NOT ordering. Pure segment-wise")
	fmt.Println("              interleaving gives 1/0/1, which is neither number. The")
	fmt.Println("              engine's 2/1/2 comes from the BUFFER: phase one encodes in")
	fmt.Println("              walk order and stops at /Started, phase two then writes the")
	fmt.Println("              three addresses that did encode - /Name, /Region, /Replicas")
	fmt.Println("              - and /Region refuses. attempts and written are the ADR's;")
	fmt.Println("              the extra error is the encode failure surviving to be")
	fmt.Println("              joined with the refusal, which an interleaved fail-fast")
	fmt.Println("              never reaches at all.")
	fmt.Println()
	fmt.Println("  So the honest statement is: ONE cell moved for the reason given, and")
	fmt.Println("  the other moved because ferry no longer has an interleaved path for")
	fmt.Println("  the row to describe.")
}

func x4PathsOf(ss []string) []Path {
	out := make([]Path, 0, len(ss))
	for _, s := range ss {
		out = append(out, Path{}.Name(strings.TrimPrefix(s, "/")))
	}
	return out
}

// --- X3: order-independence, proved rather than argued ------------------------

func runX4c() {
	saysX4("ADR-0011", `The claim under test, which the ADR does not state and the previous
	report asserted: the other two rows "are order-independent because they
	visit every address regardless".`)

	base := x4WalkOrder()
	fmt.Println("  Every one of the 8! = 40320 orderings of the eight addresses, run")
	fmt.Println("  through the policy simulator. A row is order-independent iff exactly")
	fmt.Println("  ONE distinct attempts/written/errors triple occurs across all of them.")
	fmt.Println()
	fmt.Printf("  %-26s %-22s %-22s %-22s %s\n", "policy", x4ColNames[0], x4ColNames[1], x4ColNames[2], x4ColNames[3])
	var detail []string
	for _, pol := range []struct {
		name               string
		failFast, twoPhase bool
	}{
		{"fail-fast", true, false},
		{"aggregate, interleaved", false, false},
		{"two-phase, then aggregate", false, true},
	} {
		fmt.Printf("  %-26s", pol.name)
		for col := range 4 {
			seen := map[x4Cell]int{}
			x4Perm(slices.Clone(base), func(order []string) {
				enc, ref := x4ColSets(col)
				c := (&x4Sim{order: order, cannotEnc: enc, refuse: ref,
					failFast: pol.failFast, twoPhase: pol.twoPhase}).run()
				seen[c]++
			})
			keys := make([]x4Cell, 0, len(seen))
			for k := range seen {
				keys = append(keys, k)
			}
			slices.SortFunc(keys, func(a, b x4Cell) int {
				if a.a != b.a {
					return a.a - b.a
				}
				if a.w != b.w {
					return a.w - b.w
				}
				return a.e - b.e
			})
			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				parts = append(parts, k.String())
			}
			fmt.Printf(" %-22s", fmt.Sprintf("%d distinct", len(keys)))
			if len(keys) > 1 {
				detail = append(detail, fmt.Sprintf("    %-26s %-22s %s", pol.name, x4ColNames[col],
					strings.Join(parts, ";  ")))
			}
		}
		fmt.Println()
	}
	fmt.Println()
	fmt.Println("  The cells with more than one triple, spelled out:")
	for _, d := range detail {
		fmt.Println(d)
	}
	fmt.Println()
	fmt.Println("  Eight of the eight cells in the two aggregating rows admit exactly one")
	fmt.Println("  triple over all 40320 orderings, and each is the published one. They")
	fmt.Println("  cannot move under any reordering, because neither policy stops: every")
	fmt.Println("  address is encoded and every encodable one is attempted, so attempts,")
	fmt.Println("  written and errors are all set CARDINALITIES rather than prefixes.")
	fmt.Println()
	fmt.Println("  fail-fast admits several triples in three of four columns, which is")
	fmt.Println("  the property, stated as a measurement: its numbers are the length of a")
	fmt.Println("  PREFIX of the ordering, so they are a function of the ordering.")
	fmt.Println()
	fmt.Println("  Column 1 is the exception - one triple even under fail-fast - because")
	fmt.Println("  every address refuses, so every prefix has length one.")
}

// x4Perm calls fn once per permutation of s, in place.
func x4Perm(s []string, fn func([]string)) {
	var rec func(k int)
	rec = func(k int) {
		if k == len(s) {
			fn(s)
			return
		}
		for i := k; i < len(s); i++ {
			s[k], s[i] = s[i], s[k]
			rec(k + 1)
			s[k], s[i] = s[i], s[k]
		}
	}
	rec(0)
}

// --- X4: every other number that counts something stopping early ---------------

// x4DyingSource fails every Get after the first n succeed, which is ADR-0011's
// "the plane dies at the third Get".
type x4DyingSource struct {
	vals  map[Path]Value
	after int
	seen  *[]Path
}

func (s x4DyingSource) Bind(*AddressSet) (FOpenFunc, error) {
	n := 0
	return func(context.Context) (FReader, error) {
		return &x4DyingReader{s.vals, s.after, &n, s.seen}, nil
	}, nil
}

type x4DyingReader struct {
	vals  map[Path]Value
	after int
	n     *int
	seen  *[]Path
}

func (r *x4DyingReader) Get(_ context.Context, p Path) (Value, error) {
	*r.n++
	if r.seen != nil {
		*r.seen = append(*r.seen, p)
	}
	if *r.n > r.after {
		return Value{}, errorsNew("kv: dial tcp 10.0.0.1:8500: connect: connection refused")
	}
	return r.vals[p], nil
}

// X4Eight mirrors X2Eight on the LOAD side: eight leaves, field order unrelated
// to segment-wise order, so "which six" is a question with two answers.
type X4Eight struct {
	Name     string `ferry:"Name"`
	Region   string `ferry:"Region"`
	Replicas string `ferry:"Replicas"`
	Started  string `ferry:"Started"`
	Retries  string `ferry:"Retries"`
	Endpoint string `ferry:"Endpoint"`
	Bucket   string `ferry:"Bucket"`
	Expires  string `ferry:"Expires"`
}

func runX4d() {
	ctx := context.Background()
	saysX4("ADR-0011", `"(a) the plane dies at the third Get   6 errors from 8 addresses,
	1 distinct underlying fact
	(b) a token denied on two paths       2 errors from 8 addresses, 2 distinct facts"`)

	fmt.Println("  A number is order-dependent if it counts something that stops early.")
	fmt.Println("  The sweep below takes every published number in the Accepted ADRs that")
	fmt.Println("  counts backend calls, writes, or errors, and runs it.")
	fmt.Println()

	fmt.Println("  --- (a), ADR-0011's plane-death row -------------------------------")
	vals := map[Path]Value{}
	for _, a := range x4WalkOrder() {
		vals[Path{}.Name(strings.TrimPrefix(a, "/"))] = String("v")
	}
	var seen []Path
	_, err := Load[X4Eight](ctx, x4DyingSource{vals, 2, &seen})
	els := Elements(err)
	fmt.Printf("    Get sequence                : %v\n", seen)
	fmt.Printf("    errors                      : %d from 8 addresses\n", len(els))
	var at []string
	for _, e := range els {
		at = append(at, tAddress(e).String())
	}
	fmt.Printf("    the addresses reported      : %v\n", at)
	fmt.Println()
	fmt.Println("    THE COUNT IS ORDER-INDEPENDENT and the ADR's 6 reproduces: the")
	fmt.Println("    plane dies at the third Get, so every Get from the third onward")
	fmt.Println("    fails, and 8 - 2 = 6 whatever the order is. WHICH SIX is order-")
	fmt.Println("    dependent - here the two that survived are the first two in walk")
	fmt.Println("    order, and under segment-wise they would be /Bucket and /Endpoint.")
	fmt.Println("    So (a) is not one of the moved cells' kin: the published NUMBER is")
	fmt.Println("    safe, and only the worked address list beside it would change.")
	fmt.Println()
	fmt.Println("    Swept over every position the plane could die at, to show the count")
	fmt.Println("    is 8-k and never a function of which addresses:")
	for k := range 5 {
		var s2 []Path
		_, e2 := Load[X4Eight](ctx, x4DyingSource{vals, k, &s2})
		fmt.Printf("      dies after %d Get(s) -> %d errors, survivors %v\n", k, len(Elements(e2)), s2[:min(k, len(s2))])
	}

	fmt.Println("\n  --- (b), a token denied on two paths ------------------------------")
	den := map[string]bool{"/Bucket": true, "/Region": true}
	var seenB []Path
	_, errB := Load[X4Eight](ctx, x4DenySource{vals, den, &seenB})
	fmt.Printf("    errors: %d from 8 addresses\n", len(Elements(errB)))
	var atB []string
	for _, e := range Elements(errB) {
		atB = append(atB, tAddress(e).String())
	}
	fmt.Printf("    at    : %v   (reported segment-wise, not in Get order)\n", atB)
	fmt.Println("    ORDER-INDEPENDENT in count AND in identity: aggregation visits every")
	fmt.Println("    address, so the error set is the set of denied addresses.")

	fmt.Println("\n  --- ADR-0011's Load row, fail-fast=1 / aggregating=5 --------------")
	saysX4("ADR-0011", `"Measured on five bad fields, which is 5.4 in ferry's own shape:
	fail-fast   errors=1
	aggregating errors=5"`)
	_, agg := Load[X2Five](ctx, x2FixedSource{x2BadFive()})
	_, ff := Load[X2Five](ctx, x2FixedSource{x2BadFive()}, WithSched(serial))
	fmt.Printf("    aggregating errors=%d\n", len(Elements(agg)))
	fmt.Printf("    fail-fast   errors=%d  at %v\n", len(Elements(ff)), tAddress(ff))
	fmt.Println("    The COUNT 1 is order-independent - any ordering stops at some first")
	fmt.Println("    bad field and reports one. Only the address is a function of order,")
	fmt.Println("    and the ADR does not publish it. So this row is safe.")

	fmt.Println("\n  --- ADR-0004, `lazy makes 3 backend calls, batch makes 1` ----------")
	saysX4("ADR-0004", `"Batch versus lazy is a branch inside one driver... Measured on a
	three-address schema against the Consul-shaped driver: lazy makes 3
	backend calls, batch makes 1, and the difference is one boolean in the
	driver."`)
	for _, batch := range []bool{false, true} {
		n := 0
		_, e := Load[X4Three](ctx, x4LazySource{batch: batch, calls: &n})
		label := "lazy "
		if batch {
			label = "batch"
		}
		fmt.Printf("    %s -> %d backend call(s)  err=%v\n", label, n, e)
	}
	fmt.Println("    ORDER-INDEPENDENT. Both numbers are cardinalities of a complete")
	fmt.Println("    walk - 3 is |addresses| and 1 is |round trips| - and neither policy")
	fmt.Println("    stops early. Reordering the walk cannot change either.")

	fmt.Println("\n  --- ADR-0004, `18 backend calls for SerialLoader, 3 for first-present`")
	saysX4("ADR-0004", `"Measured on a six-leaf schema over three backends: 18 backend calls
	for SerialLoader, 3 for first-present-wins over batch-fetching children."`)
	fmt.Println("    18 = 6 leaves x 3 backends, 3 = one batch per backend. Both are")
	fmt.Println("    cardinalities. FirstOf DOES stop early - at the first child holding")
	fmt.Println("    a value - but it stops over the BACKEND LIST, which is the caller's")
	fmt.Println("    argument order and not the address order, so ADR-0003 has nothing")
	fmt.Println("    to say about it. Measured, with the address order varied:")
	for _, rev := range []bool{false, true} {
		a, b := 0, 0
		_ = x4FirstOfCalls(ctx, rev, &a, &b)
		fmt.Printf("      addresses %-12s -> child A %d call(s), child B %d call(s)\n",
			map[bool]string{false: "as compiled", true: "reversed"}[rev], a, b)
	}

	fmt.Println("\n  --- ADR-0011's 10,000-address two-phase cost ----------------------")
	fmt.Println("    523 ms / 1.044 s and ~546 KB. Timings over a COMPLETE dump; no")
	fmt.Println("    early stop, so order-independent. Not re-run here: X2 already")
	fmt.Println("    covers the two-phase path and a timing is not an ordering claim.")

	fmt.Println("\n  --- ADR-0004, `refused after zero backend calls` -------------------")
	fmt.Println("    Zero is zero under every ordering.")

	fmt.Println("\n  --- ADR-0011's worked plane line ----------------------------------")
	fmt.Println("      \"fail-fast   plane: /Name /Region /Replicas    1 error\"")
	fmt.Println("    This is the ONLY other published artefact with the same dependency,")
	fmt.Println("    and it is the same row: it is the fail-fast cell of column 3 shown")
	fmt.Println("    as addresses instead of as a count. Measured on this branch it")
	fmt.Println("    still reads /Name /Region /Replicas, because column 3's stop is an")
	fmt.Println("    ENCODE failure, which the walk reaches in reflect order, and the")
	fmt.Println("    three that did encode sort back to the same three names.")
	pl := &x2Plane{}
	e := Dump(ctx, x2EightBadTimes(), x2StagingSink{plane: pl}, WithSched(serial))
	pl.written = pl.staged
	fmt.Printf("      measured: fail-fast plane: %s   %d error\n", pl.writtenStr(), len(Elements(e)))

	fmt.Println("\n  VERDICT: apart from the fail-fast row itself and its worked plane")
	fmt.Println("  line, no published number in any Accepted ADR counts something that")
	fmt.Println("  stops early over an ADDRESS sequence. Every other count is the")
	fmt.Println("  cardinality of a complete traversal.")
}

// x4DenySource is (b): a token with read access to some paths and not others.
type x4DenySource struct {
	vals map[Path]Value
	deny map[string]bool
	seen *[]Path
}

func (s x4DenySource) Bind(*AddressSet) (FOpenFunc, error) {
	return func(context.Context) (FReader, error) { return x4DenyReader(s), nil }, nil
}

type x4DenyReader struct {
	vals map[Path]Value
	deny map[string]bool
	seen *[]Path
}

func (r x4DenyReader) Get(_ context.Context, p Path) (Value, error) {
	if r.seen != nil {
		*r.seen = append(*r.seen, p)
	}
	if r.deny[p.String()] {
		return Value{}, errorsNew("vault: permission denied")
	}
	return r.vals[p], nil
}

// X4Three is ADR-0004's three-address schema.
type X4Three struct {
	A string `ferry:"a"`
	B string `ferry:"b"`
	C string `ferry:"c"`
}

// x4LazySource is ADR-0004's one driver with the branch inside it.
type x4LazySource struct {
	batch bool
	calls *int
}

func (s x4LazySource) Bind(a *AddressSet) (FOpenFunc, error) {
	addrs := a.All()
	return func(context.Context) (FReader, error) {
		if s.batch {
			*s.calls++ // ONE round trip, at open, for the whole address set
			m := map[Path]Value{}
			for _, p := range addrs {
				m[p] = String("v")
			}
			return x4BatchReader{m}, nil
		}
		return x4LazyReader{s.calls}, nil
	}, nil
}

type x4BatchReader struct{ m map[Path]Value }

func (r x4BatchReader) Get(_ context.Context, p Path) (Value, error) { return r.m[p], nil }

type x4LazyReader struct{ calls *int }

func (r x4LazyReader) Get(_ context.Context, _ Path) (Value, error) {
	*r.calls++ // one round trip per Get
	return String("v"), nil
}

// x4FirstOfCalls counts how many Gets each child of a FirstOf receives, with
// the address order varied, to show the stop is over the child list.
func x4FirstOfCalls(ctx context.Context, reverse bool, a, b *int) error {
	first := x4CountingSource{has: map[string]bool{"/a": true, "/b": true, "/c": true}, calls: a}
	second := x4CountingSource{has: map[string]bool{}, calls: b}
	src := BFirstOf(first, second)
	if reverse {
		_, err := Load[X4ThreeRev](ctx, src)
		return err
	}
	_, err := Load[X4Three](ctx, src)
	return err
}

// X4ThreeRev is X4Three with the fields declared the other way round, which is
// the only lever a probe has over the walk order.
type X4ThreeRev struct {
	C string `ferry:"c"`
	B string `ferry:"b"`
	A string `ferry:"a"`
}

type x4CountingSource struct {
	has   map[string]bool
	calls *int
}

func (s x4CountingSource) Bind(*AddressSet) (FOpenFunc, error) {
	return func(context.Context) (FReader, error) { return x4CountingReader(s), nil }, nil
}

type x4CountingReader struct {
	has   map[string]bool
	calls *int
}

func (r x4CountingReader) Get(_ context.Context, p Path) (Value, error) {
	*r.calls++
	if r.has[p.String()] {
		return String("v"), nil
	}
	return Value{}, nil
}

// --- X5: is fail-fast a policy ferry implements? ------------------------------

func runX4e() {
	saysX4("ADR-0011", `"StopOnFirstError is not shipped. The survey recommends it 'for callers
	who want the old behaviour', and ferry has no old behaviour. It is a
	public knob whose only job is to make ferry report less."
	and
	"No StopOnFirstError ships, and adding one later is a load-affecting
	Option, which ADR-0006 already priced as the cheap kind."`)

	fmt.Println("  The question is not what the ADR says but what the code exports, so")
	fmt.Println("  this reads the declarations rather than the prose.")
	fmt.Println()
	fmt.Println("    e_walk.go:71   type sched func(tasks []func() error) error   UNEXPORTED")
	fmt.Println("    e_walk.go:78   func serial(...)                              UNEXPORTED")
	fmt.Println("    ferr_sched.go  func aggregating(...)                         UNEXPORTED")
	fmt.Println("    e_opts.go:64   func WithSched(s sched) Option                exported name,")
	fmt.Println("                                                                 unexported PARAMETER")
	fmt.Println()
	fmt.Println("  `WithSched` is spelled with a capital, and it is unreachable from")
	fmt.Println("  outside the package all the same: its only argument is of an")
	fmt.Println("  unexported func type, and an importer has no way to name a value of")
	fmt.Println("  that type. So there is no caller-facing route to the fail-fast row at")
	fmt.Println("  all, and the row is a COMPARISON BASELINE, not a policy.")
	fmt.Println()
	fmt.Println("  What the default actually is, measured through the entry point with")
	fmt.Println("  no options - which is the only thing a user can reach:")
	ctx := context.Background()
	_, err := Load[X2Five](ctx, x2FixedSource{x2BadFive()})
	fmt.Printf("    Load, five bad fields, no options -> %d errors\n", len(Elements(err)))
	pl := &x2Plane{}
	de := Dump(ctx, x2EightOK(), x2RefusingSink{plane: pl, refuseAll: true})
	fmt.Printf("    Dump, plane refuses everything, no options -> %d errors from %d attempts\n",
		len(Elements(de)), pl.attempts)
	fmt.Println()
	fmt.Println("  WHAT THIS MEANS FOR THE MOVED CELLS.")
	fmt.Println("  The fail-fast row exists in ADR-0011 to price the alternative it")
	fmt.Println("  refused. Its job is the COMPARISON - `both policies leave a broken")
	fmt.Println("  plane there, six of eight addresses against one of eight` - and both")
	fmt.Println("  moved cells preserve that comparison: 1/0/1 against 8/6/2 makes the")
	fmt.Println("  ADR's point at least as sharply as 2/1/1 did, and 2/1/2 against 6/4/4")
	fmt.Println("  likewise. Nothing the ADR concludes turns on either number.")
	fmt.Println()
	fmt.Println("  So the recommendation is: do NOT amend ADR-0011's table. Amending it")
	fmt.Println("  would replace a measurement of a policy the ADR is arguing against")
	fmt.Println("  with a measurement of a policy nothing implements, taken through a")
	fmt.Println("  scheduler no caller can select, on an engine that no longer has the")
	fmt.Println("  interleaved code path the row describes. If anything is worth adding")
	fmt.Println("  it is one sentence saying the row is a counterfactual over an")
	fmt.Println("  interleaved walk in reflect field order, which is what X2 shows it is.")
}

var _ = slices.Contains[[]string]
