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
// It does not work the other way round. A key your struct maps that is itself an
// alias, as `port: *b` is, is saved as its own value and stops sharing the
// anchor: `base: &b 5432` with `port: *b` becomes `base: &b 5432` and
// `port: 5433`. The file still parses and still loads; what is lost is the
// linkage.
//
// # Types survive the trip
//
// YAML resolves a scalar's tag, so `port: 8080` is a number, `port: "8080"` is
// a string, `debug: true` is a boolean, `tags: null` is an explicit null, and a
// key that is simply not there is different again. Each of the five crosses
// ferry as its own kind and comes back spelled the way it went in. A []byte
// field is saved as standard YAML !!binary, base64.
//
// # Saving is atomic
//
// A save writes a temporary file beside yours and renames it into place once
// everything has been written, so nothing ever reads a half-written config. If
// the save fails, your file is left byte for byte as it was, and no temporary
// file is left behind.
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
