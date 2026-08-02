package main

// The caller-held binding, written as a real one so it can be run and
// measured rather than described.
//
// ADR-0010 removed one of this ticket's two motivations: the schema cache
// already replaces the 47370 ns compile with a 34 ns lookup, so what a
// caller-held value can still save is the BIND alone. This is that value.
//
// Note what it holds and does not hold. It holds the compiled schema, the
// resolved options and the driver's OpenFunc. It holds no Registry: ADR-0009
// measured that a per-call registry gives an unbounded, non-evictable schema
// cache, so the registry stays where it is, inside opts, pointing at a
// long-lived value the caller already owns.

import (
	"context"
	"reflect"
)

type Binding[T any] struct {
	s    *schema
	o    opts
	open FOpenFunc
}

// BindTo is Load's first two phases, stopped at the phase boundary ADR-0004
// already drew. Every load through the returned value skips both.
func Bind[T any](src FSource, options ...Option) (*Binding[T], error) {
	o := defaultOpts()
	for _, op := range options {
		op.apply(&o)
	}
	s, err := schemaFor(reflect.TypeFor[T](), o)
	if err != nil {
		return nil, err
	}
	open, err := src.Bind(s.as)
	if err != nil {
		return nil, err
	}
	return &Binding[T]{s: s, o: o, open: open}, nil
}

func (b *Binding[T]) Load(ctx context.Context) (T, error) {
	var seed T
	return b.LoadOver(ctx, seed)
}

func (b *Binding[T]) LoadOver(ctx context.Context, seed T) (T, error) {
	rd, err := b.open(ctx)
	if err != nil {
		return seed, err
	}
	if rel, ok := rd.(FReleaser); ok {
		defer rel.Close()
	}
	out := seed
	rv := reflect.ValueOf(&out).Elem()
	w := &walker{dir: loadDir(rd, ctx, b.o), sch: serial, ctx: ctx}
	if _, err := w.walk(b.s.root, rv, Path{}); err != nil {
		return seed, err
	}
	return out, nil
}

// BindTo is kept as an alias so the earlier probes still read, and so the ADR
// can show that the package-level Bind and the driver-facing Source.Bind
// coexist: they are the same phase, named once.
func BindTo[T any](src FSource, options ...Option) (*Binding[T], error) {
	return Bind[T](src, options...)
}
