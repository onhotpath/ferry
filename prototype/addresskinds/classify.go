package addresskinds

// The compile-side classification, over reflect: enough of ferry's
// schema walk to prove arrays-are-sections and the refusals.

import (
	"fmt"
	"reflect"
)

// MemberKind is what the compiler decides per schema member.
type MemberKind uint8

const (
	MemberLeaf MemberKind = iota
	MemberSection
	MemberComposite
)

func (k MemberKind) String() string {
	switch k {
	case MemberLeaf:
		return "leaf"
	case MemberSection:
		return "section"
	case MemberComposite:
		return "composite"
	}
	return "?"
}

// Classify maps a Go type to its address kind, with the refusals the
// board claims: a zero-length array refuses like struct{} (it maps no
// address), and an array is a SECTION — its children are compiled,
// not minted.
func Classify(t reflect.Type) (MemberKind, error) {
	switch t.Kind() {
	case reflect.Struct:
		if t.NumField() == 0 {
			return 0, fmt.Errorf("%s maps no address", t)
		}
		return MemberSection, nil
	case reflect.Array:
		if t.Len() == 0 {
			return 0, fmt.Errorf("%s maps no address: a zero-length array has no elements", t)
		}
		if _, err := Classify(t.Elem()); err != nil {
			return 0, fmt.Errorf("%s: %w", t, err) // element type checked, unlike today (#260)
		}
		return MemberSection, nil
	case reflect.Slice, reflect.Map:
		if _, err := Classify(t.Elem()); err != nil {
			return 0, fmt.Errorf("%s: %w", t, err)
		}
		return MemberComposite, nil
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return 0, fmt.Errorf("%s is permanently refused", t)
	case reflect.Pointer:
		return Classify(t.Elem())
	default:
		return MemberLeaf, nil
	}
}

// SectionChildren compiles an array's or struct's children — the
// static set that makes it a section. For arrays the children are
// index segments, known from the type alone.
func SectionChildren(t reflect.Type) ([]Segment, error) {
	switch t.Kind() {
	case reflect.Array:
		segs := make([]Segment, t.Len())
		for i := range segs {
			segs[i] = Index(i)
		}
		return segs, nil
	case reflect.Struct:
		segs := make([]Segment, 0, t.NumField())
		for i := 0; i < t.NumField(); i++ {
			segs = append(segs, Name(t.Field(i).Name))
		}
		return segs, nil
	}
	return nil, fmt.Errorf("%s is not a section", t)
}
