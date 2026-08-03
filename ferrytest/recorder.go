package ferrytest

import (
	"context"

	"github.com/onhotpath/ferry"
)

// The recording sink and the sink with nothing behind it. Neither is exported,
// and neither is a name ADR-0014 puts on the surface: what a caller gets is
// [Record] and the golden column of a [Proof], because a combinator handed out
// is a combinator somebody re-implements without the assertions below.
var (
	_ ferry.Sink      = (*recorder)(nil)
	_ ferry.Writer    = recWriter{}
	_ ferry.Committer = recCommitter{}
	_ ferry.Releaser  = recReleaser{}
	_ ferry.Committer = recBoth{}
	_ ferry.Releaser  = recBoth{}
	_ ferry.Sink      = nowhere{}
	_ ferry.Writer    = nowhere{}
)

// Record reports every address a value maps to and the boundary [ferry.Value]
// ferry encodes there, without a plane being touched at all.
//
// It is ADR-0001's schema-extraction pattern, and the mechanism is the whole
// answer: a dump into a sink that keeps what it was handed and writes it
// nowhere. Nothing else in ferry can answer the question, because what a struct
// maps to is decided by the compiler and the walk together and neither is
// reachable from outside.
//
//	mapped, err := ferrytest.Record(ctx, Config{})
//
// The value matters as well as its type, because a dump writes what the value
// holds: a zero value is the way to ask what the *type* maps to, and any other
// value answers what dumping that value would write.
//
// It takes the same [ferry.Option] list as [ferry.Dump] and for the same
// reason. A [ferry.TagKey] that the extraction could not see would answer about
// a schema no load will ever build.
//
// # Why it is generic where ADR-0014's call site is not
//
// ADR-0014 writes the call as Record(ctx, Config{}), which is exactly what this
// signature accepts: the type parameter is inferred from the value and no call
// site names it. It cannot be a plain `any` parameter, because [ferry.Dump]
// compiles the schema from its type parameter rather than from the dynamic type
// of what it was handed - deliberately, so that the schema and the walk see one
// type - so an `any` would compile the schema of `interface{}` and every call
// would be refused for naming no address.
//
// # Why the recording sink itself is not exported
//
// The combinator underneath is unexported, and that is a decision rather than
// an omission (ADR-0014). A [ferry.Writer] may also be a [ferry.Committer] and
// a [ferry.Releaser], both discovered by assertion, and a wrapper that forwards
// Set and forgets the other two silently turns a staging sink into one that
// never commits. Handing out the combinator invites exactly that wrapper to be
// written a second time, by somebody who has no reason to know the assertion is
// there.
func Record[T any](ctx context.Context, v T, opts ...ferry.Option) (map[ferry.Path]ferry.Value, error) {
	rec := recording(nowhere{})

	// Through the entry point, which is the point: what Record reports is what
	// a real dump would write, not what a second walk believes it would.
	if err := ferry.Dump(ctx, v, rec, opts...); err != nil {
		return nil, err
	}

	return rec.seen, nil
}

// recorder is the recording sink combinator: it keeps every Value ferry encoded
// against the address ferry encoded it at, and hands the write straight on to
// the sink underneath.
//
// It sits above a driver rather than beside one, and that placement is what it
// is for. ADR-0012's Observe is Load-side only, so a wrapping sink is the only
// position from which what ferry *encoded* is visible before a driver has
// spelled it - which is what the golden column of a [Proof] asserts, and what a
// round trip structurally cannot see, since a round trip composes a spelling
// with its own inverse.
type recorder struct {
	inner ferry.Sink
	seen  map[ferry.Path]ferry.Value
}

// recording wraps a sink. The map is the recorder's own, so two recordings of
// one sink do not share an answer.
func recording(sink ferry.Sink) *recorder {
	return &recorder{inner: sink, seen: map[ferry.Path]ferry.Value{}}
}

// Bind hands the address set straight through, because a recorder has no key
// function of its own and nothing it could refuse: an address the plane
// underneath dislikes is still that plane's refusal, and it must arrive
// unchanged.
func (r *recorder) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	open, err := r.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Writer, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return wrapWriter(w, r.seen), nil
	}, nil
}

// wrapWriter puts the recording in front of a writer while keeping the optional
// interfaces that writer had.
//
// The four shells are the honest spelling and a single shell implementing
// Commit and Close unconditionally is not, even though core's own entry point
// could not tell them apart: [ferry.Committer] and [ferry.Releaser] are
// discovered by assertion, so a wrapper that always answers yes reports that a
// sink stages when it does not, and a conformance suite asking a driver what it
// implements would be asking the wrapper instead.
func wrapWriter(w ferry.Writer, seen map[ferry.Path]ferry.Value) ferry.Writer {
	rec := recWriter{inner: w, seen: seen}
	c, commits := w.(ferry.Committer)
	r, releases := w.(ferry.Releaser)

	switch {
	case commits && releases:
		return recBoth{recWriter: rec, commit: c, release: r}
	case commits:
		return recCommitter{recWriter: rec, commit: c}
	case releases:
		return recReleaser{recWriter: rec, release: r}
	default:
		return rec
	}
}

// recWriter is the recording itself, and the only one of the four shells that
// adds behaviour.
type recWriter struct {
	inner ferry.Writer
	seen  map[ferry.Path]ferry.Value
}

// Set keeps what ferry encoded and then writes it.
//
// It records first, because the record is a statement about ferry rather than
// about the plane: a driver that refuses the write still had that Value handed
// to it, and a golden column that went blank whenever a plane said no would
// report the wrong half of the failure.
func (w recWriter) Set(ctx context.Context, addr ferry.Path, v ferry.Value) error {
	w.seen[addr] = v

	return w.inner.Set(ctx, addr, v)
}

// recCommitter is the shell for a staging sink.
type recCommitter struct {
	recWriter

	commit ferry.Committer
}

// Commit is the wrapped writer's, unchanged.
func (w recCommitter) Commit(ctx context.Context) error { return w.commit.Commit(ctx) }

// recReleaser is the shell for a sink holding a resource.
type recReleaser struct {
	recWriter

	release ferry.Releaser
}

// Close is the wrapped writer's, unchanged.
func (w recReleaser) Close() error { return w.release.Close() }

// recBoth is the shell for a sink that stages and holds a resource, which is
// the ordinary shape of a file sink writing through a temporary.
type recBoth struct {
	recWriter

	commit  ferry.Committer
	release ferry.Releaser
}

// Commit is the wrapped writer's, unchanged.
func (w recBoth) Commit(ctx context.Context) error { return w.commit.Commit(ctx) }

// Close is the wrapped writer's, unchanged.
func (w recBoth) Close() error { return w.release.Close() }

// nowhere is the sink with no plane behind it: it accepts every address and
// keeps none of them.
//
// It is what makes [Record] answer with no plane reachable. A recorder over it
// is a dump that runs the real compiler and the real walk and lands in a map,
// so nothing is opened, nothing is written and nothing has to be cleaned up.
//
// It implements neither [ferry.Committer] nor [ferry.Releaser], because it has
// nothing to commit and nothing to release, which is also what makes it the
// case [wrapWriter] leaves unwrapped.
type nowhere struct{}

// Bind accepts any address set and does no I/O, which is the trivial case of
// ADR-0004's rule that Bind must succeed against a plane it cannot reach.
func (nowhere) Bind(*ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	return func(context.Context) (ferry.Writer, error) { return nowhere{}, nil }, nil
}

// Set discards. The recorder above it is what kept the value.
func (nowhere) Set(context.Context, ferry.Path, ferry.Value) error { return nil }
