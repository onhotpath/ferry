package ferrytest

import "github.com/onhotpath/ferry"

// caseRootLeaf is case 23: a type that resolves to a leaf names one address, the
// root, and a plane either has a name for it or refuses it at Bind (ADR-0003,
// ADR-0004, ADR-0010).
//
// The root address is the empty path. It carries no segment, so a plane that
// builds its keys by joining segments has nothing to join and no key to write
// at - which is a legitimate answer and not a defect. What is a defect is
// taking the dump anyway: a value written at no key at all is a total loss with
// a nil error, and that is the shape this case exists to catch.
//
// So the two answers it accepts are a refusal, which it says out loud and skips
// on, and a round trip. What fails is the shape between them: a plane that took
// the dump, returned no error and holds nothing.
//
// A refusal at Bind is the expected shape, because that is where the caller
// still has the whole schema in hand and nothing has been asked of the plane. A
// refusal at the write is loud too and loses nothing, so it is a skip that says
// where the refusal came from rather than a failure.
//
// It is skipped, out loud, for a plane that cannot carry [ferry.KindString],
// because the value it round trips has to be one the plane declared it holds.
func (d *driverRun) caseRootLeaf() {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Sink == nil || inst.Source == nil {
		return
	}

	if !d.carries(ferry.String(rootLeafValue)) {
		d.skip(caseRootLeafNo, "the plane does not carry the String this case round trips at the root, so "+
			"there is no value to put there that it declared it can hold")

		return
	}

	if !d.namesTheRoot(inst) {
		return
	}

	d.rootLeafRoundTrips(inst)
}

// rootLeafValue is what this case writes at the root. It is a String because
// that is the one kind every plane with a write half carries.
const rootLeafValue = "root"

// namesTheRoot asks the write half whether this plane has a name for the root
// address at all, which is the question ADR-0004 puts at Bind: the schema is
// the whole of what is being refused, and nothing has been written yet.
//
// A refusal is the expected answer for every plane whose keys are segments
// joined together, and it is a skip rather than a failure. It is not narrowed
// to a class, because core marks every refusal a driver makes at Bind
// [ferry.ErrPlane] before the caller sees it, so which sentinel came back is
// core's answer and not this plane's.
func (d *driverRun) namesTheRoot(inst Instance) bool {
	d.rep.Helper()

	_, err := ferry.BindSink[string](inst.Sink, d.opts...)
	if err == nil {
		return true
	}

	d.skip(caseRootLeafNo, "this plane has no name for the root address and refused it at Bind, which is "+
		"the expected shape for a plane whose keys are built out of segments: "+err.Error())

	return false
}

// rootLeafRoundTrips is the other answer: a plane that took the bind holds the
// value afterwards and reads it back.
//
// The load is the half that catches the loss. A dump that wrote nowhere returns
// a nil error, so the write side alone cannot tell it from a dump that landed.
func (d *driverRun) rootLeafRoundTrips(inst Instance) {
	d.rep.Helper()

	if err := ferry.Dump(inst.ctx(), rootLeafValue, inst.Sink, d.opts...); err != nil {
		d.skip(caseRootLeafNo, "this plane bound a schema whose only address is the root and then refused "+
			"the write: "+err.Error()+". Nothing is lost by that, and Bind is where the same refusal "+
			"costs the caller least, so this is the answer late rather than a wrong one")

		return
	}

	got, err := ferry.Load[string](inst.ctx(), inst.Source, d.opts...)
	if err != nil {
		d.fail(caseRootLeafNo, "loading back the value dumped at the root failed with "+err.Error()+
			": the two halves name one address and this one is the address they disagree about")

		return
	}

	if got != rootLeafValue {
		d.fail(caseRootLeafNo, "the root holds "+got+" after a dump of "+rootLeafValue+
			": a dump at the root that lands nowhere reports no error, so this is where the loss shows")
	}
}
