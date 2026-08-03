package main

// The walk, both directions, over the candidate type set.
// Deliberately minimal: a leaf gets exactly one address, a composite gets one
// address per element (ADR-0003), and nothing is ever flattened into a joined
// string.

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
)

// --- schema compile: the static address set, from the type alone ------------

// unsupportedType is a *ferry.Error like every other schema refusal, so it
// carries the moment and the class the sort key and Elements() need. #41 D8's
// compiler half: it used to be a bare struct with its own Error(), which
// sorted as mNone and made a refusal indistinguishable from anything else.
func unsupportedType(p Path, t reflect.Type) error {
	return errAt(mCompile, ErrSchema, p, "unsupported type %s (kind %s)", t, t.Kind())
}

// compile walks the TYPE, with no value in hand, and returns the static
// address set. It is where a type outside the set is refused.
func compile(t reflect.Type) ([]Path, error) {
	var out []Path
	var errs []error
	// The type-stack that makes a recursive type a refusal rather than a hang.
	// It is a stack and not a seen-set: the same type appearing twice as
	// SIBLINGS is fine, and only appearing twice on one path is a cycle.
	stack := map[reflect.Type]bool{}
	var rec func(reflect.Type, Path)
	rec = func(t reflect.Type, p Path) {
		if classify(t) != shapeLeaf && stack[t] {
			errs = append(errs, fmt.Errorf(
				"ferry: %s: %s is recursive, so its address set is unbounded; register a codec for it",
				pathOrRoot(p), t))
			return
		}
		if classify(t) != shapeLeaf {
			stack[t] = true
			defer delete(stack, t)
		}
		switch classify(t) {
		case shapeLeaf:
			out = append(out, p)
		case shapePointer:
			rec(t.Elem(), p)
		case shapeStruct:
			// A struct is admitted by kind, so every struct "is supported".
			// The rule that stops that being a silent lossy dump is that a
			// struct which contributes no address does not compile. It is
			// checked at every level, not only at the root, because one
			// mapped sibling would otherwise hide the loss.
			before := len(out)
			for i := range t.NumField() {
				f := t.Field(i)
				if !f.IsExported() {
					continue
				}
				rec(f.Type, p.Name(fieldName(f)))
			}
			if len(out) == before {
				errs = append(errs, fmt.Errorf(
					"ferry: %s: %s maps no address: it has no exported field ferry can address; register a codec for it",
					pathOrRoot(p), t))
			}
		case shapeSlice:
			// An ARRAY's length is part of its type, so its element
			// addresses are static: N of them, known with no value in hand.
			// A SLICE's are dynamic. That difference is not cosmetic - it
			// decides whether the field is loadable from a source that
			// cannot enumerate (ADR-0004's Enumerator asymmetry).
			if t.Kind() == reflect.Array {
				for i := range t.Len() {
					rec(t.Elem(), p.Index(i))
				}
				return
			}
			rec(t.Elem(), p.Name("*"))
		case shapeMap:
			rec(t.Elem(), p.Name("*"))
		default:
			// A refused map key gets its own diagnostic, because the generic
			// "unsupported type" line tells the author nothing about the
			// obligation they have to take on, and ADR-0009 is explicit that the
			// diagnostic IS the mechanism.
			//
			// It calls mapKeyRefusal rather than carrying its own copy. #45 added
			// a SECOND such diagnostic - for a chain-claimed key, which ADR-0007's
			// reversal now refuses - and this file held a hand-written copy of the
			// first, so the two engines would have told a user different things.
			// That is ADR-0010's duplication axis 1, and #41 D2 closed the same
			// shape for kind admission by making one function the authority.
			if t.Kind() == reflect.Map && !validMapKey(t.Key()) {
				errs = append(errs, mapKeyRefusal(p, t.Key()))
				return
			}
			errs = append(errs, unsupportedType(p, t))
		}
	}
	rec(t, Path{})
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

func pathOrRoot(p Path) string {
	if p.IsRoot() {
		return "(root)"
	}
	return p.String()
}

func fieldName(f reflect.StructField) string {
	if tag := f.Tag.Get("ferry"); tag != "" {
		return tag
	}
	return f.Name
}

// --- dump: value -> map[Path]Value -----------------------------------------

func dump(v reflect.Value) (map[Path]Value, error) {
	out := map[Path]Value{}
	var rec func(reflect.Value, Path) error
	rec = func(v reflect.Value, p Path) error {
		switch classify(v.Type()) {
		case shapeLeaf:
			val, err := encLeaf(v)
			if err != nil {
				return err
			}
			out[p] = val
			return nil
		case shapePointer:
			if v.IsNil() {
				out[p] = Null()
				return nil
			}
			return rec(v.Elem(), p)
		case shapeStruct:
			for i := range v.NumField() {
				f := v.Type().Field(i)
				if !f.IsExported() {
					continue
				}
				if err := rec(v.Field(i), p.Name(fieldName(f))); err != nil {
					return err
				}
			}
			return nil
		case shapeSlice:
			// A composite with no elements mints no element address, so if
			// it also minted nothing of its own it would be indistinguishable
			// from absent - and as a map value its key would vanish outright,
			// which ADR-0001 rules out. So nil and empty both write Null.
			if v.Len() == 0 && v.Kind() != reflect.Array {
				out[p] = Null()
				return nil
			}
			for i := range v.Len() {
				if err := rec(v.Index(i), p.Index(i)); err != nil {
					return err
				}
			}
			return nil
		case shapeMap:
			if v.Len() == 0 {
				out[p] = Null()
				return nil
			}
			// Determinism is a package-wide invariant: sort the keys. And the
			// collapse check rides along, through the same helper the engine uses,
			// so the two do not drift about what a lost map entry looks like.
			ms, merr := sortedMapMembers(v, p)
			if merr != nil {
				return merr
			}
			for _, m := range ms {
				if err := rec(v.MapIndex(m.key), p.Name(m.seg.Text)); err != nil {
					return err
				}
			}
			return nil
		}
		// Same authority as the compile-time site above: a refused map key says
		// WHY here too, or a user who never calls Compile[T] sees the generic
		// line and the compile-time one disagreeing about the same type.
		if v.Kind() == reflect.Map && !validMapKey(v.Type().Key()) {
			return mapKeyRefusal(p, v.Type().Key())
		}
		return unsupportedType(p, v.Type())
	}
	if err := rec(v, Path{}); err != nil {
		return nil, err
	}
	return out, nil
}

// decMapKey turns a Name segment's text back into a map key. It is a decode
// and not a conversion: only a string key is a conversion, and everything else
// has to be parsed, which is why the admissible key set is not "any comparable".
func decMapKey(text string, dst reflect.Value) error {
	// A registered codec whose form is a String serves as a key, because a
	// key is only ever segment text. The obligation it carries is stronger
	// than a leaf codec's: the text must be INJECTIVE over the key type, or
	// two distinct keys collapse into one address.
	if c, ok := identityLookup(dst.Type()); ok {
		return c.dec(String(text), dst)
	}
	if c, ok := activeChainCodec(dst.Type()); ok {
		return c.dec(String(text), dst)
	}
	switch dst.Kind() {
	case reflect.String:
		dst.SetString(text)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return decLeaf(Number(text), dst)
	}
	return fmt.Errorf("ferry: unsupported map key type %s", dst.Type())
}

func mapKeyText(k reflect.Value) string {
	if c, ok := identityLookup(k.Type()); ok {
		if v, err := c.enc(k); err == nil && v.Kind() == VString {
			return v.Text()
		}
	}
	if c, ok := activeChainCodec(k.Type()); ok {
		if v, err := c.enc(k); err == nil && v.Kind() == VString {
			return v.Text()
		}
	}
	switch k.Kind() {
	case reflect.String:
		return k.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(k.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(k.Uint(), 10)
	}
	return fmt.Sprintf("%v", k.Interface())
}

// --- load: map[Path]Value -> value -----------------------------------------

// children enumerates the addresses present under p, which is what a real
// source's Enumerator does. Here the map stands in for the plane.
func children(vals map[Path]Value, p Path) []Path {
	seen := map[Path]bool{}
	var out []Path
	pre := p.String()
	for a := range vals {
		s := a.String()
		if len(s) <= len(pre) || s[:len(pre)] != pre {
			continue
		}
		rest := s[len(pre):]
		// cut at the next sigil after position 0
		end := len(rest)
		for i := 1; i < len(rest); i++ {
			if rest[i] == sigilName || rest[i] == sigilIndex {
				end = i
				break
			}
		}
		c := Path{pre + rest[:end]}
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return sortedPaths(out)
}

func load(vals map[Path]Value, dst reflect.Value) error {
	var rec func(reflect.Value, Path) error
	rec = func(v reflect.Value, p Path) error {
		switch classify(v.Type()) {
		case shapeLeaf:
			val, ok := vals[p]
			if !ok || val.Kind() == VAbsent {
				return nil
			}
			return decLeaf(val, v)
		case shapePointer:
			if val, ok := vals[p]; ok && val.Kind() == VNull {
				v.Set(reflect.Zero(v.Type()))
				return nil
			}
			v.Set(reflect.New(v.Type().Elem()))
			return rec(v.Elem(), p)
		case shapeStruct:
			for i := range v.NumField() {
				f := v.Type().Field(i)
				if !f.IsExported() {
					continue
				}
				if err := rec(v.Field(i), p.Name(fieldName(f))); err != nil {
					return err
				}
			}
			return nil
		case shapeSlice:
			if val, ok := vals[p]; ok && val.Kind() == VNull {
				v.Set(reflect.Zero(v.Type()))
				return nil
			}
			// The Index segment carries the position, so read it rather than
			// assigning positionally. Assigning positionally silently
			// reindexes when an element minted no address, which turns an
			// absent element into corruption of every element after it.
			kids := children(vals, p)
			n := 0
			for _, k := range kids {
				segs := k.Segments()
				last := segs[len(segs)-1]
				if last.Kind != Index {
					return fmt.Errorf("ferry: %s: sequence child %s is not an index", p, k)
				}
				i, err := strconv.Atoi(last.Text)
				if err != nil {
					return fmt.Errorf("ferry: %s: bad index %q", p, last.Text)
				}
				if i+1 > n {
					n = i + 1
				}
			}
			if v.Kind() == reflect.Array {
				// An array's length is the type's, not the plane's. An index
				// the array cannot hold is loud; a missing one leaves the
				// element at its zero value, which is #8's rule applied.
				if n > v.Len() {
					return fmt.Errorf("ferry: %s: plane has index %d, %s holds %d", p, n-1, v.Type(), v.Len())
				}
			} else {
				v.Set(reflect.MakeSlice(v.Type(), n, n))
			}
			for _, k := range kids {
				segs := k.Segments()
				i, _ := strconv.Atoi(segs[len(segs)-1].Text)
				if err := rec(v.Index(i), k); err != nil {
					return err
				}
			}
			return nil
		case shapeMap:
			kids := children(vals, p)
			if len(kids) == 0 {
				v.Set(reflect.Zero(v.Type()))
				return nil
			}
			m := reflect.MakeMapWithSize(v.Type(), len(kids))
			for _, k := range kids {
				segs := k.Segments()
				elem := reflect.New(v.Type().Elem()).Elem()
				if err := rec(elem, k); err != nil {
					return err
				}
				key := reflect.New(v.Type().Key()).Elem()
				if err := decMapKey(segs[len(segs)-1].Text, key); err != nil {
					return err
				}
				m.SetMapIndex(key, elem)
			}
			v.Set(m)
			return nil
		}
		// Same authority as the compile-time site above: a refused map key says
		// WHY here too, or a user who never calls Compile[T] sees the generic
		// line and the compile-time one disagreeing about the same type.
		if v.Kind() == reflect.Map && !validMapKey(v.Type().Key()) {
			return mapKeyRefusal(p, v.Type().Key())
		}
		return unsupportedType(p, v.Type())
	}
	return rec(dst, Path{})
}

func sortedAddrs(vals map[Path]Value) []Path {
	out := make([]Path, 0, len(vals))
	for p := range vals {
		out = append(out, p)
	}
	return sortedPaths(out)
}

var _ = slices.Sort[[]int]
