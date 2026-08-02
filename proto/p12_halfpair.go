package main

// P12: what happens to a type implementing one half of a pair.
//
// The ticket asks it as "a decoder but no matching encoder". There are three
// candidate answers - use the half anyway, fall through silently, or refuse -
// and the census decides which is affordable.

import (
	"fmt"
	"reflect"
)

// encOnly and decOnly are the two halves, on a type kind would otherwise
// admit, so falling through is a real alternative rather than a refusal in
// disguise.
type encOnly struct{ Name string }

func (v encOnly) MarshalText() ([]byte, error) { return []byte(v.Name), nil }

type decOnly struct{ Name string }

func (v *decOnly) UnmarshalText(b []byte) error { v.Name = string(b); return nil }

// halfCompile is the proposed rule: an incomplete pair is a schema-compile
// error naming the method that is missing.
func halfCompile(t reflect.Type) error {
	var errs []string
	var rec func(reflect.Type, string, map[reflect.Type]bool)
	rec = func(t reflect.Type, p string, seen map[reflect.Type]bool) {
		if seen[t] {
			return
		}
		seen[t] = true
		if _, ok := identityLookup(t); ok {
			return
		}
		if _, halves, ok := selectPaired(t, chainOrder); !ok && len(halves) > 0 {
			half := halves[0]
			errs = append(errs, fmt.Sprintf(
				"ferry: %s: %s %s, so the pair is incomplete and ferry will not use it; "+
					"implement the other half, or register a codec", p, t, half))
			return
		}
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			rec(t.Elem(), p+"/*", seen)
		case reflect.Struct:
			for i := range t.NumField() {
				if f := t.Field(i); f.IsExported() {
					rec(f.Type, p+"/"+f.Name, seen)
				}
			}
		}
	}
	rec(t, "", map[reflect.Type]bool{})
	if len(errs) == 0 {
		return nil
	}
	out := ""
	for _, e := range errs {
		out += e + "\n"
	}
	return fmt.Errorf("%s", out)
}

func runHalfPair() {
	chainOrder, chainBeforeKind = []string{"text"}, true
	defer func() { chainOrder, chainBeforeKind = nil, false }()

	fmt.Println("\n--- P12a: the three candidate answers, on the same two types ---")
	type holder struct {
		A encOnly
		B decOnly
	}
	fmt.Println("    (1) use the half anyway: measured in P2 - the value dumps and never")
	fmt.Println("        comes back, and the failure lands at Load against a plane you")
	fmt.Println("        have already written.")
	fmt.Println("    (2) fall through to kind admission, silently:")
	a, err := compile(reflect.TypeFor[holder]())
	d, _ := dump(reflect.ValueOf(holder{encOnly{"x"}, decOnly{"y"}}))
	fmt.Printf("        compiles: %v err=%v\n", a, err)
	fmt.Printf("        dumps:    %s\n", fmtVals(d))
	fmt.Println("        so a MarshalText the user wrote is ignored with no diagnostic,")
	fmt.Println("        which is what ADR-0001 rules out by name.")
	fmt.Println("    (3) refuse at schema compile, naming the missing method:")
	for _, line := range splitLines(fmt.Sprint(halfCompile(reflect.TypeFor[holder]()))) {
		if line != "" {
			fmt.Printf("        %s\n", line)
		}
	}

	fmt.Println("\n--- P12b: the blast radius of (3), measured rather than feared ---")
	fmt.Println("    In-process census over 29 types people put in config structs:")
	fmt.Println("      text 13 pairs, 0 halves; json 5, 0; binary 7, 0; gob 5, 0")
	fmt.Println("    Source scan over the whole go1.27rc2 public standard library:")
	fmt.Println("      text 13 pairs, 1 encoder-only (an internal tooling type), 0 decoder-only")
	fmt.Println("      binary 21 pairs, 0 halves; gob 4 pairs, 0 halves; json 5 pairs, 0 halves")
	fmt.Println("    Source scan over a third-party corpus (koanf v2, viper, mapstructure,")
	fmt.Println("    yaml.v3, BurntSushi/toml, google/uuid, shopspring/decimal, x/text,")
	fmt.Println("    xtools/xload, spf13/cast and their transitive dependencies):")
	fmt.Println("      text 12 pairs, 0 halves; json 14, 0; binary 3, 0; gob 1, 0")
	fmt.Println("    So the strict answer costs essentially nothing, because the encoding")
	fmt.Println("    interfaces are written and used as pairs in practice.")

	fmt.Println("\n--- P12c: where the halves DO come from ---")
	fmt.Println("    The only half pairs in the corpus are the ones a one-directional")
	fmt.Println("    interface creates. xload's own type package is three of them:")
	fmt.Println("      xloadtype.URL, .Endpoint, .Listener each have Decode(string) error")
	fmt.Println("      plus String(), and no MarshalText. The survey records that the")
	fmt.Println("      String() methods are 'unspecified, untested as a round trip, and")
	fmt.Println("      not used by the library'.")
	fmt.Println("    So the defect is not that users write halves; it is that a")
	fmt.Println("    one-directional interface makes a half the only thing they CAN write.")
	fmt.Println("    ferry declaring no codec interface of its own is what removes the")
	fmt.Println("    source rather than the symptom.")
}
