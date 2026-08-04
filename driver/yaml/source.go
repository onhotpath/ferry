package yaml

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	yamlv3 "go.yaml.in/yaml/v3"

	"github.com/onhotpath/ferry"
)

// The read half implements two of the three optional interfaces and not the
// third, and the omission is the decision.
//
// [ferry.Enumerator] is here because a YAML mapping and a YAML sequence can
// both say what is under them, and a plane that cannot list is a plane no
// map-typed field can be loaded from. [ferry.Releaser] is not, because the open
// reads the whole document and closes the file before it returns, so a Close
// here would be the `return nil` boilerplate ADR-0004 refuses by name: in the
// source it is indistinguishable from a driver that should have released
// something and did not. The write half holds a staging file and implements
// both Committer and Releaser, which is where the lifecycle lives.
var (
	_ ferry.Source     = Source{}
	_ ferry.Reader     = reader{}
	_ ferry.Enumerator = reader{}
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
// It touches nothing. The file is read when a load starts, so a source over a
// path that does not exist yet is legal to build, and a load through it sees a
// file holding no keys: every field takes its default, and a required field
// fails.
func NewSource(path string) Source { return Source{path: path} }

// Bind takes the address set and reads nothing out of it, because this driver
// walks a document tree and builds no flat key that two fields could collide
// on.
//
// It does no I/O and cannot fail. A file that does not parse is reported when
// the load reads it, not here.
func (s Source) Bind(_ *ferry.AddressSet) (ferry.OpenFunc, error) {
	path := s.path

	return func(ctx context.Context) (ferry.Reader, error) {
		doc, err := readDoc(ctx, path)
		if err != nil {
			return nil, err
		}

		return reader{doc: doc}, nil
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
}

// Get answers with what the document holds at one address.
//
// The four observations this plane can make are four different answers, and
// keeping them apart is what the typed boundary is for: `nul: null` is Null,
// `empty: ""` is String(""), `value: 8080` is Number("8080"), and a key that is
// not there is Absent.
func (r reader) Get(_ context.Context, addr ferry.Path) (ferry.Value, error) {
	v, err := valueOf(deref(lookup(r.doc, addr)))
	if err != nil {
		return ferry.Value{}, ferry.ErrorAt(addr, err)
	}

	return v, nil
}

// Children lists the addresses immediately under a container.
//
// It is how a map's keys and a sequence's length reach core at all, since
// neither exists until there is a document to read them from.
func (r reader) Children(_ context.Context, prefix ferry.Path) ([]ferry.Path, error) {
	return children(r.doc, prefix), nil
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
// The one suppression in this package is here. gosec's G304 reports a file
// opened from a variable, and for this driver the variable is the plane: the
// caller names the file, that naming is the whole of the constructor's API, and
// there is nothing to validate it against that would not be this package
// deciding which of its user's files are allowed to exist.
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
