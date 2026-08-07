# Plane compatibility

ferry makes two compatibility promises, not one.
This is the second one: what a plane holds.

The decision record is [ADR-0013](../adr/0013-what-a-plane-holds-is-a-published-interface.md).

## The two promises

> **API compatibility** is semver over the Go API, unchanged.
>
> **Plane compatibility** is the promise that, for a given Go value, a given module version writes the same text into a plane that the previous version wrote.

The second one is a property of an **artefact** rather than of a build, and no tool in the Go toolchain can see it.

That was measured rather than assumed.
Two modules with an identical exported API and one line different in a function body, one writing `"30s"` and the other `"30000000000"`:

| check | reports |
| --- | --- |
| `go build ./...` | nothing |
| `go vet ./...` | nothing |
| `gofmt -l` | nothing |
| `apidiff v1 v2` | nothing |
| the consumer's own round-trip test, on v1 | ok |
| the same test, on v2 | ok |

The consumer's test is the sharpest row.
It is the round-trip property, which is what a library asks a user to write, and it passes on both versions because nanoseconds round-trip perfectly.
A property test cannot see a representation, so neither can the user who wrote one.

> Semver describes the Go API, and the Go API is not the only thing a mapper publishes.

**Plane compatibility is stated per module.**
Core and each driver are versioned independently, so each publishes its own.
Core's covers the text a `ferry.Value` carries; a driver's covers how that `Value` is spelled on its plane.

## The three tiers

Only one of them is ferry's.

| tier | who chose the representation | who promises it | how it is checked |
| --- | --- | --- | --- |
| **pinned** | core, in the identity table and the kind table | core, at core's major version | core's golden column, `ferrytest.CoreTypes()` |
| **the registrant's** | whoever registered the codec | the registrant, at their own major version | their own golden column, in their own CI |
| **nobody's** | nobody | **nobody, and ferry says so** | nothing can |

Which tier every member of the type set is in is in [the type set guide](types.md#the-table).

### Tier one is smaller than it looks

`ferrytest.CoreTypes()` is nineteen rows and 58 cases (ADR-0014), covering all eighteen members core admits.

**The promise is exactly as wide as that table.**
An admitted member with no golden row is in tier three by accident rather than by decision, which is why `ferrytest.Complete` joins the table against core's own set rather than trusting a count.
Run for the first time against the table it replaced, that check reported seven admitted members with no proof, and they were the integer widths nobody would think to doubt (ADR-0005).

### Tier two is yours, and the diagnostic cannot carry it

A registered codec's representation is the registrant's, at the registrant's own major version.

ferry communicates most obligations through a diagnostic - `.AsMapKey()` works because a map keyed by an unopted type does not compile - and that mechanism is unavailable here, because at the moment of registration the previous release is not in the room:

```
NewRegistry(a codec)                              -> a registry, and no error
NewRegistry(TWO codecs for the same type)         -> ferry: T is already registered
```

Within one process there is one representation per type.
Across two releases of your own package there is nothing to compare against.

> **A proof with a golden column is a change detector.**

A registrant who pinned `string("api:80")` and then changes their codec has a red test in their own CI, on their own schedule, before they tag.
That is the same instrument as core's: one mechanism, two owners, and core invents nothing.

See [proving your codec](types.md#proving-your-codec).

### Tier three is the honest part, and it is large

Two whole classes of type land on a plane with a representation ferry never chose:

- **Admitted by kind, where the kind does not fit the type.**
  A `[16]byte` UUID, `json.RawMessage`, `[]rune`.
  See [category three](types.md#category-three-a-representation-nobody-chose).
- **Claimed by the text arm.**
  Any type declaring `encoding.TextMarshaler` and `encoding.TextUnmarshaler` gets its representation from its own author.
  **That set is unbounded and unenumerable**, so no completeness check can reach it, and a type in it round-trips under whatever relation its author had in mind rather than under one ferry can state.

**ferry cannot promise what it never chose.**
Saying so is more useful than a promise that quietly excludes most of what users actually store.

If you want a promise about one of these, register a codec for it and write the proof.
That moves it to tier two.

## The golden column

The golden column is plane compatibility written down.

**Core's**, in `ferrytest.CoreTypes()`, is the third column of every proof: the boundary `ferry.Value` that dumping this Go value must produce.

```
time.Duration  30s                    ->  string("30s")
time.Time      2026-08-02T12:00:00Z   ->  string("2026-08-02T12:00:00Z")
[]byte         {0x00,0xff,0x41}       ->  bytes("\x00\xffA")
float64        0.1                    ->  number("0.1")
uint64         max                    ->  number("18446744073709551615")
bool           true                   ->  bool("true")
```

**A driver's**, in `ferrytest.Plane.Golden`, is the plane contents that dumping a fixed value must produce, byte for byte:

```go
Golden: []ferrytest.Artefact{
	ferrytest.Golden(struct {
		B []byte `ferry:"b"`
	}{[]byte("hi")}, "b: !!binary aGk=\n"),
},
```

Both exist because **a round trip structurally cannot see a representation**.
A round trip tests a function against its own inverse, and a spelling is a choice of function, so changing both halves together is invisible to any test that only composes them.

Measured: with `time.Duration`'s codec replaced by one writing a nanosecond count, the round-trip property reports zero failures on the memory plane, through the real YAML driver, and on a flattening plane - and what it writes is `/t = string("30000000000")` (ADR-0013).

The same at the driver's boundary: `driver/yaml`'s `Bytes` spelling changed from base64 to hex is a one-line edit touching both halves, and both spellings round-trip:

```
base64, as the driver ships    b: !!binary aGk=      round-trips: true
hex, a one-line change         b: !!binary 6869      round-trips: true
```

Green in both worlds, and every file the previous version wrote is now garbage.

This is not byte-level plane fidelity, which ferry rules out by design.
ferry does not require that a **user's** comments, whitespace and key order survive a load and dump cycle.
A golden artefact asserts the **driver's own output** for one fixed input.

## An old artefact under a new rule

Seven representation changes, run against a stored artefact:

| change | stored | outcome |
| --- | --- | --- |
| `time.Duration`: `30s` to nanoseconds | `"30s"` | **loud**, invalid syntax |
| `time.Time`: RFC 3339 to Unix seconds | `"2026-01-15T12:00:00Z"` | **loud**, invalid syntax |
| `[]byte`: raw to base64, a 2-byte payload | `"hi"` | **loud**, illegal base64 |
| `int`: base 10 to base 16 | `"10"` | **silent, wrong**: reads as 16 |
| `[]byte`: raw to base64, a 4-byte payload | `"data"` | **silent, wrong**: three other bytes |
| `float64`: shortest to `%.17g` | `"0.1"` | silent and correct |
| `bool`: `true` to `1` | `"true"` | silent and correct |

Three loud, two silently wrong, two silently compatible.
The two base64 rows are **one change over two payloads and they disagree**.

So the promise cannot be stated over the outcome, because the outcome is a property of the pair of versions *and* of the stored value.
It has to be stated over the **text**.

The two silently-compatible rows are safe *for now*: the new parser is a superset of the old spelling by accident rather than by design, nothing stops the next change from narrowing it, and nothing recorded that anyone was relying on it.

## The version rule

> **A change to a golden row is a major version of the module that owns it, and it ships with a written migration.**
>
> There is no separate ferry-specific version number.

The rule rests on one property of the Go module system, measured against a real file-based `GOPROXY`:

```
at v1.0.0, the program prints           30s
go get -u ./...                         upgraded example.com/cfg v1.0.0 => v1.1.0
after the upgrade, it prints            30000000000

v2.0.0 is published, with the same change
go get -u ./... again                 -> nothing
go.mod mentions /v2                   -> false
```

The minor release arrived on a `go get -u` and changed what the program writes.
The major release did not arrive and could not, because `example.com/cfg/v2` is a different import path and nothing upgrades across one.

> A major version is the only release in the Go module system that a consumer cannot receive without editing a line.
> A representation change is precisely a change that must not arrive without somebody editing a line.

A ferry-specific version number was considered and has neither property: nothing in the toolchain reads it, and writing a version marker into a plane is a format decision ferry rules out by name.

Two sharp edges, stated rather than buried:

- A major version is a heavy instrument for a one-line spelling change, and it forces an import-path edit on every consumer including those who store nothing.
  That is accepted: it is the same instrument for the same reason `encoding/json/v2` is a new import path.
- A major version is **sufficient but not necessary**.
  A major release for an ordinary API reason may also change a representation, and nothing stops it.
  So the rule is one-directional, and the written migration is what carries the information the version number cannot.

## ferry cannot read what an older ferry wrote

That is deliberate, and read-old-write-new was built first and then refused.

A dual-read `time.Duration` codec, writing `30s` and accepting both `30s` and a nanosecond count, has four properties:

1. **It works.** A stored `"30000000000"` loads as `30s`.
2. **And the first dump rewrites the artefact anyway.**
   Every stored file changes on the first write after the upgrade, so the migration happens regardless - unannounced, one file at a time, in whatever order processes happen to dump.
   A read-old path does not *avoid* the migration, it performs it silently.
3. **The golden column stops being a column.**
   A proof pins one text; a dual-read codec has one on `Dump` and two on `Load`, so the triple cannot state the decode side at all.
4. **It is permanent**, because nothing can tell you whether any plane still holds the old form.
   A recording sink records what ferry **wrote** and never what the plane **held**, so a deprecation cycle has no signal to end on.

It is also a second coercion on its own terms.
`String` is admitted as the universal donor because `String` is what a plane says when it has nothing to say.
A superseded form is not that: it is ferry second-guessing text a plane asserted.

## The migration

Plane-to-plane transfer is a capability that falls out of the pluggable design for free.
A representation migration is that capability with the same file on both ends and two versions of the codec.

**Five lines of your own code** (ADR-0013):

```go
cfg, err := ferry.Load[Conf](ctx, yaml.NewSource(p), ferry.WithRegistry(old))
if err != nil {
	return err
}

return ferry.Dump(ctx, cfg, yaml.NewSink(p), ferry.WithRegistry(current))
```

Run against a real file, with `old` holding a nanosecond codec for a named duration and `current` holding `ferry.DurationLike`:

```
before                        t: "30000000000"
after                         t: 30s
reload under the new codec -> 30s, err=<nil>
the OLD codec on it        -> ferry: /t: what is set here is not a valid Timeout
```

The last line is the property a dual read does not have and cannot have.
Three of them:

- it is a **decision**, taken once, by a person, at a time they chose;
- it is **observable**, having succeeded or not over a file list they hold;
- it **terminates**, and the old codec is deleted afterwards, because the migrated data refuses it.

The same five lines cover a **driver-side** spelling change, with the two registries replaced by two driver versions and the source and sink naming two module paths.

What it costs, plainly: a mapper that can only read what its own version wrote is a worse migration story than the config files it replaced.
The answer is not that the cost is small.
It is that the alternative pays the same cost invisibly and then charges rent.

## ferry is at v0, and this is the window

v0 is the only place semver allows a decision to be taken back.
The trigger for v1 is the tag grammar surviving real use, and the golden column: not because it must be complete, but because at v1 it becomes a promise with a major version behind it, and it is cheap to change today and expensive after anything is stored.

## The pinned `encoding/json/v2` option set

"First class `encoding/json/v2` support" means an explicitly pinned option set and never inherited defaults.
The pinning has two halves, and they must be stated together because their agreeing with each other is the whole point (ADR-0005).

**If core and a ferry module disagreed about nil, about map order, or about integer precision, ferry would have grown a second conversion authority.**

### Half one: core imitates v2's Go-defined semantics and imports nothing

Core takes no non-stdlib dependency and never imports `encoding/json/v2` or `encoding/json/jsontext`.
CI asserts that core builds under `GOEXPERIMENT=nojsonv2`, and `depguard` denies both imports in every module.

| v2 semantic | ferry | why |
| --- | --- | --- |
| `omitzero`, defined in Go terms | adopted as the model | portable to a non-JSON plane |
| `omitempty`, defined in JSON terms | not adopted | there is no "empty JSON object" on a Consul plane |
| case-sensitive matching | adopted | core never folds |
| a duplicate name is an error | adopted | prefix-freeness plus driver injectivity |
| deterministic output | adopted | measured at 1 ordering over 300 |
| a nil slice and map are `null` | adopted, and widened to empty | the forced collision at a container address |
| `time.Duration` has no representation | **rejected** | ferry gives it one, and says so |

### Half two: any ferry module that calls v2 constructs its options from one pinned set

**And never passes none.**

The option surface was enumerated from `go1.27rc2`'s source rather than sampled: nine behavioural options in `encoding/json/v2`, fifteen in `jsontext`, and thirteen legacy-semantics switches in `encoding/json` (ADR-0005).

| option | pinned to | why |
| --- | --- | --- |
| `Deterministic` | **true** | measured 8 orderings over 50 at the default, 1 with it |
| `FormatNilSliceAsNull` | **true** | v2's `[]` is a shape ferry's address model cannot name |
| `FormatNilMapAsNull` | **true** | as above |
| `jsontext.CanonicalizeRawInts` | **false** | measured: turns `1234567890123456789` into `1234567890123456800` |
| `jsontext.CanonicalizeRawFloats` | **false** | saturates above `MaxFloat64` and canonicalises `-0` to `0` |
| `jsontext.AllowDuplicateNames` | **false** | the default already errors, and ferry's rule is the same rule |
| `jsontext.AllowInvalidUTF8` | **false** | ferry's `String` may hold non-UTF-8; a JSON plane must refuse it loudly rather than substitute |
| `StringifyNumbers` | **false** | ferry's decode always knows the target Go type, so precision needs no quoting |
| `OmitZeroStructFields` | **false** | omission is per field and is the tag grammar's; a global override would silently change every schema |
| `MatchCaseInsensitiveNames` | **false** | core never folds |
| `RejectUnknownMembers` | **false** | ferry maps a subset of a plane, and keys ferry does not map must survive |
| `WithMarshalers`, `WithUnmarshalers` | not pinned | the codec chain is not a global |
| all thirteen `encoding/json` legacy switches | **never set** | they exist to restore v1 semantics; `FormatDurationAsNano` and `FormatByteArrayAsArray` would silently contradict decisions above |
| the remaining `jsontext` options | the driver's | indentation, escaping, spacing, byte and depth limits change bytes or resource use, not meaning |

### It binds the first ferry module that ever imports v2

**No module does today.**
The first-party driver list is `yaml`, `kv` and `env`, and none of them imports `encoding/json/v2`.

So half one is executed and asserted in CI, and **half two is executed by nothing**.
It is pinned anyway, because the first module to import v2 must not be the place this gets decided.

The pinning is provisional in one respect: Go 1.27 was still `go1.27rc2` with `"stable": false` when the option surface was enumerated, so the set and its defaults are re-verified at GA.
