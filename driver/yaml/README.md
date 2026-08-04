# ferry/yaml

Load configuration from a YAML file into a Go struct, and write it back without wrecking the file.

```
go get github.com/onhotpath/ferry/driver/yaml
```

## Loading and saving

Given `config.yaml`:

```yaml
# the port the server listens on
port: 8080
label: "8080" # quoted, so it stays a string
debug: false
tags:
  - a
owner: platform-team
```

and this struct:

```go
type config struct {
	Port  int      `ferry:"port"`
	Label string   `ferry:"label"`
	Debug bool     `ferry:"debug"`
	Tags  []string `ferry:"tags"`
}
```

load it, change two fields, and write it back:

```go
cfg, err := ferry.Load[config](ctx, yaml.NewSource(path))
// port=8080 label="8080" debug=false tags=[a]

cfg.Debug = true
cfg.Tags = append(cfg.Tags, "b")

err = ferry.Dump(ctx, cfg, yaml.NewSink(path))
```

The file afterwards:

```yaml
# the port the server listens on
port: 8080
label: "8080" # quoted, so it stays a string
debug: true
tags:
  - a
  - b
owner: platform-team
```

That is the `Example` in [`example_test.go`](example_test.go), trimmed of its setup, so `go test` compiles it and compares that exact output.

## Saving edits a file rather than replacing it

The comment survived, so did the key order, so did the quotes around `"8080"`, and so did `owner`, which no field maps.
Only the keys your struct names are touched.

That is the reason to use this rather than marshalling a struct to YAML: a config file a person maintains stays a config file a person maintains.

**What does not survive**, all of them limits of the YAML writer rather than choices: a blank line between entries, an explicit `---` marker, a file whose entire content is comments, the original indentation, and the column a trailing comment sat in.

There is one more today and it is bigger: a scalar's tag, at a key your struct maps.
So `when: !!timestamp 2026-08-04` is saved back as `when: "2026-08-04"`.
A key no field maps keeps its tag, because it is never touched.
That is [#155](https://github.com/onhotpath/ferry/issues/155).

## Types survive the trip

A number stays a number, a quoted number stays a string, and `null` stays different from a key that is not there at all:

```yaml
nul: null    # an explicit null
empty: ""    # an empty string, which is not the same thing
value: 8080  # a number, and "8080" would be a string
             # a key that is simply missing is different again
```

A `[]byte` field is saved as standard YAML `!!binary`, base64.

## Loading and saving are separate types

`yaml.NewSource(path)` reads and `yaml.NewSink(path)` writes, so the path is written twice.
That is deliberate: code handed only a `Source` cannot save through it, and passing one to `ferry.Dump` does not compile rather than failing halfway through a write.

## Saving is atomic, and durable if you ask

A save writes a temporary file beside yours and renames it into place once everything has been written.
Nothing ever reads a half-written config, and no temporary file is ever left behind.
A save that fails leaves your file byte for byte as it was.
That is unconditional and there is no way to switch it off.

What is not unconditional is durability, which is a different promise and a far more expensive one.
By default the replacement is handed to the operating system and lives in its cache until the kernel writes it out, exactly as any ordinary file write does, so a machine that loses power in that window comes back to the old document.

```go
err = ferry.Dump(ctx, cfg, yaml.NewSink(path, yaml.Durable()))
```

`yaml.Durable()` buys the other promise: the new file's contents are flushed to the disk, and so is the directory entry that makes your path point at them, so a save that returned `nil` has reached the disk rather than the cache.
It costs a disk flush, which is usually more expensive than everything else in the save put together.
Windows has no way to flush a directory: there the contents are flushed and the durability of the rename is the filesystem's own business.

A durable save has one case where a save that failed has still replaced your file, and it is the flush that fails once the rename has landed.
It reports `ferry.ErrPlane`, because what could not be promised is that the replacement survives a crash, not that it happened.

## One thing it cannot do

A Go `string` can hold any bytes; a YAML string has to be valid text.
So a string field holding bytes that are not valid UTF-8 cannot be saved to YAML at all.

Rather than mangling the value or inventing a private YAML tag for it, saving fails and names the field, and your file is left alone.
If you need to move arbitrary bytes through a YAML file, declare that field `[]byte` instead, which is saved as `!!binary`.

This is a known limitation and is tracked in [#156](https://github.com/onhotpath/ferry/issues/156).

## More

The [package documentation](https://pkg.go.dev/github.com/onhotpath/ferry/driver/yaml) is the reference for everything above, and the design records behind it are in [`docs/adr/`](../../docs/adr/).
