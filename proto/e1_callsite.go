package main

// E1: the entry point, at a call site, before anything else.
//
// #19's ADR was drafted, reviewed and sent back because it argued from
// measurements and never showed the API a consumer meets. For the ticket that
// OWNS the entry point that mistake would be fatal, so this probe is first and
// every candidate is compiled and run rather than written out.
//
// The four candidates:
//
//   A  Load(ctx, &v, src)               xload's shape
//   B  Load[T](ctx, src) (T, error)     the research's recommendation
//   C  Load[T](ctx, seed T, src) (T, error)
//   D  a Schema[T] value the caller holds
//
// The measurements that separate them are ADR-0006's reload leak and ADR-0006's
// seeded value, not ergonomics.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

type E1DB struct {
	Host string `ferry:"host"`
	Port int    `ferry:"port,default=5432"`
}

type E1Config struct {
	Name string   `ferry:"name"`
	DB   E1DB     `ferry:"db"`
	Tags []string `ferry:"tags"`
}

// --- candidate A: xload's shape, written for real ---------------------------

func e1LoadA(ctx context.Context, dst any, src FSource) error {
	rv := reflect.ValueOf(dst)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("ferry: ErrNotPointer")
	}
	if rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("ferry: ErrNotStruct")
	}
	o := defaultOpts()
	s, err := schemaFor(rv.Elem().Type(), o)
	if err != nil {
		return err
	}
	open, err := src.Bind(NewAddressSet(s.addrs))
	if err != nil {
		return err
	}
	rd, err := open(ctx)
	if err != nil {
		return err
	}
	w := &walker{dir: loadDir(rd, ctx, o), sch: serial, ctx: ctx}
	_, err = w.walk(s.root, rv.Elem(), Path{})
	return err
}

// --- candidate D: a Schema value the caller holds ---------------------------

type Schema[T any] struct {
	s *schema
	o opts
}

func New[T any](options ...Option) (*Schema[T], error) {
	o := defaultOpts()
	for _, op := range options {
		op.apply(&o)
	}
	s, err := schemaFor(reflect.TypeFor[T](), o)
	if err != nil {
		return nil, err
	}
	return &Schema[T]{s: s, o: o}, nil
}

func (sc *Schema[T]) Load(ctx context.Context, src FSource) (T, error) {
	var zero T
	open, err := src.Bind(NewAddressSet(sc.s.addrs))
	if err != nil {
		return zero, err
	}
	rd, err := open(ctx)
	if err != nil {
		return zero, err
	}
	out := zero
	w := &walker{dir: loadDir(rd, ctx, sc.o), sch: serial, ctx: ctx}
	_, err = w.walk(sc.s.root, reflect.ValueOf(&out).Elem(), Path{})
	return out, err
}

func runE1() {
	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "e1")
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "app.yaml")
	write := func(body string) {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			panic(err)
		}
	}
	write("name: svc\ndb:\n  host: db1\n  port: 5432\ntags:\n  - a\n  - b\n")
	src := FYAMLSource{Path: p}

	fmt.Println("--- E1a: the four candidates, at a call site, all four run ---")

	// A
	var cfgA E1Config
	errA := e1LoadA(ctx, &cfgA, src)
	fmt.Printf("  A  var cfg E1Config; err := ferry.Load(ctx, &cfg, src)\n     -> %+v err=%v\n", cfgA, errA)

	// B
	cfgB, errB := Load[E1Config](ctx, src)
	fmt.Printf("  B  cfg, err := ferry.Load[E1Config](ctx, src)\n     -> %+v err=%v\n", cfgB, errB)

	// C
	cfgC, errC := LoadOver(ctx, E1Config{}, src)
	fmt.Printf("  C  cfg, err := ferry.LoadOver(ctx, E1Config{}, src)\n     -> %+v err=%v\n", cfgC, errC)

	// D
	scD, _ := New[E1Config]()
	cfgD, errD := scD.Load(ctx, src)
	fmt.Printf("  D  sc, _ := ferry.New[E1Config](); cfg, err := sc.Load(ctx, src)\n     -> %+v err=%v\n", cfgD, errD)

	fmt.Println("\n--- E1b: what each does with ADR-0006's reload, with /db/host deleted between loads ---")
	fmt.Println("    ADR-0006: an in-place reload leaks the previous load's value for every")
	fmt.Println("    address the plane has since lost, under EVERY absence rule it considered.")
	write("name: svc\ndb:\n  port: 5432\ntags:\n  - a\n  - b\n")

	// A, in place, into the value the first load populated: the leak.
	errA2 := e1LoadA(ctx, &cfgA, src)
	fmt.Printf("  A  ferry.Load(ctx, &cfg, src) into the SAME cfg   -> DB.Host=%q err=%v\n", cfgA.DB.Host, errA2)

	// B: there is no destination to leak into.
	cfgB2, _ := Load[E1Config](ctx, src)
	fmt.Printf("  B  cfg, err = ferry.Load[E1Config](ctx, src)      -> DB.Host=%q\n", cfgB2.DB.Host)

	// C: the carry-over is expressible and it is the caller writing cfg.
	cfgC2, _ := LoadOver(ctx, cfgC, src)
	fmt.Printf("  C  cfg, err = ferry.LoadOver(ctx, cfg, src)       -> DB.Host=%q  (the caller asked)\n", cfgC2.DB.Host)
	cfgC3, _ := LoadOver(ctx, E1Config{}, src)
	fmt.Printf("     cfg2, err := ferry.LoadOver(ctx, E1Config{}, src) -> DB.Host=%q\n", cfgC3.DB.Host)

	cfgD2, _ := scD.Load(ctx, src)
	fmt.Printf("  D  cfg, err = sc.Load(ctx, src)                   -> DB.Host=%q\n", cfgD2.DB.Host)

	fmt.Println("\n    A is the only shape whose ORDINARY call site is the leak. B, C and D")
	fmt.Println("    return a new value, so the leak needs the caller to write it out loud.")

	fmt.Println("\n--- E1c: ADR-0006's seeded value, which is the other half of the same rule ---")
	fmt.Println("    ADR-0006: declared defaults for leaves, SEEDED VALUES for composites and")
	fmt.Println("    for anything a tag cannot spell. A composite default does not compile.")
	write("name: svc\ndb:\n  host: db1\n")
	seed := E1Config{Tags: []string{"seeded"}}
	sB, _ := Load[E1Config](ctx, src)
	fmt.Printf("  B  Load[T] has nowhere to put a seed              -> Tags=%v\n", sB.Tags)
	sC, _ := LoadOver(ctx, seed, src)
	fmt.Printf("  C  LoadOver(ctx, seed, src)                       -> Tags=%v\n", sC.Tags)
	fmt.Println("    So B alone drops an ADR-0006 capability, and C is B plus the seed.")
	fmt.Println("    B is expressible as C with the zero value, which is how both ship.")

	fmt.Println("\n--- E1d: what the type parameter actually deletes ---")
	var notPtr E1Config
	fmt.Printf("  A  Load(ctx, cfg, src)  (forgot the &)   -> %v\n", e1LoadA(ctx, notPtr, src))
	fmt.Printf("  A  Load(ctx, &n, src)   n is an int      -> %v\n", e1LoadA(ctx, new(int), src))
	fmt.Println("  B/C/D: the first is a build error, so ErrNotPointer does not exist.")
	_, errRoot := Load[int](ctx, src)
	fmt.Printf("  B  Load[int](ctx, src)                   -> %v\n", errRoot)
	fmt.Println("     ErrNotStruct SURVIVES: Go has no constraint meaning \"any struct\".")
	fmt.Println("     Verified by compiling: `interface{ ~struct{} }` matches only the EMPTY")
	fmt.Println("     struct, and there is no wildcard form. See e1_notstruct.go.")

	fmt.Println("\n--- E1e: Validate, from a test, with no value and no plane ---")
	type E1Bad struct {
		Host string `ferry:"host,requird"`
		Port int    `ferry:"port,default=abc"`
		Nope string
	}
	fmt.Printf("  Validate[E1Config]() -> %v\n", Validate[E1Config]())
	fmt.Printf("  Validate[E1Bad]()    ->\n%v\n", indent(Validate[E1Bad]()))
}

func indent(err error) string {
	if err == nil {
		return "    <nil>"
	}
	out := ""
	for _, l := range strings.Split(err.Error(), "\n") {
		out += "    " + l + "\n"
	}
	return out[:len(out)-1]
}
