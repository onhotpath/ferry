package kv_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/kv"
)

// memory is a [kv.Client] over an ordinary Go map, and the whole of what this
// driver needs from a backend: read one key, list a folder, write one key.
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
