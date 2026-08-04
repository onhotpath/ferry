package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/onhotpath/ferry"
)

// capture is a ferrytest.T that keeps what a suite reported.
type capture struct{ lines []string }

func (c *capture) Errorf(format string, args ...any) {
	c.lines = append(c.lines, fmt.Sprintf(format, args...))
}

func (c *capture) Helper() {}

// report prints what a suite said, or says plainly that it said nothing.
func (c *capture) report(what string) {
	if len(c.lines) == 0 {
		fmt.Printf("  %s: NO REPORTS\n", what)

		return
	}

	fmt.Printf("  %s: %d report(s)\n", what, len(c.lines))

	for _, l := range c.lines {
		fmt.Printf("    - %s\n", wrap(oneLine(l)))
	}
}

// oneLine flattens a message so a table row stays a row.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// wrap keeps a long report readable in a fenced block.
func wrap(s string) string {
	const width = 100

	var (
		out  []string
		line string
	)

	for _, w := range strings.Fields(s) {
		if len(line)+len(w)+1 > width {
			out = append(out, line)
			line = "      " + w

			continue
		}

		if line == "" {
			line = w
		} else {
			line += " " + w
		}
	}

	return strings.Join(append(out, line), "\n")
}

func main() {
	sections := map[string]func(){
		"1": sectionReach,
		"2": sectionDecisive,
		"3": sectionWall,
		"4": sectionCase1,
		"5": sectionResidual,
		"6": sectionFix,
	}

	if len(os.Args) != 2 {
		fmt.Println("usage: go run ./proto137 <1..6>")
		os.Exit(2)
	}

	f, ok := sections[os.Args[1]]
	if !ok {
		fmt.Println("no such section")
		os.Exit(2)
	}

	f()
}

// show renders a recorded plane with each Value in its GoString spelling, so
// the kind is visible and the order is stable.
func show(m map[ferry.Path]ferry.Value) string {
	keys := make([]string, 0, len(m))
	by := make(map[string]ferry.Value, len(m))

	for k, v := range m {
		keys = append(keys, k.String())
		by[k.String()] = v
	}

	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%#v", k, by[k]))
	}

	return "{" + strings.Join(parts, " ") + "}"
}
