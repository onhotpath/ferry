package bench_test

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/bench"
	ferryenv "github.com/onhotpath/ferry/driver/env"
)

// fixture is the shared scratch state. It is built once in TestMain, because
// the YAML documents on disk have to outlive every benchmark that reads them
// and b.TempDir would give each one its own.
var fixture *bench.Fixture

// TestMain builds the fixture and then refuses to run anything at all unless
// every implementation of every scenario produces the identical value.
//
// The refusal is the whole design. Benchmarks that measure different work are
// worse than no benchmarks, so the equivalence check is not a test somebody
// might skip with -run - it gates the process. Break one adapter and nothing
// runs, including the benchmarks.
func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "ferry-bench-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "bench: creating the scratch directory:", err)

		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// The environment is about to be cleared and rebuilt per scenario, so the
	// scratch directory has to be resolved before that happens.
	fixture, err = bench.NewFixture(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)

		return 1
	}

	if bad := bench.CheckEquivalence(fixture); len(bad) > 0 {
		fmt.Fprintln(os.Stderr, bench.FormatMismatches(bad))

		return 1
	}

	return m.Run()
}

// TestEquivalence states the gate as a test as well, so that a plain `go test`
// reports it as a passing assertion rather than only as the absence of a
// failure.
func TestEquivalence(t *testing.T) {
	if bad := bench.CheckEquivalence(fixture); len(bad) > 0 {
		t.Fatal(bench.FormatMismatches(bad))
	}
}

// TestScenariosAreNamedOnce guards the renderer's assumption that a scenario
// name and an implementation name together identify one row.
func TestScenariosAreNamedOnce(t *testing.T) {
	seen := map[string]bool{}

	for _, sc := range bench.Scenarios() {
		if seen[sc.Name] {
			t.Errorf("scenario %q is declared twice", sc.Name)
		}

		seen[sc.Name] = true

		impls := map[string]bool{}

		for _, impl := range sc.Impls {
			if impls[impl.Name] {
				t.Errorf("scenario %q declares %q twice", sc.Name, impl.Name)
			}

			impls[impl.Name] = true
		}
	}
}

// BenchmarkLoad is the comparison. The name shape is
//
//	BenchmarkLoad/<scenario>/<cold|warm>/<library>
//
// which benchstat splits into three row dimensions, so a table can be pivoted
// on any of them without the renderer having to parse a name back apart.
func BenchmarkLoad(b *testing.B) {
	for _, sc := range bench.Scenarios() {
		b.Run(sc.Name, func(b *testing.B) {
			if sc.Setup != nil {
				sc.Setup(fixture)
			}

			for _, impl := range sc.Impls {
				b.Run("cold/"+impl.Name, func(b *testing.B) { benchCold(b, sc, impl) })
				b.Run("warm/"+impl.Name, func(b *testing.B) { benchWarm(b, sc, impl) })
			}
		})
	}
}

// benchCold pays for the constructor on every iteration, which is what defeats
// ferry's schema cache and what makes viper re-register its keys.
func benchCold(b *testing.B, sc bench.Scenario, impl bench.Impl) {
	dst := sc.NewDst()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		load, err := impl.New(fixture)
		if err != nil {
			b.Fatal(err)
		}

		if err := load(dst); err != nil {
			b.Fatal(err)
		}
	}
}

// benchWarm pays for the constructor once, which is what lets ferry's compiled
// schema be reused.
func benchWarm(b *testing.B, sc bench.Scenario, impl bench.Impl) {
	load, err := impl.New(fixture)
	if err != nil {
		b.Fatal(err)
	}

	dst := sc.NewDst()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if err := load(dst); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStructTagCost measures the confound the fixture's pooled tags exist
// to avoid: reflect.StructTag.Get scans the whole tag string linearly, so a
// struct tag carrying one key per library would charge its length to every
// library that reflects on every call, and not to the one that compiles once.
//
// It is reported beside the comparison rather than hidden, because it is the
// number that says whether pooling the tags was necessary or merely tidy.
func BenchmarkStructTagCost(b *testing.B) {
	const (
		pooled = `yaml:"host" env:"HOST"`
		perLib = `yaml:"host" env:"HOST" ferry:"host" koanf:"host" mapstructure:"host" ` +
			`xload:"HOST" caarlos:"HOST" envconfig:"HOST" json:"host" toml:"host"`
	)

	for _, tc := range []struct {
		name string
		tag  reflect.StructTag
	}{
		{"pooled", pooled},
		{"per_library", perLib},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				// The last key in the tag is the worst case and the one a
				// linear scan actually pays for. Lengths are summed rather
				// than the strings concatenated: a concatenation of two
				// non-empty results allocates and a concatenation involving
				// an empty one does not, which would put an allocation in one
				// arm of this benchmark and not the other and measure the
				// wrong thing entirely.
				sink += len(tc.tag.Get("toml")) + len(tc.tag.Get("env"))
			}
		})
	}
}

// sink keeps BenchmarkStructTagCost's result live.
var sink int

// TestFerryRefusesAStringAtAContainer is the measurement behind the fixture's
// CSV_TAGS naming, kept as a test rather than as a claim in a comment.
//
// ferry addresses a slice element at TAGS_0, so /tags is a container address
// that holds nothing itself. Setting TAGS as well - the delimited spelling
// three of the env libraries want - puts a string there, and ferry refuses the
// whole load rather than guessing which of the two the operator meant. That
// refusal is why the fixture spells the delimited form CSV_TAGS.
func TestFerryRefusesAStringAtAContainer(t *testing.T) {
	env := bench.EnvLarge()
	env["TAGS"] = "checkout,payments,eu"

	// The other tests share this process's environment, so put it back.
	t.Cleanup(func() { bench.ApplyEnv(bench.EnvLarge()) })
	bench.ApplyEnv(env)

	_, err := ferry.Load[bench.Large](context.Background(), ferryenv.New(), ferry.TagKey("yaml"))
	if err == nil {
		t.Fatal("ferry loaded a plane holding a string at the container address /tags")
	}

	if !strings.Contains(err.Error(), "/tags") {
		t.Errorf("the refusal is %q, want it to name /tags", err)
	}
}
