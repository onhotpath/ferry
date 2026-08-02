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
| `d_probe3.go` | D14-D20 |

Every rule with a live alternative is a field on `loadOpts`, so the two candidates are measured against each other rather than argued.

## Probes

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

## Four answers this prototype overturned

Each was written into a draft before it was run.

- **Null means the zero value.** Reached first because `encoding/json/v2` does exactly that, measured on `go1.27rc2`: `{"i":null}` sets a Go `int` to 0 where v1 leaves it alone. Overturned by ADR-0005's own leaf rule - every leaf accepts its own kind plus `String` and nothing else coerces - which makes `Null` at an `int` a wrong kind and needs no new principle. D3.
- **Declarations are keyed by address.** They are keyed by address *shape*: `/servers/a/port` is not in the schema and never can be, because the key comes from the value. Keyed by the realised address, every default under a map or a slice silently vanished. D15.
- **An array walks the elements the plane has.** It walks all N, because its element addresses are static. Walking only the present ones made an element's declarations conditional on a sibling. D20.
- **ADR-0005's composite rule was settled.** Its wording is "yields the zero value" and its fixtures could not tell that from "leaves unchanged", because the destination was always zero. D2, and D19 is what forces the distinction to be resolved rather than left implicit.

And one survey claim that did not reproduce: 5.7 calls the `DeepEqual` probe expensive, and on the same walk it is 549 ns against the presence bit's 422. Real, and not what "expensive" implies. The correctness half of 5.7 reproduces exactly.
