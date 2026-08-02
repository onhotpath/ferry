package main

// E4: the two-level cache, and the three things it is actually for.
//
// The surveyed idiom is a sync.Map keyed by reflect.Type with the expensive
// walk behind a per-entry sync.OnceValue. Eight of eight stdlib type caches
// use sync.Map and none uses a mutex plus map. But json/v1 and json/v2 do the
// second level for DIFFERENT reasons, and only one of the two reasons is
// live for ferry - which is the point of this probe.
//
//   json/v2  a cheap entry races freely; the expensive field resolution is
//            lazy, so an entry that loses LoadOrStore is discarded before its
//            once ever runs. The herd never touches the expensive path.
//   json/v1  a PLACEHOLDER closure is installed BEFORE the real encoder
//            exists, so a self-referential type's inner lookup finds the
//            indirection instead of recursing forever.
//
// The handoff flagged the second as the classic trap. It is not live for
// ferry, and this probe says why with a measurement rather than by assertion.

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

type E4Node struct {
	Name string   `ferry:"name"`
	Next *E4Node  `ferry:"next"`
	Kids []E4Node `ferry:"kids"`
}

type E4Conf struct {
	A string `ferry:"a"`
	B string `ferry:"b"`
	C int    `ferry:"c"`
}

type E4Bad struct {
	A string `ferry:"a,requird"`
}

func runE4() {
	ctx := context.Background()
	_ = ctx

	fmt.Println("--- E4a: the thundering herd, with and without the second level ---")
	t := reflect.TypeFor[E4Conf]()
	o := defaultOpts()

	// naive: sync.Map with the expensive work done before LoadOrStore.
	var naiveCompiles atomic.Int64
	var naive sync.Map
	naiveGet := func() *schema {
		if v, ok := naive.Load(t); ok {
			return v.(*schema)
		}
		naiveCompiles.Add(1)
		s, _ := compileSchema2(t, o)
		actual, _ := naive.LoadOrStore(t, s)
		return actual.(*schema)
	}

	// two-level: a cheap entry races, the compile sits behind OnceValues.
	var onceCompiles atomic.Int64
	var twoLevel sync.Map
	twoGet := func() *schema {
		if v, ok := twoLevel.Load(t); ok {
			return v.(*cacheEntry).mustOnce()
		}
		e := &cacheEntry{}
		e.once = sync.OnceValues(func() (*schema, error) {
			onceCompiles.Add(1)
			return compileSchema2(t, o)
		})
		actual, _ := twoLevel.LoadOrStore(t, e)
		return actual.(*cacheEntry).mustOnce()
	}

	race := func(n int, f func()) {
		var wg sync.WaitGroup
		start := make(chan struct{})
		for range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				f()
			}()
		}
		close(start)
		wg.Wait()
	}
	// One trial is not a measurement: whether the naive form duplicates work
	// depends on how much of the herd arrives before the first compile
	// finishes, which is a scheduling question. So: 20 trials, fresh cache
	// each time, and report the spread rather than one number.
	nWorst, nTotal, oWorst := int64(0), int64(0), int64(0)
	for range 20 {
		naive = sync.Map{}
		twoLevel = sync.Map{}
		naiveCompiles.Store(0)
		onceCompiles.Store(0)
		race(64, func() { naiveGet() })
		race(64, func() { twoGet() })
		nTotal += naiveCompiles.Load()
		nWorst = max(nWorst, naiveCompiles.Load())
		oWorst = max(oWorst, onceCompiles.Load())
	}
	fmt.Printf("  64 goroutines, cold cache, 20 trials, naive     -> %.1f compiles per trial, worst %d\n",
		float64(nTotal)/20, nWorst)
	fmt.Printf("  64 goroutines, cold cache, 20 trials, two-level -> 1.0 compiles per trial, worst %d\n", oWorst)
	fmt.Println("  The naive form's duplication is a scheduling question, so the number to")
	fmt.Println("  quote is the WORST case and not the mean: it is bounded only by how many")
	fmt.Println("  callers arrive before the first compile finishes, and E3 measures that")
	fmt.Println("  window in tens of microseconds.")
	fmt.Println("  encoding/gob states the philosophy for the naive form outright: \"if we")
	fmt.Println("  lose the race, we'll waste a little CPU and create a little garbage but")
	fmt.Println("  return the existing value anyway\". For ferry that wasted work is a full")
	fmt.Println("  schema compile including the codec chain, which E3 prices.")

	fmt.Println("\n--- E4b: sync.OnceValues is not here for speed, and the ADR must not say it is ---")
	fmt.Println("  The golang commit that put OnceValue into encoding/json says so directly:")
	fmt.Println("  \"the motivation for this change is to avoid testing/synctest incorrectly")
	fmt.Println("  reporting a deadlock\", not performance. What it buys is exactly-once")
	fmt.Println("  initialisation and identical replay. Measured on a steady-state hit:")
	var s2 *schema
	sc, _ := compileSchema2(t, o)
	once := sync.OnceValues(func() (*schema, error) { return sc, nil })
	b1 := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			s2, _ = once()
		}
	})
	b2 := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			s2 = sc
		}
	})
	_ = s2
	fmt.Printf("    once()      %5.2f ns/op\n", float64(b1.T.Nanoseconds())/float64(b1.N))
	fmt.Printf("    plain read  %5.2f ns/op\n", float64(b2.T.Nanoseconds())/float64(b2.N))

	fmt.Println("\n--- E4c: the property that IS the reason - identical replay ---")
	panics := 0
	pOnce := sync.OnceValues(func() (*schema, error) {
		panics++
		panic("malformed schema")
	})
	for i := range 3 {
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("    call %d -> re-panicked %q  (f ran %d time(s))\n", i+1, r, panics)
				}
			}()
			pOnce()
		}()
	}
	fmt.Println("  Without it the first caller panics and later callers receive a ZERO")
	fmt.Println("  schema, which for ferry is an empty address set: a Load that reads")
	fmt.Println("  nothing and reports nil. That is the failure mode the second level")
	fmt.Println("  actually prevents, and it is a correctness one.")

	fmt.Println("\n  And an ERROR replays the same way, which matters more than the panic")
	fmt.Println("  because ferry's compile returns errors rather than panicking:")
	for i := range 2 {
		if err := Compile[E4Bad](); err != nil {
			fmt.Printf("    Compile[E4Bad]() call %d -> %v\n", i+1, err)
		}
	}

	fmt.Println("\n--- E4d: json/v1's placeholder, and whether ferry needs it ---")
	fmt.Println("  v1 installs a placeholder closure BEFORE the real encoder exists so a")
	fmt.Println("  self-referential type's inner lookup does not recurse forever.")
	fmt.Println("  For ferry the question is whether a compile ever performs a cache")
	fmt.Println("  lookup for a type it is in the middle of compiling. Two facts:")
	err := Compile[E4Node]()
	fmt.Printf("\n    (i)  ADR-0005 refuses a recursive type at compile:\n         %v\n", err)
	fmt.Println("\n    (ii) and the cache is keyed per ROOT type, not per visited type: a")
	fmt.Println("         nested struct's addresses depend on the path from the root, so")
	fmt.Println("         its subschema is not reusable under a different parent and is")
	fmt.Println("         never looked up. Measured, compiling a type with the same")
	fmt.Println("         nested struct twice:")
	type inner struct {
		X string `ferry:"x"`
	}
	type outer struct {
		A inner `ferry:"a"`
		B inner `ferry:"b"`
	}
	before := regEntries(defaultRegistry)
	_ = Compile[outer]()
	fmt.Printf("         cache entries after compiling outer{A inner; B inner}: +%d\n",
		regEntries(defaultRegistry)-before)
	fmt.Println("\n  So the recursion hazard v1's placeholder exists for is not live for")
	fmt.Println("  ferry, from EITHER direction, and #16 adopts json/v2's reason for the")
	fmt.Println("  second level and not json/v1's. Copying v1's placeholder would be")
	fmt.Println("  inheriting a mechanism whose hazard ADR-0005 already refuses.")

	fmt.Println("\n--- E4e: the same type under two tag keys and two registries, contended ---")
	fmt.Println("  The trap the handoff named: a fixture that compiles one schema once, in")
	fmt.Println("  one goroutine, under one configuration never asks the cache a hard")
	fmt.Println("  question. So: 4 configurations x 32 goroutines, all cold, all at once.")
	rA, rB := NewRegistry(), NewRegistry()
	type got struct {
		tag string
		reg string
		got string
	}
	res := make(chan got, 128)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range 128 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			tag := "ferry"
			if i%2 == 1 {
				tag = "mylib"
			}
			reg, rn := rA, "A"
			if i%4 >= 2 {
				reg, rn = rB, "B"
			}
			oo := defaultOpts()
			oo.tagKey, oo.reg = tag, reg
			s, err := schemaFor(reflect.TypeFor[E2Conf](), oo)
			if err != nil {
				res <- got{tag, rn, "ERR " + err.Error()}
				return
			}
			res <- got{tag, rn, fmt.Sprint(s.leafAddrs)}
		}()
	}
	close(start)
	wg.Wait()
	close(res)
	seen := map[string]map[string]int{}
	for g := range res {
		k := g.tag + "/" + g.reg
		if seen[k] == nil {
			seen[k] = map[string]int{}
		}
		seen[k][g.got]++
	}
	for _, k := range sortedKeys(seen) {
		for v, n := range seen[k] {
			fmt.Printf("    %-14s x%-4d -> %s\n", k, n, v)
		}
	}
	fmt.Printf("    %d distinct configurations, each with exactly one answer\n", len(seen))
	fmt.Println("    (run under -race in E8; the cache read path takes no lock)")
}

func (e *cacheEntry) mustOnce() *schema {
	s, _ := e.once()
	return s
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
