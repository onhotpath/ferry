//go:build protoe

package winreg_test

import (
	"context"
	"errors"
	"iter"
	"testing"
	"time"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/windows/winreg"
)

// The watching tests, ported from the callback Option to the typed seam.
//
// They run against the fake store, which models the register-once-wait-once
// mechanism the machine's own registry has. The machine tests stay
// Windows-only and are untouched.

type watchedText struct {
	Text string `ferry:"Text"`
}

// watchedSource converts a source over the given store, which is the whole of
// the wiring under this variant.
func watchedSource(store winreg.Registry) *winreg.WatchedSource {
	return source(store).Watched()
}

// streamOf ranges a watch on a goroutine of its own.
func streamOf(seq iter.Seq[watchedText]) (chan watchedText, chan struct{}) {
	values := make(chan watchedText)
	done := make(chan struct{})

	go func() {
		defer close(done)

		for v := range seq {
			values <- v
		}
	}()

	return values, done
}

func value(t *testing.T, values chan watchedText, done chan struct{}, errf func() error) watchedText {
	t.Helper()

	select {
	case v := <-values:
		return v
	case <-done:
		t.Fatalf("the stream ended before a value arrived: %v", errf())
	case <-time.After(2 * time.Second):
		t.Fatal("no value arrived and the stream did not end")
	}

	return watchedText{}
}

func endsWith(t *testing.T, done chan struct{}, errf func() error) error {
	t.Helper()

	select {
	case <-done:
		return errf()
	case <-time.After(2 * time.Second):
		t.Fatal("the stream did not end")
	}

	return nil
}

// TestWatchedReloadsOnAChange is the whole of the conversion: a write anywhere
// under the driver's key is a change, and the next value off the stream is a
// fresh load.
func TestWatchedReloadsOnAChange(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	store := newFake()

	wb, err := ferry.BindWatched[watchedText](watchedSource(store))
	if err != nil {
		t.Fatalf("BindWatched: %v", err)
	}

	seq, errf := wb.Watch(ctx)
	values, done := streamOf(seq)

	first := value(t, values, done, errf)
	if first.Text != "" {
		t.Fatalf("the stream opened with %q, want the empty key's value", first.Text)
	}

	if err := store.Set(ctx, "", "Text", winreg.Datum{Type: winreg.TypeString, Text: "fresh"}); err != nil {
		t.Fatalf("writing to the store: %v", err)
	}

	if after := value(t, values, done, errf); after.Text != "fresh" {
		t.Errorf("the reload produced %q, want fresh", after.Text)
	}

	if first.Text != "" {
		t.Errorf("the held value became %q, so a reload mutated it", first.Text)
	}
}

// TestWatchedStopsWithItsContext is the lifecycle, which is the context handed
// to Watch and nothing else.
func TestWatchedStopsWithItsContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	wb, err := ferry.BindWatched[watchedText](watchedSource(newFake()))
	if err != nil {
		t.Fatalf("BindWatched: %v", err)
	}

	seq, errf := wb.Watch(ctx)
	values, done := streamOf(seq)

	value(t, values, done, errf)

	cancel()

	if err := endsWith(t, done, errf); !errors.Is(err, context.Canceled) {
		t.Errorf("the stream ended with %v, want the cancellation", err)
	}
}

// TestWatchedThatIsLostEndsTheStream is the ending the callback Option could
// not report: a registration that fails at the wait is a lost watch, and the
// caller observes it rather than waiting for a change that will never come.
func TestWatchedThatIsLostEndsTheStream(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	wb, err := ferry.BindWatched[watchedText](watchedSource(newFake().failWatch()))
	if err != nil {
		t.Fatalf("BindWatched: %v", err)
	}

	seq, errf := wb.Watch(ctx)
	values, done := streamOf(seq)

	// The stream opens with a load, so the first value is taken before the
	// wait that reports the loss.
	value(t, values, done, errf)

	if err := endsWith(t, done, errf); !errors.Is(err, ferry.ErrWatchLost) {
		t.Errorf("the stream ended with %v, want a lost watch", err)
	}
}

// TestWatchedThatEndsQuietlyStillEndsTheStream: a registry that stops reporting
// without saying why is still an ending the caller sees, rather than a silence.
func TestWatchedThatEndsQuietlyStillEndsTheStream(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	wb, err := ferry.BindWatched[watchedText](watchedSource(newFake().endWatch()))
	if err != nil {
		t.Fatalf("BindWatched: %v", err)
	}

	seq, errf := wb.Watch(ctx)
	values, done := streamOf(seq)

	value(t, values, done, errf)

	if err := endsWith(t, done, errf); !errors.Is(err, ferry.ErrWatchLost) {
		t.Errorf("the stream ended with %v, want a watch that is over", err)
	}
}

// TestWatchedIsRefusedByARegistryThatReportsNothing is the instance refusal,
// and it lands at the bind: a registry that is no notifier can never fire, so
// saying so before any load is what the refusal is for.
func TestWatchedIsRefusedByARegistryThatReportsNothing(t *testing.T) {
	t.Parallel()

	_, err := ferry.BindWatched[watchedText](watchedSource(quiet{newFake()}))
	if !errors.Is(err, ferry.ErrPlane) {
		t.Fatalf("binding a source over a registry that reports nothing gave %v, want a refusal at bind", err)
	}

	if !errors.Is(err, winreg.ErrWatch) {
		t.Errorf("the refusal is %v, which does not carry this driver's own reason", err)
	}
}

// TestWatchedWhoseRegistrationCannotBePlacedEndsTheStream: a registration the
// registry will not place is a watch that could never fire, and it is reported
// rather than waited on.
func TestWatchedWhoseRegistrationCannotBePlacedEndsTheStream(t *testing.T) {
	t.Parallel()

	wb, err := ferry.BindWatched[watchedText](watchedSource(newFake().failArm()))
	if err != nil {
		t.Fatalf("BindWatched: %v", err)
	}

	seq, errf := wb.Watch(t.Context())
	for range seq {
		t.Fatal("a watch whose registration cannot be placed yielded a value")
	}

	err = errf()
	if !errors.Is(err, ferry.ErrWatchLost) {
		t.Fatalf("the stream ended with %v, want a lost watch", err)
	}

	if !errors.Is(err, winreg.ErrWatch) {
		t.Errorf("the refusal is %v, which does not carry this driver's own reason", err)
	}
}

// TestAChangeDuringTheReloadIsNotLost is the promise this driver's mechanism
// cannot keep on its own: a registration is armed once and consumed once, so
// the next one has to be placed before the reload runs.
func TestAChangeDuringTheReloadIsNotLost(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	store := newFake()

	wb, err := ferry.BindWatched[watchedText](watchedSource(store))
	if err != nil {
		t.Fatalf("BindWatched: %v", err)
	}

	seq, errf := wb.Watch(ctx)
	values, done := streamOf(seq)

	value(t, values, done, errf)

	if err := store.Set(ctx, "", "Text", winreg.Datum{Type: winreg.TypeString, Text: "first"}); err != nil {
		t.Fatalf("writing to the store: %v", err)
	}

	// The stream is now inside the reload for "first", holding a value nobody
	// has taken. The second write lands in exactly the window a registration
	// placed after the reload would have missed.
	if err := store.Set(ctx, "", "Text", winreg.Datum{Type: winreg.TypeString, Text: "second"}); err != nil {
		t.Fatalf("writing to the store: %v", err)
	}

	// Both reloads arrive, and the last one holds what the store holds now.
	for range 2 {
		if got := value(t, values, done, errf); got.Text == "second" {
			return
		}
	}

	t.Error("the change that landed during the reload was never reported")
}

// TestAWatchedSourceStillLoads: the conversion changes nothing about loading.
func TestAWatchedSourceStillLoads(t *testing.T) {
	t.Parallel()

	store := newFake()
	if err := store.Set(t.Context(), "", "Text", winreg.Datum{Type: winreg.TypeString, Text: "held"}); err != nil {
		t.Fatalf("writing to the store: %v", err)
	}

	got, err := ferry.Load[watchedText](t.Context(), watchedSource(store))
	if err != nil {
		t.Fatalf("loading through a watched source: %v", err)
	}

	if got.Text != "held" {
		t.Errorf("the load holds %q, want held", got.Text)
	}

	// ferry.BindWatched[watchedText](source(store)) does not compile: a
	// *winreg.Source has no Watching method and there is no conversion.
}
