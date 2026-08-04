package httpdecisions

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

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

// Repeatable is question 3's mechanism: the Source is told, in its own
// configuration, which plane keys carry a sequence.
//
// It is driver configuration rather than tag grammar, so #34 stays parked.
func Repeatable(keys ...string) Option {
	return func(p *plane) {
		for _, k := range keys {
			p.declared[k] = true
			p.declaredNames = append(p.declaredNames, k)
		}
	}
}

// CheckDeclaration turns on the Bind-time check of the declaration against the
// address set the schema determines.
func CheckDeclaration() Option {
	return func(p *plane) { p.checkDeclared = true }
}

// WithClash sets question 2's policy.
func WithClash(c Clash) Option {
	return func(p *plane) { p.clash = c }
}

// WithRefusal sets question 4's policy.
func WithRefusal(r Refusal) Option {
	return func(p *plane) { p.refusal = r }
}

// WithSpelling sets which spelling the sink emits for a sequence.
func WithSpelling(s SinkSpelling) Option {
	return func(p *plane) { p.spelling = s }
}

// WithSetSemantics sets what Writer.Set does at a key the plane already holds
// values at.
func WithSetSemantics(s SetSemantics) Option {
	return func(p *plane) { p.setSem = s }
}

// Trace collects every boundary call a plane's reader and writer make, in order.
func Trace(into *[]string) Option {
	return func(p *plane) { p.trace = into }
}

// QueryPlane builds the query-parameter plane's configuration.
func QueryPlane(s Shape, opts ...Option) plane {
	return build(plane{
		name:     "query",
		shape:    s,
		keyf:     flatKey(QuerySeparator),
		sep:      QuerySeparator,
		mintf:    func(t string) string { return t },
		declared: map[string]bool{},
	}, opts)
}

// HeaderPlane builds the header plane's configuration.
func HeaderPlane(s Shape, opts ...Option) plane {
	return build(plane{
		name:     "header",
		shape:    s,
		keyf:     headerKey(),
		sep:      HeaderSeparator,
		mintf:    strings.ToLower,
		declared: map[string]bool{},
	}, opts)
}

func build(p plane, opts []Option) plane {
	for _, o := range opts {
		o(&p)
	}

	return p
}

// Source is a read half over a plane taken from the context.
type Source struct {
	p  plane
	ck any

	// binds counts how often core asked this Source for its address set, which
	// is question 3's measurement: a Source cannot enforce one schema per
	// source if it is re-bound on every load.
	binds atomic.Int64
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

// Binds is how often core has asked this Source for its address set.
func (s *Source) Binds() int64 { return s.binds.Load() }

// Bind computes this schema's plane keys and checks them, before any request is
// looked at.
func (s *Source) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	s.binds.Add(1)

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

		return newWriter(p, keys, vals), nil
	}, nil
}

// newReader mints the reader a configuration needs: one that reports at Close
// carries a ferry.Releaser and one that does not must not have the method at
// all, because the interface is discovered by assertion.
func newReader(p plane, keys *ferry.Keys, static map[string]ferry.Path, vals values) ferry.Reader {
	r := &reader{
		p: p, keys: keys.Open(), static: static, vals: vals,
		hid: map[string]ferry.Path{}, lost: map[string]ferry.Path{},
	}

	if p.refusal == RefuseAtCloseInText || p.refusal == RefuseAtCloseWithErrorAt ||
		p.refusal == RefuseAtCloseHybrid || p.clash == ClashRepeatedWinsAudited {
		return &auditReader{reader: r}
	}

	return r
}

func newWriter(p plane, keys *ferry.Keys, vals values) ferry.Writer {
	return &writer{p: p, keys: keys.Open(), vals: vals, cleared: map[string]bool{}}
}

// Fixed is a Source and a Sink over a plane held by the pair rather than taken
// from the context. It exists only so ferrytest.Driver, which builds its own
// context and knows nothing about WithQuery, can drive this driver.
//
// It is also what driver/env already does: env ships no Sink and its tests
// supply a stand-in one over the same plane, so the five conformance cases that
// guard on a nil sink actually run.
func Fixed(p plane, v values) (ferry.Source, ferry.Sink) {
	return &fixedSource{p: p, v: v}, &fixedSink{p: p, v: v}
}

// FixedSourceOnly is the same read half with no sink beside it, which is what a
// source-only driver hands ferrytest.Driver if it supplies no stand-in.
func FixedSourceOnly(p plane, v values) ferry.Source {
	return &fixedSource{p: p, v: v}
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
		return newWriter(s.p, keys, s.v), nil
	}, nil
}
