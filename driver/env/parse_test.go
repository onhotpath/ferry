package env

import (
	"errors"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
)

// The parse and the render are the seam of this format, and they are tested
// directly for the reason ferry.Path, ferry.AddressSet and ferry.Value are: the
// property that matters is that one is the other's inverse, and a round trip
// through Load and Dump composes the pair with itself and cannot see the two
// being wrong together.
//
// What the seam promises is one sentence. Parse then render is the identity on
// every input the parser accepted, byte for byte, so a save rewrites the lines
// it wrote and no others.

// corpus is every shape a real .env file has, and the identity has to hold on
// each one.
var corpus = []struct {
	name string
	text string
}{
	{"empty", ""},
	{"one assignment", "A=1\n"},
	{"no final terminator", "A=1"},
	{"blank lines", "\n\nA=1\n\n"},
	{"whitespace-only lines", "  \n\t\nA=1\n"},
	{"comments", "# a comment\nA=1\n# trailing\n"},
	{"indented comment", "    # indented\nA=1\n"},
	{"export", "export A=1\n"},
	{"export with wide spacing", "export   A=1\n"},
	{"indented export", "\texport A=1\n"},
	{"a name that is export", "export=1\n"},
	{"spaces around the equals", "A = 1\n"},
	{"trailing whitespace on a bare value", "A=1   \n"},
	{"trailing comment", "A=1 # why\n"},
	{"a hash inside a bare value", "A=a#b\n"},
	{"an empty value with a comment", "A= # why\n"},
	{"empty value", "A=\n"},
	{"single quotes", "A='hello world'\n"},
	{"double quotes", "A=\"hello world\"\n"},
	{"every escape", `A="\\ \" \n \r \t"` + "\n"},
	{"a hash inside quotes", "A='# not a comment'\n"},
	{"an equals inside quotes", "A='k=v'\n"},
	{"a comment after a quoted value", "A='x' # why\n"},
	{"crlf", "A=1\r\nB=2\r\n"},
	{"mixed terminators", "A=1\nB=2\r\nC=3\n"},
	{"a bare value holding a lone carriage return", "A=1\r2\n"},
	{"a byte order mark", bom + "A=1\n"},
	{"a multi-line double-quoted value", "A=\"one\ntwo\"\nB=2\n"},
	{"a multi-line single-quoted value", "A='one\ntwo'\nB=2\n"},
	{"bytes that are not utf-8", "A=\"\xff\xfe\"\n"},
	{"everything at once", "# head\n\nexport DB_HOST = 'db.internal' # the box\n\nDB_PORT=5432\n#tail"},
}

// TestParseThenRenderIsTheIdentity is the property the merge rests on.
//
// A save renders the whole file back, so any byte the parse could not put back
// exactly is a byte a save silently changes on a line nobody wrote to.
func TestParseThenRenderIsTheIdentity(t *testing.T) {
	t.Parallel()

	for _, c := range corpus {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			roundsBack(t, c.text)
		})
	}
}

// roundsBack is one corpus entry, lifted out of its table so that the table
// stays a table: a subtest body counts against the enclosing function's
// complexity.
func roundsBack(t *testing.T, text string) {
	t.Helper()

	f, err := parseFile([]byte(text))
	if err != nil {
		t.Fatalf("parse: %+v", err)
	}

	if got := string(f.render()); got != text {
		t.Errorf("rendered %q, want the input back byte for byte: %q", got, text)
	}
}

// TestTheParseReadsTheValueTheFileHolds is the other half of the corpus: the
// bytes come back, and so does what they mean.
func TestTheParseReadsTheValueTheFileHolds(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		text string
		want string
	}{
		{"bare", "A=1", "1"},
		{"bare with trailing whitespace", "A=1   ", "1"},
		{"bare with a trailing comment", "A=1 # why", "1"},
		{"a hash with no space in front of it", "A=a#b", "a#b"},
		{"an empty value with a comment", "A= # why", ""},
		{"empty", "A=", ""},
		{"single quotes are literal", `A='a\nb'`, `a\nb`},
		{"double quotes are escaped", `A="a\nb"`, "a\nb"},
		{"every escape", `A="\\ \" \n \r \t"`, "\\ \" \n \r \t"},
		{"export", "export A=1", "1"},
		{"a name that is export", "export=1", "1"},
		{"spaces around the equals", "A = 1", "1"},
		{"a multi-line double-quoted value", "A=\"one\ntwo\"", "one\ntwo"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			holds(t, c.text, c.want)
		})
	}
}

// holds is one file's one value, lifted out of its table.
//
// The name is A everywhere except the row whose whole point is that a variable
// may be called export.
func holds(t *testing.T, text, want string) {
	t.Helper()

	name := "A"
	if strings.HasPrefix(text, "export=") {
		name = "export"
	}

	if got := parsed(t, text)[name]; got != want {
		t.Errorf("the file holds %q at %s, want %q", got, name, want)
	}
}

// TestTheParseRefuses is the table of files this driver will not read, and every
// row is a file that would otherwise be loaded as something it does not say.
func TestTheParseRefuses(t *testing.T) {
	t.Parallel()

	for _, c := range []refusal{
		{"an unclosed single quote", "A='oops\n", ErrMalformed, "line 1"},
		{"an unclosed double quote", "A=\"oops\n", ErrMalformed, "line 1"},
		{"a line with no equals", "A=1\njust some text\n", ErrMalformed, "line 2"},
		{"an empty name", "=v\n", ErrMalformed, "line 1"},
		{"an empty name after export", "export =v\n", ErrMalformed, "line 1"},
		{"a name holding a hyphen", "A=1\nfeature-flags=on\n", ErrMalformed, "line 2"},
		{"a name beginning with a digit", "1A=x\n", ErrMalformed, "line 1"},
		{"the same name twice", "A=1\nA=2\n", ErrDuplicate, "line 2"},
		{"text after a closing quote", "A='x' oops\n", ErrMalformed, "line 1"},
		{"an escape this driver does not write", `A="a\qb"`, ErrMalformed, "line 1"},
		{"a NUL byte", "A=1\nB=a\x00b\n", ErrMalformed, "line 2"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			refuses(t, c)
		})
	}
}

// refusal is one file this driver will not read, the class it refuses with, and
// the source line it has to name.
type refusal struct {
	name string
	text string
	want error
	line string
}

// refuses is one refusal row, lifted out of its table.
func refuses(t *testing.T, c refusal) {
	t.Helper()

	_, err := parseFile([]byte(c.text))
	if err == nil {
		t.Fatal("the parse took this file, want a refusal: a file that does not parse is never loaded " +
			"as empty and never silently overwritten")
	}

	answers(t, err, ferry.ErrPlane, c.want)

	if !strings.Contains(err.Error(), c.line) {
		t.Errorf("the refusal is %q, want it to name %s", err.Error(), c.line)
	}
}

// answers asserts that one error is reachable as each of the classes named,
// which is the two-line assertion every refusal in this package makes.
func answers(t *testing.T, err error, want ...error) {
	t.Helper()

	for _, class := range want {
		if !errors.Is(err, class) {
			t.Errorf("the refusal is %+v, which does not answer errors.Is against %v", err, class)
		}
	}
}

// TestARefusalAboutSpellingQuotesTheOffendingText is ADR-0018's law 4 on the two
// refusals it applies to: what is wrong with the line is which bytes are on it,
// so the message says which, bounded and escaped.
func TestARefusalAboutSpellingQuotesTheOffendingText(t *testing.T) {
	t.Parallel()

	for _, c := range []struct{ name, text, want string }{
		{"an illegal name", "feature-flags=on\n", `"feature-flags"`},
		{"an unknown escape", `A="a\qb"`, `"\\q"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			quotes(t, c.text, c.want)
		})
	}
}

// quotes is one spelling refusal, lifted out of its table.
func quotes(t *testing.T, text, want string) {
	t.Helper()

	_, err := parseFile([]byte(text))
	if err == nil {
		t.Fatal("the parse took this file, want a refusal")
	}

	if !strings.Contains(err.Error(), want) {
		t.Errorf("the refusal is %q, want it to quote %s", err.Error(), want)
	}
}

// parsed is one file's assignments, for a file the parse takes.
func parsed(t *testing.T, text string) map[string]string {
	t.Helper()

	f, err := parseFile([]byte(text))
	if err != nil {
		t.Fatalf("parse: %+v", err)
	}

	out := map[string]string{}
	f.into(out)

	return out
}
