package main

// P14: the audit.
//
// The trap the prior sessions named: a fixture where every type implements
// exactly one arm never exercises precedence. P1 and P2 cover that. So what
// ELSE do the fixtures not cover?
//
// Every probe so far put the chain type at a LEAF, in a one-field struct, at
// a non-zero value, against an in-memory map. Four things that is blind to.

import (
	"fmt"
	"math/big"
	"net/netip"
	"reflect"
	"regexp"
	"time"

	"github.com/google/uuid"
	"golang.org/x/text/language"
)

type auditConf struct {
	One   netip.Addr            `ferry:"one"`
	Ptr   *netip.Addr           `ferry:"ptr"`
	Slice []netip.Addr          `ferry:"slice"`
	Arr   [2]netip.Addr         `ferry:"arr"`
	Map   map[string]netip.Addr `ferry:"map"`
	Deep  struct {
		Inner big.Int `ferry:"inner"`
	} `ferry:"deep"`
}

func runAudit12() {
	chainOrder, chainBeforeKind = []string{"text"}, true
	defer func() { chainOrder, chainBeforeKind = nil, false }()

	fmt.Println("\n--- P14a: the ZERO value of every type the chain admits ---")
	fmt.Println("    An unset field is the value ferry writes most often, and no probe")
	fmt.Println("    above used one. A codec that is partial over its own type fails")
	fmt.Println("    here and nowhere else.")
	fmt.Printf("    %-18s %-34s %s\n", "type", "zero encodes to", "and loads back to")
	fmt.Println("    " + dashes(90))
	for _, r := range []struct {
		name string
		t    reflect.Type
	}{
		{"netip.Addr", reflect.TypeFor[netip.Addr]()},
		{"netip.AddrPort", reflect.TypeFor[netip.AddrPort]()},
		{"netip.Prefix", reflect.TypeFor[netip.Prefix]()},
		{"big.Int", reflect.TypeFor[big.Int]()},
		{"regexp.Regexp", reflect.TypeFor[regexp.Regexp]()},
		{"language.Tag", reflect.TypeFor[language.Tag]()},
		{"uuid.UUID", reflect.TypeFor[uuid.UUID]()},
		{"time.Time", reflect.TypeFor[time.Time]()},
	} {
		z := reflect.New(r.t).Elem()
		val, err := encLeaf(z)
		if err != nil {
			fmt.Printf("    %-18s ENCODE ERR %v\n", r.name, err)
			continue
		}
		back := reflect.New(r.t).Elem()
		derr := decLeaf(val, back)
		verdict := "same"
		if derr != nil {
			verdict = "DECODE ERR: " + shorten2(derr.Error(), 46)
		} else if !reflect.DeepEqual(z.Interface(), back.Interface()) {
			verdict = fmt.Sprintf("DIFFERS: %v", back.Interface())
		}
		fmt.Printf("    %-18s %-34s %s\n", r.name, shorten2(val.GoString(), 34), verdict)
	}

	fmt.Println("\n--- P14b: the chain type in every composite position ---")
	a := netip.MustParseAddr("10.0.0.1")
	c := auditConf{
		One:   a,
		Ptr:   &a,
		Slice: []netip.Addr{a, netip.MustParseAddr("10.0.0.2")},
		Arr:   [2]netip.Addr{a, {}},
		Map:   map[string]netip.Addr{"x": a},
	}
	c.Deep.Inner = *big.NewInt(7)
	addrs, err := compile(reflect.TypeOf(c))
	fmt.Printf("    compile: %v err=%v\n", addrs, err)
	d, derr := dump(reflect.ValueOf(c))
	fmt.Printf("    dump err=%v\n", derr)
	for _, p := range sortedAddrs(d) {
		fmt.Printf("      %-16s %s\n", p, d[p].GoString())
	}
	var back auditConf
	lerr := load(d, reflect.ValueOf(&back).Elem())
	fmt.Printf("    load err=%v  equal=%v\n", lerr, reflect.DeepEqual(c, back))

	fmt.Println("\n--- P14c: through the FLATTENING plane, which reports String only ---")
	fmt.Println("    ADR-0005's G2 was hidden for two drafts because every proof fed")
	fmt.Println("    dump's own output back into load. Repeat that mistake here and the")
	fmt.Println("    text arm looks fine on env when it is not.")
	flat := map[Path]Value{}
	for p, v := range d {
		if v.Kind() == VNull {
			flat[p] = String("") // a flat plane has no null
			continue
		}
		flat[p] = String(v.Text())
	}
	var fback auditConf
	ferr := load(flat, reflect.ValueOf(&fback).Elem())
	fmt.Printf("    load from all-String plane: err=%v equal=%v\n", ferr, reflect.DeepEqual(c, fback))
	fmt.Printf("      one=%v slice=%v map=%v ptr=%v\n", fback.One, fback.Slice, fback.Map, fback.Ptr)

	fmt.Println("\n--- P14d: through the real YAML driver ---")
	fmt.Println(p4yaml(c))

	fmt.Println("--- P14e: the chain type at the ROOT of the schema ---")
	ra, rerr := compile(reflect.TypeFor[netip.Addr]())
	fmt.Printf("    compile netip.Addr as the root: %v err=%v\n", ra, rerr)
	rd, _ := dump(reflect.ValueOf(netip.MustParseAddr("10.0.0.1")))
	fmt.Printf("    dump: %s\n", fmtVals(rd))
	fmt.Println("    ^ a root leaf mints the empty path, which ADR-0003 says an address")
	fmt.Println("      may not be. It is pre-existing - a root `int` does the same under")
	fmt.Println("      ADR-0005 - so it is #16's entry point rather than this ADR's, but")
	fmt.Println("      the chain enlarges the set of types that can sit there.")

	fmt.Println("\n--- P14f: ADR-0005's completeness check cannot see a chain-admitted type ---")
	fmt.Println("    The check iterates the identity table and the admitted kind list and")
	fmt.Println("    asserts every member has a proof. Both are enumerable. The set of")
	fmt.Println("    types implementing encoding.TextMarshaler is not: reflect has no")
	fmt.Println("    'all types implementing this interface' query, and it cannot, because")
	fmt.Println("    the set depends on every package the consumer imports.")
	fmt.Println("    So the arm admits an unbounded set with no proof and no completeness")
	fmt.Println("    check, and the regexp.Regexp row above shows what that means: ferry")
	fmt.Println("    cannot even state what round-trip MEANS for it, because ADR-0005's")
	fmt.Println("    per-type relation is declared in a proof nobody wrote.")

	fmt.Println("\n--- P14g: does the chain break core's own 21 proofs? ---")
	for _, set := range []struct {
		name string
		ps   []Proof
	}{{"core scalars", coreSet()}, {"composites", auditSet()}} {
		total, bad := 0, 0
		for _, pr := range set.ps {
			total++
			if fails := failsOn(pr, memoryPlane()); len(fails) > 0 {
				bad++
				fmt.Printf("    FAIL %-14s %v\n", pr.Name(), fails)
			}
		}
		fmt.Printf("    %-14s with the chain ON, memory plane: %d/%d pass\n", set.name, total-bad, total)
	}
	fmt.Println("    ^ core's own set is unaffected, because identity is consulted before")
	fmt.Println("      the chain and none of the kind-admitted core types carries a text")
	fmt.Println("      pair. That is the check that says the chain is additive for the")
	fmt.Println("      guaranteed set, whatever it does to types outside it.")
}
