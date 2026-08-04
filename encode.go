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
		return dumpWalk(ctx, w, sch, root)
	}

	// Whether this buffers or re-walks is an implementation choice ADR-0011
	// prices and does not fix, at 546 KB held against 521 ms spent over ten
	// thousand addresses. Buffering is the cheaper of the two on an ordinary
	// config struct, and nothing outside this function can tell which was
	// taken: the property is what the plane was asked, not how ferry got there.
	phase := &encodePhase{}
	if err := dumpWalk(ctx, phase, sch, root); err != nil {
		return err
	}

	return phase.flush(ctx, w)
}

// dumpWalk is one walk of one compiled schema in the Dump direction, against
// whichever Writer the policy above decided it writes to.
//
// The minted set is the walk's own and starts empty on every dump, because the
// addresses in it came from this value and the next dump has another.
func dumpWalk(ctx context.Context, w Writer, sch *schema, root reflect.Value) error {
	dir := dumpTo{w: w, addrs: sch.addrs, minted: map[Path]struct{}{}}

	return newWalker(dir).walk(ctx, spot{n: sch.root, v: root})
}

// encodePhase is the Writer the encode phase walks against: it accepts every
// address and reaches no plane, so a walk over it performs every encode, every
// address mint and every collision check, and touches nothing.
//
// It is a Writer rather than a second direction because the walk is written
// once. A phase that asked the direction to behave differently would be axis
// two of ADR-0010's duplication back again, at the one place Dump is allowed to
// have two behaviours.
//
// The buffer is shared mutable state behind the scheduler seam, in the same
// family as the walk's own minted set and presence counter and for the same
// reason: the serial scheduler core ships needs nothing, and a concurrent one
// has to carry every one of the three rather than only its errors. It is left
// standing on purpose, because a lock here would answer the parked concurrency
// question by accident.
type encodePhase struct{ writes []stagedWrite }

var _ Writer = (*encodePhase)(nil)

// stagedWrite is one address and the Value the walk encoded for it, held in the
// order the walk produced them so that the replay is the write order a staging
// sink would have seen.
type stagedWrite struct {
	at Path
	v  Value
}

// Set records what would have been written. It never fails, which is the whole
// point: every error a walk against this produces is one ferry could have
// reached without the plane.
func (p *encodePhase) Set(_ context.Context, at Path, v Value) error {
	p.writes = append(p.writes, stagedWrite{at: at, v: v})

	return nil
}

// flush hands the plane every encoded value, and aggregates its refusals.
//
// It does not stop at the first one, and the case that decides that is the same
// one Load's rule turns on: a token with write access to some paths and not
// others reports both refused addresses here and one under fail-fast, and
// taking that away on Dump alone would be an asymmetry between the directions
// about the same fact.
func (p *encodePhase) flush(ctx context.Context, w Writer) error {
	errs := make([]error, 0, len(p.writes))

	for _, write := range p.writes {
		if err := w.Set(ctx, write.at, write.v); err != nil {
			// The moment is the walk's rather than a phase of its own. The
			// failure is the plane refusing one address, which is what an
			// interleaved dump reports at exactly the same address, and a
			// moment nothing else uses would sort the same errors differently
			// depending on whether the sink could stage.
			errs = append(errs, fromDriver(momentWalk, write.at, err))
		}
	}

	return join(errs...)
}
