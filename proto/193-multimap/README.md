# proto/193-multimap

A throwaway prototype for [#193](https://github.com/onhotpath/ferry/issues/193).

**This never merges.**
It lives on `proto/193-multimap`, outside the root `go.work`, in a module of its own.
`go build ./...` and `make check` at the repository root do not see it.

The branch also carries a **deliberate patch to core's `walk.go`**, in its own commit, marked `THROWAWAY` in the source.
That patch is the measurement, not a proposal in shipped form.
It never merges either.

## The question

An HTTP query-parameter plane and a header plane are both `map[string][]string`.
`?tags=a&tags=b` is what a plain HTML form with two same-named checkboxes produces, what `curl -d` repeated produces, and what `url.Values.Encode` produces.
How does such a plane express a sequence to ferry at all?

The obstacle is the container-address rule.
`walk.go`'s `members` answers a dynamic container's own address before it enumerates, and `container` holds that address to `Absent` or `Null`.
`?tags=a&tags=b` puts a value at exactly that address.

## What is built

Seven shapes, all real, all through `ferry.Load`, `ferry.Dump`, `ferry.NewKeys` and the shipped `ferrytest.Driver`:

| shape | positions live | at a name holding n>1 | `Children` mints positions |
| --- | --- | --- | --- |
| `indexed` | in the name, `tags.0` | refuse | never |
| `indexed-first` | in the name, `tags.0` | first wins, silently | never |
| `cardinality` | behind the name | `Absent` | n > 1 |
| `cardinality-audit` | behind the name | `Absent`, reported at `Close` | n > 1 |
| `declared` | behind the name | `Absent` if the driver was told | n >= 1, if told |
| `sequence` | behind the name | refuse | n >= 1 |
| `enumerated` | behind the name | `Absent`, reported at `Close` | n >= 1 |

`declared` takes its signal from driver configuration, `Repeatable("tags")`.
It is driver config and not tag grammar, so [#34](https://github.com/onhotpath/ferry/issues/34) stays parked.

`sequence` and `enumerated` are only sound if being asked for `Children` means core has already decided the address is a dynamic container.
That is the bend, and it is one reordering in `members`: at a slice or a map over a source that can enumerate, ask `Children` first and ask the container address only where there are no children.

## How to run it

```
cd proto/193-multimap
go test -v ./...
```

To see the shapes against unpatched core, drop the last commit on the branch:

```
git stash            # or: git revert the walk.go commit
cd proto/193-multimap && go test -count=1 -v ./...
```

Nothing here asserts a pass or a fail. Every case logs what it did, because the behaviour is the finding.

## What it showed

1. **The driver genuinely cannot tell a container address from a leaf address.**
   `TestTheDriverCannotSeeTheSchema` prints the `AddressSet` for `Tagged{Tags []string}` and for `Scalar{Q string}`: both are `[/tags]` and `[/q]`, one segment, no kind, no arity. So `Get(/tags)` and `Get(/q)` are the same call and every driver-side shape's answer there is a bet.

2. **No driver-side shape is total.** Each one trades a hole for a hole.
   `indexed` refuses `?tags=a&tags=b`, which is the commonest way a browser submits a form.
   `cardinality` accepts it and then fails `?tags=a` into a `[]string`, so a form with one checkbox ticked fails where the same form with two ticked works.
   `indexed-first` silently discards.
   `declared` is total on the query plane and silently wrong on a second schema through the same `Source`, because the declaration lives on the driver and not on the schema.

3. **`ferrytest.Driver` case 3 forbids refusing at a container `Get`.**
   `sequence` is the only shape that fails the shipped suite, on both planes, at case 3, and it fails there whether or not the bend is applied, because the case calls `Get` at the container address itself rather than through the walk.

4. **The bend makes `enumerated` total, and breaks nothing.**
   With the reordering, `enumerated` reports 0 `ferrytest.Driver` failures on both planes, 0 `ferrytest.RoundTrip` failures, and every row of the matrix correct.
   With the reordering applied, core's own suite, `ferrytest`'s, and `driver/env`, `driver/kv` and `driver/yaml`'s conformance runs are all green, unchanged.

5. **The bend is priced at one call.**
   `TestBoundaryCallCount`: a slice with two elements costs `Get=3 Children=1` before and `Get=2 Children=1` after; an absent container is unchanged; a `Null` at a container costs `Get=1 Children=0` before and `Get=1 Children=1` after.
   The extra call lands only where a plane carries `KindNull`, which of the shipped drivers is `driver/yaml` alone.

6. **The bend takes something away too.**
   A driver can no longer refuse at a container address that also has children, because `Get` is not called there.
   `indexed` under the bend reads `?tags=a&tags=b&tags.0=z` as `{Tags:[z]}` where unpatched it refused.

7. **An array never meets the rule at all.**
   `[N]T` has no container address, so `?pair=a&pair=b` into `[2]string` is silent under `indexed` and `indexed-first` - `{Pair:[ ]}`, no error - and correct under every shape that puts positions behind the name.

8. **Two spellings at one address is a silent overwrite, and the driver has to refuse it.**
   `?tags=a&tags=b&tags.0=z` addresses `/tags#0` twice. Left alone it reads `{Tags:[z b]}`. The refusal is in `Children`.

9. **The header plane behaves identically, and its `Canonical` question is real.**
   `net/http` destroys a map key's own spelling, so `map[string]int{"cpu":1,"CPU":2}` is refused at mint time by `ferry.NewKeys`: `header renders this address and /limits/CPU to one plane key, "Limits-Cpu"`.
