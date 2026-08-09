package ferryhttp

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
)

// A schema whose root is a single value has one address, the root, and it
// carries no part for this driver to join into a name. So an option names it,
// and there is one option for both planes: each plane reads the raw name its own
// way.

// TestARootLeafReadsTheParameterRootNameNames is the shape this plane exists
// for: ?q=x bound to a string is an ordinary handler, and the whole schema is
// the string.
func TestARootLeafReadsTheParameterRootNameNames(t *testing.T) {
	t.Parallel()

	ctx := WithQuery(t.Context(), url.Values{"q": {"x"}})

	got, err := ferry.Load[string](ctx, NewQuerySource(RootName("q")))
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
// The refusal names the option that lifts it, which is now the same option on
// both planes.
func TestARootLeafIsRefusedWhereNoOptionNamesIt(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		src    *Source
		option string
	}{
		"the query plane":      {NewQuerySource(), "ferryhttp.RootName"},
		"and the header one":   {NewHeaderSource(), "ferryhttp.RootName"},
		"a named empty query":  {NewQuerySource(RootName("")), "ferryhttp.RootName"},
		"a named empty header": {NewHeaderSource(RootName("")), "ferryhttp.RootName"},
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

// TestARootLeafRoundTripsThroughTheFieldRootNameNames is both halves of the
// header plane reading one field, and it pins the canonicalisation on the way:
// the name the option was given is not the name the request carries, and the
// load has to compute the same one the dump did.
//
// This is the plane-aware half of the single option: the same RootName that the
// query plane takes literally is canonicalised here.
func TestARootLeafRoundTripsThroughTheFieldRootNameNames(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	ctx := WithHeaders(t.Context(), h)
	src := NewHeaderSource(RootName("x-request-id"))

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
//
// It survives the collapse to one option intact, because the check was never in
// the option: headerKey holds every name it computes to the token grammar, and
// the root name goes through it like any other.
func TestARootFieldNameNoHeaderFieldNameMayHoldIsRefused(t *testing.T) {
	t.Parallel()

	_, err := ferry.Bind[string](NewHeaderSource(RootName("a b")))
	if err == nil {
		t.Fatal("a root field name holding a space bound anyway")
	}

	assertWraps(t, err, ErrIllegalName, ferry.ErrPlane)
}

// TestAQueryShapedRootNameOnAHeaderSourceIsSilentlyCanonicalised is what
// replaces one half of the crossed-option refusal.
//
// RootName("q") on a header source is not refused and cannot be: "q" is a legal
// field name, so the header plane's own reading of it succeeds and the source
// reads the field Q. A caller who meant the query parameter q gets a header
// lookup that no ordinary request answers.
func TestAQueryShapedRootNameOnAHeaderSourceIsSilentlyCanonicalised(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	h.Set("q", "x") // net/http stores this as Q.

	got, err := ferry.Load[string](WithHeaders(t.Context(), h), NewHeaderSource(RootName("q")))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	t.Logf("RootName(%q) on a header source loaded %q from %v", "q", got, h)

	if got != "x" {
		t.Errorf("loaded %q, want the header plane to have read the field Q", got)
	}
}

// TestAHeaderShapedRootNameOnAQuerySourceIsSilentlyLiteral is the other half.
//
// RootName("X-Request-Id") on a query source binds, because that string is a
// perfectly ordinary query parameter name, and then reads a parameter no request
// carries. What comes back is the absence, not a refusal naming the mistake.
func TestAHeaderShapedRootNameOnAQuerySourceIsSilentlyLiteral(t *testing.T) {
	t.Parallel()

	src := NewQuerySource(RootName("X-Request-Id"))

	// A request spelled the way the caller meant it: the value is in the header
	// plane, and this source reads the query plane.
	ctx := WithQuery(t.Context(), url.Values{"q": {"x"}})

	got, err := ferry.Load[string](ctx, src)
	t.Logf("RootName(%q) on a query source loaded %q, err %v", "X-Request-Id", got, err)

	if err != nil {
		t.Fatalf("load: %+v", err) // Recorded, not expected: see the log line.
	}

	if got != "" {
		t.Errorf("loaded %q, want the zero value of an absent root", got)
	}

	// And the literal reading is what it really is: a parameter spelled exactly
	// that answers.
	lit := WithQuery(t.Context(), url.Values{"X-Request-Id": {"abc"}})

	got, err = ferry.Load[string](lit, src)
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got != "abc" {
		t.Errorf("loaded %q, want the query plane to have read the parameter literally", got)
	}
}

// TestCaseIsTheOnlyThingOneOptionCannotCarryAcross records the case a
// grammar-sniffing recovery would have to get right and cannot.
//
// A query source and a header source given the same RootName disagree about
// what it names, and neither disagreement is visible to the option: "q" is a
// legal field name and "X-Request-Id" is a legal parameter name, so no property
// of the string says which plane the caller meant.
func TestCaseIsTheOnlyThingOneOptionCannotCarryAcross(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"q", "X-Request-Id", "value", "Value", "user-agent"} {
		// Every one of these is a legal name on both planes.
		if !fieldName(name) {
			t.Errorf("%q is not a legal field name, so a sniff could have refused it", name)
		}
	}
}
