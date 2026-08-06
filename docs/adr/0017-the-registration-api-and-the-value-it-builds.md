# 17. The registration API, and the `Value` it builds

Status: Accepted
Date: 2026-08-06
Ticket: [#227](https://github.com/onhotpath/ferry/issues/227)

## Context

[ADR-0004](0004-source-and-sink.md) fixed `Value` as a kind and a source text, comparable, 24 bytes, six kinds, no escape arm.
[ADR-0009](0009-typed-codec-registration.md) fixed how a caller extends the type set: three constructors returning an opaque `Reg`, and a `*Registry` that freezes at its first use.

Both decisions have held.
What has not held is the space of things a caller can build with them that are wrong.

Four issue classes accumulated against that space, and they are all the same shape: **the API lets a caller name a fact that core then has to check at run time.**

- [#231](https://github.com/onhotpath/ferry/issues/231): a registration declares a kind and the codec emits a different one, so `Register` runs an emit check, and `VKind(200)` and `KindAbsent` are both writable arguments.
- [#223](https://github.com/onhotpath/ferry/issues/223): `Value.AsBool` reads `v.text == "true"`, so `TRUE`, `yes` and junk all decode to `false` with a nil error.
- [#227](https://github.com/onhotpath/ferry/issues/227) and [#262](https://github.com/onhotpath/ferry/issues/262): the registry has a mutable window between construction and freeze, and everything that can go wrong in it does.
- [#164](https://github.com/onhotpath/ferry/issues/164): the text-pointer constraint is unexported, so a caller reading a compiler error about `textPtr[T]` cannot look it up.

This ADR is written from the prototype on branch [`proto/02b-value-seam`](https://github.com/onhotpath/ferry/tree/proto/02b-value-seam), module `prototype/valueseam`, kept out of `go.work` and run with `GOWORK=off`.
It carries 21 tests.
Every number and every run below is from that prototype.

## Decision

### `Value` carries a payload, not a spelling

> ```go
> type Value struct {
>     kind VKind  // 1 byte
>     b    bool   // 1 byte, in what was already padding
>     s    string // String and Number carry text; Bytes an immutable copy
> }
> ```
>
> Still 24 bytes.
> Still comparable, and the `map[Value]struct{}` compile assertion still holds.

ADR-0004 said scalars carry source text and never a machine number, and that is still true of `Number`, deliberately, because arbitrary precision survives text and does not survive `float64`.
It was never true that a **`Bool`** should carry text, and the shipped code formatted one into `text` and then read it back with a string comparison.

```go
func Bool(b bool) Value          { return Value{kind: KindBool, b: b} }

func (v Value) AsBool() (bool, error) {
    if v.kind != KindBool { return false, v.wrongKind(KindBool) }
    return v.b, nil
}
```

**Unparsed text under a non-text kind stops being representable**, so [#223](https://github.com/onhotpath/ferry/issues/223) is not fixed but unwritable.
The parsing that used to happen inside the accessor moves to the boundary, where the plane's own spelling is known, which is [ADR-0018](0018-the-spelling-seam.md)'s.

**Four other layouts were costed:**

| layout | fields | size | comparable |
| --- | --- | --- | --- |
| shipped | `kind`, `text string` | 24 B | yes |
| **L1**, payload in padding | `kind`, `b bool`, `s string` | **24 B**, the bool rides existing padding | yes |
| L2, zero-copy bytes | plus `by []byte` | 48 B | **no**, and the compile assertion, map keys and every `==` in a test break |
| L3, unsafe union | tag plus `unsafe.Pointer` | 16-24 B | fragile, rejected on sight |
| L4, boxed payload | `kind`, `v any` | 24 B plus a heap box | panics on an incomparable dynamic type |

L1 costs nothing and buys the whole class.
`unsafe.Sizeof` is asserted at 24 in the prototype rather than claimed.

**`Bytes` copies in both directions**, which the shipped code already did, so this is a property restated rather than a change.
Immutability is load-bearing twice: it is what keeps `Value` comparable, and it means a caller's slice is never aliased by a value ferry holds.

**Every accessor refuses the wrong kind and never guesses.**
That is ADR-0004's own rule about not panicking, with the other half added: not guessing either.

**`Null` becomes a sentinel value rather than a constructor call.**

```go
var Null Value
```

There is one null and it carries nothing, so a function returning a value that is always the same value is surface spent on nothing.
The zero `Value` is `Absent` for the same reason and already was: a `map[Path]Value` miss **is** absence, which ADR-0004 relied on for the recording sink.
Both are the `io.EOF` idiom, and this ADR unifies them with [ADR-0016](0016-the-sealed-address-model.md)'s `SectionPresent`/`SectionAbsent`/`SectionNull` rather than having two conventions in one package.

### A registration is named after the kind it declares, so there is no kind to declare

ADR-0009's shape is `TextCodec[big.Int](ferry.Number)`: a constructor plus a kind argument.
The kind argument is the whole problem.
It is an argument, so a caller can pass `VKind(200)` or `KindAbsent`, and `Register` grows an arm to refuse each.

A sealed declaration type was built and tested, and it shrinks the forgeable space from 252 junk values to exactly one, because Go's zero value is uneliminable.
**Then it was asked what the declaration earns, and the honest answer is nothing**, because after the registration call no user code ever holds one.
The declared kind is read at compile, at the emit check, and by the conformance suite's golden column, and all three are core-internal.

> **The kind moves into the constructor's name.**

```go
func BoolValue[T any](enc func(T) (bool, error),   dec func(bool) (T, error))   Codec
func NumberValue[T any](enc func(T) (string, error), dec func(string) (T, error)) Codec
func StringValue[T any](enc func(T) (string, error), dec func(string) (T, error)) Codec
func BytesValue[T any](enc func(T) ([]byte, error), dec func([]byte) (T, error)) Codec

func NumberText[T any, PT TextPointer[T]]() Codec
func StringText[T any, PT TextPointer[T]]() Codec

func StringKey[T any](enc, dec) KeyCodec
func NumberKey[T any](enc, dec) KeyCodec
```

**The halves are typed by payload, so a user never constructs a `Value` at all.**
An encoder returns a `bool`, a `string` or a `[]byte`, and core wraps it.
A decoder is handed the unwrapped payload.

That closes [#231](https://github.com/onhotpath/ferry/issues/231) structurally rather than by checking: there is no way to declare one kind and emit another, so the emit check has nothing left to check.

**The surface arithmetic, stated because it is the one thing this loses on:**

```
sealed declaration:  TextCodec + ValueCodec + type Decl + 4 values  =  7 exports
kind-per-constructor: 4 x ...Value + 2 x ...Text + 2 x ...Key       =  8 exports
```

One more export, and what it buys is that there is nothing to forge, no zero value to refuse, no `Register` arm for a junk kind, and no collision between the four declaration values and `Bool`, `Number`, `String` and `Bytes`, which are the `Value` constructors and had the names first.

**What it costs, named rather than hidden**: a registration whose kind is computed at run time becomes unwritable.
No driver, test or issue has ever wanted one.

`Reg` is renamed **`Codec`**, and the key form **`KeyCodec`**, because that is what they are and because [ADR-0007](0007-the-codec-chain-and-its-precedence.md) already calls the thing a codec everywhere else.

**Key eligibility is structural.**
`AsMapKey` is a method that exists only on `KeyCodec`, so a bytes-keyed map does not compile rather than refusing at registration.
That is [ADR-0009](0009-typed-codec-registration.md)'s "a key codec says so" made a compile fact.

**`TextPointer[T]` is exported**, which is [#164](https://github.com/onhotpath/ferry/issues/164): a constraint that appears in a compiler error a user will read must be a name that user can look up.

### Construction is the freeze, so the mutable phase does not exist

> ```go
> func NewRegistry(codecs ...Codec) *Registry
> ```
>
> One type, one complete-set constructor, no mutators.
> A registry is complete at birth and there is no window in which it is not.

ADR-0009 argued at length about **when** a registry freezes, and answered "at its first use", because a registry was built empty and filled by `Register` calls.
Every defect in the [#227](https://github.com/onhotpath/ferry/issues/227) and [#262](https://github.com/onhotpath/ferry/issues/262) class lives in the window between those two moments.

Removing the window removes the class.
A duplicate registration refuses at `NewRegistry`, before `main` runs, rather than at whichever call happened to be first.

**A builder type was considered and refused**, and the reason is the same sentence: a builder recreates the mutable window, with an extra type to document.

**This supersedes ADR-0009's freeze mechanics and none of its reasoning.**
Everything ADR-0009 proved about *why* a registry must be long-lived, scoped rather than global, and part of the schema cache key is unchanged and is why this shape works at all.
What is retired is the staged-registration machinery underneath it, including the typestate view pair that was drawn to make the illegal **transition** unrepresentable.
An illegal **state** that cannot be represented beats an illegal transition that cannot be taken.

**A nil half panics at composition**, at the `NewRegistry` call site, rather than returning an error.
That is a programming error at a program's birth, in the same family as `regexp.MustCompile`, and the alternative is an error return on a line nobody checks.

> **Amended under [#273](https://github.com/onhotpath/ferry/issues/273): the default-registry question ADR-0009's supersession note recorded as owed is now decided.**
>
> As published, this ADR replaced ADR-0009's mutable surface and left one question open, recorded there rather than guessed at: whether a frozen default registry still exists for the caller who registers nothing, and how a caller adds one codec without a mutator.
> The ruling, in three sentences:
>
> > **Core's built-in type set survives as an unexported frozen base, and it is what a caller who passes no registry gets.**
> > **`NewRegistry` always composes the caller's codecs over that base**, so registering one type never costs the caller `string`, `int`, `bool` or any other built-in.
> > **A caller codec claiming a type the base already owns refuses at `NewRegistry`**, exactly as any duplicate does.
>
> The first sentence keeps ADR-0010's zero-configuration entry points working unchanged.
> The second makes the common case - the built-ins plus a handful of domain types - the only spelling there is, so there is no empty-registry trap where a one-line `NewRegistry` silently loses the standard library.
> The third is the duplicate rule already stated above, applied to the base with no special case: overriding a built-in codec would make user code a second authority over the standard types, and if a real need for that ever appears it is its own decision with its own name, not a silent capability of this constructor.
> How a caller adds one codec without a mutator is therefore the constructor itself: `NewRegistry(ferry.NumberText[big.Int]())` is the built-ins plus `big.Int`, complete on the line it is born.

> **Amended on the merge of the surface this ADR describes, and the amendment is what shipped where this text was elliptical.**
>
> Three things this ADR wrote in shorthand had to be spelled to be implemented, and each is recorded here rather than decided quietly.
>
> **`var Null Value` is `var Null = Value{kind: KindNull}`.** As published the snippet above reads `var Null Value`, which is the zero value and is `Absent`, and the paragraph under it says so in the same breath. The declaration carries the null kind.
>
> **`NewRegistry` has one result, so every refusal it makes is a panic.** The signature quoted above returns `*Registry` and nothing else, and the nil-half paragraph argues the case for `regexp.MustCompile`'s family against "an error return on a line nobody checks". That argument is not special to a nil half: a duplicate, a pointer type, a type core owns and a codec that is not total over its own zero value are all mistakes in the source of a program that has not started. What it panics with is a `*ferry.Error` of `ErrSchema`'s class at the register moment, so a caller who recovers one reads the report ferry gives every other refusal. The cost, stated: a caller cannot build a registry from a codec set it does not already trust, and no caller has ever wanted to.
>
> **`NumberText` and `StringText` return `KeyCodec` rather than `Codec`.** The export arithmetic above counts eight constructors and this changes none of it, because a `KeyCodec` is a `Codec` and is handed to `NewRegistry` the same way. Returning the narrower type would have deleted a shipped capability: [ADR-0007](0007-the-codec-chain-and-its-precedence.md)'s refusal of a chain-claimed map key names a registration with `.AsMapKey()` as the remedy, and a type that carries a text pair reaches that remedy through these two constructors and no other. The asymmetry with `StringValue` and `StringKey` is real and is explicable: where the caller supplies the encode half there are two constructors and the caller picks, and where the type supplies it there is only one, so it must carry both possibilities.
>
> Two resolution defects were found while making this real, and both are fixed in the same change because each is a place the shipped code already disagreed with a decision recorded elsewhere. [#229](https://github.com/onhotpath/ferry/issues/229): a pointer leaf dropped the pointee codec's whole-observation half, so a `*T` over a registered `T` decoded through a gate derived from the declared kind, which [ADR-0009](0009-typed-codec-registration.md) forbids in as many words. [#230](https://github.com/onhotpath/ferry/issues/230): map key resolution never consulted the text-pair arm, so a chain-claimed type whose underlying kind is a string or an integer was admitted as a key by kind and had one representation at the leaf position and another at the key position.

### `NullValue` is one modifier over any registration

A registration says how a `T` crosses the boundary.
It does not say what a plane's `null` means to that `T`, and for a type whose zero means "unset" the answer is not the pointer baseline.

> ```go
> func NullValue[T any](inner Codec, load func() (T, error), isNull func(T) bool) Codec
> ```
>
> A load policy: what `T` a `Null` becomes.
> A dump policy: which `T` values write `Null`.
> One export covering all four kinds.

Worked, on a `Level string` whose `""` means unset:

```
level: null   ->  Level("")     ->  dumps back as   level: null
level: warn   ->  Level("warn") ->  dumps back as   level: warn
```

> **The closure law: `isNull(load())` must hold.**

A policy that loads a sentinel it cannot recognise on the way back makes the round trip lie, silently, and only on the null path.
It is pinned by its own test and it becomes a `ferrytest` case, which is [ADR-0001](0001-what-ferry-supports.md)'s rule that core's leverage over what it does not ship is a conformance harness.

**The baseline stays structural and is stated in the godoc**: `*Level` with no policy at all already gives `level: null` to a nil pointer and back, through [ADR-0006](0006-defaults-and-zero-values.md) and no codec.
`NullValue` is for the case where the caller wants null as a value **of `T`**, which a pointer cannot express without changing the field's type.

**Two alternatives, both recorded.**

- **Keep one raw whole-`Value` codec as a documented corner.**
  Rejected: its other justifications - kind flexibility, interface fields - died with the payload-typed halves above, null was its last tenant, and this houses that tenant with less surface.
- **`ferry.Null[T]`, a core-pinned nullable wrapper** in `sql.Null`'s shape, handled structurally by the walk.
  It is honest sugar and it keeps `null` distinct from the zero, which the policy form merges by design.
  It is an [ADR-0005](0005-the-supported-type-set.md) type-set change, it competes with `*T` rather than with this, and it is recorded for post-v1 rather than proposed.

### What this ADR does not decide

- **What a plane's spelling of a payload is**: [ADR-0018](0018-the-spelling-seam.md)'s, and it is where the parsing that left `AsBool` went.
- **Whether the type set itself grows**: [ADR-0005](0005-the-supported-type-set.md)'s, unchanged.
- **The codec chain's precedence**: [ADR-0007](0007-the-codec-chain-and-its-precedence.md)'s, unchanged.
- **Whether `ferry.Null[T]` ever ships.** Post-v1, and it is an ADR-0005 question when it is asked.
- **When [#227](https://github.com/onhotpath/ferry/issues/227) and [#262](https://github.com/onhotpath/ferry/issues/262) close.**
  On the merge of the registry this ADR describes, with proving tests, and not before.
  No interim fix ships against a surface that is being replaced.

## Consequences

- **`Value` carries a payload rather than a spelling**, at the same 24 bytes and the same comparability, so `AsBool` answers from a `bool` or refuses.
  [#223](https://github.com/onhotpath/ferry/issues/223)'s class stops being writable, and the parsing it did badly moves to the boundary where the plane's spelling is known.
- **`var Null Value` replaces the `Null()` constructor**, and the zero `Value` stays `Absent`, so the package has one sentinel idiom and not two.
- **A registration names its kind and takes no kind argument**, which retires [#231](https://github.com/onhotpath/ferry/issues/231) and the emit check together.
  It costs one export over the sealed-declaration alternative and it makes a computed-kind registration unwritable, which nothing has ever asked for.
- **Key eligibility is a method that only exists where it is legal**, so a bytes-keyed map is a compile error.
- **`TextPointer[T]` is exported**, because a constraint a user reads in a compiler error has to be a name they can look up.
- **`NewRegistry` is the whole registry API and there are no mutators**, so the mutable window that [#227](https://github.com/onhotpath/ferry/issues/227) and [#262](https://github.com/onhotpath/ferry/issues/262) live in is gone rather than guarded.
  This supersedes [ADR-0009](0009-typed-codec-registration.md)'s freeze mechanics and keeps every argument it made for a long-lived, scoped registry.
  A builder was refused because a builder is the window with a type in front of it.
- **A nil half panics at the composition site**, which is a departure from ferry's own "accessors return errors and never panic" and is deliberately scoped to a program's construction rather than to its boundary.
- **`NullValue` is the escape hatch for null-as-a-value-of-`T`**, one modifier over any registration, with `isNull(load())` as a documented law and a `ferrytest` case.
  The cost is that it merges `null` and the zero by design, which is exactly its contract, and `*T` remains the way to keep them apart.
- **This ADR replaces a shipped surface rather than extending one**, so nothing here lands piecemeal: the `Value` layout, the constructors and the registry are one change with one set of proving tests, and the issues it retires close on that merge.

Evidence: `prototype/valueseam` on [`proto/02b-value-seam`](https://github.com/onhotpath/ferry/tree/proto/02b-value-seam), 21 tests, including `TestValueIs24Bytes`, `TestValueIsComparableAndMapKeyable`, `TestZeroValueIsAbsent`, `TestAsBoolNeverGuesses`, `TestBytesCopiesBothWays`, `TestSixKindsEndToEnd`, `TestEncodeKindCorrectByConstruction`, `TestKeyEligibilityIsStructural`, `TestNilHalfPanicsAtConstruction`, `TestDuplicateRegistrationRefuses`, `TestWithNullLoadsAndDumps` and `TestNullPolicyClosure`.
