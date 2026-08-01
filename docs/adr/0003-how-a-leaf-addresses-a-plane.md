# 3. How a leaf field addresses a plane

Status: Proposed
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

> A compiled schema's address set contains no address that is a prefix of another.
> A path is a prefix of itself, so this subsumes exact duplicates.

Violation is a schema compilation failure listing every clash.
It is checked from `reflect.TypeFor[T]()` alone, with no plane reachable, no value in hand, and identically in both directions, which is the same assertability ADR-0001 claims for tag rejection.
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

### The collision rule, driver side: the key function is injective over the address set

Keeping the address structured does not abolish flattening collisions.
It relocates them to the only place that has the information to resolve them, and it makes the obligation checkable.

> A driver's mapping from ferry address to plane key must be injective over the address set of the schema it was given.
> If it is not, the driver fails before any I/O.

The conformance suite ADR-0002 puts in core checks it, which is what stops it being prose.
This one rule covers separator collisions, case folding, and any normalization a driver invents, because all three are the same failure: a non-injective map out of the address set.

Measured, over four address sets and three key functions:

| Address set | env, uppercase and `_` | env, no fold and `_` | dotted, no fold |
| --- | --- | --- | --- |
| `/DB/HOST`, `/DB_HOST` | rejected | rejected | ok |
| `/myKey`, `/MyKey`, `/MYKEY` | rejected | ok | ok |
| `/db.host`, `/db/host` | ok | ok | rejected |
| `/db/host`, `/db/port`, `/cache/host` | ok | ok | ok |

Two things to read out of that table.
The first row is the collision a flat key space creates and cannot see, appearing as a driver error before any I/O rather than as a silent overwrite.
The second row is viper's measured bug, and it is now an error that names both offending addresses.
No key function is universally right, which is precisely why the rule is stated over the schema rather than over the key function.

**The check needs the whole address set up front, and the compiled schema has it before any I/O.**
That is the same precondition a batch or snapshot source needs, so the collision rule and the capability the survey identifies as the biggest non-generics win in section 5.13 are unlocked by the same fact.

**It is not on the hot path.**
Because the address set is known before I/O, a driver's plane keys are computed once per schema and cached, not recomputed per lookup.
Measured: a precomputed lookup is 10.4 ns and zero allocations against 8.5 ns and zero allocations for a bare flat-map lookup.
Computing the key per call instead costs 109 ns with a segment iterator, or 477 ns with a segment slice, against a 476 ns twelve-key cached load in the survey's own prototype, which is what makes caching it a requirement and not an optimisation.
So the structured address costs roughly two nanoseconds a key against a flat string, and any larger number in this area is an implementation that forgot to precompute.

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
- What the address appears in, and whether a source is queried per key or handed the whole set: [#5](https://github.com/onhotpath/ferry/issues/5).
- Whether any Go type produces an Index segment.
  That is [#7](https://github.com/onhotpath/ferry/issues/7)'s.
  This ADR reserves the kind so that #7's answer is not forced by the address type; if #7 admits no indexed composite, Index segments simply go unused.
- How Load discovers the length of an indexed composite, which needs enumeration ([#5](https://github.com/onhotpath/ferry/issues/5)) or presence ([#8](https://github.com/onhotpath/ferry/issues/8)).
- The error types the two rules produce, which follow [#9](https://github.com/onhotpath/ferry/issues/9)'s convention rather than inventing one.

## Consequences

- Dump-side silent data loss over colliding addresses stops being a runtime hazard and becomes a schema that does not compile.
  It is the strongest guarantee available here, and it is only available because the check needs no plane and no values.
- Every driver carries an obligation it did not carry under a flat key space, and it is a real one.
  The mitigation is that it is about ten lines, it is checked by the conformance suite, and it runs once per schema rather than per key.
- Core is strictly less permissive than a flat key space, in exactly one way: a leaf and a subtree may not share a segment.
  A schema that xload accepted and that no tree plane can represent will now be rejected.
- ferry's address is comparable and its ordering is segment-wise, so both the determinism invariant and the map-key use are satisfied by the same type.
  Sorting the rendering instead is a subtle bug that produces `0 1 10 11 2`, and it will be a conformance-suite case.
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
