package kv_test

import (
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/kv"
)

// TestOptionsRefused is every way the Option list can be wrong, reported at the
// constructor rather than at the first read.
//
// The prefix cases are ADR-0003's rule with teeth: a prefix prepends segments,
// so the two spellings that make xload's prefix a typo nobody can detect - one
// that concatenates text and one that spells the separator - are refused here
// rather than producing a key nothing complains about.
func TestOptionsRefused(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		opts []kv.Option
		want string
	}{
		{name: "no segments", opts: []kv.Option{kv.WithPrefix()}, want: "no segments"},
		{name: "an empty segment", opts: []kv.Option{kv.WithPrefix("app", "")}, want: "empty"},
		{
			name: "a segment spelling the separator",
			opts: []kv.Option{kv.WithPrefix("app/cfg")},
			want: "never concatenates text",
		},
		{
			name: "two prefixes",
			opts: []kv.Option{kv.WithPrefix("app"), kv.WithPrefix("other")},
			want: "given twice",
		},
		{name: "a nil Option", opts: []kv.Option{nil}, want: "nil Option"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertRefusedOptions(t, tc.name, tc.want, tc.opts)
		})
	}
}

// assertRefusedOptions holds one Option list to being refused by both
// constructors, with a message that says which of them was wrong.
//
// Both, because one Option list is resolved the same way on either side: an
// Option that is wrong is wrong before either half of the plane exists.
func assertRefusedOptions(t *testing.T, name, want string, opts []kv.Option) {
	t.Helper()

	src, err := kv.NewSource(newFake(), opts...)
	if err == nil {
		t.Fatalf("NewSource accepted %s and handed back %v", name, src)
	}

	if !strings.Contains(err.Error(), want) {
		t.Errorf("NewSource refused with %q, which does not say %q", err, want)
	}

	if sink, err := kv.NewSink(newFake(), opts...); err == nil {
		t.Errorf("NewSink accepted %s and handed back %v", name, sink)
	}
}

// TestEveryBadOptionIsReported is the aggregation rule applied to the Option
// list itself: a caller who got two of them wrong is told about both.
func TestEveryBadOptionIsReported(t *testing.T) {
	t.Parallel()

	_, err := kv.NewSource(newFake(), kv.WithPrefix(""), nil)
	if err == nil {
		t.Fatal("NewSource accepted an empty prefix segment beside a nil Option")
	}

	for _, want := range []string{"empty", "nil Option"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not mention %q, so one of two mistakes went unreported", err, want)
		}
	}
}

// TestNilClientIsRefused is both constructors refusing a plane that was never
// supplied, at the call that can still say so.
func TestNilClientIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := kv.NewSource(nil); err == nil {
		t.Error("NewSource accepted a nil client")
	}

	if _, err := kv.NewSink(nil); err == nil {
		t.Error("NewSink accepted a nil client")
	}
}

// TestSinkRefusesWithBatch is an Option refusing to be silently ignored: a sink
// stages every write and commits them together, so there is no per-key half
// for the batch choice to select against, and ADR-0001 rules out ignoring
// anything quietly.
func TestSinkRefusesWithBatch(t *testing.T) {
	t.Parallel()

	sink, err := kv.NewSink(newFake(), kv.WithBatch())
	if err == nil {
		t.Fatalf("NewSink accepted WithBatch and handed back %v", sink)
	}

	if !strings.Contains(err.Error(), "source's Option") {
		t.Errorf("NewSink refused with %q, which does not say whose Option it is", err)
	}
}

// TestRootKeyNamesOnlyTheRoot is [kv.RootKey] doing exactly one thing: it is an
// Option both constructors take, and it moves no key of any other address.
//
// The key it does name is asserted where the mapping is, in TestKeyFunc, because
// no schema can mint a root address yet and there is no load or save that
// reaches one.
func TestRootKeyNamesOnlyTheRoot(t *testing.T) {
	t.Parallel()

	store, plain := newFake(), newFake()

	sink, err := kv.NewSink(store, kv.WithPrefix(prefix), kv.RootKey("value"))
	if err != nil {
		t.Fatalf("NewSink refused kv.RootKey: %v", err)
	}

	if _, err := kv.NewSource(newFake(), kv.WithPrefix(prefix), kv.RootKey("value")); err != nil {
		t.Fatalf("NewSource refused kv.RootKey: %v", err)
	}

	cfg := three{Host: "h", Port: "8080", Zone: "eu"}
	dumpInto(t, cfg, sink)
	dumpInto(t, cfg, mustSink(t, plain))

	if got, want := string(store.contents()), string(plain.contents()); got != want {
		t.Errorf("the store holds\n%s\nwant what a sink without kv.RootKey wrote\n%s", got, want)
	}
}

// dumpInto saves one value through one sink, failing the test where the save
// itself is what went wrong.
func dumpInto(t *testing.T, cfg three, sink *kv.Sink) {
	t.Helper()

	if err := ferry.Dump(t.Context(), cfg, sink); err != nil {
		t.Fatalf("Dump: %v", err)
	}
}
