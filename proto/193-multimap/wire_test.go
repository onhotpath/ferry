package multimap

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
)

// TestWhatGoParses is the ground truth for what a driver is handed: whether an
// index suffix and a repeated name are one shape or two in Go's own parser.
func TestWhatGoParses(t *testing.T) {
	for _, raw := range []string{
		"tags=a&tags=b",
		"tags=a",
		"tags.0=a&tags.1=b",
		"tags[]=a&tags[]=b",
		"tags=",
		"tags",
		"tags=a&tags=b&tags.0=z",
		"q=a&q=b",
		"x=",
	} {
		v, err := url.ParseQuery(raw)
		if err != nil {
			t.Logf("%-26s ERROR %v", raw, err)

			continue
		}

		t.Logf("%-26s -> %s", raw, FmtValues(v))
	}
}

// TestWhatGoEncodes is the other direction: what url.Values.Encode puts on the
// wire for a name holding two values, which is the spelling every shape here is
// judged against.
func TestWhatGoEncodes(t *testing.T) {
	for _, v := range []url.Values{
		{"tags": {"a", "b"}},
		{"tags": {"a"}},
		{"tags.0": {"a"}, "tags.1": {"b"}},
		{"q": {"a"}, "tags": {"a", "b"}},
		{"x": {""}},
	} {
		t.Logf("%-38s -> %q", FmtValues(v), v.Encode())
	}
}

// TestThroughARealServer sends each spelling through a real client and a real
// server, so the claim about what arrives is not a claim about url.ParseQuery in
// isolation.
func TestThroughARealServer(t *testing.T) {
	var got url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	for _, raw := range []string{
		"tags=a&tags=b",
		"tags=a",
		"tags.0=a&tags.1=b",
		"tags=a&tags=b&q=z",
		"x=",
	} {
		resp, err := http.Get(srv.URL + "?" + raw) //nolint:noctx // prototype
		if err != nil {
			t.Fatal(err)
		}

		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		t.Logf("sent ?%-24s arrived as %s", raw, FmtValues(got))
	}
}

// TestHeadersThroughARealServer is the header plane's ground truth, including a
// repeated Accept-Encoding, which is the header equivalent of ?tags=a&tags=b.
func TestHeadersThroughARealServer(t *testing.T) {
	var got http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	for _, sent := range [][][2]string{
		{{"Accept-Encoding", "gzip"}, {"Accept-Encoding", "br"}},
		{{"Accept-Encoding", "gzip"}},
		{{"accept-encoding", "gzip"}, {"ACCEPT-ENCODING", "br"}},
		{{"Accept-Encoding-0", "gzip"}, {"Accept-Encoding-1", "br"}},
		{{"X-Tag", "a"}, {"X-Tag", "b"}},
		{{"X", ""}},
	} {
		req, err := http.NewRequest(http.MethodGet, srv.URL, nil) //nolint:noctx // prototype
		if err != nil {
			t.Fatal(err)
		}

		for _, p := range sent {
			req.Header.Add(p[0], p[1])
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Logf("%-46s REFUSED BY net/http: %v", fmtPairs(sent), err)

			continue
		}

		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		// Only the fields this test set, so the client's own defaults do not
		// clutter the row.
		out := http.Header{}
		for _, p := range sent {
			out[http.CanonicalHeaderKey(p[0])] = got.Values(p[0])
		}

		t.Logf("sent %-46s arrived as %s", fmtPairs(sent), FmtValues(out))
	}
}

func fmtPairs(ps [][2]string) string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p[0]+": "+p[1])
	}

	return "[" + strings.Join(out, " | ") + "]"
}

// TestEndToEndThroughAHandler is the whole thing in the shape a user meets it:
// a real request, a real handler, the plane taken from the request and put in
// the context, and ferry.Load out the other side.
func TestEndToEndThroughAHandler(t *testing.T) {
	for _, s := range Shapes() {
		src := NewQuerySource(s, declaredFor(s)...)

		var line string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got, err := ferry.Load[Both](WithQuery(r.Context(), r.URL.Query()), src)
			line = outcome(got, err)

			w.WriteHeader(http.StatusNoContent)
		}))

		resp, err := http.Get(srv.URL + "?tags=a&tags=b&q=z") //nolint:noctx // prototype
		if err != nil {
			t.Fatal(err)
		}

		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		srv.Close()

		t.Logf("%-18s ?tags=a&tags=b&q=z -> %s", s, line)
	}
}
