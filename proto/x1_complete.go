package main

// #41 D18: ADR-0005's completeness check, which did not exist.
//
//	"A completeness check closes the loop, because a table that can be added to
//	 without adding a row is a table that drifts. Core's test iterates the
//	 identity table and the admitted kind list and asserts that every member has
//	 a proof in CoreTypes(), so extending the set without extending the table
//	 fails CI rather than silently widening an unproven guarantee."
//
// r10_proof.go prints prose about this and offers `ferrytest.Complete` over a
// USER's registry, which is the same idea pointed at the other table. Nothing
// pointed it at core's own, and core's own is the one D2 drifted in: uint8 was
// admitted by one authority, refused by the other, and had no proof, so all
// three facts were invisible at once.
//
// What is asserted here is SET MEMBERSHIP - which types and which kinds are
// admitted, against which proofs exist by name. Whether those proofs pass is
// the round-trip harness's question and the harness is being repointed at the
// entry point separately (#41 remediation item 14); a membership check does not
// run a single proof, so the two are independent.

import (
	"fmt"
	"reflect"
	"sort"
)

// coreMember is one thing core claims to support: an identity-table type or an
// admitted kind. `how` is which of the two admitted it, because the remedy
// differs - a table entry is removed by deleting a row, a kind is removed by
// deleting it from admittedKinds.
type coreMember struct {
	name string
	how  string
}

// coreMembers enumerates the admitted set from the two authorities that define
// it, and from nowhere else. Adding a type to byIdentity or a kind to
// admittedKinds adds a row here with no edit.
func coreMembers() []coreMember {
	var out []coreMember
	for t := range byIdentity {
		out = append(out, coreMember{t.String(), "identity table"})
	}
	for _, k := range admittedKinds {
		out = append(out, coreMember{k.String(), "admitted kind"})
	}
	// The two composite shapes kind admission claims by an ELEMENT kind rather
	// than by its own. They are members of the set in exactly the sense the
	// others are - kindAdmitsLeaf says yes - so a check that skipped them would
	// be the drift it exists to catch.
	for _, t := range []reflect.Type{
		reflect.TypeFor[[]byte](),
		reflect.TypeFor[[1]byte](),
	} {
		if kindAdmitsLeaf(t) {
			out = append(out, coreMember{byteish(t), "admitted kind"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// byteish spells [N]byte as a shape rather than at one length, because the
// proof table cannot carry a row per array length and the guarantee is not
// per length either.
// reflect spells both of them "uint8", and ADR-0005's proof table spells them
// "[]byte", so the join needs the ADR's spelling.
func byteish(t reflect.Type) string {
	if t.Kind() == reflect.Array {
		return "[N]byte"
	}
	return "[]byte"
}

// proofNames is the set of names the proof table discharges. It reads the
// harness's own table, so a row added there is seen here with no edit.
//
// The mapping from a member to a proof name is by NAME, which is the loosest
// possible join and is deliberate: Proof is an interface over a type parameter
// the check cannot recover, and ADR-0005's table names its rows itself.
func proofNames() map[string]bool {
	have := map[string]bool{}
	for _, p := range coreSet() {
		have[p.Name()] = true
	}
	return have
}

// proofAliases maps an admitted member's name onto the proof-table row that
// discharges it where the two spell the same thing differently. There is one:
// ADR-0005's own value list for [N]byte is the []byte row, because the array
// form is the same representation at a fixed length.
var proofAliases = map[string]string{
	"[N]byte": "[]byte",
}

// coreCompleteness is the assertion. It returns one line per member of the
// admitted set that has no proof, and nil when the loop is closed.
func coreCompleteness() []string {
	have := proofNames()
	var missing []string
	for _, m := range coreMembers() {
		n := m.name
		if a, ok := proofAliases[n]; ok && have[a] {
			continue
		}
		if have[n] {
			continue
		}
		missing = append(missing, fmt.Sprintf("%s (%s) has no row in CoreTypes()", n, m.how))
	}
	return missing
}

// mustCoreComplete is what ADR-0005 means by "fails CI". It is called from
// x1_complete_test.go, which is the only reason a _test.go file exists on this
// branch at all.
func mustCoreComplete(fail func(string, ...any)) {
	missing := coreCompleteness()
	if len(missing) == 0 {
		return
	}
	fail("ferry: the core type set has %d member(s) with no proof in CoreTypes():\n  %s",
		len(missing), joinLines(missing))
}
