package httpdecisions

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// Question 2: what does a name carrying both spellings mean?
//
// ?tags=a&tags=b&tags.0=z addresses /tags#0 twice.

// clashCases are every query string in which one name carries a sequence in the
// plane's own repetition and in index-suffixed names at once.
func clashCases() []string {
	return []string{
		"tags=a&tags=b&tags.0=z",
		"tags=a&tags=b&tags.1=z",
		"tags=a&tags.0=z",
		"tags=a&tags=b&tags.2=z",
		"tags=a&tags=b&tags.5=z",
		"tags.0=a&tags.1=b",
		"tags=a&tags=b",
	}
}

// TestQ2Matrix is every policy against every case, as a caller sees it.
func TestQ2Matrix(t *testing.T) {
	for _, c := range Clashes() {
		t.Logf("")
		t.Logf("=== %s ===", c)

		for _, raw := range clashCases() {
			t.Logf("  ?%-24s -> %s", raw, loadQuery[Tagged](t, raw, WithClash(c)))
		}
	}

	t.Logf("")
	t.Logf("=== indexed, for reference (positions in the name, no second dimension) ===")

	for _, raw := range clashCases() {
		t.Logf("  ?%-24s -> %s", raw, loadQueryShape[Tagged](t, Indexed, raw))
	}
}

// TestQ2FirstSpellingIsNotExpressible is why one of the four options in the
// question is not a candidate: the plane a handler is handed has already lost
// the wire order of two different names.
func TestQ2FirstSpellingIsNotExpressible(t *testing.T) {
	for _, pair := range [][2]string{
		{"tags=a&tags=b&tags.0=z", "tags.0=z&tags=a&tags=b"},
		{"tags.0=z&tags=a&tags=b", "tags=a&tags.0=z&tags=b"},
	} {
		one, err := url.ParseQuery(pair[0])
		if err != nil {
			t.Fatalf("parse: %v", err)
		}

		two, err := url.ParseQuery(pair[1])
		if err != nil {
			t.Fatalf("parse: %v", err)
		}

		t.Logf("?%-24s -> %s", pair[0], FmtValues(one))
		t.Logf("?%-24s -> %s", pair[1], FmtValues(two))
		t.Logf("   the two wire orders parse to the same plane: %v", reflect.DeepEqual(one, two))
	}

	t.Logf("")
	t.Logf("url.Values and http.Header are map[string][]string. The order of two values under one name " +
		"survives; the order of two different names is not in the type at all, so no driver can read it.")
}

// TestQ2Reachability asks whether a refusal inside Children survives the shipped
// conformance suite, and whether #208 has anything to say about it.
//
// Case 3 is about Get. This refusal is not in Get.
func TestQ2Reachability(t *testing.T) {
	for _, c := range Clashes() {
		rec := &recorder{t: t}

		ferrytest.Driver(rec, ferrytest.Plane{
			Name:  "query/" + c.String(),
			Kinds: kinds(),
			Open: func() ferrytest.Instance {
				v := url.Values{}
				src, sink := Fixed(QueryPlane(Enumerated, WithClash(c)), v)

				return ferrytest.Instance{Source: src, Sink: sink}
			},
		})

		t.Logf("%-24s ferrytest.Driver failures: %d", c, len(rec.errs))

		for _, l := range rec.errs {
			t.Logf("      %s", l)
		}
	}
}

// TestQ2WhatTheCallerSees is the full report for each policy that refuses or
// reports, which is what decides whether a caller who hits this accidentally can
// act on it.
func TestQ2WhatTheCallerSees(t *testing.T) {
	for _, c := range []Clash{ClashRefuse, ClashRepeatedWinsAudited} {
		v, err := url.ParseQuery("tags=a&tags=b&tags.0=z")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}

		_, lerr := ferry.Load[Tagged](WithQuery(context.Background(), v),
			NewQuerySource(Enumerated, WithClash(c)))

		t.Logf("")
		t.Logf("=== %s ===", c)
		t.Logf("%%v:  %v", lerr)
		t.Logf("%%+v: %+v", lerr)
		t.Logf("errors.Is(err, ferry.ErrPlane)=%v  ferry.ErrDriver=%v  ErrTwoSpellings=%v",
			is(lerr, ferry.ErrPlane), is(lerr, ferry.ErrDriver), is(lerr, ErrTwoSpellings))

		for _, e := range ferry.Elements(lerr) {
			t.Logf("element: address=%q  %v", addressOf(e), e)
		}
	}
}

func is(err, target error) bool { return err != nil && errors.Is(err, target) }

// TestQ2TheAccidentalCase is what a caller who hits this by accident actually
// did: an HTML form with a hidden field beside a checkbox group.
func TestQ2TheAccidentalCase(t *testing.T) {
	for _, c := range Clashes() {
		t.Logf("%-24s form posts tags=a&tags=b with a hidden tags.0=z -> %s",
			c, loadQuery[Tagged](t, "tags=a&tags=b&tags.0=z", WithClash(c)))
	}
}

// addressOf is what a caller reads off one element of the report.
func addressOf(err error) string {
	var e *ferry.Error
	if errors.As(err, &e) {
		return e.Address().String()
	}

	return "(none)"
}
