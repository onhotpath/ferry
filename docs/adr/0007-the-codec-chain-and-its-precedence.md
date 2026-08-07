# 7. The codec chain, and its precedence

Status: Accepted
Date: 2026-08-02
Ticket: [#12](https://github.com/onhotpath/ferry/issues/12)

## Context

ferry converts a Go value to a boundary `Value` and back.
[ADR-0005](0005-the-supported-type-set.md) settled which types core owns and how they are represented, and settled it by two mechanisms: an identity table keyed by `reflect.Type`, consulted before `reflect.Kind` admission.
It did not settle what happens between those two steps.

That gap is this ADR's, and ADR-0005 handed it over with a measurement rather than an opinion:

> The maps-no-address rule and the kind admission rule are **backstops**, and they only apply after the codec chain has declined.

The inherited answer is xload's, and it is a decode-only type switch at [load.go:403-439](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L403-L439): `Decoder`, then `encoding.TextUnmarshaler`, then `json.Unmarshaler`, then `encoding.BinaryUnmarshaler`, then `gob.GobDecoder`.
It has no encode counterpart, its order is the order the cases were written in, it never sees an empty input, and it takes no `context.Context`.
That is survey item **5.9**, which is this ADR's outright.

Four constraints bind before anything is decided.
[ADR-0001](0001-what-ferry-supports.md): core guarantees value fidelity over the set core ships, registration **extends** that set and transfers the guarantee, and silently ignoring anything is ruled out.
[ADR-0002](0002-core-and-sub-modules.md): core imports only unconditionally-available stdlib, so `encoding/json/v2` and `jsontext` are excluded, and probing for a method by name string is ruled out by name.
[ADR-0004](0004-source-and-sink.md): the boundary value is a comparable `{kind, text}` with kinds `Absent`, `Null`, `Bool`, `Number`, `String`, `Bytes`.
ADR-0005: `fmt.Stringer` is never consulted, a codec collapses a type to a leaf, a key codec must be injective, and on Load `String` is the universal donor.

[ADR-0006](0006-defaults-and-zero-values.md) ([#8](https://github.com/onhotpath/ferry/issues/8)) ran in parallel with this one and landed first.
Its definitions are applied here rather than re-derived, and the three shared seams are reconciled explicitly in [What #8 decided and this ADR applies](#what-8-decided-and-this-adr-applies).

This ADR is written from a throwaway prototype on branch `proto/12-codec-chain`, which never merges.
It is built on `proto/7-type-set`, so every measurement runs against the type set ADR-0005 actually landed, a real `Path`, a real `Value` and a real YAML plane over real files.
Every number is from that prototype unless it cites the survey.

**Three of the eighteen probes found defects: two in inherited code, one in this ADR's own rule.**
All three are recorded below rather than quietly fixed, and one of the inherited two is in a decision ADR-0005 already took.

## Decision

### What this closes, and what it does not

The ticket asked for nine things by name.
This table is the answer to each, so a reader can check the ADR against the ask without reading the rest of it.

| The ticket asked | Closed | Where |
| --- | --- | --- |
| the encode interface, and whether it mirrors the decode chain exactly | **yes**: ferry declares none, and selection is one paired decision rather than two mirrored chains | [The chain is three steps](#the-chain-is-three-steps-and-a-type-is-claimed-once-as-a-pair), [ferry declares no codec interface](#ferry-declares-no-codec-interface-of-its-own) |
| the precedence order, in both directions, as policy | **yes**, and it is one order because there is one selection | [The chain is three steps](#the-chain-is-three-steps-and-a-type-is-claimed-once-as-a-pair) |
| **whether the chain runs before or after kind admission** | **yes: before** | [The chain runs before kind admission](#the-chain-runs-before-kind-admission) |
| a type implementing a decoder but no matching encoder | **yes**: schema compile refuses, naming the missing method | [An incomplete pair does not compile](#an-incomplete-pair-does-not-compile) |
| whether core recognises json/v2's `MarshalerTo` and `UnmarshalerFrom` | **yes: no**, and ADR-0002 is not the reason | [MarshalerTo is not recognised](#marshalerto-and-unmarshalerfrom-are-not-recognised-and-adr-0002-is-not-the-reason) |
| whether a registered codec sees the raw boundary `Value` or the donated one | **yes**: the donated one, and it declares the kind donation targets | [A codec declares its kind](#a-codec-declares-its-kind-and-sees-the-donated-value) |
| whether the chain is invoked for an `Absent` or a zero value | **yes**: never for `Absent`, always for a value that is dumped at all | [Absent never reaches the chain](#absent-never-reaches-the-chain-and-null-is-the-codecs) |
| where `TextAppender` sits | **yes**: inside the text arm, not ahead of it | [TextAppender is a spelling](#textappender-is-a-spelling-not-an-arm) |
| whether a codec may take a `context.Context` | **yes: no**, and this departs from 5.9's implied fix | [A codec takes no context](#a-codec-takes-no-context) |

Four questions this ADR had to answer that the ticket did not name:

| Not asked for, answered anyway | Where |
| --- | --- |
| which arms exist at all, given that xload has five and this ADR keeps one | [Only the text pair is recognised](#only-the-text-pair-is-recognised) |
| whether a chain-admitted type may key a map, and who carries injectivity | [A chain-admitted type may key a map](#a-chain-admitted-type-may-key-a-map-and-core-cannot-check-the-obligation). Answered **yes** here and **reversed to no** under [#45](https://github.com/onhotpath/ferry/issues/45); keying a map is registration-only |
| what the chain does to ADR-0005's completeness check | [What the chain costs the harness](#what-the-chain-costs-the-harness) |
| what a pointer type does when it satisfies an arm in its own right | [Three defects found](#three-defects-found-by-running-the-decisions) |

**Three things this ADR does not close.**

- **The set the text arm admits is unbounded and unenumerable**, so ADR-0005's completeness check cannot cover it and no proof exists for its members.
  This is stated as a cost with a named mitigation, not solved.
- **`map[time.Time]string` collapses two distinct keys into one address, today, in core's own set.**
  Found here, and fixing it amends ADR-0005 rather than this one, so a ticket is proposed rather than a fix taken inline.
  *(Closed since, under [#31](https://github.com/onhotpath/ferry/issues/31), by exactly the amendment this bullet predicted: [ADR-0005](0005-the-supported-type-set.md) makes key admissibility a per-entry declaration rather than an inheritance from the identity table, and `time.Time` is dropped as a key type because no text form is injective over it under `==`.
  A `map[time.Time]string` is refused at schema compile, and the diagnostic says why.)*
- **A `[]byte` field's declared default is taken as raw bytes and not as base64.**
  Named, and it belongs to [#11](https://github.com/onhotpath/ferry/issues/11)'s documentation of how a default is written.
  *(Discharged since, exactly where this bullet sends it: [`docs/guide/tags.md`](../guide/tags.md) carries the sharp edge under its own heading, with the worked case.)*

### The chain is three steps, and a type is claimed once as a pair

> A type is claimed by the first of three steps that will have it, and the claim is a **pair**.
> The same claim serves Load and Dump.
>
> 1. **The identity table.** `reflect.Type` compared with `==`. Core's own entries are pre-seeded; registration ([#19](https://github.com/onhotpath/ferry/issues/19)) adds to the same table.
> 2. **The text pair.** `encoding.TextAppender` or `encoding.TextMarshaler`, together with `encoding.TextUnmarshaler`. Both halves required.
> 3. **Kind admission.** ADR-0005's table.
>
> A pointer type is never offered to steps 1 to 3 as itself: pointer indirection is structural and is resolved first.

That is the whole order, and it is three steps where xload has five arms and `encoding/json/v2` has five.
Every removal is argued below.

**The pair is the departure, and it is a correctness rule rather than a tidiness one.**
xload's chain is one-directional, so it never had to make encode and decode agree.
A chain written as its mirror image selects each direction independently, and that is what two type switches produce.
Measured, over types carrying an asymmetric set of interfaces:

| type | per-direction selection | paired selection |
| --- | --- | --- |
| text encoder, json decoder | dumps `string("hello")`, load fails: `invalid character 'h'` | no arm; falls through to kind, and round-trips |
| binary encoder, text decoder | dumps `bytes("\a")`, load fails: wrong kind | no arm; falls through to kind, and round-trips |
| both a text and a json pair | text/text, agrees by luck | text, agrees by construction |

Two things to read out of that.
The first two rows are values that **dump successfully and never come back**, so the failure lands at Load against a plane that has already been written.
And in both rows paired selection does not merely fail more quietly, it **succeeds**: the type falls through to kind admission and round-trips.

**The stdlib has the same defect, which is why mirroring it would not have been safe.**
Measured on `go1.27rc2`, a type whose `MarshalJSON` returns an object and which implements only `UnmarshalText`:

```
json.Marshal   -> {"a":1}
json.Unmarshal -> json: cannot unmarshal object into Go value of type ...: JSON value must be string type
```

v2 selects per direction and asks only in prose that a type's implementations "aim to have equivalent behavior".
ferry cannot ask that, because ferry's arms have **different representations** rather than different spellings of one, so the ADR makes the pair structural instead.

**Precedence is only exercised where a type carries more than one arm**, which is why the fixtures were built for it rather than around it.
Measured over 29 types people put in config structs: **10 of 29 carry more than one complete arm**, `time.Time` and `decimal.Decimal` carry all four.
A fixture where every type carries exactly one arm tests nothing about precedence, and that is the shape of mistake the previous five sessions each made once.

**Core's own entries are not replaceable.**
Registering a type already in the identity table is a loud error, not an override.
ADR-0001 says registration **extends** the set, and an override would break the golden column that pins `30s` and RFC 3339.
A user who wants a different representation for a type core owns defines a named type and registers that, which is the same escape ADR-0005 already documents for `type Timeout time.Duration`.

### ferry declares no codec interface of its own

> There is no `ferry.Encoder` and no `ferry.Decoder`.

This is the answer to "what is the encode interface", and it is that there is not one.
Three reasons, in order of weight.

**A ferry-declared interface would cost its implementor an import that `encoding.TextMarshaler` does not.**
`MarshalFerry() (ferry.Value, error)` can only be written by a package that imports ferry.
`MarshalText() ([]byte, error)` can be written by a package that has never heard of ferry, which is the whole reason `encoding`'s interfaces are where they are.
For a general-purpose mapper whose adoption depends on other people's types already working, that is decisive.

**A one-directional interface is where half pairs come from.**
Measured by scanning declarations across the whole `go1.27rc2` public standard library and a third-party corpus, the encoding interfaces are written and used as pairs: **one encoder-only type in the entire public stdlib** (`unify.Pos`, an internal tooling type) and **zero half pairs** in the third-party corpus.
The half pairs that do exist in ferry's prior art are the ones xload's own interface created: `xloadtype.URL`, `.Endpoint` and `.Listener` each carry `Decode(string) error` plus a `String()`, and the survey records those `String()` methods as "unspecified, untested as a round trip, and not used by the library".
So the defect is not that users write halves.
It is that a one-directional interface makes a half the only thing they **can** write.
Not declaring one removes the source rather than the symptom.

**What an interface would buy, registration already buys better.**
The one thing `encoding.TextMarshaler` cannot say is which boundary kind the value should be.
A ferry interface could say it; so can a registration call, which also gets a place to declare a relation and a golden value.
So the kind declaration lands in [#19](https://github.com/onhotpath/ferry/issues/19) rather than in a third interface.

The cost is stated plainly: a user who owns a type and wants a non-`String` boundary kind must write a registration call somewhere, where an interface would have let the type carry its own codec.
That is one line at a package boundary, against a new interface in the world forever.

### The chain runs before kind admission

> The text pair is consulted **before** `reflect.Kind` admission.
> A declaration beats an inference.

This is the largest question the ticket owns, and it was measured three ways over one fixture list rather than argued.
The three candidate orders and what each produces:

| type | kind only, today | chain as a rescue, after kind | chain before kind |
| --- | --- | --- | --- |
| `netip.Addr` | refused | `string("192.0.2.1")` | `string("192.0.2.1")` |
| `netip.AddrPort` | refused | `string("192.0.2.1:80")` | `string("192.0.2.1:80")` |
| `netip.Prefix` | refused | `string("10.0.0.0/8")` | `string("10.0.0.0/8")` |
| `big.Int` | refused | `string("1099511627776")` | `string("1099511627776")` |
| `decimal.Decimal` | refused | `string("1.25")` | `string("1.25")` |
| `regexp.Regexp` | refused | `string("^a.*z$")` | `string("^a.*z$")` |
| `language.Tag` | refused | `string("en-GB")` | `string("en-GB")` |
| `net.IP` | `bytes("\x00...\xc0\x00\x02\x01")` | unchanged | `string("192.0.2.1")` |
| `uuid.UUID` | `bytes("\x0e7\xdf6...")` | unchanged | `string("0e37df36-f698-11e6-...")` |
| `slog.Level` | `number("4")` | unchanged | `string("WARN")` |
| a struct with a text pair and two fields | two addresses | two addresses | one `string("1.2")` |
| `url.URL` | refused, via `url.Userinfo` | refused | refused |

**Both readings of "run the chain" rescue the same seven refusals at no fidelity cost.**
The after-kind reading is not a straw man and was implemented as ADR-0005's own framing, rescuing exactly what kind admission refuses including the maps-no-address backstop.
So the ordering question is not "does the chain help"; it is only about the four rows where kind admission would have answered.

**The artefact is the argument.**
The same struct written to a real YAML file through the real driver:

```yaml
# kind only, or the chain as a rescue after kind
id: !!binary DjffNvaYEeaN1Muc7T35dg==
level: 4
listen: !!binary AAAAAAAAAAAAAP//wAACAQ==
name: "svc"

# the chain before kind
id: "0e37df36-f698-11e6-8dd4-cb9ced3df976"
level: "WARN"
listen: "192.0.2.1"
name: "svc"
```

ADR-0001 puts legibility on the driver's side of the line, so nothing in core's guarantee catches the first one.
Both round-trip exactly.
That is precisely ADR-0005's category 3 - a representation nobody chose - and the ordering is the one lever that shrinks it.

**Two things decide it beyond legibility, and both were measured.**

**Under after-kind, exporting a field silently rewrites the type's plane representation.**
Two types differing by one exported field, both carrying the same text pair:

```
after kind    no exported field:  /V     -> string("g7")
              one exported field: /V/N   -> number("7")
before kind   no exported field:  /V     -> string("g7")
              one exported field: /V     -> string("g7")
```

Under after-kind an edit nobody would review as a serialization change rewrites every stored artefact of that type.
That is [#28](https://github.com/onhotpath/ferry/issues/28)'s breaking change, triggered from outside the encoding surface entirely.

**Before-kind has the mirror exposure, and it is stated here rather than left implied, because an earlier draft of this section read as though only after-kind drifted.**
Under before-kind the representation changes when the text methods change, and those methods can be added by a **dependency**, in a release the consumer did not read, to a type the consumer embeds.
Measured, the same type before and after an upstream release adds the pair, with no change on the consumer's side:

```
chain BEFORE kind
  dep v1 (no text pair)     addresses=[/backend/Host /backend/Port]   Host=string("h") Port=number("80")
  dep v2 (text pair added)  addresses=[/backend]                      /backend=string("h:80")

chain AFTER kind
  dep v1                    addresses=[/backend/Host /backend/Port]   unchanged
  dep v2                    addresses=[/backend/Host /backend/Port]   unchanged
```

So **both orders drift**, and the honest distinction is which edit triggers it.
Before-kind drifts on an edit that is visibly about serialization, and a reviewer seeing `func (T) MarshalText` added has been told what it is for.
After-kind drifts on exporting a field, which is not a serialization change in any reviewer's reading.
The mirror case is benign under both: a type that was **refused** acquiring a text pair goes from a compile error to a working field.

That is the argument for before-kind at its true strength, which is narrower than "after-kind drifts and before-kind does not".

**Under after-kind, ferry disagrees with `encoding/json` about a type whose author declared one text form.**
A `type Level string` whose `MarshalText` normalises to upper case writes `INFO` through `encoding/json` and would write `info` through a ferry that ignored the declaration.
Two conversion authorities disagreeing about one type is the viper defect the survey measured and ADR-0005 names.

**The cost, stated exactly, because it is real and ADR-0005 did not price it.**

- **`net.IP` loses which of its two byte encodings you had.**
  Measured: `net.ParseIP("192.0.2.1")` is 16 bytes and round-trips through text **byte-exactly**; a hand-built `net.IP{192,0,2,1}` is 4 bytes and comes back as 16.
  `bytes.Equal` says those differ and `net.IP.Equal` says they do not, so this is a loss under `==` and not under the type's own relation.
  ADR-0005's three-outcomes row for `net.IP` therefore stays true for the value `net.ParseIP` produces and needs the qualification for the other spelling.
- **A `MarshalText` that is not an inverse breaks a type that kind admission would have round-tripped.**
  Measured, and measured again through `encoding/json`, where the same type is **already** broken.
  So ferry inherits the hazard rather than inventing it, and the honest framing is the next paragraph.

  **How common that is was measured rather than left as a class**, by building values in deliberately non-canonical forms and running them through the text pair.
  **Three of fourteen do not come back identical under `DeepEqual`:**

  ```
  net.IP           net.IP{192,0,2,1}, 4 bytes    -> string("192.0.2.1")   NO
  decimal.Decimal  1.2500, trailing zeros        -> string("1.25")        NO
  regexp.Regexp    the zero value                -> string("")            NO

  netip.Addr v4-in-v6 and zoned, netip.Prefix with host bits set, big.Int -0,
  uuid.UUID from upper case and from the urn: form, language.Tag from EN-gb
  and from en-Latn-GB, decimal 1.25e2, regexp ^a.*z$        ... all yes
  ```

  **And all three are equal under the type's own equality relation**: `net.IP.Equal` is true across the four- and sixteen-byte forms, `decimal.Equal` is true across exponents -4 and -2, and `regexp`'s zero and `Compile("")` have the same `String()`.
  So the hazard is not that the text form is lossy.
  It is that `DeepEqual` is the wrong relation, which is survey item **5.7** and the thing ADR-0005 already made per-type.
  **Zero types in the measured corpus lose information under their own relation.**
  A type that canonicalises on construction, as `uuid.UUID` and `language.Tag` do, is not in the hazard class at all, because there is no non-canonical value to lose.
  The synthetic `"info"` to `"INFO"` type that does lose information has no counterpart in the corpus.
- **The address set of a type depends on the chain**, so template generation emits differently and a driver's key function is checked over a different set.
  Named here so it is not rediscovered as a bug.

**A text pair is an implicit registration, and it carries the registrant's guarantee rather than core's.**
ADR-0001 says core guarantees value fidelity over the set core ships, that registration extends the set, and that extension transfers the guarantee.
A type whose author wrote `MarshalText`/`UnmarshalText` has declared a representation ferry did not choose, which is registration in everything but the call site.
So its round trip is the type author's promise.
The difference from an explicit registration is that nobody was prompted to write a proof, and [What the chain costs the harness](#what-the-chain-costs-the-harness) is where that is priced.

### Only the text pair is recognised

> `json.Marshaler`/`Unmarshaler`, `encoding.BinaryMarshaler`/`BinaryUnmarshaler` and `gob.GobEncoder`/`GobDecoder` are **not** arms.

xload has all three and `encoding/json/v2` has the first.
Each is dropped on a measurement, not on taste.

**gob rescues nothing.**
Measured over 29 types: 5 complete gob pairs, and gob is the **sole** arm for **none** of them.
Every type with a gob pair also has a text pair, so a gob arm would only ever fire where a higher arm already answered.
An arm that can never uniquely rescue a type is an arm with no purpose.

**json rescues one type, and kind admission already handles it.**
Measured: 5 complete json pairs, and json is the sole arm for exactly one, `json.RawMessage`.
`json.RawMessage` is `jsontext.Value` is `[]byte`, which ADR-0005 already admits as `Bytes` and which round-trips exactly.
So the json arm's unique rescue count is zero, and what it would cost is not zero:

- **JSON syntax becomes literal plane bytes.**
  Measured on a real YAML file, the same two fields through each arm:

  ```yaml
  # text arm            # json arm
  level: "WARN"         level: "\"WARN\""
  money: "1.25"         money: "\"1.25\""
  ```

  On an environment plane that is `LEVEL="WARN"` with the quote characters in the value.
- **It reintroduces opaque subtree capture through the back door.**
  A `MarshalJSON` returning an object collapses `/V/A` and `/V/B#0` into one address holding `string("{\"a\":1,\"b\":[\"x\",\"y\"]}")`.
  ADR-0004 removed the group arm and ADR-0005 states that opaque capture of a structured subtree is not wanted and would be "the first member of the set whose proof nobody can write".
- **Every json form measured is the text form wrapped in JSON syntax.**
  `time.Time`, `slog.Level` and `decimal.Decimal` all produce the text form plus quotes, and `big.Int` produces the identical bytes.
  So the arm adds a representation nobody wants for types that already had a better one.

**binary rescues one type, and rescues it badly.**
Measured: 7 complete binary pairs, sole arm for exactly one, `url.URL` - which is the type ADR-0005 predicts "will be reported as a bug".
That looks like the strongest case for the arm until the artefact is examined.
`url.URL.MarshalBinary` returns the URL **text**:

```
url.URL MarshalBinary -> "https://user:pw@example.com/a/b?q=1#f"
as a Bytes leaf on YAML -> !!binary aHR0cHM6Ly91c2VyOnB3QGV4YW1wbGUuY29tL2EvYj9xPTEjZg==
```

So the binary arm would rescue `url.URL` by base64-encoding a string.
A registered codec for `url.URL` is three lines and yields `string("https://...")`.
The arm is worse than the thing it would save the user from writing, and the other six binary pairs all have a text pair that wins first anyway.

**One rule falls out of all three**, and it is the admission test for any future arm:

> An arm earns its place by being the **sole** complete pair for a type kind admission would otherwise mishandle, and by producing a representation a human can read on a flat plane.

### `TextAppender` is a spelling, not an arm

v2 puts `TextAppender` ahead of `TextMarshaler` and xload's chain does not know it exists.
Neither shape is right for ferry, for a structural reason:

> There is no appending **decoder**.

Verified on `go1.27rc2`: `encoding` exports `AppendText([]byte) ([]byte, error)` and `AppendBinary`, and no `AppendFrom` of any kind.
So `TextAppender` cannot be an arm, because an arm is a pair and there is nothing to pair it with.
It is a second spelling of the text arm's **encode half**, answered by the same `TextUnmarshaler`.

Inside the arm, `TextAppender` is preferred when both are present.
Measured, one leaf, both routes terminating in the Go string that lives in `Value.text`:

| route | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `MarshalText()` then `string(b)` | 40.0 | 32 | 2 |
| `AppendText(scratch)` then `string(b)` | 25.0 | 16 | 1 |

ferry pays one unavoidable allocation to make the string; the appender removes the other one.
**This is a tidiness win and not a performance argument**, and the ADR says which: ADR-0003 measured a twelve-key cached load at 476 ns, so the saving is a handful of nanoseconds per leaf, once per Load or Dump, off a path that is not hot.

Preferring one spelling inherits an obligation the compiler cannot check: the two must produce the same bytes.
Measured, they do on every stdlib type carrying both (`time.Time`, `netip.Addr`, `netip.AddrPort`, `netip.Prefix`), because the stdlib implements one in terms of the other.
Nothing enforces it for a user type, so it is a conformance case in the codec harness rather than a promise.

### `MarshalerTo` and `UnmarshalerFrom` are not recognised, and ADR-0002 is not the reason

ADR-0002 bars the `jsontext` import from core and states that if this ticket concludes otherwise, that is an amendment argued explicitly.
It does not conclude otherwise, and it is worth being clear that the ADR is not the reason, because "the other ADR forbids it" is a bad answer if the interface is worth an amendment.

**The interface buys nothing at ferry's boundary, measured.**
v2's own rationale is that `MarshalJSON` forces an allocation and a re-parse, and that this is API-bound rather than implementation-bound.
`MarshalerTo` fixes it by letting a value write straight into the enclosing stream.

ferry has no enclosing stream.
ADR-0004 fixed the boundary value as a standalone `{kind, text}`, so every leaf is materialised on its own regardless.
Measured on `go1.27rc2`, both routes terminating in a ferry `Value`, with `MarshalerTo` given its best case of one reused encoder and one reused buffer:

| route | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `Marshaler` | 35.0 | 16 | 1 |
| `MarshalerTo` | 63.0 | 16 | 1 |

Same allocations, roughly twice the time.
A fresh encoder per leaf, which is what a non-streaming caller actually does, costs 400 B and 6 allocs.
**The allocation `MarshalerTo` exists to remove is the one ferry's own boundary reinstates.**

And its output is JSON syntax, so it lands on a plane with the quotes in it, which is the json arm's problem from the section above with a `jsontext` import, a `go 1.27` floor and a build break under `GOEXPERIMENT=nojsonv2` attached.

So of the survey's four readings of "first class json/v2", reading (a), codec-chain recognition, is **rejected on its own merits**.
Reading (b), semantics, is already taken by ADR-0002 and ADR-0005 and costs no import.
ADR-0002's stated cost - "core's codec chain cannot natively recognise json/v2's `MarshalerTo` or `UnmarshalerFrom`" - turns out not to be a cost, and this ADR records that rather than leaving ADR-0002's consequence reading as a sacrifice.

Two facts confirmed and not relied on: v1's `json.Marshaler` is a type alias for v2's on `go1.27rc2`, so a probe would have found both with one lookup; and `MarshalerTo` does take precedence over `Marshaler` inside v2, verified by execution.
Neither matters once the arm is not recognised at all.

**A sub-module remains available and is not proposed.**
ADR-0002 puts `MarshalerTo` recognition in a sub-module if this ticket wants it.
It does not, for the same measurement: the sub-module would inherit the allocation result and the quoting result unchanged, and would ship a second conversion authority for the sake of an interface that buys nothing.
A JSON **driver** is a different thing entirely and is ADR-0004's, unaffected by this.

### An incomplete pair does not compile

> A type implementing one half of an arm and not the other is a schema compile error naming the method that is missing.

There are three candidate answers and the census decides between them.

**Using the half anyway** is per-direction selection, measured above: the value dumps and never loads.

**Falling through to kind admission silently** is the tempting answer, and it is what a naive implementation does.
Measured, a struct with a `MarshalText`-only field and an `UnmarshalText`-only field compiles clean, maps `/A/Name` and `/B/Name`, and round-trips.
It also ignores, with no diagnostic, two methods the user wrote for exactly this purpose.
ADR-0001 rules out silently ignoring anything, and this is that.

**Refusing** is the answer, and the census says it is affordable:

| corpus | text pairs | encoder only | decoder only |
| --- | --- | --- | --- |
| 29 types people put in config structs, probed in process | 13 | 0 | 0 |
| the whole `go1.27rc2` public standard library, by declaration scan | 13 | 0 | 0 |
| koanf v2, viper, mapstructure, yaml.v3, BurntSushi/toml, google/uuid, shopspring/decimal, x/text, xtools/xload, spf13/cast, and their transitive dependencies | 12 | 0 | 0 |

**Zero half pairs, in all three corpora, for all four arms.**
The scan's first pass reported one, `unify.Pos`; it is under `src/simd/archsimd/_gen/`, which the go tool does not compile at all because the directory begins with an underscore, so it is not part of the standard library and the corrected count is zero.
The binary and gob arms show zero halves everywhere too.

The diagnostic, measured:

```
ferry: /A: main.encOnly implements encoding.TextMarshaler but not encoding.TextUnmarshaler,
       so the pair is incomplete and ferry will not use it; implement the other half,
       or register a codec
ferry: /B: main.decOnly implements encoding.TextUnmarshaler but not encoding.TextMarshaler,
       so the pair is incomplete and ferry will not use it; implement the other half,
       or register a codec
```

Detection is at schema compile, from `reflect.TypeFor[T]()` alone, with no value in hand and identically in both directions, which is the same assertability ADR-0001 claims for tag rejection and ADR-0005 for the static half of the collision rule.
Violations are collected, joined and sorted, per ADR-0001's determinism invariant.
The error types follow [#9](https://github.com/onhotpath/ferry/issues/9)'s convention.

**This is the answer to the ticket's "a decoder but no matching encoder", and ADR-0005 owns the reason it is a refusal rather than a narrower guarantee.**
ADR-0005: such a type cannot round-trip, so it cannot be in the guaranteed set, and admitting it would be admitting a member whose proof cannot be written.
This ADR adds where it is detected and how loud it is.

One decision follows for the encode half specifically: **`UnmarshalText` on a value receiver does not satisfy the decode half.**
Measured, it silently writes to a copy and the destination is unchanged.
So the probe is "`T` or `*T` implements the encoder" and "`*T` implements the decoder", and a value-receiver `UnmarshalText` is an incomplete pair with a diagnostic rather than a silent no-op.

### A codec declares its kind, and sees the donated `Value`

ADR-0005 made `String` the universal donor for kind-admitted leaves and left this question here by name.
The two halves entangle, and picking either alone gives a wrong answer.

> A codec declares the boundary `Value` kind it produces, and **the kind is a field of the codec**, resolved by the same lookup that finds the codec.
> Core applies ADR-0005's donor rule unchanged before calling the codec: `String` is normalised to the declared kind, and nothing else coerces.
> The text arm's declared kind is `String`, always, because `encoding.TextMarshaler` produces text and says nothing about kind.

The "field of the codec" clause reads like an implementation note and is not one; keeping the kind in a second map next to the table is the third defect this prototype found, and it is recorded below.

**Why the codec must declare a kind.**
Measured, a `big.Int` codec whose text form is a run of digits:

| codec declares | typed plane says `Number` | flat plane says `String` |
| --- | --- | --- |
| `String` | refused, wrong kind | ok |
| `Number` | ok | ok |

A codec whose text **is** a number must say `Number`, or it works on env and fails on YAML.
That is a real design decision the registrant makes, and ADR-0005's golden column is what catches getting it wrong.

**Why core donates rather than the codec.**
Measured, the correctly-declared `Number` codec, with and without core normalising first:

```
raw       plane says Number -> ok        plane says String -> wrong kind
donated   plane says Number -> ok        plane says String -> ok
```

A codec seeing the raw value fails on env, query parameters and Consul - three of ADR-0004's four first-party planes.
That is ADR-0005's G2 defect, which survived two drafts of that ADR, delegated to every registrant one at a time.
Core owning the donation means a codec is written once and works everywhere.

**The honest cost, and it is not hidden.**
The text arm is `String`, so a YAML document holding an unquoted large integer does not load into a text-arm `big.Int`: the plane says `Number`, the arm wants `String`, and ADR-0005's rule is that a plane's kind assertion is respected rather than second-guessed.
The answer is registration with a declared kind, not a second coercion.
ADR-0005 requires a second coercion to meet the same standard of argument the first one got, and this one does not clear it.

### `Absent` never reaches the chain, and `Null` is the codec's

> The chain is **never** invoked for `Absent`.
> Every other kind reaches the codec, after donation, including `Null` and the empty string.

Survey item 5.9's last bullet is "decoders never see an empty input", and ferry reaches the same-sounding rule for the opposite reason.
Reproduced, xload's decoder against three states:

```
xload   decoder saw: ["v"]          Set="decoded:v"  Empty=""  Missing=""
ferry   decoder saw: ["v", ""]      Set="decoded:v"  Empty="decoded:"  Missing=""
```

xload's guard is `if val == "" { return false, nil }` at [load.go:415-417](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L415-L417), and it is **forced**: xload cannot tell an empty value from a missing key (5.1), so skipping both is the only safe thing it can do.
ADR-0004 made them two observations, so ferry can hand the empty string to a decoder and withhold the absent address.
5.9's defect is repaired, and the repair was paid for three ADRs ago.

**Why `Absent` in particular.**
`Absent` carries no text a codec could rebuild anything from, and what it means to a Go field is [#8](https://github.com/onhotpath/ferry/issues/8)'s and not each codec author's.
Letting a codec see it would put ADR-0006's rule in every codec, to be reimplemented and got wrong.

**Why `Null` is not intercepted.**
A codec is a pair, so whatever kind it **emits** it must **accept**.
ADR-0005's registered `net.Addr` codec returns `Null` for a nil interface and takes `Null` back, which is the mechanism that makes an interface expressible at all.
Core intercepting `Null` would break that.
A codec that does not want `Null` refuses it loudly through the ordinary wrong-kind path, which is what the text arm does.
This is checkable in the harness through the golden column rather than stated as prose.

### Omission is decided before the chain runs

ADR-0006 decided that omission is evaluated against the Go value before anything converts it.
This ADR confirms the order from the other end, and the measurement is the reason there was never a second option:

| type | Go zero? | what the zero value encodes to |
| --- | --- | --- |
| `time.Time` | yes | `string("0001-01-01T00:00:00Z")` |
| `time.Duration` | yes | `string("0s")` |
| `netip.Addr` | yes | `string("")` |
| `int` | yes | `number("0")` |
| a deliberately-set value whose text is empty | **no** | `string("")` |

"Zero in Go" and "empty on the plane" disagree in **both** directions.
An omit-if-the-encoded-form-is-empty rule would never omit a zero `time.Time`, and would drop a value the user explicitly set.

> The composed order is: [#11](https://github.com/onhotpath/ferry/issues/11)'s tag decides omission, [#8](https://github.com/onhotpath/ferry/issues/8)'s default rule decides the value, and this ADR's chain converts whatever survives.
> The chain is invoked for a zero value exactly when the field is dumped at all, and is never the thing that decides whether it is.

That is `omitzero`'s shape in `encoding/json/v2`, where the same split is documented: `omitzero` is evaluated before the marshaler runs, and `omitempty` "may require marshalling then unwriting".
ferry does not need the second half, because ADR-0005 already rejected `omitempty` on the grounds that there is no empty JSON object on a Consul plane.

**A codec must therefore be total over its type including the zero value**, because an unset field is the value the chain sees most often.
That extends ADR-0005's existing requirement - every core entry's value list carries its zero - to every registered codec and to every chain-admitted type.

### A codec takes no context

> No codec signature carries a `context.Context`, and neither does any recognised interface.

This departs from 5.9's implied fix and the reason is stated rather than assumed.

**Cost is not the argument.**
Measured at 1.00 ns and 0 allocations either way.

**The argument is that a context would make cancellability a function of precedence.**
No recognised interface takes one: `MarshalText`, `UnmarshalText`, `MarshalJSON` and xload's own `Decode(string) error` are all context-free.
A ferry-declared context-carrying arm would be the only one, so whether a given type's conversion is cancellable would depend on which arm claimed it, and would flip when the list order changed.
A lifecycle property decided by list order is not a property.

**And the use case a context would serve belongs somewhere else.**
The honest one is a codec that does I/O: decrypting through a KMS, resolving a reference.
ADR-0004 already has that seam and it is a `Source`: `Get` is context-carrying, and a decrypting source wraps another source in about ten lines, which is one of the combinators ADR-0004 already names.
Putting the I/O in the codec instead breaks three things ADR-0005 relies on: a codec collapses a type to a leaf and a leaf is a pure conversion the harness runs with no plane in sight; the round-trip proof is an offline table and an I/O codec has no proof anybody can write; and [#20](https://github.com/onhotpath/ferry/issues/20) would inherit a walk whose leaves may block indefinitely.

**ADR-0004 set the precedent for reading an absent context as a statement.**
`Bind` takes none, which is how the type says no I/O happens there, and it made the rule assertable in the conformance suite rather than merely stated.
The same instrument applies here, and the codec harness gets the same kind of case.

**Cancellation is not lost.**
Core checks the context between leaves, so a walk is cancellable at leaf granularity and a codec never has to care.
Whether it does so per leaf, per subtree, or not at all is [#20](https://github.com/onhotpath/ferry/issues/20)'s; that the codec signature does not need to carry it is this ADR's.

Options visibility, which the survey raises alongside this ("ferry should decide deliberately whether a user codec can see the load options"), is answered by the same reasoning and stated so it is not read as an omission: a codec cannot see call options, because a codec whose output depends on the call has a different representation per call and no golden row.

### A chain-admitted type may key a map, and core cannot check the obligation

ADR-0005 restricts a map key to `string`, the integer kinds, and a registered codec whose form is a `String`, and puts injectivity on the registrant.
A chain arm is a codec nobody registered, so the obligation needs a home.

> ~~A type the chain claims with declared kind `String` may key a map, on the same terms as a registered codec.~~
>
> **Reversed under [#45](https://github.com/onhotpath/ferry/issues/45). A type the chain claims may not key a map. Keying a map is registration-only.**

The key lookup must consult the same chain the leaf lookup does.
Measured: without that one line, `map[netip.Addr]string` is refused as a key type while `netip.Addr` is admitted as a leaf - two lookups answering the same question differently, which is exactly what ADR-0005's identity-before-kind rule exists to prevent.
With it, `map[netip.Addr]string` compiles to `/V/*`, dumps `/V/10.0.0.1` and `/V/10.0.0.2`, and loads back exactly.

**And the injectivity obligation is not hypothetical, which is the second defect this prototype found.**
See [Three defects found](#three-defects-found-by-running-the-decisions).

Injectivity is not checkable in general, so the position this ADR takes is the only honest one:

> Core ships as key types only those it has proved injective.
> Everything else - a chain-admitted type or a registered codec - is a proof obligation on whoever supplied it, discharged in the same harness.

#### The reversal, and why it is this ADR's sentence that goes

**"On the same terms" was the mistake, and it was this ADR's to make.**
[ADR-0009](0009-typed-codec-registration.md) landed after this one and made the terms for a registration an explicit opt-in - `StringCodec(...).AsMapKey()`, with a `map[T]V` whose key is registered without it a schema compile error - and it said why in a sentence this ADR should have been read against: *"the diagnostic is where the obligation gets communicated, which is the point: it is the only moment a registrant is guaranteed to read"*.

A chain arm has no such moment.
So the two rules composed into an outcome neither ADR wrote down:

| what the author does | may it key a map | who was asked about injectivity |
| --- | --- | --- |
| registers a codec, no `.AsMapKey()` | **no**, schema compile error | the registrant, loudly |
| registers a codec with `.AsMapKey()` | yes | the registrant, loudly |
| **registers nothing at all** | **yes**, via this ADR's text arm | **nobody** |

**The refusal is lifted by deleting a line rather than by adding one**, which makes this the only place in ferry where registering a type leaves it *less* usable than not registering it.
Measured: the same three registrations at a **leaf** all compile, so the inversion is specific to the key position.

**What this reversal is not.** It is not a claim that the chain's text is lossy.
Measured against adversarial value lists, every type the chain claims from the stdlib is injective, including the two that look like traps: a 4-in-6 address and a zoned one both keep their distinction in the text.

```
netip.Addr      5 distinct values -> 5 distinct texts  injective=true
netip.Prefix    3 -> 3   injective=true      netip.AddrPort  2 -> 2   injective=true
```

The chain is reversed **because nobody can be asked**, not because the answer would be no.
That distinction matters for the diagnostic, which has to name a type the author never mentioned and must therefore not accuse it of anything.

**What it costs when the answer *is* no**, on the shape a user actually arrives in - a case-insensitive identifier whose `MarshalText` was written for logs:

```
Compile[struct{ M map[YID]int }] -> <nil>
the Go map holds 3 keys;  ferry dumps 1 address, err=<nil>
over 200 dumps of the same value: 3 distinct outcomes, 152/200 · 28/200 · 20/200
```

Two entries dropped, no error, and the winner decided by map iteration order.
That is both halves of [ADR-0001](0001-what-ferry-supports.md) broken in one dump - nothing may be ignored silently, and no map iteration may reach a user-visible artefact - and it is the same failure ADR-0009 measured and refused for the registered case, with nobody warned.

**The deciding argument is this ADR's own harness section, three headings down.**
It already records that the text arm "admits an unbounded set with no proof and no completeness check", because the set of types implementing `encoding.TextMarshaler` is not enumerable and cannot be.
At a leaf that costs fidelity for one value, which this ADR accepted with its eyes open.
At a **key** it costs a whole map entry, silently, and ADR-0001 has no route to a guarantee: core's table does not cover the type, and there is no registration to carry the proof.
Admitting the key admits a member whose proof cannot be written, which is ADR-0005's own phrase for what it refuses.

**What the reversal costs, sized rather than asserted.**
One position out of five.
A chain-claimed type is still admitted at a leaf, as a slice element, behind a pointer and as a map **value**; only the key position moves.
The remedy is one line, and it is the line ADR-0009 already asks for.

The diagnostic, which is where ADR-0009 says the obligation lives:

```
ferry: /m: netip.Addr may not key a map: ferry claims it through its text pair
rather than through a registration, so nobody has declared its text injective
over the key type, and two keys that render alike collapse into one address;
register a codec and mark it usable as a key with
ferry.TextCodec[netip.Addr](ferry.VString).AsMapKey()
```

> **Corrected: the remedy the shipped diagnostic prints is `ferry.StringText[netip.Addr]().AsMapKey()`.**
>
> The block above quotes it as `ferry.TextCodec[netip.Addr](ferry.VString).AsMapKey()`, which is the registration surface [ADR-0009](0009-typed-codec-registration.md) published and [ADR-0017](0017-the-registration-api-and-the-value-it-builds.md) replaced: the kind stopped being an argument and became the constructor's name, so there is no `TextCodec` and no kind to pass.
> ADR-0009 prints the same remedy in the same stale spelling, and its own correction says so.
> **The diagnostic's job is unchanged**, and it is this section's whole point: a chain-claimed type at a map key is refused, and the message names the one line that fixes it.
**One thing this reversal makes worth doing.**
Two Go keys collapsing into one address is detectable **exactly** at dump time, with no registrant, no value list and no injectivity proof, because the walk already renders every key to text and sorts by it - so the check is an adjacent-pair scan over a list that is already built.
It was rejected as an *answer* to #45 because it is a runtime refusal where ADR-0005's framing is schema-compile, and because it cannot see the Load side at all.
It is worth keeping as a second line of defence, and under this reversal the only types that can reach it are core's own and ones a registrant declared with `.AsMapKey()` - so it becomes the check that catches a **wrong** `.AsMapKey()`, which ADR-0009 currently leaves entirely to the registrant's own tests.
It is now implemented, on `proto/tip`.

> **Corrected: this paragraph published a number that was measured on the wrong thing.**
> As merged it read "**0.55% to 0.64%** of work the walk already does, with zero extra allocations".
> That was benchmarked against a standalone helper scanning a slice **the benchmark had already built** - so the allocation under test was in the part the fixture did not run.
> Measured through the shipped call site instead, the first implementation cost **one extra allocation and `len(keys)*24` bytes per map**, because it built an intermediate slice the caller then copied.
>
> | keys | before the check | first implementation | as merged |
> | ---: | --- | --- | --- |
> | 8 | 5197 ns, 768 B, 10 allocs | 1749 ns, 1088 B, **11** | 1589 ns, 768 B, **10** |
> | 512 | 1106224 ns, 54528 B, 514 | 159408 ns, 76288 B, **515** | 164429 ns, 54528 B, **514** |
>
> The **speedup is real and was never in doubt** - rendering each key once replaces a comparator that re-rendered both sides on every comparison, and that is 3x at 8 keys and ~7x at 512 either way.
> What was wrong was "zero extra allocations", and a second implementation arriving from [#31](https://github.com/onhotpath/ferry/issues/31) is what showed the cost was avoidable: it builds the member list in place. That is what shipped.
>
> This is worth recording rather than quietly editing, because it is the failure mode [#41](https://github.com/onhotpath/ferry/issues/41)'s own summary names - **a probe whose fixture could not have failed** - committed inside the amendment that records it. Treat every "costs nothing" in this repository as owing a measurement at the call site rather than at the helper.

> **Amended under [#58](https://github.com/onhotpath/ferry/issues/58): the check was implemented and, through `Dump`, unreachable.**
>
> The paragraph says this check "becomes the check that catches a **wrong** `.AsMapKey()`", and that it is now implemented.
> The first half was not true through the entry point a caller reaches, and the reason is not in this check at all.
>
> The check compares the text the walk renders each key to.
> That text was **not** the registrant's codec: `validMapKey` resolved the key type at compile with the caller's registry installed, and the walk re-derived the text at walk time against a package-level registry that `SinkBinding.Dump` and `Binding.LoadOver` do not install.
> With nothing installed the cascade fell through to `fmt.Sprintf("%v", …)`, which is **accidentally injective over a struct** where the registrant's codec is not - so two keys the codec collapses rendered distinct, and nothing fired.
> It fired correctly through `dumpTo`, a probe helper that does install.
> So every measurement of it was honest and the guarantee a caller got was not the one measured.
>
> Worse than a wrong ratio: `%v` consults `fmt.Stringer`, which [ADR-0005](0005-the-supported-type-set.md) refuses **outright and by name**, for two measured reasons that apply at the key position unchanged.
> And the dump succeeded, so the failure landed at Load against a plane already written - this ADR's own named failure shape, reintroduced at the one position the pair rule does not reach.
>
> **The fix is not in this check.**
> #58 resolved the key's `{text, parse}` pair into the compiled node, alongside the leaf codec [ADR-0009](0009-typed-codec-registration.md) already resolves there, and made one function answer both admission and rendering.
> The three disjoint arms named in the next paragraph are unchanged; what changed is that each now hands back the pair, so the walk consults no registry for a key.
>
> **The ratio in the table above moves again, and the reason is the denominator.**
> The scan is unchanged - 35 ns, 262 ns and 2192 ns over 8, 64 and 512 keys, against 34, 266 and 2063 before, with zero allocations either way.
> Rendering a key no longer runs an identity lookup and a chain probe per key, so the render-and-sort it rides on fell from 4881 ns and 42 allocations to 2358 ns and 28 allocations over 8 keys, and proportionally at 64 and 512.
> The scan is therefore **1.14% to 1.48%** of a smaller number where it was 0.57% to 0.70% of a larger one.
> The paragraph's argument - that the check is free because the walk already did the work - is unweakened, and the correction above stands: it is a measurement at the call site, and the call site got cheaper.
>
> Evidence: `Y45=6` and `TestDumpCollapseCheckFiresThroughEntryPoint` on [`proto/58-mapkey`](https://github.com/onhotpath/ferry/tree/proto/58-mapkey).

**And [#31](https://github.com/onhotpath/ferry/issues/31) turned out not to be untouched after all.**
As merged this section said `map[time.Time]string` collapses on core's **own** pre-seeded entry, which ADR-0009's opt-in "deliberately does not reach", and that this reversal changes nothing there.
The first half stands and the second no longer does.
#31 landed a per-entry key declaration on core's table, and the two rules plus ADR-0009's compose as three **disjoint** arms over three populations - core's own entries, a registration, and a type only the chain claims - with the disjointness enforced by `Register`'s refusal to overwrite core's pinned set rather than by convention.
After all three, there is no route to a map key nobody vouched for.

Evidence: `Y45=all` on [`proto/tip`](https://github.com/onhotpath/ferry/tree/proto/tip).

### What the chain costs the harness

This is the weakest part of the ADR and it is stated as such.

ADR-0005's completeness check iterates the identity table and the admitted kind list and asserts that every member has a proof, so extending the set without extending the table fails CI.
Both of those are enumerable.
**The set of types implementing `encoding.TextMarshaler` is not**, and cannot be: `reflect` has no "all types implementing this interface" query, and could not have one, because the set depends on every package the consumer imports.

So the text arm admits an unbounded set with no proof and no completeness check.
What that means concretely, measured on the zero value of every chain-admitted type in the fixtures:

```
netip.Addr      string("")                       loads back the same
netip.AddrPort  string("")                       loads back the same
netip.Prefix    string("")                       loads back the same
big.Int         string("0")                      loads back the same
language.Tag    string("und")                    loads back the same
uuid.UUID       string("00000000-0000-...")      loads back the same
regexp.Regexp   string("")                       DIFFERS under DeepEqual
```

`regexp.Regexp`'s zero encodes to `""` and decodes to a compiled empty pattern, which is not the zero struct.
Under a relation comparing `String()` it round-trips; under `DeepEqual` it does not.
**ferry cannot say which is correct, because ADR-0005 puts the relation in a proof and nobody wrote one.**

Three mitigations, none of which closes it:

- `ferrytest` ships a helper that discharges a proof for a text-pair type, so a user who cares can write three lines and get the same triple a registrant gets.
- The documentation of the type set names the class, alongside ADR-0005's existing obligation to name category 3.
- Core's own set is unaffected, and that is checked rather than assumed: with the chain on, core's proofs pass **11/11 scalars and 10/10 composites** on the memory plane, because identity is consulted before the chain and no kind-admitted core type carries a text pair.

The chain is therefore **additive for the guaranteed set** and **unproven outside it**, and that is the same shape ADR-0001 already accepted for registration - with the one difference that registration has a call site where a proof can be asked for.

### What this hands [#19](https://github.com/onhotpath/ferry/issues/19)

#19 is blocked on this ticket and owns the registration API, so this is the interface between them, stated as obligations rather than suggestions.
ADR-0005 already gave #19 two, and both survive this chain unchanged: a codec collapses a type to a leaf, and a key codec's text must be injective.
Five more:

- **A registration is an entry in the same identity table the chain consults first.**
  There is no separate registry and no separate precedence question.
- **A registration for a type already in the table is a loud error, not an override.**
  That includes core's own entries, which are pre-seeded.
- **A codec declares the boundary `Value` kind it produces**, and core donates `String` to that kind before calling it.
  This is the single most consequential thing #19 inherits, because getting it wrong makes a codec that works on YAML and fails on env.
- **A codec is a pair, is total over its type including the zero value, and accepts every kind it emits.**
  All three are checkable through ADR-0005's triple.
- **A codec takes no `context.Context` and cannot see call options.**
  If #19 concludes otherwise, that is an amendment to this ADR argued explicitly, and the arm-dependence argument above is what it has to answer.

### What #8 decided and this ADR applies

[ADR-0006](0006-defaults-and-zero-values.md) was drafted in parallel and landed first.
Its definitions are applied here rather than re-derived, and the three seams the two tickets share are reconciled rather than assumed.
Every reconciliation below is stated as applying #8's rule rather than as depending on the merge order, and all four statements quoted here were re-checked against ADR-0006 as merged, after its review round.

- **Does a codec run for `Absent` or a zero value?**
  ADR-0006: "`Absent` means ferry does not write to the field. Every other observation, `Null` and the empty string included, is a value the plane holds, and it is applied."
  This ADR's invocation rule is exactly that, and 5.9's "decoders never see an empty input" is repaired: ferry's codec sees `String("")` and never sees `Absent`.
- **Evaluation order of omit-and-default against the encoder.**
  Agreed from both ends, and stated in [Omission is decided before the chain runs](#omission-is-decided-before-the-chain-runs).
- **Does a codec see the raw boundary `Value` or the donated one?**
  This ADR's, and the answer is the donated one.
  ADR-0006 assumed nothing about it, correctly.

**And ADR-0006's final round leans on this ADR's invocation rule, which is worth recording because it makes the two load-bearing on each other.**
ADR-0006 refuses `Null` at every leaf whose Go type has no null, and gives as its argument that "strictness can be relaxed per type by a registered codec, and leniency cannot be tightened at all, because the zeroing would precede the codec chain".
That argument only holds because `Null` reaches the codec rather than being intercepted by core, which is this ADR's rule and the reason it is stated as "whatever kind a codec emits it must accept".
Had this ADR intercepted `Null`, ADR-0006's escape hatch would not exist and its choice between refusing and zeroing would have been forced the other way.

One thing ADR-0006 states in a sentence and had no codec to exercise it against, now measured: a declared default is a `String` `Value` at the field's address, so a chain-admitted or registered codec gets defaults for free with no default-awareness of its own.
Measured, a struct of `netip.Addr` and `big.Int` with declared defaults, against an empty plane and against a plane supplying one of them:

```
plane is empty (all Absent)  -> addr=10.0.0.1  n=1099511627776
plane supplies addr          -> addr=192.0.2.1 n=1099511627776
```

**And one sharp edge that falls out of the two rules meeting**, which is neither ADR's alone and is named rather than left:
a default is `String` by construction, and `String` donates to `Bytes` as a relabel per ADR-0005's `[]byte` rule.
So `default=aGk=` on a `[]byte` field lands as the four bytes `aGk=` and **not** as the decoded `hi`.
Measured.
That belongs in [#11](https://github.com/onhotpath/ferry/issues/11)'s documentation of how a default is written, and it is not a case for a second coercion.

### Three defects found by running the decisions

All three were found by running rather than by reading, and all three are fixed on the prototype branch only.
The third is this ADR's own rule not being implemented where the rule says it lives, which is the more instructive kind.

**A pointer type can satisfy an arm in its own right, and that bypasses ADR-0005's nil-pointer rule.**
`*big.Int` implements the whole text pair, because `big.Int`'s text methods are on the pointer receiver.
With the chain consulted before the pointer shape, a `*big.Int` field became a leaf, ADR-0005's "a nil pointer writes `Null` at its own address" never ran, and:

```
dump  nil *big.Int  ->  /B = string("<nil>")
load  it back       ->  SIGSEGV inside math/big.(*Int).UnmarshalText on a nil receiver
```

A wrong value on the way out and a crash on the way in, from one omitted line.
The rule this produced is in the chain above: **pointer indirection is structural and is resolved before the chain is asked anything.**
The chain is asked about `T`, and a pointer-receiver method is reached by taking `T`'s address.

**`map[time.Time]string` collapses two distinct keys into one address, today.**
`validMapKey` admits anything in the identity table, and `time.Time`'s RFC 3339 text is not injective over the type:

```
a = 2026-01-15 12:00:00 +0000 UTC   Location "UTC"
b = 2026-01-15 12:00:00 +0000 GMT   Location "GMT"
a == b: false        a.Equal(b): true
MarshalText: "2026-01-15T12:00:00Z" and "2026-01-15T12:00:00Z"  identical
Go map holds 2 keys  ->  ferry dumps 1 address
```

Two Go keys, one address, no error.
This is exactly the hazard ADR-0005 states for a **registered** key codec, occurring inside **core's own set**, and no probe in #7 reached it because none used a composite map key.
It is not fixed here, because the fix - dropping `time.Time` from the admissible key set, or attaching a proof obligation to it - amends ADR-0005's decision rather than this one.
**Proposed as a ticket** in the resolution comment.

*(Closed since, under [#31](https://github.com/onhotpath/ferry/issues/31), by the first of the two fixes this paragraph names: `time.Time` is dropped from the admissible key set, and key admissibility became a per-entry declaration on [ADR-0005](0005-the-supported-type-set.md)'s table rather than something inherited from membership of it.
The section further up already records that #31 landed; this paragraph is where the defect is stated, and it is the one that read as open.)*

**The declared kind must live inside the identity-table entry, not beside it.**
This ADR says core applies the donor rule before calling any codec.
The prototype implemented that for a chain codec and not for a table one, because the chain's codec carried its kind and the table's did not, so the kind lived in a second map.
Measured, a registered `big.Int` codec correctly declaring `Number`:

```
typed plane (says Number)          -> loads
flat plane (says String)           -> value: wrong kind
```

The codec was right and the lookup was wrong, and the failure appeared only on env, query parameters and Consul, which is the same shape as ADR-0005's G2.
The rule this produced: **the kind is a field of the codec, resolved by the same lookup that finds the codec.**
Two lookups for one decision is how a chain drifts, which is the third time that pattern has bitten in this prototype.

### What this ADR does not decide

- **How omission and defaults are spelled**: [#11](https://github.com/onhotpath/ferry/issues/11).
  This ADR fixes only where the chain sits relative to them.
- **The registration API**: [#19](https://github.com/onhotpath/ferry/issues/19), with the five obligations above.
- **The error types every refusal here produces**: [#9](https://github.com/onhotpath/ferry/issues/9)'s convention, applied rather than invented.
  This ADR uses joined and sorted reports per ADR-0001's determinism invariant, and defers the types.
- **Whether the walk checks the context per leaf, per subtree, or not at all**: [#20](https://github.com/onhotpath/ferry/issues/20).
  This ADR decides only that the codec signature does not need to carry one.
- **Where the compiled schema caches the per-type claim**: [#16](https://github.com/onhotpath/ferry/issues/16).
  The claim is a property of `reflect.TypeFor[T]()` alone, so it belongs in the compiled schema and is computed once; where that lives is #16's.
- **Whether a root leaf is a legal address.**
  A chain-admitted type at the root mints the empty path, which ADR-0003 says an address may not be.
  This is pre-existing - a root `int` does the same under ADR-0005 - and belongs to [#16](https://github.com/onhotpath/ferry/issues/16)'s entry point, but the chain enlarges the set of types that can sit there, so it is named.
- **Whether a JSON driver ships, and what it passes to `encoding/json/v2`**: ADR-0004's driver list and ADR-0005's pinned option set.
  This ADR removes one reason such a module might have existed, and creates none.

## Consequences

- ferry's chain is three steps where xload's is five arms and json/v2's is five, and every removal is backed by a census rather than by taste.
  The admission test for a future arm is stated, so the list can grow without the reasoning being reinvented.
- **ferry adds no interface to the world.**
  A type works with ferry by implementing the pair `encoding` already defines, or by being registered.
  The cost is that a type wanting a non-`String` boundary kind cannot say so by implementing methods, and needs a registration call.
- Selecting a pair rather than two directions makes an asymmetric type a compile error instead of a value that dumps and never loads.
  It also means ferry is stricter than `encoding/json/v2`, which has the same hazard and handles it with prose.
- **Running the chain before kind shortens ADR-0005's refusal list by seven types and shrinks its category 3.**
  `net.IP`, `uuid.UUID` and `slog.Level` become legible on a plane.
  The price is `net.IP`'s two byte encodings collapsing into one - a loss under `==` and not under `net.IP.Equal` - and a `MarshalText` that is not an inverse breaking a type kind admission would have carried.
  ADR-0005's `net.IP` row stays true for `net.ParseIP`'s output and is qualified for a hand-built four-byte value.
- A type's address set now depends on the chain, so a struct carrying a text pair contributes one address rather than one per field.
  Template generation, the driver-side injectivity check and every stored artefact see that, and it is a property of the type's own methods rather than of its shape.
- **The text arm admits an unbounded, unenumerable set that ADR-0005's completeness check structurally cannot cover.**
  `regexp.Regexp`'s zero value is the worked example: ferry cannot say whether it round-trips, because the relation lives in a proof nobody wrote.
  This is the weakest part of this ADR, and the mitigations are a `ferrytest` helper and documentation, neither of which closes it.
- Core's own guarantee is unchanged, and that is measured rather than argued: 11/11 scalars and 10/10 composites with the chain on.
- A codec declaring its boundary kind is the single most consequential thing handed to #19, because a codec that declares the wrong one works on YAML and fails on env, or the reverse, and only the golden column catches it.
- **Rejecting `MarshalerTo` on its own merits means ADR-0002's stated cost was not a cost.**
  That is worth recording, because ADR-0002 lists "core cannot recognise `MarshalerTo`" in its consequences as a real constraint, and the measurement says the constraint bound nothing.
  It also means the `nojsonv2` escape hatch's eventual removal does not reopen this: the reason is the boundary shape, not the import.
- A codec is a pure conversion with no context and no options, which is what keeps the round-trip proof runnable offline and keeps [#20](https://github.com/onhotpath/ferry/issues/20)'s problem bounded.
  The cost is that a codec doing I/O is unrepresentable, and the answer is a wrapping `Source`, which nobody has written yet.
- Three defects were found by running the decisions rather than reading them: a pointer type satisfying an arm in its own right, a non-injective key type inside core's own set, and this ADR's own donor rule applied at one of the two lookups that needed it.
  The second is unfixed here, and it means ADR-0005's admissible key set is wrong today.
  The third is the reason the declared kind is specified as a field of the codec rather than as a parallel table.
- **Both chain orders drift under an unrelated edit**, and the ADR says so rather than claiming the property for one.
  Before-kind drifts when a dependency adds a text pair; after-kind drifts when someone exports a field.
  The case for before-kind rests on the first being a visibly serialization-shaped edit and the second not being one, which is a weaker claim than an earlier draft made.

## Items from the xload survey

The survey is [`docs/research/generics-and-modern-go.md`](../research/generics-and-modern-go.md), section 5, against `github.com/gojekfarm/xtools` at [`a90b3aa`](https://github.com/gojekfarm/xtools/commit/a90b3aad2133248cec50f6b4d6e37b0d9e788adb).

**5.9, the decoder chain is fixed, one-directional and context-free, is this ADR's outright.**
It has five bullets and ADR-0005 killed two halves; all five are answered here.

- *No `Encode` counterpart at all.*
  Addressed, and the fix is structural rather than additive: there is one selection serving both directions, so an encode counterpart cannot drift from its decoder.
  The survey's own note that `xloadtype` grew accidental encoders through `String()` is ADR-0005's, which refused `fmt.Stringer` outright.
  This ADR adds the other half of that: those three types are half pairs, and a half pair does not compile.
- *Precedence is hardcoded and undocumented as a policy, and a type implementing both `json.Unmarshaler` and `BinaryUnmarshaler` gets JSON arbitrarily.*
  Addressed twice over.
  The order is stated as policy in three lines rather than emerging from the order of a type switch, and the arbitrariness the survey names is gone by construction, because neither of those two arms exists.
  The census is why: 10 of 29 types carry more than one complete arm, so an undocumented order is not a theoretical hazard.
- *No way to register a decoder for a type you do not own, and `time.Duration` matched by `Type.String()`.*
  The string comparison is ADR-0005's and is dead.
  The registration half is [#19](https://github.com/onhotpath/ferry/issues/19)'s, and this ADR is what unblocks it, handing it five obligations and a settled precedence chain to design against.
- *`Decode(string) error` takes no `context.Context` though the whole walk is context-carrying.*
  **Deliberately not adopted**, which is the one place this ADR declines a survey item's implied fix.
  The reason is measured and stated: no recognised interface takes one, so a ferry-declared context-carrying arm would make cancellability depend on which arm claimed the type; the I/O use case belongs in a `Source`, which already has a context; and ADR-0004 set the precedent for an absent context being a load-bearing statement.
- *Decoders never see an empty input.*
  Addressed, and the repair was paid for by ADR-0004 rather than here.
  Reproduced: xload's decoder is handed neither the empty value nor the missing key and cannot tell them apart; ferry's is handed `String("")` and never `Absent`.

**5.10, composite values are string-splitting**, has a half that ADR-0003 explicitly left here: "a plane may hold a whole list in one value, and a codec may split it, and that codec's lossiness is then the user's decision."
Confirmed and unchanged.
A flat plane holding `TAGS=a,b,c` reports one `String`, and splitting it is a registered codec's job with the registrant carrying the round-trip proof.
No arm in this chain does it, and no arm should: a splitting codec is not an inverse pair unless it escapes the delimiter, which is 5.10's own defect.

**5.14** was enumerated rather than assumed, all four items.

- *Two ways to set the loader.*
  Bears on nothing here.
  ADR-0004 avoided it by construction, and this ADR adds no second way to supply a codec: registration and the text pair are not two routes to the same thing, they are one table and one interface, consulted in a stated order.
- *The `CanAddr` loop that can only run once.*
  Bears on this ADR directly, because the chain is where addressability is actually decided.
  xload's `for field.CanAddr() { field = field.Addr() }` is written as a loop and can only execute once.
  This ADR replaces the instinct behind it with a stated rule: the encode half takes the address when the method is on the pointer receiver and copies when the value is not addressable, which is measured against a map value; the decode half requires `*T` and the walk always decodes into a fresh addressable destination.
- *The non-deterministic select on a cancelled context.*
  Concurrency, and [#20](https://github.com/onhotpath/ferry/issues/20)'s.
  This ADR bears on it only by keeping codecs free of I/O and of context, which makes #20's problem smaller rather than larger.
- *Value receivers on `Error()` where pointers are returned.*
  Bears on this ADR twice.
  The error types it produces are deferred to [#9](https://github.com/onhotpath/ferry/issues/9)'s convention rather than pre-empted, as ADR-0003, ADR-0004 and ADR-0005 all did.
  And its underlying cause, that a method set differs between a value and its pointer, is a first-class concern of every arm here: `big.Int` does not implement `TextMarshaler` and `*big.Int` does, a value-receiver `UnmarshalText` writes to a copy, and a pointer type satisfying an arm in its own right is one of the two defects this prototype found.

**5.5, nondeterministic error output**, is [#9](https://github.com/onhotpath/ferry/issues/9)'s, and this ADR applies it rather than deciding it: the half-pair report is collected, joined and sorted.

**5.7, `reflect.DeepEqual` as a probe**, is [#8](https://github.com/onhotpath/ferry/issues/8)'s and ADR-0005's.
It surfaces here once, and usefully: `regexp.Regexp`'s zero round-trips under a `String()` relation and not under `DeepEqual`, which is the same instinct arriving at a type ferry has no proof for.

The remaining items are unaffected by this ADR.
