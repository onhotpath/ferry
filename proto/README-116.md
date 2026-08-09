# proto/116-refusal-remedy

Throwaway prototype for [#116](https://github.com/onhotpath/ferry/issues/116), "ADR-0005 lists uintptr as both a permanent refusal and a policy refusal".
Never merges.

Branched from `main` at the published state, so the probes run against the code and the messages as shipped.
That is the point: `TestP6` prints the refusal saying no codec can be written, and then writes one.

Run:

```
go test -v ./proto/                         # P1 to P6
cd driver/yaml && go test -v -run TestP7 .  # P7, the plane with a format
```

## The question

ADR-0005 sorts refusals by "what actually limits them" and puts `uintptr` in two categories at once.
They differ in exactly one observable way: **(c) offers registration as the remedy and (a) says nothing lifts it.**
So the contradiction reaches a user as a question with two answers.

The ADR says the sort is "tested against a registered codec rather than reasoned about", so that is the test these probes run.

## Probes

| # | question | result |
| --- | --- | --- |
| P1 | does a `uintptr` round-trip through a plane | **yes, bit-exact**, including `MaxUint64` |
| P2 | does what it points at survive | **no.** The object was collected while its address sat in the plane, and the dereference still read the old value: a use-after-free that looks like success |
| P3 | does a codec register for each of the four "permanent" kinds | **yes, for all four**, `func` included |
| P4 | what does the registry actually gate on | **totality over the zero value**, and nothing else. Map the zero to `""` and all four pass |
| P5 | does a `chan` come back through a plane | **yes**, via a name table. `chan` is comparable and `func` is not, which is the one real asymmetry inside (a) |
| P6 | the named `uintptr`, which is how the kind reaches a real struct | refused with "no codec can be written for it", then lifted by a codec in the next line |
| P7 | through a plane with a serialization format | **bit-exact through a YAML file**, both a heap address and `MaxUint64` |

## What the probes found

**Category (a) is empty, and the implementation never enforced it.**

`classify` consults the identity table before `reflect.Kind`, so a registered type is a leaf and the kind switch that refuses these is never reached.
`permanentlyRefused` in `schema.go` selects a *message* and refuses nothing.
So "Nothing lifts these, and they are the only permanent refusals" was false in the shipped binary, for all four members rather than only for the `uintptr` the issue found.

**The qualifier in (a)'s list was never implementable.**

(a) reads "`uintptr` used as a real pointer".
A `uintptr` holding an address and one holding an offset are the same `reflect.Type`, so no implementation can act on the distinction.

**The two categories were each half right, which is why it read as a contradiction.**

(c) is right about the number: P1 and P7.
(a) is right about the referent: P2.
Nothing in the type separates them, so the type-level answer has to be one or the other.

**What differs between the four kinds is the reason, not the verdict.**

- `func` is the sharpest: not comparable, so encode cannot ask which function it holds and only `nil` can work.
- `chan` is comparable, so a name table serves both halves, and its identity is still local to this process.
- `uintptr` is an integer the collector does not track. It is a size, an offset or a handle at least as often as it is an address: `unsafe.Sizeof`, `unsafe.Offsetof`, `reflect.StructField.Offset`, `syscall.Handle`, `os.File.Fd`, a memory-mapped base address.
- `unsafe.Pointer` is only ever an address, and `go vet` reports `possible misuse of unsafe.Pointer` on the decode half that rebuilds one from a number. Nothing says the same of the `uintptr` codec.

## What landed

Core declines to guess a representation, a codec supplies one, and that is true of every refusal.
The categories keep their reasons and lose the verdict they disagreed about.
Nothing that compiled before stops compiling, and no new type is admitted without the user's own codec.
