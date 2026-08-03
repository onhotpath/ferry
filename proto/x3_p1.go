package main

// X3-1. The population.
//
// Every third-party or stdlib type any Accepted ADR names as admitted or as
// refused, compiled through the tip's own entry point against a FRESH registry.
// Fresh, because r18_freeze.go registers netip.Addr and a named duration into
// the DEFAULT registry from two package init()s, and the audit's own section
// 3.3 records that a probe reading a Compile succeeding as evidence about the
// type set reads it wrong otherwise.
//
// The verdict column is computed from the error text, not asserted: a refusal
// that names "carries no ferry tag" is ADR-0008's field rule and nothing else
// produces that sentence.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"
)

// x3Holder is the shape every row is compiled in: one named, tagged field, so
// the ONLY untagged exported fields in the type are the ones the third-party
// package wrote. reflect.StructOf builds it per row so that one loop covers
// twenty types.
func x3Holder(t reflect.Type) reflect.Type {
	return reflect.StructOf([]reflect.StructField{
		{Name: "V", Type: t, Tag: reflect.StructTag(`ferry:"v"`)},
	})
}

// x3Verdict classifies a compile result into the three outcomes that matter to
// this question.
type x3Verdict struct {
	label     string // compiles / REFUSED (field rule) / refused (other)
	fieldRule bool
	fields    []string // the exported fields the field rule named
	first     string   // the first line of the refusal, for the table
	full      error
}

func x3Compile(t reflect.Type) x3Verdict {
	o := defaultOpts()
	o.reg = NewRegistry()
	_, err := compileOnce(x3Holder(t), o)
	return x3Classify(err)
}

func x3Classify(err error) x3Verdict {
	if err == nil {
		return x3Verdict{label: "compiles"}
	}
	v := x3Verdict{full: err}
	for _, line := range strings.Split(err.Error(), "\n") {
		if strings.Contains(line, "carries no ferry tag") {
			v.fieldRule = true
			// "ferry: /v/IP: field IP carries no ferry tag: ..."
			if i := strings.Index(line, "field "); i >= 0 {
				rest := line[i+len("field "):]
				if j := strings.Index(rest, " "); j > 0 {
					v.fields = append(v.fields, rest[:j])
				}
			}
		}
		if v.first == "" {
			v.first = line
		}
	}
	sort.Strings(v.fields)
	if v.fieldRule {
		v.label = "REFUSED (ADR-0008 field rule)"
	} else {
		v.label = "refused (other)"
	}
	return v
}

// x3Row is one line of ADR-0005's own tables, with the ADR's published outcome
// beside the measured one.
type x3Row struct {
	name      string
	typ       reflect.Type
	published string // what ADR-0005 says
	source    string // which ADR table the published column comes from
}

func x3Population() []x3Row {
	return []x3Row{
		// ADR-0005 "There are three outcomes for a type, not two"
		{"net.Addr", reflect.TypeFor[net.Addr](), "refused, interface kind", "0005 three-outcomes"},
		{"netip.Addr", reflect.TypeFor[netip.Addr](), "refused, maps no address", "0005 three-outcomes"},
		{"netip.AddrPort", reflect.TypeFor[netip.AddrPort](), "refused, maps no address", "0005 three-outcomes"},
		{"netip.Prefix", reflect.TypeFor[netip.Prefix](), "refused, maps no address", "0005 three-outcomes"},
		{"big.Int", reflect.TypeFor[big.Int](), "refused, maps no address", "0005 three-outcomes"},
		{"url.URL", reflect.TypeFor[url.URL](), "refused, via nested url.Userinfo", "0005 three-outcomes"},
		{"net.IP", reflect.TypeFor[net.IP](), "ADMITTED, round-trips (bytes)", "0005 three-outcomes"},
		{"[16]byte UUID", reflect.TypeFor[[16]byte](), "ADMITTED, round-trips (16 raw bytes)", "0005 three-outcomes"},
		{"net.IPNet", reflect.TypeFor[net.IPNet](), "ADMITTED, round-trips (/IP and /Mask)", "0005 three-outcomes"},
		{"net.TCPAddr", reflect.TypeFor[net.TCPAddr](), "ADMITTED, round-trips (blob + number)", "0005 three-outcomes"},
		{"sql.NullString", reflect.TypeFor[sql.NullString](), "ADMITTED, round-trips (/String, /Valid)", "0005 three-outcomes"},
		{"json.RawMessage", reflect.TypeFor[json.RawMessage](), "ADMITTED, round-trips (bytes)", "0005 three-outcomes"},
		{"type Port int", reflect.TypeFor[x3Port](), "ADMITTED, round-trips (number)", "0005 three-outcomes"},
		{"time.Duration", reflect.TypeFor[time.Duration](), "admitted, pinned representation", "0005 three-outcomes"},
		{"time.Time", reflect.TypeFor[time.Time](), "admitted, pinned representation", "0005 three-outcomes"},

		// ADR-0005's maps-no-address section, which is where the fourth name is.
		{"time.Location", reflect.TypeFor[time.Location](), "refused, maps no address", "0005 maps-no-address"},

		// The neighbours of the three that broke. No ADR names these, and they
		// are here because "how wide is this" is the question and a sample is
		// not an answer: every one of them is the same shape as a named row.
		{"net.UDPAddr", reflect.TypeFor[net.UDPAddr](), "(unnamed; same shape as net.TCPAddr)", "-"},
		{"net.UnixAddr", reflect.TypeFor[net.UnixAddr](), "(unnamed; same shape as net.TCPAddr)", "-"},
		{"net.IPAddr", reflect.TypeFor[net.IPAddr](), "(unnamed; same shape as net.TCPAddr)", "-"},
		{"sql.NullInt64", reflect.TypeFor[sql.NullInt64](), "(unnamed; same shape as sql.NullString)", "-"},
		{"sql.NullBool", reflect.TypeFor[sql.NullBool](), "(unnamed; same shape as sql.NullString)", "-"},
		{"sql.NullFloat64", reflect.TypeFor[sql.NullFloat64](), "(unnamed; same shape as sql.NullString)", "-"},
		{"sql.NullTime", reflect.TypeFor[sql.NullTime](), "(unnamed; same shape as sql.NullString)", "-"},
		{"sql.Null[string]", reflect.TypeFor[sql.Null[string]](), "(unnamed; same shape as sql.NullString)", "-"},
		{"url.Userinfo", reflect.TypeFor[url.Userinfo](), "named as the reason url.URL is refused", "0005 three-outcomes"},
		{"tls.Config", reflect.TypeFor[x3TLSLike](), "(unnamed; the shape every config struct has)", "-"},
	}
}

type x3Port int

// x3TLSLike stands in for the large third-party config struct: exported fields,
// no ferry tags, nothing exotic in it. It is written here rather than importing
// crypto/tls so the row is about the RULE and not about one package's field list.
type x3TLSLike struct {
	ServerName string
	MinVersion uint16
	NextProtos []string
}

func runX3_1() {
	fmt.Println("ADR-0005, the three-outcomes table:")
	quoteX3(
		"| net.IPNet      | **admitted, round-trips** | `/IP` and `/Mask`, two byte blobs |",
		"| net.TCPAddr    | **admitted, round-trips** | a byte blob and a number          |",
		"| sql.NullString | **admitted, round-trips** | `/String` and `/Valid`            |",
		"| json.RawMessage| **admitted, round-trips** | bytes(\"{\\\"a\\\":1}\")               |",
	)
	fmt.Println("ADR-0008, the rule ADR-0005 never saw:")
	quoteX3(
		"An exported, named struct field with no ferry tag is a schema compile error.",
		"ferry never invents a segment name.",
	)

	fmt.Println(`Compiled as struct{ V T ` + "`" + `ferry:"v"` + "`" + ` }, against a FRESH registry.`)
	fmt.Printf("chainOrder=%v chainBeforeKind=%v keyOptIn=%v\n\n", chainOrder, chainBeforeKind, keyOptIn)

	fmt.Printf("  %-18s %-38s %s\n", "type", "ADR-0005 says", "the tip does")
	fmt.Printf("  %-18s %-38s %s\n", strings.Repeat("-", 18), strings.Repeat("-", 38), strings.Repeat("-", 30))

	var broke, refusedOther, ok []string
	for _, r := range x3Population() {
		v := x3Compile(r.typ)
		mark := ""
		if v.fieldRule {
			mark = fmt.Sprintf(" on %v", v.fields)
		}
		fmt.Printf("  %-18s %-38s %s%s\n", r.name, r.published, v.label, mark)
		switch {
		case v.fieldRule:
			broke = append(broke, r.name)
		case v.label == "compiles":
			ok = append(ok, r.name)
		default:
			refusedOther = append(refusedOther, r.name)
		}
	}

	fmt.Printf("\n  compiles                       (%2d): %s\n", len(ok), strings.Join(ok, " "))
	fmt.Printf("  refused, NOT by the field rule (%2d): %s\n", len(refusedOther), strings.Join(refusedOther, " "))
	fmt.Printf("  refused BY ADR-0008's field rule(%2d): %s\n", len(broke), strings.Join(broke, " "))

	fmt.Println("\n--- X3-1b: the three ADR-0005 calls ADMITTED, in full ---")
	fmt.Println("    Not \"it round-trips onto different addresses\". It does not compile.")
	for _, r := range []x3Row{
		{"net.IPNet", reflect.TypeFor[net.IPNet](), "", ""},
		{"net.TCPAddr", reflect.TypeFor[net.TCPAddr](), "", ""},
		{"sql.NullString", reflect.TypeFor[sql.NullString](), "", ""},
	} {
		fmt.Printf("\n    struct{ V %s "+"`"+`ferry:"v"`+"`"+" }\n", r.name)
		v := x3Compile(r.typ)
		for _, l := range strings.Split(v.full.Error(), "\n") {
			fmt.Printf("      %s\n", l)
		}
	}

	fmt.Println("\n--- X3-1c: why nobody can fix it at the call site ---")
	fmt.Println("    A ferry tag is a property of the FIELD, and these fields are declared")
	fmt.Println("    in net and database/sql. The remedies the diagnostic offers are")
	fmt.Println("    `name the segment` and `ferry:\"-\"`, and both are edits to a struct")
	fmt.Println("    definition in another module. Measured, for net.IPNet:")
	f, _ := reflect.TypeFor[net.IPNet]().FieldByName("IP")
	fmt.Printf("      field IP is declared in package %q, tag = %q\n",
		reflect.TypeFor[net.IPNet]().PkgPath(), string(f.Tag))
	fmt.Println("    The only lever on the ferry side is the one X3-2 measures.")

	fmt.Println("\n--- X3-1d: a second, milder finding on the same path ---")
	fmt.Println("    ADR-0005's published diagnosis for url.URL is `/V/User: url.Userinfo")
	fmt.Println("    maps no address`, which is the right diagnosis and names the nested")
	fmt.Println("    type the user did not choose. The tip cannot produce it: the field")
	fmt.Println("    rule fires on all eleven of url.URL's exported fields, User among")
	fmt.Println("    them, so url.Userinfo is never entered.")
	uv := x3Compile(reflect.TypeFor[url.URL]())
	fmt.Printf("      lines in the refusal: %d\n", len(strings.Split(uv.full.Error(), "\n")))
	fmt.Printf("      names url.Userinfo:   %v\n", strings.Contains(uv.full.Error(), "Userinfo"))
	fmt.Println("    Same shape as the audit's D13 and D15: a published measurement the")
	fmt.Println("    tip cannot reproduce. The outcome is unchanged - url.URL is refused")
	fmt.Println("    either way - so this is an evidence defect and not a behaviour one.")

	fmt.Println("\n--- X3-1e: and the root backstop fires on top of the field rule ---")
	fmt.Println("    ADR-0008's tier rule is that a level which already reported does not")
	fmt.Println("    also run the tier below it. e_schema.go suppresses the maps-no-address")
	fmt.Println("    backstop for the level that reported - but the field errors are")
	fmt.Println("    reported one level DOWN, so the parent still sees `minted == before`")
	fmt.Println("    with its own fieldErr false, and adds a refusal that is true but")
	fmt.Println("    misleading:")
	iv := x3Compile(reflect.TypeFor[net.IPNet]())
	for _, l := range strings.Split(iv.full.Error(), "\n") {
		if strings.Contains(l, "maps no address") {
			fmt.Printf("      %s\n", l)
		}
	}
	fmt.Println("    `register a codec for it` is the correct remedy, so the misleading")
	fmt.Println("    line happens to carry the right advice. That is luck, not design.")

	fmt.Println("\n--- X3-1f: and the luck runs out as soon as the struct has a sibling ---")
	fmt.Println("    The only remedy that WORKS for a third-party struct is `register a")
	fmt.Println("    codec`, and it appears solely on the maps-no-address line - which")
	fmt.Println("    fires only when the WHOLE parent maps nothing. Add one ordinary")
	fmt.Println("    field beside it and the advice disappears:")
	one := x3Compile(reflect.TypeFor[net.IPNet]())
	fmt.Println("\n      struct{ V net.IPNet `ferry:\"v\"` }")
	for _, l := range strings.Split(one.full.Error(), "\n") {
		fmt.Printf("        %s\n", l)
	}
	o := defaultOpts()
	o.reg = NewRegistry()
	_, two := compileOnce(reflect.TypeFor[x3Sibling](), o)
	fmt.Println("\n      struct{ N net.IPNet `ferry:\"n\"`; Host string `ferry:\"host\"` }  <- one field added")
	for _, l := range strings.Split(two.Error(), "\n") {
		fmt.Printf("        %s\n", l)
	}
	fmt.Printf("\n      mentions `register a codec`: one field %v, with a sibling %v\n",
		strings.Contains(one.full.Error(), "register a codec"),
		strings.Contains(two.Error(), "register a codec"))
	fmt.Println("    Both remedies the surviving message offers - name the segment, or")
	fmt.Println("    ferry:\"-\" - are edits to net's own struct definition, so in the")
	fmt.Println("    common shape the user is told only things they cannot do. The")
	fmt.Println("    mechanism that rescues them (X3-2) is never named.")
	fmt.Println("    Reported, not fixed: that sentence is ADR-0008's own published text")
	fmt.Println("    and changing it moves a published measurement.")
}

// x3Sibling is the shape the diagnostic gets wrong: a refusing third-party
// struct next to an ordinary field, which is what a real config struct looks
// like.
type x3Sibling struct {
	N    net.IPNet `ferry:"n"`
	Host string    `ferry:"host"`
}
