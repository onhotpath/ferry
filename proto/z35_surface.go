package main

// #35: what ferrytest exports, and what the conformance suites contain.
//
// Seven ADRs assign obligations to this package and no ticket owned its shape.
// This file is the proposed surface, written as real code over the tip's own
// engine so that every call site below compiles and runs rather than reading
// well.
//
// It is deliberately NOT wired into the tip's existing harness.go, which stays
// as it is so the regression diff is honest. Where the two disagree, that
// disagreement is a measurement and is printed by Z35=3.
//
// Run: `Z35=<n|all> GOTOOLCHAIN=go1.27rc2 go run .` from proto/.

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
)

// ===========================================================================
// 1. THE APPARATUS. A plane with no format and no I/O, plus a recorder.
//    ADR-0002 admits these to core by route (b), on the line that "a plane
//    with no serialization format and no I/O is not a plane, it is a map".
// ===========================================================================

// ZPlane is ADR-0005's Plane, with ADR-0004's kind declaration folded in
// rather than described twice. The two ADRs describe one mechanism:
//
//	ADR-0004  "a driver declares the Value kinds its plane can carry"
//	ADR-0005  "the conformance suite runs the proofs that plane can express,
//	           and asserts that the ones it cannot are refused loudly"
//
// One field, named once.
type ZPlane struct {
	Name  string
	Kinds []VKind

	// Open hands back a fresh Source and Sink over ONE storage, which is why
	// it is a function rather than two fields: a round trip needs a new plane
	// per value, and "reset the same one" hides whether a proof is reading its
	// own leftovers. ADR-0005 names the pairing reason: "a driver supplying
	// two unrelated planes is the mistake it exists to prevent".
	Open func() (FSource, FSink)

	// Golden is #28's artefact case list, and it is on the Plane rather than a
	// parameter of the suite because it is the DRIVER's statement about its
	// own spelling. A suite that supplied it would be core choosing a
	// representation ADR-0005 puts on the driver's side of the line.
	Golden []ZArtefact
}

func (p ZPlane) carries(k VKind) bool { return slices.Contains(p.Kinds, k) }

// ===========================================================================
// 2. THE PROOF. One type, three columns, running through the entry point.
//    #28 makes this an obligation rather than a preference: the golden column
//    IS ferry's plane-compatibility promise, and the tip has it on the proof
//    type that runs through the superseded walk.
// ===========================================================================

// ZCase is one value and the boundary Value it must produce.
type ZCase[T any] struct {
	Value T
	Want  Value
}

// ZProof is one type's discharged obligation. The only way to make one is
// ZType, so a proof cannot exist without a relation.
type ZProof interface {
	Name() string
	// Type is what the completeness check joins on.
	//
	// The tip's check joins by NAME and its own comment gives the reason -
	// "Proof is an interface over a type parameter the check cannot recover".
	// It can: one method on the interface recovers it, and the join stops being
	// the loosest possible one. Measured, the difference is a whole class of
	// silent miss, because a proof named "[]byte" discharges a member spelled
	// "[N]byte" only by a special case somebody wrote by hand.
	Type() reflect.Type
	Cases() int
	run(p ZPlane, opts []Option) (fails, refused []string)
}

type zproof[T any] struct {
	name  string
	eq    func(a, b T) bool
	cases []ZCase[T]
}

func (p zproof[T]) Name() string       { return p.name }
func (p zproof[T]) Type() reflect.Type { return reflect.TypeFor[T]() }
func (p zproof[T]) Cases() int         { return len(p.cases) }

// ZType is the constructor. The relation is required rather than defaulted,
// because every type whose relation is not == has a carve-out somebody has to
// have thought about; and the golden is required rather than optional, because
// the property alone is blind to the representation and that blindness is what
// #28 turns into a compatibility promise nobody can check.
func ZType[T any](name string, eq func(a, b T) bool, cases ...ZCase[T]) ZProof {
	return zproof[T]{name, eq, cases}
}

// ZAt is the case constructor, named for how it reads at a call site:
//
//	ZType("int", Eq[int], ZAt(0, Num("0")), ZAt(-5, Num("-5")))
func ZAt[T any](v T, want Value) ZCase[T] { return ZCase[T]{v, want} }

func (p zproof[T]) run(pl ZPlane, opts []Option) (fails, refused []string) {
	ctx := context.Background()
	for i, c := range p.cases {
		type holder struct {
			V T `ferry:"V"`
		}
		in := holder{c.Value}
		src, sink := pl.Open()

		// Column three, checked against what the ENGINE produced, through a
		// recording sink that WRAPS the plane's own. That is #28's obligation
		// - the golden column has to sit on the proof that runs through Dump -
		// and it is why ADR-0002's recording sink is apparatus rather than a
		// convenience: ADR-0012's Observe option is Load-side only, so there
		// is no other way to see what ferry encoded before a driver spelt it.
		rec := map[Path]Value{}
		sink = zRecorder{into: rec, next: sink}
		if err := Dump(ctx, in, sink, opts...); err != nil {
			if k, ok := refusedKind(err); ok && !pl.carries(k) {
				refused = append(refused, fmt.Sprintf("%s[%d] writes %s", p.name, i, k))
				continue
			}
			fails = append(fails, fmt.Sprintf("%s[%d]: dump: %v", p.name, i, err))
			continue
		}
		if got, ok := rec[Path{}.Name("V")]; !ok {
			fails = append(fails, fmt.Sprintf("%s[%d]: golden: nothing written at /V", p.name, i))
		} else if got != c.Want {
			fails = append(fails, fmt.Sprintf("%s[%d]: golden: got %s want %s",
				p.name, i, got.GoString(), c.Want.GoString()))
		}

		out, err := Load[holder](ctx, src, opts...)
		if err != nil {
			fails = append(fails, fmt.Sprintf("%s[%d]: load: %v", p.name, i, err))
			continue
		}
		if !p.eq(in.V, out.V) {
			fails = append(fails, fmt.Sprintf("%s[%d]: %#v -> %#v", p.name, i, in.V, out.V))
		}
	}
	return fails, refused
}

// zRecorder is ADR-0002's recording sink, in the one shape that makes it
// load-bearing rather than a demo: a Sink WRAPPING a Sink. ADR-0004 already
// names `Recorder` among the combinators the contract admits and ships none;
// this is that combinator, and #28 is why it has to exist rather than be
// possible.
type zRecorder struct {
	into map[Path]Value
	next FSink
}

func (r zRecorder) Bind(a *AddressSet) (FOpenWriterFunc, error) {
	open, err := r.next.Bind(a)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (FWriter, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}
		return zRecWriter{r.into, w}, nil
	}, nil
}

type zRecWriter struct {
	into map[Path]Value
	next FWriter
}

func (w zRecWriter) Set(ctx context.Context, p Path, v Value) error {
	w.into[p] = v
	return w.next.Set(ctx, p, v)
}

// The optional interfaces have to be forwarded or the wrapper silently changes
// the driver's lifecycle, which is #10's own recorded defect: "cross-cutting
// concerns, and the thing a wrapper silently drops".
func (w zRecWriter) Commit(ctx context.Context) error {
	if c, ok := w.next.(FCommitter); ok {
		return c.Commit(ctx)
	}
	return nil
}

func (w zRecWriter) Close() error {
	if r, ok := w.next.(FReleaser); ok {
		return r.Close()
	}
	return nil
}

// ===========================================================================
// 3. THE RELATIONS. ADR-0005's five, unchanged.
// ===========================================================================

func ZEq[T comparable](a, b T) bool { return a == b }

// ZSliceEq and ZMapEq treat nil and empty as one value, which is ADR-0005's
// decision rather than a convenience.
func ZSliceEq[T any](eq func(a, b T) bool) func(a, b []T) bool {
	return func(a, b []T) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if !eq(a[i], b[i]) {
				return false
			}
		}
		return true
	}
}

func ZMapEq[K comparable, V any](eq func(a, b V) bool) func(a, b map[K]V) bool {
	return func(a, b map[K]V) bool {
		if len(a) != len(b) {
			return false
		}
		for k, av := range a {
			bv, ok := b[k]
			if !ok || !eq(av, bv) {
				return false
			}
		}
		return true
	}
}

func ZPtrEq[T any](eq func(a, b T) bool) func(a, b *T) bool {
	return func(a, b *T) bool {
		if a == nil || b == nil {
			return a == b
		}
		return eq(*a, *b)
	}
}

// ===========================================================================
// 4. THE THREE SUITES.
//
//    ADR-0001 names the round-trip property harness and the driver conformance
//    suite separately, and ADR-0007 and ADR-0009 then ask for a codec one. So
//    three entry points, and the driver one CALLS the property one rather than
//    duplicating it, which is what makes `driver/*` a single-call CI glob.
// ===========================================================================

// ZT is the assertion sink. It is `*testing.T`'s two methods and nothing else,
// so the suites are runnable from a probe as well as from a test, and so the
// package's own tests can assert on what a suite reports.
//
// ADR-0011 already found this shape necessary from the other end: "the
// primitive returns []string and takes no *testing.T, because the conformance
// suite runs against third-party drivers and wants the result as data".
type ZT interface {
	Errorf(format string, args ...any)
	Helper()
}

// ZRoundTrip is ADR-0005's verb.
//
// The proofs are a SLICE and the options are the variadic tail, which is the
// resolution of ADR-0005's `RoundTrip(t, Plane, ...Proof)` against ADR-0009's
// `RoundTrip(t, *Registry, Plane, ...Proof)`. Z35=2 measures the three
// candidates at their call sites; the deciding argument is that a *Registry
// parameter would be a SECOND way to say what ferry.WithRegistry already says,
// which is survey item 5.14's own defect.
func ZRoundTrip(t ZT, p ZPlane, proofs []ZProof, opts ...Option) {
	t.Helper()
	for _, pr := range proofs {
		f, _ := pr.run(p, opts)
		for _, s := range f {
			t.Errorf("%s: %s", p.Name, s)
		}
	}
}

// ZComplete is ONE function over the union of every table, which is the
// resolution of ADR-0005's completeness check over core's identity table and
// ADR-0009's over a registry. They were the same check over two tables.
//
// It returns data rather than asserting, because a registrant wants to append
// their own proofs and re-ask.
func ZComplete(reg *Registry, proofs ...ZProof) []string {
	have := map[reflect.Type]bool{}
	for _, p := range proofs {
		have[p.Type()] = true
	}
	var missing []string
	for _, m := range zMembers(reg) {
		if !have[m.typ] {
			missing = append(missing, fmt.Sprintf("%s (%s) has no proof", m.typ, m.how))
		}
	}
	sort.Strings(missing)
	return missing
}

type zMember struct {
	typ reflect.Type
	how string
}

// zMembers is the UNION of the three tables that define what ferry claims to
// support: core's identity table, core's admitted kinds, and a registry.
// ADR-0005's check covered the first two and ADR-0009's the third; they are
// one check and this is it.
func zMembers(reg *Registry) []zMember {
	var out []zMember
	for t := range byIdentity {
		out = append(out, zMember{t, "identity table"})
	}
	for _, k := range admittedKinds {
		out = append(out, zMember{zKindRep(k), "admitted kind"})
	}
	// The two composite shapes kind admission claims by an ELEMENT kind rather
	// than by their own.
	for _, t := range []reflect.Type{reflect.TypeFor[[]byte](), reflect.TypeFor[[1]byte]()} {
		if kindAdmitsLeaf(t) {
			out = append(out, zMember{t, "admitted kind"})
		}
	}
	if reg != nil {
		for _, t := range zRegistryTypes(reg) {
			out = append(out, zMember{t, "registered"})
		}
	}
	slices.SortFunc(out, func(a, b zMember) int { return strings.Compare(a.typ.String(), b.typ.String()) })
	return out
}

// zKindRep is the one representative type per admitted kind. A kind is not a
// type, so the join needs a canonical member, and core picks it rather than
// leaving every proof author to guess.
func zKindRep(k reflect.Kind) reflect.Type {
	switch k {
	case reflect.Bool:
		return reflect.TypeFor[bool]()
	case reflect.String:
		return reflect.TypeFor[string]()
	case reflect.Int:
		return reflect.TypeFor[int]()
	case reflect.Int8:
		return reflect.TypeFor[int8]()
	case reflect.Int16:
		return reflect.TypeFor[int16]()
	case reflect.Int32:
		return reflect.TypeFor[int32]()
	case reflect.Int64:
		return reflect.TypeFor[int64]()
	case reflect.Uint:
		return reflect.TypeFor[uint]()
	case reflect.Uint8:
		return reflect.TypeFor[uint8]()
	case reflect.Uint16:
		return reflect.TypeFor[uint16]()
	case reflect.Uint32:
		return reflect.TypeFor[uint32]()
	case reflect.Uint64:
		return reflect.TypeFor[uint64]()
	case reflect.Float32:
		return reflect.TypeFor[float32]()
	case reflect.Float64:
		return reflect.TypeFor[float64]()
	}
	// admittedKinds is core's own list and every member is above. A new kind
	// arriving here with no representative is the drift this check exists to
	// catch, so it reports rather than guessing.
	panic("ferrytest: admitted kind " + k.String() + " has no representative type")
}

// zRegistryTypes is the ONE thing ferrytest needs from a Registry, and it is
// deliberately not an accessor on a Reg. ADR-0009's finding is that a proof
// needs NOTHING from a registration, because it exercises the codec through the
// ordinary walk, and that is what keeps Reg opaque. Enumerating the types is a
// property of the registry rather than of any registration.
func zRegistryTypes(r *Registry) []reflect.Type {
	var out []reflect.Type
	for t := range r.byType {
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b reflect.Type) int { return strings.Compare(a.String(), b.String()) })
	return out
}

// ZInjective is #31's key obligation, corrected on both counts ADR-0009 got
// wrong: T is `comparable`, because injectivity is over Go's ==; and the text
// comes from FERRY rather than from a format function the prover supplies,
// because what addresses a plane is the key-text lookup and a registrant's own
// String() is not it.
func ZInjective[T comparable](reg *Registry, values ...T) []string {
	var bad []string
	done := reg.install()
	defer done()
	seen := map[string]T{}
	for _, v := range values {
		text := mapKeyText(reflect.ValueOf(v))
		if prev, dup := seen[text]; dup && prev != v {
			bad = append(bad, fmt.Sprintf("%#v and %#v both address %q", prev, v, text))
			continue
		}
		seen[text] = v
	}
	sort.Strings(bad)
	return bad
}
