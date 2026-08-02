# proto/19-registration

Throwaway prototype for [#19](https://github.com/onhotpath/ferry/issues/19), "Typed codec registration for types ferry does not own".
Never merges.

Built on `proto/12-codec-chain`, which is built on `proto/7-type-set`, which is built on `proto/5-source-sink`.
So every probe runs against a real `Path`, a real `Value`, four real drivers, a real YAML plane over real files, the type set ADR-0005 landed, and the codec chain ADR-0007 landed.
`README.md` is #12's own README and `README-proto7.md` is #7's; both are kept because their probe lists are still the map of the inherited files.

Run: `P19=all GOTOOLCHAIN=go1.27rc2 go run .`
One probe: `P19=6 GOTOOLCHAIN=go1.27rc2 go run .`
R13 wants the race detector: `P19=13 GORACE=halt_on_error=0 GOTOOLCHAIN=go1.27rc2 go run -race .`
#12's probes still run: `P12=all go run .`; #7's are the default with no variable set.

## What is new here

| file | what it is |
| --- | --- |
| `r_registry.go` | the registration and the identity table as a value: `TypeCodec`, `StringCodec`, `TextCodec`, `Registry`, and the `install` seam that lets three lifetimes be run against each other |
| `r1_callsite.go` | ten registrations written the way a user writes them, and the shapes that do not compile |
| `r2_kinds.go` | the declared kind against the accepted kinds, and ADR-0006's `Null` escape hatch discharged |
| `r3_refusals.go` | what registration refuses, what it must not refuse, and what an interface registration does and does not claim |
| `r4_dynamic.go` | dynamic registration by `reflect.Type`: what only it can express, and what it costs |
| `r5_predicate.go` | a predicate arm, and why ADR-0005's named-duration hole does not want one |
| `r6_lifetime.go` | R6, R7 and R8: global-and-mutable, scoped, and frozen-at-first-compile |
| `r9_decline.go` | decline and fall through, against json/v2's real mechanism re-measured on go1.27rc2 |
| `r10_proof.go` | the round-trip triple over a registration, and whether a proof can be required |
| `r11_mapkey.go` | a registered key codec, and where the injectivity obligation is communicated |
| `r12_schema.go` | the codec resolved into the compiled schema, and what that obliges the registry to guarantee |
| `r13_race.go` | registration racing a compile, under `-race` |
| `r14_defect.go` | the three defects this prototype found |
| `r15_audit.go` | a registered type in every composite position, at zero and populated, through all three planes |

Changes to inherited files, all load-bearing:

- `typeset.go`: `byIdentity` reads now go through `identityLookup`, which consults core's pre-seeded table and then the installed registry, so "registration is an entry in the same identity table" is the mechanism rather than a comment. `validMapKey` gained R11's `keyOptIn` seam.
- `chain.go`, `walk.go`, `p12_halfpair.go`, `chainorder.go`: the same lookup change.
- `p18_registration.go`: #12's own registration probe, with its duplicated definitions removed and pointed at `r_registry.go`. It still runs and still produces #12's results.

## Probes

| # | question | result |
| --- | --- | --- |
| R1 | the call site, and inference | **all ten registrations compile with no explicit type argument.** Five are one line, inferring `T` from a method expression and a package parse function. Every wrong shape is a build error naming `T`; a method expression needs a **value** receiver, so `url.URL` and `big.Int` cost a wrapper |
| R2 | the declared kind, and the accepted kinds | the declared kind is the **donation target** and nothing else. ADR-0006's line-194 escape hatch works, measured: a registered codec accepts `Null` at a Go `int` and returns 0 where a plain `int` refuses. **`StringCodec` cannot express it**, because its decode half calls `AsString`, which is the measured reason the API is two constructors |
| R3 | what a registration may not be | a type core owns, a duplicate, and a **pointer type** are refused; a named type over one core owns must not be. With the pointer check removed, #12's P3 defect returns in full: a nil `*big.Int` dumps `string("<nil>")`. Registration **beats** a text pair (`slog.Level` `string("WARN")` -> `number("4")`). An interface registration claims the interface and **not** the concrete type |
| R4 | static against dynamic | dynamic reaches exactly one thing statics cannot: a type with no name at a call site (`reflect.StructOf`). It costs both compile-time guarantees, measured as two panics inside ferry on third-party code. go.dev/issue/73457 re-checked 2026-08-02: **still open**. ADR-0005's named-duration hole has a typed one-liner instead: `DurationLike[T ~int64]()` |
| R5 | a predicate arm | the predicate that rescues a named duration eats **every** named `int64`: `R5Port(30000000000)` becomes `"30s"`. Two predicates matching one type is precedence by **registration order**, which is `init()` order, which ADR-0007 already refused. Cost is not the argument: 13 ns map hit against 20-43 ns scanned |
| R6 | lifetime: global and mutable | **a schema compiled before a registration is stale**, three ways. A cached refusal is loud. A cached codec resolved at compile time is **silent**: the plane holds the representation the user replaced, no error. And the cached address set is `[/B/Host /B/Port]` where the dump writes `[/B]`, so ADR-0003's prefix-free check and the driver's key function both ran over a set that does not exist |
| R7 | lifetime: scoped | keying the schema cache by `reflect.Type` alone is ADR-0004's `EnvSource{Sep}` defect one layer up. Keying by the registry **value** panics, `hash of unhashable type`. Keying by the registered **type set** collides, because the codec is the part that differs and funcs are not comparable. Keying by the registry **pointer** works, **and only if the registry is frozen**. A per-call registry gives 10000 non-evictable cache entries |
| R8 | lifetime: global, frozen | removes R6 entirely and costs two things: two tests cannot want different codecs for one type (this prototype could not have been written), and the freeze POINT is decided by `init()` order. Its one real advantage, zero configuration, is answerable by a **default registry** that is a registry |
| R9 | decline and fall through | re-measured on go1.27rc2: **`SkipFunc` is not exported**, the signal is `errors.ErrUnsupported`, and only the streaming shape may use it (`MarshalFunc` errors with "may not return errors.ErrUnsupported"). Ported to ferry, a value-dependent claim gives **one type, one schema, two address sets**. On Load it has no answer at all, because ferry holds one `Value` at one address where v2 holds the document |
| R10 | the proof | the harness needs **no accessor** on a registration: it exercises the codec through the ordinary walk, so `Reg` stays opaque. The triple catches three distinct failures the pair alone does not. Requiring a proof puts a testing import in `main`, still does not close the hole (a one-case proof passes the lossy codec), and moves a CI failure into production. **A registry is enumerable**, so ADR-0005's completeness check ports to a user's registry, which the text arm's set structurally cannot |
| R11 | a key codec | under the implied rule a non-injective registered key silently drops a map entry, which is #31's defect in user code. Opt-in makes it a **schema compile error naming the type**, which is the only moment a registrant is guaranteed to read. Implying it from the declared kind excludes nothing: the non-injective codec declares `String` |
| R12 | the compiled schema | resolving the codec to a function pointer is 283 ns against 381 ns for six leaves, which is **not** the argument. The argument is that it obliges exactly one thing: once a type is resolved against a registry, that registry's answer must never change. So "resolve into the schema" and "freeze at first use" are one decision |
| R13 | racing a compile | a mutable registry read by a compile is a **reported data race**: `mapassign` in `Register` against `mapaccess2` in `identityLookup` from `classify`. A frozen registry needs no synchronisation on the read path, which is the shape ADR-0004 already measured at 8.8 ns against 20.0 ns with a mutex |
| R14 | **three defects** | see below |
| R15 | the audit | a registered type at a leaf, behind a pointer, in a slice, in an array, as a map value, as a **map key**, in a nested struct and as an interface, populated and **all-zero**, through the memory plane, real YAML and the flattening plane. All pass but one, and the one is ADR-0005's own documented limit: a nil pointer cannot cross a plane with no null |

## Three defects this found

The first is in this prototype's own headline example, and the other two are in the one piece of reflection the registration API owns.

- **`String()` is not an inverse at the zero value, for three of the five types the one-liner is most attractive for.**
  `netip.Addr{}.String()` is `"invalid IP"` and `netip.ParseAddr("invalid IP")` fails; `netip.AddrPort` and `netip.Prefix` are the same.
  Their `MarshalText` pairs all give `""` and round-trip.
  R1 presented `StringCodec(netip.Addr.String, netip.ParseAddr)` as the ergonomic best case and R10's harness failed it on its first case.
  Worse, because registration beats the text pair, **registering it makes the type worse than not registering it**: the chain already claimed `netip.Addr` correctly.
  This is ADR-0005's `fmt.Stringer` refusal handed back to the user by hand, and the answer is that core ships `TextCodec[T](kind)` and `StringCodec`'s doc names the zero value.
- **The generic wrapper panics on a nil interface, in the encode half.**
  `v.Interface().(T)` where `T` is an interface and the field is nil: `interface conversion: interface is nil, not net.Addr`, inside ferry, before the user's codec runs.
  Fixed by comma-ok, measured at 4 ns against 6 ns for the bare assertion.
- **And in the decode half.**
  `dst.Set(reflect.ValueOf(out))` for a nil `out`: `reflect: call of reflect.Value.Set on zero Value`.
  Fixed by `dst.Set(reflect.ValueOf(&out).Elem())`.

The second and third matter more than a bug fix, because the wrapper exists precisely so a registrant never writes a `reflect.Value`.
A defect in it is a defect in every codec ever registered, and no proof a registrant can write catches it, because the codec itself was correct.
Both were found by R15's audit fixture, which is the first in three prototypes to dump a registered interface at its zero value.
