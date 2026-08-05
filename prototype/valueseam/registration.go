package valueseam

// The registration API, redesigned without Decl: kind-named
// constructors whose halves are typed by PAYLOAD, so a user codec
// never constructs a Value at all — the emit-check class (#231)
// becomes unwritable instead of checked.

import (
	"errors"
	"fmt"
	"reflect"
)

// Reg is one registration, built only by the constructors below.
type Reg struct {
	typ    reflect.Type
	kind   VKind
	encode func(v any) (Value, error) // wraps the typed half; kind correct by construction
	decode func(v Value) (any, error)
	asKey  bool
}

// ErrNilHalf is the loud refusal of a nil codec half, at construction.
var ErrNilHalf = errors.New("codec registration requires both halves")

// ── Value codecs: halves typed by payload, kind in the name ─────────

// BoolValue registers T rendered as a bool. The encode half returns a
// bool — producing a wrong-kind Value is not something it can spell.
func BoolValue[T any](enc func(T) (bool, error), dec func(bool) (T, error)) Reg {
	mustHalves(enc, dec)
	return Reg{
		typ:  reflect.TypeFor[T](),
		kind: KindBool,
		encode: func(v any) (Value, error) {
			b, err := enc(v.(T))
			if err != nil {
				return Value{}, err
			}
			return Bool(b), nil // core builds the Value; the kind cannot be wrong
		},
		decode: func(v Value) (any, error) {
			b, err := v.AsBool()
			if err != nil {
				return nil, err
			}
			return dec(b)
		},
	}
}

// NumberValue registers T rendered as canonical number text.
func NumberValue[T any](enc func(T) (string, error), dec func(string) (T, error)) Reg {
	mustHalves(enc, dec)
	return Reg{
		typ:  reflect.TypeFor[T](),
		kind: KindNumber,
		encode: func(v any) (Value, error) {
			s, err := enc(v.(T))
			if err != nil {
				return Value{}, err
			}
			return Number(s), nil
		},
		decode: func(v Value) (any, error) {
			s, err := v.AsNumber()
			if err != nil {
				return nil, err
			}
			return dec(s)
		},
	}
}

// StringValue registers T rendered as a string.
func StringValue[T any](enc func(T) (string, error), dec func(string) (T, error)) Reg {
	mustHalves(enc, dec)
	return Reg{
		typ:  reflect.TypeFor[T](),
		kind: KindString,
		encode: func(v any) (Value, error) {
			s, err := enc(v.(T))
			if err != nil {
				return Value{}, err
			}
			return String(s), nil
		},
		decode: func(v Value) (any, error) {
			s, err := v.AsString()
			if err != nil {
				return nil, err
			}
			return dec(s)
		},
	}
}

// BytesValue registers T rendered as bytes.
func BytesValue[T any](enc func(T) ([]byte, error), dec func([]byte) (T, error)) Reg {
	mustHalves(enc, dec)
	return Reg{
		typ:  reflect.TypeFor[T](),
		kind: KindBytes,
		encode: func(v any) (Value, error) {
			b, err := enc(v.(T))
			if err != nil {
				return Value{}, err
			}
			return Bytes(b), nil
		},
		decode: func(v Value) (any, error) {
			b, err := v.AsBytes()
			if err != nil {
				return nil, err
			}
			return dec(b)
		},
	}
}

func mustHalves(enc, dec any) {
	if enc == nil || dec == nil ||
		reflect.ValueOf(enc).IsNil() || reflect.ValueOf(dec).IsNil() {
		panic(ErrNilHalf) // at the composition site, not mid-load
	}
}

// ── map keys: eligibility is a method that exists only where legal ──

// KeyableReg is a registration whose kind may key a map: only the
// String and Number constructors return it, so a bytes- or
// bool-keyed map is unwritable rather than refused.
type KeyableReg struct{ Reg }

// AsMapKey marks the registration as usable at the key position.
func (k KeyableReg) AsMapKey() Reg {
	k.Reg.asKey = true
	return k.Reg
}

// StringKey and NumberKey are the keyable forms of the two eligible
// constructors. (In ferry these fold into StringValue/NumberValue
// returning KeyableReg; split here to keep the demo surface obvious.)
func StringKey[T any](enc func(T) (string, error), dec func(string) (T, error)) KeyableReg {
	return KeyableReg{Reg: StringValue[T](enc, dec)}
}

func NumberKey[T any](enc func(T) (string, error), dec func(string) (T, error)) KeyableReg {
	return KeyableReg{Reg: NumberValue[T](enc, dec)}
}

// ── the registry stub: freeze semantics per G6 ──────────────────────

type RegistryB struct {
	frozen bool
	byType map[reflect.Type]Reg
}

func NewRegistryB(regs ...Reg) (*RegistryB, error) {
	r := &RegistryB{byType: map[reflect.Type]Reg{}}
	for _, reg := range regs {
		if _, dup := r.byType[reg.typ]; dup {
			return nil, fmt.Errorf("%s is already registered", reg.typ)
		}
		r.byType[reg.typ] = reg
	}
	r.frozen = true // construction IS the freeze: no mutable phase exists
	return r, nil
}

func (r *RegistryB) lookup(t reflect.Type) (Reg, bool) {
	reg, ok := r.byType[t]
	return reg, ok
}

// EncodeVia and DecodeVia stand in for the walk's two codec seams.
func EncodeVia(r *RegistryB, v any) (Value, error) {
	reg, ok := r.lookup(reflect.TypeOf(v))
	if !ok {
		return Value{}, fmt.Errorf("%T is not registered", v)
	}
	return reg.encode(v)
}

func DecodeVia[T any](r *RegistryB, v Value) (T, error) {
	var zero T
	reg, ok := r.lookup(reflect.TypeFor[T]())
	if !ok {
		return zero, fmt.Errorf("%v is not registered", reflect.TypeFor[T]())
	}
	out, err := reg.decode(v)
	if err != nil {
		return zero, err
	}
	return out.(T), nil
}
