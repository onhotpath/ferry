package env

import (
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/onhotpath/ferry"
)

// The schemas enumeration is read through.
//
// Enumeration is asked only at a container whose members come from the value,
// so every fixture here has one: nothing static ever sits beneath such an
// address, which is why the static tier these tests used to have is gone
// (ADR-0016). An array's members come from its type and are never enumerated at
// all.
type (
	limitsMap struct {
		Limits map[string]string `ferry:"limits"`
	}
	listSlice struct {
		List []string `ferry:"list"`
	}
	nestedMaps struct {
		Limits map[string]map[string]string `ferry:"limits"`
	}
)

// TestChildrenUsesTheCanonicalForm is issue #140's second half: a map key is
// minted by the value, so it is in no compiled set, and the form it comes back
// in is the caller's choice rather than an accident.
func TestChildrenUsesTheCanonicalForm(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		opts []Option
		want []ferry.Segment
	}{
		"lower by default": {
			want: []ferry.Segment{ferry.NameSegment("http"), ferry.NameSegment("rps")},
		},
		"upper when it is asked for": {
			opts: []Option{Canonical(Upper)},
			want: []ferry.Segment{ferry.NameSegment("HTTP"), ferry.NameSegment("RPS")},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e := newEnviron()
			e.vars["LIMITS_HTTP"] = "1"
			e.vars["LIMITS_RPS"] = "2"

			got := mustChildren[limitsMap](t, e, ferry.At("limits"), tc.opts...)
			if !slices.Equal(got, tc.want) {
				t.Errorf("Children(/limits) = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCanonicalIsDeterministicAndNotTotal is the honest statement of the
// guarantee, asserted rather than left in a doc comment.
//
// The fold is many-to-one, so no form round-trips every segment: what the option
// chooses is which subset does. A key already in the chosen form comes back
// unchanged; one that is not comes back changed, deterministically, and the
// refusal that would make it total belongs to the first env-family sink, because
// there is nothing here that writes.
func TestCanonicalIsDeterministicAndNotTotal(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["LIMITS_HTTP"] = "1"

	got, err := ferry.Load[limitsMap](t.Context(), New(Environ(e.environ)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// One name, one key, whichever of the three spellings the value once had:
	// "http", "HTTP" and "Http" all render LIMITS_HTTP, and lower is the form
	// the default names.
	if !maps.Equal(got.Limits, map[string]string{"http": "1"}) {
		t.Errorf("Limits = %v, want the canonical spelling of the one name the plane holds", got.Limits)
	}
}

// TestChildrenReadsAPositionAsAPosition is the one thing a flat plane has to
// guess, and the rule is jsontext.Pointer's own admitted limitation: a segment
// carries its kind and a name does not, so canonical base 10 is a position and
// everything else is a member.
//
// The leading-zero row is why the rule is canonical base 10 rather than "parses
// as a number": "01" is the spelling of no position, so reading it as one would
// answer about position 1 instead, which is a different address.
func TestChildrenReadsAPositionAsAPosition(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		name string
		want ferry.Segment
	}{
		"zero":             {name: "LIST_0", want: ferry.IndexSegment(0)},
		"past ten":         {name: "LIST_11", want: ferry.IndexSegment(11)},
		"a leading zero":   {name: "LIST_01", want: ferry.NameSegment("01")},
		"not a number":     {name: "LIST_A", want: ferry.NameSegment("a")},
		"digits and text":  {name: "LIST_1A", want: ferry.NameSegment("1a")},
		"more than a uint": {name: "LIST_99999999999999999999", want: ferry.NameSegment("99999999999999999999")},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e := newEnviron()
			e.vars[tc.name] = "v"

			got := mustChildren[listSlice](t, e, ferry.At("list"))
			if !slices.Equal(got, []ferry.Segment{tc.want}) {
				t.Errorf("Children(/list) = %v, want %v", got, []ferry.Segment{tc.want})
			}
		})
	}
}

// TestChildrenSkipsANameWithNothingUnderIt covers the one shape an environment
// can hold that names no member: a variable whose name is the container's
// followed by the separator and nothing else.
//
// It is a variable an operator can set and no address renders to, so it is not a
// member of anything and is dropped rather than turned into an empty segment -
// which is a name this plane has already refused to spell.
func TestChildrenSkipsANameWithNothingUnderIt(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["LIST_"] = "x"
	e.vars["LIST_0"] = "a"

	want := []ferry.Segment{ferry.IndexSegment(0)}

	got := mustChildren[listSlice](t, e, ferry.At("list"))
	if !slices.Equal(got, want) {
		t.Errorf("Children(/list) = %v, want %v", got, want)
	}
}

// TestChildrenSortsSegmentWise pins the order, which is not the order the text
// sorts in: twelve positions as text give 0 1 10 11 2, and segment-wise they
// give 0 1 2 ... 11, which is what the walk needs and what a human diffing a
// load expects (ADR-0003).
func TestChildrenSortsSegmentWise(t *testing.T) {
	t.Parallel()

	const positions = 12

	e := newEnviron()
	want := make([]ferry.Segment, 0, positions)

	for i := range uint(positions) {
		e.vars["LIST_"+decimal(i)] = "v"
		want = append(want, ferry.IndexSegment(i))
	}

	got := mustChildren[listSlice](t, e, ferry.At("list"))
	if !slices.Equal(got, want) {
		t.Errorf("Children(/list) = %v, want %v", got, want)
	}
}

// TestChildrenMintsTheHeadOfADeeperName is what keeps a map of maps loadable,
// and it is the half of #235 that must not change.
//
// A variable reaching deeper than a member is spelled exactly as a nested
// container is, and this plane cannot tell the two apart: LIMITS_HTTP_PORT is
// the member "http" holding a mapping, or it is nothing at all. Enumeration
// mints the head either way, and which it was is settled by the question core
// asks next.
func TestChildrenMintsTheHeadOfADeeperName(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["LIMITS_HTTP_PORT"] = "8080"

	want := []ferry.Segment{ferry.NameSegment("http")}

	got := mustChildren[nestedMaps](t, e, ferry.At("limits"))
	if !slices.Equal(got, want) {
		t.Errorf("Children(/limits) = %v, want %v", got, want)
	}
}

// TestAMapOfMapsLoads is the same fixture through the walk, which is where the
// head being minted has to pay off: the member is a container, so core asks for
// its members rather than for a value at it.
func TestAMapOfMapsLoads(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["LIMITS_HTTP_PORT"] = "8080"

	got, err := ferry.Load[nestedMaps](t.Context(), New(Environ(e.environ)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := map[string]map[string]string{"http": {"port": "8080"}}
	if len(got.Limits) != 1 || !maps.Equal(got.Limits["http"], want["http"]) {
		t.Errorf("Limits = %v, want %v", got.Limits, want)
	}
}

// TestAValueBelowAnElementIsRefused is #235, retired.
//
// LIMITS_HTTP_PORT under a map[string]string is a value the schema has no
// address for: there is no LIMITS_HTTP to read, so the only answers available
// are this refusal and a map entry holding the Go zero with 8080 dropped. It is
// a refusal because the second is silent loss.
func TestAValueBelowAnElementIsRefused(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["LIMITS_HTTP_PORT"] = "8080"

	_, err := ferry.Load[limitsMap](t.Context(), New(Environ(e.environ)))
	if !errors.Is(err, ErrDeeperThanLeaf) {
		t.Fatalf("Load = %v, want the refusal of a value below an element", err)
	}

	var e2 *ferry.Error
	if errors.As(err, &e2) && e2.Address() != ferry.At("limits", "http") {
		t.Errorf("the refusal names %s, want /limits/http", e2.Address())
	}
}

// TestAPositionBelowAnElementIsRefused is the sequence half of the same defect:
// TAGS_1_X mints position 1 and nothing is stored at TAGS_1, so the sequence
// would gain an element holding the Go zero.
func TestAPositionBelowAnElementIsRefused(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["LIST_0"] = "a"
	e.vars["LIST_1_X"] = "b"

	if _, err := ferry.Load[listSlice](t.Context(), New(Environ(e.environ))); !errors.Is(err, ErrDeeperThanLeaf) {
		t.Fatalf("Load = %v, want the refusal of a value below an element", err)
	}
}

// TestADeclaredLeafIgnoresAnUnrelatedNeighbour is the other side of the same
// rule, and it is why the refusal is scoped to a minted address.
//
// PATH_SEPARATOR is not this schema's business. A leaf the schema declared is
// read at its own name and nowhere else, so a variable that merely shares a
// prefix with it leaves the load alone - which is the defect the typed contract
// removed at a container address and must not reintroduce at a leaf.
func TestADeclaredLeafIgnoresAnUnrelatedNeighbour(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["HOST_SEPARATOR"] = "x"

	got, err := ferry.Load[oneHost](t.Context(), New(Environ(e.environ)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.Host != "" {
		t.Errorf("Host = %q, want the zero value: the environment holds nothing at HOST", got.Host)
	}
}

// mustChildren binds one environment to the addresses T names, opens it, and
// lists one container.
func mustChildren[T any](t *testing.T, e *fakeEnviron, at ferry.Path, opts ...Option) []ferry.Segment {
	t.Helper()

	g, err := boundTo[T](e, opts...)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	lister, ok := g.r.(ferry.Enumerator)
	if !ok {
		t.Fatal("the reader does not enumerate, and a map-typed field cannot be loaded from a plane that " +
			"cannot list")
	}

	got, err := lister.Children(t.Context(), g.composite(t, at))
	if err != nil {
		t.Fatalf("Children(%s): %v", at, err)
	}

	return got
}

// decimal spells a position the way ferry does, so the fixture and the driver
// agree on what a position looks like without either asking the other.
func decimal(i uint) string {
	if i == 0 {
		return "0"
	}

	var out []byte

	for i > 0 {
		out = append([]byte{byte('0' + i%base10)}, out...)
		i /= base10
	}

	return string(out)
}
