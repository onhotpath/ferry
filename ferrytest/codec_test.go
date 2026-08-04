package ferrytest_test

import (
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestCodecIsGreenOverARegistryThatIsRight is the suite's own bar, and it is
// also the assertion that cases 2 to 6 pass on this build of core.
//
// Cases 2 and 3 are the two wrapper defects ADR-0009 found, and both were
// panics: a suite that reported them as failures rather than crashing is what
// this run demonstrates when they are absent, and TestCodecReportsAPanic is
// where the reporting itself is asserted.
func TestCodecIsGreenOverARegistryThatIsRight(t *testing.T) {
	reg := ferry.NewRegistry()
	if err := reg.Register(ferry.TextCodec[agreeingText](ferry.KindString)); err != nil {
		t.Fatalf("registering the probe: %v", err)
	}

	c := &capture{}

	ferrytest.Codec(c, reg)

	if len(c.lines) != 0 {
		t.Errorf("the codec suite reported %q over a registry that is right, want nothing", c.lines)
	}
}

// TestCodecIsGreenOverAnEmptyRegistry is the registrant who has registered
// nothing yet, and the nil registry a program that registered nothing hands over.
//
// Case 1 reads the caller's types and has none to read; the other five run
// against this package's own probes and are unaffected, which is the whole point
// of where they get their types from.
func TestCodecIsGreenOverAnEmptyRegistry(t *testing.T) {
	for name, reg := range map[string]*ferry.Registry{"empty": ferry.NewRegistry(), "nil": nil} {
		t.Run(name, func(t *testing.T) {
			c := &capture{}

			ferrytest.Codec(c, reg)

			if len(c.lines) != 0 {
				t.Errorf("the codec suite reported %q, want nothing", c.lines)
			}
		})
	}
}

// TestCodecReportsATextPairThatDisagrees is case 1, negative, and it is broken
// in exactly one way.
//
// ferry prefers AppendText, which costs one allocation rather than two, and
// preferring one spelling inherits an obligation the compiler cannot check. The
// standard library implements one in terms of the other and so agrees by
// construction; this type does not, so what the plane holds is the appender's
// bytes and a reader expecting the marshaller's is reading somebody else's.
func TestCodecReportsATextPairThatDisagrees(t *testing.T) {
	reg := ferry.NewRegistry()
	if err := reg.Register(ferry.TextCodec[disagreeingText](ferry.KindString)); err != nil {
		t.Fatalf("registering the probe: %v", err)
	}

	c := &capture{}

	ferrytest.Codec(c, reg)

	only := onlyLine(t, c)
	if !strings.Contains(only, "codec case 1") {
		t.Errorf("report = %q, want case 1 and only case 1", only)
	}

	if !strings.Contains(only, "AppendText wrote") {
		t.Errorf("report = %q, want the two spellings named", only)
	}
}

// TestCodecIsSilentWhereFerryNeverConsultsTheTextPair is #143.
//
// The type's two spellings disagree and the registration is a StringCodec, so
// registration beats the text pair and ferry calls neither half. Reporting the
// disagreement would be a false positive whose explanation is false with it: the
// plane holds neither of the two strings.
func TestCodecIsSilentWhereFerryNeverConsultsTheTextPair(t *testing.T) {
	reg := ferry.NewRegistry()

	err := reg.Register(ferry.StringCodec[disagreeingText](
		func(d disagreeingText) string { return d.text },
		func(s string) (disagreeingText, error) { return disagreeingText{text: s}, nil },
	))
	if err != nil {
		t.Fatalf("registering the probe: %v", err)
	}

	c := &capture{}

	ferrytest.Codec(c, reg)

	if len(c.lines) != 0 {
		t.Errorf("the codec suite reported %q about two methods ferry never calls for this type", c.lines)
	}
}

// TestCodecReportsAZeroThatIsNotAFixedPoint is case 3, per-registrant.
//
// The codec is total over the zero value, which is all registration checks, and
// its two halves still disagree there: the zero encodes to one text and what
// that text loads as encodes to another. Nothing but a walk over the
// registrant's own type can see it, and no proof is needed to reach it.
func TestCodecReportsAZeroThatIsNotAFixedPoint(t *testing.T) {
	reg := ferry.NewRegistry()

	err := reg.Register(ferry.StringCodec[wandering](
		func(w wandering) string {
			if w == "" {
				return "zero"
			}

			return "x:" + string(w)
		},
		func(s string) (wandering, error) { return wandering(s), nil },
	))
	if err != nil {
		t.Fatalf("registering the probe: %v", err)
	}

	c := &capture{}

	ferrytest.Codec(c, reg)

	only := onlyLine(t, c)
	if !strings.Contains(only, "codec case 3") {
		t.Errorf("report = %q, want case 3 and only case 3", only)
	}

	if !strings.Contains(only, "disagree at the one value they are both guaranteed to see") {
		t.Errorf("report = %q, want the two halves named", only)
	}
}

// wandering is a codec's type whose zero encoding is not a fixed point of the
// pair, and which registration accepts because neither half errors.
type wandering string

// TestCodecReportsThroughT is the third reason T exists, applied to this suite.
func TestCodecReportsThroughT(t *testing.T) {
	c := &capture{}

	ferrytest.Codec(c, ferry.NewRegistry())

	if c.helpers == 0 {
		t.Error("Codec never called Helper, so every failure it reports is attributed to a line in ferrytest")
	}
}

// TestCodecRefusesAnOptionListItCannotHonour is #110's shape at this signature,
// and the second half is this suite's own.
//
// A tag key names the key ferry reads for the probe structs too, and a registry
// supplied as an Option is a second registry beside the one each case needs -
// core resolves against exactly one. Both are reported once rather than as an
// identical failure in every case.
func TestCodecRefusesAnOptionListItCannotHonour(t *testing.T) {
	cases := map[string]ferry.Option{
		"a tag key":      ferry.TagKey("cfg"),
		"a registry too": ferry.WithRegistry(ferry.NewRegistry()),
	}

	for name, opt := range cases {
		t.Run(name, func(t *testing.T) {
			c := &capture{}

			ferrytest.Codec(c, ferry.NewRegistry(), opt)

			if only := onlyLine(t, c); !strings.Contains(only, "unable to compile the struct it dumps a probe in") {
				t.Errorf("report = %q, want the Option list named once", only)
			}
		})
	}
}

// agreeingText declares both halves of the text pair and one appender, in terms
// of each other, which is what every standard-library type carrying both does.
type agreeingText struct{ text string }

func (a agreeingText) MarshalText() ([]byte, error) { return []byte(a.text), nil }

func (a agreeingText) AppendText(b []byte) ([]byte, error) { return append(b, a.text...), nil }

func (a *agreeingText) UnmarshalText(b []byte) error {
	a.text = string(b)

	return nil
}

// disagreeingText is the same type with the obligation broken, which is the one
// thing case 1 exists to report.
type disagreeingText struct{ text string }

func (disagreeingText) MarshalText() ([]byte, error) { return []byte("marshalled"), nil }

func (disagreeingText) AppendText(b []byte) ([]byte, error) { return append(b, "appended"...), nil }

func (d *disagreeingText) UnmarshalText(b []byte) error {
	d.text = string(b)

	return nil
}
