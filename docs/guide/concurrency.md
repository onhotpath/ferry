# Concurrency

ferry's concurrency model is layered: the caller decides how much, the driver decides where it pays, and core spends only what both allow.
This page explains the model as [ADR-0019](../adr/0019-the-concurrency-model.md) decided it, and everything on it is shipped.

The whole model is three parties and two consents:

```mermaid
flowchart LR
  subgraph caller["CALLER - decides how much"]
    OPT["MaxConcurrency(n)<br/>one budget, set once"]
    GOR["n goroutines over one Binding<br/>across-call concurrency, free today"]
  end
  subgraph core["CORE - spends within consent"]
    SCHED["the scheduler<br/>serial by default<br/>fans out per address only where<br/>budget AND capability both say yes<br/>combines outcomes in task order"]
  end
  subgraph driver["DRIVER - knows the plane"]
    CAP["Concurrent capability<br/>MaxConcurrent() - declared tolerance<br/>absent on env and yaml, present on networked planes"]
    BATCH["batch at open<br/>the AddressSet is known since Bind<br/>one round trip beats any fanout"]
    BUDGET["ConcurrencyBudget(ctx)<br/>sizes private request parallelism<br/>inside the same envelope"]
  end
  OPT --> SCHED
  CAP --> SCHED
  OPT -.->|"rides the open's ctx"| BUDGET
  GOR -.->|"each call serial unless gated"| SCHED
```

No arrow crosses a boundary uninvited: core writes `go` exactly once, in the scheduler, and only where the caller's budget and the driver's declared tolerance both allow it; the driver spends the same budget privately behind its open, where core never looks.

## The two axes

Concurrency in ferry happens on two different axes, and they are different features.

**Across calls.**
Many Loads or Dumps at once, each walk serial.
This exists today and needs no option: a `Binding` or `SinkBinding` is safe for use from many goroutines, because everything it holds is written once at `Bind` and never again.
Three values loaded from three sources on three goroutines is already fully parallel, and the wall-clock is the slowest single load.

The lifetimes are what make that free:

```mermaid
flowchart LR
  SRC["Source / Sink value<br/>process-lived, caller constructs and owns<br/>holds the long-lived handle"] -->|"Bind: once, no I/O"| BND["Binding / SinkBinding<br/>process-lived, written once<br/>schema + AddressSet + key table"]
  BND -->|"Load / Dump call 1..n, concurrently"| OPN["open(ctx) - per call<br/>fresh instance over the plane's<br/>current contents"]
  OPN --> RDR["Reader / Writer instance<br/>serves exactly ONE walk<br/>never shared between calls"]
```

Everything left of the open is immutable and shared without a lock; everything right of it is minted per call and shared with nobody.
That boundary is why concurrent opens are the driver's published obligation while a single instance never faces concurrency unless it declared the capability.

**Within a call.**
One Load, one plane instance, the walk's per-address I/O overlapped.
This is the axis the rest of this page describes, and it exists only where three parties agree:

- the caller allows it, with the `MaxConcurrency(n)` option;
- the driver's instance offers it, by implementing the optional `Concurrent` capability (`MaxConcurrent() int`);
- core then fans out, bounded by the smaller of the two numbers.

Declaring `Concurrent` is a promise about everything the instance reaches, not only about its own fields.
`Get`, `Probe` and `Children` may all be called at once, and so may anything the instance closes over: a client, a cache, a key function, a caller-supplied callback.
An instance holding mutable state per open guards it, or does not declare the capability.

A driver that does not implement `Concurrent` is walked serially no matter what the caller asked for.
`env` and `yaml` never implement it: a file read and a process environment want one call, not eight.
Networked planes implement it because overlap is where their latency goes to die.
`driver/kv` is the one in this repository that does, and it is the worked example of what the promise costs: the [driver guide](drivers.md#concurrent) walks through it, including the one line every declaring driver owes its own key function.

## Batching beats fanout

Before reaching for `MaxConcurrency`, know that the bigger win usually belongs to the driver alone.

`Bind` hands every driver the complete `AddressSet` before any I/O happens, so a driver over a batch-capable plane can fetch everything in one round trip at open and serve the whole walk from memory.
Measured in the design campaign's prototype: 40 leaves over a 200µs-per-round-trip plane cost 43.1ms walked serially per key, 5.7ms under an 8-way fanout, and 1.1ms with a single batch fetch at open.
The fanout changes when the round trips happen; only batching changes how many there are.
The kv driver's batch-at-open is the reference implementation of this pattern.

## One budget, both layers

The caller's `MaxConcurrency(n)` is a single budget that both layers honour.

Core's scheduler obeys it when fanning out over a `Concurrent` instance.
The same number rides the open's context, where a driver reads it with `ConcurrencyBudget(ctx)` to size its own request parallelism, for example splitting one service's large batch into concurrent smaller calls.
Whether that split pays is the driver's knowledge: it helps when a service's cost grows with batch size, and it is pure rate-limit pressure when a round trip has flat cost.
No layer can exceed what the caller granted, whichever layer spends it.

## What fans out, and what does not

Fanout reaches the members a **type** names: a struct's fields, an array's elements, and what is under a pointer.
Those are the members core holds as a count, which is what the scheduler seam takes, and they are the whole of the multi-service shape ADR-0019 was written for - a handful of keys across a handful of backends, in a flat struct.

The members a **plane** names do not fan out.
A slice or a map field is filled from the segments the driver enumerated, minted and aggregated by the walk as it lists them, and a Go map written from several goroutines is a data race rather than a scheduler change.
So a wide struct of leaves overlaps and a five-hundred-key map does not.

One semaphore serves one whole walk, not one per container: a budget of 4 over a three-level struct is 4 overlapping reads and never 64.
The walk's first member at every container runs on the goroutine that got there, and only the rest ever try to take a slot, so a parent waiting on its children cannot be waiting for a slot it is itself holding.
That is also why a small container costs nothing: a walk of one member writes no `go` at all.

## What stays serial

Dump's writes stay serial per address in the walk.
Staging already splits a dump into an encode phase and a flush: the encode is pure CPU, and the flush is where the I/O lives, inside `Commit`, where the driver batches privately within the same budget.

Determinism survives concurrency by construction.
Every subtree of the walk returns one outcome value, and the scheduler combines outcomes and errors in task order, never completion order, so a concurrent walk reports byte-identically to the serial one.
The conformance suite holds any driver that declares `Concurrent` to exactly that equivalence.

## Panics under fanout

A panic out of your own codec is unaffected by any of this.
It is recovered at the call into the codec, on whichever goroutine made it, and it arrives in the report as an addressed `ErrPanic` while its siblings finish.

A panic ferry itself raises is louder under fanout than without it.
Raised on an overlapped member it is raised on a goroutine the caller does not own, so it ends the process rather than unwinding into a caller who could have recovered it.
Running the first member of every container inline narrows that window and does not close it.
It is a bug in ferry either way, and it should be reported as one.

## Caller-supplied functions

Options that accept a function, such as `env.Environ` or the kv driver's `Client`, state in their own documentation exactly when the function may be called concurrently.
The contract is static: it follows from the options supplied at `Bind` and the capabilities the driver declares, never from a runtime flag.
The rule of thumb: sharing a binding across goroutines is what makes your callable concurrent, and the sharer is the one who knows.
