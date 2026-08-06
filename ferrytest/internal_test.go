package ferrytest

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
)

// Two claims in this package have no behaviour to observe them through, so they
// are asserted from inside.
//
// This is the same exception the pure-value units in core get. The proof's
// three columns are no longer among them: [RoundTrip] reads the relation and
// the cases through [ferry.Dump] and [ferry.Load], and roundtrip_test.go
// asserts on what it reports, which is the seam this file promised to move them
// to. What is left is a key function whose only observable behaviour is also
// produced by keying on the address type, and an interface set whose two
// spellings core's own entry point cannot tell apart.

// TestStoreKeysAreRenderings is ADR-0003's first obligation, read off the key
// function itself.
//
// The obligation is about the key rather than about a behaviour, and the only
// behaviour it produces - that two spellings of one address are one slot - is
// also produced by keying on ferry.Path directly. What separates them is the
// claim the memory plane exists to make executable: the canonical rendering
// already identifies an address, so a plane with no format of its own needs
// nothing else to key by.
func TestStoreKeysAreRenderings(t *testing.T) {
	s := newMemStore()

	addrs := []ferry.Path{
		ferry.At("db", "host"),
		ferry.At("tags").Elem(0),
		ferry.At("odd/name"),
		ferry.At("Host"),
		ferry.At("host"),
	}

	for _, addr := range addrs {
		s.put(addr, ferry.String("x"))
	}

	want := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		want = append(want, addr.String())
	}

	got := make([]string, 0, len(s.entries))
	for k := range s.entries {
		got = append(got, k)
	}

	slices.Sort(got)
	slices.Sort(want)

	if !slices.Equal(got, want) {
		t.Errorf("store keys = %q, want the canonical renderings %q", got, want)
	}
}

// TestProofIsSealed asserts the seal that stops anything outside this package
// from being a [Proof].
//
// It is here because a method nothing ever calls is a method nothing ever
// checks is there: the seal has no behaviour, so no suite can observe it, and
// what it buys is the freedom for the suites to grow the methods they need
// without every proof outside this repository breaking.
func TestProofIsSealed(t *testing.T) {
	p, ok := Type("int", Eq[int], At(0, ferry.Number("0"))).(typeProof[int])
	if !ok {
		t.Fatal("Type did not build a typeProof")
	}

	p.proof()
}

// TestWrapWriterKeepsTheOptionalInterfaces is the recording sink's whole
// obligation, and it cannot be asserted through the entry point.
//
// A shell that implemented Commit and Close unconditionally and forwarded them
// to nothing would behave identically under [ferry.Dump] - a no-op Commit and a
// no-op Close return nil, which is what a writer without them produces anyway.
// What it would break is everything that asks a sink what it is: ADR-0014's
// driver conformance case 6 asserts that Commit runs only on success and that a
// Close failure appears in the reported error set, and against a wrapper that
// always answers yes it would be asserting about the wrapper.
func TestWrapWriterKeepsTheOptionalInterfaces(t *testing.T) {
	cases := []optionalCase{
		{name: "neither", inner: plainWriter{}},
		{name: "commits", inner: committingWriter{}, commits: true},
		{name: "releases", inner: releasingWriter{}, releases: true},
		{name: "both", inner: bothWriter{}, commits: true, releases: true},
	}

	for _, c := range cases {
		t.Run(c.name, c.assert)
	}
}

// optionalCase is one inner writer and the interface set its wrapper must have.
type optionalCase struct {
	name     string
	inner    ferry.Writer
	commits  bool
	releases bool
}

// assert reads the two optional interfaces off the wrapper.
func (c optionalCase) assert(t *testing.T) {
	t.Helper()

	w := wrapWriter(c.inner, map[ferry.Path]ferry.Value{})

	if _, ok := w.(ferry.Committer); ok != c.commits {
		t.Errorf("wrapped writer is a Committer = %v, want %v", ok, c.commits)
	}

	if _, ok := w.(ferry.Releaser); ok != c.releases {
		t.Errorf("wrapped writer is a Releaser = %v, want %v", ok, c.releases)
	}
}

// The four writers the table above wraps, which exist only to have the four
// combinations of the two optional interfaces.
type (
	plainWriter      struct{}
	committingWriter struct{ plainWriter }
	releasingWriter  struct{ plainWriter }
	bothWriter       struct{ plainWriter }
)

func (plainWriter) Set(context.Context, ferry.Path, ferry.Value) error { return nil }

func (committingWriter) Commit(context.Context) error { return nil }

func (releasingWriter) Close() error { return nil }

func (bothWriter) Commit(context.Context) error { return nil }

func (bothWriter) Close() error { return nil }

// TestEveryAdmittedKindHasARepresentative is the drift the panic exists to
// catch, asserted from inside because a table that agrees with itself has no
// observable behaviour when it stops agreeing.
//
// A kind is not a type, so [Complete] names one member per admitted kind. A
// kind added to the list and to no representative would be skipped by the one
// mechanism that would have reported it, which is the failure mode rather than
// a smaller version of it.
func TestEveryAdmittedKindHasARepresentative(t *testing.T) {
	t.Parallel()

	for _, k := range admittedKinds {
		if representative(k).Kind() != k {
			t.Errorf("the representative for kind %s is a %s", k, representative(k).Kind())
		}
	}
}

// TestAMissingRepresentativePanics is the same claim from the other side: the
// lookup takes its table as an argument so that the arm nothing can reach in
// the shipped tables still has a test.
func TestAMissingRepresentativePanics(t *testing.T) {
	t.Parallel()

	defer func() {
		got, ok := recover().(string)
		if !ok {
			t.Fatal("a kind with no representative did not panic with a string")
		}

		if !strings.Contains(got, reflect.Complex128.String()) {
			t.Errorf("the panic is %q, and does not name the kind that has no representative", got)
		}
	}()

	lookUpRepresentative(reflect.Complex128, map[reflect.Kind]reflect.Type{})
}

// TestCodecReportsAPanic is why every case in the codec suite is guarded.
//
// The two defects cases 2 and 3 exist for were panics inside core's own
// reflection, raised on a value the registrant's codec handled correctly. A
// suite that let one through would abort at case 2 and say nothing about the
// four cases after it, and a registrant would read a stack trace out of a
// package they have never opened.
func TestCodecReportsAPanic(t *testing.T) {
	c := &lines{}
	run := &codecRun{rep: c}

	run.guard(codecNilEncodeNo, func() { panic("the wrapper asserted a nil interface") })

	if len(c.got) != 1 {
		t.Fatalf("a panicking case reported %q, want exactly one line", c.got)
	}

	if !strings.Contains(c.got[0], "the wrapper asserted a nil interface") {
		t.Errorf("report = %q, want the panic's own value in it", c.got[0])
	}
}

// TestShellWriterCallsTheFrontAndAnswersForTheInner is the property [Driver]'s
// lifecycle case rests on.
//
// Which optional interfaces the shell carries has to be the driver's answer, or
// the case would be asserting about the wrapper. Which object the call reaches
// has to be the front, or a wrapper that counts a Commit would count none.
func TestShellWriterCallsTheFrontAndAnswersForTheInner(t *testing.T) {
	counted := &countingWriter{}

	shell := shellWriter(counted, bothWriter{})

	c, ok := shell.(ferry.Committer)
	if !ok {
		t.Fatal("the shell over a writer that commits is not a Committer")
	}

	if err := c.Commit(context.Background()); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if counted.commits != 1 {
		t.Errorf("the front was committed %d times, want once", counted.commits)
	}

	if _, ok := shellWriter(counted, plainWriter{}).(ferry.Committer); ok {
		t.Error("the shell over a writer that does not commit is a Committer, so a suite asking a driver what " +
			"it implements would be asking the wrapper")
	}
}

// countingWriter is a front that declares both optional interfaces, so that what
// the shell carries can only be the inner writer's answer.
type countingWriter struct {
	plainWriter

	commits int
	closes  int
}

func (w *countingWriter) Commit(context.Context) error {
	w.commits++

	return nil
}

func (w *countingWriter) Close() error {
	w.closes++

	return nil
}

// lines is [T] with nothing behind it, for the two claims above that have no
// caller-facing seam to be captured through.
type lines struct{ got []string }

func (l *lines) Errorf(format string, args ...any) {
	l.got = append(l.got, fmt.Sprintf(format, args...))
}

func (*lines) Helper() {}

// TestTheCodecProbesAreTotalInBothDirections is the codec suite's own fixtures
// held to what they claim.
//
// A probe that is wrong makes the case it belongs to say nothing, quietly, which
// is worse than the case failing: cases 2 to 7 are the only thing standing
// between a registrant and a defect in the registration wrapper, so the values
// they carry have to be the values the case is about.
func TestTheCodecProbesAreTotalInBothDirections(t *testing.T) {
	reg := probesIn(t, ifaceCodec(), numberCodec(), foldingCodec().AsMapKey())

	back, err := ferry.Load[ifaceHolder](context.Background(),
		Static(map[ferry.Path]ferry.Value{probeAddrPath: ferry.String("udp")}),
		ferry.WithRegistry(reg))
	if err != nil {
		t.Fatalf("loading a non-nil interface through the probe codec: %v", err)
	}

	if back.Value == nil || back.Value.Network() != "udp" {
		t.Errorf("the interface probe decoded to %#v, want a probeUDP: case 3 asserts the nil answer and this "+
			"is the answer it is distinguished from", back.Value)
	}
}

// TestTheFoldingKeysOwnStringIsInjective is the pair [Injective] rests on,
// asserted from inside because the type is this package's.
//
// The check resolves a key through ferry rather than through a format function a
// caller supplies, and the whole reason is that the two disagree: measured on
// this type, its own String() gives two texts where the registered codec writes
// one twice. If that stopped being true the fixture would be proving nothing.
func TestTheFoldingKeysOwnStringIsInjective(t *testing.T) {
	if probeMixed.String() == probeShouted.String() {
		t.Fatalf("the probe key's own String() folds %q and %q together, and the disagreement this fixture "+
			"demonstrates has gone", probeMixed, probeShouted)
	}

	got := Injective(probesIn(t, foldingCodec().AsMapKey()), probeMixed, probeShouted)
	if len(got) != 1 {
		t.Fatalf("Injective reported %q over a pair ferry writes one text for, want one line", got)
	}
}

// TestDumpAndOpenReportsAPlaneItCannotUse covers the three ways one case's
// fixture never reaches a reader, which no plane in the tests above is broken
// enough to reach and every real driver can be.
func TestDumpAndOpenReportsAPlaneItCannotUse(t *testing.T) {
	cases := map[string]struct {
		plane Plane
		want  string
	}{
		"the dump fails": {plane: brokenAt(errProbeSet, nil, nil), want: "dumping the fixture"},
		"the bind fails": {plane: brokenAt(nil, errProbeGet, nil), want: "Source.Bind"},
		"the open fails": {plane: brokenAt(nil, nil, errProbeGet), want: "opening a reader"},
		"no sink at all": {plane: Plane{Name: "read-only", Open: func() Instance { return Instance{} }}, want: ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := &lines{}
			d := &driverRun{rep: c, plane: tc.plane, carry: kindSet(nil)}

			if _, _, ok := dumpAndOpen(d, filledFixture(), leafSet(), caseContainerNo); ok {
				t.Fatal("a plane that cannot be used handed back a reader")
			}

			assertOneReport(t, c.got, tc.want)
		})
	}
}

// assertOneReport is the report a broken plane was expected to produce, or the
// silence a plane with no sink is entitled to.
func assertOneReport(t *testing.T, got []string, want string) {
	t.Helper()

	if want == "" {
		if len(got) != 0 {
			t.Errorf("reported %q, want nothing", got)
		}

		return
	}

	if len(got) != 1 {
		t.Fatalf("reported %q, want exactly one line", got)
	}

	if !strings.Contains(got[0], want) {
		t.Errorf("report = %q, want %q in it", got[0], want)
	}
}

// leafSet is the address set the fixture above is read through.
func leafSet() *ferry.AddressSet { return ferry.NewAddressSet(addrLeaf) }

// brokenAt is the memory plane with one of the three steps between a fixture and
// a reader made to fail.
func brokenAt(setErr, bindErr, openErr error) Plane {
	mem := MemPlane()
	p := mem

	p.Name = "broken"
	p.Open = func() Instance {
		inst := mem.Open()
		inst.Source = brokenSource{inner: inst.Source, bindErr: bindErr, openErr: openErr}
		inst.Sink = brokenSink{inner: inst.Sink, setErr: setErr}

		return inst
	}

	return p
}

type brokenSource struct {
	inner   ferry.Source
	bindErr error
	openErr error
}

func (s brokenSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	if s.bindErr != nil {
		return nil, s.bindErr
	}

	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		if s.openErr != nil {
			return nil, s.openErr
		}

		return open(ctx)
	}, nil
}

type brokenSink struct {
	inner  ferry.Sink
	setErr error
}

func (s brokenSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Writer, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return brokenWriter{inner: w, err: s.setErr}, nil
	}, nil
}

type brokenWriter struct {
	inner ferry.Writer
	err   error
}

func (w brokenWriter) Set(ctx context.Context, addr ferry.Path, v ferry.Value) error {
	if w.err != nil {
		return w.err
	}

	return w.inner.Set(ctx, addr, v)
}

// TestTheCodecCasesReportAWalkThatFailed is the arm every case has and no
// registry can reach: the probe is right, the walk refuses anyway.
//
// It is driven through an Option list the suite cannot honour, because that is
// the one way to make a correct probe fail from outside core. What it asserts is
// that a case which cannot run says so rather than passing, which is the
// difference between a suite and a suite-shaped silence.
func TestTheCodecCasesReportAWalkThatFailed(t *testing.T) {
	cases := map[string]func(*codecRun){
		"the nil interface, encoding":        (*codecRun).caseNilInterfaceEncode,
		"the nil interface, decoding":        (*codecRun).caseNilInterfaceDecode,
		"a constructor's own kind":           (*codecRun).caseDeclaredKind,
		"a codec that accepts what it emits": (*codecRun).caseAcceptsWhatItEmits,
		"a key codec's own text":             (*codecRun).caseKeyText,
		"a null policy":                      (*codecRun).caseNullPolicy,
	}

	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			c := &lines{}

			// A second registry, which core refuses: it resolves against exactly
			// one, and each case above supplies its own.
			run(&codecRun{rep: c, opts: []ferry.Option{ferry.WithRegistry(ferry.NewRegistry())}})

			if len(c.got) == 0 {
				t.Error("a case whose walk was refused reported nothing, which is a case that cannot fail")
			}
		})
	}
}

// probesIn is the suite's own fixtures in a registry of their own.
//
// [ferry.NewRegistry] refuses by panicking, having no error to return (ADR-0017),
// and a probe this package can no longer register is a change to core's rules
// rather than a failure of the test that names it - so it is reported as one
// line here rather than as a stack trace that aborts the run.
func probesIn(t *testing.T, codecs ...ferry.Codec) *ferry.Registry {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("the suite's own probes were refused: %v", r)
		}
	}()

	return ferry.NewRegistry(codecs...)
}

// TestProbeRegistryReportsARegistrationCoreRefuses is the arm that fires when
// the suite's own fixture stops being registrable, which is a change to core's
// registration rules rather than to any caller's code.
func TestProbeRegistryReportsARegistrationCoreRefuses(t *testing.T) {
	c := &lines{}
	run := &codecRun{rep: c}

	// A type core owns by kind admission: an entry core holds is not replaceable.
	refused := ferry.StringValue(
		func(string) (string, error) { return "", nil },
		func(string) (string, error) { return "", nil },
	)

	if _, ok := run.probeRegistry(codecTextNo, refused); ok {
		t.Fatal("a registration core refuses was taken")
	}

	assertOneReport(t, c.got, "the suite's own probe codec was refused")
}
