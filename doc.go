// Package ferry is a bidirectional, struct-first data mapper. One annotated
// struct and one tag grammar drive both directions: [Load] fills a value from
// a pluggable source, and [Dump] writes the same value back to a pluggable
// sink.
//
//	type Config struct {
//	    Host    string        `ferry:"host,required"`
//	    Port    int           `ferry:"port,default=8080"`
//	    Timeout time.Duration `ferry:"timeout,default=30s"`
//	}
//
//	cfg, err := ferry.Load[Config](ctx, yaml.Source{Path: "app.yaml"})
//
// The store a value is read from or written to is called a plane: a YAML file,
// the process environment, a KV bucket, a query string. Core knows nothing
// about any of them, and reaches one only through [Source] and [Sink]. Planes
// ship as separate modules under driver/.
//
// # The six verbs
//
//   - [Load] builds a fresh value of T from a source.
//   - [LoadOver] does the same over a seed the caller supplies, which is how a
//     reload and a composite default are spelled.
//   - [Dump] writes a value to a sink.
//   - [Compile] reports whether a type's annotation is legal, from the type
//     alone, with no value in hand and no plane reachable. It is what a test
//     calls.
//   - [Bind] hands a source the addresses a type names, once, and returns a
//     value to load through many times. [Load] is [Bind] plus one method.
//   - [BindSink] is the same on the write side, and [Dump] is [BindSink] plus
//     one method.
//
// Every verb takes the same [Option] values, and there are three of those:
// [TagKey] names the struct tag key to read, [WithRegistry] names the codec
// table to resolve types against, and [MaxConcurrency] allows a load to overlap
// its calls into a plane that said it tolerates overlap.
//
// # The tag grammar
//
// Four words, and one of them is punctuation:
//
//	tag     =  name *( "," option )  /  "-"
//	option  =  "required"  /  "omitzero"  /  "default" "=" token
//	token   =  bare  /  "'" quoted "'"
//
//	Host     string `ferry:"host,required"`
//	Port     int    `ferry:"port,default=8080"`
//	Comment  string `ferry:"comment,omitzero"`
//	Greeting string `ferry:"greeting,default='Hello, world'"`
//	Odd      string `ferry:"'a,b'"`
//	Skipped  string `ferry:"-"`
//
// Every exported field names the segment it addresses, or is marked "-". ferry
// never invents a name out of the Go field name, so exporting a field cannot
// silently change what a program writes to a plane.
//
// A name or a default value containing a comma is single-quoted, and a literal
// quote inside a quoted token is doubled. Only a leading quote is significant,
// so default=it's here needs no quoting at all. A word ferry does not have is a
// refusal rather than a silent no-op, and the diagnostic names what to write in
// its place.
//
// Each option has exactly one honest direction, so the grammar spends no syntax
// on saying which: default= and required are Load-side, omitzero is Dump-side.
//
// # The type set
//
// A type is claimed by the first of three steps that will have it, and the
// claim serves both directions, so a type whose two directions would disagree
// is refused rather than dumped and never loaded.
//
//  1. Type identity: a registered codec, or one of core's own two pinned types.
//  2. The text pair: encoding.TextAppender or encoding.TextMarshaler, together
//     with encoding.TextUnmarshaler.
//  3. reflect.Kind admission.
//
// Core pins two types by identity, and their representations do not change:
// time.Duration is a string such as "30s", and time.Time is RFC 3339 with
// nanoseconds.
//
// A type declaring both halves of the text pair is claimed by it and lands as a
// string, so net.IP is "192.0.2.1" rather than sixteen raw bytes and slog.Level
// is "WARN" rather than 4. Half a pair does not compile, and the diagnostic
// names the method that is missing; an UnmarshalText on a value receiver is a
// half pair too, because it decodes into a copy. Nothing else is consulted -
// not json.Marshaler, encoding.BinaryMarshaler or gob.GobEncoder, and not
// fmt.Stringer, which declares no inverse.
//
// Admitted by kind: bool, string, the five signed and five unsigned integer
// widths, float32 and float64, and []byte and [N]byte as bytes. A named type
// over an admitted kind is admitted with it, so `type Port int` round-trips
// with nothing registered.
//
// Composites contribute addresses rather than values. A struct mints one name
// segment per exported field, and unexported fields are skipped; a pointer
// mints no segment of its own; [N]T mints exactly N indices, because the length
// is part of the type; []T mints one index per element and map[K]V one name per
// key, both from the value rather than from the type.
//
// A map is keyed by a string or an integer kind, by time.Duration, or by a
// registered type whose registration declared [KeyCodec.AsMapKey]. Nothing else
// keys a map, because the key becomes address text and has to parse back out of
// it.
//
// chan, func, complex64, complex128, unsafe.Pointer and uintptr are refused. So
// is a struct that maps no address, and so is a recursive type, whose address
// set is unbounded; registering a codec collapses either to a leaf and is the
// remedy for both. Every violation in a type is reported rather than the first,
// each naming the address and the type, sorted.
//
// On Load a leaf accepts its own kind, and additionally accepts a string, whose
// text is parsed by exactly the parser that leaf's own kind uses. Nothing else
// coerces. So "0080" is 80 at an int field and never 0, "yes" is not a bool,
// and a plane's number is refused at a Go string field, which is what keeps a
// quoted 8080 and an unquoted one distinguishable across a round trip.
//
// # Sharp edges
//
// None of these is a defect, and every one is easier to meet in production than
// to guess at from the rules above.
//
// A time crossing a plane should be UTC. RFC 3339 carries the offset and not
// the zone identity, so a time.Time that is not UTC loses its zone's DST rules:
// a stored timestamp is unaffected, but a stored "when to run next" is wrong by
// an hour for half the year. The Location a load produces is machine-dependent
// as well, so two machines can load values that are .Equal and not ==.
// encoding/json/v2 does exactly the same thing and has no zone-preserving
// option, so this is inherited from RFC 3339 rather than chosen.
//
// An array and a slice are not interchangeable. An array's element addresses
// are known from the type, so an array loads from a source that cannot
// enumerate and a slice does not. See [Enumerator].
//
// A type admitted by kind gets a representation nobody chose. A [16]byte UUID
// lands in a YAML file as sixteen raw bytes: it round-trips exactly, it is
// simply illegible. Register a codec for a type whose stored spelling matters.
//
// []byte is []uint8 and []rune is []int32, one reflect.Type each, so ferry
// cannot tell a byte blob from a slice of small unsigned integers and picks
// bytes, and []rune is an indexed sequence of numbers rather than text.
//
// A named type over time.Duration dumps nanoseconds, because it is a distinct
// reflect.Type and falls through to its kind. [DurationLike] is the one-line
// remedy.
//
// A type claimed by the text pair may not key a map. Its text may well be
// injective, but nobody was asked, and a registration is the only place that
// declaration can live.
//
// default=aGk= on a []byte field lands as the four bytes aGk= and not the
// decoded hi. A declared default is text, and how a plane spells bytes is the
// driver's business rather than ferry's. Register a codec, or seed the value
// through [LoadOver].
//
// Message text is not API. Match on the sentinels and on the address.
//
// # Absence, defaults and zero values
//
// One rule carries all of it: absent means ferry does not write to the field.
// Every other observation, a null and the empty string included, is a value the
// plane holds and is handed to the type set, which accepts it or refuses it
// loudly. So a [LoadOver] against an empty plane leaves the seed untouched, and
// an explicit empty beats whatever the field was already carrying.
//
// A null is presence carrying a value, not a second spelling of absence.
// []byte, *T, []T and map[K]V take it and land on their own nil; every other
// leaf refuses it as a wrong kind. Nothing is zeroed silently.
//
// A struct merges and a composite replaces: a struct's fields are separate
// addresses, so the ones the plane does not have are left alone, while a slice
// or a map the plane has any children under is replaced wholesale. A *T at a
// leaf is the one shape that tells an explicit zero from an unset field.
//
// A declared default is text, applied when and only when the plane reports
// absence, and decoded by the field's own parser, so "0080" means 80 in a tag
// exactly as it does from a plane. It is leaf-only; a composite default is
// spelled by seeding [LoadOver].
//
// required is a presence test and nothing else, so an explicit empty satisfies
// it. It is not admissible on a slice or a map, where a missing key and an
// explicit empty list are one observation at a container address.
//
// omitzero compares against the Go zero value, before anything converts it, and
// is admissible at every type. A field holding its declared default is dumped
// like any other.
//
// A composite with no elements writes a null at its own address, whether it is
// nil or empty, and loads back to nil. The nil-versus-empty distinction is not
// expressible by any type in the set; a user who needs it models it as
// struct{ Set bool; Items []string }.
//
// # Registration
//
// The type set is closed, and its extension is explicit. A registered codec
// claims a type ferry does not own, in both directions at once, and the
// guarantee about that type transfers to whoever registered it:
//
//	var registry = ferry.MustRegistry(
//	    ferry.NumberText[big.Int](),
//	    ferry.DurationLike[PollInterval](),
//	)
//
// A registration is named after the kind it writes, so it takes no kind
// argument, and its halves are typed by the payload that kind carries, so a
// registrant never builds a [Value]. [BoolValue], [NumberValue], [StringValue]
// and [BytesValue] take two functions each; [NumberKey] and [StringKey] are the
// two whose kind may key a map, and they alone carry [KeyCodec.AsMapKey];
// [NumberText] and [StringText] take no functions, for a type that already
// carries a text pair and wants a different boundary kind, and they carry
// AsMapKey too. [DurationLike] closes the named-duration hole at one line per
// type, and [NullValue] is the one modifier: it says what a plane's null becomes
// and which values write one back.
//
// [NewRegistry] is the whole registry API and [WithRegistry] names a registry
// for one call. A registry takes its whole codec set at construction and has no
// mutators, so it is complete when it is born and there is no ordering rule
// between building it and using it, and every refusal a constructor found along
// the way is reported there. Core's own type set is always underneath, and a
// codec claiming a type core owns is refused like any duplicate. [MustRegistry]
// is the same constructor for the package-level var a refusal cannot be checked
// on, and it panics.
//
// A registration claims its type unconditionally: there is no decline, and "fall
// through to the next step" is spelled by not registering the type.
//
// A registry also holds the compiled-schema cache, and nothing evicts from it,
// so a registry is a value to keep. Build one per program, or one per test.
//
// # Tag key extensions
//
// ferry's own tag vocabulary is closed, and a library built on ferry gets a
// struct tag key of its own instead. [WithTagKeys] declares one on a registry,
// beside the codecs:
//
//	var registry = ferry.MustRegistry(
//	    ferry.WithTagKeys(ferry.KeyExtension{
//	        TagKey: "docs",
//	        Words:  []ferry.Word{{Name: "desc", TakesValue: true}},
//	    }),
//	)
//
//	Host string `ferry:"host,required" docs:"desc=where the service lives"`
//
// A declared key is parsed with that declaration's vocabulary, with the same
// near-miss diagnostics ferry gives its own words, and handed back inert: core
// validates and never acts. An undeclared key is another library's business and
// is never claimed, so a json or a validate tag on the same field is untouched.
// Nothing is added to what ferry's own tag accepts, and ferry:"host,docs.desc=x"
// is refused exactly as it was.
//
// The words ride the [AddressSet], keyed by the address the field named, so a
// driver reads its own key at its own Bind with [AddressSet.Extension] and a
// caller plumbs nothing. A consumer that never meets a plane reads the same
// table with [ExtensionTable].
//
// # Addresses
//
// Every place a plane holds something has an address: a [Path], an ordered
// sequence of segments each carrying a kind and a text. /db/host is two name
// segments, /tags#0 is a name segment followed by an index.
//
// An address also carries what kind of place it names, and the three kinds are
// separate types. A [LeafAddr] is a place a [Value] can be, a [SectionAddr] is
// a place whose children are known from the type, and a [CompositeAddr] is a
// place whose children come from the value. They partition the address space
// and they are not interchangeable, so /db as a section and /db as a composite
// are different addresses.
//
// Only ferry mints one, which is what puts the wrong question out of reach: a
// driver's [Reader.Get] takes a leaf, [Prober.Probe] takes a container and
// [Enumerator.Children] takes a composite, so asking a plane for the value of a
// section is a compile error rather than something to guard against at run
// time.
//
// Core never joins segments into a plane key, because a separator is plane
// knowledge. A driver is handed the whole [AddressSet] before any I/O and does
// the flattening itself, classifying once with one range over [AddressSet.Seq]
// and one type switch. [NewKeys] is the helper for that, and it checks two
// things about the result: that the plane can name every address, and that no
// two addresses collapse onto one plane key. Both refusals land before any
// backend call.
//
// # Errors
//
// A failed call carries a set rather than the first thing that went wrong.
// Range it with [Elements], and match a member with errors.Is against
// [ErrSchema], [ErrMissing], [ErrValue], [ErrPlane], [ErrDriver] or
// [ErrReadOnly]. Read where it happened with errors.AsType[*ferry.Error] and
// [Error.Address]:
//
//	for _, e := range ferry.Elements(err) {
//	    if fe, ok := errors.AsType[*ferry.Error](e); ok {
//	        log.Println(fe.Address(), errors.Is(fe, ferry.ErrMissing))
//	    }
//	}
//
// Message text is not API. Match on the sentinels and on the address, and get
// exactness from the assertions ferrytest ships. ferry's own text never repeats
// a value the plane supplied, so a plane holding secrets does not leak them
// into a log through ferry.
//
// On failure [Load] returns the zero value and [LoadOver] returns the seed it
// was handed; neither ever yields a partly built value. On Dump every value is
// encoded before any of them is written, so a dump that fails for a reason
// ferry could have known without touching the plane leaves the plane untouched.
//
// # Compatibility
//
// Two promises, and they are not the same one. The API is ordinary semver, at
// v0 today. What a plane holds is a second promise with three tiers: the
// representation of a type in core's own set is promised at core's major
// version; a registered codec's representation is its registrant's, at their
// major version; and a type admitted by kind or claimed by the text pair has a
// representation nobody chose and nobody promises. That third tier is large,
// and the text pair's half of it cannot even be enumerated, since any type in
// any module may declare one.
//
// A change to a pinned representation is a major version of the module that
// owns it, and the new ferry cannot read what the old one wrote. The migration
// is a few lines of ordinary ferry code, and it terminates, because the new
// codec refuses the old file afterwards.
//
// The design records behind these decisions are in docs/adr/.
package ferry
