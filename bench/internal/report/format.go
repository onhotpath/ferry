package report

import (
	"fmt"
	"math"
	"strconv"
)

// notMeasured is what a cell with no measurement behind it renders as. It is
// the only thing this package will ever put where a number could go, and it is
// never a zero, a dash on its own, or a plausible-looking figure.
const notMeasured = "not measured"

// formatDuration renders a benchstat sec/op value, which is in seconds, as ns
// or µs or ms with three significant figures.
func formatDuration(seconds float64) string {
	ns := seconds * 1e9

	switch {
	case ns < 1000:
		return sig(ns) + "ns"
	case ns < 1e6:
		return sig(ns/1e3) + "µs"
	default:
		return sig(ns/1e6) + "ms"
	}
}

// formatBytes renders a B/op value.
func formatBytes(b float64) string {
	if b < 1024 {
		return strconv.FormatFloat(b, 'f', -1, 64) + " B"
	}

	return sig(b/1024) + " KiB"
}

// formatCount renders an allocs/op value.
func formatCount(n float64) string {
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// sig renders a number to three significant figures, without a trailing
// ".00" that says nothing.
func sig(v float64) string {
	switch {
	case v >= 100:
		return strconv.FormatFloat(v, 'f', 0, 64)
	case v >= 10:
		return strconv.FormatFloat(v, 'f', 1, 64)
	default:
		return strconv.FormatFloat(v, 'f', 2, 64)
	}
}

// withCI appends benchstat's interval, and says so in words where benchstat
// could not compute one rather than dropping the caveat.
func withCI(text, ci string) string {
	switch ci {
	case "":
		return text
	case "∞":
		return text + " (no CI: too few samples)"
	default:
		return text + " ±" + ci
	}
}

// cell renders one unit's metric, or the not-measured marker.
func cell(m map[string]Metric, unit string) string {
	got, ok := m[unit]
	if !ok {
		return notMeasured
	}

	switch unit {
	case unitSec:
		return withCI(formatDuration(got.Value), got.CI)
	case unitBytes:
		return formatBytes(got.Value)
	case unitAllocs:
		return formatCount(got.Value)
	default:
		return strconv.FormatFloat(got.Value, 'g', -1, 64)
	}
}

// ratio renders how many times a is b, or the not-measured marker when either
// side is missing. It is the one derived figure this package computes, and it
// is computed from two measured numbers rather than remembered.
func ratio(a, b float64) string {
	if b == 0 || math.IsNaN(a) || math.IsNaN(b) {
		return notMeasured
	}

	return fmt.Sprintf("%.2fx", a/b)
}

// The three units the harness reports, spelled once.
const (
	unitSec    = "sec/op"
	unitBytes  = "B/op"
	unitAllocs = "allocs/op"
)
