# #11, the struct tag grammar

Throwaway. Never merges. Built on `proto/8-defaults`, which is built on
`proto/7-type-set` and `proto/5-source-sink`, so the probes run against a real
`Path`, a real `Value`, the real type set, the real YAML driver over real
files, and #8's real compiled schema.

    go build -o /tmp/p11 . && /tmp/p11 t     # #11's probes
    go build -o /tmp/p11 . && /tmp/p11 d     # #8's, unchanged, as a regression
    go test -run TestFerryTags -v .          # the validation entry point
    go vet ./...

Files added by #11: `t_mech.go`, `t_grammar.go`, `t_compile.go`,
`t_probe1.go`, `t_probe2.go`, `t_probe3.go`, `t_probe4.go`,
`t_validate_test.go`. One line of `d_schema.go` routes #8's walkers through
the new grammar when `t11Mode` is set, so the end-to-end probe uses real names.

## The probes

| | question | answer |
| --- | --- | --- |
| T1 | what `reflect.StructTag.Get` does to a ferry tag | an invalid Go escape makes the tag invisible; a bare `"` truncates it |
| T2 | does a broken ferry tag break the neighbouring keys | **yes**, a bare `"` destroys `json` and `yaml` on the same field |
| T2b | `go vet` and `go test` | vet catches 2 of 3 classes, misses the duplicate key; test catches none |
| T3 | can ferry diagnose what `Get` swallows | yes, by scanning the raw tag itself |
| C1 | is a plane name the Go field name? | 233 of 4835 third-party, 38 of 556 stdlib; 1/1580 for `yaml`, 0/808 for `mapstructure` |
| T4 | every diagnosis the grammar produces | 40 cases, a third ill-formed |
| T5 | near-miss | 22 of 26 misspellings get a specific remedy |
| T6 | three escape models | doubling silently renames on a stray comma; no-escaping cannot write the name at all |
| T7 | 5.10's second half | xload loses the delimiter; ferry writes it |
| T8 | the naming rule | under a Go-name default, exporting a field adds an address |
| T9 | embedding | promotion needs no word; a clash is ADR-0003's prefix-free rule |
| T10 | ADR-0006's five refusals | all five, driven by the real grammar |
| T11 | admissibility before contradictions | ADR-0006's rule, re-run |
| T12 | skip, unexported, no tag | found: a tagged unexported field was silently ignored |
| T13 | `default=aGk=` on `[]byte` | four bytes `aGk=`, ADR-0007's edge re-measured |
| T14 | the tag key pointed at `json` | 3 of 4 fields refuse |
| T14b | what the key costs #16's cache | 12 ns against 18 ns, both hashable |
| T15 | `Validate[T]()` | from a real `go test`, no value and no plane |
| T16 | ADR-0003's properties under one escape rule | 200k paths, 0 failures, 0 collisions |
| T17 | xload's `prefix=` | nesting, or ADR-0004's `Under`; concatenation unexpressible |
| T18 | a name per direction | a round-trip violation by construction |
| T19 | end to end, hostile names | 8 hostile segments through the real YAML driver, all exact |
| T20 | how json/v2 escapes a name | **it cannot**: a quoted name is refused at the name position |
| T20b | `inline` against `embed` on go1.27rc2 | `inline` is a no-op, visible only on a NAMED field |
| T20c | v2 and an unknown option | silently ignored; only near-misses of its six are rejected |
| T20d | v2's quoted token | wired to `format:` only, which 1.27 removed |
| T21 | the single-quote model | one missing backslash makes the tag invisible |
| T22 | the mistake the grammar cannot see | a bare option word in the name position |
| T23 | three diagnostic tiers | one mistake reports once |
| T24 | an empty default against no default | `default=` overwrites a seed; no default leaves it |
| T25 | where each option is admissible | `omitzero` everywhere; the other two are ADR-0006's |
| T26 | a load option on Dump, a dump option on Load | neither leaks |
| T27 | the embedding cases T9 did not reach | found three defects, see below |
| T28 | promotion round-tripped rather than compiled | equal, after the walk was made to share the field rule |

## What overturned a draft answer

- The escape was drafted as json/v2's single-quoted string after reading its
  source. T20 measured that v2 refuses a quoted name, and T21 measured that
  the model needs `\\'` in the tag source and vanishes at `\'`.
- `planField` skipped unexported fields before reading the tag, so a tagged
  unexported field was ignored in silence. T12.
- `maps no address` fired alongside a grammar error, so one mistake reported
  as two. That is the third diagnostic tier.
- T20b's first version put `,embed` and `,inline` on an ANONYMOUS field, where
  v2 promotes either way, so it measured nothing and looked like a pass.
- A promoted embedded POINTER compiled clean, loaded into a nil pointer with a
  nil error, and dumped through one. Now refused. T27.
- An embedded UNEXPORTED struct type was skipped, dropping a mapped field in
  silence, though reflect can set through it. Now promoted. T27.
- The walk did not share the compiler's field rule, so the schema promised an
  address the walk never visited. T27, fixed and round-tripped in T28.
