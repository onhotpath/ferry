package main

// The candidate value that crosses the plane boundary.
//
// Section 4 of the research doc recommends "a closed struct union in the
// slog.Value shape whose scalar leaf holds the source text, with an escape
// arm, a group arm and comma-ok for absence".
//
// This file starts from the *smallest* thing that could work - a kind plus a
// string, no `any` field at all - so that the probes have to force each extra
// arm rather than the arm being assumed. P1 prices jsontext.Token against it,
// P6 goes looking for a case that forces the group arm.

import (
	"errors"
	"strconv"
)

type VKind uint8

const (
	// Absence is a kind, and it is kind zero.
	//
	// The alternative this prototype started from was comma-ok, which is
	// what 5.1 recommends. P11 measures the two against each other. The
	// short version: a bool is a second channel, and Go lets a caller drop
	// a second channel with _ and no diagnostic, whereas a kind travels
	// inside the value and every accessor already refuses it.
	//
	// Kind zero rather than a separate sentinel, because then a
	// map[Path]Value lookup miss *is* absence, with no reconciliation
	// between "not recorded" and "recorded as absent". That is what makes
	// a recording sink able to hold the observation ADR-0001 says a loaded
	// struct erases.
	VAbsent VKind = iota
	VNull
	VBool
	VNumber // text is the source text, never a machine number
	VString
	VBytes // text holds the bytes; string is an immutable byte sequence
)

var vkindName = [...]string{"absent", "null", "bool", "number", "string", "bytes"}

func (k VKind) String() string {
	if int(k) < len(vkindName) {
		return vkindName[k]
	}
	return "VKind(" + strconv.Itoa(int(k)) + ")"
}

// Value is a kind plus source text. Two words, comparable, no `any`.
//
// Comparability is not decoration: it is what lets the conformance suite and
// the round-trip harness use == instead of a bespoke equality function, and
// it is the property slog.Value and protoreflect.Value both deliberately gave
// up (`_ [0]func()`, `pragma.DoNotCompare`) in exchange for unsafe packing
// that section 4 says ferry does not need.
type Value struct {
	kind VKind
	text string
}

// Absent is the zero Value. A driver returns it, by name, for "I looked and
// the plane does not have this address".
var Absent = Value{}

func Null() Value             { return Value{kind: VNull} }
func Bool(b bool) Value       { return Value{kind: VBool, text: strconv.FormatBool(b)} }
func Number(s string) Value   { return Value{kind: VNumber, text: s} }
func Int(i int64) Value       { return Value{kind: VNumber, text: strconv.FormatInt(i, 10)} }
func Uint(u uint64) Value     { return Value{kind: VNumber, text: strconv.FormatUint(u, 10)} }
func String(s string) Value   { return Value{kind: VString, text: s} }
func Bytes(b []byte) Value    { return Value{kind: VBytes, text: string(b)} }
func (v Value) Kind() VKind   { return v.kind }
func (v Value) Present() bool { return v.kind != VAbsent }
func (v Value) Text() string  { return v.text }

var errKind = errors.New("value: wrong kind")

// Accessors return (T, error), never panic. cty, protoreflect and slog all
// panic on kind mismatch and all document it as intentional, because their
// callers type-check first. ferry's callers are third-party driver authors.
func (v Value) AsInt() (int64, error) {
	if v.kind != VNumber {
		return 0, errKind
	}
	return strconv.ParseInt(v.text, 10, 64)
}

func (v Value) AsUint() (uint64, error) {
	if v.kind != VNumber {
		return 0, errKind
	}
	return strconv.ParseUint(v.text, 10, 64)
}

func (v Value) AsFloat() (float64, error) {
	if v.kind != VNumber {
		return 0, errKind
	}
	return strconv.ParseFloat(v.text, 64)
}

func (v Value) AsBool() (bool, error) {
	if v.kind != VBool {
		return false, errKind
	}
	return strconv.ParseBool(v.text)
}

func (v Value) AsString() (string, error) {
	if v.kind != VString {
		return "", errKind
	}
	return v.text, nil
}

func (v Value) AsBytes() ([]byte, error) {
	if v.kind != VBytes {
		return nil, errKind
	}
	return []byte(v.text), nil
}

func (v Value) GoString() string {
	if v.kind == VNull || v.kind == VAbsent {
		return v.kind.String()
	}
	return v.kind.String() + "(" + strconv.Quote(v.text) + ")"
}
