# Prototype: session 04, concurrency & resources

Module `prototype/concwalk`, deliberately out of `go.work`.

Run it:

```sh
cd prototype/concwalk
GOWORK=off go test -race -count=5 .
GOWORK=off go test -bench=. -benchmem -run='^$' .
```

## What it proves

A miniature of core's walk, with the parts #122 and #20 argue about and nothing else.

1. **The shared counter gives a wrong answer under a concurrent scheduler, deterministically.**
   `TestSharedCounterMaterialisesOnSiblingWrite` forces sibling b's write into optional subtree a's before/after window.
   No data race fires - the counter is atomic - and a materialises a pointer over an absent subtree.
   This is xload defect 5.2's shape, reproduced as logic-wrongness rather than a race.
2. **The outcome shape cannot be wrong that way.**
   Every subtree returns one `outcome{wrote, minted, writes}` and the scheduler combines outcomes exactly as it combines errors.
   Same tree, same adversarial schedule: correct.
   `(outcome, error)` is two results, inside `function-result-limit 3`, and generalises #122 to all three shared-state sites (`loadFrom.wrote`, `dumpTo.minted`, `encodePhase.writes`).
3. **Determinism is the scheduler's obligation, stated once.**
   `TestConcurrentErrorsByteIdenticalToSerial`: a concurrent outcome scheduler combines in task order and sorts errors at construction, so 50 runs report byte-identically to serial.
4. **The seam can be kept and cost nothing: the index seam.**
   `sched(n int, run func(i int) error)` replaces `sched(tasks []func() error)`.
   Measured on a 20-member container: closure seam 417ns / 480B / 21 allocs, index seam 45ns / 0B / 0 allocs.
   Aggregation stays the scheduler's alone (`TestIndexSeamAggregationIsTheSchedulers`), so #176 section 2's 94 allocations go away without killing the seam or answering #20 by accident.
5. **Release must be deferred (#254).**
   `entryShipped` (the `join(walked, released(r))` straight line) leaks the handle on a codec panic - reproduced.
   `entryDeferred` closes on the panic path with Commit never called, so closed-without-Commit stays the abort signal, and the panic keeps unwinding.

## Round 2 additions (owner's questions)

6. **Where I/O-bound concurrency actually pays: the driver boundary, not the walk.**
   `ioconc.go`: three strategies over a slow per-key plane (40 leaves, 200µs RTT), identical destinations proven.
   Round trips counted by the client: serial walk 40, concurrent walk (8 workers) still 40, prefetch-at-open 1.
   Wall clock: serial 43.1ms, fanout(8) 5.7ms, prefetch 1.1ms.
   A concurrent walk changes WHEN the round trips happen; only the driver boundary (bind-time AddressSet -> open-time batch) changes HOW MANY.
7. **A codec panic becomes an addressed error and the walk continues (#254 follow-up, owner's R3 direction).**
   `recover.go`: the recover fence wraps exactly the user-code call (codec half), never ferry's own logic.
   One panicking codec -> one typed `errCodecPanic` carrying the address, healthy siblings still load, ordinary refusals aggregate next to it, deferred release still closes-without-Commit.
   A panic outside the fence (ferry's own bug) still crashes - proven by test.

## Round 3 additions (the owner's #20 scenario, and who writes `go`)

8. **The multi-service Load** (`multisvc.go`): 8 keys routed 2/3/3 across three backends (1ms/2ms/3ms).
   Identical destinations proven; round trips: serial 8, core per-address fanout with MaxConcurrency(3) still 8, backend-grouped batches 3 (one per service).
   Wall clock: serial 18.3ms, core fanout 7.1ms, backend batches 3.4ms - the owner's "wall-clock = longest backend", and only the driver can group, because only it knows the routing.
9. **Who writes `go`: core does, gated and bounded** (`capability.go`).
   Core's scheduler fans out per address only when BOTH the caller allowed it (`MaxConcurrency(n)` Option) and the driver's instance asserted the optional capability (`ConcurrentSafe`, the Releaser/Committer idiom).
   Proven: an instance without the capability never sees an overlapped call under `MaxConcurrency(4)` (peak inflight 1); a capable one overlaps within the bound (2..4).
   env/yaml simply never assert it; kv/S3/Consul do.
