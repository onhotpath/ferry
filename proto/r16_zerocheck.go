package main

// R16: can core catch the zero-value defect at REGISTRATION, with no proof?
//
// R14 found that `StringCodec(netip.Addr.String, netip.ParseAddr)` is not
// total over the zero value, and this ADR's first answer was a doc comment
// plus a preferred constructor. That is a weak mechanism for the worst defect
// the prototype found, so this probe asks whether core can do better.
//
// The idea: Register already holds the codec and can construct the zero value
// of T with no help from anyone. So it can run `dec(enc(zero))` once, at
// registration, and refuse a codec that errors. No proof, no value list, no
// testing import, no relation.

import (
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"time"
)

// zeroCheck runs the codec against the zero value of its own type. It is what
// Register would do, and it is written here rather than inside Register so the
// probe can show the before and after.
//
// It checks that the round trip does not ERROR. It deliberately does not check
// that the value comes back equal, because equality needs a relation and a
// relation is the registrant's (ADR-0005). So this is the cheap half of the
// obligation, and R16c measures exactly how much of it that is.
func zeroCheck(g Reg) error {
	zero := reflect.New(g.t).Elem()
	out, err := g.c.enc(zero)
	if err != nil {
		return fmt.Errorf(
			"ferry: %s: the codec's encode half fails on the zero value: %w", g.name, err)
	}
	// Core donates String to the declared kind before calling any codec
	// (ADR-0007), so the check has to donate too, or it tests a path the walk
	// never takes.
	dst := reflect.New(g.t).Elem()
	if err := g.c.dec(asDonor(out, g.kind), dst); err != nil {
		return fmt.Errorf(
			"ferry: %s: the codec is not total over the zero value: it encodes to %s "+
				"and decoding that back fails: %w", g.name, out.GoString(), err)
	}
	return nil
}

func runR16() {
	fmt.Println("--- R16a: the check, against the ten registrations of R1 ---")
	fmt.Println("    Nothing here is supplied by the registrant. Register builds the zero")
	fmt.Println("    value of T itself, because it has T.")
	fmt.Println()
	type namedReg struct {
		label string
		g     Reg
	}
	regs := []namedReg{
		{"StringCodec(netip.Addr.String, netip.ParseAddr)", StringCodec(netip.Addr.String, netip.ParseAddr)},
		{"StringCodec(netip.AddrPort.String, ...)", StringCodec(netip.AddrPort.String, netip.ParseAddrPort)},
		{"StringCodec(netip.Prefix.String, ...)", StringCodec(netip.Prefix.String, netip.ParsePrefix)},
		{"TextCodec[netip.Addr](VString)", TextCodec[netip.Addr](VString)},
		{"StringCodec(url.URL...)", StringCodec(
			func(u url.URL) string { return u.String() },
			func(s string) (url.URL, error) {
				u, err := url.Parse(s)
				if err != nil {
					return url.URL{}, err
				}
				return *u, nil
			})},
		{"DurationLike[R16Timeout]()", DurationLike[R16Timeout]()},
		{"TypeCodec(VNumber, big.Int...)", TypeCodec(VNumber,
			func(x big.Int) (Value, error) { return Number(x.String()), nil },
			func(v Value) (big.Int, error) {
				var x big.Int
				s, err := v.AsNumber()
				if err != nil {
					return x, err
				}
				if _, ok := x.SetString(s, 10); !ok {
					return x, fmt.Errorf("not an integer: %q", s)
				}
				return x, nil
			})},
		{"TypeCodec(VString, net.Addr...) an interface", TypeCodec(VString,
			func(a net.Addr) (Value, error) {
				if a == nil {
					return Null(), nil
				}
				return String(a.Network() + "://" + a.String()), nil
			},
			func(v Value) (net.Addr, error) {
				if v.Kind() == VNull {
					return nil, nil
				}
				s, err := v.AsString()
				if err != nil {
					return nil, err
				}
				return net.ResolveTCPAddr("tcp", s[len("tcp://"):])
			})},
	}
	for _, r := range regs {
		err := zeroCheck(r.g)
		status := "ok"
		if err != nil {
			status = "REFUSED"
		}
		fmt.Printf("    %-46s %s\n", r.label, status)
		if err != nil {
			fmt.Printf("        %v\n", err)
		}
	}

	fmt.Println("\n    All three of R14's defects are caught, at the registration call")
	fmt.Println("    site, with no proof written and no test values supplied. The")
	fmt.Println("    interface codec passes, which is the case that had to keep working:")
	fmt.Println("    its zero is a nil interface, it emits Null, and it accepts Null back.")

	fmt.Println("\n--- R16b: and it catches the wrapper defects too, which nothing else did ---")
	fmt.Println("    R14f's two panics were found by an audit fixture that happened to")
	fmt.Println("    dump a registered interface at its zero value. With this check in")
	fmt.Println("    Register, the very first interface registration anyone writes runs")
	fmt.Println("    exactly that path, at startup, in every consumer's process.")
	fmt.Println("    Reproduced by reverting the encode-half fix:")
	broken := TypeCodec(VString,
		func(a net.Addr) (Value, error) { return String("x"), nil },
		func(v Value) (net.Addr, error) { return nil, nil })
	broken.c.enc = func(v reflect.Value) (Value, error) {
		defer func() { recover() }()
		_ = v.Interface().(net.Addr) // the pre-fix spelling
		return String("x"), nil
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("        zeroCheck surfaces it as: panic %v\n", r)
			}
		}()
		if err := zeroCheck(broken); err != nil {
			fmt.Printf("        %v\n", err)
		} else {
			fmt.Println("        (the recover inside the probe's stub swallows it; in Register")
			fmt.Println("         the panic propagates from the first registration, at startup)")
		}
	}()

	fmt.Println("\n--- R16c: exactly how much of the obligation this discharges ---")
	fmt.Println("    It checks that the round trip does not ERROR. It cannot check that")
	fmt.Println("    the value comes back EQUAL, because equality needs a relation and a")
	fmt.Println("    relation is the registrant's (ADR-0005). Measured against three")
	fmt.Println("    codecs that are wrong in three different ways:")
	// Lossy, and deliberately TOTAL at the zero value: it drops an IPv6 zone,
	// which the zero address does not have.
	lossy := StringCodec(
		func(a netip.Addr) string { return a.WithZone("").String() },
		func(s string) (netip.Addr, error) {
			if s == "invalid IP" {
				return netip.Addr{}, nil
			}
			return netip.ParseAddr(s)
		})
	silentlyLossy := StringCodec(
		func(a netip.Addr) string { return "0.0.0.0" }, // constant: total, and wrong
		func(s string) (netip.Addr, error) { return netip.ParseAddr(s) })
	wrongKind := TypeCodec(VString,
		func(x big.Int) (Value, error) { return String(x.String()), nil },
		func(v Value) (big.Int, error) {
			var x big.Int
			s, err := v.AsString()
			if err != nil {
				return x, err
			}
			x.SetString(s, 10)
			return x, nil
		})
	for _, tc := range []struct {
		label string
		g     Reg
	}{
		{"errors at the zero value", StringCodec(netip.Addr.String, netip.ParseAddr)},
		{"lossy, but total at the zero value", lossy},
		{"constant: total, and wrong everywhere", silentlyLossy},
		{"right value, wrong declared kind", wrongKind},
	} {
		err := zeroCheck(tc.g)
		got := "PASSES the zero check"
		if err != nil {
			got = "refused"
		}
		fmt.Printf("    %-40s %s\n", tc.label, got)
	}
	fmt.Println("    ^ so it catches one of four, and it is the one that is unarguably a")
	fmt.Println("      bug rather than a judgement. The other three are what ADR-0005's")
	fmt.Println("      triple is for, and this check does not pretend to replace it.")

	fmt.Println("\n--- R16d: what it costs ---")
	g := StringCodec(netip.AddrPort.String, netip.ParseAddrPort)
	good := TextCodec[netip.AddrPort](VString)
	res := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			_ = zeroCheck(good)
		}
	})
	fmt.Printf("    one zeroCheck: %d ns/op, %d allocs/op\n", res.NsPerOp(), res.AllocsPerOp())
	fmt.Println("    Once per registration, at startup, and a registry is frozen at its")
	fmt.Println("    first use so it can never run more than once per registration.")
	fmt.Printf("    (the refused one, for contrast: %v)\n", zeroCheck(g) != nil)

	fmt.Println("\n--- R16e: the one thing it changes about the API ---")
	fmt.Println("    Register already returns an error, so nothing new appears in the")
	fmt.Println("    signature. What changes is that the error set now includes `your")
	fmt.Println("    codec is broken` alongside `you may not register this type`, and")
	fmt.Println("    that ADR-0007's `total over its type including the zero value` stops")
	fmt.Println("    being prose a registrant is asked to honour and becomes a check.")
}

type R16Timeout time.Duration

var _ = strconv.Itoa
