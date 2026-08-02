package main

// #11 P1..P3: what reflect.StructTag actually does to a ferry tag, and what
// ferry can see that StructTag.Get cannot.
//
// #8 measured one instance of this (an embedded double quote truncates
// silently). These probes ask the general question, because the grammar's
// escape mechanism has to live inside whatever survives.

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Each case is the RAW struct tag text as it would appear between backquotes
// in a Go source file. That is the only form the question makes sense in.
var tagCases = []struct {
	what string
	raw  string
}{
	{"plain", `ferry:"host"`},
	{"option", `ferry:"host,required"`},
	{"embedded quote, #8's case", `ferry:"origins,default=["value"]"`},
	{"escaped quote", `ferry:"origins,default=[\"value\"]"`},
	{"backslash-comma, the naive escape", `ferry:"a\,b"`},
	{"double-backslash-comma", `ferry:"a\\,b"`},
	{"lone trailing backslash", `ferry:"a\"`},
	{"tab escape", `ferry:"a\tb"`},
	{"tilde", `ferry:"a~,b"`},
	{"percent", `ferry:"a%2Cb"`},
	{"single quotes", `ferry:"default='a,b'"`},
	{"space inside", `ferry:"a b,default=Hello, world"`},
	{"duplicate ferry key", `ferry:"first" ferry:"second"`},
	{"no ferry key", `json:"host"`},
}

// A second key is present on every case so we can ask the question that
// decides the escape model: does a broken ferry tag break OTHER libraries'
// tags on the same field?
func withNeighbour(raw string) string { return raw + ` json:"jname" yaml:"yname"` }

func init() { t11Hooks = append(t11Hooks, runTagMech) }

func runTagMech() {
	hdr("T1  what reflect.StructTag.Get returns for a ferry tag")
	fmt.Printf("  %-34s %-24s %s\n", "raw tag", "Get(\"ferry\")", "Lookup ok")
	for _, c := range tagCases {
		st := reflect.StructTag(c.raw)
		got, ok := st.Lookup("ferry")
		fmt.Printf("  %-34s %-24q %v   [%s]\n", c.raw, got, ok, c.what)
	}

	hdr("T2  does a broken ferry tag break the NEIGHBOURING keys on the same field")
	fmt.Printf("  %-34s %-14s %-10s %s\n", "raw ferry part", "ferry", "json", "yaml")
	for _, c := range tagCases {
		st := reflect.StructTag(withNeighbour(c.raw))
		f, _ := st.Lookup("ferry")
		j, _ := st.Lookup("json")
		y, _ := st.Lookup("yaml")
		flag := ""
		if j != "jname" || y != "yname" {
			flag = "   <-- COLLATERAL DAMAGE"
		}
		fmt.Printf("  %-34s %-14q %-10q %q%s\n", c.raw, f, j, y, flag)
	}

	hdr("T3  ferry scanning the raw tag itself, instead of calling Get")
	fmt.Printf("  %-34s %s\n", "raw tag", "ferry's own diagnosis")
	for _, c := range tagCases {
		v, err := rawFerryTag(reflect.StructTag(withNeighbour(c.raw)))
		if err != nil {
			fmt.Printf("  %-34s ERROR: %v\n", c.raw, err)
			continue
		}
		if v == nil {
			fmt.Printf("  %-34s (no ferry key)\n", c.raw)
			continue
		}
		fmt.Printf("  %-34s value=%q\n", c.raw, *v)
	}
}

// rawFerryTag is the scanner ferry would ship instead of StructTag.Get.
//
// It walks the conventional `key:"value"` format itself, so that the three
// things Get answers with a silent "" - a malformed value, a value whose Go
// escape is invalid, and a duplicate key - each become a diagnosis instead.
// Returns (nil, nil) when the field genuinely carries no ferry tag.
//
// The scanning loop below is reflect.StructTag.Lookup's, with the error paths
// kept rather than collapsed into `break`.
// tagKeyName is the struct tag key ferry reads. It is an Option, defaulting
// to "ferry": the key says WHERE to look and never what the content means.
var tagKeyName = "ferry"

// ValidTagKey refuses a key that could never appear in a conventional struct
// tag, at the point the Option is supplied rather than at schema compile.
func ValidTagKey(k string) error {
	if k == "" {
		return fmt.Errorf("the ferry tag key may not be empty")
	}
	for i := 0; i < len(k); i++ {
		if c := k[i]; c <= ' ' || c == ':' || c == '"' || c == 0x7f {
			return fmt.Errorf("the ferry tag key %q contains %q, which cannot appear in a struct tag key", k, string(c))
		}
	}
	return nil
}

func rawFerryTag(tag reflect.StructTag) (*string, error) {
	key := tagKeyName
	var found *string
	t := string(tag)
	for t != "" {
		// Skip leading space.
		i := 0
		for i < len(t) && t[i] == ' ' {
			i++
		}
		t = t[i:]
		if t == "" {
			break
		}
		// Scan to colon.
		i = 0
		for i < len(t) && t[i] > ' ' && t[i] != ':' && t[i] != '"' && t[i] != 0x7f {
			i++
		}
		if i == 0 || i+1 >= len(t) || t[i] != ':' || t[i+1] != '"' {
			hint := ""
			if strings.Contains(string(tag), key+`:"`) {
				hint = "; the usual cause is a bare double quote inside a "+key+" tag, which a struct tag value cannot contain"
			}
			return nil, fmt.Errorf("struct tag is not in the conventional `key:\"value\"` form, at %q%s", trunc(t), hint)
		}
		name := t[:i]
		t = t[i+1:]

		// Scan quoted string to find the value.
		i = 1
		for i < len(t) && t[i] != '"' {
			if t[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(t) {
			return nil, fmt.Errorf("struct tag key %q has an unterminated quoted value", name)
		}
		qvalue := t[:i+1]
		t = t[i+1:]

		if name != key {
			continue
		}
		value, err := strconv.Unquote(qvalue)
		if err != nil {
			return nil, fmt.Errorf(key+" tag value %s is not a valid Go quoted string (%v); "+
				"a struct tag value is unquoted by strconv.Unquote, so it may not contain a bare "+
				"double quote and may not contain an escape Go does not define", qvalue, err)
		}
		if found != nil {
			return nil, fmt.Errorf("the field carries two %s tags, %q and %q; reflect.StructTag.Get returns the first and go vet does not check it", key, *found, value)
		}
		v := value
		found = &v
	}
	return found, nil
}

func trunc(s string) string {
	if len(s) > 24 {
		return s[:24] + "..."
	}
	return s
}

// A struct whose tags are the cases above, so `go vet` has something real to
// look at. T4 runs vet against this file's own package.
type vetSubject struct {
	A string `ferry:"origins,default=["value"]"`
	B string `ferry:"a\,b"`
	C string `ferry:"ok"`
	D string `ferry:"first" ferry:"second"`
}

func vetSubjectNames() string {
	t := reflect.TypeFor[vetSubject]()
	var b strings.Builder
	for i := range t.NumField() {
		fmt.Fprintf(&b, "%s=%q ", t.Field(i).Name, t.Field(i).Tag.Get("ferry"))
	}
	return b.String()
}
