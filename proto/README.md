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
| P12 | What state each interface owns, and whether the lifetimes differ |
| P13 | The same boundaries with less surface, read against dagger |
| P14 | Does every writer need closing? |
| P15 | Release is not commit, and only one of them is conditional |
| P16 | The final contract, all four drivers rewritten against it |
| P17 | Absent, Null and the empty string, in both directions |

Two probes are audits of this prototype's own earlier answers, and both changed
it. P9 found that memoising the key table in core is unsound two different
ways, which is why the contract has a `Bind` phase. P11 found that every probe
before it used a schema with no map and no slice, which hid the fact that the
address set handed to `Bind` is the static set and not the whole set.

Four probes are audits of this prototype's own earlier answers, and all four
changed it. P9 found that memoising the key table in core is unsound two
different ways, which is why the contract has a `Bind` phase. P11 found that
every probe before it used a schema with no map and no slice, which hid that
the address set handed to `Bind` is the static set. P14 found that four of six
realistic sinks have nothing to do at the end, so a required `Close` is
`return nil` boilerplate that is indistinguishable from a missing rollback.
P15 found that `Close(ctx, cause)` conflated resource release with the
commit decision, and that they do not co-occur.

The contract the probes end at, in `final.go` with the drivers in `fdrv_*.go`:

```go
type Source interface{ Bind(*AddressSet) (OpenFunc, error) }
type Sink   interface{ Bind(*AddressSet) (OpenWriterFunc, error) }

type OpenFunc       func(context.Context) (Reader, error)
type OpenWriterFunc func(context.Context) (Writer, error)

type Reader interface{ Get(context.Context, Path) (Value, error) }
type Writer interface{ Set(context.Context, Path, Value) error }

// optional, discovered by assertion
type Releaser   interface{ Close() error }                   // = io.Closer
type Committer  interface{ Commit(ctx context.Context) error }
type Enumerator interface{ Children(context.Context, Path) ([]Path, error) }
```
