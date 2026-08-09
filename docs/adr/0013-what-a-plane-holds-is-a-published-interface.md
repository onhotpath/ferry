# 13. What a plane holds is a published interface, and how it is versioned

Status: Accepted
Date: 2026-08-03
Ticket: [#28](https://github.com/onhotpath/ferry/issues/28)

## Context

[ADR-0005](0005-the-supported-type-set.md) pins a representation for every type in core's set and makes it a checked column in the round-trip harness: `time.Duration` is `30s`, `time.Time` is RFC 3339, `[]byte` is the raw bytes, floats are shortest-round-trip text.

Those strings are not an implementation detail.
Once ferry ships they are in every user's config files, KV stores and secret backends, and they are the only thing a user's stored data consists of.

**Changing one is not a Go API break**, and that is the whole ticket.
[ADR-0002](0002-core-and-sub-modules.md) settled module versioning, independent driver tags and a named v1 trigger, and said nothing about the contents of a plane, because at the time nothing had pinned them.
ADR-0005 pinned them and filed this.

This ADR is written from a throwaway prototype on branch `proto/28-plane-compat`, which never merges.
Nine probes, `Q28=<n|all> GOTOOLCHAIN=go1.27rc2 go run .` from `proto/`.
Every number below is from that prototype unless it cites the survey.

**One probe found something that changes what this ADR can promise rather than only what it argues**, and it is recorded in [The golden column is the promise](#the-golden-column-is-the-promise-in-executable-form-and-it-is-not-in-the-harness) rather than in a footnote.

## Decision

### What this closes, and what it does not

| The ticket asked | Closed | Where |
| --- | --- | --- |
| is a representation change a major version | **yes**, and ferry states a second promise semver does not describe | [Plane compatibility](#plane-compatibility-is-a-second-promise-and-semver-is-its-instrument), [The version rule](#the-version-rule-and-the-one-property-it-rests-on) |
| does it differ for core's pinned set versus a registered codec | **yes**, three tiers with three owners | [Three tiers](#the-promise-has-three-tiers-and-only-one-of-them-is-ferrys) |
| is a registrant told at registration time ([#19](https://github.com/onhotpath/ferry/issues/19)) | **yes: no**, and ADR-0009's own mechanism is measured unavailable | [Three tiers](#the-promise-has-three-tiers-and-only-one-of-them-is-ferrys) |
| can a driver's spelling move independently of core's | **yes**, and it is the tier most exposed to accident | [Three tiers](#the-promise-has-three-tiers-and-only-one-of-them-is-ferrys) |
| is there a read-old-write-new path at all | **yes: no**, and the alternative was built rather than reasoned about | [Read-old-write-new is refused](#read-old-write-new-is-refused-and-it-was-built-first) |
| does the conformance suite pin the driver's spelling | **yes**, by a case of a kind the suite does not currently have | [The suite gets a golden artefact case](#the-suite-gets-a-golden-artefact-case-and-a-round-trip-cannot-replace-it) |

Three questions this ADR had to answer that the ticket did not name:

| Not asked for, answered anyway | Where |
| --- | --- |
| whether a representation change is one failure or several | [Three outcomes](#an-old-artefact-under-a-new-rule-has-three-outcomes) |
| how wide the promise can be, given that most of what users store has no pinned representation | [Three tiers](#the-promise-has-three-tiers-and-only-one-of-them-is-ferrys) |
| whether the instrument the promise rests on exists | [The golden column is the promise](#the-golden-column-is-the-promise-in-executable-form-and-it-is-not-in-the-harness) |

**Two things this ADR does not close.**

- **The promise cannot cover what nobody chose.**
  A type admitted by `reflect.Kind` and a type claimed by ADR-0007's text arm both land on a plane with a representation ferry never picked, and the second set is unbounded and unenumerable.
  Stated as a boundary, not solved.
- **Nothing here is enforced today**, because the column it rests on is not in the harness that runs.
  That is [#35](https://github.com/onhotpath/ferry/issues/35)'s to build, and this ADR is why it is load-bearing rather than tidy.

### The premise is confirmed rather than assumed

The ticket says a representation change is invisible to the toolchain.
Go ships a tool whose entire job is to answer "did this release break anything", so the claim is checkable.

Two modules with an identical exported API and one line different in a function body, one returning `"30s"` and the other `"30000000000"`:

| what was run | what it said |
| --- | --- |
| `go build ./...` | nothing |
| `go vet ./...` | nothing |
| `gofmt -l` | nothing |
| `apidiff v1 v2` | nothing |
| the **consumer's own** round-trip test, on v1 | `ok` |
| the same test, unchanged, on v2 | `ok` |

`apidiff` is the tool the Go team ships for exactly this question and the one `gorelease` reads to recommend a version number.
It reports nothing because nothing changed: no consumer's code stops compiling.

**The consumer's test is the sharpest row.**
It is the round-trip property, which is what a library asks a user to write, and it passes on both versions because nanoseconds round-trip perfectly.
That is ADR-0005's own measurement arriving from the consumer's side: a property test cannot see a representation, so neither can the user who wrote one.

> Semver describes the Go API, and the Go API is not the only thing a mapper publishes.

### An old artefact under a new rule has three outcomes

"It breaks stored data" is true and is not one failure.
Graded by what happens when the old text meets the new parser, which is what a running program does after an upgrade:

| the change | the stored text | what the new parser does |
| --- | --- | --- |
| `time.Duration`: `30s` -> nanoseconds | `"30s"` | **loud**, `invalid syntax` |
| `time.Time`: RFC 3339 -> Unix seconds | `"2026-01-15T12:00:00Z"` | **loud**, `invalid syntax` |
| `[]byte`: raw -> base64, a 2-byte payload | `"hi"` | **loud**, `illegal base64 data at input byte 0` |
| `int`: base 10 -> base 16 | `"10"` | **silent, wrong**: reads as 16 |
| `[]byte`: raw -> base64, a 4-byte payload | `"data"` | **silent, wrong**: reads as three other bytes |
| `float64`: shortest -> `%.17g` | `"0.1"` | silent, and correct |
| `bool`: `true` -> `1` | `"true"` | silent, and correct |

Three loud, two silently wrong, two silently compatible.

**The two base64 rows are one change over two payloads and they disagree**, which is the result that decides how the promise is worded.
Even a single representation change is not one outcome; it is a distribution over the data people happen to have.

The silently-compatible rows are the trap in the other direction, and they are the ones a maintainer would call safe.
They are safe *for now*: the new parser is a superset of the old spelling by accident rather than by design, nothing stops the next change from narrowing it, and nothing recorded that anyone was relying on it.
This is the class where "we tested it and it was fine" is true and useless.

> So the promise cannot be stated over the **outcome**, because the outcome is a property of the pair of versions *and* of the stored value.
> It has to be stated over the **text**.

### Plane compatibility is a second promise, and semver is its instrument

> **ferry makes two compatibility promises, not one.**
>
> **API compatibility** is semver over the Go API, unchanged, and is what ADR-0002 already decided.
>
> **Plane compatibility** is the promise that, for a given Go value, a given module version writes the same text into a plane that the previous version wrote.
> It is a property of an artefact rather than of a build, no tool in the Go toolchain can see it, and this ADR states it separately for that reason.

Two things follow immediately and are worth stating as rules rather than leaving to inference.

**Plane compatibility is stated per module, not for ferry as a whole.**
ADR-0002 versions core and each driver independently, so each publishes its own.
Core's covers the text a `Value` carries; a driver's covers how that `Value` is spelled on its plane.
Those are different questions with different owners, and ADR-0005 already drew the line: "`Bytes` carries the bytes, and how a plane spells them is the driver's".

**It is scoped to the golden rows, and only to them.**
A promise wider than what is pinned would be a promise ferry cannot keep, and [the third tier](#the-promise-has-three-tiers-and-only-one-of-them-is-ferrys) is why.

### The version rule, and the one property it rests on

> **A change to a golden row is a major version of the module that owns it, and it ships with a written migration.**
> There is no separate ferry-specific version number.

The rule rests on one property of the Go module system, and it was measured against a real file-based `GOPROXY` rather than quoted.
Three published versions of one module, and a consumer sitting at `v1.0.0`:

```
at v1.0.0, the program prints           30s
go get -u ./...                         upgraded example.com/cfg v1.0.0 => v1.1.0
after the upgrade, it prints            30000000000
go.mod now says                         require example.com/cfg v1.1.0

v2.0.0 is published, with the same change
go list -m -versions example.com/cfg -> example.com/cfg v1.0.0 v1.1.0
go get -u ./... again                 -> nothing
go.mod mentions /v2                   -> false
```

The minor release arrived on a `go get -u` and changed what the program writes.
The major release did not arrive and could not, because `example.com/cfg/v2` is a different import path and nothing upgrades across one.

> A major version is the only release in the Go module system that a consumer cannot receive without editing a line.
> A representation change is precisely a change that must not arrive without somebody editing a line.

**A second, ferry-specific version number was considered and has neither property.**
Nothing in the toolchain reads it, so it could not stop the upgrade.
And ferry cannot store it beside the data either: writing a version marker into a plane is a format decision, and the plane-agnosticism veto in [ADR-0001](0001-what-ferry-supports.md) rules that out by name.
It would be documentation with a number on it, and this ADR would rather write the documentation.

**What "with a written migration" costs is priced in [Read-old-write-new is refused](#read-old-write-new-is-refused-and-it-was-built-first)**, and it is five lines of user code that ferry already makes possible.

**Two consequences of using semver rather than a new number, stated because they are the sharp edges.**

A major version is a heavy instrument for a one-line spelling change, and it forces an import-path edit on every consumer including those who store nothing.
That is accepted: it is the same instrument for the same reason `encoding/json/v2` is a new import path, and the alternative is a change that arrives silently in a dependency bump.

And a major version is *sufficient but not necessary*: a major release for an ordinary API reason may also change a representation, and nothing stops it.
So the rule is one-directional, and the written migration is what carries the information the version number cannot.

**ferry is at v0 and this is the window.**
ADR-0002 puts ferry at v0 deliberately, as the only place semver allows taking a decision back, and names #11's grammar surviving real use as the v1 trigger.
**The v1 trigger gains a second input: the golden column.**
Not because it must be complete, but because at v1 it becomes a promise with a major version behind it, and it is cheap to change today and expensive after anything is stored.

### The promise has three tiers, and only one of them is ferry's

Counted rather than described.

| tier | who chose the representation | who promises it | how it is checked |
| --- | --- | --- | --- |
| **pinned** | core, in the identity table and the kind table | core, at core's major version | core's golden column |
| **the registrant's** | whoever registered the codec ([ADR-0009](0009-typed-codec-registration.md)) | the registrant, at their own major version | their own golden column, in their CI |
| **nobody's** | nobody | **nobody, and the ADR says so** | nothing can |

**Tier one is smaller than it looks**, and that is measured.
`CoreTypes()` holds **eighteen rows against eighteen admitted members**.
It matters here because the promise is exactly as wide as the table: a member with no row is in tier three by accident rather than by decision.

> **Corrected under [#41](https://github.com/onhotpath/ferry/issues/41), which closed while this ADR was in review.**
> As published this read "eleven rows against eighteen admitted members: `int16`, `int32`, `int64`, `uint`, `uint8`, `uint16` and `uint32` have none", and called D18 "red on the prototype today", with closing it [#35](https://github.com/onhotpath/ferry/issues/35)'s.
> Measured on [`proto/tip`](https://github.com/onhotpath/ferry/tree/proto/tip) at `0d86c00`: `harness.go` carries **18 proof rows** and `go test ./...` is **green**, so D18 was closed on the tip independently and this ADR's prototype was cut before that landed.
> **Nothing in the argument moves.** The count was evidence that the promise can be narrower than the admitted set by accident, and a count of zero missing members is the same rule with nothing currently failing it.
> [ADR-0014](0014-what-ferrytest-exports.md)'s table closes it again, at nineteen rows, because it is a different table with a third column - which is the finding below, and that one is **unaffected**: the golden column is still absent from `harness.go`'s proof on `0d86c00`, so `CoreTypes()` is still the proof type that cannot see a representation.

> **Amended under [#352](https://github.com/onhotpath/ferry/issues/352): the `.env` quoting and escape table joins the pinned tier, owned by `driver/env`.**
>
> As published this section's pinned tier is core's identity and kind tables, and the driver-owned spellings named anywhere in this ADR are `driver/yaml`'s scalar forms and its `!!binary`.
> `driver/env` now writes a file, so it has a representation, and it is pinned by golden rows in that module's own conformance run.
>
> The rows are the quoting choice and the escape table, in both directions:
>
> | value | written |
> | --- | --- |
> | `db.internal` | `db.internal`, bare |
> | `""` | `''` |
> | `" padded "` | `' padded '` |
> | `"# not a comment"` | `'# not a comment'` |
> | `"$HOME"` | `'$HOME'` |
> | `say "hi"` | `'say "hi"'` |
> | `"it's"` | `"it's"` |
> | `"a\nb"` | `"a\nb"`, one physical line |
> | `"\xff\xfe"` | raw bytes inside double quotes |
> | `"a\x00b"` | refused, `ErrValue` |
>
> Bare is used where every byte is in `A-Z a-z 0-9 _ . / : @ % + , = -`; single quotes where the value holds no `'`, `\n` or `\r` and is valid UTF-8; double quotes otherwise, escaping exactly `\` `"` LF CR TAB as `\\` `\"` `\n` `\r` `\t` and writing every other byte through, invalid UTF-8 included.
>
> **The tier is this ADR's own, and so is the consequence**: a change to any row above is a major version of `github.com/onhotpath/ferry/driver/env`, not a test-fixture edit.
>
> **One promise it does not make.** Every value here round trips through this driver, and every value except two classes is also one `sh` reads back identically when the file is sourced: a shell does not unescape `\n`, `\r` or `\t`, and a value holding a single quote together with a `$` or a backtick has to be double-quoted and is then expanded.
> Inventing a `\$` escape would fix the second and make the file unreadable to every other `.env` reader, which is the [ADR-0018](0018-the-spelling-seam.md) trade this driver declines.

**Tier two transfers with the guarantee ADR-0001 already transfers**, and the mechanism is not ADR-0009's.

ADR-0009 communicates obligations through a diagnostic, and is explicit that this is the point: "the diagnostic is where the obligation gets communicated ... it is the only moment a registrant is guaranteed to read".
That mechanism is unavailable here, and the reason was measured rather than asserted.
`.AsMapKey()` works because there is something to **refuse**: a map keyed by an unopted type does not compile.
A representation change has nothing to refuse, because at the moment of registration the previous release is not in the room:

```
Register(a codec)                                  -> nil
Register(a DIFFERENT codec for the same type)      -> ferry: T is already registered
```

Within one process there is one representation per type.
Across two releases of the registrant's own package there is nothing to compare against.

> So the obligation transfers and the diagnostic cannot carry it, which is a different answer from ADR-0009's and is argued rather than inherited.
> What carries it is the thing a registrant already writes if they want the guarantee at all: **a proof with a golden column is a change detector.**

A registrant who pinned `string("api:80")` and then changes their codec has a red test in their own CI, on their own schedule, before they tag.
That is the same instrument as core's, which is the point: one mechanism, two owners, and core invents nothing.

**Tier three is the honest part and it is large.**
A type admitted by `reflect.Kind` gets a representation nobody chose, which is ADR-0005's own category 3 - a `[16]byte` UUID as sixteen raw bytes, because `[16]byte` is `Bytes`.
A type claimed by ADR-0007's text arm gets one from its own author, and that set is unbounded and unenumerable, which ADR-0007 calls its own weakest point.

> **Corrected under [#167](https://github.com/onhotpath/ferry/issues/167): the category-three example named a type the text arm claims, and as published it read "`net.IP` as sixteen raw bytes, a `[16]byte` UUID likewise".**
>
> `net.IP` declares both halves of the text pair, so [ADR-0007](0007-the-codec-chain-and-its-precedence.md)'s text arm claims it before kind admission is reached and it lands as `string("192.0.2.1")`, not as sixteen raw bytes.
> [ADR-0005](0005-the-supported-type-set.md) already carries this correction inline at its own category-three paragraph, and this ADR did not.
> The `[16]byte` UUID half was correct as published and is the example that is kept, because no chain arm claims it.
>
> **The argument is unchanged**, and the fact that it needed a new example is the chain shortening the list exactly as intended.
> `net.IP` is still in the third tier of this section's own three, but by the sentence below rather than the one above: its representation comes from `net`'s author, which is the arm ferry cannot enumerate.

ferry cannot promise what it never chose.
Saying so is more useful than a promise that quietly excludes most of what users actually store, and the documentation obligation ADR-0005 already created for category 3 grows one line: **the set's documentation says which tier each member is in.**

### The golden column is the promise in executable form, and it is not in the harness

[ADR-0002](0002-core-and-sub-modules.md) admits the harnesses to core by route (b), authority, on the line that a rule is only worth anything when it ships from the same place as the thing that checks it.
Plane compatibility is exactly such a rule, and ADR-0005 already built its instrument:

> Each case carries the boundary `Value` it must produce, and the harness checks it.

**So this ADR needs no new mechanism.**
The golden column *is* plane compatibility written down, and a change to a golden row is the definition of a change to the promise.
What this ADR adds is what that column **means**: not a test fixture, but the published artefact.

**And the probe that checked it found the column is not there.**

Re-running ADR-0005's own demonstration through the *current* harness, which [#41](https://github.com/onhotpath/ferry/issues/41) item 6 repointed at `Dump` and `Load`, with `time.Duration`'s codec replaced by the nanosecond one that ADR-0005 rejects by name:

```
                                       pinned codec   nanosecond codec
memory plane                           1/1 pass       1/1 pass
yaml driver, real files                1/1 pass       1/1 pass
flattening plane                       1/1 pass       1/1 pass

and what it writes:  /t = string("30000000000")
```

Green in both worlds, on all three planes.
That is ADR-0005's finding reproduced against the engine, and it is expected.
What is not expected is the reason:

| where | shape | golden column | runs through |
| --- | --- | --- | --- |
| `harness.go` | `Type[T](name, eq, values...)` | **no** | `Dump` and `Load` |
| `r10_proof.go` | `Prove[T](name, eq, cases...)` with `Want` | yes | the superseded `walk.go`, over a `map -> map` transform |

There are two proof types, and `CoreTypes()` - the table the completeness check checks, and the thing standing in for `ferrytest` - is the one without the column.
**So the column ADR-0005 calls the whole reason a proof is a triple has never run through the engine.**
#41 item 6 repointed "the harness" at the entry point and repointed the one that could not see a representation.

That is the audit's own recorded shape once more: a green probe over a case its own fixture excluded.

For this ADR it is not a note, it is the boundary of what can be promised today:

> The compatibility promise this ADR states is checkable exactly where a golden row exists, and core's CI has none.

The spelling of the merged proof is [#35](https://github.com/onhotpath/ferry/issues/35)'s.
That it must be **one** type with three columns, running through the entry point, is this ADR's, because a promise nothing executes is prose again within two releases - which is ADR-0002's own argument for route (b), applied to the thing route (b) was admitted for.

### Read-old-write-new is refused, and it was built first

The ticket calls accepting a superseded form on Load "the obvious answer" and notes that it contradicts ADR-0005's "nothing else coerces".
ADR-0005 admits exactly one coercion and requires a second to meet the same standard of argument, so this one was built and run before it was judged.

A dual-read `time.Duration` codec, writing `30s` and accepting both `30s` and a nanosecond count:

**(1) It works.**
A stored `"30000000000"` loads as `30s`.

**(2) And the first dump rewrites the artefact anyway.**

```
before   t: "30000000000"
after    t: "30s"
```

Every stored file changes on the first write after the upgrade, so the migration happens regardless - unannounced, one file at a time, in whatever order processes happen to dump.
That is the deciding fact and it is easy to miss: a read-old path does not *avoid* the migration, it performs it silently.

**(3) The golden column stops being a column.**
A proof pins one text.
A dual-read codec has one on Dump and two on Load, so the triple cannot state the decode side at all:

```
Dump writes exactly one text     /t = string("30s")
Load accepts "30s"            -> 30s
Load accepts "30000000000"    -> 30s
```

The instrument this whole ADR rests on is the first casualty.

**(4) It is permanent, because nothing can tell you whether any plane still holds the old form.**
ferry's own schema-extraction pattern is a recording sink, which records what ferry **wrote** and never what the plane **held**:

```
the recording sink saw:  /t = string("1s")     <- ferry's text, never the plane's
```

So a deprecation cycle has no signal to end on, and the arm survives in core forever.

**And it is a second coercion on its own terms.**
ADR-0005 admits `String` as the universal donor on the argument that `String` is what a plane says when it has *nothing to say* - it means untyped text, parse it yourself.
A superseded form is not that.
It is ferry second-guessing text a plane asserted, which is the thing ADR-0005 refuses for `Number` into a Go `string`.

#### What replaces it, run

ADR-0001 puts plane-to-plane transfer in the Enabled bucket - "falls out of the pluggable design for free".
A representation migration is that capability with the same file on both ends and two versions of the codec:

```go
cfg, err := ferry.Load[Conf](ctx, yaml.Source{Path: p}, ferry.WithRegistry(old))
if err != nil {
    return err
}
return ferry.Dump(ctx, cfg, yaml.Sink{Path: p}, ferry.WithRegistry(new))
```

Five lines, and the same five for a driver-side spelling change with the two registries replaced by two driver versions.
Run against a real file:

```
before   t: "30000000000"
after    t: "30s"
verify: reload under the new codec  -> 30s, err=<nil>
the OLD codec on the migrated file  -> ferry: /t: is not a valid Timeout
```

The last line is the property the dual read does not have and cannot have.
Three of them:

- it is a **decision**, taken once, by a person, at a time they chose;
- it is **observable**, having succeeded or not over a file list they hold;
- it **terminates**, and the old codec is deleted afterwards, because the migrated data refuses it.

**What it costs, plainly, and it is the ticket's own objection.**
A mapper that can only read what its own version wrote is a worse migration story than the config files it replaced.
The answer is not that the cost is small.
It is that the alternative pays the same cost invisibly and then charges rent.

### The suite gets a golden artefact case, and a round trip cannot replace it

ADR-0005 leaves the spelling of `Bytes` to the driver, and ADR-0002 versions drivers independently, so a driver-side data break is expressible with no core change.
The measurement is what a conformance run says about it.

The YAML driver's `Bytes` spelling changed from base64 to hex - a one-line edit touching both halves, which is what a driver author would actually write, because the reader and the writer are in one file:

```
base64, as the driver ships    b: !!binary aGk=      round-trips: true
hex, a one-line change         b: !!binary 6869      round-trips: true
```

Both round-trip.
Value fidelity holds, driver fidelity holds, the conformance suite as ADR-0004 and ADR-0005 describe it is green, and every file the previous version wrote is now garbage.

This is ADR-0005's own `!!binary` story with the one thing that saved it removed.
There, "the pair was self-consistent and round-tripped; what caught it was `gopkg.in/yaml.v3`'s emitter refusing to emit invalid `!!binary`".
Here both spellings are valid YAML, so nothing external refuses.

> A round trip tests a function against its own inverse.
> A spelling is a choice of function.
> Changing both halves together is invisible to any test that only composes them.

So the suite needs an assertion of a different kind:

> **A driver's conformance run includes a golden artefact case: a fixed value, dumped, compared against fixed expected plane contents.**

**This is not the byte-level plane fidelity ADR-0001 rules out**, and the distinction is exact.
ADR-0001 rejects requiring that a *user's* comments, whitespace and key order survive a Load and Dump cycle.
This asserts the *driver's own output* for one input, which is the golden column at the driver's boundary rather than at core's.
The two are the same instrument at two layers, which is why the suite gains a case rather than a mechanism.

What the case contains, and how much of the artefact it may assert, is [#35](https://github.com/onhotpath/ferry/issues/35)'s.
That it exists, and that a round-trip case cannot stand in for it, is this ADR's.

### What this ADR does not decide

- **What `ferrytest` exports, and the shape of the merged proof**: [#35](https://github.com/onhotpath/ferry/issues/35).
  This ADR hands it two obligations rather than suggestions: the golden column must be on the proof that runs through the entry point, and the driver suite gains a golden artefact case.
  It does not decide either spelling.
- ~~**Closing the seven admitted kinds with no proof row**: [#35](https://github.com/onhotpath/ferry/issues/35), via [#41](https://github.com/onhotpath/ferry/issues/41)'s D18.~~
  *(Closed on the tip at `0d86c00` while this ADR was in review, and again by [ADR-0014](0014-what-ferrytest-exports.md)'s own table.)*
  The reason it was not housekeeping stands: an admitted member with no row is in tier three by accident.
- **Whether any golden row should change before v1.**
  This ADR makes changing one cheap now and expensive later; which ones are wrong is a question for whoever finds one.
- **The migration tooling.**
  The five-line program is a program, not a command, and whether a `cmd/` ever ships is ADR-0001's bucket rule when somebody proposes one.
  ADR-0002 reserves the directory.
- **What a driver's golden artefact case may assert about formatting.**
  Indentation and key order are the driver's and ADR-0001 already refuses to constrain them; where the line falls inside a golden artefact is [#35](https://github.com/onhotpath/ferry/issues/35)'s.
- **The error types a refused load produces**: [#11](https://github.com/onhotpath/ferry/issues/11)'s ADR-0011 convention, applied rather than invented.

## Consequences

- **ferry publishes two interfaces and now says so.**
  The Go API is versioned by semver and checked by `apidiff`; what a plane holds is versioned by the same numbers and checked by nothing the toolchain ships.
  A reader who assumes semver covers their stored data has been told otherwise in one place.
- **A representation change costs a major version**, which is a heavy instrument for a one-line change and is the only instrument that works.
  Measured: `go get -u` moves a consumer across a minor and changes what their program writes; it cannot move them across a major, because that is a different import path.
- **There is no second version number**, because nothing would read it and ferry cannot write it into a plane without making a format decision the plane-agnosticism veto forbids.
- **The promise is exactly as wide as the golden column**, and structurally cannot cover the text arm at all.
  *(As published this said "eleven rows against eighteen admitted members today"; that count was closed on the tip at `0d86c00` while this ADR was in review.
  The rule is unchanged and nothing currently fails it.)*
  ferry states three tiers instead of one promise, and the third tier's honest content is "nobody chose this".
- **The golden column is reclassified**, from a test fixture to the published artefact.
  That costs nothing to build and changes what a contributor is doing when they edit one.
- **And it does not exist in the harness that runs.**
  There are two proof types on the prototype and `CoreTypes()` is the one without the column, so the instrument this ADR rests on has never executed against the engine.
  This is the single most consequential thing this ADR found, and it is [#35](https://github.com/onhotpath/ferry/issues/35)'s to fix.
- **A registrant carries their own plane compatibility, and nothing tells them so at the call site.**
  ADR-0009's diagnostic mechanism is measured unavailable, because a registration has no previous release to compare against.
  The golden column in their own CI is the substitute, and it only helps a registrant who wrote a proof - which ADR-0001 has always permitted them not to.
- **A driver's spelling is the tier most exposed to accident**, because a self-consistent reader and writer pass every round-trip case, and the conformance suite gains a case of a kind it did not have.
- **ferry cannot read what an older ferry wrote**, deliberately.
  The migration is five lines of the user's own code, it uses a capability ADR-0001 already Enabled, and it terminates - the old codec refuses the migrated file afterwards, which is the deprecation signal a dual read never gets.
- **v0 is the whole mitigation and the window is now open.**
  ADR-0002's v1 trigger gains a second input, and after v1 every golden row is a promise with a major version behind it.

## Items from the xload survey

The survey is [`docs/research/generics-and-modern-go.md`](../research/generics-and-modern-go.md), section 5, against `github.com/gojekfarm/xtools` at [`a90b3aa`](https://github.com/gojekfarm/xtools/commit/a90b3aad2133248cec50f6b4d6e37b0d9e788adb).

**5.8, type information destroyed at the boundary.**
Bears on this ADR from an angle ADR-0001 and ADR-0005 did not cover.
Those two answered *what* the boundary carries; this one answers what happens when the answer changes.
5.8's own instance is a driver flattening a YAML list into an empty string, which is a defect rather than a representation choice - but the repair, a driver that decides how a `Value` is spelled on its plane, creates the tier-three exposure this ADR measures: a driver may change that spelling in a minor release and every round-trip case stays green.
The answer is the golden artefact case, and it is new work neither earlier ADR anticipated.

**5.9, the decoder chain is fixed, one-directional and context-free.**
Bears on this ADR through its third bullet, `time.Duration` matched by `Type.String()`.
xload has no pinned representation at all - it has a chain, and what lands on a plane is whichever arm claimed the type - so it has nothing to make a promise about and nothing to break.
That is worth recording rather than scoring a point: **this ADR exists because ADR-0005 pinned representations, and a library with no pinned representation has no plane compatibility to promise and no migration to offer.**
The cost is the same cost, paid by the user, silently, whenever a dependency adds a method.

**5.11, the YAML provider silently discards parse errors.**
Bears on the dual read specifically.
A read-old arm that fails to parse the old form and falls through to a zero value would be 5.11 exactly, manufactured by ferry rather than by a provider.
The dual read as built does not do that - it returns the error - and it is worth naming, because "accept the old form" is one implementation slip away from "accept anything".

**5.14** was enumerated rather than assumed, all four items.

- *Two ways to set the loader.*
  Bears on this ADR in one place: the migration in [Read-old-write-new is refused](#read-old-write-new-is-refused-and-it-was-built-first) uses two registries in one program, which is two representations for one type in one process.
  It is not the defect's shape, because they are never both installed - ADR-0009's registry is a value and the schema cache is keyed by it, so the two schemas are two schemas.
  Verified by running the migration: an earlier draft of the probe mutated core's own identity table instead, and the second `Dump` was served the first `Load`'s cached schema.
- *The `CanAddr` loop that can only run once.*
  Bears on nothing here.
- *The non-deterministic select on a cancelled context.*
  Bears on nothing here; concurrency is [#20](https://github.com/onhotpath/ferry/issues/20)'s.
- *Value receivers on `Error()` where pointers are returned.*
  Bears on nothing here.
  The refusals this ADR produces are ADR-0011's, applied.

The remaining items are unaffected by this ADR.
