# proto/12-codec-chain

Throwaway prototype for [#12](https://github.com/onhotpath/ferry/issues/12), "Encoding: the dual of Decoder, and its precedence".
Never merges.

Built on `proto/7-type-set`, which is built on `proto/5-source-sink`, so every probe runs against a real `Path`, a real `Value`, a real YAML plane over real files, and the type set ADR-0005 actually landed.
`README-proto7.md` is #7's own README, kept because its probe list is still the map of the inherited files.

Run: `P12=all GOTOOLCHAIN=go1.27rc2 go run .`
One probe: `P12=4 GOTOOLCHAIN=go1.27rc2 go run .`
The source scan is separate: `python3 ../scan_halfpairs.py "label" <root>`.

## What is new here

| file | what it is |
| --- | --- |
| `chain.go` | the candidate chain: four arms, paired selection against per-direction selection, and the seam into `classify` |
| `p1_census.go` | which of the eight encoding interfaces 29 real types actually carry |
| `p2_pairing.go` | what per-direction selection breaks that paired selection does not |
| `p3_receivers.go` | value against pointer receivers, unaddressable values, and the nil-pointer case |
| `p4_beforekind.go` | the three-way diff: kind only, chain after kind, chain before kind |
| `p4b_artefact.go` | the same diff as a real YAML file, plus the two probes that separate before from after |
| `p5_jsonarm.go` | what recognising `json.Marshaler` would put on a non-JSON plane |
| `p6_marshalerto.go` | what `MarshalerTo` buys at a `{kind, text}` boundary |
| `p7_appender.go` | where `TextAppender` sits, and what preferring it is worth |
| `p8_absent.go` | `Absent`, `Null` and the empty string at a codec, against xload reproduced |
| `p9_donor.go` | the kind a codec declares, and whether it sees the raw or donated `Value` |
| `p10_omit.go` | whether "zero in Go" and "empty on the plane" agree |
| `p11_context.go` | what a `context.Context` in a codec signature would actually do |
| `p12_halfpair.go` | the three answers for an incomplete pair, and the blast radius of the strict one |
| `p13_mapkey.go` | a chain-admitted type as a map key, and the injectivity obligation |
| `p14_audit.go` | the cases the other thirteen probes do not cover |
| `../scan_halfpairs.py` | half-pair census by declaration scan over a whole source tree |

Changes to inherited files, all load-bearing:

- `typeset.go`: `classify` gained the chain seam, pointer indirection moved ahead of it (see P3), and `validMapKey` now consults the chain (see P13).
- `walk.go`: `decMapKey` and `mapKeyText` consult the chain, for the same reason.

## Probes

| # | question | result |
| --- | --- | --- |
| P1 | which encoding interfaces do real types carry | **zero half pairs in 29 types**; 10 of 29 carry more than one complete arm; gob is never a sole arm and json is a sole arm only for a type kind already admits |
| P2 | does per-direction selection break what paired selection does not | **yes.** A text encoder with a json decoder dumps and never loads; a binary encoder with a text decoder is a wrong-kind error at Load. `encoding/json/v2` has the same defect and only asks in prose that the two agree |
| P3 | receiver mechanics | `big.Int` does **not** implement `TextMarshaler`; only `*big.Int` does. A map value is unaddressable so the encoder runs on a copy. **A pointer type satisfying the pair in its own right bypasses ADR-0005's nil rule: a nil `*big.Int` dumped `string("<nil>")` and the load panicked.** Fixed by resolving pointers structurally before the chain |
| P4 | before kind, after kind, or not at all | after-kind rescues 7 refusals at zero fidelity cost; before-kind rescues the same 7 **and** makes `net.IP`, `uuid.UUID` and `slog.Level` legible, at the cost of `net.IP`'s two byte encodings collapsing and a normalising `MarshalText` breaking |
| P4d | the artefact | `listen: !!binary AAAA...wAACAQ==` against `listen: "192.0.2.1"` in a real YAML file |
| P4e | what separates them | under **after-kind, exporting one field silently rewrites the type's plane representation**; under before-kind it changes nothing |
| P4f | does ferry invent the normalising hazard | no: the same type already fails to round-trip through `encoding/json` |
| P5 | the json arm | every json form is the text form wrapped in JSON syntax, and the quotes are literal plane bytes: `level: "\"WARN\""`. A json marshaler returning an object collapses `/V/A` and `/V/B#0` into one opaque address |
| P6 | what `MarshalerTo` buys | **nothing.** At its best case, one reused encoder and buffer: 63 ns / 16 B / 1 alloc against `Marshaler`'s 35 ns / 16 B / 1 alloc. The allocation it exists to remove is the one `Value{kind, text}` reinstates |
| P7 | `TextAppender` | there is no appending decoder, so it is a second **spelling** of the text arm's encode half, not an arm. Preferring it: 25 ns / 1 alloc against 40 ns / 2 allocs |
| P8 | Absent, Null, empty | xload's decoder sees neither empty nor missing, and cannot tell them apart. ferry's sees `String("")` and never sees `Absent` |
| P9 | declared kind, raw or donated | a codec whose text is digits must declare `Number` or it works on env and fails on YAML. A codec seeing the **raw** value fails on all three flat planes, which is ADR-0005's G2 delegated to every registrant |
| P10 | zero and omission | "zero in Go" and "empty on the plane" disagree in **both** directions: `time.Time{}` encodes to 20 bytes, and a deliberately-set value can encode to nothing |
| P11 | context in a codec | no recognised interface takes one; a ferry-own one would make cancellability a function of precedence; the honest use case is I/O and ADR-0004 already put that in `Source` |
| P12 | the half pair | strict refusal costs **1 type in the whole go1.27rc2 public standard library** (an internal tooling type) and **0** in a third-party corpus |
| P13 | a chain type as a map key | works once `validMapKey` consults the chain. **And `map[time.Time]string` silently collapses two distinct keys into one address today**, in core's own set |
| P14 | the audit | zero values, every composite position, the flattening plane, real YAML, and the root all pass; `regexp.Regexp`'s zero does not round-trip under `DeepEqual`; the completeness check structurally cannot see a chain-admitted type; core's 11/11 and 10/10 are unaffected |

## Two defects this found in inherited code

Both were found by running rather than by reading, and both are fixed on this branch only.

- **A pointer type can satisfy an arm in its own right.**
  `*big.Int` implements the whole text pair, so with the chain consulted before the pointer shape, a `*big.Int` field became a leaf, ADR-0005's nil-pointer rule never ran, a nil dumped as `string("<nil>")` and the load segfaulted inside `big.Int.UnmarshalText`.
  Pointer indirection is now resolved before the chain is asked anything.
- **`map[time.Time]string` collapses keys.**
  `validMapKey` admits anything in the identity table, and `time.Time`'s RFC 3339 text is not injective over the type: a `time.UTC` value and a `FixedZone("GMT", 0)` value are distinct under `==` and produce identical text.
  Two Go keys, one address, no error.
  This is ADR-0005's stated injectivity hazard occurring inside core's own set rather than in a registered codec, and no probe in #7 reached it because none used a composite map key.
