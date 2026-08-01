package main

// P14: P11 turned up something the ADR does not answer. The env driver rejects
// a segment "feature-flags", because a hyphen is not legal in an environment
// variable name. That is a perfectly ordinary thing to write in a config
// struct, so "reject" is the wrong answer and the driver should transform.
//
// But transforming is exactly what folding is: a many-to-one map. So the
// question is whether the injectivity rule is enough to make transforming safe,
// or whether transforming needs a rule of its own.

import (
	"fmt"
	"strings"
)

func envDriverTransforming() driver {
	return driver{"env+xform", func(p Path) string {
		var b []string
		for s := range p.SegmentsSeq() {
			t := strings.Map(func(r rune) rune {
				switch {
				case r >= 'a' && r <= 'z':
					return r - 32
				case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
					return r
				default:
					return '_' // hyphen, dot, space, anything
				}
			}, s.Text)
			b = append(b, t)
		}
		return strings.Join(b, "_")
	}, func(p Path) error {
		for s := range p.SegmentsSeq() {
			if s.Text == "" {
				return fmt.Errorf("%s: empty segment has no environment variable name", p)
			}
		}
		return nil
	}}
}

func p14Transform() {
	head("P14  may a driver transform, or must it reject?")

	strict, xform := envDriver(), envDriverTransforming()

	cases := []struct {
		why   string
		addrs []Path
	}{
		{"feature-flags alone", []Path{path("feature-flags", "beta")}},
		{"feature-flags AND feature_flags", []Path{path("feature-flags"), path("feature_flags")}},
		{"feature-flags AND feature.flags", []Path{path("feature-flags"), path("feature.flags")}},
		{"a dotted map key alone", []Path{path("limits", "eu.west")}},
		{"non-ASCII alone", []Path{path("Kéy")}},
		{"ordinary schema", []Path{path("db", "host"), path("db", "port")}},
	}

	fmt.Printf("    %-36s %-10s %s\n", "schema", "strict", "transforming")
	for _, c := range cases {
		fmt.Printf("    %-36s %-10s %s\n", c.why, verdict(strict.accept(c.addrs)), verdict(xform.accept(c.addrs)))
	}

	fmt.Println("\n    what the transforming driver produces where it succeeds:")
	for _, p := range []Path{path("feature-flags", "beta"), path("limits", "eu.west"), path("Kéy")} {
		fmt.Printf("        %-26s -> %s\n", p, xform.key(p))
	}

	fmt.Println("\n    and the reason it refuses the pair:")
	fmt.Printf("        %v\n", xform.accept([]Path{path("feature-flags"), path("feature_flags")}))

	fmt.Println("\n    the point: transforming is folding, and folding is safe exactly when")
	fmt.Println("    the injectivity check passes. One rule covers both. The strict driver")
	fmt.Println("    is not safer, it is only less useful.")

	// Does the transform stay injective on a realistic schema? Check the P11 one.
	var addrs []Path
	var pairs []pair
	dumpAddrsOf(&pairs)
	for _, p := range pairs {
		addrs = append(addrs, p.Addr)
	}
	fmt.Printf("\n    transforming driver over the P11 schema: %s\n", verdict(xform.accept(addrs)))
	fmt.Printf("    non-ASCII collapses, so these two would now collide: %s\n",
		verdict(xform.accept([]Path{path("Kéy"), path("K_y")})))
}
