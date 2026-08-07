package ferrytest

import (
	"context"
	"fmt"

	"github.com/onhotpath/ferry"
)

// The recording sink and the sink with nothing behind it. Neither is exported,
// and neither is a name ADR-0014 puts on the surface: what a caller gets is
// [Record] and the golden column of a [Proof], because a combinator handed out
// is a combinator somebody re-implements without the assertions below.
var (
	_ ferry.Sink      = (*recorder)(nil)
	_ ferry.Writer    = recWriter{}
	_ ferry.Writer    = shellPlain{}
	_ ferry.Committer = shellCommitter{}
	_ ferry.Releaser  = shellReleaser{}
	_ ferry.Committer = shellBoth{}
	_ ferry.Releaser  = shellBoth{}
	_ ferry.Sink      = nowhere{}
	_ ferry.Writer    = nowhere{}
)

// Record reports every address a value maps to and the boundary [ferry.Value]
// ferry encodes there, without a plane being touched at all.
//
// It answers "what does my struct actually map to?", which nothing else can:
// where a field lands is decided by ferry's tag reading and its walk together,
// and neither is reachable from outside. Under the hood it is a dump into a sink
// that keeps what it was handed and writes it nowhere, so what comes back is
// what a real dump would have written.
//
//	mapped, err := ferrytest.Record(ctx, Config{})
//
// The value matters as well as its type, because a dump writes what the value
// holds. A zero value asks what the type maps to; any other value answers what
// dumping that value would write.
//
// It takes the same [ferry.Option] list as [ferry.Dump], and it has to: a
// [ferry.TagKey] this call could not see would answer about a schema no load
// will ever build.
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
func wrapWriter(w ferry.Writer, seen map[ferry.Path]ferry.Value) ferry.Writer {
	return shellWriter(recWriter{inner: w, seen: seen}, w)
}

// shellWriter gives front the optional interfaces w has, and no others.
//
// The shells are the honest spelling and a single shell implementing
// Commit and Close unconditionally is not, even though core's own entry point
// could not tell them apart: [ferry.Committer] and [ferry.Releaser] are
// discovered by assertion, so a wrapper that always answers yes reports that a
// sink stages when it does not, and a conformance suite asking a driver what it
// implements would be asking the wrapper instead.
//
// It is written once and shared, because [Driver]'s lifecycle case wraps a
// third-party writer for exactly the same reason [Record] does, and two
// spellings of this dance is two places for the shell nobody tested to be wrong.
//
// Which interfaces the shell carries is always w's answer. Which object the call
// goes to is front's where front has the method, so a wrapper that counts a
// Commit still leaves the driver deciding whether there is one to count.
func shellWriter(front, w ferry.Writer) ferry.Writer {
	var c caps

	c.commit, _ = commitsThrough(front, w)
	c.release, _ = releasesThrough(front, w)
	c.ensure, _ = ensuresThrough(front, w)

	if c.ensure != nil {
		return ensuringShell(front, c)
	}

	switch {
	case c.commit != nil && c.release != nil:
		return shellBoth{Writer: front, Committer: c.commit, Releaser: c.release}
	case c.commit != nil:
		return shellCommitter{Writer: front, Committer: c.commit}
	case c.release != nil:
		return shellReleaser{Writer: front, Releaser: c.release}
	default:
		return shellPlain{Writer: front}
	}
}

// caps is which of the three optional interfaces the shell carries, and which
// object each call goes to. A nil member is a capability the wrapped writer
// does not have, so the shell must not claim it either.
type caps struct {
	commit  ferry.Committer
	release ferry.Releaser
	ensure  ferry.Ensurer
}

// ensuringShell is [shellWriter]'s other four arms, split out because the three
// optional interfaces make eight combinations and one function listing all
// eight is over the nesting the linter allows.
func ensuringShell(front ferry.Writer, c caps) ferry.Writer {
	switch {
	case c.commit != nil && c.release != nil:
		return shellAll{Writer: front, Committer: c.commit, Releaser: c.release, Ensurer: c.ensure}
	case c.commit != nil:
		return shellCommitEnsurer{Writer: front, Committer: c.commit, Ensurer: c.ensure}
	case c.release != nil:
		return shellReleaseEnsurer{Writer: front, Releaser: c.release, Ensurer: c.ensure}
	default:
		return shellEnsurer{Writer: front, Ensurer: c.ensure}
	}
}

// commitsThrough answers whether the shell is a [ferry.Committer] - which is w's
// answer, never front's - and which of the two the Commit goes to.
func commitsThrough(front, w ferry.Writer) (ferry.Committer, bool) {
	inner, ok := w.(ferry.Committer)
	if !ok {
		return nil, false
	}

	if outer, ok := front.(ferry.Committer); ok {
		return outer, true
	}

	return inner, true
}

// releasesThrough is [commitsThrough] for [ferry.Releaser].
func releasesThrough(front, w ferry.Writer) (ferry.Releaser, bool) {
	inner, ok := w.(ferry.Releaser)
	if !ok {
		return nil, false
	}

	if outer, ok := front.(ferry.Releaser); ok {
		return outer, true
	}

	return inner, true
}

// ensuresThrough is [commitsThrough] for [ferry.Ensurer], which is the
// capability a container's own address is written through (ADR-0016). A shell
// that dropped it would turn every nil pointer in a dump into a refusal the
// driver never made.
func ensuresThrough(front, w ferry.Writer) (ferry.Ensurer, bool) {
	inner, ok := w.(ferry.Ensurer)
	if !ok {
		return nil, false
	}

	if outer, ok := front.(ferry.Ensurer); ok {
		return outer, true
	}

	return inner, true
}

// recWriter is the recording itself: it is what [wrapWriter] puts in front of a
// driver's writer, and the shells above are what give it the driver's own
// answer to the two optional interfaces.
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
func (w recWriter) Set(ctx context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	w.seen[addr.Path()] = v

	return w.inner.Set(ctx, addr, v)
}

// Ensure keeps what a container's own address was told and then forwards it.
//
// A null at a container address used to arrive here as a Set carrying
// ferry.Null, so it is recorded as one and a caller reading a dump sees exactly
// what it saw before (ADR-0016). A section written as present carries no value,
// so there is nothing for the map to hold and it is forwarded and not recorded.
func (w recWriter) Ensure(ctx context.Context, addr ferry.Container, p ferry.Presence) error {
	if p == ferry.PresenceNull {
		w.seen[addr.Path()] = ferry.Null
	}

	return ensureThrough(ctx, w.inner, addr, p)
}

// ensureThrough forwards a container write to a writer that can take one, and
// refuses for a writer that cannot, in the same words core uses.
func ensureThrough(ctx context.Context, w ferry.Writer, addr ferry.Container, p ferry.Presence) error {
	e, ok := w.(ferry.Ensurer)
	if !ok {
		return ferry.ErrorAt(addr.Path(), fmt.Errorf("%w: this plane cannot spell a container at its own "+
			"address", ferry.ErrPlane))
	}

	return e.Ensure(ctx, addr, p)
}

// shellPlain is the shell for a writer that neither stages nor holds a
// resource, and it is not the no-op it looks like.
//
// The front is what is wrapped, and a front may carry methods of its own: the
// lifecycle case's counting writer declares Commit and Close unconditionally,
// because whether the shell around it has them is this function's decision and
// not the front's. Handing that front back bare would tell core that a driver
// stages when it does not, which is the exact misreport the four shells exist to
// prevent. Embedding the interface promotes Set and nothing else.
type shellPlain struct {
	ferry.Writer
}

// The seven shells that carry at least one optional interface.
//
// Each embeds the interfaces its combination has, so the promoted method is the
// one [shellWriter] resolved and there is no forwarding body to get wrong. The
// three optional interfaces make eight combinations and every one of them has a
// name here, because a combination with no shell is a capability silently
// dropped from a wrapped driver.
type (
	// shellCommitter is the shell for a staging sink.
	shellCommitter struct {
		ferry.Writer
		ferry.Committer
	}

	// shellReleaser is the shell for a sink holding a resource.
	shellReleaser struct {
		ferry.Writer
		ferry.Releaser
	}

	// shellEnsurer is the shell for a sink that can spell a container at its
	// own address.
	shellEnsurer struct {
		ferry.Writer
		ferry.Ensurer
	}

	// shellBoth is the shell for a sink that stages and holds a resource, which
	// is the ordinary shape of a file sink writing through a temporary.
	shellBoth struct {
		ferry.Writer
		ferry.Committer
		ferry.Releaser
	}

	shellCommitEnsurer struct {
		ferry.Writer
		ferry.Committer
		ferry.Ensurer
	}

	shellReleaseEnsurer struct {
		ferry.Writer
		ferry.Releaser
		ferry.Ensurer
	}

	shellAll struct {
		ferry.Writer
		ferry.Committer
		ferry.Releaser
		ferry.Ensurer
	}
)

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
func (nowhere) Set(context.Context, ferry.LeafAddr, ferry.Value) error { return nil }

// Ensure discards, so that a value with a nil section in it records rather than
// refusing for want of a plane that was never there.
func (nowhere) Ensure(context.Context, ferry.Container, ferry.Presence) error { return nil }
