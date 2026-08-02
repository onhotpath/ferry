package main

// Entry point for #25's probes. `B25=<n|all> go run .`

import (
	"fmt"
	"os"
	"strings"
)

func h25(n, title string) {
	fmt.Printf("\n%s\n== %s  %s\n%s\n", strings.Repeat("=", 100), n, title, strings.Repeat("=", 100))
}

var b25 = []struct {
	n     string
	title string
	fn    func()
}{
	{"B1", "what a per-request load costs, and how much of it is the bind", runB1},
	{"B2", "what a held binding does to ADR-0004's dynamic tier", runB2},
	{"B3", "option (b): can a driver hand the caller a Reader at all?", runB3},
	{"B4", "option (a): the plane in the context, end to end", runB4},
	{"B5", "the presence observation: callback, recorder, or neither", runB5},
	{"B6", "a held binding under concurrent requests", runB6},
	{"B7", "the audit: what the fixtures above do not contain", runB7},
	{"B8", "the same question on the Dump side, which B7a got wrong", runB8},
	{"B9", "three worked programs, and the additive check", runB9},
	{"B10", "an inherited walk defect: a driver error on the Load path", runB10},
	{"B11", "auditing ADR-0001's \"and its Go type\" claim", runB11},
	{"B12", "and whether template generation can reach the defaults at all", runB12},
	{"B13", "#15's question asked of the value model rather than a driver", runB13},
}

func run25(which string) {
	all := which == "all"
	found := all
	for _, p := range b25 {
		if all || strings.EqualFold(which, p.n) || which == strings.TrimPrefix(p.n, "B") {
			found = true
			h25(p.n, p.title)
			p.fn()
		}
	}
	if !found {
		fmt.Fprintln(os.Stderr, "unknown probe", which)
		os.Exit(1)
	}
}
