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
	// ferryNotesDumpInto and ferryNotesDumpFresh are the same sink in the two
	// dump scenarios, and the difference between them is the parse.
	ferryNotesDumpInto = "Edits the existing document in place: it reads and parses the file, writes only " +
		"the keys the struct maps, and leaves comments, key order, quoting and unmapped " +
		"keys intact. That read and parse is work no other column in this table does, " +
		"because no other column keeps anything - and it is most of what separates this " +
		"row from koanf's. dump_fresh is the same save with nothing at the path, which " +
		"is that parse taken away and nothing else." + ferryNotesReplace
	ferryNotesDumpFresh = "The same sink with no document at the path, so there is nothing to merge into " +
		"and nothing to keep: the save writes the file whole, which is what the other " +
		"columns do in both scenarios. The distance from this row to ferry's dump_large " +
		"row is what editing a config file in place costs, measured rather than " +
		"argued." + ferryNotesReplace

	// ferryNotesReplace is the half both dump notes share: how the file is
	// replaced, which does not change between the two scenarios.
	ferryNotesReplace = " The replacement is atomic either way: a temporary file beside the plane is " +
		"renamed over it, so nothing ever reads a half-written config and a save that " +
		"fails leaves the file byte for byte as it was. It is not flushed to the disk " +
		"unless the caller asks for that, and these rows measure the default, so what is " +
		"timed here offers the same durability koanf and the baseline do. viper is the " +
		"one column that fsyncs: its WriteConfigAs ends in f.Sync(), which is why it " +
		"sits an order of magnitude above the other three. ferry's durable mode is " +
		"yaml.Durable(), and it is opt-in because the journal commit it costs dwarfs " +
		"everything else in the save. It is not what these rows measure."
)

// ferryDumpNotes and ferryDumpRemark are what ferry's row says in each of the
// two dump scenarios.
func ferryDumpNotes(t DumpTarget) string {
	if t.Seeded {
		return ferryNotesDumpInto
	}

	return ferryNotesDumpFresh
}

func ferryDumpRemark(t DumpTarget) string {
	if t.Seeded {
		return "merges, keeps comments; atomic"
	}

	return "writes whole; atomic"
}

// ferryNotesBoundLoad and ferryNotesBoundDump are the bound rows' notes.
const (
	ferryNotesBoundLoad = "The same job through a caller-held binding. ferry.Bind hands the source the " +
		"addresses the type names once, when the binding is built, and every load through the " +
		"binding skips that; ferry.Load does it again on every call. Nothing else differs - the " +
		"same tag key, the same Registry, the same walk, the same value out - so the distance " +
		"between this row and ferry's is what holding the binding is worth and nothing else. " +
		"Building the binding is constructor work, so it lands in the cold column, and this row's " +
		"cold figure is therefore the same job ferry's cold figure measures."
	ferryNotesBoundDump = "The dump direction's half of the same thing: ferry.BindSink hands the sink " +
		"the addresses the type names once and every dump through the binding skips that. It " +
		"writes a file of its own, prepared the way its scenario prepares every row's, and it " +
		"is read back by the same third-party reader as the rest of the table."
)

// ferryModule is the import path every ferry row is attributed to, the bound
// rows included: a held binding is not a second library.
const ferryModule = "github.com/onhotpath/ferry"

// ferryBound is the name of every row measured through a caller-held binding.
//
// It carries no "/" and no space, so a benchmark name still splits into four
// segments and the -N suffix the testing package appends is still the only
// thing after it.
const ferryBound = "ferry-bound"

// ferryTag is the pooled struct tag key, which ferry is told to read instead
// of its own default. ADR-0008 makes the tag key a compile-affecting option
// and part of the schema cache key, so this is a supported configuration and
// not a trick: it is the same grammar under a different key.
const ferryTag = "yaml"

// ferryOpts is the option set every ferry adapter uses. The Registry is the
// per-type schema cache, and minting a fresh one is how the cold benchmark
// defeats it.
//
// MustRegistry rather than NewRegistry (#299): the constructor returns an error
// now, and the only thing it refuses is a bad registration. This call registers
// nothing, so there is no error here to report and nowhere at this point in a
// benchmark to report one to.
func ferryOpts() []ferry.Option {
	return []ferry.Option{ferry.TagKey(ferryTag), ferry.WithRegistry(ferry.MustRegistry())}
}

func ferryEnv[T any](notes string) Impl {
	return Impl{
		Name: "ferry", Module: ferryModule, Notes: notes,
		Remark: "compiled schema; reads only what it names",
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

// ferryBoundEnv is ferryEnv with the bind lifted out of the timed loop.
//
// Bind goes in New, which is where the harness's cold/warm axis puts anything a
// library can amortise: cold pays for it on every iteration and warm pays for
// it once. So the warm figure is a load with the bind taken out of it, and the
// cold figure is the same three steps ferryEnv's cold figure pays for, since
// Load is Bind plus Binding.Load with the handle dropped (ADR-0012).
func ferryBoundEnv[T any]() Impl {
	return Impl{
		Name: ferryBound, Module: ferryModule, Notes: ferryNotesBoundLoad, Remark: "held binding", Variant: true,
		New: func(*Fixture) (Loader, error) {
			b, err := ferry.Bind[T](ferryenv.New(), ferryOpts()...)
			if err != nil {
				return nil, fmt.Errorf("bench: ferry bind: %w", err)
			}

			return boundLoader(b), nil
		}}
}

func ferryBoundEnvSmall() Impl { return ferryBoundEnv[Small]() }
func ferryBoundEnvLarge() Impl { return ferryBoundEnv[Large]() }

// boundLoader is the timed half of every bound load row: the binding is already
// built, so all that is left is the load and the assertion the harness charges
// every column for.
func boundLoader[T any](b *ferry.Binding[T]) Loader {
	return func(dst any) error {
		p, err := dstOf[T](dst)
		if err != nil {
			return err
		}

		v, err := b.Load(context.Background())
		if err != nil {
			return fmt.Errorf("bench: ferry bound load: %w", err)
		}

		*p = v

		return nil
	}
}

func ferryYAML[T any](path func(*Fixture) string) Impl {
	return Impl{
		Name: "ferry", Module: ferryModule, Notes: ferryNotesYAML,
		Remark: "compiled schema; reads a node tree",
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

// ferryBoundYAML is ferryYAML with the bind lifted out of the timed loop. The
// source still reads and parses the document on every load: binding a source is
// not opening a plane, and this row measures the one and not the other.
func ferryBoundYAML[T any](path func(*Fixture) string) Impl {
	return Impl{
		Name: ferryBound, Module: ferryModule, Notes: ferryNotesBoundLoad, Remark: "held binding", Variant: true,
		New: func(f *Fixture) (Loader, error) {
			b, err := ferry.Bind[T](ferryyaml.NewSource(path(f)), ferryOpts()...)
			if err != nil {
				return nil, fmt.Errorf("bench: ferry bind: %w", err)
			}

			return boundLoader(b), nil
		}}
}

func ferryBoundYAMLSmall() Impl {
	return ferryBoundYAML[Small](func(f *Fixture) string { return f.YAMLSmall })
}

func ferryBoundYAMLLarge() Impl {
	return ferryBoundYAML[Large](func(f *Fixture) string { return f.YAMLLarge })
}

func ferryDump(t DumpTarget) Impl {
	return Impl{
		Name: "ferry", Module: ferryModule, Notes: ferryDumpNotes(t), Remark: ferryDumpRemark(t),
		New: func(f *Fixture) (Loader, error) {
			path, start, err := f.Prepare(t, "ferry")
			if err != nil {
				return nil, err
			}

			opts := ferryOpts()
			want := WantLarge()

			return func(dst any) error {
				if err := start(); err != nil {
					return err
				}

				if err := ferry.Dump(context.Background(), want, ferryyaml.NewSink(path), opts...); err != nil {
					return fmt.Errorf("bench: ferry dump: %w", err)
				}

				return readBackLarge(path, dst)
			}, nil
		}}
}

// ferryBoundDump is ferryDump with the bind lifted out of the timed loop,
// through the sink half of the same surface.
func ferryBoundDump(t DumpTarget) Impl {
	return Impl{
		Name: ferryBound, Module: ferryModule, Notes: ferryNotesBoundDump,
		Remark: "held sink binding", Variant: true,
		New: func(f *Fixture) (Loader, error) { return newFerryBoundDump(f, t) },
	}
}

// newFerryBoundDump prepares this row's own file, binds the sink to it once,
// and hands back the dump the timed loop runs.
func newFerryBoundDump(f *Fixture, t DumpTarget) (Loader, error) {
	// Its own file, like every other dump row, so that one column's output can
	// never become another's input.
	path, start, err := f.Prepare(t, ferryBound)
	if err != nil {
		return nil, err
	}

	b, err := ferry.BindSink[Large](ferryyaml.NewSink(path), ferryOpts()...)
	if err != nil {
		return nil, fmt.Errorf("bench: ferry bind sink: %w", err)
	}

	return boundDumper(b, path, start), nil
}

// boundDumper is the timed half of the bound dump row: the sink binding is
// already built, so what is left is the dump and the read-back every dump
// column is charged for.
func boundDumper(b *ferry.SinkBinding[Large], path string, start func() error) Loader {
	want := WantLarge()

	return func(dst any) error {
		if err := start(); err != nil {
			return err
		}

		if err := b.Dump(context.Background(), want); err != nil {
			return fmt.Errorf("bench: ferry bound dump: %w", err)
		}

		return readBackLarge(path, dst)
	}
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
