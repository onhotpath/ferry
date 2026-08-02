package main

// Entry point for #12's probes. `P12=<n|all> go run .`

import (
	"fmt"
	"os"
	"strings"
)

func h12(n, title string) {
	fmt.Printf("\n%s\n== %s  %s\n%s\n", strings.Repeat("=", 100), n, title, strings.Repeat("=", 100))
}

func run12(which string) {
	all := which == "all"
	run := func(n string) bool { return all || which == n }

	if run("1") {
		h12("P1", "the interface census")
		runCensus()
	}
	if run("2") {
		h12("P2", "paired selection versus per-direction selection")
		runPairing()
	}
	if run("3") {
		h12("P3", "receiver and addressability mechanics")
		runReceivers()
	}
	if run("4") {
		h12("P4", "the text arm before kind: the full diff, and what it costs")
		runBeforeKind()
		runArtefact()
	}
	if run("5") {
		h12("P5", "the JSON arm on a non-JSON plane")
		runJSONArm()
	}
	if run("6") {
		h12("P6", "what MarshalerTo buys at ferry's boundary")
		runMarshalerTo()
	}
	if run("7") {
		h12("P7", "TextAppender against TextMarshaler")
		runAppender()
	}
	if run("8") {
		h12("P8", "Absent, Null and the empty string at a codec")
		runAbsent()
	}
	if run("9") {
		h12("P9", "what kind a codec declares, and what it sees")
		runDonor()
	}
	if run("10") {
		h12("P10", "omission, defaults and the encoder: the composed order")
		runOmit()
	}
	if run("11") {
		h12("P11", "context in a codec signature")
		runContext()
	}
	if run("12") {
		h12("P12", "the half pair: diagnosis and blast radius")
		runHalfPair()
	}
	if run("13") {
		h12("P13", "a text-arm type as a map key")
		runMapKey()
	}
	if run("14") {
		h12("P14", "the audit: the case the fixtures do not cover")
		runAudit12()
	}
	if !all && !strings.Contains("1 2 3 4 5 6 7 8 9 10 11 12 13 14", which) {
		fmt.Fprintln(os.Stderr, "unknown probe", which)
		os.Exit(1)
	}
}
