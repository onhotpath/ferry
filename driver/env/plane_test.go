package env

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// The apparatus every test in this package runs against, and the reason it is
// apparatus rather than shipped code.
//
// A driver test that reads the real process environment is two hazards at once:
// testing.T.Setenv forbids t.Parallel, and a test that mutates the environment
// of the process it runs in is not hermetic against anything else in the same
// binary. So the driver takes its environment from a function ([Environ]) and
// every test here supplies one over a map it owns.
//
// The write half is the second half of the same idea and it is a harder call, so
// it is stated rather than assumed. This package ships no [ferry.Sink] and never
// will, which is the whole point of the plane - and it means that the round-trip
// half of ferrytest.RoundTrip never executes, because ferrytest.Driver guards
// five of its cases on a nil sink. The guarantee [Canonical] exists to provide
// would then be unproven rather than proven. So the tests supply a stand-in sink
// over the same fake environment, exactly as ferrytest's own memory plane does,
// which exercises the key function against its own inverse as a composed pair.
// It lives in a _test.go file and is reachable from nowhere else: a sink in this
// package's exported surface would be the thing the plane exists not to have.

// fakeEnviron is one test's environment, written by the stand-in sink and read
// by the driver.
type fakeEnviron struct{ vars map[string]string }

func newEnviron() *fakeEnviron { return &fakeEnviron{vars: map[string]string{}} }

// environ renders the map the way os.Environ does, sorted so that a test
// asserting on a load is not asserting on Go's map iteration order.
func (e *fakeEnviron) environ() []string {
	out := make([]string, 0, len(e.vars))
	for name, value := range e.vars {
		out = append(out, name+"="+value)
	}

	slices.Sort(out)

	return out
}

// plane describes the env plane for the conformance suite, with both halves over
// one environment.
//
// Kinds is a declaration and not a wish, and the one kind missing from it is
// the whole of what this plane cannot do. An environment variable is text, so
// Bool and Number are carried as their spellings - PORT=8080 is the most
// ordinary environment variable there is, and a plane that refused it would be
// describing something other than env. ADR-0005 measured a flattening plane
// with no null at 11 of 11 core types, and every value it refused was a nil or
// empty composite, which the walk writes as Null at a container address.
//
// So there is no Null. FOO= is a zero-length string rather than a null
// (ADR-0004), and a value ferry can only express as a Null has no
// representation here at all: the suite holds the plane to refusing those
// loudly rather than mangling them.
//
// There is no Golden and no Contents. A golden artefact pins a driver's own
// spelling of a value, and this driver has none: it never writes, so the only
// spelling a row could pin is the stand-in sink's, which is a test's and not a
// compatibility promise (ADR-0013).
func plane(opts ...Option) ferrytest.Plane {
	return ferrytest.Plane{
		Name: driverName,
		Kinds: []ferry.VKind{
			ferry.KindAbsent, ferry.KindBool, ferry.KindNumber, ferry.KindString, ferry.KindBytes,
		},
		Open: func() ferrytest.Instance {
			e := newEnviron()
			src := New(append([]Option{Environ(e.environ)}, opts...)...)

			return ferrytest.Instance{Source: src, Sink: standInSink{cfg: src.cfg, env: e}}
		},
	}
}

// standInSink is the write half of the fake environment, built on the driver's
// own key function so that what a round trip composes is this driver's fold
// against this driver's enumeration and not a test's idea of either.
type standInSink struct {
	cfg config
	env *fakeEnviron
}

// Bind checks the same two things the source's does, through the same helper, so
// a schema the plane refuses is refused in both directions.
func (s standInSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	if err := s.cfg.validate(); err != nil {
		return nil, err
	}

	keys, err := ferry.NewKeys(addrs, driverName, s.cfg.key)
	if err != nil {
		return nil, err
	}

	env := s.env

	return func(context.Context) (ferry.Writer, error) {
		return standInWriter{keys: keys.Open(), env: env}, nil
	}, nil
}

// standInWriter is one open write side. It implements neither ferry.Committer
// nor ferry.Releaser, because a map stages nothing and holds nothing.
type standInWriter struct {
	keys ferry.KeyFunc
	env  *fakeEnviron
}

// Set writes one address, or refuses a value this plane cannot hold.
//
// The refusal is per address and loud, which is what a plane with no null owes:
// an environment variable is text, so a Null has no representation here, and
// writing one as an empty string would make an empty composite and a composite
// of one empty element the same bytes.
func (w standInWriter) Set(_ context.Context, addr ferry.Path, v ferry.Value) error {
	text, err := carried(v)
	if err != nil {
		return ferry.ErrorAt(addr, err)
	}

	key, err := w.keys(addr)
	if err != nil {
		return err
	}

	w.env.vars[key] = text

	return nil
}

// carried is the plane's kind declaration as a function: the text an
// environment variable would hold, or a refusal naming the kind.
//
// Bool and Number are text here, which is what makes PORT=8080 an ordinary
// environment variable rather than a value this plane refuses. ADR-0005
// measured a flattening plane with no null at 11 of 11 core types, with the
// only refusals being the nil and empty composites the walk writes as Null at
// a container address - so the kinds this plane cannot carry number exactly
// one, and it is Null.
func carried(v ferry.Value) (string, error) {
	switch v.Kind() {
	case ferry.KindBool:
		b, err := v.AsBool()

		return strconv.FormatBool(b), err
	case ferry.KindNumber:
		return v.AsNumber()
	case ferry.KindString:
		return v.AsString()
	case ferry.KindBytes:
		b, err := v.AsBytes()

		return string(b), err
	default:
		return "", fmt.Errorf("%w: an environment variable holds text, and this plane cannot carry a %s",
			ferry.ErrPlane, v.Kind())
	}
}

// bound is one source bound to one address set and opened, which is what the
// tests that assert on Get and Children need and what neither ferry.Load nor
// ferrytest reaches directly.
func bound(e *fakeEnviron, addrs []ferry.Path, opts ...Option) (ferry.Reader, error) {
	src := New(append([]Option{Environ(e.environ)}, opts...)...)

	open, err := src.Bind(ferry.NewAddressSet(addrs...))
	if err != nil {
		return nil, err
	}

	return open(context.Background())
}
