package main

// P3: receiver and addressability mechanics.
//
// Survey item 5.14's last bullet is that a method set differs between a value
// and its pointer, and ADR-0005 refused fmt.Stringer partly for it. Every arm
// in the chain has the same exposure, so measure it rather than assume the
// pointer probe is enough.

import (
	"encoding"
	"fmt"
	"math/big"
	"net/netip"
	"reflect"
	"time"
)

func runReceivers() {
	chainOrder = []string{"text"}
	chainBeforeKind = true
	defer func() { chainOrder = nil; chainBeforeKind = false }()

	fmt.Println("\n--- P3a: where the text methods live, per type ---")
	fmt.Printf("    %-18s %-12s %-12s %-12s\n", "type", "MarshalText", "AppendText", "UnmarshalText")
	fmt.Println("    " + dashes(60))
	for _, r := range []censusRow{
		{"big.Int", reflect.TypeFor[big.Int]()},
		{"netip.Addr", reflect.TypeFor[netip.Addr]()},
		{"time.Time", reflect.TypeFor[time.Time]()},
	} {
		fmt.Printf("    %-18s %-12s %-12s %-12s\n", r.name,
			impl(r.t, ifTextM), impl(r.t, ifTextA), impl(r.t, ifTextU))
	}
	fmt.Println("    V = on the value receiver, P = pointer only.")
	fmt.Println("    A P on the ENCODE side is the case a naive probe misses entirely:")
	fmt.Println("    big.Int does not implement TextMarshaler; only *big.Int does.")

	fmt.Println("\n--- P3b: a pointer-receiver encoder against an UNADDRESSABLE value ---")
	m := map[string]big.Int{"a": *big.NewInt(1 << 40)}
	mv := reflect.ValueOf(m).MapIndex(reflect.ValueOf("a"))
	fmt.Printf("    map value CanAddr: %v\n", mv.CanAddr())
	if _, ok := asIface(mv, ifTextM.t); ok {
		c, _, _ := textCodecFor(reflect.TypeFor[big.Int]())
		v, err := c.enc(mv)
		fmt.Printf("    encode via a copy  -> %s err=%v\n", v.GoString(), err)
	}
	fmt.Println("    ^ a map value is not addressable, so ferry copies to call a")
	fmt.Println("      pointer-receiver method. Harmless for an encoder, which does not")
	fmt.Println("      mutate. A DECODER cannot be served this way, and does not have to")
	fmt.Println("      be: the walk always decodes into a fresh addressable element.")

	fmt.Println("\n--- P3c: the nil-pointer field, which is the panic case ---")
	type withPtr struct{ B *big.Int }
	var wp withPtr
	fmt.Printf("    *big.Int nil, MarshalText via the interface: ")
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("PANIC: %v\n", r)
			}
		}()
		b, err := any(wp.B).(encoding.TextMarshaler).MarshalText()
		fmt.Printf("%q err=%v\n", b, err)
	}()
	fmt.Println("    ferry never reaches that: ADR-0005 makes a nil pointer Null at the")
	fmt.Println("      pointer's own address and the pointee is never visited. Measured:")
	addrs, err := compile(reflect.TypeFor[withPtr]())
	fmt.Printf("      compile %v err=%v\n", addrs, err)
	d, err := dump(reflect.ValueOf(wp))
	fmt.Printf("      dump    %v err=%v\n", fmtVals(d), err)
	wp2 := withPtr{B: big.NewInt(5)}
	d2, _ := dump(reflect.ValueOf(wp2))
	fmt.Printf("      dump    %v (non-nil)\n", fmtVals(d2))
	var back withPtr
	err = load(d2, reflect.ValueOf(&back).Elem())
	fmt.Printf("      load    %v err=%v\n", back.B, err)

	fmt.Println("\n--- P3d: the probe ferry must use ---")
	fmt.Println("    encode half: T or *T implements TextMarshaler/TextAppender")
	fmt.Println("    decode half: *T implements TextUnmarshaler  (T alone is useless:")
	fmt.Println("      a value-receiver UnmarshalText cannot mutate the destination)")
	fmt.Println("    Measured, a value-receiver UnmarshalText silently does nothing:")
	var vr valRecvUnmarshal
	c, ok, half := textCodecFor(reflect.TypeFor[valRecvUnmarshal]())
	fmt.Printf("      pair complete? %v  half: %q\n", ok, half)
	if ok {
		rv := reflect.ValueOf(&vr).Elem()
		err := c.dec(String("hello"), rv)
		fmt.Printf("      after decode: %+v err=%v   <- the write went to a copy\n", vr, err)
	}
}

func fmtVals(d map[Path]Value) string {
	var s string
	for _, p := range sortedAddrs(d) {
		s += p.String() + "=" + d[p].GoString() + " "
	}
	return s
}

// valRecvUnmarshal declares UnmarshalText on the VALUE receiver, which
// satisfies the interface for both T and *T and writes to a copy.
type valRecvUnmarshal struct{ S string }

func (v valRecvUnmarshal) MarshalText() ([]byte, error) { return []byte(v.S), nil }
func (v valRecvUnmarshal) UnmarshalText(b []byte) error { v.S = string(b); return nil }
