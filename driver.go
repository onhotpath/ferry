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

// Reader is an open plane, answering one leaf at a time.
//
// It is asked only about a [LeafAddr], which is an address a value can be at.
// A container's own address is a [SectionAddr] or a [CompositeAddr], so asking
// this method about one does not compile, and a plane that happens to hold
// something under a container's own name can no longer have that something
// mistaken for the container's value.
//
// Absence is a kind of the value rather than a second return value: an address
// the plane does not have is reported as the zero [Value], whose [Value.Kind]
// is [KindAbsent]. There is no sentinel error for it, so a driver's own
// "not found" cannot be confused with a real failure.
//
// One rule binds an implementation: a non-nil error must reach the caller as an
// error and never as an absent value, which is the defect that turns a parse
// failure into a config silently loaded from nothing. A plane holding a
// container where the schema says leaf is a mismatch, and a driver refuses it
// with the address and what the plane holds rather than answering absence.
//
// A Reader may also implement [Prober], [Enumerator] and [Releaser]. All three
// are discovered by assertion and none is required.
type Reader interface {
	Get(ctx context.Context, addr LeafAddr) (Value, error)
}

// Writer is an open plane being written to, one leaf at a time.
//
// Set is never called with an absent value: [KindAbsent] is a [Reader]-side
// kind, and an omitted address gets no Set call at all rather than a Set of
// nothing.
//
// It is asked only about a [LeafAddr]. What a dump has to say at a container's
// own address - that the container is there and holds nothing, or that it is
// null - goes to [Ensurer], because a plane that cannot spell either of those
// should refuse rather than receive a write it will mis-store.
//
// A Writer may also implement [Ensurer], [Unsetter], [Preparer], [Committer]
// and [Releaser]. All five are discovered by assertion and none is required.
type Writer interface {
	Set(ctx context.Context, addr LeafAddr, v Value) error
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

// LeafRedirect is what a [Reader.Get] returns when the plane holds a link at
// this address and the value lives at another one.
//
// It is returned as an error and it is not a failure. It is a control answer,
// in the shape fs.SkipDir has, so a value stays the six kinds it always was and
// no caller has to handle a seventh that means "look over there". Match it with
// errors.As:
//
//	func (r reader) Get(ctx context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
//	    if to, linked := r.linkAt(addr); linked {
//	        return ferry.Value{}, &ferry.LeafRedirect{Target: to}
//	    }
//	    ...
//	}
//
// Report one hop. The chain is followed for you, the addresses already visited
// are kept, and a cycle is refused naming the address it closes through.
//
// The target is an address you were handed, because nothing outside ferry
// builds one, so a link whose target this schema does not name cannot be
// reported and stays yours to resolve or to refuse.
type LeafRedirect struct {
	// Target is where the value lives.
	Target LeafAddr
}

// Error names the address the value lives at. It reads as a statement rather
// than as a failure, because it is one.
func (r *LeafRedirect) Error() string { return "the value lives at " + r.Target.String() }

// Prober is implemented by a [Reader] whose plane can say whether a container
// is there. It answers about a [Container], which is a [SectionAddr] or a
// [CompositeAddr] and never a leaf.
//
// Return [SectionPresent], [SectionAbsent] or [SectionNull]. Absence means the
// plane does not have the address at all, a null means it has it and holds its
// own null there, and present means it has it and holds a container, which may
// be an empty one.
//
// A plane with links has a fourth answer, [SectionAt], which says the address
// names a place that lives somewhere else. Report one hop; the chain, the
// addresses already visited and the refusal of a cycle are handled for you.
//
// It is optional, in the same idiom as [Releaser], because a plane that cannot
// list often cannot answer this either. A source implementing neither this nor
// [Enumerator] loads the leaves the type determines and nothing else: a nil
// pointer stays nil where the plane is silent beneath it, and a slice or a map
// is a refusal naming the field and the source.
type Prober interface {
	Probe(ctx context.Context, addr Container) (SectionInfo, error)
}

// Enumerator is implemented by a [Reader] whose plane can list what is under a
// composite. It is how [Load] discovers the addresses that come from the value
// rather than from the type: a map's keys, a slice's length.
//
// It is asked only about a [CompositeAddr], so it cannot be asked to list a
// leaf or a section. A section's children come from the type and are never
// enumerated, which is the array-versus-slice difference seen from the driver's
// side: an array loads from a source that cannot enumerate and a slice does
// not. Loading a slice or a map from a non-enumerating source is an error
// naming the field and the source, never a silently empty one.
//
// Children returns the segments the plane holds immediately under the address,
// each a [NameSegment] or an [IndexSegment]. The driver says how the plane
// spells its members and the schema types the child, so a driver never
// constructs an address. A [Name] under a sequence, or an [Index] under a
// mapping, is refused with the segment named.
//
// The order is the plane's own, and a plane with no defined order documents the
// order it mints in.
type Enumerator interface {
	Children(ctx context.Context, addr CompositeAddr) ([]Segment, error)
}

// Ensurer is implemented by a [Writer] whose plane can spell a container at the
// container's own address: one that is present and holds nothing, and one that
// is null.
//
// [Dump] calls it where the value has nothing to say beneath a container: a nil
// pointer and an empty slice or map write [PresenceNull], and a realised
// section that emitted no child write writes [PresencePresent]. It is never
// called with [PresenceAbsent], because an address that is not written gets no
// call at all.
//
// It is optional, and a plane with no spelling for a container implements
// nothing rather than storing something misleading. Dumping a value that needs
// one to a [Writer] without it is refused, naming the address and the plane.
type Ensurer interface {
	Ensure(ctx context.Context, addr Container, p Presence) error
}

// Unsetter is implemented by a [Writer] whose plane can forget an address and
// everything held beneath it.
//
// [Dump] calls it at a slice's or a map's own address, and it is what makes a
// dump a replacement of that composite rather than an addition to it: a list
// that lost its third element, or a map that lost a key, leaves nothing of the
// previous dump behind for the next load to read back. It is the only deletion
// a dump ever performs. A field the value omits is not written and is not
// removed either, so silence never deletes anything.
//
// Unset arrives before the writes beneath that address, so a member this dump
// does write is written after it was forgotten and survives. A sink that stages
// has to keep that order across its own Commit, which for a store that deletes
// by key means resolving what to forget against what the dump staged rather
// than deleting first and hoping the ordering holds.
//
// It is idempotent and takes no view of what is there: an address the plane
// does not hold is not a failure.
//
// It is optional, and a Writer without one is refused nothing. Such a plane is
// additive at a composite, which is a property of that plane rather than of the
// value. A sink that replaces its whole plane on every dump - a file written
// through a temporary and swapped into place - already forgets by construction
// and has nothing to implement.
type Unsetter interface {
	Unset(ctx context.Context, addr CompositeAddr) error
}

// Preparer is implemented by a [Writer] that wants to see the addresses a dump
// determined from the value before the dump writes any of them.
//
// The set is the addresses that come from the value and not from the type: a
// map key, a sequence index. Everything the type determined arrived at
// [Sink.Bind] and is not repeated here. It is sorted, it is yours to keep, and
// a value holding no slice and no map produces an empty one rather than no
// call.
//
// Prepare runs once per dump, after every value has been encoded and before the
// first write. Returning nil lets the writes proceed. Refusing stops the dump
// where it stands, so nothing is written at all - which is what it is for: a
// plane that renders two of these addresses to one key loses one of them, and
// without this it can only say so from inside the write that carried the
// second, by which time the writes before it have landed.
//
// Name the offending addresses with [ErrorAt], as a key function does, so that
// each refusal is reported against the address it belongs to.
//
// It is optional, and a Writer without one is asked nothing and refused
// nothing. It is also not asked of a [Committer], which already leaves the
// plane untouched when a dump fails by not committing, and which is written to
// as the walk runs - so there is no moment at which the whole set is known and
// the plane still holds nothing.
type Preparer interface {
	Prepare(ctx context.Context, addrs []Path) error
}
