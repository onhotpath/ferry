package main

// Can `required` on a slice mean "explicitly [] is fine, null and missing are
// not"? That needs the boundary to separate `origins: []` from a missing key.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

func rs1() {
	dhdr("RS1 what the real YAML driver reports at a container address")
	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "ferryRS")
	defer os.RemoveAll(dir)

	docs := []struct{ label, body string }{
		{"key missing     ", "other: 1\n"},
		{"origins: []     ", "origins: []\n"},
		{"origins: {}     ", "origins: {}\n"},
		{"origins: null   ", "origins: null\n"},
		{"origins: [a]    ", "origins: [a]\n"},
	}
	fmt.Printf("  %-17s %-12s %-10s %s\n", "document", "Get(/origins)", "Children", "distinguishable?")
	var sig []string
	for _, d := range docs {
		f := filepath.Join(dir, "c.yaml")
		_ = os.WriteFile(f, []byte(d.body), 0o644)
		open, _ := (FYAMLSource{Path: f}).Bind(NewAddressSet([]Path{addr("origins")}))
		r, err := open(ctx)
		if err != nil {
			fmt.Printf("  %s open error: %v\n", d.label, err)
			continue
		}
		v, gerr := r.Get(ctx, addr("origins"))
		kids := []Path{}
		if en, ok := r.(FEnumerator); ok {
			kids, _ = en.Children(ctx, addr("origins"))
		}
		if rel, ok := r.(FReleaser); ok {
			_ = rel.Close()
		}
		g := v.GoString()
		if gerr != nil {
			g = "absent"
		}
		s := fmt.Sprintf("%s|%d", g, len(kids))
		sig = append(sig, s)
		fmt.Printf("  %-17s %-12s %-10d\n", d.label, g, len(kids))
	}
	fmt.Printf("\n  distinct observations: %d for %d documents\n", countDistinct(sig), len(sig))
	fmt.Println("  `key missing`, `origins: []` and `origins: {}` are ONE observation.")
	fmt.Println("  No rule keyed on what the plane reports can separate them, because")
	fmt.Println("  there is nothing to key on. This is ADR-0005's forced collision.")
}

func countDistinct(ss []string) int {
	m := map[string]bool{}
	for _, s := range ss {
		m[s] = true
	}
	return len(m)
}

// ---------------------------------------------------------------------------
// RS2 the four candidate readings, and which are implementable
// ---------------------------------------------------------------------------

type RSConf struct {
	Origins []string `ferry:"origins"`
}

func rs2() {
	dhdr("RS2 the four readings of `required` on []string")
	allowRequiredOnComposite = true
	defer func() { allowRequiredOnComposite = false }()

	planes := []struct {
		label string
		vals  map[Path]Value
	}{
		{"key missing  ", map[Path]Value{}},
		{"origins: []  ", map[Path]Value{}}, // SAME observation, by RS1
		{"origins: null", map[Path]Value{addr("origins"): Null()}},
		{"origins: [a] ", map[Path]Value{addr("origins").Index(0): String("a")}},
	}

	type reading struct {
		name string
		sat  func(vals map[Path]Value) bool
	}
	readings := []reading{
		{"(ii) has children", func(v map[Path]Value) bool { return len(children(v, addr("origins"))) > 0 }},
		{"(iii) children or null", func(v map[Path]Value) bool {
			return len(children(v, addr("origins"))) > 0 || v[addr("origins")].Kind() == VNull
		}},
		{"(iv) [] yes, null/missing no", nil}, // what the reading would need
	}

	fmt.Printf("  %-14s %-19s %-24s %s\n", "document", readings[0].name, readings[1].name, readings[2].name)
	for _, pl := range planes {
		row := fmt.Sprintf("  %-14s ", pl.label)
		for _, r := range readings {
			if r.sat == nil {
				want := "refused"
				if pl.label == "origins: []  " {
					want = "SATISFIED"
				}
				row += fmt.Sprintf("%-24s", want+" (wanted)")
				continue
			}
			v := "refused"
			if r.sat(pl.vals) {
				v = "SATISFIED"
			}
			w := 19
			if r.name == readings[1].name {
				w = 24
			}
			row += fmt.Sprintf("%-*s ", w, v)
		}
		fmt.Println(row)
	}
	fmt.Println("\n  Rows 1 and 2 are the same input, because RS1 measured them as one")
	fmt.Println("  observation. Reading (iv) demands two different answers for it, so it")
	fmt.Println("  is not a rule that can be written, at any cost inside ADR-0004's")
	fmt.Println("  kind set.")
}

// ---------------------------------------------------------------------------
// RS3 what it would take, and what that would cost
// ---------------------------------------------------------------------------

func rs3() {
	dhdr("RS3 could a seventh kind carry it?")
	fmt.Println("  ADR-0004 closed the value model at six kinds with no group arm and no")
	fmt.Println("  escape arm. Reading (iv) needs a seventh, 'present and empty'.")
	fmt.Println()
	fmt.Println("  which planes could produce it, from ADR-0004's own table:")
	rows := []struct{ plane, can, why string }{
		{"YAML", "yes", "an empty sequence node is a distinct node"},
		{"JSON", "yes", "[] is a distinct token pair"},
		{"TOML", "yes", "[] is expressible"},
		{"env", "NO", "there is no way to write an empty list; ORIGINS_0 exists or does not"},
		{"query params", "NO", "same"},
		{"KV, opaque bytes", "NO", "same"},
	}
	for _, r := range rows {
		fmt.Printf("    %-18s %-4s %s\n", r.plane, r.can, r.why)
	}
	fmt.Println()
	fmt.Println("  So even with the seventh kind, `required` on a slice would mean")
	fmt.Println("  'explicitly empty counts' on three planes and 'at least one element'")
	fmt.Println("  on the other three. That is the shape ADR-0005 refused by name:")
	fmt.Println("  a guarantee that holds on some planes and not others is not a guarantee.")
	fmt.Println()
	fmt.Println("  ADR-0005 already answers this exact distinction, for exactly this")
	fmt.Println("  reason, and its answer is to model it in the type:")
	type Origins struct {
		Set   bool     `ferry:"set,required"`
		Items []string `ferry:"items"`
	}
	type C struct {
		Origins Origins `ferry:"origins"`
	}
	s := mustSchema(reflect.TypeFor[C]())
	for _, c := range []struct {
		label string
		vals  map[Path]Value
	}{
		{"nothing              ", map[Path]Value{}},
		{"set=true, no items   ", map[Path]Value{addr("origins", "set"): String("true")}},
		{"set=true, one item   ", map[Path]Value{
			addr("origins", "set"): String("true"), addr("origins", "items").Index(0): String("a")}},
	} {
		var v C
		_, e := loadD(c.vals, s, reflect.ValueOf(&v).Elem(), loadOpts{})
		fmt.Printf("    %s -> %+v err=%v\n", c.label, v.Origins, errOrBlank(e))
	}
	fmt.Println("    `required` on Set is a leaf presence test, so it works identically")
	fmt.Println("    on every plane, including env (ORIGINS_SET=true).")
}
