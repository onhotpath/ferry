package kv

import "context"

// Client is the store this driver talks to, and the whole of what it needs from
// a backend: read one key, list what is under a folder, write one key.
//
// It is an interface rather than a dependency, so an adapter over consul/api,
// etcd, a Redis hash, a table with two columns or a test double is a few lines
// and this package never learns which of them it is talking to.
//
// Three things an implementer owns.
//
// Absence is a result and not an error. Get reports a key the store does not
// hold with found false and a nil error, so that a backend's own not-found stays
// distinguishable from a real failure. A zero-length value is a value the store
// holds, and it arrives as an empty string rather than as absence.
//
// Cancellation is yours. The driver hands its caller's context to every call and
// adds no deadline of its own, so a client that ignores the context is the only
// thing standing between a cancelled load and a blocked one.
//
// Safety for use from many goroutines at once is yours, and it is ordinary
// rather than exotic. A source or a sink is constructed once and a binding is
// held for the life of a process, so one client is reached from wherever a load
// or a save happens. A real backend's own client is usually safe already, and a
// test double over a plain map is usually not.
//
// One load can reach it from many goroutines too. This package's reader tells
// ferry that it tolerates overlapping calls, so a caller who sets
// ferry.MaxConcurrency has several of a single load's reads in your client at
// once. That is the same obligation as the paragraph above rather than a new
// one, and it is why ferry.MaxConcurrency is the caller's to set: how much
// overlap your store will take is a fact about your store and your token.
type Client interface {
	// Get answers with the bytes stored at key, and with found false where the
	// store does not hold it.
	Get(ctx context.Context, key string) (value []byte, found bool, err error)

	// List answers with every pair whose key begins with prefix, keyed by the
	// whole key rather than by the part below the prefix. An empty prefix is
	// the whole store.
	//
	// It is what one round trip means here: a load told to batch calls this
	// once and answers every field out of what it got back.
	List(ctx context.Context, prefix string) (map[string][]byte, error)

	// Put stores value at key, creating it or replacing what is there.
	Put(ctx context.Context, key string, value []byte) error
}

// ACL is implemented by a [Client] whose credentials can be asked, before
// anything is written, whether they permit a write.
//
// It is optional. A [Client] that does not implement it is assumed writable
// everywhere, which is the honest answer for a store with no access control.
//
// A save asks twice and about two different things. It asks about the whole
// prefix when it starts, which is where a token with no write access at all is
// refused, before a single key has been written. It then asks about each key
// before staging it, which is what lets a token that can write some paths and
// not others report every key it was refused rather than only the first.
//
// A nil error means the write is permitted. Any other error is returned to the
// caller under ferry's own wrapper, so your own sentinel stays reachable through
// errors.Is.
type ACL interface {
	// CanWrite reports whether these credentials permit a write at key. The
	// empty key asks about the store as a whole, which is what a sink with no
	// prefix asks at open.
	CanWrite(ctx context.Context, key string) error
}
