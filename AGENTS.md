# ferry

`ferry` is a bidirectional, struct-first data mapper for Go.
One annotated struct, one tag grammar, two directions:

- **Load**: pluggable source -> struct (env vars, YAML, HTTP query params, Consul, Vault, anything)
- **Dump**: struct -> pluggable sink (a config struct written back out to a file, a KV store, env)

Read [`CONTRIBUTING.md`](CONTRIBUTING.md) for what the conventions below mean and why.
This file is the short form.

## The API is decided, and the ADRs are where

Twenty-one ADRs in `docs/adr/` settle what ferry supports, the module layout, the address model and its sealed kinds, the source and sink contract, the type set, defaults and absence, the codec chain, the tag grammar, typed codec registration and the `Value` it builds, the entry point and schema cache, the error model, the caller-held binding, plane compatibility, the conformance package, the plane that spells one address two ways, the spelling seam, the concurrency model, watch and reload, and the multi-key extension mechanism.
Every one is Accepted, and all of them are implemented.

**The ADRs are the specification.**
Where an issue and an ADR disagree, the ADR wins.
Read the ADR that owns a decision before proposing a change to it, and if a proposal contradicts one, say so explicitly rather than quietly overriding it.
If `docs/adr/` has no entry on a topic, that decision has not been made yet.
Do not guess, and do not carry another library's answers over as though they were ferry's.

Where the shipped code and an ADR disagree, that is a defect in one of them.
Do not build around it and do not paper over it.
Bucket it as under **Issues** below: fix it in the same pull request, ask, or file it if it is a major blocker.

Corrections amend the ADR in place with a note saying what it read as published, what moved and why.
Only an unmade decision gets a new number.

An amendment the user asks for is folded into the same pull request as one commit.
A correction large enough to need evidence gets a prototype and its own pull request, and the user decides.

**The three questions that used to be parked are decided and shipped**: tag-grammar extension by [ADR-0021](docs/adr/0021-the-multi-key-extension-mechanism.md), concurrency by [ADR-0019](docs/adr/0019-the-concurrency-model.md), and watch and reload by [ADR-0020](docs/adr/0020-watch-and-reload.md).
There is no prohibition left on any of them, and each is now an ADR to read before changing it, like every other.

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
- No file carries a `toolchain` directive, `go.work` included. The floor is temporarily go 1.26, pending Go 1.27 GA (#366).
- No `replace` directive is ever checked in.
- A driver requires core at a released version. Release order is forced: tag core, bump each driver's `require`, tag drivers.

## Lint

Seven structural limits are set deliberately: `cognitive-complexity 7`, `cyclomatic 10`, `function-length 75/50`, `max-control-nesting 4`, `argument-limit 5`, `function-result-limit 3`, `line-length-limit 120`.

When one fires, **split the function**.
Never raise the number, never add a `//nolint`.

`make check` and `make lint` are what CI runs, and both must be green.
`golangci-lint run .` does not recurse.

## Tests

One seam: assert through `Load`, `LoadOver`, `Dump`, `Compile`, `Bind` and `BindSink`, or through `ferrytest`, which calls those.
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

**An issue is not a notebook.** Issue bloat is not the goal, and a finding earns its own issue only when it has substance to carry.
Something found while implementing goes into one of three buckets:

| bucket | what to do |
| --- | --- |
| straightforward | fix it in the same pull request |
| needs input | ask the user, do not guess and do not file to defer the question |
| blocker | file an issue, and only if it is major |

Evidence, measurements and notes belong on the pull request or on the issue that already owns the area, not in a new one.

## Standing rules

- Never use an em dash. Use a plain dash instead.
- In Markdown, put each sentence on its own line. Keep normal Markdown structure otherwise.
- Never add a `Co-Authored-By` trailer to commit messages.
- Never put Claude Code session URLs in PR descriptions or commit messages.
- Be concise. Favour quality, simplicity, and long-term maintainability over speed.
