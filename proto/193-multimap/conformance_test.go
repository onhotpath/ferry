package multimap

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// recorder captures what a suite reports instead of failing the run, which
// ferrytest.T exists to allow. A shape failing the suite is the finding.
type recorder struct {
	t    *testing.T
	errs []string
}

func (r *recorder) Helper() { r.t.Helper() }

func (r *recorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, oneLine(fmt.Sprintf(format, args...)))
}

// kinds is what both planes carry end to end: everything a flat plane carries,
// and no null. driver/env and driver/kv declare the same list.
func kinds() []ferry.VKind {
	return []ferry.VKind{
		ferry.KindAbsent, ferry.KindBool,
		ferry.KindNumber, ferry.KindString, ferry.KindBytes,
	}
}

// TestConformance runs the shipped ferrytest.Driver, unmodified, against every
// shape on both planes, and reports the failure count per shape.
func TestConformance(t *testing.T) {
	for _, s := range Shapes() {
		q := &recorder{t: t}
		ferrytest.Driver(q, ferrytest.Plane{
			Name:  "query/" + s.String(),
			Kinds: kinds(),
			Open: func() ferrytest.Instance {
				v := url.Values{}
				src, sink := Fixed(QueryPlane(s, declaredFor(s)...), v)

				return ferrytest.Instance{Source: src, Sink: sink}
			},
		})

		h := &recorder{t: t}
		ferrytest.Driver(h, ferrytest.Plane{
			Name:  "header/" + s.String(),
			Kinds: kinds(),
			Open: func() ferrytest.Instance {
				hdr := http.Header{}
				src, sink := Fixed(HeaderPlane(s, declaredFor(s)...), hdr)

				return ferrytest.Instance{Source: src, Sink: sink}
			},
		})

		t.Logf("%-18s ferrytest.Driver failures: query %d, header %d", s, len(q.errs), len(h.errs))

		for _, line := range q.errs {
			t.Logf("      query : %s", line)
		}

		for _, line := range h.errs {
			t.Logf("      header: %s", line)
		}
	}
}

// TestRoundTripDynamic is the proof driver/env calls the same thing: every case
// is a composite whose members come from the value, so each is a dump that mints
// addresses the static table never held and a load that has to recover them.
//
// ferrytest.Driver guards five of its cases on a nil sink, so the round-trip
// half of the proofs it runs never reaches a dynamic address; this is where the
// one-element hole shows up in a round trip.
func TestRoundTripDynamic(t *testing.T) {
	for _, s := range Shapes() {
		rec := &recorder{t: t}

		ferrytest.RoundTrip(rec, ferrytest.Plane{
			Name:  "query/" + s.String(),
			Kinds: kinds(),
			Open: func() ferrytest.Instance {
				v := url.Values{}
				src, sink := Fixed(QueryPlane(s, declaredFor(s)...), v)

				return ferrytest.Instance{Source: src, Sink: sink}
			},
		}, []ferrytest.Proof{
			ferrytest.Type("[]string", ferrytest.SliceEq(ferrytest.Eq[string]),
				ferrytest.At([]string{"a", "b", "c"}, ferry.Value{}),
				ferrytest.At([]string{"a"}, ferry.Value{}),
			),
			ferrytest.Type("map[string]string", ferrytest.MapEq[string](ferrytest.Eq[string]),
				ferrytest.At(map[string]string{"cpu": "1", "mem": "2"}, ferry.Value{}),
			),
		})

		t.Logf("%-18s ferrytest.RoundTrip (dynamic) failures: %d", s, len(rec.errs))

		for _, line := range rec.errs {
			t.Logf("      %s", line)
		}
	}
}

// TestRoundTripByHand shows the plane contents in between, which is what a round
// trip through a suite hides and what decides whether a shape is invertible.
func TestRoundTripByHand(t *testing.T) {
	for _, s := range Shapes() {
		for _, want := range []Tagged{
			{Tags: []string{"a", "b"}},
			{Tags: []string{"a"}},
		} {
			v := url.Values{}
			ctx := WithQuery(context.Background(), v)

			sink := NewQuerySink(s, declaredFor(s)...)
			if err := ferry.Dump(ctx, want, sink); err != nil {
				t.Logf("%-18s %v -> DUMP %s", s, want.Tags, oneLine(err.Error()))

				continue
			}

			src := NewQuerySource(s, declaredFor(s)...)
			got, err := ferry.Load[Tagged](ctx, src)

			t.Logf("%-18s %-10v -> %-28q -> %s", s, want.Tags, v.Encode(), outcome(got, err))
		}
	}
}

// TestHeaderMapInvertibility is the header plane's Canonical question: net/http
// destroys a map key's own spelling, so the driver has to choose one to hand
// back, and two Go keys that differ only in case are then one plane key.
func TestHeaderMapInvertibility(t *testing.T) {
	for _, want := range []map[string]int{
		{"cpu": 1, "mem": 2},
		{"CPU": 1},
		{"cpu": 1, "CPU": 2},
	} {
		h := http.Header{}
		ctx := WithHeaders(context.Background(), h)

		if err := ferry.Dump(ctx, Mapped{Limits: want}, NewHeaderSink(Cardinality)); err != nil {
			t.Logf("%-22v -> DUMP %s", want, oneLine(err.Error()))

			continue
		}

		got, err := ferry.Load[Mapped](ctx, NewHeaderSource(Cardinality))
		t.Logf("%-22v -> %-34s -> %s", want, FmtValues(h), outcome(got, err))
	}
}
