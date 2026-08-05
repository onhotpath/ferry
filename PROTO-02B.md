# Prototype: the 02b Value layout and the session-02 spelling seam

`prototype/valueseam` is a standalone module (not in `go.work`) that
asserts, in fourteen tests, every claim the design-session boards make
about `Value` and the spelling seam:

- the 24-byte L1 layout, bool payload in padding, comparability kept
  under the same compile assertion the shipped `value.go` uses;
- accessors answer from typed payloads or refuse — the #223 guessing
  path is unconstructable;
- `Bytes` copies both ways: no aliasing either direction;
- the six kinds end to end over fake flat, tree and raw-bytes planes,
  including a yaml-ish Number spelling canonicalising `0x1F` → `31`
  so #259's knowledge stays in the driver;
- the closure laws, through composed spellings (`With(Base64, Gzip,
  MaxSize)`), with Apply refusing pre-write and Invert refusing
  corrupt data, and transform order pinned;
- negative polarity (`DISABLE_*`) as a bool transform;
- the sealed `Decl` (D2): four package values register, the one
  residual zero value refuses;
- the driver memo restoring an operator's original spelling when the
  value is unchanged — the lean-boundary backpropagation, minimal.

Run it with `go test ./prototype/valueseam/`.
The design record lives on the lavish session boards; outcomes get
folded into ADR amendments when the series closes.
