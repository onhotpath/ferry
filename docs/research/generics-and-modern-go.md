# Generics and modern Go: what ferry's API can do that xload's could not

Research ticket: xload was designed before Go had generics.
What can ferry's API do that xload could not?

All measurements below were taken on this machine (Apple M1 Pro, `go1.26.5 darwin/arm64`) against `github.com/gojekfarm/xtools/xload@latest`.
Reproduction code for each is described inline.

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
  `encoding/json/v2` is in Go 1.26 behind `GOEXPERIMENT=jsonv2` and is a **Go 1.27 release blocker**, so it goes GA within about a month of this writing.
  Nothing in it is importable by a third-party mapper; only its design is reusable.
- **Typed values at the plane boundary are the single highest-leverage departure from xload.**
  Reproduced: a YAML list flattens to the empty string with no error today.

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
xload's entry point is `Load(ctx context.Context, v any, opts ...Option) error` ([load.go:37](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L37)), which immediately does two runtime kind checks and can return `ErrNotPointer` ([load.go:74-76](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L74-L76)) or `ErrNotStruct` ([load.go:79-81](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L79-L81)).
A signature of `Load[T any](ctx context.Context, dst *T, opts ...Option) error` makes `ErrNotPointer` structurally impossible: one of xload's three package-level sentinels ([load.go:15-22](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L15-L22)) stops existing.
`ErrNotStruct` survives, per the constraint limitation above.
This is a small win but it is real and free.

**(b) `reflect.TypeFor[T]()` removes the value-to-type detour and enables value-free compilation.**
xload can only reach the type through a value: `reflect.ValueOf(obj)` then `.Elem().Type()` ([load.go:72-83](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L72-L83)).
`reflect.TypeFor[T]()` ([reflect/type.go:1388](https://cs.opensource.google/go/go/+/refs/tags/go1.26.0:src/reflect/type.go) in go1.26.5, added in Go 1.22) gives the `reflect.Type` from the type parameter with no value at all.
That matters for more than tidiness: it means a schema can be compiled, validated, and cached at construction time rather than on first load, so a `ferry.New[Config](...)` constructor can return tag-grammar errors *before* any I/O happens.
xload cannot do this - a malformed tag such as an unknown option is only discovered mid-walk, on the first `Load`, after some fields have already been set ([load.go:100-103](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L100-L103) calling [parseField](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L218)).

`reflect.Type` values are comparable and canonical (identical types yield identical `Type` values), verified locally: `reflect.TypeFor[S]() == reflect.TypeOf(S{})` is `true`.
That is what makes them safe map keys for a schema cache, and it means there is no invalidation problem - a `reflect.Type` never changes meaning.

**(c) Typed decoder/encoder registration moves the `any` to registration time.**
xload's extension point is `Decoder interface { Decode(string) error }` ([load.go:403-406](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L403-L406)), discovered by a five-arm type switch on `field.Interface()` executed **per field, per call** ([load.go:414-439](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L414-L439)), plus a second near-identical switch in `hasDecoder` on the async path ([async.go:222-235](https://github.com/gojekfarm/xtools/blob/main/xload/async.go#L222-L235)).
The precedence order (`Decoder` > `encoding.TextUnmarshaler` > `json.Unmarshaler` > `encoding.BinaryUnmarshaler` > `gob.GobDecoder`) is hardcoded and not configurable.
There is no way at all to register a decoder for a type you do not own: `time.Duration` is handled by a **string comparison on the type name**, `if ty.String() == "time.Duration"` ([load.go:301](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L301)).

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
This costs ferry a `go 1.26` directive and nothing else.

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

This maps directly onto xload's worst hot-path cost: the five-arm interface type switch in `decode` run per field per call ([load.go:424-435](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L424-L435)).
Resolved once at compile time it becomes a stored function pointer and a nil check.

Adjacent precomputation tricks worth stealing from `json/v2`'s `fields.go`:

- **Precompute the wire form.** `fieldOptions.quotedName` is stored pre-quoted and pre-escaped so the marshal hot path is `b = append(b, f.quotedName...)`, skipping the encoder state machine (arshal_default.go:1148-1149). ferry's dump path should store the fully-resolved key string per leaf, not recompute `prefix + key`.
- **Precompute lookup indices.** `structFields` holds `flattened []structField`, `byActualName map[string]*structField`, and a pre-folded `byFoldedName` for case-insensitive lookup (fields.go:78-349).
- **Index splitting.** `reindex()` (fields.go:38-57) splits `index []int` into `index0 int` plus a remainder slice "to avoid bounds check during runtime", and nils the remainder when empty "to avoid pinning the backing slice".
- **`omitzero` checked before the marshaler runs** (arshal_default.go:1097-1100), whereas `omitempty` may require marshaling then unwriting (1115-1121). A real asymmetry worth copying if ferry adopts both.

### What is *not* reusable

**`encoding/json/v2` is a closed system. A third-party mapper gets zero reuse of its field-resolution machinery.**

- `structFields`, `structField`, `fieldOptions`, `makeStructFields`, `parseFieldOptions`, `foldName`, `lookupArshaler` are all unexported.
- `jsonopts` and `jsonflags` live under `encoding/json/internal/` and are compiler-blocked, not merely lowercase.
  `json.Options` is an alias for `jsonopts.Options`, whose only method is `JSONOptions(internal.NotForPublicUse)` - a deliberate sealed-interface guard.
  You cannot implement `Options` outside the json packages.
- `jsontext` exports only syntax-layer machinery (`Encoder`, `Decoder`, `Token`, `Value`, `Pointer`) - nothing about Go struct fields or tags.

The one extension point that *is* offered is `MarshalToFunc[T]` / `UnmarshalFromFunc[T]` plus `JoinMarshalers` / `WithMarshalers` ([arshal_funcs.go:175+](https://cs.opensource.google/go/go/+/refs/tags/go1.26.0:src/encoding/json/v2/arshal_funcs.go)), backed by its own `sync.Map` cache that also caches *negative* lookups (an explicit `nil` for "no funcs apply", arshal_funcs.go:146).
That is per-Go-type behaviour override riding json/v2's cache.
It is useful as a **design template for ferry's typed decoder registration** (section 1c), and useless as an actual dependency for a non-JSON mapper.

The code is BSD-licensed, so copying the design is fine; there is just nothing to import.

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

Note that range-over-func was a `GOEXPERIMENT=rangefunc` preview in Go 1.22 ("Building with `GOEXPERIMENT=rangefunc` enables this feature", [go1.22 release notes](https://go.dev/doc/go1.22)) and became a language feature in **Go 1.23**, at the same time as package `iter`.

**Not yet shipped, but directly relevant: generic methods in Go 1.27.**
The [draft Go 1.27 release notes](https://go.dev/doc/go1.27) state that "a method declaration may declare its own type parameters", with the restriction that interface methods may not.
This is the single biggest future lever on ferry's API shape, because it is the difference between

```go
ferry.Into[Config](d, ...)          // today: package-level generic function
d.Into[Config](...)                 // Go 1.27: generic method
```

Go 1.27 is not GA at time of writing (the user confirms it is roughly a month out).
**Do not depend on it, but do not design a package-level-function-only API that cannot later grow method forms.**

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
It raises ferry's minimum Go version to 1.26.
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

**In the stdlib, behind `GOEXPERIMENT=jsonv2`, off by default in Go 1.26, and generally available in Go 1.27 - roughly a month away at time of writing.**

The trajectory, verified per release:

| Release | Status |
| --- | --- |
| **1.25** | Experimental, **opt-in** via `GOEXPERIMENT=jsonv2`. [go1.25 release notes](https://go.dev/doc/go1.25#json_v2), authorising proposal [#71845](https://go.dev/issue/71845). |
| **1.26** | No change, and no mention in the release notes. Confirmed locally: still gated. |
| **1.27** (not GA) | **Generally available, opt-OUT.** The [draft go1.27 notes](https://go.dev/doc/go1.27) say `encoding/json` "is now backed by the v2 implementation", with `GOEXPERIMENT=nojsonv2` to restore v1, and that opt-out "is expected to be removed in a future release." |

That last row matters for ferry's positioning: within a month, every Go program's `encoding/json` behaviour changes, and the semantics ferry chooses for `omitempty`/`omitzero`, duplicate keys, and case sensitivity should be chosen against **v2**, not v1.

Verified for Go 1.26:

- `$GOROOT/src/encoding/json/v2/` exists in go1.26.5, alongside `encoding/json/jsontext` and `encoding/json/internal/{jsonflags,jsonopts,jsonwire,jsontest}`.
- Every file in `v2/` carries `//go:build goexperiment.jsonv2`.
  The flag is declared at `$GOROOT/src/internal/goexperiment/flags.go:106-107`.
- `go env GOEXPERIMENT` is empty by default, and the gate was confirmed empirically: importing `encoding/json/v2` fails with "build constraints exclude all Go files" unless `GOEXPERIMENT=jsonv2` is set.
- `grep -c "encoding/json/v2" $GOROOT/api/go1.26.txt` returns **0**.
  It is not yet under the Go 1 compatibility promise.
- Proposal [go.dev/issue/71497](https://go.dev/issue/71497) "encoding/json/v2: new API for encoding/json" is **closed as completed**, labelled `Proposal-Accepted` and `release-blocker`, milestone **Go 1.27**.
- It first shipped as an experiment in Go 1.25 ([go.dev/blog/jsonv2-exp](https://go.dev/blog/jsonv2-exp)).
- When the experiment is on, v1 is reimplemented on top of v2 (`$GOROOT/src/encoding/json/v2_decode.go:99` delegates to `jsonv2.Unmarshal` with `DefaultOptionsV1()`).
- `github.com/go-json-experiment/json` is now just an upstream mirror; its README directs changes to the Go project.

**Nothing in it is reusable by a third-party mapper.**
See section 2 for the full exported-surface audit and the `internal.NotForPublicUse` sealing.
The relevant takeaways for ferry are design patterns, not imports:

- `omitzero` semantics (`v2/doc.go:79-83`): omit if the value is zero "as determined by the `IsZero() bool` method if present, otherwise based on whether the field is the zero Go value."
  Contrast `omitempty`, which omits if the field would encode as null, empty string, empty object or empty array.
  Explicitly (`doc.go:143-149`): "only a nil slice or map is omitted under `omitzero`, while an empty slice or map is omitted under `omitempty` regardless of nilness."
  `omitzero` landed in **`encoding/json` v1 in Go 1.24** ([#45669](https://go.dev/issue/45669)).
  Crucially, v2 redefines `omitempty` in **JSON** terms while `omitzero` stays defined in **Go** terms, and the v1 migration doc says: "Existing usages of `omitempty` on a Go bool, number, pointer, or interface value should migrate to specifying `omitzero` instead (which is identically supported in both v1 and v2)."
  **If ferry has an omit-on-empty option, model it on `omitzero`, not `omitempty`** - it is the one with stable, backend-independent, Go-level semantics, which is exactly what a backend-agnostic mapper needs.
- `Marshalers` / `Unmarshalers` (`json.MarshalToFunc[T]`, `UnmarshalFromFunc[T]`, `JoinMarshalers`, `WithMarshalers`) is the template for ferry's typed codec registration (section 1c).
  Two details worth copying: it caches negative lookups, and `SkipFunc` lets a registered function decline and fall through to the next one and then to the default.
  The full precedence chain is `WithMarshalers` funcs -> `MarshalerTo` -> `Marshaler` -> `encoding.TextAppender` -> `encoding.TextMarshaler` -> reflection, and it is **documented**, unlike xload's (section 5.9).
  Known gap: [#73457](https://go.dev/issue/73457) (register by runtime `reflect.Type` rather than static type parameter) is still open, so json/v2 cannot do dynamic registration. ferry probably needs both.
- `omitzero` is evaluated *before* the marshaler runs; `omitempty` may require marshalling then unwriting.
  If ferry supports both, copy that asymmetry.
- The v2 options model is worth studying for ferry's own option plumbing: one `Options` type shared across both packages and both directions, later options override earlier, variadic `...Options` on every entry point, and - the thing v1 structurally could not do - options are **threaded down the call stack and readable inside user methods** via `Encoder.Options()` / `Decoder.Options()`. xload's `options` struct is package-private and invisible to a user's `Decode` method ([options.go:37-42](https://github.com/gojekfarm/xtools/blob/main/xload/options.go#L37-L42)); ferry should decide deliberately whether a user codec can see the load options.

Design decisions from the [json/v2 discussion document](https://github.com/golang/go/discussions/63397) that transfer directly:

- The reason v2 exists at all is that `MarshalJSON() ([]byte, error)` **forces an allocation and a re-parse**, and `UnmarshalJSON([]byte)` forces a pre-scan then a second parse.
  These are **API-bound, not implementation-bound** - they cannot be fixed without a new interface.
  xload's `Decoder interface { Decode(string) error }` has the identical defect: it forces the plane value to be materialised as a `string` before the decoder sees it.
  That is the single strongest argument for section 4.
- The syntactic/semantic split (`jsontext` vs `json`) exists so the syntax layer has **no `reflect` dependency**.
  ferry has the same latent split: a plane/codec layer that need not import `reflect`, and a struct-mapping layer that must.
- Stated compatibility target: "95% to 99% backwards compatibility. We do not aim for 100%." Explicit non-goal: `unsafe`.

### Other things worth knowing, and things that are not

- **The `go` directive is now a strict minimum (Go 1.21).** `go 1.26` in `go.mod` means the module cannot be built by Go 1.25, and the go command will download a newer toolchain automatically. GODEBUG-controlled behaviour changes key off it too. **This is a real API decision for ferry, not a formality**: `go 1.23` gets `iter` and `Value.Seq`; `go 1.26` additionally gets `errors.AsType` and `Value.Fields`. Pick deliberately and record it as an ADR.
- **`testing/synctest`** - experimental in 1.24 (`synctest.Run`), stable in 1.25 with a **renamed API** (`synctest.Test`). If ferry's watch API is concurrent or time-dependent, this is the right test harness. Also the reason `encoding/json` moved off `sync.WaitGroup` (section 2).
- **Generic type aliases (1.24)** - `type Foo[T any] = Bar[T]` now works, previewed in 1.23 under `GOEXPERIMENT=aliastypeparams`. Useful for exposing `type Result[T any] = internal.Result[T]` shims; marginal otherwise.
- **Type inference generalisation (1.21, extended in 1.27 draft)** - what makes `slices.SortFunc(x, myGenericCmp)` work without explicit instantiation. Relevant to how ergonomic ferry's typed codec registration can be without annotations.
- **`unique` (1.23)** - interning. Potentially relevant if ferry interns key strings across many loads, but the win is small next to schema caching. Its internal design is cited in section 2 for the "generics for allocation avoidance, not type safety" argument.
- **`maphash.Comparable` / `WriteComparable` (1.24)** - "make it possible to hash anything that can be used as a Go map key". `reflect.Type` is comparable, so this is a legitimate way to shard a schema cache. Almost certainly premature.
- **`sync.Map` reimplemented on `HashTrieMap` (1.24, [#70683](https://go.dev/issue/70683))** - "modifications of disjoint sets of keys... much less likely to contend". Background to section 2's read-cost comparison.
- **`go vet` gains `stdversion` in `go test` by default (1.27 draft)** - will catch accidental use of, say, `Value.Fields` under a `go 1.23` directive. Useful safety net for the previous bullet.

Explicitly **not** relevant despite being recent: `structs.HostLayout`, `weak` and `runtime.AddCleanup` (a schema cache keyed by `reflect.Type` should not be weak, since those values are runtime-immortal anyway), `os.Root`, the `crypto/*` churn, `go/types` iterators.

## 4. Typed values at the plane boundary

The question is how to model a dynamically-typed value that must round-trip losslessly, and what each model costs the person writing a new ferry backend.

xload's answer is "everything is a `string`, `spf13/cast` fixes it up", and section 5.8 shows what that costs: a YAML list silently becomes the empty string, and `null` is indistinguishable from `""`.
It is worth naming the deeper reason, because it is the same one that forced `encoding/json/v2` to exist.
The [json/v2 discussion](https://github.com/golang/go/discussions/63397) explains that v1's `MarshalJSON() ([]byte, error)` and `UnmarshalJSON([]byte)` are **API-bound, not implementation-bound** defects: forcing the value through a byte slice mandates an allocation and a re-parse that no amount of optimisation can remove.
`Loader.Load(ctx, key) (string, error)` has exactly this shape.
A YAML backend that already parsed `8080` into an `int` must render it back to `"8080"` so that `cast.ToInt64E` can parse it again.

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

#### (e) Bare `map[string]any` - `koanf`, `viper`

The status quo for Go config libraries, and the thing ferry is presumably trying to beat.
`koanf` carries `map[string]interface{}` throughout and delegates struct mapping to `mapstructure` (section 2), which caches nothing.

The classic failure is numeric: `encoding/json` unmarshals every number to `float64`, YAML gives `int`, TOML gives `int64`, so the same logical config produces different Go types depending on which backend loaded it, and every consumer needs `cast`-style coercion.
That is xload's problem with extra steps.

- **Lossless-ness.** Poor and, worse, **backend-dependent**. Two backends round-trip the same document to different Go types.
- **Cost to a new backend.** Lowest possible.
- **Verdict.** This is the anti-pattern. Its only virtue is the one ferry must preserve some other way: writing a backend is trivial.

#### (f) Deferred decoding - `json.RawMessage`

Worth naming as an orthogonal technique rather than a competing model: keep the value **opaque bytes** until someone who knows the target type asks for it.
`json.RawMessage` (and `jsontext.Value` in v2) does this.

For ferry this is the right representation for exactly one case: a backend that holds an encoded blob it cannot cheaply interpret (a JSON string in a Consul key, a secret payload).
It should be one arm of the union, not the whole design, because it reintroduces the parse-twice cost everywhere else.

### Comparison

| Model | Shape | Type set | Lossless | Cost to new backend | Nesting |
| --- | --- | --- | --- | --- | --- |
| `driver.Value` | bare `any`, documented set | closed(ish), 6 types | poor by design | very low | none |
| `slog.Value` | closed struct union + `Any` arm | closed + escape | good, 3-type carve-out | moderate | `KindGroup` |
| `cty` | full type system | closed + capsules | excellent, plus unknowns | high, and panics on misuse | native |
| `protoreflect.Value` | 24B unsafe union | closed, 11 types | only with its descriptor | high, panics on mismatch | via composite kinds |
| `starlark.Value` | interface + optional interfaces | fully open | good | low to add, high to consume | via interfaces |
| `map[string]any` | bare `any` | open, unconstrained | poor and backend-dependent | lowest | native but untyped |
| `json.RawMessage` | opaque bytes | n/a | perfect, deferred | low | deferred |

### Recommendation

**A closed struct-based tagged union in the `slog.Value` shape, with an explicit escape arm, an explicit nested/group arm, and an explicit raw/deferred arm.**

Reasons, in order:

1. It is the only model that fixes the reproduced YAML-list bug (5.8) without imposing `cty`-scale cost on backend authors.
2. `KindGroup` shows that nesting must be a kind, not a convention. xload's flattening is where the information is lost.
3. `KindAny` shows that a closed set does not have to be a straitjacket. Close the set for the types ferry optimises, leave one honest arm for everything else, and name it (capsule-style) rather than leaving it a bare `any`.
4. `sql.Null[T]` (Go 1.22) is the stdlib's own post-generics answer to "absent is not a value", and it validates ferry separating presence from content rather than encoding absence as a magic value.

**The costs, stated plainly:**

- ferry owns the value taxonomy forever. Adding a kind later is an API change.
- A struct union costs more to construct than passing a `string`, and writing a backend gets meaningfully harder than xload's one-method interface. That is a real adoption cost and the main argument against.
- If ferry copies slog's unsafe-ish packing it inherits slog's contract leak (three Go types unstorable) and non-comparability. **Recommend not copying the packing**: use an explicit `Kind` field. ferry's hot path is dominated by the reflection walk and backend I/O, not by value construction, so the packing buys little here that schema caching does not buy more of.
- Deferring to `driver.Value`'s "just document the allowed set" would be cheaper for backend authors, but it is precisely what produced the pgx situation, and ferry has no equivalent of a driver ecosystem large enough to route around it.

Two further rules fall out of the survey, and both are about **not** copying the fast models:

- **ferry's `Value` must be self-describing.** `protoreflect.Value` is only interpretable alongside its `FieldDescriptor`. ferry hands values to third-party backends that do not have the schema, so this is disqualifying.
- **Accessors must not panic.** `cty` and `protoreflect` both panic on type mismatch and both document it as intentional, because their callers are compilers and runtimes that type-check first. ferry's callers are backend authors. Return `(T, bool)` or an error.

**Verification status.** `driver.Value`, `sql.Null[T]`, and `slog.Value` were read directly from `$(go env GOROOT)/src` on go1.26.5 and are quoted above.
`cty` (`value.go`, `unknown.go`, `capsule.go`) and `protoreflect` (`value_union.go`, `value_unsafe.go`) were cloned and read in this session; the quoted declarations and doc comments are verbatim.
The `starlark.Value` and `koanf`/`viper` characterisations are **not** freshly audited here - they rest on those projects' documented designs and prior reading, and the koanf caching claims in section 2 (which were audited) are consistent with them.
If the `starlark` optional-interface pattern ends up load-bearing in an ADR, re-read `starlark/value.go` first.

## 5. Concrete rewrite candidates in xload's design

Ordered by how much they would change ferry's shape, not by severity.
Every item cites the xload file and line, and the ones marked **reproduced** were verified by running code against `xload@latest` on go1.26.5.

### 5.1 The `Loader` signature cannot express absence

[loader.go:9-11](https://github.com/gojekfarm/xtools/blob/main/xload/loader.go#L9-L11) is `Load(ctx context.Context, key string) (string, error)`.
`OSLoader` collapses a missing variable to the empty string ([loader.go:27-36](https://github.com/gojekfarm/xtools/blob/main/xload/loader.go#L27-L36)), and so does `MapLoader` ([maps.go:16-23](https://github.com/gojekfarm/xtools/blob/main/xload/maps.go#L16-L23)).
The consequences propagate everywhere: `required` is implemented as `val == "" && meta.required` ([load.go:147](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L147), [load.go:195](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L195), [async.go:180](https://github.com/gojekfarm/xtools/blob/main/xload/async.go#L180)), `setVal` silently no-ops on empty ([load.go:267-269](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L267-L269)), and `decode` refuses to hand an empty string to a decoder at all ([load.go:415-417](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L415-L417)).
So `FOO=""` cannot satisfy a `required` field, and a custom decoder can never be asked to interpret the empty string.
The `cached` provider had to invent a *different* interface, `Get(key string) (*string, error)`, precisely to express the third state, and its doc comment says so outright ([providers/cached/cache.go:8-18](https://github.com/gojekfarm/xtools/blob/main/xload/providers/cached/cache.go#L8-L18)).

**Trade-off for ferry.**
Any signature that can express absence costs the backend implementor something.
`(Value, bool, error)` is the cheapest and most Go-idiomatic (comma-ok), allocates nothing, and is trivially implementable.
`(*Value, error)` allocates and invites nil-deref bugs.
A sentinel `ErrNotFound` forces every backend to get error wrapping right and every caller to `errors.Is`.
Recommend comma-ok.

### 5.2 Two walks that have already diverged - **reproduced**

[load.go:71-207](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L71-L207) (`doProcess`) and [async.go:59-161](https://github.com/gojekfarm/xtools/blob/main/xload/async.go#L59-L161) (`processAsync`) are the same walk written twice.
They are not equivalent.
The sync path enters the struct-with-a-key branch on `meta.key != ""` ([load.go:141](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L141)); the async path additionally requires `hasDecoder(fVal)` ([async.go:122](https://github.com/gojekfarm/xtools/blob/main/xload/async.go#L122)).
The sync path also has a `val == "" && isNilStructPtr` escape ([load.go:151-153](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L151-L153)) that the async path lacks entirely.
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
[load_test.go:755-777](https://github.com/gojekfarm/xtools/blob/main/xload/load_test.go#L755-L777) runs every table case twice, once serially and once with `Concurrency(5)`, asserting the same `tc.want`.
But `input` is `any` ([load_test.go:18](https://github.com/gojekfarm/xtools/blob/main/xload/load_test.go#L18)) holding a **pointer** to a struct built once in the table literal, and the same pointer is passed to both subtests.
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

Both walks call `reflect.Type.Field(i)`, `Tag.Get`, and `parseField` for every field on every call ([load.go:85-103](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L85-L103), [async.go:77-95](https://github.com/gojekfarm/xtools/blob/main/xload/async.go#L77-L95)).
`parseField` does `strings.Split(tag, ",")` per field per call ([load.go:219](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L219)).
`decode` runs a five-arm interface type switch per field per call ([load.go:424-435](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L424-L435)).
`PrefixLoader` allocates a fresh closure per nested struct per call ([loader.go:20-24](https://github.com/gojekfarm/xtools/blob/main/xload/loader.go#L20-L24), called at [load.go:172](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L172)).

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

Also note that collision detection alone costs 21% of the runtime and 6 allocations ([load.go:52-61](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L52-L61) wraps the loader in a counting closure), and it is *on by default* ([options.go:49](https://github.com/gojekfarm/xtools/blob/main/xload/options.go#L49)).
With a compiled schema, key collisions are a property of the schema and can be detected **once at compile time, for free**, rather than by instrumenting every load.

**Trade-off for ferry.**
A `reflect.Type`-keyed cache is unbounded.
For statically declared types that is fine - the set is finite and small.
A program that manufactures types at runtime via `reflect.StructOf` would leak; that is an acceptable documented limitation, and it is the same limitation `encoding/json` has carried for a decade.

### 5.4 First error only, no aggregation - **reproduced**

Every error path in both walks is `return err` on the first failure.
There is no `errors.Join` anywhere in the package, and no error type implements `Unwrap() []error` ([errors.go](https://github.com/gojekfarm/xtools/blob/main/xload/errors.go) in full).
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

`collisionMap.err` ranges over a Go map ([collision.go:44](https://github.com/gojekfarm/xtools/blob/main/xload/collision.go#L44)) and `collisionSyncMap.err` ranges over a `sync.Map` ([collision.go:22](https://github.com/gojekfarm/xtools/blob/main/xload/collision.go#L22)); neither sorts.
40 runs of an identical input produced three different messages:

```
 29 x xload: key collisions detected for keys: [K J I]
  9 x xload: key collisions detected for keys: [J I K]
  2 x xload: key collisions detected for keys: [I K J]
```

`ErrCollision.Keys()` ([errors.go:74-79](https://github.com/gojekfarm/xtools/blob/main/xload/errors.go#L74-L79)) copies the slice but does not sort it either.
Fix is one `slices.Sort` call.
Since ferry needs deterministic **dump** output as a first-class property, treat determinism as a package-wide invariant rather than a per-site fix: every map iteration that reaches a user-visible artifact gets sorted.

### 5.6 Lost-update race in the concurrent collision counter

[collision.go:9-16](https://github.com/gojekfarm/xtools/blob/main/xload/collision.go#L9-L16):

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

[load.go:107-117](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L107-L117) and its async twin [async.go:163-171](https://github.com/gojekfarm/xtools/blob/main/xload/async.go#L163-L171) allocate a fresh zero value with `reflect.New(...).Interface()` and `reflect.DeepEqual` it against the populated struct to decide whether to write back a nil struct pointer.
This is expensive (an allocation plus a recursive deep comparison per nil-struct-pointer field per load) and semantically wrong: a nested struct that was legitimately loaded to all-zero values is indistinguishable from one that was never touched, so the pointer is left nil.
Threading a `bool` "any leaf was set" through the walk is cheaper and correct.

### 5.8 Type information destroyed at the boundary - **reproduced**

[maps.go:33-47](https://github.com/gojekfarm/xtools/blob/main/xload/maps.go#L33-L47) flattens `map[string]any` to `map[string]string` with `cast.ToString(value)` in the default arm.
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
The YAML provider is built directly on this ([providers/yaml/yaml.go:36-50](https://github.com/gojekfarm/xtools/blob/main/xload/providers/yaml/yaml.go#L36-L50)), so `servers: [a, b]` in a config file loads as nothing with no error.
This is the single strongest argument for typed values at the plane boundary (section 4): the YAML backend already knew it had a sequence and was forced to throw that away.

### 5.9 The decoder chain is fixed, one-directional, and context-free

[load.go:403-439](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L403-L439).
Problems, in order of importance to ferry:

- No `Encode` counterpart at all. ferry is bidirectional; this interface has to be designed as a pair from day one.
  Note that `xloadtype` already grew accidental encoders - `URL.String()`, `Listener.String()`, `Endpoint.String()` ([type/url.go:12](https://github.com/gojekfarm/xtools/blob/main/xload/type/url.go#L12), [type/listener.go:14](https://github.com/gojekfarm/xtools/blob/main/xload/type/listener.go#L14), [type/endpoint.go:15](https://github.com/gojekfarm/xtools/blob/main/xload/type/endpoint.go#L15)) - unspecified, untested as a round trip, and not used by the library.
- Precedence is hardcoded and undocumented as a policy: `Decoder` > `TextUnmarshaler` > `json.Unmarshaler` > `BinaryUnmarshaler` > `GobDecoder`. A type implementing both `json.Unmarshaler` and `BinaryUnmarshaler` gets JSON, arbitrarily.
- No way to register a decoder for a type you do not own. `time.Duration` is special-cased by **type name string comparison**, `ty.String() == "time.Duration"` ([load.go:301](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L301)), which also silently misfires for any user type named `Duration` in a package named `time`.
- `Decode(string) error` takes no `context.Context` even though the whole walk is context-carrying.
- Decoders never see an empty input ([load.go:415-417](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L415-L417)).

**Trade-off for ferry.**
Typed registration (section 1c) plus a documented, overridable precedence list costs more API surface than "implement this one interface", but it is the difference between a library you can extend and one you have to fork.

### 5.10 Composite values are string-splitting, and it is not escapable

`setVal` handles maps by `strings.Split(val, meta.delimiter)` then `strings.Split(v, meta.separator)` ([load.go:343-372](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L343-L372)) and slices by a single `strings.Split` ([load.go:374-394](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L374-L394)).
There is no escaping, so a value containing the delimiter is unrepresentable.
Nested maps, slices of structs, and arrays are all unsupported and fall to `ErrUnknownFieldType` ([load.go:396-397](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L396-L397)).
The tag grammar has the same problem: `parseField` splits the tag on `,` ([load.go:219](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L219)), so `env:"K,delimiter=,"` cannot be written.

If ferry's boundary carries typed values, a backend that natively has a list (YAML, JSON, Consul-with-JSON) hands over a list and none of this arises.
String splitting stays as the fallback for genuinely flat planes such as environment variables.

### 5.11 The YAML provider silently discards parse errors - **reproduced**

[providers/yaml/yaml.go:18-29](https://github.com/gojekfarm/xtools/blob/main/xload/providers/yaml/yaml.go#L18-L29):

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

[loader.go:40-57](https://github.com/gojekfarm/xtools/blob/main/xload/loader.go#L40-L57) is "last non-empty value wins".
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
Wrap three backends in a `SerialLoader` and a 2-field struct produces **6** backend calls, because `SerialLoader` queries every loader for every key with no short-circuit ([loader.go:44-52](https://github.com/gojekfarm/xtools/blob/main/xload/loader.go#L44-L52)).
For an in-process map that is irrelevant.
For Consul, Vault, or an HTTP config service it is `fields x backends` network round trips per load.

The `cached` provider exists precisely to paper over this ([providers/cached/loader.go:20-42](https://github.com/gojekfarm/xtools/blob/main/xload/providers/cached/loader.go#L20-L42)), but it caches with a **TTL across loads**, which is a different and weaker thing than deduplicating within one load: it trades correctness (stale reads) for a problem that is really a shape problem.

**Trade-off for ferry.**
Three options, and this is a genuine fork in the design:

1. Keep the per-key pull interface and memoise within a single load. Cheapest, keeps backends trivial, still N round trips for N distinct keys.
2. Give the source a batch entry point (`LoadAll` / snapshot) and let the walk read from the snapshot. One round trip, but every backend implementor must be able to enumerate, which a Vault or a secret store may not want to do.
3. Both, with the batch form optional via an interface upgrade (`if s, ok := src.(Snapshotter); ok`). Most flexible, most surface.

Because ferry knows the full key set from the compiled schema **before** doing any I/O (section 5.3), option 2 becomes viable in a way it never was for xload: ferry can hand the backend the exact list of keys it wants.
That is a capability xload structurally cannot have, and it is arguably a bigger win than anything generics contribute.

### 5.14 Minor, but worth not carrying over

- Two ways to set the loader: `WithLoader` ([options.go:20-22](https://github.com/gojekfarm/xtools/blob/main/xload/options.go#L20-L22)) and `LoaderFunc.apply` / `MapLoader.apply` ([loader.go:59](https://github.com/gojekfarm/xtools/blob/main/xload/loader.go#L59), [maps.go:31](https://github.com/gojekfarm/xtools/blob/main/xload/maps.go#L31)) make some loaders directly usable as options and others not.
- `for fVal.CanAddr() { fVal = fVal.Addr() }` ([load.go:135-137](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L135-L137), [load.go:419-421](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L419-L421), [async.go:223-225](https://github.com/gojekfarm/xtools/blob/main/xload/async.go#L223-L225)) is written as a loop but can only ever execute once, since `Addr()` returns a non-addressable pointer. Harmless, but it signals uncertainty about the reflection model.
- `doProcessConcurrently` ([async.go:40-56](https://github.com/gojekfarm/xtools/blob/main/xload/async.go#L40-L56)) selects between `ctx.Done()` and a `doneCh` that is already ready by the time the select runs, so on a cancelled context the returned error is chosen non-deterministically between `ctx.Err()` and the pool's error.
- Errors are declared with **value** receivers on `Error()` ([errors.go:11](https://github.com/gojekfarm/xtools/blob/main/xload/errors.go#L11)) but returned as **pointers** ([load.go:148](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L148)), so both `ErrRequired` and `*ErrRequired` satisfy `error` and only one of them is ever actually returned. Reproduced:

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

### 4. Let values cross the plane boundary typed, and make absence expressible.

The reproduced YAML-list-becomes-empty-string bug (5.8) and the `required` conflation (5.1) are the same root cause.
Recommend a **closed struct-based tagged union** in the `log/slog.Value` shape, with comma-ok for absence: `Get(ctx, key) (Value, bool, error)`.

**Cost.** A closed type set means ferry owns the value taxonomy forever, and a backend with a type ferry did not anticipate has to lossily project into it.
Mitigate with an explicit opaque/foreign arm (the `cty.Capsule` idea) rather than pretending the set is complete.
A struct union also costs more to construct than a bare `any`.
See section 4 for the full comparison; this is the decision with the most downstream consequence and it deserves its own ADR.

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

**Cost.** Sorting on paths that did not need it.
Negligible, and worth it for reproducible diffs of dumped config.

### 7. Design codec registration as typed functions with documented precedence, in both directions.

Copy `encoding/json/v2`'s `MarshalToFunc[T]` / `UnmarshalFromFunc[T]` / `JoinMarshalers` shape: typed at registration, resolved once, with a `SkipFunc`-style decline-and-fall-through and a **documented, overridable** precedence chain.
This is where generics genuinely pay (1c).
Never do what xload does at [load.go:301](https://github.com/gojekfarm/xtools/blob/main/xload/load.go#L301) and identify a type by comparing `Type.String()` to `"time.Duration"`.

**Cost.** More API surface than "implement this one interface".
Also: json/v2 still cannot register by runtime `reflect.Type` ([#73457](https://go.dev/issue/73457) open), and ferry probably needs both static and dynamic registration, which is more surface again.

### 8. Reconsider the per-key pull interface. This may be the second-biggest win after caching.

xload issues one backend call per leaf with no memoisation, and `SerialLoader` multiplies it by the number of backends (5.13, reproduced: 6 calls for a 2-field struct over 3 backends).
Because ferry knows the entire key set from the compiled schema **before any I/O**, it can offer a batch/snapshot entry point that xload structurally could not.

**Cost.** Every backend implementor must be able to enumerate or accept a key list, which some (Vault, dynamic secret stores) will not want.
Recommend the per-key interface as the required one and the batch form as an optional interface upgrade, with in-load memoisation always on.

### 9. Do not build on `encoding/json/v2`, but do track it.

Nothing in it is importable: the field resolver is `internal/`-fenced and `Options` is sealed with `internal.NotForPublicUse` (section 2).
But it goes GA in Go 1.27, roughly a month out, and it changes what "normal" means for `omitempty` versus `omitzero`, duplicate keys, and case sensitivity.
Choose ferry's semantics against v2.
Prefer `omitzero` semantics: they are defined in Go terms and are therefore backend-independent, which is exactly ferry's problem.

**Cost.** None, other than the discipline of re-checking at 1.27 GA.
Two v2 tag details were still moving at time of writing (`inline` versus `embed`, and whether `format:` survives); do not copy them without re-verifying.

### 10. Adopt Go 1.26 features knowingly, and pick the `go` directive as a decision.

Since Go 1.21 the `go` line is a strict minimum, so it is an API decision.
`go 1.23` buys `iter` and `Value.Seq`; `go 1.26` additionally buys `errors.AsType` and `reflect.Value.Fields`.
Recommend `go 1.26` and record why.
Use `Value.Fields()` in the **schema compiler only** - it allocates per iteration and is not a hot-path win (aclements on [#66631](https://go.dev/issue/66631)).
Avoid `Type.Methods()` entirely: it forces the linker to retain all exported methods in all packages.

**Cost.** Excludes users on older toolchains.
For a new library in mid-2026 with Go 1.27 a month away, that is a cheap exclusion.

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
