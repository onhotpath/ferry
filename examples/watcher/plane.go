// Package watcher is a watchable plane written by hand, small enough to read in
// one sitting, and the ferry/watch helper running over it end to end.
//
// A watchable driver announces a change by calling a func(context.Context) that
// carries no payload: it says the plane may have changed and nothing more. What
// signals a change is the driver's business - fsnotify for a file, a watch plan
// for a key-value store, a write to a map here - and turning those calls into
// freshly loaded values is watch.Signal and watch.Values.
//
// MemPlane is the driver half of that, OnChange is the option a real driver
// spells WatchFiles or Watch, and the examples in this package are the two
// halves together.
package watcher

import (
	"context"
	"maps"
	"sync"

	"github.com/onhotpath/ferry"
)

// MemPlane is a mutable in-memory plane that announces its changes the way a
// watchable driver does: through a callback that carries no payload.
//
// Build one with NewMemPlane, write to it with Set and Delete, register a
// callback with OnChange, and hand it to ferry.Bind as the source. Every open
// mints a snapshot of the contents, so a load in flight is unaffected by a write
// that lands beside it.
//
// It is a teaching plane and not a driver. It carries only flat addresses,
// because its reader implements Get and nothing else, which is all a struct of
// scalars needs; a map or a slice field wants an enumerator too. Use one of the
// modules under driver/ for anything real.
type MemPlane struct {
	mu       sync.Mutex
	data     map[ferry.Path]ferry.Value
	onChange []func(context.Context)
}

// NewMemPlane returns an empty plane, ready to write to and to bind.
func NewMemPlane() *MemPlane {
	return &MemPlane{data: map[ferry.Path]ferry.Value{}}
}

// Bind precomputes nothing, because on this plane the address is the key, and
// hands back an open that mints one snapshot per call.
//
// It reaches no plane and returns no error, which is the whole obligation for a
// plane that cannot be unreachable and can name any address.
func (p *MemPlane) Bind(_ *ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(context.Context) (ferry.Reader, error) {
		p.mu.Lock()
		defer p.mu.Unlock()

		return snapshot(maps.Clone(p.data)), nil
	}, nil
}

// OnChange registers a callback to run whenever the plane changes, which is the
// shape every watch option in ferry's drivers takes.
//
// Pass the Changed method of a watch.Signal to it. Registering several callbacks
// calls all of them, and registering none is a plane nobody is watching.
func (p *MemPlane) OnChange(f func(context.Context)) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.onChange = append(p.onChange, f)
}

// Set writes one address and announces the change: the "the plane changed
// underneath you" event a real driver receives from its backend.
func (p *MemPlane) Set(addr ferry.Path, v ferry.Value) {
	p.mu.Lock()
	p.data[addr] = v
	p.mu.Unlock()

	p.announce()
}

// Delete removes one address and announces the change. A required field whose
// address is deleted is what makes the next reload fail.
func (p *MemPlane) Delete(addr ferry.Path) {
	p.mu.Lock()
	delete(p.data, addr)
	p.mu.Unlock()

	p.announce()
}

// announce calls every registered callback, on the goroutine that wrote.
//
// A real driver calls back from its own watching goroutine instead. Either way
// the call has to return quickly, which is what watch.Signal.Changed does.
func (p *MemPlane) announce() {
	p.mu.Lock()
	subs := p.onChange
	p.mu.Unlock()

	for _, f := range subs {
		f(context.Background())
	}
}

// snapshot serves one load from an immutable copy of the contents. The zero
// ferry.Value is absent, so an address the plane does not hold reports absence
// with no error, which is exactly what the contract asks for.
type snapshot map[ferry.Path]ferry.Value

func (s snapshot) Get(_ context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	return s[addr.Path()], nil
}
