package protect_test

import (
	"errors"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/windows/protect"
)

func TestAReadFailureFromThePlaneReachesTheCallerAsAFailure(t *testing.T) {
	t.Parallel()

	src := protect.Over(erringSource{}, protect.LocalSystem, protect.FromTags(), protect.Using(newKeeper()))

	if _, err := ferry.Load[conf](t.Context(), src, ferry.WithRegistry(declaring())); !errors.Is(err, errRefused) {
		t.Errorf("a Get that failed reached the caller as %v, want %v: a decorator adds nothing to that", err,
			errRefused)
	}
}

func TestAMarkedAddressTheStoreHoldsSomethingUnprotectedAtIsReadAsItStands(t *testing.T) {
	t.Parallel()

	type kinds struct {
		Count int    `ferry:"count" protect:"secret"`
		Text  string `ferry:"text" protect:"secret"`
	}

	s := newStore()
	s.seed(ferry.At("count"), ferry.Number("7"))
	s.seed(ferry.At("text"), ferry.String("plain"))

	src := protect.Over(storeSource{s: s}, protect.LocalSystem, protect.FromTags(), protect.Using(newKeeper()))

	got, err := ferry.Load[kinds](t.Context(), src, ferry.WithRegistry(declaring()))
	if err != nil {
		t.Fatalf("loading a store nothing protected: %v", err)
	}

	if got.Count != 7 || got.Text != "plain" {
		t.Errorf("it read back as %+v, want a 7 and %q: a value that carries no marker is the plane's own answer",
			got, "plain")
	}
}

func TestAMarkerHandedBackAsBytesIsStillRecognised(t *testing.T) {
	t.Parallel()

	type kinds struct {
		Raw []byte `ferry:"raw" protect:"secret"`
	}

	s, k := newStore(), newKeeper()

	blob, err := k.Protect(t.Context(), string(protect.LocalSystem), []byte("yhidden"))
	if err != nil {
		t.Fatalf("staging the blob: %v", err)
	}

	// What this package writes is always a String. A plane may hand it back as
	// bytes where the field it is loading into is a []byte, which is what the
	// kind gate on a leaf address buys a flat driver, so the marker is looked for
	// in both.
	s.seed(ferry.At("raw"), ferry.Bytes([]byte("ferry-protect:1:"+encoded(blob))))

	src := protect.Over(storeSource{s: s}, protect.LocalSystem, protect.FromTags(), protect.Using(k))

	got, err := ferry.Load[kinds](t.Context(), src, ferry.WithRegistry(declaring()))
	if err != nil {
		t.Fatalf("loading a marker the plane handed back as bytes: %v", err)
	}

	if string(got.Raw) != "hidden" {
		t.Errorf("it read back as %q, want %q", got.Raw, "hidden")
	}
}
