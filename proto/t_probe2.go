package main

import (
	"fmt"
	"reflect"
	"time"
)

func init() { t11Hooks = append(t11Hooks, runT8to13) }

// ---- T8: the naming rule ----

// The same intent written both ways. Under rule A a field with no tag takes
// its Go name; under rule B it is a schema compile error.
type namedA struct {
	Host     string
	HTTPPort int
	TLS      bool
}

type namedB struct {
	Host     string `ferry:"host"`
	HTTPPort int    `ferry:"http_port"`
	TLS      bool   `ferry:"tls"`
}

// The same two, after somebody adds an exported field for an unrelated reason.
type namedAplus struct {
	Host     string
	HTTPPort int
	TLS      bool
	Debug    bool
}

type namedBplus struct {
	Host     string `ferry:"host"`
	HTTPPort int    `ferry:"http_port"`
	TLS      bool   `ferry:"tls"`
	Debug    bool
}

// compileGoName is rule A: an untagged exported field takes its Go name.
func compileGoName(t reflect.Type) []Path {
	var out []Path
	var rec func(reflect.Type, Path)
	rec = func(t reflect.Type, p Path) {
		if t.Kind() != reflect.Struct || classify(t) != shapeStruct {
			out = append(out, p)
			return
		}
		for i := range t.NumField() {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			n := f.Name
			if raw, err := rawFerryTag(f.Tag); err == nil && raw != nil {
				d, _ := parseFerryTag(*raw)
				if d.skip {
					continue
				}
				if d.hasName {
					n = d.name
				}
			}
			rec(f.Type, p.Name(n))
		}
	}
	rec(t, Path{})
	return out
}

// ---- T9: embedding ----

type Common struct {
	Name string `ferry:"name"`
	Env  string `ferry:"env"`
}

type embPromoted struct {
	Common
	Port int `ferry:"port"`
}

type embNested struct {
	Common `ferry:"common"`
	Port   int `ferry:"port"`
}

type embClash struct {
	Common
	Name string `ferry:"name"`
	Port int    `ferry:"port"`
}

type embSkipped struct {
	Common `ferry:"-"`
	Port   int `ferry:"port"`
}

type embNonStruct struct {
	time.Duration
	Port int `ferry:"port"`
}

// ---- T10: ADR-0006's five refusals, through the real grammar ----

type ref1 struct {
	P int `ferry:"p,default=abc"`
}
type ref2 struct {
	Tags []string `ferry:"tags,default=a"`
}
type ref3 struct {
	Origins []string `ferry:"origins,required"`
}
type ref4 struct {
	S string `ferry:"s,required,default=x"`
}
type ref5 struct {
	B int `ferry:"b,omitzero,default=8080"`
}
type refOK struct {
	C int `ferry:"c,omitzero,default=0"`
}

// one field, three rules tripped at once
type refStack struct {
	O []string `ferry:"o,required,default=value"`
}
type refStack2 struct {
	S string `ferry:"s,required,default=x"`
}
type refStack3 struct {
	I int `ferry:"i,omitzero,default=abc"`
}

// ---- T11: skip, unexported, no tag ----

type skipCases struct {
	Mapped   string `ferry:"mapped"`
	Ignored  string `ferry:"-"`
	Untagged string
	hidden   string
	tagged   string `ferry:"tagged"`
}

// ---- T13: ADR-0007's documentation obligation ----

type b64 struct {
	Secret []byte `ferry:"secret,default=aGk="`
}

func runT8to13() {
	hdr("T8  the naming rule: a Go-name default against an explicit name")
	fmt.Println("  rule A, untagged fields take the Go field name:")
	for _, p := range sortedPaths(compileGoName(reflect.TypeFor[namedA]())) {
		fmt.Printf("      %s\n", p)
	}
	fmt.Println("  rule B, a named field must name its segment:")
	_, err := compileT(reflect.TypeFor[namedA]())
	printErrs("      ", err)
	s, err := compileT(reflect.TypeFor[namedB]())
	if err == nil {
		for _, p := range sortedPaths(s.addrs) {
			fmt.Printf("      %s\n", p)
		}
	}

	fmt.Println("\n  and then somebody adds an exported field for an unrelated reason:")
	fmt.Println("  rule A:")
	for _, p := range sortedPaths(compileGoName(reflect.TypeFor[namedAplus]())) {
		fmt.Printf("      %s\n", p)
	}
	fmt.Println("  rule B:")
	_, err = compileT(reflect.TypeFor[namedBplus]())
	printErrs("      ", err)

	hdr("T9  embedding: promotion needs no word in the vocabulary")
	for _, c := range []struct {
		what string
		t    reflect.Type
	}{
		{"embedded, no ferry tag        -> promoted", reflect.TypeFor[embPromoted]()},
		{"embedded, ferry:\"common\"      -> nested", reflect.TypeFor[embNested]()},
		{"embedded, ferry:\"-\"           -> skipped", reflect.TypeFor[embSkipped]()},
		{"promotion clashing with a sibling", reflect.TypeFor[embClash]()},
		{"embedded non-struct", reflect.TypeFor[embNonStruct]()},
	} {
		fmt.Printf("  %s\n", c.what)
		s, err := compileT(c.t)
		if err != nil {
			printErrs("      ", err)
			continue
		}
		ps := sortedPaths(s.addrs)
		fmt.Printf("      %v\n", ps)
		if clash := prefixFreeViolations(ps); len(clash) > 0 {
			fmt.Printf("      prefix-free violation: %v\n", clash)
		}
	}

	hdr("T10  ADR-0006's five refusals, driven by #11's grammar rather than the placeholder")
	for _, c := range []struct {
		what string
		t    reflect.Type
	}{
		{"default that does not parse", reflect.TypeFor[ref1]()},
		{"default on a composite", reflect.TypeFor[ref2]()},
		{"required on a dynamic composite", reflect.TypeFor[ref3]()},
		{"required with a default", reflect.TypeFor[ref4]()},
		{"omitzero with a non-zero default", reflect.TypeFor[ref5]()},
		{"omitzero with a zero default (legal)", reflect.TypeFor[refOK]()},
	} {
		fmt.Printf("  %-38s", c.what)
		_, err := compileT(c.t)
		if err == nil {
			fmt.Println("compiles")
			continue
		}
		ls := errLines(err)
		fmt.Println(ls[0])
		for _, l := range ls[1:] {
			fmt.Printf("  %-38s%s\n", "", l)
		}
	}

	hdr("T11  admissibility before contradictions: one field's mistake")
	for _, c := range []struct {
		what string
		t    reflect.Type
	}{
		{"[]string  required,default=value", reflect.TypeFor[refStack]()},
		{"string    required,default=x", reflect.TypeFor[refStack2]()},
		{"int       omitzero,default=abc", reflect.TypeFor[refStack3]()},
	} {
		_, err := compileT(c.t)
		ls := errLines(err)
		fmt.Printf("  %-34s %d error(s)\n", c.what, len(ls))
		for _, l := range ls {
			fmt.Printf("      %s\n", l)
		}
	}

	hdr("T12  skip, unexported, and no tag at all")
	_, err = compileT(reflect.TypeFor[skipCases]())
	printErrs("  ", err)
	s, _ = compileT(reflect.TypeFor[struct {
		Mapped  string `ferry:"mapped"`
		Ignored string `ferry:"-"`
		hidden  string
	}]())
	if s != nil {
		fmt.Printf("  with the untagged field removed: %v\n", sortedPaths(s.addrs))
	}

	hdr("T13  ADR-0007's sharp edge, re-measured: default=aGk= on a []byte field")
	s, err = compileT(reflect.TypeFor[b64]())
	if err != nil {
		printErrs("  ", err)
	} else {
		p := s.addrs[0]
		o := s.at(p)
		var dst b64
		v := reflect.ValueOf(&dst).Elem().Field(0)
		_ = decLeaf(*o.def, v)
		fmt.Printf("  declared default text  %q\n", o.defText)
		fmt.Printf("  boundary Value         %s\n", o.def.GoString())
		fmt.Printf("  lands in the field as  %q  (%d bytes)\n", string(dst.Secret), len(dst.Secret))
		fmt.Printf("  the decoded form would be %q, and ferry does not decode it\n", "hi")
	}
}

func prefixFreeViolations(ps []Path) []string {
	var out []string
	for i := range ps {
		for j := range ps {
			if i == j {
				continue
			}
			a, b := ps[i].String(), ps[j].String()
			if a == b && i < j {
				out = append(out, a+" == "+b)
			}
		}
	}
	return out
}

func init() { t11Hooks = append(t11Hooks, runT23) }

// T23: the diagnostic rule has three tiers, not two. ADR-0006 stated the
// lower two; the grammar adds the first, and the maps-no-address check sits
// downstream of all three.
type tierGrammar struct {
	H string `ferry:"h,requird"`
}
type tierAdmis struct {
	O []string `ferry:"o,required,default=v"`
}
type tierContra struct {
	S string `ferry:"s,required,default=x"`
}
type tierMixed struct {
	A string   `ferry:"a,requird"`
	B []string `ferry:"b,required"`
	C string   `ferry:"c,required,default=x"`
	D string   `ferry:"d"`
}

func runT23() {
	hdr("T23  three diagnostic tiers, and one mistake reporting once")
	for _, c := range []struct {
		what string
		t    reflect.Type
	}{
		{"tier 1, the tag does not parse", reflect.TypeFor[tierGrammar]()},
		{"tier 2, an option is inadmissible here", reflect.TypeFor[tierAdmis]()},
		{"tier 3, two admissible options contradict", reflect.TypeFor[tierContra]()},
		{"one of each, on four fields", reflect.TypeFor[tierMixed]()},
	} {
		_, err := compileT(c.t)
		ls := errLines(err)
		fmt.Printf("  %-42s %d error(s)\n", c.what, len(ls))
		for _, l := range ls {
			fmt.Printf("      %s\n", trimTo(l, 110))
		}
	}
	fmt.Println("  tier 1 fires alone, because a tag that did not parse declares nothing for")
	fmt.Println("  tier 2 to judge; and \"maps no address\" is suppressed at a level that already")
	fmt.Println("  reported a field error, since it is that error's consequence.")
}

func init() { t11Hooks = append(t11Hooks, runT24) }

// T24: ADR-0006's one grammar requirement, exercised through a load rather
// than asserted. "" is a legitimate default and "leave the field alone" is a
// different instruction, so the two must be spellable apart.
type emptyDef struct {
	WithEmpty string `ferry:"a,default="`
	WithNone  string `ferry:"b"`
	WithText  string `ferry:"c,default=x"`
}

func runT24() {
	hdr("T24  an empty default against no default, through a load")
	s, err := compileT(reflect.TypeFor[emptyDef]())
	if err != nil {
		printErrs("  ", err)
		return
	}
	for _, p := range sortedPaths(s.addrs) {
		o := s.at(p)
		d := "no default"
		if o.hasDef {
			d = fmt.Sprintf("default %s", o.def.GoString())
		}
		fmt.Printf("  %-6s %s\n", p, d)
	}
	seed := emptyDef{WithEmpty: "seeded", WithNone: "seeded", WithText: "seeded"}
	dst := seed
	if _, err := loadD(map[Path]Value{}, s, reflect.ValueOf(&dst).Elem(), loadOpts{}); err != nil {
		fmt.Println("  load:", err)
	}
	fmt.Printf("  seeded %+v\n", seed)
	fmt.Printf("  loaded from an EMPTY plane -> %+v\n", dst)
	fmt.Println("  `default=` wrote the empty string over the seed; no default left it alone.")
}

func init() { t11Hooks = append(t11Hooks, runT25) }

// T25: which option is admissible where, and whether an option meant for one
// direction leaks into the other.
type ozAll struct {
	S    string         `ferry:"s,omitzero"`
	Sl   []string       `ferry:"sl,omitzero"`
	M    map[string]int `ferry:"m,omitzero"`
	P    *int           `ferry:"p,omitzero"`
	Nest Common         `ferry:"nest,omitzero"`
	Arr  [2]string      `ferry:"arr,omitzero"`
}

type dirLoad struct {
	A string `ferry:"a,required"`
	B string `ferry:"b,default=x"`
}
type dirDump struct {
	A string `ferry:"a,omitzero"`
	B string `ferry:"b"`
}

func runT25() {
	hdr("T25  where each option is admissible")
	_, err := compileT(reflect.TypeFor[ozAll]())
	fmt.Printf("  omitzero on leaf, slice, map, pointer, struct and array: ")
	printErrs("", err)
	fmt.Println("  omitzero is the only option admissible at every type, because it is a")
	fmt.Println("  question about the Go value (reflect.Value.IsZero) and not about an address.")
	fmt.Println()
	fmt.Println("  required: leaf, *leaf, struct, *struct, [N]T   refused on []T, map, *[]T")
	fmt.Println("  default=: leaf and *leaf only                  refused on every composite")
	fmt.Println("  both are ADR-0006's, re-run above in T10 rather than restated.")

	hdr("T26  a load-side option on Dump, and a dump-side option on Load")
	sl, err := compileT(reflect.TypeFor[dirLoad]())
	if err != nil {
		printErrs("  ", err)
		return
	}
	calls, err := dumpD(reflect.ValueOf(dirLoad{A: "", B: ""}), sl)
	fmt.Printf("  dumping a struct whose fields carry required and default= : %d Set calls, err=%v\n", len(calls), err)
	for _, c := range calls {
		fmt.Printf("      %s = %s\n", c.p, c.v.GoString())
	}
	fmt.Println("  neither option changed the dump: required asserts about a plane ferry is")
	fmt.Println("  writing, and a default answers an absence Dump never observes.")

	sd, err := compileT(reflect.TypeFor[dirDump]())
	if err != nil {
		printErrs("  ", err)
		return
	}
	var dst dirDump
	if _, err := loadD(map[Path]Value{path("a"): String("x"), path("b"): String("y")}, sd, reflect.ValueOf(&dst).Elem(), loadOpts{}); err != nil {
		fmt.Println("  load:", err)
	}
	fmt.Printf("  loading a struct whose field carries omitzero: %+v\n", dst)
	fmt.Println("  omitzero changed nothing on Load: it decides whether an address is written.")

	dd, _ := compileT(reflect.TypeFor[dirDump]())
	calls, _ = dumpD(reflect.ValueOf(dirDump{A: "", B: ""}), dd)
	fmt.Printf("  and on Dump, the same struct at its zero value: %d Set call(s)\n", len(calls))
	for _, c := range calls {
		fmt.Printf("      %s = %s\n", c.p, c.v.GoString())
	}
}
