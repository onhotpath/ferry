package ferrytest_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// The proof RoundTrip is asked to run almost everywhere below.
//
// string is the only type the engine carries today - isLeafType admits
// reflect.String and nothing else - so it is the type a hand-written proof can
// be written for, and the wider set is #72's and #73's. Four cases rather than
// one, because the memory plane refuses a second write at one address: if the
// harness reused a destination across cases, cases 2, 3 and 4 would all fail
// their dump, so a green run over this proof is also the fresh-destination rule
// being asserted.
func stringProof() []ferrytest.Proof {
	return []ferrytest.Proof{
		ferrytest.Type("string", ferrytest.Eq[string],
			ferrytest.At("", ferry.String("")),
			ferrytest.At("localhost", ferry.String("localhost")),
			ferrytest.At("a b\tc", ferry.String("a b\tc")),
			ferrytest.At("ünïcødé", ferry.String("ünïcødé")),
		),
	}
}

// oneStringProof is the same proof cut to a single case, for the tests that
// count reports rather than values.
func oneStringProof() []ferrytest.Proof {
	return []ferrytest.Proof{
		ferrytest.Type("string", ferrytest.Eq[string], ferrytest.At("localhost", ferry.String("localhost"))),
	}
}

// TestRoundTripIsGreenOverAStringProof is the ticket's own bar: the harness
// runs, through the entry point, over the one type the engine supports.
func TestRoundTripIsGreenOverAStringProof(t *testing.T) {
	c := &capture{}

	ferrytest.RoundTrip(c, ferrytest.MemPlane(), stringProof())

	if len(c.lines) != 0 {
		t.Errorf("a string proof against the memory plane reported %q, want nothing", c.lines)
	}
}

// TestRoundTripReportsThroughT asserts the third reason T exists: this package
// is authority, and the only way to hold authority to its own rules is to
// capture what it says.
func TestRoundTripReportsThroughT(t *testing.T) {
	c := &capture{}

	ferrytest.RoundTrip(c, ferrytest.MemPlane(), stringProof())

	if c.helpers == 0 {
		t.Error("RoundTrip never called Helper, so every failure it reports is attributed to a line in ferrytest")
	}
}

// TestRoundTripReportsAValueThatDiffers is one half of "independently": the
// plane hands back a different value and the representation ferry wrote is
// exactly what the golden asked for, so the round trip is the only column that
// can report.
func TestRoundTripReportsAValueThatDiffers(t *testing.T) {
	c := &capture{}
	p := &fakePlane{name: "shouting", read: upper}

	ferrytest.RoundTrip(c, p.plane(), []ferrytest.Proof{
		ferrytest.Type("string", ferrytest.Eq[string],
			ferrytest.At("localhost", ferry.String("localhost")),
		),
	})

	only := onlyLine(t, c)
	if !strings.Contains(only, `loaded "LOCALHOST", want "localhost"`) {
		t.Errorf("report = %q, want the round trip named", only)
	}

	if strings.Contains(only, "ferry encoded") {
		t.Errorf("report = %q, want the golden column silent", only)
	}
}

// nanoseconds models the time.Duration codec ADR-0005 rejects by name: thirty
// seconds spelled as 30000000000.
//
// ferry has no codec chain yet - #83 owns it - so the wrong representation is
// modelled where a codec's output would land, as the leaf's own text. What
// matters is the property the model reproduces exactly: nanoseconds round-trip
// perfectly, so the round-trip column reports zero failures against precisely
// this, and a harness with no golden column would have let ferry ship the
// representation ADR-0005 refuses.
type nanoseconds string

// TestRoundTripReportsAGoldenThatDiffers is the other half, and the reason a
// proof is a triple: the value survives the trip untouched and the
// representation is wrong.
func TestRoundTripReportsAGoldenThatDiffers(t *testing.T) {
	c := &capture{}

	ferrytest.RoundTrip(c, ferrytest.MemPlane(), []ferrytest.Proof{
		ferrytest.Type("time.Duration", ferrytest.Eq[nanoseconds],
			ferrytest.At(nanoseconds("30000000000"), ferry.String("30s")),
		),
	})

	only := onlyLine(t, c)
	if !strings.Contains(only, `ferry encoded string("30000000000") at /value, want string("30s")`) {
		t.Errorf("report = %q, want the golden column named", only)
	}

	// The measurement the golden column exists for: the round trip alone is
	// green against exactly the codec ADR-0005 rejects.
	if strings.Contains(only, "loaded") {
		t.Errorf("report = %q, want the round trip silent - nanoseconds round-trip perfectly", only)
	}
}

// TestRoundTripGoldenIsWhatFerryEncoded is what the wrapping sink is for.
//
// This plane spells what it is given differently and reads its own spelling
// back, so the round trip is perfect and the plane holds something ferry never
// wrote. A golden read off the plane would report that spelling; a golden read
// above the driver reports what ferry encoded, which is the column ADR-0013's
// promise rests on and the only one a driver changing both halves of its
// spelling together cannot hide from.
func TestRoundTripGoldenIsWhatFerryEncoded(t *testing.T) {
	c := &capture{}
	p := &fakePlane{name: "bracketed", spell: bracket, read: unbracket}

	ferrytest.RoundTrip(c, p.plane(), []ferrytest.Proof{
		ferrytest.Type("string", ferrytest.Eq[string],
			ferrytest.At("localhost", ferry.String("localhost")),
		),
	})

	if len(c.lines) != 0 {
		t.Fatalf("reported %q, want nothing: the golden is what ferry encoded", c.lines)
	}

	if got := p.minted[0].vals["/value"]; got != ferry.String("<localhost>") {
		t.Errorf("the plane holds %#v, so this test is not asserting what it says it is", got)
	}
}

// stamp is an RFC 3339 instant carried as text, which is how a time.Time proof
// can run at all today: the engine admits string leaves only, so time.Time
// itself compiles to a struct that maps no address and #72 is what widens it.
//
// The relation is genuinely time.Time.Equal underneath, which is the point.
type stamp string

// utc and berlin are one instant in two zones. == separates them, and so would
// reflect.DeepEqual; time.Time.Equal does not.
const (
	utc    = stamp("2026-08-02T12:00:00Z")
	berlin = stamp("2026-08-02T14:00:00+02:00")
)

// sameInstant is the proof's own relation, and it is time.Time.Equal reached
// through the text this plane carries.
func sameInstant(a, b stamp) bool {
	ta, aerr := time.Parse(time.RFC3339, string(a))
	tb, berr := time.Parse(time.RFC3339, string(b))

	return aerr == nil && berr == nil && ta.Equal(tb)
}

// TestRoundTripUsesTheProofsRelation asserts survey item 5.7's enforcement: the
// relation is the proof's and never the harness's.
//
// The plane hands back the same instant in another zone. Under the proof's own
// relation that is not a failure; under == - or under reflect.DeepEqual, which
// is the relation a harness reaches for when it has no other - it is a false
// one, and the repair for a harness that reports false failures is to loosen
// the comparison until it stops.
func TestRoundTripUsesTheProofsRelation(t *testing.T) {
	if reflect.DeepEqual(utc, berlin) {
		t.Fatal("the two spellings are equal, so this test asserts nothing")
	}

	p := &fakePlane{name: "zoned", read: func(ferry.Value) ferry.Value {
		return ferry.String(string(berlin))
	}}

	byInstant := &capture{}
	ferrytest.RoundTrip(byInstant, p.plane(), []ferrytest.Proof{
		ferrytest.Type("time.Time", sameInstant, ferrytest.At(utc, ferry.String(string(utc)))),
	})

	if len(byInstant.lines) != 0 {
		t.Errorf("under time.Time.Equal the proof reported %q, want nothing", byInstant.lines)
	}

	byEquality := &capture{}
	ferrytest.RoundTrip(byEquality, p.plane(), []ferrytest.Proof{
		ferrytest.Type("time.Time", ferrytest.Eq[stamp], ferrytest.At(utc, ferry.String(string(utc)))),
	})

	if len(byEquality.lines) != 1 {
		t.Errorf("under == the same case reported %q, want the false failure the relation avoids", byEquality.lines)
	}
}

// errRefused is what the fake plane fails with, so that a report can be matched
// on it rather than on a message this test wrote twice.
var errRefused = errors.New("the plane refused")

// TestRoundTripReportsADumpThatFailed asserts the failure arrives as a report
// rather than as a panic or a silence, and that the case stops there: a load
// after a dump that never wrote would report a second, derived failure.
func TestRoundTripReportsADumpThatFailed(t *testing.T) {
	c := &capture{}
	p := &fakePlane{name: "unwritable", setErr: errRefused}

	ferrytest.RoundTrip(c, p.plane(), stringProof())

	if len(c.lines) != 4 {
		t.Fatalf("reported %q, want one report per case", c.lines)
	}

	if !strings.Contains(c.lines[0], "dump:") || !strings.Contains(c.lines[0], errRefused.Error()) {
		t.Errorf("report = %q, want the dump and the driver's own message named", c.lines[0])
	}
}

// TestRoundTripReportsALoadThatFailed is the mirror, and it is a separate case
// rather than the same one: survey item 5.11 and its ferry-side mirror were
// each invisible for four prototypes because only the pair was ever checked.
func TestRoundTripReportsALoadThatFailed(t *testing.T) {
	c := &capture{}
	p := &fakePlane{name: "unreadable", getErr: errRefused}

	ferrytest.RoundTrip(c, p.plane(), []ferrytest.Proof{
		ferrytest.Type("string", ferrytest.Eq[string],
			ferrytest.At("localhost", ferry.String("localhost")),
		),
	})

	only := onlyLine(t, c)
	if !strings.Contains(only, "load:") || !strings.Contains(only, errRefused.Error()) {
		t.Errorf("report = %q, want the load and the driver's own message named", only)
	}
}

// TestRoundTripKeepsTheSinksOptionalInterfaces is why the recording combinator
// is unexported.
//
// The recorder sits between ferry and the driver's writer, and Committer and
// Releaser are discovered by assertion, so a wrapper that forwarded Set and
// forgot the other two would turn a staging sink into one that never commits -
// with the round trip still green on the memory plane and red on every real
// one. All four combinations run, because a wrapper is as easy to get wrong by
// keeping one interface as by keeping none.
func TestRoundTripKeepsTheSinksOptionalInterfaces(t *testing.T) {
	cases := []shellCase{
		{name: "neither", shell: plain},
		{name: "commits", shell: commitOnly, commits: 1},
		{name: "closes", shell: closeOnly, closes: 1},
		{name: "both", shell: commitAndClose, commits: 1, closes: 1},
	}

	for _, c := range cases {
		t.Run(c.name, c.assert)
	}
}

// shellCase is one sink shape and the lifecycle calls it must receive.
type shellCase struct {
	name    string
	shell   shell
	commits int
	closes  int
}

func (c shellCase) assert(t *testing.T) {
	t.Helper()

	rep := &capture{}
	p := &fakePlane{name: c.name, shell: c.shell}

	ferrytest.RoundTrip(rep, p.plane(), oneStringProof())

	if len(rep.lines) != 0 {
		t.Fatalf("reported %q, want nothing", rep.lines)
	}

	if s := p.minted[0]; s.commits != c.commits || s.closes != c.closes {
		t.Errorf("committed %d and closed %d, want %d and %d", s.commits, s.closes, c.commits, c.closes)
	}
}

// TestRoundTripReportsABindThatFailed and its open-side mirror assert that a
// driver's refusal reaches the report unchanged.
//
// The recorder is in the path for both, and a combinator that swallowed either
// one would make a plane that refuses every address look like a plane that
// wrote nothing - which is the shape survey item 5.11 found on the read side.
func TestRoundTripReportsABindThatFailed(t *testing.T) {
	c := &capture{}
	p := &fakePlane{name: "unbindable", bindErr: errRefused}

	ferrytest.RoundTrip(c, p.plane(), oneStringProof())

	if only := onlyLine(t, c); !strings.Contains(only, errRefused.Error()) {
		t.Errorf("report = %q, want the driver's own message", only)
	}
}

func TestRoundTripReportsAnOpenThatFailed(t *testing.T) {
	c := &capture{}
	p := &fakePlane{name: "unopenable", openErr: errRefused}

	ferrytest.RoundTrip(c, p.plane(), oneStringProof())

	if only := onlyLine(t, c); !strings.Contains(only, errRefused.Error()) {
		t.Errorf("report = %q, want the driver's own message", only)
	}
}

// TestRoundTripRefusesAPlaneItCannotDumpTo covers the two ways a Plane can
// leave RoundTrip with nowhere to write. Both are reported rather than
// panicked, because a suite that panics inside a driver's CI says nothing about
// the driver.
func TestRoundTripRefusesAPlaneItCannotDumpTo(t *testing.T) {
	t.Run("no Open", func(t *testing.T) {
		c := &capture{}

		ferrytest.RoundTrip(c, ferrytest.Plane{Name: "empty"}, stringProof())

		if only := onlyLine(t, c); !strings.Contains(only, "Open is nil") {
			t.Errorf("report = %q, want the missing Open named", only)
		}
	})

	t.Run("no sink", func(t *testing.T) {
		c := &capture{}
		p := ferrytest.Plane{
			Name: "read-only",
			Open: func() ferrytest.Instance { return ferrytest.Instance{Source: ferrytest.Static(nil)} },
		}

		ferrytest.RoundTrip(c, p, []ferrytest.Proof{
			ferrytest.Type("string", ferrytest.Eq[string], ferrytest.At("", ferry.String(""))),
		})

		if only := onlyLine(t, c); !strings.Contains(only, "mints no sink") {
			t.Errorf("report = %q, want the missing sink named", only)
		}
	})
}

// TestRoundTripRefusesAnOptionListItCannotCompileUnder is the one Option this
// signature cannot honour, reported once instead of once per case.
//
// A proof's value is a bare value, so the harness supplies the struct it
// travels in, and a tag key names the key ferry reads for that struct too. An
// Option is opaque, so there is no way to apply one to the caller's types and
// not to the harness's own.
func TestRoundTripRefusesAnOptionListItCannotCompileUnder(t *testing.T) {
	c := &capture{}

	ferrytest.RoundTrip(c, ferrytest.MemPlane(), stringProof(), ferry.TagKey("cfg"))

	if only := onlyLine(t, c); !strings.Contains(only, "unable to compile the struct it dumps a case in") {
		t.Errorf("report = %q, want the Option list named once", only)
	}
}

// TestRoundTripRefusesAProofWithNoRelation asserts the suite reports rather
// than panics. It runs against third-party code, so a panic inside it is a
// driver author's CI failing with a stack trace through ferrytest and nothing
// about their driver.
func TestRoundTripRefusesAProofWithNoRelation(t *testing.T) {
	c := &capture{}

	ferrytest.RoundTrip(c, ferrytest.MemPlane(), []ferrytest.Proof{
		ferrytest.Type[string]("string", nil, ferrytest.At("localhost", ferry.String("localhost"))),
	})

	if only := onlyLine(t, c); !strings.Contains(only, "carries no relation") {
		t.Errorf("report = %q, want the missing relation named", only)
	}
}

// onlyLine is the single report a case was expected to produce.
func onlyLine(t *testing.T, c *capture) string {
	t.Helper()

	if len(c.lines) != 1 {
		t.Fatalf("reported %q, want exactly one failure", c.lines)
	}

	return c.lines[0]
}

// upper is a plane that shouts back what it was given, which is a value that
// differs with a representation that does not.
func upper(v ferry.Value) ferry.Value {
	s, _ := v.AsString()

	return ferry.String(strings.ToUpper(s))
}

// bracket and unbracket are a driver's own spelling and its inverse: a plane
// that holds something ferry never wrote, and round-trips perfectly anyway.
func bracket(v ferry.Value) ferry.Value {
	s, _ := v.AsString()

	return ferry.String("<" + s + ">")
}

func unbracket(v ferry.Value) ferry.Value {
	s, _ := v.AsString()

	return ferry.String(strings.TrimSuffix(strings.TrimPrefix(s, "<"), ">"))
}

// shell is which of the two optional interfaces a fake plane's writer carries.
type shell uint8

const (
	plain shell = iota
	commitOnly
	closeOnly
	commitAndClose
)

// fakePlane is a plane whose every failure mode is a field, and it keeps every
// instance it minted so that a test can ask what happened to each.
type fakePlane struct {
	name    string
	spell   func(ferry.Value) ferry.Value
	read    func(ferry.Value) ferry.Value
	bindErr error
	openErr error
	setErr  error
	getErr  error
	shell   shell
	minted  []*fakeStore
}

// plane is the description RoundTrip takes. Open mints fresh contents every
// call, which is what the suite requires of a real driver.
func (p *fakePlane) plane() ferrytest.Plane {
	return ferrytest.Plane{
		Name: p.name,
		Open: func() ferrytest.Instance {
			s := &fakeStore{vals: map[string]ferry.Value{}, of: p}
			p.minted = append(p.minted, s)

			return ferrytest.Instance{Source: fakeSource{s}, Sink: p.sinkOver(s)}
		},
	}
}

func (p *fakePlane) sinkOver(s *fakeStore) ferry.Sink {
	switch p.shell {
	case commitOnly:
		return commitSink{s}
	case closeOnly:
		return closeSink{s}
	case commitAndClose:
		return stagingSink{s}
	default:
		return fakeSink{s}
	}
}

// openWriter is the one place the fake plane's two pre-write refusals live, so
// that every sink shape below refuses identically.
func (p *fakePlane) openWriter(w ferry.Writer) (ferry.OpenWriterFunc, error) {
	if p.bindErr != nil {
		return nil, p.bindErr
	}

	return func(context.Context) (ferry.Writer, error) {
		if p.openErr != nil {
			return nil, p.openErr
		}

		return w, nil
	}, nil
}

// fakeStore is one minted instance's contents plus what it was asked to do.
type fakeStore struct {
	vals    map[string]ferry.Value
	of      *fakePlane
	commits int
	closes  int
}

type fakeSource struct{ s *fakeStore }

func (k fakeSource) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(context.Context) (ferry.Reader, error) { return fakeReader{k.s}, nil }, nil
}

type fakeReader struct{ s *fakeStore }

func (r fakeReader) Get(_ context.Context, addr ferry.Path) (ferry.Value, error) {
	if r.s.of.getErr != nil {
		return ferry.Value{}, r.s.of.getErr
	}

	got := r.s.vals[addr.String()]
	if r.s.of.read == nil || got.Kind() == ferry.KindAbsent {
		return got, nil
	}

	return r.s.of.read(got), nil
}

type fakeSink struct{ s *fakeStore }

func (k fakeSink) Bind(*ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	return k.s.of.openWriter(fakeWriter{k.s})
}

type fakeWriter struct{ s *fakeStore }

func (w fakeWriter) Set(_ context.Context, addr ferry.Path, v ferry.Value) error {
	if w.s.of.setErr != nil {
		return w.s.of.setErr
	}

	if w.s.of.spell != nil {
		v = w.s.of.spell(v)
	}

	w.s.vals[addr.String()] = v

	return nil
}

// The three sinks whose writers carry one or both optional interfaces.
// stagingWriter is the ordinary shape of a file sink writing through a
// temporary; the other two exist because a wrapper is as easy to get wrong by
// keeping one interface as by keeping none.
type (
	commitSink  struct{ s *fakeStore }
	closeSink   struct{ s *fakeStore }
	stagingSink struct{ s *fakeStore }
)

func (k commitSink) Bind(*ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	return k.s.of.openWriter(commitWriter{fakeWriter{k.s}})
}

func (k closeSink) Bind(*ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	return k.s.of.openWriter(closeWriter{fakeWriter{k.s}})
}

func (k stagingSink) Bind(*ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	return k.s.of.openWriter(stagingWriter{fakeWriter{k.s}})
}

type (
	commitWriter  struct{ fakeWriter }
	closeWriter   struct{ fakeWriter }
	stagingWriter struct{ fakeWriter }
)

func (w commitWriter) Commit(context.Context) error {
	w.s.commits++

	return nil
}

func (w closeWriter) Close() error {
	w.s.closes++

	return nil
}

func (w stagingWriter) Commit(context.Context) error {
	w.s.commits++

	return nil
}

func (w stagingWriter) Close() error {
	w.s.closes++

	return nil
}
