// Package reassigned does not compile, and that is what it is for.
//
// ferry.RootRequired is a var rather than a constructor, and what keeps a var
// from being a mutable process-wide global any init in the binary could repoint
// is its type: rootRequired is a struct with exactly one inhabitant, and that
// inhabitant is unexported, so the only value assignable to the var is the value
// it already holds (ADR-0010). The rule is the compiler's, so it has no run-time
// behaviour to observe and needs a fixture the compiler rejects.
//
// It lives under testdata because the go command never matches a directory
// named testdata against ./... at any depth, so a package that cannot compile
// is never built, vetted or linted with the module while an explicit import
// path still resolves it; and the internal element above it means no importer
// outside ferry can reach it.
package reassigned

import "github.com/onhotpath/ferry"

// Reassign is the repointing an Option-typed var would have allowed.
func Reassign() {
	ferry.RootRequired = ferry.TagKey("x")
}
