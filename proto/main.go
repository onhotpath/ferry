package main

// Throwaway prototype for ferry issue #5, "Source and Sink interface
// signatures". Scratch only. Never merges.
//
// Run:        cd proto && GOTOOLCHAIN=go1.27rc2 go run .
// Benchmarks: GOTOOLCHAIN=go1.27rc2 go test -run=NONE -bench=. -benchmem .

import (
	"fmt"
	"strings"
)

func head(s string) {
	fmt.Printf("\n%s\n%s\n", s, strings.Repeat("=", len(s)))
}

func main() {
	p1Token()
	p2Absence()
	p3Amplify()
	p4Cost()
	p5DumpTyped()
	p6GroupArm()
	p7ReadOnly()
	p8Enumerate()
	p9Memo()
	p10Fidelity()
	p11Dynamic()
	p12Lifetimes()
	p13Slim()
	p14Closer()
	p15Release()
	p16Final()
	fmt.Println()
}
