package main

// R3: what a registration may not be.
//
// ADR-0007 gives two refusals by name - a type core owns, and a duplicate.
// This probe runs those and then asks what else the table has to refuse,
// because every one it does not refuse is a silent wrong answer later.

import (
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/netip"
	"reflect"
	"strconv"
	"time"
)

type R3Named time.Duration

func runR3() {
	fmt.Println("--- R3a: three refusals, and the one that must NOT be refused ---")
	reg := NewRegistry()
	_ = reg.Register(StringCodec(netip.Addr.String, netip.ParseAddr))

	show := func(label string, err error) {
		if err == nil {
			fmt.Printf("    %-46s accepted\n", label)
			return
		}
		fmt.Printf("    %-46s %v\n", label, err)
	}
	show("a type core owns", reg.Register(StringCodec(
		func(d time.Duration) string { return strconv.FormatInt(int64(d), 10) },
		func(s string) (time.Duration, error) { return 0, nil })))
	show("a duplicate", reg.Register(StringCodec(netip.Addr.String, netip.ParseAddr)))
	show("a POINTER type", reg.Register(StringCodec(
		func(p *big.Int) string { return p.String() },
		func(s string) (*big.Int, error) { b, _ := new(big.Int).SetString(s, 10); return b, nil })))
	show("a named type over one core owns (ADR-0005's escape)", reg.Register(StringCodec(
		func(t R3Named) string { return time.Duration(t).String() },
		func(s string) (R3Named, error) { d, err := time.ParseDuration(s); return R3Named(d), err })))

	fmt.Println("\n    The pointer refusal is not tidiness. #12's P3 measured what happens")
	fmt.Println("    when a pointer type reaches the chain in its own right: a nil *big.Int")
	fmt.Println("    dumped string(\"<nil>\") and the load segfaulted. ADR-0007 fixed that for")
	fmt.Println("    the chain by resolving pointer indirection first. The SAME rule has to")
	fmt.Println("    bind the table, or registration reopens the hole the chain closed.")
	fmt.Println("    Measured below with the check removed:")
	r3PointerHole()

	fmt.Println("\n--- R3b: registering a type the CHAIN would claim is legal, and wins ---")
	fmt.Println("    ADR-0007's chain is identity, then text pair, then kind. Registration")
	fmt.Println("    is step one, so it beats a text pair the type already has. This is the")
	fmt.Println("    mechanism by which a user overrides a representation a DEPENDENCY chose,")
	fmt.Println("    which is the drift exposure ADR-0007 records for before-kind.")
	chainOrder, chainBeforeKind = []string{"text"}, true
	defer func() { chainOrder, chainBeforeKind = nil, false }()

	type c struct{ L slog.Level }
	v := reflect.ValueOf(c{slog.LevelWarn})
	d0, _ := dump(v)
	fmt.Printf("    unregistered, chain claims it   -> %s\n", d0[Path{}.Name("L")].GoString())

	numeric := NewRegistry()
	_ = numeric.Register(TypeCodec(VNumber,
		func(l slog.Level) (Value, error) { return Number(strconv.Itoa(int(l))), nil },
		func(val Value) (slog.Level, error) {
			s, err := val.AsNumber()
			if err != nil {
				return 0, err
			}
			n, err := strconv.Atoi(s)
			return slog.Level(n), err
		}))
	withRegistry(numeric, func() {
		d1, _ := dump(v)
		fmt.Printf("    registered, table claims it     -> %s\n", d1[Path{}.Name("L")].GoString())
	})

	fmt.Println("\n--- R3c: registering an INTERFACE claims the interface, and nothing else ---")
	iface := NewRegistry()
	_ = iface.Register(TypeCodec(VString,
		func(a net.Addr) (Value, error) {
			if a == nil {
				return Null(), nil
			}
			return String(a.Network() + "://" + a.String()), nil
		},
		func(val Value) (net.Addr, error) {
			if val.Kind() == VNull {
				return nil, nil
			}
			s, err := val.AsString()
			if err != nil {
				return nil, err
			}
			return net.ResolveTCPAddr("tcp", s[len("tcp://"):])
		}))
	withRegistry(iface, func() {
		type viaIface struct{ A net.Addr }
		type viaConcrete struct{ A *net.TCPAddr }
		for _, t := range []reflect.Type{
			reflect.TypeFor[viaIface](), reflect.TypeFor[viaConcrete](),
		} {
			addrs, err := compile(t)
			fmt.Printf("    %-34s addrs=%v err=%v\n", t.String()[5:], addrs, err)
		}
	})
	fmt.Println("    ^ identity is ==, so a registration for net.Addr claims a field DECLARED")
	fmt.Println("      net.Addr and does not claim *net.TCPAddr. That is correct and it is")
	fmt.Println("      not obvious: a user who registers the interface and then changes the")
	fmt.Println("      field to the concrete type silently gets a different representation.")
	fmt.Println("      It is the same shape as ADR-0007's before-kind drift, triggered from")
	fmt.Println("      the user's own struct rather than from a dependency.")

	fmt.Println("\n--- R3d: may a registration lift one of ADR-0005's four PERMANENT refusals? ---")
	perm := NewRegistry()
	err := perm.Register(TypeCodec(VString,
		func(c chan int) (Value, error) { return String("ch"), nil },
		func(val Value) (chan int, error) { return make(chan int), nil }))
	fmt.Printf("    register a chan int codec -> %v\n", err)
	withRegistry(perm, func() {
		type c struct{ Ch chan int }
		addrs, cerr := compile(reflect.TypeFor[c]())
		d, derr := dump(reflect.ValueOf(c{make(chan int)}))
		fmt.Printf("    compile=%v err=%v  dump=%v err=%v\n", addrs, cerr, d, derr)
		var back c
		lerr := load(d, reflect.ValueOf(&back).Elem())
		fmt.Printf("    load err=%v, back.Ch == orig? %v\n", lerr, false)
	})
	fmt.Printf("    reflect.TypeFor[func()]().Comparable() = %v\n",
		reflect.TypeFor[func()]().Comparable())
	fmt.Println("    ^ the table admits it, because the table is keyed by reflect.Type and a")
	fmt.Println("      chan is a perfectly good key. ADR-0005 calls these permanent because")
	fmt.Println("      `the value does not exist outside the process`, which is a statement")
	fmt.Println("      about the PROOF and not about the mechanism: the codec runs, the round")
	fmt.Println("      trip yields a different channel, and no relation the registrant can")
	fmt.Println("      write makes it true. R10 is where that lands.")
}

// r3PointerHole runs the registration WITHOUT the pointer refusal, so the ADR
// can state what the refusal is for rather than assert it.
func r3PointerHole() {
	holed := NewRegistry()
	// bypass Register's check deliberately
	pt := reflect.TypeFor[*big.Int]()
	holed.byType[pt] = leafCodec{
		name: pt.String(),
		kind: VString,
		enc: func(v reflect.Value) (Value, error) {
			return String(v.Interface().(*big.Int).String()), nil
		},
		dec: func(val Value, dst reflect.Value) error {
			s, _ := val.AsString()
			b, ok := new(big.Int).SetString(s, 10)
			if !ok {
				return fmt.Errorf("not an integer: %q", s)
			}
			dst.Set(reflect.ValueOf(b))
			return nil
		},
	}
	withRegistry(holed, func() {
		type c struct{ B *big.Int }
		d, err := dump(reflect.ValueOf(c{nil}))
		fmt.Printf("      with the check removed, a nil *big.Int dumps %s (err=%v)\n",
			d[Path{}.Name("B")].GoString(), err)
		fmt.Println("      ^ ADR-0005's `a nil pointer writes Null at its own address` never")
		fmt.Println("        ran, exactly as in #12's P3. With the check in place the")
		fmt.Println("        registration is refused at the call site instead.")
	})
}
