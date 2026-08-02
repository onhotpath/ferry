package main

// P16: the final contract, with all four drivers rewritten against it and
// every property from P1 to P15 re-checked. This is the probe the ADR cites.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

func p16Final() {
	head("P16  the final contract, re-verified end to end")

	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "ferryproto")
	defer os.RemoveAll(dir)

	fmt.Println("    (a) surface")
	fmt.Printf("        %-16s %-24s %s\n", "required", "Source / Sink", "Bind          1 method")
	fmt.Printf("        %-16s %-24s %s\n", "", "Reader / Writer", "Get / Set     1 method")
	fmt.Printf("        %-16s %-24s %s\n", "optional", "Releaser", "Close() error (= io.Closer)")
	fmt.Printf("        %-16s %-24s %s\n", "", "Committer", "Commit(ctx) error")
	fmt.Printf("        %-16s %-24s %s\n", "", "Enumerator", "Children(ctx, Path)")
	fmt.Printf("        %-16s %-24s %s\n", "func types", "OpenFunc", "func(ctx) (Reader, error)")
	fmt.Printf("        %-16s %-24s %s\n", "", "OpenWriterFunc", "func(ctx) (Writer, error)")

	fmt.Println("\n    (b) driver cost, self-contained, against koanf's 31-246 / median ~120")
	total := 0
	for _, f := range []struct{ name, file, dirs string }{
		{"query params", "fdrv_query.go", "source"},
		{"env", "fdrv_env.go", "source"},
		{"kv (Consul-shaped)", "fdrv_kv.go", "source+sink"},
		{"yaml", "fdrv_yaml.go", "source+sink"},
	} {
		code, _ := countLines(f.file)
		total += code
		fmt.Printf("        %-20s %-12s %4d\n", f.name, f.dirs, code)
	}
	fmt.Printf("        %-33s %4d\n", "total", total)

	fmt.Println("\n    (c) optional interfaces actually implemented, per driver")
	fmt.Printf("        %-22s %-10s %-11s %s\n", "", "Releaser", "Committer", "Enumerator")
	for _, d := range []struct {
		name string
		v    any
	}{
		{"env reader", fEnvReader{}},
		{"query reader", fQueryReader{}},
		{"kv reader", &fKVReader{}},
		{"kv writer", &fKVWriter{}},
		{"yaml reader", fYAMLReader{}},
		{"yaml writer", &fYAMLWriter{}},
	} {
		_, rel := d.v.(FReleaser)
		_, com := d.v.(FCommitter)
		_, enu := d.v.(FEnumerator)
		fmt.Printf("        %-22s %-10v %-11v %v\n", d.name, rel, com, enu)
	}
	fmt.Println("        Nobody implements all three, and only the staging yaml")
	fmt.Println("        writer needs two. That is the case for making them optional.")

	// --- every property, re-checked --------------------------------------
	fmt.Println("\n    (d) properties")

	addrs := NewAddressSet([]Path{path("db", "host")})

	// P12: Bind does no I/O.
	_, err := FYAMLSource{Path: filepath.Join(dir, "nope.yaml")}.Bind(addrs)
	check("Bind does no I/O", err == nil)

	// P12/ADR-0003: injectivity refused before any backend call.
	kv := newKV(nil)
	_, err = FKVSource{KV: kv, Prefix: "cfg/"}.Bind(
		NewAddressSet([]Path{path("a/b"), path("a", "b")}))
	check("injectivity refused before I/O", err != nil && kv.calls() == 0)

	// P9: the precompute survives across loads.
	file := filepath.Join(dir, "cfg.yaml")
	os.WriteFile(file, []byte("db:\n  host: one\n"), 0o644)
	open, _ := FYAMLSource{Path: file}.Bind(addrs)
	ok := true
	for _, want := range []string{"one", "two", "three"} {
		os.WriteFile(file, []byte("db:\n  host: "+want+"\n"), 0o644)
		got, err := fLoad(ctx, open, addrs)
		if err != nil || got[path("db", "host")].Text() != want {
			ok = false
		}
	}
	check("3 reloads off 1 bind", ok)

	// P7: statically read-only is a compile-time refusal.
	var _ FSource = FEnv{}
	_, envIsSink := any(FEnv{}).(FSink)
	check("env does not implement Sink", !envIsSink)

	// P7: dynamically read-only refuses at open.
	rokv := newKV(nil)
	rokv.readOnly = true
	ow, _ := FKVSink{KV: rokv, Prefix: "cfg/"}.Bind(addrs)
	_, err = ow(ctx)
	check("read-only plane refuses at open", errors.Is(err, ErrReadOnly))

	// P2: three states.
	e := FEnv{Lookup: func(k string) (string, bool) {
		if k == "EMPTY" {
			return "", true
		}
		return "", false
	}}
	eo, _ := e.Bind(NewAddressSet([]Path{path("empty"), path("missing")}))
	er, _ := eo(ctx)
	ev, _ := er.Get(ctx, path("empty"))
	mv, _ := er.Get(ctx, path("missing"))
	check("absent, present-and-empty, present-and-set are distinct",
		ev.Kind() == VString && ev.Text() == "" && mv.Kind() == VAbsent)

	// P5: typed round trip through a real file.
	out := filepath.Join(dir, "rt.yaml")
	rtAddrs := NewAddressSet([]Path{path("port"), path("quoted"), path("on"), path("nul"), path("ratio")})
	want := map[Path]Value{
		path("port"): Int(8080), path("quoted"): String("8080"),
		path("on"): Bool(true), path("nul"): Null(), path("ratio"): Number("3.5"),
	}
	sow, _ := FYAMLSink{Path: out}.Bind(rtAddrs)
	if err := fDump(ctx, sow, want, rtAddrs); err != nil {
		fmt.Println("        dump:", err)
	}
	so, _ := FYAMLSource{Path: out}.Bind(rtAddrs)
	got, _ := fLoad(ctx, so, rtAddrs)
	exact := 0
	for p, w := range want {
		if got[p] == w {
			exact++
		}
	}
	check(fmt.Sprintf("typed round trip exact on %d/%d addresses", exact, len(want)), exact == len(want))

	// P15: Commit runs on success, Close always; failure leaves the plane alone.
	os.WriteFile(out, []byte("keep: me\n"), 0o644)
	fow, _ := FYAMLSink{Path: out}.Bind(addrs)
	_ = fDumpFailing(ctx, fow)
	b, _ := os.ReadFile(out)
	tmps, _ := filepath.Glob(filepath.Join(dir, ".ferry-*"))
	check("failed dump leaves plane byte-identical, no temp leaked",
		string(b) == "keep: me\n" && len(tmps) == 0)

	// P11: an address minted after Bind.
	mow, _ := FKVSink{KV: newKV(nil), Prefix: "cfg/"}.Bind(NewAddressSet([]Path{path("name")}))
	mw, _ := mow(ctx)
	check("address minted after Bind is accepted",
		mw.Set(ctx, path("labels", "env"), String("prod")) == nil)

	// P11 tier two: a minted address that collides is refused.
	ck, _ := NewKeys(NewAddressSet([]Path{path("limits", "http_port")}), "env", FEnv{}.key)
	_, _ = ck.Key(path("limits", "http_port"))
	_, mintErr := ck.Key(path("limits", "http.port"))
	check("minted address that collides is refused", mintErr != nil)

	// P8: enumeration where it exists, absent where it does not.
	yo, _ := FYAMLSource{Path: file}.Bind(addrs)
	yr, _ := yo(ctx)
	_, canEnum := yr.(FEnumerator)
	_, qCanEnum := any(fQueryReader{}).(FEnumerator)
	check("yaml enumerates, query does not", canEnum && !qCanEnum)

	// P3: batch versus lazy is one branch, same contract.
	data := map[string]string{"cfg/a": "1", "cfg/b": "2", "cfg/c": "3"}
	three := NewAddressSet([]Path{path("a"), path("b"), path("c")})
	lazyKV, batchKV := newKV(data), newKV(data)
	lo, _ := FKVSource{KV: lazyKV, Prefix: "cfg/", Lazy: true}.Bind(three)
	bo, _ := FKVSource{KV: batchKV, Prefix: "cfg/"}.Bind(three)
	_, _ = fLoad(ctx, lo, three)
	_, _ = fLoad(ctx, bo, three)
	fmt.Printf("        %-4s %s\n", "", fmt.Sprintf("lazy=%d calls  batch=%d calls  (3 addresses)",
		lazyKV.calls(), batchKV.calls()))
	check("batch collapses round trips with no extra interface",
		lazyKV.calls() == 3 && batchKV.calls() == 1)

	// Composition still needs no core surface.
	fmt.Println("\n    (e) composition, still an example rather than surface")
	os.WriteFile(file, []byte("db:\n  host: from-yaml\n"), 0o644)
	set := NewAddressSet([]Path{path("db", "host"), path("db", "port")})
	c := fFirstOf(
		FEnv{Lookup: func(k string) (string, bool) {
			if k == "DB_PORT" {
				return "5432", true
			}
			return "", false
		}},
		FYAMLSource{Path: file},
	)
	co, err := c.Bind(set)
	if err != nil {
		fmt.Println("        bind:", err)
		return
	}
	vals, _ := fLoad(ctx, co, set)
	for _, p := range set.All() {
		fmt.Printf("        %-12s %s\n", p, vals[p].GoString())
	}
}

func fDumpFailing(ctx context.Context, open FOpenWriterFunc) error {
	w, err := open(ctx)
	if err != nil {
		return err
	}
	if rel, ok := w.(FReleaser); ok {
		defer rel.Close()
	}
	_ = w.Set(ctx, path("a"), String("1"))
	return errors.New("field /b: invalid") // Commit never runs
}

// fFirstOf is precedence as a combinator: it is an FSource, so it nests.
type fFirstOfSource []FSource

func fFirstOf(s ...FSource) FSource { return fFirstOfSource(s) }

func (f fFirstOfSource) Bind(a *AddressSet) (FOpenFunc, error) {
	opens := make([]FOpenFunc, len(f))
	for i, s := range f {
		o, err := s.Bind(a) // every child validated before any I/O
		if err != nil {
			return nil, fmt.Errorf("source %d: %w", i, err)
		}
		opens[i] = o
	}
	return func(ctx context.Context) (FReader, error) {
		rs := make([]FReader, len(opens))
		for i, o := range opens {
			r, err := o(ctx)
			if err != nil {
				return nil, err
			}
			rs[i] = r
		}
		return fFirstOfReader(rs), nil
	}, nil
}

type fFirstOfReader []FReader

func (rs fFirstOfReader) Get(ctx context.Context, p Path) (Value, error) {
	for _, r := range rs {
		v, err := r.Get(ctx, p)
		if err != nil {
			return Absent, err
		}
		if v.Present() { // short-circuit, correct only because absence is observable
			return v, nil
		}
	}
	return Absent, nil
}

var _ = url.Values{}
