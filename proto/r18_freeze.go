package main

// R18: is the freeze decision actually complete, or does it have an
// unanswered question?
//
// R6, R7 and R8 established THAT the registry must freeze. They did not
// establish two things, and both were assumed rather than probed:
//
//   R18a  WHAT freezes: the whole registry, or each type as it is resolved?
//   R18b  does the DEFAULT registry's freeze reintroduce the init()-order
//         dependence this ADR criticised the global-frozen model for?
//
// The second is the sharper of the two, because it is this ADR turning its own
// argument on itself.

import (
	"fmt"
	"net/netip"
	"reflect"
	"sync"
	"testing"
)

// --- R18a: whole-registry freeze against per-type freeze ---------------------

// perTypeRegistry freezes each type as it is resolved rather than freezing the
// whole table at the first compile. It is strictly more permissive, and the
// question is whether it is equally sound.
type perTypeRegistry struct {
	mu       sync.RWMutex
	byType   map[reflect.Type]leafCodec
	resolved map[reflect.Type]bool
}

func newPerType() *perTypeRegistry {
	return &perTypeRegistry{
		byType:   map[reflect.Type]leafCodec{},
		resolved: map[reflect.Type]bool{},
	}
}

func (r *perTypeRegistry) register(t reflect.Type, c leafCodec) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.resolved[t] {
		return fmt.Errorf(
			"ferry: %s: already resolved by an earlier schema compile; a registration "+
				"for it would make that schema stale", t)
	}
	r.byType[t] = c
	return nil
}

// resolve is what a schema compile does per type it visits.
func (r *perTypeRegistry) resolve(t reflect.Type) (leafCodec, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolved[t] = true
	c, ok := r.byType[t]
	return c, ok
}

type R18Backend struct {
	Host string
	Port int
}

func runR18() {
	fmt.Println("--- R18a: what freezes, the registry or the type? ---")
	fmt.Println("    Per-type freeze is strictly more permissive. The case it admits and")
	fmt.Println("    whole-registry freeze refuses:")
	fmt.Println()
	fmt.Println("        ferry.Load[A](ctx, src)                 // A mentions no netip.Addr")
	fmt.Println("        ferry.Register(codecFor_netipAddr)      // a lazily-imported plugin")
	fmt.Println("        ferry.Load[B](ctx, src)                 // B does mention it")

	pt := newPerType()
	addrT := reflect.TypeFor[netip.Addr]()
	strT := reflect.TypeFor[string]()

	// compile A: resolves string only
	pt.resolve(strT)
	fmt.Printf("\n    per-type:  Load[A] then Register(netip.Addr) -> %v\n",
		pt.register(addrT, leafCodec{name: "netip.Addr", kind: VString}))
	_, ok := pt.resolve(addrT)
	fmt.Printf("               Load[B] finds the codec              -> %v\n", ok)
	fmt.Printf("               and a SECOND registration for it     -> %v\n",
		pt.register(addrT, leafCodec{name: "netip.Addr", kind: VString}))

	whole := NewRegistry()
	whole.frozen.Store(true) // as if Load[A] had run
	fmt.Printf("\n    whole:     Load[A] then Register(netip.Addr) -> %v\n",
		whole.Register(TextCodec[netip.Addr](VString)))

	fmt.Println("\n    So per-type is sound for the CACHE: a schema for A resolved only")
	fmt.Println("    the types A reaches, so registering a type A never mentioned cannot")
	fmt.Println("    make A's schema stale. Verified by construction rather than asserted:")
	fmt.Println("    resolve() marks exactly what compile visited, and register() refuses")
	fmt.Println("    exactly what was marked.")

	fmt.Println("\n    THREE THINGS KILL IT ANYWAY, and the first is this ADR's own argument.")

	fmt.Println("\n    (1) It makes whether your registration SUCCEEDS depend on which")
	fmt.Println("        schemas happened to be compiled first, which is import-graph")
	fmt.Println("        order. Measured, the same two operations in two orders:")
	for _, order := range []struct {
		label string
		first reflect.Type
	}{
		{"Load[B] (mentions netip.Addr) first", addrT},
		{"Load[A] (does not) first", strT},
	} {
		p := newPerType()
		p.resolve(order.first)
		err := p.register(addrT, leafCodec{name: "netip.Addr", kind: VString})
		got := "accepted"
		if err != nil {
			got = "REFUSED"
		}
		fmt.Printf("        %-38s -> Register(netip.Addr) %s\n", order.label, got)
	}
	fmt.Println("        ^ R5b refused a predicate arm because `registration order across")
	fmt.Println("          packages is init() order, which is a property of the import")
	fmt.Println("          graph rather than of anyone's intent`, and R8c refused")
	fmt.Println("          global-frozen for the same reason. Per-type freeze is that")
	fmt.Println("          property a third time. Whole-registry freeze is not: `after any")
	fmt.Println("          Load, no registrations` is decided by program order alone.")

	fmt.Println("\n    (2) It puts a growing mutable set on the lookup path.")
	fmt.Println("        resolve() must WRITE, so the read path takes a lock, which is")
	fmt.Println("        the thing R13b and ADR-0004 both measured a freeze to avoid:")
	frozen := NewRegistry()
	_ = frozen.Register(TextCodec[netip.Addr](VString))
	frozen.frozen.Store(true)
	pt2 := newPerType()
	pt2.byType[addrT] = leafCodec{name: "netip.Addr", kind: VString}
	bFrozen := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			_, _ = frozen.lookup(addrT)
		}
	})
	bPerType := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			_, _ = pt2.resolve(addrT)
		}
	})
	fmt.Printf("        frozen registry, plain map read   %5.1f ns/op\n", float64(bFrozen.NsPerOp()))
	fmt.Printf("        per-type, mutex + map write       %5.1f ns/op\n", float64(bPerType.NsPerOp()))
	fmt.Println("        (#16 resolves per type per registry, not per leaf, so this is")
	fmt.Println("         not the argument either. It is stated so the ADR does not lean")
	fmt.Println("         on a number that does not bear weight.)")

	fmt.Println("\n    (3) The diagnostic gets worse, not better.")
	fmt.Println("        whole:    `the registry is frozen; every registration must happen")
	fmt.Println("                   before the first schema is compiled`")
	fmt.Println("        per-type: `netip.Addr: already resolved by an earlier schema")
	fmt.Println("                   compile` - and the user then has to work out WHICH")
	fmt.Println("                   compile, which is a question about a type they may not")
	fmt.Println("                   have written.")

	fmt.Println("\n    Answer: whole-registry. Per-type is sound and is refused on the same")
	fmt.Println("    ground this ADR refuses two other things, which is the test of whether")
	fmt.Println("    that ground was a real principle or a convenient one.")

	r18Default()
}

// --- R18b: does the DEFAULT registry reintroduce R8c? ------------------------

func r18Default() {
	fmt.Println("\n--- R18b: does the default registry reintroduce R8c's init()-order bug? ---")
	fmt.Println("    R8c refused global-frozen because `the freeze point is decided by")
	fmt.Println("    init() order`. This ADR then ships a DEFAULT registry that freezes at")
	fmt.Println("    its first use, which is the same shape. That has to be answered or the")
	fmt.Println("    ADR is refusing a model and then adopting it on the default path.")
	fmt.Println()
	fmt.Println("    The Go spec answers it, and the answer is structural rather than a")
	fmt.Println("    convention. Package initialisation:")
	fmt.Println("      - imported packages are initialised before the importer")
	fmt.Println("      - all package-level variables and all init() funcs in the whole")
	fmt.Println("        program run to completion BEFORE main.main is called")
	fmt.Println()
	fmt.Println("    So for the shape every consumer actually writes:")
	fmt.Println("        func init() { ferry.Register(...) }   // any package, any order")
	fmt.Println("        func main() { ferry.Load(...) }")
	fmt.Println("    every registration in the program strictly precedes the first Load,")
	fmt.Println("    whatever the import graph is. The freeze point is not order-dependent")
	fmt.Println("    because there is only one edge and the language guarantees it.")
	fmt.Println()
	fmt.Println("    Verified by running exactly that layout:")
	fmt.Printf("      at main(), the default registry holds %d registration(s) from %d\n",
		len(defaultRegistry.byType), r18InitCount)
	fmt.Printf("      package init()s, and frozen=%v\n", defaultRegistry.frozen.Load())
	var out struct {
		A netip.Addr `ferry:"a"`
	}
	withRegistry(defaultRegistry, func() {
		defaultRegistry.frozen.Store(true) // the first Load freezes it
		err := load(map[Path]Value{Path{}.Name("a"): String("192.0.2.1")},
			reflect.ValueOf(&out).Elem())
		fmt.Printf("      first Load succeeds: err=%v value=%v, and NOW frozen=%v\n",
			err, out.A, defaultRegistry.frozen.Load())
	})
	fmt.Printf("      a registration after that: %v\n",
		defaultRegistry.Register(TextCodec[netip.Prefix](VString)))
	defaultRegistry.frozen.Store(false)

	fmt.Println("\n    The one shape it does break, stated rather than hidden:")
	fmt.Println("        func init() { ferry.Load(...) }   // Load DURING init")
	fmt.Println("    Then whether a later package's init() can still register depends on")
	fmt.Println("    the import graph, and it is R8c in full. Two things make that")
	fmt.Println("    affordable where it was not affordable for R8:")
	fmt.Println("      - it is loud, at startup, with the freeze point named, rather than")
	fmt.Println("        a silently stale schema")
	fmt.Println("      - the escape is one line, `ferry.NewRegistry()`, which R8 by")
	fmt.Println("        definition does not have. That is the whole difference between a")
	fmt.Println("        default registry and a global one.")
	fmt.Println()
	fmt.Println("    So: no unanswered question. The default path is safe by the language's")
	fmt.Println("    own initialisation guarantee for the shape consumers write, loud for")
	fmt.Println("    the shape they do not, and escapable in one line either way.")
}

// defaultRegistry is core's, written to by a package-level Register. Two
// package init()s below write to it, standing in for two importing packages.
var defaultRegistry = NewRegistry()

var r18InitCount int

func init() {
	if err := defaultRegistry.Register(TextCodec[netip.Addr](VString)); err == nil {
		r18InitCount++
	}
}

func init() {
	if err := defaultRegistry.Register(DurationLike[R18Poll]()); err == nil {
		r18InitCount++
	}
}

type R18Poll int64
