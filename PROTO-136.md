# PROTO-136: what is stored at a container address

Branch `proto/136-container-reads`, based on `feat/83-suites`.
Every table below is output from `go run ./proto136`, which lives in `proto136/` on this branch and runs against the real engine through `ferry.Load`, `ferry.LoadOver`, `ferry.Dump` and `ferrytest`.
Nothing here is asserted without a run behind it.
`go test ./...` is green on this branch with the probe in the tree.

## The framing, checked first

The issue says case 3 and case 12 "describe the same addresses and disagree about what is stored there", and that a driver cannot satisfy both.

**Half right, and wrong on the reason and on the consequence.**

Right: read literally against a plane that ferry's own `Dump` wrote, case 3 is false at two of its four rows.
The issue's own measurement is confirmed exactly, two and two.

Wrong on the reason: the two cases are not over the same fixture.
Case 3's four shapes are ADR-0005's **source-side** table, which was measured over a YAML document a human wrote.
Case 12's three shapes are **Go values** handed to `Dump`.
"An empty list" is a plane node in one sentence and a Go value in the other, and they are not the same thing.
The ADR is imprecise about which, rather than self-contradicting.

Wrong on the consequence, and this is the part worth correcting:

> If `Get` returned `Absent` for an empty list, then a missing key and `tags: []` would be one observation, which is the collision ADR-0005 explicitly rejected

ADR-0005's own measured table **says they are one observation** on a real YAML plane, and calls that collision forced and accepts it.
What ADR-0005 rejected was the *other* collision, on the **Dump** side: writing nothing at all for an empty composite.

> and a map key whose value minted nothing would vanish

Measured in section 4d: under case 3's reading the key **does not vanish**.
It vanishes only when `Dump` writes nothing at the container address, and case 3 says nothing about `Dump`.

Case 3's reading does break real things.
They are just not the things the issue names, and section 4 is where they are.

## 1. What Dump actually writes at a container address

`ferrytest.Record` is a dump into a sink that keeps what it was handed, so this is the real walk with no plane in the way.

```go
type shapes struct {
	NilSlice   []string          `ferry:"nilslice"`
	EmptySlice []string          `ferry:"emptyslice"`
	FullSlice  []string          `ferry:"fullslice"`
	NilMap     map[string]string `ferry:"nilmap"`
	EmptyMap   map[string]string `ferry:"emptymap"`
	FullMap    map[string]string `ferry:"fullmap"`
	NilPtr     *section          `ferry:"nilptr"`
	SetPtr     *section          `ferry:"setptr"`
	NilPtrSl   *[]string         `ferry:"nilptrslice"`
	Array      [2]string         `ferry:"array"`
}

mapped, err := ferrytest.Record(context.Background(), shapesValue())
```

Output (`go run ./proto136 1`):

```
  /array#0                 String("p")
  /array#1                 String("q")
  /emptymap                Null
  /emptyslice              Null
  /fullmap/k               String("v")
  /fullslice#0             String("a")
  /fullslice#1             String("b")
  /nilmap                  Null
  /nilptr                  Null
  /nilptrslice             Null
  /nilslice                Null
  /setptr/name             String("n")

-- the container addresses themselves, and whether a Value landed on one
  /nilslice                Null
  /emptyslice              Null
  /fullslice               nothing written at the container address
  /nilmap                  Null
  /emptymap                Null
  /fullmap                 nothing written at the container address
  /nilptr                  Null
  /setptr                  nothing written at the container address
  /nilptrslice             Null
  /array                   nothing written at the container address
```

One rule covers all ten rows.
A container address gets exactly one `Set` of `Null` when the container has no elements, and gets no `Set` at all when it has any.
A container address and something under it are never both written, which is ADR-0003.
An array has no empty form and no nil form, so it never mints its own address.

## 2. What Load actually reads back

The same plane, loaded back, with every question the walk asked recorded through a wrapping `Source`.

```go
mapped, _ := ferrytest.Record(context.Background(), shapesValue())
src := spySource{inner: ferrytest.Static(maps.Clone(mapped)), log: &log}
got, err := ferry.Load[shapes](context.Background(), src)
```

Output (`go run ./proto136 2`), with the pointer values shortened to `0x...` because they differ per run and nothing else edited:

```
-- the round trip
  in  {NilSlice:[] EmptySlice:[] FullSlice:[a b] NilMap:map[] EmptyMap:map[] FullMap:map[k:v] NilPtr:<nil> SetPtr:0x... NilPtrSl:<nil> Array:[p q]}
  out {NilSlice:[] EmptySlice:[] FullSlice:[a b] NilMap:map[] EmptyMap:map[] FullMap:map[k:v] NilPtr:<nil> SetPtr:0x... NilPtrSl:<nil> Array:[p q]}
  NilSlice==nil true  EmptySlice==nil true  NilMap==nil true  EmptyMap==nil true
  NilPtr==nil true  SetPtr &{n}  NilPtrSl==nil true

-- every question the walk asked, at a container address
  Get(/nilslice) -> Null
  Get(/emptyslice) -> Null
  Get(/fullslice) -> Absent
  Children(/fullslice) -> [/fullslice#0 /fullslice#1]
  Get(/nilmap) -> Null
  Get(/emptymap) -> Null
  Get(/fullmap) -> Absent
  Children(/fullmap) -> [/fullmap/k]
  Get(/nilptr) -> Null
  Get(/setptr) -> Absent
  Get(/nilptrslice) -> Null
  Children(/array) -> [/array#0 /array#1]
```

The walk asks `Get` at every container address it can be nil at, **first**, and only reaches for `Children` after an `Absent`.
That order is `walk.go`'s `members`, and its doc comment says why:

> A Null at the container address is a complete answer and a source that cannot list can still give it; only after Absent does the walk need the members.

For a round trip to work, the plane has to hand back exactly what it was handed: `Null` at the five addresses a `Null` was written to, and `Absent` at the four it was not.

### Case 3's four shapes, specifically

```go
store := map[ferry.Path]ferry.Value{
	ferry.At("list").Elem(0): ferry.String("a"),
	ferry.At("empty"):        ferry.Null(),
	ferry.At("emptymap"):     ferry.Null(),
}
```

Output (`go run ./proto136 3`):

```
  shape                address                      Get        case 3 says Absent
  a populated list     /list                        Absent     TRUE (err <nil>)
  an empty list        /empty                       Null       FALSE (err <nil>)
  an empty map         /emptymap                    Null       FALSE (err <nil>)
  a missing key        /missing                     Absent     TRUE (err <nil>)
```

## 3. Which of case 3's four claims hold

| case 3's claim | verdict, on a plane ferry's Dump wrote | verdict, on a human's YAML document | verdict, on a flat plane |
| --- | --- | --- | --- |
| a populated list returns `Absent` | **true** | **true** | **true** |
| an empty list returns `Absent` | **false**, it is `Null` | **true**, ADR-0005's measured table | **true**, and the empty list could not be dumped there at all |
| an empty map returns `Absent` | **false**, it is `Null` | **true**, ADR-0005's measured table | **true**, same |
| a missing key returns `Absent` | **true** | **true** | **true** |
| a nil error always | **true** | **true** | **true** |

That is the whole disagreement, and it is one cell wide in three columns.
Case 3 as written is **true for a flat plane and true for a human-authored tree document, and false for a plane ferry itself wrote**.
It is not a rule that is wrong; it is a rule stated without its fixture.

## 4. What breaks under each reading

The one fact everything else falls out of, from `walk.go`'s `container`:

> `Null` at a container address sets the field to its zero value **and counts as a write**.
> `Absent` at a container address writes nothing and sends the walk looking underneath.

`Absent` does not write is ADR-0006's whole rule, so making `Get` answer `Absent` where a `Null` was stored does not merely rename an observation.
It removes a write.

### 4a and 4b, side by side

Same plane both times, the one `Dump` wrote for `tagsOnly{Tags: []string{}, M: map[string]string{}}`, holding a `Null` at `/tags`, `/m` and `/opt`.
The second run wraps the source so that every container address answers `Absent`, which is case 3's reading implemented as a driver.

```go
seed := tagsOnly{Tags: []string{"kept"}, Opt: &section{Name: "kept"}, M: map[string]string{"k": "kept"}}
over, err := ferry.LoadOver(context.Background(), seed, src)
nl, err := ferry.Load[tagsOnly](context.Background(), noList{inner: src}) // a reader with no Children
```

Output (`go run ./proto136 4`):

```
-- 4a. the Null reading, which is what the engine does today
  the plane after Dump: [/m /opt /tags]
  Load into a zero value          -> Tags=[]string(nil) Opt=<nil> M=map[string]string(nil) err=<nil>
  LoadOver a populated seed       -> Tags=[]string(nil) Opt=<nil> M=map[string]string(nil) err=<nil>
  Load from a source that cannot list -> Tags=[]string(nil) M=map[string]string(nil) err=<nil>

-- 4b. case 3's reading: the same plane, every container address answering Absent
  Load into a zero value          -> Tags=[]string(nil) Opt=<nil> M=map[string]string(nil) err=<nil>
  LoadOver a populated seed       -> Tags=[]string{"kept"} Opt=&{kept} M=map[string]string{"k":"kept"} err=<nil>
  Load from a source that cannot list -> Tags=[]string(nil) M=map[string]string(nil)
     err: ferry: /m: the addresses under a map[string]string come from the value, and main.noListReader
          cannot list what a plane holds under an address: a source that does not implement
          ferry.Enumerator reaches every static address and no dynamic one, which is a property of that
          plane rather than of this schema
     err: ferry: /tags: (the same text, for []string; wrapped and elided here only)
```

Three things break under case 3's reading, and none of them is the thing the issue predicted.

**The seeded round trip stops round-tripping, silently.**
Dump a config whose `Tags` is empty, reload it over the running config, and the field keeps the old value.
`ferry.LoadOver`'s own doc calls this out as the reload path: `cfg, err = ferry.LoadOver(ctx, cfg, src)`.
Under the `Null` reading the field is cleared, which is what was written.
Under the `Absent` reading it is not, and there is no error.
This is the failure mode ADR-0001 rules out, arrived at through the read side rather than the write side.

**A source that cannot enumerate stops being able to load an empty composite at all.**
`Null` is a complete answer that needs no `Children`.
`Absent` sends the walk to `ferry.Enumerator`, and a Vault token with read and no list, which ADR-0004 keeps the interface optional for, now fails on a field it used to load.

**`required` on an optional section stops being satisfiable by a null.**
ADR-0006 states that `required` is satisfied by any observation other than `Absent`, and that a `Null` at a `*T` satisfies it while yielding `nil`.
That observation is read at the container address, so removing it turns a legal load into a refusal:

```go
type reqSection struct {
	Auth *section `ferry:"auth,required"`
}
```

```
-- 4e. required on an optional section, which reads presence off the same address
  Null at /auth, reported as Null   -> Auth=<nil> err=<nil>
  Null at /auth, reported as Absent -> Auth=<nil>
     err: ferry: /auth: required, and the plane supplied nothing under it
```

The plane supplied a null and was told it supplied nothing.

### 4c. Can a caller still tell an empty list from a missing key

```
  missing key                          Load -> []string(nil)   LoadOver{seed} -> []string{"seed"}
  Null at /tags (ferry's empty list)   Load -> []string(nil)   LoadOver{seed} -> []string(nil)
```

Through `Load` into a zero value: **no, under either reading**, and that is ADR-0005's forced collision working as decided.
Through `LoadOver`: **yes under the `Null` reading, no under the `Absent` reading**.
The bit survives in the write, not in the value.

So the issue's claim that case 3's reading "reopens the collision" is not right as stated, and the honest version is stronger: case 3's reading does not reopen the nil-versus-empty collision, it **erases a distinction ADR-0006 kept**, the one between a plane that spoke and a plane that was silent.

### 4d. The vanishing map key, and which side it lives on

```
  what Dump writes today: [/m/a#0 /m/b /m/c]
  loaded back              -> map[string][]string{"a":{"x"}, "b":nil, "c":nil}
  loaded with Absent at every container -> map[string][]string{"a":{"x"}, "b":nil, "c":nil}
  the draft ADR-0005 rejected, which writes nothing for an empty composite: [/m/a#0]
  loaded back              -> map[string][]string{"a":{"x"}}
```

The key `"c"` survives a driver that answers `Absent` at every container address, because the address is still there to be enumerated and `walk.go`'s `atKey` sets the map index whether or not anything landed under it.
It vanishes only in the third row, where `Dump` wrote nothing.
The vanishing is a `Set`-side property and case 3 is a `Get`-side claim, so ADR-0005's audit does not reach case 3.

### Does anything break under the `Null` reading

On a plane that can carry a null: nothing measured.
The round trip is exact for all ten shapes (section 2), `LoadOver` clears what was written, and a non-enumerating source still answers.

On a plane that cannot carry a null: the question does not arise, which is section 5.

## 5. The flat plane, which is the crux

`proto136/sec5.go` is an env-shaped plane written for this: one flat upper-case namespace, `_` as the separator, `-` folded onto `_`, a key function that refuses a non-injective address set at `Bind` naming both addresses, and **no null** in its declared kinds.

```go
func flatPlane() ferrytest.Plane {
	return ferrytest.Plane{
		Name: "flat (env-shaped, no null)",
		Kinds: []ferry.VKind{
			ferry.KindAbsent, ferry.KindBool, ferry.KindNumber, ferry.KindString, ferry.KindBytes,
		},
		Open: func() ferrytest.Instance { ... },
	}
}

// Set refuses a Null loudly, which is the whole of what "this plane has no null" means.
func (w flatWriter) Set(_ context.Context, addr ferry.Path, v ferry.Value) error {
	if v.Kind() == ferry.KindNull {
		return ferry.ErrorAt(addr, fmt.Errorf(
			"%w: this plane has no null, and an environment name either exists with text or does not exist",
			ferry.ErrPlane))
	}
	...
}
```

Output (`go run ./proto136 5`):

```
-- 5a. dumping an empty slice to a plane with no null
  Dump{Tags: []string{}, M: map[string]string{}} ->
     ferry: /m: the driver failed: plane error: this plane has no null, and an environment name either exists with text or does not exist
     ferry: /opt: the driver failed: plane error: (the same text)
     ferry: /tags: the driver failed: plane error: (the same text)
  the plane afterwards: []

-- 5b. dumping a populated one, for contrast
  err: ferry: /opt: the driver failed: plane error: this plane has no null, (the same text)
  the plane afterwards: [M_K=String("v") TAGS_0=String("a") TAGS_1=String("b")]

-- 5c. loading a container address back off a flat plane
  /tags                    Absent (err <nil>)
  /m                       Absent (err <nil>)
  /opt                     Absent (err <nil>)

-- 5d. the whole Driver suite against this plane, reported rather than failed
  log ... case 6 skipped: the plane's writer holds no resource ...
  log ... case 10 skipped: a Plane describes no per-request plane ...
  log ... case 11 skipped: the plane pins no golden artefact ...
  log ... case 12 skipped: the plane declares no null, so there is no Null for a container address to be
       handed; case 1 is where its refusal of one is asserted
```

**Every one of the twelve cases passes, and nothing falls through.**

An empty slice on a flat plane is not read back as `Absent` instead of `Null`.
It is **never written at all**, loudly, per address, and the plane is left untouched.
Case 3's second half never runs there, case 12 is skipped there, and the two sentences cannot disagree because only one of them is live.

Case 1 is exactly the thing that covers it.
Here is the same plane with one change, mangling `Null` onto `String("")`, which is what ADR-0004 measured xload doing:

```
-- 5e. the same plane, but flattening Null onto the empty string instead of refusing it
  FAIL ... []byte: case 0: the plane does not declare kind null and took null at /value without refusing it
  FAIL ... []string: case 0: the plane does not declare kind null and took null at /value without refusing it
  FAIL ... []string: case 1: the plane does not declare kind null and took null at /value without refusing it
```

`ferrytest.CoreTypes()`'s `[]string` proof is `At([]string(nil), ferry.Null())` then `At([]string{}, ferry.Null())`, so `case 1` there **is** the empty slice.
Case 1's loud-refusal half covers it by name.

**So yes, the contradiction resolves differently for tree planes and flat planes, and that changes the answer.**
Not by splitting ferry's rule in two, but by showing that case 3's text is the flat plane's rule, and case 12's is the tree plane's, and the ADR wrote both as if they were universal.

## 6. The user-visible consequence

```go
type Config struct {
	Tags   []string       `ferry:"tags"`
	Limits map[string]int `ferry:"limits"`
}

seed := Config{Tags: []string{"default"}, Limits: map[string]int{"rps": 10}}
```

Output (`go run ./proto136 6`):

```
  document                                 what the plane reports                               Load[Config].Tags    LoadOver(seed).Tags
  no tags key at all                       nothing at /tags                                     []string(nil)        []string{"default"}
  tags: []                                 Absent at /tags (ADR-0005's measured YAML driver)    []string(nil)        []string{"default"}
  tags: []                                 Null at /tags (if a driver reported the node so)     []string(nil)        []string(nil)
  tags: null                               Null at /tags                                        []string(nil)        []string(nil)
  tags: [a, b]                             two addresses under /tags                            []string{"a", "b"}   []string{"a", "b"}
  what Dump writes for Tags: []string{}    Null at /tags                                        []string(nil)        []string(nil)

-- the same rows for a map-typed field
  no limits key                                            Load -> map[string]int(nil)      LoadOver(seed) -> map[string]int{"rps":10}
  limits: {}  reported Absent                              Load -> map[string]int(nil)      LoadOver(seed) -> map[string]int{"rps":10}
  limits: null, and what Dump writes for map[string]int{}  Load -> map[string]int(nil)      LoadOver(seed) -> map[string]int(nil)
  limits: {rps: 5}                                         Load -> map[string]int{"rps":5}  LoadOver(seed) -> map[string]int{"rps":5}

-- and the flat plane, where an empty Tags cannot be dumped at all
  Dump -> ferry: /tags: the driver failed: plane error: this plane has no null, and an environment name either exists with text or does not exist
  the plane afterwards: [LIMITS_RPS=Number(10)]
```

Through `ferry.Load` into a zero value, **the two readings are indistinguishable on every row**.
Every one of the four documents gives `nil` unless there are elements.
A user who only ever calls `Load` cannot tell which reading ferry chose, and no user-facing behaviour rides on it.

Through `ferry.LoadOver`, the readings differ on exactly one row, and it is the row a reload lands on.
That is the whole user-visible cost of getting this wrong.

**One thing neither ADR settles, and it is exposed by rows 2 and 3.**
What a tree driver should answer at an explicit `tags: []` node is not written down anywhere.
ADR-0005's measured table says `Absent`, and that is a measurement of one prototype rather than a decision.
Marked as inference: whichever way case 3 is amended, that row should be stated out loud, because the two answers differ under `LoadOver` and a driver author currently has to guess.

## 7. What a driver author has to implement

**Under the `Null` reading.**
Store what you were handed and give it back.
A tree driver writes the plane's own null at the node and reads it back as `Null`; a flat driver refuses the `Null` at `Set` and never faces the read.
There is no special case for container addresses on either side, no bit to track, and `Get` at any address is the same lookup it always was.
The obligation the driver author actually carries is one they already have for every other kind: do not mangle a kind you declared you cannot carry.

**Under the `Absent` reading.**
A driver must accept a `Null` at a container address, per case 12, and then must not report it back, per case 3.
That is a special case with a shape: the driver has to know which of its addresses are containers, which is one bit per address that `ferry.AddressSet` deliberately does not expose (ADR-0003 keeps it on the compiler's side).
A tree driver could infer it from its own node shape, a flat driver cannot, and a driver that stores the boundary `Value` directly, as the memory plane does, would have to keep a second table just to know which stored `Null`s to hide.
The alternative implementation, writing nothing at the container address, is the draft ADR-0005 rejected on the measured vanishing-key result.

**Which is harder to get right: the `Absent` reading, clearly.**
It asks for a bit the driver was not given, and it asks the driver to lie about its own contents.

**Which fails more loudly when got wrong: the `Null` reading, clearly.**
Under it, a driver that loses a container `Null` fails the round trip in section 2 and, once case 3 is tightened, fails the conformance case by name.
Under the `Absent` reading, a driver that reports the `Null` back fails nothing at all today, and its users get a stale field on reload with no error anywhere.

## Also checked: what #83's Driver case 3 does today

Confirmed, and it is exactly as the issue describes it.

`ferrytest/driver.go` demands a nil error always, `Absent` and only `Absent` at `/list`, `/map` and `/missing`, and `Absent` **or** `Null` at `/nillist` and `/emptymap`, with the reason in `holdsNothing`'s doc comment.
The second half is skipped entirely for a plane that declares no null.

Measured, by running the suite against the memory plane and against a wrapped memory plane written to case 3's letter, which answers `Absent` wherever a `Null` was stored.
Output (`go run ./proto136 7`):

```
-- memory
  no case failed

-- memory, answering Absent at every container address
  no case failed

-- what tightening case 3 to exactly Null would report, against the same two
  memory                                               /nillist Null -> would pass
  memory                                               /emptymap Null -> would pass
  memory, answering Absent at every container address  /nillist Absent -> would FAIL
  memory, answering Absent at every container address  /emptymap Absent -> would FAIL
```

A driver written to case 3's letter passes all twelve cases today.
It would fail two rows of one case under the tightening, and no other case moves.

What it tightens to under each reading:

- Under the `Null` reading: `holdsNothing(r, addrNilList, ferry.KindNull)` becomes an exact-`Null` assertion, and the `Absent` arm is dropped for those two rows only. The skip for a no-null plane stays exactly as it is, because case 1 still owns the refusal.
- Under the `Absent` reading: the two rows drop their `Null` arm instead, and case 12 needs a sentence saying that a driver must accept a `Null` it will then never report, which is a rule no ADR currently states.

## The decision in front of the owner

### Option A: case 3 splits by fixture, and the `Null` reading wins

Amend ADR-0014's case 3 in place to name what wrote the plane.
A populated list and a missing key answer `Absent`.
A container address that a `Dump` wrote a nil or empty composite to answers `Null`, citing ADR-0005.
A container address on a plane that declares no null answers `Absent`, because nothing could ever have been written there.
State the open row from section 6 while the ADR is open: what a tree driver answers at an explicit `tags: []` node.

Commits ferry to: `Null` at a container address is a value the plane holds and a write, on every plane that has a null.
ADR text that moves: ADR-0014, case 3, one entry, amended in place under the convention.
Code that moves: `ferrytest/driver.go`'s `caseContainerGet` tightens two rows from "either" to exactly `Null`.
Nothing in ADR-0005, ADR-0006, ADR-0004 or the walk moves.

### Option B: case 3 stays as written and the engine changes

Commits ferry to: `Absent` at every container address, so a container's own address carries nothing on the read side.
ADR text that moves: ADR-0005's "A composite with no elements writes `Null` at its own address"; ADR-0006's Absent-and-Null-per-kind table for containers, and its `required` section, since `required` on a `*struct` is currently satisfied by that `Null` and is measured refusing it under this reading; ADR-0004's "`Null` means the plane has this address" for the container case; ADR-0014's case 12, which would then be demanding a write nothing may read.
Code that moves: `walk.go`'s `container` and `members`, plus a new answer for the non-enumerating source that can no longer load an empty composite, and a new answer for `required` on an optional section.
Measured cost: the seeded reload stops clearing, silently; `required` on an optional section refuses a plane that spoke; and #75's argument reopens.

### Option C: leave case 3 permissive, as #83 shipped it

Commits ferry to nothing, which is the problem.
ADR text that moves: one sentence in ADR-0014 admitting the permissiveness is deliberate.
Cost: a driver may drop a container `Null` on the floor and still be certified conformant, and its users get a stale field on every reload with no error anywhere.
The suite would be certifying an ambiguity rather than a contract, in the package ADR-0002 says is the contract in executable form.

### Recommendation

**Option A.**

The measurement says the disagreement is one cell wide and that the engine, ADR-0005 and ADR-0006 all already agree with each other; only ADR-0014's sentence is out of step, and only because it was written without naming its fixture.

The argument the issue gives for A is not the argument that holds.
The one that does is section 4: `Null` at a container address is a **write** and `Absent` is not, so answering `Absent` where a `Null` was stored does not rename an observation, it deletes one.
What that costs is a reload that keeps a value the plane no longer has, with no error, which is the outcome ADR-0001 exists to rule out.

Option B additionally has to invent an answer for two things that currently ride on that write: a non-enumerating source loading an empty composite, and `required` on an optional section.
Neither has an obvious one.

The flat-plane angle does not change the recommendation, but it changes how case 3 should be written.
Case 3's text is correct for flat planes and for human-authored tree documents and wrong only for planes ferry itself wrote, so the amendment should name the fixture rather than pick a winner between two sentences that were never really about the same thing.
