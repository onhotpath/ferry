package kv_test

import (
	"testing"
	"time"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/kv"
	"github.com/onhotpath/ferry/ferrytest"
)

// prefix is what every plane in this package is built under, so that the prefix
// composes with the address set in the conformance run rather than only in the
// test that names it.
const prefix = "app"

// rootKey is what this plane calls the root address, so that the conformance
// run exercises the root-leaf case rather than skipping it. Without one this
// driver has no key for the root and refuses it, which is the other legitimate
// answer and is what every plane in the rest of this package gives (#334).
const rootKey = "value"

// TestDriver is the conformance suite, in one call.
func TestDriver(t *testing.T) {
	t.Parallel()

	ferrytest.Driver(t, kvPlane(t))
}

// kvPlane describes this driver to the conformance suite.
//
// # The kinds are a declaration, and this is what it declares
//
// Every kind but Null. That is a claim about what survives a write and a read
// back through this store, not about what the store remembers: a Number is
// written as its own text and read back as a String, and every Go leaf takes a
// String and parses it, so a proof carrying a Number round-trips exactly.
//
// Null is the one that does not, and it is the whole of what a flattening plane
// refuses. ADR-0005 measured this plane class - "reports String for everything
// and has no null" - at 11 of 11 core types with 3 values refused, and the
// three are exactly the goldens that spell a Null: []byte(nil), []string(nil)
// and []string{}. Declaring fewer kinds would demand a loud refusal of every
// bool and every integer, which this plane stores perfectly well.
func kvPlane(t *testing.T) ferrytest.Plane {
	t.Helper()

	return ferrytest.Plane{
		Name: "kv",
		Kinds: []ferry.VKind{
			ferry.KindAbsent, ferry.KindBool, ferry.KindNumber, ferry.KindString, ferry.KindBytes,
		},
		Open: func() ferrytest.Instance {
			// A fresh store per call, which is ADR-0014's fresh-destination
			// rule: a plane shared across cases is the defect that hides a
			// broken second walk.
			store := newFake()

			return ferrytest.Instance{
				Source:   mustSource(t, store, kv.RootKey(rootKey)),
				Sink:     mustSink(t, store, kv.RootKey(rootKey)),
				Contents: func() ([]byte, error) { return store.contents(), nil },
			}
		},
		Golden: goldens(),
	}
}

// goldens pin this driver's own spelling of two fixed values.
//
// They are the one thing a round trip structurally cannot see: a round trip
// tests a function against its own inverse, so changing both halves together is
// invisible to it. What is pinned here is the whole of what this plane holds -
// the key each address renders to, including the prefix, and the bytes each
// value is stored as - and a change to either is a change to what every store
// ferry has ever written means (ADR-0013).
func goldens() []ferrytest.Artefact {
	return []ferrytest.Artefact{
		ferrytest.Golden(leaves{Host: "h", Port: 8080, Raw: []byte("\x00\xffA"), Wait: 30 * time.Second},
			`"app/host" = "h"`+"\n"+
				`"app/port" = "8080"`+"\n"+
				`"app/raw" = "\x00\xffA"`+"\n"+
				`"app/wait" = "30s"`+"\n"),
		ferrytest.Golden(nested{DB: section{Host: "h"}, Tags: []string{"a", "b"}},
			`"app/db/host" = "h"`+"\n"+
				`"app/tags/0" = "a"`+"\n"+
				`"app/tags/1" = "b"`+"\n"),
	}
}

// leaves is the golden row that pins one spelling per boundary kind this plane
// carries: text, a number, opaque bytes and an identity leaf.
type leaves struct {
	Host string        `ferry:"host"`
	Port int           `ferry:"port"`
	Raw  []byte        `ferry:"raw"`
	Wait time.Duration `ferry:"wait"`
}

// nested is the golden row that pins the key structure: a prefix and a nested
// struct are segments joined with the store's separator, and a sequence
// position is its own base-10 text.
type nested struct {
	DB   section  `ferry:"db"`
	Tags []string `ferry:"tags"`
}

type section struct {
	Host string `ferry:"host"`
}

func mustSource(t *testing.T, store kv.Client, opts ...kv.Option) *kv.Source {
	t.Helper()

	src, err := kv.NewSource(store, append([]kv.Option{kv.WithPrefix(prefix)}, opts...)...)
	if err != nil {
		t.Fatalf("kv.NewSource: %v", err)
	}

	return src
}

func mustSink(t *testing.T, store kv.Client, opts ...kv.Option) *kv.Sink {
	t.Helper()

	sink, err := kv.NewSink(store, append([]kv.Option{kv.WithPrefix(prefix)}, opts...)...)
	if err != nil {
		t.Fatalf("kv.NewSink: %v", err)
	}

	return sink
}
