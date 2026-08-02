package main

// Driver: environment variables. Source only.
//
// ADR-0002 already ruled that env has no honest Dump, so this driver is the
// case that decides whether Source and Sink are one interface or two: if they
// are one, this file has to implement a lie.
//
// It is also the "knows nothing about the type" case from section 4's premise
// check - every value it can produce is a VString.

import (
	"context"
	"errors"
	"os"
	"strings"
)

type EnvSource struct {
	Sep    string                      // segment join, "_" or "__"
	Lookup func(string) (string, bool) // nil means os.LookupEnv
}

func (s EnvSource) Bind(a *AddressSet) (Binding, error) {
	tab, err := KeyTable(a, "env", func(p Path) (string, error) {
		var b strings.Builder
		for i, seg := range p.Segments() {
			if seg.Text == "" {
				return "", errors.New("empty segment has no environment variable name")
			}
			if i > 0 {
				b.WriteString(s.sep())
			}
			b.WriteString(envSafe(seg.Text))
		}
		return b.String(), nil
	})
	if err != nil {
		return nil, err
	}
	look := s.Lookup
	if look == nil {
		look = os.LookupEnv
	}
	return envReader{tab, look}, nil
}

// env has nothing to fetch, so its Binding is its Reader. A driver whose
// plane needs no snapshot pays nothing for the second phase.
func (r envReader) Open(context.Context) (Reader, error) { return r, nil }

func (s EnvSource) sep() string {
	if s.Sep == "" {
		return "_"
	}
	return s.Sep
}

// envSafe transforms rather than rejects. ADR-0003: a driver that refuses to
// transform is not safer than one that does, only less useful, because the
// injectivity check in KeyTable catches what the transform collapses.
func envSafe(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

type envReader struct {
	tab  map[Path]string
	look func(string) (string, bool)
}

func (r envReader) Get(_ context.Context, addr Path) (Value, error) {
	k, ok := r.tab[addr]
	if !ok {
		return Absent, errors.New("env: address not in the set this reader was opened with: " + addr.String())
	}
	s, ok := r.look(k)
	if !ok {
		return Absent, nil
	}
	// FOO= is present and empty. That is the whole of 5.1.
	return String(s), nil
}
