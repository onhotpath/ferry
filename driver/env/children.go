package env

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/onhotpath/ferry"
)

// Children lists what the environment holds immediately under an address, which
// is how a map-typed or slice-typed field is loaded from this plane at all: its
// members come from the value rather than from the type, so they are in no
// compiled address set and only enumeration can reveal them.
//
// It answers addresses and not names, because an address carries its segment
// kind (ADR-0004). This plane holds no kind of its own, so it has to decide one,
// and the rule is the one jsontext.Pointer's own documentation admits to: a
// child spelled as canonical base 10 is a position in a sequence, and everything
// else is a member of a mapping. Getting it wrong is loud rather than silent -
// core refuses a position under a mapping and a name under a sequence, naming
// the address - so a map whose keys are decimal numerals is a refusal at Load
// and never a quietly reshaped value.
//
// The segment text comes from the compiled address set wherever the address is
// in it, so a tagged field's own spelling is recovered exactly. Only what the
// value mints falls back on the canonical form, which is [Canonical]'s subject
// and where the round-trip guarantee is stated.
//
// The result is sorted segment-wise, so it is 0 1 2 ... 11 rather than the
// 0 1 10 11 2 that sorting the rendering gives, and a caller asserting on it is
// not asserting on Go's randomised map iteration order.
func (r *reader) Children(_ context.Context, prefix ferry.Path) ([]ferry.Path, error) {
	scan, err := r.scan(prefix)
	if err != nil {
		return nil, err
	}

	kids := map[ferry.Path]struct{}{}

	for key := range r.env {
		if !strings.HasPrefix(key, scan) {
			continue
		}

		if kid, ok := r.child(prefix, key, key[len(scan):]); ok {
			kids[kid] = struct{}{}
		}
	}

	out := slices.Collect(maps.Keys(kids))
	slices.SortFunc(out, ferry.Path.Compare)

	return out, nil
}

// scan is the text every name under this prefix begins with.
//
// The empty path is the whole environment rather than an error, because it is
// the one prefix that names no address: a caller asking what is under nothing is
// asking for everything, and answering with a refusal would make the root of a
// plane the one place enumeration does not work.
func (r *reader) scan(prefix ferry.Path) (string, error) {
	if prefix == (ferry.Path{}) {
		return "", nil
	}

	key, err := r.keys(prefix)
	if err != nil {
		return "", err
	}

	return key + r.cfg.sep, nil
}

// child is one environment variable name resolved to the immediate child of
// prefix that it lies under, and whether it lies under one at all.
//
// The static tier is a lookup and not an inverse. A name the compiled set
// determined is matched against the precomputed table and the address it came
// from is used whole, which recovers the segment's own spelling; a name matching
// the prefix text but belonging to an address that is not under prefix -
// /value_x against /value at the default separator - is not a child and is
// dropped here rather than turned into one.
func (r *reader) child(prefix ferry.Path, key, rest string) (ferry.Path, bool) {
	if addr, ok := r.static[key]; ok {
		return step(prefix, addr)
	}

	head, _, _ := strings.Cut(rest, r.cfg.sep)
	if head == "" {
		return ferry.Path{}, false
	}

	return r.mint(prefix, head), true
}

// mint builds a dynamic child out of the text left over after the prefix, in the
// canonical form the caller chose.
//
// A leading zero keeps a child a name rather than making it a position: an Index
// segment's text is canonical base 10, so "01" is not the rendering of any
// position and reading it as one would silently answer about /list#1 instead.
func (r *reader) mint(prefix ferry.Path, head string) ferry.Path {
	if i, ok := position(head); ok {
		return prefix.Elem(i)
	}

	if r.cfg.canon == Upper {
		return prefix.At(strings.ToUpper(head))
	}

	return prefix.At(strings.ToLower(head))
}

// position reads a child's text as a sequence position, and reports false where
// the text is not one position's canonical spelling.
func position(text string) (uint, bool) {
	if text == "" || len(text) > 1 && text[0] == '0' {
		return 0, false
	}

	i, err := strconv.ParseUint(text, base10, 0)
	if err != nil {
		return 0, false
	}

	return uint(i), true
}

// base10 is the only base a position is ever spelled in, which is what makes the
// rendering of an address unique.
const base10 = 10

// step is the immediate child of prefix that addr lies under, built by extending
// prefix with addr's own segment at that depth rather than by parsing anything.
//
// An address is not a child of itself, so an addr equal to prefix reports false:
// the walk asks a container what is under it, and answering with the container
// would be an infinite descent.
func step(prefix, addr ferry.Path) (ferry.Path, bool) {
	depth := 0
	pre := slices.Collect(prefix.Segments())

	for seg := range addr.Segments() {
		if depth == len(pre) {
			return extend(prefix, seg), true
		}

		if seg != pre[depth] {
			return ferry.Path{}, false
		}

		depth++
	}

	return ferry.Path{}, false
}

// extend appends one segment to an address, keeping the kind it already had.
//
// The position cannot fail to read: an Index segment's text is canonical base 10
// with no leading zero, minted from a uint by ferry itself, so every digit is a
// digit and every value fits the type it came from.
func extend(p ferry.Path, s ferry.Segment) ferry.Path {
	if s.Kind() == ferry.Index {
		i, _ := position(s.Text())

		return p.Elem(i)
	}

	return p.At(s.Text())
}
