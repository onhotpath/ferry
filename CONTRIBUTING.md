# Contributing to ferry

This file explains how the repository is organised and what the conventions mean, so that a change arrives in the shape the project already uses.

`make help` lists the developer targets.
`make check` and `make lint` are what CI runs, and both must be green before a pull request is opened.

## The repository is six modules and one workspace

| module | what it is |
| --- | --- |
| `.` | core: the walk, the schema compiler, the tag grammar, the codec chain, the error model |
| `driver/env` | environment variables |
| `driver/yaml` | a YAML file, edited in place |
| `driver/kv` | a Consul-shaped key-value store |
| `driver/http` | one HTTP request's query parameters or header fields, bound once and loaded through per request |
| `driver/windows` | the Windows registry, over a seam an in-memory store fills so the suite runs everywhere |

`ferrytest` is a package inside core, not a module of its own.
It ships from the same place as the rule it checks, because a conformance suite that ships from somewhere else binds nobody.

Three module rules are asserted by CI and are not stylistic:

- **Core takes no non-stdlib dependency.**
  Its `require` block stays empty, unconditionally.
  A driver may have dependencies; core may not.
  Core must also never import `encoding/json/v2` or `encoding/json/jsontext`, and `make nojsonv2` proves it still builds without them.
- **No file carries a `toolchain` directive, `go.work` included.**
  The floor is temporarily Go 1.26, pending Go 1.27 going GA (#366).
  A consumer resolves the floor with whatever toolchain they have.
- **No `replace` directive is ever checked in.**
  `go.work` gives local development its sibling-on-disk resolution instead, and `GOWORK=off` exercises the resolution a real consumer gets.

A driver module carries no `require` on core until core is tagged.
`github.com/onhotpath/ferry@v0.0.0` cannot be resolved from the proxy, and a module with any third-party requirement loads the full module graph, which reads core's `go.mod` at the named version rather than taking the workspace copy.
`go mod tidy` in a driver therefore needs a temporary `replace` that is dropped again before the change is committed.

## Where decisions live: `docs/adr/`

An architectural decision record is a numbered file in [`docs/adr/`](docs/adr/) that settles one question and states the evidence it was settled on.
There are twenty-one, all Accepted.
Fourteen were written before the code, and [ADR-0015](docs/adr/0015-two-spellings-of-one-address.md) is the one exception: it records a rule the first multimap driver reached first, and it says so.
[ADR-0016](docs/adr/0016-the-sealed-address-model.md) to [ADR-0021](docs/adr/0021-the-multi-key-extension-mechanism.md) are the v0 design campaign, written before their code and each carrying the prototype branch it was decided on.

**The ADRs are the specification.**
Where an issue and an ADR disagree, the ADR wins.
Where the shipped code and an ADR disagree, that is a defect in one of them and it gets an issue rather than a quiet fix in whichever direction is convenient.

Read the ADR that owns a decision before proposing a change to it.
A proposal that contradicts an accepted ADR should say so out loud.
If `docs/adr/` has no entry on a topic, that decision has not been made yet, and guessing at it is how a library ends up with two answers.

Three questions were parked deliberately and have now been answered, each in its own ADR and none of them silently: tag-grammar extension in [ADR-0021](docs/adr/0021-the-multi-key-extension-mechanism.md), concurrency in [ADR-0019](docs/adr/0019-the-concurrency-model.md), and watch and reload in [ADR-0020](docs/adr/0020-watch-and-reload.md).
The rule that produced them stands: a question with no ADR gets no answer in code first.

**Corrections amend the ADR in place**, with a blockquoted note saying what it read as published, what moved, and why.
The number stays; the history stays legible.
Only a decision nobody has made yet gets a new number.

## Three kinds of documentation, and who each is for

They are not interchangeable, and the commonest mistake is writing one in another's voice.

### godoc is for the person using the thing

Doc comments on exported identifiers, and package comments, are read on `pkg.go.dev` by somebody who wants to get something done.

Lead with what it does, in the form godoc wants: `Load builds a value of T from src.`
Then how to call it, then what comes back including on failure, then the sharp edges a caller will otherwise get wrong.

**Do not put in a doc comment:**

- **ADR numbers, issue numbers or pull request numbers.**
  The reader has none of them open and cannot resolve the reference.
- **Measurements from the design process.**
  "Measured over 10,012 third-party Go files" is evidence for a decision, not a fact a caller acts on.
- **Arguments against alternatives that were never shipped.**
  The user is not choosing between designs.
  They are using the one that exists.
- **Internal vocabulary the caller never types**, unless the package comment defines it on first use.

The reasoning is not lost by leaving it out, because it is already in `docs/adr/` in full, and that is the record.
A package comment may close with one line pointing there.

Sharp edges are the valuable part and are easy to delete by accident, because they tend to be buried inside the rationale.
`time.Time` losing its zone's DST rules is a sharp edge.
`TagKey` applying to every struct in the call is a sharp edge.
Keep every one, and make it one sentence.

Unexported doc comments and inline implementation comments are the opposite case: **they keep their ADR citations**, because they are the only in-repo link between a line of code and the decision that put it there.
The boundary is exactly whether `go doc -all <pkg>` prints it.

`make godoc-check` asserts this across every published package, and it greps what `go doc` prints rather than the source, because that is exactly where the boundary is.
Run it before opening a pull request.

### `docs/guide/` is for the person who needs the whole picture

Long-form documentation: the type set as one table, the tag grammar in full, the error model, plane compatibility, and how to write a driver.

This is where detail goes that would bloat a doc comment, and it is the one place where **a claim quoting a number cites the ADR it came from**.

### The ADRs are for the person deciding

They argue.
They carry the alternatives, the measurements and the reasons.
Nothing else should try to.

## Examples belong in `example_test.go`

A code block inside a doc comment is not compiled and will rot.
An `Example` function with an `// Output:` comment is compiled and run by `go test`, so it cannot.

Promote a doc-comment code block to a real `Example` whenever it can run.
Fragments that cannot run, such as a struct declaration showing tag syntax, stay as blocks.

The three driver READMEs quote their `Example` verbatim from `example_test.go`, so a README example that stops being true fails the build.
That is the pattern to copy for any new user-facing document that shows code.

## Tests

**One seam.**
Tests assert external behaviour through `Load`, `LoadOver`, `Dump`, `Compile`, `Bind` and `BindSink`, or through `ferrytest`, which calls those.
The compiled schema, the node tree, the walker, the schema cache, the scheduler, the tag scanner and the codec chain are unexported and are not reached directly by a test.
A compiler rule is asserted through `Compile[T]()`; an address-set rule through what a recording driver's `Bind` was handed; a walk rule through what a plane was asked and what came back.
`Path`, `AddressSet`, `Value` and `Error` are the exception, because they are pure values with no engine behind them and they are themselves the seam.

**Every equivalence subtest gets a fresh destination.**
Sharing a destination across subtests is the defect that hides a broken second walk.

**A driver proves itself with one call.**
`ferrytest.Driver(t, myPlane())` runs the conformance suite.
Read [`docs/guide/drivers.md`](docs/guide/drivers.md) before writing a driver, and in particular read what `Plane.Kinds` means: it is what the plane carries end to end, not what `Get` returns, and it is an obligation in both directions.

## Benchmarks

Two different things, and they live in different places.

**ferry's own benchmarks** live beside the code they measure, in a `_test.go` file in the module that owns them.
They may use the standard library and nothing else, because the module rules above still apply: a benchmark is not a reason for core to take a dependency.

**Comparisons against other libraries** live outside the shipped modules, on a long-lived `perf/` branch, in a module of their own that is kept out of `go.work`.
Benchmarking against koanf and viper means depending on koanf and viper, and neither belongs in the dependency graph, the Renovate scope or the supply chain of a library whose core has zero non-stdlib dependencies.
The harness builds against `main`'s ferry rather than a vendored copy, so it cannot rot silently against the API.

Published numbers are produced by a pipeline and never written by hand.
A workflow runs the harness, renders the results through a program, and opens a pull request that replaces a marked section of the README.
A figure that was typed rather than measured does not go in a file, and a competitor that could not be measured is recorded as not measured, with the reason.

Whatever is published carries the machine, the Go toolchain version and every competitor version, because a benchmark number without them is not reproducible.

## The lint limits are fixed

`make lint` runs golangci-lint over every module in the workspace.
Seven structural limits are set deliberately:

```
cognitive-complexity 7    cyclomatic 10           function-length 75 statements / 50 lines
max-control-nesting 4     argument-limit 5        function-result-limit 3
line-length-limit 120
```

When one of them fires, **split the function**.
Do not raise the number and do not add a `//nolint`.
A driver's `Get` with a kind switch, and a sink's staging logic, are the shapes that hit `cognitive-complexity` first, and both split cleanly.

`function-length` and `add-constant` are relaxed for `_test.go` only.

`golangci-lint run .` does not recurse.
`make lint` is what CI runs.
A stale cache from a deleted worktree can report failures in files that no longer exist; `./.bin/golangci-lint cache clean` fixes that.

### The canary

`make lint` starts with `make lint-canary`, which asserts that the `unused` linter still reports dead code.

A linter that never ran and a linter that found nothing both print `0 issues`, so a linter can go missing without anything turning red.
`unused` is the one at risk: golangci-lint runs it inside the same metalinter as `staticcheck`, so an analyser that panics takes the whole runner down and both go quiet at once.
Which analyser is bundled is decided by the golangci-lint pin, which Renovate moves, so whether `unused` survives a bump is not something `.golangci.yml` can promise on its own.

The canary is `lintcanary.go`, a single dead function behind the `ferrylintcanary` build tag.
No ordinary build, vet, test or lint run sets that tag, so the function is invisible everywhere else.
If the canary target fails, `unused` stopped reporting: find out why rather than editing the canary.

## Issues, branches and commits

Issues live in this repository's GitHub Issues.
Use the `gh` CLI.

Every change belongs to an issue, and the pull request says which.
A pull request that finds a second problem files it rather than growing to cover it.

Commit messages say what changed and why it was worth changing, in prose, on one sentence per line.
Never add a `Co-Authored-By` trailer.

## House style

- Never use an em dash.
  Use a plain dash instead.
- In Markdown, put each sentence on its own line.
  Keep normal Markdown structure otherwise.
- Be concise.
  Favour quality, simplicity and long-term maintainability over speed.
