package ferry

import (
	"encoding"
	"errors"
	"fmt"
	"reflect"
)

// This file is the whole of ADR-0011's recover fence, and it is one file so
// that the fence's boundary can be read in one place.
//
// The rule is narrow and it is the reason the fence is safe at all: recover
// wraps exactly the call into user code, which is a codec half and nothing
// else. Every function below whose body is a single call under a deferred
// fence is one such boundary, and none of them holds a line of ferry's own
// logic. A panic from ferry's own walk - the reflect failures of #222 and #224
// among them - is outside every one of them and keeps unwinding, because a
// fence wide enough to swallow ferry's bugs would hide exactly the defects it
// was added to survive (ADR-0011 amended under #254, ADR-0019).
//
// Core's own leaves are not fenced either. They are ferry's logic wearing a
// codec's shape, so fencing them would convert ferry's own panics into
// addressed errors by the back door.

// fence is the fence's decision, given whatever recover returned and whatever
// the call was returning: a panic becomes an error of ErrPanic's class, and
// where nothing panicked the call's own answer stands.
//
// It is used as `defer func() { err = fence(recover(), err) }()` from a
// function whose body is one call into user code, which is what keeps the
// recovered set exactly that call: recover answers only for the goroutine's
// innermost deferring function, so a panic raised anywhere outside such a body
// reaches no fence at all.
func fence(p any, err error) error {
	if p == nil {
		return err
	}

	return &codecPanic{value: p}
}

// codecPanic is a user-code panic that a fence recovered, carrying what was
// recovered.
//
// It is minted at the boundary and never at the address, because the boundary
// is where the panic is caught and the address is the walk's to attach: the
// walk already wraps a codec's error in a located [Error], so a fenced panic
// arrives in the report at the address that produced it with nothing added
// here.
//
// The recovered value is printed, which is the one place ferry's own message
// text carries something ferry did not write. The obligation not to put a
// plane's value in it is the codec author's, which is the shape ADR-0011
// already gives a driver's own text, and the alternative is a report that says
// a codec panicked and will not say with what.
type codecPanic struct{ value any }

func (e *codecPanic) Error() string {
	return fmt.Sprintf("the codec panicked, and ferry recovered it so that the rest of the report survives: %v",
		e.value)
}

// Unwrap keeps [ErrPanic] reachable, which is how the class reaches a located
// error: the walk wraps a codec failure with withCause, and withCause adopts
// the class the cause declares (ADR-0011).
func (*codecPanic) Unwrap() error { return ErrPanic }

// panicked reports whether err is a panic a fence recovered.
//
// It is what stops a wrapper adding its own sentence to one. "the plane's
// value is not a valid netip.Addr" is a claim about a codec that returned, and
// a codec that panicked returned nothing to make it about.
func panicked(err error) bool {
	_, ok := errors.AsType[*codecPanic](err)

	return ok
}

// parseFailed is what a decode half's error becomes at the field it was
// decoding into: ferry's own sentence around a refusal, and a recovered panic
// passed through whole.
func parseFailed(v reflect.Value, err error) error {
	if panicked(err) {
		return err
	}

	return &parseFailure{typ: v.Type(), err: err}
}

// encodeFailed is the same on the encode half.
func encodeFailed(v reflect.Value, err error) error {
	if panicked(err) {
		return err
	}

	return &encodeFailure{typ: v.Type(), err: err}
}

// The rest of this file is one function per call into user code, and each body
// is that call and nothing else.

// formatted is [StringCodec]'s encode half.
func formatted[T any](format func(T) string, in T) (text string, err error) {
	defer func() { err = fence(recover(), err) }()

	return format(in), nil
}

// parsedFrom is [StringCodec]'s decode half.
func parsedFrom[T any](parse func(string) (T, error), text string) (out T, err error) {
	defer func() { err = fence(recover(), err) }()

	return parse(text)
}

// encodedValue is [ValueCodec]'s encode half.
func encodedValue[T any](enc func(T) (Value, error), in T) (out Value, err error) {
	defer func() { err = fence(recover(), err) }()

	return enc(in)
}

// decodedValue is [ValueCodec]'s decode half.
func decodedValue[T any](dec func(Value) (T, error), got Value) (out T, err error) {
	defer func() { err = fence(recover(), err) }()

	return dec(got)
}

// appendedText is the text pair's encode half in its appending spelling.
func appendedText(enc encoding.TextAppender) (text []byte, err error) {
	defer func() { err = fence(recover(), err) }()

	return enc.AppendText(nil)
}

// marshalledText is the text pair's encode half in its marshalling spelling.
func marshalledText(enc encoding.TextMarshaler) (text []byte, err error) {
	defer func() { err = fence(recover(), err) }()

	return enc.MarshalText()
}

// unmarshalledText is the text pair's decode half. What it decodes into is
// ferry's own fresh value and the write back into the field is ferry's own, so
// both stay outside the fence.
func unmarshalledText(dec encoding.TextUnmarshaler, text []byte) (err error) {
	defer func() { err = fence(recover(), err) }()

	return dec.UnmarshalText(text)
}
