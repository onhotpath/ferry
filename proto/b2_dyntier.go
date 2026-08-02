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

	// The key function is ADR-0004's env transform: upper-case, and every
	// other character becomes an underscore. It is not injective over map
	// keys, which is the case ADR-0003 makes a driver obligation and ADR-0004
	// checks in core. No single request below contains a collision.

	fmt.Println("\n--- B2a: two dumps through ONE held binding ---")
	bound, err := NewKeys(as, "env", bEnvKey)
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
	big, _ := NewKeys(as, "env", bEnvKey)
	for i := range 10000 {
		_, _ = big.Key(Path{}.Name("limits").Name(fmt.Sprintf("tenant-%d", i)))
	}
	st, dyn := big.held()
	fmt.Printf("  10000 requests, one distinct map key each -> %d static + %d minted, none evictable\n", st, dyn)
	fmt.Printf("  and what that costs, measured on the live heap: %d KiB for the 10000\n", bKeysHeap(as, 10000)/1024)

	fmt.Println("\n--- B2b: and where a retained set is not only memory ---")
	fmt.Println("    A minted address comes from the VALUE on Dump and from the PLANE on")
	fmt.Println("    Load (ADR-0004's enumeration asymmetry), so this case is Dump's:")
	fmt.Println("    ADR-0004's env transform maps \"http-port\" and \"http_port\" onto one")
	fmt.Println("    plane key. Two dumps, each holding ONE of them:")
	shared, _ := NewKeys(as, "env", bEnvKey)
	seq := []struct {
		req  string
		addr Path
	}{
		{"request 1: limits = {\"http-port\": 1}", Path{}.Name("limits").Name("http-port")},
		{"request 2: limits = {\"http_port\": 2}", Path{}.Name("limits").Name("http_port")},
	}
	for _, r := range seq {
		k, err := shared.Key(r.addr)
		fmt.Printf("    %-38s -> key=%-16q err=%v\n", r.req, k, err)
	}
	fmt.Println("    Write 2 is refused for colliding with an address that belongs to a")
	fmt.Println("    write that finished. On its own it is a perfectly legal dump.")

	fmt.Println("\n    The same two through a binding-per-load, which is what ferry.Load does:")
	for _, r := range seq {
		fresh, _ := NewKeys(as, "env", bEnvKey)
		k, err := fresh.Key(r.addr)
		fmt.Printf("    %-38s -> key=%-16q err=%v\n", r.req, k, err)
	}

	fmt.Println("\n--- B2c: the amendment. The static tier is the bind's; the minted set is the open's. ---")
	bk, _ := NewBoundKeys(as, "env", bEnvKey)
	for _, r := range seq {
		sess := bk.Session() // one per open, which is one per load
		k, err := sess.Key(r.addr)
		fmt.Printf("    %-38s -> key=%-16q err=%v\n", r.req, k, err)
	}
	fmt.Println("    Injectivity is a property of ONE write. Two writes to one plane at")
	fmt.Println("    different times are not required to be mutually injective, and")
	fmt.Println("    requiring it is what produced the refusal above.")

	fmt.Println("\n--- B2d: what the LOAD side actually inherits, which is narrower ---")
	fmt.Println("    On Load a dynamic address is enumerated FROM the plane, so two loads'")
	fmt.Println("    minted addresses come out of one key space and a well-behaved driver")
	fmt.Println("    cannot produce the refusal above. What Load does inherit is the")
	fmt.Println("    growth, and it inherits all of it. 20000 requests, one tenant each,")
	fmt.Println("    through ONE binding and through ferry.Load:")
	var seen []*Keys
	for _, tc := range []struct {
		name string
		src  FSource
	}{
		{"minted set on the binding", BEnvCtx{Seen: &seen}},
		{"minted set on the open", BEnvOpen{}},
	} {
		b, err := BindTo[B2Conf](tc.src)
		if err != nil {
			fmt.Printf("  %-28s bind err=%v\n", tc.name, err)
			continue
		}
		var got int
		for i := range 20000 {
			v := url.Values{fmt.Sprintf("LIMITS_TENANT%d", i): {"1"}}
			cfg, err := b.Load(BQueryContext(ctx, v))
			if err != nil {
				fmt.Printf("  %-28s load %d: %v\n", tc.name, i, err)
				break
			}
			got += len(cfg.Limits)
		}
		retained := 0
		for _, k := range seen {
			_, d := k.held()
			retained += d
		}
		seen = nil
		fmt.Printf("  %-28s %d tenants loaded, %d addresses retained by the binding\n",
			tc.name, got, retained)
	}
	fmt.Println("    One binding, one process, and the retained set is bounded only by the")
	fmt.Println("    number of distinct map keys the process has ever seen. That is the")
	fmt.Println("    same class ADR-0009 measured for a per-call registry and ADR-0010")
	fmt.Println("    restated as a property of the cache, arriving in a third place.")
}
