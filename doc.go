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
// A type is claimed by the first of three steps that will have it, and the
// claim is a pair: the same claim serves Load and Dump, so a type whose two
// directions would disagree is refused rather than dumped and never loaded.
//
//  1. type identity, reflect.Type compared with == - a registration and core's
//     own two entries, in one table
//  2. the text pair, encoding.TextAppender or encoding.TextMarshaler together
//     with encoding.TextUnmarshaler
//  3. reflect.Kind admission
//
// The ordering is the whole rule. time.Duration's kind is int64 and time.Time's
// kind is struct, so a kind-first walk would write a nanosecond count for one
// and three unexported fields for the other; time.Time also carries a text
// pair, and the table beats it, because an entry in the table is not
// replaceable.
//
// Two leaves are owned by identity, and their representations are pinned:
// time.Duration is a string such as "30s", and time.Time is RFC 3339 with
// nanoseconds through its text pair. ferry gives time.Duration a representation
// where encoding/json/v2 refuses and its legacy option gives nanoseconds, and
// says so rather than claiming to follow v2.
//
// Claimed by the text pair: any type declaring both halves of it, which is a
// declaration ferry did not choose and which the type's author is therefore
// answerable for. It lands as String, always, because encoding.TextMarshaler
// produces text and says nothing about kind. This step runs before kind
// admission because a declaration beats an inference: net.IP lands as
// "192.0.2.1" rather than as sixteen raw bytes, slog.Level as "WARN" rather
// than 4, and netip.Addr, netip.AddrPort, netip.Prefix and big.Int stop being
// refused for mapping no address. A struct claimed here contributes one address
// rather than one per field, so it needs no tag on any field of it.
//
// Half a pair does not compile, in either direction, and the diagnosis names
// the method that is missing. Using the half anyway is a value that dumps and
// never loads; falling through to kind admission ignores, with no diagnostic,
// a method the user wrote for exactly this purpose. An UnmarshalText on a value
// receiver is a half pair too, because it decodes into a copy. Neither
// json.Marshaler, encoding.BinaryMarshaler nor gob.GobEncoder is an arm, so a
// type carrying only one of those is admitted by its kind as usual.
//
// A type the chain claims may not key a map. It is not that its text is lossy -
// every such type in the standard library is injective - it is that nobody was
// asked: a registration has a call site at which the obligation is declared and
// a text pair does not.
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
// not compile, checked at every level rather than only at the root:
// time.Location has zero exported fields, so without the rule it looks
// supported and is written nowhere. And a recursive
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
// so the remedy is [DurationLike] rather than a wider rule.
//
// # Registration
//
// The type set is closed and its extension is explicit: a registered codec
// claims a type ferry does not own, and the guarantee about that type transfers
// to whoever registered it. Registering without proving is permitted and
// forfeits the guarantee.
//
//	func init() {
//	    if err := ferry.Register(
//	        ferry.TextCodec[netip.AddrPort](ferry.KindString),
//	        ferry.DurationLike[PollInterval](),
//	        ferry.ValueCodec(ferry.KindNumber, encodeBigInt, decodeBigInt),
//	    ); err != nil {
//	        panic(err)
//	    }
//	}
//
// There are three constructors and they differ by what the registrant hands
// over and by nothing else. [TextCodec] takes a kind and no functions, because
// both halves come from the type; its purpose is changing the kind rather than
// rescuing the type, since the chain already claims any type with a text pair.
// [StringCodec] takes two functions over string. [ValueCodec] takes a kind and
// two functions over [Value], and it is the only one whose decode half sees the
// whole Value - which is the only way to accept a Null into a Go type whose
// kind has no null, and the escape hatch strictness rests on. [DurationLike]
// closes the named-duration hole at one line per type.
//
// Each takes both halves at once, so a half pair, two halves swapped and two
// halves over different types are build errors rather than run-time refusals.
// Inference works at every call site with a value argument, so no explicit type
// argument is written except for [TextCodec] and [DurationLike], which have
// nothing to infer from.
//
// [Register] runs the codec against the zero value of its type before accepting
// it. That catches one class of wrong codec out of four, and it is the class
// that matters: the one-line registration a user is most likely to write is not
// an inverse at the zero value for netip.Addr, netip.AddrPort and netip.Prefix,
// and since a registration beats the text pair those types already carry,
// registering it makes the type worse than leaving it alone. What it does not
// catch - a lossy codec, a constant codec, and a codec that declares the wrong
// kind - is what a proof in ferrytest is for.
//
// A registration goes into a [Registry], which is a value. It freezes at its
// first retained schema compile, so nothing can be registered after the first
// [Load] or [Dump]; [Compile] retains no resolution and does not freeze. Core
// ships a default registry and [Register] writes to it, and [WithRegistry]
// names another. A registration claims its type unconditionally: there is no
// decline, and "fall through to the next step" is spelled by not registering
// the type. A registered type keys a map only if the registration says
// .AsMapKey(), because a key codec's text has to be injective and nobody else
// can be asked.
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
