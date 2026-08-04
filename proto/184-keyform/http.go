package keyform

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/onhotpath/ferry"
)

// ErrNoPlane reports a load or a dump through one of these sources with no
// plane in the context. Both planes are per request, so both come from the
// context and neither is held by the Source.
var ErrNoPlane = errors.New("keyform: no plane in the context")

type queryCtxKey struct{}

type headerCtxKey struct{}

// WithQuery puts a request's query parameters in the context.
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
		return nil, errors.Join(ferry.ErrPlane, ErrNoPlane)
	}
}

// QuerySource is the query-parameter plane, read side.
type QuerySource struct{ p plane }

// QuerySink is the query-parameter plane, write side.
type QuerySink struct{ p plane }

// HeaderSource is the header plane, read side.
type HeaderSource struct{ p plane }

// HeaderSink is the header plane, write side.
type HeaderSink struct{ p plane }

var (
	_ ferry.Source = (*QuerySource)(nil)
	_ ferry.Sink   = (*QuerySink)(nil)
	_ ferry.Source = (*HeaderSource)(nil)
	_ ferry.Sink   = (*HeaderSink)(nil)
)

func queryPlane(f Form, sep string) plane {
	p := plane{name: "query", keyf: f.Query(sep), mintf: func(s string) string { return s }}

	if f == Bracket || f == BracketStrict {
		p.cut = bracketCut{}
	} else {
		p.cut = flatCut{sep: sep}
	}

	return p
}

func headerPlane(f Form) plane {
	p := plane{
		name:  "header",
		keyf:  f.Header(HeaderSeparator),
		mintf: strings.ToLower,
	}

	if f == Bracket || f == BracketStrict {
		p.cut = bracketCut{}
	} else {
		p.cut = flatCut{sep: HeaderSeparator}
	}

	return p
}

// NewQuerySource builds the read half of the query plane in one of the forms.
func NewQuerySource(f Form, sep string) *QuerySource { return &QuerySource{p: queryPlane(f, sep)} }

// NewQuerySink builds the write half.
func NewQuerySink(f Form, sep string) *QuerySink { return &QuerySink{p: queryPlane(f, sep)} }

// NewHeaderSource builds the read half of the header plane.
func NewHeaderSource(f Form) *HeaderSource { return &HeaderSource{p: headerPlane(f)} }

// NewHeaderSink builds the write half.
func NewHeaderSink(f Form) *HeaderSink { return &HeaderSink{p: headerPlane(f)} }

// NewHeaderDepth1Source is the header plane that refuses to nest at all.
func NewHeaderDepth1Source() *HeaderSource {
	return &HeaderSource{p: plane{
		name:  "header",
		keyf:  HeaderDepth1(),
		cut:   flatCut{sep: HeaderSeparator},
		mintf: strings.ToLower,
	}}
}

func (s *QuerySource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	return bindSource(s.p, addrs, queryCtxKey{})
}

func (s *HeaderSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	return bindSource(s.p, addrs, headerCtxKey{})
}

func (s *QuerySink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	return bindSink(s.p, addrs, queryCtxKey{})
}

func (s *HeaderSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	return bindSink(s.p, addrs, headerCtxKey{})
}

func bindSource(p plane, addrs *ferry.AddressSet, ck any) (ferry.OpenFunc, error) {
	keys, static, err := p.bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		vals, verr := planeFrom(ctx, ck)
		if verr != nil {
			return nil, verr
		}

		return &reader{p: p, keys: keys.Open(), static: static, vals: vals}, nil
	}, nil
}

func bindSink(p plane, addrs *ferry.AddressSet, ck any) (ferry.OpenWriterFunc, error) {
	keys, _, err := p.bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Writer, error) {
		vals, verr := planeFrom(ctx, ck)
		if verr != nil {
			return nil, verr
		}

		return &writer{keys: keys.Open(), vals: vals}, nil
	}, nil
}

// Keys is the prototype's way of getting at the plane keys for a schema
// without a load: it binds against an address set and returns the table.
func Keys(p plane, addrs *ferry.AddressSet) (map[ferry.Path]string, error) {
	keys, err := ferry.NewKeys(addrs, p.name, p.keyf)
	if err != nil {
		return nil, err
	}

	out := map[ferry.Path]string{}
	name := keys.Open()

	for addr := range addrs.All() {
		key, kerr := name(addr)
		if kerr != nil {
			return nil, kerr
		}

		out[addr] = key
	}

	return out, nil
}

// fixedCtxKey carries a plane handed to a Fixed source at construction, which
// is what lets ferrytest drive this driver: the conformance suites build their
// own context and know nothing about WithQuery.
type fixedCtxKey struct{}

// FixedQuery is a QuerySource over a url.Values held by the source itself
// rather than taken from the context. It exists only so ferrytest.Driver and
// ferrytest.RoundTrip can run against this plane.
func FixedQuery(f Form, sep string, v url.Values) (*fixedSource, *fixedSink) {
	p := queryPlane(f, sep)

	return &fixedSource{p: p, v: v}, &fixedSink{p: p, v: v}
}

// FixedHeader is the same over an http.Header.
func FixedHeader(f Form, h http.Header) (*fixedSource, *fixedSink) {
	p := headerPlane(f)

	return &fixedSource{p: p, v: h}, &fixedSink{p: p, v: h}
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
		return &reader{p: s.p, keys: keys.Open(), static: static, vals: s.v}, nil
	}, nil
}

func (s *fixedSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	keys, _, err := s.p.bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(context.Context) (ferry.Writer, error) {
		return &writer{keys: keys.Open(), vals: s.v}, nil
	}, nil
}
