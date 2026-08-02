package main

// Entry point for #15's probes. `W15=<n|all> go run .`
//
// Everything under this entry point that touches a real hive is behind
// //go:build windows. The half that does not - ADR-0003's address model
// against a Registry path, and what an Enumerator can say about a plane with
// no list type - runs anywhere, because it is a question about ferry rather
// than about Windows.

import (
	"fmt"
	"os"
	"strings"
)

func h15(n, title string) {
	fmt.Printf("\n%s\n== %s  %s\n%s\n", strings.Repeat("=", 100), n, title, strings.Repeat("=", 100))
}

type w15probe struct {
	n     string
	title string
	fn    func()
	win   bool // needs a real hive
}

func run15(which string) {
	all := which == "all"
	found := all
	for _, p := range w15probes() {
		if !(all || strings.EqualFold(which, p.n) || which == strings.TrimPrefix(p.n, "W")) {
			continue
		}
		found = true
		h15(p.n, p.title)
		if p.win && !onWindows {
			fmt.Println("SKIPPED: needs a real hive, and this is not Windows.")
			continue
		}
		p.fn()
	}
	if !found {
		fmt.Fprintln(os.Stderr, "unknown probe", which)
		os.Exit(1)
	}
}
