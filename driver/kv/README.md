# ferry/driver/kv

> **Experimental.**
> Neither this module's Go API nor the spelling it gives a value on the plane is under [ADR-0013](../../docs/adr/0013-what-a-plane-holds-is-a-published-interface.md)'s compatibility promise yet, and both may move in a release that is not a new major version of this module, which [ADR-0002](../../docs/adr/0002-core-and-sub-modules.md) tags independently of core.
> The reason is that this driver is Consul-shaped rather than Consul: the only implementation of `Client` in this repository is an in-repo test fake, so nothing here has been run against a real key-value store, and the shape of `Client` and `ACL` is a design read off a specification rather than one confirmed against a backend.

ferry's driver for a Consul-shaped key-value store: opaque byte values under slash-separated keys.
It reaches the store through a `Client` interface this package declares rather than a client it imports, so Consul, etcd, a Redis hash and a table with two columns are all the same backend to it.

## Loading a struct

Three methods are the whole of what a caller implements.

```go
type Client interface {
	Get(ctx context.Context, key string) (value []byte, found bool, err error)
	List(ctx context.Context, prefix string) (map[string][]byte, error)
	Put(ctx context.Context, key string, value []byte) error
}
```

There is no Consul client in this repository, so the example implements them over a map, as `Example` in [`example_test.go`](example_test.go), which `go test` compiles and runs.

```go
type memory map[string][]byte

func (m memory) Get(_ context.Context, key string) ([]byte, bool, error) {
	value, found := m[key]

	return value, found, nil
}

func (m memory) List(_ context.Context, prefix string) (map[string][]byte, error) {
	out := map[string][]byte{}

	for key, value := range m {
		if strings.HasPrefix(key, prefix) {
			out[key] = value
		}
	}

	return out, nil
}

func (m memory) Put(_ context.Context, key string, value []byte) error {
	m[key] = value

	return nil
}

func Example() {
	type DB struct {
		Host string `ferry:"host"`
		Port int    `ferry:"port,default=5432"`
	}

	type Config struct {
		Name string `ferry:"name"`
		DB   DB     `ferry:"db"`
	}

	store := memory{
		"app/name":    []byte("checkout"),
		"app/db/host": []byte("db.internal"),
	}

	src, err := kv.NewSource(store, kv.WithPrefix("app"))
	if err != nil {
		panic(err)
	}

	cfg, err := ferry.Load[Config](context.Background(), src)
	if err != nil {
		panic(err)
	}

	fmt.Printf("%s %s:%d\n", cfg.Name, cfg.DB.Host, cfg.DB.Port)

	// Output:
	// checkout db.internal:5432
}
```

`kv.NewSink` is the other direction, and `ferry.Dump` writes the same struct back to the same keys.

## Batch or lazy, and ferry never learns which

`WithBatch()` fetches the whole prefix in one call when the reader is opened, instead of one call per address as the walk reaches it.
Nothing above the driver can tell the two apart: the three-address schema in `TestBatchAndLazyAgree` makes three backend calls lazily and one in batch, and loads the identical value either way.
Batch is one round trip and a snapshot that cannot change under the walk; lazy reads only the addresses the walk reaches, which is cheaper against a prefix holding far more than the struct names.
It is a source's option, and `NewSink` refuses it rather than ignoring it.

## What the plane holds

Opaque bytes, and no null.
A store key carries no type, so an `int` 8080 and a `string` "8080" are both stored as `8080` and both read back as text each field parses with its own parser.
Because there is no null, a nil pointer, a nil composite and an empty composite are refused loudly rather than stored as empty text, which would make "this field is nil" and "this field is the empty string" one observation.
The refusal is per address and the store is left untouched, so a struct with all three fails with three errors naming three addresses rather than with the first.

## Read-only is a runtime fact

A client whose credentials cannot write says so when the writer is opened: not when the sink is bound, which does no I/O and so cannot know, and not partway through writing, which has already half-written the store.
Implement `ACL` to be asked, and a client that implements nothing is writable everywhere.
The refusal wraps `ferry.ErrReadOnly`, and a token with write access to some paths and not others reports every address it refused rather than the first.

## The prefix is an option taking segments

`WithPrefix("app", "cfg")` puts `/db/host` at `app/cfg/db/host`.
It takes one argument per level and never text, so `WithPrefix("app/cfg")` is refused at the call rather than a key that happens to work, and it may be given once.

## Where the reasoning is

The package doc carries the full argument for every rule above.
The decisions are [ADR-0004](../../docs/adr/0004-source-and-sink.md) for the source and sink contract, [ADR-0003](../../docs/adr/0003-how-a-leaf-addresses-a-plane.md) for how an address becomes a key, and [ADR-0005](../../docs/adr/0005-the-supported-type-set.md) for what a value is spelled as.
