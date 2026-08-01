package main

// P15: the METRIC_HTTP_PORT question, run against ferry's model, mirroring the
// prior-art measurement in scratchpad/priorart.
//
// The prior art, measured:
//   xload FlattenMap sep="_"   metric.http.port vs metric.http_port -> 258/42 nondeterministic
//   xload FlattenMap sep="__"  distinct, 300/300 stable
//   xload FlattenMap sep="__"  with a key containing "__" -> 261/39 nondeterministic again
//   xload struct side          DOES detect: "key collisions detected for keys: [METRIC_HTTP_PORT]"
//                              but SkipCollisionDetection turns it off and both fields silently share
//   koanf                      last load wins, silently, no error
//   viper SetEnvKeyReplacer    "_" collides, "__" does not, nothing checks either way

import (
	"fmt"
	"strings"
)

// The env driver's join is a driver option, because flattening is the driver's.
func envJoin(sep string) driver {
	return driver{"env(sep=" + sep + ")", func(p Path) string {
		var b []string
		for s := range p.SegmentsSeq() {
			b = append(b, strings.ToUpper(s.Text))
		}
		return strings.Join(b, sep)
	}, func(p Path) error {
		for s := range p.SegmentsSeq() {
			if s.Text == "" {
				return fmt.Errorf("%s: empty segment has no environment variable name", p)
			}
		}
		return nil
	}}
}

func p15Separator() {
	head("P15  METRIC_HTTP_PORT: nesting or a name with an underscore?")

	// In core these are simply two different addresses. There is nothing to
	// disambiguate, because ferry never parses a plane key back into a path.
	nested := path("metric", "http", "port")
	flat := path("metric", "http_port")
	fmt.Printf("    metric.http.port  -> %s   (3 segments)\n", nested)
	fmt.Printf("    metric.http_port  -> %s   (2 segments)\n", flat)
	fmt.Printf("    distinct in core, always, no option involved: %v\n", nested != flat)

	fmt.Println("\n    what each env join does with the pair:")
	for _, sep := range []string{"_", "__", "___"} {
		d := envJoin(sep)
		fmt.Printf("        sep=%-5q %-22s %-22s %s\n", sep, d.key(nested), d.key(flat),
			verdict(d.accept([]Path{nested, flat})))
	}

	fmt.Println("\n    the error, in full, for the default join:")
	fmt.Printf("        %v\n", envJoin("_").accept([]Path{nested, flat}))

	// The case where the chosen separator itself appears in segment text. This
	// is where xload's "__" answer runs out, silently and nondeterministically.
	fmt.Println("\n    and where a segment contains the chosen separator:")
	n2 := path("metric__http", "port")
	f2 := path("metric", "http__port")
	for _, sep := range []string{"__", "___"} {
		d := envJoin(sep)
		fmt.Printf("        sep=%-5q %-24s %-24s %s\n", sep, d.key(n2), d.key(f2),
			verdict(d.accept([]Path{n2, f2})))
	}
	fmt.Printf("\n        xload at sep=\"__\" on this pair: 261/39 nondeterministic, no error\n")
	fmt.Printf("        ferry at sep=\"__\" on this pair: %v\n", envJoin("__").accept([]Path{n2, f2}))

	// Determinism of the refusal, which is the whole difference.
	msgs := map[string]int{}
	for range 300 {
		msgs[fmt.Sprint(envJoin("_").accept([]Path{nested, flat}))]++
	}
	fmt.Printf("\n    300 runs of the colliding case: %d distinct outcomes, all refusals\n", len(msgs))

	fmt.Println("\n    no separator is universally safe, which is why the check is over the")
	fmt.Println("    schema and not over the separator. A wider separator buys a bigger")
	fmt.Println("    margin and never a guarantee.")
}
