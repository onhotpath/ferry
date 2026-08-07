package ferry

import (
	"context"
	"reflect"
)

// Load builds a value of T from src. The type is named because there is no
// value to infer it from; [Dump] infers its own.
//
//	cfg, err := ferry.Load[Config](ctx, yaml.Source{Path: "app.yaml"})
//
// Every field the plane is silent about keeps T's zero value, and a field
// declaring default= takes its default there instead.
//
// On failure it returns the zero value of T and never a partly built one. Range
// the failure with [Elements], and match a member against [ErrSchema],
// [ErrMissing], [ErrValue], [ErrPlane], [ErrPanic], [ErrDriver] or
// [ErrReadOnly].
//
// It is [LoadOver] with the zero seed, so anything said about one holds for the
// other, and it is [Bind] plus [Binding.Load] with the handle dropped.
func Load[T any](ctx context.Context, src Source, opts ...Option) (T, error) {
	var zero T

	return LoadOver(ctx, zero, src, opts...)
}

// LoadOver builds a value of T from src, over a seed the caller supplies.
//
// It has two uses. A seed is how a composite default is spelled, since a struct
// tag holds one text and a composite's value lives at many addresses; and a
// reload is the caller writing the carry-over out loud rather than getting it
// from a destination that happens to be populated:
//
//	cfg, err := ferry.LoadOver(ctx, Config{Tags: []string{"default"}}, src)
//	cfg, err = ferry.LoadOver(ctx, cfg, src)
//
// An address the plane does not have is absent, and absence does not write, so
// every field the plane is silent about keeps the value the seed gave it. Where
// a seed and a declared default both apply to one field the declared default
// wins, because ferry cannot tell a seeded value from a zero one.
//
// On failure it returns the seed it was handed, unchanged. The walk builds into
// a copy, so a partly built value is never reachable from the caller. Range the
// failure with [Elements].
//
// It is [Bind] plus [Binding.LoadOver] with the handle dropped. A caller who
// keeps the handle gets the same load and skips the compile and the bind.
func LoadOver[T any](ctx context.Context, seed T, src Source, opts ...Option) (T, error) {
	b, err := Bind[T](src, opts...)
	if err != nil {
		return seed, err
	}

	return b.LoadOver(ctx, seed)
}

// Dump writes v to sink. The type is inferred, because the value is in hand.
//
//	err := ferry.Dump(ctx, cfg, yaml.Sink{Path: "app.yaml"})
//
// The schema is compiled from T rather than from what v happens to hold, so a
// Dump and a Load of one type cover the same address set.
//
// A field marked omitzero is skipped where it holds T's zero value. An omitted
// address gets no write at all rather than a write of nothing, so an omission
// is not a deletion: a replacing sink and a patching sink read one dump
// differently and both are correct.
//
// Encoding is a phase before any write, so a Dump that fails for a reason ferry
// could have known without touching the plane leaves the plane untouched. A
// sink implementing [Committer] is exempt, since staging already gives it that
// property, and it gets both kinds of failure in one report for it.
//
// A [Committer] is committed only where the walk succeeded, and a [Releaser] is
// closed either way, so closed-without-Commit is the abort signal and no driver
// is ever told that it failed.
//
// Range the failure with [Elements].
//
// It is [BindSink] plus [SinkBinding.Dump] with the handle dropped. So a call
// naming no sink at all is refused as a nil plane before the value is looked
// at: where the sink is nil and v is a nil pointer, the report names the sink.
func Dump[T any](ctx context.Context, v T, sink Sink, opts ...Option) error {
	b, err := BindSink[T](sink, opts...)
	if err != nil {
		return err
	}

	return b.Dump(ctx, v)
}

// bound is ADR-0004's phase split on the read side, with no type parameter: a
// compiled schema and the [OpenFunc] a driver handed back for it.
//
// [Binding] is this plus T, and runLoad is this plus a reflect.Type, so the
// bind half and the load half each exist exactly once whichever door the caller
// came in through (ADR-0012).
type bound struct {
	sch  *schema
	open OpenFunc

	// budget is the caller's MaxConcurrency, resolved at bind and spent per
	// load. It is held here rather than read off the compiled schema because it
	// is load-affecting and the schema is cached across configs (ADR-0019).
	budget int
}

// newBound is [Bind] with the type as a value. The order is load-bearing: the
// compile runs first, so an Option list that does not resolve fails before any
// driver is reached, and the retention is retained rather than discarded,
// because whatever holds the result holds the schema (ADR-0009, ADR-0010).
func newBound(t reflect.Type, src Source, opts []Option) (*bound, error) {
	cfg, err := newConfig(opts)
	if err != nil {
		return nil, err
	}

	sch, err := schemaWith(t, cfg, retained)
	if err != nil {
		return nil, err
	}

	if src == nil {
		return nil, nilPlane(nilSourceMsg)
	}

	open, err := src.Bind(sch.addrs)
	if err != nil {
		return nil, fromBind(err)
	}

	if open == nil {
		return nil, driverNil(momentBind, nilOpenMsg)
	}

	return &bound{sch: sch, open: open, budget: cfg.budget}, nil
}

// load is the per-load half: open, walk, release. dst is the addressable copy
// of the seed the walk writes into.
//
// Everything it touches is either the binding's own immutable state or minted
// here, which is what lets [Binding] promise goroutine safety with no lock
// anywhere (ADR-0012).
//
// The release is deferred, which is ADR-0004's "Close always runs" written so
// that it holds on every exit rather than on the two the straight line covers.
// A panic the fence does not catch unwinds through here, and the reader was
// left open by a release the unwind skipped (#254). It stays correct under
// fanout because the scheduler returns only once every task has, so nothing is
// still writing into dst when the release runs (ADR-0019).
//
// The budget is put on the context before the open rather than after it, so the
// driver's own open reads the same number core's scheduler is about to spend
// (ADR-0019).
func (b *bound) load(ctx context.Context, dst reflect.Value) (err error) {
	ctx = budgeted(ctx, b.budget)

	r, err := b.open(ctx)
	if err != nil {
		return fromDriver(momentOpen, Path{}, err)
	}

	if r == nil {
		return driverNil(momentOpen, nilReaderMsg)
	}

	defer func() { err = spelledFor(join(err, released(r)), r) }()

	w := newWalker(loadFrom{r: r, addrs: b.sch.addrs}, schedulerFor(b.budget, r))

	_, err = w.walk(ctx, spot{n: b.sch.root, v: loadRoot(dst)})

	return err
}

// boundSink is bound on the write side, and [SinkBinding] is this plus T.
type boundSink struct {
	sch  *schema
	open OpenWriterFunc

	// budget is the caller's MaxConcurrency. The write path never fans out, so
	// nothing here spends it; it rides the open's context so that a sink which
	// batches at Commit can spend it there, which is the only place a dump has
	// left to spend anything (ADR-0019).
	budget int

	// forget is the first composite this schema determines and whether it
	// determines one at all, which is the whole of "does a dump of this type
	// need a retraction" (ADR-0004). It is decided once, off the address set,
	// because the answer is the schema's and not the value's: a dump replaces
	// every composite it speaks about, so a type holding one needs the
	// capability whatever this call's value turns out to hold.
	forget  CompositeAddr
	forgets bool
}

// newBoundSink is [BindSink] with the type as a value.
//
// It has no value to look at, which is where ADR-0012's split puts the nil-sink
// refusal ahead of the nil-root one: a call that named no plane at all failed
// before the value was ever relevant.
func newBoundSink(t reflect.Type, sink Sink, opts []Option) (*boundSink, error) {
	cfg, err := newConfig(opts)
	if err != nil {
		return nil, err
	}

	sch, err := schemaWith(t, cfg, retained)
	if err != nil {
		return nil, err
	}

	if sink == nil {
		return nil, nilPlane(nilSinkMsg)
	}

	open, err := sink.Bind(sch.addrs)
	if err != nil {
		return nil, fromBind(err)
	}

	if open == nil {
		return nil, driverNil(momentBind, nilOpenWriterMsg)
	}

	at, forgets := sch.addrs.firstComposite()

	return &boundSink{sch: sch, open: open, budget: cfg.budget, forget: at, forgets: forgets}, nil
}

// replaceable refuses, at the open and before anything is written, a schema that
// needs a retraction against a writer that cannot make one (ADR-0004, ADR-0006).
//
// The moment is the open because both halves of the question are answered there
// and neither is answered earlier: the schema knows at compile time that it
// holds a composite, and the writer is the value whose method set says whether
// the plane can forget an address, which Bind never sees. Failing here costs the
// plane nothing, where failing mid-walk has already written part of a dump that
// was never going to be a replacement.
//
// It is addressed at a composite rather than at the plane, because the address
// is what makes the message actionable: it names the field whose members the
// plane would have kept.
func (b *boundSink) replaceable(w Writer) error {
	if !b.forgets {
		return nil
	}

	if _, ok := w.(Unsetter); ok {
		return nil
	}

	return newError(momentOpen, ErrPlane, b.forget.Path(), unforgettableMsg(w))
}

// dump is the per-dump half: refuse a nil root, open, encode and write, commit,
// release.
//
// The release is deferred for the reason [bound.load] gives, and the commit is
// not: a panic that unwinds past the walk is not a walk that succeeded, so the
// sink is closed with no Commit and ADR-0004's abort signal survives the path
// nothing was going to report on (#254).
func (b *boundSink) dump(ctx context.Context, v reflect.Value) (err error) {
	root, err := dumpRoot(v)
	if err != nil {
		return err
	}

	ctx = budgeted(ctx, b.budget)

	w, err := b.open(ctx)
	if err != nil {
		return fromDriver(momentOpen, Path{}, err)
	}

	if w == nil {
		return driverNil(momentOpen, nilWriterMsg)
	}

	defer func() { err = spelledFor(join(err, released(w)), w) }()

	if err := b.replaceable(w); err != nil {
		return err
	}

	walked := written(ctx, w, b.sch, root)
	if walked == nil {
		walked = committed(ctx, w)
	}

	return walked
}

// runLoad is [LoadOver] for a caller holding a reflect.Value rather than a T,
// which is the internal seam and nothing else. It is newBound plus load, the
// same two halves the generic door runs.
func runLoad(ctx context.Context, dst reflect.Value, src Source, opts []Option) error {
	b, err := newBound(dst.Type(), src, opts)
	if err != nil {
		return err
	}

	return b.load(ctx, dst)
}

// runDump is [Dump] for the same caller, and it is newBoundSink plus dump.
func runDump(ctx context.Context, v reflect.Value, sink Sink, opts []Option) error {
	b, err := newBoundSink(v.Type(), sink, opts)
	if err != nil {
		return err
	}

	return b.dump(ctx, v)
}

// loadRoot is the struct the walk writes into, given the value the caller's
// type named.
//
// A root pointer is materialised into a fresh allocation seeded from what the
// caller supplied, rather than written through. The copy is what keeps "the
// partial is unreachable from the caller" a property: a pointer is the one root
// shape whose contents a plain copy of the seed would still share with the
// caller, so writing through it would publish the partial by the back door.
//
// Materialising unconditionally is a placement rather than a rule. What a
// pointer means when the plane is silent is the presence bit's question, and
// the root is the one position with no address for its own absence to be
// observed at.
func loadRoot(dst reflect.Value) reflect.Value {
	if dst.Kind() != reflect.Pointer {
		return dst
	}

	fresh := reflect.New(dst.Type().Elem())
	if !dst.IsNil() {
		fresh.Elem().Set(dst.Elem())
	}

	dst.Set(fresh)

	return fresh.Elem()
}

// dumpRoot is the struct the walk reads from, and it refuses a nil one.
//
// ADR-0010 refuses a root that compiles to a leaf because the empty path is not
// an address, and measured the consequence of letting it through: a YAML sink
// wrote "{}" and returned a nil error, so the value was silently and totally
// lost. A nil root pointer is the same hole at the same address - there is
// nowhere for the Null a nil composite writes to sit - so it is refused rather
// than dumped as an empty plane with a nil error.
func dumpRoot(v reflect.Value) (reflect.Value, error) {
	if v.Kind() != reflect.Pointer {
		return v, nil
	}

	if v.IsNil() {
		return reflect.Value{}, newError(momentWalk, ErrValue, Path{},
			"the root is a nil pointer, and the root has no address of its own for a null to sit at: "+
				"dumping it would write nothing and report success")
	}

	return v.Elem(), nil
}

// released closes a reader or a writer that holds a resource, and does nothing
// for one that does not.
//
// It runs whether the walk succeeded or failed, which is what makes
// closed-without-Commit the abort signal (ADR-0004). The optional interface is
// discovered by assertion, so a driver with nothing to release implements
// nothing.
func released(plane any) error {
	c, ok := plane.(Releaser)
	if !ok {
		return nil
	}

	if err := c.Close(); err != nil {
		return fromDriver(momentClose, Path{}, err)
	}

	return nil
}

// spelledFor names every located failure of one run in the plane's own spelling,
// where the reader or writer it went through has one (ADR-0011, #159).
//
// It is the last thing either direction does, after the close error has joined
// in, so every element of one run is spelled once and never twice. The optional
// interface is discovered by assertion, so a plane that names nothing leaves the
// report exactly as core composed it.
func spelledFor(err error, plane any) error {
	n, ok := plane.(PlaneNamer)
	if err == nil || !ok {
		return err
	}

	return spellLocations(err, n)
}

// committed commits a staging sink. Its caller runs it only where the walk
// succeeded, because there is no failure to report to a driver, only a commit
// that does not happen.
func committed(ctx context.Context, w Writer) error {
	c, ok := w.(Committer)
	if !ok {
		return nil
	}

	if err := c.Commit(ctx); err != nil {
		return fromDriver(momentCommit, Path{}, err)
	}

	return nil
}
