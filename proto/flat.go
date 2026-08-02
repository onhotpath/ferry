package main

// The third plane the harness needs, and the one whose absence hid G2.
//
// ADR-0004: "a typed boundary buys YAML and TOML something real, JSON
// something partial, and Consul, environment variables and query parameters
// nothing at all - and two of the four first-party drivers are in the last
// group." A plane in that group can only ever report String. Every proof so
// far fed dump's own output back into load, so the kinds always matched and
// no probe ever exercised the majority case.

import (
	"fmt"
	"reflect"
	"time"
)

type durAlias = time.Duration

// flatten models a plane with no type information: everything it holds is
// text, so everything it reports is String.
func flatten(in map[Path]Value) (map[Path]Value, error) {
	out := make(map[Path]Value, len(in))
	for p, v := range in {
		switch v.Kind() {
		case VAbsent:
			// not stored
		case VNull:
			// A flat plane has no null. ADR-0004's own table says so:
			// FOO= is a zero-length string, not a null. So the container
			// marker cannot survive, and this is where that costs.
			out[p] = String("")
		default:
			out[p] = String(v.Text())
		}
	}
	return out, nil
}

func runFlat() {
	defer runCast()
	fmt.Println("\n--- the core set through a FLATTENING plane (env/query/kv shaped) ---")
	for _, set := range []struct {
		name   string
		proofs []Proof
	}{{"core", coreSet()}, {"composites", auditSet()}} {
		ok, bad := 0, 0
		for _, pr := range set.proofs {
			if f := pr.run(flatten); len(f) > 0 {
				bad++
				fmt.Printf("  FAIL %-22s %s\n", pr.Name(), f[0])
			} else {
				ok++
			}
		}
		fmt.Printf("  %s: %d/%d\n", set.name, ok, ok+bad)
	}

	fmt.Println("\n--- and the coercion that must NOT happen ---")
	type S struct{ V string }
	var s S
	e := load(map[Path]Value{Path{}.Name("V"): Number("8080")}, reflect.ValueOf(&s).Elem())
	fmt.Printf("  Number(\"8080\") into a Go string field -> %q err=%v\n", s.V, e)
	var b struct{ V bool }
	e2 := load(map[Path]Value{Path{}.Name("V"): String("yes")}, reflect.ValueOf(&b).Elem())
	fmt.Printf("  String(\"yes\")  into a Go bool   field -> %v err=%v\n", b.V, e2)
	var i struct{ V int }
	e3 := load(map[Path]Value{Path{}.Name("V"): String("010")}, reflect.ValueOf(&i).Elem())
	fmt.Printf("  String(\"010\")  into a Go int    field -> %v err=%v  (cast gives 8)\n", i.V, e3)
}

// The survey names cast's zero-padded-port defect by number. Check ferry's
// answer against it directly.
func runCast() {
	fmt.Println("\n--- against the defects the survey measured in spf13/cast ---")
	for _, c := range []struct{ in, castSays string }{
		{"0080", "0   (invalid octal, error swallowed)"},
		{"010", "8   (base 0: octal)"},
		{"0x10", "16  (base 0: hex)"},
		{"1.9", "1   (truncated)"},
		{"", "0   (indistinguishable from a real 0)"},
	} {
		var i struct{ V int }
		e := load(map[Path]Value{Path{}.Name("V"): String(c.in)}, reflect.ValueOf(&i).Elem())
		got := fmt.Sprintf("%d", i.V)
		if e != nil {
			got = "refused"
		}
		fmt.Printf("  %-6q ferry=%-9s cast=%s\n", c.in, got, c.castSays)
	}
	var d struct{ D int64 }
	_ = d
	fmt.Println("  cast also turns \"30\" into 30ns for a Duration; ferry's Duration")
	fmt.Println("  parses with time.ParseDuration, so \"30\" is refused and \"30s\" is 30s.")
	var dd struct{ D durAlias }
	e := load(map[Path]Value{Path{}.Name("D"): String("30")}, reflect.ValueOf(&dd).Elem())
	fmt.Printf("  String(\"30\") into time.Duration -> err=%v\n", e)
}
