package keyform

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestConformance runs the shipped conformance suites against the query plane
// in each form, so the round-trip claim is the one ferrytest makes and not the
// prototype's own.
func TestConformance(t *testing.T) {
	for _, f := range []Form{Bracket, Flat} {
		t.Run(f.String(), func(t *testing.T) {
			p := ferrytest.Plane{
				Name: "query/" + f.String(),
				Kinds: []ferry.VKind{
					ferry.KindAbsent, ferry.KindBool,
					ferry.KindNumber, ferry.KindString, ferry.KindBytes,
				},
				Open: func() ferrytest.Instance {
					v := url.Values{}
					src, sink := FixedQuery(f, DefaultQuerySeparator, v)

					return ferrytest.Instance{Source: src, Sink: sink}
				},
			}

			// Driver and not RoundTrip(CoreTypes()): a query string has no
			// null (ADR-0004), so []byte(nil), []string(nil) and []string{}
			// are refused, exactly as driver/kv refuses them. Driver expects
			// that refusal; RoundTrip over every core type does not.
			ferrytest.Driver(t, p)
		})
	}
}

// TestHeaderConformance is the same suite over the header plane, where the two
// forms do not agree. The report is captured rather than failed into, because
// the bracket form failing this suite is the finding.
func TestHeaderConformance(t *testing.T) {
	want := map[Form]bool{Bracket: true, Flat: false}

	for _, f := range []Form{Bracket, Flat} {
		t.Run(f.String(), func(t *testing.T) {
			rec := &recorder{t: t}

			ferrytest.Driver(rec, ferrytest.Plane{
				Name: "header/" + f.String(),
				Kinds: []ferry.VKind{
					ferry.KindAbsent, ferry.KindBool,
					ferry.KindNumber, ferry.KindString, ferry.KindBytes,
				},
				Open: func() ferrytest.Instance {
					h := http.Header{}
					src, sink := FixedHeader(f, h)

					return ferrytest.Instance{Source: src, Sink: sink}
				},
			})

			for _, line := range rec.errs {
				t.Logf("  ferrytest.Driver reported: %s", line)
			}

			if got := len(rec.errs) > 0; got != want[f] {
				t.Fatalf("%s: ferrytest.Driver failures = %v, want %v", f, got, want[f])
			}

			t.Logf("header/%s: ferrytest.Driver failures = %d", f, len(rec.errs))
		})
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
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}
