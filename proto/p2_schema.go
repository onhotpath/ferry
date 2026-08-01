package main

// P2: is collision detectable when the schema is compiled from the type alone,
// with no plane reachable and no value in hand? ADR-0001 says tag rejection is
// assertable that way; this asks whether address collision is too, because if
// it is, the dump-side silent-loss question dies at compile time rather than
// needing a runtime rule.

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

type leaf struct {
	Path  Path
	Field string // Go field path, for the error message
	Type  reflect.Type
}

type schema struct {
	leaves []leaf
}

func compile[T any]() (*schema, error) { return compileType(reflect.TypeFor[T]()) }

func compileType(t reflect.Type) (*schema, error) {
	s := &schema{}
	if err := walk(t, Path{}, "", s); err != nil {
		return nil, err
	}
	// The collision check: the address set must be injective over the leaves.
	// Deterministic because leaves are in reflect field order, which is source
	// order, and the report is sorted.
	seen := map[Path]leaf{}
	var clashes []string
	for _, l := range s.leaves {
		if prev, ok := seen[l.Path]; ok {
			clashes = append(clashes, fmt.Sprintf(
				"address %s is claimed by both %s and %s", l.Path, prev.Field, l.Field))
			continue
		}
		seen[l.Path] = l
	}
	if len(clashes) > 0 {
		slices.Sort(clashes)
		return nil, fmt.Errorf("schema %s:\n      %s", t, strings.Join(clashes, "\n      "))
	}
	return s, nil
}

func walk(t reflect.Type, at Path, goPath string, s *schema) error {
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}
		tag, ok := f.Tag.Lookup("ferry")
		name, _, _ := strings.Cut(tag, ",")
		if !ok {
			name = f.Name
		}
		gp := goPath + "." + f.Name
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		// A struct that is not a leaf type nests; everything else is a leaf.
		if ft.Kind() == reflect.Struct && !isLeafStruct(ft) {
			next := at
			if !strings.Contains(tag, ",squash") {
				next = at.Name(name)
			}
			if err := walk(ft, next, gp, s); err != nil {
				return err
			}
			continue
		}
		s.leaves = append(s.leaves, leaf{Path: at.Name(name), Field: gp[1:], Type: f.Type})
	}
	return nil
}

func isLeafStruct(t reflect.Type) bool { return t.String() == "time.Time" }

func p2Schema() {
	head("P2  collision at schema-compile time, from the type alone")

	// (a) Two fields claiming one address, at the same level.
	type Dup struct {
		Primary string `ferry:"host"`
		Legacy  string `ferry:"host"`
	}
	report("(a) same tag, same level", compile[Dup])

	// (b) Two fields claiming one address via nesting.
	type Inner struct {
		Host string `ferry:"host"`
	}
	type ViaNesting struct {
		DB     Inner  `ferry:"db"`
		DBHost string `ferry:"db"` // collides with the nested struct's own segment? no:
	}
	report("(b) leaf vs nested struct at one segment", compile[ViaNesting])

	// (c) The case that separates the two models. Under a flat key space joined
	//     with "_", these two are the same key. Under structured addresses they
	//     are not, so core admits them and the question moves to the driver.
	type Straddle struct {
		DB     Inner  `ferry:"DB"`
		DBHost string `ferry:"DB_HOST"`
	}
	report("(c) DB/HOST vs DB_HOST", compile[Straddle])

	// (d) Case variants. Core must not fold, so these are three addresses.
	type CaseVariants struct {
		A string `ferry:"myKey"`
		B string `ferry:"MyKey"`
		C string `ferry:"MYKEY"`
	}
	report("(d) three case variants", compile[CaseVariants])

	// (e) Squash / embedded, the other way a collision is manufactured.
	type L struct {
		Host string `ferry:"host"`
	}
	type R struct {
		Host string `ferry:"host"`
	}
	type Squashed struct {
		Left  L `ferry:",squash"`
		Right R `ferry:",squash"`
	}
	report("(e) two squashed structs", compile[Squashed])

	// (f) Determinism of the failure. Compile the same bad type many times.
	msgs := map[string]int{}
	for range 300 {
		_, err := compile[Squashed]()
		msgs[err.Error()]++
	}
	fmt.Printf("    (f) 300 compiles of (e) produced %d distinct error strings\n", len(msgs))

	// (g) No value in hand, no plane reachable.
	s, err := compile[Straddle]()
	fmt.Printf("    (g) compiled from reflect.TypeFor[T]() alone: err=%v leaves=%d\n", err, len(s.leaves))
	for _, l := range s.leaves {
		fmt.Printf("        %-16s %-14s %s\n", l.Path, l.Field, l.Type)
	}
}

func report(label string, f func() (*schema, error)) {
	_, err := f()
	if err == nil {
		fmt.Printf("    %-42s ACCEPTED\n", label)
		return
	}
	fmt.Printf("    %-42s REJECTED: %v\n", label, err)
}
