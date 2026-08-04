package ferrytest

import (
	"context"
	"errors"
	"fmt"

	"github.com/onhotpath/ferry"
)

// caseLifecycle is case 6: Commit runs only on success, Close always runs, and a
// Close failure appears in the reported error set (ADR-0004).
//
// That protocol is the whole of ferry's lifecycle, and it is why neither method
// takes a cause: no driver is ever told that it failed, and
// closed-without-Commit is the abort signal, in the shape sql.Tx and
// bufio.Writer already have.
//
// Both interfaces are optional and discovered by assertion, so the sink is
// wrapped in a shell carrying exactly the interfaces the driver's own writer
// carries. A shell that answered yes to both would be asserting about itself: a
// no-op Commit and a no-op Close return nil, which is what a writer without them
// produces anyway, so nothing under [ferry.Dump] would tell the two apart.
func (d *driverRun) caseLifecycle() {
	d.rep.Helper()

	d.lifecycleOnSuccess()
	d.lifecycleOnFailure()
	d.lifecycleOnCloseFailure()
}

// lifecycleOnSuccess is the half a driver notices first: a walk that succeeded
// commits once and closes once.
func (d *driverRun) lifecycleOnSuccess() {
	d.rep.Helper()

	s, err := d.dumpThroughLifecycle(lifecycleProbe{})
	if s == nil {
		return
	}

	if err != nil {
		d.fail(caseLifecycleNo, fmt.Sprintf("dumping one leaf failed with %v, so the lifecycle cannot be "+
			"observed against a walk that succeeded", err))

		return
	}

	if s.stages && s.commits != 1 {
		d.fail(caseLifecycleNo, fmt.Sprintf("a walk that succeeded committed %d times, want once: a staging "+
			"sink holds nothing durable until it commits", s.commits))
	}

	if s.releases && s.closes != 1 {
		d.fail(caseLifecycleNo, fmt.Sprintf("a walk that succeeded closed %d times, want once", s.closes))
	}
}

// lifecycleOnFailure is the half that matters: a walk that failed must not
// commit, and must still close.
//
// The failure is staged above the driver rather than inside it, because a
// driver's own refusal is the driver's business and what is under test here is
// the protocol around it.
func (d *driverRun) lifecycleOnFailure() {
	d.rep.Helper()

	s, err := d.dumpThroughLifecycle(lifecycleProbe{setErr: errProbeSet})
	if s == nil {
		return
	}

	if err == nil {
		d.fail(caseLifecycleNo, "a Set that failed was reported as a dump that succeeded")

		return
	}

	if s.commits != 0 {
		d.fail(caseLifecycleNo, "a walk that failed committed anyway: Commit runs only where the walk "+
			"succeeded, and closed-without-Commit is the abort signal a driver reads")
	}

	if s.releases && s.closes != 1 {
		d.fail(caseLifecycleNo, fmt.Sprintf("a walk that failed closed %d times, want once: Close runs whether "+
			"the walk succeeded or failed, or the temporary file is what leaks", s.closes))
	}
}

// lifecycleOnCloseFailure is the third clause: a Close that fails is a failure
// the caller is told about.
//
// It is skipped for a driver whose writer holds no resource, because there is
// then no Close for core to call and nothing this case could stage.
func (d *driverRun) lifecycleOnCloseFailure() {
	d.rep.Helper()

	s, err := d.dumpThroughLifecycle(lifecycleProbe{closeErr: errProbeClose})
	if s == nil {
		return
	}

	if !s.releases {
		d.skip(caseLifecycleNo, "the plane's writer holds no resource, so it implements no Close for a failing "+
			"one to be reported from")

		return
	}

	if errors.Is(err, errProbeClose) {
		return
	}

	d.fail(caseLifecycleNo, fmt.Sprintf("a Close that failed left the dump reporting %v: cleanup that fails is "+
		"reported, because a temporary that could not be renamed is a dump that did not happen", err))
}

// dumpThroughLifecycle runs one leaf through a wrapped sink and hands back what
// the wrapper saw. A nil result is a plane with no sink, already reported
// elsewhere and silent here.
func (d *driverRun) dumpThroughLifecycle(probe lifecycleProbe) (*lifecycleSink, error) {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Sink == nil {
		return nil, errNoSink
	}

	s := &lifecycleSink{inner: inst.Sink, probe: probe}

	return s, ferry.Dump(inst.ctx(), onlyLeaf{Leaf: "x"}, s, d.opts...)
}

// lifecycleProbe is what one run of the lifecycle case stages: an error from Set
// to fail the walk, an error from Close to be looked for in the report, or
// neither.
type lifecycleProbe struct {
	setErr   error
	closeErr error
}

// lifecycleSink wraps a driver's sink and counts the two lifecycle calls core
// makes on it.
type lifecycleSink struct {
	inner ferry.Sink
	probe lifecycleProbe

	// stages and releases are what the driver's own writer answered to the two
	// assertions, recorded so that the case asserts about a staging sink only
	// where there is one.
	stages   bool
	releases bool

	commits int
	closes  int
}

// Bind hands the address set straight through: a shell has no key function of
// its own and nothing it could legitimately refuse.
func (s *lifecycleSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Writer, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}

		_, s.stages = w.(ferry.Committer)
		_, s.releases = w.(ferry.Releaser)

		return shellWriter(lifecycleWriter{inner: w, sink: s}, w), nil
	}, nil
}

// lifecycleWriter is the counting front of the shell. It declares Commit and
// Close unconditionally, and [shellWriter] is what decides whether the shell
// around it carries them, so what core sees is still exactly what the driver
// implements.
type lifecycleWriter struct {
	inner ferry.Writer
	sink  *lifecycleSink
}

// Set writes, or fails the walk where this run staged a failure.
func (w lifecycleWriter) Set(ctx context.Context, addr ferry.Path, v ferry.Value) error {
	if w.sink.probe.setErr != nil {
		return w.sink.probe.setErr
	}

	return w.inner.Set(ctx, addr, v)
}

// Commit counts and forwards.
func (w lifecycleWriter) Commit(ctx context.Context) error {
	w.sink.commits++

	c, ok := w.inner.(ferry.Committer)
	if !ok {
		return nil
	}

	return c.Commit(ctx)
}

// Close counts, forwards, and then fails where this run staged a failing Close.
func (w lifecycleWriter) Close() error {
	w.sink.closes++

	if c, ok := w.inner.(ferry.Releaser); ok {
		if err := c.Close(); err != nil {
			return err
		}
	}

	return w.sink.probe.closeErr
}
