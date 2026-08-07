# 14. What `ferrytest` exports, and what the conformance suites contain

Status: Accepted
Date: 2026-08-03
Ticket: [#35](https://github.com/onhotpath/ferry/issues/35)

## Context

[ADR-0002](0002-core-and-sub-modules.md) put `ferrytest` in core by **route (b), authority**: an obligation core imposes but cannot compile-check, shipped as the thing that checks it.

> The suite is only worth anything when it ships from the same place as the rule, because it *is* the rule in executable form.

Seven ADRs have since assigned obligations to that package, and none of them owned its shape.
[ADR-0001](0001-what-ferry-supports.md) named the property harness and the driver conformance suite; [ADR-0002](0002-core-and-sub-modules.md) admitted the memory plane and the recording sink; [ADR-0003](0003-how-a-leaf-addresses-a-plane.md) put five obligations on the memory plane; [ADR-0004](0004-source-and-sink.md) added "Bind must succeed against an unreachable plane", cases for the three optional interfaces, and a driver declaring its carryable kinds; [ADR-0005](0005-the-supported-type-set.md) named eleven symbols and the completeness check; [ADR-0007](0007-the-codec-chain-and-its-precedence.md) added codec cases; [ADR-0009](0009-typed-codec-registration.md) added three things at once and filed this ticket.

Everything above is already decided.
What was not decided is what the package looks like, and two of the accumulated obligations were not obviously compatible: ADR-0005 and ADR-0009 specify `RoundTrip` with different signatures, and their two completeness checks are the same check over two tables.

Two later tickets then handed this one work rather than questions.
[#41](https://github.com/onhotpath/ferry/issues/41)'s audit wrote its conformance-case list as thirty assertions "because a rule that no suite holds is a rule the next implementation can drop the same way", and found `TestCoreTypesComplete` red on arrival with seven admitted kinds having no proof row.
*(#41 closed those seven on [`proto/tip`](https://github.com/onhotpath/ferry/tree/proto/tip) at `0d86c00` while this ADR was in review, so the membership half is closed twice over.
What that table still does not have is a golden column, which is the half this ADR is for.)*
[ADR-0013](0013-what-a-plane-holds-is-a-published-interface.md) then found that the golden column - the artefact it makes ferry's second compatibility promise - is on the wrong one of two proof types.

This ADR is written from a throwaway prototype on branch `proto/35-ferrytest`, which never merges.
Seven probes, `Z35=<n|all> GOTOOLCHAIN=go1.27rc2 go run .` from `proto/`, and the whole proposed package written as compiling code over the tip's own engine.
Every number below is from that prototype unless it cites the survey.

## Decision

### What this closes, and what it does not

| The ticket asked | Closed | Where |
| --- | --- | --- |
| the package's exported surface, as a list | **yes**, 27 names *(19 as published; the reconciliation is [there](#the-surface))* | [The surface](#the-surface) |
| one entry point or two for the harness and the driver suite | **three**, and two of them are not what the ticket expected | [Three entry points, not five](#three-entry-points-not-five) |
| how a registry reaches the harness | **as an Option**, and a parameter is refused on ADR-0004's own grounds | [How a registry reaches the harness](#how-a-registry-reaches-the-harness) |
| whether the completeness check is one function over two tables | **yes, over three**, joined by `reflect.Type` | [One completeness check](#one-completeness-check-joined-by-type) |
| what the codec conformance suite contains | **yes**, seven cases | [The case lists](#the-case-lists-and-who-owns-each) |
| one package or several | **one**, on the import graph | [The surface](#the-surface) |
| whether it has a stability promise | **two promises, in one package** | [Two stability promises](#two-stability-promises-and-only-one-of-them-is-semvers) |

Three questions this ADR had to answer that the ticket did not name:

| Not asked for, answered anyway | Where |
| --- | --- |
| whether a proof can carry its own type, which decides how the completeness check joins | [One completeness check](#one-completeness-check-joined-by-type) |
| how a suite reports, given that it runs against third-party code | [How a suite reports](#how-a-suite-reports-and-why-it-is-not-testingt) |
| which of the audit's thirty cases belong here at all | [The case lists](#the-case-lists-and-who-owns-each) |

**Two things this ADR does not close.**

- **The text arm's unbounded set.**
  ADR-0007 records that the set of types implementing `encoding.TextMarshaler` is unenumerable, so no completeness check can reach it.
  Nothing here changes that, and the surface below is the reason it stays visible: `Complete` reports over three tables and says which, so a member that is in none of them is absent by construction rather than by oversight.
- **Whether `ferrytest` is ever promoted into the root package.**
  ADR-0011 raised it for its error primitive and left it here; this ADR keeps everything in `ferrytest` and states the trigger rather than the answer.

### What a consumer writes

This section is first, because every decision below is a decision about these files, and because `ferrytest` has **four** consumers, three of which are outside this repository.
All four were written as compiling code and run; all four report clean.

**Core's own test.**
It is the only one that runs `CoreTypes()` for itself, and it is why `CoreTypes()` is exported at all: a registrant appends to it and a driver runs it.

```go
func TestCore(t *testing.T) {
    for _, p := range []ferrytest.Plane{ferrytest.MemPlane(), yamlPlane(t), flatPlane()} {
        ferrytest.RoundTrip(t, p, ferrytest.CoreTypes())
    }
    for _, s := range ferrytest.Complete(nil, ferrytest.CoreTypes()...) {
        t.Errorf("core type set: %s", s)
    }
}
```

**A driver author's test.**
This is the whole file.
`driver/*` is a CI glob ([ADR-0002](0002-core-and-sub-modules.md)), so it has to be one call, and a suite a driver author can partially adopt is a suite that measures nothing.

> **Amended under [#95](https://github.com/onhotpath/ferry/issues/95): the kind constants take a `Kind` prefix, and the `Value` constructors take the plain words.**
> As published, the three call sites below wrote `[]ferry.VKind{ferry.Absent, ferry.Null, ferry.Bool, ferry.Number, ferry.String, ferry.Bytes}` for the kinds, and `ferry.Str("")` and `ferry.Num("8080")` for the values.
> Those two spellings are the same six words, spent twice: Go has one package namespace, the constants took the plain names first, and `Str` and `Num` are what was left after they did.
> Writing them side by side in one ADR is what made it visible, and #66 found the rest of the pattern was `Nul`, `Boo` and `Byt`, which is not an API this project is willing to publish.
> **The constants move rather than the constructors**, because a constructor is written far more often than a kind constant is named, so the shorter spelling belongs on the constructor.
> `log/slog` disambiguates both sides at once, `slog.KindString` beside `slog.StringValue`, and pays for the collision twice; only one side needs to move.
> **Nothing in the decision moves.**
> The kinds are still called Absent, Null, Bool, Number, String and Bytes, the set is still closed at six, and [ADR-0004](0004-source-and-sink.md) needs no amendment because it names the kinds and says "constructors per kind" without spelling one.
> Absent keeps no constructor: the zero `Value` already is it.
> Verified before deciding, rather than assumed: a package-level `func String(string) Value` beside `VKind.String()` and `Path.String()` raises no `revive` `confusing-naming` finding under this repository's lint configuration.

> **Amended under [#101](https://github.com/onhotpath/ferry/issues/101): `Open` returns an `Instance`, and the surface is twenty names.**
> As published, `Plane.Open` was `func() (ferry.Source, ferry.Sink)`, and the call site below built its temp path inside that closure and never let it escape.
> Nothing then handed a suite the plane's contents, so [`Driver` case 11](#the-case-lists-and-who-owns-each) - the golden artefact, and the only case that sees a representation - was specified and could not run.
> [ADR-0013](0013-what-a-plane-holds-is-a-published-interface.md) is why that is not cosmetic: a round trip tests a function against its own inverse, so it structurally cannot stand in for the case, and the compatibility promise ferry makes about what a plane holds had nothing behind it.
> **`Open` now returns `ferrytest.Instance{Source, Sink, Contents}`**, where `Contents func() ([]byte, error)` yields that instance's raw contents, read after the dump has finished and after any `Committer` has committed.
> *(A fourth member, `InContext`, was added under [#185](https://github.com/onhotpath/ferry/issues/185), on the minor-release optionality this amendment argues for two paragraphs below.
> The amendment is with [the case it made runnable](#the-case-lists-and-who-owns-each).)*
>
> **Why not a field on `Plane` beside `Open`, or one on `Artefact` per row**, which is what the issue proposed.
> `Open` mints a fresh, empty plane on every call, because this ADR's own rule is that every equivalence subtest gets a fresh destination.
> The contents are therefore a property of an *instance* and not of the description, and a nullary function anywhere above the instance can only mean whichever one was minted last.
> The only way to write either field honestly is to hoist the destination out of the `Open` closure into the enclosing scope, and the cost of that was measured rather than argued: two golden artefacts against a hoisted destination with no manual reset, and the second is compared against the first artefact's plane as well as its own.
>
> > The damage is not the failing golden case.
> > It is that cases 1 to 10 are then running against a **shared destination**, which is the exact defect the fresh-destination rule exists to prevent, in the one package that publishes the rule.
>
> A struct minted inside `Open` has nowhere to hoist to, so the honest spelling is the only spelling.
>
> **A struct rather than a third return value**, which would be honest by the same construction and was built and compared.
> Three reasons, and the third is the one that decides it: a positional third result is unlabelled at the call site; it puts `Open` exactly on this repository's fixed `function-result-limit` of three, so the next per-instance need is a second breaking change; and **a struct can gain a member in a minor release where a signature cannot**.
> An optional interface asserted on the `Sink` was also built - it is the only candidate that breaks nothing - and refused because it makes a driver author write a wrapper type rather than a closure, and because a misspelled method name would silently delete the one case that sees a representation.
>
> **What `Want string` costs a plane that is not one document**, recorded here because `driver/kv` ([#86](https://github.com/onhotpath/ferry/issues/86)) is the first plane in that position and this is where the shape was chosen.
> A key-value store is a set of pairs with no document behind them, so a single-string golden obliges such a driver to render its store before any row can be written against it.
> That is defensible and it is stated as an obligation rather than solved: **a plane holding more than one storage unit must render deterministically and injectively over stores.**
> It is the same obligation [ADR-0003](0003-how-a-leaf-addresses-a-plane.md) already puts on that driver's key function, for the same reason - a rendering two different stores share is a golden row that cannot see the difference, exactly as a plane key two addresses share is data loss.
> If #86 measures the string golden genuinely insufficient, `Instance` gains a member in a minor release, and that optionality is part of why this shape was taken over the third-return-value one.
>
> **The memory plane needs no change and stays valid**: it mints an `Instance` with no `Contents`, because it stores the boundary `Value` itself and has no spelling to hand back, so case 11 is skipped for it rather than failed.
> The signal for the skip is an empty `Golden` rather than a nil `Contents`, so the two cannot disagree and a plane that pins a spelling while yielding no way to read it is refused loudly rather than passing quietly.
>
> **Nothing about `Instance` reaches the root `ferry` package**: no core type, no core import of the shape, and no method on `ferry.Source` or `ferry.Sink`.
> It is test apparatus, and it is admitted on that basis.
> **The cost is a breaking change to the apparatus promise below**, which is ordinary semver API rather than the suites' looser one.
> It is paid at v0, with one first-party call site and no external one, and ADR-0013's own reasoning is why it is paid now rather than later: cheap today, expensive after anything is stored.

```go
func TestConformance(t *testing.T) {
    ferrytest.Driver(t, ferrytest.Plane{
        Name:  "yaml",
        Kinds: []ferry.VKind{ferry.KindAbsent, ferry.KindNull, ferry.KindBool, ferry.KindNumber, ferry.KindString, ferry.KindBytes},
        Open: func() ferrytest.Instance {
            p := filepath.Join(t.TempDir(), "c.yaml")
            return ferrytest.Instance{
                Source:   yaml.Source{Path: p},
                Sink:     yaml.Sink{Path: p},
                Contents: func() ([]byte, error) { return os.ReadFile(p) },
            }
        },
        Golden: []ferrytest.Artefact{
            ferrytest.Golden(struct{ B []byte `ferry:"b"` }{[]byte("hi")}, "b: !!binary aGk=\n"),
        },
    })
}
```

> **Amended under [#109](https://github.com/onhotpath/ferry/issues/109): `Artefact` is opaque and `Golden[T]` is the only way to build one.**
>
> As published the `Golden` rows above were composite literals of `Artefact{Value any; Want string}`, and that literal cannot be dumped.
> `Dump[T]` compiles its schema from its type parameter deliberately, so `Value any` hands it `interface{}`, which names no address:
>
> ```
> Dump[any]      err = ferry: interface {} is not a struct ferry walks, so it names no address...
> Dump[probeCfg] err = <nil>
> ```
>
> A slice of `Artefact` is heterogeneous by design - that is the whole point of a golden table - so the row's type has to be captured where the compiler still has it, which is a constructor: `func Golden[T any](v T, want string) Artefact`, capturing a closure over `Dump[T]`.
> **The literal becomes a call and nothing else moves**: the expectation is still the driver's own statement, still on the `Plane`, and `Artefact` is still opaque to everything but this package.
>
> **Teaching core's `Dump` to unwrap an interface-kinded root was prototyped, measured and not taken.**
> It works in 63 lines confined to `entry.go`, and it applies to every interface-typed root rather than to `any` specifically, because `reflect` sees a kind: a call site erasing its type stops being coverable by `Compile[T]()`'s pre-flight, and the written key set stops being readable from the source.
> That is a capability question on its own merits rather than a fix for a test helper, and it is [#134](https://github.com/onhotpath/ferry/issues/134)'s.

> **Amended under [#157](https://github.com/onhotpath/ferry/issues/157): `Plane` gains an `Except` field, and the exported surface stays at twenty-three names.**
> As published, `Plane` is `Name`, `Kinds`, `Open` and `Golden`, and `Kinds` is the whole of what a driver says about which values its plane can hold.
> `Kinds` is kind-granular, and `driver/yaml` is the first plane that is not: it carries `String` and cannot spell the one string that is not valid UTF-8, because a YAML string is Unicode.
> [ADR-0005](0005-the-supported-type-set.md) owns the rule that makes that unspellable without new machinery, and it is amended there with the reasoning and the measurement.
> What is this ADR's is the surface:
>
> ```go
> // Except narrows Kinds to the values inside a declared kind that this
> // plane's own format cannot spell.
> Except func(v ferry.Value) bool
> ```
>
> **A field and not a name.**
> The exported list below is fixed by decision and asserted mechanically by `TestExportedSurface`, and a field adds no entry to it, so this is a minor-release addition to the apparatus promise rather than a change to it.
> That is the same property `Instance` was chosen for under #101 and is why it was reached for again: a struct can gain a member where a signature cannot.
> A new exported type or function would have been a twenty-fourth name and would have needed this ADR's list changed rather than annotated.
>
> **What it costs, stated rather than found later.**
> `Plane` is now five words and 80 bytes, which is over `gocritic`'s `hugeParam` threshold, so `Driver` and `RoundTrip` both report a heavy by-value parameter.
> The remedy the linter names is a `*Plane`, and that is the published signature and every driver's call site in and out of this repository.
> A description copied twice per conformance run is not worth a breaking change, so the two signatures carry a `//nolint` naming the field, and the reasoning lives on the field's own documentation.
>
> **It is not a way to skip a case**, and the enforcement is structural rather than a rule in prose.
> An excepted value is routed to `Driver` case 1's refusal half, which is where a kind the plane never declared goes, so excepting a value buys a refusal the driver has to make rather than a case that stops running.
> The narrowing is per case and not per proof, so the three string values `driver/yaml` does carry are still round-tripped, and a narrowed proof keeps each case's own number so a report names the case as `CoreTypes()` spells it.

**A registrant, discharging [ADR-0001](0001-what-ferry-supports.md)'s transferred guarantee.**
ADR-0009 measured that this has to be about four lines or nobody writes it.

```go
func TestCodec(t *testing.T) {
    reg := ferry.NewRegistry(ferry.StringText[netip.Addr]().AsMapKey())

    proofs := []ferrytest.Proof{
        ferrytest.Type("netip.Addr", ferrytest.Eq[netip.Addr],
            ferrytest.At(netip.Addr{}, ferry.String("")),
            ferrytest.At(netip.MustParseAddr("192.0.2.1"), ferry.String("192.0.2.1")),
        ),
    }
    ferrytest.RoundTrip(t, ferrytest.MemPlane(), proofs, ferry.WithRegistry(reg))
    ferrytest.Codec(t, reg)

    for _, s := range ferrytest.Injective(reg,
        netip.MustParseAddr("192.0.2.1"),
        netip.MustParseAddr("::ffff:192.0.2.1"),
        netip.MustParseAddr("fe80::1%eth0"),
    ) {
        t.Errorf("as a key: %s", s)
    }
    for _, s := range ferrytest.Complete(reg, append(ferrytest.CoreTypes(), proofs...)...) {
        t.Errorf("registry: %s", s)
    }
}
```

**An ordinary user, who is not testing ferry at all.**
This is the largest audience and the one the ticket did not name, and it is the reason [two stability promises](#two-stability-promises-and-only-one-of-them-is-semvers) are needed rather than one.
ADR-0002 admitted the memory plane on exactly this ground: "xload ships `MapLoader` and people reach for it constantly; if ferry ships nothing, every user writes the same ten lines and gets the same things wrong".

```go
func TestMyConfig(t *testing.T) {
    cfg, err := ferry.Load[Config](ctx, ferrytest.Static(map[ferry.Path]ferry.Value{
        ferry.At("port"):    ferry.Number("8080"),
        ferry.At("timeout"): ferry.String("30s"),
    }))
    ...
    // and: what did my struct actually map to?
    mapped, err := ferrytest.Record(ctx, Config{})
}
```

### The surface

**Twenty-seven exported names, in one package.**

| group | names |
| --- | --- |
| what a caller describes | `Plane`, `Instance`, `Artefact`, `Golden`, `T` |
| the proof | `Proof`, `Type`, `Case`, `At`, `Inside`, `Eq`, `BitEq`, `SliceEq`, `MapEq`, `PtrEq` |
| the suites | `RoundTrip`, `Driver`, `Codec`, `Complete`, `Injective` |
| the apparatus | `MemPlane`, `Static`, `Record` |
| the table | `CoreTypes` |
| the assertion | `Want`, `DiffErrors`, `CheckErrors` |

> **Amended under [#114](https://github.com/onhotpath/ferry/issues/114) and [#237](https://github.com/onhotpath/ferry/issues/237): a `Case` names the address it proves, the count is twenty-seven, and a `Case` with no golden fails.**
>
> As published a `Case` was a value and a golden, and the address it was read at was the harness's own wrapper address and nothing else.
> Two things followed from that and both were defects.
>
> **A composite carrying elements had no golden it could carry.**
> Such a composite writes its elements one address down and writes nothing at its own address, so the only `ferry.Value` a case pinned there could hold was `Absent`.
> That is a plane reporting that it does not hold an address rather than a representation ferry chose, and it is why [the table](#one-completeness-check-joined-by-type) carried two of ADR-0005's three mandated composite values and not three.
> #114 recorded the collision as an arithmetic one - three mandated values against a fixed 57 - and it is structural: no case could name an element.
>
> **And a forgotten golden passed in silence for exactly those types.**
> `Case` is an exported struct, so `Case[T]{Value: v}` omits `Want`, and the zero `ferry.Value` is `Absent`.
> For a leaf that failed, because ferry writes a value at the leaf's address; for a composite carrying elements the recorded value at the container address was `Absent` too, and the comparison succeeded against a golden nobody wrote.
> `Case`'s own doc promised the opposite, and the promise was enforced by nothing shipped: the structural check lived in a `_test.go` and ran over `CoreTypes()` only, never over a third party's proofs.
>
> **What moves.**
> `Case` gains an `Addr ferry.Path`, the address inside the value where the golden is pinned, relative to the value's own address, and the zero `Path` is the value's own address - so every case written before this reads exactly as it did.
> `Inside(value, addr, want)` is the constructor for a case that names one, beside `At(value, want)` for a case that does not, and it is the twenty-seventh name.
> A case whose `Want` is the zero `ferry.Value` is reported by `RoundTrip` rather than run, before the narrowing `Driver` applies, so a forgotten field is a forgotten field in both halves of case 1 rather than a demand that a driver refuse a dump nobody wrote.
> **`Absent` is not a golden anywhere.** "The golden is required, not optional" is now true for the types where forgetting it was easiest, which is what #237 asked for.
>
> **The count of cases moves with it**, to 58: the composite row carries its mandated third value, `[]string{""}`, with its golden pinned at the element rather than at the container.
> ADR-0005 is amended in place beside this, saying what the third value is for now that it is expressible.
> Two shipped conformance tests leaned on the hole deliberately - `driver/env` and `driver/http` each ran dynamic composites with an `Absent` golden column - and both now pin what ferry encoded at a minted address inside the value, which is a stronger assertion than the one they replace and the only one those addresses ever had.

> **Amended under [#175](https://github.com/onhotpath/ferry/issues/175): the count is twenty-six and the table is the whole of it.**
>
> As published this read "nineteen exported names", with a parenthetical raising it to twenty, and the table below it named twenty-four.
> Neither number was the shipped set and the drift was accumulated rather than decided: `Complete` arrived under [#79](https://github.com/onhotpath/ferry/issues/79), `Driver`, `Codec` and `Injective` under [#83](https://github.com/onhotpath/ferry/issues/83), `Instance` under [#101](https://github.com/onhotpath/ferry/issues/101), `Want`, `DiffErrors` and `CheckErrors` under [#169](https://github.com/onhotpath/ferry/issues/169), and `Golden` under [#109](https://github.com/onhotpath/ferry/issues/109), while the sentence above the table never moved.
> Two names were in no row at all: **`MemPlane`**, which the section's own "what is not on the list" paragraph explains the absence of a memory plane *type* for, correctly, while the constructor ships and is exported; and **`Golden`**, which is #109's.
> `TestExportedSurface` locks exactly the twenty-six above.
> **Nothing here is a new decision** - every name arrived with a ticket that argued for it - and what moves is that the count in the prose is the count in the table, and that `ferrytest/surface_test.go` no longer carries a doc comment explaining why the specification is wrong.

> **Amended under [#169](https://github.com/onhotpath/ferry/issues/169): the error assertion is on this list, as apparatus.**
> As published this table had no row for it, and the omission was not a decision: [ADR-0011](0011-the-error-model.md) publishes `DiffErrors`, `CheckErrors` and `Want`, says "it ships in `ferrytest` only", and hands this ADR the package's surface rather than the semantics.
> The three names were then absent from both documents at once, so nothing was built, and ferry's own position that message text is not API pointed at a remedy that did not exist.
> **The row is `the assertion`: `Want`, `DiffErrors`, `CheckErrors`.**
>
> **They are apparatus and not suites**, which is the distinction [the stability section](#two-stability-promises-and-only-one-of-them-is-semvers) turns on.
> A suite may gain a case in a minor release because the rule it executes grew; this check runs no cases and answers a question, so a change to what it reports is a change to what the caller asked for, and it moves only where semver says it may.
> `CheckErrors` takes the `T` of [the reporting section](#how-a-suite-reports-and-why-it-is-not-testingt) rather than the `*testing.T` ADR-0011 published, and `DiffErrors` returns `[]string`; that is this ADR's own split between a suite and a check, applied to the pair ADR-0011 had already drawn it for.
> The amendment recording the move is in ADR-0011, where the signature was published.

**One package, and the argument is the import graph rather than taste.**
Every suite needs `Plane`; the driver suite needs the proof table; the codec suite needs the registry and the relations; all three need the assertion sink.
A split would make a driver import two paths to run one CI job, and ADR-0002 admitted this package on the line that a rule is only worth anything when it ships from the same place as the rule.
Two places is one more than one.

**What is *not* on the list is as much of the answer as what is.**

- **No memory plane type.**
  `Plane` is a description a caller fills in, and `MemPlane()` returns one; the `Source` and `Sink` behind it stay unexported.
  ADR-0003 puts five obligations on the memory plane - key by the canonical rendering, never fold, reject a duplicate write loudly, enumerate segment-wise, and be unusable as the negative case for the driver rule - and every one of them is a property to be relied on rather than a field to be set.
- **No `Recorder` type.**
  ADR-0001's schema-extraction pattern is one call, `Record`, and ADR-0004's `Recorder` combinator is what implements it.
  Exporting the combinator would invite a user to wrap a sink and drop its optional interfaces, which is [#10](https://github.com/onhotpath/ferry/issues/10)'s own recorded defect.
- **Nothing from a `Codec`.**
  ADR-0009 established that a proof needs *nothing* from a registration, because it exercises the codec through the ordinary walk, and that this is what keeps a registration opaque.
  It stays true.
  *(Amended under [#227](https://github.com/onhotpath/ferry/issues/227): this bullet named the type `Reg`, which [ADR-0017](0017-the-registration-api-and-the-value-it-builds.md) renames to `Codec`, with `KeyCodec` for the key form.
  The constraint is unchanged and is easier to state: a registration is still opaque, and `ferrytest` still needs nothing from one.)*
  The one thing the package needs is `(*ferry.Registry).Types() []reflect.Type`, which is a property of the registry and opens no registration.

### How a registry reaches the harness

ADR-0005 says `RoundTrip(t, Plane, ...Proof)`.
ADR-0009 says `RoundTrip(t, *Registry, Plane, ...Proof)`.
They are one function and the ticket names this as the collision to resolve.

Three candidates, written at their call sites rather than argued about.

**(a) a `*Registry` parameter**, ADR-0009's own spelling.
Every call site pays for the uncommon case with a `nil`, and a caller who wants the tag key ADR-0008 put in the same schema cache key has nowhere to put it.

**(b) a field on `Plane`.**
A registry is not a property of a plane: it decides how a Go value becomes a `Value`, which happens before any plane is reached, and a driver author filling in a `Plane` literal would have to know what to put there.

**(c) Options, which is what the entry point already takes.**

```go
ferrytest.RoundTrip(t, plane, proofs)
ferrytest.RoundTrip(t, plane, proofs, ferry.WithRegistry(reg))
```

> **Amended under [#110](https://github.com/onhotpath/ferry/issues/110): the third line above was published and cannot work, and a tag key is refused rather than honoured.**
>
> As published the third call was `RoundTrip(t, plane, proofs, ferry.WithRegistry(reg), ferry.WithTagKey("cfg"))`, and the option is spelled `TagKey`, which [ADR-0010](0010-the-entry-point-and-the-schema-cache.md) settles.
> What it cannot do is compose.
> A proof's case carries a **bare value**, and ADR-0010 refuses a root that compiles to a leaf, so the harness supplies the annotated struct the value travels in.
> `TagKey` names the key ferry reads for **every** type in the call, that wrapper included, so a tag key other than the harness's own leaves it mapping no address and every case in the run fails identically.
> An `Option` is opaque by construction, so there is no partial application to reach for: the harness cannot apply one to a caller's types and withhold it from its own.
>
> **The suite owns the wrapper, so the wrapper's tag key is not the caller's to change.**
> `RoundTrip` and `Driver` each resolve the Option list once, up front, against the structs they dump, and refuse a list that leaves them unable to compile one - which is one report rather than one identical failure per case.
> Every other Option still passes straight through, `WithRegistry` among them.
>
> Two other shapes were named and are not taken.
> Building the wrapper with `reflect.StructOf` so its tag follows whatever key the options name would make the published call work, and costs a reflect-built type plus a way to read the key back out of an opaque `Option`.
> Making a case carry a struct rather than a bare value removes the wrapper entirely, and changes `Case`, `At` and every proof in `CoreTypes`, losing the terse form [ADR-0005](0005-the-supported-type-set.md) sells.

**(c) is taken, and the deciding argument is not ergonomics:**

> A `*Registry` parameter would be a **second way** to say what `ferry.WithRegistry` already says.

That is survey item **5.14**'s first entry, "two ways to set the loader", which ADR-0004 avoided by construction and ADR-0009 avoided again by making the default registry a `Registry` rather than a second mechanism.
A harness with its own registry parameter reintroduces it in the one package whose job is holding ferry to its own rules.

It also settles the thing neither ADR could see alone.
ADR-0008 put the tag key in the same cache key and ADR-0012 added Options of its own, so a parameter list would have grown twice more since ADR-0009 wrote that signature.
Options are already the mechanism for everything that keys a schema.

**The cost, stated exactly**: the proofs become a slice rather than the variadic tail, so a one-proof call reads `RoundTrip(t, plane, []ferrytest.Proof{p})`.
That is the whole price, it is paid once per call site, and `CoreTypes()` already returns a slice, which is the call every driver makes.

### The proof is one type with three columns, and it runs through the entry point

> `Type[T](name string, eq func(a, b T) bool, cases ...Case[T]) Proof`, with `Case[T]{Value T; Want ferry.Value}`.
> The golden is **required**, not optional.

ADR-0005 specified exactly this and the prototype has **two** proof types, neither of which is it:

| where | shape | golden column | runs through |
| --- | --- | --- | --- |
| `harness.go` | `Type[T](name, eq, values...)` | **no** | `Dump` and `Load` |
| `r10_proof.go` | `Prove[T](name, eq, cases...)` with `Want` | yes | the superseded `walk.go`, over a `map -> map` transform |

That is [ADR-0013](0013-what-a-plane-holds-is-a-published-interface.md)'s finding, and this ADR is where it is repaired.
The merged proof against the nanosecond `time.Duration` codec ADR-0005 rejects by name:

```
merged proof, through the entry point   4 failures, one per case, each naming the text
harness.go's proof, the same codec      11/11 proofs pass
```

**Why the golden is checked through a wrapping sink rather than an option.**
ADR-0012's `Observe` is Load-side only, so there is no other way to see what ferry **encoded** before a driver spelled it.
The recording sink ADR-0002 admitted as apparatus is what makes column three possible on a real driver, which is the first time that admission has been load-bearing rather than convenient.

**What it costs, counted.**
Nineteen rows, **58 cases each carrying a golden**, against eighteen rows of bare values.
*(As published this said "eleven rows", which was the count on the tip this prototype was cut from.
[#41](https://github.com/onhotpath/ferry/issues/41) has since added seven, and none of them carries a golden.)*

> **Amended under [#114](https://github.com/onhotpath/ferry/issues/114): the count is 58, and it is derived rather than fixed.**
>
> As published this read "57 cases", which was the total the table could reach while every case was pinned at one address.
> The number is not a decision of this ADR's: it is the sum of ADR-0005's value lists, and each row's share is asserted from the property that put its values on the list rather than from the number.
>
> | rows | each | cases |
> | --- | --- | --- |
> | `bool` | the type | 2 |
> | `string` | ADR-0005's four mandated values | 4 |
> | `int`, `int8`, `int16`, `int32`, `int64` | the zero and both bounds of the width | 15 |
> | `uint`, `uint8`, `uint16`, `uint32`, `uint64` | the upper bound and the zero, which is also the lower | 10 |
> | `float32`, `float64` | ADR-0005's nine mandated values | 18 |
> | `[]byte` | nil, empty and raw bytes, a leaf at kind `Bytes` | 3 |
> | `[3]byte` | raw bytes | 1 |
> | `time.Duration`, `time.Time` | the pinned representation ferry owns by type identity | 2 |
> | `[]string` | ADR-0005's three mandated composite values | 3 |
>
> That is 58, and the row that moved is the last: it carried nil and empty, and now carries the third value ADR-0005 mandates, `[]string{""}`, whose golden is pinned at the element rather than at the container.
> The arithmetic in #114 - "the minimum total is exactly 57, and there is no slack anywhere" - was right about the arithmetic and wrong about which number was load-bearing.
> The 57 was a measurement of a table with no address column, so it is this figure that moves and not ADR-0005's list.
That is the point rather than the price, in ADR-0005's own words: "a contributor adding a type cannot avoid stating what it looks like on a plane".

**A golden file was considered and refused.**
Writing the column to `testdata` and comparing would be the same table with a better diff, and a reviewer would see a representation change as a file change - which is exactly what ADR-0013 wants.
It is refused because a golden file grows an `-update` flag within one release, and then the change ADR-0013 exists to make visible is a flag.
ADR-0002's "the harness is a table, not a generator" is the same instinct one level up.

**And the column earned its place on its first run.**
ADR-0005 says "a composite with no elements writes `Null` at its own address, whether it is nil or empty".
`[]byte` is admitted as a **leaf**, at kind `Bytes`, so that rule does not reach it, and `[]byte(nil)` writes `null` where `[]byte{}` writes `bytes("")`.
The relation `SliceEq` conflates them deliberately; the golden column does not, and it reported the difference the first time the table was run.
Nothing is wrong in either ADR - the rule is about composites and this is a leaf - but a reader following ADR-0005's prose gets it wrong, and this is the artefact that says so.

### One completeness check, joined by type

> `Complete(reg *ferry.Registry, proofs ...Proof) []string`

ADR-0005's check iterates core's identity table and the admitted kind list.
ADR-0009's iterates a registry.
They are one check over the **union of three tables**, and neither ADR could have written it, because ADR-0005 had no `Registry` and ADR-0009 had no kind list.

It returns data rather than asserting, so a registrant appends their own proofs and re-asks with the same call core makes.

**The join is by `reflect.Type`, and this is a correction the prototype forced.**
The tip joins by **name**, and its own comment gives the reason: "`Proof` is an interface over a type parameter the check cannot recover".
It can.
One method on the interface recovers it:

```go
type Proof interface {
    Name() string
    Type() reflect.Type
    ...
}
```

The name then becomes a label for messages rather than a key, and the hand-written special case that spells `[N]byte` so a proof named `[]byte` can discharge an array member disappears.
A kind is not a type, so the union names **one representative type per admitted kind**, and a new kind arriving with no representative panics rather than being silently skipped, which is the drift the check exists to catch.

**And the completeness half of [#41](https://github.com/onhotpath/ferry/issues/41)'s D18 is closed here too.**

```
D18, against the merged table       0 missing
```

> **Corrected under #41.**
> As published this block also read `D18, against harness.go's table     7 members with no proof`, and named `int16`, `int32`, `int64`, `uint`, `uint8`, `uint16` and `uint32`.
> That was true of the tip this ADR's prototype was cut from and is not true of [`proto/tip`](https://github.com/onhotpath/ferry/tree/proto/tip) at `0d86c00`, where `harness.go` carries **18 rows** and `go test ./...` is **green**: #41 closed the membership half independently.
> So this ADR closes it a second time rather than first, and the comparison the block was making is withdrawn.
> **What is not withdrawn is the reason the table is replaced anyway**, which was never the row count: `harness.go`'s proof has no golden column on `0d86c00` either, so the eighteen rows are eighteen rows of bare values and the measurement below stands unchanged.

ADR-0013 states why the membership half is not housekeeping: the promise is exactly as wide as the table, so an admitted member with no row is outside it by accident rather than by decision.

### Three entry points, not five

The ticket asks whether the harness and the driver suite are one entry point or two.
The answer is **three**, and the third is not a split of the first two.

```go
func RoundTrip(t T, p Plane, proofs []Proof, opts ...ferry.Option)
func Driver(t T, p Plane, opts ...ferry.Option)
func Codec(t T, reg *ferry.Registry, opts ...ferry.Option)
```

`Driver` **calls** `RoundTrip` rather than duplicating it, which is what keeps `driver/*` a single-call CI glob.
`RoundTrip` stands alone because a registrant uses it against the memory plane with no driver in sight, which is how ADR-0001's "registration carries the proof" is discharged.

**And twenty of the audit's thirty cases do not belong here at all**, which is the answer the ticket did not anticipate.
Grouping the audit's list by *whose code is under test* rather than by subject:

| group | cases | whose code |
| --- | --- | --- |
| driver conformance | 11 | a third party's driver |
| codec conformance | 6 | a third party's codec |
| round-trip property | 3 | core's set, against a third party's plane |
| **schema compile** | 12 | **core's own** |
| **the walk** | 8 | **core's own** |

ADR-0002 admits to `ferrytest` what core cannot compile-check about **somebody else's** code.
A schema-compile refusal is core checking itself, and it belongs in core's own `_test.go`.
Putting it in `ferrytest` would make ferry's own unit tests part of a published surface a driver's CI depends on.

That removes twenty cases from the surface and leaves the package three verbs.

### The case lists, and who owns each

Written as assertions rather than prose, which is [#41](https://github.com/onhotpath/ferry/issues/41)'s own convention and the reason the list is countable.

**`Driver`, thirteen cases.**

1. Every proof the plane can express, and a **loud refusal** for every one it declared it cannot carry ([ADR-0005](0005-the-supported-type-set.md), [ADR-0004](0004-source-and-sink.md)).
2. `Bind` succeeds against an **unreachable** plane, and the refusal lands inside the open ([ADR-0004](0004-source-and-sink.md)).
3. `Get` at a **container address** returns `Absent` with a **nil error**, for a populated list, an empty list, an empty map and a missing key.
4. `Get` returning a non-nil error reaches the caller as an error and never as `Absent`.
5. `Children` at those addresses returns the element addresses, kinded.
6. `Commit` runs only on success, `Close` always runs, and a `Close` failure appears in the reported error set.
7. A driver producing a plane key refuses a **non-injective** key function over the address set, before any I/O, naming both addresses ([ADR-0003](0003-how-a-leaf-addresses-a-plane.md)).
8. A key function retains nothing across opens, asserted on the **write** side, which is where retention refuses a legal write ([ADR-0012](0012-the-caller-held-binding.md)).
9. A sink accepts a **dynamic** address its static table never held.
10. A driver reading its plane from the context refuses at open when it is absent, with `ErrPlane` ([ADR-0012](0012-the-caller-held-binding.md)).
11. A **golden artefact**: a fixed value, dumped, compared against fixed expected plane contents ([ADR-0013](0013-what-a-plane-holds-is-a-published-interface.md)).
12. A sink accepts `Set` of a `Null` at a **container address** - a nil composite, an empty composite, and a nil optional section - and that address was in the set its `Bind` received ([ADR-0003](0003-how-a-leaf-addresses-a-plane.md), [ADR-0005](0005-the-supported-type-set.md)).

> **Amended under [ADR-0016](0016-the-sealed-address-model.md): case 3 is a probe, case 5 compares segments, and the suite stops building address sets.**
>
> As published, case 3 asserted that `Get` at a container address answers `Absent` or `Null` with a nil error.
> That question does not compile under the sealed address model: `Get` takes a `LeafAddr` and a container's own address is a `SectionAddr` or a `CompositeAddr`.
> Case 3 is now the same obligation asked through `Prober.Probe`, which is the method that owns it: a populated composite is `PresencePresent`, and a nil composite, an empty composite and a nil optional section are `PresenceNull`, which is the row this case's own [#136](https://github.com/onhotpath/ferry/issues/136) amendment already settled.
> `Prober` is optional, so a reader that cannot answer is skipped rather than failed.
>
> **That is also the whole of [#208](https://github.com/onhotpath/ferry/issues/208)'s blocker dissolving.**
> #208 recorded that this case forbade a driver from refusing at any `Get` whose address might be a container, because the suite called `Get` at container addresses outside a walk, and a driver could not tell a container address from a leaf one.
> The suite no longer makes that call and the driver no longer has to tell: a repeated key arriving at a `LeafAddr` is a refusal with a home.
>
> Case 5 compares `[]Segment` rather than `[]Path`, because that is what `Children` returns.
> Case 12 asks that the container address reached `Bind` **at its kind**, which is a stronger question than the one it asked and the one the typed set makes askable.
>
> Cases 8 and 9 reach a value-minted address through a dump rather than by calling `Writer.Set` directly, because the three address kinds are sealed and only the compiler mints one.
> For the same reason the suite no longer constructs an `AddressSet`: it compiles a fixture type and captures the set core hands the driver's own `Bind`.
> That is strictly better than what it did, because the suite can no longer hand a driver an address the compiler would not have minted.

> **Amended when the sealed address model shipped: the list is thirteen cases, and case 13 is the one case 3 cannot make.**
>
> As published there were twelve, and case 3 asks only about containers the suite itself populated and containers it blanked.
> Neither half asks what a plane answers at a container the schema owns nothing under, and neither half puts a key in the plane that belongs to no address of the schema.
> Measured: a driver answering a section's presence out of every plane key sharing the section's prefix passed case 3 unchanged, and fabricated a whole section out of an unrelated ambient variable.
>
> 13. A plane key belonging to **no address of the schema** says nothing about a container whose key space it shares: the suite dumps one leaf of another schema whose name begins with a section's, binds the schema that has that section, and `Probe` at the section answers `PresenceAbsent`.
>
> It makes no assumption about how a plane spells a key, because the neighbour is written through the plane's own sink: a tree plane puts it beside the section and a flat plane puts it inside the section's prefix, and the required answer is the same either way.
> `Prober` is optional, so a plane that cannot answer is skipped, exactly as in case 3.
>
> **Nothing here is a new decision.** ADR-0016 already says a section's members come from the type, and this is the case that holds a driver to it.
>
> Case 3 also settled a misattribution while this landed: it dumped its second fixture before asking whether the reader probes, so a plane with no `Prober` and no `Ensurer` reported a case-3 failure that was case 12's. The probe question is now settled first.

> **Amended under [#182](https://github.com/onhotpath/ferry/issues/182) and [#269](https://github.com/onhotpath/ferry/issues/269): the list is owed four more cases, and they are recorded here rather than filed one per issue.**
>
> As published, the twelve cases above were the whole `Driver` list, and each of them is a rule some ADR had already stated.
> [ADR-0004](0004-source-and-sink.md) has since been amended to carry the held-binding contract, and a contract is exactly the thing this package exists to hold third-party code to.
> Four of its rules have no case:
>
> - **Concurrent opens.** One binding, many goroutines calling the driver's open at once, clean under `-race`, with each open's minted set independent of every other's. That is the obligation ADR-0004 now states and the one this ADR's case 8 asserts only one open at a time.
> - **A second dump through one held sink binding**, with a different value each time, neither refused by the other's minted addresses. Case 9 asserts a dynamic address arrives; this asserts the binding is not spent by the first one.
> - **`(nil, nil)` is refused**, as an error naming the driver rather than a dereference: a `Bind` returning a nil `OpenFunc` with a nil error, and an open returning a nil `Reader` with a nil error. This is the negative half of the suite rather than a driver behaviour, so it belongs in the failure list beside the other structural violations.
> - **`MemPlane` satisfies all of the above**, which is [#269](https://github.com/onhotpath/ferry/issues/269): the memory plane is the one plane whose whole job is to be the reference implementation, and it does not currently hold the concurrent-open line.
>
> **Nothing here is a new decision**, which is why this is a note rather than an ADR: every one is a rule already written down, missing its check.
> They are implementation-phase work and they land with the binding-contract batch, not on their own.

> **Amended when the binding-contract batch shipped: all four landed, and the list is fifteen cases.**
>
> 14. **Concurrent opens.** One binding, many goroutines entering the driver's own open at once, and many loads through one `Binding`, every one of them succeeding and reading what the plane holds ([ADR-0004](0004-source-and-sink.md), [ADR-0012](0012-the-caller-held-binding.md)).
> 15. **A second dump through one held sink binding**, with a different value at the same addresses, taken by the sink and landing on the plane.
>
> **Case 14 creates the concurrency and asserts the observable half.** What reports a data race is the driver's own `go test -race`, which no suite can run on its behalf, so the case's job is to enter the open from many goroutines at once and to hold every one of them to succeeding and to reading what the plane holds.
> Each half runs once sequentially first and the case is skipped where that fails, because a plane that cannot be opened at all says nothing about opening it twice, and cases 4, 6 and 10 already report a broken open.
> **The write half opens concurrently and walks nothing.** ADR-0004 obligates concurrent calls to `OpenWriterFunc` and says nothing about a plane tolerating two dumps at once; that is [ADR-0019](0019-the-concurrency-model.md)'s, and a case that dumped from two goroutines would be answering it by accident.
>
> **Nothing in this suite reports from inside a goroutine.** `T` is two methods and nothing here says they may be called from two at once, so the parallel cases collect what they found and report it afterwards - which is a constraint on the suite rather than a new obligation on every reporter a caller can pass.
>
> **Case 15 re-dumps the same addresses with a different value**, because that is what a re-save is, and it reads back only the fixture's leaf so that a plane which cannot enumerate is asked nothing it cannot answer.
> Whether a sink handles an address the *new* value no longer has is a different question and is not asked here.
>
> **`(nil, nil)` is core's refusal and not a case.** A `Bind` handing back a nil open with a nil error, and an open handing back a nil reader or writer with a nil error, are refused inside `ferry` as an error naming the driver, so every caller gets the diagnosis rather than every suite. Core's own tests pin all four.
>
> **`MemPlane` now satisfies both cases.** Its duplicate-write refusal was keyed on the store, so a second dump through one binding was refused at every address the first one wrote - the reference implementation failing the rule it exists to demonstrate. ADR-0003's obligation is unchanged and is now scoped to the open, exactly as a key function's minted set is.

> **Amended when the concurrency model shipped: the list is sixteen cases, and the sixteenth is [ADR-0019](0019-the-concurrency-model.md)'s gate.**
>
> As published the list ends at fifteen, and none of those cases asks anything of a reader that declares it tolerates overlapping calls, because no such declaration existed.
> ADR-0019 makes serial equivalence the property that makes fanout safe to enable at all, and puts it here by name: "for a driver asserting `Concurrent`, the property belongs in the conformance suite rather than in that driver's own tests, because a driver that declares the capability is making a claim core relies on."
>
> 16. **Serial equivalence.** A reader declaring `ferry.Concurrent` produces, under every concurrency budget, the destination and the error report it produces serially ([ADR-0019](0019-the-concurrency-model.md)).
>
> **Nothing here is a new rule**, which is why this is a note rather than an ADR: ADR-0019 states the gate and this is the case that runs it.
> The case is skipped, out loud, for every reader that does not declare the capability, which is every driver that has not opted in - so it is not a case that can turn a CI red for a promise its author never made.
> **Both halves are asked**, because the promise is about both: one plane written once and loaded under several budgets, and that same plane read for a second fixture's required addresses, which it holds none of, whose aggregate must read identically however it was scheduled.
> The two fixtures are addressed apart so that one instance can serve both, which is the stronger statement as well as the cheaper one: a plane where they did overlap reports it.
> Every load mints its own destination, which is this ADR's fresh-destination rule and ADR-0019's own named trap: a shared destination is what makes a broken second walk pass.
> **What a driver declares is what runs**, so the capability joins the line a run says about itself under [#201](https://github.com/onhotpath/ferry/issues/201): a reader that tolerates overlap says so before any case runs.

> **Amended under [#220](https://github.com/onhotpath/ferry/issues/220) and [#221](https://github.com/onhotpath/ferry/issues/221): the list is seventeen cases, and the seventeenth is dump-is-replace.**
>
> As published the list ends at sixteen, and case 15 says in as many words that whether a sink handles an address the *new* value no longer has is a different question and is not asked here.
> [ADR-0004](0004-source-and-sink.md)'s amendment ships `Unsetter` and makes that question answerable, so the case it named is written.
>
> 17. **A shorter dump replaces a longer one.** For a sink declaring `ferry.Unsetter`, a value whose sequence lost members, dumped over one that had them, loads back as the second value and only the second ([ADR-0004](0004-source-and-sink.md)).
>
> **The shape shrinks and the values do not**, so a plane that fails the case fails it for the one reason the case is about, and the report names the list it found rather than a field that differs.
> It is read back through `Load`, because that is where the residue was invisible: nothing in a plane says which dump a member came from.
> **It is skipped, out loud, for every sink that does not declare the capability**, which is every plane that replaces its contents wholesale and has nothing to forget as well as every plane that genuinely cannot - so it cannot turn a driver's CI red for a claim its author never made.
> The read half needs `Enumerator`, since a member the second dump did not write is exactly what no load can see without one, and a source that cannot list is asked nothing and says so.
> `MemPlane` implements `Unsetter`, because the reference implementation is the one plane whose whole job is to demonstrate the contract, and a capability no plane in this repository carried would leave the case unexercised.

> **Amended under [#201](https://github.com/onhotpath/ferry/issues/201): a run says what it scaled to, and the description gains nothing.**
>
> #201 records that `Instance` carries capabilities as optional fields, that three of its four members mean a capability by being nil or not, and that a skipped case is indistinguishable from a passing one for any reporter that is not `*testing.T`.
> It asked for a reconsideration once a real per-request driver existed rather than for a shape, and `driver/http` now exists.
> **The reconsideration is that the split is already right and the reporting was not.**
>
> Which optional interfaces a driver implements is discovered by assertion on its own reader and writer, so no field on `Instance` could describe one and none can be filled in wrongly.
> What the description declares is exactly what cannot be discovered: what the plane is called, which kinds it carries end to end, what it cannot spell inside them, how to mint a fresh one, whether its plane arrives in a `context.Context`, how to read its contents back, and what its own spelling of a fixed value is.
> A capability field for `Prober` or `Committer` would be a second way to say something the value already says, which is survey item 5.14's defect in the package whose job is holding ferry to its own rules.
>
> **What ships is the line that was missing.** A run reports, once and up front, which halves the plane mints, which of the description's optional members are filled in, and which of the six optional interfaces its reader and writer implement, and every remaining branch that skips a case says so.
> Two conformant drivers execute very different numbers of cases, and that is the point of an optional interface; what was wrong is that the difference was invisible.
>
> **`Instance` does not move**, and that is the same argument #201 makes for waiting: a shape decided with nothing on the other side of it is the mistake ADR-0012 spent its deferral argument avoiding, and no driver in or out of this repository is asking for one.

> **Amended under [#185](https://github.com/onhotpath/ferry/issues/185): case 10 had never run, because nothing in this ADR let a `Plane` describe a driver it applies to.**
> As published, and after [#101](https://github.com/onhotpath/ferry/issues/101) made `Open` return an `Instance`, the description is a plane minted directly by `Plane.Open` and it mentions no `context.Context` anywhere.
> A driver whose plane is per request carries no contents in its `Source` at all, so there was no field it could fill in, and the case shipped as an unconditional skip against every plane.
> **The assertion does not move.**
> The refusal still lands at the open and never at `Bind`, still lands per load, and still carries [ADR-0011](0011-the-error-model.md)'s `ErrPlane`, which is [ADR-0012](0012-the-caller-held-binding.md)'s ruling that a plane that was never supplied is the limiting case of a plane that cannot be reached.
> **What moves is the description**: `Instance` gains an optional `InContext func(context.Context) context.Context`, which puts that instance's contents into a context.
> The suite runs every case's own I/O under the context it returns, so a per-request driver can run the whole suite rather than only the case named after it, and case 10 runs where it is set and is skipped, explicitly, where it is not.
> A plane that leaves it nil runs under `context.Background` exactly as it always has, which is every driver in this repository, verified case by case against the published skip.
> Both halves are asked, because ADR-0012 puts the same rule on a sink whose plane is per request as on a source.
>
> **It is on `Instance` and not on `Plane`, and #185's own sketch put it on `Plane`.**
> A per-request plane's contents *are* what the context carries, and `Instance` is already the one freshly minted plane, both halves of it over one set of contents.
> A decorator on `Plane` has to do one of two things and both are wrong: mint fresh contents on every call, so that two opens within one case disagree and `Instance.Contents` cannot see either, or close over contents hoisted out of `Open`, which is the shared destination `Plane.Open` exists to make impossible.
> It is a member on an existing struct rather than a signature change, which is the optionality [#101](https://github.com/onhotpath/ferry/issues/101) took this shape for: a struct can gain a member in a minor release where a signature cannot, and this is the first time that has been spent.
> **The surface table does not move**, because `Instance` is already on it and a field adds no exported name; `TestExportedSurface` locks the same twenty-six.
> That table's own staleness is [#175](https://github.com/onhotpath/ferry/issues/175)'s and is untouched here.

Case 11 is the one that is new, and ADR-0013 gives the reason a round-trip case cannot stand in for it: a round trip tests a function against its own inverse, a spelling is a *choice* of function, and changing both halves together is invisible to any test that only composes them.
The expected contents live on the `Plane` rather than being a parameter of the suite, because the spelling is the **driver's** statement about itself and ADR-0005 puts it on the driver's side of the line.

> **Amended under [#101](https://github.com/onhotpath/ferry/issues/101).**
> As published, this case had nothing to compare `Want` against: the *expected* contents were on the `Plane`, and the *actual* contents were unreachable, because `Open` minted the plane inside a closure and returned only its two halves.
> They are now reached through `Instance.Contents`, minted alongside the instance they belong to.
> **Nothing in the case moves**: the expectation is still the driver's own statement, still on the `Plane`, and still not a parameter of the suite.
> A plane with no serialization format - the memory plane - pins no `Golden` and mints no `Contents`, and this case is skipped for it rather than failed.
> The reasoning, the two rejected shapes and the measurement that rejected them are at [the driver author's call site](#what-a-consumer-writes).

Case 12 is the Dump mirror of case 3, and it was added under [#56](https://github.com/onhotpath/ferry/issues/56), which measured two engines handing a driver two different static sets for one type.
Case 3 asks what a driver answers at a container address; case 12 asks whether it was told to expect one at all.
Without the second, a driver whose static table holds a wildcard shape instead of the container address passes every existing case and refuses a legal write, which is the shape case 9 already guards from the other side.

> **Amended under [#136](https://github.com/onhotpath/ferry/issues/136): case 3 was written against a different fixture from case 12, and never said so.**
> As published, case 3 read as one universal rule, and against a plane ferry's own `Dump` wrote it is false at two of its four rows.
> What moves is those two rows and nothing else: a populated list and a missing key still answer `Absent` with a nil error, because nothing was ever written at those addresses, and an empty list and an empty map answer `Null`, because [ADR-0005](0005-the-supported-type-set.md) writes a `Null` at a composite's own address when it has no elements.
> **The two cases were never over the same fixture, which is why they read as contradicting each other and do not.**
> Case 3's four shapes are ADR-0005's **source-side** table, measured over a document a human wrote.
> Case 12's three shapes are **Go values** handed to `Dump`.
> "An empty list" means a plane node in one sentence and a Go value in the other.
> **The principle, which is wider than this case: a driver reports what the plane holds.**
> `Absent` means the plane does not hold this address, and `Null` is a present address carrying that plane's own null.
> Reporting `Absent` for a stored `Null` deletes an observation rather than renaming one, because [ADR-0006](0006-defaults-and-zero-values.md) makes a `Null` at a container address a write and an `Absent` no write at all.
> **The tree-document answer, which no ADR gave before**: at an explicit `tags: []` node in a hand-authored document a driver answers `Null`, the same as at `tags: null`.
> A present-but-empty sequence is a present address holding an empty collection, ferry's vocabulary has one word for it, and ADR-0005 already decided that nil and empty collapse.
> That is the existing rule applied rather than a new one, and it is written down here so that a tree driver's author does not have to infer it.
> Measured, and this is what makes the ruling non-arbitrary rather than a coin toss between two sentences: under the `Absent` reading [`LoadOver`](0010-the-entry-point-and-the-schema-cache.md) silently stops clearing a seeded field, a source that cannot enumerate can no longer load an empty composite at all because `Absent` sends the walk looking for children it cannot list, and `required` on an optional section refuses a plane that supplied a null.
> Through plain `Load` into a zero value the two readings are indistinguishable on every row, so the whole cost sits on the reload path.
> **Nothing in the engine moves**, and nothing in [ADR-0004](0004-source-and-sink.md), ADR-0005 or ADR-0006 moves; case 12 stands as published.
> A plane that declares no null is untouched by all of it, because an empty composite is never written there at all, and case 1 owns that refusal by name.
> Evidence: [`proto/136-container-reads`](https://github.com/onhotpath/ferry/tree/proto/136-container-reads).

**`Codec`, seven cases.**

1. `AppendText` and `MarshalText` agree ([ADR-0007](0007-the-codec-chain-and-its-precedence.md); nothing enforces it for a user type, which is why it is a case and not a promise).
2. A registered **interface** codec at its **nil** zero value, encoding.
3. The same, decoding.
4. A registration's kind is the one its constructor names.
5. A codec accepts every kind it emits ([ADR-0007](0007-the-codec-chain-and-its-precedence.md)).
6. A key codec is **injective under `==`**, over ferry's own key text ([ADR-0005](0005-the-supported-type-set.md), amended under [#31](https://github.com/onhotpath/ferry/issues/31)).
7. A **null policy** round trips both its arms, which is `isNull(load())` seen from outside ([ADR-0017](0017-the-registration-api-and-the-value-it-builds.md)).

> **Amended on the merge of [ADR-0017](0017-the-registration-api-and-the-value-it-builds.md)'s registration surface.**
>
> As published this list held six cases, case 4 read "a codec's declared kind matches what it emits", and the suite's godoc said it freezes the registry it is handed.
>
> Case 4 was written against a registration that took the kind as an argument and returned a whole `Value`, so a codec could declare one kind and emit another and core compared the two on every encode.
> A registration is named after the kind it writes now and its halves are typed by payload, so that codec is unwritable and the comparison has nothing left to compare.
> The case is re-aimed rather than dropped, at the property every registration in the program rests on instead: it registers one codec per constructor and asserts each lands at its own kind, which is a machinery question and is exactly what this suite is for.
>
> Case 7 is new, and [ADR-0017](0017-the-registration-api-and-the-value-it-builds.md) asks for it by name: `NullValue`'s law is that `isNull(load())` holds, and a policy that loads a sentinel it cannot recognise on the way back makes the round trip lie silently and only on the null path.
> What the suite can reach is the machinery, because a `Codec` is opaque and a caller's own policies are not readable from it; a registrant's own policy is a `Proof`.
>
> **The surface table does not move**: no exported name changes, and `TestExportedSurface` locks the same twenty-six.
> The freeze sentence in the godoc goes, because there is no freeze: a registry is complete when it is built, so there is no longer an ordering rule between registering and calling this suite.
> The four-line sample above is rewritten in the same surface, because it is the shape this ADR argues nobody writes if it is longer, and it is now three.

Cases 2 and 3 are ADR-0009's two wrapper defects, and they are the reason this suite is load-bearing rather than optional: the codec was correct and the wrapper was not, so **no proof a registrant can write catches them**.
One value finds both.

**`Injective` is separate from `Codec` and is not folded into it**, because ADR-0009's own argument for the `.AsMapKey()` keyword applies to the check too: `Codec` asks what is true of the codec, and `Injective` asks what is true of the *values the registrant cares about*.
Its two corrections are #31's: `T` is `comparable`, and the text comes from ferry rather than from a format function the prover supplies, because a registrant's own `String()` is not what addresses the plane.

### How a suite reports, and why it is not `*testing.T`

```go
type T interface {
    Errorf(format string, args ...any)
    Helper()
}
```

Two methods, which `*testing.T` satisfies for free.
[ADR-0011](0011-the-error-model.md) already reached this shape from the other end for its error primitive - "it returns `[]string` and takes no `*testing.T`, because the conformance suite runs against third-party drivers and wants the result as data" - and this generalises it rather than adding a second convention.

Three things it buys, and the third is the one that matters.
A suite is runnable from a probe as well as from a test.
A caller who wants to assert that a driver fails a case, which is what a *negative* conformance test needs, can capture the report.
And **`ferrytest`'s own tests can assert on what its suites say**, which is the only way a package that is authority can be held to its own rules.

`Complete` and `Injective` return `[]string` rather than taking a `T`, and the split is not arbitrary: a suite runs cases, and those two answer a question.
A caller loops over the answer and decides whether it is a failure, which a registrant mid-migration may reasonably decide it is not.

### Two stability promises, and only one of them is semver's

ADR-0002 makes this package authority rather than a convenience, so a driver's CI depends on it, and ADR-0001 makes it the only leverage core has over what it does not ship.
The ticket asks whether it has a stability promise.
It has two, and the measurement that separates them is this:

> A conformance case is a line in a list inside `Driver`.
> Adding one changes no signature, no type and no exported name.
> `apidiff` reports nothing, and a driver's CI goes red.

That is [ADR-0013](0013-what-a-plane-holds-is-a-published-interface.md)'s shape arriving at `ferrytest`, and it is the third instance in this design of a change semver cannot see:

| | what moves | what breaks |
| --- | --- | --- |
| ADR-0013 | a golden row | every stored artefact |
| ADR-0013 | a driver's spelling | every stored artefact, with the round-trip suite green |
| here | a conformance case is added | a driver's CI |

**The first two get a major version and this one does not**, and the difference has to be stated rather than assumed:

> A new conformance case does not break a driver.
> It reports that the driver was already broken, against a rule an ADR had already landed.

So:

> **The apparatus** - `Plane`, `MemPlane`, `Static`, `Record`, the relations, `Case`, `Type` - is ordinary exported API under semver.
> An ordinary user embeds it in tests that are not about ferry at all, and it must not move.
>
> **The suites** may gain cases in a minor release, and every new case cites the ADR sentence it executes.
> A case asserting a rule no ADR states is not a case, it is a new rule, and it needs the ADR first.

That second clause is ADR-0002's route (b) turned into a release policy: the suite *is* the rule in executable form, so the suite may only grow where the rules already did.

**What it costs a driver author**, stated plainly: a minor upgrade of core can turn their CI red for a rule they never read.
The mitigations are that the rule was published in an ADR before the case existed, that the case names it, and that `driver/*` being a CI glob means first-party drivers meet it first.
That is weaker than a compile-time signal and it is the only shape available, because the alternative is a suite that never grows, which ADR-0002 already measured as prose again within two releases.

### What this ADR does not decide

- **Whether the memory plane's five ADR-0003 obligations are themselves conformance cases.**
  They are properties of a thing core ships, so they are core's own tests, by the same rule that moves twenty cases out of the surface.
- **Whether `ferrytest` is ever promoted into the root package**, which ADR-0011 raised for its error primitive.
  Not now; the trigger is a use that is not a test, and nothing has one.
- **What a driver's golden artefact may assert about formatting.**
  Indentation and key order are the driver's, and ADR-0001 refuses to constrain them.
  A driver writes the artefact it wants pinned, so the question is the driver author's rather than the suite's.
- **Whether the suites take a `context.Context`.**
  They do not today; a driver whose conformance run needs cancellation is [#20](https://github.com/onhotpath/ferry/issues/20)'s.
- **The exported verb names**, which ADR-0001 left open.
  `Plane`, `Instance`, `Proof`, `Type`, `Case`, `At`, `RoundTrip`, `Driver`, `Codec`, `Complete`, `Injective`, `Static`, `Record`, `CoreTypes` and `T` are the working spellings; `At` and `T` are the two chosen on how they read rather than on a measurement.
  *(`Instance` added under [#101](https://github.com/onhotpath/ferry/issues/101), and it is a working spelling on the same terms as the rest.)*
- **The seven admitted kinds' value lists.**
  This ADR gives them rows; whether their values are the right ones is ADR-0005's standing weakness, which it names itself: "the harness's coverage is exactly its value lists and its plane list".

## Consequences

- **Nineteen exported names in one package**, which a contributor can read in one screen, and the case lists are internal so they can grow without the surface growing.
  *(Twenty since [#101](https://github.com/onhotpath/ferry/issues/101), which added `Instance` so that `Driver` case 11 could reach the contents of the plane it had just dumped to.)*
- **The golden column runs through the entry point for the first time.**
  ADR-0005 specified it, two prototypes implemented halves of it, and the half that ran through the engine could not see a representation.
  That is [ADR-0013](0013-what-a-plane-holds-is-a-published-interface.md)'s promise acquiring the instrument it rests on.
- **The table is nineteen rows and 58 cases, each carrying a golden**, where the one it replaces is eighteen rows of bare values.
  The cost is real: adding a type to core's set is now four columns of work.
  *(As published this read "grew from eleven rows to nineteen" and credited this ADR with closing [#41](https://github.com/onhotpath/ferry/issues/41)'s D18.
  #41 closed the membership half itself, at `0d86c00`.
  The golden column is what this table adds.
  The count read 57 until [#114](https://github.com/onhotpath/ferry/issues/114) gave a case an address, and the per-row derivation is recorded with the figure itself.)*
- **A registry reaches the harness as an Option**, so the package adds no second way to say what `ferry.WithRegistry` says, and the proofs become a slice.
- **The completeness check joins by `reflect.Type`**, so a proof's name is a label rather than a key and the `[N]byte` special case disappears.
  The prototype's own comment claimed the type could not be recovered; one method recovers it.
- **Twenty of the audit's thirty cases move out of `ferrytest` and into core's own tests**, on the rule that this package holds *other people's* code to core's rules and core's own unit tests are not a published surface.
- **The package carries two promises**, and a driver author can have their CI turned red by a minor upgrade.
  That is the price of a suite that is allowed to grow, and ADR-0002 already priced the alternative.
- **One thing is exported from `ferry` for this package's sake**: `(*Registry).Types()`.
  ADR-0009's constraint that `ferrytest` needs nothing from a registration survives intact; a registry is not a registration.
- **The recording sink stops being a convenience.**
  It is the only way to see what ferry encoded before a driver spelled it, because ADR-0012's `Observe` is Load-side only, so ADR-0002's admission of it as apparatus is load-bearing for the first time.
- **The `Driver` list is twelve cases**, the twelfth asserting that a container address reached the driver's `Bind`.
  It is the first case that pins what the static set *contains* rather than how a driver behaves once handed one, and it is there because two engines disagreed about that and nothing was red.
  *(Added under [#56](https://github.com/onhotpath/ferry/issues/56).
  A thirteenth landed with the sealed address model, and the amendment above says why case 3 could not carry it.
  A fourteenth and a fifteenth landed with the held-binding contract, and the list is fifteen.)*
- **A run says what it scaled to.**
  Six of the contract's interfaces are optional, so two conformant drivers execute very different numbers of cases, and the difference used to be invisible: a case that quietly did nothing read exactly like a case that passed.

> **Corrected: the counts this section restates were never re-totalled, and four of them are now wrong.**
>
> Every figure below was true when the line carrying it was written, and each was moved by a later change that amended the section owning it and not the summary repeating it.
> Nothing here is a new decision, and no case, name or interface moves: what moves is the arithmetic.
>
> - **The optional interfaces are seven, not six.** The bullet above and the #201 amendment both read "six": `Releaser`, `Committer`, `Prober`, `Enumerator`, `Ensurer` and `Unsetter`. [ADR-0019](0019-the-concurrency-model.md) added `Concurrent`, which is discovered by assertion on the reader exactly like the other six, and `ferrytest`'s own scaling line already names seven.
> - **The `Driver` list is seventeen cases, not fifteen.** The bullet above ends its parenthetical at fifteen; the two amendments after it, the concurrency model's and [#220](https://github.com/onhotpath/ferry/issues/220)/[#221](https://github.com/onhotpath/ferry/issues/221)'s, carry the list to sixteen and then seventeen, and seventeen is what ships.
> - **The exported-name count is the surface table's**, and the first bullet of this section - "Nineteen exported names in one package", raised to twenty in its parenthetical - predates [#175](https://github.com/onhotpath/ferry/issues/175)'s re-total. The table under [The surface](#the-surface) and its amendments are the authority, and this section states no figure of its own.
> - **The registrant's sample no longer compiles as printed.** It reads `reg := ferry.NewRegistry(...)` in one assignment, and [ADR-0017](0017-the-registration-api-and-the-value-it-builds.md), amended under [#299](https://github.com/onhotpath/ferry/issues/299), makes `NewRegistry` return `(*Registry, error)` and puts the panicking spelling under `MustRegistry`. A test writes `reg := ferry.MustRegistry(...)` and stays the length this ADR argues it has to be; a caller that wants the report writes the two-value form and checks it.

## Items from the xload survey

The survey is [`docs/research/generics-and-modern-go.md`](../research/generics-and-modern-go.md), section 5, against `github.com/gojekfarm/xtools` at [`a90b3aa`](https://github.com/gojekfarm/xtools/commit/a90b3aad2133248cec50f6b4d6e37b0d9e788adb).

**5.7, `reflect.DeepEqual` used as a "was anything set?" probe.**
Bears on this ADR as the reason the relation is a required argument rather than a default, which is ADR-0005's decision applied.
This ADR adds the enforcement: `Type[T]` takes the relation positionally, so a proof cannot be constructed without one, and there is no `DeepEqual` fallback to reach for when a `time.Time` or a `NaN` reports a false failure.

**5.8, type information destroyed at the boundary.**
Bears on this ADR through the golden column, which is the only place the boundary `Value`'s *kind* is asserted rather than assumed.
ADR-0005 made the kind part of the proof; this ADR is where that check finally runs against the engine.

**5.11, the YAML provider silently discards parse errors.**
Bears directly, and is `Driver` case 4: a `Get` returning a non-nil error must reach the caller as an error and never as `Absent`.
[#41](https://github.com/onhotpath/ferry/issues/41) found ferry's own walk committing exactly that defect (D16), cancelling against a driver committing its mirror (D15), and neither was visible for four prototypes.
Two cases, not one, is the lesson: a suite that only checked the pair would have stayed green.

**5.14** was enumerated rather than assumed, all four items.

- *Two ways to set the loader.*
  Bears on this ADR decisively, and is the argument that decides `RoundTrip`'s signature.
  A `*Registry` parameter beside `ferry.WithRegistry` is that defect, in the package whose job is holding ferry to its own rules.
- *The `CanAddr` loop that can only run once.*
  Bears on nothing here; this package writes no reflection of its own beyond `reflect.TypeFor[T]()`.
- *The non-deterministic select on a cancelled context.*
  Bears on nothing here.
  The suites take no context, which is stated above rather than left as an omission.
- *Value receivers on `Error()` where pointers are returned.*
  Bears on `T`, and is why it is an interface of two methods rather than a struct: a caller passes `*testing.T` and the method set question never arises.

**5.5, nondeterministic error output**, is ADR-0011's, and this ADR applies it: `Complete` and `Injective` sort before returning, so a report is one string over repeated runs.

The remaining items are unaffected by this ADR.
