package main

// R12: how a registered codec interacts with the compiled schema.
//
// The ticket asks this by name. The research's recommendation 1 makes schema
// caching "the highest-value change" at 5.5x time and 10x allocations, and
// section 2 names the transferable idea: "compile BEHAVIOUR into the schema,
// not just data ... Resolved once at compile time it becomes a stored
// function pointer and a nil check."
//
// ADR-0007 already said where the claim belongs - "the claim is a property of
// reflect.TypeFor[T]() alone, so it belongs in the compiled schema and is
// computed once; where that lives is #16's". This probe measures what that is
// worth and what it obliges #19 to guarantee.

import (
	"fmt"
	"net/netip"
	"reflect"
	"testing"
	"time"
)

type r12Conf struct {
	A netip.Addr
	B netip.Addr
	C netip.Addr
	D time.Duration
	E string
	F int
}

// resolvedLeaf is what a compiled schema holds per address once the codec is
// resolved: a function pointer and nothing else.
type resolvedLeaf struct {
	addr Path
	enc  func(reflect.Value) (Value, error)
	idx  int
}

func runR12() {
	reg := NewRegistry()
	_ = reg.Register(StringCodec(netip.Addr.String, netip.ParseAddr))

	withRegistry(reg, func() {
		t := reflect.TypeFor[r12Conf]()
		addrs, err := compile(t)
		fmt.Printf("--- R12a: the claim is a property of the TYPE, so it resolves once ---\n")
		fmt.Printf("    compile -> %v err=%v\n", addrs, err)

		var plan []resolvedLeaf
		for i := range t.NumField() {
			f := t.Field(i)
			ft := f.Type
			var enc func(reflect.Value) (Value, error)
			if c, ok := identityLookup(ft); ok {
				enc = c.enc
			} else if c, ok := activeChainCodec(ft); ok {
				enc = c.enc
			} else {
				enc = encLeaf
			}
			plan = append(plan, resolvedLeaf{Path{}.Name(f.Name), enc, i})
		}
		fmt.Printf("    resolved %d leaves to function pointers at compile\n", len(plan))

		v := reflect.ValueOf(r12Conf{
			A: netip.MustParseAddr("10.0.0.1"),
			B: netip.MustParseAddr("10.0.0.2"),
			C: netip.MustParseAddr("10.0.0.3"),
			D: 30 * time.Second, E: "svc", F: 8080,
		})

		perLeaf := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				for i := range v.NumField() {
					_, _ = encLeaf(v.Field(i))
				}
			}
		})
		resolved := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				for _, l := range plan {
					_, _ = l.enc(v.Field(l.idx))
				}
			}
		})
		fmt.Println("\n--- R12b: the lookup, per leaf against resolved at compile ---")
		fmt.Printf("    lookup per leaf, per call   %6.1f ns/op  %3d allocs/op\n",
			float64(perLeaf.NsPerOp()), perLeaf.AllocsPerOp())
		fmt.Printf("    resolved at compile         %6.1f ns/op  %3d allocs/op\n",
			float64(resolved.NsPerOp()), resolved.AllocsPerOp())
		fmt.Println("    ^ six leaves, three of them registered. The saving is the map hit")
		fmt.Println("      and the chain probe per leaf per call. Against ADR-0003's 476 ns")
		fmt.Println("      twelve-key cached load this is not a headline, and the ADR should")
		fmt.Println("      not sell it as one: the reason to resolve at compile is that #16")
		fmt.Println("      wants a schema that is a plan rather than a description, and the")
		fmt.Println("      reason it CONSTRAINS #19 is R6.")
	})

	fmt.Println("\n--- R12c: what resolving at compile obliges the registry to guarantee ---")
	fmt.Println("    Exactly one thing, and it is the whole lifetime answer:")
	fmt.Println("      once a type has been resolved against a registry, that registry's")
	fmt.Println("      answer for that type must never change.")
	fmt.Println("    R6 is what happens without it and R7e is why a pointer key alone does")
	fmt.Println("    not give it. So `resolve the codec into the schema` and `freeze the")
	fmt.Println("    registry at first use` are one decision seen from two ends, and #19")
	fmt.Println("    cannot take the first half and leave the second to #16.")

	fmt.Println("\n--- R12d: and it is why the chain is resolved there too ---")
	fmt.Println("    ADR-0007's three steps produce ONE claim per type. A schema that")
	fmt.Println("    stores a function pointer stores the claim, so the identity table,")
	fmt.Println("    the text pair and kind admission are consulted once per type per")
	fmt.Println("    registry, and never again. That is also what makes R5's predicate")
	fmt.Println("    scan cheap enough not to be an argument, and the ADR says so rather")
	fmt.Println("    than leaning on a cost that #16 removes.")
}
