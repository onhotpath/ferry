package main

// E17 and E18, both opened by review.
//
// E17  is the redaction rule's cost serious? It is stated as "ferry authors a
//      message for every decode failure mode forever". That is a claim about a
//      COUNT and about what is LOST, and neither was measured.
// E18  two-phase is redundant on a sink that can stage. ADR-0004 already
//      discovers Committer by assertion, so ferry can ask.

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

func init() { e9Hooks = append(e9Hooks, runE17, runE18) }

// ---------------------------------------------------------------------------
// E17. Every decode failure the core type set can produce.
// ---------------------------------------------------------------------------

type decCase struct {
	label string
	val   Value
	typ   reflect.Type
}

// hint is what ferry would have to author to keep the ACTIONABLE half of a
// stdlib message once the value is gone. Empty means the redacted message
// already carries everything the stdlib one did.
var typeHints = map[string]string{
	"time.Duration": "a duration needs a unit, as in 30s or 1h30m",
	"time.Time":     "a time is RFC 3339, as in 2026-08-02T12:00:00Z",
}

func runE17() {
	hdr("E17 the redaction rule's cost: how many messages, and what is lost")

	cases := []decCase{
		// wrong kind, one per kind pair that can actually arrive
		{"Null at int", Null(), reflect.TypeFor[int]()},
		{"Null at string", Null(), reflect.TypeFor[string]()},
		{"Number at string", Number("7"), reflect.TypeFor[string]()},
		{"Bool at string", Bool(true), reflect.TypeFor[string]()},
		{"String at bool", String("yes"), reflect.TypeFor[bool]()},
		{"Bytes at int", Bytes([]byte{1}), reflect.TypeFor[int]()},
		// syntax
		{"garbage at int", String("nope"), reflect.TypeFor[int]()},
		{"garbage at uint", String("nope"), reflect.TypeFor[uint]()},
		{"garbage at float64", String("nope"), reflect.TypeFor[float64]()},
		{"garbage at bool", String("nope"), reflect.TypeFor[bool]()},
		{"empty at int", String(""), reflect.TypeFor[int]()},
		{"hex at int", String("0x10"), reflect.TypeFor[int]()},
		{"float at int", String("1.9"), reflect.TypeFor[int]()},
		// range
		{"overflow int8", Number("99999"), reflect.TypeFor[int8]()},
		{"negative at uint", Number("-1"), reflect.TypeFor[uint]()},
		{"overflow float32", Number("1e40"), reflect.TypeFor[float32]()},
		// the two identity-table types
		{"no unit at Duration", String("30"), reflect.TypeFor[time.Duration]()},
		{"garbage at Duration", String("nope"), reflect.TypeFor[time.Duration]()},
		{"garbage at Time", String("nope"), reflect.TypeFor[time.Time]()},
		{"date-only at Time", String("2026-08-02"), reflect.TypeFor[time.Time]()},
	}

	fmt.Printf("  %d distinct decode failures reachable in core's type set\n\n", len(cases))
	fmt.Printf("  %-20s %-52s %s\n", "case", "what the stdlib says", "what ferry says")
	fmt.Printf("  %-20s %-52s %s\n", strings.Repeat("-", 20), strings.Repeat("-", 52), strings.Repeat("-", 34))

	distinctFerry := map[string]bool{}
	losesReason := 0
	for _, c := range cases {
		probe := reflect.New(c.typ).Elem()
		err := decLeaf(c.val, probe)
		std := "(no error)"
		if err != nil {
			std = err.Error()
		}
		mine := safeDecodeMsg(c.val, c.typ, err)
		distinctFerry[mine] = true
		// Does the stdlib message carry a REASON that survives removing the
		// value, and that ferry's does not have?
		if carriesReason(std) && !carriesReason(mine) {
			losesReason++
		}
		fmt.Printf("  %-20s %-52s %s\n", c.label, cut(std, 52), mine)
	}

	fmt.Printf("\n  ferry authors %d distinct messages for %d failures.\n", len(distinctFerry), len(cases))
	fmt.Printf("  cases where the stdlib carried a reason and ferry's does not: %d\n", losesReason)

	fmt.Println("\n  The two that lose something, and what a hint would cost:")
	for _, c := range cases {
		probe := reflect.New(c.typ).Elem()
		err := decLeaf(c.val, probe)
		if err == nil || !carriesReason(err.Error()) {
			continue
		}
		mine := safeDecodeMsg(c.val, c.typ, err)
		if carriesReason(mine) {
			continue
		}
		h := typeHints[c.typ.String()]
		fmt.Printf("    %-22s stdlib : %s\n", c.label, err)
		fmt.Printf("    %-22s ferry  : %s\n", "", mine)
		fmt.Printf("    %-22s hinted : ferry: /addr: %s: %s\n\n", "", mine, h)
	}
	fmt.Printf("  So the hint table is %d entries, not one per type, and both are\n", len(typeHints))
	fmt.Println("  types ferry already owns in the identity table (ADR-0005).")
	fmt.Println("  Every other row loses only the value, which the address already locates.")
}

// carriesReason: does this message say WHY beyond naming the target type?
func carriesReason(s string) bool {
	for _, k := range []string{"missing unit", "out of range", "wrong kind", "cannot take", "cannot parse"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

// ---------------------------------------------------------------------------
// E18. Ask the sink whether it can stage.
// ---------------------------------------------------------------------------

type committer interface{ Commit() }

func runE18() {
	hdr("E18 two-phase, but only where the sink cannot stage")

	s := mustSchema(reflect.TypeFor[E16Conf]())
	deny := map[string]bool{"/Region": true, "/Bucket": true}
	steps := encodeAll(reflect.ValueOf(e16Value(true)), s) // S4: both kinds of failure

	// (a) a sink that CANNOT stage: two-phase, because nothing else gives the
	// untouched-plane property.
	flat := &recSink{failOn: deny}
	_, isCommitter := eWriter(flat).(committer)
	rFlat := runDump(steps, flat, polTwoPhase)
	fmt.Printf("  (a) non-staging sink, implements Committer: %v\n", isCommitter)
	fmt.Printf("      policy chosen  : two-phase\n")
	fmt.Printf("      plane          : %s\n", planeSummary(flat.written))
	fmt.Printf("      errors         : %d  %v\n", len(Elements(rFlat.err)), rFlat.err)

	// (b) a sink that CAN stage: interleaved, because Commit-on-success already
	// guarantees the plane is untouched, so the buffered pass buys nothing and
	// the caller gets MORE errors.
	st := &stageSink{failOn: deny}
	_, isCommitter2 := eWriter(st).(committer)
	rStage := runDump(steps, st, polAggregate)
	fmt.Printf("\n  (b) staging sink, implements Committer: %v\n", isCommitter2)
	fmt.Printf("      policy chosen  : interleaved aggregate\n")
	fmt.Printf("      staged         : %d, and Commit does not run, so the plane holds %d\n", len(st.staged), len(st.plane))
	fmt.Printf("      errors         : %d  %v\n", len(Elements(rStage.err)), rStage.err)
	fmt.Printf("%+v\n", rStage.err)

	fmt.Println("\n  The staging sink reports BOTH kinds in one run and touches nothing.")
	fmt.Println("  The flat sink touches nothing either, and pays for it in round trips:")
	fmt.Println("  fix the two timestamps, re-run, and only then learn about the ACL.")

	// What that second round costs, measured.
	steps2 := encodeAll(reflect.ValueOf(e16Value(false)), s) // timestamps fixed
	flat2 := &recSink{failOn: deny}
	r2 := runDump(steps2, flat2, polTwoPhase)
	fmt.Printf("\n  the second run against the flat sink: %d errors, plane %s\n",
		len(Elements(r2.err)), planeSummary(flat2.written))
	fmt.Println("  So on a flat sink two-phase is a fail-fast BETWEEN phases, and that is")
	fmt.Println("  the honest cost of not writing a plane that was never going to be whole.")
}

var _ = errors.Is
