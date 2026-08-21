package ferry

import (
	"fmt"
	"strings"
)

// Option is a setting a caller hands to [Load], [LoadOver], [Dump], [Compile],
// [Bind] or [BindSink]. There are four: [TagKey], [WithRegistry],
// [MaxConcurrency] and [RootRequired].
//
// The set is closed, because the interface's one method is unexported. A
// library built on ferry can therefore change where ferry reads its annotation
// from, and cannot change what the annotation means.
//
// Every one of them is refused when supplied twice in one call.
//
// [TagKey], [WithRegistry] and [RootRequired] change what a type compiles to, so
// all three are part of the key a compiled schema is cached under.
// [MaxConcurrency] changes only how a load is run, so it is not in that key and
// two loads of one type under two budgets still compile once.
type Option interface {
	apply(*config) error
}

// optionFunc is the one implementation, so an Option is a closure over what the
// caller supplied rather than a type per setting.
type optionFunc func(*config) error

func (f optionFunc) apply(c *config) error { return f(c) }

// config is the resolved Option set one call runs under.
//
// It used to hold compile-affecting settings alone, in ADR-0010's sense - one
// reflect.Type yields two different schemas under two values of it - because
// that is what a schema cache is keyed by. ADR-0019's MaxConcurrency is the
// first load-affecting member, and it is here rather than beside here so that
// there is one resolved Option set and one refusal path for a bad one.
//
// The cache key is what keeps the two apart, and the rule is stated at
// [schemaKey]: a compile-affecting Option is in the key and a load-affecting one
// must not be. So a load-affecting field is read from the caller's own resolved
// config and never off a cache entry, whose config is whichever one won the race
// for the slot.
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

	// budget is how many calls into one open plane a load may overlap, and
	// budgetSet is what makes a second MaxConcurrency a refusal. It is the one
	// load-affecting member, so it is deliberately absent from [schemaKey]
	// (ADR-0019).
	budget    int
	budgetSet bool

	// root is what the Option list declared about the root address, which is
	// the one address the grammar cannot reach because a declaration is written
	// on a tag and the root has none (ADR-0008). It is compile-affecting in
	// ADR-0010's sense - one reflect.Type yields two schemas under two values of
	// it - so it is in [schemaKey].
	root rootDecl

	// watchAsked is what the Option list declared about watching, and it is read
	// only by the typed watch prototype: the default build has no Option that
	// sets it. It is neither compile-affecting nor load-affecting, so it is
	// absent from [schemaKey] and from every load.
	watchAsked    bool
	watchAskedSet bool
}

// rootDecl is the declaration the root address carries, in the shape a tag
// would have carried it in below the root.
//
// required is the only member, and there is no declared default beside it: a
// default is text a tag spells and the root has no tag, so the seed [LoadOver]
// takes is the root's default instead (ADR-0006). requiredSet is what makes a
// second [RootRequired] a refusal rather than a silent idempotent one, which is
// the rule every other Option already obeys.
type rootDecl struct{ required, requiredSet bool }

// defaultTagKey is the key ferry reads when nobody says otherwise.
const defaultTagKey = "ferry"

// newConfig resolves an Option list, reporting every Option that was wrong
// rather than the first one.
//
// A nil member is one of the ones that were wrong, and not a panic. Core
// already refuses a nil Source, a nil Sink and a nil registry with a sentence
// apiece, and an Option list built by appending whatever a helper returned is
// the ordinary way a nil arrives in one.
func newConfig(opts []Option) (config, error) {
	c := config{tagKey: defaultTagKey, registry: builtins, budget: serialBudget}

	errs := make([]error, 0, len(opts))

	for i, o := range opts {
		if o == nil {
			errs = append(errs, optionError(nilOptionMsg(i)))

			continue
		}

		errs = append(errs, o.apply(&c))
	}

	// Asked once the whole list is resolved, because it is a question about two
	// settings at once: the key ferry reads is this call's, and the keys it also
	// reads are the registry's (ADR-0021).
	errs = append(errs, c.registry.exts.claims(c.tagKey))

	return c, join(errs...)
}

// nilOptionMsg names the position, because an Option is opaque and a list of
// three has nothing else to tell one member from another.
func nilOptionMsg(i int) string {
	return fmt.Sprintf("the Option at position %d is nil: an Option is built by ferry.TagKey, "+
		"ferry.WithRegistry or ferry.MaxConcurrency, so a nil one is a helper that returned nothing rather "+
		"than a setting - drop it, or return the Option it was meant to build", i)
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
//	reg := ferry.NewRegistry(ferry.StringText[netip.Addr]())
//
//	cfg, err := ferry.Load[Config](ctx, src, ferry.WithRegistry(reg))
//
// It is what lets two tests in one process want different codecs for one type,
// and what a library uses to keep its own codecs out of its consumer's calls.
//
// The registry it names is complete before this call sees it, because
// [NewRegistry] takes the whole codec set and there are no mutators, so naming
// one here changes nothing about it and there is no ordering rule to keep.
//
// It is refused when supplied twice, and a nil registry is refused rather than
// read as the default: core's own type set with no codec over it is spelled
// ferry.NewRegistry(), and omitting the Option is how it is asked for.
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

// MaxConcurrency allows a load to overlap up to n calls into the plane, where
// today it makes exactly one at a time.
//
//	cfg, err := ferry.Load[Config](ctx, consul.New(client), ferry.MaxConcurrency(8))
//
// It is a ceiling and never a target. Overlap happens only where the driver's
// open instance also declared it tolerates overlap, by implementing
// [Concurrent], and never past the smaller of the two numbers. A source that
// does not implement it - a file, a process environment - is walked serially
// whatever n says, so setting this changes nothing about a driver that did not
// offer it.
//
// The same n reaches the driver behind its own open, where it reads it with
// [ConcurrencyBudget], so a driver that batches its own requests sizes the batch
// inside the budget instead of beside it. One number is spent once, however many
// layers spend it.
//
// n must be at least 1, and 1 is legal and means serial. It changes only how a
// load is run and never what a type compiles to, so it is not part of the schema
// cache key and two loads under two budgets compile once. It is refused when
// supplied twice.
//
// Reading it back out is not the point of it, and there are three sharp edges.
//
// A concurrent load reports exactly what a serial one reports: the members of a
// container are combined in the order the container lists them and never in the
// order they finished, so the destination and the failure are the same value and
// the same text either way.
//
// Fanout covers the members a type names - a struct's fields, an array's
// elements, what is under a pointer. The members a plane names, under a slice or
// a map field, are walked in order. So a wide struct of leaves overlaps and a
// five-hundred-key map does not.
//
// A panic ferry itself raises inside an overlapped member ends the process
// rather than unwinding into the caller, because it is raised on a goroutine the
// caller does not own. A panic out of your own codec is unaffected: it is
// recovered at the call and arrives in the report at the address that produced
// it, whether or not the walk was overlapped.
func MaxConcurrency(n int) Option {
	return optionFunc(func(c *config) error {
		switch {
		case n < serialBudget:
			return optionError(fmt.Sprintf(
				"MaxConcurrency was given %d: a budget is a count of overlapping calls into the plane, so the "+
					"smallest one is 1, which is the serial walk ferry does when nobody asks", n))
		case c.budgetSet:
			return optionError(fmt.Sprintf(
				"the concurrency budget is given twice, as %d and %d: ferry spends exactly one, because two "+
					"ceilings let a caller who asked for the smaller one get the larger", c.budget, n))
		default:
			c.budget, c.budgetSet = n, true

			return nil
		}
	})
}

// RootRequired declares the root address required, which is the one address no
// struct tag can name.
//
//	port, err := ferry.Load[int](ctx, src, ferry.RootRequired)
//
// A load fails with [ErrMissing] where the plane holds nothing at the root.
// Where the root is a leaf that is the presence test required always is,
// satisfied by any observation the plane makes there and by no other thing,
// including an explicit empty text and a null. Where the root is a struct it
// means the plane supplied at least one of that struct's own children, which is
// what required means at every other section address.
//
// It is a presence test about the plane, so a seed does not answer it: sharp
// edge, ferry.LoadOver(ctx, 8080, src, ferry.RootRequired) still fails where the
// plane went silent, which is what a reload wants. [Dump] accepts it and writes
// what it was given, because requiredness is a question only a load asks.
//
// It changes what a type compiles to, so it is part of the schema cache key, and
// it is refused when supplied twice in one call.
var RootRequired rootRequired

// rootRequired is a type with exactly one inhabitant, which is what makes the
// exported var immutable without a function around it: the only value
// assignable to it is the value it already holds, and the compiler says so. An
// Option-typed var would be a process-wide global any init in the binary could
// repoint (ADR-0010).
type rootRequired struct{}

func (rootRequired) apply(c *config) error {
	if c.root.requiredSet {
		return optionError("ferry.RootRequired was supplied twice in one call")
	}

	c.root.required, c.root.requiredSet = true, true

	return nil
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
