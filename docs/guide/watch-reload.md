# Watch and reload

ferry ships no watcher, and that is a decision, not a gap.
Everything a watcher needs is already exported, a complete watcher is about thirty lines, and the parts ferry cannot know - what signals a change, how to debounce it, where to publish the new value - are yours.
This page is the pattern; the runnable version lives in `examples/watcher` and is what this page quotes.

## Reload is Load

A reload produces a new value.
Hold a `Binding[T]`, and every call to `Load` opens the plane fresh, walks it, and returns a brand-new `T`; values you handed out earlier never change.
There is no `Reload` method because it would be a second name for `Load`.

`LoadOver` is not the watcher's tool.
It exists to carry a seed forward deliberately, and it has two properties that are wrong for a watch loop:

- an address the plane has lost keeps the seed's value, silently;
- a slice or map is replaced wholesale by whatever the plane holds, never merged, while struct fields carry over individually.

If your loop needs "current config, refreshed", call `Load`.

## The loop

The driver owns the change signal: fsnotify for a file, a watch plan for Consul, a channel in a test.
The loop turns signals into fresh values:

```go
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
					return
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
```

The shape is `(iter.Seq[T], func() error)` deliberately.
A watch error is a production incident, and this is the shape where discarding it takes a visible `_` at the call site rather than a dropped second range variable.
The convention: the stream ends on the first failed reload or on context cancellation, and the error function answers why, once, after the range exits.

```go
seq, errf := Watch(ctx, binding, signal)
for cfg := range seq {
	publish(cfg) // replace the pointer, never mutate the old value
}
if err := errf(); err != nil {
	alert(err)
}
```

## The sharp edges

**Publish by replacement.**
A loaded value is yours alone; goroutines holding the previous value keep it unchanged.
Swap an atomic pointer, send on a channel, or replace under a short lock - never write into a struct another goroutine can see.

**A dump feeds your own watcher.**
If the same process dumps to the plane it watches, its own writes fire its own signal.
Coalesce, compare, or mark your own writes; the loop above deliberately does not hide this.

**Signals may coalesce and may lie.**
Treat a signal as "the plane may have changed", nothing more.
The reload reads the truth; a spurious wake costs one load.

**yaml specifics.**
File watching for the yaml driver is opt-in through a driver option, and a dump refuses at commit when the file changed underneath it between open and swap, so the dump that lost a race learns it lost instead of silently overwriting; re-dump after reloading.
