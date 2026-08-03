package ferrytest

import (
	"github.com/onhotpath/ferry"
)

// Plane is a description a caller fills in, and it is what every suite in this
// package takes instead of a driver type.
//
// There is deliberately no plane *implementation* on this package's surface,
// not even for the memory plane. A suite needs four things about a plane and
// none of them is a method: what to call it in a report, which value kinds it
// can carry, how to mint a fresh empty one, and what its own spelling of a
// known value looks like. A description carries all four and lets a driver that
// this package has never heard of be described in a struct literal.
//
// [Instance] is the second half of the same idea rather than an exception to
// it: it describes one *minted* plane, and the Source and Sink behind
// [MemPlane] stay unexported inside it.
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

	// Open mints a fresh, empty [Instance] of the plane.
	//
	// Fresh each call, because ADR-0014's own rule for the suites is that every
	// equivalence subtest gets a fresh destination; a plane shared across cases
	// is the defect that hides a broken second walk.
	Open func() Instance

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

// Instance is one freshly minted plane: both halves of it over one set of
// contents, and the way to read those contents back as the plane spells them.
//
// # Why Open returns a struct rather than the two values ADR-0014 published
//
// Both halves together, in one call, because a driver supplying two unrelated
// planes is exactly the mistake a round trip cannot detect: the dump would
// succeed, the load would report everything absent, and nothing would say why.
// That much was published. What was published with it could not support its own
// golden artefact case, and closing that gap is what put the third member here
// (#101).
//
// The obvious repairs are a Contents field on [Plane], beside Open, or a Got
// field on [Artefact], one per row. Both take no argument, so neither can name
// the instance Open minted, and the only way to write either one honestly is to
// hoist the destination out of the Open closure into the enclosing scope. That
// was measured rather than argued: two golden artefacts against a hoisted
// destination with no manual reset, and the second is compared against the
// first artefact's plane as well as its own.
//
// The damage is not in the golden artefact case, which merely fails. It is that
// cases 1 to 10 are now running against a shared destination, which is the
// exact defect the fresh-destination rule exists to prevent, in the one package
// that publishes the rule. A struct minted inside Open has nowhere to hoist to,
// so the honest spelling is the only spelling.
//
// It is a struct rather than a third result for two reasons beyond reading
// better in a return statement: a positional third result is unlabelled at the
// call site, and Open would then sit exactly on this repository's
// function-result-limit, so the next thing a suite needs per instance would be
// a second breaking change. A struct can gain a member in a minor release.
type Instance struct {
	// Source is the read half.
	Source ferry.Source

	// Sink is the write half, over the same contents.
	//
	// It is nil for a plane with no honest Dump - environment variables are
	// ADR-0004's case - and the suite then runs the read-side cases only.
	Sink ferry.Sink

	// Contents yields this instance's raw contents, exactly as the plane holds
	// them, and it is what makes [Plane.Golden] checkable.
	//
	// It is read after the dump has finished and after any [ferry.Committer]
	// has committed, because a staging sink holds nothing durable until then
	// and a driver that stages would otherwise be asked what it has not written
	// yet.
	//
	// It is nil for a plane with no serialization format, and the memory plane
	// is that case: it stores the boundary [ferry.Value] itself, so there is no
	// representation for a golden row to hold. A nil Contents makes the golden
	// artefact case skipped rather than failed. It is not the signal for the
	// skip, though - an empty [Plane.Golden] is - so the two never disagree, and
	// a plane that pins a spelling and yields no way to read it is refused
	// loudly rather than quietly passing.
	//
	// It is here rather than on [Plane] or on [Artefact] because the contents
	// are a property of an instance and not of the description: [Plane.Open]
	// mints a fresh, empty plane on every call, so a nullary function anywhere
	// above this struct could only mean whichever instance happened to be minted
	// last. See this type's own documentation for what that costs.
	//
	// The bytes rather than a string, and the error rather than a swallowed
	// one, because a file-backed plane's whole implementation is then
	// `func() ([]byte, error) { return os.ReadFile(p) }` and a read that fails
	// is reported as a read that failed rather than as an empty plane.
	Contents func() ([]byte, error)
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

	// Want is the exact contents the plane must hold afterwards, compared
	// against what [Instance.Contents] yields.
	//
	// One string, which fits a plane holding one document and puts an
	// obligation on a plane holding more than one storage unit: a key-value
	// store is a set of pairs with no document, so what it renders for this
	// comparison must be deterministic and injective over stores. That is the
	// same obligation ADR-0003 already puts on such a driver's key function,
	// which is why it is stated rather than solved here: a rendering two
	// different stores share is a golden row that cannot see the difference,
	// exactly as a plane key two addresses share is data loss.
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
		Open: func() Instance {
			s := newMemStore()

			return Instance{Source: memSource{store: s}, Sink: memSink{store: s}}
			// No Contents, for the same reason there is no Golden below: this
			// plane has no serialization format, so there is nothing raw for it
			// to hand back, and the golden artefact case is skipped for it.
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
