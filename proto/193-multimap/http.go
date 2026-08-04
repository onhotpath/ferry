package multimap

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/onhotpath/ferry"
)

type queryCtxKey struct{}

type headerCtxKey struct{}

// WithQuery puts a request's query parameters in the context. Both planes are
// per request, so both come from the context and neither is held by the Source.
func WithQuery(ctx context.Context, v url.Values) context.Context {
	return context.WithValue(ctx, queryCtxKey{}, v)
}

// WithHeaders puts a request's headers in the context.
func WithHeaders(ctx context.Context, h http.Header) context.Context {
	return context.WithValue(ctx, headerCtxKey{}, h)
}

func planeFrom(ctx context.Context, key any) (values, error) {
	switch v := ctx.Value(key).(type) {
	case url.Values:
		return v, nil
	case http.Header:
		return v, nil
	default:
		return nil, errors.Join(ferry.ErrPlane, errNoPlane)
	}
}

// Option configures a plane.
type Option func(*plane)

// Repeatable declares that a plane key carries a sequence. It is [Declared]'s
// whole mechanism, and it is driver configuration rather than tag grammar: the
// struct tag says nothing new, so #34 stays parked.
//
// The names are plane keys and not field names, because that is what the driver
// has: "tags" on the query plane, "Tags" on the header plane.
func Repeatable(keys ...string) Option {
	return func(p *plane) {
		for _, k := range keys {
			p.declared[k] = true
		}
	}
}

// Trace collects every boundary call a plane's reader and writer make, in
// order, so the walk's call sequence can be pasted rather than described.
func Trace(into *[]string) Option {
	return func(p *plane) { p.trace = into }
}

// QueryPlane builds the query-parameter plane's configuration in one shape.
func QueryPlane(s Shape, opts ...Option) plane {
	p := plane{
		name:     "query",
		shape:    s,
		keyf:     flatKey(QuerySeparator),
		sep:      QuerySeparator,
		mintf:    func(t string) string { return t },
		declared: map[string]bool{},
	}
	for _, o := range opts {
		o(&p)
	}

	return p
}

// HeaderPlane builds the header plane's configuration in one shape.
//
// The mint is a lower-case fold, which is this plane's [driver/env] Canonical
// analogue: net/http canonicalises a field name on the way in, so the segment
// spelling a map key arrived with is destroyed and the driver has to choose one
// to hand back. A schema address is recovered exactly from the static table; only
// what the value mints falls back on the fold.
func HeaderPlane(s Shape, opts ...Option) plane {
	p := plane{
		name:     "header",
		shape:    s,
		keyf:     headerKey(),
		sep:      HeaderSeparator,
		mintf:    strings.ToLower,
		declared: map[string]bool{},
	}
	for _, o := range opts {
		o(&p)
	}

	return p
}

// Source is a read half over a plane taken from the context.
type Source struct {
	p  plane
	ck any
}

// Sink is a write half over a plane taken from the context.
type Sink struct {
	p  plane
	ck any
}

var (
	_ ferry.Source = (*Source)(nil)
	_ ferry.Sink   = (*Sink)(nil)
)

// NewQuerySource builds the read half of the query plane.
func NewQuerySource(s Shape, opts ...Option) *Source {
	return &Source{p: QueryPlane(s, opts...), ck: queryCtxKey{}}
}

// NewQuerySink builds the write half of the query plane.
func NewQuerySink(s Shape, opts ...Option) *Sink {
	return &Sink{p: QueryPlane(s, opts...), ck: queryCtxKey{}}
}

// NewHeaderSource builds the read half of the header plane.
func NewHeaderSource(s Shape, opts ...Option) *Source {
	return &Source{p: HeaderPlane(s, opts...), ck: headerCtxKey{}}
}

// NewHeaderSink builds the write half of the header plane.
func NewHeaderSink(s Shape, opts ...Option) *Sink {
	return &Sink{p: HeaderPlane(s, opts...), ck: headerCtxKey{}}
}

// Bind computes this schema's plane keys and checks them, before any request is
// looked at.
func (s *Source) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	keys, static, err := s.p.bind(addrs)
	if err != nil {
		return nil, err
	}

	p := s.p

	return func(ctx context.Context) (ferry.Reader, error) {
		vals, verr := planeFrom(ctx, s.ck)
		if verr != nil {
			return nil, verr
		}

		return newReader(p, keys, static, vals), nil
	}, nil
}

// Bind is the write half's.
func (s *Sink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	keys, _, err := s.p.bind(addrs)
	if err != nil {
		return nil, err
	}

	p := s.p

	return func(ctx context.Context) (ferry.Writer, error) {
		vals, verr := planeFrom(ctx, s.ck)
		if verr != nil {
			return nil, verr
		}

		return &writer{p: p, keys: keys.Open(), vals: vals, repeat: p.shape.positionsBehindName()}, nil
	}, nil
}

// newReader mints the reader a shape needs: the audit shape's carries a
// [ferry.Releaser] and no other shape's does, because the interface is
// discovered by assertion.
func newReader(p plane, keys *ferry.Keys, static map[string]ferry.Path, vals values) ferry.Reader {
	r := &reader{p: p, keys: keys.Open(), static: static, vals: vals, hid: map[string]ferry.Path{}}
	if p.shape == CardinalityAudit || p.shape == Enumerated {
		return &auditReader{reader: r}
	}

	return r
}

// Fixed is a Source and a Sink over a plane held by the pair rather than taken
// from the context. It exists only so ferrytest.Driver, which builds its own
// context and knows nothing about WithQuery, can drive this driver.
func Fixed(p plane, v values) (ferry.Source, ferry.Sink) {
	return &fixedSource{p: p, v: v}, &fixedSink{p: p, v: v}
}

type fixedSource struct {
	p plane
	v values
}

type fixedSink struct {
	p plane
	v values
}

var (
	_ ferry.Source = (*fixedSource)(nil)
	_ ferry.Sink   = (*fixedSink)(nil)
)

func (s *fixedSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	keys, static, err := s.p.bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(context.Context) (ferry.Reader, error) {
		return newReader(s.p, keys, static, s.v), nil
	}, nil
}

func (s *fixedSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	keys, _, err := s.p.bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(context.Context) (ferry.Writer, error) {
		return &writer{p: s.p, keys: keys.Open(), vals: s.v, repeat: s.p.shape.positionsBehindName()}, nil
	}, nil
}
