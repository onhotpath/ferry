package env

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/onhotpath/ferry"
)

// Every refusal this sink makes, and where each one lands. The placement is the
// point in each case: a refusal at Bind costs a message, a refusal at the open
// costs a message and leaves the file untouched, and a refusal part way through
// a walk would have already half-written somebody's file - which is why none of
// these is one.

// TestAValueThePlaneCannotHoldIsRefusedAndTheFileIsUntouched is the leaf-level
// refusals, each one a value with no representation here.
func TestAValueThePlaneCannotHoldIsRefusedAndTheFileIsUntouched(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		dump func(t *testing.T, path string) error
	}{
		{
			"a NUL inside a value",
			func(t *testing.T, path string) error {
				t.Helper()

				return ferry.Dump(t.Context(), host{Host: "a\x00b"}, NewDotEnvSink(path))
			},
		},
		{
			"a nil pointer at a leaf",
			func(t *testing.T, path string) error {
				t.Helper()

				type opt struct {
					Host *string `ferry:"host"`
				}

				return ferry.Dump(t.Context(), opt{}, NewDotEnvSink(path))
			},
		},
		{
			"a nil slice, which is a null at a container",
			func(t *testing.T, path string) error {
				t.Helper()

				return ferry.Dump(t.Context(), tags{}, NewDotEnvSink(path))
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			refusesTheValue(t, c.dump)
		})
	}
}

// refusesTheValue is one refusal row, lifted out of its table: the dump fails,
// the file is untouched, and nothing is left staged beside it.
func refusesTheValue(t *testing.T, dump func(t *testing.T, path string) error) {
	t.Helper()

	path := staged(t, "KEEP=me\n")

	err := dump(t, path)
	if err == nil {
		t.Fatal("the dump took this value, want a refusal: a value a plane cannot carry is a loud " +
			"refusal and never a value quietly mangled")
	}

	answers(t, err, ferry.ErrValue)

	if got := read(t, path); got != "KEEP=me\n" {
		t.Errorf("the file holds %q, want it byte for byte as it was: a dump that failed writes nothing", got)
	}

	if left := staging(t, path); len(left) != 0 {
		t.Errorf("the save left %v behind, want the staged file removed", left)
	}
}

// TestASchemaWhoseSweepWouldReachAnotherFieldIsRefusedAtBind is the check that
// matters more on this half than on the other one.
//
// A slice at tags and an int at tags_count fold to TAGS_0, TAGS_1 and
// TAGS_COUNT, which are three different names, so nothing collides. What does
// collide is the space the slice is enumerated out of: TAGS_COUNT is inside it.
// On the read side that is a value read at two addresses; on this side it is a
// save of the slice deleting an operator's variable and reporting success. Core
// refuses it for both halves, and this asserts that the sink gets that refusal
// too, at Bind, with the file untouched.
func TestASchemaWhoseSweepWouldReachAnotherFieldIsRefusedAtBind(t *testing.T) {
	t.Parallel()

	type shadowed struct {
		Tags  []string `ferry:"tags"`
		Count int      `ferry:"tags_count"`
	}

	path := staged(t, "KEEP=me\n")

	_, err := ferry.BindSink[shadowed](NewDotEnvSink(path))
	if err == nil {
		t.Fatal("the sink bound this schema, want a refusal: saving the slice would delete TAGS_COUNT and " +
			"report success")
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("the refusal is %+v, want it to answer errors.Is against ferry.ErrPlane", err)
	}

	if got := read(t, path); got != "KEEP=me\n" {
		t.Errorf("the file holds %q, want it untouched: Bind does no I/O", got)
	}
}

// TestBothHalvesRefuseTheSameSchema is the other half of the case above, and it
// is why this driver adds no refusal of its own: a schema the sink cannot save
// is one the source cannot load either, and one rule answers both.
func TestBothHalvesRefuseTheSameSchema(t *testing.T) {
	t.Parallel()

	type shadowed struct {
		Tags  []string `ferry:"tags"`
		Count int      `ferry:"tags_count"`
	}

	if _, err := ferry.Bind[shadowed](New(Environ(noEnviron))); err == nil {
		t.Error("the source bound this schema while the sink refused it: one plane answering a schema two ways " +
			"is a round trip that cannot exist")
	}
}

// TestAFileThatChangedBetweenTheOpenAndTheCommitIsRefused is the optimistic
// concurrency a merge needs: a save that swapped anyway would report success for
// a save that discarded somebody else's edit.
func TestAFileThatChangedBetweenTheOpenAndTheCommitIsRefused(t *testing.T) {
	t.Parallel()

	path := staged(t, "HOST=old\n")

	set, w := writerOver[host](t, NewDotEnvSink(path))
	defer closeWriter(t, w)

	if err := w.Set(t.Context(), leafOf(t, set, ferry.At("host")), ferry.String("mine")); err != nil {
		t.Fatalf("set: %+v", err)
	}

	// Somebody else's edit, landing between the open and the commit. The length
	// changes, so the stamp sees it whatever the clock's resolution is.
	if err := os.WriteFile(path, []byte("HOST=theirs\nTHEIRS=1\n"), 0o600); err != nil {
		t.Fatalf("the other edit: %v", err)
	}

	if err := committerOf(t, w).Commit(t.Context()); err == nil {
		t.Fatal("the save took the commit, want a refusal: it merged into a file that is no longer there")
	}

	if got := read(t, path); got != "HOST=theirs\nTHEIRS=1\n" {
		t.Errorf("the file holds %q, want the other edit intact", got)
	}
}

// TestADirectoryThatTakesNoFileIsRefusedInsideTheOpen is ADR-0004's placement
// clause: read-only is a runtime fact, so it is neither a Bind refusal nor a
// failure discovered part way through a walk.
func TestADirectoryThatTakesNoFileIsRefusedInsideTheOpen(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("running as root, which writes into a directory with no write bit")
	}

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("taking the write bit off the directory: %v", err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	set, err := addrsOf[host]()
	if err != nil {
		t.Fatalf("compiling the fixture: %+v", err)
	}

	open, err := NewDotEnvSink(filepath.Join(dir, ".env")).Bind(set)
	if err != nil {
		t.Fatalf("bind: %+v", err)
	}

	if _, err = open(t.Context()); err == nil {
		t.Fatal("the open succeeded over a directory that takes no new file, want a refusal")
	}

	if !errors.Is(err, ferry.ErrReadOnly) {
		t.Errorf("the refusal is %+v, want it to answer errors.Is against ferry.ErrReadOnly", err)
	}
}

// writerOver is one open dump, for the two cases that have to hold a writer
// across something else happening to the file.
func writerOver[T any](t *testing.T, s *DotEnvSink) (*ferry.AddressSet, ferry.Writer) {
	t.Helper()

	set, err := addrsOf[T]()
	if err != nil {
		t.Fatalf("compiling the fixture: %+v", err)
	}

	open, err := s.Bind(set)
	if err != nil {
		t.Fatalf("bind: %+v", err)
	}

	w, err := open(t.Context())
	if err != nil {
		t.Fatalf("open: %+v", err)
	}

	return set, w
}

// committerOf and closeWriter are the two lifecycle halves of this writer,
// asserted where they are reached rather than assumed: a writer that stopped
// implementing either is a driver that no longer stages, and the case would
// otherwise pass by testing something else.
func committerOf(t *testing.T, w ferry.Writer) ferry.Committer {
	t.Helper()

	c, ok := w.(ferry.Committer)
	if !ok {
		t.Fatal("this writer commits nothing, and the whole of a save is staged until it does")
	}

	return c
}

func closeWriter(t *testing.T, w ferry.Writer) {
	t.Helper()

	r, ok := w.(ferry.Releaser)
	if !ok {
		t.Fatal("this writer releases nothing, and it holds a staged file")
	}

	if err := r.Close(); err != nil {
		t.Errorf("close: %+v", err)
	}
}

// leafOf is one leaf address out of a compiled set, which is the only door a
// sealed address comes through.
func leafOf(t *testing.T, set *ferry.AddressSet, at ferry.Path) ferry.LeafAddr {
	t.Helper()

	a, ok := leafIn(set, at)
	if !ok {
		t.Fatalf("the fixture names no leaf at %s", at)
	}

	return a
}

// TestBoolWordsAreWrittenAsWellAsRead is the reason that option is a [Naming]
// rather than a source setting: a sink that wrote true where the source reads on
// would be a plane that cannot load what it just saved.
func TestBoolWordsAreWrittenAsWellAsRead(t *testing.T) {
	t.Parallel()

	type feature struct {
		Enabled bool `ferry:"enabled"`
	}

	words := BoolWords("on", "off")
	path := filepath.Join(t.TempDir(), ".env")

	if err := ferry.Dump(t.Context(), feature{Enabled: true}, NewDotEnvSink(path, words)); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if got := read(t, path); got != "ENABLED=on\n" {
		t.Errorf("the file holds %q, want this plane's own word for true", got)
	}

	got, err := ferry.Load[feature](t.Context(), New(Environ(noEnviron), DotEnv(path), words))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if !got.Enabled {
		t.Error("the value did not survive the trip, and the words are what makes it a boolean on this plane")
	}
}

// TestABoolIsWrittenAsGoSpellsItWhereNoWordsWereDeclared is the other half: the
// option changes the spelling and its absence is not a gap.
func TestABoolIsWrittenAsGoSpellsItWhereNoWordsWereDeclared(t *testing.T) {
	t.Parallel()

	type feature struct {
		Enabled bool `ferry:"enabled"`
	}

	path := filepath.Join(t.TempDir(), ".env")

	if err := ferry.Dump(t.Context(), feature{}, NewDotEnvSink(path)); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if got := read(t, path); got != "ENABLED=false\n" {
		t.Errorf("the file holds %q, want what a bool's own parser reads", got)
	}
}

// TestAContainerPresenceThisPlaneHasNoAnswerForIsRefused is the default arm of
// Ensure, which exists so that a method returning nil everywhere else is one
// something would catch changing.
//
// An absent container gets no call at all in an ordinary dump, so reaching it
// means core asked something this plane has no answer for.
func TestAContainerPresenceThisPlaneHasNoAnswerForIsRefused(t *testing.T) {
	t.Parallel()

	type nested struct {
		Host string `ferry:"host"`
	}

	type outer struct {
		DB nested `ferry:"db"`
	}

	path := filepath.Join(t.TempDir(), ".env")

	set, w := writerOver[outer](t, NewDotEnvSink(path))
	defer closeWriter(t, w)

	at, ok := sectionIn(set, ferry.At("db"))
	if !ok {
		t.Fatal("the fixture names no section at /db")
	}

	ensurer, ok := w.(ferry.Ensurer)
	if !ok {
		t.Fatal("this writer ensures nothing, and a container's presence is what it answers")
	}

	if err := ensurer.Ensure(t.Context(), at, ferry.PresenceAbsent); err == nil {
		t.Error("the writer answered for an absent container, want a refusal: there is nothing to write")
	}
}

// TestADurableSaveIsStillJustASave is the option's contract: it changes when the
// bytes reach the disk and nothing about what they are.
func TestADurableSaveIsStillJustASave(t *testing.T) {
	t.Parallel()

	path := staged(t, "HOST=old\n")

	if err := ferry.Dump(t.Context(), host{Host: "new"}, NewDotEnvSink(path, Durable())); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if got := read(t, path); got != "HOST=new\n" {
		t.Errorf("the file holds %q, want the same bytes an ordinary save leaves", got)
	}
}

// staging is every file this driver staged beside the plane and did not clean up,
// which is empty after every save whether it succeeded or failed.
func staging(t *testing.T, path string) []string {
	t.Helper()

	left, err := filepath.Glob(path + ".ferry-*")
	if err != nil {
		t.Fatalf("looking for staged files: %v", err)
	}

	return left
}
