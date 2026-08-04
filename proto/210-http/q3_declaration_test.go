package httpdecisions

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/onhotpath/ferry"
)

// Question 3: may a per-request Source carry per-schema configuration?
//
// The `declared` shape needed it. `enumerated` may need none, in which case the
// question is moot for driver/http. The general question interacts with the
// caller-held binding, which pins one source to one compiled type.

// TestQ3IsItLiveForTheDriver runs #193's whole matrix under the shape that
// shipped, with no declaration at all.
//
// If every row is correct or loud, the driver needs no declaration and the
// question is moot for driver/http whatever the general answer is.
func TestQ3IsItLiveForTheDriver(t *testing.T) {
	t.Logf("=== query plane, enumerated, no declaration ===")

	for _, c := range []struct {
		label string
		got   string
	}{
		{"?tags=a&tags=b            -> Tags []string", loadQuery[Tagged](t, "tags=a&tags=b")},
		{"?tags=a                   -> Tags []string", loadQuery[Tagged](t, "tags=a")},
		{"?tags.0=a&tags.1=b        -> Tags []string", loadQuery[Tagged](t, "tags.0=a&tags.1=b")},
		{"(nothing)                 -> Tags []string", loadQuery[Tagged](t, "")},
		{"?q=a&q=b                  -> Q string", loadQuery[Scalar](t, "q=a&q=b")},
		{"?q=a                      -> Q string", loadQuery[Scalar](t, "q=a")},
		{"?q=                       -> Q string", loadQuery[Scalar](t, "q=")},
		{"?limits.cpu=4&limits.mem=8 -> map[string]int", loadQuery[Mapped](t, "limits.cpu=4&limits.mem=8")},
		{"?limits=a&limits=b        -> map[string]int", loadQuery[Mapped](t, "limits=a&limits=b")},
		{"?tags=a&tags=b&q=z        -> both", loadQuery[Both](t, "tags=a&tags=b&q=z")},
	} {
		t.Logf("  %-42s %s", c.label, c.got)
	}

	t.Logf("")
	t.Logf("=== header plane, enumerated, no declaration ===")

	for _, c := range []struct {
		label string
		got   string
	}{
		{"Accept-Encoding: gzip / br -> []string",
			loadHeader[Encodings](t, [][2]string{{"Accept-Encoding", "gzip"}, {"Accept-Encoding", "br"}})},
		{"Accept-Encoding: gzip      -> []string",
			loadHeader[Encodings](t, [][2]string{{"Accept-Encoding", "gzip"}})},
		{"Accept-Encoding: gzip / br -> string",
			loadHeader[Encoding](t, [][2]string{{"Accept-Encoding", "gzip"}, {"Accept-Encoding", "br"}})},
		{"Accept-Encoding: gzip      -> string",
			loadHeader[Encoding](t, [][2]string{{"Accept-Encoding", "gzip"}})},
	} {
		t.Logf("  %-42s %s", c.label, c.got)
	}
}

// TestQ3TheSharedSourceHazard is the flaw #193 measured, re-measured against
// both entry points: one Source carrying a declaration, two schemas.
func TestQ3TheSharedSourceHazard(t *testing.T) {
	hdr := func() http.Header {
		h := http.Header{}
		h.Add("Accept-Encoding", "gzip")

		return h
	}

	t.Logf("=== through ferry.Load, one Source, two schemas ===")

	src := NewHeaderSource(Enumerated, Repeatable("Accept-Encoding"))
	ctx := WithHeaders(context.Background(), hdr())

	seq, err := ferry.Load[Encodings](ctx, src)
	t.Logf("  handler A, Encodings []string: %s", outcome(seq, err))

	one, err := ferry.Load[Encoding](ctx, src)
	t.Logf("  handler B, Encoding  string  : %s", outcome(one, err))
	t.Logf("  Source.Binds() after two loads = %d", src.Binds())

	t.Logf("")
	t.Logf("=== through ferry.Bind, one Source, two bindings ===")

	shared := NewHeaderSource(Enumerated, Repeatable("Accept-Encoding"))

	bA, err := ferry.Bind[Encodings](shared)
	if err != nil {
		t.Fatalf("bind A: %v", err)
	}

	bB, err := ferry.Bind[Encoding](shared)
	if err != nil {
		t.Fatalf("bind B: %v", err)
	}

	ctx = WithHeaders(context.Background(), hdr())

	seq, err = bA.Load(ctx)
	t.Logf("  binding A, Encodings []string: %s", outcome(seq, err))

	one, err = bB.Load(ctx)
	t.Logf("  binding B, Encoding  string  : %s", outcome(one, err))
	t.Logf("  Source.Binds() after two binds and two loads = %d", shared.Binds())

	t.Logf("")
	t.Logf("=== the same two schemas with no declaration ===")

	plain := NewHeaderSource(Enumerated)
	ctx = WithHeaders(context.Background(), hdr())

	seq, err = ferry.Load[Encodings](ctx, plain)
	t.Logf("  handler A, Encodings []string: %s", outcome(seq, err))

	one, err = ferry.Load[Encoding](ctx, plain)
	t.Logf("  handler B, Encoding  string  : %s", outcome(one, err))
}

// TestQ3DoesTheBindingPinTheSource asks whether holding a binding is what makes
// a per-schema declaration safe.
//
// It would be, if a Source could tell that it had been bound twice and refuse.
// Whether it can depends on how often core asks, which is what this counts.
func TestQ3DoesTheBindingPinTheSource(t *testing.T) {
	v := url.Values{"tags": {"a", "b"}}
	ctx := WithQuery(context.Background(), v)

	src := NewQuerySource(Enumerated)

	for range 3 {
		if _, err := ferry.Load[Tagged](ctx, src); err != nil {
			t.Fatalf("load: %v", err)
		}
	}

	t.Logf("three ferry.Load calls over one Source, one schema: Binds() = %d", src.Binds())

	bound := NewQuerySource(Enumerated)

	b, err := ferry.Bind[Tagged](bound)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	for range 3 {
		if _, err := b.Load(ctx); err != nil {
			t.Fatalf("load: %v", err)
		}
	}

	t.Logf("one ferry.Bind and three Binding.Load calls over one Source: Binds() = %d", bound.Binds())
	t.Logf("so a Source that refused its second Bind would refuse the second ferry.Load of the same schema, " +
		"and one-schema-per-source is not a rule a Source can enforce for itself")
}

// TestQ3IsTheDeclarationCheckable measures how much of a per-schema declaration
// a driver can check against the schema at Bind, which is what decides whether
// per-schema configuration is a hazard or a contract.
func TestQ3IsTheDeclarationCheckable(t *testing.T) {
	for _, c := range []struct {
		label string
		src   ferry.Source
		load  func(ferry.Source) string
	}{
		{"a name this schema does not have (a typo)",
			NewHeaderSource(Enumerated, Repeatable("Accept-Encodings"), CheckDeclaration()),
			func(s ferry.Source) string { return loadEncodings(t, s) }},
		{"a name this schema has, as a sequence (correct)",
			NewHeaderSource(Enumerated, Repeatable("Accept-Encoding"), CheckDeclaration()),
			func(s ferry.Source) string { return loadEncodings(t, s) }},
		{"a name this schema has, as a scalar (wrong, and not checkable)",
			NewHeaderSource(Enumerated, Repeatable("Accept-Encoding"), CheckDeclaration()),
			func(s ferry.Source) string { return loadEncoding(t, s) }},
	} {
		t.Logf("%-48s -> %s", c.label, c.load(c.src))
	}
}

func loadEncodings(t *testing.T, s ferry.Source) string {
	t.Helper()

	h := http.Header{}
	h.Add("Accept-Encoding", "gzip")

	got, err := ferry.Load[Encodings](WithHeaders(context.Background(), h), s)

	return outcome(got, err)
}

func loadEncoding(t *testing.T, s ferry.Source) string {
	t.Helper()

	h := http.Header{}
	h.Add("Accept-Encoding", "gzip")

	got, err := ferry.Load[Encoding](WithHeaders(context.Background(), h), s)

	return outcome(got, err)
}
