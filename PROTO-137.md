# PROTO-137: how far `ferrytest.Codec` reaches into the registry it is handed

Branch `proto/137-codec-reach`, based on `feat/83-suites`.
Every block marked **output** is what `go run ./proto137 <n>` printed, from `proto137/` on this branch, running against the real engine through `ferry.Dump`, `ferry.Load`, `ferrytest.Codec`, `ferrytest.RoundTrip`, `ferrytest.Complete` and `ferrytest.Injective`.
`go build ./...`, `go vet ./...` and `go test ./...` are green on this branch with the probe and the section 6 prototype in the tree.

## The framing, checked first

The issue says five of `Codec`'s six cases cannot reach the registrant's codecs, and that case 1 is the exception that reads the caller's own types.

**Right on the reach, and understated in one direction and overstated in another.**

Understated: it is not five of six, it is **six of six**.
Case 1 reads the caller's own *types*, correctly, but it never invokes the caller's *codec* either - it calls the type's `AppendText` and `MarshalText`, which for a `StringCodec` or `ValueCodec` registration are methods ferry will never call for that type.
The registrant's codec is invoked **zero times** by the whole suite, measured below.

Overstated: "the cost" section says a registrant with a genuinely wrong codec "is not caught by `Codec`".
True, but the implied residual is much smaller than it reads, because three gates that are not `Codec` do catch most of it, and two of them fire without the registrant writing anything.
Section 5 enumerates all eight defect classes; **one** of the eight survives every gate, and it is the one ADR-0009 already names by hand ("the harness is exactly as good as the values").

And the fix the issue prefers is the right one, but not for the reason given.
A `reflect.Value` root does not make cases 2 to 6 per-registrant.
Measured in section 6: it makes **three** of them per-registrant, from the zero value, and two of those three are already discharged by `Register` itself.
What it does buy that nothing else does is one real defect in case 1.

---

## 1. What `Codec` does today, case by case

`ferrytest/codec.go` and `ferrytest/codeckit.go`.

| # | asserts | reads `reg`? | if not, registers instead |
| --- | --- | --- | --- |
| 1 | `AppendText` and `MarshalText` agree, at the **zero value** | **yes**, `reg.Types()` | - |
| 2 | a nil interface **encodes** through the wrapper without panicking | no | `ifaceCodec()`, a `ValueCodec[probeAddr]` |
| 3 | a `Null` **decodes** back to a nil interface | no | same probe, fresh registry |
| 4 | core refuses a codec that declares `String` and emits `Number` | no | `driftingCodec()`, a `ValueCodec[drifting]` |
| 5 | core donates `String` to the declared kind before the codec is called | no | `numberCodec()`, a `ValueCodec[numeric]` |
| 6 | the key text is the codec's, and two keys folding to one address are refused | no | `foldingCodec().AsMapKey()`, a `StringCodec[folding]` |

Each of cases 2 to 6 calls `c.probeRegistry(...)`, which is literally `ferry.NewRegistry()` plus one `Register`.
The caller's `reg` is not passed to any verb in those five cases, and `c.with(reg)` appends the *probe's* registry to the option list, never the caller's.

Case 1 does read `reg`, but through pure reflection on `reflect.New(t).Interface()`.
It never resolves a codec.

### Measured: the registrant's codec is called zero times, and `reg` is never compiled against

A registry freezes at its first retained schema compile, so "can I still register?" is a direct probe of "was this registry ever handed to a verb?".

```go
var c counts // incremented inside the registrant's own format/parse halves

reg := ferry.NewRegistry()
reg.Register(countingCodec(&c))

ferrytest.Codec(rec, reg)
reg.Register(feetCodec())          // succeeds only if nothing compiled against reg

ferrytest.RoundTrip(trip, ferrytest.MemPlane(), []ferrytest.Proof{proof}, ferry.WithRegistry(reg))
reg.Register(yardsCodec())         // now refused
```

**Output** (`go run ./proto137 1`):

```
-- the registrant's own codec, counted
  after Register           encode=1 decode=1
  after ferrytest.Codec    encode=1 decode=1
  ferrytest.Codec(t, reg): NO REPORTS

-- was reg ever handed to a verb? a registry freezes at its first retained compile
  Register(Feet) after Codec        -> nil (accepted, so no schema had been compiled against reg)
  Register(Yards) after RoundTrip   -> refused: ferry: main.Yards: the registry is frozen, because a
      schema has already been compiled against it - every registration must happen before the first
      Load or Dump, which is what stops a schema being resolved against one set of codecs and walked
      against another

  after ferrytest.RoundTrip encode=3 decode=3
  ferrytest.RoundTrip(t, MemPlane(), proofs, WithRegistry(reg)): NO REPORTS

-- and the same suite handed no registry at all
  ferrytest.Codec(t, nil): NO REPORTS
  ferrytest.Codec(t, a registry of three wrong codecs): NO REPORTS
```

Three readings, all load-bearing:

- the `encode=1 decode=1` after `Register` is `Reg.total()`, not the suite;
- `Codec` adds nothing to either counter, and leaves the registry unfrozen, so it compiled no schema against it;
- `Codec(t, nil)` and `Codec(t, <three deliberately wrong codecs>)` produce byte-identical reports.

The registry parameter is inert for everything except case 1's `Types()` loop.

---

## 2. The decisive demonstration

A codec that is genuinely wrong and survives registration, because the zero value round-trips exactly.

```go
type Meters float64

func lossyMeters() ferry.Reg {
	return ferry.StringCodec(
		func(m Meters) string { return fmt.Sprintf("%.2f", float64(m)) },
		func(s string) (Meters, error) { f, err := strconv.ParseFloat(s, 64); return Meters(f), err },
	)
}
```

`0` formats to `"0.00"` and parses back to `0`, so `Register`'s totality check passes.
Every value needing more than two decimals is silently truncated.

**Output** (`go run ./proto137 2`):

```
-- Register, which runs the codec against the zero value
  Register(lossyMeters()) -> nil (accepted)
  reg.Types()             -> [main.Meters]

-- what the codec actually does, away from the zero value
  Dump(Meters(1.0/3.0))   -> {/value=string("0.33")} err=<nil>
  Load back               -> 0.33 err=<nil>  (in 0.3333333333333333)

-- ferrytest.Codec(t, reg), the suite handed exactly this registry
  Codec: NO REPORTS

-- a Proof through RoundTrip, over the same registry and the same codec
  RoundTrip: 3 report(s)
    - plane memory: Meters: case 0: ferry encoded string("0.00") at /value, want string("0")
    - plane memory: Meters: case 1: ferry encoded string("0.33") at /value, want
      string("0.3333333333333333")
    - plane memory: Meters: case 1: loaded 0.33, want 0.3333333333333333
```

`Codec` says nothing.
`RoundTrip` over the same registry catches it on both columns: the golden column sees the representation, the relation sees the loss.
The gap is real and this is it.

---

## 3. Why it cannot be done today

### What `Types()` gives, and what `reflect.StructOf` does and does not buy

**Output** (`go run ./proto137 3`):

```
-- what (*Registry).Types() hands back
  main.Meters              kind=float64  pkg=main  (a reflect.Type, and nothing else is readable off reg)

-- what reflect.StructOf does buy: an annotated root type, built at run time
  reflect.StructOf -> struct { Value main.Meters "ferry:\"value\"" }
  a value of it    -> struct { Value main.Meters "ferry:\"value\"" }{Value:0.3333333333333333}, addressable=true

-- what it does not buy: every exported walk takes a type parameter
  ferry.Dump(ctx, root.Interface(), sink)   T infers to `any`
    -> ferry: interface {} is not a struct ferry walks, so it names no address: the root of a schema
      is a struct whose fields name the addresses, and wrapping it in one is the whole remedy
  ferry.Compile[any]()
    -> ferry: interface {} is not a struct ferry walks, so it names no address: the root of a schema
      is a struct whose fields name the addresses, and wrapping it in one is the whole remedy

-- and the type parameter itself cannot be a variable: go build on proto137/_wall/wall.go
  # command-line-arguments
  proto137/_wall/wall.go:16:21: t (local variable) is not a type
  proto137/_wall/wall.go:19:21: st (local variable) is not a type
```

So `reflect.StructOf` does buy the whole root: an annotated, addressable, correctly tagged struct type built at run time, with the registered type as its one field.
That is more than the issue gives it credit for.
What it does not buy is a way to *hand that type to a verb*, because:

- `Dump[T]`/`Load[T]` fix `T` at compile time, and `ferry.Dump(ctx, root.Interface(), sink)` infers `T = any`, which ADR-0010 refuses at the root;
- and a `reflect.Type` value cannot be written as a type argument at all, which is the compiler output above.

`Dump[T]` goes through a pointer (`reflect.ValueOf(&v).Elem()`) precisely so that an interface `T` is seen as the interface rather than as its dynamic type, so the `any` route is closed on purpose rather than by accident.

**The wall is one line thick.**
`runDump` and `runLoad` already take a `reflect.Value`; `Dump[T]` and `Load[T]` are the generic shells above them, and there is no type parameter anywhere on the walk.
That is what section 6 costs out.

---

## 4. What case 1 does cover

**Output** (`go run ./proto137 4`):

```
-- a type whose two text spellings disagree, registered through TextCodec
  Register(TextCodec[Disagree](KindString)) -> nil (accepted)
  Codec: 1 report(s)
    - codec case 1: main.Disagree: AppendText wrote "append:0" and MarshalText wrote "marshal:0":
      ferry prefers the appender, so the plane holds the first of these and a reader expecting the
      second is reading somebody else's bytes
  and what ferry actually writes: {/value=string("append:7")} err=<nil>

-- the same type, registered through StringCodec, so ferry never consults the text pair
  Register(StringCodec[Disagree](...))      -> nil (accepted)
  Codec: 1 report(s)
    - codec case 1: main.Disagree: AppendText wrote "append:0" and MarshalText wrote "marshal:0":
      ferry prefers the appender, so the plane holds the first of these and a reader expecting the
      second is reading somebody else's bytes
  and what ferry actually writes: {/value=string("codec:7")} err=<nil>

-- a registered type that declares no text pair at all
  Register(lossyMeters())                   -> nil (accepted)
  Codec: NO REPORTS
```

**The class it catches**, stated precisely: a registered type carrying **both** spellings of the text-pair encode half, where the two disagree **at the zero value**, or where exactly one of the two fails at the zero value.
That is one narrow, real defect, and the case is correct to exist: nothing else in ferry can see it.

**The residual it leaves, also precise.**
It says nothing about any value but the zero.
It says nothing about a type carrying one spelling.
It says nothing about the registrant's codec.

**And it has a false positive.**
The second block above is the same type registered through `StringCodec`, so ferry resolves the registration and never touches the text pair - `types.go`'s `leafFor` puts the registry lookup at step 0, ahead of `textPair`.
Ferry writes `codec:7`.
Case 1 still reports on `AppendText` against `MarshalText`, which is a report about two methods ferry will never call for that type.
`Codec` cannot currently tell the difference, because `Reg` is opaque and `Registry` exposes only `Types()`.
This is the one real bug the ticket turns up, and section 6 shows the reach fix closes it.

---

## 5. How much risk is actually residual

Eight defect classes, every gate a registrant has, run against each.
"silent" means the gate did not fire.

**Output** (`go run ./proto137 5`, edited only for line wrapping):

```
D1  not total over the zero value: netip.Addr through String and ParseAddr
  Register    CAUGHT: ferry: netip.Addr: the codec is not total over the zero value: it encodes to
      string("invalid IP") and decoding that back fails: the plane's value is not a valid netip.Addr

D2  lossy: Meters formatted to two decimals
  Register    passes
  Codec       silent
  Complete    CAUGHT: main.Meters has a registered codec and has no proof
  a real Dump silent, wrote {/value=string("0.33")}
  RoundTrip   CAUGHT (2): plane memory: Meters: case 0: ferry encoded string("0.33") at /value, want
      string("0.3333333333333333")

D3  constant: Meters always writes 0.00
  Register    passes
  Codec       silent
  Complete    CAUGHT: main.Meters has a registered codec and has no proof
  a real Dump silent, wrote {/value=string("0.00")}
  RoundTrip   CAUGHT (2): plane memory: Meters: case 0: ferry encoded string("0.00") at /value, want
      string("2.5")

D4  drifting kind: declares String, emits Number away from the zero value
  Register    passes
  Codec       silent
  a real Dump CAUGHT: ferry: /value: the codec for main.Drift declared string and produced number:
      the declared kind is what a plane is promised on the way out and what String is donated to on
      the way back

D5  consistently wrong kind: digit text declared String and never drifting
  Register    passes
  Codec       silent
  a real Dump silent, wrote {/value=string("42")}
  RoundTrip, golden says String silent
  RoundTrip, golden says Number CAUGHT (1): plane memory: Digits: case 0: ferry encoded string("42")
      at /value, want number("42")

D6  a key codec that is not injective, declared .AsMapKey()
  Register    passes
  Codec       silent
  Injective   CAUGHT: "Ab" and "AB" both address "ab", so one of the two entries is lost with no
      error anywhere
  Injective, over one value only silent
  a real Dump of both keys CAUGHT: ferry: /m: /m/ab is addressed more than once, and one of the two
      writes would be lost: the addresses under a map[main.Folding]string come from the value, and
      this one is an address already
  a real Dump of one key   silent, wrote {/m/ab=string("")}

D7  an interface codec that dereferences the nil interface
  Register    PANICKED: runtime error: invalid memory address or nil pointer dereference

D8  lossy, with a proof whose values happen to be lossless
  Register    passes
  Codec       silent
  Complete    silent
  RoundTrip   silent
  and the value the proof does not carry: Dump(1.0/3.0) -> {/value=string("0.33")} err=<nil>
```

### The table that falls out

| defect | `Register` | `Codec` | core, at any real `Dump` | `Complete` | `RoundTrip` + proof | `Injective` |
| --- | --- | --- | --- | --- | --- | --- |
| D1 not total at zero | **catches** | - | - | - | catches | - |
| D2 lossy | passes | silent | silent | **demands a proof** | **catches**, on a value that shows it | - |
| D3 constant | passes | silent | silent | **demands a proof** | **catches** | - |
| D4 drifting kind | passes | silent | **catches**, every dump | demands a proof | catches | - |
| D5 consistently wrong kind | passes | silent | silent | demands a proof | **only the golden column** | - |
| D6 non-injective key | passes | silent | **catches**, when both keys are present | demands a proof | - | **catches**, over the values supplied |
| D7 nil-hostile interface codec | **catches**, as a panic | - | - | - | - | - |
| D8 lossy, proof values lossless | passes | silent | silent | silent | **silent** | - |

**Three findings the issue does not have.**

**`Register`'s totality check does more than #79's "one of four".**
D7 shows it reaching the nil-interface path of the wrapper for the registrant's own type: the panic is inside the registrant's `a.Network()`, which means the comma-ok `valueOf[T]` handed it a genuine nil `Addr`.
So `Codec` case 2's *value* is already exercised per-registrant at registration.
What `Register` does not give is case 2's *report* - it is a panic with a stack, not a named case.

**Core catches D4 and D6 itself, on every dump, for the registrant's own type.**
`leafCodec.emit` compares the declared kind against the produced kind on every encode, and the walk refuses a duplicated map address as it is minted.
Those are exactly what `Codec` cases 4 and 6 assert - against a probe.
A registrant who dumps a non-zero value anywhere, in any test, gets both.

**`Complete` is the gate that actually closes the residual, and it is not mentioned in the issue.**
`ferrytest.Complete(reg, ...)` reports `main.Meters has a registered codec and has no proof`, which forces the registrant onto the path that catches D2, D3 and D5.

**What falls through everything is D8, and only D8**: a codec that is wrong, has a proof, and whose proof values happen to be lossless.
`Register` passes, `Codec` is silent, `Complete` is satisfied because a proof exists, `RoundTrip` is green on all four cases, and `Dump(1.0/3.0)` still writes `0.33`.
No reach into the registry fixes that, because the defect is in the *value list*, and ADR-0009 states it already:

> a proof carries the type's zero value, its extremes, and the values that historically break it

---

## 6. What the fix costs

I prototyped the **non-generic entry point**, because `runDump` and `runLoad` already take a `reflect.Value`, so it is the cheaper of the two by a wide margin.

`valueroot.go` on this branch, in full minus doc comments:

```go
func DumpValue(ctx context.Context, v reflect.Value, sink Sink, opts ...Option) error {
	if !v.IsValid() {
		return newError(momentWalk, ErrValue, Path{}, "the root is the zero reflect.Value, which names no type")
	}
	return runDump(ctx, v, sink, opts)
}

func LoadValue(ctx context.Context, dst reflect.Value, src Source, opts ...Option) error {
	if !dst.IsValid() { ... }
	if !dst.CanSet() { ... }
	return runLoad(ctx, dst, src, opts)
}
```

**Cost, measured**: 21 non-comment lines, **2 new exported functions**, no new exported type, no change to any existing signature, no change to the walk, the schema, the cache or the registry.
`go test ./...` stays green with it in the tree.

### What it buys, run against the registrant's own registered types

The suite builds the annotated root with `reflect.StructOf` and walks the zero value.

**Output** (`go run ./proto137 6`):

```
-- for each type the registry holds, walked from a reflect.Type alone

  main.Addr
    case 2, nil/zero encode  reached, wrote null
    case 3, decode back      reached, loaded <nil>
    case 5, String donated   reached, loaded <nil>
    case 4, drift            NOT REACHABLE: needs a non-zero value of main.Addr
    case 6, two keys         NOT REACHABLE: needs two distinct values of main.Addr

  main.Drift
    case 2, nil/zero encode  reached, wrote string("")
    case 3, decode back      reached, loaded ""
    case 5, String donated   reached, loaded ""
    case 4, drift            NOT REACHABLE: needs a non-zero value of main.Drift
    case 6, two keys         NOT REACHABLE: needs two distinct values of main.Drift

  main.Meters
    case 2, nil/zero encode  reached, wrote string("0.00")
    case 3, decode back      reached, loaded 0
    case 5, String donated   reached, loaded 0
    case 4, drift            NOT REACHABLE: needs a non-zero value of main.Meters
    case 6, two keys         NOT REACHABLE: needs two distinct values of main.Meters

-- and the one thing it fixes about case 1: whether ferry uses the text pair for this type
  TextCodec[Disagree](KindString)  AppendText says "append:0"   ferry writes reached, wrote string("append:0")
  StringCodec[Disagree](...)       AppendText says "append:0"   ferry writes reached, wrote string("codec:0")
```

(`main.Folding` is in the run too and behaves as `main.Drift` does; trimmed for length.)

**So the reach makes three of five cases per-registrant, and two of the three are redundant.**

- cases 2, 3, 5 become per-registrant, from the zero value, which is the only value core holds;
- cases 2 and 3 are already exercised at that value by `Register.total()` (D7 above), so the gain is a named report instead of a panic;
- case 5's `String` donation is structurally guaranteed by core - `donate` rewrites a `String` to the declared kind before the codec sees it, and `decodeText` accepts `String` at every leaf - so a registrant's codec cannot fail it;
- **cases 4 and 6 stay out of reach**, and would stay out of reach under *any* API change, because they need a non-zero value and two distinct keys respectively, and only a `Proof` carries values.

**The one genuine gain is case 1's false positive.**
The last block resolves it: dumping the zero value through the actual engine shows `append:0` for the `TextCodec` registration and `codec:0` for the `StringCodec` one, so the case can skip a type whose registration bypasses the text pair.

**The accessor on `Registry`** (inference, not prototyped): it costs at least one new exported type carrying the two halves as `func(reflect.Value) (Value, error)` and `func(reflect.Value, Value) error` plus the declared kind, plus one method - so more exported surface than the entry point for strictly less capability, since it reaches the codec but not the walk, and it still supplies no values.
It also contradicts ADR-0009's stated finding that "the harness needs no accessor on a registration".
On the evidence above I would not build it.

---

## 7. Is the name the problem rather than the behaviour?

Mostly yes.

The behaviour of cases 2 to 6 is correct as shipped, and the reach fix would not improve any of them beyond a report format.
What is wrong is that `Codec(t, reg)` reads as "check my registry" and takes a `*ferry.Registry` that five of six cases ignore - so the signature itself makes a promise the body does not keep.

The honest contract is **"the registration machinery works in your build of core"**, which ADR-0009 already spells:

> the wrapper is the one piece of reflection the registration API owns

So the name should be the machinery's, not the codec's.
`ferrytest.Wrapper(t, reg, opts...)` reads correctly, keeps the `reg` argument honest (case 1 uses it), and puts the registrant on notice that this is not a check of their codec.

**Renaming plus documenting is not quite a complete answer**, and the two things it leaves are both small:

1. case 1's false positive for a registration that bypasses the text pair - one real defect, fixed only by the reach;
2. the doc comment currently sells the probe as "forced rather than chosen", which is true of the reach and false of the *properties*: cases 4 and 6 could not be per-registrant even with the reach, and cases 2, 3, 5 are per-registrant already through `Register`.
   The honest sentence is stronger than the current one, not weaker.

What the doc should gain instead of an apology is one line pointing at `Complete`, which is the gate that actually closes the residual and is currently documented nowhere near `Codec`.

---

## The decision in front of the owner

### Option A - rename and re-document, change no behaviour

`Codec` becomes `Wrapper` (or similar), its doc says "this checks the registration machinery in your build, not your codecs", and it gains one sentence pointing at `Complete` and `RoundTrip` as the per-registrant path.

- **Commits ferry to**: nothing new. No core change, no new exported surface, no ADR reversal.
- **ADR text that moves**: ADR-0014's "Three entry points, not five" block (the signature), its `Codec, six cases` list heading, and the `TestCodec` example at line 151.
  ADR-0009 is untouched - `Reg` stays opaque, and the "no accessor" finding is confirmed rather than revised.
- **Leaves**: case 1's false positive, unfixed.

### Option B - Option A, plus `DumpValue`/`LoadValue` in core

- **Commits ferry to**: two exported functions taking a `reflect.Value` root, forever.
  That is a real commitment: it publishes the fact that the walk is non-generic underneath, and it hands any caller a route to walk a runtime type, which is a capability decision rather than a test-harness decision.
- **Buys**: case 1's false positive fixed; cases 2 and 3 reported by name instead of panicking at `Register`; case 5 nominally per-registrant, but structurally uninteresting.
- **ADR text that moves**: ADR-0010's entry-point section gains a non-generic root, plus ADR-0014 as in A.
- **This is #134's ground.** #134 is the interface-root question, and the general form of it is exactly this signature. Shipping it here would pre-decide #134 from the harness side, which is the wrong end.

### Option C - an accessor on `Registry`

Not recommended and not prototyped.
More exported surface than B for less capability, and it reverses ADR-0009's own measured finding for a gain section 6 shows is near zero.

### Recommendation

**Take Option A now, independently of #134, and do not wait.**

The reasoning:

- the reach is not what is wrong. The measurement in section 6 is that a `reflect.Value` root converts three of five cases, two redundantly, and cannot convert the other two under any API. So the capability the issue asks for buys one bug fix, and a rename plus a doc correction is a truer description of the same code.
- the residual risk is one class (D8), and its remedy is the proof's value list, which no API change touches.
- `Complete` already forces a proof to exist for every registered type, and `RoundTrip` already drives the registrant's own codec with the registrant's own values through the real engine. That path is complete today; the ticket's real cost is that `Codec`'s name hides it.
- **and #134 should decide the `reflect.Value` root on its own merits.** It is a capability question about what ferry can walk, not a question about what a test harness can reach. If #134 lands, case 1's false positive falls out for free and can be fixed then, in one line. If it does not land, ferry has not published a signature it took for a test suite's convenience.

The one thing worth doing beyond the rename: **file the case 1 false positive as its own small issue**, blocked on #134, so it is not lost in the rename.

`ferrytest.Codec` is not vacuous, the issue is right that it is misnamed, and it is misnamed in a way that a rename genuinely fixes.
