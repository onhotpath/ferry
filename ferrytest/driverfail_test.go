package ferrytest_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// The addresses the suite's own fixtures land at, spelled again here because a
// negative fixture is written from outside the package and has to name the
// address it breaks. They are written out rather than derived, for the reason
// the suite's own are: an address a test computes the way the code under test
// does is an address that agrees with itself.
var (
	probeLeaf    = ferry.At("leaf")
	probeList    = ferry.At("list")
	probeMap     = ferry.At("map")
	probeMissing = ferry.At("missing")
	probeSection = ferry.At("section")
)

// The failures the fixtures below stage, each one distinct so that a report can
// be traced back to the fixture that caused it.
var (
	errUnreachable = errors.New("nothing has reached this plane yet")
	errNoWriter    = errors.New("the writer could not be opened")
	errNoReader    = errors.New("the reader could not be opened")
	errNoRead      = errors.New("the address could not be read")
	errNoList      = errors.New("the container could not be listed")
	errNoWrite     = errors.New("the write was refused")
	errNoRelease   = errors.New("the writer could not be released")
)

// TestDriverCase2ReportsABindThatRefusesAnUnreachablePlane is case 2, negative,
// from the read side.
//
// Bind takes no [context.Context] precisely so that it cannot do the I/O that
// would fail against a plane nothing has reached yet, and a driver that dials or
// stats there refuses a legal address set. The misbehaviour is scoped to the one
// address set case 2 binds, so every other case runs against the plane the rest
// of these tests already trust.
//
// One defect, two reports, and the second is not slack in the fixture: case 4
// loads the same leaf-only fixture, so a Bind that refuses that set is a load
// that fails for a reason case 4 is entitled to name.
func TestDriverCase2ReportsABindThatRefusesAnUnreachablePlane(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("unreachable-source", func(inst *ferrytest.Instance) {
		inst.Source = refusingSource{inner: inst.Source, when: isLeafSet, err: errUnreachable}
	}))

	reportsExactly(t, c, "case 2: Source.Bind refused", "case 4: the load failed with")
}

// TestDriverCase2ReportsASinkBindThatRefusesAnUnreachablePlane is the write half
// of the same rule, and the second report is case 6's for the same reason: the
// lifecycle case dumps one leaf, so a sink that will not bind that address set
// has no lifecycle to observe.
func TestDriverCase2ReportsASinkBindThatRefusesAnUnreachablePlane(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("unreachable-sink", func(inst *ferrytest.Instance) {
		inst.Sink = refusingSink{inner: inst.Sink, when: isLeafSet, err: errUnreachable}
	}))

	reportsExactly(t, c, "case 2: Sink.Bind refused", "case 6: dumping one leaf failed")
}

// TestDriverCase4ReportsAnOpenThatFails is case 2's other side, asserted where
// it is legal.
//
// What a driver may not do at Bind it may do at the open, so a plane that
// refuses there is not a case 2 failure. It is still a load that failed, and
// case 4's rule is that the error reaching the caller carries the driver's own
// class - which this one cannot, because the Get it was staged for never ran.
func TestDriverCase4ReportsAnOpenThatFails(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("unopenable-source", func(inst *ferrytest.Instance) {
		inst.Source = unopenableSource{inner: inst.Source, when: isLeafSet, err: errNoReader}
	}))

	reportsExactly(t, c, "case 4: the load failed with")
}

// TestDriverCase3ReportsAGetThatFailsWhereThereIsNothingToFailAt is case 3's
// third answer, negative.
//
// A container address holds no value, so the only two honest answers there are
// Absent and the Null an empty composite was dumped with. An error is neither: a
// driver that fails at an address its plane simply does not hold has turned a
// missing optional into a broken load. The misbehaviour is scoped to the one
// address nothing ever writes to, which is the only address in the suite that
// asks the question.
func TestDriverCase3ReportsAGetThatFailsWhereThereIsNothingToFailAt(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("failing-get", func(inst *ferrytest.Instance) {
		inst.Source = pickyReadSource{inner: inst.Source, at: probeMissing, err: errNoRead}
	}))

	reportsExactly(t, c, "case 3: Get at the container address /missing failed")
}

// TestDriverCase5SkipsAReaderThatDoesNotEnumerate is ADR-0004's optional
// [ferry.Enumerator], asserted from the side that has to stay silent.
//
// A Vault token with read and no list is ordinary, so a reader that does not
// enumerate is skipped rather than failed - and the skip is said out loud,
// because a case that quietly did nothing is indistinguishable from a case that
// passed. The reader is blinded only for the address set case 5 binds, since a
// plane that never enumerates cannot load a composite and would fail case 1 for
// a reason this case is not about.
func TestDriverCase5SkipsAReaderThatDoesNotEnumerate(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("blind", func(inst *ferrytest.Instance) {
		inst.Source = blindSource{inner: inst.Source, when: isChildrenSet}
	}))

	if len(c.lines) != 0 {
		t.Errorf("a reader that does not enumerate reported %q, want the case skipped", c.lines)
	}

	if !anyLineContains(c.logs, "case 5 skipped") {
		t.Errorf("the suite logged %q, want case 5's skip said out loud", c.logs)
	}
}

// TestDriverCase5ReportsChildrenThatCannotBeListed is case 5's other failure: a
// reader that answers [ferry.Enumerator] and then fails the enumeration has not
// declined the case, it has failed it.
//
// The misbehaviour is scoped to the one sequence address case 5 asks about,
// which is the only prefix in the suite that is listed.
func TestDriverCase5ReportsChildrenThatCannotBeListed(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("unlistable", func(inst *ferrytest.Instance) {
		inst.Source = unlistableSource{inner: inst.Source, at: probeList, err: errNoList}
	}))

	reportsExactly(t, c, "case 5: Children at /list failed")
}

// TestDriverCases8And9ReportASinkThatWillNotBindAContainer is one defect over
// the two cases that share an address set.
//
// Cases 8 and 9 each bind a sink to the one container address and then mint an
// address under it, so a sink refusing that set fails both. They are asserted
// together rather than pretended apart: a fixture that could tell them apart
// would have to be scoped by call order, which is a property of this file rather
// than of the driver.
func TestDriverCases8And9ReportASinkThatWillNotBindAContainer(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("unbindable-container", func(inst *ferrytest.Instance) {
		inst.Sink = refusingSink{inner: inst.Sink, when: isContainerSet, err: errUnreachable}
	}))

	reportsExactly(t, c, "case 8: Sink.Bind:", "case 9: Sink.Bind:")
}

// TestDriverCases8And9ReportAWriterThatCannotBeOpened is the same pair one step
// later: the binding is taken and the open behind it fails.
//
// Case 8 gives up, because a first write that never happened is no evidence
// about what a second open retains. Case 9 reports twice, and the second line is
// the suite reading a failed open as a refusal - which is what a driver author
// sees, so it is asserted rather than tidied away.
func TestDriverCases8And9ReportAWriterThatCannotBeOpened(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("unopenable-writer", func(inst *ferrytest.Instance) {
		inst.Sink = unopenableSink{inner: inst.Sink, when: isContainerSet, err: errNoWriter}
	}))

	reportsExactly(t, c,
		"case 8: opening a writer",
		"case 9: opening a writer",
		"case 9: the sink refused an address under a container",
	)
}

// TestDriverCase8ReportsAKeyFunctionThatKeptWhatItMinted is case 8, negative,
// and it is the defect the case exists for.
//
// Injectivity is a property of one write. A driver that keeps its minted keys on
// the binding refuses the second of two opens that each mint one half of a
// colliding pair, reporting a collision against an address no plane still holds.
// The misbehaviour is scoped to the second of the two addresses case 8 writes,
// which nothing else in the suite names.
func TestDriverCase8ReportsAKeyFunctionThatKeptWhatItMinted(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("retaining", func(inst *ferrytest.Instance) {
		inst.Sink = pickyWriteSink{inner: inst.Sink, refuse: refuseRetainedKey}
	}))

	reportsExactly(t, c, "case 8: the second open of one binding refused a write")
}

// TestDriverCase9ReportsASinkThatTreatsItsTableAsClosed is case 9, negative.
//
// A map key is minted from the value rather than from the type, so it was never
// going to be in the static set a Bind was handed, which is why core hands out a
// key function rather than a map. The misbehaviour is scoped to the one binding
// case 9 mints under, so the same key written under the read-side fixture's own
// binding is still taken.
func TestDriverCase9ReportsASinkThatTreatsItsTableAsClosed(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("closed-table", func(inst *ferrytest.Instance) {
		inst.Sink = pickyWriteSink{inner: inst.Sink, refuse: refuseDynamicKey}
	}))

	reportsExactly(t, c, "case 9: the sink refused an address under a container")
}

// TestDriverCase11ReportsAnArtefactThatCannotBeDumped is the third of case 11's
// failures, after a disagreeing rendering and contents that cannot be read.
//
// The artefact carries its own address, which no other fixture in the suite
// writes, so the plane below is broken for the golden row alone.
func TestDriverCase11ReportsAnArtefactThatCannotBeDumped(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, markPlane())

	reportsExactly(t, c, "case 11: dumping the")
}

// TestDriverCase11ReportsAPlaneWithNoSinkToDumpThrough is the golden row against
// ADR-0004's read-only plane.
//
// Every case that writes is silent for such a plane and case 11 is not, because
// a plane that pins a spelling has claimed a write it cannot perform. Case 1's
// refusal half is silent here too, for the same reason: a value that cannot be
// dumped cannot be refused at a dump either.
func TestDriverCase11ReportsAPlaneWithNoSinkToDumpThrough(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, ferrytest.Plane{
		Name:   "read-only-golden",
		Kinds:  []ferry.VKind{ferry.KindBool, ferry.KindNumber, ferry.KindString, ferry.KindBytes},
		Open:   func() ferrytest.Instance { return ferrytest.Instance{Source: ferrytest.Static(nil)} },
		Golden: []ferrytest.Artefact{ferrytest.Golden(markFixture{Mark: "m"}, markRendering)},
	})

	if !anyLineContains(c.lines, "a golden artefact has nothing to be dumped through") {
		t.Errorf("the suite reported %q, want the golden row's missing sink among them", c.lines)
	}

	for _, line := range c.lines {
		if !strings.Contains(line, "mints no sink") {
			t.Errorf("report = %q, want the missing sink and nothing else", line)
		}
	}
}

// TestDriverCase1AcceptsAPlaneThatRefusesWhatItDisclaims is case 1's second half
// from the side that passes.
//
// The plane below carries no null and says so, and its sink refuses a Null
// rather than mangling it into a zero value. That is exactly what the
// declaration asks of a flattening driver, so the whole suite is silent - which
// is the assertion, and the pair to the fixture that disclaims a null it does
// carry.
func TestDriverCase1AcceptsAPlaneThatRefusesWhatItDisclaims(t *testing.T) {
	c := &capture{}

	p := wrapPlane("honest-flat", func(inst *ferrytest.Instance) {
		inst.Sink = pickyWriteSink{inner: inst.Sink, refuse: refuseNull}
	})
	p.Kinds = []ferry.VKind{ferry.KindAbsent, ferry.KindBool, ferry.KindNumber, ferry.KindString, ferry.KindBytes}

	ferrytest.Driver(c, p)

	if len(c.lines) != 0 {
		t.Errorf("a plane that refuses the kind it disclaims reported %q, want nothing", c.lines)
	}
}

// TestDriverCase1AcceptsAPlaneThatRefusesAValueItExcepts is [ferrytest.Plane]'s
// Except from the side that passes, and it is the shape driver/yaml needs.
//
// The plane carries KindString and excepts the one string that is not valid
// UTF-8, which its sink then refuses. Three of the string row's four values
// still round trip, so the exception costs no coverage of the kind, and the
// suite is silent - which is the assertion.
func TestDriverCase1AcceptsAPlaneThatRefusesAValueItExcepts(t *testing.T) {
	c := &capture{}

	p := wrapPlane("honest-unicode", func(inst *ferrytest.Instance) {
		inst.Sink = pickyWriteSink{inner: inst.Sink, refuse: refuseNonUTF8}
	})
	p.Except = isNonUTF8

	ferrytest.Driver(c, p)

	if len(c.lines) != 0 {
		t.Errorf("a plane that refuses the value it excepts reported %q, want nothing", c.lines)
	}
}

// TestDriverCase1DemandsARefusalForAnExceptedValue is the same declaration
// broken, and it is what stops Except being a way to skip an inconvenient case.
//
// The plane below excepts a value and then takes it, which is a mangling rather
// than a refusal, and the suite reports it exactly as it reports a kind the
// plane never declared. One line and not four is the second half: the narrowing
// is per case, so the three strings this plane does carry are round-tripped
// rather than swept into the refusal half with the fourth.
func TestDriverCase1DemandsARefusalForAnExceptedValue(t *testing.T) {
	c := &capture{}

	p := ferrytest.MemPlane()
	p.Except = isNonUTF8

	ferrytest.Driver(c, p)

	reportsExactly(t, c, "string: case 2: the plane declares kind string and excepts this value")
}

// TestDriverCase12ReportsANullAContainerAddressWillNotTake is case 12, negative,
// and case 3's dump half with it.
//
// The two are one fixture because they are one dump: case 12 is the Dump mirror
// of case 3, and both write the nil optional section this plane refuses. A
// driver that declares a null and then will not take one at a container address
// has made the declaration a lie.
func TestDriverCase12ReportsANullAContainerAddressWillNotTake(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("null-refusing", func(inst *ferrytest.Instance) {
		inst.Sink = pickyWriteSink{inner: inst.Sink, refuse: refuseSectionNull}
	}))

	reportsExactly(t, c, "case 3: dumping the fixture", "case 12: dumping a nil list")
}

// TestDriverCase6ReportsAWriterThatCannotBeOpened is case 6 with nothing to
// observe: a lifecycle needs a writer, and this plane binds and then hands none
// back.
func TestDriverCase6ReportsAWriterThatCannotBeOpened(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("unopenable-leaf-writer", func(inst *ferrytest.Instance) {
		inst.Sink = unopenableSink{inner: inst.Sink, when: isLeafSet, err: errNoWriter}
	}))

	reportsExactly(t, c, "case 6: dumping one leaf failed")
}

// TestDriverCase6ReportsACloseThatFails is case 6's third clause, negative.
//
// Cleanup that fails is reported, because a temporary that could not be renamed
// is a dump that did not happen. This plane's writer holds a resource it cannot
// release, so the walk that should have succeeded did not, and the failure the
// case stages for itself never reaches the caller because the driver's own got
// there first. Both lines are case 6's, and both are the same defect seen from
// the two halves of the clause.
func TestDriverCase6ReportsACloseThatFails(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("unclosable", func(inst *ferrytest.Instance) {
		inst.Sink = unclosableSink{inner: inst.Sink, when: isLeafSet, err: errNoRelease}
	}))

	reportsExactly(t, c, "case 6: dumping one leaf failed", "case 6: a Close that failed")
}

// TestDriverSkipsSilentlyThroughAReporterThatCannotLog is ADR-0014's [T] held to
// its own claim: two methods, and a probe implements it in four lines.
//
// A skip is written where the reporter can carry one and is otherwise the
// silence it already was, which is why every case that skips also states its
// reason in its own documentation. The assertion is that a reporter with no log
// is still a reporter: the suite runs, and it reports nothing about a plane that
// passes.
func TestDriverSkipsSilentlyThroughAReporterThatCannotLog(t *testing.T) {
	q := &quiet{}

	ferrytest.Driver(q, ferrytest.MemPlane())

	if len(q.lines) != 0 {
		t.Errorf("the suite reported %q through a reporter that cannot log, want nothing", q.lines)
	}

	if q.helpers == 0 {
		t.Error("Driver never called Helper on a reporter that is only two methods")
	}
}

// quiet is [ferrytest.T] and nothing else: no Logf, which is the reporter a
// skip has nowhere to go through.
type quiet struct {
	lines   []string
	helpers int
}

func (q *quiet) Errorf(format string, args ...any) {
	q.lines = append(q.lines, fmt.Sprintf(format, args...))
}

func (q *quiet) Helper() { q.helpers++ }

var _ ferrytest.T = (*quiet)(nil)

// wrapPlane is the memory plane with one half replaced, which is the shape every
// fixture in this file takes: one defect, scoped, over the plane the rest of
// these tests already trust.
func wrapPlane(name string, wrap func(*ferrytest.Instance)) ferrytest.Plane {
	mem := ferrytest.MemPlane()
	p := mem

	p.Name = name
	p.Open = func() ferrytest.Instance {
		inst := mem.Open()
		wrap(&inst)

		return inst
	}

	return p
}

// isLeafSet is the address set case 2 binds and case 6's dump compiles to:
// exactly the one leaf.
func isLeafSet(addrs *ferry.AddressSet) bool {
	return addrs.Len() == 1 && addrs.Has(probeLeaf)
}

// isContainerSet is the address set cases 8 and 9 bind: exactly the one mapping.
func isContainerSet(addrs *ferry.AddressSet) bool {
	return addrs.Len() == 1 && addrs.Has(probeMap)
}

// isChildrenSet is the address set case 5 binds, which is one address wider than
// case 1's and one narrower than case 3's.
func isChildrenSet(addrs *ferry.AddressSet) bool {
	return addrs.Len() == 3 && addrs.Has(probeList) && addrs.Has(probeMap) && addrs.Has(probeLeaf)
}

// refuseRetainedKey is a key function that kept what it minted: the second of
// case 8's two writes is refused because the first one was taken.
func refuseRetainedKey(_ *ferry.AddressSet, addr ferry.Path, _ ferry.Value) error {
	if addr == probeMap.At("a_b") {
		return errNoWrite
	}

	return nil
}

// refuseDynamicKey is a sink treating its precomputed table as a closed set,
// scoped to the binding case 9 mints under.
func refuseDynamicKey(addrs *ferry.AddressSet, addr ferry.Path, _ ferry.Value) error {
	if isContainerSet(addrs) && addr == probeMap.At("k") {
		return errNoWrite
	}

	return nil
}

// refuseNull is a flat plane doing what its declaration promises: a kind it
// cannot carry is refused rather than quietly mangled.
func refuseNull(_ *ferry.AddressSet, _ ferry.Path, v ferry.Value) error {
	if v.Kind() == ferry.KindNull {
		return errNoWrite
	}

	return nil
}

// isNonUTF8 is a [ferrytest.Plane.Except] over the one value driver/yaml's
// format cannot spell, restated here so that this package's own tests do not
// depend on a driver module.
func isNonUTF8(v ferry.Value) bool {
	s, err := v.AsString()

	return err == nil && !utf8.ValidString(s)
}

// refuseNonUTF8 is a plane doing what excepting a value promises: the value is
// refused rather than quietly mangled, and every other string is taken.
func refuseNonUTF8(_ *ferry.AddressSet, _ ferry.Path, v ferry.Value) error {
	if isNonUTF8(v) {
		return errNoWrite
	}

	return nil
}

// refuseSectionNull is the opposite promise broken: a plane that declares a null
// and will not take one at a container address.
func refuseSectionNull(_ *ferry.AddressSet, addr ferry.Path, _ ferry.Value) error {
	if addr == probeSection {
		return errNoWrite
	}

	return nil
}

// markFixture is a golden row's value, carrying an address no other fixture in
// the suite writes so that a plane broken for it is broken for case 11 alone.
type markFixture struct {
	Mark string `ferry:"mark"`
}

// markRendering is what a plane holding a [markFixture] would spell it as.
const markRendering = "/mark=string(\"m\")\n"

// markPlane pins a golden artefact and then refuses the write it needs, which is
// a driver whose spelling cannot be produced at all.
func markPlane() ferrytest.Plane {
	p := wrapPlane("unspellable", func(inst *ferrytest.Instance) {
		inst.Sink = pickyWriteSink{inner: inst.Sink, refuse: refuseMark}
		inst.Contents = func() ([]byte, error) { return nil, nil }
	})

	p.Golden = []ferrytest.Artefact{ferrytest.Golden(markFixture{Mark: "m"}, markRendering)}

	return p
}

// refuseMark refuses the one address a [markFixture] lands at.
func refuseMark(_ *ferry.AddressSet, addr ferry.Path, _ ferry.Value) error {
	if addr == ferry.At("mark") {
		return errNoWrite
	}

	return nil
}

// refusingSource refuses at Bind, which is what a driver doing I/O against a
// plane nothing has reached yet does.
type refusingSource struct {
	inner ferry.Source
	when  func(*ferry.AddressSet) bool
	err   error
}

func (s refusingSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	if s.when(addrs) {
		return nil, s.err
	}

	return s.inner.Bind(addrs)
}

// unopenableSource binds and then fails where a driver legally may: inside the
// open, which is where ADR-0004 puts the I/O.
type unopenableSource struct {
	inner ferry.Source
	when  func(*ferry.AddressSet) bool
	err   error
}

func (s unopenableSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil || !s.when(addrs) {
		return open, err
	}

	return func(context.Context) (ferry.Reader, error) { return nil, s.err }, nil
}

// pickyReadSource fails one Get and answers from the plane everywhere else. Its
// reader keeps [ferry.Enumerator], because a source that stopped enumerating
// would break the cases that load a composite for a reason it is not about.
type pickyReadSource struct {
	inner ferry.Source
	at    ferry.Path
	err   error
}

func (s pickyReadSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	return bindThrough(s.inner, addrs, func(r ferry.Reader) ferry.Reader {
		return pickyReader{inner: r, at: s.at, err: s.err}
	})
}

type pickyReader struct {
	inner ferry.Reader
	at    ferry.Path
	err   error
}

func (r pickyReader) Get(ctx context.Context, addr ferry.Path) (ferry.Value, error) {
	if addr == r.at {
		return ferry.Value{}, r.err
	}

	return r.inner.Get(ctx, addr)
}

func (r pickyReader) Children(ctx context.Context, prefix ferry.Path) ([]ferry.Path, error) {
	return childrenThrough(ctx, r.inner, prefix)
}

// unlistableSource answers [ferry.Enumerator] and then fails the enumeration at
// one prefix, which is a case 5 failure rather than a case 5 skip.
type unlistableSource struct {
	inner ferry.Source
	at    ferry.Path
	err   error
}

func (s unlistableSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	return bindThrough(s.inner, addrs, func(r ferry.Reader) ferry.Reader {
		return unlistableReader{inner: r, at: s.at, err: s.err}
	})
}

type unlistableReader struct {
	inner ferry.Reader
	at    ferry.Path
	err   error
}

func (r unlistableReader) Get(ctx context.Context, addr ferry.Path) (ferry.Value, error) {
	return r.inner.Get(ctx, addr)
}

func (r unlistableReader) Children(ctx context.Context, prefix ferry.Path) ([]ferry.Path, error) {
	if prefix == r.at {
		return nil, r.err
	}

	return childrenThrough(ctx, r.inner, prefix)
}

// blindSource hands back a reader that does not enumerate, which ADR-0004 makes
// ordinary rather than wrong.
type blindSource struct {
	inner ferry.Source
	when  func(*ferry.AddressSet) bool
}

func (s blindSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	if !s.when(addrs) {
		return s.inner.Bind(addrs)
	}

	return bindThrough(s.inner, addrs, func(r ferry.Reader) ferry.Reader { return blindReader{inner: r} })
}

// blindReader is Get and nothing else, deliberately: a shell that kept Children
// would be answering for an interface its plane was asked not to have.
type blindReader struct{ inner ferry.Reader }

func (r blindReader) Get(ctx context.Context, addr ferry.Path) (ferry.Value, error) {
	return r.inner.Get(ctx, addr)
}

// bindThrough is the wrapping every read-side fixture above shares: bind the
// plane underneath, and wrap whatever its open produces.
func bindThrough(inner ferry.Source, addrs *ferry.AddressSet, wrap func(ferry.Reader) ferry.Reader,
) (ferry.OpenFunc, error) {
	open, err := inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return wrap(r), nil
	}, nil
}

// childrenThrough forwards an enumeration to the plane underneath, and says so
// where there is nothing under it to enumerate.
func childrenThrough(ctx context.Context, inner ferry.Reader, prefix ferry.Path) ([]ferry.Path, error) {
	e, ok := inner.(ferry.Enumerator)
	if !ok {
		return nil, errors.New("the plane underneath does not enumerate")
	}

	return e.Children(ctx, prefix)
}

// refusingSink is [refusingSource] on the write side.
type refusingSink struct {
	inner ferry.Sink
	when  func(*ferry.AddressSet) bool
	err   error
}

func (s refusingSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	if s.when(addrs) {
		return nil, s.err
	}

	return s.inner.Bind(addrs)
}

// unopenableSink binds and then hands back no writer.
type unopenableSink struct {
	inner ferry.Sink
	when  func(*ferry.AddressSet) bool
	err   error
}

func (s unopenableSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil || !s.when(addrs) {
		return open, err
	}

	return func(context.Context) (ferry.Writer, error) { return nil, s.err }, nil
}

// pickyWriteSink refuses the writes its own predicate names, and takes every
// other one. The address set is handed to the predicate as well as the address,
// because which binding a write arrived under is what scopes a fixture to one
// case.
type pickyWriteSink struct {
	inner  ferry.Sink
	refuse func(addrs *ferry.AddressSet, addr ferry.Path, v ferry.Value) error
}

func (s pickyWriteSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Writer, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return pickyWriter{inner: w, addrs: addrs, refuse: s.refuse}, nil
	}, nil
}

type pickyWriter struct {
	inner  ferry.Writer
	addrs  *ferry.AddressSet
	refuse func(addrs *ferry.AddressSet, addr ferry.Path, v ferry.Value) error
}

func (w pickyWriter) Set(ctx context.Context, addr ferry.Path, v ferry.Value) error {
	if err := w.refuse(w.addrs, addr, v); err != nil {
		return err
	}

	return w.inner.Set(ctx, addr, v)
}

// unclosableSink's writer holds a resource it cannot release, which is the
// temporary file that could not be renamed.
//
// It implements [ferry.Releaser] unconditionally, because whether a driver holds
// a resource is not a per-binding fact, and fails only under the binding the
// fixture is scoped to.
type unclosableSink struct {
	inner ferry.Sink
	when  func(*ferry.AddressSet) bool
	err   error
}

func (s unclosableSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Writer, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}

		if !s.when(addrs) {
			return unclosableWriter{inner: w}, nil
		}

		return unclosableWriter{inner: w, err: s.err}, nil
	}, nil
}

type unclosableWriter struct {
	inner ferry.Writer
	err   error
}

func (w unclosableWriter) Set(ctx context.Context, addr ferry.Path, v ferry.Value) error {
	return w.inner.Set(ctx, addr, v)
}

func (w unclosableWriter) Close() error { return w.err }

// reportsExactly holds a run to the lines it names and to no others, which is
// the half of a negative fixture that keeps it a single-defect fixture: a plane
// that reported one more case than it was broken for is a plane broken in two
// ways.
func reportsExactly(t *testing.T, c *capture, want ...string) {
	t.Helper()

	if len(c.lines) != len(want) {
		t.Fatalf("the suite reported %d lines %q, want %d: %q", len(c.lines), c.lines, len(want), want)
	}

	for _, w := range want {
		if !anyLineContains(c.lines, w) {
			t.Errorf("the suite reported %q, want %q among them", c.lines, w)
		}
	}
}
