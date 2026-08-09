package winreg

import (
	"context"
	"errors"
	"fmt"

	"github.com/onhotpath/ferry"
)

// ErrWatch reports a watch this driver could not open.
//
// A registry that reports no changes is the case: [Watch] over one is refused,
// because a watch that opens successfully and never fires is the failure mode the
// option exists to avoid.
//
// It wraps [ferry.ErrPlane], and it stays reachable under ferry's wrapper, so
// errors.Is answers for it on what [ferry.Load] returned.
var ErrWatch = errors.New("winreg: this watch could not be opened")

// watcher is the whole of the [Watch] option: what to call, and the context that
// ends it.
//
// The context is held rather than passed in because there is nowhere to pass one
// (ADR-0020). Watching starts when the source is built and outlives every
// individual load, so the lifetime cannot come from a load's context, and the
// driver grows no Stop method for it: core ships no watch lifecycle, and one
// invented here would be a second lifecycle beside the one the caller already
// has.
type watcher struct {
	ctx      context.Context
	onChange func(context.Context)

	// on is the registry's own change notification, and it is nil until
	// [watcher.start] has found one.
	on Notifier
}

// Watch calls onChange whenever anything under this driver's key changes, so that
// a process holding a loaded value can load a fresh one.
//
//	b, err := ferry.Bind[Config](winreg.NewSource(hive, path, winreg.Watch(ctx, reload)))
//
//	func reload(ctx context.Context) {
//		cfg, err := b.Load(ctx) // a reload is a load
//		...                     // publish it by replacement, never by mutation
//	}
//
// It is opt-in and it is the only thing in this package that runs on a goroutine
// of its own. A source built without it touches the registry only when a load asks
// it to.
//
// The watch begins when the source is built and ends when ctx is done, which is
// the only way to stop it: cancel the context you gave it, and the goroutine
// returns. The context reaches onChange as its argument, so a deadline, a
// cancellation and whatever the caller put in it are all in hand there.
//
// The whole subtree is watched, so a change to any value or any subkey under this
// driver's key fires it, and a change elsewhere in the hive does not.
//
// It refuses at Bind, before any load, when the registry behind this source
// reports no changes. On Windows the machine's own registry always does; a
// [Store] of your own has to say so as well.
//
// Sharp edges, and they are the reason this is a callback and not a stream.
//
// onChange runs on the watching goroutine and one call at a time. A slow callback
// delays the next look rather than running beside itself, and changes that land
// while it runs are one call afterwards rather than several.
//
// A panic in onChange takes the process down, exactly as it would on a goroutine
// the caller started. Nothing here recovers it: there is no result to hand a
// failure back through, and a watch that swallowed the panic would leave a process
// that has silently stopped reloading.
//
// Watching starts when the source is built, so it starts before [ferry.Bind] has
// handed back the binding the callback wants to load through. Publish the binding
// to the callback in a way that orders the two - an atomic pointer, or a channel
// the callback reads before it uses one.
//
// A call says the key may have changed and nothing more. Load to find out what it
// holds now, which is correct whether the change was real or a rewrite of the same
// bytes.
//
// A dump through [Sink] over the same key fires it, so a process that both watches
// and saves its own configuration hears its own writes. Nothing here suppresses
// that.
//
// Losing the watch fires the callback once and stops. There is nowhere to report
// it, and the load that follows reports it through a surface the caller already
// handles. A cancelled context stops silently instead, so only losing the watch
// speaks.
func Watch(ctx context.Context, onChange func(context.Context)) Option {
	return sourceOnly(func(c *config) {
		if onChange == nil {
			return
		}

		c.watch = &watcher{ctx: ctx, onChange: onChange}
	})
}

// start finds the registry's change notification and puts the loop on a goroutine
// of its own. It runs inside the constructor, on the caller's goroutine, which is
// what gives a failure somewhere to go (ADR-0020).
func (w *watcher) start(store Registry) error {
	on, ok := store.(Notifier)
	if !ok {
		return watchError("this registry reports no change of its own, so a watch over it could never fire")
	}

	w.on = on

	go w.run()

	return nil
}

// run is the loop: wait for a change, call back, until the context ends or the
// watch is lost.
//
// The callback runs on this goroutine and one at a time, so a slow callback delays
// the next look rather than piling up beside itself. That is the same contract a
// driver's change signal carries anywhere: it says the plane may have changed, and
// the reload is what reads the truth (ADR-0020).
//
// Nothing here fences the callback. It is user code, and the panicking call is the
// top of this goroutine's own stack.
func (w *watcher) run() {
	for {
		changed, err := w.on.Notify(w.ctx)

		// The context is read before the answer is, and that ordering matters: a
		// watch cancelled while a change was already in flight would otherwise be
		// able to call back once more afterwards. Cancelling is the only way to
		// stop a watch, so it has to mean stopped.
		if w.ctx.Err() != nil {
			return
		}

		if err != nil {
			// The watch is gone. One call, so that the load which follows reports
			// whatever is actually wrong through a surface the caller handles.
			w.fire()

			return
		}

		if !changed {
			return
		}

		w.fire()
	}
}

// fire calls back, unless the context ended in the meantime.
func (w *watcher) fire() {
	if w.ctx.Err() == nil {
		w.onChange(w.ctx)
	}
}

// watchError states the class this driver has an opinion about and keeps
// [ErrWatch] reachable underneath it.
func watchError(msg string) error {
	return fmt.Errorf("%w: %w: %s", ferry.ErrPlane, ErrWatch, msg)
}
