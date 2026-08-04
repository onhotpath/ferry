# PROTO-137B: the internal seam, and what it buys once `ferrytest` can walk a `reflect.Type`

Branch `proto/137-both`, based on `feat/83-suites`.
Every block marked **output** is what `go run ./proto137b <section>` printed, from `proto137b/` on this branch.
`go build ./...`, `go vet ./...`, `go test ./...` and `make lint` are all green with everything below in the tree, and lint is green with **no `//nolint` anywhere**.

The starting point is PROTO-137's measurements, taken as given and not re-derived.
Where a number here contradicts one there, it is flagged.

---

## The short answer

**The internal seam works.**
Core's exported surface is byte-identical to the base commit, 58 names before and 58 names after, and `ferrytest`'s is byte-identical too, 23 names before and 23 names after.
#134 is left completely undecided.

**#143 is fixed by it**, and fixed in the way the ticket asked for: case 1 now watches what ferry writes rather than guessing which arm a registration used.
The false positive is gone and a genuine `TextCodec` disagreement is still caught.

**Option B's reach is worth less than option A's honesty, and B changes what A should say.**
The measured reach adds exactly one new catchable defect class, and it is a class the previous investigation did not have on its list.
It adds nothing to D1 through D8.
But it does dissolve the argument for the rename: the reason `Codec` was the wrong name was that its signature took a registry five of six cases ignored, and after this the registry is read, compiled against, frozen, and every type in it is walked.

---

## 1. The internal seam

### Shape

Three files, and the middle one knows nothing about ferry.

`internal/valuewalk/valuewalk.go`, in full minus doc comments - **2 lines of code**:

```go
package valuewalk

var Seam any
```

`valuewalk.go` in core - **26 lines of code, 0 exported names**:

```go
type valueWalk struct{}

func init() { valuewalk.Seam = valueWalk{} }

func (valueWalk) DumpValue(ctx context.Context, v reflect.Value, sink Sink, opts []Option) error {
	if !v.IsValid() {
		return newError(momentWalk, ErrValue, Path{},
			"the root is the zero reflect.Value, which names no type and so compiles to no schema")
	}

	return runDump(ctx, v, sink, opts)
}

func (valueWalk) LoadValue(ctx context.Context, dst reflect.Value, src Source, opts []Option) error {
	switch {
	case !dst.IsValid():
		return newError(momentWalk, ErrValue, Path{}, "...")
	case !dst.CanSet():
		return newError(momentWalk, ErrValue, Path{}, "...")
	}

	return runLoad(ctx, dst, src, opts)
}
```

`ferrytest/codecwalk.go` recovers it with one assertion against an interface it declares itself:

```go
type valueWalker interface {
	DumpValue(ctx context.Context, v reflect.Value, sink ferry.Sink, opts []ferry.Option) error
	LoadValue(ctx context.Context, dst reflect.Value, src ferry.Source, opts []ferry.Option) error
}

var coreWalk, coreWalkOK = valuewalk.Seam.(valueWalker)
```

### Why it is a bare `any` and not a typed variable

This is the one design constraint worth recording, because the obvious shape does not compile.

A typed seam variable would have to name `ferry.Sink`, `ferry.Source` and `ferry.Option`, which means `internal/valuewalk` would have to import `ferry` - and `ferry` imports `internal/valuewalk` to install into it.
That is an import cycle.

There are three ways out and only one is cheap:

1. **Erase the sink and the option list**, `func(ctx, reflect.Value, sink any, opts any) error`, and assert them back inside core.
   Works, but puts an unchecked hop in the middle of the seam and makes both call sites convert.
2. **Move `Sink`, `Source`, `Option` and their transitive closure into an internal package and alias them from `ferry`.**
   Fully type-safe with no erasure anywhere, and it means moving `Path`, `Value`, `VKind`, `Writer`, `Reader`, `AddressSet`, `OpenWriterFunc` and `OpenFunc` too - most of core's boundary vocabulary - for a test harness's benefit.
   Wildly out of proportion.
3. **One `any` holding a value with methods**, asserted back to an interface the *caller* declares over ferry's real types.
   Both ends are checked by Go's own method-set rules, the erasure is a single `any` that never travels anywhere, and the package in the middle names no ferry type.

Option 3 is what is built.
Its one weakness is that a signature drift between core and `ferrytest` becomes a failed assertion rather than a build error, so `Codec` checks `coreWalkOK` once and refuses loudly rather than running degraded.

Package-variable initialisation order makes it safe: a package's dependencies are fully initialised, variables and `init` functions both, before its own variables are, and core installs from an `init`.

### Did core's exported surface move?

**Output** (`go run ./proto137b surface /tmp/ferrybase` against the base commit `a60a652`, then `go run ./proto137b surface .`):

```
58 exported names in /tmp/ferrybase
AddressSet At Bool Bytes Committer Compile Dump DurationLike Elements Enumerator ErrDriver ErrMissing
ErrPlane ErrReadOnly ErrSchema ErrValue ErrWrongKind Error ErrorAt Index KeyFunc Keys KindAbsent
KindBool KindBytes KindNull KindNumber KindString Load LoadOver Name NewAddressSet NewKeys NewRegistry
Null Number OpenFunc OpenWriterFunc Option Path Reader Reg Register Registry Releaser Segment
SegmentKind Sink Source String StringCodec TagKey TextCodec VKind Value ValueCodec WithRegistry Writer

58 exported names in .
AddressSet At Bool Bytes Committer Compile Dump DurationLike Elements Enumerator ErrDriver ErrMissing
ErrPlane ErrReadOnly ErrSchema ErrValue ErrWrongKind Error ErrorAt Index KeyFunc Keys KindAbsent
KindBool KindBytes KindNull KindNumber KindString Load LoadOver Name NewAddressSet NewKeys NewRegistry
Null Number OpenFunc OpenWriterFunc Option Path Reader Reg Register Registry Releaser Segment
SegmentKind Sink Source String StringCodec TagKey TextCodec VKind Value ValueCodec WithRegistry Writer
```

Diffed as sorted word lists: **identical**.
`ferrytest`'s 23, diffed the same way: **identical**.

`valueWalk` is an unexported type.
Its two methods are exported names on an unexported type, reachable only through `internal/valuewalk`, which is unimportable outside this module.
A consumer of ferry cannot see, name or call any of it.

So **#134 is untouched.**
Nothing here says whether a `reflect.Value` root should be public; it says only that the harness can have one without the question being answered.

---

## 2. What became per-registrant

Cases 2, 3 and 5 now run a second time, once per type the caller's registry holds, from that type's zero value, through the real compiler and the real walk.
The six machinery cases stay: a defect in the wrapper is a defect in every codec anybody registers, and a caller whose registry is empty still wants that answered.

**Output** (`go run ./proto137b reach`, trimmed to two of four rows):

```
-- what the suite reaches for each registrant's own type, per case

  Meters (lossy)
    case 2, the zero encodes    reached, wrote string("0.00")
    case 3, and loads back      reached, re-encodes string("0.00")
    case 5, String donated      reached, declared kind is string
    case 4, drift               NOT REACHABLE: needs a value of main.Meters away from its zero
    case 6, two keys            NOT REACHABLE: needs two distinct values of main.Meters
  ferrytest.Codec(t, reg)                    NO REPORTS

  Wandering (zero is not a fixed point)
    case 2, the zero encodes    reached, wrote string("zero")
    case 3, and loads back      reached, re-encodes string("x:zero")
    case 5, String donated      reached, declared kind is string
    case 4, drift               NOT REACHABLE: needs a value of main.Wandering away from its zero
    case 6, two keys            NOT REACHABLE: needs two distinct values of main.Wandering
  ferrytest.Codec(t, reg)                    1 report(s)
    - codec case 3: main.Wandering: the zero value encodes to string("zero"), and loading that back
      and encoding it again writes string("x:zero"): the codec's two halves disagree at the one value
      they are both guaranteed to see
```

**Cases 4 and 6 stay probe-only, and that is a finding rather than a failure.**
They need a value away from the zero and two distinct keys respectively.
No API change supplies either, because only a `Proof` carries values.
That confirms PROTO-137's section 6 exactly.

### Case 3 gained a check that registration does not have

`Reg.total()` encodes the zero, decodes the result, and asserts only that **neither errors**.
It never compares.
So a codec whose decode half answers something else entirely passes registration, and the plane text is not a fixed point of the pair.

That is `Wandering` above, and it is a defect class PROTO-137's list of eight does not contain.
Call it **D9**.
Nothing but a walk over the registrant's own type sees it, and no proof is needed to reach it.

`ferrytest/codec_test.go` holds it as `TestCodecReportsAZeroThatIsNotAFixedPoint`.

### Case 5 is per-registrant and still structurally uninteresting

It now loads the registrant's own encoding back from a plane that spells it `String`, per type.
As PROTO-137 measured, core's `donate` rewrites a `String` to the declared kind before any codec is called, so a registrant's codec cannot fail it.
It is built because it costs four lines and it is the honest end-to-end statement of the donation, not because it is expected to fire.

---

## 3. #143, fixed by observation

Case 1 no longer asks "does this type carry two spellings that disagree".
It asks "**does ferry write the appender's bytes for this type**", by dumping the zero value through the caller's own registry and reading what landed.

```go
func (c *codecRun) ferryWritesTheAppender(t reflect.Type, appended []byte, appendErr error) bool {
	wrote, err := c.dumpZero(t)
	if appendErr != nil {
		return err != nil
	}

	if err != nil {
		return false
	}

	text, ok := textOf(wrote)

	return ok && text == string(appended)
}
```

**Output** (`go run ./proto137b 143`, with #143's own `Tag` fixture):

```
-- #143's fixture, registered through StringCodec, so ferry calls neither text half
  what ferry actually writes for the zero      string("")
  case 1 as it stood                         1 report(s)
    - codec case 1: main.Tag: AppendText wrote "FROM-APPEND" and MarshalText wrote "FROM-MARSHAL":
      ferry prefers the appender, so the plane holds the first of these
  ferrytest.Codec(t, reg), this branch       NO REPORTS

-- the same type registered through TextCodec, so ferry does prefer the appender
  what ferry actually writes for the zero      string("FROM-APPEND")
  case 1 as it stood                         1 report(s)
    - codec case 1: main.Tag: AppendText wrote "FROM-APPEND" and MarshalText wrote "FROM-MARSHAL":
      ferry prefers the appender, so the plane holds the first of these
  ferrytest.Codec(t, reg), this branch       1 report(s)
    - codec case 1: main.Tag: AppendText wrote "FROM-APPEND" and MarshalText wrote "FROM-MARSHAL":
      ferry prefers the appender, so the plane holds the first of these and a reader expecting the
      second is reading somebody else's bytes
```

`case 1 as it stood` is the old case reimplemented inline in the probe, so the before and after are the same run.
It fires on both registrations.
The new one fires on the `TextCodec` registration and is silent on the `StringCodec` one, which is exactly #143.

**Three things worth noting about the fix.**

It needs **no accessor on `ferry.Reg`.**
#143's own preferred remedy was "expose the arm", which it flagged as wanting ADR-0009's blessing because that ADR records "the harness needs no accessor on a registration".
Watching behaviour asks the same question of the engine instead, so ADR-0009 stands unamended.

It is **correct in the corner that looks like a false positive and is not.**
If a `StringCodec` happens to write precisely what `AppendText` would, the observation says the pair is in play and the report fires - and the report is true: the plane really does hold the appender's bytes, so a reader expecting the marshaler's really is reading somebody else's.

It **compares text across kinds**, not `ferry.String` against `ferry.String`.
A `TextCodec[T](KindNumber)` registration does use the text pair and does not write a `String`, and a kind-sensitive comparison would have re-introduced a silent false negative there.

Both `ferrytest/codec_test.go` cases are green: `TestCodecIsSilentWhereFerryNeverConsultsTheTextPair` and the pre-existing `TestCodecReportsATextPairThatDisagrees`.

---

## 4. The honest contract

### The name

**Keep `Codec`.**
Do not rename it to `Wrapper`.

PROTO-137's rename argument was that `Codec(t, reg)` reads as "check my registry" while taking a `*ferry.Registry` five of six cases ignore, so the signature makes a promise the body does not keep.
That argument is now void on its own terms.
Measured on this branch, `reg` is read by case 1, compiled against once per type it holds, frozen by the suite, and walked for every registration in it.
Renaming to `Wrapper` would now be *less* true than `Codec`, not more.

**This is the one place where B changes what A should have said**, and it is why "the best of both" is not simply A and B stapled together.

### The doc comment I would ship

It is in the tree, on `Codec` in `ferrytest/codec.go`.
In full:

```
// Codec is the codec conformance suite: six cases over the registration
// machinery, and every one of them that the zero value can reach run again over
// each codec the registry actually holds.
//
//	func TestCodec(t *testing.T) {
//	    reg := ferry.NewRegistry()
//	    if err := reg.Register(ferry.TextCodec[netip.Addr](ferry.KindString).AsMapKey()); err != nil {
//	        t.Fatal(err)
//	    }
//
//	    ferrytest.RoundTrip(t, ferrytest.MemPlane(), proofs, ferry.WithRegistry(reg))
//	    ferrytest.Codec(t, reg)
//	}
//
// It takes no [context.Context], for [Driver]'s reason, and every case cites the
// ADR sentence it executes.
//
// # What it promises, exactly
//
// Two things, and the second is bounded in a way worth reading before relying
// on it.
//
// The registration machinery works in this build of core. The generic wrapper
// is the single piece of reflection the registration API owns, it exists so
// that a registrant never writes a reflect.Value, and a defect in it is a
// defect in every codec anybody ever registers. Two such defects were found
// three prototypes in, both one token wide, and neither was catchable by any
// proof a registrant could write, because the codec itself was correct
// (ADR-0009). Cases 2 and 3 are those two, and they run against probes declared
// here, because what they assert is a property of the machinery rather than of
// any one registration.
//
// And every codec in reg survives its own zero value. For each type the
// registry holds, the suite builds an annotated root around that type and walks
// it: the zero value encodes, what ferry wrote loads back, encoding what came
// back writes the same thing again, and the same text spelled String loads
// identically, which is core's donation seen end to end. The zero value is the
// bound because it is the only value core holds without being handed one.
//
// # What it does not promise
//
// It does not check your codec away from its zero value, and most of what makes
// a codec wrong lives there. A lossy codec, a constant codec and a codec that
// declares one kind and emits another all pass every case here, because all
// three are correct at the zero value. So do two keys that fold to one address,
// which need two values to see at all.
//
// Cases 4 and 6 stay on this package's probes for that reason rather than for
// want of reach: they need a value away from the zero and two distinct keys
// respectively, and nothing core holds supplies either.
//
// The gate that closes this is [Complete], which reports every registered type
// with no [Proof] against it, and [RoundTrip], which drives the registrant's own
// values through the real engine. Run all three. A green Codec on its own says
// the machinery is sound and your codec is sound at one value, and that is the
// whole of it.
//
// # It compiles against reg
//
// A registry freezes at its first retained schema compile, and this suite
// performs one for every type reg holds. So reg is frozen when Codec returns,
// and every [ferry.Registry.Register] call must happen before the call to
// Codec rather than after it.
```

The "What it does not promise" section is what option A was for, and it survives B intact.
The pointer to `Complete` is the line PROTO-137 asked for and it is now in the doc rather than three files away.

### The freeze is a real behaviour change, and it should be in the ADR

**Output** (`go run ./proto137b contrast`, second half):

```
-- was reg handed to a verb? a registry freezes at its first retained compile
  Register(Constant) before Codec            <nil>
  Codec(t, reg)                              NO REPORTS
  Register(Digits) after Codec               ferry: main.Digits: the registry is frozen, because a
      schema has already been compiled against it - every registration must happen before the first
      Load or Dump, ...
  Codec(t, an empty registry)                NO REPORTS
  Register(Meters) after Codec               <nil>
```

Before this branch, `Codec` left the registry unfrozen and a `Register` after it succeeded.
Now a `Register` after `Codec` is refused, **unless the registry was empty when `Codec` ran**, which is the awkward part: the failure appears only for callers who have already registered something, which is all of them in practice but not in a trivially-written test.
The doc comment says it out loud.
ADR-0014's `Codec` entry should gain the same sentence.

I do not think this is avoidable.
A walk retains its resolution by definition, and retention is what the freeze exists for (ADR-0009).
A non-retaining walk would be a schema resolved against one set of codecs and walked against another, which is the exact defect the freeze prevents.

---

## 5. The eight defect classes, before and after

Every gate re-run live against every class on this branch.

**Output** (`go run ./proto137b defects`, edited only for line wrapping):

```
D1 not total over the zero value
  Register                 CAUGHT: ferry: main.Meters: the codec is not total over the zero value: it
      encodes to string("not a number") and decoding that back fails: the plane's value is not a
      valid main.Meters

D2 lossy
  Register                 passes
  ferrytest.Codec                            NO REPORTS
  Complete                 CAUGHT: main.Meters has a registered codec and has no proof
  RoundTrip + proof                          2 report(s)
    - plane memory: Meters: case 1: ferry encoded string("0.33") at /value, want
      string("0.3333333333333333")
    - plane memory: Meters: case 1: loaded 0.33, want 0.3333333333333333
  a real Dump              silent, wrote {/value=string("0.33")}

D3 constant
  Register                 passes
  ferrytest.Codec                            NO REPORTS
  Complete                 CAUGHT: main.Constant has a registered codec and has no proof
  a real Dump              silent, wrote {/value=string("0.00")}

D4 drifting kind
  Register                 passes
  ferrytest.Codec                            NO REPORTS
  Complete                 CAUGHT: main.Drift has a registered codec and has no proof
  a real Dump              CAUGHT: ferry: /value: the codec for main.Drift declared string and
      produced number: the declared kind is what a plane is promised on the way out and what String
      is donated to on the way back

D5 consistently wrong kind
  Register                 passes
  ferrytest.Codec                            NO REPORTS
  Complete                 CAUGHT: main.Digits has a registered codec and has no proof
  a real Dump              silent, wrote {/value=string("42")}

D6 non-injective key
  Register                 passes
  ferrytest.Codec                            NO REPORTS
  Complete                 CAUGHT: main.Folding has a registered codec and has no proof
  a real Dump              CAUGHT: ferry: /m: /m/ab is addressed more than once, and one of the two
      writes would be lost: the addresses under a map[main.Folding]string come from the value, and
      this one is an address already

D7 nil-hostile interface codec
  Register                 CAUGHT: PANICKED: runtime error: invalid memory address or nil pointer
      dereference

D8 lossy, proof values lossless
  Register                 passes
  ferrytest.Codec                            NO REPORTS
  Complete                 CAUGHT: main.Meters has a registered codec and has no proof
  RoundTrip + proof                          NO REPORTS
  a real Dump              silent, wrote {/value=string("0.33")}

D9 zero is not a fixed point
  Register                 passes
  ferrytest.Codec                            1 report(s)
    - codec case 3: main.Wandering: the zero value encodes to string("zero"), and loading that back
      and encoding it again writes string("x:zero"): the codec's two halves disagree at the one value
      they are both guaranteed to see
  Complete                 CAUGHT: main.Wandering has a registered codec and has no proof
  a real Dump              silent, wrote {/value=string("zero")}
```

### Before and after

`Codec` before is PROTO-137's measured column; `Codec` after is measured above.

| defect | `Register` | `Codec` **before** | `Codec` **after** | core, at a real `Dump` | `Complete` | `RoundTrip` + proof |
| --- | --- | --- | --- | --- | --- | --- |
| D1 not total at zero | **catches** | - | - | - | - | catches |
| D2 lossy | passes | silent | **silent, unchanged** | silent | demands a proof | **catches** |
| D3 constant | passes | silent | **silent, unchanged** | silent | demands a proof | catches |
| D4 drifting kind | passes | silent | **silent, unchanged** | **catches** | demands a proof | catches |
| D5 consistently wrong kind | passes | silent | **silent, unchanged** | silent | demands a proof | golden column only |
| D6 non-injective key | passes | silent | **silent, unchanged** | **catches**, both keys present | demands a proof | - |
| D7 nil-hostile iface codec | **catches**, as a panic | - | - | - | - | - |
| D8 lossy, proof lossless | passes | silent | **silent, unchanged** | silent | silent | **silent** |
| **D9 zero not a fixed point** | **passes** | **silent** | **catches** | silent | demands a proof | catches |

**Seven of the eight rows do not move.**
That is the headline of this section and it should not be softened.
D2 through D6 and D8 are all correct at the zero value by construction, and the zero value is the whole of the reach.

**D8 is unchanged and no reach can change it.**
It is the class where the codec is wrong, a proof exists, and the proof's values happen to be lossless.
The `RoundTrip + proof` line above is measured with a proof carrying `Meters(0)` and `Meters(2.5)`, both of which are exact under a two-decimal format, so every gate is green while `Dump(1.0/3.0)` still writes `0.33`.
The defect is in the **value list**, not in the API, and ADR-0009 already names it:

> a proof carries the type's zero value, its extremes, and the values that historically break it

**D9 is what the reach adds.**
It is not on PROTO-137's list because nothing in the old suite could produce it.
Its honest status: `Complete` would have forced a proof and `RoundTrip` would then have caught it, so the reach improves *when* you find out rather than *whether*.
That is a smaller win than it first looks, and it is a real one - a registrant finds it from a four-line `Codec` call before they have written a proof.

### And the previous investigation's decisive probe, re-run

**Output** (`go run ./proto137b contrast`, first half):

```
-- the same suite over three registries
  Codec(t, nil)                              NO REPORTS
  Codec(t, three wrong codecs)               NO REPORTS
  Codec(t, one wandering codec)              1 report(s)
    - codec case 3: main.Wandering: ...
```

`Codec(t, nil)` and `Codec(t, three wrong codecs)` are **still byte-identical**, even though the registry is now genuinely walked.
The three wrong codecs are the lossy one, the drifting one and the folding key one, and all three are correct at their zero value.

This is the sharpest way to state the result: **the seam removed the reach wall and the reports did not change, because the wall was never what was hiding those defects.**
What was hiding them is that they live away from the zero value, and that is a values problem, which is a `Proof` problem.

---

## 6. The cost

| | |
| --- | --- |
| **Exported names added to `ferry`** | **0** (58 before, 58 after, identical lists) |
| **Exported names added to `ferrytest`** | **0** (23 before, 23 after, identical lists) - the AST-locked surface test needs no edit |
| **New package** | `internal/valuewalk`, 1 file, **2 lines of code** |
| **New file in core** | `valuewalk.go`, **26 lines of code**, 1 `init`, no change to any existing file |
| **New file in `ferrytest`** | `codecwalk.go`, **59 lines of code** |
| **Changed in `ferrytest`** | `codec.go`, **+87 lines of code** (+161 lines with doc comments), 2 deletions |
| **Tests added** | `codec_test.go`, +67 lines, 2 new cases |
| **Total shipped code** | **174 lines**, excluding doc comments, tests and the probe |
| **Structural limits** | hold. `make lint` reports **0 issues** across all four modules, with **no `//nolint`** |
| **`//go:linkname`, `unsafe`, build tags** | none |
| **Behaviour change** | `Codec` now freezes the registry it is handed (section 4) |

The probe itself, `proto137b/`, is another 689 lines and is throwaway.
It lints clean too, which is why it is in the tree rather than deleted.

**No structural limit was broken and none was raised.**
Two functions had to be split to stay under `cognitive-complexity 7` while writing this, both in the probe, and the split was the right change in both cases.

`ferrytest`'s exported surface staying at 23 is worth stating separately, because it was a live risk: the obvious way to give the probe the same reach the suite has is to export a `RecordValue`, which would have made it 24 and put an ADR-0014 amendment on the critical path.
The probe imports `internal/valuewalk` directly instead, which it may do because it is in the same module - and which is itself a small demonstration that the seam is module-wide rather than `ferrytest`-only.

---

## Recommendation

**Ship the seam and the case 1 fix.
Do not rename.
Take the doc comment.**

Three separate calls, and they should be judged separately.

**The internal seam: yes, and it is strictly better than exporting.**
It is 28 lines across two files, adds zero exported names, and leaves #134 genuinely open.
There is no argument for `DumpValue`/`LoadValue` as public API that runs through the test harness any more, which is what the previous investigation wanted protected.
If #134 later decides that a `reflect.Value` root should be public, the exported functions become two more lines over the same `runDump` and `runLoad`, and this seam either stays or is deleted - nothing here forecloses that.

**The case 1 fix: yes, and it is the only part of this whose value is not in doubt.**
#143 is a false positive with a false explanation attached, which costs a user time on a defect that does not exist, and this closes it without an accessor on `Reg` and without ADR-0009 moving.
It is also the one thing here that *needs* the seam: there is no way to observe what ferry writes for a type you hold as a `reflect.Type` without a walk over one.

**The per-registrant pass on cases 2, 3 and 5: yes, but for less than it looks.**
It buys D9 and named reports where a panic used to come out of `Register`.
It buys nothing on D1 through D8.
It is 87 lines and it is honest, but nobody should expect it to catch a wrong codec, and the doc comment now says so in those words.

**The rename: no, and this is where I disagree with PROTO-137.**
Its argument was that the signature promises a registry the body ignores.
After the seam the body does not ignore it, so the argument does not survive its own premise.
`Wrapper` would describe the suite less accurately than `Codec` does.

**What I would not claim.**
That this makes `Codec` a check of a registrant's codec.
It checks a registrant's codec at one value, and the previous investigation's judgement - that the residual is closed by `Complete` plus `RoundTrip` and by the values in a `Proof`, not by reach - is confirmed rather than overturned by everything above.
Seven of eight rows did not move, and `Codec(t, nil)` still cannot be told from `Codec(t, three wrong codecs)`.

If the owner wants one thing from this branch, it is #143 and the honest doc comment.
The reach is what makes the first of those possible and is otherwise close to break-even.
