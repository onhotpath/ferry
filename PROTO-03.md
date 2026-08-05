# Prototype: the typed address split and the container lifecycle

`prototype/addresskinds` is a standalone module (out of `go.work`;
run `cd prototype/addresskinds && GOWORK=off go test .`) asserting the
session-03 board in fourteen tests and three benchmarks:

- ambient HOME cannot abort a load: the container question is
  unaskable under Get(LeafAddr), and presence is derived from the
  driver's bound key table, never the raw environment (#219);
- no phantom children: an env variable reaching deeper than a
  composite's elements is an orphan named loudly, and an exact-depth
  variable mints with its value intact (#235);
- a plane container where the schema wants a leaf is a kind mismatch
  refused with the address (#252);
- string vs []string address sets are typed members, no longer
  byte-identical (#239);
- tree presence answers Present for `db: {}` and Null for explicit
  null; flat presence is derived, and empty-but-present has no flat
  spelling (documented limitation, shown by test);
- the A2 hole demonstrated both ways: without a section touch a
  present-empty section degrades to absent across a dump; with the
  touch it survives; a plane without the spelling refuses the touch;
- references (yaml alias / symlink shape) resolve transparently in
  the driver with the indirection memoized for write-back, and a
  reference cycle refuses (#256's and #234's fix shape);
- the multimap mints index children from repetition in plane order,
  and a scalar at a repeated key refuses with the count (#193, #208);
- arrays classify as sections with compiled index children; [0]int
  refuses like struct{}, and element types are checked through
  composites (#255, #264, #260);
- S1/S2/S3 sizes: 16, 24, 16 bytes; dispatch benchmarks are within
  noise at zero allocations for all three.
