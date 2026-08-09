package report_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onhotpath/ferry/bench/internal/report"
)

// update rewrites the golden files. The fixtures under testdata are real
// captured output from a real run on a real machine; the goldens are what this
// package rendered from them, and regenerating them is a reviewable diff.
var update = flag.Bool("update", false, "rewrite the golden files under testdata")

func read(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}

	return string(b)
}

func golden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name)

	if *update {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatalf("writing the golden: %v", err)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the golden: %v (run with -update to create it)", err)
	}

	if got != string(want) {
		t.Errorf("rendered output differs from %s; run with -update and review the diff", path)
		t.Logf("got:\n%s", got)
	}
}

// input builds the renderer's input from the captured fixtures. The scenario
// descriptions are a small hand-written subset rather than the real harness's,
// so that this test pins the rendering and not the wording of a note.
func input(t *testing.T) *report.Input {
	t.Helper()

	stats, err := report.ParseCSV(read(t, "stat.csv"))
	if err != nil {
		t.Fatalf("parsing the CSV: %v", err)
	}

	return &report.Input{
		Meta: report.Meta{
			GoVersion:       "go1.27rc2",
			GOOS:            "linux",
			GOARCH:          "amd64",
			NumCPU:          6,
			Runner:          "a workstation, not a runner",
			FerryRevision:   "d390a8c",
			HarnessRevision: "0000000",
			Count:           "10",
			Benchtime:       "1s",
			Command:         "go test -run '^$' -bench . -benchmem -benchtime 1s -count 10 .",
			Generated:       "2026-08-04T00:00:00Z",
			Modules: map[string]string{
				"github.com/knadh/koanf/v2": "v2.3.6",
				"github.com/spf13/viper":    "v1.21.0",
			},
			ResultsLink:    "docs/perf/results.md",
			ChartLightLink: "docs/perf/perf-light.svg",
			ChartDarkLink:  "docs/perf/perf-dark.svg",
		},
		Stats:     stats,
		Scenarios: fixtureScenarios(),
		Absences: []report.AbsenceDoc{
			{Library: "github.com/example/absent", Scenario: "all", Why: "it does not exist."},
		},
		BenchstatText: read(t, "stat.txt"),
		RawBench:      read(t, "bench.txt"),
	}
}

func fixtureScenarios() []report.ScenarioDoc {
	return []report.ScenarioDoc{{
		Name:  "env_small",
		What:  "five flat fields out of the process environment.",
		Impls: fixtureImpls(),
	}, {
		// A scenario carrying a warm figure that is not comparable with the
		// rest of its column. This is the real yaml_small shape: xload's YAML
		// provider parses in its constructor, so its warm number is a map
		// lookup while every other row's is a whole load.
		//
		// The fixture had no such scenario, and that is exactly how the
		// summary came to rank ferry against a figure the results file's own
		// footnote called incomparable. A golden that covers only the easy
		// shape pins only the easy shape.
		Name:  "yaml_small",
		What:  "five flat fields out of a small YAML document.",
		Impls: fixtureYAMLImpls(),
	}, {
		// A scenario the CSV carries no rows for at all, which is the case
		// that must render as "not measured" in every cell rather than as a
		// zero or a blank.
		Name:  "no_such_scenario",
		What:  "a scenario nothing was measured for.",
		Impls: []report.ImplDoc{{Name: "ferry", Module: ferryModule, Notes: "n/a"}},
	}}
}

// fixtureYAMLImpls carries the one WarmCaveat in the fixture.
func fixtureYAMLImpls() []report.ImplDoc {
	return []report.ImplDoc{
		{Name: "ferry", Module: ferryModule, Notes: "Reads and parses on every load."},
		{Name: "koanf", Module: "github.com/knadh/koanf/v2", Notes: "Reads and parses on every load."},
		{Name: "viper", Module: "github.com/spf13/viper", Notes: "Reads and parses on every load."},
		{
			Name:   "xload",
			Module: "github.com/gojekfarm/xtools/xload",
			Notes:  "Its YAML provider parses in the constructor.",
			WarmCaveat: "xload's YAML provider reads and parses the file once, when the loader is " +
				"constructed, so this warm figure excludes the read and the parse every other row pays.",
		},
		{Name: "stdlib", Module: "", Notes: "Direct yaml.Unmarshal.", Baseline: true},
	}
}

// ferryModule is the import path both ferry rows are attributed to. A variant
// is the same library, so it carries the same path.
const ferryModule = "github.com/onhotpath/ferry"

// fixtureImpls carries the one variant in the fixture, and the captured CSV has
// no rows for it. That is the shape this golden is for: a variant the run did
// not measure has to reach the file as words in every one of its cells, in the
// scenario table and in the section that renders it against ferry, and never as
// a zero or a ratio computed against one.
//
// The measured shape is pinned by TestVariantIsPublishedAgainstTheRowItVaries,
// over a CSV written for it.
func fixtureImpls() []report.ImplDoc {
	return []report.ImplDoc{
		{Name: "ferry", Module: ferryModule, Notes: "Compiles once, caches the schema.",
			Remark: "compiled schema"},
		{
			Name: "ferry-bound", Module: ferryModule, Variant: true,
			Notes:  "The same job through a caller-held binding.",
			Remark: "held binding",
		},
		{Name: "koanf", Module: "github.com/knadh/koanf/v2", Notes: "Reads the whole environ per load.",
			Remark: "reads the whole environ per load"},
		{Name: "viper", Module: "github.com/spf13/viper", Notes: "Needs every key registered."},
		{Name: "xload", Module: "github.com/gojekfarm/xtools/xload", Notes: "Reflects per call."},
		{Name: "go-envconfig", Module: "github.com/sethvargo/go-envconfig", Notes: "Reflects per call."},
		{Name: "kelseyhightower", Module: "github.com/kelseyhightower/envconfig", Notes: "Reflects per call."},
		{Name: "stdlib", Module: "", Notes: "Hand-rolled os.Getenv.", Baseline: true,
			Remark: "os.Getenv plus strconv, by hand"},
	}
}

// variantCSV is written for these three tests rather than captured, because the
// case they cover is a variant that came out faster than the row it varies and
// faster than every other library in its scenario, which is exactly the shape
// that would corrupt a ranking. No captured run is needed to state it and none
// of these figures reaches a published file.
const variantCSV = `goos: linux
goarch: amd64
pkg: github.com/onhotpath/ferry/bench
cpu: a test fixture

,bench.txt,
,sec/op,CI
Load/env_small/cold/ferry-6,4e-06,2%
Load/env_small/warm/ferry-6,2e-06,2%
Load/env_small/cold/ferry-bound-6,4e-06,2%
Load/env_small/warm/ferry-bound-6,1e-06,2%
Load/env_small/cold/koanf-6,3e-06,2%
Load/env_small/warm/koanf-6,3e-06,2%
Load/env_small/cold/stdlib-6,15e-07,2%
Load/env_small/warm/stdlib-6,15e-07,2%
geomean,2.5e-06,

,B/op,CI
Load/env_small/warm/ferry-6,8000,1%
Load/env_small/warm/ferry-bound-6,3200,1%
Load/env_small/warm/koanf-6,17000,1%
Load/env_small/warm/stdlib-6,800,1%
geomean,5000,

,allocs/op,CI
Load/env_small/warm/ferry-6,100,1%
Load/env_small/warm/ferry-bound-6,40,1%
Load/env_small/warm/koanf-6,220,1%
Load/env_small/warm/stdlib-6,10,1%
geomean,72,
`

// variantInput is one scenario, with ferry, a variant of it, one library and
// the baseline, all measured.
func variantInput(t *testing.T) *report.Input {
	t.Helper()

	stats, err := report.ParseCSV(variantCSV)
	if err != nil {
		t.Fatalf("parsing the CSV: %v", err)
	}

	return &report.Input{
		Meta:  report.Meta{Generated: "2026-08-05T00:00:00Z"},
		Stats: stats,
		Scenarios: []report.ScenarioDoc{{
			Name:  "env_small",
			What:  "five flat fields out of the process environment.",
			Impls: fixtureImpls()[:2:2],
		}},
	}
}

// withCompetitors puts a library and the baseline back beside the two ferry
// rows, for the two tests that are about ranking.
func withCompetitors(in *report.Input) *report.Input {
	in.Scenarios[0].Impls = append(in.Scenarios[0].Impls,
		report.ImplDoc{Name: "koanf", Module: "github.com/knadh/koanf/v2", Notes: "n/a"},
		report.ImplDoc{Name: "stdlib", Notes: "n/a", Baseline: true},
	)

	return in
}

// TestVariantIsPublishedAgainstTheRowItVaries is the reason a variant is
// measured at all: the distance between it and the row it varies, computed from
// the two warm figures rather than described.
func TestVariantIsPublishedAgainstTheRowItVaries(t *testing.T) {
	got := report.Results(variantInput(t))

	i := strings.Index(got, "## The same library, measured a second way")
	if i < 0 {
		t.Fatal("the results file has no section rendering the variant against the row it varies")
	}

	row := section(got[i+1:])

	// 2µs against 1µs on the warm figure, and 100 allocations against 40.
	for _, want := range []string{"ferry-bound", "2.00µs", "1.00µs", "2.00x faster", "| 40 |", "| 100 |"} {
		if !strings.Contains(row, want) {
			t.Errorf("the variant section does not carry %q:\n%s", want, row)
		}
	}
}

// TestVariantIsNotALibraryThatBeatFerry is the rule the flag exists for.
//
// The variant in the fixture is the fastest warm figure in its scenario bar the
// baseline's. It is ferry, so reporting it as the fastest other library, or as
// a library ferry lost to, would put "ferry is 2.00x slower than ferry" in two
// published documents.
func TestVariantIsNotALibraryThatBeatFerry(t *testing.T) {
	in := withCompetitors(variantInput(t))

	const variant = "ferry-bound"

	cell := summaryRow(t, report.Summary(in), "env_small")[fastestOtherCell]
	if strings.Contains(cell, "("+variant+")") {
		t.Errorf("the summary names %s as the fastest other library: %q\n"+
			"It is ferry through a second entry point, so it is not another library at all.", variant, cell)
	}

	if !strings.Contains(cell, "(koanf)") {
		t.Errorf("the summary's fastest-other cell is %q, want the one real library in the scenario", cell)
	}

	for line := range strings.SplitSeq(losses(t, report.Results(in)), "\n") {
		if strings.Contains(line, variant) {
			t.Errorf("\"where ferry loses\" lists %s: %q", variant, line)
		}
	}
}

// TestVariantIsNotSilentlyDropped is the other half of the same rule, and it
// exists so that excluding a variant from the rankings cannot become a way to
// leave a number out.
func TestVariantIsNotSilentlyDropped(t *testing.T) {
	in := withCompetitors(variantInput(t))
	got := report.Results(in)

	cells := scenarioRow(t, got, "env_small", report.ImplDoc{Name: "ferry-bound"})
	if strings.Contains(strings.Join(cells, "|"), "not measured") {
		t.Errorf("the variant's own row in the scenario table is not fully published: %q", cells)
	}
}

// losses returns the "where ferry loses" section.
func losses(t *testing.T, results string) string {
	t.Helper()

	i := strings.Index(results, "## Where ferry loses")
	if i < 0 {
		t.Fatal("the results file has no \"where ferry loses\" section")
	}

	return section(results[i+1:])
}

// section returns what precedes the next "## " heading.
func section(rest string) string {
	if j := strings.Index(rest, "\n## "); j > 0 {
		return rest[:j]
	}

	return rest
}

func TestParseCSVReadsTheCapturedRun(t *testing.T) {
	stats, err := report.ParseCSV(read(t, "stat.csv"))
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}

	if _, ok := stats.Geomean["sec/op"]; !ok {
		t.Error("no sec/op geomean; benchstat emits one and the results file publishes it")
	}

	// benchstat leaves the geomean cell empty for a unit where some row is
	// zero, because a geometric mean over a zero is not a number. That has to
	// come through as absent rather than as a zero, which is the same rule
	// every other cell in this package follows.
	if got, ok := stats.Geomean["B/op"]; ok && got.Value == 0 {
		t.Errorf("B/op geomean parsed as %v; benchstat declined to compute it and it must stay absent", got)
	}

	m, ok := stats.Lookup(report.Key{Scenario: "env_small", Mode: "warm", Impl: "ferry"})
	if !ok {
		t.Fatal("env_small/warm/ferry is missing from the parsed CSV")
	}

	if m["sec/op"].Value <= 0 {
		t.Errorf("sec/op is %v, want a positive duration", m["sec/op"].Value)
	}

	if m["sec/op"].CI == "" {
		t.Error("no confidence interval; a published figure carries benchstat's own interval")
	}
}

// TestLookupRefusesToInvent is the rule the whole package exists for.
func TestLookupRefusesToInvent(t *testing.T) {
	stats, err := report.ParseCSV(read(t, "stat.csv"))
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}

	if _, ok := stats.Lookup(report.Key{Scenario: "nope", Mode: "warm", Impl: "ferry"}); ok {
		t.Fatal("Lookup found a benchmark that was never run")
	}
}

func TestParseCSVRefusesAnABTable(t *testing.T) {
	const ab = "goos: linux\n\n,base.txt,,head.txt,,vs base\n,sec/op,CI,sec/op,CI,P\nLoad/x-6,1e-06,2%,1.1e-06,2%,~\n"

	if _, err := report.ParseCSV(ab); err == nil {
		t.Fatal("ParseCSV accepted a two-file CSV. Its shape is different, and guessing at it " +
			"is the one thing this package must never do.")
	}
}

func TestResultsGolden(t *testing.T) {
	golden(t, "want_results.md", report.Results(input(t)))
}

func TestSummaryGolden(t *testing.T) {
	golden(t, "want_summary.md", report.Summary(input(t)))
}

// TestSummaryNeverRanksAgainstACaveatedFigure is the README's half of the rule
// the results file states in a footnote.
//
// A warm figure carrying a WarmCaveat measures a different job from the rest of
// its column: xload's YAML provider parses in its constructor, so its warm
// number excludes the file read and the parse every other row pays. Ranking
// ferry against it produced "ferry 10.00x slower" on yaml_small, a headline the
// footnote directly under it contradicted, and the headline is the part people
// quote. The summary must skip it and say that it did.
func TestSummaryNeverRanksAgainstACaveatedFigure(t *testing.T) {
	in := input(t)

	caveated := map[string][]string{}

	for _, sc := range in.Scenarios {
		for _, impl := range sc.Impls {
			if impl.WarmCaveat != "" {
				caveated[sc.Name] = append(caveated[sc.Name], impl.Name)
			}
		}
	}

	if len(caveated) == 0 {
		t.Skip("no caveated warm figure in the fixture, so there is nothing to exclude")
	}

	got := report.Summary(in)

	for scenario, names := range caveated {
		for _, name := range names {
			// The row for that scenario must not name it as the comparison.
			for line := range strings.SplitSeq(got, "\n") {
				if !strings.HasPrefix(line, "| `"+scenario+"`") {
					continue
				}

				if strings.Contains(line, "("+name+")") {
					t.Errorf("summary ranks ferry against %s in %s, whose warm figure is "+
						"marked not comparable:\n%s", name, scenario, line)
				}
			}

			if !strings.Contains(got, "`"+name+"` in `"+scenario+"`") {
				t.Errorf("summary drops %s from %s silently; it must say the row was left out",
					name, scenario)
			}
		}
	}
}

// TestChartGolden pins both themes against real captured benchmark output.
func TestChartGolden(t *testing.T) {
	golden(t, "want_chart_light.svg", report.Chart(input(t), report.LightTheme()))
	golden(t, "want_chart_dark.svg", report.Chart(input(t), report.DarkTheme()))
}

// TestChartIsSelfContained is the rule GitHub imposes and this package has to
// meet: a published SVG may not script, may not fetch, and may not depend on
// anything that is not in the file.
func TestChartIsSelfContained(t *testing.T) {
	for _, th := range []report.Theme{report.LightTheme(), report.DarkTheme()} {
		t.Run(th.Name, func(t *testing.T) {
			svg := report.Chart(input(t), th)

			// The namespace declaration is the one URL the file is allowed:
			// it identifies a vocabulary and is never fetched. Everything else
			// that looks like a URL is a reference to something outside the
			// file, which is what this test exists to refuse.
			if !strings.Contains(svg, `xmlns="http://www.w3.org/2000/svg"`) {
				t.Fatal("the chart carries no SVG namespace declaration")
			}

			body := strings.ReplaceAll(svg, `xmlns="http://www.w3.org/2000/svg"`, "")

			for _, banned := range []string{
				"<script", "<style", "<image", "<foreignObject", "<use",
				"href", "url(", "@import", "http://", "https://", "//",
			} {
				if strings.Contains(body, banned) {
					t.Errorf("the chart carries %q; a published SVG must be self-contained", banned)
				}
			}
		})
	}
}

// TestChartElementsAreTheSafeSet checks that the drawing code emitted only the
// element kinds it claims to, so that a future addition of something exotic is
// a test failure rather than a rendering surprise on GitHub.
func TestChartElementsAreTheSafeSet(t *testing.T) {
	svg := report.Chart(input(t), report.LightTheme())
	allowed := map[string]bool{"svg": true, "rect": true, "circle": true, "line": true, "text": true}

	for _, tag := range elementNames(svg) {
		if !allowed[tag] {
			t.Errorf("the chart emitted <%s>, which is outside the set this package vouches for", tag)
		}
	}
}

func elementNames(svg string) []string {
	var out []string

	for _, part := range strings.Split(svg, "<")[1:] {
		name := strings.TrimPrefix(part, "/")
		name = strings.FieldsFunc(name, func(r rune) bool {
			return r == ' ' || r == '>' || r == '/' || r == '\n'
		})[0]

		out = append(out, name)
	}

	return out
}

// TestChartShowsUnmeasuredAsWords is the rule that an absent bar reads as zero.
func TestChartShowsUnmeasuredAsWords(t *testing.T) {
	svg := report.Chart(input(t), report.LightTheme())

	// The fixture's second scenario was measured for nothing at all, so every
	// library in it has to appear with the words.
	if strings.Count(svg, ">not measured<") < 2 {
		t.Error("a library with no measurement is missing its \"not measured\" label, " +
			"so its row would read as a zero")
	}
}

// TestChartOrdersByTheMeasurement checks that ferry is not privileged by
// position: in the fixture's env_small scenario, stdlib is the fastest warm
// number and must therefore be the first row.
func TestChartOrdersByTheMeasurement(t *testing.T) {
	svg := report.Chart(input(t), report.LightTheme())

	first := strings.Index(svg, ">stdlib "+report.BaselineTag+"<")
	ferry := strings.Index(svg, ">ferry<")

	if first < 0 || ferry < 0 {
		t.Fatal("the chart is missing a library label")
	}

	if first > ferry {
		t.Error("ferry is drawn above stdlib, which was faster; rows must be ordered by the measurement")
	}
}

// TestResultsSaysNotMeasured checks the unmeasured scenario reached the file
// as words rather than as a zero.
func TestResultsSaysNotMeasured(t *testing.T) {
	got := report.Results(input(t))

	i := strings.Index(got, "## `no_such_scenario`")
	if i < 0 {
		t.Fatal("the unmeasured scenario is missing from the results file entirely")
	}

	row := got[i:]
	if j := strings.Index(row, "\n\n- "); j > 0 {
		row = row[:j]
	}

	if !strings.Contains(row, "not measured") {
		t.Errorf("the unmeasured scenario's row is %q, want every cell to say \"not measured\"", row)
	}

	if strings.Contains(row, "| 0 |") || strings.Contains(row, "0.00ns") {
		t.Errorf("a missing measurement rendered as a zero: %q", row)
	}
}

func TestReplaceSection(t *testing.T) {
	const before = "# ferry\n\nsome prose\n\n" +
		report.BeginMarker + "\nold and wrong\n" + report.EndMarker + "\n\ntrailing prose\n"

	got, err := report.ReplaceSection(before, "\nnew\n")
	if err != nil {
		t.Fatalf("ReplaceSection: %v", err)
	}

	for _, want := range []string{"# ferry", "some prose", "trailing prose", "new"} {
		if !strings.Contains(got, want) {
			t.Errorf("ReplaceSection lost %q", want)
		}
	}

	if strings.Contains(got, "old and wrong") {
		t.Error("ReplaceSection kept the old section")
	}

	// Twice is once: a workflow that runs on a schedule must not grow the file.
	again, err := report.ReplaceSection(got, "\nnew\n")
	if err != nil {
		t.Fatalf("ReplaceSection, second time: %v", err)
	}

	if again != got {
		t.Error("ReplaceSection is not idempotent, so a repeated run would grow the README")
	}
}

func TestReplaceSectionFailsWithoutMarkers(t *testing.T) {
	for name, readme := range map[string]string{
		"neither":      "# ferry\n\nno markers at all\n",
		"begin only":   "# ferry\n" + report.BeginMarker + "\n",
		"end only":     "# ferry\n" + report.EndMarker + "\n",
		"out of order": report.EndMarker + "\n" + report.BeginMarker + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := report.ReplaceSection(readme, "new")
			if err == nil {
				t.Fatalf("ReplaceSection accepted a README with %s marker(s) and returned %q", name, got)
			}

			if got != "" {
				t.Errorf("ReplaceSection returned %q alongside its error; it must return nothing to append", got)
			}
		})
	}
}

// summaryRow returns the cells of the summary's row for one scenario, split on
// the pipe, so that an assertion can name a column rather than search the whole
// line and match a figure that is legitimately in a different cell.
func summaryRow(t *testing.T, summary, scenario string) []string {
	t.Helper()

	for line := range strings.SplitSeq(summary, "\n") {
		if strings.HasPrefix(line, "| `"+scenario+"`") {
			return strings.Split(line, "|")
		}
	}

	t.Fatalf("the summary has no row for %s", scenario)

	return nil
}

// fastestOtherCell is the column the summary names a library in. Index 3 is the
// third cell of a row that opens with an empty field before the first pipe.
const fastestOtherCell = 3

// TestSummaryNeverRanksTheBaselineAsALibrary is the rule that stops the
// baseline being reported as the thing ferry lost to.
//
// The baseline is the same job with no mapping layer over it. No mapping
// library beats it, so every row loses to it by construction and "the fastest
// other library" resolving to it says only that the comparison had a floor in
// it. It is not thereby hidden: the row below asserts that the same table still
// publishes its raw figure and ferry's multiple over it.
func TestSummaryNeverRanksTheBaselineAsALibrary(t *testing.T) {
	in := input(t)
	got := report.Summary(in)

	found := 0

	for _, sc := range in.Scenarios {
		for _, impl := range sc.Impls {
			if !impl.Baseline {
				continue
			}

			found++

			cell := summaryRow(t, got, sc.Name)[fastestOtherCell]
			if strings.Contains(cell, "("+impl.Name+")") {
				t.Errorf("the summary names %s as the fastest other library in %s: %q\n"+
					"It is the baseline: nothing in the comparison can beat it, so ranking "+
					"against it is not a comparison.", impl.Name, sc.Name, cell)
			}
		}
	}

	if found == 0 {
		t.Fatal("no baseline in the fixture, so this test asserts nothing")
	}
}

// TestSummaryKeepsTheBaselineVisible is the other half of the same rule, and it
// exists so that the change above cannot become a way to drop a bad number.
func TestSummaryKeepsTheBaselineVisible(t *testing.T) {
	in := input(t)
	got := report.Summary(in)

	for _, sc := range in.Scenarios {
		for _, impl := range sc.Impls {
			if !impl.Baseline {
				continue
			}

			if _, ok := in.Stats.Lookup(report.Key{Scenario: sc.Name, Mode: "warm", Impl: impl.Name}); !ok {
				continue
			}

			row := strings.Join(summaryRow(t, got, sc.Name), "|")
			if !strings.Contains(row, "("+impl.Name+")") {
				t.Errorf("the summary's %s row does not carry the baseline's own figure: %q", sc.Name, row)
			}
		}
	}
}

// TestBaselineMultipleIsPublishedUnrounded checks that the multiple ferry is
// rendered at is the one the measurements give, at the same precision every
// other library's is rendered at.
//
// The README stopped computing it when the column was dropped, so the two halves
// of the rule are checked in the two places they now live: the results file
// carries the figure itself, and the README row carries both of its operands, so
// nothing about it is out of reach from either document.
func TestBaselineMultipleIsPublishedUnrounded(t *testing.T) {
	in := input(t)

	ferry, ok := in.Stats.Lookup(report.Key{Scenario: "env_small", Mode: "warm", Impl: "ferry"})
	if !ok {
		t.Fatal("the fixture has no warm ferry measurement in env_small")
	}

	base, ok := in.Stats.Lookup(report.Key{Scenario: "env_small", Mode: "warm", Impl: "stdlib"})
	if !ok {
		t.Fatal("the fixture has no warm baseline measurement in env_small")
	}

	want := fmt.Sprintf("%.2fx", ferry["sec/op"].Value/base["sec/op"].Value)
	if got := report.Results(in); !strings.Contains(got, want) {
		t.Errorf("the results file does not carry ferry's %s over the baseline; it is the figure "+
			"the reframing exists to publish, and it is not roundable", want)
	}

	row := strings.Join(summaryRow(t, report.Summary(in), "env_small"), "|")
	for _, operand := range []string{"2.75", "166ns"} {
		if !strings.Contains(row, operand) {
			t.Errorf("the README row lost %q, so ferry's multiple over the baseline is no longer "+
				"derivable from it: %q", operand, row)
		}
	}
}

// TestResultsGivesEveryLibraryABaselineMultiple checks the treatment is uniform:
// every row of every scenario table carries the column, so no row can be the one
// that is spared it.
func TestResultsGivesEveryLibraryABaselineMultiple(t *testing.T) {
	in := input(t)
	got := report.Results(in)

	for _, sc := range in.Scenarios {
		for _, impl := range sc.Impls {
			if _, ok := in.Stats.Lookup(report.Key{Scenario: sc.Name, Mode: "warm", Impl: impl.Name}); !ok {
				continue
			}

			cells := scenarioRow(t, got, sc.Name, impl)
			if strings.TrimSpace(cells[baselineColumn]) == "" {
				t.Errorf("%s has no baseline multiple in %s: %q", impl.Name, sc.Name, cells)
			}
		}
	}
}

// baselineColumn is the "warm x baseline" cell of a scenario table's row.
const baselineColumn = 4

func scenarioRow(t *testing.T, results, scenario string, impl report.ImplDoc) []string {
	t.Helper()

	section := results[strings.Index(results, "## `"+scenario+"`"):]

	name := impl.Name
	if impl.Baseline {
		name += " " + report.BaselineTag
	}

	for line := range strings.SplitSeq(section, "\n") {
		if strings.HasPrefix(line, "| "+name+" |") {
			return strings.Split(line, "|")
		}
	}

	t.Fatalf("%s has no row in %s's table", name, scenario)

	return nil
}
