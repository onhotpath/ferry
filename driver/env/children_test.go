package env

import (
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/onhotpath/ferry"
)

// TestChildrenStaticTierRecoversTheSpelling is issue #140's first half, and the
// reason it is a lookup rather than an inverse.
//
// The fold is many-to-one, so FEATURE_FLAGS is the name of /db/feature-flags and
// of /db/feature_flags alike and no inverse can tell them apart. It does not have
// to: the address the schema determined is in the set this binding was built
// from, so the name is matched against the precomputed table and the segment's
// own spelling comes back exactly.
func TestChildrenStaticTierRecoversTheSpelling(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["DB_FEATURE_FLAGS"] = "on"

	prefix := ferry.At("db")
	leaf := prefix.At("feature-flags")

	got := mustChildren(t, e, []ferry.Path{prefix, leaf}, prefix)

	if !slices.Equal(got, []ferry.Path{leaf}) {
		t.Errorf("Children(%s) = %v, want %v: a tagged field's address is in the compiled set, so the fold "+
			"never has to be undone", prefix, got, []ferry.Path{leaf})
	}
}

// TestChildrenStaticTierIsNotFooledByASharedPrefix holds the static tier to
// answering about addresses rather than about text.
//
// /value_x renders VALUE_X, which begins with the text every child of /value
// begins with, and it is not a child of /value: it is a sibling whose own name
// happens to start alike. The address set says so, and the driver reads it there.
func TestChildrenStaticTierIsNotFooledByASharedPrefix(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["VALUE_X"] = "1"

	prefix := ferry.At("value")

	got := mustChildren(t, e, []ferry.Path{prefix, ferry.At("value_x")}, prefix)

	if len(got) != 0 {
		t.Errorf("Children(%s) = %v, want nothing: /value_x is a sibling of /value and not a child of it",
			prefix, got)
	}
}

// TestChildrenDynamicTierUsesTheCanonicalForm is issue #140's second half: a map
// key is minted by the value, so it is in no compiled set, and the form it comes
// back in is the caller's choice rather than an accident.
func TestChildrenDynamicTierUsesTheCanonicalForm(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		opts []Option
		want []ferry.Path
	}{
		"lower by default": {
			want: []ferry.Path{ferry.At("limits", "http"), ferry.At("limits", "rps")},
		},
		"upper when it is asked for": {
			opts: []Option{Canonical(Upper)},
			want: []ferry.Path{ferry.At("limits", "HTTP"), ferry.At("limits", "RPS")},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e := newEnviron()
			e.vars["LIMITS_HTTP"] = "1"
			e.vars["LIMITS_RPS"] = "2"

			prefix := ferry.At("limits")

			got := mustChildren(t, e, []ferry.Path{prefix}, prefix, tc.opts...)
			if !slices.Equal(got, tc.want) {
				t.Errorf("Children(%s) = %v, want %v", prefix, got, tc.want)
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

	type config struct {
		Limits map[string]string `ferry:"limits"`
	}

	e := newEnviron()
	e.vars["LIMITS_HTTP"] = "1"

	got, err := ferry.Load[config](t.Context(), New(Environ(e.environ)))
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
// guess, and the rule is jsontext.Pointer's own admitted limitation: an address
// carries its segment kind and a name does not, so canonical base 10 is a
// position and everything else is a member.
//
// The leading-zero row is why the rule is canonical base 10 rather than "parses
// as a number": "01" is the rendering of no position, so reading it as one would
// answer about /list#1 instead, which is a different address.
func TestChildrenReadsAPositionAsAPosition(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		name string
		want ferry.Path
	}{
		"zero":             {name: "LIST_0", want: ferry.At("list").Elem(0)},
		"past ten":         {name: "LIST_11", want: ferry.At("list").Elem(11)},
		"a leading zero":   {name: "LIST_01", want: ferry.At("list").At("01")},
		"not a number":     {name: "LIST_A", want: ferry.At("list").At("a")},
		"digits and text":  {name: "LIST_1A", want: ferry.At("list").At("1a")},
		"more than a uint": {name: "LIST_99999999999999999999", want: ferry.At("list").At("99999999999999999999")},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e := newEnviron()
			e.vars[tc.name] = "v"

			prefix := ferry.At("list")

			got := mustChildren(t, e, []ferry.Path{prefix}, prefix)
			if !slices.Equal(got, []ferry.Path{tc.want}) {
				t.Errorf("Children(%s) = %v, want %v", prefix, got, []ferry.Path{tc.want})
			}
		})
	}
}

// TestChildrenStaticTierKeepsAPositionAPosition is the static half of the same
// question: an array's element addresses come from the type, so they are in the
// compiled set, and the kind comes back off the address rather than off a guess
// about the text.
func TestChildrenStaticTierKeepsAPositionAPosition(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["ARR_0"] = "a"
	e.vars["ARR_1"] = "b"

	prefix := ferry.At("arr")
	want := []ferry.Path{prefix.Elem(0), prefix.Elem(1)}

	got := mustChildren(t, e, append([]ferry.Path{prefix}, want...), prefix)
	if !slices.Equal(got, want) {
		t.Errorf("Children(%s) = %v, want %v", prefix, got, want)
	}
}

// TestChildrenSkipsANameWithNothingUnderIt covers the one shape an environment
// can hold that names no child: a variable whose name is the prefix followed by
// the separator and nothing else.
//
// It is a variable an operator can set and no address renders to, so it is not a
// child of anything and is dropped rather than turned into an empty segment -
// which is a name this plane has already refused to spell.
func TestChildrenSkipsANameWithNothingUnderIt(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["LIST_"] = "x"
	e.vars["LIST_0"] = "a"

	prefix := ferry.At("list")
	want := []ferry.Path{prefix.Elem(0)}

	got := mustChildren(t, e, []ferry.Path{prefix}, prefix)
	if !slices.Equal(got, want) {
		t.Errorf("Children(%s) = %v, want %v", prefix, got, want)
	}
}

// TestChildrenSortsSegmentWise pins the order, which is not the order the
// rendering sorts in: twelve positions as text give 0 1 10 11 2, and segment-wise
// they give 0 1 2 ... 11, which is what the walk needs and what a human diffing a
// load expects (ADR-0003).
func TestChildrenSortsSegmentWise(t *testing.T) {
	t.Parallel()

	const positions = 12

	e := newEnviron()
	prefix := ferry.At("list")
	want := make([]ferry.Path, 0, positions)

	for i := range uint(positions) {
		e.vars["LIST_"+decimal(i)] = "v"
		want = append(want, prefix.Elem(i))
	}

	got := mustChildren(t, e, []ferry.Path{prefix}, prefix)
	if !slices.Equal(got, want) {
		t.Errorf("Children(%s) = %v, want %v", prefix, got, want)
	}
}

// TestChildrenAtTheRootListsTheTopLevel is the one prefix that names no address.
//
// A caller asking what is under nothing is asking for everything, and answering
// with a refusal would make the root of a plane the one place enumeration does
// not work.
func TestChildrenAtTheRootListsTheTopLevel(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["DB_HOST"] = "h"
	e.vars["NAME"] = "n"

	got := mustChildren(t, e, []ferry.Path{ferry.At("db", "host"), ferry.At("name")}, ferry.Path{})

	want := []ferry.Path{ferry.At("db"), ferry.At("name")}
	if !slices.Equal(got, want) {
		t.Errorf("Children at the root = %v, want %v", got, want)
	}
}

// TestChildrenRefusesAPrefixThePlaneCannotName holds enumeration to the same
// legality rule every other lookup answers to, rather than quietly listing
// nothing.
func TestChildrenRefusesAPrefixThePlaneCannotName(t *testing.T) {
	t.Parallel()

	r, err := bound(newEnviron(), []ferry.Path{ferry.At("labels")})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	lister, ok := r.(ferry.Enumerator)
	if !ok {
		t.Fatal("the reader does not enumerate")
	}

	if _, err = lister.Children(t.Context(), ferry.At("labels", "")); !errors.Is(err, ErrIllegalName) {
		t.Errorf("Children under an unnameable prefix failed with %v, want the legality refusal", err)
	}
}

// mustChildren binds one environment, opens it and lists one prefix.
func mustChildren(t *testing.T, e *fakeEnviron, addrs []ferry.Path, prefix ferry.Path, opts ...Option) []ferry.Path {
	t.Helper()

	r, err := bound(e, addrs, opts...)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	lister, ok := r.(ferry.Enumerator)
	if !ok {
		t.Fatal("the reader does not enumerate, and a map-typed field cannot be loaded from a plane that " +
			"cannot list")
	}

	got, err := lister.Children(t.Context(), prefix)
	if err != nil {
		t.Fatalf("Children(%s): %v", prefix, err)
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
