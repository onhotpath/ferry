package httpdecisions

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
// exactly map[string][]string.
type values = map[string][]string

// plane is one driver's configuration: a name for diagnostics, the shape, the
// key function, the separator its flat join uses, how a minted segment's text is
// spelled back, and the four axes #210 varies.
type plane struct {
	name  string
	shape Shape
	keyf  ferry.KeyFunc
	sep   string
	mintf func(string) string

	// clash is question 2's axis.
	clash Clash
	// refusal is question 4's axis.
	refusal Refusal
	// spelling and setSem are question 1's.
	spelling SinkSpelling
	setSem   SetSemantics
	// declared is question 3's: the per-schema configuration a Source carries.
	declared map[string]bool
	// declaredNames is the same, in the order the caller gave it, so a refusal
	// at Bind can name what it did not find.
	declaredNames []string
	// checkDeclared turns on the Bind-time check of the declaration against the
	// address set, which is question 3's "is it checkable" half.
	checkDeclared bool

	// trace, where the caller supplied one, collects every boundary call this
	// plane's reader and writer make, in order.
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

	if err := p.checkDeclaration(static); err != nil {
		return nil, nil, err
	}

	return keys, static, nil
}

// checkDeclaration is how much of a per-schema declaration a driver can check
// against the schema at Bind, which is question 3's whole measurement.
//
// A declared name that no address in this schema renders to is checkable and is
// refused here. A declared name that is a leaf rather than a container is not,
// because an AddressSet gives one Name segment with no kind and no arity for
// both Tags []string and Q string.
func (p plane) checkDeclaration(static map[string]ferry.Path) error {
	if !p.checkDeclared {
		return nil
	}

	for _, n := range p.declaredNames {
		if _, ok := static[n]; !ok {
			return fmt.Errorf("%w: this schema has no address that renders to the declared name %q",
				ferry.ErrPlane, n)
		}
	}

	return nil
}

// reader is one open over one snapshot of a plane.
type reader struct {
	p      plane
	keys   ferry.KeyFunc
	static map[string]ferry.Path
	vals   values

	// hid records every name whose values this reader hid behind an Absent at a
	// Get, and which nothing has enumerated since.
	hid map[string]ferry.Path

	// lost records a name whose index-suffixed spelling this reader dropped in
	// favour of the repeated one, for the audited clash policy.
	lost map[string]ferry.Path

	log []string
}

var (
	_ ferry.Reader     = (*reader)(nil)
	_ ferry.Enumerator = (*reader)(nil)
)

// auditReader is the reader with a Close that reports what nothing enumerated.
//
// It is a separate type because ferry.Releaser is discovered by assertion, so a
// configuration that does not audit must not have the method at all.
type auditReader struct{ *reader }

var _ ferry.Releaser = (*auditReader)(nil)

// Close is where the silent loss becomes loud without core changing at all, and
// it is question 4's whole surface.
//
// The address is either spelled into the text or attached with ferry.ErrorAt.
// The second works because core calls Close's error through fromDriver with the
// zero Path, and fromDriver takes an address the driver named where core has
// none of its own.
func (a *auditReader) Close() error {
	errs := a.hidden()
	errs = append(errs, a.dropped()...)

	if len(errs) == 0 {
		return nil
	}

	return errors.Join(errs...)
}

// hidden is one error per name whose values were hidden behind an Absent and
// never enumerated.
func (a *auditReader) hidden() []error {
	if a.p.refusal == RefuseNever || len(a.hid) == 0 {
		return nil
	}

	names := slices.Sorted(maps.Keys(a.hid))

	if a.p.refusal == RefuseAtCloseHybrid {
		return []error{a.oneForAll(names)}
	}

	out := make([]error, 0, len(names))

	for _, n := range names {
		at := a.hid[n]
		msg := a.repeatedAt(n)

		if a.p.refusal == RefuseAtCloseWithErrorAt {
			out = append(out, ferry.ErrorAt(at, msg))

			continue
		}

		out = append(out, fmt.Errorf("%w at %s", msg, at))
	}

	return out
}

func (a *auditReader) repeatedAt(name string) error {
	return fmt.Errorf("%w: %w: it holds %d, and this address takes one: a repeated name is a sequence "+
		"and nothing read it as one", ferry.ErrPlane, ErrRepeated, len(a.vals[name]))
}

// oneForAll is the hybrid: one error carrying the first address through
// ferry.ErrorAt, with every other offending address named in the text, because
// core keeps one address off a joined Close error and discards the rest.
func (a *auditReader) oneForAll(names []string) error {
	msg := a.repeatedAt(names[0])

	if len(names) > 1 {
		rest := make([]string, 0, len(names)-1)
		for _, n := range names[1:] {
			rest = append(rest, a.hid[n].String())
		}

		msg = fmt.Errorf("%w (and the same at %s)", msg, strings.Join(rest, ", "))
	}

	return ferry.ErrorAt(a.hid[names[0]], msg)
}

// dropped is the audited clash policy's report.
func (a *auditReader) dropped() []error {
	if a.p.clash != ClashRepeatedWinsAudited {
		return nil
	}

	out := make([]error, 0, len(a.lost))

	for _, n := range slices.Sorted(maps.Keys(a.lost)) {
		out = append(out, ferry.ErrorAt(a.lost[n], fmt.Errorf("%w: %w: the repeated spelling was read and the "+
			"index-suffixed one was not", ferry.ErrPlane, ErrTwoSpellings)))
	}

	return out
}

// Get answers with what the plane holds at an address.
func (r *reader) Get(_ context.Context, addr ferry.Path) (ferry.Value, error) {
	key, err := r.keys(addr)
	if err != nil {
		return ferry.Value{}, err
	}

	if v, ok := r.atPositionFirst(addr); ok {
		return v, nil
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

// atPositionFirst is the repeated-spelling-wins policy: consult the second
// dimension before the address's own key, so a name carrying both spellings
// answers out of the repetition.
func (r *reader) atPositionFirst(addr ferry.Path) (ferry.Value, bool) {
	if !r.p.shape.positionsBehindName() {
		return ferry.Value{}, false
	}

	if r.p.clash != ClashRepeatedWins && r.p.clash != ClashRepeatedWinsAudited {
		return ferry.Value{}, false
	}

	v, ok := r.atPosition(addr)
	if !ok {
		return ferry.Value{}, false
	}

	if key, err := r.keys(addr); err == nil {
		if _, shadowed := r.vals[key]; shadowed {
			r.lost[key] = addr
		}
	}

	r.note("Get " + addr.String() + " -> " + kindOf(v) + " (position, second dimension first)")

	return v, true
}

// atName is the shape's policy at a name the plane holds values at.
func (r *reader) atName(addr ferry.Path, key string, vs []string) (ferry.Value, error) {
	switch r.p.shape.atName(len(vs), r.p.declared[key]) {
	case answerScalar:
		r.note("Get " + addr.String() + " -> String (" + strconv.Itoa(len(vs)) + " values at " + key + ")")

		return ferry.String(vs[0]), nil
	case answerAbsent:
		return r.hide(addr, key, vs)
	default:
		r.note("Get " + addr.String() + " -> refused (" + strconv.Itoa(len(vs)) + " values at " + key + ")")

		return ferry.Value{}, repeated(len(vs))
	}
}

// hide answers Absent at a name holding more than one value, betting that
// Children will follow, and is where question 4's earliest refusal would go if
// conformance case 3 allowed one.
func (r *reader) hide(addr ferry.Path, key string, vs []string) (ferry.Value, error) {
	if len(vs) > 1 && r.p.refusal == RefuseAtGet {
		r.note("Get " + addr.String() + " -> refused at Get (" + strconv.Itoa(len(vs)) + " values at " + key + ")")

		return ferry.Value{}, ferry.ErrorAt(addr, repeated(len(vs)))
	}

	if len(vs) > 1 {
		r.hid[key] = addr
	}

	r.note("Get " + addr.String() + " -> Absent (hiding " + strconv.Itoa(len(vs)) + " values at " + key + ")")

	return ferry.Value{}, nil
}

// atPosition is the fallback that makes a position behind a name readable.
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

	if err := r.fromNames(prefix, prefixKey, atRoot, behind, kids); err != nil {
		return nil, err
	}

	out := slices.Collect(maps.Keys(kids))
	slices.SortFunc(out, ferry.Path.Compare)

	r.note("Children " + prefix.String() + " -> " + fmtPaths(out))

	return out, nil
}

// fromNames is the flat cut driver/env does: every plane key that lies
// immediately under the prefix, minted as a child address.
func (r *reader) fromNames(prefix ferry.Path, prefixKey string, atRoot bool,
	behind, kids map[ferry.Path]struct{},
) error {
	for key := range r.vals {
		kid, ok := r.child(prefix, prefixKey, key, atRoot)
		if !ok {
			continue
		}

		if _, both := behind[kid]; both && r.p.clash == ClashRefuse {
			return ferry.ErrorAt(kid, fmt.Errorf("%w: %w: the two spellings address the same position "+
				"and one of the two values would be lost", ferry.ErrPlane, ErrTwoSpellings))
		}

		kids[kid] = struct{}{}
	}

	return nil
}

// positions turns the values under one name into positions under that name's
// address.
func (r *reader) positions(prefix ferry.Path, prefixKey string, kids map[ferry.Path]struct{}) {
	vs, ok := r.vals[prefixKey]
	if !ok || len(vs) == 0 {
		return
	}

	delete(r.hid, prefixKey)

	if !r.p.shape.enumerates(len(vs)) {
		return
	}

	for i := range vs {
		kids[prefix.Elem(uint(i))] = struct{}{}
	}
}

// child is one plane key resolved to the immediate child of prefix it lies
// under, driver/env's rule.
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
	p    plane
	keys ferry.KeyFunc
	vals values
	log  []string
	// cleared records the keys this dump has already replaced, so SetReplace
	// clears a key once per dump and not once per element.
	cleared map[string]bool
}

var _ ferry.Writer = (*writer)(nil)

// Set writes one address.
//
// Under SinkRepeated an element address appends under its parent's name instead
// of taking a name of its own. That is a deliberate collapse of two ferry
// addresses onto one plane key. #193 recorded it as being outside
// ferry.NewKeys's injectivity check, because the check saw "tags.0" and
// "tags.1" and found them distinct. What makes it checked after all is that the
// key it collapses onto is the parent's own, and the parent's own key is
// rendered through the same checked KeyFunc here.
func (w *writer) Set(_ context.Context, addr ferry.Path, v ferry.Value) error {
	key, err := w.keys(addr)
	if err != nil {
		return err
	}

	text, err := textOf(v)
	if err != nil {
		return err
	}

	parent, i, ok, err := w.appendsAt(addr)
	if err != nil {
		return err
	}

	if ok {
		w.replaceOnce(parent)
		w.grow(parent, int(i))
		w.vals[parent][i] = text
		w.note("Set " + addr.String() + " -> " + parent + "[" + strconv.Itoa(int(i)) + "]=" + text)

		return nil
	}

	w.replaceOnce(key)

	if w.p.setSem == SetAsIn193 {
		w.vals[key] = []string{text}
	} else {
		w.vals[key] = append(w.vals[key], text)
	}

	w.note("Set " + addr.String() + " -> " + key + "=" + text)

	return nil
}

// replaceOnce is the replace half of SetSemantics: the first write of this dump
// at a key drops whatever the plane already held there.
func (w *writer) replaceOnce(key string) {
	if w.p.setSem != SetReplace || w.cleared[key] {
		return
	}

	w.cleared[key] = true

	delete(w.vals, key)
}

// appendsAt reports the name and position an element address writes into, and
// false where this spelling gives the element a name of its own.
//
// The parent's key goes through the same checked KeyFunc every other address
// does, and its refusal is returned rather than swallowed: swallowing it is what
// silently downgrades the spelling for one element and keeps it for the rest.
func (w *writer) appendsAt(addr ferry.Path) (name string, pos uint, ok bool, err error) {
	if w.p.spelling != SinkRepeated {
		return "", 0, false, nil
	}

	parent, i, isElem := splitIndex(addr)
	if !isElem {
		return "", 0, false, nil
	}

	key, kerr := w.keys(parent)
	if kerr != nil {
		return "", 0, false, kerr
	}

	return key, i, true, nil
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

// FmtValues renders a plane deterministically.
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
var errNoPlane = errors.New("http: no plane in the context")
