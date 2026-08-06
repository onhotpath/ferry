package ferrytest

import (
	"strings"

	"github.com/onhotpath/ferry"
)

// declared is the one line a run says about itself before any case runs: what
// this plane was found to be able to do, and therefore how much of the suite is
// going to run against it.
//
// The suite scales to a driver rather than demanding one shape of it. Six of the
// contract's interfaces are optional and are discovered by assertion, and every
// case that needs one skips where it is absent, so two conformant drivers can
// have very different numbers of cases actually execute. Without this line the
// difference is invisible: a case that quietly did nothing reads exactly like a
// case that passed, and a driver author has no way to see that the obligation
// their design does carry went unmeasured.
//
// It is discovery and not description. Which optional interfaces a driver
// implements is a question its own reader and writer answer, so [Plane] carries
// no field for any of them and none can be declared wrongly; what the
// description says is only what cannot be discovered - what the plane is called,
// which kinds it carries end to end, what it cannot spell inside them, how to
// mint a fresh one, and what its own spelling of a fixed value looks like.
func (d *driverRun) declared() {
	d.rep.Helper()

	inst := d.plane.Open()

	have := []string{"a source", "a sink", "a per-request plane", "readable contents", "a pinned spelling"}
	found := []bool{
		inst.Source != nil,
		inst.Sink != nil,
		inst.InContext != nil,
		inst.Contents != nil,
		len(d.plane.Golden) > 0,
	}

	out := make([]string, 0, len(have))

	for i, name := range have {
		if found[i] {
			out = append(out, name)
		}
	}

	out = append(out, d.readCapabilities(inst)...)
	out = append(out, d.writeCapabilities(inst)...)

	d.logf("plane %s: the suite scaled to: %s", d.plane.Name, strings.Join(out, ", "))

	// The two halves are worth a sentence of their own, because a missing half
	// silences whole cases rather than narrowing one. A plane with no honest
	// Dump is ADR-0004's own case and a description rather than a defect.
	if inst.Sink == nil {
		d.logf("plane %s: the plane mints no sink, so every case that writes is silent for it", d.plane.Name)
	}

	if inst.Source == nil {
		d.logf("plane %s: the plane mints no source, so every case that reads is silent for it", d.plane.Name)
	}
}

// readCapabilities opens the read half once and asks it what it implements.
//
// A plane that cannot be opened answers nothing here and is silent about it: an
// open that fails is cases 4, 6 and 10's, and this line is not a case.
func (d *driverRun) readCapabilities(inst Instance) []string {
	d.rep.Helper()

	if inst.Source == nil {
		return nil
	}

	set, err := setOf[onlyLeaf](d.opts)
	if err != nil {
		return nil
	}

	open, err := inst.Source.Bind(set)
	if err != nil || open == nil {
		return nil
	}

	r, err := open(inst.ctx())
	if err != nil || r == nil {
		return nil
	}

	defer closeIf(r)

	var out []string

	if _, ok := r.(ferry.Prober); ok {
		out = append(out, "probes a container's own address")
	}

	if _, ok := r.(ferry.Enumerator); ok {
		out = append(out, "enumerates")
	}

	if _, ok := r.(ferry.Releaser); ok {
		out = append(out, "releases its reader")
	}

	return out
}

// writeCapabilities is the same question on the write half.
func (d *driverRun) writeCapabilities(inst Instance) []string {
	d.rep.Helper()

	if inst.Sink == nil {
		return nil
	}

	set, err := setOf[onlyLeaf](d.opts)
	if err != nil {
		return nil
	}

	open, err := inst.Sink.Bind(set)
	if err != nil || open == nil {
		return nil
	}

	w, err := open(inst.ctx())
	if err != nil || w == nil {
		return nil
	}

	defer closeIf(w)

	var out []string

	if _, ok := w.(ferry.Ensurer); ok {
		out = append(out, "ensures a container's own address")
	}

	if _, ok := w.(ferry.Committer); ok {
		out = append(out, "commits")
	}

	if _, ok := w.(ferry.Releaser); ok {
		out = append(out, "releases its writer")
	}

	return out
}
