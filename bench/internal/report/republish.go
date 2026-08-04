package report

// This file is the inverse of render.go, and it exists because a correction to
// a note in the harness is not a measurement.
//
// Re-rendering a published file used to mean reconstructing every -runner,
// -ferry-rev, -harness-rev, -count, -benchtime, -command and -generated the
// last publish used, by hand, from the file itself. Everything it needs is
// already in the file - that is the property the embedded appendices were put
// there for - so it is recovered here rather than restated, and recovered in Go
// with a golden test rather than in a shell, for the same reason the renderer
// is a program.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Published is a results file this package already wrote, taken apart again.
type Published struct {
	// Meta is the provenance the file was stamped with, less the three links.
	//
	// The links say where the published files live in the repository, which is
	// a fact about the layout rather than about the run, so they are not
	// recovered and the caller supplies them exactly as it does when
	// publishing.
	Meta Meta

	// BenchstatText and RawBench are the two embedded appendices, byte for
	// byte as the file carries them.
	BenchstatText string
	RawBench      string
}

// ErrNotPublished is returned for a file this package did not write, or for one
// whose provenance table or appendices have been edited away.
var ErrNotPublished = errors.New("report: not a results file this package wrote")

// Extract takes a published results file apart.
//
// It returns the two verbatim appendices and the provenance, which is
// everything a re-render needs that a re-render cannot measure.
func Extract(results string) (*Published, error) {
	stat, err := fenced(results, benchstatHeading)
	if err != nil {
		return nil, err
	}

	raw, err := fenced(results, rawHeading)
	if err != nil {
		return nil, err
	}

	meta, err := recoverMeta(results)
	if err != nil {
		return nil, err
	}

	return &Published{Meta: meta, BenchstatText: stat, RawBench: raw}, nil
}

// recoverMeta reads the provenance table back off the file.
//
// Every field here describes the measurement: the runner, both revisions, the
// flags, the command, the toolchain, the machine and the moment the numbers
// were produced. A re-render measures nothing, so taking any of them from the
// runtime it happens to run on, or from a workflow input somebody retyped, is
// exactly how they drift.
//
// Generated is carried forward unchanged for the same reason. It dates the
// numbers, and stamping it with the time a note was corrected would make the
// one field that dates the measurement date the prose instead.
func recoverMeta(doc string) (Meta, error) {
	at := strings.Index(doc, environmentHeading)
	if at < 0 {
		return Meta{}, fmt.Errorf("%w: no %q section", ErrNotPublished, environmentHeading)
	}

	// The timestamp is stamped above the provenance table, and it is looked for
	// only there. Prose further down the file opens a sentence with the same
	// word, and matching that would recover half a paragraph as a date.
	generated, err := generatedAt(doc[:at])
	if err != nil {
		return Meta{}, err
	}

	env := sectionFrom(doc, at+len(environmentHeading))

	command, err := fenced(env, commandIntro)
	if err != nil {
		return Meta{}, err
	}

	rows := tableRows(env)
	goos, goarch, _ := strings.Cut(rowValue(rows, labelPlatform), " / ")

	return Meta{
		GoVersion:       unrecorded(rowValue(rows, labelToolchain)),
		GOOS:            unrecorded(goos),
		GOARCH:          unrecorded(goarch),
		NumCPU:          gomaxprocs(rowValue(rows, labelCPUs)),
		Runner:          unrecorded(rowValue(rows, labelRunner)),
		FerryRevision:   unrecorded(rowValue(rows, labelFerryRev)),
		HarnessRevision: unrecorded(rowValue(rows, labelHarnessRev)),
		Count:           unrecorded(rowValue(rows, labelCount)),
		Benchtime:       unrecorded(rowValue(rows, labelBenchtime)),
		Command:         unrecorded(command),
		Generated:       generated,
		Modules:         modulesFrom(rows),
	}, nil
}

// generatedAt reads the timestamp the run was stamped with out of the preamble
// it is written into.
func generatedAt(preamble string) (string, error) {
	for line := range strings.SplitSeq(preamble, "\n") {
		if rest, ok := strings.CutPrefix(line, generatedPrefix); ok {
			return unrecorded(strings.TrimSuffix(rest, ".")), nil
		}
	}

	return "", fmt.Errorf("%w: no %q line", ErrNotPublished, generatedPrefix)
}

// gomaxprocs reads NumCPU back out of the CPUs cell, which is the only place
// the published file records it.
func gomaxprocs(cell string) int {
	i := strings.LastIndex(cell, gomaxprocsPrefix)
	if i < 0 {
		return 0
	}

	n, err := strconv.Atoi(strings.TrimSuffix(cell[i+len(gomaxprocsPrefix):], ")"))
	if err != nil {
		return 0
	}

	return n
}

// sectionFrom returns what follows an offset, up to the next heading of the
// same level.
func sectionFrom(doc string, at int) string {
	rest := doc[at:]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		return rest[:j]
	}

	return rest
}

// fenced returns the contents of the first fenced block after marker.
func fenced(doc, marker string) (string, error) {
	i := strings.Index(doc, marker)
	if i < 0 {
		return "", fmt.Errorf("%w: no %q", ErrNotPublished, marker)
	}

	rest := doc[i+len(marker):]

	open := strings.Index(rest, fence)
	if open < 0 {
		return "", fmt.Errorf("%w: nothing fenced after %q", ErrNotPublished, marker)
	}

	body := rest[open+len(fence):]

	shut := strings.Index(body, fence)
	if shut < 0 {
		return "", fmt.Errorf("%w: the block after %q is never closed", ErrNotPublished, marker)
	}

	return strings.Trim(body[:shut], "\n"), nil
}

// fence is a markdown code fence on a line of its own.
const fence = "\n```\n"

// tableRow is one two-cell row of a markdown table.
type tableRow struct{ label, value string }

// tableRows reads the two-cell rows out of a fragment, in order.
//
// The provenance table and the competitor version table are the only two in
// the section this is given, and both have that shape.
func tableRows(s string) []tableRow {
	var out []tableRow

	for line := range strings.SplitSeq(s, "\n") {
		if r, ok := twoCellRow(line); ok {
			out = append(out, r)
		}
	}

	return out
}

func twoCellRow(line string) (tableRow, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") || !strings.HasSuffix(trimmed, "|") {
		return tableRow{}, false
	}

	cells := strings.Split(strings.Trim(trimmed, "|"), "|")
	if len(cells) != 2 {
		return tableRow{}, false
	}

	label := strings.TrimSpace(cells[0])
	if strings.HasPrefix(label, "---") {
		return tableRow{}, false
	}

	return tableRow{label: label, value: strings.TrimSpace(cells[1])}, true
}

func rowValue(rows []tableRow, label string) string {
	for _, r := range rows {
		if r.label == label {
			return r.value
		}
	}

	return ""
}

// modulesFrom reads the competitor version table back.
//
// A module path is the one label in the section spelled in backticks, which is
// what tells those rows apart from the provenance rows above them. A file that
// recorded no versions has no such row, and comes back with none.
func modulesFrom(rows []tableRow) map[string]string {
	out := map[string]string{}

	for _, r := range rows {
		if path, ok := backticked(r.label); ok {
			out[path] = r.value
		}
	}

	return out
}

func backticked(s string) (string, bool) {
	if len(s) < 2 || !strings.HasPrefix(s, "`") || !strings.HasSuffix(s, "`") {
		return "", false
	}

	return s[1 : len(s)-1], true
}

// unrecorded is the inverse of value.
//
// A cell the renderer filled in because the field was empty comes back empty,
// so a recovered Meta is empty in exactly the places the published one was.
func unrecorded(s string) string {
	if s == notRecorded {
		return ""
	}

	return s
}

// ErrNotReproduced is returned when a fresh benchstat run over the raw output a
// results file carries disagrees with the benchstat block beside it.
var ErrNotReproduced = errors.New("report: the raw output does not reproduce the published benchstat block")

// Reproduces checks that fresh reproduces embedded.
//
// embedded is the benchstat block a published file carries; fresh is benchstat
// run again over the raw output the same file carries. Agreement is the guard
// that the file was internally consistent before anything was re-rendered,
// because every table above those two appendices is derived from those numbers
// and from nothing else.
//
// The one thing it ignores is the input file's path. benchstat writes the path
// it was handed into its own column header and pads the whole table to that
// path's width, so two runs over identical bytes under two names differ in
// every line without a measurement having moved. Comparing that padding would
// make the check turn on the name of a temporary file, which is the trap the
// person doing this by hand falls into.
func Reproduces(embedded, fresh string) error {
	want, got := canonicalBenchstat(embedded), canonicalBenchstat(fresh)

	if len(want) != len(got) {
		return fmt.Errorf("%w: it has %d lines against the published %d",
			ErrNotReproduced, len(got), len(want))
	}

	for i := range want {
		if want[i] != got[i] {
			return fmt.Errorf("%w: line %d reads %q where the published block reads %q",
				ErrNotReproduced, i+1, got[i], want[i])
		}
	}

	return nil
}

// canonicalBenchstat reduces benchstat text to the part of it that is a
// measurement: every name, every unit, every figure and every interval, with
// the column padding collapsed and the input path dropped.
func canonicalBenchstat(s string) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	out := make([]string, 0, len(lines))

	for i, line := range lines {
		if namesTheInputs(lines, i) {
			out = append(out, inputNames)

			continue
		}

		out = append(out, collapse(line))
	}

	return out
}

// namesTheInputs reports whether lines[i] is the header naming the input files.
//
// benchstat writes it directly above the units line, so it is the only ruled
// line whose successor is also ruled, and the only line in the block whose
// content is a path rather than a measurement.
func namesTheInputs(lines []string, i int) bool {
	return strings.Contains(lines[i], columnRule) &&
		i+1 < len(lines) && strings.Contains(lines[i+1], columnRule)
}

// collapse drops the column padding, whose width is set by the length of the
// input path rather than by anything that was measured.
func collapse(line string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(line, columnRule, " ")), " ")
}

const (
	// columnRule is the box character benchstat draws its columns with.
	columnRule = "│"

	// inputNames stands in for the header line naming the input files.
	inputNames = "<input>"
)
