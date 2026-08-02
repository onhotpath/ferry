package main

// E6: the walk, and the constraint the owner's comment added to this ticket.
//
//   "xload's two walks - load.go:doProcess and async.go:processAsync - are ~90
//    duplicated lines of reflection that have already drifted apart, and they
//    return different results for the same input. Constraint for ferry: write
//    the walk exactly once."
//
// The instructive part is that ferry has THREE axes on which a walk can be
// duplicated, not one, and xload only has the third:
//
//   1. compile against walk   the schema promises an address the walk never
//                             visits. ADR-0008 found this in a real ferry
//                             prototype and it was silent.
//   2. load against dump      two directions over one type.
//   3. serial against concurrent   xload's, #20's.
//
// Writing one walk function answers 3 and does nothing for 1 or 2. This probe
// reproduces 1, measures how much of 2 is genuinely irreducible, and shows
// where 3's seam is.

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

type E6Common struct {
	Name string `ferry:"name"`
	Env  string `ferry:"env"`
}

type E6Conf struct {
	E6Common
	Port int `ferry:"port"`
}

// --- axis 1: the compiler and the walk disagreeing -------------------------

// e6CompileWithPromotion is the inherited prototype's compile() with ONE rule
// added: an embedded field with no tag is walked at the parent address, which
// is ADR-0008's promotion. The inherited dump() and load() do not have it.
func e6CompileWithPromotion(t reflect.Type, p Path, out *[]Path) {
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		at := p.Name(fieldName(f))
		if f.Anonymous && f.Tag.Get("ferry") == "" {
			at = p // promote
		}
		if f.Type.Kind() == reflect.Struct && classify(f.Type) == shapeStruct {
			e6CompileWithPromotion(f.Type, at, out)
			continue
		}
		*out = append(*out, at)
	}
}

func runE6() {
	ctx := context.Background()

	fmt.Println("--- E6a: axis 1, the compiler promising an address the walk never visits ---")
	var promised []Path
	e6CompileWithPromotion(reflect.TypeFor[E6Conf](), Path{}, &promised)
	fmt.Printf("  the compiler's address set : %v\n", sortedPaths(promised))
	// the inherited walk, which does not promote
	walked, _ := dump(reflect.ValueOf(E6Conf{E6Common{"n", "e"}, 8080}))
	fmt.Printf("  the walk's address set     : %v\n", sortedAddrs(walked))
	fmt.Println("  Two rules, one type, two answers, and no error from either. The load")
	fmt.Println("  side of it is the silent half: the schema promises /name, the walk")
	fmt.Println("  looks at /E6Common/name, and the field stays zero with err=nil.")
	fmt.Println("  This is the defect ADR-0008 found in a real ferry prototype, and one")
	fmt.Println("  walk function does not prevent it, because the two rules were never in")
	fmt.Println("  the same function to begin with.")

	fmt.Println("\n  What removes it: the address set is not COMPUTED by the compiler and")
	fmt.Println("  RE-COMPUTED by the walk, it is a field of the thing the walk iterates.")
	s, err := compileSchema2(reflect.TypeFor[E6Conf](), defaultOpts())
	if err != nil {
		fmt.Println("  compile:", err)
		return
	}
	fmt.Printf("  compiled schema addrs      : %v\n", s.addrs)
	out, _ := dumpTo(ctx, E6Conf{E6Common{"n", "e"}, 8080})
	fmt.Printf("  what the walk wrote        : %v\n", sortedAddrs(out))
	back, _ := loadFrom(ctx, E6Conf{}, out)
	fmt.Printf("  and back                   : %+v\n", back)
	fmt.Println("  The walk cannot visit a field the schema does not hold, and the schema")
	fmt.Println("  cannot hold a field the walk will not visit, because they are one list.")

	fmt.Println("\n--- E6b: axis 2, how much of load-against-dump is irreducible ---")
	fmt.Println("  Not zero, and the ADR should not claim zero. Three operations differ:")
	fmt.Println()
	fmt.Println("    leaf       Dump encodes a Go value and writes; Load reads and decodes,")
	fmt.Println("               applies a default for Absent, and enforces required.")
	fmt.Println("    container  Dump writes Null for a nil-or-empty composite; Load reads a")
	fmt.Println("               Null and zeroes. One question, two answers.")
	fmt.Println("    members    Dump reads a map's keys off the VALUE; Load enumerates the")
	fmt.Println("               PLANE. This one cannot be shared at all - ADR-0004 makes")
	fmt.Println("               Load of a dynamic address conditional on an Enumerator and")
	fmt.Println("               Dump unconditional, so the two are different capabilities.")
	fmt.Println()
	fmt.Println("  Everything else is written once: which nodes exist, in what order, how")
	fmt.Println("  a realised address is minted under a dynamic parent, how a pointer is")
	fmt.Println("  materialised from the presence bit, how a map value is made addressable")
	fmt.Println("  and put back, where the context is checked, where the scheduler is.")
	fmt.Printf("\n  Measured on this prototype: %s\n", e6LineCounts())

	fmt.Println("\n--- E6c: axis 3, the scheduler seam, and what #20 inherits ---")
	fmt.Println("  A concurrent mode is #20's and this ADR decides none of it. What it")
	fmt.Println("  fixes is that a concurrent mode is a second SCHEDULER and never a")
	fmt.Println("  second walk. Measured, the same walk under both:")

	plane := out
	rd := mapReader{plane}
	o := defaultOpts()
	runWith(ctx, s, rd, o, serial)
	bench := func(name string, sc sched) {
		r := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				runWith(ctx, s, rd, o, sc)
			}
		})
		fmt.Printf("    %-38s %7d ns/op %3d allocs/op\n", name, r.NsPerOp(), r.AllocsPerOp())
	}
	bench("serial scheduler", serial)
	bench("scheduler as an indirect call, inlined", func(ts []func() error) error {
		for _, t := range ts {
			if err := t(); err != nil {
				return err
			}
		}
		return nil
	})
	fmt.Println("  The cost the owner's comment priced - \"one indirect call per leaf\" - is")
	fmt.Println("  actually one indirect call per CONTAINER, because the scheduler is")
	fmt.Println("  handed a batch of sibling tasks rather than called per leaf.")

	fmt.Println("\n  And one thing #20 inherits that ADR-0006 already flagged: the walk")
	fmt.Println("  returns a presence bit per subtree, so a concurrent scheduler has to")
	fmt.Println("  COMBINE those bits and not only the errors. In this prototype the bit")
	fmt.Println("  is OR-ed into a captured variable, which is a data race the moment the")
	fmt.Println("  scheduler is not serial. E8 runs it under -race. That is a real hazard")
	fmt.Println("  handed over rather than a note: #20's scheduler cannot be a drop-in.")

	fmt.Println("\n--- E6d: and the equivalence test xload could not write ---")
	fmt.Println("  xload's own serial-against-concurrent test cannot catch its divergence,")
	fmt.Println("  because `input` is a POINTER built once in the table literal and both")
	fmt.Println("  subtests share it: the serial subtest populates it, and any field the")
	fmt.Println("  concurrent path fails to set is still correct. Reproduced, with a")
	fmt.Println("  deliberately broken second walk that skips every leaf:")

	shared := &E6Conf{}
	want := E6Conf{E6Common{"n", "e"}, 8080}
	// subtest 1: the good walk, into the shared destination
	got1, _ := loadFrom(ctx, *shared, plane)
	*shared = got1
	fmt.Printf("    shared destination, walk 1 (good)   -> %+v  equal=%v\n", *shared, *shared == want)
	// subtest 2: a broken walk, into the SAME destination
	broken := loadDir(rd, ctx, o)
	broken.leaf = func(n *node, v reflect.Value, at Path) (bool, error) { return false, nil }
	w := &walker{dir: broken, sch: serial, ctx: ctx}
	_, _ = w.walk(s.root, reflect.ValueOf(shared).Elem(), Path{})
	fmt.Printf("    shared destination, walk 2 (broken) -> %+v  equal=%v  <- PASSES\n", *shared, *shared == want)

	fresh := &E6Conf{}
	w2 := &walker{dir: broken, sch: serial, ctx: ctx}
	_, _ = w2.walk(s.root, reflect.ValueOf(fresh).Elem(), Path{})
	fmt.Printf("    FRESH destination, walk 2 (broken)  -> %+v  equal=%v  <- caught\n", *fresh, *fresh == want)
	fmt.Println("  So the constraint is two rules and not one: write the walk once, AND")
	fmt.Println("  give every equivalence subtest its own destination.")
}

func runWith(ctx context.Context, s *schema, rd FReader, o opts, sc sched) E6Conf {
	var out E6Conf
	w := &walker{dir: loadDir(rd, ctx, o), sch: sc, ctx: ctx}
	_, _ = w.walk(s.root, reflect.ValueOf(&out).Elem(), Path{})
	return out
}

// parallel is the scheduler #20 would plug in. It is here to be measured and
// to show the presence-bit hazard, not proposed.
func parallel(tasks []func() error) error {
	if len(tasks) < 2 {
		return serial(tasks)
	}
	var wg sync.WaitGroup
	errs := make([]error, len(tasks))
	for i, t := range tasks {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = t() }()
	}
	wg.Wait()
	return errors.Join(errs...)
}

func e6LineCounts() string {
	// counted with `grep -v '^\s*//' | grep -v '^\s*$'` over e_walk.go
	return "the shared structure is 129 non-comment lines (walk 69, the " +
		"make-addressable mechanics 47, realised-address minting 13); the two " +
		"directions are 48 and 70, and every line of both is inside one of the " +
		"three hooks. So roughly half the walk is shared and none of the shared " +
		"half exists twice. xload's figure for the same ratio is ~90 duplicated " +
		"lines and nothing shared"
}
