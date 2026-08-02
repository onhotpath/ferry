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
	"sort"
	"strconv"
)

// --- schema compile: the static address set, from the type alone ------------

type unsupportedTypeError struct {
	path Path
	typ  reflect.Type
}

func (e unsupportedTypeError) Error() string {
	return fmt.Sprintf("ferry: %s: unsupported type %s (kind %s)", e.path, e.typ, e.typ.Kind())
}

// compile walks the TYPE, with no value in hand, and returns the static
// address set. It is where a type outside the set is refused.
func compile(t reflect.Type) ([]Path, error) {
	var out []Path
	var errs []error
	var rec func(reflect.Type, Path)
	rec = func(t reflect.Type, p Path) {
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
		case shapeSlice, shapeMap:
			// dynamic: addresses come from the value, not the type.
			// The element type is still checked here, which is the whole
			// reason compile can refuse before a value exists.
			rec(t.Elem(), p.Name("*"))
		default:
			errs = append(errs, unsupportedTypeError{p, t})
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
			// Determinism is a package-wide invariant: sort the keys.
			keys := v.MapKeys()
			sort.Slice(keys, func(i, j int) bool { return mapKeyText(keys[i]) < mapKeyText(keys[j]) })
			for _, k := range keys {
				if err := rec(v.MapIndex(k), p.Name(mapKeyText(k))); err != nil {
					return err
				}
			}
			return nil
		}
		return unsupportedTypeError{p, v.Type()}
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
			v.Set(reflect.MakeSlice(v.Type(), n, n))
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
		return unsupportedTypeError{p, v.Type()}
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
