package main

// X5: D9, core scans the raw struct tag itself.
// X6: D17, three diagnostic tiers, edit distance, and the neighbourhood.

import (
	"fmt"
	"reflect"
	"strings"
)

// The three failure modes ADR-0008 measured, each written as a real struct tag
// with a neighbour so the collateral damage is visible too.
var x1TagCases = []struct {
	what string
	raw  string
}{
	{"a bare double quote", `ferry:"origins,default=["value"]" json:"jname" yaml:"yname"`},
	{"an invalid Go escape", `ferry:"a\,b" json:"jname" yaml:"yname"`},
	{"two ferry tags", `ferry:"first" ferry:"second" json:"jname" yaml:"yname"`},
	{"a clean tag", `ferry:"host,required" json:"jname" yaml:"yname"`},
	{"no ferry tag at all", `json:"jname" yaml:"yname"`},
	{"a malformed JSON tag, ferry clean", `ferry:"host" json:"a"b" yaml:"yname"`},
	{"a malformed JSON tag, no ferry tag", `json:"a"b" yaml:"yname"`},
}

func runX1_5() {
	quoteADR(
		"ADR-0008: \"Core does not call reflect.StructTag.Get or Lookup. It scans",
		"reflect.StructField.Tag with its own parser and reports what Get answers",
		"with a silent empty string.\"")

	fmt.Printf("  %-38s %-26s %-8s %s\n", "raw tag", "Lookup(\"ferry\")", "ok", "json on the same field")
	for _, c := range x1TagCases {
		st := reflect.StructTag(c.raw)
		got, ok := st.Lookup("ferry")
		j, _ := st.Lookup("json")
		flag := ""
		if j != "jname" {
			flag = "  <-- COLLATERAL DAMAGE"
		}
		fmt.Printf("  %-38s %-26q %-8v %q%s\n", trimTo(c.raw, 36), got, ok, j, flag)
	}

	fmt.Println("\n  ferry scanning the raw tag itself:")
	for _, c := range x1TagCases {
		v, err := rawFerryTag(reflect.StructTag(c.raw), "ferry")
		switch {
		case err != nil:
			fmt.Printf("  %-38s REFUSED %s\n", trimTo(c.raw, 36), err)
		case v == nil:
			fmt.Printf("  %-38s (the field genuinely carries no ferry tag)\n", trimTo(c.raw, 36))
		default:
			fmt.Printf("  %-38s value=%q\n", trimTo(c.raw, 36), *v)
		}
	}
	fmt.Println("\n  The last two rows are ADR-0008's scoping rule: \"ferry refuses a struct")
	fmt.Println("  tag that does not parse only when the text `ferry:\"` occurs in it. A")
	fmt.Println("  field whose json tag is malformed and whose ferry tag was read cleanly")
	fmt.Println("  is go vet's problem and not ferry's.\"")

	fmt.Println("\n  and the same three through Compile, which is where a user meets them:")
	for _, tc := range []struct {
		label string
		t     reflect.Type
	}{
		{"a bare double quote", reflect.TypeFor[x1BareQuote]()},
		{"an invalid Go escape", reflect.TypeFor[x1BadEscape]()},
		{"two ferry tags", reflect.TypeFor[x1TwoTags]()},
	} {
		_, err := compileSchema2(tc.t, defaultOpts())
		fmt.Printf("    %-22s %s\n", tc.label, oneLine(err))
	}
	fmt.Println("\n    The middle row is the sharpest, and A41=9 measured the before:")
	fmt.Println("      Lookup=\"\" ok=false  ->  `field H carries no ferry tag`")
	fmt.Println("    a tag ferry cannot read reported as a tag the user did not write,")
	fmt.Println("    with the remedy being to write the tag they already wrote.")
}

type x1BareQuote struct {
	H string `ferry:"origins,default=["value"]" json:"jname"`
}

type x1BadEscape struct {
	H string `ferry:"a\,b" json:"jname"`
}

type x1TwoTags struct {
	H string `ferry:"first" ferry:"second"`
}

// --- X6 ----------------------------------------------------------------------

// The three tiers, one fixture each, exactly as ADR-0008 measures them.
type x1Tier1 struct {
	H string `ferry:"h,requird"`
}

type x1Tier2 struct {
	O []string `ferry:"o,required,default=v"`
}

type x1Tier3 struct {
	S string `ferry:"s,required,default=x"`
}

type x1AllFour struct {
	H string   `ferry:"h,requird"`
	O []string `ferry:"o,required,default=v"`
	S string   `ferry:"s,required,default=x"`
	K string   `ferry:"k"`
}

func runX1_6() {
	quoteADR(
		"ADR-0008: \"1. Well-formedness. The raw tag scans and the grammar parses.",
		"2. Admissibility. Is each option legal at this field's type, on its own?",
		"3. Contradiction. Do two options that both survived tier 2 conflict?",
		"A tier fires only for a field that cleared the tier above it.\"")

	for _, tc := range []struct {
		label string
		t     reflect.Type
	}{
		{"tier 1  `ferry:\"h,requird\"`", reflect.TypeFor[x1Tier1]()},
		{"tier 2  `ferry:\"o,required,default=v\"` on []string", reflect.TypeFor[x1Tier2]()},
		{"tier 3  `ferry:\"s,required,default=x\"`", reflect.TypeFor[x1Tier3]()},
		{"four fields, one of each", reflect.TypeFor[x1AllFour]()},
	} {
		_, err := compileSchema2(tc.t, defaultOpts())
		lines := splitLines(errText2(err))
		fmt.Printf("    %-48s %d error(s)\n", tc.label, count(err))
		for _, l := range lines {
			fmt.Printf("        %s\n", l)
		}
	}
	fmt.Println("    ^ ADR-0008's own measured column is 1, 2, 1, 3. Tier 2 reports both")
	fmt.Println("      inadmissible options and does NOT go on to report the contradiction")
	fmt.Println("      between them, which is ADR-0006's rule; the tier above them is the")
	fmt.Println("      one the tip did not have.")

	fmt.Println("\n  the 26 mistakes ADR-0008 measured, and the remedy each gets:")
	specific := 0
	for _, tok := range []string{
		// near misses
		"requird", "reqired", "requires", "Required", "require",
		"defualt=x", "deafult=x", "defaults=x", "DEFAULT=x",
		"omitzeroo", "omitzer", "OmitZero", "omit_zero",
		// the neighbourhood
		"omitempty", "inline", "embed", "squash", "prefix",
		"delimiter", "separator", "string", "flow",
		// not near anything
		"req", "r", "asString", "xyzzy",
	} {
		_, errs := parseFerryTag("h,"+tok, "ferry")
		msg := "(accepted)"
		if len(errs) > 0 {
			msg = errs[0].Error()
		}
		kind := "generic"
		switch {
		case strings.HasPrefix(msg, "has invalid appearance"):
			kind = "near miss"
			specific++
		case strings.Contains(msg, "ferry has no "):
			kind = "neighbourhood"
			specific++
		case msg == "(accepted)":
			kind = "accepted"
		}
		fmt.Printf("    %-12s %-14s %s\n", tok, kind, trimTo(msg, 76))
	}
	fmt.Printf("\n    %d of 26 got a specific remedy. ADR-0008's number is 22, and the four\n", specific)
	fmt.Println("    that do not are req, r, asString and xyzzy, which are not near anything")
	fmt.Println("    and correctly get the message naming the whole vocabulary.")

	fmt.Println("\n  and whitespace is its own diagnosis, because ferry does not trim:")
	for _, v := range []string{"h, required", "h ,required", "h,required "} {
		_, errs := parseFerryTag(v, "ferry")
		if len(errs) == 0 {
			fmt.Printf("    %-16q accepted\n", v)
			continue
		}
		fmt.Printf("    %-16q %s\n", v, errs[0])
	}
	fmt.Println("    A41=16 measured the before as `unknown option \" required\"`.")

	fmt.Println("\n  and only a LEADING quote is significant, which is the half of")
	fmt.Println("  ADR-0008 that proto/11's own splitter does not implement:")
	fmt.Printf("    %-34s %-22s %s\n", "tag", "name", "options seen")
	for _, v := range []string{
		"it's,required",
		"it's",
		"home,default=it's here",
		"greeting,default='Hello, world'",
		"brokers,default='h1:9092,h2:9092'",
		"'a,b',required",
		"'a''b'",
	} {
		d, errs := parseFerryTag(v, "ferry")
		var opts []string
		if d.required {
			opts = append(opts, "required")
		}
		if d.omitzero {
			opts = append(opts, "omitzero")
		}
		if d.hasDefault {
			opts = append(opts, "default="+d.def)
		}
		note := fmt.Sprintf("%v", opts)
		if len(errs) > 0 {
			note = "REFUSED: " + trimTo(errs[0].Error(), 46)
		}
		fmt.Printf("    %-34q %-22q %s\n", v, d.name, note)
	}
	fmt.Println("    proto/11's splitFieldsQ reads row 1 as ONE token named")
	fmt.Println("    \"it's,required\" and swallows the option with no diagnostic, which is")
	fmt.Println("    the failure ADR-0008 rejected the `,,` doubling model for, occurring")
	fmt.Println("    in the model it chose. The tip's splitTag is the decision and is now")
	fmt.Println("    the only splitter.")

	fmt.Println("\n  the tag key is an Option, and the vocabulary is not:")
	o := defaultOpts()
	o.tagKey = "json"
	_, errs := parseFerryTag("host,omitempty", o.tagKey)
	for _, e := range errs {
		fmt.Printf("    %s\n", e)
	}
}

func count(err error) int {
	if err == nil {
		return 0
	}
	return len(splitLines(err.Error()))
}

func errText2(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func oneLine(err error) string {
	if err == nil {
		return "<nil>"
	}
	return strings.ReplaceAll(err.Error(), "\n", " | ")
}
