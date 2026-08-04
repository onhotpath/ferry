package ferry

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"
)

// leafCodec is what one leaf type does at the boundary, in both directions.
//
// It is resolved once per position per schema and held on the compiled node,
// rather than being re-decided per call from the reflect.Type: a kind switch
// inside the walk is the same decision taken again on every leaf of every load
// (ADR-0010).
type leafCodec struct {
	// kind is the Value kind this leaf writes on Dump, and the one it accepts
	// on Load beside String.
	//
	// ADR-0005: every leaf accepts its own kind, and additionally accepts
	// String, whose text is parsed by exactly the parser that leaf's own kind
	// uses. Nothing else coerces. String is the universal donor because String
	// is what a plane says when it has nothing to say - it means untyped text,
	// parse it yourself - and without it ferry cannot load an integer from an
	// environment variable, which disables three of ADR-0004's four first-party
	// planes for every non-string field.
	kind VKind

	// encode is the Dump half. It reports an error for a value the
	// representation does not cover, which is time.Time outside years 0 to 9999
	// and nothing else in core's set.
	encode func(reflect.Value) (Value, error)

	// parse is the Load half, over text, and it is one function rather than one
	// per accepted kind because the parser is the leaf's own whichever kind
	// carried the text. That is the whole of what separates ferry from cast,
	// which reads "0080" as 0, "010" as 8, "1.9" into an int as 1, "" into an
	// int as 0 and "yes" into a bool as false. Every one of those is a refusal
	// here.
	parse func(reflect.Value, string) error

	// nullable is the leaf that has a null of its own.
	//
	// ADR-0006: Null is presence carrying a value, so it is admitted by exactly
	// the Go types that have a null and refused by every other leaf as a wrong
	// kind - the same refusal a Bool gets at a string field. In core's leaf set
	// that is []byte alone: [N]byte has no nil, and neither has any number, any
	// bool, any string or either identity leaf.
	nullable bool
}

// leafFor resolves a type to its leaf behaviour, by type identity first and by
// reflect.Kind second.
//
// The ordering is the whole rule (ADR-0005). time.Duration's kind is int64 and
// time.Time's kind is struct, so a kind-first resolution writes a nanosecond
// count for one and walks three unexported fields of the other. Consulting
// identity first is what makes both ferry's.
func leafFor(t reflect.Type) (leafCodec, bool) {
	if cd, ok := byIdentity[t]; ok {
		return cd, true
	}

	return leafByKind(t)
}

// pointerLeaf resolves *T where T is a leaf, and declines every other pointer
// to the composite rules.
//
// A pointer to a leaf is not a composite and is not given a container address
// of its own: the leaf already had an address, and what the pointer adds there
// is a null rather than a second place (ADR-0006). So *int is one address that
// carries a number or a null, and it is the one shape in the set that tells an
// explicit zero from an unset field on Dump.
func pointerLeaf(t reflect.Type) (leafCodec, bool) {
	elem := t.Elem()

	inner, ok := leafFor(elem)
	if !ok {
		return leafCodec{}, false
	}

	return leafCodec{
		kind:     inner.kind,
		nullable: true,
		encode: func(v reflect.Value) (Value, error) {
			if v.IsNil() {
				return Null(), nil
			}

			return inner.encode(v.Elem())
		},
		// The pointee is built fresh and only then published, which is what
		// keeps a seed the caller still holds out of the walk's reach: writing
		// through the seed's own pointer would let a partial load mutate a
		// value LoadOver promises never to touch.
		parse: func(v reflect.Value, text string) error {
			fresh := reflect.New(elem)
			if err := inner.parse(fresh.Elem(), text); err != nil {
				return err
			}

			v.Set(fresh)

			return nil
		},
	}, true
}

// byIdentity is the table of types ferry owns the representation of, in both
// directions.
//
// It is keyed by reflect.Type values obtained from reflect.TypeFor, compared
// with ==, and it contains no strings. That is the fix for survey item 5.9, and
// the mechanism matters as much as the rule: xload identifies time.Duration by
// comparing Type.String() to "time.Duration", which matches any type in any
// package called time whose name is Duration, and misses the one type it meant
// the moment somebody wraps it. Measured, reflect.TypeFor[time.Duration]() ==
// reflect.TypeFor[int64]() is false though both kinds are int64.
//
// Membership confers nothing beyond a representation. ADR-0005 declares map-key
// admissibility per entry rather than by table membership, which is the rule
// that keeps time.Time out of a map key position.
var byIdentity = map[reflect.Type]leafCodec{
	reflect.TypeFor[time.Duration](): durationCodec(),
	reflect.TypeFor[time.Time]():     timeCodec(),
}

// leafByKind is ADR-0005's kind table: bool, string, the five signed and five
// unsigned integer widths, both float widths, and []byte and [N]byte as Bytes.
//
// A named type over an admitted kind is admitted with it, which is the whole of
// what kind admission buys: type Port int round-trips as number("8080") with
// nothing registered. It is also what leaves a named type over time.Duration
// dumping nanoseconds, because such a type is a distinct reflect.Type, misses
// the identity table and falls to kind int64. That is a documented sharp edge
// rather than a defect ferry can reflect its way out of - matching on the
// underlying type would capture every ordinary type Port int as well - and its
// remedy is registration.
func leafByKind(t reflect.Type) (leafCodec, bool) {
	switch t.Kind() {
	case reflect.Bool:
		return boolCodec(), true
	case reflect.String:
		return stringCodec(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return signedCodec(t.Bits()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return unsignedCodec(t.Bits()), true
	case reflect.Float32, reflect.Float64:
		return floatCodec(t.Bits()), true
	case reflect.Slice, reflect.Array:
		return bytesCodec(t)
	default:
		return leafCodec{}, false
	}
}

// bytesCodec claims a slice or an array of bytes, and declines every other one
// to the composite rules.
//
// Both identities are forced by Go rather than chosen by ferry. Measured,
// reflect.TypeFor[[]byte]() == reflect.TypeFor[[]uint8]() is true, so ferry
// cannot offer both "a byte blob" and "a slice of small unsigned integers"; it
// has one type and picks Bytes. And reflect.TypeFor[[]rune]() ==
// reflect.TypeFor[[]int32]() is true, so []rune is an indexed composite of
// numbers rather than text, which is legal and is almost certainly not what a
// user meant. Both belong in the documentation rather than being discovered.
func bytesCodec(t reflect.Type) (leafCodec, bool) {
	if t.Elem().Kind() != reflect.Uint8 {
		return leafCodec{}, false
	}

	if t.Kind() == reflect.Slice {
		return byteSliceCodec(), true
	}

	return byteArrayCodec(t.Len()), true
}

// boolCodec is strconv.FormatBool and its inverse, so the kind carries the
// canonical spelling and never 1 or "yes".
//
// ParseBool is the leaf's own parser rather than a wider reading of what a
// human might have meant, which is why enabled: yes from YAML and ENABLED=yes
// from env are both refused here instead of being made to agree on a guess.
func boolCodec() leafCodec {
	return leafCodec{
		kind:   KindBool,
		encode: func(v reflect.Value) (Value, error) { return Bool(v.Bool()), nil },
		parse: func(v reflect.Value, text string) error {
			b, err := strconv.ParseBool(text)
			if err != nil {
				return &parseFailure{typ: v.Type(), err: err}
			}

			v.SetBool(b)

			return nil
		},
	}
}

// stringCodec carries the bytes unmodified, and is not required to be UTF-8: a
// Go string is a byte sequence and a NUL is not a terminator.
//
// It accepts String and nothing else, and the refusal that matters is Number.
// Accepting one would be ferry overriding a plane's own type information and
// would destroy the quoting distinction the boundary exists to preserve, where
// port: 8080 arrives as Number and port: "8080" as String and each round-trips
// back to its own spelling (ADR-0004, ADR-0005).
func stringCodec() leafCodec {
	return leafCodec{
		kind:   KindString,
		encode: func(v reflect.Value) (Value, error) { return String(v.String()), nil },
		parse: func(v reflect.Value, text string) error {
			v.SetString(text)

			return nil
		},
	}
}

// signedCodec is one signed width, in base 10.
//
// The width is the parse's own, not int64's, so an out-of-range value is
// strconv.ErrRange and therefore an error rather than a truncation or a
// saturation. koanf's Int64() turning 18446744073709551615 into MaxInt64 with a
// nil error is one of the silent wrong answers ferry exists in order not to
// have (ADR-0001).
func signedCodec(bits int) leafCodec {
	return leafCodec{
		kind: KindNumber,
		encode: func(v reflect.Value) (Value, error) {
			return Number(strconv.FormatInt(v.Int(), numBase)), nil
		},
		parse: func(v reflect.Value, text string) error {
			n, err := strconv.ParseInt(text, numBase, bits)
			if err != nil {
				return &parseFailure{typ: v.Type(), err: err}
			}

			v.SetInt(n)

			return nil
		},
	}
}

// unsignedCodec is one unsigned width, on the same argument as [signedCodec].
// It is the half that makes 18446744073709551615 representable at all.
func unsignedCodec(bits int) leafCodec {
	return leafCodec{
		kind: KindNumber,
		encode: func(v reflect.Value) (Value, error) {
			return Number(strconv.FormatUint(v.Uint(), numBase)), nil
		},
		parse: func(v reflect.Value, text string) error {
			n, err := strconv.ParseUint(text, numBase, bits)
			if err != nil {
				return &parseFailure{typ: v.Type(), err: err}
			}

			v.SetUint(n)

			return nil
		},
	}
}

// floatFormat and floatShortest are strconv's shortest round-tripping form:
// %g with the precision that reproduces the value exactly.
//
// Every value in both widths survives it, the four specials included, which is
// the tagged-text finding the survey reaches and which encoding/json/v2 still
// agrees with.
const (
	floatFormat   = 'g'
	floatShortest = -1
)

// floatCodec is one float width, formatted and parsed at its own bit size.
//
// The bit size is load-bearing rather than incidental: a float32 formatted at
// 64 bits gives 0.10000000149011612, which re-rounds to the same float32 and is
// a wrong-looking config file. So one third is "0.3333333333333333" at 64 bits
// and "0.33333334" at 32, and the two rows of the golden table are required to
// disagree on it.
func floatCodec(bits int) leafCodec {
	return leafCodec{
		kind: KindNumber,
		encode: func(v reflect.Value) (Value, error) {
			return Number(strconv.FormatFloat(v.Float(), floatFormat, floatShortest, bits)), nil
		},
		parse: func(v reflect.Value, text string) error {
			f, err := strconv.ParseFloat(text, bits)
			if err != nil {
				return &parseFailure{typ: v.Type(), err: err}
			}

			v.SetFloat(f)

			return nil
		},
	}
}

// byteSliceCodec is []byte, and it is the one leaf in core's set with a null.
//
// A nil slice writes Null and an empty one writes bytes(""), which is not the
// composite rule and is not in tension with it: ADR-0005 makes a composite with
// no elements write Null whether it is nil or empty, and []byte is admitted
// here as a leaf rather than as a composite, so that rule does not reach it.
// The golden table reported the difference the first time it ran, and a reader
// following the prose alone gets it wrong.
//
// Base64 is not ferry's business. Bytes carries the bytes and how a plane
// spells them is the driver's.
func byteSliceCodec() leafCodec {
	return leafCodec{
		kind:     KindBytes,
		nullable: true,
		encode: func(v reflect.Value) (Value, error) {
			if v.IsNil() {
				return Null(), nil
			}

			return Bytes(v.Bytes()), nil
		},
		parse: func(v reflect.Value, text string) error {
			v.SetBytes([]byte(text))

			return nil
		},
	}
}

// byteArrayCodec is [N]byte, which agrees with encoding/json/v2: measured, v2
// marshals [3]byte{1,2,3} as "AQID" while v1 marshals it as [1,2,3], and v1's
// behaviour survives only through the legacy FormatByteArrayAsArray option.
//
// The length is part of the type, so a plane holding a different number of
// bytes is loud rather than padded or truncated. The message names both
// lengths, which ADR-0011 permits because a length is structure rather than a
// value the plane supplied.
func byteArrayCodec(n int) leafCodec {
	return leafCodec{
		kind: KindBytes,
		encode: func(v reflect.Value) (Value, error) {
			b := make([]byte, n)
			reflect.Copy(reflect.ValueOf(b), v)

			return Bytes(b), nil
		},
		parse: func(v reflect.Value, text string) error {
			if len(text) != n {
				return &lengthFailure{typ: v.Type(), got: len(text), want: n}
			}

			reflect.Copy(v, reflect.ValueOf([]byte(text)))

			return nil
		},
	}
}

// durationCodec is time.Duration as 30s, and ferry departs from
// encoding/json/v2 here deliberately.
//
// Measured on go1.27rc2, v2 refuses a duration outright with "no default
// representation" and its v1-legacy FormatDurationAsNano gives 1000000000. So
// the two answers available upstream are "refuse" and "nanoseconds", and ferry
// takes neither: for a mapper whose commonest application is configuration,
// TIMEOUT=30s is what people write, and a plane holding 1000000000 is a worse
// artefact by every measure except symmetry with a JSON library ferry is not.
// ferry does not claim to be following v2 here.
//
// The kind is int64, so both halves reach the value through reflect's integer
// accessors and neither needs a type assertion.
func durationCodec() leafCodec {
	return leafCodec{
		kind: KindString,
		encode: func(v reflect.Value) (Value, error) {
			return String(time.Duration(v.Int()).String()), nil
		},
		parse: func(v reflect.Value, text string) error {
			d, err := time.ParseDuration(text)
			if err != nil {
				return &parseFailure{typ: v.Type(), err: err}
			}

			v.SetInt(int64(d))

			return nil
		},
	}
}

// timeCodec is time.Time as RFC 3339 with nanoseconds, through the text pair
// and never through fmt.Stringer.
//
// time.Time implements both, and only one of them round-trips: String() gives
// "2026-08-02 12:00:00 +0000 UTC", which RFC 3339 cannot parse, and with a
// monotonic reading present it gives a trailing "m=+0.000240763" - process-local
// state written into a config file. So this type is the demonstration of why
// fmt.Stringer is never consulted in either direction: String() string declares
// no inverse, and precedence alone would decide correctness (ADR-0005).
//
// Two losses are stated rather than implied. The monotonic reading is stripped,
// which is correct because it is process-local, and it is why this type's
// equality relation is time.Time.Equal rather than ==. And RFC 3339 carries the
// offset and not the zone identity, so a value that is not UTC loses its zone's
// DST rules; encoding/json/v2 produces exactly the same result on the same
// input, and its format: tag escape was removed in 1.27, so there is no
// stdlib answer to adopt.
//
// Neither half reaches the value through a type assertion. The identity table
// is keyed by reflect.Type compared with ==, so this codec is reachable from
// that one entry alone and both Sets below are assignable by construction -
// which leaves no failure arm that cannot happen and therefore cannot be tested.
func timeCodec() leafCodec {
	return leafCodec{
		kind: KindString,
		encode: func(v reflect.Value) (Value, error) {
			var when time.Time

			reflect.ValueOf(&when).Elem().Set(v)

			text, err := when.MarshalText()
			if err != nil {
				return Value{}, &encodeFailure{typ: v.Type(), err: err}
			}

			return String(string(text)), nil
		},
		parse: func(v reflect.Value, text string) error {
			var when time.Time

			if err := when.UnmarshalText([]byte(text)); err != nil {
				return &parseFailure{typ: v.Type(), err: err}
			}

			v.Set(reflect.ValueOf(when))

			return nil
		},
	}
}

// decode applies one plane observation to one field, and is the whole of
// ADR-0005's donor rule and ADR-0006's null rule in one place.
//
// Absent never reaches here: it means ferry does not write to the field at all,
// which is the walk's decision rather than a leaf's, so a seeded value keeps
// what it had and a fresh one keeps its zero.
func (cd leafCodec) decode(v reflect.Value, got Value) error {
	if got.kind == KindNull {
		return cd.decodeNull(v)
	}

	if got.kind != cd.kind && got.kind != KindString {
		return &wrongKind{got: got.kind, typ: v.Type()}
	}

	return cd.parse(v, got.text)
}

// decodeNull is ADR-0006's per-kind Null rule, which needs no new principle:
// Null is presence carrying a value, so the question is which Go types can hold
// that value, and every other leaf refuses it as a wrong kind.
//
// The refusal is what makes strictness recoverable. A registered codec for a
// type accepts Null and returns whatever it likes, so a user who wants
// "null means zero" has a mechanism; a core that zeroed in the walk would put
// the zeroing before any codec is consulted and nothing would recover
// strictness for a plain int.
func (cd leafCodec) decodeNull(v reflect.Value) error {
	if !cd.nullable {
		return &wrongKind{got: KindNull, typ: v.Type()}
	}

	v.SetZero()

	return nil
}

// wrongKind is a leaf refusing an observation whose kind it does not take.
//
// It names the kind the plane held and the type that cannot take it, and
// neither is a value the plane supplied: ADR-0011 makes that rule total,
// because ferry cannot know which addresses hold secrets.
type wrongKind struct {
	got VKind
	typ reflect.Type
}

func (e *wrongKind) Error() string {
	return fmt.Sprintf("the plane holds %s and %s cannot take one", e.got, e.typ)
}

// Unwrap keeps [ErrWrongKind] reachable, which is what makes the refusal answer
// to errors.Is against [ErrValue] as well through ADR-0011's class rules.
func (*wrongKind) Unwrap() error { return ErrWrongKind }

// parseFailure is a leaf's own parser refusing the text the plane spelled.
//
// It names the target type and whether the failure was syntax or range, and it
// never renders the cause, because strconv and time both quote the input they
// failed on and ferry chose to call them.
type parseFailure struct {
	typ reflect.Type
	err error
}

func (e *parseFailure) Error() string {
	if errors.Is(e.err, strconv.ErrRange) {
		return "the plane's value is out of range for " + e.typ.String()
	}

	return "the plane's value is not a valid " + e.typ.String()
}

func (e *parseFailure) Unwrap() error { return e.err }

// lengthFailure is a fixed-size byte array given a different number of bytes.
type lengthFailure struct {
	typ       reflect.Type
	got, want int
}

func (e *lengthFailure) Error() string {
	return fmt.Sprintf("the plane holds %d bytes and %s holds %d", e.got, e.typ, e.want)
}

// encodeFailure is a Go value the leaf's representation does not cover.
//
// It is time.Time alone in core's set: MarshalText refuses a year outside
// [0,9999], so the representation is partial over the type and the error
// surfaces rather than being swallowed.
type encodeFailure struct {
	typ reflect.Type
	err error
}

func (e *encodeFailure) Error() string {
	return "no representation for this " + e.typ.String() +
		": its text form does not cover every value the type holds"
}

func (e *encodeFailure) Unwrap() error { return e.err }
