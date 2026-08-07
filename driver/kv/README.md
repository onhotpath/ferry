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
You implement four methods and the package works against whatever you have:

```go
type Client interface {
	Get(ctx context.Context, key string) (value []byte, found bool, err error)
	List(ctx context.Context, prefix string) (map[string][]byte, error)
	Put(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
}
```

`Delete` is what makes a save a replacement rather than an addition, and it has to be idempotent: a key the store does not hold is nothing to remove and not a failure.

## Loading

The example below implements those four over a plain Go map, so it runs anywhere.
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

func (m memory) Delete(_ context.Context, key string) error {
	delete(m, key)

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

## A save replaces what it wrote last time

A store holds whatever was put in it, so a list that loses an element and a map that loses a key would otherwise leave the previous save's keys behind, and the next load would read them back as though they were still configured.

They do not.
Before a save writes a list or a map, it tells the store to forget everything under that address, and at commit time the keys it did not write are removed.
It is `ExampleSink_replace` in [`example_test.go`](example_test.go), over the same `memory` client as above:

```go
func ExampleSink_replace() {
	type Config struct {
		Tags []string `ferry:"tags"`
	}

	store := memory{}

	sink, err := kv.NewSink(store, kv.WithPrefix("app"))
	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	if err := ferry.Dump(ctx, Config{Tags: []string{"a", "b", "c"}}, sink); err != nil {
		panic(err)
	}

	if err := ferry.Dump(ctx, Config{Tags: []string{"x"}}, sink); err != nil {
		panic(err)
	}

	for _, key := range slices.Sorted(maps.Keys(store)) {
		fmt.Printf("%s = %s\n", key, store[key])
	}

	// Output:
	// app/tags/0 = x
}
```

Only a list or a map is replaced this way.
A field your value omits is not written and is not removed either: silence never deletes anything.

## Prefixes

`kv.WithPrefix("app", "cfg")` puts `db/host` at `app/cfg/db/host`.
Give it one argument per level rather than a path: `WithPrefix("app/cfg")` is rejected up front, so a prefix cannot smuggle in a level you did not mean.

## One call or one call per key

By default each field is fetched as it is needed.
`kv.WithBatch()` fetches the whole prefix in one call instead, when the source is opened.

Pick by what your store costs you: batch is one round trip and a consistent snapshot, and per-key reads only what your struct actually names, which is cheaper when the prefix holds far more than you want.
Nothing else changes - the struct you get back is identical either way.
It is a load-time option, and `NewSink` rejects it rather than quietly ignoring it.

## One load, several reads at once

A reader from this package tells ferry that it tolerates overlapping calls, so `ferry.MaxConcurrency(n)` overlaps a single load's reads:

```go
cfg, err := ferry.Load[Config](ctx, src, ferry.MaxConcurrency(4))
```

It declares no bound of its own, because how much overlap your store will take is a fact about your store and your token, and you are the one holding both.

What it costs you is what `Client` already asks for: your client has to be safe for use from many goroutines at once.
Nothing else inside one open is shared - a batch open's snapshot is only ever read, and this package serialises the key function that turns an address into a store key.

A batch open has nothing left to overlap, since it already made its one call.
The struct you get back is the same either way, under any budget, and ferry's conformance suite holds this driver to exactly that.

## Everything is bytes

A store key carries no type, so `8080` in an `int` field and `"8080"` in a `string` field are both stored as `8080`, and each field parses what it reads.

There is also no way to store "nothing", as distinct from an empty value.
So four shapes cannot be saved here: a nil pointer, a nil slice, an empty map, and a non-nil pointer to a struct whose every field was omitted.
Saving one fails and names the field rather than writing an empty string that would read back as something else.
A struct with all four reports all four, and the store is left untouched.

The failures are two classes, and a caller matching on one of them misses the other.
A null at a leaf is this package's own refusal and carries `ferry.ErrValue`.
The other three are a container speaking at its own address, which this package supplies no way to write, so ferry refuses them for it and they carry `ferry.ErrPlane`.

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
