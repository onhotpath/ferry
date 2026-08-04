package httpdecisions

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// Question 1: does driver/http ship a Sink?
//
// The brief says no. #193 ran ferrytest.RoundTrip, which needs one. What the
// tests below establish is what the sink was actually for, and what a driver
// that exports none loses.

// mutant is a read-side defect injected into an otherwise conforming driver, so
// that "the suite would have caught it" is a measurement rather than a claim.
type mutant int

const (
	mutantNone mutant = iota
	// mutantOneElementHole is #193's `cardinality` shape: Children mints
	// positions only where the name holds more than one value, so a
	// one-element sequence reads as a scalar at a container address.
	mutantOneElementHole
	// mutantFirstWins is the silent loss every other Go library ships: a
	// repeated name at a scalar address hands back the first value.
	mutantFirstWins
	// mutantNoMintedChildren is a driver whose Children only ever answers what
	// the static table already held, so a map key the value minted is lost.
	mutantNoMintedChildren
	// mutantDropsLast is a driver whose Children forgets the last position.
	mutantDropsLast
)

func (m mutant) String() string {
	switch m {
	case mutantOneElementHole:
		return "Children skips a one-element sequence"
	case mutantFirstWins:
		return "Get hands back the first of a repeated name"
	case mutantNoMintedChildren:
		return "Children answers only static names"
	case mutantDropsLast:
		return "Children drops the last position"
	default:
		return "none"
	}
}

func mutants() []mutant {
	return []mutant{mutantNone, mutantOneElementHole, mutantFirstWins, mutantNoMintedChildren, mutantDropsLast}
}

// mutantSource wraps a conforming Source and injects one defect.
type mutantSource struct {
	inner ferry.Source
	m     mutant
	stat  map[string]bool
}

func (s *mutantSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	static := map[string]bool{}

	if addrs != nil {
		for a := range addrs.All() {
			static[a.String()] = true
		}
	}

	s.stat = static

	return func(ctx context.Context) (ferry.Reader, error) {
		r, rerr := open(ctx)
		if rerr != nil {
			return nil, rerr
		}

		return &mutantReader{inner: r, m: s.m, static: static}, nil
	}, nil
}

type mutantReader struct {
	inner  ferry.Reader
	m      mutant
	static map[string]bool
}

var (
	_ ferry.Reader     = (*mutantReader)(nil)
	_ ferry.Enumerator = (*mutantReader)(nil)
)

func (r *mutantReader) Get(ctx context.Context, addr ferry.Path) (ferry.Value, error) {
	v, err := r.inner.Get(ctx, addr)
	if r.m != mutantFirstWins || err != nil || v.Kind() != ferry.KindAbsent {
		return v, err
	}

	// The defect: where the honest reader hid a repeated name behind Absent,
	// hand back the first value instead.
	kids, kerr := r.Children(ctx, addr)
	if kerr != nil || len(kids) == 0 {
		return v, err
	}

	return r.inner.Get(ctx, kids[0])
}

func (r *mutantReader) Children(ctx context.Context, prefix ferry.Path) ([]ferry.Path, error) {
	kids, err := r.inner.(ferry.Enumerator).Children(ctx, prefix)
	if err != nil {
		return nil, err
	}

	switch r.m {
	case mutantOneElementHole:
		if len(kids) == 1 && kids[0].String() != "" && isPosition(kids[0]) {
			return nil, nil
		}
	case mutantNoMintedChildren:
		out := kids[:0:0]

		for _, k := range kids {
			if r.static[k.String()] {
				out = append(out, k)
			}
		}

		return out, nil
	case mutantDropsLast:
		if len(kids) > 0 {
			return kids[:len(kids)-1], nil
		}
	case mutantNone, mutantFirstWins:
	}

	return kids, nil
}

func (r *mutantReader) Close() error {
	if c, ok := r.inner.(ferry.Releaser); ok {
		return c.Close()
	}

	return nil
}

func isPosition(p ferry.Path) bool {
	_, _, ok := splitIndex(p)

	return ok
}

// TestQ1ConformanceWithAndWithoutASink is the measurement the sink question
// turns on: what ferrytest.Driver catches when the plane mints a sink, and what
// it catches when it does not.
func TestQ1ConformanceWithAndWithoutASink(t *testing.T) {
	for _, m := range mutants() {
		with := runDriver(t, m, true)
		without, noSink := split(runDriver(t, m, false))

		t.Logf("%-42s  with sink: %d   no sink: %d (+%d nil-sink reports)",
			m, len(with), len(without), noSink)

		for _, l := range with {
			t.Logf("      with   : %s", l)
		}

		for _, l := range without {
			t.Logf("      without: %s", l)
		}
	}
}

// split separates a suite's real findings from its complaint that there is no
// sink to dump through, which is one report per proof and not one finding.
func split(errs []string) ([]string, int) {
	out, n := []string{}, 0

	for _, e := range errs {
		if strings.Contains(e, "mints no sink") {
			n++

			continue
		}

		out = append(out, e)
	}

	return out, n
}

func runDriver(t *testing.T, m mutant, sink bool) []string {
	t.Helper()

	rec := &recorder{t: t}

	ferrytest.Driver(rec, ferrytest.Plane{
		Name:  "query",
		Kinds: kinds(),
		Open: func() ferrytest.Instance {
			v := url.Values{}
			src, snk := Fixed(QueryPlane(Enumerated), v)
			inst := ferrytest.Instance{Source: &mutantSource{inner: src, m: m}}

			if sink {
				inst.Sink = snk
			}

			return inst
		},
	})

	return rec.errs
}

// TestQ1RoundTripWithAndWithoutASink is the same question for the suite that
// reaches a dynamic address, which is the one #193 leaned on.
func TestQ1RoundTripWithAndWithoutASink(t *testing.T) {
	for _, m := range mutants() {
		with := runRoundTrip(t, m, true)
		without, noSink := split(runRoundTrip(t, m, false))

		t.Logf("%-42s  with sink: %d   no sink: %d (+%d nil-sink reports)",
			m, len(with), len(without), noSink)

		for _, l := range with {
			t.Logf("      with   : %s", l)
		}
	}
}

func runRoundTrip(t *testing.T, m mutant, sink bool) []string {
	t.Helper()

	rec := &recorder{t: t}

	ferrytest.RoundTrip(rec, ferrytest.Plane{
		Name:  "query",
		Kinds: kinds(),
		Open: func() ferrytest.Instance {
			v := url.Values{}
			src, snk := Fixed(QueryPlane(Enumerated), v)
			inst := ferrytest.Instance{Source: &mutantSource{inner: src, m: m}}

			if sink {
				inst.Sink = snk
			}

			return inst
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

	return rec.errs
}

// TestQ1SinkSpelling is the sub-question: which spelling a sink emits for a
// []string, and whether each round trips against the reader that ships.
func TestQ1SinkSpelling(t *testing.T) {
	for _, sp := range []SinkSpelling{SinkRepeated, SinkIndexed} {
		for _, want := range [][]string{{"a", "b"}, {"a"}, {}} {
			v := url.Values{}
			ctx := WithQuery(context.Background(), v)

			if err := ferry.Dump(ctx, Tagged{Tags: want}, NewQuerySink(Enumerated, WithSpelling(sp))); err != nil {
				t.Logf("%-14s %-10v -> DUMP %s", sp, want, oneLine(err.Error()))

				continue
			}

			got, err := ferry.Load[Tagged](ctx, NewQuerySource(Enumerated))

			t.Logf("%-14s %-10v -> %-24q -> %s", sp, want, v.Encode(), outcome(got, err))
		}
	}
}

// TestQ1InjectivityBypass prices the collapse the write side makes outside
// ferry.NewKeys: /tags#0 and /tags#1 both write to the plane key "tags", which
// NewKeys never saw, because it was shown "tags.0" and "tags.1".
func TestQ1InjectivityBypass(t *testing.T) {
	for _, sp := range []SinkSpelling{SinkRepeated, SinkIndexed} {
		v := url.Values{}
		ctx := WithQuery(context.Background(), v)
		want := Collide{Tags: []string{"a", "b"}, Zero: "z"}

		err := ferry.Dump(ctx, want, NewQuerySink(Enumerated, WithSpelling(sp)))
		if err != nil {
			t.Logf("%-14s DUMP %+v -> %s", sp, want, oneLine(err.Error()))

			continue
		}

		got, lerr := ferry.Load[Collide](ctx, NewQuerySource(Enumerated))

		t.Logf("%-14s DUMP %+v -> plane %s -> load %s", sp, want, FmtValues(v), outcome(got, lerr))
	}
}

// TestQ1DeepInjectivityBypass is the collapse ferry.NewKeys genuinely cannot
// see, because the plane key the repeated sink collapses onto is itself minted.
//
// /m/k#0 and /m/k#1 render to "m.k.0" and "m.k.1", which is what the mint-time
// check is shown, and the writer then puts both under "m.k" - which is where
// the static address /m.k already writes.
func TestQ1DeepInjectivityBypass(t *testing.T) {
	for _, sp := range []SinkSpelling{SinkRepeated, SinkIndexed} {
		v := url.Values{}
		ctx := WithQuery(context.Background(), v)
		want := DeepCollide{M: map[string][]string{"k": {"v1", "v2"}}, X: "z"}

		err := ferry.Dump(ctx, want, NewQuerySink(Enumerated, WithSpelling(sp)))
		if err != nil {
			t.Logf("%-14s DUMP -> %s", sp, oneLine(err.Error()))

			continue
		}

		got, lerr := ferry.Load[DeepCollide](ctx, NewQuerySource(Enumerated))

		t.Logf("%-14s DUMP %+v", sp, want)
		t.Logf("%-14s   plane %s", sp, FmtValues(v))
		t.Logf("%-14s   load  %s", sp, outcome(got, lerr))
	}
}

// TestQ1SetSemantics is the decision a sink cannot avoid: what Set does at a key
// the plane already holds values at. An outbound request is the case, because
// the caller built the url.Values before ferry saw it.
func TestQ1SetSemantics(t *testing.T) {
	for _, sem := range SetSemanticsAll() {
		for _, before := range []url.Values{
			{"tags": {"x", "y", "z"}},
			{"tags": {"x"}},
			{"q": {"old"}},
		} {
			v := url.Values{}
			for k, vs := range before {
				v[k] = append([]string(nil), vs...)
			}

			ctx := WithQuery(context.Background(), v)
			want := Both{Tags: []string{"a", "b"}, Q: "new"}

			err := ferry.Dump(ctx, want, NewQuerySink(Enumerated, WithSetSemantics(sem)))
			if err != nil {
				t.Logf("%-12s plane %-28s -> DUMP %s", sem, FmtValues(before), oneLine(err.Error()))

				continue
			}

			t.Logf("%-12s plane %-28s -> %q", sem, FmtValues(before), v.Encode())
		}
	}
}
