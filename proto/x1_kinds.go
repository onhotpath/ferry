package main

// #41 D2: ADR-0005's admitted kind list, as ONE authority.
//
// The audit found `uint8` admitted by typeset.go's kindClassify and refused by
// e_schema.go's kindLeaf, so `struct{ V uint8 }` did not compile while
// `struct{ V uint16 }` did. That is ADR-0010's duplication axis 1 - a rule
// computed twice - occurring inside the type set, and ADR-0010's own answer is
// that a rule computed twice is how a design drifts.
//
// So the list stops being a `case` clause in two files and becomes data in
// one. Both kindClassify and kindLeaf now read it, and the completeness check
// in x1_complete.go iterates it, which is the mechanism ADR-0005 decided on
// and which would have caught the omission.

import "reflect"

// admittedKinds is ADR-0005's kind table, in the ADR's own order:
//
//	bool, string,
//	int, int8, int16, int32, int64,
//	uint, uint8, uint16, uint32, uint64,
//	float32, float64
//
// []byte and [N]byte are admitted too, but not by kind alone: they are the
// Slice and Array kinds qualified by an element kind, so they are the one
// arm below that cannot be a bare membership test.
var admittedKinds = []reflect.Kind{
	reflect.Bool,
	reflect.String,
	reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
	reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
	reflect.Float32, reflect.Float64,
}

var admittedKind = func() map[reflect.Kind]bool {
	m := make(map[reflect.Kind]bool, len(admittedKinds))
	for _, k := range admittedKinds {
		m[k] = true
	}
	return m
}()

// kindAdmitsLeaf is the single authority. It answers ADR-0005's question and
// only that one: does reflect.Kind alone admit t as a leaf? Identity and the
// chain are asked before it, by resolveLeaf and by classify, and neither is
// this function's business.
func kindAdmitsLeaf(t reflect.Type) bool {
	if admittedKind[t.Kind()] {
		return true
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		// []byte and [N]byte are Bytes, never an indexed composite.
		return t.Elem().Kind() == reflect.Uint8
	}
	return false
}
