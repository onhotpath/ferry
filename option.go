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
//
// # Compile-affecting and load-affecting
//
// A compiled schema is cached and reused, so which Options a caller supplied is
// part of what identifies it. The rule for that is one sentence, and it is
// stated here rather than beside the cache because it is a property of an
// Option rather than of the map:
//
//	An Option is compile-affecting if one reflect.Type yields two different
//	schemas under two values of it. A compile-affecting Option is part of the
//	cache key, and its value must be comparable. An Option that is not
//	compile-affecting must not be in the key.
//
// [TagKey] is compile-affecting: one struct under two keys is two different
// address sets, which is what ADR-0008 measured. [WithRegistry] is
// compile-affecting: two registries that disagree about one member type give
// that type two representations and the struct two schemas, which is what
// ADR-0009 measured, and a cache that ignored it would hand one registry the
// other's codec silently.
//
// Both of ferry's Options are on that side today, and that is the order the
// tickets landed in rather than a property of the design. The other side is a
// real class: a load-affecting Option changes what one load does and not what
// the type compiles to, so it must stay out of the key or two callers who
// differ only in it would be handed two identical schemas under two keys. The
// worked example is ADR-0006's presence observation - an Option handing the
// caller which addresses the plane actually answered - which is named here as
// the other side of the rule and is not proposed: where that is spelled belongs
// with the caller-facing lifecycle, and is not decided.
//
// The rule has a mechanism rather than only a paragraph. The compile-affecting
// Options are collected into a named key struct, and a static assertion that a
// plain map can hold it turns an Option whose value is not comparable into a
// build failure rather than the run-time panic a sync.Map's `any` key would
// give.
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
	c := config{tagKey: defaultTagKey, registry: defaultRegistry}

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

// WithRegistry names the codec registry this call resolves types against,
// instead of the one core ships.
//
//	reg := ferry.NewRegistry()
//	if err := reg.Register(ferry.TextCodec[netip.Addr](ferry.KindString)); err != nil { ... }
//
//	cfg, err := ferry.Load[Config](ctx, src, ferry.WithRegistry(reg))
//
// It is the escape hatch that makes a default registry affordable at all. A
// global table would leave two tests unable to want different codecs for one
// type in one process, and that is not a hypothetical: choosing between two
// representations for a type is exactly what a registrant does before shipping
// one (ADR-0009).
//
// The registry it names freezes at this call if the call retains its schema,
// which [Load], [LoadOver] and [Dump] do and [Compile] does not.
//
// ferry resolves against exactly one registry, and supplying this twice is a
// refusal on the same argument [TagKey] is: two tables that disagree about one
// type give two representations, and nothing in the call says which is meant. A
// nil registry is refused rather than read as the default, because "no
// registrations" is spelled [NewRegistry] and a nil that quietly meant
// something would make the two indistinguishable at the call site.
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
