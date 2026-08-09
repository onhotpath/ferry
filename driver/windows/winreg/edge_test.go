package winreg_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/windows/winreg"
)

// The schemas the edge tests are written against.
type (
	// textThenSection and textThenMap put a leaf before a container, so that the
	// first read is a value and the second is the container question. That is
	// what lets a cancellation staged on the first call be found by the second.
	textThenSection struct {
		Text string     `ferry:"text"`
		Opt  *omittable `ferry:"opt"`
	}

	textThenMap struct {
		Text string            `ferry:"text"`
		Tags map[string]string `ferry:"tags"`
	}

	// twoLeaves is the smallest schema with a second read in it.
	twoLeaves struct {
		One string `ferry:"one"`
		Two string `ferry:"two"`
	}

	// nothingWritten writes nothing at all, so a save over it reaches the commit
	// with no other call in between.
	nothingWritten struct {
		X string `ferry:"x,omitzero"`
	}

	// sections is a map whose members are containers rather than values, which is
	// what makes the sweep have to descend and what reaches Ensure at an address
	// the value minted.
	sections struct {
		M map[string]*omittable `ferry:"m"`
	}

	// mapOfMaps is a map whose members are composites, which is what reaches
	// Unset and Children at an address the value minted.
	mapOfMaps struct {
		M map[string]map[string]string `ferry:"m"`
	}
)

// TestPlaneNameHasNoNameForAnAddressTheRegistryCannotName is the escape hatch
// [ferry.PlaneNamer] is documented to have.
//
// A report is composed after the failure it is about and has nowhere left to put
// a second one, so an address this plane cannot name is a false and never an
// error, and ferry's own rendering of the address stands.
func TestPlaneNameHasNoNameForAnAddressTheRegistryCannotName(t *testing.T) {
	t.Parallel()

	namer := namerOf(t, source(newFake()), setOf[oneText](t))

	if got, ok := namer.PlaneName(ferry.At("host")); !ok || got != `HKEY_CURRENT_USER\Software\Example\host` {
		t.Errorf("PlaneName answered %q %v, want the full registry path", got, ok)
	}

	for _, at := range []ferry.Path{ferry.At(""), ferry.At(`a\b`)} {
		if got, ok := namer.PlaneName(at); ok {
			t.Errorf("PlaneName named %s as %q, want no name at all", at, got)
		}
	}
}

// setOf is every address T names, typed, straight from the compiler: the three
// address kinds are sealed and nothing else mints one.
func setOf[T any](t *testing.T) *ferry.AddressSet {
	t.Helper()

	c := &captureSource{}
	if _, err := ferry.Bind[T](c); err != nil {
		t.Fatalf("compiling the fixture: %v", err)
	}

	return c.set
}

type captureSource struct{ set *ferry.AddressSet }

func (c *captureSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	c.set = addrs

	return func(context.Context) (ferry.Reader, error) { return captureReader{}, nil }, nil
}

type captureReader struct{}

func (captureReader) Get(context.Context, ferry.LeafAddr) (ferry.Value, error) {
	return ferry.Value{}, nil
}

// TestALoadCancelledInFlightStops is the read half of cancellation, asked at each
// of the three questions a load puts to this plane.
//
// The registry cancels the context once it has answered, which is what a
// cancellation arriving in the middle of a walk looks like: the call in hand
// completes and the next one does not start.
func TestALoadCancelledInFlightStops(t *testing.T) {
	t.Parallel()

	cases := map[string]func(context.Context, ferry.Source) error{
		"before the second value": loadWith[twoLeaves],
		"before a container":      loadWith[textThenSection],
		"before an enumeration":   loadWith[textThenMap],
	}

	for name, load := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			store := newFake().cancelAfterEachCall(cancel)
			store.put("", "text", winreg.Datum{Type: winreg.TypeString, Text: "x"})
			store.put("", "one", winreg.Datum{Type: winreg.TypeString, Text: "x"})

			if err := load(ctx, source(store)); !errors.Is(err, context.Canceled) {
				t.Fatalf("Load answered %v, want context.Canceled", err)
			}
		})
	}
}

// TestASaveCancelledInFlightStops is the write half, asked at each of the four
// things a save asks of this plane.
//
// The commit row is the one with no other call in it: a value whose every field
// is omitted writes nothing, so the open is the last thing the registry saw and
// the commit is where the cancellation is found.
func TestASaveCancelledInFlightStops(t *testing.T) {
	t.Parallel()

	cases := map[string]func(context.Context, ferry.Sink) error{
		"before a value":       dumpWith(oneText{Text: "x"}),
		"before a subkey":      dumpWith(withOptional{Opt: &omittable{}}),
		"before a replacement": dumpWith(tagsMap{Tags: map[string]string{"a": "1"}}),
		"before the commit":    dumpWith(nothingWritten{}),
	}

	for name, dump := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkSaveCancelled(t, dump)
		})
	}
}

// checkSaveCancelled runs one save against a registry that cancels the context
// once it has answered, and holds the save to stopping with nothing written.
func checkSaveCancelled(t *testing.T, dump func(context.Context, ferry.Sink) error) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	store := newFake().cancelAfterEachCall(cancel)

	if err := dump(ctx, sink(store)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Dump answered %v, want context.Canceled", err)
	}

	if held := string(store.contents()); strings.Contains(held, "val") {
		t.Errorf("a cancelled save wrote something:\n%s", held)
	}
}

// TestAnAddressTheRegistryCannotNameIsRefusedWhereItIsMinted is the runtime tier
// of the legality check, asked at each of the three places an address a value
// minted reaches this plane.
//
// A registry value name may legally contain a backslash, so a key holding one is
// something a real registry hands back. This driver refuses it rather than
// splitting it into a subkey step, because a backslash in a name is exactly what
// would stop the plane key encoding a subkey and a value name.
func TestAnAddressTheRegistryCannotNameIsRefusedWhereItIsMinted(t *testing.T) {
	t.Parallel()

	cases := map[string]func(context.Context, ferry.Source) error{
		"reading a member":     loadWith[tagsMap],
		"probing a member":     loadWith[sections],
		"enumerating a member": loadWith[mapOfMaps],
	}

	for name, load := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := newFake().put("m", `a\b`, winreg.Datum{Type: winreg.TypeString, Text: "x"})
			store.put("tags", `a\b`, winreg.Datum{Type: winreg.TypeString, Text: "x"})

			if err := load(t.Context(), source(store)); !errors.Is(err, winreg.ErrIllegalName) {
				t.Fatalf("Load answered %v, want an error reaching winreg.ErrIllegalName", err)
			}
		})
	}
}

// TestAMemberTheRegistryListedAndDoesNotHoldIsAbsent is the race a real registry
// has and a snapshot does not: another process removes a value between the
// listing and the read.
//
// The honest answer is absence rather than a refusal. Nothing is under the name
// either, so this is not the registry holding a group of values where the field
// takes one - it is a member that is simply no longer there.
func TestAMemberTheRegistryListedAndDoesNotHoldIsAbsent(t *testing.T) {
	t.Parallel()

	store := newFake().listPhantom("gone")
	store.put("tags", "kept", winreg.Datum{Type: winreg.TypeString, Text: "here"})

	got, err := ferry.Load[tagsMap](t.Context(), source(store))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.Tags["kept"] != "here" {
		t.Errorf("loaded %v, want the member the registry does hold", got.Tags)
	}

	if held, ok := got.Tags["gone"]; ok && held != "" {
		t.Errorf("the member the registry no longer holds came back as %q", held)
	}
}

// TestAFailureUnderAMintedMemberReachesTheCaller is conformance case 4 at the one
// read this driver makes that no other driver has to: the question of whether a
// member with no value is a subkey.
func TestAFailureUnderAMintedMemberReachesTheCaller(t *testing.T) {
	t.Parallel()

	store := newFake().listPhantom("http").failUnder(`tags\http`)
	store.put("tags", "kept", winreg.Datum{Type: winreg.TypeString, Text: "here"})

	if _, err := ferry.Load[tagsMap](t.Context(), source(store)); !errors.Is(err, errFake) {
		t.Fatalf("Load answered %v, want the registry's own error", err)
	}
}

// TestASweepThatCannotRemoveReportsIt is the two removal failures a save can hit,
// and they are the only ones a listing that already succeeded can produce.
func TestASweepThatCannotRemoveReportsIt(t *testing.T) {
	t.Parallel()

	t.Run("a value that could not be removed", func(t *testing.T) {
		t.Parallel()

		store := newFake()
		mustDump(t, store, tagsMap{Tags: map[string]string{"a": "1", "b": "2"}})
		store.failRemoval("b")

		err := ferry.Dump(t.Context(), tagsMap{Tags: map[string]string{"a": "1"}}, sink(store))
		if !errors.Is(err, errFake) {
			t.Fatalf("Dump answered %v, want the registry's own error", err)
		}
	})

	t.Run("a subkey that could not be removed", func(t *testing.T) {
		t.Parallel()

		store := newFake()
		mustDump(t, store, sections{M: map[string]*omittable{"a": {}, "b": {}}})
		store.failUnder(`m\b`)

		err := ferry.Dump(t.Context(), sections{M: map[string]*omittable{"a": {}}}, sink(store))
		if !errors.Is(err, errFake) {
			t.Fatalf("Dump answered %v, want the registry's own error", err)
		}
	})
}

// TestASweepKeepsASubkeyThisSaveSpelled is the sweep's own exception: a member
// this dump asked to exist is not one the previous dump left behind, so it
// survives the replacement it sits inside.
func TestASweepKeepsASubkeyThisSaveSpelled(t *testing.T) {
	t.Parallel()

	store := newFake()
	mustDump(t, store, sections{M: map[string]*omittable{"a": {}, "b": {}}})
	mustDump(t, store, sections{M: map[string]*omittable{"a": {}}})

	held := string(store.contents())
	if !strings.Contains(held, `key "m\\a"`) {
		t.Errorf("the sweep removed a member this save wrote:\n%s", held)
	}

	if strings.Contains(held, `key "m\\b"`) {
		t.Errorf("the sweep kept a member this save replaced:\n%s", held)
	}
}

// TestACommitNeverWritesIntoAKeyItRemoved is the commit's own ordering, held to
// rather than assumed.
//
// A save removes what it replaces before it writes what it staged, and a
// replaced composite whose members are written again is the ordinary case that
// puts the two next to each other. The sweep is supposed to leave every subkey
// this save puts something in alone, and this is where the two orders are
// checked against each other.
//
// It is asserted here rather than only on Windows because Windows reports the
// mistake by accident. A removed key some other process still holds a handle on
// stays in place marked for deletion, so the write that followed the removal
// fails with ERROR_KEY_DELETED - and only while that other reader is there. A
// store that models the registry as a map answers the same write happily, which
// is why the order rather than the outcome is what this reads.
func TestACommitNeverWritesIntoAKeyItRemoved(t *testing.T) {
	t.Parallel()

	t.Run("a member written again beside one removed", func(t *testing.T) {
		t.Parallel()

		store := newFake()
		mustDump(t, store, mapOfMaps{M: map[string]map[string]string{"a": {"x": "1"}, "b": {"y": "2"}}})
		mustDump(t, store, mapOfMaps{M: map[string]map[string]string{"a": {"x": "2"}}})
		noWriteAfterRemoval(t, store)
	})

	t.Run("a member spelled again beside one removed", func(t *testing.T) {
		t.Parallel()

		store := newFake()
		mustDump(t, store, sections{M: map[string]*omittable{"a": {}, "b": {}}})
		mustDump(t, store, sections{M: map[string]*omittable{"a": {}}})
		noWriteAfterRemoval(t, store)
	})
}

// noWriteAfterRemoval is [TestACommitNeverWritesIntoAKeyItRemoved]'s one
// assertion, over whatever saves the case in front of it made.
func noWriteAfterRemoval(t *testing.T, store *fake) {
	t.Helper()

	if at, ok := store.removedThenWritten(); ok {
		t.Errorf("this save wrote into %q after removing it: a registry another process holds a handle on "+
			"keeps that key alive marked for deletion, and answers the write with ERROR_KEY_DELETED", at)
	}
}

// TestAMemberNameThatIsNotAPositionStaysAName is the recovery of a segment kind
// out of text the registry carries none of.
//
// Only canonical base-10 is a position, which is the only spelling ferry renders
// one in. A leading zero and a number too large for the type are both member
// names, because answering with a wrapped-around index would be the one thing
// worse than refusing it.
func TestAMemberNameThatIsNotAPositionStaysAName(t *testing.T) {
	t.Parallel()

	huge := "99999999999999999999999999"

	store := newFake()
	store.put("tags", "01", winreg.Datum{Type: winreg.TypeString, Text: "a"})
	store.put("tags", huge, winreg.Datum{Type: winreg.TypeString, Text: "b"})

	got, err := ferry.Load[tagsMap](t.Context(), source(store))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.Tags["01"] != "a" || got.Tags[huge] != "b" {
		t.Errorf("loaded %v, want both names back as they were written", got.Tags)
	}
}

// TestAPositionUnderAMappingIsRefused is the one residue of reading a segment
// kind off text, and it is core's refusal rather than this driver's.
//
// The members are ordered by kind before core sees them, which is what the mixed
// list here exercises: a position sorts before a name whatever the two spell.
func TestAPositionUnderAMappingIsRefused(t *testing.T) {
	t.Parallel()

	store := newFake()
	store.put("tags", "0", winreg.Datum{Type: winreg.TypeString, Text: "a"})
	store.put("tags", "name", winreg.Datum{Type: winreg.TypeString, Text: "b"})

	if _, err := ferry.Load[tagsMap](t.Context(), source(store)); err == nil {
		t.Fatal("Load read a position as a mapping key")
	}
}

// TestTheKeysOwnUnnamedValueIsNotAMember is what a container's own value slot is:
// something ferry has no address for, so it is not enumerated as a member of the
// container it belongs to.
func TestTheKeysOwnUnnamedValueIsNotAMember(t *testing.T) {
	t.Parallel()

	store := newFake()
	store.put("tags", "", winreg.Datum{Type: winreg.TypeString, Text: "the container's own"})
	store.put("tags", "a", winreg.Datum{Type: winreg.TypeString, Text: "a member"})

	got, err := ferry.Load[tagsMap](t.Context(), source(store))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(got.Tags) != 1 || got.Tags["a"] != "a member" {
		t.Errorf("loaded %v, want the member alone", got.Tags)
	}
}

// TestASinkOptionRefusalLandsAtBind is the write half of where a constructor that
// returns no error puts one.
func TestASinkOptionRefusalLandsAtBind(t *testing.T) {
	t.Parallel()

	_, err := ferry.BindSink[oneText](winreg.NewSink(0, base, winreg.Store(newFake())))
	if !errors.Is(err, winreg.ErrOption) {
		t.Fatalf("BindSink answered %v, want an error reaching winreg.ErrOption", err)
	}
}

// TestAWatchThatEndsQuietlyStopsWithoutSpeaking is the third ending a watch has:
// a registry that has stopped reporting without anything having gone wrong.
//
// Nothing is called back, because nothing changed. Only losing the watch speaks.
func TestAWatchThatEndsQuietlyStopsWithoutSpeaking(t *testing.T) {
	t.Parallel()

	fired := make(chan struct{}, 1)

	_ = source(newFake().endWatch(), winreg.Watch(t.Context(), func(context.Context) { fired <- struct{}{} }))

	select {
	case <-fired:
		t.Error("a watch that ended quietly called back")
	case <-time.After(50 * time.Millisecond):
	}
}

// TestAWatchThatCannotBeReArmedSpeaksOnce is the fourth ending, and the one the
// two-call registration adds: the wait answered with a change and the next
// registration could not be placed.
//
// It is a lost watch like any other, so it says so once and stops. The callback
// that would have followed the change is the one call, because there is nothing
// left to hear the next one with.
func TestAWatchThatCannotBeReArmedSpeaksOnce(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	store := newFake()
	fired := make(chan struct{}, 4)

	_ = source(store, winreg.Watch(ctx, func(context.Context) { fired <- struct{}{} }))

	store.failArm()

	if err := store.Create(ctx, "poke"); err != nil {
		t.Fatalf("poking the store: %v", err)
	}

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("a watch that could not be re-armed said nothing")
	}

	select {
	case <-fired:
		t.Error("a lost watch called back more than once")
	case <-time.After(50 * time.Millisecond):
	}
}

// mustDump is a save this test takes as given rather than as the thing under
// test, so that the case below it reads as one assertion.
func mustDump[T any](t *testing.T, store winreg.Registry, v T) {
	t.Helper()

	if err := ferry.Dump(t.Context(), v, sink(store)); err != nil {
		t.Fatalf("the setup Dump: %v", err)
	}
}

// TestAMintedContainerTheRegistryCannotNameIsRefused is the write-side twin of
// the minted-leaf check, at the two questions a container is asked.
//
// A map key is the caller's data, so a container whose key holds a backslash is
// in no address set and reaches this plane during the save. It is refused where
// it is minted, before anything under it has been written.
func TestAMintedContainerTheRegistryCannotNameIsRefused(t *testing.T) {
	t.Parallel()

	cases := map[string]func(context.Context, ferry.Sink) error{
		"spelling a container":  dumpWith(sections{M: map[string]*omittable{`a\b`: {}}}),
		"replacing a container": dumpWith(mapOfMaps{M: map[string]map[string]string{`a\b`: {"x": "y"}}}),
	}

	for name, dump := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkMintedContainerRefused(t, dump)
		})
	}
}

// checkMintedContainerRefused runs one save whose map key the registry cannot
// name, and holds the refusal to landing before anything was written.
func checkMintedContainerRefused(t *testing.T, dump func(context.Context, ferry.Sink) error) {
	t.Helper()

	store := newFake()

	if err := dump(t.Context(), sink(store)); !errors.Is(err, winreg.ErrIllegalName) {
		t.Fatalf("Dump answered %v, want an error reaching winreg.ErrIllegalName", err)
	}

	if held := string(store.contents()); strings.Contains(held, "val") {
		t.Errorf("the refused save wrote a value:\n%s", held)
	}
}

// TestAPlaneNameIsBuiltFromBothEndsOfThePath pins the two shapes the registry
// path has no step in.
//
// A driver built over a hive itself has no subkey to place its addresses under,
// and the root address has no name of its own: it is the driver's own key, and
// the unnamed value inside it. Neither may produce a bare or a doubled backslash,
// because the name is what a report opens with and what somebody pastes into
// regedit.
func TestAPlaneNameIsBuiltFromBothEndsOfThePath(t *testing.T) {
	t.Parallel()

	atHive := winreg.NewSource(winreg.CurrentUser, "", winreg.Store(newFake()))

	if got, ok := namerOf(t, atHive, setOf[oneText](t)).PlaneName(ferry.At("text")); !ok ||
		got != `HKEY_CURRENT_USER\text` {
		t.Errorf("PlaneName answered %q %v, want the hive and the value with one backslash between them", got, ok)
	}

	if got, ok := namerOf(t, source(newFake()), setOf[int](t)).PlaneName(ferry.Path{}); !ok ||
		got != `HKEY_CURRENT_USER\Software\Example` {
		t.Errorf("PlaneName answered %q %v, want the driver's own key with nothing after it", got, ok)
	}
}

// namerOf opens one source over one compiled set and hands back its plane namer.
func namerOf(t *testing.T, src ferry.Source, set *ferry.AddressSet) ferry.PlaneNamer {
	t.Helper()

	namer, ok := openReader(t, src, set).(ferry.PlaneNamer)
	if !ok {
		t.Fatal("the reader is no ferry.PlaneNamer")
	}

	return namer
}
