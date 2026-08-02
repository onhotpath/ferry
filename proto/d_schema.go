package main

// #8's schema: the static address set, plus what each address declares.
//
// The load-bearing claim under test is that a DECLARED DEFAULT IS A Value AT
// AN ADDRESS, of kind String, indistinguishable at the boundary from what a
// flat plane would have reported there. If that holds, defaults need no second
// conversion authority (research 4b) and they compose with the codec chain
// (#12's) for free.
//
// The tag spelling here is a placeholder. #11 owns every spelling; what is
// under test is the mechanism and where it lives.

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// allowRequiredOnComposite re-enables the pre-option-2 behaviour, so the
// probes that documented it still run.
var allowRequiredOnComposite bool

type fieldOpts struct {
	def      *Value // declared default, always String kind; nil = no default
	defText  string
	hasDef   bool
	required bool
	omitzero bool
}

type schema struct {
	root   reflect.Type
	addrs  []Path
	opts   map[Path]fieldOpts
	shapes map[Path]shape // shape of the type AT that address
}

func (s *schema) at(p Path) fieldOpts { return s.opts[p] }

// parseTag is a placeholder grammar, not a proposal. #11 owns it.
// The one property it must have and that is #8's to require: an EMPTY default
// must be expressible and distinguishable from NO default, because "" is a
// legitimate default value.
func parseTag(tag string) (name string, o fieldOpts, err error) {
	parts := strings.Split(tag, ",")
	name = parts[0]
	for _, opt := range parts[1:] {
		switch {
		case opt == "required":
			o.required = true
		case opt == "omitzero":
			o.omitzero = true
		case strings.HasPrefix(opt, "default="):
			o.hasDef = true
			o.defText = strings.TrimPrefix(opt, "default=")
			v := String(o.defText)
			o.def = &v
		default:
			err = fmt.Errorf("unknown option %q", opt)
		}
	}
	return name, o, err
}

// t11Mode routes the walkers through #11's grammar instead of #8's
// placeholder, so an end-to-end probe exercises the real names.
var t11Mode bool

func fieldTag(f reflect.StructField) (string, fieldOpts, error) {
	if t11Mode {
		plan, errs := planField(f)
		if len(errs) > 0 {
			return "", fieldOpts{}, errs[0]
		}
		if plan.skip || plan.promote {
			return "", fieldOpts{}, nil
		}
		return plan.decl.name, toFieldOpts(plan.decl), nil
	}
	tag := f.Tag.Get("ferry")
	if tag == "" {
		return f.Name, fieldOpts{}, nil
	}
	n, o, err := parseTag(tag)
	if n == "" {
		n = f.Name
	}
	return n, o, err
}

// compileD is compile() plus #8's declarations. Everything it checks is
// checked from reflect.TypeFor[T]() alone, with no value in hand and no plane
// reachable, which is the assertability property ADR-0001 claims for tag
// rejection and ADR-0003 for the static half of the collision rule.
func compileD(t reflect.Type) (*schema, error) {
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

		// `required` names an address, so it is legal only where that address
		// is ALWAYS realised. A composite's own address exists only when it is
		// nil or empty, and a non-pointer struct and an array have none at all.
		// ADMISSIBILITY first: is each option legal at this type on its own?
		// A CONTRADICTION between two options is only meaningful if both of
		// them survived that, or one mistake reports as three errors.
		reqOK, defOK := o.required, o.hasDef
		if o.required && !allowRequiredOnComposite {
			if !requiredAdmissible(t) {
				errs = append(errs, requiredOnCompositeErr(p, t, sh))
				reqOK = false
			}
		}

		// A declaration is legal only where a value can actually land.
		if o.hasDef {
			switch sh {
			case shapeLeaf:
				// Validate NOW, from the type alone: parse the declared text
				// with exactly the parser that leaf's own kind uses.
				probe := reflect.New(t).Elem()
				if err := decLeaf(*o.def, probe); err != nil {
					errs = append(errs, fmt.Errorf(
						"ferry: %s: default %q is not a valid %s: %v", pathOrRoot(p), o.defText, t, err))
					defOK = false
				}
			case shapePointer:
				if classify(t.Elem()) == shapeLeaf {
					probe := reflect.New(t.Elem()).Elem()
					if err := decLeaf(*o.def, probe); err != nil {
						errs = append(errs, fmt.Errorf(
							"ferry: %s: default %q is not a valid %s: %v", pathOrRoot(p), o.defText, t.Elem(), err))
					}
				} else {
					errs = append(errs, defOnCompositeErr(p, t))
					defOK = false
				}
			default:
				// A composite's value lives at MANY addresses and a tag holds
				// ONE text. Expressing {a,b} in it would need a list syntax
				// inside the tag, which is 5.10 - the exact defect ADR-0003
				// removed structurally with Index segments.
				errs = append(errs, defOnCompositeErr(p, t))
				defOK = false
			}
			if reqOK && defOK {
				errs = append(errs, fmt.Errorf(
					"ferry: %s: required and default contradict: a default answers absence and required forbids it", pathOrRoot(p)))
			}
			// omitzero + a default that is not the Go zero value is a
			// round-trip violation, and it is checkable here because the
			// default's text is parsed at compile.
			if defOK && o.omitzero && sh == shapeLeaf {
				probe := reflect.New(t).Elem()
				if err := decLeaf(*o.def, probe); err == nil && !probe.IsZero() {
					errs = append(errs, fmt.Errorf(
						"ferry: %s: omitzero and default=%s contradict: an explicit zero would be omitted and would load back as %s",
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
			for i := range t.NumField() {
				f := t.Field(i)
				if !f.IsExported() {
					continue
				}
				n, fo, err := fieldTag(f)
				if err != nil {
					errs = append(errs, fmt.Errorf("ferry: %s: %v", pathOrRoot(p.Name(n)), err))
				}
				rec(f.Type, p.Name(n), fo)
			}
			if len(s.addrs) == before {
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
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return s, nil
}

func defOnCompositeErr(p Path, t reflect.Type) error {
	return fmt.Errorf(
		"ferry: %s: %s is a composite, so it has no single address a default could sit at; seed the value instead",
		pathOrRoot(p), t)
}

// requiredAdmissible: `required` asserts the plane supplied this address, and
// that assertion has a plane-independent meaning exactly where the address's
// children are STATIC. That is ADR-0003's static tier, reused rather than a
// new distinction:
//
//	leaf          the address itself is static
//	struct        one Name per exported field, from the type
//	[N]T          exactly N Index segments, from the type
//	*T            follows T
//
//	[]T, map[K]V  children come from the VALUE, so "supplied" would mean
//	              "at least one element", which is a length constraint on the
//	              value and ADR-0001 rules those out.
func requiredAdmissible(t reflect.Type) bool {
	switch classify(t) {
	case shapeLeaf, shapeStruct:
		return true
	case shapePointer:
		return requiredAdmissible(t.Elem())
	case shapeSlice:
		return t.Kind() == reflect.Array
	}
	return false
}

// requiredOnCompositeErr explains the refusal in terms of the address, because
// that is what makes it a rule rather than a restriction: `required` asserts
// the plane has an address, and this address is not always there to have.
func requiredOnCompositeErr(p Path, t reflect.Type, sh shape) error {
	_ = sh
	return fmt.Errorf(
		"ferry: %s: required is not available on %s: a plane cannot report \"present and empty\" at a container address, so required could only mean \"at least one element\", which is a constraint on the value; model the distinction as a struct with a set flag, or check len() after Load",
		pathOrRoot(p), t)
}
