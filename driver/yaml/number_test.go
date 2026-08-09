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

// TestYAMLIntegerSpellingsReachTheIntegerArms pins the precondition the two
// integer parsers now sit behind.
//
// [readNumber] tries the integer arms before the float one because a leading
// zero is octal, and the guard in front of them decides only which texts can
// reach them at all. So the guard has to admit every spelling YAML gives an
// integer, and the two that would catch a wrong one out are here by name: a
// hexadecimal literal spells e and E as digits rather than as an exponent, and
// the largest unsigned is refused by ParseInt and taken by ParseUint, so a
// guard reading only the first arm's answer would drop it.
//
// A case that came back with the wrong text would be a value changing rather
// than an allocation moving, which is why this is a table of exact texts and
// not a round trip.
func TestYAMLIntegerSpellingsReachTheIntegerArms(t *testing.T) {
	for _, c := range []struct{ text, want string }{
		{"0x1F", "31"},
		{"0x1E", "30"},
		{"0XE", "14"},
		{"0o17", "15"},
		{"017", "15"},
		{"0b101", "5"},
		{"1_000", "1000"},
		{"-0x10", "-16"},
		{"18446744073709551615", "18446744073709551615"},
		{"-9223372036854775808", "-9223372036854775808"},
		{"-0", "-0"},
	} {
		t.Run(c.text, func(t *testing.T) { parsesTo(t, c.text, c.want) })
	}
}

// TestYAMLFloatSpellingsPassTheIntegerGuard is the other side of it: a text the
// guard sends straight to the float arm still parses, and still carries the
// document's own spelling rather than a reformatted one.
//
// The hexadecimal float is the case the guard's own hexadecimal arm exists for:
// there a p is an exponent where an e is a digit.
func TestYAMLFloatSpellingsPassTheIntegerGuard(t *testing.T) {
	for _, c := range []struct{ text, want string }{
		{"3.5", "3.5"},
		{"1e5", "1e5"},
		{"1E5", "1E5"},
		{"0x1p-2", "0x1p-2"},
		{"1_000.5", "1000.5"},
		{"1e309", "1e309"},
		{"99999999999999999999999999", "99999999999999999999999999"},
	} {
		t.Run(c.text, func(t *testing.T) { parsesTo(t, c.text, c.want) })
	}
}

// parsesTo is one spelling and the canonical text it carries, lifted out of the
// two tables above so that each stays a table.
func parsesTo(t *testing.T, text, want string) {
	t.Helper()

	got, err := yaml.Numbers.Parse(text)
	if err != nil {
		t.Fatalf("Parse(%q): %v", text, err)
	}

	if got != want {
		t.Errorf("Parse(%q) is %q, want %q", text, got, want)
	}
}

// The invariant the two tables above sample, stated so that nothing can get
// past it.
//
// [readNumber] tries the two integer arms before the float one, and a guard in
// front of them decides which texts reach them at all. The guard is allowed to
// admit a text the arms then refuse - that costs a refusal nobody reads and the
// float arm answers - and it is never allowed to refuse one the arms would have
// taken. That direction is not a performance question: a text that skips the
// integer arms reaches ParseFloat, which reads 017 as seventeen where the
// integer arm reads the octal fifteen, so a guard with a hole in it changes a
// value and reports no error at all.
//
// So the property is one-directional. Wherever strconv's own base-0 integer
// parsers accept a text, this plane's spelling has to carry that integer's
// canonical decimal, and the guard is only correct for as long as that holds.
// It is asserted through the spelling rather than over the guard, so it stays
// true of whatever shape the guard is next written in, or of none.

// integerAlphabet is every byte that can appear in a base-0 integer or float
// literal: the digits, the letters the three radix prefixes and hexadecimal
// use, both exponent markers, the radix point, the sign and the separator.
//
// A character outside it cannot make either parser accept a text it would
// otherwise refuse, so a sweep over this alphabet covers the property.
const integerAlphabet = "0123456789aAbBcCeEfFoOxXpP._+-"

// wantInteger is what this plane's spelling must carry for a text strconv's
// base-0 integer parsers accept, and reports whether they accept it at all.
//
// It is strconv's answer rather than the driver's, which is the point: an
// oracle written from the code under test proves only that the code agrees with
// itself.
func wantInteger(text string) (string, bool) {
	// The spelling strips the separator before it parses, so the oracle reads
	// the same text the arms are handed.
	plain := strings.ReplaceAll(text, "_", "")

	if n, err := strconv.ParseInt(plain, 0, 64); err == nil {
		// A negative zero stays negative: the two are distinguishable in Go and
		// formatting the parsed int back would lose the sign.
		if n == 0 && strings.HasPrefix(plain, "-") {
			return plain, true
		}

		return strconv.FormatInt(n, 10), true
	}

	if n, err := strconv.ParseUint(plain, 0, 64); err == nil {
		return strconv.FormatUint(n, 10), true
	}

	return "", false
}

// checkInteger is the property for one text, and does nothing for a text the
// integer parsers do not take.
func checkInteger(t *testing.T, text string) {
	t.Helper()

	want, ok := wantInteger(text)
	if !ok {
		return
	}

	got, err := yaml.Numbers.Parse(text)
	if err != nil {
		t.Fatalf("Parse(%q) refused with %v, and strconv reads it as the integer %s", text, err, want)
	}

	if got != want {
		t.Fatalf("Parse(%q) is %q, and strconv reads it as the integer %s", text, got, want)
	}
}

// TestNumberSpellingTakesEveryShortIntegerLiteral sweeps the property over
// every text up to four bytes from [integerAlphabet], which is every radix
// prefix, every sign, both exponent markers and the radix point in every
// arrangement they fit into.
//
// It is a test and not only a seed for the fuzz target below because `go test`
// runs a fuzz target's corpus and nothing more: a property nobody fuzzes is a
// property nobody checks, and this is the half that runs on every commit.
func TestNumberSpellingTakesEveryShortIntegerLiteral(t *testing.T) {
	sweep(t, "", 4)
}

// sweep walks every text up to n bytes over the alphabet, checking each.
func sweep(t *testing.T, prefix string, n int) {
	t.Helper()

	checkInteger(t, prefix)

	if n == 0 {
		return
	}

	for _, c := range integerAlphabet {
		sweep(t, prefix+string(c), n-1)
	}
}

// FuzzNumberSpellingTakesEveryIntegerLiteral is the same property with no
// bound on the length, for the arrangements a four-byte sweep cannot reach: a
// prefix and a separator and an exponent-looking digit in one text, and the
// widths' own bounds, which are twenty digits long.
//
// The seeds are the cases that would catch a wrong guard out. 0x1E and 0XE
// spell e and E as hexadecimal digits where they are exponents everywhere
// else; 0x1p-2 is the hexadecimal float where p is the exponent that e is not;
// 017 is the octal a float parser reads as seventeen; and the largest unsigned
// is refused by ParseInt and taken by ParseUint, so a guard that read only the
// first arm's answer would drop it.
func FuzzNumberSpellingTakesEveryIntegerLiteral(f *testing.F) {
	for _, s := range []string{
		"0", "-0", "+0", "017", "0o17", "0O17", "0x1F", "0x1E", "0XE", "0b101", "0B1",
		"1_000", "0x_1F", "-0x10", "18446744073709551615", "-9223372036854775808",
		"9223372036854775807", "3.5", "1e5", "1E5", "0x1p-2", "1_000.5", ".inf", ".nan",
		"", "0x", "0b", "0o", "_", "-", "+", "e", "0e0", "00", "0_0",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, text string) { checkInteger(t, text) })
}
