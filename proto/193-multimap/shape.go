// Package multimap is a throwaway prototype for issue #193: how a plane that is
// a map[string][]string - an HTTP query string, an HTTP header block - expresses
// a sequence to ferry at all.
//
// It never merges.
//
// Six shapes are built for real and run through ferry.Load, ferry.Dump and the
// shipped ferrytest.Driver. The key form is the flat join, settled in #184 and
// not revisited here.
package multimap

import (
	"errors"
	"fmt"

	"github.com/onhotpath/ferry"
)

// ErrIllegalName reports an address a plane in this package cannot name.
var ErrIllegalName = errors.New("multimap: address has no key on this plane")

// ErrRepeated reports a name the plane holds more than one value at, read at an
// address that can only take one.
var ErrRepeated = errors.New("multimap: the plane holds more than one value at this name")

func illegal(msg string) error {
	return fmt.Errorf("%w: %w: %s", ferry.ErrPlane, ErrIllegalName, msg)
}

func repeated(n int) error {
	return fmt.Errorf("%w: %w: it holds %d, and this address takes one: spell the sequence with "+
		"index-suffixed names", ferry.ErrPlane, ErrRepeated, n)
}

// Shape is one candidate answer to #193's question.
//
// Two independent decisions make up a shape. The first is where a position
// lives in the plane's key space: in the name, as an index suffix, or in the
// second dimension the multimap already has. The second is what the driver
// answers at a name it cannot tell a container address from a leaf address at,
// which is every name, because Reader.Get is handed a Path and the schema is
// not readable off one.
type Shape int

const (
	// Indexed is #193's option 1 read strictly: a sequence is spelled
	// tags.0=a&tags.1=b and nothing else is a sequence. The container address
	// /tags renders to the key "tags", which is silent, so enumeration works
	// and the container-address rule is never approached.
	//
	// A repeated key is refused at Get, loudly, naming the count.
	Indexed Shape = iota

	// IndexedFirstWins is #193's option 3: the same spelling, and a repeated
	// key silently takes v[0]. It is the cheapest and the one that loses
	// information without saying so, and it is here to measure exactly what it
	// loses.
	IndexedFirstWins

	// Cardinality is the issue's correction's own candidate: more than one
	// value at a name means the driver answers Absent at that address and
	// enumerates positions under it; exactly one value means a scalar.
	//
	// The one-element hole is the thing to measure.
	Cardinality

	// CardinalityAudit is Cardinality plus a Releaser that reports, after the
	// walk, every name whose values the driver hid behind Absent and which
	// nothing ever enumerated. It converts Cardinality's silent loss at a
	// scalar field into a loud one, without core changing at all.
	CardinalityAudit

	// Declared is the shape that buys the missing signal from the caller rather
	// than from the plane: the driver is told, in its own configuration, which
	// names carry a sequence. Those names answer Absent at Get whatever their
	// cardinality, and enumerate positions; every other name is a scalar and a
	// repeated value at one is refused.
	//
	// It is total. What it costs is that the caller states the schema twice.
	Declared

	// Sequence is the shape that needs core to ask Children before Get at a
	// dynamic container. Given that one reordering, the driver has the signal
	// it is missing - being asked for children *is* core saying "this address
	// is a slice or a map" - and everything else falls out: Children answers
	// one position per value for any cardinality including one, and Get, which
	// is then only ever reached at a leaf or an empty container, refuses a
	// repeated name.
	//
	// Run against unpatched core it fails exactly where Cardinality does.
	Sequence

	// Enumerated is Sequence corrected by what ferrytest.Driver case 3 turned
	// out to forbid.
	//
	// Refusing at a container Get is not available to any driver: case 3 asserts
	// that Get at a container address answers Absent and never fails, and it
	// calls Get there itself rather than through the walk, so no reordering
	// inside core rescues it. So this shape answers Absent at a repeated name,
	// exactly as Cardinality does, and audits at Close what nothing enumerated,
	// exactly as CardinalityAudit does.
	//
	// The single thing it changes is that Children mints one position per value
	// at any cardinality including one, which is only sound if being asked for
	// children means core has already decided the address is a dynamic
	// container. That is the bend: at a slice or a map over a source that can
	// enumerate, ask Children before Get.
	//
	// Against unpatched core it is CardinalityAudit exactly, because Get at the
	// container address runs first and answers the one value.
	Enumerated
)

func (s Shape) String() string {
	switch s {
	case Indexed:
		return "indexed"
	case IndexedFirstWins:
		return "indexed-first"
	case Cardinality:
		return "cardinality"
	case CardinalityAudit:
		return "cardinality-audit"
	case Declared:
		return "declared"
	case Sequence:
		return "sequence"
	case Enumerated:
		return "enumerated"
	default:
		return "Shape(?)"
	}
}

// Shapes is every shape, in the order the report tables them.
func Shapes() []Shape {
	return []Shape{Indexed, IndexedFirstWins, Cardinality, CardinalityAudit, Declared, Sequence, Enumerated}
}

// positionsBehindName reports whether this shape puts a sequence position in the
// multimap's second dimension rather than in the name.
//
// It is what decides both halves of the driver: whether Get falls back from
// /tags#1 to the second value at "tags", and whether the writer appends under
// one name rather than writing one index-suffixed name per element.
func (s Shape) positionsBehindName() bool {
	switch s {
	case Cardinality, CardinalityAudit, Declared, Sequence, Enumerated:
		return true
	case Indexed, IndexedFirstWins:
		return false
	default:
		return false
	}
}

// container reports what this shape does at a name holding n values, where n is
// at least one and the driver has no idea whether the address is a container.
type answer int

const (
	// answerScalar hands the first value back as a String.
	answerScalar answer = iota
	// answerAbsent hides the values, betting that Children will be called.
	answerAbsent
	// answerRefuse fails the read, naming the count.
	answerRefuse
)

// atName is the whole of a shape's Get policy at a name holding n values,
// where declared says the driver was told this name carries a sequence.
func (s Shape) atName(n int, declared bool) answer {
	if n == 1 && !(s == Declared && declared) {
		return answerScalar
	}

	switch s {
	case Indexed, Sequence:
		return answerRefuse
	case IndexedFirstWins:
		return answerScalar
	case Cardinality, CardinalityAudit, Enumerated:
		return answerAbsent
	case Declared:
		if declared {
			return answerAbsent
		}

		return answerRefuse
	default:
		return answerRefuse
	}
}

// enumerates reports whether Children should mint one position per value held
// at the prefix's own name, given the count and whether the name was declared.
//
// This is where the shapes differ most, and the difference is exactly the
// one-element hole. Cardinality can only mint positions where it already
// answered Absent, which is n > 1. Sequence mints them for any n, because being
// asked for children is core telling it the address is a container. Declared
// mints them for any n at a declared name, because the caller said so.
func (s Shape) enumerates(n int, declared bool) bool {
	switch s {
	case Cardinality, CardinalityAudit:
		return n > 1
	case Sequence, Enumerated:
		return n > 0
	case Declared:
		return declared && n > 0
	case Indexed, IndexedFirstWins:
		return false
	default:
		return false
	}
}
