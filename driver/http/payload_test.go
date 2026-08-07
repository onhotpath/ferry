package ferryhttp_test

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	ferryhttp "github.com/onhotpath/ferry/driver/http"
	"github.com/onhotpath/ferry/ferrytest"
)

// pem is the payload the tests below carry, and it is the case ADR-0018 names:
// a certificate in a []byte field, on a plane that holds text.
const pem = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"

// carrier is a schema with one payload field, which is what a source declaring
// a payload spelling is for.
type carrier struct {
	Cert []byte `ferry:"cert"`
}

// stack is the composition ADR-0018 works through, built here once so that every
// test below asserts about the same one.
func stack() ferry.Spelling[[]byte, string] {
	return ferry.With(ferryhttp.Base64(), ferryhttp.Gzip(), ferryhttp.MaxSize(4<<10))
}

// TestBase64ObeysTheLaws runs the published proof over this plane's own payload
// spelling, on its own and with no steps under it.
func TestBase64ObeysTheLaws(t *testing.T) {
	t.Parallel()

	ferrytest.Spelling(t, ferryhttp.Base64(), bytes.Equal,
		[][]byte{nil, {}, []byte("a"), []byte(pem), {0x00, 0xff, 0x1f, 0x8b}},
		[]string{"a", "!!!!", "aGVsbG8=extra", "-----BEGIN"},
	)
}

// TestThePayloadStackObeysTheLaws runs the same proof over the composition, which
// is what holds the steps to the spelling rather than each to itself.
func TestThePayloadStackObeysTheLaws(t *testing.T) {
	t.Parallel()

	ferrytest.Spelling(t, stack(), bytes.Equal,
		[][]byte{nil, {}, []byte("a"), []byte(pem), bytes.Repeat([]byte("x"), 3<<10)},
		[]string{"!!!!", base64.StdEncoding.EncodeToString([]byte("not a gzip stream at all")),
			base64.StdEncoding.EncodeToString(gzipped(t, []byte(pem))[:12])},
	)
}

// TestThePayloadStackCapsThenCompressesThenSpells is ADR-0018's worked case as a
// runnable assertion: the step written last is closest to the payload and runs
// first on the way out, so With(Base64(), Gzip(), MaxSize(n)) writes the base64
// of the gzip of a payload the cap already passed.
func TestThePayloadStackCapsThenCompressesThenSpells(t *testing.T) {
	t.Parallel()

	got, err := stack().Render([]byte(pem))
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if want := base64.StdEncoding.EncodeToString(gzipped(t, []byte(pem))); got != want {
		t.Errorf("the stack wrote %q, want the base64 of the gzip of the payload, %q", got, want)
	}

	inner, err := base64.StdEncoding.DecodeString(got)
	if err != nil {
		t.Fatalf("the stack wrote something that is not base64: %v", err)
	}

	if len(inner) < 2 || inner[0] != 0x1f || inner[1] != 0x8b {
		t.Errorf("what the base64 spells is not a gzip stream: % x", inner[:min(2, len(inner))])
	}
}

// TestMaxSizeRefusesOnTheWayOut is the outbound half of a payload step, which is
// the half that is easy to forget: a payload past the budget fails before
// anything is written.
func TestMaxSizeRefusesOnTheWayOut(t *testing.T) {
	t.Parallel()

	_, err := stack().Render(bytes.Repeat([]byte("x"), (4<<10)+1))
	if !errors.Is(err, ferry.ErrValue) {
		t.Fatalf("rendering an oversized payload gave %v, want a value refusal", err)
	}

	if !strings.Contains(err.Error(), "4097 bytes") {
		t.Errorf("the refusal is %q, and it names the size it refused", err)
	}
}

// TestMaxSizeRefusesOnTheWayIn is the same budget read from the other side, and
// it is where a payload that arrives small and decompresses large is caught.
func TestMaxSizeRefusesOnTheWayIn(t *testing.T) {
	t.Parallel()

	bomb := base64.StdEncoding.EncodeToString(gzipped(t, bytes.Repeat([]byte("x"), (4<<10)+1)))

	if len(bomb) > 4<<10 {
		t.Fatalf("the compressed payload is %d bytes, so the cap it is meant to slip past would catch it", len(bomb))
	}

	_, err := stack().Parse(bomb)
	if !errors.Is(err, ferry.ErrValue) {
		t.Fatalf("parsing a payload that decompresses past the budget gave %v, want a value refusal", err)
	}
}

// TestAPayloadLoadsThroughTheSource is the seam the spelling exists behind: a
// request carrying the base64 of the gzip of a certificate fills a []byte field
// with the certificate.
func TestAPayloadLoadsThroughTheSource(t *testing.T) {
	t.Parallel()

	spelled, err := stack().Render([]byte(pem))
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	got, err := ferry.Load[carrier](
		ferryhttp.WithQuery(t.Context(), url.Values{"cert": {spelled}}),
		ferryhttp.NewQuerySource(ferryhttp.BytesAs(stack())),
	)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if string(got.Cert) != pem {
		t.Errorf("loaded %q, want the certificate back", got.Cert)
	}
}

// TestAPayloadLoadsAtEveryPositionOfASequence is the second dimension of this
// plane read as payloads: a repeated name is a sequence, and each of its
// positions goes through the spelling too.
func TestAPayloadLoadsAtEveryPositionOfASequence(t *testing.T) {
	t.Parallel()

	type certs struct {
		Certs [][]byte `ferry:"certs"`
	}

	first, second := spell(t, []byte("one")), spell(t, []byte("two"))

	got, err := ferry.Load[certs](
		ferryhttp.WithQuery(t.Context(), url.Values{"certs": {first, second}}),
		ferryhttp.NewQuerySource(ferryhttp.BytesAs(stack())),
	)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if len(got.Certs) != 2 || string(got.Certs[0]) != "one" || string(got.Certs[1]) != "two" {
		t.Errorf("loaded %q, want the two payloads in the order the request carried them", got.Certs)
	}
}

// TestARefusalNamesTheParameterAndQuotesNothing is this driver's published
// promise holding for a spelling's refusal too: the values are attacker-supplied
// here, so the message names the parameter and never what it held.
func TestARefusalNamesTheParameterAndQuotesNothing(t *testing.T) {
	t.Parallel()

	_, err := ferry.Load[carrier](
		ferryhttp.WithQuery(t.Context(), url.Values{"cert": {"SECRET-TOKEN!!"}}),
		ferryhttp.NewQuerySource(ferryhttp.BytesAs(stack())),
	)
	if !errors.Is(err, ferry.ErrValue) {
		t.Fatalf("loading a value that is not base64 gave %v, want a value refusal", err)
	}

	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("the refusal is %q, and a refusal from this plane never quotes what the request held", err)
	}

	if !strings.Contains(err.Error(), "cert") {
		t.Errorf("the refusal is %q, and it names the parameter it is about", err)
	}
}

// TestAPayloadPlaneHoldsNoText is the sharp edge stated where it is provable: a
// declared spelling is a fact about the whole plane, so a string field over the
// same source is a value the field cannot take rather than the text that
// arrived.
func TestAPayloadPlaneHoldsNoText(t *testing.T) {
	t.Parallel()

	type mixed struct {
		Cert []byte `ferry:"cert"`
		Name string `ferry:"name"`
	}

	_, err := ferry.Load[mixed](
		ferryhttp.WithQuery(t.Context(), url.Values{"cert": {spell(t, []byte(pem))}, "name": {"checkout"}}),
		ferryhttp.NewQuerySource(ferryhttp.BytesAs(stack())),
	)
	if !errors.Is(err, ferry.ErrValue) {
		t.Fatalf("loading a string field from a payload plane gave %v, want a value refusal", err)
	}
}

// TestBytesAsWithNoSpellingIsRefused is where an Option this source cannot be
// built with lands: at Bind, before any request is looked at, which is where
// every other option of this driver is checked.
func TestBytesAsWithNoSpellingIsRefused(t *testing.T) {
	t.Parallel()

	_, err := ferry.Load[carrier](
		ferryhttp.WithQuery(t.Context(), url.Values{"cert": {"x"}}),
		ferryhttp.NewQuerySource(ferryhttp.BytesAs(nil)),
	)
	if !errors.Is(err, ferryhttp.ErrOption) {
		t.Fatalf("loading through a source with an empty BytesAs gave %v, want an option refusal", err)
	}
}

// TestTextIsUnchangedWithoutASpelling pins what a source with no payload
// spelling does, which is what it did before there was one: every value is text,
// and a []byte field takes the bytes of that text.
func TestTextIsUnchangedWithoutASpelling(t *testing.T) {
	t.Parallel()

	got, err := ferry.Load[carrier](
		ferryhttp.WithQuery(t.Context(), url.Values{"cert": {pem}}),
		ferryhttp.NewQuerySource(),
	)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if string(got.Cert) != pem {
		t.Errorf("loaded %q, want the text the request held", got.Cert)
	}
}

// spell is one payload through the stack, for a test that wants the carrier a
// request would hold.
func spell(t *testing.T, payload []byte) string {
	t.Helper()

	spelled, err := stack().Render(payload)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	return spelled
}

// gzipped is the compression computed here rather than through the transform, so
// that what the stack writes is asserted against an independent answer.
func gzipped(t *testing.T, payload []byte) []byte {
	t.Helper()

	var out bytes.Buffer

	w := gzip.NewWriter(&out)
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("compressing: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("closing the compressor: %v", err)
	}

	return out.Bytes()
}
