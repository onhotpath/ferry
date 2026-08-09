package ferryhttp

import (
	"net/http"
	"net/url"
	"os/exec"
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

// TestOnePlanesRootOptionDoesNotCompileOnTheOther is why there are two names
// rather than one: a header source given the query plane's option would read the
// root out of a name meant for the other half of the request, and the parameter
// type is what stops it, before anything runs (#338).
//
// The wording of a compiler diagnostic is Go's rather than ferry's, so what is
// asserted is that the build fails and that the message names the option and the
// plane option type it is not.
func TestOnePlanesRootOptionDoesNotCompileOnTheOther(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		pkg  string
		want []string
	}{
		"ferryhttp.RootField on a query source": {
			pkg:  "./internal/testdata/crossedroot/fieldonquery",
			want: []string{"ferryhttp.HeaderOption", "ferryhttp.QueryOption"},
		},
		"ferryhttp.RootParam on a header source": {
			pkg:  "./internal/testdata/crossedroot/paramonheader",
			want: []string{"ferryhttp.QueryOption", "ferryhttp.HeaderOption"},
		},
	} {
		t.Run(name, mustNotCompile(tc.pkg, tc.want))
	}
}

// mustNotCompile builds one fixture package and holds its refusal to naming the
// two plane option types. It is lifted out of the table above so that the table
// stays a table: a subtest body counts against the enclosing function's
// complexity.
func mustNotCompile(pkg string, want []string) func(*testing.T) {
	return func(t *testing.T) {
		t.Parallel()

		out, err := exec.CommandContext(t.Context(), "go", "build", pkg).CombinedOutput()
		if err == nil {
			t.Fatalf("%s compiled, and it is a fixture that must not", pkg)
		}

		for _, w := range want {
			if !strings.Contains(string(out), w) {
				t.Errorf("the compiler said\n\t%s\nand it does not contain\n\t%s", out, w)
			}
		}
	}
}
