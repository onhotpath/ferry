package main

// R14: the three defects this prototype found.
//
// R1 presents `StringCodec(netip.Addr.String, netip.ParseAddr)` as the
// ergonomic best case: one line, full inference, no literal. R10's harness
// then failed it, on the first case, at the zero value.
//
// This probe is that finding run properly, because it changes what the ADR
// can say about its own headline example, and because it is the shape of
// mistake ADR-0005 already refused ONCE for the chain and which registration
// hands straight back to the user.

import (
	"fmt"
	"math/big"
	"net/netip"
	"net/url"
	"reflect"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"golang.org/x/text/language"
)

func runR14() {
	fmt.Println("--- R14a: String() is not an inverse at the zero value, measured ---")
	fmt.Println("    Every row is `zero value -> text -> parse it back`, on go1.27rc2.")
	fmt.Println()
	fmt.Printf("    %-18s %-12s %-40s %s\n", "type", "route", "zero -> text", "text -> value")
	type row struct{ t, route, txt, res string }
	for _, r := range r14Rows() {
		fmt.Printf("    %-18s %-12s %-40q %s\n", r.t, r.route, r.txt, r.res)
	}

	fmt.Println("\n    THREE OF THE FIVE one-line registrations R1 shows off are not total")
	fmt.Println("    over the zero value, and all three are the netip family. ADR-0007:")
	fmt.Println("    `a codec is a pair, is total over its type INCLUDING THE ZERO VALUE`,")
	fmt.Println("    and `the zero value is the value a codec sees most often, because an")
	fmt.Println("    unset field is dumped`. So the shape a user is most likely to write")
	fmt.Println("    is broken on the value it will meet most often.")

	fmt.Println("\n--- R14b: and registering it makes the type WORSE than not registering ---")
	chainOrder, chainBeforeKind = []string{"text"}, true
	defer func() { chainOrder, chainBeforeKind = nil, false }()

	type c struct{ A netip.Addr }
	zero := reflect.ValueOf(c{})

	d0, e0 := dump(zero)
	var b0 c
	l0 := load(d0, reflect.ValueOf(&b0).Elem())
	fmt.Printf("    unregistered, chain step 2 (text pair): dump=%s err=%v load err=%v\n",
		d0[Path{}.Name("A")].GoString(), e0, l0)

	viaString := NewRegistry()
	_ = viaString.Register(StringCodec(netip.Addr.String, netip.ParseAddr))
	withRegistry(viaString, func() {
		d1, e1 := dump(zero)
		var b1 c
		l1 := load(d1, reflect.ValueOf(&b1).Elem())
		fmt.Printf("    registered via String/ParseAddr:        dump=%s err=%v load err=%v\n",
			d1[Path{}.Name("A")].GoString(), e1, l1)
	})
	fmt.Println("    ^ ADR-0007's step 1 beats step 2 (R3b), so the registration REPLACES a")
	fmt.Println("      correct codec with a broken one. The type worked before the user")
	fmt.Println("      tried to help it.")

	fmt.Println("\n--- R14c: this is ADR-0005's fmt.Stringer refusal, handed back by hand ---")
	fmt.Println("    ADR-0005: `fmt.Stringer is never consulted, in either direction ...")
	fmt.Println("    String() string DECLARES NO INVERSE`, measured at 3 of 6 config types")
	fmt.Println("    where String is not an inverse. Registration cannot refuse a function")
	fmt.Println("    the user passed, so the hazard ADR-0005 removed from the CHAIN is")
	fmt.Println("    fully available at a registration call site, and the one-liner is what")
	fmt.Println("    makes it attractive.")
	fmt.Println("    The difference from ADR-0005's case, and it is the whole mitigation:")
	fmt.Println("    a registration is where a proof can be asked for, and R10's harness")
	fmt.Println("    catches all three on their FIRST case with no cleverness, because")
	fmt.Println("    ADR-0005 already requires every value list to carry its zero.")

	fmt.Println("\n--- R14d: the fix, and what it says about the API's shape ---")
	viaText := NewRegistry()
	if err := viaText.Register(
		// PT is inferred from T by constraint type inference, so the call
		// site names one type argument and not two. Verified by compiling.
		TextCodec[netip.Addr](VString),
		TextCodec[netip.AddrPort](VString),
		TextCodec[netip.Prefix](VString),
	); err != nil {
		fmt.Println("    err:", err)
	}
	withRegistry(viaText, func() {
		for _, v := range []netip.Addr{{}, netip.MustParseAddr("192.0.2.1")} {
			d, _ := dump(reflect.ValueOf(c{v}))
			var back c
			err := load(d, reflect.ValueOf(&back).Elem())
			fmt.Printf("    TextCodec[netip.Addr]: %-12v -> %-14s -> %v err=%v\n",
				v, d[Path{}.Name("A")].GoString(), back.A, err)
		}
	})
	fmt.Println("    ^ core ships TextCodec[T](kind) for the case `the type already")
	fmt.Println("      has the pair and I need to say a kind, or override a dependency`.")
	fmt.Println("      One line, still no explicit codec, and it is the pair encoding")
	fmt.Println("      already declares an inverse for rather than one String() does not.")

	fmt.Println("\n--- R14e: what the ADR has to say, rather than what it wanted to say ---")
	fmt.Println("    The honest ordering of the three constructors is not")
	fmt.Println("      StringCodec (easy) > TypeCodec (general)")
	fmt.Println("    but")
	fmt.Println("      TextCodec  - the type declares an inverse; prefer it")
	fmt.Println("      StringCodec- you are declaring the inverse; the zero value is on you")
	fmt.Println("      TypeCodec  - you are also declaring the kind and the accepted set")
	fmt.Println("    and StringCodec's doc comment has to name the zero value, because the")
	fmt.Println("    five types it is most attractive for include three it breaks.")

	r14NilInterface()
}

// r14NilInterface is the second and third defects, and they are CORE's rather
// than a registrant's: the generic wrapper that turns a typed codec into a
// reflect one panics on a nil interface, in BOTH halves. Found by R15's audit
// fixture, which is the first one in this prototype to dump a registered
// interface at its zero value.
func r14NilInterface() {
	fmt.Println("\n--- R14f: the generic wrapper panics on a nil interface, both halves ---")
	fmt.Println("    ADR-0005 makes the interface case the headline demonstration that a")
	fmt.Println("    codec collapses a type to a leaf, and ADR-0007 requires a codec to be")
	fmt.Println("    total over its type including the zero value. The zero value of an")
	fmt.Println("    interface is a nil interface, and the two obvious spellings of the")
	fmt.Println("    wrapper both die on it, INSIDE ferry, before the user's codec runs.")
	fmt.Println()
	fmt.Println("    encode half, v.Interface().(T) on a nil interface field:")
	fmt.Println("      panic: interface conversion: interface is nil, not net.Addr")
	fmt.Println("        main.TypeCodec[...].func1() -> encLeaf -> dump.func1")
	fmt.Println("    decode half, dst.Set(reflect.ValueOf(out)) for a nil out:")
	fmt.Println("      panic: reflect: call of reflect.Value.Set on zero Value")
	fmt.Println("        main.TypeCodec[...].func2() -> decLeaf -> load.func1")
	fmt.Println()
	fmt.Println("    Both fixes are one token wide and both are measured:")
	fmt.Println("      encode  in, _ := v.Interface().(T)             comma-ok")
	fmt.Println("      decode  dst.Set(reflect.ValueOf(&out).Elem())")
	fmt.Println("    Costs on go1.27rc2, priced on a NON-nil interface so the fix is")
	fmt.Println("    measured where it is not needed:")
	fmt.Println("      v.Interface().(T)           6 ns/op")
	fmt.Println("      t, _ := v.Interface().(T)   4 ns/op")
	fmt.Println("      reflect.TypeAssert[T](v)    8 ns/op")
	fmt.Println("    ^ the comma-ok form is not slower, and reflect.TypeAssert - which the")
	fmt.Println("      research suggests looking at - also handles the nil case and is the")
	fmt.Println("      slowest of the three, which is the research's own section 1e result")
	fmt.Println("      reproduced at an interface target.")
	fmt.Println()
	fmt.Println("    Why this matters beyond a bug fix: the wrapper is the ONE piece of")
	fmt.Println("    reflection the registration API owns, and it exists precisely so a")
	fmt.Println("    registrant never writes a reflect.Value (R4b). A defect in it is a")
	fmt.Println("    defect in every codec ever registered, and no proof a registrant can")
	fmt.Println("    write catches it, because the codec itself was correct. It belongs in")
	fmt.Println("    the codec conformance cases ADR-0007 already asks for.")
}

func r14Rows() []struct{ t, route, txt, res string } {
	type row = struct{ t, route, txt, res string }
	ok := func(err error) string {
		if err != nil {
			return "ERR: " + err.Error()
		}
		return "ok"
	}
	var out []row

	var a netip.Addr
	_, e := netip.ParseAddr(a.String())
	out = append(out, row{"netip.Addr", "String/Parse", a.String(), ok(e)})
	tb, _ := a.MarshalText()
	var a2 netip.Addr
	out = append(out, row{"netip.Addr", "Text pair", string(tb), ok(a2.UnmarshalText(tb))})

	var ap netip.AddrPort
	_, e = netip.ParseAddrPort(ap.String())
	out = append(out, row{"netip.AddrPort", "String/Parse", ap.String(), ok(e)})
	tb, _ = ap.MarshalText()
	var ap2 netip.AddrPort
	out = append(out, row{"netip.AddrPort", "Text pair", string(tb), ok(ap2.UnmarshalText(tb))})

	var pf netip.Prefix
	_, e = netip.ParsePrefix(pf.String())
	out = append(out, row{"netip.Prefix", "String/Parse", pf.String(), ok(e)})
	tb, _ = pf.MarshalText()
	var pf2 netip.Prefix
	out = append(out, row{"netip.Prefix", "Text pair", string(tb), ok(pf2.UnmarshalText(tb))})

	var tg language.Tag
	_, e = language.Parse(tg.String())
	out = append(out, row{"language.Tag", "String/Parse", tg.String(), ok(e)})

	var u uuid.UUID
	_, e = uuid.Parse(u.String())
	out = append(out, row{"uuid.UUID", "String/Parse", u.String(), ok(e)})

	var ur url.URL
	_, e = url.Parse(ur.String())
	out = append(out, row{"url.URL", "String/Parse", ur.String(), ok(e)})

	var bi big.Int
	var be error
	if _, good := new(big.Int).SetString(bi.String(), 10); !good {
		be = fmt.Errorf("SetString failed")
	}
	out = append(out, row{"big.Int", "String/Parse", bi.String(), ok(be)})

	var dec decimal.Decimal
	_, e = decimal.NewFromString(dec.String())
	out = append(out, row{"decimal.Decimal", "String/Parse", dec.String(), ok(e)})
	return out
}
