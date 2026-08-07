package kv_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/kv"
	"github.com/onhotpath/ferry/ferrytest"
)

// payloads is the schema the raw tests load and save, and it is what [kv.Raw]
// is for: a store whose values are bytes rather than text.
type payloads struct {
	Cert []byte `ferry:"cert"`
}

// TestRawObeysTheLaws runs the published proof over this plane's own spelling.
// It carries bytes and refuses nothing, because every byte sequence a store
// holds is a payload and every payload is storable.
func TestRawObeysTheLaws(t *testing.T) {
	t.Parallel()

	ferrytest.Spelling(t, kv.RawSpelling, bytes.Equal,
		[][]byte{nil, {}, []byte("checkout"), {0x00, 0xff, 0x1f, 0x8b}},
		nil,
	)
}

// TestRawLoadsBytesRatherThanText is the read half through the seam: the store's
// bytes reach the field without becoming text on the way.
func TestRawLoadsBytesRatherThanText(t *testing.T) {
	t.Parallel()

	store := newFake()
	store.data["app/cert"] = []byte{0x00, 0xff, 0x1f, 0x8b}

	src, err := kv.NewSource(store, kv.WithPrefix("app"), kv.Raw())
	if err != nil {
		t.Fatalf("source: %v", err)
	}

	got, err := ferry.Load[payloads](t.Context(), src)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !bytes.Equal(got.Cert, []byte{0x00, 0xff, 0x1f, 0x8b}) {
		t.Errorf("loaded % x, want the bytes the store holds", got.Cert)
	}
}

// TestRawLoadsTheSameBytesInABatch is the other open of the same plane: a batch
// read and a lazy read cannot disagree about what the store said.
func TestRawLoadsTheSameBytesInABatch(t *testing.T) {
	t.Parallel()

	store := newFake()
	store.data["app/cert"] = []byte{0x00, 0xff}

	src, err := kv.NewSource(store, kv.WithPrefix("app"), kv.Raw(), kv.WithBatch())
	if err != nil {
		t.Fatalf("source: %v", err)
	}

	got, err := ferry.Load[payloads](t.Context(), src)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !bytes.Equal(got.Cert, []byte{0x00, 0xff}) {
		t.Errorf("loaded % x, want the bytes the store holds", got.Cert)
	}
}

// TestRawSavesTheBytesItWasGiven is the write half, which goes through the same
// spelling so that the two directions of the Option cannot drift apart.
func TestRawSavesTheBytesItWasGiven(t *testing.T) {
	t.Parallel()

	store := newFake()

	sink, err := kv.NewSink(store, kv.WithPrefix("app"), kv.Raw())
	if err != nil {
		t.Fatalf("sink: %v", err)
	}

	if err := ferry.Dump(t.Context(), payloads{Cert: []byte{0x1f, 0x8b, 0x00}}, sink); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got := store.data["app/cert"]; !bytes.Equal(got, []byte{0x1f, 0x8b, 0x00}) {
		t.Errorf("stored % x, want the bytes the field held", got)
	}
}

// TestRawRoundTripsAPayload closes the loop the laws promise, through the two
// halves of the driver rather than through the spelling alone.
func TestRawRoundTripsAPayload(t *testing.T) {
	t.Parallel()

	store := newFake()
	want := payloads{Cert: []byte{0x00, 0x01, 0xff}}

	sink, err := kv.NewSink(store, kv.WithPrefix("app"), kv.Raw())
	if err != nil {
		t.Fatalf("sink: %v", err)
	}

	if err := ferry.Dump(t.Context(), want, sink); err != nil {
		t.Fatalf("dump: %v", err)
	}

	src, err := kv.NewSource(store, kv.WithPrefix("app"), kv.Raw())
	if err != nil {
		t.Fatalf("source: %v", err)
	}

	got, err := ferry.Load[payloads](t.Context(), src)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !bytes.Equal(got.Cert, want.Cert) {
		t.Errorf("round tripped % x, want % x", got.Cert, want.Cert)
	}
}

// TestRawMakesTheWholeStoreAPayloadStore is the sharp edge stated where it is
// provable: the Option is a fact about the store and not about one field, so a
// string field over the same source is a value the field cannot take.
func TestRawMakesTheWholeStoreAPayloadStore(t *testing.T) {
	t.Parallel()

	type mixed struct {
		Cert []byte `ferry:"cert"`
		Name string `ferry:"name"`
	}

	store := newFake()
	store.data["app/cert"] = []byte{0xff}
	store.data["app/name"] = []byte("checkout")

	src, err := kv.NewSource(store, kv.WithPrefix("app"), kv.Raw())
	if err != nil {
		t.Fatalf("source: %v", err)
	}

	if _, err := ferry.Load[mixed](t.Context(), src); !errors.Is(err, ferry.ErrValue) {
		t.Fatalf("loading a string field from a payload store gave %v, want a value refusal", err)
	}
}

// TestWithoutRawAValueIsStillText pins what a store with no spelling declared
// does, which is what it did before the Option existed: every value is text, and
// every field parses it with its own parser.
func TestWithoutRawAValueIsStillText(t *testing.T) {
	t.Parallel()

	type mixed struct {
		Cert []byte `ferry:"cert"`
		Port int    `ferry:"port"`
	}

	store := newFake()
	store.data["app/cert"] = []byte("pem")
	store.data["app/port"] = []byte("8080")

	src, err := kv.NewSource(store, kv.WithPrefix("app"))
	if err != nil {
		t.Fatalf("source: %v", err)
	}

	got, err := ferry.Load[mixed](t.Context(), src)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if string(got.Cert) != "pem" || got.Port != 8080 {
		t.Errorf("loaded %+v, want the text every field parsed for itself", got)
	}
}
