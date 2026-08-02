# proto/14-15-10-enabled

Prototype for the three `wayfinder:prototype` tickets:
[#14](https://github.com/onhotpath/ferry/issues/14) template generation,
[#15](https://github.com/onhotpath/ferry/issues/15) the Windows Registry, and
[#10](https://github.com/onhotpath/ferry/issues/10) cross-cutting concerns.

They are one session because they are one question.
ADR-0001 sorts every capability into In core, Enabled, Milestoned or Ruled out and says Enabled is the default landing place.
All three of these are Enabled, and this branch is the first real test of whether that was true.

Built on `proto/16-entry-point`, which is the tip of the chain
`5 -> 7 -> 12 -> 19 -> 16` and the only branch carrying the whole engine.

Run: `T14=<n|all> GOTOOLCHAIN=go1.27rc2 go run .`
The inherited suites still run unchanged: `E16=all`, `P19=all`, `P12=all`, and the bare `go run .`.

## #14, template generation

| file | what it is |
| --- | --- |
| `t_fixture.go` | the struct every probe runs against, and the audit fixtures |
| `t_errors.go` | the minimum of ADR-0011 this ticket consumes, plus the aggregating scheduler |
| `t_plane.go` | the recorder sink, the empty plane, the fixed plane |
| `t1_reach.go` | is the defaulted value reachable at all |
| `t2_recipe.go` | the recipe, and what it costs |
| `t3_artefact.go` | **the artefact**, at four annotation levels |
| `t4_channel.go` | the annotation channel, and the half of it that is hard |
| `t5_planes.go` | which planes can be templated, and what one that cannot reports |
| `t6_prose.go` | where a comment's words come from |
| `t7_api.go` | the API surface, argued last |
| `t8_audit.go` | the cases the other probes do not contain |
| `t9_adr12.go` | reconciliation with ADR-0012, including its observing `Source` written from the ADR |

### Probes

| # | question | result |
| --- | --- | --- |
| T1 | is the defaulted value reachable at all | **no, by either route an accepted ADR names.** A Dump of the zero value carries no defaults, because ADR-0006 makes a default a Load-side rule. A Load from an empty plane applies them and then yields nothing, because `required` fires and ADR-0011 yields no value ferry built. ADR-0010's "template generation reaches the defaults through a recording sink" is true only for a struct with no `required` field |
| T2 | the recipe, and what it costs | zero-dump, then Load-until-it-stops-failing, then Dump. **2 Loads under ADR-0011's aggregating scheduler and k+1 under first-error.** It reaches the values and not the declarations: no Go type, no declared default text, and `null` where a list goes. Its "no `omitzero` field" result is **overturned by T9** |
| T3 | the artefact | four levels, emitted. L0 needs nothing; L1 needs ADR-0011's accessors plus a channel `Writer` does not have; L2 needs the compiled schema; L3 needs a source ferry has nowhere to put |
| T4 | the annotation channel | an optional `Annotator` discovered by assertion works and is ADR-0004's own pattern - and it is **the easy half**. It gives an emitter somewhere to put two facts it cannot obtain |
| T5 | which planes | the predicate is **"has a comment syntax"**, not "has a format", and it splits the feature into SEEDING (every writable plane, needs nothing new) and DOCUMENTING (text planes only) |
| T6 | where the prose comes from | the tag is closed by ADR-0008; a side table spells the address set twice and drops silently on a rename; **the Go doc comment is the source people want and `reflect.StructField` has no `Doc` field**, so it is build-time only |
| T7 | the API surface | not "a distinct entry point or Dump with an Option". SEEDING needs no entry point; DOCUMENTING is a generator, and the prose question decides whether it is a sub-module or a `cmd/` |
| T8 | the audit | five findings, below |
| T9 | reconciliation with ADR-0012 ([#25](https://github.com/onhotpath/ferry/issues/25), PR #40) | its observing `Source` **overturns one of T2's results**: an `omitzero` field and everything under a nil pointer are reachable on the load side, so the address set is 14 against the zero dump's 11. It changes nothing about the declarations. And it is the third ADR to decline the read-only schema view, which leaves #14 as the only ticket where ADR-0001's condition can still be met |

### Findings against accepted ADRs

- **A generated template that fills in its `required` addresses loads clean and produces an empty config.**
  ADR-0006 makes `required` a presence test, and `name: ""` is present.
  The repair is to emit the address commented out, which is a comment-syntax capability, so on every `values only` plane in T5 the seeded artefact is one a Load refuses until a human edits it.
- **`required` on a leaf inside an optional `*struct` makes the whole optional section mandatory.**
  ADR-0006 states the neighbouring rule for defaults - "an optional section stays optional" - and never asks it of `required`.
  ADR-0011 suppresses a required child under a required *parent* and has no rule for a child under an *absent optional* parent.
- **`required` on a leaf under a dynamic shape compiles and never fires**, so it is a marker invisible until somebody adds a map entry.
- **`required,omitzero` is admissible and breaks the recipe**: the zero dump omits the field, so nothing learns its boundary kind.
- **The template contains the credential.** A `default=hunter2` is in the type, in every compiled schema, and in the starter config. This is not a bug in the recipe; ADR-0006 requires a defaulted field to be dumped. It is not reachable by wrapping a Sink either, which is the #10 half of it.
- **ADR-0001's schema-extraction pattern does not reach every mapped key.**
  It says "dump a zero-valued or defaulted struct into a sink that records what it sees, and you have every mapped key and its Go type".
  Measured, an `omitzero` field and everything under a nil pointer are not in that set: 11 addresses against the 14 an ADR-0012 observing `Source` sees on the same type.
  The Go type is not in it either, on any route.

### Defects found in the inherited prototype, fixed here

- **`splitTag` could not parse ADR-0008's own headline example.**
  `default='Hello, world'` split at the comma and reported `unknown option "world'"`, because a quote opened a token only at the start of a whole comma-separated part rather than after `default=`.
  Every fixture on this branch used a default with no comma in it.
- `r17_usage.go` tripped `go vet`'s printf check on a raw string of sample source.

### Changes to inherited files, all load-bearing

- `e_schema.go`: the `splitTag` fix above.
- `e_opts.go`, `e_entry.go`: ADR-0010's scheduler seam promoted from a hardcoded `serial` to a load-affecting `WithSched` Option, because ADR-0011 puts aggregation there and #14 reads the required set out of an aggregate.
- `e_walk.go`: the `required` refusal carries an address and a class, per ADR-0011, because the alternative is parsing the message.

### A toolchain bug, reproduced

`reflect.TypeFor[T]().Comparable()` on a named struct type panics the linker, on **both** go1.26.5 and go1.27rc2:

```
panic: R_USEIFACE in main.main references type:.eqfunc.M1K7S which is not a type or itab
```

Twelve lines reproduce it (`scratchpad/lnk`).
`reflect.TypeOf(V{}).Comparable()` is fine and `.Name()` is fine.
It matters because ADR-0004's central claim about `Value` is that it is comparable, and this is the obvious way a harness would assert it.

Searched rather than filed: no upstream issue matches the `type:.eqfunc.*` form.
The nearest is [golang/go#69787](https://github.com/golang/go/issues/69787), the same `R_USEIFACE ... which is not a type or itab` deadcode panic on a function symbol at Go 1.22.7, so this looks like the same family with a different referent.
Filing it is somebody's, and it is not this ticket's.
