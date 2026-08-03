package main

// The audit: the cases the table above does not cover, plus the one check that
// decides whether the harness is worth anything - can it go red?

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"time"
)

func MapEq[K comparable, V any](eq func(a, b V) bool) func(a, b map[K]V) bool {
	return func(a, b map[K]V) bool {
		if len(a) != len(b) {
			return false
		}
		for k, av := range a {
			bv, ok := b[k]
			if !ok || !eq(av, bv) {
				return false
			}
		}
		return true
	}
}

func PtrEq[T any](eq func(a, b T) bool) func(a, b *T) bool {
	return func(a, b *T) bool {
		if a == nil || b == nil {
			return a == nil && b == nil
		}
		return eq(*a, *b)
	}
}

// Cred is the harness's composite fixture. The tags are HARNESS DEFECT #1
// again: the superseded walk fell back to the Go field name and the engine
// refuses an untagged field per ADR-0008. The segment text is the field name,
// so no address moves.
type Cred struct {
	User string `ferry:"User"`
	Pass string `ferry:"Pass"`
}

func credEq(a, b Cred) bool { return a == b }

func auditSet() []Proof {
	return []Proof{
		Type("map[string]int", MapEq[string](Eq[int]),
			nil, map[string]int{}, map[string]int{"rps": 1, "burst": 2},
			map[string]int{"": 0}, map[string]int{"a/b": 1, "a~b": 2, "a#b": 3}),
		Type("map[int]string", MapEq[int](Eq[string]),
			nil, map[int]string{0: "z", 10: "t", 2: "w"}),
		Type("*int", PtrEq(Eq[int]), nil, ptr(0), ptr(-5)),
		Type("*Cred", PtrEq(credEq), nil, &Cred{"u", "p"}),
		Type("struct", credEq, Cred{}, Cred{"u", "p"}),
		Type("[]Cred", SliceEq(credEq), nil, []Cred{}, []Cred{{"a", "b"}, {"", ""}}),
		Type("[][]string", SliceEq(SliceEq(Eq[string])),
			nil, [][]string{}, [][]string{{"a"}, {}, nil}),
		Type("map[string][]string", MapEq[string](SliceEq(Eq[string])),
			nil, map[string][]string{"a": {"x", "y"}, "b": nil, "c": {}}),
		Type("[4]byte", Eq[[4]byte], [4]byte{}, [4]byte{0, 255, 65, 7}),
		Type("[]time.Duration", SliceEq(Eq[time.Duration]), nil, []time.Duration{time.Second, 0}),
	}
}

func ptr[T any](v T) *T { return &v }

// --- can the harness go red? ------------------------------------------------

// A deliberately lossy codec for a type in the set, installed the way a
// careless contributor would install one: float64 through a machine float
// with a fixed 6-digit format, which is the shape structpb has.
func breakFloat64() func() {
	t := reflect.TypeFor[float64]()
	old, had := byIdentity[t]
	byIdentity[t] = leafCodec{
		name: "lossy float64",
		enc: func(v reflect.Value) (Value, error) {
			return Number(fmt.Sprintf("%.6f", v.Float())), nil
		},
		dec: func(val Value, dst reflect.Value) error {
			s, err := val.AsNumber()
			if err != nil {
				return err
			}
			var f float64
			fmt.Sscanf(s, "%f", &f)
			dst.SetFloat(f)
			return nil
		},
	}
	return func() {
		if had {
			byIdentity[t] = old
		} else {
			delete(byIdentity, t)
		}
	}
}

// The same trick for time.Duration: dump nanoseconds as a bare number, which
// is what json/v2's FormatDurationAsNano does and what a kind-only walk does.
func breakDurationEq() Proof {
	return Type("time.Duration under ==(int64)", func(a, b time.Duration) bool {
		return a == b
	}, time.Second)
}

func runAudit() {
	fmt.Println("\n--- audit: composites, pointers, nesting (memory plane) ---")
	total, bad := 0, 0
	for _, pr := range auditSet() {
		total++
		fails := failsOn(pr, memoryPlane())
		if len(fails) > 0 {
			bad++
			fmt.Printf("  FAIL %-22s\n", pr.Name())
			for _, f := range fails {
				fmt.Printf("       %s\n", f)
			}
		} else {
			fmt.Printf("  ok   %s\n", pr.Name())
		}
	}
	fmt.Printf("  %d/%d\n", total-bad, total)

	fmt.Println("\n--- can the harness go red? inject a lossy float64 codec ---")
	restore := breakFloat64()
	f := Type("float64", BitEq[float64], 0.1, 1.0/3.0, math.MaxFloat64, math.NaN())
	fails := failsOn(f, memoryPlane())
	if len(fails) == 0 {
		fmt.Println("  *** the harness stayed GREEN against a knowingly lossy codec ***")
	} else {
		fmt.Printf("  harness went red, %d failures:\n", len(fails))
		for _, x := range fails {
			fmt.Printf("       %s\n", x)
		}
	}
	restore()

	fmt.Println("\n--- and does it stay green once the codec is restored? ---")
	fmt.Printf("  restored float64 failures: %d\n", len(failsOn(Type("float64", BitEq[float64], 0.1, math.NaN()), memoryPlane())))

	fmt.Println("\n--- determinism: is the dumped address order stable? ---")
	m := map[string]int{"a": 1, "b": 2, "c": 3, "d": 4, "e": 5, "f": 6, "g": 7, "h": 8}
	seen := map[string]bool{}
	for range 300 {
		v, _ := dump(reflect.ValueOf(struct{ M map[string]int }{m}))
		var sb []string
		for _, p := range sortedAddrs(v) {
			sb = append(sb, p.String()+"="+v[p].Text())
		}
		seen[fmt.Sprint(sb)] = true
	}
	fmt.Printf("  %d distinct orderings over 300 dumps\n", len(seen))
	_ = slices.Sorted(maps.Keys(seen))
}
