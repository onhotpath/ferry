package ferry

import (
	"encoding"
	"errors"
	"fmt"
	"reflect"
)

// This file is step 2 of ADR-0007's chain, which sits between the identity
// table and kind admission:
//
//  1. the identity table, reflect.Type compared with ==
//  2. the text pair, encoding.TextAppender or encoding.TextMarshaler together
//     with encoding.TextUnmarshaler
//  3. kind admission
//
// A pointer type is never offered to any of the three as itself, because
// pointer indirection is structural and is resolved first. That is not
// tidiness: *big.Int implements the whole text pair in its own right, because
// big.Int's text methods are on the pointer receiver, so a chain consulted
// before the pointer shape makes a *big.Int field a leaf, ADR-0005's "a nil
// pointer writes Null at its own address" never runs, a nil dumps
// string("<nil>") and the load segfaults inside math/big on a nil receiver.
// Measured, and it is one omitted line.
//
// Only the text pair is an arm. json, binary and gob are each dropped on a
// census rather than on taste: over 29 types people put in config structs, gob
// is the sole arm for none of them, json's sole rescue is json.RawMessage which
// kind admission already carries as Bytes, and binary's sole rescue is url.URL,
// which it would rescue by base64-encoding a string.

var (
	textAppender    = reflect.TypeFor[encoding.TextAppender]()
	textMarshaler   = reflect.TypeFor[encoding.TextMarshaler]()
	textUnmarshaler = reflect.TypeFor[encoding.TextUnmarshaler]()
)

// textArm is which halves of the text pair one type declares.
//
// The two halves are probed differently and the asymmetry is the whole of what
// makes the pair a pair. The encode half is satisfied by T or by *T, because
// reading a value needs no address. The decode half is satisfied by *T alone:
// measured, an UnmarshalText on a value receiver writes to a copy and leaves
// the destination unchanged, so it is an incomplete pair with a diagnostic
// rather than a silent no-op.
type textArm struct {
	// appends and marshals are the two spellings of the encode half.
	// TextAppender is not an arm of its own, because an arm is a pair and
	// encoding exports no appending decoder to pair it with (verified on
	// go1.27rc2: AppendText and AppendBinary, and no AppendFrom of any kind).
	appends  bool
	marshals bool

	// unmarshals is the decode half, on the pointer receiver.
	unmarshals bool

	// copies is UnmarshalText on a value receiver, which is not the decode half
	// and is kept apart from its absence so the diagnostic can say which.
	copies bool
}

// armOf probes one type for the text pair, from reflect.TypeFor[T]() alone and
// with no value in hand.
//
// A pointer is declined for the reason at the top of this file, and an
// interface is declined because an interface's text methods are its dynamic
// value's rather than the static type's: the zero value of an interface is a
// nil interface with no receiver to call, and ADR-0009 makes an interface a
// registration with a codec that owns its own discriminator.
func armOf(t reflect.Type) textArm {
	if t.Kind() == reflect.Pointer || t.Kind() == reflect.Interface {
		return textArm{}
	}

	p := reflect.PointerTo(t)

	return textArm{
		appends:  p.Implements(textAppender),
		marshals: p.Implements(textMarshaler),
		// *T's method set contains T's, so a value-receiver UnmarshalText
		// satisfies p.Implements too. Subtracting it is what keeps the decode
		// half "the method can write back" rather than "the method exists".
		unmarshals: p.Implements(textUnmarshaler) && !t.Implements(textUnmarshaler),
		copies:     t.Implements(textUnmarshaler),
	}
}

// encodes reports whether either spelling of the encode half is present.
func (a textArm) encodes() bool { return a.appends || a.marshals }

// complete reports a whole pair, which is the only thing that claims a type.
func (a textArm) complete() bool { return a.encodes() && a.unmarshals }

// declares reports whether the type said anything at all about the text pair,
// which is what separates an incomplete pair from a type the chain has no
// opinion about.
func (a textArm) declares() bool { return a.encodes() || a.unmarshals || a.copies }

// textPair is the chain's one arm: a type that declares its own text form and
// its own inverse works with no registration at all.
//
// The declared kind is String, always, and it is a field of the codec resolved
// by the same lookup that finds the codec (ADR-0007). encoding.TextMarshaler
// produces text and says nothing about kind, so a type whose text is a run of
// digits - big.Int is the worked example - lands as String and does not load
// from a YAML plane that reports Number. The remedy is a registration
// declaring Number rather than a second coercion, and ADR-0005 requires a
// second coercion to meet the standard of argument the first one got.
func textPair(t reflect.Type) (leafCodec, bool) {
	if !armOf(t).complete() {
		return leafCodec{}, false
	}

	return leafCodec{kind: KindString, encode: encodeText, parse: parseText}, true
}

// encodeText is the encode half, and the case order is the preference:
// TextAppender is preferred over TextMarshaler where a type carries both.
//
// The saving is one allocation of two, measured at 25 ns against 40 ns for one
// leaf, and the ADR is explicit that this is tidiness rather than a performance
// argument - a twelve-key cached load is 476 ns, so the saving is a handful of
// nanoseconds off a path that is not hot. Preferring one spelling inherits an
// obligation no compiler can check, that the two produce the same bytes; the
// standard library implements one in terms of the other on every type carrying
// both, and nothing enforces it for a user type.
func encodeText(v reflect.Value) (Value, error) {
	switch enc := receiver(v).Interface().(type) {
	case encoding.TextAppender:
		text, err := enc.AppendText(nil)

		return textValue(v, text, err)
	case encoding.TextMarshaler:
		text, err := enc.MarshalText()

		return textValue(v, text, err)
	default:
		return Value{}, &encodeFailure{typ: v.Type(), err: errNoTextArm}
	}
}

// textValue is what both spellings of the encode half produce, so the failure
// arm is written once.
func textValue(v reflect.Value, text []byte, err error) (Value, error) {
	if err != nil {
		return Value{}, &encodeFailure{typ: v.Type(), err: err}
	}

	return String(string(text)), nil
}

// receiver is the pointer the text pair is called on.
//
// It takes the address where there is one and copies where there is not, which
// is xload's `for field.CanAddr() { field = field.Addr() }` replaced by a
// stated rule. The copy is not hypothetical: a map value is not addressable, so
// map[string]net.IP reaches the encode half through it, and taking the address
// would panic instead.
func receiver(v reflect.Value) reflect.Value {
	if v.CanAddr() {
		return v.Addr()
	}

	p := reflect.New(v.Type())
	p.Elem().Set(v)

	return p
}

// parseText is the decode half, into a destination the walk never seeded.
//
// The fresh value is LoadOver's property rather than tidiness: an UnmarshalText
// is free to merge into what it finds, and decoding into the field itself would
// let a seed leak into a value the plane fully determined. Every other leaf
// overwrites, so this is the rule the rest of the set already keeps.
func parseText(v reflect.Value, text string) error {
	fresh := reflect.New(v.Type())

	dec, ok := fresh.Interface().(encoding.TextUnmarshaler)
	if !ok {
		return &parseFailure{typ: v.Type(), err: errNoTextArm}
	}

	if err := dec.UnmarshalText([]byte(text)); err != nil {
		return &parseFailure{typ: v.Type(), err: err}
	}

	v.Set(fresh.Elem())

	return nil
}

// errNoTextArm is the defensive arm of a question the compiler already
// answered: a codec [textPair] built is only ever handed a value whose *T
// implements the half being asserted, so no schema ferry compiled can reach
// either arm it appears in. It is an error rather than a panic because a
// library that panics on its own invariant takes the caller's process with it.
var errNoTextArm = errors.New("the type does not implement the text pair")

// incompletePair is ADR-0007's refusal for a type that declares one half of the
// text pair and not the other, and it reports the diagnostic rather than the
// error so the compiler can locate it.
//
// There were three candidate answers and a census decided between them. Using
// the half anyway is per-direction selection, which dumps string("hello") and
// fails at Load with "invalid character 'h'" against a plane already written.
// Falling through to kind admission is what a naive implementation does:
// measured, a struct with a MarshalText-only field and an UnmarshalText-only
// field compiles clean and round-trips, ignoring with no diagnostic two methods
// the user wrote for exactly this purpose, and ADR-0001 rules out silently
// ignoring anything. Refusing is the answer, and it is affordable because three
// corpora - 29 config types probed in process, the whole go1.27rc2 public
// standard library, and eleven third-party modules with their transitive
// dependencies - hold zero half pairs between them, for all four arms.
func incompletePair(t reflect.Type) (string, bool) {
	a := armOf(t)
	if a.complete() || !a.declares() {
		return "", false
	}

	head := fmt.Sprintf("%s implements ", t)
	tail := ", so the pair is incomplete and ferry will not use it: implement the other half, or register a codec"

	switch {
	case a.copies:
		return fmt.Sprintf("%s declares UnmarshalText on a value receiver, which decodes into a copy and leaves "+
			"the field unchanged, so it is not the decode half: move UnmarshalText to *%s, or register a codec",
			t, t), true
	case a.encodes():
		return head + encoderName(a) + " but not encoding.TextUnmarshaler" + tail, true
	default:
		return head + "encoding.TextUnmarshaler but not encoding.TextMarshaler" + tail, true
	}
}

// encoderName is the half the type did write, named as the interface rather
// than as the method, because a reader looking it up needs the interface.
func encoderName(a textArm) string {
	if a.marshals {
		return "encoding.TextMarshaler"
	}

	return "encoding.TextAppender"
}

// chainKeyMsg refuses a map key the chain claims, and it is a separate message
// because it must name a type its author never mentioned and must therefore not
// accuse it of anything.
//
// ADR-0007 admitted such a key and reversed itself under #45. The reversal is
// not a claim that the text is lossy - measured against adversarial value
// lists, every type the chain claims from the standard library is injective,
// including a 4-in-6 address and a zoned one. It is that nobody can be asked. A
// registration has a call site where .AsMapKey() communicates the obligation,
// which ADR-0009 calls the only moment a registrant is guaranteed to read; a
// text pair has no such moment, so the refusal was defeatable by not
// registering, which made this the one place in ferry where registering a type
// left it less usable than not registering it.
func chainKeyMsg(t reflect.Type) string {
	return fmt.Sprintf("%s may not key a map: ferry claims it through its text pair rather than through a "+
		"registration, so nobody has declared its text injective over the key type, and two keys that render "+
		"alike collapse into one address with no error anywhere - register a codec for it and mark it usable "+
		"as a key with ferry.TextCodec[%s](ferry.KindString).AsMapKey()", t, t)
}
