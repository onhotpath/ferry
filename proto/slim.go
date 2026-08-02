package main

// The slim contract, built after reading dagger.
//
// dagger's shape is one small interface (Step[S], one method), a StepFunc
// adapter so it can be implemented in place, all the real complexity in
// concrete composable types that implement the same interface (If, Series,
// Continue, Result), and optional behaviour discovered by assertion rather
// than declared in the contract (middlewareSkipper, Unwrap).
//
// Applying that to ferry, the question is which of the six interfaces are
// genuinely contracts a driver satisfies, and which are just a phase of a
// pipeline that a function type expresses better.

import (
	"context"
	"errors"
	"fmt"
)

// ---------------------------------------------------------------------------
// Read side: two interfaces, two methods
// ---------------------------------------------------------------------------

// SlimSource is the whole read contract. One method.
type SlimSource interface {
	// Bind sees the address set and does no I/O. It returns the function
	// that will do the I/O, so the phase boundary survives without a
	// second interface to name it.
	Bind(*AddressSet) (SlimOpen, error)
}

// SlimSourceFunc is dagger's StepFunc adapter: it lets a source be written in
// place, which is what makes the combinators below one-liners.
type SlimSourceFunc func(*AddressSet) (SlimOpen, error)

func (f SlimSourceFunc) Bind(a *AddressSet) (SlimOpen, error) { return f(a) }

// SlimOpen was an interface with one method. It is a function because nothing
// ever needs to ask an opener a second question, and nothing ever needs to
// type-assert one for an optional capability.
type SlimOpen func(context.Context) (SlimReader, error)

// SlimReader stays an interface, and this is the line: a Reader is the one
// thing an optional capability attaches to. Enumerator is discovered by
// assertion on a Reader, dagger-style, and a func type cannot carry that.
type SlimReader interface {
	Get(context.Context, Path) (Value, error)
}

// SlimReaderFunc lets the common case, a reader with no optional capability,
// skip declaring a type.
type SlimReaderFunc func(context.Context, Path) (Value, error)

func (f SlimReaderFunc) Get(ctx context.Context, p Path) (Value, error) { return f(ctx, p) }

// ---------------------------------------------------------------------------
// Write side: two interfaces, three methods
// ---------------------------------------------------------------------------

type SlimSink interface {
	Bind(*AddressSet) (SlimOpenWriter, error)
}

type SlimSinkFunc func(*AddressSet) (SlimOpenWriter, error)

func (f SlimSinkFunc) Bind(a *AddressSet) (SlimOpenWriter, error) { return f(a) }

type SlimOpenWriter func(context.Context) (SlimWriter, error)

type SlimWriter interface {
	Set(context.Context, Path, Value) error
	// Close replaces Commit and Abort with one method. cause is nil to
	// commit and non-nil to abandon, so the call site is the ordinary Go
	// one - defer func() { err = w.Close(ctx, err) }() - and it is not
	// possible to forget the abort arm, which was the whole risk.
	Close(ctx context.Context, cause error) error
}

// ---------------------------------------------------------------------------
// The part dagger is actually about: combinators, not interfaces
// ---------------------------------------------------------------------------

// FirstOf is precedence. It is a SlimSource, so it nests inside any other
// combinator, and it is nine lines because SlimSourceFunc exists.
func FirstOf(srcs ...SlimSource) SlimSource {
	return SlimSourceFunc(func(a *AddressSet) (SlimOpen, error) {
		opens := make([]SlimOpen, len(srcs))
		for i, s := range srcs {
			o, err := s.Bind(a) // every child validated before any I/O
			if err != nil {
				return nil, fmt.Errorf("source %d: %w", i, err)
			}
			opens[i] = o
		}
		return func(ctx context.Context) (SlimReader, error) {
			rs := make([]SlimReader, len(opens))
			for i, o := range opens {
				r, err := o(ctx)
				if err != nil {
					return nil, fmt.Errorf("source %d: %w", i, err)
				}
				rs[i] = r
			}
			return SlimReaderFunc(func(ctx context.Context, p Path) (Value, error) {
				for _, r := range rs {
					v, err := r.Get(ctx, p)
					if err != nil {
						return Absent, err
					}
					if v.Present() {
						return v, nil
					}
				}
				return Absent, nil
			}), nil
		}, nil
	})
}

// Static is a source of constants. It is what a defaults layer is made of, and
// it is also the memory plane, and it is four lines.
func Static(m map[Path]Value) SlimSource {
	return SlimSourceFunc(func(*AddressSet) (SlimOpen, error) {
		return func(context.Context) (SlimReader, error) {
			return SlimReaderFunc(func(_ context.Context, p Path) (Value, error) { return m[p], nil }), nil
		}, nil
	})
}

// Under prefixes every address before handing it down. This is ADR-0003's
// "a prefix prepends a segment" as a combinator rather than as tag grammar.
func Under(prefix Path, src SlimSource) SlimSource {
	return SlimSourceFunc(func(a *AddressSet) (SlimOpen, error) {
		shifted := make([]Path, 0, a.Len())
		for _, p := range a.All() {
			shifted = append(shifted, prefix.Join(p))
		}
		open, err := src.Bind(NewAddressSet(shifted))
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context) (SlimReader, error) {
			r, err := open(ctx)
			if err != nil {
				return nil, err
			}
			return SlimReaderFunc(func(ctx context.Context, p Path) (Value, error) {
				return r.Get(ctx, prefix.Join(p))
			}), nil
		}, nil
	})
}

// Snapshot forces a lazy source to be read once per load rather than per
// address. This is xload's `cached` provider as eight lines of combinator
// instead of a sub-module, and unlike xload's it has no TTL, so it cannot
// serve a stale read across loads.
func Snapshot(src SlimSource) SlimSource {
	return SlimSourceFunc(func(a *AddressSet) (SlimOpen, error) {
		open, err := src.Bind(a)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context) (SlimReader, error) {
			r, err := open(ctx)
			if err != nil {
				return nil, err
			}
			snap := make(map[Path]Value, a.Len())
			for _, p := range a.All() {
				v, err := r.Get(ctx, p)
				if err != nil {
					return nil, err
				}
				snap[p] = v // Absent is kind zero, so a miss is a miss
			}
			return SlimReaderFunc(func(_ context.Context, p Path) (Value, error) { return snap[p], nil }), nil
		}, nil
	})
}

// Recorder is ADR-0001's schema-extraction pattern: dump into it and read off
// every mapped address. It is a SlimSink in seven lines.
func Recorder(into map[Path]Value) SlimSink {
	return SlimSinkFunc(func(*AddressSet) (SlimOpenWriter, error) {
		return func(context.Context) (SlimWriter, error) { return recWriter{into}, nil }, nil
	})
}

type recWriter struct{ m map[Path]Value }

func (w recWriter) Set(_ context.Context, p Path, v Value) error { w.m[p] = v; return nil }
func (w recWriter) Close(_ context.Context, cause error) error {
	if cause != nil {
		clear(w.m)
	}
	return nil
}

var errSlimReadOnly = errors.New("plane is read only")
