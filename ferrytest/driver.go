package ferrytest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/onhotpath/ferry"
)

// Driver is the driver conformance suite: twenty-three cases over one plane, and
// the whole of what a driver author writes.
//
//	func TestConformance(t *testing.T) {
//	    ferrytest.Driver(t, ferrytest.Plane{
//	        Name:  "yaml",
//	        Kinds: []ferry.VKind{ferry.KindAbsent, ferry.KindNull, ferry.KindBool,
//	            ferry.KindNumber, ferry.KindString, ferry.KindBytes},
//	        Except: notUTF8, // it carries String and not every value of it
//	        Open:   func() ferrytest.Instance { ... },
//	        Golden: []ferrytest.Artefact{ferrytest.Golden(cfg, "b: !!binary aGk=\n")},
//	    })
//	}
//
// There is one call and no menu, because a suite a driver author can partially
// adopt measures nothing. It runs everything [RoundTrip] does, so a driver
// calling this need not call that as well.
//
// Fill [Plane.Kinds] in honestly. It is what your plane carries end to end, and
// the suite turns it into an obligation in both directions: a value of a kind
// you did not declare has to be refused loudly rather than quietly mangled.
//
// It takes no [context.Context]. Every walk it runs uses [context.Background],
// because a conformance run has no deadline to inherit and no caller to cancel
// it.
//
// # A new case does not break a driver
//
// This suite may gain cases in a minor release of ferry, so a driver that passed
// yesterday can fail today and nothing in the Go toolchain will have warned you
// first: adding a case changes no signature and no exported name. That is
// intended. A new case does not break a driver, it reports that the driver was
// already broken, against a rule that was published before the case existed.
func Driver(t T, p Plane, opts ...ferry.Option) { //nolint:gocritic // hugeParam: Plane is by value.
	t.Helper()

	if p.Open == nil {
		t.Errorf("plane %s: Open is nil, so there is no plane to run the suite against", p.Name)

		return
	}

	// The Option list, resolved once against this suite's own fixtures, so an
	// Option list that is simply wrong is one report rather than one per case.
	// A [ferry.TagKey] is the one that lands here: it names the key ferry reads
	// for every type in the run, so a key that is not the one these fixtures
	// were written under leaves them unable to compile.
	if err := driverFixturesCompile(opts); err != nil {
		t.Errorf("plane %s: these Options leave the suite unable to compile the structs it dumps: %v", p.Name, err)

		return
	}

	d := &driverRun{rep: t, plane: p, opts: opts, carry: kindSet(p.Kinds)}
	d.run()
}

// driverRun is one Driver call, carried down to the cases so that each of them
// is a method with no parameter list of its own.
//
// It also holds the home for the //nolint on [Driver] and [RoundTrip]. Since
// #157 added [Plane.Except], a Plane is five words and 80 bytes, which is over
// gocritic's hugeParam threshold, so both signatures report a heavy by-value
// parameter. The remedy gocritic names is a *Plane, and that is ADR-0014's
// published signature and every driver's call site in and out of this
// repository: a description copied twice per conformance run is not worth a
// breaking change to all of them.
type driverRun struct {
	rep   reporter
	plane Plane
	opts  []ferry.Option

	// carry is [Plane.Kinds] as a set, because case 1 asks the question per
	// golden and a slice scan per case is a scan per case.
	carry map[ferry.VKind]bool
}

// run is the twenty-three cases, in the order ADR-0014 lists them.
func (d *driverRun) run() {
	d.rep.Helper()

	d.declared()
	d.caseKinds()
	d.caseBind()
	d.caseProbe()
	d.caseGetError()
	d.caseChildren()
	d.caseLifecycle()
	d.caseKeyInjectivity()
	d.caseRetention()
	d.caseDynamic()
	d.casePerRequestPlane()
	d.caseGolden()
	d.caseNullAtContainer()
	d.caseForeign()
	d.caseConcurrentOpen()
	d.caseSecondDump()
	d.caseSerialEquivalence()
	d.caseReplace()
	d.casePrepared()
	d.caseUnforgettable()
	d.caseOrdering()
	d.caseOmission()
	d.caseNamed()
	d.caseRootLeaf()
}

// caseKinds is case 1: every value the plane can express, and a loud refusal for
// every one it declared it cannot carry (ADR-0005, ADR-0004).
//
// It is one case with two halves and neither stands alone. Running only the
// expressible half would turn a flattening driver's data loss into a silence,
// and running everything would fail every flat driver for having no null - which
// is the whole reason [Plane.Kinds] is a declaration rather than an inference.
//
// The split is per case and not per proof, and that granularity is the
// declaration model's rather than this loop's convenience. A plane may carry a
// kind and not every value of it - driver/yaml carries KindString and cannot
// spell the one string that is not valid UTF-8 - so a proof is not the unit a
// plane can answer about. Answering per proof gives two wrong answers and no
// third: demand a refusal of every string, which the plane must not make, or
// drop the three that round trip, which is a silent hole in the case that proves
// them.
//
// The expressible half is [RoundTrip], called and not restated, over proofs
// narrowed to the cases this plane declared it carries.
func (d *driverRun) caseKinds() {
	d.rep.Helper()

	proofs := CoreTypes()

	can := make([]Proof, 0, len(proofs))
	cannot := make([]Proof, 0, len(proofs))

	for _, pr := range proofs {
		can = append(can, pr.only(d.carries))
		cannot = append(cannot, pr.only(d.disclaimed))
	}

	RoundTrip(d.rep, d.plane, can, d.opts...)

	h := &harness{rep: d.rep, plane: d.plane, opts: d.opts}
	for _, pr := range cannot {
		pr.refuse(h)
	}
}

// carries reports whether the plane declared it can hold one value: its kind is
// in [Plane.Kinds] and [Plane.Except] does not name it.
//
// The two are one question here because they are one declaration, read at the
// only granularity a value has. A plane that declares no kind at all can express
// nothing and every case falls to the refusal half, which is a description that
// is wrong rather than a plane that is.
func (d *driverRun) carries(v ferry.Value) bool {
	if !d.carry[v.Kind()] {
		return false
	}

	return d.plane.Except == nil || !d.plane.Except(v)
}

// disclaimed is carries' complement, and it is what the refusal half is narrowed
// to: exactly the values the plane said it cannot hold.
func (d *driverRun) disclaimed(v ferry.Value) bool { return !d.carries(v) }

// refuse is case 1's second half, and it is a method on the proof for the reason
// run is: the cases are typed by a parameter no suite can name.
//
// It runs the cases this proof was narrowed to and no others, so which cases it
// covers is [caseKinds]'s single statement of the declaration rather than a
// second copy of it here.
func (p typeProof[T]) refuse(h *harness) {
	h.rep.Helper()

	for i, c := range p.cases {
		if !p.picked(c.Want) {
			continue
		}

		p.refuseCase(h, i, c)
	}
}

// refuseCase asserts that one value the plane cannot represent is refused rather
// than mangled.
//
// The instance is fresh, for the same reason every equivalence subtest gets one:
// a destination shared across cases is the defect that hides a broken second
// walk. A plane with no sink is silent here, because the expressible half has
// already reported it once per case.
//
// A value excepted under [Plane.Except] arrives here on the same footing as a
// kind the plane never declared, and that is what stops Except being a way to
// skip a case: excepting a value buys a refusal that has to be made, not a case
// that stops running.
func (p typeProof[T]) refuseCase(h *harness, i int, c Case[T]) {
	h.rep.Helper()

	inst := h.plane.Open()
	if inst.Sink == nil {
		return
	}

	err := ferry.Dump(inst.ctx(), holder[T]{Value: c.Value}, inst.Sink, h.opts...)
	if err != nil {
		return
	}

	h.rep.Errorf("%s: %s, and took %#v at %s without refusing it: a value a plane cannot carry is a loud "+
		"refusal and never a value quietly mangled",
		h.label(p.name, i), h.disclaims(c.Want), c.Want, caseAddr(c.Addr))
}

// caseBind is case 2: Bind succeeds against an unreachable plane, and the
// refusal lands inside the open (ADR-0004).
//
// [Plane.Open] mints a fresh, empty plane, so for a file-backed driver the file
// does not exist yet and for a networked one nothing has been dialled: this is
// the unreachable plane, supplied by the description rather than staged by the
// suite. Bind takes no [context.Context] precisely so that it cannot do the I/O
// that would fail here, and what a driver may refuse at Bind is what it can see
// without touching the plane.
//
// What the suite cannot check from outside is that no backend call happened, so
// this asserts the observable half: Bind does not fail.
func (d *driverRun) caseBind() {
	d.rep.Helper()

	inst := d.plane.Open()

	set, ok := fixtureSet[onlyLeaf](d, caseBindNo)
	if !ok {
		return
	}

	if inst.Source != nil {
		if _, err := inst.Source.Bind(set); err != nil {
			d.fail(caseBindNo, fmt.Sprintf("Source.Bind refused a legal address set against a plane that "+
				"nothing has reached yet: %v", err))
		}
	}

	if inst.Sink == nil {
		return
	}

	if _, err := inst.Sink.Bind(set); err != nil {
		d.fail(caseBindNo, fmt.Sprintf("Sink.Bind refused a legal address set against a plane that "+
			"nothing has reached yet: %v", err))
	}
}

// caseProbe is case 3: a plane that can answer about a container's own address
// reports what it holds there (ADR-0016, amending what this case was before).
//
// It used to be "Get at a container address answers Absent or Null", and under
// the sealed address model that question does not compile: [ferry.Reader.Get]
// takes a [ferry.LeafAddr] and a container's own address is a
// [ferry.SectionAddr] or a [ferry.CompositeAddr]. So the case is the same
// obligation asked through the method that now owns it, and a driver refusing
// at a leaf whose plane holds two values for it is no longer refusing something
// this suite calls illegal (#208).
//
// [ferry.Prober] is optional, so a reader that cannot answer is skipped rather
// than failed: a plane that cannot list often cannot say whether a container is
// there either.
func (d *driverRun) caseProbe() {
	d.rep.Helper()

	set, ok := fixtureSet[filled](d, caseContainerNo)
	if !ok {
		return
	}

	ctx, r, ok := dumpAndOpen(d, filledFixture(), set, caseContainerNo)
	if !ok {
		return
	}

	probes := d.probeFilled(ctx, r, set)

	closeIf(r)

	// The blanks write a null at their own address, so on a plane with no null
	// they are never written at all and there is no stored answer to read back.
	// Such a plane is asked only the half of this case it can be asked, and
	// case 1 is where its refusal of the other half is asserted.
	//
	// Whether the plane probes at all is settled here, before the blanks are
	// dumped, and that ordering is the fix rather than a tidy-up: a plane with
	// no Prober and no Ensurer failed that dump, and this case reported it as
	// its own where it is case 12's.
	if probes && d.carry[ferry.KindNull] {
		d.probeBlanks()
	}
}

// probeFilled is the populated half: a container with members under it is
// present, which is the answer a plane holding only leaves can still infer.
//
// It reports whether the plane probes at all, which is what the halves after it
// are gated on.
func (d *driverRun) probeFilled(ctx context.Context, r ferry.Reader, set *ferry.AddressSet) bool {
	d.rep.Helper()

	pr, ok := r.(ferry.Prober)
	if !ok {
		d.skip(caseContainerNo, "the plane's reader does not probe, which is optional for the same reason "+
			"enumeration is")

		return false
	}

	for _, at := range []ferry.Path{addrList, addrMap} {
		if a, found := compositeIn(set, at); found {
			d.probeIs(ctx, caseContainerNo, pr, a, ferry.PresencePresent)
		}
	}

	return true
}

// caseForeign is case 13: a plane key that belongs to no address of this schema
// says nothing about a container whose key space it shares.
//
// A section's members come from the type, so the question a plane is asked at
// one is exactly whether it holds those members. A flat plane that answered out
// of every key beginning with the section's own would fabricate the section out
// of somebody else's variable, which is the defect the typed address exists to
// retire, moved one method over: a required field under the fabricated section
// then fails a load that should have left the pointer nil.
//
// The neighbour is written through this plane's own sink, so the case makes no
// assumption about how the plane spells a key. A tree plane writes it beside the
// section and a flat plane writes it inside the section's prefix, and the
// required answer is the same for both.
func (d *driverRun) caseForeign() {
	d.rep.Helper()

	set, ok := fixtureSet[justSection](d, caseForeignNo)
	if !ok {
		return
	}

	a, found := sectionIn(set, addrSection)
	if !found {
		d.fail(caseForeignNo, "the address set Bind received does not hold "+addrSection.String()+
			" as a section address, so there is no container to ask about")

		return
	}

	d.probeForeign(set, a)
}

// probeForeign dumps the neighbour and asks about the section beside it.
func (d *driverRun) probeForeign(set *ferry.AddressSet, a ferry.SectionAddr) {
	d.rep.Helper()

	ctx, r, ok := dumpAndOpen(d, neighbour{Beside: "v"}, set, caseForeignNo)
	if !ok {
		return
	}

	defer closeIf(r)

	pr, ok := r.(ferry.Prober)
	if !ok {
		d.skip(caseForeignNo, "the plane's reader does not probe, which is optional for the same reason "+
			"enumeration is")

		return
	}

	d.probeIs(ctx, caseForeignNo, pr, a, ferry.PresenceAbsent)
}

// probeBlanks is the empty half: a nil composite, an empty composite and a nil
// optional section were each told their own address holds a null, and a driver
// reports what the plane holds.
//
// Absence would delete an observation rather than rename one: the field a
// LoadOver should clear keeps its seeded value, and nothing reports why.
func (d *driverRun) probeBlanks() {
	d.rep.Helper()

	set, ok := fixtureSet[blanks](d, caseContainerNo)
	if !ok {
		return
	}

	ctx, r, ok := dumpAndOpen(d, blanksFixture(), set, caseContainerNo)
	if !ok {
		return
	}

	defer closeIf(r)

	pr, ok := r.(ferry.Prober)
	if !ok {
		d.skip(caseContainerNo, "the plane's reader does not probe, which is optional for the same reason "+
			"enumeration is")

		return
	}

	for _, at := range []ferry.Path{addrNilList, addrEmptyMap} {
		if a, found := compositeIn(set, at); found {
			d.probeIs(ctx, caseContainerNo, pr, a, ferry.PresenceNull)
		}
	}

	if a, found := sectionIn(set, addrSection); found {
		d.probeIs(ctx, caseContainerNo, pr, a, ferry.PresenceNull)
	}
}

// probeIs reads one container address and compares what the plane answered with
// what was written there.
func (d *driverRun) probeIs(ctx context.Context, n int, pr ferry.Prober, addr ferry.Container,
	want ferry.Presence,
) {
	d.rep.Helper()

	got, err := pr.Probe(ctx, addr)
	if err != nil {
		d.fail(n, fmt.Sprintf("Probe at the container address %s failed with %v, where the "+
			"fixture had just been written through this plane's own sink", addr, err))

		return
	}

	if got.Presence() == want {
		return
	}

	d.fail(n, fmt.Sprintf("Probe at the container address %s answered %s, want %s: a driver "+
		"reports what the plane holds at a container's own address, and the three answers are three "+
		"different things to a reload", addr, got.Presence(), want))
}

// caseGetError is case 4: a Get returning a non-nil error reaches the caller as
// an error and never as an Absent (ADR-0004).
//
// It is the second of a pair and both are needed. Survey item 5.11 found a YAML
// provider discarding parse errors and answering with an empty result, ferry's
// own walk committed the mirror of it, and the two cancelled: a suite that
// checked only the composition of them would have stayed green through both.
//
// What the suite can reach from outside a driver is the half that lives above
// it. A driver's own parse failure cannot be staged - the plane is the driver's
// and nothing here can corrupt it - so the reader is wrapped, made to fail at
// one address, and the assertion is that the failure arrives as a failure rather
// than as an unset field and a nil error.
func (d *driverRun) caseGetError() {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Source == nil {
		return
	}

	src := erringSource{inner: inst.Source, at: addrLeaf, err: errProbeGet}

	_, err := ferry.Load[onlyLeaf](inst.ctx(), src, d.opts...)
	if err == nil {
		d.fail(caseGetErrorNo, "a Get that failed was reported as a load that succeeded, so the field is at its "+
			"zero value and nothing says why")

		return
	}

	if !errors.Is(err, errProbeGet) {
		d.fail(caseGetErrorNo, fmt.Sprintf("the load failed with %v, which does not carry the error Get "+
			"returned: a driver's own class and text must survive to the caller", err))
	}
}

// caseChildren is case 5: Children at a composite address returns the segments
// the plane holds immediately under it, kinded (ADR-0016).
//
// Segments and not addresses, because the driver says how the plane spells its
// members and the schema types the child. The kind is the assertion: a position
// under a sequence and a name under a mapping are different answers, and core
// refuses each under the other.
//
// [ferry.Enumerator] is optional in both directions - a Vault token with read
// and no list is ordinary - so a reader that does not enumerate is skipped
// rather than failed.
func (d *driverRun) caseChildren() {
	d.rep.Helper()

	set, ok := fixtureSet[filled](d, caseChildrenNo)
	if !ok {
		return
	}

	ctx, r, ok := dumpAndOpen(d, filledFixture(), set, caseChildrenNo)
	if !ok {
		return
	}

	defer closeIf(r)

	e, ok := r.(ferry.Enumerator)
	if !ok {
		d.skip(caseChildrenNo, "the plane's reader does not enumerate, which ADR-0004 makes optional")

		return
	}

	if a, found := compositeIn(set, addrList); found {
		d.childrenAre(ctx, e, a, []ferry.Segment{ferry.IndexSegment(0), ferry.IndexSegment(1)})
	}

	if a, found := compositeIn(set, addrMap); found {
		d.childrenAre(ctx, e, a, []ferry.Segment{ferry.NameSegment(fixtureKey)})
	}

	d.childrenAtBlank()
}

// childrenAtBlank is the other half of case 5, and it is the half a driver
// fails silently: a container the plane holds nothing under, and a container it
// holds a null at, each answer with no members at all.
//
// Enumerating only the containers the suite populated measures a driver against
// what it was just told, and a driver answering out of every plane key sharing
// the container's prefix passes that. Measured, one inventing a single element
// at a blank address was reported by nothing, and a load of the field then
// fabricated a member out of an unrelated ambient variable.
//
// It is the address rather than the plane that is blank, so nothing here needs
// an empty plane: the fixture written is one schema's and the set bound is
// another's, which is case 13's idiom and makes no assumption about how the
// plane spells a key.
func (d *driverRun) childrenAtBlank() {
	d.rep.Helper()

	set, err := setOf[blanks](d.opts)
	if err != nil {
		d.fail(caseChildrenNo, "compiling the suite's own fixture: "+err.Error())

		return
	}

	childrenAreEmpty(d, set, filledFixture(), "the plane holds nothing under it")

	// A blank writes a null at its own address, so on a plane with no null it is
	// never written and there is no stored answer to enumerate. Case 1 owns that
	// plane's refusal of the null itself.
	if d.carry[ferry.KindNull] {
		childrenAreEmpty(d, set, blanksFixture(), "the plane holds a null at it")
	}
}

// childrenAreEmpty dumps one fixture, binds the blanks schema over it, and
// requires both of its composites to enumerate to nothing.
//
// It is a function rather than a method for [dumpAndOpen]'s reason: the fixture
// dumped is a type parameter, and one of the two callers writes a different
// schema from the one the reader is bound to.
func childrenAreEmpty[T any](d *driverRun, set *ferry.AddressSet, v T, why string) {
	d.rep.Helper()

	ctx, r, ok := dumpAndOpenQuiet(d, v, set, caseChildrenNo)
	if !ok {
		return
	}

	defer closeIf(r)

	e, ok := r.(ferry.Enumerator)
	if !ok {
		d.skip(caseChildrenNo, "the plane's reader does not enumerate, which ADR-0004 makes optional")

		return
	}

	for _, at := range []ferry.Path{addrNilList, addrEmptyMap} {
		if a, found := compositeIn(set, at); found {
			d.noChildren(ctx, e, a, why)
		}
	}
}

// noChildren is one blank container's enumeration, which has to be empty.
func (d *driverRun) noChildren(ctx context.Context, e ferry.Enumerator, addr ferry.CompositeAddr, why string) {
	d.rep.Helper()

	got, err := e.Children(ctx, addr)
	if err != nil {
		d.fail(caseChildrenNo, fmt.Sprintf("Children at %s failed with %v, where %s", addr, err, why))

		return
	}

	if len(got) == 0 {
		return
	}

	d.fail(caseChildrenNo, fmt.Sprintf("Children at %s answered %v, want nothing, because %s: a member "+
		"invented at a blank container is a field loaded out of somebody else's key", addr, got, why))
}

// childrenAre reads one composite's members and compares them against the
// segments the fixture put there, sorted so that the comparison is about the
// set rather than about the order a driver happens to answer in.
func (d *driverRun) childrenAre(ctx context.Context, e ferry.Enumerator,
	addr ferry.CompositeAddr, want []ferry.Segment,
) {
	d.rep.Helper()

	got, err := e.Children(ctx, addr)
	if err != nil {
		d.fail(caseChildrenNo, fmt.Sprintf("Children at %s failed with %v", addr, err))

		return
	}

	slices.SortFunc(got, compareSegments)

	if slices.Equal(got, want) {
		return
	}

	d.fail(caseChildrenNo, fmt.Sprintf("Children at %s answered %v, want %v: a segment carries its kind, so a "+
		"sequence element and a mapping member are different answers", addr, got, want))
}

// caseKeyInjectivity is case 7: a driver producing a plane key refuses a
// non-injective key function over the address set, before any I/O, naming both
// addresses (ADR-0003).
//
// One rule covers separator collisions, case folding and any normalisation a
// driver invents, because all three are the same failure: a many-to-one map out
// of the address set merges two addresses into one plane key and loses one of
// them with no error anywhere.
//
// A driver that builds no plane key at all - a tree driver walks the segments -
// carries no such obligation, and the suite cannot tell the two apart from
// outside. So Bind accepting this set is not a failure; what is asserted is that
// a Bind which refuses says which two addresses collided, because a refusal
// naming one address is a refusal a driver author cannot act on.
func (d *driverRun) caseKeyInjectivity() {
	d.rep.Helper()

	inst := d.plane.Open()

	set, ok := fixtureSet[colliding](d, caseInjectiveNo)
	if !ok {
		return
	}

	if inst.Source != nil {
		_, err := inst.Source.Bind(set)
		d.namesBothAddresses(err)
	}

	if inst.Sink == nil {
		return
	}

	_, err := inst.Sink.Bind(set)
	d.namesBothAddresses(err)
}

// namesBothAddresses holds a Bind refusal to naming the pair it refused over,
// and is silent for a Bind that accepted.
func (d *driverRun) namesBothAddresses(err error) {
	d.rep.Helper()

	if err == nil {
		return
	}

	if !errors.Is(err, ferry.ErrPlane) {
		d.fail(caseInjectiveNo, fmt.Sprintf("Bind refused an address set with %v, which is not a plane refusal: "+
			"a key function that folds two addresses together is the plane's own class", err))
	}

	if namesAPair(err) {
		return
	}

	d.fail(caseInjectiveNo, fmt.Sprintf("Bind refused with %+v, which does not name both addresses of any pair it "+
		"could have collided: one of the two is the one the author has to move", err))
}

// namesAPair reports whether some element of a refusal names both addresses of
// some pair this set could collide.
//
// Per element rather than over the whole rendering, and that is the first real
// flattening driver's finding rather than tidiness. An uppercase fold collapses
// three of the pairs above and not one, so a driver routing its refusal through
// [ferry.NewKeys] hands back an aggregate - and an aggregate's Error() is the
// one-line summary, which names one address per element and elides past three.
// Read that way, a refusal that named both addresses of all three pairs looked
// like a refusal that named neither.
//
// [ferry.Elements] returns a one-element slice for a single failure, so a driver
// refusing exactly one pair, and a driver refusing with an error of its own that
// core never wrote, both read the same here as they did before.
func namesAPair(err error) bool {
	for _, e := range ferry.Elements(err) {
		text := e.Error()
		for _, pair := range collidingPairs {
			if strings.Contains(text, pair[0].String()) && strings.Contains(text, pair[1].String()) {
				return true
			}
		}
	}

	return false
}

// caseRetention is case 8: a key function retains nothing across opens, asserted
// on the write side, which is where retention refuses a legal write (ADR-0012).
//
// Injectivity is a property of one write. Two writes to one plane at different
// times are not required to be mutually injective, and a driver that keeps its
// minted keys on the binding refuses the second of two opens that each mint one
// half of a colliding pair - reporting a collision against an address no plane
// still holds. The retention is unbounded too, which is the second half of why
// the minted set belongs to the open.
//
// The two addresses below are one pair a flattening driver folds together and
// two distinct addresses everywhere else, so a driver that does not flatten
// passes by writing both.
func (d *driverRun) caseRetention() {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Sink == nil {
		return
	}

	b, err := ferry.BindSink[justMap](inst.Sink, d.opts...)
	if err != nil {
		d.fail(caseRetentionNo, fmt.Sprintf("BindSink: %v", err))

		return
	}

	if !d.mints(inst.ctx(), b, []string{retentionFirst}, caseRetentionNo) {
		return
	}

	if d.mints(inst.ctx(), b, []string{retentionSecond}, caseRetentionNo) {
		return
	}

	d.fail(caseRetentionNo, "the second open of one binding refused a write the first open made legal, so the key "+
		"function kept what it minted: a minted set that outlives its open reports a collision against an address "+
		"no plane still holds")
}

// caseDynamic is case 9: a sink accepts a dynamic address its static table never
// held (ADR-0004).
//
// The address set a driver is bound to holds every address the type determines
// and none that a value mints, because a map key and a sequence index do not
// exist until there is a value. A driver treating its precomputed table as a
// closed set refuses a legal write, which is why core hands out a key function
// rather than a map.
//
// It mints every key the suite ever mints, in one dump, and that is not
// thoroughness for its own sake. Case 8 writes one of them first and gives up
// where the write is refused, so a sink with a restrictive key charset - a
// store that will not take a hyphen - used to end case 8 and be asked by
// nothing else at all. This case owns whether a dynamic address is taken, so it
// is the case that has to ask about each of them.
func (d *driverRun) caseDynamic() {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Sink == nil {
		return
	}

	b, err := ferry.BindSink[justMap](inst.Sink, d.opts...)
	if err != nil {
		d.fail(caseDynamicNo, fmt.Sprintf("BindSink: %v", err))

		return
	}

	if d.mints(inst.ctx(), b, dynamicKeys, caseDynamicNo) {
		return
	}

	d.fail(caseDynamicNo, "the sink refused an address under a container its Bind was handed, and a map key is "+
		"minted from the value rather than from the type, so it was never going to be in the static set")
}

// mints dumps a one-entry map through the binding and reports whether the write
// was taken.
//
// It goes through a dump rather than through the [ferry.Writer] directly,
// because a value-minted address is one only the walk can produce: the three
// address kinds are sealed and the compiler is the only thing that mints one
// (ADR-0016). Each dump is one open of the binding, which is what makes this
// the retention question.
//
// It takes the keys as a set rather than one at a time, because case 9 mints
// both of case 8's addresses in one dump: a sink that cannot spell one of them
// ends case 8, and a case that ends is a case that measured nothing unless
// something else is asking whether the write should have been taken.
func (d *driverRun) mints(ctx context.Context, b *ferry.SinkBinding[justMap], keys []string, n int) bool {
	d.rep.Helper()

	m := make(map[string]string, len(keys))
	for _, k := range keys {
		m[k] = "v"
	}

	err := b.Dump(ctx, justMap{Map: m})
	if err == nil {
		return true
	}

	d.note(n, err)

	return false
}

// note is where a refused write's own text reaches the report, so that a driver
// author sees why rather than only that.
//
// It is a skip and not a failure, because a write that never landed is no
// evidence about what a second open retained. Whether the write should have
// landed at all is case 9's, which mints both of these addresses in one dump
// precisely so that this one cannot end a case by disappearing.
func (d *driverRun) note(n int, err error) {
	d.rep.Helper()

	d.skip(n, "the write was refused with: "+err.Error())
}

// casePerRequestPlane is case 10: a driver reading its plane from the context
// refuses at open when it is absent, with [ferry.ErrPlane] (ADR-0012).
//
// It runs for a plane that fills in [Instance.InContext] and is skipped for
// every plane that does not, because a driver whose halves carry their own
// contents has no absence to refuse and nothing here would be measuring it. The
// skip is explicit rather than silent: a case that quietly did nothing would be
// indistinguishable from a case that passed, and a driver author would have no
// way to know that the one obligation their design does carry went unmeasured.
//
// Both halves are asked, because ADR-0012 puts the same rule on a sink whose
// plane is per request as on a source: it reads the plane from the context in
// exactly the same way and refuses at open when it is absent.
//
// It reaches the driver through Bind and the open rather than through
// [ferry.Load], and that is the case rather than a shortcut. What is asserted is
// *where* the refusal lands, and a load collapses Bind and the open into one
// error that cannot say which of them produced it.
func (d *driverRun) casePerRequestPlane() {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.InContext == nil {
		d.skip(casePerRequestNo, "the plane puts nothing in a context, so it does not take its plane per "+
			"request and there is no absence of one for an open to refuse")

		return
	}

	d.perRequestHalf(inst.ctx(), "a reader", d.readProbe(inst.Source))
	d.perRequestHalf(inst.ctx(), "a writer", d.writeProbe(inst.Sink))
}

// openProbe opens one half of a plane under one context and reports what the
// open answered, releasing whatever it handed back.
//
// It is one type over both halves because case 10 asks both the same question
// and neither answer involves reading or writing anything: the refusal is at the
// open, so a [ferry.Reader] and a [ferry.Writer] are equally beside the point
// once they exist.
type openProbe func(ctx context.Context) error

// readProbe binds the read half once, which is where case 10's "not at Bind"
// half is asserted, and hands the open back as a probe.
//
// Bind takes no [context.Context] (ADR-0004), so it cannot see whether a plane
// was supplied and must not refuse for the absence of one. A nil probe is a
// plane with no read half, or a Bind that has already been reported.
func (d *driverRun) readProbe(src ferry.Source) openProbe {
	d.rep.Helper()

	if src == nil {
		return nil
	}

	set, ok := fixtureSet[onlyLeaf](d, casePerRequestNo)
	if !ok {
		return nil
	}

	open, err := src.Bind(set)
	if err != nil {
		d.fail(casePerRequestNo, fmt.Sprintf("Source.Bind refused a legal address set with %v, and no plane "+
			"had been supplied: Bind takes no context, so it cannot see that absence, and a plane that is "+
			"not there is refused inside the open", err))

		return nil
	}

	return func(ctx context.Context) error {
		r, err := open(ctx)
		closeIf(r)

		return err
	}
}

// writeProbe is [driverRun.readProbe] on the write half, and it is a second
// function rather than one over an interface because a [ferry.Source] and a
// [ferry.Sink] are two interfaces with one method name and no common type.
func (d *driverRun) writeProbe(sink ferry.Sink) openProbe {
	d.rep.Helper()

	if sink == nil {
		return nil
	}

	set, ok := fixtureSet[onlyLeaf](d, casePerRequestNo)
	if !ok {
		return nil
	}

	open, err := sink.Bind(set)
	if err != nil {
		d.fail(casePerRequestNo, fmt.Sprintf("Sink.Bind refused a legal address set with %v, and no plane had "+
			"been supplied: Bind takes no context, so it cannot see that absence, and a plane that is not "+
			"there is refused inside the open", err))

		return nil
	}

	return func(ctx context.Context) error {
		w, err := open(ctx)
		closeIf(w)

		return err
	}
}

// perRequestHalf runs the case over one half of the plane: one binding, one open
// against a context carrying no plane, and one against the context the
// description supplies.
//
// Both opens are on the one binding, and that is the "per load" half of the
// rule. A driver that refused the absence once and stayed refused would pass a
// case that only opened without a plane, and it would never load anything.
func (d *driverRun) perRequestHalf(ctx context.Context, half string, probe openProbe) {
	d.rep.Helper()

	if probe == nil {
		return
	}

	d.refusesWithoutPlane(half, probe)

	if err := probe(ctx); err != nil {
		d.fail(casePerRequestNo, fmt.Sprintf("opening %s with the plane in the context failed with %v, on the "+
			"binding whose open had just refused the absence of one: a refusal that outlives the plane "+
			"arriving is a driver that never opens at all", half, err))
	}
}

// refusesWithoutPlane is the assertion the case exists for: an open against a
// context carrying no plane is refused, and refused as a plane failure.
//
// The class is [ferry.ErrPlane] and not a new one, because a plane that was
// never supplied is the limiting case of a plane that cannot be reached
// (ADR-0012), which is a class ADR-0011 already has. A driver wraps its own
// provenance marker around it, which is the driver's own sentinel and not
// something this suite can name.
func (d *driverRun) refusesWithoutPlane(half string, probe openProbe) {
	d.rep.Helper()

	err := probe(context.Background())
	if err == nil {
		d.fail(casePerRequestNo, "opening "+half+" against a context carrying no plane succeeded, so the load "+
			"that follows reads an empty plane and reports every address as missing: a plane that was "+
			"never supplied is refused and never quietly answered from")

		return
	}

	if errors.Is(err, ferry.ErrPlane) {
		return
	}

	d.fail(casePerRequestNo, fmt.Sprintf("opening %s against a context carrying no plane failed with %v, which "+
		"is not a plane refusal: a plane that was never supplied is the limiting case of one that cannot be "+
		"reached, and it carries the same class", half, err))
}

// caseGolden is case 11: a golden artefact - a fixed value, dumped, compared
// against fixed expected plane contents (ADR-0013).
//
// It is the one case that sees a representation, and a round trip structurally
// cannot stand in for it: a round trip tests a function against its own inverse,
// a spelling is a choice of function, and changing both halves together is
// invisible to any test that only composes them. Measured, a driver's Bytes
// spelling moved from base64 to hex in a one-line edit touching both halves,
// every round trip stayed green, and every file the previous version wrote
// became garbage.
//
// The expectation is on the [Plane] and is not a parameter of this suite,
// because the spelling is the driver's statement about itself: ADR-0001 refuses
// to constrain indentation and key order, so what is pinned has to be the
// author's choice.
//
// The contents are read after the dump, which is after any [ferry.Committer] has
// committed, because a staging sink holds nothing durable until then.
func (d *driverRun) caseGolden() {
	d.rep.Helper()

	if len(d.plane.Golden) == 0 {
		d.skip(caseGoldenNo, "the plane pins no golden artefact, which is a plane with no serialization format "+
			"of its own rather than a plane that skipped the case")

		return
	}

	for _, a := range d.plane.Golden {
		d.goldenRow(a)
	}
}

// goldenRow dumps one artefact into a plane minted for it alone and compares
// what the plane then holds.
func (d *driverRun) goldenRow(a Artefact) {
	d.rep.Helper()

	inst := d.plane.Open()

	switch {
	case inst.Sink == nil:
		d.fail(caseGoldenNo, "the plane mints no sink, so a golden artefact has nothing to be dumped through")

		return
	case inst.Contents == nil:
		d.fail(caseGoldenNo, "the plane pins a golden artefact and mints no Contents, so what it says its "+
			"spelling is cannot be read back and the promise has nothing behind it")

		return
	}

	if err := a.dump(inst.ctx(), inst.Sink, d.opts...); err != nil {
		d.fail(caseGoldenNo, fmt.Sprintf("dumping the %s artefact: %v", a.label, err))

		return
	}

	got, err := inst.Contents()
	if err != nil {
		d.fail(caseGoldenNo, fmt.Sprintf("reading the contents back after the %s artefact: %v", a.label, err))

		return
	}

	if string(got) == a.want {
		return
	}

	d.fail(caseGoldenNo, fmt.Sprintf("the %s artefact left the plane holding %q, want %q: a change here is a "+
		"change to what every stored artefact of this plane means", a.label, got, a.want))
}

// caseNullAtContainer is case 12: a sink accepts a Set of a Null at a container
// address - a nil composite, an empty composite and a nil optional section - and
// that address was in the set its Bind received (ADR-0003, ADR-0005).
//
// It is the Dump mirror of case 3 and it exists because two engines handed one
// driver two different static sets for one type and nothing was red. Case 3 asks
// what a driver answers at a container address; this asks whether it was told to
// expect one at all. Without it, a driver whose static table holds a wildcard
// shape instead of the container address passes every other case and refuses a
// legal write.
func (d *driverRun) caseNullAtContainer() {
	d.rep.Helper()

	if !d.carry[ferry.KindNull] {
		d.skip(caseNullNo, "the plane declares no null, so there is no Null for a container address to be "+
			"handed; case 1 is where its refusal of one is asserted")

		return
	}

	inst := d.plane.Open()
	if inst.Sink == nil {
		return
	}

	spy := &bindSpy{inner: inst.Sink}
	if err := ferry.Dump(inst.ctx(), blanksFixture(), spy, d.opts...); err != nil {
		d.fail(caseNullNo, fmt.Sprintf("dumping a nil list, an empty map and a nil section: %v", err))

		return
	}

	d.boundContainers(spy.set)
}

// boundContainers is the assertion itself: the three container addresses the
// walk just wrote a null at were in the set the sink's Bind received, each at
// the kind the walk wrote it as.
func (d *driverRun) boundContainers(set *ferry.AddressSet) {
	d.rep.Helper()

	if set == nil {
		d.fail(caseNullNo, "the sink's Bind was never handed an address set")

		return
	}

	for _, at := range []ferry.Path{addrNilList, addrEmptyMap} {
		if _, ok := compositeIn(set, at); !ok {
			d.missingContainer(at, "composite")
		}
	}

	if _, ok := sectionIn(set, addrSection); !ok {
		d.missingContainer(addrSection, "section")
	}
}

// missingContainer reports a container address the compiler did not put in the
// set the driver was bound to, at the kind the walk then wrote at.
func (d *driverRun) missingContainer(at ferry.Path, kind string) {
	d.rep.Helper()

	d.fail(caseNullNo, fmt.Sprintf("the address set Bind received does not hold %s as a %s address, which the "+
		"walk then wrote a null at", at, kind))
}

// fixtureSet is the address set of one of the suite's own fixtures, and whether
// there was one.
//
// It cannot fail from here. [Driver] resolves the caller's Option list against
// every fixture before any case runs and returns without running one where that
// fails, so a case reaching this has already been told the fixture compiles.
// The error is reported rather than dropped, because a suite that swallowed one
// would report the case as having passed - and it is reported at one site
// rather than at eight, because eight copies of an unreachable arm are eight
// places for the message to drift.
// It is a function rather than a method because the fixture's type is a
// parameter, which is [dumpAndOpen]'s reason too.
func fixtureSet[T any](d *driverRun, n int) (*ferry.AddressSet, bool) {
	d.rep.Helper()

	set, err := setOf[T](d.opts)
	if err != nil {
		d.fail(n, "compiling the suite's own fixture: "+err.Error())

		return nil, false
	}

	return set, true
}

// fail is what every case reports through, and it names the plane and the case
// so that a driver author reading their own CI output knows which of
// twenty-three went red.
func (d *driverRun) fail(n int, msg string) {
	d.rep.Helper()

	d.rep.Errorf("plane %s: case %d: %s", d.plane.Name, n, msg)
}

// skip says out loud that a case did not run.
func (d *driverRun) skip(n int, why string) {
	d.rep.Helper()

	d.logf("plane %s: case %d skipped: %s", d.plane.Name, n, why)
}

// logf is where everything this suite says that is not a failure goes, and it
// is [logTo] under this run's reporter.
func (d *driverRun) logf(format string, args ...any) {
	d.rep.Helper()

	logTo(d.rep, format, args...)
}
