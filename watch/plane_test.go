package watch_test

import (
	"context"
	"maps"
	"sync"

	"github.com/onhotpath/ferry"
)

// memPlane is a mutable in-memory plane that announces its changes the way a
// watchable driver does: through a callback that carries no payload.
//
// It is the miniature of driver/env's WatchFiles and driver/yaml's Watch, small
// enough to read in one sitting. Every open mints a snapshot of the contents, so
// a load in flight is unaffected by a write that lands beside it, and it counts
// its opens so that a test can prove a burst produced one reload.
type memPlane struct {
	mu       sync.Mutex
	data     map[ferry.Path]ferry.Value
	onChange []func(context.Context)
	opens    int
}

func newMemPlane() *memPlane {
	return &memPlane{data: map[ferry.Path]ferry.Value{}}
}

// Bind precomputes nothing, because on this plane the address is the key, and
// hands back an open that mints one snapshot per call.
func (p *memPlane) Bind(_ *ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(context.Context) (ferry.Reader, error) {
		p.mu.Lock()
		defer p.mu.Unlock()

		p.opens++

		return snapshot(maps.Clone(p.data)), nil
	}, nil
}

// OnChange registers a callback, which is what a driver's watch option takes.
// Every registered callback is called on every write.
func (p *memPlane) OnChange(f func(context.Context)) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.onChange = append(p.onChange, f)
}

// Set writes one address and announces the change.
func (p *memPlane) Set(addr ferry.Path, v ferry.Value) {
	p.mu.Lock()
	p.data[addr] = v
	p.mu.Unlock()

	p.announce()
}

// Delete removes one address and announces the change. A required field whose
// address is deleted is what makes the next reload fail.
func (p *memPlane) Delete(addr ferry.Path) {
	p.mu.Lock()
	delete(p.data, addr)
	p.mu.Unlock()

	p.announce()
}

// Opens reports how many times the plane has been opened, which is once per
// load.
func (p *memPlane) Opens() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.opens
}

func (p *memPlane) announce() {
	p.mu.Lock()
	subs := p.onChange
	p.mu.Unlock()

	for _, f := range subs {
		f(context.Background())
	}
}

// snapshot serves one load from an immutable copy of the contents. The zero
// ferry.Value is absent, so an address the plane does not hold reports absence
// with no error, which is what the contract asks for.
type snapshot map[ferry.Path]ferry.Value

func (s snapshot) Get(_ context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	return s[addr.Path()], nil
}
