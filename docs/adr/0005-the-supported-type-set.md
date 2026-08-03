# 5. The supported type set, and how round-trip is enforced

Status: Accepted
Date: 2026-08-02
Ticket: [#7](https://github.com/onhotpath/ferry/issues/7)

## Context

[ADR-0001](0001-what-ferry-supports.md) made round-trip fidelity a hard guarantee over the set core ships, split it into value fidelity and driver fidelity, and ruled that a type outside the set is a loud error at schema compile rather than a silent lossy dump.
It named this ticket as the one that enumerates the set.

Three later ADRs each left a piece of that enumeration here by name.
[ADR-0002](0002-core-and-sub-modules.md) barred `encoding/json/v2` and `jsontext` from core, and observed that a property harness over a closed enumerated set is a table rather than a generator.
[ADR-0003](0003-how-a-leaf-addresses-a-plane.md) reserved the `Index` segment kind and left "whether any Go type produces one" here.
[ADR-0004](0004-source-and-sink.md) fixed the boundary value at a comparable 24-byte `{kind, text}` with kinds `Absent`, `Null`, `Bool`, `Number`, `String`, `Bytes`, and left "which Go types map onto which kinds" here.

[#18](https://github.com/onhotpath/ferry/issues/18) settled that first-class `encoding/json/v2` support means an explicitly pinned option set and never inherited defaults, and put the pinning in this ADR because this is where the round-trip guarantee is either kept or lost.

This ADR is written from a throwaway prototype on branch `proto/7-type-set`, which never merges.
It is built on `proto/5-source-sink`, so every measurement below runs against a real `Path`, a real `Value` and a real YAML plane over real files rather than against a mock.
Every number is from that prototype unless it cites the survey.

**Seven of the fourteen probes overturned an answer this ADR had already reached in draft**, and every one was found by testing a case the earlier fixtures did not contain.
That is recorded because the shape of each miss is the argument for the harness design in the second half.

## Decision

### What this closes, and what it does not

The ticket asked for four things by name.
This table is the answer to each, so a reader can check the ADR against the ask without reading the rest of it.

| The ticket asked | Closed | Where |
| --- | --- | --- |
| the enumerated set of Go types core supports | **yes** | [The enumerated set](#the-enumerated-set), and the key set is amended by [#31](https://github.com/onhotpath/ferry/issues/31) in [The map key rule, restated](#the-map-key-rule-restated) |
| what happens to a type outside it, at what point it is detected | **yes**, at schema compile from `reflect.TypeFor[T]()` alone | [A type outside the set](#a-type-outside-the-set-is-refused-at-schema-compile-and-every-violation-is-reported) |
| how round-trip is enforced in CI | **yes**, one table run against three planes plus a completeness check | [How round-trip is enforced](#how-round-trip-is-enforced) |
| what "adding a type means adding its proof" looks like concretely | **yes**, a proof is a triple and none of the three is derivable from the others | [A proof is a triple](#a-proof-is-a-triple-because-the-property-alone-is-blind-to-representation) |
| the pinned `encoding/json/v2` option set | **yes**, all 37 options enumerated from source | [The pinned option set](#the-pinned-encodingjsonv2-option-set) |
| float precision | **yes** | [The named hazards](#the-named-hazards-each-resolved) |
| `time.Time` and its monotonic clock | **yes**, with two losses stated rather than implied | as above |
| map ordering | **yes** | as above |
| `[]byte` | **yes**, and the answer is forced rather than chosen | as above |
| `time.Duration` | **yes**, and ferry departs from json/v2 deliberately | as above |
| whether a `fmt.Stringer` fallback is ever safe | **yes: never**, for two independent measured reasons | as above |

Four questions this ADR had to answer that the ticket did not name, all of which came out of the prototype:

| Not asked for, answered anyway | Where |
| --- | --- |
| which `Value` kinds a Go type accepts on **Load**, which decides whether env, query and kv work at all | [String is the universal donor](#on-load-string-is-the-universal-donor-and-nothing-else-coerces) |
| what stops a struct admitted by kind from being a silent total loss | [A struct that maps no address](#a-struct-that-maps-no-address-does-not-compile) |
| whether a recursive type has a finite address set | [A recursive type](#a-recursive-type-does-not-compile) |
| whether an array is a static or a dynamic composite | [The enumerated set](#the-enumerated-set) |
| what actually limits each refusal, and which are permanent | [Every refusal is one of three kinds](#every-refusal-is-one-of-three-kinds-and-only-one-of-them-is-permanent) |

**Three things this ADR does not close, stated here rather than left to the reader to notice.**

- **A nil pointer cannot round-trip through a plane with no null.**
  Measured: the composites pass 10 of 10 through the memory plane and the YAML driver, and 10 of 10 through a plane that reports `String` for everything, with **13 values refused** by that plane's own declaration.
  *(Amended under [#41](https://github.com/onhotpath/ferry/issues/41). As published this read "8 of 10 ... the two failures being `*int` and `*Cred` at nil". See [How round-trip is enforced](#how-round-trip-is-enforced) for what moved and why.)*
  This is driver fidelity rather than value fidelity, so ADR-0001 already puts it on the other side of the line, but it means core's guarantee is stated over the memory plane and a driver honours as much of it as its plane can carry.
  The consequence for the conformance suite is in [How round-trip is enforced](#how-round-trip-is-enforced) and it is new work that ADR-0004 did not anticipate.
- **The nil-versus-empty distinction for a composite is not expressible at all**, by any type.
  An earlier draft of this ADR claimed `*[]T` expressed it.
  That claim was false and the probe that killed it is recorded below, because the shape of the mistake is the point.
- **A named type over `time.Duration` still dumps nanoseconds.**
  Narrower than xload's hole, and registration is the answer, and it is not closed.
  *(Closed since, by [ADR-0009](0009-typed-codec-registration.md)'s `DurationLike[T ~int64]()`, at one line per named type.)*

**And one this ADR did not know it was leaving open**, added under [#31](https://github.com/onhotpath/ferry/issues/31):

- **Core's admissible map key set was not injective, so `map[time.Time]V` lost keys silently.**
  This ADR states the injectivity obligation for a *registered* key codec and does not apply it to its own identity table, which the implementation admitted wholesale.
  Closed by [The map key rule, restated](#the-map-key-rule-restated): key admissibility is declared per entry, the obligation is under Go's `==`, `time.Time` is dropped as a key type, and a collision is refused as the address is minted.

### The set is resolved by type identity first, and by `reflect.Kind` second

> A type is looked up in an identity table keyed by `reflect.Type`.
> Only if it is absent from that table is it admitted by its `reflect.Kind`.

The identity table is how ferry owns a type whose representation it will not delegate.
It is keyed by `reflect.Type` values obtained from `reflect.TypeFor[T]()`, compared with `==`, and it contains no strings.

This is the fix for survey item **5.9**, and stating the mechanism matters as much as stating the rule.
xload identifies `time.Duration` by comparing `Type.String()` to `"time.Duration"` ([load.go:301](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L301)), which ADR-0002 rules out by name as a route.
Type identity is the route that remains, and it needs no import ferry does not already have.
Measured on `go1.27rc2`:

```
reflect.TypeFor[time.Duration]() == reflect.TypeOf(time.Second)   true
reflect.TypeFor[time.Duration]() == reflect.TypeFor[int64]()      false, though both Kind() are int64
```

The ordering is the whole rule.
`time.Duration`'s kind is `int64` and `time.Time`'s kind is `struct`, so a kind-first walk dumps a duration as a nanosecond count and a timestamp as three unexported fields.
Consulting identity first is what makes both ferry's.

**The named-type-over-`time.Duration` case is a real hole, and it is left open rather than closed badly.**
Measured: `type Timeout time.Duration` is a distinct `reflect.Type`, so it misses the table, falls to kind `int64`, and dumps `30000000000` for thirty seconds.
Closing it would require matching on the underlying type, which would then also capture every ordinary `type Port int`.
The honest answer is that a user who defines a named duration type registers a codec for it ([#19](https://github.com/onhotpath/ferry/issues/19)), and that this is a documented sharp edge rather than a defect ferry can reflect its way out of.
xload's string comparison has the same hole plus a false positive it does not have, so this is strictly better and still not good.

### The enumerated set

**Leaves admitted by identity.**
ferry owns the representation, in both directions.

| Go type | `Value` kind | representation | inverse |
| --- | --- | --- | --- |
| `time.Duration` | `String` | `time.Duration.String()`, e.g. `30s` | `time.ParseDuration` |
| `time.Time` | `String` | RFC 3339 with nanoseconds, via `MarshalText` | `UnmarshalText` |

**Leaves admitted by kind.**

| Go kind | `Value` kind | representation |
| --- | --- | --- |
| `bool` | `Bool` | `strconv.FormatBool` |
| `string` | `String` | the bytes, unmodified, not required to be UTF-8 |
| `int`, `int8`, `int16`, `int32`, `int64` | `Number` | `strconv.FormatInt`, base 10 |
| `uint`, `uint8`, `uint16`, `uint32`, `uint64` | `Number` | `strconv.FormatUint`, base 10 |
| `float32`, `float64` | `Number` | `strconv.FormatFloat(f, 'g', -1, bits)`, at the type's own bit size |
| `[]byte`, `[N]byte` | `Bytes` | the bytes, unmodified |

**Composites.**
A composite is never itself a value except in the one case below.
It contributes addresses, and its elements are the leaves.

| Go kind | segments minted | tier | note |
| --- | --- | --- | --- |
| `struct` | one `Name` per exported field | static | the field's name is [#11](https://github.com/onhotpath/ferry/issues/11)'s |
| `*T` | none of its own | static | `Null` when nil, otherwise `T`'s addresses |
| `[N]T` | one `Index` per element, exactly `N` | **static** | the length is part of the type |
| `[]T` | one `Index` per element | dynamic | the length is a property of the value |
| `map[K]V` | one `Name` per key | dynamic | `K` restricted below |

**An array is a static composite and a slice is a dynamic one, and the difference is not cosmetic.**
ADR-0003 split both collision rules into a static tier checked from the type and a dynamic tier checked as each address is minted, and ADR-0004 made Load of a dynamic address conditional on the source implementing `Enumerator`.
An array's element addresses are known from `reflect.TypeFor[T]()` with no value in hand, so **an array is loadable from a source that cannot enumerate and a slice is not**.
Measured:

```
struct{ Arr [3]string; Sl []string; M map[string]int }  compiles to
  /Arr#0  /Arr#1  /Arr#2      static, from the type
  /Sl/*   /M/*                dynamic, from the value
```

That is a real capability difference between two types a user might pick interchangeably, and it belongs in the documentation of the set rather than being discovered against a Vault token with no `LIST`.
It also means an array needs no empty-container marker and has no nil form, so none of the nil-versus-empty reasoning below applies to it.
An absent array element leaves the element at its zero value, exactly as an absent struct field does; an index the array cannot hold is loud.
Measured: `[3]string` given only index 0 loads `["a", "", ""]`, and given index 7 returns `ferry: /V: plane has index 7, [3]string holds 3`.
`encoding/json/v2` is stricter here, refusing a short array outright with `too few array elements` and offering `UnmarshalArrayFromAnyLength` as the v1-legacy escape.
ferry does not follow it, because an absent address leaving a zero value is the rule ferry applies everywhere else and an array element is a static address like any other.

**`Index` segments are used, which answers the question ADR-0003 reserved.**
ADR-0003 said that if this ticket admitted no indexed composite, the kind would simply go unused.
Slices and arrays are admitted, so it is used, and ADR-0003's measured reason for kinding segments at all stands: an emitter that guesses list-versus-map from base-10 text turns `map[string]int{"0": ...}` into a YAML sequence and destroys the key.

**A map key type is restricted to `string`, the integer kinds, and any type with a registered codec whose form is a `String` that says `.AsMapKey()`.**
A key becomes segment text on the way out and must be parsed back on the way in, so a key type is a decode and not a conversion.
The prototype's first draft converted the segment text straight to the key type and panicked on `map[int]string` with `value of type string cannot be converted to type int`, which is how the restriction was found rather than assumed.
Float keys are excluded because the mapping is not injective: two distinct `NaN` payloads both format as `NaN`.

**Membership of the identity table confers nothing here.**
Key admissibility is declared per entry, and core's own entries declare it like anybody else's.
`time.Duration` does; **`time.Time` does not**, so `map[time.Time]V` is a schema compile error.

> **Amended under [#31](https://github.com/onhotpath/ferry/issues/31).**
> As published this read "any type with a registered codec whose form is a `String`", and the implementation admitted anything in the identity table wholesale.
> `time.Time` is in that table, and its RFC 3339 text is not injective over it, so `map[time.Time]V` silently lost keys - the exact hazard this ADR states three sections below for a *registered* key codec, occurring inside the set core ships and guarantees.
> [ADR-0007](0007-the-codec-chain-and-its-precedence.md) found it, [ADR-0009](0009-typed-codec-registration.md) recorded that its own opt-in "deliberately does not reach" it, and neither could fix it, because the fix amends this ADR.
> The `.AsMapKey()` clause is [ADR-0009](0009-typed-codec-registration.md)'s, added here so the restriction reads as one rule rather than two.
> See [The map key rule, restated](#the-map-key-rule-restated) for what changed and why the refusal is forced rather than chosen.
> Evidence: `K31=<n|all>` on [`proto/31-mapkey`](https://github.com/onhotpath/ferry/tree/proto/31-mapkey).

**Refused, at schema compile.**
Sorted below into what actually limits each, because only four of these are permanent.

- `complex64`, `complex128`, `chan`, `func`, `interface`, `uintptr`, `unsafe.Pointer`.
- A map whose key type is not `string`, an integer kind, or a registered key codec. *(Amended under [#31](https://github.com/onhotpath/ferry/issues/31): as published this read "or a registered codec", which admitted `time.Time` through the identity table.)*
- A struct that maps no address, which is its own section below.
- A recursive type, which is its own section below.

Unexported struct fields are skipped.
That is stated as a rule rather than left to silence, because ADR-0001 rules out silently ignoring anything: `reflect` cannot set an unexported field, so the alternative is refusing every struct containing a `sync.Mutex`, and the loss it can cause is caught by the maps-no-address rule instead.

**Two type identities are forced by Go and are not choices ferry made.**
Measured: `reflect.TypeFor[[]byte]() == reflect.TypeFor[[]uint8]()` is true, and `reflect.TypeFor[[]rune]() == reflect.TypeFor[[]int32]()` is true.
So ferry cannot offer both "a byte blob" and "a slice of small unsigned integers"; it has one type and must pick, and it picks `Bytes`.
And `[]rune` is `[]int32`, so it is an indexed composite of numbers rather than text, which is legal and is almost certainly not what a user meant.
Both are named in the hazards section rather than left to be discovered.

### A struct that maps no address does not compile

> Every struct type visited during schema compile must contribute at least one address.
> One that contributes none is a compile error naming the address and the type.

This is the probe that overturned the draft, and it is the sharpest single result in the ticket.

A struct is admitted by kind, so under the rule as first written *every* struct was supported.
Measured, before the rule existed:

```
netip.Addr        exported 0/2 fields   0 addresses   compiles clean
netip.AddrPort    exported 0/2 fields   0 addresses   compiles clean
big.Int           exported 0/2 fields   0 addresses   compiles clean
time.Location     exported 0/7 fields   0 addresses   compiles clean

dump netip.MustParseAddr("192.0.2.1")  ->  0 addresses, nil error
load it back                           ->  invalid IP
```

A silent total loss, on four ordinary stdlib types, through the exact mechanism ADR-0001 rules out by name.
It is worse than an unsupported type because it looks supported: the schema compiles, the dump succeeds, the plane is written, and the field is empty.

The rule is checked at every level and not only at the root, because one mapped sibling would otherwise hide the loss.
Measured with the rule in place:

```
netip.Addr                       ferry: (root): netip.Addr maps no address: ...
struct{ A netip.Addr; B string } ferry: /A: netip.Addr maps no address: ...
struct{ mu sync.Mutex; Name string }   compiles, 1 address
```

The error names registration ([#19](https://github.com/onhotpath/ferry/issues/19)) as the fix, which is what registration is for: `netip.Addr` is a type ferry does not own, its `MarshalText` is the obvious codec, and the guarantee transfers to whoever registers it.

### There are three outcomes for a type, not two

The framing so far is binary: a type is in the set or it is refused.
Checked against fifteen types people actually reach for, that is false, and the third outcome is the one with the trade-off in it.

The **how** column names the mechanism that admits the type, because three of the rows below are admitted by one mechanism and refused by another, and a table with no such column cannot say so.

| type | how | outcome | what lands on the plane |
| --- | --- | --- | --- |
| `net.Addr` | - | refused, interface kind | |
| `netip.Addr`, `netip.AddrPort`, `netip.Prefix` | by chain | **admitted, round-trips** | `string("192.0.2.1")`, `string("192.0.2.1:80")`, `string("10.0.0.0/8")` |
| `big.Int` | by chain | **admitted, round-trips** | `string("1099511627776")` |
| `url.URL` | - | refused, **by the field rule**; see below | |
| `net.IP` | by chain | **admitted, round-trips** | `string("192.0.2.1")` |
| `UUID` as `[16]byte` | by kind | **admitted, round-trips** | sixteen raw bytes |
| `net.IPNet` | **registered** | **admitted, round-trips**; refused unregistered | one address, `string("10.0.0.0/8")` |
| `net.TCPAddr` | **registered** | **admitted, round-trips**; refused unregistered | one address, `string("192.0.2.1:80")` |
| `sql.NullString` | **registered** | **admitted, round-trips**; refused unregistered | one address |
| `json.RawMessage` | by kind | **admitted, round-trips** | `bytes("{\"a\":1}")` |
| `type Port int` | by kind | **admitted, round-trips** | `number("8080")` |
| `time.Duration`, `time.Time` | by identity | admitted, pinned representation | `string("1h30m0s")`, RFC 3339 |

> **Amended under [#41](https://github.com/onhotpath/ferry/issues/41), which added the `how` column rather than correcting rows one at a time.**
> Two later ADRs moved this table in opposite directions and neither could see it.
>
> [ADR-0007](0007-the-codec-chain-and-its-precedence.md)'s chain **shortened the refusal list**, which the chain table below already anticipates in terms: `netip.Addr`, `netip.AddrPort`, `netip.Prefix`, `big.Int` and `net.IP` are claimed on their text pair and land as legible strings.
> Those five rows are stale by this ADR's own permission and are corrected above.
>
> [ADR-0008](0008-the-struct-tag-grammar.md)'s mandatory-name rule - "an exported, named struct field with no ferry tag is a schema compile error" - **lengthened it**, and that direction this ADR did not authorise and could not see, because ADR-0008 landed later.
> `net.IPNet`, `net.TCPAddr` and `sql.NullString` are exported fields in other people's packages, so the rule fires on every one of them and none compiles.
> Nor can a user fix it: the remedies the diagnostic offers are *name the segment* and `ferry:"-"`, and both are edits to a struct definition in `net` or `database/sql`.
>
> **Registration rescues all three cleanly**, which is what this ADR's own mechanism sentence predicts - "a codec collapses a type to a leaf, and a leaf needs no address set" - and it is verified end to end on all three planes.
> Each mints **one** address at the field's own address rather than `/IP` and `/Mask`, so the registered representation is not the published one; it is a better one with a different address set.
> The published `/IP` and `/Mask` row is reproducible only on the superseded walk that produced it.
>
> Measured over 26 named third-party types rather than the twelve rows above: 10 compile, 3 are refused for reasons unrelated to the field rule, and **13 are refused by ADR-0008's field rule**, including every `sql.Null*`, every `net.*Addr` and `tls.Config` - the shape every configuration struct has.
> Evidence: `X3=all` on [`proto/tip`](https://github.com/onhotpath/ferry/tree/proto/tip).

So:

1. **In the set with a pinned representation.** Core's table, checked by the golden column.
2. **Refused at schema compile.** Loud, from the type alone.
3. **In the set by kind, with an unpinned representation nobody chose.**

Category 3 is the honest cost of admitting types by `reflect.Kind`.
The same rule that makes `type Port int` work for free writes a UUID into a YAML file as sixteen raw bytes, because `[16]byte` is `Bytes`.
*(As published this named `net.IP`, which was the sharper example at the time. ADR-0007's chain now claims `net.IP` on its text pair and writes `string("192.0.2.1")`, so the example moved to `[16]byte`, which no chain arm claims. The argument is unchanged, and the fact that it needed a new example is the chain shortening the list exactly as intended.)*
Value fidelity is **not** violated: every one of those round-trips exactly.
What is violated is legibility, and ADR-0001 put legibility of the plane on the driver's side of the line, so nothing in core's guarantee catches it.

This is the representation blindness stated earlier, applied to types core has no golden row for.
For core's own types the golden column pins the answer; for a type admitted by kind there is no golden row, because there is no table entry.

**It is accepted rather than fixed**, and the alternatives are worse.
Refusing named types over admitted kinds would kill `type Port int`, which is the main thing kind admission buys.
Special-casing named `[]byte` types is arbitrary and would still miss `net.IPNet`.
The mitigation is registration ([#19](https://github.com/onhotpath/ferry/issues/19)) plus documentation, and the documentation obligation is specific: **the set's documentation names category 3 and lists the common members**, because a user who sees `net.IP` "work" has no reason to look further until they read the file.

`url.URL` is worth its own line, because it is the case that will be reported as a bug.
It is refused not for itself but because its `User *url.Userinfo` field maps no address, so the rule propagates out of a nested type the user did not choose.
The error names `/V/User` and `url.Userinfo`, which is the right diagnosis, and it is still a type that "obviously should work" and does not.

> **Amended under [#41](https://github.com/onhotpath/ferry/issues/41): the outcome stands and the diagnosis no longer reproduces.**
> ADR-0008's field rule fires on all eleven of `url.URL`'s exported fields, `User` among them, so `url.Userinfo` is never entered and the refusal names none of it: twelve lines, none mentioning `url.Userinfo`.
> `url.URL` is refused either way, so this is an evidence defect rather than a behaviour one - but the sentence "the error names `/V/User` and `url.Userinfo`, which is the right diagnosis" is no longer true of any ferry.
> Evidence: `X3=1` on [`proto/tip`](https://github.com/onhotpath/ferry/tree/proto/tip).

### A recursive type does not compile

A recursive type has an unbounded static address set, so schema compile refuses it rather than recursing.
Measured: `struct{ Name string; Next *Node }`, `struct{ Kids []Tree }` and `struct{ M map[string]ViaMap }` all recurse without bound, and all three are detected by a type-stack from `reflect.TypeFor[T]()` alone with no value in hand.

This is not a limitation ferry chose so much as one ADR-0003 already implies: an address set that cannot be enumerated cannot be handed to `Bind` before I/O, which is the precondition the driver-side injectivity rule needs.

### Every refusal is one of three kinds, and only one of them is permanent

"Refused" is not one thing, and lumping the refusals together makes ferry look more closed than it is.
Sorted by what actually limits them, and tested against a registered codec rather than reasoned about:

**(a) The value does not exist outside the process.**
`chan`, `func`, `unsafe.Pointer`, `uintptr` used as a real pointer.

A codec has to produce a `Value`, which is a kind and text, and rebuild the value from that text alone.
For these there is nothing text could carry.
The sharpest case is `func`, and it fails on the encode side before the decode side is even reached: measured, `reflect.TypeFor[func()]().Comparable()` is **false**, so a codec cannot even ask which registered function this is.
A `chan` is comparable, but its identity is a pointer into this process's heap.
**Nothing lifts these, and they are the only permanent refusals.**

**(b) Core cannot compute an address set for the type.**
Interfaces, recursive types, and structs that map no address.

This is the large category and it is **entirely liftable**, for one reason worth stating as a rule:

> A codec collapses a type to a leaf, and a leaf needs no address set.

`classify` consults the identity table before `reflect.Kind`, so a registered type is a leaf, mints exactly one address, and is never walked.
Whatever made its address set uncomputable stops being asked.
Measured, the same five types before and after registering a codec:

| type | before | after |
| --- | --- | --- |
| `netip.Addr` | refused, maps no address | `string("192.0.2.1")` |
| `big.Int` | refused, maps no address | `string("1099511627776")` |
| `Node`, recursive | refused, unbounded address set | `string("a>b")` |
| `net.Addr`, an **interface** | refused, interface kind | `string("tcp://192.0.2.1:80")` |
| `map[netip.Addr]string` | refused, key type | `/V/10.0.0.1=string("a")` |

The interface row is the one that looks impossible and is not.
A registered codec for `net.Addr` owns the discriminator inside its own text, so ferry needs no type registry and the plane gets no ferry-specific tagging.
The recursive row is the same trick: a codec terminates the walk that would otherwise not terminate.

**A registered codec may also serve as a map key**, which is what lifts the last row, and it carries a stronger obligation than a leaf codec:

> A key codec's text must be **injective** over the key type, under Go's `==`.

Two distinct keys producing one text collapse into one address, silently.
Core ships `string` and the integer kinds because both are trivially injective; `float64` is excluded because two distinct `NaN` payloads both format as `NaN`.
Injectivity is not checkable in general, so it is a proof obligation on the registrant, discharged over their supplied value list in the same harness.

> **Amended under [#31](https://github.com/onhotpath/ferry/issues/31): "under Go's `==`" is new, and it is the whole ticket.**
> As published the obligation named no relation, and this ADR makes the equality relation **per type and required** two sections later, so the omission was not neutral: read under the type's own proof relation, `time.Time`'s RFC 3339 text *is* injective, because `.Equal` says the two colliding values are one.
> Read under `==` it is not, and `==` is the one that decides, because `==` is what the Go map's key identity is and therefore what decides how many entries the map holds.
> A weaker relation cannot see an entry disappear, because under it the entry was never there.
> The consequence is that a key type must satisfy a **stricter** relation than its own leaf proof, so a type can be a legal leaf and an illegal key, and `time.Time` is the first instance.

**(c) Refused by policy, not by constraint.**
`complex64`, `complex128`, `uintptr`.

Nothing structural refuses these and the ADR should not imply otherwise.
Measured: `strconv.FormatComplex` and `ParseComplex` are a total inverse pair, `(1+2i)` round-trips bit-exactly.
They are out because no plane in ferry's range has a complex type, and because a config or i18n or query-parameter struct containing a `complex128` is not a case worth a row in a table that has to be maintained forever.
`uintptr` round-trips as a `uint` and means nothing in another process.
Registration is available for anyone who disagrees, which is the correct amount of effort to ask of that user.

**So the honest summary is that ferry refuses four Go kinds permanently, and everything else is a matter of who supplies the codec.**
That is a materially different claim from "the set is closed", and it is the one ADR-0001 actually made: core's set is closed, extension is explicit, and extension carries its proof.

**One ordering consequence, and it is [#12](https://github.com/onhotpath/ferry/issues/12)'s to take.**
Category (b) shrinks a lot if the codec chain consults `encoding.TextMarshaler` and `TextUnmarshaler` before kind admission, with no registration by anyone.
Measured:

| type | has a text pair | today | if the chain ran first |
| --- | --- | --- | --- |
| `netip.AddrPort` | yes | refused | `string("192.0.2.1:80")` |
| `netip.Prefix` | yes | refused | `string("10.0.0.0/8")` |
| `netip.Addr` | yes | refused | `string("192.0.2.1")` |
| `big.Int` | yes | refused | `string("1099511627776")` |
| `net.IP` | yes | **`bytes("\x00...")`** | `string("192.0.2.1")` |
| `url.URL`, `net.IPNet`, `[16]byte` UUID | no | unchanged | unchanged |

Four refusals become support, and `net.IP` stops being an unreadable blob, which is category 3 of the three-outcomes section shrinking too.
This ADR does not decide the chain, so it states the interaction as a constraint instead:

> The maps-no-address rule and the kind admission rule are **backstops**, and they only apply after the codec chain has declined.

If #12 puts `TextMarshaler` ahead of kind, this ADR's refusal list gets shorter and nothing in it becomes wrong.
If #12 puts it after, `net.IP` keeps landing as sixteen raw bytes and that is a decision someone took rather than one that happened.

### The map key rule, restated

*Added under [#31](https://github.com/onhotpath/ferry/issues/31), which is a defect in this ADR's admissible key set rather than in anything built on it.*
*Evidence: `K31=<n|all>` on [`proto/31-mapkey`](https://github.com/onhotpath/ferry/tree/proto/31-mapkey), eleven probes, every one through the entry point.*

> **A type keys a map only if it is declared usable as one, and the declaration is per entry.**
> Core's identity table declares it, a registration declares it with `.AsMapKey()`, and nothing else confers it.
>
> **The obligation is injectivity under Go's `==`**, and core admits only what it has proved.
>
> **A collision is refused as the address is minted**, before the write it belongs to, naming the address and the key type.

Three things, and the third is what makes the first two safe.

#### Core exempted itself from its own rule, and `time.Time` is what that cost

`validMapKey` admitted anything in the identity table, so `time.Time` keyed a map on the strength of being owned rather than of being injective.
Measured through `Dump`, in three shapes rather than the one [ADR-0007](0007-the-codec-chain-and-its-precedence.md) reported, because they are not equally exotic:

| pair | `a == b` | `a.Equal(b)` | same text | Go keys -> ferry addresses |
| --- | --- | --- | --- | --- |
| `time.UTC` against `FixedZone("GMT", 0)` | false | true | true | 2 -> 1, nil error |
| `time.Now()` against `time.Now().Round(0)` | false | true | true | 2 -> 1, nil error |
| two `FixedZone("UTC", 0)` calls | false | true | true | 2 -> 1, nil error |

The second row is the ordinary one: a monotonic reading is what `time.Now()` returns and stripping it is what storing it does.

#### There is no injective text form for `time.Time`, so "keep it with a caveat" has nothing behind it

"Drop it, or keep it with a stated caveat" is the choice the ticket named, and keeping it is only available if some codec could discharge the obligation.
None can, and the reason is a property of the type rather than of RFC 3339:

> `time.Time` is `{wall uint64, ext int64, loc *Location}`, and `==` compares the `loc` **pointer**.
> No text carries a pointer.

`time.FixedZone` allocates a fresh `*Location` on every call, so two calls with the same name and the same offset produce two values that are distinct under `==` and identical under every encoding the standard library has.
Measured, on `go1.27rc2`, over the two such values:

```
MarshalText  MarshalJSON  MarshalBinary  GobEncode  Format(RFC3339Nano)  Format(...MST)
UnixNano     String()     GoString()     %#v        Location().String()  Zone()

0 of 12 encodings distinguish them
```

So the refusal is **forced rather than chosen**, and it is forced in the same sense the nil-versus-empty collision above is: there is no design that avoids it, only designs that hide it.
It also fixes the diagnostic, because a message reading "register an injective codec for it" would be naming a remedy that does not exist:

```
ferry: /v: time.Time is in core's own set and is not usable as a map key: its
       text is not injective over the type, so two distinct keys collapse into
       one address; key the map by a type that is, or convert the key yourself
```

**What the refusal costs is one real use case**, a map keyed by an instant, and the three available remedies were run rather than asserted.
`map[int64]V` keyed by `UnixNano` works and drops the zone, which is exactly the information no text could have carried.
`map[string]V` keyed by RFC 3339 works and puts the collapse in the user's own code where it is visible.
And a named type over `time.Time` with a registered codec saying `.AsMapKey()` **is accepted**, which is a user defeating the refusal with a claim nobody can check - the same shape as [#45](https://github.com/onhotpath/ferry/issues/45), arriving from the other side, where the refusal is lifted by deleting a registration rather than by adding a keyword.
Neither is a hole in the compile-time rule.
Both are why the compile-time rule cannot be the only rule.

#### The mint-time check is the second half, and it is the one that is complete

[ADR-0003](0003-how-a-leaf-addresses-a-plane.md) names two dynamic collision checks and only one of them can see this.

The **driver-side** check asks whether two addresses collapse into one plane key, and it is keyed by `Path`.
Two Go map keys collapse into one `Path` *before* the driver is asked anything, so the driver sees one address once and answers correctly.
Measured: `Key(/v/2026-01-15T12:00:00Z)` twice, two nil errors, and there cannot be an error, because the collision is upstream of the only check that existed.

The **core-side** check is prefix-freeness run "as each is minted, before the write it belongs to", and a repeated address is a prefix of itself, so the rule already covered this case in terms and no implementation ran it.
Implemented at the walk's map member step:

```
ferry: /v: map keys of type time.Time are not injective: /v/2026-01-15T12:00:00Z
       is addressed more than once, and 1 entry would be lost
```

**It arrives with a speedup**, which is worth stating because "and it is cheap" is usually a concession.
This ADR's own determinism invariant already requires a map's members to be sorted by key text, and the walk was calling the key-text function *inside the comparator*, so a key's text was recomputed `O(n log n)` times and no two texts were ever compared for equality.
Computing it once per key is what a duplicate check needs anyway:

| keys | as the tip shipped | text computed once | + the duplicate check |
| --- | --- | --- | --- |
| 8 | 4436 ns | 1713 ns | 1599 ns |
| 64 | 94017 ns | 16328 ns | 16381 ns |
| 512 | 1146337 ns | 158116 ns | 157189 ns |

**And the check is a complete backstop rather than a partial one**, for a structural reason: a plane's addresses are already a set, so enumeration cannot hand the walk one address twice.
A key collision can only be *created* where keys are minted, and keys are only minted on Dump.
What it is not is sufficient on its own, and the ADR says which: it fires on the plane being written, and by then the artefact the user loaded from has already lost the entry.

#### ADR-0001's determinism invariant is an independent argument for the refusal

The choice is not "silent but stable" against "loud".
There is no stable option, because which entry survives is which one the walk writes last, and with two equal texts that is Go's map iteration order.
Measured, 300 dumps of one value:

```
as shipped   2 distinct outcomes over 300 dumps    34/300 and 266/300
with the rule  1 distinct outcome over 300 dumps   300/300, the refusal
```

> A dump that collapses two keys has no deterministic answer to give, so the only outcome consistent with [ADR-0001](0001-what-ferry-supports.md) is a refusal.

That argument does not depend on anybody agreeing that a lost entry matters, which is what makes it the stronger of the two.
It is also the resolution of the two lines the [#41](https://github.com/onhotpath/ferry/issues/41) audit records as flaky across runs of the same binary and hands to this ticket: both are probes whose subject *is* the collapse, so neither is made deterministic by a fix, and both are now stated as an outcome **set** over 200 dumps rather than as one draw from it.

#### What core has actually proved, which is less than the set it admits

The completeness question [#41](https://github.com/onhotpath/ferry/issues/41) asked of the leaf set, asked of the key set, run through `validMapKey` itself:

| admitted as a key by | injective under `==` |
| --- | --- |
| `string` and the integer kinds, by kind | **proved**: the text is the value, or base 10 is a bijection on the width |
| `time.Duration`, by identity | **bounded**: no collision over 2^20 random values plus the extremes |
| `time.Time`, by identity | **disproved**, and no codec can fix it |
| a registered codec that said `.AsMapKey()` | the registrant's claim |

A randomised hunt is what turns a claim into either a counterexample or a bound, and the ADR states what each is worth.
A hit is a proof of non-injectivity and is the strongest result available; `time.Time` needed two values.
A miss over 2^20 is a bound and not a proof, which is what a registrant may honestly say and what core should not have to.
`string` and the integer kinds need no hunt at all, and that is the difference: they are the only two rows core can prove, and they are exactly the two this ADR named in the first place.

#### What this hands the harness

A key type needs a fourth thing that a value proof structurally cannot supply.

> A value proof asks whether this value survives a round trip.
> A key proof asks whether these two values stay two.

No value proof can see the second, because the collision is *between* values and every case in a value proof runs alone.
Measured: core's `time.Time` proof passes on all three planes, today, with the defect live.

Two corrections to the helper [ADR-0009](0009-typed-codec-registration.md) proposes, `Injective[T any](format func(T) string, values ...T) error`, both measured:

- `T` is unconstrained, so it compiles for a type Go cannot key a map with and has no `==` to test distinctness under.
  `comparable` is the constraint that makes the signature state the obligation.
- The prover supplies `format`, and **ferry does not call it**.
  What addresses the plane is the key-text function, which consults the identity table, then the chain, then the kind.
  Measured on one type through both routes: the registrant's own `String()` gives `"api:80"` and `"api:443"`, and ferry writes `"api"` and `"api"`.
  A registrant who proves their own function injective has proved nothing about what ferry writes, so the check has to take the registry and ask ferry - which is the correction [ADR-0007](0007-the-codec-chain-and-its-precedence.md) already made once, for the declared kind: one lookup, not two.

What that costs `ferrytest` in exported surface is [#35](https://github.com/onhotpath/ferry/issues/35)'s, and this ADR hands it the shape rather than the spelling.

#### One defect found on the way, which is not this ADR's

`.AsMapKey()` was gating a codec the walk never called.
The two caller-facing entry points walked with no registry installed, so a registered key codec's text was not what `Dump` wrote: measured, one registry and one value producing two addresses through the entry point and one through a probe that installs it.
The key text is the only codec lookup the compiled schema does not carry, which is [ADR-0007](0007-the-codec-chain-and-its-precedence.md)'s third defect - "two lookups for one decision is how a chain drifts" - recurring at the key position.
It belongs to [#16](https://github.com/onhotpath/ferry/issues/16)'s compiled schema rather than here, and it is named because every injectivity statement about a registered or chain-admitted key type is a statement about the wrong function until it is fixed.

### A type outside the set is refused at schema compile, and every violation is reported

Detection is at schema compile, from `reflect.TypeFor[T]()` alone, with no value in hand, no plane reachable, and identically in both directions.
That is the same assertability ADR-0001 claims for tag rejection and ADR-0003 claims for the static half of the collision rule, and it is the property that makes "a type outside the set is a loud error" testable in a unit test rather than only in an integration test.

Violations are collected and joined rather than reported one at a time, and the report is sorted, which is ADR-0001's determinism invariant applied here rather than re-decided.
Measured refusals, each naming the address and the type:

```
ferry: /C:  unsupported type complex128 (kind complex128)
ferry: /F:  unsupported type func() (kind func)
ferry: /I:  unsupported type interface { Foo() } (kind interface)
ferry: /Ch: unsupported type chan int (kind chan)
```

The error types themselves follow [#9](https://github.com/onhotpath/ferry/issues/9)'s convention rather than being invented here.

### A nil composite and an empty composite are one value, and the collision is forced

This is the second probe that overturned the draft, and the one whose reasoning most needs writing down, because the conclusion runs against the direction [#18](https://github.com/onhotpath/ferry/issues/18) came from.

Under ADR-0003 a composite gets one address per element.
A composite with no elements therefore mints no element address, and the question is what it mints of its own.
There are three Go states to tell apart, and the boundary offers two observations at a container address.

**Measured, through the real YAML source**, asking what a plane reports at a container address:

| document | `Get(/tags)` | `Children(/tags)` |
| --- | --- | --- |
| no `tags` key at all | `Absent` | none |
| `tags: []` | `Absent` | none |
| `tags: {}` | `Absent` | none |
| `tags: null` | `Null` | none |
| `tags: [a]` | `Absent` | `/tags#0` |

`tags: []`, `tags: {}` and a missing key are **one observation**.
Three states into two signals means one collision, and no option in ADR-0004's kind set removes it: there is no group arm, no escape arm, and no "present and empty" kind.

The first draft chose the other collision, writing `Null` for nil and nothing for empty, and the audit found what that costs:

```
[][]string{{"a"}, {}, nil}                       -> {{"a"}, nil, nil}     empty inner became nil
map[string][]string{"a":{"x"}, "b":nil, "c":{}}  -> {"a":{"x"}, "b":nil}  the key "c" VANISHED
```

A vanishing map key is a silently dropped entry, which is precisely what ADR-0001 rules out.
It happens because a map key's existence is signalled only by having addresses under it, so a key whose value mints nothing is not enumerable.

So the decision is the other way round:

> A composite with no elements writes `Null` at its own address, whether it is nil or empty.
> On Load, a container address with no children yields the zero value, which for a slice or a map is nil.

Nothing vanishes, the rule is total, and the normalisation lands on the Go zero value rather than on a manufactured non-zero one.
Measured after the change: 21 of 21 proofs pass, on the memory plane and through the real YAML driver alike, including `[][]string` and `map[string][]string`.

**The obvious repair does not work, and checking it is what makes the collision forced rather than merely inconvenient.**
The repair is to have both states write something distinguishable: `Null` for nil and, say, an empty `String` for empty, since ferry knows the Go type at that address even though the driver does not.
Two things kill it.
Measured, a plane with no null flattens `Null` to an empty `String` anyway, so the two collide again on exactly the planes that most need them apart.
And ADR-0004's own table records that TOML, env, query params and opaque KV cannot produce `Null` at all, so the distinction would exist on YAML and JSON and vanish on the other four.

That is the deciding argument, and it is stronger than counting states:

> A guarantee that holds on some planes and not others is not a guarantee.

Value fidelity is a property of the type set, so it has to be uniform across planes or it is not the thing ADR-0001 promised.
This is not in tension with the nil-pointer limitation recorded above, and the line between them is worth stating: there, ferry emits the right value and a plane that cannot carry it refuses loudly, which is a driver limitation.
Here, ferry would be inventing a second marker in order to build a guarantee on a capability three of the four first-party planes lack.
The first is honest; the second is a feature that would work in the demo and fail in production.

**The contrast with json/v2 is worth stating precisely, because it is easy to read this as conceding the point #18 made.**
v2's default marshals a nil slice to `[]`, which unmarshals to an empty non-nil slice, so `Load(Dump(x)) != x` and the result is a value the user never had.
ferry normalises in the opposite direction, onto nil, which is the zero value and the thing an unset field already is.
The two are not symmetric: one manufactures a value, the other collapses onto the identity Go already gives an unset composite.
The pinned option set below keeps the JSON route pointed the same way, so the two routes cannot disagree.

**What this costs, plainly, and the draft got this wrong.**
A user who needs to distinguish "no tags configured" from "tags explicitly cleared" cannot express it with `[]string`.

An earlier draft of this ADR said the expressible form was `*[]string`, with the pointer carrying presence and the slice carrying content, on the reasoning that a nil pointer writes `Null` at a leaf address rather than at a container address.
**That was asserted without being measured, and it is false.**

```
*[]string  nil pointer            mints  /P=null   loads back  nil pointer
*[]string  pointer to []string{}  mints  /P=null   loads back  nil pointer
*[]string  pointer to {"a"}       mints  /P#0      loads back  &{"a"}
```

A nil pointer and a pointer to an empty slice are one address carrying one value, because the pointer dereferences to a composite whose emptiness is written at the same address the nil pointer would use.
Adding a pointer adds no bit.

So the honest statement is that **the distinction is not expressible by any type in the set**, and a user who needs it models it explicitly, as `struct{ Set bool; Items []string }`, or registers a codec.
That is a worse answer than the draft's and it is the true one.
It is recorded at this length because it is the clearest instance in this ticket of the rule the prior four sessions all arrived at: an assertion that was never run is not a finding, and this one survived two drafts before a probe took ten lines to kill it.

**One structural consequence for ADR-0003, recorded because it is subtle.**
Measured, the same field under two values:

```
Tags []string, nil          mints  /Tags
Tags []string, {"a","b"}    mints  /Tags#0  /Tags#1
```

`/Tags` is a prefix of `/Tags#0`, so one type has two address shapes that are never simultaneously realised.
ADR-0003's prefix-free rule holds per realised address set and is not violated by either shape, but the static tier cannot treat a composite's own address and its element addresses as a static prefix clash.
This is a constraint on how the static check is written, not a change to the rule, and it is named here so it is not rediscovered as a bug.

### On Load, `String` is the universal donor, and nothing else coerces

The tables above say which `Value` kind a Go type **produces** on Dump.
They do not say which kinds it **accepts** on Load, and the draft of this ADR simply omitted the question.
Omitting it is not neutral: the strict reading, that a Go `int` accepts only `Number`, was what the prototype implemented, and it means ferry cannot load an integer from environment variables.

Measured, before the rule existed, loading a five-field struct from a plane that reports `String` for everything:

```
err = value: wrong kind
got = {Port:0 Ratio:0 On:false Timeout:0s Name:}
```

ADR-0004 records that "a typed boundary buys YAML and TOML something real, JSON something partial, and Consul, environment variables and query parameters nothing at all", and that three of its four first-party drivers are in the last group.
A plane in that group can only ever report `String`.
So the strict reading disables three of four first-party drivers for every non-string field, and the harness did not catch it because every proof fed `dump`'s own output back into `load`, where the kinds match by construction.

The rule:

> Every leaf accepts its own kind.
> Every leaf additionally accepts `String`, whose text is parsed by exactly the parser that leaf's own kind uses.
> Nothing else coerces.

`String` is the universal donor because `String` is what a plane says when it has nothing to say: it means untyped text, parse it yourself.
`Number`, `Bool` and `Bytes` are assertions, and a plane that makes one is respected rather than second-guessed.
So a `Number` is **not** accepted for a Go `string` field, measured as `value: wrong kind`, because accepting it would be ferry overriding a plane's own type information and would destroy the quoting distinction ADR-0004 preserved on purpose.

This is section 4's asymmetry stated as a rule rather than as an observation.
Load survives a string boundary because the destination field type drives the parse; Dump does not, which is why the boundary is typed at all.
The donor rule costs value fidelity nothing, because Dump still writes the precise kind and Load still accepts it.

**The parser is the leaf's own, and that is what separates this from `cast`.**
The survey measures `spf13/cast`, which xload depends on, corrupting five ordinary values.
Measured side by side on the same inputs, all through `String`:

| input | ferry | `cast` |
| --- | --- | --- |
| `"0080"` | `80` | `0`, invalid octal with the error swallowed |
| `"010"` | `10` | `8`, base 0 reads it as octal |
| `"0x10"` | refused | `16` |
| `"1.9"` into an `int` | refused | `1`, truncated |
| `""` into an `int` | refused | `0`, indistinguishable from a real zero |
| `"yes"` into a `bool` | refused | `false`, silently |
| `"30"` into a `time.Duration` | refused, `missing unit in duration "30"` | `30ns` |

Every row is a refusal or an exact answer, and none is a guess.
`"0080"` is the case the survey singles out, a zero-padded port silently becoming zero, and it is the difference between `strconv.ParseInt(s, 10, bits)` and `cast`'s base-0 inference.
`"yes"` is the survey's other named asymmetry, where `enabled: yes` from YAML is `true` and `ENABLED=yes` from env is `false`; ferry refuses both rather than making them agree on a guess.

**A leaf that does not parse is loud**, which is the same rule stated from the other side.
Measured into an `int8`:

```
Number("abc")    strconv.ParseInt: parsing "abc": invalid syntax
Number("99999")  strconv.ParseInt: parsing "99999": value out of range
Bool(true)       value: wrong kind
Number("")       strconv.ParseInt: parsing "": invalid syntax
```

Overflow is an error rather than a truncation, which is the koanf defect the survey measured, where `Int64()` turns `18446744073709551615` into `9223372036854775807` with a nil error.
The error types are [#9](https://github.com/onhotpath/ferry/issues/9)'s; that they exist at all is this ADR's.

### The named hazards, each resolved

**Float precision.**
Resolved by source text at the type's own bit size, which is the tagged-text finding section 4 of the survey reaches and which `encoding/json/v2` still agrees with in 2026.
Measured, `strconv.FormatFloat(f, 'g', -1, 64)` then `ParseFloat`, comparing bit patterns:

```
math.MaxFloat64            1.7976931348623157e+308   bit-exact
math.SmallestNonzeroFloat64  5e-324                  bit-exact
0.1, 1.0/3.0                                         bit-exact
+Inf, -Inf, NaN, -0                                  bit-exact
```

Every value in the type round-trips, including the four specials.
The bit size is load-bearing: a `float32` formatted at bit size 64 gives `0.10000000149011612`, which re-rounds to the same `float32` and is a wrong-looking config file, so `float32` formats at 32.

Two consequences ride on this and are stated where they belong rather than here.
`NaN != NaN`, so the harness's equality relation cannot be `==`, which the harness section takes as a requirement.
And `+Inf` and `NaN` are Go's spellings, which no JSON plane can hold at all: measured, `json.Marshal` of `math.Inf(1)` on `go1.27rc2` returns `unsupported value: +Inf`.
That is a driver-fidelity boundary rather than a core one, and it belongs to the conformance suite: ferry's `float64` is wider than JSON's number, and a JSON driver must refuse rather than mangle.

**`time.Time` and its monotonic clock.**
`time.Time` is in the set, its representation is RFC 3339 with nanoseconds, and **its equality relation is `time.Time.Equal` and not `==`**.

That is not a carve-out ferry invented.
The stdlib says it directly ([time.go:131-137](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/time/time.go)): "Note that the Go == operator compares not just the time instant but also the Location and the monotonic clock reading... In general, prefer `t.Equal(u)` to `t == u`."
Measured, dumping `time.Now()` and loading it back:

```
now == back            false
DeepEqual(now, back)   false
now.Equal(back)        true
```

The monotonic reading is stripped, which is correct: it is process-local and has no meaning on a plane.

**The `Location` is not preserved, and this is a genuine loss rather than a technicality.**
Measured, a value in `America/New_York`:

```
before  2026-08-02 12:00:00 -0400 EDT   Location "America/New_York"
after   2026-08-02 12:00:00 -0400       Location ""
```

RFC 3339 carries the offset and not the zone identity, so the zone name cannot survive.
`encoding/json/v2` produces exactly the same result on the same input, measured side by side, so ferry is not worse than the stdlib here; it is identical to it, and it says so rather than implying the round trip is total.

**What that loss actually costs, because "the zone name is lost" understates it.**
The instant, the wall-clock reading, the nanoseconds and the offset all survive.
What is destroyed is the zone's *rules*, so the loaded value is a fixed-offset zone that does not know DST exists.
Measured on a January value in `America/New_York`, then adding six months to cross into EDT:

```
orig.AddDate(0, 6, 0)   2026-07-15 12:00:00 -0400 EDT
back.AddDate(0, 6, 0)   2026-07-15 12:00:00 -0500
same instant?           false
```

So a stored timestamp is unaffected and a stored "when to run next" is wrong by an hour, half the year.
`time.UTC` costs nothing: it round-trips under `==`, Location included.
The practical guidance that falls out is that a `time.Time` crossing a ferry plane should be UTC, and this is a property of RFC 3339 rather than of ferry.

**The loaded `Location` is machine-dependent, which is worse than lossy.**
`time.Parse` is documented ([format.go:1012-1015](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/time/format.go)): "if the offset corresponds to a time zone used by the current location (`Local`), then Parse uses that location and zone in the returned time. Otherwise it records the time as being in a fabricated location with time fixed at the given zone offset."

Measured on a machine whose local zone is `Asia/Kolkata`, loading the same document:

```
in   Asia/Kolkata  +05:30    wire "2026-01-15T12:00:00+05:30"    out  Location "Local"
in   America/New_York -05:00 wire "2026-01-15T12:00:00-05:00"    out  Location ""
```

The first row's `Location` would be a fabricated fixed zone on any machine not at `+05:30`.
So two machines loading one plane get `time.Time` values that are `.Equal` and not `==`, and whose `Location` differs.
ADR-0001's determinism invariant is about ferry's output ordering and is not violated, but "the same plane loads to the same value everywhere" is not true for `time.Time` and a reader would assume it is.
This is inherited from `time.Parse` and there is no way to avoid it while using RFC 3339.

**`encoding/json/v2` is measured, not assumed, and it does exactly the same thing.**
It has precisely two time-related options in the whole package set, `FormatDurationAsNano` and `ParseTimeWithLooseRFC3339`, and neither touches zones.
The `format:` tag option that could have specified a zone-preserving layout was removed from the supported set in 1.27 and now errors: `Go struct field V has unsupported "format" tag option`.
Measured on `go1.27rc2`, v2 produces `{"v":"2026-01-15T12:00:00-05:00"}`, loads back to `Location` `""`, and shows the identical DST blindness and the identical `Local` machine-dependence.
v1 is the same.
**So there is no zone-preserving option in the standard library to adopt**, and ferry is not choosing a worse answer than json/v2; it is inheriting the only answer RFC 3339 permits.
A user who needs zone identity stores it as a second field, or registers a codec.

`time.Time` also has values with no text form at all: `MarshalText` errors with `year outside of range [0,9999]`, so the representation is partial over the type and the error surfaces rather than being swallowed.

**And the relation is doing more work than it looks.**
Choosing `.Equal` as `time.Time`'s relation is not only a statement about how to compare; it is a statement about what the harness is permitted not to notice.
Measured, replacing `time.Time`'s codec with one that discards the zone entirely and stores a Unix nanosecond count, the zoned value passes with **zero failures**, because `.Equal` compares instants and a discarded zone preserves the instant exactly.
The full core value list does catch that codec, at two failures, but for an unrelated reason: `time.Time{}` is year 1 and overflows `UnixNano`.

That is the sharpest available statement of why a proof is a triple.
A weaker relation admits more, so the relation is where a type's carve-out is declared, and the golden column is the only one of the three that pins the representation on purpose rather than by luck.

**Map ordering.**
Resolved by ADR-0001's determinism invariant, applied rather than re-decided: every map iteration reaching a user-visible artefact sorts its keys.
Measured, 300 dumps of an eight-key `map[string]int`: **1 distinct ordering**.
The contrast is `encoding/json/v2` at its default, measured on `go1.27rc2` over 50 marshals of the same map: **8 distinct orderings**, and 1 with `Deterministic(true)`.

**`[]byte`.**
Resolved as `Bytes`, and the decision is forced rather than chosen, because `[]byte` and `[]uint8` are the same `reflect.Type`.
`[N]byte` is `Bytes` too, which agrees with json/v2: measured, v2 marshals `[3]byte{1,2,3}` as `"AQID"` while v1 marshals it as `[1,2,3]`, and v1's behaviour survives only through the legacy `FormatByteArrayAsArray` option.
ADR-0004 already fixed that `Bytes` lives in `Value`'s `text` field, so this costs no allocation and keeps `Value` comparable.

Base64 is **not** ferry's business: `Bytes` carries the bytes, and how a plane spells them is the driver's.
That distinction was found by use rather than by reading.
The prototype's YAML sink wrote raw bytes under a `!!binary` tag without encoding them, and its reader decoded them the same wrong way, so the pair was self-consistent and round-tripped; what caught it was `gopkg.in/yaml.v3`'s emitter refusing to emit invalid `!!binary`.
That is a driver-fidelity defect invisible to a value-fidelity test, which is ADR-0001's split doing exactly the work it was separated for, and it is a conformance case.

**`time.Duration`.**
Owned by ferry, in the identity table, represented as `30s` rather than as a nanosecond count.

This is the entry #18 called the first concrete member of the set, and ferry's answer departs from json/v2 deliberately.
Measured on `go1.27rc2`: `json.Marshal(struct{ D time.Duration }{time.Second})` returns `cannot marshal from Go time.Duration within "/D": no default representation`, and the v1-legacy `FormatDurationAsNano(true)` gives `{"D":1000000000}`.
So the two available answers upstream are "refuse" and "nanoseconds", and ferry takes neither.

The Go team refused to guess and made the user choose.
For a general mapper whose most common application is configuration, `TIMEOUT=30s` is the value people actually write, and a plane holding `1000000000` is a worse artefact than one holding `30s` by every measure except symmetry with a JSON library ferry is not.
ferry cannot claim to be following json/v2 here and does not.

**Is a `fmt.Stringer` fallback ever safe?**

> No, and it is not close.
> `fmt.Stringer` is never consulted, in either direction.

The reason is a property of the interface rather than of any type: `String() string` **declares no inverse**.
`encoding.TextMarshaler` and `encoding.TextUnmarshaler` are a pair and declare one; `Stringer` is a debugging interface whose contract is human readability.
A round-trip guarantee cannot rest on an interface that does not promise one.

Measured, over types people actually put in a config struct:

| type | `String()` | is it an inverse |
| --- | --- | --- |
| `time.Duration` | `1h30m0s` | yes |
| `netip.Addr` | `2001:db8::1` | yes |
| `net.IP` | `2001:db8::1` | yes |
| `time.Time` | `2026-08-02 12:00:00 +0000 UTC` | **no**, unparseable by RFC 3339 |
| `url.URL` | `{https  u:p h /a/b f x=1 /a%2Fb  false false}` | **no** |
| `net.IPNet` | `{10.0.0.0 ff000000}` | **no** |

Three of six, and the three that work do so by luck rather than by contract.

Two of the failures are worse than they look.

**`time.Time` implements `fmt.Stringer` and `encoding.TextMarshaler` both**, verified, and only one of them round-trips.
So precedence alone would decide correctness, which is exactly the cautionary case [#12](https://github.com/onhotpath/ferry/issues/12) raises about xload's own `type` package.
With a monotonic reading present, `String()` returns `2026-08-02 14:19:19.300752695 +0530 IST m=+0.000240763`, and the trailing `m=+...` is process-local state written into a config file.

**`url.URL` and `net.IPNet` do not implement `fmt.Stringer` at all**, because the method is on the pointer: verified, `url.URL` does not implement it and `*url.URL` does.
Their rows above are what `fmt.Sprint` produces by falling back to `%v`, which is the struct dump.
So a `Stringer` fallback implemented the obvious way silently produces a struct dump for a value field and a correct string for a pointer field, off the same type.
That is survey item **5.14**'s value-receiver-versus-pointer-receiver defect in another costume, and it is a second independent reason to refuse.

`encoding.TextMarshaler` and `TextUnmarshaler` are the pair a codec chain should consult, and which interfaces are consulted in what order is [#12](https://github.com/onhotpath/ferry/issues/12)'s.
This ADR decides only that `fmt.Stringer` is not among them.

### The pinned `encoding/json/v2` option set

#18 established that "first class json/v2" means an explicitly pinned option set.
The pinning has two halves, and they must be stated together because their agreeing with each other is the whole point.

**Half one: core imitates v2's Go-defined semantics and imports nothing.**
ADR-0002 bars the import, and reading (b) of the survey's four readings costs no dependency.
Verified: the type set above, including the identity table, compiles and runs at `go 1.26` under `GOTOOLCHAIN=local GOEXPERIMENT=nojsonv2`, and builds clean on `go1.27rc2` under `GOEXPERIMENT=nojsonv2`.
**The type set does not consume ADR-0001's `go 1.27` fallback**, which is the same check ADR-0003 ran on the address model.

| v2 semantic | ferry | why |
| --- | --- | --- |
| `omitzero`, defined in Go terms | adopted as the model | portable to a non-JSON plane; the spelling is [#11](https://github.com/onhotpath/ferry/issues/11)'s |
| `omitempty`, defined in JSON terms | not adopted | there is no "empty JSON object" on a Consul plane |
| case-sensitive matching | adopted | ADR-0003 already: core never folds |
| duplicate name is an error | adopted | ADR-0003 already: prefix-free plus driver injectivity |
| deterministic output | adopted | ADR-0001 already, and measured at 1 ordering over 300 |
| nil slice and map are `null` | adopted, and widened to empty | the forced collision above |
| `time.Duration` has no representation | **rejected** | ferry gives it one, and says so |

**Half two: any ferry module that calls `encoding/json/v2` constructs its options from a single pinned set, and never passes none.**
Today no ferry module does.
ADR-0004's first-party driver list is `yaml`, `kv` and `env`, and ADR-0002 puts json/v2 `MarshalerTo` recognition in a sub-module if [#12](https://github.com/onhotpath/ferry/issues/12) wants it.
The set is pinned here anyway, because the first module to import v2 must not be the place this gets decided, and because #18 put it here.

The option surface was enumerated from `go1.27rc2`'s source rather than sampled: nine behavioural options in `encoding/json/v2`, fifteen in `jsontext`, and thirteen legacy-semantics switches in `encoding/json`.

| option | pinned to | why |
| --- | --- | --- |
| `Deterministic` | **true** | measured 8 orderings over 50 at the default, 1 with it. ADR-0001's invariant. |
| `FormatNilSliceAsNull` | **true** | v2's `[]` is a shape ferry's address model cannot name; `null` is what ferry writes for an empty composite. |
| `FormatNilMapAsNull` | **true** | as above. |
| `jsontext.CanonicalizeRawInts` | **false** | measured: turns `1234567890123456789` into `1234567890123456800`. Precision destroyer. |
| `jsontext.CanonicalizeRawFloats` | **false** | saturates above `MaxFloat64` and canonicalises `-0` to `0`. |
| `jsontext.AllowDuplicateNames` | **false** | measured: the default already errors, and ferry's rule is the same rule. |
| `jsontext.AllowInvalidUTF8` | **false** | ferry's `String` may hold non-UTF-8; a JSON plane must refuse it loudly rather than substitute. |
| `StringifyNumbers` | **false** | ferry's decode always knows the target Go type, so precision needs no quoting; quoting would change the plane's bytes for every other consumer. |
| `OmitZeroStructFields` | **false** | omission is per-field and is #11's grammar; a global override would silently change every schema. |
| `MatchCaseInsensitiveNames` | **false** | ADR-0003: core never folds. |
| `RejectUnknownMembers` | **false** | ferry maps a subset of a plane, and ADR-0001 requires keys ferry does not map to survive. |
| `WithMarshalers`, `WithUnmarshalers` | not pinned | the codec chain is [#12](https://github.com/onhotpath/ferry/issues/12)'s and [#19](https://github.com/onhotpath/ferry/issues/19)'s, not a global. |
| all thirteen `encoding/json` legacy switches | **never set** | they exist to restore v1 semantics. `FormatDurationAsNano` and `FormatByteArrayAsArray` are the two that would silently contradict decisions above, and `UnmarshalArrayFromAnyLength` is the one whose v2 default ferry knowingly does not follow. |
| the remaining `jsontext` options | driver's | indentation, escaping, spacing, byte and depth limits change bytes or resource use, not meaning. |

**Why this belongs in the type-set ADR rather than in a JSON driver's.**
If core dumps a struct to YAML and a ferry module dumps the same struct through json/v2, the two must not disagree about nil, about map order, or about integer precision.
Two conversion authorities that disagree is the viper defect the survey measured, where `GetInt` and `Unmarshal` return different answers for one key and one of them is silent.
Pinning both halves in one place is what stops ferry growing a second authority, and the pinned column above is the one artefact that has to be checked against core's own behaviour rather than assumed.

The pinning is provisional in one respect, stated rather than buried: Go 1.27 was still `go1.27rc2` with `"stable": false` when this was measured, so the option set and its defaults are re-verified at GA before ferry ships.

### How round-trip is enforced

ADR-0001 puts the property harness in core as a public testing package and makes it route (b) authority rather than a convenience.
ADR-0002 adds that it is a table over a closed enumerated set with caller-supplied values, so it needs no property-testing dependency.
This section says what the table is.

#### The equality relation is per type, and it is the entry's, not the harness's

`reflect.DeepEqual` is the wrong relation and so is `==`.
Measured: `DeepEqual(time.Now(), roundTrip(time.Now()))` is false because of the monotonic reading, and `NaN == NaN` is false, and `DeepEqual(struct{F float64}{NaN}, struct{F float64}{NaN})` is false.

That is survey item **5.7** arriving from a direction it was not expected from.
5.7 is xload using `reflect.DeepEqual` against a fresh zero value as a "was anything set?" probe, which ADR-0001 assigned to [#8](https://github.com/onhotpath/ferry/issues/8).
The same instinct, reaching for a universal structural equality, would have produced a harness that reports false failures for `time.Time` and every float, and whose obvious repair is to loosen the comparison until it stops reporting them.
So the relation is required at every entry rather than defaulted:

```go
// Proof is one type's discharged obligation. The only way to make one is Type.
type Proof interface{ ... }

// Type builds a proof. eq is required rather than defaulted, because every
// type whose relation is not == is a type whose round trip has a carve-out
// somebody has to have thought about.
func Type[T any](name string, eq func(a, b T) bool, cases ...Case[T]) Proof

type Case[T any] struct {
    Value T
    Want  ferry.Value // the boundary value ferry must produce, see below
}

// Relations for the common shapes.
func Eq[T comparable](a, b T) bool
func BitEq[T ~float32 | ~float64](a, b T) bool
func SliceEq[T any](eq func(a, b T) bool) func(a, b []T) bool
func MapEq[K comparable, V any](eq func(a, b V) bool) func(a, b map[K]V) bool
func PtrEq[T any](eq func(a, b T) bool) func(a, b *T) bool
```

Two ergonomic facts, measured by compiling rather than by reasoning.
`time.Time.Equal` is a method expression of exactly `func(time.Time, time.Time) bool`, so the one entry in core's set whose relation is not `==` needs no wrapper and reads as `Type("time.Time", time.Time.Equal, ...)`.
And inference resolves `T` from the relation, so `Type("int", Eq[int], ...)` needs no explicit instantiation.

`BitEq` compares bit patterns rather than values, which is what makes `NaN` assertable at all, and `SliceEq` and `MapEq` treat nil and empty as one value, which is this ADR's decision rather than a convenience and is commented as such.

#### A proof is a triple, because the property alone is blind to representation

This is the third probe that overturned the draft.

The round-trip property does not constrain the representation, and the flagship type is the demonstration.
Measured: replacing `time.Duration`'s codec with json/v2's `FormatDurationAsNano` shape, so thirty seconds is written as `30000000000`, the round-trip property reports **zero failures**.
Nanoseconds round-trip perfectly.
A property harness alone would have let ferry ship the exact representation it rejects by name three sections above.

So each case carries the boundary `Value` it must produce, and the harness checks it:

```
time.Duration  30s                    ->  string("30s")
time.Time      2026-08-02T12:00:00Z   ->  string("2026-08-02T12:00:00Z")
[]byte         {0x00,0xff,0x41}       ->  bytes("\x00\xffA")
float64        0.1                    ->  number("0.1")
uint64         max                    ->  number("18446744073709551615")
int            -5                     ->  number("-5")
bool           true                   ->  bool("true")
```

This is cheap only because ADR-0004 made `Value` comparable.
The golden column is an `==` against a 24-byte struct, with no serialisation and no bespoke comparison, which is the property ADR-0004 said the harness would rely on, used here for the first time.

**So "adding a type means adding its proof" is concretely: a row carrying values, a relation, and a representation.**
None of the three is derivable from the other two.

#### The values in a proof are load-bearing, and the harness says so

The same probe that showed the harness can go red also showed how narrowly.
Injecting a lossy `float64` codec that formats with `%.6f`, the harness caught **one of four** values: `1.0/3.0` failed, and `0.1`, `math.MaxFloat64` and `NaN` all passed, because a fixed six-digit format happens to be lossless for them.

A table is only as good as its values, which is the honest cost of ADR-0002's ruling that the harness is a table and not a generator.
The mitigation is that the value list for each core type is part of this decision rather than left to whoever writes the test: every core entry carries its zero value, its extremes, and the values that historically break it.
For floats that is `0`, `-0`, `0.1`, `1.0/3.0`, `MaxFloat64`, `SmallestNonzeroFloat64`, `+Inf`, `-Inf` and `NaN`; for integers the zero and both bounds of the width; for strings the empty string, an embedded NUL, non-UTF-8 bytes, and text containing a separator; for composites nil, empty, and one containing an empty element.

#### CI runs the same table against three planes, and the third one is the point

```go
func CoreTypes() []Proof   // core's set, one row per type

// A Source and a Sink over the same plane, which is what a round trip needs.
// Named rather than a pair of parameters because a driver supplying two
// unrelated planes is the mistake it exists to prevent.
type Plane struct {
    Source ferry.Source
    Sink   ferry.Sink
    Kinds  []ferry.VKind // the kinds this plane can carry, see below
}

func RoundTrip(t *testing.T, p Plane, proofs ...Proof)
```

Three planes, because each catches a class the others cannot.

- **The memory plane**, which preserves kinds exactly.
  This is where core's value-fidelity guarantee is stated, because it is the only plane that adds nothing of its own.
  Measured: 11 of 11 core types, 10 of 10 composites, nothing refused.
- **A real driver**, which has a format and I/O.
  Measured through the YAML driver: 11 of 11 and 10 of 10, nothing refused, and a fifteen-address struct with 0 of 15 addresses differing.
  The memory plane alone would have proven nothing about base64, and it was the YAML driver's emitter that surfaced the `!!binary` defect.
- **A flattening plane**, which reports `String` for everything and has no null.
  This is the plane whose absence hid the donor rule for two drafts, and it is not an exotic case: it is env, query and Consul, which is three of ADR-0004's four first-party drivers.
  Measured: 11 of 11 core types with **3 values refused**, and 10 of 10 composites with **13 values refused**.

**The refusals on the flattening plane are the finding, and they are not a bug.**
Every refused value is a nil or empty composite, which the walk writes as `Null` at the container address, and a plane with no null cannot carry it.
Eight of the ten composite types have at least one such value - `map[string]int`, `map[int]string`, `*int`, `*Cred`, `[]Cred`, `[][]string`, `map[string][]string` and `[]time.Duration` - and so do two of the eleven core types, `[]byte` and `[]string`.
ADR-0004's own table says which planes have a null: YAML and JSON can produce one, and TOML, env, query params and opaque KV cannot.

> **Amended under [#41](https://github.com/onhotpath/ferry/issues/41), and the correction is worth more than the numbers.**
> As published this section read "8 of 10 composites" with "the two failures being `*int` and `*Cred` at nil".
> Both halves came from a probe that never touched a plane: the flattening plane was a `flatten` helper, a `map -> map` transform, and it mapped `Null` onto `String("")`.
> That is not a plane with no null; it is **the silent mangling this section's own declaration rule exists to prevent**, so the only two values it reported as failing were the two whose Go type made the mangling visible on the way back.
> Re-measured through `Dump` and `Load` against a real `Source`/`Sink` pair that declares its kinds, the composites pass **10 of 10** - every proof the plane can express - and thirteen values are refused loudly instead of two failing quietly.
> The rule below is unchanged and is what makes the new numbers readable; what changed is that it is now actually exercised.
> Evidence: `X2=7` on [`proto/tip`](https://github.com/onhotpath/ferry/tree/proto/tip).
> A plane that *declares* it carries null and then refuses one is still a **failure** and not a refusal, which the same probe asserts.

So the suite cannot simply demand that every plane pass every proof, and this is new work ADR-0004 did not anticipate:

> A driver declares the `Value` kinds its plane can carry.
> The conformance suite runs the proofs that plane can express, and asserts that the ones it cannot are **refused loudly** rather than silently mangled.

That is ADR-0001's split doing its job at the point it actually bites.
Value fidelity is core's and is measured on the memory plane.
Driver fidelity is the driver's, and "this plane cannot carry a null, so a nil pointer is refused rather than loaded as a zero" is a declared property rather than a failure.
Without the declaration the suite has only two options, both wrong: fail every flat driver, or skip the check and let a nil pointer silently become a zero value.

- **A registrant** runs their own proofs against the memory plane, which is how ADR-0001's "registration carries the proof" is discharged in the registrant's own tests.

> **Amended under [#35](https://github.com/onhotpath/ferry/issues/35): the shape of this section is now [ADR-0014](0014-what-ferrytest-exports.md)'s, and two things in it were wrong.**
> `RoundTrip`'s signature takes Options rather than a `*Registry`, which is where [ADR-0009](0009-typed-codec-registration.md)'s and this ADR's two spellings reconcile.
> And the proof this ADR specifies - values, a relation, and a golden `Value` - had been built twice and never once as specified: the implementation that ran through the entry point had no golden column, and the one with the column ran through a superseded walk.
> ADR-0014 merges them, and the table grows from eleven rows of bare values to nineteen rows and 57 cases, which also closes [#41](https://github.com/onhotpath/ferry/issues/41)'s D18.

**A completeness check closes the loop**, because a table that can be added to without adding a row is a table that drifts.
Core's test iterates the identity table and the admitted kind list and asserts that every member has a proof in `CoreTypes()`, so extending the set without extending the table fails CI rather than silently widening an unproven guarantee.

> **Amended under [#41](https://github.com/onhotpath/ferry/issues/41): the check was red on arrival, and that is the point of it.**
> No prototype had implemented it.
> Written and run, it reported **eighteen admitted members, eleven proof rows, and seven with none**: `int16`, `int32`, `int64`, `uint`, `uint8`, `uint16` and `uint32`.
> So the "11 of 11 core types" above was eleven of eighteen, and the seven integer widths that had never been proved are exactly the ones nobody would think to doubt.
> That is not a defect in the rule; it is the rule working the first time anybody ran it, and it is a green light for the check rather than against it.
>
> **Closed at `0d86c00`**, which is later than the paragraph above and is why this one is in the past tense: the seven rows are in `CoreTypes()`, each carrying the boundary values that distinguish its width from its neighbours, so a codec that silently truncates to a narrower type fails here rather than in production.
> **11 of 11 is 18 of 18**, and `go test ./...` is green.
> Evidence: `TestCoreTypesComplete` on [`proto/tip`](https://github.com/onhotpath/ferry/tree/proto/tip).

### What this ADR does not decide

- Whether an absent address takes a default, and what `Absent` and `Null` mean to a Go field: [#8](https://github.com/onhotpath/ferry/issues/8).
  This ADR decides what a *value* round-trips to and hands #8 the measurement that a container address with no children is indistinguishable from an absent one on every plane surveyed.
- The error types every refusal here produces: [#9](https://github.com/onhotpath/ferry/issues/9)'s convention, applied rather than invented.
- How a field is named, whether a composite's element naming is configurable, and whether embedding is spelled `embed`: [#11](https://github.com/onhotpath/ferry/issues/11).
- Which interfaces the codec chain consults and in what order: [#12](https://github.com/onhotpath/ferry/issues/12).
  This ADR removes exactly one candidate, `fmt.Stringer`, and leaves the rest of the chain alone.
  #12 also asks what happens to a type implementing a decoder with no matching encoder.
  The only part of that answer this ADR owns is the consequence: such a type cannot round-trip, so it cannot be in the guaranteed set, and admitting it would be admitting a member whose proof cannot be written.
  Where that is detected, and whether it is a refusal or a narrower guarantee, is #12's.
- The registration API, including how a named type over `time.Duration` is rescued: [#19](https://github.com/onhotpath/ferry/issues/19).
  This ADR hands #19 two obligations it would not otherwise have: a codec collapses a type to a leaf, which is what makes an interface and a recursive type expressible, and a key codec's text must be injective over the key type or two keys collapse into one address.
  *(Amended under [#31](https://github.com/onhotpath/ferry/issues/31): the second obligation is under Go's `==`, it binds core's own table identically, and it is now backed by a mint-time refusal rather than resting on the registrant alone.)*
- **What counts as a breaking change to plane contents**: [#28](https://github.com/onhotpath/ferry/issues/28), filed from this ADR.
  *(Closed since, by [ADR-0013](0013-what-a-plane-holds-is-a-published-interface.md).
  It reclassifies the golden column below from a test fixture to the published artefact, and it found that the column is on the wrong one of two proof types: the one `CoreTypes()` uses has never checked a representation.)*
  The golden column pins `30s` and RFC 3339 and shortest-float text into every artefact a user has stored, and changing one of them breaks data while the Go API stays stable, which semver does not describe and no tool can see.
  ADR-0002 versioned modules and said nothing about values, because until this ADR nothing had pinned them.
- Where the compiled schema is cached and what the generic entry point looks like: [#16](https://github.com/onhotpath/ferry/issues/16).
- Whether the walk may run concurrently: [#20](https://github.com/onhotpath/ferry/issues/20).
- **Opaque capture of a structured subtree**, which ADR-0004 left open by name.
  It is not wanted, and the type set is where that lands: a `json.RawMessage` field would be `Bytes` only if the plane already holds bytes, and a YAML `servers:` block does not.
  ADR-0004 says the mechanism it would need is a codec claiming a subtree ([#12](https://github.com/onhotpath/ferry/issues/12)) rather than a kind, and this ADR adds the reason it should stay unbuilt: a subtree codec would own a representation core cannot property-test, so it would be the first member of the set whose proof nobody can write.
  If #12 wants it, it arrives through registration and carries its own proof, like every other extension.

## Consequences

- Core's supported set is small and enumerable, and a contributor can read it off one table rather than off the walk.
  The cost is that ordinary types people expect to work, `netip.Addr` and `url.URL` among them, do not until somebody registers them, and the compile error is the only thing standing between that and a silent empty field.
- Only four Go kinds are refused permanently: `chan`, `func`, `unsafe.Pointer` and `uintptr`, and they fail because the value does not exist outside the process.
  Everything else refused is a question of who supplies the codec, because a codec collapses a type to a leaf and a leaf needs no address set.
  That includes the two cases that look impossible, an interface and a recursive type, both measured round-tripping after registration.
  ferry is therefore much less closed than the word "refused" suggests, and the ADR says so rather than letting the list read as a wall.
- The refusal list is partly [#12](https://github.com/onhotpath/ferry/issues/12)'s to shorten.
  `netip.Addr`, `netip.AddrPort`, `netip.Prefix` and `big.Int` all carry an inverse text pair already, so whether they are supported out of the box is decided by whether the codec chain consults `TextMarshaler` before kind.
  So is whether `net.IP` lands as `192.0.2.1` or as sixteen raw bytes.
- There are three outcomes and not two, and the third is the one to watch: a type admitted by kind gets an unpinned representation nobody chose.
  `net.IP` and a `[16]byte` UUID both round-trip exactly and both write raw bytes into the plane.
  Value fidelity holds, legibility does not, and core's guarantee is silent about it by construction.
  This is the price of the same rule that makes `type Port int` work for free, and the documentation obligation it creates is real rather than a formality.
- A `time.Time` that is not UTC loses its zone's DST rules, which is worse than losing its name: arithmetic across a transition gives a different instant, and the `Location` it loads to depends on the reading machine's own zone.
  Measured, `encoding/json/v2` does exactly the same thing, has only two time options and neither touches zones, and its `format:` tag escape was removed in 1.27.
  So there is no stdlib answer to adopt, and the guidance is that a time crossing a plane should be UTC.
- The equality relation in each proof is also the specification of what the harness may not notice.
  `.Equal` cannot see `time.Time`'s zone being discarded, measured at zero failures against a codec that throws it away.
  That is the argument for the golden column stated at its strongest, and it means a contributor choosing a loose relation quietly widens what ferry tolerates.
- Identity-before-kind gives ferry `time.Duration` and `time.Time` with no string comparison anywhere, and leaves one hole: a named type over `time.Duration` dumps nanoseconds.
  That hole is documented, is narrower than xload's, and has registration as its answer.
- The maps-no-address rule turns four stdlib types from silent total losses into compile errors, and it is the single highest-value line in this ADR.
  It also means a struct with no exported fields can never be mapped, which is correct and will surprise somebody.
- `nil` and empty composites are one value, so ferry's guarantee over `[]T` and `map[K]V` is stated with a normalisation rather than as an identity, and the distinction is **not** recoverable by any type in the set.
  This is a knowing departure from the direction #18 argued from, and the argument is that the collision is forced by ADR-0003 and ADR-0004 rather than chosen, and that ferry collapses onto the zero value where v2 manufactures a non-zero one.
- An array and a slice are not interchangeable: an array's addresses are static, so it loads from a source that cannot enumerate, and a slice's are dynamic, so it does not.
  That is a capability difference between two types a user will reasonably treat as the same, and it has to be documented rather than left in the address model.
- On Load `String` is accepted everywhere, which is what makes env, query and Consul usable at all, and it is the one coercion in the design.
  It is bounded by using the leaf's own parser, so ferry refuses seven inputs `cast` silently corrupts, but it is still a coercion and it is the place to look first if ferry ever appears to have guessed.
- Core's value fidelity is guaranteed **over the memory plane**, and a driver honours as much of it as its plane can carry.
  A plane with no null cannot round-trip a nil pointer, measured, and the conformance suite therefore needs each driver to declare its carryable kinds.
  That is a new obligation on ADR-0004's contract, discovered here rather than there.
- The harness is three columns rather than one, and the third exists because the first is blind to representation.
  A contributor adding a type cannot avoid stating what it looks like on a plane.
- Because the harness is a table and not a generator, its coverage is exactly its value lists **and its plane list**, and a gap in either gives a green harness that proves little.
  Measured at one failure caught in four values against a knowingly lossy codec, and at two drafts of this ADR shipping a type set that could not load an integer from an environment variable because every proof ran against a plane that preserved kinds.
  This is ADR-0002's zero-dependency rule being paid for, and it is the weakest part of this ADR.
  The mitigation is that the three planes are named here as a requirement rather than left to whoever writes the test, and that the third one exists because its absence was caught rather than anticipated.
- The pinned json/v2 option set has two halves and only one of them is enforced today, which is worth separating rather than claiming cover for both.
  Half one, the Go-defined semantics core imitates, is executed by core's own tests: determinism, case sensitivity, the nil rule and `time.Duration`'s representation are all in the harness table.
  Half two, the option list a module passes to `encoding/json/v2`, is executed by nothing, because no ferry module imports v2.
  It is a specification awaiting an implementor, and a table nothing executes is prose again within two releases.
  The only honest mitigations are that #18 put it here rather than in a module that does not exist, and that the first module to import v2 inherits a written answer instead of inventing one under deadline.
- ferry's `float64` is wider than JSON's number, so a JSON driver must refuse `±Inf` and `NaN`.
  ferry's `time.Time` loses the zone name, identically to the stdlib.
  Both are documented properties rather than bugs, and both are conformance cases.
- The type set does not consume ADR-0001's `go 1.27` fallback, verified by building it at `go 1.26` under `GOEXPERIMENT=nojsonv2`.
- **A type can be a legal leaf and an illegal key**, because a key type's obligation is stated under `==` and a leaf's under the type's own relation, and `==` is the stricter of the two by construction whenever the two differ.
  `time.Time` is the first instance and it is the reason the distinction had to be written down; before [#31](https://github.com/onhotpath/ferry/issues/31) the design had no place to say it.
  *(Added under #31.)*
- **`map[time.Time]V` no longer compiles**, which costs a use case somebody wants and is forced rather than chosen: no text form is injective over `time.Time`, because `==` compares a `*Location` and no text carries a pointer.
  The remedy is the user's own conversion, and the diagnostic says so rather than offering a codec that cannot exist.
  *(Added under #31.)*
- **Key admissibility is declared per entry and is never inherited**, for core's table and for a registration alike.
  That is one rule where there were two, and it is the same shape [ADR-0007](0007-the-codec-chain-and-its-precedence.md) arrived at independently under [#45](https://github.com/onhotpath/ferry/issues/45) when it reversed its own chain-key sentence: nobody keys a map without somebody having said so.
  *(Added under #31.)*
- **A colliding dump is refused as the address is minted**, and it is the one place in this design where a check is both free and faster than what it replaces, because the determinism invariant had already paid for the sort and the walk was recomputing the key text inside the comparator.
  It is a complete backstop, because keys are only minted on Dump, and it is not sufficient alone, because it fires on the plane being written.
  *(Added under #31.)*

## Items from the xload survey

The survey is [`docs/research/generics-and-modern-go.md`](../research/generics-and-modern-go.md), section 5, against `github.com/gojekfarm/xtools` at [`a90b3aa`](https://github.com/gojekfarm/xtools/commit/a90b3aad2133248cec50f6b4d6e37b0d9e788adb).

**5.7, `reflect.DeepEqual` used as a "was anything set?" probe.**
Addressed here in a form ADR-0001 did not anticipate.
5.7's own instance is [#8](https://github.com/onhotpath/ferry/issues/8)'s and stays there.
What lands on this ticket is the same instinct pointed at the harness: `DeepEqual` is false for a round-tripped `time.Time` and for any struct containing `NaN`, so a harness built on it reports false failures and invites being loosened until it stops.
The relation is therefore per entry and required, and the two relations that are not `==` are the measured reason it is required.

**5.8, type information destroyed at the boundary.**
Addressed, in the half this ADR owns.
ADR-0001 answered the driver half with a stated obligation and a conformance suite, and ADR-0004 made it structural.
The type set is the other half: a Go type maps to a named `Value` kind rather than to `cast.ToString`, and the kind is checked in the golden column of every proof.
Measured through the real YAML driver, a fifteen-address struct round-trips with 0 of 15 addresses differing, including a `null`, a quoted numeric string, a byte blob and a list element containing the separator.

**5.9, the decoder chain is fixed, one-directional and context-free.**
The chain itself is [#12](https://github.com/onhotpath/ferry/issues/12)'s.
Its concrete symptom, `Type.String() == "time.Duration"`, is this ADR's and is answered by the identity table: `reflect.Type` compared with `==`, no strings, no import ferry lacks.
This ADR also removes one candidate from the chain, `fmt.Stringer`, with the measurement that three of six ordinary config types are not inverses under it and that two of the six do not implement it at all at value receiver.

**5.10, composite values are string-splitting, and it is not escapable.**
Addressed, and the half ADR-0003 left open is now closed.
ADR-0003 removed the structural cause with `Index` segments and left "which composites are supported" here.
The answer is slices, arrays and maps, so `Index` segments are used rather than reserved-and-unused.
Measured: `[]string{"a", "b,c", ""}` round-trips exactly through a real YAML plane, where xload's flat form joins to `"a,b,c,"` and splits back to four elements.
What remains genuinely open, a plane holding a whole list in one scalar and a codec splitting it, is [#12](https://github.com/onhotpath/ferry/issues/12)'s and is a choice rather than a defect.

**5.14** was enumerated rather than assumed, all four items.

- *Two ways to set the loader.*
  Bears on nothing in this ADR; ADR-0004 avoided it by construction.
- *The `CanAddr` loop that can only run once.*
  Bears on this ADR, since it is a defect in the reflection walk and this ADR specifies what the walk does per kind.
  It is not carried over: the walk addresses a field once, and the prototype's walk contains no such loop.
- *The non-deterministic select on a cancelled context.*
  Concurrency, and [#20](https://github.com/onhotpath/ferry/issues/20)'s.
  Nothing in the type set constrains it.
- *Value receivers on `Error()` where pointers are returned.*
  Bears on this ADR twice.
  The error types it produces are deferred to [#9](https://github.com/onhotpath/ferry/issues/9)'s convention rather than pre-empted, as ADR-0003 and ADR-0004 both did.
  And its underlying cause, that a method set differs between a value and its pointer, is the second independent reason `fmt.Stringer` is refused: measured, `url.URL` does not implement it and `*url.URL` does, so a fallback would behave differently for two spellings of one type.

The remaining items are unaffected by this ADR.
