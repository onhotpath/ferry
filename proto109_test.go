package ferry_test

// PROTOTYPE for #109. Not intended to merge. Every t.Log below is a
// measurement, so run this with -v and read the output.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// recSink records what a Dump asked the plane to write.
type recSink struct{ w *recWriter }

type recWriter struct {
	bound []string
	wrote []string
}

func newRec() *recSink { return &recSink{w: &recWriter{}} }

func (s *recSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	for a := range addrs.All() {
		s.w.bound = append(s.w.bound, a.String())
	}

	slices.Sort(s.w.bound)

	return func(context.Context) (ferry.Writer, error) { return s.w, nil }, nil
}

func (w *recWriter) Set(_ context.Context, at ferry.Path, v ferry.Value) error {
	w.wrote = append(w.wrote, fmt.Sprintf("%s = %s(%s)", at, v.Kind(), v.GoString()))

	return nil
}

// absentSource answers Absent for every address, so a Load succeeds and writes
// nothing.
type absentSource struct{}

func (absentSource) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(context.Context) (ferry.Reader, error) { return absentReader{}, nil }, nil
}

type absentReader struct{}

func (absentReader) Get(context.Context, ferry.Path) (ferry.Value, error) {
	return ferry.Value{}, nil
}

// failingSource fails inside the walk, at the first Get.
type failingSource struct{}

func (failingSource) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(context.Context) (ferry.Reader, error) { return failingReader{}, nil }, nil
}

type failingReader struct{}

func (failingReader) Get(context.Context, ferry.Path) (ferry.Value, error) {
	return ferry.Value{}, errBoom
}

var errBoom = errors.New("the plane refused")

type p109Cfg struct {
	Name string `ferry:"name"`
}

type p109Two struct {
	Host string `ferry:"host"`
	Port string `ferry:"port"`
}

type p109NoAddr struct {
	unexported string //nolint:unused // deliberately maps no address
}

// dumpAny runs Dump[any] and reports the addresses written and the error.
func dumpAny(t *testing.T, label string, v any) {
	t.Helper()

	r := newRec()
	err := ferry.Dump(context.Background(), v, r)

	t.Logf("%-28s bound=%v wrote=%v err=%v", label, r.w.bound, r.w.wrote, err)
}

// M1: does it work mechanically.
func TestP109_M1_Mechanics(t *testing.T) {
	var v any = p109Cfg{Name: "svc"}

	r := newRec()
	if err := ferry.Dump(context.Background(), v, r); err != nil {
		t.Fatalf("Dump[any] err = %v", err)
	}

	t.Logf("M1 bound = %v", r.w.bound)
	t.Logf("M1 wrote = %v", r.w.wrote)

	// The static-T control, for comparison.
	c := newRec()
	if err := ferry.Dump(context.Background(), p109Cfg{Name: "svc"}, c); err != nil {
		t.Fatalf("Dump[p109Cfg] err = %v", err)
	}

	t.Logf("M1 control wrote = %v", c.w.wrote)
}

// M2: the degenerate inputs.
func TestP109_M2_Degenerate(t *testing.T) {
	var nilAny any

	var nilPtr *p109Cfg

	inner := any(p109Cfg{Name: "svc"})

	dumpAny(t, "nil any", nilAny)
	dumpAny(t, "any(int)", 7)
	dumpAny(t, "any(*struct) non-nil", &p109Cfg{Name: "svc"})
	dumpAny(t, "any(*struct) nil", nilPtr)
	dumpAny(t, "any(struct) no address", p109NoAddr{})
	dumpAny(t, "any(any(struct))", inner)
	dumpAny(t, "any(map)", map[string]string{"a": "b"})
	dumpAny(t, "any(slice)", []string{"a"})
	dumpAny(t, "any(**struct)", func() **p109Cfg { p := &p109Cfg{Name: "x"}; return &p }())
	dumpAny(t, "any(nil *any)", (*any)(nil))
}

// M3: does Dump[any] now succeed where Compile[any]() fails.
func TestP109_M3_CompileInvariant(t *testing.T) {
	compileErr := ferry.Compile[any]()

	r := newRec()
	dumpErr := ferry.Dump(context.Background(), any(p109Cfg{Name: "svc"}), r)

	t.Logf("M3 Compile[any]()      = %v", compileErr)
	t.Logf("M3 Dump[any](p109Cfg{}) = %v (wrote %v)", dumpErr, r.w.wrote)

	if compileErr != nil && dumpErr == nil {
		t.Log("M3 VERDICT: the #70 invariant is BROKEN for T=any: Compile fails, the verb succeeds")
	} else {
		t.Log("M3 VERDICT: the invariant holds")
	}
}

// M4: what Load[any] does under the change.
func TestP109_M4_LoadAsymmetry(t *testing.T) {
	_, loadErr := ferry.Load[any](context.Background(), absentSource{})
	t.Logf("M4 Load[any]        err = %v", loadErr)

	_, overErr := ferry.LoadOver(context.Background(), any(p109Cfg{}), absentSource{})
	t.Logf("M4 LoadOver[any](seed p109Cfg{}) err = %v", overErr)

	r := newRec()
	dumpErr := ferry.Dump(context.Background(), any(p109Cfg{Name: "svc"}), r)
	t.Logf("M4 Dump[any]        err = %v wrote=%v", dumpErr, r.w.wrote)
}

// M5: the schema cache under a dynamic key.
func TestP109_M5_Cache(t *testing.T) {
	reg := ferry.NewRegistry()

	one := newRec()
	two := newRec()
	again := newRec()

	e1 := ferry.Dump(context.Background(), any(p109Cfg{Name: "a"}), one, ferry.WithRegistry(reg))
	e2 := ferry.Dump(context.Background(), any(p109Two{Host: "h", Port: "p"}), two, ferry.WithRegistry(reg))
	e3 := ferry.Dump(context.Background(), any(p109Cfg{Name: "b"}), again, ferry.WithRegistry(reg))

	t.Logf("M5 dyn=p109Cfg wrote=%v err=%v", one.w.wrote, e1)
	t.Logf("M5 dyn=p109Two wrote=%v err=%v", two.w.wrote, e2)
	t.Logf("M5 dyn=p109Cfg wrote=%v err=%v (second visit, must be the cached entry)", again.w.wrote, e3)
	_ = reg
}

type p109Stringer struct {
	Name string `ferry:"name"`
}

func (p109Stringer) String() string { return "p109Stringer" }

type p109AnyField struct {
	Name string `ferry:"name"`
	Blob any    `ferry:"blob"`
}

// M7: blast radius. The unwrap is at the root only, so it must not reach a
// non-empty interface any differently, and it must not reach into the walk.
func TestP109_M7_BlastRadius(t *testing.T) {
	// A non-empty interface root is unwrapped too, because the test is on the
	// reflect Kind and not on the static type being exactly `any`.
	var s fmt.Stringer = p109Stringer{Name: "svc"}

	r := newRec()
	err := ferry.Dump(context.Background(), s, r)
	t.Logf("M7 Dump[fmt.Stringer] wrote=%v err=%v", r.w.wrote, err)

	// An interface-typed field is the walk's business, not the root's, and it
	// must be unaffected.
	f := newRec()
	ferr := ferry.Dump(context.Background(), p109AnyField{Name: "svc", Blob: p109Cfg{Name: "x"}}, f)
	t.Logf("M7 struct with an `any` field: wrote=%v err=%v", f.w.wrote, ferr)

	// And the same field reached through an `any` root, so the root unwrap
	// cannot be mistaken for a general one.
	g := newRec()
	gerr := ferry.Dump(context.Background(), any(p109AnyField{Name: "svc", Blob: p109Cfg{Name: "x"}}), g)
	t.Logf("M7 the same, through an any root: wrote=%v err=%v", g.w.wrote, gerr)
}

// M8: the thing #109 is actually for. ADR-0014's literal golden table, run
// through the entry point exactly as the ADR publishes it.
func TestP109_M8_ArtefactTable(t *testing.T) {
	table := []ferrytest.Artefact{
		{Value: struct {
			B []byte `ferry:"b"`
		}{[]byte("hi")}, Want: "b: !!binary aGk=\n"},
		{Value: p109Cfg{Name: "svc"}, Want: "name: svc\n"},
		{Value: p109Two{Host: "h", Port: "p"}, Want: "host: h\nport: p\n"},
	}

	for i, a := range table {
		r := newRec()
		err := ferry.Dump(context.Background(), a.Value, r)
		t.Logf("M8 row %d (%T) wrote=%v err=%v", i, a.Value, r.w.wrote, err)
	}
}

// M6: LoadOver's "a failed load returns the seed" property.
func TestP109_M6_LoadOverSeed(t *testing.T) {
	seed := p109Cfg{Name: "seeded"}

	got, err := ferry.LoadOver(context.Background(), seed, failingSource{})

	t.Logf("M6 LoadOver over a failing source: got=%#v err=%v", got, err)

	if got != seed {
		t.Fatalf("M6 the seed was not returned: %#v", got)
	}
}

// ptrText marshals only through a pointer receiver, so a non-addressable root
// is the case that would break if receiver() did not copy.
type ptrText struct{ s string }

func (p *ptrText) MarshalText() ([]byte, error) { return []byte(p.s), nil }
func (p *ptrText) UnmarshalText(b []byte) error { p.s = string(b); return nil }

type p109Ptr struct {
	T ptrText       `ferry:"t"`
	D time.Duration `ferry:"d"`
}

// M9: addressability. The unwrapped root is not addressable, so every field
// under it is not addressable either.
func TestP109_M9_Addressability(t *testing.T) {
	v := p109Ptr{T: ptrText{s: "hello"}, D: 3 * time.Second}

	stat := newRec()
	serr := ferry.Dump(context.Background(), v, stat)
	t.Logf("M9 static root wrote=%v err=%v", stat.w.wrote, serr)

	dyn := newRec()
	derr := ferry.Dump(context.Background(), any(v), dyn)
	t.Logf("M9 any root    wrote=%v err=%v", dyn.w.wrote, derr)

	if fmt.Sprint(stat.w.wrote) != fmt.Sprint(dyn.w.wrote) {
		t.Fatal("M9 the two roots wrote different things")
	}
}

// M10: the symmetric half. LoadOver has a seed, and a seed has a dynamic type,
// so the pair can be made symmetric everywhere except Load[any] itself.
func TestP109_M10_Symmetry(t *testing.T) {
	inst := ferrytest.MemPlane().Open()

	if err := ferry.Dump(context.Background(), any(p109Two{Host: "h", Port: "p"}), inst.Sink); err != nil {
		t.Fatalf("M10 dump: %v", err)
	}

	got, err := ferry.LoadOver(context.Background(), any(p109Two{}), inst.Source)
	t.Logf("M10 LoadOver[any](seed p109Two{}) = %#v err=%v", got, err)

	_, lerr := ferry.Load[any](context.Background(), inst.Source)
	t.Logf("M10 Load[any] err = %v", lerr)

	// And the failure property, over an interface seed.
	seed := any(p109Cfg{Name: "seeded"})

	back, ferr := ferry.LoadOver(context.Background(), seed, failingSource{})
	t.Logf("M10 LoadOver[any] over a failing source: got=%#v err=%v", back, ferr)

	if back != seed {
		t.Fatalf("M10 the seed was not returned: %#v", back)
	}
}
