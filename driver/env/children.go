package env

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/onhotpath/ferry"
)

// Children lists what the environment holds immediately under a container whose
// members come from the value, which is how a map-typed or slice-typed field is
// loaded from this plane at all: its members are in no compiled address set, and
// only enumeration can reveal them.
//
// It answers segments, so the plane says how it spells each member and the
// schema decides what is at it. A member spelled as canonical base 10 is a
// position in a sequence and everything else is a member of a mapping. Getting
// it wrong is loud rather than silent, because core refuses a position under a
// mapping and a name under a sequence.
//
// A member's spelling is the canonical form, which is [Canonical]'s subject and
// where the round-trip guarantee is stated: the name was upper-cased on the way
// in and there is no way back from a fold that has already happened.
//
// A variable that reaches deeper than a member is still a member here, because a
// map of maps is spelled exactly that way and this plane cannot tell one from
// the other. Which of the two it was is settled by the question core asks next:
// see [reader.Get].
//
// The result is sorted, so it is 0 1 2 ... 11 rather than the 0 1 10 11 2 that
// sorting text gives, and a caller asserting on it is not asserting on Go's
// randomised map iteration order.
func (r *reader) Children(_ context.Context, addr ferry.CompositeAddr) ([]ferry.Segment, error) {
	scan, err := r.scan(addr)
	if err != nil {
		return nil, err
	}

	kids := map[ferry.Segment]struct{}{}

	for key := range r.env {
		if !strings.HasPrefix(key, scan) {
			continue
		}

		if kid, ok := r.mint(key[len(scan):]); ok {
			kids[kid] = struct{}{}
		}
	}

	out := slices.Collect(maps.Keys(kids))
	slices.SortFunc(out, compareSegments)

	return out, nil
}

// scan is the text every name under this container begins with.
func (r *reader) scan(addr ferry.CompositeAddr) (string, error) {
	key, err := r.keys(addr.Path())
	if err != nil {
		return "", err
	}

	return key + r.cfg.sep, nil
}

// mint builds one member out of the text left over after the container's own
// name, in the canonical form the caller chose, and reports whether there was
// one.
//
// A leading zero keeps a member a name rather than making it a position: an
// Index segment's text is canonical base 10, so "01" is not the spelling of any
// position and reading it as one would silently answer about position 1 instead.
func (r *reader) mint(rest string) (ferry.Segment, bool) {
	head, _, _ := strings.Cut(rest, r.cfg.sep)
	if head == "" {
		return ferry.Segment{}, false
	}

	if i, ok := position(head); ok {
		return ferry.IndexSegment(i), true
	}

	if r.cfg.canon == Upper {
		return ferry.NameSegment(strings.ToUpper(head)), true
	}

	return ferry.NameSegment(strings.ToLower(head)), true
}

// compareSegments orders two members the way core orders the addresses they
// name: by kind first, and a position numerically rather than as text.
//
// It is the driver's own because ferry publishes the ordering on [ferry.Path]
// and not on a bare segment, and enumeration answers segments now (ADR-0016).
func compareSegments(a, b ferry.Segment) int {
	if a.Kind() != b.Kind() {
		return int(a.Kind()) - int(b.Kind())
	}

	if a.Kind() == ferry.Index {
		return comparePositions(a.Text(), b.Text())
	}

	return strings.Compare(a.Text(), b.Text())
}

// comparePositions compares two positions numerically without parsing them.
// A position's text is canonical base 10 with no leading zero, so the longer
// number is the larger one and equal lengths compare bytewise.
func comparePositions(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}

	return strings.Compare(a, b)
}

// position reads a member's text as a sequence position, and reports false where
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
