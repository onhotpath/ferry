# 15. When a plane spells one address two ways

Status: Accepted
Date: 2026-08-05
Ticket: [#216](https://github.com/onhotpath/ferry/issues/216)

## Context

[ADR-0003](0003-how-a-leaf-addresses-a-plane.md) settles one direction of the correspondence between an address and a plane key, and only one.
Its driver-side rule is that a driver's mapping from ferry address to plane key must be injective over the address set of the schema it was given, and a key function that folds two addresses onto one key fails before any I/O, because one of the two would be lost.

Nothing settles the inverse: **two plane keys reaching one address**.

Until a plane whose key space is a multimap existed, no driver could produce one.
A key space of `map[string]string` has one key per name, so a name is either the address's or it is not.
A key space of `map[string][]string` has a second dimension, and [#193](https://github.com/onhotpath/ferry/issues/193) is the finding that a sequence can reach it two ways at once:

```
?tags=a&tags=b        the repeated name, what an HTML form and curl produce
?tags.0=a&tags.1=b    the index-suffixed name, what the flat key function renders
```

Both spell `/tags#0` and `/tags#1`.
A request carrying both spells `/tags#0` twice:

```
?tags=a&tags=b&tags.0=z
```

ADR-0003 has no opinion about that, and neither does [ADR-0004](0004-source-and-sink.md) or [ADR-0005](0005-the-supported-type-set.md).
ADR-0003's amendment under [#207](https://github.com/onhotpath/ferry/issues/207) quotes exactly this request, and quotes it as evidence for something else: it is the cost of asking `Children` before the container's own address, measured on `proto/193-multimap` as reading `{Tags:[z]}` where the old order refused at the container `Get`.
That records what one prototype did and what a reordering took away.
It does not say what the request means, and this ADR is where that is decided.

The question is not `driver/http`'s.
Multipart forms, Kafka record headers and gRPC metadata are all multimaps, all reachable later, and all able to spell one position two ways the moment a driver gives them a second spelling.
A driver package settling this would settle it for one plane and outlive nothing.

This ADR is written from a throwaway prototype on branch `proto/210-http-decisions`, which never merges.
It carries five working policies over a real query plane, each run through the shipped `ferrytest.Driver`.
Every measurement below is from that prototype.

## Decision

### The rule: over one plane's contents, no two keys reach one address

> [ADR-0003](0003-how-a-leaf-addresses-a-plane.md) requires the map from address to plane key to be **injective**, so that no two addresses render to one key.
> This ADR requires the reading to be single-valued in the other direction as well.
>
> **Over the contents of one plane, no two plane keys may reach one address.**
> Where a driver finds that two do, it refuses the load rather than choosing a winner.

Together the two rules make the correspondence one-to-one in both directions, over one schema and one plane's contents.
ADR-0003 stops one address being lost to a fold on the way out.
This stops one address being decided by a coin toss on the way in.

It is a new rule beside ADR-0003 rather than a corollary of it, and the next section is why.

### It is not checkable where injectivity is checkable, and the reason is structural

Injectivity is a property of a **function out of the address set**.
The address set is in hand before any I/O, so ADR-0003 checks the static tier at schema compile and the dynamic tier as each address is minted.

Two keys reaching one address is not a property of any function ferry holds.
It is a property of **what the plane happens to contain**, and a plane that contains it today contained nothing yesterday.
There is no schema that is wrong, no key function that is wrong, and nothing to refuse before the plane is read.

The half of the question that *is* expressible in the address model is already refused there, and this was measured rather than assumed.
Where the second spelling is itself an address - a `Zero string` tagged `tags.0` beside a `Tags []string` tagged `tags` - the collapse target is the parent's own key, the parent's own key goes through the same checked `KeyFunc`, and ADR-0003 catches it in both directions:

```
repeated       DUMP {Tags:[a b] Zero:z} -> ferry: /tags#0: the driver failed: query renders this address
                                           and /tags.0 to one plane key, "tags.0", so one of the two would be lost
```

So the address model covers the case where two addresses exist, and the case this ADR owns is the one where **only one address exists** and the plane holds two keys that reach it.
`tags.0` in `?tags=a&tags=b&tags.0=z` is not an address ferry ever issued.
It is text the plane carries, and the only thing that ever looks at it is the driver.

### Who refuses: the driver, because core cannot see a plane key

Core has no plane keys.
ADR-0003 states it as the plane-agnosticism veto and gives it one line: *flattening is the driver's, always*.

That is not an inconvenience here, it is the whole answer.
The two spellings arrive at core as one address, once.
Core cannot count them, cannot name them, and has nothing to put in a message, so a refusal in core would have to be a refusal of something core cannot describe.

The obligation is therefore a driver obligation, and it is the same shape as ADR-0003's driver-side rule: core states the property, the driver is the only party with the information to check it, and the check runs before the value it protects is used.

### Where it refuses: `Children`, which is ADR-0003's second tier for this plane

> A driver whose plane can spell one address two ways refuses in **`Children`**, at the container's address, during the walk.

Three facts make it that call and no other.

**`Children` is where these addresses are minted.**
ADR-0003 requires the dynamic tier to be checked "as each is minted, before the write it belongs to".
On a plane whose members come from the plane rather than from the type, `Children` is that moment exactly: it is where a name becomes `/tags#0`, and it is the only place both spellings are visible as text at once.

**Being asked for children is the only signal a driver gets that the address is a container.**
ADR-0003, amended under #207, makes that explicit: at a slice or a map, core asks `Children` before it asks the container's own address.
An address arrives at `Get` as one segment with no kind and no arity, so `Get(/tags)` for a `[]string` and `Get(/q)` for a `string` are the same call, and a driver at `Get` does not know a sequence is being read at all.

**`Get` is closed to a refusal, and `Close` is worse than `Children`.**
[ADR-0014](0014-what-ferrytest-exports.md)'s third conformance case forbids failing at a container `Get`, and calls `Get` there itself outside the walk, so no ordering inside core opens that door.
Whether that case should hold is [#208](https://github.com/onhotpath/ferry/issues/208)'s and is not reopened here.
A refusal deferred to `Close` is the same refusal with the moment changed from `walk` to `close`, which is strictly less information for no gain.

So `Children` is the one place a multimap driver can refuse **during the walk, with an address**, and it is the only one.

### Only an overlap is a clash

> A second spelling that names a position the first spelling does not name is an **extension**, not a clash, and is not refused.

Measured, `?tags=a&tags=b&tags.0=z` into `Tags []string`, and the six neighbouring requests:

| case | refuse | index-wins | repeated-wins | repeated-audited |
| --- | --- | --- | --- | --- |
| `tags=a&tags=b&tags.0=z` | refuse | `[z b]` | `[a b]` | refuse at close |
| `tags=a&tags=b&tags.1=z` | refuse | `[a z]` | `[a b]` | refuse at close |
| `tags=a&tags.0=z` | refuse | `[z]` | `[a]` | refuse at close |
| `tags=a&tags=b&tags.2=z` | `[a b z]` | `[a b z]` | `[a b z]` | `[a b z]` |
| `tags=a&tags=b&tags.5=z` | `ErrValue` gap | same | same | same |
| `tags.0=a&tags.1=b` | `[a b]` | `[a b]` | `[a b]` | `[a b]` |
| `tags=a&tags=b` | `[a b]` | `[a b]` | `[a b]` | `[a b]` |

Two things fall out of the fourth row.
An index-suffixed name **past** the repeated count is read identically by every policy, so a refusal keyed on overlap is precise rather than blanket.
And the fifth row is already owned: a gap is [ADR-0005](0005-the-supported-type-set.md)'s contiguity rule and needs no help from this one.

A caller who deliberately uses the two spellings for different positions keeps working.
A caller who overlaps them is refused.

### What the refusal carries

[ADR-0011](0011-the-error-model.md) settles the shape and this ADR only fills it in.

- **Class `ErrPlane`**, which is the driver refusing what the plane holds.
  ADR-0011 has core keep a driver's own class sentinel where the driver supplied one.
- **Moment `walk`**, because `Children` is in the walk.
- **Location: the container's address**, because core has an address at `Children` and core's wins.
  The position both spellings claim is structure rather than plane text, so ADR-0011 permits naming it in the message, and a driver should.
- **The driver's own sentinel**, beside `ErrPlane` and core's `ErrDriver`, so a handler can tell this refusal from every other plane failure.

Verbatim from the prototype, on the realistic accident of a hidden `tags.0` beside a checkbox group named `tags`:

```
ferry: /tags: the driver failed: plane error: http: this name carries a sequence in two spellings at
once: the two spellings address the same position and one of the two values would be lost
  walk, plane error, driver
```

`Address()` is `/tags`, the element count is one, and a handler answers `400` and names the parameter.
The refusal `driver/http` ships carries the same four things and names the position in its own text as well.

### A sink emits one spelling, and may never emit two

`driver/http` ships no `Sink`, so nothing implements this today.
It is stated anyway, because the write side is where the clash gets manufactured, and because a sink deciding it later would be deciding it against a reader that already shipped.

> A sink on a plane with two spellings **picks one and writes only that one**.
> The spelling it picks is the one its own reader inverts.

Two measured reasons, and the second is the one that is easy to miss.

**A round trip is the only thing that pins it, and it pins it exactly.**
Both spellings read back correctly on their own:

```
repeated       [a b] -> "tags=a&tags=b"     -> {Tags:[a b]}
index-suffixed [a b] -> "tags.0=a&tags.1=b" -> {Tags:[a b]}
```

So a sink is free to choose, once, and is not free to change its mind: which spelling a plane is written in is a published interface under [ADR-0013](0013-what-a-plane-holds-is-a-published-interface.md), and changing it is that ADR's migration rather than an edit.

**A sink that appends into an occupied name writes precisely what this rule refuses.**
Measured on a plane already holding `{"q":["old"]}`, dumping a struct with `Q:"new"`: the appending sink produces `q=old&q=new&tags=a&tags=b`, and `q=old&q=new` is what the same driver's reader then refuses.
Dump-then-load stops being a round trip, and the driver fails its own conformance rather than the caller's request.
What a multimap sink's `Set` does at an occupied key is not decided here; what is decided is that it may not produce a plane its own reader must refuse.

The write side needs no new check beyond that, because ADR-0003 already covers it.
The one case worth recording is the asymmetry between the two spellings under that check, because it is the reverse of what #193 assumed: pushed to a sequence under a minted map key, the repeated spelling is refused at the dump, and the index-suffixed spelling writes a plane it cannot read back.

```
index-suffixed DUMP -> plane {"m.k":["z"] "m.k.0":["v1"] "m.k.1":["v2"]}
index-suffixed load -> ErrPlane: ferry: /m/k: ... renders this address and /m.k to one plane key, "m.k"
```

### The conformance suite does not settle this, and it is important that the ADR says so

All five policies were built and run through the shipped suite.
Verbatim:

```
refuse-in-children       ferrytest.Driver failures: 0
index-spelling-wins      ferrytest.Driver failures: 0
repeated-spelling-wins   ferrytest.Driver failures: 0
repeated-wins-audited    ferrytest.Driver failures: 0
first-spelling-wins      ferrytest.Driver failures: 0
```

**This is a choice and not a consequence.**
Nothing in ADR-0014's twelve cases forces it.
Case 3 constrains `Get` at a container address, and `Children` is not asserted to be infallible anywhere, so the suite is silent by construction rather than by omission.

> **Corrected: the `Driver` list is seventeen cases, and the sentence above still holds over all of them.**
>
> As published [ADR-0014](0014-what-ferrytest-exports.md)'s list was twelve, and it has grown five times since under its own amendments.
> None of the five asserts anything about which of two spellings wins at an overlapping key, so the point this section makes is unchanged: the rule is what refuses, and the suite is silent about it by construction.
> **Case 3 is still the case named**, and it still constrains `Get` at a container address and nothing more.

Two things follow, and both belong in the record.

**The rule is what refuses, not the suite.**
An ADR that implied otherwise would be false, and a driver author reading only the suite would find nothing that stops them shipping index-wins.

**No conformance case is added here.**
A case would bind every plane, and most planes have exactly one spelling and nothing for such a case to exercise, so it would skip everywhere but on the plane the rule was written for.
Whether `ferrytest` grows one is ADR-0014's, and this ADR does not pre-empt it.

### `first-spelling-wins` is not a policy, because it is not expressible

It is listed above with a result because it was built.
What it produced is identical to index-wins, and the reason is worth stating so that nobody proposes it again.

```
?tags=a&tags=b&tags.0=z   -> {"tags":["a" "b"] "tags.0":["z"]}
?tags.0=z&tags=a&tags=b   -> {"tags":["a" "b"] "tags.0":["z"]}
   the two wire orders parse to the same plane: true
```

The plane is `map[string][]string`.
The order of two values under one name survives; the order of two different names is not in the type.
A driver cannot implement first-wins because what it is handed no longer knows which spelling was first.

So "resolve by order" is not a lenient answer to this question.
It is the same answer as "do nothing", wearing a justification that the type cannot support.

### What this ADR does not decide

- **Which spellings a plane has at all.**
  That a query name repeats is `net/url`'s; that an index suffix means a position is the driver's own reading of its key function.
  A driver with one spelling has nothing to refuse and inherits no obligation from this ADR.
- **Whether a driver may refuse at a container `Get`**: [#208](https://github.com/onhotpath/ferry/issues/208).
  This ADR takes ADR-0014 case 3 as it stands, and would place the refusal in `Children` even if #208 opened `Get`, because `Children` is where the minting happens.
- **What a multimap sink's `Set` does at a name the plane already holds.**
  Three write policies were measured and only one round trips, and the sink that has to decide between them does not exist yet.
- **Where a driver's second spelling comes from, and whether it is optional.**
  A driver may offer one spelling or two; if it offers two, this rule binds it.

## Consequences

- The correspondence between one plane's contents and one schema's addresses is now one-to-one in both directions, and the two halves are checked in two different places for a stated reason: the outward half before any I/O, the inward half at the moment the addresses are minted.
- A multimap driver carries an obligation no single-valued driver carries, and it is small: the overlap is a set membership test inside the enumeration it was already doing.
- `Children` is now a call that may fail for a reason of the driver's own, not only for a plane failure.
  That is the only refusal a multimap driver can make during the walk with an address, and it is why the moment is `walk` rather than `close`.
- A caller who mixes the two spellings by accident gets an addressed refusal and a `400` they can name a parameter in.
  Under a resolving policy the same caller reads `{Tags:[z b]}` with a nil error and never learns that `a` existed.
  That is the cost and the benefit, and they are the same sentence read twice.
- The conformance suite does not enforce this, and a driver that ignores it passes.
  ADR-0003 says of its own driver-side rule that the suite checking it is what stops it being prose; this rule has no such backing and is prose.
  Recording that plainly is the whole mitigation, and it is a weaker one than the driver rules that came before it.
- A future multimap sink inherits a decision before it is written: it has one spelling, and the round trip is what pins it.
  Changing that spelling later is a plane-compatibility migration under ADR-0013 and not an implementation detail.
- ADR-0003's amendment under #207 quotes `?tags=a&tags=b&tags.0=z` reading as `{Tags:[z]}`.
  That reading was a prototype's, quoted to price a reordering, and it is not what a driver may now do with the request.
  Anybody reading the two ADRs together has to read this one for the meaning.

## Items from the xload survey

The survey is [`docs/research/generics-and-modern-go.md`](../research/generics-and-modern-go.md), section 5, against `github.com/gojekfarm/xtools` at [`a90b3aa`](https://github.com/gojekfarm/xtools/commit/a90b3aad2133248cec50f6b4d6e37b0d9e788adb).

**5.10, composite values are string-splitting, and it is not escapable.**
Bears on this ADR as the nearest relative, and the comparison runs the other way from usual.
xload's flat plane holds a whole list in one value and splits it, so a sequence has exactly one spelling and there is nothing to clash.
It could not produce this failure, and it could not detect it either: its `Loader` answers one string per key and its key space has no second dimension for a repeated name to arrive in, so a plane that carries one has already lost everything but the first value before xload sees it.
What ferry pays for reaching the second dimension at all is this rule.

**5.12, `SerialLoader` precedence is unexpressible.**
Bears on this ADR by contrast, and the contrast is the reason `first-spelling-wins` is refused above.
Precedence between two *sources* is expressible because each source is a distinct thing that can be asked in order.
Precedence between two *spellings inside one plane* is not, because the plane is a map and the order of two names is not in the type.
Measured: the two wire orders parse to the identical plane.

**5.14** was enumerated rather than assumed, all four items.

- *Two ways to set the loader.*
  Bears on this ADR as the same defect one level down.
  xload's is two ways to say one thing in an API; this is two ways to say one thing on a plane, and the repair is the same in kind: refuse the ambiguity rather than resolve it by a rule nobody can see.
- *The `CanAddr` loop that can only run once.*
  Bears on nothing here.
- *The non-deterministic select on a cancelled context.*
  Bears on this ADR, and sharpens it.
  That defect is a winner chosen non-deterministically between two available answers; the policies refused here are worse, because the plane cannot even offer the two answers in an order, so the winner is chosen blindly rather than racily.
- *Value receivers on `Error()` where pointers are returned.*
  Bears on nothing new here.
  The refusal this ADR produces is [ADR-0011](0011-the-error-model.md)'s, applied: a class sentinel, a moment, an address core supplies, and a driver sentinel beside them.

**5.5, nondeterministic error output**, is ADR-0011's and is applied rather than decided: the refusal is one element with one address, and it sorts under the `walk` moment with everything else the walk found.

The remaining items are unaffected by this ADR.
