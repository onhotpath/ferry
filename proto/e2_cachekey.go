package main

// E2: the cache key.
//
// This is the part of #16 with the most already decided and the least written
// down. Two ADRs put something in this key independently, from two sessions
// that did not coordinate:
//
//   ADR-0008  the struct tag key, a caller-supplied Option, measured at
//             18 ns/op for map[reflect.Type] against 28 ns/op for
//             map[struct{reflect.Type; string}], no allocations either way
//   ADR-0009  the codec registry, measured at 9 ns/op for a sync.Map keyed by
//             reflect.Type against 32 ns/op for a {reflect.Type, *Registry}
//             pair, with 10 ns/op available by hanging the per-type cache off
//             the registry
//
// So the surveyed idiom - "a sync.Map keyed by reflect.Type" - is measurably
// wrong for ferry, twice, and #16 owns only the assembly.

import (
	"context"
	"fmt"
	"math/big"
	"reflect"
	"sync"
	"testing"
)

type E2Conf struct {
	Host string `ferry:"host" mylib:"HOST"`
	Port int    `ferry:"port" mylib:"PORT"`
}

type E2Big struct {
	N big.Int `ferry:"n"`
}

func runE2() {
	ctx := context.Background()
	_ = ctx

	fmt.Println("--- E2a: what a sync.Map keyed by reflect.Type alone gets wrong, both ways ---")

	// (i) the tag key. ADR-0008's measurement, re-run against a real compile.
	oF := defaultOpts()
	oM := defaultOpts()
	oM.tagKey = "mylib"
	sF, _ := compileSchema2(reflect.TypeFor[E2Conf](), oF)
	sM, _ := compileSchema2(reflect.TypeFor[E2Conf](), oM)
	fmt.Printf("  one reflect.Type, tag key \"ferry\" -> %v\n", sF.leafAddrs)
	fmt.Printf("  one reflect.Type, tag key \"mylib\" -> %v\n", sM.leafAddrs)

	// (ii) the registry. ADR-0009's measurement, re-run against a real compile.
	rA := NewRegistry()
	mustReg(rA, TextCodec[big.Int](VString))
	rB := NewRegistry()
	mustReg(rB, ValueCodec(VNumber,
		func(x big.Int) (Value, error) { return Number(x.String()), nil },
		func(v Value) (big.Int, error) {
			var x big.Int
			s, _ := v.AsNumber()
			x.SetString(s, 10)
			return x, nil
		}))
	one := big.NewInt(1099511627776)
	dA, _ := dumpTo(ctx, E2Big{*one}, WithRegistry(rA))
	dB, _ := dumpTo(ctx, E2Big{*one}, WithRegistry(rB))
	fmt.Printf("  registry A wants big.Int as text   -> %s\n", dA[Path{}.Name("n")].GoString())
	fmt.Printf("  registry B wants big.Int as number -> %s\n", dB[Path{}.Name("n")].GoString())

	// now the same thing through a cache keyed by reflect.Type alone
	var naive sync.Map
	naiveGet := func(o opts) *schema {
		t := reflect.TypeFor[E2Big]()
		if v, ok := naive.Load(t); ok {
			return v.(*schema)
		}
		done := o.reg.install()
		defer done()
		s, _ := compileSchema2(t, o)
		naive.Store(t, s)
		return s
	}
	oA, oB := defaultOpts(), defaultOpts()
	oA.reg, oB.reg = rA, rB
	nA, nB := naiveGet(oA), naiveGet(oB)
	encOf := func(s *schema) string {
		v, _ := encLeafWith(s.root.fields[0].node.codec, reflect.ValueOf(*one))
		return v.GoString()
	}
	fmt.Printf("\n  sync.Map keyed by reflect.Type alone:\n")
	fmt.Printf("    service A compiles first -> %s\n", encOf(nA))
	fmt.Printf("    service B hits the cache -> %s   <- B silently got A's codec, no error\n", encOf(nB))
	fmt.Println("  That is ADR-0004's EnvSource{Sep} defect two layers up, and it is the")
	fmt.Println("  shape every one of the eight stdlib type caches uses.")

	fmt.Println("\n--- E2b: what each candidate key costs, on ferry's actual three components ---")
	type pair3 struct {
		t   reflect.Type
		tag string
		reg *Registry
	}
	type pair2 struct {
		t   reflect.Type
		tag string
	}
	t := reflect.TypeFor[E2Conf]()
	reg := NewRegistry()
	sc := &schema{}

	var mType sync.Map
	mType.Store(t, sc)
	var mPair3 sync.Map
	mPair3.Store(pair3{t, "ferry", reg}, sc)
	var mPair2 sync.Map
	mPair2.Store(pair2{t, "ferry"}, sc)
	reg.schemas.Store(pair2{t, "ferry"}, sc)
	plainType := map[reflect.Type]*schema{t: sc}
	plainPair2 := map[pair2]*schema{{t, "ferry"}: sc}
	plainPair3 := map[pair3]*schema{{t, "ferry", reg}: sc}
	type pairR struct {
		t   reflect.Type
		reg *Registry
	}
	var mPairR sync.Map
	mPairR.Store(pairR{t, reg}, sc)
	// the nesting ADR-0009 offered, which is a sync.Map keyed by reflect.Type
	// hung off the registry - available only if nothing else is in the key.
	var regByType sync.Map
	regByType.Store(t, sc)
	// the nesting that IS available once ADR-0008's tag key is in play: two
	// levels, the inner one keeping the stdlib's shape.
	var byTag sync.Map
	inner := &sync.Map{}
	inner.Store(t, sc)
	byTag.Store("ferry", inner)

	bench := func(name string, f func()) {
		r := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				for range 16 {
					f()
				}
			}
		})
		fmt.Printf("  %-54s %6.2f ns/op %3d allocs/op\n",
			name, float64(r.T.Nanoseconds())/float64(r.N*16), r.AllocsPerOp())
	}
	var sink any
	bench("sync.Map[reflect.Type]                        (wrong)", func() { sink, _ = mType.Load(t) })
	bench("sync.Map[{Type,*Registry}]                    (ADR-9)", func() { sink, _ = mPairR.Load(pairR{t, reg}) })
	bench("registry.schemas: sync.Map[reflect.Type]      (ADR-9)", func() { sink, _ = regByType.Load(t) })
	bench("sync.Map[{Type,string,*Registry}]             (flat)", func() { sink, _ = mPair3.Load(pair3{t, "ferry", reg}) })
	bench("registry.schemas: sync.Map[{Type,string}]     (nested)", func() { sink, _ = reg.schemas.Load(pair2{t, "ferry"}) })
	bench("registry -> sync.Map[tag] -> sync.Map[Type]   (nested2)", func() {
		m, _ := byTag.Load("ferry")
		sink, _ = m.(*sync.Map).Load(t)
	})
	bench("sync.Map[{Type,string}]                       (no reg)", func() { sink, _ = mPair2.Load(pair2{t, "ferry"}) })
	bench("map[reflect.Type]                             (plain)", func() { sink = plainType[t] })
	bench("map[{Type,string}]                            (plain)", func() { sink = plainPair2[pair2{t, "ferry"}] })
	bench("map[{Type,string,*Registry}]                  (plain)", func() { sink = plainPair3[pair3{t, "ferry", reg}] })
	_ = sink

	fmt.Println("\n  The two (ADR-9) rows are ADR-0009's own two measurements, and neither is")
	fmt.Println("  available to #16 as measured, because ADR-0008's tag key is in the key too")
	fmt.Println("  and the two sessions ran in parallel. In particular the 10 ns nesting that")
	fmt.Println("  ADR-0009 offered - the per-type cache hung off the registry, inner lookup")
	fmt.Println("  keeping the stdlib's shape - needs the inner key to be a bare reflect.Type,")
	fmt.Println("  and it no longer can be. Nesting a second level recovers most of it.")
	fmt.Println("  Read against the compile it is saving: E3 measures that. Every row here is")
	fmt.Println("  inside the noise of one plane Get, so the key's SHAPE is a correctness")
	fmt.Println("  question and its cost is the argument for none of them.")

	fmt.Println("\n--- E2c: the difference between the two failures, and it is a build/run one ---")
	fmt.Println("  ADR-0006 measured the bad end of a compile-affecting Option:")
	fmt.Println("    runtime error: hash of unhashable type main.LoadOption")
	fmt.Println("  Reproduced here through a sync.Map, which takes `any` keys:")
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("    sync.Map.Load(unhashableKey{}) -> PANIC: %v\n", r)
			}
		}()
		type unhashableKey struct {
			t reflect.Type
			f func()
		}
		var m sync.Map
		m.Load(unhashableKey{t, nil})
	}()
	fmt.Println("\n  The same key in a PLAIN map is a build error, verified on go1.27rc2:")
	fmt.Println("    invalid map key type e2Unhashable")
	fmt.Println("  So the one-line static assertion in e_opts.go")
	fmt.Println("    var _ = map[schemaKey]struct{}{}")
	fmt.Println("  turns ADR-0006's runtime panic into a compile error the moment somebody")
	fmt.Println("  adds a compile-affecting Option whose value is not comparable. That is")
	fmt.Println("  free, and it is the only mechanism available: nothing else in Go checks it.")

	fmt.Println("\n--- E2d: the cache is unbounded, and reflect.StructOf is the one leak ---")
	before := regEntries(defaultRegistry)
	for i := range 200 {
		st := reflect.StructOf([]reflect.StructField{{
			Name: "F", Type: reflect.TypeFor[string](),
			Tag: reflect.StructTag(fmt.Sprintf(`ferry:"f%d"`, i)),
		}})
		o := defaultOpts()
		_, _ = schemaFor(st, o)
	}
	fmt.Printf("  200 reflect.StructOf types -> cache grew from %d to %d entries, none evictable\n",
		before, regEntries(defaultRegistry))
	fmt.Println("  Every library surveyed documents this rather than solving it, and the")
	fmt.Println("  research found no eviction in any of the eight stdlib caches. What is")
	fmt.Println("  ferry-specific is that the cache hangs off a REGISTRY, so a per-call")
	fmt.Println("  registry would leak the same way for ordinary static types - which is")
	fmt.Println("  ADR-0009's \"a registry must be long-lived\" restated as this cache's rule.")
}

func regEntries(r *Registry) int {
	n := 0
	r.schemas.Range(func(any, any) bool { n++; return true })
	return n
}
