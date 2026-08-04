package bench

import (
	"context"
	"fmt"
	"os"

	yamlv3 "go.yaml.in/yaml/v3"

	"github.com/onhotpath/ferry"
	ferryenv "github.com/onhotpath/ferry/driver/env"
	ferryyaml "github.com/onhotpath/ferry/driver/yaml"
)

// ferryNotesEnv and the rest are the notes column: what ferry did that the
// other columns did not, or did not do that they did.
const (
	ferryNotesEnv = "Compiles a schema per type and caches it on the Registry, which is what the " +
		"cold/warm gap is. Reads only the addresses the schema names, so the size of the " +
		"environ costs it nothing. Reads TAGS_0.. and LIMITS_* rather than one delimited variable."
	ferryNotesYAML = "Same compile and cache. The YAML source parses the document into a node tree " +
		"and reads addresses out of it, rather than unmarshalling into the struct."
	ferryNotesDump = "Edits the existing document in place: it reads and parses the file, writes only " +
		"the keys the struct maps, and leaves comments, key order, quoting and unmapped " +
		"keys intact. The replacement is atomic: a temporary file beside the plane is " +
		"renamed over it, so nothing ever reads a half-written config and a save that " +
		"fails leaves the file byte for byte as it was. It is not flushed to the disk " +
		"unless the caller asks for that, and this row measures the default, so what is " +
		"timed here offers the same durability koanf and the baseline do. viper is the " +
		"one column that fsyncs: its WriteConfigAs ends in f.Sync(), which is why it " +
		"sits an order of magnitude above the other three. ferry's durable mode is " +
		"yaml.Durable(), and it is opt-in because the journal commit it costs dwarfs " +
		"everything else in the save. It is not what this row measures."
)

// ferryTag is the pooled struct tag key, which ferry is told to read instead
// of its own default. ADR-0008 makes the tag key a compile-affecting option
// and part of the schema cache key, so this is a supported configuration and
// not a trick: it is the same grammar under a different key.
const ferryTag = "yaml"

// ferryOpts is the option set every ferry adapter uses. The Registry is the
// per-type schema cache, and minting a fresh one is how the cold benchmark
// defeats it.
func ferryOpts() []ferry.Option {
	return []ferry.Option{ferry.TagKey(ferryTag), ferry.WithRegistry(ferry.NewRegistry())}
}

func ferryEnv[T any](notes string) Impl {
	return Impl{
		Name: "ferry", Module: "github.com/onhotpath/ferry", Notes: notes,
		New: func(*Fixture) (Loader, error) {
			opts := ferryOpts()

			return func(dst any) error {
				p, err := dstOf[T](dst)
				if err != nil {
					return err
				}

				v, err := ferry.Load[T](context.Background(), ferryenv.New(), opts...)
				if err != nil {
					return fmt.Errorf("bench: ferry load: %w", err)
				}

				*p = v

				return nil
			}, nil
		}}
}

func ferryEnvSmall() Impl { return ferryEnv[Small](ferryNotesEnv) }
func ferryEnvLarge() Impl { return ferryEnv[Large](ferryNotesEnv) }

func ferryYAML[T any](path func(*Fixture) string) Impl {
	return Impl{
		Name: "ferry", Module: "github.com/onhotpath/ferry", Notes: ferryNotesYAML,
		New: func(f *Fixture) (Loader, error) {
			opts := ferryOpts()
			src := ferryyaml.NewSource(path(f))

			return func(dst any) error {
				p, err := dstOf[T](dst)
				if err != nil {
					return err
				}

				v, err := ferry.Load[T](context.Background(), src, opts...)
				if err != nil {
					return fmt.Errorf("bench: ferry load: %w", err)
				}

				*p = v

				return nil
			}, nil
		}}
}

func ferryYAMLSmall() Impl { return ferryYAML[Small](func(f *Fixture) string { return f.YAMLSmall }) }
func ferryYAMLLarge() Impl { return ferryYAML[Large](func(f *Fixture) string { return f.YAMLLarge }) }

func ferryDumpLarge() Impl {
	return Impl{
		Name: "ferry", Module: "github.com/onhotpath/ferry", Notes: ferryNotesDump,
		New: func(f *Fixture) (Loader, error) {
			path, err := f.Seed("ferry", YAMLLarge)
			if err != nil {
				return nil, err
			}

			opts := ferryOpts()
			want := WantLarge()

			return func(dst any) error {
				if err := ferry.Dump(context.Background(), want, ferryyaml.NewSink(path), opts...); err != nil {
					return fmt.Errorf("bench: ferry dump: %w", err)
				}

				return readBackLarge(path, dst)
			}, nil
		}}
}

// readBackLarge is the dump scenario's proof, and it is deliberately the same
// reader for every column: the libraries do not produce the same bytes - that
// is the point of the scenario - so the assertion has to be that they produce
// the same value, read back by a third party.
//
// It is inside the timed region on purpose. Every dump column pays it, so it
// is a constant added to all of them, and taking it out would mean timing a
// write nobody had checked.
func readBackLarge(path string, dst any) error {
	p, err := dstOf[Large](dst)
	if err != nil {
		return err
	}

	b, err := os.ReadFile(path) //nolint:gosec // the path is the harness's own scratch file
	if err != nil {
		return fmt.Errorf("bench: reading back %s: %w", path, err)
	}

	// A fresh value rather than *p, because reading back over a populated
	// struct would let a key the dump dropped keep the previous iteration's
	// value and turn a lost field into a pass.
	var got Large
	if err := yamlv3.Unmarshal(b, &got); err != nil {
		return fmt.Errorf("bench: reading back %s: %w", path, err)
	}

	*p = got

	return nil
}
