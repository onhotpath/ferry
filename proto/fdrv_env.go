package main

// Driver: environment variables, against the final contract. Source only,
// because ADR-0002 established that env has no honest Dump.
// Self-contained, so the line count in P16 is honest.

import (
	"context"
	"errors"
	"os"
	"strings"
)

type FEnv struct {
	Sep    string
	Lookup func(string) (string, bool)
}

func (s FEnv) Bind(a *AddressSet) (FOpenFunc, error) {
	keys, err := NewKeys(a, "env", s.key)
	if err != nil {
		return nil, err
	}
	look := s.Lookup
	if look == nil {
		look = os.LookupEnv
	}
	return func(context.Context) (FReader, error) {
		return fEnvReader{keys, look}, nil
	}, nil
}

// key transforms rather than rejects. ADR-0003: a driver that refuses to
// transform is not safer, only less useful, because NewKeys catches whatever
// the transform collapses.
func (s FEnv) key(p Path) (string, error) {
	var b strings.Builder
	sep := s.Sep
	if sep == "" {
		sep = "_"
	}
	for i, seg := range p.Segments() {
		if seg.Text == "" {
			return "", errors.New("empty segment has no environment variable name")
		}
		if i > 0 {
			b.WriteString(sep)
		}
		for _, r := range strings.ToUpper(seg.Text) {
			if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
				b.WriteRune(r)
			} else {
				b.WriteByte('_')
			}
		}
	}
	return b.String(), nil
}

type fEnvReader struct {
	keys *Keys
	look func(string) (string, bool)
}

func (r fEnvReader) Get(_ context.Context, p Path) (Value, error) {
	k, err := r.keys.Key(p)
	if err != nil {
		return Absent, err
	}
	if v, ok := r.look(k); ok {
		return String(v), nil // FOO= is present and empty
	}
	return Absent, nil
}

// Children makes env an enumerating source, so a map-typed field is loadable.
func (r fEnvReader) Children(_ context.Context, prefix Path) ([]Path, error) {
	pk, err := r.keys.Key(prefix)
	if err != nil {
		return nil, err
	}
	var out []Path
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		rest, ok := strings.CutPrefix(k, pk+"_")
		if !ok || rest == "" || strings.Contains(rest, "_") {
			continue
		}
		out = append(out, prefix.Name(strings.ToLower(rest)))
	}
	return out, nil
}
