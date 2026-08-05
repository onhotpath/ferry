// Package watcher is session 05's A0 evidence: a watcher built ENTIRELY
// from ferry's shipped, exported surface - Bind once, reload per signal,
// publish a fresh value. Core is not modified, not forked, and not asked
// for anything it does not already export.
//
// The change signal is the driver's own business (fsnotify for a file,
// a watch plan for Consul, a channel here) - exactly the split ADR-0001
// drew: core defines the behaviour, drivers own the notification.
package watcher

import (
	"context"
	"maps"
	"sync"

	"github.com/onhotpath/ferry"
)

// MemSource is a mutable in-memory plane with a change signal - the
// miniature of any watchable driver.
type MemSource struct {
	mu   sync.Mutex
	data map[ferry.Path]ferry.Value
	subs []chan struct{}
}

func NewMemSource() *MemSource {
	return &MemSource{data: map[ferry.Path]ferry.Value{}}
}

// Bind precomputes nothing (paths are the keys) and mints a snapshot
// reader per open - the concurrent-open obligation satisfied by design.
func (s *MemSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(ctx context.Context) (ferry.Reader, error) {
		s.mu.Lock()
		snap := snapshot(maps.Clone(s.data))
		s.mu.Unlock()
		return snap, nil
	}, nil
}

// Set writes one address and notifies watchers - the "plane changed
// underneath you" event.
func (s *MemSource) Set(addr ferry.Path, v ferry.Value) {
	s.mu.Lock()
	s.data[addr] = v
	subs := s.subs
	s.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- struct{}{}:
		default: // a slow watcher coalesces signals rather than blocking the plane
		}
	}
}

// Delete removes one address and notifies watchers.
func (s *MemSource) Delete(addr ferry.Path) {
	s.mu.Lock()
	delete(s.data, addr)
	subs := s.subs
	s.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Changes hands a watcher its signal channel.
func (s *MemSource) Changes() <-chan struct{} {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.subs = append(s.subs, ch)
	s.mu.Unlock()
	return ch
}

// snapshot serves one walk from an immutable copy; the zero Value is
// KindAbsent, so a missing address reports absence with no error.
type snapshot map[ferry.Path]ferry.Value

func (r snapshot) Get(ctx context.Context, addr ferry.Path) (ferry.Value, error) {
	return r[addr], nil
}

// Children enumerates one level under prefix - what makes maps loadable.
func (r snapshot) Children(ctx context.Context, prefix ferry.Path) ([]ferry.Path, error) {
	seen := map[ferry.Path]bool{}
	var out []ferry.Path
	plen := segCount(prefix)
	for p := range r {
		if segCount(p) <= plen || !underPrefix(p, prefix) {
			continue
		}
		child := truncate(p, plen+1)
		if !seen[child] {
			seen[child] = true
			out = append(out, child)
		}
	}
	return out, nil
}

func segCount(p ferry.Path) int {
	n := 0
	for range p.Segments() {
		n++
	}
	return n
}

func underPrefix(p, prefix ferry.Path) bool {
	return truncate(p, segCount(prefix)) == prefix
}

// truncate rebuilds the first n segments as a fresh Path.
func truncate(p ferry.Path, n int) ferry.Path {
	var out ferry.Path
	i := 0
	for seg := range p.Segments() {
		if i >= n {
			break
		}
		if seg.Kind() == ferry.Index {
			out = out.Elem(uint(indexOf(seg.Text())))
		} else {
			out = out.At(seg.Text())
		}
		i++
	}
	return out
}

func indexOf(text string) int { // digits only: Index segments render as digits
	n := 0
	for _, c := range text {
		n = n*10 + int(c-'0')
	}
	return n
}
