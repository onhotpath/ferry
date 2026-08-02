# proto/7-type-set

Throwaway prototype for [#7](https://github.com/onhotpath/ferry/issues/7), "The supported type set, and how round-trip is enforced".
Never merges.

Built on `proto/5-source-sink`: `address.go`, `value.go`, `final.go` and `fdrv_yaml.go` are lifted from it so the probes run against a real `Path`, a real `Value` and a real YAML plane over real files rather than a mock.

Run: `GOTOOLCHAIN=go1.27rc2 go run .`

## What is new here

| file | what it is |
| --- | --- |
| `typeset.go` | the candidate type set: an identity table consulted before `reflect.Kind`, and the leaf codecs |
| `walk.go` | schema compile, dump and load over that set |
| `harness.go` | the round-trip property harness, as a table with a per-type equality relation |
| `audit.go` | the composites, pointers and nesting the first table missed, plus the can-it-go-red check |
| `container.go` | what a real plane reports at a container address |
| `audit2.go` | the property's blind spot: representation |
| `audit3.go` | the struct admitted by kind that maps nothing |
| `gaps.go` | the audit against the ticket's literal asks, after a review challenge |
| `flat.go` | a plane with no type information, which is what hid G2 |
| `edges.go` | what actually happens to fifteen types people reach for |
| `timecost.go` | what the `time.Time` losses cost, and what the relation buys |

## Probes

| # | question | result |
| --- | --- | --- |
| P1 | is the static address set computable from the type alone | yes, no value in hand |
| P2 | does nil-vs-empty survive | no, and one collision of three states into two signals is forced |
| P3 | is a nil composite's address prefix-free against a populated one | no: `/Tags` is a prefix of `/Tags#0`, so one type has two address shapes |
| P4 | does a full struct round-trip in memory | yes, except `time.Time` under `==` |
| P5 | does it round-trip through real YAML | 0 of 15 addresses differ |
| P6 | float specials | `NaN`, `±Inf`, `-0` all bit-exact through shortest-repr text; `==` fails for NaN |
| P7 | what schema compile refuses | complex, func, interface, chan; `[]rune` is accepted as `[]int32` |
| P8 | the property harness over the core set | 11/11 on both planes |
| P9 | composites, pointers, nesting; can the harness go red | 10/10, and it goes red on a lossy codec |
| P10 | can a plane report present-and-empty at a container address | no: `tags: []`, `tags: {}` and a missing key are one observation |
| P11 | does the property catch a wrong representation | **no.** nanoseconds round-trips perfectly. Representation needs a golden column |
| P12 | a struct whose fields are all unexported | `netip.Addr`, `big.Int`, `netip.AddrPort`, `time.Location` compile clean and dump nothing, silently |
| P13 | the gap audit, run after a review challenge | five gaps, two of them severe. See below |
| P14 | the core table through a plane with no type information | 11/11 scalars, and nil pointers do not survive |
| P15 | what happens to fifteen real-world types | **three outcomes, not two.** Five refused, nine admitted, and several admitted with a surprising representation |
| P16 | what the `time.Time` losses cost | the zone loss is invisible to `.Equal`, and it breaks DST arithmetic |

## The gap audit (P13), which a review challenge forced

Every proof up to P12 fed `dump`'s own output back into `load`, so the kinds always matched.
No probe had ever loaded from a plane that reports different kinds than ferry writes, which is the majority case: ADR-0004 records that three of four first-party drivers have no type information at all.

| # | claim under test | result |
| --- | --- | --- |
| G1 | the ADR's stated `*[]T` escape hatch for nil-versus-empty | **the claim was false.** A nil pointer and a pointer to an empty slice both mint `/P=null` and both load back nil |
| G2 | loading from an all-`String` plane | **failed outright**, `value: wrong kind`, every field zero. env, query and kv could not have loaded a single integer |
| G3 | `[N]T` for a non-byte `T` | **panicked**, `reflect.MakeSlice of non-slice type`. Arrays were listed as supported and were never exercised |
| G4 | named types over an admitted kind | fine, admitted by kind |
| G5 | a bad leaf | loud in all four cases: bad syntax, overflow, wrong kind, empty text |
| G6 | embedded structs | currently a named field, `/Base/ID`; [#11](https://github.com/onhotpath/ferry/issues/11)'s to change |
| G7 | an array's addresses | **static**, N of them from the type, unlike a slice's |

G2's fix is the String-donor rule in `typeset.go`, and P14 is the plane that would have caught it.
G1 has no fix and the claim was removed from the ADR.

## Two defects found in `proto/5-source-sink` while using it

Both are driver-fidelity defects of exactly the kind ADR-0001's conformance suite exists to catch, and both were found by running a real type through a real driver rather than by reading it.

- The YAML sink writes raw bytes under a `!!binary` tag without base64-encoding them, and the reader decodes them the same wrong way. The pair is self-consistent, so it round-trips; what caught it is `gopkg.in/yaml.v3`'s own emitter refusing to emit invalid `!!binary`. Patched here to base64.
- Assigning sequence children positionally silently reindexes when an element mints no address, so element 2 becomes element 1. Fixed here by reading the position out of the `Index` segment, which is what the segment kind is for.
