package env

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"

	"github.com/onhotpath/ferry"
)

// snapshot is the one lookup a load reads from: every file [DotEnv] named,
// layered lowest first, with the process environment on top.
//
// The order is the whole of the precedence rule. A later file wins over an
// earlier one because that is what naming them in order means, and the process
// wins over every file because the process is the plane's anchor: a variable
// somebody exported is the one their shell, their container and every other
// process in the tree already sees.
//
// It is taken once per open, so one load sees one consistent environment: a
// variable that changed half way through a walk would otherwise land in some
// fields and not others, with nothing saying so.
func (c *config) snapshot() (map[string]string, error) {
	out := make(map[string]string)

	for _, path := range c.dotenv {
		f, err := readDotEnv(path)
		if err != nil {
			return nil, err
		}

		if f != nil {
			f.into(out)
		}
	}

	maps.Copy(out, environMap(c.environ()))

	return out, nil
}

// into adds one file's assignments to a layer being built up.
//
// A file assigning one name twice is refused at the parse, so there is no
// first-wins or last-wins question to answer here.
func (f *file) into(out map[string]string) {
	for i := range f.lines {
		if ln := &f.lines[i]; ln.kind == kindAssign {
			out[ln.a.name] = ln.a.value
		}
	}
}

// readDotEnv parses one file, and is shared by both halves: a save merges into
// the file that is already there, so the sink reads it exactly as the source
// does.
//
// A file that is not there is no file at all rather than a failure, and that is
// what "the files are optional" means: a path nobody has written yet holds no
// variables, every field takes its default, and a required field fails. A dump
// to a path with no file at it is how the first one gets written.
//
// A file that is there and does not parse is a refusal, in both directions and
// at the open. On the read side that is the difference between a typo being
// reported and a load quietly answering that the file was empty; on the write
// side it is the difference between a refusal and a save overwriting a file it
// could not read.
//
// This package suppresses gosec's G304 here and in syncDir, and both are the
// same suppression. G304 reports a file opened from a variable, and for this
// driver the variable is the plane: the caller names the file, that naming is
// the whole of the option's API, and there is nothing to validate it against
// that would not be this package deciding which of its user's files are allowed
// to exist.
func readDotEnv(path string) (*file, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the path is the plane, and naming it is the whole API.
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil //nolint:nilnil // A file that is not there is no file, which is not a failure.
	}

	if err != nil {
		return nil, fmt.Errorf("%w: reading the file: %w", ferry.ErrPlane, err)
	}

	return parseFile(data)
}
