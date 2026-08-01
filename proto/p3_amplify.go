package main

// P3: 5.13, the per-key pull model amplifies backend round trips.
//
// Two things to check, and the second is an audit of a claim that looked
// obvious. 5.13 reproduced two defects at once: two fields sharing one key
// cost two backend calls, and SerialLoader multiplied by the number of
// backends. ADR-0003 may already have killed the first one for free.

import (
	"context"
	"fmt"
)

type Config struct {
	Name string `ferry:"name"`
	DB   struct {
		Host string `ferry:"host"`
		Port int    `ferry:"port"`
		Auth struct {
			User string `ferry:"user"`
			Pass string `ferry:"pass"`
		} `ferry:"auth"`
	} `ferry:"db"`
	Timeout string `ferry:"timeout"`
}

func p3Amplify() {
	head("P3  5.13, round-trip amplification, and what ADR-0003 already fixed")

	ctx := context.Background()
	s, err := compile[Config]()
	if err != nil {
		fmt.Println("    compile:", err)
		return
	}

	kvData := map[string]string{
		"cfg/name": "svc", "cfg/db/host": "h", "cfg/db/port": "5432",
		"cfg/db/auth/user": "u", "cfg/db/auth/pass": "p", "cfg/timeout": "30s",
	}

	// (a) The xload shape, for the baseline: one call per leaf, no memoisation.
	kf := kvKey("cfg/")
	kv := newKV(kvData)
	for _, l := range s.leaves {
		k, _ := kf(l.Path)
		_, _ = kv.Get(k)
	}
	fmt.Printf("    (a) per-key pull, %d leaves           : %d backend calls\n", len(s.leaves), kv.calls())

	// (b) The two-phase contract, driver choosing lazy.
	kv = newKV(kvData)
	r, _ := bindOpen(ctx, KVSource{KV: kv, Prefix: "cfg/", Lazy: true}, s.addrs)
	for _, p := range s.addrs.All() {
		_, _ = r.Get(ctx, p)
	}
	fmt.Printf("    (b) two-phase, driver chose lazy      : %d backend calls\n", kv.calls())

	// (c) The same contract, driver choosing batch. No second interface, no
	//     optional upgrade: the whole address set arrives at Open, so the
	//     driver can serve every Get from one round trip if it wants to.
	kv = newKV(kvData)
	r, _ = bindOpen(ctx, KVSource{KV: kv, Prefix: "cfg/"}, s.addrs)
	for _, p := range s.addrs.All() {
		_, _ = r.Get(ctx, p)
	}
	fmt.Printf("    (c) two-phase, driver chose batch     : %d backend calls\n", kv.calls())

	// (d) Composition, 5.12's half. SerialLoader queried every loader for
	//     every key with no short-circuit, because empty meant absent so it
	//     could not tell a hit from a miss.
	fmt.Println("\n    (d) composition")
	a, b, c := newKV(map[string]string{"cfg/name": "from-a"}), newKV(kvData), newKV(kvData)
	// xload's SerialLoader: last non-empty wins, so every backend is asked.
	for _, l := range s.leaves {
		k, _ := kf(l.Path)
		_, _ = a.Get(k)
		_, _ = b.Get(k)
		_, _ = c.Get(k)
	}
	fmt.Printf("        xload SerialLoader, 3 backends    : %d calls (%d+%d+%d)\n",
		a.calls()+b.calls()+c.calls(), a.calls(), b.calls(), c.calls())

	a, b, c = newKV(map[string]string{"cfg/name": "from-a"}), newKV(kvData), newKV(kvData)
	comp := composite{}
	for _, src := range []Source{
		KVSource{KV: a, Prefix: "cfg/"}, KVSource{KV: b, Prefix: "cfg/"}, KVSource{KV: c, Prefix: "cfg/"},
	} {
		rr, err := bindOpen(ctx, src, s.addrs)
		if err != nil {
			fmt.Println("        open:", err)
			return
		}
		comp = append(comp, rr)
	}
	for _, p := range s.addrs.All() {
		_, _ = comp.Get(ctx, p)
	}
	fmt.Printf("        ferry first-present-wins, batch   : %d calls (%d+%d+%d)\n",
		a.calls()+b.calls()+c.calls(), a.calls(), b.calls(), c.calls())
	fmt.Println("        Composition needs no core surface: a composite is a Source")
	fmt.Println("        whose Open opens its children. Short-circuiting is correct")
	fmt.Println("        only because absence is now observable - 5.12's real cause.")

	// (e) The audit. 5.13's first reproduction was "2 fields sharing one key
	//     produce 2 backend calls", which ADR-0003 may have already made
	//     unrepresentable. Check rather than assert.
	fmt.Println("\n    (e) audit: does in-load memoisation still have a job?")
	type Shared struct {
		Primary string `ferry:"host"`
		Legacy  string `ferry:"host"`
	}
	if _, err := compile[Shared](); err != nil {
		fmt.Printf("        two fields at one address: %v\n", firstLine(err.Error()))
		fmt.Println("        so the duplicate-key half of 5.13 is not a source-contract")
		fmt.Println("        problem at all: prefix-freeness makes it a schema that")
		fmt.Println("        does not compile. Memoising within a load has nothing")
		fmt.Println("        left to deduplicate on the static address set.")
	} else {
		fmt.Println("        accepted - memoisation is still needed")
	}
}

type composite []Reader

func (c composite) Get(ctx context.Context, p Path) (Value, error) {
	for _, r := range c {
		v, err := r.Get(ctx, p)
		if err != nil {
			return Absent, err
		}
		// Short-circuit on presence. Under xload this was unwritable,
		// because empty meant absent so there was no hit to detect.
		if v.Present() {
			return v, nil
		}
	}
	return Absent, nil
}

func firstLine(s string) string {
	for i := range s {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
