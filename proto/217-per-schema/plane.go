package perschema

import (
	"context"
	"fmt"
	"net/textproto"
	"slices"
	"strconv"
	"strings"

	"github.com/onhotpath/ferry"
)

// sep is what the flat join spells a nested field name with. An HTTP field name
// is a token, and - is the byte every multi-word field name in the registry
// already uses.
const sep = "-"

// values is what this plane is. http.Header is exactly map[string][]string.
type values = map[string][]string

func canon(s string) string { return textproto.CanonicalMIMEHeaderKey(s) }

// rawName renders an address to the field name it would have with no alias
// applied. It is what a declaration is keyed by, so a declaration names the
// schema's own spelling and not the plane key an alias sends it to.
func rawName(addr ferry.Path) (string, error) {
	var b strings.Builder

	first := true

	for seg := range addr.Segments() {
		if seg.Text() == "" {
			return "", illegal("a segment is empty, and no join gives an empty segment a name")
		}

		if !first {
			b.WriteString(sep)
		}

		first = false

		b.WriteString(seg.Text())
	}

	if first {
		return "", illegal("the empty address names nothing")
	}

	return canon(b.String()), nil
}

// keyFunc is the plane key an address is actually read at: rawName, with an
// alias applied.
//
// The alias goes through here rather than through the reader, so ferry.NewKeys
// sees the renamed key space and runs its injectivity check over it.
func (c config) keyFunc() ferry.KeyFunc {
	return func(addr ferry.Path) (string, error) {
		n, err := rawName(addr)
		if err != nil {
			return "", err
		}

		if to, ok := c.alias[n]; ok {
			return to, nil
		}

		return n, nil
	}
}

// Source is the read half of a header plane taken from the context.
type Source struct {
	cfg   config
	binds int

	// pinned is the rendered address set this Source was first bound to, kept
	// only for the PinSchema option.
	pinned []string
}

var _ ferry.Source = (*Source)(nil)

// NewSource builds a header Source carrying whatever configuration it is given.
func NewSource(opts ...Option) *Source { return &Source{cfg: newConfig(opts)} }

// Binds is how often core has asked this Source for its address set.
func (s *Source) Binds() int { return s.binds }

// Bind computes this schema's plane keys and checks what it can, before any
// request is looked at.
func (s *Source) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	s.binds++

	if err := s.pin(addrs); err != nil {
		return nil, err
	}

	keys, err := ferry.NewKeys(addrs, "header", s.cfg.keyFunc())
	if err != nil {
		return nil, err
	}

	names := map[string]ferry.Path{}

	if addrs != nil {
		for addr := range addrs.All() {
			n, nerr := rawName(addr)
			if nerr != nil {
				return nil, nerr
			}

			names[n] = addr
		}
	}

	if err := s.cfg.checkAgainst(names); err != nil {
		return nil, err
	}

	cfg := s.cfg

	return func(ctx context.Context) (ferry.Reader, error) {
		vals, verr := planeFrom(ctx)
		if verr != nil {
			return nil, verr
		}

		return newReader(cfg, keys, vals), nil
	}, nil
}

// pin is the PinSchema option: refuse an address set that is not the one this
// Source was first bound to.
func (s *Source) pin(addrs *ferry.AddressSet) error {
	if !s.cfg.pin {
		return nil
	}

	got := rendered(addrs)

	if s.pinned == nil {
		s.pinned = got

		return nil
	}

	if !slices.Equal(s.pinned, got) {
		return fmt.Errorf("%w: %w: this Source is pinned to %v and was bound to %v",
			ferry.ErrPlane, ErrDeclaration, s.pinned, got)
	}

	return nil
}

func rendered(addrs *ferry.AddressSet) []string {
	if addrs == nil {
		return nil
	}

	out := make([]string, 0, addrs.Len())
	for p := range addrs.All() {
		out = append(out, p.String())
	}

	return out
}

// reader is one open over one snapshot of a plane.
type reader struct {
	cfg  config
	keys ferry.KeyFunc
	vals values

	// hid records every name whose values this reader hid behind an Absent
	// because a declaration said the address was a sequence, and which nothing
	// has enumerated since. It is the whole of what the audited configuration
	// reports at Close.
	hid map[string]ferry.Path
}

var (
	_ ferry.Reader     = (*reader)(nil)
	_ ferry.Enumerator = (*reader)(nil)
)

// auditReader is the reader with a Close. It is a separate type because
// ferry.Releaser is discovered by assertion, so a configuration that does not
// audit must not have the method at all.
type auditReader struct{ *reader }

var _ ferry.Releaser = (*auditReader)(nil)

func newReader(cfg config, keys *ferry.Keys, vals values) ferry.Reader {
	r := &reader{cfg: cfg, keys: keys.Open(), vals: vals, hid: map[string]ferry.Path{}}

	if cfg.audited {
		return &auditReader{reader: r}
	}

	return r
}

func (r *reader) note(s string) {
	if r.cfg.trace != nil {
		*r.cfg.trace = append(*r.cfg.trace, s)
	}
}

// Get answers with what the plane holds at an address.
func (r *reader) Get(_ context.Context, addr ferry.Path) (ferry.Value, error) {
	key, err := r.keys(addr)
	if err != nil {
		return ferry.Value{}, err
	}

	n, err := rawName(addr)
	if err != nil {
		return ferry.Value{}, err
	}

	if v, ok, derr := r.declaredAt(addr, n, r.vals[key]); ok || derr != nil {
		return v, derr
	}

	return r.undeclaredAt(addr, n, r.vals[key])
}

// declaredAt is the part of Get a declaration decides, and it reports whether it
// decided anything.
func (r *reader) declaredAt(addr ferry.Path, n string, vs []string) (ferry.Value, bool, error) {
	if !r.cfg.repeatable[n] {
		return ferry.Value{}, false, nil
	}

	// A declared sequence answers Absent at its own address whatever its
	// cardinality, and bets that Children follows. Where the address is not a
	// sequence nothing ever does, which is the hazard, and hid is what makes it
	// observable.
	if len(vs) > 0 {
		r.hid[n] = addr
	}

	r.note("Get " + addr.String() + " -> Absent (declared a sequence, " + strconv.Itoa(len(vs)) + " values at " + n + ")")

	return ferry.Value{}, true, nil
}

// undeclaredAt is the plane's own reading, which is the reading a Source with no
// per-schema configuration gives.
func (r *reader) undeclaredAt(addr ferry.Path, n string, vs []string) (ferry.Value, error) {
	switch {
	case len(vs) == 1:
		r.note("Get " + addr.String() + " -> String")

		return ferry.String(vs[0]), nil
	case len(vs) > 1:
		r.note("Get " + addr.String() + " -> Absent (" + strconv.Itoa(len(vs)) + " values, undeclared)")

		return ferry.Value{}, nil
	}

	if v, ok := r.atPosition(addr); ok {
		r.note("Get " + addr.String() + " -> String (position under a repeated name)")

		return v, nil
	}

	return r.absentAt(addr, n)
}

// absentAt is what the plane holding nothing at a name means, which is where the
// two remaining declarations land.
func (r *reader) absentAt(addr ferry.Path, n string) (ferry.Value, error) {
	if text, ok := r.cfg.fallback[n]; ok {
		r.note("Get " + addr.String() + " -> String (declared fallback)")

		return ferry.String(text), nil
	}

	if r.cfg.required[n] {
		r.note("Get " + addr.String() + " -> refused (declared required)")

		return ferry.Value{}, ferry.ErrorAt(addr, required(n))
	}

	r.note("Get " + addr.String() + " -> Absent")

	return ferry.Value{}, nil
}

// atPosition makes a position behind a repeated name readable.
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

// Children mints one position per value the plane holds at the prefix's own
// name, plus one Name segment per plane key strictly under it.
func (r *reader) Children(_ context.Context, prefix ferry.Path) ([]ferry.Path, error) {
	if (prefix == ferry.Path{}) {
		return nil, nil
	}

	key, err := r.keys(prefix)
	if err != nil {
		return nil, err
	}

	n, err := rawName(prefix)
	if err != nil {
		return nil, err
	}

	out := make([]ferry.Path, 0, len(r.vals[key]))

	for i := range r.vals[key] {
		out = append(out, prefix.Elem(uint(i)))
	}

	if len(out) > 0 {
		// Something read this name as the sequence it was declared to be, so
		// nothing was hidden.
		delete(r.hid, n)
	}

	out = append(out, r.namedUnder(prefix, key)...)

	r.note("Children " + prefix.String() + " -> " + strconv.Itoa(len(out)))

	return out, nil
}

// namedUnder is one Name segment per plane key that sits strictly below this
// prefix's own key, which is what makes a map loadable.
func (r *reader) namedUnder(prefix ferry.Path, key string) []ferry.Path {
	var out []ferry.Path

	for k := range r.vals {
		rest, ok := strings.CutPrefix(k, key+sep)
		if !ok || rest == "" || strings.Contains(rest, sep) {
			continue
		}

		out = append(out, prefix.At(rest))
	}

	return out
}

// Close is where a declaration that was wrong for this schema becomes loud
// without core changing at all, and it is the whole of the audited option.
func (a *auditReader) Close() error {
	if len(a.hid) == 0 {
		return nil
	}

	errs := make([]error, 0, len(a.hid))

	for _, n := range sortedKeys(a.hid) {
		errs = append(errs, ferry.ErrorAt(a.hid[n], repeatedAt(n, len(a.vals[a.planeKey(n)]))))
	}

	return joinAll(errs)
}

// planeKey is the key a declared name's values are actually held at, so the
// report counts what was hidden rather than what the schema spelled.
func (a *auditReader) planeKey(n string) string {
	if to, ok := a.cfg.alias[n]; ok {
		return to
	}

	return n
}
