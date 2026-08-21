//go:build protoc

package ferry_test

import (
	"context"
	"errors"
	"iter"
	"runtime"
	"testing"
	"time"

	"github.com/onhotpath/ferry"
)

// The eight scenarios, run against variant C: core mints the handle, the driver
// announces into it through a port, and the bind checks the two were wired to
// each other.

type watchConfigC struct {
	Host string `ferry:"host,required"`
}

type runner struct {
	values chan watchConfigC
	done   chan struct{}
	errf   func() error
}

func run(t *testing.T, seq iter.Seq[watchConfigC], errf func() error) *runner {
	t.Helper()

	r := &runner{values: make(chan watchConfigC), done: make(chan struct{}), errf: errf}

	go func() {
		defer close(r.done)

		for v := range seq {
			r.values <- v
		}
	}()

	return r
}

func (r *runner) next(t *testing.T) watchConfigC {
	t.Helper()

	select {
	case v := <-r.values:
		return v
	case <-r.done:
		t.Fatalf("the stream ended before a value arrived: %v", r.errf())
	case <-time.After(2 * time.Second):
		t.Fatal("no value arrived and the stream did not end")
	}

	return watchConfigC{}
}

// ended waits for the range to exit and reports what ended it. A stream that
// hangs fails here, which is the whole of scenario 5.
func (r *runner) ended(t *testing.T) error {
	t.Helper()

	select {
	case <-r.done:
		return r.errf()
	case <-time.After(2 * time.Second):
		t.Fatal("the stream did not end")
	}

	return nil
}

// wire is the whole caller wiring: open a watch, build a source over it, bind
// the two together.
func wire(t *testing.T, ctx context.Context) (*ferry.Watch, *portPlane, *ferry.WatchedBinding[watchConfigC]) {
	t.Helper()

	h := ferry.NewWatch(ctx)
	p := newPortPlane()
	p.Set(ferry.At("host"), ferry.String("db1"))
	p.wire(h)

	wb, err := ferry.BindWatched[watchConfigC](p, h)
	if err != nil {
		t.Fatalf("bind watched: %v", err)
	}

	return h, p, wb
}

func watchRun(t *testing.T, wb *ferry.WatchedBinding[watchConfigC], opts ...ferry.WatchOption) *runner {
	t.Helper()

	seq, errf := wb.Watch(opts...)

	return run(t, seq, errf)
}

// Scenario 1: a change that lands before BindWatched returns is not lost. The
// driver is already watching, so the handle records it, and the opening load
// reads what it announced.
func TestVariantCChangeBeforeBindIsNotLost(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	h := ferry.NewWatch(ctx)
	p := newPortPlane()
	p.Set(ferry.At("host"), ferry.String("db1"))
	p.wire(h)

	// The change lands while the driver is watching and nothing is bound, which
	// is the window the shipped API loses one in.
	p.Set(ferry.At("host"), ferry.String("db2"))

	wb, err := ferry.BindWatched[watchConfigC](p, h)
	if err != nil {
		t.Fatalf("bind watched: %v", err)
	}

	if got := watchRun(t, wb).next(t).Host; got != "db2" {
		t.Fatalf("first value is %q, want the change that landed before the bind returned", got)
	}
}

// Scenario 2: a burst is one reload, the reloaded value is fresh, and a value
// handed out earlier never changes underneath its holder.
func TestVariantCBurstCoalescesAndHeldValueIsImmutable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	_, p, wb := wire(t, ctx)
	r := watchRun(t, wb, ferry.Debounce(100*time.Millisecond))

	first := r.next(t)
	if first.Host != "db1" {
		t.Fatalf("first value is %q, want db1", first.Host)
	}

	for range 5 {
		p.Set(ferry.At("host"), ferry.String("db2"))
	}

	if got := r.next(t).Host; got != "db2" {
		t.Fatalf("reloaded value is %q, want db2", got)
	}

	if first.Host != "db1" {
		t.Fatalf("the value held from the first load says %q, so a reload mutated it", first.Host)
	}

	if got := p.Opens(); got != 2 {
		t.Fatalf("the plane was opened %d times, want 2: a burst of five is one reload", got)
	}
}

// Scenario 3: the stream opens with a load, and the same binding loads on
// demand, so there is one object and not three.
func TestVariantCStreamOpensWithALoad(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	_, p, wb := wire(t, ctx)

	v, err := wb.Load(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if v.Host != "db1" {
		t.Fatalf("the load produced %q, want db1", v.Host)
	}

	if got := watchRun(t, wb).next(t).Host; got != "db1" {
		t.Fatalf("the stream opened with %q, want the plane's current contents", got)
	}

	if got := p.Opens(); got != 2 {
		t.Fatalf("the plane was opened %d times, want 2: one load and one stream opening", got)
	}
}

// Scenario 4: a failed reload ends the stream and errf says so. Recovery is not
// expressible on the same handle, because a handle is one stream: the caller
// opens a new watch and binds again.
func TestVariantCFailedReloadIsObservedAndRestartsOnANewHandle(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	_, p, wb := wire(t, ctx)
	r := watchRun(t, wb)
	r.next(t)

	p.Delete(ferry.At("host"))

	if err := r.ended(t); !errors.Is(err, ferry.ErrMissing) {
		t.Fatalf("the stream ended with %v, want a missing required field", err)
	}

	// The same handle refuses a second stream, so recovery is a new one.
	seq, errf := wb.Watch()
	for range seq {
		t.Fatal("a second stream over one handle yielded a value")
	}

	if err := errf(); !errors.Is(err, ferry.ErrWatchInUse) {
		t.Fatalf("the second stream reported %v, want a refusal", err)
	}

	h2 := ferry.NewWatch(ctx)
	p2 := newPortPlane()
	p2.Set(ferry.At("host"), ferry.String("db3"))
	p2.wire(h2)

	wb2, err := ferry.BindWatched[watchConfigC](p2, h2)
	if err != nil {
		t.Fatalf("second bind: %v", err)
	}

	if got := watchRun(t, wb2).next(t).Host; got != "db3" {
		t.Fatalf("the restarted stream opened with %q, want db3", got)
	}
}

// Scenario 5: the driver loses its watch and says so, and the caller observes
// it. This is the M2 killer.
func TestVariantCQuietDeathIsObserved(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	_, p, wb := wire(t, ctx)
	r := watchRun(t, wb)
	r.next(t)

	boom := errors.New("the directory holding this file was removed")
	p.Lose(boom)

	err := r.ended(t)
	if !errors.Is(err, ferry.ErrWatchLost) || !errors.Is(err, boom) || !errors.Is(err, ferry.ErrPlane) {
		t.Fatalf("the stream ended with %v, want a lost watch carrying the driver's own reason", err)
	}
}

// Scenario 5b: a driver that ends its watch with no reason still ends the
// stream, so there is no silence even where there is no fault.
func TestVariantCEndingWithNoReasonStillEndsTheStream(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	_, p, wb := wire(t, ctx)
	r := watchRun(t, wb)
	r.next(t)

	p.Lose(nil)

	if err := r.ended(t); !errors.Is(err, ferry.ErrWatchLost) {
		t.Fatalf("the stream ended with %v, want a watch that is over", err)
	}
}

// Scenario 6: one context is the whole wiring, and cancelling it ends the
// stream and leaves no goroutine behind.
func TestVariantCOneLifetimeLeaksNothing(t *testing.T) {
	before := goroutines()

	ctx, cancel := context.WithCancel(context.Background())
	_, _, wb := wire(t, ctx)
	r := watchRun(t, wb)
	r.next(t)

	cancel()

	if err := r.ended(t); !errors.Is(err, context.Canceled) {
		t.Fatalf("the stream ended with %v, want the cancellation", err)
	}

	if after := goroutines(); after > before {
		t.Fatalf("goroutines went from %d to %d, so the watch leaked one", before, after)
	}
}

func goroutines() int {
	for range 50 {
		runtime.Gosched()
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}

	return runtime.NumGoroutine()
}

// Scenario 7: a second stream over one handle is policed.
func TestVariantCSecondStreamIsRefused(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	_, _, wb := wire(t, ctx)

	seq, errf := wb.Watch()

	var seen int

	for range seq {
		seen++

		break
	}

	if seen != 1 || errf() != nil {
		t.Fatalf("the first stream yielded %d values and reported %v, want 1 and a clean ending", seen, errf())
	}

	second, secondErr := wb.Watch()
	for range second {
		t.Fatal("a second stream over one handle yielded a value")
	}

	if err := secondErr(); !errors.Is(err, ferry.ErrWatchInUse) {
		t.Fatalf("the second stream reported %v, want a refusal", err)
	}
}

// Scenario 7b: a handle no driver was given is refused at BindWatched.
func TestVariantCHandleWiredToNothingIsRefusedAtBind(t *testing.T) {
	t.Parallel()

	h := ferry.NewWatch(t.Context())
	p := newPortPlane()
	p.Set(ferry.At("host"), ferry.String("db1"))

	_, err := ferry.BindWatched[watchConfigC](p, h)
	if !errors.Is(err, ferry.ErrWatchNotWired) {
		t.Fatalf("binding a handle nobody was given reported %v, want a refusal at bind", err)
	}
}

// Scenario 7c: a handle wired to one source and bound against another is
// refused at BindWatched, which is the mistake nothing in the shipped API
// catches at all.
func TestVariantCHandleWiredToADifferentSourceIsRefusedAtBind(t *testing.T) {
	t.Parallel()

	h := ferry.NewWatch(t.Context())
	watched := newPortPlane().wire(h)
	watched.Set(ferry.At("host"), ferry.String("db1"))

	other := newPortPlane()
	other.Set(ferry.At("host"), ferry.String("db1"))

	_, err := ferry.BindWatched[watchConfigC](other, h)
	if !errors.Is(err, ferry.ErrWatchNotWired) {
		t.Fatalf("binding a handle wired to another source reported %v, want a refusal at bind", err)
	}
}

// Scenario 7d: a driver that was given the handle and cannot watch declines,
// and the refusal lands at BindWatched carrying its own reason.
func TestVariantCDriverRefusalLandsAtBind(t *testing.T) {
	t.Parallel()

	h := ferry.NewWatch(t.Context())
	p := newPortPlane().wire(h)
	p.Set(ferry.At("host"), ferry.String("db1"))

	boom := errors.New("this source watches no files")
	p.Refuse(boom)

	_, err := ferry.BindWatched[watchConfigC](p, h)
	if !errors.Is(err, ferry.ErrNotWatchable) || !errors.Is(err, boom) {
		t.Fatalf("binding a source that declined the watch reported %v, want its own reason", err)
	}
}

// A nil handle is refused rather than dereferenced.
func TestVariantCNilHandleIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := ferry.BindWatched[watchConfigC](newPortPlane(), nil); !errors.Is(err, ferry.ErrWatchNotWired) {
		t.Fatalf("binding a nil handle reported %v, want a refusal", err)
	}
}

// Scenario 8: #361, two watchable planes layered under one binding and one
// handle.
//
// One handle is the whole answer to coordination here: both planes announce
// into the same pending slot, so two announcements of one deployment are
// already one change before any debounce is applied.
func TestVariantCTwoWatchableSourcesUnderOneBinding(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	h := ferry.NewWatch(ctx)
	l := newLayeredPlane()
	l.under.Set(ferry.At("host"), ferry.String("from-under"))
	l.wire(h)

	wb, err := ferry.BindWatched[watchConfigC](l, h)
	if err != nil {
		t.Fatalf("bind watched: %v", err)
	}

	r := watchRun(t, wb, ferry.Debounce(100*time.Millisecond))

	if got := r.next(t).Host; got != "from-under" {
		t.Fatalf("first value is %q, want the lower layer's", got)
	}

	l.under.Set(ferry.At("host"), ferry.String("stale"))
	l.over.Set(ferry.At("host"), ferry.String("from-over"))

	if got := r.next(t).Host; got != "from-over" {
		t.Fatalf("the reloaded value is %q, want the upper layer's", got)
	}

	if got := l.over.Opens() + l.under.Opens(); got != 4 {
		t.Fatalf("the two planes were opened %d times, want 4: two loads over two layers", got)
	}
}

// One layer losing its watch ends the whole stream, because there is one handle
// and one ending. That is a decision #361 has to make, and this variant makes
// it by construction.
func TestVariantCOneLayerLosingItsWatchEndsTheStream(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	h := ferry.NewWatch(ctx)
	l := newLayeredPlane()
	l.under.Set(ferry.At("host"), ferry.String("from-under"))
	l.wire(h)

	wb, err := ferry.BindWatched[watchConfigC](l, h)
	if err != nil {
		t.Fatalf("bind watched: %v", err)
	}

	r := watchRun(t, wb)
	r.next(t)

	boom := errors.New("the upper layer lost its watch")
	l.over.Lose(boom)

	if err := r.ended(t); !errors.Is(err, boom) {
		t.Fatalf("the stream ended with %v, want the layer's own reason", err)
	}
}

// A WatchOption list that does not resolve ends the stream rather than
// returning an error nobody asked for.
func TestVariantCBadWatchOptionEndsTheStream(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	_, _, wb := wire(t, ctx)

	seq, errf := wb.Watch(ferry.Debounce(-1))
	for range seq {
		t.Fatal("a stream built from a refused option yielded a value")
	}

	if err := errf(); !errors.Is(err, ferry.ErrSchema) {
		t.Fatalf("the stream reported %v, want the refused option", err)
	}
}
