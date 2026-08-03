package ferrytest_test

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// openPlane mints a fresh memory plane and opens both halves of it.
//
// Fresh per call, never shared between subtests: a destination shared across
// equivalence cases is the defect that hides a broken second walk, and this
// plane refuses a duplicate write, so sharing one would also turn every second
// case into a spurious refusal.
func openPlane(t *testing.T) (ferry.Reader, ferry.Writer) {
	t.Helper()

	inst := ferrytest.MemPlane().Open()

	return openReader(t, inst.Source), openWriter(t, inst.Sink)
}

func openReader(t *testing.T, src ferry.Source) ferry.Reader {
	t.Helper()

	open, err := src.Bind(ferry.NewAddressSet())
	if err != nil {
		t.Fatalf("Source.Bind: %v", err)
	}

	r, err := open(t.Context())
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}

	return r
}

func openWriter(t *testing.T, snk ferry.Sink) ferry.Writer {
	t.Helper()

	open, err := snk.Bind(ferry.NewAddressSet())
	if err != nil {
		t.Fatalf("Sink.Bind: %v", err)
	}

	w, err := open(t.Context())
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}

	return w
}

func mustSet(t *testing.T, w ferry.Writer, addr ferry.Path, v ferry.Value) {
	t.Helper()

	if err := w.Set(t.Context(), addr, v); err != nil {
		t.Fatalf("Set(%s): %v", addr, err)
	}
}

func mustGet(t *testing.T, r ferry.Reader, addr ferry.Path) ferry.Value {
	t.Helper()

	v, err := r.Get(t.Context(), addr)
	if err != nil {
		t.Fatalf("Get(%s): %v", addr, err)
	}

	return v
}

func mustChildren(t *testing.T, r ferry.Reader, prefix ferry.Path) []ferry.Path {
	t.Helper()

	e, ok := r.(ferry.Enumerator)
	if !ok {
		t.Fatalf("%T does not implement ferry.Enumerator", r)
	}

	kids, err := e.Children(t.Context(), prefix)
	if err != nil {
		t.Fatalf("Children(%s): %v", prefix, err)
	}

	return kids
}

// TestMemPlaneKeysByTheCanonicalRendering is ADR-0003's first obligation,
// observed from outside: two ways of spelling one address are one slot, and two
// addresses whose renderings differ are two, including the pair that differs
// only by segment kind.
func TestMemPlaneKeysByTheCanonicalRendering(t *testing.T) {
	_, w := openPlane(t)

	mustSet(t, w, ferry.At("db", "host"), ferry.String("first"))

	err := w.Set(t.Context(), ferry.At("db").At("host"), ferry.String("second"))
	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("second write at the same rendering = %v, want a refusal", err)
	}
}

// TestMemPlaneSeparatesTheSegmentKinds is the half of the same obligation that
// a flat key space cannot hold: a map key spelled "0" and position 0 render
// differently, so they are two slots here, where a plane keying on text alone
// would turn the map into a sequence and destroy the key.
func TestMemPlaneSeparatesTheSegmentKinds(t *testing.T) {
	r, w := openPlane(t)

	named, indexed := ferry.At("tags").At("0"), ferry.At("tags").Elem(0)

	mustSet(t, w, named, ferry.String("as a map key"))
	mustSet(t, w, indexed, ferry.String("as a position"))

	if got := mustGet(t, r, named); got != ferry.String("as a map key") {
		t.Errorf("Get(%s) = %#v", named, got)
	}

	if got := mustGet(t, r, indexed); got != ferry.String("as a position") {
		t.Errorf("Get(%s) = %#v", indexed, got)
	}
}

// TestMemPlaneReportsAbsenceAsAKind covers the other half of ADR-0004's
// decision that absence is a kind: a lookup miss is the zero Value, with no
// second return value to discard and no sentinel to match.
func TestMemPlaneReportsAbsenceAsAKind(t *testing.T) {
	r, _ := openPlane(t)

	got, err := r.Get(t.Context(), ferry.At("nothing", "here"))
	if err != nil || got.Kind() != ferry.KindAbsent {
		t.Errorf("Get at an unwritten address = %#v, %v, want absent and no error", got, err)
	}
}

// TestMemPlaneNeverFolds is ADR-0003's second obligation, and it is the viper
// defect turned into a property: three case-variant addresses are three
// entries, each holding its own value, and none of them is reachable through
// another's spelling.
func TestMemPlaneNeverFolds(t *testing.T) {
	r, w := openPlane(t)

	variants := []string{"Host", "host", "HOST"}
	for _, name := range variants {
		mustSet(t, w, ferry.At(name), ferry.String(name))
	}

	for _, name := range variants {
		if got := mustGet(t, r, ferry.At(name)); got != ferry.String(name) {
			t.Errorf("Get(/%s) = %#v, want string(%q)", name, got, name)
		}
	}

	kids := mustChildren(t, r, ferry.Path{})
	if len(kids) != len(variants) {
		t.Fatalf("Children at the root = %v, want %d entries", kids, len(variants))
	}

	// Segment text compares by exact bytes, so the order is the byte order of
	// the three spellings and not a folded one.
	want := []ferry.Path{ferry.At("HOST"), ferry.At("Host"), ferry.At("host")}
	if !slices.Equal(kids, want) {
		t.Errorf("Children at the root = %v, want %v", kids, want)
	}
}

// TestMemPlaneRefusesADuplicateWrite is ADR-0003's third obligation. The
// refusal is loud, it names the address, it answers to the plane class, and the
// value already there survives it - a plane that kept the last writer would
// report success for a dump that lost a field.
func TestMemPlaneRefusesADuplicateWrite(t *testing.T) {
	r, w := openPlane(t)

	addr := ferry.At("db", "host")
	mustSet(t, w, addr, ferry.String("first"))

	err := w.Set(t.Context(), addr, ferry.String("second"))
	if err == nil {
		t.Fatal("a second write at one address was accepted, want a refusal")
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("refusal = %v, want one matching ferry.ErrPlane", err)
	}

	if !strings.Contains(err.Error(), addr.String()) {
		t.Errorf("refusal = %q, want it to name %s", err, addr)
	}

	if got := mustGet(t, r, addr); got != ferry.String("first") {
		t.Errorf("Get(%s) = %#v, want the first write to have survived", addr, got)
	}
}

// TestMemPlaneEnumeratesSegmentWise is ADR-0003's fourth obligation, over the
// twelve indices that make the difference visible: sorted as text they are
// 0 1 10 11 2 3, and segment-wise they are 0 1 2 3 10 11.
func TestMemPlaneEnumeratesSegmentWise(t *testing.T) {
	r, w := openPlane(t)

	tags := ferry.At("tags")
	for _, i := range []uint{11, 2, 7, 0, 10, 4, 1, 9, 3, 8, 5, 6} {
		mustSet(t, w, tags.Elem(i), ferry.Number(strconv.FormatUint(uint64(i), 10)))
	}

	want := make([]ferry.Path, 0, 12)
	for i := range uint(12) {
		want = append(want, tags.Elem(i))
	}

	got := mustChildren(t, r, tags)
	if !slices.Equal(got, want) {
		t.Errorf("Children(%s) = %v, want %v", tags, got, want)
	}

	// Twice, because the obligation is that a test asserting on this plane's
	// contents is not asserting on Go's randomised map iteration order.
	if again := mustChildren(t, r, tags); !slices.Equal(again, got) {
		t.Errorf("Children(%s) is not stable: %v then %v", tags, got, again)
	}
}

// TestMemPlaneIsTheNegativeCaseForInjectivity is ADR-0003's fifth obligation,
// and the assertion is that nothing is refused.
//
// A flattening driver joins segments with a separator, so /db_host and
// /db/host are one environment variable and the collision has to be caught at
// Bind. This plane's key function is the identity, so the pair is two entries
// and no address set can make it collide: a conformance run here proves nothing
// about the driver-side rule, which is why that rule needs a first-party driver
// with a real key function behind it.
func TestMemPlaneIsTheNegativeCaseForInjectivity(t *testing.T) {
	r, w := openPlane(t)

	flat, structured := ferry.At("db_host"), ferry.At("db", "host")

	mustSet(t, w, flat, ferry.String("flat"))
	mustSet(t, w, structured, ferry.String("structured"))

	if got := mustGet(t, r, flat); got != ferry.String("flat") {
		t.Errorf("Get(%s) = %#v", flat, got)
	}

	if got := mustGet(t, r, structured); got != ferry.String("structured") {
		t.Errorf("Get(%s) = %#v", structured, got)
	}
}

// TestMemPlaneBindDoesNoIO pins the phase rule at the one plane where it is
// trivially satisfiable, so that the shape a driver has to copy is visible:
// Bind takes no context, keeps nothing from the set it is handed, and cannot
// fail.
func TestMemPlaneBindDoesNoIO(t *testing.T) {
	inst := ferrytest.MemPlane().Open()
	src, snk := inst.Source, inst.Sink

	// The set a schema determined is not what this plane keys on, and an
	// address outside it is still writable and readable - which is what stops a
	// precomputed table from refusing a legitimate map key.
	unknown := ferry.At("labels", "env")

	openWrite, err := snk.Bind(ferry.NewAddressSet(ferry.At("known")))
	if err != nil {
		t.Fatalf("Sink.Bind: %v", err)
	}

	w, err := openWrite(t.Context())
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}

	mustSet(t, w, unknown, ferry.String("minted"))

	openRead, err := src.Bind(nil)
	if err != nil {
		t.Fatalf("Source.Bind(nil): %v", err)
	}

	r, err := openRead(t.Context())
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}

	if got := mustGet(t, r, unknown); got != ferry.String("minted") {
		t.Errorf("Get(%s) = %#v, want the dynamic address to have been accepted", unknown, got)
	}
}

// TestMemPlaneEnumeratesContainers covers the shape a nested composite makes:
// the children of a container are addresses one segment deeper, whatever their
// kind, and a leaf has none.
func TestMemPlaneEnumeratesContainers(t *testing.T) {
	r, w := openPlane(t)

	limits := ferry.At("limits")
	mustSet(t, w, limits.At("http").Elem(0), ferry.Number("1"))
	mustSet(t, w, limits.At("http").Elem(1), ferry.Number("2"))
	mustSet(t, w, limits.At("grpc"), ferry.Null())
	mustSet(t, w, ferry.At("name"), ferry.String("svc"))

	want := []ferry.Path{limits.At("grpc"), limits.At("http")}
	if got := mustChildren(t, r, limits); !slices.Equal(got, want) {
		t.Errorf("Children(%s) = %v, want %v", limits, got, want)
	}

	seq := limits.At("http")

	want = []ferry.Path{seq.Elem(0), seq.Elem(1)}
	if got := mustChildren(t, r, seq); !slices.Equal(got, want) {
		t.Errorf("Children(%s) = %v, want %v", seq, got, want)
	}

	if got := mustChildren(t, r, seq.Elem(0)); len(got) != 0 {
		t.Errorf("Children at a leaf = %v, want none", got)
	}

	if got := mustChildren(t, r, ferry.At("absent")); len(got) != 0 {
		t.Errorf("Children at an unwritten address = %v, want none", got)
	}
}

// TestMemPlaneEnumeratesLargeIndices pins the index round trip through
// enumeration at a width no test value reaches by accident, because the plane
// rebuilds an enumerated address out of segments rather than parsing text.
func TestMemPlaneEnumeratesLargeIndices(t *testing.T) {
	r, w := openPlane(t)

	tags := ferry.At("tags")
	big := uint(1234567890)

	mustSet(t, w, tags.Elem(big), ferry.String("x"))

	want := []ferry.Path{tags.Elem(big)}
	if got := mustChildren(t, r, tags); !slices.Equal(got, want) {
		t.Errorf("Children(%s) = %v, want %v", tags, got, want)
	}
}

// TestMemWriterHasNoLifecycle asserts what ADR-0004's own table says about the
// recorder row: it stages nothing and holds nothing, so it implements neither
// optional interface. A Commit here would be a lie, and a Close would be the
// `return nil` boilerplate that reads in the source exactly like a rollback
// somebody forgot.
func TestMemWriterHasNoLifecycle(t *testing.T) {
	r, w := openPlane(t)

	if _, ok := w.(ferry.Committer); ok {
		t.Errorf("%T implements ferry.Committer, want it not to", w)
	}

	if _, ok := w.(ferry.Releaser); ok {
		t.Errorf("%T implements ferry.Releaser, want it not to", w)
	}

	if _, ok := r.(ferry.Releaser); ok {
		t.Errorf("%T implements ferry.Releaser, want it not to", r)
	}
}

// TestStatic covers the plane an ordinary user reaches for: fixed contents,
// read-only by construction, and copied out of the caller's map.
func TestStatic(t *testing.T) {
	values := map[ferry.Path]ferry.Value{
		ferry.At("port"):    ferry.Number("8080"),
		ferry.At("timeout"): ferry.String("30s"),
	}

	src := ferrytest.Static(values)

	// Mutating the caller's map afterwards must not reach a plane already
	// handed out.
	values[ferry.At("port")] = ferry.Number("9090")
	delete(values, ferry.At("timeout"))

	r := openReader(t, src)

	if got := mustGet(t, r, ferry.At("port")); got != ferry.Number("8080") {
		t.Errorf("Get(/port) = %#v, want number(\"8080\")", got)
	}

	if got := mustGet(t, r, ferry.At("timeout")); got != ferry.String("30s") {
		t.Errorf("Get(/timeout) = %#v, want string(\"30s\")", got)
	}

	want := []ferry.Path{ferry.At("port"), ferry.At("timeout")}
	if got := mustChildren(t, r, ferry.Path{}); !slices.Equal(got, want) {
		t.Errorf("Children at the root = %v, want %v", got, want)
	}
}

// TestMemPlaneDescription pins the description [ferrytest.MemPlane] fills in,
// because a suite reads it rather than the plane: the kinds it declares are all
// six, since a map of Values adds nothing of its own, and it pins no golden
// artefact because it has no spelling for one to hold.
func TestMemPlaneDescription(t *testing.T) {
	p := ferrytest.MemPlane()

	if p.Name == "" {
		t.Error("MemPlane has no Name")
	}

	want := []ferry.VKind{
		ferry.KindAbsent, ferry.KindNull, ferry.KindBool,
		ferry.KindNumber, ferry.KindString, ferry.KindBytes,
	}
	if !slices.Equal(p.Kinds, want) {
		t.Errorf("MemPlane().Kinds = %v, want %v", p.Kinds, want)
	}

	if len(p.Golden) != 0 {
		t.Errorf("MemPlane().Golden = %v, want none", p.Golden)
	}

	// Fresh per Open, or one suite's cases would refuse each other's writes.
	first := p.Open()
	mustSet(t, openWriter(t, first.Sink), ferry.At("x"), ferry.String("1"))

	second := p.Open()
	if got := mustGet(t, openReader(t, second.Source), ferry.At("x")); got.Kind() != ferry.KindAbsent {
		t.Errorf("a second Open sees %#v at /x, want absent", got)
	}
}

// TestMemPlaneYieldsNoContents is the memory plane's half of #101: a plane with
// no serialization format hands back no way to read raw contents, which is what
// makes the golden artefact case skipped for it rather than failed.
//
// It is asserted against the same instance the write half came from, because
// [ferrytest.Instance] is the whole point of that ticket - the contents belong
// to one minted plane and not to the description that minted it.
func TestMemPlaneYieldsNoContents(t *testing.T) {
	inst := ferrytest.MemPlane().Open()

	if inst.Contents != nil {
		t.Error("MemPlane mints an Instance with Contents, want none: it has no spelling to hand back")
	}

	if inst.Source == nil || inst.Sink == nil {
		t.Errorf("MemPlane mints Source %v and Sink %v, want both halves", inst.Source, inst.Sink)
	}
}
