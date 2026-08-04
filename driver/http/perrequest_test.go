package ferryhttp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// Conformance case 10 is this driver's one distinctive obligation, and it had
// never run against a real driver before this package existed: no driver in the
// repository took its plane per load, so the case shipped as a skip and then, as
// of #185, as a skip conditional on a plane that supplies a context.
//
// A case that passes proves nothing on its own, so this file does two things.
// TestCase10Runs asserts it is not among the skips for either plane this package
// ships. The rest inject, one at a time, the four ways a per-request driver can
// look right and be wrong, and assert that the case reports each of them - which
// is the only evidence that the green run above means anything.

// TestCase10Runs is the case running for the first time.
func TestCase10Runs(t *testing.T) {
	t.Parallel()

	for name, p := range shippedPlanes() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assertCase10Ran(t, p)
		})
	}
}

func assertCase10Ran(t *testing.T, p ferrytest.Plane) {
	t.Helper()

	c := &capture{}
	ferrytest.Driver(c, p)

	if len(c.lines) != 0 {
		t.Fatalf("the plane reported %q, want nothing", c.lines)
	}

	if anyLineContains(c.logs, "case 10 skipped") {
		t.Errorf("the suite logged %q, and case 10 skipped for a plane that supplies a context is the case "+
			"never running at all", c.logs)
	}
}

func shippedPlanes() map[string]ferrytest.Plane {
	return map[string]ferrytest.Plane{"query": queryPlaneFor(), "header": headerPlaneFor()}
}

// TestCase10ReportsARefusalThatIsNotMade is the case failing, three ways.
//
// Each row is this package's own query source with one thing changed at the
// open, and each defect is invisible to the other eleven cases: the two that
// concern an open with no request in the context are reached by no other case,
// and the third is a binding whose previous open failed, which no other case
// produces.
func TestCase10ReportsARefusalThatIsNotMade(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		defect defect
		want   string
	}{
		"answers from an empty request": {
			defect: defect{opensAnyway: true},
			want:   "against a context carrying no plane succeeded",
		},
		"refuses with the wrong class": {
			defect: defect{wrongClass: true},
			want:   "against a context carrying no plane failed with " + ErrNoQuery.Error() + ", which is not",
		},
		"never opens again": {
			defect: defect{staysRefused: true},
			want:   "with the plane in the context failed with plane error: " + ErrNoQuery.Error(),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := &capture{}
			ferrytest.Driver(c, mutatedPlane(tc.defect))

			// Both halves are asked and the defect is in the open they share,
			// so it is two reports and both of them are case 10's.
			reportsExactly(t, c,
				"case 10: opening a reader "+tc.want,
				"case 10: opening a writer "+tc.want)
		})
	}
}

// TestCase10ReportsARefusalMadeAtBind is the fourth way, and the one that cannot
// be scoped to case 10.
//
// A per-request driver cannot refuse the absence of a request at Bind, because
// Bind takes no context and cannot see it, so the only Bind that refuses for
// this reason is one that refuses every address set it is ever handed. That
// breaks case 2 by construction and every case that binds afterwards, which is
// the honest report rather than slack in the fixture: what is asserted here is
// that case 10 is among them and names Bind as the place the refusal landed.
func TestCase10ReportsARefusalMadeAtBind(t *testing.T) {
	t.Parallel()

	c := &capture{}
	ferrytest.Driver(c, mutatedPlane(defect{atBind: true}))

	for _, want := range []string{
		"case 10: Source.Bind refused a legal address set",
		"case 10: Sink.Bind refused a legal address set",
	} {
		if !anyLineContains(c.lines, want) {
			t.Errorf("the suite reported %q, want %q among them", c.lines, want)
		}
	}
}

// defect is the single way a run's driver is broken, and its zero value is the
// driver this package ships.
//
// One struct rather than four fixtures because all four are the shipped driver
// with one line changed, and the point of each is that the suite reports it.
type defect struct {
	// atBind refuses at Bind, which is where this refusal may not live.
	atBind bool

	// wrongClass answers the absence with an error that does not wrap
	// ferry.ErrPlane, so nothing downstream can classify it.
	wrongClass bool

	// opensAnyway answers from an empty request where the context carries none,
	// which is the failure the case exists to catch: the load then reports every
	// field missing and nothing says why.
	opensAnyway bool

	// staysRefused caches the refusal on the binding, so a request arriving
	// later cannot make the same binding open. It is the realistic version of
	// "the refusal is not per load", and the one a shared binding makes fatal.
	staysRefused bool
}

// mutatedPlane is the shipped query plane with one defect injected into both
// halves, which is where a per-request driver's obligation lives.
func mutatedPlane(d defect) ferrytest.Plane {
	p := queryPlaneFor()

	p.Open = func() ferrytest.Instance {
		v := url.Values{}
		src := NewQuerySource()

		return ferrytest.Instance{
			Source:    mutatedSource{inner: src, defect: d},
			Sink:      mutatedSink{inner: standInSink{src: src}, defect: d},
			InContext: func(ctx context.Context) context.Context { return WithQuery(ctx, v) },
		}
	}

	return p
}

type mutatedSource struct {
	inner  *Source
	defect defect
}

func (s mutatedSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	if s.defect.atBind {
		return nil, absentPlane(ErrNoQuery)
	}

	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return mutate(s.defect, open), nil
}

type mutatedSink struct {
	inner  standInSink
	defect defect
}

func (s mutatedSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	if s.defect.atBind {
		return nil, absentPlane(ErrNoQuery)
	}

	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return mutate(s.defect, open), nil
}

// mutate is the whole of the injection, and it is the same on both halves
// because the obligation is: take the request from the context per open, refuse
// its absence in the class ferry can read, and never remember having refused.
func mutate[T any](d defect, open func(ctx context.Context) (T, error)) func(context.Context) (T, error) {
	refused := false

	return func(ctx context.Context) (T, error) {
		var zero T

		if d.staysRefused && refused {
			return zero, absentPlane(ErrNoQuery)
		}

		out, err := open(ctx)
		if err == nil {
			return out, nil
		}

		refused = true

		switch {
		case d.opensAnyway:
			return open(WithQuery(ctx, url.Values{}))
		case d.wrongClass:
			return zero, errors.New(ErrNoQuery.Error())
		default:
			return zero, err
		}
	}
}

// capture is a ferrytest.T that keeps what a suite reported instead of failing
// the test that ran it, which is what lets a test assert on a suite's own
// output.
type capture struct {
	lines []string
	logs  []string
}

func (c *capture) Errorf(format string, args ...any) {
	c.lines = append(c.lines, fmt.Sprintf(format, args...))
}

// Logf is not on ferrytest.T and is implemented anyway, because a suite writes
// an explicit skip where the reporter can carry one and a skip is exactly what
// TestCase10Runs has to distinguish from a pass.
func (c *capture) Logf(format string, args ...any) {
	c.logs = append(c.logs, fmt.Sprintf(format, args...))
}

func (*capture) Helper() {}

var _ ferrytest.T = (*capture)(nil)

func anyLineContains(lines []string, want string) bool {
	for _, line := range lines {
		if strings.Contains(line, want) {
			return true
		}
	}

	return false
}

// reportsExactly asserts that a suite reported these failures and no others, so
// a defect that also broke something else is not quietly counted as this case
// working.
func reportsExactly(t *testing.T, c *capture, want ...string) {
	t.Helper()

	if len(c.lines) != len(want) {
		t.Fatalf("the suite reported %d failures %q, want %d", len(c.lines), c.lines, len(want))
	}

	for _, one := range want {
		if !anyLineContains(c.lines, one) {
			t.Errorf("the suite reported %q, want %q among them", c.lines, one)
		}
	}
}
