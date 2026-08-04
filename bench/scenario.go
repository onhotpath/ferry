package bench

import (
	"fmt"
	"os"
	"path/filepath"
)

// Loader is one unit of measured work: read the source, map it, fill dst.
// Every library's adapter is reduced to this, so that what is timed is the
// same job in every column.
//
// dst is a pointer to the scenario's struct - *Small or *Large - and it is a
// pointer rather than a returned value for one reason: funnelling a fifty-one
// field struct back through an `any` would box it, which is a heap allocation
// of the whole struct charged identically to every column and large enough to
// swamp the differences the table exists to show. A pointer in an interface
// fits the interface word and allocates nothing.
type Loader func(dst any) error

// dstOf is the type assertion every adapter starts with.
func dstOf[T any](dst any) (*T, error) {
	p, ok := dst.(*T)
	if !ok {
		var zero T

		return nil, fmt.Errorf("bench: destination is %T, want %T", dst, &zero)
	}

	return p, nil
}

// Impl is one library's adapter for one scenario.
//
// The split between New and the [Loader] it returns is the cold-versus-warm
// axis, and it is the only definition of those two words this harness uses:
//
//	cold   New is called on every iteration, then the Loader once
//	warm   New is called once, then the Loader on every iteration
//
// So anything a library is able to amortise across loads belongs inside New,
// and the pair of numbers says how much there was to amortise. ferry compiles
// a schema per type and caches it on a Registry, so its New mints a Registry:
// cold recompiles fifty-one addresses on every iteration and warm does not.
// A library that reflects on every call has nothing to put in New, its two
// numbers come out the same, and that flat line is the finding rather than a
// missing measurement.
type Impl struct {
	// Name is the column heading, and the last segment of the benchmark name.
	// It names the library, not the adapter, and carries no "/" so that a
	// benchmark name splits into exactly four segments.
	Name string

	// Module is the import path the column is attributed to, so that the
	// results file can name what was measured and at which version rather
	// than a nickname. Empty for the stdlib column, which is not a module.
	Module string

	// Notes is where a semantic difference is stated rather than smoothed
	// over. It is rendered into the results file beside the numbers, because
	// a comparison that hides what each library actually did is worth less
	// than no comparison.
	Notes string

	// WarmCaveat, when set, says that this implementation's warm figure is not
	// comparable with the other rows' and why.
	//
	// It exists because one library in this comparison does its file read and
	// its parse in the constructor, so its warm column measures map lookups
	// against a snapshot while every other row's measures a whole load. That
	// is a real property of that library and it is neither hidden nor
	// corrected for; it is declared here, next to the code that causes it, so
	// that the renderer can mark the number itself rather than trusting a
	// reader to find a sentence underneath it.
	WarmCaveat string

	// New builds the loader, and is the half a warm run pays for once.
	New func(*Fixture) (Loader, error)
}

// Scenario is one benchmark: a source, a target struct, the value every
// library has to produce, and the field of implementations.
type Scenario struct {
	// Name is the benchmark's sub-name, and becomes a benchstat row key.
	Name string

	// What it measures, in one line, rendered into the results file.
	What string

	// Setup runs before the equivalence check and before every benchmark of
	// this scenario. It is where the process environment is replaced.
	Setup func(*Fixture)

	// NewDst mints a fresh, zero destination: a *Small or a *Large.
	//
	// Every equivalence subtest gets one of its own. Sharing a destination is
	// the defect that hides a library which fails to write a field, because
	// the previous library's value is already sitting there.
	NewDst func() any

	// Want is the value every implementation must produce, as a value rather
	// than a pointer.
	Want any

	// Impls is the field. Order is the order the results file lists them in.
	Impls []Impl
}

// Fixture is the scratch state the scenarios share: the YAML documents on
// disk, and a directory each dump implementation writes its own file in.
type Fixture struct {
	// Dir is a scratch directory that outlives one benchmark iteration.
	Dir string

	// YAMLSmall and YAMLLarge are the two documents on disk. They are read
	// from disk on every iteration by every library, page cache and all,
	// because "load the config file" is the job being compared and a library
	// that reads bytes is not doing less work than one that reads a file.
	YAMLSmall string
	YAMLLarge string
}

// NewFixture writes the YAML documents into dir and returns the fixture.
func NewFixture(dir string) (*Fixture, error) {
	f := &Fixture{
		Dir:       dir,
		YAMLSmall: filepath.Join(dir, "small.yaml"),
		YAMLLarge: filepath.Join(dir, "large.yaml"),
	}

	for path, doc := range map[string]string{f.YAMLSmall: YAMLSmall, f.YAMLLarge: YAMLLarge} {
		if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
			return nil, fmt.Errorf("bench: writing %s: %w", path, err)
		}
	}

	return f, nil
}

// Seed writes doc to a file named for one dump implementation and returns the
// path.
//
// Every dump implementation gets its own file so that one library's output can
// never become another's input. The seed matters: ferry's YAML sink edits an
// existing document in place, so dumping into an empty directory would measure
// a different job from the one it is for.
func (f *Fixture) Seed(name, doc string) (string, error) {
	path := filepath.Join(f.Dir, "dump-"+name+".yaml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		return "", fmt.Errorf("bench: seeding %s: %w", path, err)
	}

	return path, nil
}

// Scenarios is the whole comparison: every scenario, in the order the results
// file reports them.
func Scenarios() []Scenario {
	return []Scenario{
		envSmallScenario(),
		envLargeScenario(),
		yamlSmallScenario(),
		yamlLargeScenario(),
		dumpLargeScenario(),
	}
}

func envSmallScenario() Scenario {
	return Scenario{
		Name:   "env_small",
		What:   "five flat fields out of the process environment, where a mapping layer has the least room to hide.",
		Setup:  func(*Fixture) { ApplyEnv(EnvSmall()) },
		NewDst: func() any { return new(Small) },
		Want:   WantSmall(),
		Impls: []Impl{
			ferryEnvSmall(), koanfEnvSmall(), viperEnvSmall(), xloadEnvSmall(),
			goEnvconfigEnvSmall(), kelseyEnvSmall(), stdlibEnvSmall(),
		},
	}
}

func envLargeScenario() Scenario {
	return Scenario{
		Name: "env_large",
		What: "fifty-one leaves over three levels, including a slice and a map, " +
			"out of the process environment.",
		Setup:  func(*Fixture) { ApplyEnv(EnvLarge()) },
		NewDst: func() any { return new(Large) },
		Want:   WantLarge(),
		Impls: []Impl{
			ferryEnvLarge(), koanfEnvLarge(), viperEnvLarge(), xloadEnvLarge(),
			goEnvconfigEnvLarge(), kelseyEnvLarge(), stdlibEnvLarge(),
		},
	}
}

func yamlSmallScenario() Scenario {
	return Scenario{
		Name:   "yaml_small",
		What:   "the same five fields, read from a YAML file on disk on every iteration.",
		Setup:  func(*Fixture) { ApplyEnv(nil) },
		NewDst: func() any { return new(Small) },
		Want:   WantSmall(),
		Impls: []Impl{
			ferryYAMLSmall(), koanfYAMLSmall(), viperYAMLSmall(), xloadYAMLSmall(), stdlibYAMLSmall(),
		},
	}
}

func yamlLargeScenario() Scenario {
	return Scenario{
		Name:   "yaml_large",
		What:   "the same fifty-one leaves, read from a YAML file on disk on every iteration.",
		Setup:  func(*Fixture) { ApplyEnv(nil) },
		NewDst: func() any { return new(Large) },
		Want:   WantLarge(),
		Impls: []Impl{
			ferryYAMLLarge(), koanfYAMLLarge(), viperYAMLLarge(), stdlibYAMLLarge(),
		},
	}
}

func dumpLargeScenario() Scenario {
	return Scenario{
		Name: "dump_large",
		What: "the other direction: the same fifty-one leaves written back out to a YAML file, " +
			"then read back to prove the round trip.",
		Setup:  func(*Fixture) { ApplyEnv(nil) },
		NewDst: func() any { return new(Large) },
		Want:   WantLarge(),
		Impls: []Impl{
			ferryDumpLarge(), koanfDumpLarge(), viperDumpLarge(), stdlibDumpLarge(),
		},
	}
}

// NotMeasured is a library that belongs in the comparison and could not be
// run, with the reason. It is rendered into the results file as a row of its
// own, because a library quietly missing from a table reads as a library that
// lost.
type NotMeasured struct {
	// Library is the module path.
	Library string

	// Scenario is the scenario it is absent from, or "all".
	Scenario string

	// Why is the reason, stated as a fact about the library.
	Why string
}

// Absences is every library this harness could not measure, and why.
func Absences() []NotMeasured {
	return []NotMeasured{
		{
			Library:  "github.com/gojekfarm/xtools/xload",
			Scenario: "yaml_large",
			Why: "its first-party provider cannot produce this struct, and the failure is in the " +
				"flatten rather than in the tag grammar. xload.FlattenMap does not descend " +
				"into a YAML sequence: it hands the whole sequence to spf13/cast.ToString, " +
				"which returns the empty string, so /tags arrives as one empty key and its " +
				"three elements are gone from the loader's map and unrecoverable from it. " +
				"(A mapping fares better and is merely reshaped: /limits explodes into " +
				"limits_rps and its siblings, where the tag addresses the map through one " +
				"delimited key.) Rebuilding the sequence would mean this harness parsing the " +
				"document itself and charging xload for the result, which would measure the " +
				"harness. Pinned by TestXloadYAMLProviderDropsASequence. yaml_small has no " +
				"sequence and no mapping, and xload is measured there with no bridge at all.",
		},
		{
			Library:  "github.com/gojekfarm/xtools/xload",
			Scenario: "dump_large",
			Why: "xload is the Load direction only. Its whole contract is a Loader that returns " +
				"a string for a key, and there is no inverse of it in the package.",
		},
		{
			Library:  "github.com/sethvargo/go-envconfig",
			Scenario: "yaml_small, yaml_large, dump_large",
			Why:      "environment variables only, by design. No file format and no dump direction.",
		},
		{
			Library:  "github.com/kelseyhightower/envconfig",
			Scenario: "yaml_small, yaml_large, dump_large",
			Why:      "environment variables only, by design. No file format and no dump direction.",
		},
		{
			Library:  "github.com/caarlos0/env",
			Scenario: "all",
			Why: "its tag parser rejects both the prefix= and the separator= options as unsupported, " +
				"which xload and go-envconfig share, so it cannot join the pooled env: tag. " +
				"Giving it a third tag key would lengthen every struct tag in the fixture and " +
				"charge that length to every library that reflects per call - see " +
				"BenchmarkStructTagCost for what that costs.",
		},
	}
}
