package env

// sinkConfig is a [DotEnvSink]'s settled configuration, copied into every writer
// so that a sink reconfigured after Bind cannot change a dump already under way
// (ADR-0012).
//
// It embeds the naming half rather than repeating it, which is what lets one
// [Naming] be handed to both constructors: the fold, the root variable and the
// boolean words mean the same thing in both directions, and the two would
// otherwise be free to drift.
type sinkConfig struct {
	config

	durable bool

	// proc is where the optional second half of a dump writes, and it is nil
	// until [Setenv] asks for one.
	proc Process
}

// sinkDefaults is the configuration a [DotEnvSink] starts from.
//
// It takes the source's defaults for the naming half, environ included, even
// though a sink reads no environment: [config.validateNaming] is the only check
// a sink makes, so the field is inert here and sharing one defaults() keeps the
// two halves' separator from drifting apart in the source.
func sinkDefaults() sinkConfig { return sinkConfig{config: defaults()} }

// sinkOnly is the implementation behind a setting only [NewDotEnvSink] takes,
// and the whole of what stops it being handed to [New].
type sinkOnly func(*sinkConfig)

func (f sinkOnly) applySink(c *sinkConfig) { f(c) }

// Durable makes a save survive a crash. The replacement's contents and the
// rename that makes your path point at them are both flushed to the disk before
// [ferry.Dump] returns.
//
//	err := ferry.Dump(ctx, cfg, env.NewDotEnvSink(".env", env.Durable()))
//
// Without it a save is still atomic: the staged file is still renamed into
// place, so nothing ever reads a half-written file and a save that fails still
// leaves yours byte for byte as it was. What it is not is written out. The
// replacement sits in the operating system's cache until the kernel gets around
// to it, and a machine that loses power in that window comes back to the old
// file.
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
func Durable() SinkOption {
	return sinkOnly(func(c *sinkConfig) { c.durable = true })
}
