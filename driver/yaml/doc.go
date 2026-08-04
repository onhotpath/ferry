// Package yaml is ferry's YAML plane: a [Source] over a YAML file and a [Sink]
// that writes one back, over the same path named twice.
//
// # What this driver exists to prove
//
// It is the plane with a serialization format, and the one where the typed
// boundary pays for itself. YAML resolves a scalar's tag, so `port: 8080` is a
// number, `port: "8080"` is a string, `debug: true` is a boolean and `tags:
// null` is a null, and each of the four crosses [ferry.Value] as its own kind
// and comes back spelled the way it went in. A stringified boundary loses all
// four permanently: null becomes "", true becomes "true", 8080 becomes "8080".
// It is also the only first-party plane that can produce [ferry.KindNull] at
// all, because !!null is a resolved tag, so the Absent-versus-Null distinction
// is exercised here against something real (ADR-0006).
//
// # It walks segments and builds no plane key
//
// ADR-0003 puts an injectivity obligation on every driver that flattens an
// address into a plane key, and this driver carries none of it: an address is a
// path through the document tree, a Name segment is a mapping member and an
// Index segment is a sequence position, so two distinct addresses are two
// distinct places by construction. Nothing here calls core's key helper, and
// [Source.Bind] and [Sink.Bind] read nothing at all out of the address set they
// are handed. The asymmetry is worth saying out loud because it surprises: a
// tree plane pays nothing for the address set, where a flat plane pays for it
// once per schema.
//
// # What a Dump preserves, and what it does not
//
// A Dump is a merge into the document that is already there rather than a fresh
// write, which is what makes a config file survive being loaded and dumped
// back: comments, key ordering, the quoting of values ferry did not touch, and
// every key ferry does not map are all still there afterwards, because the
// addresses ferry writes are the only nodes that change. That is this driver's
// business rather than a core guarantee - a marshal through map[string]any
// destroys all four - and it is asserted here.
//
// What does not survive is the emitter's limits rather than decisions: a blank
// line between two entries is dropped, an explicit --- start marker is dropped,
// a document whose whole content is comments parses to nothing and is replaced,
// indentation is two spaces on the way out whatever it was on the way in, and a
// trailing comment keeps its text but not its column, so "keep: 1   # why"
// comes back as "keep: 1 # why".
// Flow style, block scalars and the quoting of a value ferry did not write are
// all kept. A plane holding a stream of several documents is refused rather
// than half-written, because an address names a place in one of them.
//
// # The one spelling this driver owns
//
// Bytes are written base64 under YAML's own !!binary tag, and that is the whole
// of it. The spelling is pinned by a golden artefact (ADR-0013), so changing an
// encoder and its decoder together turns CI red rather than silently rewriting
// what every stored file means.
//
// # A Go string that is not valid UTF-8 does not travel through this plane
//
// It is a known limitation rather than a defect, and it is recorded in ADR-0005.
// A Go string is a byte sequence and is not required to be UTF-8; a YAML string
// is Unicode, the stream itself has to be valid UTF-8, and the emitter refuses
// invalid UTF-8 under !!str by name. So this plane carries [ferry.KindString]
// and cannot carry every value of it.
//
// A dump of such a string is refused at that one address, with
// [ferry.ErrValue], and the error names the address so that a config with twenty
// strings says which one cannot travel. The dump as a whole then fails, so the
// plane is left byte for byte as it was: the staging below either replaces it or
// leaves it alone, and a partial write is not one of the outcomes.
//
// The two alternatives were both worse. Writing it as !!binary reads back as
// [ferry.KindBytes] and reaches a codec that asked for a string. Spelling it
// under a tag of this driver's own writes ferry's naming into an operator's data
// file and pins it as a compatibility promise, which is what ADR-0003 already
// refuses one level up for the canonical rendering. What would lift the
// limitation is a node-type mechanism the driver does not own alone, which is
// tracked in issue #156.
//
// # The lifecycle
//
// The sink stages: it writes a temporary file beside the plane and renames it
// over the plane on Commit, so a walk that succeeded replaces the plane and a
// walk that failed leaves it byte-identical, with no temporary left behind
// either way. Closed-without-commit is the abort signal, so the driver is never
// told that it failed (ADR-0004).
package yaml
