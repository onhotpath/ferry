package main

// B3: option (b), "a driver-specific constructor plus a caller-facing
// LoadFrom(ctx, Reader)".
//
// The ticket's objection to it is 5.14's "two ways to set the loader in a new
// costume". That is a taste argument and it turns out not to be the binding
// one. Write the constructor and it stops before it starts: a Reader cannot
// exist without the key table, the key table cannot exist without the address
// set, and the address set is the compiled schema's.

import (
	"context"
	"fmt"
	"net/url"
	"testing"
)

// b3LooseReader is what a driver can write with no address set in hand: it
// derives the plane key per lookup instead of precomputing it.
type b3LooseReader struct {
	v   url.Values
	sep string
}

func (r b3LooseReader) Get(_ context.Context, p Path) (Value, error) {
	k, err := bQueryKey(r.sep)(p)
	if err != nil {
		return Absent, err
	}
	if vs, ok := r.v[k]; ok && len(vs) > 0 {
		return String(vs[0]), nil
	}
	return Absent, nil
}

func runB3() {
	ctx := context.Background()
	vals := b1Values()
	s := mustSchema[B1Filter]()

	fmt.Println("--- B3a: what a caller would have to be given ---")
	fmt.Println("    A driver constructor that produces a ferry.Reader needs the key")
	fmt.Println("    table. NewKeys' first argument is the address set:")
	fmt.Printf("      NewKeys(a *AddressSet, name string, f KeyFunc) (*Keys, error)\n")
	fmt.Printf("    and for this type that set is %v\n", s.as.All())
	fmt.Println("    which is a field of the compiled schema. ADR-0001 leaves \"whether")
	fmt.Println("    core ever exports a read-only schema view\" open and says to reopen it")
	fmt.Println("    only if a concrete need survives the recording-sink pattern; ADR-0010")
	fmt.Println("    declined to reopen it. So option (b) is not a second public shape on")
	fmt.Println("    the DRIVER. It is a request to export the compiled schema.")

	fmt.Println("\n--- B3b: the version that needs no address set, and what it costs ---")
	loose := b3LooseReader{vals, "."}
	strict, _ := NewKeys(s.as, "query", bQueryKey("."))
	addr := Path{}.Name("cursor")
	rows := []struct {
		name string
		fn   func()
	}{
		{"precomputed table, one Get", func() { _, _ = strict.Key(addr) }},
		{"derived per lookup, one Get", func() { _, _ = bQueryKey(".")(addr) }},
	}
	for _, r := range rows {
		res := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				r.fn()
			}
		})
		fmt.Printf("  %-34s %8d ns %6d B %4d allocs\n", r.name, res.NsPerOp(),
			res.AllocedBytesPerOp(), res.AllocsPerOp())
	}
	v, _ := loose.Get(ctx, addr)
	fmt.Printf("  the loose reader does work: /cursor -> %s\n", v.GoString())
	fmt.Println("  What it does not do is run ADR-0003's injectivity check over the set,")
	fmt.Println("  because it never sees a set. It checks one key at a time against")
	fmt.Println("  nothing, which is the obligation ADR-0004 put in core on route (b).")

	fmt.Println("\n--- B3c: and if the driver ships both, the two can disagree ---")
	fmt.Println("    One Source, one constructor, two spellings of the same grammar.")
	fmt.Println("    Nothing in the type system relates them, which is ADR-0010's")
	fmt.Println("    duplication axis 1 arriving in a driver instead of in core.")
	ns := mustSchema[B3Nested]()
	nested := url.Values{"db.host": {"db1"}, "db.port": {"5432"}}
	bound, _ := NewKeys(ns.as, "query", bQueryKey("."))
	drift := b3LooseReader{nested, "_"} // one option flipped, on one of the two paths
	for _, p := range ns.as.All() {
		k1, _ := bound.Key(p)
		k2, _ := bQueryKey("_")(p)
		v2, _ := drift.Get(ctx, p)
		fmt.Printf("    %-10s Source path -> %-10q   constructor path -> %-10q %s\n", p, k1, k2, v2.GoString())
	}
	fmt.Println("    Same driver, same request, two answers, no error from either.")
}

type B3Nested struct {
	DB struct {
		Host string `ferry:"host"`
		Port int    `ferry:"port"`
	} `ferry:"db"`
}
