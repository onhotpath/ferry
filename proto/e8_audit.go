package main

// E8: the audit.
//
// Every prior session's worst defect was a case the fixtures did not contain.
// The handoff named this ticket's likely one outright: "a fixture that
// compiles one schema once, in one goroutine, under one configuration - so
// the cache is never contended, never asked for the same type under two tag
// keys or two registries, and never asked to compile a type whose schema is
// already being built by somebody else."
//
// E4e covers the contention. This probe covers what is left: a schema with a
// registered type in every position the walk has, populated and all-zero,
// through the cache, in both directions; and the strongest claim in this
// ticket audited against the case it does not cover.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

type E8Opaque struct{ v string }

func (o E8Opaque) text() string { return o.v }

type E8Deep struct {
	Deep E8Opaque `ferry:"deep"`
}

type E8All struct {
	Leaf   E8Opaque            `ferry:"leaf"`
	Ptr    *E8Opaque           `ferry:"ptr"`
	Slice  []E8Opaque          `ferry:"slice"`
	Array  [2]E8Opaque         `ferry:"array"`
	MapVal map[string]E8Opaque `ferry:"mapval"`
	Nested E8Deep              `ferry:"nested"`
	Plain  string              `ferry:"plain,default=d"`
	Req    string              `ferry:"req,required"`
	Omit   string              `ferry:"omit,omitzero"`
}

func e8Reg() *Registry {
	r := NewRegistry()
	mustReg(r, StringCodec(E8Opaque.text, func(s string) (E8Opaque, error) { return E8Opaque{s}, nil }))
	return r
}

func runE8() {
	ctx := context.Background()

	fmt.Println("--- E8a: a registered type in every position, both directions, through the cache ---")
	reg := e8Reg()
	populated := E8All{
		Leaf:   E8Opaque{"L"},
		Ptr:    &E8Opaque{"P"},
		Slice:  []E8Opaque{{"s0"}, {"s1"}},
		Array:  [2]E8Opaque{{"a0"}, {"a1"}},
		MapVal: map[string]E8Opaque{"k": {"m"}},
		Nested: E8Deep{E8Opaque{"D"}},
		Plain:  "p",
		Req:    "r",
		Omit:   "o",
	}
	for _, c := range []struct {
		name string
		v    E8All
	}{
		{"populated", populated},
		{"every field zero", E8All{}},
	} {
		out, err := dumpTo(ctx, c.v, WithRegistry(reg))
		if err != nil {
			fmt.Printf("  %-18s dump err: %v\n", c.name, err)
			continue
		}
		back, err := loadFrom(ctx, E8All{}, out, WithRegistry(reg))
		fmt.Printf("  %-18s %2d addrs, load err=%v\n", c.name, len(out), trunc(err))
		fmt.Printf("  %-18s round trip equal: %v\n", "", e8Equal(c.v, back))
		if !e8Equal(c.v, back) {
			fmt.Printf("  %-18s  in : %+v\n", "", c.v)
			fmt.Printf("  %-18s  out: %+v\n", "", back)
		}
	}

	fmt.Println("\n  The zero row is the one that matters: #12's worst defect was that every")
	fmt.Println("  fixture put the codec at a leaf in a one-field struct at a NON-ZERO")
	fmt.Println("  value, and #19's was that no fixture dumped a registered interface at")
	fmt.Println("  its zero value. Here the zero row has a nil pointer, a nil slice, a nil")
	fmt.Println("  map, an omitted field and a required field with nothing on the plane.")

	fmt.Println("\n--- E8b: and through a REAL YAML plane, which is where E1 lives ---")
	dir, _ := os.MkdirTemp("", "e8")
	defer os.RemoveAll(dir)
	yp := filepath.Join(dir, "a.yaml")
	if err := Dump(ctx, populated, FYAMLSink{Path: yp}, WithRegistry(reg)); err != nil {
		fmt.Println("  dump:", err)
	}
	b, _ := os.ReadFile(yp)
	fmt.Printf("  %s\n", indentBlock(string(b)))
	got, err := Load[E8All](ctx, FYAMLSource{Path: yp}, WithRegistry(reg))
	fmt.Printf("  Load[E8All] -> err=%v  round trip equal: %v\n", trunc(err), e8Equal(populated, got))
	if !e8Equal(populated, got) {
		fmt.Printf("    in : %+v\n    out: %+v\n", populated, got)
	}

	fmt.Println("\n--- E8c: ADR-0006's merge-against-replace, through the seeded entry point ---")
	fmt.Println("  ADR-0006: a struct MERGES into a seeded value field by field, and a")
	fmt.Println("  slice or a map is REPLACED wholesale. Both follow from one rule and")
	fmt.Println("  neither looks like it does, so it is checked rather than assumed.")
	seed := E8All{
		Nested: E8Deep{E8Opaque{"seeded-deep"}},
		Slice:  []E8Opaque{{"seeded0"}, {"seeded1"}, {"seeded2"}},
		MapVal: map[string]E8Opaque{"seededkey": {"x"}},
		Req:    "seeded",
	}
	partial := map[Path]Value{
		Path{}.Name("req"):                 String("r"),
		Path{}.Name("slice").Index(0):      String("NEW"),
		Path{}.Name("mapval").Name("k"):    String("NEW"),
		Path{}.Name("nested").Name("deep"): String("NEW"),
		Path{}.Name("leaf"):                String("L"),
		Path{}.Name("array").Index(0):      String("a0"),
		Path{}.Name("array").Index(1):      String("a1"),
	}
	merged, err := loadFrom(ctx, seed, partial, WithRegistry(reg))
	fmt.Printf("  err=%v\n", trunc(err))
	fmt.Printf("    Nested (struct) : %+v   <- merged\n", merged.Nested)
	fmt.Printf("    Slice           : %+v   <- replaced (seed had 3)\n", merged.Slice)
	fmt.Printf("    MapVal          : %+v   <- replaced (seed had seededkey)\n", merged.MapVal)

	fmt.Println("\n--- E8d: the strongest claim in this ticket, and where it could be wrong ---")
	fmt.Println("  The claim: \"the schema and the walk cannot disagree, because they are")
	fmt.Println("  one list\". The case that would break it is a walk that reaches a field")
	fmt.Println("  by a route the schema did not compile. Two were checked:")
	fmt.Println()
	dn := reg.install()
	s, cerr := compileSchema2(reflect.TypeFor[E8All](), withReg(reg))
	dn()
	if cerr != nil {
		fmt.Println("    compile:", cerr)
		return
	}
	fmt.Printf("    compiled leaves            : %d\n", s.leaves)
	out, _ := dumpTo(ctx, populated, WithRegistry(reg))
	fmt.Printf("    addresses the dump wrote   : %d\n", len(out))
	fmt.Println("    (the two differ by the dynamic ones, which is the point: the schema")
	fmt.Println("     holds SHAPES and the walk mints ADDRESSES, and only the second")
	fmt.Println("     depends on the value)")
	fmt.Println()
	fmt.Println("  The honest residue: a DYNAMIC address is minted by the walk and was")
	fmt.Println("  never in the schema, so the compile-against-walk guarantee covers the")
	fmt.Println("  static tier only. That is not a gap this ticket can close - it is")
	fmt.Println("  ADR-0003's two tiers - and the ADR says so rather than claiming the")
	fmt.Println("  stronger version.")

	fmt.Println("\n--- E8f: is the compile refusal deterministic (ADR-0001's invariant) ---")
	type E8Bad struct {
		A string `ferry:"a,requird"`
		B int    `ferry:"b,default=abc"`
		C string
		D []int `ferry:"d,required"`
	}
	distinct := map[string]int{}
	for range 300 {
		o := defaultOpts()
		o.reg = NewRegistry()
		dd := o.reg.install()
		_, e := compileSchema2(reflect.TypeFor[E8Bad](), o)
		dd()
		distinct[strings.Join(errLines(e), "\n")]++
	}
	fmt.Printf("  300 compiles of a type with four bad fields -> %d distinct error string(s)\n", len(distinct))
	for k := range distinct {
		for _, l := range splitL(k) {
			fmt.Printf("    %s\n", l)
		}
	}

	fmt.Println("\n--- E8e: the presence-bit race, which is what #20 actually inherits ---")
	fmt.Println("  Run under -race. The walk ORs a subtree's presence bit into a captured")
	fmt.Println("  variable, so a non-serial scheduler races on it:")
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Println("    panicked:", r)
			}
		}()
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); _ = runParallelOnce(ctx, s, out, reg) }()
		wg.Wait()
	}()
	fmt.Println("    (no output here means the walk completed; the race detector's report")
	fmt.Println("     is the result, so this probe is only meaningful under -race)")
}

func runParallelOnce(ctx context.Context, s *schema, vals map[Path]Value, reg *Registry) E8All {
	o := withReg(reg)
	done := reg.install()
	defer done()
	var out E8All
	w := &walker{dir: loadDir(mapReader{vals}, ctx, o), sch: parallel, ctx: ctx}
	_, _ = w.walk(s.root, reflect.ValueOf(&out).Elem(), Path{})
	return out
}

func withReg(r *Registry) opts {
	o := defaultOpts()
	o.reg = r
	return o
}

func e8Equal(a, b E8All) bool {
	if (a.Ptr == nil) != (b.Ptr == nil) {
		return false
	}
	if a.Ptr != nil && *a.Ptr != *b.Ptr {
		return false
	}
	a.Ptr, b.Ptr = nil, nil
	return reflect.DeepEqual(a, b)
}

func indentBlock(s string) string {
	out := ""
	for _, l := range splitL(s) {
		out += "  | " + l + "\n"
	}
	return out
}

func splitL(s string) []string {
	var out []string
	cur := ""
	for i := range len(s) {
		if s[i] == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(s[i])
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
