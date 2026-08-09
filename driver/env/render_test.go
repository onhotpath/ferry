package env

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// hostile is every value this driver has to be able to write, chosen so that
// each one lands on a different arm of the quoting rules.
var hostile = []struct {
	name  string
	value string
}{
	{"ordinary", "db.internal"},
	{"empty", ""},
	{"a leading space", " lead"},
	{"a trailing space", "trail "},
	{"a hash", "# not a comment"},
	{"a single quote", "it's"},
	{"a double quote", `say "hi"`},
	{"both quotes", "a'b\"c"},
	{"a backslash", `C:\Users\me`},
	{"a dollar", "$HOME"},
	{"a backtick", "`whoami`"},
	{"a newline", "one\ntwo"},
	{"a carriage return", "one\rtwo"},
	{"a tab", "one\ttwo"},
	{"an equals", "k=v"},
	{"bytes that are not utf-8", "\xff\xfe"},
	{"a long value", strings.Repeat("x", 64<<10)},
}

// TestRenderThenParseIsTheIdentity is the seam read the other way: a value this
// driver wrote is one it reads back unchanged.
//
// The pair is what a save composes with a load, so a value that survives this
// survives the round trip whatever the file around it looks like.
func TestRenderThenParseIsTheIdentity(t *testing.T) {
	t.Parallel()

	for _, c := range hostile {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			text := "A=" + narrowest(c.value) + "\n"

			if got := parsed(t, text)[of("A")]; got != c.value {
				t.Errorf("%s wrote %q, which reads back as %q", c.name, text, got)
			}
		})
	}
}

// of is the name a row is written at, spelled through a function so that the
// assertion above reads as one line rather than two.
func of(name string) string { return name }

// TestTheQuotingIsPinned is the table the format promises, and it is the one
// place a change to any of these rows has to be a deliberate one: every .env
// file this driver has ever written means what these rows say.
func TestTheQuotingIsPinned(t *testing.T) {
	t.Parallel()

	for _, c := range []struct{ name, value, want string }{
		{"ordinary text is bare", "db.internal", "db.internal"},
		{"a number is bare", "8080", "8080"},
		{"a path is bare", "/var/run:/tmp", "/var/run:/tmp"},
		{"empty is single-quoted", "", "''"},
		{"a hash is single-quoted", "# not a comment", "'# not a comment'"},
		{"padding is single-quoted", " padded ", "' padded '"},
		{"a double quote is single-quoted", `say "hi"`, `'say "hi"'`},
		{"a dollar is single-quoted", "$HOME", "'$HOME'"},
		{"a backtick is single-quoted", "`whoami`", "'`whoami`'"},
		{"a single quote is double-quoted", "it's", `"it's"`},
		{"both quotes are double-quoted", "a'b\"c", `"a'b\"c"`},
		{"a newline is double-quoted on one line", "a\nb", `"a\nb"`},
		{"a backslash needs no escape inside single quotes", `a\b`, `'a\b'`},
		{"a backslash doubles where double quotes are forced", "it's a\\b", `"it's a\\b"`},
		{"bytes that are not utf-8 are raw inside double quotes", "\xff\xfe", "\"\xff\xfe\""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if got := narrowest(c.value); got != c.want {
				t.Errorf("wrote %q, want %q", got, c.want)
			}
		})
	}
}

// TestAValueThisDriverWroteIsOneShellReadsBack is the promise behind the quoting
// rules, checked against a real shell rather than argued.
//
// It covers every value except the ones double quotes are forced on and a shell
// then reads differently, which [shellSafe] names and [DotEnvSink] publishes.
// Those round trip through ferry exactly; what they are not is a line a shell
// sourcing the file agrees with.
func TestAValueThisDriverWroteIsOneShellReadsBack(t *testing.T) {
	t.Parallel()

	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on this machine, and this proof is about what one reads")
	}

	for _, c := range hostile {
		if !shellSafe(c.value) {
			continue
		}

		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			readsBack(t, sh, c.value)
		})
	}
}

// readsBack is one value through a real shell, lifted out of its table.
func readsBack(t *testing.T, sh, value string) {
	t.Helper()

	if got := sourced(t, sh, narrowest(value)); got != value {
		t.Errorf("sh read %q back as %q", value, got)
	}
}

// shellSafe reports whether a value is one the sh promise covers.
//
// A bare value and a single-quoted one always are: a shell reads a single-quoted
// token literally, byte for byte, which is exactly what this driver means by it.
//
// A value double quotes are forced on is where the two formats part company. A
// shell unescapes \\ and \" inside double quotes as this driver does, so those
// agree; it does not unescape \n, \r or \t, so a value holding one of those is
// two bytes to a shell and one to ferry. And it expands "$" and a backtick,
// which is why a value holding one of those goes inside single quotes wherever
// it can - the values left over are the ones that hold a single quote too, and
// no spelling satisfies both.
func shellSafe(v string) bool {
	if canBare(v) || canSingle(v) {
		return true
	}

	return canDouble(v) && !strings.ContainsAny(v, "\n\r\t")
}

// sourced writes one assignment into a file, sources it in sh, and answers with
// what the shell then holds.
//
// printf is what carries the value back, because echo mangles a backslash on
// some shells and a value holding one is exactly what this is testing.
func sourced(t *testing.T, sh, token string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("A="+token+"\n"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	out, err := exec.CommandContext(t.Context(), sh, "-c", ". "+path+`; printf %s "$A"`).Output()
	if err != nil {
		t.Fatalf("sourcing the file: %v", err)
	}

	return string(out)
}

// TestAValueHoldingANULIsRefused is the one value this plane cannot hold, and
// the refusal is about the plane rather than about the format: the environment
// block is handed to a new process as NUL-terminated strings.
func TestAValueHoldingANULIsRefused(t *testing.T) {
	t.Parallel()

	if err := spellable("a\x00b"); err == nil {
		t.Error("a value holding a NUL was spelled, want a refusal: no file and no process could hold it")
	}

	if err := spellable("ordinary"); err != nil {
		t.Errorf("an ordinary value was refused: %+v", err)
	}
}

// TestARewriteKeepsTheQuotingTheLineUsed is what keeps a save's diff to the
// lines whose value changed.
func TestARewriteKeepsTheQuotingTheLineUsed(t *testing.T) {
	t.Parallel()

	for _, c := range []struct{ name, was, value, want string }{
		{"single stays single", "'old'", "new", "'new'"},
		{"double stays double", `"old"`, "new", `"new"`},
		{"bare stays bare", "old", "new", "new"},
		{"bare that no longer can be gains quotes", "old", "new value", "'new value'"},
		{"double that would be interpolated loses them", `"old"`, "$HOME", "'$HOME'"},
		{"single that no longer can be loses them", "'old'", "it's", `"it's"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if got := spellAs(c.value, styleOf(c.was)); got != c.want {
				t.Errorf("rewrote %s as %s, want %s", c.was, got, c.want)
			}
		})
	}
}
