//go:build protoc

package ferry_test

import (
	"context"
	"maps"
	"sync"

	"github.com/onhotpath/ferry"
)

// portPlane is a mutable in-memory plane that announces its changes through the
// port a handle minted for it, which is the seam every driver keeps under this
// variant: an Option takes the handle, the constructor wires it, and the
// watching code announces.
//
// Every open mints a snapshot of the contents, so a load in flight is unaffected
// by a write that lands beside it, and it counts its opens so that a test can
// prove a burst produced one reload.
type portPlane struct {
	mu    sync.Mutex
	data  map[ferry.Path]ferry.Value
	opens int

	port *ferry.WatchPort
}

// newPortPlane builds a plane nobody watches, which is what a source built
// without the watch option is.
func newPortPlane() *portPlane {
	return &portPlane{data: map[ferry.Path]ferry.Value{}}
}

// wire is what a driver constructor given the watch option does: it asks the
// handle for a port and keeps it.
func (p *portPlane) wire(h *ferry.Watch) *portPlane {
	p.port = h.Wire(p)

	return p
}

func (p *portPlane) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(context.Context) (ferry.Reader, error) {
		p.mu.Lock()
		defer p.mu.Unlock()

		p.opens++

		return portSnapshot(maps.Clone(p.data)), nil
	}, nil
}

// Refuse is what a driver does when it was handed a handle and has nothing to
// watch: it declines rather than starting a watch that would never fire.
func (p *portPlane) Refuse(err error) { p.port.Refuse(err) }

// Set writes one address and announces the change.
func (p *portPlane) Set(addr ferry.Path, v ferry.Value) {
	p.mu.Lock()
	p.data[addr] = v
	p.mu.Unlock()

	p.announce()
}

// Delete removes one address and announces the change.
func (p *portPlane) Delete(addr ferry.Path) {
	p.mu.Lock()
	delete(p.data, addr)
	p.mu.Unlock()

	p.announce()
}

// Lose ends the watch the way a plane that cannot keep it does, and it is the
// call the shipped drivers have nowhere to make.
func (p *portPlane) Lose(err error) { p.port.Ended(err) }

// Opens reports how many times the plane has been opened, which is once per
// load.
func (p *portPlane) Opens() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.opens
}

func (p *portPlane) announce() {
	if p.port != nil {
		p.port.Changed()
	}
}

// portSnapshot serves one load from an immutable copy of the contents.
type portSnapshot map[ferry.Path]ferry.Value

func (s portSnapshot) Get(_ context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	return s[addr.Path()], nil
}

// layeredPlane is two watchable planes under one source, both announcing into
// one handle.
//
// It is #361 written out. The composite wires itself, because that is what
// [ferry.BindWatched] checks, and it announces nothing: its members do.
type layeredPlane struct {
	over  *portPlane
	under *portPlane
}

func newLayeredPlane() *layeredPlane {
	return &layeredPlane{over: newPortPlane(), under: newPortPlane()}
}

// wire wires every layer and the composite itself, which is what
// [ferry.BindWatched] checks.
func (l *layeredPlane) wire(h *ferry.Watch) {
	l.over.wire(h)
	l.under.wire(h)
	h.Wire(l)
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
