package kv

import "context"

// Client is the store this driver talks to, and the whole of what it needs from
// a backend: read one key, list what is under a folder, write one key.
//
// It is an interface rather than a dependency because ADR-0004 admits this
// driver as "Consul-shaped rather than Consul, so the exact backend stays
// open". Three methods is what the driver actually uses, so an adapter over
// consul/api, etcd or a test double is small enough that nobody is tempted to
// reach past it; anything richer here would be this package deciding what a
// key-value store is.
//
// # Absence is a result and not an error
//
// Get reports a key the store does not hold with found false and a nil error,
// for the reason ferry's own boundary reports it as a kind: a sentinel error
// makes a backend's own not-found indistinguishable from a real failure, and
// this driver would then have to guess which of the two it was holding. A
// zero-length value is a value the store holds, and it arrives as a String with
// no bytes rather than as absence (ADR-0004).
//
// # Every method takes a context
//
// The driver hands its caller's context to every call and adds no deadline of
// its own. A client that honours cancellation makes the driver honour it; a
// client that does not is the only thing standing between a cancelled Load and
// a blocked one, which is a property of the client and is stated here so that
// an implementer knows it is theirs.
type Client interface {
	// Get answers with the bytes stored at key, and with found false where the
	// store does not hold it.
	Get(ctx context.Context, key string) (value []byte, found bool, err error)

	// List answers with every pair whose key begins with prefix, keyed by the
	// whole key rather than by the part below the prefix. An empty prefix is
	// the whole store.
	//
	// It is what one round trip means here: an open told to batch calls this
	// once and answers every address out of what it got back.
	List(ctx context.Context, prefix string) (map[string][]byte, error)

	// Put stores value at key, creating it or replacing what is there.
	Put(ctx context.Context, key string, value []byte) error
}

// ACL is implemented by a [Client] whose credentials can be asked, before
// anything is written, whether they permit a write.
//
// It is optional and discovered by assertion, which is where ADR-0004 puts
// every capability a plane may or may not have. A store with no access control
// implements nothing and is writable everywhere, which is the honest answer for
// it; a required method would be `return nil` in every such client, and a
// no-op that cannot be told from an unimplemented check is exactly what
// ADR-0004 refuses for Close.
//
// It is asked twice per dump and about two different things. The sink asks
// about its own prefix inside the open, which is where a read-only refusal has
// to land: not at Bind, which does no I/O and so cannot know, and not at the
// first write, which has already half-written the plane. The writer asks about
// one key before staging it, which is what lets a token with write access to
// some paths and not others report every address it refused rather than the
// first.
//
// A nil error means the write is permitted. The error is returned to the caller
// under ferry's own wrapper, so a client's own sentinel stays reachable through
// errors.Is.
type ACL interface {
	// CanWrite reports whether these credentials permit a write at key. The
	// empty key asks about the store as a whole, which is what a sink with no
	// prefix asks at open.
	CanWrite(ctx context.Context, key string) error
}
