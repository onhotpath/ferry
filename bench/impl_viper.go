package bench

import (
	"fmt"
	"strings"

	"github.com/fatih/structs"
	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

const (
	viperNotesEnv = "Resolves an environment variable only for a key it already knows, so every " +
		"leaf key is registered up front - that registration is what New does, and it is " +
		"the whole cold/warm gap. Each load then walks the registered keys, reads the " +
		"environ for each, builds a map and mapstructure-decodes it."
	viperNotesYAML = "Reads and parses the file into its own settings map, then " +
		"mapstructure-decodes that map into the struct. The instance is reusable, but " +
		"it holds data rather than a compiled schema, so warm saves only the file-path " +
		"setup and not the parse."
	viperNotesDump = "viper holds a settings map rather than a struct, so it needs a struct-to-map " +
		"bridge to dump one. That is fatih/structs here, which is the same bridge koanf's " +
		"own structs provider uses internally, so the two dump columns are the same shape: " +
		"struct to map, map to YAML, whole file replaced. Neither preserves a comment, a key " +
		"order or a quoting decision, and neither write is atomic or fsynced."
)

// viperTag tells viper's mapstructure decoder to read the pooled yaml: tag
// rather than its own mapstructure: default.
func viperTag(c *mapstructure.DecoderConfig) { c.TagName = ferryTag }

// viperBindings are the keys whose variable name is not what viper's mechanical
// key-to-name rule would produce, bound explicitly with BindEnv.
//
// There is exactly one, and it is the slice: viper's rule would look for TAGS,
// and the delimited spelling lives at CSV_TAGS to stay out of the way of the
// indexed one. BindEnv is viper's own answer to that and is one line, which is
// what a viper user would write.
func viperBindings() map[string]string {
	return map[string]string{"tags": "CSV_TAGS"}
}

// newViperEnv is the registration a viper user writes by hand: the replacer
// that turns a dotted key into an underscored variable name, AutomaticEnv, and
// one SetDefault per leaf so that the key exists for AutomaticEnv to resolve.
func newViperEnv(keys []string) *viper.Viper {
	v := viper.New()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	bind := viperBindings()

	for _, k := range keys {
		if name, ok := bind[k]; ok {
			_ = v.BindEnv(k, name)
		}

		v.SetDefault(k, "")
	}

	return v
}

func viperEnv[T any](keys func() []string) Impl {
	return Impl{
		Name: "viper", Module: "github.com/spf13/viper", Notes: viperNotesEnv,
		New: func(*Fixture) (Loader, error) {
			v := newViperEnv(keys())

			return func(dst any) error {
				if _, err := dstOf[T](dst); err != nil {
					return err
				}

				if err := v.Unmarshal(dst, viperTag); err != nil {
					return fmt.Errorf("bench: viper unmarshal: %w", err)
				}

				return nil
			}, nil
		}}
}

func viperEnvSmall() Impl { return viperEnv[Small](SmallKeys) }
func viperEnvLarge() Impl { return viperEnv[Large](LargeKeys) }

func viperYAML[T any](path func(*Fixture) string) Impl {
	return Impl{
		Name: "viper", Module: "github.com/spf13/viper", Notes: viperNotesYAML,
		New: func(f *Fixture) (Loader, error) {
			v := viper.New()
			v.SetConfigFile(path(f))

			return func(dst any) error {
				if _, err := dstOf[T](dst); err != nil {
					return err
				}

				if err := v.ReadInConfig(); err != nil {
					return fmt.Errorf("bench: viper read: %w", err)
				}

				if err := v.Unmarshal(dst, viperTag); err != nil {
					return fmt.Errorf("bench: viper unmarshal: %w", err)
				}

				return nil
			}, nil
		}}
}

func viperYAMLSmall() Impl { return viperYAML[Small](func(f *Fixture) string { return f.YAMLSmall }) }
func viperYAMLLarge() Impl { return viperYAML[Large](func(f *Fixture) string { return f.YAMLLarge }) }

// viperDumpLarge is the dump direction through viper.
//
// It exists because excluding viper for needing a struct-to-map bridge while
// measuring koanf through exactly such a bridge was not a defensible line:
// koanf's own structs provider is fatih/structs, and this is the same call.
func viperDumpLarge() Impl {
	return Impl{
		Name: "viper", Module: "github.com/spf13/viper", Notes: viperNotesDump,
		New: func(f *Fixture) (Loader, error) {
			path, err := f.Seed("viper", YAMLLarge)
			if err != nil {
				return nil, err
			}

			want := WantLarge()

			return func(dst any) error {
				// A fresh instance per iteration, for the same reason koanf
				// gets one: an instance that already holds the settings would
				// be measuring a second write rather than a dump.
				v := viper.New()
				v.SetConfigFile(path)

				st := structs.New(want)
				st.TagName = ferryTag

				if err := v.MergeConfigMap(st.Map()); err != nil {
					return fmt.Errorf("bench: viper merge: %w", err)
				}

				if err := v.WriteConfigAs(path); err != nil {
					return fmt.Errorf("bench: viper write: %w", err)
				}

				return readBackLarge(path, dst)
			}, nil
		},
	}
}
