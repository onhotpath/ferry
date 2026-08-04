package ferrytest

import "math"

// The relations for the common shapes.
//
// [Type] takes one positionally, so these exist to make the ordinary cases
// short rather than to make any case possible: a type whose relation is none of
// these supplies its own func, and time.Time.Equal is the worked example that
// needs no wrapper at all.
//
// None of them is a default. ADR-0005 refuses a default relation outright,
// because the one that suggests itself - reflect.DeepEqual - is false for a
// round-tripped time.Time and for any struct containing NaN, and the obvious
// repair for a harness that reports false failures is to loosen the comparison
// until it stops (survey item 5.7).

// Eq relates two values by ==, and is the relation for every type whose
// identity Go already has right.
//
// It is not the relation for time.Time, which is comparable and whose == is
// wrong: two times denoting the same instant differ if one carries a monotonic
// reading or a different *time.Location. That type's relation is
// time.Time.Equal, which is a method expression of exactly the required
// signature.
func Eq[T comparable](a, b T) bool { return a == b }

// BitEq relates two floats by their bit patterns rather than by ==, which is
// what makes NaN assertable at all: NaN == NaN is false, so a proof carrying
// NaN cannot use [Eq] and a harness without this relation cannot carry the
// value that historically breaks float codecs.
//
// It also separates +0 from -0, which == conflates and which a plane does not:
// a text boundary spells them "0" and "-0", so a codec that loses the sign of
// zero is a codec this relation reports and == does not.
//
// A float32 widens to float64 for the comparison. That conversion is exact for
// every float32 value, the zeros and the infinities included, so it changes no
// answer this relation can give.
func BitEq[T ~float32 | ~float64](a, b T) bool {
	return math.Float64bits(float64(a)) == math.Float64bits(float64(b))
}

// SliceEq lifts a relation on elements to a relation on slices.
//
// It takes the element relation rather than requiring comparable elements,
// because a slice of a type whose == is wrong is still a slice: []time.Time
// needs SliceEq(time.Time.Equal) and there is nothing else it could use.
//
// Nil and empty are one value here. A composite with no elements is written the
// same way whether it is nil or empty, so the two are one observation on every
// plane, and a relation separating them would report a failure ferry has
// deliberately chosen. Where the difference does matter - []byte is a leaf and
// not a composite, so []byte(nil) and []byte{} are written differently - the
// [Case] golden is what reports it.
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

// MapEq lifts a relation on values to a relation on maps, over keys compared
// with ==.
//
// The keys are compared with == and never with a lifted relation, because two
// keys that are == are one address on the plane and there is nothing finer for
// this relation to say. [Injective] is what checks that two keys which are not
// == stay two addresses.
//
// Nil and empty are one value here too, for [SliceEq]'s reason.
func MapEq[K comparable, V any](eq func(a, b V) bool) func(a, b map[K]V) bool {
	return func(a, b map[K]V) bool {
		return len(a) == len(b) && sameEntries(a, b, eq)
	}
}

// sameEntries relates two maps of one size, which is where the key comparison
// and the lifted value relation both happen.
func sameEntries[K comparable, V any](a, b map[K]V, eq func(a, b V) bool) bool {
	for k, av := range a {
		bv, ok := b[k]
		if !ok || !eq(av, bv) {
			return false
		}
	}

	return true
}

// PtrEq lifts a relation on a type to a relation on pointers to it, relating
// two nils and separating a nil from a pointer to a zero value.
//
// That separation is the whole reason the relation exists rather than being
// spelled ==. A pointer is how a schema says "this section may be absent", so
// nil against a pointer to the zero value is precisely the distinction a
// defaulting bug destroys, and a relation comparing addresses would report
// every round trip as a failure instead.
func PtrEq[T any](eq func(a, b T) bool) func(a, b *T) bool {
	return func(a, b *T) bool {
		if a == nil || b == nil {
			return a == b
		}

		return eq(*a, *b)
	}
}
