package ferry

// PROTOTYPE for #109. Measurement 5: what the schema cache holds after two
// dynamic types go through one Dump[any] call site.

import (
	"context"
	"fmt"
	"slices"
	"testing"
)

type inCfg struct {
	Name string `ferry:"name"`
}

type inTwo struct {
	Host string `ferry:"host"`
	Port string `ferry:"port"`
}

type nopSink struct{}

func (nopSink) Bind(*AddressSet) (OpenWriterFunc, error) {
	return func(context.Context) (Writer, error) { return nopWriter{}, nil }, nil
}

type nopWriter struct{}

func (nopWriter) Set(context.Context, Path, Value) error { return nil }

func TestP109_M5_CacheKeys(t *testing.T) {
	reg := NewRegistry()

	// One call site, three calls, two dynamic types.
	dump := func(v any) error {
		return Dump(context.Background(), v, nopSink{}, WithRegistry(reg))
	}

	for _, v := range []any{inCfg{Name: "a"}, inTwo{Host: "h", Port: "p"}, inCfg{Name: "b"}} {
		if err := dump(v); err != nil {
			t.Fatalf("dump(%T) = %v", v, err)
		}
	}

	keys := cacheKeys(reg)

	t.Logf("M5 cache holds %d entries: %v", len(keys), keys)

	if len(keys) != 2 {
		t.Fatalf("M5 wanted 2 entries, got %d", len(keys))
	}

	// And the interface type itself must never appear as a key.
	if slices.Contains(keys, `{typ:interface {} tagKey:"ferry"}`) {
		t.Fatal("M5 the interface type reached the cache")
	}
}

// cacheKeys renders every schemaKey this registry's cache holds, sorted.
func cacheKeys(reg *Registry) []string {
	var keys []string

	reg.schemas.Range(func(k, _ any) bool {
		sk, _ := k.(schemaKey)
		keys = append(keys, fmt.Sprintf("{typ:%v tagKey:%q}", sk.typ, sk.tagKey))

		return true
	})

	slices.Sort(keys)

	return keys
}
