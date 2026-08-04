package httpdecisions

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// Question 4: when and where does the scalar refusal land?
//
// ?q=a&q=b into Q string refuses. The refusal a reader wants is at Get, at /q,
// during the walk, and that needs ferrytest.Driver case 3 relaxed, which is
// #208 and out of scope. So: what is the best refusal available without #208?

// TestQ4WhatACallerSees is the whole of the question: the full report, per
// option, for the case the driver exists to get right.
func TestQ4WhatACallerSees(t *testing.T) {
	for _, r := range Refusals() {
		v := url.Values{"q": {"a", "b"}}

		got, err := ferry.Load[Scalar](WithQuery(context.Background(), v), NewQuerySource(Enumerated, WithRefusal(r)))

		t.Logf("")
		t.Logf("=== %s ===", r)
		t.Logf("value: %+v", got)

		if err == nil {
			t.Logf("error: <nil>")

			continue
		}

		t.Logf("%%v : %v", err)
		t.Logf("%%+v: %+v", err)
		t.Logf("Address()            = %q", addressOf(err))
		t.Logf("errors.Is ErrPlane   = %v", errors.Is(err, ferry.ErrPlane))
		t.Logf("errors.Is ErrDriver  = %v", errors.Is(err, ferry.ErrDriver))
		t.Logf("errors.Is ErrRepeated= %v", errors.Is(err, ErrRepeated))
		t.Logf("Elements()           = %d", len(ferry.Elements(err)))
	}
}

// TestQ4TwoAddresses asks what the Close-located report does with more than one
// offending name, which is the case a real filter struct reaches first.
func TestQ4TwoAddresses(t *testing.T) {
	for _, r := range []Refusal{RefuseAtCloseInText, RefuseAtCloseWithErrorAt, RefuseAtCloseHybrid} {
		v := url.Values{"q": {"a", "b"}, "r": {"c", "d"}}

		_, err := ferry.Load[TwoScalars](WithQuery(context.Background(), v),
			NewQuerySource(Enumerated, WithRefusal(r)))

		t.Logf("")
		t.Logf("=== %s, two repeated names ===", r)
		t.Logf("%%+v: %+v", err)
		t.Logf("Elements() = %d, first address = %q", len(ferry.Elements(err)), addressOf(err))
	}
}

// TestQ4AlongsideAWalkFailure is how the Close-located refusal reads in a report
// that already has a walk failure in it, which is where a moment matters.
func TestQ4AlongsideAWalkFailure(t *testing.T) {
	// ?limits=a&limits=b fails in the walk at /limits, and ?q=a&q=b is hidden
	// and never enumerated, so it is reported at Close.
	type both struct {
		Limits map[string]int `ferry:"limits"`
		Q      string         `ferry:"q"`
	}

	v := url.Values{"limits": {"a", "b"}, "q": {"a", "b"}}

	_, err := ferry.Load[both](WithQuery(context.Background(), v),
		NewQuerySource(Enumerated, WithRefusal(RefuseAtCloseWithErrorAt)))

	t.Logf("%%v : %v", err)
	t.Logf("%%+v: %+v", err)

	for i, e := range ferry.Elements(err) {
		t.Logf("element %d: address=%q  %v", i, addressOf(e), e)
	}
}

// TestQ4RefuseAtGetAgainstTheSuite is what #208 costs to stay closed: the
// refusal a reader wants, run against the shipped conformance suite.
func TestQ4RefuseAtGetAgainstTheSuite(t *testing.T) {
	for _, r := range Refusals() {
		rec := &recorder{t: t}

		ferrytest.Driver(rec, ferrytest.Plane{
			Name:  "query",
			Kinds: kinds(),
			Open: func() ferrytest.Instance {
				v := url.Values{}
				src, sink := Fixed(QueryPlane(Enumerated, WithRefusal(r)), v)

				return ferrytest.Instance{Source: src, Sink: sink}
			},
		})

		t.Logf("%-20s ferrytest.Driver failures: %d", r, len(rec.errs))

		for _, l := range rec.errs {
			t.Logf("      %s", l)
		}
	}
}

// TestQ4RefuseAtGetOnTheRealCase shows what the refusal #208 would allow reads
// like, so that "better" is a comparison and not an assertion.
func TestQ4RefuseAtGetOnTheRealCase(t *testing.T) {
	v := url.Values{"q": {"a", "b"}}

	_, err := ferry.Load[Scalar](WithQuery(context.Background(), v),
		NewQuerySource(Enumerated, WithRefusal(RefuseAtGet)))

	t.Logf("%%+v: %+v", err)
	t.Logf("Address() = %q", addressOf(err))
}

// TestQ4TheSequenceStillWorks asserts that the refusal does not fire where the
// same input is read as the sequence it is, which is the whole reason the
// refusal has to wait for the walk to finish.
func TestQ4TheSequenceStillWorks(t *testing.T) {
	for _, r := range Refusals() {
		t.Logf("%-20s ?tags=a&tags=b -> []string: %s", r,
			loadQuery[Tagged](t, "tags=a&tags=b", WithRefusal(r)))
	}
}

// TestQ4TheTrace is the call sequence the refusal is built on, so that "the
// driver cannot know until the walk is over" is shown rather than argued.
func TestQ4TheTrace(t *testing.T) {
	for _, c := range []struct {
		label string
		raw   string
		run   func(*testing.T, string, ...Option) string
	}{
		{"?q=a&q=b   into Q string", "q=a&q=b", loadQuery[Scalar]},
		{"?tags=a&tags=b into Tags []string", "tags=a&tags=b", loadQuery[Tagged]},
	} {
		var trace []string

		out := c.run(t, c.raw, Trace(&trace), WithRefusal(RefuseAtCloseWithErrorAt))

		t.Logf("")
		t.Logf("=== %s ===", c.label)

		for _, l := range trace {
			t.Logf("    %s", l)
		}

		t.Logf("    => %s", out)
	}
}
