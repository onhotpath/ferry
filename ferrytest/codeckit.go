package ferrytest

import (
	"errors"
	"strings"

	"github.com/onhotpath/ferry"
)

// The case numbers, so that a report names the case in ADR-0014's list rather
// than a position in this file.
const (
	codecTextNo      = 1
	codecNilEncodeNo = 2
	codecNilDecodeNo = 3
	codecKindNo      = 4
	codecAcceptNo    = 5
	codecKeyNo       = 6
	codecNullNo      = 7
)

// probeAddr is an interface, and it is one because the value cases 2 and 3 are
// about cannot be spelled any other way: the zero value of an interface is a nil
// interface, and a codec registered for an interface type is where the wrapper's
// two defects lived.
//
// It carries a method rather than being empty, because `any` is what ferry
// refuses a root for and an empty interface as a field type would be a different
// argument in the same case.
type probeAddr interface {
	// Network is the one method, named after net.Addr, which is ADR-0009's own
	// worked example of a registered interface codec.
	Network() string
}

// probeUDP is a non-nil probeAddr, which is what the decode half returns for
// anything that is not a Null.
type probeUDP struct{}

// Network makes probeUDP a probeAddr.
func (probeUDP) Network() string { return "udp" }

// ifaceHolder is the struct a nil interface travels in.
type ifaceHolder struct {
	Value probeAddr `ferry:"value"`
}

// probeAddrPath is where an [ifaceHolder]'s one field lands.
var probeAddrPath = ferry.At("value")

// ifaceCodec is a correct codec for an interface type: a nil is a Null in both
// directions, which is exactly the shape ADR-0009 measured passing the
// registration check while the wrapper around it panicked.
//
// It is a null policy over a string registration, which is how ADR-0017 spells
// "this type wants a null of its own": the policy's two halves are closed under
// isNull(load()), since load returns a nil probeAddr and isNull says a nil is
// the null.
func ifaceCodec() ferry.Codec {
	return ferry.NullValue(
		ferry.StringValue(
			func(a probeAddr) (string, error) { return a.Network(), nil },
			func(string) (probeAddr, error) { return probeUDP{}, nil },
		),
		func() (probeAddr, error) { return nil, nil },
		func(a probeAddr) bool { return a == nil },
	)
}

// The four types case 4 registers, one per kind a constructor names. Each is a
// distinct reflect.Type, because one registry holds one codec per type.
type (
	kindBool   bool
	kindNumber string
	kindString string
	kindBytes  string
)

// kindHolder is the struct case 4 dumps, one field per kind.
type kindHolder struct {
	B kindBool   `ferry:"b"`
	N kindNumber `ferry:"n"`
	S kindString `ferry:"s"`
	Y kindBytes  `ferry:"y"`
}

// kindCodecs is one registration per constructor, and kindWanted is the kind
// each one must land at. They are two halves of one table and are read together.
func kindCodecs() []ferry.Registration {
	return []ferry.Registration{
		ferry.BoolValue(
			func(k kindBool) (bool, error) { return bool(k), nil },
			func(b bool) (kindBool, error) { return kindBool(b), nil }),
		ferry.NumberValue(
			func(k kindNumber) (string, error) { return string(k), nil },
			func(s string) (kindNumber, error) { return kindNumber(s), nil }),
		ferry.StringValue(
			func(k kindString) (string, error) { return string(k), nil },
			func(s string) (kindString, error) { return kindString(s), nil }),
		ferry.BytesValue(
			func(k kindBytes) ([]byte, error) { return []byte(k), nil },
			func(b []byte) (kindBytes, error) { return kindBytes(b), nil }),
	}
}

// kindWanted is the address each of [kindCodecs]' types lands at, and the kind
// the constructor that built it names.
var kindWanted = map[string]ferry.VKind{
	"b": ferry.KindBool,
	"n": ferry.KindNumber,
	"s": ferry.KindString,
	"y": ferry.KindBytes,
}

// nullable is the type case 7 registers, and its zero is the value its policy
// calls the null.
type nullable string

// nullHolder is the struct a nullable travels in.
type nullHolder struct {
	Value nullable `ferry:"value"`
}

// probeNullPath is where a [nullHolder]'s one field lands.
var probeNullPath = ferry.At("value")

// probeNullable is a nullable that is not the null, so that case 7 sees both
// arms of the policy rather than the null one twice.
const probeNullable nullable = "warn"

// nullCodec is a null policy whose two halves are closed: load returns the empty
// nullable and isNull says the empty nullable is the null.
func nullCodec() ferry.Codec {
	return ferry.NullValue(
		ferry.StringValue(
			func(n nullable) (string, error) { return string(n), nil },
			func(s string) (nullable, error) { return nullable(s), nil },
		),
		func() (nullable, error) { return "", nil },
		func(n nullable) bool { return n == "" },
	)
}

// probeNumber is the one number the probes carry, and it is a string because
// that is what a [ferry.Value] holds.
const probeNumber = "42"

// numeric is the type whose codec declares Number, which is the case core's
// donation rule exists for: a codec whose text is a run of digits must say
// Number or it works on env and fails on YAML.
type numeric string

// numberHolder is the struct a numeric value travels in.
type numberHolder struct {
	Value numeric `ferry:"value"`
}

// probeNumberPath is where a [numberHolder]'s one field lands.
var probeNumberPath = ferry.At("value")

// errNotANumber is what the number probe's decode half refuses a non-number
// with, which is the half of "accepts every kind it emits" that has to be able
// to fail.
var errNotANumber = errors.New("ferrytest: the probe codec takes a number")

// numberCodec is a codec declaring Number whose decode half reads a Number and
// nothing else, so that what it accepts is what core donated rather than what it
// was handed.
func numberCodec() ferry.Codec {
	return ferry.NumberValue(
		func(n numeric) (string, error) {
			if n == "" {
				return "0", nil
			}

			return string(n), nil
		},
		func(s string) (numeric, error) {
			if s == "" {
				return "", errNotANumber
			}

			return numeric(s), nil
		},
	)
}

// folding is the key type whose codec is not injective: its text folds case,
// where its own Go identity does not.
//
// It is the pair the whole of [Injective] rests on. A registrant checking their
// keys through the type's own spelling sees two distinct texts here, and ferry
// writes one text twice.
type folding string

// String is the type's own spelling, and it is injective: it is deliberately not
// what ferry uses, which is the disagreement [Injective] is written to survive.
func (f folding) String() string { return string(f) }

// The three spellings of one key: what a caller writes, what the codec below
// renders it to, and the second spelling that is a different key under == and
// the same address on the plane.
const (
	probeMixed   folding = "Ab"
	probeShouted folding = "AB"
	probeFolded          = "ab"
)

// foldingCodec is a key codec that lowercases, which makes two keys that are
// distinct under == render to one plane address.
func foldingCodec() ferry.KeyCodec {
	return ferry.StringKey(
		func(f folding) (string, error) { return strings.ToLower(string(f)), nil },
		func(s string) (folding, error) { return folding(s), nil },
	)
}
