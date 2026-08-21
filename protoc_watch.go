//go:build protoc

package ferry

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync"
	"time"
)

// Variant C of the typed watch prototype: core mints a sealed handle, the
// driver announces into it through a port core also mints, and the bind checks
// that the handle and the source were wired to each other.
//
// It is ADR-0017's pattern read on the lifetime rather than on the source: the
// handle is the proof-carrying value, and it carries the one context, the
// pending change and the ending. The driver keeps the Option seam all three
// shipped drivers already have.

// ErrWatchNotWired reports a watch handle no driver ever announced into, or one
// wired to a source other than the one being bound. It wraps [ErrPlane].
var ErrWatchNotWired = errors.New("this watch handle is wired to nothing")

// ErrNotWatchable reports a driver that could not open the watch it was asked
// for. It wraps [ErrPlane].
var ErrNotWatchable = errors.New("this source cannot be watched")

// ErrWatchLost reports a watch the plane could not keep. It wraps [ErrPlane].
var ErrWatchLost = errors.New("the watch was lost")

// ErrWatchInUse reports a second stream over one handle. It wraps [ErrPlane].
var ErrWatchInUse = errors.New("this watch is already being ranged")

// Watch is a watch a caller opened and has not bound yet: one context, one
// pending change, one ending.
//
// [NewWatch] is the only way to make one. Hand it to a driver's own watch
// option, then to [BindWatched], and range what that hands back.
//
//	h := ferry.NewWatch(ctx)
//	src := env.New(env.DotEnv(".env"), env.Watched(h))
//	wb, err := ferry.BindWatched[Config](src, h)
//	seq, errf := wb.Watch()
//
// It is the whole lifetime. The context it was built with bounds the driver's
// own watching goroutine and the stream alike, so cancelling it ends both and
// there is not a second one to keep in step.
//
// It is safe for use from many goroutines, and it is one stream: a second range
// over one handle is refused rather than sharing the changes out.
type Watch struct {
	ctx context.Context

	// fired holds one pending change and no more, so a burst is one change and
	// so is a change that lands while a reload is running.
	fired chan struct{}

	// done is closed when a driver reports the watch has ended, which is what
	// makes a lost watch something the caller observes rather than a silence.
	done chan struct{}

	mu      sync.Mutex
	wired   []Source
	refusal error
	endErr  error
	ended   bool
	ranged  bool
}

// NewWatch opens a watch over ctx.
//
// It starts nothing and reaches no plane. What it is, until a driver is given
// it, is a place for a change to be recorded and an ending to be reported.
func NewWatch(ctx context.Context) *Watch {
	return &Watch{ctx: ctx, fired: make(chan struct{}, 1), done: make(chan struct{})}
}

// Wire declares that src announces its changes into this handle, and answers
// with the port src announces through.
//
// A driver calls it from its own constructor, once the source is built, so that
// [BindWatched] can check the handle and the source belong together. A source
// composed of watchable sources wires itself the same way and announces
// nothing: its members do that through their own ports.
func (w *Watch) Wire(src Source) *WatchPort {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.wired = append(w.wired, src)

	return &WatchPort{w: w}
}

// WatchPort is one driver's end of a [Watch]: the context to watch under, the
// announcement, the ending, and the refusal.
//
// [Watch.Wire] is the only way to get one, so a value that can announce into a
// caller's watch exists only where a driver was handed the handle.
type WatchPort struct{ w *Watch }

// Context is the lifetime the driver watches under, and cancelling it is the
// whole of stopping. A driver needs no Stop method and grows no second
// lifecycle beside the caller's own.
func (p *WatchPort) Context() context.Context { return p.w.ctx }

// Changed records that the plane may have changed, and returns immediately.
//
// It never blocks and never fails, so a watching goroutine feels no back
// pressure from a slow consumer, and calling it with nobody ranging is normal:
// the change waits, and the next stream opens having reloaded.
func (p *WatchPort) Changed() {
	select {
	case p.w.fired <- struct{}{}:
	default:
		// One is already pending, and one pending change is all a reload needs
		// (ADR-0020).
	}
}

// Ended reports that this watch is over and why: the directory removed, the
// registration lost, the connection dropped. A nil error is an ending with no
// fault in it.
//
// The stream ends at the next change it waits for, reporting the reason. It is
// what a driver calls instead of returning from its goroutine quietly, and it
// is the whole of why a lost watch is not a process holding stale configuration
// with nothing to tell it so.
//
// Calling it twice reports the first reason and discards the second.
func (p *WatchPort) Ended(err error) {
	p.w.mu.Lock()
	defer p.w.mu.Unlock()

	if p.w.ended {
		return
	}

	p.w.endErr, p.w.ended = err, true
	close(p.w.done)
}

// Refuse reports that this driver cannot open the watch it was asked for: no
// file named, a mechanism this build has no access to.
//
// The refusal surfaces at [BindWatched], before any load, because a driver's
// constructor has nowhere to report one. A driver calls it instead of starting
// a watch that would never fire.
func (p *WatchPort) Refuse(err error) {
	p.w.mu.Lock()
	defer p.w.mu.Unlock()

	if p.w.refusal == nil {
		p.w.refusal = err
	}
}

// check answers whether this handle may be bound against src.
func (w *Watch) check(src Source) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.refusal != nil {
		return fmt.Errorf("%w: %w: %w", ErrPlane, ErrNotWatchable, w.refusal)
	}

	if len(w.wired) == 0 {
		return fmt.Errorf("%w: %w: no driver was given it", ErrPlane, ErrWatchNotWired)
	}

	for _, s := range w.wired {
		if s == src {
			return nil
		}
	}

	return fmt.Errorf("%w: %w: it was wired to a different source", ErrPlane, ErrWatchNotWired)
}

// claim makes the handle one stream.
func (w *Watch) claim() bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.ranged {
		return false
	}

	w.ranged = true

	return true
}

// ending is why the watch is over, and it is read after done is closed.
func (w *Watch) ending() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.endErr == nil {
		return fmt.Errorf("%w: %w: the plane ended this watch", ErrPlane, ErrWatchLost)
	}

	return fmt.Errorf("%w: %w: %w", ErrPlane, ErrWatchLost, w.endErr)
}

// WatchedBinding is a source bound to a compiled type and to the watch that
// announces its changes: it loads like a [Binding] and it streams.
//
// [BindWatched] produced it, which is the only way there is.
type WatchedBinding[T any] struct {
	b *Binding[T]
	h *Watch
}

// BindWatched compiles T, hands src the addresses T names, and answers with a
// binding that both loads and streams.
//
// It refuses, before any load: a handle no driver was given, a handle wired to
// a different source, and a driver that was given the handle and could not open
// the watch. The first two wrap [ErrWatchNotWired] and the third
// [ErrNotWatchable], carrying the driver's own reason underneath.
//
// It reaches no plane and starts no goroutine of its own. Whatever the driver
// started, it started under the handle's context.
func BindWatched[T any](src Source, h *Watch, opts ...Option) (*WatchedBinding[T], error) {
	if h == nil {
		return nil, fmt.Errorf("%w: %w", ErrPlane, ErrWatchNotWired)
	}

	if err := h.check(src); err != nil {
		return nil, err
	}

	b, err := Bind[T](src, opts...)
	if err != nil {
		return nil, err
	}

	return &WatchedBinding[T]{b: b, h: h}, nil
}

// Binding is the load half on its own, for handing to code that loads and does
// not watch.
func (wb *WatchedBinding[T]) Binding() *Binding[T] { return wb.b }

// Load builds a value of T, exactly as [Binding.Load] does.
func (wb *WatchedBinding[T]) Load(ctx context.Context) (T, error) { return wb.b.Load(ctx) }

// Watch streams a freshly loaded value of T: one when the range opens, and one
// for every change the handle records afterwards.
//
//	seq, errf := wb.Watch()
//	for cfg := range seq {
//	    publish(cfg)
//	}
//	if err := errf(); err != nil {
//	    alert(err)
//	}
//
// It takes no context, because the handle is the lifetime.
//
// The stream opens with a load, so there is no pre-load to write and no change
// that landed before the bind returned to lose: one that landed while the
// driver was watching and nothing was bound is pending, and the opening load
// reads what it announced.
//
// It ends on a failed reload, on a watch the driver reported over, or on
// cancellation of the handle's context, and errf reports why, once, after the
// range exits. Breaking out of the range is a clean ending and errf reports nil.
//
// One handle is one stream. A second range is refused rather than sharing the
// changes out with the first.
func (wb *WatchedBinding[T]) Watch(opts ...WatchOption) (iter.Seq[T], func() error) {
	cfg, err := resolveWatch(opts)
	if err != nil {
		return func(func(T) bool) {}, func() error { return err }
	}

	s := &watchStream[T]{b: wb.b, h: wb.h, cfg: cfg}

	return func(yield func(T) bool) { s.err = s.open(yield) }, func() error { return s.err }
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
// one per announcement. Nothing is lost by waiting: an announcement inside the
// window is the change the reload after it reads.
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
	h   *Watch
	cfg watchConfig
	err error
}

// open polices the handle and runs the loop.
func (s *watchStream[T]) open(yield func(T) bool) error {
	if !s.h.claim() {
		return fmt.Errorf("%w: %w", ErrPlane, ErrWatchInUse)
	}

	return s.run(yield)
}

// run is the loop, returning the error that ended it: nil when the caller
// stopped ranging.
func (s *watchStream[T]) run(yield func(T) bool) error {
	for {
		v, err := s.b.Load(s.h.ctx)
		if err != nil {
			return err
		}

		if !yield(v) {
			return nil
		}

		if err := s.await(); err != nil {
			return err
		}
	}
}

// await waits for one change, or for whatever ends the stream instead.
func (s *watchStream[T]) await() error {
	select {
	case <-s.h.ctx.Done():
		return s.h.ctx.Err()
	case <-s.h.done:
		return s.h.ending()
	case <-s.h.fired:
	}

	// A select with several ready cases picks between them at random, so a
	// cancelled stream would otherwise yield one more value afterwards.
	if err := s.h.ctx.Err(); err != nil {
		return err
	}

	s.settle()

	return nil
}

// settle swallows the rest of a burst under a debounce. An announcement inside
// the window is not lost: it is the state the reload after it reads.
func (s *watchStream[T]) settle() {
	if s.cfg.debounce <= 0 {
		return
	}

	timer := time.NewTimer(s.cfg.debounce)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			return
		case <-s.h.ctx.Done():
			return
		case <-s.h.fired:
		}
	}
}
