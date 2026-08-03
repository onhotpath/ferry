package main

import "testing"

// #35 closes #41's D18 by giving the seven admitted kinds proof rows, and it
// closes #28's finding by putting the golden column on the proof that runs
// through the entry point.
//
// TestCoreTypesComplete, over harness.go's table, stays RED on this branch on
// purpose: the two tables are the measurement, and deleting the old one would
// delete the evidence. #35's ADR decides that one of them ships.
func TestZ35CoreComplete(t *testing.T) {
	for _, s := range ZComplete(NewRegistry(), ZCoreTypes()...) {
		t.Errorf("core type set: %s", s)
	}
}

func TestZ35CoreConsumers(t *testing.T) {
	zCoreTest(t)
	zRegistrantTest(t)
	zUserTest(t)
	zDriverTest(t, t.TempDir())
}
