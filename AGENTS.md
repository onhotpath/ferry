# ferry

`ferry` is a bidirectional, struct-first data mapper for Go.
One annotated struct, one tag grammar, two directions:

- **Load**: pluggable source -> struct (env vars, YAML, HTTP query params, Consul, Vault, anything)
- **Dump**: struct -> pluggable sink (a config struct written back out to a file, a KV store, env)

Read [`CONTRIBUTING.md`](CONTRIBUTING.md) for what the conventions below mean and why.
This file is the short form.

## The API is decided, and the ADRs are where

Fourteen ADRs in `docs/adr/` settle the address model, the source and sink contract, the type set, defaults and absence, the codec chain, the tag grammar, registration, the entry point and schema cache, the error model, the caller-held binding, plane compatibility and the conformance package.
Every one is Accepted, and all of them are implemented.

**The ADRs are the specification.**
Where an issue and an ADR disagree, the ADR wins.
Read the ADR that owns a decision before proposing a change to it, and if a proposal contradicts one, say so explicitly rather than quietly overriding it.
If `docs/adr/` has no entry on a topic, that decision has not been made yet.
Do not guess, and do not carry another library's answers over as though they were ferry's.

Where the shipped code and an ADR disagree, that is a defect in one of them.
File it.
Do not build around it and do not paper over it.

Corrections amend the ADR in place with a note saying what it read as published, what moved and why.
Only an unmade decision gets a new number.

**Three questions are parked deliberately and must get no silent answer**: tag-grammar extension, concurrency, and watch/reload.
Nothing may ship that anticipates them.

## Doc comments

godoc is for the person using the thing, not the person who designed it.

Lead with what it does, then how to call it, then what comes back including on failure, then the sharp edges.

**Never put in a doc comment on an exported identifier, or in a package comment:**

- ADR numbers, issue numbers or pull request numbers
- measurements from the design process
- arguments against alternatives that were never shipped
- internal vocabulary the caller never types, unless the package comment defines it first

The reasoning belongs in `docs/adr/`, where it already is.
A package comment may close with one line pointing there.

Keep every sharp edge.
They are the valuable part and they are easy to delete by accident, because they sit buried inside the rationale.

**Unexported doc comments and inline implementation comments keep their ADR citations.**
They are the only in-repo link between a line of code and the decision that put it there.
The boundary is exactly whether `go doc -all <pkg>` prints it.

`make godoc-check` asserts this across every published package.
Run it before opening a pull request.

Long-form documentation goes in `docs/guide/`, and that is the one place where a claim quoting a number cites the ADR it came from.

## Examples

A code block in a doc comment is not compiled and will rot.
Promote it to an `Example` in `example_test.go` with an `// Output:` line whenever it can run.
A README that shows code quotes its `Example` verbatim, so a stale example fails the build.

## Benchmarks

ferry's own benchmarks live beside the code they measure, in a `_test.go`, and use the standard library only.

Comparisons against other libraries live on the `perf/` branch, in a module of their own, kept out of `go.work`.
Depending on koanf and viper in order to measure them is not a reason to put them in the dependency graph of a library whose core has zero non-stdlib dependencies.

Published numbers are produced by the pipeline and never written by hand.
A figure that was typed rather than measured does not go in a file.

## Module rules, asserted by CI

- Core's `require` block stays empty, and core never imports `encoding/json/v2` or `encoding/json/jsontext`.
- No `go.mod` carries a `toolchain` directive. The pin lives in `go.work` alone.
- No `replace` directive is ever checked in.
- A driver carries no `require` on core until core is tagged.

## Lint

Seven structural limits are set deliberately: `cognitive-complexity 7`, `cyclomatic 10`, `function-length 75/50`, `max-control-nesting 4`, `argument-limit 5`, `function-result-limit 3`, `line-length-limit 120`.

When one fires, **split the function**.
Never raise the number, never add a `//nolint`.

`make check` and `make lint` are what CI runs, and both must be green.
`golangci-lint run .` does not recurse.

## Tests

One seam: assert through `Load`, `LoadOver`, `Dump` and `Compile`, or through `ferrytest`, which calls those.
`Path`, `AddressSet`, `Value` and `Error` are the exception, because they are the seam.

Every equivalence subtest gets a fresh destination.

A driver proves itself with one call to `ferrytest.Driver`.

## Prior art

- `github.com/gojekfarm/xtools/xload` is the direct ancestor.
  It covers the Load direction only, via `Loader.Load(ctx, key string) (string, error)`, driven by struct tags such as `env:"HOST"` and `env:",prefix=DB_"`.
  ferry is a new library, not a fork.
  Its ancestry is a starting point for discussion, not an inherited API.
- Blog post on xload's model: https://ajatprabha.in/2024/07/07/xload-ultimate-data-loader-go-structs
- A prior-art sweep of the Go ecosystem found no existing library that drives both directions off one tag grammar over pluggable backends.
  Read `docs/` for the recorded findings rather than re-running that research.

## Issues

Issues live in this repo's GitHub Issues.
Use the `gh` CLI.

Every change belongs to an issue, and the pull request says which.
A pull request that finds a second problem files it rather than growing to cover it.

## Standing rules

- Never use an em dash. Use a plain dash instead.
- In Markdown, put each sentence on its own line. Keep normal Markdown structure otherwise.
- Never add a `Co-Authored-By` trailer to commit messages.
- Never put Claude Code session URLs in PR descriptions or commit messages.
- Be concise. Favour quality, simplicity, and long-term maintainability over speed.
