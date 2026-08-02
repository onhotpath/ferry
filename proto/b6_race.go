package main

// B6: a held binding under concurrent requests.
//
// #20 owns whether the WALK may run concurrently. This probe is about the
// other thing a binding does to concurrency, which #20 cannot answer because
// it is created by this ticket: under ferry.Load a binding is per call and is
// reached by one goroutine, and under a held binding it is reached by every
// request at once.
//
// Run under -race.

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"testing"
)

type B6Conf struct {
	Q      string         `ferry:"q"`
	Page   int            `ferry:"page"`
	Limits map[string]int `ferry:"limits"`
}

func b6Fan(n int, f func(i int)) {
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() { defer wg.Done(); f(i) }()
	}
	wg.Wait()
}

func runB6() {
	ctx := context.Background()

	fmt.Println("--- B6a: 64 concurrent requests through ONE binding, static schema ---")
	b, err := BindTo[B1Filter](BQueryCtx{})
	if err != nil {
		fmt.Println("  bind:", err)
		return
	}
	var mu sync.Mutex
	got := map[string]int{}
	b6Fan(64, func(i int) {
		v := url.Values{"q": {fmt.Sprintf("q%d", i%4)}, "page": {"1"}}
		c, err := b.Load(BQueryContext(ctx, v))
		mu.Lock()
		got[fmt.Sprintf("%v/%v", c.Q, err)]++
		mu.Unlock()
	})
	fmt.Printf("  distinct (value, error) outcomes: %d, all four q values present: %v\n",
		len(got), len(got) == 4)
	fmt.Println("  The static key table is written once by Bind and never again, so")
	fmt.Println("  reading it takes no lock - which is the property ADR-0004 measured")
	fmt.Println("  at 8.8 ns and which only a held binding ever actually collects.")

	fmt.Println("\n--- B6b: the same, with a map-typed field, so the dynamic tier is live ---")
	for _, tc := range []struct {
		name string
		src  FSource
	}{
		{"minted set on the binding", BQueryCtx{}},
		{"minted set on the open", BQueryOpen{}},
	} {
		mb, err := BindTo[B6Conf](tc.src)
		if err != nil {
			fmt.Printf("  %-28s bind err=%v\n", tc.name, err)
			continue
		}
		var emu sync.Mutex
		errs := map[string]int{}
		b6Fan(64, func(i int) {
			v := url.Values{"q": {"x"}, fmt.Sprintf("limits.tenant-%d", i): {"1"}}
			_, err := mb.Load(BQueryContext(ctx, v))
			emu.Lock()
			if err != nil {
				errs["error"]++
			} else {
				errs["ok"]++
			}
			emu.Unlock()
		})
		fmt.Printf("  %-28s %v\n", tc.name, errs)
	}
	fmt.Println("  Both are race-free; the mutex ADR-0004 put on the dynamic tier does")
	fmt.Println("  its job either way. What differs is what the value RETAINS, which is")
	fmt.Println("  B2's question and not this one.")

	fmt.Println("\n--- B6c: what the lock-free static tier is worth under contention ---")
	rows := []struct {
		name string
		fn   func(*testing.PB)
	}{
		{"held binding, 64 goroutines", func(pb *testing.PB) {
			v := url.Values{"q": {"x"}, "page": {"1"}}
			c := BQueryContext(ctx, v)
			for pb.Next() {
				_, _ = b.Load(c)
			}
		}},
		{"ferry.Load, 64 goroutines", func(pb *testing.PB) {
			v := url.Values{"q": {"x"}, "page": {"1"}}
			for pb.Next() {
				_, _ = Load[B1Filter](ctx, BQuery{Values: v})
			}
		}},
	}
	for _, r := range rows {
		res := testing.Benchmark(func(bb *testing.B) {
			bb.ReportAllocs()
			bb.SetParallelism(8)
			bb.RunParallel(r.fn)
		})
		fmt.Printf("  %-30s %8d ns %7d B %5d allocs\n", r.name, res.NsPerOp(),
			res.AllocedBytesPerOp(), res.AllocsPerOp())
	}
}
