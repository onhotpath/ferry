package ferry

import (
	"context"
	"reflect"
)

// sched runs a batch of sibling tasks and reports what they collectively failed
// at. It is the seam a concurrent mode goes behind, and it is why the walk is
// written once: a concurrent mode is a second scheduler and never a second walk
// (ADR-0010).
//
// The batch is per container rather than per leaf, which is what prices the
// seam honestly: one indirect call for a struct's whole field list, measured at
// 1141 ns against 1079 ns for the identical loop inlined.
//
// It is unexported, core ships exactly one implementation, and no exported name
// takes or returns one. That is deliberate: whether ferry ever walks
// concurrently is an open question, and an importer able to select a scheduler
// today would answer it by accident. The hazard that comes with the seam is
// stated in ADR-0010 rather than fixed here - ADR-0006's presence bit is shared
// mutable state across it, reproduced under -race - because a seam that looks
// like a drop-in and is not is worse than no seam.
type sched func(tasks []func() error) error

// serial is core's only scheduler: every task, in order, and every failure.
//
// Aggregation lives here rather than in the walk, which is ADR-0011's rule and
// ADR-0010's measurement of it: the same walk function under a first-error
// scheduler and under this one reports one error and two over a plane with two
// bad leaves, byte-identical in between. So the walk never decides whether to
// continue, and the error model costs it nothing.
func serial(tasks []func() error) error {
	errs := make([]error, 0, len(tasks))
	for _, task := range tasks {
		errs = append(errs, task())
	}

	return join(errs...)
}

// direction is the residue Load and Dump do not share.
//
// ADR-0010 counts three operations and does not claim zero: at a leaf, Dump
// encodes a Go value and writes where Load reads and decodes; at a container,
// "is this container's own address carrying the whole answer" is one question
// with two answers; and members cannot be shared at all, because Dump reads a
// map's keys off the value while Load enumerates the plane.
//
// Only the leaf has anything to do while the compiler admits leaves and structs
// of them. The container question needs a composite that can be nil, and
// members needs one whose children come from the value, so those two operations
// join this interface with the types that give them something to decide rather
// than being stubbed out ahead of them.
//
// Everything else is written once and lives on [walker]: which nodes exist and
// in what order, where the context is checked, and where the scheduler sits.
type direction interface {
	atLeaf(ctx context.Context, n *node, v reflect.Value) error
}

// walker is one walk over one compiled schema, in one direction.
//
// It holds the root Go value rather than threading a value down beside the
// node, because a compiled node already carries the reflect index path from the
// root. That is what keeps the promotion rule the compiler's alone: the walk
// cannot visit a field the schema does not hold, and the schema cannot hold a
// field the walk will not visit, because they are one list (ADR-0010).
type walker struct {
	root reflect.Value
	dir  direction
	run  sched
}

// newWalker binds a direction to the value it walks. The scheduler is not a
// parameter, because there is one and nobody chooses it.
func newWalker(root reflect.Value, dir direction) *walker {
	return &walker{root: root, dir: dir, run: serial}
}

// ctxEndedMsg is what a cancellation says at whichever node it arrived at. It
// reads at a leaf, which has an address, and at a container, which may be the
// root and have none.
//
// It carries no class, because a cancellation gets none: errors.Is against
// context.Canceled is already the match, and a ferry class for it would be a
// second spelling of a standard library one (ADR-0011).
const ctxEndedMsg = "the context ended before this part of the walk ran"

// walk is the shared half of both directions, and the whole of it.
//
// The context is checked at every node entry, which is a placement rather than
// a policy: it had to be somewhere for the walk to be written at all, and the
// concurrency ticket owns whether it is per leaf, per subtree or not at all.
func (w *walker) walk(ctx context.Context, n *node) error {
	if err := ctx.Err(); err != nil {
		return newError(momentWalk, nil, n.addr, ctxEndedMsg).withCause(err)
	}

	if n.kind == nodeLeaf {
		return w.dir.atLeaf(ctx, n, w.at(n))
	}

	return w.run(w.tasks(ctx, n))
}

// at resolves a node's Go value from the root, through the reflect index path
// the compiler recorded.
//
// No index a compile produces steps through a pointer, so this cannot panic: an
// embedded pointer is refused at compile because promotion walks the pointed-to
// struct at the parent address, and a pointer-typed field is not yet a type
// ferry maps to an address.
func (w *walker) at(n *node) reflect.Value { return w.root.FieldByIndex(n.index) }

// tasks is a container's field list as a batch for the scheduler. It is built
// whole rather than run as it is built, because a batch is what the seam takes.
func (w *walker) tasks(ctx context.Context, n *node) []func() error {
	tasks := make([]func() error, 0, len(n.fields))
	for _, f := range n.fields {
		tasks = append(tasks, func() error { return w.walk(ctx, f) })
	}

	return tasks
}

// loadFrom is the Load direction: read the plane, and write to the field only
// where the plane spoke.
type loadFrom struct{ r Reader }

var _ direction = loadFrom{}

// atLeaf reads one address and decides what the observation means to the field.
func (l loadFrom) atLeaf(ctx context.Context, n *node, v reflect.Value) error {
	got, err := l.r.Get(ctx, n.addr)
	if err != nil {
		// A non-nil error reaches the caller as an error and is never
		// substituted with Absent (ADR-0004). Reading it as absence is how a
		// total backend outage loads as an all-zero struct with a nil error,
		// which a prototype committed and nothing saw for four rounds.
		return fromDriver(momentWalk, n.addr, err)
	}

	// ADR-0006: Absent means ferry does not write to the field, so a seeded
	// value keeps what it had and a fresh one keeps its zero.
	if got.Kind() == KindAbsent {
		return nil
	}

	// Which kinds this leaf takes and how it reads their text is the leaf's,
	// resolved at compile and held on the node, so the walk decides nothing
	// about a type here (ADR-0005).
	if err := n.codec.decode(v, got); err != nil {
		return newError(momentWalk, ErrValue, n.addr, err.Error()).withCause(err)
	}

	return nil
}

// dumpTo is the Dump direction: hand the plane one Value per address.
type dumpTo struct{ w Writer }

var _ direction = dumpTo{}

// atLeaf writes one address. It never writes an Absent, which is a Reader-side
// kind: an omitted address is one that gets no Set call at all rather than one
// that gets a Set of nothing (ADR-0006).
//
// A value the leaf's representation does not cover is reported rather than
// swallowed, which in core's set is a time.Time outside years 0 to 9999 and
// nothing else.
func (d dumpTo) atLeaf(ctx context.Context, n *node, v reflect.Value) error {
	out, err := n.codec.encode(v)
	if err != nil {
		return newError(momentWalk, ErrValue, n.addr, err.Error()).withCause(err)
	}

	if err := d.w.Set(ctx, n.addr, out); err != nil {
		return fromDriver(momentWalk, n.addr, err)
	}

	return nil
}
