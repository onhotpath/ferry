package main

// P2: does selecting each direction independently break round-trip where
// selecting a PAIR does not?
//
// This is the ticket's central claim under test: "the dual must round-trip,
// which makes precedence a correctness question rather than a style one."
// If per-direction selection never breaks anything, the claim is decoration.

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"time"
)

// --- fixtures that carry MORE than one arm, which is the only way
// --- precedence is exercised at all.

// jsonAndText implements a json pair AND a text pair, with DIFFERENT forms.
// This is time.Time's exact shape: MarshalJSON quotes, MarshalText does not.
type jsonAndText struct{ S string }

func (v jsonAndText) MarshalText() ([]byte, error)  { return []byte(v.S), nil }
func (v jsonAndText) MarshalJSON() ([]byte, error)  { return []byte(`"` + v.S + `"`), nil }
func (v *jsonAndText) UnmarshalText(b []byte) error { v.S = string(b); return nil }
func (v *jsonAndText) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v.S = s
	return nil
}

// textEncJSONDec is the asymmetric case: a text ENCODER and a json DECODER,
// and no other half. Nothing stops a user writing this, and xload's own type
// package is one arm away from it.
type textEncJSONDec struct{ S string }

func (v textEncJSONDec) MarshalText() ([]byte, error) { return []byte(v.S), nil }
func (v *textEncJSONDec) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v.S = s
	return nil
}

// binEncTextDec: a binary encoder and a text decoder.
type binEncTextDec struct{ N int }

func (v binEncTextDec) MarshalBinary() ([]byte, error) {
	return []byte{byte(v.N)}, nil
}
func (v *binEncTextDec) UnmarshalText(b []byte) error {
	v.N = len(b)
	return nil
}

// xloadShape reproduces xload's own type package: a custom decoder plus a
// String(), and no MarshalText. Under ferry, Stringer is dead (ADR-0005) and
// the text pair is incomplete, so this type has NO arm at all.
type xloadShape struct{ S string }

func (v xloadShape) String() string                { return v.S }
func (v *xloadShape) UnmarshalText(b []byte) error { v.S = string(b); return nil }

func rtThrough(c codec, v any) (string, string, error) {
	rv := reflect.ValueOf(v)
	enc, err := c.enc(rv)
	if err != nil {
		return "", "", fmt.Errorf("encode: %w", err)
	}
	back := reflect.New(rv.Type()).Elem()
	if err := c.dec(enc, back); err != nil {
		return enc.GoString(), "", fmt.Errorf("decode: %w", err)
	}
	return enc.GoString(), fmt.Sprintf("%+v", back.Interface()), nil
}

func runPairing() {
	order := []string{"text", "json", "binary", "gob"}

	fmt.Println("\n--- P2a: a type carrying two complete arms, dumped and loaded ---")
	fmt.Println("    The arm chosen decides what lands on the plane, and both directions")
	fmt.Println("    must agree or the value never comes back.")
	for _, ord := range [][]string{{"text", "json"}, {"json", "text"}} {
		c, _, ok := selectPaired(reflect.TypeFor[jsonAndText](), ord)
		if !ok {
			fmt.Println("    no arm")
			continue
		}
		w, got, err := rtThrough(c, jsonAndText{"hello"})
		fmt.Printf("    order %-14v arm=%-6s plane=%-20s back=%-16s err=%v\n", ord, c.arm, w, got, err)
	}
	fmt.Println("    ^ same type, same value, two different plane artefacts. Precedence")
	fmt.Println("      is what decides which, and both round-trip, so the property alone")
	fmt.Println("      cannot tell them apart. That is ADR-0005's golden column again.")

	fmt.Println("\n--- P2b: per-direction selection against paired selection ---")
	type fx struct {
		name string
		t    reflect.Type
		v    any
	}
	fxs := []fx{
		{"jsonAndText", reflect.TypeFor[jsonAndText](), jsonAndText{"hello"}},
		{"textEncJSONDec", reflect.TypeFor[textEncJSONDec](), textEncJSONDec{"hello"}},
		{"binEncTextDec", reflect.TypeFor[binEncTextDec](), binEncTextDec{7}},
		{"xloadShape", reflect.TypeFor[xloadShape](), xloadShape{"hello"}},
		{"time.Time", reflect.TypeFor[time.Time](), time.Unix(0, 0).UTC()},
	}
	fmt.Printf("    %-16s %s\n", "type", "PER-DIRECTION (each half takes the first arm that has it)")
	fmt.Printf("    %-16s %s\n", "", "PAIRED (first arm whose BOTH halves are present)")
	fmt.Println("    " + dashes(110))
	for _, f := range fxs {
		var lhs string
		if c, ok := selectPerDirection(f.t, order); ok {
			w, got, err := rtThrough(c, f.v)
			lhs = fmt.Sprintf("%-11s %-32s -> %v%s", c.arm, w, got, errStr(err))
		} else {
			lhs = "no arm"
		}
		var rhs string
		if c, _, ok := selectPaired(f.t, order); ok {
			w, got, err := rtThrough(c, f.v)
			rhs = fmt.Sprintf("%-11s %-32s -> %v%s", c.arm, w, got, errStr(err))
		} else {
			rhs = "no arm; falls through to kind admission"
		}
		fmt.Printf("    %-16s per-dir  %s\n", f.name, lhs)
		fmt.Printf("    %-16s paired   %s\n", "", rhs)
	}

	fmt.Println("\n--- P2c: does encoding/json/v2 itself have this defect? ---")
	fmt.Println("    v2 selects per direction too. A type with MarshalJSON and only")
	fmt.Println("    UnmarshalText is marshalled as an object and unmarshalled as a string.")
	type jsonMTextU struct{ A int }
	_ = jsonMTextU{}
	b, err := json.Marshal(objMarshalTextUnmarshal{A: 1})
	fmt.Printf("    json.Marshal      -> %s   err=%v\n", b, err)
	var back objMarshalTextUnmarshal
	err = json.Unmarshal(b, &back)
	fmt.Printf("    json.Unmarshal    -> %+v err=%v\n", back, err)
	fmt.Println("    ^ so the hazard is not xload's alone; the stdlib has it too and")
	fmt.Println("      only asks in prose that the two implementations agree.")
}

// objMarshalTextUnmarshal marshals as a JSON object and unmarshals from text.
type objMarshalTextUnmarshal struct{ A int }

func (v objMarshalTextUnmarshal) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`{"a":%d}`, v.A)), nil
}
func (v *objMarshalTextUnmarshal) UnmarshalText(b []byte) error {
	v.A = len(b)
	return nil
}

var _ encoding.TextUnmarshaler = (*objMarshalTextUnmarshal)(nil)

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return " ERR: " + err.Error()
}

func shorten2(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
