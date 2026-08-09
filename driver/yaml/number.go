package yaml

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	yamlv3 "go.yaml.in/yaml/v3"

	"github.com/onhotpath/ferry"
)

// numbers is this plane's spelling of a number, and it is where YAML's own
// numeric vocabulary is turned into the text a [ferry.Value] carries and back
// (#259, ADR-0018).
//
// It exists because core cannot hold it. Core parses a number with base-10
// parsers, which is right for a plane whose numbers are base ten and wrong for
// this one: YAML defines 0x1F, 0o17, 0b101, 1_000, .inf and .nan, the resolver
// tags all of them numeric, and a base-10 parser refuses every one. Going the
// other way, +Inf is what Go formats an infinity as and is not a float to any
// YAML reader, so a document ferry wrote could not be read back by anything
// else - which is the half of the defect a round trip through this driver alone
// could never see.
//
// The knowledge lives here rather than in core because a spelling is a property
// of a plane (ADR-0018), and the payload stays canonical because that is what
// every other driver's boundary carries.
var numbers = ferry.SpellingFunc(readNumber, writeNumber)

// The three floats Go spells with words and YAML spells with dots. They are the
// payload side, so they are Go's spellings: what a [ferry.Value] carries is
// what strconv produced (ADR-0018, law 2).
const (
	posInf     = "+Inf"
	negInf     = "-Inf"
	notANumber = "NaN"
)

// The YAML side of the same three, and what a save writes for them.
const (
	yamlPosInf = ".inf"
	yamlNegInf = "-.inf"
	yamlNaN    = ".nan"
)

// readNumber turns what the document holds into the canonical text a Number
// carries.
//
// The order is the resolver's own and is not a preference: YAML reads a leading
// zero as octal, so 017 is fifteen, and trying a float first would make it
// seventeen with nothing saying so. An integer that fits neither int64 nor
// uint64 falls through to the float arm and comes back with its digits
// untouched, because [ferry.Number] is entitled to carry a number no Go machine
// type wants and the accessor with a target type in hand is what decides.
func readNumber(text string) (string, error) {
	plain := strings.ReplaceAll(text, "_", "")

	if word, ok := floatWord(plain); ok {
		return word, nil
	}

	if integral(plain) {
		if n, err := strconv.ParseInt(plain, autoBase, numBits); err == nil {
			return signedText(plain, n), nil
		}

		if n, err := strconv.ParseUint(plain, autoBase, numBits); err == nil {
			return strconv.FormatUint(n, decimal), nil
		}
	}

	if float, ok := floatText(plain); ok {
		return float, nil
	}

	return "", notANumberSpelling(text)
}

// notANumberSpelling is the parse refusal, and it quotes the scalar.
//
// A spelling's parse refusal is the one message class ADR-0011's rule against
// naming a plane-supplied value does not cover: what the operator has to fix is
// the text itself, and a message that names the address alone cannot say
// whether the scalar is a typo, a stray unit or a value that wanted quoting.
// [quoted] is what keeps the exception an exception, bounding the length and
// escaping the content to one line (ADR-0018 law 4, ADR-0011).
func notANumberSpelling(text string) error {
	return fmt.Errorf("%w: the document tags this scalar as a number and %s is spelled in none of the forms "+
		"YAML gives a number: quote it to load it as text, or correct it", ferry.ErrValue, quoted(text))
}

// autoBase is strconv's base 0, which is YAML's own set of prefixes: 0x, 0o,
// 0b and a leading zero for octal. decimal is what a Number is spelled in once
// it has crossed the boundary, and numBits is the widest machine type ferry
// parses into.
const (
	autoBase = 0
	decimal  = 10
	numBits  = 64
)

// signedText keeps a negative zero negative.
//
// It matters because a float64 negative zero is formatted -0, which reads as an
// integer here, and formatting the parsed int back would write 0: the value
// would come back as a positive zero and the two are distinguishable in Go.
func signedText(plain string, n int64) string {
	if n == 0 && strings.HasPrefix(plain, "-") {
		return plain
	}

	return strconv.FormatInt(n, decimal)
}

// floatText validates a float spelling and hands the text through unchanged,
// which is what keeps 1.50 and 1e-45 spelled as the document spelled them: the
// payload is the plane's own text, and reformatting it here would round a value
// no stage in the middle is entitled to round.
//
// A magnitude too large for a float64 is passed through for [readNumber]'s
// reason rather than refused, so a codec reading the digits still sees them.
func floatText(plain string) (string, bool) {
	_, err := strconv.ParseFloat(plain, numBits)

	return plain, err == nil || errors.Is(err, strconv.ErrRange)
}

// quoteCap is how much of a scalar a refusal may quote, in bytes, and [quoted]
// is what applies it.
//
// The two are duplicated in driver/env rather than shared, and the same
// reasoning picks the number: a mistyped number is far inside 64 bytes, and
// every credential shape that must never appear in a log is at or over it. The
// rule of three is ADR-0018's trigger for a common home, and two callers is not
// three.
const quoteCap = 64

// quoted renders a scalar for a refusal: escaped to one line, and cut to
// [quoteCap].
//
// The escaping is what bounds the message in its second dimension. A YAML
// scalar may be folded over many lines and may carry control bytes, so a
// refusal that interpolated it raw would print a paragraph where a line was
// promised (ADR-0018 law 4).
func quoted(text string) string {
	if len(text) <= quoteCap {
		return strconv.Quote(text)
	}

	cut := quoteCap
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}

	return strconv.Quote(text[:cut]) + " (truncated)"
}

// floatWord reads YAML's three worded floats, in any of the cases the spec
// allows, and answers with Go's spelling of the same value.
func floatWord(plain string) (string, bool) {
	switch strings.ToLower(plain) {
	case yamlPosInf, "+" + yamlPosInf:
		return posInf, true
	case yamlNegInf:
		return negInf, true
	case yamlNaN:
		return notANumber, true
	default:
		return "", false
	}
}

// writeNumber is the canonical-out half: the text a Number carries, spelled the
// way this plane spells it.
//
// Only the three worded floats move, and they are the whole of #259's outbound
// half: Go writes +Inf, -Inf and NaN, and yaml.v3's own reader refuses all
// three under !!float. Everything else a core leaf produces is decimal text
// that YAML already reads as the number it is, so it is written as it stands.
//
// The refusal is for a payload that is no number at all, which only a codec of
// somebody's own can produce. Writing it would put a scalar under !!int or
// !!float that no reader takes, so it is refused before the file is touched
// rather than written and discovered on the next load - the same answer
// [spellString] gives a string this plane cannot hold.
func writeNumber(text string) (string, error) {
	switch text {
	case posInf:
		return yamlPosInf, nil
	case negInf:
		return yamlNegInf, nil
	case notANumber:
		return yamlNaN, nil
	}

	if _, err := readNumber(text); err != nil {
		return "", fmt.Errorf("%w: this value is a number and is spelled in none of the forms YAML gives a "+
			"number, so no reader would take it back: spell it the way its own kind does", ferry.ErrValue)
	}

	return text, nil
}

// carrySpelling keeps the operator's own spelling of a number where the value
// being written over it is the same number.
//
// It is the rule ADR-0016 states for a reference and ADR-0018 for a spelling,
// in the one place this driver can apply it without state: what the plane said
// is preserved until the value says otherwise. A document holding rate: 0x1F,
// loaded and dumped back with the field untouched, keeps 0x1F; one whose field
// changed gets the new value in the canonical spelling, because there is
// nothing left to preserve.
//
// The comparison is between payloads and not between texts, so 1_000 survives a
// dump of the same thousand and does not survive a dump of anything else.
func carrySpelling(at, spelled *yamlv3.Node) {
	if at.Kind != yamlv3.ScalarNode || kindOf(at.Tag) != ferry.KindNumber || kindOf(spelled.Tag) != ferry.KindNumber {
		return
	}

	was, wasErr := readNumber(at.Value)

	now, nowErr := readNumber(spelled.Value)
	if wasErr != nil || nowErr != nil || was != now {
		return
	}

	spelled.Value = at.Value
}

// integral reports whether text could be an integer literal under strconv's
// base 0, which is the two integer parsers' precondition and nothing more.
//
// It exists because the order the parsers are tried in is the resolver's own
// and cannot be changed: a leading zero is octal, so the integer arms have to
// run before the float one. That leaves every float in the document being
// refused twice before it reaches the arm that takes it, and a strconv refusal
// is a heap-allocated *strconv.NumError this function is about to discard.
//
// It answers "could be" and never "is". A text this admits may still fail both
// parsers - it is a range check, not a grammar - and the arms below it decide.
// What it excludes is only what no integer literal can hold.
func integral(text string) bool {
	text = strings.TrimLeft(text, "+-")

	// Hexadecimal spells e and E as digits, so only a radix point or a binary
	// exponent makes one a float there.
	if len(text) > 1 && text[0] == '0' && (text[1] == 'x' || text[1] == 'X') {
		return !strings.ContainsAny(text, ".pP")
	}

	return !strings.ContainsAny(text, ".eEpP")
}
