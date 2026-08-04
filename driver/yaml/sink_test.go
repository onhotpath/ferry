package yaml_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/yaml"
)

// TestStagingReplacesThePlane is one half of the staging promise: a walk that
// succeeded leaves the plane replaced and no temporary behind.
//
// The directory is inspected and not just the file, because a driver that
// renames nothing and a driver that leaves its scratch file next to the plane
// both write a correct plane.
func TestStagingReplacesThePlane(t *testing.T) {
	type config struct {
		Port int `ferry:"port"`
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "plane.yaml")

	if err := os.WriteFile(path, []byte("port: 1\n"), 0o600); err != nil {
		t.Fatalf("writing the plane: %v", err)
	}

	if err := ferry.Dump(t.Context(), config{Port: 8080}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "port: 8080\n"; got != want {
		t.Errorf("the plane holds %q, want %q", got, want)
	}

	onlyFile(t, dir, "plane.yaml")
}

// TestStagingLeavesThePlaneAloneOnFailure is the other half, and it is the half
// that matters: a walk that failed leaves the plane byte-identical and no
// temporary behind.
//
// The failure is staged above the driver, because closed-without-Commit is the
// only thing the driver is told and this is what it has to be enough for.
func TestStagingLeavesThePlaneAloneOnFailure(t *testing.T) {
	type config struct {
		Port  int    `ferry:"port"`
		Label string `ferry:"label"`
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "plane.yaml")
	before := "# hand written\nport: 1\n"

	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatalf("writing the plane: %v", err)
	}

	sink := &shell{inner: yaml.NewSink(path), stop: ferry.At("label")}

	err := ferry.Dump(t.Context(), config{Port: 8080, Label: "x"}, sink)
	if err == nil {
		t.Fatal("a walk that failed was reported as a dump that succeeded")
	}

	if !errors.Is(err, errStop) {
		t.Errorf("the dump reported %v, which does not carry the failure the walk was stopped with", err)
	}

	if got := read(t, path); got != before {
		t.Errorf("the plane holds %q after a failed walk, want the %q it held before: a staged write that did "+
			"not commit must leave the plane byte-identical", got, before)
	}

	onlyFile(t, dir, "plane.yaml")
}

// TestUnwritableDirectoryRefusesInsideTheOpen holds the read-only refusal to
// where ADR-0004 puts it: inside the open, after zero writes.
//
// Not at Bind, which does no I/O and so cannot know, and not at the first Set,
// which has already half-written the plane.
func TestUnwritableDirectoryRefusesInsideTheOpen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into a directory with no write bit, so there is no refusal to observe")
	}

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("making the directory unwritable: %v", err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	open, err := yaml.NewSink(filepath.Join(dir, "plane.yaml")).Bind(ferry.NewAddressSet(ferry.At("port")))
	if err != nil {
		t.Fatalf("Bind refused before any I/O, where it cannot yet know the plane is unwritable: %v", err)
	}

	w, err := open(t.Context())
	if err == nil {
		t.Fatalf("opening a writer over an unwritable directory succeeded, and answered with %v", w)
	}

	if !errors.Is(err, ferry.ErrReadOnly) {
		t.Errorf("the open failed with %v, want an error carrying ferry.ErrReadOnly", err)
	}
}

// TestUnwritableDirectoryThroughDump is the same refusal where a caller meets
// it, and it asserts the two things only core can supply: the class, since
// ErrReadOnly is subordinate to ErrPlane rather than beside it, and that the
// dump reports rather than half-writing.
func TestUnwritableDirectoryThroughDump(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into a directory with no write bit, so there is no refusal to observe")
	}

	type config struct {
		Port int `ferry:"port"`
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "plane.yaml")
	before := "port: 1\n"

	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatalf("writing the plane: %v", err)
	}

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("making the directory unwritable: %v", err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	err := ferry.Dump(t.Context(), config{Port: 8080}, yaml.NewSink(path))
	if err == nil {
		t.Fatal("dumping into an unwritable directory succeeded")
	}

	if !errors.Is(err, ferry.ErrReadOnly) || !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("the dump reported %v, want an error carrying both ferry.ErrReadOnly and ferry.ErrPlane", err)
	}

	if got := read(t, path); got != before {
		t.Errorf("the plane holds %q, want the %q it held before a refused dump", got, before)
	}
}

// TestFidelity is the guarantee this driver owns and core does not.
//
// Comments, key ordering, indentation and every key ferry does not map survive
// a load and a dump. A marshal and unmarshal through map[string]any destroys
// all four, which is why this driver mutates the parsed node tree instead.
func TestFidelity(t *testing.T) {
	type db struct {
		Host string `ferry:"host"`
		Port int    `ferry:"port"`
	}

	type config struct {
		DB db `ferry:"db"`
	}

	const original = `# what this file is for
db:
  # where the database lives
  host: localhost # and the port is below
  port: 5432
  pool: 8
zeta: kept
alpha:
  - one
  - two
`

	path := write(t, original)

	cfg, err := ferry.Load[config](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if err := ferry.Dump(t.Context(), cfg, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got := read(t, path); got != original {
		t.Errorf("a load and a dump rewrote the plane.\n got:\n%s\nwant:\n%s", got, original)
	}
}

// TestAnchorSurvivesASave is a regression test for a save that destroyed the
// file it was saving (#196).
//
// Replacing a scalar dropped the anchor the operator had written on it, while
// every alias to it still emitted its `*name`. So the dump reported success and
// left a document that no reader could parse, including the load right after it.
// The assertion is the whole round trip, because "the dump returned nil" is
// exactly what the defect also did.
func TestAnchorSurvivesASave(t *testing.T) {
	type config struct {
		Host string `ferry:"host"`
	}

	path := write(t, "host: &h localhost\nother: *h\n")

	if err := ferry.Dump(t.Context(), config{Host: "example"}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if _, err := ferry.Load[config](t.Context(), yaml.NewSource(path)); err != nil {
		t.Fatalf("loading back what the dump wrote: %v\nthe plane holds:\n%s", err, read(t, path))
	}
}

// TestAnchorSurvivesAContainerBeingReshaped is the same defect one level up: the
// address written is under the anchored node rather than at it, so what drops
// the anchor is the reshape into a container rather than the scalar write.
//
// It reaches the file the same way, and a document whose `db` was a scalar and
// is now a mapping is exactly the shape the reshape exists to bring up to date.
func TestAnchorSurvivesAContainerBeingReshaped(t *testing.T) {
	type db struct {
		Port int `ferry:"port"`
	}

	type config struct {
		DB db `ferry:"db"`
	}

	path := write(t, "db: &d hello\nreplica: *d\n")

	if err := ferry.Dump(t.Context(), config{DB: db{Port: 5432}}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "db: &d\n  port: 5432\nreplica: *d\n"; got != want {
		t.Errorf("the plane holds %q, want %q", got, want)
	}

	if _, err := ferry.Load[config](t.Context(), yaml.NewSource(path)); err != nil {
		t.Fatalf("loading back what the dump wrote: %v\nthe plane holds:\n%s", err, read(t, path))
	}
}

// TestAnAliasFollowsTheValueItNames pins the consequence of keeping the anchor,
// which is the one place a key no field maps does not read back as it did.
//
// An operator who writes `other: *h` is saying other is whatever host is, so a
// dump at host changes what other holds. The line's text is byte-identical
// before and after; its value is not. This is a decision and not an accident,
// and a change to it changes this test.
func TestAnAliasFollowsTheValueItNames(t *testing.T) {
	type mapped struct {
		Host string `ferry:"host"`
	}

	type both struct {
		Host  string `ferry:"host"`
		Other string `ferry:"other"`
	}

	path := write(t, "host: &h localhost\nother: *h\n")

	before, err := ferry.Load[both](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if before.Other != "localhost" {
		t.Fatalf("other read back as %q before the dump, want %q", before.Other, "localhost")
	}

	if err := ferry.Dump(t.Context(), mapped{Host: "example"}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "host: &h example\nother: *h\n"; got != want {
		t.Errorf("the plane holds %q, want %q", got, want)
	}

	after, err := ferry.Load[both](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("loading back what the dump wrote: %v", err)
	}

	if after.Other != "example" {
		t.Errorf("other read back as %q after a dump that wrote host, want %q: an alias follows the value its "+
			"anchor names, so a dump at a mapped key changes what an unmapped alias to it holds", after.Other,
			"example")
	}
}

// TestTypedBoundary is the five values the typed boundary is for: dumped and
// loaded back, five of five return exactly.
//
// A stringified boundary returns one of five: null becomes "", true becomes
// "true", 8080 becomes "8080" and 3.5 becomes "3.5", and the quoted 8080 and
// the unquoted one become the same text. The spelling is asserted alongside the
// values, because that is where the distinction lives on the plane.
func TestTypedBoundary(t *testing.T) {
	type config struct {
		Nul   *string `ferry:"nul"`
		Flag  bool    `ferry:"flag"`
		Port  int     `ferry:"port"`
		Ratio float64 `ferry:"ratio"`
		Label string  `ferry:"label"`
	}

	path := filepath.Join(t.TempDir(), "plane.yaml")
	want := config{Flag: true, Port: 8080, Ratio: 3.5, Label: "8080"}

	if err := ferry.Dump(t.Context(), want, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	const spelled = "nul: null\nflag: true\nport: 8080\nratio: 3.5\nlabel: \"8080\"\n"
	if got := read(t, path); got != spelled {
		t.Errorf("the plane holds %q, want %q", got, spelled)
	}

	got, err := ferry.Load[config](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("loaded %+v, want %+v", got, want)
	}
}

// TestNestedRoundTrip asserts that nested lists and maps survive, including the
// element a flat delimiter-joined form destroys.
//
// "b,c" is that element: a driver that joins a list with commas writes a,b,c,
// and reads back three elements where there were three characters in one.
func TestNestedRoundTrip(t *testing.T) {
	type server struct {
		Host string   `ferry:"host"`
		Tags []string `ferry:"tags"`
	}

	type config struct {
		Servers []server          `ferry:"servers"`
		Labels  map[string]string `ferry:"labels"`
		Odd     []string          `ferry:"odd"`
	}

	path := filepath.Join(t.TempDir(), "plane.yaml")
	want := config{
		Servers: []server{{Host: "a", Tags: []string{"x"}}, {Host: "b"}},
		Labels:  map[string]string{"one": "1", "two": "2"},
		Odd:     []string{"a", "b,c", ""},
	}

	if err := ferry.Dump(t.Context(), want, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	got, err := ferry.Load[config](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("loaded %+v, want %+v\nthe plane holds:\n%s", got, want, read(t, path))
	}
}

// TestAbsentIsNeverWritten is a regression test for a defect a prototype
// shipped: its sink mapped Absent onto !!null, so an address ferry omitted was
// written as an explicit null and read back as Null.
//
// That is the Absent-versus-Null conflation ferry criticises xload for,
// committed on the write path, where no later load can tell it from a null the
// operator wrote. Both halves are asserted: the walk makes no Set call for the
// address, and no null reaches the file.
func TestAbsentIsNeverWritten(t *testing.T) {
	type config struct {
		Kept    string `ferry:"kept"`
		Skipped string `ferry:"-"`
	}

	path := filepath.Join(t.TempDir(), "plane.yaml")
	sink := &shell{inner: yaml.NewSink(path)}

	if err := ferry.Dump(t.Context(), config{Kept: "here", Skipped: "gone"}, sink); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if len(sink.seen) != 1 || sink.seen[0].addr != ferry.At("kept") {
		t.Errorf("the walk made the Set calls %v, want one at /kept: an address ferry omits gets no Set call "+
			"rather than a Set of nothing", sink.seen)
	}

	for _, s := range sink.seen {
		if s.val.Kind() == ferry.KindAbsent {
			t.Errorf("the walk handed the sink an Absent at %s, which is a Reader-side kind", s.addr)
		}
	}

	if got := read(t, path); strings.Contains(got, "null") {
		t.Errorf("the plane holds %q, which carries a null for an address that was never written", got)
	}
}

// TestSinkRefusesAnAbsent is the same defect from below: handed an Absent
// directly, the writer refuses it rather than writing a null.
//
// It is the half a walk cannot reach, because core never calls Set with one -
// which is exactly why the driver's own refusal has to be stated somewhere.
func TestSinkRefusesAnAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plane.yaml")
	w := openWriter(t, path)

	if err := w.Set(t.Context(), ferry.At("kept"), ferry.String("here")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	err := w.Set(t.Context(), ferry.At("gone"), ferry.Value{})
	if err == nil {
		t.Fatal("a Set of an Absent was taken, and an absent address written as a value is the conflation this " +
			"driver refuses")
	}

	if !errors.Is(err, ferry.ErrValue) {
		t.Errorf("the refusal was %v, want an error carrying ferry.ErrValue", err)
	}

	commit(t, w)

	if got, want := read(t, path), "kept: here\n"; got != want {
		t.Errorf("the plane holds %q, want %q: the refused address left nothing behind", got, want)
	}
}

// TestNonUTF8StringIsRefusedPerAddress is this driver's one known limitation,
// asserted from where a caller meets it (ADR-0005, #157).
//
// A Go string is a byte sequence and a YAML string is a Unicode one, so this
// plane carries KindString and cannot spell every value of it. What it does
// instead is refuse the one address, loudly and with the value's own class, and
// leave the plane exactly as it was: the alternative that was rejected is a tag
// of ferry's own invention written into an operator's file.
//
// The address is what makes it actionable, and it is the field's rather than the
// document's: a config with twenty strings names the one that cannot travel.
func TestNonUTF8StringIsRefusedPerAddress(t *testing.T) {
	type config struct {
		Kept string `ferry:"kept"`
		Raw  string `ferry:"raw"`
	}

	path := write(t, "kept: here\n")

	err := ferry.Dump(t.Context(), config{Kept: "here", Raw: "\xff\xfe"}, yaml.NewSink(path))
	if err == nil {
		t.Fatal("a string that is not valid UTF-8 was dumped, and this plane has no spelling for one")
	}

	if !errors.Is(err, ferry.ErrValue) {
		t.Errorf("the refusal was %v, want an error carrying ferry.ErrValue: the plane is writable and one "+
			"value did not fit its format", err)
	}

	if !errors.Is(err, ferry.ErrDriver) {
		t.Errorf("the refusal was %v, want ferry.ErrDriver, because the cause came from below", err)
	}

	e, ok := errors.AsType[*ferry.Error](err)
	if !ok {
		t.Fatalf("the refusal was %T, want a *ferry.Error carrying the address", err)
	}

	if got, want := e.Address(), ferry.At("raw"); got != want {
		t.Errorf("the refusal names %s, want %s: one address is what an operator acts on", got, want)
	}

	if got, want := read(t, path), "kept: here\n"; got != want {
		t.Errorf("the plane holds %q, want %q: a dump that failed leaves the plane byte-identical", got, want)
	}

	onlyFile(t, filepath.Dir(path), "plane.yaml")
}

// TestModeSurvives asserts a dump does not silently re-permission a file
// somebody else set up.
func TestModeSurvives(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a mode set as root says nothing about what a dump preserves")
	}

	type config struct {
		Port int `ferry:"port"`
	}

	path := write(t, "port: 1\n")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("setting the plane's mode: %v", err)
	}

	if err := ferry.Dump(t.Context(), config{Port: 2}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if got := fi.Mode().Perm(); got != 0o640 {
		t.Errorf("the plane's mode is %v after a dump, want 0640", got)
	}
}

// read is the plane's contents as a string.
func read(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the plane: %v", err)
	}

	return string(data)
}

// onlyFile asserts the directory holds exactly the one file named, which is how
// a staged temporary that was not cleaned up is caught.
func onlyFile(t *testing.T, dir, name string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the plane's directory: %v", err)
	}

	if len(entries) == 1 && entries[0].Name() == name {
		return
	}

	got := make([]string, 0, len(entries))
	for _, e := range entries {
		got = append(got, e.Name())
	}

	t.Errorf("the plane's directory holds %v, want only %q: a staged file that outlives the dump is a temporary "+
		"left behind", got, name)
}

// openWriter binds a sink over the plane and opens it, which is the two phases
// a dump goes through.
func openWriter(t *testing.T, path string) ferry.Writer {
	t.Helper()

	open, err := yaml.NewSink(path).Bind(ferry.NewAddressSet(ferry.At("unused")))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	w, err := open(t.Context())
	if err != nil {
		t.Fatalf("opening the plane: %v", err)
	}

	t.Cleanup(func() {
		if c, ok := w.(ferry.Releaser); ok {
			_ = c.Close()
		}
	})

	return w
}

// commit runs the writer's Commit, which is what core does at the end of a walk
// that succeeded.
func commit(t *testing.T, w ferry.Writer) {
	t.Helper()

	c, ok := w.(ferry.Committer)
	if !ok {
		t.Fatal("the writer does not commit, and a staging sink that cannot commit holds nothing durable")
	}

	if err := c.Commit(t.Context()); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// errStop is the failure a test stages above the driver to fail a walk.
var errStop = errors.New("the test stopped this walk")

// shell wraps this driver's sink so a test can watch the walk and stop it.
//
// It carries the two optional interfaces through by assertion rather than
// declaring them, so what core sees is exactly what the driver implements.
type shell struct {
	inner ferry.Sink
	stop  ferry.Path
	seen  []record
}

// record is one Set the walk made.
type record struct {
	addr ferry.Path
	val  ferry.Value
}

// Bind hands the address set straight through.
func (s *shell) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Writer, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return shellWriter{inner: w, sink: s}, nil
	}, nil
}

// shellWriter records, forwards, and stops the walk at one address.
type shellWriter struct {
	inner ferry.Writer
	sink  *shell
}

func (w shellWriter) Set(ctx context.Context, addr ferry.Path, v ferry.Value) error {
	w.sink.seen = append(w.sink.seen, record{addr: addr, val: v})

	if addr == w.sink.stop {
		return errStop
	}

	return w.inner.Set(ctx, addr, v)
}

func (w shellWriter) Commit(ctx context.Context) error {
	c, ok := w.inner.(ferry.Committer)
	if !ok {
		return nil
	}

	return c.Commit(ctx)
}

func (w shellWriter) Close() error {
	c, ok := w.inner.(ferry.Releaser)
	if !ok {
		return nil
	}

	return c.Close()
}
