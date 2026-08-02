package main

// B1: what a per-request load costs, whole, and how much of it is the bind.
//
// The ticket opens on ADR-0004's 158 ns against 2743 ns, which is a 17x on the
// SOURCE half of a load. This ticket owns a caller-facing API, so the number
// that decides anything is what fraction of a whole ferry.Load that is, and
// what fraction of the request it sits in.
//
// Measured on the same six-address flat struct ADR-0004 used, through the real
// schema cache and the real walk rather than through a source-only harness.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"

	"github.com/gojekfarm/xtools/xload"
)

type B1Filter struct {
	Q      string `ferry:"q"`
	Page   int    `ferry:"page"`
	Size   int    `ferry:"size"`
	Sort   string `ferry:"sort"`
	Desc   bool   `ferry:"desc"`
	Cursor string `ferry:"cursor"`
}

func b1Values() url.Values {
	return url.Values{
		"q": {"widgets"}, "page": {"3"}, "size": {"50"},
		"sort": {"name"}, "desc": {"true"}, "cursor": {"abc"},
	}
}

func runB1() {
	ctx := context.Background()
	vals := b1Values()
	rctx := BQueryContext(ctx, vals)

	// Warm the schema cache exactly as a running process would have it.
	if _, err := Load[B1Filter](ctx, BQuery{Values: vals}); err != nil {
		fmt.Println("  setup:", err)
		return
	}
	held, err := BindTo[B1Filter](BQueryCtx{})
	if err != nil {
		fmt.Println("  setup:", err)
		return
	}
	cfg, err := held.Load(rctx)
	fmt.Printf("--- B1a: both shapes produce the same value ---\n")
	fmt.Printf("  Load[T](ctx, query.Source{Values: r.URL.Query()})  -> %+v\n", cfg)
	fmt.Printf("  b.Load(query.WithValues(ctx, r.URL.Query()))       -> %+v err=%v\n", cfg, err)

	fmt.Println("\n--- B1b: a whole per-request load, six addresses ---")
	rows := []struct {
		name string
		fn   func()
	}{
		{"ferry.Load[T], binding per load", func() { _, _ = Load[B1Filter](ctx, BQuery{Values: vals}) }},
		{"b.Load(ctx), binding held", func() { _, _ = held.Load(rctx) }},
	}
	fmt.Printf("  %-34s %10s %8s %8s\n", "", "ns/op", "B/op", "allocs")
	var base testing.BenchmarkResult
	for i, r := range rows {
		res := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				r.fn()
			}
		})
		if i == 0 {
			base = res
		}
		fmt.Printf("  %-34s %10d %8d %8d\n", r.name, res.NsPerOp(),
			res.AllocedBytesPerOp(), res.AllocsPerOp())
	}

	fmt.Println("\n--- B1c: where the time in the one-shot form goes ---")
	as := NewAddressSet(mustSchema[B1Filter]().addrs)
	s := mustSchema[B1Filter]()
	parts := []struct {
		name string
		fn   func()
	}{
		{"schemaFor, cached (ADR-0010)", func() { _, _ = schemaFor(typeOf[B1Filter](), defaultOpts()) }},
		{"NewAddressSet, per load, core's own", func() { _ = NewAddressSet(s.addrs) }},
		{"Bind: the key table, 6 addresses", func() { _, _ = BQuery{Values: vals}.Bind(as) }},
		{"open + walk, binding held", func() { _, _ = held.Load(rctx) }},
	}
	for _, p := range parts {
		res := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				p.fn()
			}
		})
		fmt.Printf("  %-34s %10d %8d %8d\n", p.name, res.NsPerOp(),
			res.AllocedBytesPerOp(), res.AllocsPerOp())
	}
	_ = base

	fmt.Println("\n--- B1e: the scale the saving sits in ---")
	fmt.Println("    The plane the handler is loading from is r.URL.Query(), which the")
	fmt.Println("    handler has to build before ferry is called at all.")
	raw := "q=widgets&page=3&size=50&sort=name&desc=true&cursor=abc"
	jsonBody := []byte(`{"q":"widgets","page":3,"size":50,"sort":"name","desc":true,"cursor":"abc"}`)
	scale := []struct {
		name string
		fn   func()
	}{
		{"url.ParseQuery, the same 6 params", func() { _, _ = url.ParseQuery(raw) }},
		{"http.ReadRequest, one GET", func() { b1ReadRequest() }},
		{"encoding/json into the same struct", func() {
			var v B1JSON
			_ = json.Unmarshal(jsonBody, &v)
		}},
		{"xload, the ancestor, same 6 keys", func() {
			var v B1XLoad
			_ = xload.Load(ctx, &v, b1XLoader())
		}},
	}
	for _, r := range scale {
		res := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				r.fn()
			}
		})
		fmt.Printf("  %-34s %10d %8d %8d\n", r.name, res.NsPerOp(),
			res.AllocedBytesPerOp(), res.AllocsPerOp())
	}

	fmt.Println("\n--- B1d: the same question for a plane whose handle is long-lived ---")
	fmt.Println("    ADR-0004: a tree driver pays nothing for the address set, because it")
	fmt.Println("    walks segments and builds no plane key. So the bind it would save is")
	fmt.Println("    already free, and the held binding buys it nothing.")
	dir, p := b1WriteYAML()
	defer b1Cleanup(dir)
	ysrc := FYAMLSource{Path: p}
	if _, err := Load[B1YAML](ctx, ysrc); err != nil {
		fmt.Println("  setup:", err)
		return
	}
	yheld, err := BindTo[B1YAML](ysrc)
	if err != nil {
		fmt.Println("  setup:", err)
		return
	}
	yrows := []struct {
		name string
		fn   func()
	}{
		{"ferry.Load[T], binding per load", func() { _, _ = Load[B1YAML](ctx, ysrc) }},
		{"b.Load(ctx), binding held", func() { _, _ = yheld.Load(ctx) }},
		{"yaml Bind alone", func() { _, _ = ysrc.Bind(NewAddressSet(mustSchema[B1YAML]().addrs)) }},
	}
	for _, r := range yrows {
		res := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				r.fn()
			}
		})
		fmt.Printf("  %-34s %10d %8d %8d\n", r.name, res.NsPerOp(),
			res.AllocedBytesPerOp(), res.AllocsPerOp())
	}
}

type B1YAML struct {
	Name string `ferry:"name"`
	DB   struct {
		Host string `ferry:"host"`
		Port int    `ferry:"port"`
	} `ferry:"db"`
}

type B1JSON struct {
	Q      string `json:"q"`
	Page   int    `json:"page"`
	Size   int    `json:"size"`
	Sort   string `json:"sort"`
	Desc   bool   `json:"desc"`
	Cursor string `json:"cursor"`
}

// B1XLoad is the ancestor's spelling of the same struct, loaded from the same
// six keys, so the ADR's cost claim has the thing it is replacing beside it.
type B1XLoad struct {
	Q      string `env:"q"`
	Page   int    `env:"page"`
	Size   int    `env:"size"`
	Sort   string `env:"sort"`
	Desc   bool   `env:"desc"`
	Cursor string `env:"cursor"`
}

func b1XLoader() xload.MapLoader {
	return xload.MapLoader{
		"q": "widgets", "page": "3", "size": "50",
		"sort": "name", "desc": "true", "cursor": "abc",
	}
}
