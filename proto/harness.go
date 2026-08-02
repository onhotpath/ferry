package main

// The round-trip property harness, as ADR-0001 requires it: a public testing
// package, route (b) authority, a table over a closed enumerated set, and no
// property-testing dependency (ADR-0002).
//
// The design question this probe exists to answer: a table over a closed set
// is heterogeneous in its element type, so it cannot be a []Case[T]. And the
// equality relation cannot be == or reflect.DeepEqual, because time.Time and
// NaN both fail it. So the equality relation has to be part of each entry.

import (
	"fmt"
	"math"
	"reflect"
	"time"
)

// Proof is one type's discharged obligation. It is an interface with an
// unexported method so the only way to make one is the typed constructor.
type Proof interface {
	run(plane func(map[Path]Value) (map[Path]Value, error)) []string
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

func (p proof[T]) run(plane func(map[Path]Value) (map[Path]Value, error)) []string {
	var fails []string
	for i, v := range p.values {
		// Every proof runs through a one-field struct, because a leaf is
		// only reachable at an address, and an address only exists inside
		// a schema.
		type holder struct{ V T }
		in := holder{v}
		dumped, err := dump(reflect.ValueOf(in))
		if err != nil {
			fails = append(fails, fmt.Sprintf("%s[%d]: dump: %v", p.name, i, err))
			continue
		}
		crossed, err := plane(dumped)
		if err != nil {
			fails = append(fails, fmt.Sprintf("%s[%d]: plane: %v", p.name, i, err))
			continue
		}
		var out holder
		if err := load(crossed, reflect.ValueOf(&out).Elem()); err != nil {
			fails = append(fails, fmt.Sprintf("%s[%d]: load: %v", p.name, i, err))
			continue
		}
		if !p.eq(in.V, out.V) {
			fails = append(fails, fmt.Sprintf("%s[%d]: %#v -> %#v", p.name, i, in.V, out.V))
		}
	}
	return fails
}

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
