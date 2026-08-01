# Prototype: ferry issue #4, flat keys or structured paths

Throwaway. Scratch only. This branch never merges.

Run: `cd proto && GOTOOLCHAIN=go1.27rc2 go run .`
Benchmarks: `GOTOOLCHAIN=go1.27rc2 go test -run=NONE -bench=. -benchmem .`
Core-eligibility check: `cd proto/core126 && GOTOOLCHAIN=local GOEXPERIMENT=nojsonv2 go test ./...`

| Probe | Question |
| --- | --- |
| P1 | `jsontext.Pointer` ground truth: uniqueness, escaping, the index-vs-numeric-name limit |
| P2 | Is address collision detectable at schema-compile time from the type alone? |
| P3 | Is a dump-side collision's winner actually nondeterministic? |
| P4 | Does the candidate canonical form round trip hostile segment text? |
| P5 | Is canonical byte order the same as segment order? |
| P6 | Can a driver discharge an injectivity obligation, and what does it cost? |
| P7 | Can a template emitter build a tree from the address alone? Plus 5.10. |
| P8 | How does prefixing on nested structs express itself? |
| P9 | A value and a subtree at one address (found by accident in P2) |
| P10 | Cost of the canonical form on the hot path |
| P11 | One address set rendered onto env, YAML, Windows Registry, query params |
| P12 | Where each plane draws the line, and that every refusal is named before I/O |
| P13 | Plane-to-plane: env in, YAML out, no retagging |
| P14 | May a driver transform a segment, or must it reject? |
| P15 | METRIC_HTTP_PORT: nesting or a name with an underscore? |
