package env

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
)

// The read half is implemented, and this is the compile-time half of the claim
// that the write half is not. It fails to build the moment [Source] stops
// satisfying ferry.Source.
var _ ferry.Source = (*Source)(nil)

// TestDumpingToEnvDoesNotCompile is the acceptance criterion stated as something
// a test can actually execute.
//
// ferry.Dump's third parameter is a ferry.Sink, which is an interface, so
// ferry.Dump(ctx, v, env.New()) compiles exactly when *Source implements that
// interface and not otherwise. The assertion below is therefore the same
// question the compiler answers, asked at run time: it is false today, and it
// becomes true on the day somebody gives *Source a Bind returning a
// ferry.OpenWriterFunc - which is the day the call site would start compiling.
//
// It is not vacuous, and that is worth saying because a negative compile-time
// assertion usually is. A test asserting that some other type is not a sink
// would pass forever; this one names the type the criterion is about, and the
// only way to make it pass while the call site compiles is to delete it.
func TestDumpingToEnvDoesNotCompile(t *testing.T) {
	t.Parallel()

	if _, ok := any(New()).(ferry.Sink); ok {
		t.Error("*env.Source satisfies ferry.Sink, so ferry.Dump(ctx, v, env.New()) now compiles: setting the " +
			"process's own environment is process-global mutation, and the Dump target people want is a .env " +
			"file, which is a format and belongs to a driver of its own (ADR-0004)")
	}
}

// TestPackageDeclaresNoSink is the second half, and it covers what the first
// cannot: a sink shipped here as a type of its own rather than as a method on
// [Source].
//
// A ferry sink is a Bind returning a ferry.OpenWriterFunc, and there is no way
// to write one without naming that type, so the package's own source is scanned
// for it. Comments are not scanned - the selector has to appear as code - which
// is what lets this file and the package documentation say the words.
func TestPackageDeclaresNoSink(t *testing.T) {
	t.Parallel()

	files := shippedFiles(t)

	// The control. This package's read half is spelled with the same selector
	// shape its write half would be, so a scan that cannot see ferry.OpenFunc
	// could not see ferry.OpenWriterFunc either, and would pass by finding
	// nothing anywhere.
	if n := selectors(t, files, "OpenFunc"); n == 0 {
		t.Fatalf("the scan found no ferry.OpenFunc across %d files whose Bind returns one, so it is looking "+
			"in the wrong place and asserts nothing", len(files))
	}

	if n := selectors(t, files, "Sink", "OpenWriterFunc"); n != 0 {
		t.Errorf("this package's own source names ferry.Sink or ferry.OpenWriterFunc %d times, so it ships a "+
			"write half: env has no honest Dump, and the absence of a sink is a property of the plane rather "+
			"than a decision about scope (ADR-0004)", n)
	}
}

// shippedFiles is this package's own source, without its tests: what a consumer
// compiles when they import it.
func shippedFiles(t *testing.T) []string {
	t.Helper()

	all, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("listing this package's files: %v", err)
	}

	out := make([]string, 0, len(all))

	for _, path := range all {
		if !strings.HasSuffix(path, "_test.go") {
			out = append(out, path)
		}
	}

	return out
}

// selectors counts the qualified references to the named identifiers across a
// set of files, whatever package they are qualified by.
//
// The package qualifier is not checked, deliberately: an import alias would slip
// past a check for the literal text "ferry", and no name in this list belongs to
// anything else this package could plausibly import.
func selectors(t *testing.T, files []string, names ...string) int {
	t.Helper()

	fset := token.NewFileSet()
	found := 0

	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}

		found += countSelectors(f, names)
	}

	return found
}

// countSelectors is the walk one parsed file at a time, so that the loop above
// stays a loop and this stays a matcher.
func countSelectors(f *ast.File, names []string) int {
	found := 0

	ast.Inspect(f, func(node ast.Node) bool {
		if sel, ok := node.(*ast.SelectorExpr); ok && slices.Contains(names, sel.Sel.Name) {
			found++
		}

		return true
	})

	return found
}
