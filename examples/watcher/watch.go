// Package watcher is a working watcher built from ferry's exported surface
// alone: bind once, reload on a change signal, publish a fresh value.
//
// ferry ships no watcher, because what a watcher needs beyond Load is not
// core's to know: what signals a change, how to debounce it, and where the new
// value is published. The driver owns the signal - fsnotify for a file, a watch
// plan for a key-value store, a channel here - and the caller owns the loop.
//
// Watch is that loop, MemPlane is a plane small enough to read in one sitting,
// and the examples in this package are the two together, end to end.
package watcher

import (
	"context"
	"iter"

	"github.com/onhotpath/ferry"
)

// Watch turns a plane's change signal into a stream of freshly loaded values.
//
// Call it with a binding and whatever channel the driver signals changes on,
// then range the sequence. Every signal reloads through the binding and yields
// a brand-new T, so a value handed out earlier never changes underneath the
// goroutine holding it.
//
//	seq, errf := Watch(ctx, b, signal)
//	for cfg := range seq {
//		publish(cfg) // replace the pointer, never mutate the old value
//	}
//	if err := errf(); err != nil {
//		alert(err)
//	}
//
// The stream ends on the first failed reload or on cancellation of ctx, and
// errf answers why, once, after the range exits. It reports nil for the two
// clean endings, which are the driver closing the signal and the caller
// breaking out of the range; called before the range exits it reports nothing
// useful.
//
// Sharp edges. A signal says only that the plane may have changed, so a
// coalesced or spurious wake costs one load and nothing else, and the reload
// reads the plane's current contents either way. A process that also dumps to
// the plane it watches fires its own signal, so mark or compare those writes if
// the echo matters. And the first value arrives on the first signal, never
// before it: load once through the binding for the value to start from.
func Watch[T any](
	ctx context.Context, b *ferry.Binding[T], signal <-chan struct{},
) (seq iter.Seq[T], errf func() error) {
	var streamErr error
	seq = func(yield func(T) bool) {
		streamErr = stream(ctx, b, signal, yield)
	}
	return seq, func() error { return streamErr }
}

// stream is the loop itself, returning the error that ended it: nil when the
// driver closed the signal or the caller stopped ranging.
func stream[T any](ctx context.Context, b *ferry.Binding[T], signal <-chan struct{}, yield func(T) bool) error {
	for {
		v, ok, err := reload(ctx, b, signal)
		if err != nil {
			return err
		}
		if !ok || !yield(v) {
			return nil
		}
	}
}

// reload waits for one signal and loads through the binding.
//
// ok is false with a nil error for the one clean ending it can see, which is
// the driver closing the signal. A cancelled context and a failed load are both
// errors, and both end the stream.
func reload[T any](ctx context.Context, b *ferry.Binding[T], signal <-chan struct{}) (v T, ok bool, err error) {
	var zero T
	select {
	case <-ctx.Done():
		return zero, false, ctx.Err()
	case _, open := <-signal:
		if !open {
			return zero, false, nil
		}
	}
	v, err = b.Load(ctx)
	if err != nil {
		return zero, false, err
	}
	return v, true, nil
}
