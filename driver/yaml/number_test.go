package yaml_test

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/yaml"
	"github.com/onhotpath/ferry/ferrytest"
)

// numeric is the destination the spelling cases load into: one field per
// document below, so a case is a document and a value rather than a struct.
type numeric struct {
	N float64 `ferry:"n"`
}

// TestYAMLNumberSpellingsLoad is the inbound half of #259: every numeric
// spelling YAML defines arrives as the canonical text a leaf's own parser
// reads, so a document written by hand loads.
func TestYAMLNumberSpellingsLoad(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want float64
	}{
		{"hexadecimal", "n: 0x1F\n", 31},
		{"octal, YAML 1.2", "n: 0o17\n", 15},
		{"octal, a leading zero", "n: 017\n", 15},
		{"binary", "n: 0b101\n", 5},
		{"digits grouped by underscore", "n: 1_000\n", 1000},
		{"a negative in another base", "n: -0x10\n", -16},
		{"the largest unsigned", "n: 18446744073709551615\n", 18446744073709551615},
		{"a float grouped by underscore", "n: 1_000.5\n", 1000.5},
		{"infinity", "n: .inf\n", math.Inf(1)},
		{"infinity, signed", "n: +.INF\n", math.Inf(1)},
		{"infinity, negative", "n: -.Inf\n", math.Inf(-1)},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { loads(t, c.doc, c.want) })
	}
}

// loads is one spelling, lifted out of its table so that the table stays a
// table: a subtest body counts against the enclosing function's complexity.
func loads(t *testing.T, doc string, want float64) {
	t.Helper()

	got, err := ferry.Load[numeric](t.Context(), yaml.NewSource(write(t, doc)))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if got.N != want {
		t.Errorf("loaded %v, want %v", got.N, want)
	}
}

// TestYAMLNotANumberLoads is the twelfth case of the table above, kept apart
// because NaN is not equal to itself.
func TestYAMLNotANumberLoads(t *testing.T) {
	got, err := ferry.Load[numeric](t.Context(), yaml.NewSource(write(t, "n: .nan\n")))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !math.IsNaN(got.N) {
		t.Errorf("loaded %v, want NaN", got.N)
	}
}

// TestYAMLRefusesATaggedNonNumber is law 4 through the driver: a scalar the
// operator tagged numeric and spelled as nothing numeric is a refusal naming
// the address, and never a zero in the field.
func TestYAMLRefusesATaggedNonNumber(t *testing.T) {
	_, err := ferry.Load[numeric](t.Context(), yaml.NewSource(write(t, "n: !!float zzz\n")))
	if !errors.Is(err, ferry.ErrValue) {
		t.Fatalf("load failed with %v, want an error carrying ferry.ErrValue", err)
	}

	for _, s := range ferrytest.DiffErrors(err, ferrytest.Want{Address: ferry.At("n"), Class: ferry.ErrValue}) {
		t.Errorf("load: %s", s)
	}
}

// TestYAMLWritesFloatsEveryReaderTakes is the outbound half of #259: the three
// floats Go spells with words are written in YAML's own spelling, so a document
// ferry wrote is a document yaml.v3 reads.
func TestYAMLWritesFloatsEveryReaderTakes(t *testing.T) {
	type doc struct {
		Up   float64 `ferry:"up"`
		Down float64 `ferry:"down"`
		None float64 `ferry:"none"`
	}

	path := write(t, "")

	v := doc{Up: math.Inf(1), Down: math.Inf(-1), None: math.NaN()}
	if err := ferry.Dump(t.Context(), v, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "up: .inf\ndown: -.inf\nnone: .nan\n"; got != want {
		t.Errorf("the plane holds %q, want %q", got, want)
	}

	back, err := ferry.Load[doc](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !math.IsInf(back.Up, 1) || !math.IsInf(back.Down, -1) || !math.IsNaN(back.None) {
		t.Errorf("read back %#v, want the two infinities and a NaN", back)
	}
}

// TestYAMLKeepsAnOperatorsNumberSpelling is the preserving half: a value the
// dump did not change keeps the spelling the operator wrote, and one it did
// change is written canonically because there is nothing left to preserve.
func TestYAMLKeepsAnOperatorsNumberSpelling(t *testing.T) {
	type doc struct {
		Mask int `ferry:"mask"`
		Rate int `ferry:"rate"`
	}

	path := write(t, "mask: 0x1F\nrate: 1_000\n")

	if err := ferry.Dump(t.Context(), doc{Mask: 31, Rate: 2000}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "mask: 0x1F\nrate: 2000\n"; got != want {
		t.Errorf("the plane holds %q, want %q", got, want)
	}
}

// TestYAMLRefusesANumberItCannotSpell is the outbound refusal: a codec of
// somebody's own may spell a number any way it likes, and one this plane cannot
// write is refused before the file is touched rather than written as a scalar
// no reader takes.
func TestYAMLRefusesANumberItCannotSpell(t *testing.T) {
	type odd int

	type doc struct {
		N odd `ferry:"n"`
	}

	reg := ferry.MustRegistry(ferry.NumberValue[odd](
		func(odd) (string, error) { return "zzz", nil },
		func(string) (odd, error) { return 0, nil },
	))

	path := write(t, "n: 1\n")

	err := ferry.Dump(t.Context(), doc{}, yaml.NewSink(path), ferry.WithRegistry(reg))
	for _, s := range ferrytest.DiffErrors(err, ferrytest.Want{Address: ferry.At("n"), Class: ferry.ErrValue}) {
		t.Errorf("dump: %s", s)
	}

	if got, want := read(t, path), "n: 1\n"; got != want {
		t.Errorf("the plane holds %q, want the document it held before the refusal, %q", got, want)
	}
}

// TestYAMLNumberRefusalQuotesTheScalar is law 4 as it was scoped: a spelling's
// parse refusal says which text it refused, because the address alone cannot
// say whether the scalar is a typo, a stray unit or a value that wanted
// quoting.
func TestYAMLNumberRefusalQuotesTheScalar(t *testing.T) {
	for name, text := range map[string]string{
		"a word":         "zzz",
		"a stray unit":   "30s",
		"a folded value": "1\n2",
	} {
		t.Run(name, func(t *testing.T) { checkQuoted(t, numberRefusal(t, text), text) })
	}
}

// checkQuoted is the two things a quoting refusal must be, lifted out so that
// the table above stays a table.
func checkQuoted(t *testing.T, got, text string) {
	t.Helper()

	if !strings.Contains(got, strconv.Quote(text)) {
		t.Errorf("refused with %q, want it to quote the scalar", got)
	}

	if strings.Contains(got, "\n") {
		t.Errorf("refused with %q, want one line", got)
	}
}

// TestYAMLNumberRefusalIsBounded is the other half: the quoting is capped, so a
// scalar holding a certificate or a token cannot reach a log through a refusal.
func TestYAMLNumberRefusalIsBounded(t *testing.T) {
	for name, text := range map[string]string{
		"a long single-byte scalar": strings.Repeat("x", 300),
		"a long multi-byte scalar":  strings.Repeat("€", 30),
	} {
		t.Run(name, func(t *testing.T) { checkCut(t, numberRefusal(t, text), text) })
	}
}

// checkCut is the same, for the bound rather than the quoting.
func checkCut(t *testing.T, got, text string) {
	t.Helper()

	if strings.Contains(got, text) {
		t.Errorf("refused with %q, which carries the whole scalar", got)
	}

	if !strings.Contains(got, "(truncated)") {
		t.Errorf("refused with %q, want it to say the scalar was cut", got)
	}
}

// numberRefusal is one refusal's message, taken from the spelling itself: what
// a load reports is an aggregate whose wording is not API, and what this
// asserts is the spelling's own statement.
func numberRefusal(t *testing.T, text string) string {
	t.Helper()

	_, err := yaml.Numbers.Parse(text)
	if err == nil {
		t.Fatalf("Parse(%q) was accepted, want a refusal", text)
	}

	return err.Error()
}

// TestYAMLNumberSpellingObeysTheLaws runs the published proof over this plane's
// own spelling, which is what holds the two halves to each other rather than to
// this driver's tests.
//
// The payloads are what core's numeric leaves produce, which is the domain the
// laws quantify over: canonical decimal, the widths' bounds, the float
// spellings strconv writes, and a negative zero.
func TestYAMLNumberSpellingObeysTheLaws(t *testing.T) {
	ferrytest.Spelling(t, yaml.Numbers, ferrytest.Eq[string],
		[]string{
			"0", "-0", "31", "-16", "1000", "18446744073709551615", "-9223372036854775808",
			"3.5", "1e-45", "0.1", "+Inf", "-Inf", "NaN",
		},
		[]string{"zzz", "", "0x", "true", "1.2.3"},
	)
}
