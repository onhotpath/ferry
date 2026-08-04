# `proto/210-http` - the four remaining `driver/http` decisions

**This never merges.**
It is a throwaway prototype for [#210](https://github.com/onhotpath/ferry/issues/210), built to make four open questions measurable instead of arguable.
It lives outside the repository's root `go.work`, so `go build ./...` at the root does not see it, and it takes no third-party dependency.

## What it is

One package, `httpdecisions`, holding the same two multimap planes `proto/193-multimap` built - an HTTP query string and an HTTP header block, both exactly `map[string][]string` - with the shape question already settled.

`enumerated` won #193 and the ordering it needed landed in `main` as #209, so the shape is fixed background here.
`indexed` is kept beside it only because two of the four questions are stated against it.

What varies instead are the four axes the questions name:

| axis | type | values |
| --- | --- | --- |
| does a `Sink` ship, and what does it write | `SinkSpelling`, `SetSemantics` | `repeated` / `index-suffixed`; `as-in-193` / `append` / `replace` |
| what a name carrying both spellings means | `Clash` | refuse-in-children, index-wins, repeated-wins, repeated-wins-audited, first-spelling-wins |
| may a `Source` carry per-schema configuration | `Repeatable`, `CheckDeclaration` | on / off, checked / unchecked |
| where the scalar refusal lands | `Refusal` | close/in-text, close/`ErrorAt`, close/`ErrorAt`+text, get/refuse, never |

## The base it runs on

`proto/210-http-decisions` = `origin/main` (which already carries #209) merged with `origin/feat/202-caller-held-binding`.

The merge is textually clean and does not build: both branches add a test-only type named `counting` to package `ferry`.
The first commit on this branch renames the binding branch's to `countingPhases`.
That is a real conflict the binding branch has to resolve before it lands, and nothing more.

## How to run it

```
cd proto/210-http
go test -v .            # every question, every option, as t.Logf output
```

`run.txt` beside this file is that output, committed verbatim.
Nothing in the package asserts an expected answer: every test prints what happened, because the point is the measurement and not a regression guard.

Individual questions:

```
go test -run TestQ1 -v .   # sink or no sink, spelling, Set semantics, injectivity
go test -run TestQ2 -v .   # both spellings at one name
go test -run TestQ3 -v .   # per-schema configuration on a Source
go test -run TestQ4 -v .   # where the scalar refusal lands
go test -run TestCore -v . # a core defect question 4 walked into
```

## What it showed

**1. A sink is what proves the driver, and it does not have to be exported.**
Run against a plane with `Sink: nil`, `ferrytest.Driver` catches **none** of four injected read-side defects and emits 54 reports of "the plane mints no sink" instead; with a sink it catches every one of them.
The suite populates the plane by dumping into it, so a source-only plane has nothing to read.
`driver/env` already ships no `Sink` and supplies a stand-in one in `_test.go` for exactly this reason.

**2. Only three of the four clash policies exist.**
`first-spelling-wins` is not expressible: `?tags=a&tags=b&tags.0=z` and `?tags.0=z&tags=a&tags=b` parse to the same `url.Values`, so the wire order of two different names is not in the plane a handler is handed.
Refusing in `Children` costs nothing against the conformance suite and is the only option located in the walk.

**3. The binding does not make per-schema configuration safe.**
One `Source` carrying `Repeatable("Accept-Encoding")`, shared by a handler whose field is `[]string` and one whose field is `string`, silently zeroes the second - identically through `ferry.Load` and through `ferry.Bind`.
`ferry.Load` re-binds on every call (`Binds() = 3` for three loads), so a `Source` cannot enforce one-schema-per-source for itself either.

**4. `ferry.ErrorAt` works at `Close`, and core then drops everything but the first.**
Core wraps a `Close` failure with the zero `Path`, and `fromDriver` takes an address a driver named with `ErrorAt` where core has none of its own - so the refusal *is* attachable to `/q`.
But an `errors.Join` of two `ErrorAt`s keeps one and discards the other, silently.
`core_defect_test.go` isolates that in a source with no HTTP in it, at `Close` and at `Bind`.

The full argument, with every figure, is in `scratchpad/proto-210-decisions.md`.
