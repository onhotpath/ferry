// Package ferrytest is ferry's driver contract in executable form: the
// conformance suites, the round-trip property harness, the memory plane and the
// recording sink.
//
// # The words this package uses
//
// A plane is wherever a driver keeps configuration - a YAML file, a set of
// environment variables, a key-value store - seen through ferry's own boundary.
// [Plane] is a description of one that a driver author fills in, and every suite
// here takes that description rather than a driver type.
//
// An address is where one field lands on a plane, and the address set is every
// address a struct names. A driver is handed the whole set before any I/O, which
// is where a schema the plane cannot hold gets refused.
//
// A kind is one of the six shapes a value has as it crosses ferry's boundary:
// Absent, Null, Bool, Number, String and Bytes. A plane declares which of them
// it can carry, and that declaration is the thing a suite holds it to.
//
// Conformance is the nineteen-case suite [Driver] runs over one plane. Passing
// it is what "this driver implements ferry's contract" means.
//
// # Who this is for
//
// A driver author writes one test, and it is the whole file:
//
//	func TestConformance(t *testing.T) {
//	    ferrytest.Driver(t, myPlane())
//	}
//
// A codec author, who has registered a Go type with ferry, writes four calls:
// [RoundTrip] to drive their own values through the engine against [MemPlane],
// [Codec] to check the registration itself, [Injective] over the values they
// will use as map keys, and [Complete] to catch a registered type they wrote no
// proof for.
//
// ferry's own tests run [CoreTypes] through the same [RoundTrip], which is what
// makes these the suites everybody gets rather than a second opinion.
//
// A driver author who declares one of their plane's own spellings runs
// [Spelling] over it, which holds the pair to the rules that keep what a plane
// writes readable by the plane that wrote it.
//
// And an ordinary user, who is not testing ferry at all: [Static] fills a config
// struct from a literal instead of from a file, and [Record] answers what a
// struct actually maps to.
//
// Anybody asserting that a call failed the way it should uses [CheckErrors], or
// [DiffErrors] for the same answer as data. ferry's message text is not API, so
// the assertion is an exact set of [Want] over the address and the sentinel, and
// no substring appears in it anywhere.
//
// # Two stability promises
//
// The apparatus - [Plane], [Instance], [MemPlane], [Static], [Record], [Case],
// [Type], [Proof], [Want], [DiffErrors], [CheckErrors] and the relations - is
// ordinary exported Go API under semver.
// It ends up embedded in tests that are not about ferry, and it does not move
// outside a major version.
//
// The suites - [Driver], [RoundTrip], [Codec], [Complete], [Injective],
// [Spelling] - may
// gain cases in a minor release. So a minor upgrade of ferry can make a driver
// that passed yesterday fail today, and that is intended: a new case does not
// break a driver, it reports that the driver was already broken. Nothing in the
// Go toolchain can warn you first, because adding a case changes no signature
// and no exported name.
//
// The design records behind these decisions are in docs/adr/.
package ferrytest
