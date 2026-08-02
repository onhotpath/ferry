package main

// E5-E8: the four questions the grill left open as "measure it, do not assert".
//
// E5 (P4)  does ferry leak plane-supplied text into an error, and does the
//          diagnostic survive not leaking it
// E6       aggregate against fail-fast on Load, which is 5.4
// E7 (P1)  what aggregating costs a sink that cannot stage
// E8 (P2)  a plane that dies mid-walk: N copies of one fact, or N facts

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func init() { e9Hooks = append(e9Hooks, runE5, runE6, runE7, runE8) }

// ---------------------------------------------------------------------------
// E5 (P4). The secret.
// ---------------------------------------------------------------------------

// The secret is a realistic one: a Vault or Consul plane holds it, and it lands
// at an address whose Go type has to parse it. Every value on such a plane is a
// secret, so this is not an exotic case, it is the normal one.
const theSecret = "AKIAIOSFODNN7EXAMPLE"

type E5Conf struct {
	MaxConns int
	Timeout  time.Duration
	Ratio    float64
	Enabled  bool
	Budget   int8
}

func runE5() {
	hdr("E5  (P4) does an error leak the plane's own text")

	s := mustSchema(reflect.TypeFor[E5Conf]())

	// What a NAIVE wrapper produces - which is what the base prototype did, and
	// what every error string quoted in the accepted ADRs looks like.
	fmt.Print("  naive fmt.Errorf with the decoder's own error, straight through:\n")
	cases := []struct {
		field string
		val   Value
		typ   reflect.Type
	}{
		{"MaxConns", String(theSecret), reflect.TypeFor[int]()},
		{"Timeout", String(theSecret), reflect.TypeFor[time.Duration]()},
		{"Ratio", String(theSecret), reflect.TypeFor[float64]()},
		{"Enabled", String(theSecret), reflect.TypeFor[bool]()},
		{"Budget", Number("99999"), reflect.TypeFor[int8]()},
	}
	leaked := 0
	for _, c := range cases {
		probe := reflect.New(c.typ).Elem()
		err := decLeaf(c.val, probe)
		naive := fmt.Sprintf("ferry: %s: %v", addr(c.field), err)
		if strings.Contains(naive, theSecret) {
			leaked++
		}
		fmt.Printf("    %-9s %s\n", c.field, naive)
	}
	fmt.Printf("\n  %d of %d naive messages contain the plane's own text.\n", leaked, len(cases))

	// The same five through the model.
	fmt.Println("\n  ferry's own message, cause reachable and never printed:")
	sink := &errSink{}
	plane := map[Path]Value{}
	for _, c := range cases {
		plane[addr(c.field)] = c.val
	}
	var v E5Conf
	_, _ = loadD(plane, s, reflect.ValueOf(&v).Elem(), loadOpts{sink: sink})
	got := sink.result()
	fmt.Printf("%+v\n", got)

	stillLeaks := 0
	for _, e := range Elements(got) {
		if strings.Contains(e.Error(), theSecret) {
			stillLeaks++
		}
	}
	fmt.Printf("  elements whose message contains the secret: %d of %d\n", stillLeaks, len(Elements(got)))

	// The cause is still there, which is what stops redaction being a loss.
	fmt.Println("\n  and the cause is still REACHABLE, so a caller loses nothing programmatic:")
	for _, e := range Elements(got) {
		fmt.Printf("    %-40s ErrSyntax=%-5v ErrRange=%-5v\n",
			locLabel(e), errors.Is(e, strconv.ErrSyntax), errors.Is(e, strconv.ErrRange))
	}

	// The carve-out I said I would measure rather than assume: a DYNAMIC
	// address segment is plane-supplied too, and it appears in the location.
	fmt.Println("\n  the carve-out: a dynamic segment comes from the plane and IS printed")
	type E5Map struct{ Creds map[string]int }
	ms := mustSchema(reflect.TypeFor[E5Map]())
	var mv E5Map
	msink := &errSink{}
	_, _ = loadD(map[Path]Value{
		addr("Creds").Name("prod/db/" + theSecret): String("not-a-number"),
	}, ms, reflect.ValueOf(&mv).Elem(), loadOpts{sink: msink})
	mgot := msink.result()
	fmt.Printf("    %v\n", mgot)
	fmt.Printf("    location contains the secret: %v\n", strings.Contains(fmt.Sprint(mgot), theSecret))
	fmt.Println("    A map KEY is a name, not a value, and ferry cannot name the address")
	fmt.Println("    without it. So the rule is about VALUES, not about everything the")
	fmt.Println("    plane supplied, and that is the honest statement of it.")
}

// ---------------------------------------------------------------------------
// E6. 5.4: first error only, no aggregation.
// ---------------------------------------------------------------------------

type E6Conf struct {
	A int
	B int
	C int
	D string
	E bool
}

func runE6() {
	hdr("E6  aggregate against fail-fast on Load (5.4)")

	s := mustSchema(reflect.TypeFor[E6Conf]())
	plane := map[Path]Value{
		addr("A"): String("nope"),
		addr("B"): String("also-nope"),
		addr("C"): String("still-nope"),
		addr("D"): Number("7"),
		addr("E"): String("yes"),
	}

	var v1 E6Conf
	_, ff := loadD(plane, s, reflect.ValueOf(&v1).Elem(), loadOpts{})
	fmt.Printf("  fail-fast   errors=%d   %v\n", len(Elements(ff)), ff)

	var v2 E6Conf
	sink := &errSink{}
	_, _ = loadD(plane, s, reflect.ValueOf(&v2).Elem(), loadOpts{sink: sink})
	agg := sink.result()
	fmt.Printf("  aggregating errors=%d\n", len(Elements(agg)))
	fmt.Printf("%+v\n", agg)
	fmt.Println("  Five bad fields, and the user fixes one and re-runs four more times")
	fmt.Println("  under the first. That is 5.4 in ferry's own shape.")
}

// ---------------------------------------------------------------------------
// E7 (P1). What aggregating costs a sink that cannot stage.
// ---------------------------------------------------------------------------

type E7Conf struct {
	F01, F02, F03, F04, F05, F06 string
	F07, F08, F09, F10, F11, F12 string
}

func e7Value() E7Conf {
	return E7Conf{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
}

func runE7() {
	hdr("E7  (P1) what aggregating costs a sink that cannot stage")

	s := mustSchema(reflect.TypeFor[E7Conf]())
	v := reflect.ValueOf(e7Value())
	// Two addresses the sink refuses: an early one and a late one.
	fail := map[string]bool{"/F03": true, "/F09": true}

	ffSink := &recSink{failOn: fail}
	ffErr := dumpE(v, s, ffSink, nil)
	aggSinkRec := &recSink{failOn: fail}
	aggSink := &errSink{}
	aggErr := dumpE(v, s, aggSinkRec, aggSink)

	fmt.Printf("  a 12-address struct, the sink refusing /F03 and /F09\n\n")
	fmt.Printf("  %-28s %-9s %-9s %-8s\n", "non-staging sink", "attempts", "written", "errors")
	fmt.Printf("  %-28s %-9d %-9d %-8d\n", "fail-fast", ffSink.attempts, len(ffSink.written), len(Elements(ffErr)))
	fmt.Printf("  %-28s %-9d %-9d %-8d\n", "aggregating", aggSinkRec.attempts, len(aggSinkRec.written), len(Elements(aggErr)))

	fmt.Printf("\n  write amplification: %d extra addresses reach the plane, out of 12.\n",
		len(aggSinkRec.written)-len(ffSink.written))
	fmt.Println("  And the point that decides it: fail-fast ALREADY wrote", len(ffSink.written),
		"of them, so it")
	fmt.Println("  does not protect a plane that cannot stage, it only writes less of it.")

	// A staging sink: Commit runs only on success, so aggregation is free.
	stFF := &stageSink{failOn: fail}
	_ = dumpE(v, s, stFF, nil)
	stAgg := &stageSink{failOn: fail}
	_ = dumpE(v, s, stAgg, &errSink{})
	fmt.Printf("\n  %-28s %-9s %-9s %-8s\n", "staging sink (Committer)", "attempts", "staged", "PLANE")
	fmt.Printf("  %-28s %-9d %-9d %-8d\n", "fail-fast", stFF.attempts, len(stFF.staged), len(stFF.plane))
	fmt.Printf("  %-28s %-9d %-9d %-8d\n", "aggregating", stAgg.attempts, len(stAgg.staged), len(stAgg.plane))
	fmt.Println("  Commit runs only on success (ADR-0004), so under both policies the")
	fmt.Println("  plane is untouched. Aggregating costs a staging sink nothing.")
}

// ---------------------------------------------------------------------------
// E8 (P2). A plane that dies mid-walk.
// ---------------------------------------------------------------------------

type E8Conf struct {
	A, B, C, D, E, F, G, H string
}

func runE8() {
	hdr("E8  (P2) a plane that dies mid-walk, against one that denies two keys")

	s := mustSchema(reflect.TypeFor[E8Conf]())
	full := map[Path]Value{}
	for _, n := range []string{"A", "B", "C", "D", "E", "F", "G", "H"} {
		full[addr(n)] = String(n)
	}

	// (a) the plane dies at the third Get and stays dead.
	dead := 0
	var v1 E8Conf
	sink1 := &errSink{}
	_, _ = loadD(full, s, reflect.ValueOf(&v1).Elem(), loadOpts{
		sink: sink1,
		get: func(p Path) (Value, error) {
			dead++
			if dead >= 3 {
				return Absent, errorsNew("kv: dial tcp 10.0.0.1:8500: connect: connection refused")
			}
			return full[p], nil
		},
	})
	g1 := sink1.result()
	fmt.Printf("  (a) plane dies at the third Get: %d errors from 8 addresses\n", len(Elements(g1)))
	distinct := map[string]bool{}
	for _, e := range Elements(g1) {
		var fe *Error
		if errors.As(e, &fe) {
			distinct[fe.cause.Error()] = true
		}
	}
	fmt.Printf("      distinct underlying facts: %d\n", len(distinct))
	fmt.Printf("      %v\n", g1)

	// (b) a token with read on some paths and not others. This is the Vault
	// case, and it is the reason "a Plane error stops the walk" is refused.
	var v2 E8Conf
	sink2 := &errSink{}
	denied := map[string]bool{"/C": true, "/F": true}
	_, _ = loadD(full, s, reflect.ValueOf(&v2).Elem(), loadOpts{
		sink: sink2,
		get: func(p Path) (Value, error) {
			if denied[p.String()] {
				return Absent, errorsNew("vault: permission denied")
			}
			return full[p], nil
		},
	})
	g2 := sink2.result()
	fmt.Printf("\n  (b) a token denied on two of eight paths: %d errors\n", len(Elements(g2)))
	fmt.Printf("%+v\n", g2)
	fmt.Printf("      the six readable fields loaded: %+v\n", v2)
	fmt.Println("\n  (a) is N copies of one fact and (b) is N facts, and no rule available")
	fmt.Println("  to core tells them apart: both are ErrPlane from the same driver at")
	fmt.Println("  different addresses. Stopping on the first would make (b) unreportable")
	fmt.Println("  in bulk, which is the case ferry exists to serve.")
}
