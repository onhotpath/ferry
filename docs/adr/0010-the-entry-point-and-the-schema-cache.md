# 10. The generic entry point, and how a compiled schema is cached

Status: Accepted
Date: 2026-08-02
Ticket: [#16](https://github.com/onhotpath/ferry/issues/16)

## Context

Nine ADRs have decided what ferry does.
None of them has decided what a caller types.

[ADR-0001](0001-what-ferry-supports.md) left the exported verb names open and called `Load` and `Dump` a working assumption.
[ADR-0004](0004-source-and-sink.md) decided `Bind`, `Get`, `Set`, `Commit` and `Close`, which are driver-facing, and said in as many words that the caller-facing ones remain open.
So this ADR is the first one a consumer meets, and the first section is what they write.

Three ADRs handed this ticket something by name, and two of them did it independently.

[ADR-0008](0008-the-struct-tag-grammar.md) made the struct tag key a caller-supplied Option and measured that one `reflect.Type` under two keys yields two different address sets, so **the tag key is compile-affecting and is in the cache key**.
It also named `Validate[T]()` and required that it and `Load` share one compiler.
This ADR keeps the requirement and renames the function, which is an amendment to an Accepted ADR and is argued rather than done quietly.

[ADR-0009](0009-typed-codec-registration.md) made the codec registry a value that freezes at its first use and measured that a schema cache keyed by `reflect.Type` alone hands one registry another registry's codec, silently.
So **the registry is in the cache key too**, and the obligation is one sentence: *once a type has been resolved against a registry, that registry's answer for that type must never change.*

[ADR-0006](0006-defaults-and-zero-values.md) is the one that constrains the signature rather than the cache.
It measured that an in-place reload **leaks the previous load's values** for every address the plane has since lost, under every absence rule it considered, and concluded that the fix is not a different rule but that a reload must produce a new value rather than mutate a live one.
It also opened the question this ADR has to close: what may become part of the cache key, measured at the bad end as `hash of unhashable type main.LoadOption`.

[ADR-0007](0007-the-codec-chain-and-its-precedence.md) left two things here: where the compiled schema caches the per-type claim, and whether a root leaf is a legal address.

The owner's comment on the ticket added the **walk architecture**, which otherwise has no home on the map, with the constraint stated: write the walk exactly once.

The inherited answer is xload's `Load(ctx context.Context, v any, opts ...Option) error` at [load.go:37](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L37), with two runtime kind checks behind it, no schema cache at all (survey item **5.3**, measured at 3343 ns / 2992 B / 54 allocs against 476 ns / 320 B / 5 allocs for a cached prototype), and two walks that have already diverged (**5.2**, reproduced).

This ADR is written from a throwaway prototype on branch `proto/16-entry-point`, which never merges.
It is built on `proto/19-registration`, so every measurement runs against a real `Path`, a real `Value`, the type set ADR-0005 landed, the chain ADR-0007 landed, the `Registry` ADR-0009 landed, and a real YAML plane over real files.
Every number is from that prototype unless it cites the survey.

**Four of the eleven probes overturned something that was already written down: one an ADR-0009 measurement offered to this ticket in good faith, two decisions this ADR had itself reached in draft, and one rule of ADR-0005's that had never been run against a struct whose only field is a map.**
All four are recorded in full, because the shape of each miss is the argument for the audit that found it.

## Decision

### What this closes, and what it does not

The ticket asked for five things by name, and the owner's comment added a sixth.

| The ticket asked | Closed | Where |
| --- | --- | --- |
| `Load[T]` / `Dump[T]`, or `Load(ctx, &v)` in xload's style | **yes**: `Load[T]`, and the deciding argument is ADR-0006's reload leak rather than ergonomics | [The entry point](#the-entry-point-returns-a-value-and-that-is-adr-0006s-decision-not-a-taste-one) |
| the cache architecture | **yes**: a `sync.Map` per registry, keyed by `{reflect.Type, tagKey}`, with the compile behind a per-entry `sync.OnceValues` | [The cache](#the-cache-is-a-two-level-one-per-registry) |
| what is compiled into a leaf: data, or resolved behaviour | **yes**: resolved behaviour, and the three things it holds are priced separately because only one of them is large | [What a leaf holds](#a-leaf-holds-resolved-behaviour-and-the-three-things-it-holds-do-not-cost-the-same) |
| whether there is a public validation entry point | **yes**, and it is renamed: `Compile[T]()` is `Load`'s compiler reporting whether the schema compiled, and it takes **neither the cache nor the freeze** | [Compile](#compilet-is-the-compiler-and-it-is-not-a-use-of-the-registry) |
| what the cache key actually is | **yes**, plus the rule for what may ever join it and a one-line mechanism that enforces it at build time | [The cache key](#the-cache-key-is-three-components-and-the-rule-is-what-outlives-them) |
| **the walk architecture** (the owner's comment) | **yes**, and there are three axes it can be duplicated on rather than one | [The walk](#the-walk-is-written-once-on-three-axes-and-not-one) |

Two more this ticket inherited from other ADRs:

| Handed over by | Closed | Where |
| --- | --- | --- |
| ADR-0007 and ADR-0009: whether a root leaf is a legal address | **yes: no**, and the rule is asked of the compiled node rather than of the Go kind | [The root](#the-root-must-be-a-struct-ferry-walks-and-the-rule-is-asked-of-the-compiled-node) |
| ADR-0007: where the compiled schema caches the per-type claim | **yes**, in the leaf, resolved once | [What a leaf holds](#a-leaf-holds-resolved-behaviour-and-the-three-things-it-holds-do-not-cost-the-same) |

Four questions this ADR had to answer that nobody asked:

| Not asked for, answered anyway | Where |
| --- | --- |
| where ADR-0006's **seeded value** goes, given a signature with no destination | [The entry point](#the-entry-point-returns-a-value-and-that-is-adr-0006s-decision-not-a-taste-one) |
| whether `json/v1`'s recursive-type placeholder is live for ferry at all | [The cache](#the-cache-is-a-two-level-one-per-registry) |
| what the compile entry point does to ADR-0009's freeze, and what "first use" therefore means | [Compile](#compilet-is-the-compiler-and-it-is-not-a-use-of-the-registry) |
| what the schema cache does to [#25](https://github.com/onhotpath/ferry/issues/25)'s motivating measurement | [What this hands the tickets that were waiting](#what-this-hands-the-tickets-that-were-waiting) |

**Three things this ADR does not close.**

- **The compile-against-walk guarantee covers the static tier only.**
  A dynamic address is minted by the walk and was never in the schema, which is ADR-0003's two tiers and not a gap this ticket can shut.
- **The presence bit is shared mutable state across the scheduler seam.**
  Reproduced under `-race`. It is named as a hazard [#20](https://github.com/onhotpath/ferry/issues/20) inherits rather than fixed here.
- **The cache is unbounded and leaks for `reflect.StructOf`-generated types**, which the ticket already priced as a cost to accept, and which acquires one ferry-specific twist below.

### What a consumer writes

This section is first, because #19's ADR was drafted, reviewed and sent back for arguing from measurements without ever showing the API a consumer meets, and for the ticket that owns the entry point that would be fatal.
It is [ADR-0008](0008-the-struct-tag-grammar.md)'s tag grammar and [ADR-0009](0009-typed-codec-registration.md)'s registration throughout, and the whole of it was run.

```go
package main

import (
    "context"
    "log"

    "github.com/onhotpath/ferry"
    "github.com/onhotpath/ferry/driver/yaml"
)

type DB struct {
    Host string `ferry:"host,required"`
    Port int    `ferry:"port,default=5432"`
}

type Config struct {
    Name string   `ferry:"name"`
    DB   DB       `ferry:"db"`
    Tags []string `ferry:"tags"`
}

func main() {
    ctx := context.Background()

    cfg, err := ferry.Load[Config](ctx, yaml.Source{Path: "app.yaml"})
    if err != nil {
        log.Fatal(err)
    }

    if err := ferry.Dump(ctx, cfg, yaml.Sink{Path: "app.yaml"}); err != nil {
        log.Fatal(err)
    }
}
```

`Load` names its type and `Dump` infers it, which is not an inconsistency: `Load` has nothing to infer from and `Dump` has the value in hand.

The rest of the surface, in full:

```go
func Load[T any](ctx context.Context, src Source, opts ...Option) (T, error)
func LoadOver[T any](ctx context.Context, seed T, src Source, opts ...Option) (T, error)
func Dump[T any](ctx context.Context, v T, sink Sink, opts ...Option) error
func Compile[T any](opts ...Option) error

func TagKey(string) Option        // ADR-0008's
func WithRegistry(*Registry) Option  // ADR-0009's
```

Four functions and two Options.
`ferry.Load` is literally `LoadOver` with the zero value, so nothing is expressible through one and not the other.
On failure both return without a value ferry built: `LoadOver` returns the seed, and `Load` therefore returns the zero value.

**`LoadOver` is ADR-0006's seeded value, and it is also its reload.**
The two are one operation:

```go
// a seed, which is ADR-0006's answer for a composite default a tag cannot spell
cfg, err := ferry.LoadOver(ctx, Config{Tags: []string{"default"}}, src)

// a reload, which is the caller writing the carry-over out loud
cfg, err = ferry.LoadOver(ctx, cfg, src)
```

**In a test**, with no value in hand and no plane reachable:

```go
func TestSchema(t *testing.T) {
    if err := ferry.Compile[Config](); err != nil {
        t.Fatal(err)
    }
}
```

**A library that owns its own tag, which is ADR-0008's case for the Option:**

```go
cfg, err := ferry.Load[Config](ctx, src, ferry.TagKey("mylib"))
```

**And what a mistake looks like**, run:

```
ferry: /Host: field Host carries no ferry tag: every exported field must name
       the segment it addresses, or be marked ferry:"-"
ferry: /Nope: unknown option "requird"
ferry: /port: default "abc" is not a valid int: strconv.ParseInt: parsing "abc":
       invalid syntax
```

One error per field, joined and sorted, per ADR-0001's determinism invariant.

### The entry point returns a value, and that is ADR-0006's decision, not a taste one

> `Load` produces a new value.
> There is no destination parameter, so there is no way to hand ferry a value a previous `Load` populated without saying so at the call site.

Four candidates were implemented against the same compiled schema and the same real YAML plane, and all four run:

```
A  var cfg Config; err := ferry.Load(ctx, &cfg, src)            xload's shape
B  cfg, err := ferry.Load[Config](ctx, src)
C  cfg, err := ferry.LoadOver(ctx, Config{}, src)
D  sc, _ := ferry.New[Config](); cfg, err := sc.Load(ctx, src)
```

ADR-0006 is what separates them, and it is not close.
Measured, loading twice with `/db/host` deleted from the plane in between:

| | second load |
| --- | --- |
| A, into the same `cfg` | `DB.Host="db1"` - **the previous load's value** |
| B | `DB.Host=""` |
| C, `LoadOver(ctx, cfg, src)` | `DB.Host="db1"` - the caller asked |
| C, `LoadOver(ctx, Config{}, src)` | `DB.Host=""` |
| D | `DB.Host=""` |

**A is the only shape whose ordinary call site is the defect.**
ADR-0006 says this in as many words - "loading into a destination that already holds a previous load's results is not the operation this ADR describes, and offering it without saying so would ship the erasure defect ADR-0001 is trying to avoid" - and a signature taking `*T` offers exactly that as its only spelling.

The type parameter's other win is real and smaller.
`Load[T]` deletes `ErrNotPointer`, because there is no `&` to forget: measured, `e1LoadA(ctx, cfg, src)` returns `ErrNotPointer` at run time and the generic form is a build error.
**`ErrNotStruct` survives under every candidate**, and this ADR does not claim otherwise.
Verified by compiling on `go1.27rc2`: `interface{ ~struct{} }` is a legal constraint whose type set is exactly the types whose underlying type is the *empty* struct, there is no wildcard form, and `Load[int]` therefore compiles.
What it produces is a schema-compile refusal, which is the root-leaf rule below.

**ADR-0006's seeded value is the reason `LoadOver` exists at all, and B alone loses it.**
ADR-0006 partitions the two default mechanisms explicitly - "declared defaults for leaves, seeded values for composites and for anything a tag cannot spell" - and a composite default does not compile.
Measured, loading from a plane holding no `tags`:

```
B  Load[Config](ctx, src)                                Tags=[]
C  LoadOver(ctx, Config{Tags: []string{"seeded"}}, src)  Tags=[seeded]
```

So the shape is not "B or C", it is C with B as its zero-valued sugar.
That is ADR-0009's own answer to 5.14's first item reused: a default registry is a `Registry`, and package-level `Register` is a method call on it rather than a second path with its own rules.

**D is not refused here, because it is not this ticket's to refuse.**
A `Schema[T]` value the caller holds is a caller-facing bind-then-load split, which is [#25](https://github.com/onhotpath/ferry/issues/25)'s question by name.
What this ADR removes is one of its two motivations, measured below.

#### On failure it yields the seed, which is [#9](https://github.com/onhotpath/ferry/issues/9)'s constraint read once rather than twice

[ADR-0011](0011-the-error-model.md) ([#9](https://github.com/onhotpath/ferry/issues/9)) imposes a constraint on this signature from a ticket that does not own it, and says so: **when a Load fails, ferry yields no value.**
Aggregating means the walk continues past a failed field, so a partially populated destination exists inside ferry either way; ADR-0011 rules that it does not cross the boundary, on the grounds that a process which starts with `/db/host` set and `/db/port` zero is the worst available outcome because it runs and is wrong.

This ADR agrees, and the rule is honoured **as a property rather than as a promise**: the walk builds into a copy of the seed, so the partial is unreachable from the caller whatever happens.

**The rule has two readings at `LoadOver`, and ADR-0011 could not have seen the second**, because it was written against a signature with no seed in it.
Measured, a good reload followed by a bad plane, with the error ignored:

| on failure `LoadOver` returns | the caller's live config becomes |
| --- | --- |
| the zero value | `{Host: Port:0 Retries:0}` |
| **the seed** | `{Host:db1 Port:5432 Retries:3}` |

The zero reading **destroys a value ferry never touched**.
It is the outcome ADR-0011 rules out a partial for, reached through the other door and doing more damage, because the caller had a good value a moment earlier and now does not.

> `LoadOver` returns the **seed** on failure.
> ADR-0011's rule is read as *ferry yields no value it built*.

`Load[T]` returning the zero value then falls out rather than being a second rule, because `Load` is `LoadOver` with the zero seed, and the result is byte-identical to ADR-0011's own measurement.

**And the cost ADR-0011 names is already paid.**
It records that yielding nothing rules out a `LoadInto(ctx, src, &cfg)` shape, because "yield no value" there would mean ferry zeroing a struct the caller owns.
This ADR rules that shape out one section earlier on ADR-0006's reload leak, so the two constraints point the same way and neither is doing the other's work.

#### On the name, which was scanned rather than argued

ADR-0001 left the exported verb names open and this ADR spends two of them.
`Load` and `Dump` are the working assumptions confirmed, and `Compile` replaces ADR-0008's `Validate` on ADR-0001's own words.
`LoadOver` is new, and the candidates were scanned against every `api/go1*.txt` and the module cache rather than weighed by ear:

| candidate | stdlib | why not |
| --- | --- | --- |
| **`LoadOver`** | 0 | taken |
| `LoadInto` | **0**, and the corpus meaning is unanimous | every `-Into` in Go names a destination it **mutates**: `DeepCopyInto(out *T)`, `MergeInto`, `cloneInto`, `readDataInto`. It would set the reader's expectation to the signature this ADR deletes, and `_, err := LoadInto(ctx, cfg, src)` compiles, checks its error, leaves `cfg` untouched, and is flagged by nothing. |
| `LoadSeeded` | `Seed` x6 | ADR-0006's own noun, which is the argument for it. Against it: all six stdlib `Seed`s are randomness or crypto, so a Go reader's first association is entropy; and it names only the seed half, not the reload half. |
| `LoadOverlay` | 0 | "Overlay" is right and is **configuration** vocabulary, and ADR-0001 is explicit that ferry is not a configuration library. Borrowing it tilts the charter for a word. |
| `Reload` | 0 | names one of the two uses. A first-ever load with a seed is not a re-anything, and [#13](https://github.com/onhotpath/ferry/issues/13) may want the verb. |

`go vet`'s `unusedresult` check covers a fixed list of pure stdlib functions, so no tool catches the `LoadInto` misuse above.
That is what disqualifies it rather than the reading.

The one candidate with no name at all is folding both into `Load(ctx, seed, src, opts...)`, which deletes this question and 5.14's "two ways" exposure outright, at the cost of `Config{}` on every ordinary call site forever.
It is recorded as the runner-up rather than dismissed: the difficulty of naming the second verb is real evidence for it, and the deciding argument the other way is that the common call site is the one that should read best.

### The cache key is three components, and the rule is what outlives them

> The schema cache is keyed by the type, the struct tag key, and the registry.
> The registry is the outer level: the cache hangs off the `*Registry`, and the inner key is `struct{ reflect.Type; string }`.

Nothing in that sentence is this ticket's to decide.
ADR-0008 put the tag key in, ADR-0009 put the registry in, and both measured what happens without it.
Both were re-run against a real compile rather than inherited:

```
one reflect.Type, tag key "ferry" -> [/host /port]
one reflect.Type, tag key "mylib" -> [/HOST /PORT]

registry A wants big.Int as text   -> string("1099511627776")
registry B wants big.Int as number -> number("1099511627776")

with a sync.Map keyed by reflect.Type alone:
  service A compiles first -> string("1099511627776")
  service B hits the cache -> string("1099511627776")   <- B silently got A's codec
```

**And here is the number two ADRs offered this ticket in good faith that does not survive.**

ADR-0009 measured three shapes and recommended the third: a `{reflect.Type, *Registry}` pair key at 32 ns/op, `reflect.Type` alone at 9 ns/op, and **10 ns/op with the per-type cache hung off the registry, "so the outer lookup is a pointer dereference and the inner one keeps the stdlib's shape"**.
That last shape requires the inner key to be a bare `reflect.Type`.
ADR-0008 landed on the same day and put a `string` in it.

Re-measured on ferry's actual three components:

| shape | ns/op | allocs/op |
| --- | --- | --- |
| `sync.Map[reflect.Type]` | 11.0 | 0 |
| `sync.Map[{Type, *Registry}]` - ADR-0009's pair | 31.6 | 0 |
| `registry.schemas: sync.Map[reflect.Type]` - ADR-0009's nesting | 10.9 | 0 |
| `sync.Map[{Type, string, *Registry}]` - the flat key | 41.2 | 0 |
| **`registry.schemas: sync.Map[{Type, string}]`** - the nesting that is available | **33.7** | **0** |
| `registry -> sync.Map[tag] -> sync.Map[Type]` - nested twice | 24.4 | 0 |
| `map[reflect.Type]`, plain | 14.1 | 0 |
| `map[{Type, string}]`, plain | 18.9 | 0 |

The two ADR-0009 rows reproduce, and neither is reachable.
**The 10 ns nesting is not available to #16**, and the ADR says so rather than quoting a number that was measured against a key ferry does not have.

The shape taken is the fifth row, at 34 ns and no allocations, and the sixth row is available if it is ever worth 9 ns.
It is not the argument for anything: E3 prices the compile the cache is saving in the tens of microseconds, and ADR-0003 measured a bare plane lookup at 8.5 ns, so every row here is inside the noise of one `Get`.
**The key's shape is a correctness question and its cost is the argument for none of them.**

#### The rule, which is the part with no owner

ADR-0006 opened this and left it, and the reason it needs closing is that the two Options in the key today arrived from two ADRs that did not know about each other.
A third will arrive the same way.

> An Option is **compile-affecting** if one `reflect.Type` yields two different schemas under two values of it.
> A compile-affecting Option is part of the cache key, and its value must be comparable.
> An Option that is not compile-affecting must not be in the key.

Measured against the three Options the prototype has, each against a type on which that Option could possibly matter:

| Option | two values, same schema? | |
| --- | --- | --- |
| `TagKey` | no | compile-affecting, in the key |
| `WithRegistry` | no | compile-affecting, in the key |
| `Observe`, ADR-0006's presence observation | yes | load-affecting, not in the key |

`Observe` is in the table because the rule needs a row that is not in the key, and it is **not proposed here**: ADR-0006 put the spelling of the presence observation with the caller-facing lifecycle, which is [#25](https://github.com/onhotpath/ferry/issues/25)'s.
What it establishes is that the class exists, so the rule is not a rule with one side.

A rule with no mechanism is prose, and the mechanism here is one line.
ADR-0006's measurement of the bad end is a **runtime panic**:

```
sync.Map.Load(unhashableKey{}) -> PANIC: runtime error: hash of unhashable type
```

A `sync.Map` takes `any` keys, so it cannot do better.
A plain map can, and it does it at build time - verified on `go1.27rc2`:

```
invalid map key type e2Unhashable
```

So the compile-affecting Options are collected into a **named key struct**, and core carries a static assertion that a plain map can hold it:

```go
type schemaKey struct {
    typ    reflect.Type
    tagKey string
}

var _ = map[schemaKey]struct{}{}
```

The named struct is what makes adding a field to it a deliberate act rather than a side effect of adding an Option.
The assertion is what turns ADR-0006's measured panic into a build failure, for free, and it is the only mechanism available: nothing else in Go checks it.

### The cache is a two-level one, per registry

```go
type cacheEntry struct {
    once func() (*schema, error)   // sync.OnceValues
}
```

`Load` on the outer `sync.Map`, build a **cheap** entry on a miss, `LoadOrStore` it, and call `once()`.
An entry that loses the race is discarded before its `once` ever runs, which is `encoding/json/v2`'s two-level pattern rather than v1's.

Measured, 64 goroutines against a cold cache, over 20 trials with a fresh cache each time.
One trial is not a measurement here, because whether the naive form duplicates work depends on how much of the herd arrives before the first compile finishes:

| | compiles per trial | worst trial |
| --- | --- | --- |
| `sync.Map`, expensive work before `LoadOrStore` | 1.8 | **5** |
| the two-level form | 1.0 | 1 |

The worst case is the number to quote rather than the mean, because it is bounded only by the width of that window, and E3 measures the window in tens of microseconds.

`encoding/gob` states the philosophy for the first form outright - "if we lose the race, we'll waste a little CPU and create a little garbage but return the existing value anyway" - and eight of eight stdlib type caches accept it.
For ferry the wasted work is a whole schema compile including ADR-0007's chain, which probes method sets per type.

**`sync.OnceValues` is not here for speed, and this ADR does not claim it is.**
The golang commit that put it into `encoding/json` says the motivation was `testing/synctest` correctness and not performance.
Measured on a steady-state hit: 3.55 ns/op through `once()` against 0.66 ns/op for a plain read, so it costs rather than saves.

What it buys is exactly-once initialisation and **identical replay**, which is a correctness property and the actual reason for the second level:

```
call 1 -> re-panicked "malformed schema"  (f ran 1 time)
call 2 -> re-panicked "malformed schema"  (f ran 1 time)
call 3 -> re-panicked "malformed schema"  (f ran 1 time)
```

Without it the first caller panics and later callers receive a **zero schema**, which for ferry is an empty address set: a `Load` that reads nothing and returns nil.
Errors replay the same way, which matters more, because ferry's compile returns errors rather than panicking.

#### `json/v1`'s placeholder is not live for ferry, and this is checked rather than assumed

v1 installs a placeholder closure **before** the real encoder exists, so a self-referential type's inner lookup finds the indirection instead of recursing forever.
It is the obvious thing to copy and it would be inheriting a mechanism whose hazard ferry has already refused.
Two facts, both measured:

```
(i)  ADR-0005 refuses a recursive type at schema compile, from the type alone:
     ferry: /next: main.E4Node is recursive, so its address set is unbounded;
            register a codec for it

(ii) the cache is keyed per ROOT type, not per visited type. Compiling
     struct{ A inner; B inner } adds exactly 1 cache entry.
```

(ii) is the load-bearing half and it is a consequence of ADR-0003 rather than a choice.
A nested struct's addresses depend on the path from the root, so the same type under two parents compiles to two different address sets and its subschema is not reusable.
A compile therefore never performs a cache lookup, so it can never look up a type it is in the middle of compiling, so there is nothing for a placeholder to break the cycle of.

ferry adopts json/v2's reason for the second level and not json/v1's.

### A leaf holds resolved behaviour, and the three things it holds do not cost the same

> A compiled leaf holds its **codec function pointer**, its **static address shape**, its **compiled default** as a `Value`, and its `required` and `omitzero` flags.
> ADR-0007's three steps and ADR-0008's tag parse are asked once per position per schema, never per call.

This is the research's "single most transferable idea" and ADR-0007 already assigned the codec half here by name.
What this ADR adds is that the three things are not one win, and only one of them is large.

Measured on a twelve-leaf struct with two nested structs, which is research 5.3's shape:

| | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| compile per load, then walk | 52306 | 26424 | 1371 |
| **compile once, walk, through `Load`** | **5673** | **3080** | **126** |
| the compile alone | 47370 | 23384 | 1247 |

> **Footnote added under [#41](https://github.com/onhotpath/ferry/issues/41): what this compile did and did not include.**
> The figure was taken with ADR-0007's chain switched off - `chainOrder` was `nil` and `chainBeforeKind` was `false` on the prototype - so the sentence in [The cache is a two-level one, per registry](#the-cache-is-a-two-level-one-per-registry) calling it "a whole schema compile including ADR-0007's chain, which probes method sets per type" described work this measurement did not do.
> Turning the chain on is **free** on this fixture: 44560 B and 2577 allocs with it off and with it on, identical to the byte, because the compiler already ran a recursive kind walk per leaf and three `reflect.Implements` probes cost the same.
> So the sentence is now true rather than corrected.
>
> What did move the number is ADR-0003's prefix-free scan, which the prototype was not doing and now is: roughly a doubling, from ~52 us to ~93 us on the same fixture, and the current tip reads 90631 ns for the compile alone against 6962 ns for a cache hit.
> **The ratio is what this section argues and the ratio got better**, from nine times to thirteen, so nothing here is weakened.
> No number in the table above is edited, because the whole point of the two-level cache is that a compile happens once: optimising it is moot behind the cache, and a footnote is the right weight for a figure nobody should be tuning against.
> Evidence: `E16=3` on [`proto/tip`](https://github.com/onhotpath/ferry/tree/proto/tip), and commit `2e22bbf` for the three-state comparison.

Nine times, on this prototype, and the same shape on Dump: 54842 ns/op against 6505 ns/op.
The absolute numbers are the prototype's and not ferry's - its chain is unmemoised and its `Path` allocates per segment - and research 5.3's own figures on xload are 3343 ns against 476 ns.
What transfers is the ratio and its direction, and both reproduce.

The three components, priced separately:

| | |
| --- | --- |
| **codec resolved at compile** | 5486 ns/op |
| codec resolved per leaf per call | 6027 ns/op, **1.10x** over 12 leaves |
| reading the compiled `Path` out of a leaf | 1 ns/op |
| re-minting `/auth/user` from segments | 54 ns/op |
| a declared default, already a `Value` | 85 ns/op, 1 alloc |

**So the compile is the whole cost and the resolved codec is a modest separate win**, which is ADR-0009's own finding (283 ns against 381 ns) reproduced at a different scale and pointing the same way.
ADR-0009 also said the reason to resolve at compile is not performance, and this ADR agrees and states the real one: **ADR-0009's staleness result**.
A codec looked up per call against a mutable table gives one type two representations in one process; a codec baked into a schema against a frozen registry cannot.

One thing is deliberately **not** resolved into the leaf, and ADR-0006 decided it: a declared default is held as a `Value` and decoded fresh on every load, because a cached Go value is aliased across every load of one schema.
ADR-0006 measured the aliasing and priced the re-decode at 30.6 ns per default per load.
What compile does own is the text-to-`Value` step and its validation, which is what makes `default=abc` on an `int` a schema-compile error with no value in hand.

### The walk is written once, on three axes and not one

The owner's comment states the constraint from survey item **5.2**: xload's `doProcess` and `processAsync` are ~90 duplicated lines of reflection that have already drifted and return different results for the same input, and its own equivalence test cannot catch it.

**ferry has three axes on which a walk can be duplicated, and xload only has the third.**
Writing one walk function answers the third and does nothing for the other two, which is why "write the walk exactly once" is necessary and not sufficient.

| axis | who has it |
| --- | --- |
| 1. the **compiler** against the **walk** | ferry, found by ADR-0008 in a real prototype, silent |
| 2. **load** against **dump** | ferry, structurally - xload has one direction |
| 3. **serial** against **concurrent** | xload's, and [#20](https://github.com/onhotpath/ferry/issues/20)'s |

#### Axis 1: the schema is the list the walk iterates

Reproduced, by adding ADR-0008's promotion rule to a compiler and not to the walk that reads its output:

```
type Conf struct { Common; Port int `ferry:"port"` }

the compiler's address set : [/env /name /port]
the walk's address set     : [/Common/env /Common/name /port]
```

Two rules, one type, two answers, and no error from either.
The load side is the silent half: the schema promises `/name`, the walk looks at `/Common/name`, and the field stays zero with `err=nil`.
ADR-0008 found exactly this and drew the right conclusion - "the walk and the schema compiler must share one field rule" - and this ADR is where that becomes structural rather than a rule someone has to remember.

> The address set is not computed by the compiler and re-computed by the walk.
> It is a field of the thing the walk iterates.

The walk takes a compiled node tree and reads `node.shape`, `node.fields`, `node.codec`.
It cannot visit a field the schema does not hold, and the schema cannot hold a field the walk will not visit, because they are one list.

#### Axis 2: three operations differ, and the ADR does not claim zero

`load` and `dump` are two directions over one type and the temptation is to claim they are one function.
They are not, and the residue is exactly three operations:

| | |
| --- | --- |
| `leaf` | Dump encodes a Go value and writes; Load reads, decodes, applies a default for `Absent`, and enforces `required`. |
| `container` | "Is this container's own address carrying the whole answer?" Dump writes `Null` for a nil-or-empty composite; Load reads a `Null` and zeroes. One question, two answers. |
| `members` | Dump reads a map's keys off the **value**; Load enumerates the **plane**. |

`members` cannot be shared at all, and that is ADR-0004 rather than an implementation limit: Load of a dynamic address is conditional on the source implementing `Enumerator` and Dump is unconditional, so the two are different capabilities.

Everything else is written once: which nodes exist and in what order, how a realised address is minted under a dynamic parent, how a pointer is materialised from ADR-0006's presence bit, how a map value is made addressable and put back, where the context is checked, and where the scheduler sits.

Counted on the prototype, non-comment lines: **the shared structure is 129 lines and the two directions are 48 and 70**, and every line of both is inside one of the three hooks.
xload's figure for the same ratio is ~90 duplicated lines and nothing shared.

#### Axis 3: the scheduler is a seam, and #20 inherits a hazard rather than a drop-in

> A concurrent mode, if [#20](https://github.com/onhotpath/ferry/issues/20) wants one, is a second **scheduler** and never a second walk.

```go
type sched func(tasks []func() error) error
```

The walk hands a scheduler a batch of sibling tasks at each container.
Measured, the same walk under a `sched` value and under the identical loop inlined: 1141 ns/op against 1079 ns/op, on a three-leaf schema.

**That refines the cost the owner's comment priced.**
It is one indirect call per **container**, not per leaf, because the scheduler is handed siblings as a batch.

**And it turns out to serve [#9](https://github.com/onhotpath/ferry/issues/9) as well as [#20](https://github.com/onhotpath/ferry/issues/20), which neither ticket asked for.**
ADR-0011 aggregates, which means the walk continues past a failed field.
That is a property of the scheduler and not of the walk: measured, the same walk function under a first-error scheduler and under an aggregating one, over a plane with two unparseable leaves, gives 1 error and 2 errors respectively, with the walk byte-identical between the rows.
So "a concurrent mode is a scheduler and never a second walk" is the same seam that keeps #9's aggregation out of the walk.

And one thing #20 inherits that is a real hazard rather than a note.
ADR-0006 made the walk return a presence bit per subtree, and flagged that "a concurrent walk would have to combine those bits rather than only its errors".
Reproduced under `-race` with a goroutine-per-task scheduler dropped in unchanged:

```
WARNING: DATA RACE
Read at 0x... by goroutine 15:
  main.(*walker).walk.func1()   e_walk.go:94    present = present || p
```

So the seam exists and it is not free: #20's scheduler cannot be plugged into this walk without the presence bit being combined rather than OR-ed in place.
That is handed over explicitly, because a seam that looks like a drop-in and is not is worse than no seam.

#### And the equivalence test, which is the other half of the constraint

Survey 5.2's sharpest detail is that xload's own serial-against-concurrent test *cannot* catch its divergence, because `input` is a pointer built once in the table literal and both subtests share it: the serial subtest populates it, and any field the concurrent path fails to set is still correct.

Reproduced on ferry's own walk, with a deliberately broken second walk that skips every leaf:

```
shared destination, walk 1 (good)   -> {Common:{Name:n Env:e} Port:8080}  equal=true
shared destination, walk 2 (broken) -> {Common:{Name:n Env:e} Port:8080}  equal=true   PASSES
FRESH destination,  walk 2 (broken) -> {Common:{Name: Env:} Port:0}       equal=false  caught
```

> Every equivalence subtest gets its own destination.

That is a rule about ferry's tests rather than about its API, and it belongs here because it is half of the constraint the owner's comment set and the half that is easy to lose.
It is also cheaper to honour than it was for xload: `Load` returns a value, so a subtest that wants a shared destination has to write one.

### `Compile[T]()` is the compiler, and it is not a use of the registry

ADR-0008 named this entry point `Validate[T]()` and required that it and `Load` share one compiler, because two entry points that could disagree about whether a type is legal would be viper's two-engines defect at ferry's own front door.
That requirement is kept in full.
**The name is not**, and the amendment is argued below rather than done quietly.

> `Compile[T](opts ...Option) error` is `schemaFor(reflect.TypeFor[T](), opts)`, reporting whether the schema compiled.
> Not a second compiler with the same rules - the same function.

#### Why it is not called `Validate`

ADR-0001 rules out **a validation constraint language in struct tags** by architecture, and gives the reason as a rule rather than a preference: *validation follows parse-don't-validate: the type is the validation.*
A package that rules out validation and then exports `Validate` is telling a reader something untrue about itself, and the godoc is where that will actually be read.

The function does not breach the rule, and it is worth being exact about why, because "it is called Validate but it is fine" is not an argument.
ADR-0001's rule is about **values**: do not accept an untyped value and check it, make the invalid state unrepresentable in the type.
This entry point never sees a value.
It has no `Source` parameter, so no plane is reachable, and no value of `T` is constructed at any point.
What it asks is whether the **program's own annotation** is well-formed, from `reflect.TypeFor[T]()` alone, which is the assertability property ADR-0001 claims for tag rejection in the first place.

So the rule is not breached and the word is still wrong, and the word is the part a user meets.

#### What it is actually for, which is narrower than ADR-0008 asked for

ADR-0008 asked for "a validation entry point a user can call from a test".
Measured, against the obvious alternative of calling `Load` against an empty memory plane:

```
Compile[Conf]()                    -> <nil>
Load[Conf] from an EMPTY plane     -> ferry: /host: required, and the plane supplied nothing

Compile[Bad]()                     -> ferry: /Host: unknown option "requird"
Load[Bad] from an EMPTY plane      -> ferry: /Host: unknown option "requird"
```

**For a malformed annotation the two return the same error**, so the entry point adds nothing there.
It earns its place on one row only: it separates *"your annotation is wrong"* from *"your plane is missing something"*, which a `Load` against an empty plane conflates because `required` fires.
ADR-0006 already recorded the same fact from the other end, that a defaulted zero value is not reachable by `Load` alone.

That is the whole justification, and it is smaller than the one ADR-0008 wrote.
It is also sufficient, because the two questions have different answers and a user testing a config struct's annotation is asking the first one.

#### Why it discards the schema, which is not this ADR's choice

Parse-don't-validate, applied to this function, says do not return an error from a check - return the parsed artefact:

```go
func Compile[T any](opts ...Option) (*Schema[T], error)   // the parse
func Compile[T any](opts ...Option) error                 // the parse, discarded
```

The first is what the principle actually asks for, and it is not available here.
ADR-0001 leaves "whether core ever exports a read-only schema view" open and says to reopen it only if a concrete need survives the dump-into-a-recording-sink pattern; and a caller-held handle is [#25](https://github.com/onhotpath/ferry/issues/25)'s bind-then-load question by name.
So there is nothing for a parse to hand back.

> The discard is a consequence of ADR-0001 keeping the compiled schema unexported, not a choice #16 made.

Two things follow and are stated rather than left.
`regexp.Compile` returns its artefact, so `Compile[T]() error` diverges from the shape a Go reader expects of that verb, and this ADR takes the divergence knowingly because the alternative is exporting a schema ADR-0001 has not agreed to export.
And if [#25](https://github.com/onhotpath/ferry/issues/25) later concludes that a caller may hold a compiled schema, **this function is where it lands** and its signature becomes `(*Schema[T], error)`, which is a breaking change and is affordable for exactly the reason ADR-0002 keeps ferry at v0.

#### It takes the same Options, and ADR-0008 already forced half of that

```
Compile[Lib]()                -> ferry: /Host: field Host carries no ferry tag ...
Compile[Lib](TagKey("mylib")) -> <nil>
```

ADR-0008 left the other half open by name - "whether they also see the same **codec registry** is #19's, and it is the other thing that could make them disagree".
The answer is yes, and it is forced by ADR-0007 making a codec collapse a type to a leaf, so a registration decides whether a type compiles at all:

```
Compile[Conf]()                  -> ferry: /listen: main.Opaque maps no address ...
Compile[Conf](WithRegistry(reg)) -> <nil>
```

#### And here is what nobody has looked at, including this ADR's own draft

ADR-0009 made a registry freeze at its **first use**, which it defines as the first schema compiled against it, and its soundness argument runs through where that falls.
`Compile` compiles a schema, so on ADR-0009's wording it freezes.

**An earlier draft of this ADR decided exactly that, and it was wrong.**
It weighed two readings, found both loud, and picked freezing because a non-freezing `Compile` "reports a failure that never happens".
It never asked the next question, which is *loud to whom*.

The measurement that settles it needs a type that compiles **both ways with different representations**, which ADR-0007's chain-before-kind makes ordinary and which the draft's own fixture did not contain.
A type carrying a text pair, with a registration that would give it a different boundary kind:

```
unregistered, the chain claims it        -> /level = string("WARN")
registered, the table claims it          -> /level = number("2")

Compile first, Register's error DROPPED  -> /level = string("WARN")   err = <nil>
```

An early `Compile` freezes the registry, the later `Register` fails, and **a caller who does not check that error gets a plane holding the representation they replaced, with no error anywhere**.
That is ADR-0009's staleness defect, which the freeze exists to prevent, reached through the freeze.
The draft's first fixture for this used a type that only compiles *with* the codec, so the failure was loud and the probe measured nothing; that mistake is recorded rather than replaced, because it is the same shape as #12's and #19's worst ones.

The same sequence with a non-freezing `Compile`:

```
Compile before the registration          -> <nil>
Register's error DROPPED, then Dump      -> /level = number("2")
```

> `Compile` takes **neither the cache nor the freeze**, and it is one omission rather than two.

**The rule underneath, which is a better statement of ADR-0009's obligation than "first use".**
ADR-0009's sentence is that *once a type has been resolved against a registry, that registry's answer for that type must never change*.
That is a constraint on a resolution which is **retained**.
A compile whose result is discarded resolves nothing that outlives the call, so there is nothing to go stale.

> Caching and freezing are one decision, not two.
> A call that retains a compiled schema freezes the registry; a call that discards one does not.

Measured, which is the whole difference between the two entry points:

```
after Compile[Conf](WithRegistry(fresh))  -> frozen=false, a later Register is accepted
after a Load                              -> frozen=true,  a later Register is refused
```

**So `Compile` may be called anywhere**, including from a package-level variable during `init`, which is the shape the draft was about to make dangerous.
ADR-0009's one broken shape, a **`Load`** during `init`, is unchanged and still broken, because a `Load` retains its schema.

The residual cost is stated rather than hidden: a `Compile` that runs during `init` answers about a registry a later `init` may still add to, so it can report a failure a later registration would have fixed.
That is loud, it is at the `Compile` call, and it costs the user a moved line.
The freezing reading's failure is a plane holding a representation nobody chose.

#### The amendment to ADR-0008, stated plainly

ADR-0008 is Accepted and merged, and it spent the name.
This ADR changes it, on the grounds above, and the change is:

> **`Validate[T]()` in ADR-0008 is renamed `Compile[T]()`.**
> Nothing else about ADR-0008's decision changes: it is still the same compiler as `Load`, it still takes the same Options, and it still exists because the toolchain catches neither a tag `reflect.StructTag.Get` truncates nor a field carrying two ferry tags.

A one-line pointer is added to ADR-0008 at the point where the name is decided, so a reader arriving there is not left with a name that no longer exists.

### The root must be a struct ferry walks, and the rule is asked of the compiled node

ADR-0007 handed this over: "a chain-admitted type at the root mints the empty path, which ADR-0003 says an address may not be... this is pre-existing - a root `int` does the same - and belongs to #16's entry point, but the chain enlarges the set of types that can sit there."
ADR-0009 repeated it for a registered type.

Measured, over nine root types:

| root type | |
| --- | --- |
| `struct{...}` | `[/v]` |
| `*struct{...}` | `[/v]` |
| `int` | **refused** |
| `time.Duration`, an identity-table leaf | **refused** |
| `netip.Addr`, a **struct** the chain claims | **refused** |
| `big.Int`, a **struct** a registration claims | **refused** |
| `map[string]int` | **refused**, see below |
| `[]string` | **refused**, see below |
| `[2]string` | **refused**, see below |

Read the rows rather than the summary.
`netip.Addr` and `big.Int` are structs and are refused, because ADR-0007's chain and ADR-0009's table are consulted first and collapse them to a leaf at the empty path.

> The root must be a struct ferry **walks**, decided after the chain and the registry have been asked rather than before.

That is a rule the entry point's signature cannot express and only the compiler can, which is why it lands at schema compile rather than at `Load`.
It is also why `ErrNotStruct` surviving, which the research and the ticket both predicted, is true in a narrower way than it sounds: the refusal is not "your Go kind is not struct", it is "what you gave me compiled to a leaf".

**What a root leaf actually does, with the check removed**, is the finding:

```
the address it mints : "" (IsRoot=true), value number("8080")
YAML sink Dump       : err=<nil>, wrote "{}\n"
```

Not a panic and not a wrong value.
The sink writes an empty mapping and returns a nil error, so the value is **silently and totally lost**.
That is ADR-0005's maps-no-address class arriving at the one address ADR-0003 forgot to protect, and it is what makes this a refusal rather than a documented sharp edge.

#### A root map and a root slice are refused too, and an earlier draft got this wrong

The draft let them through, on the grounds that they mint non-empty addresses and that "plane-to-plane transfer is exactly the caller who would depend on it".
Both halves are wrong and the second is wrong on an ADR that was already written.

**The address claim holds for a populated one only.**
Measured:

```
map[string]int{"a":1}  ->  /a = number("1")
map[string]int{}       ->  "" = null          <- the empty path
map[string]int(nil)    ->  "" = null
[]string{"a"}          ->  #0 = string("a")
[]string{}             ->  "" = null
[]string(nil)          ->  "" = null
```

A composite with no elements writes `Null` at its own address, which ADR-0005 decided and this ADR does not reopen.
At the root that address **is** the empty path, so a root map reopens the exact hole the root-leaf rule closes, at the value it will most often have on a first run.
Through the real YAML sink it writes `{}` and returns a nil error, which is the same silent total loss measured above.

**And the justification cited the wrong mechanism.**
ADR-0006 already measured that plane-to-plane transfer has two shapes, and that the plane-preserving one is address-to-address, "a loop from `Reader.Get` into `Writer.Set`, [which] builds no Go value and never runs this ADR's rules at all".
It never calls `Load[T]` at any type.
The struct-mediated shape uses a struct.
So the caller the draft named as depending on the permission does not use it.

Three further costs, none of which was priced:

- **The static address set handed to `Bind` is empty**, so ADR-0003's driver-side injectivity rule is vacuously true over the whole schema, before any I/O.
- **ADR-0008's naming rule never fires**, because there is no field, so no segment name was written by a human. That is the property ADR-0008 spent thirty tags on a thirty-field struct to buy.
- **Load needs an `Enumerator` for every address**, so the type is loadable from a strict subset of the planes a struct is.

An array is refused for the same family of reasons: `[0]T` at the root mints nothing at all, which is a `Dump` that writes zero addresses and returns nil.

The remedy is one line and it gives the user back the thing ADR-0008 wanted them to have:

```go
type Labels struct {
    M map[string]string `ferry:"labels"`
}
```

**And this is the reversible direction, which is the rule the draft broke in order to allow it.**
Nobody can depend on a refusal, so admitting root dynamic composites later is additive; the draft argued the opposite way round on a caller that turned out not to exist.

### Three defects found by running the decisions

All three were found by running rather than by reading, and all three are in decisions this ADR had already reached in draft.

**`Compile` freezing the registry**, above.
The fixture that would have caught it needs a type that compiles both ways with different representations, and the draft's fixture used a type that only compiles with the codec, so the failure was loud and the probe measured nothing.
That is #12's and #19's worst-defect shape exactly: a fixture in which the case cannot go wrong quietly.

**A root map and a root slice**, above.
Found by asking what a root dynamic composite does at its **empty** value, which no probe had asked because every fixture populated it.

**The maps-no-address backstop counted the wrong thing.**
ADR-0005 requires that every struct visited during schema compile contributes at least one address.
The compiler counted **static leaf addresses**, so a map or a slice field - which contributes only a dynamic shape - counted as nothing, and `struct{ Limits map[string]int }` was refused as mapping no address.
ADR-0005's own worked example compiles that type, yielding `/Sl/*` and `/M/*`.
The rule is over **minted address shapes** and not over the static tier, and stating that is this ADR's, because ADR-0005 wrote the rule before the two tiers had a compiler to be counted in.
Found three probes after the check was written, because every earlier fixture gave each struct a scalar field.

### The unbounded cache, and the one thing ferry adds to it

The ticket priced this as a cost to accept: "an unbounded cache that leaks for `reflect.StructOf`-generated types. Every library surveyed documents this rather than solving it."
Confirmed, and the research found no eviction in any of the eight stdlib caches or in any of the five third-party libraries it read.
Measured: 200 `reflect.StructOf` types produce 200 cache entries, none evictable.

What is ferry-specific is that the cache hangs off a **registry**, so the leak has a second door: a per-call registry would leak the same way for ordinary, statically declared types.
ADR-0009 already measured that ("10000 per-call registries -> 10000 cache entries, none evictable") and concluded that a registry must be long-lived.
This ADR restates it as a property of the cache rather than of the registry, because that is where it bites.

### What this hands the tickets that were waiting

**[#25](https://github.com/onhotpath/ferry/issues/25), how a caller holds a binding.**
This ADR does not decide it and it removes one of its two motivations.
#25 opens on ADR-0004's measurement of 158 ns/1 alloc for bind-once against 2743 ns/60 allocs for bind-and-open-every-load, and asks whether ferry exposes a caller-facing bind-then-load split at all.
Those numbers are about **`Bind`**, and the schema cache is about the **compile**.
Measured here, the compile is 47370 ns and the cached lookup that replaces it is 34 ns, so a caller-held `Schema[T]` value buys nothing the cache does not already give, and #25's question narrows to the binding alone.
Candidate D above is #25's to take or leave; what it can no longer be argued on is schema-compile cost.

**[#20](https://github.com/onhotpath/ferry/issues/20), concurrency.**
The walk takes a `sched` and nothing else about concurrency is decided.
Two things are handed over rather than left: the presence bit is shared mutable state across that seam, reproduced under `-race`; and the walk checks `ctx.Err()` at every node entry, which is a placement #20 may change but which had to be somewhere for the walk to be written at all.

**[#13](https://github.com/onhotpath/ferry/issues/13), watch and reload.**
ADR-0006 constrained #13 and #16 jointly to define reload as producing a new value.
`Load` does, and `LoadOver` is the spelling for a reload that deliberately carries a previous value forward.
Whether a watcher hands a caller a channel of `T` or something else is #13's.

**[#9](https://github.com/onhotpath/ferry/issues/9), errors.**
This ADR produces refusals at schema compile, aggregated and sorted, and defers every type.
Three things are reconciled with [ADR-0011](0011-the-error-model.md), which was written in parallel and which neither ADR had seen when it was drafted.

- **Yield nothing** is adopted, and refined at `LoadOver` above.
- **ferry has exactly one aggregate constructor and never calls `errors.Join`.**
  Adopted rather than negotiated: where this ADR says a compile's refusals are "aggregated and sorted", the aggregate is ferry's, so that `Elements()` can range it and the sort key applies.
  ADR-0011 found the alternative as a live defect, where `Elements` reported one element while two errors printed.
- **A compile error and a walk error never share an aggregate**, because a compile failure means no walk runs.
  That falls out of this ADR's control flow rather than needing a rule: `schemaFor` returns before `Bind` is called.

And one shape #9 inherits from the cache rather than the other way round: **a compile error is memoised by `sync.OnceValues` and replayed identically to every later caller**, so an error convention that carries per-call context in the error value would be wrong here.
ADR-0011's point that a replayed compile error is still a compile-moment error and sorts as one is exactly right, and it is the cache that makes it replayed.

**[#35](https://github.com/onhotpath/ferry/issues/35), what `ferrytest` exports.**
Nothing is added to that package, and one thing is handed to it: the equivalence-test rule above is a `ferrytest` obligation, because "a fresh destination per subtest" is a property of the suite rather than of core.

### What this ADR does not decide

- **Where a per-request plane instance is supplied**, and whether a caller-facing bind-then-load split exists: [#25](https://github.com/onhotpath/ferry/issues/25).
- **Whether the walk may run concurrently**, and what a scheduler may assume: [#20](https://github.com/onhotpath/ferry/issues/20).
- **The error types** every refusal here produces: [#9](https://github.com/onhotpath/ferry/issues/9)'s convention, applied rather than invented.
- **The watch and reload API**: [#13](https://github.com/onhotpath/ferry/issues/13).
- **What `ferrytest` exports**: [#35](https://github.com/onhotpath/ferry/issues/35).
- **Whether core ever exports a read-only schema view.**
  ADR-0001 left it open and said to reopen it only if a concrete need survives the dump-into-a-recording-sink pattern.
  Nothing here produces one: the compiled schema is internal, `Compile` returns an error rather than a description, and template generation reaches the defaults through a recording sink.
  This ADR does record where the pressure to reopen it will come from, which is parse-don't-validate asking `Compile` to return the thing it parsed.
  It stays closed.
- **Whether `Load` ever grows an `Option` that is compile-affecting and unhashable.**
  The rule above says what happens if one is proposed; it does not say that none ever will be.

## Consequences

- **`Load` returns a value, so ferry cannot be handed a destination that already holds a previous load's results without the caller writing it.**
  That is ADR-0006's erasure defect made hard to reach rather than documented, and it is the deciding argument for the generic entry point - a bigger one than `ErrNotPointer` disappearing.
  The cost is that a caller wanting the carry-over writes `LoadOver`, and that ferry ships two load verbs where xload ships one.
- **On failure `LoadOver` yields the seed and `Load` yields the zero value**, which is [ADR-0011](0011-the-error-model.md)'s "ferry yields no value" read as *no value it built*.
  ADR-0011 imposed the rule from a ticket that does not own this surface and named that as the call it was least sure of; it is adopted, and the one reading it could not have seen is the one where returning the zero value would destroy a live config ferry never touched.
  ferry therefore has one rule here rather than two, and the partial the walk builds is unreachable from the caller as a property rather than as a documented promise.
- `ErrNotPointer` stops existing and `ErrNotStruct` survives, because Go has no constraint meaning "any struct".
  A root non-struct is refused at schema compile, so the two failures land in different places under ferry than under xload.
- **The cache key has three components and #16 chose none of them.**
  ADR-0008 and ADR-0009 each added one independently, from two sessions that did not coordinate, which is the single most important fact about this decision and the reason the rule for what may join them exists at all.
- **ADR-0009's recommended 10 ns nesting is not available**, because ADR-0008's tag key must be in the inner key.
  The shape taken costs 32 ns and no allocations against a compile in the tens of microseconds, so nothing is lost except a number in an earlier ADR that should not be quoted forward.
- A compile-affecting Option must be comparable, and the static assertion turns a violation into a build error rather than the runtime panic ADR-0006 measured.
  The assertion works only because the key is a plain-map-shaped struct; the `sync.Map` the cache actually uses would not catch it.
- **The root must be a struct ferry walks**, so a root leaf, a root map, a root slice and a root array are all refused at schema compile.
  An earlier draft admitted the dynamic composites, on a justification that cited plane-to-plane transfer - which ADR-0006 had already measured as address-to-address and building no Go value, so it never calls `Load[T]`.
  A nil or empty root map writes `Null` at the empty path and the YAML sink writes `{}` with a nil error, so the permission reopened the hole the root-leaf rule closes, at the value a first run most often has.
  Refusing is additive to lift later; the draft argued the reverse on a caller that does not exist.
- **The schema and the walk cannot disagree about the static address set**, because they are one list rather than two computations of one rule.
  That closes the defect ADR-0008 found in a real ferry prototype, and it covers the static tier only - a dynamic address is minted by the walk and was never in the schema.
- The walk is written once and the residue is three named operations, which is a weaker claim than "one walk" and the true one.
  Roughly half the walk is shared and none of the shared half exists twice.
- A concurrent mode is a scheduler and never a second walk, and #20 inherits a seam plus a data race on the presence bit.
  Saying the seam exists without saying the race does would have handed #20 something that looks like a drop-in.
- **`Compile` takes neither the cache nor the freeze**, which restates ADR-0009's obligation more precisely than "first use" does: the constraint is on a resolution that is **retained**, so caching and freezing are one decision.
  An earlier draft of this ADR had `Compile` freeze, and measured only the case where the resulting `Register` failure is checked.
  With the error dropped, that reading leaves the plane holding a representation the user replaced, with no error anywhere - which is the defect ADR-0009's freeze exists to prevent, reached through the freeze.
  ADR-0009's one broken shape, a `Load` during `init`, is unchanged.
  From a test it is always safe, and that is a property of Go's initialisation order rather than of ferry's design.
- The compile is the whole cost the cache saves, and resolving the codec into the leaf is a modest separate win.
  Neither is the argument for compiling behaviour into the schema; ADR-0009's staleness result is.
- ferry inherits the unbounded-cache limitation every surveyed library documents, plus one door of its own: a per-call registry leaks for ordinary types, not only for generated ones.
- **The compile entry point is renamed from ADR-0008's `Validate[T]()` to `Compile[T]()`**, which is an amendment to an Accepted ADR and the only one this ticket makes.
  Nothing about ADR-0008's decision changes except the word, and the word matters because ADR-0001 rules validation out by architecture and a package that does that cannot export `Validate` honestly.
  Measured, its justification is also narrower than ADR-0008 wrote: for a malformed annotation it and `Load` return the same error, and it earns its place only by separating an annotation fault from a plane fault.
- **It returns an error rather than the schema it parsed, and that is ADR-0001's decision showing through rather than this one's.**
  Parse-don't-validate asks for the artefact; ADR-0001 keeps the compiled schema unexported and [#25](https://github.com/onhotpath/ferry/issues/25) owns whether a caller may hold one.
  So `Compile[T]() error` diverges from `regexp.Compile`'s shape knowingly, and if #25 ever exports a handle this is the function whose signature changes - a break ferry can afford only because ADR-0002 keeps it at v0.
- The caller-facing surface is four functions and two Options.
  Every Option that exists today is compile-affecting, which is a coincidence of the order the tickets landed in and not a design property, and the rule above is what stops it being read as one.

## Items from the xload survey

The survey is [`docs/research/generics-and-modern-go.md`](../research/generics-and-modern-go.md), section 5, against `github.com/gojekfarm/xtools` at [`a90b3aa`](https://github.com/gojekfarm/xtools/commit/a90b3aad2133248cec50f6b4d6e37b0d9e788adb).

**5.3, no schema caching, is this ADR's outright.**
xload calls `reflect.Type.Field(i)`, `Tag.Get` and `parseField` for every field on every call, runs a five-arm interface type switch per field per call, and allocates a fresh closure per nested struct per call.
Measured by the survey at 3343 ns / 2992 B / 54 allocs against 476 ns / 320 B / 5 allocs for a cached prototype.
Reproduced on ferry's own shape at 52306 ns against 5673 ns on a twelve-leaf struct, with the prototype's own caveats stated.
The survey's recommendation - `sync.Map[reflect.Type] -> *schema` with a `sync.OnceValues` guarding the walk, following v2's two-level pattern rather than v1's - is adopted in its two-level half and **overturned in its key**, on ADR-0008's and ADR-0009's measurements rather than on this ADR's.
Its own cost note anticipated exactly that: "if ferry allows a per-instance tag name, either key the cache per instance or factor config out of the schema and thread it at call time. The second is better and harder."
Neither is what happened: ADR-0008 made the tag key an Option and ADR-0009 made the registry a value, so the key is per configuration rather than per instance, and the cache is per registry.
The survey also notes that collision detection alone costs 21% of xload's runtime because it instruments every load; under a compiled schema ADR-0003's prefix-free check runs once at compile, for free.

**5.2, two walks that have already diverged, is this ADR's by the owner's comment.**
Addressed, and widened.
The constraint as given - write the walk exactly once - is necessary and not sufficient, because ferry has three duplication axes and xload has one.
Axis 1 is reproduced here and was found in a real ferry prototype by ADR-0008; axis 2 is measured rather than claimed away; axis 3 is a scheduler seam with a named hazard.
The survey's second recommendation for 5.2 - "equivalence tests use a fresh destination per subtest and are property-based" - is adopted and reproduced: a broken walk passes against a shared destination and fails against a fresh one.

**5.1, the `Loader` signature cannot express absence**, is ADR-0004's and ADR-0006's, and it surfaces here once: the presence bit the walk returns is only correct because absence is a kind, and it is the thing a concurrent scheduler has to combine.

**5.7, `reflect.DeepEqual` as a "was anything set?" probe**, is ADR-0006's.
This ADR is where its repair lives structurally: the walk returns the presence bit per subtree and materialises `*T` from it, in the one walk rather than in two.

**5.14** was enumerated rather than assumed, all four items.
Two of the four are this ticket's by name.

- *Two ways to set the loader.*
  **This ticket's, and it is an entry-point defect.**
  xload makes some loaders directly usable as options and others not, so there are two ways to supply one thing and nothing says which.
  Avoided here by construction on the source, which is a positional argument of an interface type and never an Option.
  It is **not** avoided by construction on the load verb: `Load` and `LoadOver` are two exported functions.
  They are one function and its zero-valued sugar, nothing is expressible through one and not the other, and that is the same answer ADR-0009 gave for its default registry.
  The live risk is a third: a caller-held `Schema[T]` with its own `Load` method would be a genuine second way, and it is named as [#25](https://github.com/onhotpath/ferry/issues/25)'s to weigh rather than pre-empted here.
- *The `CanAddr` loop that can only run once.*
  **This ticket's, and it is a defect in the reflection walk.**
  `for fVal.CanAddr() { fVal = fVal.Addr() }` appears three times in xload, is written as a loop, and can only ever execute once because `Addr()` returns a non-addressable pointer.
  Not carried over, and the reason is structural rather than careful coding: the walk owns addressability in exactly two places and neither is a loop.
  Load always decodes into a fresh addressable destination, because `Load` returns a value and the walk starts at `reflect.ValueOf(&out).Elem()`.
  A map value is the only unaddressable position, and the walk creates a fresh element, walks into it, and puts it back - written once, so neither direction has to know it.
  ADR-0007 already replaced the instinct behind the loop with a stated rule about receivers, and this ADR is where the walk stops having the question.
- *The non-deterministic select on a cancelled context.*
  Concurrency, and [#20](https://github.com/onhotpath/ferry/issues/20)'s.
  This ADR neither fixes nor worsens it, and adds one thing #20 needs: the walk checks `ctx.Err()` at every node entry, which is a placement and not a policy, and #20 owns whether it is per leaf, per subtree or not at all.
- *Value receivers on `Error()` where pointers are returned.*
  Deferred to [#9](https://github.com/onhotpath/ferry/issues/9)'s convention, as ADR-0003 through ADR-0009 all did.

**5.4, first error only**, and **5.5, nondeterministic error output**, are [#9](https://github.com/onhotpath/ferry/issues/9)'s.
This ADR applies rather than decides: schema-compile refusals are collected, joined and sorted, measured at one distinct error string over 300 compiles of a type with four faults.

The remaining items are unaffected by this ADR.
