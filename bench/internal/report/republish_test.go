package report_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/onhotpath/ferry/bench/internal/report"
)

// The two published results files this package is tested against, both real.
//
//   - published_ci.md was produced by .github/workflows/perf.yml on
//     ubuntu-latest, which is the ordinary case.
//   - published_workstation.md was produced by hand on a developer's machine,
//     and its provenance table says "a developer workstation, not a CI runner"
//     and "working tree on perf/bench-harness". Neither is a value any workflow
//     input could supply, which is why recovery has to come off the file rather
//     than off the pipeline that usually writes it.
const (
	publishedCI          = "published_ci.md"
	publishedWorkstation = "published_workstation.md"
)

// publishedRecheck is benchstat run again over published_ci.md's own raw
// output, under a different filename.
//
// The filename is the point. benchstat writes the path it was handed into its
// own column header and pads every line of the table to it, so this fixture and
// the block published_ci.md carries differ in every single line while carrying
// identical measurements.
const publishedRecheck = "published_ci_recheck.txt"

func extract(t *testing.T, name string) *report.Published {
	t.Helper()

	pub, err := report.Extract(read(t, name))
	if err != nil {
		t.Fatalf("Extract(%s): %v", name, err)
	}

	return pub
}

// TestExtractRecoversTheProvenance is the decision this mode turns on, pinned.
//
// Every field below describes a measurement, and a re-render measures nothing,
// so every one of them comes off the published file rather than off the runtime
// re-rendering it or a workflow input somebody retyped. Two of them cannot come
// from anywhere else at all: NumCPU is only ever written into the CPUs cell, and
// the competitor versions are read from the build info of the binary that ran
// the benchmarks, which is not the binary re-rendering them.
func TestExtractRecoversTheProvenance(t *testing.T) {
	for _, name := range []string{publishedCI, publishedWorkstation} {
		t.Run(name, func(t *testing.T) {
			out, err := json.MarshalIndent(extract(t, name).Meta, "", "  ")
			if err != nil {
				t.Fatalf("marshalling the recovered provenance: %v", err)
			}

			golden(t, "want_meta_"+strings.TrimSuffix(name, ".md")+".json", string(out)+"\n")
		})
	}
}

// TestExtractLeavesTheLinksToTheCaller checks that the three links are not
// guessed at. They say where the files live in the repository rather than what
// was measured, and the results file spells the charts as bare filenames, so
// there is nothing in it to recover them from.
func TestExtractLeavesTheLinksToTheCaller(t *testing.T) {
	m := extract(t, publishedCI).Meta

	for name, got := range map[string]string{
		"ResultsLink":    m.ResultsLink,
		"ChartLightLink": m.ChartLightLink,
		"ChartDarkLink":  m.ChartDarkLink,
	} {
		if got != "" {
			t.Errorf("Extract filled in %s as %q; it is a fact about the repository layout "+
				"and the caller supplies it", name, got)
		}
	}
}

// TestExtractCarriesTheAppendicesVerbatim is the property the whole mode rests
// on: the two blocks come back as bytes, not as something re-derived.
func TestExtractCarriesTheAppendicesVerbatim(t *testing.T) {
	for _, name := range []string{publishedCI, publishedWorkstation} {
		t.Run(name, func(t *testing.T) {
			doc, pub := read(t, name), extract(t, name)

			assertFencedIn(t, doc, "benchstat", pub.BenchstatText)
			assertFencedIn(t, doc, "raw", pub.RawBench)
		})
	}
}

func assertFencedIn(t *testing.T, doc, what, block string) {
	t.Helper()

	if block == "" {
		t.Fatalf("the %s appendix came back empty", what)
	}

	if !strings.Contains(doc, "\n```\n"+block+"\n```\n") {
		t.Errorf("the %s appendix is not a fenced block of the file it came from, "+
			"so something re-derived it", what)
	}
}

// TestReproducesIgnoresTheInputPath is the trap this ticket exists for.
//
// benchstat writes the input file's path into its own column header and pads
// the table to it. A person extracting the raw block to a scratch file and
// running benchstat over it gets a block that differs from the published one in
// every line, with no measurement having moved, and either concludes the file
// is broken or commits the re-padded block. The check has to see through the
// path, and the re-render has to carry the published block through untouched.
func TestReproducesIgnoresTheInputPath(t *testing.T) {
	embedded := extract(t, publishedCI).BenchstatText
	fresh := read(t, publishedRecheck)

	if embedded == fresh {
		t.Fatal("the fixtures are byte-identical, so this test asserts nothing. " +
			"published_ci_recheck.txt must be benchstat over the same numbers under a different filename.")
	}

	if err := report.Reproduces(embedded, fresh); err != nil {
		t.Errorf("Reproduces rejected a run over identical bytes under another filename: %v", err)
	}
}

// TestReproducesRefusesAChangedNumber is the other half: seeing through the
// path must not turn into seeing through a measurement.
func TestReproducesRefusesAChangedNumber(t *testing.T) {
	embedded := extract(t, publishedCI).BenchstatText

	const (
		was  = "6.369µ"
		want = "6.370µ"
	)

	if !strings.Contains(embedded, was) {
		t.Fatalf("the fixture no longer carries %s, so this test mutates nothing", was)
	}

	if err := report.Reproduces(embedded, strings.Replace(embedded, was, want, 1)); err == nil {
		t.Error("Reproduces accepted a block with a different figure in it")
	}
}

// TestReproducesRefusesAnotherRun checks it is comparing the numbers rather
// than the shape: two real runs of the same benchmarks agree on every name and
// every unit and on nothing else.
func TestReproducesRefusesAnotherRun(t *testing.T) {
	ci := extract(t, publishedCI).BenchstatText
	workstation := extract(t, publishedWorkstation).BenchstatText

	if err := report.Reproduces(ci, workstation); err == nil {
		t.Error("Reproduces accepted a benchstat block from a different run entirely")
	}
}

// TestExtractRefusesAFileItDidNotWrite checks the failure is loud. A file with
// no provenance to recover must not re-render with an empty one.
func TestExtractRefusesAFileItDidNotWrite(t *testing.T) {
	doc := read(t, publishedCI)

	for name, broken := range map[string]string{
		"nothing at all":     "# some other document\n",
		"no provenance":      strings.Replace(doc, "## The machine, the toolchain and the versions", "## Elsewhere", 1),
		"no benchstat block": strings.Replace(doc, "## Raw benchstat output", "## Removed", 1),
		"no raw block":       strings.Replace(doc, "## Raw `go test -bench` output", "## Removed", 1),
		"no timestamp":       strings.Replace(doc, "\nGenerated 2026", "\nStamped 2026", 1),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := report.Extract(broken)
			if err == nil {
				t.Fatalf("Extract accepted a file with %s and returned %+v", name, got.Meta)
			}
		})
	}
}
