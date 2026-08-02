# 4. The source and sink contract

Status: Proposed
Date: 2026-08-02
Ticket: [#5](https://github.com/onhotpath/ferry/issues/5)

## Context

ferry's whole design rests on two interfaces nobody has written down: the one a plane implements to be read, and the one it implements to be written.
[ADR-0001](0001-what-ferry-supports.md) settled what ferry supports, [ADR-0002](0002-core-and-sub-modules.md) settled that no plane ships in core, and [ADR-0003](0003-how-a-leaf-addresses-a-plane.md) settled what an address is.
None of them decided the signature the address appears in.

The inherited answer is xload's `Loader.Load(ctx context.Context, key string) (string, error)` ([loader.go:9-11](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/loader.go#L9-L11)).
Three of its four elements are already ruled out by decisions taken: the key is a `string` where ADR-0003 fixed a structured `Path`, the value is a `string` where the project charter states a preference for typed values, and there is no Dump counterpart at all.
What survives is the shape - one call, one key, one answer - and ADR-0003 rules that out too, because a driver's injectivity obligation cannot be discharged one key at a time.

ADR-0001's consequences state the constraint that governs this ADR more than any other:

> Core stays small, and ferry ships fewer batteries than viper or koanf.
> Adoption therefore depends on drivers being cheap to write.

So every interface here has to earn its place against that, and the ADR records what each one costs a driver author rather than only what it buys ferry.

This ADR is written from a throwaway prototype on branch `proto/5-source-sink`, which never merges.
Every number below is from that prototype unless it cites the survey.
It contains four drivers - environment variables, HTTP query parameters, a Consul-shaped remote KV, and YAML over real files - written as real self-contained drivers rather than as sketches, because several of these questions are only answerable by writing the driver and counting.

**Four of the sixteen probes are audits of answers the prototype had already reached, and all four changed the result.**
That is recorded here because the shape of each mistake is more instructive than its fix, and because three of the four came from a reviewer asking what an interface was for rather than from the code failing.

## Decision

### The contract

```go
type Source interface {
    Bind(addrs *AddressSet) (OpenFunc, error)
}

type Sink interface {
    Bind(addrs *AddressSet) (OpenWriterFunc, error)
}

type OpenFunc       func(ctx context.Context) (Reader, error)
type OpenWriterFunc func(ctx context.Context) (Writer, error)

type Reader interface {
    Get(ctx context.Context, addr Path) (Value, error)
}

type Writer interface {
    Set(ctx context.Context, addr Path, v Value) error
}
```

Plus three optional interfaces, discovered by assertion and never required:

```go
type Releaser   interface{ Close() error }                        // this is io.Closer
type Committer  interface{ Commit(ctx context.Context) error }    // sinks that stage
type Enumerator interface {                                       // planes that can list
    Children(ctx context.Context, prefix Path) ([]Path, error)
}
```

**Four required interfaces, one method each, in both directions.**
Everything beyond that is opt-in.

### `Bind` is a separate phase because the two halves have different lifetimes

This is the one seam that survived every round of simplification, because it is the only thing carrying ADR-0003's before-any-I/O rule.

Three pieces of state, three lifetimes:

| | holds | changes when |
| --- | --- | --- |
| `Source` | driver config: path, separator, prefix, client | never, you constructed it |
| `OpenFunc` | the precomputed key table | the schema or the driver config changes |
| `Reader` | the plane's contents | every load |

ADR-0003 requires the plane keys to be precomputed rather than derived per lookup, and states that as a requirement of the design rather than an optimisation.
Reproduced on the whole path rather than on the lookup, for a six-address load through the query-parameter driver:

| | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| bind once, open per load | 158 | 16 | 1 |
| bind and open on every load | 2743 | 1744 | 60 |
| flat string keys, no address model at all | 78 | 0 | 0 |

**The first answer the prototype reached was that core memoises the table, and it is wrong two separate ways.**

Memoising on `(address set, driver name)` silently hands one driver another driver's keys.
Measured: `EnvSource{Sep: "__"}` and `EnvSource{Sep: "_"}` share a cache entry, and the second one loads `METRIC__HTTP__PORT`.
The separator is the one thing ADR-0003 made a driver option, so this is the headline case rather than an exotic misuse.

The obvious repair, letting the driver supply a comparable identity that captures everything its key function reads, is unsound.
Measured: using a driver value as a map key panics with `hash of unhashable type main.EnvSource`, because the driver holds a func field, as any driver taking a dialer, a hook or a clock will.
A contract whose correctness depends on a driver author supplying the right identity is a prose rule with a runtime panic behind it, which is the shape ADR-0001 rules out.

**Splitting the phase removes the cache, and with it both defects.**
Nothing is memoised, so nothing can be memoised wrongly.

`Bind` takes no `context.Context`, and that is load-bearing rather than tidy.
It is how the type says no I/O happens here, and it makes the rule assertable instead of merely stated.
Measured: `Bind` against a missing file returns a nil error and `Open` then fails.
The conformance suite can therefore contain the case "Bind must succeed against an unreachable plane", which is not writable at all if the address set and the I/O arrive in one call.
That is ADR-0002 route (b) applied to this ADR's own central rule.

Two more things the split buys, both measured, both cited by ADR-0001:

- **Reload.** Watch is Milestoned. One bind, three opens, three different values read. Merged, every reload recomputes the keys and re-runs the injectivity check over a set that has not changed.
- **Plane-to-plane transfer**, which is Enabled. Both planes bind before either is touched, so a transfer whose destination cannot name two of its addresses is refused after zero backend calls rather than after reading the whole source.

### `OpenFunc` is a function because nothing ever asks it a second question

An interface earns its place when a caller asks more than one thing of it, or type-asserts it for optional behaviour.

`Reader` is asserted, for `Enumerator` and `Releaser`, so it stays an interface.
`OpenFunc` is called once per load, never asserted, and never asked anything else.
That is a function.

It is *named* rather than inline for three reasons: the phase contract needs a doc site, the driver's signature then reads as the phase, and a named func type can carry methods later without a breaking change.
The precedent is `context.CancelFunc`, a named func type returned by a constructor, closing over state, documented with prose about when to call it.
Go's convention for such a type is the `-Func` suffix even when it is not an interface adapter - `CancelFunc`, `CancelCauseFunc`, `WalkFunc` - which is why it is `OpenFunc` and not `Open`.

**Two concrete names rather than one generic `OpenFunc[T]`**, which was considered and measured.
The generic form compiles, works in a driver's return position, and still infers through a combinator.
It saves exactly one exported name and costs every driver signature, forever:

```go
Bind(a *ferry.AddressSet) (ferry.OpenFunc, error)                  // concrete
Bind(a *ferry.AddressSet) (ferry.OpenFunc[ferry.Reader], error)    // generic
```

Against ADR-0001's stated constraint that adoption depends on drivers being cheap to write, that is the wrong trade: it is symmetry in ferry's source paid for in every driver's source.
A third variant, a generic `Binder[T]` with `type Source = Binder[Reader]`, is rejected on a measured diagnostic: generic type aliases are resolved away in compiler errors, so a driver that gets it wrong is told it does not implement `Binder[Reader]`, and the name the author actually wrote never appears.
Generics are still the right tool for ferry's own internal lifecycle plumbing, which is written once over a type parameter and is not driver-facing.

### Absence is a kind of the value, not a second return value

xload's signature cannot express absence (5.1), and its own `cached` provider invented `Get(key string) (*string, error)` to work around exactly that.
The consequence propagates: `required` is `val == "" && meta.required`, `setVal` silently no-ops on empty, and a decoder is never handed the empty string.

5.1 weighed three replacements and recommended comma-ok.
There is a fourth it did not weigh, and it is the one this ADR takes: **absence is `Value.Kind() == Absent`, and `Get` keeps the ordinary Go `(T, error)`.**

All four express the three states, so correctness does not separate them, and on the miss path neither does cost - 10.5, 10.9 and 11.2 ns at zero allocations for comma-ok, pointer and kinded alike, against 195 ns and three allocations for a sentinel error, which is disqualifying on its own because absence is the common case in a config load.

What separates them is the mistake each one allows:

| shape | the mistake it allows |
| --- | --- |
| `(Value, bool, error)` | `v, _, err := r.Get(...)` compiles, produces no vet diagnostic, and turns absent into a zero value. Go's blank identifier makes discarding a second channel a one-character edit. |
| `(*Value, error)` | a missing nil check panics inside ferry, on a value third-party driver code produced. |
| `(Value, error)` + sentinel | a driver returning a bare `errors.New("not found")` is indistinguishable from a real failure, and nothing checks that it wrapped correctly. |
| `(Value, error)`, absence as a kind | there is no second channel to discard. |

Two properties make the fourth better than merely tidier.

**The accessors already refuse it.**
`Absent.AsString()` and `Absent.AsInt()` both return a wrong-kind error, because absent is not string and not number.
No new discipline is required of ferry's own leaf setters.

**It survives being stored**, which is the part that matters beyond this ADR.
`Absent` is kind zero, so a `map[Path]Value` lookup miss *is* absence, and a recording sink needs no parallel presence map.
ADR-0001 milestoned plane inspection on the grounds that a loaded struct erases absence.
This is the boundary type not erasing it, which is the mechanism that milestone commits to rather than the feature.

**What this ADR does not decide** is what an absent address means to a Go field - whether it takes a default, whether `null` and absent are the same thing to a `*string`, and whether `FOO=` satisfies `required`.
That is [#8](https://github.com/onhotpath/ferry/issues/8)'s.
This ADR fixes only that the contract can tell them apart, which 5.1 says xload's cannot.

### Values cross typed, and the justification is still Dump

Section 4 of the survey reaches a decisive result: Load survives a string boundary because the destination struct field type drives parsing, and Dump cannot, because the sink must choose a representation and a string gives it nothing to choose from.

That was measured on a flat key space with composite values, and ADR-0003 changed the address model underneath it, so it is re-measured here rather than inherited.
Five values dumped through the YAML sink and loaded back through the YAML source: **typed returns 5 of 5 addresses exactly.**
Stringified loses `null` to `""`, `true` to `"true"`, `8080` to `"8080"` and `3.5` to `"3.5"`, permanently.

The asymmetry survives the address change.
What changed is the size of the loss rather than its existence: composites no longer flatten into one value, so the damage is confined to the scalar leaf.

The premise check in section 4 stands, and this ADR states it out loud rather than designing around the minority case silently.
A typed boundary buys YAML and TOML something real, JSON something partial, and Consul, environment variables and query parameters nothing at all - and two of the four first-party drivers are in the last group.
It is taken anyway, because the direction it pays off in is the direction ferry exists to add.

### The value is a kind and a source text, and nothing else

```go
type Value struct {
    kind VKind
    text string
}
```

with kinds `Absent`, `Null`, `Bool`, `Number`, `String`, `Bytes`.
Constructors per kind, accessors returning `(T, error)`.

**Scalars carry source text, never a machine number.**
Every lossless design the survey examined converged there, including `encoding/json/v2` in 2026, whose answer to precision is still to quote it as a string.
A native numeric leaf recreates `structpb`'s documented `float64` defect.

**Accessors return errors and never panic.**
`cty`, `protoreflect` and `slog` all panic on kind mismatch and all document it as intentional, because their callers type-check first.
ferry's callers are third-party driver authors.

**Bytes live in the `text` field**, because a Go string is an immutable byte sequence and nothing requires it to be UTF-8.
That removes the `any` field the survey's sketch had, and buys three things at once: `Value` is 24 bytes, it has no boxing allocation, and **it is comparable**.
`slog.Value` gives comparability up with `_ [0]func()` and `protoreflect.Value` with `pragma.DoNotCompare`, both in exchange for unsafe packing that section 4 already established ferry does not need.
Comparability is what lets the round-trip harness and the conformance suite assert with `==`, and what makes `map[Path]Value` a usable recording sink.

**Quoting survives**, which is the one thing section 4 identifies that a string boundary genuinely destroys: `port: 8080` arrives as `Number("8080")` and `port: "8080"` as `String("8080")`, and both round-trip back to their own spelling.

#### There is no group arm, and this reverses a survey recommendation

Section 4 says a group arm is "required, not optional", because xload's flattening is where the YAML list is lost (5.8, reproduced).
That was correct for a flat key space and is obsoleted by ADR-0003, which landed after it.

Under a structured address a composite gets one address per element, so `servers: [a, b]` is read at `/servers#0` and `/servers#1` and nothing ever asks the plane for the value *at* `/servers`.
Measured: the indexed form carries every element exactly, and 5.8's list-becomes-empty-string is not merely avoided but unrepresentable.
The remaining case, a flat plane holding a whole list in one value as `TAGS=a,b,c`, arrives as a scalar `String("a,b,c")`, and splitting it is a codec's job on ferry's side of the boundary where the target Go type is known.

So every address in a compiled schema is a leaf, and a group arm would be an arm no address can be at.

**One case this forecloses, stated because it is the strongest objection and it survives.**
Mapping a structured subtree onto a single Go field opaquely - a YAML `servers:` block into a `json.RawMessage` - is not expressible, because the driver would have to re-serialise the subtree without knowing the target wants bytes, and it refuses instead.
The neighbouring case is unaffected: a Consul key or an env var *holding* an encoded blob is already a scalar and arrives as `Bytes`, which is section 4f's actual motivating example.
So what is lost is opaque capture from a plane that has structure, and it is left to [#7](https://github.com/onhotpath/ferry/issues/7) and [#12](https://github.com/onhotpath/ferry/issues/12), because the mechanism it needs is a codec claiming a subtree rather than a kind in the value model.

#### There is no escape arm either, and that is the weakest call in this ADR

Section 4's argument for one is strong and is stated here rather than dismissed: `slog.Value`'s `KindAny` is why its kind set never had to grow, `attribute.Value` shipped closed and its enum has already grown twice leaving a deprecated alias behind, and adding a kind after v1 is a breaking change.

It is not taken, for two reasons specific to ferry.
ADR-0001 closed core's type set and made extension explicit and proof-carrying, so "a type ferry cannot name" is already a compile-time error rather than a value that must be representable at runtime.
And an escape arm holding a driver-native value is uninterpretable by any other driver, which breaks plane-to-plane transfer.

The mitigation is ADR-0002's: ferry is at v0, and v0 is the only place semver allows taking this back.

#### `jsontext.Token` is mirrored, not adopted

The ticket asks for this priced three ways, and the attractive facts are real: it is a raw-text-plus-kind union with `(T, error)` accessors, it imports no `reflect`, and it is GA.
Verified on `go1.27rc2`, three facts decide it, and the first two would decide it even if ADR-0002 did not exist.

**It is a stream token, not a value.**
`jsontext.BeginArray` and `EndArray` are `Token`s whose `Kind()` is `[` and `]`.
A ferry `Value` must never be able to hold "the start of something".

**Holding one past the next read panics.**
Measured: a `Token` read from a `jsontext.Decoder` and kept without `Clone` panics with `invalid jsontext.Token; it has been voided by a subsequent json.Decoder call`.
It does not degrade, it panics.
ferry hands values to third-party driver code and stores them in maps, and section 4 already ruled that accessors must not panic, so a value model with a use-after-read panic fails ferry's own stated rule.

**It has no bytes kind**, because JSON has no binary scalar, and a Consul or Registry plane has nothing else to hand over.

Only then does ADR-0002 apply, and it applies decisively: core imports only unconditionally-available stdlib, and `jsontext` vanishes under `GOEXPERIMENT=nojsonv2`.
Adopting it into core would be an amendment to ADR-0002 argued explicitly.
There is nothing to argue, because mirroring its shape costs nothing and the three facts above say mirroring is what ferry wants anyway.
Measured: `jsontext.Token` is 32 bytes, ferry's `Value` is 24 and comparable.

### Source and Sink are separate, and a read-only plane refuses in two places

They stay separate, and the deciding case is one ADR-0002 already settled: environment variables have no honest Dump.

Under one combined interface, the env and query drivers would each have to declare a `Dump` they cannot honour.
ADR-0002 refused to put half a driver in core for precisely this reason, and the same argument applies outside core.
With two interfaces the refusal is free: the env driver does not implement `Sink`, so dumping to it is a compile error at the call site rather than a runtime error, prose, or a returned `ErrUnsupported` nobody reads.

**A plane that is writable in principle but not right now refuses inside `OpenWriterFunc`**, with an error wrapping `ErrReadOnly`.
Measured, both landing there: a KV with no write ACL, and a YAML sink targeting a `0555` directory.

Not at `Bind`, because writability is a fact about the plane and not about the schema, and `Bind` does no I/O so it cannot know.
Not at the first `Set`, because Dump is the direction that runs a walk over the user's struct: failing at open costs nothing, and failing at the first `Set` has already half-written the plane.

**The cost, stated plainly**: a driver serving both directions ships two types, because one type cannot have two `Bind` methods.
`yaml.Source{Path: p}` and `yaml.Sink{Path: p}` means naming the plane twice for a round trip.
That is the price of the compile-time refusal above, and it is judged worth paying.

### Release and commit are two different things, and both are optional

`Writer` has one required method, `Set`.
The end of a dump is expressed by two optional interfaces, because the two concerns it contains do not co-occur:

| sink | release | commit |
| --- | --- | --- |
| yaml file, staging | yes | yes |
| kv, transactional | no | yes |
| kv, write-per-Set | no | no |
| recorder in `ferrytest` | no | no |
| http PUT per key | no | no |
| lazy kv source, read side | yes | n/a |

Two sinks want one and not the other, and the read side wants release with no commit at all.

**The protocol is the whole design: `Commit` runs only when the walk succeeded, `Close` always runs.**
Closed-without-Commit is the abort signal, so no driver is ever told that it failed.
That is `sql.Tx`'s shape and `bufio.Writer`'s, and it is why neither method takes a cause.
Measured on the staging YAML sink through both paths: on success the plane is replaced and no temp file remains; on a walk failure the plane is byte-identical and no temp file remains.
The sink tracks it with one bool.

Two consequences of making them optional rather than required.

**A required `Close` would be `return nil` boilerplate in four of six sinks, and that is not merely noise.**
It is indistinguishable in the source from a driver that should have rolled back and did not, and nothing in the type system tells the two apart.

**The inverse risk is covered by a test that ships regardless.**
A sink that needs `Commit` and omits it writes nothing at all, silently, which is exactly what ADR-0001 rules out.
Measured: it fails the first case in the driver-fidelity suite ADR-0001 already obliges - dump a value, load it back, compare.
That is ADR-0002 route (b) doing the job it was admitted to core for.

**`Releaser` is `io.Closer`**, verified: `var _ Releaser = (*os.File)(nil)` compiles.
It is not a name ferry invents, a driver wrapping a file or a connection satisfies it for free, and `Close() error` takes no context because cleanup that can be cancelled is how the temp file leaks.
`Commit` takes one because it is the actual I/O.

### There is no batch interface, because there is nothing left for one to do

The ticket proposed the survey's option 3: a per-key interface required, a batch form as an optional interface upgrade, memoisation always on.
None of the three survives.

**Batch versus lazy is a branch inside one driver.**
`Bind` already handed over the address set, so `OpenFunc` can fetch everything in one round trip or fetch nothing at all, and ferry never needs to know which.
Measured on a three-address schema against the Consul-shaped driver: lazy makes 3 backend calls, batch makes 1, and the difference is one boolean in the driver.
An optional `Snapshotter` interface would be ferry defining, versioning and conformance-testing a second contract to express a choice the driver can already make.

**In-load memoisation has nothing left to deduplicate.**
5.13's first reproduction was that two struct fields sharing one key produce two backend calls.
Under ADR-0003 that schema does not compile: the address set is prefix-free and a path is a prefix of itself.
Half of 5.13 was fixed by the address decision before this ticket opened.

### The address set handed to `Bind` is the static set, and core hands back a key function

ADR-0003 is explicit that not every address comes from the type: a map key's address and a slice element's index come from the value, so both collision rules run in two tiers, the second "as each is minted, before the write it belongs to".
Every probe in the prototype up to that point used a schema containing no map and no slice, which hid the consequence.

Measured with a static set of `{/name}`, dumping a `map[string]string` field to the KV driver: `Set(/labels/env)` returned `kv: address not in the opened set: /labels/env`.
A precomputed table is a closed set, so a legitimate map key was refused as though it were a driver error.

The fix changes no interface.
What changes is what core's helper returns:

> Core hands a driver a **key function with the static set already computed and checked**, not a `map[Path]string`.

A map invites a driver to treat a miss as an error, which is exactly what happened.
A key function serves the static tier from the precomputed table and mints a dynamic address on demand, running ADR-0003's legality and injectivity checks against everything already issued.
Measured: `/limits/http.port` minted against an already-issued `/limits/http_port` is refused, naming both addresses, before the write it belongs to.

The two tiers are two fields rather than one map, and that is what keeps the static tier at the cost ADR-0003 priced it at.
The static table is written once and never again, so reading it takes no lock:

| | ns/op | allocs/op |
| --- | --- | --- |
| static hit | 8.8 | 0 |
| bare map lookup, no address model | 7.3 | 0 |
| dynamic hit, after minting | 27.1 | 0 |
| single mutex over both tiers, static hit | 20.0 | 0 |

Concurrency is [#20](https://github.com/onhotpath/ferry/issues/20)'s and this ADR decides none of it.
It records only that the two-tier split is what makes a lock-free static path available, so #20 inherits a choice rather than a constraint.

### Sources compose, and composition needs no core surface

5.12: `SerialLoader` is last-non-empty-wins, queries every loader for every key with no short-circuit, and because empty means absent a later higher-priority source can never override a value back to empty.

A composite is a `Source` whose `Bind` binds its children and whose `Get` returns the first present value.
Measured on a six-leaf schema over three backends: 18 backend calls for `SerialLoader`, 3 for first-present-wins over batch-fetching children.

**First-match-wins is correct only because absence is observable**, which is 5.12's actual cause rather than its symptom.

The contract makes a family of these cheap, and this is where the surface that is *not* an interface does its work.
Each is itself a `Source` or `Sink`, so they nest, and each is between four and twelve lines:

- `FirstOf` - precedence, binding every child before any child does I/O
- `Static` - a source of constants, which is both the defaults layer and the memory plane
- `Under` - ADR-0003's "a prefix prepends a segment"
- `Snapshot` - forces a lazy source to be read once per load, which is xload's entire `cached` provider sub-module without the stale-read TTL
- `Recorder` - a `Sink` that captures what it is given, which is ADR-0001's schema-extraction pattern

None is a new contract for a driver to implement, and this ADR ships none of them.
It records that the contract admits them, and that this is where complexity should go when it appears.
Whether any lands in core is a later question governed by ADR-0001's bucket rule, and a precedence *convention* - flags beating environment beating file - stays ruled out by remit.

### Enumeration is optional, and the asymmetry is smaller than ADR-0003 feared

ADR-0003 asked this ticket to decide whether sources enumerate, and said that if the answer is no, then map-keyed and indexed addresses are Dump-only and ferry's two directions cover different address sets.

**The answer is neither yes nor no.**
`Enumerator` is an optional interface a `Reader` may implement.

It cannot be required, because it would exclude a plane class ferry explicitly wants: a Vault kv-v2 `LIST` is a separate ACL capability and a token with read and no list is ordinary, and some secret brokers answer only what you name, by design.
It cannot be omitted, because three of the four first-party planes enumerate trivially, so omitting it would make ferry unable to load a map from any plane, to no benefit.

So the two directions cover:

- **Dump**: every address, always. The value is in hand, so map keys and sequence lengths are known.
- **Load**: static addresses always; dynamic addresses from any source implementing `Enumerator`.

That is a real asymmetry and it belongs here rather than being discovered later, as ADR-0003 asked.
What it is **not** is "map-keyed addresses are Dump-only".
They are loadable from most planes, and unloadable only from the ones that genuinely cannot list.

The enumerator returns addresses rather than names, deliberately: measured, `/limits` yields Name segments and `/tags` yields Index segments, so the plane answers which composite it is rather than the caller guessing from base-10 text - the limitation ADR-0003 quotes `jsontext.Pointer`'s own godoc admitting.

Loading a map-typed field from a non-enumerating source is an error naming the field and the source, never a silently empty map.
An empty map is the most plausible-looking wrong answer available, and ADR-0001 rules out ignoring anything silently.

Whether any supported Go type produces these addresses at all is [#7](https://github.com/onhotpath/ferry/issues/7)'s; whether an absent map is empty or defaulted is [#8](https://github.com/onhotpath/ferry/issues/8)'s.

### What a driver costs, against the bar ADR-0001 named

ADR-0001's consequences name the bar: koanf gets twenty providers off a two-method interface at 31 to 246 lines each, median around 120.
The four drivers, rewritten self-contained against this contract, counting non-blank non-comment lines:

| driver | directions | optional interfaces | lines |
| --- | --- | --- | --- |
| query parameters | source | none | 45 |
| environment variables | source | `Enumerator` | 77 |
| KV, Consul-shaped | source and sink | `Enumerator`, `Committer` | 107 |
| YAML, real files | source and sink | `Enumerator`, `Committer`, `Releaser` | 194 |

Inside koanf's band, and the only one above its median implements both directions and three optional interfaces, which koanf has no equivalent of.

**No driver implements all three optional interfaces, and only the staging YAML writer needs two.**
That is the measured case for making them optional rather than part of `Reader` and `Writer`.

One asymmetry is worth stating because it surprises: **a tree plane pays nothing for the address set.**
The YAML driver never builds a plane key, it walks the segments, so it has no injectivity obligation and makes no key-table call at all.
ADR-0003's driver-side rule binds flattening drivers only.

### The first-party driver list

ADR-0002 deferred this here, because its admission rule is that a first-party driver ships only to exercise an axis of the driver contract that no existing first-party driver exercises, and the axes are a property of these signatures.

Each row is one such axis: something the contract asks of a driver that has to be exercised by real code somewhere, or the conformance suite is testing it against nothing.
**`yes` means the driver exercises that axis and blank means it does not**, so a driver earns its place by owning a row no other driver has.
The rows are read off the contract above, and the three `Committer`, `Releaser` and `Enumerator` rows are the measured output of the prototype's four drivers rather than a plan for them.

| axis | env | query | kv | yaml | memory plane |
| --- | --- | --- | --- | --- | --- |
| produces a plane key, so carries the injectivity obligation | yes | yes | yes | | |
| walks segments as a tree instead | | | | yes | |
| has a serialization format | | | | yes | |
| carries plane-side type information | | | | yes | |
| opaque bytes only | | | yes | | |
| real I/O, cancellation, partial failure | | | yes | yes | |
| batch versus lazy inside one open | | | yes | | |
| `Committer` without `Releaser` | | | yes | | |
| `Committer` and `Releaser` together | | | | yes | |
| structurally has no Dump | yes | | | | |
| per-request, hot path | | yes | | | |
| `Enumerator` | yes | | yes | yes | |

One row is easy to misread and is worth spelling out.
**"Structurally has no Dump" is env's alone, and it is not the same as "ships no sink".**
Query params ship no sink here either, but they could have one: a query string is a perfectly good dump target, and percent-encoding is a format the driver may own because ADR-0002's bar on formats constrains core and not a driver outside it.
Env cannot, for the reason ADR-0002 gave: setting the process environment is process-global mutation nobody wants, and the thing people do want is a `.env` file, which is a format.
So env is the only plane where the absence of a `Sink` implementation is a property of the plane rather than a decision about scope, which is exactly what makes it the case that keeps `Source` and `Sink` honestly separate.

**The list is `yaml`, `kv` and `env`, with `query` named as a candidate rather than a commitment.**

- **yaml** reaches four axes nothing else does, and is the only driver needing both lifecycle interfaces. It is what keeps the conformance suite honest.
- **kv** is the only real I/O, the only opaque-bytes plane, the only batch-versus-lazy choice, the only dynamically read-only sink, and the only `Committer` with nothing to release. Consul-shaped rather than Consul, so the exact backend stays open.
- **env** is the flat key function with a transform, and the only plane that structurally has no Dump, which is what keeps `Source` and `Sink` honestly separate.
- **query** is the only per-request axis and the only driver implementing no optional interface at all, which is a weak claim on two counts: its key function is a flat join like env's, and it is also the driver this contract serves least well. See below.

The memory plane's column is empty on every axis, which is ADR-0002's own point restated as a measurement.

### What this ADR does not decide

- Whether an absent address takes a default, whether `null` and absent mean the same thing to a Go field, and whether present-and-empty satisfies `required`: [#8](https://github.com/onhotpath/ferry/issues/8).
- Which Go types map onto which kinds, and whether any type produces an Index segment: [#7](https://github.com/onhotpath/ferry/issues/7).
- How a `Value` becomes a Go value and back, and the precedence of the chain that does it: [#12](https://github.com/onhotpath/ferry/issues/12).
- The error types every refusal here produces: [#9](https://github.com/onhotpath/ferry/issues/9)'s convention, applied rather than invented.
  This ADR uses `errors.Join` and sorted reports to satisfy ADR-0001's determinism invariant, and defers the types.
- Whether `Get` and `Set` may be called concurrently, and what a driver may assume: [#20](https://github.com/onhotpath/ferry/issues/20).
- Which combinators, if any, ship in core: ADR-0001's bucket rule, when one is proposed.
- The exported verb names, which ADR-0001 left open.
  `Bind`, `Get`, `Set`, `Commit` and `Close` are driver-facing and decided here; `Load` and `Dump` remain the working assumption for the caller-facing ones.

### A decision with no ticket, surfaced rather than taken

The caller-facing lifecycle has no owner on the map, and this ADR creates the need for one.

`OpenFunc` is reusable and the `Source` holds the plane, so for every plane whose handle is long-lived - a file path, a KV client - the pattern is unremarkable: bind once, open many.
For a **per-request plane it does not work**, because a query-parameter source is constructed per request, so `Bind` is per request too, and the measurement above prices that at 2743 ns and 60 allocations instead of 158 ns and 1.

The plane instance has to be suppliable at load time rather than at bind time, and none of the three available answers is obviously right: put it in the `context.Context`, add a caller-facing `LoadFrom` taking a `Reader` the caller already holds, or make the plane a parameter of `OpenFunc` and change what `Source` means.

This is not decided here, because deciding it inline would settle the caller-facing API as a side effect of a driver-facing ADR.
**Proposed as a new ticket on [#1](https://github.com/onhotpath/ferry/issues/1): "How a caller holds a binding, and where a per-request plane is supplied."**
It blocks nothing in this ADR; the contract above is the same either way.

## Consequences

- A read-only driver implements two methods, which is koanf's bar exactly, and koanf has no sink at all.
  A driver serving both directions implements four across two types.
  Everything beyond that is opt-in, and the measured driver cost stays inside koanf's band.
- Core owns a key-function helper that runs ADR-0003's legality and injectivity checks.
  That is ADR-0002 route (b), and it means a driver that hand-rolls its own key table silently opts out of the check.
  The conformance suite has to test for that, not just for the checks themselves.
- Three optional interfaces mean three prose rules the compiler cannot enforce: implement `Committer` if your writes are not durable until the end, implement `Releaser` if you hold a resource, implement `Enumerator` if your plane can list.
  Each failure mode is caught by a conformance case that has to exist anyway, and that is the entire argument for the trade.
  It is also the argument this ADR is least able to make in advance, because it depends on the suite being written well.
- A driver serving both directions ships two types.
  That is the visible cost of the compile-time read-only refusal.
- The value model is closed at six kinds with no escape arm, so adding a kind is a breaking change after v1.
  This is the one place this ADR knowingly departs from a survey recommendation on ferry-specific grounds, and v0 is the whole mitigation.
- `Value` is comparable, which the round-trip harness and the conformance suite can both rely on, and which no comparable library's value type offers.
  It is also a constraint: it forecloses ever adding a slice or map field to `Value`.
- Two of the four first-party planes gain nothing from the typed boundary.
  The cost is paid on every plane and collected on some, and the ADR says so rather than selling the feature evenly.
- ferry's two directions cover different address sets, and the difference is visible to users as "this source cannot load a map".
  It is smaller than ADR-0003 anticipated, and it is now a documented property of a driver rather than a surprise.
- Composition, defaults, prefixing, snapshotting and recording are all expressible as combinators over the same two interfaces, and none of them ships.
  Someone will ask for them in core, and the answer has to be ADR-0001's bucket rule rather than a technical one.
- The caller-facing lifecycle is now load-bearing and unowned.
  Until the proposed ticket lands, the per-request use case xload was pitched at has no worked answer, and the `query` driver's place on the first-party list is weaker for it.

## Items from the xload survey

The survey is [`docs/research/generics-and-modern-go.md`](../research/generics-and-modern-go.md), section 5, against `github.com/gojekfarm/xtools` at [`a90b3aa`](https://github.com/gojekfarm/xtools/commit/a90b3aad2133248cec50f6b4d6e37b0d9e788adb).

**5.1, the `Loader` signature cannot express absence.**
Addressed, and this ADR owns the fix ADR-0001 deferred here.
Absence is a kind of the value rather than a second return value, so a driver cannot express it wrongly and a caller cannot discard it.
Reproduced first: through xload's shape, `EMPTY` and `MISSING` are one observation; through this contract they are two, plus `SET`, at 11.2 ns and zero allocations.
The survey's own recommendation was comma-ok; this ADR takes a fourth option it did not weigh, and says so.

**5.12, `SerialLoader` precedence is unexpressible.**
Addressed.
First-present-wins is expressible and cheap, measured at 3 backend calls against 18, and correct only because 5.1 was fixed first.
The convention that would sit on top of it stays ruled out by ADR-0001's remit.

**5.13, the per-key pull model amplifies backend round trips.**
Addressed, and half of it was already dead.
The duplicate-key half is a schema that does not compile under ADR-0003's prefix-free rule.
The remaining half collapses from 3 calls to 1 on a three-address schema with no interface beyond the two the contract already has, because the driver chooses.
The survey's recommended shape, required per-key plus an optional batch upgrade, is rejected with the measurement that makes the upgrade redundant.

**5.14** was enumerated rather than assumed, all four items.

- *Two ways to set the loader.*
  Bears on this ADR directly, and is avoided by construction: there is one way to supply a source, which is to pass something implementing `Source`, and no driver type doubles as an option.
- *The `CanAddr` loop that can only run once.*
  A defect in the reflection walk, not at this boundary.
- *The non-deterministic select on a cancelled context.*
  Bears on this ADR in that `OpenFunc`, `Get`, `Set` and `Commit` all take a `context.Context` and a driver may return `ctx.Err()`.
  What ferry does when a cancellation and a driver error race is [#20](https://github.com/onhotpath/ferry/issues/20)'s, and this ADR neither fixes nor worsens it.
  `Close` takes no context deliberately: it is cleanup, and cleanup that can be cancelled is how the temp file leaks.
- *Value receivers on `Error()` where pointers are returned.*
  Bears on `ErrReadOnly` and on the refusals core produces.
  Deferred to [#9](https://github.com/onhotpath/ferry/issues/9)'s convention rather than pre-empted here, as ADR-0003 did.

**5.8, type information destroyed at the boundary**, is ADR-0001's, and this ADR is where the fix becomes structural: a YAML sequence is read per element and a YAML scalar arrives with its tag, so there is no point at which `cast.ToString` could be called.

**5.11, the YAML provider silently discards parse errors**, is ADR-0001's.
This ADR gives it a place to land: a malformed plane fails inside `OpenFunc` with a non-nil error, checked in the prototype's conformance run.

The remaining items are unaffected by this ADR.
