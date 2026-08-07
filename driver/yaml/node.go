package yaml

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	yamlv3 "go.yaml.in/yaml/v3"

	"github.com/onhotpath/ferry"
)

// The tags this driver reads and writes.
//
// A tag is what carries the plane-side type information ADR-0004 says a typed
// boundary needs, and YAML resolves one for every plain scalar: 8080 is !!int,
// "8080" is !!str, true is !!bool and null is !!null. That resolution is the
// whole reason this plane can produce six kinds where a flat plane produces
// one.
const (
	nullTag   = "!!null"
	boolTag   = "!!bool"
	intTag    = "!!int"
	floatTag  = "!!float"
	strTag    = "!!str"
	binaryTag = "!!binary"
	mapTag    = "!!map"
	seqTag    = "!!seq"
)

// mergeTag is the tag YAML resolves for a merge key. This driver neither reads
// nor writes it: it is the one tag a save has to take off a node it is not
// otherwise touching, for the reason [untagMerges] records.
const mergeTag = "!!merge"

// nullText is how this driver writes a Null. The emitter would accept an empty
// scalar for it and write `key:` with nothing after the colon, which reads back
// identically and reads worse to a human, so the word is written out.
const nullText = "null"

// leaf builds the scalar node one [ferry.Value] becomes.
//
// The style is left at zero on purpose, which is what lets the emitter drop a
// tag that the value would resolve to anyway: !!int 8080 is written 8080 and
// !!str 8080 is written "8080", and each reads back as the kind it was written
// as. A style copied from whatever node used to be at the address would decide
// the new value's type instead - a stale double-quoted style over a Number is a
// string on the way back in - which is why a write never keeps one.
func leaf(tag, text string) *yamlv3.Node {
	return &yamlv3.Node{Kind: yamlv3.ScalarNode, Tag: tag, Value: text}
}

// spell is this driver's half of the boundary: the node one Value is written
// as. What it produces is ADR-0013's compatibility promise for this plane, and
// the golden artefacts in the conformance suite are what pin it.
func spell(v ferry.Value) (*yamlv3.Node, error) {
	switch v.Kind() {
	case ferry.KindNull:
		return leaf(nullTag, nullText), nil
	case ferry.KindBool:
		return spellBool(v)
	case ferry.KindNumber:
		return spellNumber(v)
	case ferry.KindString:
		return spellString(v)
	case ferry.KindBytes:
		return spellBytes(v)
	case ferry.KindAbsent:
		return nil, fmt.Errorf("%w: an absent value has nothing to write, and an address ferry omits gets no "+
			"Set call rather than an explicit null", ferry.ErrValue)
	default:
		return nil, fmt.Errorf("%w: this plane has no spelling for kind %s", ferry.ErrValue, v.Kind())
	}
}

// spellBool writes the canonical true or false, which is the only text a
// [ferry.KindBool] carries.
func spellBool(v ferry.Value) (*yamlv3.Node, error) {
	b, err := v.AsBool()
	if err != nil {
		return nil, err
	}

	return leaf(boolTag, strconv.FormatBool(b)), nil
}

// spellNumber writes the number in this plane's own spelling of it.
//
// The text is whatever ferry produced - "0", "-0", "1e-45", "+Inf", "NaN",
// "18446744073709551615" - and all but the three worded floats are written
// through unmodified, because ADR-0004 carries a number as source text
// precisely so that no stage in the middle rounds it. The three that do move
// are the ones Go and YAML spell differently, and [numbers] is where the two
// vocabularies meet (#259).
func spellNumber(v ferry.Value) (*yamlv3.Node, error) {
	text, err := v.AsNumber()
	if err != nil {
		return nil, err
	}

	written, err := numbers.Render(text)
	if err != nil {
		return nil, err
	}

	return leaf(numberTag(written), written), nil
}

// numberOf is the read half of the same seam: what the document spelled,
// canonical, so that a leaf's own base-10 parser sees a number it can read
// (#259).
func numberOf(text string) (ferry.Value, error) {
	got, err := numbers.Parse(text)
	if err != nil {
		return ferry.Value{}, err
	}

	return ferry.Number(got), nil
}

// numberTag guesses which of YAML's two numeric tags the text belongs to, and
// only the file's tidiness rests on the guess.
//
// The emitter drops a tag the text would resolve to anyway and keeps one it
// would not, and both tags read back as [ferry.KindNumber] here, so a wrong
// guess costs a visible !!int or !!float in the file and never a wrong kind.
//
// It is given the text after [numbers] has spelled it, which is why .inf and
// .nan reach it rather than +Inf and NaN: both contain a byte this guess reads
// as floating, and both are what YAML's own resolution takes, so the tag it
// chooses is one the emitter can drop again (#259).
func numberTag(text string) string {
	if strings.ContainsAny(text, ".eEnN") {
		return floatTag
	}

	return intTag
}

// spellString writes the string, and refuses one this plane has no way to hold.
//
// A Go string is a byte sequence and is not required to be UTF-8, which ADR-0005
// states as a rule and pins with the value "\xff\xfe". A YAML string is Unicode,
// the stream itself has to be valid UTF-8, and the emitter refuses invalid UTF-8
// under !!str by name. So this plane carries KindString and cannot carry every
// value of it, and ADR-0005 records that as a known limitation of this driver.
//
// The two alternatives were both worse. Writing it as !!binary reads back as
// [ferry.KindBytes] and reaches a codec that asked for a string, which is a
// silent change of kind. Spelling it under a tag of this driver's own writes
// ferry's naming into an operator's data file and pins it as a compatibility
// promise, which ADR-0003 already refuses one level up for the canonical
// rendering: ferry's own naming is not a plane key and no driver may write it
// into a plane as one. A tag is worse than a key, because it is also
// configurable in principle and nothing here could honour a change to it.
//
// The class is [ferry.ErrValue] and not [ferry.ErrPlane]: the plane is reachable
// and writable, and what failed is one value at one address not fitting the
// format. The address comes from [writer.Set], which is where the caller's
// address is known.
func spellString(v ferry.Value) (*yamlv3.Node, error) {
	s, err := v.AsString()
	if err != nil {
		return nil, err
	}

	if !utf8.ValidString(s) {
		return nil, fmt.Errorf("%w: a YAML string is Unicode and this one is not valid UTF-8", ferry.ErrValue)
	}

	return leaf(strTag, s), nil
}

// spellBytes writes bytes base64 under YAML's !!binary.
//
// The encoding is not decoration. A prototype wrote the raw bytes under the
// tag and read them back the same wrong way, so the pair was self-consistent
// and round-tripped, and what caught it was the emitter refusing to emit
// invalid !!binary (ADR-0005). That refusal is still there, and it is a net
// under exactly half of the defect: it fires only where the raw bytes are not
// valid UTF-8. The golden artefact is what covers the other half.
func spellBytes(v ferry.Value) (*yamlv3.Node, error) {
	b, err := v.AsBytes()
	if err != nil {
		return nil, err
	}

	return leaf(binaryTag, base64.StdEncoding.EncodeToString(b)), nil
}

// valueOf is the read half of the boundary: what the plane holds at one node.
//
// A node that is not there at all answers Absent. A mapping or a sequence is a
// refusal, because this node sits at an address the schema types as a leaf and
// the document holds a container there: the two disagree about the shape of the
// data, and that is a fact about the plane rather than an absence.
//
// Answering Absent for it was #252. Core writes nothing at an absent address,
// so the walk filled the field with the Go zero and the load returned nil:
// measured, `limits: {http: {port: 1}, rps: "9"}` into a map[string]string
// loaded {"http": "", "rps": "9"} with no error, and the port was gone.
//
// The kind of a scalar comes from [kindOf], so the read and the guard [carryTag]
// applies on the way out cannot drift apart: a tag is carried across a save
// exactly where the kind it reads as has not changed.
func valueOf(n *yamlv3.Node) (ferry.Value, error) {
	if n == nil {
		return ferry.Value{}, nil
	}

	if n.Kind != yamlv3.ScalarNode {
		return ferry.Value{}, fmt.Errorf("%w: the document holds %s here and the destination takes a single "+
			"value: model the field as a map or a struct, or change the document", ferry.ErrValue, shapeOf(n))
	}

	switch kindOf(n.Tag) {
	case ferry.KindNull:
		return ferry.Null, nil
	case ferry.KindBool:
		return boolOf(n.Value)
	case ferry.KindNumber:
		return numberOf(n.Value)
	case ferry.KindBytes:
		return bytesOf(n.Value)
	default:
		return ferry.String(n.Value), nil
	}
}

// kindOf is the kind this plane reads one tag as.
//
// A tag this driver has no arm for is a String, and that is the deliberate half
// of #155 rather than a fall-through nobody chose: an unhandled tag is carried
// and not interpreted, so `when: !!timestamp 2026-08-04` arrives as the text
// the operator wrote and reaches whichever codec the field declared, and the
// save writes the tag back untouched. Refusing it instead would fail a document
// this driver can read perfectly well, and a permissive default cannot be
// tightened once it has shipped.
//
// It stayed a String when #156 landed, and that is what makes the node word
// work rather than a gap in it: a field says which tag its value is written
// under, the value comes back as the text under it, and the two agree. Reading
// a tag as some other kind would be interpretation, and nothing asks for it.
func kindOf(tag string) ferry.VKind {
	switch tag {
	case nullTag:
		return ferry.KindNull
	case boolTag:
		return ferry.KindBool
	case intTag, floatTag:
		return ferry.KindNumber
	case binaryTag:
		return ferry.KindBytes
	default:
		return ferry.KindString
	}
}

// ownTag says whether this driver has a spelling of its own for a tag.
//
// The empty tag is one of them, and deliberately: a node the merge minted, or a
// merge key [untagMerges] cleared, carries no tag the operator wrote, and
// leaving it empty would hand the emitter's own resolution the last word on the
// value's type.
func ownTag(tag string) bool {
	switch tag {
	case "", nullTag, boolTag, intTag, floatTag, strTag, binaryTag, mapTag, seqTag, mergeTag:
		return true
	default:
		return false
	}
}

// carryTag puts the operator's tag on the node replacing theirs, where this
// driver has no spelling of its own for it (#155).
//
// It runs where nothing declared a tag at the address. A field that did wins,
// for the reason [writer.retag] records: preserving is what is left to do when
// nothing said what the address should be written under (#156).
//
// A tag at an address is the operator's in the way an anchor turned out to be
// (#196). This driver never wrote !!timestamp, reads it as a String, and used
// to drop it on a save, so a document a load and a dump had merely passed
// through came back holding less than it went in with.
//
// The two guards are what make carrying safe. A tag this driver spells itself
// loses to that spelling, or a value could be written under a tag that
// contradicts it. And a tag whose kind is no longer the value's is stale in the
// way a copied style is (see [leaf]), because the read would then answer a kind
// the value is not.
//
// TaggedStyle goes with it, and it is the one style bit a write ever keeps. It
// is not the operator's quoting, which is what [leaf] refuses to copy: it says
// the tag is written out rather than left to the reader's own resolution, so
// the line comes back spelled as it was written rather than as whatever YAML
// would have resolved the bare text to.
func carryTag(at, spelled *yamlv3.Node) {
	if at.Kind != yamlv3.ScalarNode || ownTag(at.Tag) || kindOf(at.Tag) != kindOf(spelled.Tag) {
		return
	}

	spelled.Tag, spelled.Style = at.Tag, spelled.Style|yamlv3.TaggedStyle
}

// presenceOf is the container half of the read boundary: what the plane holds
// at an address whose children come from the type or from the document, rather
// than a value of its own (ADR-0016).
//
// The four observations are four answers and they are the ones this plane can
// actually make. A key that is not there is absent. An explicit `~` or `null` is
// the document saying the section is there and is nothing. A mapping or a
// sequence is present, including an empty one: `opts: {}` is what makes a
// present-but-empty section survive a reload, which is the whole reason the
// probe exists. A scalar is the document and the destination disagreeing, and it
// is refused for the reason [valueOf] refuses the mirror of it.
func presenceOf(n *yamlv3.Node) (ferry.SectionInfo, error) {
	switch {
	case n == nil:
		return ferry.SectionAbsent, nil
	case n.Kind == yamlv3.MappingNode, n.Kind == yamlv3.SequenceNode:
		return ferry.SectionPresent, nil
	case n.Kind == yamlv3.ScalarNode && n.Tag == nullTag:
		return ferry.SectionNull, nil
	default:
		return ferry.SectionAbsent, fmt.Errorf("%w: the document holds a single value here and the destination "+
			"takes a container: model the field as a leaf, or change the document", ferry.ErrValue)
	}
}

// shapeOf names what a node is, in the words a reader of the message thinks in
// rather than the parser's.
//
// It names no value the plane supplied, which ADR-0011 makes a total rule: the
// shape is structure, and the address core attaches says where.
func shapeOf(n *yamlv3.Node) string {
	switch n.Kind {
	case yamlv3.MappingNode:
		return "a mapping"
	case yamlv3.SequenceNode:
		return "a sequence"
	case yamlv3.AliasNode:
		return "an alias that resolves to nothing"
	default:
		return "a document"
	}
}

// boolOf reads a !!bool, refusing a spelling YAML's own resolution would never
// have produced.
//
// The refusal is the point: an explicit `!!bool yes` is a tag the operator
// wrote by hand, and answering false for it would be ferry deciding that an
// unreadable value is a value.
func boolOf(text string) (ferry.Value, error) {
	switch {
	case strings.EqualFold(text, "true"):
		return ferry.Bool(true), nil
	case strings.EqualFold(text, "false"):
		return ferry.Bool(false), nil
	default:
		return ferry.Value{}, fmt.Errorf("%w: the document tags a scalar !!bool that is neither true nor false",
			ferry.ErrValue)
	}
}

// bytesOf decodes a !!binary.
func bytesOf(text string) (ferry.Value, error) {
	b, err := decode64(text)
	if err != nil {
		return ferry.Value{}, err
	}

	return ferry.Bytes(b), nil
}

// decode64 undoes the base64 a !!binary is written in.
//
// The whitespace is stripped first because YAML lets a !!binary scalar be
// folded across lines, and an operator's hand-written or generated file is
// entitled to have been. The failure names no text: base64's own error reports
// a position and never the content, which is what makes it safe to keep in the
// chain under ADR-0011's rule that a message never carries a value the plane
// supplied.
func decode64(text string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text), ""))
	if err != nil {
		return nil, fmt.Errorf("%w: the scalar is tagged as base64 and did not decode: %w", ferry.ErrValue, err)
	}

	return b, nil
}
