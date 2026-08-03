package ferry

import (
	"errors"
	"fmt"
	"strconv"
)

// VKind is what a plane observed at an address, and it is the only thing that
// decides which accessor on a Value can answer.
//
// The set is closed at six, and closure is a decision rather than an oversight.
// ADR-0004 refused a group arm because ADR-0003's structured addresses read a
// composite one element at a time, so there is no address a group could be at;
// and it refused an escape arm holding a driver-native value because such a
// value is uninterpretable by every other driver, which is exactly what
// plane-to-plane transfer needs. The escape arm is recorded there as the
// weakest call in that ADR.
//
// What a seventh kind would cost: every driver that switches on Kind, every
// codec that declares a kind, and every conformance table that lists the six
// gains a case it did not have, and after v1 that is a breaking change. The
// only mitigation ADR-0004 claims is that ferry is at v0, which is the one
// place semver allows taking a closed enum back. Treat the table below as the
// enum's boundary: adding a name to it is the whole of the change, and the
// assertion under it is what makes forgetting one a compile error.
type VKind uint8

const (
	// KindAbsent means the plane does not have this address, and nothing was
	// found at it.
	//
	// It is kind zero on purpose. The zero Value is therefore KindAbsent, a
	// map[Path]Value lookup miss *is* absence, and a recording sink needs no
	// parallel presence map to tell "not recorded" from "recorded as absent"
	// (ADR-0004).
	KindAbsent VKind = iota

	// KindNull means the plane has this address and the value stored there is
	// that plane's own null.
	//
	// It is a different observation from Absent, and the difference belongs to
	// the plane: only a plane whose type system contains a null can produce it.
	// YAML and JSON can; TOML, environment variables and query parameters
	// cannot, and on those a driver reports Absent or a String and never a Null
	// (ADR-0004).
	KindNull

	// KindBool is a plane-side boolean. Its text is always "true" or "false",
	// because the Bool constructor is the only door into the kind, which is why
	// AsBool has no parse failure to report.
	KindBool

	// KindNumber is a plane-side number, carried as the source text the plane
	// spelled it with and never as a machine number.
	//
	// Every lossless design ADR-0004 examined converged on text, and a native
	// numeric leaf recreates structpb's documented float64 defect. The
	// consequence is that AsInt, AsUint and AsFloat parse, and report a failure
	// rather than guess.
	KindNumber

	// KindString is a plane-side string, and it stays distinct from Number even
	// when the text is identical.
	//
	// That distinction is how quoting survives the boundary, which is the one
	// thing a stringly-typed boundary destroys: `port: 8080` arrives as
	// Number("8080"), `port: "8080"` as String("8080"), and each round-trips
	// back to its own spelling (ADR-0004).
	KindString

	// KindBytes is an opaque byte sequence, carried in the same text field as
	// every other kind, because a Go string is an immutable byte sequence and
	// nothing requires it to be UTF-8.
	//
	// That is what removes the `any` field the survey's sketch had, and it buys
	// three things at once: Value is 24 bytes, it boxes nothing, and it is
	// comparable (ADR-0004).
	KindBytes
)

// vkindName is the closed kind set made mechanical: one entry per kind, in kind
// order, and the assertion below stops the package compiling if a kind is added
// without a name.
var vkindName = [...]string{"absent", "null", "bool", "number", "string", "bytes"}

// _ ties the name table to the kind set: the two array types are identical only
// while vkindName has exactly one entry per kind.
var _ [len(vkindName)]struct{} = [int(KindBytes) + 1]struct{}{}

// String names the kind in the lower-case spelling every ADR measurement and
// every ferrytest diff uses. An out-of-range kind renders as VKind(n) rather
// than panicking or reporting a neighbouring name.
func (k VKind) String() string {
	if int(k) < len(vkindName) {
		return vkindName[k]
	}

	return "VKind(" + strconv.Itoa(int(k)) + ")"
}

// Value is what crosses the boundary between ferry and a plane, in both
// directions: a kind, the source text that kind was spelled with, and nothing
// else.
//
// The zero Value is KindAbsent, so a driver with nothing to report returns
// ferry.Value{} and a map miss returns absence without being asked to.
//
// Comparability is load-bearing rather than incidental. It is what lets the
// round-trip harness and the conformance suite assert with ==, and what makes
// map[Path]Value a usable recording sink. It also forecloses ever adding a
// slice or a map field to Value, which is a constraint on the type rather than
// a property of today's fields. slog.Value and protoreflect.Value both give
// comparability up (`_ [0]func()`, pragma.DoNotCompare) in exchange for unsafe
// packing that ADR-0004 measured ferry as not needing.
type Value struct {
	kind VKind
	text string
}

// _ asserts Value stays comparable. A map key type must be comparable, so this
// declaration stops compiling the day Value grows a slice or a map field.
var _ map[Value]struct{}

// The constructors are one per kind, and they take the plain kind names.
//
// Go has one package namespace, so the six kind constants and the five
// constructors compete for the same six words, and only one side of that can
// keep them. The constants took the Kind prefix, because a constructor is
// written far more often than a kind constant is named and the shorter
// spelling is worth more on the constructor. slog moves both sides at once,
// slog.KindString beside slog.StringValue, and so pays for the collision twice
// where moving one side settles it.
//
// Absent gets no constructor. It is kind zero, the zero Value is it, and a
// function that builds one would suggest absence is a thing to construct rather
// than the thing that is already there.

// Null returns a Null value: the plane has this address and holds its own null
// there. A driver on a plane whose grammar has no null never calls it.
func Null() Value { return Value{kind: KindNull} }

// Bool returns a Bool value. The text is the canonical "true" or "false", which
// is what lets AsBool answer without a parse that could fail.
func Bool(b bool) Value { return Value{kind: KindBool, text: strconv.FormatBool(b)} }

// Number returns a Number value carrying the plane's own spelling of it.
//
// The text is not validated here, on purpose: a plane is entitled to spell a
// number in a way no Go type wants, and the place that finds out is the
// accessor that has a target type in hand. What that buys is that "007",
// "1e400" and "18446744073709551615" all survive the boundary intact and fail,
// if they fail at all, where the failure can be described.
func Number(text string) Value { return Value{kind: KindNumber, text: text} }

// String returns a String value. It stays distinct from a Number over the same
// text, which is the whole of how quoting survives.
func String(s string) Value { return Value{kind: KindString, text: s} }

// Bytes returns a Bytes value holding b exactly, valid UTF-8 or not.
//
// The bytes are copied into the immutable text field, so a caller may reuse
// its buffer afterwards and no later mutation can reach a Value already handed
// out.
func Bytes(b []byte) Value { return Value{kind: KindBytes, text: string(b)} }

// Kind reports what the plane observed. It is the one accessor that cannot
// fail, and the one a driver or a codec switches on before choosing another.
func (v Value) Kind() VKind { return v.kind }

// ErrWrongKind reports an accessor asked to answer for a kind that has no
// answer: an Absent holds no string, and a Number is not a Bool.
//
// Accessors return it rather than panicking, which is where ferry parts company
// with cty, protoreflect and slog. All three panic on kind mismatch and all
// three document it as intentional, because their callers type-check first.
// ferry's callers are third-party driver authors, so ferry does not get to make
// that assumption (ADR-0004). Match it with errors.Is.
//
// Its message names both kinds, which ADR-0011 permits because a kind is
// structure rather than a value the plane supplied. It carries no accessor for
// them, because a caller holds both already: Kind reports the one in hand, and
// the wanted kind is whichever accessor was called, since AsInt wants
// KindNumber by definition. A typed error would return the call site its own
// arguments.
//
// It is not a seventh class. It is subordinate to ErrValue in the same way
// ErrReadOnly is subordinate to ErrPlane: an accessor's refusal that reaches a
// caller through core answers to errors.Is(err, ErrValue) as well as to itself,
// so a caller reading the error set by class never has to know it exists
// (ADR-0011). What it is for is the finer question a codec asks - "I asked the
// wrong accessor" rather than "the plane's value did not fit" - which the class
// alone cannot answer.
var ErrWrongKind = errors.New("value: wrong kind")

// require is the single gate every accessor passes through, so that "wrong kind
// is an error and never a panic" is one branch in one place rather than a rule
// each accessor is trusted to keep.
func (v Value) require(want VKind) error {
	if v.kind == want {
		return nil
	}

	return fmt.Errorf("%w: %s is not %s", ErrWrongKind, v.kind, want)
}

// numBase and numBits name the two arguments every parse below takes. A
// plane spells a number in base 10 and ferry's widest machine types are 64 bits,
// so a number too wide for one of them is a refusal and never a narrowing.
const (
	numBase = 10
	numBits = 64
)

// numError reports a Number whose text does not parse into the machine type
// asked for.
//
// It exists rather than a bare %w wrap of strconv's error because
// strconv.NumError prints the text it failed on, and ADR-0011 makes "ferry's
// own message text never contains a value the plane supplied" a total rule:
// ferry cannot know which addresses a Vault or Consul plane holds secrets at.
// The cause stays in the chain, so errors.Is against strconv.ErrRange and
// strconv.ErrSyntax still answers, and it is never printed.
type numError struct {
	want string // the machine type asked for, which is ferry's word and not the plane's
	err  error  // the strconv cause, kept for errors.Is and never rendered
}

func (e *numError) Error() string {
	if errors.Is(e.err, strconv.ErrRange) {
		return "value: number out of range for " + e.want
	}

	return "value: number is not a valid " + e.want
}

func (e *numError) Unwrap() error { return e.err }

// AsString returns the text of a String value.
//
// It refuses a Number, which is the refusal worth stating: accepting one here
// would be ferry overriding the plane's own type information, and it would
// destroy the quoting distinction the boundary exists to preserve (ADR-0005).
// It refuses Null too, which is what makes a codec built on it unable to
// express ADR-0006's null escape hatch, and is why ADR-0009 keeps a general
// constructor whose decode half sees the whole Value.
func (v Value) AsString() (string, error) {
	if err := v.require(KindString); err != nil {
		return "", err
	}

	return v.text, nil
}

// AsNumber returns the plane's own spelling of a Number, unparsed.
//
// It is what a codec for a type wider than any Go machine number uses -
// ADR-0009's big.Int registration is the worked example - and it is the only
// accessor that hands back a number without deciding how wide it is.
func (v Value) AsNumber() (string, error) {
	if err := v.require(KindNumber); err != nil {
		return "", err
	}

	return v.text, nil
}

// AsBool returns the boolean a Bool carries.
//
// It has no parse arm because it needs none: Bool is the only constructor for
// the kind and it writes the canonical spelling, so the text of a Bool is
// "true" or "false" and never anything else.
func (v Value) AsBool() (bool, error) {
	if err := v.require(KindBool); err != nil {
		return false, err
	}

	return v.text == "true", nil
}

// AsBytes returns the bytes a Bytes value carries, exactly, valid UTF-8 or not.
//
// The returned slice is a copy, so writing to it cannot reach the Value and
// cannot reach any other holder of an equal Value. That copy is the price of
// keeping Value comparable and allocation-free everywhere else.
func (v Value) AsBytes() ([]byte, error) {
	if err := v.require(KindBytes); err != nil {
		return nil, err
	}

	return []byte(v.text), nil
}

// AsInt parses the source text of a Number as a signed 64-bit integer.
//
// Out of range is an error and never a saturation. koanf's Int64() turning
// 18446744073709551615 into MaxInt64 with a nil error is one of the silent
// wrong answers ferry exists in order not to have (ADR-0001). On any error the
// int64 returned is zero rather than strconv's saturated bound, so a caller who
// ignores the error gets an obviously wrong value instead of a plausible one.
func (v Value) AsInt() (int64, error) {
	if err := v.require(KindNumber); err != nil {
		return 0, err
	}

	n, err := strconv.ParseInt(v.text, numBase, numBits)
	if err != nil {
		return 0, &numError{want: "int64", err: err}
	}

	return n, nil
}

// AsUint parses the source text of a Number as an unsigned 64-bit integer.
//
// It is the accessor that makes 18446744073709551615 representable at all, and
// it saturates no more than AsInt does.
func (v Value) AsUint() (uint64, error) {
	if err := v.require(KindNumber); err != nil {
		return 0, err
	}

	n, err := strconv.ParseUint(v.text, numBase, numBits)
	if err != nil {
		return 0, &numError{want: "uint64", err: err}
	}

	return n, nil
}

// AsFloat parses the source text of a Number as a 64-bit float.
//
// A float is where precision is lost if it is lost anywhere, which is the whole
// reason the text is what crossed the boundary: a caller that cannot afford the
// conversion calls AsNumber instead and keeps the plane's own spelling.
func (v Value) AsFloat() (float64, error) {
	if err := v.require(KindNumber); err != nil {
		return 0, err
	}

	f, err := strconv.ParseFloat(v.text, numBits)
	if err != nil {
		return 0, &numError{want: "float64", err: err}
	}

	return f, nil
}

// GoString renders a Value the way every ADR measurement and every ferrytest
// diff spells one: absent, null, bool("true"), number("8080"), string(""),
// bytes("\xff").
//
// It is a debugging rendering and it prints the plane's own text, so it must
// never be interpolated into an error message. ADR-0011 makes that rule total,
// because ferry cannot know which addresses hold secrets.
func (v Value) GoString() string {
	if v.kind == KindAbsent || v.kind == KindNull {
		return v.kind.String()
	}

	return v.kind.String() + "(" + strconv.Quote(v.text) + ")"
}
