package main

// P13: does the chain extend the admissible map key set, and does it carry
// ADR-0005's injectivity obligation?
//
// ADR-0005 restricts a map key to string, the integer kinds, and a registered
// codec whose form is a String, and states that a key codec's text must be
// injective or two keys collapse into one address. A chain arm is a codec
// nobody registered, so the obligation has to go somewhere.

import (
	"fmt"
	"net/netip"
	"reflect"
	"time"
)

func runMapKey() {
	chainOrder, chainBeforeKind = []string{"text"}, true
	defer func() { chainOrder, chainBeforeKind = nil, false }()

	fmt.Println("\n--- P13a: a text-arm type as a map key ---")
	for _, before := range []bool{false, true} {
		chainBeforeKind = before
		m := map[netip.Addr]string{
			netip.MustParseAddr("10.0.0.1"): "a",
			netip.MustParseAddr("10.0.0.2"): "b",
		}
		h := struct{ V map[netip.Addr]string }{m}
		a, err := compile(reflect.TypeOf(h))
		fmt.Printf("    beforeKind=%-6v compile %v err=%v\n", before, a, shorten2(fmt.Sprint(err), 60))
		if err != nil {
			continue
		}
		d, derr := dump(reflect.ValueOf(h))
		fmt.Printf("                    dump    %s err=%v\n", fmtVals(d), derr)
		var back struct{ V map[netip.Addr]string }
		lerr := load(d, reflect.ValueOf(&back).Elem())
		fmt.Printf("                    load    %v err=%v\n", back.V, lerr)
	}
	fmt.Println("    ^ validMapKey has to consult the chain, or a type the chain admits")
	fmt.Println("      as a leaf is still refused as a key. That is one line, and")
	fmt.Println("      forgetting it is the sort of divergence between two lookups that")
	fmt.Println("      ADR-0005's identity-before-kind rule exists to prevent.")

	fmt.Println("\n--- P13b: is the obligation real? a NON-injective text form ---")
	fmt.Println("    time.Time is in core's identity table, its form is RFC 3339, and")
	fmt.Println("    validMapKey admits anything in that table. Two distinct time.Time")
	fmt.Println("    values whose RFC 3339 text is identical:")
	utc := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	fixed := time.Date(2026, 1, 15, 12, 0, 0, 0, time.FixedZone("GMT", 0))
	fmt.Printf("      a = %v  Location=%q\n", utc, utc.Location())
	fmt.Printf("      b = %v  Location=%q\n", fixed, fixed.Location())
	fmt.Printf("      a == b: %v   a.Equal(b): %v\n", utc == fixed, utc.Equal(fixed))
	ta, _ := utc.MarshalText()
	tb, _ := fixed.MarshalText()
	fmt.Printf("      MarshalText: %q and %q  -> identical: %v\n", ta, tb, string(ta) == string(tb))
	mm := map[time.Time]string{utc: "utc", fixed: "fixed"}
	fmt.Printf("      Go map holds %d distinct keys\n", len(mm))
	h := struct{ V map[time.Time]string }{mm}
	// #31 decided this: core's own table no longer admits time.Time as a key,
	// and the walk refuses a duplicate address as it is minted. asShipped runs
	// the world this probe was written against, and the outcome set replaces
	// the single sample, which was the second of the two flaky lines the #41
	// audit hands to this ticket.
	asShipped(func() {
		d, err := dump(reflect.ValueOf(h))
		fmt.Printf("      ferry dumps %d addresses: %s err=%v\n", len(d), fmtVals(d), err)
		outs := k31Outcomes(200, func() (map[Path]Value, error) { return dump(reflect.ValueOf(h)) })
		fmt.Printf("      %d distinct outcome(s) over 200 dumps of one value\n", len(outs))
	})
	fmt.Println("      with #31's rule:")
	fmt.Printf("      %v\n", Compile[struct {
		V map[time.Time]string `ferry:"v"`
	}]())
	fmt.Println("    ^ two keys, one address, silently. That is ADR-0005's named hazard")
	fmt.Println("      occurring inside CORE's own set rather than in a registered codec,")
	fmt.Println("      and no probe in #7 reached it because none used a composite key.")

	fmt.Println("\n--- P13c: so the rule has to be stated over the key set, not the codec ---")
	fmt.Println("    An arm's text form is injective for netip.Addr and is not for")
	fmt.Println("    time.Time, and nothing about the arm distinguishes them. Injectivity")
	fmt.Println("    is not checkable in general, so the honest positions are: core ships")
	fmt.Println("    only key types it has proved injective, and everything else is a")
	fmt.Println("    registered key codec whose registrant carries the proof.")
}
