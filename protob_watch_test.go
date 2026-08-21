//go:build protob

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

// The eight scenarios, run against variant B: the watchable source is a
// distinct type, so the refusal a caller can make is a compile error and the
// refusal a driver can make lands at BindWatched.

type watchConfigB struct {
	Host string `ferry:"host,required"`
}

// runner ranges a stream on a goroutine of its own and hands the values over
// one at a time.
type runner struct {
	values chan watchConfigB
	done   chan struct{}
	errf   func() error
}

func run(t *testing.T, seq iter.Seq[watchConfigB], errf func() error) *runner {
	t.Helper()

	r := &runner{values: make(chan watchConfigB), done: make(chan struct{}), errf: errf}

	go func() {
		defer close(r.done)

		for v := range seq {
			r.values <- v
		}
	}()

	return r
}

func (r *runner) next(t *testing.T) watchConfigB {
	t.Helper()

	select {
	case v := <-r.values:
		return v
	case <-r.done:
		t.Fatalf("the stream ended before a value arrived: %v", r.errf())
	case <-time.After(2 * time.Second):
		t.Fatal("no value arrived and the stream did not end")
	}

	return watchConfigB{}
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

// watchRun opens a stream and ranges it, which is two calls Go will not let a
// caller write as one because Watch has two results.
func watchRun(ctx context.Context, t *testing.T, wb *ferry.WatchedBinding[watchConfigB],
	opts ...ferry.WatchOption,
) *runner {
	t.Helper()

	seq, errf := wb.Watch(ctx, opts...)

	return run(t, seq, errf)
}

func planeB(t *testing.T) *armedPlane {
	t.Helper()

	p := newArmedPlane()
	p.Set(ferry.At("host"), ferry.String("db1"))

	return p
}

func boundB(t *testing.T, src ferry.WatchableSource) *ferry.WatchedBinding[watchConfigB] {
	t.Helper()

	wb, err := ferry.BindWatched[watchConfigB](src)
	if err != nil {
		t.Fatalf("bind watched: %v", err)
	}

	return wb
}

// Scenario 1: a change that lands before BindWatched returns is not lost,
// because nothing is watching yet and the stream opens with a load.
func TestVariantBChangeBeforeBindIsNotLost(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeB(t)
	src := ferry.Watchable(p, p)

	wb := boundB(t, src)

	p.Set(ferry.At("host"), ferry.String("db2"))

	if got := watchRun(ctx, t, wb).next(t).Host; got != "db2" {
		t.Fatalf("first value is %q, want the change that landed before the stream opened", got)
	}
}

// Scenario 2: a burst is one reload, the reloaded value is fresh, and a value
// handed out earlier never changes underneath its holder.
func TestVariantBBurstCoalescesAndHeldValueIsImmutable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeB(t)
	wb := boundB(t, ferry.Watchable(p, p))
	r := watchRun(ctx, t, wb, ferry.Debounce(100*time.Millisecond))

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

// Scenario 3: the stream opens with a load, and the same binding also loads on
// demand, so there is one object and not three.
func TestVariantBStreamOpensWithALoadAndTheBindingStillLoads(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeB(t)
	wb := boundB(t, ferry.Watchable(p, p))

	v, err := wb.Load(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if v.Host != "db1" {
		t.Fatalf("the load produced %q, want db1", v.Host)
	}

	if got := watchRun(ctx, t, wb).next(t).Host; got != "db1" {
		t.Fatalf("the stream opened with %q, want the plane's current contents", got)
	}
}

// Scenario 4: a failed reload ends the stream, errf says so, and the recovery
// is a second Watch over the same binding.
func TestVariantBFailedReloadIsObservedAndRestartable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeB(t)
	wb := boundB(t, ferry.Watchable(p, p))
	r := watchRun(ctx, t, wb)
	r.next(t)

	p.Delete(ferry.At("host"))

	if err := r.ended(t); !errors.Is(err, ferry.ErrMissing) {
		t.Fatalf("the stream ended with %v, want a missing required field", err)
	}

	p.Set(ferry.At("host"), ferry.String("db3"))

	if got := watchRun(ctx, t, wb).next(t).Host; got != "db3" {
		t.Fatalf("the restarted stream opened with %q, want db3", got)
	}
}

// Scenario 5: the plane loses its watch and the caller observes it.
func TestVariantBQuietDeathIsObserved(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeB(t)
	r := watchRun(ctx, t, boundB(t, ferry.Watchable(p, p)))
	r.next(t)

	boom := errors.New("the directory holding this file was removed")
	p.Lose(boom)

	err := r.ended(t)
	if !errors.Is(err, ferry.ErrWatchLost) || !errors.Is(err, boom) || !errors.Is(err, ferry.ErrPlane) {
		t.Fatalf("the stream ended with %v, want a lost watch carrying the plane's own reason", err)
	}
}

// Scenario 6: one context is the whole wiring, and cancelling it ends the
// stream and leaves no goroutine behind.
func TestVariantBOneLifetimeLeaksNothing(t *testing.T) {
	p := planeB(t)

	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	r := watchRun(ctx, t, boundB(t, ferry.Watchable(p, p)))
	r.next(t)

	cancel()

	if err := r.ended(t); !errors.Is(err, context.Canceled) {
		t.Fatalf("the stream ended with %v, want the cancellation", err)
	}

	if !settled(before) {
		t.Fatalf("goroutines went from %d to %d and stayed there, so the watch leaked one",
			before, runtime.NumGoroutine())
	}
}

// settled waits for the goroutine count to come back to where it was, because a
// goroutine returning is not instantaneous and a fixed sleep is either flaky or
// slow. It is driver/env's own watch test's helper, in its own words.
func settled(before int) bool {
	for range 100 {
		if runtime.NumGoroutine() <= before {
			return true
		}

		time.Sleep(20 * time.Millisecond)
	}

	return false
}

// Scenario 7: two consumers are well defined rather than policed. Each Watch
// arms a registration of its own, so both see every change.
func TestVariantBTwoConsumersEachSeeEveryChange(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeB(t)
	wb := boundB(t, ferry.Watchable(p, p))

	one := watchRun(ctx, t, wb)
	two := watchRun(ctx, t, wb)

	one.next(t)
	two.next(t)

	p.Set(ferry.At("host"), ferry.String("db2"))

	if got := one.next(t).Host; got != "db2" {
		t.Fatalf("the first consumer saw %q, want db2", got)
	}

	if got := two.next(t).Host; got != "db2" {
		t.Fatalf("the second consumer saw %q, want db2: two streams do not share changes out", got)
	}
}

// Scenario 7b: a driver that cannot honour the watch it was asked for is
// refused at BindWatched, before any load, carrying its own reason.
func TestVariantBUnwatchableSourceIsRefusedAtBind(t *testing.T) {
	t.Parallel()

	boom := errors.New("this source watches no files")

	_, err := ferry.BindWatched[watchConfigB](ferry.Unwatchable(boom))
	if !errors.Is(err, ferry.ErrNotWatchable) || !errors.Is(err, boom) {
		t.Fatalf("binding an unwatchable source reported %v, want a refusal carrying the driver's reason", err)
	}
}

// Scenario 7c: the zero WatchableSource is the one forgeable value, and it is
// refused at BindWatched rather than yielding a stream that never fires.
func TestVariantBZeroWatchableSourceIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := ferry.BindWatched[watchConfigB](ferry.WatchableSource{}); !errors.Is(err, ferry.ErrNotWatchable) {
		t.Fatalf("binding the zero WatchableSource reported %v, want a refusal", err)
	}
}

// A source that cannot be watched has no WatchableSource to be passed as, so
// the mistake is a compile error. This test is the assertion that the type is
// the gate: a WatchableSource is still an ordinary Source, so a caller who
// wants only to load through one writes no conversion.
func TestVariantBAWatchableSourceIsAlsoASource(t *testing.T) {
	t.Parallel()

	p := planeB(t)

	var src ferry.Source = ferry.Watchable(p, p)

	b, err := ferry.Bind[watchConfigB](src)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	v, err := b.Load(t.Context())
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if v.Host != "db1" {
		t.Fatalf("the load produced %q, want db1", v.Host)
	}
}

// Scenario 8: #361, two watchable planes layered under one binding.
func TestVariantBTwoWatchableSourcesUnderOneBinding(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	over, under := newArmedPlane(), newArmedPlane()
	under.Set(ferry.At("host"), ferry.String("from-under"))

	l := &layeredPlane{over: over, under: under}
	wb := boundB(t, ferry.Watchable(l, l))
	r := watchRun(ctx, t, wb, ferry.Debounce(100*time.Millisecond))

	if got := r.next(t).Host; got != "from-under" {
		t.Fatalf("first value is %q, want the lower layer's", got)
	}

	under.Set(ferry.At("host"), ferry.String("stale"))
	over.Set(ferry.At("host"), ferry.String("from-over"))

	if got := r.next(t).Host; got != "from-over" {
		t.Fatalf("the reloaded value is %q, want the upper layer's", got)
	}

	if got := over.Opens() + under.Opens(); got != 4 {
		t.Fatalf("the two planes were opened %d times, want 4: two loads over two layers", got)
	}
}

// A WatchOption list that does not resolve ends the stream rather than
// returning an error nobody asked for, which is the cost of Watch having no
// error result.
func TestVariantBBadWatchOptionEndsTheStream(t *testing.T) {
	t.Parallel()

	p := planeB(t)
	seq, errf := boundB(t, ferry.Watchable(p, p)).Watch(t.Context(), ferry.Debounce(-1))

	for range seq {
		t.Fatal("a stream built from a refused option yielded a value")
	}

	if err := errf(); !errors.Is(err, ferry.ErrSchema) {
		t.Fatalf("the stream reported %v, want the refused option", err)
	}
}
