package env

import (
	"cmp"
	"errors"
	"fmt"
	"os"

	"github.com/onhotpath/ferry"
)

// ErrOption reports a driver option this source or sink cannot be built with: a
// separator no environment variable name can hold, a [Form] outside the two this
// package declares, or no environment to read.
//
// [New] and [NewDotEnvSink] take options and return no error, so this lands at
// Bind, which is the first moment the driver is asked for anything. It wraps
// [ferry.ErrPlane] and stays reachable under ferry's wrapper, so errors.Is
// answers for it on what [ferry.Load] and [ferry.Dump] returned.
var ErrOption = errors.New("env: unusable driver option")

// Option is a setting handed to [New].
//
// Seven of them: [Separator], [RootVar] and [BoolWords], which are also
// [SinkOption]s, and [Canonical], [Environ], [DotEnv] and [WatchFiles], which
// belong to the read half alone.
type Option interface {
	apply(*config)
}

// SinkOption is a setting handed to [NewDotEnvSink].
//
// Five of them: [Separator], [RootVar] and [BoolWords], which are also
// [Option]s, and [Setenv] and [Durable], which belong to the write half alone.
//
// It is a separate type from [Option] so that each constructor takes the
// settings that mean something to it, and the other way round is a compile error
// rather than a setting that is quietly ignored.
type SinkOption interface {
	applySink(*sinkConfig)
}

// Naming is a setting both halves take, and it is the three that decide what a
// name is: [Separator], [RootVar] and [BoolWords].
//
// They are shared because a sink writing one spelling and a source reading
// another is a plane that cannot round trip. A sink joining with "_" writes
// TAGS_0 where a source joining with "__" looks for TAGS__0, and words given to
// one half alone make the plane write true and read on. Source and sink are two
// constructors, so nothing checks that the two agree; the failure is a load that
// finds nothing rather than a value silently corrupted, and the way to avoid it
// is to build both halves from one slice of these.
type Naming interface {
	Option
	SinkOption
}

// optionFunc is the implementation behind every setting both halves take, which
// is what makes each of those a one-line constructor.
type optionFunc func(*config)

func (f optionFunc) apply(c *config)         { f(c) }
func (f optionFunc) applySink(c *sinkConfig) { f(&c.config) }

// sourceOnly is the implementation behind a setting only [New] takes, and the
// whole of what stops it being handed to [NewDotEnvSink].
type sourceOnly func(*config)

func (f sourceOnly) apply(c *config) { f(c) }

// config is a [Source]'s settled configuration, copied into every binding so
// that a Source reconfigured after Bind cannot change a binding already handed
// out.
type config struct {
	sep   string
	canon Form
	// rootVar is the variable the root address is read from, and it is empty
	// until [RootVar] names one. The root carries no segment, so there is
	// nothing to fold a name out of and this is the only name it can have
	// (ADR-0003, #337).
	rootVar string
	environ func() []string
	// dotenv is the files layered under the process environment, lowest first,
	// and it is nil until [DotEnv] names any. Naming them does no I/O: a Bind
	// records the paths and the open reads them (ADR-0012).
	dotenv []string
	// watch is the file watcher [WatchFiles] asked for, or nil. It is started
	// inside [New], on the caller's own goroutine, so that a watcher that cannot
	// be built has somewhere to report it (ADR-0020).
	watch *watcher
	// bools is the spelling [BoolWords] built, and it is nil until it is asked
	// for: this plane holds text and nothing else, so a variable is a String
	// unless a word of this plane's own says it is a bool (ADR-0018).
	bools ferry.Spelling[bool, string]
	// wordsErr is what building the configuration refused with, held until Bind
	// for the reason [ErrOption] gives: an Option is applied inside [New], which
	// returns no error, so the refusal waits for the first moment the driver is
	// asked for anything.
	wordsErr error
}

// DefaultSeparator is the join a [Source] and a [DotEnvSink] use when no
// [Separator] is given.
//
// It is the spelling every operator already reads, and it is deliberately not
// the safest one: at "_" the fields metric.http.port and metric.http_port want
// one name, and a schema naming both is refused at Bind. A wider "__" buys a
// bigger margin and never a guarantee, since a field name may itself contain
// "__", so the default is the readable one and the check at Bind is what makes
// either safe.
const DefaultSeparator = "_"

// DefaultDotEnvFile is the file [DotEnv] reads when it is named none.
const DefaultDotEnvFile = ".env"

// defaults is the configuration a [Source] starts from.
func defaults() config {
	return config{sep: DefaultSeparator, canon: Lower, environ: os.Environ}
}

// Separator sets the string nested fields are joined with.
//
//	src := env.New(env.Separator("__")) // db.host reads DB__HOST
//
// It is the way out when two fields want one variable name and neither can be
// renamed: at "__" the fields db.host and db_host stay apart. No separator is
// safe for every schema, because a field name may contain the separator itself,
// so whatever is chosen, two fields the join would collapse are still refused at
// Bind before any I/O.
//
// It must be a non-empty run of the bytes an environment variable name may
// hold: A-Z, 0-9 and _.
//
// Give the same one to both halves. A sink writing TAGS_0 and a source reading
// TAGS__0 never meet, and nothing checks that the two agree.
func Separator(sep string) Naming {
	return optionFunc(func(c *config) { c.sep = sep })
}

// Form is the case a map key comes back in, and the set is closed at two.
type Form uint8

const (
	// Lower spells a map key in lower case, and it is the default. Map keys are
	// ordinarily lower case and environment variable names are ordinarily upper
	// case, so this is the choice under which the common key comes back
	// unchanged.
	Lower Form = iota

	// Upper spells a map key in upper case, which is what a configuration whose
	// map keys are themselves environment variable names wants.
	Upper
)

// Canonical chooses the case a map key comes back in.
//
//	src := env.New(env.Canonical(env.Upper)) // LIMITS_HTTP fills the key "HTTP"
//
// A map's keys come from the environment rather than from the struct, and the
// name was upper-cased on the way in with no way to know which case it started
// as. So LIMITS_HTTP fills a map[string]string under the key http by default,
// and under HTTP with [Upper]. Tagged fields are unaffected: their names are in
// the struct, so their own spelling is recovered exactly.
//
// A key comes back unchanged when it is already in the chosen case and holds no
// byte an environment variable name cannot hold. Otherwise it comes back changed
// either way, because there is no way back from a fold that has already
// happened. At [Lower] and the default separator, for a map at limits:
//
//	key "http"       ->  LIMITS_HTTP       ->  "http"   unchanged
//	key "HTTP"       ->  LIMITS_HTTP       ->  "http"   changed by the case fold
//	key "http-port"  ->  LIMITS_HTTP_PORT  ->  "http"   changed by the "-"
//	key "http_port"  ->  LIMITS_HTTP_PORT  ->  "http"   changed by the join
//
// The last row is the [Separator] and not this option: LIMITS_HTTP_PORT is also
// what the nested limits.http.port renders to, and it is read as the nesting,
// because a driver reading it the other way could not load a map of maps at all.
// Use env.Separator("__") if your keys contain underscores.
//
// It is read-side only. Nothing is folded on the way out, because a key written
// to a file is written as the struct spells it.
func Canonical(f Form) Option {
	return sourceOnly(func(c *config) { c.canon = f })
}

// Environ names the function a load reads its environment from, in the
// "KEY=value" form [os.Environ] returns. It defaults to [os.Environ].
//
//	src := env.New(env.Environ(func() []string { return []string{"NAME=checkout"} }))
//
// It is what makes a test hermetic, since testing.T.Setenv forbids t.Parallel
// and mutating the process environment is visible to everything else in the
// binary.
//
// It is also the escape hatch from every sharp edge the process half of this
// plane has. Whatever it returns is the top layer, above every file [DotEnv]
// named, so env.Environ(func() []string { return nil }) is how a load reads the
// files alone and nothing ambient can shadow, invent or collide with what they
// hold.
//
// It is called once per load, so one load sees one consistent snapshot and a
// later change to the environment reaches the next load rather than half of this
// one.
//
// Making it safe to call from many goroutines at once is the caller's, and it is
// ordinary rather than exotic: a [ferry.Binding] is held for the life of a
// process and loaded through from wherever, so this function is entered
// concurrently as a matter of course. [os.Environ] is safe. A closure over a map
// or a slice that anything else writes to is not, and nothing here can make it
// so.
func Environ(fn func() []string) Option {
	return sourceOnly(func(c *config) { c.environ = fn })
}

// DotEnv layers .env files underneath the process environment.
//
//	src := env.New(env.DotEnv())                            // .env, then the process
//	src := env.New(env.DotEnv("base.env", "local.env"))     // base < local < the process
//
// With no paths it reads [DefaultDotEnvFile]. Named several, a later file wins
// over an earlier one, and the process environment wins over every file: the
// process is the anchor, and the files are what fills in what it does not say.
//
// A file that is not there is an empty layer rather than a failure, which is what
// makes the files optional: every field takes its default, and a required field
// fails. A file that is there and does not parse is a refusal, so a .env with a
// typo in it is never loaded as though it were empty.
//
// It does no I/O of its own. The paths are recorded, and each load reads, parses
// and layers them, so a file edited between two loads reaches the second one.
//
// The sharp edge is the one the package documentation opens with: the process
// wins, so a variable already exported shadows the file that names it, and a
// [DotEnvSink] save that does not also set the process leaves the next load
// reading the old value. See [Setenv] and [Environ].
func DotEnv(paths ...string) Option {
	return sourceOnly(func(c *config) {
		if len(paths) == 0 {
			paths = []string{DefaultDotEnvFile}
		}

		c.dotenv = paths
	})
}

// RootVar names the environment variable a schema whose root is a single value
// is read from and written to.
//
//	port, err := ferry.Load[int](ctx, env.New(env.RootVar("APP_PORT")))
//
// Without it such a schema is refused at Bind, before any environment is read.
// The root address carries no segment, so the join that names every other
// address has nothing to join and this driver has no name to use.
//
// The name is taken as written rather than folded, and that is the whole of the
// sharp edge: the fold turns every byte outside A-Z, 0-9 and _ into _, so no
// segment could ever produce the name of a root leaf and naming it here is the
// only route to one. A name beginning with a digit is refused at Bind like any
// other, because no shell will set it.
//
// It names one variable and nothing else. Every address with a segment of its
// own is named by that segment as before, and a root whose variable is not set
// is absent, which leaves whatever [ferry.LoadOver] was seeded with in place.
//
// Give the same one to both halves, for the reason [Naming] states.
func RootVar(name string) Naming {
	return optionFunc(func(c *config) { c.rootVar = name })
}

// validate refuses a configuration this driver cannot serve, and it runs at Bind
// because that is the first moment the driver is asked for anything.
func (c *config) validate() error {
	if err := c.validateNaming(); err != nil {
		return err
	}

	if c.canon != Lower && c.canon != Upper {
		return optionError("the canonical form must be env.Lower or env.Upper")
	}

	if c.environ == nil {
		return optionError("there is no environment to read: env.Environ was given no function")
	}

	return nil
}

// validateNaming is the half of the check both directions make, because both
// fold an address into a name and both spell a boolean.
func (c *config) validateNaming() error {
	if !legalSeparator(c.sep) {
		return optionError("the separator must be a non-empty run of A-Z, 0-9 and _, " +
			"spelled as it appears in the name")
	}

	return c.wordsErr
}

// refuse records a refusal an option constructor made, keeping the first one so
// that a configuration with two mistakes in it reports the one nearest the
// beginning rather than the one nearest the end.
func (c *config) refuse(err error) { c.wordsErr = cmp.Or(c.wordsErr, err) }

// legalSeparator reports whether every byte of the separator is one an
// environment variable name may hold. A separator that is not is a name no
// operator can set, which is the whole reason this driver transforms.
func legalSeparator(sep string) bool {
	if sep == "" {
		return false
	}

	for i := range len(sep) {
		if !nameByte(sep[i]) {
			return false
		}
	}

	return true
}

// optionError states the class this driver has an opinion about and keeps
// [ErrOption] reachable underneath it.
func optionError(msg string) error {
	return fmt.Errorf("%w: %w: %s", ferry.ErrPlane, ErrOption, msg)
}
