package main

// Core's key-table helper, ported from proto/5-source-sink unchanged, plus the
// one variant this ticket has to price.
//
// ADR-0004: "Core hands a driver a key function with the static set already
// computed and checked, not a map[Path]string." The two tiers ADR-0003 names
// are two fields: a static table written once by NewKeys and never again, and
// a dynamic tier minted on demand and checked against everything already
// issued.
//
// Under a Load that binds every time, "everything already issued" means
// everything issued by THIS load, because the Keys value dies with the load.
// A caller-held binding is the first thing that makes that lifetime visible.

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

type KeyFunc func(Path) (string, error)

func bBuildKeyTable(a *AddressSet, name string, f KeyFunc) (map[Path]string, error) {
	out := make(map[Path]string, a.Len())
	seen := make(map[string]Path, a.Len())
	var illegal, clashes []string
	for _, p := range a.All() {
		k, err := f(p)
		if err != nil {
			illegal = append(illegal, fmt.Sprintf("%s: %v", p, err))
			continue
		}
		if prev, dup := seen[k]; dup {
			clashes = append(clashes, fmt.Sprintf("%q <- %s and %s", k, prev, p))
			continue
		}
		seen[k] = p
		out[p] = k
	}
	slices.Sort(illegal)
	slices.Sort(clashes)
	var errs []error
	if len(illegal) > 0 {
		errs = append(errs, fmt.Errorf("%s: cannot name: %s", name, strings.Join(illegal, "; ")))
	}
	if len(clashes) > 0 {
		errs = append(errs, fmt.Errorf("%s: key function is not injective: %s", name, strings.Join(clashes, "; ")))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// --- the shape ADR-0004 landed ----------------------------------------------

// Keys is ADR-0004's key function verbatim: both tiers live in the value Bind
// returns, so both live as long as the binding does.
type Keys struct {
	name   string
	f      KeyFunc
	static map[Path]string // immutable after NewKeys

	mu   sync.Mutex
	dyn  map[Path]string
	used map[string]Path

	minted int
}

func NewKeys(a *AddressSet, name string, f KeyFunc) (*Keys, error) {
	tab, err := bBuildKeyTable(a, name, f)
	if err != nil {
		return nil, err
	}
	used := make(map[string]Path, len(tab))
	for p, k := range tab {
		used[k] = p
	}
	return &Keys{name: name, f: f, static: tab, dyn: map[Path]string{}, used: used}, nil
}

func (k *Keys) Key(p Path) (string, error) {
	if s, ok := k.static[p]; ok {
		return s, nil
	}
	return k.mint(p)
}

func (k *Keys) mint(p Path) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if s, ok := k.dyn[p]; ok {
		return s, nil
	}
	s, err := k.f(p)
	if err != nil {
		return "", fmt.Errorf("%s: cannot name %s: %w", k.name, p, err)
	}
	if prev, dup := k.used[s]; dup {
		return "", fmt.Errorf("%s: key function is not injective: %q <- %s and %s", k.name, s, prev, p)
	}
	k.dyn[p], k.used[s] = s, p
	k.minted++
	return s, nil
}

// held reports what the value is carrying, which is the number B2 is about.
func (k *Keys) held() (static, dynamic int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.static), len(k.dyn)
}

// --- the variant: the static tier is the binding's, the minted set is the
// open's ---------------------------------------------------------------------

// BoundKeys is the immutable half. It is a pure function of (address set, key
// function), so it is safe to hold for the life of a process and safe to read
// from any number of goroutines with no lock.
type BoundKeys struct {
	name   string
	f      KeyFunc
	static map[Path]string
	used   map[string]Path
}

func NewBoundKeys(a *AddressSet, name string, f KeyFunc) (*BoundKeys, error) {
	tab, err := bBuildKeyTable(a, name, f)
	if err != nil {
		return nil, err
	}
	used := make(map[string]Path, len(tab))
	for p, k := range tab {
		used[k] = p
	}
	return &BoundKeys{name: name, f: f, static: tab, used: used}, nil
}

// Session is the mutable half, and it is created per open. Everything it mints
// is checked against the static table and against everything this session has
// already minted, which is exactly the set one write may not collide inside.
func (b *BoundKeys) Session() *KeySession {
	return &KeySession{b: b, dyn: map[Path]string{}, used: map[string]Path{}}
}

type KeySession struct {
	b    *BoundKeys
	mu   sync.Mutex
	dyn  map[Path]string
	used map[string]Path
}

func (s *KeySession) Key(p Path) (string, error) {
	if k, ok := s.b.static[p]; ok {
		return k, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if k, ok := s.dyn[p]; ok {
		return k, nil
	}
	k, err := s.b.f(p)
	if err != nil {
		return "", fmt.Errorf("%s: cannot name %s: %w", s.b.name, p, err)
	}
	if prev, dup := s.b.used[k]; dup {
		return "", fmt.Errorf("%s: key function is not injective: %q <- %s and %s", s.b.name, k, prev, p)
	}
	if prev, dup := s.used[k]; dup {
		return "", fmt.Errorf("%s: key function is not injective: %q <- %s and %s", s.b.name, k, prev, p)
	}
	s.dyn[p], s.used[k] = k, p
	return k, nil
}
