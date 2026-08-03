package main

// Entry point for #41's runtime half. `X2=<n|all> go run .`
//
// Nine probes, one per item of the remediation this session took: the
// scheduler default, the error model, `required` at a composite, the array
// bound, the Close element, the round-trip harness through the engine, the
// flattening plane's declaration, Dump's two phases, and concurrency.
//
// Every probe prints the ADR sentence it is measuring and then the measured
// result, so a reader can check the claim rather than the code.

import (
	"fmt"
	"os"
	"strings"
)

func hX2(n, title string) {
	fmt.Printf("\n%s\n== %s  %s\n%s\n", strings.Repeat("=", 100), n, title, strings.Repeat("=", 100))
}

// saysX2 prints an ADR quotation in the shape the A41 suite uses, so the two
// read the same.
func saysX2(adr, quote string) {
	fmt.Printf("\n  %s says:\n", adr)
	for _, l := range strings.Split(quote, "\n") {
		fmt.Printf("    %s\n", strings.TrimSpace(l))
	}
	fmt.Println()
}

var x2 = []struct {
	n     string
	title string
	fn    func()
}{
	{"X1", "D6: aggregation is the default, not an Option a caller must pass", runX2a},
	{"X2", "D7: ferry's own message text never carries a plane-supplied value", runX2b},
	{"X3", "D8: one Error, four classes, one aggregate constructor, sorted at construction", runX2c},
	{"X4", "D12: required at a struct and a *struct, and the one suppression bit", runX2d},
	{"X5", "D13: an index the array cannot hold is loud", runX2e},
	{"X6", "D14: a Close failure is an element of the aggregate", runX2f},
	{"X7", "the round-trip harness, through the engine, on three planes", runX2g},
	{"X8", "Dump's two phases, and the Committer interleaving", runX2h},
	{"X9", "ADR-0010's and ADR-0012's concurrency claims under the new default", runX2i},
	{"X10", "ADR-0003's segment-wise rule, and the interface that was changing it", runX2j},
}

func runX2(which string) {
	all := which == "all"
	found := all
	for _, p := range x2 {
		if all || strings.EqualFold(which, p.n) || which == strings.TrimPrefix(p.n, "X") {
			found = true
			hX2(p.n, p.title)
			p.fn()
		}
	}
	if !found {
		fmt.Fprintln(os.Stderr, "unknown probe", which)
		os.Exit(1)
	}
}
