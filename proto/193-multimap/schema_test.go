package multimap

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

// The schemas #193 names.

// Tagged is the case: a sequence field pointed at a name a browser form
// repeats.
type Tagged struct {
	Tags []string `ferry:"tags"`
}

// Scalar is the sharp edge under every shape: a single-valued field pointed at
// a repeated name.
type Scalar struct {
	Q string `ferry:"q"`
}

// Empty is ADR-0004's row that an empty string is not an absence.
type Empty struct {
	X string `ferry:"x"`
}

// Mapped needs Children at a map address for a second reason, and interacts
// with invertibility the way driver/env's Canonical option does.
type Mapped struct {
	Limits map[string]int `ferry:"limits"`
}

// AcceptSeq is the header equivalent of Tagged. A repeated Accept-Encoding is
// two entries under one field name.
type AcceptSeq struct {
	Encodings []string `ferry:"accept-encoding"`
}

// AcceptOne is the header equivalent of Scalar.
type AcceptOne struct {
	Encoding string `ferry:"accept-encoding"`
}

// Both is the schema that puts a sequence and a scalar side by side, which is
// what a real handler's request struct looks like.
type Both struct {
	Tags []string `ferry:"tags"`
	Q    string   `ferry:"q"`
}

// Fixed2 is an array, whose length the type fixes.
type Fixed2 struct {
	Pair [2]string `ferry:"pair"`
}

// HFixed2 is the header plane's array case.
type HFixed2 struct {
	Pair [2]string `ferry:"pair"`
}

// HBoth is the header plane's sequence-beside-scalar case.
type HBoth struct {
	Encodings []string `ferry:"accept-encoding"`
	Q         string   `ferry:"q"`
}

// loadQuery runs one raw query string into T through one shape and reports the
// result exactly as a caller sees it.
func loadQuery[T any](s Shape, raw string, opts ...Option) string {
	v, err := url.ParseQuery(raw)
	if err != nil {
		return "PARSE " + err.Error()
	}

	src := NewQuerySource(s, opts...)
	got, err := ferry.Load[T](WithQuery(context.Background(), v), src)

	return outcome(got, err)
}

// loadHeader is the same over a header block, built the way net/http hands one
// to a handler: canonicalised names, repeated values under one name.
func loadHeader[T any](s Shape, pairs [][2]string, opts ...Option) string {
	h := http.Header{}
	for _, p := range pairs {
		h.Add(p[0], p[1])
	}

	src := NewHeaderSource(s, opts...)
	got, err := ferry.Load[T](WithHeaders(context.Background(), h), src)

	return outcome(got, err)
}

// outcome renders a load's result in one line: the value, or the class and the
// first line of the message.
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

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// captureSource is how the prototype gets at the AddressSet a schema
// determines: a real ferry.Source whose Bind keeps what core hands it.
type captureSource struct{ addrs *ferry.AddressSet }

func (c *captureSource) Bind(a *ferry.AddressSet) (ferry.OpenFunc, error) {
	c.addrs = a

	return nil, errStop
}

var errStop = errors.New("captured")

func addrsOf[T any](t *testing.T) *ferry.AddressSet {
	t.Helper()

	c := &captureSource{}
	if _, err := ferry.Load[T](context.Background(), c); !errors.Is(err, errStop) {
		t.Fatalf("capture: %v", err)
	}

	return c.addrs
}

// TestTheDriverCannotSeeTheSchema is the premise everything else rests on, and
// it is asserted rather than assumed: a container address and a leaf address
// arrive at Bind indistinguishable, so Reader.Get cannot tell them apart.
func TestTheDriverCannotSeeTheSchema(t *testing.T) {
	seq := addrsOf[Tagged](t)
	sca := addrsOf[Scalar](t)

	show := func(label string, a *ferry.AddressSet) []string {
		out := []string{}
		for addr := range a.All() {
			out = append(out, addr.String())
		}

		t.Logf("%-28s AddressSet = %v (len %d)", label, out, a.Len())

		return out
	}

	s := show("Tagged{Tags []string}", seq)
	q := show("Scalar{Q string}", sca)

	t.Logf("a container address and a leaf address are the same shape at the boundary: "+
		"%d segment(s) each, no kind, no arity", len(s))

	if len(s) != 1 || len(q) != 1 {
		t.Fatalf("expected one address each, got %v and %v", s, q)
	}
}
