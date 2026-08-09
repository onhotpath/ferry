package kv_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/kv"
)

// These are the published-seam half of the root-key work: a schema whose only
// address is the root now exists, so what the key function was asserted about
// directly can be asserted through Bind, Dump and Load.

// TestARootLeafIsRefusedWhereNoRootKeyNamesIt is the default answer. This
// driver's key for the root would be the prefix itself, which is the folder
// every other address is written under, so it refuses instead - at Bind, where
// the caller still has the whole schema in hand and nothing has been asked of
// the store.
func TestARootLeafIsRefusedWhereNoRootKeyNamesIt(t *testing.T) {
	t.Parallel()

	t.Run("the source half", refusesTheRoot(func(t *testing.T, store *fake) error {
		t.Helper()
		_, err := ferry.Bind[int](mustSource(t, store))

		return err
	}))

	t.Run("and the sink half", refusesTheRoot(func(t *testing.T, store *fake) error {
		t.Helper()
		_, err := ferry.BindSink[int](mustSink(t, store))

		return err
	}))
}

// refusesTheRoot is one half of the plane bound against a fresh store: the bind
// fails, the refusal names the option that lifts it, and the store was never
// reached.
func refusesTheRoot(bind func(*testing.T, *fake) error) func(*testing.T) {
	return func(t *testing.T) {
		t.Parallel()

		store := newFake()

		err := bind(t, store)
		if err == nil {
			t.Fatal("the driver bound a schema whose only address is the root, with no key to write it at")
		}

		checkRootRefusal(t, err)

		if store.calls() != 0 {
			t.Errorf("the store answered %d calls for a schema that never bound", store.calls())
		}
	}
}

func checkRootRefusal(t *testing.T, err error) {
	t.Helper()

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("the refusal is %+v, want one carrying ferry.ErrPlane", err)
	}

	if !strings.Contains(err.Error(), "RootKey") {
		t.Errorf("the refusal is %q, want it to name the option that lifts it", err.Error())
	}
}

// TestARootLeafIsWrittenAtTheKeyRootKeyNames is the other answer: given a name
// for the root, the value lands on an ordinary key under the prefix, and it is
// one key and not the folder.
func TestARootLeafIsWrittenAtTheKeyRootKeyNames(t *testing.T) {
	t.Parallel()

	store := newFake()

	// A sibling under the same prefix, seeded before the dump: a root leaf is
	// one leaf and not a composite, so nothing about it is a replacement.
	if err := store.Put(t.Context(), "app/other", []byte("kept")); err != nil {
		t.Fatalf("seeding the store: %v", err)
	}

	if err := ferry.Dump(t.Context(), 8080, mustSink(t, store, kv.RootKey("value"))); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	want := `"app/other" = "kept"` + "\n" + `"app/value" = "8080"` + "\n"
	if got := string(store.contents()); got != want {
		t.Errorf("the store holds\n%s\nwant\n%s", got, want)
	}
}

// TestARootLeafRoundTripsThroughTheKeyRootKeyNames is both halves reading the
// same name, which is what makes the option a plane rule rather than a
// write-side spelling.
func TestARootLeafRoundTripsThroughTheKeyRootKeyNames(t *testing.T) {
	t.Parallel()

	store := newFake()

	if err := ferry.Dump(t.Context(), 8080, mustSink(t, store, kv.RootKey("value"))); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	got, err := ferry.Load[int](t.Context(), mustSource(t, store, kv.RootKey("value")))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got != 8080 {
		t.Errorf("loaded %d, want the value dumped at the root", got)
	}
}
