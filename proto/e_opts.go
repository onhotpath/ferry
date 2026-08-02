package main

// The Option surface #16 needs, and the one rule that governs it.
//
// ADR-0008 made the struct tag key a caller-supplied Option and measured that
// one reflect.Type under two keys yields two different address sets.
// ADR-0009 made the codec registry a value and measured that a schema cache
// keyed by reflect.Type alone hands one registry another registry's codec.
// So two independent ADRs each put something in this cache key, and neither
// knew about the other.
//
// ADR-0006 measured the bad end of the same question: `hash of unhashable
// type main.LoadOption`, because an option list is funcs.

import "reflect"

// --- the two kinds of Option ------------------------------------------------

// Option is the whole caller-facing option surface. Splitting it into two Go
// types was tried in E7 and it does not survive a variadic.
type Option interface{ apply(*opts) }

type opts struct {
	// compile-affecting: these change WHAT COMPILES, so they are in the key.
	tagKey string
	reg    *Registry
	// load-affecting: these change what one call does with a compiled schema,
	// so they are not.
	observe func(Path, Value)
	// sch is ADR-0010's scheduler seam, promoted from a hardcoded `serial` in
	// the entry point to a load-affecting Option. #14 needs it because
	// ADR-0011 puts aggregation here, and the required set is what a template
	// reads out of an aggregate. It is load-affecting by ADR-0010's own rule -
	// one compiled schema serves both schedulers - so it is not in the key,
	// which is what stops a func value reaching a map key.
	sch sched
}

func defaultOpts() opts { return opts{tagKey: "ferry", reg: defaultRegistry, sch: serial} }

type optFn func(*opts)

func (f optFn) apply(o *opts) { f(o) }

// TagKey is ADR-0008's Option. Compile-affecting.
func TagKey(k string) Option { return optFn(func(o *opts) { o.tagKey = k }) }

// WithRegistry is ADR-0009's. Compile-affecting.
func WithRegistry(r *Registry) Option { return optFn(func(o *opts) { o.reg = r }) }

// Observe is ADR-0006's presence observation. NOT compile-affecting: one
// compiled schema serves a call with it and a call without it.
func Observe(f func(Path, Value)) Option { return optFn(func(o *opts) { o.observe = f }) }

// WithSched fills ADR-0010's seam. Load-affecting, per the rule above.
func WithSched(s sched) Option { return optFn(func(o *opts) { o.sch = s }) }

// --- the cache key ----------------------------------------------------------

// schemaKey is the compile-affecting part of opts, and it is a named type so
// that adding a field to it is a deliberate act rather than a side effect of
// adding an Option.
//
// The registry is NOT a field here: it is the outer level, because ADR-0009
// measured a two-word struct key at 32 ns/op against 9 ns/op keyed by
// reflect.Type alone, and 10 ns/op with the per-type cache hung off the
// registry. E2 re-measures that on ferry's actual three-component key.
type schemaKey struct {
	typ    reflect.Type
	tagKey string
}

// The static assertion. A compile-affecting Option whose value is not
// comparable makes THIS LINE fail to build, with `invalid map key type`,
// rather than panicking at run time the way ADR-0006 measured. E2c is the
// measurement that this is a real difference and not a stylistic one.
var _ = map[schemaKey]struct{}{}

func (o opts) key(t reflect.Type) schemaKey { return schemaKey{typ: t, tagKey: o.tagKey} }
