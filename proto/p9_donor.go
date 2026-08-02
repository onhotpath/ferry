package main

// P9: what kind does a codec declare, and does it see the raw boundary Value
// or the donated one?
//
// ADR-0005 made String the universal donor for kind-admitted leaves and left
// this open by name. Two things are entangled and have to be measured
// together, because picking either one alone gives a wrong answer.

import (
	"fmt"
	"math/big"
	"reflect"
)

// bigCodec is a big.Int codec whose text form is a run of digits. Whether it
// declares String or Number decides which planes it works on.
func bigCodecKind(k VKind) codec {
	return codec{
		name: "big.Int", arm: "registered", kind: k,
		enc: func(v reflect.Value) (Value, error) {
			x := v.Interface().(big.Int)
			return Value{kind: k, text: (&x).String()}, nil
		},
		dec: func(val Value, dst reflect.Value) error {
			if val.Kind() != k {
				return fmt.Errorf("value: wrong kind: want %v got %v", k, val.Kind())
			}
			var x big.Int
			if _, ok := x.SetString(val.Text(), 10); !ok {
				return fmt.Errorf("bad big.Int %q", val.Text())
			}
			dst.Set(reflect.ValueOf(x))
			return nil
		},
	}
}

func runDonor() {
	// The two observations a plane can make of the same document.
	yamlSays := Number("1099511627776") // a typed plane: YAML, TOML, JSON
	flatSays := String("1099511627776") // env, query params, Consul

	fmt.Println("\n--- P9a: a codec's declared kind decides which planes it works on ---")
	fmt.Printf("    %-24s %-22s %s\n", "codec declares", "typed plane says Number", "flat plane says String")
	fmt.Println("    " + dashes(80))
	for _, k := range []VKind{VString, VNumber} {
		c := bigCodecKind(k)
		var col [2]string
		for i, in := range []Value{yamlSays, flatSays} {
			dst := reflect.New(reflect.TypeFor[big.Int]()).Elem()
			err := c.dec(asDonor(in, c.kind), dst)
			if err != nil {
				col[i] = "REFUSED: " + err.Error()
			} else {
				x := dst.Interface().(big.Int)
				col[i] = "ok: " + x.String()
			}
		}
		fmt.Printf("    %-24v %-22s %s\n", k, shorten2(col[0], 22), col[1])
	}
	fmt.Println("    ^ the donor rule normalises String -> declared kind and nothing else,")
	fmt.Println("      which is ADR-0005's rule applied unchanged. So a codec whose text")
	fmt.Println("      IS a number must say Number, or it works on env and fails on YAML.")
	fmt.Println("      That is a real design decision handed to the registrant, and the")
	fmt.Println("      golden column is what catches getting it wrong.")

	fmt.Println("\n--- P9b: raw against donated, for the codec that got its kind right ---")
	fmt.Println("    A big.Int codec correctly declaring Number, on both planes, with")
	fmt.Println("    and without core donating first:")
	c := bigCodecKind(VNumber)
	for _, mode := range []string{"raw", "donated"} {
		for _, in := range []Value{yamlSays, flatSays} {
			v := in
			if mode == "donated" {
				v = asDonor(in, c.kind)
			}
			dst := reflect.New(reflect.TypeFor[big.Int]()).Elem()
			fmt.Printf("    %-9s codec, plane says %-7v -> err=%v\n", mode, in.Kind(), c.dec(v, dst))
		}
	}
	fmt.Println("    ^ seeing the RAW value breaks the codec on env, query params and")
	fmt.Println("      Consul - the three of ADR-0004's four first-party planes that")
	fmt.Println("      report String for everything. That is ADR-0005's G2 defect")
	fmt.Println("      delegated to every registrant, one at a time. Core donating")
	fmt.Println("      first means a codec is written once and works everywhere.")

	fmt.Println("\n--- P9c: the text arm's kind is not the registrant's to choose ---")
	fmt.Println("    encoding.TextMarshaler produces text and says nothing about kind, so")
	fmt.Println("    the text arm is String, always. A type whose text is a number and")
	fmt.Println("    which wants to land as a Number has to register instead. Measured,")
	fmt.Println("    big.Int through the text arm on the two planes:")
	tc, _, _ := textCodecFor(reflect.TypeFor[big.Int]())
	for _, in := range []Value{yamlSays, flatSays} {
		dst := reflect.New(reflect.TypeFor[big.Int]()).Elem()
		err := tc.dec(asDonor(in, tc.kind), dst)
		fmt.Printf("      plane says %-8v -> err=%v\n", in.Kind(), err)
	}
	fmt.Println("    So a YAML document with an unquoted large integer does not load into")
	fmt.Println("    a text-arm big.Int. That is the honest cost of the arm being String,")
	fmt.Println("    and the answer is registration, not a second coercion.")
}
