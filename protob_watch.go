//go:build protob

package ferry

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"time"
)

// Variant B of the typed watch prototype: a watchable source is a distinct
// type, so handing a source that cannot be watched to the watching bind is a
// compile error rather than a refusal of any kind.
//
// This is ADR-0017's pattern read on the source seam. A driver mints the proof
// of watchability with [Watchable], the way a caller mints a codec with
// [StringValue], and everything downstream takes the minted value rather than
// re-deciding what it means.

// Notifier is the mechanism half of a watchable source: one method, called by
// the stream, answering with the registration that waits for the next change.
//
// It is not asserted anywhere. A driver hands one to [Watchable] and core reads
// it from there, so there is no capability in the assertion set and no source
// that claims a watch its options never configured.
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

// ErrNotWatchable reports a source that cannot be watched. It wraps [ErrPlane].
var ErrNotWatchable = errors.New("this source cannot be watched")

// ErrWatchLost reports a watch the plane could not keep. It wraps [ErrPlane],
// and the plane's own reason stays reachable underneath it.
var ErrWatchLost = errors.New("the watch was lost")

// WatchableSource is a [Source] that can be watched, and it is a type rather
// than an interface so that the proof is carried in the value.
//
// A driver mints one with [Watchable] and refuses one it cannot honour with
// [Unwatchable]. A caller passes it to [BindWatched], and passing anything else
// does not compile.
//
// It is a [Source] as well, so a binding that never watches takes it unchanged
// and code written against [Source] accepts it with no conversion.
//
// The zero value is a source that is not there, and [BindWatched] refuses it.
type WatchableSource struct {
	src Source
	n   Notifier

	// err is a refusal the driver found while building this value and had
	// nowhere to report, since a constructor's one result is the source itself.
	// [BindWatched] opens it.
	err error
}

// Watchable declares that src can be watched through n, and it is what a
// driver's watching constructor returns.
//
//	func NewWatched(opts ...Option) ferry.WatchableSource {
//	    s := New(opts...)
//	    if err := s.watchable(); err != nil {
//	        return ferry.Unwatchable(err)
//	    }
//	    return ferry.Watchable(s, s)
//	}
//
// The two arguments are usually one value. They are two so that a driver can
// keep the mechanism in a type of its own, and so that a source type is never
// obliged to carry a method it only sometimes means.
func Watchable(src Source, n Notifier) WatchableSource {
	return WatchableSource{src: src, n: n}
}

// Unwatchable carries a refusal a driver found while building a source it was
// asked to watch: no file named, a mechanism this build has no access to.
//
// The refusal surfaces at [BindWatched], wrapping [ErrNotWatchable], which is
// the first call that has somewhere to report it. The source it returns loads
// nothing, because there was never a watch to bind.
func Unwatchable(err error) WatchableSource {
	return WatchableSource{err: err}
}

// Bind is [Source.Bind], so a WatchableSource is a source like any other and a
// caller who wants only to load through it needs no conversion.
func (w WatchableSource) Bind(addrs *AddressSet) (OpenFunc, error) {
	if w.src == nil {
		return nil, fmt.Errorf("%w: %w", ErrPlane, ErrNotWatchable)
	}

	return w.src.Bind(addrs)
}

// WatchedBinding is a watchable source bound to a compiled type: it loads like
// a [Binding] and it streams, and nothing about it can fail for want of a watch.
//
// [BindWatched] produced it, which is the only way there is.
type WatchedBinding[T any] struct {
	b *Binding[T]
	n Notifier
}

// BindWatched compiles T, hands the source the addresses T names, and answers
// with a binding that both loads and streams.
//
//	wb, err := ferry.BindWatched[Config](env.NewWatched(env.DotEnv(".env")))
//	seq, errf := wb.Watch(ctx)
//
// It is [Bind] over a source that carries its own proof of watchability, so
// every refusal [Bind] makes it makes too, plus the one the source carried: a
// driver that could not honour the watch is refused here, before any load,
// wrapping [ErrNotWatchable].
//
// It reaches no plane and starts no goroutine. The watch opens when a stream
// does, under that stream's own context.
func BindWatched[T any](src WatchableSource, opts ...Option) (*WatchedBinding[T], error) {
	if src.err != nil {
		return nil, fmt.Errorf("%w: %w: %w", ErrPlane, ErrNotWatchable, src.err)
	}

	if src.src == nil || src.n == nil {
		return nil, fmt.Errorf("%w: %w", ErrPlane, ErrNotWatchable)
	}

	b, err := Bind[T](src.src, opts...)
	if err != nil {
		return nil, err
	}

	return &WatchedBinding[T]{b: b, n: src.n}, nil
}

// Binding is the load half on its own, for handing to code that loads and does
// not watch.
func (wb *WatchedBinding[T]) Binding() *Binding[T] { return wb.b }

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
func (wb *WatchedBinding[T]) Watch(ctx context.Context, opts ...WatchOption) (iter.Seq[T], func() error) {
	cfg, err := resolveWatch(opts)
	if err != nil {
		return func(func(T) bool) {}, func() error { return err }
	}

	s := &watchStream[T]{b: wb.b, n: wb.n, ctx: ctx, cfg: cfg}

	return func(yield func(T) bool) { s.err = s.run(yield) }, func() error { return s.err }
}

// WatchOption is a setting [WatchedBinding.Watch] takes. The set is closed,
// because the interface's one method is unexported.
type WatchOption interface {
	applyWatch(*watchConfig) error
}

type watchOptionFunc func(*watchConfig) error

func (f watchOptionFunc) applyWatch(c *watchConfig) error { return f(c) }

type watchConfig struct {
	debounce    time.Duration
	debounceSet bool
}

func resolveWatch(opts []WatchOption) (watchConfig, error) {
	var cfg watchConfig

	for _, o := range opts {
		if err := o.applyWatch(&cfg); err != nil {
			return watchConfig{}, err
		}
	}

	return cfg, nil
}

// Debounce holds a reload back until the plane has been quiet for d.
//
// A burst of changes - an editor's several events for one save, or two layered
// planes each announcing the same deployment - becomes one reload rather than
// one per announcement. The registration stays live for the whole window, so
// nothing is lost by waiting.
//
// The zero duration is no debounce at all, which is the default.
func Debounce(d time.Duration) WatchOption {
	return watchOptionFunc(func(c *watchConfig) error {
		if c.debounceSet {
			return fmt.Errorf("%w: ferry.Debounce was supplied twice", ErrSchema)
		}

		if d < 0 {
			return fmt.Errorf("%w: ferry.Debounce was given a negative duration", ErrSchema)
		}

		c.debounce, c.debounceSet = d, true

		return nil
	})
}

// watchStream is one range: the loop, and the error it ended on.
type watchStream[T any] struct {
	b   *Binding[T]
	n   Notifier
	ctx context.Context
	cfg watchConfig
	err error
}

// run is the loop, returning the error that ended it: nil when the caller
// stopped ranging.
//
// The order is load-bearing. The registration is armed before the load, so a
// change that lands while the load is running is reported by the wait that
// follows it rather than lost (ADR-0020, #272).
func (s *watchStream[T]) run(yield func(T) bool) error {
	cur, err := s.n.Notify(s.ctx)
	if err != nil {
		return lostWatch(err)
	}

	defer func() {
		if cur != nil {
			_ = cur.Close()
		}
	}()

	for {
		v, err := s.b.Load(s.ctx)
		if err != nil {
			return err
		}

		if !yield(v) {
			return nil
		}

		next, err := s.await(cur)

		cur = next

		if err != nil {
			return err
		}
	}
}

// await waits for one change on cur and answers with the registration that is
// live for the reload about to run.
func (s *watchStream[T]) await(cur Change) (Change, error) {
	ok, err := cur.Wait(s.ctx)
	if err != nil {
		_ = cur.Close()

		return nil, lostWatch(err)
	}

	if !ok {
		_ = cur.Close()

		return nil, s.ended()
	}

	return s.settle(cur)
}

// ended names the quiet ending. A plane that ends its own watch for a reason it
// will not name is still an ending the caller must be able to see.
func (s *watchStream[T]) ended() error {
	if err := s.ctx.Err(); err != nil {
		return err
	}

	return fmt.Errorf("%w: %w: the plane ended this watch", ErrPlane, ErrWatchLost)
}

// settle re-arms and, under a debounce, swallows the rest of the burst.
func (s *watchStream[T]) settle(cur Change) (Change, error) {
	deadline := time.Now().Add(s.cfg.debounce)

	for {
		next, err := s.n.Notify(s.ctx)
		_ = cur.Close()

		if err != nil {
			return nil, lostWatch(err)
		}

		cur = next

		if s.cfg.debounce <= 0 || !time.Now().Before(deadline) {
			return cur, nil
		}

		more, err := s.drain(cur, deadline)
		if err != nil {
			return nil, err
		}

		if !more {
			return cur, nil
		}
	}
}

// drain waits on cur until the debounce window closes, reporting whether
// another change arrived inside it.
func (s *watchStream[T]) drain(cur Change, deadline time.Time) (bool, error) {
	ctx, cancel := context.WithDeadline(s.ctx, deadline)
	defer cancel()

	ok, err := cur.Wait(ctx)
	if err != nil {
		_ = cur.Close()

		return false, lostWatch(err)
	}

	if !ok && s.ctx.Err() != nil {
		_ = cur.Close()

		return false, s.ctx.Err()
	}

	return ok, nil
}

// lostWatch states the class and keeps the plane's own reason reachable.
func lostWatch(cause error) error {
	return fmt.Errorf("%w: %w: %w", ErrPlane, ErrWatchLost, cause)
}
