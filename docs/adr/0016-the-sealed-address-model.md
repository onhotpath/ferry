# 16. The sealed address model, and how a plane answers about a place

Status: Accepted
Date: 2026-08-06
Ticket: [#243](https://github.com/onhotpath/ferry/issues/243)

## Context

[ADR-0003](0003-how-a-leaf-addresses-a-plane.md) settled what an address is: a structured path of segments, canonical, prefix-free, never joined by core.
[ADR-0004](0004-source-and-sink.md) settled the shape a driver answers in: `Get(ctx, addr Path) (Value, error)`, with `Children` as an optional upgrade.
Neither settled what a driver may be **asked**, and the answer they left implied is "anything, at any address".

That answer produced a class of defect rather than a defect.
Ten issues were open against it when this ADR was written, and four of them are the same mistake at four addresses.

- [#219](https://github.com/onhotpath/ferry/issues/219): a struct field named `home` makes the env driver read the ambient `$HOME`, because `Get` was asked for a value at an address no value can be at, and the process environment happened to have one.
- [#235](https://github.com/onhotpath/ferry/issues/235): `LIMITS_HTTP_PORT` makes `Children("/limits")` mint `http` as a composite child, dropping the real leaf at `/limits/http` and inventing a phantom one.
- [#252](https://github.com/onhotpath/ferry/issues/252): a YAML mapping where the schema says leaf reads as `Absent` by the container rule, and the walk then inserts the Go zero, which is a fabricated value.
- [#239](https://github.com/onhotpath/ferry/issues/239): `string`, `[]string` and `map[string]string` at one tag compile to three **byte-identical** address sets, so a driver's container check compiles and can never fire.

The last one is the cause and the first three are symptoms.
An address that does not carry what kind of place it names cannot tell a driver which question is admissible there, so every driver is left to infer it, and each infers it differently.

Three further issues are the same cause on the write side and at the value's edge: [#255](https://github.com/onhotpath/ferry/issues/255), [#264](https://github.com/onhotpath/ferry/issues/264) and [#260](https://github.com/onhotpath/ferry/issues/260) are all about an array, and [#193](https://github.com/onhotpath/ferry/issues/193) and [#208](https://github.com/onhotpath/ferry/issues/208) are both about a query string spelling one address twice.

This ADR is written from the prototype on branch [`proto/03-address-kinds`](https://github.com/onhotpath/ferry/tree/proto/03-address-kinds), module `prototype/addresskinds`, which is kept out of `go.work` and run with `GOWORK=off`.
It carries 25 tests and 3 benchmarks.
Every number below is from that prototype.

## Decision

### Three sealed address types, and only the compiler mints them

> ```go
> type LeafAddr      struct{ /* sealed */ }  // a place a Value can be
> type SectionAddr   struct{ /* sealed */ }  // static children, known from the type
> type CompositeAddr struct{ /* sealed */ }  // dynamic children, minted by the value
> ```
>
> Each holds one unexported field.
> Nothing outside core can construct one, and the schema compiler is the only thing that does.

The sealing is [ADR-0009](0009-typed-codec-registration.md)'s own device applied one level down: an unexported field means a driver cannot write the composite literal and no conversion into a struct type exists, so a forged address is a compile error rather than a runtime refusal.

The three types partition the address space and they are not interchangeable.
`/db` as a `SectionAddr` and `/db` as a `CompositeAddr` are different addresses, and asking whether a set holds one answers nothing about the other.

**The contract is re-signed once, and the three questions separate:**

```go
type Reader     interface { Get(ctx context.Context, addr LeafAddr) (Value, error) }
type Prober     interface { Probe(ctx context.Context, addr Container) (SectionInfo, error) }
type Enumerator interface { Children(ctx context.Context, addr CompositeAddr) ([]Segment, error) }
type Writer     interface { Set(ctx context.Context, addr LeafAddr, v Value) error }
type Ensurer    interface { Ensure(ctx context.Context, addr Container, p Presence) error }
```

`Reader` and `Writer` stay required, as ADR-0004 has them.
`Prober`, `Enumerator` and `Ensurer` are optional and discovered by assertion, in the same idiom as `Releaser`, because a plane that cannot list also cannot always answer whether a section is there.

> **Amended when this shipped: `Probe` takes a `Container` and not a `SectionAddr`, and `Ensurer` carries a `Presence`.**
>
> As published, `Probe` was spelled over a `SectionAddr` and `Writer.Set` over a `LeafAddr`, and the ADR said nothing about how a plane answers, or is told, about a **composite's** own address.
> Both halves of the walk need one.
> The read half has to tell an absent mapping from an explicitly null one, which is the only thing that distinguishes a seed kept from a seed cleared; the write half has to say something at the address of a nil pointer, an empty slice and an empty map, which ADR-0005 writes a null at.
> Under the signatures as published neither is expressible: `Get` and `Set` refuse a container address by construction, which is the point, and `Probe` reached only half the containers.
> That the ADR itself names `CompositeRedirect{Target CompositeAddr}` as a redirect **state** is the evidence it intended a composite to be answered about.
>
> What moved: `Container` is a second sealed sum, over `SectionAddr` and `CompositeAddr` and nothing else, and `Probe` is asked about one.
> Probing a leaf is still a compile error, so the safety property the split exists for is unchanged, and the driver-side type switch it costs is on the same cold path as `Bind`'s.
> `Ensurer` is the write-side mirror: `Probe` reads a presence at a container address and `Ensure` writes one, so the two directions say the same three things in the same vocabulary.
> ADR-0004's rule that a composite with no elements is written as a null at its own address survives exactly, spelled as `Ensure(addr, PresenceNull)` rather than as a `Set` of `Null`.

**What this does to the four defects is not to fix them but to make them unwritable.**

| | before | after |
| --- | --- | --- |
| [#219](https://github.com/onhotpath/ferry/issues/219) | `Get("/home")` finds the ambient `$HOME` | `/home` is a `SectionAddr`; the question does not compile |
| [#235](https://github.com/onhotpath/ferry/issues/235) | `Children("/limits")` mints a phantom child over a leaf | `Children` takes a `CompositeAddr`; `/limits` is not one |
| [#252](https://github.com/onhotpath/ferry/issues/252) | a mapping at a leaf reads `Absent`, and a zero is fabricated | the plane's answer at a `LeafAddr` is a kind mismatch, refused with the address and what the plane holds |
| [#239](https://github.com/onhotpath/ferry/issues/239) | three identical address sets | `{Leaf /x}`, `{Composite /x}`, `{Composite /x}`, and the driver classifies at `Bind` |

Row four is deliberately incomplete in one direction: a `[]string` and a `map[string]string` are both `{Composite /x}`, and the difference between them is withheld because no driver needs it.
A driver mints `Name` or `Index` segments and the schema types the child; which of the two Go types produced the composite is core's business.

### The alternatives were built and measured, and performance is not the axis

Two other spellings were implemented in the same prototype, against the same env driver, so the comparison is between three working things rather than between one working thing and two sketches.

| spelling | address size | `Get` dispatch, same env lookup | allocs | the wrong question is caught |
| --- | --- | --- | --- | --- |
| **S1**, three sealed types | 16 B | ~18.8 ns/op | 0 | at compile time |
| S2, one `Path` plus `Kind()` | 24 B | ~16.4 ns/op | 0 | at run time, per call, **if the driver remembers to check** |
| S3, phantom types `Addr[K]` | 16 B | ~15.2 ns/op | 0 | at compile time |

The three dispatch figures differ by call shape - a pointer method against a free function against an interface - and all three are at zero allocations.
Read them as parity.
**Safety is the axis and cost is not**, which is worth stating because the opposite was assumed.

S2 is rejected because it keeps #219's class alive as a runtime error in every driver, and a check every driver must remember is a check some driver will not.
S3 is rejected on the diagnostic: a generic address type puts `Addr[leafK]` in every driver signature and in every compiler error, which is exactly the trade ADR-0004 refused when it declined `OpenFunc[T]`, argued there on driver cost and here on the same ground.

### An address set is one iterator over a sealed sum

> ```go
> type Member interface{ member() }   // LeafAddr, SectionAddr and CompositeAddr, and nothing else
>
> func (s *AddressSet) Seq() iter.Seq[Member]
> func (s *AddressSet) Has(m Member) bool
> func (s *AddressSet) Len() int
> ```

> **Amended when this shipped: `Member` carries `Path()` and `String()` beside the sealing method.**
>
> As published the block above read `type Member interface{ member() }`, and the shipped interface is
>
> ```go
> type Member interface {
>     Path() Path
>     String() string
>
>     member()
> }
> ```
>
> Both are load-bearing rather than convenience.
> `NewKeys` ranges the set and needs the path of every member without knowing its kind, which is the whole of what makes one injectivity pass over a mixed set writable; `String` is what a refusal naming an address prints.
> Without them every caller that does not care about the kind has to type-switch anyway, which is the cost the sum was chosen to avoid.
>
> **The sealing is a Go seal and not a proof.**
> Go seals a type and not an interface: embedding a `SectionAddr` in a struct outside core promotes the unexported method, so a value satisfying `Member` that core never minted can be written.
> Core refuses one - it is in no address set, `Has` answers false, and the kind ordering ranks it after all three so it equals none of them - and the godoc says that rather than claiming a closure Go cannot make.

Three methods, not three per kind.
The alternative drawn was `Leaves()`, `Sections()` and `Composites()`, and it was proven bind-equivalent - the env driver builds the identical key table either way - so the choice is made on surface count under the rule that every export is a contact point maintained forever.

A driver binds with one range and one cold-path type switch, and the typed addresses are comparable 16-byte map keys:

```go
for m := range set.Seq() {
    switch a := m.(type) {
    case ferry.LeafAddr:      d.keys[a]   = envKey(a)
    case ferry.SectionAddr:   d.prefix[a] = envPrefix(a)
    case ferry.CompositeAddr: d.mints[a]  = envPrefix(a)
    }
}
```

That loop is what [#239](https://github.com/onhotpath/ferry/issues/239) asked for and could not have: classification happens once, at `Bind`, before any I/O, and it is the same phase ADR-0004 reserved for exactly this.
It retires [#243](https://github.com/onhotpath/ferry/issues/243)'s proposed container bit, which was a bit on an address that a kind now carries structurally.

`Has` is one method because the kinds partition: `Has(SectionAddr{/db})` and `Has(CompositeAddr{/db})` are different questions and the prototype pins that they answer differently.

> **Amended when this shipped: `AddressSet` lost its exported constructor, and the static set grew a section per struct and per array.**
>
> As published, this ADR described the set's surface as three methods and said nothing about how one is built.
> `NewAddressSet(addrs ...Path)` was exported before this change, and keeping it would have been the forging door the sealing exists to shut: a `Path` in and a typed member out is exactly the conversion the unexported field forbids.
> It is unexported now, and the only thing that reaches it is the compiler.
>
> The cost falls on anything outside core that needs a set to bind a driver with, which is the conformance suite and nothing else.
> It compiles a fixture type and captures the set core hands the driver's own `Bind`, which is strictly better than what it did before: the suite can no longer hand a driver an address the compiler would not have minted.
> ADR-0014 carries the same note.
>
> The set also grew, and the ADR implied this rather than stating it.
> `#219`'s `/home` is a `SectionAddr`, and `/home` is an ordinary nested struct, which took **no** address at all under ADR-0003: only a composite that can be nil did.
> So every nested struct and every array now contributes its own `SectionAddr`, a slice and a map contribute a `CompositeAddr`, and a pointer contributes at the kind of what it points at.
> Without that a flat driver has nothing in its table to answer a probe about, and `/home` is unaskable rather than answerable, which is not what row one of the table above says.

**The payoff, run:**

```
HOME=/root  HOME_DIR=/data   ->  Get(/home/dir)  = "/data"
                                 Probe(/home)    = Present    (from HOME_DIR, not $HOME)
unset HOME_DIR               ->  Probe(/home)    = Absent     (though $HOME remains)
```

### A probe answers a `SectionInfo`, and three of the four answers are sentinels

> ```go
> type SectionInfo struct{ /* sealed; the zero value is absent */ }
>
> var SectionPresent, SectionAbsent, SectionNull SectionInfo
>
> func SectionAt(target SectionAddr) SectionInfo
>
> func (i SectionInfo) Presence() Presence
> func (i SectionInfo) Redirect() (SectionAddr, bool)
> ```

The type is named for its work, which is `fs.FileInfo`'s idiom: information about a section, answered by a probe.
The three plain answers are **values a driver returns rather than functions it calls**, which is `io.EOF` and `fs.ErrNotExist`.
The one answer carrying data keeps a constructor, named as the sentence the driver is saying.

A driver's `Probe` then reads as prose:

```go
switch node := d.at(addr); {
case node == nil:      return ferry.SectionAbsent, nil
case node.isNull():    return ferry.SectionNull, nil
case node.isAlias():   return ferry.SectionAt(d.targetOf(node)), nil
default:               return ferry.SectionPresent, nil
}
```

**One hazard is disclosed rather than designed away.**
Package-level sentinel variables are reassignable, so `ferry.SectionPresent = ferry.SectionNull` compiles, exactly as it does for `io.EOF`.
Go's own idiom accepts that, and the alternative - three nullary functions - was offered and not taken.

An earlier draft spelled the three as `State*()` constructor functions and was sent back: three global functions to say three constants is surface spent on nothing.

> **Amended when this shipped: the `Presence` constants carry the type's name, and core reads copies of the three sentinels.**
>
> As published, this ADR wrote `Presence` with the constants the prototype used, which were `Absent`, `Present` and `Null`.
> `Null` is taken: `ferry.Null` is the null `Value`, and Go has one package namespace.
> The three are `PresenceAbsent`, `PresencePresent` and `PresenceNull`, which is `VKind`'s own convention applied rather than reopened.
>
> The reassignability hazard is disclosed as this ADR disclosed it, and core does not stand in it.
> The exported sentinels are copies handed out once, and every comparison the walk makes is against an unexported copy, so `ferry.SectionPresent = ferry.SectionNull` breaks the assigning program's own comparisons and nothing else.
> That is exactly what ADR-0017 did for `ferry.Null` and it is the same three lines.

### A reference is a plane fact reported per observation, and core owns the resolution

The demand was that core model references rather than leave every driver to chase its own aliases.
The engineered answer is that **a redirect cannot be an address kind**, because an address kind is minted by the compiler from the schema and no Go type means "this section lives elsewhere".
There is nothing for `Compile` to mint.

What can exist is a redirect **state** in the answer, targeting its own kind:

```go
func SectionAt(target SectionAddr) SectionInfo     // sections
type LeafRedirect struct{ Target LeafAddr }        // leaves, an errors.As control error
type CompositeRedirect struct{ Target CompositeAddr }
```

The leaf arm is a typed control error rather than a `Value` kind, which keeps [ADR-0004](0004-source-and-sink.md)'s six-kind lattice closed.
It is `fs.SkipDir`'s shape: a sentinel-ish error that means "not a failure, a control answer".

> **Core owns the resolution loop.**
> A driver reports one hop; core follows the chain, keeps the seen set, and refuses a cycle.

Two postures were built side by side.
Posture A is the driver resolving internally and memoising, which is what a YAML driver does today.
Posture B is the above.

**B does not replace A and this ADR does not pretend it does.**
A target the schema does not address has no `SectionAddr` to report, so a YAML anchor pointing at an unmapped node stays driver-internal under either posture.
What B buys is that cycle discipline and hop control are written **once in core** instead of once per driver, and that every driver tells one redirect story.

Measured on the prototype: `/secondary -> /primary -> /defaults` resolves `Present` in two hops, `/a` and `/b` pointing at each other refuses in core with `reference cycle through /a`, and the boundary case refuses with a message that names whose job it is:

```
/alias refers to /unmapped, which the schema does not address;
the driver must resolve or refuse
```

#### The write side of a reference is divergence, and exploring the read side found it

Two aliases, one target, and the caller mutates one of them.
The dump has to decide, and none of the three answers is obviously right.

> **W1: an unchanged section keeps its link; a diverged one materialises at its own address, and the target and every other alias are untouched.**

Asserted by a test with two aliases of one target.
The two alternatives are recorded because both are defensible:

- **W2, write through.** Divergence propagates to every alias, which is YAML anchor semantics and is dangerous precisely because it is silent.
- **W3, refuse a diverged dump pre-write.** Loud and conservative, and it makes a legal Go mutation undumpable.

W1 is the generalisation of the memo rule the spelling seam already uses: what the plane said is preserved until the value says otherwise.

### Arrays are sections, and three issues are consequences rather than fixes

A `[3]int` field's children are `/a/0`, `/a/1` and `/a/2`, known from the type alone, exactly as a struct's are.

> An array is a **section** with compiled index children.
> It is never enumerated.

Pinned in `classify.go`, each line by a test:

```
Classify([3]int{})            -> section; children /0 /1 /2 compiled from the type
Classify([]int{})             -> composite; children minted by the value
Classify(map[string]int{})    -> composite
Classify([0]int{})            -> refused: maps no address, as struct{} already is
Classify([]chan int{})        -> refused: element types are checked through composites
Classify(map[string][0]int{}) -> refused, same rule
```

- [#255](https://github.com/onhotpath/ferry/issues/255), `compileArray` never recording the container, stops existing: a section records its address by construction, and there is no separate call to forget.
- [#264](https://github.com/onhotpath/ferry/issues/264), a `Name` child appearing under an array, stops existing: arrays are not enumerated, so there is no call that could mint one.
- [#260](https://github.com/onhotpath/ferry/issues/260), a zero-length array, is refused at compile the way `struct{}` already is.

> **Amended when this shipped: the over-length array refusal goes with the enumeration that produced it.**
>
> As published, this ADR said an array is never enumerated and did not say what happens to the check that lived on that enumeration.
> Core used to ask a source that could list what it held under an array's address, and refuse an index the array has no element for: "the plane holds index 5 and [3]int holds 3".
> `Children` now takes a `CompositeAddr` and an array is a section, so the call cannot be written and the check has nowhere to stand.
>
> That is a refusal lost, and it is stated rather than left to be found.
> A plane holding `A_5` for a `[3]int` is now read as three elements and the fourth and sixth are ignored, where before an enumerating source refused the load.
> The refusal was only ever available over a source that enumerates, so it was never the property it looked like, and getting it back means asking a plane to list a place whose membership the type already fixes.
> The trade is deliberate: `#264`'s defect and this check are the same call, and the call is the thing the model removes.
>
> **The worst combination, named rather than left to be discovered.**
> "Ignored" understates it where the array is behind a pointer and the plane is flat.
> A section's presence is the presence of its members, so `P_9=9` alone against a `*[3]int` at `/p` made the section present and the load handed back `&[0 0 0]` - a whole array conjured out of a name no address of the schema renders to.
> That half is closed: this plane's presence question is scoped to the members the type determined, so a name past the end says nothing about the section, and `driver/env`'s `TestAVariableSharingASectionsPrefixIsNotTheSection` pins it.
> What remains is the loss as stated: a value-typed `[3]int` with `PAIR_9` set loads `[0 0 0]` and the name is read by nothing, pinned by `TestAnIndexPastTheEndOfAnArrayIsIgnored` so that it cannot change back in silence.
> It is consistent with how these planes treat every unmapped plane key: neither `driver/env` nor `driver/kv` has any notion of one, and a plane key no address of the schema names is not this schema's business.

### A repeated plane key mints an index, and a repeated key at a leaf refuses

`?tags=a&tags=b` into a `[]string` was unaddressable, because the container rule refused at `/tags` before any driver code ran.

> `/tags` for a `[]string` is a `CompositeAddr`, and the driver's `Children` **mints index children from repetition**.
> Order is the plane's order, and a plane with no defined order documents its minting order.

```
?tags=a&tags=b&limit=1   ->  Children(/tags) = [0, 1]
                             Get(/tags/0) = String("a")   Get(/tags/1) = String("b")
```

The second dimension was never a key-function problem.
It is enumeration, which is what composites already do, and this is the mechanism [#193](https://github.com/onhotpath/ferry/issues/193) needed.

**And the reverse case gets its refusal, which is [#208](https://github.com/onhotpath/ferry/issues/208)'s complaint dissolving rather than being answered.**
`?limit=1&limit=2` names a `LeafAddr` twice.
The driver knows `/limit` is a leaf because classification arrived at `Bind`, so it refuses loudly, at the address, naming the key and the count, before the walk.
#208 said a driver could not refuse at a container `Get`; there is no container `Get`, and the leaf refusal has a home.

This is [ADR-0015](0015-two-spellings-of-one-address.md)'s rule in the other direction: that ADR refuses two keys reaching one address, and this one refuses one key arriving twice at an address that holds one value.

### A present-but-empty section is writable, and refused where it is unspellable

Go can express empty-but-present: a non-nil `*Options` whose every field is omitted.
[ADR-0006](0006-defaults-and-zero-values.md)'s replace rule writes those fields nowhere, so the section vanishes from the plane and the round trip turns present-empty into absent.
`Null` cannot stand in, because a `Null` at a container retracts.

> When a realised section emits zero child writes, the dump makes **one section-level write**.
> A plane with no spelling for it refuses at open, before any write.

That is the `Ensurer` capability [ADR-0004](0004-source-and-sink.md) gains, and its refusal is the same shape as `Unsetter`'s.

> **Amended when this shipped: the section-level write is scoped to a nullable section, and the refusal lands at the write.**
>
> As published, this said "when a realised section emits zero child writes" without saying which sections, and "refuses at open, before any write".
> Taken at face value it applies to every struct, and it must not: a plain nested struct always exists in Go, so every struct whose fields were all omitted would emit a section write, and ADR-0006's rule that an omission is not a deletion would not survive it.
> It is scoped to a **nullable** section, which is a non-nil pointer, and that is the only shape where present and absent are distinguishable on reload at all.
> `Config{Opts: &Options{}}` is the worked example in this ADR and it is a pointer.
>
> The refusal is at the write and not at open.
> Whether any section-level write happens at all depends on the value, and a bind sees no value; refusing at open would mean refusing every schema with an optional section on every flat plane, which is most schemas and most planes.
> It is one `ErrPlane` naming the address and the sink, made before that address is written and after the addresses before it were, which is where the rest of the write side already refuses.

Worked on `Config{Opts: &Options{}}`:

```
yaml   ->  opts: {}          reload: Probe(/opts) = Present, Opts != nil    round trip closed
env    ->  REFUSED at open:  the plane cannot spell an empty section at /opts
```

The two alternatives, and why each loses:

- **Accept the degradation.** Present-empty becomes absent on reload, documented. It is a silent divergence, which is the class this whole address model exists to remove.
- **Refuse everywhere.** A legal Go value becomes undumpable on every plane, including the planes that can carry it faithfully.

The rule taken is the loud refusal scoped to exactly the planes that cannot spell the value.

### What this ADR does not decide

- **Which Go types produce which address kind beyond the classification above**: [ADR-0005](0005-the-supported-type-set.md)'s, applied here rather than reopened.
- **Whether `Prober` is ever required.** It is optional for the same reason `Enumerator` is, and a plane that implements neither loads static leaves and nothing else.
- **Whether a driver may report a redirect it invented rather than read.** Nothing checks it, in the same family as ADR-0004's three optional interfaces, and it is a conformance case.
- **What a driver does with a target outside the schema.** Posture A, and it stays the driver's.
- **The concurrency of enumeration**: [ADR-0019](0019-the-concurrency-model.md)'s.

## Consequences

- **Three address types replace one `Path` in the driver contract, and the wrong question stops compiling.**
  Four open issues are retired as classes rather than as instances, and the deciding evidence is that the two alternatives were built and measured at parity: 16 B and ~18.8 ns/op at zero allocations against 24 B and ~16.4 ns, so safety was the only axis left.
- **A driver's `Bind` classifies once, from one iterator over a sealed sum**, which is what [#239](https://github.com/onhotpath/ferry/issues/239)'s identical address sets made impossible.
  The cost is a type switch in every flattening driver's `Bind`, on the cold path, and a tree driver still pays nothing.
- **Three methods on `AddressSet` rather than three per kind**, chosen on surface count after both were proven to build the identical key table.
- **`SectionInfo` with three sentinel values and one constructor** follows `fs.FileInfo` and `io.EOF`, and inherits `io.EOF`'s reassignability hazard, which is disclosed rather than designed away.
- **Core resolves schema-addressable references and refuses cycles once**, instead of every driver doing it.
  It does not replace driver-side resolution and cannot: an unmapped target has no address to report.
  The honest description is a mechanism layered over the existing one rather than a replacement for it.
- **A diverged alias materialises at its own address on dump**, leaving the target and every other alias untouched.
  Write-through is more faithful to YAML's own semantics and is refused because its damage is silent.
- **Arrays are sections**, which retires three issues by making the code that produced them unreachable rather than by fixing it.
- **A repeated plane key is an index on a composite and a refusal on a leaf**, which is [ADR-0015](0015-two-spellings-of-one-address.md)'s rule met from the other side.
- **A present-but-empty section is a write**, so `Ensurer` exists and a flat plane refuses at open.
  The cost is one more optional capability and one more per-driver documented refusal.
- **This ADR does not change `Value`.** The leaf redirect is a control error precisely so the six-kind lattice stays closed, which is [ADR-0004](0004-source-and-sink.md)'s no-escape-arm decision surviving a case that looked like it needed one.
