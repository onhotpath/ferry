package ferry

import (
	"encoding"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// This file is step zero of ADR-0007's chain: the codecs a program registers
// for types ferry does not own, held in a value rather than in a global table.
//
// ADR-0001 makes core's type set closed and its extension explicit, and
// ADR-0009 is that sentence turned into an API. Three properties of it are
// decisions rather than implementation, and each is argued where it is spelled:
// a registration is a pair by construction, so half a pair does not compile; a
// registry freezes at its first retained schema compile, so no schema is ever
// resolved against one set of codecs and walked against another; and there is
// no decline and no registration by runtime reflect.Type, so a codec's claim is
// a property of the type alone and the address set stays computable with no
// value in hand.

// Reg is one registration: a type, the boundary kind its codec declares, and
// both halves of that codec.
//
// It is opaque, and the only way to build one is [TextCodec], [StringCodec],
// [ValueCodec] or [DurationLike]. Every one of them takes both halves at once,
// which is ADR-0007's "a codec is a pair" made unrepresentable-otherwise rather
// than documented: a half pair, two halves swapped and two halves over
// different types are each a build error, so nothing on the registration path
// has to check for one at run time.
//
// Nothing is readable off a Reg. A proof exercises a codec through the ordinary
// walk, so a harness needs no accessor on a registration, and an accessor would
// be exported surface forever (ADR-0009).
type Reg struct {
	typ   reflect.Type
	codec leafCodec

	// key is the registrant declaring this codec's text injective over its own
	// type, which is what [Reg.AsMapKey] says and what nothing else can check.
	key bool

	// err is a refusal the constructor already knows about, carried rather than
	// returned so that a constructor stays one expression at a call site and so
	// that every failure in a variadic Register is reported together.
	err error
}

// AsMapKey declares this codec's text injective over its type under Go's ==,
// which is what a map key needs and what no rule ferry can write discharges for
// a codec somebody else supplied.
//
// It is opt-in because the failure it prevents is silent. Two keys rendering to
// one text are one address, so one entry is lost with no error anywhere and
// which one survives is map iteration order. A map[T]V whose key type is
// registered without this is a schema compile error naming this method, and
// that diagnostic is the only moment a registrant is guaranteed to read
// (ADR-0009).
//
// The cost is stated rather than hidden: a registrant whose codec is injective
// and who has not said so gets a refusal for a codec that is fine. It is
// affordable because the fix is one method call named after the obligation and
// because it arrives at schema compile, where the implied rule's failure is a
// dropped map entry on a plane already written.
//
// It is a claim rather than a check. Discharging it over the values a
// registrant cares about is what ferrytest.Injective is for.
func (g Reg) AsMapKey() Reg {
	g.key = true

	return g
}

// textPtr is what makes [TextCodec] take no function arguments: *T declares
// both halves of the text pair, so both halves come from the type.
//
// It is written over *T rather than over T because the decode half has to write
// back. *T's method set contains T's, so a type declaring either half on a value
// receiver still satisfies this constraint, and the one that then does not work
// - UnmarshalText on a value receiver, which decodes into a copy - is refused by
// [Registry.Register] with the diagnostic that names the receiver.
type textPtr[T any] interface {
	*T
	encoding.TextMarshaler
	encoding.TextUnmarshaler
}

// TextCodec registers a type that already declares its own text form and its
// own inverse, at a boundary kind of the registrant's choosing.
//
// Its purpose is changing the kind rather than rescuing the type, and that is
// narrower than it looks: ADR-0007's chain already claims any type carrying a
// text pair, correctly, at kind String. What this adds is the kind, which is
// the difference between a value that loads from a flat plane and one that
// loads from a structured plane too:
//
//	ferry.TextCodec[big.Int](ferry.KindNumber)
//
// big.Int's text is a run of digits, so unregistered it lands as
// string("1099511627776") and does not load from a YAML plane that reports
// Number. Declaring Number loads from both, because String is donated to the
// declared kind on the way in.
//
// PT is inferred from T by constraint type inference, so one type argument is
// written at a call site and never two. It is the one constructor with no value
// argument to infer T from, so T is named.
func TextCodec[T any, PT textPtr[T]](kind VKind) Reg {
	t := reflect.TypeFor[T]()

	g := Reg{typ: t, codec: leafCodec{kind: kind, encode: encodeTextAs(kind), parse: parseText}}

	// The constraint guarantees *T declares both halves, so the only way the
	// pair is still incomplete is the one it cannot express: an UnmarshalText on
	// a value receiver is in *T's method set and writes to a copy anyway.
	if !armOf(t).complete() {
		g.err = regError(fmt.Sprintf("%s declares UnmarshalText on a value receiver, which decodes into a copy "+
			"and leaves the field unchanged, so it is not the decode half a registration can use - move "+
			"UnmarshalText to *%s, or register a ferry.StringCodec whose parse half returns the value", t, t))
	}

	return g
}

// encodeTextAs is the text pair's encode half at a declared kind other than
// String, which is the whole of what [TextCodec] adds to the chain's own arm.
//
// The text is unchanged and only the kind differs, because that is what the
// distinction is: encoding.TextMarshaler produces text and says nothing about
// what kind a plane should hold it at, and the registrant is the party that
// knows.
func encodeTextAs(kind VKind) func(reflect.Value) (Value, error) {
	return func(v reflect.Value) (Value, error) {
		out, err := encodeText(v)
		if err != nil {
			return Value{}, err
		}

		return Value{kind: kind, text: out.text}, nil
	}
}

// StringCodec registers a type that declares no inverse of its own, as two
// functions over string.
//
//	ferry.StringCodec(
//	    func(u url.URL) string { return u.String() },
//	    func(s string) (url.URL, error) {
//	        u, err := url.Parse(s)
//	        if err != nil {
//	            return url.URL{}, err
//	        }
//	        return *u, nil
//	    })
//
// T is inferred from either function, so no call site writes a type argument,
// and a method expression makes the whole registration one line where the
// standard library put the receivers the right way round.
//
// The declared kind is String, which is what two functions over string can
// promise. A type whose text is a run of digits wants [ValueCodec] or
// [TextCodec], and a codec that must accept a kind it never emits - a Null, for
// a Go type whose kind has no null - can only be a [ValueCodec], because this
// decode half never sees anything but a string.
//
// The zero value is worth a thought before writing one, and
// [Registry.Register] runs it rather than trusting the thought: netip.Addr,
// netip.AddrPort and netip.Prefix all render their zero as "invalid IP" and
// cannot parse it back, so the one-liner over String and Parse is refused for
// all three. Those are also the types where registering the obvious thing makes
// the type worse than leaving it alone, because the text pair they already
// carry is correct.
func StringCodec[T any](format func(T) string, parse func(string) (T, error)) Reg {
	return Reg{
		typ: reflect.TypeFor[T](),
		codec: leafCodec{
			kind: KindString,
			encode: func(v reflect.Value) (Value, error) {
				return String(format(valueOf[T](v))), nil
			},
			parse: parseInto(parse),
		},
	}
}

// ValueCodec registers a type as a kind and two functions over [Value], which
// is the general form and the only one of the three that can accept a kind it
// never emits.
//
//	ferry.ValueCodec(ferry.KindNumber,
//	    func(x big.Int) (ferry.Value, error) { return ferry.Number(x.String()), nil },
//	    func(v ferry.Value) (big.Int, error) { ... })
//
// It stays even though it is the longest of the three, because ADR-0006's
// strictness rests on it. Core refuses a Null at every leaf whose Go kind has
// no null, and its argument for refusing rather than zeroing is that a
// registered codec for the type accepts the Null and returns whatever it likes.
// A decode half that sees the whole Value is the only shape that can, and it is
// also how an interface is expressible at all: a nil interface emits Null and
// takes Null back.
//
// The declared kind is a donation target and nothing else. It does not
// constrain the kinds this codec accepts, and beyond a Null it is the one thing
// core checks about a codec it did not write: a codec declaring Number and
// producing String is reported at Dump.
func ValueCodec[T any](kind VKind, enc func(T) (Value, error), dec func(Value) (T, error)) Reg {
	accept := func(v reflect.Value, got Value) error {
		out, err := dec(donate(got, kind))
		if err != nil {
			return &parseFailure{typ: v.Type(), err: err}
		}

		setFrom(v, out)

		return nil
	}

	return Reg{
		typ: reflect.TypeFor[T](),
		codec: leafCodec{
			kind:   kind,
			encode: encodeValue(enc),
			// The text half is the whole-Value half at the declared kind, so a
			// registered type under a pointer and a registered map key both reach
			// the registrant's own decode rather than a second one.
			parse:  func(v reflect.Value, text string) error { return accept(v, Value{kind: kind, text: text}) },
			accept: accept,
		},
	}
}

// encodeValue is [ValueCodec]'s encode half, lifted out so the constructor stays
// one expression.
func encodeValue[T any](enc func(T) (Value, error)) func(reflect.Value) (Value, error) {
	return func(v reflect.Value) (Value, error) {
		out, err := enc(valueOf[T](v))
		if err != nil {
			return Value{}, &encodeFailure{typ: v.Type(), err: err}
		}

		return out, nil
	}
}

// DurationLike registers a named type over int64 with time.Duration's own
// representation, which closes ADR-0005's named-duration hole at one line per
// type.
//
//	type PollInterval time.Duration
//
//	ferry.Register(ferry.DurationLike[PollInterval]())
//
// ADR-0005 leaves the hole open on purpose: such a type is a distinct
// reflect.Type, misses the identity table and falls to kind int64, so it dumps
// a nanosecond count, and matching on the underlying type instead would capture
// every ordinary `type Port int` as well. Core ships the one-liner rather than a
// predicate arm, because a predicate over the underlying type has exactly that
// defect handed to the user, and two predicates matching one type would make
// precedence a property of the import graph.
//
// It is the other place inference does not work, for the same reason
// [TextCodec] names its type: there is no value argument to infer from.
func DurationLike[T ~int64]() Reg {
	return StringCodec(
		func(d T) string { return time.Duration(d).String() },
		func(text string) (T, error) {
			d, err := time.ParseDuration(text)

			return T(d), err
		})
}

// valueOf reads a T back out of the reflect.Value the walk holds, in the
// comma-ok form.
//
// The comma is one token wide and it is the difference between working and
// panicking at an interface field: v.Interface() on a nil interface is a nil
// any, and the single-result assertion panics on it - inside ferry, on third
// party code, at Dump. Measured, the comma-ok form is not slower than the
// panicking one (ADR-0009).
func valueOf[T any](v reflect.Value) T {
	t, _ := v.Interface().(T)

	return t
}

// setFrom writes a T back into the field the walk is holding.
//
// It goes through a fresh pointer rather than reflect.ValueOf(out) for the
// mirror image of the reason above: reflect.ValueOf of a nil interface is the
// zero reflect.Value and Set panics on it, where &out yields a Value of static
// type T whatever the dynamic value turns out to be.
func setFrom[T any](v reflect.Value, out T) {
	v.Set(reflect.ValueOf(&out).Elem())
}

// parseInto is the text decode half shared by [StringCodec] and, through it,
// by [DurationLike].
func parseInto[T any](parse func(string) (T, error)) func(reflect.Value, string) error {
	return func(v reflect.Value, text string) error {
		out, err := parse(text)
		if err != nil {
			return &parseFailure{typ: v.Type(), err: err}
		}

		setFrom(v, out)

		return nil
	}
}

// donate is ADR-0007's one coercion, applied to the whole Value rather than by
// handing a codec bare text.
//
// String is the universal donor because String is what a plane says when it has
// nothing to say. A codec declaring Number asks AsNumber, and a flat plane -
// env, Consul, a query string - reports String for everything, so without this
// the same codec would work against YAML and fail against env. Null is not
// donated to anything: it is an observation in its own right, and what a codec
// does with one is the codec's (ADR-0006).
func donate(got Value, kind VKind) Value {
	if got.kind != KindString || kind == KindString {
		return got
	}

	return Value{kind: kind, text: got.text}
}

// Registry is the set of codecs one program registers for types ferry does not
// own.
//
// It is a value rather than a global table, and it freezes at its first
// retained schema compile - the first [Load], [LoadOver] or [Dump] run against
// it. A registration after that is a loud error naming the freeze point.
//
// Both halves of that are soundness rather than taste. A schema holds the codec
// it resolved, so a table that can change after a schema is built means a value
// is dumped through a codec the registrant replaced, with no error anywhere;
// and the address set a driver was handed and precomputed keys for is the one
// the resolution produced, so a late registration that collapses a struct to a
// leaf makes a legitimate write a driver error. A frozen registry is written
// before its first reader exists and never again, which is also what keeps the
// read path a plain map lookup with no lock (ADR-0009).
//
// Whole-registry freezing is chosen over per-type freezing on import-graph
// determinism: freezing each type as it is resolved would make whether a
// registration succeeds depend on which schemas were compiled first, which is
// the same ground on which this design refuses a predicate arm.
//
// Core ships a default registry, and the package-level [Register] is a method
// call on it rather than a second path with its own rules. A test that wants a
// different codec for one type builds its own with [NewRegistry] and hands it
// over with [WithRegistry].
//
// # A registry is long-lived, because the schema cache hangs off it
//
// Every schema a [Load], [LoadOver] or [Dump] compiles is cached on the
// registry it resolved against, and that cache is unbounded: nothing is ever
// evicted, because a compiled schema is reachable again from the type that
// produced it and no eviction policy could know that it will not be. Every
// library surveyed documents this rather than solving it, and so does ferry.
//
// The usual door onto that is a program that mints types at run time, where 200
// reflect.StructOf types produce 200 entries. ferry has a second, and it opens
// on ordinary, statically declared types: a registry that is kept alive keeps
// every schema ever compiled against it alive too. Measured on ADR-0009's
// prototype, whose cache was one package-level table keyed by the type and the
// registry together, 10,000 per-call registries produced 10,000 entries, none
// evictable. Hanging the cache off the registry moves where that lands rather
// than removing it: the entries are collected with the registry, and what a
// per-call registry buys instead is a full schema compile on every call with
// nothing reused, which is the cost the cache exists to remove. Either way the
// conclusion is ADR-0009's: build one registry per program, or per test, and
// hand it to every call with [WithRegistry].
//
// The zero Registry is empty, unfrozen and usable, and a nil *Registry reads as
// an empty one so that [Registry.Types] can be asked about a program that
// registered nothing.
type Registry struct {
	// mu guards the write path alone. The read path takes no lock at all,
	// because after the freeze there is nothing to guard against.
	mu sync.Mutex

	// frozen is atomic rather than guarded, so that a compile can ask whether it
	// may read the map without taking mu on every schema.
	frozen atomic.Bool

	byType map[reflect.Type]registration

	// schemas is the schema cache, and it hangs off the registry because the
	// registry is the outer level of the cache key: two registries that disagree
	// about one type are two schemas for that type, and a cache they shared would
	// hand one of them the other's codec (ADR-0009, ADR-0010).
	//
	// It is keyed by [schemaKey] and holds *[cacheEntry] and nothing else. mu
	// does not guard it: a sync.Map is its own synchronisation, and the cache is
	// written after the freeze where byType is written before it.
	schemas sync.Map
}

// registration is one entry of the table: the resolved codec, and whether the
// registrant declared it usable as a map key.
type registration struct {
	codec leafCodec
	key   bool
}

// NewRegistry builds an empty registry, which is what a test that needs a
// different codec for one type reaches for, and what a program that would
// rather not have a package-level anything uses instead of the default one.
func NewRegistry() *Registry { return &Registry{} }

// defaultRegistry is the registry [Register] writes to and the one every verb
// uses when no [WithRegistry] says otherwise.
//
// It is a Registry and it freezes like any other, which is the whole of why a
// default one is affordable where a global mutable table is not: nothing is
// expressible through one and not the other, so this is one mechanism with a
// convenience spelling rather than two ways to supply a codec.
//
// Its freeze point is fixed by Go's own initialisation order rather than by a
// convention. Every package-level variable and every init in the program runs
// to completion before main.main, so a Register in any init strictly precedes a
// Load in main whatever the import graph is. The one shape that breaks is a
// Load during init, and it is loud, at startup, with the freeze point named.
var defaultRegistry = NewRegistry()

// Register adds codecs to core's default registry.
//
//	func init() {
//	    if err := ferry.Register(
//	        ferry.TextCodec[netip.AddrPort](ferry.KindString),
//	        ferry.DurationLike[PollInterval](),
//	    ); err != nil {
//	        panic(err)
//	    }
//	}
//
// It is [Registry.Register] on the registry core ships, and it has no rules of
// its own. Range a failure with [Elements].
func Register(regs ...Reg) error { return defaultRegistry.Register(regs...) }

// Register adds codecs to this registry, reporting every failure rather than
// the first.
//
// Each registration is applied on its own, so one refusal in a variadic call
// does not withdraw the registrations beside it, and the failures are joined
// and sorted: ADR-0001's determinism invariant applies to a startup error
// exactly as it applies to a load (ADR-0011).
//
// What it refuses, and why each has no other place to be caught: a type core
// owns, because an entry in the identity table is not replaceable; a duplicate,
// because there is no decline and so no second entry to fall through to; a
// pointer type, because pointer indirection is structural and a registration
// for one would make a nil pointer a leaf; and a codec that is not total over
// the zero value of its type.
func (r *Registry) Register(regs ...Reg) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	errs := make([]error, 0, len(regs))
	for _, g := range regs {
		errs = append(errs, r.add(g))
	}

	return join(errs...)
}

// Types is every type this registry holds a codec for.
//
// It is the one thing exported from this package for ferrytest's sake, and it
// is a property of the registry rather than of any registration: the
// completeness check joins a proof list against the union of core's own tables
// and this one, so a registrant who adds a codec and no proof finds out from
// their own CI (ADR-0014).
//
// The result is sorted, freshly allocated and the caller's to keep. A nil
// registry holds nothing, which is the one place in this file a nil receiver is
// answered: every internal caller reaches a registry through the resolved
// Option set, where it is core's default or one WithRegistry refused to take
// nil for, and this is the only entry point a caller can hand a nil to.
func (r *Registry) Types() []reflect.Type {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

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

// add applies one registration, with mu already held.
func (r *Registry) add(g Reg) error {
	if err := r.refuse(g); err != nil {
		return err
	}

	if err := g.total(); err != nil {
		return err
	}

	if r.byType == nil {
		r.byType = map[reflect.Type]registration{}
	}

	r.byType[g.typ] = registration{codec: g.codec, key: g.key}

	return nil
}

// refuse is everything a registration is held to before its codec is run.
//
// The order is the order a reader wants: a registration that was never built
// properly is reported as that rather than as whatever it looks like, and the
// freeze is reported before any question about the type, because once a
// registry is frozen nothing about the type can change the answer.
func (r *Registry) refuse(g Reg) error {
	switch {
	case g.typ == nil:
		return regError("a zero ferry.Reg names no type and carries no codec: build one with ferry.TextCodec, " +
			"ferry.StringCodec, ferry.ValueCodec or ferry.DurationLike, each of which takes both halves at once")
	case g.err != nil:
		return g.err
	case r.frozen.Load():
		return regError(fmt.Sprintf("%s: the registry is frozen, because a schema has already been compiled "+
			"against it - every registration must happen before the first Load or Dump, which is what stops a "+
			"schema being resolved against one set of codecs and walked against another", g.typ))
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

// duplicate refuses a second entry for one type, with mu already held.
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
// values from the registrant - is false, because Register holds T.
//
// It matters because the one-line registration a user is most likely to write
// is broken for three common standard-library types, and because registration
// is step one of the chain: a registration for netip.Addr over String and
// ParseAddr replaces a correct text pair with a codec that dumps
// string("invalid IP") and cannot load it back, so the type worked before the
// user tried to help it.
//
// It catches one class of wrong codec out of four, and the ratio is the honest
// statement rather than the headline: a lossy codec, a constant codec and a
// codec that declares the wrong kind all pass this. Those three are what
// ADR-0005's proof triple is for, and this check does not pretend to replace
// it.
func (g Reg) total() error {
	zero := reflect.New(g.typ).Elem()

	out, err := g.codec.emit(zero)
	if err != nil {
		return regError(fmt.Sprintf("%s: the codec is not total over the zero value, and encoding one failed: %s",
			g.typ, err)).withCause(err)
	}

	back := reflect.New(g.typ).Elem()
	if err := g.codec.decode(back, out); err != nil {
		return regError(fmt.Sprintf("%s: the codec is not total over the zero value: it encodes to %#v and "+
			"decoding that back fails: %s", g.typ, out, err)).withCause(err)
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

// freeze closes a registry to further registration, at the first schema compile
// whose resolution outlives the call that built it.
//
// [Compile] does not freeze, and that is the same decision seen from the other
// end: it compiles a schema and discards it, retaining no resolution, so it is
// safe during init where a Load is not.
func (r *Registry) freeze() {
	if r.frozen.Load() {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.frozen.Store(true)
}

// lookup is the read path, and it takes no lock.
//
// That is what the freeze buys. A mutable registry read by a compile is a data
// race whether or not any ADR mentions goroutines, and no mutex inside ferry
// fixes it, because the unlocked read is the whole point of a resolution that
// happens once per type. A frozen registry is written before its first reader
// exists and never again, so this is a plain map lookup with no lock and no
// atomic (ADR-0009).
func (r *Registry) lookup(t reflect.Type) (registration, bool) {
	cd, ok := r.byType[t]

	return cd, ok
}
