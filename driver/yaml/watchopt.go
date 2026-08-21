//go:build !protoe

package yaml

import (
	"context"
	"time"
)

// Watch calls onChange whenever the file changes underneath a source, so that a
// process holding a loaded value can load a fresh one.
//
//	b, err := ferry.Bind[Config](yaml.NewSource(path, yaml.Watch(ctx, time.Second, reload)))
//
//	func reload(ctx context.Context) {
//		cfg, err := b.Load(ctx) // a reload is a load
//		...                     // publish it by replacement, never by mutation
//	}
//
// It is opt-in and it is the only thing in this package that runs on a goroutine
// of its own. A source built without it touches the file only when a load asks
// it to.
//
// The watch begins when the source is built and ends when ctx is done, which is
// the only way to stop it: cancel the context you gave it, and the goroutine
// returns. The context reaches onChange as its argument, so a deadline, a
// cancellation and whatever the caller put in it are all in hand there.
//
// Looking is a stat every interval. An interval of zero or less takes one
// second, and a nil onChange watches nothing.
//
// Sharp edges, and they are the reason this is a callback and not a stream.
//
// onChange runs on the watching goroutine and one call at a time. A callback that
// reloads inline is a slow one, and a slow callback delays the next look rather
// than running beside itself, so changes that land while it runs are one call
// afterwards rather than several. The Changed method of a Signal from
// github.com/onhotpath/ferry/watch returns immediately instead, which leaves the
// reload on the goroutine ranging the stream and this one free to keep looking.
//
// A panic in onChange takes the process down, exactly as it would on a goroutine
// the caller started. Nothing here recovers it: there is no result to hand a
// failure back through, and a watch that swallowed the panic would leave a
// process that has silently stopped reloading. Recover inside the callback if a
// bug there should not be fatal.
//
// Watching starts when the source is built, so it starts before [ferry.Bind]
// has handed back the binding the callback wants to load through, and a change
// can land while there is nothing yet to load through. A Signal from
// github.com/onhotpath/ferry/watch is what to pass here in that case: its Changed
// method records such a change rather than losing it, and the stream that opens
// afterwards begins with that reload, as the example in this package does.
//
// A call says the file may have changed and nothing more. Load to find out what
// it holds now, which is correct whether the change was real, coalesced with
// another, or a touch that rewrote the same bytes.
//
// A dump through the same path fires it, so a process that both watches and
// saves its own config hears its own writes.
//
// One change is invisible: a rewrite landing in the same modification-time tick
// that leaves the length alone. Looking costs a stat, and a watch that hashed
// the file instead would read it whole every interval to catch a case an
// operator's editor does not produce.
func Watch(ctx context.Context, every time.Duration, onChange func(context.Context)) SourceOption {
	if every <= 0 {
		every = look
	}

	return watchOpt{w: &watch{ctx: ctx, every: every, onChange: onChange}}
}

// watchOpt is [Watch]'s value, and it is a type of its own so that [NewSource]
// can recognise it in the Option list once it has the path to watch. It settles
// nothing into the config, because what it carries is the whole setting.
type watchOpt struct{ w *watch }

func (watchOpt) applySource(*sourceConfig) {}

// startWatch starts whatever the Option list asked for, and it runs inside
// [NewSource] on the caller's own goroutine.
func startWatch(opts []SourceOption, path string) {
	for _, o := range opts {
		w, ok := o.(watchOpt)
		if !ok || w.w.onChange == nil {
			continue
		}

		w.w.start(path)

		return
	}
}
