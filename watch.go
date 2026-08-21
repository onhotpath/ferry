package ferry

import (
	"context"
	"errors"
	"fmt"
	"iter"
)

// Notifier is the mechanism a watchable source hands over: one method, called
// by the stream, answering with the registration that waits for the next
// change.
//
// ferry never probes a source for one. A source hands one over from
// [WatchableSource.Watching], so a source cannot claim a watch its options
// never configured.
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
	// True is a change, including one that landed before Wait was called.
	//
	// An error is the watch being lost, and it is the answer that keeps a
	// process from holding stale configuration with nothing to tell it so. It
	// ends the stream under [ErrWatchLost], with this error reachable
	// underneath.
	//
	// False with a nil error is the watch ending without a reason, and it ends
	// the stream too. Either answer is read as the cancellation when ctx is
	// done, so a mechanism need not decide whether a cancelled wait is a
	// failure.
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
// driver outside this module ships a watchable plane and how a process that
// reloads on a signal writes its own.
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
	// never comes. It is not the whole of watchability: what the operating
	// system has an opinion about surfaces when a stream places its first
	// registration.
	//
	// Answering nil with a nil error is a driver defect and is refused as one,
	// because a source that says it can be watched and hands over nothing is
	// the silent failure this method exists to prevent.
	Watching() (Notifier, error)
}

// ErrWatchLost reports a watch the plane could not keep. An error carrying it
// carries [ErrPlane] too, and the plane's own reason stays reachable underneath
// both.
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
// the watch it was built for is refused here, before any load. That refusal is
// the driver's own, wrapped in [ErrPlane], so the driver's watch sentinel is
// what [errors.Is] answers for. The caller knows which driver they constructed,
// so there is no cross-driver sentinel here to match instead.
//
// It reaches no plane and starts no goroutine. The watch opens when a stream
// does, under that stream's own context.
func BindWatched[T any](src WatchableSource, opts ...Option) (*WatchedBinding[T], error) {
	if src == nil {
		return nil, fmt.Errorf("%w: no watchable source was named", ErrPlane)
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
//
// The two refusals are different classes on purpose (ADR-0020). A source that
// says why it cannot be watched has made a plane refusal, and its own reason is
// what the caller matches. A source that says nothing and hands over nothing
// has broken the contract, which is [ErrDriver]'s bucket and the same one a nil
// open falls into.
func watching(src WatchableSource) (Notifier, error) {
	n, err := src.Watching()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPlane, err)
	}

	if n == nil {
		return nil, fmt.Errorf("%w: Watching answered no mechanism and no reason", ErrDriver)
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
// The two results are one stream: range seq once, and call errf after that
// range has exited. Ranging it twice, or reading errf while a range is still
// running, races. A second stream is a second call to Watch.
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
		return err
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
		return s.ending(err)
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
		return s.ending(err)
	}

	if !ok {
		return s.ending(nil)
	}

	// The next registration is placed before the reload runs, so a change
	// landing inside the reload is the next change rather than a lost one
	// (ADR-0020).
	if err := a.rearm(s.ctx, s.n); err != nil {
		return s.ending(err)
	}

	return nil
}

// ending names how the stream stopped, given whatever the mechanism said.
//
// A context that is done outranks the mechanism's own complaint, and it has to
// (ADR-0020): a driver may spell a cancelled wait or a registration it could
// not place on a dead context as an error of its own, and the three endings the
// caller matches on - the reload's error, the cancellation, and a lost watch -
// stay distinguishable only if core decides that here rather than passing the
// driver's spelling through.
//
// A nil cause is the plane ending its own watch without a reason, which is
// still an ending the caller must be able to see.
func (s *watchStream[T]) ending(cause error) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}

	if cause == nil {
		return fmt.Errorf("%w: %w: the plane ended this watch", ErrPlane, ErrWatchLost)
	}

	return fmt.Errorf("%w: %w: %w", ErrPlane, ErrWatchLost, cause)
}
