package main

// Driver: YAML files, against the final contract. Source and Sink.
// The only driver reaching a format, a tree walk, plane-side type
// information, enumeration, and a staging sink needing both Commit and Close.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// --- source -----------------------------------------------------------------

type FYAMLSource struct{ Path string }

// Bind does nothing: a tree plane produces no plane key, so it has no
// injectivity obligation and nothing to precompute. The address set is unused,
// which is a real asymmetry: ADR-0003's driver rule binds flatteners only.
func (s FYAMLSource) Bind(*AddressSet) (FOpenFunc, error) {
	return func(context.Context) (FReader, error) {
		f, err := os.Open(s.Path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		var root yaml.Node
		if err := yaml.NewDecoder(f).Decode(&root); err != nil {
			return nil, fmt.Errorf("yaml: %s: %w", s.Path, err) // 5.11
		}
		return fYAMLReader{&root}, nil
	}, nil
}

type fYAMLReader struct{ root *yaml.Node }

func (r fYAMLReader) node(addr Path) *yaml.Node {
	n := r.root
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	for _, seg := range addr.Segments() {
		var next *yaml.Node
		switch {
		case seg.Kind == Name && n.Kind == yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == seg.Text { // never folds case
					next = n.Content[i+1]
				}
			}
		case seg.Kind == Index && n.Kind == yaml.SequenceNode:
			if i, err := strconv.Atoi(seg.Text); err == nil && i < len(n.Content) {
				next = n.Content[i]
			}
		}
		if next == nil {
			return nil
		}
		n = next
	}
	return n
}

// Get is where the type information YAML has survives the boundary. xload's
// provider threw it away here (5.8).
func (r fYAMLReader) Get(_ context.Context, addr Path) (Value, error) {
	n := r.node(addr)
	if n == nil {
		return Absent, nil
	}
	if n.Kind != yaml.ScalarNode {
		return Absent, fmt.Errorf("yaml: %s is not a scalar", addr)
	}
	switch n.Tag {
	case "!!null":
		return Null(), nil
	case "!!bool":
		return Bool(n.Value == "true" || n.Value == "yes" || n.Value == "on"), nil
	case "!!int", "!!float":
		return Number(n.Value), nil
	case "!!binary":
		raw, err := base64.StdEncoding.DecodeString(n.Value)
		if err != nil {
			return Value{}, fmt.Errorf("yaml: !!binary at %s: %w", n.Value, err)
		}
		return Bytes(raw), nil
	default:
		// A quoted scalar resolves to !!str even when it looks numeric,
		// so port: "8080" and port: 8080 arrive as different kinds.
		return String(n.Value), nil
	}
}

func (r fYAMLReader) Children(_ context.Context, prefix Path) ([]Path, error) {
	n := r.node(prefix)
	if n == nil {
		return nil, nil
	}
	var out []Path
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			out = append(out, prefix.Name(n.Content[i].Value))
		}
	case yaml.SequenceNode:
		for i := range n.Content {
			out = append(out, prefix.Index(i))
		}
	}
	return out, nil
}

// --- sink -------------------------------------------------------------------

type FYAMLSink struct{ Path string }

func (s FYAMLSink) Bind(*AddressSet) (FOpenWriterFunc, error) {
	return func(context.Context) (FWriter, error) {
		tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".ferry-*")
		if err != nil {
			return nil, errors.Join(ErrReadOnly, err) // the read-only refusal
		}
		return &fYAMLWriter{s.Path, tmp, &yaml.Node{Kind: yaml.MappingNode}, false}, nil
	}, nil
}

type fYAMLWriter struct {
	path      string
	tmp       *os.File
	root      *yaml.Node
	committed bool
}

func (w *fYAMLWriter) Set(_ context.Context, addr Path, v Value) error {
	n := w.root
	segs := addr.Segments()
	for i, seg := range segs {
		child, err := fYAMLChild(n, seg, i == len(segs)-1, v)
		if err != nil {
			return fmt.Errorf("yaml: %s: %w", addr, err)
		}
		n = child
	}
	return nil
}

// fYAMLChild is the whole reason a segment carries a kind: it says build a
// sequence rather than a mapping, without guessing from base-10 text.
func fYAMLChild(n *yaml.Node, seg Segment, last bool, v Value) (*yaml.Node, error) {
	want := yaml.MappingNode
	if !last && seg.Kind == Index {
		want = yaml.SequenceNode
	}
	if seg.Kind == Name {
		if n.Kind != yaml.MappingNode {
			return nil, errors.New("a name segment under a sequence")
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == seg.Text {
				return n.Content[i+1], nil
			}
		}
		c := fYAMLNode(last, want, v)
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: seg.Text}, c)
		return c, nil
	}
	if n.Kind != yaml.SequenceNode {
		n.Kind, n.Content = yaml.SequenceNode, nil
	}
	i, err := strconv.Atoi(seg.Text)
	if err != nil {
		return nil, err
	}
	for len(n.Content) <= i {
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.MappingNode})
	}
	if last {
		n.Content[i] = fYAMLNode(true, want, v)
	}
	return n.Content[i], nil
}

// The kind switch is the typed boundary earning its keep. A string-only
// boundary has nothing to switch on and emits port: "8080".
func fYAMLNode(last bool, want yaml.Kind, v Value) *yaml.Node {
	if !last {
		return &yaml.Node{Kind: want}
	}
	n := &yaml.Node{Kind: yaml.ScalarNode}
	switch v.Kind() {
	case VAbsent, VNull:
		n.Tag, n.Value = "!!null", "null"
	case VBool:
		n.Tag, n.Value = "!!bool", v.Text()
	case VNumber:
		n.Tag, n.Value = "!!int", v.Text()
		if _, err := strconv.ParseInt(v.Text(), 10, 64); err != nil {
			n.Tag = "!!float"
		}
	case VBytes:
		n.Tag, n.Value = "!!binary", base64.StdEncoding.EncodeToString([]byte(v.Text()))
	default:
		n.Tag, n.Value, n.Style = "!!str", v.Text(), yaml.DoubleQuotedStyle
	}
	return n
}

func (w *fYAMLWriter) Commit(context.Context) error {
	enc := yaml.NewEncoder(w.tmp)
	enc.SetIndent(2)
	if err := errors.Join(enc.Encode(w.root), enc.Close(), w.tmp.Close()); err != nil {
		return err
	}
	if err := os.Rename(w.tmp.Name(), w.path); err != nil {
		return err
	}
	w.committed = true
	return nil
}

// Close is unconditional cleanup and is never told what happened: Commit
// having run or not is the whole signal.
func (w *fYAMLWriter) Close() error {
	if w.committed {
		return nil
	}
	w.tmp.Close()
	return os.Remove(w.tmp.Name())
}
