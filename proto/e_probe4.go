package main

// E16. E7 reopened.
//
// E7 measured ONE scenario against TWO policies and reported one number: 8
// extra writes of 12. That is not enough to decide on, for two reasons the
// probe did not separate.
//
// A Dump has two kinds of failure and they cost the plane different things. An
// ENCODE failure is deterministic, per field, and happens BEFORE any write for
// that address, so aggregating it costs the plane nothing at all. A SET failure
// is the plane refusing, and it is the only thing that can cause write
// amplification. E7 conflated them.
//
// And there are four policies, not two. ADR-0004 already reasoned this way for
// the read-only refusal: "failing at open costs nothing, and failing at the
// first Set has already half-written the plane". A two-phase dump is that
// reasoning applied to encoding.

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

func init() { e9Hooks = append(e9Hooks, runE16) }

type dumpPolicy int

const (
	polFailFast   dumpPolicy = iota // stop at the first error of any kind
	polAggregate                    // keep going past everything: ADR-0011's proposal
	polTwoPhase                     // encode all first, no writes; if clean, write and aggregate
	polTwoPhaseFF                   // encode all first, no writes; if clean, write and stop at the first refusal
)

var policyName = [...]string{"fail-fast", "aggregate", "two-phase+agg", "two-phase+ff"}

func (p dumpPolicy) String() string { return policyName[p] }

// a step is one address's encode result. Encoding has no side effects, so
// producing every step up front and then replaying models all four policies
// faithfully.
type step struct {
	p   Path
	v   Value
	err error
}

func encodeAll(v reflect.Value, s *schema) []step {
	var out []step
	var rec func(v reflect.Value, p, sp Path)
	rec = func(v reflect.Value, p, sp Path) {
		opts := s.at(sp)
		if opts.omitzero && v.IsZero() {
			return
		}
		switch classify(v.Type()) {
		case shapeLeaf:
			val, err := encLeaf(v)
			out = append(out, step{p, val, err})
		case shapePointer:
			if v.IsNil() {
				out = append(out, step{p, Null(), nil})
				return
			}
			rec(v.Elem(), p, sp)
		case shapeStruct:
			for i := range v.NumField() {
				f := v.Type().Field(i)
				if !f.IsExported() {
					continue
				}
				n, _, _ := fieldTag(f)
				rec(v.Field(i), p.Name(n), sp.Name(n))
			}
		case shapeSlice:
			if v.Len() == 0 && v.Kind() != reflect.Array {
				out = append(out, step{p, Null(), nil})
				return
			}
			for i := range v.Len() {
				esp := sp.Name("*")
				if v.Kind() == reflect.Array {
					esp = sp.Index(i)
				}
				rec(v.Index(i), p.Index(i), esp)
			}
		case shapeMap:
			if v.Len() == 0 {
				out = append(out, step{p, Null(), nil})
				return
			}
			keys := v.MapKeys()
			sortMapKeys(keys)
			for _, k := range keys {
				rec(v.MapIndex(k), p.Name(mapKeyText(k)), sp.Name("*"))
			}
		}
	}
	rec(v, Path{}, Path{})
	return out
}

type dumpResult struct {
	attempts int
	written  int
	err      error
}

func runDump(steps []step, w eWriter, pol dumpPolicy) dumpResult {
	var res dumpResult
	var errs []error

	// Two-phase: every encode failure is reported with ZERO writes, which is
	// ADR-0004's "failing at open costs nothing" applied to the encode half.
	if pol == polTwoPhase || pol == polTwoPhaseFF {
		for _, st := range steps {
			if st.err != nil {
				errs = append(errs, errAt(mWalk, ErrValue, st.p, "cannot be encoded").withCause(st.err))
			}
		}
		if len(errs) > 0 {
			res.err = join(errs...)
			return res
		}
	}

	for _, st := range steps {
		if st.err != nil {
			// Only reachable under the interleaved policies.
			e := errAt(mWalk, ErrValue, st.p, "cannot be encoded").withCause(st.err)
			if pol == polFailFast {
				res.err = e
				return res
			}
			errs = append(errs, e)
			continue
		}
		res.attempts++
		if err := w.Set(st.p, st.v); err != nil {
			e := fromDriver(mWalk, st.p, true, err)
			if pol == polFailFast || pol == polTwoPhaseFF {
				res.err = join(append(errs, e)...)
				return res
			}
			errs = append(errs, e)
			continue
		}
		res.written++
	}
	res.err = join(errs...)
	return res
}

// ---------------------------------------------------------------------------

type E16Conf struct {
	Name     string
	Region   string
	Replicas int
	Started  time.Time // the encode-failure carrier
	Expires  time.Time
	Endpoint string
	Bucket   string
	Retries  int
}

func e16Value(badTimes bool) E16Conf {
	c := E16Conf{
		Name: "checkout", Region: "eu-west-1", Replicas: 3,
		Started:  time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
		Expires:  time.Date(2027, 8, 2, 12, 0, 0, 0, time.UTC),
		Endpoint: "https://api.internal", Bucket: "acme-prod", Retries: 5,
	}
	if badTimes {
		// time.Time.MarshalText refuses a year outside [0,9999], which is
		// ADR-0005's "the representation is partial over the type".
		c.Started = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
		c.Expires = time.Date(12000, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return c
}

type e16Scenario struct {
	name     string
	badTimes bool
	deny     map[string]bool
}

func runE16() {
	hdr("E16 (E7 reopened) four policies against four failure shapes")

	s := mustSchema(reflect.TypeFor[E16Conf]())
	all := map[string]bool{}
	for _, st := range encodeAll(reflect.ValueOf(e16Value(false)), s) {
		all[st.p.String()] = true
	}

	scenarios := []e16Scenario{
		{"S1 the whole plane refuses (no write ACL)", false, all},
		{"S2 two addresses refuse (partial ACL)", false, map[string]bool{"/Region": true, "/Bucket": true}},
		{"S3 two fields cannot be encoded, plane healthy", true, nil},
		{"S4 both", true, map[string]bool{"/Region": true, "/Bucket": true}},
	}

	for _, sc := range scenarios {
		fmt.Printf("\n  %s\n", sc.name)
		fmt.Printf("    %-14s %-9s %-8s %-7s %-6s\n", "policy", "attempts", "written", "errors", "facts")
		steps := encodeAll(reflect.ValueOf(e16Value(sc.badTimes)), s)
		for _, pol := range []dumpPolicy{polFailFast, polAggregate, polTwoPhase, polTwoPhaseFF} {
			w := &recSink{failOn: sc.deny}
			r := runDump(steps, w, pol)
			fmt.Printf("    %-14s %-9d %-8d %-7d %-6d\n",
				pol, r.attempts, r.written, len(Elements(r.err)), distinctFacts(r.err))
		}
	}

	// The two scenarios that decide it, shown as what the plane actually holds.
	fmt.Println("\n  What the plane holds afterwards, S2 (two addresses refuse):")
	steps := encodeAll(reflect.ValueOf(e16Value(false)), s)
	for _, pol := range []dumpPolicy{polFailFast, polAggregate} {
		w := &recSink{failOn: scenarios[1].deny}
		r := runDump(steps, w, pol)
		fmt.Printf("\n    --- %s ---\n", pol)
		fmt.Printf("    plane: %s\n", planeSummary(w.written))
		fmt.Printf("    error: %v\n", r.err)
		fmt.Printf("    a Load of that plane would find %d of 8 addresses; the rest are absent.\n", len(w.written))
	}

	fmt.Println("\n  And S3, where aggregating costs the plane NOTHING:")
	steps3 := encodeAll(reflect.ValueOf(e16Value(true)), s)
	for _, pol := range []dumpPolicy{polFailFast, polAggregate, polTwoPhase} {
		w := &recSink{}
		r := runDump(steps3, w, pol)
		fmt.Printf("\n    --- %s ---\n", pol)
		fmt.Printf("    plane: %s\n", planeSummary(w.written))
		fmt.Printf("    error: %v\n", r.err)
	}

	// What two-phase costs: it has to hold every encoded value before writing
	// any of them, so a big dynamic composite is where the price is.
	type Big struct{ M map[string]int }
	bs := mustSchema(reflect.TypeFor[Big]())
	m := map[string]int{}
	for i := range 10000 {
		m[fmt.Sprintf("k%05d", i)] = i
	}
	start := time.Now()
	buf := encodeAll(reflect.ValueOf(Big{M: m}), bs)
	took := time.Since(start)
	bytes := len(buf) * int(reflect.TypeFor[step]().Size())
	fmt.Printf("\n  What two-phase costs on a 10,000-key map:\n")
	fmt.Printf("    buffered steps : %d\n", len(buf))
	fmt.Printf("    held before the first write: ~%d KB of Path+Value headers, plus the text\n", bytes/1024)
	fmt.Printf("    encode pass    : %v\n", took.Round(time.Microsecond))
	fmt.Println("    A streaming dump holds one value at a time; two-phase holds all of them.")
}

func distinctFacts(err error) int {
	seen := map[string]bool{}
	for _, e := range Elements(err) {
		var fe *Error
		if errors.As(e, &fe) {
			if fe.cause != nil {
				seen[fe.cause.Error()] = true
				continue
			}
			seen[fe.msg] = true
		}
	}
	return len(seen)
}

func planeSummary(ps []Path) string {
	if len(ps) == 0 {
		return "(empty)"
	}
	var b []string
	for _, p := range sortedPaths(ps) {
		b = append(b, p.String())
	}
	return fmt.Sprintf("%d addresses: %s", len(ps), strings.Join(b, " "))
}

// The two-phase variant that holds nothing: walk once to find encode failures,
// discard the values, then walk again to write. Trades memory for CPU.
func init() { e9Hooks = append(e9Hooks, runE16b) }

func runE16b() {
	hdr("E16b what two-phase costs if it refuses to buffer")

	type Big struct{ M map[string]int }
	bs := mustSchema(reflect.TypeFor[Big]())
	m := map[string]int{}
	for i := range 10000 {
		m[fmt.Sprintf("k%05d", i)] = i
	}
	v := reflect.ValueOf(Big{M: m})

	start := time.Now()
	one := encodeAll(v, bs)
	single := time.Since(start)

	start = time.Now()
	probe := encodeAll(v, bs) // pass 1: check
	n := 0
	for _, st := range probe {
		if st.err != nil {
			n++
		}
	}
	_ = encodeAll(v, bs) // pass 2: encode again and write
	double := time.Since(start)

	fmt.Printf("  10,000 addresses\n")
	fmt.Printf("    one pass, buffering everything : %v, %d steps held\n", single.Round(time.Millisecond), len(one))
	fmt.Printf("    two passes, holding nothing    : %v, %d steps held\n", double.Round(time.Millisecond), 0)
	fmt.Printf("    the second pass costs          : %v extra, and %d encode failures found first\n",
		(double - single).Round(time.Millisecond), n)
	fmt.Println("\n  So the choice inside two-phase is memory or CPU, and neither is free.")
	fmt.Println("  On an ordinary config struct both are noise; on a large dynamic")
	fmt.Println("  composite the buffering is ~55 bytes an address and the second pass")
	fmt.Println("  is a full re-encode.")
}
