package ferrytest

import (
	"context"
	"fmt"

	"github.com/onhotpath/ferry"
)

// RoundTrip runs every proof against one plane: each case's value is dumped,
// what ferry encoded is compared against the case's golden, and the value is
// loaded back and compared under the proof's own relation.
//
//	ferrytest.RoundTrip(t, ferrytest.MemPlane(), proofs)
//
// # It reaches the plane through [ferry.Dump] and [ferry.Load] and by no other
// route
//
// That is the whole of what this function is for, rather than a detail of how
// it is written. A harness with its own walk measures its own walk: one
// prototype resolved a registered key codec at compile and never called it,
// because a second walk re-derived the key text against a package global the
// caller-facing verbs do not install, and every measurement taken through that
// harness was honest and meaningless. Two more implemented halves of this
// harness, and the half that ran through the engine could not see a
// representation at all, so the golden column ADR-0005 calls the whole reason a
// proof is a triple had never once executed against the engine.
//
// So there is no internal shortcut here, not even one that is equivalent today,
// and the golden is read through a wrapping sink because that is the only
// position from which what ferry encoded is visible before a driver spells it.
//
// # What it does not do
//
// It does not consult [Plane.Kinds]. Running the proofs a plane can express and
// demanding a loud refusal for the ones it declared it cannot carry is
// ADR-0014's Driver case 1, and it is one case with two halves: a suite that
// skipped the unexpressible kinds without asserting the refusal would turn a
// flattening driver's data loss into a silence. Driver owns both halves. This
// function runs what it is given, which is what a registrant proving one codec
// against the memory plane wants.
//
// # What the Option list may not do
//
// The proofs are a slice rather than a variadic tail, which is the whole price
// of a registry reaching the harness as a [ferry.Option] instead of as a
// parameter: ADR-0014 takes that so ferrytest adds no second way to say what an
// Option already says, and a core type table returns a slice anyway.
//
// One Option cannot be honoured, and it is refused rather than half-applied. A
// proof's value is a bare value, so the harness supplies the annotated struct
// it travels in, and [ferry.TagKey] names the key ferry reads for every type in
// the run - that struct included. A key that is not the one the harness wrote
// its own wrapper under leaves it unable to compile, and that is reported once
// here rather than as an identical dump failure per case. It is a real
// limitation of this signature rather than a decision: an Option is opaque, so
// the harness cannot apply one to a caller's type and not to its own.
func RoundTrip(t T, p Plane, proofs []Proof, opts ...ferry.Option) {
	t.Helper()

	if p.Open == nil {
		t.Errorf("plane %s: Open is nil, so there is no plane to round trip through", p.Name)

		return
	}

	// The Option list, resolved once and against the harness's own wrapper, so
	// an Option list that is simply wrong is one report rather than one per
	// case.
	if err := ferry.Compile[holder[string]](opts...); err != nil {
		t.Errorf("plane %s: these Options leave RoundTrip unable to compile the struct it dumps a case in: %v",
			p.Name, err)

		return
	}

	h := &harness{rep: t, plane: p, opts: opts}
	for _, pr := range proofs {
		pr.run(h)
	}
}

// harness is one RoundTrip call, carried down to the cases.
//
// It exists because a proof's cases are reachable only from inside the generic
// that holds them, so the run has to be a method on [typeProof] and everything
// the run needs has to travel with it. One struct rather than four parameters
// keeps that method inside this repository's argument limit and gives the
// report label one place to be built. It travels by pointer because a [Plane]
// description is a wide value and the harness is read-only either way.
type harness struct {
	rep   reporter
	plane Plane
	opts  []ferry.Option
}

// label is what every failure this harness reports is prefixed with: the plane,
// the proof and which case.
//
// The plane is in it because the ordinary call site is a loop over planes, so a
// report that named only the proof would not say which driver went red.
func (h *harness) label(name string, i int) string {
	return fmt.Sprintf("plane %s: %s: case %d", h.plane.Name, name, i)
}

// run is [Proof]'s half of RoundTrip, and it is a method on the proof because
// the cases are typed by a parameter no suite can name.
func (p typeProof[T]) run(h *harness) {
	h.rep.Helper()

	// There is no default relation to fall back on - ADR-0005 refuses one, and
	// [Type] takes it positionally so that there is nothing to reach for - so a
	// nil one is reported here rather than panicking part way through a suite
	// that is running against somebody else's driver.
	if p.eq == nil {
		h.rep.Errorf("plane %s: %s: the proof carries no relation, and there is no default to fall back on",
			h.plane.Name, p.name)

		return
	}

	for i, c := range p.cases {
		p.runCase(h, i, c)
	}
}

// runCase is one case, against a plane minted for it alone.
//
// The instance is fresh per case, and that is ADR-0014's own rule rather than
// tidiness: a destination shared across equivalence subtests is the defect that
// hides a broken second walk, since the second case reads what the first one
// wrote.
func (p typeProof[T]) runCase(h *harness, i int, c Case[T]) {
	h.rep.Helper()

	inst := h.plane.Open()

	// A plane with no honest Dump - environment variables are ADR-0004's case -
	// has no sink, and it is reported here rather than once for the whole run:
	// checking it up front would mean minting an instance and throwing it away
	// on every call, and for a file-backed plane that is a temporary file
	// created for a question.
	if inst.Sink == nil {
		h.rep.Errorf("%s: the plane mints no sink, so there is nothing for a dump to write to", h.label(p.name, i))

		return
	}

	rec := recording(inst.Sink)
	if err := ferry.Dump(context.Background(), holder[T]{Value: c.Value}, rec, h.opts...); err != nil {
		h.rep.Errorf("%s: dump: %v", h.label(p.name, i), err)

		return
	}

	p.checkGolden(h, i, rec.seen[holderAddr], c.Want)
	p.checkTrip(h, i, inst, c)
}

// checkGolden is the column a round trip cannot see.
//
// The comparison is ==, which [ferry.Value] supports because it is a comparable
// 24-byte struct, so the kind is asserted along with the text and there is no
// bespoke comparison to get wrong. An address ferry never wrote reads back as
// the zero Value, which is Absent, and Absent is a plane reporting that it does
// not hold an address - so "encoded absent" is a true and distinct report of a
// dump that wrote nothing here.
func (p typeProof[T]) checkGolden(h *harness, i int, got, want ferry.Value) {
	h.rep.Helper()

	if got == want {
		return
	}

	h.rep.Errorf("%s: ferry encoded %#v at %s, want %#v", h.label(p.name, i), got, holderAddr, want)
}

// checkTrip loads the case back and compares under the proof's relation.
//
// The relation is the proof's and never this harness's, which is ADR-0005's
// decision and survey item 5.7's enforcement: reflect.DeepEqual reports a false
// failure for a round-tripped time.Time and for any struct holding a NaN, and a
// harness that reported those would be repaired by loosening its comparison
// until it stopped complaining.
func (p typeProof[T]) checkTrip(h *harness, i int, inst Instance, c Case[T]) {
	h.rep.Helper()

	back, err := ferry.Load[holder[T]](context.Background(), inst.Source, h.opts...)
	if err != nil {
		h.rep.Errorf("%s: load: %v", h.label(p.name, i), err)

		return
	}

	if p.eq(back.Value, c.Value) {
		return
	}

	h.rep.Errorf("%s: loaded %#v, want %#v", h.label(p.name, i), back.Value, c.Value)
}

// holder is the struct a case's value travels in.
//
// A proof's type is usually a leaf, and ADR-0010 refuses a root that compiles
// to one: the empty path is not an address, and a root leaf measured without
// the refusal wrote "{}" with a nil error, so the value was silently and
// totally lost. Wrapping it in a struct is what that refusal names as the whole
// remedy, so the harness writes the wrapper once rather than every proof
// carrying its own.
type holder[T any] struct {
	// The tag must spell holderAddr's one segment. They are two spellings of
	// one name because a struct tag cannot reference a constant, and nothing
	// but a test keeps them together: a proof that ferry encoded absent where
	// its golden expected a value is what drift between them looks like, so
	// every green round trip in this package's own tests is the check.
	Value T `ferry:"value"`
}

// holderAddr is where a [holder]'s one field lands, and it is the address the
// golden column is read at.
var holderAddr = ferry.At("value")
