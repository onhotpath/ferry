package main

// The interaction with #12 that decides how big the "surprising
// representation" category is.
//
// classify() consults the identity table, then reflect.Kind. If #12's codec
// chain also consults encoding.TextMarshaler/TextUnmarshaler, and does so
// BEFORE kind, then a large set of types stops being refused or
// mis-represented with no registration at all. Quantify it.

import (
	"encoding"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"net/url"
	"reflect"
	"time"
)

func hasTextPair(t reflect.Type) bool {
	tm := reflect.TypeFor[encoding.TextMarshaler]()
	tu := reflect.TypeFor[encoding.TextUnmarshaler]()
	return (t.Implements(tm) || reflect.PointerTo(t).Implements(tm)) &&
		reflect.PointerTo(t).Implements(tu)
}

func runChainOrder() {
	type row struct {
		name string
		t    reflect.Type
		v    any
	}
	rows := []row{
		{"netip.Addr", reflect.TypeFor[netip.Addr](), netip.MustParseAddr("192.0.2.1")},
		{"netip.AddrPort", reflect.TypeFor[netip.AddrPort](), netip.MustParseAddrPort("192.0.2.1:80")},
		{"netip.Prefix", reflect.TypeFor[netip.Prefix](), netip.MustParsePrefix("10.0.0.0/8")},
		{"big.Int", reflect.TypeFor[big.Int](), *big.NewInt(1 << 40)},
		{"net.IP", reflect.TypeFor[net.IP](), net.ParseIP("192.0.2.1")},
		{"net.IPNet", reflect.TypeFor[net.IPNet](), mustIPNet("10.0.0.0/8")},
		{"url.URL", reflect.TypeFor[url.URL](), mustU("https://h/a")},
		{"time.Time", reflect.TypeFor[time.Time](), time.Unix(0, 0).UTC()},
		{"time.Duration", reflect.TypeFor[time.Duration](), time.Second},
		{"UUID [16]byte", reflect.TypeFor[UUID](), UUID{1, 2, 3}},
		{"Node (recursive)", reflect.TypeFor[Node](), Node{"a", nil}},
	}
	fmt.Printf("  %-18s %-10s %-24s %s\n", "type", "text pair", "today (kind only)", "if the chain ran first")
	fmt.Println("  " + dashes(100))
	for _, r := range rows {
		tp := hasTextPair(r.t)
		// what happens today
		today := "leaf, pinned"
		if _, owned := identityLookup(r.t); !owned {
			h := reflect.New(reflect.StructOf([]reflect.StructField{{Name: "V", Type: r.t}})).Elem()
			if _, err := compile(h.Type()); err != nil {
				today = "REFUSED"
			} else {
				h.Field(0).Set(reflect.ValueOf(r.v))
				d, _ := dump(h)
				var s string
				for _, p := range sortedAddrs(d) {
					s += d[p].GoString() + " "
				}
				if len(s) > 22 {
					s = s[:22] + "..."
				}
				today = "admitted: " + s
			}
		}
		would := "unchanged"
		if tp {
			p := reflect.New(r.t)
			p.Elem().Set(reflect.ValueOf(r.v))
			b, err := p.Interface().(encoding.TextMarshaler).MarshalText()
			if err != nil {
				would = "text err: " + err.Error()
			} else {
				would = fmt.Sprintf("string(%q)", string(b))
			}
		}
		fmt.Printf("  %-18s %-10v %-24s %s\n", r.name, tp, today, would)
	}
	fmt.Println()
	fmt.Println("  Reading: every row where 'text pair' is true and 'today' is REFUSED or")
	fmt.Println("  an unreadable blob is a type the chain order alone decides.")
}
