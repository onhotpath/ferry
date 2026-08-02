package main

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// D7  Does a field still holding its default get dumped, or omitted?
//     The two candidate omission rules, and what each costs on the round trip.
// ---------------------------------------------------------------------------

type D7Conf struct {
	Port  int    `ferry:"port,default=8080"`
	Name  string `ferry:"name"`
	Debug bool   `ferry:"debug,omitzero"`
}

func d7() {
	dhdr("D7  dumping a field that is at its default, and the two omission rules")
	s := mustSchema(reflect.TypeFor[D7Conf]())

	for _, c := range []struct {
		label string
		v     D7Conf
	}{
		{"port explicitly 8080 (== the default)", D7Conf{Port: 8080, Name: "svc"}},
		{"port explicitly 0    (!= the default)", D7Conf{Port: 0, Name: "svc"}},
		{"port 9090", D7Conf{Port: 9090, Name: "svc", Debug: true}},
	} {
		calls, _ := dumpD(reflect.ValueOf(c.v), s)
		var back D7Conf
		_, _ = loadD(callsToMap(calls), s, reflect.ValueOf(&back).Elem(), loadOpts{})
		fmt.Printf("  %-38s Set calls=%d %v -> back=%+v  round-trip=%v\n",
			c.label, len(calls), callStr(calls), back, back == c.v)
	}

	fmt.Println("\n  now the rule ferry does NOT take: omit a field that equals its default.")
	omitIfDefault := func(v D7Conf) []setCall {
		calls, _ := dumpD(reflect.ValueOf(v), s)
		var out []setCall
		for _, c := range calls {
			if o := s.at(c.p); o.hasDef && o.def.Text() == c.v.Text() {
				continue
			}
			out = append(out, c)
		}
		return out
	}
	for _, c := range []D7Conf{{Port: 8080, Name: "svc"}, {Port: 9090, Name: "svc"}} {
		calls := omitIfDefault(c)
		var back D7Conf
		_, _ = loadD(callsToMap(calls), s, reflect.ValueOf(&back).Elem(), loadOpts{})
		// Now the SAME plane, read by code whose default has since changed.
		s2 := mustSchema(reflect.TypeFor[struct {
			Port  int    `ferry:"port,default=9999"`
			Name  string `ferry:"name"`
			Debug bool   `ferry:"debug,omitzero"`
		}]())
		var later D7Conf
		_, _ = loadD(callsToMap(calls), s2, reflect.ValueOf(&later).Elem(), loadOpts{})
		fmt.Printf("  %+v -> %v -> same code: Port=%d ; default later changed to 9999: Port=%d\n",
			c, callStr(calls), back.Port, later.Port)
	}

	fmt.Println("\n  and the contradiction omitzero+default would have hidden, had schema")
	fmt.Println("  compile not refused it (D5's third row). Forced through anyway:")
	type forced struct {
		Port int `ferry:"port"`
	}
	sf := mustSchema(reflect.TypeFor[forced]())
	sf.opts[addr("port")] = fieldOpts{omitzero: true, hasDef: true, defText: "8080", def: ptrVal(String("8080"))}
	calls, _ := dumpD(reflect.ValueOf(forced{Port: 0}), sf)
	var back forced
	_, _ = loadD(callsToMap(calls), sf, reflect.ValueOf(&back).Elem(), loadOpts{})
	fmt.Printf("  explicit Port=0, omitzero+default=8080 -> %d Set calls -> loads back as %d\n",
		len(calls), back.Port)
}

func ptrVal(v Value) *Value { return &v }

func callStr(cs []setCall) string {
	s := "["
	for i, c := range cs {
		if i > 0 {
			s += " "
		}
		s += c.p.String() + "=" + c.v.GoString()
	}
	return s + "]"
}

// ---------------------------------------------------------------------------
// D8  A default is a Value, not a stored Go value. The alternative aliases.
// ---------------------------------------------------------------------------

type D8Conf struct {
	Key []byte `ferry:"key,default=secret"`
}

func d8() {
	dhdr("D8  why a default is a Value and not a cached reflect.Value")
	s := mustSchema(reflect.TypeFor[D8Conf]())

	// (i) ferry's rule: the default is String("secret"), decoded fresh each load.
	var a, b D8Conf
	_, _ = loadD(map[Path]Value{}, s, reflect.ValueOf(&a).Elem(), loadOpts{})
	_, _ = loadD(map[Path]Value{}, s, reflect.ValueOf(&b).Elem(), loadOpts{})
	a.Key[0] = 'S'
	fmt.Printf("  as a Value:          a=%q b=%q  aliased=%v\n", a.Key, b.Key, &a.Key[0] == &b.Key[0])

	// (ii) the alternative: parse once at compile and reflect.Set the result.
	cached := reflect.New(reflect.TypeFor[[]byte]()).Elem()
	_ = decLeaf(String("secret"), cached)
	var c, d D8Conf
	reflect.ValueOf(&c).Elem().Field(0).Set(cached)
	reflect.ValueOf(&d).Elem().Field(0).Set(cached)
	c.Key[0] = 'S'
	fmt.Printf("  as a cached Go value: c=%q d=%q  aliased=%v\n", c.Key, d.Key, &c.Key[0] == &d.Key[0])
	fmt.Println("  Two loads of one schema share one backing array, and mutating either")
	fmt.Println("  corrupts the other. The same holds for any default whose Go type has")
	fmt.Println("  reference semantics, which in core's leaf set is []byte.")

	r := testing.Benchmark(func(bm *testing.B) {
		dst := reflect.New(reflect.TypeFor[int]()).Elem()
		v := String("8080")
		for bm.Loop() {
			_ = decLeaf(v, dst)
		}
	})
	fmt.Printf("  cost of decoding a default per load: %s\n", r)
}

// ---------------------------------------------------------------------------
// D9  Every default declaration is checked from the type alone.
// ---------------------------------------------------------------------------

func d9() {
	dhdr("D9  what schema compile refuses, with no value in hand and no plane")
	type c1 struct {
		P int `ferry:"p,default=abc"`
	}
	type c2 struct {
		P int `ferry:"p,default=0080"`
	}
	type c3 struct {
		T time.Duration `ferry:"t,default=30"`
	}
	type c4 struct {
		T time.Duration `ferry:"t,default=30s"`
	}
	type c5 struct {
		B int8 `ferry:"b,default=99999"`
	}
	type c6 struct {
		S string `ferry:"s,default="`
	}
	type c7 struct {
		Tags []string `ferry:"tags,default=a"`
	}
	type c8 struct {
		M map[string]int `ferry:"m,default=x"`
	}
	type c9 struct {
		In struct{ A string } `ferry:"in,default=x"`
	}
	type c10 struct {
		P *int `ferry:"p,default=5"`
	}
	for _, t := range []reflect.Type{
		reflect.TypeFor[c1](), reflect.TypeFor[c2](), reflect.TypeFor[c3](), reflect.TypeFor[c4](),
		reflect.TypeFor[c5](), reflect.TypeFor[c6](), reflect.TypeFor[c7](), reflect.TypeFor[c8](),
		reflect.TypeFor[c9](), reflect.TypeFor[c10](),
	} {
		f, _ := t.FieldByNameFunc(func(string) bool { return true })
		_, err := compileD(t)
		fmt.Printf("  %-30s %s\n", f.Tag.Get("ferry")+" ("+f.Type.String()+")", errOrOK(err))
	}
	fmt.Println("  A composite default would have to spell a list inside a tag, which is")
	fmt.Println("  5.10 - the string-splitting defect ADR-0003 removed structurally.")
}

// ---------------------------------------------------------------------------
// D10 *T at a LEAF does express unset-versus-zero, unlike *T at a composite.
//     ADR-0005's G1 measured the composite case; this is the other one.
// ---------------------------------------------------------------------------

type D10Conf struct {
	P  *int
	Sl *[]string
}

func d10() {
	dhdr("D10 what a pointer adds, at a leaf and at a composite")
	s := mustSchema(reflect.TypeFor[D10Conf]())
	zero := 0
	five := 5
	empty := []string{}
	one := []string{"a"}
	for _, c := range []struct {
		label string
		v     D10Conf
	}{
		{"P nil,   Sl nil", D10Conf{}},
		{"P=&0,    Sl=&[]", D10Conf{P: &zero, Sl: &empty}},
		{"P=&5,    Sl=&[a]", D10Conf{P: &five, Sl: &one}},
	} {
		calls, _ := dumpD(reflect.ValueOf(c.v), s)
		var back D10Conf
		_, _ = loadD(callsToMap(calls), s, reflect.ValueOf(&back).Elem(), loadOpts{})
		fmt.Printf("  %-18s mints %-34s -> back P=%-5s Sl=%s\n",
			c.label, callStr(calls), iptr(back.P), sptr(back.Sl))
	}
	fmt.Println("  /P has two distinct observations (null, number) so *int expresses it.")
	fmt.Println("  /Sl has one (null) for both nil and empty, which is ADR-0005's G1.")

	fmt.Println("\n  and the same *int through a plane that has no null (env, query, kv):")
	calls, _ := dumpD(reflect.ValueOf(D10Conf{P: nil, Sl: &one}), s)
	flat, _ := flatten(callsToMap(calls))
	var back D10Conf
	_, err := loadD(flat, s, reflect.ValueOf(&back).Elem(), loadOpts{})
	fmt.Printf("  nil *int dumped, flattened, loaded -> P=%s err=%v\n", iptr(back.P), err)
	fmt.Println("  ADR-0005 measured this as 2 of 10 composites failing on the flattening")
	fmt.Println("  plane. The rule that makes it honest rather than silent is the driver")
	fmt.Println("  declaring its carryable kinds and refusing Null loudly.")

	fmt.Println("\n  but on LOAD from such a plane the distinction survives, because absence does:")
	var b2 D10Conf
	_, _ = loadD(map[Path]Value{}, s, reflect.ValueOf(&b2).Elem(), loadOpts{})
	var b3 D10Conf
	_, _ = loadD(map[Path]Value{addr("P"): String("0")}, s, reflect.ValueOf(&b3).Elem(), loadOpts{})
	fmt.Printf("  PORT unset -> P=%s ; PORT=0 -> P=%s\n", iptr(b2.P), iptr(b3.P))
}

func iptr(p *int) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("&%d", *p)
}

func sptr(p *[]string) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("&%v", *p)
}

// ---------------------------------------------------------------------------
// D11 Presence-probing as a route to a slice's length. ADR-0003 named #8 as an
//     alternative to enumeration; this is why it is not one.
// ---------------------------------------------------------------------------

type D11Conf struct {
	Tags []string
}

func d11() {
	dhdr("D11 discovering an indexed composite's length by probing presence")
	s := mustSchema(reflect.TypeFor[D11Conf]())
	planes := []struct {
		label string
		vals  map[Path]Value
	}{
		{"TAGS_0,1,2 contiguous", map[Path]Value{
			addr("Tags").Index(0): String("a"), addr("Tags").Index(1): String("b"), addr("Tags").Index(2): String("c")}},
		{"TAGS_0 and TAGS_2, a hole", map[Path]Value{
			addr("Tags").Index(0): String("a"), addr("Tags").Index(2): String("c")}},
	}
	for _, pl := range planes {
		// probe-until-miss, which is what presence alone can do
		var got []string
		calls := 0
		for i := 0; ; i++ {
			calls++
			v := pl.vals[addr("Tags").Index(i)]
			if v.Kind() == VAbsent {
				break
			}
			got = append(got, v.Text())
		}
		// enumeration, which is what ADR-0004 gave Load instead
		var b D11Conf
		_, _ = loadD(pl.vals, s, reflect.ValueOf(&b).Elem(), loadOpts{})
		fmt.Printf("  %-27s probe-until-miss: %-12v in %d Get calls | enumerate: %v in 1\n",
			pl.label, got, calls, b.Tags)
	}
	fmt.Println("  A hole truncates, silently, which is what ADR-0001 rules out; and probing")
	fmt.Println("  costs one Get per element plus one. Enumeration is the only route.")
}

// ---------------------------------------------------------------------------
// D12 Observable presence: the mechanism ADR-0001 milestones plane inspection
//     on, and what it costs.
// ---------------------------------------------------------------------------

type D12Conf struct {
	Host string `ferry:"host,default=localhost"`
	Port int    `ferry:"port"`
	Tag  string `ferry:"tag"`
}

func d12() {
	dhdr("D12 observable presence, and drift detection by plane inspection")
	s := mustSchema(reflect.TypeFor[D12Conf]())

	planes := []struct {
		label string
		vals  map[Path]Value
	}{
		{"base           ", map[Path]Value{addr("host"): String("db1"), addr("port"): Number("5432"), addr("tag"): String("x")}},
		{"/port DELETED  ", map[Path]Value{addr("host"): String("db1"), addr("tag"): String("x")}},
		{"/port set to 0 ", map[Path]Value{addr("host"): String("db1"), addr("port"): Number("0"), addr("tag"): String("x")}},
	}
	for _, pl := range planes {
		seen := map[Path]Value{}
		var v D12Conf
		_, _ = loadD(pl.vals, s, reflect.ValueOf(&v).Elem(), loadOpts{
			observe: func(p Path, val Value) { seen[p] = val },
		})
		obs := ""
		for _, p := range sortedAddrs(seen) {
			obs += fmt.Sprintf("%s=%s ", p, seen[p].GoString())
		}
		fmt.Printf("  %s struct=%+v\n     observed %s\n", pl.label, v, obs)
	}
	fmt.Println("  Rows 2 and 3 produce an IDENTICAL struct and two different observations.")
	fmt.Println("  A key deleted from the plane and a key changed to zero are the same")
	fmt.Println("  loaded value, which is ADR-0001's 'a loaded struct erases absence'.")
	fmt.Println("  The observation is the erasure made optional, and it is what ADR-0001")
	fmt.Println("  milestones plane inspection on.")

	base := planes[0].vals
	r1 := testing.Benchmark(func(bm *testing.B) {
		var v D12Conf
		dst := reflect.ValueOf(&v).Elem()
		for bm.Loop() {
			_, _ = loadD(base, s, dst, loadOpts{})
		}
	})
	sink := func(Path, Value) {}
	r2 := testing.Benchmark(func(bm *testing.B) {
		var v D12Conf
		dst := reflect.ValueOf(&v).Elem()
		for bm.Loop() {
			_, _ = loadD(base, s, dst, loadOpts{observe: sink})
		}
	})
	fmt.Printf("  load without observer: %s\n  load with observer:    %s\n", r1, r2)
}

// ---------------------------------------------------------------------------
// D13 Does ferry ever hand a sink an Absent, and is omission deletion?
// ---------------------------------------------------------------------------

type D13Conf struct {
	A string `ferry:"a"`
	B string `ferry:"b,omitzero"`
}

func d13() {
	dhdr("D13 what a sink is handed, and what omission means to an existing plane")
	s := mustSchema(reflect.TypeFor[D13Conf]())
	calls, _ := dumpD(reflect.ValueOf(D13Conf{A: "x", B: ""}), s)
	fmt.Printf("  dump {A:x B:\"\"} with B omitzero -> %d Set calls: %s\n", len(calls), callStr(calls))
	absent := 0
	for _, c := range calls {
		if c.v.Kind() == VAbsent {
			absent++
		}
	}
	fmt.Printf("  Set calls carrying Absent: %d\n", absent)

	existing := map[Path]Value{addr("a"): String("old"), addr("b"): String("stale")}
	patch := map[Path]Value{}
	for k, v := range existing {
		patch[k] = v
	}
	for _, c := range calls {
		patch[c.p] = c.v
	}
	replace := callsToMap(calls)
	var fromPatch, fromReplace D13Conf
	_, _ = loadD(patch, s, reflect.ValueOf(&fromPatch).Elem(), loadOpts{})
	_, _ = loadD(replace, s, reflect.ValueOf(&fromReplace).Elem(), loadOpts{})
	fmt.Printf("  into a PATCHING sink over an existing plane -> loads back %+v\n", fromPatch)
	fmt.Printf("  into a REPLACING sink                       -> loads back %+v\n", fromReplace)
	fmt.Println("  Omission is 'ferry did not write here', never 'ensure nothing is here'.")
	fmt.Println("  ferry has no delete verb, so the two sinks disagree and both are legal.")
}
