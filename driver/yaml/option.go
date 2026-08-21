package yaml

// Option is a setting handed to [NewSink]. The set is closed at one: [Durable].
type Option interface {
	apply(*config)
}

// optionFunc is the one implementation, which is what makes every option below
// a one-line constructor.
type optionFunc func(*config)

func (f optionFunc) apply(c *config) { f(c) }

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
