package main

// Entry point for #45. `Y45=<n|all> go run .`
//
// #45 is a gap BETWEEN two Accepted ADRs rather than a defect in either.
// ADR-0009 makes the injectivity obligation opt-in for a REGISTRATION;
// ADR-0007 independently admits a chain-claimed `String` type as a map key and
// names the reason there is nowhere to put the opt-in - "a chain arm is a codec
// nobody registered". So the refusal is lifted by DELETING a line.
//
// The ticket asks three questions and this suite answers each by running:
//
//	Q1. May the chain claim a map key at all, or is keying a map
//	    registration-only?
//	Q2. If it may, where is the obligation communicated, given no call site?
//	Q3. Has `ferrytest.Injective` anything to say when there is no registrant?
//
// Nothing here changes engine behaviour. Y6 builds the two candidate rules in a
// throwaway copy of the admission function so their cost can be read rather
// than argued; the engine's own `validMapKey` is untouched.

import (
	"fmt"
	"os"
	"strings"
)

func hY(n, title string) {
	fmt.Printf("\n%s\n== %s  %s\n%s\n", strings.Repeat("=", 100), n, title, strings.Repeat("=", 100))
}

func saysY(adr, quote string) {
	fmt.Printf("\n  %s says:\n", adr)
	for _, l := range strings.Split(quote, "\n") {
		fmt.Printf("    %s\n", strings.TrimSpace(l))
	}
	fmt.Println()
}

var y45 = []struct {
	n     string
	title string
	fn    func()
}{
	{"Y1", "the three rows of #45's table, run end to end", runY1},
	{"Y2", "the population: which types can reach the hole at all", runY2},
	{"Y3", "is the chain's text actually injective, and where does it collapse", runY3},
	{"Y4", "what the silent collapse costs, measured on a plausible user type", runY4},
	{"Y5", "Q1: what refusing would cost, and the asymmetry it creates", runY5},
	{"Y6", "Q2: the four candidate rules, each built and read", runY6},
	{"Y7", "Q3: can injectivity be checked with no registrant and no value list", runY7},
	{"Y8", "the recommendation, and what it does not close", runY8},
}

func runY45(which string) {
	all := which == "all"
	found := all
	for _, p := range y45 {
		if all || strings.EqualFold(which, p.n) || which == strings.TrimPrefix(p.n, "Y") {
			found = true
			hY(p.n, p.title)
			p.fn()
		}
	}
	if !found {
		fmt.Fprintln(os.Stderr, "unknown probe", which)
		os.Exit(1)
	}
}

func init() {
	if p := os.Getenv("Y45"); p != "" {
		runY45(p)
		os.Exit(0)
	}
}
