package ferryhttp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/onhotpath/ferry"
)

// The two refusals a request cannot produce, because both are about a name
// rather than a value: an address neither plane can spell, and a source built
// with a separator it cannot join with.
//
// The first is reached through ferry.Dump into the stand-in sink, which is the
// only seam in this package that mints a name out of a value. On the read side
// every minted name came off the wire and is a legal name by construction, so a
// load cannot reach it - which is worth knowing rather than a gap in the test.

// mapHolder is the schema the dumps below mint their names from.
type mapHolder struct {
	M map[string]string `ferry:"m"`
}

// TestANameNeitherPlaneCanSpellIsRefused is [ErrIllegalName], one row per way an
// address fails to have a name at all.
func TestANameNeitherPlaneCanSpellIsRefused(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		src *Source
		key string
	}{
		"a map key no header field name may hold": {NewHeaderSource(), "a b"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := dumpKey(t, tc.src, tc.key)
			if err == nil {
				t.Fatal("an address with no name on this plane was written anyway")
			}

			assertWraps(t, err, ErrIllegalName, ferry.ErrPlane)
		})
	}
}

// TestAnEmptyMapKeyNeverReachesEitherPlane is the row that moved out of the test
// above, and it moved because the refusal did.
//
// An empty segment names no address in ferry's own model, so a map key rendering
// to empty text is refused at the mapping before any plane is asked for a name
// for it (#258). This driver's key function still refuses an empty part, and
// what changed is that nothing can reach it that way any more: the fact worth
// pinning here is that the value never arrives, whichever plane is underneath.
func TestAnEmptyMapKeyNeverReachesEitherPlane(t *testing.T) {
	t.Parallel()

	for name, src := range map[string]*Source{
		"query":  NewQuerySource(),
		"header": NewHeaderSource(),
	} {
		t.Run(name, func(t *testing.T) { refusesTheEmptyKey(t, src) })
	}
}

// refusesTheEmptyKey is one plane's row, lifted out of its table so that the
// table stays a table: a subtest body counts against the enclosing function's
// complexity.
func refusesTheEmptyKey(t *testing.T, src *Source) {
	t.Parallel()

	err := dumpKey(t, src, "")
	if err == nil {
		t.Fatal("a map key rendering to empty text was written anyway")
	}

	if !errors.Is(err, ferry.ErrValue) {
		t.Errorf("the refusal is not a value refusal: %v", err)
	}
}

// The third way an address fails to have a name at all is the root of a schema
// whose root is a single value, and it is reachable through the seam now that
// such a schema compiles: rootleaf_test.go asserts it through Bind.

// TestAMapKeyAHeaderNameMayHoldIsNotRefused is the control: the check above is
// about the name and not about maps, so an ordinary key goes through.
func TestAMapKeyAHeaderNameMayHoldIsNotRefused(t *testing.T) {
	t.Parallel()

	if err := dumpKey(t, NewHeaderSource(), "tenant"); err != nil {
		t.Errorf("an ordinary map key was refused: %v", err)
	}
}

// dumpKey writes a one-member map under one key into a fresh empty request,
// which is the only way this package mints a name out of a value.
func dumpKey(t *testing.T, src *Source, key string) error {
	t.Helper()

	sink := standInSink{src: src}
	if src.p.name == "header" {
		sink.holds = fieldValue
	}

	return ferry.Dump(planeless(t, src), mapHolder{M: map[string]string{key: "v"}}, sink)
}

// TestASeparatorAPlaneCannotJoinWithIsRefused is [ErrOption], and it lands
// before any request is looked at because the two constructors take options and
// return no error.
func TestASeparatorAPlaneCannotJoinWithIsRefused(t *testing.T) {
	t.Parallel()

	cases := map[string]*Source{
		"an empty query separator":               NewQuerySource(Separator("")),
		"an empty header separator":              NewHeaderSource(Separator("")),
		"a header separator that is not a token": NewHeaderSource(Separator("a b")),
		"a header separator holding a null byte": NewHeaderSource(Separator("\x00")),
		"a header separator holding a slash":     NewHeaderSource(Separator("/")),
	}

	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := ferry.Load[filter](planeless(t, src), src)
			if err == nil {
				t.Fatal("a source built with an unusable separator bound anyway")
			}

			assertWraps(t, err, ErrOption, ferry.ErrPlane)
		})
	}
}

// TestAQuerySeparatorIsUnconstrainedBeyondBeingNonEmpty is the other half of the
// declaration: every byte survives percent-encoding in a name, so the query
// plane holds a separator to nothing but being non-empty.
func TestAQuerySeparatorIsUnconstrainedBeyondBeingNonEmpty(t *testing.T) {
	t.Parallel()

	for _, sep := range []string{"\x00", "%", " ", "&", "=", "/"} {
		if _, err := ferry.Load[filter](queryCtx(t, ""), NewQuerySource(Separator(sep))); err != nil {
			t.Errorf("the query plane refused the separator %q, and every byte survives in a name: %v", sep, err)
		}
	}
}

// planeless is an empty request of whichever kind the source reads, so that a
// refusal is the separator's and not the absence of a request.
func planeless(t *testing.T, src *Source) context.Context {
	t.Helper()

	if src.p.name == "header" {
		return WithHeaders(t.Context(), http.Header{})
	}

	return WithQuery(t.Context(), url.Values{})
}

// TestErrIllegalNameIsNotErrOption keeps the two classes apart, which is what
// lets a caller tell a schema it cannot serve from a source it built wrong.
func TestErrIllegalNameIsNotErrOption(t *testing.T) {
	t.Parallel()

	if errors.Is(ErrIllegalName, ErrOption) || errors.Is(ErrOption, ErrIllegalName) {
		t.Error("the two refusal classes answer for each other, so nothing can distinguish them")
	}
}
