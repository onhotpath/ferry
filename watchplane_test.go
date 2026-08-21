package ferry_test

import (
	"context"
	"maps"
	"sync"
	"time"

	"github.com/onhotpath/ferry"
)

// armedPlane is a mutable in-memory plane that announces its changes through
// the armed-once seam core's watch loop drives.
//
// It is the miniature of a Win32 notification handle rather than of an fsnotify
// queue, deliberately: the armed-once mechanism is the weaker of the two, so a
// loop that is correct against this one is correct against a queue (ADR-0020).
// Every open mints a snapshot, so a load in flight is unaffected by a write
// beside it, and it counts its opens so a test can prove a burst was one
// reload.
type armedPlane struct {
	mu    sync.Mutex
	data  map[ferry.Path]ferry.Value
	opens int

	// gen counts announcements. A registration records the generation it was
	// armed at, so a change between Notify and Wait is reported by that Wait.
	gen uint64

	// bell is closed and replaced on every announcement and on the loss, which
	// is how a waiter is woken without a channel per waiter.
	bell chan struct{}

	// lost is the reason the watch cannot be kept, and it is what makes a quiet
	// death observable.
	lost error

	// armFail is what the next Notify refuses with, so a test can drive a
	// registration that cannot be placed.
	armFail error

	// bindFail is what Watching refuses with, so a test can drive a source that
	// is watchable by type and watches nothing by configuration.
	bindFail error

	// handOverNothing makes Watching answer nil with a nil error, which is the
	// misuse an open interface admits and core has to refuse.
	handOverNothing bool

	// armedNow and closed count registrations placed and released, so a test
	// can assert core keeps the resource obligation Change.Close is.
	armedNow int
	closed   int
}

func newArmedPlane() *armedPlane {
	return &armedPlane{data: map[ferry.Path]ferry.Value{}, bell: make(chan struct{})}
}

func (p *armedPlane) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(context.Context) (ferry.Reader, error) {
		p.mu.Lock()
		defer p.mu.Unlock()

		p.opens++

		return armedSnapshot(maps.Clone(p.data)), nil
	}, nil
}

// Watching is the claim, and it is the instance-level refusal too: a plane told
// it has nothing to watch declines here, at the bind, rather than handing over a
// mechanism that would never fire.
func (p *armedPlane) Watching() (ferry.Notifier, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.bindFail != nil {
		return nil, p.bindFail
	}

	if p.handOverNothing {
		return nil, nil
	}

	return p, nil
}

func (p *armedPlane) Notify(context.Context) (ferry.Change, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.armFail != nil {
		return nil, p.armFail
	}

	p.armedNow++

	return &armedChange{p: p, at: p.gen}, nil
}

// Set writes one address and announces the change.
func (p *armedPlane) Set(addr ferry.Path, v ferry.Value) {
	p.mu.Lock()
	p.data[addr] = v
	p.mu.Unlock()

	p.announce()
}

// Delete removes one address and announces the change. A required field whose
// address is deleted is what makes the next reload fail.
func (p *armedPlane) Delete(addr ferry.Path) {
	p.mu.Lock()
	delete(p.data, addr)
	p.mu.Unlock()

	p.announce()
}

// Lose ends the watch the way a plane that cannot keep it does: every armed
// registration answers with the reason, and no announcement ever follows.
func (p *armedPlane) Lose(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.lost = err
	close(p.bell)
	p.bell = make(chan struct{})
}

// failArming makes every registration from here on unplaceable, which is the
// mechanism dying between one wait and the next.
func (p *armedPlane) failArming(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.armFail = err
}

// refuseWatching makes Watching answer with err, which is a source that is
// watchable by type and watches nothing by configuration.
func (p *armedPlane) refuseWatching(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.bindFail = err
}

// handOverNoMechanism makes Watching answer nil with a nil error, which is the
// contract violation an open interface admits.
func (p *armedPlane) handOverNoMechanism() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.handOverNothing = true
}

// Opens reports how many times the plane has been opened, which is once per
// load.
func (p *armedPlane) Opens() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.opens
}

// outstanding reports the registrations placed but never released.
func (p *armedPlane) outstanding() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.armedNow - p.closed
}

// generation reports how many announcements this plane has made, which is what
// a settle window compares against.
func (p *armedPlane) generation() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.gen
}

func (p *armedPlane) announce() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.gen++
	close(p.bell)
	p.bell = make(chan struct{})
}

// armedChange is one registration: the generation it was armed at, and the wait
// that reports anything after it.
type armedChange struct {
	p  *armedPlane
	at uint64
}

// settle is this plane's own coalescing window, and it is here because
// coalescing is the driver's job: core does not debounce, so a plane that emits
// bursts swallows its own (ADR-0020).
const settle = 50 * time.Millisecond

func (c *armedChange) Wait(ctx context.Context) (bool, error) {
	for {
		bell, done, err := c.look()
		if err != nil {
			return false, err
		}

		if done {
			c.coalesce(ctx)

			return true, nil
		}

		select {
		case <-ctx.Done():
			return false, nil
		case <-bell:
		}
	}
}

// coalesce waits for the plane to go quiet, so a burst of announcements is one
// change and one reload.
func (c *armedChange) coalesce(ctx context.Context) {
	for {
		at := c.p.generation()

		select {
		case <-ctx.Done():
			return
		case <-time.After(settle):
		}

		if c.p.generation() == at {
			return
		}
	}
}

// look reports what the plane holds now: the bell to wait on, whether a change
// has already landed, and the loss if there is one.
func (c *armedChange) look() (<-chan struct{}, bool, error) {
	c.p.mu.Lock()
	defer c.p.mu.Unlock()

	if c.p.lost != nil {
		return nil, false, c.p.lost
	}

	if c.p.gen > c.at {
		return nil, true, nil
	}

	return c.p.bell, false, nil
}

func (c *armedChange) Close() error {
	c.p.mu.Lock()
	defer c.p.mu.Unlock()

	c.p.closed++

	return nil
}

// armedSnapshot serves one load from an immutable copy of the contents.
type armedSnapshot map[ferry.Path]ferry.Value

func (s armedSnapshot) Get(_ context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	return s[addr.Path()], nil
}

// plainPlane is a source with no watch at all. It exists to be the compile
// error: ferry.BindWatched over one does not build, because it has no Watching
// method.
type plainPlane struct{}

func (plainPlane) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(context.Context) (ferry.Reader, error) { return armedSnapshot(nil), nil }, nil
}

// halfWatchable claims watchability and hands over a mechanism that never
// fires, which is the misuse no shape here can detect.
type halfWatchable struct{ plainPlane }

func (halfWatchable) Watching() (ferry.Notifier, error) { return silentNotifier{}, nil }

type silentNotifier struct{}

func (silentNotifier) Notify(context.Context) (ferry.Change, error) { return silentChange{}, nil }

type silentChange struct{}

func (silentChange) Wait(ctx context.Context) (bool, error) {
	<-ctx.Done()

	return false, nil
}

func (silentChange) Close() error { return nil }

// racingPlane is a cancellation landing on the re-arm: the wait answers with a
// change exactly as the context goes, and the registration that would follow it
// cannot be placed on a dead context.
//
// A real mechanism says so with an error of its own - a closed notification
// handle, a watcher already torn down - which is what makes this the case where
// the cancellation and the lost watch are told apart by core and not by the
// driver's spelling.
type racingPlane struct{ plainPlane }

func (racingPlane) Watching() (ferry.Notifier, error) { return racingPlane{}, nil }

func (racingPlane) Notify(ctx context.Context) (ferry.Change, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return racingChange{}, nil
}

type racingChange struct{}

func (racingChange) Wait(ctx context.Context) (bool, error) {
	<-ctx.Done()

	return true, nil
}

func (racingChange) Close() error { return nil }

// quietPlane ends its own watch: the first wait answers false with no error
// while the context is still live, which is a plane closing a watch for a
// reason it will not name.
type quietPlane struct{ plainPlane }

func (quietPlane) Watching() (ferry.Notifier, error) { return quietPlane{}, nil }

func (quietPlane) Notify(context.Context) (ferry.Change, error) { return quietChange{}, nil }

type quietChange struct{}

func (quietChange) Wait(context.Context) (bool, error) { return false, nil }

func (quietChange) Close() error { return nil }
