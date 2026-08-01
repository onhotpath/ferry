package main

// P6: if core keeps the address structured, the collision moves to whoever
// flattens. Is "the driver's key function must be injective over the schema's
// address set, or it errors" a rule a driver can actually discharge, and what
// does it cost?

import (
	"fmt"
	"slices"
	"strings"
)

// keyFunc is what a driver supplies: address -> its own plane's key.
type keyFunc func(Path) string

func envKey(p Path) string {
	var parts []string
	for _, s := range p.Segments() {
		parts = append(parts, strings.ToUpper(s.Text))
	}
	return strings.Join(parts, "_")
}

func envKeyNoFold(p Path) string {
	var parts []string
	for _, s := range p.Segments() {
		parts = append(parts, s.Text)
	}
	return strings.Join(parts, "_")
}

func dottedKey(p Path) string {
	var parts []string
	for _, s := range p.Segments() {
		parts = append(parts, s.Text)
	}
	return strings.Join(parts, ".")
}

// checkInjective is the whole obligation, and it is this long.
func checkInjective(addrs []Path, f keyFunc) error {
	seen := map[string]Path{}
	var clashes []string
	for _, a := range sortedPaths(addrs) {
		k := f(a)
		if prev, ok := seen[k]; ok {
			clashes = append(clashes, fmt.Sprintf("%q <- %s and %s", k, prev, a))
			continue
		}
		seen[k] = a
	}
	if len(clashes) > 0 {
		return fmt.Errorf("key function is not injective over this schema:\n        %s",
			strings.Join(clashes, "\n        "))
	}
	return nil
}

func p6Driver() {
	head("P6  the driver-side injectivity obligation")

	sets := []struct {
		label string
		addrs []Path
	}{
		{"DB/HOST vs DB_HOST", []Path{path("DB", "HOST"), path("DB_HOST")}},
		{"case variants", []Path{path("myKey"), path("MyKey"), path("MYKEY")}},
		{"db.host vs db/host", []Path{path("db.host"), path("db", "host")}},
		{"plain nesting", []Path{path("db", "host"), path("db", "port"), path("cache", "host")}},
	}
	fns := []struct {
		label string
		f     keyFunc
	}{
		{"env, uppercase + _", envKey},
		{"env, no fold + _", envKeyNoFold},
		{"dotted, no fold", dottedKey},
	}

	for _, s := range sets {
		fmt.Printf("    schema addresses: %v\n", func() []string {
			var o []string
			for _, a := range s.addrs {
				o = append(o, a.String())
			}
			return o
		}())
		for _, fn := range fns {
			err := checkInjective(s.addrs, fn.f)
			if err == nil {
				var keys []string
				for _, a := range s.addrs {
					keys = append(keys, fn.f(a))
				}
				slices.Sort(keys)
				fmt.Printf("        %-20s ok    %v\n", fn.label, keys)
				continue
			}
			fmt.Printf("        %-20s ERROR %v\n", fn.label, err)
		}
	}

	// Determinism of the failure report, since ADR-0001 makes that an invariant.
	msgs := map[string]int{}
	for range 300 {
		msgs[fmt.Sprint(checkInjective([]Path{path("myKey"), path("MyKey"), path("MYKEY")}, envKey))]++
	}
	fmt.Printf("    300 checks of the case-variant set produced %d distinct messages\n", len(msgs))

	// Can it be checked before I/O? Only if the address set is known up front.
	fmt.Println("    the check needs the whole address set, which the compiled schema has before any I/O")
}
