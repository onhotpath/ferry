# 3. How a leaf field addresses a plane

Status: Accepted
Date: 2026-08-02
Ticket: [#4](https://github.com/onhotpath/ferry/issues/4)

## Context

Every leaf field in an annotated struct names a location in the plane.
ferry inherits from xload the assumption that the name is a flat string, joined from a prefix and a key by whatever separator the tag happened to spell.

That assumption is lossy on its own terms, before any question about value typing.
The survey measured koanf's `Flatten` resolving `{"a.b":1}` against `{"a":{"b":2}}` nondeterministically, 255 to 45 over 300 runs, and viper silently destroying two of three case-variant keys.
[ADR-0002](0002-core-and-sub-modules.md) named this ticket as the one whose answer the `ferrytest` memory plane has to make executable.

This ADR decides the address type, the case rule, and the collision rule in both directions.
It is written from a throwaway prototype, on branch `proto/4-address-shape`, which never merges.
Every number below is from that prototype unless it cites the survey.

## Decision

### The address is a structured path, and core never joins it

A leaf addresses a plane by an ordered, non-empty sequence of **segments**.
Core does not produce a plane key, because a separator is plane knowledge and producing one would require core to know what the plane is.

> Flattening is the driver's, always.

This is the plane-agnosticism veto applied to the address.
An environment driver joins segments with `_`; a YAML driver walks them as a tree; a Registry driver walks them as subkeys.
None of that is core's business, and the prototype's env and dotted key functions are five lines each.

### A segment carries a kind, and the kind set is closed at two

A segment is a kind and a text.
The kinds are **Name**, an object member or struct field or map key, and **Index**, a position in a sequence.
Adding a third kind is an amendment to this ADR.

The kind is not decoration, and this is the measured reason for it.
This ticket is gated by template generation, which has to emit nested YAML.
Given only string segments, an emitter has one signal for "is this container a list", which is whether the segment looks like a base-10 integer.
Measured: a schema with `servers []Elem` and `labels map[string]string{"0":..., "1":...}` emits correctly from kinded segments, and the guessing emitter turns `labels` into a YAML sequence and destroys the key text, which no later stage recovers.

This is not a flaw in the guessing emitter.
It is the limitation `jsontext.Pointer`'s own godoc states: "It is impossible to distinguish between an array index and an object name (that happens to be a base-10 encoded integer) without also knowing the structure of the top-level JSON value" ([state.go](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/jsontext/state.go)).
Verified on `go1.27rc2` rather than taken on trust: marshalling `[]Elem` and `map[string]Elem` with a key of `"0"` produces the identical leaf pointer `/servers/0/host` for both.

### The canonical form: `jsontext.Pointer`'s property, copied, not imported

An address has a **canonical text rendering with a unique representation**, so that two addresses are equal exactly when their renderings are equal.
That is the property `jsontext.Pointer` states about itself, and it is the property a flat key space lacks.

Four things follow from it, and they are why the rendering is a string rather than a `[]string`:

- The address is **comparable**, so it is usable as a map key and a set element with no encoding step at the call site.
  A `[]string` address is not, and every call site would encode anyway.
- Identity is `==`, so the collision rules below are a map insert rather than a nested comparison.
- Errors and diagnostics have one spelling for one address.
- Determinism has something stable to sort, which ADR-0001 makes a package-wide invariant.

> **Corrected: ADR-0001 states no determinism invariant, and this ADR credits it with one in four places.**
>
> As published this line, two of the bullets under [Ordering is segment-wise](#ordering-is-segment-wise-and-this-is-not-the-same-as-sorting-the-rendering), and the map-key argument later on all rest an argument on "ADR-0001's determinism invariant".
> [ADR-0001](0001-what-ferry-supports.md) contains no such sentence and never did; the word does not appear in it.
> [ADR-0004](0004-source-and-sink.md) inherited the same citation once.
>
> **The property is real and this ADR is where it is stated.** What holds is that a report and a dump are single-valued for one input, and the evidence is on this page: the error report sorts at construction, verified against two reversed `Get` sequences producing one identical report, and the dumped output is sorted at the write loop.
> **What is wrong is only the attribution**, so nothing in the decision moves: a comparable, sortable address is still what the sortedness rests on, and the measurements quoted for it stand.
> Read the requirement as this ADR's own rather than as one inherited, and do not go looking for it in ADR-0001.

**Core copies the property and does not import the type.**
ADR-0002 bars `encoding/json/jsontext` from core, and this ADR does not reopen that.
The prototype's address type was extracted into a module at `go 1.26` and tested green under `GOTOOLCHAIN=local GOEXPERIMENT=nojsonv2`, so the address model is core-eligible and does not consume ADR-0001's `go 1.27` fallback.

**ferry's canonical syntax is its own, and RFC 6901 is consciously rejected as the wire form.**
The reason is the one measured above: RFC 6901 cannot express the kind of a segment, and this ticket exists because template generation needs it.
Adopting a pointer syntax that cannot say what ferry needs to say, and then carrying the kind alongside it, would be worse than either adopting it or not.
The escaping model is taken from RFC 6901, because escaping a separator and an escape character is a solved problem and there is no reason to solve it differently.
The exact byte spelling is an implementation choice constrained by the four properties above; the prototype's spelling survived 200,000 fuzzed paths with zero round-trip failures and zero collisions, over segment text including the separator, the escape character, escape lookalikes, an embedded NUL, non-ASCII, and the empty string.

The canonical rendering is **not a plane key**, and no driver may write it into a plane as one.
`ferrytest`'s memory plane may key by it, and it may do so for exactly the reason ADR-0002 admits it to core at all: it has no serialization format, so it is a map rather than a plane.

### Ordering is segment-wise, and this is not the same as sorting the rendering

Both orders are deterministic, so ADR-0001's invariant is satisfied either way, and only one of them is the order a human diffing a dumped file expects.

Measured, twelve indices sorted by canonical bytes: `0 1 10 11 2 3 4 5 6 7 8 9`.
Sorted segment-wise, comparing Index segments numerically: `0 1 2 3 4 5 6 7 8 9 10 11`.
The two orders also disagree on plain names, because a separator byte sorts against ordinary text: `/a/b` and `/a-x` swap places between the two.

So wherever ferry enumerates addresses, in dumped output, in error reports, and in the conformance suite, it sorts segment-wise.
The canonical rendering is for identity, not for ordering.

**The three places are the whole list, and the sequence in which core asks a `Reader` for addresses is deliberately unspecified.**
It is not a fourth place and it is not an oversight: the walk is free to visit in whatever order it likes, and to visit in no order at all once [#20](https://github.com/onhotpath/ferry/issues/20) makes it concurrent.
[ADR-0011](0011-the-error-model.md) already assumes this when it says "the walk may emit in any order".

*(Clause added under [#41](https://github.com/onhotpath/ferry/issues/41), which asked whether "wherever" covers a `Get` sequence, and answered no from measurement rather than from taste.)*

Four things were measured before deciding, and each on its own would be enough.

- **Nothing ferry produces exposes the sequence.**
  ADR-0006's presence observation is a **mapping** from address to `Value`, so a lookup cannot see an order; the error report sorts at construction, verified by running two `Get` sequences that are reverses of each other and getting one identical report; the dumped output is sorted at the write loop and does not go through `Get` at all; and the conformance harness compares values at addresses and has no sequence assertion.
  A lazy driver sees its own call order, which is the driver's, and the batch branch of the same driver has no per-`Get` call to order.
- **ADR-0001's determinism invariant is already satisfied, and by the property it actually asks for.**
  Over 200 runs across eight shapes the sequence is single-valued every time - 1 distinct sequence per shape - including a map of structs, whose members come from `Children`.
  Determinism and sortedness are different properties and ADR-0001 asks only for the first.
- **The extension cannot be delivered in full.**
  A promoted embedded struct contributes **no segment**, so it has no sort key, and sorting a parent's field list cannot interleave a promoted block into it: measured, the per-node sort gives `/env /name /alpha /zeta` where segment-wise is `/alpha /env /name /zeta`, and no key produces the latter.
  A dynamic composite's members come from the driver's `Children`, which core does not sort, so a map would stay in the driver's order whatever the walk did.
- **A partial promise is worse than none**, because this ADR's own wording would make it a conformance case, and it would collide with #20 the moment the walk goes concurrent.

The one address list that does cross the driver boundary as a list - the static address set handed to `Bind` - **is** sorted segment-wise, which is what lets a driver that wants locality sort for itself.
Evidence: `X4=6..11` on [`proto/tip`](https://github.com/onhotpath/ferry/tree/proto/tip).

*(Under [#56](https://github.com/onhotpath/ferry/issues/56): what that list **contains** is settled in [An address is a place a `Value` can be](#an-address-is-a-place-a-value-can-be-and-a-container-has-one).
It is every leaf address the type determines plus every container address, and it is never a wildcard shape.)*

> **Amended under [ADR-0016](0016-the-sealed-address-model.md): the set is typed, and "every container address" is more addresses than it was.**
>
> As published, a container address was one a composite that can be nil occupies, so a plain nested struct and an array took none: only a pointer, a slice and a map contributed one.
> Under the sealed address model an address carries what kind of place it names, and a plane is asked whether a section is there, so a section with no address of its own is a question that cannot be asked.
> Every nested struct and every array now contributes a `SectionAddr`, a slice and a map contribute a `CompositeAddr`, and a pointer contributes at the kind of what it points at.
> The set is otherwise unchanged: it is still sorted segment-wise, still holds nothing a value mints, and still never holds a wildcard shape.

### The case rule: core never folds, confirmed

Core compares segment text by exact byte equality.
It never folds case, and it never normalizes.
The survey's recommendation is confirmed, and this ADR adds two reasons to the one it gave.

- **Folding manufactures collisions**, because it is many-to-one.
  That is not a hypothetical: it is what the survey measured viper doing, destroying two of three case-variant keys with no error and the winner decided by map iteration order.
- **Which characters fold is plane knowledge and locale knowledge**, so folding in core fails the plane-agnosticism veto outright.
  Environment variable names are case-insensitive on Windows and case-sensitive on Unix, and the same library cannot fold correctly for both.
- **Folding breaks driver fidelity.**
  ADR-0001 obliges a Load then Dump cycle to leave the plane meaning the same thing over the keys ferry mapped.
  A fold discards the original spelling, so Dump has nothing to restore it from.
- The newest design in the neighbourhood agrees.
  `encoding/json/v2` matches object names case-sensitively by default and makes folding an explicit per-field `case:ignore` opt-in.

A driver **may** fold, as part of its key function, when its plane genuinely is case-insensitive.
It is then caught by the driver-side rule below rather than being trusted, which is the whole difference from viper.

Whether the tag grammar offers a per-field case option is [#11](https://github.com/onhotpath/ferry/issues/11)'s.

### The collision rule, core side: the address set is prefix-free

> A compiled schema's **leaf** addresses contain no address that is a prefix of another.
> A path is a prefix of itself, so this subsumes exact duplicates.
> A **container** address is a proper prefix of what is under it by construction, which is what makes it a container, and it is exempt.
> It must still be distinct from every leaf address.

> **Amended under [#56](https://github.com/onhotpath/ferry/issues/56): the rule is over the leaf addresses, and as published it was over all of them.**
> As published this read "A compiled schema's address set contains no address that is a prefix of another", written before [ADR-0005](0005-the-supported-type-set.md) established that a composite has an address of its own.
> Taken literally the unqualified rule refuses every schema ADR-0005 requires: `/Tags` is a prefix of `/Tags#0`, and `/Opt` of `/Opt/User`.
> ADR-0005 saw half of it and called it "a constraint on how the static check is written, not a change to the rule".
> It is the rule's wording, and it is corrected here.
> **Nothing in the argument moves.**
> The hazard the rule exists for is a leaf and a subtree sharing a segment, measured on a tree emitter, and a container address is the node the subtree hangs from rather than a second value competing with it.
> Evidence: `X3=4b` on [`proto/tip`](https://github.com/onhotpath/ferry/tree/proto/tip).

Violation is a schema compilation failure listing every clash.
For the part of the address set the type determines, it is checked from `reflect.TypeFor[T]()` alone, with no plane reachable, no value in hand, and identically in both directions, which is the same assertability ADR-0001 claims for tag rejection.
That qualification is load-bearing and is spelled out in the next section rather than buried here.
Measured: 300 compiles of a colliding type produced exactly one error string.

**This is where the dump-side question the ticket absorbed actually dies.**
The ticket described dump-side collision as silent data loss with a nondeterministic winner.
The prototype refines that, and the refinement strengthens the case rather than weakening it.
Because `reflect` field order is source order, a **serial** dump loses **deterministically**: 300 dumps of three fields sharing one key produced the third value 300 times out of 300.
A **concurrent** dump is a genuine race: the same three fields produced a shifting split across all three values from run to run.
The deterministic case is the more dangerous of the two, because it is reproducible and therefore invisible in a test.
Neither survives this rule, because the schema does not compile, so no dump runs.
Making the failure a schema property rather than a runtime rule is what makes it unrepresentable instead of merely detected.

**Prefix-freeness, rather than duplicate detection, is the rule, and the prototype found out why by accident.**
A leaf at `/db` and a subtree under `/db` are two distinct addresses, so a duplicate check accepts them.
A flat plane holds both happily, as `DB` and `DB_HOST`.
A tree plane cannot: measured, writing the pair into a tree emitter leaves the scalar at `db` gone, and reversing the write order loses the other one instead.
Core adopts the constraint that tree planes impose, because that is what makes a compiled schema representable on **every** plane rather than on the one it was written against.
Plane-to-plane transfer is Enabled in ADR-0001 and falls out of the pluggable design for free; it does not fall out for free if a schema can be loadable from env and undumpable to YAML.

The cost is small and worth stating exactly, because it is easy to overstate.
`DB` and `DB_HOST` as two flat single-segment addresses are prefix-free and remain legal, since the prefix relation only holds at segment boundaries.
What becomes illegal is a struct that puts a leaf and a nested struct at the same segment, which is a schema nobody writes deliberately.

### Not every address comes from the type, so both rules run in two tiers

A struct field's address is a property of the type.
A map key's address, and a slice element's index, are properties of the **value**.
So "checked at schema compile from the type alone" is true of part of the address set and false of the rest, and this ADR says which rather than letting the stronger claim stand unqualified.

Measured on one type with two values, both walked with the same transforming env driver:

```
/limits/http_port      static
/limits/extra          DYNAMIC: keys come from the value
/labels                DYNAMIC: keys come from the value

value 1  ->  /limits/http_port  /limits/extra/burst  /labels/env              accepted
value 2  ->  /limits/http_port  /limits/extra/http.port  /limits/extra/http_port
             refused: "LIMITS_EXTRA_HTTP_PORT" <- /limits/extra/http.port and /limits/extra/http_port
```

Nothing about the type differs between those two runs.
A user can therefore write a schema that compiles, passes every driver check, and is refused later because of what a map contained.

**Both rules therefore run at two points, and neither point is after a write.**

- **Static addresses**, from struct fields: at schema compile, with no value and no plane, exactly as claimed above.
- **Dynamic addresses**, from map keys and sequence lengths: as each is minted, before the write it belongs to.
  This is an insert into the set the static pass already built, not a re-check of everything, so it stays a map insert per address.

ADR-0001's prohibition on silently ignoring anything is honoured either way, because the dynamic tier fails the operation rather than overwriting.
What is honestly weaker is the promise: a dynamic collision is caught before data is lost but **not** before the program runs, and no amount of design moves a map key into the type.

**On Load this is a genuine constraint on [#5](https://github.com/onhotpath/ferry/issues/5), not a note.**
Dump knows a map's keys and a slice's length because it holds the value.
Load does not, so a dynamic address is only reachable if the source can enumerate.
Two consequences #5 has to take as given rather than decide freely: the source contract has to hand a driver the whole address set before I/O, because the driver-side rule is unrunnable otherwise, and dynamic segments on Load are gated on enumeration existing at all.
If #5 concludes that sources never enumerate, then map-keyed and indexed addresses are Dump-only, which is a real asymmetry and belongs in #5's ADR rather than being discovered later.

### An address is a place a `Value` can be, and a container has one

*(Section added under [#56](https://github.com/onhotpath/ferry/issues/56), which measured two prototype engines producing two different static sets for one compiling type and asked which is the published one.)*

The split above says which addresses the type determines and which the value does.
It does not say what makes something an address at all, and two later ADRs answered that question without noticing they were answering it.

> An address is a place a plane can be asked for a `Value`, or handed one.
> Nothing else is an address, whatever else the compiler knows about it.

"Derivable from `reflect.TypeFor[T]()` alone" is necessary and it is not sufficient.
A map field's element shape is derivable from the type too, and there is nothing at it.

**A composite has an address of its own, and it is in the static set.**
[ADR-0005](0005-the-supported-type-set.md) decided that a composite with no elements writes `Null` at its own address, and measured `Get(/tags)` and `Children(/tags)` at that same address through a real YAML plane.
[ADR-0006](0006-defaults-and-zero-values.md) reads presence there, and [ADR-0014](0014-what-ferrytest-exports.md) makes both a driver conformance case.
So a driver is asked about `/Tags` in both directions.
A driver that was never shown `/Tags` at `Bind` cannot precompute its key, cannot include it in a batch fetch, and cannot check legality or injectivity for it before any I/O, which is what this ADR's two-check rule exists for.

**A container address carries `Absent` or `Null` and never anything else**, which is why admitting it costs this ADR nothing else.
It is not a group arm, which [ADR-0004](0004-source-and-sink.md) refused and still refuses.
Both of its possible observations mean "there is nothing under here", so it is never realised at the same time as anything beneath it and can never compete with a child for a value.
That is also why it is exempt from prefix-freeness above.

> **Amended under [#207](https://github.com/onhotpath/ferry/issues/207): the order core asks in is part of the rule, and as published this section said nothing about it.**
> As published this paragraph said what a container address may hold and stopped there.
> That silence turned out to be load-bearing for a plane whose key space is `map[string][]string`, such as HTTP query parameters or headers, where the name carrying a sequence's values **is** the name the container sits at.
> `?tags=a&tags=b` puts a value at exactly the container address, and a driver cannot route around it: an address arrives at `Get` as one `Name` segment with no kind and no arity, so `Get(/tags)` for a `[]string` and `Get(/q)` for a `string` are the same call.
>
> **The rule itself does not move.**
> A container address carries `Absent` or `Null` and never anything else.
> Only the order changes, and only for sources that enumerate.
>
> What is added is the order, and the corollary a driver may rely on:
>
> > At a slice or a map, over a source implementing [`Enumerator`](0004-source-and-sink.md), core asks `Children` before it asks the container's own address, and asks the container's own address only where `Children` returned nothing.
> > So **being asked for children at an address is core saying that address is a dynamic container**, and it is the only such signal a driver gets.
>
> That is an obligation on the optional interface as well as a permission: a `Reader` implementing `Enumerator` must be prepared for `Children` at an address its own `Get` would have answered a value at.
>
> A pointer is unaffected and is still asked `Get` first, because it has no children to enumerate and a `Null` at it is a complete answer.
> An array is unaffected, because it has no container address.
> A source that does not implement `Enumerator` is unaffected in every observable way, down to the wording of the refusal it gets at a dynamic address, and the reason the old order gave for itself was always about that source: a `Null` at the container address is a complete answer and a source that cannot list can still give it.
>
> It costs one call each way, and the cost only ever lands on a plane that can report `Null` at a container address.
> Asserted at the boundary in core's own suite: a populated dynamic container is `Get=2 Children=1` where it was `Get=3 Children=1`, and a container answering `Null` is `Get=1 Children=1` where it was `Get=1 Children=0`.
> Among the shipped drivers only `yaml` reaches the second row, and its `Children` is an in-memory node walk.
>
> **What the order takes away is stated rather than left to be discovered: an answer at a container address that has children under it is never read.**
> A driver can no longer refuse there, and a `Null` there no longer wins over the children.
> Measured on `proto/193-multimap`: a query plane reads `?tags=a&tags=b&tags.0=z` as `{Tags:[z]}` under the new order, where under the old one it refused.
> Neither was a place a driver was entitled to answer from.
> [ADR-0014](0014-what-ferrytest-exports.md)'s third conformance case already forbids failing at a container `Get`, and calls `Get` there itself outside the walk, so no reordering inside core rescues a driver that refuses there; and a plane reporting `Null` at an address it also lists children under is contradicting itself.
>
> [ADR-0006](0006-defaults-and-zero-values.md)'s does-not-write row was checked against the new order rather than assumed unaffected, and it survives unchanged.
> `Children` empty then `Get` `Absent` is the same two observations in the other sequence, so a container with no children is still indistinguishable from an absent one and a seeded container still keeps what it had.
> ADR-0006's own statement of the replacement rule, that "the plane either has children under that address or it does not, and if it has any then it has said what the composite is", is the question the new order asks first.

**Which Go types get one is read off the type**, and the test is whether the value at that address can be nil:

| field type | container address | why |
| --- | --- | --- |
| `[]T`, `map[K]V` | **yes** | nil and empty are one observation there, ADR-0005 |
| `*struct`, `*[]T`, `*map[K]V` | **yes**, exactly one | a pointer adds no second bit, ADR-0005 |
| `struct` | **no** | it cannot be nil, so `Null` at it is refused and `Absent` at it says nothing its fields do not |
| `[N]T`, `[N]byte` | **no** | an array has no nil, ADR-0006's own row |
| `[]byte` | **no**, it is a leaf | admitted at kind `Bytes`, so its address is a leaf address that happens to accept `Null` |

The line between `struct` and `*struct` is not a nicety.
ADR-0006 makes `required` on a `*Cred` satisfied by `auth: null`, and materialises the pointer from a presence bit that an explicit `Null` at `/auth` clears.
Both are statements about a value at `/auth`, and neither is writable if `/auth` is not an address.
A non-pointer `Auth Cred` gets none for the same reason read the other way.
And an address appearing in a **diagnostic** is not thereby in the set: ADR-0006's `ferry: /auth: required, and the plane supplied nothing under it` names a path whether or not a driver was ever shown one.

**A wildcard is not an address, and it never crosses the driver boundary.**
ADR-0006 attaches a declaration to the static address *shape*, `/servers/*/port`, and [ADR-0010](0010-the-entry-point-and-the-schema-cache.md) has a compiled leaf hold one.
That is a schema-internal lookup key and it is the walk's own, exactly as ADR-0006 describes it: "the walk carries two paths, the realised one it asks the plane about, and the static one it looks declarations up by".
Only the first is an address.

Handing a shape to a driver is wrong three ways, and the third is silent.
There is nothing at it to fetch, so a batch source spends a round trip on a key that cannot exist.
There is nothing at it to write, so a sink's precomputed table holds an entry for the shape and none for the container address the walk calls `Set` at for an empty composite.
And `*` is ordinary segment text under this ADR's own escaping model rather than a marker, so a schema whose map genuinely holds the key `*` renders one plane key for two members of the set, and the injectivity check either reports a collision that is not one or misses one that is.

Evidence: `X3=4b` on [`proto/tip`](https://github.com/onhotpath/ferry/tree/proto/tip).

### The collision rule, driver side: the key function is injective over the address set

Keeping the address structured does not abolish flattening collisions.
It relocates them to the only place that has the information to resolve them, and it makes the obligation checkable.

> A driver's mapping from ferry address to plane key must be injective over the address set of the schema it was given.
> If it is not, the driver fails before any I/O.

*(Clarified under [#56](https://github.com/onhotpath/ferry/issues/56): "the address set" here is the whole static set, container addresses included, and it is **not** narrowed the way the core-side prefix rule above is.
Two containers rendering to one plane key return one merged subtree from `Children`, which is the same silent merge this rule exists to catch.)*

> **Amended under [ADR-0016](0016-the-sealed-address-model.md): injectivity is per kind, and a composite reserves the key space below its own key.**
>
> As published, and as shipped when the address set grew, the rule ran over every address at once: one key per address, whatever kind of place the address named.
> That is the right rule while a container address is only ever a container, and it became the wrong one when a section took an address of its own.
> Measured on the shipped `NewKeys`, `struct { A Leafy "a"; B string "A" }` was refused on the env plane with "renders this address and /A to one plane key", and nothing would have been lost: the leaf is read at `A`, the section is only ever scanned at `A_`, and the plane still tells the two apart.
>
> **What a key is for is part of the question.**
> A flat driver reads a value at a leaf's key and never at a container's, and it uses a container's key as the prefix its members are named under.
> So the check now runs within a kind: leaf against leaf, container against container.
>
> **The same widening hid a collision that is real, and closing it is a new refusal class.**
> A composite's members come from the value, so a flat driver has no table to check them against: it lists every plane key beginning with the composite's own and reads what it finds as a member.
> A leaf of the same schema whose key begins with a composite's key, and which is not under that composite, is enumerated as one of its members - one value at two addresses, which is the loss this rule exists to prevent, and the old check never looked for it.
> A composite therefore reserves the key space its own key begins, and an address of the schema rendering into it is refused at `Bind`, naming both.
> A section reserves nothing, because its members come from the type and a driver can ask about exactly those.
>
> The new refusal is conservative on one point, stated rather than hidden: core cannot see a driver's separator, so it reserves the whole byte prefix.
> A composite at `TAGS` and an unrelated leaf at `TAGSMODE` are refused although no separator join would confuse them.
> Rename one, or nest it under the composite where the model would put it.

The conformance suite ADR-0002 puts in core checks it, which is what stops it being prose.
This one rule covers separator collisions, case folding, and any normalization a driver invents, because all three are the same failure: a non-injective map out of the address set.

**A driver is expected to transform segment text, not to reject it, and the injectivity rule is what makes that safe.**
This is the one place the prototype changed the answer.
An environment variable name may not contain a hyphen, so a driver that only validates rejects a segment `feature-flags`, which is an ordinary thing to write in a config struct.
A driver that maps illegal characters to `_` accepts it, and is not thereby less safe: measured, the transforming driver accepts `feature-flags` alone and rejects `feature-flags` alongside `feature_flags`, naming both addresses.
Transformation is folding, folding is many-to-one, and a many-to-one map out of the address set is precisely what the injectivity check exists to catch.
A driver that refuses to transform is not safer than one that does, it is only less useful.

So a driver runs **two** checks over the address set before any I/O, and they are different questions.
Legality asks whether the plane can name this address at all, which no transformation can rescue: an empty segment has no environment variable name, and a segment containing a backslash has no Registry name.
Injectivity asks whether the transformation it chose collapses two addresses into one.

Measured, over four address sets and four key functions:

| Address set | env, uppercase and `_`, no character transform | env, no fold and `_`, no character transform | dotted, no fold | env, uppercase and `_`, illegal byte to `_` |
| --- | --- | --- | --- | --- |
| `/DB/HOST`, `/DB_HOST` | rejected | rejected | ok | rejected |
| `/myKey`, `/MyKey`, `/MYKEY` | rejected | ok | ok | rejected |
| `/db.host`, `/db/host` | ok | ok | rejected | rejected |
| `/db/host`, `/db/port`, `/cache/host` | ok | ok | ok | ok |

Two things to read out of that table.
The first row is the collision a flat key space creates and cannot see, appearing as a driver error before any I/O rather than as a silent overwrite.
The second row is viper's measured bug, and it is now an error that names both offending addresses.
No key function is universally right, which is precisely why the rule is stated over the schema rather than over the key function.

> **Amended under [#125](https://github.com/onhotpath/ferry/issues/125): every column says what its key function does, and the transforming driver this section argues for has a column of its own.**
> As published the header row read `env, uppercase and _`, `env, no fold and _` and `dotted, no fold`, over three key functions, and it did not say that none of the three transforms segment text.
> **No published cell moves and nothing is re-measured.**
> All twelve reproduce under exactly that reading, which is the only reading under which every passage of this section holds: the two env columns are a case fold and a join separator and nothing else.
> What was missing is the fourth column.
> The paragraph above the table argues for a driver that maps an illegal byte to `_`, and that driver is none of the three the table measured, so a reader building an env driver from the prose and a reader building one from the table's first column got different answers.
> Row 3 is the whole of the difference: `/db.host` and `/db/host` stay apart under a key function that only folds case, and fold together under one that also maps `.` to `_`.
>
> The fourth column is the key function `driver/env` ships, and its cells are read off that driver rather than off a prototype.
> It folds a byte at a time, upper-casing a letter, keeping a digit and mapping everything else to `_`, so `/db.host` and `/db/host` both render `DB_HOST` and a schema holding both is refused at `Bind` naming both addresses.
> This section's own argument reproduces at the same seam: `feature-flags` alone renders `FEATURE_FLAGS` and binds, and `feature-flags` beside `feature_flags` is refused before any I/O.
>
> The fold is total, so nothing reaches that driver's legality check by way of a character.
> What it refuses as illegal is a shape no fold rescues: an empty segment, and a name beginning with a digit, which no shell can set.
> That is the legality half of the two checks this section separates, and it is unaffected by the column being added.

**The check needs the whole address set up front, and the compiled schema has it before any I/O.**
That is the same precondition a batch or snapshot source needs, so the collision rule and the capability the survey identifies as the biggest non-generics win in section 5.13 are unlocked by the same fact.

**It is not on the hot path.**
Because the address set is known before I/O, a driver's plane keys are computed once per schema and cached, not recomputed per lookup.
Measured: a precomputed lookup is 10.4 ns and zero allocations against 8.5 ns and zero allocations for a bare flat-map lookup.
Computing the key per call instead costs 109 ns with a segment iterator, or 477 ns with a segment slice, against a 476 ns twelve-key cached load in the survey's own prototype, which is what makes caching it a requirement and not an optimisation.
So the structured address costs roughly two nanoseconds a key against a flat string, and any larger number in this area is an implementation that forgot to precompute.

### The separator is a driver option, and no separator is universally safe

`METRIC_HTTP_PORT` is the question this whole ADR has to answer well, so it is answered here in full.
Is it `metric.http.port`, or `metric.http_port`?

**In core it is not a question**, because ferry never parses a plane key back into a path.
`/metric/http/port` has three segments and `/metric/http_port` has two, they are distinct addresses under every circumstance, and no option changes that.
Ambiguity only exists for a design that has to recover structure from a flattened string, and keeping the address structured is what removes the need.

The question survives only as a driver question: does the env driver's join keep them distinct?
The join is therefore a **driver option**, in keeping with flattening being the driver's, and the injectivity check is what makes choosing it safe:

| env join | `/metric/http/port` | `/metric/http_port` | verdict |
| --- | --- | --- | --- |
| `_` | `METRIC_HTTP_PORT` | `METRIC_HTTP_PORT` | rejected, naming both addresses |
| `__` | `METRIC__HTTP__PORT` | `METRIC__HTTP_PORT` | ok |

That is the same `__` answer xload reaches, arrived at differently, and the difference is the whole point.

**Measured against the prior art**, on the same pair, at `a90b3aa` for xload and at koanf v2 and viper v1.21.0:

| | mechanism | what happens on a collision |
| --- | --- | --- |
| xload `FlattenMap` | separator passed by the caller | at `_`, **258/42 nondeterministic** over 300 runs, no error |
| xload `FlattenMap` at `__` | as above | distinct and stable, until a key contains `__`, then **261/39 nondeterministic** |
| xload struct side | `prefix=` text concatenation | **detected**: `key collisions detected for keys: [METRIC_HTTP_PORT]`, and `SkipCollisionDetection` turns it off |
| koanf | delimiter fixed at `koanf.New(delim)` | last load wins, silently, no error |
| viper | `SetEnvKeyReplacer` | `_` collides, `__` does not, nothing checks either way |
| ferry | driver-chosen join, checked per schema | refused before any I/O, naming both addresses, 300 of 300 runs |

Two things to take from that table, and the first is a credit rather than a criticism.

**xload already detects this on the struct side, and it is right to.**
Its collision counter catches `prefix=METRIC_` plus `METRIC_HTTP_PORT` and errors by default.
What ferry changes is where the check sits and what it covers: xload checks the flattened key at Load time, which conflates the two collision rules this ADR separates, is switchable off, and has no Dump side to protect at all.
ferry's core-side rule runs at schema compile with no plane in sight, and its driver-side rule runs per driver, so a schema that is fine on YAML and impossible on env is reported as an env problem rather than as a ferry problem.

**A wider separator buys a bigger margin and never a guarantee.**
Measured on ferry's model with segments that themselves contain `__`: `sep="__"` is refused, naming both addresses, where xload at the same separator is 261/39 nondeterministic and silent.
This is why the rule is stated over the schema and not over the separator.
There is no separator a driver can pick that is safe for every schema, because segment text is the user's, so the only honest guarantee is one that is checked against the schema in hand.

### What this looks like on four planes

Worked from the prototype rather than by hand.
The tag spellings are illustrative, because the grammar is [#11](https://github.com/onhotpath/ferry/issues/11)'s; what is being shown is the address set and its renderings.

```go
type Cred struct {
    User string `ferry:"user"`
    Pass string `ferry:"pass"`
}
type DBConf struct {
    Host string `ferry:"host"`
    Port int    `ferry:"port"`
    Auth Cred   `ferry:"auth"`
}
type AppConf struct {
    Name    string         `ferry:"name"`
    DB      DBConf         `ferry:"db"`
    Tags    []string       `ferry:"tags"`
    Limits  map[string]int `ferry:"limits"`
}
```

| ferry address | env | Windows Registry | query param |
| --- | --- | --- | --- |
| `/name` | `NAME` | `HKCU\Software\Acme : name` | `name` |
| `/db/host` | `DB_HOST` | `HKCU\Software\Acme\db : host` | `db.host` |
| `/db/auth/user` | `DB_AUTH_USER` | `HKCU\Software\Acme\db\auth : user` | `db.auth.user` |
| `/tags#0` | `TAGS_0` | `HKCU\Software\Acme\tags : 0` | `tags.0` |
| `/limits/rps` | `LIMITS_RPS` | `HKCU\Software\Acme\limits : rps` | `limits.rps` |

The YAML driver never produces a key at all, because it walks the segments as a tree.
The Registry driver reads the address the way the Registry is actually shaped: every segment but the last is a subkey, and the last is a value name.
None of those spellings appears in the struct, and no driver knows the others exist.

> **Amended under [#184](https://github.com/onhotpath/ferry/issues/184): the query column is a flat join, and as published it was a bracket form.**
> As published those four rows read `db[host]`, `db[auth][user]`, `tags[0]` and `limits[rps]`, while [ADR-0004](0004-source-and-sink.md) said of the same driver that "its key function is a flat join like env's".
> Two Accepted ADRs described two key functions for one driver, and `KeyFunc`'s own godoc published the bracket one.
> **ADR-0004's line is the one that was right, and the spellings above are corrected to match it.**
> Nothing else in this ADR moves.
> The query column is an illustration of a driver's key function, and every rule this section exists to demonstrate is indifferent to which spelling fills it.
>
> **The decision was taken on a header plane and not on this one.**
> It was measured on a throwaway prototype, on branch `proto/184-key-form`, carrying a working `Source`, `Sink`, `Reader`, `Writer` and `Enumerator` over both `url.Values` and `http.Header`, with every key going through this ADR's own `NewKeys` so that the legality and injectivity checks are the shipped ones.
> On the query plane the two forms are interchangeable, and the prototype says so rather than manufacturing a winner: both pass the shipped `ferrytest.Driver`, both round trip a three-level struct, a slice and a map, each refuses exactly one schema the other accepts, and each silently loses exactly one minted map key the other keeps.
> Injectivity is a draw there.
> On a header plane it is not close.
> `[` and `]` are not `tchar`, so they are not legal in an HTTP field name: against a real `httptest` server `net/http` refused to write `db[host]` and `db[auth][user]` with `invalid header field name`, where `Db-Host` and `Db-Auth-User` arrived intact, and `textproto.CanonicalMIMEHeaderKey` leaves `db[host]` untouched because it does not recognise it as a field name at all.
> **Headers do nest, and the hyphen is how.**
> `X-Forwarded-For` and `X-Forwarded-Proto` are the IANA registry's own spelling of a nested `x-forwarded` object; the flat join produces both exactly and the bracket form refuses both, which is an assertion that headers do not nest made against a registry that nests all over the place.
> Run through the shipped conformance suite unmodified, on a header plane, the flat form reported 0 failures and the bracket form 3, at cases 3, 5 and 9.
> The set of addresses the bracket form can name is therefore a strict subset of the flat join's across the two planes an HTTP driver would serve.
>
> **What the bracket form would have bought, recorded here so that it is not relitigated from memory.**
> `?db[host]=x` is what `qs`, jQuery, Rails and PHP emit, and it is OpenAPI 3's `style: deepObject`; `?db.host=x` is Spring's convention and nobody else's.
> A query source taking the flat join will not read a browser form serialised by `qs`, and the prototype measured the consequence directly: `?db[host]=x&db[port]=1` loads under the bracket form and leaves the zero value under the flat one, and the reverse holds for `?db.host=x`.
> Bracket's silent round-trip residue is the rarer one, too: it loses an unbalanced `a][b` where the flat join loses `a.b`, and `a.b` is much the commoner map key.
> That cost is real, and it is recoverable, because [a separator is a driver option](#the-separator-is-a-driver-option-and-no-separator-is-universally-safe) and a query source can grow one exactly as the env driver did.
> A `[` in a header field name is not recoverable at all.
> Go's own parser lends the bracket no standing either way: `url.ParseQuery` reads `?tags[]=a&tags[]=b` as one key literally named `tags[]` holding two values, and `url.Values.Encode` percent-encodes `[` to `%5B` as an inert byte, so a bracket in a query string is a convention held by clients rather than a property of the encoding.
>
> What this ADR settles is the form, and the form is a join.
> Which separator an HTTP driver takes on each plane, and whether it serves one plane or two, are that driver's own and are not decided here.

The same address set gets four different answers to whether it is acceptable, and every one of them lands before any I/O:

| schema | env | registry | yaml | query |
| --- | --- | --- | --- | --- |
| a nested `db` plus a flat `db_host` leaf | rejected | ok | ok | ok |
| two fields differing only in case | rejected | rejected | ok | ok |
| a map key containing `[` | transformed | ok | ok | ok |
| a map key containing `\` | transformed | rejected | ok | ok |
| a leaf and a subtree at one segment | rejected by core, so no driver sees it | | | |

That table is the plane-agnosticism veto paying for itself.
Core has no opinion about hyphens, backslashes, brackets or case, because it cannot have one that is right for all four columns.
Each driver has the opinion that is right for its plane, and states it as an error naming both offending addresses rather than as a silent overwrite.

> **Amended under [#184](https://github.com/onhotpath/ferry/issues/184): the query plane accepts a map key containing `[`, and as published this row said it rejects one.**
> As published the row read `a map key containing [ | transformed | ok | ok | rejected`, and the rejection was a consequence of the bracket key function corrected above: a form that spells structure with `[` and `]` has to refuse a segment holding them, or lose one of two addresses.
> With the join in place there is no such byte, and the cell reads `ok`.
>
> **It was wrong on this ADR's own terms even under the bracket form**, which is why the cell moves rather than being restated.
> [The driver-side rule](#the-collision-rule-driver-side-the-key-function-is-injective-over-the-address-set) says a driver is expected to transform segment text rather than to reject it, and that the injectivity check is what makes transforming safe.
> Outright rejection is the posture a plane earns by having an external interpreter of the byte, which is `driver/kv`'s stated argument for its own refusals and is not the query plane's: a `[` is an ordinary byte in a query key, and the prototype round tripped one end to end even under the bracket form, `{map[a[b]:2]}` to `limits%5Ba%5Bb%5D%5D=2` and back to `{map[a[b]:2]}`.
> Where a collision is real the injectivity check already catches it, before any I/O and naming both addresses, which is the whole reason this ADR does not spend rejections on bytes.
>
> **No cell moves to `rejected` to replace it**, and the row below still carries the disagreement this table exists to show: a backslash is transformed on env, rejected on the Registry, and held on both of the others.
> In particular a map key holding the query join's own separator is not a rejection either.
> It is transformed like any other segment text, it is refused only when it collides with a nested address, and what survives is a Dump-then-Load residue on a key the driver has to parse back - the env driver's residue exactly, and the driver's to document rather than this table's to carry.

### Prefixing prepends a segment, and cannot concatenate text

Whether a prefix option exists at all, and how it is spelled, is [#11](https://github.com/onhotpath/ferry/issues/11)'s.
What it means in this model is this ADR's, and it is a clean break from xload.

xload's prefix is text concatenation onto a flat key, so `prefix=DB_` with key `HOST` gives `DB_HOST`, and `prefix=DB` gives `DBHOST`, and `prefix=DB_` with key `_HOST` gives `DB__HOST`.
All three are legal and two are typos that nothing can detect, because the separator is not part of the model.

Under a structured address a prefix can only prepend a segment.
`DBHOST` with no separator becomes unexpressible, which is the point: it is the spelling that manufactures the `DB/HOST` against `DB_HOST` collision in the first place.
A prefix that still spells the separator, `DB_`, is not silently wrong either; it produces `DB__HOST` on the env driver, visibly.

### What `ferrytest`'s memory plane is obliged to do

ADR-0002 shipped it as the place this decision becomes executable, so this ADR states the obligations rather than leaving them to the implementation.

- Key by the canonical rendering, which it may do because it has no format.
- Never fold case, and never normalize segment text.
- Reject a duplicate write loudly rather than overwriting, because ADR-0001 rules out silently ignoring anything.
- Enumerate in segment-wise order, so a test asserting on its contents is not asserting on map iteration order.
- Be usable as the negative case for the driver-side rule, since its key function is the identity and therefore trivially injective, which makes it exactly the wrong thing to prove the rule with.
  That is a restatement of ADR-0002's own point that the memory plane cannot keep the conformance suite honest, and it is why the rule needs a first-party driver with a real key function.

### What this ADR does not decide

- How a tag names a segment, whether prefix and squash exist, and how a segment containing the tag's own punctuation is written: [#11](https://github.com/onhotpath/ferry/issues/11).
- **Whether the struct tag key is configurable**, as xload's `FieldTagName` makes it, defaulting to `env`.
  [#11](https://github.com/onhotpath/ferry/issues/11)'s.
  It is named here because it collides with a decision ADR-0001 already took: tag validation is strict, and unrecognised tag content fails schema compilation.
  Pointing a configurable key at a tag somebody else owns, `json` being the obvious one, means strict validation rejects that tag's own options.
  So "configurable" and "strict" are compatible only with a stated answer for what happens then, and #11 owes that answer rather than inheriting the option unexamined.
- What the address appears in, and whether a source is queried per key or handed the whole set: [#5](https://github.com/onhotpath/ferry/issues/5).
- Whether any Go type produces an Index segment.
  That is [#7](https://github.com/onhotpath/ferry/issues/7)'s.
  This ADR reserves the kind so that #7's answer is not forced by the address type; if #7 admits no indexed composite, Index segments simply go unused.
- How Load discovers the length of an indexed composite, which needs enumeration ([#5](https://github.com/onhotpath/ferry/issues/5)) or presence ([#8](https://github.com/onhotpath/ferry/issues/8)).
- The error types the two rules produce, which follow [#9](https://github.com/onhotpath/ferry/issues/9)'s convention rather than inventing one.

## Consequences

- Dump-side silent data loss over colliding addresses stops being a runtime hazard and becomes a schema that does not compile, for every address the type determines.
  For map keys and sequence indices it becomes a failed write instead, because those addresses do not exist until there is a value.
  Both are loud; only the first is early, and the ADR says so rather than claiming the stronger one twice.
- Every driver carries an obligation it did not carry under a flat key space, and it is a real one.
  The mitigation is that it is about ten lines, it is checked by the conformance suite, and it runs once per schema rather than per key.
- Core is strictly less permissive than a flat key space, in exactly one way: a leaf and a subtree may not share a segment.
  A schema that xload accepted and that no tree plane can represent will now be rejected.
- The static address set handed to `Bind` contains container addresses as well as leaf addresses, and prefix-freeness is a rule about the leaves only.
  A driver therefore sees `/Tags` and `/Opt` and never `/Tags/*`, so every address it is handed is one it can fetch, write, name and check.
  The cost is that the prefix-free check has to know which of its members are containers, which is one bit per address the compiler already holds.
- A driver gets exactly one signal that an address is a dynamic container, and it is being asked for its children.
  That is what makes a plane whose element addresses share the container's own name expressible at all, and it costs the ability to answer anything at a container address that has children under it.
- What makes something an address is now stated, and it is not "the compiler knows about it".
  That test admits a wildcard shape, which is derivable from the type and has nothing at it, and the two prototype engines split on exactly that.
- ferry's address is comparable and its ordering is segment-wise, so both the determinism invariant and the map-key use are satisfied by the same type.
  Sorting the rendering instead is a subtle bug that produces `0 1 10 11 2`, and it will be a conformance-suite case.
  *(Discharged under [#306](https://github.com/onhotpath/ferry/issues/306), and it took eighteen cases to arrive: it is `Driver` case 19, recorded in [ADR-0014](0014-what-ferrytest-exports.md).
  A sequence of twelve is what it needs, because the two orders agree to nine and every other fixture in that suite carries two members, so the promise could not have been kept by adding an assertion to a case that already existed.
  What the case holds a driver to is only the order: which members came back is case 5's, which sorts the driver's answer before comparing it.)*
- The address model does not consume ADR-0001's `go 1.27` fallback, verified by building it at `go 1.26` under `GOEXPERIMENT=nojsonv2`.
- ferry is now committed to a path syntax of its own, which is a thing a reader will reasonably ask about.
  The answer has to be the measured one, that RFC 6901 cannot express a segment kind, rather than a preference.
- The tag grammar inherits a constraint it must now satisfy: a tag has to be able to name a segment whose text contains whatever punctuation the grammar itself uses.
  That is [#11](https://github.com/onhotpath/ferry/issues/11)'s to solve, and this ADR is the reason it cannot be dodged.

## Items from the xload survey

The survey is [`docs/research/generics-and-modern-go.md`](../research/generics-and-modern-go.md), section 5, against `github.com/gojekfarm/xtools` at [`a90b3aa`](https://github.com/gojekfarm/xtools/commit/a90b3aad2133248cec50f6b4d6e37b0d9e788adb).

**5.10, composite values are string-splitting, and it is not escapable.**
Addressed, in the half this ADR owns.
xload's `setVal` splits a value on a delimiter with no escaping, so a value containing the delimiter is unrepresentable ([load.go:343-394](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L343-L394)).
Index segments remove the structural cause: a composite gets one address per element instead of one shared string.
Measured on `[]string{"a", "b,c", ""}`, the flat form joins to `"a,b,c,"` and splits back to four elements, and the indexed form carries all three exactly, on a tree plane and on a flat one alike, as `TAGS_0`, `TAGS_1`, `TAGS_2`.
What remains is a genuine choice rather than a structural defect: a plane may hold a whole list in one value, and a codec may split it, and that codec's lossiness is then the user's decision.
Which composites are supported is [#7](https://github.com/onhotpath/ferry/issues/7)'s and how a codec is selected is [#12](https://github.com/onhotpath/ferry/issues/12)'s.
5.10's second half, that xload's tag grammar splits on `,` so `env:"K,delimiter=,"` cannot be written, is [#11](https://github.com/onhotpath/ferry/issues/11)'s and is named in the consequences above.

**5.12, `SerialLoader` precedence is unexpressible.**
Checked, and this ADR does not constrain it.
Composed sources answer at one address, so precedence is a question about values at a single address rather than about reconciling key spaces, which is the shape [#5](https://github.com/onhotpath/ferry/issues/5) needs and this ADR neither helps nor hinders.

**5.13, the per-key pull model amplifies backend round trips.**
Bears on this ADR, and points the same way.
The driver-side injectivity check requires the whole address set before any I/O, which is the identical precondition the survey identifies for a batch or snapshot source.
The prototype's precomputed key table is that fact used twice: once to make the check cheap, once to make the round trips collapsible.
The signature that would exploit it is [#5](https://github.com/onhotpath/ferry/issues/5)'s.

**5.14** was enumerated rather than assumed.
Two of its four items touch this ADR and neither changes it.
The non-deterministic select on a cancelled context is the same family as the concurrent-dump race measured above, and both are [#20](https://github.com/onhotpath/ferry/issues/20)'s; prefix-freeness removes the address-collision instance of it but not the general concern.
The value-receiver `Error()` defect is why this ADR defers its two failure modes to [#9](https://github.com/onhotpath/ferry/issues/9)'s convention rather than declaring error types here.
The duplicated loader-setting options and the `CanAddr` loop bear on nothing in this ADR.

**5.5, nondeterministic error output**, is [#9](https://github.com/onhotpath/ferry/issues/9)'s, and this ADR applies it rather than deciding it: both collision reports are sorted, measured at one distinct error string over 300 runs.

The remaining items are unaffected by this ADR.
