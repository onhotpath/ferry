package watcher

// The two candidate delivery shapes for #13, implemented side by side so
// the trade is felt rather than argued. Both are ~30 lines over the
// shipped surface: Binding[T].Load per signal, a fresh value per reload
// (ADR-0006's rule - LoadOver would carry stale values for addresses the
// plane has lost; see the sharp-edge test).

import (
	"context"
	"iter"

	"github.com/onhotpath/ferry"
)

// Watch is the jba shape: (iter.Seq[T], func() error). The compiler warns
// on an unused errf, so a dropped watch error is a compile-visible smell.
// The stream ends on the first reload failure OR on ctx/signal close;
// errf reports which, after the range exits.
func Watch[T any](ctx context.Context, b *ferry.Binding[T], signal <-chan struct{}) (iter.Seq[T], func() error) {
	var streamErr error
	seq := func(yield func(T) bool) {
		for {
			select {
			case <-ctx.Done():
				streamErr = ctx.Err()
				return
			case _, ok := <-signal:
				if !ok {
					return // plane closed its signal: a clean end
				}
			}
			v, err := b.Load(ctx)
			if err != nil {
				streamErr = err
				return
			}
			if !yield(v) {
				return
			}
		}
	}
	return seq, func() error { return streamErr }
}

// Watch2 is the ecosystem-expected shape: iter.Seq2[T, error]. The doc
// comment must answer adonovan's four questions; this one does:
//   - a non-nil error is the FINAL element: the sequence ends after it
//   - no value accompanies an error (the T beside it is the zero value)
//   - the caller cannot continue past an error; re-call Watch2 to resume
//   - breaking out of range simply stops the watch, no error is pending
func Watch2[T any](ctx context.Context, b *ferry.Binding[T], signal <-chan struct{}) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var zero T
		for {
			select {
			case <-ctx.Done():
				yield(zero, ctx.Err())
				return
			case _, ok := <-signal:
				if !ok {
					return
				}
			}
			v, err := b.Load(ctx)
			if err != nil {
				yield(zero, err)
				return
			}
			if !yield(v, nil) {
				return
			}
		}
	}
}
