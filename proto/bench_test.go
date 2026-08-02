package main

// P9: the hot path.
//
// The two-phase contract puts an Open in front of every Load. For a startup
// config read that is free. For xload's pitched use case - per-request HTTP
// query-param parsing - it is on the hot path, and ADR-0003 already measured
// that recomputing a driver's plane keys per call costs 109 to 477 ns against
// a 476 ns twelve-key cached load.
//
// So the question this benchmark decides is whether the key table can be
// memoised without putting a caching obligation on every driver author.

import (
	"context"
	"net/url"
	"testing"
)

func benchAddrs() (*AddressSet, url.Values) {
	s, err := compileCached[Config]()
	if err != nil {
		panic(err)
	}
	v := url.Values{}
	tab, err := KeyTable(s.addrs, "query", queryKey)
	if err != nil {
		panic(err)
	}
	for _, k := range tab {
		v.Set(k, "x")
	}
	return s.addrs, v
}

// Bind and Open on every load, which is what a one-shot ferry.Load does and
// what a per-request driver would be forced into if Bind were not reusable.
func BenchmarkBindAndLoadEveryTime(b *testing.B) {
	ctx := context.Background()
	addrs, vals := benchAddrs()
	b.ReportAllocs()
	for b.Loop() {
		r, err := bindOpen(ctx, QuerySource{Values: vals}, addrs)
		if err != nil {
			b.Fatal(err)
		}
		for _, p := range addrs.All() {
			if _, err := r.Get(ctx, p); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// The floor: no address model at all, a flat string key straight into a map.
func BenchmarkLoadFlatStringKeys(b *testing.B) {
	addrs, vals := benchAddrs()
	keys := make([]string, 0, addrs.Len())
	tab, _ := KeyTable(addrs, "query", queryKey)
	for _, p := range addrs.All() {
		keys = append(keys, tab[p])
	}
	b.ReportAllocs()
	for b.Loop() {
		for _, k := range keys {
			_ = vals[k]
		}
	}
}

// Absence signalling, at the granularity a driver actually pays it.
func BenchmarkAbsenceCommaOK(b *testing.B) {
	p := shapePlane{"SET": "8080"}
	b.ReportAllocs()
	for b.Loop() {
		_, _, _ = p.commaOK("MISSING")
	}
}

func BenchmarkAbsencePointer(b *testing.B) {
	p := shapePlane{"SET": "8080"}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = p.pointer("MISSING")
	}
}

func BenchmarkAbsenceSentinel(b *testing.B) {
	p := shapePlane{"SET": "8080"}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = p.sentinel("MISSING")
	}
}

func BenchmarkAbsenceKinded(b *testing.B) {
	p := shapePlane{"SET": "8080"}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = p.kinded("MISSING")
	}
}

// Shape B: Bind once at startup, Open per load. This is the per-request
// query-param case done the way the contract intends.
func BenchmarkLoadShapeB(b *testing.B) {
	ctx := context.Background()
	addrs, vals := benchAddrs()
	bind, err := QuerySourceB{}.Bind(addrs)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		r, err := bind.Open(ctx)
		if err != nil {
			b.Fatal(err)
		}
		for _, p := range addrs.All() {
			if _, err := r.(queryReader).GetWith(vals, p); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// P11's Keys on the static hit path. ADR-0003 priced a precomputed lookup at
// 10.4 ns / 0 allocs; the question is what supporting a later-minted address
// costs the addresses that were there all along.
func BenchmarkKeysStaticHit(b *testing.B) {
	addrs, _ := benchAddrs()
	k, err := NewKeys(addrs, "query", queryKey)
	if err != nil {
		b.Fatal(err)
	}
	p := addrs.All()[0]
	b.ReportAllocs()
	for b.Loop() {
		if _, err := k.Key(p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPlainMapHit(b *testing.B) {
	addrs, _ := benchAddrs()
	tab, _ := KeyTable(addrs, "query", queryKey)
	p := addrs.All()[0]
	b.ReportAllocs()
	for b.Loop() {
		_ = tab[p]
	}
}

func BenchmarkKeysMint(b *testing.B) {
	addrs, _ := benchAddrs()
	k, _ := NewKeys(addrs, "query", queryKey)
	p := path("labels", "env")
	if _, err := k.Key(p); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := k.Key(p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLoadSlim(b *testing.B) {
	ctx := context.Background()
	addrs, vals := benchAddrs()
	open, err := SlimQuery{Values: vals}.Bind(addrs)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		r, err := open(ctx)
		if err != nil {
			b.Fatal(err)
		}
		for _, p := range addrs.All() {
			if _, err := r.Get(ctx, p); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// The final contract, six-address load, bound once.
func BenchmarkLoadFinal(b *testing.B) {
	ctx := context.Background()
	addrs, vals := benchAddrs()
	open, err := FQuery{Values: vals}.Bind(addrs)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		r, err := open(ctx)
		if err != nil {
			b.Fatal(err)
		}
		for _, p := range addrs.All() {
			if _, err := r.Get(ctx, p); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// The same, binding every time, which is what a one-shot Load does.
func BenchmarkBindLoadFinal(b *testing.B) {
	ctx := context.Background()
	addrs, vals := benchAddrs()
	b.ReportAllocs()
	for b.Loop() {
		open, err := FQuery{Values: vals}.Bind(addrs)
		if err != nil {
			b.Fatal(err)
		}
		r, _ := open(ctx)
		for _, p := range addrs.All() {
			if _, err := r.Get(ctx, p); err != nil {
				b.Fatal(err)
			}
		}
	}
}
