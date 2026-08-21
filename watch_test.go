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

// Everything here runs through BindWatched and the watched binding's Load and
// Watch, over the in-memory watchable plane beside it, and through nothing else
// (ADR-0020).

type watchConfig struct {
	Host string `ferry:"host,required"`
}

// runner ranges a stream on a goroutine of its own and hands the values over
// one at a time.
type runner[T any] struct {
	values chan T
	done   chan struct{}
	errf   func() error
}

func run[T any](t *testing.T, seq iter.Seq[T], errf func() error) *runner[T] {
	t.Helper()

	r := &runner[T]{values: make(chan T), done: make(chan struct{}), errf: errf}

	go func() {
		defer close(r.done)

		for v := range seq {
			r.values <- v
		}
	}()

	return r
}

func (r *runner[T]) next(t *testing.T) T {
	t.Helper()

	select {
	case v := <-r.values:
		return v
	case <-r.done:
		t.Fatalf("the stream ended before a value arrived: %v", r.errf())
	case <-time.After(2 * time.Second):
		t.Fatal("no value arrived and the stream did not end")
	}

	var zero T

	return zero
}

// ended waits for the range to exit and reports what ended it. A stream that
// hangs fails here.
func (r *runner[T]) ended(t *testing.T) error {
	t.Helper()

	select {
	case <-r.done:
		return r.errf()
	case <-time.After(2 * time.Second):
		t.Fatal("the stream did not end")
	}

	return nil
}

// saysNothing asserts the stream ends without handing over another value, which
// is what a mechanism that never fires does once its context runs out.
func (r *runner[T]) saysNothing(t *testing.T) {
	t.Helper()

	select {
	case <-r.done:
	case <-r.values:
		t.Fatal("a mechanism that never fires produced a second value")
	case <-time.After(2 * time.Second):
		t.Fatal("the stream did not end")
	}
}

// watchRun opens a stream and ranges it, which is two calls Go will not let a
// caller write as one because Watch has two results.
func watchRun(ctx context.Context, t *testing.T, wb *ferry.WatchedBinding[watchConfig]) *runner[watchConfig] {
	t.Helper()

	seq, errf := wb.Watch(ctx)

	return run(t, seq, errf)
}

func watchPlane(t *testing.T) *armedPlane {
	t.Helper()

	p := newArmedPlane()
	p.Set(ferry.At("host"), ferry.String("db1"))

	return p
}

func boundWatched(t *testing.T, src ferry.WatchableSource) *ferry.WatchedBinding[watchConfig] {
	t.Helper()

	wb, err := ferry.BindWatched[watchConfig](src)
	if err != nil {
		t.Fatalf("bind watched: %v", err)
	}

	return wb
}

// A change that lands before BindWatched returns is not lost, because nothing
// is watching yet and the stream opens with a load.
func TestWatchChangeBeforeBindIsNotLost(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := watchPlane(t)
	wb := boundWatched(t, p)

	p.Set(ferry.At("host"), ferry.String("db2"))

	if got := watchRun(ctx, t, wb).next(t).Host; got != "db2" {
		t.Fatalf("first value is %q, want the change that landed before the stream opened", got)
	}
}

// A burst is one reload, the reloaded value is fresh, and a value handed out
// earlier never changes underneath its holder.
func TestWatchBurstCoalescesAndHeldValueIsImmutable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := watchPlane(t)
	r := watchRun(ctx, t, boundWatched(t, p))

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

// The stream opens with a load, and the same binding also loads on demand, so
// there is one object and not three.
func TestWatchStreamOpensWithALoadAndTheBindingStillLoads(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := watchPlane(t)
	wb := boundWatched(t, p)

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

// A failed reload ends the stream, errf says so, and the recovery is a second
// Watch over the same binding.
func TestWatchFailedReloadIsObservedAndRestartable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := watchPlane(t)
	wb := boundWatched(t, p)
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

// The plane loses its watch and the caller observes it.
func TestWatchQuietDeathIsObserved(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := watchPlane(t)
	r := watchRun(ctx, t, boundWatched(t, p))
	r.next(t)

	boom := errors.New("the directory holding this file was removed")
	p.Lose(boom)

	err := r.ended(t)
	if !errors.Is(err, ferry.ErrWatchLost) || !errors.Is(err, boom) || !errors.Is(err, ferry.ErrPlane) {
		t.Fatalf("the stream ended with %v, want a lost watch carrying the plane's own reason", err)
	}
}

// A registration that cannot be placed at all is the same ending.
func TestWatchUnplaceableRegistrationIsObserved(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := watchPlane(t)
	p.failArming(errors.New("no watch could be opened"))

	if err := watchRun(ctx, t, boundWatched(t, p)).ended(t); !errors.Is(err, ferry.ErrWatchLost) {
		t.Fatalf("the stream ended with %v, want a lost watch", err)
	}
}

// A mechanism that dies between one wait and the next ends the stream too: the
// re-arm is where a stream discovers it, and the ending is the same one.
func TestWatchMechanismLostAtTheRearmIsObserved(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := watchPlane(t)
	r := watchRun(ctx, t, boundWatched(t, p))
	r.next(t)

	boom := errors.New("the notification handle was closed")
	p.failArming(boom)
	p.Set(ferry.At("host"), ferry.String("db2"))

	if err := r.ended(t); !errors.Is(err, ferry.ErrWatchLost) || !errors.Is(err, boom) {
		t.Fatalf("the stream ended with %v, want a lost watch carrying the plane's own reason", err)
	}
}

// A cancellation racing the re-arm is the cancellation and not a lost watch.
// The mechanism refuses the registration because the context is dead, and a
// caller matching the three endings apart must not be told the watch failed
// when what happened is that they ended it.
func TestWatchCancellationRacingTheRearmIsTheCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	wb, err := ferry.BindWatched[optionalHost](racingPlane{})
	if err != nil {
		t.Fatalf("bind watched: %v", err)
	}

	seq, errf := wb.Watch(ctx)
	r := run(t, seq, errf)
	r.next(t)

	cancel()

	end := r.ended(t)
	if !errors.Is(end, context.Canceled) {
		t.Fatalf("the stream ended with %v, want the cancellation", end)
	}

	if errors.Is(end, ferry.ErrWatchLost) {
		t.Fatalf("the stream ended with %v, which reports a lost watch for a stream the caller ended", end)
	}
}

// A plane that ends its own watch quietly, with no reason and no cancellation,
// is still an ending the caller can see.
func TestWatchAQuietEndingIsStillALostWatch(t *testing.T) {
	t.Parallel()

	wb, err := ferry.BindWatched[optionalHost](quietPlane{})
	if err != nil {
		t.Fatalf("bind watched: %v", err)
	}

	seq, errf := wb.Watch(t.Context())

	var got int
	for range seq {
		got++
	}

	if got != 1 {
		t.Fatalf("the stream yielded %d values, want the one load it opens with", got)
	}

	if err := errf(); !errors.Is(err, ferry.ErrWatchLost) || !errors.Is(err, ferry.ErrPlane) {
		t.Fatalf("the stream ended with %v, want a lost watch", err)
	}
}

// Breaking out of the range is the caller ending the stream, so errf reports
// nothing and the registration is released all the same.
func TestWatchBreakingOutOfTheRangeIsClean(t *testing.T) {
	t.Parallel()

	p := watchPlane(t)
	wb := boundWatched(t, p)

	seq, errf := wb.Watch(t.Context())
	for range seq {
		break
	}

	if err := errf(); err != nil {
		t.Fatalf("breaking out of the range reported %v, want nothing", err)
	}

	if got := p.outstanding(); got != 0 {
		t.Fatalf("%d registrations were left armed, want none: the release is core's obligation", got)
	}
}

// One context is the whole wiring: cancelling it ends a stream waiting on a
// pending change, leaves no goroutine behind, and releases the registration.
func TestWatchOneLifetimeLeaksNothing(t *testing.T) {
	p := watchPlane(t)

	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	r := watchRun(ctx, t, boundWatched(t, p))
	r.next(t)

	cancel()

	if err := r.ended(t); !errors.Is(err, context.Canceled) {
		t.Fatalf("the stream ended with %v, want the cancellation", err)
	}

	if got := p.outstanding(); got != 0 {
		t.Fatalf("%d registrations were left armed, want none", got)
	}

	if !settled(before) {
		t.Fatalf("goroutines went from %d to %d and stayed there, so the watch leaked one",
			before, runtime.NumGoroutine())
	}
}

// settled waits for the goroutine count to come back to where it was, because a
// goroutine returning is not instantaneous and a fixed sleep is either flaky or
// slow.
func settled(before int) bool {
	for range 100 {
		if runtime.NumGoroutine() <= before {
			return true
		}

		time.Sleep(20 * time.Millisecond)
	}

	return false
}

// Two consumers are well defined rather than policed. Each Watch arms a
// registration of its own, so both see every change.
func TestWatchTwoConsumersEachSeeEveryChange(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p := watchPlane(t)
	wb := boundWatched(t, p)

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

// The misuse audit, in tests.

// A source configured to watch nothing is refused at the bind, before any load,
// carrying its own reason. This is the instance-level half no type can carry.
func TestWatchUnwatchableInstanceIsRefusedAtBind(t *testing.T) {
	t.Parallel()

	p := watchPlane(t)

	boom := errors.New("this source watches no files")
	p.refuseWatching(boom)

	_, err := ferry.BindWatched[watchConfig](p)
	if !errors.Is(err, ferry.ErrPlane) || !errors.Is(err, boom) {
		t.Fatalf("binding a source configured to watch nothing reported %v, want its own reason", err)
	}
}

// A source that claims watchability and hands over nothing, with no reason, has
// broken the contract rather than refused, so it is a driver defect and is
// reported as one. This is the misuse an open interface admits.
func TestWatchNilMechanismWithNoReasonIsADriverDefect(t *testing.T) {
	t.Parallel()

	p := watchPlane(t)
	p.handOverNoMechanism()

	if _, err := ferry.BindWatched[watchConfig](p); !errors.Is(err, ferry.ErrDriver) {
		t.Fatalf("binding a source that handed over no mechanism reported %v, want a driver defect", err)
	}
}

// A nil WatchableSource is refused rather than dereferenced.
func TestWatchNilSourceIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := ferry.BindWatched[watchConfig](nil); !errors.Is(err, ferry.ErrPlane) {
		t.Fatalf("binding a nil watchable source reported %v, want a refusal", err)
	}
}

// Every refusal Bind makes, BindWatched makes too.
func TestWatchBindRefusalsStillApply(t *testing.T) {
	t.Parallel()

	type bad struct {
		Host string `ferry:"host,nonsense"`
	}

	if _, err := ferry.BindWatched[bad](watchPlane(t)); !errors.Is(err, ferry.ErrSchema) {
		t.Fatalf("binding a type with an illegal tag reported %v, want a schema refusal", err)
	}
}

// A source that claims watchability and hands over a mechanism that never fires
// is the one misuse no shape here detects: it binds, it streams its first
// value, and it then waits for ever. It is recorded as a test so that the hole
// is written down rather than argued about.
func TestWatchASilentMechanismBindsAndOpensAndThenWaits(t *testing.T) {
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
	r := run(t, seq, errf)

	// It opens with a load, so the caller is not left with nothing at all.
	if got := r.next(t).Host; got != "" {
		t.Fatalf("the stream opened with %q, want the empty plane's value", got)
	}

	// And then it says nothing until the context ends it, which is the cost.
	r.saysNothing(t)

	if err := errf(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the stream ended with %v, want only the deadline", err)
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
func TestWatchAWatchableSourceIsAlsoASource(t *testing.T) {
	t.Parallel()

	var src ferry.Source = watchPlane(t)

	b, err := ferry.Bind[watchConfig](src)
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

	// ferry.BindWatched[watchConfig](plainPlane{}) does not compile: it has no
	// Watching method, and there is no conversion.
	var _ ferry.Source = plainPlane{}
}
