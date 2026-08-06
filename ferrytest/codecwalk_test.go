package ferrytest_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// The failures the fixtures below stage, each one distinct so that a report can
// be traced back to the fixture that caused it.
var (
	errNoMarshalText = errors.New("this type has no MarshalText that works")
	errNoAppendText  = errors.New("this type has no AppendText that works")
	errNoTextAtAll   = errors.New("neither half of the text pair works")
	errWentAway      = errors.New("the codec stopped working after it was registered")
	errOnlyTheZero   = errors.New("the codec encodes its zero value and nothing else")
)

// TestCodecIsGreenOverACodecDeclaringAKindOtherThanString is case 5's
// per-registrant half, positive, and it is the only place core's donation is
// asserted over a type the suite was handed rather than one it declared.
//
// A codec declaring Number or Bytes writes that kind, and it has to load the
// same text back from a plane that spells it String, because a flat plane - env,
// a query string, Consul - reports String for everything. Both registrations
// here are right, so the suite reads the text out of what ferry wrote, hands it
// back spelled String, and has nothing to say.
func TestCodecIsGreenOverACodecDeclaringAKindOtherThanString(t *testing.T) {
	for name, g := range map[string]ferry.Codec{"number": carriedAsNumber(), "bytes": carriedAsBytes()} {
		t.Run(name, func(t *testing.T) {
			c := &capture{}

			ferrytest.Codec(c, registryWith(t, g))

			if len(c.lines) != 0 {
				t.Errorf("the codec suite reported %q over a codec that loads back what it wrote", c.lines)
			}
		})
	}
}

// TestCodecIsSilentWhereFerryWroteNoTextAtAll is the codec whose zero value is a
// Null, which is ADR-0009's own worked example and the one registered shape that
// writes no text.
//
// Case 5 asks whether the codec takes the same text back spelled String, and
// there is no text: a Null is the plane's own observation rather than something
// a codec chose, and donating String to it would be core answering a question
// the plane already answered (ADR-0006). Cases 2 and 3 still run and still pass,
// which is the half of this worth asserting - a nil interface is exactly where
// the wrapper's two defects lived.
func TestCodecIsSilentWhereFerryWroteNoTextAtAll(t *testing.T) {
	c := &capture{}

	ferrytest.Codec(c, registryWith(t, nullableCodec()))

	if len(c.lines) != 0 {
		t.Errorf("the codec suite reported %q about a zero value that encodes to a Null", c.lines)
	}
}

// TestCodecIsSilentAboutATextPairFerryNeverWrote is #143 from the two arms the
// disagreeing-marshaller test does not reach.
//
// Case 1 only speaks where ferry's own output for the type is the appender's
// bytes, and neither of these is: the first type's appender refuses its zero
// value, so a registration that wrote something cannot have gone through it, and
// the second type has no working text half at all. A report either way would
// name two byte strings the plane does not hold.
func TestCodecIsSilentAboutATextPairFerryNeverWrote(t *testing.T) {
	cases := map[string]ferry.Codec{
		"the appender refuses the zero value": ferry.StringValue(
			func(a appendRefusing) (string, error) { return a.text, nil },
			func(s string) (appendRefusing, error) { return appendRefusing{text: s}, nil },
		),
		"neither half works": ferry.StringValue(
			func(b bothRefusing) (string, error) { return b.text, nil },
			func(s string) (bothRefusing, error) { return bothRefusing{text: s}, nil },
		),
	}

	for name, g := range cases {
		t.Run(name, func(t *testing.T) {
			c := &capture{}

			ferrytest.Codec(c, registryWith(t, g))

			if len(c.lines) != 0 {
				t.Errorf("the codec suite reported %q about a text pair ferry never calls for this type", c.lines)
			}
		})
	}
}

// TestCodecReportsOneHalfOfTheTextPairFailing is case 1's other report, and the
// registration is a [ferry.StringText] so that the pair really is what ferry
// writes.
//
// ferry calls whichever spelling is present and prefers the appender, so a type
// whose marshaller refuses the value its appender wrote is half a working type:
// the plane holds bytes, and every reader that reaches for the other spelling
// gets an error instead.
func TestCodecReportsOneHalfOfTheTextPairFailing(t *testing.T) {
	c := &capture{}

	ferrytest.Codec(c, registryWith(t, ferry.StringText[marshalRefusing]()))

	only := onlyLine(t, c)
	if !strings.Contains(only, "codec case 1") {
		t.Errorf("report = %q, want case 1 and only case 1", only)
	}

	if !strings.Contains(only, "one half failing is one half of the type working") {
		t.Errorf("report = %q, want the asymmetry named", only)
	}
}

// TestCodecReportsAZeroThatStopsEncoding is case 2, per-registrant, and the
// second assertion is that case 1 declines to guess when it cannot see what
// ferry wrote.
//
// [ferry.NewRegistry] runs the codec against the zero value once, as the
// registry is built, so a codec whose halves are not functions of their argument
// alone - one reading a global, a layout installed later, a feature flag - is
// total at that moment and not afterwards. This one is switched off after it is
// registered,
// which is the only way a registered codec reaches the walk unable to write its
// own zero.
//
// The type's two text spellings disagree, and case 1 says nothing about it:
// #143's rule is that the pair is in play exactly when ferry's own output is the
// appender's bytes, and here there is no output to compare against.
func TestCodecReportsAZeroThatStopsEncoding(t *testing.T) {
	live := true

	reg := registryWith(t, brittleCodec(&live))
	live = false

	c := &capture{}

	ferrytest.Codec(c, reg)

	only := onlyLine(t, c)
	if !strings.Contains(only, "codec case 2") {
		t.Errorf("report = %q, want case 2 and only case 2", only)
	}

	if !strings.Contains(only, "dumping the zero value through the registered codec failed") {
		t.Errorf("report = %q, want the encode half named", only)
	}
}

// TestCodecReportsAZeroThatStopsDecoding is case 3, per-registrant, from the
// half registration cannot re-check either.
//
// What ferry wrote for the zero value is the one input a codec's decode half is
// guaranteed to see, and a codec that refuses it cannot round trip anything at
// all.
func TestCodecReportsAZeroThatStopsDecoding(t *testing.T) {
	live := true

	reg := registryWith(t, fickleCodec(&live))
	live = false

	c := &capture{}

	ferrytest.Codec(c, reg)

	only := onlyLine(t, c)
	if !strings.Contains(only, "codec case 3") {
		t.Errorf("report = %q, want case 3 and only case 3", only)
	}

	if !strings.Contains(only, "cannot read its own output") {
		t.Errorf("report = %q, want the decode half named", only)
	}
}

// TestCodecReportsAValueItsOwnDecodeProducedAndItsEncodeRefuses is the codec
// that is total over its zero value and not over the value that zero loads as.
//
// [ferry.NewRegistry] encodes the zero and decodes the result, and stops there.
// This codec passes that and still cannot make a second lap: what its own decode half
// produced is a value its encode half refuses, so the value ferry wrote can be
// read and never written again.
//
// Two cases report it, and both are honest. Case 3 is the round trip failing and
// case 5 is the same text spelled String failing the same way: the codec
// declares a kind other than String, so case 5 has a donation to check and
// reaches the same broken encode by the other route.
//
// It runs at both of the kinds that carry a text and are not String, and case
// 5's report is the assertion that the text was read out of what ferry wrote
// rather than out of a kind the suite assumed. A suite that could only read a
// String would be silent here for both.
func TestCodecReportsAValueItsOwnDecodeProducedAndItsEncodeRefuses(t *testing.T) {
	cases := map[string]ferry.Codec{"number": waywardAsNumber(), "bytes": waywardAsBytes()}

	for name, g := range cases {
		t.Run(name, func(t *testing.T) {
			c := &capture{}

			ferrytest.Codec(c, registryWith(t, g))

			reportsExactly(t, c,
				"codec case 3: ferrytest_test.wayward: re-encoding what",
				"codec case 5: ferrytest_test.wayward: the same text spelled String loaded as something "+
					"that encodes to",
			)
		})
	}
}

// carried is the type the two kinds that carry a text and are not String are
// registered over: Number, which is what a run of digits has to be written at
// if it is to load from a structured plane as well as a flat one, and Bytes.
//
// One type serves both because each case registers into a registry of its own,
// and a registration claims its type only within one registry.
type carried string

func carriedAsNumber() ferry.Codec {
	return ferry.NumberValue(
		func(c carried) (string, error) {
			if c == "" {
				return "0", nil
			}

			return string(c), nil
		},
		func(s string) (carried, error) { return carried(s), nil },
	)
}

func carriedAsBytes() ferry.Codec {
	return ferry.BytesValue(
		func(c carried) ([]byte, error) { return []byte(c), nil },
		func(b []byte) (carried, error) { return carried(b), nil },
	)
}

// nullable is an interface whose codec answers a nil with a Null, which is
// ADR-0009's mechanism for making an interface expressible and the zero value
// cases 2 and 3 exist for.
type nullable interface {
	// Zone is the one method, so that this is not the empty interface ferry
	// refuses a root for.
	Zone() string
}

// zoned is a non-nil nullable, which is what the decode half answers anything
// that is not a Null with.
type zoned struct{}

func (zoned) Zone() string { return "utc" }

func nullableCodec() ferry.Codec {
	return ferry.NullValue(
		ferry.StringValue(
			func(n nullable) (string, error) { return n.Zone(), nil },
			func(string) (nullable, error) { return zoned{}, nil },
		),
		func() (nullable, error) { return nil, nil },
		func(n nullable) bool { return n == nil },
	)
}

// appendRefusing carries both spellings of the encode half and only the second
// works, which is a disagreement ferry never sees because the registration below
// it goes through neither.
type appendRefusing struct{ text string }

func (appendRefusing) AppendText([]byte) ([]byte, error) { return nil, errNoAppendText }

func (a appendRefusing) MarshalText() ([]byte, error) { return []byte(a.text), nil }

// bothRefusing declares the whole encode half twice over and neither spelling
// works, which is the type's own business rather than a disagreement.
type bothRefusing struct{ text string }

func (bothRefusing) AppendText([]byte) ([]byte, error) { return nil, errNoTextAtAll }

func (bothRefusing) MarshalText() ([]byte, error) { return nil, errNoTextAtAll }

// marshalRefusing is the type ferry writes through its appender and whose
// marshaller refuses every value, which is half a working type rather than a
// broken one.
type marshalRefusing struct{ text string }

func (m marshalRefusing) AppendText(p []byte) ([]byte, error) { return append(p, m.text...), nil }

func (marshalRefusing) MarshalText() ([]byte, error) { return nil, errNoMarshalText }

func (m *marshalRefusing) UnmarshalText(p []byte) error {
	m.text = string(p)

	return nil
}

// brittle carries a text pair that disagrees and a codec that stops working
// after it is registered, which puts case 1's silence and case 2's report in one
// run.
type brittle struct{ text string }

func (brittle) AppendText(p []byte) ([]byte, error) { return append(p, "appended"...), nil }

func (brittle) MarshalText() ([]byte, error) { return []byte("marshalled"), nil }

func brittleCodec(live *bool) ferry.Codec {
	return ferry.StringValue(
		func(b brittle) (string, error) {
			if !*live {
				return "", errWentAway
			}

			return b.text, nil
		},
		func(s string) (brittle, error) { return brittle{text: s}, nil },
	)
}

// fickle is brittle's mirror: the decode half is the one that stops working.
type fickle string

func fickleCodec(live *bool) ferry.Codec {
	return ferry.StringValue(
		func(f fickle) (string, error) { return string(f), nil },
		func(s string) (fickle, error) {
			if !*live {
				return "", errWentAway
			}

			return fickle(s), nil
		},
	)
}

// wayward encodes its zero value and refuses every other, and its decode half
// answers that zero encoding with one of the values its encode half refuses.
//
// waywardText is what the zero encodes to, and it is not the zero, which is the
// whole of the defect: [ferry.NewRegistry] encodes the zero and decodes the
// result and stops there, so it never asks the codec to write what its own
// decode produced.
type wayward string

const waywardText = "1"

func waywardAsNumber() ferry.Codec {
	return ferry.NumberValue(
		func(w wayward) (string, error) {
			if w == "" {
				return waywardText, nil
			}

			return "", errOnlyTheZero
		},
		func(s string) (wayward, error) { return wayward(s), nil },
	)
}

func waywardAsBytes() ferry.Codec {
	return ferry.BytesValue(
		func(w wayward) ([]byte, error) {
			if w == "" {
				return []byte(waywardText), nil
			}

			return nil, errOnlyTheZero
		},
		func(b []byte) (wayward, error) { return wayward(b), nil },
	)
}
