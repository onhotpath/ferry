package main

// Driver: HTTP query parameters. The per-request plane, and the driver ADR-0004
// says this contract serves least well.
//
// Three shapes of the same driver, so the ticket's options can be run against
// each other rather than described:
//
//	BQuery      ADR-0004 as written: the Source holds the plane, so a request
//	            constructs a Source and Bind runs per request.
//	BQueryCtx   the grammar only. The plane arrives at open time, out of the
//	            context, so one binding serves every request.
//	BQueryOpen  the same, over BoundKeys, so the minted set is the open's.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// bQueryKey is the flat join, and it is shared by every shape below so that no
// probe is comparing two different key functions by accident.
func bQueryKey(sep string) KeyFunc {
	return func(p Path) (string, error) {
		var b strings.Builder
		for i, seg := range p.Segments() {
			if strings.Contains(seg.Text, sep) {
				return "", fmt.Errorf("segment %q contains the separator %q", seg.Text, sep)
			}
			if i > 0 {
				b.WriteString(sep)
			}
			b.WriteString(seg.Text)
		}
		return b.String(), nil
	}
}

// --- (d): the Source holds the plane -----------------------------------------

type BQuery struct {
	Values url.Values
	Sep    string
}

func (s BQuery) sep() string {
	if s.Sep == "" {
		return "."
	}
	return s.Sep
}

func (s BQuery) Bind(a *AddressSet) (FOpenFunc, error) {
	keys, err := NewKeys(a, "query", bQueryKey(s.sep()))
	if err != nil {
		return nil, err
	}
	return func(context.Context) (FReader, error) {
		return bQueryReader{keys.Key, s.Values}, nil
	}, nil
}

type bQueryReader struct {
	key func(Path) (string, error)
	v   url.Values
}

func (r bQueryReader) Get(_ context.Context, p Path) (Value, error) {
	k, err := r.key(p)
	if err != nil {
		return Absent, err
	}
	if vs, ok := r.v[k]; ok && len(vs) > 0 {
		return String(vs[0]), nil // ?x= is present and empty, never absent
	}
	return Absent, nil
}

func (r bQueryReader) Children(_ context.Context, prefix Path) ([]Path, error) {
	pk, err := r.key(prefix)
	if err != nil {
		return nil, err
	}
	var out []Path
	for k := range r.v {
		if rest, ok := strings.CutPrefix(k, pk+"."); ok && !strings.Contains(rest, ".") {
			out = append(out, prefix.Name(rest))
		}
	}
	return sortedPaths(out), nil
}

// --- (a): the plane arrives in the context -----------------------------------

// bQueryCtxKey is the driver's own unexported context key, which is the shape
// context's own documentation asks for.
type bQueryCtxKey struct{}

// BQueryContext is what a handler calls. It is the driver's, not core's:
// core supplies no mechanism, and every driver with a per-request plane names
// its own.
func BQueryContext(ctx context.Context, v url.Values) context.Context {
	return context.WithValue(ctx, bQueryCtxKey{}, v)
}

// ErrNoPlane is the refusal a caller who forgot meets. It lands at open, which
// is where ADR-0004 already puts "the plane is not reachable".
var ErrNoPlane = errors.New("query: no values in the context")

type BQueryCtx struct{ Sep string }

func (s BQueryCtx) sep() string {
	if s.Sep == "" {
		return "."
	}
	return s.Sep
}

func (s BQueryCtx) Bind(a *AddressSet) (FOpenFunc, error) {
	keys, err := NewKeys(a, "query", bQueryKey(s.sep()))
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (FReader, error) {
		v, ok := ctx.Value(bQueryCtxKey{}).(url.Values)
		if !ok {
			return nil, ErrNoPlane
		}
		return bQueryReader{keys.Key, v}, nil
	}, nil
}

// BQueryOpen is BQueryCtx with the minted set moved from the binding to the
// open, which is the amendment B2 measures.
type BQueryOpen struct{ Sep string }

func (s BQueryOpen) sep() string {
	if s.Sep == "" {
		return "."
	}
	return s.Sep
}

func (s BQueryOpen) Bind(a *AddressSet) (FOpenFunc, error) {
	bound, err := NewBoundKeys(a, "query", bQueryKey(s.sep()))
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (FReader, error) {
		v, ok := ctx.Value(bQueryCtxKey{}).(url.Values)
		if !ok {
			return nil, ErrNoPlane
		}
		return bQueryReader{bound.Session().Key, v}, nil
	}, nil
}

// --- the env-shaped key function, which is where injectivity actually bites --

// bEnvKey is ADR-0004's env driver transform: upper-case, and every
// non-alphanumeric character becomes an underscore. It is not injective, which
// is exactly why ADR-0003 makes injectivity a driver obligation and ADR-0004
// puts the check in core.
func bEnvKey(p Path) (string, error) {
	var b strings.Builder
	for i, seg := range p.Segments() {
		if i > 0 {
			b.WriteByte('_')
		}
		for _, r := range seg.Text {
			switch {
			case r >= 'a' && r <= 'z':
				b.WriteRune(r - 32)
			case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
				b.WriteRune(r)
			default:
				b.WriteByte('_')
			}
		}
	}
	return b.String(), nil
}

// BEnvCtx and BEnvOpen are the same driver over bEnvKey, so B2d runs the
// dynamic tier through the whole entry point rather than through the helper.
// They read their plane from the same context key, because a probe comparing
// two BINDINGS must not also change the plane.
type BEnvCtx struct{ Seen *[]*Keys }

func (s BEnvCtx) Bind(a *AddressSet) (FOpenFunc, error) {
	keys, err := NewKeys(a, "env", bEnvKey)
	if err != nil {
		return nil, err
	}
	if s.Seen != nil {
		*s.Seen = append(*s.Seen, keys)
	}
	return func(ctx context.Context) (FReader, error) {
		v, ok := ctx.Value(bQueryCtxKey{}).(url.Values)
		if !ok {
			return nil, ErrNoPlane
		}
		return bEnvReader{keys.Key, v}, nil
	}, nil
}

type BEnvOpen struct{}

func (BEnvOpen) Bind(a *AddressSet) (FOpenFunc, error) {
	bound, err := NewBoundKeys(a, "env", bEnvKey)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (FReader, error) {
		v, ok := ctx.Value(bQueryCtxKey{}).(url.Values)
		if !ok {
			return nil, ErrNoPlane
		}
		return bEnvReader{bound.Session().Key, v}, nil
	}, nil
}

type bEnvReader struct {
	key func(Path) (string, error)
	v   url.Values
}

func (r bEnvReader) Get(_ context.Context, p Path) (Value, error) {
	k, err := r.key(p)
	if err != nil {
		return Absent, err
	}
	if vs, ok := r.v[k]; ok && len(vs) > 0 {
		return String(vs[0]), nil
	}
	return Absent, nil
}

// Children enumerates by undoing the transform the only way a flat plane can:
// it reports the raw remainder as a name segment, and the key function is then
// asked to name it again. That round trip is where the collision is minted.
func (r bEnvReader) Children(_ context.Context, prefix Path) ([]Path, error) {
	pk, err := r.key(prefix)
	if err != nil {
		return nil, err
	}
	var out []Path
	for k := range r.v {
		if rest, ok := strings.CutPrefix(k, pk+"_"); ok {
			out = append(out, prefix.Name(rest))
		}
	}
	return sortedPaths(out), nil
}
