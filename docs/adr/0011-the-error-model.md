# 11. The error model

Status: Proposed
Date: 2026-08-02
Ticket: [#9](https://github.com/onhotpath/ferry/issues/9)

## Context

This is the ticket every other ADR deferred an error type to, in the same sentence: "[#9](https://github.com/onhotpath/ferry/issues/9)'s convention, applied rather than invented".
[ADR-0003](0003-how-a-leaf-addresses-a-plane.md), [ADR-0004](0004-source-and-sink.md), [ADR-0005](0005-the-supported-type-set.md), [ADR-0006](0006-defaults-and-zero-values.md), [ADR-0007](0007-the-codec-chain-and-its-precedence.md), [ADR-0008](0008-the-struct-tag-grammar.md) and [ADR-0009](0009-typed-codec-registration.md) all defer here, across twenty-three sites.

So the first job is not the ticket body.
It is to enumerate that union and check one model against every row, because six ADRs are already written as though this convention exists.

Several of them fixed **behaviour** that this ADR applies rather than re-decides.
ADR-0001 makes determinism a package-wide invariant.
ADR-0003's collision reports are joined and sorted, measured at one distinct error string over 300 runs.
ADR-0004 uses `errors.Join` and sorted reports, and makes `ErrReadOnly` a sentinel drivers wrap.
ADR-0005 collects, joins and sorts its refusals, each naming an address and a type.
ADR-0006's refusals are per address, and it states that "whether a `required` failure stops the walk or is aggregated is #9's, and this ADR assumes aggregation without depending on it".
ADR-0008 hands over one shape explicitly: **its three diagnostic tiers are a suppression order.**

The survey is `docs/research/generics-and-modern-go.md`, section 5.
Three items are this ticket's outright: **5.4**, first error only with the concurrent path disagreeing about how many errors exist; **5.5**, nondeterministic error output; and **5.14**'s fourth item, value receivers on `Error()` where pointers are returned.

The ticket's comment adds a concrete shape: an aggregate implementing `Unwrap() []error` **and** `fmt.Formatter`, sorted by key, never the nested pairwise tree, pointer receivers on `Error()`.
It names the cost: aggregating means continuing past a failed field, so the destination is partially populated, "which needs documenting, and probably a `StopOnFirstError` option".

This ADR is written from a throwaway prototype on branch `proto/9-errors`, which never merges.
It is built on `proto/11-tag-grammar`, so every measurement runs against a real `Path`, a real `Value`, the real type set, the real tag grammar and the real YAML driver over real files.
Fifteen probes.
**One found a defect in a decision this ADR had already made, and four more changed a rendering the ADR had already written down.**

## Decision

### What this closes, and what it does not

The ticket asked for five things by name.

| The ticket asked | Closed | Where |
| --- | --- | --- |
| what the type system carries, and what is left for errors to report | **yes**, and the census is the answer | [One model](#one-model-and-the-census-that-checks-it) |
| how an implementor adds plane-specific detail without losing ferry's context | **yes**: core owns three fields, the driver owns the class and the cause | [Extension](#extension-a-driver-holds-an-opinion-about-the-class-and-about-nothing-else) |
| aggregated or first-one-fatal | **yes**, aggregated, in both directions | [Aggregation](#aggregation-one-rule-at-every-moment) |
| what context an error carries: key, path, raw value, field name, direction | **yes**, four things, and **never the raw value** | [What an error carries](#what-an-error-carries-and-the-one-accessor) |
| how a caller matches a kind without depending on concrete types | **yes**: there is no concrete type to depend on | [The vocabulary](#the-vocabulary-four-classes-and-a-provenance-marker) |

Six questions this ADR had to answer that the ticket did not name.

| Not asked for, answered anyway | Where |
| --- | --- |
| whether one model covers the union six ADRs deferred here | [One model](#one-model-and-the-census-that-checks-it) |
| where sorting happens, given that `errors.AsType` reads the tree | [Sorted at construction](#the-aggregate-is-flat-and-sorted-at-construction) |
| what the sort key is, once a `Close` failure can arrive beside walk errors | [The three-part key](#the-three-part-key-moment-then-location-then-message) |
| whether ferry may ever call `errors.Join` | [One aggregate constructor](#ferry-has-exactly-one-aggregate-constructor) |
| what `Load` hands back when it fails | [Load yields no value](#on-failure-load-yields-no-value) |
| what a test asserts against, once message text is not API | [What ferrytest gets](#what-ferrytest-gets-and-what-35-owns) |

**Three things this ADR does not close.**

- **Two of the fifty-five refusals are not expressible by this model**, and both are named rather than absorbed.
- **A dynamic address segment is plane-supplied and is printed**, so the no-plane-text rule is about values and not about everything the plane supplied.
- **A driver can misdeclare its class**, and nothing checks it.
  It is a conformance case, in the same family as ADR-0004's three optional interfaces.

### One model, and the census that checks it

> ferry has one error model.
> The **moment** a failure happened is a field on it, not a type split.

The alternative was a family split at the line that matters most: a schema error is provable from `reflect.TypeFor[T]()` alone and should fail in a unit test, and a walk error is a deployment fact an operator reads.
That line is real and this ADR keeps it, as a class rather than as a type.

The reason is that the aggregate, the location, the sort and the formatter are identical work at every moment, and ADR-0001 already made "read the error set" a **feature** rather than diagnostics: deployment validation is `Load` followed by reading the error set, and schema extraction is a dump into a recording sink.
A caller doing `Load` also cannot avoid handling both, because a schema failure surfaces through the same call as a missing key.

**The census is what checks that claim rather than asserting it.**
Every refusal an accepted ADR measured or required, classified by moment and class:

```
55 refusals enumerated, from 8 ADRs

by moment:            by class:                    carrying the Driver marker:  9
  register    2         schema      33             carrying a location:        45 of 55
  compile    30         missing      2
  bind        2         value        8
  open        3         plane       10
  walk       14         (none)       2
  commit      1
  close       1
  unknown     2
```

Two numbers are worth reading out.
**Thirty of fifty-five are schema compile**, and **thirty-three of fifty-five are one class**, which says that the large majority of what ferry can refuse is a programming defect catchable by `Validate[T]()` with no plane in sight.
That is the argument for the class vocabulary below being drawn where ADR-0008 already drew it.

**Two rows do not fit, and they are recorded rather than absorbed.**

- **A half codec pair is a build error**, not an error value.
  ADR-0009's generic inference refuses it before anything runs, so the model is never asked.
  That is a result rather than a gap.
- **The tag-key Option is validated where the Option is supplied**, not at schema compile, so it belongs to no moment in the list.
  It is a mistake at the call site, which ADR-0008 decided deliberately.

### The type: one exported name, no exported fields

```go
type Error struct{ /* no exported fields */ }

func (*Error) Error() string
func (*Error) Unwrap() error
func (*Error) Format(fmt.State, rune)
func (*Error) Address() Path
```

**The name is exported so `errors.AsType` works; no field is, so the struct can grow.**
Verified on `go1.27rc2` through both wrapping forms at once, an aggregate inside a `fmt.Errorf("%w")`:

```
errors.AsType[*ferry.Error](wrapped)   ok=true  addr=/db/port
```

This is the answer to the ticket's fifth ask in its strongest form: **there is no concrete type to depend on.**
A caller cannot build a `switch` over ferry's internals, because there are none to switch on, and the accessor set is what ferry commits to.

The cost is stated plainly: this is the least idiomatic of the three shapes available.
A Go user's first instinct is `var e *ferry.FieldError; errors.As(...)`, and here that does not compile.
The mitigation is that the accessor set is small and total, and that adding one later is additive rather than breaking, which a struct with exported fields is not.

**5.14 is closed by construction, and the prototype contains a live example of the defect.**
`Error()` is on the pointer receiver, so:

```
Error  (value)   implements error : false
*Error (pointer) implements error : true
```

xload declares `Error()` on the value and returns the pointer, so both forms satisfy `error` and the natural `var e xload.ErrRequired; errors.As(err, &e)` is a silent false.
Measured in ferry's own prototype, `unsupportedTypeError` had the value-receiver shape, which is how the defect arrives: not by choosing it, but by not choosing against it.

### The vocabulary: four classes and a provenance marker

```go
var ErrSchema, ErrMissing, ErrValue, ErrPlane error
var ErrDriver error
var ErrReadOnly error   // ADR-0004's, now a member of this family
```

A kind is a **decision**, not a description, so each class earns its place by being a different thing to do next.

- **`ErrSchema`** is provable from `reflect.TypeFor[T]()` plus the registry, with no plane in sight.
  It is defined as **what `Validate[T]()` can catch**, which reuses ADR-0008's line rather than drawing a new one.
- **`ErrMissing`** is the plane being silent at an address `required` names.
- **`ErrValue`** is the plane speaking and what it said not fitting the type.
- **`ErrPlane`** is ferry being unable to talk to the plane, or a driver refusing the address set.
- **`ErrDriver`** is **provenance**, and it crosses the four.

**`Missing` is split from `Value`, and it earns the word.**
ADR-0001 makes deployment validation a named consumer of the error set, and "these six keys are unset" and "these two hold garbage" are different messages for different people.
Collapsing them means the operator reads the list one line at a time to find out which is which.

**The static and dynamic halves of ADR-0003's one collision rule land in different classes, and that is honest rather than sloppy.**
Prefix-freeness is checked from the type, so it is `Schema`.
A colliding map key is minted from the value, so it is `Value`: it is the operator's map that collided.
Same rule, two tiers, two remediations.

**There is no `Transient` marker, and the reason is ADR-0001's own veto.**
Whether a Consul 503 is worth retrying is the driver's knowledge, and core "contains nothing that requires knowing the plane is a configuration file" applies just as hard to knowing what a backend's status codes mean.
Provenance is the thing ferry knows for certain, and it is the better proxy for the same decision: retrying an `ErrValue` is always pointless, and retrying a driver's `Get` is sometimes not.
The driver's own vocabulary stays reachable underneath, so a caller who wants the precise answer asks the driver rather than ferry.

**Cancellation gets no ferry class.**
`errors.Is(err, context.Canceled)` is the match, and a ferry class for it would be a second spelling of a stdlib one.

**The sentinel text is load-bearing, which was found by rendering rather than by choosing.**
A driver declares its class by wrapping a sentinel, so the sentinel's own text lands inside the driver's message.
`plane` read as a stray word in `...cannot contain a space: plane`; `plane error` reads as a sentence.

### Matching is `errors.Is`, and the aggregate keeps `Join`'s meaning

> The classification is matched with `errors.Is` and nothing else.
> There is no `Kind` enum and no `KindOf`.

This costs exactly one accessor, `Address`, because both axes are the same mechanism and `ErrDriver` therefore needs nothing extra.
It is also what the standard library does for this job, `fs.ErrNotExist` and `os.ErrPermission`, so it needs no teaching.
And ADR-0004 had already committed to `ErrReadOnly` as a sentinel drivers wrap, so this makes it a member of one family rather than an exception beside an enum.

**On an aggregate, `errors.Is` keeps `errors.Join`'s meaning: it answers "at least one element is of this class".**

An earlier draft of this ADR proposed departing from that, on the grounds that a caller asking "is this a schema error" gets true for a load where one field was misspelled and forty were fine.
The departure is not taken.
`errors.Is` traverses a tree and answers about the tree, and a convention that answers differently for ferry's aggregate than for `errors.Join`'s is a rule to teach and a rule to get wrong.
Counting is the caller's range loop:

```go
for _, e := range ferry.Elements(err) {
    if errors.Is(e, ferry.ErrMissing) { missing = append(missing, ferry.Address(e)) }
}
```

### What an error carries, and the one accessor

Four things: **location, moment, class, cause.**
Everything else is message text.

**The location is a `Path`, and it holds two different spaces.**
At schema compile it is the **Go field path**, because a field with no tag has no address, and the whole error is that it never named one.
Everywhere else it is the plane address.
ADR-0008's own measured diagnostic is `ferry: /Debug: field Debug carries no ferry tag`, where `/Debug` is a Go name, and two lines later `two fields address /name` is a genuine address.
This ADR states the rule rather than carrying two fields or pretending the space is always the same.

**No field name.**
Under ADR-0008 every mapped field names its segment explicitly, so at runtime the address is what the user wrote.
At schema compile the location already is the Go name.

**No direction.**
A caller who got the error from `Load` knows.
Schema compile is direction-agnostic by construction, so direction would be empty for the largest class, and the prototype found the case that would have justified it does not: what a driver failure wants is a **verb**, and the call site supplies one without the error storing it.
So ferry's own text for a driver failure is the moment in words, `opening the plane`, `closing the plane`, `the driver failed`, which also stops a location-less driver error rendering as the bare word `driver`.

**No accessor for the moment or the class.**
The class is `errors.Is`, and nobody branches on the moment.

### ferry never puts plane-supplied text in an error message

> ferry's own message text never contains a value the plane supplied.
> The cause stays in the chain and is not printed.

**This is the sharpest finding in the ticket, and the prototypes were leaking before it.**
Every ferry driver named in ADR-0004 is `yaml`, `kv` and `env`, and the charter names Vault and Consul, where **every value is a secret**.
`strconv.NumError` quotes its input unconditionally and `time.ParseDuration` puts the string in its message.

Measured, the same five leaves through a naive `fmt.Errorf("ferry: %s: %v", p, err)`, which is what the accepted ADRs quote:

```
ferry: /MaxConns: strconv.ParseInt: parsing "AKIAIOSFODNN7EXAMPLE": invalid syntax
ferry: /Timeout:  time: invalid duration "AKIAIOSFODNN7EXAMPLE"
ferry: /Ratio:    strconv.ParseFloat: parsing "AKIAIOSFODNN7EXAMPLE": invalid syntax
ferry: /Enabled:  strconv.ParseBool: parsing "AKIAIOSFODNN7EXAMPLE": invalid syntax
ferry: /Budget:   strconv.ParseInt: parsing "99999": value out of range

4 of 5 naive messages contain the plane's own text.
```

ferry cannot know which addresses hold secrets, because that would require knowing what the plane is for, which ADR-0001 forbids by name.
So the rule has to be **total**, and the same five through the model:

```
ferry: 5 errors:
  /Budget: is out of range for int8
  /Enabled: is not a valid bool
  /MaxConns: is not a valid int
  /Ratio: is not a valid float64
  /Timeout: is not a valid time.Duration

elements whose message contains the secret: 0 of 5
```

**The cause is reachable and is never printed, which is what stops redaction being a loss.**
Measured on the same five: `errors.Is(err, strconv.ErrRange)` and `errors.Is(err, strconv.ErrSyntax)` still answer correctly.
A caller loses nothing programmatic; only the log loses the secret.

**What ferry may name is structure, and what it may not name is the value.**
The observed `Value` kind, the target Go type, whether the failure was syntax or range, and an array's length are all structural and all printed.
That is the difference between `the plane holds null and int cannot take one` and `parsing "hunter2"`.

**The measured cost is that ferry has to author a message for every failure mode**, because it cannot pass a stdlib error through.
Five today, and every registered codec adds one that lands on the generic `is not a valid %s`.

**The carve-out, measured rather than assumed.**
A dynamic address segment comes from the plane too, and it appears in the location:

```
ferry: /Creds/prod~1db~1AKIAIOSFODNN7EXAMPLE: is not a valid int
```

A map key is a **name**, not a value, and ferry cannot name the address without it.
So the rule is about values, and this ADR says so rather than claiming a stronger property than it has.

**A driver's own error is printed, and the obligation not to put plane values in it is the driver's.**
That is the same shape as every other driver obligation in ADR-0004, and it is a conformance case rather than a guarantee.
Core's guarantee covers core's text.

### Extension: a driver holds an opinion about the class, and about nothing else

> A driver returns any error.
> Core wraps it and supplies the **address**, the **moment** and the **`ErrDriver` marker**, and a driver can change none of them.
> Core supplies the default class for that moment **unless** the driver's error already carries a ferry class sentinel, in which case core keeps it.

The driver's own error stays in the chain, so `errors.Is(err, consul.ErrThrottled)` and `errors.AsType[*consul.APIError]` both work through ferry unchanged.
Measured: a YAML driver's own syntax sentinel is matchable through ferry's wrapper, and ferry never had to know it existed.

**The class override is not decoration.**
A YAML source whose document does not parse is the operator's file, not the infrastructure, and only the driver knows that.
Core would have called it `Plane`; the driver declares `Value`:

```
plain error at Open : ferry: opening the plane: open /etc/app.yaml: no such file or directory
                      Plane=true  Value=false  Driver=true
declares Value      : ferry: opening the plane: invalid value: yaml: line 3: mapping values are not allowed...
                      Plane=false Value=true   Driver=true
```

ADR-0001 names 5.11, the YAML provider discarding parse errors, as the failure it rules out by architecture.
This is where that error lands with an honest class.

**`ErrorAt` is the one constructor ferry exports.**

```go
func ErrorAt(addr Path, err error) error
```

It exists because `Bind` refuses over a whole **set**: ADR-0004 puts injectivity in core's key-function helper, but **legality is the driver's**, and the driver is the only party that knows which address it disliked.

**It attaches and never classifies, which is what stops it being a second constructor**, and that is survey item 5.14's first entry checked rather than assumed:

```
ErrorAt alone is a *ferry.Error : false
ErrorAt alone matches any class : false
```

It is inert until core wraps it.
And where core already knows the address, core's wins, so a driver cannot misattribute a `Get` failure:

```
driver names /somewhere/else inside a Get at /db/host  ->  /db/host
```

It returns `error` and not `*Error`, which is what closes the typed-nil trap: there is no concrete return type to smuggle a nil through, so `return ferry.ErrorAt(a, f())` is safe to write.

**What a driver can still do wrong, stated rather than implied.**
It can wrap `ErrSchema` around an infrastructure failure and produce an error `Validate[T]()` would never have caught.
Nothing checks it.
Provenance and the address are core's and cannot be forged, so the blast radius is the class alone, and it is a conformance case.

### Aggregation: one rule at every moment

> ferry reports every failure that is not a consequence of another failure it is already reporting.

That one sentence is ADR-0006's admissibility-before-contradictions rule and ADR-0008's three tiers, stated once and extended to the walk.

**ADR-0008's suppression order and this ADR's aggregation do not conflict**, and it is worth saying so plainly because the handoff treated it as the ticket's hardest constraint.
The tiers decide **which errors come into existence**; aggregation decides **how many of those are reported**.
Suppression is upstream.
Measured through the real compiler, one field carrying two inadmissible options:

```
Origins []string `ferry:"origins,required,default=v"`   ->  2 errors, not 3
  ferry: /origins: []string is a composite, so it has no single address a default could sit at
  ferry: /origins: required is not available on []string: ...
```

The contradiction between `required` and `default` is not reported, because neither survived tier two.

**On Load, aggregate.**
Measured on five bad fields, which is 5.4 in ferry's own shape:

```
fail-fast   errors=1
aggregating errors=5
```

**On Dump, aggregate too, and the cost is real.**
Load's failures cost nothing outside the process and Dump's cost writes.
Measured on a twelve-address struct with a sink refusing two addresses:

| non-staging sink | attempts | written | errors |
| --- | --- | --- | --- |
| fail-fast | 3 | 2 | 1 |
| aggregating | 12 | 10 | 2 |

| staging sink, a `Committer` | attempts | staged | reaches the plane |
| --- | --- | --- | --- |
| fail-fast | 3 | 2 | **0** |
| aggregating | 12 | 10 | **0** |

Eight extra addresses of twelve is a bigger number than "a difference of degree" suggests, and the ADR records it rather than the framing.
Two things decide it anyway.
`Commit` runs only on success (ADR-0004), so on a sink that can stage, aggregation costs **nothing at all**.
And on a sink that cannot, fail-fast had **already written two** of them, so it does not protect the plane, it only writes less of it, on a sink whose model already accepts partial writes and where ADR-0006 already established that an omission is not a deletion.

**The alternative considered and refused: let the class decide, so an `ErrPlane` error stops the walk.**
The appeal is that a downed plane produces N copies of one fact.
Measured, and it is true:

```
(a) the plane dies at the third Get   6 errors from 8 addresses, 1 distinct underlying fact
(b) a token denied on two paths       2 errors from 8 addresses, 2 distinct facts
```

(b) is the Vault case ferry exists to serve: a token with read on some paths and not others.
Both are `ErrPlane` from the same driver at different addresses, and **no rule available to core tells them apart**.
Stopping on the first would make (b) unreportable in bulk to save (a) four duplicate lines.

**`StopOnFirstError` is not shipped.**
The survey recommends it "for callers who want the old behaviour", and ferry has no old behaviour.
It is a public knob whose only job is to make ferry report less, and ADR-0006 established that a **load-affecting** Option is cheap to add later, measured against the compile-affecting kind that lands in the schema cache key.
So not shipping it costs nothing and shipping it doubles the test matrix on every error path in the design.

### The aggregate is flat, and sorted at construction

**Flat, never nested.**
One aggregate at the top holding leaves.
The address already encodes the tree, so a `/db` aggregate holding a `/db/host` error is depth that says nothing, and 5.4's left-leaning pairwise tree becomes unrepresentable rather than merely avoided.

**ferry never nests ferry aggregates, and never rewrites a driver's tree.**
A driver's own joined error enters as **one** element with its internal shape intact.
ferry cannot attribute addresses to a third party's children, and rewriting somebody else's error tree is not ferry's business.
So the promise is precise: *ferry never nests ferry aggregates*, not *the result is always two levels deep*.

**Sorted at construction, not in `Format`, and this is the probe that pays for itself.**
`errors.AsType` returns the first match in **tree order**.
Measured over 300 runs of an identical three-error walk whose collection order is not deterministic:

| | what `AsType` picks | what it prints |
| --- | --- | --- |
| sorted at construction | **1 distinct** | 1 distinct |
| sorted in `Format` only | **3 distinct**, 29 / 44 / 227 | 1 distinct |

The second row is the finding: the printed form is stable and the programmatic reader is not, so 5.5 would look fixed and would not be.
ADR-0001's determinism invariant covers a user-visible artefact, and after this ADR the error set is one.

**One failure returns the leaf bare**, as `errors.Join` does, and `Elements` returns a **one-element slice** for it so the caller's loop reads the same whether one field failed or forty.

**No cap on the element count.**
Measured at ten thousand failing map keys: 6.2 ms to sort and join, a **79-byte** one-line form, and a 360 KB `%+v` that only a caller who asked for it ever builds.
The one-line form is already bounded by the elision below, so the unbounded thing is opt-in.
A cap would have to pick an N, and ADR-0001 forbids dropping anything silently, so a capped set has to state what it dropped anyway.

### The three-part key: moment, then location, then message

> Elements sort on **moment**, then **location**, then **message**.
> Within a moment, an element with no location sorts first.
> Locations compare segment-wise, per ADR-0003.

**The moment is the first term because of `Close`.**
ADR-0004 runs `Commit` only on success and `Close` always, so a failed dump can hold field errors **and** a `Close` failure.
Discarding the latter is silently ignoring something, which ADR-0001 forbids, so it is an element.
But a `Close` failure has no location and **explains nothing**, so a rule of "location-less sorts first" alone put it at the head of a report it had nothing to do with.
With the moment first:

```
ferry: 4 errors:
  opening the plane: kv: dial tcp: connection refused
  /db/host: value did not parse as int
  /db/port: value did not parse as int
  closing the plane: kv: flush failed
```

`Open` precedes the walk errors it caused, and `Close` follows them.

**The message tiebreak is not decoration.**
ADR-0006 measured one field producing two errors at one address, so the address is not a key.
Insertion order would settle it today, and [#20](https://github.com/onhotpath/ferry/issues/20) may make the walk concurrent, at which point insertion order is 5.5 again.
Measured, 300 runs each: two errors at one address with the insertion order reversed give **1 distinct report**, and four errors collected by four goroutines give **1 distinct report**.

The cost is that two errors at one address are ordered alphabetically rather than in the order the checks ran, which is arbitrary and stable.
That is a knowing trade against an order that is natural and not concurrency-safe.

**One thing the census bounds, worth stating because it looks like a risk and is not.**
A compile failure means no walk runs and a registration failure means no schema exists, so `compile` never shares an aggregate with `walk`, and `register` never shares one with either.
The cross-moment ordering only ever has to order `open`, `walk`, `commit` and `close`.

### Presentation: `Error()` is one line, `%+v` is the report

The ticket comment's line is that "`errors.Join`'s newline dump is not a presentation layer".
This is what replaces it.

```
%v    ferry: 3 errors: /db/port, /tls/cert, /workers#7

%+v   ferry: 3 errors:
        /db/port: is not a valid int
        /tls/cert: required, and the plane supplied nothing
        /workers#7: the plane has index 7 and [3]string holds 3
```

`Error()` is what lands inside somebody else's `fmt.Errorf("loading config: %w", err)`, and a forty-line string inside a sentence is unusable.
An `Error()` that said only "40 errors" would be useless to the operator who logged `%v`.
Naming the addresses is the compromise that is actionable on its own, because the address is the thing an operator acts on.

Past the threshold the summary elides, and at forty it is still one line:

```
loading config: ferry: 40 errors: /svc/f00, /svc/f01, /svc/f02, and 37 more
```

This is a **presentation** cap and not a data one: the count is stated, and `%+v` and `Elements` both still have everything, so ADR-0001's ban on silent truncation holds.

**The `ferry:` prefix is promoted.**
A leaf's own `Error()` carries it; inside the aggregate's report the header already said it, so the per-line prefix is suppressed rather than printed once per element.

**Message text is not API**, and this belongs in the package doc and not only here.
The accepted ADRs quote strings like `ferry: /A: main.encOnly implements encoding.TextMarshaler but not ...`, which makes them look canonical, and somebody will write `strings.Contains` against one.
The stated position is to match on the sentinels and the address, and to get precision from the test helper rather than from string matching.

### On failure, `Load` yields no value

> When `Load` returns an error it yields no value, and the caller's own variables are untouched.

Aggregating means the walk continues past a failed field, so the partial exists inside ferry either way.
The question is only what crosses the boundary.
Measured:

```
the partial the walk built  : {Host:db1 Port:0}
what Load returns           : {Host: Port:0}
```

"Unspecified, do not use" is a documentation promise, and the failure mode when somebody does not read it is a process that starts with `/db/host` set and `/db/port` zero, which for a config loader is the worst available outcome: it runs, and it is wrong.
Yielding nothing makes ignoring the error fail immediately and visibly.
The partial is worth nothing to the two consumers ADR-0001 named, because deployment validation reads the error set and schema extraction dumps into a recording sink.

**The better argument is that this turns a documentation obligation into a property.**
The ticket comment says the partial population "needs documenting"; under this rule there is nothing to document, because the state it warns about is not observable.

**This constrains [#16](https://github.com/onhotpath/ferry/issues/16), and that is stated rather than discovered.**
It rules out a `LoadInto(ctx, src, &cfg)` shape, because "yield no value" under that signature would mean ferry zeroing a struct the caller owns, which is worse than either option.
So the entry point returns the value it built.
ADR-0006 already tied the same hands once, requiring that a reload produce a new value rather than mutate a live one, and this is the same screw one turn further.

### ferry has exactly one aggregate constructor

> ferry never calls `errors.Join`.

This is the defect the prototype found in a decision this ADR had already made, and it is recorded because the shape of the miss is the point.

Schema compile still used `errors.Join`, and the consequence was silent: `Elements` reported **one** element while **two** errors printed.
A stdlib `errors.Join` result is invisible to `Elements`, is ordered by insertion, and renders as the newline dump this ADR replaced.

Worse, the prototype's own probes for ADR-0008 were **parsing that newline dump as an iterator**, splitting `err.Error()` on `\n` to count errors.
That is the ticket comment's "not a presentation layer" arriving as a live dependency rather than as a critique.

So the rule is not a style preference.
Any `errors.Join` inside ferry produces an aggregate that the reader half of this ADR cannot range and the determinism half cannot order.

### Panics, and nil

**ferry never recovers a panic from third-party code.**
A panic in a driver's `Get` or a codec's `UnmarshalText` kills the load and takes the aggregate with it, and ferry is the only party that knows the address.
It is still not recovered: a panic is a bug, not a failure mode, and converting it to an error at an address makes a broken codec look like bad config.
`encoding/json` recovers only its own sentinel and re-panics everything else, and the address is in the stack anyway.

**ferry itself never panics.**
ADR-0004 already required it of `Value`'s accessors, on the ground that ferry's callers are third-party driver authors.
ADR-0009 measured two live violations, an `interface conversion` on a nil interface and a `reflect.Value.Set` on a zero `Value`, and this is the rule they are fixed against.

**Two nil rules, both testable.**
ferry never returns a nil `*Error` as a non-nil `error`, which is why `ErrorAt` returns `error`.
The aggregate never holds a nil element, because the `errors` package doc states it is invalid for `Unwrap() []error` to return one.
`Elements(nil)` is nil.

No defensive nil receivers beyond that: `fmt` already turns a panic inside `Error()` into `%!v(PANIC=...)`, so nil-safe methods would buy a blank string in place of a visible bug.

### What `ferrytest` gets, and what [#35](https://github.com/onhotpath/ferry/issues/35) owns

Message text is not API and the vocabulary is five words, so a test that wants to assert "this exact thing went wrong at this exact place" needs somewhere for the precision to live.

```go
func DiffErrors(got error, want ...Want) []string
func CheckErrors(t *testing.T, got error, want ...Want)
type Want struct { Address ferry.Path; Class error }
```

Exact-set semantics over `(address, class)` pairs, in segment-wise order, with a diff rather than a boolean.
The primitive returns `[]string` and takes no `*testing.T`, because the conformance suite runs against third-party drivers and wants the result as data.

**Exact rather than "contains", and the prototype shows why.**
ADR-0008's tiers are a suppression order, and the defect they are most likely to develop is firing once too often.
Measured against a suppression rule that reports a consequence it should have suppressed:

```
DiffErrors:      count: /origins schema error got 3, want 2
errors.Is(...):  true
```

A contains-assertion passes straight through it.

**Three consumers need this and none is an ordinary user test**: core's own tests over `Validate[T]()`, the conformance suite's "refused loudly" which is currently prose, and a registrant discharging ADR-0009's proof.

**It ships in `ferrytest` only, and [#35](https://github.com/onhotpath/ferry/issues/35) owns whether that is where it stays.**
Promotion to the root package is a later call, and nothing stops a user importing `ferrytest` from production code meanwhile.
This ADR fixes the semantics, exact-set over `(address, class)` with no message assertion at any level, and leaves the package's surface to #35.

## Consequences

- ferry has one error model over fifty-five refusals from eight ADRs, and the two rows it does not express are named rather than absorbed.
  A contributor adding a refusal picks a moment and a class rather than a type.
- **There is no concrete type to depend on**, so the ticket's hardest ask is answered structurally.
  The cost is that the idiomatic `errors.As` into a named error type does not compile, and the package doc has to teach the accessor form.
- The vocabulary is five words and it is frozen at v1 like every other public name.
  Thirty-three of fifty-five refusals are one class, which is a strong hint that the interesting axis is the address rather than the classification.
- **A driver can hold an opinion about the class and about nothing else.**
  It can also be wrong about it, and nothing checks that, which is a conformance case in the same family as ADR-0004's three optional interfaces.
- **ferry's own error text carries no plane value, on every plane, always.**
  Measured at four leaks in five naive messages, on values a Vault or Consul plane makes secret by default.
  The cost is that ferry authors a message for every decode failure mode instead of passing one through, and the carve-out is that a dynamic address segment is plane-supplied and is printed.
- Aggregating fixes 5.4 at one error against five on the same plane, and it costs a non-staging sink eight extra writes of twelve.
  It costs a `Committer` nothing, because `Commit` never runs.
  No `StopOnFirstError` ships, and adding one later is a load-affecting Option, which ADR-0006 already priced as the cheap kind.
- **Sorting at construction rather than at print time is what makes 5.5's fix cover the programmatic reader.**
  Measured at three distinct `AsType` picks over 300 runs when the order is applied only in `Format`, while the printed form stays at one.
- The sort key is three parts because a `Close` failure has no location and explains nothing, and the message tiebreak is what keeps the order deterministic if #20 makes the walk concurrent.
  The cost is that two errors at one address are ordered alphabetically rather than in check order.
- **`Load` yielding no value turns the ticket's stated cost into a property**, and it constrains #16 to an entry point that returns the value it built.
- **ferry never calls `errors.Join`**, and that rule exists because breaking it was silent: the prototype's own probes were parsing the newline dump as an iterator.
- The error set is now a user-visible artefact under ADR-0001's determinism invariant, which it was not before, because nothing had made it orderable.
- `ferrytest` gains an exact-set assertion, and the reason it is exact is that ADR-0008's suppression rules will fail by over-reporting and a contains-assertion cannot see that.

## What this ADR does not decide

- **The entry point's signature**: [#16](https://github.com/onhotpath/ferry/issues/16), which is being written in parallel.
  This ADR hands it one constraint, that `Load` returns the value it built and never writes a partial into a caller's struct, and it inherits ADR-0006's separate constraint that a reload produces a new value.
- **What `ferrytest` exports**: [#35](https://github.com/onhotpath/ferry/issues/35).
  This ADR fixes the semantics of the error assertion and not the package's surface.
- **Whether the walk checks the context per leaf, per subtree or not at all, and what happens when a cancellation races a driver error**: [#20](https://github.com/onhotpath/ferry/issues/20).
  This ADR decides only the classification, which is that a cancellation gets no ferry class, and hands #20 one constraint: the sort tiebreak must not be insertion order, and the collection must be safe under a concurrent walk.
- **The watch API's error convention**: [#13](https://github.com/onhotpath/ferry/issues/13).
  The survey records that there is no stdlib convention for errors in an iterator and that the absence is deliberate and current, so #13 invents one and must document which of the four readings it means.
  What it inherits from here is the element type and the classification, not the streaming shape.
- **Whether the vocabulary ever grows.**
  A class is a public name and ADR-0002 keeps ferry at v0, which is the only window in which one can be taken back.
- **Whether `Elements` is ever spelled as an `iter.Seq[error]`.**
  A slice is one allocation and is what everybody will range.
  The survey's warning is about `Seq2[T, error]` and does not apply to a sequence that cannot fail, so this is an open spelling rather than a settled one.
- **Whether a cap on the element count is ever wanted.**
  Measured at ten thousand elements and 6.2 ms, so nothing forces one today; if one arrives it is an Option and it states what it dropped.

## Items from the xload survey

The survey is [`docs/research/generics-and-modern-go.md`](../research/generics-and-modern-go.md), section 5, against `github.com/gojekfarm/xtools` at [`a90b3aa`](https://github.com/gojekfarm/xtools/commit/a90b3aad2133248cec50f6b4d6e37b0d9e788adb).

**5.4, first error only, no aggregation.**
Addressed, and this ADR owns it outright.
Reproduced in ferry's own shape rather than only in xload's: the walk this ADR inherited was first-error-wins, and on five bad fields it reported one where the aggregating walk reports five.
The second half of 5.4 is the more interesting one and it is fixed by construction: xload's serial path fails fast and its concurrent path aggregates, so `Concurrency(4)` changes how many errors exist.
ferry has one collection rule at every moment and in both directions, so the two cannot disagree, and the pairwise tree the survey warns about is unrepresentable because ferry never nests its own aggregates.
The survey's recommended mitigation, a `StopOnFirstError` option, is **declined**, with the reason stated.

**5.5, nondeterministic error output.**
Addressed, and the fix is wider than the survey's.
The survey's fix is one `slices.Sort` call on the collision keys, and ADR-0001 generalised it to a package-wide invariant that every map iteration reaching a user-visible artefact is sorted.
This ADR adds the part that generalisation did not reach: the error set **is** a user-visible artefact, and sorting it only when it is printed leaves `errors.AsType` nondeterministic, measured at three distinct picks over 300 runs while the print stayed at one.
So the sort happens at construction, the key is total rather than address-only, and the tiebreak survives a concurrent walk.

**5.14** was enumerated rather than assumed, all four items.

- *Two ways to set the loader.*
  Bears on this ADR directly, because `ErrorAt` is a second thing that can put an address on an error.
  Measured rather than argued: `ErrorAt` produces no `*ferry.Error` and matches no class, so it attaches and never classifies and is inert until core wraps it.
  Where core already knows the address, core's wins, so a driver cannot misattribute a `Get` failure.
  There is one constructor of ferry errors and it is core's.
- *The `CanAddr` loop that can only run once.*
  Bears on nothing here.
  ADR-0005 and ADR-0007 each carried their half of it.
- *The non-deterministic select on a cancelled context.*
  Concurrency, and [#20](https://github.com/onhotpath/ferry/issues/20)'s.
  This ADR neither fixes nor worsens it, and hands #20 two things it should know: a cancellation is an ordinary element with no ferry class, and the sort key must stay total under a concurrent walk, measured at one distinct report over 300 runs across four goroutines.
- *Value receivers on `Error()` where pointers are returned.*
  **Addressed, and this is the item every ADR from 0003 to 0009 deferred here.**
  `Error()` is on the pointer receiver, measured: the value form does not implement `error` and the pointer form does, so the natural value-form `errors.As` cannot silently fail because it does not compile.
  The prototype contained a live instance of the defect in its own `unsupportedTypeError`, which is how the shape arrives: not by choosing it, but by not choosing against it.

**5.11, the YAML provider silently discards parse errors**, is ADR-0001's, and this ADR gives it an honest class rather than only a place to land: the driver declares `ErrValue` where core would have said `ErrPlane`, because a document that does not parse is the operator's file and not the infrastructure.

**5.1, the `Loader` signature cannot express absence**, is ADR-0004's and ADR-0006's, and it surfaces here once, as the reason `ErrMissing` can be a class at all: a plane that cannot tell an empty value from a missing key has nothing for that word to mean.

**5.7, `reflect.DeepEqual` as a probe**, is ADR-0006's and ADR-0005's.
It surfaces here as the reason the test helper asserts on `(address, class)` pairs and never on message text: a structural comparison of error values would be the same instinct in a third costume.

The remaining items are unaffected by this ADR.
