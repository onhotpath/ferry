package bench

import (
	"context"
	"fmt"

	"github.com/gojekfarm/xtools/xload"
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
	kelseyNotes = "Derives every variable name from the Go field name, so the fixture carries no " +
		"tag for it at all, except on the slice and the map, whose variable names it could " +
		"not otherwise find. Reflects on every call; nothing cached. Reads the delimited " +
		"CSV_TAGS and KV_LIMITS spellings, one variable each."
)

func xloadEnv[T any]() Impl {
	return Impl{
		Name: "xload", Module: "github.com/gojekfarm/xtools/xload", Notes: xloadNotes,
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
