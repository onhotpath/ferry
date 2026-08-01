package main

// P1: price jsontext.Token three ways, as the ticket's second comment asks.
// Adopt it, mirror its shape, or reject it with a stated reason.
//
// The attractive facts are real: it is a raw-text-plus-kind union with
// (T, error) accessors and it imports no reflect. This probe checks the facts
// that decide whether it can *be* ferry's Value rather than resemble one.

import (
	"encoding/json/jsontext"
	"fmt"
	"strconv"
	"strings"
	"unsafe"
)

func p1Token() {
	head("P1  jsontext.Token as ferry's value model: adopt, mirror, or reject")

	// (a) What kinds exist, and what a plane needs that they do not cover.
	fmt.Println("    (a) kind coverage")
	toks := []struct {
		what string
		t    jsontext.Token
	}{
		{"null", jsontext.Null},
		{"bool", jsontext.Bool(true)},
		{"number", jsontext.Float(3.5)},
		{"string", jsontext.String("8080")},
		{"begin array", jsontext.BeginArray},
		{"end array", jsontext.EndArray},
	}
	for _, tc := range toks {
		fmt.Printf("        %-12s Kind=%q String=%q\n", tc.what, tc.t.Kind(), tc.t.String())
	}
	fmt.Println("        no bytes kind: JSON has no binary scalar, and a Consul or")
	fmt.Println("        Registry plane has nothing else to hand over")

	// (b) Is it a value or a stream token? This is the decisive question.
	fmt.Println("\n    (b) value or stream token")
	fmt.Printf("        BeginArray.Kind()=%q EndArray.Kind()=%q\n",
		jsontext.BeginArray.Kind(), jsontext.EndArray.Kind())
	fmt.Println("        '[' and ']' are Tokens, so the type's domain is stream")
	fmt.Println("        positions, not values. A ferry Value must never be able to")
	fmt.Println("        hold 'the start of something'.")

	// (c) Lifetime. A Token read from a Decoder is invalidated by the next
	//     read, which is why Clone exists. That is a hazard a value handed to
	//     third-party driver code must not carry.
	fmt.Println("\n    (c) lifetime")
	dec := jsontext.NewDecoder(strings.NewReader(`["first","second"]`))
	_, _ = dec.ReadToken() // [
	first, _ := dec.ReadToken()
	kept := first // no Clone
	cloned := first.Clone()
	_, _ = dec.ReadToken() // "second" - this voids first
	fmt.Printf("        held with Clone    : %q\n", cloned.String())
	fmt.Printf("        held without Clone : %s\n", mustRecover(func() string { return kept.String() }))
	fmt.Println("        It does not degrade, it panics. ferry's Value is handed to")
	fmt.Println("        third-party driver code and stored in maps by the engine, and")
	fmt.Println("        section 4 already ruled that accessors must not panic. A")
	fmt.Println("        value model with a use-after-read panic is disqualifying.")

	// (d) Comparability and size, against the candidate.
	fmt.Println("\n    (d) shape")
	var jt jsontext.Token
	var fv Value
	fmt.Printf("        sizeof(jsontext.Token)=%d  sizeof(candidate Value)=%d\n",
		unsafe.Sizeof(jt), unsafe.Sizeof(fv))
	fmt.Printf("        candidate Value comparable: %v (String(\"a\")==String(\"a\") -> %v)\n",
		true, String("a") == String("a"))
	fmt.Printf("        candidate distinguishes quoted from unquoted 8080: %v\n",
		String("8080") != Number("8080"))

	// (e) The rule from ADR-0002, restated so the pricing is honest.
	fmt.Println("\n    (e) ADR-0002")
	fmt.Println("        core imports only unconditionally-available stdlib, and")
	fmt.Println("        jsontext vanishes under GOEXPERIMENT=nojsonv2. Adopting it")
	fmt.Println("        into core is an amendment to ADR-0002, not a free choice.")
	fmt.Println("        Mirroring its shape costs nothing and is what (a)-(d) argue for.")
}

func mustRecover(f func() string) (out string) {
	defer func() {
		if r := recover(); r != nil {
			out = fmt.Sprintf("PANIC: %v", r)
		}
	}()
	return strconv.Quote(f())
}
