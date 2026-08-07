package ferrytest_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestDriverCase7HoldsARefusalToNamingBothAddresses is case 7 from three sides,
// and each plane below is broken in exactly one way.
//
// A driver that builds no plane key carries no injectivity obligation, and the
// suite cannot tell it apart from a flattening one from outside, so a Bind that
// accepts is not a failure. What is asserted is the shape of a Bind that
// refuses: it is the plane's own class, and it names the pair, because a refusal
// naming one address leaves the author with nothing to move.
func TestDriverCase7HoldsARefusalToNamingBothAddresses(t *testing.T) {
	cases := map[string]struct {
		err  error
		want string
	}{
		"naming both": {
			err:  fmt.Errorf("%w: /db_host and /db/host render alike", ferry.ErrPlane),
			want: "",
		},
		"naming one": {
			err:  fmt.Errorf("%w: /db_host is not a legal name", ferry.ErrPlane),
			want: "does not name both addresses",
		},
		"not a plane refusal": {
			err:  errors.New("/db_host and /db/host render alike"),
			want: "which is not a plane refusal",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := &capture{}

			ferrytest.Driver(c, collidingPlane(tc.err))

			assertCase7(t, c, tc.want)
		})
	}
}

// assertCase7 holds the run to reporting the expected refusal and nothing else.
func assertCase7(t *testing.T, c *capture, want string) {
	t.Helper()

	if want == "" {
		if len(c.lines) != 0 {
			t.Errorf("a refusal naming both addresses reported %q, want nothing", c.lines)
		}

		return
	}

	for _, line := range c.lines {
		if !strings.Contains(line, want) {
			t.Errorf("report = %q, want %q and nothing else", line, want)
		}
	}

	if !anyLineContains(c.lines, want) {
		t.Errorf("the suite reported %q, want %q among them", c.lines, want)
	}
}

// TestDriverCase7TakesAnAggregateRefusal is the shape a real flattening driver
// refuses in, and it is the one shape the case above cannot produce.
//
// An uppercase fold that maps every byte an environment variable name cannot
// hold to _ collapses three of case 7's pairs, not one: the separator pair, the
// hyphen pair and the case pair. A driver routing that refusal through
// [ferry.NewKeys] therefore hands back an aggregate, whose Error() is the
// one-line summary - one address per element, elided past three - so every
// element named both of its pair and the rendering the case read named neither.
//
// The count is asserted before the suite is run, because a fold that refused
// nothing would make the rest of this test pass by having nothing to report.
func TestDriverCase7TakesAnAggregateRefusal(t *testing.T) {
	err := foldedRefusal(t)

	if n := len(ferry.Elements(err)); n < 2 {
		t.Fatalf("the fold refused %d of case 7's pairs, want more than one, or this test asserts nothing", n)
	}

	c := &capture{}

	ferrytest.Driver(c, collidingPlane(err))

	if len(c.lines) != 0 {
		t.Errorf("an aggregate whose every element names both addresses reported %q, want nothing", c.lines)
	}
}

// foldedRefusal is what case 7's own address set does to a real environment key
// function, produced through core's helper rather than written out.
func foldedRefusal(t *testing.T) error {
	t.Helper()

	_, err := ferry.NewKeys(capturedSet[collidingCfg](t), "probe", foldedKey)

	return err
}

// collidingCfg is the type case 7's address set is compiled from: three pairs a
// flattening key function folds together.
//
// It is a struct rather than a literal set because the three address kinds are
// sealed and the compiler is the only thing that mints one (ADR-0016), so an
// address set can only be had by asking for a schema's own.
type collidingCfg struct {
	Flat   string          `ferry:"db_host"`
	DB     collidingNested `ferry:"db"`
	Dashed string          `ferry:"feature-flags"`
	Scored string          `ferry:"feature_flags"`
	Upper  string          `ferry:"Host"`
	Lower  string          `ferry:"host"`
}

// collidingNested is the structured half of the first pair: /db/host, which a
// separator join renders to the same key as /db_host.
type collidingNested struct {
	Host string `ferry:"host"`
}

// foldedKey uppercases each segment, maps every other byte to _, and joins with
// _, which is driver/env's key function in the smallest form that reproduces it.
func foldedKey(addr ferry.Path) (string, error) {
	var b strings.Builder

	for seg := range addr.Segments() {
		if b.Len() > 0 {
			b.WriteByte('_')
		}

		b.WriteString(strings.Map(foldedRune, seg.Text()))
	}

	return b.String(), nil
}

// foldedRune is the character transform: upper case where it is a letter, kept
// where it is a digit, and _ everywhere else.
func foldedRune(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}

	if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
		return r
	}

	return '_'
}

// TestDriverCase5ReportsChildrenThatAreNotTheElements is case 5, negative.
//
// Children returns addresses rather than names because an address carries its
// segment kind, and this plane answers with one the fixture never put there. The
// misbehaviour is scoped to the one prefix case 5 asks about, so nothing else in
// the suite changes.
func TestDriverCase5ReportsChildrenThatAreNotTheElements(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, invitingPlane())

	only := onlyLine(t, c)
	if !strings.Contains(only, "case 5") {
		t.Errorf("report = %q, want case 5 and only case 5", only)
	}
}

// TestDriverCase11ReportsContentsThatCannotBeRead is the read half of the golden
// artefact: a plane that pins a spelling and then fails to hand it back has not
// passed the case, it has failed to answer it.
func TestDriverCase11ReportsContentsThatCannotBeRead(t *testing.T) {
	c := &capture{}

	p := renderingPlane(goldenRendering)
	inner := p.Open
	p.Open = func() ferrytest.Instance {
		inst := inner()
		inst.Contents = func() ([]byte, error) { return nil, errUnreadable }

		return inst
	}

	ferrytest.Driver(c, p)

	only := onlyLine(t, c)
	if !strings.Contains(only, errUnreadable.Error()) {
		t.Errorf("report = %q, want the read failure named", only)
	}
}

// errUnreadable is a plane that cannot show what it holds.
var errUnreadable = errors.New("the contents could not be read")

// collidingPlane is the memory plane with one refusal added: a Bind handed the
// address set case 7 builds fails, and every other Bind succeeds.
//
// The scoping is what makes this a single-defect plane. Only case 7 hands a
// driver an address set holding /db_host, so every other case runs against the
// plane the rest of these tests already trust.
func collidingPlane(err error) ferrytest.Plane {
	mem := ferrytest.MemPlane()
	p := mem

	p.Name = "colliding"
	p.Open = func() ferrytest.Instance {
		inst := mem.Open()
		inst.Source = pickySource{inner: inst.Source, err: err}
		inst.Sink = pickySink{inner: inst.Sink, err: err}

		return inst
	}

	return p
}

// collidingProbe is the address case 7 builds its set around, and nothing else
// in the suite names.
var collidingProbe = ferry.At("db_host")

type pickySource struct {
	inner ferry.Source
	err   error
}

func (s pickySource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	if hasPath(addrs, collidingProbe) {
		return nil, s.err
	}

	return s.inner.Bind(addrs)
}

type pickySink struct {
	inner ferry.Sink
	err   error
}

func (s pickySink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	if hasPath(addrs, collidingProbe) {
		return nil, s.err
	}

	return s.inner.Bind(addrs)
}

// invitingPlane is the memory plane whose reader answers one extra child under
// the one prefix case 5 asks about.
func invitingPlane() ferrytest.Plane {
	mem := ferrytest.MemPlane()
	p := mem

	p.Name = "inviting"
	p.Open = func() ferrytest.Instance {
		inst := mem.Open()
		inst.Source = invitingSource{inner: inst.Source}

		return inst
	}

	return p
}

type invitingSource struct{ inner ferry.Source }

func (s invitingSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return invitingReader{inner: r}, nil
	}, nil
}

type invitingReader struct{ inner ferry.Reader }

func (r invitingReader) Get(ctx context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	return r.inner.Get(ctx, addr)
}

func (r invitingReader) Probe(ctx context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	return probeThrough(ctx, r.inner, addr)
}

func (r invitingReader) Children(ctx context.Context, addr ferry.CompositeAddr) ([]ferry.Segment, error) {
	got, err := childrenThrough(ctx, r.inner, addr)
	if err != nil || addr.Path() != ferry.At("list") {
		return got, err
	}

	return append(got, ferry.IndexSegment(99)), nil
}

// TestDriverAgainstAPlaneWithNoSink is ADR-0004's read-only plane, which is a
// description rather than a failure: environment variables have no honest Dump,
// so the env driver ships a Source and no Sink at all.
//
// Every case that writes is silent for it, and the one report is the round
// trip's, once per case, because a proof with nowhere to be dumped is a proof
// that cannot be discharged.
func TestDriverAgainstAPlaneWithNoSink(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, ferrytest.Plane{
		Name:  "read-only",
		Kinds: allKinds(),
		Open:  func() ferrytest.Instance { return ferrytest.Instance{Source: ferrytest.Static(nil)} },
	})

	if len(c.lines) == 0 {
		t.Fatal("a plane with no sink reported nothing, and a proof needs somewhere to be dumped")
	}

	for _, line := range c.lines {
		if !strings.Contains(line, "mints no sink") {
			t.Errorf("report = %q, want the missing sink and nothing else", line)
		}
	}
}

// The negative fixture for "a value at a container address" is gone, and the
// reason is that its defect no longer compiles.
//
// It used to wrap the plane in a reader answering a String at the address the
// fixture puts a map at, and the suite caught it. Under the sealed address model
// ferry.Reader.Get takes a ferry.LeafAddr, so a container's own address cannot
// be handed to it at all and no driver can answer a value there (ADR-0016). A
// fixture for a defect that cannot be written is a fixture that proves the
// compiler, not the suite.
//
// TestDriverCase3ReportsAnAbsentWhereANullWasStored is the other half of case 3,
// negative, and it is the row #136 tightened.
//
// ADR-0005 writes a Null at an empty composite's own address, so a driver that
// answers Absent there is reporting that the plane does not hold an address it
// does hold. That deletes an observation rather than renaming one: a LoadOver
// stops clearing the field and nothing says why. The misbehaviour is scoped to
// the two addresses the blanks fixture puts an empty composite at, so every
// other case runs against the plane the rest of these tests already trust.
func TestDriverCase3ReportsAnAbsentWhereANullWasStored(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, forgettingPlane())

	if len(c.lines) != 2 {
		t.Fatalf("report = %q, want one line per empty composite", c.lines)
	}

	for _, line := range c.lines {
		if !strings.Contains(line, "case 3") {
			t.Errorf("report = %q, want case 3 and only case 3", line)
		}
	}
}

// forgettingPlane is the memory plane whose reader answers Absent at the two
// container addresses the blanks fixture dumps an empty composite to.
func forgettingPlane() ferrytest.Plane {
	mem := ferrytest.MemPlane()
	p := mem

	p.Name = "forgetting"
	p.Open = func() ferrytest.Instance {
		inst := mem.Open()
		inst.Source = forgettingSource{inner: inst.Source}

		return inst
	}

	return p
}

type forgettingSource struct{ inner ferry.Source }

func (s forgettingSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return forgettingReader{inner: r}, nil
	}, nil
}

type forgettingReader struct{ inner ferry.Reader }

func (r forgettingReader) Get(ctx context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	return r.inner.Get(ctx, addr)
}

// Probe forgets the null at the two addresses the blanks fixture wrote one to.
//
// The container question moved off Get and onto Probe with the sealed address
// model (ADR-0016), so this is the same defect asked through the method that
// now owns it.
func (r forgettingReader) Probe(ctx context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	if addr.Path() == ferry.At("nillist") || addr.Path() == ferry.At("emptymap") {
		return ferry.SectionAbsent, nil
	}

	return probeThrough(ctx, r.inner, addr)
}

func (r forgettingReader) Children(ctx context.Context, addr ferry.CompositeAddr) ([]ferry.Segment, error) {
	return childrenThrough(ctx, r.inner, addr)
}

// allKinds is every kind a plane can declare, which is what a plane that carries
// the whole boundary says about itself.
func allKinds() []ferry.VKind {
	return []ferry.VKind{
		ferry.KindAbsent, ferry.KindNull, ferry.KindBool,
		ferry.KindNumber, ferry.KindString, ferry.KindBytes,
	}
}

// capturedSet compiles T and hands back the address set core built for it.
//
// It is how a package outside ferry holds one at all: the three address kinds
// are sealed, so a set cannot be written and has to be asked for (ADR-0016).
func capturedSet[T any](t *testing.T) *ferry.AddressSet {
	t.Helper()

	spy := &setCapture{}
	if _, err := ferry.Bind[T](spy); err != nil {
		t.Fatalf("compiling the fixture: %v", err)
	}

	return spy.set
}

// setCapture is a source that exists to be bound and never read.
type setCapture struct{ set *ferry.AddressSet }

func (s *setCapture) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	s.set = addrs

	return func(context.Context) (ferry.Reader, error) { return setCapture{}, nil }, nil
}

func (setCapture) Get(context.Context, ferry.LeafAddr) (ferry.Value, error) {
	return ferry.Value{}, nil
}

// TestDriverCase16ReportsADestinationThatDependsOnTheSchedule is the value half
// of case 16, against a plane that declares it tolerates overlapping calls and
// answers one address differently when the walk is allowed to overlap.
//
// The divergence is deliberate rather than raced. What a driver's real defect
// looks like is shared mutable state, and staging one here would make this
// package's own -race run report the very thing the case exists to catch out of
// a fixture rather than out of a driver; the observable half is that the two
// schedules disagree, and that is what is staged.
func TestDriverCase16ReportsADestinationThatDependsOnTheSchedule(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, overlappingPlane(overlapping(divergeAt), ferry.String("scheduled"), nil))

	assertCase16(t, c, "want the serial load's")
}

// TestDriverCase16ReportsALoadThatFailsOnlyWhenItOverlaps is the same half one
// answer over: the overlapped load does not disagree about the value, it does
// not produce one at all.
//
// A driver that reads correctly serially and fails under overlap is the shape a
// client with a connection pool of one has, and it is a failure of the promise
// rather than of the plane: the capability says the instance takes the overlap.
func TestDriverCase16ReportsALoadThatFailsOnlyWhenItOverlaps(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, overlappingPlane(overlapping(divergeAt), ferry.Value{}, errScheduled))

	assertCase16(t, c, "where the serial load of the same plane succeeded")
}

// TestDriverCase16ReportsAFailureThatDependsOnTheSchedule is the report half:
// one required address fails only where the walk was allowed to overlap, so the
// two schedules aggregate different text.
func TestDriverCase16ReportsAFailureThatDependsOnTheSchedule(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, overlappingPlane(overlapping(refuseAt), ferry.Value{}, errScheduled))

	assertCase16(t, c, "want the serial load's")
}

// TestDriverCase16ReportsAFailureThatDisappearsWhenItOverlaps is the report
// half's other direction, and it is the one worth having a case for.
//
// A report that shrinks under overlap is the failure a caller is never told
// about: the serial walk says four addresses are missing and the overlapped one
// says the load succeeded. A suite comparing only that both schedules failed
// would pass this plane.
func TestDriverCase16ReportsAFailureThatDisappearsWhenItOverlaps(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, overlappingPlane(overlapping(demandedAddrs()...), ferry.String("v"), nil))

	assertCase16(t, c, "where the serial load of the same plane reported")
}

// TestDriverCase16SkipsWhereTheSerialLoadFails is the case declining to measure
// a schedule it has no baseline for.
//
// The address fails on every schedule, so the serial load has no answer for a
// concurrent one to be held to. That is a skip and not a failure: a load that
// cannot succeed at all is cases 4 and 6's, and case 16 reporting it again
// would be one defect reported twice.
func TestDriverCase16SkipsWhereTheSerialLoadFails(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, overlappingPlane(always(divergeAt), ferry.Value{}, errScheduled))

	assertCase16Silent(t, c, "the serial load failed")
}

// TestDriverCase16SkipsWhereThePlaneHoldsTheRequiredAddresses is the report
// half declining for the opposite reason: this plane answers all four of them,
// so there is no failure for two schedules to aggregate differently.
//
// It is an ordinary plane rather than a broken one - a process environment that
// happens to hold those names is exactly this - so the half says so and stops
// instead of manufacturing a failure to compare.
func TestDriverCase16SkipsWhereThePlaneHoldsTheRequiredAddresses(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, overlappingPlane(always(demandedAddrs()...), ferry.String("v"), nil))

	assertCase16Silent(t, c, "a load of four required addresses this plane holds none of succeeded")
}

// TestDriverCase16SkipsWhereTheFixtureCannotBeDumped is the same decline one
// step earlier: the sink refuses the address set this case writes, so there is
// nothing on the plane for two schedules to read.
//
// The refusal is scoped to case 16's own set, so every other case runs against
// the plane the rest of these tests already trust.
func TestDriverCase16SkipsWhereTheFixtureCannotBeDumped(t *testing.T) {
	c := &capture{}

	p := overlappingPlane(overlapping(), ferry.Value{}, nil)
	inner := p.Open

	p.Open = func() ferrytest.Instance {
		inst := inner()
		inst.Sink = refusingSink{inner: inst.Sink, when: isSpreadSet, err: errUnreachable}

		return inst
	}

	ferrytest.Driver(c, p)

	assertCase16Silent(t, c, "the fixture could not be dumped")
}

// TestDriverCase16IsSilentForAPlaneWithNoSink is the read-only plane: there is
// nothing to write the fixture with, so the value half has no plane to compare
// two schedules over and the report half runs on its own.
//
// A plane with no sink is ADR-0004's own case rather than a defect, so this
// case says nothing about it at all.
func TestDriverCase16IsSilentForAPlaneWithNoSink(t *testing.T) {
	c := &capture{}

	p := overlappingPlane(overlapping(), ferry.Value{}, nil)
	inner := p.Open

	p.Open = func() ferrytest.Instance {
		inst := inner()
		inst.Sink = nil

		return inst
	}

	ferrytest.Driver(c, p)

	assertNoCase16(t, c)
}

// TestDriverCase16IsSilentForAPlaneWithNoSource is the other half missing: a
// plane nothing can be read out of has no schedule to compare, and the cases
// that own a plane with no read half are the ones that report it.
func TestDriverCase16IsSilentForAPlaneWithNoSource(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, ferrytest.Plane{
		Name:  "write-only",
		Kinds: allKinds(),
		Open:  func() ferrytest.Instance { return ferrytest.Instance{Sink: ferrytest.MemPlane().Open().Sink} },
	})

	assertNoCase16(t, c)
}

// assertCase16 holds the run to reporting case 16 once per budget the suite
// compares, and nothing else.
func assertCase16(t *testing.T, c *capture, want string) {
	t.Helper()

	if len(c.lines) != case16Budgets {
		t.Fatalf("report = %q, want one line per budget the case compares", c.lines)
	}

	for _, line := range c.lines {
		if !strings.Contains(line, "case 16") {
			t.Errorf("report = %q, want case 16 and only case 16", line)
		}

		if !strings.Contains(line, want) {
			t.Errorf("report = %q, want %q in it", line, want)
		}
	}
}

// assertCase16Silent holds a run to skipping case 16 for the stated reason and
// failing nothing, which is what a case that declined to measure looks like.
func assertCase16Silent(t *testing.T, c *capture, why string) {
	t.Helper()

	assertNoCase16(t, c)

	if !anyLineContains(c.logs, "case 16 skipped: "+why) {
		t.Errorf("the suite logged %q, want case 16 skipped for %q", c.logs, why)
	}
}

// assertNoCase16 is the whole of what a plane this case does not apply to owes:
// no failure carrying its number.
func assertNoCase16(t *testing.T, c *capture) {
	t.Helper()

	for _, line := range c.lines {
		if strings.Contains(line, "case 16") {
			t.Errorf("report = %q, want nothing from case 16", line)
		}
	}
}

// case16Budgets is how many budgets case 16 holds a plane to, and therefore how
// many lines one address answered wrongly produces.
const case16Budgets = 3

// The addresses the planes below misbehave at, one set in each of case 16's
// fixtures, so that a plane wrong about the destination is not also wrong about
// the report.
var (
	divergeAt = ferry.At("under", "five")
	refuseAt  = ferry.At("needone")
)

// demandedAddrs is every address of the fixture case 16 reads its report half
// out of, which is what a plane has to answer all of for the overlapped load to
// succeed where the serial one failed.
func demandedAddrs() []ferry.Path {
	return []ferry.Path{ferry.At("needone"), ferry.At("needtwo"), ferry.At("needthree"), ferry.At("needfour")}
}

// isSpreadSet is the address set case 16's value half binds, named by the one
// address no other fixture in the suite holds.
func isSpreadSet(addrs *ferry.AddressSet) bool { return hasPath(addrs, divergeAt) }

// errScheduled is what the refusing planes fail with.
var errScheduled = errors.New("this address is only unreadable when the walk overlaps")

// scheduled is when a plane answers out of the staging rather than out of the
// plane underneath: an address, and the schedule the walk is running under.
//
// The schedule is read off the context rather than observed, which is what
// makes every fixture here deterministic. ferry.ConcurrencyBudget is the number
// the caller granted, so a plane can answer one way where core is allowed to
// overlap and another where it is not, with no goroutine of its own and nothing
// for the race detector to find.
type scheduled func(ctx context.Context, at ferry.Path) bool

// overlapping matches these addresses on a walk core was allowed to overlap,
// and nothing on a serial one.
func overlapping(at ...ferry.Path) scheduled {
	return func(ctx context.Context, p ferry.Path) bool {
		return ferry.ConcurrencyBudget(ctx) > 1 && slices.Contains(at, p)
	}
}

// always matches these addresses on every schedule, which is a plane that is
// broken rather than one that is broken by overlapping.
func always(at ...ferry.Path) scheduled {
	return func(_ context.Context, p ferry.Path) bool { return slices.Contains(at, p) }
}

// overlappingPlane is the memory plane whose reader declares the concurrency
// capability and answers the addresses when names out of the staging.
func overlappingPlane(when scheduled, v ferry.Value, err error) ferrytest.Plane {
	mem := ferrytest.MemPlane()
	p := mem

	p.Name = "overlapping"
	p.Open = func() ferrytest.Instance {
		inst := mem.Open()
		inst.Source = overlappingSource{inner: inst.Source, when: when, value: v, err: err}

		return inst
	}

	return p
}

type overlappingSource struct {
	inner ferry.Source
	when  scheduled
	value ferry.Value
	err   error
}

func (s overlappingSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return overlappingReader{inner: r, when: s.when, value: s.value, err: s.err}, nil
	}, nil
}

// overlappingReader declares the capability and is not equivalent under it.
type overlappingReader struct {
	inner ferry.Reader
	when  scheduled
	value ferry.Value
	err   error
}

// MaxConcurrent is the declaration the case gates on: no bound of this reader's
// own, so the caller's budget stands alone.
func (overlappingReader) MaxConcurrent() int { return 0 }

// Get answers out of the plane, except where this plane was staged to answer
// out of itself instead.
func (r overlappingReader) Get(ctx context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	if r.when(ctx, addr.Path()) {
		return r.value, r.err
	}

	return r.inner.Get(ctx, addr)
}

func (r overlappingReader) Probe(ctx context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	return probeThrough(ctx, r.inner, addr)
}

func (r overlappingReader) Children(ctx context.Context, addr ferry.CompositeAddr) ([]ferry.Segment, error) {
	return childrenThrough(ctx, r.inner, addr)
}
