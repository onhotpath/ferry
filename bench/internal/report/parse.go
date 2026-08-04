// Package report turns benchstat's output into the markdown that is published.
//
// It exists because the alternative is a shell pipeline of sed, and a number
// that reaches a README through sed is a number nobody can prove came from a
// measurement. Everything here is deterministic, takes its input from files a
// benchmark run produced, and is covered by a test with a fixture of real
// captured output.
//
// Nothing in this package can invent a number. There is no default, no
// fallback and no placeholder: a metric the input does not carry renders as
// "not measured" and never as an estimate.
package report

import (
	"encoding/csv"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Metric is one measured number, with the confidence interval benchstat
// computed for it.
type Metric struct {
	// Value is in benchstat's own unit for the row: seconds for sec/op,
	// bytes for B/op, a count for allocs/op.
	Value float64

	// CI is benchstat's interval as it printed it - "2%", or the infinity
	// sign when there were too few samples for one. It is carried as the
	// string benchstat produced rather than re-derived, so that the published
	// figure and benchstat's own output cannot disagree.
	CI string
}

// Stats is one benchstat CSV, parsed.
type Stats struct {
	// Header is the goos/goarch/pkg/cpu block benchstat echoes from the
	// benchmark output, in order and verbatim.
	Header []string

	// Units is every unit that appeared, in the order benchstat emitted them.
	Units []string

	// Rows is full benchmark name to unit to metric.
	Rows map[string]map[string]Metric

	// Geomean is the geometric mean benchstat computed per unit. A unit whose
	// geomean benchstat declined to compute - it does that for B/op when a
	// row is zero - is absent rather than zero.
	Geomean map[string]Metric
}

// ErrColumns is returned for a CSV carrying more than one input file's
// results.
//
// benchstat's A/B output has a different shape entirely, and guessing at it
// would be the one thing this package must never do. The comparison path is
// the pull-request label, which posts benchstat's own A/B text rather than
// re-rendering it.
var ErrColumns = errors.New("report: the CSV carries more than one input file")

// ParseCSV reads `benchstat -format csv` output.
func ParseCSV(in string) (*Stats, error) {
	st := &Stats{Rows: map[string]map[string]Metric{}, Geomean: map[string]Metric{}}

	var unit string

	for i, line := range strings.Split(in, "\n") {
		next, err := st.consume(line, unit)
		if err != nil {
			return nil, fmt.Errorf("report: line %d: %w", i+1, err)
		}

		unit = next
	}

	if len(st.Rows) == 0 {
		return nil, errors.New("report: the CSV carried no benchmark rows")
	}

	return st, nil
}

// consume folds one line in and returns the unit in force after it.
func (s *Stats) consume(line, unit string) (string, error) {
	switch {
	case strings.TrimSpace(line) == "":
		// A blank line ends a unit's block. benchstat writes one between
		// every pair, so this is what keeps two units' rows apart.
		return "", nil

	case !strings.HasPrefix(line, ","):
		return s.consumeRow(line, unit)

	default:
		return s.consumeHeader(line, unit)
	}
}

// consumeHeader handles the two comma-leading lines benchstat writes per
// block: the input-file names, and the unit.
func (s *Stats) consumeHeader(line, unit string) (string, error) {
	rec, err := splitCSV(line)
	if err != nil {
		return "", err
	}

	// ",<unit>,CI" is the unit line; anything else leading with a comma is
	// the file-name line, which for a single input is ",<file>,".
	if len(rec) == 3 && rec[2] == "CI" {
		s.Units = append(s.Units, rec[1])

		return rec[1], nil
	}

	if len(rec) > 3 {
		return "", ErrColumns
	}

	return unit, nil
}

// consumeRow handles a data row, the geomean row, and the echoed header block.
func (s *Stats) consumeRow(line, unit string) (string, error) {
	// Before the first block, benchstat echoes goos/goarch/pkg/cpu verbatim.
	if unit == "" {
		if strings.Contains(line, ": ") {
			s.Header = append(s.Header, line)
		}

		return "", nil
	}

	rec, err := splitCSV(line)
	if err != nil {
		return "", err
	}

	if len(rec) < 2 {
		return unit, nil
	}

	// benchstat leaves the value empty where it declined to compute one.
	if strings.TrimSpace(rec[1]) == "" {
		return unit, nil
	}

	v, err := strconv.ParseFloat(rec[1], 64)
	if err != nil {
		return "", fmt.Errorf("report: %q is not a number: %w", rec[1], err)
	}

	m := Metric{Value: v}
	if len(rec) > 2 {
		m.CI = rec[2]
	}

	s.record(rec[0], unit, m)

	return unit, nil
}

func (s *Stats) record(name, unit string, m Metric) {
	if name == "geomean" {
		s.Geomean[unit] = m

		return
	}

	if s.Rows[name] == nil {
		s.Rows[name] = map[string]Metric{}
	}

	s.Rows[name][unit] = m
}

func splitCSV(line string) ([]string, error) {
	r := csv.NewReader(strings.NewReader(line))
	r.FieldsPerRecord = -1

	rec, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("report: %q is not a CSV record: %w", line, err)
	}

	return rec, nil
}

// Key identifies one cell of the published table.
type Key struct {
	// Scenario and Impl are the harness's own names; Mode is "cold" or "warm".
	Scenario string
	Mode     string
	Impl     string
}

// Lookup finds the metrics for one cell, given the benchmark-name shape the
// harness produces:
//
//	Load/<scenario>/<mode>/<impl>-<gomaxprocs>
//
// The second return is false when that benchmark is not in the input, which is
// how a library that was not run in a scenario stays "not measured" rather
// than becoming a zero.
func (s *Stats) Lookup(k Key) (map[string]Metric, bool) {
	want := "Load/" + k.Scenario + "/" + k.Mode + "/" + k.Impl

	for name, m := range s.Rows {
		if trimProcs(name) == want {
			return m, true
		}
	}

	return nil, false
}

// Named finds a benchmark by its name with the -N suffix removed, for the
// benchmarks that are not part of the scenario grid.
func (s *Stats) Named(name string) (map[string]Metric, bool) {
	for got, m := range s.Rows {
		if trimProcs(got) == name {
			return m, true
		}
	}

	return nil, false
}

// trimProcs removes the -N GOMAXPROCS suffix the testing package appends.
func trimProcs(name string) string {
	i := strings.LastIndex(name, "-")
	if i < 0 {
		return name
	}

	if _, err := strconv.Atoi(name[i+1:]); err != nil {
		return name
	}

	return name[:i]
}
