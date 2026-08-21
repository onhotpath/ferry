# 20. Watch and reload, answered as Enabled

Status: Accepted
Date: 2026-08-06
Ticket: [#13](https://github.com/onhotpath/ferry/issues/13)

## The six principles this record is judged against

> **Added under [#373](https://github.com/onhotpath/ferry/issues/373): the rules every decision below is an application of, three of which were never written down.**
>
> As published this record opens with its Context and states its principles nowhere.
> They were real all along, and the three that went unwritten are the three the shipped design broke.
> They are stated first because everything after this is one of them applied, and because the next change in this area has to argue against them rather than around them.

1. **A plane fires events opt-in.**
   A source that was not asked to watch touches nothing and starts nothing.
2. **The consumer uses a watchable plane neatly.**
   One context, no goroutine the caller starts, nothing to stop, and a stream that opens with a load.
3. **Refusal lands at Bind, and at compile time where that is free.**
4. **The stream ends observably, with a reason.**
5. **An event is a hint carrying nothing, and the reload is `Load`.**
6. **Composition is part of neatly.**
   Two watchable planes under one binding is an ordinary configuration, not a hypothetical.

The first three are the ones this record already followed.
The last three are added here, and each of them is a defect in the shipped design named as a rule: a watch that died silently, an announcement that tried to carry a payload, and a composition nobody could write.

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

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): the change signal crosses the seam as a typed interface the source publishes, and the callback Option is retired in all three drivers.**
>
> As published, and as amended under [#352](https://github.com/onhotpath/ferry/issues/352) and [#272](https://github.com/onhotpath/ferry/issues/272), the shape is a caller-supplied callback handed to a driver Option at construction: `onChange func(context.Context)`, an Option and not a method, no error return, no `Stop`, the watch opened inside the constructor.
> Three drivers agreed on it and this record took that agreement as the shape being a shape.
>
> **What moves is the direction the signal travels and what it is allowed to say.**
>
> ```go
> type Notifier interface {
> 	Notify(ctx context.Context) (Change, error)
> }
>
> type Change interface {
> 	Wait(ctx context.Context) (bool, error)
> 	Close() error
> }
>
> type WatchableSource interface {
> 	Source
> 	Watching() (Notifier, error)
> }
>
> func BindWatched[T any](src WatchableSource, opts ...Option) (*WatchedBinding[T], error)
>
> func (wb *WatchedBinding[T]) Load(ctx context.Context) (T, error)
> func (wb *WatchedBinding[T]) Watch(ctx context.Context) (seq iter.Seq[T], errf func() error)
>
> var ErrWatchLost = errors.New("the watch was lost")
> ```
>
> Eight exports.
> The driver side is one method on the source that already holds the configuration, spelled the same way in every driver and taking no arguments anywhere:
>
> ```go
> func (s *Source) Watched() *WatchedSource
> ```
>
> **Principle 2 is what the callback could not do.**
> A callback is handed to a constructor, so the watch starts before `Bind` has returned the binding the callback wants to load through, and the shipped `ExampleWatchFiles` paid for that with an atomic pointer nil-checked on every call.
> This record's own [#364](https://github.com/onhotpath/ferry/issues/364) amendment named that hole and closed it with a `Signal` the caller carries between two objects.
> The typed seam removes the hole rather than absorbing it: nothing watches until a stream opens, and the stream opens with a load, so a change that landed before the bind is in the value the range yields first.
> There is one context, the stream's, where there were two that nothing reconciled.
>
> **Principle 4 is what the callback could not say.**
> `func(context.Context)` has no error return, so a driver that lost its watch had nowhere to report it; this record settled that by calling the callback once and returning, and by trusting the next load to report something.
> It does not: a lost watch means no next load ever happens, so a process holding stale configuration has nothing to tell it so.
> `Change.Wait` answers `(bool, error)`, so a lost watch ends the stream with the driver's own reason under `ErrWatchLost`, and a quiet ending is `false` with a nil error.
>
> **Principle 3 is what a caller-supplied callback cannot be.**
> `WatchableSource` is an ordinary interface and a plain `Source` does not satisfy it, so handing an unwatchable source to `BindWatched` is a compile error with no conversion available.
> What a type cannot carry is whether *this* source has anything to watch, because watchability is option-dependent: an `env.Source` is the same type whatever `DotEnv` said.
> `Watching() (Notifier, error)` is where that half is answered, once, at the bind, before any load.
>
> **Principle 5 is unchanged and is now enforced.**
> `Change.Wait` reports that the plane may have changed and carries nothing else, exactly as the callback did, and the reload is still `Load`.
> There is no `Reload`, and no payload anywhere on the seam.
>
> **Principle 1 is unchanged.**
> The constructors are untouched and still start nothing.
> `Watched()` converts, and the conversion touches nothing either: the mechanism opens when a stream does.
>
> **The announcement seam is arm-once, and the arm-before-reload invariant is core's.**
> `Notify` places a registration that is live on return and `Wait` consumes it once, which is the weakest of the three real mechanisms: a persistent queue models an armed-once registration and the reverse is false, so one core loop is correct against a poll, an fsnotify queue and a Win32 notification handle.
> Core places the next registration **before** it runs the reload, so a change landing mid-reload is the next change rather than a lost one, and no driver re-derives the subtle part.
> `Change.Close` is required rather than asserted, because it is a resource obligation core invokes at the re-arm moment and optionality would turn a forgotten release into a silent leak.
> Each `Watch` call arms its own registration, so two concurrent consumers are well defined and each sees every change.
>
> **`Watching` does no I/O.**
> It refuses what is knowable without the operating system; everything the OS has an opinion about surfaces when the first registration is placed, on the stream, before any value reaches the caller.
>
> **Why a conversion method and not a second constructor.**
> A second constructor - `env.NewWatched(opts...)` - was built and measured, and it duplicates the whole option list per driver.
> The conversion takes only what watching needs and nothing the source already has, which is [ADR-0017](0017-the-registration-api-and-the-value-it-builds.md)'s `KeyCodec.AsMapKey` precedent read on the source seam: a narrowing that says one more thing about a value already built.
> It also deletes the mistake the old shape's sharp edge was about, where the file was named in one place and the watcher set up in another and forgetting the second was silent.
> `env.New(env.DotEnv(".env")).Watched()` names both in one expression.
>
> **Why an interface and not a sealed struct core mints.**
> A sealed `WatchableSource` struct with `ferry.Watchable(src, n)` and `ferry.Unwatchable(err)` constructors was built and measured.
> It is two more exports, it never sealed anything - both constructors are exported, so anyone can mint one - and it made the one misuse it appeared to guard *easier*, because `Watchable(srcA, notifierB)` pairs a source with an unrelated mechanism in one line at the call site.
> Under the interface the same mistake needs a type whose own two methods contradict each other.
> The one misuse neither shape detects is a mechanism that binds and never fires, and it is recorded rather than argued about.
>
> **What the caller writes now, against the nine steps this record's own example needed:**
>
> ```go
> wb, err := ferry.BindWatched[Config](env.New(env.DotEnv(".env")).Watched())
> seq, errf := wb.Watch(ctx)
> for cfg := range seq { publish(cfg) }
> if err := errf(); err != nil { alert(err) }
> ```

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): "core grows no surface" is retired, and the claim is withdrawn.**
>
> As published this section's heading says core grows no surface, and its text says in as many words that ferry ships no `Watch`, no `Notifier` and no watcher.
> The #364 amendment kept the claim by putting the helper in a package of the core module rather than in the root package, and said so explicitly: "the root package `ferry` still grows no surface".
>
> **That distinction is retired.**
> `ferry/watch` is deleted and eight exports land in the root package.
> The heading stands as published because a heading is a record of what was decided, and what it decided is now wrong.
>
> The claim was true of the machinery and false of the feature, and holding it cost the two defects the amendment above names.
> What survives from the published argument is everything about *why* the machinery could land piecemeal: `Bind` split from open, `Load` returning a value, per-open minted sets, `Binding[T]`.
> Every one of those still carries the watch, and the seam added here is the one thing none of them could be: a place for the driver to say the plane changed, and a place for it to say the watch is over.
>
> **What [ADR-0001](0001-what-ferry-supports.md)'s Enabled bucket meant is unchanged.**
> Enabled is still the default landing place for a feature, and this is the case where the feature could not land outside because the hole it closes is closable only over core's own `Binding[T]` and only in the loop that owns the registration.

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): the watch's error model, and why exactly one sentinel is core's.**
>
> As published this record has no error model for the watch, because the callback had no error return and nothing could fail observably.
> Three failures exist and they land in three places.
>
> - **A driver instance that cannot watch** - `env` with no `DotEnv`, `yaml` over no path, a registry that reports no change of its own - is `Watching`'s error, wrapped by core in `ErrPlane`, with the driver's own sentinel reachable underneath: `env.ErrWatch`, `yaml.ErrWatch`, `winreg.ErrWatch`.
>   There is no core sentinel for it.
>   At a bind the caller knows which driver they constructed, so per-driver matching is the honest shape, and a cross-driver "was it specifically the watch that was refused" match has no consumer anybody has named.
>   `driver/yaml` gains a sentinel it never had, which closes a loose end its poll left open: a poll had no failure mode at all to report.
> - **A source that answers `Watching` with no mechanism and no reason** is a driver contract violation rather than a plane refusal, so it is `ErrDriver`, the same bucket as an `OpenFunc` that comes back nil.
>   A nil `WatchableSource` refuses at the bind under `ErrPlane` with a sentence.
> - **A watch the mechanism could not keep** is `ErrWatchLost`, minted by core's own loop, wrapping the driver's reason, ending the stream.
>
> **`ErrWatchLost` is the one sentinel that pays for itself**, and the test is who matches it.
> Its consumer is the caller's restart policy, which is written once against any driver, so a per-driver spelling would make that policy a switch over drivers.
> It is also the only way to tell the three-way split apart at the point it matters: a failed reload carries ferry's own load sentinels, a lost watch carries this, and a cancelled context carries `ctx.Err`.
> There is no leaner spelling that keeps those three distinguishable.
> [ADR-0011](0011-the-error-model.md) carries it in its vocabulary, and the two driver `ErrWatch` sentinels are not consolidated into it.

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): a watchable driver is proved by a conformance harness, and admitting it is [ADR-0014](0014-what-ferrytest-exports.md)'s decision.**
>
> As published this record has no conformance story for watching, because there was no seam to conform to.
> [ADR-0001](0001-what-ferry-supports.md)'s rule is that core's leverage over what it does not ship is a conformance harness, and the watch seam is exactly that shape: core owns the loop and the driver owns the mechanism.
>
> The proposal is two exports, `ferrytest.WatchPlane` and `ferrytest.Watchable`, asserting seven properties through `BindWatched` and the stream and through nothing else: the stream opens with a load, a change reloads, a burst is one reload, a held value never moves, cancelling ends it cleanly, a lost watch ends it with a reason, and a source that cannot be watched is refused at the bind.
> Two of them are optional and are skipped where the plane declares the driver cannot reach that state.
>
> **It is not part of `ferrytest` until ADR-0014 says so**, and that amendment is where the surface list moves or does not move.
> This record decides only that the seam is worth proving identically for every driver rather than once per driver.

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

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): the shipped user is `WatchedBinding.Watch`.**
>
> As amended above the convention's first shipped user is `watch.Values`.
> That function is deleted, so the sentence names something that no longer exists.
> The shipped user is `WatchedBinding.Watch`, in the root package, returning the same pair for the same reason.
> **The convention itself does not move**, and neither does anything decided above about it.

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

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): the three callback Options are deleted, and this is the list of what goes with them.**
>
> As published, and through the three amendments above, the watch is a driver Option in each of `driver/yaml`, `driver/env` and `driver/windows/winreg`, and the shape constraints listed under #272 are the record of those three agreeing.
> The typed seam replaces all three, so each driver's Option and the loop behind it go, and the agreement recorded above is superseded by an interface rather than by a fourth restatement of a callback.
>
> **What is deleted:**
>
> - **`ferry/watch`**, the whole package: `Signal`, `Signal.Changed`, `Values`.
>   Its ordering hole is closed structurally, and its coalescing moves into the drivers that know what a burst is.
> - **`env.WatchFiles(ctx, onChange)`**, **`yaml.Watch(ctx, every, onChange)`** and **`winreg.Watch(ctx, onChange)`**, with the watcher loops behind them.
> - **`driver/yaml`'s poll**: the stamp comparison loop, the default interval, and the interval argument.
> - **`examples/watcher`**, and the two driver examples that demonstrated the nine-step wiring.
>   Each watchable driver ships one runnable `Watched()` example in their place.
>
> **What is not deleted.**
> The YAML sink's commit-time refusal decided at the top of this section is untouched, and it must be said here because both mechanisms compared a `stamp` and only one of them is going: the sink still stats the file between its open and its commit, and still refuses a commit over a file that changed underneath.
> Both driver `ErrWatch` sentinels survive and are not consolidated, for the reason the error-model amendment gives.
> The three shape constraints that were never about the callback survive as properties of the seam: refusal at Bind, no `Stop`, and no interval anywhere.
>
> **What the winreg driver contributed is now core's.**
> The arm-once seam #272 records this driver building for itself, `Notifier.Arm` answering with a `Change` waited on once, is the shape core adopted; the driver's own seam keeps its name and its `Registry` contract, and the watched type adapts it.
> The bootstrap case decided under #272 - a registration placed on the nearest existing key above one that does not exist yet - is unchanged and is a property of that driver, not of the seam.

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): `driver/yaml` takes the fsnotify dependency, and "polling is the answer that needs no argument" is superseded by an owner ruling.**
>
> As published under #13 and restated under #352, this section says: "`driver/yaml` depends on `go.yaml.in/yaml/v3` and nothing else, and a watch is not a reason to widen that: the module rules say a driver's dependencies are argued for, and polling is the answer that needs no argument."
> The #352 amendment adds that "`driver/yaml`'s polling watcher is the fallback if the dependency bites, and it is already written."
>
> **Both paragraphs are superseded.**
> `driver/yaml` requires `github.com/fsnotify/fsnotify`, with `golang.org/x/sys` indirect, and watches the way `driver/env` does.
>
> **The relaxation is the owner's, and it is recorded as a ruling rather than derived from the rule.**
> The module rule that a driver's dependencies are argued for is not repealed; this is the argument, and the owner made the call that mechanism uniformity across the three watchable drivers outweighs the second dependency in this module.
>
> **What it buys, stated as three things rather than as latency.**
>
> - **A refusal this driver could not make.**
>   A poll over a directory that is not there is steady rather than wrong, so there was nothing to refuse and `driver/yaml` had no watch failure mode at all.
>   An inotify watch on the same directory fails, so `yaml.ErrWatch` exists and lands at the stream's first registration with the caller told why.
> - **A sharp edge deleted.**
>   "A rewrite landing in the same modification-time tick that leaves the file's length alone is not seen" was the cost of a stat, published in the Option's godoc and in the guide.
>   It dies with the poll.
> - **One mechanism across three drivers.**
>   `Watched()` takes no argument anywhere, so there is no interval to name in one driver and not in the other two, and the conformance harness asks all three the same questions.
>
> **What it costs, stated plainly.**
> Every consumer of `driver/yaml` now pulls fsnotify and `golang.org/x/sys`, including one that never watches anything.
> That is the same cost `driver/env` pays under #352, paid for the same reason and bounded the same way: this is a driver module, core's `require` block is untouched, and a program that imports neither driver sees neither dependency.
>
> **Duplication, measured, and deliberately not shared.**
> `driver/env` and `driver/yaml` now hold the same mechanism: watch the containing directory rather than the file, filter events by exact name, drop `Chmod`, drain the errors channel, and swallow the burst in a settle window of the driver's own.
> That is roughly 90 lines each, of which about 60 are genuinely the same, and the two differ only in that `env` watches a set of files across several directories and `yaml` watches one.
> They are separate modules, so sharing means a third module in the graph of every consumer of either, which is a worse trade than 60 duplicated lines.
> **Trigger for a shared seam: a third file-watching driver, or a change to the mechanism that has to be made twice and is got wrong once.**

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

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): `Notifier` ships, and the decline is answered by never asserting it.**
>
> As published this section writes `Notifier` out in full and declines it for three costs; the #352 amendment adds a fourth, and the #272 amendment restates the position with three watchable drivers and declines it again.
> It now ships, in a shape none of the four costs applies to, and the sketch above is superseded by the seam in the first decision: `Notify(ctx) (Change, error)` rather than `Notify(ctx) (<-chan struct{}, error)`.
>
> The three published costs, answered one at a time:
>
> - **"It adds a capability to an assertion set that is already the thing [#201](https://github.com/onhotpath/ferry/issues/201) says stops scaling."**
>   It does not, because nothing asserts it.
>   `Notifier` is handed to core by `WatchableSource.Watching`, so it is a value that crosses the seam and never a type ferry probes for.
>   The capability ladder - `Prober`, `Enumerator`, `Releaser` and the rest, all discovered by assertion - is exactly as long as it was.
> - **"It is option-dependent, so a `yaml.Source` asserts `Notifier` whether or not watching was enabled and the refusal moves to call time."**
>   This is the real objection, and it is why the seam has two halves.
>   Watchability by type is `WatchableSource`, decided at compile time; watchability by configuration is `Watching`'s error, decided at `BindWatched`.
>   Neither is a call-time refusal.
> - **"Core would own the semantics of a channel it never reads."**
>   There is no channel.
>   The shipped shape has an explicit wait and an explicit release, so core reads what it asked for and the driver owns when it answers.
>
> **The fourth cost is the one this shape turns into the argument for shipping.**
> "Nothing in the two Options is shared" was true of their internals and false of their contract: register, wait once, release, and say whether that was a change, an ending or a loss.
> `driver/windows/winreg` had already written that contract for itself, for the reason the #272 amendment gives at length, and promoting the weakest of the three mechanisms is what makes one loop correct for all three.
>
> **The restated trigger is met and taken.**
> The #352 amendment restated it as "a caller writing the same watch wiring against two drivers and wanting one binding for both".
> That caller now writes the wiring once, in core, and what remains of the two-binding half is answered below.

### What this ADR does not decide

- **Whether any first-party driver ever announces changes.** The YAML Option above is the only one on the table and it is opt-in. *(Amended under [#352](https://github.com/onhotpath/ferry/issues/352): `driver/env` ships `WatchFiles`, which is a second one and is also opt-in.)* *(Amended under [#13](https://github.com/onhotpath/ferry/issues/13): all three first-party drivers announce changes, through `Watched()` rather than through an Option, and all three are still opt-in. The count is retired, here and in the last consequence: the condition this row was ever about is the seam, not a number.)*
- **Whether `Notifier` or `ferry.Watch` ever ship.** Recorded with a named trigger, and refusing is the reversible direction. *(Amended under [#13](https://github.com/onhotpath/ferry/issues/13): both ship, as `Notifier` and `BindWatched`, and this row is decided rather than open.)*
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

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): what was built, measured and left out, and the trigger for each.**
>
> As published this list is the record of what was not decided.
> The four things below were decided and are not shipping, which is a different thing and belongs beside it: each was built and exercised, and each carries a named trigger rather than a maybe.
> None of them is planned work, and none of them is a ticket.
>
> - **The watch fan-in, composing several watchable sources under one binding.**
>   It composes the watch and never the read, because ferry has no layering helper and this is not the place to invent one.
>   **Trigger: a real cross-driver composite**, which is also where the operator-kick-plus-driver-watch combination lands.
> - **A binding accessor on the watched binding.**
>   One line, and `Load` is already on `WatchedBinding`.
>   **Trigger: the first API that needs one** - code that must be handed the load half and must not be able to watch.
> - **Per-watch options, and with them a caller-owned settle window.**
>   `Watch` takes a context and nothing else, and coalescing stays where the knowledge is: `driver/env` and `driver/yaml` know what one editor save emits and swallow it in a settle window of their own, and `winreg` has no burst to swallow.
>   A caller naming a window for a mechanism they did not choose is guessing.
>   **Trigger: the first caller that needs one** - a deploy that rewrites four files over a second, say, wanting a window wider than the driver's own for a reason the driver cannot know.
>   Adding a variadic later is source-compatible, which is why waiting costs nothing.
> - **A core convenience for the operator kick.**
>   **Out of scope entirely**, with no trigger: see the SIGHUP amendment below.

> **Amended under [#361](https://github.com/onhotpath/ferry/issues/361): two watchable sources under one binding is answered, and the common case turns out not to need the answer.**
>
> As published under #272, this record files #361 and declines to decide it, and principle 6 says composition is part of using a watchable plane neatly.
>
> **The answer has two halves and only one of them is core's.**
> Which layer wins at an address is the composing source's own business, and ferry has no layering helper to put an opinion in.
> Fanning several mechanisms into one is mechanical, and it is the fan-in above: every layer armed before any is waited on, the first to speak answering, one layer's refusal refusing the whole at the bind, and one layer's loss ending the stream with that layer's reason.
>
> **What the common case turns out to be is not composition at all.**
> `env.New(env.DotEnv("base.env", "local.env")).Watched()` is two files, one source, one watch, and the driver already layers them.
> The composite case is a caller layering *two different drivers* - a YAML file under Consul - which is rarer than this record's filing of #361 implies.
> That is why the fan-in is deferred rather than shipped: the case it serves is real and it is not the everyday one, and a helper nobody reaches for is surface to keep true.
> #361 stays open with its trigger restated rather than closed by this record.
>
> **Torn reads are not solved by any of this, and were not solved by any shape considered.**
> A composite opens each layer inside one load, so two layers can be read at two instants.
> That is a property of the composite's own open, and closing it means the composite taking a snapshot of both, which is the composite's decision to make.
> This is recorded so the next reader of #361 does not go looking for it in the watch seam.

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): a caller-fired reload is guide material, not surface.**
>
> As published this record has nothing to say about an operator-driven reload, because a callback was the caller's to fire and the question never arose.
> Under the typed seam it arises, and the answer is that it needs no core export.
>
> A process that reloads on `SIGHUP` writes a `Notifier` whose `Change.Wait` reads the signal channel, and a five-line `WatchableSource` wrapping the source it wants to reload.
> Both are ordinary code over the published seam, and `docs/guide/watch-reload.md` is where they go.
>
> **No `ferry.Trigger`, no `ferry.Manual`, and no caller-facing way to fire a change into a driver's mechanism.**
> Core minting something a caller announces into puts a handle in the caller's hands that has to be threaded to a driver Option and then to the bind, which is the wiring mistake this whole amendment exists to delete.
> A caller who wants SIGHUP *and* a driver's own watch under one binding needs the two mechanisms fanned in, which is the fan-in's trigger above and not a signal feature.

## Consequences

- **Watch moves from Milestoned to Enabled and core grows no surface**, so [ADR-0001](0001-what-ferry-supports.md)'s capability table gains its first row to change bucket.
  The proof is a watcher built against the real shipped module through a nested workspace rather than against a sketch, which is the only form of evidence this claim can take.

  > **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): the bucket move stands and the second half of this sentence does not.**
  >
  > Core grows the watch seam, in the root package.
  > The bucket move is unaffected: Enabled is where the feature is, and the reason it lands inside core rather than outside is in the amendment to the first decision.
- **The commitment ADR-0001 made was honoured**: the mechanism landed, in core, piece by piece, and the feature ships outside, which is what milestoning promised and what Enabled means. *(Amended under [#13](https://github.com/onhotpath/ferry/issues/13): the feature ships inside core, and the commitment is honoured anyway. Milestoning promised the mechanism, and the mechanism is what the seam is built out of.)*
- **The deliverables are two documents rather than an API**: `docs/guide/watch-reload.md` and a runnable `examples/watcher`.
  A capability argued as buildable is worthless undemonstrated.

  > **Amended under [#364](https://github.com/onhotpath/ferry/issues/364): the deliverables now include an API.**
  >
  > As published this consequence is that nothing ships but prose.
  > Package `ferry/watch` ships, in the core module, and the guide is written against it rather than against a listing a reader copies.
  > **Why is in the amendment to the first decision above**, and the part that belongs here is what it costs: a package in core to keep true, in exchange for the ordering hole no driver could close and a demonstration nobody has to copy.

  > **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): the deliverable is rewritten rather than layered a third time.**
  >
  > As published this consequence is two documents; as amended under #364 it is two documents and a package.
  > Layering a third qualifier onto a sentence that was wrong twice is worse than replacing it, so it is replaced.
  > **The deliverable is the seam in the root package, `docs/guide/watch-reload.md` written against it, and one runnable example per watchable driver.**
  > `examples/watcher` and `ferry/watch` are both gone, and the guide is rewritten rather than edited, because it is written against `watch.Signal` and `watch.Values` throughout.
  >
  > *(Corrected when the deliverable shipped: `ferry/watch` is gone and `examples/watcher` is not.
  > What was deleted there is the hand-rolled watch machinery - the callback registry a driver's Option used to fill - and what is left is the one thing no driver example can show, which is the caller writing the announcement seam for a plane that announces nothing.
  > That is where the `SIGHUP` kick the amendment above sends to the guide is compiled, so the guide quotes it rather than carrying a listing that rots.)*
- **A reload is `Load` and no alias ships**, so there is one spelling of one operation.
  `LoadOver`'s two traps - a lost address keeping its stale value, and a composite replaced wholesale - are published in its godoc and in the guide, because the shape a reader reaches for first is the wrong one.
- **ferry now has a convention for a fallible iterator**, `(iter.Seq[T], func() error)`, decided on deliberate-versus-accidental discard rather than on compiler enforcement, which the first draft claimed and which does not hold.
  The cost is a loop that reads worse than `Seq2`'s, paid once per API and stated plainly.
- **The YAML sink refuses a commit over an externally changed file**, which costs one stat on the commit path and turns a silent lost edit into an actionable refusal.
  File watching is opt-in through a driver Option, so nothing runs for a caller who did not ask. *(Amended under [#13](https://github.com/onhotpath/ferry/issues/13): the opt-in is `Watched()` rather than an Option, and it is still an opt-in that starts nothing until a stream opens. The sink's refusal is untouched.)*
- **`Notifier` is specified, not shipped, with a named trigger.**
  Until a second watchable driver exists, one name across drivers buys nothing and costs a capability in a set that is already crowded.

  > **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): `Notifier` ships, and the count in the sentence above is retired.**
  >
  > As published the condition is a count of watchable drivers, which #272 already flagged as no longer the live one.
  > It is retired rather than re-counted: what buys the interface is a caller wiring one binding over a plane's own mechanism, and that caller exists.
  > `Notifier` ships as a value the source hands over rather than a capability ferry asserts, so the crowded assertion set is exactly as long as it was.
- **The watch surface is eight exports in the root package**, and every cut is recorded with a trigger rather than a maybe: the fan-in, the binding accessor, per-watch options with a caller-owned settle window, and any core convenience for an operator kick.
  A surface that grows later never has to shrink, which is what paying for each export at the seam buys.
- **A stream ends in one of three distinguishable ways**, and telling them apart is `errors.Is`: a failed reload carries ferry's load sentinels, a lost watch carries `ErrWatchLost`, and a cancelled context carries `ctx.Err`.
  Silence is no longer one of the endings, which is the defect the callback shape shipped with.
- **`driver/yaml` gains `github.com/fsnotify/fsnotify` and `golang.org/x/sys`**, by an owner ruling recorded above, so every consumer of that driver pulls both whether or not it watches anything.
  What it buys is a refusal the poll could not make, a sharp edge deleted, and one mechanism across all three watchable drivers.

Evidence: `prototype/watcher` on [`proto/05-watch-grammar`](https://github.com/onhotpath/ferry/tree/proto/05-watch-grammar), four tests under `-race` against the real module: `TestWatcherBuildsOutsideCore`, `TestErrorConventions`, `TestHeldValueIsImmutable` and `TestLoadOverAsReloadSharpEdges`.

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): the evidence for everything amended above.**
>
> Branch [`proto/06-typed-watch`](https://github.com/onhotpath/ferry/tree/proto/06-typed-watch), behind build tags: core, `ferrytest`, `driver/env`, `driver/yaml` and `driver/windows/winreg`, green under `-race`, with `make check` and `make lint` green on the default build.
> Every alternative this record declines above - the second constructor, the sealed struct, a core-minted trigger, the debounce option and the fan-in - was built and measured on that branch rather than argued from a sketch.
