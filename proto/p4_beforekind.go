package main

// P4: the chain before kind admission, or after it.
//
// ADR-0005 handed this over with a partial measurement: TextMarshaler before
// kind rescues four refusals and makes net.IP readable. What it did NOT
// measure is what before-kind COSTS, and there are three candidate costs:
//
//   - a type currently admitted by kind and round-tripping exactly may stop
//     round-tripping exactly, because a text form can normalise
//   - a struct with a text pair collapses from N addresses to one
//   - the plane artefact changes for types nobody registered
//
// Run the whole thing three ways over one fixture list, and diff.

import (
	"bytes"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/netip"
	"net/url"
	"reflect"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"golang.org/x/text/language"
)

// normalisingText is the hazard class: MarshalText is not the identity on
// the underlying value, so text round-trips to a DIFFERENT value.
type normalisingText string

func (v normalisingText) MarshalText() ([]byte, error) {
	return []byte(strings.ToUpper(string(v))), nil
}
func (v *normalisingText) UnmarshalText(b []byte) error {
	*v = normalisingText(b)
	return nil
}

// structWithText is kind-admissible (two exported fields) AND carries a text
// pair. Before kind it is one leaf; after kind it is two addresses.
type structWithText struct {
	Major int
	Minor int
}

func (v structWithText) MarshalText() ([]byte, error) {
	return fmt.Appendf(nil, "%d.%d", v.Major, v.Minor), nil
}
func (v *structWithText) UnmarshalText(b []byte) error {
	_, err := fmt.Sscanf(string(b), "%d.%d", &v.Major, &v.Minor)
	return err
}

type p4row struct {
	name string
	t    reflect.Type
	v    any
	// eq is the type's own equality relation, ADR-0005's per-type relation.
	eq func(a, b any) bool
}

func p4rows() []p4row {
	deepEq := func(a, b any) bool { return reflect.DeepEqual(a, b) }
	return []p4row{
		{"netip.Addr", reflect.TypeFor[netip.Addr](), netip.MustParseAddr("192.0.2.1"), deepEq},
		{"netip.AddrPort", reflect.TypeFor[netip.AddrPort](), netip.MustParseAddrPort("192.0.2.1:80"), deepEq},
		{"netip.Prefix", reflect.TypeFor[netip.Prefix](), netip.MustParsePrefix("10.0.0.0/8"), deepEq},
		{"big.Int", reflect.TypeFor[big.Int](), *big.NewInt(1 << 40), func(a, b any) bool {
			x, y := a.(big.Int), b.(big.Int)
			return x.Cmp(&y) == 0
		}},
		{"net.IP v4-in-v6", reflect.TypeFor[net.IP](), net.ParseIP("192.0.2.1"), deepEq},
		{"net.IP 4-byte", reflect.TypeFor[net.IP](), net.IP{192, 0, 2, 1}, deepEq},
		{"net.IPMask", reflect.TypeFor[net.IPMask](), net.CIDRMask(8, 32), deepEq},
		{"net.HardwareAddr", reflect.TypeFor[net.HardwareAddr](), net.HardwareAddr{1, 2, 3, 4, 5, 6}, deepEq},
		{"normalisingText", reflect.TypeFor[normalisingText](), normalisingText("info"), deepEq},
		{"structWithText", reflect.TypeFor[structWithText](), structWithText{1, 2}, deepEq},
		{"UUID [16]byte", reflect.TypeFor[UUID](), UUID{1, 2, 3}, deepEq},
		{"uuid.UUID", reflect.TypeFor[uuid.UUID](), uuid.MustParse("0e37df36-f698-11e6-8dd4-cb9ced3df976"), deepEq},
		{"slog.Level", reflect.TypeFor[slog.Level](), slog.LevelWarn, deepEq},
		{"decimal.Decimal", reflect.TypeFor[decimal.Decimal](), decimal.RequireFromString("1.25"), func(a, b any) bool {
			return a.(decimal.Decimal).Equal(b.(decimal.Decimal))
		}},
		{"regexp.Regexp", reflect.TypeFor[regexp.Regexp](), *regexp.MustCompile(`^a.*z$`), func(a, b any) bool {
			x, y := a.(regexp.Regexp), b.(regexp.Regexp)
			return x.String() == y.String()
		}},
		{"language.Tag", reflect.TypeFor[language.Tag](), language.MustParse("en-GB"), deepEq},
		{"url.URL", reflect.TypeFor[url.URL](), mustU("https://h/a?b=1"), deepEq},
		{"type Port int", reflect.TypeFor[Port](), Port(8080), deepEq},
		{"net.IPNet", reflect.TypeFor[net.IPNet](), mustIPNet("10.0.0.0/8"), deepEq},
	}
}

// p4run compiles, dumps and loads one value in a one-field holder, returning
// the plane artefact and whether it came back.
func p4run(r p4row) (string, string) {
	h := reflect.New(reflect.StructOf([]reflect.StructField{{Name: "V", Type: r.t}})).Elem()
	h.Field(0).Set(reflect.ValueOf(r.v))
	if _, err := compile(h.Type()); err != nil {
		return "REFUSED", shorten2(firstLine(err.Error()), 40)
	}
	d, err := dump(h)
	if err != nil {
		return "dump err", shorten2(err.Error(), 40)
	}
	var parts []string
	for _, p := range sortedAddrs(d) {
		parts = append(parts, strings.TrimPrefix(p.String(), "/V")+"="+d[p].GoString())
	}
	plane := strings.Join(parts, " ")
	if plane == "" {
		plane = "(no address)"
	}
	back := reflect.New(h.Type()).Elem()
	if err := load(d, back); err != nil {
		return plane, "LOAD ERR " + shorten2(err.Error(), 30)
	}
	got := back.Field(0).Interface()
	if r.eq(r.v, got) {
		return plane, "round-trips"
	}
	return plane, fmt.Sprintf("BROKEN: %v -> %v", r.v, got)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func runBeforeKind() {
	modes := []struct {
		label  string
		order  []string
		before bool
	}{
		{"kind only (ADR-0005 today)", nil, false},
		{"text AFTER kind: rescue only what kind refuses", []string{"text"}, false},
		{"text BEFORE kind", []string{"text"}, true},
	}

	for _, m := range modes {
		chainOrder, chainBeforeKind = m.order, m.before
		fmt.Printf("\n--- %s ---\n", m.label)
		fmt.Printf("    %-18s %-40s %s\n", "type", "what lands on the plane", "value fidelity")
		fmt.Println("    " + dashes(100))
		for _, r := range p4rows() {
			plane, verdict := p4run(r)
			fmt.Printf("    %-18s %-40s %s\n", r.name, shorten2(plane, 40), verdict)
		}
	}
	chainOrder, chainBeforeKind = nil, false

	fmt.Println("\n--- P4b: net.IP under its own equality relation ---")
	a := net.ParseIP("192.0.2.1")
	b := net.IP{192, 0, 2, 1}
	fmt.Printf("    net.ParseIP(\"192.0.2.1\") len=%d bytes=%v\n", len(a), a)
	fmt.Printf("    net.IP{192,0,2,1}        len=%d bytes=%v\n", len(b), b)
	fmt.Printf("    bytes.Equal: %v    net.IP.Equal: %v\n", bytes.Equal(a, b), a.Equal(b))
	var rt net.IP
	txt, _ := a.MarshalText()
	_ = rt.UnmarshalText(txt)
	fmt.Printf("    16-byte -> MarshalText %q -> UnmarshalText len=%d  bytes.Equal=%v  .Equal=%v\n",
		txt, len(rt), bytes.Equal(a, rt), a.Equal(rt))
	fmt.Println("    ^ the text arm loses which of the two encodings you had. The type's")
	fmt.Println("      own relation says the two are the same address, so this is a loss")
	fmt.Println("      under == and not under net.IP.Equal. ADR-0005's per-type relation")
	fmt.Println("      is exactly where that carve-out has to be declared.")

	fmt.Println("\n--- P4c: what before-kind does to a struct that has BOTH ---")
	for _, before := range []bool{false, true} {
		chainOrder, chainBeforeKind = []string{"text"}, before
		addrs, err := compile(reflect.TypeFor[struct{ V structWithText }]())
		fmt.Printf("    beforeKind=%-6v addresses: %v err=%v\n", before, addrs, err)
	}
	chainOrder, chainBeforeKind = nil, false
	fmt.Println("    ^ the address set of one type depends on the chain order, so this")
	fmt.Println("      is not only a representation question: it changes what template")
	fmt.Println("      generation emits and what a driver's key function is checked over.")
}
