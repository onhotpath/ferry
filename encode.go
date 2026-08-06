package ferry

import (
	"context"
	"reflect"
)

// written is Dump's write policy, whole.
//
// > Dump encodes every address before it writes any of them. If anything fails
// > to encode, every such failure is reported and nothing is written. Otherwise
// > ferry writes, and aggregates the refusals.
//
// The property that buys is ADR-0004's own argument applied one layer in: if a
// Dump fails for a reason ferry could have known without touching the plane,
// the plane is untouched. ADR-0004 put ErrReadOnly at the open rather than at
// the first Set because "failing at open costs nothing, and failing at the
// first Set has already half-written the plane", and encoding is the last thing
// ferry can check with no plane in sight.
//
// The measurement that decides it separates two failures an earlier draft
// conflated. An encode failure is deterministic, per field, and happens before
// the write for that address, so hoisting it costs the plane nothing; a Set
// failure is the plane refusing, and it is the only one that can amplify
// writes. Over an eight-address struct holding two values with no
// representation, interleaved aggregation writes six addresses for a failure
// ferry could have known about first, and this writes none and reports both.
//
// ferry pays for the phase only where the sink cannot pay for it itself. A
// Committer stages, so Commit running only on success already leaves the plane
// untouched on failure, and interleaving then gives it a strictly better error
// set: it learns the plane's refusals and its own unrepresentable values in one
// run, where a flat sink learns the second only after the first is fixed. That
// is a reason to implement Committer, stated rather than left as folklore.
func written(ctx context.Context, w Writer, sch *schema, root reflect.Value) error {
	// Asked of the opened Writer by assertion, which is where ADR-0004 puts
	// every optional capability: a sink declares that it can stage by having
	// the method, and ferry adds no interface to ask the question with.
	if _, staging := w.(Committer); staging {
		_, err := dumpWalk(ctx, w, sch, root)

		return err
	}

	// Whether this buffers or re-walks is an implementation choice ADR-0011
	// prices and does not fix, at 546 KB held against 521 ms spent over ten
	// thousand addresses. Buffering is the cheaper of the two on an ordinary
	// config struct, and nothing outside this function can tell which was
	// taken: the property is what the plane was asked, not how ferry got there.
	//
	// The walk runs with no plane at all rather than against a stand-in that
	// accepts every address, so every error it produces is one ferry reached
	// without the plane, and what it encoded comes back in its outcome rather
	// than in a buffer every subtree appends to (ADR-0019).
	staged, err := dumpWalk(ctx, nil, sch, root)
	if err != nil {
		return err
	}

	return flush(ctx, w, staged.writes)
}

// dumpWalk is one walk of one compiled schema in the Dump direction, against
// whichever Writer the policy above decided it writes to, or against none at
// all where the encodes are staged in the outcome instead.
//
// What it hands back is the root's outcome: every address this value realised,
// and every encode it staged. Both came from this value, and the next dump has
// another, so neither outlives the call (ADR-0012).
func dumpWalk(ctx context.Context, w Writer, sch *schema, root reflect.Value) (outcome, error) {
	dir := dumpTo{w: w, addrs: sch.addrs}

	return newWalker(dir).walk(ctx, spot{n: sch.root, v: root})
}

// stagedWrite is one thing the walk has to say to the plane, held in the order
// the walk produced them so that the replay is the write order a staging sink
// would have seen.
//
// It is one type rather than two lists because the order across the two kinds
// of write is what has to be preserved: a container's own answer and the
// addresses beneath it are not interchangeable.
type stagedWrite struct {
	// leaf and v are a value write. Where at is non-nil this is a container
	// write instead, and p is what it says there.
	leaf LeafAddr
	v    Value

	at Container
	p  Presence
}

// play hands one staged write to the plane it was staged for.
//
// The moment is the walk's rather than a phase of its own. The failure is the
// plane refusing one address, which is what an interleaved dump reports at
// exactly the same address, and a moment nothing else uses would sort the same
// errors differently depending on whether the sink could stage.
func (s stagedWrite) play(ctx context.Context, w Writer) error {
	if s.at == nil {
		if err := w.Set(ctx, s.leaf, s.v); err != nil {
			return fromDriver(momentWalk, s.leaf.Path(), err)
		}

		return nil
	}

	return ensured(ctx, w, s.at, s.p)
}

// ensured replays a container-level write, refusing a plane that cannot spell
// one. The refusal is the same one an interleaved dump makes, worded once.
func ensured(ctx context.Context, w Writer, at Container, p Presence) error {
	e, ok := w.(Ensurer)
	if !ok {
		return newError(momentWalk, ErrPlane, at.Path(), unspellableMsg(p, w))
	}

	if err := e.Ensure(ctx, at, p); err != nil {
		return fromDriver(momentWalk, at.Path(), err)
	}

	return nil
}

// flush hands the plane every encoded value, and aggregates its refusals.
//
// It does not stop at the first one, and the case that decides that is the same
// one Load's rule turns on: a token with write access to some paths and not
// others reports both refused addresses here and one under fail-fast, and
// taking that away on Dump alone would be an asymmetry between the directions
// about the same fact.
func flush(ctx context.Context, w Writer, writes []stagedWrite) error {
	errs := make([]error, 0, len(writes))

	for _, write := range writes {
		errs = append(errs, write.play(ctx, w))
	}

	return join(errs...)
}
