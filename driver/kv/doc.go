// Package kv loads configuration from a Consul-shaped key-value store into a Go
// struct, and writes it back.
//
// # Experimental
//
// Neither the Go API here nor the way values are stored is settled yet, and
// either may change in a release that is not a new major version of this module.
// The reason is that this is Consul-shaped rather than Consul: the only [Client]
// in this repository is a test fake, so nothing here has been run against a real
// store, and the interfaces below are read off a specification rather than
// confirmed against a backend.
//
// # Bring your own client
//
// There is no Consul, etcd or Redis dependency here. Implement [Client]'s three
// methods over whatever store you have, and this package works against it.
//
//	src, err := kv.NewSource(store, kv.WithPrefix("app"))
//	cfg, err := ferry.Load[Config](ctx, src)
//
//	sink, err := kv.NewSink(store, kv.WithPrefix("app"))
//	err = ferry.Dump(ctx, cfg, sink)
//
// Keys come from the tags, joined with "/", so a nested db.host field reads
// app/db/host under that prefix.
//
// # Everything is bytes
//
// A store key carries no type, so 8080 in an int field and "8080" in a string
// field are stored alike and both read back as text, which each field parses
// with its own parser. Nothing is lost on the way into a Go value; what is lost
// is the store's own opinion about the value, which it never had.
//
// There is also no way to store "nothing", as distinct from an empty value. So a
// nil pointer, a nil slice and an empty map cannot be saved here, and saving one
// fails and names the field rather than writing an empty value that would read
// back as something else. A struct with all three reports all three, and the
// store is left untouched.
//
// # One call or one call per key
//
// By default each field is fetched as it is needed. [WithBatch] fetches the
// whole prefix in one call instead, when the load starts. Pick by what your
// store costs you: batch is one round trip and a consistent snapshot, and
// per-key reads only what your struct actually names, which is cheaper when the
// prefix holds far more than you want. The struct you get back is identical
// either way.
//
// # Whether you can write is discovered when the save starts
//
// Implement [ACL] and this package asks your client before writing anything. A
// client that does not implement it is assumed writable.
//
// A token with no write access fails before a single key is written rather than
// halfway through, and a token that can write some paths and not others reports
// every key it was refused rather than only the first.
//
// The design records behind these decisions are in docs/adr/.
package kv
