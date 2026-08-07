package ferryhttp

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/onhotpath/ferry"
)

// Children lists what the request holds immediately under an address, which is
// how a map-typed or slice-typed field is loaded from this plane at all: its
// members come from the request rather than from the type, so they are in no
// compiled address set and only enumeration can reveal them.
//
// Two tiers answer, and this plane is the one where they are two dimensions
// rather than two spellings. A name occurring n times answers n positions, for
// any n including one, because ?tags=a and ?tags=a&tags=b are one sequence of
// one element and one of two. Names lying under the prefix answer as well, so
// ?tags.0=a&tags.1=b reads as the same sequence and ?limits.rps=1 reads as a map
// member (#193).
//
// Where the two tiers name one position, the request is refused rather than
// resolved: ?tags=a&tags=b&tags.0=z spells position 0 twice and one of the two
// values would be lost, so it fails with [ErrTwoSpellings] at the container's
// address. Only an overlap is refused. ?tags=a&tags=b&tags.2=z extends the
// sequence and loads as three elements.
//
// It answers segments and not addresses, because the driver says how the plane
// spells its members and the schema types the child each one names (ADR-0016).
// Neither plane holds a kind of its own, so it has to decide one, and the rule
// is the one jsontext.Pointer's own documentation admits to: a child spelled as
// canonical base 10 is a position in a sequence, and everything else is a member
// of a mapping. Getting it wrong is loud rather than silent - core refuses a
// position under a mapping and a name under a sequence, naming the address - so
// a map whose keys are decimal numerals is a refusal at Load and never a quietly
// reshaped value.
//
// The result is sorted, positions numerically, so it is 0 1 2 ... 11 rather than
// the 0 1 10 11 2 that sorting the text gives, and a caller asserting on it is
// not asserting on Go's randomised map iteration order.
func (r *reader) Children(_ context.Context, addr ferry.CompositeAddr) ([]ferry.Segment, error) {
	prefix := addr.Path()

	prefixKey, err := r.keys(prefix)
	if err != nil {
		return nil, err
	}

	behind := r.positions(prefixKey)

	kids := map[ferry.Segment]struct{}{}
	maps.Copy(kids, behind)

	if err := r.fromNames(prefix, prefixKey, behind, kids); err != nil {
		return nil, err
	}

	out := slices.Collect(maps.Keys(kids))
	slices.SortFunc(out, compareSegments)

	return out, nil
}

// compareSegments orders the answer: positions before names, positions
// numerically and names bytewise.
//
// Numerically rather than as text, because sorting the text gives 0 1 10 11 2
// for twelve positions, which is the order ADR-0003 names as a subtle bug.
func compareSegments(a, b ferry.Segment) int {
	if a.Kind() != b.Kind() {
		return cmp.Compare(a.Kind(), b.Kind())
	}

	if a.Kind() == ferry.Index {
		ai, _ := position(a.Text())
		bi, _ := position(b.Text())

		return cmp.Compare(ai, bi)
	}

	return strings.Compare(a.Text(), b.Text())
}

// positions is the second dimension read as a sequence: the values a name holds,
// one position each.
//
// The position is the offset into what the name holds, so the sequence is in the
// request's own order and no rule is invented for it: net/url and net/http both
// append under a name in the order the wire carried it, and ADR-0016 asks for
// the plane's order where the plane has one.
func (r *reader) positions(prefixKey string) map[ferry.Segment]struct{} {
	out := map[ferry.Segment]struct{}{}

	for i := range r.vals[prefixKey] {
		out[ferry.IndexSegment(uint(i))] = struct{}{}
	}

	return out
}

// fromNames is the flat cut: every name that lies immediately under the prefix,
// as the child address it names.
//
// A child both tiers name is the overlap, and it is refused here rather than
// resolved. ADR-0015 places this refusal in Children because Children is where
// these addresses are minted, and it says so in terms that have since changed
// underneath it: it argued that Children was also the only call a multimap
// driver could refuse in at all, because Get carried no kind and conformance
// case 3 forbade failing at a container's Get. Neither holds now, and the
// placement survives on the first reason alone, which is the one ADR-0015 said
// it would keep even if #208 opened Get.
//
// The address the refusal carries is the container's, because core has one here
// and core's wins. The position is in the message instead.
//
// Every overlap is collected before any of them is reported, and the refusal
// names the lowest. A request can claim more than one position twice, and
// returning at the first one found would name a position picked out of a range
// over a map: the same schema and the same request would say 0 on one run and 2
// on the next, which is the instability ADR-0003 asks a driver to sort away
// (#267).
func (r *reader) fromNames(prefix ferry.Path, prefixKey string, behind, kids map[ferry.Segment]struct{}) error {
	var clashes []ferry.Segment

	for key := range r.vals {
		kid, ok := r.child(prefix, prefixKey, key)
		if !ok {
			continue
		}

		if _, both := behind[kid]; both {
			clashes = append(clashes, kid)

			continue
		}

		kids[kid] = struct{}{}
	}

	if len(clashes) == 0 {
		return nil
	}

	return twoSpellings(lowest(clashes))
}

// lowest is the smallest position among the overlaps, which is the one the
// refusal names.
//
// Smallest rather than first seen, because first seen is map order. It is also
// the one a caller fixing the request meets first, since a sequence is read from
// its own start.
func lowest(clashes []ferry.Segment) uint {
	out, _ := position(slices.MinFunc(clashes, compareSegments).Text())

	return out
}

// child is one name resolved to the immediate child of prefix that it lies
// under, and whether it lies under one at all.
//
// The static tier is a lookup and not an inverse. A name the compiled set
// determined is matched against the precomputed table and the address it came
// from is used whole, which recovers the part's own spelling; a name matching
// the prefix text but belonging to an address that is not under prefix is not a
// child and is dropped here rather than turned into one.
func (r *reader) child(prefix ferry.Path, prefixKey, key string) (ferry.Segment, bool) {
	if addr, ok := r.static[key]; ok {
		return step(prefix, addr)
	}

	rest, ok := strings.CutPrefix(key, prefixKey+r.sep)
	if !ok {
		return ferry.Segment{}, false
	}

	head, _, _ := strings.Cut(rest, r.sep)
	if head == "" {
		return ferry.Segment{}, false
	}

	if i, isPosition := position(head); isPosition {
		return ferry.IndexSegment(i), true
	}

	return ferry.NameSegment(r.p.mint(head)), true
}

// step is the segment of addr that names the immediate child of prefix it lies
// under, read off the address rather than parsed out of anything.
//
// An address is not a child of itself, so an addr equal to prefix reports false:
// the walk asks a container what is under it, and answering with the container
// would be an infinite descent.
func step(prefix, addr ferry.Path) (ferry.Segment, bool) {
	depth := 0
	pre := slices.Collect(prefix.Segments())

	for seg := range addr.Segments() {
		if depth == len(pre) {
			return seg, true
		}

		if seg != pre[depth] {
			return ferry.Segment{}, false
		}

		depth++
	}

	return ferry.Segment{}, false
}
