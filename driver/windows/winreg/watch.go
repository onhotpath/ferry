//go:build !protoe

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
// reports no changes and when the first registration could not be placed. On
// Windows the machine's own registry always reports changes; a [Store] of your
// own has to say so as well.
//
// A key that is not there yet is not a refusal. The registration goes on the
// nearest key above it that does exist, so a watch over the key a first save will
// create fires when that save creates it, and moves down to the key itself from
// then on. The cost is that until the key exists a change to something else under
// that ancestor wakes it too, which is a spurious call and costs one load.
//
// Sharp edges, and they are the reason this is a callback and not a stream.
//
// onChange runs on the watching goroutine and one call at a time. A slow callback
// delays the next look rather than running beside itself, and changes that land
// while it runs are one call afterwards rather than several: the next
// registration is placed before the callback starts, so the whole of a slow
// reload is covered by it.
//
// A panic in onChange takes the process down, exactly as it would on a goroutine
// the caller started. Nothing here recovers it: there is no result to hand a
// failure back through, and a watch that swallowed the panic would leave a process
// that has silently stopped reloading.
//
// Watching starts when the source is built, so it starts before [ferry.Bind] has
// handed back the binding the callback wants to load through, and a change can
// land while there is nothing yet to load through. A Signal from
// github.com/onhotpath/ferry/watch is what to pass here in that case: its Changed
// method records such a change rather than losing it, and the stream that opens
// afterwards begins with that reload.
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
	return watchOpt{w: &watcher{ctx: ctx, onChange: onChange}}
}

// watchOpt is [Watch]'s value, and it is a type of its own so that [NewSource]
// can recognise it in the Option list once the configuration behind it has
// settled. It settles nothing into the config, because what it carries is the
// whole setting.
type watchOpt struct{ w *watcher }

func (watchOpt) apply(*config) {}

// startWatch opens whatever the Option list asked for, and it runs inside
// [NewSource] on the caller's own goroutine, which is what gives a failure
// somewhere to go (ADR-0020).
func startWatch(opts []Option, c *config) {
	if c.err != nil {
		return
	}

	for _, o := range opts {
		w, ok := o.(watchOpt)
		if !ok || w.w.onChange == nil {
			continue
		}

		c.refuse(w.w.start(c.store))

		return
	}
}

// start finds the registry's change notification, places the first registration
// and puts the loop on a goroutine of its own. It runs inside the constructor, on
// the caller's goroutine, which is what gives a failure somewhere to go
// (ADR-0020).
//
// A context that is already done is a watch that has already ended, so nothing is
// registered and no goroutine is started. That is what the loop would do with it
// anyway, one wait later.
func (w *watcher) start(store Registry) error {
	on, ok := store.(Notifier)
	if !ok {
		return watchError("this registry reports no change of its own, so a watch over it could never fire")
	}

	w.on = on

	if w.done() {
		return nil
	}

	armed, err := on.Arm(w.ctx)
	if err != nil {
		return fmt.Errorf("%w: %w: the registry would not report its changes: %w", ferry.ErrPlane, ErrWatch, err)
	}

	go w.run(armed)

	return nil
}

// done reports a watch whose context ended before it began, which is one that
// registers nothing and starts no goroutine.
func (w *watcher) done() bool {
	select {
	case <-w.ctx.Done():
		return true
	default:
		return false
	}
}

// run is the loop: wait for the armed change, arm the next one, call back, until
// the context ends or the watch is lost.
//
// The callback runs on this goroutine and one at a time, so a slow callback delays
// the next look rather than piling up beside itself. That is the same contract a
// driver's change signal carries anywhere: it says the plane may have changed, and
// the reload is what reads the truth (ADR-0020).
//
// Nothing here fences the callback. It is user code, and the panicking call is the
// top of this goroutine's own stack.
func (w *watcher) run(armed Change) {
	for armed != nil {
		armed = w.step(armed)
	}
}

// step is one turn of the loop, and the ordering inside it is the whole of what
// stops a change being lost.
//
// The next registration is placed before the callback runs and before the current
// one is released, so there is no moment between a change and the next
// registration where the plane is unwatched. A change landing during the callback
// signals the registration that is already armed, and the wait that follows
// returns at once.
//
// It answers with the registration the next turn waits on, and nil where the
// watch has ended.
func (w *watcher) step(armed Change) Change {
	changed, err := armed.Wait(w.ctx)
	if err != nil || !changed {
		return w.stop(armed, err)
	}

	next, err := w.on.Arm(w.ctx)

	release(armed)

	if err != nil {
		return w.stop(next, err)
	}

	w.fire()

	return next
}

// stop releases the registration and ends the loop, speaking once where the watch
// was lost rather than cancelled.
//
// The context is read before the answer is, inside [watcher.fire], and that
// ordering matters: a watch cancelled while a change was already in flight would
// otherwise be able to call back once more afterwards. Cancelling is the only way
// to stop a watch, so it has to mean stopped.
func (w *watcher) stop(armed Change, err error) Change {
	release(armed)

	if err != nil {
		// The watch is gone. One call, so that the load which follows reports
		// whatever is actually wrong through a surface the caller handles.
		w.fire()
	}

	return nil
}

// release closes one registration, and nothing where there is none: an Arm that
// failed answers with no Change to close.
//
// What it reports is discarded, because a watcher that is stopping has nowhere to
// put it and the caller has already been told everything it can act on.
func release(armed Change) {
	if armed != nil {
		_ = armed.Close()
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
