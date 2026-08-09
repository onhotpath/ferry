# ferry/windows/winreg

Load configuration from the Windows registry into a Go struct, and write it back.

> **Experimental.**
> Neither the Go API nor the way values are stored is settled yet, and either may change in a release that is not a new major version of this module.

```
go get github.com/onhotpath/ferry/driver/windows
```

## Addresses are subkeys and values

The registry keeps two namespaces under every key: values, which hold data, and subkeys, which hold more of both.
A field's address maps onto that exactly.

| the schema says | the registry holds |
| --- | --- |
| `Host string` tagged `host` | the value `host` under the driver's key |
| `DB struct{...}` tagged `db`, with `Host` tagged `host` | the value `host` under the subkey `db` |
| `Tags []string` tagged `tags` | the subkey `tags`, holding the values `0`, `1`, `2` |
| `Envs map[string]struct{...}` tagged `envs` | the subkey `envs`, holding one subkey per key |
| a schema whose root is a single value | the key's own unnamed value, which regedit shows as `(Default)` |

A value `host` and a subkey `host` under one key are two different objects, and both are legal.

## The registry does not care about case, so this driver does

Setting `Host` and then `host` leaves one value, named `Host`, holding the second write's data.
No error is raised at any point, and that is the loss this module exists to stop.

So every part of an address is folded to lower case before two of them are compared, and a schema naming two addresses that fold together is refused when the load starts, naming both:

```
ferry: /Host: winreg gives this and /host the same name, "host", so one of the two would be lost
```

The case you wrote is what gets stored, because the registry keeps whichever spelling wrote a name first.
The fold is only for the check.

Two things have no registry name at all and are refused the same way: an empty part, and a part containing a backslash.
Neither has an escape to map onto, for the reason a backslash is dangerous in the first place: any byte an escape used would be a byte a map key is entitled to contain.

## Loading

This is `Example` in [`example_test.go`](example_test.go), which `go test` compiles and runs.

```go
type DB struct {
	Host string `ferry:"host"`
	Port int    `ferry:"port,default=5432"`
}

type Config struct {
	Name string `ferry:"name"`
	DB   DB     `ferry:"db"`
}

store := newMemory()
_ = store.Set(context.Background(), "", "name", winreg.Datum{Type: winreg.TypeString, Text: "checkout"})
_ = store.Set(context.Background(), "db", "host", winreg.Datum{Type: winreg.TypeString, Text: "db.internal"})

src := winreg.NewSource(winreg.LocalMachine, `SOFTWARE\Example`, winreg.Store(store))

cfg, err := ferry.Load[Config](context.Background(), src)
if err != nil {
	panic(err)
}

fmt.Printf("%s %s:%d\n", cfg.Name, cfg.DB.Host, cfg.DB.Port)
// Output: checkout db.internal:5432
```

A program on Windows passes no `winreg.Store` and reaches the machine's own registry.
The example passes one so that it runs everywhere, which is the same seam this module's own tests use.

## Saving

This is `Example_dump` in [`example_test.go`](example_test.go).

```go
type DB struct {
	Host string `ferry:"host"`
}

type Config struct {
	Name string   `ferry:"name"`
	DB   DB       `ferry:"db"`
	Tags []string `ferry:"tags"`
}

store := newMemory()
sink := winreg.NewSink(winreg.CurrentUser, `Software\Example`, winreg.Store(store))

cfg := Config{Name: "checkout", DB: DB{Host: "db.internal"}, Tags: []string{"eu", "prod"}}
if err := ferry.Dump(context.Background(), cfg, sink); err != nil {
	panic(err)
}

for _, subkey := range slices.Sorted(maps.Keys(store.vals)) {
	for _, name := range slices.Sorted(maps.Keys(store.vals[subkey])) {
		d := store.vals[subkey][name]
		fmt.Printf("%s\t%s\t%s\t%s\n", subkey, name, d.Type, d.Text)
	}
}
// Output:
// 	name	REG_SZ	checkout
// db	host	REG_SZ	db.internal
// tags	0	REG_SZ	eu
// tags	1	REG_SZ	prod
```

A save of a slice or a map is a replacement: what the previous save left under it and this one did not write is removed first.
Everything else is a merge, and a field your struct does not map is left alone.

## What a value is stored as

Reading is wide and writing is narrow.

| read | becomes |
| --- | --- |
| `REG_SZ` | a string |
| `REG_EXPAND_SZ` | a string, exactly as stored, never expanded |
| `REG_DWORD`, `REG_QWORD` | a number |
| `REG_BINARY` | bytes |
| `REG_MULTI_SZ` | refused |

| write | becomes |
| --- | --- |
| a `[]byte` field | `REG_BINARY` |
| text at an address already stored as `REG_EXPAND_SZ` | `REG_EXPAND_SZ`, the new text |
| everything else | `REG_SZ` |

`REG_EXPAND_SZ` is read raw because expanding it is not reversible.
`%SystemRoot%-literal` expands to `C:\WINDOWS-literal`, and a save afterwards would write that back over what the operator wrote.

`REG_MULTI_SZ` is refused because it spells a sequence inside one value, and ferry addresses each element of a sequence in its own right.

`REG_SZ` carries a number's own spelling intact: `007`, `3.14159265358979` and `18446744073709551615` all come back exactly as they went in.
That is why it is what a number is written as, and there is no option to choose another type.

The one type a save preserves is `REG_EXPAND_SZ`.
A save reads the address first, and text written where the registry already holds an expandable string is stored as one, because retyping it would destroy the expansion for every other reader of that key - the same break this driver refuses to commit by expanding on read.
It costs one read per string a save writes.

## The seam

There is no dependency on any particular registry.
Six methods, and the package works against whatever you have:

```go
type Registry interface {
	Get(ctx context.Context, subkey, name string) (Datum, bool, error)
	List(ctx context.Context, subkey string) (Listing, bool, error)
	Set(ctx context.Context, subkey, name string, d Datum) error
	Create(ctx context.Context, subkey string) error
	DeleteValue(ctx context.Context, subkey, name string) error
	DeleteKey(ctx context.Context, subkey string) error
}
```

Absence is a result and not an error: `Get` and `List` report an object that is not there with `found` false and a nil error.
Removal is idempotent, and `DeleteKey` removes everything under the key as well as the key itself.
`Set` creates every subkey on the way down to the value it writes.

A `Registry` that can also say when it changed implements one more method, and that is what `winreg.Watch` needs:

```go
type Notifier interface {
	Arm(ctx context.Context) (Change, error)
}

type Change interface {
	Wait(ctx context.Context) (bool, error)
	Close() error
}
```

Registering and waiting are two calls so that a registration can outlive a wait.
The watcher arms the next one before it runs your callback, which is what stops a change landing during a reload from being lost.

The machine's own registry is behind `//go:build windows` and implements both.
`winreg.Store` is where anything else is handed over, and it is why this module builds, and its tests run, on every platform.

## The options

**`winreg.WithView(v)`** chooses which side of the WOW6432Node redirector this driver reads and writes: `ViewNative`, `View64` or `View32`.
On 64-bit Windows the registry keeps two copies of parts of the tree, and a 32-bit process is redirected into `WOW6432Node` without being told, so a 32-bit service and a 64-bit installer writing the same path write two different keys.
Name the view rather than inheriting it, and give the same one to both halves.

**`winreg.Store(r)`** names the registry this driver reads and writes through.
A nil argument is the machine's own registry, which is the default.
This is what makes a test hermetic, and it is how a registry this package does not know about arrives.

**`winreg.Watch(ctx, onChange)`** calls `onChange` whenever anything under this driver's key changes, so that a process holding a loaded value can load a fresh one.
It is refused at `Bind` when the registry behind the source reports no changes, and when the first registration cannot be placed.
A key that does not exist yet is not a refusal: the registration goes on the nearest key above it, so the save that creates the key fires the watch and the watch moves down to it.
Read its documentation before using it: the callback runs on the watching goroutine one call at a time, a panic in it takes the process down, and cancelling `ctx` is the only way to stop the watch.

There is no separator option.
The hierarchy is the registry's own syntax, not a taste.

## What this plane cannot do

**The registry has types, and a save chooses between two of them.**
An operator who retyped a value to `REG_DWORD` by hand gets it back as `REG_SZ` on the next save: the data survives, the type annotation does not.
`REG_EXPAND_SZ` is the exception and is preserved.

**A save is ordered and it is not atomic.**
Every write is staged and nothing reaches the registry until the walk has succeeded, so a save that is refused leaves the registry byte for byte as it was.
Once the commit starts, the removals a slice or a map implies run first, then the keys a present-and-empty container needs, then every value in the order the walk produced it - and a machine that fails half way through is left half way through.
The registry does have transactions, and Microsoft deprecated them; this driver does not use them.

**There is no null.**
A registry value cannot exist without a type, and every type this driver writes carries a payload, so a nil pointer to a value is refused rather than stored as something that would be indistinguishable from empty text.
A container is different: a subkey that exists and holds nothing is a real object, so a non-nil struct pointer whose every field was omitted does survive a save and a load.

**Two Go strings have no `REG_SZ` spelling.**
A registry string is UTF-16 and ends at its first NUL, so a string holding a NUL, and one holding bytes that are not valid UTF-8, are both refused.
Store those in a `[]byte` field, which is written as `REG_BINARY` and carries every byte.

**Writing under `HKEY_LOCAL_MACHINE` needs administrator rights.**
A process without them is refused when the save starts, with an error reaching `ferry.ErrReadOnly`, rather than part way through.

**The fold is Go's and the registry's is Windows'.**
The two agree on ASCII and can disagree outside it. Where they do, this driver folds less, so the pair that gets through is one the registry would merge.

**A value name may be at most 16,383 characters.**
That is the registry's own limit, and there is no ferry limit under it.

**A value name holding a backslash has no address here.**
The registry allows one and ferry cannot name it, so a key holding such a value cannot be loaded as a map or a slice: minting that member is refused with `ErrIllegalName`, and every later load of the same composite is refused the same way until the value is renamed or removed.
A key only ferry has written never holds one.

## Errors

| sentinel | what it reports |
| --- | --- |
| `winreg.ErrIllegalName` | an address the registry has no name for: an empty part, or a part holding a backslash |
| `winreg.ErrValueType` | a stored value whose registry type ferry cannot carry, which is `REG_MULTI_SZ` and anything exotic |
| `winreg.ErrUnspellable` | a Go string `REG_SZ` cannot write down |
| `winreg.ErrDeeperThanLeaf` | a subkey where a container's member takes a single value |
| `winreg.ErrOption` | a hive or a view outside the sets this package declares |
| `winreg.ErrWatch` | a watch that could not be opened |
| `winreg.ErrNoRegistry` | no Windows registry on this machine, and no `winreg.Store` given |

Each wraps one of ferry's own classes and stays reachable through ferry's wrapper, so `errors.Is` answers for it on what `ferry.Load` and `ferry.Dump` returned.
