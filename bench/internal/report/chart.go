package report

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// This file draws the chart, and it draws it from the same parsed benchstat
// CSV the markdown tables are rendered from. There is one source of truth for
// every number on the page, so a chart that disagreed with the table beside it
// would be a bug rather than a possibility.
//
// # Why a dot plot and not a bar chart, for the time panel
//
// The measurements span four orders of magnitude, from 176ns to 2.4ms, which
// makes a linear axis an unreadable smear and a log axis necessary. A bar on a
// log axis is the dishonest shape: a bar's length reads as proportional to its
// value and on a log axis it is not, so a bar twice as long is a hundred times
// the number. A dot carries a position and claims nothing about the distance
// back to an origin, which is exactly what a log axis can support.
//
// The allocation panel is linear and starts at zero, because allocations span
// zero to a few thousand and a bar from a true zero is both readable and
// honest there.
//
// # What is not done here
//
// No colour identifies ferry, no ordering favours it, and nothing is averaged.
// Rows are sorted by the measurement. Cold and warm are two marks that never
// merge. A library that was not measured keeps its row, with the words rather
// than a bar of length zero, because an absent bar reads as a zero.

// Theme is the colour set for one of GitHub's two renderings.
//
// Two files are emitted and referenced from a <picture> with a
// prefers-color-scheme media query, which GitHub supports in markdown. The
// alternative - one file with an @media rule inside it - depends on the
// sanitiser and the image proxy leaving a <style> element alone, which is not
// something to bet a published chart on.
type Theme struct {
	Name    string
	Bg      string
	Text    string
	Muted   string
	Grid    string
	Axis    string
	Cold    string
	Warm    string
	Absent  string
	BarCold string
	BarWarm string
}

// LightTheme is the rendering for GitHub's light mode.
func LightTheme() Theme {
	return Theme{
		Name: "light", Bg: "#ffffff", Text: "#1f2328", Muted: "#59636e",
		Grid: "#d1d9e0", Axis: "#8c959f",
		Cold: "#8250df", Warm: "#0969da", Absent: "#8c959f",
		BarCold: "#c9a7f5", BarWarm: "#54aeff",
	}
}

// DarkTheme is the rendering for GitHub's dark mode.
func DarkTheme() Theme {
	return Theme{
		Name: "dark", Bg: "#0d1117", Text: "#e6edf3", Muted: "#9198a1",
		Grid: "#2a313c", Axis: "#6e7681",
		Cold: "#c297ff", Warm: "#6cb6ff", Absent: "#6e7681",
		BarCold: "#5a3a92", BarWarm: "#1a5a9e",
	}
}

// The chart's geometry, in user units. One place, so that changing the row
// height cannot leave a label behind.
const (
	chartWidth   = 1180.0
	labelWidth   = 168.0
	panelGap     = 44.0
	sideMargin   = 16.0
	rowHeight    = 17.0
	groupHead    = 26.0
	groupGap     = 12.0
	headerHeight = 118.0
	footerHeight = 46.0
	axisHeight   = 30.0
	markRadius   = 3.6
	capHalf      = 3.0
)

// chartRow is one library's row inside one scenario.
type chartRow struct {
	Impl    string
	IsFerry bool

	// Measured is false when the CSV carries no benchmark for this pairing at
	// all, which is a labelled gap rather than a bar.
	Measured bool

	ColdSec, WarmSec       float64
	ColdSecCI, WarmSecCI   float64
	ColdAlloc, WarmAlloc   float64
	HasColdCI, HasWarmCI   bool
	HasAllocs, HasDuration bool
}

// chartGroup is one scenario's block of rows.
type chartGroup struct {
	Scenario string
	Rows     []chartRow
}

// Chart renders the whole figure for one theme, as a self-contained SVG.
//
// Self-contained means it: no <script>, no <style>, no <image>, no external
// font, no href of any kind. Every colour and size is an inline presentation
// attribute, and the font is a generic stack with an explicit size, so nothing
// about the rendering depends on what the viewer happens to have installed.
func Chart(in *Input, th Theme) string {
	groups := chartGroups(in)
	height := chartHeight(groups)

	var b strings.Builder

	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" `+
		`viewBox="0 0 %.0f %.0f" role="img" aria-label="%s">`+"\n",
		chartWidth, height, chartWidth, height, chartAriaLabel(in))
	fmt.Fprintf(&b, `<rect x="0" y="0" width="%.0f" height="%.0f" fill="%s"/>`+"\n",
		chartWidth, height, th.Bg)

	writeChartHeader(&b, in, th)

	sec := newLogScale(groups, panelLeft(0), panelWidth())
	alloc := newLinScale(groups, panelLeft(1), panelWidth())

	writeAxis(&b, th, sec, height, "time per operation (log scale)")
	writeAxis(&b, th, alloc, height, "allocations per operation (linear, from zero)")

	writeGroups(&b, groups, th, sec, alloc)
	writeChartFooter(&b, th, height)

	b.WriteString("</svg>\n")

	return b.String()
}

func chartAriaLabel(in *Input) string {
	return fmt.Sprintf("ferry against %d config libraries: time and allocations per load, "+
		"cold and warm, over %d scenarios", len(implOrder(in)), len(in.Scenarios))
}

func panelWidth() float64 {
	return (chartWidth - labelWidth - panelGap - 2*sideMargin) / 2
}

func panelLeft(i int) float64 {
	return sideMargin + labelWidth + float64(i)*(panelWidth()+panelGap)
}

func chartHeight(groups []chartGroup) float64 {
	h := headerHeight + axisHeight + footerHeight

	for _, g := range groups {
		h += groupHead + float64(len(g.Rows))*rowHeight + groupGap
	}

	return h
}

// implOrder is every library any scenario mentions, so that a scenario which
// did not measure one still has a row for it.
func implOrder(in *Input) []string {
	seen := map[string]bool{}

	var out []string

	for _, sc := range in.Scenarios {
		for _, impl := range sc.Impls {
			if !seen[impl.Name] {
				seen[impl.Name] = true

				out = append(out, impl.Name)
			}
		}
	}

	return out
}

// chartGroups builds every row from the parsed CSV, and sorts each scenario by
// its warm time. The ordering is the measurement's, not the author's: ferry
// appears wherever its number puts it.
func chartGroups(in *Input) []chartGroup {
	all := implOrder(in)
	out := make([]chartGroup, 0, len(in.Scenarios))

	for _, sc := range in.Scenarios {
		rows := make([]chartRow, 0, len(all))
		for _, name := range all {
			rows = append(rows, buildRow(in, sc.Name, name))
		}

		sortRows(rows)
		out = append(out, chartGroup{Scenario: sc.Name, Rows: rows})
	}

	return out
}

func sortRows(rows []chartRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Measured != b.Measured {
			return a.Measured
		}

		if !a.Measured {
			return a.Impl < b.Impl
		}

		return a.WarmSec < b.WarmSec
	})
}

func buildRow(in *Input, scenario, impl string) chartRow {
	r := chartRow{Impl: impl, IsFerry: impl == ferryImpl}

	cold, coldOK := in.Stats.Lookup(Key{Scenario: scenario, Mode: "cold", Impl: impl})
	warm, warmOK := in.Stats.Lookup(Key{Scenario: scenario, Mode: "warm", Impl: impl})

	if !coldOK || !warmOK {
		return r
	}

	r.Measured = true
	r.ColdSec, r.ColdSecCI, r.HasColdCI = metricOf(cold, unitSec)
	r.WarmSec, r.WarmSecCI, r.HasWarmCI = metricOf(warm, unitSec)
	r.HasDuration = r.ColdSec > 0 && r.WarmSec > 0

	ca, _, coldHas := metricOf(cold, unitAllocs)
	wa, _, warmHas := metricOf(warm, unitAllocs)
	r.ColdAlloc, r.WarmAlloc = ca, wa
	r.HasAllocs = coldHas || warmHas || hasUnit(cold, unitAllocs)

	return r
}

func hasUnit(m map[string]Metric, unit string) bool {
	_, ok := m[unit]

	return ok
}

// metricOf returns the value, the confidence interval as a fraction, and
// whether benchstat gave an interval at all. A row benchstat could not put an
// interval on gets no error bar rather than a zero-width one, because a
// zero-width bar claims a precision that was never measured.
func metricOf(m map[string]Metric, unit string) (value, ci float64, hasCI bool) {
	got, ok := m[unit]
	if !ok {
		return 0, 0, false
	}

	pct := strings.TrimSpace(strings.TrimSuffix(got.CI, "%"))
	if pct == got.CI || pct == "" {
		return got.Value, 0, false
	}

	f, err := strconv.ParseFloat(pct, 64)
	if err != nil {
		return got.Value, 0, false
	}

	return got.Value, f / 100, true
}
