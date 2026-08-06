package ferry

import (
	"fmt"
	"strings"
)

// Option is a setting a caller hands to [Load], [LoadOver], [Dump], [Compile],
// [Bind] or [BindSink]. There are two: [TagKey] and [WithRegistry].
//
// The set is closed, because the interface's one method is unexported. A
// library built on ferry can therefore change where ferry reads its annotation
// from, and cannot change what the annotation means.
//
// Both of today's Options change what a type compiles to, so both are part of
// the key a compiled schema is cached under, and both are refused when supplied
// twice in one call.
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

	// registry is the codec table this compile resolves against, which defaults
	// to the one core ships. It belongs here rather than beside here because it
	// is compile-affecting in exactly ADR-0010's sense: one reflect.Type yields
	// two different schemas, and two different address sets, under two registries
	// that disagree about one member type (ADR-0009).
	registry    *Registry
	registrySet bool
}

// defaultTagKey is the key ferry reads when nobody says otherwise.
const defaultTagKey = "ferry"

// newConfig resolves an Option list, reporting every Option that was wrong
// rather than the first one.
func newConfig(opts []Option) (config, error) {
	c := config{tagKey: defaultTagKey, registry: builtins}

	errs := make([]error, 0, len(opts))
	for _, o := range opts {
		errs = append(errs, o.apply(&c))
	}

	return c, join(errs...)
}

// TagKey names the struct tag key ferry reads, which defaults to "ferry".
//
//	cfg, err := ferry.Load[Config](ctx, src, ferry.TagKey("mylib"))
//
// It exists for a library built on ferry, whose users should be writing that
// library's tag rather than ferry's.
//
// It names where to look and never what the content means. Under whatever key
// it is told to read, ferry reads ferry's own grammar and holds it to ferry's
// own strictness, so mylib:"host,retry=3" is still a schema compile error. That
// is the sharp edge: pointing ferry at a key another mapper already uses does
// not make that mapper's options legal, and json:"name,omitempty" refuses.
//
// It applies to every struct reached by the call it is handed to, not only to
// the top-level one, and it is refused when supplied twice: two keys on one
// field would be two address sets with nothing to choose between them.
//
// A key that could never be written into a struct tag at all - one holding a
// space, a quote or a colon - is refused here, at the call the Option was given
// to, rather than at schema compile.
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

// WithRegistry names the codec registry this call resolves types against,
// instead of the one core ships.
//
//	reg := ferry.NewRegistry()
//	if err := reg.Register(ferry.TextCodec[netip.Addr](ferry.KindString)); err != nil { ... }
//
//	cfg, err := ferry.Load[Config](ctx, src, ferry.WithRegistry(reg))
//
// It is what lets two tests in one process want different codecs for one type,
// and what a library uses to keep its own codecs out of its consumer's default
// registry.
//
// The registry it names freezes at this call if the call retains its schema,
// which [Load], [LoadOver], [Dump], [Bind] and [BindSink] do and [Compile] does
// not.
//
// It is refused when supplied twice, and a nil registry is refused rather than
// read as the default: an empty registry is spelled [NewRegistry], and omitting
// the Option is how the default one is asked for.
func WithRegistry(reg *Registry) Option {
	return optionFunc(func(c *config) error {
		switch {
		case reg == nil:
			return optionError("WithRegistry was given a nil registry: ferry.NewRegistry() builds an empty one, " +
				"and omitting the Option resolves against the registry core ships")
		case c.registrySet:
			return optionError("the registry is given twice: ferry resolves against exactly one, because two " +
				"tables that disagree about one type are two representations and nothing here chooses between them")
		default:
			c.registry, c.registrySet = reg, true

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
