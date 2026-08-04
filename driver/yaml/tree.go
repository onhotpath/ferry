package yaml

import (
	"fmt"
	"slices"
	"strconv"

	yamlv3 "go.yaml.in/yaml/v3"

	"github.com/onhotpath/ferry"
)

// An address is a path through the document and nothing is flattened, which is
// the whole of this driver's answer to ADR-0003: a Name segment selects a
// mapping member, an Index segment selects a sequence position, and the
// segment's kind is what says which - never the shape of its text. A mapping
// whose key is "0" and a sequence's first element are two different places
// here, and telling them apart from base-10 text alone is the limitation the
// kind exists to avoid.
//
// So there is no key function, no key table and no injectivity obligation:
// distinct addresses are distinct paths by construction. Nothing in this
// package calls core's key helper, and a test asserts that by reading the
// source.

// lookup answers with the node at addr, or nil where the document has nothing
// there.
func lookup(doc *yamlv3.Node, addr ferry.Path) *yamlv3.Node {
	n := root(doc)

	for seg := range addr.Segments() {
		if n = step(n, seg); n == nil {
			return nil
		}
	}

	return n
}

// root is the document's content node, or nil for a document with none.
func root(doc *yamlv3.Node) *yamlv3.Node {
	if doc == nil || len(doc.Content) == 0 {
		return nil
	}

	return doc.Content[0]
}

// step takes one segment down from n.
func step(n *yamlv3.Node, seg ferry.Segment) *yamlv3.Node {
	n = deref(n)

	if seg.Kind() == ferry.Index {
		return element(n, seg.Text())
	}

	return member(n, seg.Text())
}

// member is the value a mapping holds under one key, comparing the key text
// byte for byte. Core never folds or normalises segment text, so neither does
// this: two case-variant keys are two members.
func member(n *yamlv3.Node, name string) *yamlv3.Node {
	if n == nil || n.Kind != yamlv3.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == name && n.Content[i].Kind == yamlv3.ScalarNode {
			return n.Content[i+1]
		}
	}

	return nil
}

// element is the node at one sequence position.
func element(n *yamlv3.Node, text string) *yamlv3.Node {
	if n == nil || n.Kind != yamlv3.SequenceNode {
		return nil
	}

	i, err := indexOf(text)
	if err != nil || i >= len(n.Content) {
		return nil
	}

	return n.Content[i]
}

// aliasLimit bounds how far an alias chain is followed. An anchor has to be
// defined before it is used, so a chain in a parsed document is short and
// acyclic; the bound is here so that a document this driver did not parse
// cannot make a read spin.
const aliasLimit = 64

// deref follows an alias to the node it names, which is what makes a document
// using YAML's anchors readable rather than a subtree of absences.
//
// Only the read side follows one. Writing through an alias would change the
// anchor and every other place that refers to it, so the write side replaces
// the alias node instead.
func deref(n *yamlv3.Node) *yamlv3.Node {
	for range aliasLimit {
		if n == nil || n.Kind != yamlv3.AliasNode {
			return n
		}

		n = n.Alias
	}

	return nil
}

// indexOf reads back the position an Index segment holds.
//
// The text is canonical base 10 with no leading zero, minted by
// [ferry.Path.Elem] from a uint, so the only failure this can report is a
// position too wide for this platform's int - which no sequence in memory could
// have had, and which is reported rather than assumed away.
func indexOf(text string) (int, error) {
	i, err := strconv.Atoi(text)
	if err != nil || i < 0 {
		return 0, fmt.Errorf("%w: the sequence position does not fit this platform's int", ferry.ErrPlane)
	}

	return i, nil
}

// place is the write-side counterpart of [lookup]: the node at addr, with every
// container above it created or reshaped to hold it.
//
// Reshaping rather than refusing is deliberate. The addresses ferry writes are
// the ones the destination type determines, so a scalar sitting where a mapping
// has to go is a file whose shape has moved on from what it was written with,
// and the dump is what brings it up to date. Every node the addresses do not
// reach is left exactly as it was parsed.
func place(doc *yamlv3.Node, addr ferry.Path) (*yamlv3.Node, error) {
	// The segments are collected rather than ranged over, because the write
	// side needs to look one segment ahead: what a container has to be is
	// decided by the kind of the segment under it.
	segs := slices.Collect(addr.Segments())
	if len(segs) == 0 {
		return nil, fmt.Errorf("%w: the empty path is not an address", ferry.ErrPlane)
	}

	n := rootFor(doc, segs[0].Kind())

	for i, seg := range segs {
		child, err := slot(n, seg)
		if err != nil {
			return nil, err
		}

		n = child

		if i+1 < len(segs) {
			shape(n, segs[i+1].Kind())
		}
	}

	return n, nil
}

// rootFor is the document's content node, minted where the document is empty
// and reshaped where its top level is not what the first segment needs.
func rootFor(doc *yamlv3.Node, k ferry.SegmentKind) *yamlv3.Node {
	if len(doc.Content) == 0 {
		doc.Content = []*yamlv3.Node{{}}
	}

	n := doc.Content[0]
	shape(n, k)

	return n
}

// shape turns a node into the container that a segment of kind k is looked up
// in, leaving it alone where it is one already - which is what keeps the
// comments, the ordering and the untouched keys of an existing document.
//
// The anchor is carried over the reshape for the reason [writer.Set] carries it
// over a replaced scalar (#196): it is the operator's name for this place, and
// a reshape that dropped it would leave every alias to this node dangling and
// the document unparseable.
func shape(n *yamlv3.Node, k ferry.SegmentKind) {
	kind, tag := yamlv3.MappingNode, mapTag
	if k == ferry.Index {
		kind, tag = yamlv3.SequenceNode, seqTag
	}

	if n.Kind == kind {
		return
	}

	*n = yamlv3.Node{Kind: kind, Tag: tag, Anchor: n.Anchor, HeadComment: n.HeadComment,
		LineComment: n.LineComment, FootComment: n.FootComment}
}

// slot is the node one segment names under n, appended where n does not have it
// yet. n is already the container kind the segment needs.
func slot(n *yamlv3.Node, seg ferry.Segment) (*yamlv3.Node, error) {
	if seg.Kind() == ferry.Index {
		return elementSlot(n, seg.Text())
	}

	return memberSlot(n, seg.Text()), nil
}

// memberSlot is a mapping's value node for one key, appending the pair where
// the key is not there. Appending is what puts a new key at the end of the
// mapping and leaves every existing key where the operator wrote it.
func memberSlot(n *yamlv3.Node, name string) *yamlv3.Node {
	if v := member(n, name); v != nil {
		return v
	}

	v := &yamlv3.Node{}
	n.Content = append(n.Content, leaf(strTag, name), v)

	return v
}

// elementSlot is a sequence's node at one position, growing the sequence to
// reach it.
//
// The fill is nulls, and it is unreachable through a walk over a real value:
// core writes a sequence's positions in order from zero, so the sequence is
// always exactly one short of the position being written.
func elementSlot(n *yamlv3.Node, text string) (*yamlv3.Node, error) {
	i, err := indexOf(text)
	if err != nil {
		return nil, err
	}

	for len(n.Content) <= i {
		n.Content = append(n.Content, leaf(nullTag, nullText))
	}

	return n.Content[i], nil
}

// children is the immediate children of one address, as addresses.
//
// Addresses and not names, because an address carries its segment kind: a
// sequence position and a mapping member are different answers, and text alone
// cannot say which (ADR-0004).
func children(doc *yamlv3.Node, prefix ferry.Path) []ferry.Path {
	n := deref(lookup(doc, prefix))
	if n == nil {
		return nil
	}

	if n.Kind == yamlv3.SequenceNode {
		return positions(prefix, len(n.Content))
	}

	return keys(n, prefix)
}

// positions is a sequence's children, in order.
func positions(prefix ferry.Path, n int) []ferry.Path {
	out := make([]ferry.Path, 0, n)
	for i := range n {
		out = append(out, prefix.Elem(uint(i)))
	}

	return out
}

// keys is a mapping's children, in the order the document holds them.
//
// A duplicate key is a document YAML's own grammar permits and this driver
// answers once for, because two entries under one key are one address and
// [member] reads the first of them: enumerating it twice would hand core an
// address it then reads a single value from, twice.
func keys(n *yamlv3.Node, prefix ferry.Path) []ferry.Path {
	if n.Kind != yamlv3.MappingNode {
		return nil
	}

	seen := make(map[string]bool, len(n.Content)/2)
	out := make([]ferry.Path, 0, len(n.Content)/2)

	for i := 0; i+1 < len(n.Content); i += 2 {
		if k := n.Content[i]; k.Kind == yamlv3.ScalarNode && !seen[k.Value] {
			seen[k.Value] = true

			out = append(out, prefix.At(k.Value))
		}
	}

	return out
}
