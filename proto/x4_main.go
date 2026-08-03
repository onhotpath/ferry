package main

// Entry point for #41's MEASUREMENT half. `X4=<n|all> go run .`
//
// Two open questions, both of which were being answered by reasoning:
//
//	Q1. Which cells of ADR-0011's Dump table depend on write order, and why
//	    only one row.
//	Q2. Should ADR-0003's segment-wise rule extend to the `Get` sequence?
//
// Nothing here changes behaviour. Every probe prints the ADR sentence it is
// testing beside the measured result, and the file adds no engine code: the
// fixtures and sinks are X2's, reused so that the two suites cannot drift.
//
// The dispatcher is an init() rather than a branch in main.go, because this
// session is allowed to add probe files and nothing else.

import (
	"fmt"
	"os"
	"strings"
)

func hX4(n, title string) {
	fmt.Printf("\n%s\n== %s  %s\n%s\n", strings.Repeat("=", 100), n, title, strings.Repeat("=", 100))
}

// saysX4 prints an ADR quotation in the shape A41 and X2 both use.
func saysX4(adr, quote string) {
	fmt.Printf("\n  %s says:\n", adr)
	for _, l := range strings.Split(quote, "\n") {
		fmt.Printf("    %s\n", strings.TrimSpace(l))
	}
	fmt.Println()
}

var x4 = []struct {
	n     string
	title string
	fn    func()
}{
	{"X1", "Q1: all twelve cells of ADR-0011's Dump table, measured", runX4a},
	{"X2", "Q1: the mechanism - the same policy under three orderings", runX4b},
	{"X3", "Q1: order-independence proved over all 40320 permutations", runX4c},
	{"X4", "Q1: every other published number that counts something stopping early", runX4d},
	{"X5", "Q1: is fail-fast a policy ferry implements, or a baseline?", runX4e},
	{"X6", "Q2: what a user actually gets from ADR-0006's presence observation", runX4f},
	{"X7", "Q2: a lazy driver's backend call order, and who can see it", runX4g},
	{"X8", "Q2: the error set, verified independent of Get order", runX4h},
	{"X9", "Q2: ferrytest's exact-set diff, and the order it actually sorts in", runX4i},
	{"X10", "Q2: the rest of the surface - harness, yaml, FirstOf, Children", runX4j},
	{"X11", "Q2: what extending the rule would cost, and what it would move", runX4k},
}

func runX4(which string) {
	all := which == "all"
	found := all
	for _, p := range x4 {
		if all || strings.EqualFold(which, p.n) || which == strings.TrimPrefix(p.n, "X") {
			found = true
			hX4(p.n, p.title)
			p.fn()
		}
	}
	if !found {
		fmt.Fprintln(os.Stderr, "unknown probe", which)
		os.Exit(1)
	}
}

func init() {
	if p := os.Getenv("X4"); p != "" {
		runX4(p)
		os.Exit(0)
	}
}
