# ferry guides

Long-form documentation for people using ferry.

| guide | what is in it |
| --- | --- |
| [The supported type set](types.md) | every type ferry carries in one table, category three, array versus slice, `time.Time` and its zone, map keys, what is refused, and how to register a codec |
| [Tags, defaults and absence](tags.md) | the whole tag grammar, `default=`, `required`, `omitzero`, and what `Absent` and `Null` mean to a Go field |
| [Errors](errors.md) | what a failed call carries, how to match on it, and why the message text is not API |
| [Plane compatibility](compatibility.md) | ferry's second promise, its three tiers, what a representation change costs, and the pinned `encoding/json/v2` option set |
| [The dump lifecycle](dump-lifecycle.md) | the seven stages of a `Dump` call, what each one produces, the four sub-stages inside the walk, and the ladder of what refuses where |
| [Writing a driver](drivers.md) | the two required methods, the eight optional interfaces, the two checks before any I/O, declaring carryable kinds, and the one-call conformance suite |
| [Concurrency](concurrency.md) | the two axes, why driver batching beats fanout, the one budget both layers honour, and what a caller-supplied function may assume |
| [Watch and reload](watch-reload.md) | why reload is `Load`, the `watch` helper that turns a driver's callback into a stream of fresh values, recovering from a failed reload, and the sharp edges a watch loop inherits |

These guides are written for a reader who wants to use ferry.
The **specification** is the architectural decision records in [`../adr/`](../adr/), and where a guide and an ADR disagree, the ADR wins and the guide is wrong.

Package documentation is on [pkg.go.dev](https://pkg.go.dev/github.com/onhotpath/ferry).
