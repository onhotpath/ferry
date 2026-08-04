# ferry/kv

Load configuration from a Consul-shaped key-value store into a Go struct, and write it back.

> **Experimental.**
> Neither the Go API nor the way values are stored is settled yet, and either may change in a release that is not a new major version of this module.
> The reason is that this is Consul-*shaped* rather than Consul: the only `Client` in this repository is a test fake, so nothing here has been run against a real store, and the interface below is read off a specification rather than confirmed against a backend.

```
go get github.com/onhotpath/ferry/driver/kv
```

## Bring your own client

There is no Consul, etcd or Redis dependency here.
You implement three methods and the package works against whatever you have:

```go
type Client interface {
	Get(ctx context.Context, key string) (value []byte, found bool, err error)
	List(ctx context.Context, prefix string) (map[string][]byte, error)
	Put(ctx context.Context, key string, value []byte) error
}
```

## Loading

The example below implements those three over a plain Go map, so it runs anywhere.
It is `Example` in [`example_test.go`](example_test.go), which `go test` compiles and runs.

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

Keys come from the tags, joined with `/`, so the nested `db.host` field reads `db/host`.
`kv.NewSink` is the other direction: `ferry.Dump` writes the same struct back to the same keys.

## Prefixes

`kv.WithPrefix("app", "cfg")` puts `db/host` at `app/cfg/db/host`.
Give it one argument per level rather than a path: `WithPrefix("app/cfg")` is rejected up front, so a prefix cannot smuggle in a level you did not mean.

## One call or one call per key

By default each field is fetched as it is needed.
`kv.WithBatch()` fetches the whole prefix in one call instead, when the source is opened.

Pick by what your store costs you: batch is one round trip and a consistent snapshot, and per-key reads only what your struct actually names, which is cheaper when the prefix holds far more than you want.
Nothing else changes - the struct you get back is identical either way.
It is a load-time option, and `NewSink` rejects it rather than quietly ignoring it.

## Everything is bytes

A store key carries no type, so `8080` in an `int` field and `"8080"` in a `string` field are both stored as `8080`, and each field parses what it reads.

There is also no way to store "nothing", as distinct from an empty value.
So a nil pointer, a nil slice and an empty map cannot be saved here, and saving one fails and names the field rather than writing an empty string that would read back as something else.
A struct with all three reports all three, and the store is left untouched.

## Whether you can write is discovered when you open, not before

Implement `ACL` and the package asks your client before writing anything:

```go
type ACL interface {
	CanWrite(ctx context.Context, key string) error
}
```

A client that does not implement it is assumed writable.

A token with no write access fails when the writer opens, before a single key is written, rather than halfway through.
A token that can write some paths and not others reports every key it was refused, not just the first, so you fix your ACL once instead of once per run.

## More

The [package documentation](https://pkg.go.dev/github.com/onhotpath/ferry/driver/kv) is the reference for everything above, and the design records behind it are in [`docs/adr/`](../../docs/adr/).
