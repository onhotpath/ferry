//go:build protoe

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

// The eight scenarios, run against variant E: a watchable source is an
// interface, implementing it is the claim, and the one call that reads it is
// the bind.

type watchConfigE struct {
	Host string `ferry:"host,required"`
}

// runner ranges a stream on a goroutine of its own and hands the values over
// one at a time.
type runner struct {
	values chan watchConfigE
	done   chan struct{}
	errf   func() error
}

func run(t *testing.T, seq iter.Seq[watchConfigE], errf func() error) *runner {
	t.Helper()

	r := &runner{values: make(chan watchConfigE), done: make(chan struct{}), errf: errf}

	go func() {
		defer close(r.done)

		for v := range seq {
			r.values <- v
		}
	}()

	return r
}

func (r *runner) next(t *testing.T) watchConfigE {
	t.Helper()

	select {
	case v := <-r.values:
		return v
	case <-r.done:
		t.Fatalf("the stream ended before a value arrived: %v", r.errf())
	case <-time.After(2 * time.Second):
		t.Fatal("no value arrived and the stream did not end")
	}

	return watchConfigE{}
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
func watchRun(ctx context.Context, t *testing.T, wb *ferry.WatchedBinding[watchConfigE],
	opts ...ferry.WatchOption,
) *runner {
	t.Helper()

	seq, errf := wb.Watch(ctx, opts...)

	return run(t, seq, errf)
}

func planeE(t *testing.T) *armedPlane {
	t.Helper()

	p := newArmedPlane()
	p.Set(ferry.At("host"), ferry.String("db1"))

	return p
}

func boundE(t *testing.T, src ferry.WatchableSource) *ferry.WatchedBinding[watchConfigE] {
	t.Helper()

	wb, err := ferry.BindWatched[watchConfigE](src)
	if err != nil {
		t.Fatalf("bind watched: %v", err)
	}

	return wb
}

// Scenario 1: a change that lands before BindWatched returns is not lost,
// because nothing is watching yet and the stream opens with a load.
func TestVariantEChangeBeforeBindIsNotLost(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeE(t)
	wb := boundE(t, p)

	p.Set(ferry.At("host"), ferry.String("db2"))

	if got := watchRun(ctx, t, wb).next(t).Host; got != "db2" {
		t.Fatalf("first value is %q, want the change that landed before the stream opened", got)
	}
}

// Scenario 2: a burst is one reload, the reloaded value is fresh, and a value
// handed out earlier never changes underneath its holder.
func TestVariantEBurstCoalescesAndHeldValueIsImmutable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeE(t)
	r := watchRun(ctx, t, boundE(t, p), ferry.Debounce(100*time.Millisecond))

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
func TestVariantEStreamOpensWithALoadAndTheBindingStillLoads(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeE(t)
	wb := boundE(t, p)

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
func TestVariantEFailedReloadIsObservedAndRestartable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeE(t)
	wb := boundE(t, p)
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
func TestVariantEQuietDeathIsObserved(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeE(t)
	r := watchRun(ctx, t, boundE(t, p))
	r.next(t)

	boom := errors.New("the directory holding this file was removed")
	p.Lose(boom)

	err := r.ended(t)
	if !errors.Is(err, ferry.ErrWatchLost) || !errors.Is(err, boom) || !errors.Is(err, ferry.ErrPlane) {
		t.Fatalf("the stream ended with %v, want a lost watch carrying the plane's own reason", err)
	}
}

// Scenario 5b: a registration that cannot be placed at all is the same ending.
func TestVariantEUnplaceableRegistrationIsObserved(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeE(t)
	p.armFail = errors.New("no watch could be opened")

	if err := watchRun(ctx, t, boundE(t, p)).ended(t); !errors.Is(err, ferry.ErrWatchLost) {
		t.Fatalf("the stream ended with %v, want a lost watch", err)
	}
}

// Scenario 6: one context is the whole wiring, and cancelling it ends the
// stream and leaves no goroutine behind.
func TestVariantEOneLifetimeLeaksNothing(t *testing.T) {
	p := planeE(t)

	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	r := watchRun(ctx, t, boundE(t, p))
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
func TestVariantETwoConsumersEachSeeEveryChange(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := planeE(t)
	wb := boundE(t, p)

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

// Scenario 8: #361, two watchable planes under one binding, composed with
// ferry.WatchAll. The caller writes the layering precedence and nothing else.
func TestVariantETwoWatchableSourcesUnderOneBinding(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	over, under := newArmedPlane(), newArmedPlane()
	under.Set(ferry.At("host"), ferry.String("from-under"))

	src := ferry.WatchAll(&layeredPlane{over: over, under: under}, over, under)
	r := watchRun(ctx, t, boundE(t, src), ferry.Debounce(100*time.Millisecond))

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

// Scenario 8b: a change on the quiet layer alone still reloads, which is what
// the fan-in exists for and what a caller writing it by hand gets wrong.
func TestVariantEAChangeOnEitherLayerReloads(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	over, under := newArmedPlane(), newArmedPlane()
	under.Set(ferry.At("host"), ferry.String("from-under"))

	src := ferry.WatchAll(&layeredPlane{over: over, under: under}, over, under)
	r := watchRun(ctx, t, boundE(t, src))

	r.next(t)

	// Only the lower layer moves, and the upper one is silent throughout.
	under.Set(ferry.At("host"), ferry.String("moved"))

	if got := r.next(t).Host; got != "moved" {
		t.Fatalf("the reloaded value is %q, want the lower layer's change", got)
	}
}

// Scenario 8c: one layer losing its watch ends the stream, naming that layer's
// own reason.
func TestVariantEOneLayerLosingItsWatchEndsTheStream(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	over, under := newArmedPlane(), newArmedPlane()
	under.Set(ferry.At("host"), ferry.String("from-under"))

	src := ferry.WatchAll(&layeredPlane{over: over, under: under}, over, under)
	r := watchRun(ctx, t, boundE(t, src))
	r.next(t)

	boom := errors.New("the upper layer lost its watch")
	over.Lose(boom)

	if err := r.ended(t); !errors.Is(err, boom) {
		t.Fatalf("the stream ended with %v, want the layer's own reason", err)
	}
}

// Scenario 8d: one layer that cannot be watched refuses the whole composite at
// the bind, carrying that layer's reason.
func TestVariantEOneUnwatchableLayerRefusesTheComposite(t *testing.T) {
	t.Parallel()

	over, under := newArmedPlane(), newArmedPlane()

	boom := errors.New("this layer watches no files")
	over.bindFail = boom

	src := ferry.WatchAll(&layeredPlane{over: over, under: under}, over, under)

	_, err := ferry.BindWatched[watchConfigE](src)
	if !errors.Is(err, ferry.ErrNotWatchable) || !errors.Is(err, boom) {
		t.Fatalf("binding a composite with an unwatchable layer reported %v, want that layer's reason", err)
	}
}

// A composite of nothing is refused rather than yielding a stream that never
// fires.
func TestVariantEWatchAllOfNothingIsRefused(t *testing.T) {
	t.Parallel()

	src := ferry.WatchAll(planeE(t))

	if _, err := ferry.BindWatched[watchConfigE](src); !errors.Is(err, ferry.ErrNotWatchable) {
		t.Fatalf("binding a composite of no layers reported %v, want a refusal", err)
	}
}

// The misuse audit, in tests.

// A source configured to watch nothing is refused at the bind, before any load,
// carrying its own reason. This is the instance-level half no type can carry.
func TestVariantEUnwatchableInstanceIsRefusedAtBind(t *testing.T) {
	t.Parallel()

	p := planeE(t)

	boom := errors.New("this source watches no files")
	p.bindFail = boom

	_, err := ferry.BindWatched[watchConfigE](p)
	if !errors.Is(err, ferry.ErrNotWatchable) || !errors.Is(err, boom) {
		t.Fatalf("binding a source configured to watch nothing reported %v, want its own reason", err)
	}
}

// A source that claims watchability and hands over nothing, with no reason, is
// refused rather than dereferenced. This is the misuse an open interface admits
// and a sealed constructor did not.
func TestVariantENilMechanismWithNoReasonIsRefused(t *testing.T) {
	t.Parallel()

	p := planeE(t)
	p.handOverNothing = true

	if _, err := ferry.BindWatched[watchConfigE](p); !errors.Is(err, ferry.ErrNotWatchable) {
		t.Fatalf("binding a source that handed over no mechanism reported %v, want a refusal", err)
	}
}

// A nil WatchableSource is refused rather than dereferenced.
func TestVariantENilSourceIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := ferry.BindWatched[watchConfigE](nil); !errors.Is(err, ferry.ErrNotWatchable) {
		t.Fatalf("binding a nil watchable source reported %v, want a refusal", err)
	}
}

// A source that claims watchability and hands over a mechanism that never fires
// is the one misuse no shape here detects: it binds, it streams its first
// value, and it then waits for ever. It is recorded as a test so that the hole
// is written down rather than argued about.
func TestVariantEASilentMechanismBindsAndOpensAndThenWaits(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	// It binds cleanly, which is the hole: nothing core can ask distinguishes a
	// mechanism that will fire from one that will not.
	wb, err := ferry.BindWatched[optionalHost](halfWatchable{})
	if err != nil {
		t.Fatalf("bind watched: %v", err)
	}

	seq, errf := wb.Watch(ctx)
	values, done := drainSilent(seq)

	// It opens with a load, so the caller is not left with nothing at all.
	openedWith(t, values, done, errf)

	// And then it says nothing until the context ends it, which is the cost.
	saysNothing(t, values, done)

	if err := errf(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the stream ended with %v, want only the deadline", err)
	}
}

func drainSilent(seq iter.Seq[optionalHost]) (chan optionalHost, chan struct{}) {
	values := make(chan optionalHost)
	done := make(chan struct{})

	go func() {
		defer close(done)

		for v := range seq {
			values <- v
		}
	}()

	return values, done
}

func openedWith(t *testing.T, values chan optionalHost, done chan struct{}, errf func() error) {
	t.Helper()

	select {
	case v := <-values:
		if v.Host != "" {
			t.Fatalf("the stream opened with %q, want the empty plane's value", v.Host)
		}
	case <-done:
		t.Fatalf("the stream ended before a value arrived: %v", errf())
	case <-time.After(2 * time.Second):
		t.Fatal("no value arrived")
	}
}

func saysNothing(t *testing.T, values chan optionalHost, done chan struct{}) {
	t.Helper()

	select {
	case <-done:
	case <-values:
		t.Fatal("a mechanism that never fires produced a second value")
	case <-time.After(2 * time.Second):
		t.Fatal("the stream did not end")
	}
}

// optionalHost is what a silent plane can load: the empty plane is not a
// failure for it, so the test measures the silence and not a required field.
type optionalHost struct {
	Host string `ferry:"host"`
}

// A watchable source is an ordinary Source, so a caller who wants only to load
// through one writes no conversion, and a plain Source is a compile error at
// BindWatched rather than a refusal.
func TestVariantEAWatchableSourceIsAlsoASource(t *testing.T) {
	t.Parallel()

	var src ferry.Source = planeE(t)

	b, err := ferry.Bind[watchConfigE](src)
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

	// ferry.BindWatched[watchConfigE](plainPlane{}) does not compile: it has no
	// Watching method, and there is no conversion.
	var _ ferry.Source = plainPlane{}
}

// A WatchOption list that does not resolve ends the stream rather than
// returning an error nobody asked for.
func TestVariantEBadWatchOptionEndsTheStream(t *testing.T) {
	t.Parallel()

	seq, errf := boundE(t, planeE(t)).Watch(t.Context(), ferry.Debounce(-1))

	for range seq {
		t.Fatal("a stream built from a refused option yielded a value")
	}

	if err := errf(); !errors.Is(err, ferry.ErrSchema) {
		t.Fatalf("the stream reported %v, want the refused option", err)
	}
}
