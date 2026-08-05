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
