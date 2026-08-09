# 2. What ships in core, and what ships as a sub-module

Status: Accepted
Date: 2026-08-02
Ticket: [#3](https://github.com/onhotpath/ferry/issues/3)

## Context

[ADR-0001](0001-what-ferry-supports.md) settled that no data plane ships in core, on the `database/sql` model.
It did not settle where that boundary falls in the filesystem, how many Go modules ferry publishes, or what a contributor does when the answer is not obvious.

It also left three things to this ADR by name: whether every sub-module takes core's `go` directive, the layout of the `go 1.27` fallback, and where the data-plane boundary actually falls including the environment-variable case.

ADR-0001 gives a contributor two instruments and does not say how they compose.
The plane-agnosticism test excludes anything that requires knowing what the plane is for.
The bucket rule admits by un-buildability, with Enabled as the default landing place.
Environment variables pass the first and fail the second.
The two conformance harnesses fail the second and are In core anyway.
So the instruments are not two filters in series, and this ADR says what they are instead.

## Decision

### Core admits by a veto and two routes

Plane-agnosticism is a **veto**.
Passing it admits nothing.

Core admits exactly two kinds of thing:

- **(a) Capability.**
  A mechanism no driver can supply for itself.
  The walk, the schema compiler, tag parsing, encode and decode, defaults and zero values.
- **(b) Authority.**
  An obligation core imposes but cannot compile-check, shipped as the thing that checks it.
  The driver conformance suite and the round-trip property harness.

Route (b) is why the harnesses are in core when route (a) would push them out.
Anyone can write a conformance suite for ferry's driver contract.
The point is not that it is hard, it is that a third party's suite settles nothing.
If two people publish disagreeing suites, neither one binds a driver author.
The suite is only worth anything when it ships from the same place as the rule, because it *is* the rule in executable form.
That is the `testing/slogtest` precedent read correctly: it is not in the standard library because it is difficult, it is there because `slog.Handler`'s prose rules are the standard library's and a third-party reading of them would bind nobody.

Anything satisfying neither route ships outside core.

### No plane in core, and environment variables are not the exception

Environment variables are the case ADR-0001 named as borderline, and they resolve cleanly under the veto-and-routes reading.
`os.LookupEnv` knows nothing about configuration, so the veto passes.
It is not machinery the engine needs, and it imposes an obligation on nobody, so neither route admits it.

There is a second argument that does not rest on "it is only fifteen lines", and it is the stronger one.

**Environment variables have no honest Dump.**
Setting a process's own environment is not the operation anybody wants, and it is process-global mutation, which makes it a poor sink on its own terms.
The Dump target people actually want is a `.env` file or an `[]string` environ for `exec`.
`.env` is a format, with quoting, escaping and multiline rules, and a format is plane knowledge.
So an environment plane in core forces core either to ship half a driver, breaking ferry's bidirectional premise on its one blessed plane, or to make a `.env` quoting decision, breaking the veto.

> **Amended under [#352](https://github.com/onhotpath/ferry/issues/352): the `.env` quoting decision has been made, in `driver/env`, and core's veto is untouched.**
>
> As published this section reads as though the `.env` quoting decision were open, and the heading above it says environment variables have no honest Dump.
> `driver/env` now ships `DotEnvSink`, a `.env` reader that layers files under the process environment, and the quoting and escape table that goes with them.
> [ADR-0013](0013-what-a-plane-holds-is-a-published-interface.md) carries that table as a pinned representation.
>
> **The argument is unchanged and is the reason the code landed where it did.**
> This section's conclusion is that a `.env` quoting decision is plane knowledge and therefore cannot be core's; the driver is outside core, so making it there is this rule being followed rather than overturned.
> The sentence that moves is only the reading that nobody had made the decision yet.
>
> **The dependency is new and is the cost.**
> `driver/env`'s `require` block was empty and now carries `github.com/fsnotify/fsnotify`, which every consumer of the process-environment source pulls even though that half touches no filesystem.
> Core's own rule - stdlib, unconditionally - is untouched, because this is a driver module.
> [ADR-0020](0020-watch-and-reload.md) is where the dependency is argued.

### The memory plane is apparatus, and the line that keeps it so

The round-trip property harness has to move a value: `Dump` it into something, `Load` it back, compare.
That something is a plane, so core contains one whether or not it admits one.

It ships, exported, in the public testing package, and it enters by route (b) rather than as a driver.
xload ships `MapLoader` and people reach for it constantly; if ferry ships nothing, every user writes the same ten lines and gets the same things wrong, because a flat key space has non-obvious collision and case rules.
The survey measured what that costs elsewhere: koanf's `Flatten` resolves `{"a.b":1}` against `{"a":{"b":2}}` nondeterministically, 255 to 45 over 300 runs, and viper silently destroys two of three case-variant keys.
The memory plane is those rules written as code, the same way the suite is the contract written as a test.
Whatever [#4](https://github.com/onhotpath/ferry/issues/4) decides about keys, this is where the decision becomes executable.

The line that stops this becoming a slippery slope:

> A plane with no serialization format and no I/O is not a plane, it is a map.

YAML has a format.
Consul has I/O.
Neither ever qualifies.
The recording sink that ADR-0001's schema-extraction pattern needs ships in the same package on the same grounds.

### The repo is one core module and a set of driver modules

```
github.com/onhotpath/ferry                 go.mod    core
  ferrytest/                                         conformance suite, round-trip
                                                     harness, memory plane,
                                                     recording sink
  driver/
    <name>/                                go.mod    one module per driver
  compat/, cmd/                                      reserved, unused today
```

> **Corrected: `examples/` is a fifth entry in this layout, and it is a module.**
>
> As published the diagram has no row for it, because there was nothing to put in one.
> `examples/` now holds runnable programs that a guide page quotes, it carries its own `go.mod`, `go.work` uses it, and CI discovers it by the same glob that finds `driver/*/go.mod` - so it is built, vetted, tested and linted like any other module here.
> It is a module for this ADR's own reason: an example that imports a driver would put that driver in core's dependency graph, and core's `require` block stays empty.
> **Nothing in the layout rule moves**: one core module, one module per driver, and anything that needs a dependency core may not have gets a module of its own.

> **Amended under [#364](https://github.com/onhotpath/ferry/issues/364): `watch/` is a sixth entry in this layout, and it is a directory of the core module.**
>
> As published the diagram has one row under core, `ferrytest/`, because that was the only package core shipped beside the root one.
> `watch/` sits beside it: the typed watch helper, importing the root package and the standard library and nothing else, so core's empty `require` block is untouched.
> It is a directory rather than a module for the same reason `ferrytest/` is, and the argument that admits it under route (b) is in [ADR-0020](0020-watch-and-reload.md)'s amendment rather than repeated here.

Separate repositories are rejected because first-party drivers exist to be built against core's HEAD on every commit, which is the whole reason they exist at all.

`driver/` rather than `plane/` because ADR-0001 fixed those words: a plane is the external system, a driver is the code someone writes against the contract.
The prefix also keeps the root namespace free, which matters because ADR-0001 leaves an xload compatibility module open and [#14](https://github.com/onhotpath/ferry/issues/14) may want a command.
`ferry/xload` and `ferry/yaml` as siblings would mean entirely different things.

The load-bearing reason is that **the directory is a CI predicate**.
`driver/*` is exactly the set of modules that must pass the conformance suite, which is a glob rather than a hand-maintained list.
A contributor adding a driver gets the suite applied whether they thought about it or not, and cannot opt out without moving their module out of the directory that names what it is.

Tag prefixes follow from the module path and are not a choice: `driver/yaml/v0.1.0`.

### First-party drivers ship to exercise the contract

ferry publishes a small first-party driver set.
The admission rule is not popularity:

> A first-party driver ships only to exercise an axis of the driver contract that no existing first-party driver exercises.
> It is a test of the contract that happens to be useful.

ADR-0001 gives core one lever over what it does not ship, and that lever is the conformance suite.
A suite no real driver runs in CI is prose again within two releases.
The memory plane cannot keep it honest, precisely because it has no format and no I/O, and every interesting clause in the driver contract is about the things it lacks: serialization, nested structure flattened to keys, parse errors, network I/O and cancellation, batch versus per-key access.

The rule caps the set at roughly three and tells a contributor proposing a fourth exactly what to argue.
It is also why ferry would ship YAML but not TOML: TOML exercises nothing YAML does not, and that is not a judgement about TOML.

The list is deferred to [#5](https://github.com/onhotpath/ferry/issues/5), because which axes exist is a property of the source and sink signatures and those are not decided.

> **Corrected: four drivers ship, and the fourth argued what this rule asked it to.**
>
> As published the rule "caps the set at roughly three and tells a contributor proposing a fourth exactly what to argue".
> [ADR-0004](0004-source-and-sink.md) took the deferred list and named `yaml`, `kv` and `env`; `driver/http`, the query-parameter plane, is the fourth, and it is in on the axis this rule exists to test - a plane constructed per request, which nothing else exercises and which [ADR-0012](0012-the-caller-held-binding.md)'s held binding is shaped around.
> **The rule is unchanged and it worked.** "Roughly three" was an estimate of how many axes the contract has, not a quota, and the fourth driver was admitted by making exactly the argument this paragraph demands of it.

> **Amended under [#272](https://github.com/onhotpath/ferry/issues/272): five drivers ship, and the fifth is the first whose module cannot be built without a dependency.**
>
> As published, and as corrected above, the list is four and `driver/http` is the fourth.
> `driver/windows` is the fifth, and it is the Windows registry in both directions: `winreg` is a `Source` and a `Sink`, and `protect` is a decorator that wraps another driver's `Source` and `Sink` and puts DPAPI-NG between them and the plane.
> The estimate above is the sentence this admission keeps being read against, so it is restated rather than re-argued: the cap is however many axes the contract turns out to have, and a driver is admitted by naming one no shipped driver occupies.
>
> **Five axes, argued one at a time.**
>
> - **A tree-shaped live store.** The plane's hierarchy is the store's own syntax, and it is read and written one key at a time against a running system, where `driver/yaml` holds a document it parses whole and `driver/kv` holds a flat namespace with no hierarchy of its own.
>   Note what this does *not* make it: `winreg` is a **flat** driver in ferry's sense, because `RegOpenKeyEx` takes a subkey path string, so the driver builds a plane key and calls `ferry.NewKeys` like every other flat driver.
>   The axis is the shape of the store, not the shape of the driver's obligation.
> - **Two namespaces at every level.** A value lives under a key, so the last segment of an address names a value and the segments before it name a subkey.
>   That is the first plane where [ADR-0003](0003-how-a-leaf-addresses-a-plane.md)'s leaves and containers tables are two real namespaces of the plane rather than one namespace core splits for its own checking.
>   It rests on one fact, that a value `foo` and a subkey `foo` coexist under one key, which is asserted in [#272](https://github.com/onhotpath/ferry/issues/272)'s prose and is **not** in its measured table.
>   The measurement it was owed is now a test rather than a session: `TestWindowsHoldsAValueAndASubkeyOfOneNameAtOnce` writes both under one key on the CI runner's own registry, lists them, and loads a schema holding a leaf beside a section of that name.
>   It fails loudly rather than skipping if Windows ever disagrees, because this axis and the address mapping both fall with it.
> - **A typed plane.** `REG_SZ`, `REG_DWORD`, `REG_QWORD`, `REG_BINARY` and `REG_EXPAND_SZ` are type tags stored beside the data, so a read carries plane-side type information.
>   [ADR-0004](0004-source-and-sink.md)'s axis table has "carries plane-side type information" against `yaml` alone, and `yaml` is the driver that walks segments as a tree, so this is the first plane that produces a key and still carries a type.
>   What the other flat drivers store is untyped, in two different ways rather than one: `env` and `http` store text, and `driver/kv` stores opaque bytes, which is its own row in the same table.
> - **`ErrReadOnly` with a real cause.** Opening HKLM for write without the rights returns `ERROR_ACCESS_DENIED`, which is [ADR-0004](0004-source-and-sink.md)'s "writable in principle but not right now" arriving from the operating system.
>   Today the only producer of that error in the tree is `driver/kv`'s optional `ACL` hook, which simulates it.
> - **A decorator over another `Source` and `Sink`.** `protect` implements neither plane nor format: it wraps a driver and rewrites the values crossing the boundary.
>   Nothing in this repository exercises that composition, and it is the axis that says whether the contract is composable at all rather than only implementable.
>   The axis is the composition and not the protection, which matters because the first run of the real DPAPI-NG on a Windows runner moved what the package's own defaults are without touching this admission.
>   `SID=` and `SDDL=` protection descriptors are resolved through Active Directory's key distribution service, so they need a domain controller the machine can reach ([MS-GKDI](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-gkdi/60b439bf-d845-4bee-8487-b231d6fdfb92): the server "runs on a DC with a DC functional level of DS_BEHAVIOR_WIN2012 or higher in an Active Directory domain").
>   `windows-latest` is standalone, so `SID=S-1-5-18` - the package's original headline descriptor - failed every save with `NTE_ENCRYPTION_FAILURE` (`0x80090034`), which is the first thing this driver measured that no test double could have.
>   The package now defaults to the `LOCAL=` rules, which the machine resolves for itself, and says in its README which descriptor works in which deployment.
>   None of that changes the axis, and it is recorded here because this is where the driver was admitted.
>
> **Case folding is not one of the axes, and the issue's headline claim that it is does not survive.**
> `driver/env` folds every segment to upper case, which [ADR-0003](0003-how-a-leaf-addresses-a-plane.md) carries as the fourth column of its key-function table, and `driver/http`'s header plane folds through `textproto.CanonicalMIMEHeaderKey`.
> A folding plane was already shipped twice over, and this driver's fold is a reason it must build a key rather than a reason it may ship.
>
> **The dependency is `golang.org/x/sys`, and it is argued here because no other ADR owns it.**
> The Win32 registry and DPAPI-NG are reached through system calls and through DLL procedure lookups, and `golang.org/x/sys/windows` is the only maintained Go binding for either.
>
> The claim to be careful with here is that the module could not be written without it, because that is not true and the honest argument is a different one.
> Stdlib `syscall` carries the registry read surface on Windows: `RegOpenKeyEx`, `RegQueryValueEx`, `RegQueryInfoKey`, `RegEnumKeyEx` and `RegCloseKey` are all exported from it.
> It carries none of the write surface - there is no `RegCreateKeyEx`, `RegSetValueEx`, `RegDeleteValue` or `RegDeleteKey` in it - and it has nothing from `ncrypt.dll` either, but `syscall.NewLazyDLL` resolves a procedure out of any DLL, which is the same mechanism `protect` already uses for DPAPI-NG.
> So the gap could be closed by hand.
>
> **The argument is maintained versus frozen, and that argument holds.**
> `syscall`'s own package documentation says most new code should prefer `golang.org/x/sys`, and points at the Go 1.4 proposal that closed `syscall` to new additions; its Windows surface is what it was when that policy landed and will not grow.
> `golang.org/x/sys/windows` is where Windows API work in Go actually happens, and it is what every other Go project reaching this surface is already on.
> Writing the missing half against raw DLL lookups would be reimplementing a maintained package with an unmaintained copy, and getting the handle, rights and UTF-16 conventions right in it is exactly the work that package exists to have already done.
> That makes this a dependency traded against a frozen equivalent rather than against a convenience, which is not the trade `driver/env`'s fsnotify made: there is no polling fallback for a registry handle.
> It is also a `golang.org/x` module, which is the Go project's own repository for the platform surfaces the standard library does not export, so it is third party in the module graph and is not third party in the sense this ADR's rule is guarding against.
>
> **Who pays is every consumer of `driver/windows` and nobody else.**
> It is a module of its own, so core's `require` block is untouched and stays empty, and a program that imports `driver/yaml` and never imports this one never sees `golang.org/x/sys`.
> That is the layout rule above doing the work it exists for, and it is the whole of the mitigation.

### Core's dependency set is stdlib, unconditionally

> Core's `go.mod` carries no non-stdlib `require`, and core imports only stdlib packages that are unconditionally available at core's declared floor.

Two consequences, one intended and one that resolves a conflict the survey left open.

**The harnesses are bound by this.**
That is not a sacrifice.
ADR-0001 closed core's type set, [#7](https://github.com/onhotpath/ferry/issues/7) enumerates it, and codec registrants supply their own values for types ferry does not own.
A property harness over a closed enumerated set with caller-supplied values is a table, not a generator.
It never needed a property-testing dependency, and the rule pushes it toward the design it should have had anyway.

**`encoding/json/v2` and `encoding/json/jsontext` are excluded from core.**
They are stdlib with an asterisk.
Measured on `go1.27rc2`, a package importing `encoding/json/v2` under `GOEXPERIMENT=nojsonv2` fails with `build constraints exclude all Go files in .../src/encoding/json/v2`, and builds clean with the flag unset.
That switch is the consumer's, not ferry's, and the Go release notes present it as their escape hatch for v2 compatibility problems.

This keeps ADR-0001's `go 1.27` fallback alive **by construction** rather than by anyone remembering to check.
Exactly one thing destroys that fallback, and it is core importing json/v2.

**What it costs, stated plainly.**
Core's codec chain cannot natively recognise json/v2's `MarshalerTo` or `UnmarshalerFrom`, because declaring `MarshalJSONTo(*jsontext.Encoder) error` needs the import.
That recognition, if ferry wants it, ships in a sub-module.
There is no clever way around it: probing for the method by name string without importing the type is xload's `Type.String() == "time.Duration"` defect ([5.9](../research/generics-and-modern-go.md), [load.go:301](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L301)) in another costume.

The cost is already paid.
[#18](https://github.com/onhotpath/ferry/issues/18) settled that first-class json/v2 support means an explicitly pinned option set rather than inherited defaults, which is semantics adoption and costs no import.
The reading of "first class" that needed the import was never the one ferry chose.

This constrains [#12](https://github.com/onhotpath/ferry/issues/12).
If #12 concludes core must recognise `MarshalerTo`, that is a proposal to amend this ADR, argued explicitly.

### The `go` directive: each module declares its own, floored at core's

The floor is transitive through imports, so a sub-module declaring a **lower** directive than core buys nothing.
Measured on go1.26.5 with a sub-module at `go 1.26` importing a core at `go 1.27`:

```
go: module ../core requires go >= 1.27 (running go 1.26.5; GOTOOLCHAIN=local)
```

The lever only points up, and ferry uses it in that direction:

- A driver module may declare a **higher** `go` directive than core, never lower.
  CI asserts it.
- **Core's directive never names a release newer than the second-most-recent stable Go release.**
  ferry therefore always builds on the two releases Go itself supports.
- **Launch is a named, one-time exception.**
  `go 1.27` while 1.27 is current excludes everyone on 1.26, which ADR-0001 accepted and priced.
  Naming it as an exception matters, because otherwise the launch decision reads as precedent.
- **Raising core's floor is never a patch release.**
  Go's tooling does not treat it as breaking, but the measurement above shows what it does to a consumer: the build stops, with no change on their side.

The reason drivers are not held to core's directive verbatim, which is what [xtools does across all twenty of its modules](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/providers/yaml/go.mod), is that lockstep couples in the wrong direction.
A driver dependency that one day needs a newer Go would otherwise reach into core's floor, and core's floor is the single line deciding who can import ferry at all.

**The fallback, for the record.**
Core at `go 1.26` with no json/v2 import, and the v2 dependency in a sub-module at `go 1.27`.
Verified buildable on this design as of this date: core compiles under `GOTOOLCHAIN=local` on go1.26.5, and the `go 1.27` sub-module importing `encoding/json/v2` compiles under `GOTOOLCHAIN=go1.27rc2`.
As of this date Go 1.27 is still `go1.27rc2` with `"stable": false`, so the fallback is live rather than theoretical.

> **Amended under [#366](https://github.com/onhotpath/ferry/issues/366): core's floor drops to `go 1.26`, and the launch exception is narrowed and re-based to cover it, not withdrawn.**
>
> The general rule above is that core's directive never names a release newer than the second-most-recent stable Go release.
> With `go 1.26` current, the second-most-recent stable release is `go 1.25`, so `go 1.26` is still newer than the general rule allows.
> **The launch exception stays in force, sized to a smaller directive, rather than the repo coming back into compliance with the general rule.**
> Reading this as compliance restored would be wrong: the rule still names 1.25, and core still does not sit there.
>
> **What now holds the floor above the rule is `errors.AsType`, a Go 1.26 API used in ten places in `error.go`, not `encoding/json/v2`.**
> The exception's justification changes along with its size: it no longer prices in a whole release's worth of json/v2 access, only the one API core's own error model already depends on.
> Reaching `go 1.25` and the general rule would mean giving up `errors.AsType` in `error.go`, which is a different decision with its own cost and is not taken here.
>
> **The fallback paragraph above is now spent rather than hypothetical, and it held.**
> Every module compiles, vets and tests under `-gcflags=-lang=go1.26`, including test binaries and `driver/windows` under `GOOS=windows`, and a `go doc -all` diff between real 1.26 and 1.27 GOROOTs across all 48 stdlib packages the repo imports finds no 1.27-only symbol referenced anywhere in core or a driver.
> The one line in the repo that needed 1.27 was in `examples/concurrent-driver/plane.go`, using promoted fields in a composite literal, a 1.27 language feature, and it is rewritten to a form that compiles on both.
> `toolchain go1.27rc2` is deleted from `go.work`, so no file in the repository carries a toolchain directive at all, and CI moves to a released Go 1.26.x so the floor is genuinely exercised rather than merely declared.
>
> **"Raising core's floor is never a patch release" cuts both ways.**
> The return to `go 1.27` at GA is the same kind of event as this drop and gets the same treatment: a deliberate, named change to core's directive, not a release nobody notices.
>
> **Unchanged**: a driver module may still declare a higher `go` directive than core and never a lower one, CI still asserts it, and the comparison stays relative, so a uniform drop across core and drivers passes it untouched.

### Development and release

**No `replace` directive is ever checked in.**
A `go.work` at the repo root gives local development, and flipping one environment variable exercises the resolution a real consumer gets.
Measured:

```
GOWORK=<workspace> GOPROXY=off  ->  builds, resolves the sibling on disk
GOWORK=off         GOPROXY=off  ->  go: downloading example.com/ferry v0.3.0
                                    module lookup disabled by GOPROXY=off
```

xtools' providers each carry `replace github.com/gojekfarm/xtools/xload => ../..` in their published `go.mod`, which gives only the first mode, permanently.
Their CI has never once built against the `xload` version they publish against.
CI therefore runs each module twice, in workspace mode and with `GOWORK=off`, the second skipped until core has a tag.

**Sub-modules are versioned and released independently of core.**
Each module is tagged only when it changes.
xtools tags all twenty modules at `v0.10.0` simultaneously, including modules that did not change, which makes every version number assert something untrue and means a one-line driver fix cannot ship without a core release.
Independent tags cost legibility, since `driver/yaml/v0.2.1` does not say which core it works with.
`go.mod` says, which is where the answer belongs.

**Release order is forced**: tag core, bump each driver's `require`, tag drivers.

**ferry starts at v0 and stays there deliberately.**
ADR-0001 made ferry's tag vocabulary effectively frozen at v1, because strict rejection means adding an option later breaks anyone who used that word.
v0 is the only place semver allows taking a tag word back.
The trigger for v1 is named rather than left to drift: [#11](https://github.com/onhotpath/ferry/issues/11)'s grammar surviving real use, across at least one first-party driver and one external adopter.

> **Amended under [#28](https://github.com/onhotpath/ferry/issues/28): the trigger gains a second input, and this ADR gains a second promise.**
> [ADR-0013](0013-what-a-plane-holds-is-a-published-interface.md) states **plane compatibility** separately from the API compatibility this section decides, because what a module writes into a plane is a published interface that semver does not describe and no tool in the Go toolchain can see.
> Measured: two modules differing only in the text a function returns are clean under `go build`, `go vet`, `gofmt` and `apidiff`, and the consumer's own round-trip test passes on both.
> The instrument is the same one: a change to [ADR-0005](0005-the-supported-type-set.md)'s golden column is a **major** version of the module that owns the row, because a major version is the only release a consumer cannot receive without editing a line - measured against a real `GOPROXY`, where `go get -u` crosses a minor and changes what the program writes, and cannot cross a major.
> So the second input to the v1 trigger is the golden column: at v1 every pinned representation becomes a promise with a major version behind it.

Major versions beyond v1 are not decided here.

## Consequences

- A contributor decides core-or-not without asking, in one pass: apply the veto, then look for route (a) or route (b).
  Neither route means outside core.
- Core is one module with an empty `require` block, which is itself an adoption argument against libraries that drag in a large graph.
- The zero-dependency rule and the conditional-stdlib rule are the same instinct: core's buildability must not depend on anything a consumer can switch off.
- Core cannot recognise json/v2's `MarshalerTo` or `UnmarshalerFrom`.
  This is a real constraint on [#12](https://github.com/onhotpath/ferry/issues/12) and is stated as one.
- ferry publishes several modules from day one, so every release is at least a two-step operation and the `GOWORK=off` CI job is load-bearing rather than decorative.
- The first-party driver list cannot be written until [#5](https://github.com/onhotpath/ferry/issues/5) lands, because the rule admits by contract axis and the axes are a property of the signatures.
- `nojsonv2` is documented as expected to be removed in a future Go release.
  When it goes, half the justification for excluding json/v2 from core goes with it, and the rule deserves revisiting rather than surviving on inertia.

## Items from the xload survey

The survey is [`docs/research/generics-and-modern-go.md`](../research/generics-and-modern-go.md), section 5, against `github.com/gojekfarm/xtools` at [`a90b3aa`](https://github.com/gojekfarm/xtools/commit/a90b3aad2133248cec50f6b4d6e37b0d9e788adb).
Three of the fourteen bear on module layout.

- **5.8**, type information destroyed at the boundary, and **5.11**, the YAML provider silently discards parse errors.
  Both are defects in a provider sub-module, invisible to core's own tests.
  ADR-0001 answered them with driver fidelity as a stated obligation and a conformance suite as the check.
  This ADR is what makes that check bite: `driver/*` is a CI glob, so every first-party driver runs the suite, and 5.11 becomes a regression test against a real YAML driver rather than against a mock.
- **5.9**, the decoder chain is fixed, one-directional and context-free.
  Owned by [#12](https://github.com/onhotpath/ferry/issues/12).
  It surfaces here because its concrete symptom, matching a type by `Type.String()`, is the only route by which core could recognise `MarshalerTo` without importing `jsontext`.
  This ADR rules that route out by name rather than leaving it available.

**5.14** was enumerated rather than assumed.
Its four items are duplicated ways to set a loader, a `CanAddr` loop that can only run once, a non-deterministic select on a cancelled context, and value receivers on `Error()` where pointers are returned.
All are exported-surface or concurrency defects, owned by [#5](https://github.com/onhotpath/ferry/issues/5), [#9](https://github.com/onhotpath/ferry/issues/9) and [#20](https://github.com/onhotpath/ferry/issues/20).
None bears on module layout.

The remaining eleven are unaffected by this ADR.
