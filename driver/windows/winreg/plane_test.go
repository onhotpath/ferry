package winreg_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/windows/winreg"
)

// The schemas the container and failure tests are written against.
type (
	// omittable is a struct that writes nothing at all, so a non-nil pointer to
	// one is the case [ferry.Ensurer] exists for.
	omittable struct {
		X string `ferry:"x,omitzero"`
	}

	// withOptional is the schema that reaches Ensure: a pointer whose subtree
	// emits no write is present and empty rather than absent.
	withOptional struct {
		Opt *omittable `ferry:"opt"`
	}
)

// TestASectionThatIsThereAndEmptySurvives is what this plane can do and a flat
// one cannot.
//
// An empty subkey is a real registry object, so "the section is there and holds
// nothing" and "the section was never written" are two observations here, and the
// round trip keeps them apart. On a plane with no spelling for a container the
// first would come back as the second.
func TestASectionThatIsThereAndEmptySurvives(t *testing.T) {
	t.Parallel()

	store := newFake()

	if err := ferry.Dump(t.Context(), withOptional{Opt: &omittable{}}, sink(store)); err != nil {
		t.Fatalf("Dump: %v", err)
	}

	if held := string(store.contents()); !strings.Contains(held, `key "opt"`) {
		t.Fatalf("the save wrote no subkey for a section that is there and empty:\n%s", held)
	}

	got, err := ferry.LoadOver(t.Context(), withOptional{}, source(store))
	if err != nil {
		t.Fatalf("LoadOver: %v", err)
	}

	if got.Opt == nil {
		t.Error("the section came back absent, so present-and-empty did not survive the round trip")
	}
}

// TestAnAbsentSectionStaysAbsent is the other side of the same observation, and
// without it the test above is satisfied by a driver that materialises everything.
func TestAnAbsentSectionStaysAbsent(t *testing.T) {
	t.Parallel()

	got, err := ferry.LoadOver(t.Context(), withOptional{}, source(newFake()))
	if err != nil {
		t.Fatalf("LoadOver: %v", err)
	}

	if got.Opt != nil {
		t.Error("a section the registry never held came back present")
	}
}

// TestARegistryFailureReachesEveryQuestion is conformance case 4 asked of the two
// questions the suite cannot stage a failure for: the probe at a container's own
// address, and the enumeration of a composite.
func TestARegistryFailureReachesEveryQuestion(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		at   string
		load func(context.Context, ferry.Source) error
	}{
		"probing a container":  {at: "opt", load: loadWith[withOptional]},
		"enumerating a member": {at: "tags", load: loadWith[tagsMap]},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := tc.load(t.Context(), source(newFake().failUnder(tc.at))); !errors.Is(err, errFake) {
				t.Fatalf("Load answered %v, want the registry's own error", err)
			}
		})
	}
}

// TestACommitFailureNamesTheAddress is the write half of the same rule: a
// registry that could not be written is reported, and the report says which
// address could not be written.
func TestACommitFailureNamesTheAddress(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		at   string
		dump func(context.Context, ferry.Sink) error
	}{
		"a value that could not be written": {
			at:   "A",
			dump: dumpWith(bothNamespaces{Leaf: "x", Section: innerHost{Host: "h"}}),
		},
		"a subkey that could not be made": {at: "opt", dump: dumpWith(withOptional{Opt: &omittable{}})},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := tc.dump(t.Context(), sink(newFake().failUnder(tc.at))); !errors.Is(err, errFake) {
				t.Fatalf("Dump answered %v, want the registry's own error", err)
			}
		})
	}
}

// TestASweepFailureIsReported is the removal half: a listing that could not be
// read is one error and not one per member, because what could not be read is the
// key and the members under it were never learned.
func TestASweepFailureIsReported(t *testing.T) {
	t.Parallel()

	store := newFake()
	if err := ferry.Dump(t.Context(), tagsMap{Tags: map[string]string{"a": "1"}}, sink(store)); err != nil {
		t.Fatalf("the first Dump: %v", err)
	}

	store.failUnder("tags")

	err := ferry.Dump(t.Context(), tagsMap{Tags: map[string]string{"b": "2"}}, sink(store))
	if !errors.Is(err, errFake) {
		t.Fatalf("Dump answered %v, want the registry's own error", err)
	}
}

// TestAValueAndASubkeyOfOneNameAreOneMember is the deduplication [reader.Children]
// owes: one member is one address, so the two objects the registry keeps apart are
// still a single entry in the map.
func TestAValueAndASubkeyOfOneNameAreOneMember(t *testing.T) {
	t.Parallel()

	store := newFake().
		put("tags", "A", winreg.Datum{Type: winreg.TypeString, Text: "the value"}).
		put(`tags\a`, "ignored", winreg.Datum{Type: winreg.TypeString, Text: "x"})

	got, err := ferry.Load[tagsMap](t.Context(), source(store))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(got.Tags) != 1 || got.Tags["A"] != "the value" {
		t.Errorf("loaded %v, want one member holding the value", got.Tags)
	}
}

// TestHiveNames pins what a report opens with, since the hive is half of every
// plane name this driver prints.
func TestHiveNames(t *testing.T) {
	t.Parallel()

	want := map[winreg.Hive]string{
		winreg.LocalMachine:  "HKEY_LOCAL_MACHINE",
		winreg.CurrentUser:   "HKEY_CURRENT_USER",
		winreg.ClassesRoot:   "HKEY_CLASSES_ROOT",
		winreg.Users:         "HKEY_USERS",
		winreg.CurrentConfig: "HKEY_CURRENT_CONFIG",
		winreg.Hive(0):       "an unknown hive",
	}

	for hive, name := range want {
		if got := hive.String(); got != name {
			t.Errorf("hive %d prints as %q, want %q", hive, got, name)
		}
	}
}

// TestTypeNames pins the type tags the golden rows are read in, so a rename of
// one is a change to what every golden means rather than a silent edit.
func TestTypeNames(t *testing.T) {
	t.Parallel()

	want := map[winreg.Type]string{
		winreg.TypeString:       "REG_SZ",
		winreg.TypeExpandString: "REG_EXPAND_SZ",
		winreg.TypeDWord:        "REG_DWORD",
		winreg.TypeQWord:        "REG_QWORD",
		winreg.TypeBinary:       "REG_BINARY",
		winreg.TypeMultiString:  "REG_MULTI_SZ",
		winreg.TypeOther:        "a registry type ferry does not carry",
	}

	for kind, name := range want {
		if got := kind.String(); got != name {
			t.Errorf("type %d prints as %q, want %q", kind, got, name)
		}
	}
}

// dumpWith is one value saved through one sink, which is what the rows above are
// written through. The context is the case's own, so a row that stages a
// cancellation can hand in the context it cancels.
func dumpWith[T any](v T) func(context.Context, ferry.Sink) error {
	return func(ctx context.Context, s ferry.Sink) error { return ferry.Dump(ctx, v, s) }
}

// loadWith is one schema loaded through one source, for the same reason.
func loadWith[T any](ctx context.Context, src ferry.Source) error {
	_, err := ferry.Load[T](ctx, src)

	return err
}

// TestACancelledContextIsRefusedAtTheOpen is the one thing every call in this
// driver checks before it touches the registry, asserted where a caller can see
// it: a load and a save under a context that is already done reach the plane not
// at all.
func TestACancelledContextIsRefusedAtTheOpen(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	store := newFake()

	if _, err := ferry.Load[oneText](ctx, source(store)); !errors.Is(err, context.Canceled) {
		t.Errorf("Load answered %v, want context.Canceled", err)
	}

	if err := ferry.Dump(ctx, oneText{Text: "x"}, sink(store)); !errors.Is(err, context.Canceled) {
		t.Errorf("Dump answered %v, want context.Canceled", err)
	}

	if held := string(store.contents()); strings.Contains(held, "val") {
		t.Errorf("a cancelled save wrote something:\n%s", held)
	}
}
