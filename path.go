package ferry

import (
	"cmp"
	"iter"
	"slices"
	"strconv"
	"strings"
)

// SegmentKind says what a segment of an address names, and the set is closed at
// two: adding a third kind is an amendment to ADR-0003 rather than an
// implementation choice.
//
// The kind is load-bearing and not decoration. Given only text, an emitter has
// one signal for "is this container a list", which is whether the segment looks
// like a base-10 integer, and that signal turns a map holding the key "0" into a
// sequence and destroys the key text. That is the limitation jsontext.Pointer's
// own godoc states about itself, and it is why ferry carries the kind instead of
// recovering it from the text.
type SegmentKind uint8

const (
	// Name is an object member, a struct field or a map key.
	Name SegmentKind = iota
	// Index is a position in a sequence.
	Index
)

// String names the kind for diagnostics. It is not how the kind is rendered
// inside an address, which spells it with a delimiter byte of its own.
func (k SegmentKind) String() string {
	switch k {
	case Name:
		return "Name"
	case Index:
		return "Index"
	default:
		return "SegmentKind(" + strconv.Itoa(int(k)) + ")"
	}
}

// Segment is one step of an address: a kind and a text.
//
// A driver reads both. The kind is what lets it decide whether the container
// above this step is a mapping or a sequence without inspecting the text, and
// the text is the user's, byte for byte.
type Segment struct {
	kind SegmentKind
	text string
}

// Kind reports whether this step names a member or a position.
func (s Segment) Kind() SegmentKind { return s.kind }

// Text is the segment's text exactly as the schema or the value spelled it,
// unescaped. Core compares it by exact byte equality and never folds or
// normalises it, so a driver receives the original spelling and can restore it
// on the way back out.
func (s Segment) Text() string { return s.text }

// Path is the address of a place a plane can be asked for a Value, or handed
// one: an ordered sequence of segments, each carrying a kind and a text
// (ADR-0003).
//
// A Path is comparable, so it is a map key and a set element with no encoding
// step at the call site, and identity is ==. That holds because the canonical
// text rendering has a unique representation: two addresses render alike exactly
// when they are equal. String returns that rendering.
//
// The rendering is for identity, not for ordering. Sorted as text, twelve
// indices give 0 1 10 11 2 3 ...; Compare orders segment-wise and gives
// 0 1 2 ... 11, which is the order a human diffing a dumped file expects.
// Wherever ferry enumerates addresses it sorts with Compare.
//
// The rendering is not a plane key, and no driver may write it into a plane as
// one. Core never joins segments, because a separator is plane knowledge:
// flattening is the driver's, always. An environment driver joins with _, a
// YAML driver walks the segments as a tree, and neither spelling is core's
// business.
//
// The zero Path has no segments. An address has at least one.
type Path struct {
	// rendered is the canonical rendering and the whole of the value. A
	// []string field would cost comparability, which is the property the type
	// exists for, and every call site would encode anyway.
	rendered string
}

// Path is usable directly as a map key and a set element. That is a
// compile-time property rather than a documented promise, and this is where it
// is checked (ADR-0003).
var _ map[Path]struct{}

// The canonical syntax.
//
// RFC 6901 is rejected as the wire form because it cannot express a segment
// kind, which is the whole reason the kind exists (ADR-0003). Its escaping model
// is taken as it stands, because escaping a separator and an escape character is
// a solved problem: a Name segment is introduced by '/' and an Index segment by
// '#', so the kind is read off the rendering rather than guessed from the text,
// and the three structural bytes escape as ~0, ~1 and ~2.
//
// Uniqueness follows from that. Escaping is injective and leaves no bare
// delimiter inside a segment, so the delimiters split a rendering in exactly one
// way, and an Index segment's text is canonical base-10 with no leading zero.
const (
	nameSep  = '/'
	indexSep = '#'
	escape   = '~'
	delims   = "/#"
)

// At builds an address out of Name segments, which is what a struct field, an
// object member or a map key contributes.
//
// It is the ordinary way to write a literal address: At("db", "host") is
// /db/host. With no arguments it is the empty path, which is not an address.
func At(names ...string) Path { return Path{}.At(names...) }

// At extends the address with Name segments, leaving the receiver untouched.
func (p Path) At(names ...string) Path {
	b := make([]byte, 0, len(p.rendered)+len(names)*segmentHint)
	b = append(b, p.rendered...)

	for _, n := range names {
		b = append(b, nameSep)
		b = appendEscaped(b, n)
	}

	return Path{rendered: string(b)}
}

// Elem extends the address with an Index segment, a position in a sequence.
//
// The position is unsigned because a negative one has no meaning and ferry
// itself never panics (ADR-0011): the constraint is carried by the type rather
// than by a check the caller can trip over.
func (p Path) Elem(i uint) Path {
	b := append([]byte(p.rendered), indexSep)
	b = strconv.AppendUint(b, uint64(i), base10)

	return Path{rendered: string(b)}
}

// shape extends the address with the segment every member of a dynamic
// composite shares, which is what a slice element and a map value are compiled
// under (ADR-0006's /servers/*/port).
//
// A shape is not an address. Nothing is at it, no driver is ever handed one,
// and the walk realises it per member from the value; it exists so that a
// composite whose members come from the value is compiled once rather than per
// element. "*" is ordinary segment text under this package's escaping model
// rather than a marker, which is exactly why a shape may not leave the schema
// (ADR-0003).
func (p Path) shape() Path { return p.At(wildcard) }

// wildcard is the shape segment's text. It is spelled as ADR-0006 spells it, so
// a schema-internal lookup key reads the way the ADR that owns it writes.
const wildcard = "*"

// concat appends q's segments to p.
//
// It is a byte concatenation, which is exact rather than convenient: a
// rendering carries its own delimiters and escaping leaves no bare one inside a
// segment, so joining two renderings joins two segment sequences and can do
// nothing else.
func (p Path) concat(q Path) Path { return Path{rendered: p.rendered + q.rendered} }

// below is the part of p that extends prefix.
//
// The caller has established that prefix is a prefix of p, which the walk gets
// for free: a member's compiled address extends its container's by construction.
func (p Path) below(prefix Path) Path { return Path{rendered: p.rendered[len(prefix.rendered):]} }

// String is the canonical rendering: /db/host for two Name segments, /tags#0 for
// a Name segment followed by an Index segment.
//
// It identifies the address and nothing more. It is not a plane key, no driver
// may write it into a plane as one, and sorting it is not this type's ordering.
func (p Path) String() string { return p.rendered }

// Segments enumerates the address left to right.
//
// It is an iterator rather than a slice because a driver's key function runs
// over every address of a schema at Bind, and the address holds its rendering
// rather than a segment slice.
func (p Path) Segments() iter.Seq[Segment] {
	return func(yield func(Segment) bool) {
		for rest := p.rendered; rest != ""; {
			var seg Segment
			seg, rest, _ = cutSegment(rest)

			if !yield(seg) {
				return
			}
		}
	}
}

// Compare orders two addresses segment-wise, comparing Name segments by exact
// bytes and Index segments numerically, and orders a prefix before what extends
// it. It reports -1, 0 or +1 and is a total order, so slices.SortFunc takes it
// directly as Path.Compare.
//
// This is not the order of the renderings, and the difference is the point:
// sorted as text, twelve indices give 0 1 10 11 2 3 ..., and /a-x sorts before
// /a/b because a separator byte sorts against ordinary text (ADR-0003).
func (p Path) Compare(q Path) int {
	a, b := p.rendered, q.rendered

	for a != "" && b != "" {
		var sa, sb Segment
		sa, a, _ = cutSegment(a)
		sb, b, _ = cutSegment(b)

		if c := compareSegments(sa, sb); c != 0 {
			return c
		}
	}

	// Whichever rendering still has bytes left has more segments, so the other
	// one is a prefix of it and sorts first.
	return cmp.Compare(len(a), len(b))
}

// isPrefixOf reports whether p is a prefix of q at a segment boundary. A path
// is a prefix of itself, which is what makes ADR-0003's prefix-free rule
// subsume exact duplicates rather than needing a second check beside it.
//
// It reads the renderings because escaping leaves no bare delimiter inside a
// segment: a rendering that starts with another one and continues with a
// delimiter continues at a boundary and never in the middle of a segment. That
// is why /a-b is not under /a while /a/b is.
func (p Path) isPrefixOf(q Path) bool {
	rest, ok := strings.CutPrefix(q.rendered, p.rendered)
	if !ok {
		return false
	}

	return rest == "" || rest[0] == nameSep || rest[0] == indexSep
}

func compareSegments(a, b Segment) int {
	if a.kind != b.kind {
		return cmp.Compare(a.kind, b.kind)
	}

	if a.kind == Index {
		return compareIndexText(a.text, b.text)
	}

	return strings.Compare(a.text, b.text)
}

// compareIndexText compares two Index texts numerically without parsing them.
// Index text is canonical base-10 with no leading zero, so the longer number is
// the larger one and equal lengths compare bytewise. That also holds for indices
// no integer type could carry.
func compareIndexText(a, b string) int {
	if len(a) != len(b) {
		return cmp.Compare(len(a), len(b))
	}

	return strings.Compare(a, b)
}

const (
	// segmentHint is the capacity guess per Name segment, chosen so an ordinary
	// config key needs no second allocation.
	segmentHint = 12
	// base10 is the only base an Index segment is ever written in, because the
	// rendering has to be unique and two spellings of one position are not.
	base10 = 10
)

func appendEscaped(dst []byte, text string) []byte {
	for i := range len(text) {
		switch c := text[i]; c {
		case escape:
			dst = append(dst, escape, '0')
		case nameSep:
			dst = append(dst, escape, '1')
		case indexSep:
			dst = append(dst, escape, '2')
		default:
			dst = append(dst, c)
		}
	}

	return dst
}

// cutSegment splits the first segment off a rendering, returning it, what
// follows, and whether the segment's escaping was well formed. The caller has
// already established that s opens with a delimiter.
func cutSegment(s string) (Segment, string, bool) {
	kind := Name
	if s[0] == indexSep {
		kind = Index
	}

	text, rest := s[1:], ""
	if i := strings.IndexAny(text, delims); i >= 0 {
		text, rest = text[:i], text[i:]
	}

	unescaped, ok := unescapeText(text)

	return Segment{kind: kind, text: unescaped}, rest, ok
}

func unescapeText(s string) (string, bool) {
	i := strings.IndexByte(s, escape)
	if i < 0 {
		return s, true
	}

	b := make([]byte, 0, len(s))
	b = append(b, s[:i]...)

	for i < len(s) {
		c, n, ok := decodeEscape(s[i:])
		if !ok {
			return "", false
		}

		b = append(b, c)
		i += n
	}

	return string(b), true
}

// decodeEscape decodes the byte s opens with, returning it, how many bytes it
// was written as, and whether it was a legal escape.
func decodeEscape(s string) (decoded byte, width int, ok bool) {
	if s[0] != escape {
		return s[0], 1, true
	}

	if len(s) < 2 {
		return 0, 0, false
	}

	switch s[1] {
	case '0':
		return escape, 2, true
	case '1':
		return nameSep, 2, true
	case '2':
		return indexSep, 2, true
	default:
		return 0, 0, false
	}
}

// parsePath recovers an address from its canonical rendering, reporting whether
// the text was one.
//
// It is unexported on purpose. Nothing in the published contract parses an
// address: core mints addresses from the schema, and a driver that enumerates
// builds them with At and Elem from what its plane told it. What parsePath is
// for is the round-trip half of the canonical form, which is a property of the
// rendering and is asserted rather than promised.
func parsePath(s string) (Path, bool) {
	if s != "" && s[0] != nameSep && s[0] != indexSep {
		return Path{}, false
	}

	for rest := s; rest != ""; {
		var (
			seg Segment
			ok  bool
		)

		if seg, rest, ok = cutSegment(rest); !ok || !canonicalSegment(seg) {
			return Path{}, false
		}
	}

	return Path{rendered: s}, true
}

// canonicalSegment reports whether a decoded segment is one the renderer could
// have produced. Only an Index segment can fail: its text is base-10 with no
// leading zero, which is what makes the rendering unique and lets Compare order
// indices numerically without parsing them.
func canonicalSegment(s Segment) bool {
	if s.kind != Index {
		return true
	}

	if s.text == "" {
		return false
	}

	if s.text[0] == '0' {
		return s.text == "0"
	}

	for i := range len(s.text) {
		if s.text[i] < '0' || s.text[i] > '9' {
			return false
		}
	}

	return true
}

// AddressSet is the set of addresses a compiled schema determines, and it is
// what a driver's Bind is handed (ADR-0004). Holding it before any I/O is what
// lets a driver precompute its plane keys once per schema, check that they are
// legal on its plane, and check that its key function is injective over the set.
//
// It is sorted segment-wise, which is what lets a driver that wants locality
// sort for itself.
//
// It contains every leaf address the type determines plus every container
// address, and it never contains a wildcard shape: a wildcard is a
// schema-internal lookup key with nothing at it, and every member of this set is
// one a driver can fetch, write, name and check. Which of its members are
// containers is one bit per address the compiler holds and this type does not
// expose (ADR-0003).
//
// Addresses minted from a value, a map key or a sequence index, do not exist
// until there is a value and are not in it.
type AddressSet struct {
	// addrs is sorted by Path.Compare and holds no duplicates.
	addrs []Path
}

// NewAddressSet builds the set from the addresses a compiled schema determined,
// sorting them segment-wise. It copies what it is given, so the caller may keep
// and reuse the slice.
//
// Equal addresses collapse, because this is a set: identity is ==, so two equal
// addresses are one place and nothing is lost by holding it once.
func NewAddressSet(addrs ...Path) *AddressSet {
	sorted := slices.Clone(addrs)
	slices.SortFunc(sorted, Path.Compare)

	return &AddressSet{addrs: slices.Compact(sorted)}
}

// Len is how many addresses the set holds.
func (a *AddressSet) Len() int { return len(a.addrs) }

// All enumerates the set segment-wise. The order is the set's own and is stable
// across builds of the same schema, so a driver may key a table by position.
func (a *AddressSet) All() iter.Seq[Path] { return slices.Values(a.addrs) }

// Has reports whether the set holds this address.
func (a *AddressSet) Has(addr Path) bool {
	_, ok := slices.BinarySearchFunc(a.addrs, addr, Path.Compare)

	return ok
}
