package ferrytest

import (
	"reflect"

	"github.com/onhotpath/ferry"
)

// Proof is what one Go type's round trip through ferry has been shown to be,
// and [Type] is the only way to make one.
//
// It is three columns and not one, because none of the three is derivable from
// the other two: the values to try, the equality relation those values come back
// under, and the boundary [ferry.Value] each of them must produce. Drop the
// third and a codec that writes a durable representation nobody wants still
// passes, because whatever it writes it reads back.
//
// A registrant writes one per type they register, and [CoreTypes] is the set
// ferry writes for its own types.
//
// The interface carries an unexported method, so [Type] is the only source of
// one. That is what lets the suites grow the methods they need without breaking
// every proof outside this repository.
type Proof interface {
	// Name labels the proof in a report. It is prose for a human and is not
	// how a proof is identified.
	Name() string

	// Type is the Go type this proof discharges, and it is what [Complete]
	// joins on. Two proofs may share a [Proof.Name] and mean different types,
	// so the name is never the key.
	Type() reflect.Type

	// columns hands back the two of the three columns a suite otherwise only
	// reads while running: whether a relation was supplied at all, and the
	// golden every case pins.
	//
	// It exists because [CoreTypes] is a published artefact rather than a
	// fixture (ADR-0013), so its shape has to be assertable without a plane in
	// sight: a row added with no relation, or a case added with no golden, is a
	// change to a compatibility promise and has to fail rather than pass
	// quietly. The cases are typed by a parameter no caller can name, so there
	// is no route to them from outside the proof.
	//
	// It is unexported for the same reason [Type] is the only constructor: the
	// columns are the suites' to read, not a caller's to inspect.
	columns() (hasRelation bool, goldens []ferry.Value)

	// run discharges the proof against one plane, and it is a method here
	// rather than a loop inside [RoundTrip] because the cases are typed by a
	// parameter no suite can name. Its implementation is in roundtrip.go, with
	// the suite it belongs to.
	run(h *harness)

	// refuse is the other half of [Driver]'s first case: the cases this plane
	// declared it cannot carry must be refused loudly rather than mangled. It
	// is a method here for run's reason - the cases are typed - and its
	// implementation is in driver.go, with the suite it belongs to.
	refuse(h *harness)

	// only narrows this proof to the cases whose golden satisfies pick,
	// returning a copy and leaving the receiver alone.
	//
	// It is what makes [Driver]'s first case value-granular where [Plane.Kinds]
	// is kind-granular, and the narrowing is per case rather than per proof for
	// a reason measured on driver/yaml: its plane carries three of the string
	// row's four values and cannot spell the fourth, so a proof-level answer
	// either demands a refusal of every string, which is false, or drops the
	// three that round trip, which is a silent hole in the one case that proves
	// them. A narrowed proof keeps each case's own number, so a report still
	// names the case as [CoreTypes] spells it.
	only(pick func(ferry.Value) bool) Proof

	// proof keeps [Type] the only constructor.
	proof()
}

// Type builds a proof: a name for reports, the equality relation this type comes
// back under, and the cases.
//
//	ferrytest.Type("time.Time", time.Time.Equal,
//	    ferrytest.At(when, ferry.String("2026-08-02T12:00:00Z")),
//	)
//
//	ferrytest.Type("netip.Addr", ferrytest.Eq[netip.Addr],
//	    ferrytest.At(netip.Addr{}, ferry.String("")),
//	    ferrytest.At(netip.MustParseAddr("192.0.2.1"), ferry.String("192.0.2.1")),
//	)
//
// The relation is required and there is no default, because the two defaults
// that suggest themselves are both wrong: reflect.DeepEqual is false for a
// round-tripped [time.Time], because of its monotonic reading, and false for any
// struct holding NaN, and == is wrong for the same time.Time. A harness that
// defaulted would report failures that are not failures, and the obvious repair
// is to loosen the comparison until it stops complaining. [Eq], [BitEq],
// [SliceEq], [MapEq] and [PtrEq] cover the ordinary shapes; anything else
// supplies its own func.
//
// Inference resolves T from the relation, so Type("int", Eq[int], ...) needs no
// explicit instantiation, and [time.Time.Equal] is already a func of exactly the
// required signature.
//
// The cases are load-bearing and this is exactly as good as them. A lossy float
// codec measured against a four-value row was caught by one of the four, so
// carry the zero value, both extremes, and the values that historically break
// the type.
func Type[T any](name string, eq func(a, b T) bool, cases ...Case[T]) Proof {
	return typeProof[T]{name: name, eq: eq, cases: cases}
}

// Case is one value and the boundary [ferry.Value] ferry must produce for it.
// [At] is the way to build one.
//
// Want is required and there is no way to omit it. It is what pins the
// representation, which a round trip cannot: a codec writing a duration as
// 30000000000 nanoseconds round-trips perfectly and is still the wrong thing to
// leave in somebody's config file for the next ten years.
//
// A Case with no Want is a case with no golden, and it fails rather than
// passing: the zero [ferry.Value] is Absent, which is a plane saying it does not
// hold an address, and no value ferry writes is ever that.
type Case[T any] struct {
	// Value is the Go value that goes in.
	Value T

	// Want is the boundary Value ferry must produce for it.
	Want ferry.Value
}

// At builds a [Case] out of a value and the golden it must produce: at this
// value, this representation.
//
//	ferrytest.At(netip.MustParseAddr("192.0.2.1"), ferry.String("192.0.2.1"))
//
// It is a different function from ferry.At, which builds an address out of
// field names. The two appear side by side in a proof and are told apart by
// their package.
func At[T any](value T, want ferry.Value) Case[T] {
	return Case[T]{Value: value, Want: want}
}

// typeProof is the only implementation of [Proof]. It holds the three columns
// and hands out the two that a suite with no type parameter in hand can read.
type typeProof[T any] struct {
	name  string
	eq    func(a, b T) bool
	cases []Case[T]

	// pick is [Proof.only]'s narrowing, and nil is every case. It is a field
	// rather than a parameter of run and refuse so that the case list keeps its
	// own numbering: a filtered slice renumbers, and a report naming case 2 has
	// to mean the case [CoreTypes] wrote third.
	pick func(ferry.Value) bool
}

// Name is the label this proof was built with.
func (p typeProof[T]) Name() string { return p.name }

// Type recovers the type parameter, which is the thing the prototype's own
// comment said could not be recovered.
func (typeProof[T]) Type() reflect.Type { return reflect.TypeFor[T]() }

// columns reads the relation's presence and the golden column, which is the
// whole of what a proof can say about itself without a plane to run against.
func (p typeProof[T]) columns() (bool, []ferry.Value) {
	goldens := make([]ferry.Value, 0, len(p.cases))
	for _, c := range p.cases {
		goldens = append(goldens, c.Want)
	}

	return p.eq != nil, goldens
}

// only copies the proof with a narrowing attached. The receiver is a value, so
// the proof it was called on is unchanged and one proof can be narrowed twice
// into two complementary halves.
func (p typeProof[T]) only(pick func(ferry.Value) bool) Proof {
	p.pick = pick

	return p
}

// picked reports whether one case is inside this proof's narrowing, and an
// unnarrowed proof picks everything.
func (p typeProof[T]) picked(want ferry.Value) bool { return p.pick == nil || p.pick(want) }

func (typeProof[T]) proof() {}
