package main

// P7: where TextAppender sits.
//
// v2 puts TextAppender AHEAD of TextMarshaler and xload's chain does not know
// it exists. The question is whether that is a separate ARM or a second
// spelling of one arm's encode half, and whether the preference buys anything.

import (
	"encoding"
	"fmt"
	"net/netip"
	"reflect"
	"testing"
	"time"
)

func runAppender() {
	fmt.Println("\n--- P7a: is there an 'AppendFrom' to pair TextAppender with? ---")
	fmt.Println("    encoding's exported interfaces on go1.27rc2:")
	for _, n := range []struct {
		name string
		t    reflect.Type
	}{
		{"TextMarshaler", ifTextM.t},
		{"TextAppender", ifTextA.t},
		{"TextUnmarshaler", ifTextU.t},
		{"BinaryMarshaler", ifBinM.t},
		{"BinaryAppender", ifBinA.t},
		{"BinaryUnmarshaler", ifBinU.t},
	} {
		m := n.t.Method(0)
		fmt.Printf("      %-20s %s%s\n", n.name, m.Name, m.Type)
	}
	fmt.Println("    There is no appending DECODER, so TextAppender cannot be an arm.")
	fmt.Println("    It is a second spelling of the text arm's encode half, answered by")
	fmt.Println("    the same TextUnmarshaler. That is why it sits inside the arm rather")
	fmt.Println("    than ahead of it.")

	fmt.Println("\n--- P7b: do the two spellings agree, on every stdlib type that has both? ---")
	rows := []struct {
		name string
		v    any
	}{
		{"time.Time", time.Unix(0, 0).UTC()},
		{"netip.Addr", netip.MustParseAddr("192.0.2.1")},
		{"netip.AddrPort", netip.MustParseAddrPort("192.0.2.1:80")},
		{"netip.Prefix", netip.MustParsePrefix("10.0.0.0/8")},
	}
	for _, r := range rows {
		m, _ := asIface(reflect.ValueOf(r.v), ifTextM.t)
		a, _ := asIface(reflect.ValueOf(r.v), ifTextA.t)
		mb, _ := m.(encoding.TextMarshaler).MarshalText()
		ab, _ := a.(encoding.TextAppender).AppendText(nil)
		fmt.Printf("    %-16s MarshalText=%-32q AppendText=%-32q agree=%v\n",
			r.name, mb, ab, string(mb) == string(ab))
	}
	fmt.Println("    They agree because the stdlib implements one in terms of the other.")
	fmt.Println("    Nothing enforces that for a user type, which is a prose obligation")
	fmt.Println("    ferry inherits the moment it prefers one spelling over the other.")

	fmt.Println("\n--- P7c: what preferring TextAppender is worth, per leaf ---")
	v := netip.MustParseAddr("192.0.2.1")
	viaM := func() Value {
		b, _ := v.MarshalText()
		return String(string(b))
	}
	scratch := make([]byte, 0, 64)
	viaA := func() Value {
		b, _ := v.AppendText(scratch[:0])
		return String(string(b))
	}
	r1 := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			sink = viaM()
		}
	})
	r2 := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			sink = viaA()
		}
	})
	fmt.Printf("    %-28s %7.1f ns/op %5d B/op %3d allocs/op\n", "MarshalText -> string",
		float64(r1.NsPerOp()), r1.AllocedBytesPerOp(), r1.AllocsPerOp())
	fmt.Printf("    %-28s %7.1f ns/op %5d B/op %3d allocs/op\n", "AppendText(scratch) -> string",
		float64(r2.NsPerOp()), r2.AllocedBytesPerOp(), r2.AllocsPerOp())
	fmt.Println("    ferry pays one unavoidable allocation to make the Go string that")
	fmt.Println("    lives in Value.text. The appender removes the other one.")

	fmt.Println("\n--- P7d: and it is not on a hot path ---")
	fmt.Println("    ADR-0003 measured a twelve-key cached load at 476 ns. A dump of")
	fmt.Println("    twelve text leaves saves whatever the delta above is, twelve times,")
	fmt.Println("    once per Load or Dump. This is a tidiness win, not a performance")
	fmt.Println("    argument, and the ADR should say which it is.")
}
