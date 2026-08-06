package ferrytest_test

import (
	"context"
	"errors"
	"fmt"
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
