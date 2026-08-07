package watcher

import (
	"context"
	"maps"
	"sync"

	"github.com/onhotpath/ferry"
)

// MemPlane is a mutable in-memory plane with a change signal: the miniature of
// any watchable driver, and enough of one to run the examples in this package.
//
// Build one with NewMemPlane, write to it with Set and Delete, and hand it to
// ferry.Bind as the source. Every open mints a snapshot of the contents, so a
// load in flight is unaffected by a write that lands beside it.
//
// It is a teaching plane and not a driver. It carries only flat addresses,
// because its reader implements Get and nothing else, which is all a struct of
// scalars needs; a map or a slice field wants an enumerator too. Use one of the
// modules under driver/ for anything real.
type MemPlane struct {
	mu   sync.Mutex
	data map[ferry.Path]ferry.Value
	subs []chan struct{}
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

// Set writes one address and signals every watcher: the "the plane changed
// underneath you" event a real driver receives from its backend.
func (p *MemPlane) Set(addr ferry.Path, v ferry.Value) {
	p.mu.Lock()
	p.data[addr] = v
	p.mu.Unlock()
	p.notify()
}

// Delete removes one address and signals every watcher. A required field whose
// address is deleted is what makes the next reload fail.
func (p *MemPlane) Delete(addr ferry.Path) {
	p.mu.Lock()
	delete(p.data, addr)
	p.mu.Unlock()
	p.notify()
}

// Changes hands a watcher its own signal channel, to pass to Watch.
//
// The channel is buffered by one and is never closed, so a write that lands
// while nobody is ranging is still delivered on the next turn of the loop, and
// a slow watcher coalesces signals rather than blocking the writer.
func (p *MemPlane) Changes() <-chan struct{} {
	ch := make(chan struct{}, 1)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.subs = append(p.subs, ch)
	return ch
}

func (p *MemPlane) notify() {
	p.mu.Lock()
	subs := p.subs
	p.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- struct{}{}:
		default: // already signalled and not yet drained: coalesce
		}
	}
}

// snapshot serves one load from an immutable copy of the contents. The zero
// ferry.Value is absent, so an address the plane does not hold reports absence
// with no error, which is exactly what the contract asks for.
type snapshot map[ferry.Path]ferry.Value

func (s snapshot) Get(_ context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	return s[addr.Path()], nil
}
