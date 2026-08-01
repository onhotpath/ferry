package main

// Driver: YAML files. Source and Sink.
//
// This is the driver that exercises every axis the memory plane cannot: a
// serialization format, a tree walked by segment rather than a flat key, real
// parse errors, real file I/O, and a whole-document sink that can only
// serialise at Commit.
//
// It is also the 5.8 and 5.11 regression: a sequence must not arrive as an
// empty string, and a parse failure must not return a nil error.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Source
// ---------------------------------------------------------------------------

type YAMLSource struct{ Path string }

// Bind has nothing to do: a tree plane never produces a plane key, it walks
// the segments, so it has no injectivity obligation and makes no KeyTable
// call. That the address set is unused here is itself a finding (P4).
func (s YAMLSource) Bind(*AddressSet) (Binding, error) { return s, nil }

func (s YAMLSource) Open(context.Context) (Reader, error) {
	f, err := os.Open(s.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var root yaml.Node
	if err := yaml.NewDecoder(f).Decode(&root); err != nil {
		return nil, fmt.Errorf("yaml: %s: %w", s.Path, err)
	}
	return yamlReader{&root}, nil
}

type yamlReader struct{ root *yaml.Node }

func (r yamlReader) Get(_ context.Context, addr Path) (Value, error) {
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
			i, err := strconv.Atoi(seg.Text)
			if err == nil && i < len(n.Content) {
				next = n.Content[i]
			}
		}
		if next == nil {
			return Absent, nil
		}
		n = next
	}
	return yamlValue(n)
}

// yamlValue is where the type information YAML has survives the boundary.
// xload's provider threw it away here (5.8): cast.ToString on a sequence
// yields "", and null and "" become the same thing.
func yamlValue(n *yaml.Node) (Value, error) {
	if n.Kind != yaml.ScalarNode {
		return Absent, fmt.Errorf("yaml: %s is a %s, not a scalar", n.Tag, yamlKindName(n.Kind))
	}
	switch n.Tag {
	case "!!null":
		return Null(), nil
	case "!!bool":
		return Bool(n.Value == "true" || n.Value == "yes" || n.Value == "on"), nil
	case "!!int", "!!float":
		return Number(n.Value), nil
	case "!!binary":
		return Bytes([]byte(n.Value)), nil
	default:
		// A quoted scalar resolves to !!str even when it looks numeric, so
		// port: "8080" and port: 8080 arrive as different kinds.
		return String(n.Value), nil
	}
}

func yamlKindName(k yaml.Kind) string {
	switch k {
	case yaml.SequenceNode:
		return "sequence"
	case yaml.MappingNode:
		return "mapping"
	default:
		return "node"
	}
}

// ---------------------------------------------------------------------------
// Sink
// ---------------------------------------------------------------------------

type YAMLSink struct{ Path string }

func (s YAMLSink) Bind(*AddressSet) (WriteBinding, error) { return s, nil }

func (s YAMLSink) Open(context.Context) (Writer, error) {
	// The read-only refusal, before any value is produced. A directory that
	// is not writable fails here rather than at Commit, which is what makes
	// "does this dump have any chance of working" answerable up front.
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".ferry-*")
	if err != nil {
		return nil, errors.Join(ErrReadOnly, err)
	}
	return &yamlWriter{path: s.Path, tmp: tmp, root: &yaml.Node{Kind: yaml.MappingNode}}, nil
}

type yamlWriter struct {
	path string
	tmp  *os.File
	root *yaml.Node
}

func (w *yamlWriter) Set(_ context.Context, addr Path, v Value) error {
	n := w.root
	segs := addr.Segments()
	for i, seg := range segs {
		last := i == len(segs)-1
		child, err := yamlChild(n, seg, last, v)
		if err != nil {
			return fmt.Errorf("yaml: %s: %w", addr, err)
		}
		n = child
	}
	return nil
}

// yamlChild is the whole reason the segment kind exists: it is what tells the
// emitter to build a sequence rather than a mapping, without guessing from
// whether the text looks like a base-10 integer.
func yamlChild(n *yaml.Node, seg Segment, last bool, v Value) (*yaml.Node, error) {
	want := yaml.MappingNode
	if !last && seg.Kind == Index {
		want = yaml.SequenceNode
	}
	switch seg.Kind {
	case Name:
		if n.Kind != yaml.MappingNode {
			return nil, errors.New("a name segment under a sequence")
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == seg.Text {
				return n.Content[i+1], nil
			}
		}
		k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: seg.Text}
		c := newYAMLNode(last, want, v)
		n.Content = append(n.Content, k, c)
		return c, nil
	default:
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
			n.Content[i] = newYAMLNode(true, want, v)
		}
		return n.Content[i], nil
	}
}

func newYAMLNode(last bool, want yaml.Kind, v Value) *yaml.Node {
	if !last {
		return &yaml.Node{Kind: want}
	}
	n := &yaml.Node{Kind: yaml.ScalarNode}
	// This switch is the typed boundary earning its keep. A string-only
	// boundary has nothing to switch on and emits port: "8080".
	switch v.Kind() {
	case VAbsent:
		// Whether ferry ever hands a sink an absent value at all is #8's.
		// The contract can carry it, which is what ADR-0001 needs for
		// partial dump not to be precluded.
		n.Tag, n.Value = "!!null", "null"
	case VNull:
		n.Tag, n.Value = "!!null", "null"
	case VBool:
		n.Tag, n.Value = "!!bool", v.Text()
	case VNumber:
		n.Tag, n.Value = "!!int", v.Text()
		if _, err := strconv.ParseInt(v.Text(), 10, 64); err != nil {
			n.Tag = "!!float"
		}
	case VBytes:
		n.Tag, n.Value = "!!binary", v.Text()
	default:
		n.Tag, n.Value, n.Style = "!!str", v.Text(), yaml.DoubleQuotedStyle
	}
	return n
}

func (w *yamlWriter) Commit(_ context.Context) error {
	enc := yaml.NewEncoder(w.tmp)
	enc.SetIndent(2)
	err := errors.Join(enc.Encode(w.root), enc.Close(), w.tmp.Close())
	if err != nil {
		os.Remove(w.tmp.Name())
		return err
	}
	return os.Rename(w.tmp.Name(), w.path)
}

func (w *yamlWriter) Abort() {
	w.tmp.Close()
	os.Remove(w.tmp.Name())
}
