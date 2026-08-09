package env

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// The apparatus every test in this package runs against, and the reason it is
// apparatus rather than shipped code.
//
// A driver test that reads the real process environment is two hazards at once:
// testing.T.Setenv forbids t.Parallel, and a test that mutates the environment
// of the process it runs in is not hermetic against anything else in the same
// binary. So the driver takes its environment from a function ([Environ]) and its
// process writes through a [Process], and every test here supplies both over a
// map it owns.
//
// That is also what puts the composite plane itself under the conformance suite.
// One fake is the process half in both directions: [Environ] reads it and the
// dump's own [Setenv] half writes it, over the same file the sink writes. A
// second run supplies no environment at all, so the plane is the files alone and
// what the suite proves is the format.

// fakeEnviron is one test's process environment: read by the driver through
// [Environ], and written by a dump's process half through [Process].
type fakeEnviron struct{ vars map[string]string }

func newEnviron() *fakeEnviron { return &fakeEnviron{vars: map[string]string{}} }

// Setenv and Unsetenv are the whole of [Process] over the map.
func (e *fakeEnviron) Setenv(name, value string) error { e.vars[name] = value; return nil }
func (e *fakeEnviron) Unsetenv(name string) error      { delete(e.vars, name); return nil }

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

// rootVar is what this plane calls the root address, so that the conformance
// run exercises the root-leaf case rather than skipping it. Without one this
// driver has no name for the root and refuses it, which is the other legitimate
// answer and is the one every schema in the rest of this package gets.
const rootVar = "ROOT"

// planeKinds is what this plane carries end to end, and the one kind missing
// from it is the whole of what it cannot do.
//
// An environment variable is text, so Bool and Number are carried as their
// spellings - PORT=8080 is the most ordinary environment variable there is, and
// a plane that refused it would be describing something other than env.
// ADR-0005 measured a flattening plane with no null at 11 of 11 core types, and
// every value it refused was a nil or empty composite, which the walk writes as
// Null at a container address.
//
// So there is no Null. FOO= is a zero-length string rather than a null
// (ADR-0004), and a value ferry can only express as a Null has no representation
// here at all: the suite holds the plane to refusing those loudly rather than
// mangling them.
//
// The Except is one byte and not a class of them. A byte sequence that is not
// valid UTF-8 is written through raw inside double quotes, so unlike driver/yaml
// this plane needs no exception for those. A NUL is the one value it cannot
// hold, and it is a fact about the plane rather than about the format: the
// environment block is handed to a new process as NUL-terminated strings, so no
// spelling of one in a file could be applied to a process.
var planeKinds = []ferry.VKind{
	ferry.KindAbsent, ferry.KindBool, ferry.KindNumber, ferry.KindString, ferry.KindBytes,
}

// holdsNUL is [ferrytest.Plane.Except]: the values inside a kind this plane
// declares that it cannot spell.
func holdsNUL(v ferry.Value) bool {
	switch v.Kind() {
	case ferry.KindString:
		s, err := v.AsString()

		return err == nil && strings.IndexByte(s, 0) >= 0
	case ferry.KindBytes:
		b, err := v.AsBytes()

		return err == nil && bytes.IndexByte(b, 0) >= 0
	default:
		return false
	}
}

// filePlane is the plane the .env files make on their own: [Environ] answers
// with nothing, so nothing ambient can shadow, invent or collide with what a
// file holds, and what the suite proves is the file format.
//
// It is the run that carries the golden rows, because the file is the artefact
// whose spelling is a compatibility promise (ADR-0013).
func filePlane(t *testing.T, opts ...Naming) ferrytest.Plane {
	t.Helper()

	return ferrytest.Plane{
		Name:   driverName,
		Kinds:  planeKinds,
		Except: holdsNUL,
		Golden: goldenRows(),
		Open: func() ferrytest.Instance {
			path := filepath.Join(t.TempDir(), ".env")

			return ferrytest.Instance{
				Source:   New(sourceWith(opts, Environ(noEnviron), DotEnv(path))...),
				Sink:     NewDotEnvSink(path, sinkWith(opts)...),
				Contents: func() ([]byte, error) { return os.ReadFile(path) },
			}
		},
	}
}

// compositePlane is the whole plane: one file under one process environment,
// with the dump writing both halves.
//
// It is the run the composite itself is under, and it is what makes the process
// half of a dump a proof rather than a claim: the same fake is what [Environ]
// reads and what [Setenv] writes, so a save that leaves the two disagreeing
// fails the round trip.
func compositePlane(t *testing.T, opts ...Naming) ferrytest.Plane {
	t.Helper()

	return ferrytest.Plane{
		Name:   driverName,
		Kinds:  planeKinds,
		Except: holdsNUL,
		Open: func() ferrytest.Instance {
			path := filepath.Join(t.TempDir(), ".env")
			e := newEnviron()

			return ferrytest.Instance{
				Source:   New(sourceWith(opts, Environ(e.environ), DotEnv(path))...),
				Sink:     NewDotEnvSink(path, sinkWith(opts, Setenv(e))...),
				Contents: func() ([]byte, error) { return os.ReadFile(path) },
			}
		},
	}
}

// noEnviron is the empty process environment, and it is the escape hatch this
// package's own documentation names: whatever it returns is the top layer, so
// returning nothing is how a load reads the files alone.
func noEnviron() []string { return nil }

// sourceWith and sinkWith are the one list of [Naming] settings a plane is built
// from, widened to each constructor's own option type.
//
// Building both halves from one list is what the shipped godoc tells a caller to
// do, and it is why these tests cannot drift into a source and a sink that fold
// names differently.
func sourceWith(shared []Naming, also ...Option) []Option {
	out := make([]Option, 0, len(shared)+len(also)+1)
	out = append(out, RootVar(rootVar))

	for _, o := range shared {
		out = append(out, o)
	}

	return append(out, also...)
}

func sinkWith(shared []Naming, also ...SinkOption) []SinkOption {
	out := make([]SinkOption, 0, len(shared)+len(also)+1)
	out = append(out, RootVar(rootVar))

	for _, o := range shared {
		out = append(out, o)
	}

	return append(out, also...)
}

// The apparatus for asking this driver a question directly.
//
// The three address kinds are sealed and only the schema compiler mints them
// (ADR-0016), so a test that wants to hand this driver an address asks the
// compiler for one rather than writing it. That is the same door core comes in
// through, which is what keeps these tests asserting about the driver rather
// than about a set a test invented.

// captureSource records the address set core hands a Bind and reads nothing.
type captureSource struct{ set *ferry.AddressSet }

func (c *captureSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	c.set = addrs

	return func(context.Context) (ferry.Reader, error) { return captureReader{}, nil }, nil
}

type captureReader struct{}

func (captureReader) Get(context.Context, ferry.LeafAddr) (ferry.Value, error) {
	return ferry.Value{}, nil
}

// addrsOf is every address T names, typed, straight from the compiler.
func addrsOf[T any](opts ...ferry.Option) (*ferry.AddressSet, error) {
	c := &captureSource{}
	if _, err := ferry.Bind[T](c, opts...); err != nil {
		return nil, err
	}

	return c.set, nil
}

// leafIn, sectionIn and compositeIn find one address of one kind in a compiled
// set. A miss is a test naming an address its own fixture does not have, so the
// caller fails on it rather than reading a zero address.
func leafIn(set *ferry.AddressSet, at ferry.Path) (ferry.LeafAddr, bool) {
	for m := range set.Seq() {
		if a, ok := m.(ferry.LeafAddr); ok && a.Path() == at {
			return a, true
		}
	}

	return ferry.LeafAddr{}, false
}

func sectionIn(set *ferry.AddressSet, at ferry.Path) (ferry.SectionAddr, bool) {
	for m := range set.Seq() {
		if a, ok := m.(ferry.SectionAddr); ok && a.Path() == at {
			return a, true
		}
	}

	return ferry.SectionAddr{}, false
}

func compositeIn(set *ferry.AddressSet, at ferry.Path) (ferry.CompositeAddr, bool) {
	for m := range set.Seq() {
		if a, ok := m.(ferry.CompositeAddr); ok && a.Path() == at {
			return a, true
		}
	}

	return ferry.CompositeAddr{}, false
}

// rig is one source bound to the addresses a fixture type names and opened over
// one environment, which is what the tests that assert on Get, Probe and
// Children need and what neither ferry.Load nor ferrytest reaches directly.
type rig struct {
	set *ferry.AddressSet
	r   ferry.Reader
}

// boundTo compiles T, binds this driver to the addresses T names, and opens a
// reader over e.
func boundTo[T any](e *fakeEnviron, opts ...Option) (rig, error) {
	set, err := addrsOf[T]()
	if err != nil {
		return rig{}, err
	}

	src := New(append([]Option{Environ(e.environ)}, opts...)...)

	open, err := src.Bind(set)
	if err != nil {
		return rig{}, err
	}

	r, err := open(context.Background())
	if err != nil {
		return rig{}, err
	}

	return rig{set: set, r: r}, nil
}

// leaf, section and composite are the typed addresses a case asks about, looked
// up in the set the fixture compiled to.
func (g rig) leaf(t testingT, at ferry.Path) ferry.LeafAddr {
	t.Helper()

	a, ok := leafIn(g.set, at)
	if !ok {
		t.Fatalf("the fixture names no leaf at %s", at)
	}

	return a
}

func (g rig) section(t testingT, at ferry.Path) ferry.SectionAddr {
	t.Helper()

	a, ok := sectionIn(g.set, at)
	if !ok {
		t.Fatalf("the fixture names no section at %s", at)
	}

	return a
}

func (g rig) composite(t testingT, at ferry.Path) ferry.CompositeAddr {
	t.Helper()

	a, ok := compositeIn(g.set, at)
	if !ok {
		t.Fatalf("the fixture names no composite at %s", at)
	}

	return a
}

// testingT is the half of *testing.T these helpers need, named so that they take
// a *testing.T without the package importing anything a driver would not.
type testingT interface {
	Helper()
	Fatalf(format string, args ...any)
}
