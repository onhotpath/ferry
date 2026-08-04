package multimap

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/onhotpath/ferry"
)

// values is what both planes here are. url.Values and http.Header are both
// exactly map[string][]string, which is the whole of #193.
type values = map[string][]string

// plane is one driver's configuration: a name for diagnostics, the shape, the
// key function, the separator its flat join uses, how a minted segment's text is
// spelled back, and which names the caller declared carry a sequence.
type plane struct {
	name     string
	shape    Shape
	keyf     ferry.KeyFunc
	sep      string
	mintf    func(string) string
	declared map[string]bool

	// trace, where the caller supplied one, collects every boundary call this
	// plane's reader and writer make, in order. It is what makes the walk's
	// call sequence evidence rather than a claim.
	trace *[]string
}

// bind is the whole of Bind for both directions: NewKeys, and then the reverse
// table driver/env calls staticNames.
func (p plane) bind(addrs *ferry.AddressSet) (*ferry.Keys, map[string]ferry.Path, error) {
	keys, err := ferry.NewKeys(addrs, p.name, p.keyf)
	if err != nil {
		return nil, nil, err
	}

	static := map[string]ferry.Path{}
	name := keys.Open()

	if addrs != nil {
		for addr := range addrs.All() {
			key, kerr := name(addr)
			if kerr != nil {
				return nil, nil, kerr
			}

			static[key] = addr
		}
	}

	return keys, static, nil
}

// reader is one open over one snapshot of a plane.
type reader struct {
	p      plane
	keys   ferry.KeyFunc
	static map[string]ferry.Path
	vals   values

	// hid records every name whose values this reader hid behind an Absent at
	// a Get, and which nothing has enumerated since. It is the audit shape's
	// whole mechanism, and it is maintained by every shape so that the log is
	// comparable across them.
	hid map[string]ferry.Path

	// log is every boundary call this open made, which is what makes the walk's
	// order evidence rather than a claim.
	log []string
}

var (
	_ ferry.Reader     = (*reader)(nil)
	_ ferry.Enumerator = (*reader)(nil)
)

// auditReader is [CardinalityAudit]'s reader: the same reader with a Close that
// reports what nothing enumerated.
//
// It is a separate type because [ferry.Releaser] is discovered by assertion, so
// a shape that does not audit must not have the method at all.
type auditReader struct{ *reader }

var _ ferry.Releaser = (*auditReader)(nil)

// Close is where the silent loss becomes loud without core changing at all.
//
// A name the driver answered Absent at, betting that Children would follow, and
// which nothing ever enumerated, is a value the plane held and the load did not
// take. core joins this into the error [ferry.Load] returns.
func (a *auditReader) Close() error {
	if len(a.hid) == 0 {
		return nil
	}

	names := slices.Sorted(maps.Keys(a.hid))
	at := make([]string, 0, len(names))

	for _, n := range names {
		at = append(at, a.hid[n].String()+" ("+strconv.Itoa(len(a.vals[n]))+" values)")
	}

	return fmt.Errorf("%w: the plane holds more than one value at %s, and nothing read them: "+
		"a repeated name is a sequence, and this address takes a single value",
		ferry.ErrPlane, strings.Join(at, ", "))
}

// Get answers with what the plane holds at an address.
//
// The driver cannot see the schema. It is handed a [ferry.Path] and nothing
// else, and /tags for a []string field and /q for a string field are the same
// call, so every shape's answer here is a bet about which one it is.
func (r *reader) Get(_ context.Context, addr ferry.Path) (ferry.Value, error) {
	key, err := r.keys(addr)
	if err != nil {
		return ferry.Value{}, err
	}

	if vs, ok := r.vals[key]; ok && len(vs) > 0 {
		return r.atName(addr, key, vs)
	}

	if r.p.shape.positionsBehindName() {
		if v, ok := r.atPosition(addr); ok {
			r.note("Get " + addr.String() + " -> " + kindOf(v) + " (position under a repeated name)")

			return v, nil
		}
	}

	r.note("Get " + addr.String() + " -> Absent")

	return ferry.Value{}, nil
}

// atName is the shape's policy at a name the plane holds values at.
func (r *reader) atName(addr ferry.Path, key string, vs []string) (ferry.Value, error) {
	switch r.p.shape.atName(len(vs), r.p.declared[key]) {
	case answerScalar:
		r.note("Get " + addr.String() + " -> String (" + strconv.Itoa(len(vs)) + " values at " + key + ")")

		return ferry.String(vs[0]), nil
	case answerAbsent:
		r.hid[key] = addr
		r.note("Get " + addr.String() + " -> Absent (hiding " + strconv.Itoa(len(vs)) + " values at " + key + ")")

		return ferry.Value{}, nil
	default:
		r.note("Get " + addr.String() + " -> refused (" + strconv.Itoa(len(vs)) + " values at " + key + ")")

		return ferry.Value{}, repeated(len(vs))
	}
}

// atPosition is the fallback that makes a position behind a name readable: an
// address ending in an Index segment, whose parent's name holds that many
// values.
//
// The index still goes through the key function on the way in, so this is not
// #193's option 1: /tags#1 renders to "tags.1" through [ferry.Keys], which is
// where the injectivity check sees it, and only a miss falls through to here.
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
	if int(i) >= len(vs) {
		return ferry.Value{}, false
	}

	return ferry.String(vs[i]), true
}

// Children lists what the plane holds immediately under an address.
//
// Two sources of children, and which of them a shape uses is the whole
// question. The flat cut is what driver/env does and it finds tags.0 and
// tags.1. The second dimension is what only a multimap has, and it turns the
// values under one name into positions under that name's address.
func (r *reader) Children(_ context.Context, prefix ferry.Path) ([]ferry.Path, error) {
	atRoot := prefix == ferry.Path{}

	var prefixKey string

	if !atRoot {
		var err error
		if prefixKey, err = r.keys(prefix); err != nil {
			return nil, err
		}
	}

	kids := map[ferry.Path]struct{}{}
	behind := map[ferry.Path]struct{}{}

	if !atRoot {
		r.positions(prefix, prefixKey, behind)
		maps.Copy(kids, behind)
	}

	for key := range r.vals {
		kid, ok := r.child(prefix, prefixKey, key, atRoot)
		if !ok {
			continue
		}

		// Both spellings at one address is a value silently overwritten, and it
		// is the residue every position-behind-a-name shape has: /tags#0 is
		// enumerated out of the first value of "tags" and then read out of
		// "tags.0", which wins because it is the address's own key. Refusing is
		// cheap, and ADR-0001 is why.
		if _, clash := behind[kid]; clash {
			return nil, fmt.Errorf("%w: this name carries a sequence in the plane's own repetition and it "+
				"carries index-suffixed names as well: the two spellings address the same position and one "+
				"of the two values would be lost", ferry.ErrPlane)
		}

		kids[kid] = struct{}{}
	}

	out := slices.Collect(maps.Keys(kids))
	slices.SortFunc(out, ferry.Path.Compare)

	r.note("Children " + prefix.String() + " -> " + fmtPaths(out))

	return out, nil
}

// positions turns the values under one name into positions under that name's
// address, where the shape says they are one.
//
// Being asked for children at all is the only signal core gives a driver that an
// address is a dynamic container, and it arrives after the container-address
// question has already been answered. Clearing the audit entry here is what
// makes "nothing enumerated it" mean what it says.
func (r *reader) positions(prefix ferry.Path, prefixKey string, kids map[ferry.Path]struct{}) {
	vs, ok := r.vals[prefixKey]
	if !ok || len(vs) == 0 {
		return
	}

	delete(r.hid, prefixKey)

	if !r.p.shape.enumerates(len(vs), r.p.declared[prefixKey]) {
		return
	}

	for i := range vs {
		kids[prefix.Elem(uint(i))] = struct{}{}
	}
}

// child is one plane key resolved to the immediate child of prefix it lies
// under, driver/env's rule: the static table first, so a tagged field's own
// spelling is recovered exactly, and only what the value mints falls back on the
// canonical form.
func (r *reader) child(prefix ferry.Path, prefixKey, key string, atRoot bool) (ferry.Path, bool) {
	if addr, ok := r.static[key]; ok {
		return step(prefix, addr)
	}

	rest := key

	if !atRoot {
		var ok bool
		if rest, ok = strings.CutPrefix(key, prefixKey+r.p.sep); !ok {
			return ferry.Path{}, false
		}
	}

	head, _, _ := strings.Cut(rest, r.p.sep)
	if head == "" {
		return ferry.Path{}, false
	}

	if i, isPos := position(head); isPos {
		return prefix.Elem(i), true
	}

	return prefix.At(r.p.mintf(head)), true
}

func (r *reader) note(s string) {
	r.log = append(r.log, s)

	if r.p.trace != nil {
		*r.p.trace = append(*r.p.trace, s)
	}
}

// writer is one open dump into a plane.
type writer struct {
	p      plane
	keys   ferry.KeyFunc
	vals   values
	log    []string
	repeat bool
}

var _ ferry.Writer = (*writer)(nil)

// Set writes one address.
//
// Where the shape puts positions behind a name, an element address appends
// under its parent's name instead of taking a name of its own. That is a
// deliberate collapse of two ferry addresses onto one plane key, and it is
// outside [ferry.NewKeys]'s injectivity check, which saw "tags.0" and "tags.1"
// and found them distinct. What keeps it lossless is that a multimap's second
// dimension is ordered, so the two values are still two.
func (w *writer) Set(_ context.Context, addr ferry.Path, v ferry.Value) error {
	key, err := w.keys(addr)
	if err != nil {
		return err
	}

	text, err := textOf(v)
	if err != nil {
		return err
	}

	if parent, i, ok := w.appendsAt(addr); ok {
		w.grow(parent, int(i))
		w.vals[parent][i] = text
		w.note("Set " + addr.String() + " -> " + parent + "[" + strconv.Itoa(int(i)) + "]=" + text)

		return nil
	}

	w.vals[key] = []string{text}
	w.note("Set " + addr.String() + " -> " + key + "=" + text)

	return nil
}

// appendsAt reports the name and position an element address writes into, and
// false where this shape gives the element a name of its own.
func (w *writer) appendsAt(addr ferry.Path) (string, uint, bool) {
	if !w.repeat {
		return "", 0, false
	}

	parent, i, ok := splitIndex(addr)
	if !ok {
		return "", 0, false
	}

	key, err := w.keys(parent)
	if err != nil {
		return "", 0, false
	}

	// Under Declared only a declared name carries a sequence, so an undeclared
	// one falls back to the index-suffixed spelling and still round trips.
	if w.p.shape == Declared && !w.p.declared[key] {
		return "", 0, false
	}

	return key, i, true
}

func (w *writer) note(s string) {
	w.log = append(w.log, s)

	if w.p.trace != nil {
		*w.p.trace = append(*w.p.trace, s)
	}
}

func (w *writer) grow(key string, i int) {
	for len(w.vals[key]) <= i {
		w.vals[key] = append(w.vals[key], "")
	}
}

func textOf(v ferry.Value) (string, error) {
	switch v.Kind() {
	case ferry.KindString:
		return v.AsString()
	case ferry.KindNumber:
		return v.AsNumber()
	case ferry.KindBool:
		b, err := v.AsBool()

		return strconv.FormatBool(b), err
	case ferry.KindBytes:
		b, err := v.AsBytes()

		return string(b), err
	case ferry.KindAbsent, ferry.KindNull:
		return "", illegal("this plane holds no value of that kind")
	default:
		return "", illegal("this plane holds no value of that kind")
	}
}

// splitIndex is an address ending in a position, split into what it is under and
// the position itself.
func splitIndex(addr ferry.Path) (parent ferry.Path, i uint, ok bool) {
	segs := slices.Collect(addr.Segments())
	if len(segs) == 0 {
		return ferry.Path{}, 0, false
	}

	last := segs[len(segs)-1]
	if last.Kind() != ferry.Index {
		return ferry.Path{}, 0, false
	}

	pos, ok := position(last.Text())
	if !ok {
		return ferry.Path{}, 0, false
	}

	for _, s := range segs[:len(segs)-1] {
		parent = extend(parent, s)
	}

	return parent, pos, true
}

// step is the immediate child of prefix that addr lies under. driver/env's.
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

func extend(p ferry.Path, s ferry.Segment) ferry.Path {
	if s.Kind() == ferry.Index {
		i, _ := position(s.Text())

		return p.Elem(i)
	}

	return p.At(s.Text())
}

func kindOf(v ferry.Value) string { return v.Kind().String() }

func fmtPaths(ps []ferry.Path) string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.String())
	}

	return "[" + strings.Join(out, " ") + "]"
}

// FmtValues renders a plane deterministically, which is what lets a table in the
// report be compared across shapes.
func FmtValues(v values) string {
	names := make([]string, 0, len(v))
	for k := range v {
		names = append(names, k)
	}

	sort.Strings(names)

	parts := make([]string, 0, len(names))

	for _, k := range names {
		vs := make([]string, 0, len(v[k]))
		for _, one := range v[k] {
			vs = append(vs, strconv.Quote(one))
		}

		parts = append(parts, strconv.Quote(k)+":["+strings.Join(vs, " ")+"]")
	}

	return "{" + strings.Join(parts, " ") + "}"
}

// errNoPlane is what a load or a dump with no plane in the context fails with.
var errNoPlane = errors.New("multimap: no plane in the context")
