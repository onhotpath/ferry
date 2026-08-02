package main

// B4: where the plane comes from, once a binding is held.
//
// Two mechanisms are runnable: the plane in the context, and the plane as a
// typed parameter of the load. The second is the ticket's option (c) with the
// type parameter moved off Source and onto ferry's own entry point, which is a
// shape the ticket did not enumerate and which ADR-0004's objection was not
// written against. Both are written here.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// --- the typed-plane shape ---------------------------------------------------

// PlaneSource is an OPTIONAL interface, in ADR-0004's own style: discovered by
// assertion, implemented by the drivers that need it, invisible to the rest.
// It is generic, and Source is not, so no ordinary driver's signature changes
// and no ordinary driver writes P = struct{}.
type PlaneSource[P any] interface {
	BindPlane(*AddressSet) (func(context.Context, P) (FReader, error), error)
}

type PlaneBinding[T, P any] struct {
	s    *schema
	o    opts
	open func(context.Context, P) (FReader, error)
}

// BindPlaneTo is where the assertion happens. P is bound HERE, at the caller's
// call site, which is what makes asserting to a generic interface legal at all.
func BindPlaneTo[T, P any](src FSource, options ...Option) (*PlaneBinding[T, P], error) {
	o := defaultOpts()
	for _, op := range options {
		op.apply(&o)
	}
	s, err := schemaFor(typeOf[T](), o)
	if err != nil {
		return nil, err
	}
	ps, ok := src.(PlaneSource[P])
	if !ok {
		return nil, fmt.Errorf("ferry: %T does not supply a %T plane", src, *new(P))
	}
	open, err := ps.BindPlane(s.as)
	if err != nil {
		return nil, err
	}
	return &PlaneBinding[T, P]{s: s, o: o, open: open}, nil
}

func (b *PlaneBinding[T, P]) Load(ctx context.Context, plane P) (T, error) {
	var out T
	rd, err := b.open(ctx, plane)
	if err != nil {
		return out, err
	}
	w := &walker{dir: loadDir(rd, ctx, b.o), sch: serial, ctx: ctx}
	_, err = w.walk(b.s.root, valueOfPtr(&out), Path{})
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}

// BQueryPlane is the same query driver again, implementing both.
type BQueryPlane struct{ Sep string }

func (s BQueryPlane) sep() string {
	if s.Sep == "" {
		return "."
	}
	return s.Sep
}

func (s BQueryPlane) Bind(a *AddressSet) (FOpenFunc, error) {
	return BQueryCtx{Sep: s.Sep}.Bind(a)
}

func (s BQueryPlane) BindPlane(a *AddressSet) (func(context.Context, url.Values) (FReader, error), error) {
	keys, err := NewKeys(a, "query", bQueryKey(s.sep()))
	if err != nil {
		return nil, err
	}
	return func(_ context.Context, v url.Values) (FReader, error) {
		return bQueryReader{keys.Key, v}, nil
	}, nil
}

// --- the probe ---------------------------------------------------------------

func runB4() {
	ctx := context.Background()
	vals := b1Values()

	fmt.Println("--- B4a: the two call sites, both run ---")
	ctxB, err := BindTo[B1Filter](BQueryCtx{})
	if err != nil {
		fmt.Println("  bind:", err)
		return
	}
	c1, e1 := ctxB.Load(BQueryContext(ctx, vals))
	fmt.Printf("  (a) b.Load(query.WithValues(ctx, r.URL.Query()))\n      -> %+v err=%v\n", c1, e1)
	c0, e0 := Load[B1Filter](BQueryContext(ctx, vals), BQueryCtx{})
	fmt.Printf("  (a) one-shot, the SAME driver shape and no binding held:\n")
	fmt.Printf("      ferry.Load[Filter](query.WithValues(ctx, r.URL.Query()), query.Source{})\n      -> %+v err=%v\n", c0, e0)
	fmt.Println("      One driver shape serves both, so the driver has one public way to")
	fmt.Println("      be given its plane, which is 5.14's first item closed on the")
	fmt.Println("      driver rather than only on core.")

	pl, err := BindPlaneTo[B1Filter, url.Values](BQueryPlane{})
	if err != nil {
		fmt.Println("  bindplane:", err)
		return
	}
	c2, e2 := pl.Load(ctx, vals)
	fmt.Printf("  (c) b.Load(ctx, r.URL.Query())\n      -> %+v err=%v\n", c2, e2)

	fmt.Println("\n--- B4b: what each does when the caller forgets ---")
	c3, e3 := ctxB.Load(ctx)
	fmt.Printf("  (a) no values in the context -> %+v\n      err=%v\n", c3, e3)
	fmt.Printf("      errors.Is(err, ErrNoPlane) = %v, and it lands at OPEN, which is\n", errors.Is(e3, ErrNoPlane))
	fmt.Println("      where ADR-0004 already puts \"the plane is not reachable\".")
	fmt.Println("  (c) there is nothing to forget: the parameter is url.Values and")
	fmt.Println("      omitting it does not compile.")

	fmt.Println("\n--- B4c: and what each does when a driver has no plane to take ---")
	_, e4 := BindPlaneTo[B1YAML, url.Values](FYAMLSource{Path: "x.yaml"})
	fmt.Printf("  (c) BindPlaneTo[T, url.Values](yaml.Source{}) -> %v\n", e4)
	fmt.Println("      A run-time refusal, at bind, from a type assertion. The optional")
	fmt.Println("      interface is discovered the way ADR-0004 discovers Enumerator.")

	fmt.Println("\n--- B4d: composition, which is where they separate ---")
	dir, yp := b1WriteYAML()
	defer os.RemoveAll(dir)
	_ = filepath.Dir(yp)
	fmt.Println("    ADR-0004: FirstOf binds every child before any child does I/O, and")
	fmt.Println("    each combinator is itself a Source, so they nest. Precedence over a")
	fmt.Println("    per-request plane is the case the query driver exists for: query")
	fmt.Println("    parameters beating a file.")
	combined := BFirstOf(BQueryCtx{}, FYAMLSource{Path: yp})
	cb, err := BindTo[B4Conf](combined)
	if err != nil {
		fmt.Println("  bind:", err)
		return
	}
	for _, q := range []url.Values{{}, {"name": {"from-query"}}} {
		got, err := cb.Load(BQueryContext(ctx, q))
		fmt.Printf("  (a) FirstOf(query, yaml), query=%-24v -> %+v err=%v\n", q, got, err)
	}
	fmt.Println("    The ctx reaches the query child because FirstOf's open already")
	fmt.Println("    passes it to every child's open. FirstOf needed no change at all.")
	fmt.Println()
	fmt.Println("    (c) has no such call. FirstOf is a Source, so it has no BindPlane,")
	fmt.Println("    and giving it one means a FirstOfPlane[P] whose children must all")
	fmt.Println("    agree on P - which is exactly the objection ADR-0004 recorded")
	fmt.Println("    against putting the parameter on Source, surviving the move of the")
	fmt.Println("    parameter onto the entry point.")
	fmt.Println("    Here the two children are url.Values and a file, so P is url.Values")
	fmt.Println("    for one and nothing for the other, and there is no P to write.")
}

type B4Conf struct {
	Name string `ferry:"name"`
}

// BFirstOf is ADR-0004's combinator, unchanged, so the probe above is not
// measuring a special case written for it.
func BFirstOf(srcs ...FSource) FSource { return bFirstOf(srcs) }

type bFirstOf []FSource

func (f bFirstOf) Bind(a *AddressSet) (FOpenFunc, error) {
	opens := make([]FOpenFunc, len(f))
	for i, s := range f {
		o, err := s.Bind(a)
		if err != nil {
			return nil, fmt.Errorf("source %d: %w", i, err)
		}
		opens[i] = o
	}
	return func(ctx context.Context) (FReader, error) {
		rs := make([]FReader, 0, len(opens))
		for i, o := range opens {
			r, err := o(ctx)
			if err != nil {
				return nil, fmt.Errorf("source %d: %w", i, err)
			}
			rs = append(rs, r)
		}
		return bFirstReader(rs), nil
	}, nil
}

type bFirstReader []FReader

func (rs bFirstReader) Get(ctx context.Context, p Path) (Value, error) {
	for _, r := range rs {
		v, err := r.Get(ctx, p)
		if err != nil {
			return Absent, err
		}
		if v.Kind() != VAbsent {
			return v, nil
		}
	}
	return Absent, nil
}
