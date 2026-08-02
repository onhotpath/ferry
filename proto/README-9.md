# #9, errors as a first-class, extensible concern

Throwaway. Never merges. Built on `proto/11-tag-grammar`, which is built on
`proto/8-defaults` -> `proto/7-type-set` -> `proto/5-source-sink`, so the
probes run against a real `Path`, a real `Value`, the real type set, the real
tag grammar, #8's compiled schema and the real YAML driver over real files.

    GOTOOLCHAIN=go1.27rc2 go build -o /tmp/p9 . && /tmp/p9 e     # #9's probes
    GOTOOLCHAIN=go1.27rc2 go build -o /tmp/p9 . && /tmp/p9 etui  # the rendering, by hand
    /tmp/p9 t   /tmp/p9 d   /tmp/p9                              # #11, #8 and #7, as regressions
    go vet ./...

## The question

ADRs 0001 through 0009 defer "the error types every refusal here produces" to
#9, and every one of them is written as though #9's convention already exists.
So the question is not the ticket body alone: it is whether **one** error model
covers that whole union, and what it costs.

`e_error.go` is the portable part - a pure module with no I/O, no probe code
and no terminal code, so it is the thing that lifts into core if ADR-0010 is
accepted. Everything else here is a shell over it.

## What is new here

| file | what it is |
| --- | --- |
| `e_error.go` | the model: one type with no exported fields, five sentinels, `ErrorAt`, `Elements`, the flat aggregate, and the three-part sort key |
| `e_census.go` | E1, the enumerated union of every refusal ADRs 0001-0009 defer here |
| `e_load.go` | the walk's side: the sink that turns first-error-wins into aggregating, and the redaction rule at the one place plane text enters |
| `e_dump.go` | a Dump that talks to a real writer and can fail one, which `dumpD` cannot |
| `e_ferrytest.go` | what `ferrytest` would ship: an exact-set diff over `(address, class)` |
| `e_probe1.go` | E2-E4 |
| `e_probe2.go` | E5-E8 |
| `e_probe3.go` | E9-E15 |
| `e_probe4.go` | E16, E16b: E7 reopened after review |
| `e_tui.go` | the interactive shell, for the one question a table answers badly |

`d_load.go`, `d_schema.go` and `t_compile.go` are patched to route their errors
through the model. With no sink the walk behaves exactly as before, so #8's and
#11's probes are regressions rather than casualties.

**One deliberate deviation from the prototype convention**: ferry's `proto/`
runs batch probes because ADRs cite numbers, and that is what E1-E15 are. The
interactive shell exists for **presentation** only, because "does `%+v` read
well at three errors and at forty" is a feel question a table answers badly.

## The probes

| # | question | result |
| --- | --- | --- |
| E1 | the union: does one model cover every refusal the ADRs defer here | **55 refusals from 8 ADRs, 53 fit.** 30 of 55 are schema compile and 33 of 55 are one class |
| E2 | `errors.AsType` into a type with no exported fields, and 5.14 | reaches it through `%w` and `Unwrap() []error`; the value form does not implement `error` |
| E3 | sorting at construction against sorting in `Format` | **the finding.** Sorted only in `Format`, `AsType` picks 3 distinct over 300 runs while printing 1 |
| E4 | is the three-part key total, under a concurrent walk | 1 distinct report over 300, both for two errors at one address and for four goroutines |
| E5 | does an error leak the plane's own text | **4 of 5 naive messages carry the secret; 0 of 5 after.** The cause stays reachable |
| E6 | aggregate against fail-fast on Load | 1 error against 5, on the same plane. 5.4 in ferry's own shape |
| E7 | what aggregating costs a sink that cannot stage | **8 extra addresses of 12.** Free on a `Committer`, because `Commit` never runs |
| E8 | a plane that dies mid-walk, against one that denies two keys | 6 errors from 1 fact, and 2 errors from 2 facts, and **core cannot tell them apart** |
| E9 | is `ErrorAt` a second way to attach an address | no: it attaches, never classifies, and is inert until core wraps it |
| E10 | does the aggregate need a cap | 10,000 elements: 6.2 ms to sort and join, and a 79-byte one-line form |
| E11 | the rendering at one, at three and at forty | the one-line form stays inside a wrapping sentence and still names three addresses |
| E12 | the `ferrytest` diff, and the defect it exists to catch | catches a suppression rule firing once too often; `errors.Is` does not |
| E13 | the audit: the fixture shapes the other probes do not contain | **found a defect**, see below |
| E14 | what `Load` hands back when it fails | the partial exists inside the walk and does not cross the boundary |
| E15 | a driver holding an opinion about the class | it can declare `Value` where core would have said `Plane`, and cannot forge provenance or an address |
| E16 | **E7 reopened**: four policies against four failure shapes | **E7 measured the wrong thing.** Only a `Set` failure can amplify writes; an encode failure costs the plane nothing |
| E16b | what two-phase costs if it refuses to buffer | 523 ms and ~546 KB held, against 1.044 s and nothing held, on 10,000 addresses |

## E7 reopened, and what it changed

E7 used a sink that could only refuse a write, so it measured **`Set`**
failures and reported the number as though it were the cost of aggregating in
general. It is not. An **encode** failure is deterministic, per field, and
happens before the write for that address, so aggregating it costs the plane
nothing. E16 separates them and adds the two policies E7 never had:

```
S3 two fields cannot be encoded, plane healthy
  fail-fast      3 attempts  3 written  1 error
  aggregate      6 attempts  6 written  2 errors     <- six writes for a failure
  two-phase      0 attempts  0 written  2 errors        ferry could have caught first
```

So the rule ADR-0011 now carries is two-phase then aggregate, which buys a
property worth stating: **if a Dump fails for a reason ferry could have known
without touching the plane, the plane is untouched.** That is ADR-0004's own
argument for `ErrReadOnly` at `OpenWriter` rather than at the first `Set`,
applied one layer in.

The `Set` half stays aggregating, because S2's partial ACL is the Vault case
E8 argued Load must serve, and taking it away on Dump alone would be an
asymmetry between the directions on the same fact.

## What running it changed

- **`errors.Join` anywhere in ferry breaks the aggregate contract, silently.**
  E13 reported *one* element while *two* errors printed: schema compile still
  used `errors.Join`, whose result `Elements` cannot range and whose order is
  insertion order. Worse, #11's own probes were parsing `errors.Join`'s newline
  dump as an iterator, which is the ticket comment's "not a presentation layer"
  arriving as a live dependency. Fixed by routing schema compile through the
  model. The rule this produced: **ferry has exactly one aggregate constructor
  and never calls `errors.Join`.**
- **The address was printed twice** when a driver used `ErrorAt` and core then
  attached the same address. The carrier has to be unwrapped away once core has
  taken the address from it.
- **The sentinel text is load-bearing**, which was found by rendering rather
  than by choosing. A driver declares its class by wrapping a sentinel, so the
  sentinel's text lands inside the driver's own message: `plane` read as a
  stray word, `plane error` reads as a sentence.
- **A location-less driver error rendered as the bare word `driver`.** ferry's
  own text for a driver failure is the moment in words - `opening the plane`,
  `closing the plane` - which is also why direction needs no field: the call
  site knows the verb without the error storing it.
- **The message tiebreak reorders two errors at one address** relative to the
  order the checks run in. Deterministic either way, and the alternative
  (insertion order) is what #20 would break.

## Two rows of the census the model does not express

Recorded rather than smoothed over.

- **A half codec pair is a build error**, not an error value: ADR-0009's
  generic inference refuses it before anything runs. Out of scope, and that is
  a result rather than a gap.
- **The tag-key Option is validated where the Option is supplied**, not at
  schema compile, so it belongs to no moment in the list.

## A note on the toolchain

An early baseline binary printed `json: unable to marshal ...` where a fresh
build of the identical source prints `json: cannot marshal ...`, both reporting
`go1.27rc2`. The stale build came from a cached toolchain. Anything sourced
from `go1.27rc2` is worth rebuilding before it is quoted; ADR-0001 already
requires re-verifying at GA.
