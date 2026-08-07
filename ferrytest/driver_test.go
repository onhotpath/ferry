package ferrytest_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestDriverIsGreenAgainstTheMemoryPlane is the suite's own bar.
//
// The memory plane is the one plane that adds nothing - it stores the boundary
// Value itself - so a case it fails is a case that is wrong about ferry rather
// than about a driver.
func TestDriverIsGreenAgainstTheMemoryPlane(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, ferrytest.MemPlane())

	if len(c.lines) != 0 {
		t.Errorf("the suite reported %q against the memory plane, want nothing", c.lines)
	}
}

// TestDriverCallsRoundTrip is the sentence that keeps driver/* a single-call CI
// glob, held to mechanically.
//
// A suite that restated the round trip would drift from it, and a driver author
// who has to make two calls has one they can leave out. The evidence is the
// report's own shape: RoundTrip labels a failure with the proof and the case
// number, and nothing else in the suite produces that line.
func TestDriverCallsRoundTrip(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, shoutingPlane())

	if !anyLineContains(c.lines, "loaded") {
		t.Errorf("the suite reported %q, want a round-trip comparison among them", c.lines)
	}
}

// TestDriverReportsThroughT is the third reason T exists, applied to this suite:
// a package that is authority can only be held to its own rules by capturing
// what it says.
func TestDriverReportsThroughT(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, ferrytest.MemPlane())

	if c.helpers == 0 {
		t.Error("Driver never called Helper, so every failure it reports is attributed to a line in ferrytest")
	}
}

// TestDriverRefusesAPlaneItCannotMint reports rather than panicking, because a
// suite that panics inside a driver's CI says nothing about the driver.
func TestDriverRefusesAPlaneItCannotMint(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, ferrytest.Plane{Name: "empty"})

	if only := onlyLine(t, c); !strings.Contains(only, "Open is nil") {
		t.Errorf("report = %q, want the missing Open named", only)
	}
}

// TestDriverRefusesAnOptionListItCannotCompileUnder is where #110 lands for this
// signature: the suite supplies the structs it dumps, and a tag key names the
// key ferry reads for those too. It is reported once rather than as an identical
// failure in every case.
func TestDriverRefusesAnOptionListItCannotCompileUnder(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, ferrytest.MemPlane(), ferry.TagKey("cfg"))

	if only := onlyLine(t, c); !strings.Contains(only, "unable to compile the structs it dumps") {
		t.Errorf("report = %q, want the Option list named once", only)
	}
}

// TestDriverCase1DemandsARefusalForADisclaimedKind is the negative test for case
// 1, and it is broken in exactly one way: a plane that carries a null and says
// it does not.
//
// That is the shape the declaration exists to catch from the other side. A flat
// plane has no null, so a nil composite is a value it cannot represent, and the
// two wrong answers are to fail every flat driver or to let a nil pointer
// silently become a zero value. What the suite demands instead is a loud
// refusal, and this plane gives none.
func TestDriverCase1DemandsARefusalForADisclaimedKind(t *testing.T) {
	c := &capture{}

	p := ferrytest.MemPlane()
	p.Kinds = slices.DeleteFunc(p.Kinds, func(k ferry.VKind) bool { return k == ferry.KindNull })

	ferrytest.Driver(c, p)

	if len(c.lines) == 0 {
		t.Fatal("a plane that disclaims the null it carries reported nothing")
	}

	for _, line := range c.lines {
		if !strings.Contains(line, "does not declare kind null") {
			t.Errorf("report = %q, want only case 1's missing refusal", line)
		}
	}
}

// TestDriverCase11ComparesAgainstThePlanesOwnContents is the golden artefact,
// from both sides.
//
// It is the case a round trip structurally cannot stand in for: a round trip
// tests a function against its own inverse, and a spelling is a choice of
// function, so changing both halves together is invisible to any test that only
// composes them. Here the plane's spelling is fixed and the expectation moves,
// which is the same observation from the other end.
func TestDriverCase11ComparesAgainstThePlanesOwnContents(t *testing.T) {
	t.Run("agreeing", func(t *testing.T) {
		c := &capture{}

		ferrytest.Driver(c, renderingPlane(goldenRendering))

		if len(c.lines) != 0 {
			t.Errorf("a plane whose golden matches its contents reported %q", c.lines)
		}
	})

	t.Run("disagreeing", func(t *testing.T) {
		c := &capture{}

		ferrytest.Driver(c, renderingPlane("/leaf=string(\"other\")\n"))

		only := onlyLine(t, c)
		if !strings.Contains(only, "case 11") {
			t.Errorf("report = %q, want case 11 and only case 11", only)
		}
	})
}

// TestDriverCase11RefusesAPlaneThatPinsASpellingAndCannotShowIt is the pair the
// empty-Golden signal exists to keep apart.
//
// A plane with no serialization format pins no golden and mints no Contents, and
// the case is skipped for it. A plane that pins one and mints no Contents has
// made a promise nothing can read back, and is refused loudly rather than
// passing quietly.
func TestDriverCase11RefusesAPlaneThatPinsASpellingAndCannotShowIt(t *testing.T) {
	c := &capture{}

	p := ferrytest.MemPlane()
	p.Golden = []ferrytest.Artefact{ferrytest.Golden(goldenFixture{Leaf: "x"}, goldenRendering)}

	ferrytest.Driver(c, p)

	if only := onlyLine(t, c); !strings.Contains(only, "mints no Contents") {
		t.Errorf("report = %q, want the unreadable promise named", only)
	}
}

// TestDriverSkipsExplicitly is ADR-0014's "the skip is explicit rather than
// silent", asserted where a skip can be seen.
//
// Case 10 is skipped for a plane that puts nothing in a context, which is every
// driver in this repository and the memory plane: such a plane's halves carry
// their own contents, so there is no absence of a plane for an open to refuse.
// It is not skipped where an Instance fills in InContext, and
// perrequest_test.go asserts that other half. Case 11 is skipped for a plane
// with no serialization format, which the memory plane is. A case that quietly
// did nothing would be indistinguishable from a case that passed.
func TestDriverSkipsExplicitly(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, ferrytest.MemPlane())

	for _, want := range []string{"case 10 skipped", "case 11 skipped"} {
		if !anyLineContains(c.logs, want) {
			t.Errorf("the suite logged %q, want %q among them", c.logs, want)
		}
	}
}

// TestDriverCase6ObservesAStagingSink is the lifecycle protocol against a sink
// that has one: Commit only on success, Close either way.
//
// The memory plane implements neither optional interface, so it exercises the
// half of case 6 where there is nothing to call. This is the other half, and it
// is also the assertion that the shell the case wraps a driver in keeps the
// driver's own answer to what it implements.
func TestDriverCase6ObservesAStagingSink(t *testing.T) {
	c := &capture{}
	s := &stagingCounts{}

	ferrytest.Driver(c, stagingPlane(s))

	if len(c.lines) != 0 {
		t.Fatalf("a staging plane reported %q", c.lines)
	}

	commits, closes := s.read()
	if commits == 0 || closes == 0 {
		t.Errorf("the suite observed %d commits and %d closes, want the driver's own lifecycle exercised",
			commits, closes)
	}

	if closes <= commits {
		t.Errorf("Close ran %d times against %d commits, and Close runs where Commit does not", closes, commits)
	}
}

// goldenFixture is the value the golden rows below dump. It is a struct because
// the root of a schema is one.
type goldenFixture struct {
	Leaf string `ferry:"leaf"`
}

// goldenRendering is what [renderSink] holds after a [goldenFixture] carrying
// "x" has been dumped through it.
const goldenRendering = "/leaf=string(\"x\")\n"

// renderingPlane is the memory plane given a spelling: every write is recorded
// and rendered back, so a golden row has something to be compared against.
//
// It wraps rather than replaces, which keeps every other case running against
// the plane the rest of this file already trusts, so a report from this plane is
// about case 11 and nothing else.
func renderingPlane(want string) ferrytest.Plane {
	mem := ferrytest.MemPlane()
	p := mem

	p.Name = "rendering"
	p.Open = func() ferrytest.Instance {
		inst := mem.Open()
		r := &renderSink{inner: inst.Sink, seen: map[string]string{}}
		inst.Sink, inst.Contents = r, r.contents

		return inst
	}
	p.Golden = []ferrytest.Artefact{ferrytest.Golden(goldenFixture{Leaf: "x"}, want)}

	return p
}

// renderSink is a spelling with nothing behind it: it renders what it was
// written, deterministically and injectively over the writes it saw.
type renderSink struct {
	inner ferry.Sink
	seen  map[string]string
}

func (s *renderSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Writer, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return renderWriter{inner: w, sink: s}, nil
	}, nil
}

// contents is the rendering, sorted so that it is one string over repeated runs.
func (s *renderSink) contents() ([]byte, error) {
	lines := make([]string, 0, len(s.seen))
	for addr, v := range s.seen {
		lines = append(lines, addr+"="+v+"\n")
	}

	slices.Sort(lines)

	return []byte(strings.Join(lines, "")), nil
}

type renderWriter struct {
	inner ferry.Writer
	sink  *renderSink
}

func (w renderWriter) Set(ctx context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	w.sink.seen[addr.String()] = v.GoString()

	return w.inner.Set(ctx, addr, v)
}

func (w renderWriter) Ensure(ctx context.Context, addr ferry.Container, p ferry.Presence) error {
	return ensureThrough(ctx, w.inner, addr, p)
}

func (w renderWriter) Unset(ctx context.Context, addr ferry.CompositeAddr) error {
	return unsetThrough(ctx, w.inner, addr)
}

// shoutingPlane is the memory plane with a reader that changes what it holds,
// which is the one failure only a round trip can report.
func shoutingPlane() ferrytest.Plane {
	mem := ferrytest.MemPlane()
	p := mem

	p.Name = "shouting"
	p.Open = func() ferrytest.Instance {
		inst := mem.Open()
		inst.Source = shoutingSource{inner: inst.Source}

		return inst
	}

	return p
}

type shoutingSource struct{ inner ferry.Source }

func (s shoutingSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return shoutingReader{inner: r}, nil
	}, nil
}

type shoutingReader struct{ inner ferry.Reader }

func (r shoutingReader) Get(ctx context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	got, err := r.inner.Get(ctx, addr)
	if err != nil {
		return got, err
	}

	s, ok := textOf(got)
	if !ok {
		return got, nil
	}

	return ferry.String(strings.ToUpper(s)), nil
}

// textOf is the String value's own text, and reports false for anything else.
func textOf(v ferry.Value) (string, bool) {
	if v.Kind() != ferry.KindString {
		return "", false
	}

	s, err := v.AsString()

	return s, err == nil
}

func (r shoutingReader) Probe(ctx context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	return probeThrough(ctx, r.inner, addr)
}

func (r shoutingReader) Children(ctx context.Context, addr ferry.CompositeAddr) ([]ferry.Segment, error) {
	return childrenThrough(ctx, r.inner, addr)
}

// stagingCounts is what a staging plane's writer was asked to do.
// stagingCounts is what the staging spy records.
//
// It is guarded, because case 14 opens the write half from many goroutines at
// once and a spy that counted without a lock would be reporting a race in the
// fixture rather than in anything under test. That is the obligation ADR-0004
// puts on a driver's own open, met by the stand-in for one.
type stagingCounts struct {
	mu      sync.Mutex
	commits int
	closes  int
}

// commit and close are the two counters, taken under the lock.
func (c *stagingCounts) commit() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.commits++
}

func (c *stagingCounts) close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closes++
}

// read hands back both counts at once, so a caller compares two numbers from
// one moment.
func (c *stagingCounts) read() (commits, closes int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.commits, c.closes
}

// stagingPlane is the memory plane whose writer stages and holds a resource,
// which is the ordinary shape of a file sink writing through a temporary.
func stagingPlane(s *stagingCounts) ferrytest.Plane {
	mem := ferrytest.MemPlane()
	p := mem

	p.Name = "staging"
	p.Open = func() ferrytest.Instance {
		inst := mem.Open()
		inst.Sink = stagingSpy{inner: inst.Sink, counts: s}

		return inst
	}

	return p
}

type stagingSpy struct {
	inner  ferry.Sink
	counts *stagingCounts
}

func (s stagingSpy) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Writer, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return stagingSpyWriter{inner: w, counts: s.counts}, nil
	}, nil
}

type stagingSpyWriter struct {
	inner  ferry.Writer
	counts *stagingCounts
}

func (w stagingSpyWriter) Set(ctx context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	return w.inner.Set(ctx, addr, v)
}

func (w stagingSpyWriter) Ensure(ctx context.Context, addr ferry.Container, p ferry.Presence) error {
	return ensureThrough(ctx, w.inner, addr, p)
}

func (w stagingSpyWriter) Unset(ctx context.Context, addr ferry.CompositeAddr) error {
	return unsetThrough(ctx, w.inner, addr)
}

func (w stagingSpyWriter) Commit(context.Context) error {
	w.counts.commit()

	return nil
}

func (w stagingSpyWriter) Close() error {
	w.counts.close()

	return nil
}

// anyLineContains reports whether any of a captured report's lines holds want.
func anyLineContains(lines []string, want string) bool {
	return slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, want) })
}
