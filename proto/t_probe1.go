package main

import (
	"fmt"
	"strings"
)

func init() { t11Hooks = append(t11Hooks, runT4to7) }

// T4: every diagnosis the grammar produces, over a corpus built so that a
// third of it is ill-formed. The handoff names the trap this exists to avoid:
// "a grammar fixture where every tag is well-formed, or where no segment text
// contains the grammar's own punctuation".
var grammarCases = []string{
	// well formed
	"host",
	"host,required",
	"host,default=localhost",
	"host,default=",
	"port,default=8080,omitzero",
	"-",
	"'-'",
	"'a,b'",
	"'a=b'",
	"'a''b'",
	"feature-flags",
	"db.host",
	"it's",
	"K,default=','",
	"greeting,default='Hello, world'",
	"greeting,default=it's here",
	"path,default=/etc/ferry",
	"home,default=~/.cache/app",
	"query,default=a=b",
	"brokers,default='h1:9092,h2:9092'",
	// ill formed
	"",
	",required",
	"-,required",
	"default=8080",
	"host,requird",
	"host,REQUIRED",
	"host, required",
	"host,omitEmpty",
	"host,omit_empty",
	"host,omitempty",
	"host,inline",
	"host,prefix=DB_",
	"host,nodump",
	"host,required,required",
	"host,,required",
	"host,default",
	"host,required=yes",
	"host,defualt=x",
	"'a,b",
	"host,default='abc",
	"host,default='abc'def",
}


func runT4to7() {
	hdr("T4  every diagnosis the grammar produces")
	for _, c := range grammarCases {
		d, errs := parseFerryTag(c)
		if len(errs) == 0 {
			fmt.Printf("  %-28s ok    %s\n", `ferry:"`+c+`"`, describe(d))
			continue
		}
		for i, e := range errs {
			lead := `ferry:"` + c + `"`
			if i > 0 {
				lead = ""
			}
			fmt.Printf("  %-28s REFUSED %v\n", lead, e)
		}
	}

	hdr("T5  near-miss: what a misspelling is told")
	misses := []string{"requird", "REQUIRED", "Required", "reqired", "required ", "omitzero ", "omitZero", "omit_zero", "OMITZERO", "defualt", "Default", "deafult", "omitzeroo", "req", "r", "omitempty", "inline", "squash", "prefix", "delimiter", "separator", "case", "flow", "remain", "asString", "xyzzy"}
	hit, missCount := 0, 0
	for _, m := range misses {
		_, errs := parseFerryTag("host," + m)
		msg := "ok (accepted!)"
		if len(errs) > 0 {
			msg = errs[0].Error()
		}
		kind := "  generic"
		switch {
		case strings.Contains(msg, "specify"):
			kind = "NEAR-MISS"
			hit++
		case strings.Contains(msg, "ferry has no"):
			kind = "  FOREIGN"
			hit++
		case strings.Contains(msg, "whitespace"):
			kind = "    SPACE"
			hit++
		default:
			missCount++
		}
		fmt.Printf("  %-12s %s  %s\n", m, kind, trimTo(msg, 96))
	}
	fmt.Printf("\n  %d of %d misspellings got a specific remedy; %d got only \"unknown option\"\n", hit, len(misses), missCount)

	hdr("T6  the three escape models against the same two inputs")
	fmt.Println("  the goal, from ADR-0003: a tag must be able to name a segment whose text")
	fmt.Println("  contains the grammar's own punctuation. 5.10's second half is that xload")
	fmt.Println("  cannot: its parseField splits on `,` so env:\"K,delimiter=,\" is unwritable.")
	fmt.Println()
	fmt.Printf("  %-22s %-26s %s\n", "model", "name `a,b`", "stray comma: `host,,required`")
	fmt.Printf("  %-22s %-26s %s\n", "bare or 'quoted'", show(escName("'a,b'")), show(escName("host,,required")))
	fmt.Printf("  %-22s %-26s %s\n", "doubling `,,`", show(dblName("a,,b")), show(dblName("host,,required")))
	fmt.Printf("  %-22s %-26s %s\n", "no escaping", show(noneName("a,b")), show(noneName("host,,required")))

	hdr("T7  5.10's second half, written out in each grammar")
	fmt.Println("  xload   env:\"K,delimiter=,\"        -> ", xloadParse("K,delimiter=,"))
	d, errs := parseFerryTag("K,default=','")
	fmt.Printf("  ferry   ferry:\"K,default=','\"      ->  name=%q default=%q errs=%d\n", d.name, d.defText, len(errs))
	d, errs = parseFerryTag("'a,b',required")
	fmt.Printf("  ferry   ferry:\"'a,b',required\"     ->  name=%q required=%v errs=%d\n", d.name, d.required, len(errs))
}

func describe(d tagDecl) string {
	if d.skip {
		return "not mapped"
	}
	var b []string
	b = append(b, fmt.Sprintf("name=%q", d.name))
	if d.required {
		b = append(b, "required")
	}
	if d.omitzero {
		b = append(b, "omitzero")
	}
	if d.hasDef {
		b = append(b, fmt.Sprintf("default=%q", d.defText))
	}
	return strings.Join(b, " ")
}

func trimTo(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

type escResult struct {
	name string
	opts []string
	err  string
}

func show(r escResult) string {
	if r.err != "" {
		return "REFUSED: " + r.err
	}
	return fmt.Sprintf("name=%q opts=%v", r.name, r.opts)
}

func escName(s string) escResult {
	d, errs := parseFerryTag(s)
	r := escResult{name: d.name}
	if d.required {
		r.opts = append(r.opts, "required")
	}
	if len(errs) > 0 {
		r.err = errs[0].Error()
	}
	return r
}

// dblName is the doubling model: `,,` is a literal comma and there is no
// escape character at all.
func dblName(s string) escResult {
	var fields []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == ',' && i+1 < len(s) && s[i+1] == ',' {
			cur.WriteByte(',')
			i++
			continue
		}
		if s[i] == ',' {
			fields = append(fields, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(s[i])
	}
	fields = append(fields, cur.String())
	return escResult{name: fields[0], opts: fields[1:]}
}

// noneName is the no-escaping model: split on every comma, and a name that
// needs one is simply unwritable.
func noneName(s string) escResult {
	f := strings.Split(s, ",")
	if len(f) > 1 && strings.Contains(s, ",") {
		return escResult{err: "the name `a,b` is unwritable: every comma separates"}
	}
	return escResult{name: f[0], opts: f[1:]}
}

// xloadParse is xload's parseField, reduced to the line that matters:
// strings.Split(tag, ",") at load.go:219.
func xloadParse(tag string) string {
	parts := strings.Split(tag, ",")
	return fmt.Sprintf("key=%q opts=%q  (the delimiter is now the empty string)", parts[0], parts[1:])
}
