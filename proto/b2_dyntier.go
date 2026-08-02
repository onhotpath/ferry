package main

// B2: what a held binding does to ADR-0004's dynamic tier.
//
// ADR-0004: "A key function serves the static tier from the precomputed table
// and mints a dynamic address on demand, running ADR-0003's legality and
// injectivity checks against everything ALREADY ISSUED."
//
// Under a Load that binds every time, "already issued" is scoped to one load,
// because the Keys value is created by Bind and dies with the call. Nobody had
// to decide that; it fell out of the entry point. A caller-held binding is the
// first thing that gives the phrase a second reading, and the second reading
// is not the same design.

import (
	"context"
	"fmt"
	"net/url"
)

type B2Conf struct {
	Limits map[string]int `ferry:"limits"`
}

func runB2() {
	ctx := context.Background()
	s := mustSchema[B2Conf]()
	as := NewAddressSet(s.addrs)
	fmt.Printf("  the STATIC address set handed to Bind: %v\n", as.All())
	fmt.Println("  every /limits/<key> is minted by the walk, from the value (ADR-0003's")
	fmt.Println("  two tiers), so it is never in that set.")

	// Two requests, each a legal map on its own. The key function joins on ".",
	// so a key containing a "." is refused as illegal, and two DIFFERENT keys
	// that join to one plane key are refused as non-injective. Neither request
	// contains a collision on its own.
	req1 := url.Values{"limits.rps": {"10"}}
	req2 := url.Values{"limits.burst": {"20"}}

	fmt.Println("\n--- B2a: two dumps through ONE held binding ---")
	bound, err := NewKeys(as, "query", bQueryKey("."))
	if err != nil {
		fmt.Println("  bind:", err)
		return
	}
	for i, keys := range [][]string{{"rps"}, {"burst"}, {"rps"}} {
		var errs []string
		for _, k := range keys {
			if _, err := bound.Key(Path{}.Name("limits").Name(k)); err != nil {
				errs = append(errs, err.Error())
			}
		}
		st, dyn := bound.held()
		fmt.Printf("  write %d, keys %v -> errs=%v   the binding now holds %d static + %d minted\n",
			i+1, keys, errs, st, dyn)
	}

	fmt.Println("\n  Nothing failed, and that is the point: the minted set only ever grows.")
	fmt.Println("  10k requests with distinct map keys is 10k entries in a value the")
	fmt.Println("  caller holds for the life of the process, none of them evictable.")
	big, _ := NewKeys(as, "query", bQueryKey("."))
	for i := range 10000 {
		_, _ = big.Key(Path{}.Name("limits").Name(fmt.Sprintf("tenant-%d", i)))
	}
	st, dyn := big.held()
	fmt.Printf("  10000 requests, one distinct map key each -> %d static + %d minted, none evictable\n", st, dyn)

	fmt.Println("\n--- B2b: and it is not only memory. A LEGAL write is refused. ---")
	fmt.Println("    The key function is the flat join ADR-0004's env driver has, so a")
	fmt.Println("    key containing the separator collides with one that does not.")
	shared, _ := NewKeys(as, "query", bQueryKey("."))
	seq := []struct {
		req  string
		addr Path
	}{
		{"request 1: limits = {\"http.port\": 1}", Path{}.Name("limits").Name("http.port")},
		{"request 2: limits = {\"http\": {...}}", Path{}.Name("limits").Name("http").Name("port")},
	}
	for _, r := range seq {
		k, err := shared.Key(r.addr)
		fmt.Printf("    %-38s -> key=%-16q err=%v\n", r.req, k, err)
	}
	fmt.Println("    Request 2 is refused for colliding with an address that belongs to a")
	fmt.Println("    request that finished. On its own it is a perfectly legal write.")

	fmt.Println("\n    The same two through a binding-per-load, which is what ferry.Load does:")
	for _, r := range seq {
		fresh, _ := NewKeys(as, "query", bQueryKey("."))
		k, err := fresh.Key(r.addr)
		fmt.Printf("    %-38s -> key=%-16q err=%v\n", r.req, k, err)
	}

	fmt.Println("\n--- B2c: the amendment. The static tier is the bind's; the minted set is the open's. ---")
	bk, _ := NewBoundKeys(as, "query", bQueryKey("."))
	for _, r := range seq {
		sess := bk.Session() // one per open, which is one per load
		k, err := sess.Key(r.addr)
		fmt.Printf("    %-38s -> key=%-16q err=%v\n", r.req, k, err)
	}
	fmt.Println("    Injectivity is a property of ONE write. Two writes to one plane at")
	fmt.Println("    different times are not required to be mutually injective, and")
	fmt.Println("    requiring it is what produced the refusal above.")

	fmt.Println("\n--- B2d: through the whole entry point, so it is not a helper-level claim ---")
	for _, tc := range []struct {
		name string
		src  FSource
	}{
		{"BQueryCtx : minted set on the binding", BQueryCtx{}},
		{"BQueryOpen: minted set on the open", BQueryOpen{}},
	} {
		b, err := BindTo[B2Conf](tc.src)
		if err != nil {
			fmt.Printf("  %-38s bind err=%v\n", tc.name, err)
			continue
		}
		var out []string
		for _, vals := range []url.Values{req1, req2, {"limits.http.port": {"1"}}} {
			cfg, err := b.Load(BQueryContext(ctx, vals))
			out = append(out, fmt.Sprintf("%v/%v", cfg.Limits, err != nil))
		}
		fmt.Printf("  %-38s %v\n", tc.name, out)
	}
}
