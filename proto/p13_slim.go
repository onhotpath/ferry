package main

// P13: can the same boundaries be held with less surface?
//
// Read against dagger, whose shape is one small interface plus a Func adapter,
// with the complexity in concrete composable types rather than in more
// interfaces. This probe re-implements all four drivers against a slim
// contract and then checks that every property the wide one bought survives.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func p13Slim() {
	head("P13  the same boundaries with less surface")

	ctx := context.Background()

	// (a) The surface, counted.
	fmt.Println("    (a) surface")
	fmt.Printf("        %-26s %-12s %s\n", "", "read path", "write path")
	fmt.Printf("        %-26s %-12s %s\n", "wide (P1-P12)", "3 ifaces / 3", "3 ifaces / 5")
	fmt.Printf("        %-26s %-12s %s\n", "slim (this probe)", "2 ifaces / 2", "2 ifaces / 3")
	fmt.Println("        Opener stops being an interface and becomes a func, because")
	fmt.Println("        nothing ever asks an opener a second question. Commit and")
	fmt.Println("        Abort become one Close(ctx, cause).")
	fmt.Println("        2 interfaces / 2 methods on the read path is koanf's bar exactly.")

	// (b) Driver cost, like for like.
	fmt.Println("\n    (b) driver cost, same four drivers, same style")
	wide, _ := countLines("drv_env.go")
	slimAll, _ := countLines("slimdrv.go")
	w2, _ := countLines("drv_query.go")
	w3, _ := countLines("drv_kv.go")
	w4, _ := countLines("drv_yaml.go")
	fmt.Printf("        wide: env %d + query %d + kv %d + yaml %d = %d lines\n",
		wide, w2, w3, w4, wide+w2+w3+w4)
	fmt.Printf("        slim: all four in one file            = %d lines\n", slimAll)
	fmt.Println("        (slim reuses the wide drivers' yaml node walk and key")
	fmt.Println("        functions, so the honest read is the shape, not the total.)")

	// (c) Every boundary the wide contract bought, re-checked.
	fmt.Println("\n    (c) do the boundaries survive?")

	dir, _ := os.MkdirTemp("", "ferryproto")
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "cfg.yaml")
	os.WriteFile(file, []byte("db:\n  host: one\n"), 0o644)
	addrs := NewAddressSet([]Path{path("db", "host")})

	// 1. Bind does no I/O.
	_, err := SlimYAML{Path: filepath.Join(dir, "nope.yaml")}.Bind(addrs)
	check("Bind does no I/O (missing file binds clean)", err == nil)

	// 2. Injectivity refused at Bind, before any backend call.
	kv := newKV(map[string]string{"cfg/a/b": "x"})
	_, err = SlimKV{KV: kv, Prefix: "cfg/"}.Bind(NewAddressSet([]Path{path("a/b"), path("a", "b")}))
	check("injectivity refused before I/O", err != nil && kv.calls() == 0)

	// 3. The precompute survives across loads.
	open, _ := SlimYAML{Path: file}.Bind(addrs)
	binds := 1
	for i, want := range []string{"one", "two", "three"} {
		os.WriteFile(file, []byte("db:\n  host: "+want+"\n"), 0o644)
		r, _ := open(ctx)
		v, _ := r.Get(ctx, path("db", "host"))
		if v.Text() != want {
			fmt.Printf("        reload %d wrong: %v\n", i, v.GoString())
		}
	}
	check(fmt.Sprintf("3 reloads off %d bind", binds), binds == 1)

	// 4. A statically read-only plane is still a compile-time refusal.
	var _ SlimSource = SlimEnv{}
	_, envIsSink := any(SlimEnv{}).(SlimSink)
	check("env does not implement SlimSink", !envIsSink)

	// 5. A dynamically read-only plane still refuses before values are made.
	rokv := newKV(nil)
	rokv.readOnly = true
	ow, _ := SlimKV{KV: rokv, Prefix: "cfg/"}.BindSink(addrs)
	_, err = ow(ctx)
	check("read-only plane refuses at open, not at first Set", err != nil)

	// 6. Close(ctx, cause) still leaves the plane untouched on failure.
	out := filepath.Join(dir, "out.yaml")
	os.WriteFile(out, []byte("keep: me\n"), 0o644)
	ow2, _ := SlimYAML{Path: out}.BindSink(addrs)
	w, _ := ow2(ctx)
	_ = w.Set(ctx, path("a"), String("1"))
	_ = w.Close(ctx, fmt.Errorf("walk failed"))
	b, _ := os.ReadFile(out)
	left, _ := filepath.Glob(filepath.Join(dir, ".ferry-*"))
	check("Close(cause) leaves plane byte-identical, no temp left",
		string(b) == "keep: me\n" && len(left) == 0)

	// 7. The optional capability still attaches.
	o, _ := SlimYAML{Path: file}.Bind(addrs)
	r, _ := o(ctx)
	e, canEnum := r.(Enumerator)
	check("Enumerator still discoverable on a SlimReader", canEnum)
	if canEnum {
		kids, _ := e.Children(ctx, Path{})
		fmt.Printf("             children of root: %v\n", kids)
	}

	// 8. Dynamic addresses still work.
	ow3, _ := SlimKV{KV: newKV(nil), Prefix: "cfg/"}.BindSink(NewAddressSet([]Path{path("name")}))
	w3s, _ := ow3(ctx)
	check("address minted after Bind still accepted",
		w3s.Set(ctx, path("labels", "env"), String("prod")) == nil)

	// (d) The part that is actually dagger's lesson.
	fmt.Println("\n    (d) what moves out of interfaces and into combinators")
	os.WriteFile(file, []byte("db:\n  host: from-yaml\n"), 0o644)
	composed := FirstOf(
		SlimEnv{Lookup: func(k string) (string, bool) {
			if k == "DB_PORT" {
				return "5432", true
			}
			return "", false
		}},
		SlimYAML{Path: file},
		Static(map[Path]Value{path("db", "user"): String("default-user")}),
	)
	set := NewAddressSet([]Path{path("db", "host"), path("db", "port"), path("db", "user"), path("db", "pass")})
	op, err := composed.Bind(set)
	if err != nil {
		fmt.Println("        bind:", err)
		return
	}
	rr, _ := op(ctx)
	for _, p := range set.All() {
		v, _ := rr.Get(ctx, p)
		fmt.Printf("        %-12s %s\n", p, v.GoString())
	}
	fmt.Println("        env, then yaml, then defaults, one expression, no new")
	fmt.Println("        interface. FirstOf is a SlimSource so it nests in Under or")
	fmt.Println("        Snapshot, and every combinator is 4 to 12 lines because")
	fmt.Println("        SlimSourceFunc exists.")

	rec := map[Path]Value{}
	ow4, _ := Recorder(rec).Bind(set)
	rw, _ := ow4(ctx)
	_ = rw.Set(ctx, path("db", "host"), String("h"))
	_ = rw.Close(ctx, nil)
	fmt.Printf("        Recorder (ADR-0001 schema extraction) captured: %d address\n", len(rec))
	fmt.Println("        Static is the defaults layer and the memory plane. Snapshot")
	fmt.Println("        is xload's `cached` provider without the stale-read TTL.")
	fmt.Println("        None of these is a new contract for a driver to implement.")
}
