package main

// X7: D5, the key-codec opt-in is the rule, and the diagnostic says why.

import (
	"fmt"
	"net/netip"
	"reflect"
	"time"
)

type X1Limits struct {
	Limits map[netip.Addr]int `ferry:"limits"`
}

func runX1_7() {
	quoteADR(
		"ADR-0009: \"A registration is usable as a map key only if it says so:",
		"StringCodec(...).AsMapKey(). A map[T]V whose key type is registered",
		"without it is a schema compile error.\"")

	for _, tc := range []struct {
		label string
		reg   func() *Registry
	}{
		{"registered WITHOUT .AsMapKey()", func() *Registry {
			return mustReg(NewRegistry(), TextCodec[netip.Addr](VString))
		}},
		{"registered WITH .AsMapKey()", func() *Registry {
			return mustReg(NewRegistry(), TextCodec[netip.Addr](VString).AsMapKey())
		}},
		{"not registered at all", NewRegistry},
	} {
		o := defaultOpts()
		o.reg = tc.reg()
		s, err := schemaFor(reflect.TypeFor[X1Limits](), o)
		if err != nil {
			fmt.Printf("    %-32s REFUSED\n", tc.label)
			for _, l := range splitLines(err.Error()) {
				fmt.Printf("        %s\n", l)
			}
			continue
		}
		fmt.Printf("    %-32s compiles, addrs %v\n", tc.label, s.addrs)
	}

	fmt.Println("\n  A41=5 measured the tip's two answers, and neither was ADR-0009's:")
	fmt.Println("    keyOptIn = false (the package default) -> compiles, silently")
	fmt.Println("    keyOptIn = true  (set by hand)         -> `unsupported map key type netip.Addr`")
	fmt.Println("  The first is the dropped-map-entry failure ADR-0009 measured. The second")
	fmt.Println("  refuses without naming the obligation or the remedy, and ADR-0009 is")
	fmt.Println("  explicit that \"the diagnostic is where the obligation gets communicated,")
	fmt.Println("  which is the point: it is the only moment a registrant is guaranteed to")
	fmt.Println("  read\".")

	fmt.Println("\n  the rule reaches the walk's own diagnostic too, so both engines agree:")
	fmt.Println("    walk.go carried ADR-0009's message already, behind the same switch.")
	fmt.Printf("    keyOptIn (now the decided value, and no longer consulted) = %v\n", keyOptIn)

	fmt.Println("\n  and core's own key types are untouched, because their proof is core's:")
	for _, tc := range []struct {
		label string
		t     reflect.Type
	}{
		{"map[string]int", reflect.TypeFor[struct {
			M map[string]int `ferry:"m"`
		}]()},
		{"map[int]string", reflect.TypeFor[struct {
			M map[int]string `ferry:"m"`
		}]()},
		{"map[time.Duration]int (core-owned)", reflect.TypeFor[X1DurKey]()},
		{"map[bool]int", reflect.TypeFor[struct {
			M map[bool]int `ferry:"m"`
		}]()},
	} {
		_, err := compileSchema2(tc.t, defaultOpts())
		fmt.Printf("    %-36s %s\n", tc.label, oneLine(err))
	}
}

type X1DurKey struct {
	M map[time.Duration]int `ferry:"m"`
}
