package main

// X3: D4, Register runs the codec against the zero value.

import (
	"fmt"
	"net/netip"
	"reflect"
)

func runX1_3() {
	quoteADR(
		"ADR-0009: \"Register encodes the zero value of T, donates String to the",
		"declared kind, decodes it back, and refuses the registration if either",
		"half errors.\"")

	fmt.Println("  the same registrations R16a runs, now through (*Registry).Register:")
	for _, tc := range []struct {
		label string
		g     Reg
	}{
		{"StringCodec(netip.Addr.String, ParseAddr)", StringCodec(netip.Addr.String, netip.ParseAddr)},
		{"StringCodec(netip.AddrPort.String, ...)", StringCodec(netip.AddrPort.String, netip.ParseAddrPort)},
		{"StringCodec(netip.Prefix.String, ...)", StringCodec(netip.Prefix.String, netip.ParsePrefix)},
		{"TextCodec[netip.Addr](VString)", TextCodec[netip.Addr](VString)},
		{"TextCodec[netip.AddrPort](VString)", TextCodec[netip.AddrPort](VString)},
		{"DurationLike[R16Timeout]()", DurationLike[R16Timeout]()},
	} {
		r := NewRegistry()
		err := r.Register(tc.g)
		status := fmt.Sprintf("accepted, table holds %d", len(r.byType))
		if err != nil {
			status = "REFUSED"
		}
		fmt.Printf("    %-44s %s\n", tc.label, status)
		if err != nil {
			for _, l := range splitLines(err.Error()) {
				fmt.Printf("        %s\n", l)
			}
		}
	}
	fmt.Println("\n    Three refusals, and they are ADR-0009's own three: netip.Addr,")
	fmt.Println("    netip.AddrPort and netip.Prefix through String/Parse. R17's usage")
	fmt.Println("    table accepted all three. \"registering it makes the type worse than")
	fmt.Println("    not registering it\" is now unrepresentable through the API rather")
	fmt.Println("    than a sentence in an ADR.")

	fmt.Println("\n  and the refusal is total: nothing is half-registered.")
	r := NewRegistry()
	err := r.Register(
		TextCodec[netip.Addr](VString),
		StringCodec(netip.Prefix.String, netip.ParsePrefix),
		TextCodec[netip.AddrPort](VString),
	)
	fmt.Printf("    three at once, the middle one broken -> %d accepted, err lines %d\n",
		len(r.byType), len(splitLines(x1ErrText(err))))
	var names []string
	for t := range r.byType {
		names = append(names, t.String())
	}
	sortStrings(names)
	fmt.Printf("    the table holds: %v\n", names)
	fmt.Println("    ^ Register already reported per-registration errors and kept going;")
	fmt.Println("      a refused codec simply never reaches the table.")

	fmt.Println("\n  what the refused type does now, which is the whole argument:")
	fresh := NewRegistry()
	_ = fresh.Register(StringCodec(netip.Addr.String, netip.ParseAddr))
	o := defaultOpts()
	o.reg = fresh
	_, cerr := schemaFor(reflect.TypeFor[struct {
		A netip.Addr `ferry:"a"`
	}](), o)
	fmt.Printf("    Compile[struct{ A netip.Addr }] -> %v\n", x1ErrText(cerr))
	fmt.Println("    ^ a schema compile error naming the type, instead of a dump that")
	fmt.Println("      writes string(\"invalid IP\") and a load that fails on it.")
}

func x1ErrText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
