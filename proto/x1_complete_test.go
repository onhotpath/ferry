package main

import "testing"

// ADR-0005: "Core's test iterates the identity table and the admitted kind list
// and asserts that every member has a proof in CoreTypes(), so extending the
// set without extending the table fails CI rather than silently widening an
// unproven guarantee."
//
// This is that test. It is RED on this branch, and the red is the finding: the
// admitted kind list holds fourteen kinds and ADR-0005's own proof table holds
// eleven rows, seven of which are kinds. See X1=2.
func TestCoreTypesComplete(t *testing.T) {
	mustCoreComplete(t.Errorf)
}
