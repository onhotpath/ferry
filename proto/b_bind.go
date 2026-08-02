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

func (b *Binding[T]) LoadOver(ctx context.Context, seed T) (res T, err error) {
	rd, oerr := b.open(ctx)
	if oerr != nil {
		// ADR-0011's moment ordering exists for exactly this: an Open failure
		// precedes the walk errors it caused, and here there are none, because
		// the walk never ran.
		return seed, fromDriver(mOpen, Path{}, false, oerr)
	}
	// #41 D14 on the Load side. ADR-0004 runs Close unconditionally; ADR-0011
	// then makes a Close failure an ELEMENT of the aggregate, because
	// discarding it is silently ignoring something and ADR-0001 forbids that.
	// It has no location and explains nothing, which is why the sort key's
	// first term is the moment: it sorts AFTER the walk errors it did not cause
	// rather than at the head of a report it had nothing to do with.
	if rel, ok := rd.(FReleaser); ok {
		defer func() {
			if e := rel.Close(); e != nil {
				err = join(err, fromDriver(mClose, Path{}, false, e))
				// And once the load has failed, ferry yields no value it built.
				res = seed
			}
		}()
	}
	out := seed
	rv := reflect.ValueOf(&out).Elem()
	w := &walker{dir: loadDir(rd, ctx, b.o), sch: b.o.sch, ctx: ctx}
	if _, werr := w.walk(b.s.root, rv, Path{}); werr != nil {
		return seed, werr
	}
	return out, nil
}

// BindTo is kept as an alias so the earlier probes still read, and so the ADR
// can show that the package-level Bind and the driver-facing Source.Bind
// coexist: they are the same phase, named once.
func BindTo[T any](src FSource, options ...Option) (*Binding[T], error) {
	return Bind[T](src, options...)
}
