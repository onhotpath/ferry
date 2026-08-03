package ferry

import "context"

// Source is the read half of a plane: one method, handed the addresses a
// compiled schema determined, handing back the function that opens a reader
// over them.
//
// One method is the budget rather than the minimum. ADR-0001 makes adoption
// depend on drivers being cheap to write, and measures the bar as koanf's
// twenty providers over a two-method interface; ADR-0004's four drivers land
// inside that band, at 45 to 194 lines, with a read-only driver implementing
// two methods in total.
//
// Bind is a phase of its own because the three pieces of state have three
// lifetimes: a Source holds driver config, which changes when you construct
// another one; an OpenFunc holds the precomputed key table, which changes when
// the schema or the config does; and a Reader holds the plane's contents, which
// change on every load. ADR-0003 requires the plane keys to be precomputed once
// per schema rather than derived per lookup, and states it as a requirement of
// the design rather than an optimisation. Measured over a six-address load:
// 158ns and one allocation bound once, against 2743ns and sixty bound on every
// load.
//
// The split is also what removes a cache. Core memoising the key table on
// (address set, driver name) hands one driver another driver's keys, measured
// on two environment sources differing only in their separator, and letting a
// driver supply its own identity is unsound because a driver holding a func
// field is not hashable. Nothing is memoised here, so nothing can be memoised
// wrongly.
//
// # Bind does no I/O, and the missing context is how the type says so
//
// Bind takes no [context.Context] deliberately. A driver must succeed at Bind
// against a plane it cannot reach and fail inside the open instead, which is a
// conformance case ADR-0014 can only write because the address set and the I/O
// arrive in two calls rather than one.
//
// What Bind may fail for is what it can see without touching the plane: an
// address its plane cannot name, or a key function that is not injective over
// the set. Both are refusals about the schema, both name the address they
// dislike with [ErrorAt], and both happen before any backend call - which is
// what lets a plane-to-plane transfer be refused after zero calls rather than
// after reading the whole source.
//
// # What the set contains
//
// Every leaf address the type determines plus every container address, and
// never a wildcard shape (ADR-0003). A driver is therefore handed only
// addresses it can fetch, write, name and check. Addresses minted from a value,
// a map key or a sequence index, are not in it and arrive later; a driver that
// treats its precomputed table as a closed set refuses a legal write, which is
// the defect ADR-0004 measured and the reason core hands out a key function
// rather than a map.
type Source interface {
	Bind(addrs *AddressSet) (OpenFunc, error)
}

// Sink is the write half of a plane, and it is a separate interface from
// [Source] rather than the other half of one.
//
// The deciding case is environment variables, which have no honest Dump:
// setting the process environment is process-global mutation, and the thing
// people actually want is a .env file, which is a format and therefore plane
// knowledge. Under one combined interface the env driver would have to declare
// a Dump it cannot honour. With two, the refusal is free and it is a compile
// error at the call site rather than a runtime error or an ErrUnsupported
// nobody reads.
//
// The cost is stated rather than hidden: a driver serving both directions ships
// two types, because one type cannot have two Bind methods, so a round trip
// names the plane twice.
//
// A plane that is writable in principle but not right now refuses inside the
// [OpenWriterFunc], with an error wrapping [ErrReadOnly] - not at Bind, which
// does no I/O and so cannot know, and not at the first Set, which has already
// half-written the plane (ADR-0004).
type Sink interface {
	Bind(addrs *AddressSet) (OpenWriterFunc, error)
}

// OpenFunc opens a [Reader] over the addresses a [Source] was bound to. It is
// called once per load and may be called many times against one Bind.
//
// It is a function rather than an interface because nothing ever asks it a
// second question. An interface earns its place when a caller asks more than
// one thing of it or type-asserts it for optional behaviour: Reader is asserted
// for [Enumerator] and [Releaser], so Reader stays an interface, and OpenFunc is
// called once and never asked anything else.
//
// It is named rather than inline for three reasons: the phase contract needs a
// documentation site, a driver's signature then reads as the phase, and a named
// func type can grow methods later without a breaking change. The precedent is
// [context.CancelFunc] - a named func type returned by a constructor, closing
// over state, documented with prose about when to call it - and Go's convention
// for such a type is the -Func suffix even where it adapts no interface.
//
// # Why there are two concrete names and not one generic one
//
// A generic OpenFunc[T] compiles, works in a driver's return position and
// infers through a combinator. It saves exactly one exported name here and
// costs every driver signature, forever:
//
//	Bind(a *ferry.AddressSet) (ferry.OpenFunc, error)                // concrete
//	Bind(a *ferry.AddressSet) (ferry.OpenFunc[ferry.Reader], error)  // generic
//
// Against ADR-0001's constraint that adoption depends on drivers being cheap to
// write, that is symmetry in ferry's source paid for in every driver's source.
// A generic Binder[T] with type aliases over it is worse again, on a measured
// diagnostic: generic type aliases are resolved away in compiler errors, so a
// driver that gets it wrong is told it does not implement a name it never
// wrote.
//
// Batch and lazy are both this function's business and neither is core's. Bind
// already handed over the whole address set, so an OpenFunc may fetch
// everything in one round trip or fetch nothing at all; measured on a
// three-address schema, three backend calls against one, with the difference
// being one boolean inside the driver. That is why there is no Snapshotter
// interface: it would be a second contract for a choice the driver can already
// make.
type OpenFunc func(ctx context.Context) (Reader, error)

// OpenWriterFunc opens a [Writer] over the addresses a [Sink] was bound to.
//
// It is where a read-only refusal lands, wrapping [ErrReadOnly]: a KV with no
// write ACL and a file sink targeting an unwritable directory both fail here,
// after zero writes, rather than half way through a walk over the user's
// struct.
//
// It is a second concrete name rather than an instantiation of [OpenFunc], for
// the reason recorded there.
type OpenWriterFunc func(ctx context.Context) (Writer, error)

// Reader is an open plane, answering one address at a time.
//
// # Absence is a kind of the value and not a second return value
//
// Get keeps the ordinary Go (T, error) and reports absence as
// [Value.Kind] == [KindAbsent]. The three alternatives all express the three
// states and none is measurably faster on the miss path; what separates them is
// the mistake each one allows. (Value, bool, error) lets v, _, err := r.Get(...)
// compile with no vet diagnostic and turn absent into a zero value. (*Value,
// error) turns a missing nil check into a panic inside ferry, on a value
// third-party driver code produced. A sentinel error makes a driver's bare
// errors.New("not found") indistinguishable from a real failure, and costs 195ns
// and three allocations on what is the common case in a config load. A kind has
// no second channel to discard.
//
// Two properties earn it beyond tidiness: the accessors already refuse it,
// since Absent is neither string nor number, so no new discipline is required
// of core's leaf setters; and it survives being stored, so a map[Path]Value
// lookup miss is absence and a recording sink needs no parallel presence map.
//
// # What Get must never do
//
// A non-nil error must reach the caller as an error and never as an Absent.
// That is ADR-0014's conformance case 4, and it exists because survey item 5.11
// found a YAML provider discarding parse errors and returning an empty result;
// core committed the mirror of it and neither was visible for four prototypes.
//
// At a container address a driver returns Absent or Null and nothing else: a
// composite is read one element at a time under ADR-0003's structured
// addresses, so there is no group value for the container itself to hold.
//
// A Reader may also implement [Enumerator] and [Releaser]. Both are discovered
// by assertion and neither is required.
type Reader interface {
	Get(ctx context.Context, addr Path) (Value, error)
}

// Writer is an open plane being written to, one address at a time.
//
// Set is never called with an Absent value. Absent is a Reader-side kind - it
// is what a plane reports when it does not have an address - and an omitted
// address is one that gets no Set call at all rather than one that gets a Set
// of nothing. A composite with no elements is written as Null at its own
// address, which is a value the plane holds and a different observation
// entirely.
//
// A Writer may also implement [Committer] and [Releaser]. Both are discovered
// by assertion and neither is required.
type Writer interface {
	Set(ctx context.Context, addr Path, v Value) error
}

// Releaser is io.Closer, and a [Reader] or [Writer] implements it when it
// holds a resource. It is not a name ferry invents: a driver wrapping a file or
// a connection satisfies it already.
//
// Close takes no context because cleanup that can be cancelled is how the temp
// file leaks. It always runs, whether the walk succeeded or failed, and
// closed-without-[Committer.Commit] is the abort signal - so no driver is ever
// told that it failed, in the shape sql.Tx and bufio.Writer already have.
//
// It is optional rather than a method on Reader and Writer because a required
// Close is `return nil` boilerplate in four of ADR-0004's six sinks, and that is
// not merely noise: in the source it is indistinguishable from a driver that
// should have rolled back and did not, and nothing in the type system tells the
// two apart. The inverse risk - a driver that needed a lifecycle method and
// omitted it - is covered by a conformance case that ships regardless, because
// a sink that writes nothing at all fails the first dump-load-compare case in
// the suite.
type Releaser interface {
	Close() error
}

// Committer is implemented by a [Writer] whose writes are not durable until the
// end of a successful walk: a staging file sink, a transactional KV.
//
// Commit runs only when the walk succeeded. Close, if the writer has one, runs
// either way. That protocol is the whole design, and it is why neither method
// takes a cause: there is no failure to report to a driver, only a commit that
// does not happen.
//
// It takes a [context.Context] where [Releaser.Close] does not, because this is
// the actual I/O.
//
// It is separate from Releaser rather than folded into one lifecycle interface
// because the two concerns do not co-occur: a transactional KV commits with
// nothing to release, a write-per-Set KV does neither, and a lazy read side
// releases with nothing to commit.
type Committer interface {
	Commit(ctx context.Context) error
}

// Enumerator is implemented by a [Reader] whose plane can list what is under an
// address. It is how Load discovers the addresses that come from the value
// rather than from the type - a map's keys, a sequence's length - which the
// static address set cannot contain because they do not exist until there is a
// value.
//
// It is optional in both directions, and neither answer was available:
//
//   - It cannot be required, because it would exclude a plane class ferry wants.
//     A Vault kv-v2 LIST is a separate ACL capability, a token with read and no
//     list is ordinary, and some secret brokers answer only what you name, by
//     design.
//   - It cannot be omitted, because three of ADR-0004's four first-party planes
//     enumerate trivially, and omitting it would make ferry unable to load a map
//     from any plane at all.
//
// So the two directions cover different address sets, which is a documented
// property of a driver rather than a surprise: Dump reaches every address
// always, since the value is in hand; Load reaches the static addresses always
// and the dynamic ones only through this interface. Loading a map-typed field
// from a non-enumerating source is an error naming the field and the source,
// never a silently empty map.
//
// Children returns addresses rather than names, deliberately. An address
// carries its segment kind, so the plane says whether the container is a
// mapping or a sequence instead of the caller guessing it from base-10 text -
// which is the limitation jsontext.Pointer's own documentation admits to, and
// the reason [SegmentKind] exists.
type Enumerator interface {
	Children(ctx context.Context, prefix Path) ([]Path, error)
}
