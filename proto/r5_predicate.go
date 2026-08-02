package main

// R5: a predicate arm.
//
// The tempting generalisation of R4c(1): instead of registering N named types
// one at a time, register a PREDICATE over reflect.Type and claim whatever
// matches. It is the shape that would close ADR-0005's named-duration hole
// without the user naming each type, and it is the shape mapstructure's
// DecodeHookFunc and viper's decode hooks both have.
//
// It is measured here rather than dismissed, because it is the one extension
// to ADR-0007's chain that #19 could plausibly argue for.

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"
)

type predReg struct {
	name string
	pred func(reflect.Type) bool
	kind VKind
	make func(reflect.Type) leafCodec
}

// predRegistry is the predicate form. Note what it cannot be: a map.
type predRegistry struct {
	preds []predReg
}

func (p *predRegistry) lookup(t reflect.Type) (leafCodec, string, bool) {
	for _, r := range p.preds {
		if r.pred(t) {
			return r.make(t), r.name, true
		}
	}
	return leafCodec{}, "", false
}

type R5Port int64
type R5Timeout time.Duration
type R5Retries int64

func runR5() {
	durationLike := predReg{
		name: "underlying int64, treated as a duration",
		pred: func(t reflect.Type) bool { return t.Kind() == reflect.Int64 },
		kind: VString,
		make: func(t reflect.Type) leafCodec {
			return leafCodec{name: t.String(), kind: VString,
				enc: func(v reflect.Value) (Value, error) {
					return String(time.Duration(v.Int()).String()), nil
				},
				dec: func(val Value, dst reflect.Value) error {
					s, err := val.AsString()
					if err != nil {
						return err
					}
					d, err := time.ParseDuration(s)
					if err != nil {
						return err
					}
					dst.SetInt(int64(d))
					return nil
				}}
		},
	}
	numberLike := predReg{
		name: "underlying int64, treated as a number",
		pred: func(t reflect.Type) bool { return t.Kind() == reflect.Int64 },
		kind: VNumber,
		make: func(t reflect.Type) leafCodec {
			return leafCodec{name: t.String(), kind: VNumber,
				enc: func(v reflect.Value) (Value, error) {
					return Number(strconv.FormatInt(v.Int(), 10)), nil
				},
				dec: func(val Value, dst reflect.Value) error {
					s, err := val.AsNumber()
					if err != nil {
						return err
					}
					n, err := strconv.ParseInt(s, 10, 64)
					if err != nil {
						return err
					}
					dst.SetInt(n)
					return nil
				}}
		},
	}

	fmt.Println("--- R5a: the predicate that closes the named-duration hole also eats ---")
	fmt.Println("    every other named int64 in the program. There is no predicate that")
	fmt.Println("    separates them, because there is nothing in the TYPE that differs.")
	p := &predRegistry{preds: []predReg{durationLike}}
	for _, t := range []reflect.Type{
		reflect.TypeFor[R5Timeout](), reflect.TypeFor[R5Port](), reflect.TypeFor[R5Retries](),
	} {
		c, _, ok := p.lookup(t)
		v := reflect.New(t).Elem()
		v.SetInt(int64(30 * time.Second))
		out, _ := c.enc(v)
		fmt.Printf("    %-16s claimed=%v -> %s\n", t, ok, out.GoString())
	}
	fmt.Println("    ^ R5Port(30000000000) is a port number and it just became \"30s\".")
	fmt.Println("      ADR-0005 already ruled this out for core - `closing it would require")
	fmt.Println("      matching on the underlying type, which would then also capture every")
	fmt.Println("      ordinary type Port int` - and a predicate arm is that rule handed to")
	fmt.Println("      the user with the same defect intact.")

	fmt.Println("\n--- R5b: two predicates matching one type is precedence by list order ---")
	p2 := &predRegistry{preds: []predReg{durationLike, numberLike}}
	p3 := &predRegistry{preds: []predReg{numberLike, durationLike}}
	v := reflect.New(reflect.TypeFor[R5Timeout]()).Elem()
	v.SetInt(int64(30 * time.Second))
	for i, reg := range []*predRegistry{p2, p3} {
		c, name, _ := reg.lookup(reflect.TypeFor[R5Timeout]())
		out, _ := c.enc(v)
		fmt.Printf("    order %d: %-42s -> %s\n", i+1, name, out.GoString())
	}
	fmt.Println("    ^ this is precisely what ADR-0007 removed. Its chain is `a type is")
	fmt.Println("      claimed by the FIRST of three steps that will have it`, and step one")
	fmt.Println("      is a map keyed by reflect.Type, so within it there is no order to get")
	fmt.Println("      wrong. A predicate arm makes registration order load-bearing, and")
	fmt.Println("      registration order across packages is init() order, which is a")
	fmt.Println("      property of the import graph rather than of anyone's intent.")

	fmt.Println("\n--- R5c: and it costs the identity lookup its shape ---")
	t := reflect.TypeFor[R5Timeout]()
	m := map[reflect.Type]leafCodec{t: durationLike.make(t)}
	mapNs := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			_ = m[t]
		}
	})
	for _, n := range []int{1, 4, 16} {
		pp := &predRegistry{}
		for i := 0; i < n-1; i++ {
			pp.preds = append(pp.preds, predReg{
				pred: func(reflect.Type) bool { return false }, make: durationLike.make})
		}
		pp.preds = append(pp.preds, durationLike)
		res := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				_, _, _ = pp.lookup(t)
			}
		})
		fmt.Printf("    %2d predicate(s), scanned  %8.1f ns/op\n", n, float64(res.NsPerOp()))
	}
	fmt.Printf("    map[reflect.Type] lookup  %8.1f ns/op\n", float64(mapNs.NsPerOp()))
	fmt.Println("    ^ the cost is not the argument and the ADR should not pretend it is:")
	fmt.Println("      #16 resolves the codec into the compiled schema once (R12), so this")
	fmt.Println("      is per type and not per leaf. The argument is R5a and R5b.")

	fmt.Println("\n--- R5d: what the user actually wanted, and what they get instead ---")
	fmt.Println("    The want is `all my duration-shaped types, without twelve lines`.")
	fmt.Println("    R4c(1)'s DurationLike[T ~int64]() gives it at one line per type, with")
	fmt.Println("    the type NAMED, which is the whole difference: the user says which")
	fmt.Println("    int64s are durations, rather than a predicate guessing.")
}
