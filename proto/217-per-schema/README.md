# `proto/217-per-schema` - may a `Source` carry per-schema configuration?

**This never merges.**
It is a throwaway prototype for [#217](https://github.com/onhotpath/ferry/issues/217).
It lives outside the repository's root `go.work`, so `go build ./...` at the root does not see it, and it takes no third-party dependency.

## The question

[ADR-0004](../../docs/adr/0004-source-and-sink.md)'s lifetime table says a `Source` holds driver config that "never changes, you constructed it".
A schema arrives at `Bind`.
Some driver designs want configuration that is *about the schema* rather than about the plane - a multimap driver where the caller declares which names are sequences, so `Repeatable("tags")` on the `Source`.

[#210](https://github.com/onhotpath/ferry/issues/210) measured one such declaration silently zeroing a field, and a rule was written from that result rather than tested:

> A `Source` may carry per-schema configuration only where it is checkable against the `AddressSet` at `Bind`.
> The name-exists half is checkable; the is-it-a-container half is not.

**That rule is what this prototype tests.**

## The base it runs on

`proto/217-per-schema-config` = `origin/main` at `fc8707c` merged with `origin/feat/202-caller-held-binding` at `a3b8948`.

**The merge is textually clean and builds and tests green with no fixup**, which is a change from #210: that branch had to rename a test-only `counting` type, and the binding branch's rebase (`a3b8948`, "reconcile what moved underneath") has since resolved it.

## How to run it

```
cd proto/217-per-schema
go test -v .            # this prototype's own questions
go test -v ./repro210/  # #210's code, verbatim, on this base
```

`run.txt` and `run-repro210.txt` beside this file are that output, committed verbatim.
Nothing asserts an expected answer: every test prints what happened, because the point is the measurement and not a regression guard.

| | |
| --- | --- |
| `go test -run TestQ1 -v .` | #210's three results, re-measured in a second driver |
| `go test -run TestQ2 -v .` | the checkable `Source` and the not-checkable one |
| `go test -run TestQ3 -v .` | the rule test |
| `go test -run TestQ4 -v .` | what `AddressSet` actually makes checkable |
| `go test -run TestQ5 -v .` | whether core could help |
| `go test -run TestQ6 -v .` | whether a `Source` can pin itself to one schema |

`repro210/` is `proto/210-http-decisions`'s `proto/210-http` copied byte for byte, so a result that moved would show up as a diff in its output rather than as an argument.

## What it showed

**1. All three of #210's results reproduce, in its code and independently in this package's.**
One `Source` carrying `Repeatable("Accept-Encoding")`, two schemas: `{Encodings:[gzip]}` then `{Encoding:}`, no error.
Bit-identical through `ferry.Load` and through `ferry.Bind`.
`Binds()` is 3 for three `ferry.Load` calls and 1 for a held binding.

A fourth #210 result **has** changed, and it is not one of the three: the core defect where a joined `Close` error kept one address and discarded the rest is fixed on this base by #212 and #215, both of which landed after #210.
`Elements()` is now 2 for two, both located.
That matters because finding 2's fix puts a refusal at `Close`.

**2. The proposed rule does not hold, and it fails from both sides.**
Five configurations over one experiment - two handlers, each correct with its own `Source`, sharing one `Source` carrying handler A's configuration:

| configuration | checkable at `Bind` | can be false of a schema | B's answer changes | B's error |
| --- | --- | --- | --- | --- |
| `Prefix`, the control | yes | no | yes | none - silent |
| `Repeatable` | no | **yes** | yes | none - **silent** |
| `Repeatable`+`Audited` | no | **yes** | yes | `ErrPlane` - **loud** |
| `Alias` | **yes** | no | yes | none - silent |
| `Required` | yes | no | yes | `ErrPlane` - loud |
| `Fallback` | **yes** | no | yes | none - silent |

`Alias` and `Fallback` satisfy the rule and change a second schema's answer silently.
`Repeatable`+`Audited` violates it and is loud.
Checkability and safety are two axes, not one.

**3. `B's answer changes` is `yes` on every row, including the control**, which is what stops the experiment from being read as a defect criterion.
`Prefix` is ordinary plane configuration of exactly the kind ADR-0004's lifetime table blesses, and it lands in the same cell as `Alias` and `Fallback`.
Any configuration changes the second schema's answer, because that is what configuration does.

**What separates the rows is the `can be false of a schema` column, and only the two `Repeatable` rows are in it.**
A prefix, an alias and a fallback say where the plane holds something; a caller may not want one, but no schema makes one untrue.
`Repeatable("accept-encoding")` says the Go type behind that name is a sequence, and against `Encoding string` that is simply false.
The defect is the one cell that is falsifiable and silent.

**4. An `AddressSet` distinguishes a statically-membered container from a leaf, and cannot distinguish a dynamically-membered one.**

```
[2]string    -> [/tags#0 /tags#1]
struct{A}    -> [/tags/a]
[]string     -> [/tags]
map[str]str  -> [/tags]
string       -> [/tags]
```

The last three are byte-identical, and a multimap driver's `Repeatable` is only ever about those.

**5. Core holds the bit and discards it at one line.**
Core calls `Children` at `/accept-encoding` for the `[]string` and never for the `string`, from the same address set.
`compiler.addressSet` unions `leaves` and `containers` and drops which was which, and says in its own comment that it does so deliberately.
Nothing crosses back from the driver at `Bind` except an error, so core cannot refuse a mismatch it cannot see.

**6. The only guard a `Source` can build for itself is blind to the case it would be built for.**
Pinning the address set rather than counting `Bind` calls survives the one-shot path - three `ferry.Load` calls, `Binds()=3`, no refusal - and refuses a genuinely different schema.
It never fires for `Encodings []string` against `Encoding string`, because those two schemas have the same address set.

The full argument is in `scratchpad/proto-217-per-schema-config.md`.
