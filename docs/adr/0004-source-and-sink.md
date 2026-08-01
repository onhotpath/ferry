# 4. The source and sink contract

Status: Proposed
Date: 2026-08-02
Ticket: [#5](https://github.com/onhotpath/ferry/issues/5)

## Context

ferry's whole design rests on two interfaces nobody has written down: the one a plane implements to be read, and the one it implements to be written.
[ADR-0001](0001-what-ferry-supports.md) settled what ferry supports, [ADR-0002](0002-core-and-sub-modules.md) settled that no plane ships in core, and [ADR-0003](0003-how-a-leaf-addresses-a-plane.md) settled what an address is.
None of them decided the signature the address appears in.

The inherited answer is xload's `Loader.Load(ctx context.Context, key string) (string, error)` ([loader.go:9-11](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/loader.go#L9-L11)).
Three of its four elements are already ruled out by decisions that have been taken: the key is a `string` where ADR-0003 fixed a structured `Path`, the value is a `string` where the project charter states a preference for typed values, and there is no Dump counterpart at all.
What survives is the shape - one call, one key, one answer - and ADR-0003 rules that out too, because a driver's injectivity obligation cannot be discharged one key at a time.

This ADR decides the signatures, the value that crosses them, how a read-only plane refuses, and the first-party driver list that [ADR-0002](0002-core-and-sub-modules.md) deferred here.

It is written from a throwaway prototype on branch `proto/5-source-sink`, which never merges.
Every number below is from that prototype unless it cites the survey.
The prototype contains four drivers written as real drivers rather than as sketches - environment variables, HTTP query parameters, a Consul-shaped remote KV, and YAML over real files - because two of this ADR's questions are answerable only by writing the driver and counting.

## Decision

### The contract

```go
type Source interface {
    Bind(addrs *AddressSet) (Binding, error)
}

type Binding interface {
    Open(ctx context.Context) (Reader, error)
}

type Reader interface {
    Get(ctx context.Context, addr Path) (Value, error)
}

type Sink interface {
    Bind(addrs *AddressSet) (WriteBinding, error)
}

type WriteBinding interface {
    Open(ctx context.Context) (Writer, error)
}

type Writer interface {
    Set(ctx context.Context, addr Path, v Value) error
    Commit(ctx context.Context) error
    Abort()
}
```

Six interfaces, three methods on the required read path, five on the required write path.
The rest of this section is why each of those is the number it is.

### Bind is a separate phase because the two halves have different lifetimes

ADR-0003 requires that a driver see the whole address set before any I/O, and that its plane keys be precomputed rather than derived per lookup.
It states that as a requirement of the design and not an optimisation, on measured grounds: a precomputed lookup is 10.4 ns against 8.5 ns for a bare flat-map lookup, where computing per call costs 109 to 477 ns against a 476 ns twelve-key load.

Reproduced here on the whole path rather than on the lookup, for a six-address load through the query-parameter driver:

| | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| bind once, open per load | 152 | 16 | 1 |
| bind and open on every load | 2691 | 1272 | 56 |
| flat string keys, no address model at all | 76 | 0 | 0 |

So the precompute has to survive across loads, and the only question is where it lives.

**The first answer this prototype reached was that core memoises it, and it is wrong two separate ways.**
Both were found by auditing a design that already looked finished, and both are recorded because the shape of the mistake is more instructive than the fix.

Memoising on `(address set, driver name)` silently gives one driver another driver's keys.
Measured: `EnvSource{Sep: "__"}` and `EnvSource{Sep: "_"}` share a cache entry, and the second one loads `METRIC__HTTP__PORT`.
The separator is the one thing ADR-0003 made a driver option, so this is the headline case rather than an exotic misuse.

The obvious repair - let the driver supply a comparable identity capturing everything its key function reads - is unsound.
Measured: using a driver value as a map key panics with `hash of unhashable type main.EnvSource`, because the driver holds a func field, as any driver taking a dialer, a hook or a clock will.
A contract whose correctness depends on a driver author supplying the right identity is a prose rule with a runtime panic behind it, which is the shape ADR-0001 rules out.

**Splitting the phase removes the cache, and with it both defects.**
The address set and the plane data have genuinely different lifetimes: the key table depends on the driver and the schema, the data depends on when you asked.
`Bind` is the first, `Open` is the second, and nothing is memoised so nothing can be memoised wrongly.

`Bind` takes no `context.Context`, and that is load-bearing rather than tidy: it is how the type says that no I/O happens here, which is exactly where ADR-0003 requires the legality and injectivity checks to run.

### There is no batch interface, because there is nothing left for one to do

The ticket proposed the survey's option 3: a per-key interface required, a batch form as an optional interface upgrade, memoisation always on.
None of the three survives contact with the split.

**Batch versus lazy is a branch inside one driver.**
`Bind` already handed over the address set, so `Open` can fetch everything in one round trip or fetch nothing at all, and ferry never needs to know which.
Measured on a six-leaf schema against the Consul-shaped driver:

| | backend calls |
| --- | --- |
| xload's per-key pull | 6 |
| this contract, driver chose lazy | 6 |
| this contract, driver chose batch | 1 |

The difference between the second and third rows is one boolean in `drv_kv.go`.
An optional `Snapshotter` interface would be ferry defining, versioning and conformance-testing a second contract to express a choice the driver can already make.

**In-load memoisation has nothing left to deduplicate.**
5.13's first reproduction was that two struct fields sharing one key produce two backend calls.
Under ADR-0003 that schema does not compile: the address set is prefix-free and a path is a prefix of itself, so `schema Shared: address /host is claimed by both Primary and Legacy`.
Half of 5.13 was fixed by the address decision before this ticket opened, and the remaining half is composition, below.

### Absence is a kind of the value, not a second return value

xload's signature cannot express absence (5.1), and its own `cached` provider invented `Get(key string) (*string, error)` to work around exactly that.
The consequence propagates: `required` is `val == "" && meta.required`, `setVal` silently no-ops on empty, and a decoder is never handed the empty string.

5.1 weighed three replacements and recommended comma-ok.
There is a fourth it did not weigh, and it is the one this ADR takes: **absence is `Value.Kind() == Absent`, and `Get` keeps the ordinary Go `(T, error)`.**

All four express the three states, so correctness does not separate them, and on the miss path neither does cost - 10.5 ns and zero allocations for comma-ok, pointer and kinded alike, against 199 ns and three allocations for a sentinel error, which is disqualifying on its own because absence is the common case in a config load.

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
No new discipline is required of ferry's own leaf setters; the kind switch they already write covers it.

**It survives being stored, which is the part that matters beyond this ADR.**
`Absent` is kind zero, so a `map[Path]Value` lookup miss *is* absence, and a recording sink needs no parallel presence map to hold the observation.
ADR-0001 milestoned plane inspection on the grounds that a loaded struct erases absence.
This is the boundary type not erasing it, which is the mechanism that milestone commits to rather than the feature.

**What this ADR does not decide** is what an absent address then means to a Go field - whether it takes a default, whether `null` and absent are the same thing to a `*string`, and whether `FOO=` satisfies `required`.
That is [#8](https://github.com/onhotpath/ferry/issues/8)'s.
This ADR only fixes that the contract can tell them apart, which 5.1 says xload's cannot.

### Values cross typed, and the justification is still Dump

Section 4 of the survey reaches a decisive result: Load survives a string boundary because the destination struct field type drives parsing, and Dump cannot, because the sink must choose a representation and a string gives it nothing to choose from.

That result was measured on a flat key space with composite values, and ADR-0003 changed the address model underneath it, so it is re-measured here rather than inherited.
The same seven values dumped through a real YAML sink and loaded back through a real YAML source:

| | addresses returning the original value exactly |
| --- | --- |
| typed | 7 of 7 |
| stringified | 3 of 7 |

with `null` becoming `""`, `true` becoming `"true"`, `8080` becoming `"8080"` and `3.5` becoming `"3.5"`, permanently.
The asymmetry survives the address change.
What changed is the size of the loss rather than its existence: composites no longer flatten into one value, so the damage is now confined to the scalar leaf.

The premise check in section 4 stands and this ADR states it out loud rather than designing around the minority case silently.
A typed boundary buys YAML and TOML something real, JSON something partial, and Consul, environment variables and query parameters nothing at all - and two of ferry's four first-party drivers are in the last group.
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
That is what removes the `any` field the survey's sketch had, and it buys three things at once: `Value` is 24 bytes, it has no boxing allocation, and **it is comparable**.
`slog.Value` gives comparability up with `_ [0]func()` and `protoreflect.Value` with `pragma.DoNotCompare`, both in exchange for unsafe packing that section 4 already established ferry does not need.
Comparability is what lets the round-trip harness and the conformance suite assert with `==` rather than with a bespoke equality function, and it is what makes `map[Path]Value` a usable recording sink.

**Quoting survives**, which is the one thing section 4 identifies that a string boundary genuinely destroys: `port: 8080` arrives as `Number("8080")` and `port: "8080"` as `String("8080")`, and both round-trip back to their own spelling.

#### There is no group arm, and this reverses a survey recommendation

Section 4 says a group arm is "required, not optional", because xload's flattening is exactly where the YAML list is lost (5.8, reproduced).
That was correct for a flat key space and is obsoleted by ADR-0003, which landed after it.

Under a structured address a composite gets one address per element, so `servers: [a, b]` is read at `/servers#0` and `/servers#1` and nothing ever asks the plane for the value *at* `/servers`.
Measured: the indexed form carries every element exactly, and 5.8's list-becomes-empty-string is not merely avoided but unrepresentable.
The remaining case, a flat plane holding a whole list in one value as `TAGS=a,b,c`, arrives as a scalar `String("a,b,c")`, and splitting it is a codec's job on ferry's side of the boundary where the target Go type is known.

So every address in a compiled schema is a leaf, and a group arm would be an arm no address can be at.
A driver asked for a composite address anyway refuses loudly - measured: `yaml: !!seq is a sequence, not a scalar`.

**One case this forecloses, stated because it is the strongest objection and it survives.**
Mapping a structured subtree onto a single Go field opaquely - a YAML `servers:` block into a `json.RawMessage` - is not expressible, because the driver would have to re-serialise the subtree without knowing that the target wants bytes, and it refuses instead.
The neighbouring case is unaffected: a Consul key or an env var *holding* an encoded blob is already a scalar and arrives as `Bytes`, which is section 4f's actual motivating example.
So what is lost is opaque capture from a plane that has structure, and that is a feature ferry would have to want on purpose.
It is left to [#7](https://github.com/onhotpath/ferry/issues/7) and [#12](https://github.com/onhotpath/ferry/issues/12), because the mechanism it would need is a codec claiming a subtree rather than a kind in the value model, and it would also have to answer to ADR-0001's round-trip guarantee before it could ship.

#### There is no escape arm either, and that is the weakest call in this ADR

Section 4's argument for one is strong and is stated here rather than dismissed: `slog.Value`'s `KindAny` is why its kind set never had to grow, `attribute.Value` shipped closed and its enum has already grown twice leaving a deprecated alias behind, and adding a kind after v1 is a breaking change.

It is not taken, for two reasons that are specific to ferry rather than general.
ADR-0001 closed core's type set and made extension explicit and proof-carrying, so "a type ferry cannot name" is already a compile-time error rather than a value that has to be representable at runtime.
And an escape arm holding a driver-native value is uninterpretable by any other driver, which would break plane-to-plane transfer - Enabled in ADR-0001 and expected to fall out of the pluggable design for free.

The mitigation is ADR-0002's: ferry is at v0, and v0 is the only place semver allows taking this back.
If a plane appears that needs a kind this set cannot express, adding it before v1 is legal, and the trigger for v1 is already named.

#### `jsontext.Token` is mirrored, not adopted

The ticket asks for this priced three ways, and the attractive facts are real: it is a raw-text-plus-kind union with `(T, error)` accessors, it imports no `reflect`, and it is already GA.
Verified on `go1.27rc2`, three facts decide it, and the first two would decide it even if ADR-0002 did not exist.

**It is a stream token, not a value.**
`jsontext.BeginArray` and `EndArray` are `Token`s whose `Kind()` is `[` and `]`.
A ferry `Value` must never be able to hold "the start of something", and a type whose domain is stream positions is the wrong domain.

**Holding one past the next read panics.**
Measured: a `Token` read from a `jsontext.Decoder` and kept without `Clone` panics with `invalid jsontext.Token; it has been voided by a subsequent json.Decoder call`.
It does not degrade, it panics.
ferry hands values to third-party driver code and stores them in maps, and section 4 already ruled that accessors must not panic; a value model with a use-after-read panic is disqualifying on ferry's own stated rule.

**It has no bytes kind**, because JSON has no binary scalar, and a Consul or Registry plane has nothing else to hand over.

Only then does ADR-0002 apply, and it applies decisively: core imports only unconditionally-available stdlib, and `jsontext` vanishes under `GOEXPERIMENT=nojsonv2`.
Adopting it into core would be an amendment to ADR-0002 argued explicitly.
There is nothing to argue, because mirroring its shape costs nothing and the three facts above say mirroring is what ferry wants anyway.
Measured for completeness: `jsontext.Token` is 32 bytes, ferry's candidate is 24 and comparable.

### Source and Sink are separate, and a read-only plane refuses in two places

They stay separate, and the deciding case is one ADR-0002 already settled: environment variables have no honest Dump.

Under one combined interface, `EnvSource` and `QuerySource` would each have to declare a `Dump` they cannot honour.
ADR-0002 refused to put half a driver in core for precisely this reason, and the same argument applies to a driver outside core.
With two interfaces the refusal is free: `EnvSource` does not implement `Sink`, so dumping to it is a compile error at the call site rather than a runtime error, prose, or a returned `ErrUnsupported` nobody reads.

**A plane that is writable in principle but not right now refuses at `Open`**, with an error wrapping `ErrReadOnly`.
Measured, both landing at `Open`: a KV with no write ACL, and a YAML sink targeting a `0555` directory.

`Open` rather than `Bind` because writability is a fact about the plane and not about the schema, and `Bind` does no I/O so it cannot know.
`Open` rather than the first `Set` because Dump is the direction that runs a walk over the user's struct: failing at `Open` costs nothing, and failing at the first `Set` has already half-written the plane.

### The Writer has three methods, and the third one earns its place

`Set` accumulates, `Commit` is the point a whole-document plane serialises, and `Abort` is what a staging sink needs when the walk fails partway.

`Abort` is not symmetry.
Measured against the YAML sink, which stages into a temp file and renames on commit: with `Abort`, zero temp files are left behind; with the writer simply dropped, one is.
And a partway failure is the normal case rather than the exotic one, because 5.4 requires ferry to aggregate errors and continue past a failed field rather than returning the first.

Measured separately: after `Abort`, an existing plane is byte-identical.

**This is also where ADR-0001's delta and partial dump milestone is honoured.**
That milestone commits only to the sink contract not precluding it.
It does not: the writer saw the address set at `Bind` and then a subset of `Set` calls, so "what was not set" is already computable at `Commit` with no new method.
Whether ferry ever exposes that is [#8](https://github.com/onhotpath/ferry/issues/8)'s and a later Option's.

### The address set handed to `Bind` is the static set, and core hands back a key function

This is the second correction the prototype made to itself, and it is the one that matters most for anyone implementing this.

ADR-0003 is explicit that not every address comes from the type: a map key's address and a slice element's index come from the value, so both collision rules run in two tiers, the second "as each is minted, before the write it belongs to".
Every probe in this prototype up to that point used a schema containing no map and no slice, which hid the consequence.

Measured with a schema whose static set is `{/name}`, dumping a `map[string]string` field to the KV driver: `Set(/labels/env)` returned `kv: address not in the opened set: /labels/env`.
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
| static hit | 9.1 | 0 |
| bare map lookup, no address model | 7.3 | 0 |
| dynamic hit, after minting | 26.8 | 0 |
| single mutex over both tiers, static hit | 20.0 | 0 |

Concurrency is [#20](https://github.com/onhotpath/ferry/issues/20)'s, and this ADR decides none of it.
It records only that the two-tier split is what makes a lock-free static path available at all, so #20 inherits a choice rather than a constraint.

### Sources compose, and composition needs no core surface

5.12: `SerialLoader` is last-non-empty-wins, queries every loader for every key with no short-circuit, and because empty means absent a later higher-priority source can never override a value back to empty.

A composite is a `Source` whose `Bind` binds its children and whose `Get` returns the first present value.
Measured on a six-leaf schema over three backends:

| | backend calls |
| --- | --- |
| xload `SerialLoader` | 18 |
| first-present-wins over batch-fetching children | 3 |

**First-match-wins is correct only because absence is observable**, which is 5.12's actual cause rather than its symptom.
ADR-0003 recorded that precedence under a structured address is a question about values at one address rather than about reconciling key spaces, and that is what this is.

Two things this deliberately does not do.
It does not put a composite in core's exported surface; it is an example, on the same footing as plane-to-plane transfer in ADR-0001.
And it does not establish a precedence *convention* - flags beating environment beating file - which ADR-0001 rules out by remit.
What is decided here is only that the contract does not preclude composition and that short-circuiting it is sound.

### Enumeration is optional, and the asymmetry is smaller than ADR-0003 feared

ADR-0003 asked this ticket to decide whether sources enumerate, and said that if the answer is no, then map-keyed and indexed addresses are Dump-only and ferry's two directions cover different address sets.

**The answer is neither yes nor no.**

```go
type Enumerator interface {
    Children(ctx context.Context, prefix Path) ([]Path, error)
}
```

An optional interface a `Reader` may implement.
It cannot be required, because it would exclude a plane class ferry explicitly wants: a Vault kv-v2 `LIST` is a separate ACL capability and a token with read and no list is ordinary, and some secret brokers answer only what you name, by design.
It cannot be omitted, because every one of ferry's four first-party planes can enumerate trivially - `os.Environ`, a parsed YAML tree, `url.Values`, a KV prefix list - so omitting it would make ferry unable to load a map from any plane, to no benefit.

So the two directions cover:

- **Dump**: every address, always. The value is in hand, so map keys and sequence lengths are known.
- **Load**: static addresses always; dynamic addresses from any source implementing `Enumerator`.

That is a real asymmetry and it belongs here rather than being discovered later, as ADR-0003 asked.
What it is **not** is "map-keyed addresses are Dump-only".
They are loadable from most planes, and unloadable from the ones that genuinely cannot list.

The enumerator returns addresses rather than names, and that is deliberate: measured, `/limits` yields Name segments and `/tags` yields Index segments, so the plane answers which composite it is rather than the caller guessing from base-10 text - the limitation ADR-0003 quotes `jsontext.Pointer`'s own godoc admitting.

Loading a map-typed field from a non-enumerating source is an error naming the field and the source, never a silently empty map.
An empty map is the most plausible-looking wrong answer available, and ADR-0001 rules out ignoring anything silently.

Whether any supported Go type produces these addresses at all is [#7](https://github.com/onhotpath/ferry/issues/7)'s.
Whether an absent map is empty or defaulted is [#8](https://github.com/onhotpath/ferry/issues/8)'s.

### What a driver costs, against the bar ADR-0001 named

ADR-0001's consequences name the adoption bar: koanf gets twenty providers off a two-method interface at 31 to 246 lines each, median around 120.
The four drivers in the prototype, written as real drivers, counting non-blank non-comment lines:

| driver | directions | lines |
| --- | --- | --- |
| query parameters | source | 68 |
| environment variables | source | 67 |
| KV, Consul-shaped | source and sink | 157 |
| YAML, real files | source and sink | 180 |

Inside koanf's band, and the two that exceed its median are the two that also implement a sink, which koanf has none of.

A read-only driver implements three methods where koanf's bar is two.
What the third buys is the whole of the `Bind` section above.
Two of these four drivers satisfy `Binding` with a method that returns the receiver, because their plane needs no snapshot.

One asymmetry is worth stating because it surprises: **a tree plane pays nothing for the address set.**
The YAML driver never builds a plane key, it walks the segments, so it has no injectivity obligation and makes no key-table call at all.
ADR-0003's driver-side rule binds flattening drivers only.

### The first-party driver list

ADR-0002 deferred this here, because its admission rule is that a first-party driver ships only to exercise an axis of the driver contract that no existing first-party driver exercises, and the axes are a property of these signatures.
Now that the signatures exist the axes can be read off them:

| axis | env | query | kv | yaml | memory plane |
| --- | --- | --- | --- | --- | --- |
| produces a plane key, so carries the injectivity obligation | x | x | x | | |
| walks segments as a tree instead | | | | x | |
| has a serialization format | | | | x | |
| carries plane-side type information | | | | x | |
| opaque bytes only | | | x | | |
| real I/O, cancellation, partial failure | | | x | x | |
| batch versus lazy inside one Open | | | x | | |
| whole-document sink: Commit and Abort | | | | x | |
| no honest Dump at all | x | | | | |
| per-request, hot path | | x | | | |
| enumeration | | | | x | |

**The list is `yaml`, `kv` and `env`, with `query` named as a candidate rather than a commitment.**

- **yaml** reaches five axes nothing else does: a format, a tree walk, plane-side type information, the whole-document sink, and enumeration. It is the driver that keeps the conformance suite honest.
- **kv** is the only real I/O, the only opaque-bytes plane, the only batch-versus-lazy choice, and the only dynamically read-only sink. Consul-shaped rather than Consul, so the exact backend stays open.
- **env** is the flat key function with a transform, and the source-with-no-sink case, which is the one that keeps `Source` and `Sink` honestly separate.
- **query** is the only per-request axis, and it is the weakest of the four: its key function is a flat join like env's, so it earns its place on hot-path pressure alone rather than on a contract axis. It also has an unresolved problem of its own, below.

The memory plane's column is empty on every axis, which is ADR-0002's own point restated as a measurement rather than as an argument.

### What this ADR does not decide

- Whether an absent address takes a default, whether `null` and absent mean the same thing to a Go field, and whether a present-and-empty value satisfies `required`: [#8](https://github.com/onhotpath/ferry/issues/8).
  This ADR fixes only that the contract can tell the three apart.
- Which Go types map onto which kinds, and whether any type produces an Index segment: [#7](https://github.com/onhotpath/ferry/issues/7).
- How a `Value` becomes a Go value and back, and the precedence of the chain that does it: [#12](https://github.com/onhotpath/ferry/issues/12).
- The error types every refusal here produces: [#9](https://github.com/onhotpath/ferry/issues/9)'s convention, applied rather than invented.
  This ADR uses `errors.Join` and sorted reports to satisfy ADR-0001's determinism invariant, and defers the types.
- Whether `Get` and `Set` may be called concurrently, and what a driver may assume: [#20](https://github.com/onhotpath/ferry/issues/20).
- The exported verb names, which ADR-0001 left open.
  `Bind`, `Open`, `Get`, `Set`, `Commit` and `Abort` are the driver-facing names and are decided here; `Load` and `Dump` remain the working assumption for the caller-facing ones.

### A decision with no ticket, surfaced rather than taken

The caller-facing lifecycle has no owner on the map, and this ADR creates the need for one.

`Bind` is reusable and `Open` is per load, so a caller holds something between them.
For every plane whose handle is long-lived - a file path, a KV client - that is unremarkable: the source holds the plane, bind once, open many.
For a **per-request plane it does not work**, because `QuerySource{Values: v}` is constructed per request, so `Bind` is per request too, and the measurement above says that costs 2691 ns and 56 allocations instead of 152 ns and 1.

The plane instance has to be suppliable at load time rather than at bind time, and none of the three available answers is obviously right: put it in the `context.Context`, add a caller-facing `LoadFrom` taking a `Reader` the caller already has, or make the plane a parameter of `Open` and change what `Source` means.

This is not decided here, because deciding it inline would settle the caller-facing API as a side effect of a driver-facing ADR.
**Proposed as a new ticket on [#1](https://github.com/onhotpath/ferry/issues/1): "How a caller holds a binding, and where a per-request plane is supplied."**
It blocks nothing in this ADR; the contract above is the same either way.

## Consequences

- ferry's driver contract is three methods to read and five to write, against koanf's two.
  The extra ones are individually justified above and the measured driver cost stays inside koanf's band, but the bar was raised and adoption is the thing at risk.
- Core owns a key-function helper that runs ADR-0003's legality and injectivity checks.
  That is ADR-0002 route (b) - core's obligation shipping as the thing that discharges it - and it means a driver that hand-rolls its own key table silently opts out of the check.
  The conformance suite has to test for that, not just for the checks themselves.
- The value model is closed at six kinds with no escape arm, so adding a kind is a breaking change after v1.
  This is the one place this ADR knowingly departs from a survey recommendation on grounds specific to ferry, and v0 is the whole mitigation.
- `Value` is comparable, which the round-trip harness and the conformance suite can both rely on, and which no comparable library's value type offers.
  It is also a constraint: it forecloses ever adding a slice or map field to `Value`.
- Two of ferry's four first-party planes gain nothing from the typed boundary.
  The cost is paid on every plane and collected on some, and the ADR says so rather than selling the feature evenly.
- ferry's two directions cover different address sets, and the difference is visible to users as "this source cannot load a map".
  It is a smaller asymmetry than ADR-0003 anticipated, but it is a real one and it is now a documented property of a driver rather than a surprise.
- Composition is expressible and unshipped.
  Someone will ask for it in core, and the answer has to be ADR-0001's remit rule rather than a technical one.
- The caller-facing lifecycle is now load-bearing and unowned.
  Until the proposed ticket lands, the per-request use case xload was pitched at has no worked answer.

## Items from the xload survey

The survey is [`docs/research/generics-and-modern-go.md`](../research/generics-and-modern-go.md), section 5, against `github.com/gojekfarm/xtools` at [`a90b3aa`](https://github.com/gojekfarm/xtools/commit/a90b3aad2133248cec50f6b4d6e37b0d9e788adb).

**5.1, the `Loader` signature cannot express absence.**
Addressed, and this ADR owns the fix ADR-0001 deferred here.
Absence is a kind of the value rather than a second return value, so a driver cannot express it wrongly and a caller cannot discard it.
Reproduced first: through xload's shape, `EMPTY` and `MISSING` are one observation; through this contract they are two, plus `SET`, at the same 10.5 ns and zero allocations.
The survey's own recommendation was comma-ok; this ADR takes a fourth option it did not weigh, and says so.

**5.12, `SerialLoader` precedence is unexpressible.**
Addressed.
First-present-wins is expressible and cheap, measured at 3 backend calls against 18, and it is correct only because 5.1 was fixed first.
The convention that would sit on top of it stays ruled out by ADR-0001's remit.

**5.13, the per-key pull model amplifies backend round trips.**
Addressed, and half of it was already dead.
The duplicate-key half is a schema that does not compile under ADR-0003's prefix-free rule.
The remaining half collapses from 6 calls to 1 with no interface beyond `Bind` and `Open`, because the driver chooses.
The survey's recommended shape - required per-key plus an optional batch upgrade - is rejected with the measurement that makes the upgrade redundant.

**5.14** was enumerated rather than assumed, all four items.

- *Two ways to set the loader.*
  Bears on this ADR directly, and is avoided by construction: there is one way to supply a source, which is to pass something implementing `Source`, and no driver type doubles as an option.
- *The `CanAddr` loop that can only run once.*
  A defect in the reflection walk, not at this boundary.
  It does not bear on these signatures.
- *The non-deterministic select on a cancelled context.*
  Bears on this ADR in that every method here except `Abort` takes a `context.Context` and a driver may return `ctx.Err()`.
  What ferry does when a cancellation and a driver error race is [#20](https://github.com/onhotpath/ferry/issues/20)'s, and this ADR neither fixes nor worsens it.
  `Abort` takes no context deliberately: it is cleanup, and cleanup that can be cancelled is how the temp file leaks.
- *Value receivers on `Error()` where pointers are returned.*
  Bears on `ErrReadOnly` and on the two refusals core produces.
  Deferred to [#9](https://github.com/onhotpath/ferry/issues/9)'s convention rather than pre-empted here, exactly as ADR-0003 did.

**5.8, type information destroyed at the boundary**, is ADR-0001's, and this ADR is where the fix becomes structural: a YAML sequence is read per element and a YAML scalar arrives with its tag, so there is no point at which `cast.ToString` could be called.

**5.11, the YAML provider silently discards parse errors**, is ADR-0001's.
This ADR gives it a place to land: a malformed plane fails at `Open` with a non-nil error, checked in the prototype's conformance run.

The remaining items are unaffected by this ADR.
