package main

// Auditing the ADR against the ticket's literal asks, hunting for what the
// fixtures never covered. Every proof so far fed dump's own output back into
// load, so the kinds always matched. No probe ever loaded from a plane that
// reports different kinds than ferry writes.

import (
	"fmt"
	"reflect"
	"time"
)

type Port int
type Hostname string
type Enabled bool

func runGaps() {
	defer runArrays()
	fmt.Println("\n--- G1  the *[]T escape hatch the ADR recommends: does it work? ---")
	type H struct{ P *[]string }
	empty := []string{}
	one := []string{"a"}
	for _, c := range []struct {
		label string
		v     *[]string
	}{
		{"nil pointer      ", nil},
		{"ptr to empty     ", &empty},
		{"ptr to populated ", &one},
	} {
		d, err := dump(reflect.ValueOf(H{c.v}))
		var back H
		lerr := load(d, reflect.ValueOf(&back).Elem())
		desc := "nil ptr"
		if back.P != nil {
			desc = fmt.Sprintf("ptr -> %#v", *back.P)
		}
		fmt.Printf("  %s -> addrs=%v  back=%-24s err=%v/%v\n", c.label, addrList(d), desc, err, lerr)
	}
	fmt.Println("  ^^ if the middle row is indistinguishable from the first, the ADR's")
	fmt.Println("     stated escape hatch does not exist.")

	fmt.Println("\n--- G2  a FLAT plane reports String for everything (env, query, kv) ---")
	fmt.Println("     ADR-0004: two of four first-party drivers have no type information.")
	type Conf2 struct {
		Port    int
		Ratio   float64
		On      bool
		Timeout time.Duration
		Name    string
	}
	flat := map[Path]Value{
		Path{}.Name("Port"):    String("8080"),
		Path{}.Name("Ratio"):   String("3.5"),
		Path{}.Name("On"):      String("true"),
		Path{}.Name("Timeout"): String("30s"),
		Path{}.Name("Name"):    String("svc"),
	}
	var c2 Conf2
	err := load(flat, reflect.ValueOf(&c2).Elem())
	fmt.Printf("  load from an all-String plane: err=%v\n  got: %+v\n", err, c2)

	fmt.Println("\n--- G3  arrays: what if the plane's element count differs from N? ---")
	type A struct{ V [3]string }
	full, _ := dump(reflect.ValueOf(A{[3]string{"a", "b", "c"}}))
	short := map[Path]Value{Path{}.Name("V").Index(0): String("a")}
	over := map[Path]Value{}
	for k, v := range full {
		over[k] = v
	}
	over[Path{}.Name("V").Index(7)] = String("h")
	for _, c := range []struct {
		label string
		in    map[Path]Value
	}{{"exact  ", full}, {"short  ", short}, {"overrun", over}} {
		var a A
		e := load(c.in, reflect.ValueOf(&a).Elem())
		fmt.Printf("  %s -> %#v  err=%v\n", c.label, a.V, e)
	}

	fmt.Println("\n--- G4  named types over an admitted kind ---")
	type N struct {
		P Port
		H Hostname
		E Enabled
	}
	a4, e4 := compile(reflect.TypeFor[N]())
	d4, _ := dump(reflect.ValueOf(N{8080, "h", true}))
	var b4 N
	l4 := load(d4, reflect.ValueOf(&b4).Elem())
	fmt.Printf("  compile=%v err=%v\n  dump=%v\n  back=%+v err=%v\n", a4, e4, valList(d4), b4, l4)

	fmt.Println("\n--- G5  is a bad leaf loud? ---")
	type I struct{ N int8 }
	for _, c := range []struct {
		label string
		v     Value
	}{
		{"not a number ", Number("abc")},
		{"overflows int8", Number("99999")},
		{"wrong kind   ", Bool(true)},
		{"empty text   ", Number("")},
	} {
		var i I
		e := load(map[Path]Value{Path{}.Name("N"): c.v}, reflect.ValueOf(&i).Elem())
		fmt.Printf("  %-14s %-16s -> %v  err=%v\n", c.label, c.v.GoString(), i.N, e)
	}

	fmt.Println("\n--- G6  embedded structs ---")
	type Base struct{ ID string }
	type Emb struct {
		Base
		Name string
	}
	a6, e6 := compile(reflect.TypeFor[Emb]())
	fmt.Printf("  compile=%v err=%v\n", a6, e6)
}

func addrList(m map[Path]Value) []string {
	var out []string
	for _, p := range sortedAddrs(m) {
		out = append(out, p.String()+"="+m[p].GoString())
	}
	return out
}
func valList(m map[Path]Value) []string { return addrList(m) }

func runArrays() {
	fmt.Println("\n--- G7  an array's addresses are STATIC, a slice's are not ---")
	type A struct {
		Arr [3]string
		Sl  []string
		M   map[string]int
	}
	a, e := compile(reflect.TypeFor[A]())
	fmt.Printf("  compile err=%v\n", e)
	for _, p := range sortedPaths(a) {
		fmt.Printf("    %s\n", p)
	}
	fmt.Println("  -> the three /Arr#N addresses are known from the type with no value,")
	fmt.Println("     so an array is loadable from a source that cannot enumerate.")

	fmt.Println("\n  and an absent array element leaves the zero value, like a struct field:")
	var av struct{ V [3]string }
	le := load(map[Path]Value{Path{}.Name("V").Index(0): String("a")}, reflect.ValueOf(&av).Elem())
	fmt.Printf("    %#v err=%v\n", av.V, le)
	var av2 struct{ V [3]string }
	le2 := load(map[Path]Value{Path{}.Name("V").Index(7): String("h")}, reflect.ValueOf(&av2).Elem())
	fmt.Printf("    index 7 into [3]string -> err=%v\n", le2)
}
