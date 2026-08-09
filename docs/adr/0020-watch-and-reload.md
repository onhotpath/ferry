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

> **Amended under [#364](https://github.com/onhotpath/ferry/issues/364): a typed helper ships in the core module as package `ferry/watch`, and the root package still grows nothing.**
>
> As published this section ships no code at all.
> Core ships no `Watch`, no `Notifier` and no watcher, and the decision's deliverables are two documents.
>
> **What moves is the deliverables.**
> `Signal` and `Values` ship, in the core module, in a package of their own:
>
> ```go
> func New() *Signal
> func (s *Signal) Changed(context.Context)
> func Values[T any](ctx context.Context, s *Signal, b *ferry.Binding[T]) (iter.Seq[T], func() error)
> ```
>
> The root package `ferry` still grows no surface, the driver seam is unchanged, and `Notifier` still does not ship.
> `Signal.Changed`'s method value has type `func(context.Context)`, which is exactly what all three drivers' watch Options already take, so nothing on the driver side moves either.
>
> **Why: the thirty lines above and the drivers that shipped do not compose.**
> The blessed loop takes `signal <-chan struct{}`, and no shipped driver publishes a channel - three of them ship a callback, for the reason the amendments below give.
> All three start watching inside their constructor, which is before `ferry.Bind` has returned, so a change can land when there is nothing yet to load through.
> **That ordering hole is one no driver can close**, because the binding is core's and does not exist when the driver's watch opens.
> The shipped `ExampleWatchFiles` is what the hole cost: it nil-checked an atomic pointer on every callback, silently dropped a change that landed before `Bind` returned, and had nowhere to send a failed reload's error.
>
> **[ADR-0002](0002-core-and-sub-modules.md) admits it by route (b), not route (a).**
> `watch/` is a directory of the core module like `ferrytest/`, not a module of its own: it imports the root package and the standard library and nothing else, so core's empty `require` block is untouched.
> What admits it is that the helper is this ADR's own rule - a signal says the plane may have changed, and the reload is `Load` - in executable form, and that the one hole it closes is closable only over core's own `Binding[T]`.
> A third party's version of that loop settles the ordering for nobody, which is route (b)'s argument read where it applies.
>
> **The thirty lines survive in this ADR and nowhere else.**
> `examples/watcher`'s hand-rolled loop is deleted, because a demonstration of code a caller now imports is a second implementation to keep true.

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

> **Amended under [#364](https://github.com/onhotpath/ferry/issues/364): the convention has its first shipped user.**
>
> As published nothing in ferry returned this pair, and the convention was decided on a prototype.
> `watch.Values` now ships it.
> The shape is unchanged, and so is everything decided above about it.

### The YAML sink refuses a commit over a file that changed underneath

A dump reads, stages and swaps.
A watcher makes the window between the read and the swap a window in which somebody else edits the file, and the swap then silently discards their edit.

> An external change to the file between the dump's open and its commit **refuses at `Commit`**, with an actionable error, and the caller re-dumps.

That is optimistic concurrency, and it is the rule this project already applies to delta writes: efficient, but never wrong.
It costs one stat, on the commit path, on a file the driver already has open.

**File watching for YAML is opt-in through a driver Option.**
The fsnotify machinery never runs unless it is asked for, which joins the driver's existing Option set rather than growing core's, and is [ADR-0018](0018-the-spelling-seam.md)'s rule that a plane's facts are declared where the plane is.

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13) as this Option was implemented: its shape is settled, and it is neither fsnotify-backed nor channel-shaped.**
>
> As published this paragraph named the Option and left two things unstated that the implementation could not leave open: what the Option hands the caller, and what it watches with.
>
> **The Option takes a callback, and the callback returns nothing.**
>
> ```go
> func Watch(ctx context.Context, every time.Duration, onChange func(context.Context)) SourceOption
> ```
>
> A channel would have been `Notifier`'s shape without `Notifier`'s name, and this ADR's own reason for not shipping that interface - core owning the semantics of a channel it never reads - applies to a driver publishing one just as squarely.
> The context is the argument because it is what carries the caller's deadline, cancellation and values into the reload, and a reload is `Load(ctx)`.
> There is no error return: the driver has nowhere to put one, since it has no logger and no reporting surface, and a "non-nil stops the watch" reading would smuggle in the lifecycle API this ADR refused.
> Stopping rides the context the driver already has, which is the whole lifecycle: cancel it and the goroutine returns.
>
> **Watching is a stat per interval, not fsnotify.**
> `driver/yaml` depends on `go.yaml.in/yaml/v3` and nothing else, and a watch is not a reason to widen that: the module rules say a driver's dependencies are argued for, and polling is the answer that needs no argument.
> The cost is stated rather than hidden - the caller names the interval, and a rewrite landing in the same modification-time tick that leaves the file's length alone is not seen - and it is the same stamp the commit-time refusal above compares, so the two share one mechanism.
> `Notifier`'s trigger is unchanged by this: a second watchable driver is still what would buy one name across drivers.
>
> **A panic in the callback is not fenced**, which is [ADR-0011](0011-the-error-model.md)'s rule read where it applies rather than extended by analogy.
> That fence exists so one panicking call cannot kill an aggregate that had already collected every other address's answer, on a goroutine whose stack does not name the walk that started it.
> Here there is no aggregate, no error the recovery could be delivered into, and the callback is the top of the watching goroutine's own stack - so the ground the amendment under [#254](https://github.com/onhotpath/ferry/issues/254) said had stopped being true is true again, and the published rule stands: a panic in user code is a bug, and recovering it would leave a process that has silently stopped reloading.
>
> **One sharp edge falls out of the Option starting the watch when the source is built**: the callback that loads through a `Binding` refers to a binding `ferry.Bind` has not returned yet, so the caller has to order the two.
> It is in the Option's godoc, in the guide and in the driver's runnable example, because it is the first thing anyone wiring this up hits.

> **Amended under [#352](https://github.com/onhotpath/ferry/issues/352): `driver/env` watches with fsnotify, and `driver/yaml` still polls.**
>
> As published the amendment above reads as though polling-not-fsnotify were the project's answer.
> It is `driver/yaml`'s answer and it is unchanged there.
> `driver/env` ships `WatchFiles(ctx, onChange)` and it takes the dependency:
>
> ```go
> func WatchFiles(ctx context.Context, onChange func(context.Context)) Option
> ```
>
> **The dependency is argued here because that is what the module rules require, and it is a real cost.**
> `driver/env`'s `require` block was empty, and every consumer of the process-environment source - which touches no filesystem at all - now pulls `github.com/fsnotify/fsnotify` and `golang.org/x/sys`.
> What buys it is that this driver watches a set of files rather than one, that a `.env` file is edited by hand far more often than a rendered config file is, and that the interval a poll would need is the one thing this Option has no honest default for when the caller named three files in three directories.
> `driver/yaml`'s polling watcher is the fallback if the dependency bites, and it is already written.
>
> **Everything else about the shape is `yaml.Watch`'s, deliberately.**
> Callback not channel, no error return, stopping rides the context, no `Stop` method, the watch opened synchronously inside the constructor so a failure has somewhere to go, the callback unfenced, the context read twice, and the same sharp edge about ordering the binding.
> The one difference is the missing `every time.Duration`, because fsnotify has no interval.
> Two watchable drivers disagreeing about the shape would be worse than either shape.
>
> **What fsnotify buys is answered in kind rather than in latency.**
> The directory is watched rather than the file, because an editor and this package's own sink both replace a file by renaming another over it and an inotify watch survives that attached to an inode nobody reads any more.
> Events are filtered by exact name, which is what keeps the sink's own `.ferry-*` staging files out of the callback, and a 50ms settle timer coalesces the burst one save produces.
> Losing the watch fires the callback once and returns, so the next `Load` reports the truth through a surface the caller already handles.
>
> **`Notifier`'s stated trigger is now met, and it is deliberately not taken.**
> The trigger above is "more than one driver can genuinely announce a change and a caller has to write the assertion twice", and there are two such drivers as of this amendment.
> It still does not ship, for the three costs the next section lists, all unchanged - and for a fourth this amendment adds: nothing in the two Options is shared.
> One polls a stat and one reads an inotify queue, and what a common interface would carry between them is a channel neither driver publishes and core never reads.
> The trigger is restated as the one that would actually bite: **a caller writing the same watch wiring against two drivers and wanting one binding for both.**

> **Amended under [#272](https://github.com/onhotpath/ferry/issues/272): there are three watchable drivers, the shape held a third time, and `Notifier` stays declined with the composition question filed.**
>
> As published, and as amended above, this ADR counts two: `driver/yaml`, which polls a stat, and `driver/env`, which reads an fsnotify queue.
> `driver/windows` is the third.
> `winreg.Watch(ctx, onChange)` watches the whole subtree under the driver's key through `RegNotifyChangeKeyValue`, and every sentence above that counts to two is now counting to three.
>
> **The shape constraints are what this amendment exists to record, because a third driver is where a shape either is one or was never one.**
> Every one of them held, checked against the shipped code rather than against the intent:
>
> - a callback and not a channel, `onChange func(context.Context)`;
> - an Option and not a method on the source;
> - no error return from the callback, because there is still nowhere to put one;
> - no `Stop`, and cancelling the context that was passed in is the whole lifecycle;
> - the watch opened synchronously inside the constructor, on the caller's own goroutine, so a failure has somewhere to go;
> - a failure to open refused at `Bind` rather than at the first load, as `ErrWatch` wrapping [`ferry.ErrPlane`](0011-the-error-model.md);
> - the callback unfenced, one call at a time, and told only that the plane may have changed;
> - and no interval, because `RegNotifyChangeKeyValue` has none, which is the same difference `driver/env` has from `driver/yaml` and for the same reason.
>
> Two drivers agreeing could have been one driver copied.
> Three agreeing over three unrelated mechanisms - a poll, an inotify queue and a Win32 notification handle - is the shape being a shape.
>
> **One promise the shape makes is not free on this mechanism, and it is the one thing this driver had to build rather than inherit.**
> "Changes that land while the callback runs are one call afterwards" is true of a poll and of an fsnotify queue because the watch is persistent: it is still armed while the callback runs, and events queue behind it.
> `RegNotifyChangeKeyValue` is armed once and consumed once, so a driver that registered inside its own wait would have no registration for the whole of the callback and the load inside it, and a change landing in that window would fire nothing ever again - a process left holding stale configuration with no signal that it is stale.
> The fix is in the driver's own seam rather than in the shape: registering and waiting are two calls, `Notifier.Arm` answering with a `Change` that is waited on once, and the loop arms the next registration **before** it runs the callback and before it releases the current one.
> The shape's promise then holds on all three, and the seam's own test double models the same thing rather than modelling something stronger.
>
> **The registration is placed inside the constructor**, which is what makes "the watch is opened on the caller's goroutine so a failure has somewhere to go" a fact about this driver rather than only about its type assertion: a registration that cannot be placed is `ErrWatch` at `Bind`.
> A key that does not exist yet is deliberately not that failure.
> The registration goes on the nearest existing key above it and watches that subtree, so the bootstrap case - a process watching the key its own first dump will create - fires when the dump creates it and moves down to the key itself on the next turn.
> Refusing at `Bind` instead was the honest alternative and was rejected: a configuration key that does not exist yet is ordinary, and the refusal would land on the whole load rather than on the watch.
>
> **`Notifier` stays declined, and this amendment states the position rather than leaving a third watcher to reopen it silently.**
> Its trigger was met at two and deliberately not taken; a third does not change any of the four costs recorded above, and the fourth of them gets stronger rather than weaker, because there is now a third mechanism with nothing in common with the other two.
> What a common interface would carry between a stat, an inotify queue and a registry notification is still a channel no driver publishes and core never reads.
>
> **What a third watcher does change is the question the restated trigger names**, and that question is now filed rather than restated a third time: [#361](https://github.com/onhotpath/ferry/issues/361), two watchable sources composed into one load, detect it, coordinate it or document it.
> With three drivers announcing changes, a caller wiring two of them under one binding is an ordinary configuration rather than a hypothetical, and what happens when both fire is undecided here.
> This ADR does not decide it, and #361 is where it gets decided.
>
> **Two sentences elsewhere in this document read as counts and are now wrong.**
> The bullet under [What this ADR does not decide](#what-this-adr-does-not-decide) says `driver/env` "is a second one"; there is a third, and it is opt-in like both of the others.
> The last consequence says "until a second watchable driver exists", which stopped being the live condition under [#352](https://github.com/onhotpath/ferry/issues/352) and is superseded here: the condition is now #361's answer, not a count.

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

> **Amended under [#364](https://github.com/onhotpath/ferry/issues/364): the shipped helper is not `Notifier` and does not move its trigger.**
>
> As published this section is the only place a change signal is named at all, so a package called `watch` shipping in the core module reads as `Notifier` arriving under a different name.
> It is not.
> **The helper adds no capability to the assertion set**, no driver publishes a channel, and core still reads no channel it did not create: a `Signal` is a channel the caller made and handed to the driver as a callback, which is the opposite direction to the interface written out above.
> Nothing here is option-dependent either, because there is no interface for a source to assert.
>
> **The restated trigger is what the helper serves, on the caller's side of the seam.**
> The [#352](https://github.com/onhotpath/ferry/issues/352) amendment restated it as a caller writing the same watch wiring against two drivers and wanting one binding for both, and that is exactly the caller `watch.Values` is for.
> It is also why the driver-side interface still buys nothing: the wiring was the cost, and it is now paid once in a package rather than once per driver in an interface neither driver's internals share anything behind.
>
> **The helper deliberately removes back pressure, which a hand-written callback has.**
> `Changed` records a pending change and returns immediately, so a slow consumer no longer delays the driver's next look, where a callback that loads inline holds the watching goroutine for the length of the reload.
> What is paid for it is coalescing: one slot, so a burst is one change and so is a change that lands while a reload is already running.

### What this ADR does not decide

- **Whether any first-party driver ever announces changes.** The YAML Option above is the only one on the table and it is opt-in. *(Amended under [#352](https://github.com/onhotpath/ferry/issues/352): `driver/env` ships `WatchFiles`, which is a second one and is also opt-in.)*
- **Whether `Notifier` or `ferry.Watch` ever ship.** Recorded with a named trigger, and refusing is the reversible direction.
- **Drift detection.** [ADR-0001](0001-what-ferry-supports.md) calls drift and watch the pull and push forms of one concern; the plane-inspection half is still Milestoned on its own terms, and this ADR does not move it.
- **What a watcher does with a partial failure mid-stream.** The error ends the stream, and a caller who wants to continue rebuilds the iterator, which is a caller's policy rather than ferry's.
- **Concurrency inside one reload**: [ADR-0019](0019-the-concurrency-model.md)'s.

> **Amended under [#364](https://github.com/onhotpath/ferry/issues/364): a helper package ships, and these rows stand.**
>
> As published this list is read against a decision that shipped no code, so which rows `ferry/watch` touches is worth saying out loud.
>
> **`ferry.Watch` in the root package still does not ship**, and the row above is unchanged.
> `watch.Values` is a function in a package of the core module, reached as `watch.Values`, and the root package's surface is the same as it was.
>
> **The partial-failure row is unchanged, and `watch.Values` implements exactly the policy it states.**
> The error ends the stream, and a caller who wants to continue rebuilds the iterator by calling `Values` again on the same `Signal`.
> Nothing is lost in between: a change that lands while no stream is ranging is pending when the next one opens.
> A `Follow`-style convenience wrapping that rebuild loop was considered and deliberately not shipped, because what it would wrap is a retry policy and this row says the policy is the caller's.
> It is a candidate later addition if real callers turn out to write the same loop.

## Consequences

- **Watch moves from Milestoned to Enabled and core grows no surface**, so [ADR-0001](0001-what-ferry-supports.md)'s capability table gains its first row to change bucket.
  The proof is a watcher built against the real shipped module through a nested workspace rather than against a sketch, which is the only form of evidence this claim can take.
- **The commitment ADR-0001 made was honoured**: the mechanism landed, in core, piece by piece, and the feature ships outside, which is what milestoning promised and what Enabled means.
- **The deliverables are two documents rather than an API**: `docs/guide/watch-reload.md` and a runnable `examples/watcher`.
  A capability argued as buildable is worthless undemonstrated.

  > **Amended under [#364](https://github.com/onhotpath/ferry/issues/364): the deliverables now include an API.**
  >
  > As published this consequence is that nothing ships but prose.
  > Package `ferry/watch` ships, in the core module, and the guide is written against it rather than against a listing a reader copies.
  > **Why is in the amendment to the first decision above**, and the part that belongs here is what it costs: a package in core to keep true, in exchange for the ordering hole no driver could close and a demonstration nobody has to copy.
- **A reload is `Load` and no alias ships**, so there is one spelling of one operation.
  `LoadOver`'s two traps - a lost address keeping its stale value, and a composite replaced wholesale - are published in its godoc and in the guide, because the shape a reader reaches for first is the wrong one.
- **ferry now has a convention for a fallible iterator**, `(iter.Seq[T], func() error)`, decided on deliberate-versus-accidental discard rather than on compiler enforcement, which the first draft claimed and which does not hold.
  The cost is a loop that reads worse than `Seq2`'s, paid once per API and stated plainly.
- **The YAML sink refuses a commit over an externally changed file**, which costs one stat on the commit path and turns a silent lost edit into an actionable refusal.
  File watching is opt-in through a driver Option, so nothing runs for a caller who did not ask.
- **`Notifier` is specified, not shipped, with a named trigger.**
  Until a second watchable driver exists, one name across drivers buys nothing and costs a capability in a set that is already crowded.

Evidence: `prototype/watcher` on [`proto/05-watch-grammar`](https://github.com/onhotpath/ferry/tree/proto/05-watch-grammar), four tests under `-race` against the real module: `TestWatcherBuildsOutsideCore`, `TestErrorConventions`, `TestHeldValueIsImmutable` and `TestLoadOverAsReloadSharpEdges`.
