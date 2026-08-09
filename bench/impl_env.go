package bench

import (
	"context"
	"fmt"
	"strings"

	"github.com/gojekfarm/xtools/xload"
	xyaml "github.com/gojekfarm/xtools/xload/providers/yaml"
	kelsey "github.com/kelseyhightower/envconfig"
	sethvargo "github.com/sethvargo/go-envconfig"
)

const (
	xloadNotes = "ferry's direct ancestor, and the Load direction only. Reflects the struct on " +
		"every call and looks up one variable per leaf, so there is nothing to amortise " +
		"and the two columns are the same number by construction. Reads the delimited " +
		"CSV_TAGS and KV_LIMITS spellings, one variable each, where ferry and koanf read " +
		"one variable per element."
	goEnvconfigNotes = "Reflects the struct on every call and looks up one variable per leaf. " +
		"Nothing cached, so cold and warm are the same number. Reads the delimited " +
		"CSV_TAGS and KV_LIMITS spellings, one variable each."
	xloadYAMLNotes = "xload is not limited to the environment: its first-party provider module " +
		"xload/providers/yaml reads the file, unmarshals it and flattens it into a " +
		"MapLoader. The keys the flatten produces are the document's own, which are lower " +
		"case, and the pooled env: tag is upper case because go-envconfig shares it, so the " +
		"loader is wrapped in a one-line LoaderFunc that folds the case - the same shape as " +
		"xload's own PrefixLoader, and the reason there is no third tag key."
	xloadYAMLCaveat = "xload's YAML provider reads and parses the file once, when the loader is " +
		"constructed, and every later load is a map lookup against that snapshot. So this " +
		"warm figure excludes the file read and the YAML parse that ferry, koanf, viper and " +
		"the stdlib baseline all pay on every single load. The cold column is where these " +
		"rows are comparable; the warm one measures a different job."
	kelseyNotes = "Derives every variable name from the Go field name, so the fixture carries no " +
		"tag for it at all, except on the slice and the map, whose variable names it could " +
		"not otherwise find. Reflects on every call; nothing cached. Reads the delimited " +
		"CSV_TAGS and KV_LIMITS spellings, one variable each."
)

func xloadEnv[T any]() Impl {
	return Impl{
		Name: "xload", Module: "github.com/gojekfarm/xtools/xload", Notes: xloadNotes,
		Remark: "reflects per call; one delimited variable",
		New: func(*Fixture) (Loader, error) {
			return func(dst any) error {
				if _, err := dstOf[T](dst); err != nil {
					return err
				}

				if err := xload.Load(context.Background(), dst); err != nil {
					return fmt.Errorf("bench: xload: %w", err)
				}

				return nil
			}, nil
		}}
}

func xloadEnvSmall() Impl { return xloadEnv[Small]() }
func xloadEnvLarge() Impl { return xloadEnv[Large]() }

func goEnvconfigEnv[T any]() Impl {
	return Impl{
		Name: "go-envconfig", Module: "github.com/sethvargo/go-envconfig", Notes: goEnvconfigNotes,
		Remark: "reflects per call; one delimited variable",
		New: func(*Fixture) (Loader, error) {
			return func(dst any) error {
				if _, err := dstOf[T](dst); err != nil {
					return err
				}

				if err := sethvargo.Process(context.Background(), dst); err != nil {
					return fmt.Errorf("bench: go-envconfig: %w", err)
				}

				return nil
			}, nil
		}}
}

func goEnvconfigEnvSmall() Impl { return goEnvconfigEnv[Small]() }
func goEnvconfigEnvLarge() Impl { return goEnvconfigEnv[Large]() }

func kelseyEnv[T any]() Impl {
	return Impl{
		Name: "kelseyhightower", Module: "github.com/kelseyhightower/envconfig", Notes: kelseyNotes,
		Remark: "reflects per call; names from the fields",
		New: func(*Fixture) (Loader, error) {
			return func(dst any) error {
				if _, err := dstOf[T](dst); err != nil {
					return err
				}

				// The empty prefix is what makes the keys the bare NAME,
				// SERVER_HOST and so on, matching the other columns.
				if err := kelsey.Process("", dst); err != nil {
					return fmt.Errorf("bench: kelseyhightower/envconfig: %w", err)
				}

				return nil
			}, nil
		}}
}

func kelseyEnvSmall() Impl { return kelseyEnv[Small]() }
func kelseyEnvLarge() Impl { return kelseyEnv[Large]() }

// xloadYAMLSmall is xload against a YAML file, through xload's own provider
// module rather than through anything this harness rolled by hand.
//
// It appears in yaml_small and not in yaml_large, and the reason is a measured
// property of the provider rather than a limit of the harness. See
// TestXloadYAMLProviderDropsASequence, and the entry in Absences.
func xloadYAMLSmall() Impl {
	return Impl{
		Name:   "xload",
		Module: "github.com/gojekfarm/xtools/xload/providers/yaml",
		Notes:  xloadYAMLNotes,
		// The construction is where the file read and the parse happen, which
		// makes this row's warm number incomparable with every other row's.
		// Declared here, beside the code that causes it, so the renderer can
		// mark the cell rather than leaving it to a reader to notice.
		WarmCaveat: xloadYAMLCaveat,
		New: func(f *Fixture) (Loader, error) {
			// The separator has to agree with the prefixes in the struct tags,
			// which the provider's own doc comment insists on: the tags nest
			// with "_", so the flatten does too.
			flat, err := xyaml.NewFileLoader(f.YAMLSmall, "_")
			if err != nil {
				return nil, fmt.Errorf("bench: xload yaml provider: %w", err)
			}

			loader := xload.LoaderFunc(func(_ context.Context, key string) (string, error) {
				return flat[strings.ToLower(key)], nil
			})

			return func(dst any) error {
				if _, err := dstOf[Small](dst); err != nil {
					return err
				}

				if err := xload.Load(context.Background(), dst, xload.WithLoader(loader)); err != nil {
					return fmt.Errorf("bench: xload: %w", err)
				}

				return nil
			}, nil
		},
	}
}
