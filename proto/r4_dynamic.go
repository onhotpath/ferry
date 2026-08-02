package main

// R4: static registration against dynamic registration by runtime reflect.Type.
//
// The ticket asks for this by name, and cites go.dev/issue/73457, which is
// still open. Re-checked against the GitHub API on 2026-08-02:
//
//   title  proposal: encoding/json/v2: MarshalFunc with reflect.Type
//   state  open, labels Proposal / LibraryProposal, last touched 2025-08-07
//
// So json/v2 still cannot do it and ferry is not copying an answer here. The
// question is whether ferry needs it, which is a question about what a caller
// can and cannot NAME at the call site.

import (
	"fmt"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// RegisterType is the dynamic form: the type is a value rather than a type
// argument, so the two halves take and return reflect.Value.
func (r *Registry) RegisterType(
	t reflect.Type, kind VKind,
	enc func(reflect.Value) (Value, error),
	dec func(Value, reflect.Value) error,
) error {
	return r.Register(Reg{
		t: t, name: t.String(), kind: kind,
		c: leafCodec{name: t.String(), kind: kind, enc: enc, dec: dec},
	})
}

type R4Millis time.Duration
type R4Seconds time.Duration

func runR4() {
	fmt.Println("--- R4a: what only the DYNAMIC form can express ---")
	fmt.Println("    A type reached at runtime and never named in source. reflect.StructOf")
	fmt.Println("    is the only way to make one in pure Go, and it is what a plugin, a")
	fmt.Println("    code generator's runtime half, or a schema-driven mapper produces.")
	dyn := reflect.StructOf([]reflect.StructField{
		{Name: "X", Type: reflect.TypeFor[int]()},
	})
	fmt.Printf("    reflect.StructOf(...) = %v, nameable at a call site? no\n", dyn)

	reg := NewRegistry()
	err := reg.RegisterType(dyn, VString,
		func(v reflect.Value) (Value, error) {
			return String("X=" + strconv.FormatInt(v.Field(0).Int(), 10)), nil
		},
		func(val Value, dst reflect.Value) error {
			s, err := val.AsString()
			if err != nil {
				return err
			}
			n, err := strconv.Atoi(strings.TrimPrefix(s, "X="))
			if err != nil {
				return err
			}
			dst.Field(0).SetInt(int64(n))
			return nil
		})
	fmt.Printf("    RegisterType(dyn, ...) -> err=%v\n", err)
	withRegistry(reg, func() {
		v := reflect.New(dyn).Elem()
		v.Field(0).SetInt(7)
		d, derr := dump(v)
		fmt.Printf("    dump -> %v err=%v\n", d, derr)
	})

	fmt.Println("\n--- R4b: what the dynamic form COSTS, measured ---")
	fmt.Println("    Everything the typed form makes unrepresentable becomes a runtime")
	fmt.Println("    question again. Three of them, run:")

	// MOVED BY #41 (D4). Both panics below used to surface at Dump and at Load,
	// because Register never ran the codec. With ADR-0009's zero-value check
	// wired into Register, both surface AT THE REGISTRATION CALL instead, which
	// is R16b's own claim made literal: "in Register the panic propagates from
	// the first registration, at startup". The probe's finding is unchanged -
	// the dynamic form defers to runtime what the typed form makes a build
	// error - and what moved is how early runtime notices.
	bad := NewRegistry()
	// (i) the codec's idea of the type and the registered type disagree.
	fmt.Printf("    (i)   wrong type inside enc -> PANIC: %v\n", r4Panic(func() {
		_ = bad.RegisterType(reflect.TypeFor[netip.Addr](), VString,
			func(v reflect.Value) (Value, error) {
				return String(v.Interface().(time.Time).String()), nil // wrong type
			},
			func(val Value, dst reflect.Value) error { return nil })
	}))

	// (ii) the decode half writes to the wrong field, or the wrong width.
	bad2 := NewRegistry()
	fmt.Printf("    (ii)  wrong Set on dst    -> PANIC: %v\n", r4Panic(func() {
		_ = bad2.RegisterType(reflect.TypeFor[R4Millis](), VNumber,
			func(v reflect.Value) (Value, error) { return Number("1"), nil },
			func(val Value, dst reflect.Value) error {
				dst.SetString("nope") // an int64 destination
				return nil
			})
	}))
	fmt.Println("          ^ both now at the registration call rather than at Dump and at")
	fmt.Println("            Load, because Register runs dec(enc(zero)) (ADR-0009, #41 D4).")

	// (iii) nothing forces the two halves to be about one type at all.
	fmt.Println("    (iii) the two halves need not agree with each other or with t; the")
	fmt.Println("          typed form makes that a build error (R1c) and the dynamic form")
	fmt.Println("          makes it a panic inside ferry, on third-party code, at Dump.")

	fmt.Println("\n--- R4c: can the typed form reach every case the dynamic form does? ---")
	fmt.Println("    No, and the gap is exactly one thing: a type with no name at a call")
	fmt.Println("    site. Everything else the dynamic form was reached for turns out to")
	fmt.Println("    have a typed spelling. The two cases that motivate it:")

	fmt.Println("\n    (1) `I have N named types over one underlying type` - ADR-0005's")
	fmt.Println("        named-duration hole, generalised. Typed spelling, one line each:")
	fam := NewRegistry()
	if err := fam.Register(
		DurationLike[R4Millis](),
		DurationLike[R4Seconds](),
	); err != nil {
		fmt.Println("        err:", err)
	}
	withRegistry(fam, func() {
		type c struct {
			M R4Millis
			S R4Seconds
		}
		d, _ := dump(reflect.ValueOf(c{R4Millis(1500 * time.Millisecond), R4Seconds(90 * time.Second)}))
		for _, p := range sortedAddrs(d) {
			fmt.Printf("        %-4s %s\n", p, d[p].GoString())
		}
		var back c
		lerr := load(d, reflect.ValueOf(&back).Elem())
		fmt.Printf("        load err=%v -> %v %v\n", lerr,
			time.Duration(back.M), time.Duration(back.S))
	})
	fmt.Println("        ^ core can ship DurationLike[T ~int64]() as a one-liner, which")
	fmt.Println("          closes ADR-0005's documented sharp edge with no reflect.Type in")
	fmt.Println("          anyone's hand. It needs an explicit type argument, because there")
	fmt.Println("          is no value argument to infer from - the one place in this API")
	fmt.Println("          where inference does not work, and it costs one bracket pair.")

	fmt.Println("\n    (2) `I want to register in a loop over a []reflect.Type` - which is")
	fmt.Println("        the dynamic form's real ergonomic pitch. Measured: the loop body")
	fmt.Println("        cannot be written without a codec per type anyway, because the")
	fmt.Println("        codec is what differs. A loop over types sharing ONE codec is a")
	fmt.Println("        loop over conversions, which is (1).")
}

// DurationLike is the typed answer to ADR-0005's named-duration hole. It is
// one line at a call site and needs no reflect.Type anywhere.
func DurationLike[T ~int64]() Reg {
	return StringCodec(
		func(t T) string { return time.Duration(t).String() },
		func(s string) (T, error) {
			d, err := time.ParseDuration(s)
			return T(d), err
		})
}

// r4Panic runs f and returns whatever it panicked with, or nil.
func r4Panic(f func()) (out any) {
	defer func() { out = recover() }()
	f()
	return nil
}
