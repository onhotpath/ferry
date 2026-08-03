package main

// R11: a registered codec as a map key, and where the injectivity obligation
// is communicated.
//
// ADR-0005: "A registered codec may also serve as a map key ... A key codec's
// text must be injective over the key type. Injectivity is not checkable in
// general, so it is a proof obligation on the registrant."
//
// ADR-0007 then found the same hazard inside CORE's set (map[time.Time]string
// collapses two keys) and filed #31 rather than fixing it. #31 is not this
// ticket's, and this probe does not touch it. What IS this ticket's is the
// question ADR-0005 left as prose: how does a registrant find out they have
// taken on the obligation?

import (
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"time"
)

// R11Host is deliberately non-injective under its own text: two distinct Go
// values produce one string. This is the registered-codec version of the
// defect ADR-0007 found in core's set.
type R11Host struct {
	Name string
	Port int
}

func r11NonInjective() Reg {
	return StringCodec(
		func(h R11Host) string { return strings.ToLower(h.Name) }, // Port and case dropped
		func(s string) (R11Host, error) { return R11Host{Name: s}, nil })
}

func runR11() {
	fmt.Println("--- R11a: the implied rule. Any identity-table entry may key a map ---")
	fmt.Println("    This is what ADR-0005 and #12's prototype both do, and it is what")
	fmt.Println("    made map[netip.Addr]string work at all. Run it with a NON-injective")
	fmt.Println("    codec and it is exactly #31's defect, in user code:")
	keyOptIn = false
	bad := NewRegistry()
	_ = bad.Register(r11NonInjective())
	withRegistry(bad, func() {
		m := map[R11Host]int{
			{"API", 80}:  1,
			{"api", 443}: 2,
		}
		type c struct{ M map[R11Host]int }
		addrs, err := compile(reflect.TypeFor[c]())
		fmt.Printf("    compile map[R11Host]int -> %v err=%v\n", addrs, err)
		d, derr := dump(reflect.ValueOf(c{m}))
		fmt.Printf("    Go map holds %d keys -> ferry dumps %d address(es), err=%v\n",
			len(m), len(d), derr)
		for _, p := range sortedAddrs(d) {
			fmt.Printf("      %-8s %s\n", p, d[p].GoString())
		}
	})
	fmt.Println("    ^ one entry silently dropped, no error, and which one survives is map")
	fmt.Println("      iteration order. ADR-0001 rules out silently ignoring anything.")

	fmt.Println("\n--- R11b: what a LEAF codec and a KEY codec are actually promising ---")
	fmt.Println("    They are different obligations and the difference is measurable.")
	fmt.Println("    The same codec, used at a leaf, is fine:")
	withRegistry(bad, func() {
		type c struct{ H R11Host }
		d, _ := dump(reflect.ValueOf(c{R11Host{"API", 80}}))
		var back c
		_ = load(d, reflect.ValueOf(&back).Elem())
		fmt.Printf("      leaf: %v -> %s -> %v\n",
			R11Host{"API", 80}, d[Path{}.Name("H")].GoString(), back.H)
	})
	fmt.Println("      ^ lossy, so it fails a round-trip PROOF - but it fails loudly, at")
	fmt.Println("        one address, with the value visible. As a KEY it fails by making")
	fmt.Println("        a sibling entry cease to exist, which no proof over the key type")
	fmt.Println("        alone can see, because the collision is between two values.")

	fmt.Println("\n--- R11c: the opt-in rule. A registration says it may key a map ---")
	keyOptIn = true
	defer func() { keyOptIn = false }()

	// TextCodec rather than StringCodec(netip.Addr.String, netip.ParseAddr),
	// CHANGED BY #41 (D4): Register now runs the codec against the zero value,
	// and the String/Parse pair is not total over it - it encodes to
	// "invalid IP" and fails to decode that back. So that registration is
	// refused, netip.Addr never enters the table, and both rows below then
	// compiled through ADR-0007's chain instead, which made neither of
	// ADR-0009's two published rows reproducible. TextCodec is the spelling
	// ADR-0009's own remedy sentence names.
	optOut := NewRegistry()
	if err := optOut.Register(TextCodec[netip.Addr](VString)); err != nil {
		fmt.Println("    register err:", err)
	}
	optIn := NewRegistry()
	if err := optIn.Register(TextCodec[netip.Addr](VString).AsMapKey()); err != nil {
		fmt.Println("    register err:", err)
	}

	type m struct{ M map[netip.Addr]int }
	for _, tc := range []struct {
		label string
		r     *Registry
	}{
		{"registered, no AsMapKey", optOut},
		{"registered with AsMapKey", optIn},
	} {
		withRegistry(tc.r, func() {
			addrs, err := compile(reflect.TypeFor[m]())
			fmt.Printf("    %-26s compile -> %v err=%v\n", tc.label, addrs, err)
		})
	}
	fmt.Println("    ^ the refusal is at SCHEMA COMPILE, from reflect.TypeFor[T]() alone,")
	fmt.Println("      which is the same assertability every other refusal in the design")
	fmt.Println("      has. And the diagnostic is where the obligation gets communicated:")
	fmt.Println("      it is the only moment a registrant is guaranteed to read.")

	fmt.Println("\n--- R11d: what opt-in costs, honestly ---")
	fmt.Println("    A user who registers netip.Addr and then writes map[netip.Addr]int")
	fmt.Println("    gets a compile error for a codec that IS injective. That is a false")
	fmt.Println("    refusal, and it is the cost. Two things make it affordable:")
	fmt.Println("      - the fix is one method call, named after the obligation")
	fmt.Println("      - the error is at schema compile with the type named, so the user")
	fmt.Println("        never ships it")
	fmt.Println("    Against: the implied rule's failure is a dropped map entry at Dump,")
	fmt.Println("    on a plane already being written, discovered by a diff or never.")

	fmt.Println("\n--- R11e: and it does NOT fix #31, which is core's half ---")
	keyOptIn = true
	a := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	b := time.Date(2026, 1, 15, 12, 0, 0, 0, time.FixedZone("GMT", 0))
	fmt.Printf("    a == b: %v   a.Equal(b): %v\n", a == b, a.Equal(b))
	type tm struct{ M map[time.Time]int }
	d, _ := dump(reflect.ValueOf(tm{map[time.Time]int{a: 1, b: 2}}))
	fmt.Printf("    map[time.Time]int, 2 Go keys -> %d ferry address(es)\n", len(d))
	fmt.Println("    ^ unchanged, because time.Time is CORE's entry and not a registration.")
	fmt.Println("      Opt-in binds what registration adds and cannot reach what core")
	fmt.Println("      pre-seeded. Fixing that amends ADR-0005's admissible key set, which")
	fmt.Println("      is #31's and is deliberately not touched here.")
	fmt.Println("      Worth stating in the ADR because the two look like one bug and the")
	fmt.Println("      opt-in rule would otherwise read as having fixed it.")

	fmt.Println("\n--- R11f: the third option, considered and rejected ---")
	fmt.Println("    `imply it from the declared kind: a String-kind codec may key a map`")
	fmt.Println("    is what #12's prototype does. It is R11a with an extra condition that")
	fmt.Println("    excludes nothing relevant: R11Host's codec declares String, and it is")
	fmt.Println("    the non-injective one. The kind says what the text LOOKS like and")
	fmt.Println("    injectivity is about what the text FORGETS, and no kind can express")
	fmt.Println("    that.")
}
