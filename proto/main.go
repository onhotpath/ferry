package main

// Throwaway prototype for ferry issue #4, "Flat keys or structured paths".
// Scratch only. Never merges. Run: GOTOOLCHAIN=go1.27rc2 go run .

import (
	"fmt"
	"strings"
)

func head(s string) {
	fmt.Printf("\n%s\n%s\n", s, strings.Repeat("=", len(s)))
}

func main() {
	p1Pointer()
	p2Schema()
	p3DumpLoss()
	p4Canon()
	p5Order()
	p6Driver()
	p7Tree()
	p8Prefix()
	p9PrefixFree()
	p11Consumer()
	p12Rejections()
	p13PlaneToPlane()
	p14Transform()
	p15Separator()
	p16Dynamic()
	fmt.Println()
}
