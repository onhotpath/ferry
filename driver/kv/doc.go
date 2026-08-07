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
// There is no Consul, etcd or Redis dependency here. Implement [Client]'s four
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
// # A save replaces what it wrote last time
//
// A store holds whatever was put in it, so a list that lost an element and a map
// that lost a key would otherwise leave the previous save's keys behind, and the
// next load would read them back as though they were still configured.
//
// They do not. Before a save writes a list or a map, it tells the store to
// forget everything under that address, and at commit time the keys it did not
// write are removed. Saving a one-element Tags over a store holding app/tags/0,
// app/tags/1 and app/tags/2 leaves app/tags/0 and nothing else.
//
// Only a list or a map is replaced this way. A field your value omits is not
// written and is not removed either, so silence never deletes anything.
//
// # Everything is bytes
//
// A store key carries no type, so 8080 in an int field and "8080" in a string
// field are stored alike and both read back as text, which each field parses
// with its own parser. Nothing is lost on the way into a Go value; what is lost
// is the store's own opinion about the value, which it never had.
//
// There is also no way to store "nothing", as distinct from an empty value. So
// four shapes cannot be saved here: a nil pointer, a nil slice, an empty map,
// and a non-nil pointer to a struct whose every field was omitted. Saving one
// fails and names the field rather than writing an empty value that would read
// back as something else, a struct with all four reports all four, and the store
// is left untouched.
//
// The failures are two classes and a caller matching on one of them misses the
// other. A null at a leaf is this package's own refusal and carries
// [ferry.ErrValue]. The other three are a container speaking at its own address,
// which this package supplies no way to write, so ferry refuses them for it and
// they carry [ferry.ErrPlane]. Match both, or match neither and read the error.
//
// # Or a store that holds payloads
//
// [Raw] says the store's values are bytes rather than text, so a value crosses
// ferry's boundary as the bytes the store holds and nothing in the middle turns
// them into a string:
//
//	src, err := kv.NewSource(store, kv.WithPrefix("app"), kv.Raw())
//
// It is symmetric: a sink built with it writes a []byte field's bytes exactly as
// they are, through the same spelling, so the two directions cannot drift apart.
//
// It is a fact about the whole store, because a key carries no type for a driver
// to consult. Once it is declared every value is a payload, so an int, a string
// or a duration field over the same store is a value the field cannot take:
// declare it for a store whose values are payloads, and read the fields that are
// not through a source of their own.
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
// # One load, several reads at once
//
// A reader from this package tells ferry that it tolerates overlapping calls,
// so a caller who sets ferry.MaxConcurrency has several of one load's reads in
// flight together. It declares no bound of its own: how much overlap a store
// will take is a fact about that store and that token, and the caller is the
// one holding both.
//
// What it costs you is what [Client] already asks for, which is that your
// client is safe for use from many goroutines at once. Nothing else in one
// open is shared: a batch open's snapshot is read and never written, and this
// package serialises the key function that turns an address into a store key.
//
// A batch open has nothing to overlap - it already made its one call - so the
// budget buys nothing there, and the struct you get back is the same either
// way, under any budget.
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
