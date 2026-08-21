//go:build protoe

package ferry

import (
	"context"
	"errors"
	"fmt"
	"iter"
)

// Variant E of the typed watch prototype: a watchable source is an interface,
// implementing it is the claim, and the one call that reads it is the bind.
//
// It is variant B with the minting constructors removed. What B carried in a
// sealed struct - the mechanism, and a refusal the constructor found and could
// not report - E asks for at the bind instead, through one method that answers
// with both. The compile-time gate is unchanged, because a plain [Source] still
// does not satisfy [WatchableSource].

// Notifier is the mechanism a watchable source hands over: one method, called
// by the stream, answering with the registration that waits for the next
// change.
//
// It is not asserted anywhere. A source hands one to core from
// [WatchableSource.Watching], so there is no capability in the assertion set
// and no source that claims a watch its options never configured.
type Notifier interface {
	// Notify registers for the next change on this plane and answers with the
	// [Change] that waits for it.
	//
	// The registration is live when Notify returns, so a change between Notify
	// and [Change.Wait] is reported by that Wait rather than missed. ctx bounds
	// the whole watch.
	Notify(ctx context.Context) (Change, error)
}

// Change is one armed registration: one wait, and the release that follows it.
type Change interface {
	// Wait blocks until the change this registration was armed for happens,
	// until ctx is done, or until the watch cannot be kept.
	//
	// True is a change, including one that landed before Wait was called. False
	// with a nil error is the watch ending quietly. Any error is the watch being
	// lost, and it is the answer that keeps a process from holding stale
	// configuration with nothing to tell it so.
	Wait(ctx context.Context) (bool, error)

	// Close releases the registration, once, after the wait.
	Close() error
}

// WatchableSource is a [Source] that can be watched. Implementing it is the
// claim, and [BindWatched] takes one where [Bind] takes a [Source].
//
// A driver publishes one from the source that already holds the configuration,
// so the file to watch and the watch itself are named in one place:
//
//	src := env.New(env.DotEnv(".env")).Watched()
//
// It is an ordinary interface and anything may implement it, which is how a
// driver outside this module ships a watchable plane and how a caller composes
// two of them into one.
type WatchableSource interface {
	Source

	// Watching answers with the mechanism this source's changes arrive
	// through, and it is called once, by [BindWatched].
	//
	// It does no I/O, and the missing [context.Context] is how the type says
	// so - the same rule [Source.Bind] follows. What it may refuse is what the
	// source can see without touching the plane: no file named, a registry
	// handle that cannot report changes, a build without the mechanism. That
	// refusal is the instance-level half of watchability, which no type can
	// carry, and it lands at the bind rather than at the first change that
	// never comes.
	//
	// Answering nil with a nil error is refused as though it had refused,
	// because a source that says it can be watched and hands over nothing is
	// the silent failure this method exists to prevent.
	Watching() (Notifier, error)
}

// ErrNotWatchable reports a source that cannot be watched. It wraps [ErrPlane].
var ErrNotWatchable = errors.New("this source cannot be watched")

// ErrWatchLost reports a watch the plane could not keep. It wraps [ErrPlane],
// and the plane's own reason stays reachable underneath it.
var ErrWatchLost = errors.New("the watch was lost")

// WatchedBinding is a watchable source bound to a compiled type: it loads like
// a [Binding] and it streams, and nothing about it can fail for want of a watch.
//
// [BindWatched] produced it, which is the only way there is.
type WatchedBinding[T any] struct {
	b *Binding[T]
	n Notifier
}

// BindWatched compiles T, hands src the addresses T names, and answers with a
// binding that both loads and streams.
//
//	wb, err := ferry.BindWatched[Config](env.New(env.DotEnv(".env")).Watched())
//	seq, errf := wb.Watch(ctx)
//
// It is [Bind] over a source that can be watched, so every refusal [Bind] makes
// it makes too, plus the one the source itself makes: a source that cannot open
// the watch it was built for is refused here, before any load, wrapping
// [ErrNotWatchable] and carrying its own reason underneath.
//
// It reaches no plane and starts no goroutine. The watch opens when a stream
// does, under that stream's own context.
func BindWatched[T any](src WatchableSource, opts ...Option) (*WatchedBinding[T], error) {
	if src == nil {
		return nil, fmt.Errorf("%w: %w", ErrPlane, ErrNotWatchable)
	}

	n, err := watching(src)
	if err != nil {
		return nil, err
	}

	b, err := Bind[T](src, opts...)
	if err != nil {
		return nil, err
	}

	return &WatchedBinding[T]{b: b, n: n}, nil
}

// watching asks the source for its mechanism and refuses both ways it can fail
// to hand one over.
func watching(src WatchableSource) (Notifier, error) {
	n, err := src.Watching()
	if err != nil {
		return nil, fmt.Errorf("%w: %w: %w", ErrPlane, ErrNotWatchable, err)
	}

	if n == nil {
		return nil, fmt.Errorf("%w: %w: it reported no reason and handed over no mechanism",
			ErrPlane, ErrNotWatchable)
	}

	return n, nil
}

// Load builds a value of T, exactly as [Binding.Load] does.
func (wb *WatchedBinding[T]) Load(ctx context.Context) (T, error) { return wb.b.Load(ctx) }

// Watch streams a freshly loaded value of T: one when the range opens, and one
// for every change the plane reports afterwards.
//
//	seq, errf := wb.Watch(ctx)
//	for cfg := range seq {
//	    publish(cfg)
//	}
//	if err := errf(); err != nil {
//	    alert(err)
//	}
//
// The stream opens with a load, so there is no pre-load to write and no change
// that landed before the range started to lose. Every value is built by a load,
// so a value handed out earlier never changes underneath its holder.
//
// It ends on a failed reload, on a watch the plane could not keep, or on
// cancellation of ctx, and errf reports why, once, after the range exits.
// Breaking out of the range is a clean ending and errf reports nil.
//
// Each call opens a registration of its own, so two streams over one binding
// each see every change rather than sharing them out. The reload runs on the
// ranging goroutine and no goroutine is started here.
func (wb *WatchedBinding[T]) Watch(ctx context.Context) (seq iter.Seq[T], errf func() error) {
	s := &watchStream[T]{b: wb.b, n: wb.n, ctx: ctx}

	return func(yield func(T) bool) { s.err = s.run(yield) }, func() error { return s.err }
}

// watchStream is one range: the loop, and the error it ended on.
type watchStream[T any] struct {
	b   *Binding[T]
	n   Notifier
	ctx context.Context
	err error
}

// armed is the registration a stream currently holds: one at a time, and never
// none while the stream is running.
//
// The order inside rearm is load-bearing. The next registration is placed
// before the current one is released, so a change landing while a reload runs
// is reported by the wait that follows it rather than lost, which is what an
// armed-once mechanism cannot do for itself (ADR-0020, #272).
type armed struct{ c Change }

func (a *armed) rearm(ctx context.Context, n Notifier) error {
	next, err := n.Notify(ctx)
	if err != nil {
		return lostWatch(err)
	}

	a.close()
	a.c = next

	return nil
}

// close releases what is held, once. It is idempotent, so the deferred release
// at the top of a stream is correct on every exit, including the one where the
// registration was never placed at all.
func (a *armed) close() {
	if a.c == nil {
		return
	}

	_ = a.c.Close()
	a.c = nil
}

// run is the whole range: place the first registration, then loop.
func (s *watchStream[T]) run(yield func(T) bool) error {
	a := &armed{}
	defer a.close()

	if err := a.rearm(s.ctx, s.n); err != nil {
		return err
	}

	return s.loop(a, yield)
}

// loop is one turn per value: load, hand it over, wait for the next change.
//
// The registration is already armed when the load runs, which is what makes a
// change landing inside the load the next change rather than a lost one.
func (s *watchStream[T]) loop(a *armed, yield func(T) bool) error {
	for {
		v, err := s.b.Load(s.ctx)
		if err != nil {
			return err
		}

		if !yield(v) {
			return nil
		}

		if err := s.await(a); err != nil {
			return err
		}
	}
}

// await waits for one change, leaving a armed for the reload that follows it.
func (s *watchStream[T]) await(a *armed) error {
	ok, err := a.c.Wait(s.ctx)
	if err != nil {
		return lostWatch(err)
	}

	if !ok {
		return s.ended()
	}

	// The next registration is placed before the reload runs, so a change
	// landing inside the reload is the next change rather than a lost one.
	return a.rearm(s.ctx, s.n)
}

// ended names the quiet ending. A wait that answers false with no error is the
// context being done, and a plane that ends its own watch for a reason it will
// not name is still an ending the caller must be able to see.
func (s *watchStream[T]) ended() error {
	if err := s.ctx.Err(); err != nil {
		return err
	}

	return fmt.Errorf("%w: %w: the plane ended this watch", ErrPlane, ErrWatchLost)
}

// lostWatch states the class and keeps the plane's own reason reachable.
func lostWatch(cause error) error {
	return fmt.Errorf("%w: %w: %w", ErrPlane, ErrWatchLost, cause)
}
