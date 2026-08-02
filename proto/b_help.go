package main

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strings"
	"path/filepath"
	"reflect"
	"runtime"
)

func typeOf[T any]() reflect.Type { return reflect.TypeFor[T]() }

func mustSchema[T any](options ...Option) *schema {
	o := defaultOpts()
	for _, op := range options {
		op.apply(&o)
	}
	s, err := schemaFor(reflect.TypeFor[T](), o)
	if err != nil {
		panic(err)
	}
	return s
}

func b1WriteYAML() (dir, path string) {
	dir, _ = os.MkdirTemp("", "b1")
	path = filepath.Join(dir, "app.yaml")
	os.WriteFile(path, []byte("name: svc\ndb:\n  host: db1\n  port: 5432\n"), 0o644)
	return dir, path
}

func b1Cleanup(dir string) { os.RemoveAll(dir) }


// b1ReadRequest parses one ordinary GET off the wire, which is the smallest
// honest unit of "a request happened".
func b1ReadRequest() {
	const wire = "GET /search?q=widgets&page=3&size=50&sort=name&desc=true&cursor=abc HTTP/1.1\r\nHost: x\r\n\r\n"
	r, err := http.ReadRequest(bufio.NewReader(strings.NewReader(wire)))
	if err != nil {
		panic(err)
	}
	_ = r.URL.Query()
}

func valueOfPtr[T any](p *T) reflect.Value { return reflect.ValueOf(p).Elem() }

func bHeapAlloc() int64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return int64(m.HeapAlloc)
}

// bKeysHeap is the live-heap cost of a Keys value holding n minted addresses,
// measured with the value still reachable so the GC cannot take it back.
func bKeysHeap(as *AddressSet, n int) int64 {
	before := bHeapAlloc()
	k, err := NewKeys(as, "env", bEnvKey)
	if err != nil {
		panic(err)
	}
	for i := range n {
		if _, err := k.Key(Path{}.Name("limits").Name(fmt.Sprintf("tenant%d", i))); err != nil {
			panic(err)
		}
	}
	after := bHeapAlloc()
	runtime.KeepAlive(k)
	return after - before
}

func valueOf[T any](v T) reflect.Value { return reflect.ValueOf(v) }

