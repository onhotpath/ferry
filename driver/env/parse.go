package env

import (
	"errors"
	"fmt"
	"strings"

	"github.com/onhotpath/ferry"
)

// ErrMalformed reports a .env file this driver cannot read.
//
// It is raised at the open, in both directions, so a file that does not parse
// is never loaded as empty and never silently overwritten by a save.
//
// It wraps [ferry.ErrPlane], and it stays reachable under ferry's wrapper, so
// errors.Is answers for it on what [ferry.Load] and [ferry.Dump] returned.
var ErrMalformed = errors.New("env: this file is not a .env file")

// ErrDuplicate reports a .env file assigning one name twice.
//
// A reader that takes the first and one that takes the last are both live
// conventions, so the file says two things and there is no way to pick. A save
// makes it worse: the sweep has no second occurrence to leave behind, so the
// file would come back holding the new value and the old one.
//
// It wraps [ferry.ErrPlane], and it stays reachable under ferry's wrapper.
var ErrDuplicate = errors.New("env: this file assigns one name twice")

// lineKind is what one logical line of a .env file is, and the set is closed at
// three: everything in the file is a blank, a comment, or an assignment.
type lineKind uint8

const (
	kindBlank lineKind = iota
	kindComment
	kindAssign
)

// file is one parsed .env file: the map a load reads, and the ordered line model
// a save merges into, out of one parse.
//
// The two are one structure because a save is a read-modify-write: the value at
// a name and the exact bytes the line was written as are needed together, and
// parsing twice would let them disagree.
type file struct {
	// bom is a UTF-8 byte order mark the file opened with, re-emitted first so
	// that a file an editor wrote one into keeps it.
	bom string

	// lines is every logical line in source order. A double-quoted value that
	// crosses physical lines is one entry, because the span is what a rewrite or
	// a removal has to treat as a unit.
	lines []line

	// index is variable name to position in lines, which is what makes a write
	// at a name a rewrite in place rather than an append.
	index map[string]int

	// eol is the first terminator the file used, and what an appended line
	// takes. A file with no terminator at all takes "\n".
	eol string
}

// line is one logical line, held so that rendering it back is the identity on
// every line a save did not touch:
//
//	lead + name + pre + "=" + post + src + trail + term
//
// for an assignment, and raw + term for anything else.
type line struct {
	kind lineKind

	// raw is the exact source text of a blank or a comment line, terminator
	// excluded.
	raw string

	// term is "\n", "\r\n", or "" at a final line the file did not terminate.
	term string

	// gone marks a line the sweep removed. It is a flag rather than a deletion
	// so that every position [file.index] holds stays valid.
	gone bool

	// a is the assignment, and it is meaningful only at kindAssign.
	a assign
}

// assign is one variable's line, split at every place a save may need to keep
// the operator's own bytes.
//
// src is the value token exactly as it was written, quotes included, and value
// is src decoded. A rewrite moves both together and never derives src from
// value, which is what keeps an untouched line byte-identical.
type assign struct {
	// lead is the indentation and an "export " prefix with its own spacing.
	lead string

	name string

	// pre and post are the spacing either side of "=".
	pre, post string

	src   string
	value string

	// trail is the spacing and any trailing comment after the value token.
	trail string
}

// bom is the byte order mark a Windows editor writes at the head of a file. It
// is spelled as an escape because the character itself is invisible in source.
const bom = "\ufeff"

// exportWord is the prefix a .env file written to be sourced by a shell carries,
// and it is kept on a line that already had it rather than added to a new one.
const exportWord = "export"

// parseFile reads the whole of a .env file into the one model both directions
// use.
//
// A refusal here carries the 1-based source line, which is what an operator
// opens the file at, and never the value on the line, which is theirs.
func parseFile(data []byte) (*file, error) {
	src := string(data)

	if i := strings.IndexByte(src, 0); i >= 0 {
		return nil, malformed(lineOf(src, i), "this file holds a NUL byte, and an environment variable is a "+
			"NUL-terminated string, so no variable here could ever hold one")
	}

	f := &file{index: make(map[string]int)}

	if rest, found := strings.CutPrefix(src, bom); found {
		f.bom, src = bom, rest
	}

	s := &scanner{src: src, at: 1}

	for s.pos < len(s.src) {
		ln, err := s.next()
		if err != nil {
			return nil, err
		}

		if err := f.add(&ln, s.start); err != nil {
			return nil, err
		}
	}

	return f, nil
}

// add appends one parsed line and indexes it, refusing a name the file already
// assigned.
func (f *file) add(ln *line, at int) error {
	if f.eol == "" {
		f.eol = ln.term
	}

	f.lines = append(f.lines, *ln)

	if ln.kind != kindAssign {
		return nil
	}

	if first, twice := f.index[ln.a.name]; twice {
		return fmt.Errorf("%w: %w: line %d assigns a name line %d already assigned: one reader of this file "+
			"takes the first and another takes the last, so the file says two things",
			ferry.ErrPlane, ErrDuplicate, at, first+1)
	}

	f.index[ln.a.name] = len(f.lines) - 1

	return nil
}

// lineOf is the 1-based source line an offset falls on.
func lineOf(src string, at int) int {
	return 1 + strings.Count(src[:at], "\n")
}

// scanner is one pass over a file's bytes.
//
// at is the line the cursor is on and start is the line the logical line being
// read began on, which is the one a refusal names: a double-quoted value may
// cross several physical lines, and the assignment is what the operator has to
// go and look at.
type scanner struct {
	src   string
	pos   int
	at    int
	start int
}

// next reads one logical line.
func (s *scanner) next() (line, error) {
	s.start = s.at
	from := s.pos
	lead := s.spaces()

	switch {
	case s.atEOL():
		return s.plain(kindBlank, from), nil
	case s.peek() == '#':
		s.toEOL()

		return s.plain(kindComment, from), nil
	default:
		return s.assignment(lead)
	}
}

// plain finishes a blank or a comment line, whose whole content is its own
// bytes.
func (s *scanner) plain(kind lineKind, from int) line {
	raw := s.src[from:s.pos]

	return line{kind: kind, raw: raw, term: s.term()}
}

// assignment reads one variable's line, and is where every refusal about the
// shape of a line is made.
func (s *scanner) assignment(lead string) (line, error) {
	lead += s.exported()

	nameAt := s.pos
	name := s.word()
	pre := s.spaces()

	if err := s.expectEq(name, nameAt); err != nil {
		return line{}, err
	}

	post := s.spaces()

	src, value, err := s.valueToken()
	if err != nil {
		return line{}, err
	}

	trail, err := s.tail(src)
	if err != nil {
		return line{}, err
	}

	return line{kind: kindAssign, term: s.term(), a: assign{
		lead: lead, name: name, pre: pre, post: post, src: src, value: value, trail: trail,
	}}, nil
}

// expectEq consumes the "=" and settles the name, which are one question: a line
// with no "=" on it is not an assignment at all, and a line with one whose text
// in front of it is not a name is an assignment this driver cannot read.
func (s *scanner) expectEq(name string, nameAt int) error {
	if s.peek() == '=' {
		s.pos++

		return s.legalName(name)
	}

	head := s.src[nameAt:s.lineEnd()]
	if i := strings.IndexByte(head, '='); i >= 0 {
		return s.badName(head[:i])
	}

	return s.refuse("this line is not blank, not a comment and holds no \"=\", so it assigns nothing")
}

// legalName refuses the two names a shell cannot set. The name was read as a run
// of the bytes a name may hold, so what is left to check is that there is one
// and that it does not begin with a digit.
func (s *scanner) legalName(name string) error {
	switch {
	case name == "":
		return s.refuse("this line assigns to an empty name, and no shell will set one")
	case name[0] >= '0' && name[0] <= '9':
		return s.badName(name)
	default:
		return nil
	}
}

// exported consumes an "export " prefix and answers with it, spacing included,
// or with nothing where the line has none.
//
// The trailing space is what makes it a prefix: export=1 assigns to a variable
// called export, and rewinding is how that stays true.
func (s *scanner) exported() string {
	from := s.pos

	if !strings.HasPrefix(s.src[s.pos:], exportWord) {
		return ""
	}

	s.pos += len(exportWord)

	if s.spaces() == "" {
		s.pos = from

		return ""
	}

	return s.src[from:s.pos]
}

// valueToken reads the value as written, and answers with the token and with
// what it decodes to.
func (s *scanner) valueToken() (src, value string, err error) {
	switch s.peek() {
	case '\'':
		return s.literal()
	case '"':
		return s.escaped()
	default:
		return s.bare()
	}
}

// literal reads a single-quoted token, whose content is itself. It may cross
// physical lines, and the span is one line of this model.
func (s *scanner) literal() (src, value string, err error) {
	from := s.pos

	i := strings.IndexByte(s.src[from+1:], '\'')
	if i < 0 {
		return "", "", s.unterminated()
	}

	end := from + i + 2
	s.take(end)

	return s.src[from:end], s.src[from+1 : end-1], nil
}

// escaped reads a double-quoted token and decodes its escapes. It may cross
// physical lines too, and a newline inside it is part of the value.
func (s *scanner) escaped() (src, value string, err error) {
	from := s.pos

	end, ok := closingQuote(s.src, from+1)
	if !ok {
		return "", "", s.unterminated()
	}

	value, err = s.unescape(s.src[from+1 : end])
	if err != nil {
		return "", "", err
	}

	s.take(end + 1)

	return s.src[from : end+1], value, nil
}

// closingQuote is where a double-quoted token ends, stepping over the byte a
// backslash escapes so that \" does not close it.
//
// Stepping two at a backslash is also what guarantees [scanner.unescape] a byte
// after every backslash it sees: the byte a backslash consumed here is inside
// the token by construction, so it is never the closing quote and never past the
// end.
func closingQuote(src string, i int) (int, bool) {
	for i < len(src) {
		switch src[i] {
		case '\\':
			i += 2
		case '"':
			return i, true
		default:
			i++
		}
	}

	return 0, false
}

// unescape decodes a double-quoted token's body, and refuses an escape this
// driver does not write.
//
// Refusing rather than reading \q as a literal q, or as \q, is the only answer
// that does not silently change a value: every backslash this driver writes is
// written \\, so a bare \q in a file did not come from here and there is no way
// to know which reader wrote it.
func (s *scanner) unescape(body string) (string, error) {
	if !strings.Contains(body, `\`) {
		return body, nil
	}

	var b strings.Builder

	b.Grow(len(body))

	for i := 0; i < len(body); i++ {
		c := body[i]

		if c == '\\' {
			i++

			var ok bool
			if c, ok = escapedByte(body[i]); !ok {
				return "", s.badEscape(body[i-1 : i+1])
			}
		}

		b.WriteByte(c)
	}

	return b.String(), nil
}

// escapedByte is the read half of this driver's escape table, and it is the
// whole of it: five escapes, and every other byte stands for itself.
func escapedByte(c byte) (byte, bool) {
	switch c {
	case '\\', '"':
		return c, true
	case 'n':
		return '\n', true
	case 'r':
		return '\r', true
	case 't':
		return '\t', true
	default:
		return 0, false
	}
}

// bare reads an unquoted value, which ends at the line, at a "#" with whitespace
// in front of it, or at the trailing whitespace before either.
func (s *scanner) bare() (src, value string, err error) {
	from := s.pos

	s.toEOL()

	text := s.src[from:s.pos]
	text = text[:commentAt(s.src, from, len(text))]
	value = strings.TrimRight(text, " \t")
	s.pos = from + len(value)

	return value, value, nil
}

// commentAt is how much of a bare value's text is the value: everything up to a
// "#" with whitespace in front of it, and the whole of it where there is none.
//
// The byte in front is read out of the source rather than out of the text, so
// the spacing after "=" counts: A= #c is an empty value with a comment, and
// A=a#b is the value a#b.
func commentAt(src string, from, n int) int {
	for i := range n {
		if src[from+i] == '#' && from+i > 0 && space(src[from+i-1]) {
			return i
		}
	}

	return n
}

// tail is whatever follows the value token up to the line's end.
//
// After a quoted token it is checked, because text there is a line this driver
// cannot render back and would otherwise be dropped by a save. After a bare one
// there is nothing to check: the value ended where the comment began.
func (s *scanner) tail(token string) (string, error) {
	from := s.pos

	s.toEOL()

	rest := s.src[from:s.pos]

	if quotedToken(token) {
		if text := strings.TrimLeft(rest, " \t"); text != "" && text[0] != '#' {
			return "", s.refuse("this line holds text after the closing quote that is not a comment")
		}
	}

	return rest, nil
}

// quotedToken reports whether a value token carries its own quotes.
func quotedToken(src string) bool {
	return src != "" && (src[0] == '\'' || src[0] == '"')
}

// The cursor primitives. Each one moves the cursor and nothing else, so that
// every rule above reads as a rule rather than as index arithmetic.

// peek is the byte at the cursor, and 0 at the end of the file. No line can hold
// a NUL, so 0 is unambiguous.
func (s *scanner) peek() byte {
	if s.pos >= len(s.src) {
		return 0
	}

	return s.src[s.pos]
}

// space reports whether a byte is the whitespace a .env line may hold beside a
// terminator.
func space(c byte) bool { return c == ' ' || c == '\t' }

// spaces consumes a run of whitespace and answers with it.
func (s *scanner) spaces() string {
	from := s.pos
	for s.pos < len(s.src) && space(s.src[s.pos]) {
		s.pos++
	}

	return s.src[from:s.pos]
}

// word consumes a run of the bytes an environment variable name may hold.
func (s *scanner) word() string {
	from := s.pos
	for s.pos < len(s.src) && nameByte(s.src[s.pos]) {
		s.pos++
	}

	return s.src[from:s.pos]
}

// atEOL reports whether the cursor is at a terminator or at the end of the file.
//
// A lone carriage return is not one. It is an ordinary byte, so a value holding
// it keeps it and a file written on one platform and read on another does not
// change meaning.
func (s *scanner) atEOL() bool {
	return s.pos >= len(s.src) || s.src[s.pos] == '\n' ||
		s.src[s.pos] == '\r' && s.pos+1 < len(s.src) && s.src[s.pos+1] == '\n'
}

// toEOL moves the cursor to the terminator, or to the end of the file.
func (s *scanner) toEOL() {
	for !s.atEOL() {
		s.pos++
	}
}

// lineEnd is where the physical line the cursor is on ends, without moving it.
func (s *scanner) lineEnd() int {
	if i := strings.IndexByte(s.src[s.pos:], '\n'); i >= 0 {
		return s.pos + i
	}

	return len(s.src)
}

// term consumes the terminator and answers with it, or with "" at a final line
// the file did not terminate.
func (s *scanner) term() string {
	switch {
	case s.pos >= len(s.src):
		return ""
	case s.src[s.pos] == '\n':
		s.pos++
		s.at++

		return "\n"
	default:
		s.pos += 2
		s.at++

		return "\r\n"
	}
}

// take moves the cursor to an offset a token scan already found, counting the
// lines crossed on the way so that the next refusal names the right one.
func (s *scanner) take(to int) {
	s.at += strings.Count(s.src[s.pos:to], "\n")
	s.pos = to
}

// The refusals. Each names the line, which is structure, and the two that are
// about spelling additionally quote the offending text through the bounded
// [quoted] this package already uses (ADR-0018 law 4, ADR-0011).

// refuse is a malformed file at the line being read.
func (s *scanner) refuse(msg string) error { return malformed(s.start, msg) }

// unterminated is the one refusal a scan makes at the end of the file rather
// than at a byte.
func (s *scanner) unterminated() error {
	return s.refuse("this line opens a quoted value that the file never closes")
}

// badName refuses text in front of an "=" that is not an environment variable
// name.
func (s *scanner) badName(text string) error {
	return malformed(s.start, "this line assigns to "+quoted(text)+", which is not an environment variable "+
		"name: a name is a letter or _ followed by letters, digits and _")
}

// badEscape refuses an escape this driver does not write.
func (s *scanner) badEscape(text string) error {
	return malformed(s.start, "this line holds the escape "+quoted(text)+" inside a double-quoted value, and "+
		"the escapes here are \\\\ \\\" \\n \\r and \\t")
}

// malformed states the class this driver has an opinion about and keeps
// [ErrMalformed] reachable underneath it.
func malformed(at int, msg string) error {
	return fmt.Errorf("%w: %w: line %d: %s", ferry.ErrPlane, ErrMalformed, at, msg)
}
