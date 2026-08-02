package main

// Driver: HTTP query parameters, against the final contract. Source only.
// The per-request plane, and the one with no type information at all.

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

type FQuery struct{ Values url.Values }

func (s FQuery) Bind(a *AddressSet) (FOpenFunc, error) {
	keys, err := NewKeys(a, "query", fQueryKey)
	if err != nil {
		return nil, err
	}
	return func(context.Context) (FReader, error) {
		return fQueryReader{keys, s.Values}, nil
	}, nil
}

func fQueryKey(p Path) (string, error) {
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

type fQueryReader struct {
	keys *Keys
	v    url.Values
}

func (r fQueryReader) Get(_ context.Context, p Path) (Value, error) {
	k, err := r.keys.Key(p)
	if err != nil {
		return Absent, err
	}
	if vs, ok := r.v[k]; ok && len(vs) > 0 {
		return String(vs[0]), nil // ?x= is present and empty
	}
	return Absent, nil
}
