package main

// X3-4. Do the two engines disagree?
//
// The audit's section 1 is that the chain is two chains, and its highest-value
// row is that the round-trip harness ran through walk.go rather than through
// the engine. #41 item 6 repointed the harness. This probe asks the remaining
// half of that question: walk.go is still in the binary, still compiled, and
// still reachable - and it has NO mandatory-tag rule at all.
//
//	walk.go:121  func fieldName(f reflect.StructField) string {
//	                 if tag := f.Tag.Get("ferry"); tag != "" { return tag }
//	                 return f.Name
//	             }
//
// So the Go field name IS the default on one engine and is a compile error on
// the other. That is the shape of defect this whole audit exists to find, and
// it is not one type's problem: it is every untagged struct in the prototype.

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"reflect"
	"sort"
	"strings"
)

func runX3_4() {
	fmt.Println("--- X3-4a: the two admission authorities, side by side ---")
	fmt.Println("    left:  compile() from walk.go, the superseded walk")
	fmt.Println("    right: compileOnce() -> compileSchema2 from e_schema.go, the engine")
	fmt.Println()
	fmt.Printf("    %-18s %-34s %s\n", "type", "walk.go compile()", "e_schema.go")
	fmt.Printf("    %-18s %-34s %s\n", strings.Repeat("-", 18), strings.Repeat("-", 34), strings.Repeat("-", 34))
	disagree := 0
	for _, r := range x3Population() {
		h := x3Holder(r.typ)
		_, oldErr := compile(h)
		nv := x3Compile(r.typ)
		oldS := "compiles"
		if oldErr != nil {
			oldS = "refused"
		}
		newS := "compiles"
		if nv.label != "compiles" {
			newS = "refused"
			if nv.fieldRule {
				newS = "refused (field rule)"
			}
		}
		flag := ""
		if oldS != strings.SplitN(newS, " ", 2)[0] {
			flag = "  <- DISAGREE"
			disagree++
		}
		fmt.Printf("    %-18s %-34s %s%s\n", r.name, oldS, newS, flag)
	}
	fmt.Printf("\n    %d of %d types get a different answer from the two engines.\n", disagree, len(x3Population()))

	fmt.Println("\n--- X3-4b: the disagreement WAS not confined to third-party types ---")
	fmt.Println("    fieldName() invents the Go field name for ANY untagged field, so the")
	fmt.Println("    two engines disagreed about the prototype's own fixtures too. Conf is")
	fmt.Println("    main.go's type and the default `go run .` compiles it on walk.go:")
	ca, cerr := compile(reflect.TypeFor[Conf]())
	fmt.Printf("      walk.go   compile(Conf) -> err=%v, %d addresses, first=%v\n", cerr, len(ca), ca[0])
	o := defaultOpts()
	o.reg = NewRegistry()
	s2, e2 := compileOnce(reflect.TypeFor[Conf](), o)
	n := 0
	if e2 != nil {
		n = len(errLines(e2))
	}
	if e2 == nil {
		fmt.Printf("      e_schema  Compile[Conf] -> err=<nil>, %d addresses, first=%v\n", len(s2.addrs), s2.addrs[0])
	} else {
		fmt.Printf("      e_schema  Compile[Conf] -> %d refusals, first = %s\n", n, x3One(e2))
	}
	fmt.Println(`    AS FOUND, walk.go gave 13 addresses and e_schema gave 11 REFUSALS for
    this one type, so ` + "`" + `go run .` + "`" + ` with no env var set - the suite a reader runs
    FIRST - exercised the naming rule ADR-0008 refused, on a struct the
    current engine would not compile.

    #41 tagged Conf and Inner with the segment names walk.go was already
    inventing. The naming-rule disagreement is closed: 11 refusals to 0,
    and every address and every published number on this fixture is
    unchanged on walk.go.

    AND A DIFFERENT DISAGREEMENT IS NOW VISIBLE UNDERNEATH IT, which the
    refusals were hiding. The two engines still mint different STATIC
    address sets for the same compiling type:`)
	if e2 == nil {
		onlyNew, onlyOld := x3AddrDiff(ca, s2.addrs)
		fmt.Printf("      only in e_schema: %v\n", onlyNew)
		fmt.Printf("      only in walk.go : %v\n", onlyOld)
	}
	fmt.Println(`    That is not ADR-0008's rule at all. walk.go puts a dynamic composite
    into the STATIC set as a wildcard - /Tags/* - where e_schema puts the
    container address it is enumerated under, and e_schema additionally
    mints /Opt for the optional section itself. Which of the two is
    ADR-0003's two-tier model is a real question and it is NOT this
    ticket's: the static set is what Bind receives, so the answer changes
    a DRIVER contract. Recorded, not decided.

    NOR DOES ANY OF IT RETIRE walk.go, and that decision is recorded
    rather than dodged: it is the engine ADR-0003, ADR-0004, ADR-0005,
    ADR-0007 and ADR-0009's numbers were taken on, 34 probe files call it,
    and X3-4c below reproduces ADR-0005's published /IP and /Mask row ON
    IT. Deleting it deletes the ability to reproduce published evidence,
    which is the opposite of what #41 exists to protect. What was wrong
    was the DEFAULT fixture, not the file.

    The 12-of-26 disagreement above stands and is not fixable by tagging,
    because those are exported fields in other people's packages -
    ADR-0005's registration answer, X3-2.`)

	fmt.Println("\n--- X3-4c: what walk.go actually does with net.IPNet ---")
	fmt.Println("    This is ADR-0005's published row, reproduced - on the engine that")
	fmt.Println("    produced it, which is not the engine anything else runs on now.")
	_, ipn, _ := net.ParseCIDR("10.0.0.0/8")
	vals, derr := dump(reflect.ValueOf(x3Box[net.IPNet]{*ipn}))
	fmt.Printf("      walk.go dump -> err=%v\n", derr)
	dumpAddrs(vals)
	var back x3Box[net.IPNet]
	lerr := load(vals, reflect.ValueOf(&back).Elem())
	fmt.Printf("      walk.go load -> err=%v, value=%v\n", lerr, back.V.String())
	fmt.Println("      ADR-0005 says: `/IP` and `/Mask`, two byte blobs. The ADDRESSES are")
	fmt.Println("      confirmed, and they are the Go field names IP and Mask, invented by")
	fmt.Println("      fieldName() - which is the rule ADR-0008 refused, and ADR-0003 does")
	fmt.Println("      not fold, so those segments are Go identifiers on the plane.")
	fmt.Println("      The KINDS have already moved: /v/IP is a string and not a byte blob,")
	fmt.Println("      because #41 D3 turned ADR-0007's chain on and net.IP carries the text")
	fmt.Println("      pair. So even on walk.go the published `two byte blobs` is now one")
	fmt.Println("      string and one blob, and that half is stale by ADR-0005's own")
	fmt.Println("      permission rather than against it (X3-5).")

	fmt.Println("\n      and sql.NullString, likewise:")
	nv, _ := dump(reflect.ValueOf(x3Box[sql.NullString]{sql.NullString{String: "x", Valid: true}}))
	dumpAddrs(nv)
	fmt.Println("\n      and net.TCPAddr, likewise:")
	tv, _ := dump(reflect.ValueOf(x3Box[net.TCPAddr]{net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 80}}))
	dumpAddrs(tv)

	fmt.Println("\n--- X3-4d: which engine is ADR-0005's harness on NOW? ---")
	fmt.Println("    #41 item 6 repointed it at Dump and Load, so the harness is on")
	fmt.Println("    e_schema.go. That closes the audit's highest-value row and it also")
	fmt.Println("    means ADR-0005's three-outcomes table is now the ONLY published")
	fmt.Println("    ADR-0005 measurement still taken on walk.go, because CoreTypes()")
	fmt.Println("    contains no third-party struct:")
	names := []string{}
	for _, p := range coreSet() {
		names = append(names, p.Name())
	}
	fmt.Printf("      CoreTypes() = %v\n", names)
	fmt.Println("      Not one of them is a struct in another package, so the harness")
	fmt.Println("      would not have caught this even after being repointed. The table")
	fmt.Println("      is unprotected by the property suite, in both directions.")

	fmt.Println("\n--- X3-4e: and the harness cannot be extended to cover it without a codec ---")
	fmt.Println("    Adding Type(\"net.IPNet\", eq, values...) to CoreTypes() with no")
	fmt.Println("    registration in scope compiles the proof and then fails at Dump:")
	pl := memoryPlane()
	src, sink := pl.Open()
	ctx := context.Background()
	err := Dump(ctx, x3Box[net.IPNet]{*ipn}, sink, WithRegistry(NewRegistry()))
	fmt.Printf("      Dump[struct{ V net.IPNet }] with a fresh registry -> %s\n", x3One(err))
	_, lerr2 := Load[x3Box[net.IPNet]](ctx, src, WithRegistry(NewRegistry()))
	fmt.Printf("      Load[struct{ V net.IPNet }] with a fresh registry -> %s\n", x3One(lerr2))
	fmt.Println("    which is the whole finding, restated at the door every user comes in")
	fmt.Println("    through.")
}

// x3AddrDiff is the symmetric difference of two address sets, as sorted text,
// since the two engines are not obliged to enumerate in the same order.
func x3AddrDiff(old, new_ []Path) (onlyNew, onlyOld []string) {
	set := func(ps []Path) map[string]bool {
		m := map[string]bool{}
		for _, p := range ps {
			m[p.String()] = true
		}
		return m
	}
	o, n := set(old), set(new_)
	for s := range n {
		if !o[s] {
			onlyNew = append(onlyNew, s)
		}
	}
	for s := range o {
		if !n[s] {
			onlyOld = append(onlyOld, s)
		}
	}
	sort.Strings(onlyNew)
	sort.Strings(onlyOld)
	return onlyNew, onlyOld
}
