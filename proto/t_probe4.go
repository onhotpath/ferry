package main

// T20: encoding/json/v2 solves ferry's escaping problem in Go 1.27, and says
// why in a source comment:
//
//	"The grammar is nearly identical to a double-quoted Go string literal,
//	 but uses single quotes as the terminators. The reason for a custom
//	 grammar is because both backtick and double quotes cannot be used
//	 verbatim in a struct tag."
//	 -- go1.27rc2 src/encoding/json/v2/fields.go, consumeTagOption
//
// That is T1/T2's constraint, reached independently by the standard library,
// in the release ADR-0001 makes ferry's floor. So the question is not whether
// to invent an escape but whether to copy this one.

import (
	json "encoding/json/v2"
	"fmt"
	"reflect"
	"time"
)

func init() { t11Hooks = append(t11Hooks, runT20) }

type v2Plain struct {
	A string `json:"a,b"`
}
type v2Quoted struct {
	A string `json:"'a,b'"`
}
type v2QuotedOpt struct {
	A string `json:"'a,b',omitzero"`
	B string `json:"'x=y'"`
	C string `json:"'has space'"`
	D string `json:"'-'"`
	E string `json:"'tilde~here'"`
}
type v2QuoteInName struct {
	A string `json:"'it\\'s'"`
}
// embed on a NAMED field is where the two words actually differ: an anonymous
// field is promoted by v2 with or without the option, so testing it there
// measures nothing.
type v2Embed struct {
	C    Common `json:",embed"`
	Port int    `json:"port"`
}
type v2Inline struct {
	C    Common `json:",inline"`
	Port int    `json:"port"`
}
type v2AnonEmbed struct {
	Common
	Port int `json:"port"`
}
type v2Fmt struct {
	T time.Time `json:"t,format:'2006-01-02'"`
}
type v2FmtComma struct {
	T time.Time `json:"t,format:'2006,01,02'"`
}
type v2Unknown struct {
	A string `json:"a,xyzzy"`
}
type v2NearMiss struct {
	A string `json:"a,omitEmpty"`
}

func runT20() {
	hdr("T20  how encoding/json/v2 escapes a name, measured on go1.27rc2")
	for _, c := range []struct {
		what string
		v    any
	}{
		{`json:"a,b"          unquoted`, v2Plain{A: "v"}},
		{`json:"'a,b'"        single-quoted`, v2Quoted{A: "v"}},
		{`json:"'a,b',omitzero"`, v2QuotedOpt{A: "v", B: "y", C: "s", D: "d", E: "t"}},
		{`json:"'it\\'s'"     a quote inside`, v2QuoteInName{A: "v"}},
	} {
		b, err := json.Marshal(c.v)
		if err != nil {
			fmt.Printf("  %-34s ERROR %v\n", c.what, err)
			continue
		}
		fmt.Printf("  %-34s %s\n", c.what, b)
	}

	fmt.Println()
	fmt.Println("  the raw tag as reflect sees it, for the escaped-quote case:")
	t := reflect.TypeFor[v2QuoteInName]()
	v, ok := t.Field(0).Tag.Lookup("json")
	fmt.Printf("    Lookup -> %q ok=%v\n", v, ok)

	hdr("T20b  json/v2's `inline` is `embed` in 1.27, verified by execution")
	b, err := json.Marshal(v2AnonEmbed{Common: Common{Name: "n", Env: "e"}, Port: 8080})
	fmt.Printf("  anonymous field, no option -> %s   err=%v\n", b, err)
	b, err = json.Marshal(v2Embed{C: Common{Name: "n", Env: "e"}, Port: 8080})
	fmt.Printf("  NAMED field, json:\",embed\"  -> %s   err=%v\n", b, err)
	b, err = json.Marshal(v2Inline{C: Common{Name: "n", Env: "e"}, Port: 8080})
	fmt.Printf("  NAMED field, json:\",inline\" -> %s   err=%v\n", b, err)
	fmt.Println("  `inline` is not in v2's option set at all in 1.27, so it falls through the")
	fmt.Println("  default arm and is ignored. That is the Kubernetes no-op, still reproducible.")

	hdr("T20d  the one place v2 DOES allow a quoted value, and what it cannot reach")
	b, err = json.Marshal(v2Fmt{T: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)})
	fmt.Printf("  format with a plain layout   -> %s  err=%v\n", b, err)
	b, err = json.Marshal(v2FmtComma{T: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)})
	fmt.Printf("  format with a comma layout  -> %s  err=%v\n", b, err)
	fmt.Println("  so v2 has the machinery for a quoted, comma-carrying token and wires it to")
	fmt.Println("  `format:` only. consumeTagOption is called with allowQuoted=false at the")
	fmt.Println("  NAME position, which is why the name above was refused.")

	hdr("T20c  what json/v2 does with an option it does not know")
	b, err = json.Marshal(v2Unknown{A: "v"})
	fmt.Printf("  json:\"a,xyzzy\"     -> %s   err=%v\n", b, err)
	b, err = json.Marshal(v2NearMiss{A: "v"})
	fmt.Printf("  json:\"a,omitEmpty\" -> %s   err=%v\n", b, err)
	fmt.Println("  so v2 rejects a NEAR MISS of one of its six options and silently ignores")
	fmt.Println("  everything else, with a source comment saying that is not a promise:")
	fmt.Println("    \"Everything else is ignored. This does not mean it is forward compatible")
	fmt.Println("     to insert arbitrary tag options since a future version of this package")
	fmt.Println("     may understand that tag.\"")
	fmt.Println("  ADR-0001 chose to reject instead, and this measures how much stricter that")
	fmt.Println("  is than v2 rather than leaving the two sounding identical.")
}

func init() { t11Hooks = append(t11Hooks, runT21) }

// The single-quote model, priced. It is the shape v2's source contains, and
// the reason ferry does not take it is one backslash.
type sqRight struct {
	A string `ferry:"'it\\'s'"`
}
type sqWrong struct {
	A string `ferry:"'it\'s'"`
	B string `json:"jname"`
}

func runT21() {
	hdr("T21  the single-quote model's one-backslash landmine")
	for _, c := range []struct {
		what string
		t    reflect.Type
	}{
		{`ferry:"'it\\'s'"  the correct spelling`, reflect.TypeFor[sqRight]()},
		{`ferry:"'it\'s'"   one backslash short`, reflect.TypeFor[sqWrong]()},
	} {
		f := c.t.Field(0)
		v, ok := f.Tag.Lookup("ferry")
		extra := ""
		if c.t.NumField() > 1 {
			j, jok := c.t.Field(1).Tag.Lookup("json")
			extra = fmt.Sprintf("   neighbour json tag: %q ok=%v", j, jok)
		}
		fmt.Printf("  %-38s Lookup -> %-12q ok=%v%s\n", c.what, v, ok, extra)
	}
	fmt.Println("  a single-quoted grammar needs `\\'` inside a name, which a struct tag value")
	fmt.Println("  spells `\\\\'`, and writing one backslash makes the whole tag invisible.")
	fmt.Println("  `~` needs no backslash at any depth, which is why ferry takes it.")

	hdr("T22  the one mistake the grammar cannot see")
	for _, c := range []string{"required", "omitzero", "default"} {
		d, errs := parseFerryTag(c)
		fmt.Printf("  ferry:%-12q -> %s   errs=%d\n", c, describe(d), len(errs))
	}
	fmt.Println("  a bare option word in the NAME position is a segment name, not an option.")
	fmt.Println("  ferry cannot refuse it: ADR-0003 says core has no opinion about segment")
	fmt.Println("  text, and a plane key really can be called `required`. What ferry can and")
	fmt.Println("  does refuse is the structural version of the same slip:")
	for _, c := range []string{"default=8080", "required=yes"} {
		_, errs := parseFerryTag(c)
		if len(errs) > 0 {
			fmt.Printf("  ferry:%-14q -> %s\n", c, trimTo(errs[0].Error(), 88))
		}
	}
}
