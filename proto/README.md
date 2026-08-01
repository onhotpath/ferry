# Prototype: ferry issue #5, source and sink interface signatures

Throwaway. Scratch only. This branch never merges.

Run: `cd proto && GOTOOLCHAIN=go1.27rc2 go run .`
Benchmarks: `GOTOOLCHAIN=go1.27rc2 go test -run=NONE -bench=. -benchmem .`

| Probe | Question |
| --- | --- |
| P1 | Can `jsontext.Token` *be* ferry's value model, or only resemble one? |
| P2 | Absence: comma-ok, pointer, sentinel, or a kind of the value? |
| P3 | 5.13's round-trip amplification, and what ADR-0003 already fixed for free |
| P4 | What a driver costs to write, against koanf's 31-246 line bar |
| P5 | Typed vs string values, re-measured on the structured address model |
| P6 | Is a group arm still required once composites get one address each? |
| P7 | Source and Sink: one interface or two, and where read-only lands |
| P8 | Can Load reach a map key, and can enumeration be required? |
| P9 | Where the precomputed key table lives (audits P1-P8's own answer) |
| P10 | The contract axes, and therefore the first-party driver list |
| P11 | Audit: an address minted after Bind (finds a real defect) |

Two probes are audits of this prototype's own earlier answers, and both changed
it. P9 found that memoising the key table in core is unsound two different
ways, which is why the contract has a `Bind` phase. P11 found that every probe
before it used a schema with no map and no slice, which hid the fact that the
address set handed to `Bind` is the static set and not the whole set.

The contract the probes end at:

```go
type Source interface{ Bind(*AddressSet) (Binding, error) }
type Binding interface{ Open(context.Context) (Reader, error) }
type Reader interface{ Get(context.Context, Path) (Value, error) }

type Sink interface{ Bind(*AddressSet) (WriteBinding, error) }
type WriteBinding interface{ Open(context.Context) (Writer, error) }
type Writer interface {
	Set(context.Context, Path, Value) error
	Commit(context.Context) error
	Abort()
}
```
