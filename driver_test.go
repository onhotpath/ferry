package ferry

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"
)

// Releaser is io.Closer and not a name ferry invents, so a driver wrapping a
// file or a connection satisfies it for free. ADR-0004 states the check in
// exactly this form, and these three declarations are it: os.File satisfies
// Releaser, and the two interfaces are assignable to each other in both
// directions, which is the whole of "this is io.Closer".
var (
	_ Releaser  = (*os.File)(nil)
	_ Releaser  = io.Closer(nil)
	_ io.Closer = Releaser(nil)
)

// ctxType is the parameter the phase rule is stated in terms of.
var ctxType = reflect.TypeFor[context.Context]()

// TestBindTakesNoContext is the assertable form of ADR-0004's central rule.
// Bind does no I/O, and the type says so by having nowhere to put a
// cancellation. If a context ever appears here, "Bind must succeed against an
// unreachable plane" stops being writable as a conformance case.
func TestBindTakesNoContext(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  reflect.Type
	}{
		{name: "Source", typ: reflect.TypeFor[Source]()},
		{name: "Sink", typ: reflect.TypeFor[Sink]()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if n := countParams(methodOf(t, tc.typ, "Bind"), ctxType); n != 0 {
				t.Errorf("%s.Bind takes %d context.Context parameters, want 0", tc.name, n)
			}
		})
	}
}

// TestIOTakesContext is the other half of the same rule: everything that does
// reach the plane carries a cancellation, so a driver has somewhere to return
// ctx.Err() from. Close is the deliberate exception, because cleanup that can
// be cancelled is how the temp file leaks.
func TestIOTakesContext(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  reflect.Type
		want int
	}{
		{name: "OpenFunc", typ: reflect.TypeFor[OpenFunc](), want: 1},
		{name: "OpenWriterFunc", typ: reflect.TypeFor[OpenWriterFunc](), want: 1},
		{name: "Reader.Get", typ: methodOf(t, reflect.TypeFor[Reader](), "Get"), want: 1},
		{name: "Writer.Set", typ: methodOf(t, reflect.TypeFor[Writer](), "Set"), want: 1},
		{name: "Committer.Commit", typ: methodOf(t, reflect.TypeFor[Committer](), "Commit"), want: 1},
		{name: "Enumerator.Children", typ: methodOf(t, reflect.TypeFor[Enumerator](), "Children"), want: 1},
		{name: "Prober.Probe", typ: methodOf(t, reflect.TypeFor[Prober](), "Probe"), want: 1},
		{name: "Ensurer.Ensure", typ: methodOf(t, reflect.TypeFor[Ensurer](), "Ensure"), want: 1},
		{name: "Releaser.Close", typ: methodOf(t, reflect.TypeFor[Releaser](), "Close"), want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if n := countParams(tc.typ, ctxType); n != tc.want {
				t.Errorf("%s takes %d context.Context parameters, want %d", tc.name, n, tc.want)
			}
		})
	}
}

func methodOf(t *testing.T, typ reflect.Type, name string) reflect.Type {
	t.Helper()

	m, ok := typ.MethodByName(name)
	if !ok {
		t.Fatalf("%s has no %s method", typ, name)
	}

	return m.Type
}

func countParams(fn, want reflect.Type) int {
	n := 0

	for i := range fn.NumIn() {
		if fn.In(i) == want {
			n++
		}
	}

	return n
}

// probe is a whole driver, both directions, over one map: the smallest thing
// that has to compile for the contract to be implementable at all. It is also
// where the optional interfaces are exercised the way core exercises them,
// which is by assertion and never by requirement.
type probe struct {
	values   map[Path]Value
	presence map[Path]Presence
	bound    *AddressSet
	closed   bool
	commit   bool
}

func (p *probe) Bind(addrs *AddressSet) (OpenFunc, error) {
	p.bound = addrs

	return func(context.Context) (Reader, error) { return p, nil }, nil
}

func (p *probe) Get(_ context.Context, addr LeafAddr) (Value, error) {
	return p.values[addr.Path()], nil
}

func (*probe) Children(context.Context, CompositeAddr) ([]Segment, error) {
	return []Segment{IndexSegment(0)}, nil
}

func (p *probe) Probe(_ context.Context, addr Container) (SectionInfo, error) {
	if p.presence[addr.Path()] == PresenceNull {
		return SectionNull, nil
	}

	return SectionAbsent, nil
}

func (p *probe) Set(_ context.Context, addr LeafAddr, v Value) error {
	p.values[addr.Path()] = v

	return nil
}

func (p *probe) Ensure(_ context.Context, addr Container, held Presence) error {
	p.presence[addr.Path()] = held

	return nil
}

func (p *probe) Commit(context.Context) error {
	p.commit = true

	return nil
}

func (p *probe) Close() error {
	p.closed = true

	return nil
}

// TestContractIsImplementable walks one value through both halves the way core
// will: bind before any I/O, open, use, then discover the optional interfaces
// by assertion rather than by demanding them.
func TestContractIsImplementable(t *testing.T) {
	addr := leafOf(At("db", "host"), KindString)
	p := &probe{values: map[Path]Value{}, presence: map[Path]Presence{}}

	src, sink := Source(p), Sink(&sinkOnly{probe: p})

	open, err := sink.Bind(newAddressSet(addr))
	if err != nil {
		t.Fatalf("Sink.Bind: %v", err)
	}

	w, err := open(t.Context())
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}

	if err = w.Set(t.Context(), addr, String("localhost")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	commitAndClose(t, w)
	readBack(t, src, addr)

	if !p.commit || !p.closed {
		t.Errorf("commit=%v closed=%v, want both true", p.commit, p.closed)
	}
}

// sinkOnly is the second type ADR-0004 says a driver serving both directions
// has to ship, because one type cannot have two Bind methods. That cost is
// stated in the ADR and this is what it looks like.
type sinkOnly struct{ probe *probe }

func (s *sinkOnly) Bind(addrs *AddressSet) (OpenWriterFunc, error) {
	s.probe.bound = addrs

	return func(context.Context) (Writer, error) { return s.probe, nil }, nil
}

func commitAndClose(t *testing.T, w Writer) {
	t.Helper()

	c, ok := w.(Committer)
	if !ok {
		t.Fatalf("%T does not implement Committer", w)
	}

	if err := c.Commit(t.Context()); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	r, ok := w.(Releaser)
	if !ok {
		t.Fatalf("%T does not implement Releaser", w)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func readBack(t *testing.T, src Source, addr LeafAddr) {
	t.Helper()

	open, err := src.Bind(newAddressSet(addr))
	if err != nil {
		t.Fatalf("Source.Bind: %v", err)
	}

	r, err := open(t.Context())
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}

	got, err := r.Get(t.Context(), addr)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got != String("localhost") {
		t.Errorf("Get(%s) = %#v, want %#v", addr, got, String("localhost"))
	}

	missing, err := r.Get(t.Context(), leafOf(At("nope"), KindString))
	if err != nil || missing.Kind() != KindAbsent {
		t.Errorf("Get at a missing address = %#v, %v, want absent and no error", missing, err)
	}

	enumerate(t, r, compositeOf(At("tags"), KindString))
	probeSection(t, r, sectionOf(At("db")))
}

func enumerate(t *testing.T, r Reader, addr CompositeAddr) {
	t.Helper()

	e, ok := r.(Enumerator)
	if !ok {
		t.Fatalf("%T does not implement Enumerator", r)
	}

	kids, err := e.Children(t.Context(), addr)
	if err != nil || len(kids) != 1 || kids[0] != IndexSegment(0) {
		t.Errorf("Children(%s) = %v, %v", addr, kids, err)
	}
}

// probeSection is the third optional interface, discovered the same way the
// other two are: by assertion, never by requirement.
func probeSection(t *testing.T, r Reader, addr SectionAddr) {
	t.Helper()

	pr, ok := r.(Prober)
	if !ok {
		t.Fatalf("%T does not implement Prober", r)
	}

	got, err := pr.Probe(t.Context(), addr)
	if err != nil || got != SectionAbsent {
		t.Errorf("Probe(%s) = %#v, %v, want absent and no error", addr, got, err)
	}
}

// TestReadOnlyRefusalLandsInTheOpen pins where a plane that is writable in
// principle but not right now says so: inside the OpenWriterFunc, not at Bind,
// which does no I/O and cannot know, and not at the first Set, which has
// already half-written the plane.
func TestReadOnlyRefusalLandsInTheOpen(t *testing.T) {
	var s Sink = readOnly{}

	open, err := s.Bind(newAddressSet(leafOf(At("x"), KindString)))
	if err != nil {
		t.Fatalf("Bind refused before any I/O: %v", err)
	}

	w, err := open(t.Context())
	if w != nil {
		t.Errorf("open returned a writer %v, want none", w)
	}

	if !errors.Is(err, ErrReadOnly) {
		t.Errorf("open error = %v, want one matching ErrReadOnly", err)
	}
}

type readOnly struct{}

func (readOnly) Bind(*AddressSet) (OpenWriterFunc, error) {
	return func(context.Context) (Writer, error) {
		return nil, ErrReadOnly
	}, nil
}
