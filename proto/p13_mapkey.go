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
	m := map[netip.Addr]string{
		netip.MustParseAddr("10.0.0.1"): "a",
		netip.MustParseAddr("10.0.0.2"): "b",
	}
	ha := struct{ V map[netip.Addr]string }{m}
	for _, before := range []bool{false, true} {
		chainBeforeKind = before
		a, err := compile(reflect.TypeOf(ha))
		fmt.Printf("    beforeKind=%-6v compile %v err=%v\n", before, a, shorten2(fmt.Sprint(err), 60))
	}
	fmt.Println("      ^ REFUSED under either order, and the order is not what decides it.")

	fmt.Println("\n    and the same map with the type REGISTERED as a key:")
	reg := mustReg(NewRegistry(), TextCodec[netip.Addr](VString).AsMapKey())
	doneReg := reg.install()
	a, err := compile(reflect.TypeOf(ha))
	fmt.Printf("      compile %v err=%v\n", a, shorten2(fmt.Sprint(err), 60))
	if err == nil {
		d, derr := dump(reflect.ValueOf(ha))
		fmt.Printf("      dump    %s err=%v\n", fmtVals(d), derr)
		var back struct{ V map[netip.Addr]string }
		lerr := load(d, reflect.ValueOf(&back).Elem())
		fmt.Printf("      load    %v err=%v\n", back.V, lerr)
	}
	doneReg()

	fmt.Println(`
    AS FOUND, both rows above compiled and round-tripped, because
    validMapKey consulted the chain. The argument for that line was that
    a type the chain admits as a LEAF should not be refused as a KEY -
    two lookups answering the same question differently, which is what
    ADR-0005's identity-before-kind rule exists to prevent.

    #45 measured what that reasoning missed, and ADR-0007 reversed its
    own sentence: the two lookups do not ask the same question. The leaf
    lookup asks "can ferry represent this", and the key lookup asks "can
    two values of this collapse into one address". ADR-0009 landed after
    ADR-0007 and made the answer to the second an explicit .AsMapKey()
    opt-in for a registration, and a chain arm has no call site at which
    to say it - so the obligation was defeatable by NOT registering, and
    the refusal was lifted by deleting a line rather than adding one.

    So keying a map is registration-only, the capability is not lost -
    it moved to the registration above - and P13b's obligation is now
    carried by whoever writes .AsMapKey(). Y45 is the measurement.`)

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
	d, err := dump(reflect.ValueOf(h))
	fmt.Printf("      ferry dumps %d addresses: %s err=%v\n", len(d), fmtVals(d), err)
	fmt.Println(`    ^ AS FOUND: two keys, one address, SILENTLY, and which entry
      survived decided by map iteration order - this line was one of the
      two in the whole suite that flipped between runs of the same
      binary. That is ADR-0005's named hazard occurring inside CORE's
      own set rather than in a registered codec, and no probe in #7
      reached it because none used a composite key.

      It is now a refusal, and the refusal is ADR-0007's R3 under #45
      rather than anything aimed at this case. #31 stays OPEN and this
      is worth being precise about: R3 does not amend the admissible
      key set, which is what #31 asks for. map[time.Time]string still
      COMPILES, and time.Time is still a key type core admits on core's
      own proof. What R3 removes is the silence - a dump that would
      lose an entry now says so, at the address it would have lost it
      at. The narrower fix and the ticket are not the same thing.

      Measured: with R3 this suite is byte-identical over 8 runs; the
      same binary without it produced 2 distinct outputs over 8.`)

	fmt.Println("\n--- P13c: so the rule has to be stated over the key set, not the codec ---")
	fmt.Println("    An arm's text form is injective for netip.Addr and is not for")
	fmt.Println("    time.Time, and nothing about the arm distinguishes them. Injectivity")
	fmt.Println("    is not checkable in general, so the honest positions are: core ships")
	fmt.Println("    only key types it has proved injective, and everything else is a")
	fmt.Println("    registered key codec whose registrant carries the proof.")
	fmt.Println(`    #45 added a third position that costs nothing and subsumes neither:
    injectivity over a TYPE is undecidable, but a collapse between two
    values IN HAND is not, so ferry checks the map it is actually
    dumping. It proves nothing about the type and it loses no entry.`)
}
