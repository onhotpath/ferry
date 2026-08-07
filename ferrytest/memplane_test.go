package ferrytest_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// The fixtures these tests address the plane through.
//
// Every address below is compiled from one of them rather than written out,
// because the three address kinds are sealed and the schema compiler is the
// only thing that mints one (ADR-0016). That is stronger than the literals it
// replaces: a test can no longer assert about an address ferry would never
// produce.
type (
	// memDB is one section over one leaf: /db and /db/host.
	memDB struct {
		DB memHost `ferry:"db"`
	}

	memHost struct {
		Host string `ferry:"host"`
	}

	// memNamedZero puts a Name segment whose text is "0" under /tags, which is
	// the half of the segment-kind pair a flat key space cannot hold.
	memNamedZero struct {
		Tags memZero `ferry:"tags"`
	}

	memZero struct {
		Zero string `ferry:"0"`
	}

	// memIndexedZero puts position 0 under the same /tags, through an array,
	// whose element addresses come from the type.
	memIndexedZero struct {
		Tags [1]string `ferry:"tags"`
	}

	// memFolds is the composite three case-variant keys are minted under.
	memFolds struct {
		Folds map[string]string `ferry:"folds"`
	}

	// memThree is three leaves, for the refusal that names each of them.
	memThree struct {
		Host string `ferry:"host"`
		Port string `ferry:"port"`
		Rate string `ferry:"rate"`
	}

	// memTags is the sequence the twelve indices are minted under.
	memTags struct {
		Tags []string `ferry:"tags"`
	}

	// memLabels is a leaf under a section no other fixture here binds.
	memLabels struct {
		Labels memEnv `ferry:"labels"`
	}

	memEnv struct {
		Env string `ferry:"env"`
	}

	// memLimits is a composite whose members are themselves composites, which
	// is the shape a nested container makes.
	memLimits struct {
		Limits map[string][]string `ferry:"limits"`
		Name   string              `ferry:"name"`
	}

	// memOptional is Go's present-but-empty section: a non-nil pointer whose
	// every field is omitted.
	memOptional struct {
		Opts *memOpts `ferry:"opts"`
	}

	memOpts struct {
		Name string `ferry:"name,omitzero"`
	}

	// memStaticCfg is what the fixed-contents plane is read into.
	memStaticCfg struct {
		Port    int    `ferry:"port"`
		Timeout string `ferry:"timeout"`
	}
)

// openPlane mints a fresh memory plane and opens both halves of it over one
// schema's address set.
//
// Fresh per call, never shared between subtests: a destination shared across
// equivalence cases is the defect that hides a broken second walk, and this
// plane refuses a duplicate write, so sharing one would also turn every second
// case into a spurious refusal.
func openPlane(t *testing.T, set *ferry.AddressSet) (ferry.Reader, ferry.Writer) {
	t.Helper()

	inst := ferrytest.MemPlane().Open()

	return openReader(t, inst.Source, set), openWriter(t, inst.Sink, set)
}

func openReader(t *testing.T, src ferry.Source, set *ferry.AddressSet) ferry.Reader {
	t.Helper()

	open, err := src.Bind(set)
	if err != nil {
		t.Fatalf("Source.Bind: %v", err)
	}

	r, err := open(t.Context())
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}

	return r
}

func openWriter(t *testing.T, snk ferry.Sink, set *ferry.AddressSet) ferry.Writer {
	t.Helper()

	open, err := snk.Bind(set)
	if err != nil {
		t.Fatalf("Sink.Bind: %v", err)
	}

	w, err := open(t.Context())
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}

	return w
}

// leafOf and compositeOf read one typed address back off a compiled set.
//
// The types are exported and only their construction is sealed, so a package
// outside ferry can hold one it was given and can ask a set for one, which is
// what these do.
func leafOf(t *testing.T, set *ferry.AddressSet, at ferry.Path) ferry.LeafAddr {
	t.Helper()

	for m := range set.Seq() {
		if a, ok := m.(ferry.LeafAddr); ok && a.Path() == at {
			return a
		}
	}

	t.Fatalf("the compiled set holds no leaf at %s", at)

	return ferry.LeafAddr{}
}

func compositeOf(t *testing.T, set *ferry.AddressSet, at ferry.Path) ferry.CompositeAddr {
	t.Helper()

	for m := range set.Seq() {
		if a, ok := m.(ferry.CompositeAddr); ok && a.Path() == at {
			return a
		}
	}

	t.Fatalf("the compiled set holds no composite at %s", at)

	return ferry.CompositeAddr{}
}

func mustSet(t *testing.T, w ferry.Writer, addr ferry.LeafAddr, v ferry.Value) {
	t.Helper()

	if err := w.Set(t.Context(), addr, v); err != nil {
		t.Fatalf("Set(%s): %v", addr, err)
	}
}

func mustGet(t *testing.T, r ferry.Reader, addr ferry.LeafAddr) ferry.Value {
	t.Helper()

	v, err := r.Get(t.Context(), addr)
	if err != nil {
		t.Fatalf("Get(%s): %v", addr, err)
	}

	return v
}

func mustChildren(t *testing.T, r ferry.Reader, addr ferry.CompositeAddr) []ferry.Segment {
	t.Helper()

	e, ok := r.(ferry.Enumerator)
	if !ok {
		t.Fatalf("%T does not implement ferry.Enumerator", r)
	}

	kids, err := e.Children(t.Context(), addr)
	if err != nil {
		t.Fatalf("Children(%s): %v", addr, err)
	}

	return kids
}

func mustProbe(t *testing.T, r ferry.Reader, addr ferry.Container) ferry.Presence {
	t.Helper()

	pr, ok := r.(ferry.Prober)
	if !ok {
		t.Fatalf("%T does not implement ferry.Prober", r)
	}

	info, err := pr.Probe(t.Context(), addr)
	if err != nil {
		t.Fatalf("Probe(%s): %v", addr, err)
	}

	return info.Presence()
}

// TestMemPlaneSeparatesTheSegmentKinds is the half of ADR-0003's first
// obligation that a flat key space cannot hold: a map key spelled "0" and
// position 0 render differently, so they are two slots here, where a plane
// keying on text alone would turn the map into a sequence and destroy the key.
//
// The two addresses come from two schemas that name the same container, which
// is what makes them the same prefix with two different steps under it.
func TestMemPlaneSeparatesTheSegmentKinds(t *testing.T) {
	named := leafOf(t, capturedSet[memNamedZero](t), ferry.At("tags", "0"))
	indexed := leafOf(t, capturedSet[memIndexedZero](t), ferry.At("tags").Elem(0))

	r, w := openPlane(t, capturedSet[memNamedZero](t))

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
	set := capturedSet[memDB](t)
	r, _ := openPlane(t, set)

	addr := leafOf(t, set, ferry.At("db", "host"))

	got, err := r.Get(t.Context(), addr)
	if err != nil || got.Kind() != ferry.KindAbsent {
		t.Errorf("Get at an unwritten address = %#v, %v, want absent and no error", got, err)
	}
}

// TestMemPlaneNeverFolds is ADR-0003's second obligation, and it is the viper
// defect turned into a property: three case-variant keys are three entries,
// each holding its own value, and none of them is reachable through another's
// spelling.
//
// They are map keys rather than tagged fields, because what a value mints is
// where a folding plane does its damage: a tagged address is in the compiled
// set and a minted one is not.
func TestMemPlaneNeverFolds(t *testing.T) {
	inst := ferrytest.MemPlane().Open()

	variants := map[string]string{"Host": "Host", "host": "host", "HOST": "HOST"}

	if err := ferry.Dump(t.Context(), memFolds{Folds: variants}, inst.Sink); err != nil {
		t.Fatalf("dumping three case-variant keys: %v", err)
	}

	back, err := ferry.Load[memFolds](t.Context(), inst.Source)
	if err != nil {
		t.Fatalf("loading them back: %v", err)
	}

	if !maps.Equal(back.Folds, variants) {
		t.Errorf("the round trip gave %v, want %v: a fold would have collapsed them", back.Folds, variants)
	}

	// Segment text compares by exact bytes, so the order is the byte order of
	// the three spellings and not a folded one.
	set := capturedSet[memFolds](t)
	want := []ferry.Segment{
		ferry.NameSegment("HOST"), ferry.NameSegment("Host"), ferry.NameSegment("host"),
	}

	got := mustChildren(t, openReader(t, inst.Source, set), compositeOf(t, set, ferry.At("folds")))
	if !slices.Equal(got, want) {
		t.Errorf("Children(/folds) = %v, want %v", got, want)
	}
}

// TestMemPlaneRefusesADuplicateWrite is ADR-0003's third obligation. The
// refusal is loud, it answers to the plane class, and the value already there
// survives it - a plane that kept the last writer would report success for a
// dump that lost a field. Naming the address is the test below.
func TestMemPlaneRefusesADuplicateWrite(t *testing.T) {
	set := capturedSet[memDB](t)
	r, w := openPlane(t, set)

	addr := leafOf(t, set, ferry.At("db", "host"))
	mustSet(t, w, addr, ferry.String("first"))

	err := w.Set(t.Context(), addr, ferry.String("second"))
	if err == nil {
		t.Fatal("a second write at one address was accepted, want a refusal")
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("refusal = %v, want one matching ferry.ErrPlane", err)
	}

	if got := mustGet(t, r, addr); got != ferry.String("first") {
		t.Errorf("Get(%s) = %#v, want the first write to have survived", addr, got)
	}
}

// TestMemPlaneNamesTheAddressItRefused is the other half of that obligation,
// asserted where a caller reads it. The address is attached with
// [ferry.ErrorAt], which makes it data for core rather than text the plane
// wrote, so what names it is the ferry error a dump reports.
//
// The duplicate is forced by holding one writer across two dumps, because that
// is what a duplicate write now is: what an earlier open wrote is a previous
// dump, and a binding may be dumped through any number of times.
func TestMemPlaneNamesTheAddressItRefused(t *testing.T) {
	sink := oneWriter{inner: ferrytest.MemPlane().Open().Sink}
	cfg := memThree{Host: "h", Port: "1", Rate: "2"}

	if err := ferry.Dump(t.Context(), cfg, &sink); err != nil {
		t.Fatalf("the first dump failed: %v", err)
	}

	err := ferry.Dump(t.Context(), cfg, &sink)

	els := ferry.Elements(err)
	if len(els) != 3 {
		t.Fatalf("a second dump through one writer gave %d elements, want 3:\n%+v", len(els), err)
	}

	for i, want := range []ferry.Path{ferry.At("host"), ferry.At("port"), ferry.At("rate")} {
		checkRefusalAt(t, els[i], want)
	}
}

// oneWriter is a sink shell that hands every open the same writer, which is
// how a test reaches two writes at one address inside one writer's lifetime
// now that the memory plane scopes its refusal to the open.
//
// It is not a shape a driver should have. It is the fixture that makes the
// refusal observable through [ferry.Dump], where core turns the address the
// plane attached into the address a caller reads.
type oneWriter struct {
	inner ferry.Sink
	w     ferry.Writer
}

func (s *oneWriter) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Writer, error) {
		if s.w == nil {
			s.w, err = open(ctx)
		}

		return s.w, err
	}, nil
}

// TestMemPlaneTakesASecondDumpThroughOneBinding is ADR-0004's reusable binding
// held to by the plane whose whole job is to be the reference implementation.
//
// One SinkBinding dumps any number of times, with a different value each time,
// and the second dump writes the addresses the first one wrote. A duplicate
// refusal that outlived its open made the reference plane refuse the reload it
// exists to demonstrate.
func TestMemPlaneTakesASecondDumpThroughOneBinding(t *testing.T) {
	inst := ferrytest.MemPlane().Open()

	b, err := ferry.BindSink[memThree](inst.Sink)
	if err != nil {
		t.Fatalf("BindSink: %v", err)
	}

	if err := b.Dump(t.Context(), memThree{Host: "h", Port: "1", Rate: "2"}); err != nil {
		t.Fatalf("the first dump failed: %v", err)
	}

	if err := b.Dump(t.Context(), memThree{Host: "h2", Port: "2", Rate: "3"}); err != nil {
		t.Fatalf("the second dump through one binding failed: %v", err)
	}

	back, err := ferry.Load[memThree](t.Context(), inst.Source)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if want := (memThree{Host: "h2", Port: "2", Rate: "3"}); back != want {
		t.Errorf("the plane holds %+v, want the second dump's value %+v", back, want)
	}
}

func checkRefusalAt(t *testing.T, el error, want ferry.Path) {
	t.Helper()

	e, ok := errors.AsType[*ferry.Error](el)
	if !ok || e.Address() != want {
		t.Errorf("element = %v, want a ferry error at %s", el, want)
	}

	if !errors.Is(el, ferry.ErrPlane) {
		t.Errorf("the refusal at %s does not answer to ferry.ErrPlane:\n%+v", want, el)
	}
}

// TestMemPlaneEnumeratesSegmentWise is ADR-0003's fourth obligation, over the
// twelve indices that make the difference visible: sorted as text they are
// 0 1 10 11 2 3, and segment-wise they are 0 1 2 3 10 11.
func TestMemPlaneEnumeratesSegmentWise(t *testing.T) {
	inst := ferrytest.MemPlane().Open()

	tags := make([]string, 12)
	for i := range tags {
		tags[i] = strconv.Itoa(i)
	}

	if err := ferry.Dump(t.Context(), memTags{Tags: tags}, inst.Sink); err != nil {
		t.Fatalf("dumping twelve positions: %v", err)
	}

	set := capturedSet[memTags](t)
	addr := compositeOf(t, set, ferry.At("tags"))
	r := openReader(t, inst.Source, set)

	want := make([]ferry.Segment, 0, len(tags))
	for i := range uint(len(tags)) {
		want = append(want, ferry.IndexSegment(i))
	}

	got := mustChildren(t, r, addr)
	if !slices.Equal(got, want) {
		t.Errorf("Children(%s) = %v, want %v", addr, got, want)
	}

	// Twice, because the obligation is that a test asserting on this plane's
	// contents is not asserting on Go's randomised map iteration order.
	if again := mustChildren(t, r, addr); !slices.Equal(again, got) {
		t.Errorf("Children(%s) is not stable: %v then %v", addr, got, again)
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
	set := capturedSet[collidingCfg](t)
	r, w := openPlane(t, set)

	flat := leafOf(t, set, ferry.At("db_host"))
	structured := leafOf(t, set, ferry.At("db", "host"))

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

	// The set a schema determined is not what this plane keys on, and an
	// address outside it is still writable and readable - which is what stops a
	// precomputed table from refusing a legitimate map key.
	unknown := leafOf(t, capturedSet[memLabels](t), ferry.At("labels", "env"))

	w := openWriter(t, inst.Sink, capturedSet[memDB](t))
	mustSet(t, w, unknown, ferry.String("minted"))

	openRead, err := inst.Source.Bind(nil)
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
// the members of a container are one segment deeper, whatever their kind, and
// what is under each of them is enumerated in turn.
//
// The nested half is asserted through a round trip rather than through a second
// Children call, because the address of a member is minted from the value and
// the walk is the only thing that holds one.
func TestMemPlaneEnumeratesContainers(t *testing.T) {
	inst := ferrytest.MemPlane().Open()

	cfg := memLimits{
		Limits: map[string][]string{"http": {"1", "2"}, "grpc": {"3"}},
		Name:   "svc",
	}

	if err := ferry.Dump(t.Context(), cfg, inst.Sink); err != nil {
		t.Fatalf("dumping a nested composite: %v", err)
	}

	set := capturedSet[memLimits](t)
	limits := compositeOf(t, set, ferry.At("limits"))

	want := []ferry.Segment{ferry.NameSegment("grpc"), ferry.NameSegment("http")}
	if got := mustChildren(t, openReader(t, inst.Source, set), limits); !slices.Equal(got, want) {
		t.Errorf("Children(%s) = %v, want %v", limits, got, want)
	}

	back, err := ferry.Load[memLimits](t.Context(), inst.Source)
	if err != nil {
		t.Fatalf("loading the nested composite back: %v", err)
	}

	if !maps.EqualFunc(back.Limits, cfg.Limits, slices.Equal) || back.Name != cfg.Name {
		t.Errorf("the round trip gave %+v, want %+v", back, cfg)
	}
}

// TestMemPlaneAnswersAtAContainersOwnAddress is the plane's half of the
// container question, which moved off Get and onto Probe with the sealed
// address model (ADR-0016).
//
// The three answers are three different things to a reload, and this plane has
// to keep them apart: a null a nil composite wrote, the presence a realised but
// empty section wrote, and the absence of an address nothing reached.
func TestMemPlaneAnswersAtAContainersOwnAddress(t *testing.T) {
	inst := ferrytest.MemPlane().Open()

	if err := ferry.Dump(t.Context(), memTags{}, inst.Sink); err != nil {
		t.Fatalf("dumping a nil sequence: %v", err)
	}

	set := capturedSet[memTags](t)

	r := openReader(t, inst.Source, set)
	if got := mustProbe(t, r, compositeOf(t, set, ferry.At("tags"))); got != ferry.PresenceNull {
		t.Errorf("Probe at a nil sequence's own address = %s, want %s", got, ferry.PresenceNull)
	}

	other := ferrytest.MemPlane().Open()
	if got := mustProbe(t, openReader(t, other.Source, set),
		compositeOf(t, set, ferry.At("tags"))); got != ferry.PresenceAbsent {
		t.Errorf("Probe at an address nothing wrote = %s, want %s", got, ferry.PresenceAbsent)
	}
}

// TestMemPlaneCarriesAPresentButEmptySection is what the section-level write
// buys: Go can express a non-nil pointer whose every field is omitted, and
// without an answer at the section's own address the round trip would turn it
// into absence.
func TestMemPlaneCarriesAPresentButEmptySection(t *testing.T) {
	inst := ferrytest.MemPlane().Open()

	if err := ferry.Dump(t.Context(), memOptional{Opts: &memOpts{}}, inst.Sink); err != nil {
		t.Fatalf("dumping a realised but empty section: %v", err)
	}

	back, err := ferry.Load[memOptional](t.Context(), inst.Source)
	if err != nil {
		t.Fatalf("loading it back: %v", err)
	}

	if back.Opts == nil {
		t.Error("a present-but-empty section reloaded as absent, which is the divergence the section-level " +
			"write exists to close")
	}
}

// TestMemWriterHasNoLifecycle asserts what ADR-0004's own table says about the
// recorder row: it stages nothing and holds nothing, so it implements neither
// optional interface. A Commit here would be a lie, and a Close would be the
// `return nil` boilerplate that reads in the source exactly like a rollback
// somebody forgot.
//
// It does spell a container at its own address, and that is the one capability
// a map of Values genuinely has: the answer is a slot in the map like any other.
func TestMemWriterHasNoLifecycle(t *testing.T) {
	r, w := openPlane(t, capturedSet[memDB](t))

	if _, ok := w.(ferry.Committer); ok {
		t.Errorf("%T implements ferry.Committer, want it not to", w)
	}

	if _, ok := w.(ferry.Releaser); ok {
		t.Errorf("%T implements ferry.Releaser, want it not to", w)
	}

	if _, ok := r.(ferry.Releaser); ok {
		t.Errorf("%T implements ferry.Releaser, want it not to", r)
	}

	if _, ok := w.(ferry.Ensurer); !ok {
		t.Errorf("%T does not implement ferry.Ensurer, so a nil section could not be written", w)
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

	cfg, err := ferry.Load[memStaticCfg](t.Context(), src)
	if err != nil {
		t.Fatalf("loading from fixed contents: %v", err)
	}

	if cfg.Port != 8080 || cfg.Timeout != "30s" {
		t.Errorf("Load gave %+v, want the contents the source was built with", cfg)
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

	set := capturedSet[memDB](t)
	addr := leafOf(t, set, ferry.At("db", "host"))

	// Fresh per Open, or one suite's cases would refuse each other's writes.
	first := p.Open()
	mustSet(t, openWriter(t, first.Sink, set), addr, ferry.String("1"))

	second := p.Open()
	if got := mustGet(t, openReader(t, second.Source, set), addr); got.Kind() != ferry.KindAbsent {
		t.Errorf("a second Open sees %#v at %s, want absent", got, addr)
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

// memNested is a mapping whose members are themselves composites, which is the
// only shape that puts a container's own answer *underneath* another container.
type memNested struct {
	Groups map[string][]string `ferry:"groups"`
}

// TestMemPlaneForgetsTheMarksUnderACompositeToo is the half of forgetting that a
// scan over the stored values alone does not reach.
//
// A nil member of a mapping writes a null at its own address, and this plane
// keeps that as a mark rather than as an entry, so a dump that replaced the
// mapping and dropped only the entries would leave the mark behind. What the
// next load then sees is a member the value no longer has, reported as present
// and null, which is the residue the whole capability exists to remove.
func TestMemPlaneForgetsTheMarksUnderACompositeToo(t *testing.T) {
	inst := ferrytest.MemPlane().Open()

	b, err := ferry.BindSink[memNested](inst.Sink)
	if err != nil {
		t.Fatalf("BindSink: %v", err)
	}

	if err := b.Dump(t.Context(), memNested{Groups: map[string][]string{"gone": nil, "kept": {"a"}}}); err != nil {
		t.Fatalf("the first dump failed: %v", err)
	}

	if err := b.Dump(t.Context(), memNested{Groups: map[string][]string{"kept": {"b"}}}); err != nil {
		t.Fatalf("the second dump failed: %v", err)
	}

	back, err := ferry.Load[memNested](t.Context(), inst.Source)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if _, held := back.Groups["gone"]; held {
		t.Errorf("the plane loads back %+v: the member the second value dropped was written as a null at its "+
			"own address, and a mark the replacement did not forget is a member that outlived its value",
			back.Groups)
	}
}
