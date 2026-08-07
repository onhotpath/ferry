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

	onlyPlane(t, dir)
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

	onlyPlane(t, dir)
}

// TestDurableFlushFailureIsADumpThatDidNotCommit is the second sync's own
// failure, seen from where a caller meets it (#187).
//
// A directory with write and execute and no read takes the staged file and takes
// the rename, and refuses the open a flush of it needs. So the plane is replaced
// and the flush that would make the replacement durable cannot happen, which is
// a dump that did not commit: it reports, it carries the class the rename's own
// failure carries, and it leaves no temporary behind, because the rename has
// already taken the one there was.
//
// The plane holding the new document is asserted rather than tolerated. It is
// the one case where a save that failed has still replaced the file, and doc.go
// names it.
func TestDurableFlushFailureIsADumpThatDidNotCommit(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root opens a directory with no read bit, so there is no refusal to observe")
	}

	type config struct {
		Port int `ferry:"port"`
	}

	dir, path := unreadableDirWithPlane(t)

	err := ferry.Dump(t.Context(), config{Port: 8080}, yaml.NewSink(path, yaml.Durable()))

	// Restored before anything is asserted, because every assertion below reads
	// the directory this test just made unreadable.
	if cerr := os.Chmod(dir, 0o700); cerr != nil {
		t.Fatalf("restoring the directory: %v", cerr)
	}

	if err == nil {
		t.Fatal("a durable dump whose replacement could not be flushed reported that it committed")
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("the dump reported %v, want an error carrying ferry.ErrPlane, which is the class the rename's "+
			"own failure carries", err)
	}

	if got, want := read(t, path), "port: 8080\n"; got != want {
		t.Errorf("the plane holds %q, want %q: the rename landed and it is the flush after it that failed", got,
			want)
	}

	onlyPlane(t, dir)
}

// TestDefaultDumpDoesNotFlushTheDirectory is the option seen from the other
// side, and it is the one place the two modes are told apart from outside the
// driver (#188).
//
// The same directory that fails a durable dump takes a default one, because a
// default save never opens the directory at all. What it does still do is the
// atomic swap: the plane holds the new document and no temporary is left, which
// is the half that is not the caller's to decline.
func TestDefaultDumpDoesNotFlushTheDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root opens a directory with no read bit, so the durable mode would not fail here either")
	}

	type config struct {
		Port int `ferry:"port"`
	}

	dir, path := unreadableDirWithPlane(t)

	err := ferry.Dump(t.Context(), config{Port: 8080}, yaml.NewSink(path))

	if cerr := os.Chmod(dir, 0o700); cerr != nil {
		t.Fatalf("restoring the directory: %v", cerr)
	}

	if err != nil {
		t.Fatalf("a default dump failed with %v, and the flush it declined is the only thing this directory "+
			"refuses", err)
	}

	if got, want := read(t, path), "port: 8080\n"; got != want {
		t.Errorf("the plane holds %q, want %q: the swap is not the caller's to decline", got, want)
	}

	onlyPlane(t, dir)
}

// TestDurableDumpRoundTrips is the durable path on a directory that takes the
// flush: what a caller who asked for durability gets is the same document, not a
// different one.
func TestDurableDumpRoundTrips(t *testing.T) {
	type config struct {
		Port  int    `ferry:"port"`
		Label string `ferry:"label"`
	}

	dir := t.TempDir()
	path := filepath.Join(dir, planeName)
	want := config{Port: 8080, Label: "8080"}

	if err := ferry.Dump(t.Context(), want, yaml.NewSink(path, yaml.Durable())); err != nil {
		t.Fatalf("dump: %v", err)
	}

	got, err := ferry.Load[config](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("loaded %+v, want %+v", got, want)
	}

	onlyPlane(t, dir)
}

// unreadableDirWithPlane stages a plane in a directory that takes a write and
// refuses to be opened: write and execute let os.CreateTemp and os.Rename
// through, and the missing read bit is what a flush of the directory needs.
//
// The caller restores the mode, because it has to happen after the dump and
// before any assertion that reads the directory.
func unreadableDirWithPlane(t *testing.T) (dir, path string) {
	t.Helper()

	dir = t.TempDir()
	path = filepath.Join(dir, planeName)

	if err := os.WriteFile(path, []byte("port: 1\n"), 0o600); err != nil {
		t.Fatalf("writing the plane: %v", err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := os.Chmod(dir, 0o300); err != nil {
		t.Fatalf("making the directory unreadable: %v", err)
	}

	return dir, path
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

	open, err := yaml.NewSink(filepath.Join(dir, "plane.yaml")).Bind(addressesOf[scalars](t).set)
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

// TestAMappedAliasIsWrittenThroughToItsAnchor is the leaf half of #198: the
// address ferry writes is itself an alias.
//
// The write lands on the anchor, so the alias line is byte for byte what the
// operator wrote and the linkage survives. Replacing the alias node instead
// would have written `port: 5433` and quietly unshared it from `base`.
func TestAMappedAliasIsWrittenThroughToItsAnchor(t *testing.T) {
	type mapped struct {
		Port int `ferry:"port"`
	}

	type both struct {
		Base int `ferry:"base"`
		Port int `ferry:"port"`
	}

	path := write(t, "base: &b 5432\nport: *b\n")

	if err := ferry.Dump(t.Context(), mapped{Port: 5433}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "base: &b 5433\nport: *b\n"; got != want {
		t.Errorf("the plane holds %q, want %q: a mapped alias is written through to the anchor it names", got, want)
	}

	after, err := ferry.Load[both](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("loading back what the dump wrote: %v", err)
	}

	if after.Port != 5433 || after.Base != 5433 {
		t.Errorf("the plane loads back as %+v, want both 5433: an alias shares one value with its anchor", after)
	}
}

// TestAnAliasedMappingKeepsTheKeysItShares is the container half of #198, and it
// is the half that loses data rather than a linkage.
//
// `db` is an alias to a mapping and only `db/port` is mapped. Replacing the
// alias node with a fresh mapping wrote `db: {port: 5433}`, so `db/host` - which
// read back as localhost before the dump and which no field maps - was gone
// afterwards.
func TestAnAliasedMappingKeepsTheKeysItShares(t *testing.T) {
	type db struct {
		Port int `ferry:"port"`
	}

	type mapped struct {
		DB db `ferry:"db"`
	}

	type full struct {
		Host string `ferry:"host"`
		Port int    `ferry:"port"`
	}

	type both struct {
		DB full `ferry:"db"`
	}

	path := write(t, "base: &b\n  host: localhost\n  port: 5432\ndb: *b\n")

	if err := ferry.Dump(t.Context(), mapped{DB: db{Port: 5433}}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "base: &b\n  host: localhost\n  port: 5433\ndb: *b\n"; got != want {
		t.Errorf("the plane holds %q, want %q", got, want)
	}

	after, err := ferry.Load[both](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("loading back what the dump wrote: %v", err)
	}

	if after.DB.Host != "localhost" {
		t.Errorf("db/host reads back as %q after a dump at db/port, want %q: a key no field maps and that the "+
			"address only reached through an alias is not the dump's to drop", after.DB.Host, "localhost")
	}
}

// TestAnAliasedSequenceIsWrittenThrough is the same walk through a sequence
// rather than a mapping, which is the other container kind an address can need.
func TestAnAliasedSequenceIsWrittenThrough(t *testing.T) {
	type mapped struct {
		Other []string `ferry:"other"`
	}

	path := write(t, "list: &l\n  - a\n  - b\nother: *l\n")

	if err := ferry.Dump(t.Context(), mapped{Other: []string{"a", "z"}}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "list: &l\n  - a\n  - z\nother: *l\n"; got != want {
		t.Errorf("the plane holds %q, want %q", got, want)
	}
}

// TestAnAliasAtASequencePositionIsWrittenThrough puts the alias at a position
// rather than under a key, which is the segment kind the mapping cases never
// reach.
func TestAnAliasAtASequencePositionIsWrittenThrough(t *testing.T) {
	type mapped struct {
		Ports []int `ferry:"ports"`
	}

	path := write(t, "base: &b 5432\nports:\n  - *b\n")

	if err := ferry.Dump(t.Context(), mapped{Ports: []int{5433}}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "base: &b 5433\nports:\n  - *b\n"; got != want {
		t.Errorf("the plane holds %q, want %q", got, want)
	}
}

// TestAnAliasToAScalarIsReplacedWhereTheAddressNeedsAContainer is where writing
// through stops, and the guard is what keeps it from doing damage.
//
// `db` aliases a scalar and the destination type says db is a mapping. Following
// the alias would rewrite `base` into a mapping under `other`, which no field
// maps and which reads back as 5432 - and it would keep nothing, because an
// anchored scalar has no members for the reshape to lose. So the alias node
// itself is replaced, which is what this driver did before #198.
func TestAnAliasToAScalarIsReplacedWhereTheAddressNeedsAContainer(t *testing.T) {
	type db struct {
		Port int `ferry:"port"`
	}

	type mapped struct {
		DB db `ferry:"db"`
	}

	path := write(t, "base: &b 5432\ndb: *b\nother: *b\n")

	if err := ferry.Dump(t.Context(), mapped{DB: db{Port: 5433}}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "base: &b 5432\ndb:\n  port: 5433\nother: *b\n"; got != want {
		t.Errorf("the plane holds %q, want %q: an alias naming a scalar at an address that has to be a mapping "+
			"is replaced rather than followed", got, want)
	}
}

// TestTwoAddressesAtOneAnchorAreRefused is the hazard writing through an alias
// introduces, caught before anything is written (#198).
//
// The document says base and port are one value and the destination says they
// are two. Whichever the walk reached last would have won, so a dump that
// returned nil would load back a value the caller never held. It is refused, and
// the plane is left byte for byte as it was.
func TestTwoAddressesAtOneAnchorAreRefused(t *testing.T) {
	type config struct {
		Base int `ferry:"base"`
		Port int `ferry:"port"`
	}

	const doc = "base: &b 5432\nport: *b\n"

	path := write(t, doc)

	err := ferry.Dump(t.Context(), config{Base: 1, Port: 2}, yaml.NewSink(path))
	if err == nil {
		t.Fatal("two addresses were written to one anchored value with different values, and the file can hold " +
			"only one of them")
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("the refusal was %v, want an error carrying ferry.ErrPlane: what refused is the shape of the "+
			"document rather than the value", err)
	}

	e, ok := errors.AsType[*ferry.Error](err)
	if !ok {
		t.Fatalf("the refusal was %T, want a *ferry.Error carrying the address", err)
	}

	if got, want := e.Address(), ferry.At("port"); got != want {
		t.Errorf("the refusal names %s, want %s: the address that arrived second is the one that could not be "+
			"written", got, want)
	}

	if got := read(t, path); got != doc {
		t.Errorf("the plane holds %q, want %q: a dump that failed leaves the plane byte-identical", got, doc)
	}

	onlyPlane(t, filepath.Dir(path))
}

// TestTwoAddressesUnderOneAnchorAreRefused is the same collision one level down,
// where neither node written carries an anchor of its own.
//
// The anchor is on the mapping and the addresses land on a leaf under it, so a
// guard that asked whether the node in hand was anchored would have let this
// through - and it wrote 2 where the caller's struct said 1, with the dump
// returning nil.
func TestTwoAddressesUnderOneAnchorAreRefused(t *testing.T) {
	type db struct {
		Port int `ferry:"port"`
	}

	type config struct {
		Base db `ferry:"base"`
		DB   db `ferry:"db"`
	}

	const doc = "base: &b\n  port: 5432\ndb: *b\n"

	path := write(t, doc)

	err := ferry.Dump(t.Context(), config{Base: db{Port: 1}, DB: db{Port: 2}}, yaml.NewSink(path))
	if err == nil {
		t.Fatal("two addresses under one anchored mapping were given different values, and the file can hold " +
			"only one of them")
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("the refusal was %v, want an error carrying ferry.ErrPlane", err)
	}

	if got := read(t, path); got != doc {
		t.Errorf("the plane holds %q, want %q: a dump that failed leaves the plane byte-identical", got, doc)
	}

	onlyPlane(t, filepath.Dir(path))
}

// TestTwoAddressesAtOneAnchorAreRefusedAtAContainerWrite is the same collision
// where one of the two addresses is a container's own.
//
// A container write is the first thing that can change the kind of an anchored
// node, so it is the one write an alias to that node is not followed through
// (#198). The two addresses then landed on two different nodes, the collision
// was never seen, and the dump destroyed the alias in silence and returned nil -
// with which of the two happened depending on the order of the struct's fields.
func TestTwoAddressesAtOneAnchorAreRefusedAtAContainerWrite(t *testing.T) {
	type opts struct {
		Host string `ferry:"host,omitzero"`
	}

	type baseFirst struct {
		Base *opts  `ferry:"base"`
		Use  string `ferry:"use"`
	}

	type useFirst struct {
		Use  string `ferry:"use"`
		Base *opts  `ferry:"base"`
	}

	const doc = "base: &b hello\nuse: *b\n"

	t.Run("the container is written first", func(t *testing.T) {
		refusesAnchor(t, doc, baseFirst{Base: &opts{}, Use: "z"})
	})

	t.Run("the leaf is written first", func(t *testing.T) {
		refusesAnchor(t, doc, useFirst{Use: "z", Base: &opts{}})
	})
}

// refusesAnchor dumps one value over one document and asserts that the shared
// anchor was refused and the plane left as it was.
func refusesAnchor[T any](t *testing.T, doc string, v T) {
	t.Helper()

	path := write(t, doc)

	err := ferry.Dump(t.Context(), v, yaml.NewSink(path))
	if err == nil {
		t.Fatal("two addresses were written to one anchored value with different values, and the file can hold " +
			"only one of them")
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("the refusal was %v, want an error carrying ferry.ErrPlane", err)
	}

	if got := read(t, path); got != doc {
		t.Errorf("the plane holds %q, want %q: a dump that failed leaves the plane byte-identical", got, doc)
	}

	onlyPlane(t, filepath.Dir(path))
}

// TestTwoAddressesAtOneAnchorAgreeing is the other side of that guard: the
// document says the two are one value and the destination agrees, so there is
// nothing to refuse.
func TestTwoAddressesAtOneAnchorAgreeing(t *testing.T) {
	type config struct {
		Base int `ferry:"base"`
		Port int `ferry:"port"`
	}

	path := write(t, "base: &b 5432\nport: *b\n")

	if err := ferry.Dump(t.Context(), config{Base: 5433, Port: 5433}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "base: &b 5433\nport: *b\n"; got != want {
		t.Errorf("the plane holds %q, want %q", got, want)
	}
}

// TestAMergeKeySurvivesASave pins the merge key against the tag YAML resolves
// for it (#198).
//
// A save re-emits the whole document, and the emitter printed a tag it could not
// re-derive from the text, so `<<: *d` came back as `!!merge <<: *d` - a key the
// operator wrote, rewritten by a dump that was not addressing it.
func TestAMergeKeySurvivesASave(t *testing.T) {
	type db struct {
		Port int `ferry:"port"`
	}

	type config struct {
		DB db `ferry:"db"`
	}

	path := write(t, "defaults: &d\n  host: localhost\n  port: 5432\ndb:\n  <<: *d\n  port: 5432\n")

	if err := ferry.Dump(t.Context(), config{DB: db{Port: 5433}}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	want := "defaults: &d\n  host: localhost\n  port: 5432\ndb:\n  <<: *d\n  port: 5433\n"
	if got := read(t, path); got != want {
		t.Errorf("the plane holds %q, want %q: a merge key is the operator's and a save does not retag it", got, want)
	}
}

// TestAnAliasNoFieldMapsIsUntouched is the promise the alias work must not have
// cost: a dump at one address leaves an anchor and an alias somewhere else in
// the document exactly as they were parsed.
func TestAnAliasNoFieldMapsIsUntouched(t *testing.T) {
	type config struct {
		Name string `ferry:"name"`
	}

	path := write(t, "name: old\nbase: &b 5432\nport: *b\n")

	if err := ferry.Dump(t.Context(), config{Name: "new"}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "name: new\nbase: &b 5432\nport: *b\n"; got != want {
		t.Errorf("the plane holds %q, want %q", got, want)
	}
}

// TestAnUnhandledTagSurvivesThreeStages is the acceptance bar for the tag a
// save used to destroy (#155): load, dump, load, with the third stage compared
// against the first, and the file's own text asserted alongside.
//
// !!timestamp is a tag this driver has no reading of its own for, at a key the
// struct maps, so the save rewrites that scalar and used to write it back as a
// plain quoted string. The tag is the operator's, the way #199 found the anchor
// to be, and a dump that was asked to change nothing must change nothing.
func TestAnUnhandledTagSurvivesThreeStages(t *testing.T) {
	type config struct {
		When    string `ferry:"when"`
		Timeout string `ferry:"timeout"`
		Port    int    `ferry:"port"`
	}

	// Two tags, because YAML resolves one of them back from the bare text and
	// cannot resolve the other at all, and a save has to keep both lines.
	const doc = "when: !!timestamp 2026-08-02T12:00:00Z\ntimeout: !mycompany:duration 30s\nport: 5432\n"

	path := write(t, doc)

	first, err := ferry.Load[config](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("the first load: %v", err)
	}

	if err := ferry.Dump(t.Context(), first, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got := read(t, path); got != doc {
		t.Errorf("the plane holds %q, want %q: the tag at a mapped key is the operator's and the save was asked "+
			"to change no value", got, doc)
	}

	third, err := ferry.Load[config](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("the third load: %v", err)
	}

	if third != first {
		t.Errorf("the third stage is %+v and the first was %+v", third, first)
	}
}

// TestTheDriversOwnSpellingBeatsTheTagItReplaces is the first guard on carrying
// a tag: this driver has a spelling for !!float and for !!int, so the tag it
// writes wins over the one that was there.
//
// Without the guard a value could be written under a tag contradicting it, and
// the file would say !!float over an integer nothing rounded.
func TestTheDriversOwnSpellingBeatsTheTagItReplaces(t *testing.T) {
	type config struct {
		Port int `ferry:"port"`
	}

	path := write(t, "port: 3.5\n")

	if err := ferry.Dump(t.Context(), config{Port: 8080}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "port: 8080\n"; got != want {
		t.Errorf("the plane holds %q, want %q: a tag this driver spells itself is not the operator's to keep",
			got, want)
	}
}

// TestATagIsDroppedWhereTheKindChanged is the second guard: the address held a
// string under a tag this driver does not read, and now holds a number.
//
// The tag described the value that was there, so it is stale in the way a
// copied quoting style is, and keeping it would leave the file claiming a
// timestamp over an integer.
func TestATagIsDroppedWhereTheKindChanged(t *testing.T) {
	type config struct {
		When int `ferry:"when"`
	}

	path := write(t, "when: !!timestamp 2026-08-02T12:00:00Z\n")

	if err := ferry.Dump(t.Context(), config{When: 5}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "when: 5\n"; got != want {
		t.Errorf("the plane holds %q, want %q: a tag over a value of another kind is stale", got, want)
	}
}

// TestAnAliasedDocumentSurvivesThreeStages is the acceptance bar a lossy round
// trip hides from: load, dump, load, with the third stage compared against the
// first.
//
// A read and a write that are wrong in the same direction round-trip cleanly, so
// the file's own text is asserted alongside the values.
func TestAnAliasedDocumentSurvivesThreeStages(t *testing.T) {
	type db struct {
		Host string `ferry:"host"`
		Port int    `ferry:"port"`
	}

	type config struct {
		DB db `ferry:"db"`
	}

	const doc = "base: &b\n  host: localhost\n  port: 5432\ndb: *b\n"

	path := write(t, doc)

	first, err := ferry.Load[config](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("the first load: %v", err)
	}

	if err := ferry.Dump(t.Context(), first, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got := read(t, path); got != doc {
		t.Errorf("the plane holds %q, want %q: dumping what was just loaded changes no value and so changes no "+
			"line", got, doc)
	}

	third, err := ferry.Load[config](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("the third load: %v", err)
	}

	if third != first {
		t.Errorf("the third stage is %+v and the first was %+v", third, first)
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

	a := addressesOf[scalars](t)

	if err := w.Set(t.Context(), a.leaf(t, ferry.At("kept")), ferry.String("here")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	err := w.Set(t.Context(), a.leaf(t, ferry.At("gone")), ferry.Value{})
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

	onlyPlane(t, filepath.Dir(path))
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

// planeName is what every test in this package calls its plane.
const planeName = "plane.yaml"

// onlyPlane asserts the directory holds the plane and nothing else, which is how
// a staged temporary that was not cleaned up is caught.
func onlyPlane(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the plane's directory: %v", err)
	}

	if len(entries) == 1 && entries[0].Name() == planeName {
		return
	}

	got := make([]string, 0, len(entries))
	for _, e := range entries {
		got = append(got, e.Name())
	}

	t.Errorf("the plane's directory holds %v, want only %q: a staged file that outlives the dump is a temporary "+
		"left behind", got, planeName)
}

// openWriter binds a sink over the plane and opens it, which is the two phases
// a dump goes through.
func openWriter(t *testing.T, path string) ferry.Writer {
	t.Helper()

	open, err := yaml.NewSink(path).Bind(addressesOf[scalars](t).set)
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

func (w shellWriter) Set(ctx context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	w.sink.seen = append(w.sink.seen, record{addr: addr.Path(), val: v})

	if addr.Path() == w.sink.stop {
		return errStop
	}

	return w.inner.Set(ctx, addr, v)
}

// Ensure forwards the container-level write, so a shell over a sink that can
// spell one does not silently take the capability away (ADR-0016).
func (w shellWriter) Ensure(ctx context.Context, addr ferry.Container, p ferry.Presence) error {
	e, ok := w.inner.(ferry.Ensurer)
	if !ok {
		return nil
	}

	return e.Ensure(ctx, addr, p)
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
