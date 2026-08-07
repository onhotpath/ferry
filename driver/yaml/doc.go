// Package yaml loads configuration from a YAML file into a Go struct, and
// writes it back without wrecking the file.
//
//	cfg, err := ferry.Load[Config](ctx, yaml.NewSource("config.yaml"))
//	err = ferry.Dump(ctx, cfg, yaml.NewSink("config.yaml"))
//
// [NewSource] reads and [NewSink] writes, so the path is named twice. That is
// deliberate: code handed only a [Source] cannot save through it, and passing
// one to [ferry.Dump] does not compile rather than failing halfway through a
// write.
//
// # Saving edits the file rather than replacing it
//
// Only the keys your struct names are touched. Comments, key order, flow style,
// block scalars, the quoting of values ferry did not write and every key no
// field maps are all still there afterwards. That is the reason to use this
// rather than marshalling a struct to YAML: a config file a person maintains
// stays a config file a person maintains.
//
// What does not survive is the YAML writer's limits rather than choices: a blank
// line between two entries, an explicit --- start marker, a file whose whole
// content is comments, the original indentation, which is two spaces on the way
// out, and the column a trailing comment sat in.
//
// A scalar's tag is not on that list. A tag this package has no reading of its
// own for survives a save at a key your struct maps, so `when: !!timestamp
// 2026-08-04` is saved back with its tag still on it. Three cases replace it: a
// tag this package writes itself, a tag whose value is no longer the kind it
// was, which is stale in the way the old quoting would be, and a tag your own
// field declared through [Extension].
//
// A file holding a stream of several documents is refused rather than
// half-written, because an address names a place in one of them.
//
// # A list or a map is replaced whole
//
// The editing stops at a list or a map your struct maps, because what is in one
// comes from your value rather than from your type. Saving []string{"x"} over
//
//	tags:
//	  - a
//	  - b
//	  - c
//
// leaves `tags: [x]` and not `[x, b, c]`, and a map that lost a key no longer
// holds that key in the file. Otherwise the elements you dropped would load
// straight back the next time, out of a file that says something your value
// never did.
//
// What stays is still yours: the comments on it, the anchor on it and the tag it
// was written under all survive, because the entry is edited where it already
// sits rather than written out fresh. A list is cut at the last position saved,
// which is what keeps the positions before it from being renumbered.
//
// A struct's fields are not touched by this. A field your value leaves out is
// left exactly where it is, and so is every key no field of yours maps.
//
// # An anchor is kept, so an alias to it moves
//
// An anchor you wrote on a value ferry replaces stays on it, and that is the one
// case where a key no field maps does not read back as it did. Given
// `host: &h localhost` and `other: *h`, saving host as "example" writes
// `host: &h example`, and `other` - whose line is byte for byte what it was -
// now reads back as "example". That is what an anchor means: other is whatever
// host is. Dropping the anchor instead would leave `*h` pointing at nothing and
// the file would no longer parse for any reader.
//
// It works the other way round too. Saving a key that is itself an alias writes
// through to the anchor it names, so given `base: &b 5432` and `port: *b`,
// saving port as 5433 writes `base: &b 5433` and leaves the `port: *b` line
// alone. The value moves and the linkage survives, which is the same reading of
// an anchor.
//
// Two consequences follow, and both are worth knowing before you write one.
//
// A save refuses where your struct disagrees with the document about two keys
// that share an anchor. If `base` and `port` above are both mapped and you save
// 1 and 2, the file can hold only one of them, so the save fails with
// [ferry.ErrPlane] naming the second address and your file is left as it was.
// Saving the same value to both is fine.
//
// An alias naming a scalar, at an address your struct says is a mapping or a
// list, is replaced rather than followed: given `base: &b 5432` and `db: *b`,
// saving db as a struct writes `db:` with your fields under it and leaves
// `base` alone. Following it would rewrite `base` itself under every other alias
// to it, and there is nothing to be gained, because a scalar has no keys of its
// own to keep.
//
// # A merge key is read, and written as an override
//
// A mapping that says `<<: *defaults` holds what defaults holds, and a load
// reads it that way. Given
//
//	defaults: &d
//	  host: localhost
//	  port: 1
//	db:
//	  <<: *d
//	  port: 5432
//
// db loads as host "localhost" and port 5432: the key the mapping spells itself
// wins, the merge fills in the rest, and `<<` is never a key of your own. A
// map-typed field over db holds host and port and no member named `<<`. Written
// as a list, `<<: [*a, *b]`, the earlier source wins; a source that merges in
// turn is followed too.
//
// A save does not write through one. Saving db's host writes a `host` key into
// db, which shadows the merged one, and leaves defaults exactly as it is -
// because writing through would move the value under every other mapping that
// merges defaults, which is not what one field of one struct asked for.
//
// The one place that costs you something is a map or a list, which a save
// replaces whole. The mapping's own members are what the replacement keeps, so a
// map-typed field over db is written back with host and port spelled out and the
// `<<` line gone. The values are the ones your value held either way; the line
// that produced them is not kept, because keeping it would leave behind every
// inherited key the replacement meant to drop. Model a merged mapping as a
// struct if the `<<` line has to survive.
//
// # Types survive the trip
//
// YAML resolves a scalar's tag, so `port: 8080` is a number, `port: "8080"` is
// a string, `debug: true` is a boolean, `tags: null` is an explicit null, and a
// key that is simply not there is different again. Each of the five crosses
// ferry as its own kind and comes back spelled the way it went in. A []byte
// field is saved as standard YAML !!binary, base64.
//
// A tag this package does not read is carried and not interpreted. `!!timestamp
// 2026-08-04` and `!mycompany:duration 30s` both load as the string after the
// tag, whatever the tag says, and reach whichever codec your field declared. So
// a field of a type that parses that text works today, and the tag itself
// changes nothing about how the value is read.
//
// # A field can say what node type it is written as
//
// Keeping a tag the file already had is one thing; putting one there is
// another, and a save cannot guess it: what crosses ferry's boundary is a value
// and not a Go type, so `wait: 30s` says nothing about wanting
// !mycompany:duration.
//
// [Extension] is where a field says it. Declare this package's struct tag key
// on a registry, annotate the field, and pass the registry to the call:
//
//	var registry = ferry.MustRegistry(ferry.WithTagKeys(yaml.Extension()))
//
//	type Config struct {
//	    Wait string `ferry:"wait" yamlext:"node=!mycompany:duration"`
//	}
//
//	err := ferry.Dump(ctx, cfg, yaml.NewSink(path), ferry.WithRegistry(registry))
//
// The save writes `wait: !mycompany:duration 30s`, whether or not the file had
// a tag there, and a tag it did have at that address loses to the declared one.
// A load needs nothing: the value arrives as the text after the tag either way,
// which is what lets the annotation survive a load, a save and a second load
// with nothing lost. Read [Extension] for what it refuses.
//
// # Saving is atomic, and durable if you ask
//
// A save writes a temporary file beside yours and renames it into place once
// everything has been written, so nothing ever reads a half-written config, and
// no temporary file is ever left behind. A save that fails leaves your file byte
// for byte as it was. That is unconditional and there is no way to switch it
// off.
//
// What is not unconditional is durability, which is a different promise and a
// far more expensive one. By default the replacement is handed to the operating
// system and lives in its cache until the kernel writes it out, exactly as any
// ordinary file write does, so a machine that loses power in that window comes
// back to the old document.
//
//	err = ferry.Dump(ctx, cfg, yaml.NewSink("config.yaml", yaml.Durable()))
//
// [Durable] buys the other promise: the new file's contents are flushed to the
// disk, and so is the directory entry that makes your path point at them, so a
// save that returned nil has reached the disk rather than the cache. It costs a
// disk flush, which is usually more expensive than everything else in the save
// put together. Windows has no way to flush a directory: there the contents are
// flushed and the durability of the rename is the filesystem's own business.
//
// A durable save has one case where a save that failed has still replaced your
// file, and it is the flush that fails once the rename has landed. It reports
// [ferry.ErrPlane], because what could not be promised is that the replacement
// survives a crash, not that it happened.
//
// # A save refuses a file somebody else edited
//
// A save is a merge into the document that is already there, so it reads the
// file, stages the replacement and renames it into place. An edit landing in
// that window would be swapped away without a word, so the save compares the
// file against what it read - one stat, before the rename - and reports
// [ferry.ErrPlane] where it changed, leaving your file exactly as the other
// writer left it. Load again, apply the same change to what the file holds now,
// and save again.
//
// The check is the file's length and modification time, so the one edit it
// cannot see is a rewrite in the same modification-time tick that leaves the
// length alone.
//
// # Watching the file
//
// [Watch] calls you back when the file changes underneath a source, which is how
// a process holding a loaded value learns to load a fresh one:
//
//	src := yaml.NewSource("config.yaml", yaml.Watch(ctx, time.Second, onChange))
//
//	func onChange(ctx context.Context) {
//	    cfg, err := b.Load(ctx) // a reload is a load; publish it by replacement
//	}
//
// It is opt-in and it is the only thing in this package that runs on a goroutine
// of its own: a source built without it touches the file only when a load asks
// it to. Cancelling the context you gave it is what stops it. Looking is a stat
// every interval rather than a subscription, so watching a file costs this
// module no dependency and the interval is yours to name.
//
// Read [Watch] before wiring one up. Two of its sharp edges bite immediately:
// watching starts when the source is built, which is before [ferry.Bind] has
// handed back the binding your callback wants to load through, and a panic in
// the callback takes the process down exactly as it would on a goroutine you
// started yourself.
//
// # One thing it cannot do
//
// A Go string can hold any bytes; a YAML string has to be valid text. So a
// string field holding bytes that are not valid UTF-8 cannot be saved to YAML
// at all. Rather than mangling the value, the save fails with [ferry.ErrValue]
// and names the field, and your file is left alone. Declare that field []byte
// instead if you need to move arbitrary bytes through a YAML file.
//
// The design records behind these decisions are in docs/adr/.
package yaml
