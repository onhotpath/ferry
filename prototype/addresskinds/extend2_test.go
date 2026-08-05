package addresskinds

import (
	"strings"
	"testing"
)

// ── O2 complete: Seq + Has ───────────────────────────────────────────

func TestSeqBindsEquivalently(t *testing.T) {
	set := NewAddressSet()
	set.AddLeaf("/db/host")
	set.AddSection("/db")
	set.AddComposite("/tags")
	environ := map[string]string{"DB_HOST": "x"}

	a := bindEnv(environ, set)
	b := bindEnvViaSeq(environ, set)
	if len(a.keys) != len(b.keys) || len(a.prefix) != len(b.prefix) {
		t.Fatal("Seq bind must build the identical table")
	}
}

func TestHasIsOneMethodForThreeKinds(t *testing.T) {
	set := NewAddressSet()
	leaf := set.AddLeaf("/db/host")
	sec := set.AddSection("/db")
	if !set.Has(leaf) || !set.Has(sec) {
		t.Fatal("members must be found")
	}
	if set.Has(CompositeAddr{p: path{p: "/db"}}) {
		t.Fatal("the same rendered path under another kind is not a member — kinds partition")
	}
}

// ── the leaf redirect arm ────────────────────────────────────────────

type leafRefReader struct {
	values map[string]string
	links  map[string]LeafAddr
}

func (r leafRefReader) Get(addr LeafAddr) (Value, error) {
	if t, ok := r.links[addr.String()]; ok {
		return Value{}, &LeafRedirect{Target: t}
	}
	if v, ok := r.values[addr.String()]; ok {
		return Value{Kind: KindString, Text: v}, nil
	}
	return Value{}, nil
}

func TestLeafRedirectResolves(t *testing.T) {
	rd := leafRefReader{
		values: map[string]string{"/defaults/host": "db.local"},
		links:  map[string]LeafAddr{"/primary/host": {p: path{p: "/defaults/host"}}},
	}
	v, err := ResolveLeaf(rd, LeafAddr{p: path{p: "/primary/host"}})
	if err != nil || v.Text != "db.local" {
		t.Fatalf("leaf redirect: %+v, %v", v, err)
	}
}

func TestLeafRedirectCycleRefuses(t *testing.T) {
	rd := leafRefReader{links: map[string]LeafAddr{
		"/a": {p: path{p: "/b"}},
		"/b": {p: path{p: "/a"}},
	}}
	_, err := ResolveLeaf(rd, LeafAddr{p: path{p: "/a"}})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("leaf cycle must refuse in core: %v", err)
	}
}

// ── W1: write divergence, the discovered edge ────────────────────────

func linkedFixture() *treeDriver {
	d := newTreeDriver(map[string]any{
		"defaults":  map[string]any{"host": "db.local"},
		"primary":   ref{target: "/defaults"},
		"secondary": ref{target: "/defaults"}, // a second alias of the same target
	})
	// Loads resolved both links; the memo recorded them.
	_, _ = d.Probe(SectionAddr{p: path{p: "/primary"}})
	_, _ = d.Probe(SectionAddr{p: path{p: "/secondary"}})
	return d
}

func TestUnchangedSectionKeepsItsLink(t *testing.T) {
	d := linkedFixture()
	err := d.WriteSection(SectionAddr{p: path{p: "/primary"}}, map[string]string{"host": "db.local"})
	if err != nil {
		t.Fatal(err)
	}
	if _, isRef := d.snapshot()["primary"].(ref); !isRef {
		t.Fatal("an unchanged section must keep its link")
	}
}

func TestDivergedSectionMaterialises(t *testing.T) {
	d := linkedFixture()
	err := d.WriteSection(SectionAddr{p: path{p: "/primary"}}, map[string]string{"host": "other.host"})
	if err != nil {
		t.Fatal(err)
	}
	snap := d.snapshot()
	// The diverged address materialised a copy...
	m, ok := snap["primary"].(map[string]any)
	if !ok || m["host"] != "other.host" {
		t.Fatalf("diverged section must materialise: %#v", snap["primary"])
	}
	// ...the shared target is untouched...
	if snap["defaults"].(map[string]any)["host"] != "db.local" {
		t.Fatal("the target must not see the divergence")
	}
	// ...and the OTHER alias still points at the untouched target:
	if _, isRef := snap["secondary"].(ref); !isRef {
		t.Fatal("the second alias must keep its link")
	}
	p, err := d.Probe(SectionAddr{p: path{p: "/secondary"}})
	if err != nil || p != Present {
		t.Fatalf("second alias still resolves: %v, %v", p, err)
	}
}
