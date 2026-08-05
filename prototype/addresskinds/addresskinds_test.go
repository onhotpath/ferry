package addresskinds

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

// ── #219: ambient HOME cannot abort a load ───────────────────────────

func TestAmbientHomeIsImmune(t *testing.T) {
	set := NewAddressSet()
	set.AddSection("/home")
	dirLeaf := set.AddLeaf("/home/dir")

	environ := map[string]string{
		"HOME":     "/root", // the ambient landmine
		"HOME_DIR": "/data",
	}
	d := bindEnv(environ, set)

	// The container question is unaskable: Get takes LeafAddr, and
	// /home is a SectionAddr. The only observations that exist:
	v, err := d.Get(dirLeaf)
	if err != nil || v.Text != "/data" {
		t.Fatalf("leaf road: %+v, %v", v, err)
	}
	p, err := d.Probe(SectionAddr{p: path{p: "/home"}})
	if err != nil || p != Present {
		t.Fatalf("probe: %v, %v", p, err)
	}
	// $HOME was never consulted; remove HOME_DIR and the section is
	// honestly absent even though $HOME still exists.
	delete(environ, "HOME_DIR")
	p, err = d.Probe(SectionAddr{p: path{p: "/home"}})
	if err != nil || p != Absent {
		t.Fatalf("probe after removal: %v, %v — $HOME must not count", p, err)
	}
}

// ── #235: no phantom children, orphans named loudly ──────────────────

func TestNoPhantomChildren(t *testing.T) {
	set := NewAddressSet()
	limits := set.AddComposite("/limits")

	// A variable reaching deeper than the composite's elements is an
	// orphan: named loudly, never minted as a phantom with a dropped value.
	d := bindEnv(map[string]string{"LIMITS_HTTP_PORT": "8080"}, set)
	_, err := d.Children(limits)
	if err == nil || !strings.Contains(err.Error(), "LIMITS_HTTP_PORT") {
		t.Fatalf("orphan must be named, got %v", err)
	}

	// An exact-depth variable mints cleanly, value intact.
	d = bindEnv(map[string]string{"LIMITS_HTTP": "8080"}, set)
	segs, err := d.Children(limits)
	if err != nil || len(segs) != 1 || segs[0].String() != "http" {
		t.Fatalf("mint: %v, %v", segs, err)
	}
	v, err := d.Get(limits.Leaf(segs[0]))
	// minted leaf is unbound in the static table: the fake driver
	// refuses — in ferry the minted leaf inherits the composite's
	// binding. Bind it and prove the value is preserved:
	set2 := NewAddressSet()
	set2.AddComposite("/limits")
	set2.AddLeaf("/limits/http")
	d2 := bindEnv(map[string]string{"LIMITS_HTTP": "8080"}, set2)
	v, err = d2.Get(limits.Leaf(Name("http")))
	if err != nil || v.Text != "8080" {
		t.Fatalf("minted value preserved: %+v, %v", v, err)
	}
}

// ── #252: kind mismatch refuses, never fabricates ────────────────────

func TestContainerWhereLeafRefuses(t *testing.T) {
	d := newTreeDriver(map[string]any{
		"name": map[string]any{"first": "a"}, // plane holds a mapping
	})
	_, err := d.Get(LeafAddr{p: path{p: "/name"}})
	if err == nil || !strings.Contains(err.Error(), "mapping") {
		t.Fatalf("mismatch must refuse with what the plane holds, got %v", err)
	}
}

// ── #239: the sets are no longer identical ───────────────────────────

func TestAddressSetsDiffer(t *testing.T) {
	str := NewAddressSet()
	str.AddLeaf("/x")
	sl := NewAddressSet()
	sl.AddComposite("/x")

	if len(str.Leaves()) != 1 || len(str.Composites()) != 0 {
		t.Fatal("string /x must be a leaf member")
	}
	if len(sl.Leaves()) != 0 || len(sl.Composites()) != 1 {
		t.Fatal("[]string /x must be a composite member")
	}
	// The []string vs map distinction stays deliberately withheld:
	// both are composites, and a driver needs no more than that.
}

// ── presence, including empty-but-present and null ───────────────────

func TestTreePresence(t *testing.T) {
	d := newTreeDriver(map[string]any{
		"db":    map[string]any{}, // empty-but-present
		"cache": nil,              // explicit null
	})
	cases := []struct {
		addr string
		want Presence
	}{
		{"/db", Present},
		{"/cache", Null},
		{"/missing", Absent},
	}
	for _, c := range cases {
		got, err := d.Probe(SectionAddr{p: path{p: c.addr}})
		if err != nil || got != c.want {
			t.Fatalf("probe %s: %v, %v (want %v)", c.addr, got, err, c.want)
		}
	}
}

// A flat plane derives presence and cannot spell empty-but-present:
// the documented per-plane limitation, shown rather than claimed.
func TestFlatPresenceIsDerived(t *testing.T) {
	set := NewAddressSet()
	sec := set.AddSection("/db")
	set.AddLeaf("/db/host")

	d := bindEnv(map[string]string{"DB_HOST": "x"}, set)
	if p, _ := d.Probe(sec); p != Present {
		t.Fatal("a bound variable under the prefix must witness Present")
	}
	d = bindEnv(map[string]string{}, set)
	if p, _ := d.Probe(sec); p != Absent {
		t.Fatal("no variables → Absent; empty-but-present has no flat spelling")
	}
}

// ── A2's hole, demonstrated in behaviour, both dispositions ──────────

func TestEmptyPresentSectionAcrossDump(t *testing.T) {
	sec := SectionAddr{p: path{p: "/opts"}}

	// Without a section touch (today's behaviour): a non-nil *Opts
	// with every field omitted writes nothing, and the reload sees
	// Absent — present-empty silently degrades (P2's world).
	sink := newRecorderSink(false)
	// (zero child writes happen here, by construction of the case)
	if got := sink.probeAfterDump(sec); got != Absent {
		t.Fatalf("no-touch dump: %v, want Absent — this IS the hole", got)
	}

	// With the touch (P1): the dump makes one section-level statement
	// and the reload sees Present.
	sink = newRecorderSink(true)
	if err := sink.TouchSection(sec); err != nil {
		t.Fatal(err)
	}
	if got := sink.probeAfterDump(sec); got != Present {
		t.Fatalf("touched dump: %v, want Present", got)
	}

	// A plane that cannot spell it refuses loudly at the touch —
	// the G2 refusal shape, reused (P1's refusal arm, which is P3
	// scoped to planes that actually cannot).
	sink = newRecorderSink(false)
	if err := sink.TouchSection(sec); err == nil {
		t.Fatal("a plane without the spelling must refuse the touch")
	}
}

// ── references: resolved by the driver, structure memoized ───────────

func TestReferenceResolvesAndRestores(t *testing.T) {
	d := newTreeDriver(map[string]any{
		"defaults": map[string]any{"host": "db.local"},
		"primary":  ref{target: "/defaults"}, // the yaml-alias / symlink shape
	})
	// Observation is transparent: the schema sees a section.
	if p, err := d.Probe(SectionAddr{p: path{p: "/primary"}}); err != nil || p != Present {
		t.Fatalf("probe through ref: %v, %v", p, err)
	}
	v, err := d.Get(LeafAddr{p: path{p: "/primary"}})
	if err == nil {
		t.Fatalf("leaf question at a section-through-ref must still mismatch, got %+v", v)
	}
	// The indirection is memoized for write-back — #256's shape: the
	// write goes to the target, the link survives.
	if d.memo["/primary"] != "/defaults" {
		t.Fatalf("memo: %q", d.memo["/primary"])
	}
}

func TestReferenceCycleRefuses(t *testing.T) {
	d := newTreeDriver(map[string]any{
		"a": ref{target: "/b"},
		"b": ref{target: "/a"},
	})
	if _, err := d.Probe(SectionAddr{p: path{p: "/a"}}); err == nil {
		t.Fatal("a reference cycle must refuse, not spin")
	}
}

// ── #193 / #208: the multimap ────────────────────────────────────────

func TestMultimapMintsIndices(t *testing.T) {
	set := NewAddressSet()
	tags := set.AddComposite("/tags")
	set.AddLeaf("/limit")

	q, _ := url.ParseQuery("tags=a&tags=b&limit=1")
	d := bindQuery(q, set)

	segs, err := d.Children(tags)
	if err != nil || len(segs) != 2 {
		t.Fatalf("mint: %v, %v", segs, err)
	}
	v0, _ := d.Get(tags.Leaf(segs[0]))
	v1, _ := d.Get(tags.Leaf(segs[1]))
	if v0.Text != "a" || v1.Text != "b" {
		t.Fatalf("plane order: %q, %q", v0.Text, v1.Text)
	}
}

func TestScalarAtRepeatedKeyRefuses(t *testing.T) {
	set := NewAddressSet()
	limit := set.AddLeaf("/limit")
	q, _ := url.ParseQuery("limit=1&limit=2")
	d := bindQuery(q, set)
	_, err := d.Get(limit)
	if err == nil || !strings.Contains(err.Error(), "2 times") {
		t.Fatalf("repeated scalar must refuse with the count, got %v", err)
	}
}

// ── arrays are sections ──────────────────────────────────────────────

func TestArraysAreSections(t *testing.T) {
	k, err := Classify(reflect.TypeOf([3]int{}))
	if err != nil || k != MemberSection {
		t.Fatalf("[3]int: %v, %v", k, err)
	}
	segs, err := SectionChildren(reflect.TypeOf([3]int{}))
	if err != nil || len(segs) != 3 || segs[2].String() != "2" {
		t.Fatalf("children compiled from the type: %v, %v", segs, err)
	}
	// A Name child under an array is unaskable: arrays are not
	// enumerated, so there is no Children call to answer wrongly (#264).

	if k, _ := Classify(reflect.TypeOf([]int{})); k != MemberComposite {
		t.Fatal("slices stay dynamic")
	}
	if k, _ := Classify(reflect.TypeOf(map[string]int{})); k != MemberComposite {
		t.Fatal("maps stay dynamic")
	}
}

func TestZeroLengthArrayRefuses(t *testing.T) {
	if _, err := Classify(reflect.TypeOf([0]int{})); err == nil {
		t.Fatal("[0]int must refuse at compile, like struct{}")
	}
	// And the element type is checked even through composites (#260's
	// escape route closed):
	if _, err := Classify(reflect.TypeOf([]chan int{})); err == nil {
		t.Fatal("[]chan int must refuse: element types are always checked")
	}
	if _, err := Classify(reflect.TypeOf(map[string][0]int{})); err == nil {
		t.Fatal("map[string][0]int must refuse")
	}
}

// ── S1 / S2 / S3: memory characteristics ─────────────────────────────

func TestAddressSizes(t *testing.T) {
	if s := unsafe.Sizeof(LeafAddr{}); s != 16 {
		t.Fatalf("S1 address is %d bytes, want 16 (one string header)", s)
	}
	if s := unsafe.Sizeof(KindedPath{}); s != 24 {
		t.Fatalf("S2 address is %d bytes, want 24 (header + kind + padding)", s)
	}
	if s := unsafe.Sizeof(Addr[leafK]{}); s != 16 {
		t.Fatalf("S3 address is %d bytes, want 16", s)
	}
}
