package main

// X8: D3, the chain is on and a declaration beats an inference.

import (
	"context"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"reflect"
)

func runX1_8() {
	quoteADR(
		"ADR-0007: \"The text pair is consulted BEFORE reflect.Kind admission.",
		"A declaration beats an inference.\"",
		"",
		"and, for the list: \"json.Marshaler/Unmarshaler,",
		"encoding.BinaryMarshaler/BinaryUnmarshaler and gob.GobEncoder/GobDecoder",
		"are NOT arms.\"")

	fmt.Printf("  chainOrder      = %v\n", chainOrder)
	fmt.Printf("  chainBeforeKind = %v\n", chainBeforeKind)
	fmt.Println("  A41=3 measured the tip's defaults as [] and false, which is the")
	fmt.Println("  \"kind only\" column of ADR-0007's headline table.")

	fmt.Println("\n  ADR-0007's headline table, reproduced against a FRESH registry:")
	fresh := NewRegistry()
	o := defaultOpts()
	o.reg = fresh
	for _, tc := range []struct {
		label string
		t     reflect.Type
		val   any
	}{
		{"netip.Addr", reflect.TypeFor[struct {
			V netip.Addr `ferry:"v"`
		}](), netip.MustParseAddr("192.0.2.1")},
		{"netip.Prefix", reflect.TypeFor[struct {
			V netip.Prefix `ferry:"v"`
		}](), netip.MustParsePrefix("192.0.2.0/24")},
		{"net.IP", reflect.TypeFor[struct {
			V net.IP `ferry:"v"`
		}](), net.ParseIP("192.0.2.1")},
		{"big.Int", reflect.TypeFor[struct {
			V big.Int `ferry:"v"`
		}](), *big.NewInt(1 << 40)},
	} {
		s, err := schemaFor(tc.t, o)
		if err != nil {
			fmt.Printf("    %-14s REFUSED %s\n", tc.label, oneLine(err))
			continue
		}
		rv := reflect.New(tc.t).Elem()
		rv.Field(0).Set(reflect.ValueOf(tc.val))
		out := map[Path]Value{}
		done := o.reg.install()
		w := &walker{dir: dumpDir(out), sch: o.sch, ctx: context.Background()}
		_, derr := w.walk(s.root, rv, Path{})
		done()
		fmt.Printf("    %-14s compiles -> %s  err=%v\n", tc.label,
			out[Path{}.Name("v")].GoString(), derr)
	}
	fmt.Println("    ^ that is the column ADR-0007 chose. The tip shipped the other one:")
	fmt.Println("      netip.Addr and netip.Prefix refused as `maps no address`, and")
	fmt.Println("      net.IP dumped as bytes(\"\\x00...\\xff\\xff\\xc0\\x00\\x02\\x01\").")

	fmt.Println("\n  what the flip costs the compile, which is ADR-0010's own sentence:")
	fmt.Println("    ADR-0010 argues the two-level cache saves \"a whole schema compile")
	fmt.Println("    including ADR-0007's chain, which probes method sets per type\", at")
	fmt.Println("    47370 ns. With chainOrder nil that compile probed no method sets at")
	fmt.Println("    all, so the sentence described work the measurement did not do.")
	fmt.Println("    It does now. E16=3's `the compile alone` row is the measurement, and")
	fmt.Println("    this session reports it rather than amending the ADR.")
	fmt.Println()
	fmt.Println("    Measured on this machine, three states, same fixture, same binary shape:")
	fmt.Println("      prefix-free = duplicate only, chain off   ~52 us   25536 B  1372 allocs")
	fmt.Println("      prefix-free = segment-wise,   chain off   ~93 us   44560 B  2577 allocs")
	fmt.Println("      prefix-free = segment-wise,   chain ON    ~92 us   44560 B  2577 allocs")
	fmt.Println("    So the whole of the movement is ADR-0003's prefix-free scan, which the")
	fmt.Println("    reconcile landed, and the chain is free on this fixture: turning it on")
	fmt.Println("    replaces one recursive kindWouldRefuse walk per leaf with three")
	fmt.Println("    reflect.Implements probes, and the two cost the same.")

	fmt.Println("\n  and the three arms ADR-0007 rules out are still off:")
	for _, name := range []string{"json", "binary", "gob"} {
		in := false
		for _, a := range chainOrder {
			if a == name {
				in = true
			}
		}
		fmt.Printf("    %-8s in chainOrder: %v\n", name, in)
	}
}
