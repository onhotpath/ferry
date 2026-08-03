package ferry

import (
	"fmt"
	"strings"
)

// Option is a setting a caller hands to [Compile], and to every verb that
// compiles a schema on its way to a plane.
//
// The set is closed, because the interface's one method is unexported. Every
// Option is one ferry decided on, so a library built on ferry cannot introduce
// a second authority over what an annotation means: ADR-0008 opens the tag
// *key* and keeps the *vocabulary* shut, and this is where that line is drawn
// in the type system rather than in prose.
type Option interface {
	apply(*config) error
}

// optionFunc is the one implementation, so an Option is a closure over what the
// caller supplied rather than a type per setting.
type optionFunc func(*config) error

func (f optionFunc) apply(c *config) error { return f(c) }

// config is the resolved Option set one compile runs under.
//
// Everything in it is compile-affecting in ADR-0010's sense - one reflect.Type
// yields two different schemas under two values of it - which is what makes it
// the thing a schema cache is keyed by. A load-affecting Option would not
// belong here.
type config struct {
	// tagKey is the struct tag key the compiler reads, and tagKeySet is what
	// makes a second TagKey a refusal rather than a silent last-wins.
	tagKey    string
	tagKeySet bool
}

// defaultTagKey is the key ferry reads when nobody says otherwise.
const defaultTagKey = "ferry"

// newConfig resolves an Option list, reporting every Option that was wrong
// rather than the first one.
func newConfig(opts []Option) (config, error) {
	c := config{tagKey: defaultTagKey}

	errs := make([]error, 0, len(opts))
	for _, o := range opts {
		errs = append(errs, o.apply(&c))
	}

	return c, join(errs...)
}

// TagKey names the struct tag key ferry reads, which defaults to "ferry".
//
// It names where to look and never what the content means. Whatever key ferry
// is told to read, it reads ferry's grammar under it and holds it to ferry's
// strictness, so TagKey("mylib") changes nothing about what mylib:"host,retry=3"
// is: a schema compile error, and correctly so. The case it exists for is a
// library built on ferry, whose users should be writing that library's tag
// rather than ferry's (ADR-0008).
//
// ferry reads exactly one key, and supplying this twice is a refusal. A list is
// a precedence question wearing a convenience costume: two keys on one field
// give two address sets, and nothing in the tag says which is meant.
//
// The key is checked here, where the Option is supplied, rather than at schema
// compile. A key that could never appear in a conventional struct tag is a
// mistake in the program that wrote it and not in the struct being compiled,
// and the error arrives at whichever call the Option was handed to.
func TagKey(key string) Option {
	// Checked eagerly, so that the refusal is about this call and not about
	// the type some later Compile happens to name.
	err := checkTagKey(key)

	return optionFunc(func(c *config) error {
		switch {
		case err != nil:
			return err
		case c.tagKeySet:
			return optionError(fmt.Sprintf(
				"the tag key is given twice, as %q and %q: ferry reads exactly one key, "+
					"because a list of keys is a precedence ladder nothing in the tag chooses between",
				c.tagKey, key))
		default:
			c.tagKey, c.tagKeySet = key, true

			return nil
		}
	})
}

// checkTagKey holds a supplied key to what a struct tag key can be. The
// conventional form is key:"value" pairs separated by spaces, so a key carrying
// a space, a quote or a colon could never be written into one at all.
func checkTagKey(key string) error {
	if key == "" {
		return optionError("the tag key may not be empty")
	}

	if strings.ContainsRune(key, comma) {
		return optionError(fmt.Sprintf(
			"the tag key %q contains a comma, which reads as a list of keys: ferry reads exactly one", key))
	}

	if i := badTagKeyByte(key); i >= 0 {
		return optionError(fmt.Sprintf(
			"the tag key %q contains %q, which cannot appear in a struct tag key", key, key[i:i+1]))
	}

	return nil
}

// badTagKeyByte is where a key stops being writable as a struct tag key, or -1.
// The set is the one reflect's own scanner stops at.
func badTagKeyByte(key string) int {
	for i := range len(key) {
		if c := key[i]; c <= ' ' || c == ':' || c == '"' || c == del {
			return i
		}
	}

	return -1
}

// del is the one byte above space that a struct tag key may not contain.
const del = 0x7f

// optionError is a refusal about the call site rather than about a type.
//
// It carries no location, because there is no address and no field to name: the
// mistake is in the Option list. It is a compile-moment error because that is
// the call it fails, and a location-less element sorts to the head of its
// moment, which is where a report of "your Options are wrong" belongs.
func optionError(msg string) error {
	return newError(momentCompile, ErrSchema, Path{}, msg)
}
