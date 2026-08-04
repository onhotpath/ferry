package main

// The re-render mode.
//
// Some of what the results file says is prose the harness supplies rather than
// a measurement: the per-library notes in bench/impl_*.go, the scenario
// descriptions in bench/scenario.go, the reasons in Absences(), and the static
// paragraphs in internal/report/render.go. When one of those is wrong, the fix
// is a harness change plus a re-render, and not one number may move.
//
// This mode does that from the committed file itself. The numbers come from
// benchstat run again over the raw output the file already embeds, and the
// provenance comes off the file's own table, so nothing that describes the
// measurement is ever restated by a person or by a workflow input.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/onhotpath/ferry/bench/internal/report"
)

// The two appendices, written out under the names the workflow then hands to
// benchstat.
const (
	rawName  = "bench.txt"
	statName = "stat.txt"
)

// republish re-renders a results file from its own contents.
func (o *opts) republish() error {
	if err := o.refuseRestated(); err != nil {
		return err
	}

	doc, err := read(o.rerender)
	if err != nil {
		return err
	}

	pub, err := report.Extract(doc)
	if err != nil {
		return fmt.Errorf("%s: %w", o.rerender, err)
	}

	if o.extractTo != "" {
		return o.extract(pub)
	}

	in, err := o.rerenderInput(pub)
	if err != nil {
		return err
	}

	return o.write(&in)
}

// refuseRestated rejects the flags that describe a measurement.
//
// In this mode every one of them is recovered from the file, so passing one is
// a request to restate something that must not move. Ignoring it silently is
// how a re-render would come to publish a runner it did not run on.
func (o *opts) refuseRestated() error {
	for _, f := range []struct{ name, value string }{
		{"-runner", o.runner}, {"-ferry-rev", o.ferryRev}, {"-harness-rev", o.benchRev},
		{"-count", o.count}, {"-benchtime", o.benchtime}, {"-command", o.command},
		{"-generated", o.generated}, {"-stat", o.stat}, {"-raw", o.raw},
	} {
		if f.value != "" {
			return fmt.Errorf("%s describes the measurement and is recovered from %s, "+
				"so passing it would restate it", f.name, o.rerender)
		}
	}

	return nil
}

// extract writes the two embedded appendices out and stops.
//
// It is a step of its own because this program does not run benchstat. The
// workflow does, at the version it pins, which is the same benchstat the
// published numbers were produced with.
func (o *opts) extract(pub *report.Published) error {
	for _, f := range []struct{ name, body string }{
		{rawName, pub.RawBench},
		{statName, pub.BenchstatText},
	} {
		out := filepath.Join(o.extractTo, f.name)

		if err := os.WriteFile(out, []byte(f.body+"\n"), published); err != nil {
			return fmt.Errorf("writing %s: %w", out, err)
		}

		fmt.Println("perfreport: wrote", out)
	}

	return nil
}

// rerenderInput builds the renderer's input from a file that was already
// published.
//
// The two appendices are carried through byte for byte, and the benchstat block
// in particular is never regenerated: benchstat writes the path it was handed
// into its own column header, so a fresh block produced under a different
// filename would change the published file with no measurement having moved.
// The figures come from a fresh CSV over the raw output rather than from that
// block, because the block is rounded to what benchstat prints and every table
// above it is not.
func (o *opts) rerenderInput(pub *report.Published) (report.Input, error) {
	if o.results == "" || o.csv == "" {
		return report.Input{}, errors.New("-results and -csv are required to re-render")
	}

	if err := o.checkReproduces(pub); err != nil {
		return report.Input{}, err
	}

	stats, err := o.parseCSV()
	if err != nil {
		return report.Input{}, err
	}

	meta := pub.Meta
	o.relink(&meta)

	return report.Input{
		Meta:          meta,
		Stats:         stats,
		Scenarios:     scenarioDocs(),
		Absences:      absenceDocs(),
		BenchstatText: pub.BenchstatText,
		RawBench:      pub.RawBench,
	}, nil
}

// checkReproduces is the guard the mode rests on: the raw output the file
// carries has to produce the benchstat block beside it, or the file was not
// internally consistent and re-rendering it would publish that inconsistency
// under a fresh commit.
func (o *opts) checkReproduces(pub *report.Published) error {
	if o.recheck == "" {
		return errors.New("-recheck is required to re-render: without it nothing checks that the " +
			"published file's raw output still produces its own benchstat block")
	}

	fresh, err := read(o.recheck)
	if err != nil {
		return err
	}

	if err := report.Reproduces(pub.BenchstatText, fresh); err != nil {
		return fmt.Errorf("%s: %w", o.rerender, err)
	}

	fmt.Println("perfreport: the embedded raw output reproduces the embedded benchstat block")

	return nil
}

func (o *opts) parseCSV() (*report.Stats, error) {
	csvText, err := read(o.csv)
	if err != nil {
		return nil, err
	}

	return report.ParseCSV(csvText)
}

// relink is the one part of the provenance a re-render supplies rather than
// recovers.
//
// The three links say where the published files live in the repository, which
// is a fact about the layout and not about the run, and the results file spells
// the charts as bare filenames so they could not be recovered in full anyway.
func (o *opts) relink(m *report.Meta) {
	m.ResultsLink = o.link(filepath.Base(o.results))
	m.ChartLightLink = o.link(chartLight)
	m.ChartDarkLink = o.link(chartDark)
}
