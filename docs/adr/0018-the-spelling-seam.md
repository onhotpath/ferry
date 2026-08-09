# 18. The spelling seam: how a plane says a payload its own way

Status: Accepted
Date: 2026-08-06
Ticket: [#259](https://github.com/onhotpath/ferry/issues/259)

## Context

[ADR-0004](0004-source-and-sink.md) put a typed boundary between a plane and ferry, and [ADR-0017](0017-the-registration-api-and-the-value-it-builds.md) made the `Value` crossing it carry a payload rather than a spelling.
Neither says who turns `on` into `true`, `0x1F` into `31`, or a `!!binary` node into bytes.

Core did, and core is the one party that cannot know.

- [#259](https://github.com/onhotpath/ferry/issues/259): core's base-10 number parser is applied to a YAML plane, so `.inf`, `0x1F` and `1_000` are refused although YAML defines all three, and a dump writes `+Inf`, which no YAML reader accepts.
- A `bool` on a plane that spells it `on` and `off` cannot load at all, because the donation path hands `on` to `strconv.ParseBool`.
- `[]byte` has exactly one rendering per driver, hard-coded, invisible and unswappable.
- [#230](https://github.com/onhotpath/ferry/issues/230): a chain-claimed key type falls back to `reflect.Kind` at the map-key position, so `/m/WARN` and a `WARN` leaf can disagree for one type.

Each is the same fact: **a spelling is a property of a plane, and core is not a plane.**

This ADR is written from the prototype on branch [`proto/02b-value-seam`](https://github.com/onhotpath/ferry/tree/proto/02b-value-seam).
Every number below is from it or from the benchmark it carries.

## Decision

### Two two-method interfaces, and core exports no spelling of its own

> ```go
> type Spelling[T, C any] interface {
>     Parse(c C) (T, error)   // carrier -> payload
>     Render(v T) (C, error)  // payload -> carrier
> }
>
> type Transform[T any] interface {
>     Apply(v T) (T, error)   // payload -> payload, on the way out
>     Invert(v T) (T, error)  // payload -> payload, on the way in
> }
>
> func With[T, C any](s Spelling[T, C], ts ...Transform[T]) Spelling[T, C]
> ```
>
> Core ships the two contracts and the composition, and **no named spelling at all**.
> Drivers compose.

**`C` is the carrier, and it is a type parameter because it is not always text.**
An earlier draft locked the carrier to `string`, which is wrong for the KV plane: a bytes store hands a driver raw `[]byte`, and forcing that through a string is the lossy conversion this whole seam exists to remove.
`C` is `string` for env and HTTP, `[]byte` for kv, and whatever a future binary plane carries.

**`Transform` is fallible in both directions, and the outbound half is the one people miss.**
`Apply` is not total.
A query parameter has a size budget, a charset has a domain, a canonical form is a requirement, and all three refuse on the way **out**.
Those refusals land in the pre-write encode phase, which is where a knowable-before-I/O failure already belongs.
`Invert` refuses corrupt plane data: a gzip transform's `Invert` fails on a truncated stream rather than returning garbage.

**Two interfaces rather than two structs**, and the deciding ground is that every export is a contact point maintained forever.
A struct needs a constructor to be safe, the constructor needs both halves to be non-nil, and the pair is then a third exported name.
Two methods need none of it.

**Performance is not the axis, and it was measured before the argument was made.**
Six runs each, mean, on go1.26.5:

| | struct with a constructor | two-method interface | allocs |
| --- | --- | --- | --- |
| `Parse`, map-table hit | 10.9 ns/op | 11.2 ns/op | 0 / 0 |
| `Render` | 2.53 ns/op | 2.43 ns/op | 0 / 0 |
| across a no-inline seam, then `Parse` | 12.0 ns/op | 12.4 ns/op | 0 / 0 |
| construction, once per declared spelling | 140 ns, 4 allocs | 125 ns, 3 allocs | off the hot path |

Every difference is at or under 0.5 ns/op, which is inside run-to-run noise on this machine.

**`ParseFunc` and `RenderFunc` stay, as documentation types**, and `SpellingFunc` adapts a pair of them to the interface:

```go
type ParseFunc[C, T any]  func(c C) (T, error)
type RenderFunc[T, C any] func(v T) (C, error)

func SpellingFunc[T, C any](p ParseFunc[C, T], r RenderFunc[T, C]) Spelling[T, C]
```

That is `http.Handler` and `http.HandlerFunc`, completed: the interface is the contract, the named func types are what a signature reads as, and the adapter is the escape hatch for a driver that has two closures and no reason to declare a type.

**`With` is variadic and a free function**, not a method.
A method would have to be on the interface, so every implementation would provide it and core would ship an embeddable base to spare them.
Nested calls read worse than a pipeline:

```go
ferry.With(ferryhttp.Base64(), ferryhttp.Gzip(), ferryhttp.MaxSize(4<<10))
```

Render is `spell(apply(v))` and parse is `invert(unspell(c))`, so the written order reads as a nesting: the step written **last** is closest to the payload and runs **first** on the way out, and the way in undoes them in the reverse of that.
The composed value is an unexported type, which keeps it off the surface.

> **Amended under [#259](https://github.com/onhotpath/ferry/issues/259): the ordering sentence above is rewritten, because as published it contradicted this ADR's own worked case.**
>
> It read "the transforms compose in written order on the way out and in reverse on the way in", which says the step written *first* runs first outbound.
> Three lines below, the worked case says `With(Base64(), Gzip(), MaxSize)` dumps as "cap, gzip, base64", which is the step written *last* running first, and the prototype this ADR is written from implements that.
> **The worked case and the prototype are the decision**, and the sentence was the slip: `With(s, t1, t2)` is `s(t1(t2(v)))` outbound, read left to right as outermost to innermost.
> Nothing about the implementation moves.

### A spelling is declared where the plane is, which is a driver Option

> A spelling is a fact about a plane, so it is declared through the driver's own Option set.
> Core's Option set stays closed and gains nothing.

```go
src := env.Source{Options: []env.Option{
    env.BoolWords("on", "off", "true", "false"),
}}
```

**Field-level spelling is deliberately not offered**, because it reopens the tag grammar, and the tag grammar's extension has its own answer in [ADR-0021](0021-the-multi-key-extension-mechanism.md): a per-address form is a **driver-declared** key read at that driver's `Bind`, never a word inside ferry's tag.
So `ferry:"transform=uppercase"` is refused now and stays refused: ferry's namespace is frozen, and the plane-wide seam is here.

### Compositions live per driver until a third driver writes the same one

> `env.BoolWords`, `env.Negated`, `ferryhttp.Base64`, `ferryhttp.Gzip`, `ferryhttp.MaxSize`, `kv.Raw`.
> Nothing is shared, and consolidation waits for a third caller.

> **Amended under [#259](https://github.com/onhotpath/ferry/issues/259): `env.Negated` is struck from the list above, and nothing ships under that name.**
>
> As published the list named six compositions, `env.Negated` among them, carried over from the prototype where it demonstrated a `Transform[bool]` over a negative-polarity variable such as `DISABLE_CACHE`.
> **It is not expressible under this ADR's own rules.**
> A driver Option is plane-wide, so a plane-wide `Negated` inverts every boolean the plane holds, which is meaningless; and the form it actually wants is per-address, which the section above refuses here and [ADR-0021](0021-the-multi-key-extension-mechanism.md) refuses again from the other side.
> So it was a prototype artefact in a list of shipped names, and the list now reads `env.BoolWords`, `ferryhttp.Base64`, `ferryhttp.Gzip`, `ferryhttp.MaxSize`, `kv.Raw`.
> `Transform` itself is unaffected: `Gzip` and `MaxSize` are what it is for, and both are plane-wide facts.
>
> **`env.BoolWords` is plane-wide as written, and that is ratified rather than tolerated.**
> The words decide what a boolean is on a plane that carries no type information, so a variable holding a declared word arrives as a `Bool` wherever it is read and a `string` field over it is then refused; the doc says so, and the answer is to choose words your text values do not use.
> A kind-gated refinement - consulting the schema's kind at the address before applying a spelling - is contingent on the address set exposing a per-address kind, which it does not, and is parked at [#309](https://github.com/onhotpath/ferry/issues/309).
> Nothing here anticipates it.

> **Amended under [#340](https://github.com/onhotpath/ferry/issues/340): the contingency the paragraph above names is satisfied, and the kind-gated refinement shipped.**
>
> As published, the option was plane-wide because there was nowhere to read a per-address kind from, and the paragraph above parked the refinement on exactly that.
> [ADR-0016](0016-the-sealed-address-model.md) now has a typed address carry the kind the schema wants at it, published as `LeafAddr.Wants`, so the contingency is met and the option changed with it.
>
> What moved: the words are consulted where the schema wants a bool and nowhere else.
> A `string` field over `FEATURE=on` loads the text `on` rather than being refused.
>
> **The old remedy is retired.**
> Choosing words your text values do not use is no longer the answer, because there is no longer a collision to avoid inside one schema: a bool field and a string field over two variables holding the same declared word both load, each getting what it asked for.
>
> **The sharp edge is kept and it is a different one.**
> A variable holding a declared word is a boolean where the schema wants one and text where it does not, so two programs reading one environment can read one variable two ways, and which way is the schema's business rather than the environment's.
>
> **The write side is unaffected, and nothing in this ADR's laws moves.**
> A `Value` carries its own kind, so a sink renders a `KindBool` with the words and everything else as text, and there is nothing at a write for an address to decide.
> The seam is still plane-wide in what it declares; what is per-address is only where the declaration lands.

Some duplication between drivers, zero shared surface, and every vocabulary stays honest to its plane.
Core's `require` block stays empty, which is a module rule and not a preference: a shared spelling module would be a fourth thing to version and a place for a plane-untrue rendering to hide.

The rule of three is the trigger, and it is written down so that consolidating later is a decision rather than a discovery.

**Worked, on the four defects above:**

```
env  ENABLED=on         with env.BoolWords("on","off")  ->  Value carries true; dump writes "on"
yaml rate: 0x1F         with yaml's own resolver        ->  Number("31"); dump writes valid yaml
http PEM []byte         With(Base64(), Gzip(), MaxSize) ->  dump: cap, gzip, base64; load: reverse
kv   any []byte         kv.Raw(), C = []byte            ->  no text conversion at any point
```

`/m/WARN` and a `WARN` leaf agree because a map key is the `String` spelling of the key type on this plane: one table, both positions, which is [#230](https://github.com/onhotpath/ferry/issues/230).

### Six laws, five about the spelling and one about the closures

Five are about the spelling itself, and every spelling, shipped or supplied, must satisfy all five.

1. **`parse(render(v)) == v`** for every value of the kind. Round-trip closure.
2. **The write form is always inside the accept set**, and the accept set may be wider. Wider in, canonical out.
3. **`render` is deterministic**: one value, one spelling.
4. **A parse refusal carries the address and the offending text.** Never a zero value, never a guess.
5. **An override changes spelling only, never semantics.** No rendering may turn a `Bool` into a tri-state.

> **Amended under [#259](https://github.com/onhotpath/ferry/issues/259): law 4 is scoped, and the offending text it carries is bounded.**
>
> As published law 4 read "a parse refusal carries the address and the offending text", which [ADR-0011](0011-the-error-model.md) forbids outright: ferry's own message text never contains a value the plane supplied, and that rule is total because ferry cannot know which addresses hold secrets.
> The two were in flat contradiction and the implementation had to pick one.
>
> **Law 4 wins here and only here.** A spelling's parse refusal is the one message whose entire content is the text: `"onn" is not one of this plane's boolean words (on, off)` is actionable and `a value at /enabled is not one of this plane's boolean words` is not, because the operator cannot see which of the words they missed.
> Every other message class keeps ADR-0011's rule unchanged.
>
> **The exception is bounded in both dimensions, which is what stops it becoming the leak the rule exists to prevent.**
> The quoted text is cut to **64 bytes** and escaped to a single printable line.
> 64 is chosen against what the message has to show and what it must never show: a mistyped word or number is far inside it, and the shortest credential shape is already at or over it - an AWS key ID is 20 bytes and its secret 40, a PEM line is 64 before its header, and a JWT is longer than any of them.
> A value that was cut says so.
> Escaping is not decoration: a folded YAML scalar or a variable holding newlines would otherwise print a paragraph where a line was promised.
>
> **Redaction composes rather than being configured.** A plane that must quote nothing wraps its spelling in one whose `Parse` returns its own refusal, which is `Spelling` composing with itself and needs no option, no flag and no second surface.
>
> The bound is the driver's to apply, since core ships no spelling; `driver/env` and `driver/yaml` each implement it, and consolidating waits for the third caller under the rule of three above.

Law 2 with its worked case: `BoolWords("on", "off", "true", "false")` accepts four spellings and always writes `on`.
Because `on` is itself in the accept set, law 1 closes.
A rendering that wrote `yes` while accepting only `on` and `off` writes a spelling it cannot read back, and `ferrytest` refuses it.

> **Law 6, which is a law about the closures rather than about the spelling: they must be pure functions.**

The docs state it as a contract and `ferrytest` enforces it, with a round-trip proof and a purity probe: the same input twice, the same output, no observable state.
A closure that consults outside state breaks round-trip testability, which is what laws 1 and 3 exist to protect.

**And the honest limit of the fence, conceded rather than claimed.**
A retained handle to mutable state defeats any of this, and **no construction shape closes the hole**.
A func value in Go is a reference to arbitrary state:

```go
a  := &priv{table: ...}
sp := ferry.SpellingFunc(a.parse, a.render)   // method values capture a
a.table = nil                                 // the bound plane's behaviour just changed
```

The same happens through an interface implementation, and through a struct-with-constructor, which is why the constructor form was not preferred on safety grounds.
**What actually fences it is an idiom rather than a type**: a driver's constructors take **data, not functions**, so `BoolWords("on", "off")` builds its closures over locals it owns and the paved road has no handle to retain.
The raw adapter is the escape hatch, bound by the purity law and the `ferrytest` probe.
That is the whole safety story, and it is stated rather than sold.

### The lean boundary: `Value` carries semantics, and the driver remembers the rest

`0x1F` arrives as `Number("31")`, canonical.
The `0x` spelling is plane knowledge, and the plane is not core.

> A driver that needs the original spelling back memoises it **address-keyed, on demand**, in its own state.
> `Value` never carries it.

That keeps `Value` at 24 bytes and comparable, which the whole conformance harness rests on, and it puts the cost on the one driver that wants the property rather than on every plane.
The prototype demonstrates it end to end: the driver's memo restores the original spelling on dump for the addresses it saw, and writes the canonical form for the addresses it did not.

The same rule governs [ADR-0016](0016-the-sealed-address-model.md)'s reference divergence, where an unchanged section keeps its link and a diverged one materialises.
Both are one principle: what the plane said is preserved until the value says otherwise, and the preserving is the driver's.

### What this ADR does not decide

- **What the payload types are**: [ADR-0017](0017-the-registration-api-and-the-value-it-builds.md)'s, and this seam sits under it rather than beside it.
- **Which spellings ship in which first-party driver.** That is each driver's, under the rule of three above.
- **Whether a shared spelling module ever exists.** When a third driver writes the same composition, and not before.
- **Whether a per-address form is expressible.** [ADR-0021](0021-the-multi-key-extension-mechanism.md)'s, as a driver-declared tag key, and never as a word in ferry's tag.
- **Whether `ferrytest` can prove purity in general.** It cannot; it probes, and the law is a documented contract with a probe behind it, exactly as ADR-0004's three optional interfaces are.

## Consequences

- **A spelling is a driver Option, and core exports no named spelling**, so core's Option set stays closed and every rendering is declared where the plane is.
  [#259](https://github.com/onhotpath/ferry/issues/259) is fixed by moving the knowledge rather than by widening core's parser: `driver/yaml` owns `Number` through YAML's own resolver, so hand-written YAML loads and dumped YAML is valid YAML.
- **Two two-method interfaces plus one variadic free function is the entire core surface.**
  The struct-with-constructor alternative was measured at parity - every difference at or under 0.5 ns/op at zero allocations - so the choice was made on surface count, which is the only axis that was left.
- **`Transform` refuses in both directions**, which puts a size or domain refusal in the pre-write encode phase where a knowable-before-I/O failure belongs, and a corrupt-data refusal at parse with the address attached.
- **The carrier is a type parameter**, so a bytes plane never passes through a string.
  The cost is a second type parameter in every driver-facing spelling signature, which is the trade [ADR-0004](0004-source-and-sink.md) refused for `OpenFunc[T]` and accepts here because the alternative is lossy rather than merely verbose.
- **Compositions live per driver, and the rule of three is the trigger for consolidating.**
  The cost is duplication between drivers today; the benefit is that no plane-untrue rendering has anywhere to hide and core's `require` block stays empty.
- **Six laws, five about the spelling and one about the closures**, documented as contracts and probed by `ferrytest`.
  Purity cannot be proved and the retained-handle hole cannot be closed by any construction shape; the fence is the idiom that driver constructors take data rather than functions, and this ADR says so instead of claiming a guarantee it does not have.
- **`Value` carries semantics and the driver memoises the spelling**, address-keyed and on demand.
  That is what keeps `Value` 24 bytes and comparable, and it puts the cost on the driver that wants the property.
