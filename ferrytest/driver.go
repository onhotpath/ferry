package ferrytest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/onhotpath/ferry"
)

// Driver is the driver conformance suite: twelve cases over one plane, and the
// whole of what a driver author writes.
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
// It calls [RoundTrip] rather than restating it, which is what keeps driver/* a
// single-call CI glob (ADR-0002): a suite a driver author can partially adopt is
// a suite that measures nothing, so there is one call and no menu.
//
// # It takes no context.Context
//
// Stated here rather than left as an omission (ADR-0014). Every walk it runs
// uses [context.Background], because a conformance run has no deadline to
// inherit and no caller to be cancelled by; a driver whose conformance run needs
// cancellation is #20's question and not this signature's.
//
// # A new case does not break a driver
//
// The suites may gain cases in a minor release where the apparatus may not, and
// the difference is not semver's to see: adding a case changes no signature, no
// type and no exported name, apidiff reports nothing, and a driver's CI goes
// red. That is affordable for exactly one reason, and it is the reason each case
// below cites the ADR sentence it executes:
//
//	A new conformance case does not break a driver. It reports that the driver
//	was already broken, against a rule an ADR had already landed.
//
// A case asserting a rule no ADR states is not a case, it is a new rule, and it
// needs the ADR first (ADR-0014).
func Driver(t T, p Plane, opts ...ferry.Option) { //nolint:gocritic // hugeParam: see Plane.Except.
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
type driverRun struct {
	rep   reporter
	plane Plane
	opts  []ferry.Option

	// carry is [Plane.Kinds] as a set, because case 1 asks the question per
	// golden and a slice scan per case is a scan per case.
	carry map[ferry.VKind]bool
}

// run is the twelve cases, in the order ADR-0014 lists them.
func (d *driverRun) run() {
	d.rep.Helper()

	d.caseKinds()
	d.caseBind()
	d.caseContainerGet()
	d.caseGetError()
	d.caseChildren()
	d.caseLifecycle()
	d.caseKeyInjectivity()
	d.caseRetention()
	d.caseDynamic()
	d.casePerRequestPlane()
	d.caseGolden()
	d.caseNullAtContainer()
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

	err := ferry.Dump(context.Background(), holder[T]{Value: c.Value}, inst.Sink, h.opts...)
	if err != nil {
		return
	}

	h.rep.Errorf("%s: %s, and took %#v at %s without refusing it: a value a plane cannot carry is a loud "+
		"refusal and never a value quietly mangled",
		h.label(p.name, i), h.disclaims(c.Want), c.Want, holderAddr)
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
	set := ferry.NewAddressSet(addrLeaf)

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

// caseContainerGet is case 3: Get at a container address answers with a nil
// error and reports what the plane holds there (ADR-0004, ADR-0014 amended
// under #136).
//
// A composite is read one element at a time under ADR-0003's structured
// addresses, so there is no group value for the container itself to hold, and a
// driver answering one is a driver core cannot interpret.
//
// Which of the two empty answers a row demands follows from what was written
// there. A populated list and a missing address hold Absent, because nothing
// was ever written at either. An empty composite holds Null, because ADR-0005
// writes a Null at a composite's own address when it has no elements, and a
// driver reports what the plane holds: answering Absent for a stored Null
// deletes an observation rather than renaming one, and takes a LoadOver's
// clearing of the field with it.
func (d *driverRun) caseContainerGet() {
	d.rep.Helper()

	set := ferry.NewAddressSet(addrList, addrMap, addrLeaf, addrMissing)

	if r, ok := dumpAndOpen(d, filledFixture(), set, caseContainerNo); ok {
		d.holdsNothing(r, addrList)
		d.holdsNothing(r, addrMap)
		d.holdsNothing(r, addrMissing)
		closeIf(r)
	}

	// The empty composites write a Null, so on a plane with no null they are
	// never written at all and there is no stored answer to read back. Such a
	// plane is asked only the half of this case it can be asked, and case 1 is
	// where its refusal of the other half is asserted.
	if !d.carry[ferry.KindNull] {
		return
	}

	blankSet := ferry.NewAddressSet(addrNilList, addrEmptyMap, addrSection)

	r, ok := dumpAndOpen(d, blanksFixture(), blankSet, caseContainerNo)
	if !ok {
		return
	}

	defer closeIf(r)

	d.holdsNull(r, addrNilList)
	d.holdsNull(r, addrEmptyMap)
}

// holdsNothing reads one container address nothing was ever written at, and
// reports a driver that answered with a value, or with an error where there is
// nothing to fail at.
//
// Absent is the only answer here, because a composite is read one element at a
// time and no Set ever reached the address.
func (d *driverRun) holdsNothing(r ferry.Reader, addr ferry.Path) {
	d.rep.Helper()

	v, ok := d.containerGet(r, addr)
	if !ok || v.Kind() == ferry.KindAbsent {
		return
	}

	d.fail(caseContainerNo, fmt.Sprintf("Get at the container address %s answered %#v, where nothing was ever "+
		"written: a composite is read one element at a time, so there is no group value for the container "+
		"itself to hold", addr, v))
}

// holdsNull reads one container address an empty composite was dumped to, and
// reports a driver that answered anything but the Null it was handed.
//
// ADR-0005 writes a Null at a composite's own address when it has no elements,
// and a driver reports what the plane holds. Absent means the plane does not
// hold the address, so answering it here deletes an observation rather than
// renaming one: the field a LoadOver should clear keeps its seeded value, and
// nothing reports why.
func (d *driverRun) holdsNull(r ferry.Reader, addr ferry.Path) {
	d.rep.Helper()

	v, ok := d.containerGet(r, addr)
	if !ok || v.Kind() == ferry.KindNull {
		return
	}

	d.fail(caseContainerNo, fmt.Sprintf("Get at the container address %s answered %#v, where an empty composite "+
		"was dumped and a Null landed: a driver reports what the plane holds, and Absent says the plane does "+
		"not hold the address at all", addr, v))
}

// containerGet is the read both container assertions share, and the one failure
// they share with it: a container address holds no value to fail at.
func (d *driverRun) containerGet(r ferry.Reader, addr ferry.Path) (ferry.Value, bool) {
	d.rep.Helper()

	v, err := r.Get(context.Background(), addr)
	if err != nil {
		d.fail(caseContainerNo, fmt.Sprintf("Get at the container address %s failed with %v, where a container "+
			"address holds no value and so has nothing to fail at", addr, err))

		return ferry.Value{}, false
	}

	return v, true
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

	_, err := ferry.Load[onlyLeaf](context.Background(), src, d.opts...)
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

// caseChildren is case 5: Children at a container address returns the element
// addresses, kinded (ADR-0004).
//
// Addresses and not names, because an address carries its segment kind, so the
// plane says whether the container is a mapping or a sequence instead of the
// caller guessing it from base-10 text. Comparing whole [ferry.Path] values is
// what asserts the kind: two addresses are equal exactly when they render alike,
// and an Index segment renders differently from a Name one.
//
// [ferry.Enumerator] is optional in both directions - a Vault token with read
// and no list is ordinary - so a reader that does not enumerate is skipped
// rather than failed.
func (d *driverRun) caseChildren() {
	d.rep.Helper()

	set := ferry.NewAddressSet(addrList, addrMap, addrLeaf)

	r, ok := dumpAndOpen(d, filledFixture(), set, caseChildrenNo)
	if !ok {
		return
	}

	defer closeIf(r)

	e, ok := r.(ferry.Enumerator)
	if !ok {
		d.skip(caseChildrenNo, "the plane's reader does not enumerate, which ADR-0004 makes optional")

		return
	}

	d.childrenAre(e, addrList, []ferry.Path{addrList.Elem(0), addrList.Elem(1)})
	d.childrenAre(e, addrMap, []ferry.Path{addrMap.At(fixtureKey)})
}

// childrenAre reads one container's children and compares them against the
// addresses the fixture put there, sorted segment-wise so that the comparison is
// about the set rather than about the order a driver happens to answer in.
func (d *driverRun) childrenAre(e ferry.Enumerator, prefix ferry.Path, want []ferry.Path) {
	d.rep.Helper()

	got, err := e.Children(context.Background(), prefix)
	if err != nil {
		d.fail(caseChildrenNo, fmt.Sprintf("Children at %s failed with %v", prefix, err))

		return
	}

	slices.SortFunc(got, ferry.Path.Compare)

	if slices.Equal(got, want) {
		return
	}

	d.fail(caseChildrenNo, fmt.Sprintf("Children at %s answered %v, want %v: an address carries its segment "+
		"kind, so a sequence element and a mapping member are different answers", prefix, got, want))
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
	set := ferry.NewAddressSet(collidingAddrs()...)

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

	text := err.Error()
	for _, pair := range collidingPairs {
		if strings.Contains(text, pair[0].String()) && strings.Contains(text, pair[1].String()) {
			return
		}
	}

	d.fail(caseInjectiveNo, fmt.Sprintf("Bind refused with %v, which does not name both addresses of any pair it "+
		"could have collided: one of the two is the one the author has to move", err))
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

	open, err := inst.Sink.Bind(ferry.NewAddressSet(addrMap))
	if err != nil {
		d.fail(caseRetentionNo, fmt.Sprintf("Sink.Bind: %v", err))

		return
	}

	if !d.mints(open, addrMap.At("a-b"), caseRetentionNo) {
		return
	}

	if d.mints(open, addrMap.At("a_b"), caseRetentionNo) {
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
func (d *driverRun) caseDynamic() {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Sink == nil {
		return
	}

	open, err := inst.Sink.Bind(ferry.NewAddressSet(addrMap))
	if err != nil {
		d.fail(caseDynamicNo, fmt.Sprintf("Sink.Bind: %v", err))

		return
	}

	if d.mints(open, addrMap.At(fixtureKey), caseDynamicNo) {
		return
	}

	d.fail(caseDynamicNo, "the sink refused an address under a container its Bind was handed, and a map key is "+
		"minted from the value rather than from the type, so it was never going to be in the static set")
}

// mints opens a writer and writes one address through it, reporting whether the
// write was taken. The open is closed either way, because a suite that leaked a
// file handle per case would fail a driver for the suite's own reason.
func (d *driverRun) mints(open ferry.OpenWriterFunc, addr ferry.Path, n int) bool {
	d.rep.Helper()

	w, err := open(context.Background())
	if err != nil {
		d.fail(n, fmt.Sprintf("opening a writer: %v", err))

		return false
	}

	defer closeIf(w)

	return w.Set(context.Background(), addr, ferry.String("v")) == nil
}

// casePerRequestPlane is case 10: a driver reading its plane from the context
// refuses at open when it is absent, with [ferry.ErrPlane] (ADR-0012).
//
// It ships and is skipped, because a [Plane] does not describe a driver that
// takes its plane per request and no first-party driver in this repository has
// one. The skip is explicit rather than silent: a case that quietly did nothing
// would be indistinguishable from a case that passed, and a driver author would
// have no way to know that the one obligation their design does carry went
// unmeasured.
func (d *driverRun) casePerRequestPlane() {
	d.rep.Helper()

	d.skip(casePerRequestNo, "a Plane describes no per-request plane, so there is no open for the absence of one "+
		"to be refused at")
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

	if err := a.dump(context.Background(), inst.Sink, d.opts...); err != nil {
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
	if err := ferry.Dump(context.Background(), blanksFixture(), spy, d.opts...); err != nil {
		d.fail(caseNullNo, fmt.Sprintf("dumping a nil list, an empty map and a nil section: %v", err))

		return
	}

	for _, addr := range []ferry.Path{addrNilList, addrEmptyMap, addrSection} {
		if spy.set != nil && spy.set.Has(addr) {
			continue
		}

		d.fail(caseNullNo, fmt.Sprintf("the address set Bind received does not hold the container address %s, "+
			"which the walk then wrote a Null at", addr))
	}
}

// fail is what every case reports through, and it names the plane and the case
// so that a driver author reading their own CI output knows which of twelve went
// red.
func (d *driverRun) fail(n int, msg string) {
	d.rep.Helper()

	d.rep.Errorf("plane %s: case %d: %s", d.plane.Name, n, msg)
}

// skip says out loud that a case did not run.
//
// [T] is two methods and neither of them is a log, deliberately: it is what
// *testing.T satisfies for free and what a probe can implement in four lines. So
// a skip is written where the reporter can carry one and is otherwise the
// silence it already was - which is why the reason is in each case's own
// documentation as well, where it cannot be lost.
func (d *driverRun) skip(n int, why string) {
	d.rep.Helper()

	l, ok := d.rep.(interface {
		Logf(format string, args ...any)
	})
	if !ok {
		return
	}

	l.Logf("plane %s: case %d skipped: %s", d.plane.Name, n, why)
}
