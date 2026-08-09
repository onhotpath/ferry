# 6. Defaults, zero values, and what absence means to a Go field

Status: Accepted
Date: 2026-08-02
Ticket: [#8](https://github.com/onhotpath/ferry/issues/8)

## Context

[ADR-0004](0004-source-and-sink.md) established that the boundary can tell three observations apart: `Absent`, `Null`, and a present empty string.
It said in as many words that what they *mean to a Go field* is this ticket's, and it left three questions here by name: whether an absent address takes a default, whether `null` and absent are the same thing to a `*string`, and whether `FOO=` satisfies `required`.

[ADR-0005](0005-the-supported-type-set.md) left the same three here again, and added the constraint that binds hardest.
A composite with no elements writes `Null` at its own address whether it is nil or empty, because `tags: []`, `tags: {}` and a missing key are one observation on a real YAML plane.
Three Go states, two signals, and the collision is forced rather than chosen.

[ADR-0001](0001-what-ferry-supports.md) put defaults **In core** and made them the worked example of a capability that is not a configuration concern.
It also milestoned drift detection by plane inspection on this ticket, on the grounds that a loaded struct erases absence.
[ADR-0003](0003-how-a-leaf-addresses-a-plane.md) named this ticket, alongside enumeration, as a possible route to discovering the length of an indexed composite.

This ADR is written from a throwaway prototype on branch `proto/8-defaults`, which never merges.
It is built on `proto/7-type-set`, which is built on `proto/5-source-sink`, so every number below runs against a real `Path`, a real `Value`, the real type set, and a real YAML plane over real files.
Forty-two probes across two rounds.
**Four overturned an answer this ADR had already reached in draft, and a review round overturned four more, including this ADR's own deciding argument for one of its calls.**
One survey claim did not reproduce.
Every rule with a live alternative is a switch in the prototype, so the two candidates are measured against each other rather than argued.
xload has no defaults mechanism at all.
Its tag options are `prefix`, `delimiter`, `separator` and `required` ([load.go:219-249](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L219-L249)), so there is nothing inherited here, only 5.1's consequences to avoid.

## Decision

### What this closes, and what it does not

The ticket asked for eight things by name.
This table is the answer to each, so a reader can check the ADR against the ask without reading the rest of it.

| The ticket asked | Closed | Where |
| --- | --- | --- |
| where a default lives: a tag, a constructor, an interface, or something else | **yes**, a struct tag compiled into the schema as a `Value` | [A default is a Value at an address](#a-default-is-a-value-at-an-address) |
| whether a field still holding its default is dumped or omitted | **yes**, dumped | [A defaulted field is dumped](#a-defaulted-field-is-dumped-like-any-other-and-omission-is-per-field-and-zero-based) |
| whether an explicitly-zero field is distinguishable from an unset one on Dump, and whether it should be | **yes**: not for a value type, and it should not be; `*T` at a leaf is the exception | [Explicitly zero against unset](#explicitly-zero-against-unset-on-dump-and-the-one-type-that-tells-them-apart) |
| what happens when the plane holds an explicit empty and the struct holds a non-zero default | **yes**, the plane wins | [One rule under all of it](#one-rule-under-all-of-it-absent-does-not-write) |
| what `Absent` and `Null` each mean to a Go field, per kind | **yes**, one table | [Absent and Null, per kind](#absent-and-null-per-kind) |
| whether ferry ever hands a sink an `Absent` | **yes: never**, and it is a contract rule | [ferry never hands a sink an Absent](#ferry-never-hands-a-sink-an-absent) |
| whether present-and-empty satisfies `required` | **yes**, it does, and `required` is admissible only on the static tier | [required is a presence test](#required-is-a-presence-test-and-nothing-else) |
| observable presence, and what the milestoned mechanism actually is | **yes**, a per-address observation of one Load | [Observable presence](#observable-presence-is-an-observation-of-a-load-not-a-property-of-a-field) |

Five questions this ADR had to answer that the ticket did not name, all of which came out of the prototype:

| Not asked for, answered anyway | Where |
| --- | --- |
| what a second Load into the same destination does, which is what watch and reload will do | [Absent does not write is a rule about one Load](#absent-does-not-write-is-a-rule-about-one-load) |
| what a declaration attaches to, when the address it applies to comes from the value | [A declaration attaches to the address shape](#a-declaration-attaches-to-the-address-shape-not-to-an-address) |
| whether a default beneath an optional subtree materialises the pointer | [A default fills a hole in a section](#a-default-fills-a-hole-in-a-section-and-never-conjures-the-section) |
| whether an array element with a declaration behaves like a struct field or like a slice element | as above |
| whether presence is a route to an indexed composite's length, which ADR-0003 asked | [Presence is not a route to a length](#presence-is-not-a-route-to-an-indexed-composites-length) |
| what partial presence does to a seeded value, which is not the same question at every kind | [A struct merges and a composite replaces](#a-struct-merges-and-a-composite-replaces) |
| what `required` means at a container address, and where it is admissible at all | [Where required is admissible](#where-required-is-admissible-the-static-tier-and-only-it) |
| how one field's several mistakes are reported | [One mistake should not report as three errors](#one-mistake-should-not-report-as-three-errors) |
| which refusals are liftable later, and by what | [Refusing is the reversible direction](#refusing-is-the-reversible-direction) |

**Two things this ADR does not close, stated here rather than left to the reader to notice.**

- **An in-place reload leaks the previous load's value** for any address the plane has since lost.
  Measured, and it is true under both candidate rules, so it is not an artefact of the one taken.
  The resolution is that a reload produces a new value rather than mutating a live one, which is [#16](https://github.com/onhotpath/ferry/issues/16)'s and [#13](https://github.com/onhotpath/ferry/issues/13)'s to spell.
- **ferry has no way to say "delete this address".**
  An omission is the absence of a `Set` call, which a replacing sink and a patching sink read differently, and both readings are legal.
  *(Both halves of this bullet are closed under [#254](https://github.com/onhotpath/ferry/issues/254): only one reading is legal, and ferry has a delete verb.
  See [the amendment below](#a-struct-merges-and-a-composite-replaces).)*

### One rule under all of it: Absent does not write

> **`Absent` means ferry does not write to the field.**
> Every other observation, `Null` and the empty string included, is a value the plane holds, and it is handed to the type set, which either accepts it or refuses it loudly.

That single sentence answers the ticket's fourth ask directly.
An explicit empty on the plane beats a non-zero default, because present beats absent and empty is present.
Measured, on a field declaring `default=anonymous`:

```
Absent              ->  Name="anonymous"
String("") (FOO=)   ->  Name=""
a real value        ->  Name="svc"
```

This is exactly what ADR-0004 bought and what xload structurally cannot have.
`OSLoader` collapses a missing variable to the empty string ([loader.go:27-36](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/loader.go#L27-L36)), so the middle row does not exist there: `FOO=` and no `FOO` are one observation.
5.12 is the same defect one level up, where a later higher-priority source can never override a value back to empty, and ADR-0004 already recorded that first-present-wins is correct only because absence is observable.
The rule above is the same fact applied to the field rather than to the source stack.

The other half of the rule is the part that is easy to miss, and it is what makes a seeded struct work:

```
seed {Name:"svc", Port:8080, Tags:["a"]}, loaded from an EMPTY plane
  ->  {Name:"svc", Port:8080, Tags:["a"]}
```

Nothing was written, so nothing changed.
This is also what `encoding/json` has always done in both versions: measured on `go1.27rc2`, unmarshalling `{}` into a populated struct leaves every field alone, in v1 and v2 alike.

#### Absent does not write is a rule about one Load

The rule is stated relative to the value the caller supplied, and that qualification is load-bearing rather than pedantic.

Measured, loading twice into one destination with `/port` deleted from the plane in between:

```
in place:            {Host:db1 Port:5432}  ->  {Host:db1 Port:5432}
into a fresh value:                            {Host:db1 Port:0}
```

The second load leaks the first one's value.
That is true under **both** candidate rules, because a scalar's absence and a container's are different questions and neither rule clears a scalar.
So the fix is not a different absence rule, it is that **a reload produces a new value rather than mutating a live one**, which is what ADR-0001's watch machinery wants anyway when it speaks of a value republished safely.

This ADR does not decide the entry point, which is [#16](https://github.com/onhotpath/ferry/issues/16)'s, and it does not decide the watch API, which is [#13](https://github.com/onhotpath/ferry/issues/13)'s.
It states the constraint both inherit: whatever they are, loading into a destination that already holds a previous load's results is not the operation this ADR describes, and offering it without saying so would ship the erasure defect ADR-0001 is trying to avoid.

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): the entry point exists, so the constraint above has a name and two sharp edges, and both are published rather than left as a warning about an unwritten API.**
>
> As published this section said the fix is that a reload produces a new value rather than mutating a live one, and handed the spelling to [#16](https://github.com/onhotpath/ferry/issues/16) and [#13](https://github.com/onhotpath/ferry/issues/13).
> Both have answered.
> [ADR-0010](0010-the-entry-point-and-the-schema-cache.md) shipped `Load[T]`, which produces a new value, and `LoadOver(ctx, seed, src)`, which does not.
> [ADR-0020](0020-watch-and-reload.md) rules that **reload is `Load`**, with no second verb, on exactly this section's argument.
> **What is new is that `LoadOver` is now the thing a reader will reach for by mistake**, so its two traps are stated here, where the rule they violate lives, and measured on a watcher built against the real module.
>
> **A lost address keeps the previous load's value.**
> That is the measurement above, unchanged, and it is the whole reason reload is not `LoadOver`.
> Delete `/port` from the plane and reload over the held value, and the field still reads `5432`, from a plane that no longer says so.
>
> **A composite is replaced wholesale rather than merged**, which is the section below applied to a seed the caller thought of as a base rather than as a previous result.
> A `map` or a slice in the seed does not survive the plane having any child under its address, and it survives entirely when the plane has none.
> Neither is wrong; both are surprising in a loop that runs every time a file changes.
>
> `LoadOver` remains exactly what this ADR describes and what ADR-0010 shipped: a load over a caller-supplied **seed**, which is a source of defaults, and not a refresh of a value a previous load populated.



#### A struct merges and a composite replaces

The rule is stated as though it says one thing at every kind.
It does, and it does not look like it, so the consequence is written down rather than left to be discovered.

Measured, a seeded value with three addresses touched on the plane:

```
seed    {Auth:{User:u Pass:p}  Tags:[a b]  Limits:{rps:1 burst:2}}
plane   /auth/user, /tags#0 and /limits/rps only
result  {Auth:{User:NEW Pass:p}  Tags:[NEW]  Limits:{rps:99}}
```

`Auth` merged and `Tags` and `Limits` were replaced.
Both follow from one rule.
A struct's fields are separate addresses, so the ones the plane does not have are `Absent` and are not written.
A composite is a single decision: the plane either has children under that address or it does not, and if it has any then it has said what the composite is.

There is no third option available.
Merging a slice or a map would mean deciding what an absent index or an absent key means against a seeded one, and neither question has an answer the plane can supply, because ADR-0005 already established that a plane cannot report present-and-empty at a container address.

> **Amended under [#254](https://github.com/onhotpath/ferry/issues/254): the replace rule gains one clarifying line, and the Dump side of it is stated rather than left as the open question this ADR filed against itself.**
>
> As published this section stated replace on the Load side and the "two things this ADR does not close" list said, in full, that "ferry has no way to say delete this address" and that an omission is the absence of a `Set` call which a replacing sink and a patching sink read differently, and that both readings are legal.
> **Both readings are no longer legal.** One is:
>
> > **Omission is no statement, and the plane is untouched.**
> > Retraction is always explicit.
> > Replace governs every address the dump **speaks about**, and no address it is silent at.
>
> That is the symmetric twin of the rule this ADR is built on.
> Absence does not write into the struct; omission does not write into the plane.
> Under it `omitzero` means what its name says, and under the other reading it would mean delete: a zero-valued Go field beside an operator-owned key would silently remove that key on the next dump, and every `omitzero` in a schema would be a landmine on a plane ferry does not exclusively own.
>
> Worked, on a struct whose `Comment string` carries `omitzero`, dumping `{Host: "db2", Comment: ""}` over a plane holding `host: db1` and `comment: keep me`:
>
> ```
> omission is no statement   ->  host: db2      comment: keep me
> strict projection          ->  host: db2      (comment gone)
> ```
>
> **Deleting an address is now expressible, and it is a verb rather than a silence.**
> [ADR-0004](0004-source-and-sink.md) gains the optional `Unsetter` capability, refused at open where the driver lacks it, and a `Null` at a container address still retracts the subtree as this ADR already said.
> So the sentence "ferry has no way to say delete this address" is discharged, and the way it is said is one nobody can reach by accident.
>
> **The word replace is unchanged and it is the scope that was ambiguous.**
> A composite the dump writes at all is replaced, exactly as above.
> A composite the dump never mentions is not a composite the dump replaced with nothing.
>
> **And merge is not the other half of a pair.**
> Replace is the only dump semantics ferry has; merge semantics exist only behind an explicit Option, and no such Option ships.
> That is the same asymmetry [refusing is the reversible direction](#refusing-is-the-reversible-direction) states for every other rule here: replacing is the loud behaviour, a merge is the permissive one, and the permissive one is not retractable once it is on by default.
> A delta write is admissible under the same rule and on one further condition, which is that reconciliation is guaranteed: efficient, but never wrong.

> **Corrected: a writer with no `Unsetter` is passed over in silence, and the amendment above says otherwise.**
>
> As published, the amendment reads "[ADR-0004](0004-source-and-sink.md) gains the optional `Unsetter` capability, **refused at open where the driver lacks it**".
> ADR-0004's own amendment under [#220](https://github.com/onhotpath/ferry/issues/220) and [#221](https://github.com/onhotpath/ferry/issues/221) had already withdrawn that half, and this ADR did not pick it up.
> A sink that implements no `Unsetter` is refused nothing: the dump writes what it speaks about and drops the retraction, silently.
>
> **The reason is the asymmetry this ADR is built on**, applied one step further out.
> What a value has to say at a container's own address must be spellable, or the dump stores something misleading, so a missing `Ensurer` is refused - at the address, during the walk, and not at the open.
> An unset is about what the plane already holds, which core cannot know anything about, so a missing `Unsetter` refuses nothing.
> **Nothing else in the amendment moves**: deleting an address is still expressible, still a verb rather than a silence, and a `Null` at a container address still retracts the subtree.

> **Corrected under [#306](https://github.com/onhotpath/ferry/issues/306): the correction above is withdrawn, and the amendment it corrected was right the first time.**
>
> As published, the block above reads "**a writer with no `Unsetter` is passed over in silence**", and it was written to bring this ADR into line with an interim amendment on [ADR-0004](0004-source-and-sink.md) that had itself never been ratified.
> The write-session grammar the first design board settled says the opposite, and it is what the sentence it corrected already said: a schema that needs a retraction against a writer that lacks one refuses at the open, before any I/O.
> [ADR-0004](0004-source-and-sink.md)'s own correction under the same issue restores the rule, states the predicate - the address set holds a composite, the opened writer implements no `Unsetter` - and records why the interim reading's ground dissolved once both shipped sinks implemented the capability.
>
> **So the amendment two blocks above stands as published**, "refused at open where the driver lacks it" included, and nothing else on this page moves: omission is still no statement, deletion is still a verb nobody reaches by accident, and a `Null` at a container address still retracts the subtree.
> The asymmetry this ADR is built on survives too, restated where it belongs: what a value has to say at a container's own address depends on the value, so a missing `Ensurer` is refused at the address; whether a schema can need a retraction is the schema's own property, so a missing `Unsetter` is refused at the open.



### Absent and Null, per kind

`Null` is not a second spelling of absence.
ADR-0004 fixed that `Null` means the plane **has** this address and the value stored there is that plane's own null.
So `Null` is presence carrying a value, and the question is which Go types can hold that value.

The answer needs no new principle, because ADR-0005 already stated the rule for every other kind:

> Every leaf accepts its own kind.
> Every leaf additionally accepts `String`, whose text is parsed by exactly the parser that leaf's own kind uses.
> Nothing else coerces.

`Null` is admitted by exactly the Go types that have a null, and refused by every other leaf as a wrong kind.
That is the same refusal a `Bool` gets at a `string` field.

| Go kind | `Absent` | `Null` |
| --- | --- | --- |
| `bool`, `string`, the integer kinds, the float kinds | does not write | **refused**, wrong kind |
| `time.Duration`, `time.Time` | does not write | **refused**, wrong kind |
| `[]byte` | does not write | nil slice |
| `[N]byte` | does not write | **refused**, an array has no nil |
| `*T` | does not write | nil pointer |
| `[]T`, `map[K]V` | does not write | nil, the zero value |
| `[N]T` | each element is a static address, walked either way | per element, by the element's own row |
| `struct` | per field | a struct has no address of its own, so it is only reachable through `*T` |

Measured, one address at a time into a seeded struct, against the two rejected readings:

| address | admitted by the type set | `Null` means zero | `Null` means absent |
| --- | --- | --- | --- |
| `/S` string | **refused** | `""` | `"seed"` |
| `/I` int | **refused** | `0` | `7` |
| `/D` time.Duration | **refused** | `0s` | `1s` |
| `/By` []byte | `""` | `""` | `"xy"` |
| `/P` *int | `nil` | `nil` | `nil` |
| `/Sl` []string | `nil` | `nil` | `nil` |

**The two rejected readings, and why each loses.**

*`Null` means the zero value*, which is what `encoding/json/v2` does.
Measured on `go1.27rc2`, and this is a change from v1 rather than an inherited convention:

```
{"i":null} into a populated struct    v1: I=7     v2: I=0
{"st":null}                           v1: "seed"  v2: ""
```

*`Null` means absent*, which is v1's behaviour and which would let `port: null` take the default.
It throws away ADR-0004's central result at the exact point that result was built for: absence and null would be one thing to a Go field, having been made two at the boundary one ADR earlier.
It is also, plainly, silently ignoring a value the plane holds, which ADR-0001 rules out by architecture.

**The reading taken wins on recoverability, and the two are not symmetric.**
Measured, the same `Null` at two fields:

| core rule | can a user get the other behaviour? |
| --- | --- |
| refuse | **yes.** A registered codec for its own type accepts `Null` and returns 0. That is ADR-0005's stated escape hatch used for what it is for. |
| zero | **no.** The zeroing happens in the walk before any codec is consulted, so nothing recovers strictness for a plain `int`. |

Strictness is recoverable by a mechanism ferry already ships, and it fails loudly while a user finds out they need it.
Leniency is unrecoverable and fails silently.
That asymmetry is the argument, and it does not depend on any feature outside core.

**An earlier draft decided this on plane-to-plane transfer, and that argument is wrong.**
It is recorded because the shape of the mistake matters more than its fix.
The draft argued that under the zeroing reading a YAML `port: null` becomes `PORT=0` during transfer, silently.
Measured, there are two shapes of transfer and the argument was about the wrong one:

```
source YAML:  host: h    port:        (a blank key is !!null)

(a) address-to-address    /host string("h")   /port null      preserves it exactly
(b) struct-mediated       Load into T, Dump out
```

(a) is a loop from `Reader.Get` into `Writer.Set`, builds no Go value, and **never runs this ADR's rules at all**.
It is also the transfer ADR-0001 makes Enabled, so it ships outside core and is not core's to protect this way.
And (b) already rewrites the plane in a way ADR-0005 accepted: measured, `Tags: []` loads to nil and dumps as `/Tags=null`.
So the draft's argument proved too much, since applied consistently it would have rejected ADR-0005's own nil-and-empty normalisation.

**The cost of the rule taken is narrower than the draft claimed.**
Measured through the real YAML driver, on a field declaring `default=8080`:

| document | plane reports | refuse | zero | absent |
| --- | --- | --- | --- | --- |
| key deleted | `absent` | **8080** | **8080** | **8080** |
| `# port: 9090` commented out | `absent` | **8080** | **8080** | **8080** |
| `port:` blank | `null` | refused | **0** | **8080** |
| `port: null` | `null` | refused | **0** | **8080** |
| `port: ""` | `string("")` | refused | refused | refused |

Commenting a line out removes the key, so it is `Absent` and takes the default under every rule.
The cost is confined to a blank key and an explicit `null`, and there the zeroing reading gives `0`, which is the one answer nobody wants.
So this is not correctness bought with ergonomics: the rejected reading is worse on both.
The error message is where the remaining cost is survivable, and it should name the remedy; the error type is [#9](https://github.com/onhotpath/ferry/issues/9)'s.

**If a knob is ever wanted here it is a load Option and never a tag option.**
The reason is an asymmetry that governs three calls in this ADR and is worth stating once.
ADR-0001 freezes the tag vocabulary on publication, so a tag option is expensive to add later and must be decided now.
Nothing freezes Options, and this one would be load-time: measured, one compiled schema serves both policies, so it never touches whatever keys the schema cache.
Deciding the core rule now and leaving the Option available is therefore the cheap path, and this ADR takes it and ships no knob.

**One consequence worth stating because it looks like a gap.**
Nothing ferry dumps ever writes a `Null` at an address whose Go type refuses one: `Null` is emitted only for a nil pointer, a nil or empty composite, and a nil `[]byte`, all of which accept it back.
So the refusal fires only against a plane a human wrote, never against ferry's own output, and value fidelity is untouched.

### A default is a Value at an address

> A default is declared on the field, it is text, and schema compile turns it into a `Value` of kind `String` held at that field's address.
> On Load it is applied when, and only when, the plane reports `Absent` there.

The spelling of the option is [#11](https://github.com/onhotpath/ferry/issues/11)'s.
The mechanism and its home are this ADR's, and the load-bearing claim is that **a default is indistinguishable at the boundary from what a flat plane would have reported**.

Three things follow, and they are the argument for this shape over the alternatives.

**One conversion authority.**
`default=8080` becomes `String("8080")`, which ADR-0005 already made the universal donor, parsed by the leaf's own parser.
There is no second decode path to keep in step, no second set of errors, and a registered codec's type gets defaults for free because the value reaching it is the same value a plane would have supplied.
That is the survey's 4b requirement discharged rather than restated: viper's two engines return different answers for one key, and a default applied by a second mechanism would be exactly that shape.

**Every declaration is checked from `reflect.TypeFor[T]()` alone**, with no value in hand and no plane reachable, which is the assertability property ADR-0001 claims for tag rejection and ADR-0003 for the static half of the collision rule.
Measured:

```
p,default=abc   (int)            ferry: /p: default "abc" is not a valid int: invalid syntax
b,default=99999 (int8)           ferry: /b: default "99999" is not a valid int8: value out of range
t,default=30    (time.Duration)  ferry: /t: default "30" is not a valid time.Duration: missing unit
t,default=30s   (time.Duration)  compiles
p,default=0080  (int)            compiles, and means 80
s,default=      (string)         compiles, and means ""
```

`0080` is the survey's zero-padded-port case, and it lands the same way in a tag as it does from a plane, because it is the same parser.

**Text, decoded fresh on every load, rather than a Go value cached at compile.**
The alternative was tried and it aliases.
Measured, two loads of one schema with a `[]byte` default:

```
as a Value:            a="Secret"  b="secret"   aliased=false
as a cached Go value:  c="Secret"  d="Secret"   aliased=true
```

Two independently loaded structs share one backing array, and mutating either corrupts the other.
The re-decode that avoids it costs 30.6 ns per default per load.

**A default is leaf-only, and a composite default does not compile.**

```
tags,default=a  ([]string)        ferry: /tags: []string is a composite, so it has no single
                                  address a default could sit at; seed the value instead
```

A composite's value lives at many addresses and a tag holds one text, so expressing `{a, b}` would need a list syntax inside the tag.
That is survey item **5.10** exactly, the string-splitting defect ADR-0003 removed structurally with `Index` segments, and reintroducing it inside the tag grammar would be a strange place to put it back.

**And the JSON-ish spelling a user would reach for is not writable in a struct tag at all**, which is a separate fact from 5.10 and was measured rather than assumed.
A struct tag's value is delimited by double quotes and a Go raw string literal cannot escape one, so:

```
`ferry:"origins,default=["value"]"`   reflect.StructTag.Get returns   origins,default=[
```

The rest of the tag is gone with no runtime diagnostic.
`go vet` does catch it, and `go test` does not: verified on a real module, where `go test ./...` passes clean and `go vet ./...` reports `not compatible with reflect.StructTag.Get`.
That is ADR-0001's claim about the vet gap confirmed for a `ferry` tag specifically rather than inferred from the analyzer's documentation.
A pointer to a leaf is not a composite and does take a default, because it has an address of its own.

**A default is a Load-side rule.**
Dump writes the value in hand and never substitutes a default for it.

#### The three alternatives, and what each costs

**A constructor, or more precisely a pre-populated destination**, is not an alternative at all: it falls out of "Absent does not write", it needs no mechanism, and it is strictly more expressive because it can express a composite.
It is also strictly less inspectable, and that is the deciding difference.
ADR-0001 made template generation and schema extraction Enabled, and both work by walking the schema or dumping into a recording sink.
A default that exists only as a Go value in somebody's constructor is invisible to both.
Measured, the declared form reaching a template:

```
load from an EMPTY plane  ->  {Host:localhost Port:8080}
dumped into a recorder    ->  [/host=string("localhost") /port=number("8080")]
```

So the two coexist and partition cleanly: **declared defaults for leaves, seeded values for composites and for anything a tag cannot spell.**
Where both apply to one field the declared default wins, because ferry cannot tell a seeded value from a zero one and 5.7 is what happens when a library tries.

**An interface the type satisfies**, in the shape of `SetDefaults()`, is refused.
ADR-0005 refused `fmt.Stringer` on the ground that `String() string` declares no inverse, and `SetDefaults()` is weaker still: it declares nothing at all that ferry can inspect, check, or show to a template.
It is also a second authority that runs before or after the walk, and every ordering question it raises is one the declared form does not have.

**A `Static` source under `FirstOf`**, which ADR-0004 names by that name as "both the defaults layer and the memory plane", stays expressible and is not ferry's answer.
Measured, renaming one field:

```
Static source, before the rename  ->  Port=8080
Static source, after the rename   ->  Port=0
```

A `Static` defaults layer spells the address set a second time, and nothing checks the two spellings agree, so a rename silently drops the default.
A declared default cannot drift because it is on the field.
The combinator remains a user's composition and is not a second ferry-supplied way to declare a default, which is survey item 5.14's first entry answered rather than assumed.

#### A declaration attaches to the address shape, not to an address

This is the second probe that overturned the draft, and it is a one-line rule with a silent failure behind it.

A map key's address and a slice element's index come from the value, not the type (ADR-0003's dynamic tier), so `/servers/a/port` is not in the compiled schema and never can be.
The declaration lives at `/servers/*/port`.

Measured, with one map entry and one slice element on the plane:

```
looked up by the realised address  ->  Servers=map[a:{h1 0}]     Pool=[{h2 0}]
looked up by the address shape     ->  Servers=map[a:{h1 8080}]  Pool=[{h2 8080}]
```

The first row is not an error, it is every default under a map or a slice silently not applying.
So the walk carries two paths: the realised one it asks the plane about, and the static one it looks declarations up by.

*(Clarified under [#56](https://github.com/onhotpath/ferry/issues/56): the shape is the walk's own lookup key and is never handed to a driver.
[ADR-0003](0003-how-a-leaf-addresses-a-plane.md) states why, and states that `/Tags` and `/Opt` are addresses where `/Tags/*` is not.
The presence bit this ADR reads at an optional section depends on the second half of that: an explicit `Null` at `/Opt` is an observation at an address, and it is unwritable if the section has none.)*

#### A default fills a hole in a section, and never conjures the section

A `*T` where `T` is a composite is materialised exactly when something under it was present on the plane.
A declared default beneath it does **not** count as presence.

Measured, with `Auth *Auth` whose `User` field declares `default=admin`:

| plane | a default is not presence | a default is presence |
| --- | --- | --- |
| nothing under `/auth` | `Auth=nil` | `Auth=&{User:admin}` |
| `/auth/pass` present | `Auth=&{User:admin Pass:p}` | `Auth=&{User:admin Pass:p}` |

Under the second rule no `*T` with a default anywhere beneath it can ever be nil, which is the whole meaning of the pointer.
Under the first, an optional section stays optional and its defaults fill holes in it once it exists.

A pointer to a **leaf** is a different case and is not affected: its default sits at its own address, so `P *int` with `default=5` loads as `&5` from an empty plane.

**An array element is a static address and is walked either way**, which makes it behave like a struct field rather than like a slice element.
Measured on `[2]Server` where `Server.Port` declares `default=8080`, with only `/arr#0/host` on the plane:

```
{Arr:[{Host:h Port:8080} {Host: Port:8080}]}
```

Element 1 has nothing on the plane and still takes its default.
A slice element in the same position does not exist at all.
That is ADR-0005's array-against-slice asymmetry appearing a second time, in a place ADR-0005 did not look, and it belongs in the documentation of the set for the same reason.

> **Amended under [#336](https://github.com/onhotpath/ferry/issues/336): the root is an address now, and it is the one address with no tag on it, so it is the one address that can carry `required` and cannot carry a declared default.**
>
> As published, every rule in this ADR was written about a field, because a declaration was written on a field's tag and the root was not an address at all.
> [ADR-0003](0003-how-a-leaf-addresses-a-plane.md) has since named the empty path the root address, and a root that resolves to a leaf compiles there.
> Everything this ADR decides about absence holds there unchanged: absence does not write, and a seed survives it.
>
> What does not exist there is a **declared** default.
> A declared default is written on a tag, this ADR's whole treatment of it assumes a tag, and the root has none.
> The decision is not to invent a second way to declare one.
>
> > A seed is the caller's default.
> > A declared default exists only where a tag can spell one, and the root has no tag.
>
> So `ferry.LoadOver(ctx, 8080, src)` is the root's default, and it is better typed than a declaration would have been: the seed is a `T` rather than a text this ADR's own chain would have to decode on every load.
> The cost is stated rather than hidden: `Compile` validates a tagged field's default text from the type alone and has nothing to validate at the root, because there is no text there to be wrong.
>
> `required` at the root **is** declared, by `ferry.RootRequired`, because requiredness is a fact about the schema and not about the caller's starting value.
> It composes with a seed and is not weakened by one: `required` is a presence test about the plane, satisfied by any observation other than `Absent`, and a seed is not an observation.
> `LoadOver(ctx, 8080, src, ferry.RootRequired)` therefore carries 8080 forward on a reload and still fails with `ErrMissing` where the plane went silent, which is the shape a reload wants and no declared default could give it.
>
> At a struct root it is the same word with the same meaning it has at every other section address, which the subsection below states: the plane supplied at least one of the address's static children.

### A defaulted field is dumped like any other, and omission is per-field and zero-based

> A field holding its default is dumped.
> ferry never compares a value against its default on Dump.

Two independent reasons, and the first is decisive on its own.

**ferry cannot tell "still at its default" from "explicitly set to the same value as the default".**
They are the same bits.
Telling them apart needs a presence bit next to the field, which is a type the user writes, and reaching for a structural comparison instead is survey item 5.7 in a new costume.

**Omitting a defaulted value makes the stored artefact under-specified**, so the value it denotes is decided by whichever version of the code reads it.
Measured, dumping `Port: 8080` under an omit-if-default rule and then reading the same plane back after the default changed to 9999:

```
stored plane: [/name=string("svc")]     the /port key is not there
read by the same code       ->  Port=8080
read after the default moved -> Port=9999
```

That is a data-breaking change with a stable Go API, which is precisely what [#28](https://github.com/onhotpath/ferry/issues/28) was filed for.
Writing the value keeps the artefact self-describing and the default a fallback rather than a shared secret between the writer and the reader.

**Omission exists, and it is `omitzero`.**
ADR-0005 already adopted `omitzero`'s Go-defined semantics as the model and left the spelling to #11.
It is a comparison against the Go zero value, which is computable, cheap and per-field, and it is not a comparison against the default.

**`omitzero` and a non-zero default contradict, and it is checkable at schema compile.**
Measured, forced through with the check disabled:

```
explicit Port=0, omitzero + default=8080  ->  0 Set calls  ->  loads back as 8080
```

The user's explicit zero became 8080.
Because a default's text is parsed at compile, the pair can be refused there, with no value in hand:

```
b,omitzero,default=8080   ferry: /b: omitzero and default=8080 contradict: an explicit zero
                          would be omitted and would load back as 8080
c,omitzero,default=0      compiles
```

The second compiles because a default equal to the zero value is not a contradiction: omitting it and reapplying it land on the same value.
`required` and `default` contradict for the plainer reason that a default answers absence and `required` forbids it.
Both are refusals this ADR requires; **where option contradictions are enforced and what they are spelled is #11's**, as ADR-0001 already assigned.

**Delta dump is where "only write what differs" belongs**, and ADR-0001 already Milestoned it.
Making it a property of defaults would be building one feature inside another.

### Explicitly zero against unset on Dump, and the one type that tells them apart

> A value-typed field's explicit zero and its unset state are one value on Dump, and ferry does not try to separate them.

On Dump ferry holds a Go value, and "unset" is not a state a Go struct has.
Any mechanism that claimed otherwise would be inferring it, which is 5.7.

**`*T` at a leaf does separate them, and this is a genuine refinement of ADR-0005 rather than a contradiction of it.**
ADR-0005 measured that a pointer adds no bit to a composite: a nil `*[]string` and a pointer to an empty one both mint `Null` at one address.
At a leaf the two observations are distinct, measured:

```
P nil   ->  /P = null
P = &0  ->  /P = number("0")
P = &5  ->  /P = number("5")
```

So ADR-0005's "the distinction is not expressible by any type in the set" is a statement about **nil against empty for a composite**, and it does not extend to unset against zero for a leaf.
`*int` expresses that one exactly.

**And the asymmetry ADR-0005 named is on Dump only.**
Measured, a nil `*int` dumped through a plane that has no null and loaded back:

```
nil *int  ->  flattened  ->  ferry: /P: strconv.ParseInt: parsing "": invalid syntax
```

That is ADR-0005's 8-of-10 result at the individual type.
On **Load** from the same plane the distinction survives intact, because absence does:

```
PORT unset  ->  P=nil
PORT=0      ->  P=&0
```

So the honest statement is that `*T` at a leaf is a full unset-against-zero on any plane in the Load direction, and a full one in the Dump direction only on a plane that has a null.
YAML and JSON do; TOML, env, query parameters and opaque KV do not, by ADR-0004's own table.
A driver on the second list declares the kinds it can carry and refuses `Null` loudly, which is the obligation ADR-0005 already created.

### `required` is a presence test, and nothing else

> `required` is satisfied by any observation other than `Absent`.

Measured at a leaf:

| plane | `required` |
| --- | --- |
| `Absent` | refused, naming the address |
| `String("")`, which is `FOO=` | **satisfied** |
| `Null` at a type that can hold one | **satisfied** |
| a value | satisfied |

This is 5.1's most-cited consequence fixed.
xload implements `required` as `val == "" && meta.required` ([load.go:147](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L147)), so `FOO=` cannot satisfy it there, and ADR-0005 made `FOO=` arrive as `String("")` and never as `Absent` precisely so that this is a live question rather than a hypothetical.

The rule is narrow on purpose.
`required` asserts that the plane spoke about the address, not that the value is useful, because "not empty" is a constraint on the value and ADR-0001 ruled a validation constraint language out by architecture.
That is also why `required` never means "non-nil": `Null` at a `*T` satisfies it and yields `nil`, which is the user getting exactly what their type asked for.

#### Where `required` is admissible: the static tier, and only it

> `required` names an address, so it is admissible exactly where that address's children come from the **type** rather than from the value.

That is ADR-0003's static tier reused, not a new distinction:

| field | `required` | why |
| --- | --- | --- |
| `string`, `int`, `time.Duration`, `[]byte`, `[N]byte` | **admissible** | the address is itself static |
| `*T` where `T` is a leaf | **admissible** | as above |
| `struct`, `*struct` | **admissible** | one `Name` per exported field, from the type |
| `[N]T` | **admissible** | exactly `N` `Index` segments, from the type |
| `[]T`, `map[K]V`, `*[]T` | **refused at schema compile** | the elements come from the value |

At a composite it means **the plane supplied at least one of the address's static children.**
Measured, `Auth *Cred` with `required`, through the real YAML driver and then through a flat plane:

| document | | |
| --- | --- | --- |
| key absent | refused | `Auth=nil` |
| `auth: {}` | refused | `Auth=nil` |
| `auth: {user: u}` | satisfied | `&{User:u}` |
| `auth: {pass: p}` | satisfied | `&{Pass:p}` |
| `auth: null` | satisfied | `Auth=nil` |
| `AUTH_USER` unset, on env | refused | `Auth=nil` |
| `AUTH_USER=u`, on env | satisfied | `&{User:u}` |

One meaning on both.
The only row where the two could differ is `auth: null`, which a flat plane cannot express, so the divergence cannot arise.

This also repairs a defect the earlier draft shipped: `required` on a **non-pointer struct** was accepted at schema compile and enforced by nothing, because such a struct has no address of its own to be absent at.
It now means the same thing it means on `*struct`, measured: `ferry: /auth: required, and the plane supplied nothing under it`.

#### Why a slice and a map are refused, and what a user does instead

On a dynamic composite `required` could only mean "at least one element", which is a length constraint on the value rather than an assertion about the plane.
The tell is that it would make one word mean two different guarantees: `required string` is satisfied by `FOO=` and implies nothing about length, while `required []string` would imply `len >= 1`.

**The reading a user actually wants is not writable at all**, and this is the sharpest result in the section.
The wanted reading is that an explicit `[]` satisfies `required` while a missing key and a `null` do not.
Measured through the real YAML driver, at a container address:

| document | `Get(/origins)` | `Children` |
| --- | --- | --- |
| key missing | `absent` | 0 |
| `origins: []` | `absent` | 0 |
| `origins: {}` | `absent` | 0 |
| `origins: null` | `null` | 0 |
| `origins: [a]` | `absent` | 1 |

Five documents, **three distinct observations**.
A missing key and `origins: []` are one observation, so a rule that answers them differently is not a rule that can be written.
This is ADR-0005's forced collision, and it is the same fact that made nil and empty one value.

**A seventh `Value` kind would not rescue it either**, which is worth recording because it is the obvious next idea.
ADR-0004 closed the model at six kinds with no group arm and no escape arm, so the reading needs a new "present and empty".
But env, query parameters and opaque KV cannot express an empty list at all: `ORIGINS_0` exists or it does not.
So the rule would hold on three plane classes of six, which is the shape ADR-0005 refused by name:

> A guarantee that holds on some planes and not others is not a guarantee.

The refusal therefore carries the remedy, because the user reaching for it has a legitimate intent:

```
ferry: /origins: required is not available on []string: a plane cannot report
"present and empty" at a container address, so required could only mean "at
least one element", which is a constraint on the value; model the distinction
as a struct with a set flag, or check len() after Load
```

ADR-0005 already reached the identical distinction and gave the identical answer, to model it in the type.
Measured, and it is uniform across planes because `required` then sits on a leaf:

```
Origins struct { Set bool `required`; Items []string }

nothing              ->  refused at /origins/set
set=true, no items   ->  {Set:true Items:[]}
set=true, one item   ->  {Set:true Items:[a]}
```

#### Refusing is the reversible direction

This ADR refuses in three places where a permission was arguable: `Null` at a leaf that cannot hold one, `required` on a dynamic composite, and spending a tag word on either.
They share one reason, stated once:

> A refusal is liftable later with no break, because nothing depended on it failing.
> A permission is not retractable.

So where a rule is genuinely undecided, refusing is the direction that keeps the decision open.
**Both refusals above can later be lifted by a load Option**, and neither can be lifted by a tag option, because ADR-0001 freezes the tag vocabulary on publication and does not freeze Options.

The two Options are not equally cheap, and [#16](https://github.com/onhotpath/ferry/issues/16) needs the difference now rather than when it writes the cache.
Measured:

| Option | effect | cost |
| --- | --- | --- |
| a `Null` policy | changes decode at load time | one compiled schema serves both, so nothing touches the cache key |
| lifting `required` on a collection | changes **what compiles** | one `reflect.Type` yields two different schemas, so the Option becomes part of whatever keys the schema cache |

And that key is awkward for a reason ADR-0004 already measured against drivers.
Reproduced here against an option value: `runtime error: hash of unhashable type main.LoadOption`, the same panic as its `hash of unhashable type main.EnvSource`, because an option list is funcs.
So a compile-affecting Option is a heavier thing than a load-affecting one, and #16 inherits that rather than discovering it.

#### One mistake should not report as three errors

A field carrying `required` and a default on a `[]string` trips three separate rules.
Reporting all three invites a user to fix one and get two more, so the diagnostics follow one rule:

> Check each option's admissibility at this type first.
> Check contradictions only among the options that survived.

A contradiction between two options is only meaningful if both are individually legal there.
Measured:

```
[]string  required,default=value   2 errors: required inadmissible, default inadmissible
string    required,default=x       1 error:  the contradiction, since both are admissible here
int       omitzero,default=abc     1 error:  the bad default, since a default that does not
                                             parse has no value to compare against zero
```

### ferry never hands a sink an `Absent`

ADR-0004 flagged this as the likely answer and left it here.
Confirmed, and it is a contract rule rather than a convention:

> `Value.Kind() == Absent` is a `Reader`-side kind.
> `Writer.Set` is never called with it, and an omitted address gets no `Set` call at all.

Three reasons, and the first is that there is nothing for it to mean.
On Dump ferry holds the value and is the one making the plane, so there is no observation to report; `Absent` is a statement about looking and not finding.

The second is measured, and it is ADR-0004's own recorded prototype defect: its YAML sink mapped `Absent` to `!!null`, so an absent address was written as an explicit null and read back as `Null`.
That is the conflation ferry criticises xload for, committed on the write path, and it happened because the sink was handed a kind it had no honest answer for.

The third is that omission needs no kind.
Measured, dumping a struct with one `omitzero` field at its zero value:

```
2 mapped addresses  ->  1 Set call  ->  0 Set calls carrying Absent
```

This keeps `Writer` at one method with no special value, and it is what ADR-0001's Milestoned delta and partial dump needs from the sink contract.

**Omission is not deletion**, and it is the patching sink's reading that survives.

> **Amended under [#254](https://github.com/onhotpath/ferry/issues/254): as published this subsection measured two sinks disagreeing, called both legal, and left "ensure nothing is here" to ADR-0001's Milestoned delta dump.**
>
> Only the patching reading is legal now, and the missing verb is [ADR-0004](0004-source-and-sink.md)'s `Unsetter`, which is a capability of the sink contract rather than anything delta dump has to arrive before.
> A sink that replaces an address ferry was silent at is wrong, and the reason is [the amendment above](#a-struct-merges-and-a-composite-replaces): omission is no statement, so there is nothing at that address for a dump to have replaced.
> The measured disagreement is now one sink conforming and one sink failing a conformance case.
> The rest of this subsection stands: an omission still means only that ferry did not write, `Writer` still has one method, and no `Set` ever carries an `Absent`.

### Observable presence is an observation of a Load, not a property of a field

ADR-0001 milestoned drift detection by plane inspection on this ticket, on the grounds that a loaded struct erases absence.
Milestoning commits to a mechanism and never to a feature, so this section says what the mechanism is.

> One Load can report, per address, the boundary `Value` it observed, including `Absent`.
> Nothing is stored on the field and no type is added to the set.

Measured, three planes and the struct each produces:

| plane | struct | observation at `/port` |
| --- | --- | --- |
| `/port` = 5432 | `{Port:5432}` | `number("5432")` |
| `/port` deleted | `{Port:0}` | `absent` |
| `/port` = 0 | `{Port:0}` | `number("0")` |

Rows two and three are one struct and two observations.
A key deleted from the plane and a key changed to zero are indistinguishable after the walk, which is ADR-0001's sentence made concrete, and the observation is that erasure made optional.

The mechanism costs nothing: measured at 790.8 ns for the load without it and 754.9 ns with it, which is the same number.
The walk already has the information, so exposing it is a matter of not throwing it away.

**Why this is not the runtime accessor ADR-0001 ruled out.**
`Get("db.host") any` is ruled out by architecture because a second accessor path is a second conversion engine by construction.
An observation of the boundary converts nothing: it reports the `Value` the driver produced, before any leaf decode, so there is exactly one conversion engine and the observation is upstream of it.

**Two things it deliberately is not.**

It is not a presence-carrying field type.
An `Optional[T]` in core's type set would need a proof in the harness, a row in the golden column, and a representation on every plane, and its dump form at a composite is the ambiguity ADR-0005 measured as unresolvable.
ADR-0005 closed the set and said a user who needs presence in the value models it as `struct{ Set bool; Items []string }`, and that stands.

It is not a schema view.
ADR-0001 left "whether core ever exports a read-only schema view" open and said it should be reopened only if a concrete need survives the dump-into-a-recording-sink pattern.
Plane inspection does not need one, because the addresses come from the load that is running.

Whether the observation is spelled as a callback, a recorder, or a returned report is an API question that belongs with the caller-facing lifecycle, which is [#25](https://github.com/onhotpath/ferry/issues/25)'s.
What this ADR fixes is that the information survives the walk, which is the part ADR-0001 milestoned.

> **Recorded under [#306](https://github.com/onhotpath/ferry/issues/306): the mechanism is built, it needs nothing from core, and this note says where it lives.**
>
> The closure table above marks this ask closed, and an audit read that as an over-claim on the ground that core exports no reporting surface and the walk keeps only its per-subtree `outcome`.
> Core exports none, and it was never going to: the paragraph above hands the spelling to #25 and commits this ADR to the information surviving the walk and to nothing else.
>
> **It survives, and the reason is structural rather than something the walk had to be taught.**
> A load asks the plane for every address it visits, and absence is a kind of the `Value` rather than a second return value, so a `Source` a caller wrapped sees the whole observation, `Absent` included, before any leaf decode.
> The mechanism is that decorator: a `Bind` that hands the address set through, and a `Get` that keeps what came back.
> It converts nothing, it is upstream of the one conversion engine, and it is roughly thirty lines a caller writes once.
>
> Evidence is core's own test, `TestPresenceSurvivesTheWalkAsAnObservationOfOneLoad` in `absence_test.go`, which is the three-row table above run through such a decorator: a key deleted and a key set to zero produce one struct and two observations.
> It has been there since absence itself landed.
>
> **The one limit worth stating** is that what is observed is every address the load asked the plane about, which is what "an observation of a Load" means.
> A member under a slice or a map exists only where the plane listed it, so a decorator sees the addresses that were realised rather than an address shape that was not.
> That is the same tier boundary [a declaration attaches to the address shape](#a-declaration-attaches-to-the-address-shape-not-to-an-address) draws, and it is why the observation is a route to what the plane said and not to what it could have said.

#### Presence is not a route to an indexed composite's length

ADR-0003 asked whether a length is discoverable through enumeration ([#5](https://github.com/onhotpath/ferry/issues/5)) or through presence (this ticket).
ADR-0004 answered it with `Enumerator`.
This ADR closes the other half rather than leaving two partial answers, because the presence route looks workable and is not.

Probing `/tags#0`, `/tags#1`, and so on until a miss, measured:

| plane | probe until miss | enumerate |
| --- | --- | --- |
| indices 0, 1, 2 | `[a b c]` in 4 `Get` calls | `[a b c]` in 1 |
| indices 0 and 2, a hole | `[a]` in 2 `Get` calls | `[a "" c]` in 1 |

A hole truncates the list silently, which is what ADR-0001 rules out, and the cost is one `Get` per element plus one.
**Enumeration is the only route**, and ADR-0004's asymmetry stands unchanged.

### The walk carries a presence bit, which is 5.7's repair

This is the fix for the survey item ADR-0001 assigned to this ticket outright.

xload allocates a fresh zero value with `reflect.New` and `reflect.DeepEqual`s it against the populated struct to decide whether to write back a nil struct pointer ([load.go:107-117](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L107-L117) and its async twin [async.go:163-171](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/async.go#L163-L171)).

> ferry's walk returns, per subtree, whether any address beneath it was **present on the plane**.
> A `*T` is materialised exactly when that bit is set.

Measured, the same four planes through both:

| plane | `DeepEqual` against a fresh zero | the presence bit |
| --- | --- | --- |
| nothing under `/Opt` | `nil` | `nil` |
| `/Opt/A` and `/Opt/N` present, **both zero** | `nil` | `&{A:"" N:0}` |
| `/Opt/A` present and non-zero | `&{A:"x" N:0}` | `&{A:"x" N:0}` |
| explicit `Null` at `/Opt` | `nil` | `nil` |

Row two is the defect 5.7 names, reproduced exactly: a subtree the plane really did set to all zeros is indistinguishable from one nothing touched.

**The important part is that 5.7 and 5.1 are one fix and not two.**
5.7's own recommendation is to thread a bool through the walk, and in xload that bool would have been wrong, because its `Loader` cannot report presence: a zero and a miss are one observation there, so the bit would have been set by both or neither.
The repair is only correct because ADR-0004 made absence a kind first.
That is this ticket's half of 5.1, which ADR-0001 split between #5 and #8: #5 made the boundary able to say it, and #8 makes the walk consume it.

**The survey's cost claim does not reproduce, and this ADR says so.**
5.7 calls the `DeepEqual` probe expensive.
Measured on the same walk with each probe attached, it is 595.7 ns against the presence bit's 372.7.
That is a real difference and it is not what "expensive" implies.
The correctness half of 5.7 reproduces exactly, and it is the whole argument.

> **Amended under [#122](https://github.com/onhotpath/ferry/issues/122): the bit becomes an outcome value, and the sentence above is what makes the change safe rather than what has to change.**
>
> As published this section said the walk returns, **per subtree**, whether any address beneath it was present, and the consequences said "the walk now returns a bool per subtree".
> **The rule survives and the shape does not.**
> The bit becomes one field of a value, because the walk turned out to have **three** per-subtree facts and not one, and shared mutable state was carrying the other two:
>
> ```go
> type outcome struct {
>     wrote  bool     // this section's bit; still exactly what materialises a *T
>     minted []string // the dynamic addresses this subtree realised
>     writes []string // the writes this subtree staged
> }
> ```
>
> The two new fields are **lists and not bits**, which is the part that does the work.
> A parent needs the addresses themselves, not the fact that there were some, and carrying them upward is what retires the two shared collections the walk kept beside the bit: the minted set the Dump side accumulated, and the staged-write list the encode phase accumulated.
>
> **They compose by append in task order and the bit composes by OR**, so a parent's answer is a function of its children's returns and of nothing a sibling wrote.
> Task order rather than completion order is what keeps the result and the error report deterministic under any scheduler.
>
> **A shared counter gives a deterministically wrong answer**, and that is why the shape moved rather than the count.
> Reproduced: a sibling's write increments the counter a parent reads, so a pointer subtree that wrote nothing materialises because its neighbour did.
> That is row two of the table above, arriving through a different door, on the write path, five years after xload committed the same class of mistake with `reflect.DeepEqual`.
> An outcome value composes at each container by combining children rather than by reading a location any of them may have written, so the parent's answer is a function of its own subtree and of nothing else.
>
> **And it is what makes [ADR-0010](0010-the-entry-point-and-the-schema-cache.md)'s scheduler seam usable.**
> ADR-0010 handed [#20](https://github.com/onhotpath/ferry/issues/20) a hazard rather than a drop-in: it reproduced a data race under `-race` on this ADR's own bit, at `present = present || p`.
> A value returned per task has nothing to race on, and [ADR-0019](0019-the-concurrency-model.md) is where that is measured and relied on.
>
> Evidence: `prototype/concwalk` on [`proto/04-concurrency`](https://github.com/onhotpath/ferry/tree/proto/04-concurrency), which materialises the shared-counter defect deterministically, asserts that outcomes compose, and runs the walk under `-race`.



### What this hands [#11](https://github.com/onhotpath/ferry/issues/11)

#11 is blocked on this ticket and owns every spelling, so this is the interface between them, stated as obligations rather than as suggestions.

**Three options exist and need names.**

- One meaning "this leaf's value when the plane reports `Absent`", carrying text.
- One meaning "fail if the plane reports `Absent`".
- One meaning "do not write this address when the Go value is its zero value", which is ADR-0005's already-adopted `omitzero` model.

**Five refusals belong at schema compile.**

- A default whose text does not parse into the field's type, by the leaf's own parser.
- A default on a composite.
- `required` on a dynamic composite, which is a slice, a map, or a pointer to either.
- `required` together with a default.
- `omitzero` together with a default that is not the field's zero value.

**And one diagnostic rule**, because these stack: admissibility is checked first, and a contradiction between two options is reported only if both survived it.
Otherwise one field's single mistake reports as three errors.

**One grammar property this ADR requires.**
An empty default must be expressible and must be distinguishable from no default at all, because `""` is a legitimate default value and "leave the field alone" is a different instruction.
This is the same class of problem ADR-0003 already handed #11, where a tag has to be able to name a segment containing the grammar's own punctuation; default text has the same need for the same reason, and 5.10's second half is the cautionary case, where xload's grammar splits on `,` so `env:"K,delimiter=,"` cannot be written at all.

**One thing #11 does not inherit.**
Where a declaration attaches is decided here, not there: it attaches to the static address shape, so a declaration inside a map value or a slice element is written once and applies to every realised element.

### What this ADR does not decide

- **The spelling of every option above, and where contradictions are enforced**: [#11](https://github.com/onhotpath/ferry/issues/11), as ADR-0001 assigned.
- **The error types** every refusal here produces: [#9](https://github.com/onhotpath/ferry/issues/9)'s convention, applied rather than invented.
  This ADR's refusals are per address and are joined and sorted, which is ADR-0001's determinism invariant applied rather than re-decided.
  Whether a `required` failure stops the walk or is aggregated is #9's, and this ADR assumes aggregation without depending on it.
- **Whether the codec chain is invoked for an `Absent` or a zero value**: [#12](https://github.com/onhotpath/ferry/issues/12)'s.
  This ADR defines what the states are and what each means to a Go field; #12 owns the invocation rule, and survey item 5.9's "decoders never see an empty input" is #12's to fix.
  What #12 inherits from here is that a declared default arrives at the leaf as a `String` `Value` at an address, so whatever the chain does for a plane value it should do for a default, and that `Absent` means no write rather than a value to convert.
- **Whether a codec sees the raw boundary `Value` or the donated one**: [#12](https://github.com/onhotpath/ferry/issues/12)'s, and this ADR assumes nothing about it.
- **The composed order of omission, defaults and the encoder on Dump.**
  This ADR decides that omission is evaluated against the Go value before anything converts it, which is `omitzero`'s shape in `encoding/json/v2` and not `omitempty`'s.
  What converts it, and whether an encoder can change the answer, is [#12](https://github.com/onhotpath/ferry/issues/12)'s.
- **The entry point, and whether a caller supplies a seed value**: [#16](https://github.com/onhotpath/ferry/issues/16).
  This ADR gives it two constraints.
  A Load must not be offered as an in-place refresh of a value a previous Load populated, or the leak measured above ships.
  And if either refusal above is ever lifted by an Option, a compile-affecting Option becomes part of the schema cache key, where an option value is unhashable for the reason ADR-0004 measured.
- **Whether either refusal is ever lifted, and by what.**
  Both are liftable by a load Option and neither by a tag option.
  This ADR ships no Option and records the route so that adding one later is a decision rather than a discovery.
- **The watch and reload API**: [#13](https://github.com/onhotpath/ferry/issues/13), which inherits the same constraint.
- **How a registered codec declares a default**, if it ever should: [#19](https://github.com/onhotpath/ferry/issues/19).
  The mechanism here needs nothing from it, because a codec's type is a leaf and a leaf takes a text default like any other.
- **How template generation reaches the defaulted value of a type.**
  Measured: `required` fires on a Load from an empty plane, so the defaulted zero value is not reachable by Load alone.
  That is [#14](https://github.com/onhotpath/ferry/issues/14)'s to resolve, and the mechanism it needs is the compiled schema holding the defaults, which this ADR puts there.
- **Whether Dump ever gains a delete**, as discussed above, and whether the presence observation is spelled as a callback, a recorder or a returned report, which belongs with [#25](https://github.com/onhotpath/ferry/issues/25).

## Consequences

- One rule carries the whole ADR: `Absent` does not write, and everything present is applied.
  A contributor deciding a new case applies it rather than looking for a table entry, and every answer above is derived from it plus ADR-0005's leaf rule.
- A default is a `Value` at an address, so ferry has one conversion authority in both directions and a default is checkable, showable to a template, and impossible to drift from the field it belongs to.
  The cost is that it is text and therefore leaf-only, and a composite default is a compile error with "seed the value instead" as its remedy.
- Two default mechanisms coexist and they partition by expressiveness: declared defaults are inspectable and leaf-only, seeded values are arbitrary and invisible.
  Where both apply the declared one wins, because ferry cannot tell a seeded value from a zero one.
- `Null` is refused at every leaf that has no null, which is ADR-0005's existing rule with `Null` included rather than a new one.
  The visible cost is confined to a blank YAML key and an explicit `null`, measured; a commented-out line removes the key, so it is `Absent` and takes the default.
  The argument is recoverability rather than transfer: strictness can be relaxed per type by a registered codec, and leniency cannot be tightened at all, because the zeroing would precede the codec chain.
- `encoding/json/v2` disagrees, measured: `null` into a Go `int` gives 0 there and a refusal here, and that is a change v2 made from v1.
  ferry departs from it knowingly, for the second time after `time.Duration`, and for a ferry-specific reason rather than a preference.
- A field at its default is dumped, so a ferry-written artefact is self-describing and changing a default does not change what a stored plane means.
  The cost is a noisier file, and the place to fix that is ADR-0001's Milestoned delta dump rather than a rule about defaults.
- `omitzero` and a non-zero default are a contradiction that schema compile refuses.
  Without that refusal an explicit zero is omitted on Dump and reappears as the default on Load, measured, which is a round-trip violation ferry's own harness would not catch because the harness dumps and loads with one schema and one default.
- `*T` at a leaf is the one type that expresses unset against zero, and it does so fully on Load from any plane and on Dump only from a plane with a null.
  That is a narrower claim than "pointers express optionality" and it needs to be documented as narrowly as it is true, next to ADR-0005's finding that a pointer adds nothing at a composite.
- An array element with a declaration behaves like a struct field and a slice element does not, which is the second place ADR-0005's static-against-dynamic difference surfaces as a behavioural difference between two types a user will treat as interchangeable.
- `Absent` is a `Reader`-side kind only, so ferry's value model is asymmetric between the directions, and that asymmetry is a conformance case rather than a note.
  A sink is never handed an observation it has no honest answer for, which is the prototype defect ADR-0004 recorded on itself.
  *(Built under [#306](https://github.com/onhotpath/ferry/issues/306), and it is two checks rather than one, because the rule has two halves with two owners.
  That no `Set` call carries an `Absent` is core's, provable inside core with no plane in sight, and core's own tests count it.
  What a driver can still get wrong is the consequence, so `Driver` case 20 is the plane-side half: an address a dump was silent at holds nothing afterwards, read back over a seed that has to survive.
  [ADR-0014](0014-what-ferrytest-exports.md) records the case.)*
- An omission is the absence of a `Set` call and not a deletion, so the plane is untouched at every address the dump was silent at.
  *(Amended under [#254](https://github.com/onhotpath/ferry/issues/254): as published this bullet read that a replacing sink and a patching sink give different results for one dump and both are correct, and that ferry has no delete verb and this ADR does not add one.
  Both halves are false now.
  Only the patching reading is correct, because omission is no statement; and deletion is expressible, as [ADR-0004](0004-source-and-sink.md)'s optional `Unsetter`, which is a verb a caller reaches deliberately and can never reach by accident.)*
- Presence survives the walk, which is the mechanism ADR-0001's plane-inspection milestone commits to, and it costs nothing measurable.
  It is an observation of a Load rather than a property of a field, so it adds no type to the closed set and no schema view.
- `required` is a presence assertion and nothing more, so `FOO=` satisfies it and `Null` at a `*T` satisfies it while yielding nil.
  Someone will want "present and non-empty" and the answer has to be ADR-0001's parse-don't-validate position rather than a new option.
- `required` is admissible only where an address's children come from the type, so it works on leaves, structs, pointers and arrays and is a schema-compile error on a slice or a map.
  That reuses ADR-0003's static tier rather than inventing a line, gives one meaning on every plane class, and repairs a draft defect where `required` on a non-pointer struct was accepted and enforced by nothing.
  The cost is that "this list must be configured" has no ferry spelling, and the honest reason is that the reading users want is not writable: a missing key and `origins: []` are one observation, and a seventh `Value` kind would not fix it because three of six plane classes cannot express an empty list at all.
- Three refusals in this ADR are liftable later by a load Option and by no tag option, because ADR-0001 freezes the tag vocabulary and does not freeze Options.
  Refusing is therefore the direction that keeps a decision open, and this ADR ships no Option.
  A compile-affecting Option would become part of #16's schema cache key, and an option value is unhashable for the reason ADR-0004 measured, so the two routes are not equally cheap.
- Diagnostics check admissibility before contradictions, so one field's mistake reports as the mistakes that are real rather than as every rule it happens to trip.
- An in-place reload leaks the previous load's values for addresses the plane has lost, under every rule considered.
  This ADR cannot fix it and instead constrains #16 and #13 to define reload as producing a new value.
  Until one of them lands, that hazard exists in whatever the working entry point is.
- A struct merges into a seeded value field by field and a slice or a map is replaced wholesale.
  Both follow from the one rule and neither looks like it does, so this is a documentation obligation rather than a design choice.
- The walk now returns a presence fact per subtree, which is a change to the walk's own signature rather than a new interface, and it is what makes both `*T` materialisation and the presence observation possible from one pass.
  *(Amended under [#122](https://github.com/onhotpath/ferry/issues/122): as published this bullet said the walk returns a **bool** per subtree.
  It returns an `outcome` value whose first field is that bool, and whose other two fields are the minted-address list and the staged-write list that shared collections used to carry.
  The rule and the signature change are unaffected; the return type is wider.)*

## Items from the xload survey

The survey is [`docs/research/generics-and-modern-go.md`](../research/generics-and-modern-go.md), section 5, against `github.com/gojekfarm/xtools` at [`a90b3aa`](https://github.com/gojekfarm/xtools/commit/a90b3aad2133248cec50f6b4d6e37b0d9e788adb).

**5.7, `reflect.DeepEqual` used as a "was anything set?" probe.**
Addressed, and this ADR owns it outright.
The walk returns a presence bit per subtree and a `*T` is materialised from it.
Reproduced first, in ferry's own shape: a subtree the plane set to all zeros is `nil` under the `DeepEqual` probe and `&{A:"" N:0}` under the bit.
The survey's cost claim did not reproduce as stated, at 595.7 ns against 372.7 on the same walk, and this ADR says so rather than repeating it.
The finding that matters instead is that 5.7's own recommended repair would have been wrong in xload, because a bool threaded through a walk whose loader cannot report presence is set by a zero and a miss alike.

**5.1, the `Loader` signature cannot express absence.**
Addressed, and this is the half ADR-0001 left here.
ADR-0004 made the boundary able to say it; this ADR makes the walk consume it, at the three places 5.1 lists as consequences.
`required` becomes a presence test rather than `val == "" && meta.required`, so `FOO=` satisfies it.
`setVal`'s silent no-op on empty becomes a decision rather than an accident: `Absent` does not write and every present value does, empty included.
And `decode`'s refusal to hand an empty string to a decoder is #12's to fix, with the states it needs defined here.

**5.10, composite values are string-splitting.**
Bears on this ADR in one place and points the same way ADR-0003 did.
A composite default in a tag would have to spell a list inside the tag, which reintroduces the defect at the grammar rather than at the value, so a composite default does not compile.
Measured separately, the JSON-ish spelling is not even writable: `reflect.StructTag.Get` truncates `default=["value"]` at the embedded quote and returns `origins,default=[`, which `go vet` catches and `go test` does not.
5.10's second half, that xload's grammar splits on `,` so `env:"K,delimiter=,"` is unwritable, is #11's and is named above as a constraint default text inherits.

**5.12, `SerialLoader` precedence is unexpressible.**
Bears on this ADR and is already fixed by ADR-0004.
It is cited here because its underlying cause, that a later source can never override a value back to empty, is the same fact as this ADR's answer to the ticket's fourth ask, one level down: present beats absent, and empty is present.

**5.14** was enumerated rather than assumed, all four items.

- *Two ways to set the loader.*
  Bears on this ADR directly, because ADR-0004 records that a `Static` source under `FirstOf` is "both the defaults layer and the memory plane", and that is a second way to express a default.
  Not avoided by construction, so it is answered instead: core ships exactly one way to **declare** a default, the combinator remains a user's composition rather than a ferry-supplied alternative, and the measured difference is that the combinator spells the address set twice and silently drops a default when a field is renamed.
- *The `CanAddr` loop that can only run once.*
  Bears on this ADR, since it is a defect in the reflection walk and this ADR changes what the walk does per kind.
  Not carried over: the walk addresses a field once, and the prototype's walk contains no such loop.
- *The non-deterministic select on a cancelled context.*
  Concurrency, and [#20](https://github.com/onhotpath/ferry/issues/20)'s.
  This ADR neither fixes nor worsens it.
  It does add one thing #20 should know: the walk now returns a presence fact per subtree, so a concurrent walk would have to combine those rather than only its errors.
  *(Amended under [#122](https://github.com/onhotpath/ferry/issues/122): this was published as "combine those bits", and the hazard it names was real - [ADR-0010](0010-the-entry-point-and-the-schema-cache.md) later reproduced a data race on exactly that bit under a goroutine-per-task scheduler.
  It is discharged rather than inherited: the per-subtree facts are returned as one `outcome` value that composes from children, so there is no shared location for a scheduler to combine in place.
  [ADR-0019](0019-the-concurrency-model.md) is #20's answer and it relies on this.)*
- *Value receivers on `Error()` where pointers are returned.*
  Bears on the `required` failure and on the default-validation refusals, which are the error types this ADR produces.
  Deferred to [#9](https://github.com/onhotpath/ferry/issues/9)'s convention rather than pre-empted, as ADR-0003, ADR-0004 and ADR-0005 all did.

The remaining items are unaffected by this ADR.
