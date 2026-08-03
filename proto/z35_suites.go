package main

// #35: the driver conformance suite and the codec conformance suite.
//
// The case lists are the #41 audit's section 5, which wrote them out as
// assertions rather than prose precisely so this ticket could count them, plus
// the two #28 and #31 add.

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// ZArtefact is #28's golden artefact case: a fixed value, dumped, compared
// against fixed expected plane CONTENTS.
//
// It is not ADR-0001's rejected byte-level plane fidelity, and the line is
// exact: that is about preserving a USER's comments and key order across a
// Load and Dump cycle. This is the driver's own output for one input, which is
// the golden column at the driver's boundary instead of at core's.
//
// Read is how the suite gets the plane's bytes, and it is the driver's,
// because only the driver knows what "the artefact" is: a file for yaml, a set
// of keys for kv, an environ for env.
type ZArtefact struct {
	Value any
	Want  string
}

// ===========================================================================
// The driver suite. ONE call, because `driver/*` is a CI glob and a suite a
// driver author can partially adopt is a suite that measures nothing.
// ===========================================================================

func ZDriver(t ZT, p ZPlane) {
	t.Helper()

	// 1. Every proof the plane can express, and a loud refusal for the rest.
	//    This is ADR-0005's three-plane requirement seen from the driver's
	//    side: the driver supplies one plane and the suite supplies the table.
	for _, pr := range ZCoreTypes() {
		fails, _ := pr.run(p, nil)
		for _, s := range fails {
			t.Errorf("%s: round trip: %s", p.Name, s)
		}
	}

	// 2. #28's golden artefact case. A round-trip suite structurally cannot
	//    see a spelling change, because both halves change together.
	for i, a := range p.Golden {
		got, err := zDumpArtefact(p, a.Value)
		switch {
		case err != nil:
			t.Errorf("%s: golden artefact %d: %v", p.Name, i, err)
		case got != a.Want:
			t.Errorf("%s: golden artefact %d: got %q want %q", p.Name, i, got, a.Want)
		}
	}

	// 3. ADR-0004's Bind rule, which is the one case that is unwritable at all
	//    if the address set and the I/O arrive in one call.
	zCase(t, p, "Bind succeeds against an unreachable plane", func() error {
		src, _ := p.Open()
		if _, err := src.Bind(NewAddressSet([]Path{Path{}.Name("nope")})); err != nil {
			return fmt.Errorf("Bind did I/O: %v", err)
		}
		return nil
	})

	// 4. ADR-0005's container answer, which #41 D15 and D16 found as two
	//    latent defects that cancelled.
	zCase(t, p, "Get at a container address is Absent with a NIL error", func() error {
		return zContainerCase(p)
	})

	// 5. ADR-0004's lifecycle, which #41 D14 found dropping a Close failure.
	zCase(t, p, "Commit runs only on success and Close always runs", func() error {
		return zLifecycleCase(p)
	})

	// 6. ADR-0003's driver-side injectivity, over the address set.
	zCase(t, p, "a non-injective key function is refused before any I/O", func() error {
		return zKeyFuncCase(p)
	})

	// 7. ADR-0004's dynamic tier, which the ADR's own worked example is a Dump.
	zCase(t, p, "a sink accepts a dynamic address its static table never held", func() error {
		return zDynamicCase(p)
	})
}

// zCase is the whole reporting convention: a case is a name and a func
// returning an error, so adding one is a line in a list rather than a new
// exported symbol. That is what makes "the suite may gain cases in a minor
// release" a statement about behaviour rather than about API.
func zCase(t ZT, p ZPlane, name string, f func() error) {
	t.Helper()
	if err := f(); err != nil {
		t.Errorf("%s: %s: %v", p.Name, name, err)
	}
}

func zDumpArtefact(p ZPlane, v any) (string, error) {
	ctx := context.Background()
	_, sink := p.Open()
	fs, ok := sink.(FYAMLSink)
	if !ok {
		return "", fmt.Errorf("this plane supplies no way to read its artefact")
	}
	if err := zDumpAny(ctx, v, sink); err != nil {
		return "", err
	}
	b, err := os.ReadFile(fs.Path)
	return string(b), err
}

// zDumpAny is Dump without the type parameter, which a suite needs because its
// case list is heterogeneous. It is the one place ferrytest wants what
// ADR-0010's generic entry point does not offer, and Z35=5 measures the
// alternatives.
func zDumpAny(ctx context.Context, v any, sink FSink) error {
	return zDumpReflect(ctx, reflect.ValueOf(v), sink)
}

func zContainerCase(p ZPlane) error {
	ctx := context.Background()
	src, sink := p.Open()
	type c struct {
		Tags []string `ferry:"tags"`
	}
	if err := Dump(ctx, c{[]string{"a"}}, sink); err != nil {
		return err
	}
	open, err := src.Bind(NewAddressSet([]Path{Path{}.Name("tags")}))
	if err != nil {
		return err
	}
	r, err := open(ctx)
	if err != nil {
		return err
	}
	v, gerr := r.Get(ctx, Path{}.Name("tags"))
	if gerr != nil {
		return fmt.Errorf("Get at /tags returned %v, want a nil error", gerr)
	}
	if v.Kind() != VAbsent {
		return fmt.Errorf("Get at /tags returned %s, want absent", v.GoString())
	}
	return nil
}

func zLifecycleCase(p ZPlane) error {
	ctx := context.Background()
	_, sink := p.Open()
	rec := &zLifeSink{next: sink}
	type c struct {
		A string `ferry:"a"`
	}
	if err := Dump(ctx, c{"x"}, rec); err != nil {
		return err
	}
	if !rec.closed {
		return fmt.Errorf("Close did not run on a successful dump")
	}
	return nil
}

func zKeyFuncCase(p ZPlane) error {
	// A driver that produces no plane key has no obligation here, which
	// ADR-0004 measured: "a tree plane pays nothing for the address set".
	// The case has to be able to say so rather than fail.
	set := NewAddressSet([]Path{
		Path{}.Name("db").Name("host"),
		Path{}.Name("db_host"),
	})
	if _, err := NewKeys(set, p.Name, zEnvKey); err == nil {
		return fmt.Errorf("a key function collapsing /db/host and /db_host was accepted")
	}
	return nil
}

func zDynamicCase(p ZPlane) error {
	ctx := context.Background()
	_, sink := p.Open()
	type c struct {
		Labels map[string]string `ferry:"labels"`
	}
	return Dump(ctx, c{map[string]string{"env": "prod"}}, sink)
}

func zEnvKey(p Path) (string, error) {
	var b strings.Builder
	for i, s := range p.Segments() {
		if i > 0 {
			b.WriteByte('_')
		}
		b.WriteString(strings.ToUpper(s.Text))
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("empty address")
	}
	return b.String(), nil
}

type zLifeSink struct {
	next   FSink
	closed bool
}

func (s *zLifeSink) Bind(a *AddressSet) (FOpenWriterFunc, error) {
	open, err := s.next.Bind(a)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (FWriter, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}
		return &zLifeWriter{s, w}, nil
	}, nil
}

type zLifeWriter struct {
	owner *zLifeSink
	next  FWriter
}

func (w *zLifeWriter) Set(ctx context.Context, p Path, v Value) error {
	return w.next.Set(ctx, p, v)
}
func (w *zLifeWriter) Commit(ctx context.Context) error {
	if c, ok := w.next.(FCommitter); ok {
		return c.Commit(ctx)
	}
	return nil
}
func (w *zLifeWriter) Close() error {
	w.owner.closed = true
	if r, ok := w.next.(FReleaser); ok {
		return r.Close()
	}
	return nil
}

// ===========================================================================
// The codec suite. ADR-0007 asks for cases and names one; ADR-0009 adds two,
// both from defects in its own generic wrapper, and neither is catchable by a
// registrant's own proof because the codec was correct and the wrapper was not.
// ===========================================================================

func ZCodec(t ZT, reg *Registry) {
	t.Helper()
	ctx := context.Background()

	for _, ty := range zRegistryTypes(reg) {
		name := ty.String()

		// ADR-0007: AppendText and MarshalText must agree. Nothing enforces it
		// for a user type, which is why it is a case rather than a promise.
		if s := zAppendAgrees(ty); s != "" {
			t.Errorf("codec %s: %s", name, s)
		}

		// ADR-0009's two wrapper defects, at the ONE value that finds both:
		// a registered interface at its nil zero value, in each direction.
		if ty.Kind() == reflect.Interface {
			if err := zNilInterface(ctx, ty, reg); err != nil {
				t.Errorf("codec %s: at the nil zero value: %v", name, err)
			}
		}

		// ADR-0009's zero-value totality, which Register itself runs. The case
		// asserts that it RAN, because #41 D4 found the check beside the API
		// rather than in it.
		if err := zZeroTotal(ty, reg); err != nil {
			t.Errorf("codec %s: %v", name, err)
		}
	}
}

func zAppendAgrees(t reflect.Type) string {
	pt := reflect.PointerTo(t)
	appender := reflect.TypeFor[interface{ AppendText([]byte) ([]byte, error) }]()
	marshaler := reflect.TypeFor[interface{ MarshalText() ([]byte, error) }]()
	if !pt.Implements(appender) || !pt.Implements(marshaler) {
		return ""
	}
	v := reflect.New(t)
	a, err1 := v.Interface().(interface {
		AppendText([]byte) ([]byte, error)
	}).AppendText(nil)
	m, err2 := v.Interface().(interface {
		MarshalText() ([]byte, error)
	}).MarshalText()
	if err1 != nil || err2 != nil {
		return ""
	}
	if string(a) != string(m) {
		return fmt.Sprintf("AppendText gives %q and MarshalText gives %q", a, m)
	}
	return ""
}

func zNilInterface(ctx context.Context, t reflect.Type, reg *Registry) error {
	holder := reflect.StructOf([]reflect.StructField{
		{Name: "V", Type: t, Tag: `ferry:"V"`},
	})
	v := reflect.New(holder).Elem()
	out := map[Path]Value{}
	if err := zDumpReflect(ctx, v, MemSink{out}, WithRegistry(reg)); err != nil {
		return fmt.Errorf("dump: %w", err)
	}
	dst := reflect.New(holder)
	if err := zLoadReflect(ctx, dst, MemSource{out}, WithRegistry(reg)); err != nil {
		return fmt.Errorf("load: %w", err)
	}
	return nil
}

func zZeroTotal(t reflect.Type, reg *Registry) error {
	c, ok := func() (leafCodec, bool) {
		done := reg.install()
		defer done()
		return identityLookup(t)
	}()
	if !ok {
		return nil
	}
	zero := reflect.New(t).Elem()
	v, err := c.enc(zero)
	if err != nil {
		return fmt.Errorf("the codec is not total over the zero value: encode: %w", err)
	}
	if v.Kind() == VString && c.kind != VString {
		v = Value{kind: c.kind, text: v.text}
	}
	dst := reflect.New(t).Elem()
	if err := c.dec(v, dst); err != nil {
		return fmt.Errorf("the codec is not total over the zero value: it encodes to %s and decoding that back fails: %w", v.GoString(), err)
	}
	return nil
}

// ===========================================================================
// The apparatus the four consumers reach for.
// ===========================================================================

// ZStatic is ADR-0004's `Static` combinator: a Source of constants, which is
// both the defaults layer and the memory plane's read half.
func ZStatic(m map[Path]Value) FSource { return MemSource{m} }

// ZRecord is ADR-0001's schema-extraction pattern as one call: dump into a
// recording sink and read what was mapped. ADR-0001 puts this in the Enabled
// bucket and says core exports no schema view; this is the reason that holds.
func ZRecord[T any](ctx context.Context, v T) (map[Path]Value, error) {
	out := map[Path]Value{}
	err := Dump(ctx, v, MemSink{out})
	return out, err
}

func zMemoryPlane() ZPlane {
	return ZPlane{
		Name:  "ferrytest.MemPlane",
		Kinds: allKinds,
		Open: func() (FSource, FSink) {
			m := map[Path]Value{}
			return MemSource{m}, MemSink{m}
		},
	}
}

func zYAMLPlane(dir string) ZPlane {
	return ZPlane{
		Name:  "yaml driver",
		Kinds: allKinds,
		Open: func() (FSource, FSink) {
			p := filepath.Join(dir, fmt.Sprintf("z%d.yaml", zSeq()))
			return FYAMLSource{Path: p}, FYAMLSink{Path: p}
		},
	}
}

func zFlatPlane() ZPlane {
	return ZPlane{
		Name:  "flattening plane",
		Kinds: flatKinds,
		Open: func() (FSource, FSink) {
			st := NewFlatStore()
			return FlatSource{st}, FlatSink{st}
		},
	}
}

// zCoreRegistry is the registry core's own test passes to ZComplete. It is
// empty, because core's pre-seeded entries are not registrations, and
// ZComplete unions the registry's types with core's own two tables.
func zCoreRegistry() *Registry { return NewRegistry() }

var _ = netip.Addr{}
var _ = time.Second

// zDumpReflect and zLoadReflect are Dump and Load with the type parameter
// erased, which the suites need because their case lists are heterogeneous.
// They exist here, in the suite, rather than in core: ADR-0009 refused a
// dynamic registration partly because "a reflect.StructOf type can never be T"
// under a generic entry point, and this is the same boundary from the other
// side. What ferrytest needs is not a non-generic entry point in core, it is
// the ability to build one for itself out of the generic one.
func zDumpReflect(ctx context.Context, v reflect.Value, sink FSink, options ...Option) error {
	o := defaultOpts()
	for _, op := range options {
		op.apply(&o)
	}
	s, err := schemaFor(v.Type(), o)
	if err != nil {
		return err
	}
	open, err := sink.Bind(s.as)
	if err != nil {
		return err
	}
	w, err := open(ctx)
	if err != nil {
		return err
	}
	out := map[Path]Value{}
	undo := o.reg.install()
	wk := &walker{dir: dumpDir(out), sch: o.sch, ctx: ctx}
	_, werr := wk.walk(s.root, v, Path{})
	undo()
	if werr != nil {
		return werr
	}
	for _, p := range sortedAddrs(out) {
		if err := w.Set(ctx, p, out[p]); err != nil {
			return err
		}
	}
	if c, ok := w.(FCommitter); ok {
		if err := c.Commit(ctx); err != nil {
			return err
		}
	}
	if r, ok := w.(FReleaser); ok {
		return r.Close()
	}
	return nil
}

func zLoadReflect(ctx context.Context, dst reflect.Value, src FSource, options ...Option) error {
	o := defaultOpts()
	for _, op := range options {
		op.apply(&o)
	}
	s, err := schemaFor(dst.Type().Elem(), o)
	if err != nil {
		return err
	}
	open, err := src.Bind(s.as)
	if err != nil {
		return err
	}
	r, err := open(ctx)
	if err != nil {
		return err
	}
	undo := o.reg.install()
	defer undo()
	wk := &walker{dir: loadDir(r, ctx, o), sch: o.sch, ctx: ctx}
	_, werr := wk.walk(s.root, dst.Elem(), Path{})
	return werr
}
