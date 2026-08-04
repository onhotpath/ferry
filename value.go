package ferry

import (
	"errors"
	"fmt"
	"strconv"
)

// VKind is what a plane observed at an address, and it decides which accessor
// on a [Value] can answer.
//
// The set is closed at six: [KindAbsent], [KindNull], [KindBool], [KindNumber],
// [KindString] and [KindBytes]. There is no kind for a whole composite, because
// a composite is read one element at a time, and none for a driver-native value,
// because no other driver could interpret one.
type VKind uint8

const (
	// KindAbsent means the plane does not have this address at all.
	//
	// It is kind zero, so the zero [Value] is absence and a map[Path]Value
	// lookup miss reports it without being asked. Absence is what a driver
	// returns for an address its plane is silent about, and it is never handed
	// to a [Writer].
	KindAbsent VKind = iota

	// KindNull means the plane has this address and holds its own null there.
	//
	// It is a different observation from absence, and only a plane whose type
	// system contains a null can produce it. YAML and JSON can; TOML,
	// environment variables and query parameters cannot, and a driver over those
	// reports absence or a string and never a null.
	KindNull

	// KindBool is a plane-side boolean. Its text is always "true" or "false",
	// because [Bool] is the only way to build one, which is why [Value.AsBool]
	// has no parse failure to report.
	KindBool

	// KindNumber is a plane-side number, carried as the text the plane spelled
	// it with rather than as a machine number, so no width is chosen before a
	// target type is in hand. [Value.AsInt], [Value.AsUint] and [Value.AsFloat]
	// parse it and report a failure rather than guess.
	KindNumber

	// KindString is a plane-side string, and it stays distinct from a number
	// over the same text. That is how quoting survives the boundary: `port:
	// 8080` arrives as Number("8080"), `port: "8080"` as String("8080"), and
	// each round-trips back to its own spelling.
	KindString

	// KindBytes is an opaque byte sequence, valid UTF-8 or not. How a plane
	// spells bytes - base64, hex, raw - is the driver's business.
	KindBytes
)

// vkindName is the closed kind set made mechanical: one entry per kind, in kind
// order, and the assertion below stops the package compiling if a kind is added
// without a name.
var vkindName = [...]string{"absent", "null", "bool", "number", "string", "bytes"}

// _ ties the name table to the kind set: the two array types are identical only
// while vkindName has exactly one entry per kind.
var _ [len(vkindName)]struct{} = [int(KindBytes) + 1]struct{}{}

// String names the kind in lower case: absent, null, bool, number, string,
// bytes. An out-of-range kind renders as VKind(n) rather than panicking or
// reporting a neighbouring name.
func (k VKind) String() string {
	if int(k) < len(vkindName) {
		return vkindName[k]
	}

	return "VKind(" + strconv.Itoa(int(k)) + ")"
}

// Value is what crosses the boundary between ferry and a plane, in both
// directions: a [VKind], the text that kind was spelled with, and nothing else.
//
// Build one with [Null], [Bool], [Number], [String] or [Bytes], read it with
// [Value.Kind] and the As accessors. The zero Value is [KindAbsent], so a driver
// with nothing to report returns ferry.Value{}.
//
// A Value is comparable, so it is usable as a map key and assertable with ==,
// and it boxes nothing.
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

// Null returns a null value: the plane has this address and holds its own null
// there. A driver over a plane whose grammar has no null never calls it.
func Null() Value { return Value{kind: KindNull} }

// Bool returns a boolean value. Its text is the canonical "true" or "false",
// which is what lets [Value.AsBool] answer without a parse that could fail.
func Bool(b bool) Value { return Value{kind: KindBool, text: strconv.FormatBool(b)} }

// Number returns a numeric value carrying the plane's own spelling of it.
//
// The text is not validated here. A plane is entitled to spell a number in a
// way no Go type wants, and the accessor that has a target type in hand is what
// finds out, so "007", "1e400" and "18446744073709551615" all survive the
// boundary intact and fail, if they fail at all, where the failure can be
// described.
func Number(text string) Value { return Value{kind: KindNumber, text: text} }

// String returns a string value. It stays distinct from a [Number] over the
// same text, which is how quoting survives a round trip.
func String(s string) Value { return Value{kind: KindString, text: s} }

// Bytes returns a byte-sequence value holding b exactly, valid UTF-8 or not.
//
// The bytes are copied, so the caller may reuse its buffer afterwards and no
// later mutation can reach a Value already handed out.
func Bytes(b []byte) Value { return Value{kind: KindBytes, text: string(b)} }

// Kind reports what the plane observed. It is the one accessor that cannot
// fail, and the one a driver or a codec switches on before choosing another.
func (v Value) Kind() VKind { return v.kind }

// ErrWrongKind reports an accessor asked to answer for a kind that has no
// answer: an absent value holds no string, and a number is not a bool. Every
// accessor on [Value] returns it rather than panicking, and it is matched with
// errors.Is.
//
// It is subordinate to [ErrValue] rather than a class of its own, so a refusal
// reaching a caller through core answers errors.Is(err, ferry.ErrValue) too and
// a caller reading the error set by class never has to know it exists. What it
// is for is the finer question a codec asks: "I called the wrong accessor",
// rather than "the plane's value did not fit".
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

// AsString returns the text of a string value, and [ErrWrongKind] for every
// other kind.
//
// The refusal worth knowing about is a number: accepting one would override the
// plane's own type information and destroy the quoting distinction the boundary
// preserves. It refuses a null too, so a codec that has to accept one is a
// [ValueCodec] rather than a [StringCodec].
func (v Value) AsString() (string, error) {
	if err := v.require(KindString); err != nil {
		return "", err
	}

	return v.text, nil
}

// AsNumber returns the plane's own spelling of a numeric value, unparsed, and
// [ErrWrongKind] for every other kind.
//
// It is the accessor a codec for a type wider than any Go machine number uses -
// big.Int is the worked example - because it hands back the digits without
// deciding how wide they are.
func (v Value) AsNumber() (string, error) {
	if err := v.require(KindNumber); err != nil {
		return "", err
	}

	return v.text, nil
}

// AsBool returns the boolean a [KindBool] value carries, and [ErrWrongKind] for
// every other kind. It has no parse failure, because [Bool] is the only way to
// build one and it writes the canonical spelling.
func (v Value) AsBool() (bool, error) {
	if err := v.require(KindBool); err != nil {
		return false, err
	}

	return v.text == "true", nil
}

// AsBytes returns the bytes a [KindBytes] value carries, exactly, valid UTF-8
// or not, and [ErrWrongKind] for every other kind.
//
// The returned slice is a copy, so writing to it cannot reach the [Value] or
// any other holder of an equal one.
func (v Value) AsBytes() ([]byte, error) {
	if err := v.require(KindBytes); err != nil {
		return nil, err
	}

	return []byte(v.text), nil
}

// AsInt parses the text of a numeric value as a signed 64-bit integer.
//
// Out of range is an error and never a saturation, and on any error the int64
// returned is zero rather than strconv's saturated bound, so a caller who
// ignores the error gets an obviously wrong value rather than a plausible one.
// The error wraps strconv.ErrRange or strconv.ErrSyntax, and a non-numeric kind
// gives [ErrWrongKind].
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

// AsUint parses the text of a numeric value as an unsigned 64-bit integer. It
// is the accessor that makes 18446744073709551615 representable at all, and it
// saturates no more than [Value.AsInt] does.
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

// AsFloat parses the text of a numeric value as a 64-bit float.
//
// A float is where precision is lost if it is lost anywhere, and that is why
// the plane's own text is what crossed the boundary: a caller who cannot afford
// the conversion calls [Value.AsNumber] and keeps the spelling.
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

// GoString renders a Value for a diff or a test failure: absent, null,
// bool("true"), number("8080"), string(""), bytes("\xff").
//
// It prints the plane's own text, so it must never be interpolated into an
// error message: ferry cannot know which addresses hold secrets, and neither
// can a caller writing a log line.
func (v Value) GoString() string {
	if v.kind == KindAbsent || v.kind == KindNull {
		return v.kind.String()
	}

	return v.kind.String() + "(" + strconv.Quote(v.text) + ")"
}
