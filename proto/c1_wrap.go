package main

// C1 and C2: the mechanism, and the thing it silently breaks.
//
// xload's answer to cross-cutting concerns is a wrapped `Loader`, and
// ADR-0004 lists five combinators of the same shape without shipping any.
// ADR-0012 then SHIPPED the shape as its answer to ADR-0006's presence
// observation: "One `Source` wrapping another observes every boundary `Value`
// the load saw". So the mechanism is settled before #10 opens, and what #10
// has left to establish is what it costs.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// --- the naive wrappers, written the way anybody would write them -----------

type cNaiveSource struct {
	inner FSource
	seen  *[]string
}

func (s cNaiveSource) Bind(a *AddressSet) (FOpenFunc, error) {
	open, err := s.inner.Bind(a)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (FReader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}
		return cNaiveReader{r, s.seen}, nil
	}, nil
}

type cNaiveReader struct {
	inner FReader
	seen  *[]string
}

func (r cNaiveReader) Get(ctx context.Context, p Path) (Value, error) {
	v, err := r.inner.Get(ctx, p)
	*r.seen = append(*r.seen, p.String())
	return v, err
}

type cNaiveSink struct {
	inner FSink
	log   *[]string
}

func (s cNaiveSink) Bind(a *AddressSet) (FOpenWriterFunc, error) {
	open, err := s.inner.Bind(a)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (FWriter, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}
		return cNaiveWriter{w, s.log}, nil
	}, nil
}

type cNaiveWriter struct {
	inner FWriter
	log   *[]string
}

func (w cNaiveWriter) Set(ctx context.Context, p Path, v Value) error {
	*w.log = append(*w.log, kv(p, v))
	return w.inner.Set(ctx, p, v)
}

// --- the forwarding wrapper, which is what the naive one has to become ------

type cFwdSink struct {
	inner   FSink
	rewrite func(Path, Value) Value
	log     *[]string
}

func (s cFwdSink) Bind(a *AddressSet) (FOpenWriterFunc, error) {
	open, err := s.inner.Bind(a)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (FWriter, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}
		return cFwdWriter{w, s.rewrite, s.log}, nil
	}, nil
}

type cFwdWriter struct {
	inner   FWriter
	rewrite func(Path, Value) Value
	log     *[]string
}

func (w cFwdWriter) Set(ctx context.Context, p Path, v Value) error {
	if w.rewrite != nil {
		v = w.rewrite(p, v)
	}
	if w.log != nil {
		*w.log = append(*w.log, kv(p, v))
	}
	return w.inner.Set(ctx, p, v)
}

// Commit and Close forward CONDITIONALLY, which is the only correct shape and
// is four times as much code as the method it forwards.
func (w cFwdWriter) Commit(ctx context.Context) error {
	if c, ok := w.inner.(FCommitter); ok {
		return c.Commit(ctx)
	}
	return nil
}

func (w cFwdWriter) Close() error {
	if r, ok := w.inner.(FReleaser); ok {
		return r.Close()
	}
	return nil
}

func runC1() {
	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "c10")
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "app.yaml")
	_ = os.WriteFile(p, []byte("name: svc\ndb:\n  host: db1\n  password: hunter2\n"), 0o644)

	fmt.Println("(a) a wrapping Source sees every boundary Value, in the Load direction")
	var seen []string
	cfg, err := Load[CConf](ctx, cNaiveSource{FYAMLSource{Path: p}, &seen}, WithSched(tAggregating))
	fmt.Printf("    loaded: %+v  err=%v\n", cfg, errShortW(err))
	fmt.Printf("    the wrapper saw %d addresses: %v\n", len(seen), seen)
	fmt.Println("    That is ADR-0012's Observing wrapper, and it needs nothing from core.")

	fmt.Println("\n(b) a wrapping Sink sees every write, in the Dump direction")
	var log []string
	rec := newRecorder()
	if err := Dump(ctx, cfg, cNaiveSink{rec, &log}); err != nil {
		fmt.Println("    dump:", err)
	}
	fmt.Printf("    the wrapper saw %d writes: %v\n", len(log), log)
	fmt.Println("    So the mechanism is symmetric and it is the same shape in both")
	fmt.Println("    directions: a Source wrapping a Source, a Sink wrapping a Sink.")
	fmt.Println("    Neither needs a ferry-declared Middleware type, and ADR-0001's bucket")
	fmt.Println("    rule therefore keeps cross-cutting concerns Enabled: the mechanism is")
	fmt.Println("    two interfaces core already has.")
}

// CConf is #10's fixture: a config with a credential in it.
type CConf struct {
	Name string `ferry:"name"`
	DB   CDB    `ferry:"db"`
}

type CDB struct {
	Host     string `ferry:"host"`
	Password string `ferry:"password,default=hunter2"`
}
