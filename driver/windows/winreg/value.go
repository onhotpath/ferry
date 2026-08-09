package winreg

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/onhotpath/ferry"
)

// Type is what the registry records a value as, and it is the type tag stored
// beside the data rather than anything ferry decides.
type Type uint8

// The registry value types this driver has an opinion about. Everything else the
// registry can hold arrives as [TypeOther] and is refused with [ErrValueType].
const (
	// TypeString is REG_SZ, and it is the only type this driver ever writes text
	// as.
	TypeString Type = iota

	// TypeExpandString is REG_EXPAND_SZ. It is read as the text that is actually
	// stored, never with its %VARIABLES% expanded.
	TypeExpandString

	// TypeDWord is REG_DWORD, a 32-bit unsigned integer.
	TypeDWord

	// TypeQWord is REG_QWORD, a 64-bit unsigned integer.
	TypeQWord

	// TypeBinary is REG_BINARY, and it is the only type this driver ever writes
	// bytes as.
	TypeBinary

	// TypeMultiString is REG_MULTI_SZ, a sequence spelled inside one value.
	TypeMultiString

	// TypeOther is every other registry type, which this driver reads as a value
	// it cannot carry rather than guessing at it.
	TypeOther
)

// String is the Win32 name of the type, which is what a refusal prints.
func (t Type) String() string {
	switch t {
	case TypeString:
		return "REG_SZ"
	case TypeExpandString:
		return "REG_EXPAND_SZ"
	case TypeDWord:
		return "REG_DWORD"
	case TypeQWord:
		return "REG_QWORD"
	case TypeBinary:
		return "REG_BINARY"
	case TypeMultiString:
		return "REG_MULTI_SZ"
	default:
		return "a registry type ferry does not carry"
	}
}

// Datum is one registry value: what the registry records it as, and what it
// holds.
//
// One of the two payload fields carries the value and the other is empty, and
// which of them is decided by [Datum.Type]: [TypeBinary] uses Binary and every
// other type uses Text. A number's Text is its own base-10 spelling, which is
// what lets a value stored as REG_DWORD reach a Go field as a number without
// this driver having to decide how wide the field is.
type Datum struct {
	// Type is what the registry records this value as.
	Type Type

	// Text is the payload of every type but [TypeBinary]: the stored text of a
	// string, and the base-10 spelling of a number.
	Text string

	// Binary is the payload of a [TypeBinary] value, and nil for every other
	// type.
	Binary []byte
}

// ErrValueType reports a registry value whose own type ferry has no address for.
//
// REG_MULTI_SZ is the case that arises: it is a sequence spelled inside one
// value, so a field reading it would have to take the whole list as one string or
// this driver would have to invent addresses the registry does not have. Neither
// is honest, so it is refused at the address that holds it and the field is left
// alone.
//
// It wraps [ferry.ErrPlane], and it stays reachable under ferry's wrapper, so
// errors.Is answers for it on what [ferry.Load] returned.
var ErrValueType = errors.New("winreg: this registry value has a type ferry cannot carry")

// ErrUnspellable reports a value ferry carries that REG_SZ cannot write down.
//
// A registry string is UTF-16 and NUL-terminated, so two Go strings have no
// faithful spelling here: one holding a NUL, which every Windows reader truncates
// at, and one that is not valid UTF-8, which the conversion to UTF-16 replaces
// with U+FFFD. Both are refused loudly rather than stored mangled. A []byte field
// is unaffected, because bytes are written as REG_BINARY and that type carries
// every byte including NUL.
//
// It wraps [ferry.ErrValue] rather than [ferry.ErrPlane], because nothing is
// wrong with the registry: the value has no representation here, and retrying it
// is pointless in the way an ErrValue promises.
//
// It stays reachable under ferry's wrapper, so errors.Is answers for it on what
// [ferry.Dump] returned.
var ErrUnspellable = errors.New("winreg: a registry string cannot spell this text")

// errNoNull is the refusal that makes this a plane with no null rather than a
// plane that quietly has one.
//
// A registry value cannot exist without a type, and every type this driver writes
// carries a payload, so there is nothing to store for a null that would not also
// be the spelling of empty text or of no bytes.
var errNoNull = fmt.Errorf("%w: a registry value cannot exist without a type, and this address was handed a "+
	"null: a nil pointer to a value has nothing to be written as here, and storing empty text for it would "+
	"make it indistinguishable from an empty string", ferry.ErrValue)

// held is the one place a stored value becomes a [ferry.Value], so that nothing
// in this driver can disagree about what the registry said.
//
// The read side is deliberately wider than the write side. Group Policy and human
// operators write REG_DWORD, REG_QWORD and REG_EXPAND_SZ, and a driver that could
// not read them would be useless against a key anybody else maintains.
//
// REG_EXPAND_SZ is read raw and is never expanded. Expanding it would turn
// %SystemRoot%-literal into C:\WINDOWS-literal, and the next dump would write that
// back over what the operator wrote - a plane-compatibility break committed by the
// driver on somebody else's data.
func held(d Datum) (ferry.Value, error) {
	switch d.Type {
	case TypeString, TypeExpandString:
		return ferry.String(d.Text), nil
	case TypeDWord, TypeQWord:
		return ferry.Number(d.Text), nil
	case TypeBinary:
		return ferry.Bytes(d.Binary), nil
	case TypeMultiString:
		return ferry.Value{}, badType("REG_MULTI_SZ spells a sequence inside one value, and ferry addresses " +
			"each element of a sequence in its own right, so there is no address here to read it into")
	default:
		return ferry.Value{}, badType("this driver reads REG_SZ, REG_EXPAND_SZ, REG_DWORD, REG_QWORD and " +
			"REG_BINARY, and stores REG_SZ and REG_BINARY")
	}
}

// badType states the class this driver has an opinion about and keeps
// [ErrValueType] reachable underneath it.
func badType(msg string) error {
	return fmt.Errorf("%w: %w: %s", ferry.ErrPlane, ErrValueType, msg)
}

// stored is the one place a [ferry.Value] becomes a registry value.
//
// Two types are written and no more. Bytes go to REG_BINARY, which is the only
// type that carries an arbitrary byte sequence, and everything else goes to
// REG_SZ, which is the only type that carries the boundary's own spelling of a
// number intact: 007, 3.14159265358979 and 18446744073709551615 all come back
// exactly as they were written, and REG_DWORD can express none of the three while
// REG_QWORD normalises the first and refuses the second.
//
// Writing a value replaces its type, so a value an operator retyped by hand is
// retyped back on the next dump. The data survives and the type annotation does
// not, and that is stated in this package's documentation rather than worked
// around: reading the plane before writing it, to keep the type somebody else
// chose, would make a dump depend on what the plane already held, and this driver
// stages a replacement instead.
//
// Absent never arrives, because an omitted address gets no write at all.
func stored(v ferry.Value) (Datum, error) {
	switch v.Kind() {
	case ferry.KindString:
		return text(v.AsString())
	case ferry.KindNumber:
		return text(v.AsNumber())
	case ferry.KindBool:
		b, err := v.AsBool()

		return text(strconv.FormatBool(b), err)
	case ferry.KindBytes:
		b, err := v.AsBytes()
		if err != nil {
			return Datum{}, err
		}

		return Datum{Type: TypeBinary, Binary: b}, nil
	case ferry.KindNull, ferry.KindAbsent:
		return Datum{}, errNoNull
	default:
		return Datum{}, fmt.Errorf("%w: the registry was handed %s, which is not a kind ferry's boundary has",
			ferry.ErrValue, v.Kind())
	}
}

// text is the REG_SZ arm of every accessor above, so that the switch reads as one
// decision rather than four copies of the same two checks.
func text(s string, err error) (Datum, error) {
	if err != nil {
		return Datum{}, err
	}

	if err := spellable(s); err != nil {
		return Datum{}, err
	}

	return Datum{Type: TypeString, Text: s}, nil
}

// spellable refuses the two Go strings a REG_SZ value cannot hold, and it names
// neither of them in its message: the text is the caller's value, and ADR-0011
// keeps a value out of ferry's own message text.
func spellable(s string) error {
	switch {
	case strings.IndexByte(s, 0) >= 0:
		return unspellable("it holds a NUL, and a registry string ends at the first one, so every reader of " +
			"this value would see the text up to it and nothing after")
	case !utf8.ValidString(s):
		return unspellable("it is not valid UTF-8, and a registry string is UTF-16, so the conversion would " +
			"replace what cannot be encoded and store something else: write it as a []byte field instead, " +
			"which is stored as REG_BINARY and carries every byte")
	default:
		return nil
	}
}

// unspellable states the class this driver has an opinion about and keeps
// [ErrUnspellable] reachable underneath it.
func unspellable(msg string) error {
	return fmt.Errorf("%w: %w: %s", ferry.ErrValue, ErrUnspellable, msg)
}
