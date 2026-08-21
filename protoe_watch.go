//go:build protoe

package ferry

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"time"
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

// WatchAll makes one [WatchableSource] out of the source a load reads through
// and the watchable sources whose changes it is composed of.
//
//	base  := env.New(env.DotEnv("base.env")).Watched()
//	local := env.New(env.DotEnv("local.env")).Watched()
//	src   := ferry.WatchAll(layered{base, local}, base, local)
//
// A change on any of them is a change on the whole, and one announcement per
// layer for one deployment is one reload under [Debounce]. Every layer is armed
// before any of them is waited on, so a change on the quiet layer while the
// noisy one is being read is not lost.
//
// It composes the watch and never the read. Which layer wins at an address is
// the read source's own business, because ferry has no opinion about layering
// and this is not the place to invent one.
//
// It refuses at [BindWatched] where any layer refuses, naming that layer's own
// reason, and where it was given no layer at all.
func WatchAll(read Source, of ...WatchableSource) WatchableSource {
	return allOf{read: read, of: of}
}

// allOf is [WatchAll]'s value: one reader, many mechanisms.
type allOf struct {
	read Source
	of   []WatchableSource
}

func (a allOf) Bind(addrs *AddressSet) (OpenFunc, error) {
	if a.read == nil {
		return nil, nilPlane(nilSourceMsg)
	}

	return a.read.Bind(addrs)
}

// Watching collects every layer's mechanism, refusing on the first layer that
// cannot be watched, so a composite is refused at the bind exactly as a single
// source is.
func (a allOf) Watching() (Notifier, error) {
	if len(a.of) == 0 {
		return nil, errors.New("ferry.WatchAll was given no watchable source")
	}

	ns := make([]Notifier, 0, len(a.of))

	for _, w := range a.of {
		n, err := watching(w)
		if err != nil {
			return nil, err
		}

		ns = append(ns, n)
	}

	return fanIn(ns), nil
}

// fanIn is many mechanisms behind one, and it is the whole of what a caller
// composing two watchable planes used to have to write.
type fanIn []Notifier

// Notify arms every layer before any of them is waited on.
func (f fanIn) Notify(ctx context.Context) (Change, error) {
	cs := make([]Change, 0, len(f))

	for _, n := range f {
		c, err := n.Notify(ctx)
		if err != nil {
			closeAll(cs)

			return nil, err
		}

		cs = append(cs, c)
	}

	return &fanInChange{cs: cs}, nil
}

// fanInChange is one registration per layer, waited on together.
type fanInChange struct{ cs []Change }

// Wait answers with whichever layer speaks first, and stops the rest.
func (f *fanInChange) Wait(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	answers := make(chan waited, len(f.cs))

	for _, c := range f.cs {
		go func() {
			ok, err := c.Wait(ctx)
			answers <- waited{ok: ok, err: err}
		}()
	}

	first := <-answers

	cancel()

	// Every layer's wait is drained before this returns, so no goroutine
	// outlives the call that started it.
	for range len(f.cs) - 1 {
		<-answers
	}

	return first.ok, first.err
}

// waited is one layer's answer.
type waited struct {
	ok  bool
	err error
}

func (f *fanInChange) Close() error {
	closeAll(f.cs)

	return nil
}

// closeAll releases every registration and discards what each reports, which is
// what a watcher can do with it.
func closeAll(cs []Change) {
	for _, c := range cs {
		_ = c.Close()
	}
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
func (wb *WatchedBinding[T]) Watch(ctx context.Context, opts ...WatchOption,
) (seq iter.Seq[T], errf func() error) {
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

	return s.settle(a)
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

// settle re-arms and, under a debounce, swallows the rest of the burst.
//
// Something is armed at every moment, including while the burst is being
// swallowed, so coalescing costs latency and never a change.
func (s *watchStream[T]) settle(a *armed) error {
	deadline := time.Now().Add(s.cfg.debounce)

	for {
		if err := a.rearm(s.ctx, s.n); err != nil {
			return err
		}

		more, err := s.quiet(a, deadline)
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
func (s *watchStream[T]) quiet(a *armed, deadline time.Time) (bool, error) {
	if s.cfg.debounce <= 0 || !time.Now().Before(deadline) {
		return false, nil
	}

	ctx, cancel := context.WithDeadline(s.ctx, deadline)
	defer cancel()

	ok, err := a.c.Wait(ctx)
	if err != nil {
		return false, lostWatch(err)
	}

	if !ok && s.ctx.Err() != nil {
		return false, s.ctx.Err()
	}

	return ok, nil
}

// lostWatch states the class and keeps the plane's own reason reachable.
func lostWatch(cause error) error {
	return fmt.Errorf("%w: %w: %w", ErrPlane, ErrWatchLost, cause)
}
