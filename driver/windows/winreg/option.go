package winreg

import (
	"cmp"
	"errors"
	"fmt"

	"github.com/onhotpath/ferry"
)

// ErrOption reports a driver option this source or sink cannot be built with: a
// hive outside the five this package declares, or a [View] outside the three.
//
// [NewSource] and [NewSink] take options and return no error, so this lands at
// Bind, which is the first moment the driver is asked for anything. It wraps
// [ferry.ErrPlane] and stays reachable under ferry's wrapper, so errors.Is
// answers for it on what [ferry.Load] and [ferry.Dump] returned.
var ErrOption = errors.New("winreg: unusable driver option")

// Option is a setting handed to [NewSource].
//
// Three of them: [WithView] and [Store], which are also [SinkOption]s, and
// [Watch], which belongs to the read half alone.
type Option interface {
	apply(*config)
}

// SinkOption is a setting handed to [NewSink].
//
// Two of them, [WithView] and [Store], and both are [Option]s as well.
//
// It is a separate type from [Option] so that each constructor takes the
// settings that mean something to it, and the other way round is a compile error
// rather than a setting that is quietly ignored.
type SinkOption interface {
	applySink(*config)
}

// Common is a setting both halves take: [WithView] and [Store].
//
// They are shared because a sink writing into one view or one registry and a
// source reading another is a plane that cannot round trip. Source and sink are
// two constructors, so nothing checks that the two agree, and the way to avoid it
// is to build both halves from one slice of these.
type Common interface {
	Option
	SinkOption
}

// optionFunc is the implementation behind every setting both halves take, which
// is what makes each of those a one-line constructor.
type optionFunc func(*config)

func (f optionFunc) apply(c *config)     { f(c) }
func (f optionFunc) applySink(c *config) { f(c) }

// sourceOnly is the implementation behind a setting only [NewSource] takes, and
// the whole of what stops it being handed to [NewSink].
//
// There is deliberately no sinkOnly beside it. Nothing this driver's write half
// takes is a setting the read half does not, and a type nothing constructs is
// dead code the unused linter reports (ADR-0002's lint limits are not raised to
// keep ceremony alive).
type sourceOnly func(*config)

func (f sourceOnly) apply(c *config) { f(c) }

// config is a [Source]'s or a [Sink]'s settled configuration, copied into every
// binding so that one reconfigured after Bind cannot change a binding already
// handed out.
type config struct {
	hive Hive
	base string
	view View

	// store is the registry this driver reads and writes through. It is nil
	// until [config.settle] resolves it, which is [Store]'s argument where one
	// was given and the machine's own registry otherwise.
	store Registry

	// watch is the change notification [Watch] asked for, or nil. It is started
	// inside the constructor, on the caller's own goroutine, so that a watch
	// that cannot be opened has somewhere to report it (ADR-0020).
	watch *watcher

	// err is what building the configuration refused with, held until Bind for
	// the reason [ErrOption] gives: an Option is applied inside a constructor
	// that returns no error, so the refusal waits for the first moment the
	// driver is asked for anything.
	err error
}

// newConfig resolves one option list into the configuration a source or a sink
// runs under, and opens the watch where one was asked for.
//
// apply is the loop over whichever of the two option types the caller has, which
// is the only thing that differs between the two constructors.
func newConfig(hive Hive, subkey string, apply func(*config)) config {
	c := config{hive: hive, base: subkey}

	apply(&c)
	c.settle()

	if c.err == nil && c.watch != nil {
		c.refuse(c.watch.start(c.store))
	}

	return c
}

// settle checks what the options said and resolves the registry behind them.
//
// The registry is resolved here rather than at the open because a watch is opened
// in the constructor and needs one, and because opening it is not I/O: an
// implementation records the hive, the subkey and the view, and every call it
// answers opens and closes the key it needs. That is also what makes this driver
// safe to enter from many goroutines with no handle shared between them.
func (c *config) settle() {
	if c.hive < LocalMachine || c.hive > CurrentConfig {
		c.refuse(optionError("the hive must be one of winreg.LocalMachine, winreg.CurrentUser, " +
			"winreg.ClassesRoot, winreg.Users or winreg.CurrentConfig"))

		return
	}

	if c.view != ViewNative && c.view != View64 && c.view != View32 {
		c.refuse(optionError("the view must be winreg.ViewNative, winreg.View64 or winreg.View32"))

		return
	}

	if c.store != nil {
		return
	}

	store, err := open(c.hive, c.base, c.view)

	c.store = store
	c.refuse(err)
}

// refuse records a refusal, keeping the first so that a configuration with two
// mistakes in it reports the one nearest the beginning rather than the one
// nearest the end.
func (c *config) refuse(err error) { c.err = cmp.Or(c.err, err) }

// validate is what Bind asks before it computes a single key.
func (c *config) validate() error { return c.err }

// name is what this plane calls the key one plane key lies at, which is the hive,
// the subkey the driver was built over, and the key itself.
//
// It is a function of the address and of this driver's own configuration and of
// nothing the registry holds, which is [ferry.PlaneNamer]'s one obligation.
func (c *config) name(key string) string { return joinKey(c.hive.String(), joinKey(c.base, key)) }

// WithView chooses which side of the registry redirector this driver reads and
// writes.
//
//	src := winreg.NewSource(winreg.LocalMachine, `SOFTWARE\Example`, winreg.WithView(winreg.View64))
//
// On 64-bit Windows the registry keeps two copies of parts of the tree, and a
// 32-bit process is redirected into WOW6432Node without being told. So a 32-bit
// service and a 64-bit installer writing "the same" key write two different keys,
// and the way out is for both of them to name the view rather than to inherit it.
//
// It defaults to [ViewNative], which is whatever the running process would get on
// its own.
//
// Give the same one to both halves. A sink writing the 64-bit view and a source
// reading the 32-bit one never meet, and nothing checks that the two agree.
func WithView(v View) Common {
	return optionFunc(func(c *config) { c.view = v })
}

// Store names the registry this driver reads and writes through.
//
//	src := winreg.NewSource(winreg.CurrentUser, `Software\Example`, winreg.Store(fake))
//
// A nil argument is the machine's own registry, which is the default and which
// exists on Windows and nowhere else: elsewhere, a source or a sink built without
// this refuses at Bind with [ErrNoRegistry].
//
// It is what makes a test hermetic, and it is the seam a registry this package
// does not know about arrives through - a remote one, a snapshot of a hive, or a
// store that is registry-shaped without being a registry.
//
// Give the same one to both halves, for the reason [Common] states.
func Store(r Registry) Common {
	return optionFunc(func(c *config) { c.store = r })
}

// optionError states the class this driver has an opinion about and keeps
// [ErrOption] reachable underneath it.
func optionError(msg string) error {
	return fmt.Errorf("%w: %w: %s", ferry.ErrPlane, ErrOption, msg)
}
