package ferrytest_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// The failures the fixtures in this file stage, each distinct so that a report
// can be traced back to the fixture that caused it.
var (
	errSpentOpen  = errors.New("this open has already been used once")
	errAlreadyHad = errors.New("this address was written by an earlier dump")
	errCharset    = errors.New("a map key may not hold a hyphen")
)

// TestDriverSaysWhatItScaledTo is what a driver author has instead of counting
// which cases ran.
//
// Seven of the contract's interfaces are optional and are discovered by
// assertion, so two conformant drivers execute very different numbers of cases,
// and a case that quietly did nothing reads exactly like a case that passed.
// The memory plane probes, enumerates, ensures and forgets a composite, and
// holds neither resource nor staging, so the line names those four capabilities
// and neither "commits" nor "releases".
func TestDriverSaysWhatItScaledTo(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, ferrytest.MemPlane())

	line, ok := lineContaining(c.logs, "the suite scaled to:")
	if !ok {
		t.Fatalf("the suite logged %q, want one line saying what it scaled to", c.logs)
	}

	for _, want := range []string{
		"a source", "a sink", "probes a container's own address", "enumerates",
		"ensures a container's own address",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("the line %q does not name %q", line, want)
		}
	}

	for _, unwanted := range []string{"commits", "releases"} {
		if strings.Contains(line, unwanted) {
			t.Errorf("the line %q names %q, which the memory plane does not implement", line, unwanted)
		}
	}
}

// TestDriverSaysWhichHalfIsMissing is the same visibility for the one thing
// that silences whole cases rather than narrowing one: a plane with no honest
// Dump, which is ADR-0004's own case and a description rather than a defect.
func TestDriverSaysWhichHalfIsMissing(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, ferrytest.Plane{
		Name:  "read-only",
		Kinds: allKinds(),
		Open:  func() ferrytest.Instance { return ferrytest.Instance{Source: ferrytest.Static(nil)} },
	})

	if !anyLineContains(c.logs, "mints no sink, so every case that writes is silent") {
		t.Errorf("the suite logged %q, want the missing half named once", c.logs)
	}
}

// lineContaining is the first captured line holding want.
func lineContaining(lines []string, want string) (string, bool) {
	for _, line := range lines {
		if strings.Contains(line, want) {
			return line, true
		}
	}

	return "", false
}

// TestDriverCase14ReportsAnOpenThatIsSpentByItsFirstUse is case 14, negative,
// and it is the defect the case exists for.
//
// A driver that keeps mutable state in the closure it hands back was correct
// while a bind lived and died inside one Load, and is wrong now that a caller
// holds a binding and loads through it from many goroutines. An open that
// succeeds once and fails afterwards is that defect made deterministic, so the
// case can be asserted without depending on a scheduler.
func TestDriverCase14ReportsAnOpenThatIsSpentByItsFirstUse(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("spent-open", func(inst *ferrytest.Instance) {
		inst.Source = spentSource{inner: inst.Source, once: &sync.Once{}}
	}))

	if !anyLineContains(c.lines, "case 14") {
		t.Errorf("the suite reported %q, want case 14 naming an open that a second caller could not use", c.lines)
	}
}

// spentSource hands back an open that works once and refuses afterwards, which
// is what a driver writing to what it closed over looks like from outside.
//
// The first use is the suite's own sequential one, so what the concurrent half
// meets is a closure already spent - and a plane that could never be opened at
// all would be skipped rather than reported, which is the other half of the
// rule this fixture pins.
type spentSource struct {
	inner ferry.Source
	once  *sync.Once
}

func (s spentSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		spent := true
		s.once.Do(func() { spent = false })

		if spent {
			return nil, errSpentOpen
		}

		return open(ctx)
	}, nil
}

// TestDriverCase14SkipsAPlaneThatCannotBeOpenedAtAll is the other half of the
// same rule: a plane that fails the one sequential open says nothing about
// eight at once, and cases 4, 6 and 10 are where a broken open is reported.
func TestDriverCase14SkipsAPlaneThatCannotBeOpenedAtAll(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("never-opens", func(inst *ferrytest.Instance) {
		inst.Source = unopenableSource{inner: inst.Source, when: anySet, err: errNoReader}
	}))

	for _, line := range c.lines {
		if strings.Contains(line, "case 14") {
			t.Errorf("case 14 reported %q, want it silent about an open that never succeeds", line)
		}
	}

	if !anyLineContains(c.logs, "case 14 skipped") {
		t.Errorf("the suite logged %q, want case 14 saying out loud that it could not run", c.logs)
	}
}

// anySet is every address set, for a fixture whose misbehaviour is not scoped
// to one case's own binding.
func anySet(*ferry.AddressSet) bool { return true }

// TestDriverCase15ReportsASinkThatIsSpentByItsFirstDump is case 15, negative.
//
// A sink keeping the set of addresses it has written on the binding rather than
// on the open refuses the second dump of the same configuration, which is the
// ordinary reload every held binding exists for. It is also the defect the
// memory plane itself carried until this case was written.
func TestDriverCase15ReportsASinkThatIsSpentByItsFirstDump(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("write-once", func(inst *ferrytest.Instance) {
		inst.Sink = writeOnceSink{inner: inst.Sink, seen: map[string]bool{}}
	}))

	if !anyLineContains(c.lines, "case 15: the second dump through the same binding failed") {
		t.Errorf("the suite reported %q, want case 15 naming the refused second dump", c.lines)
	}
}

// writeOnceSink refuses any address it has ever written, across every open of
// every binding, which is a minted set that outlived the open that made it.
type writeOnceSink struct {
	inner ferry.Sink
	seen  map[string]bool
}

func (s writeOnceSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Writer, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return writeOnceWriter{inner: w, seen: s.seen}, nil
	}, nil
}

type writeOnceWriter struct {
	inner ferry.Writer
	seen  map[string]bool
}

func (w writeOnceWriter) Set(ctx context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	if w.seen[addr.Path().String()] {
		return ferry.ErrorAt(addr.Path(), errAlreadyHad)
	}

	w.seen[addr.Path().String()] = true

	return w.inner.Set(ctx, addr, v)
}

func (w writeOnceWriter) Ensure(ctx context.Context, addr ferry.Container, p ferry.Presence) error {
	return ensureThrough(ctx, w.inner, addr, p)
}

func (w writeOnceWriter) Unset(ctx context.Context, addr ferry.CompositeAddr) error {
	return unsetThrough(ctx, w.inner, addr)
}

// TestDriverCase15SkipsASinkWhoseFirstDumpFailed is the attribution rule, which
// is what keeps one defect from being reported by three cases.
//
// Case 15 asks what a second dump through a held binding does, and a sink that
// could not take the first one has not answered that question badly, it has not
// been asked it. Case 1 owns a dump that cannot happen, so this case says out
// loud that it stood down and leaves the report to the case it belongs to.
func TestDriverCase15SkipsASinkWhoseFirstDumpFailed(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("no-first-dump", func(inst *ferrytest.Instance) {
		inst.Sink = pickyWriteSink{inner: inst.Sink, refuse: refuseTheLeaf}
	}))

	if !anyLineContains(c.logs, "case 15 skipped") {
		t.Errorf("the suite logged %q, want case 15 standing down for a first dump that failed", c.logs)
	}

	if !anyLineContains(c.logs, "case 1 is where a dump that cannot happen is reported") {
		t.Errorf("the suite logged %q, want the skip naming the case the report belongs to", c.logs)
	}

	for _, line := range c.lines {
		if strings.Contains(line, "case 15:") {
			t.Errorf("case 15 reported %q, and a case that stood down reports nothing", line)
		}
	}
}

// refuseTheLeaf refuses the one write every fixture in this suite makes, so the
// first dump of case 15 fails before there is a second one to ask about.
func refuseTheLeaf(_ *ferry.AddressSet, addr ferry.Path, _ ferry.Value) error {
	if addr == probeLeaf {
		return errNoWrite
	}

	return nil
}

// TestDriverCase15ReportsASinkThatKeepsTheEarlierValue is the half that is
// worse than a refusal, because nothing says so: the second dump is accepted
// and the plane still holds what the first one wrote.
func TestDriverCase15ReportsASinkThatKeepsTheEarlierValue(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("stale", func(inst *ferrytest.Instance) {
		inst.Sink = droppingSink{inner: inst.Sink}
	}))

	if !anyLineContains(c.lines, "case 15: after two dumps the plane holds") {
		t.Errorf("the suite reported %q, want case 15 naming the value the plane kept", c.lines)
	}
}

// droppingSink takes the second dump's value at the leaf and writes nothing,
// reporting success, which is the failure a refusal at least makes visible.
type droppingSink struct{ inner ferry.Sink }

func (s droppingSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Writer, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return droppingWriter{inner: w}, nil
	}, nil
}

type droppingWriter struct{ inner ferry.Writer }

func (w droppingWriter) Set(ctx context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	if addr.Path() == probeLeaf && v == ferry.String("y") {
		return nil
	}

	return w.inner.Set(ctx, addr, v)
}

func (w droppingWriter) Ensure(ctx context.Context, addr ferry.Container, p ferry.Presence) error {
	return ensureThrough(ctx, w.inner, addr, p)
}

func (w droppingWriter) Unset(ctx context.Context, addr ferry.CompositeAddr) error {
	return unsetThrough(ctx, w.inner, addr)
}

// TestDriverCase9ReportsASinkThatCannotSpellOneDynamicKey is the hole case 8
// used to leave, and the reason case 9 mints every key the suite mints.
//
// A store with a restrictive key charset - one that will not take a hyphen -
// refuses the first of the two addresses case 8 writes. Case 8 is then over,
// legitimately, because a write that never landed says nothing about what a
// second open retained. Nothing else wrote that address, so the whole suite was
// green for a sink that cannot store a map key an ordinary config struct holds.
func TestDriverCase9ReportsASinkThatCannotSpellOneDynamicKey(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("hyphen-free", func(inst *ferrytest.Instance) {
		inst.Sink = pickyWriteSink{inner: inst.Sink, refuse: refuseHyphen(errCharset)}
	}))

	if !anyLineContains(c.lines, "case 9: the sink refused an address under a container") {
		t.Errorf("the suite reported %q, want case 9 naming the dynamic address the sink would not take", c.lines)
	}

	if !anyLineContains(c.logs, "case 8 skipped: the write was refused with") {
		t.Errorf("the suite logged %q, want case 8 saying out loud that it could not run", c.logs)
	}
}

// refuseHyphen declines any address holding a hyphen, which is the map key case
// 8 mints first and one of the three case 9 mints together.
func refuseHyphen(err error) func(*ferry.AddressSet, ferry.Path, ferry.Value) error {
	return func(_ *ferry.AddressSet, addr ferry.Path, _ ferry.Value) error {
		if strings.Contains(addr.String(), "-") {
			return err
		}

		return nil
	}
}

// TestDriverCase5ReportsChildrenInventedAtABlankContainer is case 5's other
// half, and it is what a driver answering out of every key sharing a prefix
// passes when only the populated containers are enumerated.
//
// A member invented at a container the plane holds nothing under is a field
// loaded out of somebody else's key, with a nil error and a plausible value.
func TestDriverCase5ReportsChildrenInventedAtABlankContainer(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("fabricating", func(inst *ferrytest.Instance) {
		inst.Source = fabricatingSource{inner: inst.Source}
	}))

	if !anyLineContains(c.lines, "case 5: Children at /nillist answered") {
		t.Errorf("the suite reported %q, want case 5 naming the members invented at a blank container", c.lines)
	}
}

// fabricatingSource invents one member at the blank composite the suite asks
// about, and answers truthfully everywhere else.
type fabricatingSource struct{ inner ferry.Source }

func (s fabricatingSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return fabricatingReader{inner: r}, nil
	}, nil
}

type fabricatingReader struct{ inner ferry.Reader }

func (r fabricatingReader) Get(ctx context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	return r.inner.Get(ctx, addr)
}

func (r fabricatingReader) Probe(ctx context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	return probeThrough(ctx, r.inner, addr)
}

func (r fabricatingReader) Children(ctx context.Context, addr ferry.CompositeAddr) ([]ferry.Segment, error) {
	got, err := childrenThrough(ctx, r.inner, addr)
	if err != nil || addr.Path() != ferry.At("nillist") {
		return got, err
	}

	return append(got, ferry.IndexSegment(0)), nil
}

// TestDriverReleasesEveryReaderItOpens is the leak the suite's own fixture used
// to have: the wrapper case 4 loads through declared Get and nothing else, so
// core's release found no Releaser and the driver's own reader was never
// closed.
//
// One descriptor per Driver call, for a file- or connection-backed driver.
func TestDriverReleasesEveryReaderItOpens(t *testing.T) {
	counted := &openCount{}

	ferrytest.Driver(&capture{}, wrapPlane("counted", func(inst *ferrytest.Instance) {
		inst.Source = countingSource{inner: inst.Source, count: counted}
	}))

	counted.mu.Lock()
	defer counted.mu.Unlock()

	if counted.opens != counted.closes {
		t.Errorf("the suite opened %d readers and closed %d: a suite minting an instance per case leaks a "+
			"handle per case unless every one of them is released", counted.opens, counted.closes)
	}
}

// openCount is what the counting plane records, under a lock because case 14
// opens from many goroutines at once.
type openCount struct {
	mu            sync.Mutex
	opens, closes int
}

type countingSource struct {
	inner ferry.Source
	count *openCount
}

func (s countingSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}

		s.count.mu.Lock()
		s.count.opens++
		s.count.mu.Unlock()

		return countingReader{inner: r, count: s.count}, nil
	}, nil
}

type countingReader struct {
	inner ferry.Reader
	count *openCount
}

func (r countingReader) Get(ctx context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	return r.inner.Get(ctx, addr)
}

func (r countingReader) Probe(ctx context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	return probeThrough(ctx, r.inner, addr)
}

func (r countingReader) Children(ctx context.Context, addr ferry.CompositeAddr) ([]ferry.Segment, error) {
	return childrenThrough(ctx, r.inner, addr)
}

func (r countingReader) Close() error {
	r.count.mu.Lock()
	defer r.count.mu.Unlock()

	r.count.closes++

	return nil
}
