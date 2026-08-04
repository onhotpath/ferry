// Package main is the #137 probe: what ferrytest.Codec can and cannot reach.
package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/onhotpath/ferry"
)

// Meters is a named float64, so it is not in core's own set and is registrable.
type Meters float64

// Feet and Yards are two more registrable types, used only to probe the freeze.
type (
	Feet  float64
	Yards float64
)

// counts is how many times each half of the registrant's own codec ran.
type counts struct {
	encode int
	decode int
}

func (c counts) String() string { return fmt.Sprintf("encode=%d decode=%d", c.encode, c.decode) }

// countingCodec is a correct codec for Meters that records every call.
func countingCodec(c *counts) ferry.Reg {
	return ferry.StringCodec(
		func(m Meters) string {
			c.encode++

			return strconv.FormatFloat(float64(m), 'g', -1, 64)
		},
		func(s string) (Meters, error) {
			c.decode++
			f, err := strconv.ParseFloat(s, 64)

			return Meters(f), err
		},
	)
}

// lossyMeters is the decisive codec: genuinely wrong, and it survives
// registration because the zero value round-trips exactly.
//
// 0 formats to "0.00" and parses back to 0, so Register's totality check is
// satisfied. Every value needing more than two decimals is silently truncated.
func lossyMeters() ferry.Reg {
	return ferry.StringCodec(
		func(m Meters) string { return fmt.Sprintf("%.2f", float64(m)) },
		func(s string) (Meters, error) {
			f, err := strconv.ParseFloat(s, 64)

			return Meters(f), err
		},
	)
}

// constMeters always writes the same text, and the zero value happens to be
// that text, so it too survives registration.
func constMeters() ferry.Reg {
	return ferry.StringCodec(
		func(Meters) string { return "0.00" },
		func(s string) (Meters, error) {
			f, err := strconv.ParseFloat(s, 64)

			return Meters(f), err
		},
	)
}

// Digits is a named string whose text is always a run of digits, which is the
// "declares the wrong kind and never drifts off it" shape.
type Digits string

// digitsAsString declares String for a value that every structured plane will
// report as a Number, and is consistent about it, so nothing core checks fires.
func digitsAsString() ferry.Reg {
	return ferry.StringCodec(
		func(d Digits) string { return string(d) },
		func(s string) (Digits, error) { return Digits(s), nil },
	)
}

// Drift is the type whose codec declares one kind and emits another away from
// its zero value.
type Drift string

// driftingCodec declares String, emits String at the zero value so registration
// accepts it, and emits Number everywhere else.
func driftingCodec() ferry.Reg {
	return ferry.ValueCodec[Drift](ferry.KindString,
		func(d Drift) (ferry.Value, error) {
			if d == "" {
				return ferry.String(""), nil
			}

			return ferry.Number("42"), nil
		},
		func(v ferry.Value) (Drift, error) {
			s, err := v.AsString()

			return Drift(s), err
		},
	)
}

// Folding is a key type whose registered text folds case where its Go identity
// does not.
type Folding string

// foldingKey is a non-injective key codec that declares itself injective.
func foldingKey() ferry.Reg {
	return ferry.StringCodec(
		func(f Folding) string { return strings.ToLower(string(f)) },
		func(s string) (Folding, error) { return Folding(s), nil },
	).AsMapKey()
}

// Addr is a registrable interface type, which is the shape Codec's cases 2 and
// 3 are about.
type Addr interface {
	Network() string
}

// udp is a non-nil Addr.
type udp struct{}

func (udp) Network() string { return "udp" }

// nilHostileAddr is an interface codec whose encode half dereferences the nil
// interface, which is the defect cases 2 and 3 exist for, written by a
// registrant rather than by core.
func nilHostileAddr() ferry.Reg {
	return ferry.ValueCodec[Addr](ferry.KindString,
		func(a Addr) (ferry.Value, error) { return ferry.String(a.Network()), nil },
		func(v ferry.Value) (Addr, error) {
			if v.Kind() == ferry.KindNull {
				return nil, nil
			}

			return udp{}, nil
		},
	)
}

// Disagree carries both spellings of the text pair and they disagree, which is
// the one defect Codec case 1 catches.
type Disagree struct{ n int }

// AppendText is the spelling ferry prefers.
func (d Disagree) AppendText(b []byte) ([]byte, error) {
	return append(b, []byte("append:"+strconv.Itoa(d.n))...), nil
}

// MarshalText is the spelling ferry does not call when both are present.
func (d Disagree) MarshalText() ([]byte, error) {
	return []byte("marshal:" + strconv.Itoa(d.n)), nil
}

// UnmarshalText makes the pair complete, on the pointer receiver.
func (d *Disagree) UnmarshalText(b []byte) error {
	_, rest, ok := strings.Cut(string(b), ":")
	if !ok {
		return errNoColon
	}

	n, err := strconv.Atoi(rest)
	d.n = n

	return err
}

var errNoColon = errors.New("proto137: not a Disagree text")

// Holder is the struct a single value travels in, since a root that compiles to
// a leaf is refused.
type Holder[T any] struct {
	Value T `ferry:"value"`
}

// Keyed is the struct a map travels in.
type Keyed[T comparable] struct {
	Map map[T]string `ferry:"m"`
}
