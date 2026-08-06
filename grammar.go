package ferry

import (
	"errors"
	"fmt"
	"strings"
)

// The grammar, which is four words and one of them is punctuation (ADR-0008):
//
//	tag      =  name *( "," option )  /  "-"
//	name     =  token          ; and may not be empty
//	option   =  "required"  /  "omitzero"  /  "default" "=" token
//	token    =  bare  /  quoted
//	bare     =  *( any byte except "," , and not beginning with "'" )
//	quoted   =  "'" *( any byte except "'"  /  "''" ) "'"
//
// There is no escape character. Only a leading quote is significant, so an
// apostrophe inside a bare token is just an apostrophe and default=it's here
// needs no quoting. The design is encoding/json/v2's own, which states ferry's
// constraint independently - "both backtick and double quotes cannot be used
// verbatim in a struct tag" - and which does not actually offer it, because v2
// consumes a member name with quoting disallowed.
//
// The one divergence is the inner escape: v2 writes \' and ferry doubles the
// quote, SQL's convention, because a backslash in a struct tag value must be
// written \\ and a spelling one character short of that is invisible to Lookup.

const (
	comma   = ','
	quote   = '\''
	equals  = "="
	skipTag = "-"
)

// tag is a parsed tag value: what the field is called on the plane, and what it
// asks for there.
type tag struct {
	name     string
	skip     bool
	required bool
	omitzero bool
	def      string
	hasDef   bool
}

// parseTag decodes a tag value, reporting every fault it carries rather than
// the first. These are the first diagnostic tier - the tag is well formed or it
// is not - and a field that fails here is not asked anything below.
func parseTag(value, key string) (tag, []error) {
	fields := splitFields(value)

	t, err := parseName(fields[0], len(fields) > 1, key)

	var errs []error
	if err != nil {
		errs = append(errs, err)
	}

	if t.skip {
		return t, nil
	}

	// The options are read even where the name was not, because both are the
	// same tier and both are the user's mistake. That is what makes the
	// xload-shaped migration `,prefix=DB_` two loud errors rather than one.
	for _, raw := range fields[1:] {
		if e := t.addOption(raw, key); e != nil {
			errs = append(errs, e)
		}
	}

	return t, errs
}

// splitFields cuts a tag value at the commas that separate its name from its
// options.
//
// It is not a strings.Split, and that is the whole point of the quoted form: a
// comma inside a quoted token is text, so the cut is quote-aware. xload splits
// on the comma and cannot express env:"K,delimiter=," at all - reproduced, the
// delimiter arrives as the empty string.
func splitFields(v string) []string {
	out := make([]string, 0, fieldsHint)

	for i := 0; ; {
		end := fieldEnd(v, i)
		out = append(out, v[i:end])

		if end == len(v) {
			return out
		}

		i = end + 1
	}
}

// fieldsHint is the capacity guess: a name and two options covers every tag in
// every ADR measurement.
const fieldsHint = 3

// fieldEnd is the index of the comma that ends the field beginning at i, or the
// end of the value.
//
// A quote is significant only where a token starts, which is at the field's own
// start and after its first "=". That is what makes default=a=b the text a=b,
// and what leaves an apostrophe in the middle of a bare token alone.
func fieldEnd(s string, i int) int {
	tokenStart, seenEq := true, false

	for i < len(s) {
		switch {
		case s[i] == comma:
			return i
		case tokenStart && s[i] == quote:
			i, _ = quotedEnd(s, i)
			tokenStart = false
		case s[i] == '=' && !seenEq:
			i, seenEq, tokenStart = i+1, true, true
		default:
			i, tokenStart = i+1, false
		}
	}

	return i
}

// quotedEnd is one past the closing quote of the quoted token at i, and whether
// there was one. A doubled quote is a literal quote and does not close.
func quotedEnd(s string, i int) (int, bool) {
	for j := i + 1; j < len(s); {
		switch {
		case s[j] != quote:
			j++
		case j+1 < len(s) && s[j+1] == quote:
			j += 2
		default:
			return j + 1, true
		}
	}

	return len(s), false
}

// parseName decodes the name, which is mandatory: ferry never invents a segment
// name, because a Go field name is byte-exactly what the author wanted about
// one time in twenty, measured over 10,012 third-party files and the standard
// library (ADR-0008).
func parseName(raw string, hasOptions bool, key string) (tag, error) {
	switch {
	case raw == skipTag && !hasOptions:
		return tag{skip: true}, nil
	case raw == skipTag:
		return tag{}, fmt.Errorf(
			"%s names no segment: write %s:%q on its own to leave the field unmapped, "+
				"or %s:\"'-',...\" to name the segment %s", skipTag, key, skipTag, key, skipTag)
	case raw == "":
		// The empty tag value has to mean something, and "you left the name
		// out" is the more useful reading than "the segment is called nothing".
		return tag{}, fmt.Errorf(
			"the name is empty: every field must name the segment it addresses, or be marked %s:%q",
			key, skipTag)
	}

	if i := bareEqIndex(raw); i >= 0 {
		return tag{}, nameEqError(raw, i, key)
	}

	name, err := parseToken(raw, "name")
	if err != nil {
		return tag{}, err
	}

	// The emptiness check is asked again here, after the token is decoded,
	// because the quoted empty name is not empty raw text: '' reached this
	// point, produced "", and minted the empty Name segment ADR-0008 says twice
	// the grammar cannot write. Measured before the check existed, the schema
	// compiled clean and the yaml sink wrote a document whose first key was the
	// empty string, while the env source refused the same schema at Bind (#233).
	if name == "" {
		return tag{}, fmt.Errorf(
			"the name is empty: an empty segment names no address, so every field must name the segment it "+
				"addresses, or be marked %s:%q", key, skipTag)
	}

	return tag{name: name}, nil
}

// bareEqIndex is where a bare name contains "=", or -1. A quoted name may
// contain one, so 'a=b' is the segment a=b and needs no rule of its own.
func bareEqIndex(raw string) int {
	if raw[0] == quote {
		return -1
	}

	return strings.Index(raw, equals)
}

// nameEqError is the one place the grammar guesses at intent, and it guesses
// only where a bare name contains "=". That covers the xload-shaped migration
// mistake, env:",prefix=DB_", which reports as an empty name plus an unknown
// option rather than as a segment nobody meant.
func nameEqError(raw string, i int, key string) error {
	head := raw[:i]
	if !isOptionWord(head) {
		return fmt.Errorf(
			`a name may not contain "=": write %s:"'%s'" if the segment really is called that`, key, raw)
	}

	remedy := head
	if head == optDefault {
		remedy = raw
	}

	return fmt.Errorf(
		`a name may not contain "=", and %q looks like the %s option with no name in front of it: `+
			`write %s:"<name>,%s"`, raw, head, key, remedy)
}

// parseToken decodes a name or an option value: bare, or single-quoted with the
// quote doubled inside it.
func parseToken(raw, noun string) (string, error) {
	if raw == "" || raw[0] != quote {
		return raw, nil
	}

	end, ok := quotedEnd(raw, 0)

	switch {
	case !ok:
		return "", fmt.Errorf("%s %q is not terminated: a quoted %s ends at a single quote, "+
			"and a literal quote inside it is doubled", noun, raw, noun)
	case end != len(raw):
		return "", fmt.Errorf("%s %q has text after the closing quote", noun, raw)
	}

	return strings.ReplaceAll(raw[1:end-1], "''", "'"), nil
}

// addOption reads one option. Every word ferry does not recognise is refused,
// which is materially stricter than encoding/json/v2, where an unknown option
// is ignored and a source comment says that is not a promise.
func (t *tag) addOption(raw, key string) error {
	if raw == "" {
		return errors.New("empty option: two commas with nothing between them")
	}

	if err := checkOptionSpacing(raw); err != nil {
		return err
	}

	if raw[0] == quote {
		return fmt.Errorf("option %q is quoted, and an option is a word rather than a token: %s", raw, vocabulary)
	}

	head, value, hasValue := strings.Cut(raw, equals)

	switch head {
	case optRequired:
		return setFlag(&t.required, optRequired, hasValue)
	case optOmitzero:
		return setFlag(&t.omitzero, optOmitzero, hasValue)
	case optDefault:
		return t.setDefault(value, hasValue)
	default:
		return unknownOption(head, key)
	}
}

// checkOptionSpacing gives surrounding whitespace its own diagnosis rather than
// letting it read as an unknown option, because ferry does not trim and
// `h, required` is a mistake a reader's eye slides over.
//
// Only the option's own word is held to it. Whitespace after "default=" is part
// of the value, which is a token and is the user's text.
func checkOptionSpacing(raw string) error {
	head, _, _ := strings.Cut(raw, equals)

	if raw != strings.TrimLeft(raw, whitespace) || head != strings.TrimRight(head, whitespace) {
		return fmt.Errorf("option %q has surrounding whitespace: ferry does not trim, so %q and %q are "+
			"different words", raw, strings.TrimSpace(head), head)
	}

	return nil
}

// whitespace is what ferry does not trim.
const whitespace = " \t\r\n"

// setFlag records one of the two options that take no value.
func setFlag(flag *bool, word string, hasValue bool) error {
	switch {
	case hasValue:
		return fmt.Errorf("option %q takes no value", word)
	case *flag:
		return fmt.Errorf("option %q is given twice", word)
	}

	*flag = true

	return nil
}

// setDefault records a declared default as the text it is. The option's value
// is not optional, because ADR-0006 requires an empty default to be
// distinguishable from no default: default= is the empty string and the
// option's absence leaves the field alone.
func (t *tag) setDefault(value string, hasValue bool) error {
	switch {
	case !hasValue:
		return errors.New(`option "default" needs a value: write default=<text>, ` +
			`and default= on its own is the empty string`)
	case t.hasDef:
		return errors.New(`option "default" is given twice`)
	}

	text, err := parseToken(value, "value")
	if err != nil {
		return err
	}

	t.def, t.hasDef = text, true

	return nil
}
