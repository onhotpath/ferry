// Package watcher is the announcement seam written by the caller: a process
// that reloads when it is told to, rather than when a plane says so.
//
// A driver that can watch its own plane converts to a watchable source with
// Watched(), and nothing here is needed. What is needed here is the other case:
// a plane with no watch of its own, reloaded on SIGHUP or on an admin endpoint.
// ferry mints nothing for that on purpose, because the seam is small enough to
// write. [Kick] is the whole of it.
//
// [MemPlane] is the plane the examples reload from. It is small enough to read
// in one sitting and it is not a driver; use one of the modules under driver/
// for anything real.
package watcher

import (
	"context"
	"errors"
	"os"

	"github.com/onhotpath/ferry"
)

// Kick makes any source watchable off a channel the caller owns, so a reload
// happens when this process decides and not when a plane announces.
//
// The usual channel is the one os/signal fills with SIGHUP, which the runnable
// Example on this package shows end to end. An admin endpoint is the same
// wiring, sending on that channel itself rather than waiting for the operating
// system to.
//
// Buffer the channel. A registration is live from the moment the stream places
// it, and the buffer is what holds a kick that lands while a reload is still
// running; an unbuffered channel drops one unless the stream is already
// waiting.
//
// A Kick carries one mechanism and no more, so a process that wants its own
// kick and a driver's own watch under one binding has two mechanisms to fan in,
// and ferry does not fan them in for you.
type Kick struct {
	// Source is the plane to reload from. It does not have to be watchable,
	// which is the point of wrapping it.
	Source ferry.Source

	// On is the channel a kick arrives on, and one receive is one change. What
	// is received is never read: like every change in ferry it is a hint, and
	// the reload is what reads the plane.
	//
	// Closing it ends the stream under [ferry.ErrWatchLost], because a
	// mechanism that has gone away is exactly what that reports.
	On <-chan os.Signal
}

// Bind hands the addresses straight to the source, which is the whole of what
// wrapping one costs.
//
// A Kick naming no source is refused here, so the mistake lands at
// [ferry.BindWatched] rather than at the first load.
func (k Kick) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	if k.Source == nil {
		return nil, errors.New("no source to reload")
	}

	return k.Source.Bind(addrs)
}

// Watching hands over the mechanism, and refuses a Kick with no channel to
// reload on.
//
// The refusal lands at [ferry.BindWatched], before any load, carrying
// [ferry.ErrPlane]. A watch that could never fire is worth learning about at
// startup rather than from a stream that goes quiet.
func (k Kick) Watching() (ferry.Notifier, error) {
	if k.On == nil {
		return nil, errors.New("no channel to reload on")
	}

	return kick{on: k.On}, nil
}

// kick is the mechanism and one registration over it at the same time, which a
// channel can be: it is armed from the moment it exists, so a kick landing
// between the registration and the wait sits in the buffer rather than being
// lost. That is the invariant ferry's stream asks a Notifier for (ADR-0020).
type kick struct{ on <-chan os.Signal }

func (k kick) Notify(context.Context) (ferry.Change, error) { return k, nil }

func (k kick) Wait(ctx context.Context) (bool, error) {
	select {
	case <-ctx.Done():
		return false, nil
	case _, open := <-k.on:
		// A closed channel is the mechanism going away, and false with no
		// error is how the seam spells that (ADR-0020).
		return open, nil
	}
}

// Close releases nothing. The channel is the caller's and outlives every
// registration made over it.
func (kick) Close() error { return nil }
