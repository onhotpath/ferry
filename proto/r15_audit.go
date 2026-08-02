package main

// R15: the audit.
//
// Every prior session's worst defect was a case the fixtures did not contain,
// and the hand-off named the likely trap for this one: "a fixture where
// registration happens before any schema is compiled, so the ordering
// question never bites". R6 is that probe and it found the staleness. This
// one goes after the rest of the uncovered surface:
//
//   - a registered type in EVERY composite position, not just at a leaf in a
//     one-field struct (which is #12's own recorded worst fixture defect)
//   - the zero value in every one of those positions
//   - all three planes ADR-0005 requires, including the flattening one
//   - the seam with ADR-0006: a declared default reaching a registered codec
//   - a registered type behind a nil pointer
//   - a registered INTERFACE emitting Null, through a plane with no null

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"reflect"
	"strconv"
	"strings"
)

type R15Addr = netip.Addr

// r15Conf puts one registered type in every position the walk has.
type r15Conf struct {
	Leaf   R15Addr
	Ptr    *R15Addr
	Slice  []R15Addr
	Array  [2]R15Addr
	MapVal map[string]R15Addr
	MapKey map[R15Addr]int
	Nested r15Inner
	Iface  net.Addr
}

type r15Inner struct{ Deep R15Addr }

func r15Registry() *Registry {
	r := NewRegistry()
	// TextCodec, not StringCodec: R14 is why.
	if err := r.Register(
		TextCodec[netip.Addr](VString).AsMapKey(),
		TypeCodec(VString,
			func(a net.Addr) (Value, error) {
				if a == nil {
					return Null(), nil
				}
				return String(a.Network() + "://" + a.String()), nil
			},
			func(v Value) (net.Addr, error) {
				if v.Kind() == VNull || (v.Kind() == VString && v.Text() == "") {
					return nil, nil
				}
				s, err := v.AsString()
				if err != nil {
					return nil, err
				}
				return net.ResolveTCPAddr("tcp", s[len("tcp://"):])
			}),
	); err != nil {
		panic(err)
	}
	return r
}

func runR15() {
	keyOptIn = true
	defer func() { keyOptIn = false }()
	reg := r15Registry()

	a1 := netip.MustParseAddr("192.0.2.1")
	a2 := netip.MustParseAddr("2001:db8::1")

	populated := r15Conf{
		Leaf:   a1,
		Ptr:    &a2,
		Slice:  []R15Addr{a1, a2},
		Array:  [2]R15Addr{a1, a2},
		MapVal: map[string]R15Addr{"a": a1},
		MapKey: map[R15Addr]int{a1: 1, a2: 2},
		Nested: r15Inner{a2},
		Iface:  &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 80},
	}
	// Every field at its zero value, which is the case #12's fixtures missed
	// and which R14 showed is where a codec actually breaks.
	var zeroed r15Conf

	dir, _ := os.MkdirTemp("", "ferryr15")
	defer os.RemoveAll(dir)

	withRegistry(reg, func() {
		fmt.Println("--- R15a: the static address set, with a registered type everywhere ---")
		addrs, err := compile(reflect.TypeFor[r15Conf]())
		fmt.Printf("    compile err=%v\n", err)
		for _, p := range addrs {
			fmt.Printf("      %s\n", p)
		}

		for _, tc := range []struct {
			label string
			v     r15Conf
		}{{"populated", populated}, {"every field ZERO", zeroed}} {
			fmt.Printf("\n--- R15b: %s, through all three planes ---\n", tc.label)
			d, derr := dump(reflect.ValueOf(tc.v))
			fmt.Printf("    dump err=%v, %d addresses\n", derr, len(d))
			for _, p := range sortedAddrs(d) {
				fmt.Printf("      %-18s %s\n", p, d[p].GoString())
			}
			for _, plane := range []struct {
				name string
				fn   func(map[Path]Value) (map[Path]Value, error)
			}{
				{"memory", identityPlane},
				{"yaml, real files", yamlPlane(dir)},
				{"flattening (String for everything)", flatten},
			} {
				crossed, perr := plane.fn(d)
				if perr != nil {
					fmt.Printf("    %-36s plane err=%v\n", plane.name, perr)
					continue
				}
				var back r15Conf
				lerr := load(crossed, reflect.ValueOf(&back).Elem())
				fmt.Printf("    %-36s load err=%v  equal=%v %s\n",
					plane.name, lerr, r15Equal(tc.v, back), r15Diff(tc.v, back))
			}
		}

		fmt.Println("\n--- R15c: the seam with ADR-0006. A declared default reaches the codec ---")
		fmt.Println("    ADR-0006: a default is compiled into a String Value at the field's")
		fmt.Println("    address, so a registered codec gets defaults with no default")
		fmt.Println("    awareness. #12's P15 measured that for a CHAIN codec. This is a")
		fmt.Println("    REGISTERED one, which is the case the sentence was written for.")
		type withDefault struct{ A R15Addr }
		defaults := map[Path]Value{Path{}.Name("A"): String("10.0.0.1")}
		for _, tc := range []struct {
			label string
			plane map[Path]Value
		}{
			{"plane empty, default applies", map[Path]Value{}},
			{"plane supplies the address", map[Path]Value{Path{}.Name("A"): String("192.0.2.1")}},
		} {
			merged := map[Path]Value{}
			for p, v := range defaults {
				merged[p] = v
			}
			for p, v := range tc.plane {
				merged[p] = v
			}
			var out withDefault
			err := load(merged, reflect.ValueOf(&out).Elem())
			fmt.Printf("    %-30s -> %v err=%v\n", tc.label, out.A, err)
		}

		fmt.Println("\n--- R15d: a registered INTERFACE emitting Null, on a plane with no null ---")
		type ifaceOnly struct{ N net.Addr }
		d, _ := dump(reflect.ValueOf(ifaceOnly{nil}))
		fmt.Printf("    dump nil net.Addr           -> %s\n", d[Path{}.Name("N")].GoString())
		flat, _ := flatten(d)
		fmt.Printf("    through the flattening plane -> %s\n", flat[Path{}.Name("N")].GoString())
		var back ifaceOnly
		lerr := load(flat, reflect.ValueOf(&back).Elem())
		fmt.Printf("    loads back                   -> %v err=%v\n", back.N, lerr)
		fmt.Println("    ^ ADR-0005 records exactly this for core: a nil pointer cannot")
		fmt.Println("      round-trip through a plane with no null, and it is DRIVER")
		fmt.Println("      fidelity rather than value fidelity. A registered codec inherits")
		fmt.Println("      the same limit, and it inherits the same escape: this codec")
		fmt.Println("      accepts String(\"\") as well as Null and therefore survives, which")
		fmt.Println("      is a choice its registrant made and core did not make for them.")
		fmt.Println("      That is R2's `accepted kinds are not implied by the declared")
		fmt.Println("      kind`, doing real work rather than being a principle.")

		fmt.Println("\n--- R15e: what this audit did NOT clear ---")
		fmt.Println("    - a registered type at the ROOT mints the empty path, which")
		fmt.Println("      ADR-0003 says an address may not be. Pre-existing, named in")
		fmt.Println("      ADR-0007 as #16's, and registration enlarges the set of types")
		fmt.Println("      that can sit there. Re-measured:")
		rootAddrs, rootErr := compile(reflect.TypeFor[R15Addr]())
		fmt.Printf("        compile(netip.Addr) as the root -> %v err=%v\n", rootAddrs, rootErr)
		fmt.Println("    - #31's map[time.Time]string, untouched on purpose (R11e).")
		fmt.Println("    - concurrency beyond R13's data race, which is #20's.")
	})
}

func r15Equal(a, b r15Conf) bool {
	if a.Leaf != b.Leaf || a.Nested != b.Nested || a.Array != b.Array {
		return false
	}
	if (a.Ptr == nil) != (b.Ptr == nil) || (a.Ptr != nil && *a.Ptr != *b.Ptr) {
		return false
	}
	if len(a.Slice) != len(b.Slice) {
		return false
	}
	for i := range a.Slice {
		if a.Slice[i] != b.Slice[i] {
			return false
		}
	}
	if len(a.MapVal) != len(b.MapVal) || len(a.MapKey) != len(b.MapKey) {
		return false
	}
	for k, v := range a.MapVal {
		if b.MapVal[k] != v {
			return false
		}
	}
	for k, v := range a.MapKey {
		if b.MapKey[k] != v {
			return false
		}
	}
	return fmt.Sprint(a.Iface) == fmt.Sprint(b.Iface)
}

// r15Diff names the fields that differ, because "equal=false" without a
// field name is the kind of result that gets waved through.
func r15Diff(a, b r15Conf) string {
	var d []string
	if a.Leaf != b.Leaf {
		d = append(d, "Leaf")
	}
	if a.Nested != b.Nested {
		d = append(d, "Nested")
	}
	if a.Array != b.Array {
		d = append(d, "Array")
	}
	if (a.Ptr == nil) != (b.Ptr == nil) || (a.Ptr != nil && b.Ptr != nil && *a.Ptr != *b.Ptr) {
		d = append(d, fmt.Sprintf("Ptr(%v->%v)", a.Ptr, b.Ptr))
	}
	if len(a.Slice) != len(b.Slice) {
		d = append(d, fmt.Sprintf("Slice(%d->%d)", len(a.Slice), len(b.Slice)))
	}
	if len(a.MapVal) != len(b.MapVal) {
		d = append(d, fmt.Sprintf("MapVal(%d->%d)", len(a.MapVal), len(b.MapVal)))
	}
	if len(a.MapKey) != len(b.MapKey) {
		d = append(d, fmt.Sprintf("MapKey(%d->%d)", len(a.MapKey), len(b.MapKey)))
	}
	if fmt.Sprint(a.Iface) != fmt.Sprint(b.Iface) {
		d = append(d, fmt.Sprintf("Iface(%v->%v)", a.Iface, b.Iface))
	}
	if len(d) == 0 {
		return ""
	}
	return "  differs: " + strings.Join(d, " ")
}

var _ = strconv.Itoa
