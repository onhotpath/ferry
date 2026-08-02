package main

// X1 and X2: the type set's two repairs.

import (
	"fmt"
	"reflect"
)

func runX1_1() {
	quoteADR(
		"ADR-0005: the kind table admits bool, string, int, int8, int16, int32,",
		"int64, uint, uint8, uint16, uint32, uint64, float32, float64.")

	fmt.Println("  one single-field struct per admitted kind, through Compile:")
	for _, tc := range []struct {
		label string
		t     reflect.Type
	}{
		{"bool", reflect.TypeFor[struct {
			V bool `ferry:"v"`
		}]()},
		{"string", reflect.TypeFor[struct {
			V string `ferry:"v"`
		}]()},
		{"int", reflect.TypeFor[struct {
			V int `ferry:"v"`
		}]()},
		{"int8", reflect.TypeFor[struct {
			V int8 `ferry:"v"`
		}]()},
		{"int16", reflect.TypeFor[struct {
			V int16 `ferry:"v"`
		}]()},
		{"int32", reflect.TypeFor[struct {
			V int32 `ferry:"v"`
		}]()},
		{"int64", reflect.TypeFor[struct {
			V int64 `ferry:"v"`
		}]()},
		{"uint", reflect.TypeFor[struct {
			V uint `ferry:"v"`
		}]()},
		{"uint8", reflect.TypeFor[struct {
			V uint8 `ferry:"v"`
		}]()},
		{"uint16", reflect.TypeFor[struct {
			V uint16 `ferry:"v"`
		}]()},
		{"uint32", reflect.TypeFor[struct {
			V uint32 `ferry:"v"`
		}]()},
		{"uint64", reflect.TypeFor[struct {
			V uint64 `ferry:"v"`
		}]()},
		{"float32", reflect.TypeFor[struct {
			V float32 `ferry:"v"`
		}]()},
		{"float64", reflect.TypeFor[struct {
			V float64 `ferry:"v"`
		}]()},
		{"[]byte", reflect.TypeFor[struct {
			V []byte `ferry:"v"`
		}]()},
		{"[4]byte", reflect.TypeFor[struct {
			V [4]byte `ferry:"v"`
		}]()},
	} {
		_, err := compileSchema2(tc.t, defaultOpts())
		status := "compiles"
		if err != nil {
			status = "REFUSED: " + err.Error()
		}
		fmt.Printf("    %-10s %s\n", tc.label, status)
	}
	fmt.Println("    ^ uint8 was the one that did not, on the tip. A41=2 measured it as")
	fmt.Println("      `ferry: /v: unsupported type uint8 (kind uint8)`.")

	fmt.Println("\n  the two authorities, now one:")
	fmt.Printf("    %-10s %-28s %s\n", "kind", "kindClassify (typeset.go)", "kindLeaf (e_schema.go)")
	agree := true
	for _, t := range []reflect.Type{
		reflect.TypeFor[bool](), reflect.TypeFor[string](),
		reflect.TypeFor[int](), reflect.TypeFor[int8](), reflect.TypeFor[int16](),
		reflect.TypeFor[int32](), reflect.TypeFor[int64](),
		reflect.TypeFor[uint](), reflect.TypeFor[uint8](), reflect.TypeFor[uint16](),
		reflect.TypeFor[uint32](), reflect.TypeFor[uint64](),
		reflect.TypeFor[float32](), reflect.TypeFor[float64](),
		reflect.TypeFor[[]byte](), reflect.TypeFor[[]string](),
		reflect.TypeFor[complex128](),
	} {
		a, b := kindClassify(t) == shapeLeaf, kindLeaf(t)
		if a != b {
			agree = false
		}
		fmt.Printf("    %-10s %-28v %v\n", t, a, b)
	}
	fmt.Printf("\n    the two agree at every kind: %v\n", agree)
	fmt.Println("    They agree because there is nothing left to disagree: both call")
	fmt.Println("    kindAdmitsLeaf, which reads admittedKinds, which is the list. That is")
	fmt.Println("    ADR-0010's duplication axis 1 closed inside the type set.")
}

func runX1_2() {
	quoteADR(
		"ADR-0005: \"A completeness check closes the loop, because a table that can",
		"be added to without adding a row is a table that drifts. Core's test",
		"iterates the identity table and the admitted kind list and asserts that",
		"every member has a proof in CoreTypes(), so extending the set without",
		"extending the table fails CI rather than silently widening an unproven",
		"guarantee.\"")

	have := proofNames()
	fmt.Printf("  the admitted set, from its two authorities (%d members):\n", len(coreMembers()))
	for _, m := range coreMembers() {
		n := m.name
		ok := have[n]
		via := ""
		if !ok {
			if a, is := proofAliases[n]; is && have[a] {
				ok, via = true, " (via the "+a+" row)"
			}
		}
		mark := "MISSING"
		if ok {
			mark = "proved"
		}
		fmt.Printf("    %-16s %-16s %s%s\n", n, m.how, mark, via)
	}

	missing := coreCompleteness()
	fmt.Printf("\n  CoreTypes() holds %d rows. The check reports %d member(s) with no proof.\n",
		len(coreSet()), len(missing))
	if len(missing) == 0 {
		fmt.Println("  COMPLETENESS: PASS")
	} else {
		fmt.Println("  COMPLETENESS: FAIL")
		for _, m := range missing {
			fmt.Printf("      %s\n", m)
		}
		fmt.Println("\n  That red is the finding, and it is not this session's to resolve.")
		fmt.Println("  ADR-0005's own harness list is `bool string int int8 uint64 float64")
		fmt.Println("  float32 []byte time.Duration time.Time []string`, which is eleven rows")
		fmt.Println("  over fourteen admitted kinds. The seven above are admitted by kind,")
		fmt.Println("  round-trip through the walk, and carry no proof - so ADR-0005's")
		fmt.Println("  \"11 of 11 core types\" is 11 of 11 ROWS and not 11 of 11 members.")
		fmt.Println("  Closing it means adding rows to coreSet() in harness.go, which is")
		fmt.Println("  where the harness is being repointed at the entry point separately.")
	}

	fmt.Println("\n  and the drift it exists to catch, demonstrated:")
	saved := admittedKinds
	savedSet := admittedKind
	admittedKinds = append(append([]reflect.Kind{}, saved...), reflect.Complex128)
	admittedKind = map[reflect.Kind]bool{}
	for _, k := range admittedKinds {
		admittedKind[k] = true
	}
	fmt.Printf("    admitting complex128 with no proof -> the check now reports %d, including:\n",
		len(coreCompleteness()))
	for _, m := range coreCompleteness() {
		if len(m) >= 10 && m[:10] == "complex128" {
			fmt.Printf("      %s\n", m)
		}
	}
	admittedKinds, admittedKind = saved, savedSet
	fmt.Printf("    reverted -> %d\n", len(coreCompleteness()))

	fmt.Println("\n  The check asserts SET MEMBERSHIP and runs no proof, so it is independent")
	fmt.Println("  of where the harness points. TestCoreTypesComplete in")
	fmt.Println("  x1_complete_test.go is the CI half; it is red for the seven above.")
}
