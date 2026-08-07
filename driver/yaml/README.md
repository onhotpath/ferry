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

A scalar's tag is not on that list.
A tag this package has no reading of its own for survives a save at a key your struct maps, so `when: !!timestamp 2026-08-04` is saved back with its tag still on it.
Three cases replace it: a tag this package writes itself, a tag whose value is no longer the kind it was, which is stale in the way the old quoting would be, and a tag your own field declared - see [A field can say what node type it is written as](#a-field-can-say-what-node-type-it-is-written-as).

Such a tag is carried and not interpreted: `!!timestamp 2026-08-04` and `!mycompany:duration 30s` both load as the string after the tag, whatever the tag says, and reach whichever codec your field declared.

## A list or a map is replaced whole

The editing stops at a list or a map your struct maps, because what is in one comes from your value and not from your type.
Save `[]string{"x"}` over

```yaml
tags:
  - a
  - b
  - c
```

and the file holds `tags: [x]`, not `[x, b, c]`.
A map that lost a key no longer holds that key either.
Anything else leaves the elements you dropped in the file, where they load straight back the next time out of a document that says something your value never did.
That was [#220](https://github.com/onhotpath/ferry/issues/220), and both legs of it were silent.

What stays is still yours.
The entry is edited where it already sits rather than written out fresh, so its comments, its anchor and its tag all survive:

```yaml
# the file
tags:
  - &first x # keep me
note: untouched
```

is what saving `[]string{"x"}` leaves of a three-element list whose first position carried that anchor and that comment.
A list is cut at the last position saved, which is what keeps the positions before it from being renumbered.

A struct's fields are not touched by this rule: a field your value leaves out is left exactly where it is, and so is every key no field of yours maps.

## An anchor is kept, so an alias to it moves

There is exactly one case where a key no field of yours maps does not read back as it did.

```yaml
host: &h localhost
other: *h
```

Save `host` as `example` and the file becomes:

```yaml
host: &h example
other: *h
```

`other`'s line is byte for byte what it was, and `other` now reads back as `example` rather than `localhost`.
That is what an anchor means: the operator who wrote `other: *h` said other is whatever host is, and the alias follows.

The alternative is worse.
Dropping the anchor leaves `*h` pointing at nothing, so the save reports success and writes a file that no reader can parse, including ferry's own load right afterwards.
That was [#196](https://github.com/onhotpath/ferry/issues/196).

It works the other way round too.
A key your struct maps that is itself an alias is written through to the anchor it names:

```yaml
base: &b 5432
port: *b
```

With `port` mapped, saving it as `5433` writes `base: &b 5433` and leaves the `port: *b` line exactly as it was.
The value moves and the linkage survives.
Saving it as its own value instead would have written `port: 5433` and quietly unshared the two, which was [#198](https://github.com/onhotpath/ferry/issues/198).

Two things follow from that.

**A save refuses where your struct and the document disagree.**
With both `base` and `port` above mapped, saving `1` and `2` asks the file to hold two values in one place.
It fails with `ferry.ErrPlane` naming the second address, and your file is left byte for byte as it was.
Saving the same value to both is fine, because that is what the document already says.

**An alias naming a scalar is replaced where the address needs a container.**

```yaml
base: &b 5432
db: *b
other: *b
```

With `db` mapped as a struct, saving it writes `db:` with your fields under it and leaves `base` and `other` alone.
Following the alias would rewrite `base` into a mapping under `other`, which no field maps, and it would keep nothing: a scalar has no keys of its own for the reshape to lose.

## Types survive the trip

A number stays a number, a quoted number stays a string, and `null` stays different from a key that is not there at all:

```yaml
nul: null    # an explicit null
empty: ""    # an empty string, which is not the same thing
value: 8080  # a number, and "8080" would be a string
             # a key that is simply missing is different again
```

A `[]byte` field is saved as standard YAML `!!binary`, base64.

## A field can say what node type it is written as

Keeping a tag the file already had is one thing.
Putting one there is another, and a save cannot guess it: what crosses ferry's boundary is a value and not a Go type, so `wait: 30s` says nothing about wanting `!mycompany:duration`.

`yaml.Extension()` is where a field says it.
Declare this package's struct tag key on a registry, annotate the field, and pass the registry to the call:

```go
registry := ferry.NewRegistry(ferry.WithTagKeys(yaml.Extension()))

type config struct {
	Wait string `ferry:"wait" yamlext:"node=!mycompany:duration"`
	Port int    `ferry:"port"`
}

cfg, err := ferry.Load[config](ctx, yaml.NewSource(path), ferry.WithRegistry(registry))
err = ferry.Dump(ctx, cfg, yaml.NewSink(path), ferry.WithRegistry(registry))
```

`wait: 30s` in, and out:

```yaml
wait: !mycompany:duration 30s
port: 8080
```

That is `ExampleExtension` in [`example_test.go`](example_test.go), trimmed of its setup.

The tag is written whether or not the file had one there, and a tag the file did have at that address loses to the declared one.
A load needs nothing, because the value arrives as the text after the tag either way - which is what lets the annotation survive a load, a save and a second load with nothing lost.

Four things it refuses.
A tag that is not a tag: one not starting with `!`, one naming nothing after it, or one with a space in it.
A tag this package spells itself, `!!str` and `!!int` and the rest, because the value's own kind decides those.
The address of a struct, a list or a map, because a node tag names how one value is written and those are places rather than values - annotate the fields under them.
And a value this plane writes as a number, a boolean or bytes, refused when that value is written, because a scalar under a tag this plane does not read comes back as a string and the value would not survive the trip.
A null is written plainly rather than refused, so an optional field that happens to be unset does not fail a save.

The key is `yamlext` and not `yaml`, which is the key go-yaml's own marshaller reads: a field may carry both.
A registry that was not given the declaration reads none of it, and a save writes what it always wrote.

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

## A save refuses a file somebody else edited

A save is a merge into the document that is already there, so it reads the file, stages the replacement and renames it into place.
An edit that lands in that window would be swapped away without a word.

So the save compares the file against what it read, one stat before the rename, and refuses with `ferry.ErrPlane` where it changed:

```
the plane changed after this save read it, and saving now would discard that change:
load the plane again, apply the same edits to what it holds now, and save again
```

Your file is left exactly as the other writer left it.
The one edit the check cannot see is a rewrite that lands in the same modification-time tick and leaves the file's length alone.

## Watching the file

Pass `yaml.Watch` to be called when the file changes underneath a source, which is how a process holding a loaded value learns to load a fresh one:

```go
src := yaml.NewSource(path, yaml.Watch(ctx, 10*time.Millisecond, onChange))

b, err := ferry.Bind[config](src)

// ... an operator edits the file ...

func onChange(ctx context.Context) {
	cfg, err := b.Load(ctx) // a reload is a load; publish it by replacement
}
```

That is `ExampleWatch` in [`example_test.go`](example_test.go), trimmed of its setup and its plumbing.

It is opt-in, and it is the only thing in this package that runs on a goroutine of its own.
Cancelling the context you gave it is what stops it.
Looking is a stat every interval rather than an fsnotify subscription, so that watching a file costs this module no dependency, and the interval is yours to name.

Two sharp edges are worth reading before you wire it up, and the rest are in the [package documentation](https://pkg.go.dev/github.com/onhotpath/ferry/driver/yaml#Watch).
Watching starts when the source is built, which is before `ferry.Bind` has handed back the binding your callback wants to load through, so publish that binding to the callback through something that orders the two.
And a panic in the callback takes the process down, exactly as it would on a goroutine you started yourself.

The loop this feeds, and the reasons ferry ships no watcher of its own, are in [the watch and reload guide](../../docs/guide/watch-reload.md).

## One thing it cannot do

A Go `string` can hold any bytes; a YAML string has to be valid text.
So a string field holding bytes that are not valid UTF-8 cannot be saved to YAML at all.

Rather than mangling the value or inventing a private YAML tag for it, saving fails and names the field, and your file is left alone.
If you need to move arbitrary bytes through a YAML file, declare that field `[]byte` instead, which is saved as `!!binary`.

This is a known limitation and is tracked in [#157](https://github.com/onhotpath/ferry/issues/157).
A node tag does not reach it: a `string` field carries no custom type to annotate, and which of your strings are not valid UTF-8 is not something you can know in advance.

## More

The [package documentation](https://pkg.go.dev/github.com/onhotpath/ferry/driver/yaml) is the reference for everything above, and the design records behind it are in [`docs/adr/`](../../docs/adr/).
