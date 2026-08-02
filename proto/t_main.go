package main

// Entry point for #14's probes. `T14=<n|all> go run .`

import (
	"fmt"
	"os"
	"strings"
)

func h14(n, title string) {
	fmt.Printf("\n%s\n== %s  %s\n%s\n", strings.Repeat("=", 100), n, title, strings.Repeat("=", 100))
}

var t14 = []struct {
	n     string
	title string
	fn    func()
}{
	{"T1", "is the defaulted value reachable at all", runT1},
	{"T2", "the recipe, and what it costs", runT2},
	{"T3", "the artefact, at four annotation levels", runT3},
	{"T4", "the annotation channel, and the half that is hard", runT4},
	{"T5", "which planes can be templated, and what one that cannot reports", runT5},
	{"T6", "where a comment's words come from", runT6},
	{"T7", "the API surface, argued last", runT7},
	{"T8", "the audit: the cases the other probes do not contain", runT8},
	{"T9", "reconciliation with ADR-0012 (#25)", runT9},
}

func run14(which string) {
	all := which == "all"
	found := all
	for _, p := range t14 {
		if all || strings.EqualFold(which, p.n) || which == strings.TrimPrefix(p.n, "T") {
			found = true
			h14(p.n, p.title)
			p.fn()
		}
	}
	if !found {
		fmt.Fprintln(os.Stderr, "unknown probe", which)
		os.Exit(1)
	}
}
