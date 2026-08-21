//go:build protoa

package ferry

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync"
	"time"
)

// Variant A of the typed watch prototype: the transition to watching is a
// method on the binding, and the capability is discovered at that call.
//
// The driver seam is winreg's Arm/Change shape promoted to core, because it is
// the only one of the three shipped mechanisms that cannot be modelled by a
// weaker one: a persistent queue can pretend to be armed once, and an armed-once
// registration cannot pretend to be a queue (ADR-0020, #272).
//
// Everything in this file is behind the protoa build tag, so the default build
// of the module is untouched.

// Notifier is implemented by a [Source] whose plane can report a change.
//
// It is optional and it is discovered by assertion, in the same idiom as
// [Prober] and [Releaser]. A source that is no Notifier is refused at
// [Binding.Watch], which is the one call that needs the capability.
//
// Registering and waiting are two calls rather than one. A watcher arms the
// next registration before it reloads, so a registration is live for the whole
// of the reload and a change landing inside it is the next change rather than a
// change lost for ever.
type Notifier interface {
	// Notify registers for the next change on this plane and answers with the
	// [Change] that waits for it.
	//
	// The registration is live when Notify returns, so a change between Notify
	// and [Change.Wait] is reported by that Wait rather than missed. ctx bounds
	// the whole watch: when it is done every registration made under it ends.
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

// ErrNotWatchable reports a source that cannot be watched.
//
// It wraps [ErrPlane] and it is what [Binding.Watch] returns for a source that
// implements no [Notifier].
var ErrNotWatchable = errors.New("this source cannot be watched")

// ErrWatchLost reports a watch the plane could not keep.
//
// It wraps [ErrPlane], and the plane's own reason stays reachable underneath it,
// so a stream that ends this way says why rather than going quiet.
var ErrWatchLost = errors.New("the watch was lost")

// ErrWatchInUse reports a second range over one [Watched].
var ErrWatchInUse = errors.New("this watch is already being ranged")

// WatchOption is a setting [Binding.Watch] takes. The set is closed, because
// the interface's one method is unexported.
type WatchOption interface {
	applyWatch(*watchConfig) error
}

type watchOptionFunc func(*watchConfig) error

func (f watchOptionFunc) applyWatch(c *watchConfig) error { return f(c) }

// watchConfig is the resolved WatchOption set one stream runs under.
type watchConfig struct {
	debounce    time.Duration
	debounceSet bool
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

// Watched is a binding that is being watched: a stream of freshly loaded values
// and the error that ended it.
//
// [Binding.Watch] produced it. Range [Watched.Values] and read [Watched.Err]
// after the range exits.
//
// It is single use. One Watched is one stream, and a second range over it is
// refused rather than sharing the changes out.
type Watched[T any] struct {
	b   *Binding[T]
	n   Notifier
	ctx context.Context
	cfg watchConfig

	mu     sync.Mutex
	ranged bool
	err    error
	errSet bool
}

// Watch opens a watch over the plane this binding was bound to.
//
//	w, err := b.Watch(ctx)
//	for cfg := range w.Values() {
//	    publish(cfg)
//	}
//	if err := w.Err(); err != nil {
//	    alert(err)
//	}
//
// It refuses a source that cannot be watched, with an error wrapping
// [ErrNotWatchable], and it refuses a [WatchOption] list that does not resolve.
// It reaches no plane and starts no goroutine: the registration is placed when
// the range starts, and ctx is the whole lifetime of both the registration and
// the stream.
func (b *Binding[T]) Watch(ctx context.Context, opts ...WatchOption) (*Watched[T], error) {
	n, ok := b.b.src.(Notifier)
	if !ok {
		return nil, fmt.Errorf("%w: %w", ErrPlane, ErrNotWatchable)
	}

	var cfg watchConfig

	for _, o := range opts {
		if err := o.applyWatch(&cfg); err != nil {
			return nil, err
		}
	}

	return &Watched[T]{b: b, n: n, ctx: ctx, cfg: cfg}, nil
}

// Values streams a freshly loaded value of T: one when the range opens, and one
// for every change the plane reports afterwards.
//
// The stream opens with a load, so there is no pre-load to write and no change
// that landed before the range started to lose. Every value is built by a load,
// so a value handed out earlier never changes underneath the goroutine holding
// it.
//
// It ends on a failed reload, on a watch the plane could not keep, or on
// cancellation of the context [Binding.Watch] was given, and [Watched.Err] says
// which. Breaking out of the range is a clean ending and reports nil.
//
// The reload runs on the ranging goroutine and no goroutine is started here.
func (w *Watched[T]) Values() iter.Seq[T] {
	return func(yield func(T) bool) {
		if !w.claim() {
			w.record(fmt.Errorf("%w: %w", ErrPlane, ErrWatchInUse))

			return
		}

		w.record(w.stream(yield))
	}
}

// Err reports what ended the stream, and nil where the caller stopped ranging.
// Reading it before the range has exited reports nothing useful.
func (w *Watched[T]) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.err
}

// claim makes the Watched single use: the first range takes it and no later one
// can.
func (w *Watched[T]) claim() bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.ranged {
		return false
	}

	w.ranged = true

	return true
}

// record keeps the first failure that was reported, so a refusal handed to a
// second range cannot overwrite what the real stream ended on.
//
// A clean ending records nothing, which is what lets the refusal a later range
// earns be the thing Err reports after a caller broke out of the first one.
func (w *Watched[T]) record(err error) {
	if err == nil {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.errSet {
		return
	}

	w.err, w.errSet = err, true
}

// stream is the loop, returning the error that ended it: nil when the caller
// stopped ranging.
//
// The order is load-bearing. The registration is armed before the load, so a
// change that lands while the load is running is reported by the wait that
// follows it rather than lost, which is winreg's armed-once mechanism made
// safe in core instead of in each driver (ADR-0020, #272).
func (w *Watched[T]) stream(yield func(T) bool) error {
	cur, err := w.n.Notify(w.ctx)
	if err != nil {
		return watchLost(err)
	}

	// await hands back nil where it closed what it was given, so the release
	// runs once on every exit and never twice.
	defer func() {
		if cur != nil {
			_ = cur.Close()
		}
	}()

	for {
		v, err := w.b.Load(w.ctx)
		if err != nil {
			return err
		}

		if !yield(v) {
			return nil
		}

		next, err := w.await(cur)

		cur = next

		if err != nil {
			return err
		}
	}
}

// await waits for one change on cur and answers with the registration that is
// live for the reload about to run.
func (w *Watched[T]) await(cur Change) (Change, error) {
	ok, err := cur.Wait(w.ctx)
	if err != nil {
		_ = cur.Close()

		return nil, watchLost(err)
	}

	if !ok {
		_ = cur.Close()

		return nil, w.ended()
	}

	return w.settle(cur)
}

// ended names the quiet ending. A wait that answers false with no error is the
// context being done, and a plane that ends its own watch for a reason it will
// not name is still an ending the caller must be able to see.
func (w *Watched[T]) ended() error {
	if err := w.ctx.Err(); err != nil {
		return err
	}

	return fmt.Errorf("%w: %w: the plane ended this watch", ErrPlane, ErrWatchLost)
}

// settle re-arms and, under a debounce, swallows the rest of the burst.
//
// Something is armed at every moment, including while the burst is being
// swallowed, so coalescing costs latency and never a change.
func (w *Watched[T]) settle(cur Change) (Change, error) {
	deadline := time.Now().Add(w.cfg.debounce)

	for {
		next, err := w.n.Notify(w.ctx)
		_ = cur.Close()

		if err != nil {
			return nil, watchLost(err)
		}

		cur = next

		if w.cfg.debounce <= 0 || !time.Now().Before(deadline) {
			return cur, nil
		}

		more, err := w.drain(cur, deadline)
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
func (w *Watched[T]) drain(cur Change, deadline time.Time) (bool, error) {
	ctx, cancel := context.WithDeadline(w.ctx, deadline)
	defer cancel()

	ok, err := cur.Wait(ctx)
	if err != nil {
		_ = cur.Close()

		return false, watchLost(err)
	}

	if !ok && w.ctx.Err() != nil {
		_ = cur.Close()

		return false, w.ctx.Err()
	}

	return ok, nil
}

// watchLost states the class and keeps the plane's own reason reachable.
func watchLost(cause error) error {
	return fmt.Errorf("%w: %w: %w", ErrPlane, ErrWatchLost, cause)
}
