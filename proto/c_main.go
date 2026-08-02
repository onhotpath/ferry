package main

// Entry point for #10's probes. `C10=<n|all> go run .`
//
// #10 was held until ADR-0012 was accepted, because a middleware wraps a
// `Source` and #25 owned whether the plane instance still arrives at
// construction. ADR-0012 answers that it does for every driver whose plane is
// long-lived, and that a per-request driver takes its plane from the context -
// which a wrapper passes through untouched. So wrapping survives, and this
// ticket is about what it costs rather than whether it works.

import (
	"fmt"
	"os"
	"strings"
)

func h10(n, title string) {
	fmt.Printf("\n%s\n== %s  %s\n%s\n", strings.Repeat("=", 100), n, title, strings.Repeat("=", 100))
}

var c10 = []struct {
	n     string
	title string
	fn    func()
}{
	{"C1", "the mechanism: a wrapper in both directions", runC1},
	{"C2", "the optional interfaces, and what a wrapper silently drops", runC2},
	{"C3", "redaction on dump, and what the wrapper has to be told", runC3},
	{"C4", "what dump needs that load does not", runC4},
	{"C5", "the alternatives, each refused by a rule that already exists", runC5},
}

func run10(which string) {
	all := which == "all"
	found := all
	for _, p := range c10 {
		if all || strings.EqualFold(which, p.n) || which == strings.TrimPrefix(p.n, "C") {
			found = true
			h10(p.n, p.title)
			p.fn()
		}
	}
	if !found {
		fmt.Fprintln(os.Stderr, "unknown probe", which)
		os.Exit(1)
	}
}
