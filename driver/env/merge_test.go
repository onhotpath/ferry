package env

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
)

// A save is a merge into the file that is already there, and these are what
// "merge" means, one property per subtest: what the operator wrote survives, and
// what this dump wrote replaces the line it stood on rather than being appended
// beneath it.

// merged is a fixture: write text to a fresh file, dump v over it, and answer
// with what the file holds afterwards.
func merged[T any](t *testing.T, text string, v T, opts ...Naming) string {
	t.Helper()

	path := staged(t, text)

	if err := ferry.Dump(t.Context(), v, NewDotEnvSink(path, sinkWith(opts)...)); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	return read(t, path)
}

// staged writes one file into a fresh directory and answers with its path.
func staged(t *testing.T, text string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	return path
}

// read is what the file holds now.
func read(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the file: %v", err)
	}

	return string(data)
}

// host is the one-field schema most of the cases below save, so that what
// changes between them is the file rather than the struct.
type host struct {
	Host string `ferry:"host"`
}

// TestASaveKeepsWhatTheOperatorWrote is the whole of the merge, one property at
// a time.
func TestASaveKeepsWhatTheOperatorWrote(t *testing.T) {
	t.Parallel()

	for _, c := range []struct{ name, was, want string }{
		{
			"a comment above the line survives",
			"# where the database lives\nHOST=old\n",
			"# where the database lives\nHOST=new\n",
		},
		{
			"a variable no field maps survives",
			"HOST=old\nUNRELATED=keep\n",
			"HOST=new\nUNRELATED=keep\n",
		},
		{
			"the order survives",
			"Z=last\nHOST=old\nA=first\n",
			"Z=last\nHOST=new\nA=first\n",
		},
		{
			"an export prefix survives",
			"export HOST=old\n",
			"export HOST=new\n",
		},
		{
			"the spacing survives",
			"  HOST  =  old\n",
			"  HOST  =  new\n",
		},
		{
			"a trailing comment survives",
			"HOST=old # the box\n",
			"HOST=new # the box\n",
		},
		{
			"blank lines survive",
			"\n\nHOST=old\n\n",
			"\n\nHOST=new\n\n",
		},
		{
			"the quoting survives",
			"HOST='old'\n",
			"HOST='new'\n",
		},
		{
			"a file with no final terminator gains one only where a line is appended",
			"HOST=old",
			"HOST=new",
		},
		{
			"a name the file does not hold is appended",
			"# head\nOTHER=1\n",
			"# head\nOTHER=1\nHOST=new\n",
		},
		{
			"an appended line terminates the last line first",
			"OTHER=1",
			"OTHER=1\nHOST=new\n",
		},
		{
			"a file that is not there is created",
			"",
			"HOST=new\n",
		},
		{
			"a file using CRLF keeps it for the line it appends",
			"OTHER=1\r\n",
			"OTHER=1\r\nHOST=new\r\n",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if got := merged(t, c.was, host{Host: "new"}); got != c.want {
				t.Errorf("the file holds %q, want %q", got, c.want)
			}
		})
	}
}

// tags is the schema every replacement case below saves, and it is a slice
// because a slice is where dump-is-replace has something to do.
type tags struct {
	Tags []string `ferry:"tags"`
}

// TestASaveOfAShorterSliceLeavesFewerVariables is dump-is-replace on this plane,
// and the sweep it needs is what the comment cases below are about.
func TestASaveOfAShorterSliceLeavesFewerVariables(t *testing.T) {
	t.Parallel()

	for _, c := range []struct{ name, was, want string }{
		{
			"the positions this save did not write are removed",
			"TAGS_0=a\nTAGS_1=b\nTAGS_2=c\n",
			"TAGS_0=x\n",
		},
		{
			"a comment written directly above a removed line goes with it",
			"TAGS_0=a\n# about the second one\nTAGS_1=b\n",
			"TAGS_0=x\n",
		},
		{
			"a blank line between the comment and the line keeps the comment",
			"TAGS_0=a\n# about the section\n\nTAGS_1=b\n",
			"TAGS_0=x\n# about the section\n\n",
		},
		{
			"a variable outside the slice is untouched",
			"TAGS_0=a\nTAGS_1=b\nOTHER=keep\n",
			"TAGS_0=x\nOTHER=keep\n",
		},
		{
			"a multi-line value goes whole",
			"TAGS_0=a\nTAGS_1=\"one\ntwo\"\nOTHER=keep\n",
			"TAGS_0=x\nOTHER=keep\n",
		},
		{
			"a longer slice puts its new members beside their siblings",
			"TAGS_0=a\nOTHER=keep\n",
			"TAGS_0=x\nTAGS_1=y\nOTHER=keep\n",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			replaces(t, c.was, c.want)
		})
	}
}

// replaces is one replacement row, lifted out of its table.
//
// The slice saved is one element except in the row whose point is that a longer
// one puts its new members beside their siblings, and that row names the member
// it expects.
func replaces(t *testing.T, was, want string) {
	t.Helper()

	saved := tags{Tags: []string{"x"}}
	if strings.Contains(want, "TAGS_1=y") {
		saved.Tags = []string{"x", "y"}
	}

	if got := merged(t, was, saved); got != want {
		t.Errorf("the file holds %q, want %q", got, want)
	}
}

// TestASaveInheritsTheFilesMode is the permission bits an operator set on a file
// this driver replaces by renaming another over it.
func TestASaveInheritsTheFilesMode(t *testing.T) {
	t.Parallel()

	path := staged(t, "HOST=old\n")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("setting the mode: %v", err)
	}

	if err := ferry.Dump(t.Context(), host{Host: "new"}, NewDotEnvSink(path)); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if got := fi.Mode().Perm(); got != 0o640 {
		t.Errorf("the file is mode %v, want the mode it had: a save must not silently tighten or loosen it", got)
	}
}

// TestASaveIsWrittenThroughASymlink is #256's rule on this driver: the rename
// goes to the file the link names, and the link is left as it is.
func TestASaveIsWrittenThroughASymlink(t *testing.T) {
	t.Parallel()

	target := staged(t, "HOST=old\n")
	link := filepath.Join(filepath.Dir(target), "link.env")

	if err := os.Symlink(target, link); err != nil {
		t.Skipf("this filesystem has no symlinks: %v", err)
	}

	if err := ferry.Dump(t.Context(), host{Host: "new"}, NewDotEnvSink(link)); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if got := read(t, target); got != "HOST=new\n" {
		t.Errorf("the file the link names holds %q, want the save to have gone through the link", got)
	}

	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}

	if fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the link is a regular file now, so the rename replaced it: every reader of the file it named " +
			"would go on reading the old contents")
	}
}

// TestTheNamingOptionsHaveToMatchAcrossThePair is the sharp edge two
// constructors buy: nothing in the type system checks that a sink writing one
// spelling and a source reading another agree, so the failure is asserted to be
// a load that finds nothing rather than a value silently corrupted.
func TestTheNamingOptionsHaveToMatchAcrossThePair(t *testing.T) {
	t.Parallel()

	type nested struct {
		Host string `ferry:"host"`
	}

	type outer struct {
		DB nested `ferry:"db"`
	}

	path := filepath.Join(t.TempDir(), ".env")

	if err := ferry.Dump(t.Context(), outer{DB: nested{Host: "written"}},
		NewDotEnvSink(path, Separator("__"))); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	got, err := ferry.Load[outer](t.Context(), New(Environ(noEnviron), DotEnv(path)))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got.DB.Host != "" {
		t.Errorf("the load read %q, want nothing: the sink wrote DB__HOST and this source reads DB_HOST, and a "+
			"miss is the failure, not a corrupted value", got.DB.Host)
	}

	if held := read(t, path); !strings.Contains(held, "DB__HOST=written") {
		t.Errorf("the file holds %q, want the sink's own spelling to be intact", held)
	}
}
