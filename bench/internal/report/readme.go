package report

import (
	"errors"
	"fmt"
	"strings"
)

// The two markers the README section lives between. They are matched
// literally, and nothing outside them is ever touched.
const (
	BeginMarker = "<!-- ferry:perf:begin -->"
	EndMarker   = "<!-- ferry:perf:end -->"
)

// ErrNoMarkers is returned when the README does not carry both markers, in
// that order.
//
// It is an error and not a fallback on purpose. Appending to the end of a
// README because the markers could not be found is how a generated section
// ends up duplicated three times in a file nobody reviewed, and a caller that
// wants to proceed without patching the README has to say so out loud.
var ErrNoMarkers = errors.New("report: the README is missing the ferry:perf markers")

// ReplaceSection puts section between the markers in readme and returns the
// whole file.
func ReplaceSection(readme, section string) (string, error) {
	begin := strings.Index(readme, BeginMarker)
	end := strings.Index(readme, EndMarker)

	if begin < 0 || end < 0 || end < begin {
		return "", fmt.Errorf("%w: expected %s ... %s", ErrNoMarkers, BeginMarker, EndMarker)
	}

	var b strings.Builder

	b.WriteString(readme[:begin])
	b.WriteString(BeginMarker)
	b.WriteString("\n")
	b.WriteString(strings.Trim(section, "\n"))
	b.WriteString("\n")
	b.WriteString(readme[end:])

	return b.String(), nil
}

// Summary renders the README section: a small table and a link, and nothing
// that would go stale differently from the results file.
//
// The table is ferry's warm number against the fastest other library measured
// in the same scenario, because that is the comparison a reader wants and the
// one it would be dishonest to leave out.
func Summary(in *Input) string {
	var b strings.Builder

	fmt.Fprint(&b, "\nMeasured, not claimed.\n")
	fmt.Fprint(&b, "The table is machine-generated from a benchmark run; the harness refuses to run at\n")
	fmt.Fprint(&b, "all unless every library produces the identical struct from the identical source.\n\n")
	fmt.Fprint(&b, "| scenario | ferry (warm) | fastest other | |\n")
	fmt.Fprint(&b, "| --- | --- | --- | --- |\n")

	excluded := make(map[string][]string, len(in.Scenarios))

	for _, sc := range in.Scenarios {
		excluded[sc.Name] = writeSummaryRow(&b, in, sc)
	}

	writeSummaryExclusions(&b, excluded, in.Scenarios)
	writeSummaryChart(&b, in)

	fmt.Fprintf(&b, "\nFull results, the machine, the toolchain, the competitor versions, what each library\n")
	fmt.Fprintf(&b, "actually did and what was not measured: [%s](%s).\n",
		value(in.Meta.ResultsLink), value(in.Meta.ResultsLink))
	fmt.Fprintf(&b, "\nRun on %s, `-count %s`, `-benchtime %s`, Go %s.\n",
		value(in.Meta.Runner), value(in.Meta.Count), value(in.Meta.Benchtime), value(in.Meta.GoVersion))

	return b.String()
}

// writeSummaryChart references the two SVGs the same run produced, through a
// <picture> so that GitHub picks the one that suits the reader's theme.
func writeSummaryChart(b *strings.Builder, in *Input) {
	if in.Meta.ChartLightLink == "" || in.Meta.ChartDarkLink == "" {
		return
	}

	fmt.Fprintf(b, "\n<picture>\n  <source media=\"(prefers-color-scheme: dark)\" srcset=\"%s\">\n"+
		"  <img alt=\"%s\" src=\"%s\">\n</picture>\n",
		in.Meta.ChartDarkLink, chartAlt, in.Meta.ChartLightLink)
}

func writeSummaryRow(b *strings.Builder, in *Input, sc ScenarioDoc) (excluded []string) {
	ferry, ferryOK := warmSeconds(in, sc.Name, ferryImpl)

	best, bestName, excluded := fastestOther(in, sc)

	switch {
	case !ferryOK || bestName == "":
		fmt.Fprintf(b, "| `%s` | %s | %s | |\n", sc.Name, notMeasured, notMeasured)
	case best < ferry:
		fmt.Fprintf(b, "| `%s` | %s | %s (%s) | ferry %s slower |\n",
			sc.Name, formatDuration(ferry), formatDuration(best), bestName, ratio(ferry, best))
	default:
		fmt.Fprintf(b, "| `%s` | %s | %s (%s) | ferry %s faster |\n",
			sc.Name, formatDuration(ferry), formatDuration(best), bestName, ratio(best, ferry))
	}

	return excluded
}

// writeSummaryExclusions names every library left out of the comparison above,
// with the scenario it was left out of.
//
// Leaving one out silently would be the same defect as omitting a library that
// lost: the table would read as the whole field when it is not.
func writeSummaryExclusions(b *strings.Builder, byScenario map[string][]string, order []ScenarioDoc) {
	var lines []string

	for _, sc := range order {
		names := byScenario[sc.Name]
		if len(names) == 0 {
			continue
		}

		lines = append(lines, fmt.Sprintf("`%s` in `%s`", strings.Join(names, "`, `"), sc.Name))
	}

	if len(lines) == 0 {
		return
	}

	fmt.Fprintf(b, "\nLeft out of the comparison above because its warm figure measures a different job:\n"+
		"%s.\nThe results file says what the difference is, and gives the column where those\n"+
		"rows are comparable.\n", strings.Join(lines, "; "))
}

// fastestOther is the quickest warm measurement in a scenario that is not
// ferry's, with its name. An empty name means nothing else was measured.
//
// A row carrying a WarmCaveat is skipped, and skipping it is the whole point.
// The caveat says that warm figure measures a different job from the rest of
// the column - xload's YAML provider parses in its constructor, so its warm
// number excludes the file read and the parse every other row pays. Ranking
// ferry against it produces a headline the results file's own footnote
// contradicts, and the headline is the part people quote.
//
// The excluded names come back so the summary can say who is missing from the
// comparison rather than dropping them silently.
func fastestOther(in *Input, sc ScenarioDoc) (best float64, name string, excluded []string) {
	for _, impl := range sc.Impls {
		if impl.Name == ferryImpl {
			continue
		}

		got, ok := warmSeconds(in, sc.Name, impl.Name)
		if !ok {
			continue
		}

		if impl.WarmCaveat != "" {
			excluded = append(excluded, impl.Name)

			continue
		}

		if name == "" || got < best {
			best, name = got, impl.Name
		}
	}

	return best, name, excluded
}
