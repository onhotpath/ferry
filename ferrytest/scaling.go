package ferrytest

import (
	"strings"

	"github.com/onhotpath/ferry"
)

// declared is the one line a run says about itself before any case runs: what
// this plane was found to be able to do, and therefore how much of the suite is
// going to run against it.
//
// The suite scales to a driver rather than demanding one shape of it. Seven of
// the contract's interfaces are optional and are discovered by assertion, and
// every case that needs one skips where it is absent, so two conformant drivers
// can have very different numbers of cases actually execute. Without this line the
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

	r, ok := d.openedReader(inst)
	if !ok {
		return nil
	}

	defer closeIf(r)

	return declaredBy(r,
		implemented[ferry.Prober]("probes a container's own address"),
		implemented[ferry.Enumerator]("enumerates"),
		implemented[ferry.Releaser]("releases its reader"),
		implemented[ferry.Concurrent]("tolerates overlapping calls"),
	)
}

// openedReader is the read half, opened once, or nothing.
func (d *driverRun) openedReader(inst Instance) (ferry.Reader, bool) {
	d.rep.Helper()

	if inst.Source == nil {
		return nil, false
	}

	set, err := setOf[onlyLeaf](d.opts)
	if err != nil {
		return nil, false
	}

	open, err := inst.Source.Bind(set)
	if err != nil || open == nil {
		return nil, false
	}

	r, err := open(inst.ctx())

	return r, err == nil && r != nil
}

// writeCapabilities is the same question on the write half.
func (d *driverRun) writeCapabilities(inst Instance) []string {
	d.rep.Helper()

	w, ok := d.openedWriter(inst)
	if !ok {
		return nil
	}

	defer closeIf(w)

	return declaredBy(w,
		implemented[ferry.Ensurer]("ensures a container's own address"),
		implemented[ferry.Unsetter]("forgets a composite and everything under it"),
		implemented[ferry.Committer]("commits"),
		implemented[ferry.Releaser]("releases its writer"),
	)
}

// capability is one optional interface and what to call it in the line.
type capability struct {
	has  func(plane any) bool
	name string
}

// implemented builds one, in the one idiom the whole contract uses for an
// optional interface: ask the value in hand, never the description.
func implemented[T any](name string) capability {
	return capability{
		has: func(plane any) bool {
			_, ok := plane.(T)

			return ok
		},
		name: name,
	}
}

// declaredBy is what one half of a plane answered.
func declaredBy(plane any, of ...capability) []string {
	out := make([]string, 0, len(of))

	for _, c := range of {
		if c.has(plane) {
			out = append(out, c.name)
		}
	}

	return out
}

// openedWriter is [driverRun.openedReader] on the write half, and is a second
// function for the reason every other pair here is: a [ferry.Source] and a
// [ferry.Sink] are two interfaces with one method name and no common type.
func (d *driverRun) openedWriter(inst Instance) (ferry.Writer, bool) {
	d.rep.Helper()

	if inst.Sink == nil {
		return nil, false
	}

	set, err := setOf[onlyLeaf](d.opts)
	if err != nil {
		return nil, false
	}

	open, err := inst.Sink.Bind(set)
	if err != nil || open == nil {
		return nil, false
	}

	w, err := open(inst.ctx())

	return w, err == nil && w != nil
}
