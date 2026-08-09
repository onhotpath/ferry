package protect_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/windows/protect"
	"github.com/onhotpath/ferry/driver/windows/winreg"
)

// This file is the composition this module's own argument rests on: protection
// over the registry, which is what makes a credential in HKCU readable by the
// principal the descriptor names and by nobody else.
//
// Every other test here runs the decorator over an ordinary address-keyed store,
// which is the right shape for proving that it composes with any plane. This one
// proves the specific pairing, over the registry driver's own exported seam:
// [winreg.Store] takes a [winreg.Registry], so a store written here is the whole
// of what it takes to run both drivers with no Windows underneath.
//
// The registry fake is written in this package rather than borrowed. The one
// beside `winreg` lives in that package's own _test.go and is unreachable from
// here, and exporting it would put test apparatus on the module's surface.

// The subkey both halves are built over.
const regKey = `Software\Ferry\Test`

func TestProtectOverTheRegistryWritesCiphertextAtTheMarkedNameAndNowhereElse(t *testing.T) {
	t.Parallel()

	r, k := newRegStore(), newKeeper()
	_, dst := regHalves(r, k)
	want := conf{Auth: auth{Token: "s3cr3t", User: "bob"}, Plain: "public"}

	if err := ferry.Dump(t.Context(), want, dst, ferry.WithRegistry(declaring())); err != nil {
		t.Fatalf("dumping through protect over winreg: %v", err)
	}

	if r.holds("s3cr3t") {
		t.Fatalf("the registry holds the secret in the clear: %+v", r.at("auth", "token"))
	}

	token := r.at("auth", "token")
	if !strings.HasPrefix(token.Text, "ferry-protect:1:") {
		t.Errorf("the marked name holds %+v, want this package's marker and a ciphertext", token)
	}

	if token.Type != winreg.TypeString {
		t.Errorf("the ciphertext landed as %v, want REG_SZ: what this package writes is always a string",
			token.Type)
	}

	// The two unmarked addresses are the control: one beside the marked value in
	// the same subkey, and one in the key above it.
	if got := r.at("auth", "user"); got.Text != "bob" {
		t.Errorf("the unmarked name beside it holds %+v, want bob in the clear", got)
	}

	if got := r.at("", "plain"); got.Text != "public" {
		t.Errorf("the unmarked name in the key above holds %+v, want public in the clear", got)
	}
}

func TestProtectOverTheRegistryRoundTrips(t *testing.T) {
	t.Parallel()

	r, k := newRegStore(), newKeeper()
	src, dst := regHalves(r, k)
	want := conf{Auth: auth{Token: "s3cr3t", User: "bob"}, Plain: "public"}

	if err := ferry.Dump(t.Context(), want, dst, ferry.WithRegistry(declaring())); err != nil {
		t.Fatalf("dumping through protect over winreg: %v", err)
	}

	got, err := ferry.Load[conf](t.Context(), src, ferry.WithRegistry(declaring()))
	if err != nil {
		t.Fatalf("loading back through protect over winreg: %v", err)
	}

	if got != want {
		t.Errorf("it came back as %+v, want %+v", got, want)
	}
}

// regHalves is both decorated halves over one registry store, built the way this
// package's documentation tells a caller to build them.
func regHalves(r winreg.Registry, k protect.Protector) (ferry.Source, ferry.Sink) {
	return protect.Over(winreg.NewSource(winreg.CurrentUser, regKey, winreg.Store(r)),
			protect.LocalSystem, protect.FromTags(), protect.Using(k)),
		protect.OverSink(winreg.NewSink(winreg.CurrentUser, regKey, winreg.Store(r)),
			protect.LocalSystem, protect.FromTags(), protect.Using(k))
}

// regStore is a [winreg.Registry] in memory: a subkey path to the values under
// it, and the set of subkeys that exist.
//
// The two maps are the registry's two namespaces, which is the axis that driver
// occupies: a value and a subkey of one name coexist under one key, so a listing
// answers about both and neither shadows the other.
type regStore struct {
	mu   sync.Mutex
	vals map[string]map[string]winreg.Datum
	keys map[string]bool
}

var _ winreg.Registry = (*regStore)(nil)

func newRegStore() *regStore {
	return &regStore{vals: map[string]map[string]winreg.Datum{}, keys: map[string]bool{"": true}}
}

// at is what the registry holds at one name, for a test that asserts on the
// stored form rather than on what a load gave back.
func (r *regStore) at(subkey, name string) winreg.Datum {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.vals[subkey][name]
}

// holds reports some stored value carrying this text, which is how a test says
// "the secret is not in the registry" and means it.
func (r *regStore) holds(text string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, names := range r.vals {
		for _, d := range names {
			if strings.Contains(d.Text, text) || strings.Contains(string(d.Binary), text) {
				return true
			}
		}
	}

	return false
}

func (r *regStore) Get(_ context.Context, subkey, name string) (winreg.Datum, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	d, found := r.vals[subkey][name]

	return d, found, nil
}

func (r *regStore) List(_ context.Context, subkey string) (winreg.Listing, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.keys[subkey] {
		return winreg.Listing{}, false, nil
	}

	var l winreg.Listing

	for name := range r.vals[subkey] {
		l.Values = append(l.Values, name)
	}

	for key := range r.keys {
		if child, under := immediate(subkey, key); under {
			l.Keys = append(l.Keys, child)
		}
	}

	slices.Sort(l.Values)
	slices.Sort(l.Keys)

	return l, true, nil
}

func (r *regStore) Set(_ context.Context, subkey, name string, d winreg.Datum) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.ensure(subkey)

	if r.vals[subkey] == nil {
		r.vals[subkey] = map[string]winreg.Datum{}
	}

	r.vals[subkey][name] = d

	return nil
}

func (r *regStore) Create(_ context.Context, subkey string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.ensure(subkey)

	return nil
}

func (r *regStore) DeleteValue(_ context.Context, subkey, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.vals[subkey], name)

	return nil
}

func (r *regStore) DeleteKey(_ context.Context, subkey string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for key := range r.keys {
		if key == subkey || strings.HasPrefix(key, subkey+`\`) {
			delete(r.keys, key)
			delete(r.vals, key)
		}
	}

	return nil
}

// ensure marks a subkey and every key above it as present, which is the implicit
// creation the write path is held to.
func (r *regStore) ensure(subkey string) {
	parts := strings.Split(subkey, `\`)

	for i := range parts {
		if up := strings.Join(parts[:i+1], `\`); up != "" {
			r.keys[up] = true
		}
	}
}

// immediate is the one segment by which key extends parent, and whether it is a
// direct child of it at all.
func immediate(parent, key string) (string, bool) {
	if parent == "" {
		return key, key != "" && !strings.Contains(key, `\`)
	}

	rest, under := strings.CutPrefix(key, parent+`\`)

	return rest, under && !strings.Contains(rest, `\`)
}
