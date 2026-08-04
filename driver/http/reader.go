package ferryhttp

import (
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/onhotpath/ferry"
)

// reader is one open over one request's parameters or fields.
//
// Everything in it is allocated here rather than shared with the binding it came
// from, which is what lets one binding serve every goroutine net/http runs a
// handler in: the binding holds the name table and never changes, and this holds
// what one load observed.
type reader struct {
	p      plane
	sep    string
	keys   ferry.KeyFunc
	static map[string]ferry.Path
	vals   values

	// hid is every name this load answered Absent at because it occurs more
	// than once, and which nothing has enumerated since. A name that is later
	// enumerated is deleted from it, so what is left when the load closes is
	// exactly the names that were sequences and were read as single values.
	hid map[string]ferry.Path
}

func newReader(p plane, sep string, keys ferry.KeyFunc, static map[string]ferry.Path, vals values) *reader {
	return &reader{p: p, sep: sep, keys: keys, static: static, vals: vals, hid: map[string]ferry.Path{}}
}

// The optional interfaces this reader carries. Enumeration is one of them
// because listing a request's parameters is trivial, and it is what makes a
// map-typed or slice-typed field loadable from this plane at all (ADR-0004).
// [ferry.Releaser] is the second, because this plane's one refusal that cannot
// be made during the walk is made at Close (#193, #208).
var (
	_ ferry.Reader     = (*reader)(nil)
	_ ferry.Enumerator = (*reader)(nil)
	_ ferry.Releaser   = (*reader)(nil)
)

// Get answers with what the request holds at an address.
//
// The answer is a String or an Absent and never a Null, because ?x= is a
// zero-length string and not a null (ADR-0004): neither plane carries type
// information of its own, so every value either holds is text, and the one
// distinction they do carry - present against absent - is the one a required
// field tests.
//
// At a name occurring more than once the answer is Absent, because that name is
// a sequence and its values live at the positions under it rather than at the
// name itself. Whether that was the right answer is not knowable here: one
// occurrence and two are the same name asked for once. So the name is recorded,
// enumerating it clears the record, and a record still standing when the load
// closes is a sequence nothing read as one (ADR-0011's aggregation carries it).
func (r *reader) Get(_ context.Context, addr ferry.Path) (ferry.Value, error) {
	key, err := r.keys(addr)
	if err != nil {
		return ferry.Value{}, err
	}

	if vs := r.vals[key]; len(vs) > 0 {
		return r.atName(addr, key, vs), nil
	}

	if v, ok := r.atPosition(addr); ok {
		return v, nil
	}

	return ferry.Value{}, nil
}

// atName is the answer at a name the request holds values at.
func (r *reader) atName(addr ferry.Path, key string, vs []string) ferry.Value {
	if len(vs) == 1 {
		return ferry.String(vs[0])
	}

	r.hid[key] = addr

	return ferry.Value{}
}

// atPosition answers an element address out of the second dimension: ?tags=a&tags=b
// puts two values under one name, and the position is which of the two.
//
// A key function that refuses the parent is not an error here. The address was
// asked for, so core already has a name for it, and this is the fallback for an
// address whose own name holds nothing.
func (r *reader) atPosition(addr ferry.Path) (ferry.Value, bool) {
	parent, i, ok := splitIndex(addr)
	if !ok {
		return ferry.Value{}, false
	}

	key, err := r.keys(parent)
	if err != nil {
		return ferry.Value{}, false
	}

	vs := r.vals[key]
	if i >= uint(len(vs)) {
		return ferry.Value{}, false
	}

	return ferry.String(vs[i]), true
}

// Close reports every name that was a sequence and was read as a single value,
// one failure per name, each located at the field it is about.
//
// It is the earliest moment the report can be made, and that is a property of
// the plane rather than a choice. At the moment such a name is read there is one
// call at one address and nothing to distinguish a name occurring twice from a
// name occurring once, so only the walk finishing without having enumerated it
// settles the question (#193, #208).
//
// [ferry.ErrorAt] is the outermost wrapper on each element, which is what lets
// core keep the address; the driver's own sentence is inside it. A request with
// two such names reports both (#211).
func (r *reader) Close() error {
	if len(r.hid) == 0 {
		return nil
	}

	errs := make([]error, 0, len(r.hid))

	// Sorted by name so that a report over more than one name is not a report
	// over Go's randomised map iteration order.
	for _, key := range slices.Sorted(maps.Keys(r.hid)) {
		errs = append(errs, ferry.ErrorAt(r.hid[key], repeated(len(r.vals[key]))))
	}

	return errors.Join(errs...)
}

// splitIndex is an address ending in a position, split into what it is under and
// the position itself.
func splitIndex(addr ferry.Path) (ferry.Path, uint, bool) {
	segs := slices.Collect(addr.Segments())
	if len(segs) == 0 {
		return ferry.Path{}, 0, false
	}

	last := segs[len(segs)-1]
	if last.Kind() != ferry.Index {
		return ferry.Path{}, 0, false
	}

	i, ok := position(last.Text())
	if !ok {
		return ferry.Path{}, 0, false
	}

	var parent ferry.Path
	for _, seg := range segs[:len(segs)-1] {
		parent = extend(parent, seg)
	}

	return parent, i, true
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
