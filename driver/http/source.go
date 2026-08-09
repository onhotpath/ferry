package ferryhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/onhotpath/ferry"
)

// ErrNoQuery reports a load through [NewQuerySource] whose context carries no
// query parameters, which means [WithQuery] was not called or was called with a
// nil url.Values. A nil one carries nothing a request could have supplied, so it
// is the same absence and is refused as one; an allocated url.Values holding no
// parameters is a request that carries none, and loads.
//
// It is the handler's own defect and not the request's: a load that answered
// from nothing instead would report every field missing, and a required field
// would fail for a request that supplied it. So it is refused before anything is
// read.
//
// It wraps [ferry.ErrPlane] and stays reachable under ferry's wrapper, so
// errors.Is answers for it on what [ferry.Load] returned.
var ErrNoQuery = errors.New("http: no query parameters in the context")

// ErrNoHeaders reports a load through [NewHeaderSource] whose context carries no
// header fields, which means [WithHeaders] was not called or was called with a
// nil http.Header. It is [ErrNoQuery]'s counterpart and is refused at the same
// moment for the same reason.
var ErrNoHeaders = errors.New("http: no header fields in the context")

// queryCtxKey is this package's own key for the query plane: unexported, of its
// own type, so nothing outside can construct one and no other package's key can
// collide with it (ADR-0012).
type queryCtxKey struct{}

// headerCtxKey is the same for the header plane. Two keys rather than one,
// because two planes in one load is exactly what a handler reading a filter out
// of the query and a tenant out of a header does.
type headerCtxKey struct{}

// WithQuery returns a context carrying one request's query parameters, which is
// how a load through [NewQuerySource] reaches them.
//
//	f, err := ferry.Load[Filter](ferryhttp.WithQuery(r.Context(), r.URL.Query()), src)
//
// The values are read and never written to, so passing r.URL.Query() directly is
// safe even though it is a fresh map on every call.
//
// A load whose context did not come through this call is refused with
// [ErrNoQuery] rather than answered from nothing, and so is one that came
// through it carrying a nil url.Values.
func WithQuery(ctx context.Context, v url.Values) context.Context {
	return context.WithValue(ctx, queryCtxKey{}, v)
}

// WithHeaders returns a context carrying one request's header fields, which is
// how a load through [NewHeaderSource] reaches them.
//
//	t, err := ferry.Load[Tenant](ferryhttp.WithHeaders(r.Context(), r.Header), src)
//
// The fields are read and never written to. A load whose context did not come
// through this call, or came through it carrying a nil http.Header, is refused
// with [ErrNoHeaders].
func WithHeaders(ctx context.Context, h http.Header) context.Context {
	return context.WithValue(ctx, headerCtxKey{}, h)
}

// values is what both planes are. url.Values and http.Header are both exactly
// map[string][]string, and the second dimension is where a sequence lives.
type values = map[string][]string

// plane is the half of a [Source] that differs between query parameters and
// header fields: what to call it, how to render an address as a name, how to
// spell a name back as a map key, what it calls the root address, and how to
// find the request in a context.
type plane struct {
	name     string
	sep      string
	key      func(sep string, root rootName) ferry.KeyFunc
	mint     func(text string) string
	checkSep func(sep string) error
	root     func(c config) (rootName, error)
	from     func(ctx context.Context) (values, error)
}

// queryPlane describes the query-parameter plane.
//
// mint is the identity: a query parameter name is any byte sequence, so a name
// this driver reads back is the name that arrived, and a map key round trips
// through it unchanged.
func queryPlane() plane {
	return plane{
		name:     "query",
		sep:      QuerySeparator,
		key:      flatKey,
		mint:     func(text string) string { return text },
		checkSep: notEmpty,
		root:     queryRoot,
		from:     queryFrom,
	}
}

// queryRoot is what this plane calls the root address, which [RootName] is the
// only thing that gives it (#338).
//
// The query plane's reading of the raw name is the identity, the same transform
// flatKey applies to every other name on this plane: a query parameter name is
// any byte sequence, so there is nothing to canonicalise and nothing to refuse
// but an empty name.
func queryRoot(c config) (rootName, error) {
	return rootName{name: c.root, option: "ferryhttp.RootName"}, nil
}

// headerPlane describes the header-field plane.
//
// mint lower-cases, because a field name is canonicalised on the way in and
// there is no way back to the case it started as. A map key that is already
// lower case comes back unchanged, which is the case worth optimising for; one
// that is not comes back lower whatever this driver does.
func headerPlane() plane {
	return plane{
		name:     "header",
		sep:      HeaderSeparator,
		key:      headerKey,
		mint:     strings.ToLower,
		checkSep: tokenSeparator,
		root:     headerRoot,
		from:     headerFrom,
	}
}

// headerRoot is [queryRoot] on the header plane, and [RootName] fills it there
// too (#338).
//
// The header plane's reading of the raw name is not the identity, and it does
// not happen here: headerKey wraps flatKey, so the name this returns goes
// through the same canonicalisation and the same token-grammar check as a name
// a tag spelled. That is the whole of what one option can be plane-aware about,
// because the name is all the caller stated.
func headerRoot(c config) (rootName, error) {
	return rootName{name: c.root, option: "ferryhttp.RootName"}, nil
}

// queryFrom is the query plane taken out of the context, and a nil one is the
// absence rather than an empty request.
//
// The type assertion alone is not the check. A nil url.Values boxes into a
// non-nil interface, so a handler that called [WithQuery] with nothing passes it
// and the load then answers every field from a map that holds nothing, which is
// the outcome ADR-0012's per-request refusal exists to prevent. An empty but
// allocated url.Values is a request that carries no parameters and loads.
func queryFrom(ctx context.Context) (values, error) {
	v, ok := ctx.Value(queryCtxKey{}).(url.Values)
	if !ok || v == nil {
		return nil, absentPlane(ErrNoQuery)
	}

	return v, nil
}

// headerFrom is [queryFrom] on the header plane, nil included for the same
// reason.
func headerFrom(ctx context.Context) (values, error) {
	h, ok := ctx.Value(headerCtxKey{}).(http.Header)
	if !ok || h == nil {
		return nil, absentPlane(ErrNoHeaders)
	}

	return h, nil
}

// absentPlane is the refusal a per-request driver makes at its open when nothing
// supplied the request, and its class is the one ADR-0012 assigns: a plane that
// was never supplied is the limiting case of a plane that cannot be reached.
func absentPlane(which error) error {
	return fmt.Errorf("%w: %w: put it there with ferryhttp.WithQuery or ferryhttp.WithHeaders",
		ferry.ErrPlane, which)
}

// Source is one request's query parameters or header fields as a ferry plane,
// read side.
//
//	src := ferryhttp.NewQuerySource() // once, at start-up
//	f, err := ferry.Load[Filter](ferryhttp.WithQuery(r.Context(), r.URL.Query()), src)
//
// It carries no request, which is what makes one Source serve every handler: the
// values arrive in the context, per load, through [WithQuery] or [WithHeaders].
// Build one with [NewQuerySource] or [NewHeaderSource]; the zero Source has no
// plane to read and refuses rather than guessing.
//
// A Source is safe for use from many goroutines, which is what net/http running
// every handler in a goroutine of its own requires. The names it computes for a
// type are computed once and never written to afterwards, and everything one
// load needs of its own is allocated when that load starts.
//
// There is no ferryhttp.Sink beside it. This package loads only, so [ferry.Dump]
// through it is a compile error at the call site rather than a failure at run
// time.
type Source struct {
	p   plane
	cfg config
}

// Source is the whole of what this package implements, and the absence of
// [ferry.Sink] beside it is the point rather than an omission.
var _ ferry.Source = (*Source)(nil)

// NewQuerySource builds a [Source] over a request's query parameters.
//
//	src := ferryhttp.NewQuerySource()
//	f, err := ferry.Load[Filter](ferryhttp.WithQuery(r.Context(), r.URL.Query()), src)
//
// With no options it joins nested fields with [QuerySeparator]. Change that with
// [Separator], and name the parameter a schema whose root is a single value
// reads from with [RootName].
func NewQuerySource(opts ...Option) *Source {
	return newSource(queryPlane(), opts)
}

// NewHeaderSource builds a [Source] over a request's header fields.
//
//	src := ferryhttp.NewHeaderSource()
//	t, err := ferry.Load[Tenant](ferryhttp.WithHeaders(r.Context(), r.Header), src)
//
// With no options it joins nested fields with [HeaderSeparator]. Change that
// with [Separator], and name the field a schema whose root is a single value
// reads from with [RootName].
func NewHeaderSource(opts ...Option) *Source {
	return newSource(headerPlane(), opts)
}

func newSource(p plane, opts []Option) *Source {
	c := config{sep: p.sep}
	for _, o := range opts {
		o.apply(&c)
	}

	return &Source{p: p, cfg: c}
}

// Bind computes this schema's parameter or field names and checks them, and it
// is where a schema this plane cannot hold is refused.
//
// Two things are checked, before any request is looked at: that every field has
// a name on this plane at all, and that no two fields render to the same name. A
// schema failing either is refused here, in one error naming every offending
// field along with the one it collided with.
//
// It does no I/O and does not look at the context, so it succeeds whether or not
// a request has been supplied. A load with no request in its context is refused
// when that load starts, which is the first moment the absence is visible.
func (s *Source) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	keys, static, err := bindPlane(s.p, s.cfg, addrs)
	if err != nil {
		return nil, err
	}

	p, cfg := s.p, s.cfg

	return func(ctx context.Context) (ferry.Reader, error) {
		vals, err := p.from(ctx)
		if err != nil {
			return nil, err
		}

		return newReader(p, cfg, keys, static, vals), nil
	}, nil
}

// bindPlane is the whole of binding, for either direction: check the options,
// build the checked name table, and read it back the other way.
func bindPlane(p plane, cfg config, addrs *ferry.AddressSet) (*ferry.Keys, map[string]ferry.Path, error) {
	sep := cfg.sep

	if err := p.checkSep(sep); err != nil {
		return nil, nil, err
	}

	if cfg.bytesErr != nil {
		return nil, nil, cfg.bytesErr
	}

	root, err := p.root(cfg)
	if err != nil {
		return nil, nil, err
	}

	keys, err := ferry.NewKeys(addrs, p.name, p.key(sep, root))
	if err != nil {
		return nil, nil, err
	}

	static, err := staticNames(addrs, keys)
	if err != nil {
		return nil, nil, err
	}

	return keys, static, nil
}

// staticNames is the precomputed table read backwards: every name the type
// determined, mapped to the address that determined it.
//
// It is what makes the static tier of enumeration exact. Both key functions are
// many-to-one over segment text - the query one because a part may itself
// contain the separator, the header one because it folds case as well - so a
// name cannot be parsed back into an address in general. An address the schema
// determined is in this table, so matching a name against it recovers the part's
// own spelling rather than a fold of it, and only what the request mints has to
// be spelled back by the plane's own rule.
//
// It is built once per Bind and never written to afterwards, which is what lets
// one binding be read from many goroutines with no synchronisation.
func staticNames(addrs *ferry.AddressSet, keys *ferry.Keys) (map[string]ferry.Path, error) {
	out := make(map[string]ferry.Path, addrs.Len())
	name := keys.Open()

	// The kind each member carries is what a driver classifies on at Bind
	// (ADR-0016), and this table is the one place that does not need it: a name
	// is a function of the segments, and reading it back recovers the spelling
	// whatever kind of place the address names.
	for m := range addrs.Seq() {
		addr := m.Path()

		key, err := name(addr)
		if err != nil {
			// Unreachable: NewKeys computed a name for every address in this
			// set already, and the table answers a static address without
			// consulting the key function again. It is returned rather than
			// ignored because a driver that swallows an error here would be
			// deciding that core was wrong.
			return nil, err
		}

		out[key] = addr
	}

	return out, nil
}
