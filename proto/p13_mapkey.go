package main

// P13: does the chain extend the admissible map key set, and does it carry
// ADR-0005's injectivity obligation?
//
// ADR-0005 restricts a map key to string, the integer kinds, and a registered
// codec whose form is a String, and states that a key codec's text must be
// injective or two keys collapse into one address. A chain arm is a codec
// nobody registered, so the obligation has to go somewhere.

import (
	"context"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
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
	// Two tickets closed this from two directions within hours of each other,
	// and the resolution keeps both. #31 took core's own table: time.Time is no
	// longer admitted as a key at all, so this refuses at COMPILE. #45 took the
	// walk: a duplicate address is refused as it is minted, so anything that
	// reaches a dump refuses THERE. asShipped runs the world before either.
	//
	// The single-sample `ferry dumps N addresses` print is deliberately NOT
	// here. It was the flaky line, it draws one value from a distribution, and
	// the outcome set below states the same fact without lying about arity.
	asShipped(func() {
		outs := k31Outcomes(200, func() (map[Path]Value, error) { return dump(reflect.ValueOf(h)) })
		fmt.Printf("      as shipped: %d distinct outcome(s) over 200 dumps of ONE value\n", len(outs))
	})
	fmt.Println("      with #31's rule, at compile:")
	fmt.Printf("      %v\n", errOneLine(Compile[struct {
		V map[time.Time]string `ferry:"v"`
	}]()))
	fmt.Println("      and with #45's rule, at the dump, on a key core still admits:")
	fmt.Printf("      %v\n", errOneLine(k31DumpCollision()))
	fmt.Println(`    ^ AS FOUND: two keys, one address, SILENTLY, and which entry
      survived decided by map iteration order - this was one of the two
      lines in the whole suite that flipped between runs of the same
      binary. That is ADR-0005's named hazard occurring inside CORE's
      own set rather than in a registered codec, and no probe in #7
      reached it because none used a composite key.

      BOTH are closed now, by two rules that do not subsume each other.
      #31 removes time.Time from the admissible key set, so this exact
      case never reaches a plane. #45's check removes the SILENCE for
      every key that is still admitted - core's own and any registrant
      who said .AsMapKey() and was wrong - and it is the only one of
      the two that can catch a lying registrant.

      Measured: zero map-key variance in this suite over 12 runs.`)

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

// P13Lie is a registrant who said .AsMapKey() and was wrong: its text is
// case-folded, so two distinct Go values render alike. #31's rule cannot reach
// it - the type is not core's - and #45's dump-time check is the only thing
// that does. That is the case the two rules do not share.
type P13Lie struct{ S string }

func k31DumpCollision() error {
	reg := mustReg(NewRegistry(), StringCodec(
		func(l P13Lie) string { return strings.ToLower(l.S) },
		func(s string) (P13Lie, error) { return P13Lie{s}, nil },
	).AsMapKey())
	v := struct {
		M map[P13Lie]int `ferry:"m"`
	}{M: map[P13Lie]int{{"Prod"}: 1, {"prod"}: 2, {"PROD"}: 3}}
	_, err := dumpTo(context.Background(), v, WithRegistry(reg))
	return err
}
