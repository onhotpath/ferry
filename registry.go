package ferry

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
)

// This file is step zero of ADR-0007's chain: the codecs a program registers
// for types ferry does not own, held in a value rather than in a global table.
// The constructors that build them are in registration.go.
//
// ADR-0001 makes core's type set closed and its extension explicit, ADR-0009 is
// that sentence turned into an API, and ADR-0017 replaced its freeze mechanics
// with construction. Three properties are decisions rather than implementation,
// and each is argued where it is spelled: a registration is a pair by
// construction, so half a pair does not compile; construction is the freeze, so
// there is no window in which a registry is reachable and incomplete and no
// schema is ever resolved against one set of codecs and walked against another;
// and there is no decline and no registration by runtime reflect.Type, so a
// codec's claim is a property of the type alone and the address set stays
// computable with no value in hand.
//
// Everything ADR-0009 proved about why a registry must be long-lived, scoped
// rather than global, and part of the schema cache key is unchanged, and it is
// why this shape works at all.

// Registry is the set of codecs one program registers for types ferry does not
// own, over the set core already owns.
//
// Build one with [NewRegistry], name it for a call with [WithRegistry], and keep
// it. It is complete when it is built and there is nothing to add afterwards, so
// a package-level var or a per-test local is the whole idiom.
//
// A registry is a value to keep, because the compiled-schema cache hangs off
// it. Nothing is ever evicted from that cache, so a registry that stays alive
// keeps every schema ever compiled against it alive too, and a fresh registry
// per call means a full schema compile per call. Build one per program, or one
// per test.
//
// A nil *Registry reads as one holding no codec of its own, so [Registry.Types]
// can be asked about a program that registered nothing.
type Registry struct {
	// byType is written once, by [NewRegistry], before the value escapes, and is
	// never written again. That is what lets the read path take no lock and hold
	// no atomic: a registry has no phase in which it is both reachable and
	// mutable, so a resolution cannot race a registration and there is no freeze
	// to arrange (ADR-0017, #227, #262).
	byType map[reflect.Type]registration

	// exts is the foreign struct tag keys this registry was told to read beside
	// ferry's own, written once by [NewRegistry] before the value escapes for
	// the reason byType is (ADR-0021).
	//
	// It reaches the cache key through its own canonical form rather than
	// through this struct, which is what keeps [schemaKey] comparable.
	exts extSet

	// schemas is the schema cache, and it hangs off the registry because the
	// registry is the outer level of the cache key: two registries that disagree
	// about one type are two schemas for that type, and a cache they shared would
	// hand one of them the other's codec (ADR-0009, ADR-0010).
	//
	// It is keyed by [schemaKey] and holds *[cacheEntry] and nothing else.
	schemas sync.Map
}

// NewRegistry builds the registry a call resolves types against: core's own type
// set, plus one codec per [Codec] handed to it, plus whatever foreign struct tag
// keys [WithTagKeys] declares.
//
//	var registry = ferry.NewRegistry(
//	    ferry.NumberText[big.Int](),
//	    ferry.StringText[netip.Addr]().AsMapKey(),
//	    ferry.WithTagKeys(yamlext.Extension()),
//	)
//
//	cfg, err := ferry.Load[Config](ctx, src, ferry.WithRegistry(registry))
//
// It is the whole of the registry API. A registry is complete when it is built
// and there are no mutators, so there is no window in which one is reachable and
// incomplete, and no ordering rule to keep between building it and using it.
//
// Core's own set is always underneath, so registering one type never costs a
// caller string, int, bool, time.Duration or anything else ferry already
// carries. Passing no codec at all is exactly the set core ships, which is what
// a call with no [WithRegistry] resolves against.
//
// It panics rather than returning an error, in regexp.MustCompile's family: a
// registration is written once, at a program's birth, and every refusal below is
// a mistake in the source rather than a condition a running program meets. What
// it panics with is an *[Error] of [ErrSchema]'s class, so a caller who recovers
// one reads the same report ferry gives any other refusal.
//
// It refuses five things about a codec. A nil one. A pointer type, because
// pointer indirection is structural and a codec for one would lose the null a
// nil pointer writes. A type core owns, whose representation is pinned and not
// replaceable, every predeclared type included: define a named type over it and
// register that. A second codec for a type another codec in the same call
// already claimed, since a registration claims its type unconditionally and
// there is no decline. And a codec that is not total over the zero value of its
// type, which is checked by running it.
//
// That last check catches one class of wrong codec out of three. A lossy codec
// and a constant codec both pass it, and the way to discharge those is a proof
// through ferrytest.
//
// What it refuses about a declared tag key is listed on [WithTagKeys], and is
// refused here for the same reason and in the same words.
func NewRegistry(items ...Registration) *Registry {
	r := &Registry{
		byType: make(map[reflect.Type]registration, len(items)),
		exts:   extSet{words: map[string]map[string]Word{}},
	}

	for _, it := range items {
		if it == nil {
			panic(regError(nilRegistrationMsg))
		}

		it.registerOn(r)
	}

	r.exts.seal()

	return r
}

// nilRegistrationMsg names both kinds of item, because a variadic that takes a
// sealed union makes a nil of either writable and neither one says which it
// meant to be.
const nilRegistrationMsg = "ferry.NewRegistry was given a nil registration: a codec comes from one of the " +
	"kind-named constructors, each of which takes both halves at once, and a tag key declaration comes from " +
	"ferry.WithTagKeys"

// builtins is what a call with no [WithRegistry] resolves against: core's own
// type set and no codec over it.
//
// It is a frozen base rather than a default a program writes to. Nothing can add
// to it, which is the whole of why a package-level registry is affordable here
// where a global mutable table is not: there is no window, no ordering rule, and
// no way for one package's registration to reach another package's load
// (ADR-0017, amended under #273).
var builtins = NewRegistry()

// Types is every type this registry holds a codec for, sorted.
//
// It exists so that a completeness check can join a list of proofs against the
// types that were registered, and tell a registrant who added a codec and no
// proof. The result is freshly allocated and the caller's to keep, and a nil
// registry holds nothing.
func (r *Registry) Types() []reflect.Type {
	if r == nil {
		return nil
	}

	out := make([]reflect.Type, 0, len(r.byType))
	for t := range r.byType {
		out = append(out, t)
	}

	// Sorted on the package path first, so that two types whose String() agree -
	// which is the false positive ADR-0005's identity table exists to avoid - are
	// still ordered by something that tells them apart.
	slices.SortFunc(out, func(a, b reflect.Type) int {
		if c := strings.Compare(a.PkgPath(), b.PkgPath()); c != 0 {
			return c
		}

		return strings.Compare(a.String(), b.String())
	})

	return out
}

// add applies one registration, while the registry is still inside
// [NewRegistry] and has not escaped.
//
// Every refusal is a panic, because the constructor has no error to return and
// the reason it has none is the decision: a registry is complete at birth, so a
// refusal here is a program that cannot start rather than a call that failed
// (ADR-0017).
func (r *Registry) add(g registration) {
	if err := r.refuse(g); err != nil {
		panic(err)
	}

	if err := g.total(); err != nil {
		panic(err)
	}

	r.byType[g.typ] = g
}

// refuse is everything a registration is held to before its codec is run.
//
// The order is the order a reader wants: the structural facts about the type,
// then what else the table already holds. What is not a registration at all is
// refused a step earlier, at [NewRegistry], because the union it takes makes a
// nil of either kind writable (ADR-0021).
func (r *Registry) refuse(g registration) error {
	switch {
	case g.typ.Kind() == reflect.Pointer:
		return regError(fmt.Sprintf("%s may not be registered: pointer indirection is structural and a pointer "+
			"type never reaches the table, so an entry for one would make a nil pointer a leaf and lose the "+
			"null it writes at its own address - register %s instead", g.typ, g.typ.Elem()))
	case coreOwns(g.typ):
		return regError(fmt.Sprintf("%s is in core's own set and its representation is pinned: an entry ferry "+
			"owns is not replaceable, because a stored plane holds what ferry promised for it - define a named "+
			"type over it and register that", g.typ))
	default:
		return r.duplicate(g.typ)
	}
}

// duplicate refuses a second entry for one type.
//
// There is no decline, so a registration claims its type unconditionally and
// there is never a second entry to fall through to. What a caller reaching for
// one usually wants is spelled "do not register this type", which falls through
// to the text pair and then to kind admission.
func (r *Registry) duplicate(t reflect.Type) error {
	if _, ok := r.byType[t]; !ok {
		return nil
	}

	return regError(fmt.Sprintf("%s is already registered: a registration claims its type unconditionally and "+
		"there is no decline, so two entries for one type would be a precedence question nothing chooses "+
		"between - keep the one that is right, and build a second registry if both are", t))
}

// coreOwns reports a type whose representation is core's own: the two entries of
// the identity table, and the predeclared types kind admission claims.
//
// A named type is never one of them, and that is ADR-0005's documented escape
// rather than a loophole: `type PollInterval time.Duration` has a package path,
// misses this rule, and is exactly what a registrant defines when the type they
// wanted is pinned.
//
// The predeclared half matters as much as the identity half. Registering int
// would replace the representation of every int in every struct in the program,
// which is a thing to do on purpose to a type somebody named and never to the
// language's own.
func coreOwns(t reflect.Type) bool {
	if _, ok := byIdentity[t]; ok {
		return true
	}

	if t.PkgPath() != "" {
		return false
	}

	_, ok := leafByKind(t)

	return ok
}

// total runs the codec against the zero value of its type, which is the one
// value core holds without being given anything by the registrant.
//
// It encodes the zero, runs the same donation the walk runs, decodes it back,
// and refuses if either half errors. That replaces a doc comment with a check
// that fires at the call site, and the assumption which made a doc comment look
// like the only option - that core cannot say anything about a codec without
// values from the registrant - is false, because [NewRegistry] holds T.
//
// The registrant's own halves run under the fence, so a codec that panics on its
// own zero value is this refusal rather than a stack trace out of a package the
// registrant has never opened (ADR-0011 as amended under #254, #262).
//
// It matters because the one-line registration a user is most likely to write
// is broken for three common standard-library types, and because registration
// is step one of the chain: a registration for netip.Addr over String and
// ParseAddr replaces a correct text pair with a codec that dumps
// string("invalid IP") and cannot load it back, so the type worked before the
// user tried to help it.
//
// It catches one class of wrong codec out of three, and the ratio is the honest
// statement rather than the headline: a lossy codec and a constant codec both
// pass this. Those two are what ADR-0005's proof triple is for, and this check
// does not pretend to replace it. The third class, a codec that declares one
// kind and emits another, is not on the list any more because ADR-0017's
// payload-typed halves make it unwritable.
//
// The cause is attached without adopting its class (#228). A registrant is
// invited to wrap ferry's own sentinels in their own errors, so a codec whose
// zero value fails with an ErrPlane inside it would otherwise turn a refusal
// about a registration call site into a report reading "register, plane error".
// The class override belongs at the walk, where a codec is speaking about a
// value the plane actually held.
func (g registration) total() error {
	zero := reflect.New(g.typ).Elem()

	out, err := g.codec.encode(zero)
	if err != nil {
		return regError(fmt.Sprintf("%s: the codec is not total over the zero value, and encoding one failed: %s",
			g.typ, err)).because(err)
	}

	back := reflect.New(g.typ).Elem()
	if err := g.codec.decode(back, out); err != nil {
		return regError(fmt.Sprintf("%s: the codec is not total over the zero value: it encodes to %#v and "+
			"decoding that back fails: %s", g.typ, out, err)).because(err)
	}

	return nil
}

// regError is a refusal about a registration call site.
//
// It carries no location, because a registration names no address and no field:
// there is no schema yet, and the type is in the message. The moment is
// register, which sorts before every other, so a program that registers badly
// and then loads reads its own startup failure first.
//
// It is the one place ferry's own message text quotes a Value, and the
// exception is narrow enough to state: what it quotes is the codec's own
// encoding of the zero value of a Go type, produced by core with nothing from
// any plane, so ADR-0011's rule about never printing what a plane supplied is
// not in play.
func regError(msg string) *Error {
	return newError(momentRegister, ErrSchema, Path{}, msg)
}

// lookup is the read path, and it takes no lock.
//
// That is what construction-is-the-freeze buys, and it is the whole of #227's
// fix. A registry read by a compile while something writes it is a data race
// whether or not any ADR mentions goroutines, and no mutex inside ferry fixes
// it, because the unlocked read is the point of a resolution that happens once
// per type. A registry is written before it escapes [NewRegistry] and never
// again, so this is a plain map lookup with no lock and no atomic (ADR-0017).
func (r *Registry) lookup(t reflect.Type) (registration, bool) {
	cd, ok := r.byType[t]

	return cd, ok
}
