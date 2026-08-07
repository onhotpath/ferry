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

// spellNumber writes the plane's own spelling of the number, unparsed.
//
// The text is whatever ferry produced - "0", "-0", "1e-45", "+Inf", "NaN",
// "18446744073709551615" - and it is written through unmodified, because
// ADR-0004 carries a number as source text precisely so that no stage in the
// middle rounds it.
func spellNumber(v ferry.Value) (*yamlv3.Node, error) {
	text, err := v.AsNumber()
	if err != nil {
		return nil, err
	}

	return leaf(numberTag(text), text), nil
}

// numberTag guesses which of YAML's two numeric tags the text belongs to, and
// only the file's tidiness rests on the guess.
//
// The emitter drops a tag the text would resolve to anyway and keeps one it
// would not, and both tags read back as [ferry.KindNumber] here, so a wrong
// guess costs a visible !!int or !!float in the file and never a wrong kind.
// What the tag does buy is the values YAML's own resolution will not take:
// +Inf, -Inf and NaN are strings to the resolver, so without a tag on them a
// float would come back as a String and fail its codec.
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
// A mapping or a sequence answers Absent, and so does a node that is not there
// at all. ADR-0003 reads a composite one element at a time, so there is no
// group value for a container to hold, and a driver answering one is a driver
// core cannot interpret.
//
// The kind comes from [kindOf], so the read and the guard [carryTag] applies
// on the way out cannot drift apart: a tag is carried across a save exactly
// where the kind it reads as has not changed.
func valueOf(n *yamlv3.Node) (ferry.Value, error) {
	if n == nil || n.Kind != yamlv3.ScalarNode {
		return ferry.Value{}, nil
	}

	switch kindOf(n.Tag) {
	case ferry.KindNull:
		return ferry.Null, nil
	case ferry.KindBool:
		return boolOf(n.Value)
	case ferry.KindNumber:
		return ferry.Number(n.Value), nil
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
// tightened once it has shipped. Reading a tag as some other kind is
// interpretation, needs a mechanism to say so, and is #156.
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
		return ferry.Value{}, fmt.Errorf("%w: the plane tagged a scalar !!bool that is neither true nor false",
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
