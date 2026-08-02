package main

// B9: three worked programs, and the check that the binding is additive.
//
// The claim this probe exists to test is stronger than "the binding is
// optional". It is:
//
//	Deleting Bind, BindSink, Binding[T] and SinkBinding[T] from ferry leaves
//	every one of these programs compiling and producing the same values.
//
// Prose cannot establish that. B9c compiles the no-binding programs against a
// generated ferry package that DOES NOT HAVE the binding API, so the compiler
// is what answers.

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// --- program 1: no binding. Startup configuration. --------------------------

type B9Conf struct {
	Name string `ferry:"name"`
	DB   struct {
		Host string `ferry:"host,required"`
		Port int    `ferry:"port,default=5432"`
	} `ferry:"db"`
}

// b9Startup reads a config file once and writes it back once. There is nothing
// to hold a binding for: each phase happens exactly one time, so a binding
// would be a value constructed and immediately discarded.
func b9Startup(ctx context.Context, path string) (B9Conf, error) {
	cfg, err := Load[B9Conf](ctx, FYAMLSource{Path: path})
	if err != nil {
		return cfg, err
	}
	return cfg, Dump(ctx, cfg, FYAMLSink{Path: path})
}

// --- program 2: a Load binding. One per request. -----------------------------

// b9Handler is the case this ticket was filed for. The binding is built once;
// the plane arrives per request.
func b9Handler(b *Binding[B1Filter], q url.Values) (B1Filter, error) {
	return b.Load(BQueryContext(context.Background(), q))
}

// b9HandlerNoBinding is the same handler written against a ferry with no
// binding API at all. Same driver value, same context constructor, same result.
func b9HandlerNoBinding(q url.Values) (B1Filter, error) {
	return Load[B1Filter](BQueryContext(context.Background(), q), BQueryCtx{})
}

// --- program 3: a Dump binding. One per tick. --------------------------------

type B9Stats struct {
	Uptime  int            `ferry:"uptime"`
	Queues  map[string]int `ferry:"queues"`
	Version string         `ferry:"version"`
}

// b9Export writes a stats struct into a KV plane on every tick. The sink's
// configuration never changes and the value's shape does, which is the case
// B8 showed a sink binding serves.
func b9Export(ctx context.Context, b *SinkBinding[B9Stats], s B9Stats) error {
	return b.Dump(ctx, s)
}

func b9ExportNoBinding(ctx context.Context, sink FSink, s B9Stats) error {
	return bDumpOneShot(ctx, s, sink)
}

// --- the probe ---------------------------------------------------------------

func runB9() {
	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "b9")
	defer os.RemoveAll(dir)

	fmt.Println("--- B9a: program 1, no binding, because there is nothing to reuse ---")
	p := filepath.Join(dir, "app.yaml")
	os.WriteFile(p, []byte("name: svc\ndb:\n  host: db1\n"), 0o644)
	cfg, err := b9Startup(ctx, p)
	back, _ := os.ReadFile(p)
	fmt.Printf("    Load then Dump, once each -> %+v err=%v\n", cfg, err)
	fmt.Printf("    the file now reads: %s", strings.ReplaceAll(string(back), "\n", " | "))
	fmt.Println("\n    A binding here would be constructed, used once and dropped, which is")
	fmt.Println("    what ferry.Load and ferry.Dump already do internally.")

	fmt.Println("\n--- B9b: programs 2 and 3, each written both ways ---")
	q := url.Values{"q": {"widgets"}, "page": {"7"}}
	lb, err := Bind[B1Filter](BQueryCtx{})
	if err != nil {
		fmt.Println("    bind:", err)
		return
	}
	withB, e1 := b9Handler(lb, q)
	withoutB, e2 := b9HandlerNoBinding(q)
	fmt.Printf("    handler, binding held    -> %+v err=%v\n", withB, e1)
	fmt.Printf("    handler, no binding      -> %+v err=%v\n", withoutB, e2)
	fmt.Printf("    identical                -> %v\n", withB == withoutB)

	stats := B9Stats{Uptime: 42, Version: "1.2.3", Queues: map[string]int{"in": 3, "out": 1}}
	s1, s2 := NewStore(), NewStore()
	sb, err := BindSink[B9Stats](BKVSink{Store: s1, PerOpen: true})
	if err != nil {
		fmt.Println("    bindsink:", err)
		return
	}
	e3 := b9Export(ctx, sb, stats)
	e4 := b9ExportNoBinding(ctx, BKVSink{Store: s2, PerOpen: true}, stats)
	fmt.Printf("    exporter, binding held   -> %v err=%v\n", s1.Keys(), e3)
	fmt.Printf("    exporter, no binding     -> %v err=%v\n", s2.Keys(), e4)
	fmt.Printf("    identical                -> %v\n", fmt.Sprint(s1.Keys()) == fmt.Sprint(s2.Keys()))

	fmt.Println("\n--- B9c: and the additive claim, checked by the compiler ---")
	fmt.Println("    The three no-binding programs are compiled below against a ferry")
	fmt.Println("    package that exports Load, LoadOver, Dump and Compile and NOTHING")
	fmt.Println("    else - no Bind, no BindSink, no Binding, no SinkBinding.")
	out, err := b9CompileWithout(dir)
	if err != nil {
		fmt.Printf("    go build FAILED: %v\n%s\n", err, out)
	} else {
		fmt.Println("    go build: ok, so the no-binding programs reference no part of")
		fmt.Println("    this ADR's surface. The binding is additive by construction.")
	}
	fmt.Println("\n    And the negative control, the same module with one line added that")
	fmt.Println("    does use the binding:")
	out2, err2 := b9CompileWithBinding(dir)
	if err2 == nil {
		fmt.Println("    go build: ok - WRONG, the control should not compile")
	} else {
		fmt.Printf("    go build: %s\n", b9FirstError(out2))
		fmt.Println("    so the check above is a real one and not a build that ignores it.")
	}
}

// b9CompileWithout writes a module whose ferry package has the one-shot verbs
// only, plus the three programs, and builds it.
func b9CompileWithout(dir string) (string, error) {
	return b9Build(filepath.Join(dir, "without"), "")
}

func b9CompileWithBinding(dir string) (string, error) {
	return b9Build(filepath.Join(dir, "control"), `
func useBinding() { b, _ := ferry.Bind[Filter](query.Source{}); _ = b }
`)
}

func b9Build(root, extra string) (string, error) {
	if err := os.MkdirAll(filepath.Join(root, "ferry"), 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(root, "query"), 0o755); err != nil {
		return "", err
	}
	write := func(rel, body string) error {
		return os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644)
	}
	if err := write("go.mod", "module b9\n\ngo 1.27\n"); err != nil {
		return "", err
	}
	if err := write("ferry/ferry.go", b9FerryStub); err != nil {
		return "", err
	}
	if err := write("query/query.go", b9QueryStub); err != nil {
		return "", err
	}
	if err := write("main.go", b9Programs+extra); err != nil {
		return "", err
	}
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=go1.27rc2", "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func b9FirstError(out string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, ":") && !strings.HasPrefix(l, "#") {
			return strings.TrimSpace(l)
		}
	}
	return strings.TrimSpace(out)
}

// b9FerryStub is ferry WITHOUT this ADR: ADR-0010's four functions and
// ADR-0004's four interfaces, and not one identifier more.
const b9FerryStub = `package ferry

import "context"

type Path struct{ s string }
type Value struct{ k int }
type AddressSet struct{ p []Path }
type Option interface{ ferryOption() }

type Source interface{ Bind(*AddressSet) (OpenFunc, error) }
type Sink interface{ Bind(*AddressSet) (OpenWriterFunc, error) }
type OpenFunc func(context.Context) (Reader, error)
type OpenWriterFunc func(context.Context) (Writer, error)
type Reader interface{ Get(context.Context, Path) (Value, error) }
type Writer interface{ Set(context.Context, Path, Value) error }

func Load[T any](ctx context.Context, src Source, opts ...Option) (T, error) {
	var t T
	return t, nil
}
func LoadOver[T any](ctx context.Context, seed T, src Source, opts ...Option) (T, error) {
	return seed, nil
}
func Dump[T any](ctx context.Context, v T, sink Sink, opts ...Option) error { return nil }
func Compile[T any](opts ...Option) error                                   { return nil }
`

const b9QueryStub = `package query

import (
	"context"
	"net/url"

	"b9/ferry"
)

type key struct{}

func WithValues(ctx context.Context, v url.Values) context.Context {
	return context.WithValue(ctx, key{}, v)
}

type Source struct{ Sep string }

func (Source) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) { return nil, nil }

type Sink struct{ Sep string }

func (Sink) Bind(*ferry.AddressSet) (ferry.OpenWriterFunc, error) { return nil, nil }
`

// b9Programs is the three no-binding programs, written the way the ADR shows
// them rather than adapted to the stub.
const b9Programs = `package main

import (
	"context"
	"net/http"

	"b9/ferry"
	"b9/query"
)

type Conf struct {
	Name string ` + "`ferry:\"name\"`" + `
}

type Filter struct {
	Q    string ` + "`ferry:\"q\"`" + `
	Page int    ` + "`ferry:\"page\"`" + `
}

type Stats struct {
	Uptime int            ` + "`ferry:\"uptime\"`" + `
	Queues map[string]int ` + "`ferry:\"queues\"`" + `
}

// program 1: startup, no binding, unchanged by this ADR.
func startup(ctx context.Context, src ferry.Source, sink ferry.Sink) (Conf, error) {
	cfg, err := ferry.Load[Conf](ctx, src)
	if err != nil {
		return cfg, err
	}
	return cfg, ferry.Dump(ctx, cfg, sink)
}

// program 2: the handler, without a binding.
func handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := ferry.Load[Filter](query.WithValues(r.Context(), r.URL.Query()), query.Source{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = f
	})
}

// program 3: the exporter, without a binding.
func export(ctx context.Context, sink ferry.Sink, s Stats) error {
	return ferry.Dump(ctx, s, sink)
}

func main() {
	_ = startup
	_ = handler
	_ = export
	_ = ferry.Compile[Conf]
}
`
