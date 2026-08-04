package ferry

import (
	"context"
	"fmt"
	"reflect"
	"slices"
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
// Two of the three are here. Members is the one still missing, because it needs
// a composite whose children come from the value, and every composite in the
// static tier gets its members from the type.
//
// Everything else is written once and lives on [walker]: which nodes exist and
// in what order, where the context is checked, and where the scheduler sits.
type direction interface {
	atLeaf(ctx context.Context, n *node, v reflect.Value) error

	// atNullable is the container question, at a composite that can be nil.
	// Dump answers it from the value and Load from the plane, and into is the
	// walk of what is beneath, which either direction may decline to run.
	atNullable(ctx context.Context, n *node, v reflect.Value, into descend) error

	// atArray is what an array's own address is asked before its elements are
	// walked, which is nothing on Dump and one question on Load: an array's
	// membership is the type's, so a plane holding an index outside it is
	// holding something this type cannot take.
	atArray(ctx context.Context, n *node, v reflect.Value) error
}

// descend walks what is under a container, over the value the direction decided
// to walk it over. It is a parameter rather than a return, because a direction
// that has to decide something after the subtree ran - whether a pointer is
// materialised at all - cannot express that by returning.
type descend func(reflect.Value) error

// walker is one walk over one compiled schema, in one direction.
//
// The value is threaded down beside the node rather than resolved from the root
// per node, because a pointer is a container the walk descends through: an
// index path rooted at the whole value would step through the pointer, and
// reflect.Value.FieldByIndex panics on a nil one. So a node's index path is
// relative to the container above it, which keeps the promotion rule the
// compiler's alone: the walk cannot visit a field the schema does not hold, and
// the schema cannot hold a field the walk will not visit, because they are one
// list (ADR-0010).
type walker struct {
	dir direction
	run sched
}

// newWalker binds a direction to the value it walks. The scheduler is not a
// parameter, because there is one and nobody chooses it.
func newWalker(dir direction) *walker {
	return &walker{dir: dir, run: serial}
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
func (w *walker) walk(ctx context.Context, n *node, v reflect.Value) error {
	if err := ctx.Err(); err != nil {
		return newError(momentWalk, nil, n.addr, ctxEndedMsg).withCause(err)
	}

	switch n.kind {
	case nodeLeaf:
		return w.dir.atLeaf(ctx, n, v)
	case nodePointer:
		return w.dir.atNullable(ctx, n, v, func(inner reflect.Value) error {
			return w.run(w.tasks(ctx, n, inner))
		})
	case nodeArray:
		if err := w.dir.atArray(ctx, n, v); err != nil {
			return err
		}

		return w.run(w.tasks(ctx, n, v))
	default:
		return w.run(w.tasks(ctx, n, v))
	}
}

// tasks is a container's member list as a batch for the scheduler. It is built
// whole rather than run as it is built, because a batch is what the seam takes.
func (w *walker) tasks(ctx context.Context, n *node, v reflect.Value) []func() error {
	tasks := make([]func() error, 0, len(n.fields))

	for i, f := range n.fields {
		member := memberOf(n, v, i, f)
		tasks = append(tasks, func() error { return w.walk(ctx, f, member) })
	}

	return tasks
}

// memberOf resolves one member's Go value out of the container's.
//
// The three containers reach their members three ways, and a pointer's is not
// FieldByIndex at an empty path: that panics on anything but a struct, so a
// *[N]T would fail at the array rather than reaching its elements.
func memberOf(n *node, v reflect.Value, i int, f *node) reflect.Value {
	switch n.kind {
	case nodeArray:
		return v.Index(i)
	case nodePointer:
		// A pointer mints no segment, so there is no step between it and what
		// it points at: its one member is the pointed-to value itself.
		return v
	default:
		return v.FieldByIndex(f.index)
	}
}

// loadFrom is the Load direction: read the plane, and write to the field only
// where the plane spoke.
type loadFrom struct {
	r Reader

	// wrote counts the writes the walk has made, and it is how "materialised
	// exactly where something under it was present" is decided at a pointer
	// (ADR-0006): the count before the subtree ran against the count after.
	//
	// It is shared mutable state behind the scheduler seam, which is the hazard
	// ADR-0010 records rather than one this ticket introduces. A concurrent
	// scheduler needs it per subtree; the serial one core ships needs nothing.
	wrote *int
}

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

	*l.wrote++

	return nil
}

// atNullable materialises a pointer exactly where the plane spoke under it.
//
// The three observations are three answers. A Null at the pointer's own address
// is the plane saying the section is there and is nothing, so the pointer is
// nil. Anything else at that address is a value at a container address, which
// carries Absent or Null and never anything else (ADR-0003). Absent is the
// plane saying nothing about the section itself, so what decides is whether it
// said anything beneath it: a declared default is not presence and neither is a
// seed, so an optional section stays optional (ADR-0006).
func (l loadFrom) atNullable(ctx context.Context, n *node, v reflect.Value, into descend) error {
	got, err := l.r.Get(ctx, n.addr)
	if err != nil {
		return fromDriver(momentWalk, n.addr, err)
	}

	if got.Kind() == KindNull {
		v.SetZero()
		*l.wrote++

		return nil
	}

	if got.Kind() != KindAbsent {
		return newError(momentWalk, ErrValue, n.addr, fmt.Sprintf(
			"the plane holds %s at a container address, which holds absence or a null and nothing else",
			got.Kind()))
	}

	return l.materialise(v, into)
}

// materialise builds the pointee fresh, walks into it, and publishes it only
// where the walk wrote.
//
// The fresh allocation is not a tidiness. LoadOver's failure property rests on
// a shallow copy of the seed, so a walk that wrote through the seed's own
// pointer would publish a partial load into a value the caller still holds and
// the property would break in silence.
func (l loadFrom) materialise(v reflect.Value, into descend) error {
	fresh := reflect.New(v.Type().Elem())
	if !v.IsNil() {
		fresh.Elem().Set(v.Elem())
	}

	before := *l.wrote
	if err := into(fresh.Elem()); err != nil {
		return err
	}

	if *l.wrote > before {
		v.Set(fresh)
	}

	return nil
}

// atArray refuses a plane holding an index this array cannot.
//
// An array's length is part of its type, so an index outside it is a value with
// no field to land in, and padding or truncating it would be the silent loss
// ADR-0001 rules out. It is asked only of a plane that can enumerate, which is
// the same asymmetry that makes an array loadable from one that cannot: the
// elements are read by name either way, and only enumeration can reveal an
// index that is not one of them.
func (l loadFrom) atArray(ctx context.Context, n *node, v reflect.Value) error {
	lister, ok := l.r.(Enumerator)
	if !ok {
		return nil
	}

	kids, err := lister.Children(ctx, n.addr)
	if err != nil {
		return fromDriver(momentWalk, n.addr, err)
	}

	errs := make([]error, 0, len(kids))
	for _, kid := range kids {
		errs = append(errs, overLength(n, kid, v))
	}

	return join(errs...)
}

// overLength reports one child of an array address that the array has no
// element for, and nothing for one it has.
//
// It compares against the element addresses the compiler minted rather than
// against a number, because those addresses are the whole of what this array
// can hold and comparing what a driver enumerated with what the type determined
// is the same question stated once.
func overLength(n *node, kid Path, v reflect.Value) error {
	at, ok := lastIndex(kid)
	if !ok || holds(n, kid) {
		return nil
	}

	return newError(momentWalk, ErrValue, n.addr, fmt.Sprintf(
		"the plane holds index %s and %s holds %d", at, v.Type(), len(n.fields)))
}

// holds reports whether one of a container's members is at this address.
func holds(n *node, addr Path) bool {
	return slices.ContainsFunc(n.fields, func(f *node) bool { return f.addr == addr })
}

// lastIndex is the text of an address's last segment, and whether that segment
// names a position rather than a member.
func lastIndex(p Path) (text string, ok bool) {
	for seg := range p.Segments() {
		text, ok = seg.Text(), seg.Kind() == Index
	}

	return text, ok
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

// atNullable writes a Null where the pointer is nil and descends where it is
// not, which are the two states a pointer has and the two observations a
// container address carries.
//
// It never writes anything at the container address when the pointer is set,
// because the answer is then under it: a container address is never realised at
// the same time as anything beneath it (ADR-0003).
func (d dumpTo) atNullable(ctx context.Context, n *node, v reflect.Value, into descend) error {
	if !v.IsNil() {
		return into(v.Elem())
	}

	if err := d.w.Set(ctx, n.addr, Null()); err != nil {
		return fromDriver(momentWalk, n.addr, err)
	}

	return nil
}

// atArray asks nothing. An index outside the array is something a plane can
// hold and a value cannot, so it is a question with a Load side only.
func (dumpTo) atArray(context.Context, *node, reflect.Value) error { return nil }
