// Package ferry is a bidirectional, struct-first data mapper. One annotated
// struct and one tag grammar drive both directions: Load fills a value from a
// pluggable source, and Dump writes the same value back to a pluggable sink.
//
// Core carries only what no driver can supply for itself - the walk, the schema
// compiler, tag parsing, the codec chain, defaults and zero values - and only
// what core imposes but cannot compile-check, which ships as the thing that
// checks it. Nothing in core knows what a plane is for; planes ship as driver
// modules under driver/ (ADR-0001, ADR-0002).
//
// # The type set
//
// A type is resolved by type identity first and by reflect.Kind second, and the
// ordering is the whole rule: time.Duration's kind is int64 and time.Time's
// kind is struct, so a kind-first walk would write a nanosecond count for one
// and three unexported fields for the other.
//
// Two leaves are owned by identity, and their representations are pinned:
// time.Duration is a string such as "30s", and time.Time is RFC 3339 with
// nanoseconds through its text pair. ferry gives time.Duration a representation
// where encoding/json/v2 refuses and its legacy option gives nanoseconds, and
// says so rather than claiming to follow v2.
//
// Admitted by kind: bool as Bool; string as String, carrying the bytes
// unmodified and not required to be UTF-8; the five signed and five unsigned
// integer widths as Number in base 10; float32 and float64 as Number, formatted
// at their own bit size; and []byte and [N]byte as Bytes. A named type over an
// admitted kind is admitted with it, so `type Port int` round-trips with
// nothing registered.
//
// On Load every leaf accepts its own kind, and additionally accepts String,
// whose text is parsed by exactly the parser that leaf's own kind uses. Nothing
// else coerces: String is what a plane says when it has nothing to say, while
// Number, Bool and Bytes are assertions a plane made and ferry respects. So a
// Number is refused at a Go string, because accepting it would destroy the
// quoting distinction the boundary preserves. Null is accepted by exactly the
// types that have a null, which among the leaves is []byte alone.
//
// fmt.Stringer is never consulted, in either direction, because String() string
// declares no inverse.
//
// # The composites whose addresses come from the type
//
// A composite is not itself a value. It contributes addresses, and its elements
// are the leaves. A struct mints one Name segment per exported field; a pointer
// mints no segment of its own; and [N]T mints exactly N Index segments, because
// the length is part of the type.
//
// Unexported fields are skipped, which is a rule rather than a silence: reflect
// cannot set one, so the alternative is refusing every struct containing a
// sync.Mutex, and the loss it could hide is caught by the rule below instead.
//
// A composite that can be nil has an address of its own, and the test is
// exactly that: *struct gets one, a plain struct does not because it cannot be
// nil, and [N]T does not because an array has no nil. Such an address carries
// absence or a null and never anything else, so it is never realised at the same
// time as anything beneath it. *T where T is a leaf is not a composite at all:
// the leaf already had an address, and the pointer adds a null to it.
//
// An array and a slice are not interchangeable, and the difference is a real
// capability rather than a spelling. An array's element addresses are known from
// reflect.TypeFor[T]() with no value in hand, so an array is loadable from a
// source that cannot enumerate and a slice is not. An absent element leaves the
// element at its zero value, exactly as an absent struct field does, and an
// index the array cannot hold is loud.
//
// Two whole-type refusals fall out of this. A struct that maps no address does
// not compile, checked at every level rather than only at the root: netip.Addr,
// netip.AddrPort, big.Int and time.Location all have zero exported fields, so
// without the rule they look supported and are written nowhere. And a recursive
// type does not compile, because its address set is unbounded and a set that
// cannot be enumerated cannot be handed to a driver before any I/O. Both name
// registration as the fix, because a codec collapses a type to a leaf and a leaf
// needs no address set.
//
// # The composites whose addresses come from the value
//
// []T mints one Index segment per element and map[K]V one Name segment per key,
// and nothing about the type differs between two values that mint different
// address sets. So a schema can compile, pass every driver check, and be refused
// later because of what a map contained: both collision rules run at two points
// and neither is after a write - the static tier at schema compile with no value
// and no plane, and the dynamic tier as each address is minted, before the write
// it belongs to.
//
// A composite with no elements writes Null at its own address, whether it is nil
// or empty, and loads back to nil. Three Go states meet two observations at a
// container address and the collision is forced rather than chosen: measured
// through a real YAML plane, a missing key, an empty list and an empty mapping
// are one observation. The distinction between nil and empty is therefore not
// expressible by any type in the set - not by *[]T either, whose nil pointer and
// pointer to an empty slice are one address carrying one value - and a user who
// needs it models it as struct{ Set bool; Items []string }.
//
// Load reaches a dynamic address only through [Enumerator], which a [Reader] may
// implement and need not. It cannot be required, because a Vault token with read
// and no list is ordinary, and it cannot be omitted, because a map could then be
// loaded from no plane at all. So the two directions cover different address
// sets: Dump reaches every address always, since the value is in hand, and
// loading a slice or a map from a source that cannot list is an error naming the
// field and the source rather than a silently empty one. A Null at the
// container's own address is a complete answer and needs no enumeration.
//
// A type keys a map only if it is declared usable as one, per entry, and nothing
// else confers it - membership of the identity table included. The obligation is
// injectivity under Go's ==, because == is what a Go map's key identity is and
// therefore what decides how many entries the map holds. string and the integer
// kinds are admitted, and so is time.Duration; time.Time is refused, and the
// refusal is forced rather than chosen, because == compares its *Location and no
// text carries a pointer. Float keys are excluded because two distinct NaN
// payloads both format as NaN.
//
// A map's members are written in the order of their key text, which is ADR-0001's
// determinism invariant at the one place a Go map reaches a plane. Two members
// rendering to one address are refused as the address is minted, naming it,
// because there is no stable answer to give: which of the two writes survives is
// which the walk makes last.
//
// # The type set's sharp edges
//
// Three of these are not defects, and every one of them is easier to meet in
// production than to guess at from the rules above.
//
// A type admitted by kind gets a representation nobody chose. []byte and
// [N]byte are Bytes, so a [16]byte UUID lands in a YAML file as sixteen raw
// bytes. Value fidelity is not violated - it round-trips exactly - but
// legibility is, and no rule in core catches it.
//
// Two type identities are forced by Go rather than chosen: []byte is []uint8
// as one reflect.Type, so ferry cannot offer both a byte blob and a slice of
// small unsigned integers and picks Bytes; and []rune is []int32, so it is an
// indexed composite of numbers rather than text.
//
// A named type over time.Duration dumps nanoseconds. Such a type is a distinct
// reflect.Type, so it misses the identity table and falls to its kind. Matching
// on the underlying type instead would capture every ordinary `type Port int`,
// so the remedy is a registered codec rather than a wider rule.
//
// # What is refused, and what that costs
//
// chan, func, unsafe.Pointer and uintptr are refused permanently, because the
// value does not exist outside the process and no text could carry it.
// complex64 and complex128 are refused by policy rather than by constraint: no
// plane in ferry's range has a complex type. Everything else outside the set is
// a question of who supplies the codec. Every violation in a type is reported
// rather than the first one, each naming the address and the type, and the
// report is sorted.
//
// # How an address reaches a plane
//
// Core never produces a plane key, because a separator is plane knowledge:
// flattening is the driver's, always. What a driver is handed instead is the
// whole address set, before any I/O, and [NewKeys], which computes its plane
// keys once per schema and checks two different things about them. Legality is
// the driver's own question - whether its plane can name an address at all -
// and no transformation rescues it. Injectivity is core's observation about the
// set: a key function rendering two addresses to one plane key would merge them
// silently, so it is refused before any backend call, naming both. One rule
// covers separator collisions, case folding and any normalisation a driver
// invents, because all three are the same failure.
//
// So a driver is expected to transform segment text rather than to reject it,
// which is what makes an ordinary feature-flags loadable from a plane whose
// names may not contain a hyphen. An address a value mints - a map key, a
// sequence index - is checked as it is minted, before the write it belongs to,
// against the table and against everything the same open has minted. A tree
// driver walks the segments, builds no key at all, and calls none of this.
//
// # Errors
//
// ferry reports every failure that is not a consequence of another failure it
// is already reporting, so a failed call carries a set rather than the first
// thing that went wrong. Range it with [Elements], and match a member with
// errors.Is against [ErrSchema], [ErrMissing], [ErrValue], [ErrPlane],
// [ErrDriver] or [ErrReadOnly]. Read where it happened with
// errors.AsType[*ferry.Error] and [Error.Address]; there is no concrete type to
// switch on, and no enum.
//
// Message text is not API. Match on the sentinels and on the address rather
// than on a string, and get precision from the ferrytest assertions. ferry's own
// text never repeats a value the plane supplied - the cause stays in the chain
// and is never printed - so a plane that holds secrets does not leak them into
// a log through ferry (ADR-0011).
package ferry
