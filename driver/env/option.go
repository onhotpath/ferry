package env

import (
	"errors"
	"fmt"
	"os"

	"github.com/onhotpath/ferry"
)

// ErrOption reports a driver option this source cannot be built with: a
// separator no environment variable name can hold, a [Form] outside the two
// this package declares, or no environment to read.
//
// [New] takes options and returns no error, so this lands at Bind, which is the
// first moment the driver is asked for anything. It wraps [ferry.ErrPlane] and
// stays reachable under ferry's wrapper, so errors.Is answers for it on what
// [ferry.Load] returned.
var ErrOption = errors.New("env: unusable driver option")

// Option configures a [Source]. The set is closed at four: [Separator],
// [Canonical], [Environ] and [BoolWords].
type Option interface {
	apply(*config)
}

// optionFunc is the one implementation, which is what makes every option below
// a one-line constructor.
type optionFunc func(*config)

func (f optionFunc) apply(c *config) { f(c) }

// config is a [Source]'s settled configuration, copied into every binding so
// that a Source reconfigured after Bind cannot change a binding already handed
// out.
type config struct {
	sep     string
	canon   Form
	environ func() []string
	// bools is the spelling [BoolWords] built, and it is nil until it is asked
	// for: this plane holds text and nothing else, so a variable is a String
	// unless a word of this plane's own says it is a bool (ADR-0018).
	bools ferry.Spelling[bool, string]
	// ungated selects the shipped, plane-wide reading of the words over the
	// prototyped kind-gated one, so that both paths are exhibitable from one
	// branch. It is not shipped surface (proto: #309).
	ungated bool
	// wordsErr is what building it refused with, held until Bind for the reason
	// [ErrOption] gives: an Option is applied inside [New], which returns no
	// error, so the refusal waits for the first moment the driver is asked for
	// anything.
	wordsErr error
}

// DefaultSeparator is the join a [Source] uses when no [Separator] is given.
//
// It is the spelling every operator already reads, and it is deliberately not
// the safest one: at "_" the fields metric.http.port and metric.http_port want
// one name, and a schema naming both is refused at Bind. A wider "__" buys a
// bigger margin and never a guarantee, since a field name may itself contain
// "__", so the default is the readable one and the check at Bind is what makes
// either safe.
const DefaultSeparator = "_"

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
func Separator(sep string) Option {
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
func Canonical(f Form) Option {
	return optionFunc(func(c *config) { c.canon = f })
}

// Environ names the function a load reads its environment from, in the
// "KEY=value" form [os.Environ] returns. It defaults to [os.Environ].
//
//	src := env.New(env.Environ(func() []string { return []string{"NAME=checkout"} }))
//
// It is what makes a test hermetic, since testing.T.Setenv forbids t.Parallel
// and mutating the process environment is visible to everything else in the
// binary. It is also how an environ captured elsewhere - a child process's, a
// .env file already parsed - is loaded through this driver.
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
	return optionFunc(func(c *config) { c.environ = fn })
}

// validate refuses a configuration this driver cannot serve, and it runs at Bind
// because that is the first moment the driver is asked for anything.
func (c config) validate() error {
	if !legalSeparator(c.sep) {
		return optionError("the separator must be a non-empty run of A-Z, 0-9 and _, " +
			"spelled as it appears in the name")
	}

	if c.canon != Lower && c.canon != Upper {
		return optionError("the canonical form must be env.Lower or env.Upper")
	}

	if c.environ == nil {
		return optionError("there is no environment to read: env.Environ was given no function")
	}

	return c.wordsErr
}

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
