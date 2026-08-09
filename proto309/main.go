package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/env"
	ferryhttp "github.com/onhotpath/ferry/driver/http"
	"github.com/onhotpath/ferry/driver/kv"
	fyaml "github.com/onhotpath/ferry/driver/yaml"
)

var tmp string

func main() {
	d, err := os.MkdirTemp("", "proto309")
	if err != nil {
		panic(err)
	}

	tmp = d
	defer os.RemoveAll(tmp)

	// One mode per process: the schema cache is keyed by type, so flipping the
	// mode in-process serves the first mode's compiled schema back.
	mode := ""
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	ferry.RootSentinel = mode

	fmt.Printf("################ MODE: sentinel=%q ################\n", mode)

	if mode == "decl" {
		ferry.RootSentinel = ""

		coreShape()
		driverShape()

		return
	}

	runAll()
}

func line(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")

	return s
}

// guard turns a panic into a recorded observation so the matrix keeps running.
func guard(what string) {
	if r := recover(); r != nil {
		fmt.Printf("  %-28s PANIC %v\n", what, r)
	}
}

func report(what string, err error, extra string) {
	if err != nil {
		fmt.Printf("  %-28s ERR  %s\n", what, line(err.Error()))
	} else {
		fmt.Printf("  %-28s ok   %s\n", what, extra)
	}
}

// ---- env ----

func envLoad[T any](label string, environ []string) {
	defer guard("env  Load")

	src := env.New(env.Environ(func() []string { return environ }))

	got, err := ferry.Load[T](context.Background(), src)
	report("env  Load", err, fmt.Sprintf("%#v", got))

	_ = label
}

// ---- http ----

func httpLoad[T any](vals url.Values) {
	defer guard("http Load(query)")

	src := ferryhttp.NewQuerySource()
	ctx := ferryhttp.WithQuery(context.Background(), vals)

	got, err := ferry.Load[T](ctx, src)
	report("http Load(query)", err, fmt.Sprintf("%#v", got))
}

// ---- yaml ----

func yamlDump[T any](name string, v T) {
	defer guard("yaml Dump")

	p := filepath.Join(tmp, name+".yaml")
	_ = os.Remove(p)

	err := ferry.Dump(context.Background(), v, fyaml.NewSink(p))

	b, rerr := os.ReadFile(p)
	if rerr != nil {
		b = []byte("<no file>")
	}

	report("yaml Dump", err, fmt.Sprintf("file=%q", string(b)))
}

func yamlLoad[T any](name, doc string) {
	defer guard("yaml Load")

	p := filepath.Join(tmp, name+".in.yaml")
	if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
		panic(err)
	}

	got, err := ferry.Load[T](context.Background(), fyaml.NewSource(p))
	report("yaml Load", err, fmt.Sprintf("%#v", got))
}

// ---- kv ----

func kvDump[T any](v T) {
	defer guard("kv   Dump")

	store := newMemKV()

	sink, err := kv.NewSink(store)
	if err != nil {
		report("kv   Dump", err, "")

		return
	}

	err = ferry.Dump(context.Background(), v, sink)
	report("kv   Dump", err, "keys="+dumpKeys(store))
}

func dumpKeys(store *memKV) string {
	snap := store.snapshot()
	keys := make([]string, 0, len(snap))

	for k := range snap {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%q=%q", k, string(snap[k])))
	}

	if len(parts) == 0 {
		return "{} (ZERO KEYS)"
	}

	return "{" + strings.Join(parts, ", ") + "}"
}

// rootVarLoad is the driver-option alternative to a universal sentinel.
func rootVarLoad[T any](name string, environ []string) {
	label := fmt.Sprintf("env  Load(RootVar=%q)", name)

	defer guard(label)

	opts := []env.Option{env.Environ(func() []string { return environ })}
	if name != "" {
		opts = append(opts, env.RootVar(name))
	}

	got, err := ferry.Load[T](context.Background(), env.New(opts...))
	report(label, err, fmt.Sprintf("%#v", got))
}

// yamlRoundTrip dumps to a file and loads back out of it.
func yamlRoundTrip[T any](name string, v T) {
	defer guard("yaml roundtrip")

	p := filepath.Join(tmp, name+".rt.yaml")
	_ = os.Remove(p)

	if err := ferry.Dump(context.Background(), v, fyaml.NewSink(p)); err != nil {
		report("yaml roundtrip(dump)", err, "")

		return
	}

	b, _ := os.ReadFile(p)

	got, err := ferry.Load[T](context.Background(), fyaml.NewSource(p))
	report("yaml roundtrip", err, fmt.Sprintf("file=%q -> %#v", string(b), got))
}

// realEnvLoad reads through the actual process environment, which is where the
// fold of "~" onto "_" meets the variable every shell already sets.
func realEnvLoad[T any]() {
	defer guard("env  Load(real environ)")

	got, err := ferry.Load[T](context.Background(), env.New())
	report("env  Load(real environ)", err, fmt.Sprintf("%#v (os.Getenv(_)=%q)", got, os.Getenv("_")))
}

// kvDumpWith dumps under core root Options, which is the write side of a
// load-side declaration.
func kvDumpWith[T any](v T, opt ferry.Option) {
	label := fmt.Sprintf("kv   Dump(v=%v, opt)", v)

	defer guard(label)

	store := newMemKV()

	sink, err := kv.NewSink(store)
	if err != nil {
		report(label, err, "")

		return
	}

	err = ferry.Dump(context.Background(), v, sink, opt)
	report(label, err, "keys="+dumpKeys(store))
}

// kvDumpPrefixed writes through a prefixed sink, where the empty path renders
// to the prefix key itself.
func kvDumpPrefixed[T any](v T) {
	defer guard("kv   Dump(prefix=app)")

	store := newMemKV()
	_ = store.Put(context.Background(), "app/other", []byte("kept"))

	sink, err := kv.NewSink(store, kv.WithPrefix("app"))
	if err != nil {
		report("kv   Dump(prefix=app)", err, "")

		return
	}

	err = ferry.Dump(context.Background(), v, sink)
	report("kv   Dump(prefix=app)", err, "keys="+dumpKeys(store))
}

// yamlDumpOver dumps onto a file that already holds a mapping.
func yamlDumpOver[T any](name string, v T, existing string) {
	defer guard("yaml Dump(over doc)")

	p := filepath.Join(tmp, name+".over.yaml")
	if err := os.WriteFile(p, []byte(existing), 0o600); err != nil {
		panic(err)
	}

	err := ferry.Dump(context.Background(), v, fyaml.NewSink(p))

	b, _ := os.ReadFile(p)

	report("yaml Dump(over doc)", err, fmt.Sprintf("file=%q", string(b)))
}

func kvLoad[T any](seed map[string]string) {
	defer guard("kv   Load")

	store := newMemKV()
	for k, v := range seed {
		_ = store.Put(context.Background(), k, []byte(v))
	}

	src, err := kv.NewSource(store)
	if err != nil {
		report("kv   Load", err, "")

		return
	}

	got, err := ferry.Load[T](context.Background(), src)
	report("kv   Load", err, fmt.Sprintf("%#v", got))
}
