package main

// R2: the kind a codec declares, and the kinds a codec accepts.
//
// ADR-0007 settled that a codec declares the boundary kind it PRODUCES and
// that core donates String to that kind before calling it. It did not settle
// what a codec ACCEPTS, and ADR-0006 landed a capability that turns on the
// difference: `Null` is refused at every leaf whose Go kind has no null, and
// the stated escape hatch is that "a registered codec for its own type accepts
// Null and returns 0".
//
// So the accepted set is strictly wider than the declared kind, and the
// question this probe answers is whether the registration API can express
// that or accidentally forecloses it.

import (
	"fmt"
	"math/big"
	"reflect"
	"strconv"
)

type R2Count int

func runR2() {
	fmt.Println("--- R2a: the declared kind is the DONATION TARGET, and it is one kind ---")
	fmt.Println("    Re-confirmed against #12's P9 rather than taken on trust, because the")
	fmt.Println("    registration API is where the declaration is written.")
	for _, kind := range []VKind{VString, VNumber} {
		reg := NewRegistry()
		_ = reg.Register(TypeCodec(kind,
			func(x big.Int) (Value, error) { return Value{kind: kind, text: x.String()}, nil },
			func(v Value) (big.Int, error) {
				var x big.Int
				if v.Kind() != kind {
					return x, errKind
				}
				x.SetString(v.Text(), 10)
				return x, nil
			}))
		withRegistry(reg, func() {
			type c struct{ B big.Int }
			for _, plane := range []struct {
				label string
				v     Value
			}{
				{"typed plane says Number", Number("1099511627776")},
				{"flat plane says String", String("1099511627776")},
			} {
				var out c
				err := load(map[Path]Value{Path{}.Name("B"): plane.v}, reflect.ValueOf(&out).Elem())
				fmt.Printf("    declared %-7s %-26s -> %v\n", kind, plane.label, errOr(err, out.B.String()))
			}
		})
	}

	fmt.Println("\n--- R2b: ADR-0006's escape hatch, discharged through the API ---")
	fmt.Println("    ADR-0006 line 194: a plane `null` at a Go int is refused as a wrong")
	fmt.Println("    kind, and the recoverability argument is that `a registered codec for")
	fmt.Println("    its own type accepts Null and returns 0`. That is a claim about #19's")
	fmt.Println("    API, made before #19 existed. Run it.")

	plainInt := map[Path]Value{Path{}.Name("N"): Null()}
	type plain struct{ N int }
	var p plain
	fmt.Printf("    plain int, plane says null              -> %v\n",
		errOr(load(plainInt, reflect.ValueOf(&p).Elem()), fmt.Sprint(p.N)))

	// The registered codec that relaxes it. Note it declares Number, because
	// that is what it PRODUCES, and separately accepts Null, which it never
	// produces. The two are different questions and the API keeps them apart.
	lenient := NewRegistry()
	_ = lenient.Register(TypeCodec(VNumber,
		func(c R2Count) (Value, error) { return Number(strconv.Itoa(int(c))), nil },
		func(v Value) (R2Count, error) {
			if v.Kind() == VNull {
				return 0, nil // the escape hatch, three tokens wide
			}
			s, err := v.AsNumber()
			if err != nil {
				return 0, err
			}
			n, err := strconv.Atoi(s)
			return R2Count(n), err
		}))
	withRegistry(lenient, func() {
		type c struct{ N R2Count }
		for _, in := range []Value{Null(), Number("7"), String("7")} {
			var out c
			err := load(map[Path]Value{Path{}.Name("N"): in}, reflect.ValueOf(&out).Elem())
			fmt.Printf("    registered R2Count, plane says %-8s -> %v\n",
				in.GoString(), errOr(err, fmt.Sprint(out.N)))
		}
	})
	fmt.Println("    ^ ADR-0006's argument holds, and it holds because ADR-0007 lets Null")
	fmt.Println("      reach the codec. If the API had derived the accepted set from the")
	fmt.Println("      declared kind, this would be unreachable and ADR-0006's choice")
	fmt.Println("      between refusing and zeroing would have been forced the other way.")

	fmt.Println("\n--- R2c: the ergonomic helper FORECLOSES the escape hatch ---")
	strict := NewRegistry()
	_ = strict.Register(StringCodec(
		func(c R2Count) string { return strconv.Itoa(int(c)) },
		func(s string) (R2Count, error) { n, err := strconv.Atoi(s); return R2Count(n), err }))
	withRegistry(strict, func() {
		type c struct{ N R2Count }
		var out c
		err := load(map[Path]Value{Path{}.Name("N"): Null()}, reflect.ValueOf(&out).Elem())
		fmt.Printf("    StringCodec R2Count, plane says null    -> %v\n", errOr(err, fmt.Sprint(out.N)))
	})
	fmt.Println("    ^ StringCodec's decode half calls Value.AsString, which refuses Null.")
	fmt.Println("      So the two-argument helper cannot express ADR-0006's escape hatch and")
	fmt.Println("      the general form must stay. This is the measured reason the API is")
	fmt.Println("      TWO constructors and not one: the helper is the 90% case and the")
	fmt.Println("      general form is the one whose decode half sees the whole Value.")

	fmt.Println("\n--- R2d: what a codec may EMIT that it did not declare ---")
	fmt.Println("    ADR-0007: `a codec is a pair ... and accepts every kind it emits`, and")
	fmt.Println("    ADR-0005's net.Addr codec returns Null for a nil interface. So a")
	fmt.Println("    declared kind of String and an emitted Null coexist by design.")
	nilable := NewRegistry()
	_ = nilable.Register(TypeCodec(VString,
		func(c *R2Count) (Value, error) {
			if c == nil {
				return Null(), nil
			}
			return String(strconv.Itoa(int(*c))), nil
		},
		func(v Value) (*R2Count, error) {
			if v.Kind() == VNull {
				return nil, nil
			}
			s, err := v.AsString()
			if err != nil {
				return nil, err
			}
			n, err := strconv.Atoi(s)
			c := R2Count(n)
			return &c, err
		}))
	fmt.Println("    (that registration is refused for a different reason, see R3)")
	fmt.Println("    Stated as a rule rather than a special case in the enc check:")
	fmt.Println("      the declared kind constrains the donation target, NOT the emitted")
	fmt.Println("      kind set. Null is emittable by any codec, and a codec that emits it")
	fmt.Println("      must accept it, which is checkable in the harness's golden column.")

	fmt.Println("\n--- R2e: the check core can afford, and the one it cannot ---")
	liar := NewRegistry()
	_ = liar.Register(TypeCodec(VNumber,
		func(s R2Liar) (Value, error) { return String("not a number"), nil },
		func(v Value) (R2Liar, error) { return "", nil }))
	withRegistry(liar, func() {
		_, err := dump(reflect.ValueOf(struct{ V R2Liar }{"x"}))
		fmt.Printf("    declares Number, emits String -> %v\n", err)
	})
	fmt.Println("    ^ one comparison, in core, on every encode. What it CANNOT catch is a")
	fmt.Println("      codec declaring the right kind and the wrong text, which is exactly")
	fmt.Println("      what ADR-0005's golden column exists for. R10 is where that lands.")
}

type R2Liar string

func errOr(err error, ok string) string {
	if err != nil {
		return "err: " + err.Error()
	}
	return ok
}
