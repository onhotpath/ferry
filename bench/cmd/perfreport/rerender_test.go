package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onhotpath/ferry/bench/internal/report"
)

// The fixtures live with the package that extracts them, and are read from here
// rather than copied, because a second copy of a hundred-kilobyte published file
// is a second thing that can go stale.
//
// published_ci.md was written by .github/workflows/perf.yml. The CSV and the
// recheck file are benchstat over the raw output that file embeds, the recheck
// one deliberately under a different filename.
const (
	fixtures = "../../internal/report/testdata"

	fixtureResults = "published_ci.md"
	fixtureCSV     = "published_ci_stat.csv"
	fixtureRecheck = "published_ci_recheck.txt"
	fixtureLight   = "published_ci_light.svg"
	fixtureDark    = "published_ci_dark.svg"
)

func fixture(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(fixtures, name))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}

	return string(b)
}

// rerenderOpts is the mode as the workflow invokes it, writing into a scratch
// directory so that the published file can be compared with what came out.
func rerenderOpts(t *testing.T, dir string) *opts {
	t.Helper()

	return &opts{
		rerender: filepath.Join(fixtures, fixtureResults),
		csv:      filepath.Join(fixtures, fixtureCSV),
		recheck:  filepath.Join(fixtures, fixtureRecheck),
		results:  filepath.Join(dir, "results.md"),
		linkDir:  "docs/perf",
	}
}

// TestRerenderRepublishesTheSameBytes is the acceptance test for the whole
// mode, and everything else about it is worthless without it.
//
// Re-rendering a published file with no prose changed has to give back exactly
// the file that was published, and the two charts beside it. Anything less and
// the diff a reviewer reads is a mix of the correction that was intended and
// whatever the re-render moved on its own.
//
// It is also where every recovered field is proved at once: the provenance
// table carries the runner, both revisions, the flags, the command and the
// timestamp; the CPUs cell carries GOMAXPROCS, which no re-render could measure
// because it is a fact about the machine that ran the benchmarks; and both
// charts carry the runner, the toolchain and the flags in their footers.
func TestRerenderRepublishesTheSameBytes(t *testing.T) {
	dir := t.TempDir()

	if err := rerenderOpts(t, dir).republish(); err != nil {
		t.Fatalf("republish: %v", err)
	}

	for name, want := range map[string]string{
		"results.md":     fixture(t, fixtureResults),
		"perf-light.svg": fixture(t, fixtureLight),
		"perf-dark.svg":  fixture(t, fixtureDark),
	} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("reading what was re-rendered: %v", err)
		}

		if string(got) != want {
			t.Errorf("re-rendering changed %s. A re-render measures nothing, so the only thing "+
				"that may move is prose the harness supplies.\n%s", name, firstDifference(want, string(got)))
		}
	}
}

// firstDifference names the line the two disagree on, because a hundred
// kilobytes of diff in a test log is not a report anybody reads.
func firstDifference(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")

	for i := range min(len(w), len(g)) {
		if w[i] != g[i] {
			return fmt.Sprintf("line %d:\n  published:   %s\n  re-rendered: %s", i+1, w[i], g[i])
		}
	}

	return fmt.Sprintf("the files differ in length: %d lines published, %d re-rendered", len(w), len(g))
}

// TestRerenderPatchesTheREADMEFromTheRecoveredProvenance checks the README's
// section is written from the file's own provenance too, and not from the
// machine doing the re-render.
func TestRerenderPatchesTheREADMEFromTheRecoveredProvenance(t *testing.T) {
	dir := t.TempDir()
	readme := filepath.Join(dir, "README.md")

	if err := os.WriteFile(readme, []byte(report.BeginMarker+"\nstale\n"+report.EndMarker+"\n"), 0o600); err != nil {
		t.Fatalf("writing the README: %v", err)
	}

	o := rerenderOpts(t, dir)
	o.readme = readme

	if err := o.republish(); err != nil {
		t.Fatalf("republish: %v", err)
	}

	const want = "Run on ubuntu-latest, `-count 10`, `-benchtime 1s`, Go go1.27rc2."

	if got := fixture(t, fixtureResults); !strings.Contains(got, "| runner | ubuntu-latest |") {
		t.Fatal("the fixture no longer names ubuntu-latest, so this test asserts nothing")
	}

	b, err := os.ReadFile(readme)
	if err != nil {
		t.Fatalf("reading the README: %v", err)
	}

	if !strings.Contains(string(b), want) {
		t.Errorf("the README section does not read %q, so it was written from something other "+
			"than the published file's provenance:\n%s", want, b)
	}
}

// TestRerenderRefusesToRestateTheMeasurement is the decision in code.
//
// Every one of these flags describes the run. A re-render does not run
// anything, so accepting one and quietly ignoring it is how a corrected file
// comes to name a runner it never executed on.
func TestRerenderRefusesToRestateTheMeasurement(t *testing.T) {
	for name, set := range map[string]func(*opts){
		"-runner":      func(o *opts) { o.runner = "somewhere else" },
		"-ferry-rev":   func(o *opts) { o.ferryRev = "deadbeef" },
		"-harness-rev": func(o *opts) { o.benchRev = "deadbeef" },
		"-count":       func(o *opts) { o.count = "20" },
		"-benchtime":   func(o *opts) { o.benchtime = "2s" },
		"-command":     func(o *opts) { o.command = "go test ." },
		"-generated":   func(o *opts) { o.generated = "2030-01-01T00:00:00Z" },
		"-stat":        func(o *opts) { o.stat = "stat.txt" },
		"-raw":         func(o *opts) { o.raw = "bench.txt" },
	} {
		t.Run(name, func(t *testing.T) {
			o := rerenderOpts(t, t.TempDir())
			set(o)

			if err := o.republish(); err == nil {
				t.Errorf("re-render accepted %s, which describes the measurement it is recovering", name)
			}
		})
	}
}

// TestRerenderRefusesWithoutTheReproductionCheck checks the guard cannot be
// skipped by leaving a flag off.
func TestRerenderRefusesWithoutTheReproductionCheck(t *testing.T) {
	o := rerenderOpts(t, t.TempDir())
	o.recheck = ""

	if err := o.republish(); err == nil {
		t.Error("re-render published without checking that the file's raw output still " +
			"produces its own benchstat block")
	}
}

// TestRerenderRefusesAFileThatDoesNotReproduce is the guard doing its job: a
// published file whose tables no longer follow from its own raw output is not
// re-rendered, it is reported.
func TestRerenderRefusesAFileThatDoesNotReproduce(t *testing.T) {
	dir := t.TempDir()
	wrong := filepath.Join(dir, "recheck.txt")

	fresh := strings.Replace(fixture(t, fixtureRecheck), "6.369µ", "9.999µ", 1)
	if err := os.WriteFile(wrong, []byte(fresh), 0o600); err != nil {
		t.Fatalf("writing the recheck file: %v", err)
	}

	o := rerenderOpts(t, dir)
	o.recheck = wrong

	if err := o.republish(); err == nil {
		t.Error("re-render published a file whose raw output no longer produces its benchstat block")
	}
}

// TestExtractWritesWhatBenchstatIsGiven checks the first step hands the raw
// output on unchanged, since everything downstream is derived from it.
func TestExtractWritesWhatBenchstatIsGiven(t *testing.T) {
	dir := t.TempDir()

	o := &opts{rerender: filepath.Join(fixtures, fixtureResults), extractTo: dir}
	if err := o.republish(); err != nil {
		t.Fatalf("republish: %v", err)
	}

	doc := fixture(t, fixtureResults)

	for _, name := range []string{rawName, statName} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}

		if !strings.Contains(doc, strings.TrimRight(string(b), "\n")) {
			t.Errorf("%s is not a block of the published file, so extraction changed it", name)
		}
	}
}
