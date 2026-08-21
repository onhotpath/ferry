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
	// BindWatch refuses a watch this source cannot open, and it is called
	// during [Bind] when [WithWatch] asked for one.
	//
	// It does no I/O, and the missing [context.Context] is how the type says so.
	// What it may refuse is what the driver can see without touching the plane:
	// a source configured to watch nothing, a mechanism this build has no access
	// to. Everything else is refused by [Notifier.Notify], where the
	// registration is actually placed.
	//
	// It is the second half of the seam because watchability is
	// option-dependent: a source type implements this interface whatever its
	// options say, so the type assertion alone cannot answer whether this
	// particular source has anything to watch.
	BindWatch() error

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

// WithWatch declares that this binding will be watched, so that a source that
// cannot be watched is refused at [Bind] rather than at [Binding.Watch].
//
//	b, err := ferry.Bind[Config](src, ferry.WithWatch())
//
// It reaches no plane. It asserts the source implements [Notifier] and asks the
// driver's own [Notifier.BindWatch] whether this source has anything to watch,
// both before any load and both refusing with [ErrNotWatchable].
//
// A binding built without it can still be watched, and pays for the omission by
// finding out at the [Binding.Watch] call instead.
func WithWatch() Option { return watchDeclared{} }

// watchDeclared is [WithWatch]'s value, and it is a type of its own so that the
// bind can recognise it in the Option list. It changes nothing a schema
// compiles to and nothing a load runs under, so it settles nothing into the
// resolved config.
type watchDeclared struct{}

func (watchDeclared) apply(*config) error { return nil }

// afterBind is the protoa half of the build-tagged pair: where [WithWatch] was
// given, the capability refusal lands here, on the Bind seam, and never at the
// call that opens the stream.
func afterBind(opts []Option, src Source) error {
	asked := declared(opts)

	if asked > 1 {
		return fmt.Errorf("%w: ferry.WithWatch was supplied twice", ErrSchema)
	}

	if asked == 0 {
		return nil
	}

	_, err := notifierOf(src)

	return err
}

// declared counts the WithWatch options in one list.
func declared(opts []Option) int {
	var n int

	for _, o := range opts {
		if _, ok := o.(watchDeclared); ok {
			n++
		}
	}

	return n
}

// notifierOf is the whole capability check, and it is one function so that the
// Bind seam and the Watch call cannot drift into refusing different things.
func notifierOf(src Source) (Notifier, error) {
	n, ok := src.(Notifier)
	if !ok {
		return nil, fmt.Errorf("%w: %w", ErrPlane, ErrNotWatchable)
	}

	if err := n.BindWatch(); err != nil {
		return nil, fmt.Errorf("%w: %w: %w", ErrPlane, ErrNotWatchable, err)
	}

	return n, nil
}

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
// It refuses a [WatchOption] list that does not resolve. It also refuses a
// source that cannot be watched, with an error wrapping [ErrNotWatchable] -
// unless the binding was built with [WithWatch], which moves that refusal onto
// [Bind] where a plane refusal belongs.
//
// It reaches no plane and starts no goroutine: the registration is placed when
// the range starts, and ctx is the whole lifetime of both the registration and
// the stream.
func (b *Binding[T]) Watch(ctx context.Context, opts ...WatchOption) (*Watched[T], error) {
	n, err := notifierOf(b.b.src)
	if err != nil {
		return nil, err
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
		return watchLost(err)
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

// stream is the whole range: place the first registration, then loop.
func (w *Watched[T]) stream(yield func(T) bool) error {
	a := &armed{}
	defer a.close()

	if err := a.rearm(w.ctx, w.n); err != nil {
		return err
	}

	return w.loop(a, yield)
}

// loop is one turn per value: load, hand it over, wait for the next change.
//
// The registration is already armed when the load runs, which is what makes a
// change landing inside the load the next change rather than a lost one.
func (w *Watched[T]) loop(a *armed, yield func(T) bool) error {
	for {
		v, err := w.b.Load(w.ctx)
		if err != nil {
			return err
		}

		if !yield(v) {
			return nil
		}

		if err := w.await(a); err != nil {
			return err
		}
	}
}

// await waits for one change, leaving a armed for the reload that follows it.
func (w *Watched[T]) await(a *armed) error {
	ok, err := a.c.Wait(w.ctx)
	if err != nil {
		return watchLost(err)
	}

	if !ok {
		return w.ended()
	}

	return w.settle(a)
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
func (w *Watched[T]) settle(a *armed) error {
	deadline := time.Now().Add(w.cfg.debounce)

	for {
		if err := a.rearm(w.ctx, w.n); err != nil {
			return err
		}

		more, err := w.quiet(a, deadline)
		if err != nil {
			return err
		}

		if !more {
			return nil
		}
	}
}

// quiet reports whether the burst is still arriving. It is where the debounce
// window is read, so that the loop above holds one decision at a time.
func (w *Watched[T]) quiet(a *armed, deadline time.Time) (bool, error) {
	if w.cfg.debounce <= 0 || !time.Now().Before(deadline) {
		return false, nil
	}

	ctx, cancel := context.WithDeadline(w.ctx, deadline)
	defer cancel()

	ok, err := a.c.Wait(ctx)
	if err != nil {
		return false, watchLost(err)
	}

	if !ok && w.ctx.Err() != nil {
		return false, w.ctx.Err()
	}

	return ok, nil
}

// watchLost states the class and keeps the plane's own reason reachable.
func watchLost(cause error) error {
	return fmt.Errorf("%w: %w: %w", ErrPlane, ErrWatchLost, cause)
}
