// Package perschema is a throwaway prototype for issue #217: may a Source carry
// per-schema configuration?
//
// It never merges.
//
// ADR-0004's lifetime table says a Source holds driver config that "never
// changes, you constructed it". A schema arrives at Bind. Some driver designs
// want configuration that is about the schema rather than about the plane, and
// #210 measured one of them - Repeatable(name) on a multimap Source - silently
// zeroing a field when one Source is shared by two schemas.
//
// The rule written from that result, and the thing this prototype tests, is:
//
//	A Source may carry per-schema configuration only where it is checkable
//	against the AddressSet at Bind. The name-exists half is checkable; the
//	is-it-a-container half is not.
//
// The package holds one multimap plane - a header block, exactly
// map[string][]string - and five per-schema configurations spanning two axes
// the rule conflates: whether a wrong declaration is checkable at Bind, and
// whether a wrong declaration is loud or silent at load.
//
// Nothing here asserts an expected answer. Every test prints what happened.
package perschema
