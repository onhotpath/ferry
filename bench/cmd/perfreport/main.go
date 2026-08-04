// Command perfreport turns a benchmark run into the two files that are
// published: the results file under docs/perf/, and the marked section of the
// README.
//
// It is a program rather than a shell pipeline because a number that reaches a
// README through sed is a number nobody can prove came from a measurement.
// Every figure it writes is read out of the benchstat CSV it is handed. There
// is no default, no placeholder and no fallback: a measurement that is not in
// the input renders as "not measured".
//
// It imports the benchmark package, and that is deliberate twice over. It is
// where the scenario descriptions and the per-library notes live, so they
// cannot drift from the code that was measured; and linking it puts every
// competitor module into this binary's own build info, which is where the
// published version table comes from - the versions that were linked, rather
// than the versions a go.mod asked for.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	gopath "path"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/onhotpath/ferry/bench"
	"github.com/onhotpath/ferry/bench/internal/report"
)

// published is the mode of the two files this command writes. Both are
// committed to a pull request and read by everyone, so both are world-readable.
const published = 0o644

func main() {
	// flag.Parse belongs here rather than in the helper that declares the
	// flags: a package-level parse in a helper is a side effect at a distance,
	// and a test that imports this package would trip over it.
	o := parseFlags()
	flag.Parse()

	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, "perfreport:", err)
		os.Exit(1)
	}
}

// opts is the command line, kept in one struct so that run stays readable and
// so that adding an input is one edit rather than three.
type opts struct {
	csv       string
	stat      string
	raw       string
	results   string
	readme    string
	runner    string
	ferryRev  string
	benchRev  string
	count     string
	benchtime string
	command   string
	generated string
	linkDir   string
	allowNoMk bool
}

// chartLight and chartDark are the two files the chart is written to, beside
// the results file. Two rather than one because a <picture> with a
// prefers-color-scheme source is how GitHub markdown picks between a light and
// a dark image, and it is the only mechanism that does not depend on the
// sanitiser leaving a <style> element inside the SVG alone.
const (
	chartLight = "perf-light.svg"
	chartDark  = "perf-dark.svg"
)

func parseFlags() *opts {
	o := &opts{}

	flag.StringVar(&o.csv, "csv", "", "path to `benchstat -format csv` output (required)")
	flag.StringVar(&o.stat, "stat", "", "path to benchstat text output, embedded verbatim (required)")
	flag.StringVar(&o.raw, "raw", "", "path to raw `go test -bench` output, embedded verbatim (required)")
	flag.StringVar(&o.results, "results", "", "results file to write (required)")
	flag.StringVar(&o.readme, "readme", "", "README to patch between the ferry:perf markers")
	flag.StringVar(&o.runner, "runner", "", "what the run executed on, e.g. ubuntu-latest (GitHub-hosted)")
	flag.StringVar(&o.ferryRev, "ferry-rev", "", "the ferry commit the harness was built against")
	flag.StringVar(&o.benchRev, "harness-rev", "", "the harness commit")
	flag.StringVar(&o.count, "count", "", "the -count the run used")
	flag.StringVar(&o.benchtime, "benchtime", "", "the -benchtime the run used")
	flag.StringVar(&o.command, "command", "", "the exact command the numbers came from")
	flag.StringVar(&o.generated, "generated", "", "timestamp to stamp; defaults to now in RFC 3339")
	flag.StringVar(&o.linkDir, "link-dir", "",
		"repository-relative `directory` the published files live in, e.g. docs/perf. "+
			"This is how the README spells the links, which is not the same string as -results.")
	flag.BoolVar(&o.allowNoMk, "allow-missing-markers", false,
		"write the results file and warn, rather than failing, when the README has no markers")

	return o
}

func run(o *opts) error {
	if err := o.require(); err != nil {
		return err
	}

	in, err := o.input()
	if err != nil {
		return err
	}

	if err := os.WriteFile(o.results, []byte(report.Results(&in)), published); err != nil {
		return fmt.Errorf("writing %s: %w", o.results, err)
	}

	fmt.Println("perfreport: wrote", o.results)

	if err := o.writeCharts(&in); err != nil {
		return err
	}

	return o.patchREADME(&in)
}

func (o *opts) require() error {
	for _, f := range []struct{ name, value string }{
		{"-csv", o.csv}, {"-stat", o.stat}, {"-raw", o.raw}, {"-results", o.results},
	} {
		if f.value == "" {
			return fmt.Errorf("%s is required", f.name)
		}
	}

	return nil
}

func (o *opts) input() (report.Input, error) {
	csvText, err := read(o.csv)
	if err != nil {
		return report.Input{}, err
	}

	statText, err := read(o.stat)
	if err != nil {
		return report.Input{}, err
	}

	rawText, err := read(o.raw)
	if err != nil {
		return report.Input{}, err
	}

	stats, err := report.ParseCSV(csvText)
	if err != nil {
		return report.Input{}, err
	}

	return report.Input{
		Meta:          o.meta(),
		Stats:         stats,
		Scenarios:     scenarioDocs(),
		Absences:      absenceDocs(),
		BenchstatText: statText,
		RawBench:      rawText,
	}, nil
}

func (o *opts) meta() report.Meta {
	generated := o.generated
	if generated == "" {
		generated = time.Now().UTC().Format(time.RFC3339)
	}

	return report.Meta{
		GoVersion:       runtime.Version(),
		GOOS:            runtime.GOOS,
		GOARCH:          runtime.GOARCH,
		NumCPU:          runtime.NumCPU(),
		Runner:          o.runner,
		FerryRevision:   o.ferryRev,
		HarnessRevision: o.benchRev,
		Count:           o.count,
		Benchtime:       o.benchtime,
		Command:         o.command,
		Generated:       generated,
		Modules:         linkedModules(),
		ResultsLink:     o.link(filepath.Base(o.results)),
		ChartLightLink:  o.link(chartLight),
		ChartDarkLink:   o.link(chartDark),
	}
}

// link spells a published file the way the README has to refer to it.
//
// Without -link-dir there is nothing honest to write, so it writes nothing and
// the renderer says "not recorded" rather than emitting a path that resolves
// on the machine that ran the command and nowhere else.
func (o *opts) link(name string) string {
	if o.linkDir == "" {
		return ""
	}

	return gopath.Join(o.linkDir, name)
}

// writeCharts emits both themes beside the results file.
func (o *opts) writeCharts(in *report.Input) error {
	dir := filepath.Dir(o.results)

	themes := []struct {
		name  string
		theme report.Theme
	}{
		{chartLight, report.LightTheme()},
		{chartDark, report.DarkTheme()},
	}

	for i := range themes {
		out := filepath.Join(dir, themes[i].name)

		if err := os.WriteFile(out, []byte(report.Chart(in, themes[i].theme)), published); err != nil {
			return fmt.Errorf("writing %s: %w", out, err)
		}

		fmt.Println("perfreport: wrote", out)
	}

	return nil
}

func (o *opts) patchREADME(in *report.Input) error {
	if o.readme == "" {
		return nil
	}

	current, err := read(o.readme)
	if err != nil {
		return err
	}

	patched, err := report.ReplaceSection(current, report.Summary(in))
	if err != nil {
		if o.allowNoMk && errors.Is(err, report.ErrNoMarkers) {
			fmt.Fprintf(os.Stderr,
				"perfreport: WARNING: %s carries no %s marker, so its section was not updated.\n",
				o.readme, report.BeginMarker)

			return nil
		}

		return err
	}

	if err := os.WriteFile(o.readme, []byte(patched), published); err != nil {
		return fmt.Errorf("writing %s: %w", o.readme, err)
	}

	fmt.Println("perfreport: patched", o.readme)

	return nil
}

// linkedModules is the version table, read from this binary's own build info.
//
// It is restricted to the modules the harness actually compares, so that a
// transitive dependency of viper's does not end up published as though it were
// a competitor.
func linkedModules() map[string]string {
	want := comparedModules()
	out := map[string]string{}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return out
	}

	for _, dep := range info.Deps {
		if want[dep.Path] {
			out[dep.Path] = dep.Version
		}
	}

	return out
}

// comparedModules is the set of import paths any scenario attributes a column
// to.
func comparedModules() map[string]bool {
	want := map[string]bool{}

	for _, sc := range bench.Scenarios() {
		for _, impl := range sc.Impls {
			if impl.Module != "" {
				want[impl.Module] = true
			}
		}
	}

	return want
}

func scenarioDocs() []report.ScenarioDoc {
	scs := bench.Scenarios()
	out := make([]report.ScenarioDoc, 0, len(scs))

	for _, sc := range scs {
		impls := make([]report.ImplDoc, 0, len(sc.Impls))
		for _, impl := range sc.Impls {
			impls = append(impls, report.ImplDoc{
				Name:       impl.Name,
				Module:     impl.Module,
				Notes:      impl.Notes,
				Baseline:   impl.Baseline,
				WarmCaveat: impl.WarmCaveat,
			})
		}

		out = append(out, report.ScenarioDoc{Name: sc.Name, What: sc.What, Impls: impls})
	}

	return out
}

func absenceDocs() []report.AbsenceDoc {
	abs := bench.Absences()
	out := make([]report.AbsenceDoc, 0, len(abs))

	for _, a := range abs {
		out = append(out, report.AbsenceDoc{Library: a.Library, Scenario: a.Scenario, Why: a.Why})
	}

	return out
}

func read(path string) (string, error) {
	b, err := os.ReadFile(path) //nolint:gosec // every path here is an operator's own argument
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}

	return string(b), nil
}
