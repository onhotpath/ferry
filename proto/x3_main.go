package main

// Entry point for #41's ADR-0005 / ADR-0008 contradiction. `X3=<n|all> go run .`
//
// The question this suite exists to answer:
//
//	ADR-0005's three-outcomes table lists net.IPNet, net.TCPAddr and
//	sql.NullString as "admitted, round-trips", with the addresses each one
//	lands on. ADR-0008 then landed a rule ADR-0005 never saw:
//
//	  "An exported, named struct field with no ferry tag is a schema compile
//	   error. ferry never invents a segment name."
//
//	All three are structs in other people's packages, whose exported fields
//	nobody can add a ferry tag to. So they do not round-trip differently.
//	They do not compile at all.
//
// Nothing here amends an ADR. Which one gives way is the repo owner's call.
// Every claim in the #41 report has a probe in this file behind it.

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func h41t(n, title string) {
	fmt.Printf("\n%s\n== %s  %s\n%s\n", strings.Repeat("=", 100), n, title, strings.Repeat("=", 100))
}

var x3 = []struct {
	n     string
	title string
	fn    func()
}{
	{"X1", "the population: every third-party type an ADR names, compiled", runX3_1},
	{"X2", "whether registration rescues them, end to end on three planes", runX3_2},
	{"X3", "a custom encoder/decoder for net.IPNet, and the zero value", runX3_3},
	{"X4", "whether the two engines disagree", runX3_4},
	{"X5", "the rows the chain already moved, in the other direction", runX3_5},
}

func runX3(which string) {
	all := which == "all"
	found := all
	for _, p := range x3 {
		if all || strings.EqualFold(which, p.n) || which == strings.TrimPrefix(p.n, "X") {
			found = true
			h41t(p.n, p.title)
			p.fn()
		}
	}
	if !found {
		fmt.Fprintln(os.Stderr, "unknown probe", which)
		os.Exit(1)
	}
}

// quoteX3 prints an ADR sentence the way x1_main.go's quoteADR does, so the
// three #41 suites read the same.
func quoteX3(lines ...string) {
	for _, l := range lines {
		fmt.Printf("  > %s\n", l)
	}
	fmt.Println()
}

// x3Ctx is context.Background(), named so x3_p5.go's one-liner reads.
func x3Ctx() context.Context { return context.Background() }
