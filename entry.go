package ferry

import (
	"context"
	"reflect"
)

// Load builds a value of T from src.
//
// It names its type where [Dump] infers one, which is not an inconsistency:
// Load has nothing to infer from and Dump has the value in hand.
//
//	cfg, err := ferry.Load[Config](ctx, yaml.Source{Path: "app.yaml"})
//
// It returns a value and takes no destination, and that is ADR-0006's decision
// rather than a taste one. Measured on the shape that takes a *T, a second load
// into the same destination returns the previous load's value for every address
// the plane has since lost, and a signature taking *T offers exactly that as its
// only spelling.
//
// It is [LoadOver] with the zero seed, in the implementation and not only in
// the prose, so nothing is expressible through one and not the other and the two
// cannot drift.
//
// On failure it returns the zero value, which falls out of the rule stated at
// LoadOver rather than being a second rule. Range the failure with [Elements].
func Load[T any](ctx context.Context, src Source, opts ...Option) (T, error) {
	var zero T

	return LoadOver(ctx, zero, src, opts...)
}

// LoadOver builds a value of T from src, over a seed the caller supplies.
//
// It is one operation with two uses, and both are ADR-0006's. A seed is that
// ADR's answer for a composite default a tag cannot spell, and a reload is the
// caller writing the carry-over out loud rather than getting it from a
// destination that happens to be populated:
//
//	cfg, err := ferry.LoadOver(ctx, Config{Tags: []string{"default"}}, src)
//	cfg, err = ferry.LoadOver(ctx, cfg, src)
//
// An address the plane does not have is Absent, and Absent does not write, so
// every field the plane is silent about keeps the value the seed gave it.
//
// On failure it returns the seed it was handed. ADR-0011's rule is that ferry
// yields no value it *built*, and returning the zero value here would destroy a
// live configuration ferry never touched - the same worst outcome reached
// through the other door. It is honoured as a property rather than as a
// promise: the walk builds into a copy of the seed, so the partial the walk
// built is unreachable from the caller whatever happens.
func LoadOver[T any](ctx context.Context, seed T, src Source, opts ...Option) (T, error) {
	// The copy is the whole mechanism. The walk writes here and never to seed,
	// so there is no path by which a partial crosses the boundary.
	over := seed

	if err := runLoad(ctx, reflect.ValueOf(&over).Elem(), src, opts); err != nil {
		return seed, err
	}

	return over, nil
}

// Dump writes v to sink.
//
// It infers its type where [Load] names one, because the value is in hand.
//
//	err := ferry.Dump(ctx, cfg, yaml.Sink{Path: "app.yaml"})
//
// The schema is compiled from T rather than from what v happens to hold, so a
// Dump and a Load of one type are the same address set and one compiler.
//
// A sink implementing [Committer] is committed only where the walk succeeded,
// and a sink implementing [Releaser] is closed either way. That is ADR-0004's
// protocol rather than a lifecycle ferry invents: no driver is ever told that
// it failed, and closed-without-Commit is the abort signal.
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

	open, err := src.Bind(sch.addrs)
	if err != nil {
		return fromDriver(momentBind, Path{}, err)
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

	open, err := sink.Bind(sch.addrs)
	if err != nil {
		return fromDriver(momentBind, Path{}, err)
	}

	w, err := open(ctx)
	if err != nil {
		return fromDriver(momentOpen, Path{}, err)
	}

	// The minted set is the walk's own and starts empty on every dump, because
	// the addresses in it came from this value and the next dump has another.
	dir := dumpTo{w: w, addrs: sch.addrs, minted: map[Path]struct{}{}}

	walked := newWalker(dir).walk(ctx, spot{n: sch.root, v: root})
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
