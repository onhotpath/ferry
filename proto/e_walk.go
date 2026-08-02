package main

// The walk, written exactly once.
//
// Research 5.2 is the strongest single argument in the survey: xload's serial
// and concurrent walks are ~90 duplicated lines that have already drifted and
// return different results for the same input, and its own equivalence test
// cannot catch it. The constraint handed to #16 is "write the walk exactly
// once", and this file is the attempt.
//
// What is written once here is the STRUCTURE: which nodes exist, which
// addresses they mint, in what order, how a pointer is materialised, how the
// presence bit combines, and where the scheduler seam is. What is genuinely
// per-direction is three operations, and no shape removes them - E6 measures
// exactly how big the residue is rather than claiming it is zero.

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
)

// member is one realised child of a DYNAMIC composite.
type member struct {
	seg Segment
	key reflect.Value // nMap only, already the key type
	idx int           // nSlice only
}

// direction is the whole per-direction surface. Three operations.
type direction struct {
	name string

	// leaf handles exactly one address, and is the only thing that touches a
	// boundary Value. It returns whether the address was present (Load) or
	// written (Dump), which is ADR-0006's presence bit.
	leaf func(n *node, v reflect.Value, at Path) (bool, error)

	// container is asked at a pointer, slice or map BEFORE descending: is
	// this container's own address carrying the whole answer? On Dump that is
	// "nil or empty, so write Null"; on Load it is "the plane holds Null
	// here, so zero it". Symmetric by construction, which is why it is one
	// hook and not two.
	container func(n *node, v reflect.Value, at Path) (handled bool, present bool, err error)

	// members supplies the realised children of a dynamic composite. This is
	// the one operation that is irreducibly per-direction: Dump reads them off
	// the value, Load enumerates the plane (ADR-0004's Enumerator asymmetry).
	members func(n *node, v reflect.Value, at Path) ([]member, error)
}

// sched is #20's seam, and nothing more. The serial scheduler is the
// identity; a bounded pool would be a second implementation of THIS and never
// a second copy of the walk.
type sched func(tasks []func() error) error

func serial(tasks []func() error) error {
	for _, t := range tasks {
		if err := t(); err != nil {
			return err
		}
	}
	return nil
}

type walker struct {
	dir direction
	sch sched
	ctx context.Context
}

// walk is the whole traversal. Every structural rule in ferry appears in this
// function exactly once.
func (w *walker) walk(n *node, v reflect.Value, at Path) (bool, error) {
	if err := w.ctx.Err(); err != nil {
		return false, err
	}
	switch n.kind {
	case nLeaf:
		return w.dir.leaf(n, v, at)

	case nStruct:
		present := false
		tasks := make([]func() error, 0, len(n.fields))
		for _, f := range n.fields {
			at := at
			if f.node.shape.String() != n.shape.String() {
				at = childAddr(at, f.node.shape, n.shape)
			}
			tasks = append(tasks, func() error {
				p, err := w.walk(f.node, v.Field(f.idx), at)
				present = present || p
				return err
			})
		}
		return present, w.sch(tasks)

	case nPtr:
		handled, present, err := w.dir.container(n, v, at)
		if handled || err != nil {
			return present, err
		}
		// A pointer is materialised exactly when something beneath it was
		// present, which is ADR-0006's repair for survey item 5.7. On Dump
		// the pointer is non-nil by the time we get here, so elem is its
		// pointee; on Load it is a fresh value we keep only if it was touched.
		if !v.IsNil() {
			return w.walk(n.elem, v.Elem(), at)
		}
		fresh := reflect.New(n.typ.Elem()).Elem()
		p, err := w.walk(n.elem, fresh, at)
		if !p && err != nil {
			// CANDIDATE FIX for the finding #14 reported: a `required` leaf
			// beneath an optional *T made the whole section mandatory.
			//
			// If nothing under the pointer was present, the section does not
			// exist, and a `required` failure beneath it is a CONSEQUENCE of
			// that rather than a failure of its own. ADR-0011's one rule -
			// "report every failure that is not a consequence of another" -
			// applied to the mirror of the case it already handles, which is a
			// required child under a REQUIRED parent.
			err = dropMissingUnder(err)
		}
		if err != nil {
			return p, err
		}
		if p && v.CanSet() {
			ptr := reflect.New(n.typ.Elem())
			ptr.Elem().Set(fresh)
			v.Set(ptr)
		}
		return p, nil

	case nArray:
		// Static: N element addresses, from the type.
		present := false
		tasks := make([]func() error, 0, n.n)
		for i := range n.n {
			tasks = append(tasks, func() error {
				p, err := w.walk(n.elem, v.Index(i), at.Index(i))
				present = present || p
				return err
			})
		}
		return present, w.sch(tasks)

	case nSlice, nMap:
		handled, present, err := w.dir.container(n, v, at)
		if handled || err != nil {
			return present, err
		}
		ms, err := w.dir.members(n, v, at)
		if err != nil {
			return false, err
		}
		if len(ms) == 0 {
			return false, nil
		}
		return w.dynamic(n, v, at, ms)
	}
	return false, fmt.Errorf("ferry: %s: unwalkable node", at)
}

// dynamic is the make-it-addressable / put-it-back mechanics, written once so
// that neither direction has to know that a map value is unaddressable.
func (w *walker) dynamic(n *node, v reflect.Value, at Path, ms []member) (bool, error) {
	present := false
	if n.kind == nSlice {
		max := 0
		for _, m := range ms {
			if m.idx+1 > max {
				max = m.idx + 1
			}
		}
		if v.CanSet() && v.Len() < max {
			v.Set(reflect.MakeSlice(n.typ, max, max))
		}
		tasks := make([]func() error, 0, len(ms))
		for _, m := range ms {
			tasks = append(tasks, func() error {
				p, err := w.walk(n.elem, v.Index(m.idx), at.Index(m.idx))
				present = present || p
				return err
			})
		}
		return present, w.sch(tasks)
	}
	out := v
	if v.CanSet() {
		out = reflect.MakeMapWithSize(n.typ, len(ms))
	}
	for _, m := range ms {
		elem := reflect.New(n.typ.Elem()).Elem()
		if !v.CanSet() || v.Kind() == reflect.Map && v.Len() > 0 {
			if got := v.MapIndex(m.key); got.IsValid() {
				elem.Set(got)
			}
		}
		p, err := w.walk(n.elem, elem, at.Name(m.seg.Text))
		if err != nil {
			return present, err
		}
		present = present || p
		if v.CanSet() {
			out.SetMapIndex(m.key, elem)
		}
	}
	if v.CanSet() {
		v.Set(out)
	}
	return present, nil
}

// childAddr re-mints a child's address under the realised parent address. The
// compiled shape is static ("/servers/*/port"); the realised one is not
// ("/servers/a/port"). ADR-0006 measured what happens if the two are confused:
// every default under a map or a slice silently stops applying.
func childAddr(realParent, childShape, parentShape Path) Path {
	cs, ps := childShape.Segments(), parentShape.Segments()
	out := realParent
	for _, s := range cs[len(ps):] {
		if s.Kind == Index {
			i, _ := strconv.Atoi(s.Text)
			out = out.Index(i)
		} else {
			out = out.Name(s.Text)
		}
	}
	return out
}

// --- the two directions ------------------------------------------------------

func dumpDir(out map[Path]Value) direction {
	return direction{
		name: "dump",
		leaf: func(n *node, v reflect.Value, at Path) (bool, error) {
			// ADR-0006/ADR-0007: omission is evaluated against the Go value
			// before anything converts it.
			if n.omitzero && v.IsZero() {
				return false, nil
			}
			val, err := encLeafWith(n.codec, v)
			if err != nil {
				return false, fmt.Errorf("ferry: %s: %w", at, err)
			}
			out[at] = val
			return true, nil
		},
		container: func(n *node, v reflect.Value, at Path) (bool, bool, error) {
			if n.kind == nPtr {
				if v.IsNil() {
					out[at] = Null()
					return true, true, nil
				}
				return false, false, nil
			}
			if v.Len() == 0 {
				out[at] = Null()
				return true, true, nil
			}
			return false, false, nil
		},
		members: func(n *node, v reflect.Value, at Path) ([]member, error) {
			if n.kind == nSlice {
				ms := make([]member, v.Len())
				for i := range v.Len() {
					ms[i] = member{seg: Segment{Kind: Index, Text: strconv.Itoa(i)}, idx: i}
				}
				return ms, nil
			}
			keys := v.MapKeys()
			slices.SortFunc(keys, func(a, b reflect.Value) int {
				return cmpStr(mapKeyText(a), mapKeyText(b))
			})
			ms := make([]member, 0, len(keys))
			for _, k := range keys {
				ms = append(ms, member{seg: Segment{Kind: Name, Text: mapKeyText(k)}, key: k})
			}
			return ms, nil
		},
	}
}

// loadDir reads through ADR-0004's Reader rather than a prebuilt map,
// because that is what makes the dynamic tier honest: a map key's address is
// not in the static set handed to Bind, so it can only come from an
// Enumerator, and a source that does not implement one must fail loudly
// rather than yield an empty map.
func loadDir(r FReader, ctx context.Context, o opts) direction {
	// DEFECT FOUND BY #15, fixed here.
	//
	// The inherited version discarded the reader's error and substituted
	// Absent. A driver reporting "this address holds a type I cannot express"
	// - which is exactly what a Registry driver does for REG_MULTI_SZ - was
	// therefore indistinguishable from a missing key, so the address silently
	// took its default or its zero value.
	//
	// That is survey item 5.11's shape (a provider discarding a parse error),
	// which ADR-0001 rules out BY ARCHITECTURE and names as the failure it
	// rules out by name, occurring inside ferry's own walk rather than inside
	// a driver. No fixture caught it because every prototype source returns a
	// nil error.
	get := func(at Path) (Value, error) {
		v, err := r.Get(ctx, at)
		if err != nil {
			v = Value{}
		}
		if o.observe != nil {
			o.observe(at, v)
		}
		return v, err
	}
	return direction{
		name: "load",
		leaf: func(n *node, v reflect.Value, at Path) (bool, error) {
			val, err := get(at)
			if err != nil {
				return false, tErrAt(at, tErrPlane, "%v", err)
			}
			if val.Kind() == VAbsent {
				if n.required {
					// ADR-0011's shape rather than a bare fmt.Errorf, because
					// #14 reads the required set out of the error set and
					// message text is not API. See t_errors.go.
					return false, tErrAt(at, tErrMissing, "required, and the plane supplied nothing")
				}
				if n.def != nil {
					// The default is a Value at an address, indistinguishable
					// at the boundary from what a flat plane would report.
					return false, decLeafWith(n.codec, *n.def, v)
				}
				return false, nil // Absent does not write
			}
			if err := decLeafWith(n.codec, val, v); err != nil {
				return true, fmt.Errorf("ferry: %s: %w", at, err)
			}
			return true, nil
		},
		container: func(n *node, v reflect.Value, at Path) (bool, bool, error) {
			val, err := get(at)
			if err != nil {
				return true, false, tErrAt(at, tErrPlane, "%v", err)
			}
			if val.Kind() == VNull {
				if v.CanSet() {
					v.Set(reflect.Zero(n.typ))
				}
				return true, true, nil
			}
			return false, false, nil
		},
		members: func(n *node, v reflect.Value, at Path) ([]member, error) {
			en, ok := r.(FEnumerator)
			if !ok {
				return nil, fmt.Errorf("ferry: %s: %s needs addresses the source cannot enumerate: "+
					"this source does not implement Enumerator", at, n.typ)
			}
			kids, err := en.Children(ctx, at)
			if err != nil {
				return nil, err
			}
			ms := make([]member, 0, len(kids))
			for _, k := range kids {
				segs := k.Segments()
				last := segs[len(segs)-1]
				if n.kind == nSlice {
					i, err := strconv.Atoi(last.Text)
					if err != nil {
						return nil, fmt.Errorf("ferry: %s: bad index %q", at, last.Text)
					}
					ms = append(ms, member{seg: last, idx: i})
					continue
				}
				key := reflect.New(n.typ.Key()).Elem()
				if err := decMapKey(last.Text, key); err != nil {
					return nil, err
				}
				ms = append(ms, member{seg: last, key: key})
			}
			return ms, nil
		},
	}
}

// mapReader is the memory plane: a Reader over a map[Path]Value, which is
// what ADR-0002 admits to core as apparatus rather than as a driver.
type mapReader struct{ m map[Path]Value }

func (m mapReader) Get(_ context.Context, p Path) (Value, error)       { return m.m[p], nil }
func (m mapReader) Children(_ context.Context, p Path) ([]Path, error) { return children(m.m, p), nil }

func cmpStr(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
