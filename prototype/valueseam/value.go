// Package valueseam prototypes the 02b Value layout (L1) and the
// session-02 spelling seam, so every claim on the boards is asserted
// by a test rather than believed.
package valueseam

import (
	"fmt"
	"strconv"
)

// VKind names the six things a plane observation can be.
type VKind uint8

const (
	KindAbsent VKind = iota
	KindNull
	KindBool
	KindNumber
	KindString
	KindBytes
)

func (k VKind) String() string {
	switch k {
	case KindAbsent:
		return "absent"
	case KindNull:
		return "null"
	case KindBool:
		return "bool"
	case KindNumber:
		return "number"
	case KindString:
		return "string"
	case KindBytes:
		return "bytes"
	}
	return fmt.Sprintf("vkind(%d)", uint8(k))
}

// Value is the L1 layout: discriminant + largest member, with the
// bool payload riding in what was padding. 24 bytes, comparable.
type Value struct {
	kind VKind  // 1 byte
	b    bool   // 1 byte, in padding
	s    string // String and Number carry text; Bytes an immutable copy
}

// The compile assertion from the shipped code, kept: Value stays
// comparable, so this stops compiling if a slice or map field lands.
var _ map[Value]struct{}

func Null() Value              { return Value{kind: KindNull} }
func Bool(b bool) Value        { return Value{kind: KindBool, b: b} }
func Number(text string) Value { return Value{kind: KindNumber, s: text} }
func String(s string) Value    { return Value{kind: KindString, s: s} }

// Bytes copies b into the immutable payload: comparability and
// non-aliasing are what the copy buys.
func Bytes(b []byte) Value { return Value{kind: KindBytes, s: string(b)} }

func (v Value) Kind() VKind { return v.kind }

func (v Value) wrongKind(want VKind) error {
	return fmt.Errorf("value holds %s, not %s", v.kind, want)
}

// AsBool answers from the payload or refuses. Guessing is unwritable:
// no constructor pairs KindBool with text.
func (v Value) AsBool() (bool, error) {
	if v.kind != KindBool {
		return false, v.wrongKind(KindBool)
	}
	return v.b, nil
}

// AsInt parses the canonical decimal text Number carries.
func (v Value) AsInt() (int64, error) {
	if v.kind != KindNumber {
		return 0, v.wrongKind(KindNumber)
	}
	return strconv.ParseInt(v.s, 10, 64)
}

func (v Value) AsNumber() (string, error) {
	if v.kind != KindNumber {
		return "", v.wrongKind(KindNumber)
	}
	return v.s, nil
}

func (v Value) AsString() (string, error) {
	if v.kind != KindString {
		return "", v.wrongKind(KindString)
	}
	return v.s, nil
}

// AsBytes copies out; the caller owns the slice.
func (v Value) AsBytes() ([]byte, error) {
	if v.kind != KindBytes {
		return nil, v.wrongKind(KindBytes)
	}
	return []byte(v.s), nil
}
