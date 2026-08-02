package main

// Entry point for #16's probes. `E16=<n|all> go run .`

import (
	"fmt"
	"os"
	"strings"
)

func h16(n, title string) {
	fmt.Printf("\n%s\n== %s  %s\n%s\n", strings.Repeat("=", 100), n, title, strings.Repeat("=", 100))
}

var e16 = []struct {
	n     string
	title string
	fn    func()
}{
	{"E1", "the entry point, at a call site", runE1},
	{"E2", "the cache key: three components, and what it costs to hold them", runE2},
	{"E3", "resolved behaviour in the leaf, against a lookup per call", runE3},
	{"E4", "the two-level cache: the herd, the panic, and recursive types", runE4},
	{"E5", "Validate, and what it does to the freeze", runE5},
	{"E6", "the walk written once, and how big the residue is", runE6},
	{"E7", "a root leaf, and the Option rule", runE7},
	{"E8", "the audit: the cases the other probes do not cover", runE8},
}

func run16(which string) {
	all := which == "all"
	found := all
	for _, p := range e16 {
		if all || strings.EqualFold(which, p.n) || which == strings.TrimPrefix(p.n, "E") {
			found = true
			h16(p.n, p.title)
			p.fn()
		}
	}
	if !found {
		fmt.Fprintln(os.Stderr, "unknown probe", which)
		os.Exit(1)
	}
}
