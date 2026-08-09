package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"net/netip"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/kv"
)

// candidates are every spelling a root leaf might plausibly land on, seeded at
// once so a Load that hits one tells us which.
func envCandidates(v string) []string {
	return []string{"_=" + v, "~=" + v + "-from-tilde", "ROOT=" + v + "-from-ROOT"}
}

func queryCandidates(v string) url.Values {
	return url.Values{"_": {v + "-from-underscore"}, "~": {v}, "": {v + "-from-empty"}, "root": {v + "-from-root"}}
}

func kvSeed(v string) map[string]string {
	return map[string]string{"_": v, "~": v, "": v, "root": v}
}

func head(label string) {
	fmt.Printf("\n--- root type: %s ---\n", label)
}

func compileOnly[T any]() {
	defer guard("compile")

	err := ferry.Compile[T]()
	report("compile", err, "schema compiled")
}

func runAll() {
	head("int")
	compileOnly[int]()
	envLoad[int]("int", envCandidates("8080"))
	httpLoad[int](queryCandidates("8080"))
	yamlDump[int]("int", 8080)
	yamlLoad[int]("int", "8080\n")
	yamlLoad[int]("intmap", "~: 8080\n")
	yamlRoundTrip[int]("int", 8080)
	kvDump[int](8080)
	kvLoad[int](kvSeed("8080"))
	kvRoundTrip[int](8080)

	head("string")
	compileOnly[string]()
	envLoad[string]("string", envCandidates("hello"))
	httpLoad[string](queryCandidates("hello"))
	yamlDump[string]("string", "hello")
	yamlLoad[string]("string", "hello\n")
	yamlRoundTrip[string]("string", "hello")
	kvDump[string]("hello")
	kvRoundTrip[string]("hello")

	head("[]byte")
	compileOnly[[]byte]()
	envLoad[[]byte]("bytes", envCandidates("aGVsbG8="))
	yamlDump[[]byte]("bytes", []byte("hello"))
	yamlLoad[[]byte]("bytes", "aGVsbG8=\n")
	yamlRoundTrip[[]byte]("bytes", []byte("hello"))
	kvDump[[]byte]([]byte("hello"))
	kvRoundTrip[[]byte]([]byte("hello"))

	head("json.RawMessage")
	compileOnly[json.RawMessage]()
	envLoad[json.RawMessage]("raw", envCandidates(`{"a":1}`))
	envLoad[json.RawMessage]("raw-b64", envCandidates(`eyJhIjoxfQ==`))
	yamlDump[json.RawMessage]("raw", json.RawMessage(`{"a":1}`))
	yamlLoad[json.RawMessage]("raw", "{\"a\":1}\n")
	yamlRoundTrip[json.RawMessage]("raw", json.RawMessage(`{"a":1}`))
	kvDump[json.RawMessage](json.RawMessage(`{"a":1}`))
	kvRoundTrip[json.RawMessage](json.RawMessage(`{"a":1}`))

	head("netip.Addr")
	compileOnly[netip.Addr]()
	envLoad[netip.Addr]("addr", envCandidates("10.0.0.1"))
	httpLoad[netip.Addr](queryCandidates("10.0.0.1"))
	yamlDump[netip.Addr]("addr", netip.MustParseAddr("10.0.0.1"))
	yamlLoad[netip.Addr]("addr", "10.0.0.1\n")
	yamlRoundTrip[netip.Addr]("addr", netip.MustParseAddr("10.0.0.1"))
	kvDump[netip.Addr](netip.MustParseAddr("10.0.0.1"))
	kvRoundTrip[netip.Addr](netip.MustParseAddr("10.0.0.1"))

	head("*int")
	compileOnly[*int]()
	envLoad[*int]("ptrint", envCandidates("8080"))
	yamlDump[*int]("ptrint", ptr(8080))
	yamlLoad[*int]("ptrint", "8080\n")
	yamlRoundTrip[*int]("ptrint", ptr(8080))
	kvDump[*int](ptr(8080))
	kvRoundTrip[*int](ptr(8080))

	head("prefix and overwrite hazards (int root)")
	kvDumpPrefixed[int](8080)
	yamlDumpOver[int]("over", 8080, "keep: me\nother: 2\n")

	head("env sentinel collision with the real process environment")
	realEnvLoad[string]()

	head("driver option alternative: env.RootVar")
	rootVarLoad[int]("APP_PORT", []string{"APP_PORT=8080", "_=shell-noise"})
	rootVarLoad[int]("", []string{"APP_PORT=8080", "_=shell-noise"})

	head("control: struct{P int `ferry:\"p\"`}")
	compileOnly[ctl]()
	kvDump[ctl](ctl{P: 8080})
}

type ctl struct {
	P int `ferry:"p"`
}

func ptr[T any](v T) *T { return &v }

// kvRoundTrip dumps into a store and loads back out of the same store, which is
// how the actual key spelling is observed rather than guessed.
func kvRoundTrip[T any](v T) {
	defer guard("kv   roundtrip")

	store := newMemKV()

	sink, err := kv.NewSink(store)
	if err != nil {
		report("kv   roundtrip", err, "")

		return
	}

	if err := ferry.Dump(context.Background(), v, sink); err != nil {
		report("kv   roundtrip(dump)", err, "")

		return
	}

	src, err := kv.NewSource(store)
	if err != nil {
		report("kv   roundtrip", err, "")

		return
	}

	got, err := ferry.Load[T](context.Background(), src)
	report("kv   roundtrip", err, fmt.Sprintf("store=%s -> %#v", dumpKeys(store), got))
}
