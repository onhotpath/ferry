package ferry

import (
	"context"
	"reflect"
)

// PROTOTYPE for #137 and #134. Not a proposal in its published form: the names,
// the refusals and the doc comments are sketched only far enough to cost the
// shape out.
//
// The whole of the non-generic entry point is that runDump and runLoad already
// take a reflect.Value. Dump[T] and LoadOver[T] are the thin generic shells that
// pin the static type; underneath them there is no type parameter anywhere on
// the walk, so exposing the root as a reflect.Value adds no machinery at all.

// DumpValue is Dump with the root supplied as a reflect.Value, so a caller
// holding only a runtime type can walk it.
//
// v is the root value itself and not a pointer to it, which is the one thing
// this signature makes the caller's problem: Dump[T] goes through a pointer so
// that an interface T is seen as the interface rather than as whatever dynamic
// type it holds, and a reflect.Value has already made that choice.
func DumpValue(ctx context.Context, v reflect.Value, sink Sink, opts ...Option) error {
	if !v.IsValid() {
		return newError(momentWalk, ErrValue, Path{}, "the root is the zero reflect.Value, which names no type")
	}

	return runDump(ctx, v, sink, opts)
}

// LoadValue is Load with the destination supplied as an addressable
// reflect.Value, which is what the walk writes into.
func LoadValue(ctx context.Context, dst reflect.Value, src Source, opts ...Option) error {
	if !dst.IsValid() {
		return newError(momentWalk, ErrValue, Path{}, "the destination is the zero reflect.Value, which names no type")
	}

	if !dst.CanSet() {
		return newError(momentWalk, ErrValue, Path{},
			"the destination is not settable: pass reflect.New(t).Elem() or an addressable field")
	}

	return runLoad(ctx, dst, src, opts)
}
