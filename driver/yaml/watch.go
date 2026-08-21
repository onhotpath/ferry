//go:build !protoe

package yaml

import (
	"context"
	"time"
)

// look is how often a watch stats the file when the caller named no interval.
//
// A second is the resolution a config file is worth: an operator edits one by
// hand, and a reload that lands within a second of the save is
// indistinguishable from one that landed at it.
const look = time.Second

// watch is the whole of the [Watch] option: what to call, how often to look, and
// the context that ends it.
//
// The context is held rather than passed in because there is nowhere to pass one
// (ADR-0020). Watching starts when the source is built and outlives every
// individual load, so the lifetime cannot come from a load's context, and the
// driver grows no Stop method for it: core ships no watch lifecycle, and one
// invented here would be a second lifecycle beside the one the caller already
// has.
type watch struct {
	ctx      context.Context
	every    time.Duration
	onChange func(context.Context)
}

// start fingerprints the plane as it is now and polls it from then on.
//
// The first stamp is taken here, on the caller's own goroutine, and not on the
// polling one: taken there, a change made between [NewSource] returning and the
// goroutine first running would be read as the starting state and never
// announced. Taken here it is the state the caller had when it asked to watch,
// so every change after that is seen whenever the goroutine gets to run.
func (w *watch) start(path string) {
	go w.poll(path, planeStamp(path))
}

// poll is the loop: look, compare, call, until the context ends.
//
// The callback runs on this goroutine and one at a time, so a slow callback
// delays the next look rather than piling up beside itself, and changes that
// land while it runs coalesce into the one comparison that follows it. That is
// the same contract a driver's change signal carries anywhere: it says the plane
// may have changed, and the reload is what reads the truth (ADR-0020).
//
// Nothing here fences the callback. It is user code, and ADR-0011's fence exists
// to keep one panicking call from killing an aggregate that had already
// collected other addresses' answers - there is no aggregate here, no error to
// deliver one into, and the panicking call is the top of this goroutine's own
// stack, which is exactly the ground the fence was added because concurrency had
// taken away.
func (w *watch) poll(path string, from stamp) {
	tick := time.NewTicker(w.every)
	defer tick.Stop()

	for w.wait(tick.C) {
		if now := planeStamp(path); now != from {
			from = now

			w.onChange(w.ctx)
		}
	}
}

// wait blocks until the next look is due and reports whether to take it.
//
// The context is read twice, and the second read is the one that matters: a
// select whose two cases are both ready picks between them at random, so a
// watch cancelled while a tick was pending would otherwise be able to call back
// once more afterwards. Cancelling is the only way to stop a watch, so it has to
// mean stopped.
func (w *watch) wait(tick <-chan time.Time) bool {
	select {
	case <-w.ctx.Done():
		return false
	case <-tick:
		return w.ctx.Err() == nil
	}
}
