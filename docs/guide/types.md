# The supported type set

Which Go types ferry carries, what each one looks like on the plane, and the sharp edges that are easier to meet in production than to guess at from the rules.

The decision record is [ADR-0005](../adr/0005-the-supported-type-set.md), and where this guide and that ADR disagree, the ADR wins.

## How a type is claimed

A type is claimed by the first of three steps that will have it, and the claim is a pair: the same claim serves `Load` and `Dump`, so a type whose two directions would disagree is refused rather than dumped and never loaded (ADR-0007).

1. **Type identity.** `reflect.Type` compared with `==`, against one table holding core's own two entries and every codec anybody registered.
2. **The text pair.** `encoding.TextAppender` or `encoding.TextMarshaler`, together with `encoding.TextUnmarshaler`. Both halves are required.
3. **Kind admission.** `reflect.Kind`, against the table below.

The ordering is the whole rule.
`time.Duration`'s kind is `int64` and `time.Time`'s kind is `struct`, so a kind-first walk would write a nanosecond count for one and three unexported fields for the other.
`time.Time` also carries a text pair, and the identity table beats it, because an entry in that table is not replaceable.

A pointer type is never offered to any of the three as itself: pointer indirection is structural and is resolved first.

## The table

The **tier** column is which of the three [plane-compatibility tiers](compatibility.md) the member is in, which is the question "who chose this representation, and who promises it will not change".
Tier one is core's pinned set, tier two is a registrant's, and tier three is nobody's.

### Leaves ferry owns by type identity

| Go type | boundary kind | what lands on the plane | tier |
| --- | --- | --- | --- |
| `time.Duration` | `String` | `time.Duration.String()`, so thirty seconds is `30s` | **one, pinned** |
| `time.Time` | `String` | RFC 3339 with nanoseconds, `2026-08-02T12:00:00.123456789Z` | **one, pinned** |

Exactly two, and their representations are ferry's own.
`time.Duration` gets one where `encoding/json/v2` refuses outright and its legacy option gives nanoseconds, because for a mapper whose commonest application is configuration, `30s` is what people write (ADR-0005).

### Leaves admitted by kind

| Go type | boundary kind | what lands on the plane | tier |
| --- | --- | --- | --- |
| `bool` | `Bool` | `strconv.FormatBool`, so `true` and never `1` or `yes` | **one, pinned** |
| `string` | `String` | the bytes, unmodified, and not required to be UTF-8 | **one, pinned** |
| `int`, `int8`, `int16`, `int32`, `int64` | `Number` | `strconv.FormatInt`, base 10 | **one, pinned** |
| `uint`, `uint8`, `uint16`, `uint32`, `uint64` | `Number` | `strconv.FormatUint`, base 10 | **one, pinned** |
| `float32`, `float64` | `Number` | `strconv.FormatFloat(f, 'g', -1, bits)`, at the type's own bit size | **one, pinned** |
| `[]byte`, `[N]byte` | `Bytes` | the bytes, unmodified, and how the plane spells them is the driver's | **one, pinned** |
| a named type over any of the above | as its kind | as its kind: `type Port int` writes `number("8080")` | one for the representation, [three](#category-three-a-representation-nobody-chose) for the fit |

A named type over an admitted kind is admitted with it, so `type Port int` round-trips with nothing registered.
That is the same rule that puts a `[16]byte` UUID on the plane as sixteen raw bytes, which is the subject of [category three](#category-three-a-representation-nobody-chose) below.

### Composites

A composite is not itself a value except in the one case at the end.
It contributes addresses, and its elements are the leaves.

| Go type | segments minted | addresses come from | tier |
| --- | --- | --- | --- |
| `struct` | one `Name` per exported field | the type | its fields' |
| `*T` | none of its own | the type | its element's |
| `[N]T` | exactly `N` `Index` segments | **the type** | its element's |
| `[]T` | one `Index` per element | the value | its element's |
| `map[K]V` | one `Name` per key | the value | its element's |
| any composite with no elements | none | - | **one, pinned**: it writes `Null` at its own address |

Unexported fields are skipped.
That is a rule rather than a silence: `reflect` cannot set one, so the alternative is refusing every struct containing a `sync.Mutex`.

A composite that **can be nil** has an address of its own, and the test is exactly that: `*struct` gets one, a plain `struct` does not because it cannot be nil, and `[N]T` does not because an array has no nil.

A composite with no elements writes `Null` at its own address, whether it is nil or empty, and loads back to nil.
Three Go states meet two observations at a container address and the collision is forced rather than chosen: measured through a real YAML plane, a missing key, an empty list and an empty mapping are one observation (ADR-0005).
So the distinction between nil and empty is not expressible by any type in the set, `*[]T` included, and a user who needs it models it as `struct{ Set bool; Items []string }`.

### Types nobody in ferry chose a representation for

| how it is claimed | example | what lands on the plane | tier |
| --- | --- | --- | --- |
| the text pair | `net.IP`, `netip.Addr`, `netip.AddrPort`, `netip.Prefix`, `big.Int`, `slog.Level` | `String`, always: `string("192.0.2.1")`, `string("WARN")` | **three, nobody's** |
| a registered codec | whatever you registered | whatever your codec emits | **two, the registrant's** |
| kind admission, where the kind does not fit the type | `[16]byte` UUID, `json.RawMessage`, `[]rune` | raw bytes, raw bytes, one `Number` address per rune | **three, nobody's** |

The text arm lands as `String` **always**, because `encoding.TextMarshaler` produces text and says nothing about kind.
It runs before kind admission because a declaration beats an inference: `net.IP` lands as `"192.0.2.1"` rather than as sixteen raw bytes, `slog.Level` as `"WARN"` rather than `4`, and `netip.Addr`, `netip.AddrPort`, `netip.Prefix` and `big.Int` stop being refused for mapping no address.

A struct claimed by the text pair contributes one address rather than one per field, so it needs no tag on any of its fields.

**Half a pair does not compile**, in either direction, and the diagnosis names the method that is missing.
An `UnmarshalText` on a value receiver is a half pair too, because it decodes into a copy.
Neither `json.Marshaler`, `encoding.BinaryMarshaler` nor `gob.GobEncoder` is an arm, so a type carrying only one of those is admitted by its kind as usual.
`fmt.Stringer` is never consulted, in either direction, because `String() string` declares no inverse.

## The text arm's set is unbounded, and that is a boundary of the guarantee

The set of Go types implementing `encoding.TextMarshaler` is not enumerable and cannot be: `reflect` has no "all types implementing this interface" query, and could not have one, because the set depends on every package the consumer imports (ADR-0007).

Three consequences, and they are stated as a known boundary rather than as a footnote:

- **No completeness check can reach it.**
  `ferrytest.Complete` joins core's identity table, one representative type per admitted kind, and a registry.
  A type the text arm claims is in none of the three, so nothing reports that it has no proof.
- **Nobody promises its representation.**
  The bytes it writes come from its own author, at their release schedule, under whatever rule they had in mind.
  ferry cannot promise what it never chose, and a promise that quietly excluded most of what users actually store would be worse than saying so.
- **It round-trips under whatever relation its author had in mind**, rather than under one ferry can state.
  Measured over fourteen such types, three do not come back identical under `reflect.DeepEqual` after a text round trip, and none loses information under its own relation (ADR-0007).
  `net.ParseIP("192.0.2.1")` is 16 bytes and round-trips byte-exactly; a hand-built `net.IP{192,0,2,1}` is 4 bytes and comes back as 16.

If you want a promise about a text-pair type, [register a codec for it](#extending-the-set) and write the proof.
That moves it from tier three to tier two, where the golden column is yours and your CI holds you to it.

## Category three: a representation nobody chose

There are three outcomes for a type and not two (ADR-0005):

1. **In the set with a pinned representation.** Core's table, checked by the golden column.
2. **Refused at schema compile.** Loud, from the type alone.
3. **In the set by kind, with a representation nobody chose.**

Category three is the honest cost of admitting types by `reflect.Kind`.
The same rule that makes `type Port int` work for free writes a UUID into a file as sixteen raw bytes, because `[16]byte` is `Bytes`.

```go
type Config struct {
	ID [16]byte `ferry:"id"`
}
```

dumped through `driver/yaml` gives:

```yaml
id: !!binary VQ6EAOKbQdSnFkRmVUQAAA==
```

and loads back to exactly the same sixteen bytes.
**Value fidelity is not violated.**
What is violated is legibility: nothing in the file says `550e8400-e29b-41d4-a716-446655440000`, and nobody operating this service can read it or edit it.
ADR-0001 puts legibility of the plane on the driver's side of the line, so no rule in core catches it.

### The common members

A user who sees a type "work" has no reason to look further until they read the file, so the class is named and its members listed:

| type | what lands on the plane | what a reader probably expected |
| --- | --- | --- |
| a `[N]byte` UUID | `N` raw bytes, base64 in YAML, unreadable in a KV store | `550e8400-e29b-41d4-a716-446655440000` |
| any named `[]byte`, `json.RawMessage` included | the raw bytes | for `json.RawMessage`, the JSON text |
| `[]rune` | one `Number` address per rune, because `[]rune` **is** `[]int32` | one string |
| a named type over `time.Duration` | a nanosecond count, `number("30000000000")` | `30s` |
| a named type over an admitted kind, generally | its kind's representation | usually correct, and this is the case the rule exists for |

Two of those identities are forced by Go rather than chosen.
`[]byte` is `[]uint8` as one `reflect.Type`, so ferry cannot offer both a byte blob and a slice of small unsigned integers and picks `Bytes`.
And `[]rune` is `[]int32`, so it is an indexed composite of numbers rather than text, which is legal and is almost certainly not what you meant.

The named-duration case is the one with a one-line fix, because a named type is a distinct `reflect.Type` and misses the identity table:

```go
type Poll time.Duration

var registry = ferry.NewRegistry(ferry.DurationLike[Poll]())
```

Matching on the underlying type instead would capture every ordinary `type Port int`, which is why the remedy is `DurationLike` rather than a wider rule.

For the rest, the remedy is the same: register a codec, and the representation becomes yours at tier two.

## An array and a slice are not interchangeable

This is a real capability difference between two types a user will reasonably treat as the same.

An array's element addresses are known from `reflect.TypeFor[T]()` with no value in hand.
A slice's are not: `[]T` mints one `Index` segment per element, and nothing about the type differs between two values that mint different address sets.

**So an array loads from a source that cannot enumerate, and a slice does not.**

```go
type arrCfg struct{ Tags [3]string `ferry:"tags"` }
type sliceCfg struct{ Tags []string `ferry:"tags"` }
```

Against a source whose `Reader` does not implement `ferry.Enumerator`, holding `/tags#0` and `/tags#1`:

```
array   {Tags:[a b ]}    err=<nil>
slice   {Tags:[]}        err=ferry: /tags: the addresses under a []string come from the value,
                             and this reader cannot list what a plane holds under an
                             address: a source that does not implement ferry.Enumerator
                             reaches every static address and no dynamic one
```

Against a source that does enumerate, both load `[a b]`.

`Enumerator` is optional in both directions and neither answer was available (ADR-0004).
It cannot be required, because a Vault kv-v2 `LIST` is a separate ACL capability and a token with read and no list is ordinary.
It cannot be omitted, because a map could then be loaded from no plane at all.
So the two directions cover different address sets: `Dump` reaches every address always, since the value is in hand, and loading a slice or a map from a source that cannot list is an error naming the field and the source rather than a silently empty one.

That is not a thing to discover against a Vault token with no `LIST`, which is why it is here rather than only in the address model.

Three more consequences of the same split:

- An array needs no empty-container marker and has no nil form, so none of the nil-versus-empty reasoning above applies to it.
- An absent array element leaves the element at its zero value, exactly as an absent struct field does.
  An index the array cannot hold is loud: `[3]string` given index 7 returns `ferry: /V: plane has index 7, [3]string holds 3`.
- An array element is a static address, so it takes its declared `default=` with nothing on the plane.
  A slice element in the same position does not exist at all.

`encoding/json/v2` is stricter here, refusing a short array outright and offering a legacy escape.
ferry does not follow it, because an absent address leaving a zero value is the rule ferry applies everywhere else.

## `time.Time` loses its zone's rules, which is worse than losing its name

`time.Time` is in the set, its representation is RFC 3339 with nanoseconds, and its equality relation is `time.Time.Equal` and not `==`.
That is not a carve-out ferry invented: the standard library asks for it by name, because `==` also compares the monotonic reading and the `*Location` pointer.

RFC 3339 carries the **offset** and not the zone identity, so the zone name cannot survive.
The instant, the wall-clock reading, the nanoseconds and the offset all survive.
**What is destroyed is the zone's rules**, so the loaded value is a fixed-offset zone that does not know DST exists.

Measured on a July value in `Europe/London`, round-tripped through a plane, then advanced six months into GMT:

```
original       2026-07-15 09:30:00 +0100 BST    Location "Europe/London"
loaded         2026-07-15 09:30:00 +0100        Location ""
Equal          true
==             false

original.AddDate(0, 6, 0)   2027-01-15 09:30:00 +0000 GMT
loaded.AddDate(0, 6, 0)     2027-01-15 09:30:00 +0100
```

**A stored timestamp is unaffected. A stored "when to run next" is wrong by an hour, half the year.**

### And the loaded `Location` is machine-dependent

`time.Parse` is documented to return `Local` when the offset happens to match the reading machine's own local zone, and a fabricated fixed-offset location otherwise.
Loading the same document on two machines:

```
TZ unset or TZ=UTC        Location ""              +6mo -> 09:30 +0100   (wrong)
TZ=Europe/London          Location "Europe/London" +6mo -> 09:30 +0000   (right)
```

So two machines loading one plane get `time.Time` values that are `.Equal` and not `==`, whose `Location` differs, and whose arithmetic across a DST transition disagrees.
"The same plane loads to the same value everywhere" is not true for `time.Time`, and a reader would assume it is.

### This is inherited, not chosen

`encoding/json/v2` does exactly the same thing, measured side by side on `go1.27rc2`.
It has precisely two time-related options in the whole package set, `FormatDurationAsNano` and `ParseTimeWithLooseRFC3339`, and neither touches zones.
The `format:` tag option that could have specified a zone-preserving layout was removed from the supported set in 1.27 and now errors (ADR-0005).

**There is no zone-preserving option in the standard library to adopt.**
ferry is not choosing a worse answer than json/v2; it is inheriting the only answer RFC 3339 permits.

### The guidance

**A `time.Time` crossing a ferry plane should be UTC.**
`time.UTC` costs nothing: it round-trips under `==`, `Location` included.
A user who needs zone identity stores it as a second field, or registers a codec.

One more edge: `time.Time` has values with no text form at all.
`MarshalText` errors for a year outside `[0,9999]`, so the representation is partial over the type, and the error surfaces rather than being swallowed.

## What a plane may hand back

On `Load` every leaf accepts its own kind, and additionally accepts `String`, whose text is parsed by exactly the parser that leaf's own kind uses.

**Nothing else coerces.**
`String` is what a plane says when it has nothing to say, while `Number`, `Bool` and `Bytes` are assertions a plane made and ferry respects.
So a `Number` is refused at a Go `string`, because accepting it would destroy the quoting distinction the boundary preserves.

That one donation is what makes a flat plane work at all: an environment variable, a query parameter and an opaque KV value are all `String`, and two of the three drivers in this repository are that shape.

`Null` is accepted by exactly the types that have a null, which among the leaves is `[]byte` alone, plus `*T`, `[]T` and `map[K]V` among the composites.
Every other leaf refuses it as a wrong kind.
See [tags, defaults and absence](tags.md) for the full per-kind table.

## Map keys

A type keys a map only if it is **declared** usable as one, per entry, and nothing else confers it - membership of the identity table included.

The obligation is injectivity under Go's `==`, because `==` is what a Go map's key identity is and therefore what decides how many entries the map holds.

- `string` and the integer kinds are admitted.
- `time.Duration` is admitted, and is one of only two rows core can actually prove: no collision over 2^20 random values plus the extremes (ADR-0005).
- **`time.Time` is refused**, and the refusal is forced rather than chosen: `==` compares its `*Location` and no text carries a pointer.
  `time.UTC` and `FixedZone("GMT", 0)` are two distinct Go keys and one address.
- Float keys are excluded because two distinct `NaN` payloads both format as `NaN`.
- A type the text arm claims may **not** key a map, and the reversal is because nobody can be asked rather than because the answer would be no: a registration has a call site at which the obligation is declared and a text pair does not.
  Register it with `StringText`, `NumberText`, `StringKey` or `NumberKey` and say `.AsMapKey()`.

A map's members are written in the order of their key text, which is ferry's determinism invariant at the one place a Go map reaches a plane.
Two members rendering to one address are refused as the address is minted, naming it, because there is no stable answer to give.

## What is refused

Only four Go kinds are refused permanently, because the value does not exist outside the process and no text could carry it:

```
chan    func    unsafe.Pointer    uintptr
```

`complex64` and `complex128` are refused by policy rather than by constraint: no plane in ferry's range has a complex type, and registering a codec lifts it if yours does.

Two whole-type refusals fall out of the address model, and both name registration as the fix:

- **A struct that maps no address does not compile**, checked at every level rather than only at the root.
  `time.Location`, `netip.Addr`, `netip.AddrPort` and `big.Int` all have zero exported fields, so without the rule they look supported and are written nowhere.
  Measured before the rule existed: dumping `netip.MustParseAddr("192.0.2.1")` produced 0 addresses and a nil error (ADR-0005).
  Three of those four are now claimed by the text arm and never reach this rule.
- **A recursive type does not compile**, because its address set is unbounded and a set that cannot be enumerated cannot be handed to a driver before any I/O.

Every violation in a type is reported rather than the first one, each naming the address and the type, and the report is sorted.
`ferry.Compile[T]()` asks the question with no plane in sight.

## Extending the set

The type set is closed and its extension is explicit.
A registered codec claims a type ferry does not own, and the guarantee about that type transfers to whoever registered it.
Registering without proving is permitted and forfeits the guarantee.

```go
var registry = ferry.NewRegistry(
	ferry.StringText[netip.AddrPort]().AsMapKey(),
	ferry.DurationLike[PollInterval](),
	ferry.NumberValue(encodeBigInt, decodeBigInt),
)
```

A registration is named after the kind it writes, so there is no kind argument, and its halves are typed by payload, so a registrant never builds a `Value`:

| constructor | you supply | the kind it writes |
| --- | --- | --- |
| `BoolValue(enc, dec)` | two functions over `bool` | `Bool` |
| `NumberValue(enc, dec)` | two functions over `string` | `Number` |
| `StringValue(enc, dec)` | two functions over `string` | `String` |
| `BytesValue(enc, dec)` | two functions over `[]byte` | `Bytes` |
| `NumberKey(enc, dec)`, `StringKey(enc, dec)` | the same, and `.AsMapKey()` | `Number`, `String` |
| `NumberText[T]()`, `StringText[T]()` | nothing; the type carries a text pair | `Number`, `String` |

`NumberText` and `StringText` are not for rescuing a type - the chain already claims any type with a text pair - they are for changing its kind, and for saying `.AsMapKey()`.
`DurationLike[T ~int64]()` closes the named-duration hole at one line per type.
`NullValue(inner, load, isNull)` is the one modifier, and it is what accepts a `Null` into a Go type whose kind has no null; its law is that `isNull(load())` holds.

Only `NumberKey`, `StringKey`, `NumberText` and `StringText` return the type carrying `.AsMapKey()`, so a bytes-keyed map is a build error rather than a refusal.

Each constructor takes both halves at once, so a half pair, two halves swapped and two halves over different types are build errors rather than run-time refusals.
A nil half panics at the constructor.

`NewRegistry` runs each codec against the zero value of its type before accepting it.
That catches one class of wrong codec out of three, and it is the class that matters: the one-line registration a user is most likely to write is not an inverse at the zero value for `netip.Addr`, `netip.AddrPort` and `netip.Prefix`.
What it does not catch is a lossy codec and a constant codec, and those are what a proof in `ferrytest` is for.

A registry is complete when it is built.
There are no mutators, so there is no window in which one is reachable and incomplete and no ordering rule between building it and using it, and every refusal above is a panic at construction rather than an error on a line nobody checks.
Core's own type set is always underneath, so registering one type never costs you `string`, `int`, `bool` or `time.Duration`; a codec claiming a type core owns is refused like any duplicate.

### Proving your codec

Three calls, and a green `Codec` on its own is not enough:

```go
func TestMyCodecs(t *testing.T) {
	reg := ferry.NewRegistry(ferry.StringText[netip.Addr]().AsMapKey())

	proofs := append(ferrytest.CoreTypes(), ferrytest.Type("netip.Addr", ferrytest.Eq[netip.Addr],
		ferrytest.At(netip.MustParseAddr("192.0.2.1"), ferry.String("192.0.2.1")),
	))

	ferrytest.RoundTrip(t, ferrytest.MemPlane(), proofs, ferry.WithRegistry(reg))
	ferrytest.Codec(t, reg)

	for _, s := range ferrytest.Complete(reg, proofs...) {
		t.Errorf("registry: %s", s)
	}

	for _, s := range ferrytest.Injective(reg,
		netip.MustParseAddr("192.0.2.1"),
		netip.MustParseAddr("::ffff:192.0.2.1"),
	) {
		t.Errorf("as a key: %s", s)
	}
}
```

- `RoundTrip` drives your values through the real engine, dumping, comparing against your golden, loading back and comparing under your own relation.
- `Codec` checks the registration machinery and each codec at its zero value, and nothing beyond.
  A lossy codec and a constant codec pass every case in it, and so does a null policy whose two halves disagree.
- `Complete` reports every type the registry holds that your proofs do not discharge.
- `Injective` reports every pair of the values **you** name that ferry writes to one map key, which is the obligation `.AsMapKey()` declares and nothing core can check.

The third column of a proof - the golden - is the one that pins the representation.
A round trip tests a function against its own inverse, so changing both halves together is invisible to it.
Measured: with `time.Duration`'s codec replaced by one writing `30000000000`, the round-trip property alone reports zero failures (ADR-0005).

## Where the promise ends

The table above is nineteen rows and 57 cases in executable form, as `ferrytest.CoreTypes()` (ADR-0014), and it covers all eighteen members core admits.
**The promise is exactly as wide as that table.**
An admitted member with no row is outside the promise by accident rather than by decision, which is why `ferrytest.Complete` joins the table against core's own set rather than trusting the count.

What that promise is, what a change to it costs, and who owns each tier is [plane compatibility](compatibility.md).
