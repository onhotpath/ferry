package main

// The entry point, and the cache behind it.
//
// This is the shape E1 argues for, written as a real one so every later probe
// runs against it rather than against a sketch.

import (
	"context"
	"reflect"
	"sync"
)

// --- the cache --------------------------------------------------------------

// cacheEntry is the cheap thing that goes into the map. The expensive walk
// sits behind a per-entry sync.OnceValues, which is encoding/json/v2's
// two-level pattern: an entry that loses the LoadOrStore race is discarded
// before its once ever runs, so the thundering herd never touches the
// expensive path.
//
// sync.OnceValues is NOT here for speed - the golang commit that introduced
// it into encoding/json says the motivation was testing/synctest correctness
// and not performance. It is here for exactly-once initialisation and for the
// property that it re-panics the same value on every subsequent call, so a
// malformed schema panics identically forever rather than the first caller
// panicking and later callers receiving a zero schema.
type cacheEntry struct {
	once func() (*schema, error)
}

func schemaFor(t reflect.Type, o opts) (*schema, error) {
	// The freeze is ADR-0009's, and this is the line that makes "first use"
	// locatable. What makes a compile a USE is that its result is RETAINED:
	// ADR-0009's obligation is that a registry's answer for a type must never
	// change once that type has been resolved against it, and a resolution
	// that is discarded retains nothing to go stale. So caching and freezing
	// are one decision, taken here, and compileOnce below takes neither.
	o.reg.frozen.Store(true)

	k := o.key(t)
	if e, ok := o.reg.schemas.Load(k); ok {
		return e.(*cacheEntry).once()
	}
	e := &cacheEntry{}
	e.once = sync.OnceValues(func() (*schema, error) {
		done := o.reg.install()
		defer done()
		return compileSchema2(t, o)
	})
	actual, _ := o.reg.schemas.LoadOrStore(k, e)
	return actual.(*cacheEntry).once()
}

// --- the entry point --------------------------------------------------------

// Load reads a plane into a NEW value of T and returns it.
//
// It returns a value rather than filling a destination because ADR-0006
// measured that an in-place reload leaks the previous load's values for every
// address the plane has since lost, under every absence rule it considered,
// and concluded that a reload must produce a new value rather than mutate a
// live one. A signature that cannot be handed a live value cannot ship that
// defect.
func Load[T any](ctx context.Context, src FSource, options ...Option) (T, error) {
	var seed T
	return LoadOver(ctx, seed, src, options...)
}

// LoadOver is Load with ADR-0006's seeded value supplied explicitly. The seed
// is taken BY VALUE and a new value is returned, so `cfg, err = LoadOver(ctx,
// cfg, src)` is expressible and is visibly the caller asking for the carry-over
// rather than ferry doing it behind a pointer.
func LoadOver[T any](ctx context.Context, seed T, src FSource, options ...Option) (T, error) {
	o := defaultOpts()
	for _, op := range options {
		op.apply(&o)
	}
	var zero T
	s, err := schemaFor(reflect.TypeFor[T](), o)
	if err != nil {
		return zero, err
	}
	done := o.reg.install()
	defer done()

	open, err := src.Bind(NewAddressSet(s.addrs))
	if err != nil {
		return zero, err
	}
	rd, err := open(ctx)
	if err != nil {
		return zero, err
	}
	if rel, ok := rd.(FReleaser); ok {
		defer rel.Close()
	}
	out := seed
	rv := reflect.ValueOf(&out).Elem()
	w := &walker{dir: loadDir(rd, ctx, o), sch: serial, ctx: ctx}
	if _, err := w.walk(s.root, rv, Path{}); err != nil {
		return zero, err
	}
	return out, nil
}

// Dump writes v out to a sink. T is inferred, so no call site names it.
func Dump[T any](ctx context.Context, v T, sink FSink, options ...Option) error {
	o := defaultOpts()
	for _, op := range options {
		op.apply(&o)
	}
	s, err := schemaFor(reflect.TypeFor[T](), o)
	if err != nil {
		return err
	}
	done := o.reg.install()
	defer done()

	out := map[Path]Value{}
	w := &walker{dir: dumpDir(out), sch: serial, ctx: ctx}
	if _, err := w.walk(s.root, reflect.ValueOf(v), Path{}); err != nil {
		return err
	}
	addrs := NewAddressSet(sortedAddrs(out))
	open, err := sink.Bind(addrs)
	if err != nil {
		return err
	}
	return fDump(ctx, open, out, addrs)
}

// Compile compiles the schema for T and reports whether it compiled.
//
// It is named Compile and not Validate because ADR-0001 spends the word
// "validation" on a thing ferry rules out by architecture, and a package that
// rules out validation and then exports Validate is telling a reader something
// untrue. What this asks is whether the program's own ANNOTATION is
// well-formed, from reflect.TypeFor[T]() alone, with no value constructed and
// no plane reachable.
//
// It returns only an error because ADR-0001 keeps the compiled schema
// unexported. That is a divergence from regexp.Compile's shape, and it is a
// consequence of a decision taken elsewhere rather than a choice made here.
//
// ADR-0008 requires that it and Load share ONE compiler, or it compiles a
// schema no Load will, so it is literally the same call.
func Compile[T any](options ...Option) error {
	o := defaultOpts()
	for _, op := range options {
		op.apply(&o)
	}
	_, err := compileOnce(reflect.TypeFor[T](), o)
	return err
}

// compileOnce is schemaFor without the cache and without the freeze, and the
// two omissions are the same omission. E10 measured what happens otherwise: a
// Compile from a package-level var freezes the default registry mid-import
// graph, a later init's Register fails, and a dropped error leaves the plane
// holding a representation the user replaced, with no error anywhere.
func compileOnce(t reflect.Type, o opts) (*schema, error) {
	done := o.reg.install()
	defer done()
	return compileSchema2(t, o)
}

// dumpTo is the in-memory half of Dump, used by probes that have no sink.
func dumpTo[T any](ctx context.Context, v T, options ...Option) (map[Path]Value, error) {
	o := defaultOpts()
	for _, op := range options {
		op.apply(&o)
	}
	s, err := schemaFor(reflect.TypeFor[T](), o)
	if err != nil {
		return nil, err
	}
	done := o.reg.install()
	defer done()
	out := map[Path]Value{}
	w := &walker{dir: dumpDir(out), sch: serial, ctx: ctx}
	_, err = w.walk(s.root, reflect.ValueOf(v), Path{})
	return out, err
}

// loadFrom is the in-memory half of Load.
func loadFrom[T any](ctx context.Context, seed T, vals map[Path]Value, options ...Option) (T, error) {
	o := defaultOpts()
	for _, op := range options {
		op.apply(&o)
	}
	var zero T
	s, err := schemaFor(reflect.TypeFor[T](), o)
	if err != nil {
		return zero, err
	}
	done := o.reg.install()
	defer done()
	out := seed
	rv := reflect.ValueOf(&out).Elem()
	w := &walker{dir: loadDir(mapReader{vals}, ctx, o), sch: serial, ctx: ctx}
	if _, err := w.walk(s.root, rv, Path{}); err != nil {
		return zero, err
	}
	return out, nil
}
