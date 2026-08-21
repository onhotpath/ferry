//go:build !protoe

package winreg_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/windows/winreg"
)

// The watching tests of the callback Option, kept whole and kept out of the
// protoe build, where that Option does not exist.

// TestAWatchThatIsLostSpeaksOnce is ADR-0020's one loud ending: there is nowhere
// to report a lost watch, so the callback runs once and the load that follows
// reports whatever is actually wrong.
func TestAWatchThatIsLostSpeaksOnce(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	fired := make(chan struct{}, 4)

	_ = source(newFake().failWatch(), winreg.Watch(ctx, func(context.Context) { fired <- struct{}{} }))

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("a lost watch said nothing")
	}

	// One call and then the goroutine returns, so nothing else arrives.
	select {
	case <-fired:
		t.Error("a lost watch called back more than once")
	case <-time.After(50 * time.Millisecond):
	}
}

// TestAWatchStopsSilentlyWhenItsContextEnds is the other ending, and the only way
// to stop a watch: cancelling is not a change, so nothing is reported.
func TestAWatchStopsSilentlyWhenItsContextEnds(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	store := newFake()
	fired := make(chan struct{}, 1)

	_ = source(store, winreg.Watch(ctx, func(context.Context) { fired <- struct{}{} }))

	if err := store.Create(t.Context(), "poke"); err != nil {
		t.Fatalf("poking the store: %v", err)
	}

	select {
	case <-fired:
		t.Error("a cancelled watch called back")
	case <-time.After(50 * time.Millisecond):
	}
}

// TestAWatchWithNoCallbackIsNoWatch is the one shape [winreg.Watch] takes and
// does nothing with: there is nothing to call, so there is nothing to open and
// nothing to refuse.
func TestAWatchWithNoCallbackIsNoWatch(t *testing.T) {
	t.Parallel()

	if err := bindOf[oneText](source(quiet{newFake()}, winreg.Watch(t.Context(), nil))); err != nil {
		t.Errorf("Bind refused a source whose watch has no callback: %v", err)
	}
}

// TestAWatchThatEndsQuietlyStopsWithoutSpeaking is the third ending a watch has:
// a registry that has stopped reporting without anything having gone wrong.
//
// Nothing is called back, because nothing changed. Only losing the watch speaks.
func TestAWatchThatEndsQuietlyStopsWithoutSpeaking(t *testing.T) {
	t.Parallel()

	fired := make(chan struct{}, 1)

	_ = source(newFake().endWatch(), winreg.Watch(t.Context(), func(context.Context) { fired <- struct{}{} }))

	select {
	case <-fired:
		t.Error("a watch that ended quietly called back")
	case <-time.After(50 * time.Millisecond):
	}
}

// TestAWatchThatCannotBeReArmedSpeaksOnce is the fourth ending, and the one the
// two-call registration adds: the wait answered with a change and the next
// registration could not be placed.
//
// It is a lost watch like any other, so it says so once and stops. The callback
// that would have followed the change is the one call, because there is nothing
// left to hear the next one with.
func TestAWatchThatCannotBeReArmedSpeaksOnce(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	store := newFake()
	fired := make(chan struct{}, 4)

	_ = source(store, winreg.Watch(ctx, func(context.Context) { fired <- struct{}{} }))

	store.failArm()

	if err := store.Create(ctx, "poke"); err != nil {
		t.Fatalf("poking the store: %v", err)
	}

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("a watch that could not be re-armed said nothing")
	}

	select {
	case <-fired:
		t.Error("a lost watch called back more than once")
	case <-time.After(50 * time.Millisecond):
	}
}

// TestWatchCallsBackOnAChange is ADR-0020's shape asserted end to end: a callback
// rather than a channel, an Option rather than a method, no Stop, and cancellation
// riding the context.
func TestWatchCallsBackOnAChange(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	store := newFake()
	fired := make(chan struct{}, 1)

	_ = source(store, winreg.Watch(ctx, func(context.Context) {
		select {
		case fired <- struct{}{}:
		default:
		}
	}))

	// One change and no polling. The registration is placed in the constructor,
	// on the caller's own goroutine, so it is already live when this returns and
	// a change after it cannot land in a gap.
	poke(t, store, "poke")

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("the watch never called back")
	}
}

// TestAChangeDuringTheCallbackIsNotLost is the window the two-call registration
// exists to close, and the property [winreg.Watch] documents.
//
// A watcher that registered inside its own wait would have no registration for
// the whole of the callback and the load inside it, so a change landing there
// would fire nothing ever again. The next registration is placed before the
// callback runs instead, so a change during it is one call afterwards.
func TestAChangeDuringTheCallbackIsNotLost(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var (
		store   = newFake()
		inside  = make(chan struct{})
		release = make(chan struct{})
		again   = make(chan struct{}, 1)
		calls   atomic.Int32
	)

	_ = source(store, winreg.Watch(ctx, func(context.Context) {
		if calls.Add(1) == 1 {
			close(inside)
			<-release

			return
		}

		select {
		case again <- struct{}{}:
		default:
		}
	}))

	poke(t, store, "first")

	select {
	case <-inside:
	case <-time.After(2 * time.Second):
		t.Fatal("the watch never called back")
	}

	// The callback is in hand and nothing else is poking the store, so this is
	// the change that lands in the window.
	poke(t, store, "during")
	close(release)

	select {
	case <-again:
	case <-time.After(2 * time.Second):
		t.Fatal("a change that landed while the callback was running was lost")
	}
}

// TestAWatchOverAKeyThatIsNotThereYetFiresWhenItAppears is the bootstrap case: a
// process that watches the key its own first save will create.
//
// The registration goes on the nearest key above one that is not there, so
// creating it is a change like any other. Refusing instead would leave a
// configuration that never reloads and says nothing about it.
func TestAWatchOverAKeyThatIsNotThereYetFiresWhenItAppears(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	store := newFake()
	if err := store.DeleteKey(ctx, ""); err != nil {
		t.Fatalf("removing the driver's own key: %v", err)
	}

	fired := make(chan struct{}, 1)

	_ = source(store, winreg.Watch(ctx, func(context.Context) {
		select {
		case fired <- struct{}{}:
		default:
		}
	}))

	poke(t, store, "")

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("the watch never called back when the key it watches appeared")
	}
}

// TestWatchIsRefusedByARegistryThatReportsNothing is the other half of ADR-0020's
// rule: a watch that opens successfully and never fires is the failure the option
// exists to avoid, so it is refused at Bind instead.
func TestWatchIsRefusedByARegistryThatReportsNothing(t *testing.T) {
	t.Parallel()

	src := source(quiet{newFake()}, winreg.Watch(t.Context(), func(context.Context) {}))

	err := bindOf[oneText](src)
	if !errors.Is(err, winreg.ErrWatch) {
		t.Fatalf("Bind answered %v, want an error reaching winreg.ErrWatch", err)
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("the refusal does not reach ferry.ErrPlane: %v", err)
	}
}

// TestAWatchThatCannotBeRegisteredIsRefusedAtBind is the other failure that
// happens before any wait: the registry reports changes in principle and would
// not take this registration.
//
// It lands at Bind because the registration is placed in the constructor, on the
// caller's own goroutine, which is the whole reason it is placed there.
func TestAWatchThatCannotBeRegisteredIsRefusedAtBind(t *testing.T) {
	t.Parallel()

	src := source(newFake().failArm(), winreg.Watch(t.Context(), func(context.Context) {}))

	err := bindOf[oneText](src)
	if !errors.Is(err, winreg.ErrWatch) {
		t.Fatalf("Bind answered %v, want an error reaching winreg.ErrWatch", err)
	}

	if !errors.Is(err, errFake) {
		t.Errorf("the refusal does not carry what the registry said: %v", err)
	}
}

// poke makes one change in a store, which is what every watch test waits for.
func poke(t *testing.T, store *fake, subkey string) {
	t.Helper()

	if err := store.Create(t.Context(), subkey); err != nil {
		t.Fatalf("poking the store: %v", err)
	}
}
