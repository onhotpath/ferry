package report

// This file is the harness's description of itself, reduced to plain data.
//
// The renderer takes it rather than importing the benchmark package, so that
// rendering can be tested against a fixture without linking viper, koanf and
// the rest, and so that nothing in the rendering path can reach a library and
// ask it a question at render time. Everything published comes from a
// measurement file or from one of these structs.

// ScenarioDoc describes one benchmarked scenario.
type ScenarioDoc struct {
	// Name matches the benchmark sub-name and the CSV row.
	Name string

	// What it measures, in one line.
	What string

	// Impls are the libraries compared, in the order they are listed.
	Impls []ImplDoc
}

// ImplDoc describes one library's column.
type ImplDoc struct {
	// Name matches the last segment of the benchmark name.
	Name string

	// Module is the import path measured, empty for the stdlib column.
	Module string

	// Notes is the semantic difference stated out loud.
	Notes string

	// Baseline marks the column that is not a library: the same job written by
	// hand with no mapping layer over it.
	//
	// It is the floor by construction, so it is never the answer to "fastest
	// other library" - every row loses to it and saying which lost least is
	// not information. It is published in every table it would otherwise be
	// in, and it is the denominator of the multiple rendered against every
	// library including ferry.
	Baseline bool

	// WarmCaveat, when set, says the warm figure is not comparable with the
	// other rows' and why. It marks the cell rather than only the prose.
	WarmCaveat string
}

// AbsenceDoc is a library that belongs in the comparison and was not measured.
type AbsenceDoc struct {
	Library  string
	Scenario string
	Why      string
}

// Meta is everything about the run that is not a measurement.
//
// It carries no defaults. A field the caller leaves empty renders as
// "not recorded", because a benchmark figure without the machine and the
// versions is not reproducible and saying so is better than filling it in.
type Meta struct {
	// GoVersion is runtime.Version() of the binary that ran the benchmarks.
	GoVersion string

	// GOOS, GOARCH and NumCPU come from the runtime.
	GOOS   string
	GOARCH string
	NumCPU int

	// Runner names the machine class, e.g. "ubuntu-latest (GitHub-hosted)".
	Runner string

	// FerryRevision and HarnessRevision are the two commits that were built
	// together.
	FerryRevision   string
	HarnessRevision string

	// Count and Benchtime are the flags the run used.
	Count     string
	Benchtime string

	// Command is the exact command line the numbers came from.
	Command string

	// Generated is the timestamp, supplied by the caller so that rendering is
	// a pure function of its input and can be tested.
	Generated string

	// Modules maps import path to version, read from the harness binary's own
	// build info rather than from go.mod, so it is the version that was
	// linked and not the version that was asked for.
	Modules map[string]string

	// ResultsLink, ChartLightLink and ChartDarkLink are the published files as
	// the README has to spell them: repository-relative, which is not the same
	// string as the filesystem path the command wrote them to.
	ResultsLink    string
	ChartLightLink string
	ChartDarkLink  string
}
