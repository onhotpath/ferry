package httpdecisions

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
)

// The schemas the four questions are stated against.

// Tagged is the case: a sequence field pointed at a name a browser form
// repeats.
type Tagged struct {
	Tags []string `ferry:"tags"`
}

// Scalar is the sharp edge: a single-valued field pointed at a repeated name.
type Scalar struct {
	Q string `ferry:"q"`
}

// Both is what a real handler's request struct looks like.
type Both struct {
	Tags []string `ferry:"tags"`
	Q    string   `ferry:"q"`
}

// TwoScalars is question 4's second-address case: two names, both repeated, so
// the report has two elements to carry.
type TwoScalars struct {
	Q string `ferry:"q"`
	R string `ferry:"r"`
}

// Mapped is the map case.
type Mapped struct {
	Limits map[string]int `ferry:"limits"`
}

// Collide is question 1's injectivity case: a sequence and a scalar whose
// static key is the sequence's own element key.
type Collide struct {
	Tags []string `ferry:"tags"`
	Zero string   `ferry:"tags.0"`
}

// DeepCollide is the injectivity case that ferry.NewKeys genuinely cannot see:
// the sequence lives under a minted map key, so the plane key the repeated
// sink collapses onto is itself minted and was never in the address set.
type DeepCollide struct {
	M map[string][]string `ferry:"m"`
	X string              `ferry:"m.k"`
}

// Encodings is the header handler that wants a sequence.
type Encodings struct {
	Encodings []string `ferry:"accept-encoding"`
}

// Encoding is the second handler, whose field at the same name is a scalar.
// Question 3 is whether one Source can serve both.
type Encoding struct {
	Encoding string `ferry:"accept-encoding"`
}

// loadQuery runs one raw query string into T and reports the result exactly as a
// caller sees it.
func loadQuery[T any](t *testing.T, raw string, opts ...Option) string {
	t.Helper()

	v, err := url.ParseQuery(raw)
	if err != nil {
		return "PARSE " + err.Error()
	}

	got, err := ferry.Load[T](WithQuery(context.Background(), v), NewQuerySource(Enumerated, opts...))

	return outcome(got, err)
}

// loadQueryShape is the same with the shape as an explicit parameter, for the
// rows question 2 states against `indexed`.
func loadQueryShape[T any](t *testing.T, s Shape, raw string, opts ...Option) string {
	t.Helper()

	v, err := url.ParseQuery(raw)
	if err != nil {
		return "PARSE " + err.Error()
	}

	got, err := ferry.Load[T](WithQuery(context.Background(), v), NewQuerySource(s, opts...))

	return outcome(got, err)
}

// loadHeader is the same over a header block, built the way net/http hands one
// to a handler.
func loadHeader[T any](t *testing.T, pairs [][2]string, opts ...Option) string {
	t.Helper()

	h := http.Header{}
	for _, p := range pairs {
		h.Add(p[0], p[1])
	}

	got, err := ferry.Load[T](WithHeaders(context.Background(), h), NewHeaderSource(Enumerated, opts...))

	return outcome(got, err)
}

// outcome renders a load's result in one line.
func outcome[T any](got T, err error) string {
	if err == nil {
		return fmt.Sprintf("%+v", got)
	}

	els := ferry.Elements(err)
	if len(els) <= 1 {
		return classOf(err) + " " + oneLine(err.Error())
	}

	parts := make([]string, 0, len(els))
	for _, e := range els {
		parts = append(parts, classOf(e)+" "+oneLine(e.Error()))
	}

	return strings.Join(parts, " || ")
}

func classOf(err error) string {
	switch {
	case errors.Is(err, ferry.ErrValue):
		return "ErrValue:"
	case errors.Is(err, ferry.ErrPlane):
		return "ErrPlane:"
	case errors.Is(err, ferry.ErrMissing):
		return "ErrMissing:"
	default:
		return "err:"
	}
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// kinds is what both planes carry end to end. driver/env and driver/kv declare
// the same list.
func kinds() []ferry.VKind {
	return []ferry.VKind{
		ferry.KindAbsent, ferry.KindBool,
		ferry.KindNumber, ferry.KindString, ferry.KindBytes,
	}
}

// recorder captures what a suite reports instead of failing the run, which
// ferrytest.T exists to allow.
type recorder struct {
	t    *testing.T
	errs []string
}

func (r *recorder) Helper() { r.t.Helper() }

func (r *recorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, oneLine(fmt.Sprintf(format, args...)))
}
