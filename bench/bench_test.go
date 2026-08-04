package bench_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	xyaml "github.com/gojekfarm/xtools/xload/providers/yaml"

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

// TestFerryReadsTheChildrenAndNotTheContainer is the measurement behind the
// fixture's CSV_TAGS naming, kept as a test rather than as a claim in a
// comment.
//
// ferry addresses a slice element at TAGS_0, so /tags is a container address
// that holds nothing itself. Setting TAGS as well - the delimited spelling
// three of the env libraries want - puts a string there.
//
// Core used to refuse the whole load for that. It no longer does: at a slice or
// a map, over a source that implements Enumerator, it asks for the container's
// children before it asks the container's own address, and asks the container's
// own address only where there were no children (ADR-0003, amended under #209).
// driver/env enumerates, so TAGS_0.. win and the string at TAGS is never read.
//
// The whole struct is compared rather than the one field, because "the load
// succeeded" would pass just as well if the extra variable had quietly moved
// something else.
func TestFerryReadsTheChildrenAndNotTheContainer(t *testing.T) {
	env := bench.EnvLarge()
	env["TAGS"] = "checkout,payments,eu"

	// The other tests share this process's environment, so put it back.
	t.Cleanup(func() { bench.ApplyEnv(bench.EnvLarge()) })
	bench.ApplyEnv(env)

	got, err := ferry.Load[bench.Large](context.Background(), ferryenv.New(), ferry.TagKey("yaml"))
	if err != nil {
		t.Fatalf("ferry refused a plane holding a string at the container address /tags: %v", err)
	}

	if want := bench.WantLarge(); !reflect.DeepEqual(got, want) {
		t.Errorf("a string at /tags changed what ferry loaded\n  got:  %#v\n  want: %#v", got, want)
	}
}

// TestXloadYAMLProviderDropsASequence pins the measurement behind xload's
// absence from yaml_large, so that the reason in Absences is checked rather
// than asserted.
//
// xload's own YAML provider flattens the document with xload.FlattenMap, which
// recurses into a nested mapping and does not recurse into a sequence: the
// sequence goes through spf13/cast.ToString, which yields the empty string. So
// a []string field is unreachable, and unreachable from the loader's output
// rather than merely awkward - the elements are not anywhere in the map.
func TestXloadYAMLProviderDropsASequence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.yaml")
	if err := os.WriteFile(path, []byte(bench.YAMLLarge), 0o600); err != nil {
		t.Fatal(err)
	}

	flat, err := xyaml.NewFileLoader(path, "_")
	if err != nil {
		t.Fatal(err)
	}

	// The scalars are all there, so this is about the sequence and not about
	// the provider being broken.
	if got := flat["server_http_port"]; got != "8081" {
		t.Fatalf("the provider did not flatten a nested scalar: /server/http/port = %q", got)
	}

	if got, ok := flat["tags"]; !ok || got != "" {
		t.Errorf(`flat["tags"] = %q, present=%v; want the empty string, `+
			`which is what cast.ToString makes of a sequence`, got, ok)
	}

	for _, k := range []string{"tags_0", "tags_1", "tags_2"} {
		if _, ok := flat[k]; ok {
			t.Errorf("flat[%q] exists, so the provider does index sequences after all "+
				"and xload's absence from yaml_large needs revisiting", k)
		}
	}

	// A mapping is reshaped rather than lost, which is the contrast that makes
	// the sequence the blocking case.
	if got := flat["limits_rps"]; got != "1000" {
		t.Errorf(`flat["limits_rps"] = %q, want "1000"; a mapping is flattened per entry`, got)
	}
}
