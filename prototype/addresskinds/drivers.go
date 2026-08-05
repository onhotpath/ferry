package addresskinds

// Three fake drivers, each just deep enough to prove its board claim.

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// ── envDriver: the flat plane (#219, #235) ──────────────────────────

// envDriver classifies at Bind from the typed AddressSet: its key
// table knows which env keys are leaves, which prefixes are sections
// or composites. The ambient environment is consulted only through
// that table.
type envDriver struct {
	environ map[string]string // the process environment stand-in
	keys    map[string]string // rendered leaf address → env key
	prefix  map[string]string // rendered section/composite address → env prefix
}

func envKey(rendered string) string {
	k := strings.TrimPrefix(rendered, "/")
	k = strings.ReplaceAll(k, "/", "_")
	return strings.ToUpper(k)
}

func bindEnv(environ map[string]string, set *AddressSet) *envDriver {
	d := &envDriver{environ: environ, keys: map[string]string{}, prefix: map[string]string{}}
	for _, a := range set.Leaves() {
		d.keys[a.String()] = envKey(a.String())
	}
	for _, a := range set.Sections() {
		d.prefix[a.String()] = envKey(a.String()) + "_"
	}
	for _, a := range set.Composites() {
		d.prefix[a.String()] = envKey(a.String()) + "_"
	}
	return d
}

// Get takes a LeafAddr: the container question is unaskable, so an
// ambient HOME can never abort a load (#219 by construction).
func (d *envDriver) Get(addr LeafAddr) (Value, error) {
	key, ok := d.keys[addr.String()]
	if !ok {
		return Value{}, fmt.Errorf("driver asked for unbound leaf %s", addr)
	}
	if v, ok := d.environ[key]; ok {
		return Value{Kind: KindString, Text: v}, nil
	}
	return Value{}, nil // Absent
}

// Probe derives presence from the key table: any bound variable under
// the prefix → Present. Empty-but-present is unspellable on this
// plane, which is a documented limitation, not a bug.
func (d *envDriver) Probe(addr SectionAddr) (Presence, error) {
	prefix, ok := d.prefix[addr.String()]
	if !ok {
		return Absent, fmt.Errorf("driver asked for unbound section %s", addr)
	}
	for k := range d.environ {
		if strings.HasPrefix(k, prefix) {
			return Present, nil
		}
	}
	return Absent, nil
}

// Children mints only exact-depth keys for a leaf-elemented
// composite; a deeper key is an orphan named loudly — the #235
// phantom (mint the head, drop the value) is unwritable.
func (d *envDriver) Children(addr CompositeAddr) ([]Segment, error) {
	prefix, ok := d.prefix[addr.String()]
	if !ok {
		return nil, fmt.Errorf("driver asked for unbound composite %s", addr)
	}
	var segs []Segment
	for k := range d.environ {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := strings.TrimPrefix(k, prefix)
		if strings.Contains(rest, "_") {
			return nil, fmt.Errorf(
				"variable %s reaches deeper than the elements of %s; no phantom child is minted", k, addr)
		}
		segs = append(segs, Name(strings.ToLower(rest)))
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].String() < segs[j].String() })
	return segs, nil
}

// ── treeDriver: the tree plane (#252, presence, references) ─────────

// ref is a plane-internal indirection: a yaml anchor/alias, an
// fs-symlink. The driver resolves it transparently and memoizes the
// structure so an unchanged write restores the reference (#256's fix
// shape, and the A2 Reference discussion made concrete).
type ref struct{ target string }

type treeDriver struct {
	root map[string]any    // nested map[string]any | string | nil | ref
	memo map[string]string // rendered address → ref target (structure preserved)
}

func newTreeDriver(root map[string]any) *treeDriver {
	return &treeDriver{root: root, memo: map[string]string{}}
}

func (d *treeDriver) lookup(rendered string) (any, bool) {
	node := any(d.root)
	if rendered == "" || rendered == "/" {
		return node, true
	}
	for _, seg := range strings.Split(strings.TrimPrefix(rendered, "/"), "/") {
		m, ok := node.(map[string]any)
		if !ok {
			return nil, false
		}
		child, ok := m[seg]
		if !ok {
			return nil, false
		}
		node = child
	}
	return node, true
}

// resolve follows references, memoizing the indirection per address.
func (d *treeDriver) resolve(rendered string, node any) (any, error) {
	seen := map[string]bool{}
	for {
		r, ok := node.(ref)
		if !ok {
			return node, nil
		}
		if seen[r.target] {
			return nil, fmt.Errorf("reference cycle through %s", r.target)
		}
		seen[r.target] = true
		d.memo[rendered] = r.target
		target, ok := d.lookup(r.target)
		if !ok {
			return nil, fmt.Errorf("reference at %s names missing %s", rendered, r.target)
		}
		node = target
	}
}

// Get refuses a kind mismatch with the address and what the plane
// actually holds — #252's fabricated zero is unwritable.
func (d *treeDriver) Get(addr LeafAddr) (Value, error) {
	node, ok := d.lookup(addr.String())
	if !ok {
		return Value{}, nil // Absent
	}
	node, err := d.resolve(addr.String(), node)
	if err != nil {
		return Value{}, err
	}
	switch n := node.(type) {
	case nil:
		return Value{Kind: KindNull}, nil
	case string:
		return Value{Kind: KindString, Text: n}, nil
	case map[string]any:
		return Value{}, fmt.Errorf("%s holds a mapping, the schema wants a value there", addr)
	}
	return Value{}, fmt.Errorf("%s holds an unsupported node", addr)
}

func (d *treeDriver) Probe(addr SectionAddr) (Presence, error) {
	node, ok := d.lookup(addr.String())
	if !ok {
		return Absent, nil
	}
	node, err := d.resolve(addr.String(), node)
	if err != nil {
		return Absent, err
	}
	switch node.(type) {
	case nil:
		return Null, nil
	case map[string]any:
		return Present, nil // including empty: db: {}
	}
	return Absent, fmt.Errorf("%s holds a value, the schema wants a section there", addr)
}

// ── queryDriver: the multimap (#193, #208) ──────────────────────────

type queryDriver struct {
	values url.Values
	leaves map[string]string // rendered → query key (bound at Bind)
	comps  map[string]string
}

func bindQuery(values url.Values, set *AddressSet) *queryDriver {
	d := &queryDriver{values: values, leaves: map[string]string{}, comps: map[string]string{}}
	for _, a := range set.Leaves() {
		d.leaves[a.String()] = strings.TrimPrefix(a.String(), "/")
	}
	for _, a := range set.Composites() {
		d.comps[a.String()] = strings.TrimPrefix(a.String(), "/")
	}
	return d
}

// Children mints index children from repetition, in plane order:
// the second dimension is enumeration, which composites already do.
func (d *queryDriver) Children(addr CompositeAddr) ([]Segment, error) {
	key, ok := d.comps[addr.String()]
	if !ok {
		return nil, fmt.Errorf("driver asked for unbound composite %s", addr)
	}
	segs := make([]Segment, len(d.values[key]))
	for i := range d.values[key] {
		segs[i] = Index(i)
	}
	return segs, nil
}

// Get for a minted element leaf answers by position; for a declared
// scalar leaf, a repeated key refuses loudly with both positions —
// the refusal #208 said the driver could not express.
func (d *queryDriver) Get(addr LeafAddr) (Value, error) {
	if key, ok := d.leaves[addr.String()]; ok {
		vals := d.values[key]
		switch len(vals) {
		case 0:
			return Value{}, nil
		case 1:
			return Value{Kind: KindString, Text: vals[0]}, nil
		default:
			return Value{}, fmt.Errorf(
				"%s is a single value and the query names %q %d times", addr, key, len(vals))
		}
	}
	// minted element: /tags/0 → key "tags", index 0
	parts := strings.Split(strings.TrimPrefix(addr.String(), "/"), "/")
	if len(parts) == 2 {
		key, idx := parts[0], parts[1]
		var i int
		if _, err := fmt.Sscanf(idx, "%d", &i); err == nil {
			vals := d.values[key]
			if i < len(vals) {
				return Value{Kind: KindString, Text: vals[i]}, nil
			}
		}
	}
	return Value{}, fmt.Errorf("driver asked for unbound leaf %s", addr)
}
