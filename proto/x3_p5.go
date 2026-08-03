package main

// X3-5. The rows the chain already moved, in the other direction.
//
// This probe exists because the population in X3-1 is only half the drift in
// ADR-0005's three-outcomes table. #41 D3 turned ADR-0007's chain ON and put
// the text pair BEFORE kind admission, which is what ADR-0007 decided and what
// ADR-0005 anticipated in terms:
//
//	The maps-no-address rule and the kind admission rule are BACKSTOPS, and
//	they only apply after the codec chain has declined.
//
//	If #12 puts TextMarshaler ahead of kind, this ADR's refusal list gets
//	shorter and nothing in it becomes wrong.
//
// So four rows that say "refused" now compile, and one row that says
// "bytes(...)" now lands as a string. ADR-0005 pre-authorised all five and
// none of them is a contradiction. They are here so the report can say which
// of ADR-0005's stale rows are stale by the ADR's own permission and which are
// stale because ADR-0008 landed a rule ADR-0005 never saw.

import (
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"reflect"
)

func runX3_5() {
	fmt.Println("ADR-0005's chain table, and its own permission:")
	quoteX3(
		"| type            | has a text pair | today             | if the chain ran first  |",
		"| netip.AddrPort  | yes             | refused           | string(\"192.0.2.1:80\")  |",
		"| netip.Prefix    | yes             | refused           | string(\"10.0.0.0/8\")    |",
		"| netip.Addr      | yes             | refused           | string(\"192.0.2.1\")     |",
		"| big.Int         | yes             | refused           | string(\"1099511627776\") |",
		"| net.IP          | yes             | **bytes(\"\\x00...\")** | string(\"192.0.2.1\")  |",
		"| url.URL, net.IPNet, [16]byte UUID | no | unchanged | unchanged |",
	)
	fmt.Printf("chainOrder=%v chainBeforeKind=%v  (both set by #41 D3)\n\n", chainOrder, chainBeforeKind)

	fmt.Println("--- X3-5a: measured, against a fresh registry ---")
	reg := NewRegistry()
	type row struct {
		name string
		typ  reflect.Type
		val  any
	}
	rows := []row{
		{"netip.AddrPort", reflect.TypeFor[netip.AddrPort](), netip.MustParseAddrPort("192.0.2.1:80")},
		{"netip.Prefix", reflect.TypeFor[netip.Prefix](), netip.MustParsePrefix("10.0.0.0/8")},
		{"netip.Addr", reflect.TypeFor[netip.Addr](), netip.MustParseAddr("192.0.2.1")},
		{"big.Int", reflect.TypeFor[big.Int](), *big.NewInt(1099511627776)},
		{"net.IP", reflect.TypeFor[net.IP](), net.ParseIP("192.0.2.1")},
		{"net.IPNet", reflect.TypeFor[net.IPNet](), x3MustCIDR("10.0.0.0/8")},
		{"[16]byte UUID", reflect.TypeFor[[16]byte](), [16]byte{}},
	}
	done := reg.install()
	for _, r := range rows {
		_, pairOK, _ := textCodecFor(r.typ)
		lc, leafOK := resolveLeaf(r.typ)
		which := "not a leaf"
		if leafOK {
			which = fmt.Sprintf("leaf via %s, kind %v", lc.name, lc.kind)
		}
		fmt.Printf("    %-16s text pair=%-5v  %s\n", r.name, pairOK, which)
	}
	done()

	fmt.Println("\n--- X3-5b: what each now dumps ---")
	x3Dump1[netip.AddrPort](reg, "netip.AddrPort", netip.MustParseAddrPort("192.0.2.1:80"))
	x3Dump1[netip.Prefix](reg, "netip.Prefix", netip.MustParsePrefix("10.0.0.0/8"))
	x3Dump1[netip.Addr](reg, "netip.Addr", netip.MustParseAddr("192.0.2.1"))
	x3Dump1[big.Int](reg, "big.Int", *big.NewInt(1099511627776))
	x3Dump1[net.IP](reg, "net.IP", net.ParseIP("192.0.2.1"))
	x3Dump1[[16]byte](reg, "[16]byte UUID", [16]byte{1, 2, 3})
	x3Dump1[net.IPNet](reg, "net.IPNet", x3MustCIDR("10.0.0.0/8"))

	fmt.Println("\n    Five of ADR-0005's rows are now stale BY THE ADR'S OWN PERMISSION,")
	fmt.Println("    which is what \"the refusal list gets shorter and nothing in it")
	fmt.Println("    becomes wrong\" means. The three in X3-1 are stale for the opposite")
	fmt.Println("    reason: a rule landed that ADR-0005 did not authorise and could not")
	fmt.Println("    see, and its outcome column is now false rather than merely shorter.")
	fmt.Println("\n    net.IPNet is the row that appears in BOTH tables, marked `unchanged`")
	fmt.Println("    in the chain table and `admitted, round-trips` in the outcomes table.")
	fmt.Println("    The chain table is still right: the chain does not claim it. The")
	fmt.Println("    outcomes table is not.")
}

func x3Dump1[T any](reg *Registry, label string, v T) {
	got, err := dumpTo(x3Ctx(), x3Box[T]{v}, WithRegistry(reg))
	if err != nil {
		fmt.Printf("    %-16s -> %s\n", label, x3One(err))
		return
	}
	for _, p := range sortedAddrs(got) {
		fmt.Printf("    %-16s %-10s %s\n", label, p, got[p].GoString())
		label = ""
	}
}
