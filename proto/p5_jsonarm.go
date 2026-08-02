package main

// P5: what recognising the JSON arm would actually put on a ferry plane.
//
// The adoption argument for it is real ("my type already works with
// encoding/json"). The census says the arm rescues almost nothing, so the
// question is what it costs where it DOES fire.

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"reflect"
	"time"

	"github.com/shopspring/decimal"
)

// objJSON is the case ferry's value model has no arm for: a JSON marshaler
// whose output is a structured document, not a scalar.
type objJSON struct {
	A int
	B []string
}

func (v objJSON) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		A int      `json:"a"`
		B []string `json:"b"`
	}{v.A, v.B})
}
func (v *objJSON) UnmarshalJSON(b []byte) error {
	var t struct {
		A int      `json:"a"`
		B []string `json:"b"`
	}
	if err := json.Unmarshal(b, &t); err != nil {
		return err
	}
	v.A, v.B = t.A, t.B
	return nil
}

func runJSONArm() {
	fmt.Println("\n--- P5a: text arm against json arm, same types, same values ---")
	fmt.Printf("    %-18s %-32s %s\n", "type", "text arm", "json arm")
	fmt.Println("    " + dashes(90))
	for _, r := range []struct {
		name string
		t    reflect.Type
		v    any
	}{
		{"time.Time", reflect.TypeFor[time.Time](), time.Unix(0, 0).UTC()},
		{"big.Int", reflect.TypeFor[big.Int](), *big.NewInt(1 << 40)},
		{"slog.Level", reflect.TypeFor[slog.Level](), slog.LevelWarn},
		{"decimal.Decimal", reflect.TypeFor[decimal.Decimal](), decimal.RequireFromString("1.25")},
		{"objJSON", reflect.TypeFor[objJSON](), objJSON{1, []string{"x", "y"}}},
	} {
		var lhs, rhs string
		if c, ok, _ := textCodecFor(r.t); ok {
			w, _, err := rtThrough(c, r.v)
			lhs = w + errStr(err)
		} else {
			lhs = "(no text pair)"
		}
		if c, ok, _ := jsonCodecFor(r.t); ok {
			w, _, err := rtThrough(c, r.v)
			rhs = w + errStr(err)
		} else {
			rhs = "(no json pair)"
		}
		fmt.Printf("    %-18s %-32s %s\n", r.name, shorten2(lhs, 32), rhs)
	}
	fmt.Println("    ^ every json form is the text form wrapped in JSON syntax, and the")
	fmt.Println("      quotes are then literal bytes in the plane. On env that is")
	fmt.Println("      LEVEL=\"WARN\" including the quote characters.")

	fmt.Println("\n--- P5b: the json arm on a real YAML file ---")
	type jconf struct {
		Level slog.Level      `ferry:"level"`
		Money decimal.Decimal `ferry:"money"`
	}
	for _, ord := range [][]string{{"text"}, {"json"}} {
		chainOrder, chainBeforeKind = ord, true
		fmt.Printf("\n    arm %v:\n", ord)
		for _, l := range splitLines(p4yaml(jconf{slog.LevelWarn, decimal.RequireFromString("1.25")})) {
			fmt.Printf("      %s\n", l)
		}
	}
	chainOrder, chainBeforeKind = nil, false

	fmt.Println("\n--- P5c: a JSON marshaler whose output is not a scalar ---")
	chainOrder, chainBeforeKind = []string{"json"}, true
	addrs, err := compile(reflect.TypeFor[struct{ V objJSON }]())
	d, derr := dump(reflect.ValueOf(struct{ V objJSON }{objJSON{1, []string{"x", "y"}}}))
	fmt.Printf("    addresses %v err=%v\n", addrs, err)
	fmt.Printf("    plane     %s err=%v\n", fmtVals(d), derr)
	chainOrder, chainBeforeKind = nil, false
	addrs2, _ := compile(reflect.TypeFor[struct{ V objJSON }]())
	d2, _ := dump(reflect.ValueOf(struct{ V objJSON }{objJSON{1, []string{"x", "y"}}}))
	fmt.Printf("    kind      %v -> %s\n", addrs2, fmtVals(d2))
	fmt.Println("    ^ the json arm collapses a structured value into ONE opaque address.")
	fmt.Println("      ADR-0004 removed the group arm and ADR-0005 says opaque capture of")
	fmt.Println("      a structured subtree is not wanted; the json arm reintroduces it")
	fmt.Println("      by the back door, and the plane can no longer address /V/B#0.")
}
