package ferrytest

import (
	"reflect"

	"github.com/onhotpath/ferry"
)

// Proof is one type's discharged obligation, and the only way to make one is
// [Type].
//
// ADR-0001 makes a registration carry its own proof rather than core promising
// something about a type it has never seen, so a proof is what a registrant
// writes and what core writes for its own set. It is three columns and not one
// - values, an equality relation, and the boundary [ferry.Value] each value
// must produce - because none of the three is derivable from the other two.
//
// The interface carries an unexported method, so this package's constructor is
// the only source of one. That is what lets the suites grow the methods they
// need without every proof outside this repository breaking, and it is the same
// reason ADR-0011 exports the name [ferry.Error] and none of its fields.
//
// # Type is a method because the completeness check joins by type
//
// ADR-0014's completeness check runs over the union of three tables - core's
// identity table, one representative type per admitted kind, and a registry -
// and it has to decide whether a proof discharges a member. Joining by Name is
// the obvious thing and it is wrong: the prototype that did it needed a
// hand-written special case spelling a fixed-size byte array so that a proof
// named "[]byte" could discharge an array member, and its own comment claimed
// the type could not be recovered from a proof because Proof is an interface
// over a type parameter. It can, and one method does it.
//
// So Name is a label for a report and never a key. Two proofs may share a name
// and mean different types, and the join will not be confused by it.
type Proof interface {
	// Name labels the proof in a report. It is prose for a human and is not
	// how a proof is identified.
	Name() string

	// Type is the Go type this proof discharges, which is what a completeness
	// check joins on.
	Type() reflect.Type

	// proof keeps [Type] the only constructor.
	proof()
}

// Type builds a proof: a name, the equality relation for this type, and the
// cases.
//
// The relation is positional and required rather than defaulted, and that is
// the enforcement half of survey item 5.7. reflect.DeepEqual is the wrong
// relation and so is ==: measured, DeepEqual of a time.Time against its own
// round trip is false because of the monotonic reading, and DeepEqual of two
// structs holding NaN is false as well. A harness that defaulted to it would
// report false failures for time.Time and for every float, and the obvious
// repair is to loosen the comparison until it stops complaining. Taking the
// relation positionally means there is no fallback to reach for: a type whose
// relation is not == is a type whose round trip has a carve-out somebody has to
// have thought about.
//
// Two ergonomic facts, established by compiling rather than by reasoning.
// time.Time.Equal is a method expression of exactly func(time.Time, time.Time)
// bool, so the one entry in core's set whose relation is not == needs no
// wrapper. And inference resolves T from the relation, so Type("int", Eq[int],
// ...) needs no explicit instantiation.
//
//	ferrytest.Type("time.Time", time.Time.Equal,
//	    ferrytest.At(when, ferry.String("2026-08-02T12:00:00Z")),
//	)
func Type[T any](name string, eq func(a, b T) bool, cases ...Case[T]) Proof {
	return typeProof[T]{name: name, eq: eq, cases: cases}
}

// Case is one value and the boundary [ferry.Value] ferry must produce for it.
//
// The golden is required rather than optional, and it is the column that makes
// the proof more than a property. The round-trip property does not constrain
// the representation at all: measured, replacing time.Duration's codec with the
// nanosecond shape ADR-0005 rejects by name, so that thirty seconds is written
// as 30000000000, the property reports zero failures, because nanoseconds
// round-trip perfectly. A property harness alone would have let ferry ship the
// exact representation it rejects three sections above.
//
// It is cheap only because [ferry.Value] is comparable: the check is an == over
// a 24-byte struct, with no serialisation and no bespoke comparison.
//
// The values in a proof are load-bearing, and the harness is exactly as good as
// them. Measured with a lossy float64 codec formatting at six digits, the table
// caught one of four values: 1.0/3.0 failed and 0.1, MaxFloat64 and NaN all
// passed, because a fixed six-digit format happens to be lossless for those
// three. So a proof carries the type's zero value, its extremes, and the values
// that historically break it (ADR-0005).
//
// [At] is the way to build one, and a Case with no Want is a case with no
// golden: the zero [ferry.Value] is Absent, which is a plane reporting that it
// does not have an address, and no value ferry encodes is ever that.
type Case[T any] struct {
	// Value is the Go value that goes in.
	Value T

	// Want is the boundary Value ferry must produce for it.
	Want ferry.Value
}

// At builds a [Case] out of a value and the golden it must produce.
//
// It is named for how the call site reads - at this value, this representation
// - and it is a different function from ferry.At, which builds an address out
// of segment names. The two appear side by side in a proof and are told apart
// by their package.
func At[T any](value T, want ferry.Value) Case[T] {
	return Case[T]{Value: value, Want: want}
}

// typeProof is the only implementation of [Proof]. It holds the three columns
// and hands out the two that a suite with no type parameter in hand can read.
type typeProof[T any] struct {
	name  string
	eq    func(a, b T) bool
	cases []Case[T]
}

// Name is the label this proof was built with.
func (p typeProof[T]) Name() string { return p.name }

// Type recovers the type parameter, which is the thing the prototype's own
// comment said could not be recovered.
func (typeProof[T]) Type() reflect.Type { return reflect.TypeFor[T]() }

func (typeProof[T]) proof() {}
