# 19. The concurrency model, and who writes `go`

Status: Accepted
Date: 2026-08-06
Ticket: [#20](https://github.com/onhotpath/ferry/issues/20)

## Context

[ADR-0001](0001-what-ferry-supports.md) left "whether ferry has a concurrent mode at all, in either direction" deliberately open, and every ADR since has deferred to [#20](https://github.com/onhotpath/ferry/issues/20) rather than pre-empting it.
Four of them handed it something concrete.

[ADR-0010](0010-the-entry-point-and-the-schema-cache.md) built the seam: "a concurrent mode, if #20 wants one, is a second **scheduler** and never a second walk", and handed over a hazard rather than a drop-in, reproducing a data race on [ADR-0006](0006-defaults-and-zero-values.md)'s presence bit under a goroutine-per-task scheduler.
ADR-0006 flagged the same bit.
[ADR-0004](0004-source-and-sink.md) recorded that the two-tier key split is what makes a lock-free static path available, so #20 inherits a choice rather than a constraint.
[ADR-0012](0012-the-caller-held-binding.md) shipped the binding and handed over the fact that a binding is now shared across goroutines, with a published promise that a later #20 answer must not break.

The question the ticket was originally asking is whether the **walk** runs concurrently.
The scenario it exists for turned out to be a different question: eight configuration keys that live across three backend services, where the wall clock a caller cares about is the slowest service and not the sum.

This ADR is written from the prototype on branch [`proto/04-concurrency`](https://github.com/onhotpath/ferry/tree/proto/04-concurrency), module `prototype/concwalk`, out of `go.work`, run with `GOWORK=off go test -race -count=5 .`.
It carries 23 tests and 2 benchmarks.
Every number below is from it.

## Decision

### The answer is layered, and the first layer is not core's

> **Batch at open is the first answer.**
> A driver that knows its plane's routing turns many round trips into few, and core writes no `go` to do it.
>
> **Core's fanout is the second layer**, gated by the driver and bounded by the caller.
>
> **One number is the budget at both layers.**

That order is not a preference, and it is the finding that reframed the ticket.

**A concurrent walk changes when the round trips happen; only the driver boundary changes how many.**
Measured over a slow per-key plane, 40 leaves at 200µs per round trip, with the three strategies proven to produce identical destinations:

| | round trips | wall clock |
| --- | --- | --- |
| serial walk | 40 | 43.1 ms |
| concurrent walk, 8 workers | **40** | 5.7 ms |
| prefetch at open, one `List` | **1** | 1.1 ms |

Prefetch is five times faster than eight-way fanout and does it with one backend call instead of forty.
The mechanism it needs already exists: `Bind` hands the driver the whole `AddressSet` before any I/O, which is exactly what ADR-0004 designed the phase split for, and [ADR-0016](0016-the-sealed-address-model.md) now hands it typed so the driver can group by kind as well as by key.

**And the multi-service case is where a core fanout is not enough on its own.**
Eight keys routed 2/3/3 across three backends at 1 ms, 2 ms and 3 ms:

| | round trips | wall clock |
| --- | --- | --- |
| serial walk | 8 | 18.3 ms |
| core per-address fanout, `MaxConcurrency(3)` | **8** | 7.1 ms |
| backend-grouped batches | **3** | 3.4 ms |

3.4 ms is the slowest service, which is the answer the scenario wanted, and **only the driver can produce it, because only the driver knows the routing.**

### Core ships one Option, and the driver consents

> ```go
> func MaxConcurrency(n int) Option           // the caller's bound
> type Concurrent interface{ MaxConcurrent() int }  // the driver's tolerance
> ```
>
> Core's scheduler is the only place `go` is ever written.
> It fans out only where **both** parties said yes, and never past `min` of the two bounds.
> Absence of the capability is serial.
> `MaxConcurrent() <= 0` means the driver imposes no bound of its own.

`Concurrent` is discovered by assertion on the **instance**, in exactly the `Releaser` and `Committer` idiom, because safety is a property of the open instance rather than of the `Source` value.
`env` and `yaml` simply never declare it; a networked KV driver does.

Proven with an inflight meter:

```
MaxConcurrency(4), instance without the capability  ->  peak overlap 1
MaxConcurrency(4), instance with the capability     ->  peak 2..4, never 5
```

**The method returns a number rather than being an empty marker**, and that is a correction taken during design rather than the first shape.
An empty `ConcurrentSafe()` marker was prototyped and works; it is ceremony, with nothing to implement and nothing to return, and its only content is its own name.
A method that returns the driver's real tolerance - a pool size, a rate limit - **is information**, and core clamps to `min(option, tolerance)`, so the caller bounds, the driver bounds, and core spends inside both.
The cost is a documented convention for "no bound of my own", which is the `<= 0` line above.

Two further spellings were costed and rejected.
A `Capabilities() Caps` struct is attractive because one door could carry retraction, section touch and concurrency together and end the assertion zoo, but `Releaser` and `Committer` are **behaviour** and a struct of facts cannot hold them, so it is either two mechanisms side by side or a much larger migration riding this decision.
A driver Option is the wrong axis: an Option is a caller's choice, and whether a plane tolerates overlap is a fact about the plane, so `consul.Parallel(8)` on an unsafe client makes a contract violation expressible in one line.

### One budget, honoured at both layers

The refining question was whether a driver should subdivide one backend's keys inside the budget left over after grouping.
Prototyped on twelve keys routed 2/4/6 with a budget of six:

| cost model | one batch per backend | hybrid subdivision |
| --- | --- | --- |
| size-dependent, 1 ms base plus 0.5 ms per key | 4.4 ms | **2.4 ms** |
| flat per round trip | 3.27 ms | 3.32 ms, and twice the requests |

It pays under one model and is a wash under the other, and **core cannot know which model a plane has.**
So subdivision is the driver's call, and the budget is one number rather than two:

> ```go
> func ConcurrencyBudget(ctx context.Context) int
> ```
>
> The caller sets `MaxConcurrency(n)`.
> Core's own fanout obeys it, and the same number rides the open's `context.Context`, so a driver can size its request parallelism inside the caller's budget instead of beside it.

Two budgets would let a caller who asked for three get six, which is the defect a budget exists to prevent.

**Dump's flush stays the driver's, at `Commit`.**
Nothing in this ADR fans out the write path, because ADR-0004 already put the staging decision inside the sink and a sink that batches does it where it already batches.

### Serial equivalence is the gate, and it is a `ferrytest` property

> A concurrent run must produce a destination and an error report **byte-identical** to the serial run.

That is asserted three ways and it is what makes fanout safe to enable at all.

The scheduler combines in **task order and never completion order**, which makes the report deterministic on top of [ADR-0011](0011-the-error-model.md)'s sort at construction.
Measured: 50 concurrent runs report byte-identically to serial.

**And the equivalence test has a trap [ADR-0010](0010-the-entry-point-and-the-schema-cache.md) already named**: survey item 5.2's sharpest detail is that a shared destination makes a broken second walk pass.
Every equivalence subtest gets a fresh destination, which is already this repository's rule.

For a driver asserting `Concurrent`, the property belongs in the conformance suite rather than in that driver's own tests, because a driver that declares the capability is making a claim core relies on.

### The three walk facts this rests on live in the ADRs that own them

This ADR does not restate them and it could not exist without them.

- **The walk returns an outcome value per subtree**, not a bool and not a shared counter, which is [ADR-0006](0006-defaults-and-zero-values.md) amended.
  A shared counter gives a **deterministically wrong** answer under a concurrent scheduler - a sibling's write materialises a pointer over an absent subtree, with an atomic counter and a silent race detector - and that is what a value returned per task makes unrepresentable.
- **The scheduler seam takes a count and one body**, which is [ADR-0010](0010-the-entry-point-and-the-schema-cache.md) amended.
  Measured on a 20-member container: 417 ns, 480 B and 21 allocations for the closure seam against **45 ns, 0 B and 0 allocations** for the index seam.
- **User-code panics become addressed errors and ferry's own stay panics**, which is [ADR-0011](0011-the-error-model.md) amended.
  Under fanout a panicking codec in one goroutine must not take the aggregate with it, and a fence that swallowed ferry's own bugs would hide the very defects a new scheduler introduces.

One more is a defect against a rule ADR-0004 already states rather than a new decision.
ADR-0004 says `Close` always runs, and the shipped entry point runs it on a straight line, so a panic skips it and the handle leaks.
**Release is deferred, unconditionally**, on success, error, cancellation and panic, with `Commit` still on the success path only, so closed-without-`Commit` survives as the abort signal.
Reproduced and fixed in the prototype, and it is [#254](https://github.com/onhotpath/ferry/issues/254).

### What this ADR does not decide

- **Whether any first-party driver asserts `Concurrent`.** Each driver's, on its own plane's facts. `env` and `yaml` will not.
- **How a driver batches internally.** Deliberately: the hybrid measurement shows the right answer depends on a cost model only the driver has.
- **Whether Dump ever fans out.** Nothing asks for it; the staging decision is already the sink's.
- **What happens when a cancellation and a driver error race.** Survey 5.4's, unchanged by this ADR, and the walk checks cancellation where [ADR-0010](0010-the-entry-point-and-the-schema-cache.md) put it.
- **Whether the `Capabilities()` consolidation ever happens.** It is the right shape only alongside a full `Releaser`/`Committer` migration, and it is recorded as available.

## Consequences

- **#20 is answered as a layered model rather than as a yes or a no**, and the ticket is retitled to it.
  Driver batch-at-open is the first answer because it is the only one that changes the number of round trips: 1 against 40, and 3 against 8 on the multi-service case, where a core fanout leaves the count untouched and only overlaps it.
- **Core ships one Option and writes `go` in exactly one place**, gated by an optional `Concurrent` capability on the instance and bounded by `min` of the two numbers.
  Absence is serial, so no existing driver changes behaviour and none has to be audited before this lands.
- **The capability returns a number rather than being an empty marker**, so the declaration carries the driver's real tolerance and core can clamp.
  The cost is one documented convention, `<= 0` meaning no driver-imposed bound.
- **The budget is one number reaching both layers**, through the Option for core's fanout and through the open's context for the driver's own batching.
  Two budgets would let a caller who asked for three get six.
- **Serial equivalence is a conformance property for any driver that asserts the capability**, not a promise in prose, and the report is deterministic because the scheduler combines in task order.
- **The walk changes this rests on are three amendments in three ADRs rather than a fourth model here.**
  The outcome value is ADR-0006's, the index seam is ADR-0010's, and the recover fence is ADR-0011's; each is amended where its rule already lives.
  Deferred release is not an amendment at all, because ADR-0004 already says `Close` always runs and the shipped code does not.
- **Two required deliverables ride this decision**: `docs/concurrency.md` and a runnable `examples/concurrent-driver`, because a capability a driver author cannot see worked end to end is a capability nobody implements correctly.
- **[ADR-0012](0012-the-caller-held-binding.md)'s published promise survives.**
  A binding is safe from many goroutines, and nothing here makes a walk mutate anything reachable from one: the static key table is written once, the minted set is per open, and every per-subtree fact is now a return value.

Evidence: `prototype/concwalk` on [`proto/04-concurrency`](https://github.com/onhotpath/ferry/tree/proto/04-concurrency), 23 tests under `-race -count=5` and 2 benchmarks, including `TestSharedCounterMaterialisesOnSiblingWrite`, `TestOutcomeComposesUnderConcurrentScheduler`, `TestConcurrentErrorsByteIdenticalToSerial`, `TestIndexSeamAggregationIsTheSchedulers`, `TestRoundTripLedger`, `TestMultiRoundTripLedger`, `TestCapabilityGate`, `TestHybridPaysWhenCostGrowsWithSize`, `TestHybridIsAWashWhenCostIsFlat`, `TestShippedShapeLeaksOnPanic` and `TestDeferredReleaseClosesOnPanic`.
