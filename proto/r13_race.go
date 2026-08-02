package main

// R13: registration racing a compile.
//
// Concurrency is #20's and this probe decides none of it. What it does is
// establish which of R6, R7 and R8 needs a synchronisation story at all,
// because "the registry is a map and Load reads it" is a data race in the Go
// memory model whether or not any ADR mentions goroutines.
//
// Run with: P19=13 GORACE=halt_on_error=0 go run -race .

import (
	"fmt"
	"net/netip"
	"reflect"
	"sync"
)

func runR13() {
	fmt.Println("--- R13a: a mutable registry read by a compile is a data race ---")
	fmt.Println("    This is the shape under R6 and under R7-without-a-freeze. Under")
	fmt.Println("    `go run -race` it is reported; under `go run` it is a corrupt map")
	fmt.Println("    read or a silently missed registration.")

	r := NewRegistry()
	type conf struct{ A netip.Addr }
	t := reflect.TypeFor[conf]()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = r.Register(StringCodec(netip.Addr.String, netip.ParseAddr))
	}()
	go func() {
		defer wg.Done()
		withRegistry(r, func() { _, _ = compile(t) })
	}()
	wg.Wait()
	fmt.Println("    (the probe's own install() takes a mutex, so this run is serialised;")
	fmt.Println("     the race is in Registry.byType being written after a reader exists,")
	fmt.Println("     which no mutex inside ferry can fix because the READ is the whole")
	fmt.Println("     point of a lock-free cache hit.)")

	fmt.Println("\n--- R13b: a FROZEN registry needs no synchronisation on the read path ---")
	fmt.Println("    The map is written before the first reader exists and never again,")
	fmt.Println("    so the read path is a plain map lookup with no lock and no atomic.")
	fmt.Println("    ADR-0004 already relies on exactly this shape and says so:")
	fmt.Println("      `The static table is written once and never again, so reading it")
	fmt.Println("       takes no lock` - 8.8 ns/op against 20.0 ns/op with one mutex.")
	fmt.Println("    So freezing is not only what makes the schema cache sound (R12c), it")
	fmt.Println("    is what keeps #20's problem from starting inside the registry.")

	frozen := NewRegistry()
	_ = frozen.Register(StringCodec(netip.Addr.String, netip.ParseAddr))
	frozen.frozen.Store(true)
	var wg2 sync.WaitGroup
	results := make([]int, 8)
	for i := range 8 {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			// A frozen registry is read-only, so a reader needs no install().
			if _, ok := frozen.lookup(reflect.TypeFor[netip.Addr]()); ok {
				results[i] = 1
			}
		}()
	}
	wg2.Wait()
	n := 0
	for _, v := range results {
		n += v
	}
	fmt.Printf("    8 concurrent readers of a frozen registry -> %d/8 found the codec\n", n)
	fmt.Printf("    a write after the freeze                  -> %v\n",
		frozen.Register(StringCodec(netip.Prefix.String, netip.ParsePrefix)))

	fmt.Println("\n--- R13c: what this does NOT decide ---")
	fmt.Println("    Whether the walk itself may run concurrently is #20's, and nothing")
	fmt.Println("    here constrains it. What #19 hands #20 is a registry that is")
	fmt.Println("    immutable for the whole life of every schema compiled against it,")
	fmt.Println("    which is one fewer shared mutable thing than a global table would")
	fmt.Println("    have handed it.")
}
