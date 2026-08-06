package ferry

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestPathString(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		path Path
		want string
	}{
		// The five renderings ADR-0003's worked table publishes.
		"single name":         {path: At("name"), want: "/name"},
		"two names":           {path: At("db", "host"), want: "/db/host"},
		"three names":         {path: At("db", "auth", "user"), want: "/db/auth/user"},
		"map key":             {path: At("limits", "rps"), want: "/limits/rps"},
		"index":               {path: At("tags").Elem(0), want: "/tags#0"},
		"empty path":          {path: At(), want: ""},
		"empty segment":       {path: At(""), want: "/"},
		"two empty segments":  {path: At("", ""), want: "//"},
		"separator in text":   {path: At("a/b"), want: "/a~1b"},
		"escape in text":      {path: At("a~b"), want: "/a~0b"},
		"index sep in text":   {path: At("a#b"), want: "/a~2b"},
		"escape lookalike":    {path: At("~1"), want: "/~01"},
		"every special byte":  {path: At("~/#"), want: "/~0~1~2"},
		"nul in text":         {path: At("a\x00b"), want: "/a\x00b"},
		"non-ascii in text":   {path: At("héllo"), want: "/héllo"},
		"index inside a path": {path: At("a").Elem(3).At("b"), want: "/a#3/b"},
		"two indices":         {path: At("a").Elem(0).Elem(1), want: "/a#0#1"},
		"large index":         {path: At("a").Elem(4294967295), want: "/a#4294967295"},
		"extends a path":      {path: At("db").At("auth", "user"), want: "/db/auth/user"},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := c.path.String(); got != c.want {
				t.Errorf("String() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestPathRenderingIsUnique(t *testing.T) {
	t.Parallel()

	// Every pair here is a distinct address that a naive rendering collapses,
	// or that ADR-0003 names as a hazard.
	cases := map[string][2]Path{
		"map key 0 against index 0":    {At("limits", "0"), At("limits").Elem(0)},
		"one segment against two":      {At("a/b"), At("a", "b")},
		"escaped against literal":      {At("a~1b"), At("a/b")},
		"empty segment against none":   {At(""), At()},
		"empty segment against two":    {At(""), At("", "")},
		"trailing empty segment":       {At("a"), At("a", "")},
		"case differs":                 {At("Host"), At("host")},
		"index against name of digits": {At("tags").Elem(10), At("tags", "10")},
		"nested against flat":          {At("db", "host"), At("db_host")},
	}

	for name, pair := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertDistinct(t, pair[0], pair[1])
		})
	}
}

func assertDistinct(t *testing.T, a, b Path) {
	t.Helper()

	if a == b {
		t.Fatalf("addresses compare equal: %q", a)
	}

	if a.String() == b.String() {
		t.Errorf("distinct addresses render alike: %q", a)
	}
}

func TestPathIsAMapKeyAndASetElement(t *testing.T) {
	t.Parallel()

	// The point of the type: no encoding step at the call site, and the text
	// "0" as a map key is a different place from the position 0.
	byAddr := map[Path]string{
		At("limits", "0"):      "the map key 0",
		At("limits").Elem(0):   "the first element",
		At("db", "host"):       "localhost",
		At("db", "auth", "us"): "root",
	}

	if got := byAddr[At("limits", "0")]; got != "the map key 0" {
		t.Errorf("map[/limits/0] = %q, want the map key 0", got)
	}

	if got := byAddr[At("limits").Elem(0)]; got != "the first element" {
		t.Errorf("map[/limits#0] = %q, want the first element", got)
	}

	// A path built a segment at a time is the same key as one built in one go.
	if got := byAddr[At("db").At("host")]; got != "localhost" {
		t.Errorf("map[/db/host] built stepwise = %q, want localhost", got)
	}

	if _, ok := byAddr[At("db", "port")]; ok {
		t.Error("map holds /db/port, which was never inserted")
	}

	set := map[Path]struct{}{At("db", "host"): {}}
	if _, ok := set[At("db", "host")]; !ok {
		t.Error("set does not hold /db/host")
	}
}

func TestPathSegments(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		path Path
		want []Segment
	}{
		"empty path": {path: At(), want: nil},
		"one name":   {path: At("name"), want: []Segment{{kind: Name, text: "name"}}},
		"empty segment": {
			path: At(""),
			want: []Segment{{kind: Name, text: ""}},
		},
		"two names": {
			path: At("db", "host"),
			want: []Segment{{kind: Name, text: "db"}, {kind: Name, text: "host"}},
		},
		"name then index": {
			path: At("tags").Elem(7),
			want: []Segment{{kind: Name, text: "tags"}, {kind: Index, text: "7"}},
		},
		"index then name": {
			path: At("s").Elem(1).At("host"),
			want: []Segment{{kind: Name, text: "s"}, {kind: Index, text: "1"}, {kind: Name, text: "host"}},
		},
		"escapes decode": {
			path: At("a/b", "c~d", "e#f"),
			want: []Segment{{kind: Name, text: "a/b"}, {kind: Name, text: "c~d"}, {kind: Name, text: "e#f"}},
		},
		"digits stay a name": {
			path: At("labels", "0"),
			want: []Segment{{kind: Name, text: "labels"}, {kind: Name, text: "0"}},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := slices.Collect(c.path.Segments()); !slices.Equal(got, c.want) {
				t.Errorf("Segments() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestPathSegmentsStopsEarly(t *testing.T) {
	t.Parallel()

	var seen []Segment

	for s := range At("db", "auth", "user").Segments() {
		seen = append(seen, s)

		break
	}

	want := []Segment{{kind: Name, text: "db"}}
	if !slices.Equal(seen, want) {
		t.Errorf("first segment only = %v, want %v", seen, want)
	}
}

func TestSegmentAccessors(t *testing.T) {
	t.Parallel()

	segs := slices.Collect(At("tags").Elem(4).Segments())

	if got := segs[0].Kind(); got != Name {
		t.Errorf("Kind() = %v, want Name", got)
	}

	if got := segs[0].Text(); got != "tags" {
		t.Errorf("Text() = %q, want tags", got)
	}

	if got := segs[1].Kind(); got != Index {
		t.Errorf("Kind() = %v, want Index", got)
	}

	if got := segs[1].Text(); got != "4" {
		t.Errorf("Text() = %q, want 4", got)
	}
}

func TestSegmentKindString(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		kind SegmentKind
		want string
	}{
		"name":         {kind: Name, want: "Name"},
		"index":        {kind: Index, want: "Index"},
		"out of range": {kind: SegmentKind(7), want: "SegmentKind(7)"},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := c.kind.String(); got != c.want {
				t.Errorf("String() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestSegmentTextIsComparedByExactBytes(t *testing.T) {
	t.Parallel()

	// Core never folds and never normalises. These are three addresses, not
	// one, and the survey measured a library destroying two of them.
	variants := []Path{At("myKey"), At("MyKey"), At("MYKEY")}

	set := make(map[Path]struct{}, len(variants))
	for _, p := range variants {
		set[p] = struct{}{}
	}

	if len(set) != len(variants) {
		t.Errorf("case variants collapsed: %d distinct of %d", len(set), len(variants))
	}

	// Unicode normalisation is not core's either: NFC and NFD spellings of the
	// same word are distinct segment texts.
	nfc, nfd := At("café"), At("café")
	if nfc == nfd {
		t.Error("NFC and NFD spellings compare equal")
	}
}

func TestPathCompare(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		a, b Path
		want int
	}{
		"equal":                     {a: At("db", "host"), b: At("db", "host"), want: 0},
		"empty is equal":            {a: At(), b: At(), want: 0},
		"empty sorts first":         {a: At(), b: At("a"), want: -1},
		"prefix sorts first":        {a: At("db"), b: At("db", "host"), want: -1},
		"container before its leaf": {a: At("tags"), b: At("tags").Elem(0), want: -1},
		"names by bytes":            {a: At("db", "host"), b: At("db", "port"), want: -1},
		"upper before lower":        {a: At("Host"), b: At("host"), want: -1},
		"indices numerically":       {a: At("t").Elem(2), b: At("t").Elem(10), want: -1},
		"two digit indices":         {a: At("t").Elem(9), b: At("t").Elem(11), want: -1},
		"index against itself":      {a: At("t").Elem(7), b: At("t").Elem(7), want: 0},
		"name before index":         {a: At("t", "0"), b: At("t").Elem(0), want: -1},
		// ADR-0003: segment-wise and rendering order disagree here, and this is
		// the direction segment-wise gives.
		"separator does not sort": {a: At("a", "b"), b: At("a-x"), want: -1},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertCompare(t, c.a, c.b, c.want)
		})
	}
}

// assertCompare checks the order both ways round, because an ordering that is
// not antisymmetric is not one.
func assertCompare(t *testing.T, a, b Path, want int) {
	t.Helper()

	if got := a.Compare(b); got != want {
		t.Errorf("%q.Compare(%q) = %d, want %d", a, b, got, want)
	}

	if got := b.Compare(a); got != -want {
		t.Errorf("%q.Compare(%q) = %d, want %d", b, a, got, -want)
	}
}

// twelveIndices is ADR-0003's measured case, handed over in reverse so that a
// sort has something to do.
func twelveIndices() []Path {
	const n = 12

	paths := make([]Path, 0, n)
	for i := range n {
		paths = append(paths, At("tags").Elem(uint(n-1-i)))
	}

	return paths
}

func TestOrderingIsSegmentWise(t *testing.T) {
	t.Parallel()

	assertSegmentWiseOrder(t, twelveIndices(), []string{
		"/tags#0", "/tags#1", "/tags#2", "/tags#3", "/tags#4", "/tags#5",
		"/tags#6", "/tags#7", "/tags#8", "/tags#9", "/tags#10", "/tags#11",
	})
}

// TestSortingTheRenderingIsNotSegmentWise is the half that makes the test above
// mean something: the rendering is for identity, and sorting it gives the order
// ADR-0003 calls a subtle bug.
func TestSortingTheRenderingIsNotSegmentWise(t *testing.T) {
	t.Parallel()

	assertRenderingOrder(t, twelveIndices(), []string{
		"/tags#0", "/tags#1", "/tags#10", "/tags#11", "/tags#2", "/tags#3",
		"/tags#4", "/tags#5", "/tags#6", "/tags#7", "/tags#8", "/tags#9",
	})
}

// TestTheTwoOrdersDisagreeOnPlainNames is the same disagreement with no index in
// sight: a separator byte sorts against ordinary text.
func TestTheTwoOrdersDisagreeOnPlainNames(t *testing.T) {
	t.Parallel()

	pair := []Path{At("a-x"), At("a", "b")}

	assertSegmentWiseOrder(t, pair, []string{"/a/b", "/a-x"})
	assertRenderingOrder(t, pair, []string{"/a-x", "/a/b"})
}

func assertSegmentWiseOrder(t *testing.T, paths []Path, want []string) {
	t.Helper()

	sorted := slices.Clone(paths)
	slices.SortFunc(sorted, Path.Compare)

	if got := renderings(sorted); !slices.Equal(got, want) {
		t.Errorf("segment-wise = %v, want %v", got, want)
	}
}

func assertRenderingOrder(t *testing.T, paths []Path, want []string) {
	t.Helper()

	texts := renderings(paths)
	slices.Sort(texts)

	if !slices.Equal(texts, want) {
		t.Errorf("by rendering = %v, want %v", texts, want)
	}
}

func TestParsePathAccepts(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty":           "",
		"one name":        "/name",
		"empty segment":   "/",
		"escapes":         "/a~0~1~2b",
		"index":           "/tags#0",
		"two digit index": "/tags#11",
		"index then name": "#3/host",
	}

	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertParsed(t, text)
		})
	}
}

// TestParsePathRejects covers what makes the rendering unique: only these
// escapes exist, and an Index text is canonical base-10.
func TestParsePathRejects(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"no leading delimiter":    "name",
		"trailing escape":         "/a~",
		"unknown escape":          "/a~3b",
		"escape at the very end":  "/~",
		"empty index":             "/tags#",
		"index with leading zero": "/tags#01",
		"index that is not one":   "/tags#a",
		"index part digits":       "/tags#1a",
		"negative index":          "/tags#-1",
	}

	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertNotParsed(t, text)
		})
	}
}

func assertParsed(t *testing.T, text string) {
	t.Helper()

	got, ok := parsePath(text)
	if !ok {
		t.Fatalf("parsePath(%q) rejected a canonical rendering", text)
	}

	if got.String() != text {
		t.Errorf("parsePath(%q).String() = %q", text, got)
	}
}

func assertNotParsed(t *testing.T, text string) {
	t.Helper()

	if got, ok := parsePath(text); ok {
		t.Errorf("parsePath(%q) accepted it, as %q", text, got)
	}
}

func TestAddressSet(t *testing.T) {
	t.Parallel()

	// A compiled schema's set: leaf addresses plus the container addresses
	// ADR-0003 puts in it, each typed by what can be asked at it, handed over
	// out of order and with one repeat.
	set := newAddressSet(
		leafOf(At("tags").Elem(10)),
		leafOf(At("db", "host")),
		compositeOf(At("tags")),
		leafOf(At("tags").Elem(2)),
		leafOf(At("db", "auth", "user")),
		leafOf(At("db", "host")),
		leafOf(At("name")),
		sectionOf(At("db")),
	)

	want := []string{
		"section /db", "leaf /db/auth/user", "leaf /db/host",
		"leaf /name", "composite /tags", "leaf /tags#2", "leaf /tags#10",
	}
	if got := kinded(set); !slices.Equal(got, want) {
		t.Errorf("Seq() = %v, want %v", got, want)
	}

	if got := set.Len(); got != len(want) {
		t.Errorf("Len() = %d, want %d", got, len(want))
	}

	if !set.Has(leafOf(At("tags").Elem(2))) {
		t.Error("Has(leaf /tags#2) = false")
	}

	if set.Has(leafOf(At("tags", "2"))) {
		t.Error("Has(leaf /tags/2) = true, and it was never in the set")
	}
}

// TestAddressSetPartitionsByKind is what makes the set answer the question a
// driver actually has. The kinds partition the address space, so one path under
// two kinds is two addresses and a set holding one answers nothing about the
// other (ADR-0016).
func TestAddressSetPartitionsByKind(t *testing.T) {
	t.Parallel()

	set := newAddressSet(sectionOf(At("db")), leafOf(At("db", "host")))

	if !set.Has(sectionOf(At("db"))) {
		t.Error("Has(section /db) = false, and the set was built with it")
	}

	if set.Has(compositeOf(At("db"))) {
		t.Error("Has(composite /db) = true, and a section is not a composite")
	}

	if set.Has(leafOf(At("db"))) {
		t.Error("Has(leaf /db) = true, and a container address holds no value")
	}
}

func TestAddressSetIsCopiedFromItsInput(t *testing.T) {
	t.Parallel()

	addrs := []Member{leafOf(At("b")), leafOf(At("a"))}

	set := newAddressSet(addrs...)
	addrs[0] = leafOf(At("z"))

	if got := kinded(set); !slices.Equal(got, []string{"leaf /a", "leaf /b"}) {
		t.Errorf("Seq() = %v after the caller reused its slice", got)
	}
}

func TestEmptyAddressSet(t *testing.T) {
	t.Parallel()

	set := newAddressSet()

	if got := set.Len(); got != 0 {
		t.Errorf("Len() = %d, want 0", got)
	}

	if set.Has(leafOf(At("a"))) {
		t.Error("Has(leaf /a) = true on an empty set")
	}

	if got := slices.Collect(set.Seq()); len(got) != 0 {
		t.Errorf("Seq() yielded %v", got)
	}
}

// TestANilAddressSetAnswersAsAnEmptyOne is the guard every accessor carries,
// asserted at all three rather than at the two a walk happens to reach.
//
// A driver holds the set core handed it and asks it questions later, so the
// nil a driver kept from a Bind it never completed answers rather than panics.
func TestANilAddressSetAnswersAsAnEmptyOne(t *testing.T) {
	t.Parallel()

	var set *AddressSet

	if got := set.Len(); got != 0 {
		t.Errorf("Len() = %d on a nil set, want 0", got)
	}

	if set.Has(leafAt(At("a"))) {
		t.Error("Has(leaf /a) = true on a nil set")
	}

	if got := slices.Collect(set.Seq()); len(got) != 0 {
		t.Errorf("Seq() yielded %v on a nil set", got)
	}
}

// kinded renders a set as its members' kinds and addresses, because two members
// at one path render alike and are not one address.
func kinded(a *AddressSet) []string {
	out := make([]string, 0, a.Len())
	for m := range a.Seq() {
		out = append(out, describe(m))
	}

	return out
}

// FuzzPath checks the two properties the canonical form is for: a rendering
// parses back to the address it came from, and two distinct segment sequences
// never render alike. The seeds are the cases ADR-0003 names - the separator,
// the escape character, escape lookalikes, an embedded NUL, non-ASCII and the
// empty string.
func FuzzPath(f *testing.F) {
	seeds := []string{
		"", "host", "/", "#", "~", "~0", "~1", "~2", "~~", "a/b", "a~1b", "a#0",
		"\x00", "a\x00b", "héllo", "0", "01", "*", "//", "~1~0",
	}

	for _, a := range seeds {
		for _, b := range seeds {
			f.Add(a, b, uint(0))
		}
	}

	f.Add("a", "b", uint(10))
	f.Add("", "", ^uint(0))

	f.Fuzz(func(t *testing.T, a, b string, i uint) {
		seen := make(map[string]string)

		for _, c := range fuzzPaths(a, b, i) {
			checkRoundTrip(t, c.path, c.segs)
			checkNoCollision(t, seen, c.path, c.segs)
		}
	})
}

type fuzzCase struct {
	segs []Segment
	path Path
}

// fuzzPaths builds every address reachable from two segment texts and one
// index, including the pairs most likely to collide: one segment holding both
// texts against two segments holding one each, and a name of digits against an
// index.
func fuzzPaths(a, b string, i uint) []fuzzCase {
	idx := Segment{kind: Index, text: strconv.FormatUint(uint64(i), 10)}
	na := Segment{kind: Name, text: a}
	nb := Segment{kind: Name, text: b}

	return []fuzzCase{
		{segs: []Segment{na}, path: At(a)},
		{segs: []Segment{nb}, path: At(b)},
		{segs: []Segment{na, nb}, path: At(a, b)},
		{segs: []Segment{nb, na}, path: At(b, a)},
		{segs: []Segment{{kind: Name, text: a + b}}, path: At(a + b)},
		{segs: []Segment{na, idx}, path: At(a).Elem(i)},
		{segs: []Segment{na, idx, nb}, path: At(a).Elem(i).At(b)},
		{segs: []Segment{na, {kind: Name, text: idx.text}}, path: At(a, idx.text)},
	}
}

func checkRoundTrip(t *testing.T, p Path, want []Segment) {
	t.Helper()

	got, ok := parsePath(p.String())
	if !ok {
		t.Fatalf("parsePath(%q) rejected its own rendering, from %v", p, want)
	}

	if got != p {
		t.Fatalf("parsePath(%q) = %q", p, got)
	}

	if segs := slices.Collect(p.Segments()); !slices.Equal(segs, want) {
		t.Fatalf("%q decodes to %v, want %v", p, segs, want)
	}
}

func checkNoCollision(t *testing.T, seen map[string]string, p Path, segs []Segment) {
	t.Helper()

	key := segKey(segs)

	if prev, ok := seen[p.String()]; ok && prev != key {
		t.Fatalf("%q renders both %s and %s", p, prev, key)
	}

	seen[p.String()] = key
}

// segKey identifies a segment sequence without going through the rendering
// under test, so the collision check does not assume what it is checking.
func segKey(segs []Segment) string {
	var b strings.Builder

	for _, s := range segs {
		b.WriteString(s.kind.String())
		b.WriteString(strconv.Quote(s.text))
	}

	return b.String()
}

func renderings(paths []Path) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, p.String())
	}

	return out
}
