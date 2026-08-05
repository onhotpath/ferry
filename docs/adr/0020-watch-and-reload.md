# 20. Watch and reload, answered as Enabled

Status: Accepted
Date: 2026-08-06
Ticket: [#13](https://github.com/onhotpath/ferry/issues/13)

## Context

[ADR-0001](0001-what-ferry-supports.md) put watch and reload in the **Milestoned** bucket, with the note "machinery lands in core when it lands", and was explicit about what milestoning commits to: the mechanism, never the feature.
It is the only capability in that bucket besides plane inspection and delta dump.

Every ADR since has fed it a piece of that machinery, and none of them decided the question.

- [ADR-0004](0004-source-and-sink.md) split `Bind` from open, and named reload as one of the two things the split buys: one bind, three opens, three different values.
- [ADR-0006](0006-defaults-and-zero-values.md) measured that an in-place reload leaks the previous load's value for any address the plane has since lost, and ruled that the resolution is a reload producing a new value.
- [ADR-0010](0010-the-entry-point-and-the-schema-cache.md) made `Load` return a value.
- [ADR-0012](0012-the-caller-held-binding.md) shipped `Binding[T]`, which is the bind-once/open-many shape, and recorded that the key-helper amendment is owed to this ticket whether or not a caller-facing binding ever existed.
- [ADR-0011](0011-the-error-model.md) built the error model and left the convention for an error accompanying an iterator undecided, because nothing in ferry returned one.

So the question is no longer what machinery watch needs.
It is whether any of it is missing.

This ADR is written from the prototype on branch [`proto/05-watch-grammar`](https://github.com/onhotpath/ferry/tree/proto/05-watch-grammar), module `prototype/watcher`, which runs against the **real shipped ferry module** through its own nested `go.work` with no `replace` anywhere and the root workspace untouched.
That matters more than usual here: the claim is that nothing is missing, and the only honest way to test that claim is to build the thing outside core against what core actually exports.
Four tests, green under `-race`.

## Decision

### Watch moves from Milestoned to Enabled, and core grows no surface

> A watcher builds entirely outside core, over `Binding[T]` and a change signal the driver owns.
> ferry ships no `Watch`, no `Notifier` and no watcher.

The whole thing is about thirty lines:

```go
func Watch[T any](ctx context.Context, b *ferry.Binding[T], signal <-chan struct{}) (iter.Seq[T], func() error) {
    var streamErr error
    seq := func(yield func(T) bool) {
        for {
            select {
            case <-ctx.Done():
                streamErr = ctx.Err()
                return
            case _, ok := <-signal:
                if !ok {
                    return                    // the plane closed its signal: a clean end
                }
            }
            v, err := b.Load(ctx)             // a reload IS Load
            if err != nil {
                streamErr = err
                return
            }
            if !yield(v) {
                return
            }
        }
    }
    return seq, func() error { return streamErr }
}
```

That is [ADR-0001](0001-what-ferry-supports.md)'s own split arriving on schedule: the machinery was sessions one through four's work and it is done, so the feature lands outside, which is where **Enabled is the default landing place** already said it should.

**A milestoned entry cannot rot into a broken promise, and this one did not.**
ADR-0001 committed to the mechanism and never to the feature, and the mechanism is `Bind` plus open-many plus `Load` returning a value plus per-open minted sets.
Every one of those shipped for its own reasons.

**What is required is documentation rather than code**: `docs/guide/watch-reload.md` and a runnable `examples/watcher`, which are this decision's deliverables, because a capability whose whole argument is "you can build it" is worth nothing if nobody is shown the thirty lines.

### A reload is `Load`, and there is no second verb

> `Load` produces a new value.
> That is the reload.
> No `Reload` alias ships.

ADR-0006 already argued this from the leak and ADR-0010 already implemented the half that matters.
What this ADR adds is the refusal to name it twice: an alias would be a second spelling of one operation, which is survey item 5.14's first entry at ferry's own front door, and [ADR-0012](0012-the-caller-held-binding.md) took the same line about the verbs and the binding.

**`LoadOver` is not the watcher's tool, and its two traps are published** rather than left for a reader to find.
Both reproduced against the real module, on a plane that loses `/host` and `/tags/b` between two loads:

```
prev  = {Host: "db1", Tags: {a:1, b:2}}

b.LoadOver(ctx, prev)  ->  {Host: "db1",  Tags: {a:1}}
b.Load(ctx)            ->  {Host: "",     Tags: {a:1}}
```

The first row leaks: `/host` is gone from the plane and the value still says `db1`.
The second row shows the composite replaced wholesale rather than merged, which is [ADR-0006](0006-defaults-and-zero-values.md)'s replace rule and is surprising in a loop that runs on every file change.

`LoadOver` carries a previous value forward per field for structs and all-or-nothing for slices and maps, and an address the plane lost keeps the seed's value.
That is exactly what a seed is for and exactly what a reload is not.
Both traps are now in its godoc and in the guide, and ADR-0006 carries them as an amendment where the rule they violate lives.

**A held value never mutates when the plane changes**, asserted through the shipped surface, which is the property the whole design rests on and which the prototype exists to check rather than assume.

### The delivery shape is `(iter.Seq[T], func() error)`, and it settles a convention ferry did not have

> An iterator that can fail returns the sequence and an error function, and the error is read after the range.

Both candidate shapes were implemented and exercised on the same failure.

```go
seq, errf := watcher.Watch(ctx, b, signal)
for v := range seq {
    publish(v)
}
if err := errf(); err != nil {
    alert(err)
}
```

against `iter.Seq2[T, error]`:

```go
for v, err := range watcher.Watch2(ctx, b, signal) {
    if err != nil { alert(err); break }
    publish(v)
}
```

The `Seq2` form reads better and that is not in dispute.

**The deciding property is narrower than the first draft of this decision claimed, and the correction is recorded.**
The first argument was that the error cannot be silently lost because an unused `errf` is a compiler error.
It is not: `seq, _ := watcher.Watch(...)` compiles, exactly as `for v, _ := range` does.

What survives is the difference between a **deliberate** discard and an **accidental** one.
Dropping the second range variable is the ordinary shape of ranging over anything, so `for v, _ := range` reads as normal code and is one character from correct.
Writing `seq, _ :=` is a statement about an error the author had to look at.
That is a smaller claim than the first one and it is the true one.

`Seq2` also carries a documentation burden the standard library deliberately has no convention for: whether the error is final, what the value beside it is, whether ranging may continue past it, and what a `break` leaves pending.
Four questions that must be answered per API against one shape that answers them by construction.

**This answers a question [ADR-0011](0011-the-error-model.md) left open** rather than opening one, and it is recorded there.

### The YAML sink refuses a commit over a file that changed underneath

A dump reads, stages and swaps.
A watcher makes the window between the read and the swap a window in which somebody else edits the file, and the swap then silently discards their edit.

> An external change to the file between the dump's open and its commit **refuses at `Commit`**, with an actionable error, and the caller re-dumps.

That is optimistic concurrency, and it is the rule this project already applies to delta writes: efficient, but never wrong.
It costs one stat, on the commit path, on a file the driver already has open.

**File watching for YAML is opt-in through a driver Option.**
The fsnotify machinery never runs unless it is asked for, which joins the driver's existing Option set rather than growing core's, and is [ADR-0018](0018-the-spelling-seam.md)'s rule that a plane's facts are declared where the plane is.

### `Notifier` is recorded as the upgrade path, and does not ship

The one thing a watcher needs that is not a ferry concept is the change signal, and today it is the driver's own: a channel, an fsnotify loop, a Consul watch plan.
A capability would give that one name across drivers.

```go
// Notify mints an independent signal per call. The channel receives after the plane
// may have changed - coalescing is legal, false positives are legal, payloads are not -
// and closes when watching ends. The error refuses a plane that cannot be watched here.
type Notifier interface {
    Notify(ctx context.Context) (<-chan struct{}, error)
}
```

It is written out in full because a recorded upgrade path that is not specified is not a path.

**It does not ship, for three reasons stated as costs rather than as objections.**
It adds a capability to an assertion set that is already the thing [#201](https://github.com/onhotpath/ferry/issues/201) says stops scaling.
It is option-dependent, so a `yaml.Source` asserts `Notifier` whether or not watching was enabled and the refusal moves to call time, which is the one shape ferry's refusal ladder keeps trying to avoid.
And core would own the semantics of a channel it never reads.

**The trigger is named**: it ships when more than one driver can genuinely announce a change and a caller has to write the assertion twice.
That is the rule of three applied one door down, and it is the same trigger [ADR-0018](0018-the-spelling-seam.md) sets for consolidating spellings.
`ferry.Watch` in core sits behind that, not beside it.

### What this ADR does not decide

- **Whether any first-party driver ever announces changes.** The YAML Option above is the only one on the table and it is opt-in.
- **Whether `Notifier` or `ferry.Watch` ever ship.** Recorded with a named trigger, and refusing is the reversible direction.
- **Drift detection.** [ADR-0001](0001-what-ferry-supports.md) calls drift and watch the pull and push forms of one concern; the plane-inspection half is still Milestoned on its own terms, and this ADR does not move it.
- **What a watcher does with a partial failure mid-stream.** The error ends the stream, and a caller who wants to continue rebuilds the iterator, which is a caller's policy rather than ferry's.
- **Concurrency inside one reload**: [ADR-0019](0019-the-concurrency-model.md)'s.

## Consequences

- **Watch moves from Milestoned to Enabled and core grows no surface**, so [ADR-0001](0001-what-ferry-supports.md)'s capability table gains its first row to change bucket.
  The proof is a watcher built against the real shipped module through a nested workspace rather than against a sketch, which is the only form of evidence this claim can take.
- **The commitment ADR-0001 made was honoured**: the mechanism landed, in core, piece by piece, and the feature ships outside, which is what milestoning promised and what Enabled means.
- **The deliverables are two documents rather than an API**: `docs/guide/watch-reload.md` and a runnable `examples/watcher`.
  A capability argued as buildable is worthless undemonstrated.
- **A reload is `Load` and no alias ships**, so there is one spelling of one operation.
  `LoadOver`'s two traps - a lost address keeping its stale value, and a composite replaced wholesale - are published in its godoc and in the guide, because the shape a reader reaches for first is the wrong one.
- **ferry now has a convention for a fallible iterator**, `(iter.Seq[T], func() error)`, decided on deliberate-versus-accidental discard rather than on compiler enforcement, which the first draft claimed and which does not hold.
  The cost is a loop that reads worse than `Seq2`'s, paid once per API and stated plainly.
- **The YAML sink refuses a commit over an externally changed file**, which costs one stat on the commit path and turns a silent lost edit into an actionable refusal.
  File watching is opt-in through a driver Option, so nothing runs for a caller who did not ask.
- **`Notifier` is specified, not shipped, with a named trigger.**
  Until a second watchable driver exists, one name across drivers buys nothing and costs a capability in a set that is already crowded.

Evidence: `prototype/watcher` on [`proto/05-watch-grammar`](https://github.com/onhotpath/ferry/tree/proto/05-watch-grammar), four tests under `-race` against the real module: `TestWatcherBuildsOutsideCore`, `TestErrorConventions`, `TestHeldValueIsImmutable` and `TestLoadOverAsReloadSharpEdges`.
