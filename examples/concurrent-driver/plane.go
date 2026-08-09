// Package concurrentdriver is a driver author's miniature of the concurrency
// model: a plane behind a network, a reader that declares how much overlap it
// tolerates, and a caller who decides how much to spend.
//
// The three parties are all here and each of them is three lines long. Read
// Reader.MaxConcurrent for the driver's half, the open in Plane.Bind for how
// the caller's budget reaches a driver that wants to spend it privately, and
// the example file for the caller's half.
//
// It is a teaching plane and not a driver. Its reads hold themselves open until
// as many are open as the plane was told to expect, which is a device that
// makes the example's output the same on every run and which nothing real would
// do. Use one of the modules under driver/ for anything real.
package concurrentdriver

import (
	"context"
	"sync"
	"time"

	"github.com/onhotpath/ferry"
)

// Plane is a fixed set of addresses behind a pretend network.
//
// Build one with New or NewSerial. The two differ in one thing: whether the
// instance an open hands back declares that it tolerates overlapping reads.
type Plane struct {
	values map[ferry.Path]ferry.Value

	// tolerates is whether the open instance implements ferry.Concurrent, and
	// bound is the number it declares when it does. A bound of zero or less is
	// how an instance says it imposes no limit of its own.
	tolerates bool
	bound     int

	// expect is how many reads the plane holds open before it answers any of
	// them, so that the overlap the example prints is the same on every run.
	expect int

	mu       sync.Mutex
	open     int
	peak     int
	budget   int
	rendevue chan struct{}
	once     sync.Once
}

// New returns a plane whose instances declare that they tolerate overlapping
// reads, with no bound of their own, so the caller's budget stands alone.
//
// expect is how many reads to hold open at once before answering, which is what
// keeps the example deterministic and is not something a real plane does.
func New(values map[ferry.Path]ferry.Value, expect int) *Plane {
	return &Plane{values: values, tolerates: true, expect: expect, rendevue: make(chan struct{})}
}

// NewSerial returns the same plane whose instances declare nothing. A load from
// one is walked one address at a time whatever the caller asked for.
func NewSerial(values map[ferry.Path]ferry.Value) *Plane {
	return &Plane{values: values, expect: 1, rendevue: make(chan struct{})}
}

// Bind is called once per binding and reaches no plane. The open below is
// called once per load, and it is where a driver reads the caller's budget: one
// number bounds core's own walk and the driver's private request parallelism
// alike, so a driver that batches sizes the batch with this.
func (p *Plane) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(ctx context.Context) (ferry.Reader, error) {
		p.mu.Lock()
		p.budget = ferry.ConcurrencyBudget(ctx)
		p.mu.Unlock()

		if p.tolerates {
			return Reader{reader{p: p}}, nil
		}

		return reader{p: p}, nil
	}, nil
}

// Peak is the largest number of reads this plane ever had open at once.
func (p *Plane) Peak() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.peak
}

// Budget is what the last open read off its context.
func (p *Plane) Budget() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.budget
}

// reader is one open instance. It declares nothing, so ferry walks it one
// address at a time.
type reader struct{ p *Plane }

// Get answers one address. The zero ferry.Value is absent, so an address the
// plane does not hold reports absence with no error.
func (r reader) Get(_ context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	r.p.arrive()
	defer r.p.leave()

	return r.p.values[addr.Path()], nil
}

// Reader is the same instance with the capability on it.
//
// This is the whole of the driver's half. Declaring it is a promise that
// everything this instance reaches is safe to use from several goroutines at
// once, which for this plane is a map nobody writes to.
type Reader struct{ reader }

// MaxConcurrent is this instance's own tolerance. Zero means it imposes no
// bound of its own and the caller's number stands alone; a real driver returns
// its pool size or its rate limit here.
func (r Reader) MaxConcurrent() int { return r.p.bound }

// arrive and leave are the teaching device, not the model: the plane holds
// every read open until as many are open as it was told to expect, so the peak
// the example prints does not depend on how the goroutines happened to be
// scheduled.
func (p *Plane) arrive() {
	p.mu.Lock()
	p.open++

	if p.open > p.peak {
		p.peak = p.open
	}

	enough := p.open >= p.expect
	p.mu.Unlock()

	if enough {
		p.once.Do(func() { close(p.rendevue) })
	}

	select {
	case <-p.rendevue:
	case <-time.After(time.Second):
	}
}

func (p *Plane) leave() {
	p.mu.Lock()
	p.open--
	p.mu.Unlock()
}
