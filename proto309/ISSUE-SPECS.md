# Issue specs for #309: the root leaf

Written from the prototype on `proto/309-root-leaf`, whose three commits are the evidence for every behavioural claim below.

| commit | what it establishes |
| --- | --- |
| `e3055d4` | the root leaf legalised at the empty path, and the full root-type x plane x direction matrix, recorded in `proto309/OBSERVED-empty-path.txt` and `OBSERVED-sentinel-tilde.txt` |
| `0731cde` | both candidate shapes for the root declaration, including the driver-side one that was rejected, recorded in `proto309/OBSERVED-declaration.txt` |
| `f78b5ee` | the ratified trim: `var RootRequired`, seed-as-default, the driver-side shape deleted, lint and `godoc-check` green |

Every behaviour stated here is a line in one of those `OBSERVED-*.txt` files, produced by a program that ran.
Nothing below is predicted.

Base for all file and line citations is `origin/main` at `17e2839`.

## Two structural facts that shape the whole campaign

**Only one unit is blocked on the legalisation.**
A driver receives an `*AddressSet` only from a compiled schema, because `addrsOf[T]` in `driver/kv/fake_test.go:401` goes through `ferry.Bind[T]` and core exports no `AddressSet` constructor.
So a test that needs the empty address to reach a driver cannot be written until Issue 3 lands.
The `*int` pointer-root panic is in that position and rides inside Issue 3.
The kv guard is not, because `driver/kv/key_test.go` already tests `keyFunc` directly, in an idiom whose own doc comment licenses exactly this case.

**Only one of the three ferrytest wrappers can be removed by Issue 3.**
`wrapperOf` in `ferrytest/codecwalk.go:61` runs against ferrytest's own in-memory planes, which can name the root, so it dissolves.
`holder[T]` in `ferrytest/roundtrip.go` and `keyed[T]` in `ferrytest/injective.go:121` run against the caller's plane, and no real driver can name the root until Issue 5.
They stay, and Issue 3 must say why in a comment so the next reader does not remove them.

---

## Issue 1: a key-value store has no key for the root, and today the value lands on the folder

**Type**: `fix(kv)` with a `feat(kv)` half.
File as one issue: the guard and the option are one decision, and a guard with no way to lift it would ship a dead end.

### Depends on

Nothing.
This is the one unit that ships standalone, and it should ship **first**, before the address that reaches the defect exists.

### Exact changes

The defect is in `driver/kv/key.go:59`, `keyFunc`.
It loops over `addr.Segments()` and validates each segment through `nameable`.
A zero-segment address enters the loop zero times, so every check is skipped and the key is `strings.Join(prefix, separator)`.
There is no empty-address guard on either the source or the sink side.

That value is exactly what `rootKey(prefix)` returns at `driver/kv/key.go:105`, which is the key `Sink.opener` hands to `ACL.CanWrite` as its whole-store probe, and the key `folder()` at line 112 treats as the parent of everything.
Measured at `e3055d4`, in `proto309/OBSERVED-empty-path.txt`:

```
kv   Dump                    ok   keys={""="8080"}
kv   Dump(prefix=app)        ok   keys={"app/other"="kept", "app"="8080"}
```

With no prefix that is `Put(ctx, "", value)`, which an in-memory client accepts and real Consul rejects.
With a prefix the root value lands on the folder key, so the store holds a value on an interior node, silently.

ADR-0010's published note says a kv sink "writes zero keys with a nil error".
That measurement is stale and is corrected by Issue 3, which should quote this issue's numbers.

`driver/kv/key.go`:

- `keyFunc` gains a root branch before the segment loop, guarded by `addr.IsRoot()` once Issue 3 publishes it, or by `addr == ferry.Path{}` if this ships first.
  `Path` is comparable, so the second spelling works today and becomes a one-line follow-up.
- Where the config names no root key, refuse.
  Where it names one, the key is the prefix's segments plus that name, so it is an ordinary key **under** the prefix and never the prefix itself.
- New `nameableRoot(name string) error` beside `nameable` at line 87, applying the same two rules that function applies: the name may not be empty and may not contain `separator`.

`driver/kv/option.go`:

- `config` gains `rootKey string`.
- `func RootKey(name string) Option`, in the one-line-constructor idiom the other options use.
  Its godoc names the option and the refusal and cites no ADR and no issue.

Refusal wording, drafted in this driver's existing voice:

```
kv: this schema's root is a single value, and a key-value store has no key for
one: the store's own key at this prefix is the folder every other address is
written under, so writing the value there would put a value on an interior
node - name the key with kv.RootKey
```

Diff shape: about 20 lines in `key.go`, 15 in `option.go`, plus godoc.
`driver/env`'s `RootVar` at `f78b5ee` is the template for the option.

### Tests

`driver/kv/key_test.go` is the seam that makes this unit independent.
It is `package kv`, it calls `keyFunc` directly, and its doc comment already states the justification this case needs:

> It is asserted here rather than through a load or a save because a store key is what every stored artefact of this plane is named by, and because the two refusals are the driver's half of ADR-0003's legality obligation: core refuses an empty minted name at the mapping before this driver is asked (#258), so the empty-part row has no other way in and would otherwise be a guard nothing proves.

Its table has six rows and no `ferry.Path{}` row.
Add the seventh and eighth:

- `"the root, unnamed"`: `{addr: ferry.Path{}, refuse: "no key for one"}`.
- `"the root, named"`: a case shape carrying `rootKey`, asserting `ferry.Path{}` under prefix `app` and `RootKey("value")` renders `app/value`.
  This needs the case struct to grow one field, which is the only structural change to that test.
- `"a root key with a /"` and `"an empty root key"`: both refuse, through `nameableRoot`.

Then, through the published seam, once Issue 3 has landed and a schema can mint the address:

- `TestARootLeafIsRefusedWhereNoRootKeyNamesIt`: `ferry.Bind[int]` and `ferry.BindSink[int]` both fail, `errors.Is` against `ferry.ErrPlane`, the message names `kv.RootKey`, and the fake client received nothing.
- `TestARootLeafIsWrittenAtTheKeyRootKeyNames`: `ferry.Dump(ctx, 8080, sink)` with `RootKey("value")` and `WithPrefix("app")` writes exactly `{"app/value": "8080"}`, and a sibling `app/other` seeded beforehand survives.
- `TestARootLeafRoundTripsThroughTheKeyRootKeyNames`: dump then `ferry.Load[int]`.
- A root-leaf row in `driver/kv/conformance_test.go` once Issue 3 ships case 23.

Fresh store per subtest.

### ADR amendments folded in

None owned here.
The stale ADR-0010 measurement is Issue 3's to correct.

### Acceptance criteria

- No schema can cause `kv` to write at `rootKey(prefix)`.
- The refusal is at Bind, wraps `ferry.ErrPlane`, and names `kv.RootKey`.
- `make check`, `make lint`, `make godoc-check` green in `driver/kv`.
- New lines at 100% in the codecov missing-lines table.

---

## Issue 2: a root pointer to a leaf panics inside reflect, and only the refusal is hiding it

**Type**: `fix`.
**This is not a standalone issue.**
File it for tracking if the granularity is wanted, but it ships as leading commits inside Issue 3's PR.

### Depends on

Issue 3, for its tests.
The panic is unreachable while the root-leaf refusal stands, and there is no direct-call seam in core equivalent to kv's `key_test.go`.

### Exact changes

`entry.go`'s `loadRoot` and `dumpRoot` both dereference a pointer root unconditionally.
That is right for `*Config`, whose element is the struct the walk descends into.
It is wrong for `*int`, which `compilePointer` resolves through `registry.pointerLeaf` to a leaf carrying a null rather than a pointer to one.
The leaf's codec is built for `*int` and is handed an `int`.

Measured at `e3055d4`, the first run after the refusal was lifted:

```
yaml Dump   PANIC reflect: call of reflect.Value.IsNil on int Value
yaml Load   PANIC reflect.Set: value of type *int is not assignable to type int
kv   Dump   PANIC reflect: call of reflect.Value.IsNil on int Value
```

The stack runs `types.go:152` (`Registry.pointerLeaf`'s encode half) through `walk.go:1152` `dumpTo.atLeaf`.
The panic is in the shared walk rather than in the compiler, and it escapes the fence.

`dumpRoot`'s nil-pointer refusal has the same confusion in its scope and its message.
A nil `*Config` genuinely has nowhere to put the Null a nil composite writes and must stay refused.
A nil `*int` is a leaf whose codec owns what nil means, and the root address is an address, so it must not be.

`entry.go`:

- `loadRoot(dst reflect.Value)` becomes `loadRoot(n *node, dst reflect.Value)`, returning `dst` untouched where `n.kind == nodeLeaf`.
- `dumpRoot(v reflect.Value)` becomes `dumpRoot(n *node, v reflect.Value)`, returning `(v, nil)` where `n.kind == nodeLeaf`.
- Both call sites pass `b.sch.root`.
- The nil-root message narrows from "the root is a nil pointer, and the root has no address of its own for a null to sit at" to "the root is a nil pointer to a struct, and a struct has no address of its own for a null to sit at".

The shape is verified working at `f78b5ee`.
That branch threads it through a `rootValue` helper, which is one indirection more than needed once the sentinel is gone; prefer the direct `n *node` parameter.

### Tests

Through the published seam, fresh destination per subtest:

- `TestARootPointerToALeafCarriesItsNull`: `ferry.Dump[*int](ctx, nil, sink)` writes `Null` at the root address and returns nil; `ferry.Load[*int]` from a plane holding `Null` gives nil; from a plane holding `Number("8080")` gives a non-nil `*int` of 8080.
- `TestANilRootPointerToAStructIsStillRefused`: `ferry.Dump[*Config](ctx, nil, sink)` still fails, `errors.Is` against `ferry.ErrValue`, and the sink was written nothing.
  This is what stops the narrowing going too far.

The panic itself needs no test of its own: if the fix regresses, both of the above panic.

### ADR amendments folded in

None.
ADR-0005's "a pointer to a leaf is a leaf with a null" is already correct and is what this fix honours.

### Acceptance criteria

- `Load[*int]` and `Dump[*int]`, and the same for any pointer-to-leaf root, complete without panicking in both directions against a plane that names the root.
- The nil-root-struct refusal is unchanged in behaviour and only reworded.
- 100% on new lines.

---

## Issue 3: the root address is an address, and a single value at the root is a schema

**Type**: `feat!`.
Breaking: types that were refused now compile.

### Depends on

Issue 1 should land first, so the kv folder-key write is closed before the address that reaches it exists.
Contains Issue 2 as leading commits.
Blocks Issues 4, 5a, 5b and 5c.

### Exact changes

`schema.go`:

- `compileRoot` calls a new `compileRootLeaf(t)` instead of `c.errAt(Path{}, rootLeafMsg(t))`.
- `compileRootLeaf` builds `&node{kind: nodeLeaf, addr: Path{}, codec: cd}` and calls `recordLeaf(site{owned: true})`.
  It carries no `def`, no `hasDef` and no `required`: the root has no tag to write them on, and `required` arrives in Issue 4.
- New `rootLeafCodec(t)` asking `pointerLeaf` for a pointer and `leafFor` otherwise, which is `rootIsLeaf`'s own split reused so the two cannot drift.
- `rootLeafMsg` is deleted.
- The not-a-struct refusal is untouched and now carries the whole load for root maps, slices and arrays, which stay refused for the reason ADR-0010 already gives: an empty one writes its Null at its own address, that address is the root, and the value is lost with a nil error.
- `compileRoot`'s doc comment is a small essay about why a root leaf is refused, and it contains two measurements that are both wrong on today's `main`.
  Rewrite it rather than editing around it.

`path.go`:

- Publish `func (p Path) IsRoot() bool { return p.rendered == "" }`.
  This is a real gap: ADR-0010's measurement note quotes `IsRoot=true` and no such method exists.
  A driver's key function needs it to decide whether to consult its root option or refuse.
- `At`'s doc changes from "the empty path, which is not an address" to the root address.
- The `Path` type doc's closing line, "The zero Path has no segments. An address has at least one.", must change.

`entry.go` and `encode.go`:

- The prototype threads `at: sch.root.addr` into the starting `spot`.
  **Leave it out.**
  The root address is always the empty path under the ratified design, so the change is a no-op and would ship untestable code.
  It is noted only because a reviewer diffing against `e3055d4` will ask.

`ferrytest/codecwalk.go`:

- Delete `wrapperOf` (line 61), `wrapperName` and `wrapperTag` (lines 46 and 47), and `wrapperPath` (line 52).
- `dumpZero` at line 76 becomes `c.dumpRoot(reflect.New(t).Elem())`.
- `dumpRoot` at line 87 reads `rec.seen[ferry.Path{}]`.
- `loadInto` at lines 93 and 94 builds `reflect.New(t).Elem()` and seeds `Static(map[ferry.Path]ferry.Value{{}: v})`.
- The paragraph at lines 57 to 60 explaining why the tag key is spelled literally goes with them: a bare leaf reads no tag.

Verified working: `proto309/ferrytest_probe_test.go` at `0731cde` loads `8080` through `ferrytest.Static` at the empty path.

`ferrytest/roundtrip.go` and `ferrytest/injective.go`:

- `holder[T]` (`roundtrip.go:241`, used at `roundtrip.go:50`, `179`, `219` and `driver.go:215`) **stays**.
- `keyed[T]` (`injective.go:121`) **stays**.
- Both run against the caller's plane, and no real driver can name the root until Issue 5.
  Add one line to each doc comment saying that, so the next reader does not remove them for the reason this issue removes `wrapperOf`.

`ferrytest/plane.go:277`:

- `Golden`'s godoc says "A bare leaf is refused there for naming no address, so it is refused here too."
  That is a published API constraint and it is no longer true.
  It becomes a statement about what the plane can name rather than about what ferry refuses.

`ferrytest`, the new conformance case.
The suite is **twenty-two** cases and a root-leaf case is the twenty-third.
Five edit sites:

1. New file `ferrytest/rootleaf.go` with `func (d *driverRun) caseRootLeaf()`, modelled on `ferrytest/naming.go`, which is case 22 and the most recent case added.
2. `ferrytest/driver.go:116`, append `d.caseRootLeaf()` after `d.caseNamed()`.
3. `ferrytest/driverkit.go:37`, add `caseRootLeafNo = 23` after `caseNamedNo = 22`.
4. Prose counts at `ferrytest/driver.go:13`, `driver.go:90`, `driver.go:1179` and `ferrytest/doc.go:20`.
5. The ADR-0014 amendment, below.

The case must **skip** rather than fail where the plane has no name for the root, in the idiom `d.skip(caseNamedNo, ...)` uses at `driver.go:1187`.
"This plane has no name for the root" is a legitimate answer, and it is the answer env, http and kv give until Issue 5.
Its negative tests go in `ferrytest/rootleaffail_test.go`, following `ferrytest/namingfail_test.go`.

### Tests

Six existing test functions assert the refusal.
They are the specification of the old behaviour and become the specification of the new.

- `composite_test.go:775` `TestTheRootMustBeAStructFerryWalks` splits.
  The array, slice and map rows keep their message and stay.
  The `int` and `*int` rows move to a new `TestARootLeafCompilesToOneAddress`.
  The `through Load` and `and through Dump` subtests invert: a root `int` now binds a driver, and the plane sees exactly one address.
- `composite_test.go:1145` `TestARootTypeACodecClaimsIsRefusedAsALeaf` becomes `TestARootTypeACodecClaimsCompilesAsALeaf`.
  `rootEndpoint` with a text pair, `rootEndpoint` with a registration, and `netip.Addr` all compile to one leaf at the root.
  It keeps its #306 purpose, which was the ordering, and that ordering is unchanged.
- `composite_test.go:1173` and `1190`, `TestACodecdRootIsRefusedThroughLoad` and `ThroughDump`, invert.
  The `treeSink` fixture at `composite_test.go:829` was written to prove the old silent `{}` write; it becomes the fixture proving a tree-shaped sink is now asked about the root and can refuse it.
- `composite_test.go:1239` `TestTwoRegistriesDisagreeingAboutTheRootCompileTwoSchemas` keeps its point, which is that the registry is in the cache key, and changes its assertion: with the registry the type is one root leaf, without it a section at `/v`, and both compile.
- `schema_test.go:589` `TestCompileRoot` splits the same way.

New, through the seam:

- `TestARootLeafIsOneAddressAndItIsTheRoot`: `ferry.Bind[int]` hands the driver an `AddressSet` of exactly one member whose `Path().IsRoot()` is true.
- `TestPathIsRootIsTrueOnlyForTheEmptyPath`: `Path` is an explicit exception to the one-seam rule, so this is a direct unit test.
- `TestARootLeafRoundTripsThroughAMemoryPlane`: `Dump` then `Load` for `int`, `string`, `[]byte`, `json.RawMessage`, `netip.Addr` and `*int`, fresh destination each.
  Every one of those is measured at `e3055d4`.
- Issue 2's two tests.

### ADR amendments folded in

One commit, amending in place, each with the convention's note saying what it read as published, what moved and why.

**ADR-0003**, the substantive one.
"A leaf addresses a plane by an ordered, non-empty sequence of segments" becomes a sequence that may be empty, with the empty one named the root address.
Three consequences must be stated:

- the root address has no parent;
- it is never enumerated, because a plane is asked for the value at it and never for its children;
- prefix-freeness is vacuous over a schema whose only member is the root.

And one new obligation on drivers:

> A plane names the root address by its own rule or refuses it, and refusing it at Bind is the expected shape.

**ADR-0010**, narrowing.
The "root must be a struct ferry walks" section keeps its ordering ruling verbatim, which is correct and is what #306 fixed.
What changes is the consequence: a root that resolves to a leaf compiles, and the refusal narrows to maps, slices and arrays for the reasons that section already gives.
Both of its measurements are stale and must be replaced with this branch's numbers:

- YAML does not write `{}` with a nil error.
  On today's `main` it refuses loudly, at `driver/yaml/tree.go` `place`.
- kv does not write zero keys.
  It writes one, on the folder key, per Issue 1.

The `IsRoot=true` in its measurement block is now real rather than aspirational.

**ADR-0004**, one line.
A driver may refuse an address it has no name for, and the empty address is the case that has one; Bind is where it belongs.

**ADR-0014**, the conformance count.
The published count is twenty-two, reached by #159's amendment at line 701, which is the template to follow.
The new case takes it to twenty-three.
The counts appear at lines 644, 667, 701, 717, 894 and 900, and the ADR's convention is to amend the count in place at each rather than restate a total.

### Acceptance criteria

- `Compile[int]`, `Compile[*int]`, `Compile[netip.Addr]`, `Compile[[]byte]` and `Compile[json.RawMessage]` all succeed and yield exactly one address.
- `Compile[map[string]int]`, `Compile[[]string]` and `Compile[[2]string]` still refuse, with their existing messages.
- `ferrytest` has no `wrapperOf`; `holder[T]` and `keyed[T]` are still present and each carries a comment saying why.
- All four driver modules still green, refusing the root, which is correct until Issue 5.
- `make check`, `make lint`, `make godoc-check` green everywhere.
- No exported doc comment gained an ADR number or an issue number.

---

## Issue 4: the root has no tag, so required is an Option and the default is the seed

**Type**: `feat(core)`.

### Depends on

Issue 3.
Independent of Issues 1, 2 and 5.

### Exact changes

`option.go`:

```go
// RootRequired declares the root address required.
//
//	port, err := ferry.Load[int](ctx, src, ferry.RootRequired)
var RootRequired rootRequired

type rootRequired struct{}

func (rootRequired) apply(c *config) error {
	if c.root.requiredSet {
		return optionError("ferry.RootRequired was supplied twice in one call")
	}

	c.root.required, c.root.requiredSet = true, true

	return nil
}
```

- `config` gains `root rootDecl`, where `type rootDecl struct{ required, requiredSet bool }`.
- A value rather than a constructor, and specifically not an `Option`-typed value.
  `rootRequired` is a `struct{}` with exactly one inhabitant, so the only value assignable to the exported var is the value it already holds.
  An `Option`-typed var would be a mutable process-wide global that any `init` in the binary could repoint.
  Verified at `f78b5ee` as a compile error:

```
cannot use ferry.TagKey("x") (value of interface type ferry.Option)
    as ferry.rootRequired value in assignment: need type assertion
```

- One wart to accept or reject deliberately: `go doc` renders it `var RootRequired rootRequired`, naming a type the caller cannot write.
  It costs the caller nothing, because nobody ever needs to name the type.
  If clean godoc is preferred, `func RootRequired() Option` is the fallback and nothing else in this issue changes.

`cache.go`:

- `schemaKey` gains `root rootDecl`.
  It is compile-affecting in exactly the sense that type documents, and it is comparable, so the `map[schemaKey]struct{}` guard at `cache.go:63` keeps holding.
  Verified at `f78b5ee`: two loads of `int` under different root Options key two schemas.

`schema.go`:

- `compileRootLeaf` sets `required: c.cfg.root.required`.
- `compileRoot` applies `required` to a struct root too, through a small `declareOnStructRoot` helper.
  This works today and is genuinely useful: it means the plane supplied at least one of the root's children, which `walk.go`'s `atStatic` already answers, and the tag grammar cannot express it.
  Measured: `ferry: required, and the plane supplied nothing under it`.
- `schemaWith` passes `root: cfg.root` into the key.

**No `RootDefault`.**
Dropped deliberately.
`LoadOver(ctx, 4242, src)` against a silent plane returns `4242`, measured at `f78b5ee`.
The seed is better typed than a text decoded through a codec on every load, and it puts the default at the call site.

### Godoc clarifications, exact text

Shipped at `f78b5ee`.

`Load`, appended to the absence paragraph:

> Every field the plane is silent about keeps T's zero value, and a field declaring default= takes its default there instead.
> A root that is a leaf has no tag and so declares no default; [LoadOver] is where it gets one, and the seed is it.

`LoadOver`, replacing the "two uses" opening:

> It has three uses.
> A seed is how a composite default is spelled, since a struct tag holds one text and a composite's value lives at many addresses; it is also the only default the root has, because a declared default is written on a tag and the root has no tag; and a reload is the caller writing the carry-over out loud rather than getting it from a destination that happens to be populated:
>
> ```go
> cfg, err := ferry.LoadOver(ctx, Config{Tags: []string{"default"}}, src)
> port, err := ferry.LoadOver(ctx, 8080, src)
> cfg, err = ferry.LoadOver(ctx, cfg, src)
> ```

### Tests

Through the seam, fresh destination per subtest.
Every row is an observed result from `proto309/OBSERVED-declaration.txt` at `f78b5ee`.

| test | assertion |
| --- | --- |
| `TestARootLeafIsNotRequiredByDefault` | `Load[int]`, plane silent, gives `0` and a nil error |
| `TestRootRequiredRefusesASilentPlane` | `ferry: required, and the plane holds nothing at this address`, `errors.Is` true for `ErrMissing` and false for `ErrPlane` |
| `TestRootRequiredIsSatisfiedByAnyObservation` | plane holds the value, no error |
| `TestRootRequiredTwiceIsRefused` | `ErrSchema`, and the message names the Option |
| `TestASeedIsTheRootsDefault` | `LoadOver(4242)`, plane silent, gives `4242` |
| `TestRootRequiredFiresEvenWithASeed` | `LoadOver(4242, ..., ferry.RootRequired)` still `ErrMissing` |
| `TestRootRequiredOnAStructRoot` | `ferry: required, and the plane supplied nothing under it`, `ErrMissing` |
| `TestTwoRootDeclarationsKeyTwoSchemas` | same type, one load with and one without; one errors and one does not, and a second load against the first config still succeeds |
| `TestRootRequiredIsLoadOnly` | `Dump(0, sink, ferry.RootRequired)` writes the zero value and returns nil |

Plus a runnable example in `example_test.go` with an `// Output:` line, showing `LoadOver(ctx, 8080, src)` as the root's default.

`TestRootRequiredFiresEvenWithASeed` must be pinned with a comment, because the composition looks contradictory and is not.
`required` is a presence test about the plane, satisfied by any observation other than Absent, and a seed is not an observation.

The immutability of the var is a compile-time property, so it gets a fixture rather than a test: a file that attempts `ferry.RootRequired = ferry.TagKey("x")` and must fail to build, in the idiom `internal/testdata/schemakey/unhashable` already uses.

### ADR amendments folded in

**ADR-0006**, drafted in full:

> **Amended for the root address ([#309]).**
>
> A root that resolves to a leaf is legal now, and it is the one address with no struct tag on it.
> Everything this ADR decides about absence holds there unchanged: absence does not write, and a seed survives it.
>
> What does not exist there is a **declared** default.
> A declared default is written on a tag, this ADR's whole treatment of it assumes a tag, and the root has none.
> The decision is not to invent a second way to declare one.
>
> > A seed is the caller's default.
> > A declared default exists only where a tag can spell one, and the root has no tag.
>
> So `ferry.LoadOver(ctx, 8080, src)` is the root's default, and it is better typed than a declaration would have been: the seed is a `T` rather than a text this ADR's own chain would have to decode on every load.
> The cost is stated rather than hidden: `Compile` validates a tagged field's default text from the type alone and has nothing to validate at the root, because there is no text there to be wrong.
>
> `required` at the root **is** declared, by `ferry.RootRequired`, because requiredness is a fact about the schema and not about the caller's starting value.
> It composes with a seed and is not weakened by one: `required` is a presence test about the plane, satisfied by any observation other than Absent, and a seed is not an observation.
> `LoadOver(ctx, 8080, src, ferry.RootRequired)` therefore carries 8080 forward on a reload and still fails with `ErrMissing` where the plane went silent, which is the shape a reload wants and no declared default could give it.

**ADR-0008**, one line: the grammar has exactly one address it cannot reach, the root, and the declaration that would have gone on its tag lives in the Option list instead.

**ADR-0010**, one line: the closed Option set is four rather than three, and the new member is compile-affecting and in the cache key.

### Acceptance criteria

- Every row of the matrix passes.
- `ferry.RootRequired` cannot be reassigned to any other Option, proven by a fixture that must fail to build.
- No `RootDefault` anywhere.
- `make godoc-check` green: neither the var's doc nor the two entry-point paragraphs cite an ADR or an issue.
- 100% on new lines.

---

## Issue 5: the root spellings, one issue per driver

Four planes, three issues, because kv's belongs to Issue 1.

They are filed separately rather than as one because each driver is its own Go module with its own `go.mod`, its own CI job and its own `ferrytest.Driver` run, and the answers are not variations on one mechanism: env and http need a name from an Option, kv needs a key and a guard, and yaml needs no name at all because a scalar document already is the root.
A single PR across four modules cannot be reviewed as one thing.

### Issue 5a: a root leaf reads the variable env.RootVar names

**Type**: `feat(env)`.
**Depends on** Issue 3.

`driver/env/option.go`: `config` gains `rootVar string`, and `func RootVar(name string) Option`.

`driver/env/key.go`: `config.key` substitutes `c.rootVar` for the empty join before the existing empty-name refusal, so an unnamed root still gets `env: address has no environment variable name: the empty address names nothing`.

Shipped and measured at `e3055d4` and `f78b5ee`.
`RootVar("APP_PORT")` reads `APP_PORT=8080`; without it, the refusal lands at Bind.

**Two incidental fixes belong in this PR**, both caused by the change.
`config` crosses gocritic's 80-byte `hugeParam` threshold once it grows `rootVar`, so its four value receivers, `key`, `join`, `validate` and `observe`, become pointer receivers, and one test call site at `driver/env/key_test.go:88` needs a local variable.
Confirmed green at `f78b5ee`.

**Do not add a `RootRequired` or `RootDefault` to this option.**
That shape was prototyped at `0731cde` and rejected on evidence: it silently accepted the contradictory pair and returned a value, it produced `ErrMissing` and `ErrPlane` on one failure, and it made the same schema required through one source and not through another.
`env.RootVar` names the variable and says nothing about requiredness.

Sharp edge for the godoc: the fold at `foldByte` maps every non-alphanumeric byte to `_`, so a root leaf cannot be reached by any name the fold would produce from a segment.
Naming it explicitly is the only route.
This is also why the sentinel design died: `~` folds to `_`, and `ferry.Load[string]` returned `"/home/ap/.nix-profile/bin/go"` out of the shell's `$_`.

Tests: `Load[int]` with the variable set, unset, and with no `RootVar` at all; the refusal is at Bind and answers `errors.Is` against `ferry.ErrPlane` and `env.ErrIllegalName`; the case 23 conformance run.

### Issue 5b: a root leaf reads the parameter http.RootParam names

**Type**: `feat(http)`.
**Depends on** Issue 3.

Symmetric to 5a, doubled because this driver has two planes.
`RootParam(name)` for the query plane and `RootField(name)` for the header plane.
Prefer two names over one `RootName`: `NewQuerySource` and `NewHeaderSource` are already two constructors, and a caller who mixes them up should get a compile error rather than a header field called `q`.

`driver/http/key.go`'s `join` gains the root branch.
**The existing refusal reads `illegalName("there is nothing here to name")` at `driver/http/key.go:90`**, raised by the `if first` guard after the segment loop.
Do not grep for "the empty address names nothing"; that was prototype-era wording and does not exist on `main`.

`ErrIllegalName`'s godoc at `key.go:13` already names the two degenerate cases, including "the empty address itself", and must change.

Note the header plane wraps the query plane's `join` through `headerKey` at `key.go:107` and then canonicalises through `textproto.CanonicalMIMEHeaderKey` and validates against the token grammar, so a root field name is held to that grammar too.

This is the plane where a root leaf is most natural: `?q=...` bound to a `string` is an ordinary handler.

Tests: `Load[string]` from `url.Values{"q": {"x"}}` with `RootParam("q")`; the refusal without it; a header round trip including canonicalisation; a root field name that fails the token grammar; the case 23 conformance run.

### Issue 5c: the root of a document is the root of the schema

**Type**: `feat(yaml)`.
**Depends on** Issue 3.

No option at all.
`driver/yaml/tree.go` `place` currently refuses `len(segs) == 0` with `the empty path is not an address`.
It gains a root branch returning the document's own content node, minted where the document is empty:

```go
// docRoot is the document's own content node, minted where the document is
// empty. It is shaped by nothing, because the root leaf's own write decides what
// it becomes.
func docRoot(doc *yamlv3.Node) *yamlv3.Node {
	if len(doc.Content) == 0 {
		doc.Content = []*yamlv3.Node{{}}
	}

	return doc.Content[0]
}
```

The contrast with `rootFor` is the whole point: `rootFor` calls `shape(n, k)` to force the top level into a mapping or a sequence, and `docRoot` deliberately does not, which is what makes a scalar document possible.

Measured at `e3055d4`: `Dump(ctx, 8080, sink)` writes `8080\n`, and `Load[int]` of `8080\n` reads it.
Every leaf type round-trips, including `!!binary` for `[]byte`.

**Split the function rather than carrying the branch inline.**
`place` reaches cognitive complexity 8 against the limit of 7 with the branch inline.
Extracting `docRoot` is what brings it back under.
Confirmed green at `f78b5ee`.

**Rebase warning.**
`main` has moved about 2100 lines across 40 files since the prototype, and `driver/yaml/tree.go` alone is +253.
The prototype's `place` patch will not apply cleanly and must be rewritten against the current function.
`place` has exactly one caller, `writer.put` at `driver/yaml/sink.go:471`, which is reached only from `Set` and `Ensure`, so the whole write path funnels through one place and the blast radius is small once the patch is redone.

**The sharp edge, which must be in the exported godoc on `Sink`.**
Dumping a root leaf onto a file that already holds a mapping replaces the whole document.
Measured: a file holding `keep: me\nother: 2\n` becomes `8080\n`.
Say it in the caller's language, with no ADR number.

**And say plainly that this is consistent rather than exceptional.**
This driver has no replace-versus-patch option.
The `Option` set is closed at one, `Durable`, and dump-is-replace is unconditional, implemented by `writer.Unset` recording the composite and `writer.replace` and `writer.prune` running at commit.
`driver/yaml/replace_test.go` is the reproduction suite for #220 rather than a test of a mode.
A root leaf replacing the document is the same rule applied at the one address that has no parent to be pruned from.

Tests: dump and load a scalar document for each leaf kind; the replacement case with an existing mapping asserted byte for byte; the reverse, that a struct root still patches rather than replaces; the case 23 conformance run.

### Acceptance criteria, all three

- `ferry.Load[T]` and `ferry.Dump` for a root leaf work end to end on the plane, or refuse at Bind naming the plane's own option.
- The case 23 conformance run passes for the plane, or skips with a reason where the plane names no root.
- No exported doc comment cites an ADR or an issue.
- `make check`, `make lint`, `make godoc-check` green in the module.
- 100% on new lines.

---

## Suggested order

1. **Issue 1**, on `main`, standalone.
   Closes the folder-key write before anything can reach it.
2. **Issue 3**, containing Issue 2.
   The legalisation, `Path.IsRoot`, the ferrytest work and the three ADR amendments.
3. **Issue 5c**, then **5a**, then **5b**, in any order.
   5c first because it is the cheapest and the most persuasive: it is the one plane where the feature reads as obvious rather than as configuration.
4. **Issue 4**, any time after Issue 3.

Issue 4 is deliberately last rather than blocking: `RootRequired` is the weaker half of what was prototyped, and a root leaf is useful without it.
