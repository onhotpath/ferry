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

// The watching tests. They run against the fake store, which models the
// register-once-wait-once mechanism the machine's own registry has, so every one
// of them runs on every operating system.
//
// machine_windows_test.go is where the same seam is put to
// RegNotifyChangeKeyValue instead, and it is the only place the real
// notification is entered.

// watchedSource converts a source over the given store, which is the whole of
// the wiring a caller writes.
func watchedSource(store winreg.Registry) *winreg.WatchedSource {
	return source(store).Watched()
}

// stream is one ranged watch: the values it hands over, the channel that closes
// when it ends, and what it ended with.
type stream struct {
	values chan oneText
	done   chan struct{}
	errf   func() error
}

// watchOf binds a watched source and ranges it on a goroutine of its own, which
// is the opening of every case below and what lets a test take one value and
// then move the plane.
//
// The context is this stream's own and ends with the test, so no case has a
// lifetime to write down and none of them leaves a goroutine behind.
func watchOf(t *testing.T, src *winreg.WatchedSource) *stream {
	t.Helper()

	wb, err := ferry.BindWatched[oneText](src)
	if err != nil {
		t.Fatalf("BindWatched: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	seq, errf := wb.Watch(ctx)

	return rangeOf(ctx, seq, errf)
}

// rangeOf ranges the sequence and hands values over one at a time.
//
// The send races the context so that a value nobody took does not hold the
// ranging goroutine open past the end of the test.
func rangeOf(ctx context.Context, seq iter.Seq[oneText], errf func() error) *stream {
	s := &stream{values: make(chan oneText), done: make(chan struct{}), errf: errf}

	go func() {
		defer close(s.done)

		for v := range seq {
			select {
			case s.values <- v:
			case <-ctx.Done():
				return
			}
		}
	}()

	return s
}

// patience is how long a case waits for a value or an ending before it calls the
// stream stuck.
//
// It is generous for a store that answers in memory and it is not the number the
// machine tests use: the real notification runs on whatever a Windows runner has
// left, so machine_windows_test.go asks for its own.
const patience = 2 * time.Second

// next is the value the stream hands over, and a failure where the stream ended
// or said nothing at all instead.
func (s *stream) next(t *testing.T) oneText { return s.nextWithin(t, patience) }

// nextWithin is next, waiting as long as the caller says.
func (s *stream) nextWithin(t *testing.T, d time.Duration) oneText {
	t.Helper()

	select {
	case v := <-s.values:
		return v
	case <-s.done:
		t.Fatalf("the stream ended before a value arrived: %v", s.errf())
	case <-time.After(d):
		t.Fatal("no value arrived and the stream did not end")
	}

	return oneText{}
}

// end is what the stream ended with, and a failure where it has not ended.
func (s *stream) end(t *testing.T) error {
	t.Helper()

	select {
	case <-s.done:
		return s.errf()
	case <-time.After(patience):
		t.Fatal("the stream did not end")
	}

	return nil
}

// write puts one value under the driver's own key, which is one change.
func write(t *testing.T, store winreg.Registry, text string) {
	t.Helper()

	if err := store.Set(t.Context(), "", "text", winreg.Datum{Type: winreg.TypeString, Text: text}); err != nil {
		t.Fatalf("writing to the store: %v", err)
	}
}

// TestAWatchThatIsLostEndsTheStream is the ending the callback option could not
// report: a registration that fails at the wait is a lost watch, and the caller
// observes it rather than waiting for a change that will never come.
func TestAWatchThatIsLostEndsTheStream(t *testing.T) {
	t.Parallel()

	s := watchOf(t, watchedSource(newFake().failWatch()))

	// The stream opens with a load, so the first value is taken before the wait
	// that reports the loss.
	s.next(t)

	err := s.end(t)
	if !errors.Is(err, ferry.ErrWatchLost) {
		t.Fatalf("the stream ended with %v, want a lost watch", err)
	}

	if !errors.Is(err, errFake) {
		t.Errorf("the ending does not carry what the registry said: %v", err)
	}
}

// TestAWatchThatEndsQuietlyStillEndsTheStream: a registry that stops reporting
// without saying why is still an ending the caller sees, rather than a silence.
func TestAWatchThatEndsQuietlyStillEndsTheStream(t *testing.T) {
	t.Parallel()

	s := watchOf(t, watchedSource(newFake().endWatch()))
	s.next(t)

	if err := s.end(t); !errors.Is(err, ferry.ErrWatchLost) {
		t.Errorf("the stream ended with %v, want a watch that is over", err)
	}
}

// TestWatchingIsRefusedByARegistryThatReportsNothing is the instance refusal,
// and it lands at the bind: a registry that is no notifier can never fire, so
// saying so before any load is what the refusal is for.
func TestWatchingIsRefusedByARegistryThatReportsNothing(t *testing.T) {
	t.Parallel()

	_, err := ferry.BindWatched[oneText](watchedSource(quiet{newFake()}))
	if !errors.Is(err, ferry.ErrPlane) {
		t.Fatalf("binding a source over a registry that reports nothing gave %v, want a refusal at bind", err)
	}

	if !errors.Is(err, winreg.ErrWatch) {
		t.Errorf("the refusal is %v, which does not carry this driver's own reason", err)
	}
}

// TestWatchingIsRefusedWhenTheOptionsDidNotResolve is the other bind refusal a
// watched source makes, and it is the source's own: an option this driver cannot
// use is refused before the registry is asked whether it reports changes, so the
// caller is told what is actually wrong.
func TestWatchingIsRefusedWhenTheOptionsDidNotResolve(t *testing.T) {
	t.Parallel()

	src := winreg.NewSource(winreg.Hive(0), base, winreg.Store(newFake())).Watched()

	_, err := ferry.BindWatched[oneText](src)
	if !errors.Is(err, winreg.ErrOption) {
		t.Fatalf("binding a source built over no hive gave %v, want the option refusal", err)
	}

	if errors.Is(err, winreg.ErrWatch) {
		t.Errorf("the refusal is %v, which blames the watch for an option this driver cannot use", err)
	}
}

// TestARegistrationThatCannotBePlacedEndsTheStream: a registration the registry
// will not place is a watch that could never fire.
//
// It ends the stream rather than the bind, because placing it is I/O and
// [winreg.Source.Watched] does none: the first registration goes down when a
// stream opens, under that stream's own context.
func TestARegistrationThatCannotBePlacedEndsTheStream(t *testing.T) {
	t.Parallel()

	s := watchOf(t, watchedSource(newFake().failArm()))

	err := s.end(t)
	if !errors.Is(err, ferry.ErrWatchLost) {
		t.Fatalf("the stream ended with %v, want a lost watch", err)
	}

	if !errors.Is(err, winreg.ErrWatch) {
		t.Errorf("the ending is %v, which does not carry this driver's own reason", err)
	}
}

// TestAWatchThatCannotBeReArmedEndsTheStream is the failure the two-call
// registration adds: the wait answered with a change and the next registration
// could not be placed.
//
// It is a lost watch like any other, and the caller hears it rather than holding
// a value nothing will ever refresh.
func TestAWatchThatCannotBeReArmedEndsTheStream(t *testing.T) {
	t.Parallel()

	store := newFake()
	s := watchOf(t, watchedSource(store))

	s.next(t)

	store.failArm()
	write(t, store, "fresh")

	if err := s.end(t); !errors.Is(err, ferry.ErrWatchLost) {
		t.Errorf("the stream ended with %v, want a lost watch", err)
	}
}

// TestAChangeDuringTheReloadIsNotLost is the promise this driver's mechanism
// cannot keep on its own: a registration is armed once and consumed once, so the
// next one has to be placed before the reload runs.
func TestAChangeDuringTheReloadIsNotLost(t *testing.T) {
	t.Parallel()

	store := newFake()
	s := watchOf(t, watchedSource(store))

	s.next(t)
	write(t, store, "first")

	// The stream is now inside the reload for "first", holding a value nobody
	// has taken. The second write lands in exactly the window a registration
	// placed after the reload would have missed.
	write(t, store, "second")

	// Both reloads arrive, and the last one holds what the store holds now.
	for range 2 {
		if got := s.next(t); got.Text == "second" {
			return
		}
	}

	t.Error("the change that landed during the reload was never reported")
}

// TestAWatchOverAKeyThatIsNotThereYetFiresWhenItAppears is the bootstrap case: a
// process that watches the key its own first dump will create.
//
// There is no special case in the caller. The registration goes on the nearest
// key above one that is not there, so creating it is a change like any other, and
// refusing instead would leave a configuration that never reloads and says
// nothing about it.
func TestAWatchOverAKeyThatIsNotThereYetFiresWhenItAppears(t *testing.T) {
	t.Parallel()

	store := newFake()
	if err := store.DeleteKey(t.Context(), ""); err != nil {
		t.Fatalf("removing the driver's own key: %v", err)
	}

	s := watchOf(t, watchedSource(store))

	if first := s.next(t); first.Text != "" {
		t.Fatalf("the stream opened with %q over a key that is not there", first.Text)
	}

	// The caller's own first dump, through the write half over the same store,
	// which is what creates the key being watched.
	if err := ferry.Dump(t.Context(), oneText{Text: "written"}, sink(store)); err != nil {
		t.Fatalf("the first dump: %v", err)
	}

	for range 2 {
		if got := s.next(t); got.Text == "written" {
			return
		}
	}

	t.Error("the watch never reported the key its own first dump created")
}

// TestAWatchedSourceStillLoads: the conversion changes nothing about loading.
//
// ferry.BindWatched over the source itself does not compile, because a
// *winreg.Source has no Watching method and the conversion is the only way to
// one.
func TestAWatchedSourceStillLoads(t *testing.T) {
	t.Parallel()

	store := newFake()
	write(t, store, "held")

	got, err := ferry.Load[oneText](t.Context(), watchedSource(store))
	if err != nil {
		t.Fatalf("loading through a watched source: %v", err)
	}

	if got.Text != "held" {
		t.Errorf("the load holds %q, want held", got.Text)
	}
}
