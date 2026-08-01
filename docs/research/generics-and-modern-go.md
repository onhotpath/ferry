# Generics and modern Go: what ferry's API can do that xload's could not

Research ticket: xload was designed before Go had generics.
What can ferry's API do that xload could not?

All measurements below were taken on this machine (Apple M1 Pro, `go1.26.5 darwin/arm64`) against `github.com/gojekfarm/xtools/xload` at commit [`a90b3aa`](https://github.com/gojekfarm/xtools/commit/a90b3aad2133248cec50f6b4d6e37b0d9e788adb).
Every source link in this document is a permalink to that same commit, so the quoted line numbers stay correct as xtools moves on.
Reproduction code for each measurement is described inline.

**Go 1.27 amendment (2026-07-31).**
Sections marked with 1.27 claims were re-verified against a real `go1.27rc2 darwin/arm64` toolchain installed locally via `golang.org/dl/go1.27rc2`, its `api/go1.27.txt`, and its `src/` tree.
Go 1.27 is **not GA at time of writing** - `go.dev/dl` lists `go1.27rc2` as not stable.
Stdlib links pinned to `refs/tags/go1.27rc2` are release-candidate source and can still change before GA; everything sourced from them is marked **(1.27 RC)**.
Claims sourced from `tip.golang.org/doc/go1.27` alone, with no toolchain confirmation, are marked **(draft notes)**.

## Summary

- **Generics do not remove the reflection walk, and it is worth saying so up front.**
  A struct-tag-driven mapper must use `reflect` to enumerate and set fields of arbitrary user structs, and Go has no way to constrain a type parameter to "any struct" (verified, not assumed).
  What generics genuinely buy is a **typed skin over a reflective core**: `Load[T]` makes `ErrNotPointer` unrepresentable, `reflect.TypeFor[T]()` lets the schema be compiled and validated from the type alone with no value in hand, and typed codec registration moves the `any` round trip from per-field-per-call to once at registration.
- **The real win is not generics, it is schema caching, which xload has none of.**
  A 12-key struct costs xload **3343 ns / 2992 B / 54 allocs** per `Load`.
  A ~90-line prototype that compiles the schema once and caches it by `reflect.Type` does the same work in **476 ns / 320 B / 5 allocs**: 5.5x time and 10x allocations against the fairer `SkipCollisionDetection` baseline.
  This is available to ferry with or without generics.
  Compiling the schema up front also makes the full key set known **before any I/O**, which unlocks batch/snapshot sources - a capability xload structurally cannot have (section 5.13).
- **xload's serial and concurrent walks already produce different results for the same input**, silently and with no error, and differ again in how many errors they report.
  Both reproduced below.
  xload has an equivalence test for exactly this that cannot catch it, because both subtests share one destination pointer.
- **The stdlib has moved a long way since xload's design.**
  `errors.Join` (1.20), `sync.OnceValue` (1.21), `reflect.TypeFor` (1.22), `iter.Seq2` and `slices.Sorted`/`maps.Keys` (1.23), `reflect.TypeAssert` (1.25), and `reflect.Type.Fields()` / `Value.Fields()` (1.26) all post-date it and all bear on things xload does badly.
  **`encoding/json/v2` and `encoding/json/jsontext` are GA in Go 1.27** (verified on `go1.27rc2`: both appear in `api/go1.27.txt`, and both import and run with no `GOEXPERIMENT` set).
  This **corrects** an earlier claim in this document that nothing in json/v2 is importable.
  Most of it now is: the whole `encoding/json/v2` and `encoding/json/jsontext` exported surface, including `GetOption`.
  What remains closed is narrower and still real - the struct field resolver is unexported, and `Options` is still sealed with `internal.NotForPublicUse` (section 2).
  Setting `GOEXPERIMENT=nojsonv2` makes both packages unimportable again, which is a cost any dependant inherits (section 3, "What first-class `encoding/json/v2` could concretely mean").
- **Typed values at the boundary are the highest-leverage departure from xload, but the honest justification is Dump, not Load.**
  Measured: a typed dump-then-load round trip is exact; a stringified one turns `port: 8080` into `port: "8080"` permanently.
  Load survives strings because the struct field type drives parsing.
  Two premises worth puncturing before the ADR: most backends (Consul, env, query params) have **no** type information to preserve, and every lossless design surveyed - including `encoding/json/v2` in 2026 - converged on **tagged text**, not native numbers.

## 1. Where generics genuinely remove reflection, and where reflection is unavoidable

### Reflection is unavoidable for the walk itself

Three things a struct-tag mapper does have no non-reflective expression in Go:

1. **Enumerating fields of an arbitrary type.**
   `reflect.Type.NumField` / `Field(i)` is the only mechanism.
   Go has no compile-time metaprogramming, no `for field := range T`, no type-level field list.
2. **Reading struct tags.**
   `reflect.StructField.Tag` is the only access path.
   Tags are opaque strings to the compiler; `go vet`'s `structtag` check is the only static validation and it only checks well-formedness, not semantics.
3. **Setting a field whose type is not known at the call site.**
   `reflect.Value.Set*` is the only mechanism.
   A generic `func set[T any](p *T, v T)` requires `T` at the call site, which is exactly what you do not have midway through a walk.

Generics cannot help with any of these, and it is worth being blunt about why: **Go has no constraint that means "any struct type."**
Verified by compiling `type structish interface{ ~struct{} }` on go1.26.5 - it compiles as a constraint but `~struct{}` matches only types whose underlying type is the *empty* struct, and using it outside constraint position fails with `cannot use type structish outside a type constraint`.
There is no wildcard form.
So `Load[T any]` cannot reject `Load[int]` at compile time; `ErrNotStruct` stays a runtime error.

The only way to remove reflection from the walk is code generation, which is a different product with a different distribution story (a `go:generate` step, generated files in user repos, and a build-time dependency).
`goccy/go-json`-style opcode compilation and `encoding/json/v2`-style cached field tables both keep reflection but pay for it once per type; that is the realistic target.

### Where type parameters genuinely help

**(a) `Load[T]` deletes an error case.**
xload's entry point is `Load(ctx context.Context, v any, opts ...Option) error` ([load.go:37](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L37)), which immediately does two runtime kind checks and can return `ErrNotPointer` ([load.go:74-76](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L74-L76)) or `ErrNotStruct` ([load.go:79-81](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L79-L81)).
A signature of `Load[T any](ctx context.Context, dst *T, opts ...Option) error` makes `ErrNotPointer` structurally impossible: one of xload's three package-level sentinels ([load.go:15-22](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L15-L22)) stops existing.
`ErrNotStruct` survives, per the constraint limitation above.
This is a small win but it is real and free.

**(b) `reflect.TypeFor[T]()` removes the value-to-type detour and enables value-free compilation.**
xload can only reach the type through a value: `reflect.ValueOf(obj)` then `.Elem().Type()` ([load.go:72-83](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L72-L83)).
`reflect.TypeFor[T]()` ([reflect/type.go:1388](https://cs.opensource.google/go/go/+/refs/tags/go1.26.0:src/reflect/type.go) in go1.26.5, added in Go 1.22) gives the `reflect.Type` from the type parameter with no value at all.
That matters for more than tidiness: it means a schema can be compiled, validated, and cached at construction time rather than on first load, so a `ferry.New[Config](...)` constructor can return tag-grammar errors *before* any I/O happens.
xload cannot do this - a malformed tag such as an unknown option is only discovered mid-walk, on the first `Load`, after some fields have already been set ([load.go:100-103](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L100-L103) calling [parseField](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L218)).

`reflect.Type` values are comparable and canonical (identical types yield identical `Type` values), verified locally: `reflect.TypeFor[S]() == reflect.TypeOf(S{})` is `true`.
That is what makes them safe map keys for a schema cache, and it means there is no invalidation problem - a `reflect.Type` never changes meaning.

**(c) Typed decoder/encoder registration moves the `any` to registration time.**
xload's extension point is `Decoder interface { Decode(string) error }` ([load.go:403-406](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L403-L406)), discovered by a five-arm type switch on `field.Interface()` executed **per field, per call** ([load.go:414-439](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L414-L439)), plus a second near-identical switch in `hasDecoder` on the async path ([async.go:222-235](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/async.go#L222-L235)).
The precedence order (`Decoder` > `encoding.TextUnmarshaler` > `json.Unmarshaler` > `encoding.BinaryUnmarshaler` > `gob.GobDecoder`) is hardcoded and not configurable.
There is no way at all to register a decoder for a type you do not own: `time.Duration` is handled by a **string comparison on the type name**, `if ty.String() == "time.Duration"` ([load.go:301](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L301)).

With generics the user-facing API becomes typed and compile-time checked:

```go
ferry.WithDecoder(func(v ferry.Value) (time.Duration, error) { ... })
```

The internal registry is still `map[reflect.Type]func(reflect.Value, Value) error`, and the generic constructor closes over the typed function and does the `reflect.Value` -> `*T` conversion **once at registration** instead of per field per call.
This is exactly the shape `encoding/json/v2` chose with `json.MarshalToFunc[T]` / `json.UnmarshalFromFunc[T]` (see section 2).
It is a genuine seam: it removes an `any` from the user's signature, removes five interface assertions per field from the hot path, and makes third-party types extensible without owning them.
It does not remove reflection - the registry is keyed by `reflect.Type` and the call site still holds a `reflect.Value`.

**(d) Typed accessors on a `Value` union are mostly cosmetic.**
If ferry's boundary value is a tagged union (section 4), a generic `As[T](v Value) (T, error)` reads nicely but the failure mode is still runtime.
Generic *constructors* are slightly better because inference works: `ferry.Of(42)` needs no annotation.
Do not oversell this; it is ergonomics, not safety.

**(e) `reflect.TypeAssert[T]` - measured, and it is not the win it looks like.**
Go 1.26 added `func TypeAssert[T any](v Value) (T, bool)`, documented as "semantically equivalent to `v.Interface().(T)`" ([reflect/value.go:1515-1518](https://cs.opensource.google/go/go/+/refs/tags/go1.26.0:src/reflect/value.go)).
The obvious pitch is that it avoids boxing.
Benchmarked on go1.26.5/M1 Pro:

| case | `v.Interface().(T)` | `reflect.TypeAssert[T]` |
| --- | --- | --- |
| concrete struct target | 3.38 ns, 0 allocs | 3.67 ns, 0 allocs |
| `time.Duration` target | 3.37 ns, 0 allocs | 2.57 ns, 0 allocs |
| **interface target** (the xload case) | 4.50 ns, 0 allocs | 7.29 ns, 0 allocs |

For the case ferry actually cares about - asserting to an *interface* like `Decoder` or `encoding.TextUnmarshaler` - `TypeAssert` was **62% slower** in my benchmark and neither form allocated.
Use it for clarity where it fits, but do not build a performance argument on it.
The way to make the decoder lookup cheap is to resolve it once at schema-compile time and store a function pointer in the leaf, not to micro-optimise the assertion.

**(f) `errors.AsType[E error]` (Go 1.26) is the cleanest generics win of the lot.**
`func AsType[E error](err error) (E, bool)` replaces the `var e *T; errors.As(err, &e)` two-step, and the [go1.26 release notes](https://go.dev/doc/go1.26) describe it as "type-safe, faster, and, in most cases, easier to use."
It is a genuine correctness improvement for a library with a rich error taxonomy, because the constraint `E error` makes the value-versus-pointer footgun reproduced in section 5.14 unrepresentable: you cannot ask for a type that does not implement `error`.
This costs ferry a `go 1.26` directive and nothing else, and recommendation 10 now argues for a `go 1.27` floor anyway, which subsumes it.

### Honest scorecard

| Seam | Generics help? |
| --- | --- |
| Walking fields of arbitrary structs | No. Reflection or codegen only. |
| Reading struct tags | No. |
| Setting a field mid-walk | No. |
| Entry-point pointer check | Yes, `ErrNotPointer` disappears. |
| Entry-point struct check | No, Go cannot constrain to "any struct". |
| Compiling a schema before any value exists | Yes, via `reflect.TypeFor[T]()`. |
| Decoder/encoder registration for third-party types | Yes, substantially. Typed user API, one assertion at registration. |
| Per-type memoization | No. Generics give no per-instantiation package state (package-level generic variables are a syntax error, verified). A `sync.Map` keyed by `reflect.Type` is still the mechanism. |
| Typed value accessors | Cosmetic. |
| Error inspection (`errors.AsType`) | Yes. Type-safe, and forecloses a real footgun. Costs a `go 1.26` directive. |
| `reflect.TypeAssert` on the hot path | No. Measured neutral-to-worse (1e). |

## 2. State of the art for compiled/cached reflection schemas

### The dominant idiom: `sync.Map` keyed by `reflect.Type`

Every type-schema cache in the Go standard library uses the same shape.
Verified by reading `$(go env GOROOT)/src` on go1.26.5:

```
encoding/json/encode.go:379         var encoderCache sync.Map        // map[reflect.Type]encoderFunc
encoding/json/encode.go:1329        var fieldCache sync.Map          // map[reflect.Type]structFields
encoding/json/v2/arshal.go:540      var lookupArshalerCache sync.Map // map[reflect.Type]*arshaler
encoding/json/v2/arshal_funcs.go:84 fncCache sync.Map                // map[reflect.Type]arshaler
encoding/gob/type.go:40             var userTypeCache sync.Map       // map[reflect.Type]*userTypeInfo
encoding/xml/typeinfo.go:47         var tinfoMap sync.Map            // map[reflect.Type]*typeInfo
encoding/binary/binary.go:692       var structSize sync.Map          // map[reflect.Type]int
```

Eight out of eight use `sync.Map`; none uses a mutex plus map.
The access pattern is always `Load` -> build on miss -> `LoadOrStore`, and **duplicate work on a race is explicitly accepted**.
`encoding/gob/type.go:45-47` states the philosophy:

> Construct a new userTypeInfo and atomically add it to the userTypeCache.
> If we lose the race, we'll waste a little CPU and create a little garbage
> but return the existing value anyway.

This is viable because `reflect.Type` values are canonical and comparable.
[`reflect/type.go:38-39`](https://pkg.go.dev/reflect#Type): "Type values are comparable, such as with the `==` operator, so they can be used as map keys."
Verified locally: `reflect.TypeFor[S]() == reflect.TypeOf(S{})` is `true`.

**There is no invalidation story anywhere, and none is needed.**
No library surveyed implements eviction: not `sqlx/reflectx`, not `validator`, not `json/v2`.
The cache is bounded by the set of types the program statically declares, so it converges after warmup.
The one leak vector is runtime-generated types via `reflect.StructOf`; nobody guards against it.

### The refinement: cheap outer cache, expensive work behind a per-entry `sync.Once`

The most important pattern for ferry is the two-level one, because a naive `sync.Map` still lets N racing goroutines each run the expensive field walk.

`encoding/json/v2` solves it by making the cached value cheap to build and the field resolution lazy.
`lookupArshaler` ([arshal.go:540-554](https://cs.opensource.google/go/go/+/refs/tags/go1.26.0:src/encoding/json/v2/arshal.go)) races freely, because `makeStructArshaler` ([arshal_default.go:1056-1078](https://cs.opensource.google/go/go/+/refs/tags/go1.26.0:src/encoding/json/v2/arshal_default.go)) only allocates closures:

```go
func makeStructArshaler(t reflect.Type) *arshaler {
	var fncs arshaler
	var (
		once    sync.Once
		fields  structFields
		errInit *SemanticError
	)
	init := func() { fields, errInit = makeStructFields(t) }
	fncs.marshal = func(...) error {
		...
		once.Do(init)
```

`makeStructFields` is a BFS over embedded structs with tag parsing, dominance resolution, two sorts and two map builds.
It only fires on the first actual marshal, so arshalers that lose the `LoadOrStore` race are discarded before their `once` ever runs.
The thundering herd never touches the expensive path.

`encoding/json` v1 uses `sync.Map` + `sync.OnceValue` directly, and does it for a second reason - **recursive types** ([encode.go:388-412](https://cs.opensource.google/go/go/+/refs/tags/go1.26.0:src/encoding/json/encode.go)):

```go
indirect := sync.OnceValue(func() encoderFunc { return newTypeEncoder(t, true) })
fi, loaded := encoderCache.LoadOrStore(t, encoderFunc(func(e *encodeState, v reflect.Value, opts encOpts) {
	indirect()(e, v, opts)
}))
if loaded { return fi.(encoderFunc) }
f := indirect()
encoderCache.Store(t, f) // collapse the indirection after building
```

A placeholder closure is installed **before** the real encoder exists, so a self-referential type's inner lookup finds the indirection instead of recursing forever.
The final `Store` collapses the indirection so steady-state lookups pay no closure hop.

Worth knowing: this code used a `sync.WaitGroup` until Go 1.25.
It was changed by [golang/go commit 68bc0d84e9dd](https://go-review.googlesource.com/c/go/+/673335) (fixes [go.dev/issue/73733](https://go.dev/issue/73733)), and the commit message is the clearest primary statement of what `OnceValue` buys:

> Use a sync.OnceValue rather than a sync.WaitGroup to coordinate access to encoderCache entries.
> The OnceValue better expresses the intent of the code (we want to initialize the cache entry only once).
> However, the motivation for this change is to avoid testing/synctest incorrectly reporting a deadlock when multiple bubbles call Marshal at the same time.
> Goroutines blocked on WaitGroup.Wait are "durably blocked"... Goroutines blocked on OnceValue are not durably blocked, avoiding the problem.

Note that the motivation was `testing/synctest` correctness, **not** performance.
Do not claim a speedup from `OnceValue`.

`sync.OnceValue` is not a cache, it is a memoized thunk ([sync/oncefunc.go:41-71](https://cs.opensource.google/go/go/+/refs/tags/go1.26.0:src/sync/oncefunc.go)).
One property matters for schema compilation: it **re-panics the same value on every subsequent call** if `f` panicked.
A malformed tag that panics once panics identically forever, rather than the first caller panicking and later callers silently receiving a zero schema.

| Mechanism | Solves | Cost | Duplicate work on race? |
| --- | --- | --- | --- |
| `sync.Map` keyed by `reflect.Type` | concurrent storage, read-mostly | hash-trie walk + `any` unbox | Yes |
| `sync.Mutex` + map | storage plus exclusion | every read serializes | No |
| `atomic.Value` over a copy-on-write map | lock-free reads | O(n) copy per write | No |
| `sync.OnceValue` | exactly-once init of one entry | one atomic load after first call | No |
| `[]atomic.Pointer[T]` indexed by type address | storage without hashing | one shift, one atomic load | Yes |

### The second idiom: copy-on-write map behind `atomic.Value`

`go-playground/validator` ([cache.go:33-71](https://github.com/go-playground/validator/blob/master/cache.go#L33-L71)):

```go
type structCache struct {
	lock sync.Mutex
	m    atomic.Value // map[reflect.Type]*cStruct
}

func (sc *structCache) Set(key reflect.Type, value *cStruct) {
	m := sc.m.Load().(map[reflect.Type]*cStruct)
	nm := make(map[reflect.Type]*cStruct, len(m)+1)
	for k, v := range m { nm[k] = v }   // full copy
	nm[key] = value
	sc.m.Store(nm)
}
```

The read path is one `atomic.Value.Load` plus a plain map lookup - strictly cheaper than `sync.Map.Load`, which since Go 1.24 walks a hash-trie.
The write path is an O(n) copy of the whole map under a mutex.
This is correct only because the key set converges; it would be pathological for churning keys.
`goccy/go-json` uses the same shape for its slow-path fallback ([compiler.go:75-84](https://github.com/goccy/go-json/blob/master/internal/encoder/compiler.go)).

Note validator's zero value is unusable - `m` must be primed with `sc.m.Store(make(map[reflect.Type]*cStruct))` in `New()` or `Get` panics.

### The pattern that matters most: compile *behaviour* into the schema, not just data

This recurs in every high-quality library and it is the single most transferable idea:

- `json/v2` stores `structField.fncs *arshaler` per field ([fields.go:70](https://cs.opensource.google/go/go/+/refs/tags/go1.26.0:src/encoding/json/v2/fields.go)), and `isZero`/`isEmpty` as `func(addressableValue) bool` resolved at schema-build time (fields.go:71-72, assigned at 226-249).
  The "does this type have an `IsZero()` method, and is it on the value or pointer receiver" question is answered **once**, not per call.
- `validator` stores `cTag.fn FuncCtx` - the validator function pointer resolved at parse time ([cache.go:87-101](https://github.com/go-playground/validator/blob/master/cache.go#L87-L101)).
- `goccy/go-json` compiles an entire opcode program per type.

This maps directly onto xload's worst hot-path cost: the five-arm interface type switch in `decode` run per field per call ([load.go:424-435](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L424-L435)).
Resolved once at compile time it becomes a stored function pointer and a nil check.

Adjacent precomputation tricks worth stealing from `json/v2`'s `fields.go`:

- **Precompute the wire form.** `fieldOptions.quotedName` is stored pre-quoted and pre-escaped so the marshal hot path is `b = append(b, f.quotedName...)`, skipping the encoder state machine (arshal_default.go:1148-1149). ferry's dump path should store the fully-resolved key string per leaf, not recompute `prefix + key`.
- **Precompute lookup indices.** `structFields` holds `flattened []structField`, `byActualName map[string]*structField`, and a pre-folded `byFoldedName` for case-insensitive lookup (fields.go:78-349).
- **Index splitting.** `reindex()` (fields.go:38-57) splits `index []int` into `index0 int` plus a remainder slice "to avoid bounds check during runtime", and nils the remainder when empty "to avoid pinning the backing slice".
- **`omitzero` checked before the marshaler runs** (arshal_default.go:1097-1100), whereas `omitempty` may require marshaling then unwriting (1115-1121). A real asymmetry worth copying if ferry adopts both.

### What is and is not reusable

**Corrected for Go 1.27.**
This subsection previously read "`encoding/json/v2` is a closed system... zero reuse", and "there is just nothing to import."
That was true while the package was `GOEXPERIMENT`-gated.
It is **no longer true**: in Go 1.27 both `encoding/json/v2` and `encoding/json/jsontext` are ordinary importable stdlib packages under the Go 1 compatibility promise.
The accurate statement is narrower, and the narrower part still bites.

**Verified on `go1.27rc2`:**

- `grep -c "^pkg encoding/json/v2," api/go1.27.txt` returns **45**; `encoding/json/jsontext` returns **113**; `encoding/json` gains **21**.
  A package listed in `api/go1.NN.txt` has passed the Go release API gate and is covered by the compatibility promise.
- A module with `go 1.27` importing both packages builds and runs with `GOEXPERIMENT` unset (verified by running it).
- `GOEXPERIMENT=nojsonv2` makes both packages vanish again: `imports encoding/json/v2: build constraints exclude all Go files`.
  This is not hypothetical - `nojsonv2` is the release notes' documented escape hatch for compatibility problems, so any dependant that trips over v1/v2 differences and reaches for it takes ferry's build down too.

**Still closed, and this is the part that matters to a mapper:**

- `structFields`, `structField`, `fieldOptions`, `makeStructFields`, `parseFieldOptions`, `foldName`, `lookupArshaler` remain unexported in 1.27 ([fields.go](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/v2/fields.go), [arshal.go:527-529](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/v2/arshal.go)).
  **ferry still writes its own field resolver.** This is the single most consequential "no reuse" fact and 1.27 does not change it.
- `Options` is still sealed.
  [`jsonopts/options.go:14-18`](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/internal/jsonopts/options.go) is unchanged:

  ```go
  type Options interface {
      // JSONOptions is exported so related json packages can implement Options.
      JSONOptions(internal.NotForPublicUse)
  }
  ```

  Verified empirically: a package outside the json tree that tries to implement it fails at `use of internal package encoding/json/internal not allowed`.
  [go.dev/issue/77703](https://go.dev/issue/77703) ("exported read-write aggregate Options type") is **closed**, so this is settled, not pending.
- **New nuance, and it partially softens the above:** `GetOption` *is* exported and works from outside.
  `func GetOption[T any](Options, func(T) Options) (T, bool)` lets third-party code **read** any option out of an opaque `Options` by passing the option constructor itself as the key.
  Verified: `json.GetOption(opts, json.Deterministic)` returns `(true, true)`, `json.GetOption(opts, jsontext.WithIndent)` returns `("  ", true)`, and an unset option returns `ok == false`.
  So the sealing is "you cannot **define** an option", not "you cannot **inspect** options".
  For ferry, that means a ferry codec that wraps a json/v2 codec can read the caller's json options; it cannot add its own to the same bag.

**`jsontext` exports more than this document previously credited, and some of it is directly relevant.**
The old text said it exports "nothing about Go struct fields or tags", which is still true, but it undersold the syntax layer.
Worth ferry's attention ([`api/go1.27.txt`](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:api/go1.27.txt), verified):

- **`jsontext.Pointer`**, an RFC 6901 JSON Pointer as a `string` newtype, with `Tokens() iter.Seq[string]`, `Parent()`, `Contains(Pointer) bool`, `AppendToken(string) Pointer`, `LastToken()`, `IsValid()`.
  Its godoc ([state.go:82-95](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/jsontext/state.go)) states the property ferry's flat key space lacks: "There is exactly one representation of a pointer to a particular value, so comparability of `Pointer` values is equivalent to checking whether they both point to the exact same value."
  It also states the limitation ferry would inherit: "It is impossible to distinguish between an array index and an object name (that happens to be a base-10 encoded integer) without also knowing the structure of the top-level JSON value."
  This is the closest thing in the stdlib to a **structured key path** and it is the obvious model to weigh against xload's flat delimiter-joined strings (5.10, and the measured `Flatten` nondeterminism in section 4).
- **`jsontext.Token`** is a closed union that stores decoded scalars as **raw text plus a discriminator**, exactly the "tagged text" shape section 4 recommends.
  Its own comment table ([token.go:56-86](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/jsontext/token.go)) enumerates the raw-versus-exact forms per kind.
  Its accessors return `(T, error)` - `Token.Int() (int64, error)`, `Float()`, `Uint()` - not panics, which is the convention section 4 argues for against `cty` and `protoreflect`.
- **`jsontext` does not import `reflect`.**
  Verified with `go list -deps encoding/json/jsontext` on `go1.27rc2`: the output contains `internal/reflectlite` (pulled in by `errors`) and no `reflect`.
  That confirms the syntactic/semantic split this document cites in section 3 as a template for ferry's own plane/codec versus struct-mapping layering.

The extension point remains `MarshalToFunc[T]` / `UnmarshalFromFunc[T]` plus `JoinMarshalers` / `WithMarshalers` ([arshal_funcs.go](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/v2/arshal_funcs.go)), backed by its own `sync.Map` cache that also caches *negative* lookups (an explicit `nil` for "no funcs apply").
That is per-Go-type behaviour override riding json/v2's cache.
It is a **design template for ferry's typed decoder registration** (section 1c), and it is now also a real dependency ferry could take, at the cost analysed in section 3.

The code is BSD-licensed, so copying the design is fine; the difference from before is that importing is now also an option.

**The only exported, general-purpose field mapper in the Go ecosystem is `github.com/jmoiron/sqlx/reflectx`** - and it is the weakest design of the five surveyed ([reflectx/reflect.go:62-113](https://github.com/jmoiron/sqlx/blob/master/reflectx/reflect.go#L62-L113)):

```go
func (m *Mapper) TypeMap(t reflect.Type) *StructMap {
	m.mutex.Lock()
	mapping, ok := m.cache[t]
	if !ok {
		mapping = getMapping(t, m.tagName, m.mapFunc, m.tagMapFunc)
		m.cache[t] = mapping
	}
	m.mutex.Unlock()
	return mapping
}
```

A full `sync.Mutex` held across the entire BFS, not an RWMutex, not double-checked; every cache hit serializes on one mutex.
The upside is that there is no thundering herd by construction.
Its godoc states the thesis for all of this plainly ([reflect.go:41-43](https://github.com/jmoiron/sqlx/blob/master/reflectx/reflect.go#L41-L43)): `GetByTraversal` is "analogous to `reflect.FieldByIndex`, but using the cached traversal rather than re-executing the reflect machinery each time."

### The counterexample: mapstructure and koanf cache nothing

This is directly relevant because `koanf` is the closest ecosystem neighbour to ferry.

`github.com/go-viper/mapstructure/v2` (`mitchellh/mapstructure` is archived) **does not import `sync` at all**, and no map anywhere is keyed by `reflect.Type`.
`decodeStructFromMap` rebuilds everything per call: fresh key-tracking maps, a fresh BFS queue, a fresh `fields` slice, and re-parses tags per field per call with the comment "We always parse the tags cause we're looking for other tags too."
The only thing named "cache" is `cachedDecodeHook`, which memoises the hook function's dispatch per `Decoder`, not per type.

Running mapstructure's **own** benchmark file unmodified (Apple M1 Pro, go1.26.5):

```
Benchmark_Decode-8           2668 ns/op   1880 B/op   51 allocs/op   (mapstructure)
Benchmark_DecodeViaJSON-8    2014 ns/op    768 B/op   26 allocs/op   (json.Marshal + json.Unmarshal)
```

Uncached reflection is **33% slower and allocates 2x** compared to serialising the whole thing to JSON bytes and parsing them back, purely because `encoding/json` has `cachedTypeFields` and mapstructure has nothing.
That is the most persuasive number available for the caching argument, and it is generated by the library's own benchmark.

`knadh/koanf` inherits this verbatim: no `sync.Map`, no `sync.Once`, no type-keyed map anywhere, and `UnmarshalWithConf` constructs a **brand-new `mapstructure.Decoder` on every call** ([koanf.go:264-297](https://github.com/knadh/koanf/blob/master/koanf.go)).
For a library called once at startup that is fine.
xload is pitched for per-HTTP-request query-param parsing, and ferry inherits that use case, so it is not fine here.

### Generics buy nothing for the cache itself

- **There is no generic `sync.Map` in the stdlib as of Go 1.26, and there will not be one in the current `sync` package.**
  [go.dev/issue/47657](https://go.dev/issue/47657) (`PoolOf`, `MapOf`, `ValueOf`) is closed as **not planned** with a `Proposal-Hold` label.
  [go.dev/issue/69027](https://go.dev/issue/69027) (generic `sync.Map`) closed as not planned - changing it in place "would not be backward compatible".
  [go.dev/issue/73015](https://go.dev/issue/73015) (export `internal/sync.HashTrieMap`) closed as duplicate.
  The live path is [go.dev/issue/71076](https://go.dev/issue/71076), **`proposal: sync/v2: new package`, still open with no milestone**.
  Do not design around it landing.
- The generic implementation already exists, unexported.
  Since Go 1.24 ([go.dev/issue/70683](https://go.dev/issue/70683)), `sync.Map` is a thin `any, any` instantiation of `internal/sync.HashTrieMap[K comparable, V any]` (`sync/map.go:38-42`).
- The stdlib's own case for generics over a type-keyed cache is **allocation avoidance, not type safety** - `unique/handle.go:55-64`:

  > The two-level map might seem odd at first since the HashTrieMap could have `any` as its key type, but the issue is escape analysis.
  > We do not want to force lookups to escape the argument... What is worth it though, is saving on those allocations.

  Note `unique` keys on `*abi.Type` (a pointer), not `reflect.Type` (an interface), to avoid the interface hash.
  Since ferry would store a `*schema` pointer as the value, the `any`-boxing allocation the `sync/v2` proposal complains about does not apply.
- None of the five third-party libraries surveyed uses generics for its type cache.

### Honest gap

I could not find a benchmark in **any** of these repos that isolates cached-versus-uncached schema resolution (no `BenchmarkMakeStructFields` or equivalent).
`encoding/json/v2`'s README claims "at performance parity with v1 for marshaling, but dramatically faster for unmarshaling" and points at an external `jsonbench` repo, which was not run.
Any "10-100x from schema caching" figure circulating in blog posts is not backed by a primary source I could locate.
The numbers in section 5.3 of this document are ferry-specific and were generated here for that reason.

## 3. Relevant stdlib since xload's design

Version claims below were verified against `$(go env GOROOT)/api/go1.NN.txt` on the local go1.26.5 toolchain.
Those files are the Go release process's own API gate, so they are the authoritative record of which release an identifier landed in; each line also carries the proposal issue number.
Where a claim is about behaviour rather than existence, the stdlib source is cited.

### The additions that matter, with versions

| Landed | Identifier | Issue | Relevance to ferry |
| --- | --- | --- | --- |
| **1.20** | `errors.Join(...error) error` | [#53435](https://go.dev/issue/53435) | Per-field error aggregation |
| **1.20** | `reflect.Value.SetZero()` | [#52376](https://go.dev/issue/52376) | Clearing a field without building a zero value |
| **1.20** | `reflect.Value.Equal`, `.Comparable()` | [#46746](https://go.dev/issue/46746) | Cheaper than `reflect.DeepEqual` for the 5.7 probe |
| **1.20** | `reflect.Value.Grow(int)` | [#48000](https://go.dev/issue/48000) | Slice building on the load path |
| **1.21** | `sync.OnceFunc`, `OnceValue[T]`, `OnceValues[T1,T2]` | [#56102](https://go.dev/issue/56102) | Schema compilation, exactly once |
| **1.21** | `cmp.Ordered`, `cmp.Compare`, `cmp.Less` | [#59488](https://go.dev/issue/59488) | Deterministic ordering |
| **1.21** | `slices.Sort`, `SortFunc`, `Compare`, `BinarySearch`, ... | [#60091](https://go.dev/issue/60091) | Deterministic dump output |
| **1.22** | `reflect.TypeFor[T]() Type` | [#60088](https://go.dev/issue/60088) | Schema from a type parameter, no value needed |
| **1.22** | `cmp.Or[T comparable](...T) T` | [#60204](https://go.dev/issue/60204) | First-non-zero precedence chains |
| **1.23** | `iter.Seq[T]`, `iter.Seq2[K,V]`, `iter.Pull`, `Pull2` | [#61897](https://go.dev/issue/61897) | Watch API, leaf iteration |
| **1.23** | `slices.Sorted`, `SortedFunc`, `Collect`, `Values`, `All`, `AppendSeq`, `Chunk`, `Backward` | [#61899](https://go.dev/issue/61899) | Sorting an iterator directly into a deterministic slice |
| **1.23** | `maps.All`, `Keys`, `Values`, `Collect`, `Insert` | [#61900](https://go.dev/issue/61900) | `slices.Sorted(maps.Keys(m))` is the determinism one-liner |
| **1.23** | `reflect.Value.Seq()`, `Seq2()`, `Type.CanSeq()`, `CanSeq2()` | [#66056](https://go.dev/issue/66056) | Range over a reflected slice/map |
| **1.23** | `structs.HostLayout` | [#66408](https://go.dev/issue/66408) | **Not relevant** - see below |
| **1.24** | `omitzero` tag option in `encoding/json` v1 | [#45669](https://go.dev/issue/45669) | The semantics ferry's dump direction should copy |
| **1.25** | `reflect.TypeAssert[T](Value) (T, bool)` | [#62121](https://go.dev/issue/62121) | Generic type assertion; measured in section 1e |
| **1.26** | `errors.AsType[E error](error) (E, bool)` | [#51945](https://go.dev/issue/51945) | **Generic `errors.As`** - see below |
| **1.26** | `reflect.Type.Fields() iter.Seq[StructField]` | [#66631](https://go.dev/issue/66631) | The struct walk, natively |
| **1.26** | `reflect.Value.Fields() iter.Seq2[StructField, Value]` | [#66631](https://go.dev/issue/66631) | Field plus value in one range |
| **1.26** | `reflect.Type.Methods/Ins/Outs`, `Value.Methods()` | [#66631](https://go.dev/issue/66631) | Codec discovery, with a caveat below |
| **1.27 (RC)** | `encoding/json/v2`, `encoding/json/jsontext` GA | [#71497](https://go.dev/issue/71497) | Importable at last; see the 1.27 subsection below |
| **1.27 (RC)** | generic methods (language) | - | `d.Into[Config](...)` becomes expressible |
| **1.27 (RC)** | `hash/maphash.Hasher[T]`, `ComparableHasher[T]` | [#70471](https://go.dev/issue/70471) | A named contract for a custom-keyed schema cache |
| **1.27 (RC)** | `net/url.(*URL).Clone`, `url.Values.Clone` | [#73450](https://go.dev/issue/73450) | Deep copy for a `url.URL`-wrapping ferry type |
| **1.27 (RC)** | `strings.CutLast`, `bytes.CutLast` | [#71151](https://go.dev/issue/71151) | Splitting a flat key on its last delimiter |

Note that range-over-func was a `GOEXPERIMENT=rangefunc` preview in Go 1.22 ("Building with `GOEXPERIMENT=rangefunc` enables this feature", [go1.22 release notes](https://go.dev/doc/go1.22)) and became a language feature in **Go 1.23**, at the same time as package `iter`.

**Generic methods in Go 1.27 - confirmed, and verified by compiling them.**
The Go 1.27 release notes state that "a method declaration may declare its own type parameters", with the restriction that "methods of interfaces may not declare type parameters nor can interface methods be implemented by generic methods."
Verified on `go1.27rc2`: `func (d *Dumper) Into[T any](v T) string` compiles and runs, `type J interface{ M[T any](T) }` fails with `interface method must have no type parameters`, and the feature is language-version gated - under a `go 1.26` directive the same file fails with `generic method requires go1.27 or later (-lang was set to go1.26; check go.mod)`.

This is the single biggest lever on ferry's API shape, because it is the difference between

```go
ferry.Into[Config](d, ...)          // today: package-level generic function
d.Into[Config](...)                 // Go 1.27: generic method
```

Go 1.27 is still an RC at time of writing.
The interface restriction is the part that constrains ferry: a generic method cannot satisfy an interface, so a plugin seam expressed as an interface can never take a type parameter.
**Design so the package-level-function form can later grow method forms, and do not put a would-be generic method on any interface.**

### `errors.Join` - what it does and does not give you

Read from `$GOROOT/src/errors/join.go`:

- Nil values are discarded; `Join` returns `nil` if every value is nil.
- The result implements `Unwrap() []error`, and `errors.Is`/`errors.As` traverse it.
- **`Error()` is a bare newline concatenation** of the elements (join.go:44-57), with a special case returning the single element's message verbatim when there is exactly one.

Three further facts that constrain ferry's error design:

- **`joinError` has no `Format` method.** `%+v` and `%v` are identical. The Go 2 error-printing draft (the `Formatter`/`Printer` interfaces in `golang/proposal/design/go2draft-error-printing.md`) was **never adopted**; there is no `errors.Formatter` in Go 1.26. Structured or indented aggregate output means implementing `fmt.Formatter` yourself.
- **`errors.Unwrap` does not unwrap a `Join`.** `$GOROOT/src/errors/wrap.go` says so explicitly: "In particular Unwrap does not unwrap errors returned by [Join]." Only `Is`/`As`/`AsType` traverse the tree, pre-order depth-first.
- **It is invalid for an `Unwrap() []error` to return a slice containing a nil error.** Stated in the package doc. ferry's aggregate must filter.

`errors.Join` is the right *plumbing* - it is what makes `errors.Is`/`errors.As` work across an aggregate - but it is not a presentation layer.
Fifty joined field errors render as fifty raw lines with no grouping, no key, and no ordering guarantee beyond insertion order.
ferry should define its own aggregate type that implements `Unwrap() []error` (so it composes with the stdlib), formats via `fmt.Formatter`, and **sorts by key**.

`fmt.Errorf` gained support for multiple `%w` verbs in the same release ([go1.20 release notes](https://go.dev/doc/go1.20)).
It returns a `*fmt.wrapErrors` whose `Error()` is the formatted message, **not** a newline join - a useful difference if ferry wants one readable sentence that still unwraps to many causes.

**`errors.AsType` (Go 1.26, [#51945](https://go.dev/issue/51945)) is a real generics win and belongs in section 1.**

```go
func AsType[E error](err error) (E, bool)
```

The [go1.26 release notes](https://go.dev/doc/go1.26) call it "a generic version of `As`... type-safe, faster, and, in most cases, easier to use", and the `errors` package doc now uses it in preference to `As` in its own examples.
It handles `Unwrap() []error` identically.
For ferry this replaces the two-line `var e *ferry.FieldError; if errors.As(err, &e)` dance with `fieldErr, ok := errors.AsType[*ferry.FieldError](err)`, and it structurally prevents the value-versus-pointer footgun reproduced in section 5.14, because the type parameter must itself satisfy `error`.
The cost is a `go 1.26` directive.

xload uses none of this: no `errors.Join` on the serial path, no type implementing `Unwrap() []error`, first error wins (section 5.4).

### `iter.Seq` and the error problem - **there is no stdlib convention**

I grepped the entire Go 1.26.5 stdlib for any exported function returning `iter.Seq2[T, error]`.
**There are zero.** Every stdlib `Seq2` is a `(key, value)` or `(index, element)` pair:

```
maps.All      -> iter.Seq2[K, V]
slices.All    -> iter.Seq2[int, E]
slices.Backward -> iter.Seq2[int, E]
reflect.Value.Fields()  -> iter.Seq2[StructField, Value]
reflect.Value.Methods() -> iter.Seq2[Method, Value]
reflect.Value.Seq2()    -> iter.Seq2[Value, Value]
```

Every stdlib `Seq`/`Seq2` iterates in-memory data and cannot fail.

**This is not an accident, it is a deliberate omission.**
The accepted proposal [#61897](https://go.dev/issue/61897) originally contained an `### Errors` section recommending `iter.Seq2[string, error]`, and its `Seq2` doc said "conventionally key-value **or value-error** pairs".
The commit that imported the proposal text into the tree (`fe36ce6`, [CL 591096](https://go-review.googlesource.com/c/go/+/591096), rsc, reviewed by adonovan) says verbatim:

> Add the doc comment from the proposal, lightly edited.
> **The edits are to drop mention of value-error Seq2 usage** and to adjust for the bool result changes.

The open tracking issue is [go.dev/issue/71901](https://go.dev/issue/71901) "iter: document general guidance for writing iterator APIs" - open, `NeedsDecision`, milestone Backlog.
ianlancetaylor, 2025-02-23: "This discussion is why we don't have general guidance about how to return errors. People don't yet agree."

The substantive objections on record, both worth ferry taking seriously:

- **bradfitz on [#70084](https://go.dev/issue/70084)**, reporting a Go team discussion: `iter.Seq2[T, error]` "makes it too easy for callers to ignore errors... imagine a stream of integers in `iter.Seq2[int, error]`... if the caller did `for n := range seq`, they'd get a zero at the end... is that a real zero, or a zero with an error they discarded?"
  This is not hypothetical: [#65236](https://go.dev/issue/65236) (accepted, Go 1.23) made omitting iteration variables legal, and there is **no vet check** for a dropped iterator error in Go 1.26.
- **adonovan on [#76802](https://go.dev/issue/76802)** enumerates four mutually incompatible readings of `Seq2[T, error]`: each element fails independently; partial result plus error; may fail to start; truncated by first error.
  A library that ships this without saying which one it means has shipped an ambiguity.

Concrete evidence that the Go team is holding the line: [#70657](https://go.dev/issue/70657) (`bufio.Scanner.TextSeq`) was **declined** in 2025 with adonovan writing "we should put this proposal aside until we have a consensus on how to deal with iterable sequences with the potential for errors", and [#76802](https://go.dev/issue/76802), filed by ianlancetaylor himself, was **withdrawn**.
The direction they are actually pursuing is the opposite: keep `Err()` and add a vet check - [#17747](https://go.dev/issue/17747) ("cmd/vet: check for missing Err calls") is accepted with milestone **Go 1.28**.

Practical consequences for ferry's watch API:

- `iter.Seq2[Event, error]` is what the ecosystem uses and is a defensible choice, but it is a convention ferry **invents and must document**, not one it inherits.
  If ferry ships it, answer adonovan's four questions in the doc comment explicitly: does a non-nil error terminate the sequence, can a value accompany an error, may the caller continue.
- The stdlib's own pattern for fallible streaming is a **stateful iterator plus `Err()`** (`bufio.Scanner`, `sql.Rows`), which composes worse with range-over-func but is unambiguous.
- A third option, jba on [#70084](https://go.dev/issue/70084): return `(iter.Seq[T], func() error)`.
  He argues it is "strictly better than putting an `Err` method on the iterator, because you can't forget about `errf`: the compiler will warn about an unused variable."
  For a config library where a silently swallowed watch error is a production incident, that compiler assist is worth more than the syntactic elegance of `Seq2`.
- `iter.Pull2` exists for manual driving; `stop` **must** be called on early abandonment or the coroutine leaks and the iterator's defers never run.
  `defer stop()` is the convention.

**There is no official Go recommendation on error propagation through iterators, and the absence is deliberate and current.**
Treat this as an open design choice ferry must make and justify, not a solved one.

### `sync.OnceValue` / `OnceValues`

Go 1.21, [#56102](https://go.dev/issue/56102).
Signatures: `OnceFunc(f func()) func()`, `OnceValue[T any](f func() T) func() T`, `OnceValues[T1, T2 any](f func() (T1, T2)) func() (T1, T2)`.

The panic semantics cut both ways (`$GOROOT/src/sync/oncefunc.go`): if `f` panics, **the same panic value is re-raised on every subsequent call**, and `f` is dropped so it is not kept alive.

That is better than a hand-rolled `sync.Once`, where the first caller panics and every later caller silently receives a zero-valued schema.
But it also means a type whose schema compilation panics is **permanently poisoned with no recovery path**.

So the recommendation is specific: use **`sync.OnceValues[*Schema, error]`** and make schema compilation return an error rather than panic.
A malformed struct then yields a repeatable *error*, not a repeatable *panic*.
That is the difference between a user seeing "field Foo: unknown tag option `requird`" every time and their process crashing every time.

### `reflect` additions material to a struct walker

The single most material one is **Go 1.26's `Type.Fields()` / `Value.Fields()`** ([#66631](https://go.dev/issue/66631)), which is exactly the loop xload hand-writes twice.
Source at `$GOROOT/src/reflect/value.go:2638-2656`:

```go
// Fields returns an iterator over each [StructField] of v along with its [Value].
//
// The sequence is equivalent to calling [Value.Field] successively
// for each index i in the range [0, NumField()).
//
// It panics if v's Kind is not Struct.
func (v Value) Fields() iter.Seq2[StructField, Value] {
```

Be honest about the size of this win: it is a readability change, and it may be a **pessimisation** on a hot path.
It is a thin wrapper over `t.Field(i)` / `v.Field(i)`, it allocates a closure per call, and it does nothing for caching.
aclements said as much on the proposal ([#66631](https://go.dev/issue/66631)): "One (perennial) potential issue is having to allocate the `StructField.Index` slice on each iteration even if the caller isn't using the `StructField` at all", and separately that the `Type` methods "will force an allocation of the returned closure."
`Fields()` also does **not** descend into embedded structs - "the i'th field yielded by `Fields` is the same as `Type.Field(i)` and `Value.Field(i)`" - so promoted fields still need `reflect.VisibleFields` (Go 1.17).

It replaces load.go:85-87 and async.go:77-79 with a `range`, and that is all.
It raises ferry's minimum Go version to 1.26, which recommendation 10's `go 1.27` floor subsumes.
Since ferry compiles the schema **once** per type, the loop it would improve runs once per type anyway, so the readability is free and the allocation concern is irrelevant.
Adopt it in the compiler, not in any per-call path, and benchmark before assuming it is faster than `for i := range t.NumField()`.

One trap in the same proposal: `Type.Methods`'s godoc warns that "calling this method will force the linker to retain all exported methods in all packages."
That is real binary bloat ([#77222](https://go.dev/issue/77222) reported it in the wild).
**Use `Fields`, avoid `Methods`** - ferry's codec discovery should use `Type.Implements`/`reflect.TypeFor` against known interfaces, not enumerate methods.

The others:

- `reflect.TypeFor[T]()` (1.22) - covered in section 1b, this one is genuinely structural.
- `reflect.TypeAssert[T]` (1.25) - benchmarked in section 1e, do not build a performance case on it.
- `Value.SetZero()` (1.20) - the correct way to clear a field; xload has no need today but a Dump direction that honours `omitzero` will.
- `Value.Equal` / `Value.Comparable()` (1.20) - a cheaper, non-recursive alternative to the `reflect.DeepEqual` probe at load.go:113 for comparable types, though it panics on non-comparable ones so it needs the `Comparable()` guard.
- `Value.Seq()` / `Seq2()` and `Type.CanSeq()` / `CanSeq2()` (1.23) - range over a reflected slice or map; useful on the Dump side for walking a `map[string]T` field.

**Nothing has landed that helps with struct tags, and I mean nothing.**
`$GOROOT/api/*.txt` contains exactly three `StructTag` lines in Go's entire history:

```
api/go1.txt    pkg reflect, type StructTag string
api/go1.txt    pkg reflect, method (StructTag) Get(string) string
api/go1.7.txt  pkg reflect, method (StructTag) Lookup(string) (string, bool)
```

`Lookup` ([#14883](https://go.dev/issue/14883), Go 1.7) is the **only** change to `reflect.StructTag` ever accepted.
No option splitting, no comma parsing, no validation.
Any tag grammar ferry defines is ferry's to parse and ferry's to validate.

**And `go vet`'s `structtag` analyzer will not help either - it is weaker than commonly assumed.**
Reading the vendored source at `$GOROOT/src/cmd/vendor/golang.org/x/tools/go/analysis/passes/structtag/structtag.go`:

- It checks **syntax only** (space-separated pairs, key syntax, quoting).
- Suspicious spaces are checked only for a hardcoded key list: `var checkTagSpaces = map[string]bool{"json": true, "xml": true, "asn1": true}`.
- Duplicate *names* are checked only for `json` and `xml`.
- It knows nothing about custom keys, so `ferry:"whatever"` is **never** validated. It does not catch a misspelled option (`json:"x,omitmepty"` passes) and does not catch duplicate keys in one tag (`` `json:"b" json:"b2"` `` passes; `Get` returns the first).
- **It is not run by `go test`.** `go help test` lists the default subset as "atomic, bool, buildtags, directive, errorsas, ifaceassert, nilfunc, printf, stringintconv, and tests" - `structtag` is absent, and the entry is literally commented out in `defaultVetFlags` at `$GOROOT/src/cmd/go/internal/test/test.go` referencing [#18085](https://go.dev/issue/18085).

**Implication: ferry cannot assume any user's CI catches a malformed ferry tag.**
Either ship a validation entry point users call in a test (which `reflect.TypeFor[T]()` makes possible without a value, section 1b), or ship an `analysis.Analyzer` for the `ferry` key, or both.
Worth copying from `encoding/json/v2`'s tag parser: it rejects near-miss mutants (`omitEmpty`, `omit_empty`) with "has invalid appearance of `%s` tag option; specify `%s` instead", and rejects duplicate options outright.

### Struct tag proposals: two open, neither close to landing

Searched `repo:golang/go is:issue "struct tag" in:title label:Proposal` and inspected the live ones.
The two that would matter to ferry:

- **[go.dev/issue/74472](https://go.dev/issue/74472) "proposal: spec: typed struct tags"** - **open**, labels `LanguageChange`, `Proposal-Hold`, `LanguageProposal`, milestone `Proposal`, created 2025-07-04.
  It would expand struct tags to allow a comma-separated list of constant expressions alongside the existing string, i.e. actual typed, compiler-checked tag values.
  The issue body itself opens with "[Status: currently on hold]".
  This is the proposal that would eventually make a tag grammar like ferry's compile-time-checked.
  It is a language change on hold; do not plan around it.
- **[go.dev/issue/60791](https://go.dev/issue/60791) "proposal: encoding,encoding/json: common struct tag for field names"** - **open**, milestone `Proposal`, created 2023-06-14.
  A shared field-name tag so a struct does not need separate `json:`, `toml:`, `yaml:` tags.
  Relevant to ferry's "one tag grammar, many backends" pitch, since it is the stdlib exploring the same idea.
  No sign of movement.

Two more that a mapper author should be watching:

- **[go.dev/issue/60770](https://go.dev/issue/60770) "encoding/json: export tagOptions, parseTag"** - **open**, milestone `Proposal`, filed 2023-06-13, **no proposal-review decision in three years**.
  This is exactly the "let libraries stop reimplementing tag parsing" ask, and its dormancy is the answer.
- **[go.dev/issue/74819](https://go.dev/issue/74819) "encoding/json/jsonstruct: new API for handling JSON fields in a Go struct"** (dsnet, 2025) - **open**, `LibraryProposal`.
  Would expose which struct fields are serialisable, their names, and their `reflect.StructField`.
  The closest thing to a future stdlib version of ferry's schema compiler.

Note on [go.dev/issue/40281](https://go.dev/issue/40281), which is easy to misread: it proposed multi-key tags (`` `json,bson,xml:"field"` ``), was **accepted and implemented for Go 1.16**, then rsc reopened it because it silently redefined existing tags (`` `json xml bson:"other"` `` returns `""` on Go 1.15 and `"other"` on 1.16, with no compile error), and it was **accepted a second time as a rollback**.
Net effect: multi-key struct tags exist in **no released Go version**.
The retry, [go.dev/issue/62035](https://go.dev/issue/62035), is open but dormant.

On the timeline for #74472, the two current signals point in opposite directions and both are real: griesemer applied `Proposal-Hold` in Nov 2025 citing bandwidth, and aclements wrote in Apr 2026 that he had "started meeting with @rsc, @griesemer, @adonovan, and @neild to 'jump start' and prioritize this proposal", partly because it "affects decisions on `json/v2`".
As of 2026-07-31 the hold label is still on and no updated proposal has been filed.

**Conclusion: struct tags remain unvalidated, untyped strings through at least Go 1.28.**
Merovius' framing in #74472 is the best statement of why this is a problem and is worth quoting in ferry's own design docs: `reflect` defines a "conventional mini-language", packages define "micro-languages" for values, options define "nano-languages", field types define "pico-languages" for formats, and "with all these layers of bespoke syntax, each with its own rules for quoting and set of allowed and disallowed characters, it becomes increasingly easy to make mistakes."
He also notes tag keys are **not namespaced** - two different third-party YAML packages both claim `yaml:`.
That is a direct argument for ferry choosing a distinctive tag key and for supporting a configurable one.

### `structs.HostLayout` - not relevant

Go 1.23, [#66408](https://go.dev/issue/66408).
It is a zero-sized marker embedded in a struct to tell the compiler the type's layout must match the host platform's C ABI.
It concerns cgo and syscall interop.
It has **nothing to do with struct tags, field iteration, or mappers**, and ferry should ignore it.
Listed here only because the ticket asked.

### `cmp`, `slices`, `maps` for deterministic output

ferry needs sorted, deterministic dump output, and this is now a one-liner rather than a hand-rolled sort:

```go
for _, k := range slices.Sorted(maps.Keys(m)) { ... }
```

`maps.Keys` (1.23) returns an `iter.Seq[K]`; `slices.Sorted` (1.23) collects and sorts it in one call.
For a non-`Ordered` key type, `slices.SortedFunc` with `cmp.Compare` on a projection.

Two traps worth recording.

**The `SortFunc` comparator changed shape** between `golang.org/x/exp/slices` and the stdlib version in Go 1.21.
Proposal [#60091](https://go.dev/issue/60091) originally proposed the `x/exp` signature `func(a, b E) bool` verbatim; rsc changed it during review ("let's switch to cmp consistently throughout... The argument in favor of cmp is consistency within package slices. Now if you are sorting a slice and then doing a binary search over it, you only need to write one comparison function and use it in both calls").
The shipped signature is `func SortFunc[S ~[]E, E any](x S, cmp func(a, b E) int)`.
A copied `bool` comparator fails to compile, which is loud and fine.
The **silent** hazard is a hand-written comparator that returns `0`/`1` instead of `-1`/`0`/`+1`: that compiles and sorts wrongly.
The godoc also requires a strict weak ordering and says "the function should return 0 for incomparable items."

**`maps.Keys` changed meaning.** It was removed from Go 1.21 before it shipped ([#61538](https://go.dev/issue/61538), a release blocker) specifically so Go 1.23 could give it an iterator signature.
`golang.org/x/exp/maps.Keys` returns `[]K`; stdlib `maps.Keys` returns `iter.Seq[K]`.
Any code carried over from `x/exp` idioms is silently a different API.

`cmp.Or` (1.22) is a small but genuinely useful one for a config mapper: `cmp.Or(fromFlag, fromEnv, fromFile, defaultValue)` returns the first non-zero argument, which is the precedence chain shape xload's `SerialLoader` hand-rolls badly (section 5.12).
Note it keys on the **zero value**, so it inherits the same "empty means absent" conflation ferry is trying to escape; use it for the typed layer, not the plane boundary.
For multi-key deterministic ordering, `cmp.Or(cmp.Compare(a.X, b.X), cmp.Compare(a.Y, b.Y))` is the idiomatic tiebreak chain.

### `encoding/json/v2` status

**GA in Go 1.27 (RC verified), importable with no build flags, and under the Go 1 compatibility promise.**

The trajectory, verified per release:

| Release | Status |
| --- | --- |
| **1.25** | Experimental, **opt-in** via `GOEXPERIMENT=jsonv2`. [go1.25 release notes](https://go.dev/doc/go1.25#json_v2), authorising proposal [#71845](https://go.dev/issue/71845). |
| **1.26** | No change, and no mention in the release notes. Confirmed locally: still gated, `grep -c "encoding/json/v2" $GOROOT/api/go1.26.txt` returns 0. |
| **1.27 (RC)** | **Generally available, opt-OUT.** Verified on `go1.27rc2`: `internal/buildcfg/exp.go:87` sets `JSONv2: true` in the baseline experiment set, `api/go1.27.txt` carries 45 `encoding/json/v2` lines and 113 `encoding/json/jsontext` lines, and a `go 1.27` module importing both builds and runs with `GOEXPERIMENT` unset. `GOEXPERIMENT=nojsonv2` restores v1 **and removes both new packages from the build**. The [go1.27 release notes](https://go.dev/doc/go1.27) say the opt-out "is expected to be removed in a future release." |

Proposal [go.dev/issue/71497](https://go.dev/issue/71497) is **closed as completed**, labelled `Proposal-Accepted` and `release-blocker`, milestone **Go 1.27** (confirmed via `gh issue view`).

That last row matters for ferry's positioning: every Go program's `encoding/json` behaviour changes at 1.27, and the semantics ferry chooses for `omitempty`/`omitzero`, duplicate keys, and case sensitivity should be chosen against **v2**, not v1.

**`encoding/json` v1 is now an options-configured skin over v2.**
The 21 new `encoding/json` API lines in `api/go1.27.txt` are almost entirely legacy-semantics switches - `OmitEmptyWithLegacySemantics`, `CallMethodsWithLegacySemantics`, `MergeWithLegacySemantics`, `StringifyWithLegacySemantics`, `ReportErrorsWithLegacySemantics`, `FormatDurationAsNano`, `FormatByteArrayAsArray`, `UnmarshalArrayFromAnyLength`, `MatchCaseSensitiveDelimiter`, `ParseTimeWithLooseRFC3339`, `ParseBytesWithLooseRFC4648`, plus `DefaultOptionsV1`.
Two of the new lines are structural and worth knowing:

- `type RawMessage = jsontext.Value` - v1's `RawMessage` is now an **alias** for the v2 syntax-layer type ([v2_stream.go:190](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/v2_stream.go)).
- `type Marshaler = json.Marshaler` and `type Unmarshaler = json.Unmarshaler` - v1's marshaler interfaces are now **aliases** for v2's.
  So `encoding/json.Marshaler` and `encoding/json/v2.Marshaler` are the same type in 1.27, and a codec chain that probes for one finds the other.
  Note this only holds when jsonv2 is enabled; under `nojsonv2` v1 declares its own identical interface.
  Either way the method set is identical, so a `Type.Implements` probe behaves the same, which means ferry never needs to probe both.

**Only `MarshalerTo` / `UnmarshalerFrom` are genuinely new interfaces**, and they are analysed in "What first-class `encoding/json/v2` could concretely mean" below.

**The v2 tag grammar changed between 1.26 and 1.27, and this document's earlier "still moving" caveat resolved as follows** (diffed `doc.go` between the two toolchains):

| 1.26 | 1.27 (RC) | Why |
| --- | --- | --- |
| `inline` | **renamed to `embed`** | [#79985](https://go.dev/issue/79985), commit [`6a1dd03`](https://github.com/golang/go/commit/6a1dd0342331). `json:",inline"` was already a widely-copied **no-op** in the Kubernetes ecosystem (~29k hits), so shipping it with real meaning would have silently changed those programs. |
| `unknown` option, `DiscardUnknownMembers` | **removed** | [#77271](https://go.dev/issue/77271), commit [`c9cbeb0`](https://github.com/golang/go/commit/c9cbeb0a1b08). "Adding new features is always backwards compatible, but removing or changing them is not." `RejectUnknownMembers` survives for v1 `DisallowUnknownFields` compatibility. |
| `format:` option | **removed from the supported set** | [#79071](https://go.dev/issue/79071), commit [`0b54a75`](https://github.com/golang/go/commit/0b54a7531935). "With Go 1.28 prospectively having typed struct tags in some form or another, the json/v2 working group decided to remove support for the `format` tag option since this would be more naturally expressed as a typed struct tag." |
| `string` applied recursively | **top-level only** | [#79065](https://go.dev/issue/79065). "The `string` option only applies to the top-level of the Go struct field value." |
| names via single-quoted string literal | **removed** | Gone from `doc.go` in 1.27. |

`format:` is still *parsed*, and still rejected.
Verified on `go1.27rc2`: `json:"t,format:'2006-01-02'"` on a `time.Time` field yields

```
json: cannot marshal from Go main.S: Go struct field T has unsupported `format` tag option
```

The implementation survives behind an internal `jsonflags.FormatTagSupported` flag that only the `github.com/go-json-experiment/json` mirror can set ([jsonopts/options.go:20-23](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/internal/jsonopts/options.go)).

**The shipping 1.27 v2 tag option set is therefore exactly five: `omitzero`, `omitempty`, `string`, `case:ignore|strict`, `embed`** ([doc.go:68-119](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/v2/doc.go)).

Two lessons ferry should take from that churn, both cheap:

1. **A tag option name that is already in circulation as a no-op is a compatibility hazard**, which is a concrete instance of the "tag keys are not namespaced" argument recorded later in this section.
   ferry gets this for free by choosing a distinctive tag key, but the *option* names inside the tag have the same exposure if ferry ever supports a compatibility mode over an existing key.
2. **The Go team removed a feature (`format:`) rather than ship it ahead of typed struct tags.**
   ferry has the same choice for any per-field format directive, and the same reason to keep the surface small until [#74472](https://go.dev/issue/74472) resolves.

Verified for Go 1.26 (retained because it is the state anyone on 1.26 still sees):

- `$GOROOT/src/encoding/json/v2/` exists in go1.26.5, alongside `encoding/json/jsontext` and `encoding/json/internal/{jsonflags,jsonopts,jsonwire,jsontest}`.
- Every file in `v2/` carries `//go:build goexperiment.jsonv2`.
  The flag is declared at `$GOROOT/src/internal/goexperiment/flags.go:106-107`.
- `go env GOEXPERIMENT` is empty by default, and the gate was confirmed empirically: importing `encoding/json/v2` fails with "build constraints exclude all Go files" unless `GOEXPERIMENT=jsonv2` is set.
- **The gate is transitive and cannot be satisfied by a library on its consumers' behalf.**
  A module importing a library that imports `encoding/json/v2` fails to build unless the *consuming* build sets `GOEXPERIMENT=jsonv2`.
  `go.mod` has no way to declare it: a `goexperiment jsonv2` line is rejected with `unknown directive`, and there is no `goexperiment` counterpart to the `godebug` directive.
  This is why the "`go 1.26` plus GOEXPERIMENT" route is not open to ferry (recommendation 10).
- `grep -c "encoding/json/v2" $GOROOT/api/go1.26.txt` returns **0**.
  It is not under the Go 1 compatibility promise in 1.26.
- It first shipped as an experiment in Go 1.25 ([go.dev/blog/jsonv2-exp](https://go.dev/blog/jsonv2-exp)).
- When the experiment is on, v1 is reimplemented on top of v2 (`$GOROOT/src/encoding/json/v2_decode.go:99` delegates to `jsonv2.Unmarshal` with `DefaultOptionsV1()`).
- `github.com/go-json-experiment/json` is now just an upstream mirror; its README directs changes to the Go project.

**Corrected: it is no longer true that "nothing in it is reusable by a third-party mapper."**
In 1.27 the packages import normally; what stays closed is the field resolver and the ability to define an `Options` value.
See the rewritten section 2 subsection for the full audit.
The design patterns below still stand on their own, and are now *also* available as imports:

- `omitzero` semantics ([doc.go:70-74](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/v2/doc.go)): omit if the value is zero "as determined by the `IsZero() bool` method if present, otherwise based on whether the field is the zero Go value."
  Contrast `omitempty`, which omits if the field would encode as null, empty string, empty object or empty array.
  Explicitly ([doc.go:121-128](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/v2/doc.go)): "only a nil slice or map is omitted under `omitzero`, while an empty slice or map is omitted under `omitempty` regardless of nilness."
  1.27 also adds a caller-side `OmitZeroStructFields(bool)` option, "semantically equivalent to specifying the `omitzero` tag option on every field in a Go struct" ([options.go:174-182](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/v2/options.go)) - a precedent for ferry offering the same choice per-call rather than only per-field.
  `omitzero` landed in **`encoding/json` v1 in Go 1.24** ([#45669](https://go.dev/issue/45669)).
  Crucially, v2 redefines `omitempty` in **JSON** terms while `omitzero` stays defined in **Go** terms, and the v1 migration doc says: "Existing usages of `omitempty` on a Go bool, number, pointer, or interface value should migrate to specifying `omitzero` instead (which is identically supported in both v1 and v2)."
  **If ferry has an omit-on-empty option, model it on `omitzero`, not `omitempty`** - it is the one with stable, backend-independent, Go-level semantics, which is exactly what a backend-agnostic mapper needs.
- `Marshalers` / `Unmarshalers` (`json.MarshalToFunc[T]`, `UnmarshalFromFunc[T]`, `JoinMarshalers`, `WithMarshalers`) is the template for ferry's typed codec registration (section 1c).
  Two details worth copying: it caches negative lookups, and `SkipFunc` lets a registered function decline and fall through to the next one and then to the default.
  The full precedence chain is `WithMarshalers` funcs -> `MarshalerTo` -> `Marshaler` -> `encoding.TextAppender` -> `encoding.TextMarshaler` -> reflection, and it is **documented**, unlike xload's (section 5.9).
  Re-verified on `go1.27rc2` in source and by execution; see "What first-class `encoding/json/v2` could concretely mean" below for the mechanism and the both-interfaces case.
  Known gap: [#73457](https://go.dev/issue/73457) (register by runtime `reflect.Type` rather than static type parameter) is **still open** at 1.27 (confirmed via `gh issue view`, milestone `Proposal`), so json/v2 cannot do dynamic registration. ferry probably needs both.
- `omitzero` is evaluated *before* the marshaler runs; `omitempty` may require marshalling then unwriting.
  If ferry supports both, copy that asymmetry.
- The v2 options model is worth studying for ferry's own option plumbing: one `Options` type shared across both packages and both directions, later options override earlier, variadic `...Options` on every entry point, and - the thing v1 structurally could not do - options are **threaded down the call stack and readable inside user methods** via `Encoder.Options()` / `Decoder.Options()`. xload's `options` struct is package-private and invisible to a user's `Decode` method ([options.go:37-42](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/options.go#L37-L42)); ferry should decide deliberately whether a user codec can see the load options.

Design decisions from the [json/v2 discussion document](https://github.com/golang/go/discussions/63397) that transfer directly:

- The reason v2 exists at all is that `MarshalJSON() ([]byte, error)` **forces an allocation and a re-parse**, and `UnmarshalJSON([]byte)` forces a pre-scan then a second parse.
  These are **API-bound, not implementation-bound** - they cannot be fixed without a new interface.
  xload's `Decoder interface { Decode(string) error }` has the identical defect: it forces the plane value to be materialised as a `string` before the decoder sees it.
  That is the single strongest argument for section 4.
- The syntactic/semantic split (`jsontext` vs `json`) exists so the syntax layer has **no `reflect` dependency**.
  ferry has the same latent split: a plane/codec layer that need not import `reflect`, and a struct-mapping layer that must.
- Stated compatibility target: "95% to 99% backwards compatibility. We do not aim for 100%." Explicit non-goal: `unsafe`.

### Other things worth knowing, and things that are not

- **The `go` directive is now a strict minimum (Go 1.21).** `go 1.26` in `go.mod` means the module cannot be built by Go 1.25, and the go command will download a newer toolchain automatically. GODEBUG-controlled behaviour changes key off it too. **This is a real API decision for ferry, not a formality**: `go 1.23` gets `iter` and `Value.Seq`; `go 1.26` additionally gets `errors.AsType` and `Value.Fields`; `go 1.27` additionally gets generic methods and an unflagged `encoding/json/v2`. Pick deliberately and record it as an ADR (recommendation 10).
- **`testing/synctest`** - experimental in 1.24 (`synctest.Run`), stable in 1.25 with a **renamed API** (`synctest.Test`). If ferry's watch API is concurrent or time-dependent, this is the right test harness. Also the reason `encoding/json` moved off `sync.WaitGroup` (section 2).
- **Generic type aliases (1.24)** - `type Foo[T any] = Bar[T]` now works, previewed in 1.23 under `GOEXPERIMENT=aliastypeparams`. Useful for exposing `type Result[T any] = internal.Result[T]` shims; marginal otherwise.
- **`new(expr)` (1.26, missed by the first pass of this document).** The built-in `new` now accepts an expression, not only a type: `new(x)` allocates a variable of `x`'s type initialised to `x`'s value ([spec, Allocation](https://go.dev/ref/spec#Allocation)). Verified compiling on both go1.26.5 and go1.27rc2 under a `go 1.26` directive. This is not a 1.27 feature, despite the 1.27 stdlib using it (`url.URL.Clone` is `uc := new(*u)`). It is worth recording here because the [go1.26 release notes](https://go.dev/doc/go1.26) motivate it with **exactly ferry's problem**: "This feature is particularly useful when working with serialization packages such as `encoding/json` or protocol buffers that use a pointer to represent an optional value, as it enables an optional field to be populated in a simple expression." That is xload's `cached` provider signature `Get(key string) (*string, error)` (5.1), and any ferry API that returns `*T` for a three-state value. It does not change the recommendation to prefer comma-ok over `*T` (5.1); it just makes the `*T` form cheaper to write where it does appear.
- **Type inference generalisation (1.21, extended in 1.27, confirmed)** - what makes `slices.SortFunc(x, myGenericCmp)` work without explicit instantiation. The 1.27 change, quoted from the release notes: "Function type inference has been generalized to apply in all contexts where a generic function is assigned to a variable of (or converted to) a matching function type." Verified on `go1.27rc2`: `var f func(int) int = generic` compiles with no explicit instantiation, and is language-version gated like the other 1.27 language changes. Directly relevant to ferry's typed codec registration, since it is what lets a user pass a generic helper into `WithDecoder` without writing `[time.Duration]`.
- **`unique` (1.23)** - interning. Potentially relevant if ferry interns key strings across many loads, but the win is small next to schema caching. Its internal design is cited in section 2 for the "generics for allocation avoidance, not type safety" argument.
- **`maphash.Comparable` / `WriteComparable` (1.24), plus `maphash.Hasher[T]` / `ComparableHasher[T]` (1.27)** - see "Go 1.27, verified against `go1.27rc2`" below. The 1.27 additions do **not** change the assessment: still a legitimate way to shard a schema cache, still premature.
- **`sync.Map` reimplemented on `HashTrieMap` (1.24, [#70683](https://go.dev/issue/70683))** - "modifications of disjoint sets of keys... much less likely to contend". Background to section 2's read-cost comparison.
- **`go vet` gains `stdversion` in `go test` by default - confirmed landed in 1.27, with one RC caveat.** See below.

Explicitly **not** relevant despite being recent: `structs.HostLayout`, `weak` and `runtime.AddCleanup` (a schema cache keyed by `reflect.Type` should not be weak, since those values are runtime-immortal anyway), `os.Root`, the `crypto/*` churn, `go/types` iterators.

### Go 1.27, verified against `go1.27rc2`

**Status: release candidate, not GA.**
`go.dev/dl?mode=json&include=all` lists `go1.27rc1` and `go1.27rc2` with `"stable": false`; the newest stable is `go1.26.5`.
Everything below was checked against a locally installed `go1.27rc2 darwin/arm64` (`golang.org/dl/go1.27rc2`), its `api/go1.27.txt`, and its `src/` tree, unless marked otherwise.
An RC can still change before GA.
Re-check `api/go1.27.txt` on the final release before treating any of it as fixed.

**The relevant-package sweep, with "no change" stated explicitly.**
Counts are `grep -c "^pkg <name>," api/go1.27.txt`, which is the Go release process's own API gate:

| Package | New API in 1.27 | Bearing on ferry |
| --- | --- | --- |
| `reflect` | **0. No change.** | The struct walk is unchanged from 1.26. `Type.Fields()` / `Value.Fields()` / `TypeAssert` remain the newest tools. |
| `errors` | **0. No change.** | `errors.AsType` (1.26) is still the newest. No `errors.Formatter`; the aggregate-formatting gap in this section is unchanged. |
| `iter` | **0. No change.** | And [#71901](https://go.dev/issue/71901) is still open, so there is still **no** stdlib convention for errors in iterators. Recommendation 11 stands unchanged. |
| `sync` | **0. No change.** | No generic `sync.Map`; `sync/v2` ([#71076](https://go.dev/issue/71076)) still has no milestone. Recommendation 13 stands. |
| `slices` | **0. No change.** | `slices.Sorted(maps.Keys(m))` remains the determinism idiom. |
| `maps` | **0. No change.** | As above. |
| `cmp` | **0. No change.** | `cmp.Or` unchanged. |
| `strings` | 1: `CutLast(string, string) (string, string, bool)` | [#71151](https://go.dev/issue/71151). Marginal but real for flat keys: `prefix, leaf, ok := strings.CutLast(key, ".")` replaces a `LastIndex` plus two slices when splitting a delimiter-joined key from the right. |
| `bytes` | 1: `CutLast` | Same, for `[]byte` planes. |
| `net/url` | 2: `(*URL).Clone() *URL`, `(Values).Clone() Values` | [#73450](https://go.dev/issue/73450). See below. |
| `hash/maphash` | 6: `Hasher[T]`, `ComparableHasher[T]` and methods | [#70471](https://go.dev/issue/70471). See below. |
| `encoding/json` | 21 | Mostly legacy-semantics options; two type aliases. Covered above. |
| `encoding/json/v2` | 45 | GA. Covered above. |
| `encoding/json/jsontext` | 113 | GA. Covered in section 2. |

Nothing else in the 1.27 minor-library list touches a struct-tag-driven mapper.
For the record, the rest is `compress/flate` (encoder rewrite, output bytes may differ), `crypto/*` (ML-DSA and TLS), `database/sql` (`ConvertAssign`, `RowsColumnScanner`), `go/constant`, `go/scanner`, `go/token`, `go/types`, `math/big`, `math/rand/v2`, `net`, `net/http`, `net/http/httptest`, `runtime/secret`, `syscall`, `testing/synctest` (new `Sleep` helper), and `unicode` (15 to 17).
New packages `crypto/mldsa`, `uuid`, and the `GOEXPERIMENT=simd` `simd`/`simd/archsimd` packages are irrelevant here.

**`hash/maphash.Hasher` and `ComparableHasher` - the assessment does not change.**
The shapes ([hasher.go:122-140](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/hash/maphash/hasher.go)):

```go
type Hasher[T any] interface {
    Hash(*Hash, T)
    Equal(T, T) bool
}
type ComparableHasher[T comparable] struct{ ... }
```

The godoc frames this as a contract, not a container: "A `Hasher` defines the interface between a hash-based container and its elements... enabling those values to be inserted in hash tables and similar data structures", and notes that "Hashers may be useful even for comparable types, to define an equivalence relation that differs from the usual one (`==`)", with a case-insensitive string hasher as its worked example.
`go/types` immediately adopted it: `go/types.Hasher` is "an implementation of `maphash.Hasher` for `Types` that respects the `Identical` equivalence relation", with `HasherIgnoreTags` for `IdenticalIgnoreTags`.

For ferry this is **the same conclusion as before, with better vocabulary**.
`reflect.Type` is comparable, so a `sync.Map` keyed by it already works and needs no hashing help.
`Hasher` only earns its keep if ferry ever needs a schema cache keyed by something that is *not* `==`-comparable or where `==` is the wrong relation - for example keying on (type, tag-name) with case folding.
There is still no hash-based container in the stdlib that consumes a `Hasher`, so adopting it today means writing the container too.
**Still premature. Do not put it in the first cut.**

**`net/url.Clone` - directly relevant to ferry's own type package.**
xload's `xloadtype` wraps `url.URL` ([type/url.go](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/type/url.go)) and grew an accidental encoder via `URL.String()` (5.9).
`url.URL` contains a `*Userinfo` pointer, so a plain struct copy shares it; `Clone` is three lines and copies it out ([url.go:1352-1363](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/net/url/url.go)):

```go
func (u *URL) Clone() *URL {
    if u == nil { return nil }
    uc := new(*u)
    if u.User != nil { uc.User = new(*u.User) }
    return uc
}
```

`Values.Clone()` is the deep copy for `map[string][]string`.
Two consequences for a ferry type package that owns a URL-like type:

- If ferry ever hands a `*url.URL` to a sink, or caches one in a schema, `Clone` is now the correct copy and a struct assignment is not.
- `url.Values` is the query-param plane's native representation (section 4's premise check).
  A snapshot source (5.13) that captures `url.Values` should `Clone` it, or the caller can mutate the snapshot underneath the walk.

Costing: both are one-line calls, and both push ferry's floor to `go 1.27` if used.
Neither is load-bearing; hand-rolled equivalents are four lines.

**`go vet`'s `stdversion` under `go test` - confirmed landed, with a caveat that matters right now.**
`defaultVetFlags` in [`cmd/go/internal/test/test.go:654-690`](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/cmd/go/internal/test/test.go) contains `"-stdversion"` in 1.27 and does not in 1.26 (diffed directly between the two toolchains).
The release notes describe it as reporting "the use of standard library symbols that are too new for the Go version in force in the referring file, as determined by `go` directive in `go.mod` and build tags on the file."

**But it did not fire in an RC test.**
A `go 1.26` module calling `strings.CutLast` (a 1.27 symbol) built, vetted, and tested clean under `go1.27rc2`.
The reason is mechanical: `stdversion` resolves symbol versions from the vendored `x/tools` stdlib manifest, and in rc2 that manifest tops out at Go 1.26 - `grep -c '"CutLast"'` and `grep -c 'encoding/json/v2'` on [`cmd/vendor/golang.org/x/tools/internal/stdlib/manifest.go`](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/cmd/vendor/golang.org/x/tools/internal/stdlib/manifest.go) both return 0.
So the *analyzer* is on by default, and its *data* has not been regenerated for 1.27 yet.

Practical reading for ferry: `stdversion` is a genuine safety net for the "used a symbol newer than my `go` directive" mistake, it is on by default from 1.27, and it will presumably have current data by GA - but **do not rely on it to catch 1.27-era mistakes on 1.27 itself**, and re-test this at GA.
Note also that `-structtag` is **still commented out** in the same `defaultVetFlags` block (it was renamed from `-structtags` but not enabled), so this section's conclusion that no user's CI validates a ferry tag is unchanged.
Recommendation 12 stands.

**The other 1.27 language changes.**
Generic methods and generalized type inference are covered above.
The third is minor: "A key in a struct literal may now be any valid field selector for the struct type, not just a (top-level) field name of the struct."
That is a convenience for users writing ferry config literals with embedded structs; it changes nothing about ferry's API.

There are three, not four.
`new(expr)`, which 1.27's own `url.URL.Clone` uses (`uc := new(*u)`), is a **Go 1.26** language change, not a 1.27 one - verified by compiling it on go1.26.5 and by finding it in the [go1.26 release notes](https://go.dev/doc/go1.26).
It is recorded in "Other things worth knowing" above.

### What first-class `encoding/json/v2` could concretely mean

The project has decided ferry will support `encoding/json/v2` first class.
This subsection lays out what that could mean and what each reading costs.
**It does not pick one; that is an ADR's job.**
It also does not re-argue the decision, and it does not re-cost the two routes already closed: an unconditional `encoding/json/v2` import under a `go 1.26` directive (rejected because the GOEXPERIMENT requirement is transitive and every consumer would have to set it), and a `//go:build goexperiment.jsonv2` dual path inside ferry (rejected because ferry's round-trip guarantee would then have to hold identically on two code paths, doubling the property-test matrix).

#### The interfaces, exactly

From [`arshal_methods.go:39-122`](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/v2/arshal_methods.go) on `go1.27rc2`:

```go
type Marshaler interface {
    MarshalJSON() ([]byte, error)
}
type MarshalerTo interface {
    MarshalJSONTo(*jsontext.Encoder) error
}
type Unmarshaler interface {
    UnmarshalJSON([]byte) error
}
type UnmarshalerFrom interface {
    UnmarshalJSONFrom(*jsontext.Decoder) error
}
```

Three facts a codec chain has to handle:

1. **v2's `Marshaler`/`Unmarshaler` are v1's.**
   In 1.27, `encoding/json` declares `type Marshaler = json.Marshaler` and `type Unmarshaler = json.Unmarshaler` as aliases to the v2 types (`api/go1.27.txt`).
   Even under `nojsonv2`, where v1 declares its own, the method sets are identical, so a `Type.Implements(reflect.TypeFor[json.Marshaler]())` probe gives the same answer either way.
   **ferry does not need to probe both.**
2. **A type may implement both, and `MarshalerTo` wins.**
   The godoc says so ("If a type implements both `Marshaler` and `MarshalerTo`, then `MarshalerTo` takes precedence"), and so does the mechanism: `makeMethodArshaler` wraps `fncs.marshal` in source order `TextMarshaler` (line 132), `TextAppender` (158), `Marshaler` (181), `MarshalerTo` (212), each closure capturing the previous one, so the **last** wrapped is the outermost and therefore the effective handler.
   Measured on `go1.27rc2` with a type implementing both: `json.Marshal` returned `"v2"`, not `"v1"`.
   Same for `UnmarshalerFrom` over `Unmarshaler`.
   Registering a `MarshalToFunc` for the same type beat both and returned `"func"`.
3. **The full documented order is `WithMarshalers` funcs -> `MarshalerTo` -> `Marshaler` -> `encoding.TextAppender` -> `encoding.TextMarshaler` -> reflection.**
   Note this inverts xload's, which puts its own `Decoder` first and then `encoding.TextUnmarshaler` before `json.Unmarshaler` (5.9).
   The godoc also states the obligation ferry inherits if it honours both arms: when a type implements both, "both implementations should aim to have equivalent behavior for the default marshal options."
   That is an **unenforceable prose rule**, exactly the kind section 4 recommendation 5 says to back with a conformance suite.

**What ferry has to decide, and the cost of each answer:**

| Option | Cost |
| --- | --- |
| Recognise **only** v1-shaped `MarshalJSON`/`UnmarshalJSON` | Cheapest, works on any Go version, and inherits the exact defect that caused v2 to exist: the value must be materialised as `[]byte` and re-parsed. Section 4 already identifies this as xload's `Decode(string) error` defect in another costume. |
| Recognise **both**, `MarshalerTo` first | Matches v2 and is what a user who has already migrated will expect. Costs a hard `encoding/json/jsontext` import (so `go 1.27`, and breakage under `nojsonv2`), plus ferry must document what happens when the two implementations disagree - the stdlib merely asks that they not. |
| Recognise **neither**, define ferry's own pair over `ferry.Value` | The only option that is honest about ferry being a non-JSON mapper: a `MarshalJSONTo(*jsontext.Encoder)` method cannot write to a Consul KV or an env var. Costs users a third interface to implement, and forfeits the "my type already works with json/v2" adoption story. |
| Define ferry's own pair, and **additionally** adapt v2's as a fallback | Most user-friendly, largest surface, and the adapter has to answer the question the others dodge: what does a JSON-only encoder mean on a plane that has no JSON. Probably `jsontext` bytes into a ferry `Raw` arm (section 4f), which reintroduces parse-twice. |

#### Which v2 *semantics* are portable to a non-JSON plane

This is the part that matters most, because ferry's planes are not JSON.
A semantic defined in **Go** terms transfers; one defined in **JSON** terms does not.
Quotes are from [`v2_options.go:22-128`](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/v2_options.go), the stdlib's own v1-versus-v2 difference list.

| v2 semantic | Defined in | Portable to a non-JSON plane? |
| --- | --- | --- |
| **`omitzero`** - omit if zero "as determined by the `IsZero() bool` method if present, otherwise based on whether the field is the zero Go value" | **Go terms** | **Yes, fully.** This is the one to adopt, and this document already recommended it before v2 went GA. It needs no notion of what the plane's "empty" is. |
| **`omitempty`** - v2 omits if the field "encodes as an 'empty' JSON value, which is defined as a JSON null, or an empty JSON string, object, or array" | **JSON terms** | **No.** v2 deliberately moved it from Go terms (v1) to JSON terms. On a Consul plane there is no "empty JSON object". Adopting v2's `omitempty` would force ferry to define per-plane emptiness, which is the opposite of one tag grammar over many planes. |
| **Case sensitivity** - v2 matches "using an exact, case-sensitive match" where v1 was case-insensitive; `case:ignore` / `case:strict` per field, `MatchCaseInsensitiveNames` per call | **Go/name terms** | **Yes.** Nothing JSON-specific about name matching. Note v2's `case:ignore` also ignores dashes and underscores, and on multiple matches "the field with an exact name match is selected, otherwise an error is reported due to an ambiguous set of candidate fields" - erroring on ambiguity rather than picking one is exactly what recommendation 8b asks ferry to do, and it is the opposite of viper's silent case folding (section 4f). |
| **Duplicate name rejection** - "In v1, a JSON object with duplicate names is permitted. In contrast, in v2 a JSON object with duplicate names results in an error." | **Plane terms, but generically** | **Yes, in spirit.** Measured on `go1.27rc2`: unmarshaling `{"a":1,"a":2}` returns `jsontext: duplicate object member name "a"`. The generic form is "a plane that presents the same key twice is an error, not a last-wins race", which is precisely xload's collision problem (5.5, 5.6) and koanf's `Flatten` nondeterminism (section 4f). Adopt the **rule**; the enforcement point is ferry's, not `jsontext`'s. |
| **Nil slice/map** - "In v1, a nil Go slice or Go map is marshaled as a JSON null. In contrast, v2 marshals a nil Go slice or Go map as an empty JSON array or JSON object" | **JSON terms** | **No, and see the round-trip warning below.** |
| **Invalid UTF-8 rejection** | JSON string terms | Partly. The generic rule "reject values the plane cannot represent" transfers; the specific check does not. |
| **Merge semantics on unmarshal** - v2 "merges when unmarshaling a JSON object, otherwise it replaces" | JSON terms | **No.** ferry's equivalent question ("does Load into a pre-populated struct merge or replace?") is real and open, but v2's answer is phrased entirely in JSON kinds. |
| **`Deterministic`** | Go terms | Yes, and see the warning below. |
| **`time.Duration` has no default representation** | Go terms | **Yes, and it is a decision ferry must make explicitly.** See below. |

#### Round-trip fidelity: v2 versus v1, measured

ferry's round-trip guarantee is a hard constraint, so this is worth measuring rather than assuming.
All measured on `go1.27rc2 darwin/arm64`.

**v2 round-trips nil-versus-empty *worse* than v1 by default.**

```
input: {S: nil, E: []string{}, M: nil}
v1  out -> {"s":null,"e":[],"m":null}     round trip: S nil? true   E nil? false  M nil? true
v2  out -> {"s":[],"e":[],"m":{}}         round trip: S nil? false  E nil? false  M nil? false
v2 + FormatNilSliceAsNull(true) + FormatNilMapAsNull(true)
    out -> {"s":null,"e":[],"m":null}     round trip: S nil? true   E nil? false  M nil? true
```

v1 preserved the nil-versus-empty distinction; v2's defaults destroy it.
It is recoverable with two options, but a ferry that adopts "v2 defaults" wholesale silently loses a distinction its own three-state-presence rule (recommendation 4b) exists to preserve.
**If ferry adopts v2 semantics, this is the one to override, and the ADR should say so in a sentence.**

**v2 map output is nondeterministic by default; v1's was deterministic.**

```
map[string]int with 8 keys, 50 marshals each
v2 default                 -> 8 distinct orderings
v2 Deterministic(true)     -> 1
v1                         -> 1
```

The `Deterministic` godoc ([options.go:134-141](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/v2/options.go)) is careful about what it promises: "Different processes of the same program will serialize equal values to the same bytes, but different versions of the same program are not guaranteed to produce the exact same sequence of bytes."
This directly contradicts recommendation 6, which makes determinism a package-wide invariant for ferry because dumped config must diff cleanly.
**Another v2 default ferry must override, not inherit.**
It is also a useful precedent for how narrowly to word ferry's own determinism promise.

**Precision is unchanged, and section 4's convergent finding survives v2 GA intact.**

```
v2 marshal of uint64(18446744073709551615)
  plain field  -> 18446744073709551615     (wire form is exact)
  `,string`    -> "18446744073709551615"
unmarshal the same bytes into `any`
  plain field  -> 1.8446744073709552e+19   (float64, lossy)
  `,string`    -> "18446744073709551615"   (string, exact)
```

The stdlib says this itself ([doc.go:215-229](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/v2/doc.go)): "v1 and v2 may still lose precision when unmarshaling into an `any` interface value, where unmarshal uses a `float64` by default to represent a JSON number.
To change the default, specify the `WithUnmarshalers` option with a custom unmarshaler that pre-populates the interface value with a concrete Go type that can preserve precision."
So in Go 1.27, **the stdlib's answer to lossless numbers through a dynamically-typed boundary is still "quote them, or register a codec"**, which is exactly section 4's finding.
Note that the escape hatch offered is `WithUnmarshalers` - the same typed-registration seam recommendation 7 tells ferry to copy.

**`time.Duration` has no representation in v2, and this is a decision ferry cannot avoid.**
Measured: `json.Marshal(struct{ D time.Duration }{time.Second})` on `go1.27rc2` returns

```
json: cannot marshal from Go time.Duration within "/D": no default representation
```

v1's behaviour (nanoseconds as a bare number) survives only through the `FormatDurationAsNano` option.
xload handles `time.Duration` by comparing `Type.String()` to `"time.Duration"` ([load.go:301](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L301)), which this document already flags as wrong (5.9).
The Go team's answer to the same problem was to **refuse to guess and make the user choose**.
For a config mapper that is arguably the wrong default - `TIMEOUT=30s` is the single most common config value after strings and ints - but it means ferry cannot claim it is "just following json/v2" whichever way it goes.
Decide it, and record that json/v2 deliberately went the other way.

#### The four readings of "first class", with costs

**(a) Codec-chain recognition only.**
ferry's schema compiler probes for `MarshalerTo`/`UnmarshalerFrom` alongside its own interfaces and `encoding.TextMarshaler`/`TextUnmarshaler`, and calls them when the plane is JSON-shaped.

- **Cost:** a hard `encoding/json/jsontext` import in ferry core, so `go 1.27` and a build break under `nojsonv2`.
  A documented answer for what `MarshalJSONTo` means on a non-JSON plane, which is the awkward part: either "unsupported on this plane" (surprising) or "encode to `jsontext` bytes and hand the plane a `Raw`" (parse-twice).
- **Cheapest thing that could count as first class**, and the one most likely to be what users mean.

**(b) Adopt v2's *semantics* as ferry's own.**
`omitzero` as the omit rule, case-sensitive matching with an opt-in `case:ignore`, duplicate keys are an error.

- **Cost:** near zero in dependencies, since this is imitation, not import.
  It costs the discipline of overriding two v2 defaults that are wrong for ferry (nil-as-empty, nondeterministic map order) and of *not* adopting the JSON-defined ones (`omitempty`, merge semantics).
- This is the highest value-to-cost ratio of the four and it is available at any `go` version.
  It is also the only one that keeps ferry's tag grammar coherent across planes.

**(c) Use json/v2 or `jsontext` internally.**
For example, `jsontext.Value` as the `Raw` arm of ferry's value union (section 4f), `jsontext.Pointer` as the model for structured keys, or json/v2 as the implementation of a JSON plane's encode/decode.

- **Cost:** `go 1.27` and `nojsonv2` fragility for the whole of ferry core, in exchange for machinery ferry would otherwise write.
  `jsontext.Value` in particular is attractive because v1's `RawMessage` is now an alias for it, so a ferry `Raw` arm typed as `jsontext.Value` is directly usable by anyone holding a `json.RawMessage`.
  Against: it types ferry's value model to JSON at the very point where section 4 argues the model must be plane-agnostic, and `jsontext.Pointer`'s own godoc admits it cannot distinguish an array index from a numeric object name.
- **Recommend deciding this per-item rather than wholesale.**
  Copying `jsontext.Pointer`'s uniqueness property costs nothing; importing it costs a version floor.

**(d) Ship a json/v2-backed sub-module.**
`github.com/.../ferry/plane/json` as a separate Go module with its own `go.mod`.

- **Cost:** one more module to version, tag, and release, and the usual sub-module friction (a `replace` during development, a two-step release when the core API changes).
- **Benefit, and it is the decisive one:** ferry core keeps a lower `go` directive and does not break under `nojsonv2`, while the JSON plane gets `go 1.27` and first-class v2.
  This is also already the plan of record for data planes - "core ships the engine but no data-plane implementations" - so a json/v2-backed plane module is not a new distribution shape, it is the existing one.
- The tension: options (a) and (c) put v2 in **core**, which is precisely what this option avoids.
  If the codec chain in core has to know about `MarshalerTo`, the sub-module does not save core from the version floor.
  So (a) and (d) are in real conflict and the ADR has to resolve it.

**The one cross-cutting cost, stated once:** every option that puts an `encoding/json/v2` or `encoding/json/jsontext` import in a given module raises that module's floor to `go 1.27` **and** makes it unbuildable under `GOEXPERIMENT=nojsonv2` (verified).
`nojsonv2` is the release notes' own escape hatch for compatibility problems and is "expected to be removed in a future release", so the exposure is real but shrinking.

## 4. Typed values at the plane boundary

The question is how to model a dynamically-typed value that must round-trip losslessly, and what each model costs the person writing a new ferry backend.

xload's answer is "everything is a `string`, `spf13/cast` fixes it up", and section 5.8 shows what that costs: a YAML list silently becomes the empty string, and `null` is indistinguishable from `""`.
It is worth naming the deeper reason, because it is the same one that forced `encoding/json/v2` to exist.
The [json/v2 discussion](https://github.com/golang/go/discussions/63397) explains that v1's `MarshalJSON() ([]byte, error)` and `UnmarshalJSON([]byte)` are **API-bound, not implementation-bound** defects: forcing the value through a byte slice mandates an allocation and a re-parse that no amount of optimisation can remove.
`Loader.Load(ctx, key) (string, error)` has exactly this shape.
A YAML backend that already parsed `8080` into an `int` must render it back to `"8080"` so that `cast.ToInt64E` can parse it again.

### First, check the premise: most backends do not know the type

The ticket's framing assumes backends have type information to preserve.
Checked against each backend's own source, that is true for a minority.

| Backend | Does it know the type? | Evidence |
| --- | --- | --- |
| **Consul KV** | **No.** Opaque bytes. | `hashicorp/consul/api/kv.go:38-40`: "Value is the value for the key. This can be any value, but it will be base64 encoded upon transport. `Value []byte`" |
| **env vars** | **No.** | `os.LookupEnv` returns `string`. |
| **HTTP query params** | **No.** | `net/url.Values` is `map[string][]string`. |
| **Vault** | **Barely**, and it reached for `json.Number` to keep what it has. | `api/secret.go:31` is `map[string]interface{}`, but `ParseSecret` at `api/secret.go:358` calls `dec.UseNumber()` before decoding. |
| **JSON** | **Weakly.** All numbers are `float64`. | `$GOROOT/src/encoding/json/decode.go:55-64`. |
| **YAML** | **Yes, but by guessing** - and quoting is the real signal. | `yaml.Node` carries `Kind`, `Tag`, `Value string`. `resolve.go:126-205` is literally `strconv.ParseInt` then `ParseUint` then `ParseFloat`. But `indicatedString()` (`yaml.go:463-467`) forces `!!str` for any quoted scalar. |
| **TOML** | **Yes, without guessing.** The grammar disambiguates. | `go-toml/v2/unstable/kind.go:8-44`: `String, Bool, Float, Integer, LocalDate, LocalTime, LocalDateTime, DateTime, Array, InlineTable`. |

The decisive confirmation: koanf has a fully open `map[string]any` value model, and its Consul provider still emits `string`, because there is nothing else to emit (`koanf/providers/consul/consul.go:95`: `mp[pair.Key] = string(pair.Value)`).

**So a typed boundary buys YAML and TOML something real, JSON something partial, and Consul, env, and query params nothing at all.**
xload is pitched partly at HTTP query params, which is the zero-benefit case.
Say this out loud in the ADR rather than designing for the minority case silently.

The one thing YAML knows that a string boundary genuinely destroys is **quoting**: `port: "8080"` and `port: 8080` are different documents, and the difference is authoritative rather than inferred.

### The design space

There are four distinct answers in the Go ecosystem, and they differ mainly in whether the type set is **open** or **closed** and whether the value is an **interface** or a **struct**.

#### (a) Bare `any` with a documented restricted set - `database/sql/driver.Value`

`$GOROOT/src/database/sql/driver/driver.go:47-62`:

```go
// Value is a value that drivers must be able to handle.
// It is either nil, a type handled by a database driver's [NamedValueChecker]
// interface, or an instance of one of these types:
//
//	int64
//	float64
//	bool
//	[]byte
//	string
//	time.Time
type Value any
```

The type is literally `any`; the restriction is **documentation plus convention**, enforced at runtime by `driver.ValueConverter` implementations.
Six types, deliberately minimal: one signed integer width, one float width, no unsigned, no nested structure.

- **Representation.** Bare interface. Boxing allocation for non-pointer values.
- **Open or closed.** Nominally closed, but with two escape hatches: `NamedValueChecker` lets a driver accept anything, and `driver.Valuer` lets any user type project itself into the set.
- **Lossless-ness.** Poor by construction, and knowingly so. `uint64` above `math.MaxInt64` has no representation. Everything numeric collapses to `int64` or `float64`.
- **Cost to a new backend.** Very low, which is the point. A driver author handles six cases.
- **What it cost in practice.** The whole `pgx` ecosystem routes around it: pgx has its own `pgtype` system and a native interface precisely so it does not have to squeeze PostgreSQL's type system through six types. That is the clearest available evidence of what a too-small closed set costs a mature ecosystem.

The generic companion, **`sql.Null[T]`, added in Go 1.22** ([#60370](https://go.dev/issue/60370)), is directly instructive for ferry (`$GOROOT/src/database/sql/sql.go:415-418`):

```go
type Null[T any] struct {
	V     T
	Valid bool
}
```

This is the stdlib conceding that "absent" needs to be a separate bit from "value", and using generics to say it once instead of once per type (`NullString`, `NullInt64`, `NullBool`, ... all predate it).
**This is the strongest precedent for ferry's comma-ok absence signal**, and it is a genuinely post-generics API.

#### (b) Closed struct-based tagged union - `log/slog.Value`

This is the closest fit to ferry's requirements and the one I recommend studying first.
`$GOROOT/src/log/slog/value.go:18-40`:

```go
// A Value can represent any Go value, but unlike type any,
// it can represent most small values without an allocation.
// The zero Value corresponds to nil.
type Value struct {
	_ [0]func() // disallow ==
	// num holds the value for Kinds Int64, Uint64, Float64, Bool and Duration,
	// the string length for KindString, and nanoseconds since the epoch for KindTime.
	num uint64
	// If any is of type Kind, then the value is in num as described above.
	// If any is of type *time.Location, then the Kind is Time ...
	// If any is of type stringptr, then the Kind is String ...
	// Otherwise, the Kind is Any and any is the value.
	any any
}
```

with a ten-arm kind enum: `KindAny` (0, so the zero `Value` is nil), `Bool`, `Duration`, `Float64`, `Int64`, `String`, `Time`, `Uint64`, `Group`, `LogValuer`.

Four things about this design matter to ferry:

1. **It is a struct, not an interface, explicitly for allocation reasons** - "unlike type `any`, it can represent most small values without an allocation." The `any` field doubles as the kind discriminator, so there is no separate tag word.
2. **`KindAny` is the escape hatch**, and it is arm 0. The set is closed for the *fast* path and open for everything else. A backend with an exotic type is never blocked, it just loses the optimisation.
3. **`KindGroup` is the recursive arm** (`[]Attr`). Nesting is a first-class kind, not an afterthought. xload has no representation for nesting at all, which is why YAML lists vanish.
4. **`KindLogValuer` is lazy resolution.** `LogValuer` is documented as a way "to defer expensive operations until they are needed, or to expand a single value into a sequence of components." For ferry that maps onto a secret backend that should not fetch until the field is actually read.

The honest cost is visible in the source comment: "This implies that Attrs cannot store values of type `Kind`, `*time.Location` or `stringptr`."
The packing trick **leaks into the public contract**: three Go types cannot be stored.
That is the price of the zero-allocation representation, and ferry should decide whether it is worth paying or whether a plain explicit discriminator field is better.
Also note `_ [0]func()` deliberately makes `Value` non-comparable, which forecloses using it as a map key.

- **Representation.** Closed struct union with an `any` escape arm.
- **Open or closed.** Closed set, open escape hatch. Best of both, in my view.
- **Lossless-ness.** Good, with the three-type carve-out above.
- **Cost to a new backend.** Moderate: constructors per kind (`slog.IntValue`, `StringValue`, ...) plus a `Kind()` switch on the read side. Substantially more than `driver.Value`, substantially less than a full type system.

#### (c) Full type system with a type/value distinction - `cty`

`github.com/zclconf/go-cty`, the value model under HCL and Terraform.
The maximalist end.
`cty/value.go:30-33`:

```go
type Value struct {
	ty Type
	v  any
}
```

Every value carries its own `cty.Type`.
Three features distinguish it:

- **Unknown values.** `UnknownVal(t Type)` (`cty/unknown.go:27`) produces a value that is known to be of type `t` but whose content is not yet determined, with optional **refinements** that narrow the possible range. This exists for Terraform's plan phase.
- **Marks**, for propagating taint such as "this value is sensitive" through operations.
- **Capsule types** as the escape hatch: `func Capsule(name string, nativeType reflect.Type) Type` (`cty/capsule.go:79`), plus `CapsuleWithOps` so a capsule can participate in cty operations. This is a **named** escape arm, not a bare `any`.

- **Open or closed.** Closed primitives, extensible via capsule types.
- **Lossless-ness.** Excellent, and it is the only model surveyed that can express "I do not know this value yet".
- **Cost to a new backend.** High, and higher than it first looks. `cty/convert` alone is around 8000 lines. More importantly, the `Value` doc comment states the operating philosophy plainly: operations "panic if any invariants turn out not to be satisfied. These panic errors are not intended to be handled, but rather indicate a bug in the calling application that should be fixed with more checks prior to executing operations."
  For ferry, whose backends are written by third parties, a value model that panics on misuse pushes a large correctness burden onto exactly the people who will read the least documentation.

For ferry this is too much.
Unknowns and marks solve problems (deferred plan evaluation, sensitivity propagation) ferry does not have.
The one idea worth stealing is **capsule types**: a principled, named escape arm.

#### (c2) Closed union that is only meaningful alongside a descriptor - `protoreflect.Value`

`google.golang.org/protobuf/reflect/protoreflect`.
Instructive because it is the most performance-tuned closed union in wide use.
`value_unsafe.go:40-62`:

```go
// value is a union where only one type can be represented at a time.
// The struct is 24B large on 64-bit systems and requires the minimum storage
// necessary to represent each possible type.
//
// The Go GC needs to be able to scan variables containing pointers.
// As such, pointers and non-pointers cannot be intermixed.
type value struct {
	pragma.DoNotCompare // 0B
	typ unsafe.Pointer  // 8B - pointer to the Go type
	ptr unsafe.Pointer  // 8B - data pointer for String, Bytes, or interface
	num uint64          // 8B - Bool/Int32/Int64/.../Enum, or the String/Bytes length
}
```

Note `pragma.DoNotCompare`, the same non-comparability trick as `slog.Value`'s `_ [0]func()`, and that there is **no pure-Go fallback**: `unsafe` unconditionally.

Two properties matter for ferry:

1. **The value is not self-describing.** `value_union.go` documents that the same Go `int64` represents `Int64Kind`, `Sint64Kind`, and `Sfixed64Kind`, distinguished only by the accompanying `FieldDescriptor`. A `protoreflect.Value` handed around without its descriptor has lost information. That is a real cost: it means the value and the schema must travel together, everywhere.
2. **Type mismatches panic.** "Converting to/from a Value and a concrete Go value panics on type mismatch. For example, `ValueOf("hello").Int()` panics."

ferry should take the compactness lesson and reject both of these.
A ferry `Value` handed to a backend must be **self-describing** (the backend does not have the schema), and accessors must return `(T, bool)` or an error rather than panicking.

#### (d) Interface with a small required method set plus optional interfaces - `starlark.Value`

`github.com/google/starlark-go`, `starlark/value.go`.
The required set is tiny (`String()`, `Type()`, `Freeze()`, `Truth()`, `Hash()`), and capability is discovered through **optional interfaces** (`Indexable`, `Mapping`, `Iterable`, `Comparable`, `HasAttrs`, ...).

This is the fully **open** model: third parties add value types freely, and the runtime asks "does this value support indexing?" rather than "is this value a list?".

- **Cost to a new backend.** Low to add a type, but every *consumer* must handle the "this value does not implement the interface I need" case, so complexity moves from the type author to every call site.
- **Relevance to ferry.** The optional-interface *technique* is worth borrowing for backend capability discovery (`if s, ok := src.(Snapshotter)`, section 5.13). As the value model itself it is wrong for ferry: ferry's consumers are ferry's own leaf setters, and they want exhaustive matching, not capability probing.

#### (d2) Closed union with no escape hatch - `otel attribute.Value`

`go.opentelemetry.io/otel/attribute/value.go:29-34`, a 48-byte struct with 12 kinds.
Two things make it worth citing.

**It is fully closed with no escape arm**, and the consequences are visible: `AsInterface()` returns an unexported `unknownValueType{}` (`value.go:401,431`) for anything it cannot name, accessors are unchecked (`AsBool` is `rawToBool(v.numeric)`, with the doc saying "Make sure that the Value's type is BOOL"), and the enum has already grown twice, leaving a deprecated `INVALID = EMPTY` alias behind.
Compare `slog.Value`, whose `KindAny` escape arm meant its kind set never had to grow.
**This is the argument for including an escape arm from day one.**

**And OTel already ran the experiment ferry is considering, then reversed it.**
`log/DESIGN.md:482-497`:

> The original design defined `Kind`, `Value`, and `KeyValue` in `go.opentelemetry.io/otel/log`... Keeping log-specific types would **duplicate API surface, require conversion helpers, and make bridge code choose between two equivalent value models.** Therefore log records now use `attribute.Value`...

That is a recorded, primary-source failure of the "one value model here, a different one there" shape.
`DESIGN.md:449-461` also states the rationale for rejecting bare `any`: it "avoid[s] unconstrained `interface{}` handling in bridge implementations."

#### (e) Closed recursive sum type - `structpb.Value`, and why it is the cautionary tale

`google.golang.org/protobuf/types/known/structpb` is the closest existing analogue to a "config value" sum type: null, number, string, bool, struct, list.
It is also lossy in three separate ways, all documented in its own generated source:

- `struct.pb.go:315-316`: "When converting an int64 or uint64 to a NumberValue, **numeric precision loss is possible since they are stored as a float64**."
- `struct.pb.go:297-313`: `[]byte` is "stored as StringValue; base64-encoded", and comes back from `AsInterface` as a **string**, not bytes.
- `AsInterface` (`struct.pb.go:412-442`) returns `NaN`/`Inf` as the **strings** `"NaN"`/`"Infinity"`.

**If ferry designs a JSON-shaped closed union, this is the result.**
Do not copy it.

#### (f) Bare `map[string]any` - `koanf`, `viper`

The status quo for Go config libraries, and the thing ferry is presumably trying to beat.
`koanf` carries `map[string]interface{}` throughout and delegates struct mapping to `mapstructure` (section 2), which caches nothing.

**The important finding here is counterintuitive and it undercuts the whole "typed boundary" pitch if taken naively.**
koanf has full type information in its map, and its accessors still corrupt values.
`koanf.go:474-495`, `toInt64`, falls through to `strconv.ParseFloat(fmt.Sprintf("%v", v), 64)` then `int64(f)`, and `getters.go:10-16` discards the error:

```go
func (ko *Koanf) Int64(path string) int64 {
	if v := ko.Get(path); v != nil {
		i, _ := toInt64(v)   // error discarded
		return i
	}
	return 0
}
```

Measured end-to-end through real koanf v2 plus the real YAML parser:

| YAML input | `Get()` Go type | `Int64()` | `String()` |
| --- | --- | --- | --- |
| `big: 18446744073709551615` | `uint64` (correct) | **`9223372036854775807`** | `"18446744073709551615"` (correct) |
| `ratio: 3.9` | `float64` | **`3`**, no error | `"3.9"` |
| `quoted: "8080"` | `string` | `8080` | `"8080"` |
| `plain: 8080` | `int` | `8080` | `"8080"` |

**For `big`, the string path was lossless and the typed path was lossy.**
The loss did not happen in transport, it happened in conversion.
A typed boundary does not fix a bad accessor, and ferry should not claim it does.

koanf also sets `WeaklyTypedInput: true` by default at the struct boundary (`koanf.go:271`), opting back into weak typing anyway.

viper is worse in ways ferry should explicitly avoid:

- **Two conversion engines that disagree.** `viper.go:810-812` routes `GetInt` through `cast` while `Unmarshal` goes through `mapstructure` with different hooks. Measured on one viper instance with one set of env vars: `GetInt(f)` returns `1` silently while `Unmarshal` errors on the same key; `GetStringSlice` returns a **one**-element slice where `Unmarshal` produces **three**.
- **Destructive key folding.** `util.go:89-100` lowercases every key on every config read. Measured: `myKey: A` / `MyKey: B` / `MYKEY: C` collapses to `{"mykey":"C"}` - two values destroyed, no error, winner decided by map iteration order.
- Measured: `nul: null` makes `IsSet` false and vanishes from `AllSettings()`; `emptymap: {}` has `IsSet` true but is absent from `AllKeys()`, so `WriteConfig` drops it.

And the flat-key namespace is itself lossy, independent of value typing.
Measured over 300 runs, `koanf`'s `maps.Flatten({"a.b":1, "a":{"b":2}}, ".")` produced `{a.b:2}` 255 times and `{a.b:1}` 45 times.
**ferry inherits flat string keys from xload and inherits this bug with them.**

- **Lossless-ness.** Poor, backend-dependent, and undermined again at the accessor.
- **Cost to a new backend.** The lowest, and this is the number ferry has to beat. koanf's `Provider` is two methods, and its 20 providers are **31 to 246 lines, median around 120**.
- **Notable gap:** koanf has **no sink at all**. Grep for write/save/put across the repo returns nothing; the only egress is `Marshal(Parser) ([]byte, error)`. viper writes to files only. **Bidirectional struct-to-backend mapping is genuinely unoccupied territory**, which corroborates the prior-art sweep in `AGENTS.md`.

For completeness, `spf13/cast`, which xload depends on, measured directly:

```
"010"   -> ToInt=8    (base 0: octal)
"0x10"  -> ToInt=16   (base 0: hex)
"0080"  -> ToInt=0    (invalid octal; ToInt swallows the error)
"1.9"   -> ToInt=1
""      -> ToInt=0    (indistinguishable from a real 0)
"30"    -> ToDuration=30ns   (not 30 seconds)
"a b c" -> ToStringSlice=["a","b","c"]   (splits on whitespace)
"a,b,c" -> ToStringSlice=["a,b,c"]       (ONE element)
```

A zero-padded port `"0080"` silently becomes `0`.
`enabled: yes` from YAML is `true`; `ENABLED=yes` from env is `false`.

#### (f) Deferred decoding - `json.RawMessage`

Worth naming as an orthogonal technique rather than a competing model: keep the value **opaque bytes** until someone who knows the target type asks for it.
`json.RawMessage` (and `jsontext.Value` in v2) does this.

For ferry this is the right representation for exactly one case: a backend that holds an encoded blob it cannot cheaply interpret (a JSON string in a Consul key, a secret payload).
It should be one arm of the union, not the whole design, because it reintroduces the parse-twice cost everywhere else.

### The convergent finding: everyone ends up at tagged text

This is the most important result in this section, and it partially vindicates xload's ancestor.
**Every design surveyed falls back to a string for the values its model cannot hold.**

| Design | Fallback | Citation |
| --- | --- | --- |
| `driver.Value` scan | `asString` then `strconv.Parse*` | `$GOROOT/src/database/sql/convert.go:438-467, 498-520` |
| pgx through `database/sql` | `var d string` for 93 of 106 registered types | `pgx/stdlib/sql.go:849-855` |
| `cty/json` numbers | decimal text, with the type sent separately | `cty/json/value.go:31,62` |
| `structpb` int64 | float64, or quoted per protojson | `struct.pb.go:315-316` |
| `slog` `KindAny` | `fmt.Sprintf("%+v", ...)` | `text_handler.go:96-122` |
| `attribute.Value` | `uint64 > MaxInt64` becomes a string | otel bridge sources |
| `encoding/json` | `json.Number` is `type Number string` | `$GOROOT/src/encoding/json/decode.go:191` |
| **`encoding/json/v2`** | **the `string` tag option** | [doc.go:215-229](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/v2/doc.go) (line numbers updated for 1.27; was `doc.go:237-251` in 1.26) |
| `jsontext.Token` | raw decoded text plus a kind discriminator | [token.go:56-86](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/jsontext/token.go) |
| `yaml.Node` | `Tag string` plus `Value string` | `yaml.go:381-391` |

The json/v2 quote is the punchline, because it is the newest design in the list.
Re-read on `go1.27rc2` after v2 went GA, where it is **unchanged in substance and slightly expanded**:

> The `string` tag option can be used to specify that an integer type is to be quoted within a JSON string to avoid loss of precision.
> Furthermore, **v1 and v2 may still lose precision when unmarshaling into an `any` interface value, where unmarshal uses a float64 by default to represent a JSON number.**
> To change the default, specify the `WithUnmarshalers` option with a custom unmarshaler that pre-populates the interface value with a concrete Go type that can preserve precision.

Measured on `go1.27rc2`, which is the direct confirmation: `uint64(18446744073709551615)` marshals exactly on the wire, and unmarshaling those same bytes into an `any` yields `1.8446744073709552e+19`, while the `,string` field survives as the exact decimal text.

**In 2026, with `encoding/json/v2` GA, the standard library's official answer to lossless numbers through a dynamically-typed boundary is still: quote them as strings, or register a typed codec.**
The second half of that sentence is new in 1.27's wording and is worth noting, because the escape hatch the stdlib offers is `WithUnmarshalers` - the same typed-registration seam recommendation 7 tells ferry to build.

Nobody who cared about lossless scalars ended up at a native machine number.
They ended up at **decimal text plus a type tag**.
That is very close to what xload already does, with two differences that are the entire ballgame: xload's string carries **no tag**, and its conversion layer (`cast`) **swallows errors**.

### Performance is not an argument here

Measured, tagged-string versus packed union:

```
slog.Int64Value                 1.10 ns/op    0 B/op   0 allocs/op
TaggedString{Tag, FormatInt}   17.58 ns/op    7 B/op   0 allocs/op
slog.Value.Int64()              2.06 ns/op    0 B/op   0 allocs/op
TaggedString + ParseInt        27.20 ns/op    0 B/op   0 allocs/op
```

Sizes: `any` 16 B, `slog.Value` 24 B, `protoreflect.Value` 24 B, a `{Tag uint8; S string}` 24 B, `cty.Value` 32 B, `attribute.Value` 48 B.

A tagged string is roughly 13x slower to read than a packed union and costs the same 24 bytes.
**At 100 config keys that is 2.7 microseconds, once, at startup.**

Every performance rationale quoted in this section - protobuf's "always incurs an allocation for primitives", slog's "without an allocation", otel's "would decrease the performance" - is for a **per-request hot path**.
Note also that protobuf's Go-1.11-era claim is now partly outdated: measured, `any(int64)` for small values under 256 hits Go's static cache at 0 allocs; only large values cost one.

ferry's per-request case is HTTP query-param parsing, which is exactly the backend that has **no type information to preserve** (see the premise check above).
So the performance argument and the losslessness argument point at **disjoint** backends.
Do not build the value model on a perf rationale borrowed from a logging library.

### The decisive result: Load and Dump are not symmetric

The same logical config emitted through real koanf parsers, once with typed values and once stringified:

```
--- yaml / TYPED                --- yaml / STRING
name: svc                       name: svc
nul: null                       nul: ""
"on": true                      "on": "true"
port: 8080                      port: "8080"
ratio: 3.5                      ratio: "3.5"
tags:                           tags: a,b
    - a
    - b

--- toml / TYPED                --- toml / STRING
err = cannot convert type       port = "8080"
      <nil> to Tree             tags = "a,b"
```

And the round trip:

```
original          : {"on":true, "port":8080, "tags":["a","b"]}
typed  dump->load : {"on":true, "port":8080, "tags":["a","b"]}   exact
string dump->load : {"on":"true","port":"8080","tags":"a,b"}     permanently wrong
```

**The Load direction survives a string boundary. The Dump direction cannot.**

The reason is structural, not incidental.
On Load, the destination struct field type tells the mapper how to parse, so a string is sufficient - this is precisely ferry's advantage over koanf and viper, whose untyped accessors have to guess.
On Dump, the **sink** must choose a representation, and only the struct knows the type; a string-flattened sink emits `port: "8080"`, which is not a round trip, it is a wrong config file.

pgx documents the identical asymmetry in its own domain (`conn.go:672-674`): query modes that know the target type accept an ambiguous Go value, and modes that do not must reject it.

**This is the single strongest argument for typed values, and it is a Dump argument, not a Load argument.**
It is also why "keep strings for Load, add types only for Dump" is tempting - and why it should still be rejected, per the OTel reversal above.

### Comparison

| Model | Shape | Type set | Lossless | Cost to new backend | Nesting |
| --- | --- | --- | --- | --- | --- |
| `driver.Value` | bare `any`, documented set | closed(ish), 6 types | poor by design; overflow is an error | 2 methods per type | none |
| pgx `pgtype` | own codec system | open via `RegisterType` | good natively, `string` through the shim | ~20 methods, ~290 lines per type | native |
| `slog.Value` | 24 B packed union + `Any` arm | closed 10 kinds + escape | good, 3-type carve-out | 4 methods + 6 prose rules + an external guide | `KindGroup` |
| `attribute.Value` | 48 B struct | **closed, no escape** | overflow becomes string; enum grew twice | 12-arm switch, silent default | `SLICE`/`MAP` |
| `cty` | full type system, 32 B | closed + capsules | excellent, plus unknowns; JSON needs the type out of band | ~20k lines; panics on misuse | native |
| `protoreflect.Value` | 24 B unsafe union | closed, 11 types | **only with its descriptor** | descriptor machinery; panics | composite kinds |
| `structpb.Value` | closed recursive sum | closed | **int64 precision, bytes to base64, NaN to string** | trivial | native |
| `starlark.Value` | interface + 18 optional interfaces | fully open | no general serialization; `nil` leaks | 5 methods, no base type | via interfaces |
| `map[string]any` | bare `any` | open, unconstrained | poor, backend-dependent, corrupted at the accessor | **31-246 lines, median ~120** | native but untyped |
| **tagged text** (`json.Number`, `yaml.Node`) | string + tag | open by construction | none for scalars; errors deferred to parse | trivial | n/a |

### Recommendation

**A small closed struct union in the `slog.Value` shape, whose scalar leaf stores the source text rather than a machine number, with an explicit escape arm, a group arm, and a raw arm.**

Concretely, something like `{Kind Kind; text string; ref any}` with kinds `Absent`, `Null`, `Bool`, `Number`, `String`, `Bytes`, `List`, `Map`, `Any`.

The reasoning, and note that the "store text" part is a change of position forced by the evidence:

1. **Tagged text is where every lossless design converged**, including `encoding/json/v2` in 2026. A native numeric leaf re-creates the `float64` problem that `structpb` has and that `json.Number` exists to fix.
2. **The performance case for a packed native union does not apply to ferry.** Measured at 27 ns to read a tagged string, versus 2 ns for a packed union. At 100 keys, once at startup, that is 2.7 microseconds. Every library that packed natively did so for a per-request hot path ferry does not have.
3. **`KindAny` is why slog's kind set never grew and `attribute.Value`'s grew twice.** Ship the escape arm on day one, and name it (capsule-style) rather than leaving it a bare `any`.
4. **A group arm is required, not optional.** xload's flattening is exactly where the YAML list is lost (5.8, reproduced).
5. **The real payoff is Dump, not Load.** Load survives strings because the struct field type drives parsing; Dump cannot, because the sink must choose a representation. That asymmetry is the honest justification for the whole change.
6. `sql.Null[T]` (Go 1.22) validates separating presence from content rather than encoding absence as a magic value, and it validates doing it generically on day one - the stdlib spent eleven years hand-writing `NullString`, `NullInt64`, `NullByte` before generics collapsed them.

**The costs, stated plainly:**

- ferry owns the value taxonomy forever. Adding a kind later is an API change.
- **Backend authoring cost is the main argument against, and the bar is known: koanf's providers are 31-246 lines, median around 120, off a two-method interface.** If ferry's backend interface makes a Consul provider meaningfully longer than 156 lines, the value model has overreached - and remember Consul gains nothing from typing at all.
- **Do not copy slog's unsafe packing.** It leaks into the public contract (three Go types unstorable) and forces non-comparability, for a speedup that is irrelevant here. Use an explicit `Kind` field.
- Accessors must return `(T, bool)` or an error. `cty`, `protoreflect`, and `slog` all panic on kind mismatch and all document it as intentional, because their callers type-check first. ferry's callers are third-party backend authors.
- ferry's `Value` must be **self-describing**. `protoreflect.Value` is uninterpretable without its `FieldDescriptor`; a config sink that receives a value and no schema cannot decide how to write it.

**Two rejected alternatives, with the reason:**

- **"Strings for Load, typed values for Dump."** Tempting, because §"Load and Dump are not symmetric" shows Load genuinely survives strings and this is the smallest change from xload. Reject it: OTel ran exactly this two-value-model shape and reversed it, recording that it would "duplicate API surface, require conversion helpers, and make bridge code choose between two equivalent value models" (`log/DESIGN.md:482-497`).
- **`driver.Value`-style "document the allowed set" with no escape hatch.** Cheapest for backend authors, but it provably degenerates: pgx routes 93 of its 106 types through `string` when forced back through it (`pgx/stdlib/sql.go:849-855`). All of the constraint, none of the benefit.

**Six things to do regardless of which model wins:**

1. **Three-state presence is non-negotiable.** `absent` is not `null` is not `""`. xload conflates all three at [load.go:147](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L147); viper drops `null` entirely. This is the single most valuable thing the new boundary buys.
2. **Numbers as source text, parsed at the destination where the target type is known, returning an error.** Never `float64` in the middle.
3. **One conversion authority.** Viper's `Get*`-versus-`Unmarshal` split produces two different answers for one key, one of them silent. If ferry ships both a struct path and an accessor path, they must share one code path.
4. **Separate the tree walk from the leaf switch**, as slog does (`handler.go:476-531` resolves `LogValuer` and recurses `KindGroup`, so `appendJSONValue` only ever sees 8 of 10 kinds). This roughly halves what a backend author writes.
5. **Ship a conformance suite.** slog's `Handler` is 4 methods but carries six unchecked prose rules, and its own doc says "Before implementing your own handler, consult https://go.dev/s/slog-handler-guide". What rescues it is `testing/slogtest`, 17 explained conformance cases. If ferry's sink contract has any obligation the compiler cannot check, ship the tests with it.
6. **Do not fold key case, and decide the delimiter-collision rule now.** Both are key-space losses independent of value typing, and ferry inherits flat string keys from xload. Measured: viper destroys two of three case-variant keys; koanf's `Flatten` resolves `{"a.b":1}` against `{"a":{"b":2}}` nondeterministically (255/45 over 300 runs).

Worth stealing from slog's `LogValuer` (`value.go:487-516`) if ferry adds lazy values for secret backends: resolution is **iterative not recursive**, bounded at 100 hops, and `recover()`s a panicking implementation into an error value rather than crashing the caller.
Worth stealing from `cty`'s marks (`marks.go:11-26`) if ferry ever handles secrets: a marked value is deliberately made **unusable** by the normal integration methods, so callers cannot silently drop the mark on a round trip.

Two further rules fall out of the survey, and both are about **not** copying the fast models:

- **ferry's `Value` must be self-describing.** `protoreflect.Value` is only interpretable alongside its `FieldDescriptor`. ferry hands values to third-party backends that do not have the schema, so this is disqualifying.
- **Accessors must not panic.** `cty` and `protoreflect` both panic on type mismatch and both document it as intentional, because their callers are compilers and runtimes that type-check first. ferry's callers are backend authors. Return `(T, bool)` or an error.

**Verification status.**
This block covers the whole document, not only section 4.

**Go 1.26 (stable), original research.**
`driver.Value`, `sql.Null[T]`, `slog.Value`, `encoding/json`, and `encoding/json/v2` were read from `$(go env GOROOT)/src` on go1.26.5 and are quoted verbatim.
`cty`, `protoreflect`, `structpb`, `koanf`, `viper`, `cast`, `pgx`, `yaml.v3`, `go-toml/v2`, `mapstructure`, and `otel` were read from shallow clones taken 2026-07-31; declarations and doc comments above are verbatim.
Version claims are checked against `$(go env GOROOT)/api/go1.NN.txt`.
Measurements marked as measured were executed on darwin/arm64, go1.26.5.

**Go 1.27 (release candidate), 2026-07-31 amendment.**
A real toolchain was installed and used: `go install golang.org/dl/go1.27rc2@latest && go1.27rc2 download`, giving `go version go1.27rc2 darwin/arm64` at `~/sdk/go1.27rc2`.
Everything marked **(1.27 RC)** was verified against that toolchain's own `api/go1.27.txt` and `src/` tree, not against the draft notes.
Specifically verified:

- Package-level API counts per package from `api/go1.27.txt`, including the zero counts for `reflect`, `errors`, `iter`, `sync`, `slices`, `maps`, and `cmp`.
- `encoding/json/v2` and `encoding/json/jsontext` import and run with `GOEXPERIMENT` unset, and fail to build under `GOEXPERIMENT=nojsonv2`.
- `Options` sealing: reproduced the compile failure when implementing it from outside; `GetOption` reproduced working from outside.
- Marshaler precedence with a type implementing both `Marshaler` and `MarshalerTo`, and the `WithMarshalers` override, by execution.
- `format:` tag rejection, `time.Duration` "no default representation", duplicate-name rejection, nil slice/map round trip against v1 and against v2 with and without `FormatNil*AsNull`, map-ordering determinism over 50 marshals, and uint64 precision through `any`, all by execution.
- Generic methods, the interface-method restriction, the `-lang` gate, and generalized type inference, by compilation.
- That `new(expr)` is a **Go 1.26** language change and not a 1.27 one, by compiling it on go1.26.5 under a `go 1.26` directive and by locating it in the go1.26 release notes. The first pass of this document missed it entirely; it is now recorded in "Other things worth knowing".
- `-stdversion` present in `defaultVetFlags` in 1.27 and absent in 1.26, by direct diff of the two toolchains' `cmd/go/internal/test/test.go`.
- `inline` -> `embed`, `unknown` removal, and `format` removal by diffing `encoding/json/v2/doc.go` between the two toolchains, cross-referenced to commits `6a1dd03`, `c9cbeb0`, `0b54a75` and issues [#79985](https://go.dev/issue/79985), [#77271](https://go.dev/issue/77271), [#79071](https://go.dev/issue/79071).
- Issue states and milestones for [#71497](https://go.dev/issue/71497), [#70471](https://go.dev/issue/70471), [#73450](https://go.dev/issue/73450), [#71151](https://go.dev/issue/71151), [#73457](https://go.dev/issue/73457), [#74819](https://go.dev/issue/74819), [#77703](https://go.dev/issue/77703), [#74472](https://go.dev/issue/74472) via `gh issue view`.

Known gaps, stated rather than papered over:

- **Go 1.27 is an RC, not GA.** `go.dev/dl` reports `go1.27rc2` with `"stable": false`. Anything sourced from `refs/tags/go1.27rc2` can change before release. **Re-verify the v2 tag option set, the `nojsonv2` behaviour, and `api/go1.27.txt` at GA before any of it hardens into an ADR.**
- The prose of the release notes was read from `go.dev/doc/go1.27`, which is the draft; nothing in it was accepted where the toolchain disagreed, but sections quoted for wording (the language changes, the json paragraphs) are draft wording.
- **`stdversion` could not be shown to actually fire on a 1.27 symbol**, because the vendored `x/tools` stdlib manifest in rc2 has no Go 1.27 entries. The analyzer's presence in `defaultVetFlags` is verified; its usefulness at 1.27 is not.
- **The RC toolchain's json/v2 was not benchmarked.** The release notes' claim that "unmarshal performance is significantly faster" is taken on their word. The unresolved gap noted in section 2 - that no repo surveyed benchmarks cached versus uncached schema resolution in isolation - is unchanged.
- **No ferry prototype was built against json/v2.** The four "first class" readings and their costs are analysis, not measurement. In particular the parse-twice cost of routing a non-JSON plane through `jsontext` bytes is asserted from the v2 design rationale, not measured.
- **`starlark.Value`**: interface declarations and package doc only. Nothing verified on its `Int` representation, `Freeze` cost, or serialization. If the optional-interface pattern becomes load-bearing in an ADR, re-read `starlark/value.go` first.
- **`cty`**: `convert/`, `gocty/`, and `msgpack/` were not read line by line, so the `gocty` ergonomics and msgpack precision claims are structural inference.
- **`protobuf`**: `types/dynamicpb` not read at all.
- The otel container-allocation figures are second-hand within this research; the 48-byte size is measured directly.

## 5. Concrete rewrite candidates in xload's design

Ordered by how much they would change ferry's shape, not by severity.
Every item cites the xload file and line, and the ones marked **reproduced** were verified by running code against `xload@latest` on go1.26.5.

### 5.1 The `Loader` signature cannot express absence

[loader.go:9-11](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/loader.go#L9-L11) is `Load(ctx context.Context, key string) (string, error)`.
`OSLoader` collapses a missing variable to the empty string ([loader.go:27-36](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/loader.go#L27-L36)), and so does `MapLoader` ([maps.go:16-23](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/maps.go#L16-L23)).
The consequences propagate everywhere: `required` is implemented as `val == "" && meta.required` ([load.go:147](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L147), [load.go:195](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L195), [async.go:180](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/async.go#L180)), `setVal` silently no-ops on empty ([load.go:267-269](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L267-L269)), and `decode` refuses to hand an empty string to a decoder at all ([load.go:415-417](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L415-L417)).
So `FOO=""` cannot satisfy a `required` field, and a custom decoder can never be asked to interpret the empty string.
The `cached` provider had to invent a *different* interface, `Get(key string) (*string, error)`, precisely to express the third state, and its doc comment says so outright ([providers/cached/cache.go:8-18](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/providers/cached/cache.go#L8-L18)).

**Trade-off for ferry.**
Any signature that can express absence costs the backend implementor something.
`(Value, bool, error)` is the cheapest and most Go-idiomatic (comma-ok), allocates nothing, and is trivially implementable.
`(*Value, error)` allocates and invites nil-deref bugs.
A sentinel `ErrNotFound` forces every backend to get error wrapping right and every caller to `errors.Is`.
Recommend comma-ok.

### 5.2 Two walks that have already diverged - **reproduced**

[load.go:71-207](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L71-L207) (`doProcess`) and [async.go:59-161](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/async.go#L59-L161) (`processAsync`) are the same walk written twice.
They are not equivalent.
The sync path enters the struct-with-a-key branch on `meta.key != ""` ([load.go:141](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L141)); the async path additionally requires `hasDecoder(fVal)` ([async.go:122](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/async.go#L122)).
The sync path also has a `val == "" && isNilStructPtr` escape ([load.go:151-153](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L151-L153)) that the async path lacks entirely.
The net effect is that when a struct field carries both a key and a `Decode` method and that key resolves to empty, the sync path falls through and recurses into the nested fields while the async path skips them:

```go
type Inner struct {
    A string `env:"A"`
    B string `env:"B"`
}
func (i *Inner) Decode(s string) error { i.A = "decoded:" + s; return nil }

type Cfg struct { In Inner `env:"IN"` }

ldr := xload.MapLoader{"IN": "", "A": "from-A", "B": "from-B"}
```

Result on go1.26.5, same struct, same loader, same input:

```
sync : {In:{A:from-A B:from-B}} err=<nil>
async: {In:{A: B:}} err=<nil>
```

Adding `xload.Concurrency(4)` silently changes the answer and reports no error.
That is the strongest single argument in this document for writing the walk exactly once.

**And xload does have a test for exactly this, which structurally cannot catch it.**
[load_test.go:755-777](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load_test.go#L755-L777) runs every table case twice, once serially and once with `Concurrency(5)`, asserting the same `tc.want`.
But `input` is `any` ([load_test.go:18](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load_test.go#L18)) holding a **pointer** to a struct built once in the table literal, and the same pointer is passed to both subtests.
The serial subtest populates it; the concurrent subtest then loads into an already-populated struct and asserts against `tc.want`.
Any field the concurrent path fails to set is still correct, because the serial path set it a moment earlier.
The equivalence test passes by construction.

**Trade-off for ferry.**
Beyond writing the walk once: if ferry keeps a concurrent mode, its equivalence tests must use a **fresh destination per subtest**, and ideally be property-based (generate a struct shape, assert serial and concurrent produce identical output and identical errors).

**Trade-off for ferry.**
One walk with a pluggable scheduler (serial vs bounded pool) costs one indirect call per leaf and makes the concurrent path harder to reason about in isolation.
Two walks cost correctness, and this repo is the proof.
Write it once.

### 5.3 No schema caching - measured

Both walks call `reflect.Type.Field(i)`, `Tag.Get`, and `parseField` for every field on every call ([load.go:85-103](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L85-L103), [async.go:77-95](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/async.go#L77-L95)).
`parseField` does `strings.Split(tag, ",")` per field per call ([load.go:219](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L219)).
`decode` runs a five-arm interface type switch per field per call ([load.go:424-435](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L424-L435)).
`PrefixLoader` allocates a fresh closure per nested struct per call ([loader.go:20-24](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/loader.go#L20-L24), called at [load.go:172](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L172)).

Benchmarked against a 12-key struct with two prefixed nested structs, backed by an in-memory `MapLoader` so essentially all cost is the walk:

```
BenchmarkLoad-8                    3343 ns/op   2992 B/op   54 allocs/op   (defaults)
BenchmarkLoadNoCollision-8         2628 ns/op   2256 B/op   48 allocs/op   (SkipCollisionDetection)
BenchmarkCachedSchema-8             476 ns/op    320 B/op    5 allocs/op   (prototype)
```

The prototype is ~90 lines: compile `[]leaf{index []int, key string, kind reflect.Kind}` once, store it in a `sync.Map` keyed by `reflect.TypeFor[T]()` behind a `sync.Once` per entry, then walk with `Value.FieldByIndex`.
That is **5.5x faster and ~10x fewer allocations** against the fair `SkipCollisionDetection` baseline.

**Honest caveat.**
The prototype does not implement the decoder chain, pointer-to-struct initialisation, maps, `required`, collision detection, or context propagation.
Adding those back will cost some of the gap.
But the expensive ones get *cheaper* under compilation, not more expensive: "does this field have a decoder" is resolved once at compile time into a stored function pointer instead of five interface assertions per call, and prefixes are folded into full keys at compile time instead of a closure per nested struct per call.

Also note that collision detection alone costs 21% of the runtime and 6 allocations ([load.go:52-61](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L52-L61) wraps the loader in a counting closure), and it is *on by default* ([options.go:49](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/options.go#L49)).
With a compiled schema, key collisions are a property of the schema and can be detected **once at compile time, for free**, rather than by instrumenting every load.

**Trade-off for ferry.**
A `reflect.Type`-keyed cache is unbounded.
For statically declared types that is fine - the set is finite and small.
A program that manufactures types at runtime via `reflect.StructOf` would leak; that is an acceptable documented limitation, and it is the same limitation `encoding/json` has carried for a decade.

### 5.4 First error only, no aggregation - **reproduced**

Every error path in both walks is `return err` on the first failure.
There is no `errors.Join` anywhere in the package, and no error type implements `Unwrap() []error` ([errors.go](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/errors.go) in full).
With three fields that all fail to parse:

```
err = xload: unable to type-cast value for key A: unable to cast "nope" of type string to int64
Unwrap()[]error? false
```

Fields `B` and `C` are never reported.
For a config loader this is the wrong default: the user fixes `A`, re-runs, and discovers `B`.

**But the concurrent path aggregates, and the serial path does not.**
This is a second, independent instance of the 5.2 drift.
`sourcegraph/conc`'s pool joins task errors, so with three missing required fields:

```
sync  err=required key missing: P            (only the first)
async err=required key missing: A
          required key missing: B
          required key missing: C            (all three)
```

So `xload.Concurrency(4)` silently changes not just *which* error you get but *how many*.
On a struct with one `*Sub` and one `string`, both required and both absent, the serial path reports `P` and the concurrent path reports `Q` - different errors, same input.

The aggregate the concurrent path produces is worth studying as a counter-example, because it is exactly what ferry must not ship.
Walking the returned error tree for three failed fields:

```
*errors.joinError
  *errors.joinError
    *errors.joinError
      *xload.ErrRequired
    *xload.ErrRequired
  *xload.ErrRequired
```

The pool joins **pairwise**, so the result is a left-leaning nested tree, not a flat list.
A caller who does the obvious `err.(interface{ Unwrap() []error }).Unwrap()` gets **2** children, not 3 leaves, and has to recurse.
Ordering is nondeterministic: over 40 identical runs the message came out as `A,B,C` 15 times and `A,C,B` 25 times.

**Trade-off for ferry.**
Aggregating means continuing past a failed field, which means the destination struct is partially populated when `Load` returns an error.
That needs to be an explicit, documented policy (ferry should say: on error, the destination is unspecified, do not use it), plus probably a `StopOnFirstError` option for callers who want the old behaviour.

### 5.5 Nondeterministic error output - **reproduced**

`collisionMap.err` ranges over a Go map ([collision.go:44](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/collision.go#L44)) and `collisionSyncMap.err` ranges over a `sync.Map` ([collision.go:22](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/collision.go#L22)); neither sorts.
40 runs of an identical input produced three different messages:

```
 29 x xload: key collisions detected for keys: [K J I]
  9 x xload: key collisions detected for keys: [J I K]
  2 x xload: key collisions detected for keys: [I K J]
```

`ErrCollision.Keys()` ([errors.go:74-79](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/errors.go#L74-L79)) copies the slice but does not sort it either.
Fix is one `slices.Sort` call.
Since ferry needs deterministic **dump** output as a first-class property, treat determinism as a package-wide invariant rather than a per-site fix: every map iteration that reaches a user-visible artifact gets sorted.

### 5.6 Lost-update race in the concurrent collision counter

[collision.go:9-16](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/collision.go#L9-L16):

```go
v, loaded := m.LoadOrStore(key, 1)
if loaded {
    m.Store(key, v.(int)+1)
}
```

That is a non-atomic read-modify-write.
Concurrent `add` calls for the same key can lose increments.
This is a **logical** lost update, not a data race - each `sync.Map` operation is individually atomic, so `go test -race` is clean (verified, 200 iterations at `Concurrency(8)`), which is exactly why it has survived.
In practice a 2-way collision is still detected (only one caller can observe `loaded == false`), so it undercounts rather than fails open, but it is still wrong and it boxes an `int` into `any` on every key.
An `atomic.Int64` per entry, or simply doing collision detection at schema-compile time (5.3), removes the problem.

### 5.7 `reflect.DeepEqual` used as a "was anything set?" probe

[load.go:107-117](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L107-L117) and its async twin [async.go:163-171](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/async.go#L163-L171) allocate a fresh zero value with `reflect.New(...).Interface()` and `reflect.DeepEqual` it against the populated struct to decide whether to write back a nil struct pointer.
This is expensive (an allocation plus a recursive deep comparison per nil-struct-pointer field per load) and semantically wrong: a nested struct that was legitimately loaded to all-zero values is indistinguishable from one that was never touched, so the pointer is left nil.
Threading a `bool` "any leaf was set" through the walk is cheaper and correct.

### 5.8 Type information destroyed at the boundary - **reproduced**

[maps.go:33-47](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/maps.go#L33-L47) flattens `map[string]any` to `map[string]string` with `cast.ToString(value)` in the default arm.
`cast.ToString` swallows its error.
Running `FlattenMap` over a realistic YAML shape:

```
nested_k   => "v"
nullv      => ""
port       => "8080"
ratio      => "0.5"
servers    => ""
```

A YAML list becomes the **empty string**, silently.
A YAML `null` is indistinguishable from an empty string.
The YAML provider is built directly on this ([providers/yaml/yaml.go:36-50](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/providers/yaml/yaml.go#L36-L50)), so `servers: [a, b]` in a config file loads as nothing with no error.
This is the single strongest argument for typed values at the plane boundary (section 4): the YAML backend already knew it had a sequence and was forced to throw that away.

### 5.9 The decoder chain is fixed, one-directional, and context-free

[load.go:403-439](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L403-L439).
Problems, in order of importance to ferry:

- No `Encode` counterpart at all. ferry is bidirectional; this interface has to be designed as a pair from day one.
  Note that `xloadtype` already grew accidental encoders - `URL.String()`, `Listener.String()`, `Endpoint.String()` ([type/url.go:12](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/type/url.go#L12), [type/listener.go:14](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/type/listener.go#L14), [type/endpoint.go:15](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/type/endpoint.go#L15)) - unspecified, untested as a round trip, and not used by the library.
- Precedence is hardcoded and undocumented as a policy: `Decoder` > `TextUnmarshaler` > `json.Unmarshaler` > `BinaryUnmarshaler` > `GobDecoder`. A type implementing both `json.Unmarshaler` and `BinaryUnmarshaler` gets JSON, arbitrarily.
- No way to register a decoder for a type you do not own. `time.Duration` is special-cased by **type name string comparison**, `ty.String() == "time.Duration"` ([load.go:301](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L301)), which also silently misfires for any user type named `Duration` in a package named `time`.
- `Decode(string) error` takes no `context.Context` even though the whole walk is context-carrying.
- Decoders never see an empty input ([load.go:415-417](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L415-L417)).

**Trade-off for ferry.**
Typed registration (section 1c) plus a documented, overridable precedence list costs more API surface than "implement this one interface", but it is the difference between a library you can extend and one you have to fork.

### 5.10 Composite values are string-splitting, and it is not escapable

`setVal` handles maps by `strings.Split(val, meta.delimiter)` then `strings.Split(v, meta.separator)` ([load.go:343-372](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L343-L372)) and slices by a single `strings.Split` ([load.go:374-394](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L374-L394)).
There is no escaping, so a value containing the delimiter is unrepresentable.
Nested maps, slices of structs, and arrays are all unsupported and fall to `ErrUnknownFieldType` ([load.go:396-397](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L396-L397)).
The tag grammar has the same problem: `parseField` splits the tag on `,` ([load.go:219](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L219)), so `env:"K,delimiter=,"` cannot be written.

If ferry's boundary carries typed values, a backend that natively has a list (YAML, JSON, Consul-with-JSON) hands over a list and none of this arises.
String splitting stays as the fallback for genuinely flat planes such as environment variables.

### 5.11 The YAML provider silently discards parse errors - **reproduced**

[providers/yaml/yaml.go:18-29](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/providers/yaml/yaml.go#L18-L29):

```go
func NewFileLoader(path, sep string) (_ xload.MapLoader, err error) {
	f, err := os.Open(path)
	...
	defer func() {
		err = f.Close()
	}()
	return NewLoader(f, sep)
}
```

The deferred assignment **overwrites** the named return with `Close`'s error, which is normally nil.
Feeding it malformed YAML:

```
loader=map[] err=<nil>
```

A parse failure returns a nil loader and a nil error.
This is a one-line `errors.Join(err, f.Close())` fix (section 3), and it is worth citing because it is exactly the class of bug that a pre-`errors.Join` codebase accumulates.

### 5.12 `SerialLoader` precedence is unexpressible

[loader.go:40-57](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/loader.go#L40-L57) is "last non-empty value wins".
It queries **every** loader for **every** key, so N backends means N round trips per key even when the first one answered.
And because empty means absent (5.1), a later, higher-priority source can never override an earlier value back to empty.

**Trade-off for ferry.**
Once absence is expressible, first-match-wins becomes both correct and cheap (short-circuit on the first present value).
That flips the default precedence relative to xload, which is a breaking difference worth an explicit ADR rather than a quiet change.

### 5.13 The per-key pull model amplifies backend round trips - **reproduced**

`Loader.Load` is called once per leaf field, with no memoisation within a single `Load` call.
Instrumenting a counting loader:

```
backend calls for 2 fields sharing one key: 2
SerialLoader over 3 backends, 2 fields:     2 2 2
```

Two struct fields tagged with the same key produce two backend calls.
Wrap three backends in a `SerialLoader` and a 2-field struct produces **6** backend calls, because `SerialLoader` queries every loader for every key with no short-circuit ([loader.go:44-52](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/loader.go#L44-L52)).
For an in-process map that is irrelevant.
For Consul, Vault, or an HTTP config service it is `fields x backends` network round trips per load.

The `cached` provider exists precisely to paper over this ([providers/cached/loader.go:20-42](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/providers/cached/loader.go#L20-L42)), but it caches with a **TTL across loads**, which is a different and weaker thing than deduplicating within one load: it trades correctness (stale reads) for a problem that is really a shape problem.

**Trade-off for ferry.**
Three options, and this is a genuine fork in the design:

1. Keep the per-key pull interface and memoise within a single load. Cheapest, keeps backends trivial, still N round trips for N distinct keys.
2. Give the source a batch entry point (`LoadAll` / snapshot) and let the walk read from the snapshot. One round trip, but every backend implementor must be able to enumerate, which a Vault or a secret store may not want to do.
3. Both, with the batch form optional via an interface upgrade (`if s, ok := src.(Snapshotter); ok`). Most flexible, most surface.

Because ferry knows the full key set from the compiled schema **before** doing any I/O (section 5.3), option 2 becomes viable in a way it never was for xload: ferry can hand the backend the exact list of keys it wants.
That is a capability xload structurally cannot have, and it is arguably a bigger win than anything generics contribute.

### 5.14 Minor, but worth not carrying over

- Two ways to set the loader: `WithLoader` ([options.go:20-22](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/options.go#L20-L22)) and `LoaderFunc.apply` / `MapLoader.apply` ([loader.go:59](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/loader.go#L59), [maps.go:31](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/maps.go#L31)) make some loaders directly usable as options and others not.
- `for fVal.CanAddr() { fVal = fVal.Addr() }` ([load.go:135-137](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L135-L137), [load.go:419-421](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L419-L421), [async.go:223-225](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/async.go#L223-L225)) is written as a loop but can only ever execute once, since `Addr()` returns a non-addressable pointer. Harmless, but it signals uncertainty about the reflection model.
- `doProcessConcurrently` ([async.go:40-56](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/async.go#L40-L56)) selects between `ctx.Done()` and a `doneCh` that is already ready by the time the select runs, so on a cancelled context the returned error is chosen non-deterministically between `ctx.Err()` and the pool's error.
- Errors are declared with **value** receivers on `Error()` ([errors.go:11](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/errors.go#L11)) but returned as **pointers** ([load.go:148](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L148)), so both `ErrRequired` and `*ErrRequired` satisfy `error` and only one of them is ever actually returned. Reproduced:

  ```
  errors.As(err, &pe) where pe *xload.ErrRequired  -> true
  errors.As(err, &ve) where ve  xload.ErrRequired  -> false
  ```

  Users who write the natural `var e xload.ErrRequired; errors.As(err, &e)` get a silent false. ferry should use pointer receivers on `Error()` so only the pointer form implements `error`, and state the convention in the package doc.

## What this means for ferry

Blunt list, each with the cost stated.
None of these is an ADR; they are inputs to ADRs.

### 1. Compile a schema per type and cache it. This is the highest-value change, and it is not about generics.

Measured 5.5x time and 10x allocations on ferry's own shape (5.3).
Use `sync.Map[reflect.Type] -> *schema` where `*schema` is cheap to allocate and holds a `sync.OnceValues[*compiled, error]` guarding the actual walk, following `encoding/json/v2`'s two-level pattern rather than v1's.
Compile *behaviour* into the leaf, not just data: resolved codec function pointer, fully-resolved key string, resolved zero-check predicate.

**Cost.** An unbounded cache that leaks for `reflect.StructOf`-generated types.
Documented limitation, same as every library surveyed.
Also: if ferry allows a per-instance tag name, either key the cache per instance (sqlx model) or factor config out of the schema and thread it at call time (json/v2 model). The second is better and harder.

### 2. Make the entry point `Load[T]` / `Dump[T]`.

`ErrNotPointer` stops existing.
`reflect.TypeFor[T]()` lets ferry compile and **validate the schema before any I/O**, so tag errors surface at construction, not halfway through a mutation.

**Cost.** `ErrNotStruct` survives, because Go cannot constrain a type parameter to "any struct" (verified).
Do not claim otherwise in the README.

### 3. Write the walk exactly once.

xload's two walks produce different results for the same input, and its equivalence test cannot catch it because both subtests share one destination pointer (5.2).
If ferry keeps a concurrent mode, the walk takes a scheduler, and the equivalence tests use a fresh destination per subtest and are property-based.

**Cost.** One indirect call per leaf, and a concurrent path that is harder to reason about in isolation.
Worth it.

### 4. Let values cross the boundary typed, but store scalars as source text, and justify it on Dump.

The reproduced YAML-list-becomes-empty-string bug (5.8) and the `required` conflation (5.1) are the same root cause.
Recommend a **closed struct union in the `slog.Value` shape whose scalar leaf holds the source text**, with an escape arm, a group arm, and comma-ok for absence: `Get(ctx, key) (Value, bool, error)`.

Three corrections to the obvious version of this, all forced by evidence in section 4:

- **Store text, not machine numbers.** Every lossless design converged on tagged text, including `encoding/json/v2`, whose 2026 answer to precision is still "quote it as a string". A native numeric leaf recreates `structpb`'s `float64` bug.
- **Justify it on Dump.** Load survives a string boundary because the struct field type drives parsing; Dump cannot, because the sink must choose a representation. Measured: a typed dump-then-load round trip is exact, a stringified one is permanently wrong. Do not sell this as a Load-side win, because it mostly is not.
- **Do not sell it on performance.** Measured 27 ns to read a tagged string versus 2 ns for a packed union: 2.7 microseconds at 100 keys, once, at startup. The libraries that packed natively did so for per-request hot paths.

**Cost.** ferry owns the taxonomy forever, and backend authoring gets harder than xload's one-method interface.
The bar to beat is known: koanf's 20 providers are 31-246 lines, median ~120, off a two-method interface.
Note also that a typed boundary buys **nothing** for Consul, env vars, or query params, which know only bytes (section 4 premise check) - and query params are xload's pitched hot path.
This is the decision with the most downstream consequence and it deserves its own ADR.

### 4b. Three-state presence, one conversion authority, and errors that surface.

Independent of which value model wins:
`absent` is not `null` is not `""`;
the struct path and any accessor path must share one conversion implementation (viper's two engines give two different answers for one key, one silently);
and conversion must return errors rather than swallowing them.
Measured: koanf's `Int64()` turns `18446744073709551615` into `9223372036854775807` with a nil error, while its `String()` is lossless.
**A typed boundary does not fix a bad accessor**, so do not let the value-model work substitute for this.

### 5. Aggregate errors, sort them, and use `errors.AsType`.

Ship an aggregate that implements `Unwrap() []error` (so `errors.Is`/`As`/`AsType` traverse it) **and** `fmt.Formatter` (because `errors.Join`'s newline dump is not a presentation layer), with elements sorted by key.
Never produce the nested pairwise tree xload's concurrent path emits (5.4).
Use pointer receivers on `Error()` so only one form implements `error`.

**Cost.** Aggregating means continuing past a failed field, so the destination is partially populated on error.
Document that explicitly and offer a `StopOnFirstError` option.
`errors.AsType` costs a `go 1.26` directive.

### 6. Treat determinism as a package-wide invariant, not a per-site fix.

Every map iteration reaching a user-visible artifact gets `slices.Sorted(maps.Keys(m))`.
xload gets this wrong in the collision error today (5.5, reproduced: three orderings in 40 runs), and ferry's dump direction makes it a correctness property rather than a cosmetic one.

**This now conflicts directly with a json/v2 default and the conflict must be resolved deliberately.**
Measured on `go1.27rc2`: v2 marshals a Go map in a nondeterministic order by default (8 distinct orderings over 50 marshals of one 8-key map) where v1 was deterministic; `Deterministic(true)` restores it.
A ferry that adopts "v2 semantics" without qualification inherits nondeterministic dump output, which is the one thing this recommendation forbids.
Worth copying instead is how narrowly v2 words its promise: "the same input value will be serialized as the exact same output bytes.
Different processes of the same program will serialize equal values to the same bytes, but different versions of the same program are not guaranteed to produce the exact same sequence of bytes."

**Cost.** Sorting on paths that did not need it.
Negligible, and worth it for reproducible diffs of dumped config.

### 7. Design codec registration as typed functions with documented precedence, in both directions.

Copy `encoding/json/v2`'s `MarshalToFunc[T]` / `UnmarshalFromFunc[T]` / `JoinMarshalers` shape: typed at registration, resolved once, with a `SkipFunc`-style decline-and-fall-through and a **documented, overridable** precedence chain.
This is where generics genuinely pay (1c).
Never do what xload does at [load.go:301](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L301) and identify a type by comparing `Type.String()` to `"time.Duration"`.

Since 1.27 this shape is also importable rather than only copyable, and the precedence it documents was re-verified by execution: `WithMarshalers` funcs beat `MarshalerTo`, which beats `Marshaler`, which beats `encoding.TextAppender`, which beats `encoding.TextMarshaler`.
Note ferry's chain will differ from xload's in a way worth calling out in the ADR: xload puts its own `Decoder` first and `encoding.TextUnmarshaler` ahead of `json.Unmarshaler` (5.9), which is the reverse of v2's ordering of the text and JSON arms.

**Cost.** More API surface than "implement this one interface".
Also: json/v2 still cannot register by runtime `reflect.Type` ([#73457](https://go.dev/issue/73457) still open at 1.27), and ferry probably needs both static and dynamic registration, which is more surface again.
And if ferry recognises `MarshalerTo`/`UnmarshalerFrom`, it inherits an unenforceable prose obligation: v2's godoc only *asks* that a type implementing both `Marshaler` and `MarshalerTo` "aim to have equivalent behavior".
Back that with a conformance case rather than a sentence (recommendation 8b).

### 8. Reconsider the per-key pull interface. This may be the second-biggest win after caching.

xload issues one backend call per leaf with no memoisation, and `SerialLoader` multiplies it by the number of backends (5.13, reproduced: 6 calls for a 2-field struct over 3 backends).
Because ferry knows the entire key set from the compiled schema **before any I/O**, it can offer a batch/snapshot entry point that xload structurally could not.

**Cost.** Every backend implementor must be able to enumerate or accept a key list, which some (Vault, dynamic secret stores) will not want.
Recommend the per-key interface as the required one and the batch form as an optional interface upgrade, with in-load memoisation always on.
Keep the required interface small: koanf gets 20 providers off two methods, and that is the adoption bar.

### 8b. Ship a conformance suite for the backend contract, and fix the flat key space.

Any obligation the compiler cannot check needs a test that can.
slog's `Handler` is four methods carrying six unchecked prose rules, and what makes it survivable is `testing/slogtest`'s 17 explained conformance cases.
Separately, flat string keys are lossy on their own terms, before any value-typing question: measured, koanf's `Flatten` resolves a collision between `{"a.b":1}` and `{"a":{"b":2}}` nondeterministically (255/45 over 300 runs), and viper destroys two of three case-variant keys silently.
ferry inherits flat keys from xload.
**Decide the collision rule and the case rule explicitly, and never fold case.**

### 9. Support `encoding/json/v2` first class, and be precise about which parts.

**Rewritten.**
This recommendation previously read "do not build on `encoding/json/v2`, but do track it", on the evidence that nothing in it was importable.
Half of that evidence has expired and half has not, so the replacement is narrower rather than simply inverted.

**What changed:** in Go 1.27 (RC verified) `encoding/json/v2` and `encoding/json/jsontext` are ordinary importable stdlib packages, in `api/go1.27.txt`, buildable with no `GOEXPERIMENT`.
The blanket "nothing is importable" claim is withdrawn.

**What did not change, and still constrains ferry:**

- The struct field resolver (`makeStructFields`, `structFields`, `fieldOptions`, `foldName`, `lookupArshaler`) is still unexported.
  **ferry writes its own schema compiler regardless.** This is the reuse that would have mattered most and it is still unavailable.
  The nearest future relief is [#74819](https://go.dev/issue/74819) (`encoding/json/jsonstruct`), still **open** with no milestone.
- `Options` is still sealed with `internal.NotForPublicUse`, and [#77703](https://go.dev/issue/77703), which asked for an open aggregate `Options` type, is **closed**.
  ferry cannot add an option to json/v2's bag.
  It *can* read one, via the exported `GetOption` - that is new and worth knowing.
- json/v2 still cannot register a codec by runtime `reflect.Type` ([#73457](https://go.dev/issue/73457), still open).
  ferry needs both static and dynamic registration and gets no help here.

**What "first class" could mean concretely, and what each reading costs, is set out in section 3** ("What first-class `encoding/json/v2` could concretely mean").
The four readings are codec-chain recognition of `MarshalerTo`/`UnmarshalerFrom`; adopting v2's semantics; importing json/v2 or `jsontext` internally; and shipping a json/v2-backed sub-module.
They are not mutually exclusive and they are not all equally cheap.
**Do not treat "first class" as a single decision.**

**Three things the ADR must not get wrong**, all measured on `go1.27rc2`:

- **`omitzero`, not `omitempty`.** v2 defines `omitzero` in Go terms and `omitempty` in JSON terms. Only the first is portable to a Consul or env plane.
- **Override two v2 defaults rather than inherit them.** v2 marshals nil slices and maps as `[]` and `{}`, destroying the nil-versus-empty distinction that v1 preserved and that ferry's three-state presence rule exists to protect; and v2 map output is nondeterministic by default where v1's was deterministic, which contradicts recommendation 6.
- **`time.Duration` has no representation in v2** and errors at runtime. ferry must decide this itself and cannot claim to be following json/v2 either way.

**Cost.** Any module that imports `encoding/json/v2` or `encoding/json/jsontext` takes a `go 1.27` floor **and** stops building under `GOEXPERIMENT=nojsonv2` (verified).
The semantics-only reading costs neither.
The v2 tag grammar also churned right up to the RC - `inline` became `embed` ([#79985](https://go.dev/issue/79985)), `unknown` and `DiscardUnknownMembers` were removed ([#77271](https://go.dev/issue/77271)), `format:` was removed pending typed struct tags ([#79071](https://go.dev/issue/79071)) - so re-verify the shipping option set at GA before copying any of it.

### 10. Pick the `go` directive as a decision. First-class json/v2 forces `go 1.27`.

**Changed, and the trade-off is restated rather than silently flipped.**
This previously recommended `go 1.26`.
It now recommends **the first Go release where `encoding/json/v2` is GA, which is Go 1.27**, for any module that imports json/v2.

Since Go 1.21 the `go` line is a **strict minimum**, not a hint.
`go 1.27` in `go.mod` means a Go 1.26 toolchain cannot build the module at all; it will either download a newer toolchain or fail.
**This line decides who can import ferry**, and it is the one decision here with no technical escape.

What each floor buys:

| Directive | Buys |
| --- | --- |
| `go 1.23` | `iter`, `reflect.Value.Seq`, range-over-func |
| `go 1.26` | plus `errors.AsType`, `reflect.Value.Fields`, `reflect.TypeAssert` |
| `go 1.27` | plus generic methods, generalized type inference, an unflagged `encoding/json/v2` and `jsontext`, `stdversion` under `go test` |

**Why the `go 1.26` plus `GOEXPERIMENT=jsonv2` route is closed** (verified on go1.26.5, and recorded so the option is not revisited):

- A `go 1.26` module with `GOEXPERIMENT=jsonv2` set can import `encoding/json/v2`; without the flag it fails with `build constraints exclude all Go files in $GOROOT/src/encoding/json/v2`.
- **The requirement is transitive.** A module importing a library that imports json/v2 fails unless the *consuming* build sets the flag. A library cannot satisfy it for its consumers.
- **`go.mod` cannot declare it.** A `goexperiment jsonv2` line is rejected with `unknown directive`; there is a `godebug` directive but no `goexperiment` counterpart.

So the `go 1.26` route would make every ferry user set an environment variable to build. That is hostile and it is not on the table.
The other rejected variant, a `//go:build goexperiment.jsonv2` dual path inside ferry, is rejected for a different reason: ferry's round-trip guarantee would have to hold identically on both paths, doubling the property-test matrix for a transitional benefit.

**The trade-off, stated plainly.**
`go 1.27` excludes everyone still on 1.26 and earlier, at a moment when 1.27 is not yet GA.
That is a larger exclusion than `go 1.26` was, and it is deliberate: ferry is in design with no code and will not ship before 1.27 is out, so the exclusion is of users who will have upgraded by the time ferry exists.
If that timing assumption breaks - if ferry ships sooner, or if a large consumer is pinned to 1.26 - the fallback is **recommendation 9's option (d)**: keep core at `go 1.26` with no json/v2 import, and put the v2 dependency in a sub-module at `go 1.27`.
That preserves the choice at the cost of one more module to release.

Independent of the json/v2 question:
use `Value.Fields()` in the **schema compiler only** - it allocates per iteration and is not a hot-path win (aclements on [#66631](https://go.dev/issue/66631)).
Avoid `Type.Methods()` entirely: it forces the linker to retain all exported methods in all packages.

**Cost.** Excludes users on older toolchains, and the exclusion is now one release deeper than before.
Record it as an ADR with the sub-module fallback named, so the decision is reversible without redesigning the core.

### 11. Decide the watch API's error convention explicitly, and expect to defend it.

There is **no stdlib blessing for `iter.Seq2[T, error]`** - the recommendation was deliberately removed from the accepted iterator proposal before it landed, and [#71901](https://go.dev/issue/71901) is still open because "people don't yet agree" (section 3).
If ferry ships `Seq2[Event, error]`, answer adonovan's four questions in the doc comment.
Seriously consider jba's `(iter.Seq[Event], func() error)` instead: the compiler warns on an unused error function, and a silently dropped watch error is a production incident.

**Cost.** `Seq2` is what the ecosystem expects and reads better.
The alternative is safer and uglier.
This is a genuine trade, not a clear win either way.

### 12. Ship your own tag validation. Nothing else will do it.

`reflect.StructTag` has had exactly one change in Go's history (`Lookup`, Go 1.7), typed struct tags ([#74472](https://go.dev/issue/74472)) are on hold, and `go vet`'s `structtag` analyzer **is not run by `go test` by default** and only knows about `json`, `xml`, and `asn1` anyway (section 3).
A misspelled `ferry:"KEY,requird"` will reach production unless ferry catches it.
Provide a validation entry point users can call in a test - `reflect.TypeFor[T]()` makes this possible with no value - and copy json/v2's near-miss rejection ("has invalid appearance of `%s` tag option; specify `%s` instead").

**Cost.** More parser code and more error-message surface.
It is the difference between silent misconfiguration and a clear failure.

### 13. Things to explicitly not do

- Do not wait for a generic `sync.Map`. [#47657](https://go.dev/issue/47657) is dead; [#71076](https://go.dev/issue/71076) (`sync/v2`) is open with no milestone.
- Do not wait for typed struct tags. On hold, and a language change.
- Do not use `reflect.TypeAssert` as a performance measure. Measured neutral to 62% worse for interface targets (1e).
- Do not use `unsafe`. json/v2 declares avoiding it an explicit goal; `goccy/go-json` shows the cost, having shipped a `//go:build race` variant that silenced the race detector rather than fixing the race, for years.
- Do not carry over `structs.HostLayout` curiosity. It is a cgo ABI marker and has nothing to do with any of this.
