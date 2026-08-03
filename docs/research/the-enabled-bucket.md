# The Enabled bucket, tested: template generation, the Windows Registry, and cross-cutting concerns

Findings for [#14](https://github.com/onhotpath/ferry/issues/14), [#15](https://github.com/onhotpath/ferry/issues/15) and [#10](https://github.com/onhotpath/ferry/issues/10).
All three carry `wayfinder:prototype`, so this document and the code it cites are the artefact and no ADR is proposed.

Code: branch [`proto/14-15-10-enabled`](https://github.com/onhotpath/ferry/tree/proto/14-15-10-enabled), built on `proto/16-entry-point`, which is the tip of `5 -> 7 -> 12 -> 19 -> 16` and the only branch carrying the whole engine.
Run: `T14=<n|all>`, `W15=<n|all>`, `C10=<n|all>`, with `GOTOOLCHAIN=go1.27rc2 go run .` from `proto/`.
Every number below is from that branch unless it cites the survey.
The Windows half runs on GitHub's `windows-latest` in [`.github/workflows/windows-registry.yml`](https://github.com/onhotpath/ferry/blob/proto/14-15-10-enabled/.github/workflows/windows-registry.yml), which is the first CI this repository has had.

## Summary

[ADR-0001](../adr/0001-what-ferry-supports.md) sorts every capability into In core, Enabled, Milestoned or Ruled out, and says Enabled is the default landing place: *"A capability earns its way into core by being un-buildable from outside it."*
All three tickets are Enabled.
This is the first test of whether that was true.

**Enabled survives, and it is not free.**
All three are buildable from outside core, none needs a new core interface, and each one costs something that is not visible from the ADR that bucketed it.

Three results bear on decisions that are already Accepted.

- **Two accepted ADRs say opposite things about the same mechanism and neither had been run.**
  [ADR-0010](../adr/0010-the-entry-point-and-the-schema-cache.md) keeps the read-only schema view closed because *"template generation reaches the defaults through a recording sink"*.
  [ADR-0006](../adr/0006-defaults-and-zero-values.md) says the opposite from the other end and assigns the problem here.
  Run, ADR-0010's sentence is true only for a struct with no `required` field, and `required` is what a template exists to announce.
- **ADR-0004's stated reason for `Enumerator` returning addresses does not survive contact with a plane that has no list type.**
  Core never reads the kind the enumerator returns.
  The decision is right and the reason is wrong, and a Registry driver author reading the ADR would conclude they cannot implement the interface honestly.
- **ADR-0004's three optional interfaces compose badly with the wrapping that #10's whole mechanism is made of.**
  A naive middleware over the real YAML sink makes the plane never get written, with a nil error.

Two findings this document originally left open were resolved during review and are now measured rather than proposed: an absent optional section suppresses a `required` child (section 1.5), and the `REG_MULTI_SZ` hole closes with a registered codec plus a driver option, so ADR-0004's value model needs no amendment (section 2.7).
The second corrects a reading earlier in this document.

Six defects were found in the inherited prototype, five of them silent, and two of those were latent defects cancelling each other out.

## 1. Template generation (#14)

### 1.1 The defaulted value is not reachable by either route an accepted ADR names

`T14=1`.

| route | what it produces |
| --- | --- |
| `Dump` the zero value into a recording sink | no defaults at all: `/listen` is `string("")`, not `:8080`; `/timeout` is `0s`, not `30s` |
| `Load` from an empty plane | the defaults are applied inside the walk and **none of it crosses the boundary**, because `required` fires and [ADR-0011](../adr/0011-the-error-model.md) yields no value ferry built |
| the same type with every `required` removed | `{Listen::8080 Timeout:30s}`, which is the case ADR-0010's sentence is true for |

A Dump carries no defaults because ADR-0006 makes a default a Load-side rule, in as many words: *"A default is a Load-side rule.
Dump writes the value in hand and never substitutes a default."*

So ADR-0006's own hand-off is the accurate one: *"`required` fires on a Load from an empty plane, so the defaulted zero value is not reachable by Load alone.
That is #14's to resolve, and the mechanism it needs is the compiled schema holding the defaults."*

### 1.2 The recipe, and what it costs

`T14=2`.
Three core operations, no new interface, written as a function a third party could write.

1. Dump the zero value into a recording sink, which is the only way to learn the boundary `Value` a required address will accept.
   ADR-0011 makes that step necessary: the error set names the address and the class and deliberately does not name the type.
2. Load from a plane holding only what step 3 has learned so far, adding each reported `required` address, until the Load stops failing.
3. Dump the value that Load produced into a second recording sink.

| scheduler | Loads |
| --- | --- |
| aggregating ([ADR-0011](../adr/0011-the-error-model.md)) | **2**, always |
| first error | **k+1** for k required addresses |

The cost is therefore a property of ADR-0011's aggregation rather than of the recipe, and [ADR-0010](../adr/0010-the-entry-point-and-the-schema-cache.md) puts the scheduler behind a seam that a caller outside core can fill.

**The recipe reaches the values and not the declarations.**
The Go type, the declared default *text*, and the `required` marker are in the compiled schema, which ADR-0001 keeps unexported.
`/db/port` reads `number("5432")`, which is the default *applied*, and there is no channel through which an emitter learns that 5432 came from a declaration.

### 1.3 The artefact

`T14=3` emits four real files at four annotation levels.
The finding is not which is nicest, it is what channel each level needed.

| level | needs |
| --- | --- |
| L0, the plain dump | nothing: `Dump` and a `Sink` |
| L1, `+` required markers | ADR-0011's `Elements`/`Address`/`ErrMissing`, **plus an annotation channel `Writer` does not have** |
| L2, `+` Go type and declared default text | the compiled schema, which is unexported |
| L3, `+` prose | a source ferry has nowhere to put |

The emitted L3 artefact, run:

```yaml
# Lines commented out below have no default and ferry refuses to load
# until each one is present. Uncomment and set them.
db:
  # Hostname of the primary Postgres instance.
  # (string, REQUIRED)
  # host:
  # (string, default hunter2)
  password: "hunter2"
  # (int, default 5432)
  port: 5432
# Per-route request ceilings, keyed by route name.
# (map[string]int)
limits: null
# (time.Duration, default 30s)
timeout: "30s"
```

### 1.4 A template that fills in its required addresses defeats `required`

`T14=8`, A2, and it is the sharpest result in the ticket.

The obvious template writes `name: ""` at a required address.
ADR-0006 makes `required` a **presence test** - *"`required` is satisfied by any observation other than `Absent`"* - and the empty string is present.
Measured, loading the emitted template back:

```
Load of the emitted template: err=<nil>
Name=""  DB.Host=""
```

The starter config satisfies every `required` in the schema and the service boots with an empty name and an empty database host.

**The repair is to emit the address commented out**, so it is `Absent`.
ADR-0006 already measured that a commented-out line removes the key.
Measured, the repaired artefact refuses correctly:

```
ferry: 3 errors:
  /db/host: required, and the plane supplied nothing
  /name: required, and the plane supplied nothing
  /tls/cert: required, and the plane supplied nothing
```

That repair is a comment-syntax capability, so on every `values only` plane in section 1.7 the seeded artefact is one a Load refuses until a human edits it.
That is the right answer and it looks like a broken tool.

**yaml.v3 has no disabled-entry node**, so even a plane with a comment syntax has no library-level notion of a key that is present in the document and absent to a reader.
Every emitter does this textually, and this one does too.

### 1.5 `required` under an absent optional section, found here and resolved

`T14=8`, A3.

```go
type TLS struct {
    Cert string `ferry:"cert,required"`   // if you configure TLS, you must give a cert
    Key  string `ferry:"key"`
}
type Conf struct {
    TLS *TLS `ferry:"tls"`                // ...but TLS itself is optional
}
```

Measured against a plane holding a complete config and no `tls` section, this refused with `/tls/cert: required, and the plane supplied nothing`.
So one `required` anywhere beneath an optional pointer made the whole section mandatory and the pointer stopped meaning anything.

ADR-0006 asks the neighbouring question about **defaults** and answers it well - *"a default fills a hole in a section, and never conjures the section"*, measured - and never asks it of `required`.

**Resolved by the repo owner**: when a section is optional as a whole, a `required` inside it does not apply while the section is absent.
The fix is one condition in the walk's nil-pointer branch, and it is ADR-0011's own rule - *"report every failure that is not a consequence of another"* - applied to the mirror of the case it already handles, a required child under a **required** parent.

| plane | scheduler | result | |
| --- | --- | --- | --- |
| no `tls` section at all | aggregating | accepted | correct |
| `tls/key` present, `cert` missing | aggregating | refused | correct |
| `tls/key` present, `cert` missing | **first error** | **accepted** | **wrong** |

**The precondition is not obvious and is why both scheduler rows are printed.**
The fix is gated on ADR-0006's presence bit, which is accumulated across the pointer's siblings.
Under first-error scheduling the failing field returns before its siblings run, so the bit is incomplete and the suppression fires on a section that *is* present - strictly worse than the bug it fixes.

It is sound in ferry as designed, because ADR-0011 says *"on Load, aggregate"* and declines to ship `StopOnFirstError`, calling it *"a public knob whose only job is to make ferry report less"*.
What it hands [#20](https://github.com/onhotpath/ferry/issues/20) is a **second** hazard on the presence bit: ADR-0010 already reports a data race on it, and it is now load-bearing for a correctness rule, so a scheduler that abandons siblings on the first error breaks this in the silent direction.

**One cost falls out rather than being chosen**, and it is #14's.
A template can no longer *discover* a conditionally-required field: the recipe learns required-ness by reading the error set, and the error is now correctly suppressed, so a generated template renders the section as `tls: null` and cannot say "if you add a tls section, cert is mandatory".
That is the price of reading declarations out of an error set rather than out of a schema, which is section 1.7's wall in a different place.

**A separate inherited defect surfaced alongside it.**
`required` on the pointer *itself*, which ADR-0006 decides means *"the plane supplied at least one of the address's static children"*, is enforced by nothing on `proto/16-entry-point`.
ADR-0006 explicitly records repairing that in its own draft; the repair was never carried onto the branch every later ADR was measured against.
With it in place the two spellings mean two useful and distinct things - `tls,required` for a mandatory section, `cert,required` for mandatory-if-present - and today only the second does anything, and it does the first one's job.

### 1.6 The annotation channel is the easy half

`T14=4`.

An optional `Annotator` interface discovered by assertion works, and is exactly ADR-0004's `Committer`/`Releaser`/`Enumerator` pattern.
It is not the problem.

Of the four things L2 prints, where each comes from:

| | |
| --- | --- |
| the value at `/db/port` | the recording sink |
| `REQUIRED` at `/db/host` | ADR-0011's error set |
| the Go **type** | nowhere in ferry's surface |
| the declared default **text** | nowhere in ferry's surface |

So an `Annotator` would give an emitter somewhere to put two facts it cannot obtain.

The available substitute is a second `reflect` walk in the emitter.
It works, and it is [ADR-0010](../adr/0010-the-entry-point-and-the-schema-cache.md)'s walk-duplication axis 1 by construction.
Reproduced against a real divergence rather than described: an emitter that reimplements the field rule and handles an embedded field the obvious way disagrees with the compiler, annotates `/port` and silently leaves `/env` bare, with no error from anything.
ADR-0008 found the identical defect in a real ferry prototype and called it silent.

### 1.7 Which planes can be templated, and what one that cannot reports

`T14=5`.
**The predicate is "has a comment syntax", not "has a format", and it is a strictly smaller set.**

| plane | has a Dump | comment | what a template can be |
| --- | --- | --- | --- |
| yaml, toml | yes | yes | full template |
| json | yes | **no** | values only |
| env, the process | no (ADR-0002) | n/a | not a sink at all |
| `.env` file | yes | yes | full template, but a different plane |
| kv, Consul, Vault | yes | **no** | values only |
| query parameters | possible | **no** | values only |
| Windows Registry | yes | **no** | values only |

A JSON emitter has two options for `# (REQUIRED)` and neither is a template: drop it, or invent a `"//db/host"` key, which is a new address in a key space ADR-0003 says is the schema's and which the matching Load reads back as an unmapped key.

**So the feature is two features and only one of them is new.**

- **Seeding**: write the defaulted values to the plane.
  Every writable plane, and it is exactly `Dump` of the value the recipe produces.
  Needs nothing that does not exist.
- **Documenting**: emit an artefact a human edits, carrying the markers.
  Needs a comment syntax, and needs facts `Writer` cannot carry and ADR-0001 keeps unexported.

ADR-0001 describes template generation as *"dump a defaulted struct to a starter config a user can edit"*, which is the first clause describing seeding and the second describing documenting.

A plane that cannot annotate cannot silently drop the markers, because ADR-0001 rules out ignoring anything silently.
The two available answers are to refuse at the generator - the assertion is available at bind time, which is the same before-any-I/O property ADR-0004 buys with a context-free `Bind` - or to degrade to seeding and say so in the return value.

### 1.8 Where the prose comes from

`T14=6`.
Three candidate sources, all run.

- **A tag option** is closed. `Compile` with `ferry:"name,desc=..."` gives `unknown option "desc=the service name"`, ADR-0001 freezes the vocabulary on publication, and ADR-0008 measured that a comma appears in 22 of 565 real free-text tag values, so prose in a tag needs the quoted form on roughly one value in twenty-five forever.
- **A Go doc comment** is what a Go programmer already writes, and it was extracted from this prototype's own source with `go/parser`.
  `reflect.StructField` has **no** `Doc` field, verified, so a doc comment is not in the binary and this route is unavailable at run time at all.
- **A caller-supplied side table** works today and spells the address set a second time.
  Reproduced: rename `ferry:"host"` to `ferry:"hostname"` and the prose silently stops applying, with no error from anything.
  ADR-0006 measured the identical failure against a `Static` defaults source.

**The source people want is a build-time source**, which is what makes template generation a generator rather than a function.
ADR-0002 reserves `cmd/` and says the prefix keeps the root namespace free *"because ... #14 may want a command"*.

### 1.9 The API surface

`T14=7`, argued last because the artefact is what rules three candidates out.

- **`Dump` of a zero-valued struct with an Option**, which the ticket names, is refuted on three counts, none of which an Option fixes: the defaults are not applied on Dump, `required` is not knowable from a Dump, and an `omitzero` field and an empty composite are not in the output.
- **A distinct entry point in core** fails ADR-0001's bucket rule rather than its veto. The veto passes, since an annotated starter artefact is not a configuration-only idea.
- **A sub-module at run time** works, and its signature admits the defect: it needs a `Notes` parameter that no ferry call returns.
- **A command reading the Go source** has the prose, the tags and the types from one parse, so `Notes` stops being a parameter.
  What it cannot see is a registered codec: measured, one type is refused against a bare registry and compiles against one with a `StringCodec`, and whether the type is templatable at all is decided by a line of Go in an `init` the generator never runs.

So the answer to *"is it a distinct entry point or `Dump` with an Option"* is neither, and the shape of the question needs revising: seeding needs no entry point, and what decides where documenting lives is the prose question rather than the API.

## 2. The Windows Registry (#15)

### 2.1 Reconnaissance came first, and it earned its place

`W15=0`, on the runner.

The handoff's stated trap for this ticket is *"a Registry key you have write permission on"*.

```
process token elevated              : true
member of BUILTIN\Administrators    : true
create+write HKCU\Software\...      -> <nil>
create+write HKLM\SOFTWARE\...      -> <nil>
create+write HKLM\SECURITY\...      -> Access is denied.
SetStringValue through a QUERY_VALUE handle -> Access is denied.
```

GitHub's `windows-latest` runner is elevated and an administrator, so a permission probe against `HKLM` would have gone green having tested nothing.
The refusal is produced by the access mask on the handle instead, which is a real `ERROR_ACCESS_DENIED` from the same API.

### 2.2 ADR-0003's address model expresses a Registry path, and its worked column is correct

`W15=1`.
ADR-0003 wrote its Registry column from reasoning and said so.
Run against a nine-line key function, its worked example matches **5 of 5** and its four schema predictions match **4 of 4**.

Two things it did not anticipate, both measured on a real hive.

**The Registry has two namespaces per key.**
A value named `db` and a subkey named `db` coexist under one key, measured: `values=["db"] subkeys=["db"]`.
So `/db` and `/db/host` are not one location, and ADR-0003's prefix-free rule is **strictly stricter than this plane needs**.
ADR-0003's justification is that core adopts the constraint tree planes impose so a schema is representable on every plane, and its own four-planes table puts `ok` in the registry column for the neighbouring row.
The cost it priced as *"a schema nobody writes deliberately"* is confirmed, and it is paid on a plane its table calls a tree.

**The plane folds case and preserves spelling.**
`ReadValueNames` reports `CaseProbe` after a case-insensitive match on `caseprobe`.
So a driver must fold when **checking** injectivity and write the **original** spelling.
ADR-0003 frames folding as part of the key function - *"a driver may fold, as part of its key function, when its plane genuinely is case-insensitive"* - which is right for env, where the plane neither folds nor preserves, and silently breaks a map key here.
Measured on the runner with the corrected shape: `map[Prod:1 staging:2]` round-trips exactly and the hive holds `["Prod" "staging"]`.
A driver that folded its emitted key would write `prod`, and ADR-0003's injectivity rule cannot catch it, because one key is trivially injective.

### 2.3 `Enumerator`'s stated justification does not survive

`W15=2`, and this is a finding against a reason rather than a decision.

ADR-0004 made `Enumerator` return addresses rather than names, with one reason: *"the plane answers which composite it is rather than the caller guessing from base-10 text - the limitation ADR-0003 quotes `jsontext.Pointer`'s own godoc admitting."*

The Registry has no array type, so a `[]string` and a `map[string]string` with numeric keys are byte-identical in a hive and it **cannot answer**.

Measured, the same hive read into both Go types, by a source forced to report each kind:

```
plane says Name (all a Registry can say)      []string -> [a b c]
                                              map      -> map[0:a 1:b 2:c]
plane says Index (a guess from base-10 text)  []string -> [a b c]
                                              map      -> map[0:a 1:b 2:c]
```

All four agree, because **core never reads the kind the enumerator returned**.
The walk takes the last segment's text and decides from `n.kind`, which is the compiled schema's.

The decision is right and the reason is wrong, and the correct reason is stronger: the schema has to be the authority, because ADR-0010 makes the address set a field of the thing the walk iterates precisely so the compiler and the walk cannot disagree.
A plane that could answer would be a second authority on the same question.
Returning a `Path` remains right for two reasons ADR-0004 did not give: it is already the type the caller needs, and it carries the escaping, so a child name containing the rendering's punctuation needs no second convention.

This matters because a driver author reading the ADR would conclude a Registry driver cannot implement `Enumerator` honestly, and one reading the code would find it can.

### 2.4 The values fit and the types do not

`W15=3` against a fake with the hive's storage model, `W15=4` against a real hive.

A six-field struct round-trips exactly on the runner, `equal=true`:

```
big        REG_QWORD(1099511627776)
blob       REG_BINARY("\x00\xffA")
name       REG_SZ("svc")
neg        REG_SZ("-1")
port       REG_DWORD(8080)
timeout    REG_SZ("30s")
```

Four findings, in order of severity.

**A Go `bool` cannot round-trip.**
The Registry has no boolean and every convention in the wild is a `REG_DWORD` holding 0 or 1.
That arrives as `Number`, and ADR-0005 refuses `Number` at a `bool` deliberately, because accepting it would be ferry overriding a plane's own type information.
Measured on a real hive, a `REG_DWORD 1` written by any other program gives `On=false err=ferry: /on: value: wrong kind`.
The two honest driver choices are "what every other Windows program reads and ferry cannot load" and "what ferry round-trips and nothing else recognises".

**`Value`'s `Number` carries no width, so a Load-Dump cycle rewrites the plane's own type.**
A `REG_QWORD(8080)` read and written back is a `REG_DWORD(8080)`.
Value fidelity holds and driver fidelity does not, and the conformance suite as ADR-0001 describes it compares keys and values, so the value is equal and it would not catch this.
`REG_EXPAND_SZ` loses its type the same way, and the semantics with it: `REG_EXPAND_SZ` tells every Windows reader to expand the variable and `REG_SZ` tells it not to.
A driver could read before writing, which makes every `Set` a `Get` first - survey item 5.13's amplification - and still has nothing to preserve on a key that does not exist yet, which is every template and every first run.

**`REG_MULTI_SZ` has no representation in the six kinds**, and section 2.7 is what closes it.
Measured on a real hive, `["a" "b,c" ""]` round-trips through Win32 exactly, including the element containing a comma and the empty element.
It is a real, lossless, native list of strings at **one** value name.
ADR-0004 closed the model with no group arm on the argument that a composite gets one address per element so nothing ever asks the plane for the value *at* `/servers`, and named the remaining case as a flat plane holding a whole list in one value, arriving as `String("a,b,c")` for a codec to split.
`REG_MULTI_SZ` is not that case: the elements are separately delimited by the plane, so there is nothing for a codec to split and no delimiter to choose.

**A negative number has no Registry integer type**, so one Go field gets two plane types decided by the value rather than by the type.
ADR-0005's golden column pins a representation per Go type, and here the representation is a function of the value.

What does fit: `REG_SZ` to `String` exactly, `REG_BINARY` to `Bytes` exactly including a NUL and a non-UTF-8 byte, `REG_NONE` to `Null`, and the integers exactly as values.

### 2.5 The sink shape holds, and permissions land where ADR-0004 puts them

`W15=4`, on the runner.

```
ReadOnly sink               -> plane is read only: registry: the key was opened without KEY_SET_VALUE
errors.Is(err, ErrReadOnly) -> true
```

ADR-0004 puts the read-only refusal inside `OpenWriterFunc` rather than at `Bind`, and the Registry is the plane where that is most obviously right: the access mask is chosen when the key is **opened**, so nothing at bind time could have known.

The Registry sink cannot implement `Committer`, because the subkeys a write needs are created as it goes.
Under ADR-0011 that means it gets the encode phase rather than interleaved aggregation, and pays for an untouched plane in round trips.

### 2.6 The second data point #25 asked for

`W15=5`, written against [ADR-0012](../adr/0012-the-caller-held-binding.md) rather than #25's open options, because it was accepted while this ran.

**The ordinary Registry case confirms ADR-0004 as written.**
ADR-0012's discriminator is *"does the caller obtain this value freshly for each load?"*, and for an ordinary driver the answer is no: `HKCU` is already the current user's hive, the subkey path is a constant, and the `HKEY` opens inside `OpenFunc`, which is exactly where ADR-0004 puts a plane's handle.

**The conflation is nonetheless real here, for a second and independent cause.**
For the Registry the key table is a pure function of the address set and the base path and does not depend on the hive at all.
So for query parameters the two lifetimes come apart because the plane is constructed per request, and for the Registry they come apart because the key table does not depend on the plane.
Two causes, one conflation, which answers #15's question of whether it is a general problem or a query-params problem: **general**.

**ADR-0012's context rule generalises to it unchanged.**
A service reading a different user's hive per request obtains the plane freshly per load, so the discriminator puts it in the context.
Run, one bind and two hives:

```
hive 1 -> {Name:tenant-one DB:{Host:db1 Port:5432}}
hive 2 -> {Name:tenant-two DB:{Host:db2 Port:5433}}
no hive in the context -> err=registry: no hive in the context
```

**And ADR-0012's key-helper amendment is not optional for this plane.**
It measured the retained-minted-set defect on env's `http-port` against `http_port`, where the fold is the driver's choice.
Here the fold is the plane's, so a driver cannot decline it.
Measured, two tenants holding `Prod` and `prod`:

```
minted set on the binding  [write 1 ok, write 2 REFUSED (collides with "Prod")]
minted set on the open     [write 1 ok, write 2 ok]
```

### 2.7 The `REG_MULTI_SZ` hole closes with a codec and a driver option, and needs no amendment

`W15=6`, `W15=7`.

**First, whether it is a real case or one this ticket reached for.**
Read-only on the runner's own install, reporting type and element count and never the data:

| value | type | elements |
| --- | --- | --- |
| `Services\Dnscache\DependOnService` | `REG_MULTI_SZ` | 2 |
| `Services\LanmanWorkstation\DependOnService` | `REG_MULTI_SZ` | 3 |
| `Session Manager\Memory Management\PagingFiles` | `REG_MULTI_SZ` | 1 |
| `Control\Network\FilterClasses` | `REG_MULTI_SZ` | 15 |
| `Session Manager\PendingFileRenameOperations` | not present | |

Four of five on a stock image, so it is ordinary.
The fifth is a real `REG_MULTI_SZ` value and only exists while a reboot-pending rename is scheduled, so it is a poor example and is listed as one.

**Second, the write-side damage, which is worse than the read side.**
What ferry writes today for `Deps []string` tagged `DependOnService`:

```
services\acme\dependonservice : 0 = REG_SZ("RpcSs")
services\acme\dependonservice : 1 = REG_SZ("Tcpip")
```

A **subkey** holding values named `0` and `1`, where Windows expects one `REG_MULTI_SZ` value of that name.
The service control manager does not see a dependency list at all.
So this is not only a type ferry cannot read, it is a type ferry actively destroys on a round trip.

**Third, the route, which was built rather than reasoned about.**
A named type with a registered codec, using ADR-0005's own mechanism - *"a codec collapses a type to a leaf, and a leaf needs no address set"*:

```go
type MultiSZ []string
reg.Register(ferry.ValueCodec(ferry.KindBytes, encNULJoined, decNULJoined))
```

| | address set | plane holds | round trip |
| --- | --- | --- | --- |
| plain `[]string` | `/deps#0`, `/deps#1` | a subkey of numbered values | through ferry only |
| codec alone | `/deps` | `REG_BINARY` | **exact** |
| codec plus a driver option | `/deps` | `REG_MULTI_SZ(["RpcSs" "Tcpip" ""])` | **exact** |

The NUL join is lossless rather than a delimiter choice, because the Win32 format is NUL-separated so an element cannot contain one.

**And the codec cannot close it alone, for a reason worth stating precisely.**
A codec's entire output is a `Value`, which is a kind and text, and none of the six kinds means `MULTI_SZ`, so there is no channel through which it tells the driver what it produced.
The encoding cannot be self-describing either: NUL-joined bytes are indistinguishable from a genuine binary blob containing a NUL, so a driver that guessed would corrupt the second case.
Declaring `String` instead fails at the Win32 level, because a `REG_SZ` is NUL-terminated and cannot carry an embedded NUL.

**So the missing half is a driver option, which is the shape ADR-0003 already gives the separator on exactly this reasoning, and ADR-0004's value model needs no amendment.**
This corrects an earlier reading in this document, which had `REG_MULTI_SZ` as the first measured plane needing the escape arm ADR-0004 calls *"the weakest call in this ADR"*.
It is not: the escape arm would carry a driver-native value, which ADR-0004 refuses because it breaks plane-to-plane transfer, and the codec route carries plane-neutral bytes instead.

**What it costs the user, which is the part that should decide whether ferry is happy with this.**
Three things for what is in Go a `[]string`: define a named type, register a codec, and configure the driver.
Steps two and three spell the same fact twice, once as a type and once as an address predicate, which is the drift ADR-0006 measured against a `Static` defaults source and section 3.3 measured against a redaction table.
And the failure mode of doing none of them is silent on the plane: a plain `[]string` compiles, dumps, and round-trips through ferry perfectly while writing a shape no Windows consumer recognises.

## 3. Cross-cutting concerns (#10)

Held until ADR-0012 was accepted, because a middleware wraps a `Source` and #25 owned whether the plane instance still arrives at construction.
It does, and a per-request driver's context plane passes through a wrapper untouched.

### 3.1 The mechanism needs nothing new, in either direction

`C10=1`.
A `Source` wrapping a `Source` sees every boundary `Value` on Load; a `Sink` wrapping a `Sink` sees every write on Dump.
ADR-0012 already shipped the first shape as its answer to ADR-0006's presence observation.
No ferry-declared `Middleware` type is needed, and ADR-0001's bucket rule therefore keeps cross-cutting concerns Enabled.

### 3.2 A naive wrapper makes the plane never get written

`C10=2`, and this is the sharpest result in the ticket.

```
no wrapper at all        dump err=<nil>  file exists=true    bytes=52
the naive wrapper        dump err=<nil>  file exists=false   bytes=-1
the forwarding wrapper   dump err=<nil>  file exists=true    bytes=52
```

ADR-0004 has three optional interfaces *"discovered by assertion and never required"*, and weighed the case where the thing failing the assertion is a **driver**:

> A sink that needs `Commit` and omits it writes nothing at all, silently, which is exactly what ADR-0001 rules out.
> Measured: it fails the first case in the driver-fidelity suite.

The thing failing the assertion here is a **wrapper over a driver that satisfies it**.
A wrapper is not in `driver/*`, so the CI glob never runs the suite against it, and the mitigation ADR-0004 relies on does not reach it.

Each interface fails differently and only one is loud:

| dropped | what happens | loud |
| --- | --- | --- |
| `Committer` | the staging sink never commits, nothing is written | **no**, nil error |
| `Releaser` | the temp file leaks, measured at 3 of 3 dumps | **no** |
| `Enumerator` | a map field is refused, naming the source as unable to enumerate when it can | yes |

**Go embedding does not rescue it**, measured rather than assumed: a wrapper embedding the `FWriter` **interface** does not implement `FCommitter`, because embedding an interface promotes only its own method set.
Embedding the concrete type would work and is unavailable, since a wrapper takes an `FSink` and does not know what it wrapped.

So the trade ADR-0004 made is between boilerplate in every driver and a silent failure in every wrapper, and it weighed only the first.

### 3.3 Redaction on dump

`C10=3`.

**A wrapping Sink can guarantee a secret never reaches the sink.**
It sees every `Set` before the inner sink does, and that is a property of ADR-0004's contract rather than of the wrapper.

**It cannot guarantee a secret never reaches the plane**, because section 1.7 established that an annotated template emitter has to bypass `Sink` entirely.
Measured: the emitted template contains the credential while the redacted dump does not.

**And the wrapper has to be told which addresses are secret.**
All three sources are closed:

- a tag option - ADR-0008 refused `nodump` and `readonly` by name, because *"a field ferry loads and never writes cannot round-trip"*, which is the violation redaction is;
- a schema view - ADR-0001 left it open, ADR-0010 and ADR-0012 both declined to reopen it;
- a caller-supplied side table - which drifts, reproduced: rename the tag and the redaction silently stops.

A dynamic address is a further hole: a map key's address comes from the value, so a predicate over a fixed address list cannot name `/creds/prod`, and the middleware needs a second address language.

**A probe here predicted the opposite of what it measured, and the correction is kept.**
The prediction was that ADR-0009's `dec(enc(zero))` check would refuse a redacting codec.
It does not, and the reason is stated in ADR-0009 rather than being an oversight: the check asks whether the round trip **errors** and deliberately not whether the value returns equal, *"because equality needs a relation and a relation is the registrant's"*.
So redaction is expressible as a codec, ferry permits it, and ADR-0009 already says what that means: *"Registering without proving is permitted and forfeits the guarantee."*
It remains the worse of the two mechanisms, on **visibility** rather than correctness: a codec is registered once in some package's `init` and applies to every use of that type in both directions in every importing program, and a middleware appears in the `Dump` call that uses it.

### 3.4 Dump needs strictly more of a wrapper than Load does

`C10=4`.
The mechanism is symmetric and the obligations are not: a `Source` wrapper forwards one optional interface whose failure is loud, and a `Sink` wrapper forwards two whose failures are both silent.

A redacting sink breaks value fidelity by design, which is consistent only because a wrapper is not a driver and nothing runs the conformance suite against it.
The same fact means nothing checks its `Committer` forwarding either.

A wrapper that hides `Committer` also moves the sink from ADR-0011's interleaved aggregation to its encode phase, which ADR-0011 measured as a materially worse error set at 4 errors against 2 on the same plane.
So dropping the assertion changes ferry's error policy as well as losing the commit, and neither is visible at the call site.

## 4. Defects found in the inherited prototype

All six were found by running rather than by reading, and five were silent.
None is a defect in an ADR.

- **`prefixFree` checked only for exact duplicates.**
  ADR-0003 is explicit that prefix-freeness rather than duplicate detection is the rule and gives the measured reason, so the headline rule of ADR-0003 was not implemented on the branch every later ADR was measured against.
  `struct{ DB string; DBSub S }` with both tagged `db` compiled clean.
  Found by asking whether the Registry is more permissive than core and getting `ok` from core.
- **The walk discarded a `Reader`'s error and substituted `Absent`.**
  A driver reporting "this address holds a type I cannot express" was indistinguishable from a missing key, and the field silently took its default.
  That is survey item **5.11**'s shape, which ADR-0001 rules out by architecture and names as the failure it rules out by name, occurring inside ferry's own walk.
  Nothing caught it because every prototype source returns a nil error.
- **The YAML driver returned a non-nil error at a container address**, where ADR-0005 measures the required answer as `Absent`.
  This was reachable only after the previous defect was fixed: while the walk swallowed errors, the driver behaved correctly by accident.
  **Two latent defects that cancelled out**, which is why neither was visible.
- **`splitTag` could not parse ADR-0008's own headline example.**
  `default='Hello, world'` split at the comma and reported `unknown option "world'"`, because a quote opened a token only at the start of a whole comma-separated part rather than after `default=`.
  Every fixture on the branch used a default with no comma in it, which is the case ADR-0008 measured as 3.9% of real free-text tag values and singled out as the one that has to read well.
- **`required` on a `*struct` is enforced by nothing.**
  ADR-0006 decides that it means "the plane supplied at least one of the address's static children", and explicitly records repairing this in its own draft after shipping a version where it "was accepted at schema compile and enforced by nothing".
  That repair was never carried onto `proto/16-entry-point`.
  Found while checking section 1.5's neighbouring case, and not fixed here: it is ADR-0006's repair rather than this ticket's.
- **`r17_usage.go` tripped `go vet`'s printf check** on a raw string of sample source.

## 5. A toolchain bug

`reflect.TypeFor[T]().Comparable()` on a named struct type panics the linker, on **both** go1.26.5 and go1.27rc2:

```
panic: R_USEIFACE in main.main references type:.eqfunc.M1K7S which is not a type or itab
```

Twelve lines reproduce it.
`reflect.TypeOf(V{}).Comparable()` is fine and `.Name()` is fine.

It matters because ADR-0004's central claim about `Value` is that it is comparable, and this is the obvious way a harness would assert it.
Searched rather than filed: no upstream issue matches the `type:.eqfunc.*` form, and the nearest is [golang/go#69787](https://github.com/golang/go/issues/69787), the same `R_USEIFACE ... which is not a type or itab` deadcode panic on a function symbol at Go 1.22.7.

## 6. Items from the xload survey

The survey is [`generics-and-modern-go.md`](generics-and-modern-go.md), section 5, against `github.com/gojekfarm/xtools` at [`a90b3aa`](https://github.com/gojekfarm/xtools/commit/a90b3aad2133248cec50f6b4d6e37b0d9e788adb).

**5.10, composite values are string-splitting.**
Touched by #15 and it is the one item this work makes *harder* rather than easier.
ADR-0003 removed the structural cause with `Index` segments, and `REG_MULTI_SZ` is a plane holding a native list at one address.
The tempting answer is to join the elements with a separator, which is 5.10 reintroduced at the driver rather than at the tag.
Section 2.7 takes the lossless one instead: NUL-joined `Bytes` behind a registered codec, where the NUL is not a delimiter anybody chose but the format's own separator, so an element cannot contain it.

**5.11, the YAML provider silently discards parse errors.**
Addressed, and found live in ferry's own walk rather than in a driver: `loadDir` discarded the `Reader`'s error and substituted `Absent`.
Section 4.

**5.7, `reflect.DeepEqual` as a "was anything set?" probe.**
Bears on #14 once.
The recipe cannot ask "is this field still at its default", for the reason ADR-0006 already gave, and it does not try: it reads the required set out of the error set instead.

**5.13, the per-key pull model amplifies backend round trips.**
Bears on #15.
A Registry driver that preserved `REG_DWORD` against `REG_QWORD` would have to `Get` before every `Set`, which is exactly the amplification 5.13 names, and it would still not close the hole on a key that does not exist yet.

**5.3, no schema caching**, and **5.2, two walks that have already diverged**, are ADR-0010's.
5.2 surfaces in #14 as the reason a template emitter's second `reflect` walk is a defect rather than an inconvenience, and it was reproduced against a real divergence over an embedded field.

**5.14** was enumerated rather than assumed, all four items.

- *Two ways to set the loader.*
  Bears on #10 directly and it is the item this ticket was most likely to commit.
  It is avoided: a middleware is a `Source` or a `Sink`, passed positionally exactly as any driver is, and no wrapper doubles as an Option.
  It bears on #14 in a different costume: seeding and documenting are two operations and the recipe is one code path, so the risk is a template API that also offers `Dump`.
- *The `CanAddr` loop that can only run once.*
  Bears on nothing here.
  No probe adds a walk.
- *The non-deterministic select on a cancelled context.*
  Concurrency, and [#20](https://github.com/onhotpath/ferry/issues/20)'s.
  One thing #20 should know: a wrapping `Reader` and a wrapping `Writer` are on the path of every `Get` and `Set`, so a wrapper holding state needs a lock under a concurrent walk exactly as ADR-0012's recorder does.
- *Value receivers on `Error()` where pointers are returned.*
  Closed by ADR-0011 by construction.
  The minimum of ADR-0011 carried on this branch uses a pointer receiver for the same reason.

## 7. Honest gaps

- **No first-party Registry driver was written for `driver/`.** What exists is a prototype driver over a `wStore` seam, with a fake and a real backend. It implements `Source`, `Sink`, `Enumerator` and no `Committer`, and it has no conformance suite because none exists yet.
- **The `REG_MULTI_SZ` route is implemented and is not a first-party driver's.** Section 2.7 measures a codec plus a driver option round-tripping exactly. What is untested is whether the driver option is the right *spelling*, and whether a first-party driver would want to detect the type on read and preserve it on write rather than being told.
- **The permission probe does not test an ACL.** It tests an access mask on a handle and one hive denied to administrators. A denied ACL on a key the process created was not attempted, because the runner is elevated and the mask route is deterministic.
- **Nothing was run on a non-elevated Windows session**, so "what a normal user's process sees" is inferred from the access-mask result rather than measured.
- **The template emitter was written for YAML only.** The TOML and `.env` rows in section 1.6 are reasoned from those formats having a comment syntax, not run.
- **The `cmd/` generator of section 1.8 was not built.** Only its inputs were measured: the doc comments were extracted with `go/parser` and the registry blind spot was measured.
- **The `Annotator` interface was written to emit with and not proposed.** Whether ADR-0004 should grow a fourth optional interface is a decision, and section 3.2 is an argument that it should grow *fewer*.
- **No wrapper conformance case was written**, which is the concrete thing section 3.2 argues `ferrytest` owes. It belongs to [#35](https://github.com/onhotpath/ferry/issues/35).
- **The bind cost in section 2.6 is a microbenchmark of the key function alone**, not a whole-load figure, so it is not comparable to ADR-0012's numbers.
- **The `required` suppression of section 1.5 is untested under a concurrent scheduler**, which is where its precondition would actually break. #20 owns whether one exists.
- **`required` on the pointer itself is still unenforced** on `proto/16-entry-point`. Section 1.5 records it; the repair is ADR-0006's and was not implemented here.
