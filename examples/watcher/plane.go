package watcher

import (
	"context"
	"maps"
	"sync"

	"github.com/onhotpath/ferry"
)

// MemPlane is a mutable in-memory plane, and the thing the examples in this
// package reload from.
//
// Build one with [NewMemPlane], write to it with [MemPlane.Set] and
// [MemPlane.Delete], and hand it to [Kick] as the source. Every open mints a
// snapshot of the contents, so a load in flight is unaffected by a write that
// lands beside it.
//
// It announces nothing, which is why it needs a [Kick]: the change a stream
// waits for comes from the process rather than from here.
//
// It is a teaching plane and not a driver. It carries only flat addresses,
// because its reader implements Get and nothing else, which is all a struct of
// scalars needs; a map or a slice field wants an enumerator too. Use one of the
// modules under driver/ for anything real.
type MemPlane struct {
	mu   sync.Mutex
	data map[ferry.Path]ferry.Value
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
func (p *MemPlane) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(context.Context) (ferry.Reader, error) {
		p.mu.Lock()
		defer p.mu.Unlock()

		return snapshot(maps.Clone(p.data)), nil
	}, nil
}

// Set writes one address. Nothing is announced, so the next load is what sees
// it, and a stream sees it on the next kick.
func (p *MemPlane) Set(addr ferry.Path, v ferry.Value) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.data[addr] = v
}

// Delete removes one address. A required field whose address is deleted is what
// makes the next reload fail.
func (p *MemPlane) Delete(addr ferry.Path) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.data, addr)
}

// snapshot serves one load from an immutable copy of the contents. The zero
// ferry.Value is absent, so an address the plane does not hold reports absence
// with no error, which is exactly what the contract asks for.
type snapshot map[ferry.Path]ferry.Value

func (s snapshot) Get(_ context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	return s[addr.Path()], nil
}
