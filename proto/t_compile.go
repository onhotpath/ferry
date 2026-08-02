package main

// compileT is #8's compiler with #11's grammar in place of the placeholder.
//
// Everything below the field rule is ADR-0006's, unchanged and re-run rather
// than re-derived: the five refusals, the admissibility-before-contradictions
// diagnostic rule, and the default-is-a-String-Value-at-an-address mechanism.
// What is new here is only where the declarations come from.

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

func toFieldOpts(d tagDecl) fieldOpts {
	o := fieldOpts{required: d.required, omitzero: d.omitzero}
	if d.hasDef {
		v := String(d.defText)
		o.hasDef, o.defText, o.def = true, d.defText, &v
	}
	return o
}

// Validate is the entry point the ticket's own comment asks for: tag rejection
// assertable from a test with no value in hand and no plane reachable.
//
// It is schema compile with the schema thrown away, deliberately. Two entry
// points that could disagree about whether a type is legal would be the viper
// defect at ferry's own front door.
func Validate[T any]() error {
	_, err := compileT(reflect.TypeFor[T]())
	return err
}

func compileT(t reflect.Type) (*schema, error) {
	s := &schema{root: t, opts: map[Path]fieldOpts{}, shapes: map[Path]shape{}}
	var errs []error
	stack := map[reflect.Type]bool{}

	var rec func(reflect.Type, Path, fieldOpts)
	rec = func(t reflect.Type, p Path, o fieldOpts) {
		sh := classify(t)
		s.shapes[p] = sh
		if sh != shapeLeaf && stack[t] {
			errs = append(errs, fmt.Errorf("ferry: %s: %s is recursive", pathOrRoot(p), t))
			return
		}
		if sh != shapeLeaf {
			stack[t] = true
			defer delete(stack, t)
		}

		// ADR-0006's diagnostic rule: admissibility first, contradictions only
		// among the options that survived it.
		reqOK, defOK := o.required, o.hasDef
		if o.required && !requiredAdmissible(t) {
			errs = append(errs, requiredOnCompositeErr(p, t, sh))
			reqOK = false
		}
		if o.hasDef {
			switch sh {
			case shapeLeaf:
				probe := reflect.New(t).Elem()
				if err := decLeaf(*o.def, probe); err != nil {
					errs = append(errs, fmt.Errorf("ferry: %s: default %q is not a valid %s: %v", pathOrRoot(p), o.defText, t, err))
					defOK = false
				}
			case shapePointer:
				if classify(t.Elem()) == shapeLeaf {
					probe := reflect.New(t.Elem()).Elem()
					if err := decLeaf(*o.def, probe); err != nil {
						errs = append(errs, fmt.Errorf("ferry: %s: default %q is not a valid %s: %v", pathOrRoot(p), o.defText, t.Elem(), err))
						defOK = false
					}
				} else {
					errs = append(errs, defOnCompositeErr(p, t))
					defOK = false
				}
			default:
				errs = append(errs, defOnCompositeErr(p, t))
				defOK = false
			}
			if reqOK && defOK {
				errs = append(errs, fmt.Errorf("ferry: %s: required and default contradict: a default answers absence and required forbids it", pathOrRoot(p)))
			}
			if defOK && o.omitzero && sh == shapeLeaf {
				probe := reflect.New(t).Elem()
				if err := decLeaf(*o.def, probe); err == nil && !probe.IsZero() {
					errs = append(errs, fmt.Errorf("ferry: %s: omitzero and default=%s contradict: an explicit zero would be omitted and would load back as %s",
						pathOrRoot(p), o.defText, o.defText))
				}
			}
		}

		switch sh {
		case shapeLeaf:
			s.addrs = append(s.addrs, p)
			s.opts[p] = o
		case shapePointer:
			if classify(t.Elem()) == shapeLeaf {
				s.addrs = append(s.addrs, p)
				s.opts[p] = o
				s.shapes[p] = shapePointer
				return
			}
			s.opts[p] = o
			rec(t.Elem(), p, fieldOpts{})
		case shapeStruct:
			before := len(s.addrs)
			bad := walkStructFields(t, p, &errs, rec)
			// ADR-0006's diagnostic rule, one level up. "maps no address" is
			// only meaningful if every field at this level was understood; a
			// struct whose one field carries a misspelled option maps no
			// address BECAUSE of that, and reporting both makes one mistake
			// read as two.
			if len(s.addrs) == before && !bad {
				errs = append(errs, fmt.Errorf("ferry: %s: %s maps no address", pathOrRoot(p), t))
			}
			if o.required {
				s.opts[p] = o
			}
		case shapeSlice:
			s.opts[p] = o
			if t.Kind() == reflect.Array {
				for i := range t.Len() {
					rec(t.Elem(), p.Index(i), fieldOpts{})
				}
				return
			}
			rec(t.Elem(), p.Name("*"), fieldOpts{})
		case shapeMap:
			s.opts[p] = o
			rec(t.Elem(), p.Name("*"), fieldOpts{})
		default:
			errs = append(errs, unsupportedTypeError{p, t})
		}
	}
	rec(t, Path{}, fieldOpts{})
	// ADR-0003's core-side collision rule, applied rather than re-decided: the
	// static address set is prefix-free, and a path is a prefix of itself.
	errs = append(errs, prefixFree(s.addrs)...)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return s, nil
}

// walkStructFields is the field rule. An embedded field with no ferry tag is
// walked AT THE PARENT ADDRESS, which is what makes promotion need no word in
// the vocabulary.
func walkStructFields(t reflect.Type, p Path, errs *[]error, rec func(reflect.Type, Path, fieldOpts)) bool {
	bad := false
	for i := range t.NumField() {
		f := t.Field(i)
		plan, ferrs := planField(f)
		for _, e := range ferrs {
			bad = true
			*errs = append(*errs, fmt.Errorf("ferry: %s: %v", pathOrRoot(p.Name(f.Name)), e))
		}
		if plan.skip {
			continue
		}
		if plan.promote {
			et := f.Type
			// A promoted field's addresses land at the PARENT, so the pointer
			// has no address subtree of its own and ADR-0006's presence bit
			// has nothing to materialise it from. Measured before this
			// refusal existed: a promoted embedded *T compiled clean, loaded
			// into a nil pointer with a nil error, and dumped through one.
			if et.Kind() == reflect.Pointer {
				*errs = append(*errs, fmt.Errorf("ferry: %s: %s is an embedded pointer with no ferry tag, and a promoted field has no address of its own for the pointer to be optional at; give it a ferry tag to nest it, or ferry:\"-\"",
					pathOrRoot(p.Name(f.Name)), f.Type))
				bad = true
				continue
			}
			if et.Kind() != reflect.Struct || classify(et) != shapeStruct {
				*errs = append(*errs, fmt.Errorf("ferry: %s: %s is embedded and is not a struct ferry walks, so its fields cannot be promoted; give it a ferry tag to nest it, or ferry:\"-\"",
					pathOrRoot(p.Name(f.Name)), f.Type))
				continue
			}
			if walkStructFields(et, p, errs, rec) {
				bad = true
			}
			continue
		}
		if !plan.decl.hasName {
			continue // the name was refused; the error is already recorded
		}
		rec(f.Type, p.Name(plan.decl.name), toFieldOpts(plan.decl))
	}
	return bad
}

func errLines(err error) []string {
	if err == nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(err.Error(), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func printErrs(indent string, err error) {
	if err == nil {
		fmt.Println(indent + "compiles")
		return
	}
	for _, l := range errLines(err) {
		fmt.Println(indent + l)
	}
}

// prefixFree is ADR-0003's core-side rule. It is here because promotion is the
// one thing #11's field rule adds that can manufacture a clash, and the point
// is that it needs no new rule to catch it.
func prefixFree(ps []Path) []error {
	var errs []error
	for i := range ps {
		for j := range ps {
			if i >= j {
				continue
			}
			a, b := ps[i].String(), ps[j].String()
			switch {
			case a == b:
				errs = append(errs, fmt.Errorf("ferry: two fields address %s", a))
			case strings.HasPrefix(b, a) && (b[len(a)] == '/' || b[len(a)] == '#'):
				errs = append(errs, fmt.Errorf("ferry: %s is a prefix of %s", a, b))
			case strings.HasPrefix(a, b) && (a[len(b)] == '/' || a[len(b)] == '#'):
				errs = append(errs, fmt.Errorf("ferry: %s is a prefix of %s", b, a))
			}
		}
	}
	return errs
}
