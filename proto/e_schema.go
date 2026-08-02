package main

// The compiled schema: what #16 owns.
//
// Every ADR since ADR-0003 leans on the address set being computable from
// reflect.TypeFor[T]() alone. This file is that computation, plus the one
// thing the research calls "the single most transferable idea": a leaf holds
// RESOLVED BEHAVIOUR - a codec function pointer, a fully-resolved address
// shape, a compiled default - rather than data to be re-derived per call.
//
// The structural rules live here and ONLY here. ADR-0008 found the defect
// that motivates that: with the field rule in the compiler alone, the schema
// promised /name and the walk never visited it. The inherited prototype has
// the same shape in a milder form - compile(), dump() and load() each carry
// their own copy of "which fields, which addresses, in what order" - and E6
// reproduces a divergence in it.

import (
	"cmp"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// --- the compiled form ------------------------------------------------------

type nodeKind uint8

const (
	nLeaf nodeKind = iota
	nPtr
	nStruct
	nSlice
	nArray
	nMap
)

// node is one position in the type. Its address SHAPE is static: a dynamic
// segment is spelled "*", which is ADR-0006's "a declaration attaches to the
// address shape, not to an address".
type node struct {
	kind  nodeKind
	typ   reflect.Type
	shape Path

	// leaf only, all resolved at compile
	codec    leafCodec
	def      *Value // the declared default, already a Value
	required bool
	omitzero bool

	fields []sfield // nStruct
	elem   *node    // nPtr, nSlice, nArray, nMap
	n      int      // nArray
}

type sfield struct {
	idx  int
	node *node
}

type schema struct {
	root *node
	// addrs is the STATIC address set ADR-0004 hands to Bind: static leaves,
	// plus the own-address of every container that can carry a Null. A
	// DYNAMIC address shape ("/tags/*") is deliberately not in it - those are
	// minted as they are realised, which is ADR-0003's second tier.
	addrs     []Path
	leafAddrs []Path
	leaves    int
}

// --- compile ----------------------------------------------------------------

type compileCtx struct {
	o          opts
	errs       []error
	addrs      []Path // static leaves only
	containers []Path // static container own-addresses
	minted     int    // every address shape minted, static or not
	stack      map[reflect.Type]bool
}

func compileSchema2(t reflect.Type, o opts) (*schema, error) {
	c := &compileCtx{o: o, stack: map[reflect.Type]bool{}}
	root := c.rec(t, Path{})

	// The root leaf question ADR-0007 and ADR-0009 both handed here by name.
	// A chain-admitted or registered type at the root mints the empty path,
	// which ADR-0003 says an address may not be. E7 is the measurement.
	if root != nil && root.kind == nLeaf {
		c.errs = append(c.errs, fmt.Errorf(
			"ferry: the root type %s is a leaf, so it addresses the empty path and ADR-0003 says an address is non-empty; "+
				"wrap it in a struct with a named field", t))
	}
	if len(c.errs) > 0 {
		slices.SortFunc(c.errs, func(a, b error) int { return cmp.Compare(a.Error(), b.Error()) })
		return nil, errors.Join(c.errs...)
	}
	// ADR-0003's prefix-free rule, over the static tier.
	if err := prefixFree(c.addrs); err != nil {
		return nil, err
	}
	return &schema{
		root:      root,
		addrs:     sortedPaths(append(append([]Path{}, c.addrs...), c.containers...)),
		leafAddrs: sortedPaths(c.addrs),
		leaves:    countLeaves(root),
	}, nil
}

func (c *compileCtx) rec(t reflect.Type, p Path) *node {
	// Identity, then the text pair, then kind - ADR-0007's three steps, asked
	// ONCE per position and stored, rather than per field per call.
	if lc, ok := resolveLeaf(t); ok {
		c.minted++
		if !hasStar(p) {
			c.addrs = append(c.addrs, p)
		}
		return &node{kind: nLeaf, typ: t, shape: p, codec: lc}
	}
	if t.Kind() == reflect.Pointer {
		c.container(p)
		e := c.rec(t.Elem(), p)
		if e == nil {
			return nil
		}
		return &node{kind: nPtr, typ: t, shape: p, elem: e}
	}
	if c.stack[t] {
		c.errs = append(c.errs, fmt.Errorf(
			"ferry: %s: %s is recursive, so its address set is unbounded; register a codec for it", pathOrRoot(p), t))
		return nil
	}
	c.stack[t] = true
	defer delete(c.stack, t)

	switch t.Kind() {
	case reflect.Struct:
		n := &node{kind: nStruct, typ: t, shape: p}
		before := len(c.addrs)
		fieldErr := false
		for i := range t.NumField() {
			f := t.Field(i)
			tg, err := parseTag(f, c.o.tagKey)
			if err != nil {
				c.errs = append(c.errs, fmt.Errorf("ferry: %s: %w", pathOrRoot(p.Name(f.Name)), err))
				fieldErr = true
				continue
			}
			if tg.skip {
				continue
			}
			at := p
			if !tg.promote {
				at = p.Name(tg.name)
			}
			child := c.rec(f.Type, at)
			if child == nil {
				fieldErr = true
				continue
			}
			c.applyOptions(child, tg, at)
			n.fields = append(n.fields, sfield{idx: i, node: child})
		}
		// ADR-0005's maps-no-address backstop, suppressed at any level that
		// already reported a field error (ADR-0008's tier rule).
		if len(c.addrs) == before && !fieldErr {
			c.errs = append(c.errs, fmt.Errorf(
				"ferry: %s: %s maps no address: it has no exported field ferry can address; register a codec for it",
				pathOrRoot(p), t))
			return nil
		}
		return n
	case reflect.Array:
		n := &node{kind: nArray, typ: t, shape: p, n: t.Len()}
		// An array's element addresses are STATIC: N of them, from the type.
		for i := range t.Len() {
			e := c.rec(t.Elem(), p.Index(i))
			if e == nil {
				return nil
			}
			if i == 0 {
				n.elem = e
			}
		}
		return n
	case reflect.Slice:
		c.container(p)
		e := c.rec(t.Elem(), p.Name("*"))
		if e == nil {
			return nil
		}
		return &node{kind: nSlice, typ: t, shape: p, elem: e}
	case reflect.Map:
		if !validMapKey(t.Key()) {
			c.errs = append(c.errs, fmt.Errorf("ferry: %s: unsupported map key type %s", pathOrRoot(p), t.Key()))
			return nil
		}
		c.container(p)
		e := c.rec(t.Elem(), p.Name("*"))
		if e == nil {
			return nil
		}
		return &node{kind: nMap, typ: t, shape: p, elem: e}
	}
	c.errs = append(c.errs, unsupportedTypeError{p, t})
	return nil
}

// applyOptions is ADR-0006's five refusals and ADR-0008's tier rule, run
// against the node that was actually compiled rather than against the field's
// declared type. That matters: a field whose type the chain claimed is a LEAF,
// so `required` is admissible on it even though its Go kind is a struct.
func (c *compileCtx) applyOptions(n *node, tg tag, at Path) {
	dyn := n.kind == nSlice || n.kind == nMap ||
		(n.kind == nPtr && (n.elem.kind == nSlice || n.elem.kind == nMap))
	var bad []error
	if tg.required {
		if dyn {
			bad = append(bad, fmt.Errorf("ferry: %s: required is not available on %s: a plane cannot report "+
				"\"present and empty\" at a container address", at, n.typ))
		} else {
			n.required = true
		}
	}
	if tg.hasDefault {
		switch {
		case n.kind != nLeaf && !(n.kind == nPtr && n.elem.kind == nLeaf):
			bad = append(bad, fmt.Errorf("ferry: %s: %s is a composite, so it has no single address a default "+
				"could sit at; seed the value instead", at, n.typ))
		default:
			leaf := n
			if leaf.kind == nPtr {
				leaf = leaf.elem
			}
			v := String(tg.def)
			// The declaration is checked from reflect.TypeFor[T]() alone,
			// with no value in hand - ADR-0006's assertability property.
			probe := reflect.New(leaf.typ).Elem()
			if err := decLeafWith(leaf.codec, v, probe); err != nil {
				bad = append(bad, fmt.Errorf("ferry: %s: default %q is not a valid %s: %v", at, tg.def, leaf.typ, err))
			} else {
				n.def = &v
			}
		}
	}
	if tg.omitzero {
		n.omitzero = true
	}
	// Contradictions only among the options that survived admissibility.
	if len(bad) == 0 {
		if tg.required && tg.hasDefault {
			bad = append(bad, fmt.Errorf("ferry: %s: required and default contradict", at))
		}
		if tg.omitzero && n.def != nil && !isZeroDefault(n) {
			bad = append(bad, fmt.Errorf("ferry: %s: omitzero and default=%s contradict: an explicit zero "+
				"would be omitted and would load back as %s", at, tg.def, tg.def))
		}
	}
	c.errs = append(c.errs, bad...)
}

func isZeroDefault(n *node) bool {
	leaf := n
	if leaf.kind == nPtr {
		leaf = leaf.elem
	}
	probe := reflect.New(leaf.typ).Elem()
	if err := decLeafWith(leaf.codec, *n.def, probe); err != nil {
		return false
	}
	return probe.IsZero()
}

// container records a container's own address, which is where ADR-0005's
// "a composite with no elements writes Null at its own address" is observed.
// A container UNDER a dynamic shape has no static address at all.
func (c *compileCtx) container(p Path) {
	if p.IsRoot() || hasStar(p) {
		return
	}
	c.containers = append(c.containers, p)
}

func hasStar(p Path) bool {
	for _, s := range p.Segments() {
		if s.Kind == Name && s.Text == "*" {
			return true
		}
	}
	return false
}

func countLeaves(n *node) int {
	if n == nil {
		return 0
	}
	switch n.kind {
	case nLeaf:
		return 1
	case nStruct:
		t := 0
		for _, f := range n.fields {
			t += countLeaves(f.node)
		}
		return t
	default:
		return countLeaves(n.elem)
	}
}

func prefixFree(addrs []Path) error {
	seen := map[string]bool{}
	var clash []error
	for _, a := range addrs {
		s := a.String()
		if seen[s] {
			clash = append(clash, fmt.Errorf("ferry: two fields address %s", s))
		}
		seen[s] = true
	}
	slices.SortFunc(clash, func(a, b error) int { return cmp.Compare(a.Error(), b.Error()) })
	if len(clash) > 0 {
		return errors.Join(clash...)
	}
	return nil
}

// resolveLeaf is ADR-0007's steps 1 and 2, asked once. Pointer indirection is
// structural and is resolved by the caller before this is reached, which is
// ADR-0009's third refusal in the compiler rather than at a registration.
func resolveLeaf(t reflect.Type) (leafCodec, bool) {
	if t.Kind() == reflect.Pointer {
		return leafCodec{}, false
	}
	if c, ok := identityLookup(t); ok {
		return c, true
	}
	if c, ok := activeChainCodec(t); ok {
		return leafCodec{name: c.name, kind: c.kind, enc: c.enc, dec: c.dec}, true
	}
	if kindLeaf(t) {
		return leafCodec{name: "kind:" + t.Kind().String(), kind: kindOf(t), enc: encLeaf, dec: decLeaf}, true
	}
	return leafCodec{}, false
}

func kindLeaf(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	case reflect.Slice, reflect.Array:
		return t.Elem().Kind() == reflect.Uint8
	}
	return false
}

func kindOf(t reflect.Type) VKind {
	switch t.Kind() {
	case reflect.Bool:
		return VBool
	case reflect.String:
		return VString
	case reflect.Slice, reflect.Array:
		return VBytes
	}
	return VNumber
}

func decLeafWith(c leafCodec, v Value, dst reflect.Value) error {
	return c.dec(asDonor(v, c.kind), dst)
}

func encLeafWith(c leafCodec, v reflect.Value) (Value, error) { return c.enc(v) }

// --- the tag, ADR-0008's grammar in the small -------------------------------

type tag struct {
	name       string
	skip       bool
	promote    bool
	required   bool
	omitzero   bool
	hasDefault bool
	def        string
}

func parseTag(f reflect.StructField, key string) (tag, error) {
	raw, ok := f.Tag.Lookup(key)
	if !f.IsExported() {
		if f.Anonymous {
			// an embedded field of unexported TYPE is still promoted
			if !ok {
				return tag{promote: true}, nil
			}
		}
		return tag{skip: true}, nil
	}
	if !ok {
		if f.Anonymous {
			return tag{promote: true}, nil
		}
		return tag{}, fmt.Errorf("field %s carries no %s tag: every exported field must name the segment "+
			"it addresses, or be marked %s:\"-\"", f.Name, key, key)
	}
	if raw == "-" {
		return tag{skip: true}, nil
	}
	parts := splitTag(raw)
	t := tag{name: unquoteTok(parts[0])}
	if t.name == "" {
		return t, fmt.Errorf("the %s tag names no segment", key)
	}
	for _, o := range parts[1:] {
		switch {
		case o == "required":
			t.required = true
		case o == "omitzero":
			t.omitzero = true
		case strings.HasPrefix(o, "default="):
			t.hasDefault = true
			t.def = unquoteTok(strings.TrimPrefix(o, "default="))
		default:
			return t, fmt.Errorf("unknown option %q", o)
		}
	}
	return t, nil
}

// splitTag honours ADR-0008's single-quoted token: a comma inside quotes does
// not split.
func splitTag(s string) []string {
	var out []string
	var cur strings.Builder
	inq := false
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\'' && (cur.Len() == 0 || inq):
			if inq && i+1 < len(s) && s[i+1] == '\'' {
				cur.WriteByte('\'')
				cur.WriteByte('\'')
				i++
				continue
			}
			inq = !inq
			cur.WriteByte('\'')
		case s[i] == ',' && !inq:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(s[i])
		}
	}
	out = append(out, cur.String())
	return out
}

func unquoteTok(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	return s
}
