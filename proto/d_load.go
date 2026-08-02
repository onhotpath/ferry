package main

// #8's Load walk. Four things it does that the #7 walk did not:
//
//  1. It distinguishes ABSENT from NULL at every kind, rather than treating a
//     map miss as "nothing to do". Absent does not write. Null is a value the
//     plane holds, and it is admitted by exactly the Go types that can hold it.
//  2. It applies a declared default when, and only when, the plane is Absent.
//  3. It carries a PRESENCE BIT per subtree, which is 5.7's repair. xload
//     allocates a fresh zero value and reflect.DeepEqual's it to decide whether
//     to keep a nil struct pointer; that cannot tell a legitimately all-zero
//     subtree from an untouched one, AND it could not have been fixed in xload
//     even by threading a bool, because its Loader cannot report presence at
//     all (5.1). The bit is only correct because ADR-0004 fixed 5.1 first.
//  4. It carries the STATIC path alongside the realised one, because a
//     declaration attaches to the address SHAPE and a map key's realised
//     address is not in the schema.

import (
	"fmt"
	"reflect"
	"strconv"
)

type loadOpts struct {
	// defaultsCountAsPresence: does a declared default under a *T subtree
	// materialise the pointer? Probed both ways in D14.
	defaultsCountAsPresence bool
	// absentZeroesComposite: ADR-0005's literal wording, "a container address
	// with no children yields the zero value". Probed against "Absent does not
	// write" in D2.
	absentZeroesComposite bool
	// nullMeansZero / nullMeansAbsent: the two rejected readings of Null at a
	// leaf that cannot hold one. Probed in D3.
	nullMeansZero   bool
	nullMeansAbsent bool
	observe         func(Path, Value)
	// byRealisedAddress: look declarations up by the realised address rather
	// than by the static shape. On is the bug D15 measures.
	byRealisedAddress bool
}

// planeAt reads the boundary observation at p. A map miss IS Absent, which is
// ADR-0004's "Absent is kind zero" property being used rather than restated.
func planeAt(vals map[Path]Value, p Path) Value { return vals[p] }

func loadD(vals map[Path]Value, s *schema, dst reflect.Value, o loadOpts) (bool, error) {
	// sp is the STATIC path (the schema's), p is the REALISED one (the plane's).
	// They diverge under a map or a slice, and only sp indexes declarations.
	var rec func(v reflect.Value, p, sp Path) (bool, error)

	rec = func(v reflect.Value, p, sp Path) (bool, error) {
		sh := classify(v.Type())
		lookup := sp
		if o.byRealisedAddress {
			lookup = p
		}
		opts := s.at(lookup)

		absentAt := func() (bool, error) {
			if opts.required {
				return false, fmt.Errorf("ferry: %s: required, and the plane does not have it", p)
			}
			if opts.hasDef {
				// A default IS a plane value at this address: same kind, same
				// parser, same errors, decoded fresh on every load.
				if err := decLeaf(*opts.def, v); err != nil {
					return false, fmt.Errorf("ferry: %s: %v", p, err)
				}
				return o.defaultsCountAsPresence, nil
			}
			// Absent does not write. Whatever the field held survives, which
			// is what makes a seeded struct a defaults mechanism with no core
			// surface at all.
			return false, nil
		}

		switch sh {
		case shapeLeaf:
			val := planeAt(vals, p)
			if o.observe != nil {
				o.observe(p, val)
			}
			switch {
			case val.Kind() == VAbsent:
				return absentAt()
			case val.Kind() == VNull && o.nullMeansAbsent:
				return absentAt()
			case val.Kind() == VNull && o.nullMeansZero:
				v.Set(reflect.Zero(v.Type()))
				return true, nil
			}
			// Everything else, Null included, goes to the type set. A leaf
			// that can hold a null accepts it; every other leaf refuses it as
			// a wrong kind, which is ADR-0005's existing rule with nothing
			// added for #8.
			if err := decLeaf(val, v); err != nil {
				return true, fmt.Errorf("ferry: %s: %v", p, err)
			}
			return true, nil

		case shapePointer:
			val := planeAt(vals, p)
			if classify(v.Type().Elem()) == shapeLeaf {
				// A pointer to a LEAF has its own address, and this is where
				// *T earns its keep: nil and &zero are two observations.
				if o.observe != nil {
					o.observe(p, val)
				}
				switch val.Kind() {
				case VAbsent:
					if opts.required {
						return false, fmt.Errorf("ferry: %s: required, and the plane does not have it", p)
					}
					if opts.hasDef {
						nv := reflect.New(v.Type().Elem())
						if err := decLeaf(*opts.def, nv.Elem()); err != nil {
							return false, fmt.Errorf("ferry: %s: %v", p, err)
						}
						v.Set(nv)
						return o.defaultsCountAsPresence, nil
					}
					return false, nil
				case VNull:
					v.Set(reflect.Zero(v.Type()))
					return true, nil
				}
				nv := reflect.New(v.Type().Elem())
				if err := decLeaf(val, nv.Elem()); err != nil {
					return true, fmt.Errorf("ferry: %s: %v", p, err)
				}
				v.Set(nv)
				return true, nil
			}
			if val.Kind() == VNull {
				if o.observe != nil {
					o.observe(p, val)
				}
				v.Set(reflect.Zero(v.Type()))
				return true, nil
			}
			if opts.required && val.Kind() == VAbsent && len(children(vals, p)) == 0 {
				return false, fmt.Errorf("ferry: %s: required, and the plane does not have it", p)
			}
			// A pointer to a COMPOSITE. Materialise it exactly when something
			// under it was present. This is 5.7, and it is a walk decision
			// rather than a comparison against a fresh zero value.
			probe := reflect.New(v.Type().Elem())
			got, err := rec(probe.Elem(), p, sp)
			if err != nil {
				return got, err
			}
			if !got {
				return false, nil // untouched: the pointer stays as it was
			}
			v.Set(probe)
			return true, nil

		case shapeStruct:
			any := false
			var walk func(reflect.Value, Path, Path) error
			walk = func(v reflect.Value, p, sp Path) error {
				for i := range v.NumField() {
					f := v.Type().Field(i)
					if t11Mode {
						// One field rule for the compiler and the walk. Two
						// would let the schema promise an address the walk
						// never visits, which is a silent loss.
						plan, _ := planField(f)
						if plan.skip {
							continue
						}
						if plan.promote {
							if err := walk(v.Field(i), p, sp); err != nil {
								return err
							}
							continue
						}
						got, err := rec(v.Field(i), p.Name(plan.decl.name), sp.Name(plan.decl.name))
						any = any || got
						if err != nil {
							return err
						}
						continue
					}
					if !f.IsExported() {
						continue
					}
					n, _, _ := fieldTag(f)
					got, err := rec(v.Field(i), p.Name(n), sp.Name(n))
					any = any || got
					if err != nil {
						return err
					}
				}
				return nil
			}
			if err := walk(v, p, sp); err != nil {
				return any, err
			}
			if opts.required && !any {
				return false, fmt.Errorf("ferry: %s: required, and the plane supplied nothing under it", p)
			}
			return any, nil

		case shapeSlice:
			val := planeAt(vals, p)
			if o.observe != nil {
				o.observe(p, val)
			}
			if opts.required && val.Kind() == VAbsent && len(children(vals, p)) == 0 {
				// A container's presence is children, or a Null at its own
				// address. It cannot be present-and-empty, because no plane can
				// report that (ADR-0005), so a required composite cannot be
				// satisfied by an empty one.
				return false, fmt.Errorf("ferry: %s: required, and the plane does not have it", p)
			}
			if val.Kind() == VNull {
				v.Set(reflect.Zero(v.Type()))
				return true, nil
			}
			kids := children(vals, p)
			if len(kids) == 0 && v.Kind() != reflect.Array {
				if o.absentZeroesComposite {
					v.Set(reflect.Zero(v.Type()))
				}
				return false, nil
			}
			n := 0
			for _, k := range kids {
				segs := k.Segments()
				last := segs[len(segs)-1]
				if last.Kind != Index {
					return true, fmt.Errorf("ferry: %s: sequence child %s is not an index", p, k)
				}
				i, err := strconv.Atoi(last.Text)
				if err != nil {
					return true, fmt.Errorf("ferry: %s: bad index %q", p, last.Text)
				}
				if i+1 > n {
					n = i + 1
				}
			}
			if v.Kind() == reflect.Array {
				if n > v.Len() {
					return true, fmt.Errorf("ferry: %s: plane has index %d, %s holds %d", p, n-1, v.Type(), v.Len())
				}
				// An ARRAY's element addresses are static, so every element is
				// walked whether or not the plane has anything under it, exactly
				// as every struct field is. Walking only the present ones would
				// make an array element's declarations conditional on a sibling,
				// which no static address is anywhere else.
				any := false
				for i := range v.Len() {
					got, err := rec(v.Index(i), p.Index(i), sp.Index(i))
					any = any || got
					if err != nil {
						return any, err
					}
				}
				if opts.required && len(kids) == 0 {
					return false, fmt.Errorf("ferry: %s: required, and the plane supplied nothing under it", p)
				}
				return any, nil
			}
			v.Set(reflect.MakeSlice(v.Type(), n, n))
			for _, k := range kids {
				segs := k.Segments()
				i, _ := strconv.Atoi(segs[len(segs)-1].Text)
				if _, err := rec(v.Index(i), k, sp.Name("*")); err != nil {
					return true, err
				}
			}
			return true, nil

		case shapeMap:
			val := planeAt(vals, p)
			if o.observe != nil {
				o.observe(p, val)
			}
			if opts.required && val.Kind() == VAbsent && len(children(vals, p)) == 0 {
				// A container's presence is children, or a Null at its own
				// address. It cannot be present-and-empty, because no plane can
				// report that (ADR-0005), so a required composite cannot be
				// satisfied by an empty one.
				return false, fmt.Errorf("ferry: %s: required, and the plane does not have it", p)
			}
			if val.Kind() == VNull {
				v.Set(reflect.Zero(v.Type()))
				return true, nil
			}
			kids := children(vals, p)
			if len(kids) == 0 {
				if o.absentZeroesComposite {
					v.Set(reflect.Zero(v.Type()))
				}
				return false, nil
			}
			m := reflect.MakeMapWithSize(v.Type(), len(kids))
			for _, k := range kids {
				segs := k.Segments()
				elem := reflect.New(v.Type().Elem()).Elem()
				if _, err := rec(elem, k, sp.Name("*")); err != nil {
					return true, err
				}
				key := reflect.New(v.Type().Key()).Elem()
				if err := decMapKey(segs[len(segs)-1].Text, key); err != nil {
					return true, err
				}
				m.SetMapIndex(key, elem)
			}
			v.Set(m)
			return true, nil
		}
		return false, unsupportedTypeError{p, v.Type()}
	}

	return rec(dst, Path{}, Path{})
}

// --- Dump, with #8's omission rule -----------------------------------------

// dumpD records every Set call, and records omissions as "no call at all"
// rather than as an Absent value. ADR-0004 flagged the likely answer and left
// it here: ferry never hands a sink an Absent.
type setCall struct {
	p Path
	v Value
}

func dumpD(v reflect.Value, s *schema) ([]setCall, error) {
	var out []setCall
	var rec func(v reflect.Value, p, sp Path) error
	rec = func(v reflect.Value, p, sp Path) error {
		opts := s.at(sp)
		if opts.omitzero && v.IsZero() {
			return nil // omission is the ABSENCE OF A CALL, never a value
		}
		switch classify(v.Type()) {
		case shapeLeaf:
			val, err := encLeaf(v)
			if err != nil {
				return err
			}
			out = append(out, setCall{p, val})
			return nil
		case shapePointer:
			if v.IsNil() {
				out = append(out, setCall{p, Null()})
				return nil
			}
			return rec(v.Elem(), p, sp)
		case shapeStruct:
			var walk func(reflect.Value, Path, Path) error
			walk = func(v reflect.Value, p, sp Path) error {
				for i := range v.NumField() {
					f := v.Type().Field(i)
					if t11Mode {
						plan, _ := planField(f)
						if plan.skip {
							continue
						}
						if plan.promote {
							if err := walk(v.Field(i), p, sp); err != nil {
								return err
							}
							continue
						}
						if err := rec(v.Field(i), p.Name(plan.decl.name), sp.Name(plan.decl.name)); err != nil {
							return err
						}
						continue
					}
					if !f.IsExported() {
						continue
					}
					n, _, _ := fieldTag(f)
					if err := rec(v.Field(i), p.Name(n), sp.Name(n)); err != nil {
						return err
					}
				}
				return nil
			}
			return walk(v, p, sp)
		case shapeSlice:
			if v.Len() == 0 && v.Kind() != reflect.Array {
				out = append(out, setCall{p, Null()})
				return nil
			}
			for i := range v.Len() {
				esp := sp.Name("*")
				if v.Kind() == reflect.Array {
					esp = sp.Index(i)
				}
				if err := rec(v.Index(i), p.Index(i), esp); err != nil {
					return err
				}
			}
			return nil
		case shapeMap:
			if v.Len() == 0 {
				out = append(out, setCall{p, Null()})
				return nil
			}
			keys := v.MapKeys()
			sortMapKeys(keys)
			for _, k := range keys {
				if err := rec(v.MapIndex(k), p.Name(mapKeyText(k)), sp.Name("*")); err != nil {
					return err
				}
			}
			return nil
		}
		return unsupportedTypeError{p, v.Type()}
	}
	if err := rec(v, Path{}, Path{}); err != nil {
		return nil, err
	}
	return out, nil
}

func sortMapKeys(keys []reflect.Value) {
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && mapKeyText(keys[j]) < mapKeyText(keys[j-1]); j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
}

func callsToMap(cs []setCall) map[Path]Value {
	m := make(map[Path]Value, len(cs))
	for _, c := range cs {
		m[c.p] = c.v
	}
	return m
}
