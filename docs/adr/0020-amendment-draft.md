# DRAFT: the ADR-0020 amendment for the typed watch seam

**This file is a draft and is not an ADR.**
It is the text proposed for [ADR-0020](0020-watch-and-reload.md), written on branch `proto/06-typed-watch` and carried here so it can be read beside the prototype rather than reconstructed from a pull request.
Nothing in `docs/adr/0020-watch-and-reload.md` has been edited.
When this is accepted it is folded into that file in the amend-in-place voice this repository uses, and this file is deleted in the same commit.

Evidence for every claim below is the `protoe` build of branch `proto/06-typed-watch`: core, `ferrytest`, `driver/env`, `driver/yaml` and `driver/windows/winreg`, green under `-race`, with `make check` and `make lint` green on the default build.

---

## The six principles

They are stated first because every decision below is one of them applied, and because three of them were never written down.

1. **A plane fires events opt-in.**
   A source that was not asked to watch touches nothing and starts nothing.
2. **The consumer uses a watchable plane neatly.**
   One context, no goroutine the caller starts, nothing to stop, and a stream that opens with a load.
3. **Refusal lands at Bind, and at compile time where that is free.**
4. **The stream ends observably, with a reason.**
5. **An event is a hint carrying nothing, and the reload is `Load`.**
6. **Composition is part of neatly.**
   Two watchable planes under one binding is an ordinary configuration, not a hypothetical.

The first three are the owner's.
The last three are added here, and each of them is a defect in the shipped design named as a rule: a watch that died silently, an announcement that tried to carry a payload, and a composition nobody could write.

---

## Amendment 1: the typed seam supersedes the callback shape

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): the change signal crosses the seam as a typed interface the source publishes, and the callback Option is retired in all three drivers.**

As published, and as amended under [#352](https://github.com/onhotpath/ferry/issues/352) and [#272](https://github.com/onhotpath/ferry/issues/272), the shape is a caller-supplied callback handed to a driver Option at construction: `onChange func(context.Context)`, an Option and not a method, no error return, no `Stop`, the watch opened inside the constructor.
Three drivers agreed on it and this ADR recorded that agreement as the shape being a shape.

**What moves is the direction the signal travels and what it is allowed to say.**

```go
type Notifier interface {
	Notify(ctx context.Context) (Change, error)
}

type Change interface {
	Wait(ctx context.Context) (bool, error)
	Close() error
}

type WatchableSource interface {
	Source
	Watching() (Notifier, error)
}

func BindWatched[T any](src WatchableSource, opts ...Option) (*WatchedBinding[T], error)

func (wb *WatchedBinding[T]) Load(ctx context.Context) (T, error)
func (wb *WatchedBinding[T]) Watch(ctx context.Context) (seq iter.Seq[T], errf func() error)

var ErrWatchLost = errors.New("the watch was lost")
```

Eight exports.
The driver side is one method on the source that already holds the configuration:

```go
func (s *Source) Watched() *WatchedSource   // env, yaml, winreg: the same spelling, no arguments
```

**The reasons, one per principle.**

**Principle 2 is what the callback could not do.**
A callback is handed to a constructor, so the watch starts before `Bind` has returned the binding the callback wants to load through, and the shipped `ExampleWatchFiles` paid for that with an atomic pointer nil-checked on every call.
This ADR's own [#364](https://github.com/onhotpath/ferry/issues/364) amendment named that hole and closed it with a `Signal` the caller carries between two objects.
The typed seam removes the hole rather than absorbing it: nothing watches until a stream opens, and the stream opens with a load, so a change that landed before the bind is in the value the range yields first.
There is one context, the stream's, where there were two that nothing reconciled.

**Principle 4 is what the callback could not say.**
`func(context.Context)` has no error return, so a driver that lost its watch had nowhere to report it; this ADR settled that by calling the callback once and returning, and by trusting the next load to report something.
It does not: a lost watch means no next load ever happens, so a process holding stale configuration has nothing to tell it so.
`Change.Wait` answers `(bool, error)`, so a lost watch ends the stream with the driver's own reason under `ErrWatchLost`, and a quiet ending is `false` with a nil error.
This is the only new sentinel core carries, and it earns its place in amendment 4.

**Principle 3 is what a caller-supplied callback cannot be.**
`WatchableSource` is an ordinary interface and a plain `Source` does not satisfy it, so handing an unwatchable source to `BindWatched` is a compile error with no conversion available.
What a type cannot carry is whether *this* source has anything to watch, because watchability is option-dependent: an `env.Source` is the same type whatever `DotEnv` said.
`Watching() (Notifier, error)` is where that half is answered, once, at the bind, before any load.

**Principle 5 is unchanged and is now enforced.**
`Change.Wait` reports that the plane may have changed and carries nothing else, exactly as the callback did, and the reload is still `Load`.
There is no `Reload`, and no payload anywhere on the seam.

**Principle 1 is unchanged.**
`New`, `NewSource` and `NewSource` for the registry are untouched and still start nothing.
`Watched()` converts, and the conversion touches nothing either: the mechanism opens when a stream does.

**Why a conversion method and not a second constructor.**
A second constructor - `env.NewWatched(opts...)` - was built and measured on this branch, and it duplicates the whole option list per driver.
The conversion takes only what watching needs and nothing the source already has, which is [ADR-0017](0017-the-registration-api-and-the-value-it-builds.md)'s `KeyCodec.AsMapKey` precedent read on the source seam: a narrowing that says one more thing about a value already built.
It also removes the mistake the old shape's sharp edge was about, and the owner named it exactly: the file was specified in one place and the watcher set up in another, and forgetting the second was silent.
`env.New(env.DotEnv(".env")).Watched()` names both in one expression.

**Why an interface and not a sealed struct core mints.**
A sealed `WatchableSource` struct with `ferry.Watchable(src, n)` and `ferry.Unwatchable(err)` constructors was built and measured on this branch.
It is two more exports, it never sealed anything - both constructors are exported, so anyone can mint one - and it made the one misuse it appeared to guard *easier*, because `Watchable(srcA, notifierB)` pairs a source with an unrelated mechanism in one line at the call site.
Under the interface the same mistake needs a type whose own two methods contradict each other.
The one misuse neither shape detects is a mechanism that binds and never fires, and it is recorded rather than argued about: it is a passing test on this branch.

**What the caller writes now, against the nine steps this ADR's own example needed:**

```go
wb, err := ferry.BindWatched[Config](env.New(env.DotEnv(".env")).Watched())
seq, errf := wb.Watch(ctx)
for cfg := range seq { publish(cfg) }
if err := errf(); err != nil { alert(err) }
```

---

## Amendment 2: "core grows no surface" is retired

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): core carries the watch seam, and the claim that it does not is withdrawn.**

As published, the first decision of this ADR is titled "Watch moves from Milestoned to Enabled, and core grows no surface", and it says in as many words that ferry ships no `Watch`, no `Notifier` and no watcher.
The [#364](https://github.com/onhotpath/ferry/issues/364) amendment kept the claim by putting the helper in a package of the core module rather than in the root package, and said so explicitly: "the root package `ferry` still grows no surface".

**That distinction is retired.**
`ferry/watch` is deleted and eight exports land in the root package.

The claim was true of the machinery and false of the feature, and holding it cost the two defects amendment 1 names.
What survives from the published argument is everything about *why* the machinery could land piecemeal: `Bind` split from open, `Load` returning a value, per-open minted sets, `Binding[T]`.
Every one of those still carries the watch, and the seam added here is the one thing none of them could be: a place for the driver to say the plane changed and a place for it to say the watch is over.

**What ADR-0001's Enabled bucket meant is unchanged.**
Enabled is still the default landing place for a feature, and this is the case where the feature could not land outside because the hole it closes is closable only over core's own `Binding[T]` and only in the loop that owns the registration.

---

## Amendment 3: `Notifier` ships, and the decline is answered by never asserting it

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): the interface this ADR wrote out in full and declined now ships, in a shape none of its three costs applies to.**

As published, the last decision of this ADR specifies `Notifier` and declines it for three costs, and the [#352](https://github.com/onhotpath/ferry/issues/352) amendment adds a fourth.
The [#272](https://github.com/onhotpath/ferry/issues/272) amendment restates the position with three watchable drivers and declines it again.

The three published costs, answered one at a time:

- **"It adds a capability to an assertion set that is already the thing [#201](https://github.com/onhotpath/ferry/issues/201) says stops scaling."**
  It does not, because nothing asserts it.
  `Notifier` is handed to core by `WatchableSource.Watching`, so it is a value that crosses the seam and never a type ferry probes for.
  The capability ladder - `Prober`, `Enumerator`, `Releaser` and the rest, all discovered by assertion - is exactly as long as it was.
- **"It is option-dependent, so a `yaml.Source` asserts `Notifier` whether or not watching was enabled and the refusal moves to call time."**
  This is the real objection and it is why the seam has two halves.
  Watchability by type is `WatchableSource`, decided at compile time; watchability by configuration is `Watching`'s error, decided at `BindWatched`.
  Neither is a call-time refusal.
- **"Core would own the semantics of a channel it never reads."**
  There is no channel.
  The published sketch was `Notify(ctx) (<-chan struct{}, error)`, and the shipped shape is `Notify(ctx) (Change, error)` with an explicit wait and an explicit release, so core reads what it asked for and the driver owns when it answers.

The fourth cost, added under #352 - "nothing in the two Options is shared" - is the one this shape turns into the argument for shipping.
What the three mechanisms share is not their internals; it is the *contract*: register, wait once, release, and say whether that was a change, an ending or a loss.
`driver/windows/winreg` had already written that contract for itself, in `registry.go`, as `Notifier.Arm` and `Change`, for the reason the #272 amendment gives at length.
Promoting the weaker of the three mechanisms is what makes one loop correct for all three: a persistent queue can model an armed-once registration and the reverse is false.
The winreg port on this branch is an adapter and nothing else.

**The restated trigger is met and taken.**
The #352 amendment restated the trigger as "a caller writing the same watch wiring against two drivers and wanting one binding for both".
That caller now writes the wiring once, in core, and #361 is answered in amendment 5.

---

## Amendment 4: the error model of the watch, and why exactly one sentinel is core's

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): `ErrWatchLost` is core's, and every refusal at Bind is the driver's own error under `ErrPlane`.**

Three failures exist and they land in three places.

- **A driver instance that cannot watch** - `env` with no `DotEnv`, `yaml` over no path, a registry that reports no change of its own - is `Watching`'s error, wrapped by core in `ErrPlane`, with the driver's own sentinel reachable underneath: `env.ErrWatch`, `yaml.ErrWatch`, `winreg.ErrWatch`.
  There is no core sentinel for it.
  At a bind the caller knows which driver they constructed, so per-driver matching is the honest shape, and a cross-driver "was it specifically the watch that was refused" match has no consumer anybody has named.
  `driver/yaml` gains a sentinel it never had, which closes a loose end recorded in the hygiene list below: its poll had no failure mode at all to report.
- **A source that answers `Watching` with no mechanism and no reason** is a driver contract violation, not a plane refusal, so it is `ErrDriver` - the same bucket as an `OpenFunc` that comes back nil.
- **A watch the mechanism could not keep** is `ErrWatchLost`, minted by core's own loop, wrapping the driver's reason, ending the stream.

**`ErrWatchLost` is the one sentinel that pays for itself**, and the test is who matches it.
Its consumer is the caller's restart policy, which is written once against any driver, so a per-driver spelling would make that policy a switch over drivers.
It is also the only way to tell the three-way split apart at the point it matters: a failed reload carries ferry's own load sentinels, a lost watch carries this, and a cancelled context carries `ctx.Err`.
There is no leaner spelling that keeps those three distinguishable.

---

## Amendment 5: what is deferred, and the trigger for each

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): four things were built, measured and left out, and each has a trigger rather than a maybe.**

- **`Debounce` as a core `WatchOption`.**
  Built on this branch as one export.
  Left out because coalescing is knowledge the driver has and the caller does not: `driver/env` and `driver/yaml` know what one editor save emits and swallow it in a 50ms settle window of their own, and `winreg` has no burst to swallow.
  A caller naming a window for a mechanism they did not choose is guessing.
  **Trigger: a caller who wants a window wider than the driver's own for a reason the driver cannot know** - a deploy that rewrites four files over a second, say.
  It arrives as a `WatchOption`, and `WatchOption` arrives with it.
- **`WatchAll(read Source, of ...WatchableSource) WatchableSource`.**
  Built on this branch, in the test tree, over nothing but the published seam - which is the whole of the argument for leaving it out.
  It composes the watch and never the read, because ferry has no layering helper and this is not the place to invent one.
  **Trigger: callers writing it.**
  Amendment 6 records why that may take a while.
- **`WatchedBinding.Binding() *Binding[T]`.**
  One line, and `Load` is already on `WatchedBinding`.
  **Trigger: a caller who has to hand the load half to code that must not be able to watch.**
- **`WatchedBinding.Watch` taking `WatchOption`s at all.**
  It takes a context and nothing else.
  **Trigger: the first `WatchOption` that ships**, which is `Debounce` or nothing.

---

## Amendment 6: composition, and what #361 turns out to be

> **Amended under [#361](https://github.com/onhotpath/ferry/issues/361): two watchable sources under one binding is answered, and the common case turns out not to need the answer.**

As published under #272, this ADR files #361 and declines to decide it.

**The answer has two halves and only one of them is core's.**
Which layer wins at an address is the composing source's own business, and ferry has no layering helper to put an opinion in.
Fanning several mechanisms into one is mechanical, and it is `WatchAll` above: every layer armed before any is waited on, the first to speak answering, one layer's refusal refusing the whole at the bind, one layer's loss ending the stream with that layer's reason.

**What the prototype found is that the common case is not composition at all.**
`env.New(env.DotEnv("base.env", "local.env")).Watched()` is two files, one source, one watch, and the driver already layers them.
The composite case is a caller layering *two different drivers* - a YAML file under Consul - which is rarer than this ADR's filing of #361 implies.
That is why `WatchAll` is deferred rather than shipped: the case it serves is real and it is not the everyday one, and a helper nobody reaches for is surface to keep true.

**Torn reads are not solved by any of this and are not solved by any shape considered.**
A composite opens each layer inside one load, so two layers can be read at two instants.
That is a property of the composite's own open, and closing it means the composite taking a snapshot of both, which is the composite's decision to make.
This is recorded so the next reader of #361 does not go looking for it in the watch seam.

---

## Amendment 7: SIGHUP is guide material, not surface

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): a caller-fired reload needs no core export.**

A process that reloads on `SIGHUP` writes a `Notifier` whose `Change.Wait` reads the signal channel, and a five-line `WatchableSource` wrapping the source it wants to reload.
Both are ordinary code over the published seam, and `docs/guide/watch-reload.md` is where they go.

No `ferry.Trigger`, no `ferry.Manual`, no caller-facing way to fire a change into a driver's mechanism.
Core minting something a caller can announce into is the shape variant C of the prototype took, and it was rejected: it puts a handle in the caller's hands that has to be threaded to a driver Option and then to the bind, which is the wiring mistake this whole amendment exists to delete.

**Trigger, and it is `WatchAll`'s:** a caller who wants SIGHUP *and* a driver's own watch under one binding needs the two mechanisms fanned in, and that is the composition helper above rather than a signal feature.

---

## What this deletes

- **`ferry/watch`**, the whole package: `Signal`, `Signal.Changed`, `Values`.
  Its ordering hole is closed structurally and its coalescing moves into the drivers.
- **`env.WatchFiles(ctx, onChange)`**, **`yaml.Watch(ctx, every, onChange)`** and **`winreg.Watch(ctx, onChange)`**, with the `watcher` loops behind them.
- **`driver/yaml`'s poll**: the stamp comparison loop, the `look` default interval, and the interval argument.
  The sink's commit-time stamp comparison is untouched, because that is the sink's mechanism and not the watch's.
- **`examples/watcher`**, and the two driver examples that demonstrated the nine-step wiring.
  Each driver ships one `ExampleSource_Watched` in its place.

Both driver `ErrWatch` sentinels **survive** and are not consolidated; see amendment 4.

---

## Amendment 8: `driver/yaml` takes the fsnotify dependency

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): the polling watcher is retired, and the "polling is the answer that needs no argument" paragraph is superseded by an owner ruling.**

As published under #13 and restated under #352, this ADR says: "`driver/yaml` depends on `go.yaml.in/yaml/v3` and nothing else, and a watch is not a reason to widen that: the module rules say a driver's dependencies are argued for, and polling is the answer that needs no argument."
The #352 amendment adds that "`driver/yaml`'s polling watcher is the fallback if the dependency bites, and it is already written."

**Both paragraphs are superseded.**
`driver/yaml` requires `github.com/fsnotify/fsnotify`, with `golang.org/x/sys` indirect, and watches the way `driver/env` does.

**The relaxation is the owner's and is recorded as a ruling rather than derived from the rule.**
The module rule that a driver's dependencies are argued for is not repealed; this is the argument, and the owner made the call that mechanism uniformity across the three watchable drivers outweighs the second dependency in this module.

**What it buys, stated as three things rather than as latency.**

- **A refusal this driver could not make.**
  A poll over a directory that is not there is steady rather than wrong, so there was nothing to refuse and `driver/yaml` had no watch failure mode at all.
  An inotify watch on the same directory fails, so `yaml.ErrWatch` exists and lands at the stream's first registration with the caller told why.
- **A sharp edge deleted.**
  "One change is invisible: a rewrite landing in the same modification-time tick that leaves the length alone" was the cost of a stat, and it is published in the Option's godoc and in the guide.
  It dies with the poll.
- **One mechanism across three drivers.**
  `Watched()` takes no argument anywhere, so there is no interval to name in one driver and not in the other two, and the conformance suite in amendment 9 asks all three the same questions.

**What it costs, stated plainly.**
Every consumer of `driver/yaml` now pulls fsnotify and `golang.org/x/sys`, including one that never watches anything.
That is the same cost `driver/env` pays under #352 and it is paid for the same reason, and it is bounded the same way: this is a driver module, core's `require` block is untouched, and a program that imports neither driver sees neither dependency.

**Duplication, measured.**
`driver/env` and `driver/yaml` now hold the same watch mechanism, and the shared shape is: watch the containing directory rather than the file, filter events by exact name, drop `Chmod`, drain the errors channel, and swallow the burst in a 50ms settle window.
That is roughly 90 lines each, of which about 60 are genuinely the same.
The two differ in what they watch - `env` watches a set of files across several directories and `yaml` watches one - and in nothing else.
**A shared seam is not built now and is not proposed here.**
They are separate modules, so sharing means a third module that both depend on, and 60 duplicated lines across two drivers is a worse trade than a module in the graph of every consumer of either.
The trigger, if it is ever wanted: a third file-watching driver, or a change to the mechanism that has to be made twice and is got wrong once.

---

## Amendment 9: a conformance suite for a watchable driver

> **Amended under [#13](https://github.com/onhotpath/ferry/issues/13): `ferrytest.Watchable` is proposed, and it is [ADR-0014](0014-what-ferrytest-exports.md)'s decision to admit.**

This ADR's own rule, from [ADR-0001](0001-what-ferry-supports.md), is that core's leverage over what it does not ship is a conformance harness.
The watch seam is exactly that: core owns the loop and the driver owns the mechanism, and nothing today checks that a driver's mechanism keeps the promises the loop depends on.

The sketch on this branch is two exports, `ferrytest.WatchPlane` and `ferrytest.Watchable`, and it asserts seven properties through `BindWatched` and the stream and through nothing else: the stream opens with a load, a change reloads, a burst is one reload, a held value never moves, cancelling ends it cleanly, a lost watch ends it with a reason, and a source that cannot be watched is refused at the bind.
Two of them are optional and are skipped where the plane declares the driver cannot reach that state.

It runs against all three drivers on this branch - `env` and `yaml` for real, `winreg` against its fake.
**It is not part of `ferrytest` until ADR-0014 says so**, and the surface test on this branch was taught to honour build constraints precisely so that a proposal behind a tag is provably not part of the published surface.

---

## Record hygiene: the in-place corrections the real pull request must carry

None of these has been made on this branch.
No published ADR is edited here.

**In [ADR-0020](0020-watch-and-reload.md) itself:**

1. **The stale counts.**
   Under "What this ADR does not decide", the first bullet's #352 note says `driver/env` "is a second one"; the #272 amendment already flags this and the fix has not been made.
   The last consequence still reads "until a second watchable driver exists", which #272 says is superseded and which this amendment supersedes again: the condition is the seam, not a count.
2. **The deliverables sentence.**
   The third consequence as published is "the deliverables are two documents rather than an API", already amended once under #364 to "the deliverables now include an API".
   It needs amending a third time and should be rewritten rather than layered: the deliverable is the seam in the root package, the guide, and one example per watchable driver.
3. **The `(iter.Seq[T], func() error)` section.**
   Its #364 amendment says the convention "has its first shipped user", naming `watch.Values`.
   That user is deleted; the shipped user becomes `WatchedBinding.Watch`.
   The convention itself does not move.
4. **The `LoadOver` traps section** is untouched and stays exactly as it is.
   It is the one part of this ADR the new seam does not disturb.
5. **The YAML sink's commit-time refusal section** is untouched.
   Amendment 8 must say so explicitly where it retires the poll, because both mechanisms compared a `stamp` and only one of them is going.

**Elsewhere:**

6. **[ADR-0002](0002-core-and-sub-modules.md), the #352 amendment**, records `driver/env`'s fsnotify dependency and points at ADR-0020 as where it is argued.
   It gains `driver/yaml`'s, pointing at amendment 8.
   The `golang.org/x/sys` argument under #272 contains the sentence "which is not the trade `driver/env`'s fsnotify made: there is no polling fallback for a registry handle" - the fallback it refers to is now deleted, so that sentence needs a note rather than a rewrite.
7. **[ADR-0002](0002-core-and-sub-modules.md), the `watch/` line.**
   The #364 amendment to "Core admits by a veto and two routes" describes `watch/` as a directory of the core module admitted by route (b).
   The package is deleted; the line goes with it.
8. **[ADR-0014](0014-what-ferrytest-exports.md)** decides whether `WatchPlane` and `Watchable` join the exported list, and the list in `ferrytest/surface_test.go` moves with it or does not move at all.
9. **[ADR-0011](0011-the-error-model.md)**'s sentinel table gains `ErrWatchLost` and gains nothing else.
   The per-driver watch sentinels are documented by their own drivers under `ErrPlane`; this is **not** a consolidation, and the earlier draft's line about a core sentinel replacing both driver `ErrWatch` sentinels is withdrawn.
10. **`driver/windows/winreg` has no runnable watch example** and never had one, where `env` and `yaml` both do.
    The port makes one writable against the fake, and the pull request should carry it.
11. **`docs/guide/watch-reload.md` is rewritten rather than edited.**
    It is written against `watch.Signal` and `watch.Values` throughout, and both are deleted.
    Its new scope: the four-line wiring, the three endings and how to tell them apart, restart policy, the SIGHUP recipe from amendment 7, and composing two drivers with the caller-written fan-in from amendment 6.
12. **`README.md` and each driver's `README.md`** quote the watch example verbatim from the `Example` that generates them, so each has to move with its example.
