package yaml

import (
	"context"
	"time"
)

// Option is a setting handed to [NewSink]. The set is closed at one: [Durable].
type Option interface {
	apply(*config)
}

// SourceOption is a setting handed to [NewSource]. The set is closed at one:
// [Watch].
//
// It is a separate type from [Option] so that each constructor takes the
// settings that mean something to it, and the other way round is a compile
// error rather than a setting that is quietly ignored.
type SourceOption interface {
	applySource(*sourceConfig)
}

// optionFunc is the one implementation, which is what makes every option below
// a one-line constructor.
type optionFunc func(*config)

func (f optionFunc) apply(c *config) { f(c) }

// sourceOptionFunc is the same on the read side.
type sourceOptionFunc func(*sourceConfig)

func (f sourceOptionFunc) applySource(c *sourceConfig) { f(c) }

// sourceConfig is a [Source]'s settled configuration.
//
// Its zero value is a source that watches nothing, which is the whole of the
// opt-in: the polling machinery never runs for a caller who did not ask for it
// (ADR-0020).
type sourceConfig struct {
	watch *watch
}

// config is a [Sink]'s settled configuration, copied into every writer so that a
// Sink reconfigured after Bind cannot change a dump already under way
// (ADR-0012).
//
// Its zero value is the default save, so there is no defaults() here: the option
// set is one opt-in and nothing is switched on until somebody asks.
type config struct {
	durable bool
}

// Durable makes a save survive a crash. The replacement's contents and the
// rename that makes your path point at them are both flushed to the disk before
// [ferry.Dump] returns.
//
//	err := ferry.Dump(ctx, cfg, yaml.NewSink("config.yaml", yaml.Durable()))
//
// Without it a save is still atomic: the staged file is still renamed into
// place, so nothing ever reads a half-written config and a save that fails still
// leaves your file byte for byte as it was. What it is not is written out. The
// replacement sits in the operating system's cache until the kernel gets around
// to it, and a machine that loses power in that window comes back to the old
// document. That is what an ordinary file write gives you and it is the default
// here, because the usual reason to write a config file is that something is
// about to read it back.
//
// It buys that at the price of a disk flush, which is the most expensive thing a
// save does and is usually more expensive than everything else in the save put
// together. Ask for it where losing the write would matter, and not by reflex.
//
// Windows has no way to flush a directory, so a durable save there flushes the
// contents and leaves the durability of the rename to the filesystem.
//
// One sharp edge: a flush that fails once the rename has landed is a save that
// failed with your file already replaced. It reports [ferry.ErrPlane], because
// what could not be promised is that the replacement survives a crash, not that
// it happened.
func Durable() Option {
	return optionFunc(func(c *config) { c.durable = true })
}

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
// onChange runs on the watching goroutine and one call at a time. A slow
// callback delays the next look rather than running beside itself, and changes
// that land while it runs are one call afterwards rather than several.
//
// A panic in onChange takes the process down, exactly as it would on a goroutine
// the caller started. Nothing here recovers it: there is no result to hand a
// failure back through, and a watch that swallowed the panic would leave a
// process that has silently stopped reloading. Recover inside the callback if a
// bug there should not be fatal.
//
// Watching starts when the source is built, so it starts before [ferry.Bind]
// has handed back the binding the callback wants to load through. Publish the
// binding to the callback in a way that orders the two - an atomic pointer, or a
// channel the callback reads before it uses one - as the example in this package
// does.
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
	return sourceOptionFunc(func(c *sourceConfig) {
		if onChange == nil {
			return
		}

		if every <= 0 {
			every = look
		}

		c.watch = &watch{ctx: ctx, every: every, onChange: onChange}
	})
}
