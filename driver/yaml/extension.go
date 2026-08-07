package yaml

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/onhotpath/ferry"
)

// This file is this driver's half of ADR-0021, and it is #156's answer.
//
// The read side of a custom node type was never the hard half: the tag is in
// the file, [kindOf] reads a tag it has no arm for as a String, and the text
// after it reaches whichever codec the field declared. The write side is where
// the hole was. [ferry.Writer.Set] is handed a [ferry.Value] and not a Go type,
// deliberately (ADR-0004), so a save saw String("30s") with nothing saying that
// this one wanted !mycompany:duration - and a document that had one lost it,
// because [carryTag] can only keep a tag the file already held.
//
// ADR-0021's mechanism puts the answer where the address is defined once: on
// the field, in a key this driver owns, compiled into an address-keyed table
// that rides the [ferry.AddressSet] both Binds are handed. So the annotation
// answers on the write, which is exactly where the discarded type left the
// hole, and a caller plumbs nothing.
//
// Core validates the words against the declaration and acts on none of them.
// Acting is this driver's, and so is the proof that what it writes reads back:
// TestADeclaredNodeTagSurvivesThreeStages is that proof, on #156's own bar of a
// load, a dump and a second load with the third stage compared against the
// first.

// ExtensionKey is the struct tag key [Extension] declares.
//
// It is not "yaml", which is the key go-yaml's own marshaller reads: a field
// may carry both, and each library reads the key it owns.
const ExtensionKey = "yamlext"

// nodeWord is the one word of the vocabulary: the tag the address is written
// under.
const nodeWord = "node"

// bang is what every YAML tag starts with, in both of the shorthand forms a
// document spells: !local and !!standard.
const bang = "!"

// Extension declares this driver's struct tag key, for a registry to read
// beside ferry's own.
//
//	var Registry = ferry.NewRegistry(ferry.WithTagKeys(yaml.Extension()))
//
//	type Config struct {
//	    Wait string `ferry:"wait" yamlext:"node=!mycompany:duration"`
//	}
//
//	err := ferry.Dump(ctx, cfg, yaml.NewSink(path), ferry.WithRegistry(Registry))
//
// The vocabulary is one word. node=<tag> is the YAML tag the value at that
// address is written under, spelled the way a document spells it, with its
// leading ! and its type after it. A save then writes
//
//	wait: !mycompany:duration 30s
//
// where it would have written `wait: 30s`, whether or not the file had a tag
// there, and a tag the file did have at that address loses to the one declared.
//
// It changes nothing about a load, because nothing has to. A tag this package
// has no reading of its own for is carried and not interpreted, so the value
// arrives as the text after the tag and reaches whichever codec your field
// declared. That is also what makes the word survive a round trip, and it is
// the shape of its one limit: such a value comes back as a string, so the word
// belongs on a field this plane writes as one.
//
// Four things it refuses, all of them at the save's [Sink.Bind] except the last:
//
//   - a tag that is not a tag: one not starting with !, one naming nothing
//     after the !, or one with a space in it
//   - a tag this package spells itself, !!str and !!int and the rest, because
//     the value's own kind decides those and a declaration cannot contradict it
//   - the address of a struct, a slice or a map, because a node tag names how
//     one value is written and those are places rather than values
//   - a value this plane writes as a number, a boolean or bytes, refused when
//     that value is written, since a tag this plane cannot read back would make
//     the value return as a string
//
// A null is the one value written without the tag rather than refused: there is
// no value there for a node type to describe, and the address reads back null
// either way.
//
// A registry that was not given this declaration reads nothing: the key is then
// another library's business, the tags stay in your structs, and a save writes
// what it always wrote.
func Extension() ferry.KeyExtension {
	return ferry.KeyExtension{
		TagKey: ExtensionKey,
		Words:  []ferry.Word{{Name: nodeWord, TakesValue: true}},
	}
}

// nodeTags is this driver's own view of the address set: the tag declared at
// each address, read once at Bind and never written to afterwards (ADR-0021).
//
// Every refusal a declaration alone can carry fires here, before any I/O and
// before the operator's file has been touched, which is the same bargain
// [Sink.Bind]'s address-set checks already make in the other drivers.
//
// A set carrying no declaration at all builds no map, which is every load and
// every save under a registry that was not given [Extension].
func nodeTags(addrs *ferry.AddressSet) (map[ferry.Path]string, error) {
	view := addrs.Extension(ExtensionKey)
	if len(view) == 0 {
		return nil, nil
	}

	leaves := leafPaths(addrs)
	out := make(map[ferry.Path]string, len(view))

	for addr, words := range view {
		tag, declared := words[nodeWord]
		if !declared {
			continue
		}

		if err := checkNodeAddr(addr, tag, leaves); err != nil {
			return nil, err
		}

		out[addr] = tag
	}

	return out, nil
}

// checkNodeAddr holds one declaration to what this driver can act on: a tag it
// can write, at an address that holds a value.
func checkNodeAddr(addr ferry.Path, tag string, leaves map[ferry.Path]bool) error {
	if !leaves[addr] {
		return ferry.ErrorAt(addr, fmt.Errorf("%w: a node tag names how one value is written and this address "+
			"holds a struct, a list or a map: annotate the fields under it, whose addresses this plane writes "+
			"a value at", ferry.ErrPlane))
	}

	if err := checkNodeTag(tag); err != nil {
		return ferry.ErrorAt(addr, err)
	}

	return nil
}

// checkNodeTag refuses a node word this driver cannot honour.
//
// The last of the four is the guard [carryTag] makes on the other side of the
// same question (#155): this driver's own spelling of a value is decided by the
// value's kind, and a declaration that could contradict it would let the file
// say !!int over text nothing parsed.
func checkNodeTag(tag string) error {
	switch {
	case !strings.HasPrefix(tag, bang):
		return fmt.Errorf("%w: the node tag %q does not start with %q, and a YAML tag is written !local or "+
			"!!standard", ferry.ErrPlane, tag, bang)
	case strings.Trim(tag, bang) == "":
		return fmt.Errorf("%w: the node tag %q names no type after the %q", ferry.ErrPlane, tag, bang)
	case strings.ContainsFunc(tag, unicode.IsSpace):
		return fmt.Errorf("%w: the node tag %q holds a space, and a tag ends where the value begins",
			ferry.ErrPlane, tag)
	case ownTag(tag):
		return fmt.Errorf("%w: the node tag %q is one this plane spells itself: the kind of the value decides "+
			"that tag, so declaring it could only agree with the value or contradict it", ferry.ErrPlane, tag)
	}

	return nil
}

// leafPaths is every address in the set that holds a value.
//
// The three address kinds partition the set (ADR-0016), so this is what says
// that a declaration sits somewhere a tag can be written: a section's or a
// composite's own node is a mapping or a sequence, and its members are
// elsewhere.
func leafPaths(addrs *ferry.AddressSet) map[ferry.Path]bool {
	out := make(map[ferry.Path]bool, addrs.Len())

	for m := range addrs.Seq() {
		if _, ok := m.(ferry.LeafAddr); ok {
			out[m.Path()] = true
		}
	}

	return out
}
