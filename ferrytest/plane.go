package ferrytest

import (
	"github.com/onhotpath/ferry"
)

// Plane is a description a caller fills in, and it is what every suite in this
// package takes instead of a driver type.
//
// There is deliberately no plane *type* on this package's surface, not even for
// the memory plane. A suite needs four things about a plane and none of them is
// a method: what to call it in a report, which value kinds it can carry, how to
// mint a fresh empty one, and what its own spelling of a known value looks like.
// A description carries all four and lets a driver that this package has never
// heard of be described in a struct literal.
type Plane struct {
	// Name labels the plane in a report. It is a label and never a key: two
	// planes with one name are a confusing report and not a collision.
	Name string

	// Kinds is what this plane can carry, declared by the driver about itself.
	//
	// It exists because the suite cannot simply demand that every plane pass
	// every proof. A flattening plane - env, query parameters, opaque KV, which
	// is three of ADR-0004's four first-party drivers - reports String for
	// everything and has no null, so a nil composite is a value it cannot
	// represent. Without the declaration the suite has two options and both are
	// wrong: fail every flat driver, or skip the check and let a nil pointer
	// silently become a zero value.
	//
	// With it, the suite runs the proofs this plane can express and asserts
	// that the ones it cannot are refused loudly rather than mangled. A plane
	// that declares a kind and then refuses it is a failure and not a refusal
	// (ADR-0005).
	Kinds []ferry.VKind

	// Open mints a fresh, empty instance of the plane and returns both halves
	// of it over the same contents.
	//
	// Both halves together, in one call, because a driver supplying two
	// unrelated planes is exactly the mistake a round trip cannot detect: the
	// dump would succeed, the load would report everything absent, and nothing
	// would say why. A plane with no honest Dump - environment variables are
	// ADR-0004's case - returns a nil Sink, and the suite runs the read-side
	// cases only.
	//
	// Fresh each call, because ADR-0014's own rule for the suites is that every
	// equivalence subtest gets a fresh destination; a plane shared across cases
	// is the defect that hides a broken second walk.
	Open func() (ferry.Source, ferry.Sink)

	// Golden pins this driver's own spelling of a fixed value, and it is empty
	// for a plane that has no serialization format.
	//
	// It lives on the Plane rather than being a parameter of the suite because
	// the spelling is the driver's statement about itself: ADR-0001 refuses to
	// constrain indentation or key order, so what is pinned has to be the
	// driver author's choice rather than the suite's demand. What it buys is
	// the one thing a round trip structurally cannot see - a round trip tests a
	// function against its own inverse, and changing both halves together is
	// invisible to any test that only composes them (ADR-0013).
	Golden []Artefact
}

// Artefact is one fixed value and the plane contents that dumping it must
// produce, byte for byte.
//
// It is the driver's half of ferry's second compatibility promise. A change to
// one of these rows is a change to what every stored artefact of that plane
// means, which is a major version of the module that owns it and not a test
// fixture edit (ADR-0013).
type Artefact struct {
	// Value is dumped. It is an ordinary annotated struct, and it is `any`
	// because a Plane describes one plane over many types rather than one type.
	Value any

	// Want is the exact contents the plane must hold afterwards.
	Want string
}

// MemPlane is the plane with nothing of its own: a map from address to Value,
// with no serialization format, no I/O and no key function beyond the identity.
//
// It is what a registrant runs their own proofs against, which is how
// ADR-0001's "registration carries the proof" is discharged without a driver in
// sight, and it is where core's value-fidelity guarantee is stated, because it
// is the only plane that adds nothing.
//
// # It is the wrong thing to prove the driver rule with, and that is the point
//
// ADR-0003 puts a key function's injectivity obligation on every driver that
// flattens an address into a plane key. This plane's key function is the
// identity - it keys by the canonical rendering, which is unique per address -
// so it is trivially injective and no address set can make it collide. A
// conformance run against this plane therefore proves nothing at all about that
// rule, which is ADR-0002's own point that the memory plane cannot keep the
// suite honest, and it is why the rule needs a first-party driver with a real
// key function behind it.
//
// # What it is obliged to do
//
// ADR-0003 states five obligations, and every one is a property to rely on
// rather than a field to set. It keys by the canonical rendering, which it may
// do because it has no format - a plane with no format and no I/O is a map. It
// never folds case and never normalises segment text, so three case-variant
// addresses are three entries. It refuses a duplicate write loudly rather than
// overwriting, because ADR-0001 rules out silently ignoring anything. It
// enumerates segment-wise, so a test asserting on its contents is not asserting
// on map iteration order. And it is the negative case above.
//
// Each call to the returned Plane's Open mints an empty plane; the Source and
// the Sink it hands back share one set of contents, and neither type is
// exported.
func MemPlane() Plane {
	return Plane{
		Name: "memory",
		Kinds: []ferry.VKind{
			ferry.KindAbsent, ferry.KindNull, ferry.KindBool,
			ferry.KindNumber, ferry.KindString, ferry.KindBytes,
		},
		Open: func() (ferry.Source, ferry.Sink) {
			s := newMemStore()

			return memSource{store: s}, memSink{store: s}
		},
		// No Golden. A golden artefact pins a driver's spelling, and this plane
		// has no spelling: it stores the boundary Value itself, so there is no
		// representation for a row to hold and nothing a future change could
		// silently alter (ADR-0013).
	}
}

// Static is a source of constants: the contents are fixed when it is built and
// nothing writes to it afterwards.
//
// It is the plane an ordinary user reaches for, who is not testing ferry at
// all and wants a config struct filled from a literal rather than from a file:
//
//	cfg, err := ferry.Load[Config](ctx, ferrytest.Static(map[ferry.Path]ferry.Value{
//	    ferry.At("port"):    ferry.Number("8080"),
//	    ferry.At("timeout"): ferry.String("30s"),
//	}))
//
// That audience is the largest one this package has and the reason its
// apparatus carries an ordinary semver promise while its suites are allowed to
// grow cases (ADR-0014). ADR-0002 admitted the memory plane on the same ground:
// xload ships MapLoader and people reach for it constantly, so a library that
// ships nothing has every user write the same ten lines and get the same things
// wrong.
//
// How it differs from [MemPlane]: this returns one half of the contract, a
// ferry.Source over contents the caller supplied, and there is no Sink for it,
// so dumping to it is a compile error at the call site rather than a runtime
// refusal. MemPlane returns a description of a read-write plane that starts
// empty and is minted fresh per Open, which is what a suite needs and what a
// user filling in a config does not.
//
// The map is copied, so a later mutation of the caller's map cannot reach a
// plane already handed out. It shares everything else with the memory plane,
// including keying by the canonical rendering and never folding.
func Static(values map[ferry.Path]ferry.Value) ferry.Source {
	s := newMemStore()
	for addr, v := range values {
		s.put(addr, v)
	}

	return memSource{store: s}
}
