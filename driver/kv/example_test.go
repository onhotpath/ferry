package kv_test

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/kv"
)

// memory is a [kv.Client] over an ordinary Go map, and the whole of what this
// driver needs from a backend: read one key, list a folder, write one key,
// remove one key.
//
// It is here so the example runs, and so a reader can see the shape a real
// adapter over consul/api, etcd or a two-column table has to fill.
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

// Example loads an annotated struct out of a store, under a prefix.
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

// ExampleSink_replace shows what a second save does to a list that lost an
// element: the keys the save did not write are removed, so a load afterwards
// reads the value that was saved rather than the union of both saves.
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
