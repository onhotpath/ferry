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

## Two defects found in `proto/5-source-sink` while using it

Both are driver-fidelity defects of exactly the kind ADR-0001's conformance suite exists to catch, and both were found by running a real type through a real driver rather than by reading it.

- The YAML sink writes raw bytes under a `!!binary` tag without base64-encoding them, and the reader decodes them the same wrong way. The pair is self-consistent, so it round-trips; what caught it is `gopkg.in/yaml.v3`'s own emitter refusing to emit invalid `!!binary`. Patched here to base64.
- Assigning sequence children positionally silently reindexes when an element mints no address, so element 2 becomes element 1. Fixed here by reading the position out of the `Index` segment, which is what the segment kind is for.
