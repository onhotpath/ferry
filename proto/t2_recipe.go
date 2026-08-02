package main

// T2: the recipe, written out, and what it costs.
//
// T1 established that neither of the two routes an accepted ADR names reaches
// a defaulted value on a struct with a `required` field. This is the shortest
// thing that does, built only from surface ADR-0010 and ADR-0011 already
// export. It is deliberately written as a function a THIRD PARTY could write,
// because ADR-0001 buckets template generation Enabled and that claim is what
// #14 is testing.

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// tPlan is everything a template emitter needs about one schema.
type tPlan struct {
	vals     map[Path]Value // the boundary value at each address, defaults applied
	addrs    []Path
	required map[Path]bool
	loads    int // how many Loads it took
	dumps    int
}

// tPlanFor is the recipe. Three core operations, no new interface.
//
//  1. Dump the zero value into a recording sink. This is the only way to
//     learn the boundary Value a required address will accept, and ADR-0011
//     makes it necessary: the error set names the address and the class and
//     deliberately does not name the type, because message text is not API.
//  2. Load from a plane holding only what step 3 has learned so far, adding
//     each reported `required` address, until the Load stops failing.
//  3. Dump the value that Load produced into a second recording sink.
func tPlanFor[T any](ctx context.Context, sch sched) (tPlan, error) {
	p := tPlan{vals: map[Path]Value{}, required: map[Path]bool{}}

	var zero T
	zrec := newRecorder()
	if err := Dump(ctx, zero, zrec); err != nil {
		return p, err
	}
	p.dumps++

	filled := map[Path]Value{}
	var cfg T
	for range 64 { // bounded: one iteration per required address at worst
		var err error
		cfg, err = LoadOver(ctx, zero, tFixedSource{vals: filled}, WithSched(sch))
		p.loads++
		if err == nil {
			break
		}
		progress := false
		for _, e := range tElements(err) {
			if !errors.Is(e, tErrMissing) {
				return p, err // not something a template can answer
			}
			a := tAddress(e)
			if p.required[a] {
				continue
			}
			p.required[a] = true
			// The boundary value a template puts at a required address is the
			// ZERO of its Go type, which the step-1 dump already holds. If the
			// zero dump did not reach it, the address is under an omitzero or
			// an absent subtree and there is nothing type-correct to supply -
			// see T8.
			if v, ok := zrec.vals[a]; ok {
				filled[a] = v
			} else {
				filled[a] = String("")
			}
			progress = true
		}
		if !progress {
			return p, err
		}
	}

	rec := newRecorder()
	if err := Dump(ctx, cfg, rec); err != nil {
		return p, err
	}
	p.dumps++
	p.vals, p.addrs = rec.vals, rec.addrs()
	return p, nil
}

func runT2() {
	ctx := context.Background()

	for _, c := range []struct {
		name string
		sch  sched
	}{
		{"aggregating (ADR-0011)", tAggregating},
		{"first error (the inherited default)", serial},
	} {
		p, err := tPlanFor[TConf](ctx, c.sch)
		fmt.Printf("%s\n  err=%v  Loads=%d  Dumps=%d  required=%d  addresses=%d\n",
			c.name, err, p.loads, p.dumps, len(p.required), len(p.addrs))
	}

	fmt.Println("\nSo the cost of the recipe is a property of ADR-0011's scheduler:")
	fmt.Println("  aggregating -> 2 Loads, always")
	fmt.Println("  first-error -> k+1 Loads for k required addresses")
	fmt.Println("ADR-0010 puts the scheduler behind a load-affecting Option, so a")
	fmt.Println("template generator outside core can choose the aggregating one.")

	p, _ := tPlanFor[TConf](ctx, tAggregating)
	fmt.Println("\nwhat the recipe produced:")
	for _, a := range p.addrs {
		mark := "  "
		if p.required[a] {
			mark = "R "
		}
		fmt.Printf("  %s%-16s %s\n", mark, a, p.vals[a].GoString())
	}

	fmt.Println("\nthree things it did NOT produce, all measured rather than reasoned:")
	fmt.Printf("  /debug is present in the address set: %v\n", contains(p.addrs, path("debug")))
	fmt.Println("    - an omitzero field at its zero value gets no Set call in EITHER dump,")
	fmt.Println("      so a generated template never tells the user the knob exists.")
	fmt.Printf("  /tags renders as %s and /limits as %s\n", p.vals[path("tags")].GoString(), p.vals[path("limits")].GoString())
	fmt.Println("    - ADR-0005 makes nil and empty one value, so a template cannot say")
	fmt.Println("      \"a list goes here\"; it can only say null.")
	fmt.Println("  the DECLARED DEFAULT TEXT is nowhere in the output. /db/port reads")
	fmt.Printf("      %s, which is the default APPLIED, and there is no channel through\n", p.vals[path("db", "port")].GoString())
	fmt.Println("      which the emitter learns that 5432 came from a declaration rather")
	fmt.Println("      than from the plane. Nor does it learn the Go type. Both live in the")
	fmt.Println("      compiled schema, which ADR-0001 keeps unexported and ADR-0010 kept so.")

	fmt.Println("\nand the alternative to asking core, priced:")
	fmt.Println("  an emitter holding T can read reflect.TypeFor[T]() itself and parse the")
	fmt.Println("  ferry tags a second time. Measured, on this fixture:")
	tag2 := tWalkTags(reflect.TypeFor[TConf](), Path{}, map[Path]tNote{})
	for _, a := range sortedPaths(keysOf(tag2)) {
		fmt.Printf("    %-16s %s\n", a, tNoteText(tag2[a]))
	}
	fmt.Println("  It works, and it is ADR-0010's walk-duplication axis 1 by construction:")
	fmt.Println("  a second implementation of \"which fields, at which addresses\" that")
	fmt.Println("  nothing keeps in step with the compiler. ADR-0008 found exactly this")
	fmt.Println("  defect in a real ferry prototype and called it silent.")
}

// tWalkTags is the thing a third-party emitter is forced to write, and it is
// here to be measured rather than recommended. It reimplements the field rule,
// the promotion rule and the address minting, against the same reflect type
// the compiler already walked.
func tWalkTags(t reflect.Type, p Path, out map[Path]tNote) map[Path]tNote {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return out
	}
	for i := range t.NumField() {
		f := t.Field(i)
		raw, ok := f.Tag.Lookup("ferry")
		if !ok || raw == "-" || !f.IsExported() {
			continue
		}
		parts := splitTag(raw)
		at := p.Name(unquoteTok(parts[0]))
		n := tNote{gotype: f.Type.String()}
		for _, o := range parts[1:] {
			switch {
			case o == "required":
				n.required = true
			case o == "omitzero":
				n.omitzero = true
			case strings.HasPrefix(o, "default="):
				n.hasDef = true
				n.def = unquoteTok(strings.TrimPrefix(o, "default="))
			}
		}
		out[at] = n
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct && !isLeafish(ft) {
			tWalkTags(ft, at, out)
		}
	}
	return out
}

func tNoteText(n tNote) string {
	s := n.gotype
	var bits []string
	if n.required {
		bits = append(bits, "required")
	}
	if n.omitzero {
		bits = append(bits, "omitzero")
	}
	if n.hasDef {
		bits = append(bits, "default="+n.def)
	}
	if len(bits) > 0 {
		s += " [" + strings.Join(bits, " ") + "]"
	}
	return s
}

func isLeafish(t reflect.Type) bool { _, ok := resolveLeaf(t); return ok }

func contains(ps []Path, p Path) bool {
	for _, x := range ps {
		if x == p {
			return true
		}
	}
	return false
}

func keysOf[V any](m map[Path]V) []Path {
	out := make([]Path, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
