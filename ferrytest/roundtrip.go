package ferrytest

import (
	"fmt"
	"slices"

	"github.com/onhotpath/ferry"
)

// RoundTrip runs every proof against one plane: each case's value is dumped,
// what ferry encoded is compared against the case's golden, and the value is
// loaded back and compared under the proof's own relation.
//
//	ferrytest.RoundTrip(t, ferrytest.MemPlane(), proofs)
//
// It reaches the plane through [ferry.Dump] and [ferry.Load] and by no other
// route, so what it measures is the engine a caller uses rather than a second
// walk written to resemble it.
//
// This is the call a codec author makes. [Driver] calls it in turn, so a driver
// author gets it for free and need not call it separately.
//
// # What it does not do
//
// It does not consult [Plane.Kinds]. It runs every case it is handed, which is
// what a codec author proving one type against [MemPlane] wants. Running the
// values a plane can express and demanding a loud refusal for the ones it
// declared it cannot carry is [Driver]'s job, and [Driver] narrows each proof
// before handing it here.
//
// # One Option it cannot honour
//
// A proof carries a bare value, so this harness supplies the annotated struct
// the value travels in. [ferry.TagKey] renames the tag key for every type in the
// call, that struct included, so a tag key other than the harness's own leaves
// it unable to compile its own wrapper. That is refused once, up front, rather
// than reported as an identical failure per case.
func RoundTrip(t T, p Plane, proofs []Proof, opts ...ferry.Option) { //nolint:gocritic // hugeParam: Plane by value.
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

// disclaims names which half of the plane's declaration a value fell outside
// of, because the two are different repairs on the driver's side: a kind the
// plane never declared is a [Plane.Kinds] entry, and a value inside a kind it
// did declare is [Plane.Except].
//
// It is a linear scan over a slice that is at most six long, on a path that has
// already decided to report a failure.
func (h *harness) disclaims(v ferry.Value) string {
	if !slices.Contains(h.plane.Kinds, v.Kind()) {
		return "the plane does not declare kind " + v.Kind().String()
	}

	return "the plane declares kind " + v.Kind().String() + " and excepts this value"
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
		p.consider(h, i, c)
	}
}

// consider is the two gates one case passes before it runs.
//
// The golden is checked here and not at [Type], because a [Case] is an exported
// struct literal and a field left out is not a call that can refuse (ADR-0014's
// "the golden is required, not optional", asserted where the case is reached
// rather than promised where it is written).
//
// It is checked before the narrowing rather than after, because the zero
// [ferry.Value] is Absent: a plane that does not declare KindAbsent would send a
// case with no golden into [Driver]'s refusal half, where a forgotten field
// becomes a demand that the driver refuse a dump nobody wrote.
//
// The narrowing itself is not RoundTrip consulting [Plane.Kinds] - it never does
// - it is RoundTrip running the proof it was handed, which is what [Driver]
// narrows before handing one over. A narrowed proof runs the cases it was
// narrowed to and numbers them as [CoreTypes] wrote them.
func (p typeProof[T]) consider(h *harness, i int, c Case[T]) {
	h.rep.Helper()

	if c.Want == (ferry.Value{}) {
		h.rep.Errorf("%s: the case pins no golden, and absence is not one: a case states the boundary value "+
			"ferry must produce at the address it names, and a composite carrying elements names an "+
			"address inside itself", h.label(p.name, i))

		return
	}

	if !p.picked(c.Want) {
		return
	}

	p.runCase(h, i, c)
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
	if err := ferry.Dump(inst.ctx(), holder[T]{Value: c.Value}, rec, h.opts...); err != nil {
		h.rep.Errorf("%s: dump: %v", h.label(p.name, i), err)

		return
	}

	at := caseAddr(c.Addr)

	p.checkGolden(h, i, at, encodedAt(rec.seen, at), c.Want)
	p.checkTrip(h, i, inst, c)
}

// checkGolden is the column a round trip cannot see.
//
// The comparison is ==, which [ferry.Value] supports because it is a comparable
// 24-byte struct, so the kind is asserted along with the text and there is no
// bespoke comparison to get wrong. An address ferry never wrote reads back as
// the zero Value, which is Absent, so a case whose golden is pinned where
// nothing was written reports "encoded absent" and names the address it looked
// at.
func (p typeProof[T]) checkGolden(h *harness, i int, at string, got, want ferry.Value) {
	h.rep.Helper()

	if got == want {
		return
	}

	h.rep.Errorf("%s: ferry encoded %#v at %s, want %#v", h.label(p.name, i), got, at, want)
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

	back, err := ferry.Load[holder[T]](inst.ctx(), inst.Source, h.opts...)
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
// A proof's type is usually a leaf, and a leaf at the root sits at the root
// address, which a plane names by a rule of its own or refuses at Bind
// (ADR-0003, ADR-0010). The suite runs against the caller's plane, and until
// the driver spellings land no real driver names the root - so the wrapper is
// what gives a proof an address every plane can spell, and it stays for that
// reason rather than because a root leaf is refused. [driverRun.caseRootLeaf]
// is where the root address itself is exercised, and it skips where the plane
// has no name for it.
type holder[T any] struct {
	// The tag must spell holderAddr's one segment. They are two spellings of
	// one name because a struct tag cannot reference a constant, and nothing
	// but a test keeps them together: a proof that ferry encoded absent where
	// its golden expected a value is what drift between them looks like, so
	// every green round trip in this package's own tests is the check.
	Value T `ferry:"value"`
}

// holderAddr is where a [holder]'s one field lands, and it is the address a
// case's own golden is read at.
var holderAddr = ferry.At("value")

// caseAddr renders the address a case pins its golden at: the holder's own,
// extended by the address the case named inside the value.
//
// It is text rather than a [ferry.Path] because a case's address is relative and
// core exports no way to join two of them - ADR-0003 keeps a Path's construction
// to At and Elem, and keeps the wrapper this harness supplies out of the
// contract a proof is written to. The rendering identifies the address, so
// joining two renderings joins two segment sequences and can do nothing else.
func caseAddr(rel ferry.Path) string { return holderAddr.String() + rel.String() }

// encodedAt is what ferry wrote at one rendered address, and the zero
// [ferry.Value] where it wrote nothing.
//
// The scan is over one dump of one case's value, and it compares renderings
// because two addresses render alike exactly when they are equal (ADR-0003).
func encodedAt(seen map[ferry.Path]ferry.Value, at string) ferry.Value {
	for addr, v := range seen {
		if addr.String() == at {
			return v
		}
	}

	return ferry.Value{}
}
