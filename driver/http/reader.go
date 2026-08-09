package ferryhttp

import (
	"context"
	"slices"
	"strings"

	"github.com/onhotpath/ferry"
)

// reader is one open over one request's parameters or fields.
//
// Everything in it is allocated here rather than shared with the binding it came
// from, which is what lets one binding serve every goroutine net/http runs a
// handler in: the binding holds the name table and never changes, and this holds
// what one load observed.
type reader struct {
	p     plane
	sep   string
	bytes ferry.Spelling[[]byte, string]

	// names is the binding's checked name table, held for the reports rather
	// than for the reads: it answers what this plane calls an address without
	// minting anything (ADR-0011, #159).
	names *ferry.Keys

	keys   ferry.KeyFunc
	static map[string]ferry.Path
	vals   values
}

func newReader(p *plane, cfg config, names *ferry.Keys, static map[string]ferry.Path, vals values) *reader {
	return &reader{p: *p, sep: cfg.sep, bytes: cfg.bytes, names: names,
		keys: names.Open(), static: static, vals: vals}
}

// The optional interfaces this reader carries. Enumeration is one of them
// because listing a request's parameters is trivial, and it is what makes a
// map-typed or slice-typed field loadable from this plane at all (ADR-0004).
// [ferry.Prober] is the second, and on this plane it exists for its refusal
// rather than for its answers: a request has no spelling for a container at the
// container's own name, so what it can say there is that the name holds a value
// the destination has nowhere to put (ADR-0016).
//
// [ferry.Releaser] is not among them any more, and its absence is the shape of
// what the sealed address model bought. This reader implemented Close for one
// reason: to report, too late, a name it could not refuse at the moment it was
// read (#193, #208). Every refusal it makes now is made during the walk at the
// address it is about, so there is nothing left to say at Close and a Close
// that returns nil is indistinguishable from a release somebody forgot.
var (
	_ ferry.Reader     = (*reader)(nil)
	_ ferry.Prober     = (*reader)(nil)
	_ ferry.Enumerator = (*reader)(nil)
	_ ferry.PlaneNamer = (*reader)(nil)
)

// PlaneName is the parameter or field name an address arrived in, which is what
// a report opens with in place of the address: /db/host prints as db.host on the
// query plane and as Db-Host on the header plane.
//
// It reads the table Bind built and never this open's key function, so it mints
// nothing and cannot refuse. An address this plane has no name for is a false,
// and ferry's own rendering stands (ADR-0011, #159).
func (r *reader) PlaneName(addr ferry.Path) (string, bool) { return r.names.PlaneName(addr) }

// Get answers with what the request holds at an address.
//
// The answer is a String or an Absent and never a Null, because ?x= is a
// zero-length string and not a null (ADR-0004): neither plane carries type
// information of its own, so every value either holds is text, and the one
// distinction they do carry - present against absent - is the one a required
// field tests. A source told how this plane spells a payload answers with Bytes
// instead, and [reader.observe] is where that fork is (ADR-0018).
//
// A name the request holds more than one value at is refused here, and
// [reader.atName] is where the reason is written down.
func (r *reader) Get(_ context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	at := addr.Path()

	key, err := r.keys(at)
	if err != nil {
		return ferry.Value{}, err
	}

	if vs := r.vals[key]; len(vs) > 0 {
		return r.atName(vs)
	}

	text, ok := r.atPosition(at)
	if !ok {
		return ferry.Value{}, nil
	}

	return r.observe(text)
}

// observe is what one value of this plane is at the boundary.
//
// It is a String unless this plane was told how it spells a payload, and then
// it is that spelling's own reading of the text. The fork is plane-wide because
// a request carries no type information for this driver to consult: what a
// value is here is what the plane was declared to hold, and a value the
// spelling refuses is a refusal rather than a fallback to text, since a
// payload spelling accepts far too much ordinary text for a fallback to mean
// anything (ADR-0018).
func (r *reader) observe(text string) (ferry.Value, error) {
	if r.bytes == nil {
		return ferry.String(text), nil
	}

	payload, err := r.bytes.Parse(text)
	if err != nil {
		return ferry.Value{}, err
	}

	return ferry.Bytes(payload), nil
}

// Probe answers whether the request holds a container at one address.
//
// A request has no null, so the answer is present or absent and never null: a
// container is here when the request holds something that belongs to it, and
// absent when it holds nothing.
//
// What belongs to it depends on which kind of container it is, and the two are
// different questions rather than one question asked twice. A container whose
// members come from the value owns both the names under its own name and the
// repetitions of that name, because a name occurring more than once is the
// sequence it carries. A container whose members come from the type owns only
// the names under it, so a value at its own name is the request and the
// destination disagreeing, and it is refused rather than dropped.
func (r *reader) Probe(_ context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	at := addr.Path()

	key, err := r.keys(at)
	if err != nil {
		return ferry.SectionAbsent, err
	}

	own := len(r.vals[key])

	if _, dynamic := addr.(ferry.CompositeAddr); !dynamic && own > 0 {
		return ferry.SectionAbsent, ferry.ErrorAt(at, atContainer(own))
	}

	if own > 0 || r.anyUnder(key) {
		return ferry.SectionPresent, nil
	}

	return ferry.SectionAbsent, nil
}

// anyUnder reports whether any name lies strictly under this one, which is the
// same cut [reader.Children] makes and is what makes a probe and an enumeration
// agree about whether a container is there.
func (r *reader) anyUnder(key string) bool {
	for name := range r.vals {
		if rest, ok := strings.CutPrefix(name, key+r.sep); ok && rest != "" {
			return true
		}
	}

	return false
}

// atName is the answer at a name the request holds values at: one value is the
// value, and more than one is refused.
//
// The address is a leaf, because core asks Get about a [ferry.LeafAddr] and
// about nothing else, so it is an address that holds one value by construction.
// A name occurring twice at it is a sequence arriving where the destination
// takes a scalar, and taking the first would discard the rest in silence
// (ADR-0016).
//
// That refusal used to be deferred to Close, and the deferral is what the
// sealed address model removed rather than improved. An address arrived at Get
// carrying no kind and no arity, so /tags for a []string and /q for a string
// were the same call and one occurrence was indistinguishable from two; the
// driver had to answer Absent, record the name, and wait to see whether
// anything enumerated it (ADR-0015, #193, #208). The kind arrives with the
// address now, so the refusal is made where it is about, during the walk, and
// core attaches the address to it.
func (r *reader) atName(vs []string) (ferry.Value, error) {
	if len(vs) == 1 {
		return r.observe(vs[0])
	}

	return ferry.Value{}, repeated(len(vs))
}

// atPosition answers an element address out of the second dimension: ?tags=a&tags=b
// puts two values under one name, and the position is which of the two.
//
// A key function that refuses the parent is not an error here. The address was
// asked for, so core already has a name for it, and this is the fallback for an
// address whose own name holds nothing.
func (r *reader) atPosition(addr ferry.Path) (string, bool) {
	parent, i, ok := splitIndex(addr)
	if !ok {
		return "", false
	}

	key, err := r.keys(parent)
	if err != nil {
		return "", false
	}

	vs := r.vals[key]
	if i >= uint(len(vs)) {
		return "", false
	}

	return vs[i], true
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
