# 21. How a library extends the tag surface without touching ferry's grammar

Status: Accepted
Date: 2026-08-06
Ticket: [#34](https://github.com/onhotpath/ferry/issues/34)

## Context

[ADR-0008](0008-the-struct-tag-grammar.md) settled ferry's tag grammar at four words and refused eleven others, one at a time, each with a reason.
Its governing asymmetry, inherited from [ADR-0001](0001-what-ferry-supports.md), is that a word not spent stays available and a word spent is permanent.
It also settled that the tag **key** is a caller Option defaulting to `ferry`, that ferry reads exactly one key, and that a list of keys is refused.

[ADR-0001](0001-what-ferry-supports.md) closed the vocabulary on publication, and `CONTRIBUTING.md` lists tag-grammar extension as one of three questions parked deliberately, with the instruction that nothing may ship which anticipates them.

Three concrete needs have accumulated against that.

- [#156](https://github.com/onhotpath/ferry/issues/156): the YAML driver drops an operator's node tag on a round trip, because nothing in the schema tells it that `/wait` should be written as `!mycompany:duration`.
- Documentation generation wants a human description per address, and the alternative it reaches for is a side table keyed by field name, which drifts on the first rename.
- Validation libraries want per-field constraints that ferry must never enforce itself.

This ADR is the answer to the parked question, and it is written from the prototype on branch [`proto/05-watch-grammar`](https://github.com/onhotpath/ferry/tree/proto/05-watch-grammar), module `prototype/tagext`, out of `go.work`.
Thirteen tests.

It is a separate number rather than an amendment to [ADR-0008](0008-the-struct-tag-grammar.md) because it decides a question ADR-0008 did not decide and was told not to.
ADR-0008 gets a short note pointing here.

## Decision

### An extension declares its own tag key, and ferry's namespace never opens

> ```go
> type Config struct {
>     Host string `ferry:"host,required" mylib:"retry=3,secret" docs:"desc=the host"`
> }
> ```
>
> ferry parses `ferry:"..."` with ferry's grammar and nothing else.
> A **declared** foreign key is parsed with **that extension's** vocabulary and handed back inert.
> An undeclared key is another library's business and is never claimed.

**This is the mechanism Go already has**, and the alternative that was prototyped first is worse.
That alternative put namespaced words inside ferry's own tag - `ferry:"host,required,mylib.retry=3"` - and it works, but it means ferry's tag now contains text ferry does not own, and the sentence [ADR-0008](0008-the-struct-tag-grammar.md) and [ADR-0001](0001-what-ferry-supports.md) both rest on becomes qualified.

Under separate keys it stays literally true:

```
ferry:"host,mylib.retry=3"   ->  still REFUSED: ferry has no such option
```

`TestFerryTagStaysClosed` pins exactly that.
**ferry's vocabulary is not extended.
ferry is taught to read a key that is not its own**, and those are different sentences.

**[ADR-0008](0008-the-struct-tag-grammar.md)'s "ferry reads exactly one key, and a list is refused" is unchanged and is about the same thing it was always about.**
That rule refuses reading ferry's *grammar* under two keys, because two keys on one field then give two address sets and nothing says which is meant.
A declared extension key carries no ferry vocabulary and produces no address, so it is not a second answer to any question ferry asks.
The refusal survives: `ferry:"a" mylib:"b"` still yields exactly one address, `/a`.

**[#261](https://github.com/onhotpath/ferry/issues/261) is a hard precondition.**
The shipped tag scanner substring-matches the configured key, so a `mylib` tag beside a `ferry` tag can already be misread today.
Nothing in this ADR may land before that is fixed, and it is worth fixing regardless.

### Declarations register at `NewRegistry`, beside the codecs

> ```go
> var Registry = ferry.MustRegistry(
>     ferry.NumberText[big.Int](),
>     ferry.WithTagKeys(
>         yamlext.Extension(),   // ferry.KeyExtension{TagKey: "yamlext", Words: ...}
>         docs.Extension(),
>     ),
> )
> ```
>
> ```go
> type KeyExtension struct { TagKey string; Words []Word }
> type Word         struct { Name string; TakesValue bool }
> ```

> **Amended under [#34](https://github.com/onhotpath/ferry/issues/34), on the merge of the mechanism this ADR describes: `NewRegistry`'s parameter is widened to a sealed union, because the example above does not compile against the constructor as it shipped.**
>
> As published the snippet passes `ferry.WithTagKeys(...)` as a vararg beside a `Codec`, and [ADR-0017](0017-the-registration-api-and-the-value-it-builds.md)'s constructor is `NewRegistry(codecs ...Codec)` with `Codec` sealed by an unexported method.
> A declaration is not a codec, so there was no type the example's two arguments both had.
> The ruling: `NewRegistry(items ...Registration)`, where `Registration` is a new sealed interface satisfied by `Codec` and by what `WithTagKeys` returns.
> The example above is then exactly what compiles, and [ADR-0017](0017-the-registration-api-and-the-value-it-builds.md) is amended to record the signature.
>
> Two alternatives were refused.
> A second explicit parameter for declarations breaks this ADR's own pasted-together literal and splits one complete-set constructor into two lists a caller keeps in step.
> Making `WithTagKeys` return a `Codec`-shaped no-op over no type spells a declaration as a codec, which is the thing this section is at pains not to do, and it would have put a value in the codec table that claims nothing.
>
> **What a declaration is refused for is unchanged**, and it is refused at the same moment and with the same `*ferry.Error` a codec's refusal carries.
> Two riders follow from the shipped shape and are recorded rather than decided quietly.
> The key ferry itself reads is a caller Option and not the registry's, so "claiming `ferry`'s own key" is refused at the registry against the default key, and a call whose `TagKey` names a key its registry declares is refused at that call, with the Option list, before any type is described.
> A declared key's words are read at the address the field's own `ferry` tag named, so a field marked `-`, and a field ferry reads no tag on, carry their extension words nowhere: the table is address-keyed and there is no address for them to sit at.
> A field under a slice or a map is the same case for [ADR-0003](0003-how-a-leaf-addresses-a-plane.md)'s reason - what is compiled there is an address shape, which joins no address set - so its words are held to the declaration and recorded nowhere, which is what "a driver sees extension data only for addresses it was bound to" means when the two rules meet.
>
> `Word`'s boolean field shipped as `TakesValue`, not `TakesVal` as first published, and every snippet above is corrected to match.

> **Amended under [#299](https://github.com/onhotpath/ferry/issues/299): the package-level var above is spelled `ferry.MustRegistry`.**
>
> As published the snippet reads `var Registry = ferry.NewRegistry(...)`, which no longer compiles: [ADR-0017](0017-the-registration-api-and-the-value-it-builds.md)'s constructor returns `(*Registry, error)` now, and `MustRegistry` is the panicking half a `var` declaration has to use.
> What a declaration is refused for, and the `*ferry.Error` it is refused with, are unchanged; a declaration's refusals are simply returned beside the codecs' rather than raised.

The registry is already the **outer level of the schema cache**, so a declaration joins the cache key with no new machinery.
[ADR-0017](0017-the-registration-api-and-the-value-it-builds.md)'s construction-is-the-freeze applies here too: the declarations are complete at the registry's birth and there is no window in which they are not.

Two alternatives were built.
A per-`Bind` Option makes the declaration a property of a call rather than of a program, which multiplies cache entries and lets two call sites disagree about one struct.
A global registration function is refused for every reason [ADR-0009](0009-typed-codec-registration.md) refused a global registry.

**The declaration reduces to a canonical, comparable value fixed at `Bind`.**
Hashability is asserted at build time with core's own `map[...]struct{}{}` trick, and the canonical form is order-independent, so declaring the same two extensions in the other order does not mint a second cache entry.
Fixing it at `Bind` is what stops a live watcher's binding being invalidated by anything downstream.

**A typed declaration is what makes diagnostics possible**, which is [#34](https://github.com/onhotpath/ferry/issues/34)'s third item.
`Word{Name, TakesValue}` is enough for the near-miss table to cover an extension's vocabulary without degrading ferry's own:

```
mylib:"rerty=3"   ->  unknown option "rerty": did you mean "retry"?   (mylib's vocabulary)
ferry:"requird"   ->  did you mean "required"?                        (undegraded)
mylib:"retry=3"   with no declaration  ->  refuses exactly as today
```

Collisions refuse at declaration, once, before any tag parses: claiming `ferry`'s own key, declaring one key twice, or a punctuated key.

### The table rides the `AddressSet`, so nobody plumbs anything

> ```go
> func (a *AddressSet) Extension(key string) /* an address-keyed view */
> func ExtensionTable[T any](opts ...Option) (ExtTable, error)
> ```
>
> A driver reads its own key's view at its own `Bind`, which is a handoff it already receives.
> An out-of-band consumer reads the whole table without a plane.

The `Bind` handoff is what makes this cost a caller nothing:

```go
func (s Sink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
    nodeTags := map[ferry.Path]string{}
    for addr, words := range addrs.Extension("yamlext") {
        nodeTags[addr] = words["node"]           // memoised once per schema
    }
    ...
}
```

> **Amended under [#156](https://github.com/onhotpath/ferry/issues/156), on the merge of the first consumer: the `node` word carries the tag as a document spells it, `!` and all, and the snippet above prefixes one it should not.**
>
> As published the line reads `nodeTags[addr] = "!" + words["node"]`, so a field would have been annotated `yamlext:"node=mycompany:duration"`.
> YAML has two shorthand forms, `!local` and `!!standard`, and the prefix rule can spell only the first: `!!timestamp` - which is [#156](https://github.com/onhotpath/ferry/issues/156)'s own second case, a tag the driver knows nothing about - would have to be written `node=!timestamp`, which reads as a mistake.
> Shipped, the word's value is the tag verbatim, `yamlext:"node=!mycompany:duration"`, so what a field declares is what the file holds and a round trip compares one against the other with nothing in between.
> `driver/yaml` refuses a value that is not a tag at its own `Bind`.
> The snippet above, and the same two lines in core's godoc, are corrected to `nodeTags[addr] = words["node"]`.
>
> **Two riders on the first consumer, recorded because they are this ADR's rung-three permission being spent.**
> The declaration is `yaml.Extension()` in `driver/yaml` itself and not a `yamlext` package: the tag key stays `yamlext`, because `yaml` is the key go-yaml's own marshaller reads, and a package of its own buys nothing until a second word wants one.
> The precedence question [#156](https://github.com/onhotpath/ferry/issues/156) asks - a schema annotation and the tag already in the file, both naming one address - is settled for the schema: preserving the operator's tag exists for the addresses nothing declared, and the declaration is the thing a save could not otherwise know.

The caller writes `yaml.Sink{Path: "app.yaml"}` and `ferry.Dump(ctx, cfg, sink)`, unchanged, because the registry carries the declaration and the `AddressSet` carries the table.
**A driver can only ever see extension data for addresses it was bound to**, which is a scoping property rather than a convenience.

Manual plumbing - handing the table to the driver as a driver Option - was the alternative, and it puts a line in every caller's code for a property the schema already knows.

**The table is address-keyed and minted at compile, where the address is defined once.**
That is [#156](https://github.com/onhotpath/ferry/issues/156)'s shape and it is why a side table loses: a side table keyed by field name drifts on the first rename, reproduced in the prototype, and an address-keyed table cannot, because the address is what the tag declared.

### Inert to core, and a consumer may act

The fear behind the parked question is a second authority: two things deciding what a value means, disagreeing, with no rule saying which wins.

> **Core parses declared extension words, validates them against their declaration, hands the table back, and never acts on them.**
> A driver or a library may act.
> Round-trip fidelity is then the **consumer's** proof obligation.

The line is drawn where the authority is, and it is easiest to see on a ladder of three consumers.

**A documentation generator** reads `ExtensionTable[Config]` and never meets a plane.
Load and Dump are byte-identical with and without it.
Plainly inert.

**A validation library** runs after `Load`, outside the walk, and refuses a value ferry accepted.
The plane never sees it and ferry refused nothing new.
Still inert - and the boundary is exactly here: **had ferry itself refused `retries=99` at decode, ferry would have become the validator**, and that is the second authority [#34](https://github.com/onhotpath/ferry/issues/34) is afraid of.

**The YAML driver** reads its own view at `Bind` and writes a different node tag:

```
before:  wait: 30s
after:   wait: !mycompany:duration 30s
```

The plane's bytes changed because of a tag word.
**Core converted nothing differently; the driver acted, as it already does on its own Options**, and a driver acting on its own declared vocabulary is not a second authority over ferry's semantics - it is the same authority a driver already has over how its plane spells things, which is [ADR-0018](0018-the-spelling-seam.md)'s whole seam.

The stricter rule - stop at rung two, so nothing may change plane bytes - was offered and refused, because it fails [#156](https://github.com/onhotpath/ferry/issues/156) and #156 is the only concrete driver need on the table.

**What the consumer owes is a proof**, on #156's own bar: load, dump, load, and get the same thing.
Core cannot check it, in the same family as [ADR-0004](0004-source-and-sink.md)'s three optional interfaces and [ADR-0001](0001-what-ferry-supports.md)'s rule that a registrant carries the proof of their own extension.

### Two riders, stated because they follow and were not asked for

`Validate[T]` must accept a declaration, or a user who declared one cannot validate the struct that uses it.
And a declaration option joins `TagKey` in whatever refuses a round trip through two disagreeing configurations, which is [#110](https://github.com/onhotpath/ferry/issues/110)'s.

A `go vet`-style analyzer over extension tags is deferred: it is buildable outside core over the same declarations, which makes it Enabled by [ADR-0001](0001-what-ferry-supports.md)'s bucket rule rather than a core question.

### What this ADR does not decide

- **Which extensions exist.** None ship in core, and the first consumer is [#156](https://github.com/onhotpath/ferry/issues/156)'s YAML node tag, in the driver that needs it.
- **Whether `KeyExtension` and `Word` ever move to a sub-package.** They stay at the root under the rule of three; a `ferrytag` package is available if a third distinct consumer appears.
- **Whether ferry's own vocabulary ever grows.** It does not, and this ADR is the reason it does not have to.
- **Anything about ferry's four words.** [ADR-0008](0008-the-struct-tag-grammar.md)'s, unchanged, including every one of its eleven refusals.
- **How a per-address representation is spelled.** As a driver-declared key read at that driver's `Bind`, which is this mechanism, and never as a word in ferry's tag: `ferry:"transform=uppercase"` stays refused, and the plane-wide answer is [ADR-0018](0018-the-spelling-seam.md)'s.

## Consequences

- **The parked tag-grammar question is answered without spending a single ferry word**, which is [ADR-0001](0001-what-ferry-supports.md)'s asymmetry honoured rather than traded against.
  ferry's vocabulary is closed exactly as published; what is new is that ferry reads keys it does not own, when told to.
- **The mechanism is Go's own**, so a field's tag stays readable and `json:"host"` on the same field is untouched.
  The namespaced-words-inside-ferry's-tag alternative was prototyped first and is rejected because it puts foreign text inside a tag ferry claims to own strictly.
- **[#261](https://github.com/onhotpath/ferry/issues/261) is a hard precondition** and nothing here lands before it.
- **Declarations live on the registry**, so they join the schema cache key with no new machinery, they are complete at construction, and the canonical form is order-independent and fixed at `Bind`, which is what keeps a live binding stable.
- **The table rides the `AddressSet`**, so a driver reads its own view at a handoff it already receives and a caller plumbs nothing.
  A driver sees extension data only for addresses it was bound to.
- **Diagnostics cover declared vocabularies without degrading ferry's own**, which is the third of [#34](https://github.com/onhotpath/ferry/issues/34)'s asks and the reason the declaration is typed rather than a set of strings.
- **Core is inert and a consumer may act**, with the line drawn at whether ferry's own semantics change.
  A driver acting on its declared words is the authority a driver already has over its plane's spelling; ferry refusing a value on a foreign word would be the second authority, and it is refused.
  The cost is that round-trip fidelity for such a driver is the driver's proof obligation and core cannot check it.
- **[#156](https://github.com/onhotpath/ferry/issues/156) is unblocked** and is the first consumer, which is also the test of whether the rung-three permission was right.

Evidence: `prototype/tagext` on [`proto/05-watch-grammar`](https://github.com/onhotpath/ferry/tree/proto/05-watch-grammar), thirteen tests, including `TestMultiKeyShape`, `TestFerryTagStaysClosed`, `TestForeignNearMiss`, `TestKeyCollisionsRefuse`, `TestMultiKeyDeclCanonical`, `TestRegistryToDriverPipeline`, `TestDeclarationIsTheRegistrys`, `TestDeclaredKeyTypoRefusesAtCompile`, `TestExtensionIsInert` and `TestDeclIsCanonicalAndComparable`.
