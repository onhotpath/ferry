package main

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func dhdr(s string) { fmt.Printf("\n=== %s ===\n", s) }

func mustSchema(t reflect.Type) *schema {
	s, err := compileD(t)
	if err != nil {
		panic(err)
	}
	return s
}

func addr(names ...string) Path {
	p := Path{}
	for _, n := range names {
		p = p.Name(n)
	}
	return p
}

// ---------------------------------------------------------------------------
// D1  Absent does not write, so a seeded struct IS a defaults mechanism.
// ---------------------------------------------------------------------------

type D1Conf struct {
	Name string
	Port int
	Tags []string
}

func d1() {
	dhdr("D1  what Absent does to a field that is NOT at its zero value")
	s := mustSchema(reflect.TypeFor[D1Conf]())
	seed := func() D1Conf { return D1Conf{Name: "svc", Port: 8080, Tags: []string{"a"}} }

	for _, c := range []struct {
		label string
		plane map[Path]Value
	}{
		{"empty plane (everything Absent)", map[Path]Value{}},
		{"plane has Port only", map[Path]Value{addr("Port"): Number("9090")}},
		{"plane has Port=0 explicitly", map[Path]Value{addr("Port"): Number("0")}},
	} {
		v := seed()
		_, err := loadD(c.plane, s, reflect.ValueOf(&v).Elem(), loadOpts{})
		fmt.Printf("  %-32s -> %#v err=%v\n", c.label, v, err)
	}
}

// ---------------------------------------------------------------------------
// D2  ADR-0005 said "a container address with no children yields the zero
//     value". Every fixture it said that from loaded into a FRESH ZERO
//     destination, where "yields zero" and "leaves unchanged" are the same
//     observation. They are not the same rule.
// ---------------------------------------------------------------------------

type D2Conf struct {
	Tags   []string
	Limits map[string]int
}

func d2() {
	dhdr("D2  the fixture that could not tell two rules apart")
	s := mustSchema(reflect.TypeFor[D2Conf]())
	planes := []struct {
		label string
		vals  map[Path]Value
	}{
		{"no key at all (Absent)", map[Path]Value{}},
		{"explicit Null at the container", map[Path]Value{addr("Tags"): Null(), addr("Limits"): Null()}},
	}
	for _, dst := range []struct {
		label string
		make  func() D2Conf
	}{
		{"ZERO destination", func() D2Conf { return D2Conf{} }},
		{"SEEDED destination", func() D2Conf { return D2Conf{Tags: []string{"a"}, Limits: map[string]int{"rps": 1}} }},
	} {
		for _, pl := range planes {
			for _, rule := range []struct {
				label string
				o     loadOpts
			}{
				{"absent-does-not-write", loadOpts{}},
				{"absent-yields-zero    ", loadOpts{absentZeroesComposite: true}},
			} {
				v := dst.make()
				_, _ = loadD(pl.vals, s, reflect.ValueOf(&v).Elem(), rule.o)
				fmt.Printf("  %-19s %-30s %s -> Tags=%v(nil=%v) Limits=%v\n",
					dst.label, pl.label, rule.label, v.Tags, v.Tags == nil, v.Limits)
			}
		}
	}
	fmt.Println("  NOTE: the two rules are indistinguishable in the top block and disagree in the bottom one.")
}

// ---------------------------------------------------------------------------
// D3  What Absent and Null mean to a Go field, per kind. The table the ticket
//     asks for by name, and the three candidate readings of Null.
// ---------------------------------------------------------------------------

type D3Inner struct{ User string }

type D3Conf struct {
	S   string
	I   int
	B   bool
	D   time.Duration
	By  []byte
	P   *int
	PS  *D3Inner
	Sl  []string
	M   map[string]int
	Arr [2]string
}

func d3() {
	dhdr("D3  Absent and Null, per kind, into a SEEDED destination")
	s := mustSchema(reflect.TypeFor[D3Conf]())
	five := 5
	seed := func() D3Conf {
		return D3Conf{
			S: "seed", I: 7, B: true, D: time.Second, By: []byte("xy"),
			P: &five, PS: &D3Inner{User: "u"},
			Sl: []string{"a"}, M: map[string]int{"k": 1}, Arr: [2]string{"p", "q"},
		}
	}
	show := func(label string, v D3Conf, err error) {
		p := "nil"
		if v.P != nil {
			p = fmt.Sprint(*v.P)
		}
		ps := "nil"
		if v.PS != nil {
			ps = fmt.Sprintf("&%v", *v.PS)
		}
		fmt.Printf("  %-26s S=%-6q I=%-3d B=%-6v D=%-4v By=%-4q P=%-4s PS=%-8s Sl=%-5v M=%-10v Arr=%v\n",
			label, v.S, v.I, v.B, v.D, string(v.By), p, ps, v.Sl, v.M, v.Arr)
		if err != nil {
			fmt.Printf("     err: %v\n", err)
		}
	}

	show("seed", seed(), nil)

	v := seed()
	_, err := loadD(map[Path]Value{}, s, reflect.ValueOf(&v).Elem(), loadOpts{})
	show("ABSENT everywhere", v, err)

	// One address at a time, so a refusal at one leaf does not mask the rest.
	fmt.Println("\n  NULL, one address at a time, under the three candidate readings:")
	each := []struct {
		name string
		get  func(D3Conf) string
	}{
		{"S", func(c D3Conf) string { return fmt.Sprintf("%q", c.S) }},
		{"I", func(c D3Conf) string { return fmt.Sprint(c.I) }},
		{"B", func(c D3Conf) string { return fmt.Sprint(c.B) }},
		{"D", func(c D3Conf) string { return fmt.Sprint(c.D) }},
		{"By", func(c D3Conf) string { return fmt.Sprintf("%q", string(c.By)) }},
		{"P", func(c D3Conf) string { return iptr(c.P) }},
		{"PS", func(c D3Conf) string {
			if c.PS == nil {
				return "nil"
			}
			return "&" + c.PS.User
		}},
		{"Sl", func(c D3Conf) string { return fmt.Sprintf("%v nil=%v", c.Sl, c.Sl == nil) }},
		{"M", func(c D3Conf) string { return fmt.Sprintf("%v nil=%v", c.M, c.M == nil) }},
	}
	rules := []struct {
		label string
		o     loadOpts
	}{
		{"admitted-by-the-type-set", loadOpts{}},
		{"null-means-zero", loadOpts{nullMeansZero: true}},
		{"null-means-absent", loadOpts{nullMeansAbsent: true}},
	}
	fmt.Printf("  %-4s %-46s %-24s %s\n", "addr", rules[0].label, rules[1].label, rules[2].label)
	for _, e := range each {
		row := fmt.Sprintf("  %-4s ", "/"+e.name)
		for _, r := range rules {
			c := seed()
			_, err := loadD(map[Path]Value{addr(e.name): Null()}, s, reflect.ValueOf(&c).Elem(), r.o)
			out := e.get(c)
			if err != nil {
				out = "REFUSED"
			}
			w := 46
			if r.label != rules[0].label {
				w = 24
			}
			row += fmt.Sprintf("%-*s ", w, out)
		}
		fmt.Println(row)
	}
	c := seed()
	_, e := loadD(map[Path]Value{addr("I"): Null()}, s, reflect.ValueOf(&c).Elem(), loadOpts{})
	fmt.Printf("\n  the refusal in full: %v\n", e)
}

// ---------------------------------------------------------------------------
// D4  The plane holds an explicit empty value and the struct holds a non-zero
//     default. This is the ticket's fourth named ask.
// ---------------------------------------------------------------------------

type D4Conf struct {
	Name string `ferry:"name,default=anonymous"`
	Port int    `ferry:"port,default=8080"`
}

func d4() {
	dhdr("D4  explicit-empty on the plane against a non-zero default")
	s := mustSchema(reflect.TypeFor[D4Conf]())
	for _, c := range []struct {
		label string
		plane map[Path]Value
	}{
		{"Absent", map[Path]Value{}},
		{`String("") (FOO=)`, map[Path]Value{addr("name"): String(""), addr("port"): String("")}},
		{"Null", map[Path]Value{addr("name"): Null(), addr("port"): Null()}},
		{"a real value", map[Path]Value{addr("name"): String("svc"), addr("port"): String("9090")}},
	} {
		var v D4Conf
		_, err := loadD(c.plane, s, reflect.ValueOf(&v).Elem(), loadOpts{})
		fmt.Printf("  %-18s -> Name=%-11q Port=%-5d err=%v\n", c.label, v.Name, v.Port, err)
	}
	fmt.Println("  xload cannot reach the second row at all: OSLoader collapses a missing")
	fmt.Println("  variable to \"\" (5.1), so FOO= and no FOO are one observation and a")
	fmt.Println("  defaults layer built on it can never be overridden back to empty (5.12).")
}

// ---------------------------------------------------------------------------
// D5  Does present-and-empty satisfy required?
// ---------------------------------------------------------------------------

type D5Conf struct {
	Token string  `ferry:"token,required"`
	Name  *string `ferry:"name,required"`
}

func d5() {
	dhdr("D5  what satisfies required")
	s := mustSchema(reflect.TypeFor[D5Conf]())
	for _, c := range []struct {
		label string
		plane map[Path]Value
	}{
		{"Absent", map[Path]Value{}},
		{`String("") (FOO=)`, map[Path]Value{addr("token"): String(""), addr("name"): String("")}},
		{"Null", map[Path]Value{addr("token"): Null(), addr("name"): Null()}},
		{"a value", map[Path]Value{addr("token"): String("t"), addr("name"): String("n")}},
	} {
		var v D5Conf
		_, err := loadD(c.plane, s, reflect.ValueOf(&v).Elem(), loadOpts{})
		ok := "SATISFIED"
		if err != nil {
			ok = "refused  "
		}
		n := "nil"
		if v.Name != nil {
			n = fmt.Sprintf("&%q", *v.Name)
		}
		fmt.Printf("  %-18s -> %s Token=%-4q Name=%-5s %v\n", c.label, ok, v.Token, n, errOrBlank(err))
	}
	fmt.Println("  xload: required is `val == \"\" && meta.required` (load.go:147), so FOO=")
	fmt.Println("  CANNOT satisfy it. That is 5.1's consequence, not a separate defect.")

	fmt.Println("\n  and the contradictions schema compile refuses, with no value in hand:")
	type bad1 struct {
		A string `ferry:"a,required,default=x"`
	}
	type bad2 struct {
		B int `ferry:"b,omitzero,default=8080"`
	}
	type bad3 struct {
		C int `ferry:"c,omitzero,default=0"`
	}
	for _, t := range []reflect.Type{reflect.TypeFor[bad1](), reflect.TypeFor[bad2](), reflect.TypeFor[bad3]()} {
		f := t.Field(0)
		_, err := compileD(t)
		fmt.Printf("  %-28s %v\n", f.Tag.Get("ferry"), errOrOK(err))
	}
	fmt.Println("  The third compiles because a default EQUAL to the zero value is not a")
	fmt.Println("  contradiction: omitting it and reapplying it land on the same value.")
}

func errOrBlank(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func errOrOK(err error) string {
	if err == nil {
		return "compiles"
	}
	return err.Error()
}

// ---------------------------------------------------------------------------
// D6  5.7: reflect.DeepEqual as a "was anything set?" probe.
// ---------------------------------------------------------------------------

type D6Inner struct {
	A string
	N int
}

type D6Conf struct {
	Opt  *D6Inner
	Name string
}

var d6inner = mustSchema(reflect.TypeFor[D6Inner]())

// xloadStyle is 5.7 reproduced in ferry's shape: load into a fresh value, then
// reflect.DeepEqual it against a fresh ZERO value to decide whether anything
// was set. load.go:107-117 and async.go:163-171.
func xloadStyle(vals map[Path]Value, dst *D6Conf) {
	sub := map[Path]Value{}
	for p, v := range vals {
		segs := p.Segments()
		if len(segs) == 2 && segs[0].Text == "Opt" {
			sub[Path{}.Name(segs[1].Text)] = v
		}
	}
	probe := reflect.New(reflect.TypeFor[D6Inner]())
	_, _ = loadD(sub, d6inner, probe.Elem(), loadOpts{})
	fresh := reflect.New(reflect.TypeFor[D6Inner]()).Interface()
	if !reflect.DeepEqual(probe.Interface(), fresh) {
		dst.Opt = probe.Interface().(*D6Inner)
	}
	if v, ok := vals[addr("Name")]; ok {
		_ = decLeaf(v, reflect.ValueOf(&dst.Name).Elem())
	}
}

func d6() {
	dhdr("D6  5.7: telling an untouched subtree from one legitimately loaded to zero")
	s := mustSchema(reflect.TypeFor[D6Conf]())

	cases := []struct {
		label string
		plane map[Path]Value
	}{
		{"nothing under /Opt at all", map[Path]Value{addr("Name"): String("svc")}},
		{"/Opt/A and /Opt/N present, BOTH ZERO", map[Path]Value{
			addr("Name"): String("svc"), addr("Opt", "A"): String(""), addr("Opt", "N"): Number("0")}},
		{"/Opt/A present and non-zero", map[Path]Value{
			addr("Name"): String("svc"), addr("Opt", "A"): String("x")}},
		{"explicit Null at /Opt", map[Path]Value{addr("Name"): String("svc"), addr("Opt"): Null()}},
	}
	for _, c := range cases {
		var a D6Conf
		xloadStyle(c.plane, &a)
		var b D6Conf
		_, _ = loadD(c.plane, s, reflect.ValueOf(&b).Elem(), loadOpts{})
		fmt.Printf("  %-38s DeepEqual-probe: %-16s presence-bit: %s\n", c.label, ptrStr(a.Opt), ptrStr(b.Opt))
	}
	fmt.Println("  Row 2 is the defect 5.7 names: a subtree the plane really did set to all")
	fmt.Println("  zeros is indistinguishable from one nothing touched. The repair needs")
	fmt.Println("  ADR-0004's presence to exist at all, which is why 5.1 and 5.7 are one fix.")

	d6cost()
}

func ptrStr(p *D6Inner) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("&%+v", *p)
}

// d6cost measures the two "was anything set?" implementations against each
// other on ONE walk, rather than comparing two differently-shaped walks. The
// survey calls 5.7 expensive; this is what that is worth at this size.
func d6cost() {
	fmt.Println("\n  the same walk, with each probe attached:")
	sub := map[Path]Value{addr("A"): String(""), addr("N"): Number("0")}
	fresh := reflect.New(reflect.TypeFor[D6Inner]()).Interface()

	r1 := testing.Benchmark(func(bm *testing.B) {
		for bm.Loop() {
			probe := reflect.New(reflect.TypeFor[D6Inner]())
			_, _ = loadD(sub, d6inner, probe.Elem(), loadOpts{})
			// 5.7: a fresh allocation plus a recursive deep comparison, per
			// nil-struct-pointer field per load.
			z := reflect.New(reflect.TypeFor[D6Inner]()).Interface()
			if reflect.DeepEqual(probe.Interface(), z) {
				_ = probe
			}
		}
	})
	r2 := testing.Benchmark(func(bm *testing.B) {
		for bm.Loop() {
			probe := reflect.New(reflect.TypeFor[D6Inner]())
			got, _ := loadD(sub, d6inner, probe.Elem(), loadOpts{})
			if !got {
				_ = probe
			}
		}
	})
	_ = fresh
	fmt.Printf("    walk + DeepEqual against a fresh zero: %s\n", r1)
	fmt.Printf("    walk + the presence bit it returns:    %s\n", r2)
}
