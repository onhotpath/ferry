# The performance harness

This directory is a module of its own, `github.com/onhotpath/ferry/bench`, and it never
ships.
It lives on the `perf/bench-harness` branch and is not merged to `main`.

## Why it is not on `main`

It depends on viper, koanf, xload, go-envconfig and kelseyhightower/envconfig.
Putting that dependency tree in the repository people clone would put it in Renovate's scope
and in the supply chain of a library whose core has zero non-stdlib dependencies.
So the harness sits on a branch, and `.github/workflows/perf.yml` on `main` is what brings
the two together at the moment it runs.

That arrangement has a second effect, and it is the reason to prefer it over vendoring:
the harness is always built against whatever `main` is when the job runs.
A harness on a branch that never merges would otherwise rot silently against core's API.
Here a build failure in the workflow is a real signal that core moved.

## The `replace` question, and what CI actually asserts

`.github/workflows/ci.yml` discovers modules by globbing `driver/*/go.mod` and prepending
`.`, and every assertion in the `invariants` job iterates that list:

- **no `replace` directive is checked in** iterates the discovered list;
- **no `go.mod` pins a toolchain** iterates the discovered list;
- **`go.work` uses every discovered module** requires `go work edit -json`'s use list to
  equal the discovered list *exactly*.

So `bench/` is outside every one of those assertions, and adding `./bench` to the root
`go.work` would break the third one - the use list would then hold a module the glob does
not find, and the step fails by design.

This module therefore does two things:

- **it is not in the root `go.work`**, which keeps that assertion passing and keeps the
  harness out of the four shipped modules' resolution entirely;
- **it carries a `go.work` of its own**, listing `..`, `../driver/env` and `../driver/yaml`,
  so anything run from this directory resolves core and the two drivers sibling-on-disk
  from the surrounding checkout.

The result is that `bench/go.mod` carries **no `replace` directive** and **no `require` on
core**, which is the same shape `driver/yaml/go.mod` has and for the same reason: core
carries no `v*` tag, so `github.com/onhotpath/ferry@v0.0.0` cannot be resolved from the
proxy, and a module with any third-party requirement loads the full module graph and reads
core's `go.mod` at the named version rather than taking the workspace copy.

`go mod tidy` will re-add those requires. Drop them again afterwards:

```
go mod tidy
go mod edit -droprequire=github.com/onhotpath/ferry \
            -droprequire=github.com/onhotpath/ferry/driver/env \
            -droprequire=github.com/onhotpath/ferry/driver/yaml
go build ./...
```

The root `Makefile`'s `MODULES` list does not include `bench`, so `make vet`, `make lint`,
`make test` and `make tidy` never touch it.
`make check`'s `gofmt` and `gci` steps walk files rather than modules, so this directory is
held to the same formatting as the rest of the repository, which is intentional.

## Running it

```
cd bench
go test ./...                                        # the equivalence gate
go test -run '^$' -bench . -benchmem -benchtime 1s -count 10 . | tee bench.txt
benchstat bench.txt > stat.txt
benchstat -format csv bench.txt > stat.csv
go run ./cmd/perfreport -csv stat.csv -stat stat.txt -raw bench.txt \
    -results ../docs/perf/results.md -link-dir docs/perf -readme ../README.md \
    -runner 'this workstation' -count 10 -benchtime 1s \
    -command "go test -run '^\$' -bench . -benchmem -benchtime 1s -count 10 ."
```

`-results` is a filesystem path and `-link-dir` is the repository-relative directory the
same files live in, which is what the README's links have to spell.
They are two flags because they are two different strings: the command runs from `bench/`
and writes to `../docs/perf`, and a README link of `../docs/perf/results.md` would resolve
nowhere.

`perfreport` writes three files, not one: the results markdown, and `perf-light.svg` and
`perf-dark.svg` beside it.
The charts are drawn by the same program from the same parsed benchstat output as the
tables, so they cannot disagree with each other, and both are rewritten on every run, so a
stale chart cannot survive one.

If the README carries no `<!-- ferry:perf:begin -->` marker the command fails, loudly,
rather than appending a section to the end of the file.
`-allow-missing-markers` downgrades that to a warning and is what the workflow passes, so
that the pipeline is usable before the pull request adding the markers has landed.

`go test ./...` is the gate.
It runs every library once against every scenario, outside any timed loop, and fails the
whole process - benchmarks included - unless all of them produce the identical populated
struct.
A benchmark whose columns are doing different work is worse than no benchmark, so the
refusal is not a test somebody can skip past with `-run`.

## The layout

| file | what |
| --- | --- |
| `config.go` | the two structs under test, and why there are only three tag keys |
| `fixture.go` | the expected value, the environment and the YAML document, written out three times independently |
| `scenario.go` | the `Impl` and `Scenario` types, the scenario list, and the list of libraries that were not measured |
| `impl_*.go` | one adapter per library |
| `equivalence.go` | the gate |
| `bench_test.go` | `TestMain`, the gate as a test, and the benchmarks |
| `internal/report/` | the renderer: benchstat CSV in, markdown and SVG out, with golden tests over real captured output |
| `internal/report/chart.go` | the chart's data model: rows, ordering, scales |
| `internal/report/chartdraw.go` | the chart's drawing: five element kinds, no script, no style, no external reference |
| `cmd/perfreport/` | the command the workflow runs |

## Adding a library

1. Write `impl_<name>.go` with one `Impl` per scenario it can express.
2. Add it to the scenario's `Impls` in `scenario.go`.
3. Run `go test ./...`. If its populated struct differs from every other library's, that is
   the gate working; fix the adapter, or, if the library genuinely cannot express the
   struct, take it out of `Impls` and put it in `Absences()` with the reason.

Never give a library an easier struct to make it pass.

## The chart

`perfreport` emits two SVGs, one per GitHub theme, referenced from a `<picture>` with a
`prefers-color-scheme` source.
Two files rather than one with an `@media` rule inside it, because a rule inside the file
depends on GitHub's sanitiser and image proxy leaving a `<style>` element alone and that is
not something to bet a published chart on.

The chart is drawn by hand in Go rather than by a charting library.
No dependency was worth it for two panels, and every element in the output is one of five
kinds - `svg`, `rect`, `circle`, `line`, `text` - which a test asserts, along with the
absence of `<script>`, `<style>`, `<image>`, `href` and anything else that would make the
file depend on something outside itself.

Three decisions in it are about honesty rather than taste, and each is stated on the chart:

- **The time panel is a log scale**, because the measurements span four orders of magnitude
  and a linear axis makes four competitors an indistinguishable smear.
  It carries **marks and not bars**: a bar's length reads as proportional to its value, and
  on a log axis it is not, so a bar twice as long would be a hundred times the number.
- **The allocation panel is linear and starts at zero.** A bar chart with a cut-off baseline
  is the commonest way a benchmark chart misleads, and the baseline is not an option this
  code offers.
- **Rows are ordered by the measurement**, so ferry appears wherever its number puts it.
  Nothing is coloured to make it stand out; a small ring beside its label is the only thing
  that identifies it, because a reader has to be able to find it.

A library that was not measured keeps its row and says `not measured` in both panels.
An absent bar reads as a zero, and a zero is a claim nobody measured.
