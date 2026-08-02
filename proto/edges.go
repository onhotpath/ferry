package main

// What actually happens to the types people reach for. The ADR presents two
// outcomes, in the set or refused. Check whether that is true.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"net/url"
	"reflect"
	"time"
)

type UUID [16]byte

func classifyOutcome(t reflect.Type, v reflect.Value) (string, string) {
	h := reflect.New(reflect.StructOf([]reflect.StructField{{Name: "V", Type: t}})).Elem()
	if v.IsValid() {
		h.Field(0).Set(v)
	}
	if _, err := compile(h.Type()); err != nil {
		return "REFUSED at compile", shorten(err.Error())
	}
	d, err := dump(h)
	if err != nil {
		return "refused at dump", shorten(err.Error())
	}
	var parts []string
	for _, p := range sortedAddrs(d) {
		parts = append(parts, p.String()+"="+d[p].GoString())
	}
	// does it round-trip?
	back := reflect.New(h.Type()).Elem()
	if err := load(d, back); err != nil {
		return "admitted, load fails", shorten(err.Error())
	}
	rt := "round-trips"
	if !reflect.DeepEqual(h.Interface(), back.Interface()) {
		rt = "ROUND TRIP DIFFERS"
	}
	return "admitted: " + rt, shorten(fmt.Sprint(parts))
}

func shorten(s string) string {
	if len(s) > 92 {
		return s[:92] + "..."
	}
	return s
}

func runEdges() {
	ny, _ := time.LoadLocation("America/New_York")
	cases := []struct {
		name string
		v    any
	}{
		{"net.Addr (interface)", (*net.Addr)(nil)},
		{"netip.Addr", netip.MustParseAddr("192.0.2.1")},
		{"netip.AddrPort", netip.MustParseAddrPort("192.0.2.1:80")},
		{"netip.Prefix", netip.MustParsePrefix("10.0.0.0/8")},
		{"net.IP", net.ParseIP("192.0.2.1")},
		{"net.IPNet", mustIPNet("10.0.0.0/8")},
		{"net.TCPAddr", net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 80}},
		{"url.URL", mustU("https://u:p@h/a?x=1")},
		{"big.Int", *big.NewInt(1 << 40)},
		{"UUID ([16]byte)", UUID{0x01, 0x23, 0x45, 0x67}},
		{"json.RawMessage", json.RawMessage(`{"a":1}`)},
		{"sql.NullString", sql.NullString{String: "x", Valid: true}},
		{"time.Time (NY)", time.Date(2026, 8, 2, 12, 0, 0, 0, ny)},
		{"time.Duration", 90 * time.Minute},
		{"type Port int", Port(8080)},
	}
	fmt.Printf("  %-22s %-24s %s\n", "type", "outcome", "what lands on the plane")
	fmt.Println("  " + dashes(110))
	for _, c := range cases {
		t := reflect.TypeOf(c.v)
		v := reflect.ValueOf(c.v)
		if c.name == "net.Addr (interface)" {
			t = reflect.TypeFor[net.Addr]()
			v = reflect.Value{}
		}
		out, detail := classifyOutcome(t, v)
		fmt.Printf("  %-22s %-24s %s\n", c.name, out, detail)
	}
}

func dashes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '-'
	}
	return string(b)
}

func mustU(s string) url.URL   { u, _ := url.Parse(s); return *u }
func mustIPNet(s string) net.IPNet { _, n, _ := net.ParseCIDR(s); return *n }
