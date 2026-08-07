package ferrytest_test

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestTypeCarriesItsType is the property ADR-0014's completeness check rests
// on: a proof reports the Go type it discharges, so the check joins by
// reflect.Type rather than by the name it is labelled with.
func TestTypeCarriesItsType(t *testing.T) {
	for _, tc := range []struct {
		name  string
		proof ferrytest.Proof
		want  reflect.Type
	}{
		{
			name:  "int",
			proof: ferrytest.Type("int", ferrytest.Eq[int], ferrytest.At(-5, ferry.Number("-5"))),
			want:  reflect.TypeFor[int](),
		},
		{
			name: "slice of bytes",
			proof: ferrytest.Type("bytes", ferrytest.SliceEq(ferrytest.Eq[byte]),
				ferrytest.At([]byte("hi"), ferry.Bytes([]byte("hi")))),
			want: reflect.TypeFor[[]byte](),
		},
		{
			// The name is deliberately the same as the row above. Joining by
			// name is what forced a hand-written special case spelling [N]byte;
			// joining by type needs none.
			name: "array of bytes, sharing a name with the slice",
			proof: ferrytest.Type("bytes", ferrytest.Eq[[2]byte],
				ferrytest.At([2]byte{'h', 'i'}, ferry.Bytes([]byte("hi")))),
			want: reflect.TypeFor[[2]byte](),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.proof.Type(); got != tc.want {
				t.Errorf("Type() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestNameIsALabel pins the other half of the same decision: the name is prose
// for a report, so two proofs may share one and still be told apart.
func TestNameIsALabel(t *testing.T) {
	slice := ferrytest.Type("bytes", ferrytest.SliceEq(ferrytest.Eq[byte]))
	array := ferrytest.Type("bytes", ferrytest.Eq[[2]byte])

	if slice.Name() != array.Name() {
		t.Fatalf("the two proofs no longer share a name: %q and %q", slice.Name(), array.Name())
	}

	if slice.Type() == array.Type() {
		t.Errorf("two proofs sharing a name report one type, %v", slice.Type())
	}
}

// TestProofIsSealed asserts that [ferrytest.Type] is the only way to make a
// Proof: the interface carries an unexported method, so no type outside this
// package can implement it, which is what lets the suites grow the methods they
// need without breaking every proof in the wild.
func TestProofIsSealed(t *testing.T) {
	typ := reflect.TypeFor[ferrytest.Proof]()

	unexported := 0

	for i := range typ.NumMethod() {
		if typ.Method(i).PkgPath != "" {
			unexported++
		}
	}

	if unexported == 0 {
		t.Error("Proof has no unexported method, so anything can implement it")
	}
}

// TestTimeEqualNeedsNoWrapper is ADR-0005's ergonomic claim, asserted by
// compiling: time.Time.Equal is a method expression of exactly the signature
// [ferrytest.Type] takes, and it is the relation for the one entry in core's
// set whose == is wrong.
func TestTimeEqualNeedsNoWrapper(t *testing.T) {
	when := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)

	p := ferrytest.Type("time.Time", time.Time.Equal,
		ferrytest.At(when, ferry.String("2026-08-02T12:00:00Z")),
	)

	if got := p.Type(); got != reflect.TypeFor[time.Time]() {
		t.Errorf("Type() = %v, want time.Time", got)
	}

	// Two spellings of one instant. reflect.DeepEqual separates them, which is
	// ADR-0005's measured reason for refusing it as a default relation, and
	// time.Time.Equal is what relates them.
	elsewhere := when.In(time.FixedZone("elsewhere", 3600))
	if reflect.DeepEqual(when, elsewhere) {
		t.Fatal("the two spellings of one instant are structurally equal, so this asserts nothing")
	}

	if !when.Equal(elsewhere) {
		t.Error("time.Time.Equal does not relate two spellings of one instant")
	}
}

// TestAtBuildsACase pins the constructor: the golden is positional, so a case
// cannot be written without stating what the value looks like on a plane, and
// the address it pins it at is the value's own.
func TestAtBuildsACase(t *testing.T) {
	c := ferrytest.At(30*time.Second, ferry.String("30s"))

	if c.Value != 30*time.Second {
		t.Errorf("Value = %v, want 30s", c.Value)
	}

	if c.Want != ferry.String("30s") {
		t.Errorf("Want = %#v, want string(\"30s\")", c.Want)
	}

	if c.Addr != (ferry.Path{}) {
		t.Errorf("Addr = %q, want the value's own address", c.Addr)
	}
}

// TestInsideBuildsACase pins the sibling: the address is positional too, so a
// case that names one cannot be written without saying what is there.
func TestInsideBuildsACase(t *testing.T) {
	at := ferry.At("origins").Elem(1)
	c := ferrytest.Inside(map[string][]string{"origins": {"a", "b"}}, at, ferry.String("b"))

	if c.Addr != at {
		t.Errorf("Addr = %q, want %q", c.Addr, at)
	}

	if c.Want != ferry.String("b") {
		t.Errorf("Want = %#v, want string(\"b\")", c.Want)
	}

	if got := c.Value["origins"]; len(got) != 2 || got[1] != "b" {
		t.Errorf("Value = %v, want the map the case was built with", c.Value)
	}
}

func TestEq(t *testing.T) {
	if !ferrytest.Eq(1, 1) || ferrytest.Eq(1, 2) {
		t.Error("Eq is not ==")
	}

	if !ferrytest.Eq("", "") || ferrytest.Eq("a", "b") {
		t.Error("Eq is not == over strings")
	}
}

// TestBitEq is why the relation exists: NaN is a value core's float proofs
// carry, NaN == NaN is false, and the signed zeros are two spellings a plane
// keeps and == does not.
func TestBitEq(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b float64
		want bool
	}{
		{name: "NaN relates to itself", a: math.NaN(), b: math.NaN(), want: true},
		{name: "NaN relates to nothing else", a: math.NaN(), b: 0, want: false},
		{name: "the signed zeros are two values", a: 0, b: math.Copysign(0, -1), want: false},
		{name: "zero relates to itself", a: 0, b: 0, want: true},
		{name: "infinities relate to themselves", a: math.Inf(1), b: math.Inf(1), want: true},
		{name: "the infinities are two values", a: math.Inf(1), b: math.Inf(-1), want: false},
		{name: "ordinary values relate", a: 0.1, b: 0.1, want: true},
		{name: "ordinary values differ", a: 0.1, b: 0.2, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wide := ferrytest.BitEq(tc.a, tc.b)
			narrow := ferrytest.BitEq(float32(tc.a), float32(tc.b))

			if wide != tc.want || narrow != tc.want {
				t.Errorf("BitEq(%v, %v) = %v as float64 and %v as float32, want %v",
					tc.a, tc.b, wide, narrow, tc.want)
			}
		})
	}
}

// TestSliceEq covers the conflation ADR-0005 decided and the element relation
// that makes a slice of an uncomparable-by-== type work at all.
func TestSliceEq(t *testing.T) {
	eq := ferrytest.SliceEq(ferrytest.Eq[int])

	for _, tc := range []struct {
		name string
		a, b []int
		want bool
	}{
		{name: "nil and empty are one value", a: nil, b: []int{}, want: true},
		{name: "empty and nil are one value", a: []int{}, b: nil, want: true},
		{name: "equal elements relate", a: []int{1, 2}, b: []int{1, 2}, want: true},
		{name: "different lengths do not", a: []int{1}, b: []int{1, 2}, want: false},
		{name: "different elements do not", a: []int{1, 2}, b: []int{1, 3}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := eq(tc.a, tc.b); got != tc.want {
				t.Errorf("SliceEq(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}

	when := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	elsewhere := when.In(time.FixedZone("elsewhere", 3600))

	if !ferrytest.SliceEq(time.Time.Equal)([]time.Time{when}, []time.Time{elsewhere}) {
		t.Error("SliceEq does not lift the element relation it was given")
	}
}

// TestMapEq covers the same conflation on the other composite, plus the two
// ways a map can differ that a length check alone does not see.
func TestMapEq(t *testing.T) {
	eq := ferrytest.MapEq[string](ferrytest.Eq[int])

	for _, tc := range []struct {
		name string
		a, b map[string]int
		want bool
	}{
		{name: "nil and empty are one value", a: nil, b: map[string]int{}, want: true},
		{name: "equal entries relate", a: map[string]int{"a": 1}, b: map[string]int{"a": 1}, want: true},
		{name: "different sizes do not", a: map[string]int{"a": 1}, b: map[string]int{}, want: false},
		{name: "a missing key does not", a: map[string]int{"a": 1}, b: map[string]int{"b": 1}, want: false},
		{name: "a different value does not", a: map[string]int{"a": 1}, b: map[string]int{"a": 2}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := eq(tc.a, tc.b); got != tc.want {
				t.Errorf("MapEq(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestPtrEq covers the distinction a pointer exists to carry: nil is not a
// pointer to the zero value, and a relation that compared addresses would call
// every round trip a failure.
func TestPtrEq(t *testing.T) {
	eq := ferrytest.PtrEq(ferrytest.Eq[int])

	zero, alsoZero, one := 0, 0, 1

	for _, tc := range []struct {
		name string
		a, b *int
		want bool
	}{
		{name: "two nils relate", a: nil, b: nil, want: true},
		{name: "nil against a pointer to zero does not", a: nil, b: &zero, want: false},
		{name: "a pointer to zero against nil does not", a: &zero, b: nil, want: false},
		{name: "two pointers to equal values relate", a: &zero, b: &alsoZero, want: true},
		{name: "two pointers to different values do not", a: &zero, b: &one, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := eq(tc.a, tc.b); got != tc.want {
				t.Errorf("PtrEq = %v, want %v", got, tc.want)
			}
		})
	}
}
