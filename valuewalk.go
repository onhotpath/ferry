package ferry

import (
	"context"
	"reflect"

	"github.com/onhotpath/ferry/internal/valuewalk"
)

// valueWalk is the reflect.Value-rooted walk, published on an internal seam and
// on nothing else.
//
// Both methods are the generic entry points minus their generics: [Dump] and
// [LoadOver] already reduce to runDump and runLoad over a reflect.Value, so
// this adds no engine, no second compiler and no second walk. What it adds is a
// door, and the door is internal because who may walk a runtime type is a
// capability decision (#134) rather than a test harness's to take.
//
// It is a type with methods rather than two function variables so that the
// caller can recover both halves with one assertion, over an interface it
// declares in its own package against ferry's real types. That keeps the seam
// type-checked at both ends with no type erasure anywhere in the middle.
type valueWalk struct{}

// init installs the seam. It runs before any variable in any package that
// imports either this one or valuewalk, which is what makes the caller's
// assertion safe at package-variable scope.
func init() { valuewalk.Seam = valueWalk{} }

// DumpValue is [Dump] with the root handed over as a reflect.Value.
//
// v is the root value itself and not a pointer to it, matching what runDump
// receives from [Dump]: the pointer there exists to stop reflect.ValueOf
// unwrapping an interface T, and a caller holding a reflect.Value has already
// chosen its static type.
func (valueWalk) DumpValue(ctx context.Context, v reflect.Value, sink Sink, opts []Option) error {
	if !v.IsValid() {
		return newError(momentWalk, ErrValue, Path{},
			"the root is the zero reflect.Value, which names no type and so compiles to no schema")
	}

	return runDump(ctx, v, sink, opts)
}

// LoadValue is [LoadOver] with the destination handed over as a reflect.Value.
//
// dst is the addressable value the walk writes into, which is what LoadOver
// builds with reflect.ValueOf(&over).Elem(). It carries the seed in it, so the
// zero of dst is [Load] and a populated dst is LoadOver.
func (valueWalk) LoadValue(ctx context.Context, dst reflect.Value, src Source, opts []Option) error {
	switch {
	case !dst.IsValid():
		return newError(momentWalk, ErrValue, Path{},
			"the destination is the zero reflect.Value, which names no type and so compiles to no schema")
	case !dst.CanSet():
		return newError(momentWalk, ErrValue, Path{},
			"the destination is not addressable, so the walk has nowhere to write what it built")
	}

	return runLoad(ctx, dst, src, opts)
}
