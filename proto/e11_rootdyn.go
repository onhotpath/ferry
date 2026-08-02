package main

// E11: auditing my own call that a root map and a root slice stay legal.
//
// The ADR argues it on plane-to-plane transfer being the caller who would
// depend on the permission. That argument is checked here rather than
// repeated, and so is what a root dynamic composite does at its EMPTY value,
// which no probe has looked at.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

func runE11() {
	ctx := context.Background()

	fmt.Println("--- E11a: what a root map/slice mints at its EMPTY value ---")
	for _, c := range []struct {
		name string
		v    any
	}{
		{"map[string]int{\"a\":1}", map[string]int{"a": 1}},
		{"map[string]int{}      ", map[string]int{}},
		{"map[string]int(nil)   ", map[string]int(nil)},
		{"[]string{\"a\"}        ", []string{"a"}},
		{"[]string{}            ", []string{}},
		{"[]string(nil)         ", []string(nil)},
	} {
		o := defaultOpts()
		s, err := schemaFor(reflect.TypeOf(c.v), o)
		if err != nil {
			fmt.Printf("  %-22s compile: %v\n", c.name, trunc(err))
			continue
		}
		out := map[Path]Value{}
		w := &walker{dir: dumpDir(out), sch: serial, ctx: ctx}
		_, derr := w.walk(s.root, reflect.ValueOf(c.v), Path{})
		addrs := ""
		for _, p := range sortedAddrs(out) {
			addrs += fmt.Sprintf("%q=%s ", p.String(), out[p].GoString())
		}
		fmt.Printf("  %-22s static=%v dump=%s err=%v\n", c.name, s.addrs, addrs, derr)
	}

	fmt.Println("\n--- E11b: and through the real YAML sink ---")
	dir, _ := os.MkdirTemp("", "e11")
	defer os.RemoveAll(dir)
	for i, v := range []any{map[string]int{"a": 1}, map[string]int{}, []string{"a"}, []string(nil)} {
		p := filepath.Join(dir, fmt.Sprintf("m%d.yaml", i))
		var err error
		switch x := v.(type) {
		case map[string]int:
			err = Dump(ctx, x, FYAMLSink{Path: p})
		case []string:
			err = Dump(ctx, x, FYAMLSink{Path: p})
		}
		b, _ := os.ReadFile(p)
		fmt.Printf("  %-24v -> err=%v wrote %q\n", v, err, string(b))
	}

	fmt.Println("\n--- E11c: what the ADR's own justification actually says ---")
	fmt.Println("  The draft argues root dynamic composites stay legal because")
	fmt.Println("  \"plane-to-plane transfer is exactly the caller who would depend on it\".")
	fmt.Println("  ADR-0006 already measured that transfer has TWO shapes:")
	fmt.Println("    (a) address-to-address: a loop from Reader.Get into Writer.Set,")
	fmt.Println("        which \"builds no Go value and never runs this ADR's rules at all\"")
	fmt.Println("    (b) struct-mediated: Load into T, Dump out")
	fmt.Println("  (a) never calls Load[T] at any type, and (b) uses a struct.")

	fmt.Println("\n--- E11d: what the permission cost, and why it is now refused ---")
	o := defaultOpts()
	sm, merr := schemaFor(reflect.TypeFor[map[string]int](), o)
	ss, serr := schemaFor(reflect.TypeFor[E11Struct](), o)
	if merr != nil {
		fmt.Printf("  map[string]int at the root : %s\n", trunc(merr))
	} else {
		fmt.Printf("  map[string]int at the root : static set handed to Bind = %v (len %d)\n", sm.addrs, len(sm.addrs))
	}
	if serr != nil {
		fmt.Printf("  struct{Limits map[...]}    : %v\n", serr)
	} else {
		fmt.Printf("  struct{Limits map[...]}    : static set handed to Bind = %v (len %d)\n", ss.addrs, len(ss.addrs))
	}
	fmt.Println()
	fmt.Println("  Four costs, and the first is decisive because it is silent:")
	fmt.Println("   1. a nil or EMPTY root map/slice writes Null at its own address, which")
	fmt.Println("      at the root IS the empty path, and the YAML sink writes \"{}\" and")
	fmt.Println("      returns a nil error. That is the same silent total loss E7b measured")
	fmt.Println("      for a root leaf, reached through the door the permission left open.")
	fmt.Println("   2. the static set handed to Bind is EMPTY, so ADR-0003's driver-side")
	fmt.Println("      injectivity rule is vacuously true over the whole schema.")
	fmt.Println("   3. ADR-0008's naming rule never fires: no field, so no segment name")
	fmt.Println("      was written by a human - the property ADR-0008 spent thirty tags on")
	fmt.Println("      a thirty-field struct to buy.")
	fmt.Println("   4. Load needs an Enumerator for every address, so the type is loadable")
	fmt.Println("      from a strict subset of the planes a struct is.")
	fmt.Println()
	fmt.Println("  Against a benefit that E11c shows is not there. So the root must be a")
	fmt.Println("  struct ferry walks, and a caller who wants a bare map writes:")
	fmt.Println("      type Labels struct{ M map[string]string `ferry:\"labels\"` }")
	fmt.Println("  which is one line and buys them a named segment.")
	fmt.Println()
	fmt.Println("  This is also the reversible direction, which is the rule the draft broke")
	fmt.Println("  to allow it: nobody can depend on a refusal, and lifting it later is")
	fmt.Println("  additive.")
}

type E11Struct struct {
	Limits map[string]int `ferry:"limits"`
}
