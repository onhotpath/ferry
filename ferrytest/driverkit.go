package ferrytest

import (
	"context"
	"errors"

	"github.com/onhotpath/ferry"
)

// The case numbers, so that a report names the case in ADR-0014's list rather
// than a position in this file, and so that renumbering the list is one edit.
//
// Cases 1 and 3 have no constant: case 1 reports through [RoundTrip], which
// labels by proof and case, and case 3 is the only one that reports from more
// than one method.
const (
	caseBindNo          = 2
	caseContainerNo     = 3
	caseGetErrorNo      = 4
	caseChildrenNo      = 5
	caseLifecycleNo     = 6
	caseInjectiveNo     = 7
	caseRetentionNo     = 8
	caseDynamicNo       = 9
	casePerRequestNo    = 10
	caseGoldenNo        = 11
	caseNullNo          = 12
	caseForeignNo       = 13
	caseConcurrentNo    = 14
	caseSecondDumpNo    = 15
	caseEquivalentNo    = 16
	caseReplaceNo       = 17
	casePreparedNo      = 18
	caseUnforgettableNo = 19
	caseOrderingNo      = 20
	caseOmissionNo      = 21
	caseNamedNo         = 22
)

// The suite's own fixtures, and their addresses.
//
// They are ordinary annotated structs, so what the cases exercise is the same
// compiler and the same walk a driver meets in production, and the addresses
// below are written out rather than derived: an address a test computes the same
// way the code under test does is an address that agrees with itself.
type (
	// filled is a populated list, a populated map and a leaf: the read-side
	// fixture, carrying no Null so that a plane with no null can be asked it.
	filled struct {
		List []string          `ferry:"list"`
		Map  map[string]string `ferry:"map"`
		Leaf string            `ferry:"leaf"`
	}

	// blanks is a nil composite, an empty composite and a nil optional section,
	// which is case 12's list and the second half of case 3's.
	blanks struct {
		NilList  []string          `ferry:"nillist"`
		EmptyMap map[string]string `ferry:"emptymap"`
		Section  *blankSection     `ferry:"section"`
	}

	// blankSection is what makes blanks carry an optional section: a pointer to
	// a composite is the shape that can be nil, and a plain struct is not.
	blankSection struct {
		Name string `ferry:"name"`
	}

	// justSection is one optional section and nothing else, which is the
	// container case 13 asks about.
	justSection struct {
		Section *blankSection `ferry:"section"`
	}

	// neighbour is one leaf of another schema whose name begins with that
	// section's, which a flat plane spells inside the section's own key space
	// and a tree plane spells beside it. It is no address of justSection either
	// way, so it says nothing about the section.
	neighbour struct {
		Beside string `ferry:"section_x"`
	}

	// justMap is the fixture for the two cases about a value-minted address:
	// its one member's address does not exist until there is a value, so a
	// dump of it is the only way to reach one.
	justMap struct {
		Map map[string]string `ferry:"map"`
	}

	// foldedPair is case 18's fixture: one leaf whose address the type
	// determines, and a mapping whose two keys a flattening plane renders to
	// one plane key.
	//
	// The leaf is the observable half. It is not the address that collides and
	// it is written by the same dump, so what the plane holds at it afterwards
	// is what says whether the refusal arrived before any of the writes or
	// among them. Its address is [onlyLeaf]'s, which is how the case reads it
	// back without asking a plane that cannot list to enumerate anything.
	foldedPair struct {
		Leaf string            `ferry:"leaf"`
		Map  map[string]string `ferry:"map"`
	}

	// onlyLeaf is the fixture for the cases that must not depend on
	// [ferry.Enumerator], because loading a slice or a map from a source that
	// cannot list is an error for a reason those cases are not about.
	onlyLeaf struct {
		Leaf string `ferry:"leaf"`
	}

	// spread is case 16's fixture: enough leaves at two depths for a walk to
	// have something to overlap, and no composite anywhere, so a plane that
	// cannot enumerate is asked nothing it cannot answer.
	//
	// It is comparable, which is what lets the case compare two destinations
	// without a relation: every member is a string, and the equivalence being
	// asserted is equality of the whole value rather than of a field.
	spread struct {
		One   string      `ferry:"one"`
		Two   string      `ferry:"two"`
		Three string      `ferry:"three"`
		Four  string      `ferry:"four"`
		Under spreadUnder `ferry:"under"`
	}

	// spreadUnder is the second depth, which is what makes the fixture a walk
	// that enters a container inside a container rather than one flat list.
	spreadUnder struct {
		Five string `ferry:"five"`
		Six  string `ferry:"six"`
	}

	// dozen is case 20's fixture: a sequence long enough for the two orders to
	// disagree, which twelve members is the smallest round number that does.
	//
	// Below ten positions the rendering and the segments sort alike, so a
	// sequence of two - which is what every other fixture here carries - cannot
	// tell a driver that orders by text from one that orders by position.
	dozen struct {
		List []string `ferry:"list"`
	}

	// omitted is case 21's fixture: one leaf a dump writes and one the dump is
	// silent at, because it carries omitzero and holds its zero value.
	//
	// It is leaf-only, so a plane that cannot list is asked nothing it cannot
	// answer, and the silent address is a string rather than a composite so that
	// what the plane holds there is read back by an ordinary load.
	omitted struct {
		Leaf string `ferry:"leaf"`
		Gone string `ferry:"gone,omitzero"`
	}

	// demanded is case 16's other fixture: several addresses a plane that holds
	// nothing cannot answer, so a load of it reports one failure per member.
	//
	// It is the report half of the equivalence, and it needs no sink at all: a
	// fresh plane is empty by construction, which is what makes every one of
	// these required addresses missing.
	// Its addresses are its own and are no address of [spread], so a plane can
	// be wrong about one half of the case without being wrong about the other.
	demanded struct {
		One   string `ferry:"needone,required"`
		Two   string `ferry:"needtwo,required"`
		Three string `ferry:"needthree,required"`
		Four  string `ferry:"needfour,required"`
	}
)

// spreadFixture is case 16's populated value, minted per use so that no load
// can be handed a value another load has already been compared against.
func spreadFixture() spread {
	return spread{
		One:   "1",
		Two:   "2",
		Three: "3",
		Four:  "4",
		Under: spreadUnder{Five: "5", Six: "6"},
	}
}

// dozenFixture is twelve members whose values name their own positions, minted
// per use like every other fixture here.
//
// The text says where the member belongs, so a report of what came back reads as
// the order the plane answered in rather than as twelve strings that have to be
// counted.
func dozenFixture() dozen {
	return dozen{List: []string{
		"at0", "at1", "at2", "at3", "at4", "at5",
		"at6", "at7", "at8", "at9", "at10", "at11",
	}}
}

// omittedFixture is case 21's value: the written leaf, and the omitzero field at
// its zero value, which is what makes the dump silent at the second address.
func omittedFixture() omitted {
	return omitted{Leaf: fixtureLeaf}
}

// omittedSeed is what the load of case 21's fixture starts from at the address
// the dump was silent at.
//
// Absence does not write, so a plane that was told nothing there leaves this
// text exactly where it was, and a plane that stored something instead reports
// that something.
const omittedSeed = "seeded"

// fixtureLeaf is the value at the one leaf address the fixtures share.
const fixtureLeaf = "x"

// fixtureKey is the one map key the fixtures carry, and the one a dynamic
// address is minted at.
const fixtureKey = "k"

// The two map keys case 8 mints, one per open. They are one pair a flattening
// driver folds together and two distinct addresses everywhere else.
const (
	retentionFirst  = "a-b"
	retentionSecond = "a_b"
)

// dynamicKeys is what case 9 mints, in one dump: its own key and the first of
// case 8's two.
//
// Case 8's first write is here and its second is not, and the asymmetry is the
// hole this closes rather than an oversight. A refused second write fails case 8
// out loud, because that is the retention the case is about; a refused first
// write only ends it, so without this line a sink that cannot spell that key was
// asked by nothing at all.
var dynamicKeys = []string{fixtureKey, retentionFirst}

// The fixtures' addresses.
var (
	addrList     = ferry.At("list")
	addrMap      = ferry.At("map")
	addrLeaf     = ferry.At("leaf")
	addrNilList  = ferry.At("nillist")
	addrEmptyMap = ferry.At("emptymap")
	addrSection  = ferry.At("section")
	addrGone     = ferry.At("gone")
)

// setOf compiles T and hands back the address set core built for it, typed.
//
// It is how this package holds a typed address at all. The three address kinds
// are sealed and the schema compiler is the only thing that mints one
// (ADR-0016), so a case needing a [ferry.LeafAddr] asks the compiler for the
// schema's own rather than writing one, which also means the suite can never
// hand a driver an address the compiler would not have.
func setOf[T any](opts []ferry.Option) (*ferry.AddressSet, error) {
	spy := &setSpy{}
	if _, err := ferry.Bind[T](spy, opts...); err != nil {
		return nil, err
	}

	return spy.set, nil
}

// setSpy is a source that exists to be bound and never read.
type setSpy struct{ set *ferry.AddressSet }

func (s *setSpy) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	s.set = addrs

	return func(context.Context) (ferry.Reader, error) { return setSpy{}, nil }, nil
}

func (setSpy) Get(context.Context, ferry.LeafAddr) (ferry.Value, error) { return ferry.Value{}, nil }

// sectionIn and compositeIn find one member of a set by address and by kind,
// which is the question the typed set exists to answer: /db as a section and
// /db as a composite are different addresses.
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

// filledFixture is one populated value, minted per use so that no case can hand
// another case a value it has already mutated.
func filledFixture() filled {
	return filled{
		List: []string{"a", "b"},
		Map:  map[string]string{fixtureKey: "v"},
		Leaf: fixtureLeaf,
	}
}

// blanksFixture is the three shapes that write a Null at their own address: a
// nil slice, an empty map and a nil pointer to a section.
func blanksFixture() blanks {
	return blanks{EmptyMap: map[string]string{}}
}

// secondFixture is [filledFixture] again with a different value everywhere, and
// it is what case 15 saves the second time.
//
// Every address is the address the first dump wrote, and no address is one it
// did not: a re-save is a different value at the same places, and a second dump
// that also changed the shape would be asking what a sink does about an address
// the new value no longer has, which is a different question.
func secondFixture() filled {
	return filled{
		List: []string{"c", "d"},
		Map:  map[string]string{fixtureKey: "w"},
		Leaf: secondLeaf,
	}
}

// secondLeaf is the value the second dump leaves at the fixture's leaf, and it
// is what the plane must hold afterwards.
const secondLeaf = "y"

// shrunkFixture is [filledFixture] with a shorter list, and it is what case 17
// saves the second time.
//
// Only the shape moves. The map and the leaf hold what the first dump wrote, so
// a plane that fails the case fails it for the one reason the case is about: a
// position the second dump did not write, still there afterwards.
func shrunkFixture() filled {
	return filled{
		List: []string{"z"},
		Map:  map[string]string{fixtureKey: "v"},
		Leaf: fixtureLeaf,
	}
}

// foldedFixture is what case 18 dumps: the leaf, and the two map keys case 8
// mints one per open.
//
// Both keys in one value is what makes it the case's fixture rather than case
// 8's. A plane that renders them to one key holds one address where the value
// has two, which is the refusal the case is about; a plane that keeps them apart
// takes the dump, and the case says so and stops.
func foldedFixture() foldedPair {
	return foldedPair{
		Leaf: fixtureLeaf,
		Map:  map[string]string{retentionFirst: "1", retentionSecond: "2"},
	}
}

// driverFixturesCompile resolves the caller's Option list against every fixture
// the suite dumps, which is where an Option that cannot be honoured is reported.
func driverFixturesCompile(opts []ferry.Option) error {
	return errors.Join(
		ferry.Compile[filled](opts...),
		ferry.Compile[blanks](opts...),
		ferry.Compile[onlyLeaf](opts...),
		ferry.Compile[foldedPair](opts...),
		ferry.Compile[spread](opts...),
		ferry.Compile[demanded](opts...),
		ferry.Compile[justSection](opts...),
		ferry.Compile[neighbour](opts...),
		ferry.Compile[dozen](opts...),
		ferry.Compile[omitted](opts...),
	)
}

// collidingPairs are addresses a flattening key function folds together, and
// which no plane may hold both of under one key.
//
// One rule covers all three shapes: a separator collision, a character a plane
// cannot name being mapped onto one it can, and case folding. They are the same
// failure - a many-to-one map out of the address set - which is why ADR-0003
// states the obligation once.
var collidingPairs = [][2]ferry.Path{
	{ferry.At("db_host"), ferry.At("db", "host")},
	{ferry.At("feature-flags"), ferry.At("feature_flags")},
	{ferry.At("Host"), ferry.At("host")},
}

// colliding is the type those pairs are the addresses of.
//
// It is a struct rather than a hand-built address set because the address kinds
// are sealed and only the compiler mints one (ADR-0016), and because a set the
// compiler produced is one a driver could actually be handed.
type colliding struct {
	Flat   string         `ferry:"db_host"`
	DB     collidingUnder `ferry:"db"`
	Dashed string         `ferry:"feature-flags"`
	Scored string         `ferry:"feature_flags"`
	Upper  string         `ferry:"Host"`
	Lower  string         `ferry:"host"`
}

// collidingUnder is the subtree whose leaf address a flat key function folds
// onto the sibling spelled with the separator in it.
type collidingUnder struct {
	Host string `ferry:"host"`
}

// kindSet is [Plane.Kinds] as a set. A plane declaring nothing can express
// nothing, which is a description that is wrong rather than a plane that is, and
// case 1 reports it as a refusal missing for every proof.
func kindSet(kinds []ferry.VKind) map[ferry.VKind]bool {
	out := make(map[ferry.VKind]bool, len(kinds))
	for _, k := range kinds {
		out[k] = true
	}

	return out
}

// dumpAndOpen dumps one fixture into a fresh instance and hands back a reader
// over the same contents, together with the context that reader was opened
// under and must be read through.
//
// It is a function rather than a method because the fixture's type is a
// parameter, and a method cannot take one. The instance is fresh per call, which
// is ADR-0014's fresh-destination rule: a plane shared across cases is the
// defect that hides a broken second walk.
//
// The context comes back with the reader rather than being rebuilt by the
// caller, because for a per-request plane it is the instance's contents
// (ADR-0012) and a second one built from a second instance would be a second
// plane: the fixture would have been dumped into one and read out of the other.
func dumpAndOpen[T any](d *driverRun, v T, set *ferry.AddressSet, n int) (context.Context, ferry.Reader, bool) {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Sink == nil || inst.Source == nil {
		return nil, nil, false
	}

	ctx := inst.ctx()

	if err := ferry.Dump(ctx, v, inst.Sink, d.opts...); err != nil {
		d.fail(n, "dumping the fixture: "+err.Error())

		return nil, nil, false
	}

	return d.openOver(ctx, inst, set, n)
}

// dumpAndOpenQuiet is [dumpAndOpen] for a case that is not the owner of the
// dump it needs.
//
// A sink refusing to write a null at a container address is case 12's failure
// and case 3 already reports the read half of it, so a third case reporting the
// same refusal is one mistake reported three times, which is the misattribution
// the case-3 fixture ordering was fixed for. Everything after the dump is still
// reported here, because from there on the failure is this case's own.
func dumpAndOpenQuiet[T any](d *driverRun, v T, set *ferry.AddressSet, n int) (context.Context, ferry.Reader, bool) {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Sink == nil || inst.Source == nil {
		return nil, nil, false
	}

	ctx := inst.ctx()

	if err := ferry.Dump(ctx, v, inst.Sink, d.opts...); err != nil {
		return nil, nil, false
	}

	return d.openOver(ctx, inst, set, n)
}

// openOver binds one instance's read half to a set and opens it, under the
// context that instance's contents live in.
func (d *driverRun) openOver(ctx context.Context, inst Instance, set *ferry.AddressSet,
	n int,
) (context.Context, ferry.Reader, bool) {
	d.rep.Helper()

	open, err := inst.Source.Bind(set)
	if err != nil {
		d.fail(n, "Source.Bind: "+err.Error())

		return nil, nil, false
	}

	r, err := open(ctx)
	if err != nil {
		d.fail(n, "opening a reader: "+err.Error())

		return nil, nil, false
	}

	return ctx, r, true
}

// closeIf releases a reader or a writer that holds a resource, which is what
// keeps a suite that mints an instance per case from leaking a handle per case.
//
// The error is discarded deliberately and in one place: what a Close reports is
// case 6's subject, and every other case closing loudly would report case 6's
// failure eleven more times.
func closeIf(plane any) {
	if c, ok := plane.(ferry.Releaser); ok {
		_ = c.Close()
	}
}

// The errors the suite stages, so that a case can assert that its own failure
// and no other reached the caller.
var (
	errProbeGet   = errors.New("ferrytest: the conformance suite made this Get fail")
	errProbeSet   = errors.New("ferrytest: the conformance suite made this Set fail")
	errProbeClose = errors.New("ferrytest: the conformance suite made this Close fail")
	errNoSink     = errors.New("ferrytest: the plane mints no sink")
)

// erringSource wraps a driver's read half and makes one address fail.
//
// It keeps no [ferry.Enumerator], which is why the case that uses it loads a
// leaf-only fixture: a shell handing out interfaces its inner reader does not
// have is the defect [shellWriter] exists to avoid, and one that drops them is
// only usable where nothing needs them.
//
// It does release, and that is not the same trade. Dropping Enumerator costs
// the case nothing, because nothing it loads enumerates; dropping Close leaked
// the driver's own reader once per Driver call, measured at 61 opens against 60
// closes, and for a file- or connection-backed driver that is a descriptor.
type erringSource struct {
	inner ferry.Source
	at    ferry.Path
	err   error
}

// Bind hands the address set straight through and wraps whatever the open
// produces.
func (s erringSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return erringReader{inner: r, at: s.at, err: s.err}, nil
	}, nil
}

// erringReader is the open half of [erringSource].
type erringReader struct {
	inner ferry.Reader
	at    ferry.Path
	err   error
}

// Get fails at one address and answers from the plane everywhere else.
func (r erringReader) Get(ctx context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	if addr.Path() == r.at {
		return ferry.Value{}, r.err
	}

	return r.inner.Get(ctx, addr)
}

// Close releases the driver's own reader, and answers for a reader that holds
// nothing.
//
// Declaring it unconditionally is what a shell may not do for [ferry.Committer]
// or [ferry.Enumerator], where a no-op answer is indistinguishable from the
// driver's own, and is safe here for the reason core's own release is: a Close
// that reports nothing is exactly what a reader holding no resource produces.
func (r erringReader) Close() error {
	c, ok := r.inner.(ferry.Releaser)
	if !ok {
		return nil
	}

	return c.Close()
}

// bindSpy keeps the address set a driver's Bind was handed, which is the only
// way to ask what the compiler told the driver to expect.
type bindSpy struct {
	inner ferry.Sink
	set   *ferry.AddressSet
}

// Bind records the set and hands it on unchanged.
func (s *bindSpy) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	s.set = addrs

	return s.inner.Bind(addrs)
}
