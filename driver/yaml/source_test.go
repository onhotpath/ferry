package yaml_test

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/yaml"
)

// The document the read-side cases are asked about. It carries one of each
// observation this plane can make, plus the one spelling this driver owns.
const observed = `nul: null
empty: ""
value: 8080
quoted: "8080"
yes: true
ratio: 3.5
bin: !!binary aGk=
when: 2026-08-02T12:00:00Z
list:
  - a
  - b
map:
  k: v
`

// TestObservations is the four distinct observations at four addresses that the
// typed boundary exists for, plus the kinds only a resolved tag can produce.
//
// A stringified boundary collapses the first four into one: null becomes "",
// 8080 becomes "8080", the quoted 8080 becomes the same "8080", and the missing
// key becomes "" as well. Here they are four values and five kinds.
func TestObservations(t *testing.T) {
	r := openReader(t, write(t, observed))
	a := addressesOf[scalars](t)

	cases := []struct {
		name string
		addr ferry.Path
		want ferry.Value
	}{
		{"an explicit null is Null", ferry.At("nul"), ferry.Null},
		{"an empty quoted string is an empty String", ferry.At("empty"), ferry.String("")},
		{"an unquoted number is a Number", ferry.At("value"), ferry.Number("8080")},
		{"a key that is not there is Absent", ferry.At("missing"), ferry.Value{}},
		{"a quoted number is a String", ferry.At("quoted"), ferry.String("8080")},
		{"a resolved bool is a Bool", ferry.At("yes"), ferry.Bool(true)},
		{"a float keeps the plane's own spelling", ferry.At("ratio"), ferry.Number("3.5")},
		{"a !!binary is Bytes", ferry.At("bin"), ferry.Bytes([]byte("hi"))},
		{"a timestamp is a String, because ferry's time codec reads one", ferry.At("when"),
			ferry.String("2026-08-02T12:00:00Z")},
		{"an element of a sequence is read by position", ferry.At("list").Elem(1), ferry.String("b")},
		{"a member of a mapping is read by name", ferry.At("map", "k"), ferry.String("v")},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { holds(t, r, a.leaf(t, c.addr), c.want) })
	}
}

// holds is one observation, lifted out of its table so that the table stays a
// table: a subtest body counts against the enclosing function's complexity.
func holds(t *testing.T, r ferry.Reader, addr ferry.LeafAddr, want ferry.Value) {
	t.Helper()

	got, err := r.Get(t.Context(), addr)
	if err != nil {
		t.Fatalf("Get(%s): %v", addr, err)
	}

	if got != want {
		t.Errorf("Get(%s) = %#v, want %#v", addr, got, want)
	}
}

// TestAContainerWhereTheSchemaWantsAValueIsRefused is #252, at this plane.
//
// A mapping or a sequence at an address the schema types as a leaf used to
// answer Absent, and absence does not write, so core filled the field with the
// Go zero and the load returned nil: `limits: {http: {port: 1}, rps: "9"}` into
// a map[string]string loaded {"http": "", "rps": "9"} and the port was gone.
// The driver reports what the plane holds, and a container is not an absence.
func TestAContainerWhereTheSchemaWantsAValueIsRefused(t *testing.T) {
	r := openReader(t, write(t, observed))
	a := addressesOf[leafShapes](t)

	for _, at := range []ferry.Path{ferry.At("list"), ferry.At("map")} {
		got, err := r.Get(t.Context(), a.leaf(t, at))
		if err == nil {
			t.Fatalf("Get at %s answered %#v, where the document holds a container: a value that is not "+
				"there and a container that is are two different observations", at, got)
		}

		if !errors.Is(err, ferry.ErrValue) {
			t.Errorf("Get at %s failed with %v, want an error carrying ferry.ErrValue", at, err)
		}
	}
}

// TestProbe is the container half of the read boundary: what this plane answers
// at an address whose children come from the type or from the document.
//
// All three answers are distinguishable here, which is what makes this driver
// the one that pins them. An empty mapping is present rather than absent, and
// that row is the one a present-but-empty section round trips through.
func TestProbe(t *testing.T) {
	r := openReader(t, write(t, observed+"section: {}\n"))
	a := addressesOf[containers](t)

	p, ok := r.(ferry.Prober)
	if !ok {
		t.Fatal("the reader does not probe, and a plane that cannot say whether a section is there is one no " +
			"optional section can be loaded from")
	}

	cases := []struct {
		name string
		addr ferry.Container
		want ferry.SectionInfo
	}{
		{"a sequence is present", a.composite(t, ferry.At("list")), ferry.SectionPresent},
		{"a mapping is present", a.composite(t, ferry.At("map")), ferry.SectionPresent},
		{"an empty mapping is present", a.section(t, ferry.At("section")), ferry.SectionPresent},
		{"an explicit null is null", a.composite(t, ferry.At("nul")), ferry.SectionNull},
		{"a key that is not there is absent", a.composite(t, ferry.At("missing")), ferry.SectionAbsent},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { probes(t, p, c.addr, c.want) })
	}
}

// probes is one probe, lifted out of its table for [holds]'s reason.
func probes(t *testing.T, p ferry.Prober, addr ferry.Container, want ferry.SectionInfo) {
	t.Helper()

	got, err := p.Probe(t.Context(), addr)
	if err != nil {
		t.Fatalf("Probe(%s): %v", addr, err)
	}

	if got != want {
		t.Errorf("Probe(%s) = %#v, want %#v", addr, got, want)
	}
}

// TestAValueWhereTheSchemaWantsAContainerIsRefused is [TestProbe]'s mirror, and
// it is the same disagreement seen from the other side: the document holds one
// value where the destination takes a container.
func TestAValueWhereTheSchemaWantsAContainerIsRefused(t *testing.T) {
	r := openReader(t, write(t, observed))
	a := addressesOf[containers](t)

	p, ok := r.(ferry.Prober)
	if !ok {
		t.Fatal("the reader does not probe")
	}

	got, err := p.Probe(t.Context(), a.composite(t, ferry.At("value")))
	if err == nil {
		t.Fatalf("Probe at a scalar answered %#v, want a refusal", got)
	}

	if !errors.Is(err, ferry.ErrValue) {
		t.Errorf("Probe at a scalar failed with %v, want an error carrying ferry.ErrValue", err)
	}
}

// The two destinations whose members the type fixes, and which the wrong
// collection is read into.
type (
	// optionalSection carries a sibling whose name begins with the section's,
	// which is a key beside the section and no member of it: the renderings say
	// so, because a segment ends at a delimiter and /optsx continues past /opts
	// in the middle of one.
	optionalSection struct {
		Opts  *sectionMembers `ferry:"opts"`
		OptsX string          `ferry:"optsx"`
	}

	sectionMembers struct {
		Host string `ferry:"host"`
	}

	// fixedArray puts the array behind a pointer, because a container question
	// is asked at a position that can be nil and a value-typed one is walked
	// without one.
	fixedArray struct {
		Pair *[2]int `ferry:"pair"`
	}
)

// TestTheWrongCollectionAtASectionIsRefused is [TestAContainerWhereTheSchemaWantsAValueIsRefused]
// at the container mirror.
//
// A struct's members are named and an array's are positions, so a sequence at
// the one and a mapping at the other are the document and the destination
// disagreeing about the shape of the data. Answering present for either builds
// the section out of nothing, fills every member with the Go zero and drops what
// the document held, with nothing saying so.
func TestTheWrongCollectionAtASectionIsRefused(t *testing.T) {
	t.Run("a sequence where the members are named", func(t *testing.T) {
		_, err := ferry.Load[optionalSection](t.Context(), yaml.NewSource(write(t, "opts: [1, 2]\n")))
		refusesShape(t, err)
	})

	t.Run("a mapping where the members are positions", func(t *testing.T) {
		_, err := ferry.Load[fixedArray](t.Context(), yaml.NewSource(write(t, "pair: {x: 1}\n")))
		refusesShape(t, err)
	})

	t.Run("a sibling sharing the section's name is not the section", func(t *testing.T) {
		got, err := ferry.Load[optionalSection](t.Context(), yaml.NewSource(write(t, "optsx: v\n")))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}

		if got.Opts != nil || got.OptsX != "v" {
			t.Errorf("loaded %+v, want the sibling filled and the section left nil", got)
		}
	})
}

// refusesShape asserts that a load failed with this plane's own class for a
// document whose shape the destination cannot take.
func refusesShape(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("Load answered without error, where the document holds the collection the destination's members " +
			"do not live in")
	}

	if !errors.Is(err, ferry.ErrValue) {
		t.Errorf("Load failed with %v, want an error carrying ferry.ErrValue", err)
	}
}

// TestChildren asserts that enumeration answers with segments carrying their
// kind, so that a sequence position and a mapping member stay different
// answers.
func TestChildren(t *testing.T) {
	r := openReader(t, write(t, observed))
	a := addressesOf[containers](t)

	e, ok := r.(ferry.Enumerator)
	if !ok {
		t.Fatal("the reader does not enumerate, and a plane that cannot list is one no map can be loaded from")
	}

	cases := []struct {
		name   string
		prefix ferry.CompositeAddr
		want   []ferry.Segment
	}{
		{"a sequence answers with positions", a.composite(t, ferry.At("list")),
			[]ferry.Segment{ferry.IndexSegment(0), ferry.IndexSegment(1)}},
		{"a mapping answers with names", a.composite(t, ferry.At("map")),
			[]ferry.Segment{ferry.NameSegment("k")}},
		{"a scalar has no children", a.composite(t, ferry.At("value")), nil},
		{"an address that is not there has no children", a.composite(t, ferry.At("missing")), nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { lists(t, e, c.prefix, c.want) })
	}
}

// lists is one enumeration, lifted out of its table for [holds]'s reason.
func lists(t *testing.T, e ferry.Enumerator, prefix ferry.CompositeAddr, want []ferry.Segment) {
	t.Helper()

	got, err := e.Children(t.Context(), prefix)
	if err != nil {
		t.Fatalf("Children(%s): %v", prefix, err)
	}

	if !segmentsEqual(got, want) {
		t.Errorf("Children(%s) = %v, want %v", prefix, got, want)
	}
}

// TestMalformedDocument is the ADR-0014 case 4 shape a suite cannot stage from
// outside: the driver's own parse failure.
//
// It must fail, it must fail inside the open rather than at Bind, and the class
// is ErrValue rather than ErrPlane - ferry reached the plane and read it, and
// what did not parse is the operator's file. Survey item 5.11 found a YAML
// provider discarding exactly this error and answering with an empty result.
func TestMalformedDocument(t *testing.T) {
	path := write(t, "key: [unclosed\n")

	open, err := yaml.NewSource(path).Bind(addressesOf[scalars](t).set)
	if err != nil {
		t.Fatalf("Bind refused a legal address set over a plane it has not read: %v", err)
	}

	r, err := open(t.Context())
	if err == nil {
		t.Fatalf("opening a plane that is not a YAML document succeeded, and answered with %v", r)
	}

	if !errors.Is(err, ferry.ErrValue) {
		t.Errorf("opening a malformed document failed with %v, want an error carrying ferry.ErrValue: a document "+
			"that does not parse is the operator's file and not an unreachable plane", err)
	}

	if errors.Is(err, ferry.ErrPlane) {
		t.Errorf("opening a malformed document declared ferry.ErrPlane (%v), which is what core would have "+
			"defaulted to and what the driver is here to overrule", err)
	}
}

// TestMalformedDocumentThroughLoad asserts the same failure survives to a
// caller of ferry.Load, which is where anybody outside this package meets it.
func TestMalformedDocumentThroughLoad(t *testing.T) {
	type config struct {
		Key string `ferry:"key"`
	}

	_, err := ferry.Load[config](t.Context(), yaml.NewSource(write(t, "key: [unclosed\n")))
	if err == nil {
		t.Fatal("loading from a malformed document succeeded, so every field is at its zero value and nothing " +
			"says why")
	}

	if !errors.Is(err, ferry.ErrValue) {
		t.Errorf("Load reported %v, want an error carrying ferry.ErrValue", err)
	}
}

// TestADuplicatedKeyIsRefused is #257: the file said one thing to yaml.v3, which
// refuses it outright, another to every reader that takes the last spelling, and
// a third to ferry, which took the first and reported nothing.
//
// A save made that worse: it rewrote the occurrence an address reaches and left
// the other, so the file came back holding the new value and the old one, and
// the old one is the one a last-wins reader believes.
func TestADuplicatedKeyIsRefused(t *testing.T) {
	type config struct {
		Host string `ferry:"host"`
		Port int    `ferry:"port"`
	}

	_, err := ferry.Load[config](t.Context(), yaml.NewSource(write(t, "host: a\nport: 1\nhost: b\n")))
	if err == nil {
		t.Fatal("loading a document that spells one key twice succeeded, and the file says two different things")
	}

	if !errors.Is(err, ferry.ErrValue) {
		t.Errorf("Load reported %v, want an error carrying ferry.ErrValue: a document ferry could read and cannot "+
			"make sense of is the operator's file", err)
	}

	if errors.Is(err, ferry.ErrPlane) {
		t.Errorf("Load declared ferry.ErrPlane (%v), which is what core would have defaulted to", err)
	}
}

// TestADuplicatedKeyIsRefusedWhereverItIs asserts the refusal is about the
// document and not about the addresses: a save re-emits the whole file, so a
// duplicate under a key no field maps is one a save would rewrite half of.
func TestADuplicatedKeyIsRefusedWhereverItIs(t *testing.T) {
	type config struct {
		Host string `ferry:"host"`
	}

	docs := []string{
		"host: a\nnested:\n  k: 1\n  k: 2\n",
		"host: a\nlist:\n  - k: 1\n    k: 2\n",
	}

	for _, doc := range docs {
		if _, err := ferry.Load[config](t.Context(), yaml.NewSource(write(t, doc))); !errors.Is(err, ferry.ErrValue) {
			t.Errorf("loading %q reported %v, want an error carrying ferry.ErrValue", doc, err)
		}
	}
}

// TestADuplicatedKeyRefusesTheSaveAtTheOpen is the write half, and it is the
// half the refusal exists for: the refusal lands before anything is staged, so
// the operator's file is byte-identical and no temporary is left behind.
func TestADuplicatedKeyRefusesTheSaveAtTheOpen(t *testing.T) {
	type config struct {
		Host string `ferry:"host"`
		Port int    `ferry:"port"`
	}

	before := "host: a\nport: 1\nhost: b\n"
	path := write(t, before)

	err := ferry.Dump(t.Context(), config{Host: "z", Port: 9}, yaml.NewSink(path))
	if err == nil {
		t.Fatal("a save into a document that spells one key twice succeeded, and it rewrites one of the two")
	}

	if !errors.Is(err, ferry.ErrValue) {
		t.Errorf("Dump reported %v, want an error carrying ferry.ErrValue", err)
	}

	if got := read(t, path); got != before {
		t.Errorf("the plane holds %q, want %q: a save that refused leaves the file byte-identical", got, before)
	}
}

// TestTwoKeysThatMerelyLookAlikeAreTwoKeys is the boundary the refusal must not
// cross. Core never folds or normalises segment text, so two case-variant keys
// are two members and a document holding both is a document this driver reads.
func TestTwoKeysThatMerelyLookAlikeAreTwoKeys(t *testing.T) {
	type config struct {
		Upper string `ferry:"Host"`
		Lower string `ferry:"host"`
	}

	got, err := ferry.Load[config](t.Context(), yaml.NewSource(write(t, "Host: a\nhost: b\n")))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if want := (config{Upper: "a", Lower: "b"}); got != want {
		t.Errorf("loaded %+v, want %+v", got, want)
	}
}

// TestAKeyThatIsNotAScalarIsPassedOver is the other boundary. YAML lets a
// mapping key be a collection, no address reaches one, and the refusal is about
// which of two occurrences an address reads - so a document holding one is read
// exactly as it was before, and the key beside it still loads.
func TestAKeyThatIsNotAScalarIsPassedOver(t *testing.T) {
	type config struct {
		Host string `ferry:"host"`
	}

	got, err := ferry.Load[config](t.Context(), yaml.NewSource(write(t, "? [a, b]\n: v\nhost: a\n")))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.Host != "a" {
		t.Errorf("loaded %+v, want host a", got)
	}
}

// TestMissingFileHoldsNothing asserts that a plane with no file at it opens and
// reports absence, rather than failing.
//
// Absent is how a plane says it does not hold an address, and a config file
// that has not been written yet holds none of them. It is also what makes a
// Dump to a fresh path the way the first file gets written.
func TestMissingFileHoldsNothing(t *testing.T) {
	r := openReader(t, filepath.Join(t.TempDir(), "absent.yaml"))

	got, err := r.Get(t.Context(), addressesOf[scalars](t).leaf(t, ferry.At("anything")))
	if err != nil {
		t.Fatalf("Get against a plane with no file: %v", err)
	}

	if got != (ferry.Value{}) {
		t.Errorf("Get against a plane with no file answered %#v, want absent", got)
	}
}

// TestAliasIsFollowed asserts that a document using YAML's anchors reads as the
// values it names rather than as a subtree of absences.
func TestAliasIsFollowed(t *testing.T) {
	r := openReader(t, write(t, "base: &b\n  host: localhost\nnext: *b\n"))

	got, err := r.Get(t.Context(), addressesOf[scalars](t).leaf(t, ferry.At("next", "host")))
	if err != nil {
		t.Fatalf("Get through an alias: %v", err)
	}

	if want := ferry.String("localhost"); got != want {
		t.Errorf("Get through an alias = %#v, want %#v", got, want)
	}
}

// The document #234 reproduced with: one key the mapping spells itself, and one
// it inherits through YAML's merge key.
const inherited = `defaults: &d
  host: localhost
  port: 1
db:
  <<: *d
  port: 5432
`

// The same document with its numbers written as strings, which is what a
// map[string]string destination can take one of.
const inheritedText = `defaults: &d
  host: localhost
  port: "1"
db:
  <<: *d
  port: "5432"
`

// The two destinations that document is read into: the one whose members the
// type fixes, and the one whose members the document does.
type (
	inheritedSection struct {
		DB inheritedMembers `ferry:"db"`
	}

	inheritedMembers struct {
		Host string `ferry:"host"`
		Port int    `ferry:"port"`
	}

	inheritedMap struct {
		DB map[string]string `ferry:"db"`
	}
)

// TestAMergeKeyIsResolved is #234's first outcome: a key the document supplies
// through `<<` was absent, so a field the file does hold loaded as its zero
// value with nothing saying so.
//
// A merge key is a standard YAML feature and not an unknown tag: the document
// says db holds what defaults holds, and a driver that reads the document has
// to read that too.
func TestAMergeKeyIsResolved(t *testing.T) {
	got, err := ferry.Load[inheritedSection](t.Context(), yaml.NewSource(write(t, inherited)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := inheritedSection{DB: inheritedMembers{Host: "localhost", Port: 5432}}
	if got != want {
		t.Errorf("loaded %+v, want %+v: the mapping's own key wins and the inherited one fills the rest", got, want)
	}
}

// TestAMergeKeyIsNotAMember is #234's second outcome, and the worse of the two:
// `<<` was enumerated as a data key, so a map-typed destination came back
// holding a member literally named `<<` whose value the driver invented.
func TestAMergeKeyIsNotAMember(t *testing.T) {
	got, err := ferry.Load[inheritedMap](t.Context(), yaml.NewSource(write(t, inheritedText)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := map[string]string{"host": "localhost", "port": "5432"}
	if !maps.Equal(got.DB, want) {
		t.Errorf("loaded %v, want %v: `<<` is YAML's syntax and never a key the document holds", got.DB, want)
	}
}

// TestMergedKeysAreEnumeratedAfterTheMappingsOwn asserts the order enumeration
// answers in, which is the order the merge resolves in: what the mapping spells
// itself, in the document's order, and then what it inherits.
func TestMergedKeysAreEnumeratedAfterTheMappingsOwn(t *testing.T) {
	r := openReader(t, write(t, inherited))
	a := addressesOf[inheritedMap](t)

	e, ok := r.(ferry.Enumerator)
	if !ok {
		t.Fatal("the reader does not enumerate")
	}

	lists(t, e, a.composite(t, ferry.At("db")),
		[]ferry.Segment{ferry.NameSegment("port"), ferry.NameSegment("host")})
}

// TestMergeSourcesResolveInOrder is the rest of YAML's own rule for `<<`, at the
// two shapes a single key is written in: a sequence of sources, where an earlier
// one wins over a later one, and a source that merges in turn.
func TestMergeSourcesResolveInOrder(t *testing.T) {
	cases := []struct {
		name string
		doc  string
	}{
		{"a sequence of sources takes the earlier one", `first: &f
  host: localhost
second: &s
  host: elsewhere
  port: 5432
db:
  <<: [*f, *s]
`},
		{"a source that merges in turn is followed", `base: &b
  host: localhost
mid: &m
  <<: *b
  port: 5432
db:
  <<: *m
`},
		{"a mapping written out rather than aliased is a source", `db:
  <<:
    host: localhost
    port: 5432
`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { inherits(t, c.doc) })
	}
}

// inherits loads one merged document and asserts the two members reached the
// destination, lifted out of its table for [holds]'s reason.
func inherits(t *testing.T, doc string) {
	t.Helper()

	got, err := ferry.Load[inheritedSection](t.Context(), yaml.NewSource(write(t, doc)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if want := (inheritedSection{DB: inheritedMembers{Host: "localhost", Port: 5432}}); got != want {
		t.Errorf("loaded %+v, want %+v", got, want)
	}
}

// TestAKeyNamedLikeTheMergeKeyIsAbsent is the same rule reached from the other
// side: `<<` is not a member the document holds, so a field that names it reads
// as absent rather than as whatever the merge brought in.
func TestAKeyNamedLikeTheMergeKeyIsAbsent(t *testing.T) {
	type db struct {
		Merge string `ferry:"<<"`
	}

	type config struct {
		DB db `ferry:"db"`
	}

	got, err := ferry.Load[config](t.Context(), yaml.NewSource(write(t, inherited)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.DB.Merge != "" {
		t.Errorf("loaded %q at db.<<, want nothing: a YAML syntax token is not a key the document holds",
			got.DB.Merge)
	}
}

// TestEverySourceOfASequenceMergeIsEnumerated is the enumeration half of the
// sequence case: a map-typed destination holds the members of every source, and
// the earlier source's value is the one that survives a name they share.
func TestEverySourceOfASequenceMergeIsEnumerated(t *testing.T) {
	doc := "first: &f\n  host: \"localhost\"\nsecond: &s\n  host: \"elsewhere\"\n  port: \"5432\"\n" +
		"db:\n  <<: [*f, *s]\n"

	got, err := ferry.Load[inheritedMap](t.Context(), yaml.NewSource(write(t, doc)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := map[string]string{"host": "localhost", "port": "5432"}
	if !maps.Equal(got.DB, want) {
		t.Errorf("loaded %v, want %v", got.DB, want)
	}
}

// TestAMergeChainStopsAtTheBound is the bound the read is given: a chain of
// merges deeper than this driver follows stops, and the load answers with what
// it reached rather than spinning. An operator's file never gets near it, and a
// document this driver did not parse cannot use it to hang a load.
func TestAMergeChainStopsAtTheBound(t *testing.T) {
	var doc strings.Builder

	doc.WriteString("k0: &k0\n  host: localhost\n")

	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&doc, "k%d: &k%d\n  <<: *k%d\n", i, i, i-1)
	}

	doc.WriteString("db:\n  <<: *k40\n  port: 5432\n")

	got, err := ferry.Load[inheritedSection](t.Context(), yaml.NewSource(write(t, doc.String())))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if want := (inheritedSection{DB: inheritedMembers{Port: 5432}}); got != want {
		t.Errorf("loaded %+v, want %+v: a chain past the bound supplies nothing rather than spinning", got, want)
	}
}

// TestAMergeKeyThatNamesNoMappingSuppliesNothing is the bound on following one.
// A `<<` naming a scalar merges nothing, and a chain of them that never ends
// stops rather than spinning: neither is a document this driver can read members
// out of, and neither may make a load hang.
func TestAMergeKeyThatNamesNoMappingSuppliesNothing(t *testing.T) {
	got, err := ferry.Load[inheritedMap](t.Context(), yaml.NewSource(write(t, "db:\n  <<: 8080\n  port: \"1\"\n")))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if want := map[string]string{"port": "1"}; !maps.Equal(got.DB, want) {
		t.Errorf("loaded %v, want %v", got.DB, want)
	}
}

// TestUnreadableScalars asserts that a value this plane cannot read is a loud
// refusal rather than a plausible wrong answer.
func TestUnreadableScalars(t *testing.T) {
	cases := []struct {
		name string
		doc  string
	}{
		{"a hand-written !!bool that is neither true nor false", "v: !!bool yes\n"},
		{"a !!binary that is not base64", "v: !!binary not-base64!\n"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { refuses(t, c.doc) })
	}
}

// refuses reads the one address a document holds and asserts a refusal, lifted
// out of its table for [holds]'s reason.
func refuses(t *testing.T, doc string) {
	t.Helper()

	got, err := openReader(t, write(t, doc)).Get(t.Context(), addressesOf[scalars](t).leaf(t, ferry.At("v")))
	if err == nil {
		t.Fatalf("Get answered %#v, want a refusal", got)
	}

	if !errors.Is(err, ferry.ErrValue) {
		t.Errorf("Get failed with %v, want an error carrying ferry.ErrValue", err)
	}
}

// write puts a document in a directory of its own and hands back the path.
func write(t *testing.T, doc string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "plane.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	return path
}

// openReader binds a source over the plane and opens it, which is the two
// phases a load goes through.
func openReader(t *testing.T, path string) ferry.Reader {
	t.Helper()

	open, err := yaml.NewSource(path).Bind(addressesOf[scalars](t).set)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	r, err := open(t.Context())
	if err != nil {
		t.Fatalf("opening the plane: %v", err)
	}

	return r
}

// segmentsEqual compares two member lists, treating nil and empty as one
// answer: a plane with nothing under an address has no children either way.
func segmentsEqual(got, want []ferry.Segment) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}
