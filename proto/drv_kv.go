package main

// Driver: a Consul-shaped remote KV. Source and Sink.
//
// The plane with real round trips, so it is where 5.13 gets measured. It is
// also the "opaque bytes, no type information" case, and the one that shows
// that batch versus lazy is a choice *inside* one driver rather than a second
// interface for ferry to define.

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// fakeKV stands in for the network. Every method bumps a counter.
type fakeKV struct {
	data     map[string][]byte
	gets     int
	lists    int
	puts     int
	txns     int
	readOnly bool
}

func newKV(pairs map[string]string) *fakeKV {
	d := map[string][]byte{}
	for k, v := range pairs {
		d[k] = []byte(v)
	}
	return &fakeKV{data: d}
}

func (k *fakeKV) Get(key string) ([]byte, bool) { k.gets++; v, ok := k.data[key]; return v, ok }

func (k *fakeKV) List(prefix string) map[string][]byte {
	k.lists++
	out := map[string][]byte{}
	for key, v := range k.data {
		if strings.HasPrefix(key, prefix) {
			out[key] = v
		}
	}
	return out
}

func (k *fakeKV) Txn(w map[string][]byte) error {
	if k.readOnly {
		return errors.New("permission denied")
	}
	k.txns++
	for key, v := range w {
		k.puts++
		k.data[key] = v
	}
	return nil
}

func (k *fakeKV) calls() int { return k.gets + k.lists + k.txns }

// ---------------------------------------------------------------------------
// Source
// ---------------------------------------------------------------------------

type KVSource struct {
	KV     *fakeKV
	Prefix string
	Lazy   bool // fetch per Get instead of once in Open
}

func kvKey(prefix string) KeyFunc {
	return func(p Path) (string, error) {
		var b strings.Builder
		b.WriteString(prefix)
		for i, seg := range p.Segments() {
			if i > 0 {
				b.WriteByte('/')
			}
			if strings.Contains(seg.Text, "/") {
				return "", errors.New("segment contains a slash")
			}
			b.WriteString(seg.Text)
		}
		return b.String(), nil
	}
}

func (s KVSource) Bind(a *AddressSet) (Binding, error) {
	tab, err := KeyTable(a, "kv", kvKey(s.Prefix))
	if err != nil {
		return nil, err
	}
	return kvBinding{s, tab}, nil
}

type kvBinding struct {
	s   KVSource
	tab map[Path]string
}

func (b kvBinding) Open(context.Context) (Reader, error) {
	r := &kvReader{kv: b.s.KV, tab: b.tab}
	if !b.s.Lazy {
		// The whole address set was known at Bind, so one List serves
		// every address. This is the collapse 5.13 identifies, and it
		// needs no interface beyond the two the contract already has.
		r.snap = b.s.KV.List(commonPrefix(b.tab))
	}
	return r, nil
}

func commonPrefix(tab map[Path]string) string {
	keys := make([]string, 0, len(tab))
	for _, k := range tab {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	a, b := keys[0], keys[len(keys)-1]
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return a[:i]
}

type kvReader struct {
	kv   *fakeKV
	tab  map[Path]string
	snap map[string][]byte
}

func (r *kvReader) Get(_ context.Context, addr Path) (Value, error) {
	k, ok := r.tab[addr]
	if !ok {
		return Absent, errors.New("kv: address not in the opened set: " + addr.String())
	}
	var b []byte
	if r.snap != nil {
		b, ok = r.snap[k]
	} else {
		b, ok = r.kv.Get(k)
	}
	if !ok {
		return Absent, nil
	}
	// Opaque bytes. This plane gains nothing from a typed boundary, which
	// section 4's premise check says out loud and this driver demonstrates.
	return Bytes(b), nil
}

// ---------------------------------------------------------------------------
// Sink
// ---------------------------------------------------------------------------

type KVSink struct {
	KV     *fakeKV
	Prefix string
}

func (s KVSink) Bind(a *AddressSet) (WriteBinding, error) {
	tab, err := KeyTable(a, "kv", kvKey(s.Prefix))
	if err != nil {
		return nil, err
	}
	return kvWriteBinding{s, tab}, nil
}

type kvWriteBinding struct {
	s   KVSink
	tab map[Path]string
}

func (b kvWriteBinding) Open(context.Context) (Writer, error) {
	// Writability is a fact about the plane now, not about the schema, so
	// it belongs at Open rather than at Bind.
	if b.s.KV.readOnly {
		return nil, ErrReadOnly
	}
	return &kvWriter{kv: b.s.KV, tab: b.tab, buf: map[string][]byte{}}, nil
}

type kvWriter struct {
	kv  *fakeKV
	tab map[Path]string
	buf map[string][]byte
}

func (w *kvWriter) Set(_ context.Context, addr Path, v Value) error {
	k, ok := w.tab[addr]
	if !ok {
		return errors.New("kv: address not in the opened set: " + addr.String())
	}
	w.buf[k] = []byte(v.Text())
	return nil
}

func (w *kvWriter) Commit(_ context.Context) error { return w.kv.Txn(w.buf) }
func (w *kvWriter) Abort()                         { clear(w.buf) }
