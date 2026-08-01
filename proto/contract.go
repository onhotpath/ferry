package main

// The candidate source and sink contract.
//
// The shape is forced, not chosen. ADR-0003 requires that a driver be handed
// the whole address set before any I/O, because its injectivity obligation is
// unrunnable one key at a time. That kills xload's Load(ctx, key) outright.
//
// What remains is where the seam falls, and P9 is the probe that decided it:
// the address set and the plane data have different lifetimes, so they get
// different phases. Bind sees the address set and does no I/O; Open does the
// I/O; Get reads one address.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// AddressSet is the compiled schema's address set, handed over before I/O.
// It is passed by pointer and is the *same* pointer for a given compiled
// schema, which is what lets core memoise a driver's key table on it.
type AddressSet struct {
	addrs []Path // segment-wise sorted, prefix-free
}

func NewAddressSet(addrs []Path) *AddressSet {
	return &AddressSet{addrs: sortedPaths(addrs)}
}

func (a *AddressSet) All() []Path { return a.addrs }
func (a *AddressSet) Len() int    { return len(a.addrs) }

// ---------------------------------------------------------------------------
// Load side
// ---------------------------------------------------------------------------

type Source interface {
	// Bind is handed the complete address set, and takes no context
	// because it does no I/O. It is where a driver checks legality and
	// injectivity and precomputes its plane keys, which is exactly what
	// ADR-0003 requires to happen before any I/O.
	//
	// The returned Binding is reusable: bind once per schema, open once
	// per load. That is what makes precomputation structural rather than
	// a cache someone has to key correctly - see P9.
	Bind(addrs *AddressSet) (Binding, error)
}

type Binding interface {
	// Open is the I/O. A driver may fetch everything it needs here in one
	// round trip, or fetch nothing and be lazy in Get. That choice is the
	// driver's, which is why there is no separate "batch" interface to
	// upgrade to.
	Open(ctx context.Context) (Reader, error)
}

type Reader interface {
	// Get is the ordinary Go two-return shape. Absence is not a second
	// return value, it is Value.Kind() == VAbsent, so it cannot be dropped
	// with _ and it survives being stored in a map. See P2 and P11.
	Get(ctx context.Context, addr Path) (Value, error)
}

// ---------------------------------------------------------------------------
// Dump side
// ---------------------------------------------------------------------------

type Sink interface {
	Bind(addrs *AddressSet) (WriteBinding, error)
}

type WriteBinding interface {
	// Open is the read-only refusal point for a plane that is writable in
	// principle but not right now. A plane that can never be written does
	// not implement Sink at all, and that refusal is a compile error.
	Open(ctx context.Context) (Writer, error)
}

type Writer interface {
	Set(ctx context.Context, addr Path, v Value) error
	// Commit is the point a whole-document plane serialises. Abort exists
	// because without it a temp-file sink leaks on a mid-dump failure -
	// see P7.
	Commit(ctx context.Context) error
	Abort()
}

// ErrReadOnly is what a conditionally-writable plane returns from Sink.Open.
var ErrReadOnly = errors.New("plane is read only")

// ---------------------------------------------------------------------------
// The key table: core's, not the driver's
// ---------------------------------------------------------------------------

// KeyFunc is a driver's mapping from a ferry address to a plane key.
// Returning an error is the legality refusal ("this plane cannot name this
// address at all"); returning a colliding key is caught by KeyTable.
type KeyFunc func(Path) (string, error)

// KeyTable precomputes a driver's plane keys and runs ADR-0003's legality and
// injectivity checks over the result. It is a pure function: Bind calls it
// once, and the Binding holds the table, so there is no cache to key and no
// identity for a driver author to get wrong (P9).
//
// It lives in core rather than in each driver on ADR-0002's route (b): the
// injectivity rule is core's obligation, so the thing that discharges it ships
// from the same place as the rule.
func KeyTable(a *AddressSet, name string, f KeyFunc) (map[Path]string, error) {
	return buildKeyTable(a, name, f)
}

func buildKeyTable(a *AddressSet, name string, f KeyFunc) (map[Path]string, error) {
	out := make(map[Path]string, len(a.addrs))
	seen := make(map[string]Path, len(a.addrs))
	var illegal, clashes []string
	for _, p := range a.addrs {
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
	// Determinism is a package-wide invariant (ADR-0001), so both reports
	// are sorted before they are joined.
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

// bindOpen is the probe-side convenience for "bind then open in one go", which
// is what a one-shot ferry.Load would do internally.
func bindOpen(ctx context.Context, s Source, a *AddressSet) (Reader, error) {
	b, err := s.Bind(a)
	if err != nil {
		return nil, err
	}
	return b.Open(ctx)
}

func bindOpenSink(ctx context.Context, s Sink, a *AddressSet) (Writer, error) {
	b, err := s.Bind(a)
	if err != nil {
		return nil, err
	}
	return b.Open(ctx)
}
