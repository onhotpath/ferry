//go:build protoa

package ferry_test

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/onhotpath/ferry"
)

// The eight scenarios every variant of the typed watch prototype is judged on,
// run against variant A: the transition is a method on the binding, and the
// capability is asserted there.

type watchConfigA struct {
	Host string `ferry:"host,required"`
}

// runner ranges a stream on a goroutine of its own and hands the values over one
// at a time, so a test can interleave a plane write with a read.
type runner struct {
	values chan watchConfigA
	done   chan struct{}
	w      *ferry.Watched[watchConfigA]
}

func run(t *testing.T, w *ferry.Watched[watchConfigA]) *runner {
	t.Helper()

	r := &runner{values: make(chan watchConfigA), done: make(chan struct{}), w: w}

	go func() {
		defer close(r.done)

		for v := range w.Values() {
			r.values <- v
		}
	}()

	return r
}

// next takes one value, failing the test rather than hanging for ever if the
// stream has ended or gone quiet.
func (r *runner) next(t *testing.T) watchConfigA {
	t.Helper()

	select {
	case v := <-r.values:
		return v
	case <-r.done:
		t.Fatalf("the stream ended before a value arrived: %v", r.w.Err())
	case <-time.After(2 * time.Second):
		t.Fatal("no value arrived and the stream did not end")
	}

	return watchConfigA{}
}

// ended waits for the range to exit and reports what ended it. A stream that
// hangs fails here, which is the whole of scenario 5.
func (r *runner) ended(t *testing.T) error {
	t.Helper()

	select {
	case <-r.done:
		return r.w.Err()
	case <-time.After(2 * time.Second):
		t.Fatal("the stream did not end")
	}

	return nil
}

func watchedA(t *testing.T, ctx context.Context, src ferry.Source,
	opts ...ferry.WatchOption,
) *ferry.Watched[watchConfigA] {
	t.Helper()

	b, err := ferry.Bind[watchConfigA](src)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	w, err := b.Watch(ctx, opts...)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	return w
}

func planeA(t *testing.T) *armedPlane {
	t.Helper()

	p := newArmedPlane()
	p.Set(ferry.At("host"), ferry.String("db1"))

	return p
}

// Scenario 1: a change that lands before the watch is opened is not lost,
// because the stream opens with a load rather than with a wait.
func TestVariantAChangeBeforeWatchIsNotLost(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeA(t)

	b, err := ferry.Bind[watchConfigA](p)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	// The change lands between the bind and the watch, which is the window the
	// shipped API loses one in.
	p.Set(ferry.At("host"), ferry.String("db2"))

	w, err := b.Watch(ctx)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	r := run(t, w)

	if got := r.next(t).Host; got != "db2" {
		t.Fatalf("first value is %q, want the change that landed before the watch opened", got)
	}
}

// Scenario 2: a burst is one reload, the reloaded value is fresh, and a value
// handed out earlier never changes underneath its holder.
func TestVariantABurstCoalescesAndHeldValueIsImmutable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeA(t)
	r := run(t, watchedA(t, ctx, p, ferry.Debounce(100*time.Millisecond)))

	first := r.next(t)
	if first.Host != "db1" {
		t.Fatalf("first value is %q, want db1", first.Host)
	}

	for range 5 {
		p.Set(ferry.At("host"), ferry.String("db2"))
	}

	second := r.next(t)
	if second.Host != "db2" {
		t.Fatalf("reloaded value is %q, want db2", second.Host)
	}

	if first.Host != "db1" {
		t.Fatalf("the value held from the first load says %q, so a reload mutated it", first.Host)
	}

	if got := p.Opens(); got != 2 {
		t.Fatalf("the plane was opened %d times, want 2: a burst of five is one reload", got)
	}
}

// Scenario 3: the stream opens with a load, so there is no pre-load to write and
// no second idiom for the first value.
func TestVariantAStreamOpensWithALoad(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeA(t)
	r := run(t, watchedA(t, ctx, p))

	if got := r.next(t).Host; got != "db1" {
		t.Fatalf("first value is %q, want the plane's current contents with no change announced", got)
	}

	if got := p.Opens(); got != 1 {
		t.Fatalf("the plane was opened %d times for one value, want 1", got)
	}
}

// Scenario 4: a failed reload ends the stream, says so through Err, and the
// caller's recovery is a second Watch over the same binding.
func TestVariantAFailedReloadIsObservedAndRestartable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeA(t)

	b, err := ferry.Bind[watchConfigA](p)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	w, err := b.Watch(ctx)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	r := run(t, w)
	r.next(t)

	p.Delete(ferry.At("host"))

	if err := r.ended(t); !errors.Is(err, ferry.ErrMissing) {
		t.Fatalf("the stream ended with %v, want a missing required field", err)
	}

	// Recovery is a second Watch, and the restored value arrives on its first
	// load rather than on a change nobody has announced yet.
	p.Set(ferry.At("host"), ferry.String("db3"))

	w2, err := b.Watch(ctx)
	if err != nil {
		t.Fatalf("second watch: %v", err)
	}

	if got := run(t, w2).next(t).Host; got != "db3" {
		t.Fatalf("the restarted stream opened with %q, want db3", got)
	}
}

// Scenario 5: the plane loses its watch, and the caller observes it. This is the
// M2 killer: the stream must end with a reason and never go quiet.
func TestVariantAQuietDeathIsObserved(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeA(t)
	r := run(t, watchedA(t, ctx, p))
	r.next(t)

	boom := errors.New("the directory holding this file was removed")
	p.Lose(boom)

	err := r.ended(t)
	if !errors.Is(err, ferry.ErrWatchLost) {
		t.Fatalf("the stream ended with %v, want a lost watch", err)
	}

	if !errors.Is(err, boom) {
		t.Fatalf("the stream ended with %v, which does not carry the plane's own reason", err)
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Fatalf("the stream ended with %v, which is not of the plane class", err)
	}
}

// Scenario 5b: a registration that cannot even be placed is the same ending, and
// it is reported rather than yielding a stream that never fires.
func TestVariantAUnplaceableRegistrationIsObserved(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeA(t)
	p.armFail = errors.New("no watch could be opened")

	r := run(t, watchedA(t, ctx, p))

	if err := r.ended(t); !errors.Is(err, ferry.ErrWatchLost) {
		t.Fatalf("the stream ended with %v, want a lost watch", err)
	}
}

// Scenario 6: one context is the whole wiring, and cancelling it ends the stream
// and leaves no goroutine behind.
func TestVariantAOneLifetimeLeaksNothing(t *testing.T) {
	p := planeA(t)

	before := goroutines()

	ctx, cancel := context.WithCancel(context.Background())
	r := run(t, watchedA(t, ctx, p))
	r.next(t)

	cancel()

	if err := r.ended(t); !errors.Is(err, context.Canceled) {
		t.Fatalf("the stream ended with %v, want the cancellation", err)
	}

	if after := goroutines(); after > before {
		t.Fatalf("goroutines went from %d to %d, so the watch leaked one", before, after)
	}
}

// goroutines settles the scheduler and counts what is left, which is the shape
// driver/env's own watch test uses.
func goroutines() int {
	for range 50 {
		runtime.Gosched()
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}

	return runtime.NumGoroutine()
}

// Scenario 7: a second range is policed rather than sharing the changes out.
func TestVariantASecondRangeIsRefused(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	w := watchedA(t, ctx, planeA(t))

	var seen int

	for range w.Values() {
		seen++

		break
	}

	if seen != 1 {
		t.Fatalf("the first range yielded %d values, want 1", seen)
	}

	if err := w.Err(); err != nil {
		t.Fatalf("breaking out of the range reported %v, want a clean ending", err)
	}

	for range w.Values() {
		t.Fatal("a second range over one Watched yielded a value")
	}

	if err := w.Err(); !errors.Is(err, ferry.ErrWatchInUse) {
		t.Fatalf("the second range reported %v, want a refusal", err)
	}
}

// Scenario 7b: a source that cannot be watched is refused at the Watch call,
// which is the ladder cost this variant pays.
func TestVariantAUnwatchableSourceIsRefusedAtWatch(t *testing.T) {
	t.Parallel()

	b, err := ferry.Bind[watchConfigA](plainPlane{})
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	if _, err := b.Watch(t.Context()); !errors.Is(err, ferry.ErrNotWatchable) {
		t.Fatalf("watching an unwatchable source reported %v, want a refusal", err)
	}
}

// Scenario 8: #361, two watchable planes layered under one binding.
//
// Core sees one source, so the fan-in and the precedence are the caller's, and
// what core supplies is the debounce that makes both planes announcing one
// deployment a single reload.
func TestVariantATwoWatchableSourcesUnderOneBinding(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	over, under := newArmedPlane(), newArmedPlane()
	under.Set(ferry.At("host"), ferry.String("from-under"))

	l := &layeredPlane{over: over, under: under}
	r := run(t, watchedA(t, ctx, l, ferry.Debounce(100*time.Millisecond)))

	if got := r.next(t).Host; got != "from-under" {
		t.Fatalf("first value is %q, want the lower layer's", got)
	}

	// Both layers announce the same deployment, which is the torn read #361
	// names. The debounce is what turns it into one reload of a settled plane.
	under.Set(ferry.At("host"), ferry.String("stale"))
	over.Set(ferry.At("host"), ferry.String("from-over"))

	if got := r.next(t).Host; got != "from-over" {
		t.Fatalf("the reloaded value is %q, want the upper layer's", got)
	}

	if got := over.Opens() + under.Opens(); got != 4 {
		t.Fatalf("the two planes were opened %d times, want 4: two loads over two layers", got)
	}
}

// Scenario 7c, the salvaged shape: with WithWatch the refusal lands on Bind,
// which is where a plane refusal belongs, and Watch can no longer fail on
// capability grounds.
func TestVariantAWithWatchRefusesAtBind(t *testing.T) {
	t.Parallel()

	if _, err := ferry.Bind[watchConfigA](plainPlane{}, ferry.WithWatch()); !errors.Is(err, ferry.ErrNotWatchable) {
		t.Fatalf("binding an unwatchable source with WithWatch reported %v, want a refusal at Bind", err)
	}
}

// Scenario 7d: watchability is option-dependent, so the type assertion alone is
// not the answer. A source that is a Notifier by type and watches nothing by
// configuration is refused at Bind too, through the driver's own BindWatch.
func TestVariantAWithWatchRefusesAnOptionDependentWatchAtBind(t *testing.T) {
	t.Parallel()

	p := planeA(t)
	p.bindFail = errors.New("this source watches no files")

	_, err := ferry.Bind[watchConfigA](p, ferry.WithWatch())
	if !errors.Is(err, ferry.ErrNotWatchable) {
		t.Fatalf("binding a source configured to watch nothing reported %v, want a refusal at Bind", err)
	}

	if !errors.Is(err, p.bindFail) {
		t.Fatalf("the refusal is %v, which does not carry the driver's own reason", err)
	}
}

// Scenario 7e: WithWatch does not change what a watchable source does, and a
// binding built with it streams exactly as one built without it.
func TestVariantAWithWatchStreamsUnchanged(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeA(t)

	b, err := ferry.Bind[watchConfigA](p, ferry.WithWatch())
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	w, err := b.Watch(ctx)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	if got := run(t, w).next(t).Host; got != "db1" {
		t.Fatalf("first value is %q, want db1", got)
	}
}

// A second WithWatch is a refusal, which is the rule every core Option follows.
func TestVariantAWithWatchTwiceIsRefused(t *testing.T) {
	t.Parallel()

	_, err := ferry.Bind[watchConfigA](planeA(t), ferry.WithWatch(), ferry.WithWatch())
	if !errors.Is(err, ferry.ErrSchema) {
		t.Fatalf("two WithWatch options reported %v, want a refusal", err)
	}
}
