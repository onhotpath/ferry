// Package ferrytest is ferry's driver contract in executable form: the
// conformance suite, the round-trip property harness, the memory plane and the
// recording sink.
//
// It ships from core rather than from a module of its own because a third
// party's conformance suite settles nothing - two disagreeing suites would bind
// no driver author. The suite is worth something only when it ships from the
// same place as the rule, because it is the rule (ADR-0002).
package ferrytest
