package addresskinds

// Dispatch cost of the three spellings: the S1/S3 typed call versus
// the S2 per-call kind check, over the same env lookup.

import "testing"

var (
	sinkV Value
	sinkE error
)

func benchEnv() (map[string]string, map[string]string) {
	environ := map[string]string{"DB_HOST": "x", "DB_PORT": "5432", "HOME": "/root"}
	keys := map[string]string{"/db/host": "DB_HOST", "/db/port": "DB_PORT"}
	return environ, keys
}

func BenchmarkGet_S1_Typed(b *testing.B) {
	set := NewAddressSet()
	set.AddLeaf("/db/host")
	environ, _ := benchEnv()
	d := bindEnv(environ, set)
	addr := LeafAddr{p: path{p: "/db/host"}}
	b.ReportAllocs()
	for b.Loop() {
		sinkV, sinkE = d.Get(addr)
	}
}

func BenchmarkGet_S2_KindChecked(b *testing.B) {
	environ, keys := benchEnv()
	addr := NewKindedPath("/db/host", PathLeaf)
	b.ReportAllocs()
	for b.Loop() {
		sinkV, sinkE = s2Get(environ, keys, addr)
	}
}

type s3env struct {
	environ map[string]string
	keys    map[string]string
}

func (d s3env) Get(addr Addr[leafK]) (Value, error) {
	if v, ok := d.environ[d.keys[addr.String()]]; ok {
		return Value{Kind: KindString, Text: v}, nil
	}
	return Value{}, nil
}

func BenchmarkGet_S3_Phantom(b *testing.B) {
	environ, keys := benchEnv()
	var d s3Reader = s3env{environ: environ, keys: keys}
	addr := NewAddr[leafK]("/db/host")
	b.ReportAllocs()
	for b.Loop() {
		sinkV, sinkE = d.Get(addr)
	}
}
