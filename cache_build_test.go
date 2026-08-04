package ferry

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// TestAnUnhashableCacheKeyDoesNotCompile is the assertion behind ADR-0010's
// rule for what may key the schema cache.
//
// The rule is that a compile-affecting Option's value must be comparable, and a
// rule with no mechanism is prose. The mechanism is that the key is a named
// struct and that core asserts a plain map can hold it, which the compiler
// checks: there is no run-time behaviour to observe, so what is asserted is
// that a fixture carrying core's key plus one unhashable member fails to build,
// with the diagnostic Go gives for it.
//
// The exact wording of a compiler diagnostic is Go's rather than ferry's, so
// what is held is the phrase that names the fault and the type it names.
func TestAnUnhashableCacheKeyDoesNotCompile(t *testing.T) {
	t.Parallel()

	mustNotCompile(t, "./internal/testdata/schemakey/unhashable",
		[]string{"invalid map key type", "schemaKey"})
}

// TestASyncMapWouldOnlyPanicAtRunTime is the other half of the same assertion,
// and it is why the mechanism is a plain map rather than the map the cache
// actually uses.
//
// A sync.Map takes an `any` key, so it cannot refuse anything until it hashes
// one, and ADR-0006 measured what that costs: a panic, at run time, inside
// whatever call first reached the cache. The static assertion is what turns
// this into the build failure above, for free, and it is the only mechanism Go
// offers.
func TestASyncMapWouldOnlyPanicAtRunTime(t *testing.T) {
	t.Parallel()

	// The shape internal/testdata/schemakey/unhashable declares, reached through
	// a sync.Map instead of a plain one.
	type unhashableKey struct {
		tagKey  string
		observe func(addr string)
	}

	var m sync.Map

	got := func() (recovered string) {
		defer func() { recovered = fmt.Sprint(recover()) }()

		m.Load(unhashableKey{tagKey: defaultTagKey, observe: func(string) {}})

		return "no panic, and this key type cannot be hashed"
	}()

	if !strings.Contains(got, "hash of unhashable type") {
		t.Errorf("a sync.Map answered %q for an unhashable key, want the run-time hash panic", got)
	}
}
