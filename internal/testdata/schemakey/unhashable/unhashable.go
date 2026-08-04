// Package unhashable does not compile, and that is what it is for.
//
// ADR-0010's rule is that a compile-affecting Option is part of the schema
// cache key and that its value must be comparable, and the mechanism enforcing
// it is one line: the key is a named struct, and core asserts that a plain map
// can hold it. A plain map refuses an unhashable key type at build time, where
// the sync.Map the cache actually uses takes an `any` key and can do no better
// than a run-time panic inside somebody's Load.
//
// This package is core's own key struct with one member added, of the shape an
// Option carrying a callback would have. It is the assertion that the mechanism
// works, because a rule the compiler enforces has no run-time behaviour to
// observe it through.
//
// It lives under testdata because the go command never matches a directory
// named testdata against ./... at any depth, so a package that cannot compile
// is never built, vetted or linted with the module while an explicit import
// path still resolves it; and the internal element above it means no importer
// outside ferry can reach it.
package unhashable

import "reflect"

// observer is the shape of a load-affecting Option's value smuggled into the
// key: a func, which is one of Go's three unhashable kinds and the one an
// Option is most likely to carry.
type observer func(addr string)

// schemaKey is core's key struct with that member added.
type schemaKey struct {
	typ     reflect.Type
	tagKey  string
	observe observer
}

// The assertion core carries, at the key core would then have.
var _ = map[schemaKey]struct{}{}
