//go:build protoa

package ferry_test

import (
	"context"
	"maps"
	"sync"

	"github.com/onhotpath/ferry"
)

// armedPlane is a mutable in-memory plane that announces its changes through the
// armed-once seam variant A promotes to core.
//
// It is the miniature of winreg's RegNotifyChangeKeyValue rather than of an
// fsnotify queue, deliberately: the armed-once mechanism is the weaker of the
// two, so a loop that is correct against this one is correct against a queue.
// Every open mints a snapshot, so a load in flight is unaffected by a write
// beside it, and it counts its opens so a test can prove a burst was one reload.
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

func (p *armedPlane) Notify(context.Context) (ferry.Change, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.armFail != nil {
		return nil, p.armFail
	}

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

// Opens reports how many times the plane has been opened, which is once per
// load.
func (p *armedPlane) Opens() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.opens
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

func (c *armedChange) Wait(ctx context.Context) (bool, error) {
	for {
		bell, done, err := c.look()
		if done || err != nil {
			return done, err
		}

		select {
		case <-ctx.Done():
			return false, nil
		case <-bell:
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

func (c *armedChange) Close() error { return nil }

// armedSnapshot serves one load from an immutable copy of the contents.
type armedSnapshot map[ferry.Path]ferry.Value

func (s armedSnapshot) Get(_ context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	return s[addr.Path()], nil
}

// plainPlane is a source with no watch at all, which is what [Binding.Watch]
// has to refuse.
type plainPlane struct{}

func (plainPlane) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(context.Context) (ferry.Reader, error) { return armedSnapshot(nil), nil }, nil
}

// layeredPlane is two watchable planes under one source: the first that holds an
// address answers, and a change on either is a change on the whole.
//
// It is #361 written out. Nothing in core composes sources, so this is the
// caller's own type, and what variant A gives it is one interface to implement.
type layeredPlane struct {
	over  *armedPlane
	under *armedPlane
}

func (l *layeredPlane) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	overOpen, err := l.over.Bind(addrs)
	if err != nil {
		return nil, err
	}

	underOpen, err := l.under.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		o, err := overOpen(ctx)
		if err != nil {
			return nil, err
		}

		u, err := underOpen(ctx)
		if err != nil {
			return nil, err
		}

		return layeredReader{over: o, under: u}, nil
	}, nil
}

// Notify arms both planes and answers with a registration that reports the
// first of the two to fire.
func (l *layeredPlane) Notify(ctx context.Context) (ferry.Change, error) {
	o, err := l.over.Notify(ctx)
	if err != nil {
		return nil, err
	}

	u, err := l.under.Notify(ctx)
	if err != nil {
		_ = o.Close()

		return nil, err
	}

	return &layeredChange{over: o, under: u}, nil
}

type layeredReader struct {
	over  ferry.Reader
	under ferry.Reader
}

func (r layeredReader) Get(ctx context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	v, err := r.over.Get(ctx, addr)
	if err != nil || v.Kind() != ferry.KindAbsent {
		return v, err
	}

	return r.under.Get(ctx, addr)
}

// layeredChange waits on both registrations and answers with the first to
// speak, which is the fan-in every composed watch has to write for itself.
type layeredChange struct {
	over  ferry.Change
	under ferry.Change
}

type changeResult struct {
	ok  bool
	err error
}

func (c *layeredChange) Wait(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan changeResult, 2)

	for _, ch := range []ferry.Change{c.over, c.under} {
		go func() {
			ok, err := ch.Wait(ctx)
			results <- changeResult{ok: ok, err: err}
		}()
	}

	first := <-results
	cancel()
	<-results

	return first.ok, first.err
}

func (c *layeredChange) Close() error {
	_ = c.over.Close()

	return c.under.Close()
}
