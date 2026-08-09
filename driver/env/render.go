package env

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/onhotpath/ferry"
)

// render is the file's bytes: every line that is still there, in order, each
// recomposed from the parts it was taken apart into.
//
// It is the identity on a file nothing wrote to, byte for byte, which is what
// makes a save a merge rather than a rewrite: an untouched line's value token is
// the operator's own text and is never derived back from the value it decodes
// to.
func (f *file) render() []byte {
	var b strings.Builder

	b.WriteString(f.bom)

	for i := range f.lines {
		f.lines[i].writeTo(&b)
	}

	return []byte(b.String())
}

// writeTo appends one line, and nothing at all for one the sweep removed.
func (l *line) writeTo(b *strings.Builder) {
	if l.gone {
		return
	}

	if l.kind == kindAssign {
		b.WriteString(l.a.lead)
		b.WriteString(l.a.name)
		b.WriteString(l.a.pre)
		b.WriteByte('=')
		b.WriteString(l.a.post)
		b.WriteString(l.a.src)
	} else {
		b.WriteString(l.raw)
	}

	b.WriteString(l.trailer())
}

// trailer is what follows a line's own content: the trailing comment on an
// assignment, and then the terminator.
func (l *line) trailer() string {
	if l.kind == kindAssign {
		return l.a.trail + l.term
	}

	return l.term
}

// bareByte reports whether a byte may appear in a value written with no quotes
// at all.
//
// It is deliberately narrower than what the parser accepts: wider in, canonical
// out (ADR-0018 law 2). Every byte outside this set either means something to a
// shell, means something to this parser, or is not printable, and the cost of
// quoting a value that did not strictly need it is one pair of quotes.
func bareByte(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	case c == '_', c == '.', c == '/', c == ':', c == '@', c == '%', c == '+', c == ',', c == '=', c == '-':
		return true
	default:
		return false
	}
}

// canBare reports whether a value can be written with no quotes.
//
// The empty value cannot: an unquoted nothing after "=" is how this format
// spells the empty string, and it would parse back correctly, but a pair of
// single quotes says so out loud and a file an operator reads is better for it.
func canBare(v string) bool {
	if v == "" {
		return false
	}

	for i := range len(v) {
		if !bareByte(v[i]) {
			return false
		}
	}

	return true
}

// canSingle reports whether a value can be written inside single quotes, where
// every byte stands for itself.
//
// A single-quoted token has no escape, so the quote itself cannot appear in one,
// and a terminator inside one would make the value cross physical lines when it
// need not.
//
// A byte sequence that is not text goes inside double quotes instead, even
// though single quotes would hold it. Single quotes are what a value an operator
// reads and edits is written in, and a payload is neither.
func canSingle(v string) bool {
	return !strings.ContainsAny(v, "'\n\r") && utf8.ValidString(v)
}

// canDouble reports whether a value can be written inside double quotes without
// changing what a shell reads back.
//
// A shell interpolates inside double quotes, so "$HOME" sourced from a file is
// the shell's home directory rather than the five bytes this driver wrote.
// ferry interpolates nothing, so a value holding one of those goes inside single
// quotes instead, and no escape for "$" is invented that no other reader of this
// format understands.
//
// The one value that gets neither is one holding a "$" or a backtick and a
// single quote at once: single quotes cannot hold it and double quotes let the
// shell at it, and the escape table has no third answer. Such a value round
// trips through this driver exactly, and a shell sourcing the file interpolates
// it. It is one of the two exceptions [DotEnvSink] publishes; the other is the
// three escapes a shell does not read the way this driver writes them.
func canDouble(v string) bool { return !strings.ContainsAny(v, "$`") }

// single is a value inside single quotes.
func single(v string) string { return "'" + v + "'" }

// double is a value inside double quotes, with the five bytes that have an
// escape escaped and every other byte written through.
//
// Bytes that are not valid UTF-8 are written raw, which is what lets a file hold
// what an environment holds: an environment variable is a byte string, and a
// []byte field is parsed from the raw text, so any encoded spelling would not
// survive being read back.
func double(v string) string {
	var b strings.Builder

	b.Grow(len(v) + 2)
	b.WriteByte('"')

	for i := range len(v) {
		if esc, ok := escapeOf(v[i]); ok {
			b.WriteString(esc)

			continue
		}

		b.WriteByte(v[i])
	}

	b.WriteByte('"')

	return b.String()
}

// escapeOf is the write half of this driver's escape table, and it is the exact
// inverse of [escapedByte]: five bytes are escaped and nothing else is.
func escapeOf(c byte) (string, bool) {
	switch c {
	case '\\':
		return `\\`, true
	case '"':
		return `\"`, true
	case '\n':
		return `\n`, true
	case '\r':
		return `\r`, true
	case '\t':
		return `\t`, true
	default:
		return "", false
	}
}

// narrowest is the token this driver writes a value as when it is choosing
// freely: bare where it can, single quotes where it cannot, and double quotes as
// the total fallback.
//
// The order is narrowest first. Bare is what an operator expects to see for the
// ordinary value, single quotes hold any text without an escape in sight, and
// double quotes are the only spelling that holds a newline, a quote or a byte
// sequence that is not text at all.
//
// It is total over every Go string except one holding a NUL, which [spellable]
// refuses before this is reached.
func narrowest(v string) string {
	switch {
	case canBare(v):
		return v
	case canSingle(v):
		return single(v)
	default:
		return double(v)
	}
}

// spellAs is the token for a value, preferring the quoting style the line
// already used where the new value permits it.
//
// Preferring it is what keeps a save's diff to the lines that changed value: a
// single-quoted value stays single-quoted, and a bare one that grows a space
// becomes quoted rather than making the file unparseable. The preference never
// overrides safety, so a value that gains a "$" leaves double quotes behind.
//
// A rewritten value that holds a newline collapses to one physical line, because
// a newline is written as its escape. An untouched multi-line value keeps its
// shape byte for byte.
func spellAs(v string, style byte) string {
	switch style {
	case '\'':
		if canSingle(v) {
			return single(v)
		}
	case '"':
		if canDouble(v) {
			return double(v)
		}
	default:
		if canBare(v) {
			return v
		}
	}

	return narrowest(v)
}

// styleOf is the quoting a value token was written with, read off its first
// byte, and 0 for a bare one.
func styleOf(src string) byte {
	if quotedToken(src) {
		return src[0]
	}

	return 0
}

// spellable refuses the one value this plane cannot hold.
//
// A NUL is a fact about the plane rather than about the format: the environment
// block is passed to a new process as NUL-terminated strings, so no spelling of
// one could round trip, and a .env file holding one would be a file the process
// half of a save could not apply.
func spellable(v string) error {
	if strings.IndexByte(v, 0) < 0 {
		return nil
	}

	return fmt.Errorf("%w: this value holds a NUL byte, and an environment variable is a NUL-terminated "+
		"string: no spelling of it in a file or in a process could be read back", ferry.ErrValue)
}

// carried is the text an environment variable would hold for one value, or a
// refusal naming the kind this plane cannot carry.
//
// Bool goes through the plane's own words where [BoolWords] named any, which is
// what stops this plane writing true and reading on. Number, String and Bytes
// are the text they already are, and a byte sequence is written through raw for
// the reason [double] gives.
//
// Null and Absent are refused. FOO= is already the empty string, so a null has
// nothing to be here, and an address ferry omits gets no call at all.
func (c *config) carried(v ferry.Value) (string, error) {
	switch v.Kind() {
	case ferry.KindBool:
		return c.spellBool(v)
	case ferry.KindNumber:
		return v.AsNumber()
	case ferry.KindString:
		return v.AsString()
	case ferry.KindBytes:
		b, err := v.AsBytes()

		return string(b), err
	case ferry.KindNull, ferry.KindAbsent:
		return "", errNoNull
	default:
		return "", fmt.Errorf("%w: an environment variable holds text, and this plane cannot carry a %s",
			ferry.ErrValue, v.Kind())
	}
}

// spellBool writes a boolean in this plane's own words, and in Go's where none
// were declared.
func (c *config) spellBool(v ferry.Value) (string, error) {
	b, err := v.AsBool()
	if err != nil {
		return "", err
	}

	if c.bools == nil {
		return strconv.FormatBool(b), nil
	}

	return c.bools.Render(b)
}

// errNoNull is the refusal that makes this a plane with no null rather than a
// plane that quietly has one.
var errNoNull = fmt.Errorf("%w: an environment variable holds text and has no null, and this name was handed "+
	"one: a nil pointer to a value has nothing to be written as here, and FOO= is already the empty string",
	ferry.ErrValue)
