package env

import (
	"errors"
	"fmt"
	"os"

	"github.com/onhotpath/ferry"
)

// ErrOption reports a driver option this source cannot be built with: a
// separator no environment variable name can hold, a canonical form outside the
// closed set below, or no environment to read.
//
// It is a refusal about the driver's own configuration rather than about the
// schema, and it lands at Bind because that is the first moment the driver is
// asked for anything. It wraps [ferry.ErrPlane], which is the class for a driver
// refusing to serve the address set it was bound to, and it stays reachable
// under ferry's wrapper so that errors.Is answers for it through [ferry.Load].
var ErrOption = errors.New("env: unusable driver option")

// Option configures a [Source]. It is an interface with an unexported method,
// so the set of options is this package's and a caller cannot mint one that
// reaches inside.
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
}

// DefaultSeparator is the join a [Source] uses when no [Separator] is given.
//
// It is the spelling every operator already reads, and it is deliberately not
// the safest one: at "_" the addresses /metric/http/port and /metric/http_port
// render one name and the schema naming both is refused at Bind. The wider "__"
// buys a bigger margin and never a guarantee, because a segment may itself
// contain "__" (ADR-0003), so the default is the readable one and the check is
// what makes either safe.
const DefaultSeparator = "_"

// defaults is the configuration a [Source] starts from.
func defaults() config {
	return config{sep: DefaultSeparator, canon: Lower, environ: os.Environ}
}

// Separator sets the string the segments of an address are joined with.
//
// No separator is universally safe, which is why this is an option rather than
// a constant: segment text is the user's, so the only honest guarantee is one
// checked against the schema in hand (ADR-0003). Whatever is chosen, a set of
// addresses the join collapses is refused before any I/O, naming both.
//
// It must be a non-empty run of the bytes an environment variable name may
// hold, spelled as they appear in the name: A-Z, 0-9 and _.
func Separator(sep string) Option {
	return optionFunc(func(c *config) { c.sep = sep })
}

// Form is the canonical spelling this driver returns a dynamic segment in, and
// the set is closed at two.
type Form uint8

const (
	// Lower spells a dynamic segment in lower case, and it is the default.
	//
	// It is the inverse of the uppercase fold for the spellings people write in
	// a config value: a map key is ordinarily lower case, and an environment
	// variable name is ordinarily upper case, so lower is the choice under which
	// the common key round-trips unchanged.
	Lower Form = iota

	// Upper spells a dynamic segment in upper case, which is what a schema whose
	// map keys are themselves environment variable names wants.
	Upper
)

// Canonical names the form a dynamic segment comes back in, and it exists
// because this driver's key function is not invertible.
//
// The fold is many-to-one over segment text: /limits/http, /limits/HTTP and
// /limits/Http all render LIMITS_HTTP. Dump does not care, but Load does,
// because [Source] enumerates and Children has to hand back an address - and the
// fold has already destroyed which of the three it was. The static tier is
// unaffected, since a tagged field's address is in the compiled set and is
// recovered from it exactly; a map key's is not, so this option decides it.
//
// # It buys determinism and not totality
//
// No inverse round-trips every segment, because no inverse of a many-to-one map
// exists. What this option chooses is which subset does. The guarantee is
// therefore stated over canonical keys, and a key outside them comes back
// changed. At Lower and the default separator, over a map at /limits:
//
//	key "http"       ->  LIMITS_HTTP       ->  "http"   round-trips
//	key "HTTP"       ->  LIMITS_HTTP       ->  "http"   changed by the fold
//	key "http-port"  ->  LIMITS_HTTP_PORT  ->  "http"   changed by the transform
//	key "http_port"  ->  LIMITS_HTTP_PORT  ->  "http"   changed by the join
//
// A segment round-trips exactly when the fold leaves it alone: it is already in
// the chosen form, it holds no byte an environment variable name cannot hold,
// and it does not contain the [Separator]. The last of those is the join and not
// this option: LIMITS_HTTP_PORT is what a map key "http_port" renders to and
// also what a nested /limits/http/port renders to, and the enumeration resolves
// it as the nesting, because a driver that refused it could not load a map of
// maps at all. A wider separator moves the boundary and never removes it.
//
// Making that total means refusing a dynamic segment that is not already
// canonical, at the moment it is written. There is nothing here to refuse:
// this package ships no sink, so the refusal belongs to the first env-family
// sink - a .env file or an environ slice - and it is deferred to it.
func Canonical(f Form) Option {
	return optionFunc(func(c *config) { c.canon = f })
}

// Environ names the function a load reads its environment from, in the
// "KEY=value" form [os.Environ] returns. It defaults to [os.Environ].
//
// It is an option because reading the real process environment is a hazard in a
// test: testing.T.Setenv forbids t.Parallel, and a test that mutates the process
// environment is not hermetic. It is also what makes an environ captured
// elsewhere - a child process's, a .env file already parsed - loadable through
// the same driver.
//
// It is called once per open, so one load sees one consistent snapshot and a
// later change to the environment reaches the next load rather than half of this
// one.
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

	return nil
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
