# Errors

What a failed call carries, how to match on it, and why the message text is not API.

The decision record is [ADR-0011](../adr/0011-the-error-model.md).

## ferry reports every failure at once

A failed call carries a **set** rather than the first thing that went wrong.

> ferry reports every failure that is not a consequence of another failure it is already reporting.

```go
type DB struct {
	Host string `ferry:"host,required"`
	Port int    `ferry:"port,default=5432"`
}

type Config struct {
	Name string `ferry:"name,required"`
	DB   DB     `ferry:"db"`
}
```

loaded from a plane holding only `/db/port = "not-a-number"`:

```
%v    ferry: 3 errors: /db/host, /db/port, /name

%+v   ferry: 3 errors:
        /db/host: required, and the plane holds nothing at this address
        /db/port: the plane's value is not a valid int
        /name: required, and the plane holds nothing at this address
```

`Error()` is one line, and `%+v` is the report.
Elements are sorted at construction, on the moment, then the location, then the message, so repeated runs of an identical failure print an identical report (ADR-0011).

There is no `StopOnFirstError`.
"These six keys are unset" and "these two hold garbage" are different messages for different people, and both are worth having in one run.

## Matching

Two axes, one mechanism, and no enum:

```go
for _, e := range ferry.Elements(err) {
	if !errors.Is(e, ferry.ErrMissing) {
		continue
	}

	if fe, ok := errors.AsType[*ferry.Error](e); ok {
		missing = append(missing, fe.Address())
	}
}
```

`ferry.Elements` is defined over any `error`, not only over ferry's aggregate.
One failure returns a one-element slice, so your loop reads the same whether one field failed or forty, and `Elements(nil)` is nil.
It sees through an `fmt.Errorf("loading config: %w", err)` wrapper.

`errors.Is` on the aggregate keeps `errors.Join`'s meaning: it answers "at least one element is of this class".

### The six sentinels

| sentinel | what happened | what it means to do next |
| --- | --- | --- |
| `ErrSchema` | provable from the type plus the registry, with no plane in sight | fix the struct; `ferry.Compile[T]()` catches these before any I/O |
| `ErrMissing` | the plane was silent at an address `required` names | supply the value |
| `ErrValue` | the plane spoke and what it said does not fit the type | fix the stored value |
| `ErrPlane` | ferry could not talk to the plane, or a driver refused the address set | fix the plane or the connection |
| `ErrReadOnly` | the plane is writable in principle but not now | fix the permission |
| `ErrDriver` | **provenance**, and it crosses the four above | the failure came from driver code rather than from core |

Cancellation gets no ferry class: `errors.Is(err, context.Canceled)` is the match, and a ferry class for it would be a second spelling of a standard library one.

There is no `Transient` marker, no `Kind` enum and no `KindOf`.

### The one accessor

```go
type Error struct{ /* no exported fields */ }

func (*Error) Error() string
func (*Error) Unwrap() error
func (*Error) Format(fmt.State, rune)
func (*Error) Address() Path
```

**The name is exported so `errors.AsType` works; no field is, so the struct can grow.**
There is no concrete type to switch on and no accessor for the class, because the class is `errors.Is`.

`Address()` is a method and the only spelling.
There is no free `ferry.Address(err)` and none is planned: it would have to return the zero `Path` for anything that is not a `*ferry.Error`, and one value meaning both "no address" and "not ferry's error" is a worse ambiguity than the type step it saves.

Note that `Error` is on the pointer receiver, so `errors.AsType[*ferry.Error]` is the spelling and `ferry.Error` alone does not implement `error`.

At schema compile the address is the **Go field path**, because a field with no tag has no address and the whole error is that it never named one.
Everywhere else it is the plane address.

### A driver's own error stays reachable

Every element of an aggregate ferry constructs is a `*ferry.Error`.
Core is the only party that mints one, and a driver's error is wrapped rather than admitted, so a foreign error is always a **cause under** a ferry element and never an element beside one.

That means `errors.Is(err, consul.ErrThrottled)` and `errors.AsType[*consul.APIError]` both work through ferry unchanged.

The aggregate is flat, never nested: the address already encodes the tree.
A driver's own joined error enters as one element with its internal shape intact.

## Message text is not API

The ADRs quote error strings, which makes them look canonical, and somebody will write `strings.Contains` against one.

> **Match on the sentinels and on the address.**
> Never on a string.

Every message in this repository is free to be reworded in a patch release.
The sentinel set, the address, and the fact that a failed call reports every failure are what ferry commits to.

For precision in a test, assert over the exact set of `(address, class)` pairs rather than over a substring.
That assertion ships, in [`ferrytest`](../../ferrytest/):

```go
ferrytest.CheckErrors(t, err,
	ferrytest.Want{Address: ferry.At("db", "host"), Class: ferry.ErrMissing},
	ferrytest.Want{Address: ferry.At("db", "port"), Class: ferry.ErrValue},
	ferrytest.Want{Address: ferry.At("name"), Class: ferry.ErrMissing},
)
```

A difference is one line per expectation nothing matched and one line per failure nothing expected, each naming the address and the class, so you learn which failure moved rather than that a count did:

```
got /name: missing, and nothing wanted it: ferry: /name: required, and the plane holds nothing at this address
want /name: invalid value, and nothing reported it
```

That is the report for the call above with `/name` written as `ferry.ErrValue`: the failure that arrived and the expectation that did not match it, both named.

`ferrytest.DiffErrors(err, want...)` is the same check returning `[]string` instead of failing anything, which is what the conformance suite runs against a third-party driver and what a test asserting that a driver *fails* needs.

Three things worth knowing before you write one:

- `Class` is matched with `errors.Is`, so a subordinate sentinel is a narrower expectation: `ErrReadOnly` matches only a failure that declares it, where `ErrPlane` matches that one and every other plane failure beside it.
- A `Want` pairs with at most one failure and a failure with at most one `Want`, so two failures at one address need two `Want`s.
- The zero `Address` is a value and not a wildcard: it matches the failure that has no address, such as a plane that would not close.

**Exact, and not "contains."**
ferry's schema-compile diagnostics are a suppression order, and the defect they are most likely to develop is firing once too often.
A field tagged `required,default=v` on a slice must report two errors, not three, and a contains-assertion passes straight through the difference (ADR-0011).

## ferry never prints a value the plane supplied

ferry's own message text never contains a value the plane supplied.
The cause stays in the chain and is never printed, so a plane that holds secrets does not leak them into a log through ferry.

The cause is reachable through `errors.Unwrap`, which is what stops redaction being a loss.

One carve-out, stated because it is real: a **map key** is a name and not a value, and ferry cannot name the address without it.
So the rule is about values.

## Dump writes nothing it could have known not to

On `Dump` the aggregation is preceded by a phase: **every value is encoded before any of them is written**.

> If a `Dump` fails for a reason ferry could have known without touching the plane, the plane is untouched.

A sink implementing `ferry.Committer` is exempt, because staging already gives it that property, and it gets a better report for it: both kinds of failure in one run, where a sink that cannot stage learns the plane's own refusals only once the values it could not encode are fixed.

## What a failed Load returns

**When a `Load` fails, ferry returns no value it built.**

`Load[T]` returns the zero value of `T`.
`LoadOver` returns the seed it was given, unchanged.

## Panics

ferry itself never panics, and ferry never recovers a panic from third-party code.
A driver or a codec that panics panics through ferry, with its own stack intact.

`ferry.ErrorAt` returns `error` and not `*Error`, which is what closes the typed-nil trap: there is no concrete return type to smuggle a nil through, so `return ferry.ErrorAt(addr, f())` is safe to write.

## For driver authors

`ferry.ErrorAt(addr, err)` is the one constructor ferry exports.
It **attaches and never classifies**, so it is inert until core wraps it, and where core already knows the address, core's wins.

A driver returns any error.
Core wraps it and supplies the address, the moment, and the `ErrDriver` marker, and a driver can change none of them.
Core supplies the default class for that moment **unless** the driver's error already carries a ferry class sentinel, in which case core keeps it.

So a driver states a class only where it has an opinion core cannot form:

- a malformed document is `ErrValue`, not `ErrPlane`, because it is the operator's file;
- a plane that is writable in principle but not now is `ErrReadOnly`;
- an unreachable backend is `ErrPlane`.

Wrap your own sentinel with `%w` so a caller's `errors.Is` reaches it through ferry's wrapper.
The conformance suite asserts that reachability.

See [writing a driver](drivers.md).
