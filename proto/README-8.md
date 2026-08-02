# proto/8-defaults

Throwaway prototype for [#8](https://github.com/onhotpath/ferry/issues/8), "Defaults and zero values".
Never merges.

Built on `proto/7-type-set`, which is built on `proto/5-source-sink`, so every probe runs against a real `Path`, a real `Value`, the real type set and a real YAML plane over real files.
`README.md` documents that inheritance; this file documents only what #8 added.

Run: `GOTOOLCHAIN=go1.27rc2 go run . d`
(`go run .` with no argument still runs #7's eighteen probes, which are unchanged and still green.)

## What is new here

| file | what it is |
| --- | --- |
| `d_schema.go` | the compiled schema: static addresses plus what each one declares, and every check that runs from `reflect.TypeFor[T]()` alone |
| `d_load.go` | the Load walk with Absent/Null separated, defaults applied, and a presence bit carried per subtree; plus a Dump that records `Set` calls so an omission is observable as the absence of one |
| `d_probe1.go` | D1-D6 |
| `d_probe2.go` | D7-D13 |
| `d_probe3.go` | D14-D22 |
| `d_null.go` | N1-N3, the Null-at-a-scalar question reopened under review |
| `d_reqc.go` | R1-R4, what `required` does at a container address |
| `d_opt2.go`, `d_opt2b.go` | O2a-O2d and B1-B3, the two candidate cuts for `required` |
| `d_stack.go` | S1-S3, stacked refusals and the tag spelling |
| `d_reqslice.go` | RS1-RS3, whether "explicitly `[]` satisfies required" is writable |

Every rule with a live alternative is a field on `loadOpts`, so the two candidates are measured against each other rather than argued.

## Probes

D21 and D22 are the audit of this ADR's own strongest claim, run after the ADR was drafted.
Both found something.

| # | question | result |
| --- | --- | --- |
| D1 | what Absent does to a field not at its zero value | nothing: the seed survives, so a pre-populated struct is a defaults mechanism with no core surface |
| D2 | ADR-0005's "a container address with no children yields the zero value" | **under-determined.** Its fixtures all loaded into a zero destination, where "yields zero" and "leaves unchanged" are one observation |
| D3 | what Absent and Null mean per kind, three candidate readings of Null | Null is admitted by the types that can hold one and refused by every other leaf, which is ADR-0005's existing rule with nothing added |
| D4 | plane holds explicit empty, struct holds a non-zero default | present beats absent: `FOO=` wins and gives `""`; into an `int` it is loud |
| D5 | what satisfies `required`, and which option pairs contradict | presence satisfies it, `FOO=` included; `required`+`default` and `omitzero`+non-zero-`default` are compile errors |
| D6 | 5.7, `reflect.DeepEqual` as a "was anything set?" probe | the correctness claim reproduces exactly; **the cost claim does not**, at 549 ns against 422 |
| D7 | is a field at its default dumped or omitted | dumped. Omit-if-default makes the stored artefact's meaning depend on the reading code's default: 8080 becomes 9999 |
| D8 | why a default is a `Value` and not a cached Go value | a cached `[]byte` default is **aliased across every load of one schema**; re-decoding costs 31.8 ns |
| D9 | what a default declaration is checked against, and when | the leaf's own parser, at schema compile, with no value in hand; a composite default is refused |
| D10 | what a pointer adds, at a leaf and at a composite | at a leaf it adds a real bit (`null` against `number("0")`); at a composite it adds none, which is ADR-0005's G1 |
| D11 | probing presence to find an indexed composite's length | a hole truncates silently, at one `Get` per element. Enumeration is the only route |
| D12 | observable presence | a deleted key and a key changed to zero are one struct and two observations; the observer is free |
| D13 | what a sink is handed, and what an omission means | zero `Set` calls carry `Absent`; a patching and a replacing sink disagree, and both are legal |
| D14 | does a default under an optional subtree materialise the pointer | if it does, no `*T` with a default beneath it can ever be nil |
| D15 | a default inside a map value or a slice element | **looked up by the realised address it silently vanishes.** A declaration attaches to the address shape |
| D16 | what template generation gets | defaults reach the artefact, and `required` fires first, which is #14's to resolve |
| D17 | defaults as a `Static` source under `FirstOf` | a field rename silently drops every default, because the addresses are spelled twice |
| D18 | the same default through all three of the harness's planes | identical on all three; and a blank YAML key is a `null`, so it is refused rather than defaulted |
| D19 | loading twice into one destination with a key deleted in between | **the previous load's value leaks, under both rules.** Absent-does-not-write is a rule about one load |
| D20 | a default inside an array element | applies, because an array element is a static address; a slice element's is not |
| D21 | partial presence into a seeded destination, per kind | **a struct merges and a composite replaces.** Both follow from the one rule and neither looks like it does |
| D22 | `required` at a container address | reached by no other probe, and unimplemented until this one. A container cannot be present-and-empty, so `tags: []` cannot satisfy it |

## Four answers this prototype overturned

Each was written into a draft before it was run.

- **Null means the zero value.** Reached first because `encoding/json/v2` does exactly that, measured on `go1.27rc2`: `{"i":null}` sets a Go `int` to 0 where v1 leaves it alone. Overturned by ADR-0005's own leaf rule - every leaf accepts its own kind plus `String` and nothing else coerces - which makes `Null` at an `int` a wrong kind and needs no new principle. D3.
- **Declarations are keyed by address.** They are keyed by address *shape*: `/servers/a/port` is not in the schema and never can be, because the key comes from the value. Keyed by the realised address, every default under a map or a slice silently vanished. D15.
- **An array walks the elements the plane has.** It walks all N, because its element addresses are static. Walking only the present ones made an element's declarations conditional on a sibling. D20.
- **ADR-0005's composite rule was settled.** Its wording is "yields the zero value" and its fixtures could not tell that from "leaves unchanged", because the destination was always zero. D2, and D19 is what forces the distinction to be resolved rather than left implicit.

## Round two: twenty more probes, opened by review

The owner challenged the ADR's deciding argument for refusing `Null` at a scalar, which was that plane-to-plane transfer would silently turn a YAML null into a zero.

| # | question | result |
| --- | --- | --- |
| N1 | does the transfer argument hold | **no.** The plane-preserving transfer is address-to-address, never builds a Go value, and never runs #8's rules. The struct-mediated one already rewrites `Tags: []` to `null`, which ADR-0005 accepted |
| N2 | is either null policy recoverable without a knob | **asymmetric.** Under refuse a registered codec recovers leniency; under zero nothing recovers strictness, because the zeroing precedes the chain |
| N3 | the four ways a human writes "no value here" | **the ergonomic claim was overstated.** A commented-out line removes the key, so it is `Absent` and takes the default. Only a blank key and an explicit `null` reach the null path, and there zeroing gives `0`, the one answer nobody wants |
| R1 | `required` at a container address, through real YAML | separates two documents that load to the identical value, and the error says "the plane does not have it" when it does |
| R2 | the `null` workaround on a plane with no null | **does not exist.** On env, query, TOML and KV no document satisfies a required composite while leaving it empty |
| R3 | is the address `required` names even in the set | a composite's own address exists only when it is nil or empty; a non-pointer struct and an array have none at all |
| R4 | the two composites with no own address | `required` on a non-pointer struct was **accepted at compile and enforced by nothing** |
| O2a-d | the first cut: refuse `required` on every composite | works, and refuses `*Cred` too, which is a genuine use case |
| S1 | a composite carrying both `required` and a default | three errors for one field, so admissibility must be checked before contradictions |
| S2 | what the tag does with `default=["value"]` | **`reflect.StructTag.Get` truncates it to `origins,default=[`.** `go vet` catches it, `go test` does not, which confirms ADR-0001's vet-gap claim for a `ferry` tag |
| S3 | the diagnostic rule applied | two errors, not three, and the contradiction still fires where both options are admissible |
| B1-B3 | the second cut: the static/dynamic line | `required` is admissible where children come from the TYPE. `*Cred` works again, on every plane |
| RS1 | can "explicitly `[]` satisfies required" be written | **no.** Five documents, three observations: `key missing`, `[]` and `{}` are one |
| RS2 | the four readings side by side | the wanted reading needs two answers for one input |
| RS3 | would a seventh kind rescue it | no: env, query and KV cannot express "present and empty" at all, so the rule would hold on three planes of six |

**Four more answers overturned in round two:** the transfer argument that decided `Null`; the ergonomic claim that argued against it; the composite-versus-leaf cut for `required`; and the refusal of `required` on `*Cred`.

**And two rules the ADR had not stated, found by auditing its strongest claim.**
"Absent does not write" was written as though it said one thing at every kind.
D21 shows it does not look like it: a struct merges field by field and a slice or a map is replaced wholesale, because a container is one decision the plane either made or did not.
D22 reached a case no earlier probe had: `required` on a composite was accepted by schema compile and enforced by nothing.

And one survey claim that did not reproduce: 5.7 calls the `DeepEqual` probe expensive, and on the same walk it is 549 ns against the presence bit's 422. Real, and not what "expensive" implies. The correctness half of 5.7 reproduces exactly.
