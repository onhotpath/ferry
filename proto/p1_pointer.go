package main

// P1: ground truth on jsontext.Pointer. Verify the claims made in the ticket
// comment rather than repeating them: unique representation, Tokens(), and the
// index-versus-numeric-name ambiguity its own godoc admits.

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
)

func p1Pointer() {
	head("P1  jsontext.Pointer, measured on go1.27rc2")

	// (a) Does it round trip arbitrary segment text?
	fmt.Println("(a) segment text through AppendToken -> Tokens")
	for _, segs := range [][]string{
		{"db", "host"},
		{"a/b", "c"},   // separator inside a segment
		{"a~b", "c"},   // escape char inside a segment
		{"", "x"},      // empty segment
		{"a~1b"},       // text that looks like an escape
		{"Kéy"},   // non-ASCII
		{"a\nb"},       // control character
		{"servers", "0"}, // numeric-looking name
	} {
		var p jsontext.Pointer
		for _, s := range segs {
			p = p.AppendToken(s)
		}
		got := slices.Collect(p.Tokens())
		fmt.Printf("    %-22q -> %-20q -> %-22q valid=%v roundtrip=%v\n",
			segs, string(p), got, p.IsValid(), slices.Equal(segs, got))
	}

	// (b) The uniqueness claim: is it a property of the type, or only of
	//     pointers the package builds? What does a hand-written string do?
	fmt.Println("(b) uniqueness: hand-written vs constructed")
	built := jsontext.Pointer("").AppendToken("a/b")
	for _, raw := range []jsontext.Pointer{"/a~1b", "/a/b", "a~1b", "/a~1B", "//"} {
		fmt.Printf("    %-10q valid=%-5v equalToBuilt=%-5v tokens=%q\n",
			string(raw), raw.IsValid(), raw == built, slices.Collect(raw.Tokens()))
	}

	// (c) The admitted limitation: index versus numeric object name.
	fmt.Println("(c) array index vs numeric object name")
	type Elem struct {
		Host string `json:"host"`
	}
	type WithSlice struct {
		Servers []Elem `json:"servers"`
	}
	type WithMap struct {
		Servers map[string]Elem `json:"servers"`
	}
	seen := map[string][]string{}
	collect := func(label string, v any) {
		opts := json.WithMarshalers(json.MarshalToFunc(
			func(enc *jsontext.Encoder, s string) error {
				seen[label] = append(seen[label], string(enc.StackPointer()))
				return enc.WriteToken(jsontext.String(s))
			}))
		if _, err := json.Marshal(v, opts); err != nil {
			fmt.Println("    marshal error:", err)
		}
	}
	collect("slice", WithSlice{Servers: []Elem{{Host: "a"}}})
	collect("map", WithMap{Servers: map[string]Elem{"0": {Host: "a"}}})
	leafOf := func(ss []string) string {
		for _, s := range ss {
			if strings.HasSuffix(s, "/host") {
				return s
			}
		}
		return ""
	}
	fmt.Printf("    []Elem          all pointers %q, leaf %q\n", seen["slice"], leafOf(seen["slice"]))
	fmt.Printf("    map[string]Elem all pointers %q, leaf %q\n", seen["map"], leafOf(seen["map"]))
	fmt.Printf("    the two leaf pointers are identical: %v\n",
		leafOf(seen["slice"]) == leafOf(seen["map"]))

	// (d) Cost of decoding a pointer back to segments.
	fmt.Println("(d) Tokens() is a decode, not a field read")
	p := jsontext.Pointer("/db/servers/0/host")
	fmt.Printf("    Parent=%q LastToken=%q Contains(/db)=%v\n",
		string(p.Parent()), p.LastToken(), jsontext.Pointer("/db").Contains(p))
	fmt.Printf("    underlying kind: %v (a string newtype, so comparable and map-key usable)\n",
		strings.Contains(fmt.Sprintf("%T", p), "Pointer"))
}
