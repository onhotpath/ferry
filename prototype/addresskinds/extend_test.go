package addresskinds

import (
	"strings"
	"testing"
)

// ── the []string vs map question, answered by a check ────────────────

func TestElementFormDifferentiates(t *testing.T) {
	tags := CompositeAddr{p: path{p: "/tags"}} // []string in the schema

	// A driver minting names under a slice is refused, segment named.
	err := ValidateSegments(IndexElements, tags, []Segment{Name("foo")})
	if err == nil || !strings.Contains(err.Error(), `"foo"`) {
		t.Fatalf("name under a slice must refuse: %v", err)
	}
	// Digit-shaped names coerce: env's TAGS_0 arrives as Name("0").
	if err := ValidateSegments(IndexElements, tags, []Segment{Name("0"), Index(1)}); err != nil {
		t.Fatalf("indices must pass: %v", err)
	}

	labels := CompositeAddr{p: path{p: "/labels"}} // map[string]string
	if err := ValidateSegments(NameElements, labels, []Segment{Name("app")}); err != nil {
		t.Fatalf("names under a map must pass: %v", err)
	}
	if err := ValidateSegments(NameElements, labels, []Segment{Index(0)}); err == nil {
		t.Fatal("an index under a map must refuse")
	}
}

// ── posture B: core resolves, drivers report ─────────────────────────

func postureBFixture() (*treeDriverB, *AddressSet) {
	set := NewAddressSet()
	set.AddSection("/defaults")
	set.AddSection("/primary")
	set.AddSection("/secondary")
	set.AddSection("/a")
	set.AddSection("/b")
	d := newTreeDriver(map[string]any{
		"defaults":  map[string]any{"host": "db.local"},
		"primary":   ref{target: "/defaults"},
		"secondary": ref{target: "/primary"}, // two hops
		"a":         ref{target: "/b"},
		"b":         ref{target: "/a"}, // cycle
		"orphan":    ref{target: "/unmapped"},
	})
	return &treeDriverB{inner: d, set: set}, set
}

func TestCoreResolvesReferenceChain(t *testing.T) {
	d, _ := postureBFixture()
	p, err := ResolveSection(d, SectionAddr{p: path{p: "/secondary"}})
	if err != nil || p != Present {
		t.Fatalf("two-hop chain: %v, %v", p, err)
	}
}

func TestCoreRefusesReferenceCycle(t *testing.T) {
	d, _ := postureBFixture()
	_, err := ResolveSection(d, SectionAddr{p: path{p: "/a"}})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle must refuse in core, once: %v", err)
	}
}

func TestPostureBBoundary(t *testing.T) {
	// The honest limit: a reference to a plane location the schema
	// does not address has no SectionAddr to report. Posture B cannot
	// carry it; the driver resolves internally (posture A) or refuses.
	d, set := postureBFixture()
	set.AddSection("/orphan")
	d2 := &treeDriverB{inner: d.inner, set: set}
	_, err := d2.ProbeB(SectionAddr{p: path{p: "/orphan"}})
	if err == nil || !strings.Contains(err.Error(), "does not address") {
		t.Fatalf("unmapped target must surface the boundary: %v", err)
	}
}

// ── O2: one iterator carries what three methods carry ────────────────

func TestAllIteratorBindsEquivalently(t *testing.T) {
	set := NewAddressSet()
	set.AddLeaf("/db/host")
	set.AddSection("/db")
	set.AddComposite("/tags")
	environ := map[string]string{"DB_HOST": "x"}

	a := bindEnv(environ, set)
	b := bindEnvViaAll(environ, set)

	if len(a.keys) != len(b.keys) || len(a.prefix) != len(b.prefix) {
		t.Fatalf("O1 and O2 must build identical tables: %v/%v vs %v/%v",
			a.keys, a.prefix, b.keys, b.prefix)
	}
	for k, v := range a.keys {
		if b.keys[k] != v {
			t.Fatalf("key table differs at %s", k)
		}
	}
}
