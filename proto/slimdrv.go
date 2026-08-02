package main

// The same four drivers against the slim contract, written the same way, so
// the line counts in P13 are a like-for-like comparison rather than a claim.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// --- env: source only -------------------------------------------------------

type SlimEnv struct {
	Sep    string
	Lookup func(string) (string, bool)
}

func (s SlimEnv) Bind(a *AddressSet) (SlimOpen, error) {
	keys, err := NewKeys(a, "env", func(p Path) (string, error) {
		var b strings.Builder
		for i, seg := range p.Segments() {
			if seg.Text == "" {
				return "", errors.New("empty segment has no environment variable name")
			}
			if i > 0 {
				b.WriteString(cmpOr(s.Sep, "_"))
			}
			b.WriteString(envSafe(seg.Text))
		}
		return b.String(), nil
	})
	if err != nil {
		return nil, err
	}
	look := cmpOrFn(s.Lookup, os.LookupEnv)
	return func(context.Context) (SlimReader, error) {
		return SlimReaderFunc(func(_ context.Context, p Path) (Value, error) {
			k, err := keys.Key(p)
			if err != nil {
				return Absent, err
			}
			if v, ok := look(k); ok {
				return String(v), nil
			}
			return Absent, nil
		}), nil
	}, nil
}

// --- query params: source only ----------------------------------------------

type SlimQuery struct{ Values url.Values }

func (s SlimQuery) Bind(a *AddressSet) (SlimOpen, error) {
	keys, err := NewKeys(a, "query", queryKey)
	if err != nil {
		return nil, err
	}
	return func(context.Context) (SlimReader, error) {
		return SlimReaderFunc(func(_ context.Context, p Path) (Value, error) {
			k, err := keys.Key(p)
			if err != nil {
				return Absent, err
			}
			if vs, ok := s.Values[k]; ok && len(vs) > 0 {
				return String(vs[0]), nil
			}
			return Absent, nil
		}), nil
	}, nil
}

// --- kv: source and sink ----------------------------------------------------

type SlimKV struct {
	KV     *fakeKV
	Prefix string
	Lazy   bool
}

func (s SlimKV) Bind(a *AddressSet) (SlimOpen, error) {
	keys, err := NewKeys(a, "kv", kvKey(s.Prefix))
	if err != nil {
		return nil, err
	}
	return func(context.Context) (SlimReader, error) {
		var snap map[string][]byte
		if !s.Lazy {
			snap = s.KV.List(s.Prefix)
		}
		return SlimReaderFunc(func(_ context.Context, p Path) (Value, error) {
			k, err := keys.Key(p)
			if err != nil {
				return Absent, err
			}
			var b []byte
			var ok bool
			if snap != nil {
				b, ok = snap[k]
			} else {
				b, ok = s.KV.Get(k)
			}
			if !ok {
				return Absent, nil
			}
			return Bytes(b), nil
		}), nil
	}, nil
}

func (s SlimKV) BindSink(a *AddressSet) (SlimOpenWriter, error) {
	keys, err := NewKeys(a, "kv", kvKey(s.Prefix))
	if err != nil {
		return nil, err
	}
	return func(context.Context) (SlimWriter, error) {
		if s.KV.readOnly {
			return nil, errSlimReadOnly
		}
		return &slimKVWriter{s.KV, keys, map[string][]byte{}}, nil
	}, nil
}

type slimKVWriter struct {
	kv   *fakeKV
	keys *Keys
	buf  map[string][]byte
}

func (w *slimKVWriter) Set(_ context.Context, p Path, v Value) error {
	k, err := w.keys.Key(p)
	if err != nil {
		return err
	}
	w.buf[k] = []byte(v.Text())
	return nil
}

func (w *slimKVWriter) Close(_ context.Context, cause error) error {
	if cause != nil {
		return nil // nothing was written; the buffer is dropped
	}
	return w.kv.Txn(w.buf)
}

// --- yaml: source and sink --------------------------------------------------

type SlimYAML struct{ Path string }

func (s SlimYAML) Bind(*AddressSet) (SlimOpen, error) {
	// A tree plane produces no plane key, so it has no injectivity
	// obligation and nothing to precompute.
	return func(context.Context) (SlimReader, error) {
		f, err := os.Open(s.Path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		var root yaml.Node
		if err := yaml.NewDecoder(f).Decode(&root); err != nil {
			return nil, fmt.Errorf("yaml: %s: %w", s.Path, err)
		}
		return slimYAMLReader{&root}, nil
	}, nil
}

// slimYAMLReader is a named type rather than a SlimReaderFunc precisely
// because it carries the optional capability. This is the case that keeps
// SlimReader an interface.
type slimYAMLReader struct{ root *yaml.Node }

func (r slimYAMLReader) Get(ctx context.Context, p Path) (Value, error) {
	return yamlReader{r.root}.Get(ctx, p)
}

func (r slimYAMLReader) Children(ctx context.Context, prefix Path) ([]Path, error) {
	return yamlEnumReader{yamlReader{r.root}}.Children(ctx, prefix)
}

func (s SlimYAML) BindSink(*AddressSet) (SlimOpenWriter, error) {
	return func(context.Context) (SlimWriter, error) {
		tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".ferry-*")
		if err != nil {
			return nil, errors.Join(errSlimReadOnly, err)
		}
		return &slimYAMLWriter{s.Path, tmp, &yaml.Node{Kind: yaml.MappingNode}}, nil
	}, nil
}

type slimYAMLWriter struct {
	path string
	tmp  *os.File
	root *yaml.Node
}

func (w *slimYAMLWriter) Set(ctx context.Context, p Path, v Value) error {
	return (&yamlWriter{w.path, w.tmp, w.root}).Set(ctx, p, v)
}

func (w *slimYAMLWriter) Close(_ context.Context, cause error) error {
	if cause != nil {
		w.tmp.Close()
		return os.Remove(w.tmp.Name())
	}
	enc := yaml.NewEncoder(w.tmp)
	enc.SetIndent(2)
	if err := errors.Join(enc.Encode(w.root), enc.Close(), w.tmp.Close()); err != nil {
		os.Remove(w.tmp.Name())
		return err
	}
	return os.Rename(w.tmp.Name(), w.path)
}

func cmpOr(a, b string) string {
	if a == "" {
		return b
	}
	return a
}

func cmpOrFn(a, b func(string) (string, bool)) func(string) (string, bool) {
	if a == nil {
		return b
	}
	return a
}

var _ = strconv.Itoa
