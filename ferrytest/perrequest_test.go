package ferrytest_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// The fake per-request driver this file is built on, and the reason it exists.
//
// Case 10 is the obligation of a driver whose plane instance is obtained freshly
// per load (ADR-0012), and no driver in this repository has one, so the case had
// nothing to run against and shipped as an unconditional skip (#185). This is
// that driver, written as test apparatus and reaching no further than these
// tests: a source and a sink that carry no contents at all, a context key of
// their own, and one exported-constructor-shaped function that puts a plane into
// a context.
//
// It is the shape the first real one takes - `NewQuerySource()` beside
// `WithQuery(ctx, r.URL.Query())` - with the query values replaced by the
// memory plane, so that what these tests measure is the suite rather than a
// second implementation of a store.

// planeKey is this fake driver's own context key, unexported and of its own
// type, which is the whole of the mechanism ADR-0012 says core supplies none of.
type planeKey struct{}

// withPlane is the fake driver's exported constructor: it puts one plane
// instance into a context, and it is what [ferrytest.Instance.InContext] hands
// the suite.
func withPlane(ctx context.Context, inst ferrytest.Instance) context.Context {
	return context.WithValue(ctx, planeKey{}, inst)
}

// errNoPlane is the refusal the fake makes at its open, and its class is the one
// ADR-0012 assigns: a plane that was never supplied is the limiting case of a
// plane that cannot be reached.
var errNoPlane = fmt.Errorf("%w: fake: no plane in the context", ferry.ErrPlane)

// errWrongClass is the same refusal with the class left off, which is one of the
// two ways a driver can look right and be wrong here.
var errWrongClass = errors.New("fake: no plane in the context")

// perRequestDefect is the single way a run's fake driver is broken, and its zero
// value is the driver that is correct.
//
// One struct rather than four fixtures because all four are the same driver with
// one line changed, and the point of each is that the suite reports it.
type perRequestDefect struct {
	// atBind refuses at Bind, which is where this refusal may not live: Bind
	// takes no context, so it cannot see whether a plane was supplied.
	atBind error

	// wrongClass is what the open answers instead of a plane refusal.
	wrongClass error

	// opensAnyway answers from an empty plane where there is no plane in the
	// context, which is the failure the case exists to catch: the load then
	// reports every address as missing and nothing says why.
	opensAnyway bool

	// staysRefused caches the open's refusal on the binding, so a plane that
	// arrives later cannot make the same binding open. It is the realistic
	// version of "the refusal is not per load", and it is invisible to every
	// other case, because case 10 is the only one that opens a binding whose
	// previous open failed.
	staysRefused bool
}

// plane is the whole of what a per-request driver does at its open: take the
// plane from the context, or refuse.
func (d perRequestDefect) plane(ctx context.Context) (ferrytest.Instance, error) {
	inst, ok := ctx.Value(planeKey{}).(ferrytest.Instance)

	switch {
	case ok:
		return inst, nil
	case d.opensAnyway:
		return ferrytest.MemPlane().Open(), nil
	case d.wrongClass != nil:
		return ferrytest.Instance{}, d.wrongClass
	default:
		return ferrytest.Instance{}, errNoPlane
	}
}

// perRequestPlane is a plane whose two halves hold nothing and whose contents
// reach them only through the context.
//
// The inner memory instance is minted inside Open, never hoisted out of it, so
// the fresh-destination rule holds exactly as it does for a plane that carries
// its own contents: one instance, one set of contents, reachable through the
// two halves and through the context that supplies them.
func perRequestPlane(d perRequestDefect) ferrytest.Plane {
	mem := ferrytest.MemPlane()

	p := mem
	p.Name = "per-request"
	p.Open = func() ferrytest.Instance {
		inner := mem.Open()

		return ferrytest.Instance{
			Source: perRequestSource{defect: d},
			Sink:   perRequestSink{defect: d},
			InContext: func(ctx context.Context) context.Context {
				return withPlane(ctx, inner)
			},
		}
	}

	return p
}

// perRequestSource is the read half: a value with no contents in it, which is
// what makes it constructible before any request exists.
type perRequestSource struct{ defect perRequestDefect }

// Bind holds the address set and does nothing else, because everything this
// driver needs to reach a plane arrives at the open.
func (s perRequestSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	if s.defect.atBind != nil {
		return nil, s.defect.atBind
	}

	refused := false

	return func(ctx context.Context) (ferry.Reader, error) {
		if s.defect.staysRefused && refused {
			return nil, errNoPlane
		}

		inst, err := s.defect.plane(ctx)
		if err != nil {
			refused = true

			return nil, err
		}

		return bindAndOpenReader(ctx, inst.Source, addrs)
	}, nil
}

// openReader binds the plane the context supplied and opens it, handing back the
// inner reader itself so that whatever optional interfaces it carries survive.
func bindAndOpenReader(ctx context.Context, src ferry.Source, addrs *ferry.AddressSet) (ferry.Reader, error) {
	open, err := src.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return open(ctx)
}

// perRequestSink is the write half, and it reads its plane from the context in
// exactly the way the read half does, which is what ADR-0012 requires of it.
type perRequestSink struct{ defect perRequestDefect }

// Bind holds the address set, for the reason the read half's does.
func (s perRequestSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	if s.defect.atBind != nil {
		return nil, s.defect.atBind
	}

	refused := false

	return func(ctx context.Context) (ferry.Writer, error) {
		if s.defect.staysRefused && refused {
			return nil, errNoPlane
		}

		inst, err := s.defect.plane(ctx)
		if err != nil {
			refused = true

			return nil, err
		}

		return bindAndOpenWriter(ctx, inst.Sink, addrs)
	}, nil
}

// openWriter is [bindAndOpenReader] on the write half.
func bindAndOpenWriter(ctx context.Context, sink ferry.Sink, addrs *ferry.AddressSet) (ferry.Writer, error) {
	open, err := sink.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return open(ctx)
}

// TestDriverIsGreenAgainstAPerRequestPlane is the whole suite against a driver
// that takes its plane from the context, and it asserts two things at once.
//
// That every case runs green says the suite reaches such a plane at all: each
// case opens, dumps and loads under the context the description supplies, so a
// case that had kept [context.Background] would report a plane that is not there
// rather than a driver that is broken.
//
// That case 10 is not among the skips is the case itself running for the first
// time.
func TestDriverIsGreenAgainstAPerRequestPlane(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, perRequestPlane(perRequestDefect{}))

	if len(c.lines) != 0 {
		t.Fatalf("a correct per-request driver reported %q, want nothing", c.lines)
	}

	if anyLineContains(c.logs, "case 10 skipped") {
		t.Errorf("the suite logged %q, and case 10 skipped for a plane that supplies a context is the case "+
			"never running at all", c.logs)
	}
}

// TestDriverCase10ReportsARefusalThatIsNotMade is the case failing, which is the
// only evidence that it works.
//
// Each row is the correct fake with one line changed, and each defect is
// invisible to the other eleven cases: the two that concern an open with no
// plane in the context are reached by no other case, and the third is a binding
// whose previous open failed, which no other case produces.
func TestDriverCase10ReportsARefusalThatIsNotMade(t *testing.T) {
	cases := map[string]struct {
		defect perRequestDefect
		want   string
	}{
		"answers from an empty plane": {
			defect: perRequestDefect{opensAnyway: true},
			want:   "against a context carrying no plane succeeded",
		},
		"refuses with the wrong class": {
			defect: perRequestDefect{wrongClass: errWrongClass},
			want:   "against a context carrying no plane failed with fake: no plane in the context, which is not",
		},
		"never opens again": {
			defect: perRequestDefect{staysRefused: true},
			want:   "with the plane in the context failed with plane error: fake: no plane in the context",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := &capture{}

			ferrytest.Driver(c, perRequestPlane(tc.defect))

			// Both halves are asked and the defect is in the open they share,
			// so it is two reports and both of them are case 10's.
			reportsExactly(t, c,
				"case 10: opening a reader "+tc.want,
				"case 10: opening a writer "+tc.want)
		})
	}
}

// TestDriverCase10ReportsARefusalMadeAtBind is the fourth way, and it is the one
// that cannot be scoped to case 10.
//
// A per-request driver cannot refuse the absence of a plane at Bind, because
// Bind takes no context and cannot see it, so the only Bind that refuses for
// this reason is one that refuses every address set it is ever handed. That
// breaks case 2 by construction and every case that binds afterwards, which is
// the honest report rather than slack in the fixture: what is asserted here is
// that case 10 is among them and names Bind as the place the refusal landed.
func TestDriverCase10ReportsARefusalMadeAtBind(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, perRequestPlane(perRequestDefect{atBind: errNoPlane}))

	for _, want := range []string{
		"case 10: Source.Bind refused a legal address set",
		"case 10: Sink.Bind refused a legal address set",
	} {
		if !anyLineContains(c.lines, want) {
			t.Errorf("the suite reported %q, want %q among them", c.lines, want)
		}
	}
}
