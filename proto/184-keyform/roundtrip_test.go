package keyform

import (
	"context"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/onhotpath/ferry"
)

// TestQueryRoundTrip dumps a value into a url.Values through each form and
// loads it back, which is the invertibility question answered end to end
// through ferry.Dump and ferry.Load rather than by inspecting a key function.
func TestQueryRoundTrip(t *testing.T) {
	for _, f := range []Form{Bracket, Flat} {
		t.Run(f.String(), func(t *testing.T) {
			roundTripQuery(t, f, Nested3{DB: DB3{Host: "h", Auth: Cred{User: "u"}}})
			roundTripQuery(t, f, Slicey{Tags: []string{"a", "b", "c"}})
			roundTripQuery(t, f, Mappy{Limits: map[string]int{"rps": 10}})
			roundTripQuery(t, f, Mappy{Limits: map[string]int{"a.b": 1}})
			roundTripQuery(t, f, Mappy{Limits: map[string]int{"a[b]": 2}})
			roundTripQuery(t, f, Mappy{Limits: map[string]int{"a][b": 3}})
			roundTripQuery(t, f, Mappy{Limits: map[string]int{"a b": 4}})
			roundTripQuery(t, f, Mappy{Limits: map[string]int{"a-b": 5}})
			roundTripQuery(t, f, Mappy{Limits: map[string]int{"0": 6}})
		})
	}
}

func roundTripQuery[T any](t *testing.T, f Form, in T) {
	t.Helper()

	vals := url.Values{}
	ctx := WithQuery(context.Background(), vals)

	if err := ferry.Dump(ctx, in, NewQuerySink(f, DefaultQuerySeparator)); err != nil {
		t.Logf("  %-30s DUMP REFUSED %v", show(in), err)

		return
	}

	wire := vals.Encode()

	back, err := ferry.Load[T](ctx, NewQuerySource(f, DefaultQuerySeparator))
	if err != nil {
		t.Logf("  %-30s -> %-42s LOAD REFUSED %v", show(in), wire, err)

		return
	}

	verdict := "SAME"
	if !equalish(in, back) {
		verdict = "DIFFERENT"
	}

	t.Logf("  %-30s -> %-42s -> %-30s %s", show(in), wire, show(back), verdict)
}

// TestHeaderRoundTrip is the same over http.Header.
func TestHeaderRoundTrip(t *testing.T) {
	for _, f := range []Form{Bracket, Flat} {
		t.Run(f.String(), func(t *testing.T) {
			roundTripHeader(t, f, Header1{RequestID: "r1", Auth: "Bearer x"})
			roundTripHeader(t, f, HeaderNested{XForwarded: Forwarded{For: "1.2.3.4", Proto: "https"}})
			roundTripHeader(t, f, Nested2{DB: DB2{Host: "h", Port: 5432}})
			roundTripHeader(t, f, Mappy{Limits: map[string]int{"RPS": 10}})
			roundTripHeader(t, f, Mappy{Limits: map[string]int{"rps": 10}})
		})
	}
}

func roundTripHeader[T any](t *testing.T, f Form, in T) {
	t.Helper()

	h := http.Header{}
	ctx := WithHeaders(context.Background(), h)

	if err := ferry.Dump(ctx, in, NewHeaderSink(f)); err != nil {
		t.Logf("  %-34s DUMP REFUSED %v", show(in), err)

		return
	}

	back, err := ferry.Load[T](ctx, NewHeaderSource(f))
	if err != nil {
		t.Logf("  %-34s -> %-52s LOAD REFUSED %v", show(in), show(mapOf(h)), err)

		return
	}

	verdict := "SAME"
	if !equalish(in, back) {
		verdict = "DIFFERENT"
	}

	t.Logf("  %-34s -> %-52s -> %-34s %s", show(in), show(mapOf(h)), show(back), verdict)
}

// TestWhatAClientActuallySends loads a slice out of the query string a plain
// HTML form, curl or a Go client produces: a repeated parameter.
func TestWhatAClientActuallySends(t *testing.T) {
	raw := map[string]string{
		"repeated  ?tags=a&tags=b":       "tags=a&tags=b",
		"bracket   ?tags[0]=a&tags[1]=b": "tags[0]=a&tags[1]=b",
		"flat      ?tags.0=a&tags.1=b":   "tags.0=a&tags.1=b",
		"php-empty ?tags[]=a&tags[]=b":   "tags[]=a&tags[]=b",
	}

	for _, label := range slices.Sorted(maps.Keys(raw)) {
		v, err := url.ParseQuery(raw[label])
		if err != nil {
			t.Fatal(err)
		}

		for _, f := range []Form{Bracket, Flat} {
			ctx := WithQuery(context.Background(), v)

			out, lerr := ferry.Load[Slicey](ctx, NewQuerySource(f, DefaultQuerySeparator))
			if lerr != nil {
				t.Logf("  %-32s %-8s LOAD REFUSED %v", label, f, lerr)

				continue
			}

			t.Logf("  %-32s %-8s -> %v", label, f, out)
		}
	}

	// The same for a nested struct, which is the deepObject convention.
	for _, f := range []Form{Bracket, Flat} {
		for _, raw := range []string{"db[host]=x&db[port]=1", "db.host=x&db.port=1"} {
			v, err := url.ParseQuery(raw)
			if err != nil {
				t.Fatal(err)
			}

			ctx := WithQuery(context.Background(), v)

			out, lerr := ferry.Load[Nested2](ctx, NewQuerySource(f, DefaultQuerySeparator))
			if lerr != nil {
				t.Logf("  %-32s %-8s LOAD REFUSED %v", "?"+raw, f, lerr)

				continue
			}

			t.Logf("  %-32s %-8s -> %v", "?"+raw, f, out)
		}
	}
}

func mapOf(h http.Header) map[string][]string { return h }

func equalish(a, b any) bool { return sprint(a) == sprint(b) }
