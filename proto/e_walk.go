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
	"reflect"
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

	// bound is asked at an ARRAY before descending: does the plane hold an
	// index this array cannot? #41 D13. It is nil on Dump, where the value's
	// own length is the only length there is and the question cannot arise.
	//
	// ADR-0005 publishes `ferry: /V: plane has index 7, [3]string holds 3`.
	// That check lives at walk.go:333 on the superseded walk; this walk never
	// grew one, so `[3]string` given only index 7 loaded ["" "" ""] with a nil
	// error - a plane address silently discarded, which ADR-0001 rules out.
	bound func(n *node, at Path) error

	// enforceRequired says whether `required` is a question this direction can
	// ask. It is Load's alone: `required` is an assertion about what the PLANE
	// supplied, and on Dump the plane is the thing being written.
	enforceRequired bool
}

// sched is #20's seam, and nothing more. The default is `aggregating` (see
// ferr_sched.go); a bounded pool would be a second implementation of THIS and
// never a second copy of the walk.
type sched func(tasks []func() error) error

// serial is first-error-wins. It is NOT the default any more - ADR-0011
// declines to ship StopOnFirstError and the tip was shipping it by defaulting -
// but it stays reachable through WithSched, because ADR-0010's claim is that
// the same walk function under either scheduler is byte-identical in between,
// and that claim needs both ends of the seam to remain measurable.
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
		err := w.sch(tasks)
		return present, w.requiredHere(n, at, present, err)

	case nPtr:
		handled, present, err := w.dir.container(n, v, at)
		if handled || err != nil {
			// A Null at the address SATISFIES `required`: ADR-0006's own table
			// has `auth: null` in the satisfied column, because the plane did
			// speak at the address. So `handled` returns untouched.
			return present, err
		}
		// A pointer is materialised exactly when something beneath it was
		// present, which is ADR-0006's repair for survey item 5.7. On Dump
		// the pointer is non-nil by the time we get here, so elem is its
		// pointee; on Load it is a fresh value we keep only if it was touched.
		if !v.IsNil() {
			p, err := w.walk(n.elem, v.Elem(), at)
			return p, w.requiredHere(n, at, p, err)
		}
		fresh := reflect.New(n.typ.Elem()).Elem()
		p, err := w.walk(n.elem, fresh, at)
		if !p && err != nil {
			// The finding #14 reported: a `required` leaf beneath an optional
			// *T made the whole section mandatory.
			//
			// If nothing under the pointer was present, the section does not
			// exist, and a `required` failure beneath it is a CONSEQUENCE of
			// that rather than a failure of its own. ADR-0011's one rule -
			// "report every failure that is not a consequence of another" -
			// applied to the mirror of the case it already handles, which is a
			// required child under a REQUIRED parent.
			//
			// This is only sound under an aggregating scheduler, because the
			// presence bit it is gated on is accumulated ACROSS siblings and
			// `serial` abandons the later ones. Aggregation is now the default,
			// so the gate is sound by default rather than by the caller having
			// asked. See ferr_sched.go.
			err = dropMissingUnder(err)
		}
		// The composite's own `required` is asked AFTER the drop, which is what
		// turns the required-parent case into ADR-0011's one suppression bit:
		// the children's ErrMissing elements collapse into the parent's single
		// summary sentence instead of standing beside it.
		if err = w.requiredHere(n, at, p, err); err != nil {
			return p, err
		}
		if p && v.CanSet() {
			ptr := reflect.New(n.typ.Elem())
			ptr.Elem().Set(fresh)
			v.Set(ptr)
		}
		return p, nil

	case nArray:
		// #41 D13, ported from walk.go:333. An array's length is the TYPE's,
		// not the plane's, so an index the array cannot hold is loud, while a
		// missing one leaves its element at the zero value (ADR-0006's rule).
		// Asked before descending, exactly as the superseded walk asked it.
		if w.dir.bound != nil {
			if err := w.dir.bound(n, at); err != nil {
				return false, err
			}
		}
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
	return false, errAt(mWalk, ErrSchema, at, "unwalkable node")
}

// requiredHere is #41 D12 and ADR-0011's ONE suppression bit, in one place.
//
// ADR-0006: "At a composite it means the plane supplied at least one of the
// address's static children", measured for both `struct` and `*struct` as
// `ferry: /auth: required, and the plane supplied nothing under it`. The ADR
// records repairing exactly this in its own draft; the repair never reached
// this branch line, so `applyOptions` set node.required on struct and pointer
// nodes and the walk read n.required in exactly one place, direction.leaf.
//
// Three gates, and each is a sentence from an ADR:
//
//   - enforceRequired: `required` is a Load-side assertion about the plane.
//   - present: ADR-0006's presence bit IS "the plane supplied at least one
//     static child", already computed by the walk, so this costs nothing.
//   - err == nil: ADR-0011's suppression bit. "A composite's `required` failure
//     is suppressed when a child under it already reported", because the
//     parent's check is the child's summary and two errors with one remediation
//     is ADR-0008's tier rule broken at the walk.
//
// The neighbouring case needs nothing, which is why this is one bit and not a
// redesign: a child that is PRESENT and fails to decode has already set the
// presence bit, so the parent's `required` does not fire and its decode error
// stands alone.
func (w *walker) requiredHere(n *node, at Path, present bool, err error) error {
	if err != nil || present || !n.required || !w.dir.enforceRequired {
		return err
	}
	if n.kind == nLeaf || (n.kind == nPtr && n.elem.kind == nLeaf) {
		return err // a leaf's own required is direction.leaf's, and it already ran
	}
	return errAt(mWalk, ErrMissing, at, "required, and the plane supplied nothing under it")
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
				// ADR-0011: ferry authors the sentence and keeps the cause
				// reachable but unprinted. A codec's error is third-party text
				// and core makes no promise about what is in it.
				return false, errAt(mWalk, ErrValue, at, "%s", safeEncodeMsg(n.typ)).withCause(err)
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
			// n.key and not a lookup. #58: the compiled node carries the pair
			// the caller's registry resolved, so this renders what the
			// registrant's codec says whether or not anything is installed -
			// which is what makes the collapse check above reachable through
			// `Dump` at all.
			return sortedMapMembers(v, at, n.key)
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
		name:            "load",
		enforceRequired: true,
		leaf: func(n *node, v reflect.Value, at Path) (bool, error) {
			val, err := get(at)
			if err != nil {
				// ADR-0011's extension rule: core supplies the address, the
				// moment and the ErrDriver marker, and the driver may hold an
				// opinion about the class and about nothing else.
				return false, fromDriver(mWalk, at, true, err)
			}
			if val.Kind() == VAbsent {
				if n.required {
					// ADR-0011's shape rather than a bare fmt.Errorf, because
					// #14 reads the required set out of the error set and
					// message text is not API.
					return false, errAt(mWalk, ErrMissing, at, "required, and the plane supplied nothing")
				}
				if n.def != nil {
					// The default is a Value at an address, indistinguishable
					// at the boundary from what a flat plane would report.
					if err := decLeafWith(n.codec, *n.def, v); err != nil {
						return false, errAt(mWalk, ErrValue, at, "%s", safeDecodeMsg(*n.def, n.typ, err)).withCause(err)
					}
					return false, nil
				}
				return false, nil // Absent does not write
			}
			if err := decLeafWith(n.codec, val, v); err != nil {
				// D7. The naive form here was fmt.Errorf("ferry: %s: %w"),
				// which ADR-0011 measured four leaks in five for, on a plane
				// class where every value is a secret. The cause stays in the
				// chain - errors.Is(err, strconv.ErrRange) still answers - and
				// is never printed.
				return true, errAt(mWalk, ErrValue, at, "%s", safeDecodeMsg(val, n.typ, err)).withCause(err)
			}
			return true, nil
		},
		bound: func(n *node, at Path) error {
			en, ok := r.(FEnumerator)
			if !ok {
				// A source that cannot enumerate cannot report an index at
				// all, so there is no index to be out of range. ADR-0004's
				// Enumerator asymmetry, and not a silent skip: the addresses
				// this array reads are static and are all read regardless.
				return nil
			}
			kids, err := en.Children(ctx, at)
			if err != nil {
				return fromDriver(mWalk, at, true, err)
			}
			max := -1
			for _, k := range kids {
				segs := k.Segments()
				last := segs[len(segs)-1]
				if last.Kind != Index {
					continue
				}
				if i, err := strconv.Atoi(last.Text); err == nil && i > max {
					max = i
				}
			}
			if max >= n.n {
				// ADR-0005's published line, word for word. Everything it names
				// is STRUCTURE - an index and two lengths - so it survives the
				// redaction rule untouched.
				return errAt(mWalk, ErrValue, at, "plane has index %d, %s holds %d", max, n.typ, n.n)
			}
			return nil
		},
		container: func(n *node, v reflect.Value, at Path) (bool, bool, error) {
			val, err := get(at)
			if err != nil {
				return true, false, fromDriver(mWalk, at, true, err)
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
				return nil, errAt(mWalk, ErrPlane, at, "%s needs addresses the source cannot enumerate: "+
					"this source does not implement Enumerator", n.typ)
			}
			kids, err := en.Children(ctx, at)
			if err != nil {
				return nil, fromDriver(mWalk, at, true, err)
			}
			ms := make([]member, 0, len(kids))
			for _, k := range kids {
				segs := k.Segments()
				last := segs[len(segs)-1]
				if n.kind == nSlice {
					i, err := strconv.Atoi(last.Text)
					if err != nil {
						// The segment IS the address, so naming it is
						// ADR-0011's stated carve-out and not a leak.
						return nil, errAt(mWalk, ErrValue, at, "bad index %q", last.Text)
					}
					ms = append(ms, member{seg: last, idx: i})
					continue
				}
				// The Load half of the same pair, from the same node. Without
				// it a Dump that wrote the codec's text could not be read back
				// by the entry point that wrote it.
				key := reflect.New(n.typ.Key()).Elem()
				if err := n.key.parse(last.Text, key); err != nil {
					return nil, errAt(mWalk, ErrValue, at.Name(last.Text),
						"is not a valid %s map key", n.typ.Key()).withCause(err)
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

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// GONE UNDER #58: `keyCodecInstalled`, #31's seam for K31=10, which switched
// the stopgap install around the two caller-facing walks on and off.
//
// A seam is only worth its cost while both sides of it are reachable, and after
// #58 neither is: the walk consults no registry for a key at all, so there is
// no "installed" world and no "not installed" world to compare. K31=10 keeps
// its finding by reproducing the OLD RESOLUTION directly rather than by
// steering the engine into a state the engine no longer has - which is also the
// honest shape, because the defect was in where the answer came from and not in
// a flag.

// keyCollisionCheck is #31's seam: off, the walk overwrites and an entry
// vanishes with a nil error, which is the world as the tip shipped.
var keyCollisionCheck = true
