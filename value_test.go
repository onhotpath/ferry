package ferry

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
	"unsafe"
)

// kindSample is one column of the accessor matrix: a kind, and a Value of it.
type kindSample struct {
	kind VKind
	v    Value
}

// kindSamples returns one Value per kind, in kind order.
//
// TestAccessorMatrix asserts the length against the kind set, so a seventh kind
// arrives as a failing test here rather than as a row nobody wrote.
func kindSamples() []kindSample {
	return []kindSample{
		{KindAbsent, Value{}},
		{KindNull, Null},
		{KindBool, Bool(true)},
		{KindNumber, Number("8080")},
		{KindString, String("8080")},
		{KindBytes, Bytes([]byte("hi"))},
	}
}

// accessorCase is one row of the accessor matrix: an accessor, the single kind
// it answers for, and what it hands back alongside a refusal.
type accessorCase struct {
	name string
	kind VKind
	call func(Value) (any, error)
	zero any
}

// valueAccessors returns every accessor that returns (T, error).
//
// Kind is not here and cannot be: it answers for every kind and returns no
// error, which is what makes it the accessor a caller reaches for first.
func valueAccessors() []accessorCase {
	return []accessorCase{
		{"AsString", KindString, func(v Value) (any, error) { s, err := v.AsString(); return s, err }, ""},
		{"AsNumber", KindNumber, func(v Value) (any, error) { s, err := v.AsNumber(); return s, err }, ""},
		{"AsBool", KindBool, func(v Value) (any, error) { b, err := v.AsBool(); return b, err }, false},
		{"AsBytes", KindBytes, func(v Value) (any, error) { b, err := v.AsBytes(); return b, err }, []byte(nil)},
		{"AsInt", KindNumber, func(v Value) (any, error) { n, err := v.AsInt(); return n, err }, int64(0)},
		{"AsUint", KindNumber, func(v Value) (any, error) { n, err := v.AsUint(); return n, err }, uint64(0)},
		{"AsFloat", KindNumber, func(v Value) (any, error) { f, err := v.AsFloat(); return f, err }, float64(0)},
	}
}

// TestAccessorMatrix runs every kind against every accessor, which is the
// exhaustive form ADR-0004's "accessors return errors and never panic" needs:
// the property is about the cells nobody thinks about, Absent.AsString() and
// Absent.AsInt() among them.
func TestAccessorMatrix(t *testing.T) {
	ks := kindSamples()
	if len(ks) != int(KindBytes)+1 {
		t.Fatalf("the matrix covers %d kinds and the kind set has %d", len(ks), int(KindBytes)+1)
	}

	for _, k := range ks {
		for _, a := range valueAccessors() {
			t.Run(k.kind.String()+"/"+a.name, func(t *testing.T) {
				checkAccessorCell(t, k, a)
			})
		}
	}
}

// checkAccessorCell asserts one cell: the accessor answers for its own kind, and for
// every other kind refuses with ErrWrongKind and the zero T.
func checkAccessorCell(t *testing.T, k kindSample, a accessorCase) {
	t.Helper()

	got, err := callAccessor(t, k.v, a)
	if k.kind == a.kind {
		if err != nil {
			t.Fatalf("%s on %s: unexpected error %v", a.name, k.kind, err)
		}

		return
	}

	if !errors.Is(err, ErrWrongKind) {
		t.Fatalf("%s on %s: error %v, want one matching ErrWrongKind", a.name, k.kind, err)
	}

	if !reflect.DeepEqual(got, a.zero) {
		t.Fatalf("%s on %s: returned %#v with the error, want the zero %#v", a.name, k.kind, got, a.zero)
	}
}

// callAccessor turns "never panics" into an assertion rather than a crashed test
// binary, so a panicking cell names itself.
func callAccessor(t *testing.T, v Value, a accessorCase) (any, error) {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s panicked on %s: %v", a.name, v.Kind(), r)
		}
	}()

	return a.call(v)
}

// TestZeroValueIsAbsent asserts kind zero, which is what makes a map miss
// absence itself rather than something a recording sink has to reconcile.
func TestZeroValueIsAbsent(t *testing.T) {
	if KindAbsent != 0 {
		t.Fatalf("KindAbsent is %d, want kind zero", KindAbsent)
	}

	var v Value
	if v.Kind() != KindAbsent {
		t.Fatalf("the zero Value has kind %s, want absent", v.Kind())
	}

	if (Value{}) != v {
		t.Fatal("Value{} and the zero Value differ")
	}

	// A lookup miss is the observation, with no parallel presence map.
	miss := map[string]Value{"/host": String("h")}["/nope"]
	if miss.Kind() != KindAbsent {
		t.Fatalf("a map miss has kind %s, want absent", miss.Kind())
	}

	// And absence stays distinct from every present observation, empty ones
	// included: ADR-0004's "absent is not null is not the empty string".
	for _, present := range []Value{Null, String(""), Bytes(nil), Number("")} {
		if present == (Value{}) {
			t.Fatalf("%#v compares equal to Absent", present)
		}
	}
}

// TestComparableAsMapKey exercises the property the round-trip harness and the
// conformance suite both rest on.
func TestComparableAsMapKey(t *testing.T) {
	// A recording sink is this map, so the assertion is the sink's: four
	// observations that differ only in kind or only in text stay four entries.
	entries := []kindSample{
		{KindNumber, Number("8080")},
		{KindString, String("8080")},
		{KindNull, Null},
		{KindAbsent, Value{}},
	}

	m := make(map[Value]VKind, len(entries))
	for _, e := range entries {
		m[e.v] = e.kind
	}

	if len(m) != len(entries) {
		t.Fatalf("%d distinct values collapsed to %d map keys", len(entries), len(m))
	}

	for _, e := range entries {
		if got := m[e.v]; got != e.kind {
			t.Fatalf("%#v looked up as %s, want %s", e.v, got, e.kind)
		}
	}

	same, again := Number("8080"), Number("8080")
	if same != again {
		t.Fatal("two equal values compare unequal")
	}

	quoted := String("8080")
	if same == quoted {
		t.Fatal("a number and a string over the same text compare equal")
	}
}

// TestValueIsTwoWords asserts the 24 bytes ADR-0004 claims.
//
// unsafe is imported here and nowhere in core: the claim is about the memory
// layout, unsafe.Sizeof is the direct way to read it, and a test is where an
// import that core must not have belongs.
func TestValueIsTwoWords(t *testing.T) {
	const want = 24

	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skipf("24 bytes is the 64-bit layout; this word is %d bytes", unsafe.Sizeof(uintptr(0)))
	}

	if got := unsafe.Sizeof(Value{}); got != want {
		t.Fatalf("Value is %d bytes, want %d", got, want)
	}
}

// TestKindSetIsClosedAtSix asserts the enum's boundary, and that a kind outside
// it renders as itself rather than borrowing a neighbour's name.
func TestKindSetIsClosedAtSix(t *testing.T) {
	want := []struct {
		kind VKind
		name string
	}{
		{KindAbsent, "absent"},
		{KindNull, "null"},
		{KindBool, "bool"},
		{KindNumber, "number"},
		{KindString, "string"},
		{KindBytes, "bytes"},
	}

	if len(want) != int(KindBytes)+1 {
		t.Fatalf("the kind set has %d members and this table names %d", int(KindBytes)+1, len(want))
	}

	for i, w := range want {
		if int(w.kind) != i {
			t.Fatalf("%s is kind %d, want %d", w.name, w.kind, i)
		}

		if got := w.kind.String(); got != w.name {
			t.Fatalf("kind %d renders as %q, want %q", i, got, w.name)
		}
	}

	if got := VKind(len(want)).String(); got != "VKind(6)" {
		t.Fatalf("a kind past the end renders as %q, want %q", got, "VKind(6)")
	}
}

// TestPresenceRendersItself is [TestKindSetIsClosedAtSix] for the container side
// of the boundary.
//
// The rendering is what a driver author reads in a failing assertion, and a
// presence outside the set has to render as itself rather than borrow a
// neighbour's name.
func TestPresenceRendersItself(t *testing.T) {
	t.Parallel()

	want := []struct {
		presence Presence
		name     string
	}{
		{PresenceAbsent, "absent"},
		{PresencePresent, "present"},
		{PresenceNull, "null"},
	}

	if len(want) != len(presenceName) {
		t.Fatalf("the presence set has %d members and this table names %d", len(presenceName), len(want))
	}

	for i, w := range want {
		if int(w.presence) != i {
			t.Fatalf("%s is presence %d, want %d", w.name, w.presence, i)
		}

		if got := w.presence.String(); got != w.name {
			t.Errorf("presence %d renders as %q, want %q", i, got, w.name)
		}
	}

	if got, name := Presence(len(presenceName)).String(), "Presence("+strconv.Itoa(len(presenceName))+")"; got != name {
		t.Errorf("a presence past the end renders as %q, want %q", got, name)
	}
}

// TestSectionInfoRendersItself is the same for what a probe answers, which is
// what a driver's own test prints with %#v when it fails.
func TestSectionInfoRendersItself(t *testing.T) {
	t.Parallel()

	cases := []struct {
		info SectionInfo
		want string
	}{
		{SectionAbsent, "absent"},
		{SectionPresent, "present"},
		{SectionNull, "null"},
		{SectionInfo{}, "absent"},
	}

	for _, c := range cases {
		if got := c.info.GoString(); got != c.want {
			t.Errorf("the probe answer renders as %q, want %q", got, c.want)
		}
	}
}

// TestQuotingSurvives is the property a stringly-typed boundary destroys:
// `port: 8080` and `port: "8080"` are two observations, and each round-trips to
// its own spelling.
func TestQuotingSurvives(t *testing.T) {
	unquoted, quoted := Number("8080"), String("8080")

	if unquoted == quoted {
		t.Fatal("the quoted and unquoted spellings compare equal")
	}

	n, err := unquoted.AsNumber()
	if err != nil || n != "8080" {
		t.Fatalf("Number(\"8080\").AsNumber() = %q, %v", n, err)
	}

	s, err := quoted.AsString()
	if err != nil || s != "8080" {
		t.Fatalf("String(\"8080\").AsString() = %q, %v", s, err)
	}

	// Neither answers as the other, which is what keeps them two observations
	// rather than one with a hint attached.
	if _, err := unquoted.AsString(); !errors.Is(err, ErrWrongKind) {
		t.Fatalf("Number(\"8080\").AsString() error is %v, want one matching ErrWrongKind", err)
	}

	if _, err := quoted.AsNumber(); !errors.Is(err, ErrWrongKind) {
		t.Fatalf("String(\"8080\").AsNumber() error is %v, want one matching ErrWrongKind", err)
	}
}

// TestSourceTextSurvives asserts that constructing and reading back returns the
// exact source text, over spellings a machine number would round off, reorder
// or normalise away.
func TestSourceTextSurvives(t *testing.T) {
	texts := []string{
		"", "0", "8080", "007", "+8080", "-0",
		"3.50", "1e400", "0.1", "1.0000000000000000001",
		"18446744073709551615", "-9223372036854775809",
		"0x10", " 8 ", "NaN", "é", "é", "a\x00b",
	}

	for _, text := range texts {
		t.Run(strconv.Quote(text), func(t *testing.T) {
			checkTextSurvives(t, text)
		})
	}
}

func checkTextSurvives(t *testing.T, text string) {
	t.Helper()

	num, str := Number(text), String(text)

	got, err := num.AsNumber()
	if err != nil || got != text {
		t.Fatalf("Number(%q).AsNumber() = %q, %v", text, got, err)
	}

	got, err = str.AsString()
	if err != nil || got != text {
		t.Fatalf("String(%q).AsString() = %q, %v", text, got, err)
	}

	if num == str {
		t.Fatalf("Number(%q) and String(%q) compare equal", text, text)
	}
}

// TestBytesAreCarriedUnmodified covers the reason bytes live in the text field:
// a Go string is an immutable byte sequence and nothing requires it to be
// UTF-8.
func TestBytesAreCarriedUnmodified(t *testing.T) {
	raw := []byte{0xff, 0xfe, 0x00, 'a', 0x80, 0xc3}
	if utf8.Valid(raw) {
		t.Fatal("the case does not test what it claims: these bytes are valid UTF-8")
	}

	v := Bytes(raw)

	got, err := v.AsBytes()
	if err != nil {
		t.Fatalf("AsBytes on a Bytes value: %v", err)
	}

	if !bytes.Equal(got, raw) {
		t.Fatalf("AsBytes returned % x, want % x", got, raw)
	}

	// Neither the caller's buffer nor the returned slice is the Value.
	raw[0], got[1] = 0x01, 0x02

	again, err := v.AsBytes()
	if err != nil {
		t.Fatalf("AsBytes on a Bytes value: %v", err)
	}

	if !bytes.Equal(again, []byte{0xff, 0xfe, 0x00, 'a', 0x80, 0xc3}) {
		t.Fatalf("mutation reached the Value: % x", again)
	}

	// An empty Bytes is a present observation and stays one.
	empty, err := Bytes(nil).AsBytes()
	if err != nil || len(empty) != 0 {
		t.Fatalf("Bytes(nil).AsBytes() = % x, %v", empty, err)
	}
}

// numCase is one parse of a Number's text into a machine type.
type numCase struct {
	name    string
	text    string
	call    func(Value) (any, error)
	want    any
	wantErr error
}

// TestNumberParsing covers the accessors that parse, and the rule that an
// out-of-range text is an error and never a saturation.
func TestNumberParsing(t *testing.T) {
	asInt := func(v Value) (any, error) { n, err := v.AsInt(); return n, err }
	asUint := func(v Value) (any, error) { n, err := v.AsUint(); return n, err }
	asFloat := func(v Value) (any, error) { f, err := v.AsFloat(); return f, err }

	for _, c := range []numCase{
		{"int", "8080", asInt, int64(8080), nil},
		{"int negative", "-1", asInt, int64(-1), nil},
		{"int above the range", "18446744073709551615", asInt, int64(0), strconv.ErrRange},
		{"int below the range", "-9223372036854775809", asInt, int64(0), strconv.ErrRange},
		{"int from a float", "3.5", asInt, int64(0), strconv.ErrSyntax},
		{"int from words", "abc", asInt, int64(0), strconv.ErrSyntax},
		{"uint at the top", "18446744073709551615", asUint, uint64(18446744073709551615), nil},
		{"uint from a negative", "-1", asUint, uint64(0), strconv.ErrSyntax},
		{"uint above the range", "18446744073709551616", asUint, uint64(0), strconv.ErrRange},
		{"float", "3.5", asFloat, 3.5, nil},
		{"float from an integer", "8080", asFloat, 8080.0, nil},
		{"float above the range", "1e400", asFloat, 0.0, strconv.ErrRange},
		{"float from words", "abc", asFloat, 0.0, strconv.ErrSyntax},
	} {
		t.Run(c.name, func(t *testing.T) {
			checkNumCase(t, c)
		})
	}
}

func checkNumCase(t *testing.T, c numCase) {
	t.Helper()

	got, err := c.call(Number(c.text))
	if !errors.Is(err, c.wantErr) {
		t.Fatalf("parsing %q: error %v, want one matching %v", c.text, err, c.wantErr)
	}

	if !reflect.DeepEqual(got, c.want) {
		t.Fatalf("parsing %q: got %#v, want %#v", c.text, got, c.want)
	}
}

// TestBoolIsCanonical covers the invariant that lets AsBool answer with no
// parse arm: the Bool constructor is the only door into the kind.
func TestBoolIsCanonical(t *testing.T) {
	for _, want := range []bool{true, false} {
		t.Run(strconv.FormatBool(want), func(t *testing.T) {
			got, err := Bool(want).AsBool()
			if err != nil || got != want {
				t.Fatalf("Bool(%t).AsBool() = %t, %v", want, got, err)
			}
		})
	}

	if Bool(true) == Bool(false) {
		t.Fatal("true and false compare equal")
	}
}

// TestErrorsNameStructureAndNotText asserts ADR-0011's total rule at the one
// place in this file that can break it: a parse failure whose cause prints the
// text it failed on.
func TestErrorsNameStructureAndNotText(t *testing.T) {
	const secret = "18446744073709551615"

	_, err := Number(secret).AsInt()
	if !errors.Is(err, strconv.ErrRange) {
		t.Fatalf("error %v does not match strconv.ErrRange", err)
	}

	if strings.Contains(err.Error(), secret) {
		t.Fatalf("the message repeats the plane's text: %q", err.Error())
	}

	if !strings.Contains(err.Error(), "int64") {
		t.Fatalf("the message names no target type: %q", err.Error())
	}

	// A syntax failure takes the other arm, and keeps the same rule.
	_, err = Number("hunter2").AsFloat()
	if !errors.Is(err, strconv.ErrSyntax) {
		t.Fatalf("error %v does not match strconv.ErrSyntax", err)
	}

	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("the message repeats the plane's text: %q", err.Error())
	}

	// A wrong-kind refusal names the two kinds, which is structure, and nothing
	// the plane spelled.
	_, err = String("hunter2").AsInt()
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("the message repeats the plane's text: %q", err.Error())
	}

	if !strings.Contains(err.Error(), "string is not number") {
		t.Fatalf("the refusal does not name both kinds: %q", err.Error())
	}
}

// TestGoString covers the rendering every ADR measurement and every ferrytest
// diff is written in.
func TestGoString(t *testing.T) {
	for _, c := range []struct {
		v    Value
		want string
	}{
		{Value{}, "absent"},
		{Null, "null"},
		{Bool(true), "bool(true)"},
		{Bool(false), "bool(false)"},
		{Number("8080"), `number("8080")`},
		{String(""), `string("")`},
		{String("8080"), `string("8080")`},
		{Bytes([]byte{0xff}), `bytes("\xff")`},
	} {
		t.Run(c.want, func(t *testing.T) {
			checkGoString(t, c.v, c.want)
		})
	}
}

func checkGoString(t *testing.T, v Value, want string) {
	t.Helper()

	if got := v.GoString(); got != want {
		t.Fatalf("GoString() = %q, want %q", got, want)
	}

	// %#v is the point of the method's name: a Value in a diff renders itself.
	if got := fmt.Sprintf("%#v", v); got != want {
		t.Fatalf("%%#v = %q, want %q", got, want)
	}
}
