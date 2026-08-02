# 12. How a caller holds a binding, and where a per-request plane is supplied

Status: Accepted
Date: 2026-08-02
Ticket: [#25](https://github.com/onhotpath/ferry/issues/25)

## Context

[ADR-0004](0004-source-and-sink.md) filed this ticket against itself, in a section titled "A decision with no ticket, surfaced rather than taken".
`Source.Bind` is expensive and its result is reusable, which works for every plane whose handle is long-lived and does not work for a plane that *is* the request.
The cause it names is that `Source` conflates two things with different lifetimes: the driver's key grammar, which is per schema, and the plane instance, which for `queryparam.Source{Values: r.URL.Query()}` is per request.

Four ADRs handed this ticket something by name.

[ADR-0004](0004-source-and-sink.md) supplied the measurement and the framing, and closed with the sentence this ADR has to answer: "the caller-facing lifecycle is now load-bearing and unowned... the per-request use case xload was pitched at has no worked answer, and the `query` driver's place on the first-party list is weaker for it."

[ADR-0010](0010-the-entry-point-and-the-schema-cache.md) **halved the motivation before this ticket opened**.
It measured the schema compile at 47370 ns against a 34 ns cached lookup, so a caller-held value can no longer be argued on compile cost, and the question narrows to the bind alone.
It also implemented the caller-held candidate as `Schema[T]`, listed it as candidate D, and declined to refuse it because refusing it is this ticket's.

[ADR-0010](0010-the-entry-point-and-the-schema-cache.md) again, on `Compile[T]() error`: "if #25 ever exports a handle this is the function whose signature changes", which would be a break affordable only because [ADR-0002](0002-core-and-sub-modules.md) keeps ferry at v0.

[ADR-0006](0006-defaults-and-zero-values.md) put a **second, unrelated decision** on this surface, twice: "whether the observation is spelled as a callback, a recorder, or a returned report is an API question that belongs with the caller-facing lifecycle, which is #25's."
That is the presence observation ADR-0001 milestones plane inspection on.
[ADR-0011](0011-the-error-model.md) leaves nothing here; it is named in the ticket's neighbourhood but its "what this ADR does not decide" list does not reach this surface.

Three constraints arrive as inherited rather than open.
[ADR-0009](0009-typed-codec-registration.md) requires a registry to be long-lived, because a per-call one gives an unbounded, non-evictable schema cache.
[ADR-0010](0010-the-entry-point-and-the-schema-cache.md) requires `Load` to produce a new value, on ADR-0006's reload leak and ADR-0011's yield-nothing rule.
[#20](https://github.com/onhotpath/ferry/issues/20) owns the walk's concurrency, and this ADR says what a caller may share without deciding any of it.

This ADR is written from a throwaway prototype on branch `proto/25-binding`, which never merges.
It is built on `proto/16-entry-point`, so every measurement runs against the real schema cache, the real walk, the real `Path` and `Value`, a real YAML plane over real files, and a query-parameter driver over ADR-0004's own key-table helper.
Every number is from that prototype unless it cites the survey.

**On the numbers in this ADR.**
Run three times on the same binary, `ns/op` moved by up to 40 per cent on the headline row and one pair of parallel rows crossed over entirely, while every allocation column was identical to the byte.
So allocations are quoted as measurements, times as a scale, and one claim an earlier draft of the prototype made from a single run is withdrawn below.

## Decision

### What this closes

| The ticket asked | Closed | Where |
| --- | --- | --- |
| where a per-request plane instance is supplied | **yes**: in the `context.Context`, by the driver, and core supplies no mechanism | [The plane](#the-plane-arrives-in-the-context-because-it-is-the-only-per-load-channel-there-is) |
| whether that is part of the `Source` contract or an escape hatch | **neither**: it is a rule about the driver's own public shape, and `Source` is untouched | [One shape per driver](#a-driver-with-a-per-request-plane-has-one-public-shape-and-that-is-514s-first-item) |
| whether ferry exposes a caller-facing bind-then-load split | **yes**: `Bind[T]` and `BindSink[T]`, in both directions, and `Load[T]` and `Dump[T]` are literally them with the handle discarded | [The binding](#a-caller-may-hold-a-binding-and-the-one-shot-verb-is-that-binding-with-the-handle-dropped) |
| whether it amends ADR-0004's signatures | **no signature, one helper**: the static table is the bind's and the minted set is the open's | [The two tiers](#adr-0004s-key-helper-is-amended-the-minted-set-belongs-to-the-open) |

Two more this ticket inherited:

| Handed over by | Closed | Where |
| --- | --- | --- |
| ADR-0006: whether the presence observation is a callback, a recorder or a returned report | **yes: none of the three**, it is a `Source` wrapping a `Source` and core spells nothing | [The observation](#the-presence-observation-is-a-source-and-core-spells-nothing) |
| ADR-0010: whether `Compile[T]()`'s signature changes | **no**, and the reason is that a binding is not a schema | [Compile](#compilet-error-stands-because-a-binding-is-not-a-schema) |

**Three things this ADR does not close.**

- **Whether the walk may run concurrently** is [#20](https://github.com/onhotpath/ferry/issues/20)'s, unchanged.
  What this ADR adds to #20 is that a binding is now a value reached by many goroutines at once, which under `Load[T]` alone it never was.
- **Whether ferry ever grows a third binding shape**, for a plane that is neither a `Source` nor a `Sink`.
  Nothing asks for one; ADR-0004's two interfaces are what these two functions mirror.
- **Whether core ever ships the observing `Source`.**
  The mechanism is decided; whether a combinator ships is ADR-0001's bucket rule, exactly as ADR-0004 left `FirstOf`.

### What a consumer writes

This section is first, because ADR-0009 was sent back for arguing from measurements without showing the API a consumer meets, and this ticket owns a caller-facing shape.
Three programs, chosen because one of them wants no binding at all, and every one of them was run.

The surface this ADR adds, in full:

```go
func Bind[T any](src Source, opts ...Option) (*Binding[T], error)
func BindSink[T any](sink Sink, opts ...Option) (*SinkBinding[T], error)

func (b *Binding[T])     Load(ctx context.Context) (T, error)
func (b *Binding[T])     LoadOver(ctx context.Context, seed T) (T, error)
func (b *SinkBinding[T]) Dump(ctx context.Context, v T) error
```

Two functions, two types, three methods, no new Option and no new interface.
Neither type has any other method or any exported field, so both are handles and neither is a schema view.

#### Program 1: startup configuration, which wants no binding

```go
func load(ctx context.Context, path string) (Config, error) {
    cfg, err := ferry.Load[Config](ctx, yaml.Source{Path: path})
    if err != nil {
        return cfg, err
    }
    return cfg, ferry.Dump(ctx, cfg, yaml.Sink{Path: path})
}
```

Nothing changes here and nothing should.
Each phase happens exactly once, so a binding would be a value constructed, used and dropped, which is what `Load` and `Dump` already do internally.
Run: `{Name:svc DB:{Host:db1 Port:5432}}`, with the default applied and the file rewritten.

Two independent reasons a binding buys this program nothing, and the second is the interesting one.
There is nothing to reuse.
And even if there were, a tree driver pays nothing at `Bind`: it walks segments and builds no plane key, so the phase a binding hoists is already free.
Measured, the same YAML load with and without a held binding: ~16000 ns and 142 allocations against ~16000 ns and 138.

#### Program 2: a request handler, which is what this ticket was filed for

```go
type Filter struct {
    Q      string `ferry:"q"`
    Page   int    `ferry:"page"`
    Size   int    `ferry:"size"`
    Sort   string `ferry:"sort"`
    Desc   bool   `ferry:"desc"`
    Cursor string `ferry:"cursor"`
}

func NewHandler() (http.Handler, error) {
    b, err := ferry.Bind[Filter](query.Source{})   // once, at startup
    if err != nil {
        return nil, err
    }
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        f, err := b.Load(query.WithValues(r.Context(), r.URL.Query()))
        if err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
        }
        ...
    }), nil
}
```

**The same handler with no binding**, which is the same driver value and the same context constructor:

```go
func NewHandler() http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        f, err := ferry.Load[Filter](query.WithValues(r.Context(), r.URL.Query()), query.Source{})
        ...
    })
}
```

Run side by side against one request, the two produce the identical value, and the difference is 85 allocations against 45.

**And a caller who forgets the plane**, measured:

```
err = query: no values in the context
```

The refusal lands at open, which is where ADR-0004 already puts "the plane is not reachable", and it is per load rather than at bind.
Its class is [ADR-0011](0011-the-error-model.md)'s `ErrPlane` with the driver's provenance marker, which is a decision this ADR takes rather than a line the prototype printed: the prototype predates the error model and returns a bare sentinel.

#### Program 3: an exporter, which is the same shape on the write side

```go
type Stats struct {
    Uptime  int            `ferry:"uptime"`
    Queues  map[string]int `ferry:"queues"`
    Version string         `ferry:"version"`
}

func NewExporter(kv *consul.Client) (*Exporter, error) {
    b, err := ferry.BindSink[Stats](consul.Sink{Client: kv, Prefix: "svc/"})
    if err != nil {
        return nil, err
    }
    return &Exporter{b: b}, nil
}

func (e *Exporter) Tick(ctx context.Context, s Stats) error {
    return e.b.Dump(ctx, s)   // every tick, and the map's shape changes between them
}
```

**The same exporter with no binding:**

```go
func (e *Exporter) Tick(ctx context.Context, s Stats) error {
    return ferry.Dump(ctx, s, consul.Sink{Client: e.kv, Prefix: "svc/"})
}
```

Run, both write the same four keys, `[QUEUES_IN QUEUES_OUT UPTIME VERSION]`, and the difference is 171 allocations against 133.

`Queues` is a map, so each tick realises a different address set, and the binding is held across all of them.
That is the section below on why the sink binding works at all.

#### The observation, which needs nothing from core

```go
rec := ferry.NewRecord()                       // or the caller's own fifteen lines
cfg, err := ferry.Load[Config](ctx, ferry.Observing(src, rec))

rec.At(addr)   // number("0")  the plane holds a zero
               // absent       the plane holds nothing
```

`Observing` is a `Source` wrapping a `Source`, so it composes where any other source does, and it is the caller's if core never ships one.
Put on a child of a `FirstOf` it answers which layer supplied a value, measured:

```go
q, f := ferry.NewRecord(), ferry.NewRecord()
cfg, err := ferry.Load[Config](ctx, ferry.FirstOf(
    ferry.Observing(query.Source{}, q),
    ferry.Observing(yaml.Source{Path: p}, f),
))
```

```
q.At(/name)  ->  string("from-query")
f.At(/name)  ->  absent
```

### The whole of this ADR is additive, and the compiler is what says so

The claim is stronger than "the binding is optional", and it is the one a reader should be able to check:

> Delete `Bind`, `BindSink`, `Binding[T]` and `SinkBinding[T]` from ferry and all three programs above still compile and still produce the same values.

Prose cannot establish that, so the prototype compiles the no-binding versions of all three programs against a generated `ferry` package that exports **`Load`, `LoadOver`, `Dump`, `Compile` and nothing else**, with ADR-0004's four interfaces behind them and no binding API at any point.

```
go build ./...   ->  ok
```

And the negative control, the same module with one line added that does use it:

```
./main.go:58:35: undefined: ferry.Bind
```

So the check is a real one rather than a build that would have passed regardless.

**One thing is not additive, and it is not the ferry API.**
The rule below that a per-request driver takes its plane from the context and never from a field is a *driver* convention, and a ferry with no binding would not have chosen it: a `query.Source{Values: v}` field reads better and serves the only call site such a ferry has.
So the surface is additive and the convention is a decision, and the cost of the convention is a call site that reads worse for a caller who never holds a binding.
That is stated again in the consequences rather than left here.

### A caller may hold a binding, and the one-shot verb is that binding with the handle dropped

> `Bind[T](src, opts...) (*Binding[T], error)` performs the schema lookup and the driver's `Bind`, and returns a value the caller may hold and load through many times.
> `Load[T]` and `LoadOver` **are** `Bind` followed by the corresponding method, with the handle discarded.

The second sentence is an implementation constraint and not a description, for the reason ADR-0010 gives about its own two verbs: two entry points that could disagree about what a load does are 5.14's first item at ferry's own front door.
In the prototype `LoadOver` is four lines and all four are `Bind` plus the method.

**What decides it is the measurement, and ADR-0010 already removed the other argument.**
A whole per-request load of the six-address struct ADR-0004 used, through the real cache and the real walk:

| | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `ferry.Load[T]`, binding per load | ~4500 | 2592 | **85** |
| `b.Load(ctx)`, binding held | ~2200 | 1248 | **45** |

Forty allocations and half the bytes, on every request, forever.

**The scale that decides whether forty allocations matter is what else the same request costs**, and it is not a number this ADR gets to choose:

| | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `url.ParseQuery`, the same six parameters | ~400 | 96 | 6 |
| `http.ReadRequest`, one GET off the wire | ~2300 | 5681 | 17 |
| `encoding/json` into the same struct | ~800 | 80 | 1 |
| **`xload`, the ancestor, the same six keys** | ~1800 | 1464 | **22** |

The last row is the one that settles it.
**Binding per load, ferry allocates 3.9 times what xload allocates on the use case xload was pitched at**, and a held binding brings it to 2.0 times.
The residual is the prototype's own, whose `Path` allocates per segment, and ADR-0010 already records that caveat about the same prototype.
What a library cannot do is ship a design in which the fix for losing to its own ancestor exists, is one function, and was declined.

**And the argument for refusing was real and is answered rather than ignored.**
ADR-0006's rule that refusing is the reversible direction applies: `Bind[T]` is additive, so not shipping it costs nothing later.
That rule breaks ties, and this is not a tie.
It also cuts the other way here, because the driver-side shape it forces is *not* additive: see the next section but one.

**Two things a held binding is not.**

It is not a compiled-schema handle.
`Binding[T]` exposes no address set, no field list, no defaults and no error set, so ADR-0001's read-only schema view stays closed and nothing in this ADR reopens it.

It is not a second way to supply a source.
A `Source` is a positional argument of an interface type to either verb, and no driver type doubles as an Option, which is how ADR-0004 closed the same item.

**`Bind` freezes the registry and `Compile` does not**, which is ADR-0010's rule with a new caller and no new wording, verified:

```
after Compile[T](WithRegistry(r1))    -> frozen=false
after Bind[T](src, WithRegistry(r2))  -> frozen=true
```

ADR-0010 states the rule as *a call that retains a compiled schema freezes the registry; a call that discards one does not*.
`Bind` retains one for the life of the binding, so it freezes, and ADR-0009's one broken shape, a load during `init`, is unchanged.

#### It applies to Dump too, and an earlier draft of this ADR said the opposite

> `BindSink[T](sink, opts...) (*SinkBinding[T], error)` is the same split on the write side, and `Dump[T]` is it with the handle discarded.

**This reverses a finding that was in this ADR when it was first opened for review, and the reversal is the more instructive half.**

The draft's audit probe observed that the prototype's `Dump` binds the sink **after** the walk, with the realised address set, and measured one type producing three of them:

```
Limits=map[]                  Tags=[]    ->  [/limits /name /tags]
Limits=map[rps:1]             Tags=[]    ->  [/limits/rps /name /tags]
Limits=map[burst:2 rps:1]     Tags=[x]   ->  [/limits/burst /limits/rps /name /tags#0]
```

From that it concluded that a sink binding is not hoistable, and named `members` and ADR-0004's enumeration asymmetry as the reason.

The measurement is right about the prototype and the conclusion does not follow, because **the prototype was not doing what ADR-0004 says.**
ADR-0004's own section is titled "The address set handed to `Bind` is the static set, and core hands back a key function", and its worked example for that rule is a **dump**:

> Measured with a static set of `{/name}`, dumping a `map[string]string` field to the KV driver: `Set(/labels/env)` returned `kv: address not in the opened set: /labels/env`.

Its fix is that the driver **mints** the dynamic address at the write it belongs to, not that core binds later.
Binding the sink with the realised set was a prototype shortcut that quietly made ADR-0004's dynamic tier unreachable on the write path, and the draft read that shortcut as a property of Dump.

Corrected, and the prototype's `Dump` rewired to bind with the static set: one sink binding, three dumps of three different shapes, all three written.

```
Name=a  Limits=map[]                  ->  [LIMITS NAME]
Name=b  Limits=map[rps:1]             ->  [LIMITS_RPS NAME]
Name=c  Limits=map[rps:2 burst:3]     ->  [LIMITS_BURST LIMITS_RPS NAME]
```

The static table holds `/name` and `/limits`; every `LIMITS_<key>` is minted at its own `Set`.
The rewiring is behaviour-preserving: ADR-0010's eleven probes and ADR-0009's seventeen produce identical output afterwards, apart from timings and one line of [#31](https://github.com/onhotpath/ferry/issues/31)'s known map-key collision, which flips run to run on the same binary either way.

**So both directions bind with a set that is a pure function of the schema**, and both bindings are hoistable for one reason rather than two.
What stays asymmetric is what ADR-0004 already said is asymmetric: Load reads dynamic addresses out of the plane through `Enumerator`, and Dump reads them off the value.
That is a fact about where a dynamic address comes from, not about when the sink is bound.

Measured, the same six-field struct written to a flat KV sink:

| | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `ferry.Dump`, binding per call | ~7300 | 4298 | **171** |
| `b.Dump(ctx, v)`, binding held | ~5200 | 3054 | **133** |

**And the context rule is not Load-only either.**
A sink whose plane is per request, a response buffer or a per-request recorder, reads it from the context exactly as a source does, and refuses at open when it is absent.
Run, with the same sink bound once:

```
with a store in the context  ->  [LIMITS NAME]
with none                    ->  err = kv: no store in the context
```

### The plane arrives in the context, because it is the only per-load channel there is

> A driver whose plane instance is per request takes it from the `context.Context`, using its own unexported key and its own exported constructor.
> Core supplies no mechanism, names no key, and has no plane parameter anywhere.

**This is a structural result and not a preference.**
ADR-0004's per-load call chain is `open(ctx) -> Reader` and then `Get(ctx, addr)`.
`ctx` is the only value a caller supplies that reaches a driver after `Bind`.
Every other mechanism has to change ADR-0004's signatures or manufacture a second key path, and both are measured below.

The ticket's objection to this option is that "the plane is the thing being operated on, not request-scoped metadata travelling alongside it".
Three answers, and the ADR does not pretend the objection has none.

**The refusal lands where the contract already puts it.**
The second half of the objection is that a missing value is a runtime error where every other refusal lands at compile time or at bind.
ADR-0004 designed `Bind` to take no context precisely so that "Bind must succeed against an unreachable plane" is assertable, and it puts every plane-reachability failure inside `open`.
A missing per-request plane *is* the plane being unreachable, so it lands in the place that already exists, per load, wrapping ADR-0011's `ErrPlane` with the driver's provenance marker.
It is not a new class and it is not a new moment.

**It composes, and composition is the query driver's own headline case.**
Precedence over a per-request plane, query parameters beating a file, is exactly what ADR-0004's `FirstOf` is for.
Measured, with `FirstOf` verbatim from ADR-0004 and not one line changed:

```
FirstOf(query, yaml), no query parameter       ->  {Name:svc}
FirstOf(query, yaml), ?name=from-query         ->  {Name:from-query}
```

The context reaches the query child because `FirstOf`'s own open already passes it to every child's open.

**In this one case the plane genuinely is request-scoped data crossing an API boundary**, which is what `context`'s documentation is about, rather than an optional parameter, which is what it warns against.
The value is mandatory, and its absence is loud.

#### The alternative that is type-safe, written out and measured

The ticket's option (c) is a type parameter on `Source`, which ADR-0004 rejected.
There is a shape the ticket did not enumerate, and it is strictly better than (c): move the type parameter off `Source` and onto ferry's own entry point, so the driver implements an **optional** interface discovered by assertion, in ADR-0004's own style.

```go
type PlaneSource[P any] interface {
    BindPlane(*AddressSet) (func(context.Context, P) (Reader, error), error)
}

func BindPlane[T, P any](src Source, opts ...Option) (*PlaneBinding[T, P], error)
```

It compiles and it runs.
`P` is bound at the caller's call site, which is what makes the assertion to a generic interface legal at all, and `Source` is untouched, so no ordinary driver writes `P = struct{}` and no ordinary driver's signature changes.
The call site is better than the context's:

```go
f, err := b.Load(ctx, r.URL.Query())    // url.Values, checked by the compiler
```

and the caller cannot forget, because omitting the argument does not compile.

**It loses on composition, and ADR-0004's objection survives the move.**
`FirstOf` is a `Source`, so it has no `BindPlane`.
Giving it one means a `FirstOf[P]` whose children must all agree on `P`, and the motivating combination is a query source and a file, where `P` is `url.Values` for one child and nothing for the other.
There is no `P` to write.

That is the same objection ADR-0004 recorded against putting the parameter on `Source`, and moving the parameter to the entry point does not answer it, because the thing that cannot be typed is the combinator and not the driver.

It is the runner-up and it is recorded as one.
If [#20](https://github.com/onhotpath/ferry/issues/20) or a later ticket finds that combinators over per-request planes are not wanted, this shape is available and additive.

#### And option (b) is not a driver decision at all

The ticket's option (b) is "a driver-specific constructor plus a caller-facing `LoadFrom(ctx, Reader)`", with the objection that the driver then has two public shapes.
That objection is true and it is not the binding one.

A `Reader` cannot exist without the key table, and core's helper takes the address set:

```go
NewKeys(a *AddressSet, name string, f KeyFunc) (*Keys, error)
```

The address set is a field of the compiled schema.
ADR-0001 leaves "whether core ever exports a read-only schema view" open and says to reopen it only if a concrete need survives the recording-sink pattern; ADR-0010 declined to reopen it.
**So option (b) is not a second shape on the driver, it is a request to export the compiled schema**, and it is refused on ADR-0001's terms rather than on this ADR's.

The version that needs no address set is the one that derives the key per lookup, and that is the thing ADR-0003 measured and called a requirement rather than an optimisation:

| | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| precomputed table, one `Get` | 13 | 0 | 0 |
| derived per lookup, one `Get` | 160 | 72 | 5 |

It also never sees a set, so it checks one key at a time against nothing, and ADR-0003's injectivity obligation is silently not discharged.

And if a driver ships both anyway, the two key paths are related by nothing.
Measured, with one option flipped on one of the two paths:

```
/db/host   Source path -> "db.host"    constructor path -> "db_host"
/db/port   Source path -> "db.port"    constructor path -> "db_port"
```

Same driver, same request, two answers, no error from either.
That is ADR-0010's duplication axis 1, the compiler against the walk, arriving inside a driver instead of inside core.

### A driver with a per-request plane has one public shape, and that is 5.14's first item

This is the item this ticket was most likely to commit, so it is stated as a rule rather than left to follow from the sections above.

> A driver whose plane instance is obtained freshly per load takes it **only** from the context.
> It does not also carry it in a field.

Without that rule, `query.Source{Values: v}` and `query.Source{}` plus `query.WithValues` both exist, and xload's defect is reproduced exactly: two ways to supply one thing, nothing saying which, and a precedence rule to write when both are given.

The discriminator is not "is this field configuration".
It is: **does the caller obtain this value freshly for each load?**
A file path, a separator, a KV client and a prefix are all constructed once and are fields.
`r.URL.Query()` is obtained per request and is not.

**Which of the two shapes survives is decided by a strict-superset argument and not by taste.**
The context form serves both call sites, measured:

```
b.Load(query.WithValues(ctx, r.URL.Query()))                             -> {Q:widgets Page:3 ...}
ferry.Load[Filter](query.WithValues(ctx, r.URL.Query()), query.Source{}) -> {Q:widgets Page:3 ...}
```

The field form serves only the second.
That is the same shape of argument ADR-0010 used for `Load` against `LoadOver` and ADR-0009 used for its default registry: one thing, and its sugar or its subset, never two paths.

**The cost is real and is stated rather than sold.**
`ferry.Load[Filter](query.WithValues(ctx, r.URL.Query()), query.Source{})` reads worse than `ferry.Load[Filter](ctx, query.Source{Values: r.URL.Query()})` would.
A caller who never holds a binding pays a call-site wart for a saving they do not collect.
That is the price of one shape, and it is judged worth paying because the alternative is the defect the survey names first.

**One limitation, because it is a consequence and not an oversight.**
One context key is one plane instance per driver package per load, so two query-parameter planes in one load are not expressible without the driver exporting a keyed constructor.
Two of them in one load is exotic, the remedy is the driver's, and it is recorded here rather than discovered later.

### ADR-0004's key helper is amended: the minted set belongs to the open

No interface in ADR-0004 changes.
One sentence in it does, and it is the sentence a held binding gives a second reading.

ADR-0004: "A key function serves the static tier from the precomputed table and mints a dynamic address on demand, running ADR-0003's legality and injectivity checks against **everything already issued**."

Under `Load[T]` alone, "already issued" means issued by this load, because the key function is created by `Bind` and dies with the call.
Nobody decided that; it fell out of the entry point.
Hold the binding and the same words mean *issued since the process started*.

> The **static table** is the bind's, immutable, and lock-free to read.
> The **minted set** is the **open's**, created per load, and checked against the static table and against everything this load has minted.

**Injectivity is a property of one write.**
Two writes to one plane at different times are not required to be mutually injective, and requiring it produces a refusal with no defect behind it.

Measured on ADR-0004's own env transform, where `http-port` and `http_port` are one plane key, with each write holding only one of them:

```
retained across writes:   write 1 -> "LIMITS_HTTP_PORT"
                          write 2 -> REFUSED, not injective: /limits/http-port and /limits/http_port
minted set per write:     write 1 -> "LIMITS_HTTP_PORT"
                          write 2 -> "LIMITS_HTTP_PORT"
```

**The case is Dump's, and now that `BindSink[T]` exists it is reachable through the public API rather than only through the helper.**
Two dumps through one held sink binding, each value holding one of the two map keys, and neither colliding with itself:

```
minted set on the binding   [LIMITS_HTTP_PORT NAME]   REFUSED
minted set on the open      [LIMITS_HTTP_PORT NAME]   [LIMITS_HTTP_PORT NAME]
```

A caller who holds a sink binding and dumps a map twice gets a refusal naming an address no plane still holds.
That is the amendment's whole justification, and it was a helper-level curiosity until the draft's Dump finding was reversed.

**Load inherits the growth rather than the refusal, and the ADR does not claim the wider version.**
A minted address comes from the value on Dump and from the plane on Load, so on Load two loads' minted addresses come out of one plane's key space and a driver whose enumeration round-trips cannot produce the refusal above.
An earlier version of this probe claimed it on Load, on a fixture that refused for a different reason entirely, and the claim did not survive being run.

What Load does inherit is all of the growth.
Measured through the entry point, 20000 requests each carrying one distinct map key, through one binding:

```
minted set on the binding   20000 tenants loaded, 20000 addresses retained
minted set on the open      20000 tenants loaded,     0 addresses retained
```

and on the live heap, 10000 minted addresses cost **1812 KiB**, none of it evictable.

That is the class ADR-0009 measured for a per-call registry and ADR-0010 restated as a property of the cache, arriving in a third place.
The rule ferry already applies is that an unbounded non-evictable cache may not hang off a long-lived value, and this is the same rule at the key table.

**And this is a latent defect rather than one this ticket creates.**
[#13](https://github.com/onhotpath/ferry/issues/13) will bind once and open many inside a watcher whether or not a caller-facing binding exists, so the amendment is owed even if `Bind[T]` had been refused.

### The presence observation is a `Source`, and core spells nothing

ADR-0006 fixed that the observation survives the walk and left three candidate spellings here.
The answer is none of the three.

> One `Source` wrapping another observes every boundary `Value` the load saw, including `Absent`.
> Core exports no Option, no callback and no report.

The whole mechanism is a wrapper whose `Reader` records what it is asked, and it uses no API a driver author does not already have.
ADR-0006's own table, reproduced through both an Option callback and a wrapper:

| plane | struct | Option callback | `Source` wrapper |
| --- | --- | --- | --- |
| `/db/port = 5432` | `{Host:h Port:5432}` | `number("5432")` | `number("5432")` |
| `/db/port` deleted | `{Host:h Port:0}` | `absent` | `absent` |
| `/db/port = 0` | `{Host:h Port:0}` | `number("0")` | `number("0")` |

Rows two and three are one struct and two observations, which is the whole feature, and both spellings deliver it.

**They see the same set, and the reason is structural**: the walk reaches the plane through exactly one call, `Reader.Get`, so an Option's callback is a hook on that call and a wrapper is that call.
Measured, the two address lists are identical.

**The wrapper answers a question the Option cannot.**
Under a `FirstOf` the Option reports the composed answer, because that is all the walk ever sees.
A wrapper goes where it is put:

```
query child  at /name -> string("from-query")
yaml  child  at /name -> absent
the Option   at /name -> string("from-query")
```

"Which layer answered" is the question ADR-0001 milestones drift detection on, and only the wrapper can be asked it.

**Cost**: 4 extra allocations for the Option and 9 for a locked wrapper, on a 139-allocation load, with the time difference inside this machine's run-to-run noise.

**Two consequences, both stated because they are not free.**

ADR-0010's rule for what may join the cache key needs a row on both sides, and `Observe` was its only load-affecting example.
Withdrawing it means **every Option ferry has today is compile-affecting**, which is a coincidence of the order the tickets landed in rather than a design property, and ADR-0010's rule is what stops it being read as one.
The rule is unchanged and its example is now hypothetical.

Under a concurrent walk the recorder is written from several goroutines, so it needs a lock.
That is true of a callback closure too, so the wrapper is not worse, and it is [#20](https://github.com/onhotpath/ferry/issues/20)'s to say whether the walk ever does that.

### `Compile[T]() error` stands, because a binding is not a schema

ADR-0010 recorded that `Compile[T]() error` diverges from `regexp.Compile`'s shape knowingly, that parse-don't-validate asks for the artefact, and that "if #25 ever exports a handle this is the function whose signature changes".

It does not change.
What this ADR exports is a handle on **a schema bound to a plane**, not on a schema.
`Binding[T]` cannot answer any question about `T`: it has two methods, both of which need a context and a plane, and it exposes no address set and no field view.
So there is still nothing for a parse to hand back, ADR-0001's read-only schema view stays closed, and the divergence ADR-0010 took knowingly stays taken for the reason it gave.

The break ADR-0010 priced against v0 is therefore not spent.

### One thing core gives back for free, with no API attached

Measuring the bind's share of a load turned up a cost that is neither the bind's nor anybody's API.

`Load` was rebuilding the `*AddressSet` handed to `Bind` on every call, from a slice that never changes:

| | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `NewAddressSet`, per load | ~1230 | 656 | **39** |

It is a pure function of the compiled schema, so it is built once at compile and held there.
That took a per-request load from 125 allocations to 85 before either shape in this ADR was measured, and it means the numbers above attribute to `Bind` only what `Bind` costs.
ADR-0004's headline 158 against 2743 was measured on a source-only harness with no schema and no walk behind it, so this cost was in neither ADR's numbers and neither had the whole-load figure.

### What this hands the tickets that were waiting

**[#20](https://github.com/onhotpath/ferry/issues/20), concurrency.**
Nothing about the walk is decided here, and one fact is handed over: a binding is now shared mutable state across requests, which under `Load[T]` alone it never was.
Measured, 64 concurrent loads through one binding, clean under `-race`, both with a static schema and with a live dynamic tier.
What makes that true is the amendment above, plus ADR-0004's own two-tier split: the static table is written once and read without a lock, and the minted set is per open and therefore per goroutine.
A driver's own `OpenFunc` closure must be safe for concurrent calls, and that is a driver obligation this ADR creates and the conformance suite has to cover.

**[#13](https://github.com/onhotpath/ferry/issues/13), watch and reload.**
The bind-once/open-many shape #13 wants is `Binding[T]`, and `LoadOver` on it is ADR-0006's reload carried over explicitly.
The key-helper amendment is owed to #13 whether or not this ADR had exported anything, because a watcher binds once by construction.

**[#35](https://github.com/onhotpath/ferry/issues/35), what `ferrytest` exports.**
Three obligations, all new.
A conformance case that a driver reading its plane from the context refuses at open when it is absent, and refuses with `ErrPlane`.
A case that a driver's key function retains nothing across opens, which is the amendment stated as a test rather than as prose, and it has to run on the **write** side, because that is where the retention refuses a legal write.
And a case that a sink accepts a dynamic address its static table never held, which is ADR-0004's own `Set(/labels/env)` example promoted from a paragraph to a test - the prototype's `Dump` silently made it unreachable for as long as it bound with the realised set, and nothing caught that.

**[#5](https://github.com/onhotpath/ferry/issues/5) and ADR-0004.**
The sentence ADR-0004 closed on is discharged: the per-request use case has a worked answer, and `query`'s claim on the first-party list is stronger than ADR-0004 left it, because it is now the only driver exercising the context-supplied plane axis as well as the per-request one.
ADR-0004's driver table gains a row, "supplies its plane at open rather than at construction", which `query` owns and no other first-party driver has.

### An inherited claim that did not reproduce

Auditing this ADR's own strongest claim turned up one belonging to another.

ADR-0010 prints a two-row table for what `LoadOver` returns on failure and calls it measured.
Its probe's zero-reading row calls a helper that returns the **seed**, so as committed both rows print the seed and the table's first row was never produced by anything.
Re-run with a variant that actually returns the zero value:

```
the seed reading -> {Name:db1}
the zero reading -> {Name:}
```

**ADR-0010's decision is unaffected and its published row is exactly what a correct probe produces.**
What was not measured is the row itself, and it is recorded here because the effort's own standard is that an assertion that was never run is not a finding.

### What this ADR does not decide

- **Whether the walk may run concurrently, and what a scheduler may assume**: [#20](https://github.com/onhotpath/ferry/issues/20).
- **The watch and reload API**: [#13](https://github.com/onhotpath/ferry/issues/13).
- **What `ferrytest` exports**: [#35](https://github.com/onhotpath/ferry/issues/35).
- **Whether any combinator ships in core**, including the observing one: ADR-0001's bucket rule, when one is proposed.
- **Whether `BindPlane[T, P]` ever ships.**
  It works, it is type-safe, and it does not compose. Refusing it is the reversible direction and it stays available.
- **Whether core ever exports a read-only schema view.**
  ADR-0001 left it open, ADR-0010 declined to reopen it, and this ADR is the ticket ADR-0010 named as the one that might.
  It does not: a binding is not a schema.

## Consequences

- **ferry ships a caller-held binding, and the deciding argument is the ancestor.**
  Binding per load, ferry allocates 85 times per request against xload's 22 on the use case xload was pitched at; a held binding brings it to 45.
  The ergonomic argument was not decisive in either direction and the compile-cost argument was already gone, removed by ADR-0010 before this ticket opened.
- **`Load[T]` is `Bind[T]` with the handle discarded and `Dump[T]` is `BindSink[T]` with the handle discarded, in the implementation and not only in the prose.**
  That is what keeps 5.14's first item closed on the verbs: nothing is expressible through one and not the other, and the two cannot drift because there is one code path.
- **The whole surface is additive, and the compiler says so rather than the ADR.**
  The three worked programs, written without any binding, compile against a generated ferry that exports `Load`, `LoadOver`, `Dump` and `Compile` and nothing else, and a negative control using one binding call fails with `undefined: ferry.Bind`.
  A user who never holds a binding writes exactly what they wrote before this ADR.
  **One thing is not additive and it is not the ferry API**: the driver convention below would not have been chosen by a ferry with no binding, so the surface is additive and the convention is a decision.
- **The split exists in both directions**, `Bind[T]` over a `Source` and `BindSink[T]` over a `Sink`, because ADR-0004 hands a **static** address set to both and a driver mints the dynamic ones at the write they belong to.
  An earlier draft of this ADR refused the sink half, on a probe that measured this prototype binding the sink with the realised set - a shortcut that made ADR-0004's own dynamic tier unreachable on the write path.
  Correcting the prototype reversed the finding, and the reversal is recorded in full because the draft had read a prototype shortcut as a property of Dump.
  Two functions rather than one, because `Source` and `Sink` are separate types and Go has no overloading, which is the visible cost of ADR-0004's compile-time read-only refusal reaching this surface too.
- **A per-request plane travels in the `context.Context`, and core supplies no mechanism for it.**
  ADR-0004's contract has exactly one per-load channel, so every alternative either changes its signatures or manufactures a second key path.
  The cost is that a mandatory input is not in a signature, and the mitigation is that its absence is loud, per load, at the moment the contract already reserves for an unreachable plane.
- **A driver with a per-request plane has one public shape and it is the context one.**
  This is the ADR's least comfortable decision, because it makes the simplest query-parameter call site read worse than it would with a `Values` field, and because it is the one place the binding's existence reaches back into code this ADR does not own.
  It is taken because the two-shape alternative is the survey's first item verbatim, and because the context shape serves both call sites while the field shape serves one.
  It applies to a sink with a per-request plane in exactly the same way, verified.
- **One context key is one plane instance per driver package per load.**
  Two query-parameter planes in one load need a keyed constructor from the driver, and core will not grow one.
- **ADR-0004's key helper is amended, and no interface changes.**
  The static table is the bind's and the minted set is the open's, because injectivity is a property of one write.
  Without it a held sink binding refuses the second of two dumps that each hold one of two map keys the plane's transform folds together, through the public API; and a held source binding retains one address per distinct map key ever seen, measured at 20000 retained and 1812 KiB per 10000.
  The amendment is owed to [#13](https://github.com/onhotpath/ferry/issues/13) independently of anything this ADR exports.
- **The presence observation is a `Source` wrapping a `Source`, so ADR-0006's three candidate spellings are all declined and core exports nothing for it.**
  It is strictly more expressive than an Option, because it can be put on a child of a combinator and answer which layer supplied a value, which is the question ADR-0001 milestones drift detection on.
  The cost is that ADR-0010's Option rule loses its only load-affecting example, so every Option ferry has is now compile-affecting, and the ADR says that is a coincidence rather than a property.
- **`Compile[T]() error` stands and the v0 break ADR-0010 priced is not spent**, because a binding is a handle on a schema bound to a plane and answers no question about the type.
- **A binding is shared across goroutines and a driver's `OpenFunc` and `OpenWriterFunc` must be safe for concurrent calls**, which is a new driver obligation and a new conformance case.
  It is clean under `-race` in the prototype for the drivers there, and [#20](https://github.com/onhotpath/ferry/issues/20) inherits the fact rather than a design.
- **Core stops rebuilding the address set on every load**, taking a per-request load from 125 allocations to 85 with no API attached, found while separating the bind's cost from the load's.
- **The timings in this ADR move by up to 40 per cent between runs of the same binary and the allocation counts do not move at all**, so the allocation columns are the measurements and one single-run claim about parallel throughput was withdrawn rather than published.

## Items from the xload survey

The survey is [`docs/research/generics-and-modern-go.md`](../research/generics-and-modern-go.md), section 5, against `github.com/gojekfarm/xtools` at [`a90b3aa`](https://github.com/gojekfarm/xtools/commit/a90b3aad2133248cec50f6b4d6e37b0d9e788adb).

**5.14** was enumerated rather than assumed, all four items.
The first is this ticket's most likely defect and it is answered in three places rather than one.

- *Two ways to set the loader.*
  **This ADR's, on three surfaces, and it is the reason two of its decisions read the way they do.**
  On the **source**: unchanged from ADR-0004, one way, a positional argument of an interface type, and no driver type doubles as an Option.
  On the **verbs**: `Load[T]` is `Bind[T]` plus the method with the handle discarded and `Dump[T]` is `BindSink[T]` plus the method, in the implementation, so each direction has one code path and nothing is expressible through one spelling and not the other.
  The compiler check above is the same item from the other side: a program that uses neither binding compiles against a ferry that has neither, so the second way is not a second way a user has to choose between, it is a value they may keep.
  On the **driver**: this is where the exposure is new, because a per-request driver could carry its plane in a field *and* read it from the context.
  It is closed by a rule that a driver has one of the two and it is the context, argued on the context form serving both call sites and the field form serving one.
  The ticket predicted this defect against option (b) specifically, and B3 found that option (b) never gets far enough to commit it: it needs the compiled schema's address set exported, which ADR-0001 keeps closed.
- *The `CanAddr` loop that can only run once.*
  A defect in the reflection walk, and ADR-0010 records that ferry's walk owns addressability in exactly two places and neither is a loop.
  This ADR adds no walk and moves neither place.
  It does not apply.
- *The non-deterministic select on a cancelled context.*
  Bears on this ADR once and not in the way it bears on the others.
  This ADR puts a required value **in** the `context.Context`, so a driver's `open` now reads the context for two different reasons, a plane and a cancellation.
  They do not race: the plane is read with `ctx.Value` before any I/O and the cancellation is checked by the walk, which is ADR-0010's placement and [#20](https://github.com/onhotpath/ferry/issues/20)'s to change.
  What ferry does when a cancellation and a driver error race stays #20's, and this ADR neither fixes nor worsens it.
- *Value receivers on `Error()` where pointers are returned.*
  Closed by ADR-0011 by construction, and this ADR produces no new error type.
  The one refusal it introduces, a per-request plane that is not in the context, is an existing class, `ErrPlane`, wrapped by the driver, so 5.14's fourth item has nothing new to bite on.

**5.13, the per-key pull model amplifies backend round trips.**
Touched, and the direction is the same as ADR-0004's.
A held binding does not change how many backend calls a load makes, because batch versus lazy is still a branch inside one driver behind `OpenFunc`.
What it removes is the per-load reconstruction of the key table, which is not a round trip and which ADR-0004's own numbers had bundled with it.

**5.3, no schema caching**, is ADR-0010's outright and is the reason this ticket is smaller than it opened.
Its measurement is what left the bind alone in scope, and this ADR adds one thing to it: `Load` was rebuilding the address set per call on top of the cached schema, at 39 allocations of its own, which is 5.3's own defect surviving one level up from the thing that fixed it.

**5.1, the `Loader` signature cannot express absence**, is ADR-0004's and ADR-0006's.
It surfaces here because the presence observation is only expressible at all because absence is a kind: a wrapping `Reader` records `Absent` as a value, and nothing about the wrapper would work if a miss and an empty string were one observation.

**5.12, `SerialLoader` precedence is unexpressible**, is ADR-0004's.
This ADR is where its replacement is exercised against a per-request plane: `FirstOf(query, yaml)` runs unchanged under a held binding, because the context reaches every child's open.

The remaining items are unaffected by this ADR.
