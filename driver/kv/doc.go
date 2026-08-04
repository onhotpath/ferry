// Package kv is ferry's driver for a Consul-shaped key-value store: opaque byte
// values under slash-separated keys, reached through a client interface this
// package declares rather than a client it imports.
//
// Consul-shaped rather than Consul, so the exact backend stays open (ADR-0004).
// A caller wraps whatever store they have in [Client] - Consul, etcd, a Redis
// hash, a table with two columns - and the driver never learns which.
//
// # What this plane can carry, and what it refuses
//
// A store key holds bytes and nothing else, so the boundary kind does not
// survive a write: a Number spelled "8080" and a String spelled "8080" are
// stored alike and both read back as a String. Nothing is lost on the way into
// a Go value, because every leaf takes a String and parses it with its own
// parser; what is lost is the plane's own opinion about the value, which this
// plane never had.
//
// It has no null, so the values a walk writes as a Null at a container address
// are refused loudly rather than stored as something else: a nil pointer, a nil
// composite and an empty composite. Storing an empty value for them would make
// "this field is nil" and "this field is the empty string" one observation,
// which is the silent mangling ADR-0005's declaration rule exists to prevent.
//
// # Batch and lazy, which ferry never learns either
//
// Bind is handed the whole address set before any I/O, so an open may fetch
// every value in one call or fetch nothing at all, and the difference is
// [WithBatch]. Measured on a three-address schema: three backend calls lazily
// and one in batch, with identical results. That is why ferry has no
// Snapshotter interface - it would be a second contract for a choice the driver
// already makes (ADR-0004).
//
// # The axes this driver owns
//
// It is on ferry's first-party list because it is the only driver reaching five
// axes of the driver contract at once (ADR-0004): real I/O with cancellation
// and partial failure, opaque bytes, batch versus lazy inside one open, a sink
// that is read-only as a runtime fact rather than a schema fact, and a
// committer with nothing to release.
package kv
