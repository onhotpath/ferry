package main

// P10: the contract axes, and therefore the first-party driver list.
//
// ADR-0002 deferred the list to this ticket, because its admission rule is
// "a first-party driver ships only to exercise an axis of the driver contract
// that no existing first-party driver exercises", and the axes are a property
// of the signatures. Now that the signatures exist, the axes can be read off
// them rather than guessed.
//
// It also runs the conformance obligations that fall out, against the four
// drivers, so the list is chosen from what is actually covered.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type axis struct {
	name                              string
	env, query, kv, yaml, memoryPlane bool
}

func p10Fidelity() {
	head("P10  contract axes, and the first-party driver list ADR-0002 deferred")

	axes := []axis{
		{"produces a plane key (KeyTable, injectivity)", true, true, true, false, false},
		{"walks segments as a tree instead", false, false, false, true, false},
		{"has a serialization format", false, false, false, true, false},
		{"carries plane-side type information", false, false, false, true, false},
		{"opaque bytes only", false, false, true, false, false},
		{"real I/O, cancellation, partial failure", false, false, true, true, false},
		{"batch versus lazy inside one Open", false, false, true, false, false},
		{"whole-document sink: Commit and Abort", false, false, false, true, false},
		{"no honest Dump at all", true, false, false, false, false},
		{"per-request, hot path", false, true, false, false, false},
		{"enumeration", false, false, false, true, false},
	}
	fmt.Printf("    %-46s %4s %5s %3s %5s %5s\n", "axis", "env", "query", "kv", "yaml", "mem")
	for _, a := range axes {
		fmt.Printf("    %-46s %4s %5s %3s %5s %5s\n", a.name,
			mark(a.env), mark(a.query), mark(a.kv), mark(a.yaml), mark(a.memoryPlane))
	}
	fmt.Println("    The memory plane column is empty on every axis, which is")
	fmt.Println("    ADR-0002's own point stated as a measurement: it has no format")
	fmt.Println("    and no I/O, so it cannot keep the conformance suite honest.")

	// The conformance obligations these signatures create, run for real.
	fmt.Println("\n    (b) obligations the compiler cannot check, run against a driver")
	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "ferryproto")
	defer os.RemoveAll(dir)

	// 1. absence is not emptiness
	env := EnvSource{Lookup: func(k string) (string, bool) {
		if k == "EMPTY" {
			return "", true
		}
		return "", false
	}}
	as := NewAddressSet([]Path{path("empty"), path("missing")})
	r, _ := bindOpen(ctx, env, as)
	e, _ := r.Get(ctx, path("empty"))
	m, _ := r.Get(ctx, path("missing"))
	check("absence is not emptiness", e.Kind() == VString && m.Kind() == VAbsent)

	// 2. an address outside the opened set is an error, not a miss
	_, err := r.Get(ctx, path("never-opened"))
	check("unopened address is an error, not absent", err != nil)

	// 3. a parse failure is an error, not an empty reader (5.11)
	bad := filepath.Join(dir, "bad.yaml")
	os.WriteFile(bad, []byte("a: [1, 2\n"), 0o644)
	_, err = bindOpen(ctx, YAMLSource{Path: bad}, NewAddressSet(nil))
	check("malformed plane fails Open loudly (5.11)", err != nil)

	// 4. the key function is injective, checked before I/O
	kv := newKV(nil)
	_, err = bindOpen(ctx, KVSource{KV: kv, Prefix: "cfg/"},
		NewAddressSet([]Path{path("a/b"), path("a", "b")}))
	check("non-injective key function refused before I/O", err != nil && kv.calls() == 0)

	// 5. determinism of that refusal
	msgs := map[string]int{}
	for range 300 {
		_, err := buildKeyTable(NewAddressSet([]Path{path("a/b"), path("a", "b"), path("c/d"), path("c", "d")}),
			"kv", kvKey("cfg/"))
		msgs[err.Error()]++
	}
	check(fmt.Sprintf("300 refusals produced %d distinct strings", len(msgs)), len(msgs) == 1)

	// 6. a sink leaves the plane untouched when the walk aborts
	out := filepath.Join(dir, "out.yaml")
	os.WriteFile(out, []byte("keep: me\n"), 0o644)
	w, _ := bindOpenSink(ctx, YAMLSink{Path: out}, NewAddressSet([]Path{path("a")}))
	_ = w.Set(ctx, path("a"), String("1"))
	w.Abort()
	b, _ := os.ReadFile(out)
	check("Abort leaves the plane byte-identical", string(b) == "keep: me\n")

	fmt.Println("\n    (c) the driver list this implies")
	fmt.Println("        yaml   - format, tree walk, plane-side types, Commit/Abort,")
	fmt.Println("                 enumeration. Five axes nothing else reaches.")
	fmt.Println("        kv     - real I/O, opaque bytes, batch-versus-lazy, and the")
	fmt.Println("                 dynamically read-only sink.")
	fmt.Println("        env    - the flat key function with a transform, and the")
	fmt.Println("                 source-with-no-sink case, which is the one that")
	fmt.Println("                 keeps Source and Sink honestly separate.")
	fmt.Println("        query  - the only per-request axis. Weakest case of the four:")
	fmt.Println("                 its key function is a flat join like env's, so it")
	fmt.Println("                 earns its place on hot-path pressure alone.")
}

func mark(b bool) string {
	if b {
		return "  x  "
	}
	return "  .  "
}

func check(what string, ok bool) {
	s := "FAIL"
	if ok {
		s = "ok"
	}
	fmt.Printf("        %-4s %s\n", s, what)
}
