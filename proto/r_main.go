package main

// Entry point for #19's probes. `P19=<n|all> go run .`

import (
	"fmt"
	"os"
	"strings"
)

func h19(n, title string) {
	fmt.Printf("\n%s\n== %s  %s\n%s\n", strings.Repeat("=", 100), n, title, strings.Repeat("=", 100))
}

var r19 = []struct {
	n     string
	title string
	fn    func()
}{
	{"R1", "the call site, and whether inference works", runR1},
	{"R2", "the declared kind, and the kinds a codec accepts", runR2},
	{"R3", "what a registration may not be", runR3},
	{"R4", "static registration against dynamic registration by reflect.Type", runR4},
	{"R5", "a predicate arm, and ADR-0005's named-duration hole", runR5},
	{"R6", "lifetime (a): global and mutable", runR6},
	{"R7", "lifetime (b): scoped, and what the schema cache must be keyed by", runR7},
	{"R8", "lifetime (c): global, frozen at first compile", runR8},
	{"R9", "decline and fall through", runR9},
	{"R10", "the proof: required, or enabled", runR10},
	{"R11", "a key codec, and where injectivity is communicated", runR11},
	{"R12", "the codec resolved into the compiled schema", runR12},
	{"R13", "registration racing a compile", runR13},
	{"R14", "three defects: String() at the zero value, and the nil interface", runR14},
	{"R16", "can Register catch the zero-value defect with no proof", runR16},
	{"R17", "the consumer's view: call sites, and the five open questions", runR17},
	{"R15", "the audit: the cases the other probes do not cover", runR15},
}

func run19(which string) {
	all := which == "all"
	found := all
	for _, p := range r19 {
		if all || strings.EqualFold(which, p.n) || which == strings.TrimPrefix(p.n, "R") {
			found = true
			h19(p.n, p.title)
			p.fn()
		}
	}
	if !found {
		fmt.Fprintln(os.Stderr, "unknown probe", which)
		os.Exit(1)
	}
}
