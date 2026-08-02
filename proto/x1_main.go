package main

// Entry point for #41's compiler-side repairs. `X1=<n|all> go run .`
//
// One probe per deviation this session closed. Each prints the ADR sentence it
// is implementing and the measured result, and each is written so that the
// BEFORE is visible in the audit suite's own output (A41=all) rather than
// asserted here.

import (
	"fmt"
	"os"
	"strings"
)

func h41c(n, title string) {
	fmt.Printf("\n%s\n== %s  %s\n%s\n", strings.Repeat("=", 100), n, title, strings.Repeat("=", 100))
}

func quoteADR(lines ...string) {
	for _, l := range lines {
		fmt.Printf("  > %s\n", l)
	}
	fmt.Println()
}

var x1 = []struct {
	n     string
	title string
	fn    func()
}{
	{"X1", "D2: uint8 is admitted, and there is one admission authority", runX1_1},
	{"X2", "D18: ADR-0005's completeness check, run for the first time", runX1_2},
	{"X3", "D4: Register runs the codec against the zero value", runX1_3},
	{"X4", "D11: a promoted embedded pointer is refused", runX1_4},
	{"X5", "D9: core scans the raw struct tag instead of calling Lookup", runX1_5},
	{"X6", "D17: three diagnostic tiers, edit distance, and the neighbourhood", runX1_6},
	{"X7", "D5: the key-codec opt-in is the rule, and it says why", runX1_7},
	{"X8", "D3: the chain is on, and a declaration beats an inference", runX1_8},
}

func runX1(which string) {
	all := which == "all"
	found := all
	for _, p := range x1 {
		if all || strings.EqualFold(which, p.n) || which == strings.TrimPrefix(p.n, "X") {
			found = true
			h41c(p.n, p.title)
			p.fn()
		}
	}
	if !found {
		fmt.Fprintln(os.Stderr, "unknown probe", which)
		os.Exit(1)
	}
}

func trimTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
