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
// [ErrMissing], [ErrValue], [ErrPlane], [ErrDriver] or [ErrReadOnly].
//
// It is [LoadOver] with the zero seed, so anything said about one holds for the
// other.
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
func LoadOver[T any](ctx context.Context, seed T, src Source, opts ...Option) (T, error) {
	// The copy is the whole mechanism. The walk writes here and never to seed,
	// so there is no path by which a partial crosses the boundary.
	over := seed

	if err := runLoad(ctx, reflect.ValueOf(&over).Elem(), src, opts); err != nil {
		return seed, err
	}

	return over, nil
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
func Dump[T any](ctx context.Context, v T, sink Sink, opts ...Option) error {
	// Through a pointer, so the schema and the walk both see T rather than
	// whatever dynamic type an interface T would hand reflect.ValueOf.
	return runDump(ctx, reflect.ValueOf(&v).Elem(), sink, opts)
}

// runLoad is Load and LoadOver, once. dst is the addressable copy of the seed
// the walk writes into.
func runLoad(ctx context.Context, dst reflect.Value, src Source, opts []Option) error {
	sch, err := schemaOf(dst.Type(), opts, retained)
	if err != nil {
		return err
	}

	if src == nil {
		return nilPlane(nilSourceMsg)
	}

	open, err := src.Bind(sch.addrs)
	if err != nil {
		return fromBind(err)
	}

	r, err := open(ctx)
	if err != nil {
		return fromDriver(momentOpen, Path{}, err)
	}

	dir := loadFrom{r: r, wrote: new(int)}
	walked := newWalker(dir).walk(ctx, spot{n: sch.root, v: loadRoot(dst)})

	return join(walked, released(r))
}

// runDump is Dump, once.
func runDump(ctx context.Context, v reflect.Value, sink Sink, opts []Option) error {
	sch, err := schemaOf(v.Type(), opts, retained)
	if err != nil {
		return err
	}

	root, err := dumpRoot(v)
	if err != nil {
		return err
	}

	if sink == nil {
		return nilPlane(nilSinkMsg)
	}

	open, err := sink.Bind(sch.addrs)
	if err != nil {
		return fromBind(err)
	}

	w, err := open(ctx)
	if err != nil {
		return fromDriver(momentOpen, Path{}, err)
	}

	walked := written(ctx, w, sch, root)
	if walked == nil {
		walked = committed(ctx, w)
	}

	return join(walked, released(w))
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
