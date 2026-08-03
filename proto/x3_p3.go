package main

// X3-3. A custom encoder/decoder for net.IPNet, asked for by name.
//
// net.IPNet has a natural text form - 10.0.0.0/8 - and an inverse pair for it
// in the standard library: (*net.IPNet).String() and net.ParseCIDR. So the
// obvious registration is
//
//	StringCodec(func(n net.IPNet) string { return n.String() }, parseCIDR)
//
// and the question is whether it survives ADR-0009's zero-value check, because
//
//	net.IPNet{}.String() == "<nil>"
//
// and net.ParseCIDR will not take "<nil>". That is ADR-0009's own netip.Addr
// case, on a different type, reached by writing the obvious thing.

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"reflect"
	"strings"
)

// x3IPNetString is the obvious registration. It is what a user writes.
func x3IPNetString() Reg {
	return StringCodec(
		func(n net.IPNet) string { return n.String() },
		func(s string) (net.IPNet, error) {
			_, ipn, err := net.ParseCIDR(s)
			if err != nil {
				return net.IPNet{}, err
			}
			return *ipn, nil
		})
}

// x3IPNetTotal is x3IPNetString made total over the zero value by keeping the
// zero on the STRING side, as the empty string. It is still a StringCodec, so
// its boundary kind is VString and every plane can carry it.
func x3IPNetTotal() Reg {
	return StringCodec(
		func(n net.IPNet) string {
			if n.IP == nil && n.Mask == nil {
				return ""
			}
			return n.String()
		},
		func(s string) (net.IPNet, error) {
			if s == "" {
				return net.IPNet{}, nil
			}
			_, ipn, err := net.ParseCIDR(s)
			if err != nil {
				return net.IPNet{}, err
			}
			return *ipn, nil
		})
}

// x3IPNetNull is the other remedy: the zero value is ABSENCE, so say so with
// the kind rather than with a sentinel string. Only ValueCodec can express it,
// because it is the only constructor whose decode half sees the whole Value.
// The cost is measured below: it is unrepresentable on a plane with no null.
func x3IPNetNull() Reg {
	return ValueCodec[net.IPNet](VString,
		func(n net.IPNet) (Value, error) {
			if n.IP == nil && n.Mask == nil {
				return Null(), nil
			}
			return String(n.String()), nil
		},
		func(v Value) (net.IPNet, error) {
			if v.Kind() == VNull {
				return net.IPNet{}, nil
			}
			s, err := v.AsString()
			if err != nil {
				return net.IPNet{}, err
			}
			_, ipn, err := net.ParseCIDR(s)
			if err != nil {
				return net.IPNet{}, err
			}
			return *ipn, nil
		})
}

func runX3_3() {
	fmt.Println("--- X3-3a: the natural text form, and its inverse ---")
	_, ipn, err := net.ParseCIDR("10.0.0.0/8")
	fmt.Printf("    net.ParseCIDR(\"10.0.0.0/8\") -> %v, err=%v\n", ipn, err)
	fmt.Printf("    ipn.String()                -> %q\n", ipn.String())
	var zero net.IPNet
	fmt.Printf("    net.IPNet{}.String()        -> %q\n", zero.String())
	_, _, zerr := net.ParseCIDR(zero.String())
	fmt.Printf("    net.ParseCIDR(%q)       -> err=%v\n", zero.String(), zerr)
	fmt.Println("\n    A total inverse pair over every non-zero value, and not over the")
	fmt.Println("    zero. That is exactly ADR-0009's netip.Addr shape:")
	quoteX3(
		"Register(StringCodec(netip.Addr.String, netip.ParseAddr)) is not total over",
		"the zero value: it encodes to string(\"invalid IP\") and decoding that back fails.",
	)
	fmt.Println("    Note also that String() is on the POINTER receiver, so there is no")
	fmt.Println("    `net.IPNet.String` method expression to hand StringCodec: the closure")
	fmt.Println("    is forced, which is a second small difference from the netip case.")
	fmt.Printf("    net.IPNet implements fmt.Stringer:  %v\n",
		reflect.TypeFor[net.IPNet]().Implements(reflect.TypeFor[fmt.Stringer]()))
	fmt.Printf("    *net.IPNet implements fmt.Stringer: %v\n",
		reflect.PointerTo(reflect.TypeFor[net.IPNet]()).Implements(reflect.TypeFor[fmt.Stringer]()))
	_, textOK, half := textCodecFor(reflect.TypeFor[net.IPNet]())
	fmt.Printf("    the text pair, either receiver:     %v  (half=%q)\n", textOK, half)
	fmt.Println("    -> so ADR-0007's chain cannot claim it and never will. net.IPNet is")
	fmt.Println("       one of the three rows ADR-0005's chain table marks `unchanged`.")

	fmt.Println("\n--- X3-3b: does it survive Register's zero-value check? ---")
	fmt.Println("    #41 D4 put ADR-0009's check inside Register, where the ADR says it")
	fmt.Println("    lives. So this is the first prototype on which the obvious codec is")
	fmt.Println("    refused at the registration call rather than at the first zero value.")
	for _, c := range []struct {
		label string
		g     Reg
	}{
		{"StringCodec(n.String, ParseCIDR)   [the obvious one]", x3IPNetString()},
		{"StringCodec, zero <-> \"\"           [remedy 1]", x3IPNetTotal()},
		{"ValueCodec,  zero <-> Null         [remedy 2]", x3IPNetNull()},
	} {
		r := NewRegistry()
		err := r.Register(c.g)
		st := "accepted"
		if err != nil {
			st = "REFUSED"
		}
		fmt.Printf("\n    %-52s %s\n", c.label, st)
		if err != nil {
			for _, l := range strings.Split(err.Error(), "\n") {
				fmt.Printf("      %s\n", l)
			}
		}
	}
	fmt.Println("\n    So the answer is: the obvious codec is refused, exactly as ADR-0009's")
	fmt.Println("    netip.Addr case predicts, and a codec that handles the zero value is")
	fmt.Println("    the remedy. Two shapes of remedy, measured against each other below.")

	fmt.Println("\n--- X3-3c: what the two remedies cost, on three planes ---")
	dir, _ := os.MkdirTemp("", "x3c")
	defer os.RemoveAll(dir)
	vals := []struct {
		label string
		v     net.IPNet
	}{
		{"10.0.0.0/8", *ipn},
		{"2001:db8::/32", x3MustCIDR("2001:db8::/32")},
		{"192.0.2.1/32", x3MustCIDR("192.0.2.1/32")},
		{"the zero value", net.IPNet{}},
	}
	for _, rem := range []struct {
		label string
		g     Reg
	}{
		{"remedy 1: zero <-> \"\"   (StringCodec, kind String)", x3IPNetTotal()},
		{"remedy 2: zero <-> Null (ValueCodec,  kind String)", x3IPNetNull()},
	} {
		fmt.Printf("\n    %s\n", rem.label)
		reg := NewRegistry()
		if err := reg.Register(rem.g); err != nil {
			fmt.Println("      registration refused:", err)
			continue
		}
		for _, pl := range []Plane{memoryPlane(), yamlPlane(dir), flatPlane()} {
			fmt.Printf("      %s\n", pl.Name)
			for _, v := range vals {
				x3Report(reg, pl, v.label, v.v,
					func(a, b net.IPNet) bool { return a.String() == b.String() },
					func(n net.IPNet) string { return n.String() })
			}
		}
	}
	fmt.Println("\n    Remedy 1 round-trips every value on every plane, including the flat")
	fmt.Println("    one. Remedy 2 loses the zero value on the flat plane, loudly, because")
	fmt.Println("    flatKinds does not contain VNull and ADR-0005 requires the driver to")
	fmt.Println("    refuse rather than mangle. That is the declaration working; it is")
	fmt.Println("    still a codec that is unusable on env, query params and Consul.")

	fmt.Println("\n--- X3-3d: the boundary Value kind, and what lands on the plane ---")
	reg := NewRegistry()
	_ = reg.Register(x3IPNetTotal())
	o := defaultOpts()
	o.reg = reg
	s, cerr := compileOnce(reflect.TypeFor[x3Box[net.IPNet]](), o)
	fmt.Printf("    compile -> %v, address set %v\n", cerr, s.addrs)
	done := reg.install()
	lc, _ := resolveLeaf(reflect.TypeFor[net.IPNet]())
	fmt.Printf("    the leaf codec chosen: name=%q kind=%v\n", lc.name, lc.kind)
	done()
	ctx := context.Background()
	got, derr := dumpTo(ctx, x3Box[net.IPNet]{*ipn}, WithRegistry(reg))
	fmt.Printf("    dump 10.0.0.0/8 -> err=%v\n", derr)
	dumpAddrs(got)
	got0, _ := dumpTo(ctx, x3Box[net.IPNet]{}, WithRegistry(reg))
	fmt.Println("    dump the zero value ->")
	dumpAddrs(got0)
	fmt.Println("\n    VString, one address, and it is /v. ADR-0005's row for the same type")
	fmt.Println("    is two VBytes addresses at /v/IP and /v/Mask. The registration does")
	fmt.Println("    not restore the published representation; it replaces it with a better")
	fmt.Println("    one and with a different address set.")

	fmt.Println("\n--- X3-3e: the map-key half does not arise for this type, and Go says so ---")
	fmt.Printf("    reflect.TypeFor[net.IPNet]().Comparable() = %v\n",
		reflect.TypeFor[net.IPNet]().Comparable())
	fmt.Println("    net.IPNet holds two slices, so map[net.IPNet]V is not a Go type and")
	fmt.Println("    `invalid map key type net.IPNet` is a COMPILER error before ferry is")
	fmt.Println("    reached. .AsMapKey() on this registration is unreachable, which is")
	fmt.Println("    worth stating because ADR-0009's opt-in is now the default (#41 D5)")
	fmt.Println("    and a reader would otherwise expect the question to be asked.")
	fmt.Println("    The comparable neighbour with the same text form is netip.Prefix, and")
	fmt.Println("    it is where #41 D5's reported hole showed. As FOUND the first row")
	fmt.Println("    below compiled: the CHAIN claimed netip.Prefix as a key with nobody")
	fmt.Println("    having said .AsMapKey(). That became #45, and ADR-0007 reversed its")
	fmt.Println("    own sentence - keying a map is registration-only - so all three rows")
	fmt.Println("    now run under one rule:")
	for _, c := range []struct {
		label string
		reg   *Registry
	}{
		{"nothing registered at all", NewRegistry()},
		{"registered without .AsMapKey()", x3PrefixReg(false)},
		{"registered with    .AsMapKey()", x3PrefixReg(true)},
	} {
		if c.reg == nil {
			fmt.Printf("    %-38s registration refused by the zero check\n", c.label)
			continue
		}
		oo := defaultOpts()
		oo.reg = c.reg
		_, e := compileOnce(reflect.TypeFor[x3PrefixMap](), oo)
		fmt.Printf("    %-38s Compile[map[netip.Prefix]int] -> %v\n", c.label, e)
	}

	runX3_3f()
}

// runX3_3f is the half of the obligation the zero check cannot discharge, and
// it is on this codec rather than on a contrived one.
func runX3_3f() {
	fmt.Println("\n--- X3-3f: the half Register CANNOT check, on this very codec ---")
	fmt.Println("    ADR-0009's check is `dec(enc(zero))` does not ERROR. ADR-0005 keeps")
	fmt.Println("    the equality obligation with the registrant, because equality needs a")
	fmt.Println("    relation. Both remedies above pass the check and both are LOSSY on a")
	fmt.Println("    value net.IPNet can hold and ParseCIDR normalises away:")
	lossy := net.IPNet{IP: net.ParseIP("192.0.2.5"), Mask: net.CIDRMask(24, 32)}
	fmt.Printf("      in       IP=%v Mask=%v  String()=%q\n", lossy.IP, lossy.Mask, lossy.String())
	reg := NewRegistry()
	if err := reg.Register(x3IPNetTotal()); err != nil {
		fmt.Println("      registration refused:", err)
		return
	}
	out, de, le := x3Trip(reg, memoryPlane(), lossy)
	fmt.Printf("      out      IP=%v Mask=%v  String()=%q  dumpErr=%v loadErr=%v\n",
		out.IP, out.Mask, out.String(), de, le)
	fmt.Printf("      String() equal: %v      reflect.DeepEqual: %v\n",
		lossy.String() == out.String(), reflect.DeepEqual(lossy, out))
	fmt.Println("    net.ParseCIDR masks the host bits off, so 192.0.2.5/24 loads back as")
	fmt.Println("    192.0.2.0/24 and the two are equal under String() and not under")
	fmt.Println("    DeepEqual. Register accepted the codec, both planes carried it, and")
	fmt.Println("    nothing in ferry noticed. That is ADR-0005's triple - a relation and")
	fmt.Println("    a value list in the harness - being the only thing that catches it,")
	fmt.Println("    which is what ADR-0009's R16c row already says and is worth having a")
	fmt.Println("    measurement of on the type the owner asked about.")
}

type x3PrefixMap struct {
	M map[netip.Prefix]int `ferry:"m"`
}

// x3PrefixReg registers netip.Prefix with a codec that IS total over the zero
// value, so the row measures the opt-in and not the zero check.
func x3PrefixReg(asKey bool) *Registry {
	g := StringCodec(
		func(p netip.Prefix) string {
			if !p.IsValid() {
				return ""
			}
			return p.String()
		},
		func(s string) (netip.Prefix, error) {
			if s == "" {
				return netip.Prefix{}, nil
			}
			return netip.ParsePrefix(s)
		})
	if asKey {
		g = g.AsMapKey()
	}
	r := NewRegistry()
	if err := r.Register(g); err != nil {
		return nil
	}
	return r
}

func x3MustCIDR(s string) net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return *n
}
