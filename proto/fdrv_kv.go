package main

// Driver: a Consul-shaped remote KV, against the final contract.
// Source and Sink, as two types, because one type cannot have two Bind
// methods. Exercises real I/O, opaque bytes, batch-versus-lazy, the
// dynamically read-only sink, and Commit without Close.

import (
	"context"
	"errors"
	"strings"
)

func fKVKey(prefix string) KeyFunc {
	return func(p Path) (string, error) {
		var b strings.Builder
		b.WriteString(prefix)
		for i, seg := range p.Segments() {
			if strings.Contains(seg.Text, "/") {
				return "", errors.New("segment contains a slash")
			}
			if i > 0 {
				b.WriteByte('/')
			}
			b.WriteString(seg.Text)
		}
		return b.String(), nil
	}
}

// --- source -----------------------------------------------------------------

type FKVSource struct {
	KV     *fakeKV
	Prefix string
	Lazy   bool // per-address fetch instead of one snapshot per load
}

func (s FKVSource) Bind(a *AddressSet) (FOpenFunc, error) {
	keys, err := NewKeys(a, "kv", fKVKey(s.Prefix))
	if err != nil {
		return nil, err
	}
	return func(context.Context) (FReader, error) {
		r := &fKVReader{kv: s.KV, keys: keys}
		if !s.Lazy {
			// The address set was known at Bind, so one List serves
			// every address. Batch versus lazy is this one branch.
			r.snap = s.KV.List(s.Prefix)
		}
		return r, nil
	}, nil
}

type fKVReader struct {
	kv   *fakeKV
	keys *Keys
	snap map[string][]byte
}

func (r *fKVReader) Get(_ context.Context, p Path) (Value, error) {
	k, err := r.keys.Key(p)
	if err != nil {
		return Absent, err
	}
	var b []byte
	var ok bool
	if r.snap != nil {
		b, ok = r.snap[k]
	} else {
		b, ok = r.kv.Get(k)
	}
	if !ok {
		return Absent, nil
	}
	return Bytes(b), nil // opaque bytes: this plane gains nothing from typing
}

func (r *fKVReader) Children(_ context.Context, prefix Path) ([]Path, error) {
	pk, err := r.keys.Key(prefix)
	if err != nil {
		return nil, err
	}
	var out []Path
	for k := range r.kv.List(pk + "/") {
		rest := strings.TrimPrefix(k, pk+"/")
		if rest == "" || strings.Contains(rest, "/") {
			continue
		}
		out = append(out, prefix.Name(rest))
	}
	return out, nil
}

// --- sink -------------------------------------------------------------------

type FKVSink struct {
	KV     *fakeKV
	Prefix string
}

func (s FKVSink) Bind(a *AddressSet) (FOpenWriterFunc, error) {
	keys, err := NewKeys(a, "kv", fKVKey(s.Prefix))
	if err != nil {
		return nil, err
	}
	return func(context.Context) (FWriter, error) {
		// Writability is a fact about the plane now, not about the
		// schema, so it lands here and not at Bind.
		if s.KV.readOnly {
			return nil, ErrReadOnly
		}
		return &fKVWriter{s.KV, keys, map[string][]byte{}}, nil
	}, nil
}

type fKVWriter struct {
	kv   *fakeKV
	keys *Keys
	buf  map[string][]byte
}

func (w *fKVWriter) Set(_ context.Context, p Path, v Value) error {
	k, err := w.keys.Key(p) // may mint a dynamic address
	if err != nil {
		return err
	}
	w.buf[k] = []byte(v.Text())
	return nil
}

// Commit and no Close: there is no resource to release, and an uncommitted
// buffer needs no cleanup because it never reached the plane.
func (w *fKVWriter) Commit(context.Context) error { return w.kv.Txn(w.buf) }
