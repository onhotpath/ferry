package env

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/onhotpath/ferry"
)

// The precedence ladder is the whole of what makes this one plane rather than
// two: the files are layers in the order the caller named them, and the process
// environment is the anchor above all of them.

// layered is a fixture: write each file, load through a source that names them
// in order under the given environment, and answer with the struct.
func layered[T any](t *testing.T, files []string, vars []string, opts ...Option) T {
	t.Helper()

	dir := t.TempDir()
	paths := make([]string, 0, len(files))

	for i, text := range files {
		path := filepath.Join(dir, "layer"+string(rune('a'+i))+".env")
		paths = append(paths, path)

		if text != missing {
			write(t, path, text)
		}
	}

	src := New(append([]Option{Environ(func() []string { return vars }), DotEnv(paths...)}, opts...)...)

	got, err := ferry.Load[T](t.Context(), src)
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	return got
}

// missing marks a layer whose file is deliberately never written. It is a value
// no file can hold, so it cannot be mistaken for one.
const missing = "\x00 never written"

// write puts one layer on disk.
func write(t *testing.T, path, text string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// errorIs is errors.Is, named so that a case reads as the claim it makes.
func errorIs(err, want error) bool { return errors.Is(err, want) }

// TestALaterFileWinsOverAnEarlierOne is what naming the files in order means.
func TestALaterFileWinsOverAnEarlierOne(t *testing.T) {
	t.Parallel()

	got := layered[host](t, []string{"HOST=base\n", "HOST=local\n"}, nil)

	if got.Host != "local" {
		t.Errorf("loaded %q, want the later file's value", got.Host)
	}
}

// TestTheProcessWinsOverEveryFile is the plane's anchor, and it is the sharp edge
// the package documentation opens with.
func TestTheProcessWinsOverEveryFile(t *testing.T) {
	t.Parallel()

	got := layered[host](t, []string{"HOST=base\n", "HOST=local\n"}, []string{"HOST=ambient"})

	if got.Host != "ambient" {
		t.Errorf("loaded %q, want the process's value: a variable that is exported wins over every file", got.Host)
	}
}

// TestAFileThatIsNotThereIsAnEmptyLayer is what "the files are optional" means,
// and it is the same answer driver/yaml gives a path with nothing at it.
func TestAFileThatIsNotThereIsAnEmptyLayer(t *testing.T) {
	t.Parallel()

	got := layered[host](t, []string{missing, "HOST=local\n"}, nil)

	if got.Host != "local" {
		t.Errorf("loaded %q, want the file that is there to be read and the one that is not to be nothing",
			got.Host)
	}
}

// TestALowerLayerFillsInWhatTheOnesAboveDoNotSay is the other half of layering:
// a name only one layer holds is visible whichever layer that is.
func TestALowerLayerFillsInWhatTheOnesAboveDoNotSay(t *testing.T) {
	t.Parallel()

	type both struct {
		Host string `ferry:"host"`
		Port string `ferry:"port"`
		Name string `ferry:"name"`
	}

	got := layered[both](t, []string{"HOST=base\nPORT=5432\n", "HOST=local\n"}, []string{"NAME=checkout"})

	if got != (both{Host: "local", Port: "5432", Name: "checkout"}) {
		t.Errorf("loaded %+v, want each name from the highest layer that holds it", got)
	}
}

// TestAFileThatDoesNotParseIsRefusedRatherThanReadAsEmpty is conformance case 4's
// rule on the read side: a load that answered "the file held nothing" for a file
// with a typo in it is a config load reporting success for a file it never read.
func TestAFileThatDoesNotParseIsRefusedRatherThanReadAsEmpty(t *testing.T) {
	t.Parallel()

	path := staged(t, "HOST=ok\nthis is not an assignment\n")

	_, err := ferry.Load[host](t.Context(), New(Environ(noEnviron), DotEnv(path)))
	if err == nil {
		t.Fatal("the load took this file, want a refusal")
	}

	if !errorIs(err, ErrMalformed) {
		t.Errorf("the refusal is %+v, want it to answer errors.Is against ErrMalformed", err)
	}
}

// TestDotEnvWithNoPathsReadsTheDefaultFile is the one-argument-less call, and the
// name it reads is published as a constant so a caller can name the same file.
func TestDotEnvWithNoPathsReadsTheDefaultFile(t *testing.T) {
	t.Parallel()

	c := defaults()
	DotEnv().apply(&c)

	if len(c.dotenv) != 1 || c.dotenv[0] != DefaultDotEnvFile {
		t.Errorf("env.DotEnv() reads %v, want the one file %q", c.dotenv, DefaultDotEnvFile)
	}
}
