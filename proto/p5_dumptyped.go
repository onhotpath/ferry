package main

// P5: does the typed boundary still earn its keep once the address is
// structured?
//
// Section 4's decisive result was measured on a flat key space with composite
// values. ADR-0003 changed the address model underneath it, so the result is
// re-measured on ferry's actual shape rather than inherited.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func p5DumpTyped() {
	head("P5  typed vs string values, re-measured on the structured address model")

	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "ferryproto")
	defer os.RemoveAll(dir)

	addrs := NewAddressSet([]Path{
		path("name"), path("port"), path("ratio"), path("on"), path("nul"),
		path("quoted"), path("db", "host"),
	})

	typed := map[string]Value{
		"/name": String("svc"), "/port": Int(8080), "/ratio": Number("3.5"),
		"/on": Bool(true), "/nul": Null(), "/quoted": String("8080"),
		"/db/host": String("h"),
	}
	// The same information, flattened to text the way a string-only boundary
	// forces. This is xload's Loader signature applied to the Dump direction.
	stringified := map[string]Value{}
	for k, v := range typed {
		stringified[k] = String(v.Text())
	}

	for _, tc := range []struct {
		label string
		vals  map[string]Value
	}{{"typed", typed}, {"stringified", stringified}} {
		out := filepath.Join(dir, tc.label+".yaml")
		w, err := bindOpenSink(ctx, YAMLSink{Path: out}, addrs)
		if err != nil {
			fmt.Println("    open:", err)
			return
		}
		for _, p := range addrs.All() {
			if err := w.Set(ctx, p, tc.vals[p.String()]); err != nil {
				fmt.Println("    set:", err)
				w.Abort()
				return
			}
		}
		if err := w.Commit(ctx); err != nil {
			fmt.Println("    commit:", err)
			return
		}
		b, _ := os.ReadFile(out)
		fmt.Printf("\n    --- %s ---\n", tc.label)
		for _, ln := range splitLines(string(b)) {
			fmt.Println("       ", ln)
		}
	}

	// The round trip. Dump then Load, and compare kinds, which is what
	// ADR-0001's driver-fidelity obligation is actually about.
	fmt.Println("\n    (b) round trip through a real YAML file")
	for _, tc := range []struct {
		label string
		vals  map[string]Value
	}{{"typed", typed}, {"stringified", stringified}} {
		out := filepath.Join(dir, tc.label+".yaml")
		r, err := bindOpen(ctx, YAMLSource{Path: out}, addrs)
		if err != nil {
			fmt.Println("    open:", err)
			return
		}
		same := 0
		var drift []string
		for _, p := range addrs.All() {
			got, _ := r.Get(ctx, p)
			want := typed[p.String()]
			if got == want {
				same++
			} else {
				drift = append(drift, fmt.Sprintf("%s %s -> %s", p, want.GoString(), got.GoString()))
			}
		}
		fmt.Printf("        %-12s %d/%d addresses returned the original value exactly\n",
			tc.label, same, addrs.Len())
		for _, d := range drift {
			fmt.Println("            ", d)
		}
	}

	fmt.Println("\n    (c) reading")
	fmt.Println("        The asymmetry survives the address change: Load still")
	fmt.Println("        tolerates a string boundary because the struct field type")
	fmt.Println("        drives parsing, and Dump still cannot, because the sink has")
	fmt.Println("        to choose a YAML tag and a string gives it nothing to choose")
	fmt.Println("        from. What did change is the *size* of the loss: composites")
	fmt.Println("        no longer flatten into one value, so the damage is now only")
	fmt.Println("        at the scalar leaf.")
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
