# 9. Typed codec registration, and the lifetime of the identity table

Status: Accepted
Date: 2026-08-02
Ticket: [#19](https://github.com/onhotpath/ferry/issues/19)

## Context

[ADR-0001](0001-what-ferry-supports.md) made core's type set closed and its extension explicit:

> That set is extensible only by explicit typed codec registration ([#19](https://github.com/onhotpath/ferry/issues/19)), for types ferry does not own.
> Registration extends the set, and the guarantee transfers to the registrant.
> Registering without proving is permitted and forfeits the guarantee.

This ADR is that sentence turned into an API.

Four ADRs have since put obligations on it by name, and none of them is reopened here.
[ADR-0005](0005-the-supported-type-set.md): a codec collapses a type to a leaf and a leaf needs no address set, which is what makes an interface and a recursive type expressible at all; a key codec's text must be injective; a proof is a triple of values, a relation and a boundary `Value`.
[ADR-0006](0006-defaults-and-zero-values.md): a declared default arrives as a `String` `Value` at the field's address, and a registered codec is the only way to accept `Null` into a Go type whose kind has no null.
[ADR-0007](0007-the-codec-chain-and-its-precedence.md): a registration is an entry in the same identity table the chain consults first; registering a type already in the table is a loud error; a codec declares the boundary kind it produces and core donates `String` to it; a codec is a pair, is total over its type including the zero value, and accepts every kind it emits; a codec takes no `context.Context` and cannot see call options.
All five of ADR-0007's were exercised against a scratch registration on `proto/12-codec-chain`, so they arrive here known dischargeable rather than merely stated.

What is left open is genuinely open, and one question dominates.
ADR-0007 deliberately did not touch the table's **lifetime**, because that interacts with [#16](https://github.com/onhotpath/ferry/issues/16)'s schema cache rather than with precedence.
The inherited answer is xload's, which has no registration at all: its only extension point is implementing `Decoder` on your own type, and `time.Duration` is reached by comparing `Type.String()` ([load.go:301](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L301)).
That is survey item **5.9**'s third bullet, and it is this ticket's.

This ADR is written from a throwaway prototype on branch `proto/19-registration`, which never merges.
It is built on `proto/12-codec-chain`, so every measurement runs against a real `Path`, a real `Value`, the type set ADR-0005 landed, the chain ADR-0007 landed, and a real YAML plane over real files.
Every number is from that prototype unless it cites the survey.

**Three of the eighteen probes found defects, and all three are in code this ADR proposes rather than in code it inherited.**
One is in this ADR's own headline example.
The other two are in the single piece of reflection the registration API owns, so they would have been defects in every codec anyone ever registered.
All three are recorded in full rather than quietly fixed.

## Decision

### What this closes, and what it does not

The ticket asked for seven things by name.
This table is the answer to each, so a reader can check the ADR against the ask without reading the rest of it.

| The ticket asked | Closed | Where |
| --- | --- | --- |
| what registration looks like at the call site, and whether inference works without explicit instantiation | **yes**: three constructors, and inference works everywhere there is a value argument to infer from | [Three constructors](#three-constructors-and-inference-works) |
| static registration only, or also dynamic registration by runtime `reflect.Type` | **yes: static only** | [Registration is static](#registration-is-static-and-the-entry-point-is-why) |
| **whether registration is global, per-call, or per-instance** | **yes**: the table is a value, it freezes at its first use, and core ships a default one | [The table is a value](#the-table-is-a-value-and-it-freezes-at-its-first-use) |
| how a registered codec interacts with the compiled schema in [#16](https://github.com/onhotpath/ferry/issues/16) | **yes**, as one obligation stated in one sentence | [What this hands #16](#what-this-hands-16) |
| whether encode and decode register together as a pair | **yes**, and the API makes a half **unwritable** rather than refused | [Three constructors](#three-constructors-and-inference-works) |
| the decline-and-fall-through mechanism, if any | **yes: none**, and json/v2's real mechanism does not port | [There is no decline](#there-is-no-decline-and-jsonv2s-mechanism-does-not-port) |
| whether registration can require, or only enable, a proof | **yes: only enable**, for three measured reasons | [The proof is enabled](#the-proof-is-enabled-and-can-never-be-required) |

Five questions this ADR had to answer that the ticket did not name:

| Not asked for, answered anyway | Where |
| --- | --- |
| what a registration may not be, given that ADR-0007 named only two refusals | [What a registration may not be](#what-a-registration-may-not-be) |
| how a key codec's injectivity obligation is communicated at registration | [A key codec says so](#a-key-codec-says-so-and-that-is-where-injectivity-is-communicated) |
| whether the API can accidentally foreclose ADR-0006's `Null` escape hatch | [The declared kind is a donation target](#the-declared-kind-is-a-donation-target-and-the-accepted-set-is-the-codecs) |
| how ADR-0005's named-duration hole is actually closed | [Registration is static](#registration-is-static-and-the-entry-point-is-why) |
| whether a registration may lift one of ADR-0005's four permanent refusals | [What a registration may not be](#what-a-registration-may-not-be) |
| whether core can check a codec at registration with nothing from the registrant | [Register runs the codec against the zero value](#register-runs-the-codec-against-the-zero-value) |
| what the API looks like to a consumer, which is what actually gets signed off | [What a consumer writes](#what-a-consumer-writes) |

**Three things this ADR does not close.**

- **A type with no name at a call site cannot be registered.**
  Stated as an accepted cost with the case that would reopen it named, not solved.
- **`map[time.Time]string` still collapses two distinct keys into one address.**
  That is [#31](https://github.com/onhotpath/ferry/issues/31), it is core's own set rather than a registration, and the opt-in rule below deliberately does not reach it.
  *(Closed since, by [ADR-0005](0005-the-supported-type-set.md)'s [The map key rule, restated](0005-the-supported-type-set.md#the-map-key-rule-restated), under #31.
  It reached the same shape this ADR did and applied it to core: key admissibility is declared per entry and is never inherited from the identity table.
  `time.Time` is dropped as a key type, because no text form is injective over it under `==`.)*
- **Whether the schema cache lives on the registry or beside it** is [#16](https://github.com/onhotpath/ferry/issues/16)'s.
  This ADR fixes the one property #16 has to preserve and measures what happens without it.

### What a consumer writes

This section is first, because every decision below is a decision about this file.
It is [ADR-0008](0008-the-struct-tag-grammar.md)'s tag grammar throughout, and the whole of it was run against the prototype's walk, a real `Path`, a real `Value` and a real YAML plane.

```go
package main

import (
    "math/big"
    "net/netip"
    "net/url"
    "time"

    "github.com/onhotpath/ferry"
    "github.com/onhotpath/ferry/driver/yaml"
)

type PollInterval time.Duration

type AppConfig struct {
    Listen   netip.AddrPort `ferry:"listen"`
    Upstream url.URL        `ferry:"upstream"`
    Poll     PollInterval   `ferry:"poll,default=30s"`
    MaxBytes big.Int        `ferry:"max_bytes"`
}

func init() {
    err := ferry.Register(
        // 1. The type declares its own inverse. ferry uses it, and the only
        //    thing you supply is the boundary kind.
        ferry.TextCodec[netip.AddrPort](ferry.String),

        // 2. You declare the inverse, as two functions.
        ferry.StringCodec(
            func(u url.URL) string { return u.String() },
            func(s string) (url.URL, error) {
                u, err := url.Parse(s)
                if err != nil {
                    return url.URL{}, err
                }
                return *u, nil
            }),

        // 3. Core ships this one, because ADR-0005 named the hole.
        ferry.DurationLike[PollInterval](),

        // 4. You declare the inverse AND the kind, because big.Int's text is a
        //    run of digits and a YAML plane reports it as Number.
        ferry.ValueCodec(ferry.Number,
            func(x big.Int) (ferry.Value, error) { return ferry.Num(x.String()), nil },
            func(v ferry.Value) (big.Int, error) {
                var x big.Int
                s, err := v.AsNumber()
                if err != nil {
                    return x, err
                }
                if _, ok := x.SetString(s, 10); !ok {
                    return x, fmt.Errorf("not an integer: %q", s)
                }
                return x, nil
            }),
    )
    if err != nil {
        panic(err)
    }
}

func main() {
    var cfg AppConfig
    if err := ferry.Load(ctx, &cfg, yaml.Source{Path: "app.yaml"}); err != nil {
        log.Fatal(err)
    }
}
```

`ferry.Load`'s exact signature is [#16](https://github.com/onhotpath/ferry/issues/16)'s and is written here only so the file is a file.
What the four registrations produce, run:

```
/listen      string("0.0.0.0:8080")
/max_bytes   number("1099511627776")
/poll        string("30s")
/upstream    string("https://api.example.com/v1")
```

```yaml
listen: "0.0.0.0:8080"
max_bytes: 1099511627776
poll: "30s"
upstream: "https://api.example.com/v1"
```

Loaded back from that YAML: `listen=0.0.0.0:8080 poll=30s max=1099511627776`.
Loaded back from a **flat** plane, which is env or Consul and reports `String` for everything: the same four values, exactly.
That second line is the whole reason the kind is an argument rather than a default, and it is ADR-0007's most consequential inheritance made concrete: `max_bytes` declared `Number`, so it loads from a plane that says `Number` **and** from one that says `String`.
Declaring `String` would have worked on env and failed on YAML.

**Three call sites this file does not show, because they are refusals.**

```
ferry: netip.Addr: the codec is not total over the zero value: it encodes to
       string("invalid IP") and decoding that back fails: ParseAddr("invalid IP"):
       unable to parse IP

ferry: time.Duration is in core's own set and its representation is pinned;
       define a named type over it and register that

ferry: /limits: netip.Addr has a registered codec but is not declared usable as
       a map key; a key codec's text must be injective over the key type, or two
       keys collapse into one address; add .AsMapKey() to the registration if it is
```

The first is [The zero-value check](#register-runs-the-codec-against-the-zero-value), the second is [What a registration may not be](#what-a-registration-may-not-be), the third is [A key codec says so](#a-key-codec-says-so-and-that-is-where-injectivity-is-communicated).

### Three constructors, and inference works

```go
func TextCodec[T any, PT textPtr[T]](kind VKind) Reg
func StringCodec[T any](format func(T) string, parse func(string) (T, error)) Reg
func ValueCodec[T any](kind VKind, enc func(T) (Value, error), dec func(Value) (T, error)) Reg

func (r *Registry) Register(regs ...Reg) error
```

`Reg` is opaque and the only way to make one is a constructor that takes **both halves**.
That is ADR-0007's "a codec is a pair" made unrepresentable-otherwise rather than documented, and it is why the pair question needs no runtime check.

**The three differ by what the registrant hands over, and nothing else.**
The first draft of this ADR called the general form `TypeCodec` and could not explain to a reviewer how `TextCodec` and `StringCodec` differed, which is a naming defect rather than a design one.

| constructor | you supply | the type must |
| --- | --- | --- |
| `TextCodec[T](kind)` | a kind, and nothing else | implement `encoding.TextMarshaler` and `encoding.TextUnmarshaler` |
| `StringCodec(format, parse)` | two functions over `string` | nothing |
| `ValueCodec(kind, enc, dec)` | a kind and two functions over `Value` | nothing |

`TextCodec` takes **no function arguments at all**: both halves come from the type.
That is the distinction the two names hid, and it is why `TypeCodec` is renamed `ValueCodec`: the trio then reads String, Value, Text, named after what the two halves speak.

**When each is the only one that works**, run rather than asserted.

**`TextCodec`, when the type declares an inverse and only the KIND is wrong.**
This is its whole purpose, and it is narrower than the first draft claimed.
ADR-0007's chain already claims any type with a text pair, correctly, at kind `String`.
So `TextCodec` is not for rescuing such a type, it is for **changing its kind**:

```
big.Int unregistered, ADR-0007 step 2 gives  ->  string("1099511627776")
TextCodec[big.Int](Number)                   ->  number("1099511627776")
and it now loads from a YAML plane saying Number
```

Eleven lines of `ValueCodec` replaced by one, because the type already declares the inverse.

**`StringCodec`, when the type declares no inverse.**
`url.URL` has no text pair, so the wrong constructor is a build error rather than a runtime surprise:

```
TextCodec[url.URL](String)
  url.URL does not satisfy interface{*url.URL; UnmarshalText([]byte) error}
  (missing method UnmarshalText)
```

**`ValueCodec`, when the codec must accept a kind it never emits.**
This is ADR-0006's escape hatch, and `StringCodec` cannot express it because its decode half only ever sees a string:

```
StringCodec, plane holds `poll:` (a YAML null)  ->  err: value: wrong kind
ValueCodec , plane holds `poll:` (a YAML null)  ->  0s
```

**Inference works at every call site that has a value argument, and it is not marginal.**
Ten registrations were written the way a user would write them and compiled; not one carries an explicit type argument.
Five of them are one line, because `T` is inferred from a **method expression** on one side and a package parse function on the other:

```go
StringCodec(netip.Addr.String, netip.ParseAddr)
StringCodec(netip.AddrPort.String, netip.ParseAddrPort)
StringCodec(netip.Prefix.String, netip.ParsePrefix)
StringCodec(language.Tag.String, language.Parse)
StringCodec(uuid.UUID.String, uuid.Parse)
```

**Do not copy those five.**
Three of them are wrong, and [The three defects](#the-three-defects-found-by-running-the-decisions) is why.
They are shown here because the ergonomic result is real and separable from the correctness result: the API infers, and inference is not what makes a codec right.

The remaining five cost between six and eighteen lines, and the shape is the same in each: `url.URL` needs a wrapper on both halves, a named `time.Duration` needs two literals, `big.Int` and `decimal.Decimal` need `ValueCodec` because their text is a number, and `net.Addr` needs `ValueCodec` because an interface codec owns a discriminator inside its own text.

**A half pair is a build error, and the diagnostic documents the API.**
Verified on `go1.27rc2`:

```
StringCodec(netip.Addr.String, netip.ParsePrefix)
  in call to StringCodec, type func(s string) (netip.Prefix, error) of netip.ParsePrefix
  does not match inferred type func(string) (netip.Addr, error) for func(string) (T, error)

StringCodec(netip.ParseAddr, netip.Addr.String)      // halves swapped
  in call to StringCodec, type func(s string) (netip.Addr, error) of netip.ParseAddr
  does not match inferred type func(string) string for func(T) string

StringCodec(netip.Addr.String)                       // one half only
  not enough arguments in call to StringCodec
    have (func(netip.Addr) string)
    want (func(T) string, func(string) (T, error))
```

ADR-0007 measured zero half pairs across three corpora and made an incomplete **interface** pair a schema compile error.
A half **registration** does not need that treatment, because it does not compile.

**One ergonomic limit was found and no API shape removes it.**
A method expression requires a **value** receiver, so `url.URL.String` is `invalid method expression url.URL.String (needs pointer receiver (*url.URL).String)` and `big.Int.String` is the same.
That is a property of the standard library's receivers, and it is the difference between a one-line registration and a seven-line one.

`PT` is inferred from `T` by constraint type inference, verified by compiling, so `TextCodec` names one type argument and not two: `TextCodec[netip.Addr](String)`.

**The naming is a readability decision and the ADR marks it as one.**
`TypeCodec` was the name #12's prototype used, and the only argument for changing it is the one above: the trio has to be distinguishable by name, and String and Text are near-synonyms in English while meaning "the boundary kind" and "`encoding.TextMarshaler`" here.
No measurement decides it, and it is the one spelling in this ADR taken on how it reads.

### The declared kind is a donation target, and the accepted set is the codec's

ADR-0007 fixed that a codec declares the boundary `Value` kind it **produces** and that core donates `String` to that kind before calling it.
It did not fix what a codec **accepts**, and ADR-0006 landed a capability that turns on exactly that difference.

> The declared kind is the donation target and nothing else.
> It does not constrain the kinds a codec accepts, and it does not constrain the kinds a codec emits.

**Why the accepted set must be wider.**
ADR-0006 refuses `Null` at every leaf whose Go kind has no null, and its argument for choosing refusal over zeroing is recoverability, stated at line 194:

> A registered codec for its own type accepts `Null` and returns 0.
> That is ADR-0005's stated escape hatch used for what it is for.

That is a claim about this ADR's API, made before this API existed.
Run:

```
plain int, plane says null                 -> err: value: wrong kind
registered R2Count, plane says null        -> 0
registered R2Count, plane says number("7") -> 7
registered R2Count, plane says string("7") -> 7
```

The codec declares `Number`, because that is what it produces, and separately accepts `Null`, which it never produces.
ADR-0006's argument holds, and it holds only because ADR-0007 lets `Null` reach the codec rather than intercepting it.
Had this ADR derived the accepted set from the declared kind, ADR-0006's choice between refusing and zeroing would have been forced the other way, and the two ADRs would have been quietly inconsistent.

**And this is the measured reason there are three constructors and not one.**

```
StringCodec R2Count, plane says null       -> err: value: wrong kind
```

`StringCodec`'s decode half calls `Value.AsString`, which refuses `Null`.
So the two-argument helper **cannot express ADR-0006's escape hatch**, and the general form whose decode half sees the whole `Value` has to stay.
An API with only the ergonomic constructor would have closed a door another ADR is holding open.

**Why the emitted set must be wider too.**
ADR-0005's `net.Addr` codec returns `Null` for a nil interface, which is the mechanism that makes an interface expressible at all, and ADR-0007 requires that whatever a codec emits it accepts.
So a codec declaring `String` and emitting `Null` is not a special case in an encode check, it is the rule: the declaration targets donation, and `Null` is emittable by any codec.

**What core checks, and what it cannot.**
One comparison per encode:

```
declares Number, emits String -> ferry: codec for main.R2Liar declared kind number but produced string
```

That catches a codec that lies about its declared kind.
It cannot catch a codec that declares the right kind and the wrong text, which is what ADR-0005's golden column exists for and what the proof section below is about.

### What a registration may not be

ADR-0007 named two refusals.
Measured, the table needs three, and the third is the one that matters.

| | diagnostic |
| --- | --- |
| a type core owns | `time.Duration is in core's own set and its representation is pinned; define a named type over it and register that` |
| a duplicate | `netip.Addr is already registered` |
| **a pointer type** | `*big.Int: pointer indirection is structural and a pointer type never reaches the table; register big.Int instead` |
| a named type over one core owns | **accepted**, and it must be: it is ADR-0005's documented escape |

**The pointer refusal is not tidiness, and the prototype proved it by removing it.**
ADR-0007's first defect was that `*big.Int` satisfies the whole text pair in its own right, so a `*big.Int` field became a leaf, ADR-0005's nil-pointer rule never ran, a nil dumped as `string("<nil>")` and the load segfaulted.
ADR-0007 fixed that for the **chain**, by resolving pointer indirection before the chain is asked anything.
With the check removed from the table, measured:

```
with the check removed, a nil *big.Int dumps string("<nil>")
```

The same hole, reopened through the other door.
So the rule ADR-0007 states for the chain binds the table identically, and it is enforced at the registration call site rather than in the walk, which is earlier and names the type to register instead.

**Registering a type the chain would claim is legal, and it wins.**
Measured, with the text arm on:

```
slog.Level unregistered, chain claims it -> string("WARN")
slog.Level registered, table claims it   -> number("4")
```

ADR-0007's chain is identity, then the text pair, then kind, so registration is step one and beats a text pair the type already has.
This is not a loophole: it is the mechanism by which a user overrides a representation a **dependency** chose, which is precisely the drift exposure ADR-0007 records against before-kind ordering.
Registration is the answer to that exposure, and it is worth saying so, because ADR-0007 named the exposure and left the remedy to this ticket.

**Registering an INTERFACE claims the interface, and nothing else.**
Measured, with a `net.Addr` codec registered:

```
struct{ A net.Addr }     -> addrs=[/A]
struct{ A *net.TCPAddr } -> addrs=[/A/IP /A/Port /A/Zone]
```

Identity is `==`, so this is correct and it is not obvious.
A user who registers the interface and then changes the field to the concrete type silently gets a different representation and a different address set, from an edit that is not a serialization change in any reviewer's reading.
That is the same shape as ADR-0007's after-kind drift, triggered from the user's own struct rather than from a dependency, and it belongs in the documentation of registration rather than being discovered against a stored artefact.

**A registration may lift one of ADR-0005's four permanent refusals, and the table does not stop it.**
Measured, a `chan int` codec:

```
register a chan int codec -> nil
compile=[/Ch]  dump=map[/Ch:string("ch")]  load err=nil
back.Ch == orig -> false
```

The table takes it, because the table is keyed by `reflect.Type` and a `chan` is a perfectly good key.
**ferry does not add a check for this**, and the reason is that ADR-0005's "the value does not exist outside the process" is a statement about the **proof** rather than about the mechanism.
The codec runs; the round trip yields a different channel; and no relation the registrant can write makes that true, because a relation that returns true for two distinct channels says every channel is every other channel.
ADR-0001 already permits registering without proving and says it forfeits the guarantee.
A kind check here would refuse a registrant who has knowingly forfeited it, which is not core's call.

### `Register` runs the codec against the zero value

> `Register` encodes the zero value of `T`, donates `String` to the declared kind, decodes it back, and refuses the registration if either half errors.

This is the answer to the first defect below, and it replaces the answer an earlier draft of this ADR gave, which was a doc comment and a preferred constructor.
That was too weak for the worst defect the prototype found, and the reason it looked like the only option was an assumption that never got checked: that core cannot say anything about a codec without values from the registrant.

**Core has one value it does not need to be given.**
`Register` holds `T`, so it can build `reflect.New(t).Elem()` itself.
Run against the registrations of the section above:

```
StringCodec(netip.Addr.String, netip.ParseAddr)   REFUSED
    ferry: netip.Addr: the codec is not total over the zero value: it encodes to
    string("invalid IP") and decoding that back fails: ParseAddr("invalid IP"):
    unable to parse IP
StringCodec(netip.AddrPort.String, ...)           REFUSED
StringCodec(netip.Prefix.String, ...)             REFUSED
TextCodec[netip.Addr](String)                     ok
StringCodec(url.URL...)                           ok
DurationLike[R16Timeout]()                        ok
ValueCodec(Number, big.Int...)                    ok
ValueCodec(String, net.Addr...)  an interface     ok
```

All three defects, caught at the call site, with no proof written and no test values supplied.
The interface codec passes, which is the case that had to keep working: its zero is a nil interface, it emits `Null`, and it accepts `Null` back.

**The check has to donate, or it tests a path the walk never takes.**
ADR-0007 makes core donate `String` to the declared kind before calling any codec, so the check does the same.
A `Number`-declaring codec whose decode half calls `AsNumber` would otherwise fail a check that the real walk passes.

**Exactly how much of the obligation it discharges, measured against four wrong codecs.**

| the codec | the zero check |
| --- | --- |
| errors at the zero value | **refused** |
| lossy, but total at the zero value | passes |
| a constant: total, and wrong everywhere | passes |
| right value, wrong declared kind | passes |

One of four, and it is the only one of the four that is unarguably a bug rather than a judgement.
The other three are what ADR-0005's triple is for, and this check does not pretend to replace it.
The ADR states the ratio rather than the headline, because "core checks your codec at registration" would read as more than it is.

**And it would have caught the two wrapper defects.**
Those were found by an audit fixture that happened to dump a registered interface at its zero value, three prototypes in.
With this check in `Register`, the first interface registration anyone writes runs exactly that path, at startup, in every consumer's process.

**Cost: 162 ns and 6 allocations, once per registration.**
A registry freezes at its first use, so it can never run more than once per registration.
Nothing changes in the signature, because `Register` already returns an error.
What changes is that ADR-0007's "total over its type including the zero value" stops being prose a registrant is asked to honour and becomes a check.

**One thing it forecloses, stated because it is a real refusal.**
A codec whose type has no meaningful zero, and which errors on it deliberately, can no longer be registered.
ADR-0007 already requires totality over the zero value, so this ADR is enforcing a rule rather than adding one, and the escape is to make the codec total: a zero that means nothing still has to encode to something the decode half accepts.

### Registration is static, and the entry point is why

> There is no registration by runtime `reflect.Type`.

json/v2 cannot do it either, and that was re-checked rather than inherited from the ticket body.
[go.dev/issue/73457](https://go.dev/issue/73457), "proposal: encoding/json/v2: MarshalFunc with reflect.Type", against the GitHub API on 2026-08-02: **open**, labels `Proposal` and `LibraryProposal`, last touched 2025-08-07.
So there is no upstream answer to copy, and ferry decides on its own terms.

**What only the dynamic form can express**, measured: a type reached at runtime and never named in source.
`reflect.StructOf` is the only way to make one in pure Go, and a dynamic registration for it works.

**What it costs**, measured: everything the typed form makes unrepresentable becomes a runtime question again.

```
wrong type inside enc -> PANIC: interface conversion: interface {} is netip.Addr, not time.Time
wrong Set on dst      -> PANIC: reflect: call of reflect.Value.SetString on int64 Value
```

Both are panics **inside ferry**, on third-party code, at Dump.
The typed form makes both a build error naming `T`.

**The deciding argument is not the cost, it is that the method would compile and nobody could call it.**
This is what "contingent" means here, and it is worth spelling out rather than leaving as a word.
The method, if it shipped:

```go
func (r *ferry.Registry) RegisterType(
    t reflect.Type, kind ferry.VKind,
    enc func(reflect.Value) (ferry.Value, error),
    dec func(ferry.Value, reflect.Value) error,
) error
```

It is reachable only if a caller can **ask** ferry about a type they cannot name, and that is a property of [#16](https://github.com/onhotpath/ferry/issues/16)'s entry point rather than of this ADR:

| entry point | is `RegisterType` reachable |
| --- | --- |
| `ferry.Load[T](ctx, src)` | **no.** `T` is written in source, so a `reflect.StructOf` type can never be `T`, and the method is dead code |
| `ferry.Load(ctx, v any, src)` | **yes.** `v` may hold a value of a runtime-built type |

The second row is xload's own shape, at [load.go:37](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L37), with two runtime kind checks behind it.
The research recommends the first, and ADR-0001 calls `Load` and `Dump` a working assumption rather than a decision.
So this is live rather than academic, and this ADR does not get to settle it.

**The recommendation is therefore to refuse for now, on reversibility**, which is ADR-0006's own rule stated in its own words: "a refusal is liftable later with no break and a permission is not retractable".
Adding `RegisterType` after #16 ships a non-generic entry point is additive.
Shipping it now and finding nobody can call it is a method ferry maintains forever, which is the "interface with nothing behind it" ADR-0001 rules out by name.

**And the case people actually reach for it with has a typed spelling.**
The pitch is "I have N named types over one underlying type", which is ADR-0005's named-duration hole generalised.
Core ships the one-liner instead:

```go
func DurationLike[T ~int64]() Reg
```

Measured over two named duration types:

```
/M string("1.5s")   /S string("1m30s")   load -> 1.5s 1m30s
```

**This is the answer to ADR-0005's documented sharp edge**, which that ADR left open as "a user who defines a named duration type registers a codec for it, and this is a documented sharp edge rather than a defect ferry can reflect its way out of".
It is one line per type, with no `reflect.Type` in anyone's hand.
It needs an explicit type argument, because there is no value argument to infer from, and that is the one place in this API where inference does not work.

**A predicate arm is the other generalisation, and it is refused.**
Instead of naming each type, register a predicate over `reflect.Type` and claim whatever matches.
It was built and run rather than dismissed, because it is the one extension to ADR-0007's chain this ticket could plausibly have argued for.

The predicate that rescues a named duration eats every named `int64` in the program:

```
R5Timeout -> string("30s")
R5Port    -> string("30s")     <- a port number, 30000000000
R5Retries -> string("30s")
```

ADR-0005 already ruled this out for core, saying that "closing it would require matching on the underlying type, which would then also capture every ordinary `type Port int`".
A predicate arm is that rule handed to the user with the defect intact.

And two predicates matching one type is precedence by **list order**:

```
order 1: underlying int64, treated as a duration -> string("30s")
order 2: underlying int64, treated as a number   -> number("30000000000")
```

Registration order across packages is `init()` order, which is a property of the import graph rather than of anyone's intent.
ADR-0007's chain is a map keyed by `reflect.Type` at step one precisely so that there is no order to get wrong, and a predicate arm reintroduces it.
The lookup cost is **not** the argument and the ADR does not lean on it: 13 ns for a map hit against 20 to 43 ns for a scan of 1 to 16 predicates, and [#16](https://github.com/onhotpath/ferry/issues/16) resolves the claim once per type anyway.

### The table is a value, and it freezes at its first use

This is the largest question the ticket owns.

> A registration goes into a `Registry`, which is a value.
> **A registry freezes at its first use**, which is the first schema compiled against it, and a registration after that is a loud error.
> Core ships a default registry, and the package-level `Register` writes to it.

Three candidates were built and run against each other.
The fixtures all register **after** a schema has been compiled, because a fixture that registers first cannot see the question at all, and that is the shape of mistake the prior sessions each made once.

#### What the question looks like to a consumer

The lifetime is invisible in the good case and decides the API's shape in every other, so the two spellings a consumer could meet are worth writing out before the measurements:

```go
// (A) implicit registry
func init() { ferry.Register(...) }
var cfg AppConfig
err := ferry.Load(ctx, &cfg, yaml.Source{Path: "app.yaml"})

// (B) explicit registry
reg := ferry.NewRegistry()
reg.Register(...)
cfg, err := ferry.Load[AppConfig](ctx, src, ferry.WithRegistry(reg))
```

This ADR needs **both** to be available, which is why core ships a default registry.
Neither spelling is this ticket's to choose: the entry point is [#16](https://github.com/onhotpath/ferry/issues/16)'s, and [ADR-0008](0008-the-struct-tag-grammar.md) has already put its own tag-key Option into the same cache key.

**Here is the concrete thing #16 cannot get right without this ADR.**
All eight type caches in the standard library are a `sync.Map` keyed by `reflect.Type`, and ADR-0008 measured that shape at 18 ns.
It is the obvious thing for #16 to write, and it is wrong the moment two registries exist in one process:

```
var schemaCache sync.Map // map[reflect.Type]*schema

service A wants big.Int as text     ->  string("1099511627776")
service B wants big.Int as a number ->  string("1099511627776")   (cache hit)
```

B silently gets A's representation, and writes a quoted string into a YAML file where its own codec declared `Number`.
No error.
That is ADR-0004's `EnvSource{Sep}` defect one layer up, and it is not visible from #16's ticket at all.

**So the split this ADR proposes, stated as a split rather than left implied:**

| owner | decision |
| --- | --- |
| **#19**, here | a registration goes into a `Registry` value; a `Registry` freezes at its first use; core ships a default one |
| **#16** | the entry point's signature, whether (A) or (B) or both are spelled, where the cache lives, and what else is in its key |
| **the seam** | once a type has been resolved against a registry, that registry's answer for that type must never change; and the cache key must distinguish two registries |

ADR-0008 already wrote half of this handoff, in #16's own words: "whether the codec registry belongs there depends on [#19](https://github.com/onhotpath/ferry/issues/19) making it process-wide or per-instance".
This ADR's answer is per-instance, with a default instance, and frozen.
So yes, it belongs in the key.

#### What freezes: the registry, not each type

Freezing each type as it is resolved is strictly more permissive, and it is **sound for the cache**: a schema for `A` resolved only the types `A` reaches, so registering a type `A` never mentioned cannot make `A`'s schema stale.
It admits a case whole-registry freeze refuses:

```go
ferry.Load[A](ctx, src)              // A mentions no netip.Addr
ferry.Register(codecForNetipAddr)    // a lazily-imported plugin
ferry.Load[B](ctx, src)              // B does mention it
```

It is refused anyway, and the first reason is this ADR's own argument turned on itself.

**Whether a registration succeeds would depend on which schemas were compiled first.**
Measured, the same two operations in two orders:

```
Load[B] (mentions netip.Addr) first  ->  Register(netip.Addr) REFUSED
Load[A] (does not) first             ->  Register(netip.Addr) accepted
```

That is import-graph order deciding an outcome, which is the exact ground on which this ADR refuses a predicate arm and refuses the global-frozen model.
Whole-registry freeze is not that: "after any Load, no registrations" is decided by program order alone.
A principle applied twice and abandoned on the third case was not a principle.

**It also puts a growing mutable set on the lookup path**, because marking a type resolved is a write, so the read path takes a lock.
Measured at 16 ns/op for a frozen registry's plain map read against 57 ns/op for a mutex plus map write.
That is **not** the argument, and the ADR says so: #16 resolves per type per registry rather than per leaf, so this is off any hot path.

**And the diagnostic gets worse.**
"The registry is frozen; every registration must happen before the first schema is compiled" tells a user what to do.
"`netip.Addr`: already resolved by an earlier schema compile" makes them work out which compile, about a type they may not have written.

#### The default registry, and the init-order question it has to answer

The global-frozen model is refused above partly because "the freeze point is decided by `init()` order".
This ADR then ships a default registry that freezes at its first use, which is the same shape, so it has to answer the same objection or it is refusing a model and adopting it on the default path.

**The Go spec answers it, and the answer is structural rather than a convention.**
Imported packages are initialised before the importer, and every package-level variable and every `init` in the whole program runs to completion **before `main.main` is called**.
So for the shape every consumer writes:

```go
func init() { ferry.Register(...) }   // any package, any order
func main() { ferry.Load(...) }
```

every registration in the program strictly precedes the first Load, whatever the import graph is.
The freeze point is not order-dependent because there is only one edge and the language guarantees it.
Verified by running exactly that layout, with two package `init`s writing to one default registry:

```
at main(), the default registry holds 2 registrations from 2 init()s, frozen=false
first Load succeeds, and now frozen=true
a registration after that -> ferry: netip.Prefix: the registry is frozen; every
                             registration must happen before the first schema is compiled
```

**The one shape it does break, stated rather than hidden**: a `Load` **during** `init`.
Then whether a later package's `init` can still register does depend on the import graph, and it is the global-frozen objection in full.
Two things make it affordable here where it was not affordable there.
It is loud, at startup, with the freeze point named, rather than a silently stale schema.
And the escape is one line, `ferry.NewRegistry()`, which the global model by definition does not have.
That is the whole difference between a default registry and a global one, and it is why the objection lands on one and not the other.

#### Global and mutable is unsound, three ways

Measured, with a schema cache keyed by `reflect.Type`, which is the shape all eight stdlib type caches use.

**A cached refusal stays a refusal.**

```
compile #1, nothing registered    -> ferry: /A: netip.Addr maps no address ...
compile #2, netip.Addr REGISTERED -> the same error, from the cache
the same compile, uncached        -> addrs=[/A], err=nil
```

Infuriating, and not dangerous, because it is loud.

**A cached codec is silent.**
The research's recommendation and ADR-0007 both put the resolved codec **in** the compiled schema, and that is the version that goes wrong:

```
codec baked into the schema at compile #1 -> string("192.0.2.1")
codec the registration asked for          -> bytes("\x00...\xc0\x00\x02\x01")
```

No error, no diagnostic, and the plane now holds the representation the user replaced.

**And the cached address set is the one that matters.**

```
compile #1, R6Backend unregistered -> addrs=[/B/Host /B/Port]
compile #2, R6Backend REGISTERED   -> addrs=[/B/Host /B/Port]   (cache hit)
the same compile, uncached         -> addrs=[/B]
what dump actually writes          -> [/B]
```

ADR-0004 hands the **static address set** to `Bind` before any I/O.
So ADR-0003's prefix-free check ran over a set that does not exist, the driver's key function was checked for injectivity over the wrong set, and `/B` is not in the table it precomputed, which makes a legitimate write a driver error.
A global mutable table makes every one of those a function of when the first Load happened.

#### Scoped, and what the schema cache must be keyed by

Four candidate keys, all measured.

| key | result |
| --- | --- |
| `reflect.Type` alone | two registries that disagree about `net.IP` share an entry, and the second gets the first's codec |
| the registry **value** | `PANIC: runtime error: hash of unhashable type` |
| the registered **type set** | two registries with the same types and different codecs have the same signature and collide |
| the registry **pointer** | works |

The second row is ADR-0004's own finding arriving one layer up.
That ADR measured a driver value panicking as a map key because it holds a func field, and concluded that "a contract whose correctness depends on a driver author supplying the right identity is a prose rule with a runtime panic behind it".
A registry is nothing but func fields and maps, so it is the worst case of that.

The third row is why no content hash can work: a key can only see what is comparable, and the codec is both the part that differs and the part Go cannot compare.

**The pointer key works, and it is sound only if the registry is frozen.**

```
compile with an empty registry  -> string("192.0.2.1")
register into it, compile again -> string("192.0.2.1")   (cache hit)
```

Same pointer, different contents, same entry.
The staleness of the global model returns in full.
So the pointer key does not remove the freeze, it makes the freeze **per registry** instead of per process, and that is the whole difference between the scoped model and the frozen-global one.

**A per-call registry is ruled out by the same measurement that admits a long-lived one.**

```
10000 per-call registries -> 10000 cache entries, none evictable
```

The research surveyed eight stdlib type caches and found no eviction anywhere, and states why: "the cache is bounded by the set of types the program statically declares, so it converges after warmup".
Keying by a per-call value destroys exactly that property.
A registry has to be long-lived, and that is a real constraint on how [#16](https://github.com/onhotpath/ferry/issues/16) spells the entry point rather than a free choice.

#### Global-and-frozen was costed rather than assumed

It removes the staleness entirely, and it costs two things.

**Two tests cannot want different codecs for one type.**
Under a global table the second `Register` returns "already registered", or "registration is closed" if any earlier test compiled a schema, depending on test order.
Measured on this prototype's own probes: R6, R7 and R11 each need a **different** codec for `net.IP` or for one key type, in one process.
Under a global table this prototype could not have been written, and neither can a registrant's own test suite for a codec they are choosing between two representations for.

**The freeze point is decided by `init()` order.**
The first compile is inside whichever Load or Dump ran first, which across packages is the import graph's.
That is the same property this ADR refused a predicate arm for two sections above, in a different costume: correct today, broken by an import a dependency adds.

**Its one real advantage is zero configuration**, and that is answerable without taking the model.
Core ships a **default registry**, package-level `Register` writes to it, and it freezes on first use exactly like any other.
So `ferry.Register(...)` in an `init()` followed by `ferry.Load[Config](ctx, src)` is available, and the scoped form is the escape hatch a test reaches for rather than the only way in.

#### And freezing is what keeps the registry out of #20's problem

A mutable registry read by a compile is a data race, whether or not any ADR mentions goroutines.
Reported by the race detector:

```
Write at 0x... by goroutine 9:   runtime.mapassign -> (*Registry).Register
Previous read by goroutine 10:   runtime.mapaccess2 -> identityLookup -> classify -> compile
```

No mutex inside ferry fixes it, because the read is the whole point of a lock-free cache hit.
A frozen registry is written before the first reader exists and never again, so the read path is a plain map lookup with no lock and no atomic.
ADR-0004 already relies on that shape and priced it: 8.8 ns/op for the write-once static table against 20.0 ns/op with a single mutex.

Concurrency is [#20](https://github.com/onhotpath/ferry/issues/20)'s and nothing here decides it.
What this ADR hands #20 is a registry that is immutable for the whole life of every schema compiled against it, which is one fewer shared mutable thing than a global table would have handed it.

### There is no decline, and json/v2's mechanism does not port

> A registration claims its type unconditionally.
> There is no way for a codec to say "not mine" at runtime.

**The mechanism was re-measured first, because both the ticket body and the research doc are wrong about how json/v2 spells it.**
Both say v2 has a `SkipFunc`.
Verified on `go1.27rc2`: `grep -rn SkipFunc $GOROOT/src/encoding/json/` returns **nothing**, and the mechanism survives as an unexported `maySkip bool` at [arshal_funcs.go:95](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/v2/arshal_funcs.go).
`SkipFunc` exists in the `github.com/go-json-experiment/json` mirror only.

The capability was respelled rather than removed, and it was run rather than read.
`JoinMarshalers`' godoc: "If a function returns [errors.ErrUnsupported], then the next applicable function is called, otherwise the default marshaling behavior is used."
Measured, in a module importing `encoding/json/v2` on `go1.27rc2`:

```
MarshalToFunc  {A:1}  -> "first:1"    err=nil
MarshalToFunc  {A:-1} -> "second:-1"  err=nil       declined, fell through
MarshalFunc    {A:1}  -> "buf:1"      err=nil
MarshalFunc    {A:-1} ->              err=json: cannot marshal from Go main.T: marshal
                                      function of type func(T) ([]byte, error) may not
                                      return errors.ErrUnsupported
all functions decline -> {"A":-1}     err=nil       default behaviour
```

So v2 permits declining exactly where declining is observably free, on the streaming shape where nothing has been written, and forbids it on the shape that has already produced a buffer.
ferry's codec produces a `Value` rather than writing to a stream, so "nothing has been written yet" is trivially true and **v2's own constraint does not bind ferry**.
The question is therefore genuinely open on ferry's terms, and it was asked on ferry's terms.

**A value-dependent claim makes the address set value-dependent.**
Measured, one type, one compiled schema, a codec that declines when a field is zero:

```
compile, codec claims the type   -> [/B]
compile, codec declines the type -> [/B/Host /B/Port]

{Host:h Port:80} claimed  -> mints [/B] = string("h:80")
{Host:h Port:0}  declined -> mints [/B/Host /B/Port]
```

Every ADR since ADR-0003 leans on the address set being computable from `reflect.TypeFor[T]()` alone with no value in hand.
ADR-0004 hands the static set to `Bind` before any I/O, so the driver has precomputed a key table that is right for one of those values and wrong for the other, and ADR-0003's prefix-free check ran over a set the walk is about to leave.

**And the Load direction has no answer at all.**
The two rows above are two different **plane layouts**, not two code paths:

```
plane holds /B=string("h:80")           , codec claims -> {Host:h Port:80}
plane holds /B/Host and /B/Port         , kind admits  -> {Host:h Port:80}
```

A codec declining on Load would have to ask the plane for addresses the source was never bound to, which ADR-0004's contract does not allow and which needs an `Enumerator` the plane may not implement.
v2 can fall through on decode because it holds the whole document; ferry holds one `Value` at one address.
That is the structural difference, and it is the reason the mechanism does not port rather than the reason it is unwanted.

**The decline that would be sound is a decline about the type**, which is the predicate arm, refused above for its own reasons.
So the two sound-looking spellings of "not mine" are one spelling, and it is already decided.

**What ferry gives up, priced.**
v2's decline exists because `WithMarshalers` is a **list**, so a caller composes several and needs a way to pass.
ferry's step one is a map, and ADR-0007 already made a duplicate a loud error, so there is never a second entry to fall through to.
The only thing decline could reach is steps two and three of ADR-0007's chain, and both are reachable by not registering the type:

```
net.IP unregistered, chain step 2 claims it -> string("192.0.2.1")
net.IP with no chain, step 3 claims it      -> bytes("\x00...\xc0\x00\x02\x01")
```

"Fall through to the next step" is spelled "do not register this type", and it is a decision made once at a call site rather than per value at Dump.

### A key codec says so, and that is where injectivity is communicated

ADR-0005 admits a registered codec as a map key and puts injectivity on the registrant as a proof obligation.
It did not say how a registrant finds out they have taken it on, and ADR-0007 then found the same hazard inside **core's** own set and filed [#31](https://github.com/onhotpath/ferry/issues/31).

> A registration is usable as a map key only if it says so: `StringCodec(...).AsMapKey()`.
> A `map[T]V` whose key type is registered without it is a schema compile error.

**The implied rule was run first.**
Measured, a registered codec whose text drops a field:

```
Go map holds 2 keys -> ferry dumps 1 address
/M/api  number("2")
```

One entry silently dropped, no error, and which one survives is map iteration order.
That is #31's defect occurring in user code, and ADR-0001 rules out silently ignoring anything.

**A leaf codec and a key codec are promising different things, and the difference is measurable.**
The same lossy codec at a leaf is fine in the sense that matters: it fails a round-trip proof, loudly, at one address, with the value visible.

```
leaf: {API 80} -> string("api") -> {api 0}
```

As a key it fails by making a **sibling entry cease to exist**, which no proof over the key type alone can see, because the collision is between two values rather than inside one.

**Opt-in makes it a schema compile error, and the diagnostic is the mechanism.**
What a consumer writes and what they get:

```go
func init() {
    ferry.Register(ferry.TextCodec[netip.Addr](ferry.String))
}

type Config struct {
    Name   string             `ferry:"name"`
    Limits map[netip.Addr]int `ferry:"limits"`
}
```

```
ferry: /limits: netip.Addr has a registered codec but is not declared usable as a
       map key; a key codec's text must be injective over the key type, or two keys
       collapse into one address; add .AsMapKey() to the registration if it is
```

The fix is one method call, `ferry.TextCodec[netip.Addr](ferry.String).AsMapKey()`, after which the schema compiles to `[/limits/*]`.

The refusal is at schema compile from `reflect.TypeFor[T]()` alone, which is the same assertability every other refusal in this design has.
And the diagnostic is where the obligation gets communicated, which is the point: it is the only moment a registrant is guaranteed to read.

**The cost, stated exactly.**
A user who registers `netip.Addr` and then writes `map[netip.Addr]int` gets a compile error for a codec that **is** injective.
That is a false refusal.
It is affordable because the fix is one method call named after the obligation, and because the error arrives at schema compile so the user never ships it.
The implied rule's failure is a dropped map entry at Dump, on a plane already being written, discovered by a diff or never.

**Implying it from the declared kind was considered and excludes nothing.**
That is what ADR-0007's prototype does, and the non-injective codec above declares `String`.
The kind says what the text looks like; injectivity is about what the text forgets; no kind expresses that.

**A third mechanism exists, and it is additive rather than an alternative.**
ADR-0005 says the obligation is "discharged over their supplied value list in the same harness", and never says what that check is.
It is three lines, and it runs:

```go
func Injective[T any](format func(T) string, values ...T) error
```

> **Amended under [#31](https://github.com/onhotpath/ferry/issues/31): the signature is wrong in two ways, both measured.**
> `T` must be `comparable`, because injectivity is over Go's `==` and an unconstrained `T` compiles for a type Go cannot key a map with.
> And `format` is not what ferry calls: what addresses the plane is the key-text lookup, so a registrant proving their own function injective has proved nothing about what ferry writes.
> Measured, one type through both routes: the registrant's `String()` gives `"api:80"` and `"api:443"`, and ferry writes `"api"` and `"api"`.
> The check takes the registry and asks ferry, which is this ADR's own one-lookup-not-two rule applied to the key position.
> The spelling is [#35](https://github.com/onhotpath/ferry/issues/35)'s.

```
Injective(netip.Addr.String, 10.0.0.1, 10.0.0.2, 2001:db8::1)  ->  nil
Injective(R17Host.Name, {api,80}, {api,443})
  ->  ferry: key codec for R17Host is not injective: values[0] and values[1]
      both encode to "api"
```

The two mechanisms answer different questions, and neither subsumes the other.
`AsMapKey` asks "did you think about this", once, at the call site, of everyone.
`Injective` asks "is it true of the values you care about", in a test, of whoever writes one.
**Shipping only the harness** leaves the registrant who writes no proof with the silently dropped entry above, which is the failure ADR-0001 rules out by architecture.
**Shipping only the keyword** leaves a registrant who has said the word with no way to check that the word was true.
This ADR proposes both, and if only one can ship it should be the keyword, because the failure it prevents is the silent one.

**And this does not fix [#31](https://github.com/onhotpath/ferry/issues/31).**
Measured, unchanged:

```
a == b: false   a.Equal(b): true
map[time.Time]int, 2 Go keys -> 1 ferry address
```

`time.Time` is core's pre-seeded entry, not a registration, so the opt-in rule cannot reach it.
Fixing that amends ADR-0005's admissible key set, which is #31's.

**And there was a third case, which [#45](https://github.com/onhotpath/ferry/issues/45) found and [ADR-0007](0007-the-codec-chain-and-its-precedence.md) closed.**
Nothing in this ADR changes: its rule is scoped to "a registration" and is correct as written.
The gap was that ADR-0007 admitted a **chain-claimed** `String` type as a map key "on the same terms as a registered codec", and the terms were not the same, because a chain arm has no call site at which to say `.AsMapKey()`.
So the opt-in was defeatable by **not registering**, which made this the only place in ferry where registering a type left it less usable than not registering it.
ADR-0007 reversed its own sentence: keying a map is registration-only.
This cross-reference exists because neither ADR was wrong on its own and the composition was, which is the failure mode a reader of one document cannot see.
This is said explicitly because the two look like one bug and the opt-in rule would otherwise read as having fixed both.

### The proof is enabled, and can never be required

ADR-0001's sentence is taken literally rather than strengthened.

**The harness needs no accessor on a registration**, which is the finding that keeps `Reg` opaque:

```go
func RoundTrip(t *testing.T, r *ferry.Registry, p Plane, proofs ...Proof)
```

A proof exercises the codec through the ordinary walk, so `ferrytest` never opens a `Reg`.
That matters because `ferrytest` is a separate package from `ferry`, so anything it needed from a `Reg` would be exported surface forever.
It needs nothing.

**What ADR-0005's triple catches over a registration**, run:

| what is wrong | caught by |
| --- | --- |
| lossy text | the property, on 3 of 4 cases |
| right value, wrong representation: a `Number` codec where the proof says `String` | **only the golden column**, on every case |
| not total over the zero value | the property, on the first case |

The middle row is ADR-0005's own argument reproduced against a registration: the value round-trips perfectly, and only column three sees it.
It is also the difference between a codec that works on YAML and one that works on env, which ADR-0007 calls the most consequential thing #19 inherits.

**Three measured reasons registration cannot require a proof, in order of weight.**

1. **It puts a testing import in `main`.**
   A proof carries values, so requiring it at registration puts test fixtures in production code and `ferrytest` in core's import graph, which is the thing ADR-0002 put the harness in a separate package to avoid.
2. **It does not close the hole.**
   ADR-0005 measured a knowingly lossy float codec caught by 1 of 4 values.
   Reproduced here: the lossy `netip.Addr` codec that fails a four-case proof on three cases **passes a one-case proof**, and nothing can check a value list.
3. **It fires in the wrong place.**
   `Register` returns an error, so a failing proof would be a startup failure rather than a CI failure.
   ADR-0001 makes the harness route (b) authority, and authority that fires in production is an outage.

**What core does instead, and it is not nothing.**
The declared-kind check on every encode, a build error for a half pair, and a harness that takes a `*Registry` so discharging the obligation is four lines.

**And one thing that falls out of the registry being a value.**
ADR-0005's completeness check iterates core's identity table and asserts that every member has a proof, so extending the set without extending the table fails CI.
A **registry is enumerable in exactly the same way**:

```
ferrytest.Complete(registry, proofs...)            -> missing=[]
after adding one registration and no proof         -> missing=[netip.Prefix]
```

ADR-0007's weakest point is that the text arm admits an unbounded, unenumerable set that this check structurally cannot cover.
Registration is not in that class, and this is the difference: a registrant who wants the guarantee can have their own CI hold them to it, which is what makes "permitted and forfeits the guarantee" a choice rather than a shrug.

### What this hands [#16](https://github.com/onhotpath/ferry/issues/16)

ADR-0007 already said where the per-type claim belongs: "the claim is a property of `reflect.TypeFor[T]()` alone, so it belongs in the compiled schema and is computed once; where that lives is #16's".
This ADR measures what that is worth and states the one thing it obliges.

**The performance is not the argument and this ADR does not sell it as one.**
Six leaves, three of them registered:

```
lookup per leaf, per call   381 ns/op   5 allocs/op
resolved at compile         283 ns/op   5 allocs/op
```

Against ADR-0003's 476 ns twelve-key cached load, that is real and it is not a headline.
The reason to resolve at compile is that #16 wants a schema that is a plan rather than a description.

**The obligation is one sentence, and it is the whole lifetime answer seen from the other end:**

> Once a type has been resolved against a registry, that registry's answer for that type must never change.

The staleness measurements above are what happens without it.
So "resolve the codec into the schema" and "freeze the registry at its first use" are one decision, and #19 cannot take the first half and leave the second to #16.

**The two tickets nonetheless stay separate, because the freeze has no unanswered question.**
That was checked rather than assumed, and the two things it could have been blocked on are both settled above without reference to #16's signature:

- **what freezes** is the registry rather than each type, decided on import-graph determinism, which is a property of the freeze and not of the entry point
- **the default registry's freeze point** is fixed by Go's own initialisation order for the shape consumers write, which is a property of the language and not of the entry point

What #16 still supplies is a way to hand a registry over and a place to put the cache.
Neither changes an answer here, and both would have to be re-derived if #19 stayed silent.
So the split is a split rather than a deferral: this ADR answers everything it can answer without #16, and #16 inherits a stated obligation instead of an open question.

**Three further constraints #16 inherits**, each measured here rather than asserted:

- **The schema cache key must include the registry.**
  Keying by `reflect.Type` alone is ADR-0004's `EnvSource{Sep}` defect one layer up.
- **The cheap shape is available and the obvious one is not.**
  A `sync.Map` keyed by a `{reflect.Type, *Registry}` pair costs 32 ns/op against 9 ns/op keyed by `reflect.Type` alone, because a two-word struct key boxes into an interface where a `reflect.Type` already is one.
  Hanging the per-type cache **off** the registry gives 10 ns/op, so the outer lookup is a pointer dereference and the inner one keeps the stdlib's shape.
  Which of those #16 picks is #16's; that the pair key is not free is this ADR's measurement.
- **A registry must be long-lived**, because a per-call one gives an unbounded, non-evictable cache.

### The three defects found by running the decisions

All three were found by running rather than by reading, and all three are in code this ADR proposes.
The first is in its own headline example.

**`String()` is not an inverse at the zero value, for three of the five types the one-liner is most attractive for.**
Measured on `go1.27rc2`, zero value to text and back:

| type | route | zero to text | text back to a value |
| --- | --- | --- | --- |
| `netip.Addr` | `String`/`ParseAddr` | `"invalid IP"` | **fails** |
| `netip.Addr` | text pair | `""` | ok |
| `netip.AddrPort` | `String`/`ParseAddrPort` | `"invalid AddrPort"` | **fails** |
| `netip.AddrPort` | text pair | `""` | ok |
| `netip.Prefix` | `String`/`ParsePrefix` | `"invalid Prefix"` | **fails** |
| `netip.Prefix` | text pair | `""` | ok |
| `language.Tag` | `String`/`Parse` | `"und"` | ok |
| `uuid.UUID` | `String`/`Parse` | the nil UUID | ok |
| `url.URL`, `big.Int`, `decimal.Decimal` | `String`/parse | `""`, `"0"`, `"0"` | ok |

ADR-0007 requires a codec to be total over its type **including the zero value**, and gives the reason: "the zero value is the value a codec sees most often, because an unset field is dumped".
So the shape a user is most likely to write is broken on the value it will meet most often.

**And registering it makes the type worse than not registering it.**

```
unregistered, chain step 2 (text pair): dump=string("")           load err=nil
registered via String/ParseAddr:        dump=string("invalid IP") load err=ParseAddr("invalid IP"): unable to parse IP
```

Registration is step one and beats the text pair, so the registration **replaces a correct codec with a broken one**.
The type worked before the user tried to help it.

**This is ADR-0005's `fmt.Stringer` refusal handed back to the user by hand.**
That ADR refused `fmt.Stringer` outright because "`String() string` declares no inverse", measured at three of six ordinary config types where it is not one.
Registration cannot refuse a function the user passed, so the hazard ADR-0005 removed from the chain is fully available at a registration call site, and the one-liner is what makes it attractive.

**The answer is the zero-value check, and this ADR's first draft got it wrong.**
That draft answered with a doc comment plus a preferred constructor, on the assumption that core cannot say anything about a codec without values from the registrant.
The assumption was never checked and it is false: `Register` holds `T`, so it builds the zero value itself.
[The zero-value check](#register-runs-the-codec-against-the-zero-value) refuses all three, at the call site, before any schema exists.

Two further things follow and are kept, because the check is one of four and not four of four:

- Core ships `TextCodec[T](kind)`, whose purpose is narrower than the draft claimed: ADR-0007's chain already claims a type with a text pair, correctly, so `TextCodec` is for **changing the kind** rather than for rescuing the type.
- `StringCodec`'s doc comment names the zero value, because the check reports it and the doc should have prevented it.

And a third thing that the draft asserted and the probes contradicted: **most users hitting this defect should not be registering the type at all.**
ADR-0005's "register a codec for it" error was what prompted the bad registration, and ADR-0007's chain running before kind means that error no longer fires for `netip.Addr`.
So the population at risk is smaller than the draft implied: it is a user changing a representation on purpose, not a user rescuing a refused type.

**The generic wrapper panics on a nil interface, in the encode half.**
`ValueCodec`'s wrapper turns a typed codec into a reflective one, and the obvious spelling is a type assertion:

```
v.Interface().(T) on a nil interface field
  panic: interface conversion: interface is nil, not net.Addr
  ValueCodec[...].func1() -> encLeaf -> dump
```

**And in the decode half.**

```
dst.Set(reflect.ValueOf(out)) for a nil out
  panic: reflect: call of reflect.Value.Set on zero Value
  ValueCodec[...].func2() -> decLeaf -> load
```

Both fixes are one token wide.
The encode half takes the comma-ok form and the decode half takes `dst.Set(reflect.ValueOf(&out).Elem())`, which yields a `Value` of static type `T` whatever the dynamic value is.
Costs on `go1.27rc2`, priced on a **non-nil** interface so the fix is measured where it is not needed:

| | ns/op |
| --- | --- |
| `v.Interface().(T)` | 6 |
| `t, _ := v.Interface().(T)` | 4 |
| `reflect.TypeAssert[T](v)` | 8 |

The comma-ok form is not slower.
`reflect.TypeAssert` also handles the nil case and is the slowest of the three, which is the research's own section 1e result reproduced at an interface target.

**Why these two matter beyond a bug fix.**
The wrapper is the one piece of reflection the registration API owns, and it exists precisely so a registrant never writes a `reflect.Value`.
A defect in it is a defect in every codec ever registered, and no proof a registrant can write catches it, because the codec itself was correct.
ADR-0005 makes the interface case the headline demonstration that a codec collapses a type to a leaf, and the zero value of an interface is a nil interface, so this is the intersection of two rules the design leans on.
It belongs in the codec conformance cases ADR-0007 already asks for, and this ADR says so rather than treating it as an implementation note.

Both were found by the audit fixture, which is the first in three prototypes to dump a registered interface at its zero value.

### What ADR-0008 decided and this ADR applies

[ADR-0008](0008-the-struct-tag-grammar.md) landed while this ADR was in review, and its three seams with registration are reconciled rather than assumed.

- **No tag option names a codec, in either direction.**
  ADR-0008 states it and gives the reason: ADR-0007 put selection in the identity table and the text pair, so a per-field override would be a second selection authority for one type.
  This ADR adds nothing to the tag grammar and needs nothing from it.
  A registration is a call site, and there is exactly one way to supply a codec.
- **A registered type is a leaf, so it takes `default=`, `required` and `omitzero` with no codec-side awareness.**
  ADR-0007 measured that against a chain codec; the audit below measures it against a registered one, which is the case ADR-0006's sentence was written for.
- **The tag key is part of whatever keys the schema cache**, which ADR-0008 hands #16 as a measured obligation.
  This ADR adds the registry to the same key, and ADR-0008 anticipated it in #16's words: "whether the codec registry belongs there depends on [#19](https://github.com/onhotpath/ferry/issues/19) making it process-wide or per-instance".
  The answer is per-instance, so it belongs there.
  ADR-0008's own measurement is the cheap end of that question, a `string` in the key at 28 ns against 18 ns; a registry pointer is the same shape, and [What this hands #16](#what-this-hands-16) records the cheaper nesting available to it.

One consequence of ADR-0008 that this ADR did not have when it was drafted, and which makes its worked example honest rather than illustrative: every exported field must carry a `ferry` tag or schema compile fails.
So the consumer file above is a complete file rather than a sketch with the annotations elided.

### The audit

Every prior session's worst defect was a case the fixtures did not contain, and #12's recorded one was that every fixture put the codec at a leaf in a one-field struct at a non-zero value.
So a registered type was put in every position the walk has, populated and all-zero, through all three planes ADR-0005 requires.

```
/Leaf  /Ptr  /Slice/*  /Array#0  /Array#1  /MapVal/*  /MapKey/*  /Nested/Deep  /Iface
```

| fixture | memory | yaml, real files | flattening |
| --- | --- | --- | --- |
| populated | ok | ok | ok |
| every field zero | ok | ok | **`Ptr` differs** |

**The one failure is ADR-0005's own documented limit, unchanged.**
A nil `*netip.Addr` writes `Null`, and a plane with no null cannot carry it, so it loads back as a non-nil pointer to the zero value.
ADR-0005 measured exactly this for `*int` and `*Cred` and put it on the driver-fidelity side of the line.
A registered type inherits it identically, which is the result worth recording: registration adds no new plane limitation and removes none.

**Two seams were checked rather than assumed.**

ADR-0006's default reaches a registered codec with no default-awareness in the codec:

```
plane empty, default applies -> 10.0.0.1
plane supplies the address   -> 192.0.2.1
```

ADR-0007 measured that for a chain codec; this is the registered one, which is the case ADR-0006's sentence was written for.

And a registered interface emitting `Null` survives a plane that has no null, because its codec accepts `String("")` as well:

```
dump nil net.Addr            -> null
through the flattening plane -> string("")
loads back                   -> <nil>
```

That is the accepted-set rule doing real work rather than being a principle: the registrant chose to accept a kind they never emit, and core did not choose it for them.

### What this ADR does not decide

- **Where the compiled schema is cached, and what the entry point looks like**: [#16](https://github.com/onhotpath/ferry/issues/16), with the one obligation and three constraints above.
- **Whether a root leaf is a legal address.**
  A registered type at the root mints the empty path, which ADR-0003 says an address may not be.
  Pre-existing, named by ADR-0007 as #16's, and registration enlarges the set of types that can sit there.
- **Core's admissible map key set**: [#31](https://github.com/onhotpath/ferry/issues/31), untouched on purpose.
- **The error types every refusal here produces**: [#9](https://github.com/onhotpath/ferry/issues/9)'s convention, applied rather than invented.
  This ADR collects and joins its registration errors per ADR-0001's determinism invariant and defers the types.
- **How a default is spelled, and how a field is named**: [#11](https://github.com/onhotpath/ferry/issues/11).
- **Whether the walk may run concurrently**: [#20](https://github.com/onhotpath/ferry/issues/20).
  This ADR decides only that the registry is immutable for the life of every schema compiled against it.
- **What `ferrytest` exports, and what the conformance suites contain**: [#35](https://github.com/onhotpath/ferry/issues/35), filed from this ADR.
  This ADR adds three things to a package that seven ADRs now assign obligations to and no ticket owned: `RoundTrip` taking a `*Registry`, `Complete` over a registry, and `Injective` over a key codec's value list.
  It establishes that each is possible, and that none of them needs anything from a `Reg`, which is the constraint the package's design inherits.
  It does not decide their spelling, and it names two collisions it created rather than leaving them to be found: ADR-0005's `RoundTrip` and this one's are the same function with different signatures, and ADR-0005's completeness check over core's table and this one's over a registry are the same check over two tables.
- **The exported verb names**, which ADR-0001 left open.
  `Register`, `Registry`, `TextCodec`, `StringCodec`, `ValueCodec`, `DurationLike` and `AsMapKey` are the working spellings, and the trio's naming is argued rather than assumed.

## Consequences

- A registrant writes one line for a type whose text pair already exists, and up to eighteen for an interface.
  Inference works at every call site with a value argument, so no user writes an explicit type argument except for `DurationLike[T]` and `TextCodec[T]`, which have nothing to infer from.
- **A half pair is a build error rather than a runtime refusal**, so ADR-0007's "a codec is a pair" needs no check on the registration path at all.
- **The one-line registration a user is most likely to write is wrong for three common stdlib types**, and `Register` refuses all three itself, at the call site, with nothing supplied by the registrant.
  That check catches one class of wrong codec out of four, and the ADR states the ratio rather than the headline.
  The other three classes are ADR-0005's triple's, and they still only help a registrant who writes a proof.
- Registration beats the text pair, which is what makes it the remedy for the dependency-drift exposure ADR-0007 recorded against before-kind ordering, and also what makes the defect above worse than it would otherwise be.
  Both follow from the same rule and the ADR states both.
- **The freeze is answered in full here, so #19 and #16 stay separate tickets.**
  The two questions that could have blocked it, what freezes and where the default registry's freeze point falls, are both decided by properties of the freeze and of the Go language rather than of #16's signature.
- **The registry is a value that freezes at its first use, so nothing in ferry can be registered late.**
  A user who registers after their first Load gets a loud error rather than a silently stale schema, and the error names the freeze point.
  This is a real ergonomic constraint and it is the price of every soundness result in this ADR.
- Core ships a default registry, so the zero-configuration path survives, and a test that wants a different codec for one type constructs its own registry rather than being unable to run.
- **ferry has no dynamic registration**, so a `reflect.StructOf` type cannot be registered.
  The argument that it has nowhere to land is contingent on [#16](https://github.com/onhotpath/ferry/issues/16) keeping a generic entry point, and this ADR says so rather than claiming a settled reason.
- **There is no decline**, so a codec's claim is a property of the type alone, which is what keeps the address set computable with no value in hand.
  The cost is that a type whose representation should depend on its value has no expression, and the answer is that such a type has no proof anybody can write either.
- A registered key codec must opt in, so `map[T]V` over a freshly registered `T` is a compile error until the registrant says the word.
  That is a false refusal for an injective codec, and it is the only place in this design where ferry asks for a keyword rather than inferring.
  `ferrytest.Injective` is proposed alongside it rather than instead of it, because the keyword and the check answer different questions.
- **The trio is renamed so it can be told apart**: `TextCodec` supplies nothing but a kind, `StringCodec` supplies two string functions, `ValueCodec` supplies a kind and two `Value` functions.
  The old name `TypeCodec` did not distinguish itself from either sibling, and this is the one place the ADR changes a spelling on a readability argument rather than a measurement.
- **Registration is enumerable where the text arm is not**, so ADR-0005's completeness check ports to a user's registry.
  ADR-0007's weakest point does not get better, but registration does not join it.
- Surfaced and filed rather than decided inline: **`ferrytest` has seven ADRs' worth of assigned obligation and had no owner**, now [#35](https://github.com/onhotpath/ferry/issues/35).
  This ADR is the fourth to add to that package and the first to add three things at once, which is what made the gap visible.
- The generic wrapper is the one piece of reflection this API owns, and two defects in it would have been defects in every registered codec.
  Both are one token wide and neither is catchable by a registrant's proof, which makes the codec conformance suite ADR-0007 asks for load-bearing rather than optional.
- ADR-0005's named-duration hole is closed by `DurationLike[T ~int64]()`, at one line per named type.
  That ADR called it "a documented sharp edge rather than a defect ferry can reflect its way out of", and it is still not reflected out of: the user names the types.
- Registration adds no new plane limitation and removes none.
  A nil pointer to a registered type fails on a plane with no null exactly as a nil pointer to a core type does.

## Items from the xload survey

The survey is [`docs/research/generics-and-modern-go.md`](../research/generics-and-modern-go.md), section 5, against `github.com/gojekfarm/xtools` at [`a90b3aa`](https://github.com/gojekfarm/xtools/commit/a90b3aad2133248cec50f6b4d6e37b0d9e788adb).

**5.9's third bullet is this ADR's**: "No way to register a decoder for a type you do not own, and `time.Duration` matched by `Type.String()`."

The `Type.String()` half is dead, and it died in ADR-0005's identity table rather than here.
The registration half is answered in full: a caller registers a typed pair for a type they do not own, at one to eighteen lines, with `T` inferred, and the codec goes into the same identity table the chain consults first.
The rest of 5.9 is ADR-0007's and is unaffected.

Two things worth recording, because 5.9 is where the survey's own recommendation sits.

**Recommendation 7 says to copy `MarshalToFunc[T]` / `UnmarshalFromFunc[T]` / `JoinMarshalers` "with a `SkipFunc`-style decline-and-fall-through and a documented, overridable precedence chain".**
Three of those four are declined, and each on a measurement rather than on taste.
The typed-function shape is adopted.
`SkipFunc` does not exist in the standard library, its respelling does not port to a non-streaming boundary, and ferry takes neither.
"Overridable precedence" is refused because ADR-0007 already made the chain three fixed steps and a map at step one, and R5 measured what a user-orderable list does.

**Recommendation 7's own cost note says "ferry probably needs both static and dynamic registration".**
That is checked and declined, with the reason stated: the dynamic half has nowhere to land while the entry point is generic.

**5.14** was enumerated rather than assumed, all four items.

- *Two ways to set the loader.*
  Bears on this ADR directly and is the item it came closest to repeating.
  A default registry plus a scoped registry is two ways to supply a codec, which is the shape of the defect.
  It is avoided by them being **one mechanism**: the default registry is a `Registry`, it freezes on first use like any other, and package-level `Register` is a method call on it rather than a second path with its own rules.
  Nothing is expressible through one and not the other.
- *The `CanAddr` loop that can only run once.*
  Bears on this ADR through the generic wrapper, which is the code that takes a `reflect.Value` and produces a `T`.
  It is not carried over, and the defects it did have are the opposite kind: not a loop that runs once, but a type assertion that panics on a nil interface, recorded above.
  ADR-0007 already replaced the instinct behind the loop with a stated rule about receivers, and registration does not reopen it, because a registered codec is called with the value at its own address and never takes an address of its own.
- *The non-deterministic select on a cancelled context.*
  Concurrency, and [#20](https://github.com/onhotpath/ferry/issues/20)'s.
  This ADR bears on it in one direction only, and it is the helpful one: a frozen registry is not shared mutable state, measured against the race detector's report for a mutable one.
- *Value receivers on `Error()` where pointers are returned.*
  Bears on this ADR twice.
  The error types it produces are deferred to [#9](https://github.com/onhotpath/ferry/issues/9)'s convention rather than pre-empted, as ADR-0003 through ADR-0007 all did.
  And its underlying cause, that a method set differs between a value and its pointer, is why `url.URL.String` is not a legal method expression and why the pointer-type refusal exists at all.

**5.5, nondeterministic error output**, is [#9](https://github.com/onhotpath/ferry/issues/9)'s, and this ADR applies it rather than deciding it: `Register` collects every failure in a variadic call and reports them joined, rather than returning at the first.

**5.7, `reflect.DeepEqual` as a probe**, is ADR-0005's, and it surfaces here as the reason the proof's relation is the registrant's and not the harness's.
The registered `netip.Addr` codec's failure at the zero value is caught under `==`; a registrant who chooses a looser relation quietly widens what ferry tolerates, which is ADR-0005's own consequence applied to a type core does not own.

**5.10's remaining half**, a flat plane holding a whole list in one value and a codec splitting it, is confirmed unchanged from ADR-0007: a splitting codec is a registered codec, the registrant carries the round-trip proof, and no arm in the chain does it.

The remaining items are unaffected by this ADR.
