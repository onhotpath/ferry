package main

// The round-trip property harness, as ADR-0001 requires it: a public testing
// package, route (b) authority, a table over a closed enumerated set, and no
// property-testing dependency (ADR-0002).
//
// The design question this probe exists to answer: a table over a closed set
// is heterogeneous in its element type, so it cannot be a []Case[T]. And the
// equality relation cannot be == or reflect.DeepEqual, because time.Time and
// NaN both fail it. So the equality relation has to be part of each entry.

// #41 item 6, and it is the highest-value row in the audit's own list.
//
// This harness called dump() and load() from walk.go, the SUPERSEDED walk. So
// ADR-0005's "11 of 11 core types, 10 of 10 composites, on three planes" is a
// statement about code the engine no longer uses, and the walk every ADR from
// 0007 onward measured had never been round-trip property-tested. It now runs
// through `Dump` and `Load` - the entry points - like everything else.
//
// The shape stays ADR-0005's on purpose, so #35 can lift it: CoreTypes() is the
// table, Plane is the Source/Sink pair plus its declared kinds, and RoundTrip
// runs one against the other. Nothing here proposes public surface; #35 owns
// what ferrytest exports.

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"
)

// Plane is ADR-0005's, named rather than spelled as a pair of parameters
// "because a driver supplying two unrelated planes is the mistake it exists to
// prevent". Kinds is the declaration the same ADR requires:
//
//	A driver declares the `Value` kinds its plane can carry. The conformance
//	suite runs the proofs that plane can express, and asserts that the ones it
//	cannot are refused loudly rather than silently mangled.
type Plane struct {
	Name  string
	Kinds []VKind

	// Open hands back a fresh Source and Sink over ONE storage. It is a
	// function rather than two fields because a round trip needs a new plane
	// per value, and because the alternative - reset the same one - hides
	// whether a proof is reading its own leftovers.
	Open func() (FSource, FSink)
}

func (p Plane) carries(k VKind) bool { return slices.Contains(p.Kinds, k) }

// allKinds is what a plane with somewhere to put the kind declares.
var allKinds = []VKind{VAbsent, VNull, VBool, VNumber, VString, VBytes}

// Proof is one type's discharged obligation. It is an interface with an
// unexported method so the only way to make one is the typed constructor.
//
// run returns two lists, and the split is ADR-0005's: a FAILURE is the plane
// getting a proof wrong, and a REFUSAL is the plane declining a kind it already
// declared it cannot carry, which is a property rather than a failure.
type Proof interface {
	run(p Plane) (fails, refused []string)
	Name() string
}

type proof[T any] struct {
	name   string
	eq     func(a, b T) bool
	values []T
}

func (p proof[T]) Name() string { return p.name }

// Type is the constructor. The equality relation is required rather than
// defaulted, because every type whose relation is not == is a type whose
// round trip has a carve-out that somebody has to have thought about.
func Type[T any](name string, eq func(a, b T) bool, values ...T) Proof {
	return proof[T]{name, eq, values}
}

// Eq is the relation for a type whose == is the right one.
func Eq[T comparable](a, b T) bool { return a == b }

// BitEq is the relation for floats: NaN != NaN under ==, but a round trip
// that preserves the bit pattern has preserved the value.
func BitEq[T ~float32 | ~float64](a, b T) bool {
	return math.Float64bits(float64(a)) == math.Float64bits(float64(b))
}

// SliceEq lifts a relation over a slice, distinguishing nil from empty.
// SliceEq treats nil and empty as one value, which is ferry's decision rather
// than a convenience: a composite with no elements mints no element address,
// and no plane surveyed can report "present and empty" at a container address.
func SliceEq[T any](eq func(a, b T) bool) func(a, b []T) bool {
	return func(a, b []T) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if !eq(a[i], b[i]) {
				return false
			}
		}
		return true
	}
}

func (p proof[T]) run(pl Plane) (fails, refused []string) {
	ctx := context.Background()
	for i, v := range p.values {
		// Every proof runs through a one-field struct, because a leaf is
		// only reachable at an address, and an address only exists inside
		// a schema.
		//
		// HARNESS DEFECT #1, found by repointing at the engine. The field
		// carried NO ferry tag, because the superseded walk fell back to the Go
		// field name. ADR-0008 refuses that by decision - "every mapped field
		// names its segment explicitly" - so the engine reported
		// `ferry: /V: field V carries no ferry tag` for all 21 proofs on all
		// three planes. `ferry:"V"` keeps the address ADR-0005 publishes.
		type holder struct {
			V T `ferry:"V"`
		}
		in := holder{v}
		src, sink := pl.Open()

		// THROUGH THE ENTRY POINT. This is the whole of #41 item 6: the two
		// lines that used to be dump()/load() over walk.go.
		if err := Dump(ctx, in, sink); err != nil {
			if k, ok := refusedKind(err); ok {
				if pl.carries(k) {
					// The plane declared it carries this kind and then refused
					// it, which is a driver contradicting its own declaration.
					fails = append(fails, fmt.Sprintf(
						"%s[%d]: plane declares it carries %s and refused it: %v", p.name, i, k, err))
					continue
				}
				refused = append(refused, fmt.Sprintf("%s[%d] writes %s", p.name, i, k))
				continue
			}
			fails = append(fails, fmt.Sprintf("%s[%d]: dump: %v", p.name, i, err))
			continue
		}
		out, err := Load[holder](ctx, src)
		if err != nil {
			fails = append(fails, fmt.Sprintf("%s[%d]: load: %v", p.name, i, err))
			continue
		}
		if !p.eq(in.V, out.V) {
			fails = append(fails, fmt.Sprintf("%s[%d]: %#v -> %#v", p.name, i, in.V, out.V))
		}
	}
	return fails, refused
}

// RoundTrip is ADR-0005's verb, minus the *testing.T the prototype has no use
// for. It reports rather than asserts, which is also what makes it usable from
// a probe.
//
// A proof PASSES when nothing it could express went wrong. A refusal is not a
// failure - it is the plane's own declaration being honoured - but it is
// counted and printed per value, because "8 of 10 composites" and "10 of 10
// composites with 8 values the plane cannot carry" are different sentences and
// ADR-0005's own numbers turn on which one was measured.
type RTResult struct {
	Pass, Fail int // proofs
	Refused    int // VALUES the plane declared it cannot carry
	Lines      []string
}

func RoundTrip(pl Plane, proofs ...Proof) RTResult {
	var r RTResult
	for _, pr := range proofs {
		f, ref := pr.run(pl)
		r.Refused += len(ref)
		if len(f) > 0 {
			r.Fail++
			r.Lines = append(r.Lines, fmt.Sprintf("  FAIL %-20s", pr.Name()))
			for _, s := range f {
				r.Lines = append(r.Lines, "       "+s)
			}
			continue
		}
		r.Pass++
		note := ""
		if len(ref) > 0 {
			note = fmt.Sprintf("   (%d value(s) refused: %s)", len(ref), ref[0])
		}
		r.Lines = append(r.Lines, fmt.Sprintf("  ok   %-20s%s", pr.Name(), note))
	}
	return r
}

func (r RTResult) summary() string {
	s := fmt.Sprintf("%d/%d proofs pass", r.Pass, r.Pass+r.Fail)
	if r.Refused > 0 {
		s += fmt.Sprintf(", %d value(s) refused by the plane's own declaration", r.Refused)
	}
	return s
}

// CoreTypes is ADR-0005's name for the table below, so the shape #35 lifts is
// recognisable. It is deliberately a function and not a var: the table holds
// time.Time values built from a loaded location.
func CoreTypes() []Proof { return coreSet() }

// failsOn is run's first return alone, for the inherited call sites that only
// ever asked about failures. A plane that declares every kind can never
// produce a refusal, so for the memory and YAML planes the two are the same
// question.
func failsOn(pr Proof, pl Plane) []string { f, _ := pr.run(pl); return f }

// coreSet is the table. Adding a type means adding a row, and a row cannot be
// written without naming the relation under which the type round-trips.
func coreSet() []Proof {
	ny, _ := time.LoadLocation("America/New_York")
	return []Proof{
		Type("bool", Eq[bool], true, false),
		Type("string", Eq[string], "", "a", "b,c", "\x00", "héllo", "  "),
		Type("int", Eq[int], 0, 1, -1, math.MaxInt, math.MinInt),
		Type("int8", Eq[int8], 0, math.MaxInt8, math.MinInt8),
		Type("uint64", Eq[uint64], 0, math.MaxUint64),
		Type("float64", BitEq[float64], 0, math.Copysign(0, -1), 0.1, 1.0/3.0,
			math.MaxFloat64, math.SmallestNonzeroFloat64,
			math.Inf(1), math.Inf(-1), math.NaN()),
		Type("float32", BitEq[float32], 0, 0.1, math.MaxFloat32),
		Type("[]byte", SliceEq(Eq[byte]), nil, []byte{}, []byte{0x00, 0xff, 0x41}),
		Type("time.Duration", Eq[time.Duration], 0, time.Second, 90*time.Minute, -time.Second),
		// The one entry whose relation is not ==, and the stdlib says so:
		// "In general, prefer t.Equal(u) to t == u" (time/time.go:136).
		Type("time.Time", time.Time.Equal,
			time.Time{},
			time.Unix(0, 0).UTC(),
			time.Date(2026, 8, 2, 12, 0, 0, 0, ny),
			time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC),
		),
		Type("[]string", SliceEq(Eq[string]), nil, []string{}, []string{"a", "b,c", ""}),
	}
}
