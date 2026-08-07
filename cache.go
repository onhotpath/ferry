package ferry

import (
	"reflect"
	"sync"
)

// This file is the schema cache: a compiled schema is computed once per
// configuration and reused, and the rule for what may ever key it.
//
// The key's three components are the type, the struct tag key and the registry,
// and none of them was this file's to choose. ADR-0008 put the tag key in, and
// measured that one reflect.Type under two keys yields two different address
// sets. ADR-0009 put the registry in, and measured that a cache keyed by
// reflect.Type alone hands one registry another registry's codec, silently, so
// a service gets a representation it replaced with no error anywhere. What is
// decided here is the rule for the fourth component, which will arrive from an
// ADR that has not seen either of the first two:
//
//	An Option is compile-affecting if one reflect.Type yields two different
//	schemas under two values of it. A compile-affecting Option is part of the
//	cache key, and its value must be comparable. An Option that is not
//	compile-affecting must not be in the key.
//
// The rule is stated for callers on [Option], because it is the thing a reader
// of that type needs in order to know why TagKey is in the key and a load-time
// setting would not be. The mechanism is below.

// schemaKey is the inner key of one registry's cache: the type, the struct tag
// key the compiler read it under, and the foreign tag keys it was told to read
// beside that one.
//
// It is a named struct rather than an anonymous one so that adding a field to
// it is a deliberate act rather than a side effect of adding an Option. Every
// compile-affecting Option in [config] beside the registry is collected here,
// and the registry is not a member because it is the outer level: the cache
// hangs off the *[Registry] (ADR-0010).
//
// The declaration is a member on the same rule as the tag key, and it is
// carried in its canonical order-independent form, so two registries declaring
// the same extensions in opposite orders key one schema rather than two
// (ADR-0021).
type schemaKey struct {
	typ    reflect.Type
	tagKey string
	decl   extDecl
}

// This is the whole mechanism behind the rule above, and it is the only one Go
// offers: a plain map refuses an unhashable key type at build time -
//
//	invalid map key type schemaKey
//
// - where the sync.Map the cache actually uses takes an `any` key and can do no
// better than the run-time panic ADR-0006 measured, `hash of unhashable type`.
// So a compile-affecting Option whose value is not comparable stops this
// package compiling, at the line that added it, rather than panicking inside
// somebody's Load. internal/testdata/schemakey/unhashable is the fixture that
// asserts it, because a rule enforced by the compiler has no run-time behaviour
// to observe. It is also what holds an extension declaration to being reducible
// to a comparable value: [extDecl] is in the key, so a declaration that could
// not be canonicalised would stop this line compiling (ADR-0021).
var _ = map[schemaKey]struct{}{}

// cacheEntry is one key's place in the cache, and the second of the two levels.
//
// It holds a func rather than a *schema because what is stored on a miss has to
// be cheap: an entry is built before the race for the map slot is run, and the
// entry that loses is discarded before its once ever fires. That is
// encoding/json/v2's two-level pattern rather than v1's, whose single level
// does the expensive work first and throws it away on a loss (ADR-0010).
type cacheEntry struct {
	// once is a sync.OnceValues over the compile. It is not here for speed, and
	// measured it costs rather than saves on a steady-state hit. What it buys is
	// exactly-once initialisation and identical replay, and the replay is the
	// half that matters: ferry's compile returns errors, and without it a later
	// caller receives a zero schema - an empty address set, and therefore a Load
	// that reads nothing and returns nil.
	once func() (*schema, error)
}

// newCacheEntry is the cheap half: a closure over the key and the resolved
// Option set, with nothing compiled yet.
//
// The config it closes over is sound to discard along with a losing entry,
// because everything in a config that a compile can see is in the key: the
// registry is the map this entry is going into, and the tag key is beside the
// type in it. Two entries racing for one slot would compile the same schema, so
// which one wins is not observable.
func newCacheEntry(k schemaKey, cfg config) *cacheEntry {
	return &cacheEntry{once: sync.OnceValues(func() (*schema, error) {
		return compileSchema(k.typ, cfg)
	})}
}

// schemaFor is the cached compile: Load on the outer map, a cheap entry built
// on a miss, LoadOrStore to settle the race, and the per-entry once.
//
// json/v1's recursive-type placeholder is deliberately not copied, and the
// reason is checked rather than assumed. A recursive type is refused at schema
// compile from the type alone (ADR-0005), and the cache is keyed per root type
// rather than per visited type, because a nested struct's addresses depend on
// the path from the root and its subschema is therefore not reusable. So a
// compile never performs a cache lookup, so it can never look up the type it is
// in the middle of compiling, so there is no cycle for a placeholder to break.
//
// Nothing is ever removed from the map. What an unbounded cache costs, and the
// one door onto it that is ferry's own rather than every surveyed library's, is
// documented on [Registry] where a caller will read it.
func (r *Registry) schemaFor(k schemaKey, cfg config) (*schema, error) {
	e, ok := r.schemas.Load(k)
	if !ok {
		e, _ = r.schemas.LoadOrStore(k, newCacheEntry(k, cfg))
	}

	// The map is this file's alone and holds nothing but a *cacheEntry, so the
	// comma is the type system's price for a sync.Map rather than a case that
	// can arise.
	entry, _ := e.(*cacheEntry)

	return entry.once()
}
