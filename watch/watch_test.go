package watch_test

import (
	"context"
	"errors"
	"iter"
	"sync"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/watch"
)

// Endpoint is a second shape, so that one signal can be shown driving two
// bindings of different types.
type Endpoint struct {
	Host string `ferry:"host,required"`
}

// bind builds a plane holding one host, wires a fresh signal to it, and binds
// Config against it.
func bind(t *testing.T) (*memPlane, *watch.Signal, *ferry.Binding[Config]) {
	t.Helper()

	plane := newMemPlane()
	plane.Set(ferry.At("host"), ferry.String("db1"))

	s := watch.New()
	plane.OnChange(s.Changed)

	b, err := ferry.Bind[Config](plane)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	return plane, s, b
}

// first takes the first value a stream yields and stops ranging there, which is
// the shape most of these tests want.
func first[T any](seq iter.Seq[T]) (T, bool) {
	for v := range seq {
		return v, true
	}

	var zero T

	return zero, false
}

// countUntilCancelled ranges the whole stream, cancelling once the first value
// arrives, so anything the signal still holds is yielded and counted before the
// stream ends on the cancellation.
func countUntilCancelled[T any](seq iter.Seq[T], cancel context.CancelFunc) int {
	var n int

	for range seq {
		n++

		cancel()
	}

	return n
}

// expectNoValues fails if the stream yields anything at all.
func expectNoValues[T any](t *testing.T, seq iter.Seq[T]) {
	t.Helper()

	if _, ok := first(seq); ok {
		t.Fatal("the stream yielded a value it should not have")
	}
}

// expectCancelled fails unless the stream ended on the cancellation of its
// context.
func expectCancelled(t *testing.T, errf func() error) {
	t.Helper()

	if !errors.Is(errf(), context.Canceled) {
		t.Fatalf("errf lost the cancellation: %v", errf())
	}
}

// expectClean fails unless the stream ended the way a break ends it.
func expectClean(t *testing.T, errf func() error) {
	t.Helper()

	if err := errf(); err != nil {
		t.Fatalf("break is a clean ending: %v", err)
	}
}

// A change recorded before Values is called is not lost, which is the whole
// reason the signal holds a slot rather than the range holding it.
func TestChangeBeforeTheRangeIsNotLost(t *testing.T) {
	plane, s, b := bind(t)

	plane.Set(ferry.At("host"), ferry.String("db2")) // before Values exists

	seq, errf := watch.Values(t.Context(), s, b)

	cfg, ok := first(seq)
	if !ok || cfg.Host != "db2" {
		t.Fatalf("the stream did not open with the pending change: %+v", cfg)
	}

	expectClean(t, errf)
}

// A burst of changes is one reload, which the plane's open counter proves.
func TestABurstIsOneReload(t *testing.T) {
	plane, s, b := bind(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	seq, errf := watch.Values(ctx, s, b)

	for range 5 {
		plane.Set(ferry.At("host"), ferry.String("db2"))
	}

	before := plane.Opens()

	// The range keeps going after the cancellation, so a second value would be
	// counted here rather than missed.
	if values := countUntilCancelled(seq, cancel); values != 1 {
		t.Fatalf("five changes yielded %d values, want 1", values)
	}

	if opens := plane.Opens() - before; opens != 1 {
		t.Fatalf("five changes opened the plane %d times, want 1", opens)
	}

	expectCancelled(t, errf)
}

// Every value is loaded when it is yielded, so a change that lands between two
// turns of the loop is in the next value.
func TestEachValueIsFreshlyLoaded(t *testing.T) {
	plane, s, b := bind(t)

	seq, errf := watch.Values(t.Context(), s, b)

	plane.Set(ferry.At("host"), ferry.String("db2"))

	var got []string

	for cfg := range seq {
		got = append(got, cfg.Host)

		if len(got) == 2 {
			break
		}

		plane.Set(ferry.At("host"), ferry.String("db3"))
	}

	if len(got) != 2 || got[0] != "db2" || got[1] != "db3" {
		t.Fatalf("the stream did not reload per change: %v", got)
	}

	expectClean(t, errf)
}

// A value already handed out is never touched by a later reload, which is what
// makes publication a replacement rather than a mutation.
func TestAHeldValueIsNotMutatedByAReload(t *testing.T) {
	plane, s, b := bind(t)

	held, err := b.Load(t.Context())
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	seq, _ := watch.Values(t.Context(), s, b)

	plane.Set(ferry.At("host"), ferry.String("db2"))

	if cfg, ok := first(seq); !ok || cfg.Host != "db2" {
		t.Fatalf("reloaded value is stale: %+v", cfg)
	}

	if held.Host != "db1" || held.Port != 8080 {
		t.Fatalf("the held value moved underneath the caller: %+v", held)
	}
}

// A failed reload ends the stream with no value yielded, and the load's error
// reaches the caller intact.
func TestAFailedReloadEndsTheStream(t *testing.T) {
	plane, s, b := bind(t)

	seq, errf := watch.Values(t.Context(), s, b)

	plane.Delete(ferry.At("host")) // the plane loses a required address

	expectNoValues(t, seq)

	if !errors.Is(errf(), ferry.ErrMissing) {
		t.Fatalf("errf did not carry the load failure: %v", errf())
	}
}

// Recovery is ranging again over the same signal, and the change that fixed the
// plane is waiting when the second stream opens.
func TestValuesAgainResumesAfterAFailure(t *testing.T) {
	plane, s, b := bind(t)

	seq, errf := watch.Values(t.Context(), s, b)

	plane.Delete(ferry.At("host"))

	expectNoValues(t, seq)

	if !errors.Is(errf(), ferry.ErrMissing) {
		t.Fatalf("errf did not carry the load failure: %v", errf())
	}

	plane.Set(ferry.At("host"), ferry.String("db2")) // no stream is ranging

	seq, errf = watch.Values(t.Context(), s, b)

	cfg, ok := first(seq)
	if !ok || cfg.Host != "db2" {
		t.Fatalf("the second stream lost the change: %+v", cfg)
	}

	expectClean(t, errf)
}

// Cancellation ends the stream, and it ends it even when a change is already
// pending: a select with both cases ready must not deliver one more value.
func TestCancellationEndsTheStream(t *testing.T) {
	t.Run("nothing pending", func(t *testing.T) {
		_, s, b := bind(t)

		ctx, cancel := context.WithCancel(t.Context())
		seq, errf := watch.Values(ctx, s, b)
		cancel()

		expectNoValues(t, seq)
		expectCancelled(t, errf)
	})

	t.Run("change pending", func(t *testing.T) {
		plane, s, b := bind(t)

		ctx, cancel := context.WithCancel(t.Context())
		seq, errf := watch.Values(ctx, s, b)

		plane.Set(ferry.At("host"), ferry.String("db2"))
		cancel()

		expectNoValues(t, seq)
		expectCancelled(t, errf)
	})
}

// Breaking out of the range is a clean ending and errf reports nil.
func TestBreakIsACleanEnding(t *testing.T) {
	plane, s, b := bind(t)

	seq, errf := watch.Values(t.Context(), s, b)

	plane.Set(ferry.At("host"), ferry.String("db2"))

	for range seq {
		break
	}

	if err := errf(); err != nil {
		t.Fatalf("break is a clean ending: %v", err)
	}
}

// Changed never blocks and is safe from many goroutines, which is what keeps a
// driver's watching goroutine free of the consumer's pace.
func TestChangedNeverBlocksAndIsConcurrencySafe(t *testing.T) {
	plane, s, b := bind(t)

	var wg sync.WaitGroup

	for range 8 {
		wg.Go(func() {
			for range 100 {
				s.Changed(context.Background()) // nobody is ranging
			}
		})
	}

	wg.Wait()

	plane.Set(ferry.At("host"), ferry.String("db2"))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	seq, errf := watch.Values(ctx, s, b)

	if values := countUntilCancelled(seq, cancel); values != 1 {
		t.Fatalf("801 calls left %d values pending, want 1", values)
	}

	expectCancelled(t, errf)
}

// One signal drives two bindings, one after the other, including two different
// types over the same plane.
func TestOneSignalDrivesTwoBindingsInTurn(t *testing.T) {
	plane, s, b := bind(t)

	other, err := ferry.Bind[Endpoint](plane)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	plane.Set(ferry.At("host"), ferry.String("db2"))

	seq, errf := watch.Values(t.Context(), s, b)

	cfg, ok := first(seq)
	if !ok || cfg.Host != "db2" || cfg.Port != 8080 {
		t.Fatalf("first stream loaded %+v", cfg)
	}

	expectClean(t, errf)

	plane.Set(ferry.At("host"), ferry.String("db3"))

	seq2, errf2 := watch.Values(t.Context(), s, other)

	e, ok := first(seq2)
	if !ok || e.Host != "db3" {
		t.Fatalf("second stream loaded %+v", e)
	}

	expectClean(t, errf2)
}
