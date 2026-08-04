package ferry

import "context"

// Source is the read half of a plane: one method, handed the addresses a
// compiled schema determined, handing back the function that opens a [Reader]
// over them.
//
// Binding is a phase of its own because the three pieces of state have three
// lifetimes. A Source holds driver config, which changes when you construct
// another one; an [OpenFunc] holds whatever the driver precomputed for this
// schema, such as its plane keys; and a [Reader] holds the plane's contents,
// which change on every load.
//
// Bind does no I/O, and the missing [context.Context] is how the type says so.
// A driver must succeed at Bind against a plane it cannot reach, and fail
// inside the open instead. What it may fail for is what it can see without
// touching the plane: an address its plane cannot name, or a key function that
// is not injective over the set. Both name the offending address with
// [ErrorAt], and both land before any backend call, which is what lets a
// plane-to-plane transfer be refused after zero calls rather than after reading
// the whole source.
//
// See [AddressSet] for what Bind is handed, and [NewKeys] for the helper that
// does the flattening and both checks.
type Source interface {
	Bind(addrs *AddressSet) (OpenFunc, error)
}

// Sink is the write half of a plane, and it is a separate interface from
// [Source] rather than the other half of one.
//
// A plane with no honest Dump - the process environment is the case - ships a
// [Source] and no Sink, so dumping to it is a compile error at the call site
// rather than a refusal at run time. The cost is that a driver serving both
// directions ships two types, since one type cannot have two Bind methods, so a
// round trip names the plane twice.
//
// A plane that is writable in principle but not right now refuses inside the
// [OpenWriterFunc], with an error wrapping [ErrReadOnly]: not at Bind, which
// does no I/O and cannot know, and not at the first write, which has already
// half-written the plane.
type Sink interface {
	Bind(addrs *AddressSet) (OpenWriterFunc, error)
}

// OpenFunc opens a [Reader] over the addresses a [Source] was bound to. It is
// called once per load, and may be called many times against one Bind.
//
// It may be called from many goroutines at once, because a caller may hold what
// [Source.Bind] returned and load through it concurrently. A driver that
// precomputes at Bind and only reads that afterwards already satisfies this;
// one that writes to what it closed over does not.
//
// Whether the driver fetches the whole plane here in one round trip or fetches
// nothing until the first Get is the driver's own choice, and core has no
// opinion: Bind already handed over the whole address set, so both are
// expressible with no extra interface.
type OpenFunc func(ctx context.Context) (Reader, error)

// OpenWriterFunc opens a [Writer] over the addresses a [Sink] was bound to. It
// is called once per dump, and may be called many times against one Bind.
//
// It may be called from many goroutines at once, on the same terms as
// [OpenFunc] and for the same reason: a caller may hold what [Sink.Bind]
// returned and dump through it concurrently.
//
// It is where a read-only refusal lands, wrapping [ErrReadOnly]: a KV with no
// write ACL, or a file sink over an unwritable directory, fails here after zero
// writes rather than half way through a walk over the user's struct.
type OpenWriterFunc func(ctx context.Context) (Writer, error)

// Reader is an open plane, answering one address at a time.
//
// Absence is a kind of the value rather than a second return value: an address
// the plane does not have is reported as the zero [Value], whose [Value.Kind]
// is [KindAbsent]. There is no sentinel error for it, so a driver's own
// "not found" cannot be confused with a real failure.
//
// Two rules bind an implementation. A non-nil error must reach the caller as an
// error and never as an absent value, which is the defect that turns a parse
// failure into a config silently loaded from nothing. And at a container
// address a driver returns absence or a null and nothing else, because a
// composite is read one element at a time and there is no group value for the
// container itself to hold.
//
// A Reader may also implement [Enumerator] and [Releaser]. Both are discovered
// by assertion and neither is required.
type Reader interface {
	Get(ctx context.Context, addr Path) (Value, error)
}

// Writer is an open plane being written to, one address at a time.
//
// Set is never called with an absent value: [KindAbsent] is a [Reader]-side
// kind, and an omitted address gets no Set call at all rather than a Set of
// nothing. A composite with no elements is written as a null at its own
// address, which is a value the plane holds and a different thing entirely.
//
// A Writer may also implement [Committer] and [Releaser]. Both are discovered
// by assertion and neither is required.
type Writer interface {
	Set(ctx context.Context, addr Path, v Value) error
}

// Releaser is io.Closer, and a [Reader] or [Writer] implements it when it holds
// a resource. It is not a name ferry invents: a driver wrapping a file or a
// connection satisfies it already.
//
// Close takes no context, because cleanup that can be cancelled is how the temp
// file leaks. It always runs, whether the walk succeeded or failed, and
// closed-without-[Committer.Commit] is the abort signal, so no driver is ever
// told that it failed.
//
// It is optional so that a driver with nothing to release implements nothing,
// rather than writing a `return nil` that reads exactly like a rollback
// somebody forgot.
type Releaser interface {
	Close() error
}

// Committer is implemented by a [Writer] whose writes are not durable until the
// end of a successful walk: a staging file sink, a transactional KV.
//
// Commit runs only when the walk succeeded; [Releaser.Close], if the writer has
// one, runs either way. Neither takes a cause, because there is no failure to
// report to a driver, only a commit that does not happen.
//
// It takes a [context.Context] where Close does not, because this is the actual
// I/O. It is separate from [Releaser] because the two do not co-occur: a
// transactional KV commits with nothing to release, and a lazy read side
// releases with nothing to commit.
type Committer interface {
	Commit(ctx context.Context) error
}

// Enumerator is implemented by a [Reader] whose plane can list what is under an
// address. It is how [Load] discovers the addresses that come from the value
// rather than from the type: a map's keys, a slice's length.
//
// It is optional, because a plane that cannot list is ordinary - a Vault token
// with read and no LIST is the usual case - so the two directions cover
// different address sets. [Dump] reaches every address always, since the value
// is in hand; Load reaches the addresses the type determines always, and the
// rest only through this interface.
//
// That is the array-versus-slice difference, seen from the driver's side: an
// array's element addresses come from its type, so an array loads from a source
// that cannot enumerate and a slice does not. Loading a slice or a map from a
// non-enumerating source is an error naming the field and the source, never a
// silently empty one. A null at the container's own address is a complete
// answer and needs no enumeration.
//
// Children returns addresses rather than names, because an address carries its
// [SegmentKind]: the plane says whether the container is a mapping or a
// sequence, instead of the caller guessing it from base-10 text.
type Enumerator interface {
	Children(ctx context.Context, prefix Path) ([]Path, error)
}
