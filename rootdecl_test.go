package ferry

import (
	"errors"
	"strings"
	"testing"
)

// Every assertion in this file goes through Load, LoadOver and Dump. The root
// declaration is a statement about what the plane was asked at the root address
// and what came back, so it is asserted as exactly that and never by reading a
// compiled node.

// rootPort is the leaf root the declaration is asserted on. It is a named type
// of this file's own, so nothing else in the package shares a schema cache
// entry with it.
type rootPort int

// rootHost is the struct root, where required means the plane supplied at least
// one of the root's children.
type rootHost struct {
	Host string `ferry:"host"`
}

// TestARootLeafIsNotRequiredByDefault is the baseline the Option moves off.
//
// The root has no tag, so nothing is declared there unless the Option list
// declares it, and a silent plane is an absence like any other: it does not
// write, and the destination keeps what it had.
func TestARootLeafIsNotRequiredByDefault(t *testing.T) {
	t.Parallel()

	got, err := Load[int](t.Context(), silentSource())
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got != 0 {
		t.Errorf("loaded %d from a silent plane, want the zero value", got)
	}
}

// TestRootRequiredRefusesASilentPlane is the Option doing its one job.
//
// The failure is ErrMissing and not ErrPlane: nothing went wrong with the
// plane, which answered the question it was asked, and what is refused is the
// answer.
func TestRootRequiredRefusesASilentPlane(t *testing.T) {
	t.Parallel()

	_, err := Load[int](t.Context(), silentSource(), RootRequired)
	if err == nil {
		t.Fatal("a silent plane under RootRequired returned no error")
	}

	if !strings.Contains(reportOf(err), "required, and nothing is set here") {
		t.Errorf("the report is %+v, want it to say the root holds nothing", err)
	}

	if !errors.Is(err, ErrMissing) {
		t.Errorf("%+v is not an ErrMissing", err)
	}

	if errors.Is(err, ErrPlane) {
		t.Errorf("%+v is an ErrPlane, and the plane answered the question it was asked", err)
	}
}

// TestRootRequiredIsSatisfiedByAnyObservation is what required means and the
// whole of what it means: a presence test, answered by the plane having spoken
// at the address, whatever it said.
func TestRootRequiredIsSatisfiedByAnyObservation(t *testing.T) {
	t.Parallel()

	t.Run("a value at the root", aValueAtTheRootSatisfiesRequired)
	t.Run("and an explicit empty, which is an observation too", anEmptyAtTheRootSatisfiesRequired)
}

func aValueAtTheRootSatisfiesRequired(t *testing.T) {
	t.Parallel()

	src := planeSource{p: newPlane(map[Path]Value{{}: Number("8080")})}

	got, err := Load[int](t.Context(), src, RootRequired)
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got != 8080 {
		t.Errorf("loaded %d, want the number the plane held", got)
	}
}

func anEmptyAtTheRootSatisfiesRequired(t *testing.T) {
	t.Parallel()

	src := planeSource{p: newPlane(map[Path]Value{{}: String("")})}

	got, err := Load[string](t.Context(), src, RootRequired)
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got != "" {
		t.Errorf("loaded %q, want the empty text the plane held", got)
	}
}

// TestRootRequiredTwiceIsRefused holds the Option to the rule every other one
// obeys, and the refusal names it so that the caller can find it in a list of
// four.
func TestRootRequiredTwiceIsRefused(t *testing.T) {
	t.Parallel()

	_, err := Load[int](t.Context(), silentSource(), RootRequired, RootRequired)
	if err == nil {
		t.Fatal("RootRequired supplied twice returned no error")
	}

	if !errors.Is(err, ErrSchema) {
		t.Errorf("%+v is not an ErrSchema", err)
	}

	if !strings.Contains(reportOf(err), "ferry.RootRequired") {
		t.Errorf("the report is %+v, want it to name the Option that was given twice", err)
	}
}

// TestASeedIsTheRootsDefault is what stands in for a declared default at the
// one address no tag can name.
//
// A default below the root is text on a tag, decoded through the leaf's own
// parser on every load. The root has no tag, and the seed is better typed than
// a text would have been: it is already a T.
func TestASeedIsTheRootsDefault(t *testing.T) {
	t.Parallel()

	got, err := LoadOver(t.Context(), 4242, silentSource())
	if err != nil {
		t.Fatalf("load over: %+v", err)
	}

	if got != 4242 {
		t.Errorf("loaded %d from a silent plane, want the seed", got)
	}
}

// TestRootRequiredFiresEvenWithASeed is the composition that looks
// contradictory and is not, which is why it is pinned here.
//
// required is a presence test about the plane, satisfied by any observation
// other than Absent, and a seed is not an observation: it is the value the walk
// starts from. So a reload carries the previous value forward and still refuses
// where the plane went silent, which is the shape a reload wants and no
// declared default could give it.
func TestRootRequiredFiresEvenWithASeed(t *testing.T) {
	t.Parallel()

	got, err := LoadOver(t.Context(), 4242, silentSource(), RootRequired)
	if err == nil {
		t.Fatal("a silent plane under a seed and RootRequired returned no error")
	}

	if !errors.Is(err, ErrMissing) {
		t.Errorf("%+v is not an ErrMissing", err)
	}

	if got != 4242 {
		t.Errorf("the failed load returned %d, want the seed it was handed", got)
	}
}

// TestRootRequiredOnAStructRoot is the Option at the other root shape, where it
// means what required means at every other section address.
func TestRootRequiredOnAStructRoot(t *testing.T) {
	t.Parallel()

	t.Run("a silent plane is refused", aSilentPlaneUnderAStructRootIsRefused)
	t.Run("and one child is enough to satisfy it", oneChildSatisfiesAStructRoot)
}

func aSilentPlaneUnderAStructRootIsRefused(t *testing.T) {
	t.Parallel()

	_, err := Load[rootHost](t.Context(), silentSource(), RootRequired)
	if err == nil {
		t.Fatal("a silent plane under RootRequired returned no error")
	}

	if !strings.Contains(reportOf(err), "required, and nothing is set under it") {
		t.Errorf("the report is %+v, want it to say nothing was supplied under the root", err)
	}

	if !errors.Is(err, ErrMissing) {
		t.Errorf("%+v is not an ErrMissing", err)
	}
}

func oneChildSatisfiesAStructRoot(t *testing.T) {
	t.Parallel()

	src := planeSource{p: newPlane(map[Path]Value{At("host"): String("db.internal")})}

	got, err := Load[rootHost](t.Context(), src, RootRequired)
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got.Host != "db.internal" {
		t.Errorf("loaded %q, want what the plane held under the root", got.Host)
	}
}

// TestTwoRootDeclarationsKeyTwoSchemas is the cache rule for the new member.
//
// One type compiles to a required root under the Option and to an optional one
// without it, so a cache that could not tell the two apart would hand the
// second caller the first caller's schema. The third load is the half that
// catches the failure in the other direction: the entry the first load stored
// is still the required one.
func TestTwoRootDeclarationsKeyTwoSchemas(t *testing.T) {
	t.Parallel()

	if _, err := Load[rootPort](t.Context(), silentSource(), RootRequired); err == nil {
		t.Fatal("the declared load returned no error, so the declaration was lost")
	}

	got, err := Load[rootPort](t.Context(), silentSource())
	if err != nil {
		t.Fatalf("the undeclared load took the declared schema: %+v", err)
	}

	if got != 0 {
		t.Errorf("loaded %d from a silent plane, want the zero value", got)
	}

	if _, err := Load[rootPort](t.Context(), silentSource(), RootRequired); err == nil {
		t.Error("the declared load stopped refusing, so it took the undeclared schema")
	}
}

// TestRootRequiredIsLoadOnly is the sharp edge stated as behaviour: a dump takes
// the Option and writes what it was given, because requiredness is a question
// only a load asks.
func TestRootRequiredIsLoadOnly(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})

	if err := Dump(t.Context(), 0, planeSink{p: p}, RootRequired); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	mustBeAddresses(t, p.set, []string{""})

	if got := p.values[Path{}]; got != Number("0") {
		t.Errorf("the plane holds %#v at the root, want the zero value the dump was given", got)
	}
}

// TestRootRequiredCannotBeReassigned is the immutability of the exported var,
// which is a compile fact and has no run-time behaviour to observe.
//
// The exact wording of a compiler diagnostic is Go's rather than ferry's, so
// what is held is the phrase that names the fault and the identifier it is
// about.
func TestRootRequiredCannotBeReassigned(t *testing.T) {
	t.Parallel()

	mustNotCompile(t, "./internal/testdata/rootoption/reassigned",
		[]string{"cannot use ferry.TagKey", "ferry.rootRequired value in assignment"})
}

// silentSource is a plane that holds nothing, built fresh per call because a
// plane shared across loads is the same defect as a destination shared across
// subtests.
func silentSource() planeSource {
	return planeSource{p: newPlane(map[Path]Value{})}
}
