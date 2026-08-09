package ferryhttp

import (
	"net/textproto"
	"strings"
	"testing"
)

// This file is the prototype's negative result: the best sniff that could
// recover crossed-use detection from one option, measured against a corpus of
// names a real caller would write. It is not shipped and nothing in the driver
// calls it.

// sniffCrossed is the strongest guess available to a query source: refuse a
// RootName that is already in canonical header-field spelling and holds a
// hyphen, on the theory that only a header field is written X-Request-Id.
//
// A header source has no counterpart sniff at all. Every query parameter name
// worth writing is also a legal field name, and canonicalisation swallows the
// case difference, so there is nothing on that side to look at.
func sniffCrossed(name string) bool {
	return strings.Contains(name, "-") && textproto.CanonicalMIMEHeaderKey(name) == name
}

// TestTheSniffCatchesTheObviousCrossing is the case it was built for.
func TestTheSniffCatchesTheObviousCrossing(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"X-Request-Id", "Authorization-Token", "X-Tenant"} {
		if !sniffCrossed(name) {
			t.Errorf("the sniff let %q through on a query source", name)
		}
	}
}

// TestTheSniffIsWrongBothWays is why it cannot ship.
//
// The false positives are ordinary query parameters a real API serves, and the
// false negatives are header fields a caller would plausibly hand a query
// source by mistake. Neither list is exotic.
func TestTheSniffIsWrongBothWays(t *testing.T) {
	t.Parallel()

	// Legitimate query parameters the sniff would refuse. Every one of these is
	// a name an HTTP API actually uses in a query string.
	falsePositives := []string{"Sort-By", "Page-Size", "Utm-Source", "Order-By"}
	for _, name := range falsePositives {
		if sniffCrossed(name) {
			t.Logf("false positive: the sniff refuses the query parameter %q", name)

			continue
		}

		t.Errorf("expected the sniff to refuse %q, so the false-positive list is stale", name)
	}

	// Header fields the sniff lets through on a query source, which is the
	// silent outcome the two-option design existed to prevent.
	falseNegatives := []string{"authorization", "x-request-id", "Accept", "Host", "q"}
	for _, name := range falseNegatives {
		if !sniffCrossed(name) {
			t.Logf("false negative: the sniff passes %q on a query source", name)

			continue
		}

		t.Errorf("expected the sniff to pass %q, so the false-negative list is stale", name)
	}
}

// TestNoSniffExistsForTheHeaderSource is the other half, and it is the harder
// one: the header plane cannot refuse a query-shaped name at all, because every
// query-shaped name is a legal field name.
func TestNoSniffExistsForTheHeaderSource(t *testing.T) {
	t.Parallel()

	// Names a caller would only ever mean as a query parameter.
	for _, name := range []string{"q", "page", "sort", "filter", "limit"} {
		if !fieldName(name) {
			t.Errorf("%q is not a legal field name, which would have given the header plane something to refuse", name)

			continue
		}

		t.Logf("%q is a legal header field name, so the header plane has nothing to refuse it on", name)
	}
}
