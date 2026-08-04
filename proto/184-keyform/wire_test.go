package keyform

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"sort"
	"strings"
	"testing"
)

// TestWhatGoParses is the ground truth for what arrives: whether Go's own query
// parser gives a bracket any meaning at all.
func TestWhatGoParses(t *testing.T) {
	for _, raw := range []string{
		"tags[0]=a&tags[1]=b",
		"tags=a&tags=b",
		"db[host]=x&db[port]=5432",
		"db.host=x&db.port=5432",
		"db%5Bhost%5D=x",
		"tags[]=a&tags[]=b",
		"a b=1",
		"a+b=1",
		"a%20b=1",
	} {
		v, err := url.ParseQuery(raw)
		if err != nil {
			t.Logf("%-28s ERROR %v", raw, err)

			continue
		}

		names := make([]string, 0, len(v))
		for k := range v {
			names = append(names, k)
		}

		sort.Strings(names)

		var b strings.Builder
		for _, k := range names {
			b.WriteString(" " + quote(k) + ":" + fmtSlice(v[k]))
		}

		t.Logf("%-28s ->%s", raw, b.String())
	}
}

// TestWhatGoEncodes is the other direction: what Values.Encode puts on the wire
// for each form's keys.
func TestWhatGoEncodes(t *testing.T) {
	for _, v := range []url.Values{
		{"db[host]": {"x"}, "db[port]": {"5432"}},
		{"db.host": {"x"}, "db.port": {"5432"}},
		{"tags[0]": {"a"}, "tags[1]": {"b"}},
		{"tags.0": {"a"}, "tags.1": {"b"}},
		{"tags": {"a", "b"}},
	} {
		t.Logf("%-40s -> %q", fmtValues(v), v.Encode())
	}
}

// TestRoundTripThroughARealServer sends each form's keys through a real client
// and a real server, so the claim about what arrives is not a claim about
// url.ParseQuery in isolation.
func TestRoundTripThroughARealServer(t *testing.T) {
	var got url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	for _, v := range []url.Values{
		{"db[host]": {"x"}, "tags[0]": {"a"}, "tags[1]": {"b"}},
		{"db.host": {"x"}, "tags.0": {"a"}, "tags.1": {"b"}},
		{"tags": {"a", "b"}},
	} {
		resp, err := http.Get(srv.URL + "?" + v.Encode()) //nolint:noctx // prototype
		if err != nil {
			t.Fatal(err)
		}

		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		t.Logf("sent %-46q arrived as %s", v.Encode(), fmtValues(got))
	}
}

// TestHeaderNamesOnTheWire is the decisive one for the header plane: whether
// net/http will send a field name each form produces.
func TestHeaderNamesOnTheWire(t *testing.T) {
	var got http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	for _, name := range []string{
		"X-Request-Id",   // one segment, idiomatic
		"Db-Host",        // flat join, two segments
		"Db-Auth-User",   // flat join, three segments
		"db[host]",       // bracket, two segments
		"db[auth][user]", // bracket, three segments
		"tags[0]",        // bracket, a sequence position
		"Db.Host",        // a dot join
		"Db_Host",        // an env-style join
	} {
		req, err := http.NewRequest(http.MethodGet, srv.URL, nil) //nolint:noctx // prototype
		if err != nil {
			t.Fatal(err)
		}

		req.Header[name] = []string{"v"}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Logf("%-16q canonical=%-16q REFUSED BY net/http: %v",
				name, textproto.CanonicalMIMEHeaderKey(name), err)

			continue
		}

		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		t.Logf("%-16q canonical=%-16q arrived as %v", name,
			textproto.CanonicalMIMEHeaderKey(name), got.Values(name))
	}
}

// TestCanonicalisation is what textproto does to each form's rendering, which
// is what invertibility on the header plane turns on.
func TestCanonicalisation(t *testing.T) {
	for _, s := range []string{
		"x-request-id", "db-host", "db_host", "db.host", "db[host]",
		"DB-HOST", "Db-Host", "x-forwarded-for",
	} {
		t.Logf("%-16q -> %q", s, textproto.CanonicalMIMEHeaderKey(s))
	}
}

func quote(s string) string { return `"` + s + `"` }

func fmtSlice(v []string) string {
	return "[" + strings.Join(v, " ") + "]"
}

func fmtValues(v url.Values) string {
	names := make([]string, 0, len(v))
	for k := range v {
		names = append(names, k)
	}

	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, k := range names {
		parts = append(parts, quote(k)+":"+fmtSlice(v[k]))
	}

	return "{" + strings.Join(parts, " ") + "}"
}
