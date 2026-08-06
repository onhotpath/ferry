# Writing a driver

A ferry driver is two methods and, if your plane needs them, up to five more.
This is the whole contract, plus the one call that proves you got it right.

The decision records are [ADR-0004](../adr/0004-source-and-sink.md) for the contract, [ADR-0003](../adr/0003-how-a-leaf-addresses-a-plane.md) for addressing, [ADR-0016](../adr/0016-the-sealed-address-model.md) for the address kinds the contract is signed over, and [ADR-0014](../adr/0014-what-ferrytest-exports.md) for the conformance suite.

Four drivers ship in this repository and are worth reading beside this page: [`driver/env`](../../driver/env/), a read-only flat plane; [`driver/kv`](../../driver/kv/), a flat plane in both directions with a caller-supplied client; [`driver/yaml`](../../driver/yaml/), a tree plane that edits a file in place; and [`driver/http`](../../driver/http/), a read-only plane over a query string or a header block, which is the one that has to spell one address two ways.
`ferrytest.MemPlane` in [`ferrytest/memplane.go`](../../ferrytest/memplane.go) is a complete, working driver of about the size yours will be, and is the shortest thing to read first.

## The vocabulary

- A **plane** is whatever holds your data: a file, a KV store, a process environment, an HTTP query string.
- A **path** is a `ferry.Path`: an ordered sequence of segments, each carrying a kind (`Name` or `Index`) and a text.
  `/db/host` and `/tags#0` are paths.
- An **address** is a path plus what kind of place it names, and the three kinds are three types.
- A **plane key** is what your plane calls that address: `DB_HOST`, `db/host`, or nothing at all if your plane is a tree and you walk the segments.
- A **`ferry.Value`** is what crosses the boundary: a kind and a text, in a comparable 24-byte struct.
  The six kinds are `Absent`, `Null`, `Bool`, `Number`, `String` and `Bytes`.

**Core never produces a plane key**, because a separator is plane knowledge.
Flattening is the driver's, always.

## The three address kinds

```go
type LeafAddr      struct{ /* sealed */ }  // a place a Value can be
type SectionAddr   struct{ /* sealed */ }  // children known from the type
type CompositeAddr struct{ /* sealed */ }  // children minted by the value
```

A **leaf** is a place a `ferry.Value` can be: a `string`, an `int`, a `time.Duration`, anything a codec carries.
A **section** is a struct, an array, or either of those behind a pointer: its children come from the type, so they are all in the address set you were handed and no driver is ever asked to list them.
A **composite** is a slice or a map: its children do not exist until there is a value, so they are in no address set and only enumeration reveals them.

The three **partition** the address space and they are not interchangeable.
`/db` as a section and `/db` as a composite are different addresses, and asking a set whether it holds one answers nothing about the other.

**Each carries one unexported field, so nothing outside core can build one.**
There is no conversion into the struct type and no constructor, and the schema compiler is the only thing that mints an address at all.
That is what puts the wrong question out of reach rather than under a runtime guard: `Get` takes a leaf, `Probe` takes a container and `Children` takes a composite, so asking a plane for the *value* of a section is a compile error in your driver.

That matters because it was a live class of defect and not a hypothetical.
[ADR-0016](../adr/0016-the-sealed-address-model.md) records four issues that were the same mistake at four addresses, the sharpest being a struct field named `home` that made the env driver read the ambient `$HOME`, because `Get` was asked for a value at an address no value can be at and the process environment happened to have one.

Read an address with the two methods every kind has:

```go
addr.Path()    // the path, kind dropped: what a key function walks
addr.String()  // the canonical rendering, for a diagnostic
```

`Path()` is the seam to everything path-shaped, including `ferry.KeyFunc`, which still takes a `ferry.Path` because a plane key is a function of the segments and never of the kind.

**Which Go type produced a composite is withheld**, deliberately.
A `[]string` and a `map[string]string` are both a `CompositeAddr`.
You mint `Name` or `Index` segments from what your plane holds, and the schema types the child; which of the two Go types is behind it is core's business and refusing a mismatch is core's job.

## The two required methods

```go
type Source interface {
	Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error)
}

type Sink interface {
	Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error)
}

type OpenFunc       func(ctx context.Context) (Reader, error)
type OpenWriterFunc func(ctx context.Context) (Writer, error)

type Reader interface {
	Get(ctx context.Context, addr ferry.LeafAddr) (ferry.Value, error)
}

type Writer interface {
	Set(ctx context.Context, addr ferry.LeafAddr, v ferry.Value) error
}
```

A read-only driver implements **two methods in total**: one `Bind` and one `Get`.

Both required methods take a **leaf**, so neither is ever asked about a container's own address.
What a plane holds at a container address is `Prober`'s answer, and what a dump has to say there is `Ensurer`'s, both below.

`Source` and `Sink` are separate interfaces rather than two halves of one, and the deciding case is environment variables, which have no honest dump.
With two interfaces the refusal is free: `driver/env` does not implement `Sink`, so `ferry.Dump` with it is a compile error at the call site rather than a runtime error or an `ErrUnsupported` nobody reads.

The cost is stated rather than hidden: a driver serving both directions ships **two types**, because one type cannot have two `Bind` methods, so a round trip names the plane twice.

### The three lifetimes

`Bind` is a phase of its own because the three pieces of state have three lifetimes:

| | holds | changes when |
| --- | --- | --- |
| your `Source` | driver config: path, separator, prefix, client | never; you constructed it |
| the `OpenFunc` | the precomputed key table | the schema or the driver config changes |
| the `Reader` | the plane's contents | every load |

`OpenFunc` may be called many times against one `Bind`, and it may be called from many goroutines at once, because a caller may hold what `Bind` returned and load through it concurrently.
Precompute at `Bind` and only read it afterwards and you already satisfy that; write to what you closed over and you do not.
The same holds for `OpenWriterFunc`.

Whether an open fetches everything in one round trip or fetches nothing at all is **your** business and not core's: `Bind` already handed you the whole address set.
That is why there is no `Snapshotter` interface.
`driver/kv` makes the choice with one option, `kv.WithBatch()`.

## The two checks that happen before any I/O

**`Bind` takes no `context.Context`, and that is how the type says no I/O happens here.**

Your `Bind` must succeed against a plane it cannot reach, and fail inside the open instead.
That is a conformance case, and it is only writable at all because the address set and the I/O arrive in two calls rather than one.

What `Bind` may fail for is exactly what it can see without touching the plane:

1. **Legality.** An address your plane cannot name at all.
   This is your question and no transformation rescues it.
2. **Injectivity.** A key function that renders two addresses to one plane key.
   That would merge them silently, so it is refused before any backend call, naming both.

One rule covers separator collisions, case folding and any normalisation you invent, because all three are the same failure.

Both are what `ferry.NewKeys` does for you:

```go
func NewKeys(a *ferry.AddressSet, name string, f ferry.KeyFunc) (*ferry.Keys, error)
type KeyFunc func(addr ferry.Path) (string, error)
```

A `KeyFunc` takes a `ferry.Path` and not a typed address, because a plane key is a function of the segments and never of the kind: `DB_HOST` is the same join whether `/db/host` is a leaf or a section.
Read one off a typed address with `addr.Path()`.

Hand it the address set, your driver's short name for diagnostics, and your key function.
It computes every static key once, checks both properties over the whole set, and returns an error naming every offending address:

```
ferry: /db_port: flat renders this address and /db/port to one plane key, "db_port",
       so one of the two would be lost
```

Return that error from `Bind` unchanged.
Core supplies the moment and leaves the rest alone.

**A driver is expected to transform segment text rather than to reject it.**
A key function that only validates refuses `feature-flags`, which is an ordinary thing to write in a config struct.
One that maps the hyphen to `_` accepts it and is not thereby less safe, because a many-to-one map out of the address set is precisely what the injectivity check catches.

**A hand-rolled key table opts out of both checks, silently.**
Nothing obliges you to route lookups through `ferry.Keys`, and core is not in the call if you do not, so it can give you no diagnostic.
The conformance suite hands you an address set your own transform folds together and asserts that `Bind` refuses.

**A tree driver calls none of this.**
It walks the segments, builds no plane key, and carries no injectivity obligation.
`driver/yaml` is that shape and pays nothing for the address set.

### Call `Keys.Open()` once per open, not once per `Bind`

```go
return func(context.Context) (ferry.Reader, error) {
	return reader{store: store, key: keys.Open()}, nil // right
}, nil
```

Each call to `Open` gets a fresh minted set.
Addresses a **value** mints - a map key, a sequence index - are not in the static set and arrive later, and they are checked as they are minted against the static table and against everything the same open has minted.

A `KeyFunc` hoisted out of the open retains what it minted across loads, so the second open refuses a write the first made legal, against an address no plane still holds.
That is conformance case 8, and the retention is unbounded: measured at 20,000 addresses held across 20,000 loads through one binding (ADR-0012).

The returned function is the open's and is not safe for concurrent use.
The binding it came from is, because the static table never changes after `NewKeys` returns.

### What the address set contains, and classifying it once

```go
func (a *ferry.AddressSet) Seq() iter.Seq[ferry.Member]
func (a *ferry.AddressSet) Has(m ferry.Member) bool
func (a *ferry.AddressSet) Len() int
```

Every address the type determines, each **typed** by what can be asked at it, and never a wildcard shape.
So you are handed only addresses you can fetch, write, name and check, and you are told which of those each one admits.

`ferry.Member` is one of those three kinds, and every address ferry hands you was minted by the schema compiler, so the type switch below covers every address you will be given.
Go seals a type and not an interface, so a value of your own can be written that satisfies `ferry.Member`; core refuses one, because it is in no address set and `Has` answers false for it.
Nothing you get from `Seq` is one.
`ferry.Container` is the sum of `SectionAddr` and `CompositeAddr`, which is what `Probe` and `Ensure` take.

**Classify once, at `Bind`, before any I/O:**

```go
func (s Source) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	keys, err := ferry.NewKeys(addrs, "flat", key)
	if err != nil {
		return nil, err
	}

	leaves := make(map[ferry.LeafAddr]string, addrs.Len())

	for m := range addrs.Seq() {
		if leaf, ok := m.(ferry.LeafAddr); ok {
			leaves[leaf] = mustKey(keys, leaf)
		}
	}
	...
}
```

One range and one type switch, on the cold path, and the typed addresses are comparable and go straight into a map as keys.

**That loop is the payoff, and it was previously unwritable.**
[ADR-0016](../adr/0016-the-sealed-address-model.md) records the measurement: a `string`, a `[]string` and a `map[string]string` at one tag compiled to three **byte-identical** address sets, so a driver's Bind-time "is this a container?" check compiled and could never fire.
They now compile to `{Leaf /x}`, `{Composite /x}` and `{Composite /x}`, and the third row is deliberately incomplete: the slice and the map are the same address because no driver needs the difference.

**The set is bigger than it used to be.**
Every nested struct and every array now contributes a `SectionAddr`, where before only a composite that could be nil contributed anything at all ([ADR-0003](../adr/0003-how-a-leaf-addresses-a-plane.md), amended).
A section with no address of its own is a section nothing can be asked about, and `Probe` is exactly that question.

The set is sorted segment-wise and the order is stable across builds of one schema, so you may key a table by position.

A driver that treats its precomputed table as a **closed** set refuses a legal write, because the addresses a value mints are in no address set.
That is why core hands out a key function rather than a map.

## The five optional interfaces

All five are discovered by type assertion on your `Reader` or `Writer`, and none is required.

```go
type Releaser interface {
	Close() error
}

type Committer interface {
	Commit(ctx context.Context) error
}

type Prober interface {
	Probe(ctx context.Context, addr ferry.Container) (ferry.SectionInfo, error)
}

type Enumerator interface {
	Children(ctx context.Context, addr ferry.CompositeAddr) ([]ferry.Segment, error)
}

type Ensurer interface {
	Ensure(ctx context.Context, addr ferry.Container, p ferry.Presence) error
}
```

| implement | if |
| --- | --- |
| `Releaser` | your reader or writer holds a resource |
| `Committer` | your writes are not durable until the end of a successful walk |
| `Prober` | your plane can say whether a container is there |
| `Enumerator` | your plane can list what is under a composite |
| `Ensurer` | your plane can spell a container at the container's own address |

Of the four drivers here, `driver/yaml` implements all five and the flat planes implement fewer: `driver/kv` reads with `Prober` and `Enumerator` and writes with `Committer` alone, and its refusal to implement `Ensurer` is a declaration rather than an omission, because a store that holds bytes at keys has no way to say that a container is there and holds nothing.

That spread is the case for making them optional rather than methods on `Reader` and `Writer`: a required `Close` would be `return nil` boilerplate in four of ADR-0004's six sinks, and in the source that is indistinguishable from a driver that should have rolled back and did not.
The same argument holds for `Ensurer`: a stub that stored a zero-length value would make "the section is present and empty" and "the field is empty text" one observation, which is precisely the collision the kinds exist to keep apart.

### `Releaser` and `Committer`

**The protocol is the whole design.**

> `Commit` runs only when the walk succeeded.
> `Close`, if you have one, runs either way.

Closed-without-`Commit` is the abort signal, so **no driver is ever told that it failed**.
That is `sql.Tx`'s shape and `bufio.Writer`'s, and it is why neither method takes a cause: there is no failure to report to a driver, only a commit that does not happen.

`Releaser` **is** `io.Closer`, so a driver wrapping a file or a connection satisfies it already.
`Close` takes no context because cleanup that can be cancelled is how the temp file leaks.
`Commit` takes one because it is the actual I/O.

A `Close` failure appears in the error set ferry reports.

A sink that needs `Commit` and omits it writes nothing at all, silently.
That is caught by the first case in the suite, which dumps a value and loads it back.

### `Prober`

This is what a plane is asked at a container's own address, and it is the only thing asked there.

```go
func (r reader) Probe(_ context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	switch node := r.at(addr.Path()); {
	case node == nil:      return ferry.SectionAbsent, nil
	case node.isNull():    return ferry.SectionNull, nil
	default:               return ferry.SectionPresent, nil
	}
}
```

The three plain answers are **values you return rather than functions you call**, which is `io.EOF`'s idiom, and the type is named for its work the way `fs.FileInfo` is: information about a section, reported by a probe.
A caller reads one back with `info.Presence()`, giving `ferry.PresenceAbsent`, `ferry.PresencePresent` or `ferry.PresenceNull`.
The zero `ferry.SectionInfo` is absence, so a driver with nothing to report returns `ferry.SectionInfo{}`.

There is a fourth answer, `ferry.SectionAt`, for a plane with links, and it is the subject of [Links](#links) below.

The three sentinels are package-level vars and are therefore reassignable, exactly as `io.EOF` is.
Core does not stand in that hazard, because what the walk compares against is an unexported copy, so assigning to one breaks the assigning program's own comparisons and nothing else.
Do not.

**What each answer means to the walk:**

| you answer | a nil-able section | a slice or a map that listed nothing |
| --- | --- | --- |
| `SectionAbsent` | the plane said nothing, so what is under it decides | the plane said nothing, so the field keeps its seed |
| `SectionPresent` | the pointer is materialised even where nothing was written beneath it | the field is zeroed, and the plane counts as having spoken |
| `SectionNull` | the pointer is nil | the field is zeroed, and the plane counts as having spoken |

A slice and a map collapse the last two rows, because nil and empty are one Go value there.
What separates them from `SectionAbsent` is not the value they leave but what they say to the section above: a null and a present-and-empty container are the plane speaking, and absence is not, so a `LoadOver` clears the field for the first two and keeps the seed for the third.

The `SectionPresent` row on the left is the whole reason a present-but-empty section survives a reload.
Go can express empty-but-present, a non-nil `*Options` whose every field is omitted, and without a driver that can both write and report that state the round trip turns present-empty into absence.

A composite is probed only where `Children` came back empty, so a populated container never reaches this table at all.
A source with no `Enumerator` is a special case: a `SectionNull` there is a complete answer it can still give, and anything else is the refusal naming the field and the source.

`Prober` is optional for the same reason `Enumerator` is: a plane that cannot list often cannot answer this either.
A source implementing neither loads the leaves the type determines and nothing else, which is a documented property of your plane rather than a surprise.

**A flat plane can usually still answer**, by inference rather than by storage.
`driver/env` and `driver/kv` both answer `SectionPresent` when anything is held under the container's prefix and `SectionAbsent` otherwise, and neither ever answers `SectionNull`, because neither plane has a null.

### `Enumerator`

This is how `Load` discovers the addresses that come from the **value** rather than from the type: a map's keys, a sequence's length.

It could not be required, because a Vault kv-v2 `LIST` is a separate ACL capability and a token with read and no list is ordinary.
It could not be omitted, because a map could then be loaded from no plane at all.

So the two directions cover different address sets, and that is a documented property of your driver rather than a surprise:

- **`Dump` reaches every address, always**, since the value is in hand.
- **`Load` reaches the static addresses always**, and the dynamic ones only through `Enumerator`.

Loading a map-typed field from a non-enumerating source is an error naming the field and the source, never a silently empty map.
An array loads either way, because an array's element addresses are known from the type.

**`Children` is asked only about a `CompositeAddr`.**
An array is a **section** and is never enumerated: its children are compiled from the type, so there is no call that could mint one, and a `Name` child appearing under an array is not a bug to fix but a shape that no longer exists ([ADR-0016](../adr/0016-the-sealed-address-model.md)).
The same signature makes it impossible to be asked to list a leaf.

**`Children` returns segments and not addresses.**

```go
func (r reader) Children(_ context.Context, addr ferry.CompositeAddr) ([]ferry.Segment, error) {
	// ferry.NameSegment("k") for a mapping member, ferry.IndexSegment(0) for a position
}
```

You say how the plane spells its members and **the schema types the child**, so you never construct an address at all and you cannot answer about one you were not asked about.
The kind is still yours: a `NameSegment` says the plane holds a mapping member and an `IndexSegment` says it holds a position, and core refuses a name under a sequence and a position under a mapping, naming the segment.
Order is the plane's own, and a plane with no defined order documents the order it mints in.
If you sort, sort segment-wise: sorting the rendering gives `0 1 10 11 2` for twelve indices, and that is a conformance case.

**At a slice or a map, core asks you for children before it probes the container's own address**, and probes only where you returned nothing.
A member list is the whole answer wherever there is one, and the probe is only there to tell an absent container from an explicitly null one.

> **Retracted, and worth saying why.**
> This guide used to say that being asked for children was the only signal core gave you that an address was a dynamic container, because "an address arrives at `Get` carrying no kind and no arity, so a container address and a leaf address are the same call".
> That was true and it is exactly what ADR-0016 removed.
> The signal now arrives at `Bind`, in the address set, before any I/O and for every container rather than only the dynamic ones, and the calls are different methods over different types.
> It also used to say that `Children` was the only place you could refuse an overlap during the walk, "because case 3 forbids failing at a container `Get`".
> There is no container `Get`, and a driver refusing a repeated plane key at a **leaf** now has a home for that refusal.

**If your plane can spell one address two ways, refuse the overlap where the two spellings meet.**
A multimap plane such as a query string can reach `/tags#0` by a repeated name and by an index-suffixed one, and a request carrying both reaches it twice.
Report each child segment at most once, and where two plane keys reach one child, fail rather than pick a winner: `ErrPlane`, at the composite's address, with a sentinel of your own beside it ([ADR-0015](../adr/0015-two-spellings-of-one-address.md)).
Only an overlap is a clash, so `?tags=a&tags=b&tags.2=z` extends the sequence and loads as three elements.
The conformance suite does not check it: all five candidate policies scored zero failures (ADR-0015), so this one is on you.

### `Ensurer`

This is what a dump says at a container's own address, and the mirror of `Prober`.

```go
type Ensurer interface {
	Ensure(ctx context.Context, addr ferry.Container, p ferry.Presence) error
}
```

Core calls it where the value has nothing to say **beneath** a container:

- a nil pointer, an empty slice and an empty map write `ferry.PresenceNull`;
- a non-nil pointer whose subtree emitted no write at all writes `ferry.PresencePresent`, which is how present-and-empty gets stored.

`ferry.PresenceAbsent` never arrives, because an address ferry omits gets no call at all rather than a call saying nothing.
That is the same rule that keeps `KindAbsent` off the write side entirely.

**Implementing nothing is a legitimate answer and it is the loud one.**
A plane with no spelling for a container implements no `Ensurer`, and core refuses such a dump with `ErrPlane`, naming the address and your plane.
`driver/kv` takes exactly that position and says so in its own source: storing a zero-length value instead would collapse two observations into one, which is worse than a refusal a caller can read.

The alternatives were weighed and both lose ([ADR-0016](../adr/0016-the-sealed-address-model.md)).
Accepting the degradation makes present-empty become absent on reload, which is the silent divergence the whole address model exists to remove; refusing everywhere makes a legal Go value undumpable on planes that could carry it perfectly.
The rule taken is the loud refusal scoped to exactly the planes that cannot spell the value.

## Links

A plane whose grammar has aliases, a YAML anchor or a filesystem symlink, has two ways to serve one, and both are legitimate.

**Resolve it yourself and report nothing.**
That is what `driver/yaml` does: it follows an anchor on the read side and writes through it on the write side, and core never learns that a link was there.
Everything about the alias, including the cycle discipline and the write rule, is yours.

**Or report one hop and let core follow the chain.**
`Prober` answers `ferry.SectionAt(target)` and `Get` returns a `*ferry.LeafRedirect`, and core keeps the set of addresses already asked, follows the chain, and refuses a cycle naming the address it closed through.

```go
func (r reader) Probe(_ context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	if to, linked := r.linkAt(addr); linked {
		return ferry.SectionAt(to), nil
	}
	...
}

func (r reader) Get(_ context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	if to, linked := r.linkAt(addr); linked {
		return ferry.Value{}, &ferry.LeafRedirect{Target: to}
	}
	...
}
```

The leaf arm is an **error that is not a failure**, in the shape `fs.SkipDir` has, and it is an error rather than a seventh `VKind` on purpose: a value stays the six kinds it always was, and no codec has to handle one meaning "look over there".
Match it with `errors.As`.

Four rules bind you, and three of them are enforced.

**Report one hop and stop.**
Following the chain yourself is the thing this exists to save you from, and doing both is not wrong so much as pointless.

**A section may only name a section, and a composite only a composite.**
What is under a section comes from the type and what is under a composite comes from the value, so a link across them names a place its own members could not be. Core refuses it.

**The target is an address you were handed.**
Nothing outside ferry mints one, so a link whose target this schema does not name cannot be reported at all, and that case stays yours: resolve it internally, or refuse it in your own words. Reporting a target from some other schema's set is refused, naming whose job the case is.

**A section link resolves presence, and nothing else.**
The values beneath a linked section are still read at their own addresses, so if your alias moves the children too, report a `LeafRedirect` per leaf or resolve them yourself.

### The write side of a link is divergence

Two aliases of one target, and the caller mutates one of them.
The dump has to decide, and this is the one rule core cannot enforce for you, because it turns on knowing that a section was *reached* through a link, and core keeps nothing between a load and a dump.

> An unchanged section keeps its link.
> A diverged one materialises at its own address, and the target and every other alias are untouched.

Writing through the link instead propagates a change to every alias, which is what a YAML anchor means and is dangerous precisely because it is silent; refusing a diverged dump outright makes a legal Go mutation undumpable.
The rule taken is the generalisation of the memo rule the spelling seam already uses: what the plane said is preserved until the value says otherwise ([ADR-0016](../adr/0016-the-sealed-address-model.md)).

No first-party driver reports a link today, so nothing here resolves one in production, and `ferrytest` asserts nothing about it.
`driver/yaml`'s anchors are the first option above and are unaffected: its write-through behaviour is its own, settled separately, and this rule governs a section core resolved rather than one a driver never reported.

## What `Get` and `Set` must and must not do

### `Get`

**A non-nil error must reach the caller as an error and never as an `Absent`.**
That is conformance case 4, and it exists because a real YAML provider was found discarding parse errors and returning an empty result, and core committed the mirror of it, and neither was visible for four prototypes.

Absence is a **kind of the value** and not a second return value: report it as the zero `ferry.Value`, whose kind is `KindAbsent`.

**There is no container arm.**
`Get` takes a `ferry.LeafAddr`, so the question "what do you hold at this container's own address" cannot be asked here at all and is `Prober`'s.
That deletes a whole class of driver bug rather than documenting it, and the process environment is the worked case: a struct field named `home` is a section, so `$HOME` can no longer be read as its value.

**A plane holding a container where the schema says leaf is a refusal, not an `Absent`.**
Answering absence there is how a YAML mapping at a `string` field becomes the Go zero value with a nil error, which is silent loss.
Refuse it, naming the address and what your plane actually holds.

**On a multimap plane, a key the plane holds more than one value at is that same refusal.**
`?tags=a&tags=b` is a sequence, and a `ferry.LeafAddr` holds one value, so reading it into the field would have to discard one.
Refuse it here, with the count and without any of the values, and let core attach the address ([ADR-0016](../adr/0016-the-sealed-address-model.md)).
You need no table to know this: the parameter type is the classification, so the same request is two elements at a `[]string` and a refusal at a `string`, and neither reading is a rule you wrote.
`driver/http` used to defer that refusal to `Close`, because an address arrived at `Get` carrying no kind and the driver had to answer `Absent` and wait to see whether anything enumerated the name; making the refusal here removed the deferral, the bookkeeping it needed, and the double report a `required` field got for one mistake.

### `Set`

**`Set` is never called with an `Absent`.**
`Absent` is a reader-side kind, and an omitted address is one that gets no `Set` call at all rather than one that gets a `Set` of nothing.
So an omission is not a deletion: a replacing sink and a patching sink read one dump differently and both are correct.

**`Set` takes a `ferry.LeafAddr` too**, so a `Null` at a container's own address never arrives here.
A composite with no elements still writes a null at its own address, exactly as it did; it is spelled `Ensure(addr, ferry.PresenceNull)` because a plane that cannot spell one should refuse rather than receive a write it will mis-store.

**A plane that is writable in principle but not right now refuses inside the `OpenWriterFunc`**, with an error wrapping `ferry.ErrReadOnly`.
Not at `Bind`, which does no I/O and so cannot know, and not at the first `Set`, which has already half-written the plane.

## Declaring the kinds your plane carries

The suite cannot simply demand that every plane pass every proof.
A flattening plane reports `String` for everything and has no null, so a nil composite is a value it cannot represent.
Without a declaration the suite has two options and both are wrong: fail every flat driver, or skip the check and let a nil pointer silently become a zero value.

So you declare it, as `ferrytest.Plane.Kinds`:

```go
Kinds: []ferry.VKind{
	ferry.KindAbsent, ferry.KindBool, ferry.KindNumber, ferry.KindString, ferry.KindBytes,
},
```

**`Kinds` is what the plane carries end to end, not what your `Get` returns.**
A flat store writes a `Number` as its own text and reads it back as a `String`, and every Go leaf takes a `String` and parses it, so a proof carrying a `Number` round-trips exactly - and the plane declares `Number`.

**It is an obligation in both directions.**
Case 1 has two halves and neither stands alone:

- a kind you **do** declare must round-trip every value of it;
- a kind you **do not** declare must be **refused loudly** on the way in.

Declaring one you cannot carry makes a proof fail.
Omitting one you can carry silently converts that proof into a refusal check and stops proving it.
**A plane that declares a kind and then refuses a value of it is a failure and not a refusal.**

Work out your list from what your plane can actually do rather than from a template.
A flattening plane with no null measures at 11 of 11 core types with 3 values refused, and the three are the nil and empty composites (ADR-0005), so its honest declaration is `Absent, Bool, Number, String, Bytes` and no `Null`.
A list of `Absent, String, Bytes` would demand a store that refuses every bool and every port number.

### `Except`, when your format carries a kind but not every value of it

```go
Except func(v ferry.Value) bool
```

`Kinds` is kind-granular and a format need not be.
`driver/yaml` is the instance: a Go string is a byte sequence and a YAML string is a Unicode one, so the plane carries `KindString` and cannot carry the one value of it that is not valid UTF-8.
Neither half of the declaration can say that.
Dropping `KindString` would disclaim every ordinary string the plane carries perfectly, and declaring it and then refusing a value of it is a failure.

It is a **predicate** rather than a list of values, because what is being declared is a property of the format.

**An excepted value buys a refusal you have to actually make, not a skip.**
The suite routes it to case 1's refusal half, which is exactly where a kind you never declared goes.
Reach for it only when the limit is a property of the format, and record it in the ADR that owns the type set.

## Errors

Core's six sentinels are `ErrSchema`, `ErrMissing`, `ErrValue`, `ErrPlane`, `ErrDriver` and `ErrReadOnly`.

A driver returns **any** error.
Core wraps it and supplies the address, the moment and the `ErrDriver` marker, and you can change none of them.
Core supplies the default class for that moment **unless** your error already carries a ferry class sentinel, in which case core keeps it.

So:

- **Wrap your own sentinel with `%w`** so a caller's `errors.Is` reaches it through ferry's wrapper.
  The conformance suite asserts that reachability, and `errors.AsType[*yourpkg.APIError]` works through ferry unchanged.
- **State a class only where you have an opinion core cannot form.**
  A malformed document is `ErrValue`, not `ErrPlane`, because it is the operator's file.
  A plane that is writable in principle but not now is `ErrReadOnly`.
- **Use `ferry.ErrorAt(addr, err)` to attach an address.**
  It attaches and never classifies, so it is inert until core wraps it, and where core already knows the address, core's wins.
  It returns `error` and not `*ferry.Error`, which is what makes `return ferry.ErrorAt(a, f())` safe to write.
- **Name every address you disliked, not just the first.**
  A refusal over a whole address set may carry one `ErrorAt` per member, joined together, and core reports one failure per address with its own cause and its own class.
  What you return without an address on it stays whole and is reported as one failure with none.
- **Wrap it in a sentence of your own if you have one.**
  `fmt.Errorf("flushing the write buffer: %w", ferry.ErrorAt(addr, err))` reports one failure at `addr` whose text and whose whole chain are both intact, however many times you wrapped it.
  A sentence around a *join* of them is dropped instead, because it describes all of them and the failures are reported one address at a time.
  The result of `ErrorAt` renders as the error it carries and never as the address, so printing one yourself rather than returning it shows no address: the address is data for core, and what names it is the ferry error a caller gets back.

Do not put a value the plane supplied into an error message.
ferry's own text never does, which is what stops a plane holding secrets from leaking into a log through ferry.
An address is structure and may be named; the value stored at it is the plane's.

See [errors](errors.md) for the caller's side of all of this.

## A worked example

A flat key-value plane over a Go map, with a staging writer.
Every case in the conformance suite passes against it.

```go
// Package flat is a worked example driver: a flat key-value plane over a Go map.
package flat

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/onhotpath/ferry"
)

// Store is the backend. A real driver would hold a client instead.
type Store map[string]string

// Source is the read half.
type Source struct{ store Store }

// NewSource builds a source over store.
func NewSource(store Store) Source { return Source{store: store} }

// Bind checks every address before any I/O, and returns the function that opens a reader.
func (s Source) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	keys, err := ferry.NewKeys(addrs, "flat", key)
	if err != nil {
		return nil, err
	}

	store := s.store

	// Open once per open, never once per Bind: a key function that outlives
	// its open retains what it minted and refuses a later legal write.
	return func(context.Context) (ferry.Reader, error) {
		return reader{store: store, key: keys.Open()}, nil
	}, nil
}

// prefix is the text every key under a container's own key begins with. This
// plane joins with an underscore, so a container at "db" holds "db_host".
func prefix(k string) string { return k + "_" }

// key renders one address as a plane key: segments joined with an underscore,
// with the bytes a segment may carry and a key may not folded into one.
//
// A driver transforms segment text rather than rejecting it, so an ordinary
// ferry:"feature-flags" is loadable here. What it refuses is what no
// transformation can rescue: this store is line-oriented, so a key can hold
// neither a newline nor the separator between a key and its value.
func key(addr ferry.Path) (string, error) {
	var b strings.Builder

	for seg := range addr.Segments() {
		if strings.ContainsAny(seg.Text(), "=\n") {
			return "", fmt.Errorf("%w: a key here cannot contain = or a newline, and %q does",
				ferry.ErrPlane, seg.Text())
		}

		if b.Len() > 0 {
			b.WriteByte('_')
		}

		b.WriteString(strings.NewReplacer(".", "_", "-", "_").Replace(seg.Text()))
	}

	return b.String(), nil
}

type reader struct {
	store Store
	key   ferry.KeyFunc
}

// Get answers with what the store holds, as a String, or with Absent.
//
// It is asked only about a leaf, so a container's own key is never read as a
// value: what is asked there is Probe.
func (r reader) Get(_ context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	k, err := r.key(addr.Path())
	if err != nil {
		return ferry.Value{}, err
	}

	text, ok := r.store[k]
	if !ok {
		return ferry.Value{}, nil
	}

	return ferry.String(text), nil
}

// Probe answers whether the store holds anything under a container's own key.
//
// A flat store has no null, so a container here is present or absent and never
// null: there is nothing it could store that would mean "there and empty" and
// not also mean "empty text".
func (r reader) Probe(_ context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	k, err := r.key(addr.Path())
	if err != nil {
		return ferry.SectionInfo{}, err
	}

	for held := range r.store {
		if strings.HasPrefix(held, prefix(k)) {
			return ferry.SectionPresent, nil
		}
	}

	return ferry.SectionAbsent, nil
}

// Sink is the write half, a second type over the same store.
type Sink struct{ store Store }

// NewSink builds a sink over store.
func NewSink(store Store) Sink { return Sink{store: store} }

// Bind runs the same two checks and returns the function that opens a writer.
func (s Sink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	keys, err := ferry.NewKeys(addrs, "flat", key)
	if err != nil {
		return nil, err
	}

	store := s.store

	return func(context.Context) (ferry.Writer, error) {
		return &writer{store: store, key: keys.Open(), staged: Store{}}, nil
	}, nil
}

type writer struct {
	store  Store
	staged Store
	key    ferry.KeyFunc
}

// Set stages one leaf.
func (w *writer) Set(_ context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	k, err := w.key(addr.Path())
	if err != nil {
		return err
	}

	text, err := text(v)
	if err != nil {
		return err
	}

	w.staged[k] = text

	return nil
}

// Commit publishes the staged writes, and runs only when the walk succeeded.
func (w *writer) Commit(context.Context) error {
	for k, v := range w.staged {
		w.store[k] = v
	}

	return nil
}

// text is what this plane can hold. Everything is a string, and there is no null.
//
// A null at a container's own address does not arrive here at all: core asks
// ferry.Ensurer for that, this writer implements none, and core refuses the
// dump at the address and names this plane.
func text(v ferry.Value) (string, error) {
	switch v.Kind() {
	case ferry.KindString:
		return v.AsString()
	case ferry.KindNumber:
		return v.AsNumber()
	case ferry.KindBool:
		b, err := v.AsBool()

		return strconv.FormatBool(b), err
	case ferry.KindBytes:
		b, err := v.AsBytes()

		return string(b), err
	default:
		return "", fmt.Errorf("%w: this plane has no null, so it cannot hold %s", ferry.ErrValue, v.Kind())
	}
}
```

The reader implements `Prober` and not `Enumerator`, and the writer implements `Committer` and neither `Releaser` nor `Ensurer`.

Each of those absences is a real cost stated rather than hidden.
No `Enumerator` means a map-typed or slice-typed field cannot be loaded from this plane, though an array can, because an array's element addresses come from the type.
No `Releaser` because a Go map holds no resource, and a `Close` returning nil is indistinguishable in the source from a rollback somebody forgot.
No `Ensurer` because this store has no way to say that a container is there and holds nothing, so dumping a nil optional section through it is a refusal naming the address rather than a value quietly stored as empty text.

`Prober` is worth having even here.
Without it a nil `*Options` seeded by the caller could not be told apart from one the plane never mentioned, and with it the prefix scan answers from the same store the reader already holds.

Using it:

```go
store := flat.Store{"name": "checkout", "db_port": "5432"}

cfg, err := ferry.Load[Config](ctx, flat.NewSource(store))
// {Name:checkout DB:{Port:5432}}
```

Both refusals, before any I/O and at mint time:

```
type bad struct {
	A string `ferry:"db_port"`
	B DB     `ferry:"db"`
}
ferry: /db_port: flat renders this address and /db/port to one plane key, "db_port",
       so one of the two would be lost

ferry.Dump(ctx, cfg{M: map[string]string{"a=b": "x"}}, flat.NewSink(store))
ferry: /m/a=b: the driver failed: plane error: a key here cannot contain = or a newline,
       and "a=b" does
```

## Proving it: one call

```go
func TestConformance(t *testing.T) {
	ferrytest.Driver(t, ferrytest.Plane{
		Name: "flat",
		Kinds: []ferry.VKind{
			ferry.KindAbsent, ferry.KindBool, ferry.KindNumber, ferry.KindString, ferry.KindBytes,
		},
		Open: func() ferrytest.Instance {
			store := flat.Store{}

			return ferrytest.Instance{
				Source:   flat.NewSource(store),
				Sink:     flat.NewSink(store),
				Contents: func() ([]byte, error) { return contents(store), nil },
			}
		},
		Golden: []ferrytest.Artefact{
			ferrytest.Golden(struct {
				Port int    `ferry:"port"`
				Name string `ferry:"name"`
			}{8080, "checkout"}, "name=checkout\nport=8080\n"),
		},
	})
}
```

That is the whole file.
`Driver` is thirteen cases and it calls `RoundTrip` rather than restating it, because a suite you can partially adopt is a suite that measures nothing.

### The four fields

**`Name`** labels the plane in a report.
It is a label and never a key: two planes with one name are a confusing report and not a collision.

**`Kinds`**, and optionally `Except`, as above.

**`Open` must mint a fresh, empty plane on every call.**
A hoisted plane makes cases 1 to 10 run against a shared destination, which is the defect `Instance` exists to prevent, in the one package that publishes the fresh-destination rule.
A struct minted inside `Open` has nowhere to hoist to, so the honest spelling is the only spelling.

`Instance.Sink` is nil for a plane with no honest dump, and the suite then runs the read-side cases only.
`Instance.Contents` yields the plane's raw contents exactly as it holds them, read after the dump has finished and after any `Committer` has committed.
A file-backed plane's whole implementation of it is `func() ([]byte, error) { return os.ReadFile(p) }`.

`Instance.InContext` is what a driver whose plane instance is obtained freshly per load fills in, and it is nil for every other plane.
It puts this instance's contents into a context, which is where such a driver takes its plane from ([ADR-0012](../adr/0012-the-caller-held-binding.md)):

```go
Open: func() ferrytest.Instance {
	v := url.Values{}

	return ferrytest.Instance{
		Source:    ferryhttp.NewQuerySource(),
		InContext: func(ctx context.Context) context.Context { return ferryhttp.WithQuery(ctx, v) },
	}
},
```

Set it and every case runs its own I/O under the context it returns, so the whole suite reaches your plane the way a request would, and case 10 stops being skipped.
Close over contents minted inside `Open`, never over contents hoisted out of it, and supply the same contents on every call: one case opens the plane more than once, and each open has to find what the last one wrote.
A sink whose plane is per request fills it in the same way, from the same function, because an instance is both halves over one set of contents.

**`Golden`** pins your own spelling of a fixed value, and it is empty for a plane with no serialization format.
It lives on the `Plane` rather than being a parameter of the suite, because the spelling is your statement about yourself: ferry refuses to constrain indentation or key order, so what is pinned has to be your choice.

A `Golden` row buys the one thing a round trip structurally cannot see.
A round trip tests a function against its own inverse, and a spelling is a choice of function, so changing both halves together is invisible to any test that only composes them.
`driver/yaml`'s `Bytes` spelling changed from base64 to hex round-trips perfectly and turns every stored file into garbage.

**A change to a golden row is a major version of your module**, and it ships with a written migration.
See [plane compatibility](compatibility.md).

`Want` is one string, which puts an obligation on a plane holding more than one storage unit: what it renders for this comparison must be **deterministic and injective over stores**.
That is the same obligation your key function already carries.

### The thirteen cases

1. Every proof the plane can express round-trips, and every kind it declared it cannot carry is refused loudly.
2. `Bind` succeeds against an unreachable plane, and the refusal lands inside the open.
3. `Probe` at a container address answers what the plane holds there, with a nil error: a populated composite is present, and a nil composite, an empty composite and a nil optional section are null.
4. `Get` returning a non-nil error reaches the caller as an error and never as `Absent`.
5. `Children` at a composite address returns the element segments, kinded.
6. `Commit` runs only on success, `Close` always runs, and a `Close` failure appears in the reported error set.
7. A driver producing a plane key refuses a non-injective key function over the address set, before any I/O, naming both addresses.
8. A key function retains nothing across opens, asserted on the **write** side.
9. A sink accepts a dynamic address its static table never held.
10. A driver reading its plane from the context refuses at open when it is absent, with `ErrPlane`.
11. A golden artefact: a fixed value, dumped, compared against fixed expected plane contents.
12. A sink writes a null at a container address, and that address was in the set its `Bind` received **at its kind**: a composite for a nil or empty composite, a section for a nil optional section.
13. A plane key belonging to no address of the schema says nothing about a container whose key space it shares: `Probe` at a section beside it answers absent.

Cases 8 and 9 reach a value-minted address by dumping a one-entry map rather than by calling `Set` directly, because the address kinds are sealed and only the compiler mints one.
For the same reason the suite no longer builds an `AddressSet` by hand: it compiles a fixture type and captures the set core hands your own `Bind`, so it can never hand you an address the compiler would not have minted ([ADR-0014](../adr/0014-what-ferrytest-exports.md), amended).

A case that cannot apply to your plane is skipped and says so:

```
case 3 skipped: the plane's reader does not probe, which is optional for the same reason enumeration is
case 5 skipped: the plane's reader does not enumerate, which ADR-0004 makes optional
case 6 skipped: the plane's writer holds no resource, so it implements no Close
case 10 skipped: the plane puts nothing in a context, so it does not take its plane per request
case 12 skipped: the plane declares no null; case 1 is where its refusal of one is asserted
case 13 skipped: the plane's reader does not probe, which is optional for the same reason enumeration is
```

### A new case does not break you, exactly

The suites may gain cases in a minor release where the apparatus may not, and the difference is not semver's to see: adding a case changes no signature, no type and no exported name, `apidiff` reports nothing, and your CI goes red.

That is affordable for exactly one reason, and it is why every case cites the ADR sentence it executes:

> A new conformance case does not break a driver.
> It reports that the driver was already broken, against a rule an ADR had already landed.

A case asserting a rule no ADR states is not a case, it is a new rule, and it needs the ADR first.

## Shipping it

A driver is a module of its own, versioned independently of core, so your users take your representation changes on your schedule and not on core's.

Two rules from [ADR-0002](../adr/0002-core-and-sub-modules.md) that apply outside this repository too:

- **The zero-dependency rule is core's alone.**
  Your driver may have whatever dependencies it genuinely needs.
- **`encoding/json/v2` and `encoding/json/jsontext` are denied in every ferry module**, and if you do import v2, the option set you construct is pinned rather than inherited.
  See [the pinned option set](compatibility.md#the-pinned-encodingjsonv2-option-set).

Give your module a README that shows a load and a save, and say what your plane cannot do.
The three in this repository are written that way and are the house pattern.
