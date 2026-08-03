# Auditing the prototype chain against the Accepted ADRs

Findings for [#41](https://github.com/onhotpath/ferry/issues/41), which carries `wayfinder:research`, so this document is the artefact and no ADR is proposed.

Code: branch [`proto/41-audit`](https://github.com/onhotpath/ferry/tree/proto/41-audit), built on `proto/25-binding`, which is the tip of `7 -> 12 -> 19 -> 16 -> 25`.
Run: `A41=<n|all> GOTOOLCHAIN=go1.27rc2 go run .` from `proto/`.
Thirty-one probes, each taking a normative sentence out of an Accepted ADR and running it through the tip's own entry points.
Every number and every quoted output below is from that branch.

Adding the audit file is behaviour-preserving against the four inherited suites: `E16=all`, `P19=all`, `P12=all` and `B25=all` are byte-identical before and after, apart from timings, two column widths and one heap address in [#31](https://github.com/onhotpath/ferry/issues/31)'s known map-key line.

## Summary

The ticket's question is whether the throwaway prototypes still do what the Accepted ADRs say they do.
They do not, and the shape of the gap is not the one the ticket anticipated.

**The chain is two chains, and the tip carries one of them.**
`proto/8-defaults`, `proto/11-tag-grammar` and `proto/9-errors` are a branch line that dead-ends.
Nothing from any of them is on the tip.
ADR-0006, ADR-0008 and ADR-0011 were each measured on a codebase that no later ADR was measured against, and the tip re-implements a subset of each from scratch, in the schema compiler `proto/16-entry-point` wrote.
That is not a defect in any prototype; it is the structural fact that explains most of the individual deviations below, and it was not visible from any ticket.

**Seventeen deviations, of which five were already known.**
Five are [`the-enabled-bucket.md`](the-enabled-bucket.md)'s, found by the [#14](https://github.com/onhotpath/ferry/issues/14)/[#15](https://github.com/onhotpath/ferry/issues/15)/[#10](https://github.com/onhotpath/ferry/issues/10) session, and all five are confirmed still live on the tip because that session's fixes landed on `proto/14-15-10-enabled`, which is a sibling of the tip rather than an ancestor.
Twelve are new here.

**Three of them are the ticket's own shape twice over: a published measurement that the tip cannot produce.**

- ADR-0005 prints `[3]string` given index 7 returning `ferry: /V: plane has index 7, [3]string holds 3`.
  That check is in `walk.go` at line 333, which the tip's engine no longer calls, and `e_walk.go` never grew one.
  The tip loads `["" "" ""]` with a nil error.
- ADR-0005's container-address table prints `Get(/tags) = Absent` for `tags: [a]`.
  The driver returns `Absent` **and a non-nil error**, and the probe that produced the table is `v, _ := r.Get(ctx, probe)`.
  The published row is what a probe that discards the error produces.
- ADR-0010 says the schema compile the cache saves is "a whole schema compile including ADR-0007's chain, which probes method sets per type".
  On the tip `chainOrder` is `nil` and `chainBeforeKind` is `false` by default, so the 47370 ns compile it measured probed no method sets at all.

**And the round-trip harness, which is ADR-0001's route-(b) authority over the whole type set, does not run through the tip's engine.**
`harness.go` calls `dump()` and `load()` from `walk.go`.
ADR-0005's "11 of 11 core types, 10 of 10 composites, on three planes" is a statement about the superseded walk.
The walk every ADR from 0007 onward measured has never been round-trip property-tested.

**And the deviations that reach an ADR rather than a prototype nearly all have one shape: a probe whose fixture could not have failed.**
This was not visible when the census was written and emerged from remediating it, so it is stated here and evidenced in section 8.
It is worth separating from "the prototypes drifted", because it is a different defect with a different remedy.
A prototype that drifts stops doing something and a probe goes red; the fix is code.
A fixture that cannot fail leaves the probe green while the sentence beside it becomes false, and no amount of re-running finds it.
Five of the measurements this work moved are of that kind: ADR-0005's flattening-plane row taken against a `map -> map` transform rather than a plane, ADR-0005's completeness check with eleven proof rows against eighteen admitted members, ADR-0005's container-address row produced by `v, _ := r.Get(...)`, ADR-0008's splitter fixture with no tag carrying both an apostrophe and an option, and ADR-0010's `LoadOver` row whose zero-reading case called a helper that returns the seed.
Every one is a green probe over a case its own fixture excluded.

Nothing here reopens a decision.
One finding is a candidate for an ADR amendment and is named as such rather than taken: ADR-0010's compile-cost sentence describes work the measurement did not do.

## 1. The chain is two chains

The ticket, the handover and [`the-enabled-bucket.md`](the-enabled-bucket.md) all describe one chain.
Reading `git ls-tree` per branch rather than the prose:

```
proto/4-address-shape ── proto/5-source-sink
                              │
                              └── proto/7-type-set ─┬─ proto/8-defaults ── proto/11-tag-grammar ── proto/9-errors
                                                    │        (ADR-0006)         (ADR-0008)            (ADR-0011)
                                                    │
                                                    └─ proto/12 ── proto/19 ── proto/16 ── proto/25   ← the tip
                                                        (0007)      (0009)      (0010)     (0012)
```

The upper line ends at `proto/9-errors` and nothing from it is on the tip.
`d_*.go` (ADR-0006's forty-two probes), `t_*.go` (ADR-0008's forty-one) and `proto/9-errors`' `e_*.go` (ADR-0011's twenty-one) exist on no branch the tip descends from.
`proto/16-entry-point` then took the `e_` prefix for its own eleven probes, so the two sets of `e_main.go` are unrelated files with the same name on two branches.

Three consequences, and all three are structural rather than anybody's mistake.

**ADR-0006's and ADR-0008's rules exist on the tip only as far as `e_schema.go` re-implements them.**
That file's own header says so - "ADR-0008's grammar in the small" - and it is a fair description: four tag words, one option parser, five refusals.
What it is not is ADR-0008's grammar.
Six of the twelve new deviations below are the difference.

**ADR-0011 has no implementation on the tip at all.**
There is no error type, no class sentinel, no aggregate constructor and no aggregating scheduler outside one probe.
That is not drift; the code was never on this line.

**Two scratch branches now carry different fixes.**
`the-enabled-bucket.md` repaired `prefixFree`, the walk's error deletion, the YAML container answer and `splitTag` on `proto/14-15-10-enabled`, which is cut from `proto/16-entry-point`.
The tip is cut from `proto/16-entry-point` too, so those fixes are on a sibling and the tip still has all four.
Whoever cuts the next prototype inherits one branch or the other, and they are not the same code.

## 2. Method, and what a verdict means

Every row below is produced by running, through `Load[T]`, `Dump`, `Compile[T]`, `Bind[T]` or `schemaFor` - never through the inherited `walk.go`, unless the row is about `walk.go`.
Where a probe reports something the tip does not do, the probe also shows the ADR's own measured line, so the two can be read side by side.

Three verdicts, per the ticket:

| verdict | meaning |
| --- | --- |
| **implemented, exercised** | the tip does it, and a probe on the tip touches it |
| **implemented, never exercised** | the tip does it, and nothing on the tip runs it - including where the only probe that runs it is on an ancestor or a sibling branch |
| **not implemented** | the tip does not do it |

One refinement the ticket did not name and the code forced.
Several rules are implemented on the tip **behind a package-level switch whose default is the answer the ADR refused**.
`chainOrder`, `chainBeforeKind` and `keyOptIn` are all of this kind.
They are recorded as **implemented, off**, because the distinction matters for what the fix is: nothing has to be written, and every measurement taken without setting them was taken in the world the ADR rejected.

## 3. The census

Thirty-one statements, run.
The first table is the deviations; the second is the statements that hold, which is what makes the coverage claim in section 7 checkable.

### 3.1 Deviations

The `verdict` column is what the audit **measured**, and it is left as measured.
The `fixed` column was added afterwards and is the only part of this table that is not dated evidence; section 8 records the remediation it points at.

| # | ADR | the statement | verdict | probe | fixed |
| --- | --- | --- | --- | --- | --- |
| D1 | 0003 | the address set is prefix-free, not merely duplicate-free | not implemented | A1 | `854baef` |
| D2 | 0005 | `uint8` is admitted by kind | not implemented | A2 | `7242ab5` |
| D3 | 0007 | the text pair is consulted before kind admission | implemented, off | A3 | `2e22bbf` |
| D4 | 0009 | `Register` runs `dec(enc(zero))` and refuses on failure | not implemented | A4 | `7242ab5` |
| D5 | 0009 | a key codec opts in with `AsMapKey()` | implemented, off | A5 | `7242ab5`, corrected `ce65f41` |
| D6 | 0011 | ferry aggregates, at every moment, in both directions | not implemented | A6 | `958d225` |
| D7 | 0011 | ferry's own message text never carries a plane value | not implemented | A7 | `958d225` |
| D8 | 0011 | one `Error` type, four classes, one aggregate constructor, never `errors.Join` | not implemented | A8 | `958d225` runtime, `0d86c00` compiler |
| D9 | 0008 | core does not call `reflect.StructTag.Get` or `Lookup` | not implemented | A9 | `7242ab5`, corrected `ce65f41` |
| D10 | 0008 | a token is bare or single-quoted with the quote doubled | not implemented | A10 | `854baef` |
| D11 | 0008 | a promoted embedded pointer is a schema compile error | not implemented | A11 | `7242ab5` |
| D12 | 0006 | `required` at a struct or `*struct` means the plane supplied a child | not implemented | A12 | `958d225` |
| D13 | 0005 | an index an array cannot hold is loud | not implemented | A13 | `958d225` |
| D14 | 0004 / 0011 | `Close` always runs, and its failure is an element rather than discarded | partly | A14 | `958d225` |
| D15 | 0005 | a container address reports `Absent`, with no error | not implemented | A15 | `854baef` |
| D16 | 0001 / 0011 | the walk does not discard a `Reader`'s error | not implemented | B10 | `854baef` |
| D17 | 0008 | three diagnostic tiers, edit distance, and a vocabulary table | not implemented | A16 | `7242ab5` |

D1, D10, D12, D15 and D16 are `the-enabled-bucket.md`'s, re-run here against the tip.
The other twelve are new.

**All seventeen are closed on `proto/tip`.**
D8 took two commits and was the last to close, because its compiler half is not a swap: `join` sorts on `(moment, location, message)`, so a schema refusal has to *be* a `*Error` carrying all three before changing the constructor buys anything.
Section 8 has the detail, the one further deviation the remediation itself uncovered, and the two questions it hands on.

### 3.2 Statements that hold

| ADR | the statement | verdict | probe |
| --- | --- | --- | --- |
| 0003 | ordering is segment-wise and not a sort of the rendering | implemented, exercised | A26 |
| 0003 | core compares segment text by exact bytes and never folds | implemented, exercised | A26 |
| 0004 | `Bind` does no I/O; an unreachable plane refuses at open | implemented, exercised | A17 |
| 0004 | `Commit` runs only on success, `Close` always runs | implemented, exercised | A14 |
| 0004 | a map field from a non-enumerating source is loud, never an empty map | implemented, exercised | A29 |
| 0004 | absence is a kind of the value, and `Absent` is kind zero | implemented, exercised | A18, A20 |
| 0005 | `String` is the universal donor, and nothing else coerces | implemented, exercised | A21 |
| 0005 | the seven inputs `cast` corrupts are refused or exact | implemented, exercised | A21 |
| 0005 | a nil and an empty composite are one value, normalised onto nil | implemented, exercised | A20 |
| 0005 | a recursive type does not compile | implemented, exercised | A28 |
| 0005 | a struct that maps no address does not compile | implemented, exercised | A28 |
| 0005 | identity before kind: `time.Duration` is `30s`, not nanoseconds | implemented, exercised | A25 |
| 0006 | `Absent` does not write; an explicit empty beats a non-zero default | implemented, exercised | A18 |
| 0006 | `Null` is admitted by the types that have one and refused elsewhere | implemented, never exercised | A19 |
| 0006 | a declaration attaches to the address **shape** | implemented, exercised | A22 |
| 0006 | a default fills a hole and never conjures an optional section | implemented, exercised | A23 |
| 0006 | `*T` at a leaf takes a default at its own address | implemented, exercised | A23 |
| 0009 | a registry freezes at its first **retained** use | implemented, exercised | A27 |
| 0010 | `Compile[T]` takes neither the cache nor the freeze | implemented, exercised | A27 |
| 0010 | the root must be a struct ferry walks | implemented, exercised | A24 |
| 0010 | the maps-no-address backstop counts minted shapes, not static leaves | implemented, exercised | A28 |
| 0010 | the cache key is `{type, tag key}` per registry | implemented, exercised | A25 |
| 0010 | the schema and the walk are one list, not two computations | implemented, exercised | E6 |
| 0009 | the default registry's freeze point is safe by Go's initialisation order | implemented, exercised | A31 |
| 0001 | a compile refusal is deterministic: 1 distinct string over 300 runs | implemented, exercised | A30 |

The one **implemented, never exercised** row in that table is ADR-0006's `Null` table: it is correct at all six kinds, and no probe on the tip loads a `Null` at a leaf until this one.

### 3.3 One measurement hazard, which is nobody's defect

`r18_freeze.go` registers `netip.Addr` and a named duration into the **default** registry from two package `init()`s, in order to demonstrate ADR-0009's own claim that "every `init` completes before `main.main`".
It does demonstrate it.
It also means the registry `defaultOpts()` hands to every `Load`, `Dump`, `Compile` and `Bind` in the binary is non-empty before any probe runs:

```
the default registry at the top of main(): 2 registrations
  netip.Addr
  main.R18Poll

Compile[struct{ Addr netip.Addr }]()                    -> <nil>
Compile[struct{ Addr netip.Addr }](WithRegistry(fresh)) -> ferry: /addr: netip.Addr maps no address ...
```

No ADR statement is violated.
What is affected is any probe that reads a `Compile` succeeding as evidence about the **chain**, and the first draft of A3 in this session was one.
It is recorded because the next probe to ask a question about `netip.Addr` on this branch will hit it too.

## 4. The deviations, in full

Each entry is the ADR sentence, what the tip does, and whether a published claim depends on it.

### D1. ADR-0003's prefix-free rule is duplicate detection

> A compiled schema's address set contains no address that is a prefix of another.
> A path is a prefix of itself, so this subsumes exact duplicates.

`prefixFree` in `e_schema.go` inserts each address's whole rendering into a `map[string]bool` and reports a repeat.
The prefix relation at a segment boundary is never asked.

```
struct{ Flat string `ferry:"db"`; Nested struct{ Host string `ferry:"host"` } `ferry:"db"` }

Compile      -> <nil>
address set  -> [/db /db/host]
TREE plane   -> dump err = yaml: /db/host: a name segment under a sequence, file written = ""
FLAT plane   -> dump err = <nil>, keys = [DB DB_HOST]
```

That is ADR-0003's own worked reason, reproduced: a schema that a flat plane accepts and a tree plane cannot represent, which the rule exists to refuse at compile so that no driver ever sees it.
The exact-duplicate half does work: two fields tagged `name` give `ferry: two fields address /name`.

**Depends on:** ADR-0003's own numbers were taken on `proto/4-address-shape`, which has `p9_prefix_free.go`, so no published measurement is invalidated.
Repaired on `proto/14-15-10-enabled`; still live on the tip.

### D2. `uint8` is refused, and two admission authorities disagree

ADR-0005's kind table admits `uint`, `uint8`, `uint16`, `uint32`, `uint64`.

```
uint8   -> ferry: /v: unsupported type uint8 (kind uint8)
uint16  -> <nil>
int8    -> <nil>
float32 -> <nil>

                kindClassify (typeset.go)   kindLeaf (e_schema.go)
uint8           true                        false
uint16          true                        true
```

`kindLeaf` omits `reflect.Uint8` from its case list where `kindClassify` has it.
This is ADR-0010's duplication axis 1 - the compiler against the walk - occurring between the compiler and the type set it was supposed to be reading.
It is invisible because `[]byte` and `[N]byte` are handled by the slice and array arms, so only a bare `uint8` field reaches it.

**Depends on:** nothing published.
ADR-0005's harness list is `bool string int int8 uint64 float64 float32 []byte time.Duration time.Time []string`, which contains no `uint8`, and it runs on `walk.go` anyway.

### D3. The codec chain is off, and its default order is the one ADR-0007 rejected

> The text pair is consulted **before** `reflect.Kind` admission.
> A declaration beats an inference.

```
chainOrder      = []  (len 0)
chainBeforeKind = false

as the tip ships    netip.Addr   -> ferry: /addr: netip.Addr maps no address ...
                    netip.Prefix -> ferry: /p: netip.Prefix maps no address ...
                    net.IP       -> bytes("\x00...\xff\xff\xc0\x00\x02\x01")
chain before kind   netip.Addr   -> <nil>
                    netip.Prefix -> <nil>
                    net.IP       -> string("192.0.2.1")
```

The first block is the "kind only" column of ADR-0007's headline table, reproduced exactly.
The second is the column the ADR chose.

Those rows are taken against a **fresh** registry, and the first draft of this probe got them wrong by not doing so.
`r18_freeze.go` has two package `init()`s that register `netip.Addr` and a named duration into the **default** registry, so `Compile[struct{ Addr netip.Addr }]()` succeeds on the tip - through the registration, not through the chain.
That is ADR-0009's own model working exactly as designed, and it is recorded as A31 rather than as a deviation.
What it costs is that every E16 and B25 measurement taken through `defaultOpts()` ran against a registry holding two codecs no probe in either suite asked for, and that a probe reading `Compile` succeeding as evidence about the chain reads it wrong.

Every P12 and R probe that measures the chain sets both in its own body and reverts them in a `defer`.
**No E16 or B25 probe sets either.**
So ADR-0010's eleven probes and ADR-0012's thirteen were all taken with ADR-0007's decision switched off.

**Depends on: yes, and this is the one candidate for an amendment.**
ADR-0010's argument for the two-level cache is that "for ferry the wasted work is a whole schema compile including ADR-0007's chain, which probes method sets per type".
The 47370 ns it measured probed no method sets, because the chain was off.
The decision is not in doubt - a compile that also runs the chain is strictly more expensive, so the direction is safe - but the sentence describes work the measurement did not do.
Raised rather than resolved: see section 6.

### D4. `Register` does not run the codec against the zero value

> `Register` encodes the zero value of `T`, donates `String` to the declared kind, decodes it back, and refuses the registration if either half errors.

```
Register(StringCodec(netip.Addr.String, netip.ParseAddr)) -> <nil>
zeroCheck(same)                                           -> ferry: netip.Addr: the codec is not total
                                                             over the zero value: it encodes to
                                                             string("invalid IP") and decoding that back
                                                             fails: ParseAddr("invalid IP"): unable to parse IP

dump the zero value -> string("invalid IP")   err=<nil>
load it back        -> ferry: /a: ParseAddr("invalid IP"): unable to parse IP
```

`zeroCheck` is a free function in `r16_zerocheck.go`.
`(*Registry).Register` checks frozen, pointer, core-owned and duplicate, and never calls it.
So ADR-0009's headline defect - "registering it makes the type worse than not registering it" - is fully available through the tip's own API, and the check the ADR decided on is beside the API rather than in it.

That is ADR-0007's own third defect in a new place: a rule implemented somewhere other than where the rule says it lives.

**Depends on:** no published number.
R16's own table is produced by calling `zeroCheck` directly, so it is honest about what it measured; what it does not say is that `Register` never calls it.

### D5. The key-codec opt-in defaults to the rule ADR-0009 refused

`keyOptIn` is `false` at package level.

```
keyOptIn = false, codec registered without .AsMapKey()
  Compile[struct{ Limits map[netip.Addr]int }] -> <nil>

keyOptIn = true
  -> ferry: /limits: unsupported map key type netip.Addr
```

Two things.
The rule is off, so a registered key codec silently keys a map with no opt-in, which is the dropped-map-entry failure ADR-0009 measured.
And when it is on, the diagnostic is the generic unsupported-key message, not the one the ADR specifies, which is where the injectivity obligation was supposed to be communicated:

> ferry: /limits: netip.Addr has a registered codec but is not declared usable as a map key; a key codec's text must be injective over the key type, or two keys collapse into one address; add `.AsMapKey()` to the registration if it is

**Depends on:** R11 and R15 set `keyOptIn` themselves, so ADR-0009's rows are honest.

### D6. There is no aggregation

> ferry reports every failure that is not a consequence of another failure it is already reporting.

```
five leaves, every one unparseable
  errors reported: 1
  ferry: /a: strconv.ParseInt: parsing "x": invalid syntax
```

`Load`, `LoadOver`, `Dump`, `Binding.Load` and `SinkBinding.Dump` all construct `&walker{... sch: serial ...}`, and `serial` returns on the first non-nil error.
The aggregating scheduler exists, as `aggregate` in `e12_yield.go`, and nothing outside that probe uses it.
So the tip is `StopOnFirstError`, which is the one Option ADR-0011 declines to ship.

**Depends on:** ADR-0010's E12d row - 1 error against 2 with the walk byte-identical - is produced by the probe that owns `aggregate`, so it is honest.
ADR-0012's allocation counts are on the success path and are unaffected.

### D7. ferry's message text carries the plane's value

> ferry's own message text never contains a value the plane supplied.

```
the plane holds a secret at an int address
  ferry: /max_conns: strconv.ParseInt: parsing "AKIAIOSFODNN7EXAMPLE": invalid syntax
  contains the plane's own text: true
```

`loadDir`'s leaf wraps with `fmt.Errorf("ferry: %s: %w", at, err)`, which is exactly the naive form ADR-0011 measured four leaks in five for, on a plane class where every value is a secret.

### D8. There is no error model

No `Error` type, no `Address()` accessor, no `ErrSchema`/`ErrMissing`/`ErrValue`/`ErrPlane`/`ErrDriver`, no `Elements`, no `ErrorAt`, no `DiffErrors`.
`ErrReadOnly` exists, because it is ADR-0004's and predates ADR-0011.
`compileSchema2` sorts its refusals and then calls `errors.Join`, which ADR-0011 rules out by name because the aggregate it produces is invisible to `Elements` and ordered by insertion.

Sorting at construction is nonetheless honoured, measured at 1 distinct string over 300 compiles (A30).
So the property ADR-0011 cares most about holds by accident of the compiler doing its own sort, and the mechanism it decided on is absent.

**Depends on:** nothing on the tip.
ADR-0011's twenty-one probes are on `proto/9-errors`, which is not an ancestor.

### D9. Core reads the tag with `reflect.StructTag.Lookup`

> Core does not call `reflect.StructTag.Get` or `Lookup`.
> It scans `reflect.StructField.Tag` with its own parser and reports what `Get` answers with a silent empty string.

`parseTag` calls `f.Tag.Lookup(key)`.
All three of ADR-0008's failure modes are therefore invisible:

```
a bare double quote    Lookup="origins,default=[" ok=true   Compile -> <nil>
                       and the json tag on the same field is now ""
an invalid Go escape   Lookup=""                  ok=false  Compile -> field H carries no ferry tag
two ferry tags         Lookup="first"             ok=true   Compile -> <nil>
```

The middle row is the sharpest: a tag ferry cannot read is reported as a tag the user did not write, and the remedy the message offers is to write the tag they already wrote.

### D10. The grammar cannot write ADR-0008's own headline example

```
"greeting,default='Hello, world'"   -> ["greeting" "default='Hello" " world'"]
"brokers,default='h1:9092,h2:9092'" -> ["brokers" "default='h1:9092" "h2:9092'"]
"'a,b'"                             -> ["'a,b'"]

Compile -> ferry: /Brokers: unknown option "h2:9092'" | ferry: /Greeting: unknown option " world'"
```

`splitTag` treats a quote as significant only when it is the first byte of a comma-separated part, so a quoted **option value**, which begins after `default=`, is not quoted at all.
The quoted **name** case works.
This is 3.9% of real free-text tag values by ADR-0008's own census, and the one case it says has to read well by ten to one.

Repaired on `proto/14-15-10-enabled`; still live on the tip.

### D11. A promoted embedded pointer is not refused, and a nil one writes `Null` at the empty path

> embedded **pointer**, with no ferry tag: **schema compile error**

```
Compile[struct{ *Common; Port int }] -> <nil>
address set                          -> [/env /name /port]
load /name=string("n")               -> &{Name:n Env:}   err=<nil>
```

The Load case ADR-0008 measured as a silent total loss **works** on this tip, because `e_walk.go` materialises the pointer from the promoted children's presence bit.
So the tip is better than the prototype ADR-0008 measured, and the refusal is still absent - and the refusal has a second reason the ADR did not have:

```
dump with the embedded pointer nil, err=<nil>
  ""       null
  "/port"  number("8080")
```

A promoted embedded pointer's own address **is the empty path**, so a nil one writes `Null` at the address ADR-0003 says may not exist and ADR-0010's root rule refuses at every other door.
That is ADR-0010's root-leaf hole reached through a door the root rule does not cover, and it is new here.

### D12. `required` at a struct and a `*struct` is enforced by nothing

```
Compile[struct{ Auth *Cred `ferry:"auth,required"` }] -> <nil>
  empty plane -> {Auth:<nil>}          err=<nil>
Compile[struct{ Auth Cred  `ferry:"auth,required"` }] -> <nil>
  empty plane -> {Auth:{User: Pass:}}  err=<nil>
```

ADR-0006's measured line for both is `ferry: /auth: required, and the plane supplied nothing under it`, and it records repairing exactly this in its own draft.
`applyOptions` sets `node.required` on a struct and on a pointer node; `e_walk.go` reads `n.required` in exactly one place, `direction.leaf`.

Found independently by the #14/#15/#10 session on `proto/16-entry-point` and recorded rather than fixed there, on the ground that the repair is ADR-0006's.

### D13. An array index the array cannot hold is silent

```
[3]string, plane holds only #0  -> ["a" "" ""]  err=<nil>     matches ADR-0005
[3]string, plane holds only #7  -> ["" "" ""]   err=<nil>     ADR-0005 says loud
```

ADR-0005's published line is `ferry: /V: plane has index 7, [3]string holds 3`.
That message exists, at `walk.go:333`, on the walk the tip no longer uses.
`e_walk.go`'s `nArray` case iterates `n.n` static element addresses and never enumerates, so an index outside the array is not read and not reported.

**Depends on: yes, in the narrow sense the ticket names.**
The published row is real and was produced by `walk.go`; it is not reproducible on the tip.

### D14. A `Close` failure is discarded

```
success      calls=[set /a commit close]   err=<nil>
Set fails    calls=[set /a close]          err=kv: no write ACL
Close fails  calls=[set /a commit close]   err=<nil>          <- the failure vanished
both fail    calls=[set /a close]          err=kv: no write ACL
```

ADR-0004's protocol is implemented exactly: `Commit` runs only when the walk succeeded and `Close` always runs.
ADR-0011 then makes a `Close` failure an element of the aggregate, with the moment first in the sort key precisely so that it can sit after the walk errors it did not cause.
`SinkBinding.Dump` writes `defer rel.Close()` and drops the result.
`final.go`'s `fDump` does capture it, through `joinErr`, which returns the first error and discards the second - so the older lifecycle drops it too whenever anything else already failed.

### D15. The YAML driver returns an error at a container address

> | `tags: [a]` | `Get(/tags)` = `Absent` | `Children(/tags)` = `/tags#0` |

```
Get(/tags) on `tags: [a, b]` -> absent, err = yaml: /tags is not a scalar
```

`fYAMLReader.Get` returns `Absent, fmt.Errorf("yaml: %s is not a scalar", addr)` for any non-scalar node.
The published table row is `absent`, and the probe that produced it is `container.go:44`:

```go
v, _ := r.Get(ctx, probe)
```

So ADR-0005's row is what a probe that discards the error prints.
This is the third known deviation's shape - a published row its own probe did not produce - occurring in ADR-0005 rather than in ADR-0010.

### D16. The walk deletes the error `Reader.Get` returns

Already in #41's body, and it is what makes D15 invisible: `loadDir`'s `get()` is

```go
v, err := r.Get(ctx, at)
if err != nil {
    v = Value{}
}
```

D15 and D16 are two latent defects that cancel, which the #14/#15/#10 session found first and which is why neither was visible for four prototypes.
Fixing either alone surfaces the other.

### D17. There is one diagnostic and no tiers

```
requird (near miss)  -> ferry: /H: unknown option "requird"
omitempty (foreign)  -> ferry: /H: unknown option "omitempty"
leading space        -> ferry: /H: unknown option " required"
```

ADR-0008 decides three tiers, edit distance, a table of the neighbourhood's vocabulary, whitespace as its own diagnosis, and 22 specific remedies out of 26 measured mistakes.
None of it is on the tip.
Tier 2 and tier 3 do exist inside `applyOptions` - admissibility before contradictions - so ADR-0006's diagnostic rule is honoured and ADR-0008's tier above it is not.

## 5. The conformance-case list for [#35](https://github.com/onhotpath/ferry/issues/35)

Every deviation is a case `ferrytest` has to carry, because a rule that no suite holds is a rule the next implementation can drop the same way.
Written as assertions rather than as prose, and grouped by which suite owns them.

**The driver conformance suite** (a `Source`/`Sink` pair, run per driver):

1. `Bind` succeeds against an unreachable plane, and the refusal lands inside the open. *(holds on the tip; keep it)*
2. `Get` at a **container address** returns `Absent` with a **nil error**, for every container shape the plane can hold: a populated list, an empty list, an empty map, a missing key. *(D15)*
3. `Get` returning a non-nil error reaches the caller as an error and never as `Absent`. *(D16)*
4. `Children` at the same addresses returns the element addresses, kinded. *(holds)*
5. `Commit` runs only on success; `Close` always runs; a `Close` failure appears in the reported error set. *(D14)*
6. A driver that produces a plane key refuses a non-injective key function over the address set, before any I/O, naming both addresses.
7. A driver's key function retains nothing across opens, asserted on the **write** side, which is where retention refuses a legal write. *(ADR-0012)*
8. A sink accepts a dynamic address its static table never held - ADR-0004's `Set(/labels/env)` example, promoted from a paragraph. *(ADR-0012)*
9. A driver reading its plane from the context refuses at open when it is absent, with `ErrPlane`. *(ADR-0012)*
10. A driver declares the `Value` kinds its plane can carry, and the suite asserts that the proofs it cannot express are refused loudly rather than mangled. *(ADR-0005)*

**The schema-compile suite** (no plane, `reflect.TypeFor[T]()` only):

11. A leaf and a subtree at one segment do not compile, and the message names both addresses. *(D1)*
12. Two fields addressing one segment do not compile. *(holds)*
13. A promoted embedded pointer does not compile. *(D11)*
14. A field whose tag `reflect.StructTag.Get` would silently truncate is reported as a malformed tag and not as a missing one. *(D9)*
15. A field carrying two ferry tags is reported. *(D9)*
16. A quoted option value containing a comma parses, at the name position and at `default=` alike. *(D10)*
17. A near-miss option gets its specific remedy; a foreign mapper's word gets its own sentence; surrounding whitespace is its own diagnosis. *(D17)*
18. `required` on a struct, a `*struct` and an array compiles; on a slice, a map or a pointer to either it does not. *(holds)*
19. A registration whose codec is not total over the zero value is refused **at the registration call**. *(D4)*
20. A `map[T]V` whose key type is registered without `AsMapKey()` does not compile, with the injectivity sentence. *(D5)*
21. Every Go kind ADR-0005 admits compiles, one single-field struct per kind, including `uint8`. *(D2)*

**The walk suite** (memory plane, no driver):

22. `required` at a struct and a `*struct` fails when the plane supplied no child under it, and is suppressed when a child under it already reported. *(D12, and ADR-0011's one suppression bit)*
23. An index outside an array's length is an error naming the index and the length. *(D13)*
24. A `Null` at each of the six kind classes gives ADR-0006's table exactly. *(holds, and exercised for the first time here)*
25. Five failing leaves report five errors, on Load and on Dump alike. *(D6)*
26. A Dump that fails to encode writes nothing at all; a Dump onto a `Committer` reports both failure kinds in one run. *(ADR-0011)*
27. No ferry-authored message contains text the plane supplied, asserted over every reachable decode failure. *(D7)*
28. The error set is an exact-set diff over `(address, class)`, which needs the classes to exist. *(D8)*

**The round-trip property harness:**

29. `CoreTypes()` runs **through the entry point** - `Dump` then `Load` - rather than through a walk of its own.
    This is the single most important row in the list: on the tip the harness calls `walk.go`, so the engine every ADR from 0007 onward measured has never been property-tested.
30. The completeness check iterates the identity table and the admitted kind list, so a kind missing from either side of `kindLeaf`/`kindClassify` fails CI. *(D2)*

## 6. What has to be fixed, and in what order

Nothing below is done.
The ticket's constraint is that fixing a prototype moves the evidence base underneath a merged ADR, so each fix is regression-diffed against the four suites and anything that moves a published number is an ADR amendment argued at the gate.

Ordered by what unblocks what, not by severity.

| # | fix | where | notes |
| --- | --- | --- | --- |
| 1 | reconcile the two scratch branches | `proto/25-binding` vs `proto/14-15-10-enabled` | four fixes exist on a sibling of the tip. Until they are on one branch, "the tip" is ambiguous and every later measurement inherits whichever branch it was cut from. This is the prerequisite for everything else. |
| 2 | D16 then D15, together | `e_walk.go`, `fdrv_yaml.go` | they cancel, so neither may be fixed alone. Already fixed together on the sibling. |
| 3 | D2 `uint8` | `e_schema.go` `kindLeaf` | one token. Then delete `kindClassify` or make `kindLeaf` call it, so there is one authority. |
| 4 | D1 prefix-free | `e_schema.go` `prefixFree` | ~10 lines, and `proto/4-address-shape` has a working one to port. Expected to change nothing on the four suites; if it refuses a fixture, that fixture was illegal. |
| 5 | D12 `required` at a container | `e_walk.go` | needs a hook at `nStruct`/`nPtr` reading the subtree's presence bit. Interacts with ADR-0011's suppression bit and, per PR #43, with the scheduler: an aggregating scheduler is a precondition for the suppression to be sound. |
| 6 | D13 array bound | `e_walk.go` `nArray` | the message exists at `walk.go:333` to port. |
| 7 | D4 zero-value check | `r_registry.go` `Register` | call `zeroCheck`. Will refuse three registrations in R17's usage table, which is the point; check whether any published R-row asserts the acceptance. |
| 8 | D5 / D3 defaults | `typeset.go`, `chain.go` | flipping `chainOrder`, `chainBeforeKind` and `keyOptIn` to the decided values is one line each and **will move published numbers**, because the compile then probes method sets. Gate before flipping. |
| 9 | D6 aggregation | `e_entry.go`, `b_bind.go`, `b8_dump.go` | swap `serial` for `aggregate` at five call sites. Needed before #5 is sound. |
| 10 | D14 `Close` | `b8_dump.go`, `final.go` | capture it; it becomes an element once #9 and #11 exist. |
| 11 | D7, D8 the error model | new | ADR-0011 has no implementation on any ancestor of the tip. This is the largest item and it is a port from `proto/9-errors`, not a rewrite. |
| 12 | D9, D10, D17 the tag grammar | `e_schema.go` | likewise a port, from `proto/11-tag-grammar`'s `t_grammar.go`. D10 alone is small and is already fixed on the sibling. |
| 13 | D11 embedded pointer | `e_schema.go` | the refusal is four lines; the `Null`-at-the-empty-path consequence is why it is not cosmetic. |
| 14 | the harness | `harness.go` | point it at `Dump`/`Load` rather than `walk.go`. Expected to go red, which is the finding. |

Item 8 and item 14 are the two that should be argued before they are done.
Item 8 moves published numbers.
Item 14 is expected to fail, and what it fails on is more valuable than the fix.

## 7. What this audit did not cover

Stated as a list, because an audit that does not state its own coverage is the thing it exists to prevent.

**Thirty-one statements were run.**
Mining the twelve ADRs' `## Consequences` and `## Items from the xload survey` sections yields on the order of two hundred that a prototype could stop implementing.
So this is roughly one statement in six or seven, chosen by two heuristics: a rule an ADR records repairing in its own draft, and a rule whose only implementation would have had to cross the branch break in section 1.
Both heuristics are biased toward finding deviations, and the hit rate - seventeen in thirty-one - should be read with that in mind rather than extrapolated.

**Not covered at all:**

- **ADR-0002.** Its statements are about module layout, `go.mod` and CI, and the prototype is one `main` package with a `go.sum`. Nothing in it is checkable against this tip, and its `nojsonv2` and `GOWORK` measurements would need a fresh module rather than a probe.
- **ADR-0001's four buckets.** Whether a capability is In core, Enabled, Milestoned or Ruled out is not a property a prototype implements. `the-enabled-bucket.md` is the test of that and it is a different ticket's.
- **The pinned `encoding/json/v2` option set.** ADR-0005 says in its own consequences that half two is "executed by nothing, because no ferry module imports v2", and that is still true. Nothing changed and nothing was checked.
- **Every measurement in ADR-0003 and ADR-0004 taken on `proto/4` and `proto/5`.** The four drivers, the driver-cost line counts, the `Bind` 158 ns against 2743 ns, the four-plane address table, the 200,000-path fuzz, `jsontext.Token`'s 32 bytes. None of those files is on the tip; `fdrv_yaml.go` is the only one that survived. Re-running them means checking out `proto/5-source-sink`, which is a session of its own.
- **ADR-0006's, ADR-0008's and ADR-0011's own prototypes.** Section 1 explains why: they are on a dead branch line. Auditing *those* branches against *those* ADRs is a different and possibly more useful question than auditing the tip, and it is not this ticket as written.
- **Anything about concurrency.** ADR-0010's `-race` report on the presence bit and ADR-0012's 64-goroutine binding run were not re-run.
- **The `%+v` report, the elision threshold, the sort key's three parts.** All ADR-0011's, all unimplementable to check while D8 stands.

**Two things checked and left unresolved:**

- Whether ADR-0010's compile-cost sentence needs amending (D3). Raised in section 6, not decided.
- Whether ADR-0004's "naming the field and the source" is met by a message that names the address and the Go type but not the source (A29). Recorded as a note on an implemented row rather than as a deviation, because the rule is honoured and the wording is not.

**One thing this audit cannot do.**
It checks the tip against the ADRs.
It does not check the ADRs against each other, and section 1 is the reason that matters: three ADRs were measured on a branch line that no later ADR could see, so a contradiction between ADR-0006 and ADR-0010 would be invisible to every probe on either branch.
[`the-enabled-bucket.md`](the-enabled-bucket.md) found one of exactly that shape between ADR-0006 and ADR-0010, by asking a question neither ADR's prototype could answer.

## 8. Resolution

Added after the census was published, so that sections 1 to 7 stay a dated record of what was found rather than a changelog.
Everything below happened on scratch branches; nothing here is library code.

### 8.1 Where the code is now

`proto/tip` is the single prototype tip and it is the reconciliation of `proto/25-binding` and `proto/14-15-10-enabled` plus four rounds of repair.
Every other `proto/*` branch is history.

| branch | what it carried | now |
| --- | --- | --- |
| `proto/25-binding` | ADR-0012's thirteen probes, and the audit's own `A41` | merged at `854baef` |
| `proto/14-15-10-enabled` | `the-enabled-bucket.md`'s four fixes | merged at `854baef` |
| `proto/41-compiler` | D2 D18 D4 D11 D9 D17 D5, then D3 | merged |
| `proto/41-runtime` | D6 D7 D8 D12 D13 D14, the harness, the flattening plane | merged |
| `proto/41-typeset-tag` | `X3`, ADR-0005's admitted set against ADR-0008's field rule | merged |
| `proto/41-measure` | `X4`, ADR-0011's table and the `Get`-order question | merged |
| `proto/45-mapkey` | `Y45`, [#45](https://github.com/onhotpath/ferry/issues/45)'s three questions | merged |

**All seventeen deviations are closed, and so is D18.**
D8 was the last, and its compiler half was the hardest, for a reason worth stating: it is not a swap.
`join` sorts on `(moment, location, message)`, so a schema refusal has to *be* a `*Error` carrying all three before switching the constructor buys anything - otherwise it sorts as `mNone` with no location and the result orders by rendered string again, which is what it did before.

Every fix was regression-diffed against every suite, normalised for timings and heap addresses, and the whole set is byte-identical apart from the rows a fix was meant to move.
**`go test ./...` is green**, for the first time since this audit began.

### 8.2 D18, which the remediation found rather than the audit

ADR-0005 specifies a completeness check - "core's test iterates the identity table and the admitted kind list and asserts that every member has a proof in `CoreTypes()`" - and nothing implemented it.
Implemented at `7242ab5`, it is **red on arrival**: eighteen admitted members, eleven proof rows, seven with none (`int16 int32 int64 uint uint8 uint16 uint32`).
ADR-0005's published "11 of 11 core types" is eleven of eighteen.
That is an ADR amendment and not a prototype fix, and it is one of the five fixture defects named in the summary.

**Closed.** The seven rows are in `coreSet()`, each with the boundary values that distinguish its width from its neighbours, so a codec that silently truncates fails here rather than in production.
ADR-0005's eleven-of-eighteen is eighteen of eighteen, and `go test ./...` is green.

### 8.3 The audit's own two leaks

Found in this file's probe source rather than in the engine, and fixed at `e6b6a2b`.

`A3` and `A5` each drive a package-level switch and each restored a **hardcoded** pre-fix default rather than deferring the tip's own.
So `A4` through `A31` ran with ADR-0007's chain off and ADR-0009's key opt-in off, and part of their output was an artefact of the probe above them.
Both now `defer`.

`A31` ranged `defaultRegistry.byType` unsorted, which is a live violation of ADR-0001's determinism invariant **inside the audit's own probe**.
Two further probes printed live pointers, so `A41=all` was not byte-stable across runs of the same binary.
All three fixed; `A41=all` is now identical over repeated runs.

With the two leaks closed, thirteen `VERDICT:` strings in `A41` were describing a world that no longer exists and were reconciled against measured output, each keeping what was found as an "as FOUND" clause.
All thirty-one probes now read `IMPLEMENTED`. `A8` read `PARTLY` until D8's compiler half landed.

### 8.4 What was still open, and what closed it

Every item this section listed when the resolution was first written is now closed except two, both of which are somebody else's ticket by design.

- **D8's compiler half.** ~~`e_schema.go:107` and `:423` call `errors.Join`.~~ **Closed.** Fourteen refusal sites, plus `mapKeyRefusal` and `unsupportedType`, are now `*Error` values carrying `mCompile`, `ErrSchema` and an address; both `errors.Join` calls are `join`. Measured on a schema with two independent refusals: it is a ferry aggregate and `Elements()` reports 2 of 2.
  The rendering consequence is real and it is the ADR's: an aggregate prints a **summary** under `%v` with the elements under `%+v`. Eleven probe sites split `Error()` on newlines and were finding one line saying "3 errors" and none of them; they go through `Elements()` now, which is the probes reading their own finding.
- **`DiffErrors` sorts by the canonical rendering.** **Closed.** The key holds the `Path` rather than its `String()`, and the sort is `CompareSegmentwise`. Twelve indices now read `0 1 2 3 4 5 6 7 8 9 10 11`. `Path` is already comparable, so this cost nothing.
- **`e3_resolved.go` indexes `s.root.fields[5]` positionally.** **Closed.** Both lookups are by address. [#50](https://github.com/onhotpath/ferry/pull/50)'s costing of a per-node field sort had found that index 5 would become a node with a nil `def` and segfault the probe, which is what makes this worth fixing rather than noting.
- **`P12` and `P19`'s map-iteration-order flakes.** **P12's is closed**, by R3 rather than by anything aimed at it: the line that flipped was `map[time.Time]string` losing an entry, and losing it is now a refusal. Measured, `P12=all` is byte-identical over 8 runs of one binary where the same binary without R3 gave 2 distinct outputs over 8. `P19`'s two `R11` lines are refusals for the same reason; the flake was not observed there in 6 runs, so no claim is made that one was fixed.
- **[#45](https://github.com/onhotpath/ferry/issues/45), the `.AsMapKey()` gap.** **Closed**, and it produced a fifth ADR amendment. See 8.6.
- **The `Get`-order question.** **Closed** by one clause in ADR-0003 saying the sequence is deliberately unspecified. See 8.6.

**`walk.go` is not retired, and that is a decision rather than an omission.**
It is the engine ADR-0003, ADR-0004, ADR-0005, ADR-0007 and ADR-0009's numbers were taken on, **34 probe files** call it, and `X3=4c` reproduces ADR-0005's published `/IP` and `/Mask` row *on it*.
Deleting it deletes the ability to reproduce published evidence, which is the opposite of what this ticket exists to protect.

What was actually wrong is narrower and is fixed: `Conf` and `Inner`, the default `go run .` fixture, carried **no ferry tags**, so the suite a reader runs first exercised the naming rule ADR-0008 refused, on a struct the current engine would not compile - 13 addresses against 11 refusals.
They are tagged with the segment names `walk.go` was already inventing, so every address and every published number on that fixture is unchanged and the naming disagreement goes from 11 refusals to 0.

**And that surfaced one the refusals were hiding**, which is reported and not fixed:

```
only in e_schema: [/Limits /Opt /Tags]
only in walk.go : [/Limits/* /Tags/*]
```

The two engines mint different **static** address sets for the same compiling type.
`walk.go` puts a dynamic composite into the static set as a wildcard where `e_schema` puts the container address it is enumerated under, and `e_schema` additionally mints `/Opt` for the optional section itself.
That is ADR-0003's two-tier model rather than ADR-0008's naming rule, and **the static set is what `Bind` receives**, so deciding it changes a driver contract.
Not this ticket's, and it needs a ticket.

The 12-of-26 third-party disagreement stands and is not fixable by tagging, because those are exported fields in other people's packages.
ADR-0005's answer to that is registration, and [#46](https://github.com/onhotpath/ferry/pull/46) is where it landed.

### 8.5 The six ADR amendments this produced

Each landed as its own PR against its own ADR, so a reviewer saw one argument at a time.

| ADR | what moved | why it was not a quiet edit | PR |
| --- | --- | --- | --- |
| 0005 | the flattening-plane row, the completeness check, and a **how** column on the three-outcomes table | three published numbers change and three rows marked "admitted, round-trips" no longer compile | [#46](https://github.com/onhotpath/ferry/pull/46) |
| 0008 | one phrase, "one per field" | the worked block's element count is right and its attribution is not | [#47](https://github.com/onhotpath/ferry/pull/47) |
| 0010 | a footnote on the 47370 ns compile | the sentence beside it described work the measurement did not do; it does now | [#48](https://github.com/onhotpath/ferry/pull/48) |
| 0011 | one sentence, and **no change to the table** | the row that moved is a counterfactual no importer can reach | [#49](https://github.com/onhotpath/ferry/pull/49) |
| 0003 | one clause: the `Get` sequence is deliberately unspecified | "wherever ferry enumerates addresses" reads as covering a fourth place, and the extension is undeliverable | [#50](https://github.com/onhotpath/ferry/pull/50) |
| 0007 | **a decision reversed**: keying a map is registration-only | the permission it granted predates the terms it invoked | [#51](https://github.com/onhotpath/ferry/pull/51) |

The last one is the only **reversal**, and it is the one worth reading, because neither ADR was wrong on its own.
ADR-0007 admitted a chain-claimed `String` type as a map key "on the same terms as a registered codec"; ADR-0009 landed afterwards and made those terms an explicit `.AsMapKey()` opt-in, whose whole mechanism is a diagnostic at the registration call site.
A chain arm has no call site.
So the two composed into an outcome neither wrote down - **the refusal is lifted by deleting a line** - which made the map key the only position in ferry where registering a type left it *less* usable than not registering it.

That is the same shape as this audit's own summary finding, one level up: not a rule that drifted, but two correct rules whose composition nobody measured.
A reader of either document could not have seen it.

### 8.6 What this ticket hands on

Three things, each with a ticket or needing one.

- **[#31](https://github.com/onhotpath/ferry/issues/31) is untouched and is now half-mitigated.** `map[time.Time]string` still compiles - R3 does not amend the admissible key set, which is what #31 asks for - but a dump that would lose an entry now refuses at the address it would have lost it at. The silence is gone; the ticket is not.
- **The two engines' static address sets disagree** about dynamic composites and optional sections, which is a driver-contract question and has no ticket.
- **[#35](https://github.com/onhotpath/ferry/issues/35) inherits section 5's thirty conformance cases**, plus the four this remediation added: the collapse check, the segment-wise diff, `Elements()` over a schema refusal, and the completeness check now that it is green.
