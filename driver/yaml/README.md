# ferry/driver/yaml

ferry's YAML plane: a `Source` that fills an annotated struct from a YAML file, and a `Sink` that writes one back.
It is the plane with a type system of its own, so a number stays a number, a quoted number stays a string, and an explicit `null` stays distinct from a key that is not there.

```go
import "github.com/onhotpath/ferry/driver/yaml"
```

## An example

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

That is `Example` in [`example_test.go`](example_test.go), so `go test` compiles it and compares that output.

## A dump is a merge, not a rewrite

Everything above survived: the comment, the key order, the quoting of a value ferry did not touch, and `owner`, which no field maps.
Only the addresses ferry maps are replaced.
A marshal through `map[string]any` loses all four, and this is the driver's most useful property for a file a human maintains.

Five things do not survive, and they are the emitter's limits rather than choices: a blank line between entries, an explicit `---` marker, a document whose entire content is comments, the original indentation, and a trailing comment's column.
There is a sixth today, and it is larger: the tag of a scalar at a mapped address, so `when: !!timestamp 2026-08-04` is written back as `when: "2026-08-04"`.
An unmapped key keeps its tag, because ferry never touches it.
That is [#155](https://github.com/onhotpath/ferry/issues/155).
The package doc lists all of them.

## Two types, and the path named twice

`NewSource(path)` and `NewSink(path)` are separate types over the same file, so the path is written twice.
That is the price of a caller who holds only a `Source` being unable to dump through it: passing one to `ferry.Dump` does not compile, rather than failing at run time once something has already been opened.

## What the plane holds

All six of ferry's value kinds, and it is the only first-party plane that can produce a null, because `!!null` is a resolved tag.
Four observations that a stringified boundary collapses into one:

```yaml
nul: null    # Null
empty: ""    # String("")
value: 8080  # Number("8080")
             # a key that is not there is Absent
```

Bytes travel as `!!binary`, base64, and that is the only spelling this driver owns.

## The file is replaced atomically

A dump stages a temporary file beside the plane and renames it over the plane when the walk has finished.
A walk that succeeded replaces the file in one step, so nothing ever reads a half-written config.
A walk that failed leaves the file byte for byte as it was, with no temporary left behind.

## One limitation

A Go string is a sequence of bytes and does not have to be valid UTF-8.
A YAML string is Unicode, so a Go string that is not valid UTF-8 has no spelling here at all.
ferry refuses it at that one address, naming the address, rather than quietly changing the value or inventing a tag for it, and the file is left alone.
If you need to move arbitrary bytes through a YAML file, declare the field `[]byte`.
Tracked in [#157](https://github.com/onhotpath/ferry/issues/157) and recorded as a known limitation in [ADR-0005](../../docs/adr/0005-the-supported-type-set.md).

## Where the reasoning is

The package doc argues every choice above and is the place to read next.
The decisions it implements are [ADR-0003](../../docs/adr/0003-how-a-leaf-addresses-a-plane.md) for addressing, [ADR-0004](../../docs/adr/0004-source-and-sink.md) for the source and sink contract, [ADR-0005](../../docs/adr/0005-the-supported-type-set.md) for the type set, and [ADR-0013](../../docs/adr/0013-what-a-plane-holds-is-a-published-interface.md) for what a plane holds.
