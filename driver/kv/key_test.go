package kv

import (
	"errors"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
)

// TestKeyFunc pins the mapping from an address to a store key, including the two
// shapes no transformation rescues.
//
// It is asserted here rather than through a load or a save because a store key
// is what every stored artefact of this plane is named by, and because the two
// refusals are the driver's half of ADR-0003's legality obligation: core refuses
// an empty minted name at the mapping before this driver is asked (#258), so the
// empty-part row has no other way in and would otherwise be a guard nothing
// proves. The root rows are here because this is where the mapping is stated in
// one table; what they mean at the verbs is in rootleaf_test.go, through Bind,
// Dump and Load (#334, #335).
//
// RootKey("") and no RootKey at all are one configuration and one refusal, since
// the name a caller did not give and the empty name they did give are the same
// empty string. Both rows are kept, because the second is the one that would
// otherwise slip through as the prefix itself.
func TestKeyFunc(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		prefix  []string
		rootKey string
		addr    ferry.Path
		want    string
		refuse  string
	}{
		"a leaf":            {addr: ferry.At("host"), want: "host"},
		"a nested leaf":     {addr: ferry.At("db", "host"), want: "db/host"},
		"a position":        {addr: ferry.At("tags").Elem(0), want: "tags/0"},
		"under a prefix":    {prefix: []string{"app"}, addr: ferry.At("db", "host"), want: "app/db/host"},
		"an empty part":     {addr: ferry.At("labels", ""), refuse: "empty part"},
		"a part with a /":   {addr: ferry.At("labels", "a/b"), refuse: "part containing"},
		"the root, unnamed": {addr: ferry.Path{}, refuse: "no key for one"},
		"the root, named": {
			prefix: []string{"app"}, rootKey: "value", addr: ferry.Path{}, want: "app/value",
		},
		"a root key with a /": {rootKey: "a/b", addr: ferry.Path{}, refuse: "root key"},
		"an empty root key":   {rootKey: "", addr: ferry.Path{}, refuse: "no key for one"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := keyFunc(tc.prefix, tc.rootKey)(tc.addr)
			if tc.refuse == "" {
				checkKey(t, got, tc.want, err)

				return
			}

			checkRefusal(t, err, tc.refuse)
		})
	}
}

// checkKey holds one row that has a key to the key it must have.
func checkKey(t *testing.T, got, want string, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("the key function refused with %v, want the key %q", err, want)
	}

	if got != want {
		t.Errorf("the key is %q, want %q", got, want)
	}
}

// checkRefusal holds one row that has no key to the refusal it must be, which is
// the plane's own class and a message saying what about the address it is.
func checkRefusal(t *testing.T, err error, says string) {
	t.Helper()

	if err == nil {
		t.Fatal("the key function named an address a store cannot name")
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("the refusal is %v, want one carrying ferry.ErrPlane", err)
	}

	if got := err.Error(); !strings.Contains(got, says) {
		t.Errorf("the refusal is %q, want one saying %q", got, says)
	}
}
