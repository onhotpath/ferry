package yaml

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	yamlv3 "go.yaml.in/yaml/v3"

	"github.com/onhotpath/ferry"
)

// The read half implements three of the four optional interfaces and not the
// fourth, and the omission is the decision.
//
// [ferry.Enumerator] is here because a YAML mapping and a YAML sequence can
// both say what is under them, and a plane that cannot list is a plane no
// map-typed field can be loaded from. [ferry.Prober] is here because a document
// tree distinguishes all three answers a container address carries: a missing
// key, an explicit null, and a mapping that is there and empty (ADR-0016).
// [ferry.Releaser] is not, because the open reads the whole document and closes
// the file before it returns, so a Close here would be the `return nil`
// boilerplate ADR-0004 refuses by name: in the source it is indistinguishable
// from a driver that should have released something and did not. The write half
// holds a staging file and implements both Committer and Releaser, which is
// where the lifecycle lives.
var (
	_ ferry.Source     = Source{}
	_ ferry.Reader     = reader{}
	_ ferry.Prober     = reader{}
	_ ferry.Enumerator = reader{}
	_ ferry.PlaneNamer = reader{}
)

// Source reads a struct's fields out of a YAML file.
//
// It is a separate type from [Sink] rather than the other half of one, so a
// round trip names the path twice:
//
//	cfg, err := ferry.Load[Config](ctx, yaml.NewSource("config.yaml"))
//	err = ferry.Dump(ctx, cfg, yaml.NewSink("config.yaml"))
//
// The repetition buys the refusal being a compile error: code handed only a
// Source cannot save through it, and nothing has to check at run time to say
// so.
type Source struct {
	path string
}

// NewSource returns a source over the YAML file at path.
//
// It touches nothing, and starts nothing, unless it is given [Watch]. The file
// is read when a load starts, so a source over a path that does not exist yet is
// legal to build, and a load through it sees a file holding no keys: every field
// takes its default, and a required field fails.
//
// Pass [Watch] to be called when the file changes underneath the source. That is
// the one setting that does something before a load: it takes the file's current
// state here and polls from a goroutine of its own until the context it was
// given is done.
func NewSource(path string, opts ...SourceOption) Source {
	var c sourceConfig

	for _, o := range opts {
		o.applySource(&c)
	}

	if c.watch != nil {
		c.watch.start(path)
	}

	return Source{path: path}
}

// Bind builds no flat key from the address set, because this driver walks a
// document tree and two fields cannot collide on a path.
//
// What it does take from the set is the shape each section's own members have.
// A struct's members are named and an array's are positions, so the document has
// to hold a mapping at the one and a sequence at the other, and a load through
// this binding refuses the other way round rather than reading an empty section
// out of it.
//
// It does no I/O and cannot fail. A file that does not parse is reported when
// the load reads it, not here.
func (s Source) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	path, sections := s.path, sectionShapes(addrs)

	return func(ctx context.Context) (ferry.Reader, error) {
		doc, err := readDoc(ctx, path)
		if err != nil {
			return nil, err
		}

		return reader{doc: doc, sections: sections}, nil
	}, nil
}

// reader is one parsed document, held whole.
//
// The whole file in memory is the honest shape for this plane rather than a
// choice about batching: a YAML document has to be parsed from the start to
// answer anything at all, so there is no lazy read to be had, and one read per
// open is what ADR-0004 leaves to the driver.
type reader struct {
	doc *yamlv3.Node

	// sections is the node kind each declared section's members live in, built
	// once at Bind and never written to afterwards.
	sections map[ferry.SectionAddr]yamlv3.Kind
}

// PlaneName is the document's own name for an address, and it is [nameInDocument]
// forwarded: this half of the driver holds nothing a name depends on
// (ADR-0011, #159).
func (reader) PlaneName(addr ferry.Path) (string, bool) { return nameInDocument(addr) }

// sectionShapes reads the container shape of every section the type determined
// out of the address set (ADR-0016).
//
// Only a section is in it. A composite's members come from the value, and
// whether a slice or a map produced it is deliberately not part of its address,
// so the document decides that one and [children] reads whichever it holds.
//
// A section the type put no member under - an empty struct - is in no entry, so
// nothing is asserted about the node at it.
func sectionShapes(addrs *ferry.AddressSet) map[ferry.SectionAddr]yamlv3.Kind {
	out := make(map[ferry.SectionAddr]yamlv3.Kind, addrs.Len())

	for m := range addrs.Seq() {
		section, ok := m.(ferry.SectionAddr)
		if !ok {
			continue
		}

		if kind, known := memberKind(addrs, section.Path()); known {
			out[section] = kind
		}
	}

	return out
}

// memberKind is the node kind one section's own members live in, read off the
// first address the type put under it: a position needs a sequence and a name
// needs a mapping.
//
// A section's members are all of one segment kind, because core refuses a
// position under a mapping and a name under a sequence, so the first one settles
// it.
func memberKind(addrs *ferry.AddressSet, at ferry.Path) (yamlv3.Kind, bool) {
	for m := range addrs.Seq() {
		kind, ok := firstBelow(at, m.Path())
		if !ok {
			continue
		}

		if kind == ferry.Index {
			return yamlv3.SequenceNode, true
		}

		return yamlv3.MappingNode, true
	}

	return 0, false
}

// firstBelow is the kind of the first segment of p below prefix, and whether p
// lies below prefix at all.
//
// The canonical renderings decide it. ADR-0003's escaping leaves no bare
// delimiter inside a segment, so the byte that continues past the prefix is the
// delimiter that introduces the next segment and never part of one.
func firstBelow(prefix, p ferry.Path) (ferry.SegmentKind, bool) {
	rest, ok := strings.CutPrefix(p.String(), prefix.String())
	if !ok || rest == "" {
		return 0, false
	}

	switch rest[0] {
	case indexDelim:
		return ferry.Index, true
	case nameDelim:
		return ferry.Name, true
	default:
		return 0, false
	}
}

// The two bytes ADR-0003 introduces a segment with, which is how a rendering
// says which kind comes next.
const (
	nameDelim  = '/'
	indexDelim = '#'
)

// Get answers with what the document holds at one leaf.
//
// The four observations this plane can make are four different answers, and
// keeping them apart is what the typed boundary is for: `nul: null` is Null,
// `empty: ""` is String(""), `value: 8080` is Number("8080"), and a key that is
// not there is Absent.
//
// A mapping or a sequence at a leaf's address is none of those four, and it is
// a refusal naming the address and what the document holds there. The
// destination takes one value and the file holds a container: the two disagree,
// and answering absence would leave the field at its zero value with nothing
// saying why.
func (r reader) Get(_ context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	v, err := valueOf(deref(lookup(r.doc, addr.Path())))
	if err != nil {
		return ferry.Value{}, ferry.ErrorAt(addr.Path(), err)
	}

	return v, nil
}

// Probe answers whether the document holds a container at one address.
//
// A key that is not there is absent, an explicit `null` is null, and a mapping
// or a sequence is present, including an empty one: `opts: {}` reads back as a
// section that is there and holds nothing, which is what lets a present-empty
// section survive a round trip. A single value where a container belongs is a
// refusal naming the address.
//
// So is the wrong collection at a section. A struct's members are named and an
// array's are positions, so a sequence where the destination takes a struct, and
// a mapping where it takes an array, are the document and the destination
// disagreeing about the shape of the data: reading it as a section that is there
// would fill every field with the Go zero and drop what the document held.
func (r reader) Probe(_ context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	at := deref(lookup(r.doc, addr.Path()))

	info, err := presenceOf(at)
	if err == nil {
		err = r.holdable(addr, at)
	}

	if err != nil {
		return ferry.SectionAbsent, ferry.ErrorAt(addr.Path(), err)
	}

	return info, nil
}

// holdable refuses a collection of the kind this container's members do not live
// in, and passes everything else through.
//
// Only a section is checked. A composite's members come from the value, and
// ADR-0016 withholds slice-versus-map from its address on purpose, so the
// document decides which one it holds and core's own checks on the segments
// [children] answers are what catch a document that holds the other.
func (r reader) holdable(addr ferry.Container, at *yamlv3.Node) error {
	section, ok := addr.(ferry.SectionAddr)
	if !ok || at == nil {
		return nil
	}

	want, known := r.sections[section]
	if !known || at.Kind == want || !collection(at.Kind) {
		return nil
	}

	return fmt.Errorf("%w: the plane holds %s here and the destination's members are %s: change the document, "+
		"or model the field as what the plane holds", ferry.ErrValue, shapeOf(at), membersOf(want))
}

// collection reports whether a node is one of the two kinds that hold members.
func collection(k yamlv3.Kind) bool {
	return k == yamlv3.MappingNode || k == yamlv3.SequenceNode
}

// membersOf names what a section's members are, in the words a reader of the
// message thinks in rather than the parser's.
func membersOf(k yamlv3.Kind) string {
	if k == yamlv3.SequenceNode {
		return "positions the type fixes"
	}

	return "names the type fixes"
}

// Children lists the segments the document holds immediately under a composite.
//
// It is how a map's keys and a sequence's length reach core at all, since
// neither exists until there is a document to read them from. A sequence
// answers with positions and a mapping with names, and the schema types the
// child each one addresses.
func (r reader) Children(_ context.Context, addr ferry.CompositeAddr) ([]ferry.Segment, error) {
	return children(r.doc, addr.Path()), nil
}

// readDoc parses the plane, and is shared by both halves: a dump merges into
// the document that is already there, so the sink reads it exactly as the
// source does.
//
// A file that is not there is an empty document rather than a failure. Absent
// is how a plane reports that it does not hold an address, a config file that
// has not been written yet holds none of them, and a dump to a path with no
// file at it is how the first one gets written.
//
// This package suppresses gosec's G304 here and in syncDir, and both are the
// same suppression. G304 reports a file opened from a variable, and for this
// driver the variable is the plane: the caller names the file, that naming is
// the whole of the constructor's API, and there is nothing to validate it
// against that would not be this package deciding which of its user's files are
// allowed to exist.
func readDoc(ctx context.Context, path string) (*yamlv3.Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path) //nolint:gosec // G304: the path is the plane, and naming it is the whole API.
	if errors.Is(err, fs.ErrNotExist) {
		return &yamlv3.Node{Kind: yamlv3.DocumentNode}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("%w: reading the plane: %w", ferry.ErrPlane, err)
	}

	return parseDoc(data)
}

// parseDoc turns the file's bytes into a document.
//
// A document that does not parse fails here, inside the open, with a non-nil
// error and never with an empty result. Survey item 5.11 found a YAML provider
// discarding parse errors and answering with an empty result, which is a
// config load reporting success for a file it never read.
//
// The class is [ferry.ErrValue] and the driver states it rather than letting
// core default the moment to ErrPlane: ferry could reach the plane and read it,
// and what failed is the operator's own file. The parser's message stays in the
// chain and is printed, which is what makes the failure actionable - it names a
// line - and it names no value the document holds.
func parseDoc(data []byte) (*yamlv3.Node, error) {
	var doc yamlv3.Node

	dec := yamlv3.NewDecoder(bytes.NewReader(data))

	if err := dec.Decode(&doc); err != nil {
		// A file that is empty, or that holds nothing but comments, has no
		// document in it at all, and holds no addresses rather than failing.
		if errors.Is(err, io.EOF) {
			return &yamlv3.Node{Kind: yamlv3.DocumentNode}, nil
		}

		return nil, fmt.Errorf("%w: the plane is not a YAML document: %w", ferry.ErrValue, err)
	}

	if err := onlyDocument(dec); err != nil {
		return nil, err
	}

	return &doc, nil
}

// onlyDocument refuses a plane holding more than one document.
//
// A YAML file may carry a stream of documents separated by ---, and an address
// names a place in one of them. Reading the first and ignoring the rest is
// tolerable; writing the first and dropping the rest is silent data loss on the
// operator's file, and ADR-0001 rules out ignoring anything silently. So the
// stream is refused at the open, in both directions, where it costs a message
// rather than a file.
func onlyDocument(dec *yamlv3.Decoder) error {
	var next yamlv3.Node

	switch err := dec.Decode(&next); {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		return fmt.Errorf("%w: the plane is not a YAML document: %w", ferry.ErrValue, err)
	default:
		return fmt.Errorf("%w: the plane holds more than one document, and an address names a place in one of "+
			"them: a dump would write the first and drop the rest", ferry.ErrValue)
	}
}
