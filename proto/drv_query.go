package main

// Driver: HTTP query parameters. Source only.
//
// The per-request hot path, and the plane with no type information at all.
// It is here mainly to answer whether the two-phase contract can survive a
// per-request Open - see the benchmark.

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

type QuerySource struct{ Values url.Values }

func (s QuerySource) Bind(a *AddressSet) (Binding, error) {
	tab, err := KeyTable(a, "query", queryKey)
	if err != nil {
		return nil, err
	}
	return queryReader{tab, s.Values}, nil
}

func (r queryReader) Open(context.Context) (Reader, error) { return r, nil }

type queryReader struct {
	tab map[Path]string
	v   url.Values
}

func (r queryReader) Get(_ context.Context, addr Path) (Value, error) {
	k, ok := r.tab[addr]
	if !ok {
		return Absent, errors.New("query: address not in the opened set: " + addr.String())
	}
	vs, ok := r.v[k]
	if !ok || len(vs) == 0 {
		return Absent, nil
	}
	// ?x= is present and empty; ?x absent is absent. url.Values is one of
	// the few planes where the stdlib already draws that line for you.
	return String(vs[0]), nil
}

// QuerySourceB is drv_query under shape B: the key table is computed at Bind
// and the per-request url.Values arrives at Open. This is the shape that makes
// the per-request plane work without any cache.
type QuerySourceB struct{}

func (QuerySourceB) Bind(a *AddressSet) (Binding, error) {
	tab, err := buildKeyTable(a, "query", queryKey)
	if err != nil {
		return nil, err
	}
	return queryBinding{tab}, nil
}

type queryBinding struct{ tab map[Path]string }

func (b queryBinding) Open(context.Context) (Reader, error) {
	return queryReader{b.tab, nil}, nil
}

func (r queryReader) GetWith(v url.Values, addr Path) (Value, error) {
	k, ok := r.tab[addr]
	if !ok {
		return Absent, errors.New("query: address not in the bound set: " + addr.String())
	}
	vs, ok := v[k]
	if !ok || len(vs) == 0 {
		return Absent, nil
	}
	return String(vs[0]), nil
}

func queryKey(p Path) (string, error) {
	var b strings.Builder
	for i, seg := range p.Segments() {
		if strings.ContainsAny(seg.Text, "[]") {
			return "", errors.New("segment contains a bracket")
		}
		if i == 0 && seg.Kind == Name {
			b.WriteString(seg.Text)
			continue
		}
		b.WriteString("[" + seg.Text + "]")
	}
	return b.String(), nil
}
