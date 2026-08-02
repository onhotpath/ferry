package main

// P12: what does each interface actually hold, and does it have its own
// lifetime? Written to answer "each surface has to justify its existence".

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func p12Lifetimes() {
	head("P12  three interfaces, three lifetimes")

	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "ferryproto")
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "cfg.yaml")
	os.WriteFile(file, []byte("db:\n  host: one\n"), 0o644)

	addrs := NewAddressSet([]Path{path("db", "host")})

	// (a) Bind does no I/O, and that is assertable rather than promised.
	fmt.Println("    (a) Bind against a plane that is not there")
	_, err := YAMLSource{Path: filepath.Join(dir, "nope.yaml")}.Bind(addrs)
	fmt.Printf("        Bind on a missing file  : err=%v\n", err)
	b, _ := YAMLSource{Path: filepath.Join(dir, "nope.yaml")}.Bind(addrs)
	_, err = b.Open(ctx)
	fmt.Printf("        then Open               : err=%v\n", oneLine(err))
	fmt.Println("        The conformance suite can assert this. With one merged")
	fmt.Println("        Open(ctx, addrs) it could not, because Open legitimately")
	fmt.Println("        does I/O, so 'checks run before I/O' would be prose again.")

	// (b) The injectivity refusal, with the backend call counter watching.
	fmt.Println("\n    (b) ADR-0003's check, with a counter on the plane")
	kv := newKV(map[string]string{"cfg/a/b": "x"})
	_, err = KVSource{KV: kv, Prefix: "cfg/"}.Bind(
		NewAddressSet([]Path{path("a/b"), path("a", "b")}))
	fmt.Printf("        Bind  : err=%v\n", oneLine(err))
	fmt.Printf("        backend calls made      : %d\n", kv.calls())

	// (c) Reload. This is the lifetime that makes Binding pay.
	fmt.Println("\n    (c) reload: ADR-0001 milestones watch")
	src := YAMLSource{Path: file}
	bind, _ := src.Bind(addrs) // once
	for i, want := range []string{"one", "two", "three"} {
		os.WriteFile(file, []byte("db:\n  host: "+want+"\n"), 0o644)
		r, err := bind.Open(ctx) // per reload
		if err != nil {
			fmt.Println("        open:", err)
			return
		}
		v, _ := r.Get(ctx, path("db", "host"))
		fmt.Printf("        reload %d -> %-14s (binds: 1, opens: %d)\n", i+1, v.GoString(), i+1)
	}
	fmt.Println("        Each reload re-reads the plane and re-uses the key table.")
	fmt.Println("        Merged, every reload would recompute the keys and re-run")
	fmt.Println("        the injectivity check over a set that has not changed.")

	// (d) Two planes, both checked before either is touched.
	fmt.Println("\n    (d) plane-to-plane, which ADR-0001 marks Enabled")
	bad := NewAddressSet([]Path{path("DB", "HOST"), path("DB_HOST")})
	kv2 := newKV(map[string]string{"cfg/DB/HOST": "h"})
	srcB, srcErr := KVSource{KV: kv2, Prefix: "cfg/"}.Bind(bad)
	_, sinkErr := EnvSink{}.Bind(bad)
	fmt.Printf("        source binds            : err=%v\n", srcErr)
	fmt.Printf("        sink binds              : err=%v\n", oneLine(sinkErr))
	fmt.Printf("        backend calls made      : %d\n", kv2.calls())
	fmt.Println("        The transfer is refused before the source is read. Merged,")
	fmt.Println("        you would read the whole source plane and then find out the")
	fmt.Println("        destination cannot name two of its addresses.")
	_ = srcB

	// (e) So what does each one hold?
	fmt.Println("\n    (e) the state each interface owns")
	fmt.Println("        Source   driver config          YAMLSource{Path}, EnvSource{Sep}")
	fmt.Println("                 lives as long as your program, knows no schema")
	fmt.Println("        Binding  config x address set   the precomputed key table")
	fmt.Println("                 pure computation, no clock, no plane")
	fmt.Println("        Reader   the plane at a moment  the parsed tree, the snapshot")
	fmt.Println("                 one load")
}

// EnvSink exists only for (d): env has no honest Dump, so this is not a real
// driver. It is here to show a flattening sink refusing at Bind.
type EnvSink struct{ Sep string }

func (s EnvSink) Bind(a *AddressSet) (WriteBinding, error) {
	if _, err := KeyTable(a, "env", envKeyFunc(s.Sep)); err != nil {
		return nil, err
	}
	return nil, nil
}
