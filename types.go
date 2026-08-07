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

	// accept is the whole-Value decode half, and it is nil for every leaf whose
	// rule is core's own: take your own kind or a String, parse the text with
	// your own parser, and refuse everything else including Null.
	//
	// It is a hook rather than the bool it replaces, and the bool is what made
	// the difference invisible. "Accepts Null" spelled as a bool can only mean
	// one thing, that Null loads as the Go zero, which is right for []byte and
	// for every pointer leaf and is not what ADR-0009's ValueCodec needs: that
	// codec's decode half sees the whole Value, which is the only way to accept
	// a Null into a Go type whose kind has no null and to tell number("7") from
	// string("7") while doing it. So the accepted set is the codec's rather than
	// something core derives from the declared kind, and ADR-0006's strictness
	// rests on exactly that (ADR-0009).
	accept func(reflect.Value, Value) error
}

// nullIsZero gives a leaf a null of its own, which in core's set is []byte and
// every pointer leaf.
//
// ADR-0006: Null is presence carrying a value, so it is admitted by exactly the
// Go types that have a null and refused by every other leaf as a wrong kind -
// the same refusal a Bool gets at a string field. [N]byte has no nil, and
// neither has any number, any bool, any string or either identity leaf.
//
// The wrapper closes over the codec as it stands before accept is set, so the
// fall-through below reaches whatever rule that codec already had and never
// itself. It falls through to decode rather than to decodeText, because the
// codec being wrapped may carry an accept half of its own: a *T over a
// registered T is exactly that shape, and reaching past it would derive the
// accepted set from the declared kind, which is the derivation ADR-0009 forbids
// (#229).
func nullIsZero(cd leafCodec) leafCodec {
	inner := cd

	cd.accept = func(v reflect.Value, got Value) error {
		if got.kind == KindNull {
			v.SetZero()

			return nil
		}

		return inner.decode(v, got)
	}

	return cd
}

// leafFor resolves a type to its leaf behaviour, by type identity first, by the
// text pair second, and by reflect.Kind third.
//
// A registration is an entry in the same identity table the chain consults
// first, so it is asked before core's own entries and before everything else.
// Registering a type the chain would have claimed is therefore legal and wins,
// which is not a loophole: it is the mechanism by which a user overrides a
// representation a dependency chose, and ADR-0007 recorded that exposure and
// left the remedy here. The two halves of step one cannot collide, because
// registering a type core owns is refused at the registration call site.
//
// The ordering is the whole rule (ADR-0005, ADR-0007). time.Duration's kind is
// int64 and time.Time's kind is struct, so a kind-first resolution writes a
// nanosecond count for one and walks three unexported fields of the other.
// time.Time also carries a text pair, so it is the case that proves the first
// step beats the second: an entry in the table is not replaceable, and a user
// wanting another representation for a type core owns defines a named type and
// registers that.
//
// The chain sits before kind admission because a declaration beats an
// inference. It shortens ADR-0005's refusal list by seven types and shrinks the
// category of representations nobody chose: net.IP stops landing as sixteen raw
// bytes and lands as 192.0.2.1, slog.Level as WARN rather than 4. Both orders
// drift under an unrelated edit and the ADR says so - before-kind drifts when a
// dependency adds a text pair, after-kind when somebody exports a field - and
// the case rests on the first being a visibly serialization-shaped edit and the
// second not being one.
func (r *Registry) leafFor(t reflect.Type) (leafCodec, bool) {
	if reg, ok := r.lookup(t); ok {
		return reg.codec, true
	}

	if cd, ok := byIdentity[t]; ok {
		return cd, true
	}

	if cd, ok := textPair(t); ok {
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
func (r *Registry) pointerLeaf(t reflect.Type) (leafCodec, bool) {
	elem := t.Elem()

	inner, ok := r.leafFor(elem)
	if !ok {
		return leafCodec{}, false
	}

	return nullIsZero(leafCodec{
		kind: inner.kind,
		encode: func(v reflect.Value) (Value, error) {
			if v.IsNil() {
				return nullValue, nil
			}

			return inner.encode(v.Elem())
		},
		// The pointee is built fresh and only then published, which is what
		// keeps a seed the caller still holds out of the walk's reach: writing
		// through the seed's own pointer would let a partial load mutate a
		// value LoadOver promises never to touch.
		parse:  intoFresh(elem, inner.parse),
		accept: pointerAccept(elem, inner),
	}), true
}

// intoFresh runs a pointee's text half into a value built for the purpose, and
// publishes the pointer only once that half succeeded.
func intoFresh(elem reflect.Type, parse func(reflect.Value, string) error) func(reflect.Value, string) error {
	return func(v reflect.Value, text string) error {
		fresh := reflect.New(elem)
		if err := parse(fresh.Elem(), text); err != nil {
			return err
		}

		v.Set(fresh)

		return nil
	}
}

// pointerAccept carries a pointee codec's own whole-observation half up to the
// pointer, and is nil where the pointee has none.
//
// Without it a *T over a registered T decodes through core's own rule, gated by
// a comparison against the declared kind, so the same codec that accepts a bool
// at a T field refuses one at a *T field with "what is set here is bool, and *T
// cannot take one". The accepted set is the codec's and is never derived from
// the kind it declared (ADR-0009, #229).
func pointerAccept(elem reflect.Type, inner leafCodec) func(reflect.Value, Value) error {
	if inner.accept == nil {
		return nil
	}

	return func(v reflect.Value, got Value) error {
		fresh := reflect.New(elem)
		if err := inner.accept(fresh.Elem(), got); err != nil {
			return err
		}

		v.Set(fresh)

		return nil
	}
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
	reflect.TypeFor[time.Duration](): durationLeaf(),
	reflect.TypeFor[time.Time]():     timeLeaf(),
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
		return boolLeaf(), true
	case reflect.String:
		return stringLeaf(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return signedLeaf(t.Bits()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return unsignedLeaf(t.Bits()), true
	case reflect.Float32, reflect.Float64:
		return floatLeaf(t.Bits()), true
	case reflect.Slice, reflect.Array:
		return bytesLeaf(t)
	default:
		return leafCodec{}, false
	}
}

// bytesLeaf claims a slice or an array of bytes, and declines every other one
// to the composite rules.
//
// Both identities are forced by Go rather than chosen by ferry. Measured,
// reflect.TypeFor[[]byte]() == reflect.TypeFor[[]uint8]() is true, so ferry
// cannot offer both "a byte blob" and "a slice of small unsigned integers"; it
// has one type and picks Bytes. And reflect.TypeFor[[]rune]() ==
// reflect.TypeFor[[]int32]() is true, so []rune is an indexed composite of
// numbers rather than text, which is legal and is almost certainly not what a
// user meant. Both belong in the documentation rather than being discovered.
func bytesLeaf(t reflect.Type) (leafCodec, bool) {
	if t.Elem().Kind() != reflect.Uint8 {
		return leafCodec{}, false
	}

	if t.Kind() == reflect.Slice {
		return byteSliceLeaf(), true
	}

	return byteArrayLeaf(t.Len()), true
}

// boolLeaf is strconv.FormatBool and its inverse, so the kind carries the
// canonical spelling and never 1 or "yes".
//
// ParseBool is the leaf's own parser rather than a wider reading of what a
// human might have meant, and the refusal it produces is core declining to
// guess rather than core ruling on what a plane may spell. A plane whose
// booleans are yes and no, or on and off, says so where the plane is, through
// its driver's own spelling, and what reaches here is then a Bool and not text
// for this parser to widen (ADR-0018).
func boolLeaf() leafCodec {
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

// stringLeaf carries the bytes unmodified, and is not required to be UTF-8: a
// Go string is a byte sequence and a NUL is not a terminator.
//
// It accepts String and nothing else, and the refusal that matters is Number.
// Accepting one would be ferry overriding a plane's own type information and
// would destroy the quoting distinction the boundary exists to preserve, where
// port: 8080 arrives as Number and port: "8080" as String and each round-trips
// back to its own spelling (ADR-0004, ADR-0005).
func stringLeaf() leafCodec {
	return leafCodec{
		kind:   KindString,
		encode: func(v reflect.Value) (Value, error) { return String(v.String()), nil },
		parse: func(v reflect.Value, text string) error {
			v.SetString(text)

			return nil
		},
	}
}

// signedLeaf is one signed width, in base 10.
//
// The width is the parse's own, not int64's, so an out-of-range value is
// strconv.ErrRange and therefore an error rather than a truncation or a
// saturation. koanf's Int64() turning 18446744073709551615 into MaxInt64 with a
// nil error is one of the silent wrong answers ferry exists in order not to
// have (ADR-0001).
func signedLeaf(bits int) leafCodec {
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

// unsignedLeaf is one unsigned width, on the same argument as [signedLeaf].
// It is the half that makes 18446744073709551615 representable at all.
func unsignedLeaf(bits int) leafCodec {
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

// floatLeaf is one float width, formatted and parsed at its own bit size.
//
// The bit size is load-bearing rather than incidental: a float32 formatted at
// 64 bits gives 0.10000000149011612, which re-rounds to the same float32 and is
// a wrong-looking config file. So one third is "0.3333333333333333" at 64 bits
// and "0.33333334" at 32, and the two rows of the golden table are required to
// disagree on it.
func floatLeaf(bits int) leafCodec {
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

// byteSliceLeaf is []byte, and it is the one leaf in core's set with a null.
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
func byteSliceLeaf() leafCodec {
	return nullIsZero(leafCodec{
		kind: KindBytes,
		encode: func(v reflect.Value) (Value, error) {
			if v.IsNil() {
				return nullValue, nil
			}

			return Bytes(v.Bytes()), nil
		},
		parse: func(v reflect.Value, text string) error {
			v.SetBytes([]byte(text))

			return nil
		},
	})
}

// byteArrayLeaf is [N]byte, which agrees with encoding/json/v2: measured, v2
// marshals [3]byte{1,2,3} as "AQID" while v1 marshals it as [1,2,3], and v1's
// behaviour survives only through the legacy FormatByteArrayAsArray option.
//
// The length is part of the type, so a plane holding a different number of
// bytes is loud rather than padded or truncated. The message names both
// lengths, which ADR-0011 permits because a length is structure rather than a
// value the plane supplied.
func byteArrayLeaf(n int) leafCodec {
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

// durationLeaf is time.Duration as 30s, and ferry departs from
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
func durationLeaf() leafCodec {
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

// timeLeaf is time.Time as RFC 3339 with nanoseconds, through the text pair
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
func timeLeaf() leafCodec {
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

// mapKey is what a key type does at the address it becomes, in both directions.
//
// It is not a [leafCodec] and carries no kind, because a key never crosses the
// boundary as a Value: it becomes the segment text of an address on the way out
// and is parsed back out of that text on the way in. That is also why a key type
// is a decode rather than a conversion - a prototype that converted the segment
// text straight to the key type panicked on map[int]string with "value of type
// string cannot be converted to type int", which is how the restriction below
// was found rather than assumed.
//
// Rendering cannot fail for a key type core admits, because core admits only
// key types whose text is total over the type. It can for a registered one: a
// registrant's encode half is theirs, the zero-value check discharges one value
// of it and no rule discharges the rest, so the error is reported at the
// container address rather than swallowed into an empty segment (ADR-0009).
// Parsing can fail either way, because the text is the plane's.
type mapKey struct {
	text  func(reflect.Value) (string, error)
	parse func(reflect.Value, string) error
}

// coreKey is a key type core admits, whose rendering is total over the type by
// construction and so has no failure arm to report.
func coreKey(text func(reflect.Value) string, parse func(reflect.Value, string) error) mapKey {
	return mapKey{
		text:  func(v reflect.Value) (string, error) { return text(v), nil },
		parse: parse,
	}
}

// mapKeyFor resolves a type's key behaviour, and declines every type that is not
// declared usable as a map key.
//
// Admissibility is declared per entry and nothing else confers it, membership of
// the identity table included. The obligation is injectivity under Go's ==,
// because == is what a Go map's key identity is and therefore what decides how
// many entries the map holds: two keys rendering to one text are one address,
// and one entry is lost with no error anywhere. string and the integer kinds are
// admitted because they are trivially injective - the text is the value, or base
// 10 is a bijection on the width - and time.Duration because a randomised hunt
// over 2^20 values plus the extremes found no collision. time.Time is in the
// same identity table and is refused, which is the whole of what "per entry"
// means (ADR-0005).
// A registration is asked first and answers for itself either way, which is
// what makes .AsMapKey() the whole of the question for a registered type: a
// registered codec that did not say the word is refused here even where its
// kind would have admitted it, because the registration replaced the
// representation the kind rule was reasoning about.
func (r *Registry) mapKeyFor(t reflect.Type) (mapKey, bool) {
	if reg, ok := r.lookup(t); ok {
		return registeredKey(reg)
	}

	// Identity before kind, for the reason [Registry.leafFor] gives:
	// time.Duration's kind is int64, so a kind-first resolution would key a map
	// by a nanosecond count and write /timeouts/30000000000 where ferry writes
	// /timeouts/30s.
	if t == reflect.TypeFor[time.Duration]() {
		return coreKey(func(v reflect.Value) string { return time.Duration(v.Int()).String() },
			durationLeaf().parse), true
	}

	// The chain is consulted before the kind switch for the same reason
	// [Registry.leafFor] consults it there: a type has one representation at
	// every position it appears in. Without this a type the chain claims through
	// its text pair - slog.Level, or any named string type carrying one - is
	// admitted as a key by its underlying kind and addresses /m/4 while the same
	// type at a leaf writes string("WARN"), and ADR-0007's refusal of a
	// chain-claimed key is enforced only for the ones whose kind is struct
	// (ADR-0005, ADR-0007 as amended under #45, #230).
	if armOf(t).complete() {
		return mapKey{}, false
	}

	switch t.Kind() {
	case reflect.String:
		return coreKey(reflect.Value.String, stringLeaf().parse), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return coreKey(func(v reflect.Value) string { return strconv.FormatInt(v.Int(), numBase) },
			signedLeaf(t.Bits()).parse), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return coreKey(func(v reflect.Value) string { return strconv.FormatUint(v.Uint(), numBase) },
			unsignedLeaf(t.Bits()).parse), true
	default:
		return mapKey{}, false
	}
}

// registeredKey is a registered codec doing duty as a key: the address segment
// is the text its encode half produced, and the key is read back through its own
// decode half rather than through a second one.
//
// The kind the codec declared is discarded here, and that is the same rule the
// leaf position keeps from the other side: a key never crosses the boundary as a
// Value, so what a plane would have called this text is not a question anybody
// is asking at an address segment.
func registeredKey(reg registration) (mapKey, bool) {
	if !reg.key {
		return mapKey{}, false
	}

	return mapKey{
		text: func(v reflect.Value) (string, error) {
			out, err := reg.codec.encode(v)
			if err != nil {
				return "", err
			}

			return out.text(), nil
		},
		parse: reg.codec.parse,
	}, true
}

// mapKeyMsg refuses a key type, and it is five messages rather than one because
// only three of them have a remedy that exists.
//
// A message reading "register an injective codec for it" would be naming a
// remedy that does not exist for the first two: no text form of time.Time can be
// injective, because == compares a *Location and no text carries a pointer, and
// two distinct NaN payloads both format as NaN. Naming a remedy that cannot work
// is the mistake ADR-0005 corrected for time.Time by name.
//
// The identity table is consulted before the chain here for the same reason
// [Registry.leafFor] consults it first: time.Time carries a text pair, and one
// question answered by two lookups is how a chain drifts.
func (r *Registry) mapKeyMsg(t reflect.Type) string {
	switch {
	case r.registered(t):
		return registeredKeyMsg(t)
	case t == reflect.TypeFor[time.Time]():
		return "time.Time is in core's own set and is not usable as a map key: its text is not injective over " +
			"the type, because == compares the *Location and no text carries a pointer, so two distinct keys " +
			"collapse into one address - key the map by a type that is injective, or convert the key yourself"
	case armOf(t).complete():
		return chainKeyMsg(t)
	case t.Kind() == reflect.Float32 || t.Kind() == reflect.Float64:
		return fmt.Sprintf("%s is not usable as a map key: two distinct NaN payloads both format as NaN, so its "+
			"text is not injective over the type and two distinct keys collapse into one address - key the "+
			"map by a type that is injective, or convert the key yourself", t)
	default:
		return fmt.Sprintf("%s is not usable as a map key: a key becomes the segment text of an address and has "+
			"to parse back out of it, so ferry keys a map by a string or an integer kind - key the map by one, "+
			"or register a codec for it that declares itself usable as one", t)
	}
}

// registered reports whether a registration claims this type, which is the one
// question about a registry that is not itself a resolution.
func (r *Registry) registered(t reflect.Type) bool {
	_, ok := r.lookup(t)

	return ok
}

// registeredKeyMsg refuses a registered key type whose registrant did not
// declare it usable as one.
//
// The refusal is at schema compile, from reflect.TypeFor[T]() alone, which is
// the same assertability every other refusal in this design has. And the
// diagnostic is where the obligation gets communicated rather than merely where
// it is enforced: a registration is the one moment a registrant is guaranteed
// to read, and the implied rule's failure is a map entry that ceases to exist
// on a plane already written (ADR-0009).
func registeredKeyMsg(t reflect.Type) string {
	return fmt.Sprintf("%s has a registered codec but is not declared usable as a map key: a key codec's text "+
		"must be injective over the key type under ==, or two keys that render alike collapse into one address "+
		"and one entry is lost with no error anywhere - add .AsMapKey() to the registration if it is", t)
}

// decode applies one plane observation to one field, and is the whole of
// ADR-0005's donor rule and ADR-0006's null rule in one place.
//
// Absent never reaches here: it means ferry does not write to the field at all,
// which is the walk's decision rather than a leaf's, so a seeded value keeps
// what it had and a fresh one keeps its zero.
//
// A leaf carrying its own accept half owns the whole rule, which is what makes
// strictness recoverable. Core refuses a Null at a plain int, and a registered
// codec for its own type accepts one and returns 0; a core that zeroed in the
// walk instead would put the zeroing before any codec is consulted and nothing
// would recover strictness for the plain int (ADR-0006, ADR-0009).
func (cd leafCodec) decode(v reflect.Value, got Value) error {
	if cd.accept != nil {
		return cd.accept(v, got)
	}

	return cd.decodeText(v, got)
}

// decodeText is core's own decode rule: a leaf takes its own kind and a String,
// parses the text with its own parser, and refuses everything else - a Null
// among them, because a leaf reaching here has no null of its own.
func (cd leafCodec) decodeText(v reflect.Value, got Value) error {
	if got.kind != cd.kind && got.kind != KindString {
		return &wrongKind{got: got.kind, typ: v.Type()}
	}

	return cd.parse(v, got.text())
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
	return fmt.Sprintf("what is set here is %s, and %s cannot take one", e.got, e.typ)
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
		return "what is set here is out of range for " + e.typ.String()
	}

	return "what is set here is not a valid " + e.typ.String() + parseHint(e.typ)
}

// parseHint is the two types whose stdlib error carried a reason ferry's
// redaction rule drops, and it is why the rule costs nothing rather than
// costing a diagnostic (ADR-0011).
//
// Every other refusal loses only the value, which the address already locates.
// These two lose "missing unit" and "not RFC 3339", so ferry states the rule
// instead of echoing the input, and the hint is better than the message it
// replaces. Both types are in core's identity table, so the obligation lands
// where the representation was already pinned; a registered codec adds no
// entry, because its own representation is proved by its own proof.
func parseHint(t reflect.Type) string {
	switch t {
	case reflect.TypeFor[time.Duration]():
		return ": a duration needs a unit, as in 30s or 1h30m"
	case reflect.TypeFor[time.Time]():
		return ": a time is RFC 3339, as in 2026-08-02T12:00:00Z"
	default:
		return ""
	}
}

func (e *parseFailure) Unwrap() error { return e.err }

// lengthFailure is a fixed-size byte array given a different number of bytes.
type lengthFailure struct {
	typ       reflect.Type
	got, want int
}

func (e *lengthFailure) Error() string {
	return fmt.Sprintf("what is set here is %d bytes and %s holds %d", e.got, e.typ, e.want)
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
