package main

// P15: the seam with #8, measured rather than assumed.
//
// ADR-0006 (open, unmerged) decides: Absent means ferry does not write to the
// field; every other observation including Null and the empty string is a
// value the plane holds and is applied; and a declared default is a Value of
// kind String held at the field's address, applied when and only when the
// plane reports Absent there.
//
// #12 lands second, so it applies those definitions rather than inventing its
// own. Two things follow that have to be checked rather than asserted.

import (
	"fmt"
	"math/big"
	"net/netip"
	"reflect"
)

func runDefaults() {
	chainOrder, chainBeforeKind = []string{"text"}, true
	defer func() { chainOrder, chainBeforeKind = nil, false }()

	type conf struct {
		Addr netip.Addr `ferry:"addr"`
		N    big.Int    `ferry:"n"`
	}

	fmt.Println("\n--- P15a: a declared default reaching a chain-admitted codec ---")
	fmt.Println("    ADR-0006 makes a default a String Value at the field's address, so")
	fmt.Println("    the chain sees exactly what a flat plane would have handed it.")
	// ADR-0006's model: schema compile turns each declared default into a
	// String Value, and Load substitutes it wherever the plane says Absent.
	defaults := map[Path]Value{
		Path{}.Name("addr"): String("10.0.0.1"),
		Path{}.Name("n"):    String("1099511627776"),
	}
	for _, plane := range []struct {
		label string
		vals  map[Path]Value
	}{
		{"plane is empty (all Absent)", map[Path]Value{}},
		{"plane supplies addr", map[Path]Value{Path{}.Name("addr"): String("192.0.2.1")}},
	} {
		merged := map[Path]Value{}
		for p, v := range defaults {
			merged[p] = v
		}
		for p, v := range plane.vals {
			merged[p] = v
		}
		var c conf
		err := load(merged, reflect.ValueOf(&c).Elem())
		fmt.Printf("    %-28s -> addr=%v n=%v err=%v\n", plane.label, c.Addr, (&c.N).String(), err)
	}
	fmt.Println("    ^ so a codec needs no default-awareness at all, and there is no")
	fmt.Println("      second decode path. ADR-0006 says this in a sentence; this is the")
	fmt.Println("      sentence run against a codec ADR-0006 had no way to exercise.")

	fmt.Println("\n--- P15b: the one case where the two rules interact badly ---")
	fmt.Println("    A default is String by construction. A codec declaring Number gets")
	fmt.Println("    it donated, which works. A codec declaring BYTES does not:")
	type blobConf struct {
		B []byte `ferry:"b"`
	}
	for _, def := range []string{"aGk=", "hi"} {
		var bc blobConf
		err := load(map[Path]Value{Path{}.Name("b"): String(def)}, reflect.ValueOf(&bc).Elem())
		fmt.Printf("    default=%-6q -> []byte(%q), %d bytes, err=%v\n", def, bc.B, len(bc.B), err)
	}
	fmt.Println("    String donates to Bytes as a relabel, which is ADR-0005's own rule")
	fmt.Println("    for []byte, so the text of a default is taken as the raw bytes and")
	fmt.Println("    NOT as base64. That is a real sharp edge and it belongs in #11's")
	fmt.Println("    documentation of how a default is written, not in a second coercion.")

	fmt.Println("\n--- P15c: omission is decided before the chain, confirmed against #8 ---")
	fmt.Println("    ADR-0006: 'omission is evaluated against the Go value before")
	fmt.Println("    anything converts it'. P10 measured why that is the only order that")
	fmt.Println("    works: time.Time's zero encodes to 20 bytes and a deliberately-set")
	fmt.Println("    value can encode to nothing, so the two tests disagree in both")
	fmt.Println("    directions. The two ADRs reach the same order from opposite ends.")
}
