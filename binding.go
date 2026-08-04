package ferry

import (
	"context"
	"reflect"
)

// This file is the whole of ADR-0012's caller-held binding, and it is a file of
// its own so that the ADR's additivity claim is one the compiler settles rather
// than one the prose asserts: delete it, and every program that never held a
// binding still builds, because the only callers outside it are the three
// bodies in entry.go.
//
// Nothing here is a second engine. Each of the five functions below is the
// corresponding half in entry.go with T in front of it, so the bind half and
// the load half exist exactly once and there is no second path to drift.

// Binding is a source bound to a compiled type: [Bind] produced it, and every
// [Binding.Load] through it skips the work that produced it.
//
// It is safe for use from many goroutines. What it holds is written once, when
// it is built, and never again, so a handler may keep one and load through it
// on every request. The driver's own open is called once per load, and a driver
// whose open is not safe for concurrent calls is not safe behind a Binding.
//
// It holds no resource and there is nothing to close. The plane is opened and
// released inside each load.
//
// It is not a view of the type. It answers no question about T: it has two
// methods, and both of them need a context and a plane.
type Binding[T any] struct{ b *bound }

// SinkBinding is a sink bound to a compiled type: [BindSink] produced it, and
// every [SinkBinding.Dump] through it skips the work that produced it.
//
// It is safe for use from many goroutines, on the same terms as [Binding], and
// it holds no resource.
//
// The addresses it handed the sink are the ones T names. An address a value
// mints - a map key, a sequence index - is minted at the write it belongs to
// and checked there, so one SinkBinding dumps values of different shapes: a map
// with two keys today and three tomorrow needs no second binding, and two dumps
// are never held to be injective against each other.
type SinkBinding[T any] struct{ b *boundSink }

// Bind compiles T and hands src the addresses T names, once, and returns a
// value to load through many times.
//
//	b, err := ferry.Bind[Config](env.New())   // once, at startup
//	...
//	cfg, err := b.Load(ctx)                   // as often as you like
//
// It is [Load]'s own first two steps, stopped where a driver's own Bind stops.
// So [Load] is this plus [Binding.Load] with the handle dropped, and anything
// true of one is true of the other.
//
// It reaches no plane. A source that cannot see its plane binds cleanly here
// and fails inside the load, which is where a plane that is not there is
// refused. What it does refuse is what a driver can see without touching a
// plane: an address the plane cannot name, and a key function that renders two
// addresses to one key.
//
// It takes the same [Option] values every other verb takes, and it retains the
// schema it compiled, so it freezes the [Registry] it resolved against. Range a
// failure with [Elements], and match a member against [ErrSchema] or
// [ErrPlane].
func Bind[T any](src Source, opts ...Option) (*Binding[T], error) {
	b, err := newBound(reflect.TypeFor[T](), src, opts)
	if err != nil {
		return nil, err
	}

	return &Binding[T]{b: b}, nil
}

// BindSink compiles T and hands sink the addresses T names, once, and returns a
// value to dump through many times.
//
//	b, err := ferry.BindSink[Stats](yaml.NewSink("stats.yaml"))
//	...
//	err = b.Dump(ctx, s)   // every tick
//
// It is [Dump]'s own first two steps, so [Dump] is this plus [SinkBinding.Dump]
// with the handle dropped.
//
// It reaches no plane, so a sink that is writable in principle and not right
// now binds cleanly here and refuses inside the dump. It takes the same
// [Option] values every other verb takes, and it retains the schema it
// compiled, so it freezes the [Registry] it resolved against.
//
// It has no value in hand, so a nil sink is refused here and a nil root pointer
// is refused at the dump.
func BindSink[T any](sink Sink, opts ...Option) (*SinkBinding[T], error) {
	b, err := newBoundSink(reflect.TypeFor[T](), sink, opts)
	if err != nil {
		return nil, err
	}

	return &SinkBinding[T]{b: b}, nil
}

// Load builds a value of T from the plane this binding was bound to.
//
// It is exactly what [Load] does, minus the compile and the bind, so every rule
// about absence, defaults, required and the failure report is the same. On
// failure it returns the zero value of T and never a partly built one.
//
// It is [Binding.LoadOver] with the zero seed.
func (b *Binding[T]) Load(ctx context.Context) (T, error) {
	var zero T

	return b.LoadOver(ctx, zero)
}

// LoadOver builds a value of T over a seed the caller supplies, from the plane
// this binding was bound to.
//
// It is exactly what [LoadOver] does, minus the compile and the bind. An
// address the plane does not have is absent, and absence does not write, so
// every field the plane is silent about keeps the value the seed gave it. On
// failure it returns the seed it was handed, unchanged.
func (b *Binding[T]) LoadOver(ctx context.Context, seed T) (T, error) {
	// The copy is the whole mechanism. The walk writes here and never to seed,
	// so there is no path by which a partial crosses the boundary (ADR-0011).
	// Through a pointer, so the walk sees T rather than whatever dynamic type an
	// interface T would hand reflect.ValueOf.
	over := seed

	if err := b.b.load(ctx, reflect.ValueOf(&over).Elem()); err != nil {
		return seed, err
	}

	return over, nil
}

// Dump writes v to the plane this binding was bound to.
//
// It is exactly what [Dump] does, minus the compile and the bind. The
// [Committer] and [Releaser] protocol, omitzero and the failure report are all
// the same, and every value is still encoded before any of them is written.
func (b *SinkBinding[T]) Dump(ctx context.Context, v T) error {
	// Through a pointer, for the reason LoadOver takes one.
	return b.b.dump(ctx, reflect.ValueOf(&v).Elem())
}
