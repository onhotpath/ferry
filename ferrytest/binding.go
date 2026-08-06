package ferrytest

import (
	"context"
	"fmt"
	"sync"

	"github.com/onhotpath/ferry"
)

// caseConcurrentOpen is case 14: one binding, many goroutines opening the plane
// through it at once (ADR-0004).
//
// A binding is process-lived and shared, so the open closure a driver hands back
// is entered from many goroutines as a matter of course. A driver that writes to
// what it closed over - a buffer, a last-error field, a lazily built table - was
// correct when a bind lived and died inside one Load and is wrong now, and the
// obligation is on the open rather than merely tolerated.
//
// What this case creates is the concurrency; what reports a data race is the
// caller's own `go test -race`, which is where a driver's CI runs it. So the
// assertion here is the observable half: every open succeeds, and every load
// through the binding returns what the plane holds rather than what another
// goroutine's open was in the middle of.
//
// Every half runs once on its own first, and the case is skipped where that
// fails: a plane that cannot be opened at all says nothing about opening it
// twice at once, and the cases that own a broken open report it already.
//
// The write half opens concurrently and walks nothing, deliberately. ADR-0004
// obligates concurrent calls to OpenWriterFunc and says nothing about a plane
// tolerating two dumps at once, which is a different question with a home of its
// own; a case that dumped from two goroutines would be answering it by accident.
func (d *driverRun) caseConcurrentOpen() {
	d.rep.Helper()

	set, err := setOf[onlyLeaf](d.opts)
	if err != nil {
		d.fail(caseConcurrentNo, "compiling the suite's own fixture: "+err.Error())

		return
	}

	d.concurrentLoads()
	d.concurrentOpens(set)
}

// concurrentLoads is the read half, through the shape a caller actually holds:
// one Binding, Load from many goroutines.
func (d *driverRun) concurrentLoads() {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Source == nil {
		return
	}

	if inst.Sink != nil {
		if err := ferry.Dump(inst.ctx(), onlyLeaf{Leaf: concurrentValue}, inst.Sink, d.opts...); err != nil {
			d.skip(caseConcurrentNo, "the fixture could not be dumped, so there is nothing to load "+
				"concurrently: "+err.Error())

			return
		}
	}

	b, err := ferry.Bind[onlyLeaf](inst.Source, d.opts...)
	if err != nil {
		d.skip(caseConcurrentNo, "the source refused the address set, which case 2 owns: "+err.Error())

		return
	}

	if _, err := b.Load(inst.ctx()); err != nil {
		d.skip(caseConcurrentNo, "one load on its own failed, so nothing here would be measuring "+
			"concurrency: "+err.Error())

		return
	}

	d.inParallel(func() string { return oneConcurrentLoad(b, inst) })
}

// oneConcurrentLoad is one of the goroutines: the same load the sequential one
// above already succeeded at.
//
// It returns what went wrong rather than reporting it, because [T] is two
// methods with nothing said about calling them from two goroutines, and a suite
// that called a caller's reporter concurrently would be requiring something of
// every implementation of it that ADR-0014 never asked for.
func oneConcurrentLoad(b *ferry.Binding[onlyLeaf], inst Instance) string {
	got, err := b.Load(inst.ctx())
	if err != nil {
		return fmt.Sprintf("one of %d concurrent loads through one binding failed with %v, where the same "+
			"load on its own succeeded: the open a binding holds is entered from many goroutines at once",
			concurrentOpens, err)
	}

	if inst.Sink != nil && got.Leaf != concurrentValue {
		return fmt.Sprintf("one of %d concurrent loads read %q, want %q: an open sharing mutable state with "+
			"another open answers out of whichever one ran last", concurrentOpens, got.Leaf, concurrentValue)
	}

	return ""
}

// concurrentOpens is the obligation as ADR-0004 states it, on both halves: the
// open closure itself, entered concurrently, handing back a usable plane every
// time.
//
// It reaches the closure directly rather than through a binding, because on the
// write side that is the whole of what is obligated and a dump would be asking
// something else.
func (d *driverRun) concurrentOpens(set *ferry.AddressSet) {
	d.rep.Helper()

	inst := d.plane.Open()

	d.readOpensInParallel(inst, set)
	d.writeOpensInParallel(inst, set)
}

// readOpensInParallel and writeOpensInParallel are the two halves, and they are
// two functions because a [ferry.Source] and a [ferry.Sink] are two interfaces
// with one method name and no common type.
func (d *driverRun) readOpensInParallel(inst Instance, set *ferry.AddressSet) {
	d.rep.Helper()

	if inst.Source == nil {
		return
	}

	open, ok := d.bound(inst.Source.Bind(set))
	if !ok {
		return
	}

	d.openInParallel(inst.ctx(), "a reader", func(ctx context.Context) (any, error) { return open(ctx) })
}

func (d *driverRun) writeOpensInParallel(inst Instance, set *ferry.AddressSet) {
	d.rep.Helper()

	if inst.Sink == nil {
		return
	}

	open, ok := d.boundSink(inst.Sink.Bind(set))
	if !ok {
		return
	}

	d.openInParallel(inst.ctx(), "a writer", func(ctx context.Context) (any, error) { return open(ctx) })
}

// bound and boundSink turn one half's Bind into an open this case can use, and
// are silent about a Bind that refused, which is case 2's and case 7's.
func (d *driverRun) bound(open ferry.OpenFunc, err error) (ferry.OpenFunc, bool) {
	d.rep.Helper()

	if err != nil {
		d.skip(caseConcurrentNo, "the source refused the address set, which case 2 owns: "+err.Error())

		return nil, false
	}

	return open, true
}

func (d *driverRun) boundSink(open ferry.OpenWriterFunc, err error) (ferry.OpenWriterFunc, bool) {
	d.rep.Helper()

	if err != nil {
		d.skip(caseConcurrentNo, "the sink refused the address set, which case 2 owns: "+err.Error())

		return nil, false
	}

	return open, true
}

// openInParallel enters one open closure from many goroutines and releases
// everything it hands back.
//
// One open runs first, on its own, and the parallel half is skipped where that
// fails: an open that cannot succeed once has nothing to say about eight at
// once, and cases 4, 6 and 10 are where a broken open is reported.
func (d *driverRun) openInParallel(ctx context.Context, half string, open func(context.Context) (any, error)) {
	d.rep.Helper()

	plane, err := open(ctx)
	closeIf(plane)

	if err != nil {
		d.skip(caseConcurrentNo, "opening "+half+" once, on its own, failed: "+err.Error())

		return
	}

	d.inParallel(func() string {
		plane, err := open(ctx)
		closeIf(plane)

		if err != nil {
			return fmt.Sprintf("one of %d concurrent opens of %s failed with %v, where the same open on its "+
				"own succeeded: a binding's opens are obligated to be safe from many goroutines at once",
				concurrentOpens, half, err)
		}

		return ""
	})
}

// inParallel runs one body from several goroutines at once, waits for all of
// them, and reports what they found afterwards.
//
// The start is gated on one channel rather than staggered, because opens that
// never overlap are opens this case did not test, and a race detector needs the
// overlap as much as the assertion does.
//
// Nothing reports from inside a goroutine. [T] is two methods and ADR-0014 says
// nothing about calling them from two at once, so a suite that did would be
// putting an obligation on every reporter a caller can pass - and this package's
// own probe reporters, which are four lines each, are exactly the ones that
// would not carry it.
func (d *driverRun) inParallel(body func() string) {
	d.rep.Helper()

	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
		found = make([]string, concurrentOpens)
	)

	for i := range concurrentOpens {
		wg.Go(func() {
			<-start
			found[i] = body()
		})
	}

	close(start)
	wg.Wait()

	for _, msg := range found {
		if msg != "" {
			d.fail(caseConcurrentNo, msg)
		}
	}
}

// concurrentOpens is how many goroutines case 14 runs, and concurrentValue is
// what the read half writes and reads back.
const (
	concurrentOpens = 8
	concurrentValue = "x"
)

// caseSecondDump is case 15: a second dump through one held sink binding, with a
// different value, refused by neither the first one's minted addresses nor its
// writes (ADR-0004).
//
// A binding is reusable without limit, and neither half of it is spent by having
// been used. Case 8 asks that two opens minting different addresses do not
// collide; this asks the ordinary thing underneath it, which is a program
// holding one binding and saving its configuration again.
//
// The second value is different at the same addresses, because that is what a
// re-save is. A sink refusing it has not made a mistake about injectivity - the
// two writes are at different times, and ADR-0004 does not require those to be
// mutually injective - it has kept a record belonging to the open it was made
// in.
func (d *driverRun) caseSecondDump() {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Sink == nil {
		return
	}

	b, err := ferry.BindSink[filled](inst.Sink, d.opts...)
	if err != nil {
		d.skip(caseSecondDumpNo, "the sink refused the address set, which case 2 owns: "+err.Error())

		return
	}

	if err := b.Dump(inst.ctx(), filledFixture()); err != nil {
		d.skip(caseSecondDumpNo, "the first dump failed, so there is no second one to ask about, and case 1 "+
			"is where a dump that cannot happen is reported: "+err.Error())

		return
	}

	if err := b.Dump(inst.ctx(), secondFixture()); err != nil {
		d.fail(caseSecondDumpNo, "the second dump through the same binding failed with "+err.Error()+
			": a binding is not spent by having been dumped through, and a record of what one open wrote "+
			"is what refuses the next one")

		return
	}

	d.secondDumpLanded(inst)
}

// secondDumpLanded is the other half: what the plane holds afterwards is the
// second dump.
//
// A sink that took the second dump and quietly kept the first would report
// success for a save that did not happen, which is worse than the refusal above
// because nothing says so. It is read at the fixture's leaf and nowhere else,
// so a plane that cannot enumerate is asked nothing it cannot answer.
func (d *driverRun) secondDumpLanded(inst Instance) {
	d.rep.Helper()

	if inst.Source == nil {
		return
	}

	got, err := ferry.Load[onlyLeaf](inst.ctx(), inst.Source, d.opts...)
	if err != nil {
		d.skip(caseSecondDumpNo, "the leaf could not be read back, so what the second dump left cannot be "+
			"compared: "+err.Error())

		return
	}

	if got.Leaf == secondLeaf {
		return
	}

	d.fail(caseSecondDumpNo, fmt.Sprintf("after two dumps the plane holds %q at %s, want the second dump's "+
		"%q: a sink that takes a re-save and keeps the earlier value reports success for a save that did "+
		"not happen", got.Leaf, addrLeaf, secondLeaf))
}
