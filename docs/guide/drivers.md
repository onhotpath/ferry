# Writing a driver

A ferry driver is two methods and, if your plane needs them, up to three more.
This is the whole contract, plus the one call that proves you got it right.

The decision records are [ADR-0004](../adr/0004-source-and-sink.md) for the contract, [ADR-0003](../adr/0003-how-a-leaf-addresses-a-plane.md) for addressing, and [ADR-0014](../adr/0014-what-ferrytest-exports.md) for the conformance suite.

Three drivers ship in this repository and are worth reading beside this page: [`driver/env`](../../driver/env/), a read-only flat plane; [`driver/kv`](../../driver/kv/), a flat plane in both directions with a caller-supplied client; and [`driver/yaml`](../../driver/yaml/), a tree plane that edits a file in place.
`ferrytest.MemPlane` in [`ferrytest/memplane.go`](../../ferrytest/memplane.go) is a complete, working driver of about the size yours will be, and is the shortest thing to read first.

## The vocabulary

- A **plane** is whatever holds your data: a file, a KV store, a process environment, an HTTP query string.
- An **address** is a `ferry.Path`: an ordered sequence of segments, each carrying a kind (`Name` or `Index`) and a text.
  `/db/host` and `/tags#0` are addresses.
- A **plane key** is what your plane calls that address: `DB_HOST`, `db/host`, or nothing at all if your plane is a tree and you walk the segments.
- A **`ferry.Value`** is what crosses the boundary: a kind and a text, in a comparable 24-byte struct.
  The six kinds are `Absent`, `Null`, `Bool`, `Number`, `String` and `Bytes`.

**Core never produces a plane key**, because a separator is plane knowledge.
Flattening is the driver's, always.

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
	Get(ctx context.Context, addr ferry.Path) (ferry.Value, error)
}

type Writer interface {
	Set(ctx context.Context, addr ferry.Path, v ferry.Value) error
}
```

A read-only driver implements **two methods in total**: one `Bind` and one `Get`.

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

`OpenFunc` may be called many times against one `Bind`.

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

### What the address set contains

Every leaf address the type determines, **plus every container address**, and never a wildcard shape.

So you are handed only addresses you can fetch, write, name and check.
A container address is in the set so that a batch fetch, a legality check and an injectivity check reach it before any I/O rather than at the first `Get`.

A driver that treats its precomputed table as a **closed** set refuses a legal write.
That is why core hands out a key function rather than a map.

## The three optional interfaces

All three are discovered by type assertion on your `Reader` or `Writer`, and none is required.

```go
type Releaser interface {
	Close() error
}

type Committer interface {
	Commit(ctx context.Context) error
}

type Enumerator interface {
	Children(ctx context.Context, prefix ferry.Path) ([]ferry.Path, error)
}
```

| implement | if |
| --- | --- |
| `Releaser` | your reader or writer holds a resource |
| `Committer` | your writes are not durable until the end of a successful walk |
| `Enumerator` | your plane can list what is under an address |

Of the three drivers here, `driver/env` implements one, `driver/kv` two, and `driver/yaml` all three.
The memory plane in `ferrytest` implements one.

That spread is the case for making them optional rather than methods on `Reader` and `Writer`: a required `Close` would be `return nil` boilerplate in four of ADR-0004's six sinks, and in the source that is indistinguishable from a driver that should have rolled back and did not.

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

### `Enumerator`

This is how `Load` discovers the addresses that come from the **value** rather than from the type: a map's keys, a sequence's length.

It could not be required, because a Vault kv-v2 `LIST` is a separate ACL capability and a token with read and no list is ordinary.
It could not be omitted, because a map could then be loaded from no plane at all.

So the two directions cover different address sets, and that is a documented property of your driver rather than a surprise:

- **`Dump` reaches every address, always**, since the value is in hand.
- **`Load` reaches the static addresses always**, and the dynamic ones only through `Enumerator`.

Loading a map-typed field from a non-enumerating source is an error naming the field and the source, never a silently empty map.
An array loads either way, because an array's element addresses are known from the type.

**`Children` returns addresses and not names**, deliberately.
An address carries its segment kind, so the plane says whether the container is a mapping or a sequence rather than the caller guessing it from base-10 text.
Sort them segment-wise: sorting the rendering gives `0 1 10 11 2` for twelve indices, and that is a conformance case.

A `Null` at a container's own address is a complete answer and needs no enumeration.

## What `Get` and `Set` must and must not do

### `Get`

**A non-nil error must reach the caller as an error and never as an `Absent`.**
That is conformance case 4, and it exists because a real YAML provider was found discarding parse errors and returning an empty result, and core committed the mirror of it, and neither was visible for four prototypes.

Absence is a **kind of the value** and not a second return value: report it as the zero `ferry.Value`, whose kind is `KindAbsent`.

At a **container** address, answer `Absent` or `Null` and nothing else.
A composite is read one element at a time, so there is no group value for the container itself to hold.
Which of the two is the plane's own business, and the rule is that **a driver reports what the plane holds**: an address the plane does not hold is `Absent`, and a present address carrying the plane's own null is `Null`.
At an explicit `tags: []` node in a hand-authored document a tree driver answers `Null`, the same as at `tags: null` (ADR-0014, amended).

### `Set`

**`Set` is never called with an `Absent`.**
`Absent` is a reader-side kind, and an omitted address is one that gets no `Set` call at all rather than one that gets a `Set` of nothing.
So an omission is not a deletion: a replacing sink and a patching sink read one dump differently and both are correct.

A composite with no elements is written as `Null` at its own address, which is a value the plane holds and a different observation entirely.

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
func (r reader) Get(_ context.Context, addr ferry.Path) (ferry.Value, error) {
	k, err := r.key(addr)
	if err != nil {
		return ferry.Value{}, err
	}

	text, ok := r.store[k]
	if !ok {
		return ferry.Value{}, nil
	}

	return ferry.String(text), nil
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

// Set stages one address.
func (w *writer) Set(_ context.Context, addr ferry.Path, v ferry.Value) error {
	k, err := w.key(addr)
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

It implements `Committer` and not `Releaser` - a Go map holds no resource - and not `Enumerator`, which is a real cost stated rather than hidden: a map-typed or slice-typed field cannot be loaded from it, and an array can.

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
`Driver` is twelve cases and it calls `RoundTrip` rather than restating it, because a suite you can partially adopt is a suite that measures nothing.

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

### The twelve cases

1. Every proof the plane can express round-trips, and every kind it declared it cannot carry is refused loudly.
2. `Bind` succeeds against an unreachable plane, and the refusal lands inside the open.
3. `Get` at a container address answers `Absent` or `Null`, per what the plane holds, with a nil error.
4. `Get` returning a non-nil error reaches the caller as an error and never as `Absent`.
5. `Children` at those addresses returns the element addresses, kinded.
6. `Commit` runs only on success, `Close` always runs, and a `Close` failure appears in the reported error set.
7. A driver producing a plane key refuses a non-injective key function over the address set, before any I/O, naming both addresses.
8. A key function retains nothing across opens, asserted on the **write** side.
9. A sink accepts a dynamic address its static table never held.
10. A driver reading its plane from the context refuses at open when it is absent, with `ErrPlane`.
11. A golden artefact: a fixed value, dumped, compared against fixed expected plane contents.
12. A sink accepts `Set` of a `Null` at a container address, and that address was in the set its `Bind` received.

A case that cannot apply to your plane is skipped and says so:

```
case 5 skipped: the plane's reader does not enumerate, which ADR-0004 makes optional
case 6 skipped: the plane's writer holds no resource, so it implements no Close
case 10 skipped: the plane puts nothing in a context, so it does not take its plane per request
case 12 skipped: the plane declares no null; case 1 is where its refusal of one is asserted
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
