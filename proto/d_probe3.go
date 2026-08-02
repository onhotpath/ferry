package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

// ---------------------------------------------------------------------------
// D14 Does a default under an optional subtree materialise the pointer?
// ---------------------------------------------------------------------------

type D14Auth struct {
	User string `ferry:"user,default=admin"`
	Pass string `ferry:"pass"`
}

type D14Conf struct {
	Name string   `ferry:"name"`
	Auth *D14Auth `ferry:"auth"`
	TLS  *D14Auth `ferry:"tls"`
	P    *int     `ferry:"p,default=5"`
}

func d14() {
	dhdr("D14 a default inside an optional subtree")
	s := mustSchema(reflect.TypeFor[D14Conf]())
	planes := []struct {
		label string
		vals  map[Path]Value
	}{
		{"nothing under /auth", map[Path]Value{addr("name"): String("svc")}},
		{"/auth/pass present", map[Path]Value{addr("name"): String("svc"), addr("auth", "pass"): String("p")}},
	}
	for _, rule := range []struct {
		label string
		o     loadOpts
	}{
		{"a default does NOT count as presence", loadOpts{}},
		{"a default DOES count as presence    ", loadOpts{defaultsCountAsPresence: true}},
	} {
		for _, pl := range planes {
			var v D14Conf
			_, _ = loadD(pl.vals, s, reflect.ValueOf(&v).Elem(), rule.o)
			fmt.Printf("  %s | %-20s -> Auth=%-16s TLS=%-16s P=%s\n",
				rule.label, pl.label, authStr(v.Auth), authStr(v.TLS), iptr(v.P))
		}
	}
	fmt.Println("  Under the second rule no *T with a default anywhere beneath it can ever")
	fmt.Println("  be nil, which is the whole meaning of the pointer. Under the first, a")
	fmt.Println("  default fills a hole IN a section and never conjures the section.")
	fmt.Println("  /p is a pointer to a LEAF, so its default sits at its own address and")
	fmt.Println("  applies either way: that is not the same case.")
}

func authStr(a *D14Auth) string {
	if a == nil {
		return "nil"
	}
	return fmt.Sprintf("&%+v", *a)
}

// ---------------------------------------------------------------------------
// D15 A declaration attaches to the address SHAPE, not to a realised address.
// ---------------------------------------------------------------------------

type D15Elem struct {
	Host string `ferry:"host"`
	Port int    `ferry:"port,default=8080"`
}

type D15Conf struct {
	Servers map[string]D15Elem `ferry:"servers"`
	Pool    []D15Elem          `ferry:"pool"`
}

func d15() {
	dhdr("D15 a default inside a map value and a slice element")
	s := mustSchema(reflect.TypeFor[D15Conf]())
	fmt.Println("  the schema's static addresses:")
	for _, p := range sortedPaths(s.addrs) {
		o := s.at(p)
		d := ""
		if o.hasDef {
			d = " default=" + o.defText
		}
		fmt.Printf("     %s%s\n", p, d)
	}
	plane := map[Path]Value{
		addr("servers", "a", "host"):       String("h1"),
		addr("pool").Index(0).Name("host"): String("h2"),
	}
	for _, c := range []struct {
		label string
		o     loadOpts
	}{
		{"looked up by the REALISED address", loadOpts{byRealisedAddress: true}},
		{"looked up by the address SHAPE   ", loadOpts{}},
	} {
		var v D15Conf
		_, _ = loadD(plane, s, reflect.ValueOf(&v).Elem(), c.o)
		fmt.Printf("  %s -> Servers=%v Pool=%v\n", c.label, v.Servers, v.Pool)
	}
	fmt.Println("  /servers/a/port is not in the schema and never can be, because the key")
	fmt.Println("  comes from the value (ADR-0003's dynamic tier). The declaration lives at")
	fmt.Println("  /servers/*/port and the walk has to carry both paths to find it.")
}

// ---------------------------------------------------------------------------
// D16 What template generation and schema extraction get out of this.
// ---------------------------------------------------------------------------

type D16Conf struct {
	Host  string `ferry:"host,default=localhost"`
	Port  int    `ferry:"port,default=8080"`
	Token string `ferry:"token,required"`
	Tags  []string
}

func d16() {
	dhdr("D16 template generation is Load from an empty plane, then Dump")
	s := mustSchema(reflect.TypeFor[D16Conf]())
	var v D16Conf
	_, err := loadD(map[Path]Value{}, s, reflect.ValueOf(&v).Elem(), loadOpts{})
	fmt.Printf("  load from an EMPTY plane -> %+v\n  err=%v\n", v, err)
	calls, _ := dumpD(reflect.ValueOf(v), s)
	fmt.Printf("  dumped into a recorder   -> %s\n", callStr(calls))
	fmt.Println("  The defaults reach the artefact, which is what ADR-0001's Enabled")
	fmt.Println("  template generation and schema extraction both need. But required")
	fmt.Println("  fires first, so the defaulted value of T is not reachable by Load")
	fmt.Println("  alone. That is #14's to resolve, and the mechanism it needs is the")
	fmt.Println("  compiled schema holding the defaults, which this ADR puts there.")
	fmt.Println("  Note also what a default is NOT: it is a Load-side rule only, so")
	fmt.Println("  Dump writes the value in hand and never substitutes a default.")
}

// ---------------------------------------------------------------------------
// D17 The alternative that needs no core mechanism: defaults as a Static
//     source under FirstOf. ADR-0004 names it by that name.
// ---------------------------------------------------------------------------

func d17() {
	dhdr("D17 defaults as a Static source, and what it costs")
	type v1 struct {
		Port int `ferry:"port"`
	}
	type v2 struct {
		Port int `ferry:"listen_port"` // the field was renamed
	}
	static := map[Path]Value{addr("port"): String("8080")}

	for _, c := range []struct {
		label string
		t     reflect.Type
		get   func(reflect.Value) int
	}{
		{"before the rename", reflect.TypeFor[v1](), func(v reflect.Value) int { return int(v.Field(0).Int()) }},
		{"after the rename ", reflect.TypeFor[v2](), func(v reflect.Value) int { return int(v.Field(0).Int()) }},
	} {
		s := mustSchema(c.t)
		dst := reflect.New(c.t).Elem()
		_, _ = loadD(static, s, dst, loadOpts{})
		fmt.Printf("  Static source: %s -> Port=%d\n", c.label, c.get(dst))
	}
	fmt.Println("  A Static defaults layer spells addresses by hand, so it is a SECOND")
	fmt.Println("  place the address set is written down and nothing checks the two agree.")
	fmt.Println("  A declared default cannot drift, because it is on the field.")
	fmt.Println("  It is also invisible to schema extraction and template generation,")
	fmt.Println("  which see the plane and not the source stack.")
	fmt.Println("  The combinator stays expressible; it is a composition and not a second")
	fmt.Println("  ferry-supplied way to declare a default (5.14's first item).")
}

// ---------------------------------------------------------------------------
// D18 A default is indistinguishable from a plane value, on every plane.
// ---------------------------------------------------------------------------

type D18Conf struct {
	Host string `ferry:"host,default=localhost"`
	Port int    `ferry:"port,default=8080"`
	TO   string `ferry:"to,default="`
}

func d18() {
	dhdr("D18 the same default through all three planes the harness uses")
	ctx := context.Background()
	s := mustSchema(reflect.TypeFor[D18Conf]())

	// memory
	var a D18Conf
	_, _ = loadD(map[Path]Value{}, s, reflect.ValueOf(&a).Elem(), loadOpts{})
	fmt.Printf("  memory plane, empty     -> %+v\n", a)

	// flattening plane (String for everything, no null)
	flat, _ := flatten(map[Path]Value{})
	var b D18Conf
	_, _ = loadD(flat, s, reflect.ValueOf(&b).Elem(), loadOpts{})
	fmt.Printf("  flattening plane, empty -> %+v\n", b)

	// real YAML over a real file
	dir, _ := os.MkdirTemp("", "ferry8")
	defer os.RemoveAll(dir)
	f := filepath.Join(dir, "c.yaml")
	_ = os.WriteFile(f, []byte("host: fromfile\n"), 0o644)
	vals, err := yamlVals(ctx, f, s.addrs)
	if err != nil {
		fmt.Println("  yaml read err:", err)
		return
	}
	var c D18Conf
	_, _ = loadD(vals, s, reflect.ValueOf(&c).Elem(), loadOpts{})
	fmt.Printf("  real YAML, host only    -> %+v\n", c)

	// and what a hand-written null does on the plane that can express one
	_ = os.WriteFile(f, []byte("host: fromfile\nport:\n"), 0o644)
	vals2, _ := yamlVals(ctx, f, s.addrs)
	fmt.Printf("  yaml `port:` (empty key) reports %s at /port\n", vals2[addr("port")].GoString())
	var d D18Conf
	_, e := loadD(vals2, s, reflect.ValueOf(&d).Elem(), loadOpts{})
	fmt.Printf("  loading it              -> %+v err=%v\n", d, e)
	fmt.Println("  That is the ergonomic cost of admitting Null by type: a commented-out")
	fmt.Println("  or blank YAML key is a null, and an int cannot hold one, so it is a")
	fmt.Println("  loud refusal rather than the default. Deleting the key takes the default.")
}

func yamlVals(ctx context.Context, path string, addrs []Path) (map[Path]Value, error) {
	as := NewAddressSet(addrs)
	open, err := (FYAMLSource{Path: path}).Bind(as)
	if err != nil {
		return nil, err
	}
	return fLoad(ctx, open, as)
}

// ---------------------------------------------------------------------------
// D19 The case none of the other probes covered: loading TWICE into one
//     destination, with the plane changing in between.
// ---------------------------------------------------------------------------

type D19Conf struct {
	Host string `ferry:"host"`
	Port int    `ferry:"port"`
}

func d19() {
	dhdr("D19 reload: the same destination, loaded twice, with a key deleted")
	s := mustSchema(reflect.TypeFor[D19Conf]())
	first := map[Path]Value{addr("host"): String("db1"), addr("port"): Number("5432")}
	second := map[Path]Value{addr("host"): String("db1")} // /port DELETED

	for _, c := range []struct {
		label string
		o     loadOpts
	}{
		{"absent-does-not-write", loadOpts{}},
		{"absent-yields-zero   ", loadOpts{absentZeroesComposite: true}},
	} {
		var v D19Conf
		_, _ = loadD(first, s, reflect.ValueOf(&v).Elem(), c.o)
		before := v
		_, _ = loadD(second, s, reflect.ValueOf(&v).Elem(), c.o)
		var fresh D19Conf
		_, _ = loadD(second, s, reflect.ValueOf(&fresh).Elem(), c.o)
		fmt.Printf("  %s  in place: %+v -> %+v | into a FRESH value: %+v\n", c.label, before, v, fresh)
	}
	fmt.Println("  Loading in place leaks the previous load's value for a key the plane no")
	fmt.Println("  longer has, under BOTH rules, because a scalar's absence is not a")
	fmt.Println("  container's. So 'Absent does not write' is a statement about ONE load")
	fmt.Println("  into the destination the caller supplied, and a reload has to produce a")
	fmt.Println("  NEW value rather than mutate a live one. That constrains #16 and #13.")
}

// ---------------------------------------------------------------------------
// D20 A default inside an array element, whose addresses are static.
// ---------------------------------------------------------------------------

type D20Conf struct {
	Arr [2]D15Elem `ferry:"arr"`
}

func d20() {
	dhdr("D20 a default inside an array element (static addresses, unlike a slice)")
	s := mustSchema(reflect.TypeFor[D20Conf]())
	for _, p := range sortedPaths(s.addrs) {
		o := s.at(p)
		d := ""
		if o.hasDef {
			d = " default=" + o.defText
		}
		fmt.Printf("     %s%s\n", p, d)
	}
	var v D20Conf
	_, _ = loadD(map[Path]Value{addr("arr").Index(0).Name("host"): String("h")}, s,
		reflect.ValueOf(&v).Elem(), loadOpts{})
	fmt.Printf("  /arr#0/host set, everything else absent -> %+v\n", v)
	fmt.Println("  Element 1 has nothing under it on the plane and still takes its default,")
	fmt.Println("  because an array element is a STATIC address like a struct field and is")
	fmt.Println("  walked either way. A slice element is not: it exists only if the plane")
	fmt.Println("  has one. Two types a user treats as interchangeable, differing again.")
}

// ---------------------------------------------------------------------------
// D21 The audit of this ADR's strongest claim: does "Absent does not write"
//     say the same thing at every kind, or does it only look like it?
// ---------------------------------------------------------------------------

type D21Inner struct {
	User string `ferry:"user"`
	Pass string `ferry:"pass"`
}

type D21Conf struct {
	Auth   D21Inner       `ferry:"auth"`
	Tags   []string       `ferry:"tags"`
	Limits map[string]int `ferry:"limits"`
}

func d21() {
	dhdr("D21 partial presence into a seeded destination, per kind")
	s := mustSchema(reflect.TypeFor[D21Conf]())
	seed := func() D21Conf {
		return D21Conf{
			Auth:   D21Inner{User: "u", Pass: "p"},
			Tags:   []string{"a", "b"},
			Limits: map[string]int{"rps": 1, "burst": 2},
		}
	}
	plane := map[Path]Value{
		addr("auth", "user"):  String("NEW"),
		addr("tags").Index(0): String("NEW"),
		addr("limits", "rps"): Number("99"),
	}
	v := seed()
	_, _ = loadD(plane, s, reflect.ValueOf(&v).Elem(), loadOpts{})
	fmt.Printf("  seed   %+v\n", seed())
	fmt.Printf("  plane  /auth/user, /tags#0 and /limits/rps only\n")
	fmt.Printf("  result %+v\n", v)
	fmt.Println("  A STRUCT merges, because each field is its own address and the ones the")
	fmt.Println("  plane does not have are Absent. A SLICE and a MAP replace, because the")
	fmt.Println("  container is one decision and the plane made it. Both follow from the")
	fmt.Println("  rule; they do not look like they do, and that has to be documented.")
}

// ---------------------------------------------------------------------------
// D22 required at a container address, which no other probe reached.
// ---------------------------------------------------------------------------

type D22Conf struct {
	Tags   []string       `ferry:"tags,required"`
	Limits map[string]int `ferry:"limits,required"`
	Auth   *D21Inner      `ferry:"auth,required"`
}

func d22() {
	dhdr("D22 required at a container address")
	s := mustSchema(reflect.TypeFor[D22Conf]())
	for _, c := range []struct {
		label string
		plane map[Path]Value
	}{
		{"nothing at all", map[Path]Value{}},
		{"explicit Null at each", map[Path]Value{
			addr("tags"): Null(), addr("limits"): Null(), addr("auth"): Null()}},
		{"one child under each", map[Path]Value{
			addr("tags").Index(0): String("a"), addr("limits", "rps"): Number("1"),
			addr("auth", "user"): String("u")}},
	} {
		var v D22Conf
		_, err := loadD(c.plane, s, reflect.ValueOf(&v).Elem(), loadOpts{})
		fmt.Printf("  %-22s -> Tags=%v Limits=%v Auth=%v err=%v\n",
			c.label, v.Tags, v.Limits, v.Auth != nil, err)
	}
	fmt.Println("  A container's presence is children, or a Null at its own address.")
	fmt.Println("  It CANNOT be present-and-empty, because no plane can report that")
	fmt.Println("  (ADR-0005's forced collision), so `tags: []` reads as Absent and")
	fmt.Println("  cannot satisfy required. That is inherited, not chosen.")
}
