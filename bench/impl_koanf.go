package bench

import (
	"fmt"
	"os"
	"strings"

	kyaml "github.com/knadh/koanf/parsers/yaml"
	kenv "github.com/knadh/koanf/providers/env/v2"
	kfile "github.com/knadh/koanf/providers/file"
	kstructs "github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

const (
	koanfNotesEnv = "Reads the whole environ on every load and unflattens it into a map, then " +
		"mapstructure-decodes the map into the struct. Nothing is cached between loads, " +
		"so there is nothing for the warm column to amortise. Its env provider yields " +
		"strings, so the []string field needs an explicit split in the TransformFunc - " +
		"the map falls out of LIMITS_* unflattening for free."
	koanfNotesYAML = "Parses the file into a map, then mapstructure-decodes the map into the " +
		"struct: two passes over the data and one intermediate map per load, none of it cached."
	koanfNotesDump = "Reflects the struct into a map with fatih/structs, marshals the map to YAML " +
		"and writes the file whole. It never reads what is at the path, so this row is the " +
		"same code doing the same work in both dump scenarios and its two figures differ " +
		"only by the removal a dump_fresh iteration begins with. Over a document that is " +
		"already there that means comments, key order, quoting and every key no field maps " +
		"are lost; over a path with no file at it there is nothing to lose, which is why " +
		"dump_fresh is the scenario where this column and ferry's are doing the same job. " +
		"The write is not atomic."
)

// koanfConf is koanf's unmarshal configuration. Tag is what lets koanf read
// the pooled yaml: tag instead of its own koanf: default.
func koanfConf() koanf.UnmarshalConf {
	return koanf.UnmarshalConf{Tag: ferryTag}
}

// koanfEnvProvider is the transform a koanf user writes for this environment.
//
// Lower-casing and turning _ into . is the documented pattern. The two extra
// cases are the honest cost of a flat plane: CSV_TAGS arrives as one delimited
// string and koanf cannot know it is a list, so the split is written here; and
// the two variables spelled for the delimited libraries are dropped, because
// leaving TAGS_0.. in would collide with the split list at the same key.
func koanfEnvProvider() *kenv.Env {
	return kenv.Provider(".", kenv.Opt{TransformFunc: func(k, v string) (string, any) {
		switch {
		case k == "CSV_TAGS":
			return "tags", strings.Split(v, ",")
		case k == "KV_LIMITS", strings.HasPrefix(k, "TAGS_"):
			return "", nil
		default:
			return strings.ReplaceAll(strings.ToLower(k), "_", "."), v
		}
	}})
}

func koanfEnv[T any](notes string) Impl {
	return Impl{
		Name: "koanf", Module: "github.com/knadh/koanf/v2", Notes: notes,
		Remark: "reads the whole environ per load",
		New: func(*Fixture) (Loader, error) {
			return func(dst any) error {
				if _, err := dstOf[T](dst); err != nil {
					return err
				}

				k := koanf.New(".")
				if err := k.Load(koanfEnvProvider(), nil); err != nil {
					return fmt.Errorf("bench: koanf load: %w", err)
				}

				if err := k.UnmarshalWithConf("", dst, koanfConf()); err != nil {
					return fmt.Errorf("bench: koanf unmarshal: %w", err)
				}

				return nil
			}, nil
		}}
}

func koanfEnvSmall() Impl { return koanfEnv[Small](koanfNotesEnv) }
func koanfEnvLarge() Impl { return koanfEnv[Large](koanfNotesEnv) }

func koanfYAML[T any](path func(*Fixture) string) Impl {
	return Impl{
		Name: "koanf", Module: "github.com/knadh/koanf/v2", Notes: koanfNotesYAML,
		Remark: "file to map to struct; nothing cached",
		New: func(f *Fixture) (Loader, error) {
			p := path(f)

			return func(dst any) error {
				if _, err := dstOf[T](dst); err != nil {
					return err
				}

				k := koanf.New(".")
				if err := k.Load(kfile.Provider(p), kyaml.Parser()); err != nil {
					return fmt.Errorf("bench: koanf load: %w", err)
				}

				if err := k.UnmarshalWithConf("", dst, koanfConf()); err != nil {
					return fmt.Errorf("bench: koanf unmarshal: %w", err)
				}

				return nil
			}, nil
		}}
}

func koanfYAMLSmall() Impl { return koanfYAML[Small](func(f *Fixture) string { return f.YAMLSmall }) }
func koanfYAMLLarge() Impl { return koanfYAML[Large](func(f *Fixture) string { return f.YAMLLarge }) }

func koanfDump(t DumpTarget) Impl {
	return Impl{
		Name: "koanf", Module: "github.com/knadh/koanf/v2", Notes: koanfNotesDump,
		Remark: "replaces the file whole",
		New: func(f *Fixture) (Loader, error) {
			path, start, err := f.Prepare(t, "koanf")
			if err != nil {
				return nil, err
			}

			want := WantLarge()

			return func(dst any) error {
				if err := start(); err != nil {
					return err
				}

				k := koanf.New(".")
				if err := k.Load(kstructs.Provider(want, ferryTag), nil); err != nil {
					return fmt.Errorf("bench: koanf structs load: %w", err)
				}

				b, err := k.Marshal(kyaml.Parser())
				if err != nil {
					return fmt.Errorf("bench: koanf marshal: %w", err)
				}

				if err := os.WriteFile(path, b, 0o600); err != nil {
					return fmt.Errorf("bench: koanf write: %w", err)
				}

				return readBackLarge(path, dst)
			}, nil
		}}
}
