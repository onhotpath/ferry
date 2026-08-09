package watch

import (
	"context"
	"iter"

	"github.com/onhotpath/ferry"
)

// Signal records that a plane may have changed.
//
// Build one with [New], hand [Signal.Changed] to whatever the driver's watch
// option takes, and range [Values] to receive a freshly loaded value per change.
//
// It holds one pending change and no more, so a burst is one change and so is a
// change that lands while a reload is already running. It carries no payload,
// because the announcement it records carries none: the reload is what reads
// what the plane holds now.
//
// It is safe for use from many goroutines. One range at a time is what it is
// for, and two ranges over the same Signal share the pending change out between
// them rather than each receiving it.
type Signal struct {
	fired chan struct{}
}

// New returns a Signal with nothing pending.
func New() *Signal {
	// One slot is the coalescing rule: a burst of announcements is one pending
	// change, and a change landing mid-reload is the next one (ADR-0020).
	return &Signal{fired: make(chan struct{}, 1)}
}

// Changed records a change and returns immediately.
//
// It never blocks and never fails, so a driver's watching goroutine feels no
// back pressure from a slow consumer, and calling it with nobody ranging is
// normal: the change waits, and the next [Values] opens with it.
//
// Its method value has type func(context.Context), which is the shape a
// driver's watch option takes. The context argument is unused, because a change
// announcement carries nothing, including a deadline.
func (s *Signal) Changed(context.Context) {
	select {
	case s.fired <- struct{}{}:
	default:
		// One is already pending, and one pending change is all a reload needs
		// (ADR-0020). Dropping this one is what keeps the driver's goroutine
		// free of the consumer's pace.
	}
}

// Values streams a freshly loaded value of T for every change s records.
//
// Range the sequence, then read the error function after the range exits:
//
//	seq, errf := watch.Values(ctx, s, b)
//	for cfg := range seq {
//		publish(cfg)
//	}
//	if err := errf(); err != nil {
//		alert(err)
//	}
//
// Every value comes from a load through b, so it is brand new and a value
// handed out earlier never changes underneath the goroutine holding it. A
// change s recorded before this call - including one that landed before
// [ferry.Bind] returned - is not lost, and the stream opens with that reload.
// The first value otherwise arrives on the first change, never before it: load
// once through b for the value to start from, or call s.Changed before ranging
// to open the stream with the plane's current contents.
//
// The stream ends on the first failed reload or on cancellation of ctx, and
// errf reports why, once, after the range exits. The load's error passes through
// untouched, so [errors.Is] against ferry's sentinels answers what went wrong;
// a cancelled context reports ctx.Err. Breaking out of the range is a clean
// ending and errf reports nil, as does calling errf before the range has exited,
// which reports nothing useful.
//
// Recovery from a failure is calling Values again on the same s. Nothing is lost
// in between: a change that lands while no stream is ranging is pending when the
// next one opens.
//
// The reload runs on the ranging goroutine and no goroutine is started here, so
// the stream lives exactly as long as the range does.
//
// Sharp edges. The context a driver watches under and ctx here are different
// values, and passing one that outlives the driver's leaves a range waiting on a
// signal nothing will fire again. One range per Signal at a time: two ranges
// share the pending changes out rather than each seeing them, which is not
// policed. And a change is only ever a hint, so a coalesced or spurious one
// costs one load and yields a value equal to the last.
func Values[T any](ctx context.Context, s *Signal, b *ferry.Binding[T]) (seq iter.Seq[T], errf func() error) {
	// The error is delivered beside the sequence rather than inside it, which is
	// ferry's convention for a fallible iterator (ADR-0020).
	var streamErr error

	seq = func(yield func(T) bool) {
		streamErr = stream(ctx, s, b, yield)
	}

	return seq, func() error { return streamErr }
}

// stream is the loop itself, returning the error that ended it: nil when the
// caller stopped ranging.
//
// A failed reload ends the stream, and rebuilding the iterator is the caller's
// policy rather than ferry's (ADR-0020).
func stream[T any](ctx context.Context, s *Signal, b *ferry.Binding[T], yield func(T) bool) error {
	for {
		v, err := next(ctx, s, b)
		if err != nil {
			return err
		}

		if !yield(v) {
			return nil
		}
	}
}

// next waits for one change and loads through the binding.
//
// The binding is safe to load through from any goroutine, so the reload runs
// here on the ranging goroutine and needs no goroutine of its own (ADR-0019).
// The load's error is returned as it came back, since the caller matches it
// against ferry's sentinels (ADR-0011).
func next[T any](ctx context.Context, s *Signal, b *ferry.Binding[T]) (T, error) {
	var zero T

	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case <-s.fired:
	}

	// A select with both cases ready picks either one, so a cancelled stream
	// would otherwise yield one more value after the cancellation.
	if err := ctx.Err(); err != nil {
		return zero, err
	}

	return b.Load(ctx)
}
