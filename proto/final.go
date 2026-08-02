package main

// The contract this prototype ends at, after P13, P14 and P15.
//
// Four interfaces, one required method each. Three optional interfaces
// discovered by assertion. Two named func types for the phase that is a phase
// rather than a contract.

import "context"

// ---------------------------------------------------------------------------
// Required
// ---------------------------------------------------------------------------

type FSource interface {
	// Bind sees the address set and does no I/O. Legality and injectivity
	// are checked here, which is what ADR-0003 requires and what a
	// conformance case can assert by binding against an unreachable plane.
	//
	// The returned OpenFunc is reusable: bind once per schema, open once
	// per load.
	Bind(*AddressSet) (FOpenFunc, error)
}

type FSink interface {
	Bind(*AddressSet) (FOpenWriterFunc, error)
}

// FOpenFunc is a function and not an interface because nothing ever asks an
// opener a second question and nothing ever type-asserts one. It is named so
// the phase contract has a doc site. Precedent: context.CancelFunc.
type FOpenFunc func(context.Context) (FReader, error)

type FOpenWriterFunc func(context.Context) (FWriter, error)

// FReader stays an interface: it is the thing optional capabilities attach to.
type FReader interface {
	Get(context.Context, Path) (Value, error)
}

type FWriter interface {
	Set(context.Context, Path, Value) error
}

// ---------------------------------------------------------------------------
// Optional, discovered by assertion
// ---------------------------------------------------------------------------

// FReleaser is io.Closer. A reader or a writer holding a resource for the
// duration of one load implements it; most do not.
type FReleaser interface{ Close() error }

// FCommitter is implemented by a sink that stages. ferry calls Commit only
// when the walk succeeded, and Close always. Closed-without-Commit is the
// abort signal, so no sink is ever told that it failed.
type FCommitter interface {
	Commit(ctx context.Context) error
}

// FEnumerator is implemented by a reader whose plane can list what it holds
// under an address. Without it a map-typed field is a loud error on Load.
type FEnumerator interface {
	Children(ctx context.Context, prefix Path) ([]Path, error)
}

// ---------------------------------------------------------------------------
// The lifecycle, which is core's and not a driver's
// ---------------------------------------------------------------------------

func fLoad(ctx context.Context, open FOpenFunc, addrs *AddressSet) (map[Path]Value, error) {
	r, err := open(ctx)
	if err != nil {
		return nil, err
	}
	if rel, ok := r.(FReleaser); ok {
		defer rel.Close()
	}
	out := make(map[Path]Value, addrs.Len())
	for _, p := range addrs.All() {
		v, err := r.Get(ctx, p)
		if err != nil {
			return nil, err
		}
		out[p] = v // Absent is kind zero, so a miss records as a miss
	}
	return out, nil
}

func fDump(ctx context.Context, open FOpenWriterFunc, vals map[Path]Value, addrs *AddressSet) (err error) {
	w, err := open(ctx)
	if err != nil {
		return err
	}
	if rel, ok := w.(FReleaser); ok {
		defer func() { err = joinErr(err, rel.Close()) }()
	}
	for _, p := range addrs.All() {
		if err := w.Set(ctx, p, vals[p]); err != nil {
			return err
		}
	}
	if c, ok := w.(FCommitter); ok {
		return c.Commit(ctx)
	}
	return nil
}

func joinErr(a, b error) error {
	if a == nil {
		return b
	}
	return a
}
