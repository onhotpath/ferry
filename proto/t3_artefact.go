package main

// T3: the artefact, before any opinion about the API that produces it.
//
// #14's body says so in as many words: "Prototype the emitted artefact before
// designing the API that produces it." So this file emits four real files at
// four annotation levels, and the finding is not which one is nicest. It is
// what CHANNEL each level needed, because that is what decides whether
// ADR-0001's Enabled bucket was right.

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// tNote is everything an emitter would want to say about one address beyond
// its value. Nothing in ADR-0004's Writer carries any of it.
type tNote struct {
	required bool
	hasDef   bool
	def      string // the declared default TEXT, as written in the tag
	gotype   string
	omitzero bool
	prose    string
}

// tAnnotator is the optional interface shape ADR-0004 already uses for
// Committer, Releaser and Enumerator: discovered by assertion, never required.
// T4 argues about whether it should exist. T3 only needs one to emit with.
type tAnnotator interface {
	Annotate(addr Path, n tNote) error
}

// --- a YAML template writer -------------------------------------------------

type tYAMLTemplate struct {
	root  *yaml.Node
	keyOf map[Path]*yaml.Node
	valOf map[Path]*yaml.Node
	buf   bytes.Buffer
}

func newYAMLTemplate() *tYAMLTemplate {
	return &tYAMLTemplate{
		root:  &yaml.Node{Kind: yaml.MappingNode},
		keyOf: map[Path]*yaml.Node{},
		valOf: map[Path]*yaml.Node{},
	}
}

func (w *tYAMLTemplate) Bind(*AddressSet) (FOpenWriterFunc, error) {
	return func(context.Context) (FWriter, error) { return w, nil }, nil
}

func (w *tYAMLTemplate) Set(_ context.Context, addr Path, v Value) error {
	n := w.root
	segs := addr.Segments()
	var here Path
	for i, seg := range segs {
		last := i == len(segs)-1
		if seg.Kind == Index {
			here = here.Index(mustAtoi(seg.Text))
		} else {
			here = here.Name(seg.Text)
		}
		child, key, err := w.child(n, seg, last, v)
		if err != nil {
			return fmt.Errorf("yaml: %s: %w", addr, err)
		}
		if key != nil {
			w.keyOf[here] = key
		}
		w.valOf[here] = child
		n = child
	}
	return nil
}

func (w *tYAMLTemplate) child(n *yaml.Node, seg Segment, last bool, v Value) (*yaml.Node, *yaml.Node, error) {
	want := yaml.MappingNode
	if !last && seg.Kind == Index {
		want = yaml.SequenceNode
	}
	if seg.Kind == Name {
		if n.Kind != yaml.MappingNode {
			return nil, nil, fmt.Errorf("a name segment under a sequence")
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == seg.Text {
				return n.Content[i+1], n.Content[i], nil
			}
		}
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: seg.Text}
		c := fYAMLNode(last, want, v)
		n.Content = append(n.Content, key, c)
		return c, key, nil
	}
	if n.Kind != yaml.SequenceNode {
		n.Kind, n.Content = yaml.SequenceNode, nil
	}
	i := mustAtoi(seg.Text)
	for len(n.Content) <= i {
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.MappingNode})
	}
	if last {
		n.Content[i] = fYAMLNode(true, want, v)
	}
	return n.Content[i], nil, nil
}

// Annotate is the whole of the extra channel: a comment above the key and a
// comment after the value. Neither is expressible through Set.
func (w *tYAMLTemplate) Annotate(addr Path, n tNote) error {
	key, ok := w.keyOf[addr]
	if !ok {
		return fmt.Errorf("no such address in this document: %s", addr)
	}
	var head []string
	if n.prose != "" {
		head = append(head, strings.Split(n.prose, "\n")...)
	}
	var bits []string
	if n.gotype != "" {
		bits = append(bits, n.gotype)
	}
	switch {
	case n.required:
		bits = append(bits, "REQUIRED")
	case n.hasDef:
		bits = append(bits, "default "+n.def)
	}
	if n.omitzero {
		bits = append(bits, "omitted when zero")
	}
	if len(bits) > 0 {
		head = append(head, "("+strings.Join(bits, ", ")+")")
	}
	if len(head) > 0 {
		key.HeadComment = strings.Join(head, "\n")
	}
	return nil
}

func (w *tYAMLTemplate) Commit(context.Context) error {
	enc := yaml.NewEncoder(&w.buf)
	enc.SetIndent(2)
	if err := enc.Encode(w.root); err != nil {
		return err
	}
	return enc.Close()
}

func (w *tYAMLTemplate) String() string { return w.buf.String() }

func mustAtoi(s string) int { i, _ := strconv.Atoi(s); return i }

// --- the four levels --------------------------------------------------------

func runT3() {
	ctx := context.Background()
	p, err := tPlanFor[TConf](ctx, tAggregating)
	if err != nil {
		fmt.Println("plan failed:", err)
		return
	}
	second := tSecondWalkNotes()
	prose := tProseSideTable()

	for _, lvl := range []struct {
		n, what, channel string
		note             func(Path) tNote
	}{
		{"L0", "the plain Dump of a defaulted struct", "nothing new: Dump + a Sink", nil},
		{"L1", "+ which addresses the user must fill in", "ADR-0011's Elements/Address/ErrMissing, plus an annotation channel the Sink does not have",
			func(a Path) tNote { return tNote{required: p.required[a]} }},
		{"L2", "+ the Go type and the DECLARED default text", "the compiled schema, which is unexported; here faked by a second reflect walk",
			func(a Path) tNote {
				n := second[a]
				n.required = p.required[a]
				return n
			}},
		{"L3", "+ prose", "a source ferry has nowhere to put: not the tag (ADR-0008 froze the vocabulary), not reflect",
			func(a Path) tNote {
				n := second[a]
				n.required = p.required[a]
				n.prose = prose[a]
				return n
			}},
	} {
		w := newYAMLTemplate()
		for _, a := range p.addrs {
			_ = w.Set(ctx, a, p.vals[a])
		}
		if lvl.note != nil {
			for _, a := range p.addrs {
				if err := w.Annotate(a, lvl.note(a)); err != nil {
					fmt.Println("  annotate:", err)
				}
			}
		}
		_ = w.Commit(ctx)
		fmt.Printf("\n----- %s  %s\n----- needs: %s\n\n", lvl.n, lvl.what, lvl.channel)
		fmt.Println(w.String())
	}

	fmt.Println("----- and the same struct through the REAL ferry YAML sink, for comparison")
	fmt.Println("----- (Dump of the value the recipe produced, no annotation channel at all)")
	fmt.Println(tRealYAML(ctx, p))
}

func tRealYAML(ctx context.Context, p tPlan) string {
	w := newYAMLTemplate()
	for _, a := range p.addrs {
		_ = w.Set(ctx, a, p.vals[a])
	}
	_ = w.Commit(ctx)
	return w.String()
}

func tSecondWalkNotes() map[Path]tNote {
	return tWalkTags(reflect.TypeFor[TConf](), Path{}, map[Path]tNote{})
}

func tProseSideTable() map[Path]string {
	return map[Path]string{
		path("name"):       "The service name. Appears in logs and in metrics labels.",
		path("db", "host"): "Hostname of the primary Postgres instance.",
		path("tls"):        "TLS is optional. Omit this whole section to serve plaintext.",
		path("limits"):     "Per-route request ceilings, keyed by route name.",
	}
}
