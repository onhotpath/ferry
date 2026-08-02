package main

// B7: the audit. Every session so far has found its worst defect in the case
// its fixtures did not contain, so this probe is written against the claims
// above rather than against the design.
//
// The claims being audited:
//
//	1. "a caller-held binding is Load's first two phases, stopped at the phase
//	   boundary ADR-0004 already drew" - every fixture above is a LOAD.
//	2. "the minted set belongs to the open" - every fixture above mints from a
//	   map key, which is one of ADR-0003's two dynamic sources.
//	3. "one binding serves every request" - every fixture above binds a source
//	   whose configuration never changes.
//	4. B6c's parallel figure - measured with nothing else in the loop.

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
)

type B7Conf struct {
	Name   string         `ferry:"name"`
	Limits map[string]int `ferry:"limits"`
	Tags   []string       `ferry:"tags"`
}

func runB7() {
	ctx := context.Background()

	fmt.Println("--- B7a: the claim was made about Load. What is a Dump binding? ---")
	fmt.Println("    Dump's own code binds the sink AFTER the walk:")
	fmt.Println("      out := map[Path]Value{}; walk(...); sink.Bind(NewAddressSet(sortedAddrs(out)))")
	fmt.Println("    so the address set a sink is handed comes from the VALUE, not from")
	fmt.Println("    the type. Measured, one type and three values:")
	for _, v := range []B7Conf{
		{Name: "a"},
		{Name: "a", Limits: map[string]int{"rps": 1}},
		{Name: "a", Limits: map[string]int{"rps": 1, "burst": 2}, Tags: []string{"x"}},
	} {
		out, err := dumpTo(ctx, v)
		if err != nil {
			fmt.Println("    dump:", err)
			continue
		}
		fmt.Printf("    %-46v -> %v\n", fmt.Sprintf("%v/%v", v.Limits, v.Tags), sortedAddrs(out))
	}
	fmt.Println("    Three address sets, one type. A sink binding is therefore not")
	fmt.Println("    hoistable out of the call in general, and a Dump-side binding would")
	fmt.Println("    be reusable only for a schema with no dynamic tier - which is a")
	fmt.Println("    property of T that the caller would have to know.")
	fmt.Println("    That is ADR-0010's `members` operation and ADR-0004's enumeration")
	fmt.Println("    asymmetry arriving on this surface: Dump reads a map's keys off the")
	fmt.Println("    value, so it cannot know its own address set before it has one.")

	fmt.Println("\n--- B7b: the other dynamic source, a sequence index ---")
	fmt.Println("    Every minting fixture above used a map key. ADR-0003's other")
	fmt.Println("    dynamic segment is an Index, and a slice's length is a property of")
	fmt.Println("    the value too.")
	shared, _ := NewKeys(NewAddressSet(mustSchema[B7Conf]().addrs), "query", bQueryKey("."))
	for _, n := range []int{3, 1, 5} {
		var errs int
		for i := range n {
			if _, err := shared.Key(Path{}.Name("tags").Index(i)); err != nil {
				errs++
			}
		}
		st, dyn := shared.held()
		fmt.Printf("    a %d-element slice -> errs=%d, the binding now holds %d static + %d minted\n",
			n, errs, st, dyn)
	}
	fmt.Println("    An index never collides with another index, so the growth is")
	fmt.Println("    bounded by the longest slice ever loaded rather than unbounded.")
	fmt.Println("    The map-key case is the unbounded one, and it is the common one.")

	fmt.Println("\n--- B7c: a binding outlives the plane's configuration ---")
	dir, _ := os.MkdirTemp("", "b7")
	defer os.RemoveAll(dir)
	p1 := filepath.Join(dir, "a.yaml")
	os.WriteFile(p1, []byte("name: first\n"), 0o644)
	b, err := BindTo[B4Conf](FYAMLSource{Path: p1})
	if err != nil {
		fmt.Println("  bind:", err)
		return
	}
	v1, _ := b.Load(ctx)
	os.WriteFile(p1, []byte("name: second\n"), 0o644)
	v2, _ := b.Load(ctx)
	fmt.Printf("    same binding, file rewritten between loads -> %+v then %+v\n", v1, v2)
	fmt.Println("    The binding holds the driver's OpenFunc, so it re-reads the plane")
	fmt.Println("    every load. What it does NOT re-read is the Source value it was")
	fmt.Println("    built from: a caller who wants a different path binds again.")
	os.Remove(p1)
	_, e3 := b.Load(ctx)
	fmt.Printf("    plane removed entirely -> err=%v\n", e3 != nil)
	fmt.Println("    which is ADR-0004's \"Bind must succeed against an unreachable")
	fmt.Println("    plane, and Open is where it fails\", holding across loads too.")

	fmt.Println("\n--- B7d: auditing this ADR's own timings ---")
	fmt.Printf("    GOMAXPROCS=%d, and every benchmark loop here contains nothing but the\n", runtime.GOMAXPROCS(0))
	fmt.Println("    operation. Run three times, B1b's ns/op moved 4399 / 5596 / 5393 for")
	fmt.Println("    one row and B6c's parallel rows crossed over entirely, while every")
	fmt.Println("    allocation column was identical to the byte.")
	fmt.Println("    So the ADR quotes allocations as measurements and times as a scale.")
	fmt.Println("    An earlier draft of this probe claimed a 4.8x under contention; it")
	fmt.Println("    does not reproduce and it is withdrawn.")

	fmt.Println("\n--- B7d2: one row of ADR-0010's own table, re-run ---")
	fmt.Println("    Auditing an inherited claim rather than my own: ADR-0010 prints a")
	fmt.Println("    two-row table for LoadOver's failure return and calls it measured.")
	fmt.Println("    Its probe's zero-reading row calls loadFrom, which returns the SEED,")
	fmt.Println("    so as committed both rows print the seed. Re-run with a variant that")
	fmt.Println("    actually returns the zero value:")
	bad := map[Path]Value{Path{}.Name("name"): Number("1")}
	live := B4Conf{Name: "db1"}
	seedRet, e1 := loadFrom(ctx, live, bad)
	zeroRet, _ := b7LoadZero(ctx, live, bad)
	fmt.Printf("    the seed reading -> %+v   (err=%v)\n", seedRet, e1 != nil)
	fmt.Printf("    the zero reading -> %+v\n", zeroRet)
	fmt.Println("    ADR-0010's conclusion is unaffected and its published row is what a")
	fmt.Println("    correct probe produces; what was not measured is the row itself.")

	fmt.Println("\n--- B7d3: does holding a binding change ADR-0009's freeze? ---")
	r1, r2 := NewRegistry(), NewRegistry()
	_ = Compile[B4Conf](WithRegistry(r1))
	bb, _ := Bind[B4Conf](FYAMLSource{Path: "nope.yaml"}, WithRegistry(r2))
	_ = bb
	fmt.Printf("    after Compile[T](WithRegistry(r1)) -> frozen=%v\n", r1.frozen.Load())
	fmt.Printf("    after Bind[T](src, WithRegistry(r2)) -> frozen=%v\n", r2.frozen.Load())
	fmt.Println("    Bind RETAINS a compiled schema, so it freezes; Compile discards one,")
	fmt.Println("    so it does not. That is ADR-0010's rule with a new caller and no new")
	fmt.Println("    wording: caching and freezing are one decision.")

	fmt.Println("\n--- B7e: and the case the ticket is actually about, end to end ---")
	fmt.Println("    A handler, written both ways, against the same request.")
	vals := url.Values{"q": {"widgets"}, "page": {"2"}}
	one, err1 := Load[B1Filter](ctx, BQuery{Values: vals})
	hb, _ := BindTo[B1Filter](BQueryCtx{})
	two, err2 := hb.Load(BQueryContext(ctx, vals))
	fmt.Printf("    ferry.Load[Filter](ctx, query.Source{Values: r.URL.Query()}) -> %+v %v\n", one, err1)
	fmt.Printf("    b.Load(query.WithValues(r.Context(), r.URL.Query()))          -> %+v %v\n", two, err2)
	fmt.Printf("    identical: %v\n", one == two)
}

// b7LoadZero is LoadOver reading ADR-0011's rule the other way: yield the zero
// value on failure rather than the seed.
func b7LoadZero[T any](ctx context.Context, seed T, vals map[Path]Value) (T, error) {
	out, err := loadFrom(ctx, seed, vals)
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}
