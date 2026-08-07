package ferryhttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
)

// The tests in this file go through ferry.Load, which is the seam a caller uses,
// and they read their planes out of a query string or a header block spelled by
// hand. Between them they pin what this driver's shipped half reads out of a
// request, which is the half no round trip can see: a round trip tests a
// function against its own inverse, so moving the reader and the stand-in sink
// together would be invisible to every proof in conformance_test.go.

// nested is the second half of the schema TestTwoFieldsCannotShareAName builds,
// which is a type of its own because a nested struct literal is one this
// repository's lint set does not allow.
type nested struct {
	Host string `ferry:"host"`
}

// filter is the schema most of the rows below load.
type filter struct {
	Q     string            `ferry:"q"`
	Tags  []string          `ferry:"tags"`
	Limit int               `ferry:"limit,default=10"`
	Sort  map[string]string `ferry:"sort"`
}

// TestQueryLoadsWhatTheRequestSpells pins the reading of a query string, one row
// per shape of the plane this driver has an opinion about.
func TestQueryLoadsWhatTheRequestSpells(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		query string
		want  filter
	}{
		"nothing at all":          {"", filter{Limit: 10}},
		"one scalar":              {"q=go", filter{Q: "go", Limit: 10}},
		"an empty value is a set": {"q=", filter{Q: "", Limit: 10}},
		"a default applies":       {"q=go", filter{Q: "go", Limit: 10}},
		"a repeated name":         {"tags=a&tags=b", filter{Tags: []string{"a", "b"}, Limit: 10}},
		"one occurrence is one element": {
			"tags=a", filter{Tags: []string{"a"}, Limit: 10},
		},
		"index-suffixed names": {
			"tags.0=a&tags.1=b", filter{Tags: []string{"a", "b"}, Limit: 10},
		},
		"an index past the repetition extends it": {
			"tags=a&tags=b&tags.2=c", filter{Tags: []string{"a", "b", "c"}, Limit: 10},
		},
		"a nested map": {
			"sort.name=asc&sort.age=desc",
			filter{Sort: map[string]string{"name": "asc", "age": "desc"}, Limit: 10},
		},
		"a number is text on the wire": {"limit=50", filter{Limit: 50}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assertLoads(t, tc.query, tc.want)
		})
	}
}

func assertLoads(t *testing.T, query string, want filter) {
	t.Helper()

	got, err := ferry.Load[filter](queryCtx(t, query), NewQuerySource())
	if err != nil {
		t.Fatalf("loading %q: %v", query, err)
	}

	if fmt.Sprintf("%+v", got) != fmt.Sprintf("%+v", want) {
		t.Errorf("loading %q gave %+v, want %+v", query, got, want)
	}
}

// TestHeaderLoadsWhatTheRequestSpells is the same for the header plane, and it
// carries the two rows a query string cannot state: that a field name is matched
// case-insensitively, and that a hyphen is how a header nests.
func TestHeaderLoadsWhatTheRequestSpells(t *testing.T) {
	t.Parallel()

	type forwarded struct {
		For   string `ferry:"for"`
		Proto string `ferry:"proto"`
	}

	type tenant struct {
		ID        string    `ferry:"x-tenant-id"`
		Encodings []string  `ferry:"accept-encoding"`
		Forwarded forwarded `ferry:"x-forwarded"`
	}

	h := http.Header{}
	h.Set("x-TENANT-id", "acme")
	h.Add("Accept-Encoding", "gzip")
	h.Add("Accept-Encoding", "br")
	h.Set("X-Forwarded-For", "203.0.113.7")
	h.Set("X-Forwarded-Proto", "https")

	got, err := ferry.Load[tenant](WithHeaders(t.Context(), h), NewHeaderSource())
	if err != nil {
		t.Fatalf("loading from headers: %v", err)
	}

	want := tenant{
		ID:        "acme",
		Encodings: []string{"gzip", "br"},
		Forwarded: forwarded{For: "203.0.113.7", Proto: "https"},
	}

	if fmt.Sprintf("%+v", got) != fmt.Sprintf("%+v", want) {
		t.Errorf("loading the headers gave %+v, want %+v", got, want)
	}
}

// TestARepeatedNameIsNeverReadAsOneValue is the refusal a scalar field earns
// when the request names it twice, and it is made while that field is read.
//
// The moment is the whole of what changed here. This refusal used to be made at
// Close, because an address arrived at Get carrying no kind: /tags for a
// []string and /q for a string were the same call, so the driver could not tell
// a sequence from a scalar and had to answer Absent and wait to see whether
// anything enumerated the name (#193, #208). Get takes a ferry.LeafAddr now, so
// the address says it holds one value and the refusal has a home at it.
func TestARepeatedNameIsNeverReadAsOneValue(t *testing.T) {
	t.Parallel()

	type scalars struct {
		Q string `ferry:"q"`
		R string `ferry:"r"`
	}

	err := loadErr[scalars](t, "q=a&q=b")

	if !errors.Is(err, ErrRepeated) {
		t.Fatalf("loading ?q=a&q=b gave %v, want it to wrap ErrRepeated", err)
	}

	assertWraps(t, err, ferry.ErrPlane, ferry.ErrDriver)

	var located *ferry.Error
	if !errors.As(err, &located) || located.Address() != ferry.At("q") {
		t.Errorf("the refusal is at %v, want /q: %v", addressOf(err), err)
	}

	// The moment is the walk and no longer the close, which a caller reads off
	// the report: a refusal made while the field is read says the driver
	// failed, and one made at Close says the plane was being closed.
	if strings.Contains(err.Error(), "closing the plane") {
		t.Errorf("the refusal is still deferred to Close: %v", err)
	}

	// Two offending names report two failures rather than one, which is what
	// core keeping every address a driver named buys (#211/#212).
	both := loadErr[scalars](t, "q=a&q=b&r=c&r=d")
	if n := len(ferry.Elements(both)); n != 2 {
		t.Errorf("two repeated names reported %d failures, want 2: %+v", n, both)
	}
}

// TestTheSameNameIsASequenceOrARefusalByTheKindOfTheAddress is #193 and #208
// read as one sentence, which is what they turned out to be.
//
// One request, two destinations. ?tags=a&tags=b is two elements where the
// schema says []string, and it is a refusal where the schema says string, and
// the driver decides between them from the kind of the address it is asked
// about rather than from anything in the request. Before the address carried a
// kind, both were the same call at /tags and neither answer could be made where
// it belonged.
func TestTheSameNameIsASequenceOrARefusalByTheKindOfTheAddress(t *testing.T) {
	t.Parallel()

	type asSequence struct {
		Tags []string `ferry:"tags"`
	}

	type asScalar struct {
		Tags string `ferry:"tags"`
	}

	const query = "tags=a&tags=b"

	got, err := ferry.Load[asSequence](queryCtx(t, query), NewQuerySource())
	if err != nil {
		t.Fatalf("loading %s into a []string: %v", query, err)
	}

	// The plane's own order, because a position is the offset into what the
	// name holds and net/url appends in the order the wire carried.
	if want := []string{"a", "b"}; fmt.Sprint(got.Tags) != fmt.Sprint(want) {
		t.Errorf("loading %s gave %v, want %v in the request's own order", query, got.Tags, want)
	}

	refused := loadErr[asScalar](t, query)
	if !errors.Is(refused, ErrRepeated) {
		t.Fatalf("loading %s into a string gave %v, want it to wrap ErrRepeated", query, refused)
	}

	if at := addressOf(refused); at != ferry.At("tags") {
		t.Errorf("the refusal is at %v, want /tags: %v", at, refused)
	}
}

// TestARepeatedNameAtARequiredFieldReportsOneMistakeOnce is the sharpest thing
// making the refusal at Get buys, and it is a diagnosis rather than a rule.
//
// The old placement had to answer Absent at the field and report at Close, so a
// required field pointed at a repeated name failed twice for one mistake: once
// for the absence the driver had manufactured, and once at the close for the
// repetition that caused it. The field is refused where it is read now, so the
// absence never happens and there is one failure to act on.
func TestARepeatedNameAtARequiredFieldReportsOneMistakeOnce(t *testing.T) {
	t.Parallel()

	type required struct {
		Q string `ferry:"q,required"`
	}

	err := loadErr[required](t, "q=a&q=b")

	if n := len(ferry.Elements(err)); n != 1 {
		t.Errorf("one repeated name at a required field reported %d failures, want 1: %+v", n, err)
	}

	if errors.Is(err, ferry.ErrMissing) {
		t.Errorf("the request names this field twice and it was also reported missing: %v", err)
	}

	if !errors.Is(err, ErrRepeated) {
		t.Errorf("the refusal does not say the name is repeated: %v", err)
	}
}

// TestOnePositionSpelledTwiceIsRefused is the one refusal this driver makes
// during the walk, and it is narrow on purpose: only a position both spellings
// name is a clash, and an index-suffixed name past the repetition extends the
// sequence instead.
func TestOnePositionSpelledTwiceIsRefused(t *testing.T) {
	t.Parallel()

	err := loadErr[filter](t, "tags=a&tags=b&tags.0=z")

	if !errors.Is(err, ErrTwoSpellings) {
		t.Fatalf("loading the clash gave %v, want it to wrap ErrTwoSpellings", err)
	}

	assertWraps(t, err, ferry.ErrPlane)

	// Core has the container's address at Children and core's wins, so the
	// refusal is located at the sequence and names the position in its text.
	if got := addressOf(err); got != ferry.At("tags") {
		t.Errorf("the refusal is at %v, want /tags: %v", got, err)
	}

	if !strings.Contains(err.Error(), "position 0") {
		t.Errorf("the refusal does not name the position both spellings claim: %v", err)
	}
}

// TestANameAtASectionsOwnAddressIsRefused is the container-side mirror of
// [TestARepeatedNameIsNeverReadAsOneValue], and it is the request and the
// destination disagreeing about what an address is.
//
// A section's members are the names under it, so nothing at the section's own
// name could hold the value: reading it as absence would build the section out
// of the Go zero and drop what the request actually sent, which is the silent
// wrong answer. The count is what the refusal carries, because it is structure
// rather than text the request supplied.
//
// A composite is the other way round - its members come from the value, so a
// repetition of its own name is the sequence it carries and is read rather than
// refused, which is the row this test pins beside the refusal.
func TestANameAtASectionsOwnAddressIsRefused(t *testing.T) {
	t.Parallel()

	type conf struct {
		Opt  *nested  `ferry:"opt"`
		Tags []string `ferry:"tags"`
	}

	for query, want := range map[string]int{"opt=x": 1, "opt=x&opt=y": 2} {
		err := loadErr[conf](t, query)

		assertWraps(t, err, ferry.ErrValue, ferry.ErrDriver)

		if got := addressOf(err); got != ferry.At("opt") {
			t.Errorf("loading %q refused at %v, want /opt: %v", query, got, err)
		}

		if !strings.Contains(err.Error(), fmt.Sprintf("%d times", want)) {
			t.Errorf("loading %q does not say the name occurs %d times: %v", query, want, err)
		}
	}

	// The same shape at a composite is the sequence it carries, so the refusal
	// is scoped to the container whose members come from the type.
	got, err := ferry.Load[conf](queryCtx(t, "tags=a&tags=b"), NewQuerySource())
	if err != nil {
		t.Fatalf("loading a repeated name at a composite: %v", err)
	}

	if !slices.Equal(got.Tags, []string{"a", "b"}) {
		t.Errorf("loaded %v, want the two occurrences as two elements", got.Tags)
	}
}

// TestAPlaneThatWasNeverSuppliedIsRefused is case 10's obligation stated at the
// seam a caller reaches: a handler that forgot the context call gets a refusal
// and never a struct full of zero values.
func TestAPlaneThatWasNeverSuppliedIsRefused(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		src  *Source
		want error
	}{
		"query":  {NewQuerySource(), ErrNoQuery},
		"header": {NewHeaderSource(), ErrNoHeaders},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assertRefusesTheAbsence(t, tc.src, tc.want)
		})
	}
}

func assertRefusesTheAbsence(t *testing.T, src *Source, want error) {
	t.Helper()

	_, err := ferry.Load[filter](t.Context(), src)
	if err == nil {
		t.Fatal("a load with no request in the context succeeded, so every field reported missing")
	}

	assertWraps(t, err, want, ferry.ErrPlane)
}

// assertWraps is the errors.Is loop every refusal in this file makes.
func assertWraps(t *testing.T, err error, want ...error) {
	t.Helper()

	for _, one := range want {
		if !errors.Is(err, one) {
			t.Errorf("the refusal does not wrap %v: %v", one, err)
		}
	}
}

// TestTheOtherPlanesContextIsNotThisPlanes is what two context keys buy: a
// handler that supplied the query and asked for headers is told so, rather than
// reading the query as though it were a header block.
func TestTheOtherPlanesContextIsNotThisPlanes(t *testing.T) {
	t.Parallel()

	_, err := ferry.Load[filter](queryCtx(t, "q=go"), NewHeaderSource())
	if !errors.Is(err, ErrNoHeaders) {
		t.Errorf("a header load under a query context gave %v, want ErrNoHeaders", err)
	}
}

// TestARefusalNeverQuotesWhatTheRequestHeld is ADR-0011's rule held to on the
// one plane where every value is attacker-supplied. It is a log-injection
// defence as much as a secret-leak one: this driver's whole plane arrives from
// somebody nobody vetted.
func TestARefusalNeverQuotesWhatTheRequestHeld(t *testing.T) {
	t.Parallel()

	type creds struct {
		Secret string `ferry:"secret"`
		Auth   string `ferry:"authorization"`
	}

	h := http.Header{}
	h.Add("Authorization", "Bearer hunter2")
	h.Add("Authorization", "Bearer swordfish")

	headerErr := loadHeaderErr[creds](t, h)
	queryErr := loadErr[creds](t, "secret=hunter2&secret=swordfish")

	for name, err := range map[string]error{"query": queryErr, "header": headerErr} {
		for _, leaked := range []string{"hunter2", "swordfish", "Bearer"} {
			if strings.Contains(fmt.Sprintf("%+v", err), leaked) {
				t.Errorf("the %s refusal quotes %q, which the request supplied: %+v", name, leaked, err)
			}
		}
	}

	// The name is not the value, and the parameter is what a handler answering
	// 400 has to name. It is the request's own spelling of it and not ferry's
	// rendering of the address, because this driver names the address it refuses
	// about (#159).
	if !strings.HasPrefix(fmt.Sprintf("%v", queryErr), "ferry: secret: ") {
		t.Errorf("the refusal does not open with the parameter it is about: %v", queryErr)
	}
}

// TestTwoFieldsCannotShareAName is the injectivity check, refused before any
// request is looked at, and the reason [Separator] exists.
func TestTwoFieldsCannotShareAName(t *testing.T) {
	t.Parallel()

	type collides struct {
		Flat   string `ferry:"db.host"`
		Nested nested `ferry:"db"`
	}

	// The context carries a request, so a refusal here is the schema's and not
	// the absence of a plane.
	_, err := ferry.Load[collides](queryCtx(t, ""), NewQuerySource())
	if err == nil {
		t.Fatal("two fields rendering to one parameter name loaded, so one of the two was lost")
	}

	if !strings.Contains(err.Error(), `"db.host"`) {
		t.Errorf("the refusal does not name the parameter both fields want: %v", err)
	}

	// The wider join is the way out, and it is the whole of what the option is
	// for.
	if _, err := ferry.Load[collides](queryCtx(t, ""), NewQuerySource(Separator(".."))); err != nil {
		t.Errorf("the same schema at the wider join was refused: %v", err)
	}
}

// TestQueryCarriesEveryByteSequence is why the query plane declares no Except.
//
// It is measured rather than reasoned: every value goes out through
// url.Values.Encode and comes back through url.ParseQuery, which is the round
// trip a real request makes, and the corpus is the one ferrytest's own string
// and byte rows carry.
func TestQueryCarriesEveryByteSequence(t *testing.T) {
	t.Parallel()

	type one struct {
		V string `ferry:"v"`
	}

	for _, want := range []string{"", "a\x00b", "\xff\xfe", "a/b,c#0", "\x00\xffA", "a\nb", " a ", "a+b", "a&b=c"} {
		out := url.Values{}
		out.Set("v", want)

		back, err := url.ParseQuery(out.Encode())
		if err != nil {
			t.Fatalf("parsing %q back: %v", out.Encode(), err)
		}

		got, err := ferry.Load[one](WithQuery(t.Context(), back), NewQuerySource())
		if err != nil {
			t.Fatalf("loading %q: %v", want, err)
		}

		if got.V != want {
			t.Errorf("%q went out as %q and came back as %q", want, out.Encode(), got.V)
		}
	}
}

// TestHeaderRefusesWhatNetHTTPRefuses is why the header plane declares an
// Except, and it measures the constraint against a real client and a real server
// rather than reading it off the specification.
//
// Two properties, and the second is the one nobody writes down: net/http refuses
// to send a field value holding a control character other than a tab, and a
// value with a leading or trailing space or tab arrives trimmed. Both mean the
// header plane cannot spell a string this driver would otherwise have to claim.
func TestHeaderRefusesWhatNetHTTPRefuses(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Header.Get("X-Probe"))
	}))
	t.Cleanup(srv.Close)

	for _, text := range []string{"", "a\x00b", "\xff\xfe", "a/b,c#0", "\x00\xffA", "a\nb", " a ", "a\tb", "a\x7fb"} {
		survives := sendsBack(t, srv.URL, text)
		spellable := fieldValue(text) == nil

		if spellable != survives {
			t.Errorf("this plane says %q is spellable=%v, and net/http says it survives=%v",
				text, spellable, survives)
		}
	}
}

// sendsBack reports whether a header value reaches a server unchanged.
func sendsBack(t *testing.T, target, text string) bool {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	req.Header.Set("X-Probe", text)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the response: %v", err)
	}

	return string(body) == text
}

// queryCtx is one request's query parameters, parsed from the string a caller
// would see on the wire.
func queryCtx(t *testing.T, query string) context.Context {
	t.Helper()

	v, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("parsing %q: %v", query, err)
	}

	return WithQuery(t.Context(), v)
}

func loadErr[T any](t *testing.T, query string) error {
	t.Helper()

	_, err := ferry.Load[T](queryCtx(t, query), NewQuerySource())
	if err == nil {
		t.Fatalf("loading %q succeeded, want a refusal", query)
	}

	return err
}

func loadHeaderErr[T any](t *testing.T, h http.Header) error {
	t.Helper()

	_, err := ferry.Load[T](WithHeaders(t.Context(), h), NewHeaderSource())
	if err == nil {
		t.Fatal("loading the headers succeeded, want a refusal")
	}

	return err
}

// addressOf is the address a refusal is attached to, or the empty path.
func addressOf(err error) ferry.Path {
	var e *ferry.Error
	if errors.As(err, &e) {
		return e.Address()
	}

	return ferry.Path{}
}
