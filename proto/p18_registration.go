package main

// P18: the registration shape #12's decisions FORCE.
//
// The registration API is #19's and this probe does not propose one. What it
// does is write down the part #19 has no freedom over, because it falls out
// of decisions this ADR takes, and run it against real types so the
// obligations are checkable rather than prose.
//
// Five things are forced:
//   1. one call takes BOTH halves, because a codec is a pair
//   2. the boundary kind is a required argument, because P9 showed defaulting
//      it to String makes a numeric codec fail on YAML
//   3. the signature is generic over T, so there is no `any` round trip per
//      call and no reflect.Value in a user's face
//   4. no context.Context
//   5. registering a type core owns is a loud error, not an override
//
// What #19 still gets to choose is named at the bottom.

import (
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"net/url"
	"reflect"
	"time"
)

// The forced shape now lives in r_registry.go, shared with #19's probes.

// --- the probe --------------------------------------------------------------

type Timeout time.Duration

func runRegistration() {
	chainOrder, chainBeforeKind = []string{"text"}, true
	defer func() { chainOrder, chainBeforeKind = nil, false }()

	fmt.Println("\n--- P18a: the five registrations a real user actually writes ---")

	reg := NewRegistry()
	defer reg.install()()
	err := reg.Register(
		// 1. The gap this ADR leaves open by dropping the binary arm.
		StringCodec(
			func(u url.URL) string { return u.String() },
			func(s string) (url.URL, error) {
				p, err := url.Parse(s)
				if err != nil {
					return url.URL{}, err
				}
				return *p, nil
			}),

		// 2. ADR-0005's named hole: a named type over time.Duration.
		StringCodec(
			func(t Timeout) string { return time.Duration(t).String() },
			func(s string) (Timeout, error) {
				d, err := time.ParseDuration(s)
				return Timeout(d), err
			}),

		// 3. The kind declaration doing real work: big.Int's text IS a number,
		//    so it declares Number and loads from a YAML plane that says so.
		TypeCodec(VNumber,
			func(x big.Int) (Value, error) { return Number((&x).String()), nil },
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
			}),

		// 4. An INTERFACE. The codec owns the discriminator inside its own
		//    text, so ferry needs no type registry and the plane gets no
		//    ferry-specific tagging. Note it emits Null, so it accepts Null.
		TypeCodec(VString,
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
			}),
	)
	fmt.Printf("    Register(...) -> err=%v\n", err)

	type conf struct {
		Endpoint url.URL            `ferry:"endpoint"`
		Timeout  Timeout            `ferry:"timeout"`
		Big      big.Int            `ferry:"big"`
		Peer     net.Addr           `ferry:"peer"`
		Hosts    map[netip.Addr]int `ferry:"hosts"`
	}
	c := conf{
		Endpoint: mustU("https://example.com/a?q=1"),
		Timeout:  Timeout(90 * time.Second),
		Big:      *big.NewInt(1 << 40),
		Peer:     &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 80},
		Hosts:    map[netip.Addr]int{netip.MustParseAddr("10.0.0.1"): 1},
	}
	addrs, cerr := compile(reflect.TypeFor[conf]())
	fmt.Printf("    compile: %v err=%v\n", addrs, cerr)
	d, derr := dump(reflect.ValueOf(c))
	fmt.Printf("    dump err=%v\n", derr)
	for _, p := range sortedAddrs(d) {
		fmt.Printf("      %-16s %s\n", p, d[p].GoString())
	}
	fmt.Println("    as a real YAML file:")
	for _, l := range splitLines(p4yaml(c)) {
		fmt.Printf("      %s\n", l)
	}

	fmt.Println("\n--- P18b: the same thing loaded back, from BOTH plane shapes ---")
	for _, plane := range []struct {
		label string
		vals  map[Path]Value
	}{
		{"typed plane (what dump wrote)", d},
		{"flat plane (String for everything)", flattenAll(d)},
	} {
		var back conf
		lerr := load(plane.vals, reflect.ValueOf(&back).Elem())
		fmt.Printf("    %-36s err=%v\n", plane.label, lerr)
		fmt.Printf("      endpoint=%s timeout=%v big=%s peer=%v hosts=%v\n",
			back.Endpoint.String(), time.Duration(back.Timeout), (&back.Big).String(), back.Peer, back.Hosts)
	}
	fmt.Println("    ^ big.Int declared Number, so it loads from the typed plane that")
	fmt.Println("      says Number AND from the flat plane that says String. Declaring")
	fmt.Println("      String instead would have worked on the second and failed on the")
	fmt.Println("      first. That is why kind is a required argument.")

	fmt.Println("\n--- P18c: the two registrations that must fail ---")
	fmt.Printf("    %v\n", reg.Register(StringCodec(
		func(d time.Duration) string { return fmt.Sprint(int64(d)) },
		func(s string) (time.Duration, error) { return 0, nil })))
	fmt.Printf("    %v\n", reg.Register(StringCodec(
		func(u url.URL) string { return "" },
		func(s string) (url.URL, error) { return url.URL{}, nil })))

	fmt.Println("\n--- P18d: a codec that lies about its declared kind is caught ---")
	_ = reg.Register(TypeCodec(VNumber,
		func(s liar) (Value, error) { return String("not a number"), nil },
		func(v Value) (liar, error) { return "", nil }))
	_, lerr := dump(reflect.ValueOf(struct{ V liar }{"x"}))
	fmt.Printf("    dump err: %v\n", lerr)
	fmt.Println("    ^ that check is core's and costs one comparison. It is not a")
	fmt.Println("      substitute for the golden column, which is what catches a codec")
	fmt.Println("      that declares the right KIND and the wrong TEXT.")
}

type liar string

func flattenAll(in map[Path]Value) map[Path]Value {
	out := map[Path]Value{}
	for p, v := range in {
		if v.Kind() == VNull {
			out[p] = v
			continue
		}
		out[p] = String(v.Text())
	}
	return out
}
