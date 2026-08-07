package ferrytest

import (
	"context"
	"errors"
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
//
// Two of the four have a consequence the others do not. A shell dropping
// [ferry.Ensurer] turns every nil pointer in a dump into a refusal the driver
// never made (ADR-0016), and one dropping [ferry.Unsetter] turns a driver whose
// dumps replace into one whose dumps accumulate (ADR-0004). A third,
// [ferry.Preparer], turns a driver that refuses a folded pair with the plane
// untouched into one that refuses it half way through the dump. So the table is
// all thirty-two combinations.
//
// The inner writer of each row is built by [shells] itself, because the shells
// are exactly the thirty-two method sets this asserts over: writing bespoke
// writers beside them would be a second spelling of the same list, and the one
// that drifted would be the one nobody noticed.
func TestWrapWriterKeepsTheOptionalInterfaces(t *testing.T) {
	for i := range shells {
		t.Run(combinationName(i), optionalCase{bits: i}.assert)
	}
}

// optionalCase is one combination of the five optional interfaces, as the bits
// [caps.combination] reads.
type optionalCase struct{ bits int }

// assert reads the five optional interfaces off the wrapper, and holds each of
// them to the bit that built the writer underneath.
func (c optionalCase) assert(t *testing.T) {
	t.Helper()

	w := wrapWriter(combinationOf(c.bits), map[ferry.Path]ferry.Value{})

	_, commits := w.(ferry.Committer)
	_, releases := w.(ferry.Releaser)
	_, ensures := w.(ferry.Ensurer)
	_, unsets := w.(ferry.Unsetter)
	_, prepares := w.(ferry.Preparer)

	var none caps

	got := none.combination()

	for bit, has := range map[int]bool{
		hasCommit: commits, hasRelease: releases, hasEnsure: ensures, hasUnset: unsets, hasPrepare: prepares,
	} {
		if has {
			got |= bit
		}
	}

	if got != c.bits {
		t.Errorf("the wrapped writer carries %s, want %s", combinationName(got), combinationName(c.bits))
	}
}

// combinationOf is a writer whose method set is exactly one combination, built
// from the shell for it.
func combinationOf(bits int) ferry.Writer {
	return shells[bits](plainWriter{}, caps{
		commit:  commits{},
		release: releases{},
		ensure:  ensures{},
		unset:   unsets{},
		prepare: prepares{},
	})
}

// combinationName labels one combination, so that a failure names the
// capabilities rather than a number.
func combinationName(bits int) string {
	named := []struct {
		bit  int
		name string
	}{
		{hasCommit, "commits"},
		{hasRelease, "releases"},
		{hasEnsure, "ensures"},
		{hasUnset, "unsets"},
		{hasPrepare, "prepares"},
	}

	out := make([]string, 0, len(named))

	for _, n := range named {
		if bits&n.bit != 0 {
			out = append(out, n.name)
		}
	}

	if len(out) == 0 {
		return "none"
	}

	return strings.Join(out, " and ")
}

// The five capabilities as stubs, which is all the thirty-two combinations need:
// what a shell carries is a method set, and these are the methods.
type (
	plainWriter struct{}
	commits     struct{}
	releases    struct{}
	ensures     struct{}
	unsets      struct{}
	prepares    struct{}
)

func (plainWriter) Set(context.Context, ferry.LeafAddr, ferry.Value) error { return nil }

func (commits) Commit(context.Context) error { return nil }

func (releases) Close() error { return nil }

func (ensures) Ensure(context.Context, ferry.Container, ferry.Presence) error { return nil }

func (unsets) Unset(context.Context, ferry.CompositeAddr) error { return nil }

func (prepares) Prepare(context.Context, []ferry.Path) error { return nil }

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

// TestCodecRefusesToRunWithoutTheSeam is the suite's own precondition, asserted
// from inside because the seam is resolved once at package initialisation and
// no build a test can produce lacks it.
//
// The refusal is the alternative to a degraded run, which is why it is worth an
// assertion of its own: without the seam the per-registrant half cannot run at
// all, and case 1 falls back to guessing which arm a registration used - so a
// suite that carried on would report a disagreement ferry never consults rather
// than reporting nothing.
//
// It is not parallel, because it flips a package variable back.
func TestCodecRefusesToRunWithoutTheSeam(t *testing.T) {
	defer func(was bool) { coreWalkOK = was }(coreWalkOK)

	coreWalkOK = false

	c := &lines{}

	Codec(c, ferry.MustRegistry())

	if len(c.got) != 1 {
		t.Fatalf("a build with no seam reported %q, want exactly one line and no case run", c.got)
	}

	if !strings.Contains(c.got[0], "no reflect.Value-rooted walk") {
		t.Errorf("report = %q, want the missing seam named", c.got[0])
	}
}

// TestShellWriterCallsTheFrontAndAnswersForTheInner is the property [Driver]'s
// lifecycle case rests on.
//
// Which optional interfaces the shell carries has to be the driver's answer, or
// the case would be asserting about the wrapper. Which object the call reaches
// has to be the front, or a wrapper that counts a Commit would count none.
func TestShellWriterCallsTheFrontAndAnswersForTheInner(t *testing.T) {
	t.Run("the call reaches the front", theFrontTakesTheCall)
	t.Run("what it carries is the inner's answer", theInnerDecidesTheInterfaces)
}

// theFrontTakesTheCall is the second half of the shell's rule: which interfaces
// it has is the inner writer's answer, and which object each call goes to is the
// front's where the front has the method.
//
// Three of the five are asked, one per shape a call can take: one the front
// answers on its own, one that reaches the plane, and one handed the addresses
// a dump realised.
func theFrontTakesTheCall(t *testing.T) {
	counted := &countingWriter{}

	theFrontCommits(t, counted)
	theFrontForgets(t, counted)
	theFrontPrepares(t, counted)
}

func theFrontCommits(t *testing.T, counted *countingWriter) {
	t.Helper()

	c, ok := shellWriter(counted, combinationOf(hasCommit|hasRelease)).(ferry.Committer)
	if !ok {
		t.Fatal("the shell over a writer that commits is not a Committer")
	}

	if err := c.Commit(context.Background()); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if counted.commitCount != 1 {
		t.Errorf("the front was committed %d times, want once", counted.commitCount)
	}
}

func theFrontForgets(t *testing.T, counted *countingWriter) {
	t.Helper()

	u, ok := shellWriter(counted, combinationOf(hasUnset)).(ferry.Unsetter)
	if !ok {
		t.Fatal("the shell over a writer that can forget an address is not an Unsetter")
	}

	if err := u.Unset(context.Background(), ferry.CompositeAddr{}); err != nil {
		t.Fatalf("Unset: %v", err)
	}

	if counted.unsets != 1 {
		t.Errorf("the front was asked to forget %d times, want once", counted.unsets)
	}
}

func theFrontPrepares(t *testing.T, counted *countingWriter) {
	t.Helper()

	p, ok := shellWriter(counted, combinationOf(hasPrepare)).(ferry.Preparer)
	if !ok {
		t.Fatal("the shell over a writer that is handed a dump's realised addresses is not a Preparer")
	}

	if err := p.Prepare(context.Background(), nil); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if counted.prepares != 1 {
		t.Errorf("the front was asked to prepare %d times, want once", counted.prepares)
	}
}

// theInnerDecidesTheInterfaces is the first half, asked of the front that
// declares every optional interface: a shell answering for the front would carry
// all four over a writer that carries none.
func theInnerDecidesTheInterfaces(t *testing.T) {
	counted := &countingWriter{}

	if _, ok := shellWriter(counted, plainWriter{}).(ferry.Committer); ok {
		t.Error("the shell over a writer that does not commit is a Committer, so a suite asking a driver what " +
			"it implements would be asking the wrapper")
	}

	if _, ok := shellWriter(counted, plainWriter{}).(ferry.Ensurer); ok {
		t.Error("the shell over a writer that cannot spell a container at its own address is an Ensurer, so " +
			"core would hand it a write the driver never said it could take")
	}

	if _, ok := shellWriter(counted, plainWriter{}).(ferry.Unsetter); ok {
		t.Error("the shell over a writer that cannot forget an address is an Unsetter, so a driver whose " +
			"dumps accumulate would be reported as one whose dumps replace")
	}

	if _, ok := shellWriter(counted, plainWriter{}).(ferry.Preparer); ok {
		t.Error("the shell over a writer that asks for nothing is a Preparer, so a driver that finds a folded " +
			"pair at the colliding write would be reported as one that finds it before any of them")
	}
}

// countingWriter is a front that declares every optional interface, so that
// what the shell carries can only be the inner writer's answer.
type countingWriter struct {
	plainWriter

	commitCount int
	closes      int
	ensured     int
	unsets      int
	prepares    int
}

func (w *countingWriter) Commit(context.Context) error {
	w.commitCount++

	return nil
}

func (w *countingWriter) Close() error {
	w.closes++

	return nil
}

func (w *countingWriter) Ensure(context.Context, ferry.Container, ferry.Presence) error {
	w.ensured++

	return nil
}

func (w *countingWriter) Unset(context.Context, ferry.CompositeAddr) error {
	w.unsets++

	return nil
}

func (w *countingWriter) Prepare(context.Context, []ferry.Path) error {
	w.prepares++

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
	t.Run("the interface probe", theInterfaceProbeIsTotal)
	t.Run("the number probe", theNumberProbeIsTotal)
}

// theInterfaceProbeIsTotal drives the probe cases 2 and 3 rest on at the value
// neither of them hands it. Both drive the nil, so a policy that answered a null
// for every value would pass them while carrying nothing at all.
func theInterfaceProbeIsTotal(t *testing.T) {
	reg := probesIn(t, ifaceCodec())

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

	wrote, err := Record(context.Background(), ifaceHolder{Value: probeUDP{}}, ferry.WithRegistry(reg))
	if err != nil {
		t.Fatalf("dumping a non-nil interface through the probe codec: %v", err)
	}

	if got := wrote[probeAddrPath]; got != ferry.String("udp") {
		t.Errorf("the interface probe encoded a non-nil to %#v, want %#v", got, ferry.String("udp"))
	}
}

// theNumberProbeIsTotal drives case 5's probe at the values that case does not:
// its encode half at both arms, and the empty text its decode half refuses so
// that what case 5 observes is what core donated rather than what it was handed.
func theNumberProbeIsTotal(t *testing.T) {
	reg := probesIn(t, numberCodec())

	for in, want := range map[numeric]ferry.Value{"": ferry.Number("0"), probeNumber: ferry.Number(probeNumber)} {
		wrote, err := Record(context.Background(), numberHolder{Value: in}, ferry.WithRegistry(reg))
		if err != nil {
			t.Fatalf("dumping the number probe at %q: %v", in, err)
		}

		if got := wrote[probeNumberPath]; got != want {
			t.Errorf("the number probe encoded %q to %#v, want %#v", in, got, want)
		}
	}

	if _, err := ferry.Load[numberHolder](context.Background(),
		Static(map[ferry.Path]ferry.Value{probeNumberPath: ferry.String("")}),
		ferry.WithRegistry(reg)); err == nil {
		t.Error("the number probe accepted an empty text, so case 5 would pass over a codec that takes anything")
	}
}

// The codec suite's own failure arms.
//
// Each case builds the probe registry it is about, and every one of them asserts
// core's own machinery, so the only build where an arm below fires is a build
// where core is broken - which is exactly what they exist for and is not a build
// a test can produce. What is reachable is the registry an arm reads, so these
// hand one a registry broken in the way the arm is written to describe. A suite
// whose diagnosis is wrong is worse than no suite, and nothing else would say so.

// nullPolicy builds a null policy over [nullable] out of its two halves, which
// is where every way one can be broken lives.
func nullPolicy(load func() (nullable, error), isNull func(nullable) bool) ferry.Codec {
	return ferry.NullValue(
		ferry.StringValue(
			func(n nullable) (string, error) { return string(n), nil },
			func(s string) (nullable, error) { return nullable(s), nil }),
		load, isNull)
}

// TestCodecReportsANullPolicyThatDoesNotHold is case 7's report, at each of the
// three ways the policy's law breaks.
func TestCodecReportsANullPolicyThatDoesNotHold(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		codec ferry.Codec
		in    nullHolder
		want  string
	}{
		"a value it calls null is written as itself": {
			codec: nullPolicy(func() (nullable, error) { return "", nil }, func(nullable) bool { return false }),
			want:  "a null policy writes a null for the values it calls null",
		},
		// The null is the non-zero value here, because a policy whose load half
		// refuses the zero is refused at registration: the totality check
		// encodes the zero, sees a null and decodes it back through this half.
		"a null it cannot read back": {
			codec: nullPolicy(func() (nullable, error) { return "", errProbeGet }, func(n nullable) bool {
				return n == probeNullable
			}),
			in:   nullHolder{Value: probeNullable},
			want: "loading",
		},
		"a null it loads as a value it does not recognise": {
			codec: nullPolicy(func() (nullable, error) { return probeNullable, nil }, func(n nullable) bool {
				return n == ""
			}),
			want: "makes the round trip lie on the null path alone",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := &lines{}
			run := &codecRun{rep: c}

			run.nullRoundTrips(probesIn(t, tc.codec), tc.in, ferry.Null)

			onlyReport(t, c, tc.want)
		})
	}
}

// onlyReport holds a run to one line, saying what that line has to name.
func onlyReport(t *testing.T, c *lines, want string) {
	t.Helper()

	if len(c.got) != 1 {
		t.Fatalf("the run reported %q, want exactly one line", c.got)
	}

	if !strings.Contains(c.got[0], want) {
		t.Errorf("report = %q, want %q in it", c.got[0], want)
	}
}

// TestCodecReportsTwoKeysThatWereNotRefused is case 6's second half, and the
// registry it is handed is the one shape that reaches the arm: a key codec that
// is injective renders the two probe keys to two addresses, so the write core is
// asserted to refuse is one it has no reason to.
func TestCodecReportsTwoKeysThatWereNotRefused(t *testing.T) {
	t.Parallel()

	c := &lines{}
	run := &codecRun{rep: c}

	run.keyCollisionRefused(probesIn(t, ferry.StringKey(
		func(f folding) (string, error) { return string(f), nil },
		func(s string) (folding, error) { return folding(s), nil }).AsMapKey()))

	onlyReport(t, c, "one entry is lost")
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

			if _, _, ok := dumpAndOpen(d, filledFixture(), leafSet(t), caseContainerNo); ok {
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
//
// It comes from the compiler rather than from a literal, because the three
// address kinds are sealed and nothing outside core mints one (ADR-0016).
func leafSet(t *testing.T) *ferry.AddressSet {
	t.Helper()

	set, err := setOf[onlyLeaf](nil)
	if err != nil {
		t.Fatalf("compiling the suite's own fixture: %v", err)
	}

	return set
}

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

func (w brokenWriter) Set(ctx context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	if w.err != nil {
		return w.err
	}

	return w.inner.Set(ctx, addr, v)
}

// Ensure forwards the container-level write, so that this shell fails at the
// step it was built to fail at rather than at the capability it dropped.
func (w brokenWriter) Ensure(ctx context.Context, addr ferry.Container, p ferry.Presence) error {
	if w.err != nil {
		return w.err
	}

	return ensureThrough(ctx, w.inner, addr, p)
}

// Unset forwards for the reason Ensure does: the shell drops no capability the
// plane underneath has, so it fails at the step it was built to fail at.
func (w brokenWriter) Unset(ctx context.Context, addr ferry.CompositeAddr) error {
	if w.err != nil {
		return w.err
	}

	u, ok := w.inner.(ferry.Unsetter)
	if !ok {
		return errors.New("the plane underneath cannot forget an address")
	}

	return u.Unset(ctx, addr)
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
			run(&codecRun{rep: c, opts: []ferry.Option{ferry.WithRegistry(ferry.MustRegistry())}})

			if len(c.got) == 0 {
				t.Error("a case whose walk was refused reported nothing, which is a case that cannot fail")
			}
		})
	}
}

// probesIn is the suite's own fixtures in a registry of their own.
//
// A probe this package can no longer register is a change to core's rules rather
// than a failure of the test that names it, so it is reported as one line here.
func probesIn(t *testing.T, codecs ...ferry.Registration) *ferry.Registry {
	t.Helper()

	reg, err := ferry.NewRegistry(codecs...)
	if err != nil {
		t.Fatalf("the suite's own probes were refused: %v", err)
	}

	return reg
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
