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
// There is one more and it is bigger. A scalar's tag, at a key your struct maps,
// is dropped: `when: !!timestamp 2026-08-04` is saved back as
// `when: "2026-08-04"`. A key no field maps keeps its tag, because it is never
// touched.
//
// A file holding a stream of several documents is refused rather than
// half-written, because an address names a place in one of them.
//
// # Types survive the trip
//
// YAML resolves a scalar's tag, so `port: 8080` is a number, `port: "8080"` is
// a string, `debug: true` is a boolean, `tags: null` is an explicit null, and a
// key that is simply not there is different again. Each of the five crosses
// ferry as its own kind and comes back spelled the way it went in. A []byte
// field is saved as standard YAML !!binary, base64.
//
// # Saving is atomic, and it is durable
//
// A save writes a temporary file beside yours and renames it into place once
// everything has been written, so nothing ever reads a half-written config, and
// no temporary file is ever left behind. A save that fails leaves your file byte
// for byte as it was, with one exception, named below.
//
// Durability is the second promise. The new file's contents are flushed to the
// disk, and so is the directory entry that makes your path point at them, so a
// save that returned nil has reached the disk rather than the page cache.
// Windows has no way to flush a directory: there the contents are flushed and
// the durability of the rename is the filesystem's own business.
//
// The exception is a directory flush that fails once the rename has already
// landed. The save reports [ferry.ErrPlane] and your file holds the new
// document, because what could not be promised is that the replacement survives
// a crash, not that it happened.
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
