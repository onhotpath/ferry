package ferryhttp

import (
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
// It answers addresses and not names, because an address carries its segment
// kind (ADR-0004). Neither plane holds a kind of its own, so it has to decide
// one, and the rule is the one jsontext.Pointer's own documentation admits to: a
// child spelled as canonical base 10 is a position in a sequence, and everything
// else is a member of a mapping. Getting it wrong is loud rather than silent -
// core refuses a position under a mapping and a name under a sequence, naming
// the address - so a map whose keys are decimal numerals is a refusal at Load
// and never a quietly reshaped value.
//
// The result is sorted segment-wise, so it is 0 1 2 ... 11 rather than the
// 0 1 10 11 2 that sorting the rendering gives, and a caller asserting on it is
// not asserting on Go's randomised map iteration order.
func (r *reader) Children(_ context.Context, prefix ferry.Path) ([]ferry.Path, error) {
	prefixKey, err := r.prefixKey(prefix)
	if err != nil {
		return nil, err
	}

	behind := r.positions(prefix, prefixKey)

	kids := map[ferry.Path]struct{}{}
	maps.Copy(kids, behind)

	if err := r.fromNames(prefix, prefixKey, behind, kids); err != nil {
		return nil, err
	}

	out := slices.Collect(maps.Keys(kids))
	slices.SortFunc(out, ferry.Path.Compare)

	return out, nil
}

// prefixKey is the name the prefix itself renders to.
//
// The empty path is the whole request rather than an error, because it is the
// one prefix that names no address: a caller asking what is under nothing is
// asking for everything, and answering with a refusal would make the root of a
// plane the one place enumeration does not work.
func (r *reader) prefixKey(prefix ferry.Path) (string, error) {
	if prefix == (ferry.Path{}) {
		return "", nil
	}

	return r.keys(prefix)
}

// positions is the second dimension read as a sequence: the values a name holds,
// one position each.
//
// Enumerating a name is also what settles the question [reader.Get] could not
// answer, so the record it left is dropped here: something read this name as a
// sequence, which is what it is.
//
// The root has no second dimension. A request may carry a parameter with an
// empty name - "?=v" is one - and the root is not a sequence because of it.
func (r *reader) positions(prefix ferry.Path, prefixKey string) map[ferry.Path]struct{} {
	out := map[ferry.Path]struct{}{}

	if prefix == (ferry.Path{}) {
		return out
	}

	vs := r.vals[prefixKey]
	if len(vs) == 0 {
		return out
	}

	delete(r.hid, prefixKey)

	for i := range vs {
		out[prefix.Elem(uint(i))] = struct{}{}
	}

	return out
}

// fromNames is the flat cut: every name that lies immediately under the prefix,
// as the child address it names.
//
// A child both tiers name is the overlap, and it is refused here rather than
// resolved. It is the one refusal this driver can make during the walk at all,
// because being asked for children at an address is core saying that address is
// a dynamic container (ADR-0003, amended under #207), and conformance case 3
// forbids a refusal at a container's Get.
//
// The address the refusal carries is the container's, because core has one here
// and core's wins. The position is in the message instead.
func (r *reader) fromNames(prefix ferry.Path, prefixKey string, behind, kids map[ferry.Path]struct{}) error {
	for key := range r.vals {
		kid, ok := r.child(prefix, prefixKey, key)
		if !ok {
			continue
		}

		if _, both := behind[kid]; both {
			_, i, _ := splitIndex(kid)

			return twoSpellings(i)
		}

		kids[kid] = struct{}{}
	}

	return nil
}

// child is one name resolved to the immediate child of prefix that it lies
// under, and whether it lies under one at all.
//
// The static tier is a lookup and not an inverse. A name the compiled set
// determined is matched against the precomputed table and the address it came
// from is used whole, which recovers the part's own spelling; a name matching
// the prefix text but belonging to an address that is not under prefix is not a
// child and is dropped here rather than turned into one.
func (r *reader) child(prefix ferry.Path, prefixKey, key string) (ferry.Path, bool) {
	if addr, ok := r.static[key]; ok {
		return step(prefix, addr)
	}

	rest, ok := r.under(prefix, prefixKey, key)
	if !ok {
		return ferry.Path{}, false
	}

	head, _, _ := strings.Cut(rest, r.sep)
	if head == "" {
		return ferry.Path{}, false
	}

	if i, isPosition := position(head); isPosition {
		return prefix.Elem(i), true
	}

	return prefix.At(r.p.mint(head)), true
}

// under is the text left over after the prefix, and whether the name lies under
// it at all. At the root every name does, and the whole name is what is left.
func (r *reader) under(prefix ferry.Path, prefixKey, key string) (string, bool) {
	if prefix == (ferry.Path{}) {
		return key, true
	}

	return strings.CutPrefix(key, prefixKey+r.sep)
}

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
