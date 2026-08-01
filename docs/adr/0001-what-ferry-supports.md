# 1. What ferry supports, and what it explicitly does not

Status: Accepted
Date: 2026-08-02
Ticket: [#2](https://github.com/onhotpath/ferry/issues/2)

## Context

ferry maps an annotated Go struct to a keyed data plane, in both directions: Load reads a plane into a struct, Dump writes a struct back out to one.
This ADR is ferry's capability charter.
It fixes the rule by which any capability is judged in or out, applies that rule to everything known today, and records what is deliberately left open.

It decides no interface signature, no tag grammar and no type enumeration.
Those have their own ADRs and are named here only as owners.

## Decision

### ferry does not know what the plane is for

ferry is a general-purpose bidirectional mapper, not a configuration library.
That is a claim about the plane, not about the feature list, and it is normative:

> Core contains nothing that requires knowing the plane is a configuration file.

The test is symmetric.
It equally excludes anything that requires knowing the plane is an i18n bundle, a query string, or a secret store.
Environment variables, YAML, Consul, HTTP query parameters and a Windows Registry hive are all the same shape to ferry.

Defaults are the worked example, because they are the feature most often mistaken for a configuration concern.
A struct describing a product may want `Colour` to default to white when the customer chose nothing.
That is a property of mapping a struct into a Go value, and it is the same mechanism a configuration struct uses for a listen port.
Configuration is ferry's most common application, not its subject.

### Core ships mechanism, not the feature built on it

Core ships the machinery.
What builds on the machinery ships outside core, unless it cannot.

An interface with nothing behind it is not a mechanism, it is a promise.
Where a capability needs core machinery - the walk re-run, a struct repopulated in place, a value republished safely - that machinery is core's, and only the part a driver can genuinely supply ships outside.

### Four buckets

| Bucket | Meaning | What ferry commits to |
| --- | --- | --- |
| **In core** | A mechanism every plane needs and no plane can supply for itself. | Building and maintaining it. |
| **Enabled** | Core's mechanisms make it buildable. ferry does not ship it. | Not standing in the way. |
| **Milestoned** | A core mechanism it needs does not exist yet. | **The mechanism, never the feature.** |
| **Ruled out** | See "What ferry will not support". | Nothing. |

**Enabled is the default landing place.**
A capability earns its way into core by being un-buildable from outside it, which is the bar `database/sql` sets for a driver.

Milestoning commits to a mechanism and never to a feature.
"Watch is milestoned" commits ferry to building the machinery watch needs, in core, when it comes.
It commits nothing about ever shipping a watcher.
A milestoned entry therefore cannot rot into a broken promise: if the mechanism ships and nobody builds the feature on it, the charter was still honoured.

### Where today's capabilities land

| Capability | Bucket | Note |
| --- | --- | --- |
| Traversal, tag parsing, encode and decode, defaults, zero values | In core | |
| Support for Go structs and stdlib types | In core | The set is enumerated by [#7](https://github.com/onhotpath/ferry/issues/7). |
| Tag validation | In core | Strict. See below. |
| Round-trip property harness, driver conformance suite | In core | The only leverage core has over what it does not ship. |
| Data plane implementations | Enabled | No driver in core. The boundary is [#3](https://github.com/onhotpath/ferry/issues/3). |
| Template generation | Enabled | First feature intended to ship. [#14](https://github.com/onhotpath/ferry/issues/14). |
| Schema extraction: `--help`, docs, deployment checks | Enabled | Dump into a recording sink. See below. |
| Drift detection: value drift | Enabled | Load a fresh value, dump both, diff by plane key. |
| Plane-to-plane transfer | Enabled | Falls out of the pluggable design. Ships as an example. |
| Drift detection: plane inspection | Milestoned | Needs observable presence ([#8](https://github.com/onhotpath/ferry/issues/8)) and optional source enumeration ([#5](https://github.com/onhotpath/ferry/issues/5)). |
| Watch and reload | Milestoned | Machinery lands in core when it lands. [#13](https://github.com/onhotpath/ferry/issues/13). |
| Delta and partial dump | Milestoned | The commitment is that the sink contract does not preclude it, so it can arrive later as an Option with the complexity hidden by the implementor. |

Template generation and watch are the two buckets side by side.
Template generation is Enabled: core ships the walk, the sink contract and typed values, and the thing that writes a starter artefact ships outside.
Watch is Milestoned: the machinery does not exist yet, and building it is core's job when it comes.

**Schema extraction needs no new core surface.**
Dump a zero-valued or defaulted struct into a sink that records what it sees, and you have every mapped key and its Go type without touching a plane.
Deployment validation is `Load` against the live plane followed by reading the error set.
Core therefore exports no schema view, and whether it ever should is left open below.

**Drift detection splits.**
Value drift - what would change if I reloaded - is buildable today by dumping the held struct and a freshly loaded one into recording sinks and diffing by plane key, which is also the correct way to build it rather than reaching for `reflect.DeepEqual` on the struct.
Plane inspection is not, because a loaded struct erases absence: a key deleted from the plane returns as the default or the zero value, indistinguishable from a value that changed to zero.
Drift and watch are the pull and push forms of the same concern, and both have their harder half in Milestoned.

### Round-trip fidelity is two properties with two owners

Round-trip fidelity is a hard guarantee, and it hides two promises that need different owners.

**Value fidelity.**
`Dump` a value into a plane, `Load` it back, and get a value equal to the original.
This is a property of the Go type set.
Core guarantees it over the set core ships, and property-tests it.

**Driver fidelity.**
`Load` from a plane, `Dump` back, and the plane still means the same thing.
This is a property of the driver, not of the type set, because it depends on the source parsing faithfully.
Core cannot guarantee it, so core states it as an obligation on drivers and ships the conformance suite that checks it.
It is scoped to the keys ferry mapped, and to semantic equality of key and value pairs.

**Byte-level plane fidelity is rejected.**
Comments, whitespace, key ordering and keys ferry does not map must survive a Load and Dump cycle untouched.
That is the driver's business and it is not a fidelity violation.
"Round trip" invites readers to assume bytes, so the ADR says otherwise explicitly.

The separation is not academic.
xload's reproduced defect 5.8 is a driver failure wearing a type-set costume: a YAML list arrives as an empty string because the provider flattened it, and no amount of type-set discipline in core would have caught it.
Conflating the two would let ferry claim a guarantee it structurally cannot honour.

### The supported type set is extensible, and extension carries the proof

Core guarantees value fidelity over **the set core ships**.
That set is extensible only by explicit typed codec registration ([#19](https://github.com/onhotpath/ferry/issues/19)), for types ferry does not own.

Registration extends the set, and the guarantee transfers to the registrant.
Core ships the round-trip property harness as a public testing package so a registrant can discharge that obligation in their own tests.
Registering without proving is permitted and forfeits the guarantee.
A type that is neither in core's set nor registered is a loud error at schema compile time, never a silent lossy dump.

So the charter does not say "closed type set" unqualified, because at runtime the set is not closed.
It says core's set is closed, extension is explicit, and extension carries its proof.

This is the second instance of one principle, stated once and applied twice:

> Core's leverage over what it does not ship is a conformance harness, not an interface.

Drivers get one, codecs get one.
The precedent is `testing/slogtest`: `slog.Handler` is four methods carrying six unchecked prose rules, and what makes it survivable is seventeen explained conformance cases.

### Tag content is validated by core, strictly

Core validates its own tags.
Unrecognised or malformed tag content fails schema compilation rather than being ignored.

Nothing else in the toolchain will do this.
`go vet`'s `structtag` analyzer is not run by `go test` by default, and it knows only `json`, `xml` and `asn1`, so it cannot see a `ferry` tag at all.
A misspelled `ferry:"PORT,requird"` reaches production unless ferry rejects it.

This is reflection, so it is **not** compile-time, and the charter does not imply otherwise.
Because schema compilation needs only `reflect.TypeFor[T]()`, rejection is assertable in a unit test with no value in hand and no plane reachable.

`encoding/json` silently ignores unknown tag options, and this is a deliberate break with that expectation.
It is consistent with the round-trip position: a tag outside the closed grammar is the same failure at the same seam as a type outside the closed set.
The cost is accepted knowingly, and it is the cost `encoding/json/v2` accepted for the same reason: ferry cannot add a tag option later without it being a breaking change for anyone who used that word, and the `ferry` tag namespace is closed to third-party tooling.

What counts as malformed, near-miss suggestions, and where option contradictions sit are [#11](https://github.com/onhotpath/ferry/issues/11)'s.

### Go 1.27 is the floor

ferry targets the first Go release in which `encoding/json/v2` is generally available, which is Go 1.27.

Since Go 1.21 the `go` directive is a strict minimum rather than a hint, so this line decides who can import ferry.
It deliberately excludes everyone on Go 1.26 and earlier.
That exclusion is affordable because ferry is in design with no code and will not ship before 1.27 is out, so it excludes users who will have upgraded by the time ferry exists.

Two routes to json/v2 on an earlier floor were considered and rejected, and are recorded so they are not revisited.
`go 1.26` with `GOEXPERIMENT=jsonv2` fails because the flag is set by whoever compiles, it propagates transitively to every consumer, and `go.mod` has no `goexperiment` directive with which a library could declare it on its consumers' behalf.
A `//go:build goexperiment.jsonv2` dual path inside ferry fails because the round-trip guarantee would have to hold identically on both paths, doubling the property-test matrix for a transitional benefit.

The decision is reversible without redesigning core.
The fallback is core at `go 1.26` with no json/v2 import and the v2 dependency in a sub-module at `go 1.27`.
Whether every sub-module takes core's floor is [#3](https://github.com/onhotpath/ferry/issues/3)'s.

### What ferry will not support

Two kinds, and the charter says which, because a flat list invites readers to assume the strong reading for both.

**Ruled out by architecture.**
These cannot exist without changing a decision above, so they change only if this ADR does.

- **Runtime path accessors**, in the shape of `Get("db.host") any`.
  ferry is struct-first, and a second accessor path is a second conversion engine by construction.
  The survey measured what that costs: viper's two engines return different answers for one key, one of them silently, and koanf's `Int64()` turns `18446744073709551615` into `9223372036854775807` with a nil error while its `String()` on the same key is lossless.
- **A validation constraint language in struct tags**, in the shape of `min=`, `max=`, `oneof=`.
  Validation follows parse-don't-validate: the type is the validation.
- **Byte-level plane fidelity.**
- **Silently ignoring anything.**
  Unrecognised tag content, types outside the supported set, lossy dumps, and discarded parse errors are all loud failures.
  xload's reproduced defect 5.11, a YAML provider that discards parse errors, is the failure this rules out by name.

**Ruled out by remit.**
These are buildable on ferry by anyone.
ferry will not ship or maintain them, and reversing that needs no design change.

- Configuration file search paths and discovery, in the shape of `SetConfigName` and `AddConfigPath`.
- Flag and command-line binding.
- A built-in source-precedence ladder as a convention, in the shape of flags beating environment beating file beating defaults.
  This rules out the convention, not source composition.
  Whether sources compose at all, and how precedence is expressed, is [#5](https://github.com/onhotpath/ferry/issues/5)'s.

### Deliberately left open

Stated as open rather than left to omission.

- **The exported verb names.**
  `Load` and `Dump` are the working assumption, not a decision.
- **Whether core ever exports a read-only schema view.**
  Deferred, to be reopened only if a concrete need survives the dump-into-a-recording-sink pattern.
- **Whether an xload compatibility sub-module ever ships.**
  Migration is a written guide first, and never a constraint on core.
  ferry makes a clean break from xload's tag grammar and owes it nothing.
- **Whether ferry has a concurrent mode at all**, in either direction.
  [#20](https://github.com/onhotpath/ferry/issues/20).

## Consequences

- A contributor can decide core-or-not without asking: apply the rule, then the bucket test.
  Anything that requires knowing the plane is a configuration file is out.
  Anything buildable from outside core stays outside it.
- Core stays small, and ferry ships fewer batteries than viper or koanf.
  Adoption therefore depends on drivers being cheap to write.
  The bar is known: koanf gets twenty providers at between 31 and 246 lines each, median around 120, off a two-method interface.
- Two obligations core cannot compile-check, driver fidelity and codec round-trip, are discharged by two harnesses that can.
  Both are core's to build and maintain, and both are load-bearing rather than optional extras.
- Strict tag rejection means ferry's tag vocabulary is effectively frozen once published.
  Adding an option later is a breaking change.
- The `go 1.27` floor is the one decision here with no technical escape for consumers, mitigated only by the named sub-module fallback.

## Items from the xload survey

The survey is `docs/research/generics-and-modern-go.md`, section 5, against `github.com/gojekfarm/xtools` at [`a90b3aa`](https://github.com/gojekfarm/xtools/commit/a90b3aad2133248cec50f6b4d6e37b0d9e788adb).
These are regression targets for ferry, not upstream bug reports.

Addressed by this ADR:

- **5.8**, type information destroyed at the boundary.
  Driver fidelity is a stated obligation, and the conformance suite is the check.
- **5.11**, the YAML provider silently discards parse errors.
  Silently ignoring anything is ruled out by architecture.

Named here, fixed elsewhere:

- **5.1**, the `Loader` signature cannot express absence.
  This ADR records its consequence - a loaded struct erases absence, which is why plane inspection is Milestoned - and leaves the fix to [#5](https://github.com/onhotpath/ferry/issues/5) and [#8](https://github.com/onhotpath/ferry/issues/8).

Owned by other tickets, listed so none is silently dropped:
5.2, 5.4 and 5.6 by [#20](https://github.com/onhotpath/ferry/issues/20);
5.3 by [#16](https://github.com/onhotpath/ferry/issues/16);
5.5 by [#9](https://github.com/onhotpath/ferry/issues/9);
5.7 by [#8](https://github.com/onhotpath/ferry/issues/8);
5.9 by [#12](https://github.com/onhotpath/ferry/issues/12);
5.10 by [#4](https://github.com/onhotpath/ferry/issues/4) and [#11](https://github.com/onhotpath/ferry/issues/11);
5.12 and 5.13 by [#5](https://github.com/onhotpath/ferry/issues/5);
5.14 across [#9](https://github.com/onhotpath/ferry/issues/9) and [#11](https://github.com/onhotpath/ferry/issues/11).
