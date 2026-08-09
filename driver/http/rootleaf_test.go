package ferryhttp

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
)

// A schema whose root is a single value has one address, the root, and it
// carries no part for this driver to join into a name. So the plane's own option
// names it, and there is one option per plane.

// TestARootLeafReadsTheParameterRootParamNames is the shape this plane exists
// for: ?q=x bound to a string is an ordinary handler, and the whole schema is
// the string.
func TestARootLeafReadsTheParameterRootParamNames(t *testing.T) {
	t.Parallel()

	ctx := WithQuery(t.Context(), url.Values{"q": {"x"}})

	got, err := ferry.Load[string](ctx, NewQuerySource(RootParam("q")))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got != "x" {
		t.Errorf("loaded %q, want what the named parameter holds", got)
	}
}

// TestARootLeafIsRefusedWhereNoOptionNamesIt is the default answer on both
// planes, and it lands at Bind: the caller still has the whole schema in hand
// and no request has been looked at.
//
// The refusal names the option that lifts it, and it is the option belonging to
// the constructor that was called rather than the other plane's.
func TestARootLeafIsRefusedWhereNoOptionNamesIt(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		src    *Source
		option string
	}{
		"the query plane":     {NewQuerySource(), "ferryhttp.RootParam"},
		"and the header one":  {NewHeaderSource(), "ferryhttp.RootField"},
		"a named empty query": {NewQuerySource(RootParam("")), "ferryhttp.RootParam"},
	} {
		t.Run(name, refusesTheRoot(tc.src, tc.option))
	}
}

// refusesTheRoot is one source's row, lifted out of its table so that the table
// stays a table: a subtest body counts against the enclosing function's
// complexity.
func refusesTheRoot(src *Source, option string) func(*testing.T) {
	return func(t *testing.T) {
		t.Parallel()

		_, err := ferry.Bind[string](src)
		if err == nil {
			t.Fatal("a schema whose only address is the root bound, with no name to read it at")
		}

		assertWraps(t, err, ErrIllegalName, ferry.ErrPlane)

		if !strings.Contains(err.Error(), option) {
			t.Errorf("the refusal is %q, want it to name the option that lifts it", err.Error())
		}
	}
}

// TestARootLeafRoundTripsThroughTheFieldRootFieldNames is both halves of the
// header plane reading one field, and it pins the canonicalisation on the way:
// the name the option was given is not the name the request carries, and the
// load has to compute the same one the dump did.
func TestARootLeafRoundTripsThroughTheFieldRootFieldNames(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	ctx := WithHeaders(t.Context(), h)
	src := NewHeaderSource(RootField("x-request-id"))

	if err := ferry.Dump(ctx, "abc", standInSink{src: src, holds: fieldValue}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if _, ok := h["X-Request-Id"]; !ok {
		t.Errorf("the request holds %v, want the field under its canonical spelling", h)
	}

	got, err := ferry.Load[string](ctx, src)
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got != "abc" {
		t.Errorf("loaded %q, want the value dumped at the root", got)
	}
}

// TestARootFieldNameNoHeaderFieldNameMayHoldIsRefused is the grammar a field
// name has, applied to the one name that arrives from an option rather than
// from a tag or a map key.
func TestARootFieldNameNoHeaderFieldNameMayHoldIsRefused(t *testing.T) {
	t.Parallel()

	_, err := ferry.Bind[string](NewHeaderSource(RootField("a b")))
	if err == nil {
		t.Fatal("a root field name holding a space bound anyway")
	}

	assertWraps(t, err, ErrIllegalName, ferry.ErrPlane)
}

// TestOnePlanesRootOptionIsRefusedOnTheOther is why there are two names rather
// than one: a header source given the query plane's option reads the root out of
// a name meant for the other half of the request, so it is refused instead.
//
// It is an option refusal and not a naming one, so it lands whatever the schema
// is, and the schema here holds no root leaf at all.
func TestOnePlanesRootOptionIsRefusedOnTheOther(t *testing.T) {
	t.Parallel()

	for name, src := range map[string]*Source{
		"ferryhttp.RootField on a query source":  NewQuerySource(RootField("Value")),
		"ferryhttp.RootParam on a header source": NewHeaderSource(RootParam("value")),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := ferry.Bind[filter](src)
			if err == nil {
				t.Fatal("a source built with the other plane's root option bound anyway")
			}

			assertWraps(t, err, ErrOption, ferry.ErrPlane)
		})
	}
}
