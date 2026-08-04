package report_test

import (
	"flag"
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
		Impls: []report.ImplDoc{{Name: "ferry", Module: "github.com/onhotpath/ferry", Notes: "n/a"}},
	}}
}

// fixtureYAMLImpls carries the one WarmCaveat in the fixture.
func fixtureYAMLImpls() []report.ImplDoc {
	return []report.ImplDoc{
		{Name: "ferry", Module: "github.com/onhotpath/ferry", Notes: "Reads and parses on every load."},
		{Name: "koanf", Module: "github.com/knadh/koanf/v2", Notes: "Reads and parses on every load."},
		{Name: "viper", Module: "github.com/spf13/viper", Notes: "Reads and parses on every load."},
		{
			Name:   "xload",
			Module: "github.com/gojekfarm/xtools/xload",
			Notes:  "Its YAML provider parses in the constructor.",
			WarmCaveat: "xload's YAML provider reads and parses the file once, when the loader is " +
				"constructed, so this warm figure excludes the read and the parse every other row pays.",
		},
		{Name: "stdlib", Module: "", Notes: "Direct yaml.Unmarshal."},
	}
}

func fixtureImpls() []report.ImplDoc {
	return []report.ImplDoc{
		{Name: "ferry", Module: "github.com/onhotpath/ferry", Notes: "Compiles once, caches the schema."},
		{Name: "koanf", Module: "github.com/knadh/koanf/v2", Notes: "Reads the whole environ per load."},
		{Name: "viper", Module: "github.com/spf13/viper", Notes: "Needs every key registered."},
		{Name: "xload", Module: "github.com/gojekfarm/xtools/xload", Notes: "Reflects per call."},
		{Name: "go-envconfig", Module: "github.com/sethvargo/go-envconfig", Notes: "Reflects per call."},
		{Name: "kelseyhightower", Module: "github.com/kelseyhightower/envconfig", Notes: "Reflects per call."},
		{Name: "stdlib", Module: "", Notes: "Hand-rolled os.Getenv."},
	}
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

	first := strings.Index(svg, ">stdlib<")
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
