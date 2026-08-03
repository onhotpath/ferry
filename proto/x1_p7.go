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

	fmt.Println("\n  the third row is NOT the opt-in failing. It is the hole neither ADR")
	fmt.Println("  closes, and it is reported rather than resolved:")
	fmt.Println("    ADR-0007: \"A type the chain claims with declared kind String may key")
	fmt.Println("      a map, on the same terms as a registered codec\", because \"a chain")
	fmt.Println("      arm is a codec nobody registered\" and so has no call site to say")
	fmt.Println("      .AsMapKey() at.")
	fmt.Println("    ADR-0009 scopes its rule to \"a registration\".")
	fmt.Println("    So for any type carrying the text pair, the obligation is defeatable")
	fmt.Println("    by NOT registering it, and the refusal above is lifted by deleting a")
	fmt.Println("    line rather than by adding one. R11e records the sibling case for")
	fmt.Println("    core's own pre-seeded entries and hands it to #31; this one has no")
	fmt.Println("    ticket. Not decided here.")

	fmt.Println("\n  keyOptIn stays connected, and the DEFAULT is what changed:")
	fmt.Printf("    keyOptIn = %v (the decided rule)\n", keyOptIn)
	fmt.Println("    A first pass at this disconnected it instead, which left R11 steering")
	fmt.Println("    a control wired to nothing and made both of ADR-0009's published rows")
	fmt.Println("    unreproducible. R11a's whole job is to run the rule ADR-0009 REFUSED")
	fmt.Println("    and watch it drop a map entry, which is the argument for the opt-in,")
	fmt.Println("    and that needs the seam. The defect was the default, not the seam.")
	fmt.Println("    walk.go carries the same message behind the same switch, so the two")
	fmt.Println("    engines agree.")

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
