package main

// What a composite carrying BOTH required and a default reports.

import (
	"fmt"
	"reflect"
)

type S1 struct {
	Origins []string `ferry:"origins,required,default=value"`
}

type S2 struct {
	Origins []string `ferry:"origins,default=value"`
}

type S3 struct {
	Origins []string `ferry:"origins,required"`
}

// S4 is what the bracket-and-quote spelling ACTUALLY does to a struct tag.
type S4 struct {
	Origins []string `ferry:"origins,default=["value"]"`
}

// S5 is the same intent written the only way a tag can carry it.
type S5 struct {
	Origins []string `ferry:"origins,default=[value]"`
}

func stack() {
	dhdr("S1  a composite carrying required AND a default")
	for _, c := range []struct {
		label string
		t     reflect.Type
	}{
		{"origins,required", reflect.TypeFor[S3]()},
		{"origins,default=value", reflect.TypeFor[S2]()},
		{"origins,required,default=value", reflect.TypeFor[S1]()},
	} {
		_, err := compileD(c.t)
		fmt.Printf("\n  %s\n", c.label)
		if err == nil {
			fmt.Println("     compiles")
			continue
		}
		for i, line := range splitJoined(err) {
			fmt.Printf("     %d. %s\n", i+1, line)
		}
	}

	dhdr("S2  what the tag itself does with a bracketed list")
	for _, c := range []struct {
		label string
		t     reflect.Type
	}{
		{`ferry:"origins,default=["value"]"`, reflect.TypeFor[S4]()},
		{`ferry:"origins,default=[value]"`, reflect.TypeFor[S5]()},
	} {
		f := c.t.Field(0)
		raw, ok := f.Tag.Lookup("ferry")
		fmt.Printf("  %-36s Lookup ok=%-5v value=%q\n", c.label, ok, raw)
	}
	fmt.Println("  The first spelling closes the tag at its own quote, so reflect reads")
	fmt.Println("  `origins,default=[` and the rest of the tag is gone with no diagnostic.")
	fmt.Println("  A struct tag's value is double-quoted and a Go raw string literal")
	fmt.Println("  cannot escape a double quote, so the JSON-ish list spelling is not")
	fmt.Println("  writable at all. That is on top of 5.10, not instead of it.")
}

func splitJoined(err error) []string {
	type unwrapper interface{ Unwrap() []error }
	if u, ok := err.(unwrapper); ok {
		var out []string
		for _, e := range u.Unwrap() {
			out = append(out, e.Error())
		}
		return out
	}
	return []string{err.Error()}
}

func stack2() {
	dhdr("S3  the diagnostic rule: admissibility first, contradictions second")
	type leafBoth struct {
		V string `ferry:"v,required,default=x"`
	}
	type leafBadDef struct {
		V int `ferry:"v,omitzero,default=abc"`
	}
	for _, c := range []struct {
		label string
		t     reflect.Type
	}{
		{"[]string  required,default=value", reflect.TypeFor[S1]()},
		{"string    required,default=x", reflect.TypeFor[leafBoth]()},
		{"int       omitzero,default=abc", reflect.TypeFor[leafBadDef]()},
	} {
		_, err := compileD(c.t)
		fmt.Printf("\n  %s\n", c.label)
		if err == nil {
			fmt.Println("     compiles")
			continue
		}
		for i, line := range splitJoined(err) {
			fmt.Printf("     %d. %s\n", i+1, line)
		}
	}
	fmt.Println("\n  Row 1 reports the two real mistakes and not the pairwise contradiction,")
	fmt.Println("  because neither option was admissible for a contradiction to be about.")
	fmt.Println("  Row 2 reports it, because both options ARE admissible on a string.")
	fmt.Println("  Row 3 reports only the bad default, not omitzero+default, because a")
	fmt.Println("  default that does not parse has no value to compare against zero.")
}
