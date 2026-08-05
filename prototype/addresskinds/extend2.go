package addresskinds

// Round three: the complete O2 AddressSet surface (iter.Seq + Has),
// the leaf redirect arm, and the write-divergence rule (W1) — the
// sharp edge the reference model exposes after cycles.

import (
	"errors"
	"fmt"
	"iter"
	"maps"
)

// ── O2, complete: one iterator, one membership test ─────────────────

// Seq enumerates every member the schema determines. The caller
// type-switches once, at Bind, on the cold path.
func (s *AddressSet) Seq() iter.Seq[Member] {
	return func(yield func(Member) bool) {
		for _, a := range s.leaves {
			if !yield(a) {
				return
			}
		}
		for _, a := range s.sections {
			if !yield(a) {
				return
			}
		}
		for _, a := range s.composites {
			if !yield(a) {
				return
			}
		}
	}
}

// Has answers membership for any kind through one method: the sealed
// sum makes the kind dispatch internal.
func (s *AddressSet) Has(m Member) bool {
	switch a := m.(type) {
	case LeafAddr:
		_, ok := s.leaves[a.String()]
		return ok
	case SectionAddr:
		_, ok := s.sections[a.String()]
		return ok
	case CompositeAddr:
		_, ok := s.composites[a.String()]
		return ok
	}
	return false
}

// bindEnvViaSeq is the full O2 bind: range-over-func, typed map keys.
func bindEnvViaSeq(environ map[string]string, set *AddressSet) *envDriver {
	d := &envDriver{environ: environ, keys: map[string]string{}, prefix: map[string]string{}}
	for m := range set.Seq() {
		switch a := m.(type) {
		case LeafAddr:
			d.keys[a.String()] = envKey(a.String())
		case SectionAddr:
			d.prefix[a.String()] = envKey(a.String()) + "_"
		case CompositeAddr:
			d.prefix[a.String()] = envKey(a.String()) + "_"
		}
	}
	return d
}

// ── the leaf redirect arm ───────────────────────────────────────────

// LeafRedirect is the control error a Reader returns when a leaf's
// content lives at another leaf the schema addresses. Value stays a
// closed six-kind lattice (02b): the redirect is control flow, not a
// kind, and the type preserves the invariant that a leaf redirect
// targets a leaf.
type LeafRedirect struct{ Target LeafAddr }

func (r *LeafRedirect) Error() string {
	return fmt.Sprintf("content lives at %s", r.Target)
}

// ResolveLeaf is core's leaf-side resolution loop — the same shape as
// ResolveSection, cycle discipline written once.
func ResolveLeaf(rd Reader, addr LeafAddr) (Value, error) {
	seen := map[string]bool{addr.String(): true}
	for {
		v, err := rd.Get(addr)
		var r *LeafRedirect
		if !errors.As(err, &r) {
			return v, err
		}
		if seen[r.Target.String()] {
			return Value{}, fmt.Errorf("reference cycle through %s", r.Target)
		}
		seen[r.Target.String()] = true
		addr = r.Target
	}
}

// ── W1: the write-divergence rule ───────────────────────────────────

// The edge, stated: /primary is a link to /defaults; the load
// resolved it; the caller mutated the section; the dump must decide.
// Writing through the link mutates /defaults — visible at every other
// alias. W1: an unchanged section keeps its link (the 02b memo rule,
// generalised); a diverged section MATERIALISES — the link breaks at
// the diverged address only, and the target is untouched.
func (d *treeDriver) WriteSection(addr SectionAddr, content map[string]string) error {
	rendered := addr.String()
	if target, linked := d.memo[rendered]; linked {
		current, ok := d.lookup(target)
		if ok && sectionEqual(current, content) {
			return nil // unchanged: the link survives, nothing to write
		}
		// diverged: materialise a copy here; the target is untouched.
		delete(d.memo, rendered)
	}
	return d.setSection(rendered, content)
}

func sectionEqual(node any, content map[string]string) bool {
	m, ok := node.(map[string]any)
	if !ok || len(m) != len(content) {
		return false
	}
	for k, v := range content {
		s, ok := m[k].(string)
		if !ok || s != v {
			return false
		}
	}
	return true
}

func (d *treeDriver) setSection(rendered string, content map[string]string) error {
	parent, last, err := d.parentOf(rendered)
	if err != nil {
		return err
	}
	next := map[string]any{}
	for k, v := range content {
		next[k] = v
	}
	parent[last] = next
	return nil
}

func (d *treeDriver) parentOf(rendered string) (map[string]any, string, error) {
	segs := splitPath(rendered)
	if len(segs) == 0 {
		return nil, "", fmt.Errorf("no parent for %s", rendered)
	}
	node := d.root
	for _, s := range segs[:len(segs)-1] {
		child, ok := node[s].(map[string]any)
		if !ok {
			return nil, "", fmt.Errorf("no parent for %s", rendered)
		}
		node = child
	}
	return node, segs[len(segs)-1], nil
}

func splitPath(rendered string) []string {
	var out []string
	cur := ""
	for _, r := range rendered {
		if r == '/' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// snapshot exposes the plane for assertions.
func (d *treeDriver) snapshot() map[string]any { return maps.Clone(d.root) }
