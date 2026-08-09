# feat!: an address carries the kind the schema wants at it, and the environment's words stop deciding for the whole plane

Part of the #309 campaign.

This is the K2 promotion.
A typed address gains one accessor, `LeafAddr.Wants() VKind`, so a plane that carries no type information of its own can apply a spelling exactly where the schema asks for one.
`env.BoolWords` is the first caller and the whole of the driver-side change is one expression.

Breaking: loads that were refused now succeed.
A `string` field over a variable holding a declared boolean word loads the text instead of being refused, which is the sharp edge ADR-0018 ratified and this issue removes.

Ratified shape, from the surface exploration on `proto/309-kind-gate`: **candidate C**, one exported name, no accessor on `CompositeAddr` until a driver asks for one.
Three other spellings were built and run against the same exhibit and are not what ships: two methods on `AddressSet`, one method on `AddressSet`, and no new name at all.
The last is not a matter of taste and is refuted by the seal: every existing question on a set is asked with an address, no address can be constructed outside core, and "does the schema want a bool here" needs a kind in the question.

### Depends on

Independent of #334, #336, #337, #338 and #339.
Those touch `driver/kv/key.go`, `driver/kv/option.go`, `driver/yaml/tree.go`, `driver/env`'s key function and `ferrytest`'s case 23, and this issue touches none of them.

**Depends on #335 in one line of code, and only if #335 lands first.**
#335 adds `compileRootLeaf` calling `recordLeaf(site{owned: true})`.
This issue changes `recordLeaf`'s signature to carry the kind, so whichever lands second updates the other's call site:

- #335 first: this issue writes that call as `c.recordLeaf(site{owned: true}, cd.kind)`, which is one argument and is the desirable outcome anyway, because a root leaf then answers `Wants` like every other leaf and a driver spelling the root reads it the same way.
- This issue first: #335's spec line for `compileRootLeaf` gains the kind argument, taken from the codec `rootLeafCodec` already resolved there.

There is no ordering constraint beyond that one argument.

**Rebase risk, stated against `origin/main` at `17e2839`.**
`main` moved five commits since the prototype's base at `2165378`, and `schema.go` is +113 lines across #116 and #224.
None of it touches the functions this issue edits: the drift is line numbers only, and every citation below is against `17e2839`.
The prototype commits `12c045e`, `8e7293a` and `d2029c4` will **not** cherry-pick, for two reasons rather than one: the line drift, and the fact that the prototype tree deliberately carries all four candidate surfaces at once behind a `gate` interface and a `using(...)` option so that they could be compared.
Re-author from this spec.
What ships is the intersection of `d2029c4`'s candidate C paths and nothing else.

### Exact changes

**`addr.go`.**

- `LeafAddr` at line 27 becomes a struct with a second field: `p Path` and `want VKind`.
  The doc comment gains a line saying the address carries what the schema wants at it, in the caller's language and with no ADR or issue number.
- `CompositeAddr` at line 45 gains the same unexported `want` field, holding the kind the schema wants at the leaves its value mints and `KindAbsent` where its members are not leaves.
  **It gets no exported accessor.**
  The fact is carried so that publishing `ElemKind` later is one method body and no plumbing, and so that a composite address the walk mints is `==` to the one the set holds.
  `SectionAddr` at line 35 is untouched: a section holds no value and has no kind to want.
- Publish exactly one name:

  ```go
  // Wants is the kind of value the schema wants at this address.
  func (a LeafAddr) Wants() VKind { return a.want }
  ```

  It is named `Wants` and not `Kind` deliberately, and the reason is a hazard rather than taste.
  A `LeafAddr` is a parameter of `Writer.Set`, where the `Value` in hand also answers `Kind()`, and the two can legitimately disagree: a nil pointer leaf encodes a `Null` at an address whose schema kind is the pointee's.
  The godoc must say that a driver answers with what its plane holds and never with what this returns, and that a leaf accepts a `KindString` beside its own kind whatever this says, so `Wants` is what the schema wants rather than the whole of what it takes.
- The kindless mint is **deleted**.
  `leafOf` and `compositeOf` at lines 129 and 131 take the kind: `func leafOf(p Path, want VKind) LeafAddr` and `func compositeOf(p Path, want VKind) CompositeAddr`.
  `sectionOf` at line 130 is unchanged.
  What enforces it is the compiler and nothing else: there is no arity that compiles without a kind, so a mint that forgot one is a build failure rather than an address that is silently equal to nothing.
  The prototype kept a kindless `leafOf` beside the kind-carrying one to spare its test files, and that is exactly the hole this bullet closes.
  Test files that call `leafOf` must be updated rather than served by a second constructor.
- `memberAt` at line 147 takes the kind and passes it to the leaf and composite arms.
- The comparability assertions at lines 119 to 123 stay as they are and stay true.
- Nothing else on `AddressSet` changes.
  `KindAt`, `ElemKind` and `Kind` on the set exist in the prototype as candidates A and B and **none of them ships**.

**`schema.go`.**

- `leaf` at line 206 gains `kind VKind`.
  It is the codec's declared kind for a leaf entry and the element's for a composite entry, which is why the field is on the one record type both use.
- `recordLeaf` at line 820 becomes `func (c *compiler) recordLeaf(s site, want VKind)` and records it.
- `recordComposite` at line 846 becomes `func (c *compiler) recordComposite(s site, elem VKind)`.
  Its two callers at lines 751 and 780 pass the element kind, resolved with one helper asking `c.cfg.registry.leafFor(t.Elem())` and answering `KindAbsent` where the element is not a leaf.
  The third caller at line 867 passes `KindAbsent` or the pointee's element kind, whichever the position it stands at determines.
  **The prototype's separate `compiler.elemKinds map[Path]VKind` and `recordElemKind` do not ship.**
  They existed to key candidate A's second table, and with the kind on the composite record there is nothing left to key.
- `compileLeaf` at line 976 passes `cd.kind` at line 986: `c.recordLeaf(s, cd.kind)`.
- `addressSet` at line 1322 mints `leafOf(l.addr, l.kind)` and `compositeOf(k.addr, k.kind)`.
  The prototype's `leafKinds` and `compositeElemKinds` helpers do not ship, for the same reason the map does not.

**`walk.go`.**

This is what makes an address a value minted answer for itself, and it is the whole reason candidate C is smaller than candidates A and B rather than merely differently shaped.
A node under a dynamic composite is compiled once at the address shape its members share, and the walk stands at the address it realised, so the node it is standing at already knows what the schema wants there.

- `spot.leaf()` at line 208 becomes `leafOf(s.at, s.n.codec.kind)`.
- `spot.composite()` at line 224 and the composite arm of `spot.container()` at line 214 pass the element node's kind.
  Add one unexported helper on `spot` reading `s.n.fields[elemShape].codec.kind` and answering `KindAbsent` where the node has no fields, and keep `container` under the complexity limit by putting the branch in the helper rather than inline.
- `mint` at line 1462 passes the element's kind into `memberAt` rather than `KindAbsent`.
  Membership compares path and address kind and ignores the want, so this does not change what `Has` answers, and it is required all the same: an address built for a question must be `==` to the address the set holds, or the two ways of asking about one place disagree and a driver keying a table by address is the code that finds out.

**`driver/env`.**

- `option.go`: `config` at line 36 gains nothing.
  The prototype's `config.gate` field and `using(...)` option are the candidate scaffolding and do not ship.
- `env.go`: `Source.Bind` at line 61 gains nothing, `declaredLeaves` at line 106 gains nothing, and `reader` at line 261 gains nothing.
  This is the measurement: candidate C costs the driver **no** `Bind` pass, **no** table, and **no** prefix scan over composite paths.
- `env.go` line 320, inside `reader.Get`, is the only changed line of the read path:

  ```go
  if text, ok := r.env[key]; ok {
      return r.cfg.observe(addr, text), nil
  }
  ```

- `words.go`: `config.observe` at line 167 takes the address and gates on it, which is the entire driver-side implementation:

  ```go
  func (c config) observe(addr ferry.LeafAddr, text string) ferry.Value {
      if c.bools == nil || addr.Wants() != ferry.KindBool {
          return ferry.String(text)
      }

      b, err := c.bools.Parse(text)
      if err != nil {
          return ferry.String(text)
      }

      return ferry.Bool(b)
  }
  ```

  The existing unexported doc comment keeps its ADR citation and gains a sentence saying the words are consulted where the schema wants a bool.

- **The write side gets no gate, and the issue says so rather than leaving it to be discovered.**
  A `Value` already carries its kind, so a sink renders a `KindBool` with the words and everything else as text, and there is nothing for an address to decide.
  The whole change is load-side.

**Godoc, `driver/env/words.go` lines 27 to 31.**

Write the new sharp edge before deleting the old one, because it is the same sentence one clause further on and deleting first is how it gets lost.

The old text:

> The sharp edge is that this is a fact about the whole environment and not about one field, which is what makes it declarable at all.
> A variable holding one of these words arrives as a boolean wherever it is read, so a string field over FEATURE=on is then a value the field cannot take rather than the text "on": choose words your text values do not use.

The new text, in the caller's language, citing no ADR and no issue:

> The words are consulted where the schema wants a bool and nowhere else, so a string field over FEATURE=on loads the text "on".
> The sharp edge is what that means for two programs reading one environment: a variable holding a declared word is a boolean where the schema wants one and text where it does not, so one variable can be read two ways, and which way is the schema's business rather than the environment's.

`driver/env/README.md` lines 118 and 119 carry the same claim in the same words and change with it.
`ExampleBoolWords` at `driver/env/example_test.go:87` stays true and is unchanged; the README quotes it verbatim, which is what keeps the two from drifting.

### Tests

The exhibit is the point, and it is one environment read two ways.

`driver/env`, new file:

- `TestTwoReadersOfOneEnvironment`: `FEATURE=on`, `env.BoolWords("on", "off")`.
  A schema with `Feature bool` loads `true`.
  A schema with `Feature string` loads `"on"`.
  Both through `ferry.Load`, both from one `Environ` closure.
  On `main` today the second is refused with `ferry: FEATURE: what is set here is bool, and string cannot take one`, which is the run this issue changes.
- `TestTheCollisionInsideOneSchema`: one schema holding `Feature bool` and `Mode string` over `FEATURE=on` and `MODE=on`, loading `{true, "on"}`.
  This is the case the deleted sharp edge was about, and it is the reason the change is worth making: the old remedy, choosing words your text values do not use, is not available to a schema author who does not own the `Source`.
- `TestAMintedAddressIsGatedByItsOwnKind`: `map[string]bool` from `FLAGS_BETA=on` loads `beta:true`, and `map[string]string` from the same variable loads `beta:"on"`.
  This is the test that pins the `walk.go` plumbing.
  Measured on the prototype with the answer withheld from minted addresses, the first case fails with `ferry: FLAGS_BETA: what is set here is not a valid bool`, so this is a regression test and not a formality.
- `TestTheRoundTripCloses`: dump `Feature string` holding `"on"` through a flat text sink that renders `KindBool` with the same words, then load it back through `env` and get `"on"`.
  There is no `env.Sink` and this package will not grow one, so the sink is a test-local flat plane over `config.key`, and it is the write-side half of the exhibit rather than a driver.
  This is the strongest argument in the change and it belongs in the tests: on `main` ferry writes a plane through a sink that ferry then refuses to load through its own source, for the same field.
- Benchmark: one `Load` through a `Binding`, kept from the prototype, so the parity claim stays measured rather than remembered.

`ferry`, new file:

- `TestWantsAnswersTheSchemasKindPerLeaf`: through `Bind` and a probe source that records the set it was handed, which is `kinds_test.go`'s own idiom at `kinds_test.go:15`.
  A schema covering `bool`, `string`, `int`, `[]byte`, `time.Duration` and a `[2]bool` array, asserting `KindBool`, `KindString`, `KindNumber`, `KindBytes`, `KindString` and `KindBool` at both element addresses.
- `TestAMintedAddressCarriesTheSchemasAnswer`: the address handed to `Reader.Get` under a `map[string]bool` answers `KindBool`, asserted at the driver seam rather than by reaching into the walk.
- `TestAnAddressIsEqualToTheOneTheSetHolds`: for every leaf in a set, the address the walk hands a driver is `==` to the member the set holds, and a table keyed by the set answers for it.
  This is the equality-versus-membership hazard, pinned.
  Membership compares path and address kind, equality compares every field, so an address minted without the schema's answer would be in the set and a key in no table built from it.
  With the kindless mint deleted the case is unwritable, and this test is what says so out loud if somebody adds a constructor back.
- `TestTheAddressWeighsWhatItWeighs`: `unsafe.Sizeof` for `Path`, `SectionAddr`, `LeafAddr` and `CompositeAddr`, logged and asserted, so the ADR's corrected figures cannot drift in silence.

Measured on the prototype, and these are the numbers the ADR amendment carries:

```
Path 16 B, SectionAddr 16 B, LeafAddr 24 B, CompositeAddr 24 B
Load, 300000x5:  shipped ~603 ns/op   gated ~597 ns/op   both 12 allocs, 817 B/op
```

### ADR amendments folded in

One commit, both ADRs, amended in place with the what-moved note, in the house style.

**ADR-0016, `docs/adr/0016-the-sealed-address-model.md`.**

- A new amendment block under the sealed-types section: as published, an address said what kind of place it names and nothing about what is at it, so a plane with no type information of its own had to decide a spelling for the whole plane or not at all.
  What moved: a typed address carries the kind of value the schema wants at it, minted by the compiler for a static address and by the walk from the node it is standing at for one a value produced, so an address answers for itself and a driver reads it off the address it was already handed.
  Say what it costs, which is that the mint takes the kind at every site, and what that buys, which is that a driver needs no table and no prefix scan for the addresses it did not get at `Bind`.
  Say that a composite carries the same answer for its members and publishes nothing, and that a section carries none.
- Correct the published measurement in two places rather than one.
  Line 137 reads "the typed addresses are comparable 16-byte map keys" and is now wrong for two of the three: `LeafAddr` and `CompositeAddr` are 24 bytes and `SectionAddr` is 16.
  The consequences bullet at line 447 quotes "16 B and ~18.8 ns/op at zero allocations against 24 B and ~16.4 ns" as the deciding evidence between S1 and S2, and that sentence stays as history and gains the note that the shipped address is now the same width as the alternative it was measured against.
  **Nothing about the S1-over-S2 decision moves, and the correction must say so explicitly.**
  S2 was rejected because a `Path` plus a `Kind()` leaves the wrong question compiling and caught at run time if a driver remembers to check; what ships is still three sealed types, and the width is a payload on a typed address rather than a classification a driver has to test.
- The table row at lines 92 to 94 is a record of what was measured then and is not edited.

**ADR-0018, `docs/adr/0018-the-spelling-seam.md`, lines 127 to 130.**

That block ratifies `env.BoolWords` as plane-wide and parks the kind-gated refinement at #309 on the ground that the address set exposes no per-address kind.
It is amended in place: the contingency it names is satisfied, the refinement shipped, and what the option means changed with it.
Keep the sharp edge and rewrite it, because it is still a sharp edge and it is a different one: a variable holding a declared word is a boolean where the schema wants one and text where it does not, so two programs reading one environment can read one variable two ways, and the schema decides which.
Say plainly that the old remedy is retired: choosing words your text values do not use is no longer the answer, because there is no longer a collision to avoid inside one schema.
Say that the write side is unaffected and why, which is that a `Value` carries its own kind and the seam's laws are unchanged.
The worked case at line 140 stays true and is not edited.

`docs/guide/drivers.md` lines 745 to 767 describe the seam and its laws rather than the plane-wide rule, and need no change; check them at the time rather than trusting this sentence.

### Acceptance criteria

- `LeafAddr.Wants() VKind` is the only exported name added, in the whole change, across every module.
- No exported doc comment cites an ADR, an issue or a pull request, asserted by `make godoc-check`.
- There is no constructor for a typed address that does not take the kind, and no exported constructor at all.
- `ferry.Load` of a `string` field over a variable holding a declared boolean word returns the text, and of a `bool` field over the same variable returns the boolean, from one environment in one test.
- A `map[string]bool` and a `map[string]string` over one variable both load, which is the minted-address case.
- The dump direction is unchanged, and no gate exists on the write path.
- `make check`, `make lint` and `make godoc-check` green in every module, with no `//nolint` and no raised limit.
  Note for the implementer: candidate A's builder failed `cognitive-complexity` at 11 in the prototype and candidate C has no builder at all, so the shipped shape has room, and `spot.container` is the one function to watch.
- `make test` green in every module, and the `ferrytest` conformance run green for every first-party driver.
- New lines at 100% in the codecov missing-lines table.
- Bench parity against `main`: one `Load` through a `Binding`, same allocation count and same bytes per op, and no regression outside run-to-run noise on the runner.

---

Evidence: `proto/309-kind-gate` commits `12c045e`, `8e7293a`, `d2029c4`, cited against `origin/main` at `17e2839`.
The exhibit runs as tests on that branch, and the four candidate surfaces run side by side against it.
Part of the #309 campaign.
