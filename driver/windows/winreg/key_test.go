package winreg_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/windows/winreg"
)

// The schemas the naming tests are written against.
//
// They are ordinary structs and they come in through the schema compiler, which
// is the same door core comes in through: the three address kinds are sealed and
// nothing else mints one, so a test that wants to hand this driver an address
// compiles a type for it.
type (
	// hostTwice is the collision the whole module exists to refuse: two leaves
	// that differ only in case, which the registry stores as one value with the
	// second write's data and no error anywhere.
	hostTwice struct {
		Upper string `ferry:"Host"`
		Lower string `ferry:"host"`
	}

	// alphaSection and alphaComposite are a section and a composite at one
	// folded name. Both are the subkey alpha, so one of the two really would be
	// lost.
	alphaSection struct {
		Alpha alphaInner        `ferry:"Alpha"`
		Map   map[string]string `ferry:"alpha"`
	}
	alphaInner struct {
		Host string `ferry:"host"`
	}

	// leafBesideSection is the pair that must be accepted: the value a and the
	// subkey a under one key are two registry objects, and refusing them would
	// refuse a schema this plane holds perfectly.
	leafBesideSection struct {
		Leaf    string    `ferry:"A"`
		Section innerHost `ferry:"a"`
	}
	innerHost struct {
		Host string `ferry:"host"`
	}

	// backslashLeaf and backslashSection are the illegal name in the terminal
	// segment and in a non-terminal one. Both are refused: with a backslash legal
	// in a value name, /db\host and /db/host would be two registry objects
	// producing one plane key.
	backslashLeaf struct {
		Host string `ferry:"db\\host"`
	}
	backslashSection struct {
		DB innerHost `ferry:"db\\x"`
	}

	// tagsMap is the schema the runtime checks are made through: a map whose keys
	// are values rather than type information, so its addresses are minted during
	// the dump and checked as they are minted.
	tagsMap struct {
		Tags map[string]string `ferry:"tags"`
	}
)

// TestBindRefusesACollidingSchema is the assertion conformance case 7 cannot
// make.
//
// The suite's own injectivity case accepts a Bind that does not refuse, by
// design, because a tree driver builds no plane key and carries no such
// obligation and the suite cannot tell the two apart from outside. So a driver
// that silently collapsed /Host and /host would pass it. This is where that is
// held to.
func TestBindRefusesACollidingSchema(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		bind func(ferry.Source) error
		pair [2]string
	}{
		"two leaves differing only in case": {
			bind: bindOf[hostTwice],
			pair: [2]string{"/Host", "/host"},
		},
		"a section and a composite at one folded name": {
			bind: bindOf[alphaSection],
			pair: [2]string{"/Alpha", "/alpha"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := tc.bind(source(newFake()))
			if err == nil {
				t.Fatalf("Bind accepted %s and %s, which the registry stores as one object", tc.pair[0], tc.pair[1])
			}

			assertPlaneRefusalNaming(t, err, tc.pair)
		})
	}
}

// TestBindAcceptsWhatTheNamespacesKeepApart is the other half of the same rule,
// and without it the test above is satisfied by a driver that refuses everything.
//
// It is also the assertion that stops somebody later "fixing" the namespace split.
// A value and a subkey of one name are two registry objects, and a driver that
// refused the pair would refuse a schema this plane holds.
func TestBindAcceptsWhatTheNamespacesKeepApart(t *testing.T) {
	t.Parallel()

	if err := bindOf[leafBesideSection](source(newFake())); err != nil {
		t.Errorf("Bind refused a leaf beside a section of the same name, which the registry keeps apart: %v", err)
	}
}

// TestBindRefusesAnUnnameableAddress is the legality half, and the backslash rows
// are where this driver differs from the shape the issue behind it proposed: a
// backslash is refused in every part, the terminal one included.
func TestBindRefusesAnUnnameableAddress(t *testing.T) {
	t.Parallel()

	cases := map[string]func(ferry.Source) error{
		"a backslash in the value's own name": bindOf[backslashLeaf],
		"a backslash in a subkey's name":      bindOf[backslashSection],
	}

	for name, bind := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkUnnameable(t, bind(source(newFake())))
		})
	}
}

// checkUnnameable holds one legality refusal to naming this driver's own
// sentinel and to reaching ferry's class for it.
func checkUnnameable(t *testing.T, err error) {
	t.Helper()

	if !errors.Is(err, winreg.ErrIllegalName) {
		t.Fatalf("Bind answered %v, want an error reaching winreg.ErrIllegalName", err)
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("the refusal does not reach ferry.ErrPlane: %v", err)
	}
}

// TestMintedKeysAreCheckedAsTheyAreMinted is the runtime tier of the same rule,
// and it is the one conformance covers least: a map's keys are the caller's data,
// so they are in no address set and reach the plane during the dump.
//
// The two rows are the two ways a map key can be impossible here: it folds onto a
// key this dump has already used, or it has no registry name at all.
func TestMintedKeysAreCheckedAsTheyAreMinted(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		tags map[string]string
		want error
	}{
		"two keys differing only in case": {
			tags: map[string]string{"Host": "a", "host": "b"},
			want: ferry.ErrPlane,
		},
		"a key holding a backslash": {
			tags: map[string]string{"a\\b": "x"},
			want: winreg.ErrIllegalName,
		},
		// Core refuses an empty map key before this driver is reached, so the
		// key function's own refusal of an empty part is unreachable through the
		// walk. It is still the honest answer for anything that reaches it, and
		// what this row holds to is that the dump is refused and writes nothing.
		"an empty key": {
			tags: map[string]string{"": "x"},
			want: ferry.ErrValue,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkMintRefused(t, tc.tags, tc.want)
		})
	}
}

// checkMintRefused dumps one map and holds the refusal to the class it belongs
// to and to having written nothing.
func checkMintRefused(t *testing.T, tags map[string]string, want error) {
	t.Helper()

	store := newFake()

	err := ferry.Dump(t.Context(), tagsMap{Tags: tags}, sink(store))
	if !errors.Is(err, want) {
		t.Fatalf("Dump answered %v, want an error reaching %v", err, want)
	}

	if held := string(store.contents()); strings.Contains(held, "tags") {
		t.Errorf("the refused dump left this behind:\n%s", held)
	}
}

// TestOneDumpWritesOneValuePerName is driver/env's own backstop, copied rather
// than shared because ADR-0002 forbids the internal module that would carry it.
//
// Core's own minting should make it unreachable through the walk, which is
// exactly why it is worth having: it is the invariant that still holds if this
// driver ever stages by a pair it computed itself, and it converts the registry's
// silent second-write-wins into a refusal at the last possible moment.
func TestOneDumpWritesOneValuePerName(t *testing.T) {
	t.Parallel()

	// The map fixture is the only way to reach the writer twice at one name, and
	// core refuses the pair first, so what this asserts is that the refusal
	// happens at all and that nothing was written.
	store := newFake()

	if err := ferry.Dump(t.Context(), tagsMap{Tags: map[string]string{"A": "1", "a": "2"}}, sink(store)); err == nil {
		t.Fatal("Dump wrote two values at one registry name")
	}

	if held := string(store.contents()); strings.Contains(held, "val") {
		t.Errorf("the refused dump wrote a value:\n%s", held)
	}
}

// bindOf is one schema bound to one source, which is the moment every check in
// this file lands at.
func bindOf[T any](src ferry.Source) error {
	_, err := ferry.Bind[T](src)

	return err
}

// assertPlaneRefusalNaming holds a refusal to naming both offending addresses and
// to reaching ferry.ErrPlane, which is what makes it actionable.
func assertPlaneRefusalNaming(t *testing.T, err error, pair [2]string) {
	t.Helper()

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("the refusal does not reach ferry.ErrPlane: %v", err)
	}

	for _, at := range pair {
		if !strings.Contains(err.Error(), at) {
			t.Errorf("the refusal does not name %s: %v", at, err)
		}
	}
}
