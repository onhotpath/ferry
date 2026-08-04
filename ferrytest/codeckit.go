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
func ifaceCodec() ferry.Reg {
	return ferry.ValueCodec[probeAddr](ferry.KindString,
		func(a probeAddr) (ferry.Value, error) {
			if a == nil {
				return ferry.Null(), nil
			}

			return ferry.String(a.Network()), nil
		},
		func(v ferry.Value) (probeAddr, error) {
			if v.Kind() == ferry.KindNull {
				return nil, nil
			}

			return probeUDP{}, nil
		},
	)
}

// drifting is the type whose codec declares one kind and emits another at every
// value but its zero, which is the shape a registration-time check cannot see.
type drifting string

// driftHolder is the struct a drifting value travels in.
type driftHolder struct {
	Value drifting `ferry:"value"`
}

// driftingCodec declares String, emits String at the zero value so that
// registration accepts it, and emits Number everywhere else.
func driftingCodec() ferry.Reg {
	return ferry.ValueCodec[drifting](ferry.KindString,
		func(d drifting) (ferry.Value, error) {
			if d == "" {
				return ferry.String(""), nil
			}

			return ferry.Number(probeNumber), nil
		},
		func(v ferry.Value) (drifting, error) {
			s, err := v.AsString()

			return drifting(s), err
		},
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
func numberCodec() ferry.Reg {
	return ferry.ValueCodec[numeric](ferry.KindNumber,
		func(n numeric) (ferry.Value, error) {
			if n == "" {
				return ferry.Number("0"), nil
			}

			return ferry.Number(string(n)), nil
		},
		func(v ferry.Value) (numeric, error) {
			s, err := v.AsNumber()
			if err != nil {
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
func foldingCodec() ferry.Reg {
	return ferry.StringCodec[folding](
		func(f folding) string { return strings.ToLower(string(f)) },
		func(s string) (folding, error) { return folding(s), nil },
	)
}
